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

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/lordbasex/sentineldesk/internal/desktop"
	"github.com/lordbasex/sentineldesk/internal/media"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// toolDef is one entry in the MCP catalogue: name, description, input schema
// and what the tool is allowed to do to the machine.
type toolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`

	// Risk is mandatory. It drives the MCP_POLICY levels and the annotations
	// published in tools/list, and it lives here rather than in a table
	// elsewhere so that adding a tool and classifying it are the same edit —
	// see registry.go for what the separation used to cost. The zero value is
	// riskUnset and fails at startup; there is no safe default to fall back on,
	// because the two plausible ones point in opposite directions.
	Risk riskLevel `json:"-"`

	// RequiresControl marks the tools that must hold the room's controls before
	// they run — the ones that put events into X, plus the two that publish the
	// desktop outside the room.
	//
	// Unlike Risk this has a meaningful default: most tools do not need the
	// desktop, and false is the conservative answer because it grants nothing.
	// It lives here for the other reason Risk does — so that a client can be
	// told, rather than having to know. See registry.go.
	RequiresControl bool `json:"-"`
}

// --- JSON Schema helpers -------------------------------------------------

func schema(props map[string]any, required ...string) json.RawMessage {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	b, _ := json.Marshal(m)
	return b
}
func pStr(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func pInt(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }
func pBool(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

// --- catalogue -------------------------------------------------------------

func (s *Server) buildTools() []toolDef {
	base := []toolDef{
		{
			Name:        "screenshot",
			Risk:        riskRead,
			Description: "Capture the current desktop screen. destination: inline (default) returns the PNG to you; container writes it to a file on the desktop; download makes the browser of whoever is watching save it on their own machine. The capture is identical in all three — it comes straight from the X framebuffer, with no compression loss.",
			InputSchema: schema(map[string]any{
				"destination": pStr("inline | container | download (default inline)"),
				"path":        pStr("where to write it, with destination container/download (default the recordings directory)"),
			}),
		},
		{
			Name:            "mouse_move",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Move the mouse pointer to absolute screen coordinates (x, y).",
			InputSchema:     schema(map[string]any{"x": pInt("X coordinate"), "y": pInt("Y coordinate")}, "x", "y"),
		},
		{
			Name:            "mouse_click",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Click a mouse button. Optionally move to (x, y) first. button: 1=left (default), 2=middle, 3=right. Set double=true for a double click.",
			InputSchema: schema(map[string]any{
				"x": pInt("optional X to move to first"), "y": pInt("optional Y to move to first"),
				"button": pInt("1=left, 2=middle, 3=right"), "double": pBool("double click"),
			}),
		},
		{
			Name:            "type_text",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Type a string of text into the focused window (handles any character, including accents).",
			InputSchema:     schema(map[string]any{"text": pStr("text to type")}, "text"),
		},
		{
			Name:            "key_combo",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Press a key or key combination using X keysym names, e.g. 'Return', 'Escape', 'ctrl+c', 'alt+Tab', 'super+d', 'ctrl+shift+t'.",
			InputSchema:     schema(map[string]any{"keys": pStr("key or combo, e.g. ctrl+c")}, "keys"),
		},
		{
			Name:        "launch_app",
			Risk:        riskDanger,
			Description: "Launch a program on the desktop (runs detached, does not block). Pass the command line, e.g. 'firefox-esr', 'lxterminal', 'chromium https://example.com'. Set as_root:true for administration GUIs that need privileges (a file manager on /etc, gparted, synaptic).",
			InputSchema: schema(map[string]any{
				"command": pStr("command line to run"),
				"as_root": pBool("launch as root via sudo (default false)"),
			}, "command"),
		},
		{
			Name:        "list_windows",
			Risk:        riskRead,
			Description: "List all open windows: window id, desktop, geometry (x,y,w,h), class and title. Use the id with activate_window/close_window.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "activate_window",
			Risk:        riskWrite,
			Description: "Focus and raise a window by its id (from list_windows).",
			InputSchema: schema(map[string]any{"id": pStr("window id, e.g. 0x02000007")}, "id"),
		},
		{
			Name:        "run_command",
			Risk:        riskDanger,
			Description: "Run a shell command inside the desktop and return stdout, stderr and exit code. Full control of the container — use for diagnostics and automation. Set as_root:true to run it through passwordless sudo (edit /etc, manage services, install things).",
			InputSchema: schema(map[string]any{
				"command": pStr("shell command"), "timeout_ms": pInt("timeout in ms (default 15000)"),
				"as_root": pBool("run as root via sudo (default false)"),
			}, "command"),
		},
		{
			Name:        "wait",
			Risk:        riskRead,
			Description: "Sleep for the given number of milliseconds (give the UI time to react before the next action).",
			InputSchema: schema(map[string]any{"ms": pInt("milliseconds to wait")}, "ms"),
		},
		{
			Name:        "start_recording",
			Risk:        riskWrite,
			Description: "Start recording the screen (and optionally audio) to a video file, in parallel with the live stream. container: mp4 (default, H.264+AAC), webm (VP8+Opus) or mkv. Returns the output path.",
			InputSchema: schema(map[string]any{
				"container":   pStr("mp4 | webm | mkv (default mp4)"),
				"fps":         pInt("frames per second (default 30)"),
				"bitrate":     pInt("video bitrate in kbps (default 4000)"),
				"audio":       pBool("also record audio (default true)"),
				"destination": pStr("container (default) keeps the file on the desktop; download makes the watching browser save it when the recording stops"),
			}),
		},
		{
			Name:        "stop_recording",
			Risk:        riskWrite,
			Description: "Stop the current recording, finalize the file cleanly and return its path and size in bytes. destination overrides the one chosen at start: download hands the finished file to the browser of whoever is watching.",
			InputSchema: schema(map[string]any{
				"destination": pStr("container | download"),
			}),
		},
		{
			Name:        "get_recording_status",
			Risk:        riskRead,
			Description: "Report whether a recording is in progress, with elapsed seconds, current size and path.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "list_recordings",
			Risk:        riskRead,
			Description: "List the recorded video files (path, size, modified time).",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "get_clipboard",
			Risk:        riskRead,
			Description: "Read the desktop clipboard (X CLIPBOARD selection) as text.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "set_clipboard",
			Risk:        riskWrite,
			Description: "Write text to the desktop clipboard (so it can be pasted with Ctrl+V).",
			InputSchema: schema(map[string]any{"text": pStr("text to place on the clipboard")}, "text"),
		},
	}
	base = append(base, s.buildRegistryTools()...)
	base = append(base, s.buildAdvancedTools()...)
	base = append(base, s.buildUITools()...)
	base = append(base, s.buildSysTools()...)
	base = append(base, s.buildRootTools()...)
	base = append(base, s.buildNextTools()...)
	base = append(base, s.roomTools()...)
	base = append(base, s.terminalTools()...)
	return append(base, s.buildBrowserTools()...)
}

// --- despacho -------------------------------------------------------------

// dispatch runs a tool and returns its MCP content plus an error flag.
func (s *Server) dispatch(ctx context.Context, name string, rawArgs json.RawMessage, policy *Policy) ([]map[string]any, bool) {
	args := map[string]any{}
	if len(rawArgs) > 0 {
		_ = json.Unmarshal(rawArgs, &args)
	}
	// The catalogue asking about itself. It comes first because it is the one
	// tool whose answer depends on the caller's policy rather than the desktop.
	if content, isErr, handled := s.dispatchRegistry(ctx, name, args, policy); handled {
		return content, isErr
	}
	// Sharing the desktop: these answer about the room rather than touching it.
	if out, isErr, handled := s.callTerminal(ctx, name, args); handled {
		if content, ok := out.([]map[string]any); ok {
			return content, isErr
		}
		return jsonContent(out), isErr
	}
	if out, isErr, handled := s.callRoom(ctx, name, args); handled {
		if content, ok := out.([]map[string]any); ok {
			return content, isErr
		}
		return jsonContent(out), isErr
	}

	switch name {
	case "screenshot":
		return s.toolScreenshot(args)
	case "mouse_move":
		return s.toolMouseMove(argInt(args, "x"), argInt(args, "y"))
	case "mouse_click":
		return s.toolMouseClick(args)
	case "type_text":
		return s.toolTypeText(argStr(args, "text"))
	case "key_combo":
		return s.toolKeyCombo(argStr(args, "keys"))
	case "launch_app":
		asRoot, _ := args["as_root"].(bool)
		return s.toolLaunchApp(argStr(args, "command"), asRoot)
	case "list_windows":
		return s.toolListWindows()
	case "activate_window":
		return s.toolActivateWindow(argStr(args, "id"))
	case "run_command":
		asRoot, _ := args["as_root"].(bool)
		return s.toolRunCommand(ctx, argStr(args, "command"), argInt(args, "timeout_ms"), asRoot)
	case "wait":
		return s.toolWait(argInt(args, "ms"))
	case "start_recording":
		return s.toolStartRecording(args)
	case "stop_recording":
		return s.toolStopRecording(args)
	case "get_recording_status":
		return jsonContent(s.recorder.Status()), false
	case "list_recordings":
		return s.toolListRecordings()
	case "get_clipboard":
		text, _ := s.clip.Get()
		return textContent("%s", text), false
	case "set_clipboard":
		s.clip.Set(argStr(args, "text"))
		return textContent("clipboard set"), false
	}
	// Advanced tools: windows, processes, OCR, gamepad, files, streaming
	if content, isErr, handled := s.dispatchAdvanced(ctx, name, args); handled {
		return content, isErr
	}
	// Accessibility tools: operate by structure rather than by pixels
	if content, isErr, handled := s.dispatchUI(ctx, name, args); handled {
		return content, isErr
	}
	// Browser tools over CDP, against the real DOM
	if content, isErr, handled := s.dispatchBrowser(ctx, name, args); handled {
		return content, isErr
	}
	// Persistent terminal, SSH and low-level windows
	if content, isErr, handled := s.dispatchSys(ctx, name, args); handled {
		return content, isErr
	}
	// Administration: privileges, packages and services
	if content, isErr, handled := s.dispatchRoot(ctx, name, args); handled {
		return content, isErr
	}
	// Resolution, smart waits, macro-actions, diffing, snapshots, action log
	if content, isErr, handled := s.dispatchNext(ctx, name, args); handled {
		return content, isErr
	}
	return textContent("unknown tool: %s", name), true
}

func argStr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
func argInt(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// --- implementaciones (P0) ------------------------------------------------

// toolScreenshot captures the screen. Where the picture ends up is the caller's
// choice: inline (the agent looks at it), on the desktop's disk, or downloaded
// by whoever is watching in a browser. The capture itself is the same in all
// three cases — it comes from the X framebuffer, with no compression loss.
func (s *Server) toolScreenshot(args map[string]any) ([]map[string]any, bool) {
	dest := strings.ToLower(argStr(args, "destination"))
	if dest == "" || dest == "inline" {
		b64, err := desktop.GrabScreenshotPNG(s.display)
		if err != nil {
			return textContent("screenshot failed: %v", err), true
		}
		return imageContent(b64, "image/png"), false
	}

	path := argStr(args, "path")
	if path == "" {
		path = filepath.Join(s.recorder.Dir,
			"screenshot-"+time.Now().Format("20060102-150405")+".png")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return textContent("screenshot failed: %v", err), true
	}
	if err := desktop.GrabToFile(s.display, path, 0, 0, 0, 0); err != nil {
		return textContent("screenshot failed: %v", err), true
	}

	res := map[string]any{"path": path}
	if fi, err := os.Stat(path); err == nil {
		res["size_bytes"] = fi.Size()
	}
	if dest == "download" {
		res["delivered_to"] = s.deliver(path, filepath.Base(path))
		if res["delivered_to"] == 0 {
			res["note"] = "nobody is watching in a browser: the file stayed on the desktop"
		}
	}
	return jsonContent(res), false
}

func (s *Server) toolMouseMove(x, y int) ([]map[string]any, bool) {
	s.injector.Move(x, y)
	s.reportPointer(x, y)
	return textContent("moved to (%d, %d)", x, y), false
}

func (s *Server) toolMouseClick(args map[string]any) ([]map[string]any, bool) {
	if _, ok := args["x"]; ok {
		s.injector.Move(argInt(args, "x"), argInt(args, "y"))
		s.reportPointer(argInt(args, "x"), argInt(args, "y"))
	}
	btn := argInt(args, "button")
	if btn == 0 {
		btn = 1
	}
	clicks := 1
	if b, _ := args["double"].(bool); b {
		clicks = 2
	}
	for i := 0; i < clicks; i++ {
		s.injector.Button(btn, true)
		s.injector.Button(btn, false)
	}
	return textContent("clicked button %d x%d", btn, clicks), false
}

func (s *Server) toolTypeText(text string) ([]map[string]any, bool) {
	if text == "" {
		return textContent("nothing to type"), false
	}
	if err := s.xdo("type", "--clearmodifiers", "--", text); err != nil {
		return textContent("type failed: %v", err), true
	}
	return textContent("typed %d chars", len([]rune(text))), false
}

func (s *Server) toolKeyCombo(keys string) ([]map[string]any, bool) {
	if keys == "" {
		return textContent("no keys given"), true
	}
	if err := s.xdo("key", "--clearmodifiers", keys); err != nil {
		return textContent("key failed: %v", err), true
	}
	return textContent("pressed %s", keys), false
}

func (s *Server) toolLaunchApp(command string, asRoot bool) ([]map[string]any, bool) {
	if command == "" {
		return textContent("no command given"), true
	}
	// setsid detaches the process from the daemon, so closing the MCP
	// connection does not take the application down with it.
	args := []string{"sh", "-c", command}
	if asRoot {
		if !sudoAvailable {
			return textContent("this image has no passwordless sudo"), true
		}
		// -E preserves DISPLAY. Xvfb runs with -ac, so root can open windows on
		// :0 without having to fight the xauth cookie.
		args = append([]string{"sudo", "-n", "-E"}, args...)
	}
	cmd := exec.Command("setsid", args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+s.display)
	// Capture stderr. Detached applications write their failures there and
	// nowhere else, so without this a missing library or a bad configuration
	// disappears completely and the agent is told the launch worked.
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return textContent("launch failed: %v", err), true
	}

	// Start() only reports that the fork succeeded. A program that exits a
	// moment later — no such binary inside the shell, a missing .so, no
	// display — used to be indistinguishable from one that started fine.
	// Waiting briefly turns "launched" into something worth believing.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		stderr := strings.TrimSpace(errBuf.String())
		if len(stderr) > 1200 {
			stderr = stderr[len(stderr)-1200:]
		}
		out := map[string]any{
			"command": command,
			"running": false,
			"error":   fmt.Sprintf("the program exited immediately: %v", err),
		}
		if stderr != "" {
			out["stderr"] = stderr
		} else {
			out["hint"] = "it printed nothing to stderr; check the command name " +
				"with list_installed_apps, or run it through terminal_run to see " +
				"the shell's own error"
		}
		return jsonContent(out), true

	case <-time.After(700 * time.Millisecond):
		// Still alive. Keep reaping in the background so it does not zombie.
		go func() { <-done }()
		out := map[string]any{
			"command": command, "running": true, "pid": cmd.Process.Pid,
			"as_root": asRoot,
			"note": "still running after 700 ms. A window may take longer to " +
				"appear — use wait_for_window, or open_app_and_wait next time.",
		}
		return jsonContent(out), false
	}
}

type windowInfo struct {
	ID      string `json:"id"`
	Desktop int    `json:"desktop"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Class   string `json:"class"`
	Title   string `json:"title"`
}

