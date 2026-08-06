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

// The tool registry: what each tool is, what it can do to the machine, and how
// an agent finds the handful it needs among a hundred and fifteen.
//
// Two problems are solved here, and they turn out to be the same problem.
//
// The first is classification. Risk used to live in two flat maps at the top of
// mcppolicy.go, three hundred lines away from the tools they described, and
// nothing connected one to the other. A tool missing from both maps was refused
// under MCP_POLICY=readonly and permitted under MCP_POLICY=safe — quietly, with
// no way to notice. By the time the maps were checked against the catalogue,
// forty-six of the hundred and fourteen tools had drifted into that gap: not
// only harmless ones like shell_read and room_state being refused, but
// terminal_run and terminal_open — which type a command line into a shell and
// press Return — being allowed under the level whose entire promise is that it
// does not run code. Risk now lives on the toolDef, beside the schema, and the
// maps are derived from it. The failure it replaces was silent; this one is a
// startup error.
//
// The second is discovery. A hundred and fifteen schemas is a large fraction of
// a model's context spent before it reads the request, and most hosts pay it on
// every turn. Some of them already defer tool loading and search on demand;
// where the host does not, MCP_DISCOVERY=1 does it from this side — tools/list
// answers with a small core plus tool_search, and everything else stays
// callable by name. That asymmetry is the whole design: discovery narrows what
// is *advertised*, never what is *permitted*. Policy remains the only thing
// that can refuse a call, so a hidden tool is one the model has not been told
// about yet, not one it is forbidden to use.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// --- risk --------------------------------------------------------------------

// riskLevel is what a tool can do to the machine, and the only input the three
// MCP_POLICY levels need.
type riskLevel int

const (
	// riskUnset is the zero value and never a valid answer. That is the point:
	// a toolDef written without a Risk fails at startup instead of inheriting
	// whatever the surrounding map happened to say.
	riskUnset riskLevel = iota

	// riskRead observes and changes nothing. These are the only tools that
	// survive MCP_POLICY=readonly.
	riskRead

	// riskWrite drives the desktop — input, windows, volume, the clipboard. It
	// changes what is on screen, which is what an agent is for, but it cannot
	// reach past the desktop to the system underneath.
	riskWrite

	// riskDanger runs code, touches the system, or moves data outward. These
	// are what MCP_POLICY=safe removes.
	riskDanger
)

func (r riskLevel) String() string {
	switch r {
	case riskRead:
		return "read"
	case riskWrite:
		return "write"
	case riskDanger:
		return "danger"
	}
	return "unclassified"
}

// --- categories --------------------------------------------------------------

// categoryRules maps a tool to a theme. The order matters: the first rule that
// matches wins, so the specific entries have to come before the general ones —
// window_properties is a window tool, not a properties tool.
//
// Categories exist for tool_search, which weighs a match on the category above
// one in the description: someone asking about "ssh" wants the thirteen ssh_*
// tools before every tool whose description happens to mention a remote host.
var categoryRules = []struct {
	match    func(string) bool
	category string
}{
	{func(n string) bool { return strings.HasPrefix(n, "ssh_") }, "ssh"},
	{func(n string) bool { return strings.HasPrefix(n, "shell_") }, "shell"},
	{func(n string) bool { return strings.HasPrefix(n, "terminal_") || n == "check_errors" }, "terminal"},
	{func(n string) bool { return strings.HasPrefix(n, "browser_") }, "browser"},
	{func(n string) bool { return strings.HasPrefix(n, "ui_") || n == "fill_form" }, "accessibility"},
	{func(n string) bool { return strings.HasPrefix(n, "gamepad_") }, "gamepad"},
	{func(n string) bool { return strings.HasPrefix(n, "snapshot_") }, "snapshot"},
	{func(n string) bool { return strings.HasPrefix(n, "mouse_") }, "input"},
	{func(n string) bool { return n == "type_text" || n == "key_combo" }, "input"},
	{func(n string) bool { return strings.Contains(n, "recording") }, "recording"},
	{func(n string) bool { return strings.Contains(n, "restream") }, "restream"},
	{func(n string) bool { return strings.Contains(n, "clipboard") }, "clipboard"},
	{func(n string) bool { return strings.Contains(n, "desktop") }, "desktops"},
	{func(n string) bool { return strings.Contains(n, "window") }, "windows"},
	{func(n string) bool { return strings.Contains(n, "packages") }, "packages"},
	{func(n string) bool {
		switch n {
		case "read_file", "write_file", "list_directory":
			return true
		}
		return false
	}, "files"},
	{func(n string) bool {
		switch n {
		case "get_audio_state", "set_volume":
			return true
		}
		return false
	}, "audio"},
	{func(n string) bool {
		switch n {
		case "list_processes", "kill_process", "is_running", "list_installed_apps",
			"launch_app", "run_command", "open_app_and_wait":
			return true
		}
		return false
	}, "processes"},
	{func(n string) bool {
		switch n {
		case "screenshot", "screenshot_region", "get_screen_info", "get_pixel_color",
			"read_screen_text", "find_text", "set_resolution":
			return true
		}
		return false
	}, "screen"},
	{func(n string) bool {
		switch n {
		case "room_state", "request_control", "release_control":
			return true
		}
		return false
	}, "room"},
	{func(n string) bool {
		switch n {
		case "sudo_status", "service_control":
			return true
		}
		return false
	}, "system"},
}

