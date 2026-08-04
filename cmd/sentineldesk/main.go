// SentinelDesk
// A collaborative operating system for people and AI agents.
//
// Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
// Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
//
// Licensed under the Apache License, Version 2.0.
//
// This product's name and logo are trademarks of Federico Pereira and are not
// covered by the license above. See the README for the trademark policy.
//
// SPDX-License-Identifier: Apache-2.0

// Command sentineldesk runs a Linux desktop and streams it to the browser over
// WebRTC.
//
// One binary serves everything:
//   - HTTP : the embedded browser client and the file-transfer endpoints
//   - WS   : /ws, which is the only door — authentication happens there, and
//     nothing else is delivered until it succeeds
//   - MCP  : a local Unix socket that lets an AI agent drive the desktop
//
// Video (ximagesrc) and audio (pulsesrc) are captured and encoded by GStreamer
// inside this process (go-gst); each RTP packet goes straight from appsink to a
// Pion track. The encoder is chosen automatically (NVENC → VA-API → VP8) and
// steered at runtime: keyframes on PLI, bitrate from congestion estimation.
//
// This file is wiring only. The behaviour lives under internal/.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/tinyzimmer/go-gst/gst"

	"github.com/lordbasex/sentineldesk/internal/config"
	"github.com/lordbasex/sentineldesk/internal/desktop"
	"github.com/lordbasex/sentineldesk/internal/mcp"
	"github.com/lordbasex/sentineldesk/internal/media"
	"github.com/lordbasex/sentineldesk/internal/stream"
	"github.com/lordbasex/sentineldesk/internal/version"
	"github.com/lordbasex/sentineldesk/internal/webui"
)