// listWindows returns the open windows, already parsed. Besides the tool itself,
// the macro-actions (open_app_and_wait) use it to spot the window that appeared.
func (s *Server) listWindows() []windowInfo {
	// wmctrl -l -G -x: id desktop x y w h wm_class host title
	//                   0   1      2 3 4 5 6        7    8+
	out, err := s.output("wmctrl", "-l", "-G", "-x")
	if err != nil {
		return nil
	}
	var wins []windowInfo
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 9 {
			continue
		}
		w := windowInfo{ID: f[0], Class: f[6]}
		w.Desktop, _ = strconv.Atoi(f[1])
		w.X, _ = strconv.Atoi(f[2])
		w.Y, _ = strconv.Atoi(f[3])
		w.W, _ = strconv.Atoi(f[4])
		w.H, _ = strconv.Atoi(f[5])
		w.Title = strings.Join(f[8:], " ")
		wins = append(wins, w)
	}
	return wins
}

func (s *Server) toolListWindows() ([]map[string]any, bool) {
	return jsonContent(s.listWindows()), false
}

func (s *Server) toolActivateWindow(id string) ([]map[string]any, bool) {
	if id == "" {
		return textContent("no window id"), true
	}
	if err := s.run("wmctrl", "-i", "-a", id); err != nil {
		return textContent("activate failed: %v", err), true
	}
	return textContent("activated window %s", id), false
}