func categoryOf(name string) string {
	for _, r := range categoryRules {
		if r.match(name) {
			return r.category
		}
	}
	return "general"
}

// --- annotations ---------------------------------------------------------------

// annotations translates the risk level into the hints the MCP specification
// defines, so a host that understands them can shape its own permission prompt
// without knowing anything about MCP_POLICY. It costs nothing to publish and it
// is the standard place for exactly this, which is a better argument than any
// custom field would have been.
func (t toolDef) annotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    t.Risk == riskRead,
		"destructiveHint": t.Risk == riskDanger,

		// Not in the specification, and namespaced so it cannot collide with
		// something that later is. It answers a question no standard hint does
		// and that a client cannot work out for itself: will this call be held
		// at the room gate until the agent holds the controls?
		//
		// Risk is no substitute. ui_click is write and gated, set_volume is
		// write and not; start_restream is danger and gated, write_file is
		// danger and not. Without this published, a client that wants to ask
		// for control at the right moment has to carry its own copy of the
		// list — which is the drift this whole file exists to end.
		"sentineldesk/requiresControl": t.RequiresControl,
	}
}

// MarshalJSON writes the wire form: the three fields the specification requires
// plus the annotations derived above. Risk itself stays out — it is the input
// to the hints, not a second copy of them.
func (t toolDef) MarshalJSON() ([]byte, error) {
	type wire toolDef // a distinct type, so this method is not called again
	return json.Marshal(struct {
		wire
		Annotations map[string]any `json:"annotations"`
	}{wire(t), t.annotations()})
}

// --- validation ----------------------------------------------------------------

