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

// The MCP (Model Context Protocol) server, which lets an AI drive the desktop.
//
// The daemon opens a local Unix socket (0600) and the -mcp-stdio sub-command is
// a thin stdio<->socket bridge that the AI host spawns. Killing the host
// therefore never takes the desktop down with it.
//
// The control logic reuses what already exists — desktop.InputInjector,
// desktop.Joystick, desktop.Clipboard — plus helpers for windows, execution and
// capture.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/lordbasex/sentineldesk/internal/config"
	"github.com/lordbasex/sentineldesk/internal/desktop"
	"github.com/lordbasex/sentineldesk/internal/media"
	"github.com/lordbasex/sentineldesk/internal/shell"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	mcpProtocolVersion = "2024-11-05"
	mcpServerName      = "sentineldesk"
)

// --- JSON-RPC 2.0 ---------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- Server ----------------------------------------------------------------

// Deliverer hands a file on the desktop's disk to the connected browsers, and
// reports how many were told. It is an interface so that this package does not
// depend on the streaming session just to send one message.
type Deliverer interface {
	Deliver(absPath, name string) int
}

// Server answers MCP requests and turns them into actions on the desktop.
type Server struct {
	cfg      config.Config
	display  string
	injector *desktop.InputInjector
	joystick *desktop.Joystick
	clip     *desktop.Clipboard
	recorder *media.Recorder
	shells   *shell.ShellManager
	sshm     *shell.SSHManager
	policy   *Policy
	actions  *ActionLog

	// How to hand a finished file to the browsers. Optional: with nobody
	// watching, destination:download degrades to leaving it on disk and saying
	// so, rather than failing.
	delivery Deliverer

	// The room, so the agent can be a participant rather than an invisible
	// hand: a name in the list, a marker on screen, and a turn at the controls.
	// Optional — a bridge process has none.
	room      Rooms
	agentName string

	// Where the recording in progress should end up, remembered from
	// start_recording because stop_recording is what has the file.
	recDestination string
	tools          []toolDef

	uiMu   sync.Mutex
	uiLast map[string]uiNode // last snapshot of the tree, for ui_diff

	restreamMu  sync.Mutex
	restream    *exec.Cmd
	restreamURL string
}

func NewServer(cfg config.Config, injector *desktop.InputInjector, joystick *desktop.Joystick, clip *desktop.Clipboard, rec *media.Recorder) *Server {
	s := &Server{
		cfg:      cfg,
		display:  cfg.Display,
		injector: injector,
		joystick: joystick,
		clip:     clip,
		recorder: rec,
		shells:   shell.NewShellManager(),
		sshm:     shell.NewSSHManager(),
		policy:   NewPolicy(),
		actions:  NewActionLog(),
	}
	s.tools = s.buildTools()
	return s
}

// SetDelivery wires up browser delivery. Without it, destination:download has
// nowhere to send the file and falls back to leaving it on the desktop.
func (s *Server) SetDelivery(d Deliverer) { s.delivery = d }

// SetRoom puts the agent in the shared session. Without it the agent still
// works, but invisibly and without arbitration — which is only right when
// nothing else can be watching.
func (s *Server) SetRoom(r Rooms, name string) {
	s.room, s.agentName = r, name
}

// deliver hands a file to the browsers, returning how many were told.
func (s *Server) deliver(path, name string) int {
	if s.delivery == nil {
		return 0
	}
	return s.delivery.Deliver(path, name)
}

// Listen opens the Unix socket and serves connections, one MCP session each.
func (s *Server) Listen(sockPath string) error {
	_ = os.Remove(sockPath) // a stale socket from an earlier run
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		log.Printf("mcp: could not set 0600 on the socket: %v", err)
	}
	log.Printf("mcp: listening on %s (%d tools)", sockPath, len(s.tools))
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return nil
}

// serve processes one connection's JSON-RPC messages, one per line.
//
// Each connection starts from the daemon's policy and may RESTRICT itself
// further (the "sentineldesk/policy" method) but never widen. That asymmetry is
// what makes it safe to hand an agent a read-only endpoint without changing
// anyone else's.
func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	connPolicy := s.policy
	var policyMu sync.RWMutex
	var writeMu sync.Mutex
	enc := json.NewEncoder(conn)
	write := func(resp rpcResponse) {
		writeMu.Lock()
		defer writeMu.Unlock()
		resp.JSONRPC = "2.0"
		_ = enc.Encode(resp)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		// This connection's restriction is handled inline rather than in a
		// goroutine, so that it is in force before any tool call that follows.
		if req.Method == "sentineldesk/policy" {
			var p struct {
				Level string `json:"level"`
				Deny  string `json:"deny"`
				Allow string `json:"allow"`
			}
			_ = json.Unmarshal(req.Params, &p)
			policyMu.Lock()
			connPolicy = s.policy.Restrict(p.Level, p.Deny, p.Allow)
			applied := connPolicy.Describe()
			policyMu.Unlock()
			if req.ID != nil {
				write(rpcResponse{ID: req.ID, Result: applied})
			}
			continue
		}

		policyMu.RLock()
		active := connPolicy
		policyMu.RUnlock()

		// One goroutine per request: a slow or wedged tool must not freeze the
		// rest of the connection. The client pairs responses by id anyway.
		go s.handle(req, write, active)
	}
}