func (s *Server) toolRunCommand(ctx context.Context, command string, timeoutMs int, asRoot bool) ([]map[string]any, bool) {
	if command == "" {
		return textContent("no command"), true
	}
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	cmd, err := elevate(ctx, command, asRoot)
	if err != nil {
		return textContent("%v", err), true
	}
	cmd.Env = append(os.Environ(), "DISPLAY="+s.display)
	// WaitDelay matters when the command forks a child that inherits the pipe —
	// xclip daemonising to own the selection, for instance. Without it we would
	// wait for an EOF that never comes.
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return jsonContent(map[string]any{
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"timed_out": ctx.Err() == context.DeadlineExceeded,
		"as_root":   asRoot,
	}), false
}

func (s *Server) toolWait(ms int) ([]map[string]any, bool) {
	if ms < 0 {
		ms = 0
	}
	if ms > 60000 {
		ms = 60000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return textContent("waited %d ms", ms), false
}

func (s *Server) toolStartRecording(args map[string]any) ([]map[string]any, bool) {
	audio := true
	if v, ok := args["audio"].(bool); ok {
		audio = v
	}
	path, err := s.recorder.Start(media.RecordOpts{
		Container: argStr(args, "container"),
		FPS:       argInt(args, "fps"),
		Kbps:      argInt(args, "bitrate"),
		Audio:     audio,
	})
	if err != nil {
		return textContent("start_recording failed: %v", err), true
	}
	// Remember where this recording should go, because stop_recording is the
	// call that has a finished file to hand over.
	s.recDestination = strings.ToLower(argStr(args, "destination"))
	return textContent("recording to %s", path), false
}

func (s *Server) toolStopRecording(args map[string]any) ([]map[string]any, bool) {
	path, size, err := s.recorder.Stop()
	if err != nil {
		return textContent("stop_recording failed: %v", err), true
	}
	res := map[string]any{"path": path, "size_bytes": size}

	// The destination given here wins; otherwise the one chosen at start.
	dest := strings.ToLower(argStr(args, "destination"))
	if dest == "" {
		dest = s.recDestination
	}
	s.recDestination = ""
	if dest == "download" {
		res["delivered_to"] = s.deliver(path, filepath.Base(path))
		if res["delivered_to"] == 0 {
			res["note"] = "nobody is watching in a browser: the file stayed on the desktop"
		}
	}
	return jsonContent(res), false
}

func (s *Server) toolListRecordings() ([]map[string]any, bool) {
	entries, err := os.ReadDir(s.recorder.Dir)
	if err != nil {
		return textContent("list_recordings failed: %v", err), true
	}
	var recs []map[string]any
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		recs = append(recs, map[string]any{
			"path":       s.recorder.Dir + "/" + e.Name(),
			"size_bytes": fi.Size(),
			"modified":   fi.ModTime().Format(time.RFC3339),
		})
	}
	return jsonContent(recs), false
}

// --- execution helpers that carry DISPLAY ---------------------------------

func (s *Server) xdo(args ...string) error {
	return s.run("xdotool", args...)
}

func (s *Server) run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+s.display)
	return cmd.Run()
}

func (s *Server) output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+s.display)
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

var _ = fmt.Sprintf
