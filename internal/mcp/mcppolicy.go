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

// Permission policy and action log for the MCP server.
//
// With root available the MCP has total power over the container. That is fine
// when a person is driving it and wrong when an unsupervised agent is. Almost
// every MCP server in the ecosystem grants unrestricted access with no middle
// ground; this offers three levels and two lists.
//
//	MCP_POLICY=full      (default) everything is allowed
//	MCP_POLICY=safe      everything except what changes the system or runs code
//	MCP_POLICY=readonly  observation only: see the screen, read the tree, list
//
//	MCP_DENY=run_command,ssh_*     additionally deny these (suffix * for prefix)
//	MCP_ALLOW=screenshot,ui_*      when set, ONLY these
//
// The log records every call with its timestamp. While a recording is running it
// also stores the position within the video, so the .mp4 ends up indexed by
// action and what happened can be audited or replayed.

import (
	"encoding/json"
	"fmt"
	"github.com/lordbasex/sentineldesk/internal/media"
	"os"
	"strings"
	"sync"
	"time"
)

// --- tool classification -----------------------------------------------------

// readOnlyTools observe without changing anything. They are the only ones that
// survive
// MCP_POLICY=readonly.
var readOnlyTools = map[string]bool{
	"screenshot": true, "screenshot_region": true, "get_screen_info": true,
	"get_pixel_color": true, "read_screen_text": true, "find_text": true,
	"get_mouse_position": true, "get_active_window": true, "list_windows": true,
	"list_desktops": true, "list_processes": true, "is_running": true,
	"list_installed_apps": true, "read_file": true, "list_directory": true,
	"get_clipboard": true, "get_audio_state": true, "get_desktop_info": true,
	"get_recording_status": true, "list_recordings": true, "wait": true,
	"ui_tree": true, "ui_find": true, "ui_get_text": true, "ui_wait_for": true,
	"ui_diff": true, "browser_text": true, "browser_tabs": true,
	"window_properties": true, "window_hierarchy": true, "wait_for_window": true,
	"wait_for_idle": true, "shell_list": true, "ssh_list": true, "ssh_tunnels": true,
	"ssh_list_remote": true, "sudo_status": true, "search_packages": true,
	"action_log": true, "snapshot_list": true, "list_restreams": true,
}

// dangerousTools run code, touch the system, or move data outward. These are the
// ones MCP_POLICY=safe removes.
var dangerousTools = map[string]bool{
	"run_command": true, "launch_app": true, "kill_process": true,
	"write_file": true, "browser_eval": true,
	"shell_open": true, "shell_exec": true, "shell_input": true, "shell_close": true,
	"ssh_connect": true, "ssh_exec": true, "ssh_upload": true, "ssh_download": true,
	"ssh_tunnel_local": true, "ssh_tunnel_remote": true, "ssh_tunnel_close": true,
	"ssh_keygen": true, "ssh_copy_id": true, "ssh_disconnect": true,
	"install_packages": true, "remove_packages": true, "service_control": true,
	"set_resolution": true, "snapshot_restore": true, "snapshot_delete": true,
	"start_restream": true, "stop_restream": true,
}

// Policy decides whether a tool may run.
type Policy struct {
	level string   // full | safe | readonly
	deny  []string // patterns (prefix match with *)
	allow []string // when non-empty, an exclusive allow-list
}

func NewPolicy() *Policy {
	p := &Policy{level: strings.ToLower(strings.TrimSpace(os.Getenv("MCP_POLICY")))}
	switch p.level {
	case "safe", "readonly", "full":
	case "":
		p.level = "full"
	default:
		fmt.Fprintf(os.Stderr, "mcp: unknown MCP_POLICY=%q, using full\n", p.level)
		p.level = "full"
	}
	p.deny = splitPatterns(os.Getenv("MCP_DENY"))
	p.allow = splitPatterns(os.Getenv("MCP_ALLOW"))
	return p
}