func (s *Server) handle(req rpcRequest, write func(rpcResponse), policy *Policy) {
	switch req.Method {
	case "initialize":
		write(rpcResponse{ID: req.ID, Result: map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": mcpServerName, "version": buildVersion()},
		}})
	case "notifications/initialized", "initialized":
		// a notification: no response is expected
	case "ping":
		write(rpcResponse{ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		// Advertise only what this connection may use. Offering a tool that
		// will be refused is an invitation to walk into a wall.
		write(rpcResponse{ID: req.ID, Result: map[string]any{"tools": policy.Filter(s.tools)}})
	case "tools/call":
		s.handleToolCall(req, write, policy)
	default:
		if req.ID != nil {
			write(rpcResponse{ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
		}
	}
}

func (s *Server) handleToolCall(req rpcRequest, write func(rpcResponse), policy *Policy) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		write(rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
		return
	}
	// The single point every tool call passes through, which is where policy is
	// applied and the action log written — without cluttering each tool.
	args := map[string]any{}
	if len(params.Arguments) > 0 {
		_ = json.Unmarshal(params.Arguments, &args)
	}
	entry := actionEntry{
		Time: nowStamp(), Tool: params.Name, Args: summarizeArgs(args),
		VideoAt: videoOffset(s.recorder),
	}

	if ok, reason := policy.Allowed(params.Name, args); !ok {
		entry.OK = false
		entry.Denied = reason
		s.actions.Add(entry)
		write(rpcResponse{ID: req.ID, Result: map[string]any{
			"content": textContent("denied by the server policy: %s", reason),
			"isError": true,
		}})
		return
	}

	// Turn-taking, enforced in ONE place so a new input tool cannot forget it.
	// Policy above is the hard ceiling; this is the cooperative layer below it.
	if injectsInput(params.Name) {
		if err := s.mayInject(); err != nil {
			entry.OK = false
			entry.Denied = "room arbitration"
			s.actions.Add(entry)
			write(rpcResponse{ID: req.ID, Result: map[string]any{
				"content": textContent("%v", err),
				"isError": true,
			}})
			return
		}
	}

	start := time.Now()
	content, isErr := s.dispatch(params.Name, params.Arguments)
	entry.Millis = time.Since(start).Milliseconds()
	entry.OK = !isErr
	s.actions.Add(entry)

	write(rpcResponse{ID: req.ID, Result: map[string]any{
		"content": content,
		"isError": isErr,
	}})
}

// injectsInput lists the tools that put events into X, which is where an agent
// and a person actually collide.
//
// Deliberately narrow. Installing a package or reading a file while somebody
// works is not a conflict; two hands on the same mouse is. Widening this to
// every state-changing tool would make the agent useless for background work.
func injectsInput(name string) bool {
	switch name {
	case "mouse_move", "mouse_click", "mouse_down", "mouse_up", "mouse_drag",
		"mouse_scroll", "type_text", "key_combo",
		"gamepad_button", "gamepad_axis", "gamepad_state", "gamepad_tap",
		"ui_click", "ui_set_text", "ui_focus", "fill_form", "terminal_run":
		return true
	// Not input, but held to the same rule and for a stronger reason: this
	// publishes what is on everyone's screen to somewhere outside the room.
	// Starting or stopping that while a person is working is not the agent's
	// call to make alone.
	case "start_restream", "stop_restream":
		return true
	}
	return false
}

func buildVersion() string { return "1.0.0" }

// --- Result helpers -------------------------------------------------

// textContent builds a text MCP content block.
func textContent(format string, args ...any) []map[string]any {
	return []map[string]any{{"type": "text", "text": fmt.Sprintf(format, args...)}}
}

// imageContent builds an image MCP content block (base64 PNG).
func imageContent(b64, mime string) []map[string]any {
	return []map[string]any{{"type": "image", "data": b64, "mimeType": mime}}
}

// jsonContent serialises v and returns it as text, for structured answers.
func jsonContent(v any) []map[string]any {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return textContent("error serializando resultado: %v", err)
	}
	return textContent("%s", string(b))
}

// --- Bridge stdio <-> socket (sub-comando -mcp-stdio) ---------------------

// RunBridge wires stdin/stdout to the daemon's socket. The AI host
// spawns "sentineldesk -mcp-stdio"; this process is only a pipe.
func RunBridge(sockPath, level, deny, allow string) error {
	if sockPath == "" {
		return fmt.Errorf("-mcp-sock is required with -mcp-stdio")
	}
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("could not connect to the MCP socket %s: %w", sockPath, err)
	}
	defer conn.Close()

	// THIS connection's restriction, sent before anything from the client is
	// allowed through. The server only applies it if it is stricter than its
	// own, so a bridge can never gain permissions — only give them up.
	if level != "" || deny != "" || allow != "" {
		req := map[string]any{
			"jsonrpc": "2.0", "method": "sentineldesk/policy",
			"params": map[string]string{"level": level, "deny": deny, "allow": allow},
		}
		b, _ := json.Marshal(req)
		if _, err := conn.Write(append(b, '\n')); err != nil {
			return fmt.Errorf("could not set the connection policy: %w", err)
		}
	}

	done := make(chan struct{}, 2)
	go func() { io.Copy(conn, os.Stdin); done <- struct{}{} }()
	go func() { io.Copy(os.Stdout, conn); done <- struct{}{} }()
	<-done
	return nil
}