// Accept the upgrade only from the page we serve, or when there is no Origin at
// all (non-browser clients — the WebSocket login validates them anyway).
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		return err == nil && strings.EqualFold(u.Host, r.Host)
	},
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("sentineldesk: ")

	// -mcp-stdio runs a thin stdio<->socket bridge for an AI host instead of the
	// daemon. It never touches the desktop.
	mcpStdio := flag.Bool("mcp-stdio", false, "run as MCP stdio bridge (connects to -mcp-sock)")
	mcpSock := flag.String("mcp-sock", config.Str("MCP_SOCK", ""), "MCP unix socket path")
	// These restrict ONLY this connection. The daemon sets the ceiling through
	// MCP_POLICY; a bridge can drop below it but never rise above, which is what
	// makes it safe to hand an agent a read-only endpoint.
	mcpPolicy := flag.String("mcp-policy", "", "restrict this bridge: full | safe | readonly")
	mcpDeny := flag.String("mcp-deny", "", "comma-separated tools to deny (suffix * for prefix match)")
	mcpAllow := flag.String("mcp-allow", "", "comma-separated allow-list for this bridge")
	// The binary as its own distribution: report what it is, serve the deploy
	// tree it was built with, or write that tree to disk for a script.
	showVersion := flag.Bool("version", false, "print version and exit")
	installMode := flag.Bool("install", false, "serve the embedded deploy files over HTTP (for installing elsewhere)")
	installPort := flag.Int("install-port", 80, "port for -install")
	extractDir := flag.String("extract-deploy", "", "write the embedded deploy files to this directory and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("sentineldesk", version.String())
		return
	}
	if *extractDir != "" {
		if err := extractDeploy(*extractDir); err != nil {
			log.Fatalf("extract-deploy: %v", err)
		}
		return
	}
	if *installMode {
		if err := runInstallServer(*installPort); err != nil {
			log.Fatalf("install: %v", err)
		}
		return
	}

	if *mcpStdio {
		if err := mcp.RunBridge(*mcpSock, *mcpPolicy, *mcpDeny, *mcpAllow); err != nil {
			log.Fatalf("mcp-stdio: %v", err)
		}
		return
	}

	cfg := config.Load()
	gst.Init(nil)

	injector, err := desktop.NewInputInjector(cfg.Display)
	if err != nil {
		log.Fatalf("cannot connect to display %s: %v", cfg.Display, err)
	}

	strategy := media.SelectEncoder(cfg.Encoder)
	log.Printf("video strategy: %s (%s, hardware=%v)", strategy.Name, strategy.MimeType, strategy.Hardware)

	// One room: the screen is encoded ONCE and fanned out to everyone watching,
	// rather than a pair of pipelines per client.
	room := stream.NewRoom(cfg, strategy)

	// One recorder, shared by the agent and by the toolbar button: a recording
	// is a single resource and two of them writing at once would collide.
	recorder := media.NewRecorder(cfg.Display, cfg.AudioDevice, "")

	// The return path: whatever the browser's microphone captures enters the
	// desktop as an ordinary capture device.
	//
	// Built now rather than on the first share. A page enumerates its audio
	// devices when it loads, so a microphone that appears later is missing from
	// the list of the very page that wanted it.
	upstream := media.NewUpstream(cfg)
	if err := upstream.EnsureMic(); err != nil {
		log.Printf("virtual microphone unavailable: %v", err)
	}

	// With a local cursor the client needs the real pointer shape (resize
	// arrows, text beam, hand…); XFixes reports every change.
	var cursors *desktop.CursorTracker
	if !cfg.RemoteCursor {
		if cursors, err = desktop.NewCursorTracker(cfg.Display); err != nil {
			log.Printf("no cursor-shape tracking (XFixes): %v", err)
			cursors = nil
		}
	}

	// Two-way clipboard (xclip) and virtual gamepad (uinput). Both optional: if
	// they are missing the desktop works fine without them.
	clip := desktop.NewClipboard(cfg.Display)
	joystick, err := desktop.NewJoystick()
	if err != nil {
		log.Printf("joystick disabled (no /dev/uinput): %v", err)
		joystick = nil
	} else {
		log.Printf("virtual gamepad created (uinput)")
	}

	auth := stream.NewAuth(cfg.AuthUser, cfg.AuthPass, cfg.AuthSecret, cfg.AuthTTL)
	webRoot := webui.FS()

	gate := stream.NewIPGate()

	mux := http.NewServeMux()
	mux.Handle("/", webui.Handler())

	// The documentation, behind the same login as the desktop.
	//
	// The client shell at "/" is served openly on purpose: it is an empty frame
	// that can do nothing until it authenticates over the WebSocket, and gating
	// it would only mean serving a login page from two places. The docs are
	// different — they are content, and on a deployment reachable from the
	// internet they would otherwise be readable by anyone who guessed the path.
	//
	// A page the browser navigates to cannot send a header, so this reads the
	// cookie the client mirrors its token into after logging in. No cookie, or a
	// stale one, and the reader is sent to the front door.
	mux.Handle("/docs/", requireSession(auth, webui.Handler()))

	// The browser's file manager (download/upload), behind the same session
	// token the WebSocket login issues.
	files := stream.NewFileServer(cfg.FilesRoot, auth)
	files.Register(mux)

	// Handing finished screenshots and recordings to the browsers. It is what
	// makes destination:download work for both the agent and the person.
	delivery := stream.NewDelivery(files, room)

	// MCP server: an AI agent drives the desktop over a local Unix socket.
	if *mcpSock != "" {
		server := mcp.NewServer(cfg, injector, joystick, clip, recorder)
		server.SetDelivery(delivery)
		// The agent joins the same room as the people: it gets a name in the
		// participant list and has to take turns with them. Without this it
		// would still work, but invisibly — a pointer moving on its own with
		// nobody able to tell whether a colleague or the model was driving.
		server.SetRoom(room, config.Str("AGENT_NAME", "AI agent"))
		if err := server.Listen(*mcpSock); err != nil {
			log.Printf("mcp: cannot open socket %s: %v", *mcpSock, err)
		}
	}

	// The only informational endpoint (always 200, no secrets): it says whether
	// a login is required. Credentials, ICE configuration and everything else
	// travel over the authenticated WebSocket.
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"required": auth.Enabled()})
	})

	// The languages the client can offer. Discovered from the embedded files,
	// so adding a translation is a matter of dropping a JSON into assets/lang.
	langs := webui.Languages()
	log.Printf("languages available: %d", len(langs))
	mux.HandleFunc("/lang/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		json.NewEncoder(w).Encode(langs)
	})

	// Some browsers request /favicon.ico unconditionally: always serve it so a
	// 404 never shows up in the console.
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(webRoot, "favicon.svg")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Rate-limit by origin BEFORE the upgrade: a banned or too-eager origin
		// does not even get to spend a WebSocket handshake.
		ip := stream.ClientIP(r)
		key := stream.GateKey(ip)
		if gate.Banned(key) {
			http.Error(w, "locked", http.StatusTooManyRequests)
			return
		}
		if ok, retryAfter := gate.Take(key); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds()+1)))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade: %v", err)
			return
		}
		sess := stream.NewSession(cfg, strategy, room, upstream, recorder, delivery, injector, cursors,
			clip, joystick, auth, gate, key, ws, ip)
		sess.Run() // blocks until the client disconnects
	})

	addr := ":" + strconv.Itoa(cfg.HTTPPort)
	certFile, keyFile, err := stream.EnsureTLS(cfg)
	if err != nil {
		log.Fatalf("TLS configuration: %v", err)
	}
	scheme := "http"
	if certFile != "" {
		scheme = "https"
	}
	log.Printf("desktop on %s://0.0.0.0%s (display %s, %d fps, auth=%v)",
		scheme, addr, cfg.Display, cfg.FPS, cfg.AuthUser != "")
	if certFile != "" {
		log.Fatal(http.ListenAndServeTLS(addr, certFile, keyFile, mux))
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}

// requireSession gates a handler on the session cookie the browser client sets
// once it has authenticated over the WebSocket.
//
// With authentication switched off it is a pass-through: there is no session to
// require, and refusing everybody on a development instance would be a puzzle
// rather than a protection.
func requireSession(auth *stream.Auth, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.Enabled() {
			h.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie("sentineldesk_session")
		if err == nil {
			if tok, derr := url.QueryUnescape(c.Value); derr == nil && auth.ValidToken(tok) {
				h.ServeHTTP(w, r)
				return
			}
		}
		// A redirect rather than a 401: whoever asked for this is a person with
		// a browser, and the useful answer is the login screen, not a status
		// code. Sub-resources get the 401 they can actually act on.
		if r.Header.Get("Sec-Fetch-Mode") == "navigate" || r.Method == http.MethodGet &&
			strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		http.Error(w, "not authenticated", http.StatusUnauthorized)
	})
}
