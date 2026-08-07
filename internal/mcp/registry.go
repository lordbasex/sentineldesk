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
// an agent finds the handful it needs among a hundred and nineteen.
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
// The second is discovery. A hundred and nineteen schemas is a large fraction of
// a model's context spent before it reads the request, and most hosts pay it on
// every turn. Some of them already defer tool loading and search on demand;
// where the host does not, MCP_DISCOVERY=1 does it from this side — tools/list
// answers with a small core plus tool_search, and everything else stays
// callable by name. That asymmetry is the whole design: discovery narrows what
// is *advertised*, never what is *permitted*. Policy remains the only thing
// that can refuse a call, so a hidden tool is one the model has not been told
// about yet, not one it is forbidden to use.

import (
	"context"
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

// --- visibility ----------------------------------------------------------------

// visibility is whether a person sharing the desktop sees a tool act.
type visibility int

const (
	// visUnset is the zero value and, for a tool that changes anything, never
	// a valid answer — same reasoning as riskUnset. The difference is that
	// here there IS a correct default for one class of tool, and it is proved
	// rather than assumed: see visHidden.
	visUnset visibility = iota

	// visHidden changes state and puts nothing on the screen. run_command,
	// install_packages, write_file, the ssh_* and shell_* families.
	//
	// Every riskRead tool is also this, by construction rather than by
	// declaration: a tool that changes nothing cannot be seen changing
	// something.
	visHidden

	// visVisible changes what is on the screen without injecting input:
	// launching an application, moving a window, driving the browser through
	// DevTools. A person watching sees the result appear; they do not see it
	// being done.
	visVisible

	// visInjects drives the desktop the way a person would — pointer, keyboard,
	// gamepad, or text through the accessibility layer. A person watching sees
	// the pointer move and the characters arrive. These are exactly the tools
	// that must hold the room's controls first, and validateCatalogue enforces
	// that: a tool claiming to inject without RequiresControl would be typing
	// into somebody else's session.
	visInjects
)

func (v visibility) String() string {
	switch v {
	case visHidden:
		return "hidden"
	case visVisible:
		return "visible"
	case visInjects:
		return "injects"
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

		// Whether a person sharing the desktop sees this happen: hidden,
		// visible or injects. Also not in the specification, and also not
		// derivable by a client — requiresControl looks like the same question
		// and is not. A runtime asked for a demonstration reads this to choose
		// terminal_run over run_command; without it published, that choice can
		// only be made by a table the client carries and the server contradicts.
		"sentineldesk/visibility": t.effectiveVisibility().String(),
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
	var unseen, readNotHidden, injectsUngated []string
	for _, t := range tools {
		if t.Risk == riskUnset {
			unclassified = append(unclassified, t.Name)
		}
		switch {
		case t.Risk == riskRead:
			// Hidden by construction, so declaring anything else is a
			// contradiction rather than a preference. Declaring visHidden
			// explicitly is allowed and redundant; declaring visible or
			// injects means one of the two fields is wrong.
			if t.Visibility != visUnset && t.Visibility != visHidden {
				readNotHidden = append(readNotHidden, t.Name)
			}
		case t.Visibility == visUnset:
			unseen = append(unseen, t.Name)
		}
		if t.Visibility == visInjects && !t.RequiresControl {
			injectsUngated = append(injectsUngated, t.Name)
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
	if len(unseen) > 0 {
		sort.Strings(unseen)
		problems = append(problems, fmt.Sprintf(
			"%d tool(s) that change something with no Visibility: %s — "+
				"add visHidden, visVisible or visInjects to the toolDef",
			len(unseen), strings.Join(unseen, ", ")))
	}
	if len(readNotHidden) > 0 {
		sort.Strings(readNotHidden)
		problems = append(problems, fmt.Sprintf(
			"read-only tool(s) declaring they can be seen: %s — a tool that "+
				"changes nothing cannot be visible; one of Risk or Visibility is wrong",
			strings.Join(readNotHidden, ", ")))
	}
	if len(injectsUngated) > 0 {
		sort.Strings(injectsUngated)
		problems = append(problems, fmt.Sprintf(
			"tool(s) that inject input without RequiresControl: %s — "+
				"they would be typing into somebody else's session",
			strings.Join(injectsUngated, ", ")))
	}
	// A renamed tool must not leave its search vocabulary behind. The stranded
	// entry would never match anything and nothing would ever say so — the tool
	// would simply get harder to find, which is the failure mode this whole file
	// was written to stop being silent.
	var stranded []string
	for name := range toolKeywords {
		if !seen[name] {
			stranded = append(stranded, name)
		}
	}
	if len(stranded) > 0 {
		sort.Strings(stranded)
		problems = append(problems, fmt.Sprintf(
			"toolKeywords names %d tool(s) that are not in the catalogue: %s",
			len(stranded), strings.Join(stranded, ", ")))
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
	// and report in prose, so a per-tool argument kind would mean touching all
	// of them.
	denialToolError denialKind = "tool_error"

	// denialBadArgs — an argument was sent that the tool does not have.
	//
	// This is the case the note above said to split out when something needed
	// it. Something did. Every tool ignored arguments it did not recognise, so
	// a caller that misremembered a name got no signal at all: ui_tree was
	// called with max_depth instead of depth three times, returned the full
	// tree each time, and read as a tool whose depth setting did nothing.
	// Reproduced on tools with nothing in common — wait accepted
	// totally_made_up_parameter, screenshot accepted nonsense_arg — so it was
	// never a tool's oversight but the absence of a check.
	//
	// It matters more for a model than for a person. A person notices the
	// output did not change; a model has only the reply, and a reply that
	// reports success for a call that quietly did something else is
	// indistinguishable from one that did what was asked.
	denialBadArgs denialKind = "bad_arguments"

	// denialCancelled — the call was stopped, by notifications/cancelled or by
	// the connection closing.
	//
	// This exists because without it a cancelled call reports success. A killed
	// process is just a process with a non-zero exit status, so run_command
	// answered {"exit_code": -1} and no error at all — true about the process
	// and a lie about the request. A client reading that would tell the model
	// its command ran and failed, when in fact the client itself had stopped
	// it. Whatever the tool managed to return describes work that was
	// interrupted, not work that was done.
	denialCancelled denialKind = "cancelled"

	// denialEmergency — this connection has been halted. Distinct from policy
	// because it is about who is calling rather than what they called, and it
	// is lifted by an operator rather than by asking differently.
	denialEmergency denialKind = "emergency"
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

// argIndex is the set of argument names each tool declares.
type argIndex map[string]map[string]bool

// buildArgIndex reads the property names out of every tool's input schema.
//
// The schemas were already being published to clients and then never consulted
// again, which is how an argument no tool has could be accepted by all of them.
// Indexing at startup means the check costs a map lookup per call, and means a
// tool cannot forget it — the same reasoning that moved risk and the room gate
// onto toolDef.
func buildArgIndex(tools []toolDef) argIndex {
	idx := make(argIndex, len(tools))
	for _, t := range tools {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		// A tool with no properties gets an empty set, which correctly refuses
		// every argument rather than accepting any.
		_ = json.Unmarshal(t.InputSchema, &schema)
		names := make(map[string]bool, len(schema.Properties))
		for name := range schema.Properties {
			names[name] = true
		}
		idx[t.Name] = names
	}
	return idx
}

// unknownArgs returns the argument names this tool does not declare, sorted so
// the message is the same every time it is produced.
func (idx argIndex) unknownArgs(tool string, args map[string]any) []string {
	known, ok := idx[tool]
	if !ok {
		return nil
	}
	var bad []string
	for name := range args {
		// _meta is the specification's own extension slot and belongs to the
		// protocol rather than to the tool, so it is never a tool's argument
		// and never a mistake.
		if name == "_meta" || known[name] {
			continue
		}
		bad = append(bad, name)
	}
	sort.Strings(bad)
	return bad
}

// declared lists the argument names a tool does take, for the refusal message.
func (idx argIndex) declared(tool string) []string {
	names := make([]string, 0, len(idx[tool]))
	for name := range idx[tool] {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// --- discovery -----------------------------------------------------------------

// coreTools is what tools/list advertises when MCP_DISCOVERY is on: enough to
// look at the desktop, read its structure, click, type and run something — plus
// the one tool that finds the rest. An agent that never calls tool_search can
// still do useful work with only these; the point is that it no longer pays for
// a hundred and nineteen schemas to find out whether it needs ssh_tunnel_remote.
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
		// Both directions. The prefix test used to run only one way, so a query
		// saying "application" could not reach the alias "app" — which is half
		// of why "open the calculator application" never found launch_app.
		if alias == term ||
			(strings.HasPrefix(alias, term) && len(term) >= 4) ||
			(strings.HasPrefix(term, alias) && len(alias) >= 4) {
			return true
		}
	}
	return false
}

// toolKeywords is the vocabulary that connects a task to the tool that does it.
//
// Categories were not enough. A category alias helps a query that already names
// the theme — "remote access" reaches the ssh_* family — but it cannot choose
// between thirteen tools inside that family, and it does nothing at all for the
// tools whose theme is obvious and whose *name* is the obstacle. "open the
// calculator application" is a request for launch_app, and before this map it
// returned browser_open, terminal_open, shell_open and open_app_and_wait,
// because those four have the query's only distinctive word in their names and
// launch_app does not have it anywhere.
//
// Three kinds of entry earn their place here, and nothing else should:
//
//   - the word for the thing that is not the word in the name — "uninstall" for
//     remove_packages, "checkpoint" for the snapshot family, "pause" for wait;
//   - the spelling the writer did not use — "color" beside "colour", "a11y"
//     beside "accessibility";
//   - the multi-word phrase that means the tool and nothing else — "port
//     forward", "always on top", "bring to front". These are matched against the
//     whole query rather than term by term, which is what makes them worth more
//     than their words separately: "forward" alone is ambiguous between the two
//     tunnel directions, "port forward" is not.
//
// What does not belong here is a word already in the tool's name or description.
// It buys nothing — those are searched — and it makes the map look authoritative
// when it is only supplementary. A tool absent from this map is not misfiled;
// it is a tool whose name says what it does, which is most of them.
//
// Every key is checked against the catalogue at startup, so a renamed tool
// cannot leave its vocabulary behind. Whether the vocabulary is *sufficient* is
// a different question, and the only honest answer to it is a measurement:
// search_test.go holds one plain-English query per tool and fails when recall
// drops. Add a tool, add its query; if it ranks, it needs nothing here.
var toolKeywords = map[string][]string{
	// Processes — the family the naming hurts most, because "open", "start" and
	// "run" are what a person says and three other families own those words.
	"launch_app":          {"open", "start", "application", "program", "app", "open the"},
	"open_app_and_wait":   {"open and wait", "start and wait", "launch and wait", "until", "and wait"},
	"run_command":         {"execute", "shell", "one-off", "command line"},
	"list_processes":      {"running", "tasks", "what is running", "processes"},
	"is_running":          {"already open", "is it open", "still running", "alive"},
	"kill_process":        {"stop", "terminate", "quit", "frozen", "hung", "force quit"},
	"list_installed_apps": {"installed", "available applications", "what applications"},
	"list_commands":       {"binaries", "executables", "programs", "what can i run", "path"},

	// Screen.
	"screenshot":        {"picture", "capture", "image", "look", "see"},
	"screenshot_region": {"crop", "rectangle", "part of the screen", "area"},
	"get_screen_info":   {"resolution", "size", "dimensions", "how big"},
	"get_pixel_color":   {"color", "colour", "rgb", "pixel", "dot", "shade"},
	"read_screen_text":  {"ocr", "what does it say", "read the screen"},
	"find_text":         {"locate", "where does it say", "where is the word", "search the screen"},
	"set_resolution":    {"change resolution", "resize the display", "1920", "1280"},

	// Windows.
	"activate_window":   {"front", "foreground", "bring to front", "switch to", "raise"},
	"get_active_window": {"which window", "has focus", "current window", "frontmost"},
	"move_window":       {"position", "corner", "place", "put the window", "reposition"},
	"resize_window":     {"narrower", "wider", "taller", "shorter", "dimensions"},
	"minimize_window":   {"hide", "out of the way", "iconify", "taskbar"},
	"maximize_window":   {"as big as", "bigger", "fill the screen", "enlarge"},
	"restore_window":    {"unmaximize", "back to", "previous size", "undo maximize"},
	"fullscreen_window": {"full screen", "entire display", "whole screen"},
	"window_properties": {"details", "attributes", "geometry", "about that window"},
	"window_hierarchy":  {"parent", "child", "tree of windows", "nesting"},
	"window_set_state":  {"always on top", "above the others", "sticky", "shaded", "keep above"},
	"wait_for_window":   {"until the window", "window to open", "window to appear"},

	// Desktops.
	"list_desktops":      {"workspaces", "how many workspaces", "virtual desktops"},
	"get_desktop_info":   {"which workspace", "current workspace", "am i on"},
	"switch_desktop":     {"go to workspace", "next workspace", "change workspace"},
	"set_window_desktop": {"send to workspace", "move to workspace", "another workspace"},

	// The room.
	"room_state":      {"who", "connected", "participants", "viewers", "people", "others", "sharing"},
	"request_control": {"take the controls", "claim", "grab", "acquire", "may i"},
	"release_control": {"give back", "hand back", "relinquish", "let go of the controls", "done"},

	// Accessibility.
	"ui_tree":     {"structure", "hierarchy", "widgets", "layout", "what is in the app"},
	"ui_find":     {"locate the button", "search box", "which element", "find the field"},
	"ui_at_point": {"what is at", "under the pointer", "at these coordinates", "what is there", "identify", "under the mouse"},
	"ui_click":    {"press the button", "activate the element", "push"},
	"ui_focus":    {"cursor", "caret", "put the cursor", "select the field"},
	"ui_get_text": {"read the field", "contents of the field", "what does it hold"},
	"ui_set_text": {"write into", "put text in", "enter into the field"},
	"ui_diff":     {"changed", "difference", "since i last", "what is new"},
	"ui_wait_for": {"until it appears", "dialog to appear", "element to appear"},
	"fill_form":   {"complete the form", "several fields", "fill in"},

	// Terminal and shell — two families a query cannot tell apart on names.
	"terminal_open": {"terminal window", "xterm", "console window"},
	"terminal_run":  {"into the terminal", "at the prompt", "in the console"},
	"terminal_read": {"terminal output", "what the terminal", "console output"},
	"shell_open":    {"background session", "persistent", "keep using", "long running"},
	"shell_exec":    {"in the session", "same session", "persistent command"},
	"shell_input":   {"send a line", "answer the prompt", "stdin", "waiting for input"},
	"shell_read":    {"session output", "what the session", "printed"},
	"shell_list":    {"open sessions", "my sessions", "which sessions"},
	"shell_close":   {"end the session", "finish the session"},
	"check_errors":  {"fail", "failed", "failure", "problem", "wrong", "broken", "crash", "went wrong"},

	// Browser.
	"browser_open":     {"website", "web site", "web page", "url"},
	"browser_goto":     {"navigate", "different address", "another page", "go to"},
	"browser_text":     {"page contents", "what the page says", "read the page"},
	"browser_type":     {"text box", "input field", "into the website", "on the site"},
	"browser_click":    {"press on the page", "button on the page", "link"},
	"browser_eval":     {"javascript", "js", "script in the page", "evaluate"},
	"browser_tabs":     {"open pages", "which tabs", "what is open in the browser"},
	"browser_wait_for": {"until the element", "element to appear", "page to show"},

	// Files.
	"read_file":      {"contents of the file", "cat", "show the file"},
	"write_file":     {"save to a file", "create a file", "put in a file"},
	"list_directory": {"folder", "what is inside", "ls", "contents of the directory"},

	// Packages.
	"install_packages": {"apt install", "add software", "get the package"},
	"remove_packages":  {"uninstall", "purge", "get rid of the package", "delete the package"},
	"search_packages":  {"is there a package", "look for software", "find a package"},

	// Snapshots.
	"snapshot_create":  {"checkpoint", "save the state", "come back to", "backup"},
	"snapshot_list":    {"checkpoints", "what backups", "saved states"},
	"snapshot_restore": {"roll back", "revert", "go back to", "undo everything"},
	"snapshot_delete":  {"throw away the checkpoint", "remove the backup"},

	// Recording and restreaming.
	"start_recording":      {"film", "capture video", "make a video", "record"},
	"stop_recording":       {"stop the video", "finish recording", "end the recording"},
	"get_recording_status": {"still recording", "am i recording", "is it recording"},
	"list_recordings":      {"videos", "what have i recorded", "past recordings"},
	"start_restream":       {"youtube", "twitch", "go live", "broadcast"},
	"stop_restream":        {"stop the broadcast", "go offline", "end the stream"},
	"list_restreams":       {"broadcasts", "what is live", "active streams"},

	// SSH — thirteen tools whose names differ by one word, so the phrases matter
	// more here than anywhere else.
	"ssh_connect":       {"log in to", "sign in to", "open a connection", "reach the machine"},
	"ssh_disconnect":    {"close the connection", "log out", "drop the connection"},
	"ssh_list":          {"which hosts", "my connections", "connected to"},
	"ssh_exec":          {"on the remote", "on that machine", "over ssh"},
	"ssh_upload":        {"send the file", "copy to the server", "put the file"},
	"ssh_download":      {"fetch the file", "copy from the server", "get the file"},
	"ssh_list_remote":   {"files on the remote", "directory on the server", "what is on the server"},
	"ssh_keygen":        {"key pair", "private key", "public key", "make a key"},
	"ssh_copy_id":       {"passwordless", "install the key", "trust the key", "without a password"},
	"ssh_tunnel_local":  {"port forward", "forward a local port", "reach a remote service"},
	"ssh_tunnel_remote": {"reverse tunnel", "expose locally", "publish my service"},
	"ssh_tunnels":       {"open forwards", "which tunnels", "active tunnels", "forwards"},
	"ssh_tunnel_close":  {"close the tunnel", "shut down", "stop forwarding", "tear down"},

	// Input.
	"mouse_click":        {"click at", "coordinates", "click there"},
	"mouse_move":         {"pointer to", "move the cursor", "hover"},
	"mouse_down":         {"hold the button", "press and hold", "begin the drag"},
	"mouse_up":           {"let go", "release the button", "end the drag"},
	"mouse_drag":         {"drag and drop", "drag onto", "move it onto"},
	"mouse_scroll":       {"wheel", "scroll down", "scroll up"},
	"get_mouse_position": {"where is the pointer", "cursor position", "pointer location"},
	"type_text":          {"write", "enter text", "keyboard"},
	"key_combo":          {"shortcut", "control and", "press ctrl", "hotkey", "modifier"},

	// Clipboard, audio, gamepad.
	"get_clipboard":   {"what did i copy", "paste buffer", "copied"},
	"set_clipboard":   {"copy this", "put on the clipboard", "make it pasteable"},
	"get_audio_state": {"muted", "how loud", "is there sound"},
	"set_volume":      {"louder", "quieter", "turn the sound", "turn it down", "turn it up"},
	"gamepad_axis":    {"stick", "analog", "thumbstick", "trigger"},
	"gamepad_button":  {"hold a controller", "controller button down"},
	"gamepad_tap":     {"press a controller", "tap the controller"},
	"gamepad_state":   {"controller reporting", "what the controller", "pad state"},

	// System and bookkeeping.
	"sudo_status":        {"as root", "privileges", "am i allowed", "elevated"},
	"service_control":    {"restart the", "daemon", "supervisor", "bounce the"},
	"action_log":         {"history", "audit", "what has been done", "trail", "past calls"},
	"subscribe_events":   {"notify", "tell me when", "instead of polling", "be told", "watch for changes", "let me know"},
	"unsubscribe_events": {"stop notifying", "no more notifications", "stop telling me", "stop sending"},
	"wait":               {"pause", "sleep", "delay", "for a moment", "seconds"},
	"wait_for_idle":      {"stops changing", "settles", "quiet", "finishes drawing", "stable"},
}

// keywordIndex splits toolKeywords into the single words, which are compared
// against one query term at a time, and the phrases, which are compared against
// the whole query. Building it once beats re-splitting on every search.
type keywordIndex struct {
	words   map[string]map[string]bool // tool -> set of single-word keywords
	phrases map[string][]string        // tool -> multi-word keywords
}

var keywords = buildKeywordIndex()

func buildKeywordIndex() keywordIndex {
	idx := keywordIndex{
		words:   make(map[string]map[string]bool, len(toolKeywords)),
		phrases: make(map[string][]string, len(toolKeywords)),
	}
	for tool, list := range toolKeywords {
		for _, kw := range list {
			kw = strings.ToLower(strings.TrimSpace(kw))
			if strings.Contains(kw, " ") {
				idx.phrases[tool] = append(idx.phrases[tool], kw)
				continue
			}
			if idx.words[tool] == nil {
				idx.words[tool] = map[string]bool{}
			}
			idx.words[tool][kw] = true
		}
	}
	return idx
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
// of keywords already answers well, on a corpus of a hundred and nineteen short
// strings that fits in a cache line's worth of cache misses. A hit on the name
// outranks the category, which outranks the description, because a tool called
// ssh_exec is a better answer to "ssh" than one that mentions ssh in passing.
func searchTools(tools []toolDef, query string, limit int) []searchHit {
	lower := strings.ToLower(query)
	var terms []string
	for _, term := range strings.Fields(lower) {
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
		kwWords := keywords.words[t.Name]

		score, strong, weak := 0, 0, 0

		// Phrases first, and against the whole query rather than term by term.
		// A phrase surviving intact is the strongest signal in the file: "port
		// forward" appearing in a sentence is not an accident of vocabulary the
		// way "forward" on its own is.
		for _, phrase := range keywords.phrases[t.Name] {
			if strings.Contains(lower, phrase) {
				score += 10
				strong++
			}
		}

		for _, term := range terms {
			// Underscores are separators, not letters: "remote access" should
			// find ssh_list_remote, and it will not if the term has to survive
			// as a contiguous substring of the whole name.
			//
			// Two-letter terms are matched only as whole words. Left as
			// substrings they matched anything: "am" in "which workspace am I
			// on" hit every gamepad tool through the "am" in "gamepad", and
			// four tools that had nothing to do with the question outranked the
			// one that answered it.
			hit := 0
			switch {
			case name == term:
				hit = 12
			case kwWords[term]:
				hit = 9
			case len(term) == 2:
				// whole-word only
				for _, part := range strings.Split(spaced, " ") {
					if part == term {
						hit = 8
						break
					}
				}
			case strings.Contains(spaced, term):
				hit = 8
			case strings.Contains(name, term):
				hit = 6
			}
			if categoryMatches(cat, term) {
				hit += 4
			}
			// A name, keyword or category hit is evidence about the tool. A
			// description hit is evidence about its prose, so it scores but does
			// not count towards the all-terms bonus below — otherwise the tools
			// with the longest descriptions win every vague query.
			if hit > 0 {
				strong++
			} else if len(term) >= 4 && strings.Contains(desc, term) {
				weak++
			}
			if strings.Contains(desc, term) {
				hit++
			}
			score += hit
		}

		// A tool with no name, keyword or category hit is normally not an
		// answer. The exception is the query that describes the tool in words
		// none of those three happen to hold: several distinctive terms landing
		// in one description is weak evidence, but it is evidence, and refusing
		// it outright is what left twenty-eight tools unreachable by any
		// phrasing at all. They enter far down the list, which is the right
		// place for a guess — visible to an agent reading ten results, never
		// ahead of a tool that actually matched.
		if strong == 0 {
			if weak < 2 {
				continue
			}
			score = weak
		} else {
			// Matching several terms beats matching one of them well: a query is
			// a description of one tool, not a bag of alternatives.
			score += strong * 3
		}
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
func (s *Server) dispatchRegistry(ctx context.Context, name string, args map[string]any, policy *Policy) ([]map[string]any, bool, bool) {
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