// validateCatalogue reports every tool that was defined without a risk level.
//
// Adding a tool means writing a toolDef and a dispatch case; before this, it
// also meant remembering two maps in another file, and forgetting them failed
// in the direction of granting access. Now the catalogue is the single source
// and the check runs on the way up, so the mistake costs a startup message
// rather than a permission nobody meant to give.
func validateCatalogue(tools []toolDef) error {
	var unclassified []string
	seen := map[string]bool{}
	var dupes []string
	for _, t := range tools {
		if t.Risk == riskUnset {
			unclassified = append(unclassified, t.Name)
		}
		if seen[t.Name] {
			dupes = append(dupes, t.Name)
		}
		seen[t.Name] = true
	}
	var problems []string
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		problems = append(problems, fmt.Sprintf(
			"%d tool(s) with no Risk: %s — add riskRead, riskWrite or riskDanger to the toolDef",
			len(unclassified), strings.Join(unclassified, ", ")))
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		problems = append(problems, "duplicate tool name(s): "+strings.Join(dupes, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("mcp catalogue: %s", strings.Join(problems, "; "))
	}
	return nil
}

// riskIndex is the catalogue keyed by name, built once at startup. The policy
// consults it on every call, which is why it is a map and not a scan.
type riskIndex map[string]riskLevel

func buildRiskIndex(tools []toolDef) riskIndex {
	idx := make(riskIndex, len(tools))
	for _, t := range tools {
		idx[t.Name] = t.Risk
	}
	return idx
}

// controlIndex is the set of tools the room gates, derived from the catalogue
// the same way the risk maps are. It replaced a switch statement in mcp.go for
// the same reason the risk maps went: a list of names kept apart from the tools
// it describes is a list that stops describing them.
type controlIndex map[string]bool

func buildControlIndex(tools []toolDef) controlIndex {
	idx := make(controlIndex)
	for _, t := range tools {
		if t.RequiresControl {
			idx[t.Name] = true
		}
	}
	return idx
}

// --- denial kinds ----------------------------------------------------------------

// denialKind says why a tools/call did not succeed, in a form a program can
// branch on.
//
// The sentence a caller gets back is written for a model to read, and it is
// rewritten whenever a better sentence is found — this repository does that
// routinely. A client that has to tell a policy refusal from a room refusal by
// matching substrings is therefore one wording change away from breaking, and
// the three cases need genuinely different responses: a policy refusal is
// final and the model should be told the capability is not available; a room
// refusal means ask a person and try again; a tool failure may be worth
// retrying. Guessing wrong turns "wait your turn" into "give up".
//
// So the reason travels twice: the prose in the content, unchanged, and this
// alongside it.
type denialKind string

const (
	// denialPolicy — MCP_POLICY, MCP_DENY or MCP_ALLOW refused the call. Final:
	// nothing the caller does will change it within this connection.
	denialPolicy denialKind = "policy"

	// denialRoom — the tool needs the desktop's controls and the agent does not
	// hold them. Not final: call request_control, or wait for whoever is
	// driving to finish.
	denialRoom denialKind = "room"

	// denialUnknown — no such tool in the catalogue.
	denialUnknown denialKind = "unknown_tool"

	// denialToolError — the tool ran and reported failure. This is the residual
	// case and it is deliberately coarse: tools validate their own arguments
	// and report in prose, so an invalid_arguments kind would mean touching all
	// of them. Worth splitting out when something needs it, not before.
	denialToolError denialKind = "tool_error"
)

// toolCallResult builds the tools/call result. An empty kind means success.
//
// _meta is where the specification puts extension data on a result, and the key
// is namespaced for the same reason the requiresControl annotation is: this is
// ours, and it should not collide with a field the protocol may later define.
func toolCallResult(content []map[string]any, kind denialKind) map[string]any {
	res := map[string]any{"content": content, "isError": kind != ""}
	if kind != "" {
		res["_meta"] = map[string]any{"sentineldesk/denial": string(kind)}
	}
	return res
}

// nameIndex is the set of tool names the catalogue defines, so that a call for
// something that does not exist can be told apart from one that was refused.
type nameIndex map[string]bool

func buildNameIndex(tools []toolDef) nameIndex {
	idx := make(nameIndex, len(tools))
	for _, t := range tools {
		idx[t.Name] = true
	}
	return idx
}

// --- discovery -----------------------------------------------------------------

// coreTools is what tools/list advertises when MCP_DISCOVERY is on: enough to
// look at the desktop, read its structure, click, type and run something — plus
// the one tool that finds the rest. An agent that never calls tool_search can
// still do useful work with only these; the point is that it no longer pays for
// a hundred and fifteen schemas to find out whether it needs ssh_tunnel_remote.
var coreTools = map[string]bool{
	"tool_search":  true,
	"screenshot":   true,
	"ui_tree":      true,
	"ui_find":      true,
	"ui_click":     true,
	"mouse_click":  true,
	"type_text":    true,
	"key_combo":    true,
	"list_windows": true,
	"run_command":  true,
	"wait":         true,
	"room_state":   true,
}

// discoveryEnabled reports whether tools/list should be trimmed to the core.
//
// Off by default, and deliberately so: a host that already defers tool loading
// does this better than the server can, because it sees the conversation. This
// is for the hosts that do not.
func discoveryEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MCP_DISCOVERY"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// listedTools is what tools/list answers with: the connection's policy applied
// always, and the core-set narrowing applied only when discovery is on.
//
// The two filters are not the same kind of thing and must not be confused. The
// policy one is a permission and is enforced a second time in handleToolCall;
// this one is only about what gets mentioned, and nothing enforces it anywhere.
// A tool that discovery leaves out is still callable the moment the model knows
// its name — which is the entire mechanism, not a hole in it.
func (s *Server) listedTools(policy *Policy) []toolDef {
	allowed := policy.Filter(s.tools)
	if !s.discovery {
		return allowed
	}
	out := make([]toolDef, 0, len(coreTools))
	for _, t := range allowed {
		if coreTools[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// --- search --------------------------------------------------------------------

// searchStopwords are the words a plain-English request is made of. Left in,
// they dominate the result: every long description contains "the" and "a", so
// the tools with the most prose win regardless of what was asked. Dropping them
// is the difference between "record a video of the desktop" returning
// start_recording and returning whichever tool has the wordiest description.
var searchStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "can": true, "do": true, "for": true, "from": true,
	"get": true, "how": true, "i": true, "in": true, "is": true, "it": true,
	"me": true, "my": true, "of": true, "on": true, "or": true, "so": true,
	"some": true, "someone": true, "something": true, "that": true, "the": true,
	"then": true, "there": true, "this": true, "to": true, "up": true,
	"want": true, "was": true, "what": true, "when": true, "which": true,
	"with": true, "you": true, "your": true,
}

// categoryAliases are the words people use for a theme that are not the theme's
// name. Without them a query has to already contain the answer: "give someone
// remote access" describes the ssh tools exactly and shares not one character
// with the string "ssh".
//
// This is a small hand-written vocabulary rather than anything learned, and it
// is meant to stay small. It earns its place by covering the gap between how a
// task is described and how the tool that does it was named.
var categoryAliases = map[string][]string{
	"ssh":           {"remote", "sftp", "scp", "tunnel", "port", "forward", "server", "host", "login"},
	"shell":         {"bash", "command", "session", "console", "sh"},
	"terminal":      {"console", "command", "cli", "prompt", "xterm"},
	"browser":       {"chrome", "chromium", "web", "page", "url", "dom", "tab", "site"},
	"accessibility": {"a11y", "atspi", "widget", "button", "label", "element", "form", "field"},
	"windows":       {"window", "app", "application", "focus", "raise", "geometry"},
	"input":         {"keyboard", "mouse", "click", "type", "press", "key", "scroll", "drag"},
	"screen":        {"display", "pixel", "ocr", "capture", "screenshot", "resolution", "text"},
	"files":         {"file", "directory", "folder", "path", "read", "write", "download", "upload"},
	"processes":     {"process", "program", "pid", "launch", "start", "run", "kill", "app"},
	"packages":      {"apt", "install", "package", "software", "dependency"},
	"recording":     {"record", "video", "capture", "mp4", "film"},
	"restream":      {"rtmp", "stream", "broadcast", "publish", "youtube", "twitch"},
	"room":          {"control", "session", "participant", "viewer", "share", "turn"},
	"audio":         {"sound", "volume", "mute", "speaker", "mic"},
	"clipboard":     {"copy", "paste", "cut"},
	"desktops":      {"workspace", "desktop", "virtual"},
	"gamepad":       {"joystick", "controller", "button", "axis"},
	"snapshot":      {"backup", "restore", "checkpoint", "rollback"},
	"system":        {"service", "systemd", "sudo", "root", "privilege", "daemon"},
}

// categoryMatches reports whether a query term points at a category, either by
// naming it or through one of its aliases.
func categoryMatches(category, term string) bool {
	if strings.Contains(category, term) {
		return true
	}
	for _, alias := range categoryAliases[category] {
		if alias == term || strings.HasPrefix(alias, term) && len(term) >= 4 {
			return true
		}
	}
	return false
}

type searchHit struct {
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Risk        string          `json:"risk"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	score       int
}

// searchTools ranks the catalogue against a free-text query.
//
// The scoring is deliberately dumb — substring matching over name, category and
// description, weighted in that order. Anything cleverer (embeddings, a real
// index) would need a model or a dependency to answer a question that a handful
// of keywords already answers well, on a corpus of a hundred and fifteen short
// strings that fits in a cache line's worth of cache misses. A hit on the name
// outranks the category, which outranks the description, because a tool called
// ssh_exec is a better answer to "ssh" than one that mentions ssh in passing.
func searchTools(tools []toolDef, query string, limit int) []searchHit {
	var terms []string
	for _, term := range strings.Fields(strings.ToLower(query)) {
		term = strings.Trim(term, ".,;:!?\"'()")
		if len(term) < 2 || searchStopwords[term] {
			continue
		}
		terms = append(terms, term)
	}
	if limit <= 0 {
		limit = 10
	}
	var hits []searchHit
	for _, t := range tools {
		// tool_search does not answer questions about the desktop, and a model
		// running it already has it. Leaving it in means every search returns
		// itself, which is a wasted slot in a list of ten.
		if t.Name == "tool_search" {
			continue
		}
		name := strings.ToLower(t.Name)
		spaced := strings.ReplaceAll(name, "_", " ")
		cat := categoryOf(t.Name)
		desc := strings.ToLower(t.Description)
		score, strong := 0, 0
		for _, term := range terms {
			// Underscores are separators, not letters: "remote access" should
			// find ssh_list_remote, and it will not if the term has to survive
			// as a contiguous substring of the whole name.
			hit := 0
			switch {
			case name == term:
				hit = 12
			case strings.Contains(spaced, term):
				hit = 8
			case strings.Contains(name, term):
				hit = 6
			}
			if categoryMatches(cat, term) {
				hit += 4
			}
			// A name or category hit is evidence about the tool. A description
			// hit is evidence about its prose, so it scores but does not count
			// towards the all-terms bonus below — otherwise the tools with the
			// longest descriptions win every vague query.
			if hit > 0 {
				strong++
			}
			if strings.Contains(desc, term) {
				hit++
			}
			score += hit
		}
		if strong == 0 {
			continue
		}
		// Matching several terms beats matching one of them well: a query is a
		// description of one tool, not a bag of alternatives.
		score += strong * 3
		hits = append(hits, searchHit{
			Name: t.Name, Category: cat, Risk: t.Risk.String(),
			Description: t.Description, InputSchema: t.InputSchema, score: score,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].Name < hits[j].Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// --- the tool itself -------------------------------------------------------------

func (s *Server) buildRegistryTools() []toolDef {
	return []toolDef{
		{
			Name: "tool_search",
			Description: "Find the tools for a task without loading all of them. " +
				"Give it a plain description of what you want to do — 'give someone " +
				"remote access', 'read the text on screen', 'open a tunnel' — and it " +
				"returns the best matching tools with their full input schemas, so " +
				"you can call them straight away. Every tool is callable by name " +
				"whether or not tools/list advertised it; this is how you learn the " +
				"names. Pass category to list a whole theme (ssh, browser, shell, " +
				"terminal, accessibility, windows, input, screen, files, processes, " +
				"packages, snapshot, recording, restream, room, audio, clipboard, " +
				"desktops, gamepad, system).",
			Risk: riskRead,
			InputSchema: schema(map[string]any{
				"query":    pStr("what you are trying to do, in words"),
				"category": pStr("optional: restrict to one theme"),
				"limit":    pInt("how many to return (default 10)"),
			}),
		},
	}
}

// dispatchRegistry takes the connection's policy rather than reading one off
// the Server, because requests from different connections run concurrently and
// each may have restricted itself differently. Threading it through is a wider
// signature; the alternative was shared mutable state and a race.
func (s *Server) dispatchRegistry(name string, args map[string]any, policy *Policy) ([]map[string]any, bool, bool) {
	if name != "tool_search" {
		return nil, false, false
	}
	query := argStr(args, "query")
	category := strings.ToLower(strings.TrimSpace(argStr(args, "category")))
	limit := argInt(args, "limit")

	// The catalogue is filtered by the connection's policy before it is
	// searched. Turning up a tool the connection may never call would be a
	// worse answer than turning up nothing.
	pool := s.tools
	if policy != nil {
		pool = policy.Filter(pool)
	}
	if category != "" {
		var narrowed []toolDef
		for _, t := range pool {
			if categoryOf(t.Name) == category {
				narrowed = append(narrowed, t)
			}
		}
		pool = narrowed
		// A category on its own is a valid request: list the theme.
		if strings.TrimSpace(query) == "" {
			if limit <= 0 {
				limit = len(pool)
			}
			query = category
		}
	}
	if strings.TrimSpace(query) == "" && category == "" {
		return textContent("`query` is missing: describe what you are trying to do"), true, true
	}

	hits := searchTools(pool, query, limit)
	if len(hits) == 0 {
		return textContent(
			"nothing matched %q. Categories: ssh, browser, shell, terminal, "+
				"accessibility, windows, input, screen, files, processes, packages, "+
				"snapshot, recording, restream, room, audio, clipboard, desktops, "+
				"gamepad, system, general.", query), false, true
	}
	return jsonContent(map[string]any{
		"matched": len(hits),
		"of":      len(pool),
		"tools":   hits,
	}), false, true
}