func splitPatterns(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func matchPattern(pat, name string) bool {
	if strings.HasSuffix(pat, "*") {
		return strings.HasPrefix(name, strings.TrimSuffix(pat, "*"))
	}
	return pat == name
}

// Allowed reports whether the tool may run and, when it may not, why.
func (p *Policy) Allowed(name string, args map[string]any) (bool, string) {
	for _, pat := range p.deny {
		if matchPattern(pat, name) {
			return false, fmt.Sprintf("%q is in MCP_DENY", name)
		}
	}
	if len(p.allow) > 0 {
		ok := false
		for _, pat := range p.allow {
			if matchPattern(pat, name) {
				ok = true
				break
			}
		}
		if !ok {
			return false, fmt.Sprintf("MCP_ALLOW is set and does not include %q", name)
		}
	}
	switch p.level {
	case "readonly":
		if !readOnlyTools[name] {
			return false, fmt.Sprintf("MCP_POLICY=readonly: %q changes the system", name)
		}
	case "safe":
		if dangerousTools[name] {
			return false, fmt.Sprintf("MCP_POLICY=safe: %q runs code or touches the system", name)
		}
		// as_root escalates even when the tool itself is harmless.
		if v, ok := args["as_root"].(bool); ok && v {
			return false, "MCP_POLICY=safe: as_root is disabled"
		}
	}
	return true, ""
}

// levelRank orders the levels from most to least permissive.
var levelRank = map[string]int{"full": 2, "safe": 1, "readonly": 0}

// Restrict returns a policy equal to or STRICTER than the current one, never
// more permissive: a client may give up permissions, not grant itself any. That
// is what makes it possible to hand an agent a read-only connection to the very
// same daemon you are using with full access.
func (p *Policy) Restrict(level, deny, allow string) *Policy {
	out := &Policy{level: p.level}
	out.deny = append(append([]string{}, p.deny...), splitPatterns(deny)...)

	if level = strings.ToLower(strings.TrimSpace(level)); level != "" {
		if r, ok := levelRank[level]; ok && r < levelRank[p.level] {
			out.level = level
		}
	}

	// Intersect the allow-lists: if the daemon already limited things to a set,
	// the connection may only keep a subset of it.
	req := splitPatterns(allow)
	switch {
	case len(p.allow) == 0:
		out.allow = req
	case len(req) == 0:
		out.allow = p.allow
	default:
		for _, r := range req {
			for _, base := range p.allow {
				if matchPattern(base, strings.TrimSuffix(r, "*")) || base == r {
					out.allow = append(out.allow, r)
					break
				}
			}
		}
		if out.allow == nil {
			// Nothing in common, so nothing is allowed. Saying so explicitly
			// beats letting an empty list read as "no restrictions".
			out.allow = []string{"\x00none"}
		}
	}
	return out
}

// Filter keeps only the tools this policy allows, so that tools/list never
// advertises something the server is going to refuse.
func (p *Policy) Filter(tools []toolDef) []toolDef {
	if p.level == "full" && len(p.deny) == 0 && len(p.allow) == 0 {
		return tools
	}
	out := make([]toolDef, 0, len(tools))
	for _, t := range tools {
		if ok, _ := p.Allowed(t.Name, nil); ok {
			out = append(out, t)
		}
	}
	return out
}

func (p *Policy) Describe() map[string]any {
	return map[string]any{"level": p.level, "deny": p.deny, "allow": p.allow}
}

// --- action log --------------------------------------------------------------

type actionEntry struct {
	Time    string `json:"time"`
	Tool    string `json:"tool"`
	Args    string `json:"args,omitempty"`
	OK      bool   `json:"ok"`
	Millis  int64  `json:"ms"`
	VideoAt string `json:"video_at,omitempty"` // mm:ss within the recording
	Denied  string `json:"denied,omitempty"`
}

// ActionLog keeps the most recent actions in memory and, on request, in a JSONL
// file.
type ActionLog struct {
	mu      sync.Mutex
	entries []actionEntry
	max     int
	file    *os.File
}

func NewActionLog() *ActionLog {
	l := &ActionLog{max: 2000}
	if path := os.Getenv("ACTION_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: could not open ACTION_LOG=%s: %v\n", path, err)
		} else {
			l.file = f
		}
	}
	return l
}

// Add records one call. videoAt is empty when no recording is running.
func (l *ActionLog) Add(e actionEntry) {
	l.mu.Lock()
	l.entries = append(l.entries, e)
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
	f := l.file
	l.mu.Unlock()
	if f != nil {
		if b, err := json.Marshal(e); err == nil {
			f.Write(append(b, '\n'))
		}
	}
}

// Tail returns the last n entries, optionally filtered by tool name.
func (l *ActionLog) Tail(n int, filter string) []actionEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.entries) {
		n = len(l.entries)
	}
	src := l.entries[len(l.entries)-n:]
	if filter == "" {
		out := make([]actionEntry, len(src))
		copy(out, src)
		return out
	}
	var out []actionEntry
	for _, e := range src {
		if strings.Contains(e.Tool, filter) {
			out = append(out, e)
		}
	}
	return out
}

func (l *ActionLog) Clear() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(l.entries)
	l.entries = nil
	return n
}

// summarizeArgs keeps the arguments readable without dumping a whole file into
// the log.
func summarizeArgs(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	s := string(b)
	if len(s) > 220 {
		s = s[:220] + "…"
	}
	return s
}

// videoOffset returns mm:ss within the running recording, or "".
func videoOffset(rec *media.Recorder) string {
	if rec == nil {
		return ""
	}
	st := rec.Status()
	active, _ := st["recording"].(bool)
	if !active {
		return ""
	}
	secs, _ := st["seconds"].(int)
	return fmt.Sprintf("%02d:%02d", secs/60, secs%60)
}

func nowStamp() string { return time.Now().Format("2006-01-02T15:04:05.000Z07:00") }
