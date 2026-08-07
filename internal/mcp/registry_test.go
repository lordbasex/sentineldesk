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

// The catalogue's own consistency check.
//
// NewServer already refuses to start on an unclassified tool, but a startup
// error is found by whoever runs the desktop next, which may be a user. These
// tests find it at `make test`, on a machine with no X display and no
// GStreamer pipeline, because buildTools only assembles literals — the one part
// of this program that can be exercised without a desktop underneath it.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lordbasex/sentineldesk/internal/config"
)

// catalogue builds the tool list the way NewServer does, without needing any of
// the desktop plumbing a real Server holds.
func catalogue(t *testing.T) []toolDef {
	t.Helper()
	tools := (&Server{}).buildTools()
	if len(tools) == 0 {
		t.Fatal("buildTools returned nothing")
	}
	return tools
}

// TestEveryToolIsClassified is the check this whole refactor exists for. Before
// it, risk lived in two maps in another file and 46 of 114 tools were in
// neither — silently refused under readonly and silently allowed under safe.
func TestEveryToolIsClassified(t *testing.T) {
	if err := validateCatalogue(catalogue(t)); err != nil {
		t.Fatalf("%v", err)
	}
}

// TestPolicyLevelsFollowRisk pins the meaning of the three levels to the
// classification, so that a tool reclassified by accident shows up here rather
// than as a permission somebody did not intend to grant.
func TestPolicyLevelsFollowRisk(t *testing.T) {
	tools := catalogue(t)
	idx := buildRiskIndex(tools)

	readonly := &Policy{level: "readonly", risk: idx}
	safe := &Policy{level: "safe", risk: idx}
	full := &Policy{level: "full", risk: idx}

	for _, tool := range tools {
		gotRO, _ := readonly.Allowed(tool.Name, nil)
		if want := tool.Risk == riskRead; gotRO != want {
			t.Errorf("readonly allowed %s = %v, want %v (risk %s)", tool.Name, gotRO, want, tool.Risk)
		}
		gotSafe, _ := safe.Allowed(tool.Name, nil)
		if want := tool.Risk != riskDanger; gotSafe != want {
			t.Errorf("safe allowed %s = %v, want %v (risk %s)", tool.Name, gotSafe, want, tool.Risk)
		}
		if ok, why := full.Allowed(tool.Name, nil); !ok {
			t.Errorf("full refused %s: %s", tool.Name, why)
		}
	}
}

// TestUnknownToolIsRefusedBelowFull covers the fail-closed direction: a name
// that is not in the catalogue must not slip through the level checks.
func TestUnknownToolIsRefusedBelowFull(t *testing.T) {
	idx := buildRiskIndex(catalogue(t))
	for _, level := range []string{"readonly", "safe"} {
		p := &Policy{level: level, risk: idx}
		if ok, _ := p.Allowed("no_such_tool", nil); ok {
			t.Errorf("%s allowed a tool that does not exist", level)
		}
	}
}

// TestRestrictCarriesTheRiskIndex guards a failure that would look like a
// tightening and behave like a lockout: a restricted policy with no index
// refuses everything below full, including the reads it is supposed to permit.
func TestRestrictCarriesTheRiskIndex(t *testing.T) {
	idx := buildRiskIndex(catalogue(t))
	base := &Policy{level: "full", risk: idx}
	got := base.Restrict("readonly", "", "")
	if got.risk == nil {
		t.Fatal("Restrict dropped the risk index")
	}
	if ok, why := got.Allowed("screenshot", nil); !ok {
		t.Errorf("restricted readonly refused screenshot: %s", why)
	}
	if ok, _ := got.Allowed("run_command", nil); ok {
		t.Error("restricted readonly allowed run_command")
	}
}

// TestAnnotationsMatchRisk checks the wire form, since a host reading
// readOnlyHint is trusting it in the same way MCP_POLICY does.
func TestAnnotationsMatchRisk(t *testing.T) {
	for _, tool := range catalogue(t) {
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		var wire struct {
			Name        string `json:"name"`
			InputSchema struct {
				Type string `json:"type"`
			} `json:"inputSchema"`
			Annotations struct {
				ReadOnly    bool `json:"readOnlyHint"`
				Destructive bool `json:"destructiveHint"`
			} `json:"annotations"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		if wire.Name != tool.Name {
			t.Errorf("marshalled name %q, want %q", wire.Name, tool.Name)
		}
		// The schema must survive the custom marshaller — it is the part a
		// model needs in order to call anything at all.
		if wire.InputSchema.Type != "object" {
			t.Errorf("%s: inputSchema did not survive marshalling", tool.Name)
		}
		if want := tool.Risk == riskRead; wire.Annotations.ReadOnly != want {
			t.Errorf("%s: readOnlyHint %v, want %v", tool.Name, wire.Annotations.ReadOnly, want)
		}
		if want := tool.Risk == riskDanger; wire.Annotations.Destructive != want {
			t.Errorf("%s: destructiveHint %v, want %v", tool.Name, wire.Annotations.Destructive, want)
		}
	}
}

// TestCoreToolsExist stops discovery mode from advertising a name that is not
// in the catalogue — which would leave a host with a tool it cannot call and no
// hint that the list was wrong rather than the desktop.
func TestCoreToolsExist(t *testing.T) {
	have := map[string]bool{}
	for _, tool := range catalogue(t) {
		have[tool.Name] = true
	}
	for name := range coreTools {
		if !have[name] {
			t.Errorf("coreTools lists %q, which is not a tool", name)
		}
	}
	if !coreTools["tool_search"] {
		t.Error("discovery mode without tool_search hides the catalogue with no way back")
	}
}

// TestInjectingToolsAreNotReadOnly cross-checks the two classifications that
// exist for different reasons: RequiresControl decides who may drive right now,
// Risk decides what the agent may ever do. Nothing that puts events into X can
// honestly be called read-only.
func TestInjectingToolsAreNotReadOnly(t *testing.T) {
	for _, tool := range catalogue(t) {
		if tool.RequiresControl && tool.Risk == riskRead {
			t.Errorf("%s requires control but is classified read", tool.Name)
		}
	}
}

// gatedBeforeTheRefactor is the switch statement that used to live in mcp.go,
// frozen here verbatim.
//
// Moving the list onto the toolDefs was a mechanical change and had to stay one:
// which tools the room arbitrates is a product decision about when an agent and
// a person collide, not a detail of where the list is stored. This is the proof
// that nothing was added or dropped on the way.
//
// Changing this set is allowed — it is not sacred — but it is a separate,
// deliberate act. Editing this list to make a failing test pass is the mistake
// it exists to catch.
var gatedBeforeTheRefactor = []string{
	"mouse_move", "mouse_click", "mouse_down", "mouse_up", "mouse_drag",
	"mouse_scroll", "type_text", "key_combo",
	"gamepad_button", "gamepad_axis", "gamepad_state", "gamepad_tap",
	"ui_click", "ui_set_text", "ui_focus", "fill_form", "terminal_run",
	"start_restream", "stop_restream",
}

func TestControlGateParity(t *testing.T) {
	want := map[string]bool{}
	for _, name := range gatedBeforeTheRefactor {
		want[name] = true
	}

	got := map[string]bool{}
	for _, tool := range catalogue(t) {
		if tool.RequiresControl {
			got[tool.Name] = true
		}
	}

	for name := range want {
		if !got[name] {
			t.Errorf("%s was gated before the refactor and is not now", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%s is gated now and was not before the refactor", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("gated %d tools, want %d", len(got), len(want))
	}
}

// TestServerGateReadsTheCatalogue closes the loop: handleToolCall asks
// s.injectsInput, so the index has to agree with the field it was built from.
func TestServerGateReadsTheCatalogue(t *testing.T) {
	s := testServer(t)
	for _, tool := range s.tools {
		if s.injectsInput(tool.Name) != tool.RequiresControl {
			t.Errorf("%s: gate says %v, toolDef says %v",
				tool.Name, s.injectsInput(tool.Name), tool.RequiresControl)
		}
	}
	if s.injectsInput("no_such_tool") {
		t.Error("the gate claimed a tool that does not exist needs control")
	}
}

// TestRequiresControlIsPublished is the point of the whole change: a client has
// to be able to learn this from tools/list instead of carrying its own copy.
func TestRequiresControlIsPublished(t *testing.T) {
	for _, tool := range catalogue(t) {
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		var wire struct {
			Annotations struct {
				RequiresControl bool `json:"sentineldesk/requiresControl"`
			} `json:"annotations"`
		}
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatalf("%s: %v", tool.Name, err)
		}
		if wire.Annotations.RequiresControl != tool.RequiresControl {
			t.Errorf("%s: published %v, want %v",
				tool.Name, wire.Annotations.RequiresControl, tool.RequiresControl)
		}
	}
}

// TestRiskDoesNotImplyControl records why the annotation is needed at all: a
// client cannot derive the room gate from the risk level, in either direction.
func TestRiskDoesNotImplyControl(t *testing.T) {
	byName := map[string]toolDef{}
	for _, tool := range catalogue(t) {
		byName[tool.Name] = tool
	}
	for _, c := range []struct {
		gated, notGated string
		risk            riskLevel
	}{
		{"ui_click", "set_volume", riskWrite},
		{"start_restream", "write_file", riskDanger},
	} {
		a, b := byName[c.gated], byName[c.notGated]
		if a.Risk != c.risk || b.Risk != c.risk {
			t.Fatalf("%s and %s no longer share risk %s", c.gated, c.notGated, c.risk)
		}
		if !a.RequiresControl {
			t.Errorf("%s should require control", c.gated)
		}
		if b.RequiresControl {
			t.Errorf("%s should not require control", c.notGated)
		}
	}
}

func TestToolSearchFindsTheObviousThings(t *testing.T) {
	tools := catalogue(t)
	cases := []struct {
		query string
		want  string
	}{
		{"give someone remote access over ssh", "ssh_"},
		{"read the text on the screen", "read_screen_text"},
		{"take a screenshot", "screenshot"},
		{"open a tunnel", "ssh_tunnel"},
		{"click a button by name", "ui_"},
		{"install a package", "install_packages"},
	}
	for _, c := range cases {
		hits := searchTools(tools, c.query, 10)
		if len(hits) == 0 {
			t.Errorf("%q matched nothing", c.query)
			continue
		}
		found := false
		for _, h := range hits {
			if len(h.Name) >= len(c.want) && h.Name[:len(c.want)] == c.want {
				found = true
				break
			}
		}
		if !found {
			names := make([]string, len(hits))
			for i, h := range hits {
				names[i] = h.Name
			}
			t.Errorf("%q did not surface %s* in the top %d: %v", c.query, c.want, len(hits), names)
		}
	}
}

func TestToolSearchRespectsTheLimit(t *testing.T) {
	hits := searchTools(catalogue(t), "window", 3)
	if len(hits) > 3 {
		t.Errorf("limit 3 returned %d", len(hits))
	}
	// The schema has to come back with the hit: the point of searching is to be
	// able to call what you found without a second round trip.
	for _, h := range hits {
		if len(h.InputSchema) == 0 {
			t.Errorf("%s came back without its schema", h.Name)
		}
	}
}

// --- over the wire -------------------------------------------------------------
//
// The tests above check the catalogue as data. These check the path a real AI
// host takes to reach it — serve() reading JSON-RPC off a socket, the policy
// filter on tools/list, the discovery filter on top of it, and a tool_search
// call going through dispatch. None of it touches X, so it runs anywhere.

// session drives one Server over an in-memory connection.
type session struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
	id   int
}

func newSession(t *testing.T, s *Server) *session {
	t.Helper()
	client, server := net.Pipe()
	go s.serve(server)
	t.Cleanup(func() { client.Close() })
	return &session{t: t, conn: client, r: bufio.NewReader(client)}
}

// send writes a request and returns its id without waiting for the answer, so a
// test can send something else — a cancellation — while it is still running.
func (c *session) send(method string, params any) int {
	c.t.Helper()
	c.id++
	req := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method}
	if params != nil {
		req["params"] = params
	}
	c.write(req, method)
	return c.id
}

// notify writes a request with no id, which is what a notification is.
func (c *session) notify(method string, params any) {
	c.t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		req["params"] = params
	}
	c.write(req, method)
}

func (c *session) write(req map[string]any, method string) {
	c.t.Helper()
	line, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("%v", err)
	}
	_ = c.conn.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		c.t.Fatalf("write %s: %v", method, err)
	}
}

// readFull returns the response id alongside the result, so a test can prove a
// reply belongs to the request it thinks it does.
func (c *session) readFull() (int, map[string]any) {
	c.t.Helper()
	_ = c.conn.SetDeadline(time.Now().Add(20 * time.Second))
	raw, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var resp struct {
		ID     int            `json:"id"`
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		c.t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		c.t.Fatalf("%s", resp.Error.Message)
	}
	return resp.ID, resp.Result
}

func (c *session) read() map[string]any {
	c.t.Helper()
	_, res := c.readFull()
	return res
}

func (c *session) call(method string, params any) map[string]any {
	c.t.Helper()
	c.send(method, params)
	return c.read()
}

func (c *session) listedNames() []string {
	c.t.Helper()
	raw, _ := json.Marshal(c.call("tools/list", nil)["tools"])
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		c.t.Fatalf("%v", err)
	}
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name
	}
	return names
}

// testServer builds a real Server with none of the desktop attached. Nothing in
// construction or in the tools exercised here reaches X.
func testServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(config.Config{Display: ":99"}, nil, nil, nil, nil)
}

func TestToolsListAdvertisesTheWholeCatalogue(t *testing.T) {
	s := testServer(t)
	got := newSession(t, s).listedNames()
	if len(got) != len(s.tools) {
		t.Fatalf("tools/list returned %d, catalogue has %d", len(got), len(s.tools))
	}
}

// TestDiscoveryHidesButDoesNotForbid is the invariant the whole discovery mode
// rests on. If a hidden tool ever stopped being callable, discovery would have
// silently become a permission system — one nobody wrote, audited or logged.
func TestDiscoveryHidesButDoesNotForbid(t *testing.T) {
	s := testServer(t)
	s.discovery = true
	c := newSession(t, s)

	listed := c.listedNames()
	if len(listed) >= len(s.tools) {
		t.Fatalf("discovery listed %d of %d tools", len(listed), len(s.tools))
	}
	for _, name := range listed {
		if !coreTools[name] {
			t.Errorf("discovery advertised %s, which is not in the core set", name)
		}
	}

	// get_screen_info is deliberately not in the core set. It must still run.
	if coreTools["get_screen_info"] {
		t.Fatal("this test needs a tool outside the core set")
	}
	res := c.call("tools/call", map[string]any{
		"name": "tool_search", "arguments": map[string]any{"query": "screen resolution"},
	})
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("tool_search failed: %v", res["content"])
	}
}

func TestToolSearchOverTheWireReturnsSchemas(t *testing.T) {
	c := newSession(t, testServer(t))
	res := c.call("tools/call", map[string]any{
		"name":      "tool_search",
		"arguments": map[string]any{"category": "ssh", "limit": 5},
	})
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("tool_search reported an error: %v", res["content"])
	}
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatal("tool_search returned no content")
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	var payload struct {
		Matched int `json:"matched"`
		Tools   []struct {
			Name        string          `json:"name"`
			Category    string          `json:"category"`
			Risk        string          `json:"risk"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("tool_search did not return JSON: %v\n%s", err, text)
	}
	if payload.Matched == 0 {
		t.Fatal("category ssh matched nothing")
	}
	for _, tool := range payload.Tools {
		if tool.Category != "ssh" {
			t.Errorf("category ssh returned %s (%s)", tool.Name, tool.Category)
		}
		if tool.Risk == "" || tool.Risk == "unclassified" {
			t.Errorf("%s came back with risk %q", tool.Name, tool.Risk)
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("%s came back without its schema", tool.Name)
		}
	}
}

// TestReadonlyConnectionSearchesOnlyWhatItMayCall covers the reason tool_search
// takes the connection's policy: surfacing a tool that will then be refused is
// a worse answer than surfacing nothing.
func TestReadonlyConnectionSearchesOnlyWhatItMayCall(t *testing.T) {
	c := newSession(t, testServer(t))
	c.call("sentineldesk/policy", map[string]any{"level": "readonly"})

	res := c.call("tools/call", map[string]any{
		"name":      "tool_search",
		"arguments": map[string]any{"query": "run a command in a shell"},
	})
	content, _ := res["content"].([]any)
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)

	var payload struct {
		Tools []struct {
			Name string `json:"name"`
			Risk string `json:"risk"`
		} `json:"tools"`
	}
	// No match at all is a valid answer here; an unparseable one is not.
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return
	}
	for _, tool := range payload.Tools {
		if tool.Risk != "read" {
			t.Errorf("a readonly connection was offered %s (risk %s)", tool.Name, tool.Risk)
		}
	}
}

// --- denial kinds ----------------------------------------------------------------
//
// The sentence a caller gets back is written for a model to read and gets
// reworded whenever a better one is found. These pin the machine-readable half,
// which is the part a runtime branches on: policy is final, room means ask a
// person and retry, tool_error may be worth retrying. Getting them confused
// turns "wait your turn" into "give up".

// roomWithoutControls is a Rooms that never grants control, so the gate refuses.
// Only the three methods mayInject touches do anything.
type roomWithoutControls struct{ Rooms }

func (roomWithoutControls) JoinAgent(string) string      { return AgentID }
func (roomWithoutControls) IsController(string) bool     { return false }
func (roomWithoutControls) Controller() (string, string) { return "someone", "Viewer 1" }

// denialOf calls a tool and returns the kind reported alongside the content.
// An empty string means the call succeeded.
func (c *session) denialOf(name string, args map[string]any) string {
	c.t.Helper()
	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	res := c.call("tools/call", params)

	isErr, _ := res["isError"].(bool)
	meta, hasMeta := res["_meta"].(map[string]any)
	if !isErr {
		if hasMeta {
			c.t.Errorf("%s succeeded but carried _meta %v", name, meta)
		}
		return ""
	}
	if !hasMeta {
		c.t.Fatalf("%s failed with no _meta to say why", name)
	}
	kind, _ := meta["sentineldesk/denial"].(string)
	if kind == "" {
		c.t.Fatalf("%s: _meta has no denial kind: %v", name, meta)
	}
	return kind
}

func TestDenialKindUnknownTool(t *testing.T) {
	c := newSession(t, testServer(t))
	if got := c.denialOf("no_such_tool", nil); got != string(denialUnknown) {
		t.Errorf("kind %q, want %q", got, denialUnknown)
	}
}

// TestDenialKindUnknownToolAtEveryLevel is why the catalogue is checked before
// policy. The same nonexistent name used to come back as a policy refusal under
// safe and an unknown tool under full — two answers to one question.
func TestDenialKindUnknownToolAtEveryLevel(t *testing.T) {
	for _, level := range []string{"full", "safe", "readonly"} {
		t.Run(level, func(t *testing.T) {
			c := newSession(t, testServer(t))
			c.call("sentineldesk/policy", map[string]any{"level": level})
			if got := c.denialOf("no_such_tool", nil); got != string(denialUnknown) {
				t.Errorf("kind %q, want %q", got, denialUnknown)
			}
		})
	}
}

// TestAConnectionCannotWidenItself is the invariant the whole restriction
// mechanism exists for, checked where it is actually used rather than on the
// method in isolation.
//
// Restrict itself was always correct. serve called it on the DAEMON's policy
// every time, so each request started afresh at the ceiling and a connection
// that had dropped itself to readonly could ask for full and be given it. The
// unit test for Restrict passed throughout; it took a live check against a real
// desktop to notice, because the bug was in the caller.
func TestAConnectionCannotWidenItself(t *testing.T) {
	c := newSession(t, testServer(t))

	applied := c.call("sentineldesk/policy", map[string]any{"level": "readonly"})
	if applied["level"] != "readonly" {
		t.Fatalf("restricting to readonly gave %v", applied["level"])
	}

	for _, level := range []string{"full", "safe"} {
		applied = c.call("sentineldesk/policy", map[string]any{"level": level})
		if applied["level"] != "readonly" {
			t.Errorf("asking for %s from readonly gave %v", level, applied["level"])
		}
	}
	// And the ceiling really is enforced, not just reported.
	if got := c.denialOf("run_command", map[string]any{"command": "true"}); got != string(denialPolicy) {
		t.Errorf("run_command after asking for full: kind %q, want %q", got, denialPolicy)
	}
}

// TestRestrictionsAccumulate: denials add up across calls rather than replacing
// each other, which is the same monotonicity from the other direction.
func TestRestrictionsAccumulate(t *testing.T) {
	c := newSession(t, testServer(t))
	c.call("sentineldesk/policy", map[string]any{"deny": "ui_*"})
	c.call("sentineldesk/policy", map[string]any{"deny": "browser_*"})

	if got := c.denialOf("ui_tree", nil); got != string(denialPolicy) {
		t.Errorf("the first denial was forgotten: ui_tree kind %q", got)
	}
	if got := c.denialOf("browser_tabs", nil); got != string(denialPolicy) {
		t.Errorf("the second denial did not take: browser_tabs kind %q", got)
	}
}

// TestDenialKindPolicy also proves the separation holds the other way: a tool
// that exists but is hidden by the level reports policy, not unknown_tool.
func TestDenialKindPolicy(t *testing.T) {
	c := newSession(t, testServer(t))
	c.call("sentineldesk/policy", map[string]any{"level": "readonly"})
	if got := c.denialOf("run_command", map[string]any{"command": "true"}); got != string(denialPolicy) {
		t.Errorf("kind %q, want %q", got, denialPolicy)
	}
}

func TestDenialKindPolicyFromDenyList(t *testing.T) {
	c := newSession(t, testServer(t))
	c.call("sentineldesk/policy", map[string]any{"deny": "ui_*"})
	if got := c.denialOf("ui_tree", nil); got != string(denialPolicy) {
		t.Errorf("kind %q, want %q", got, denialPolicy)
	}
}

// TestDenialKindRoom is the one the agent loop most needs to tell apart: it
// means ask a person and try again, not give up.
func TestDenialKindRoom(t *testing.T) {
	s := testServer(t)
	s.SetRoom(roomWithoutControls{}, "AI agent")
	c := newSession(t, s)

	got := c.denialOf("mouse_move", map[string]any{"x": 10, "y": 10})
	if got != string(denialRoom) {
		t.Errorf("kind %q, want %q", got, denialRoom)
	}

	// A tool that does not need the controls is unaffected by the same room.
	if got := c.denialOf("tool_search", map[string]any{"query": "screen"}); got != "" {
		t.Errorf("tool_search was refused with kind %q", got)
	}
}

// TestDenialKindOrder pins the precedence. A gated tool that policy already
// refuses must report policy: the room question never arises, because the call
// was not going to happen either way.
func TestDenialKindOrder(t *testing.T) {
	s := testServer(t)
	s.SetRoom(roomWithoutControls{}, "AI agent")
	c := newSession(t, s)
	c.call("sentineldesk/policy", map[string]any{"level": "readonly"})

	// mouse_move requires control AND is refused by readonly.
	if got := c.denialOf("mouse_move", map[string]any{"x": 10, "y": 10}); got != string(denialPolicy) {
		t.Errorf("kind %q, want %q — policy outranks the room gate", got, denialPolicy)
	}
}

// TestDenialKindIsLogged keeps the audit trail machine-readable too: the reason
// a call was refused should not have to be recovered from prose there either.
func TestDenialKindIsLogged(t *testing.T) {
	s := testServer(t)
	c := newSession(t, s)
	c.denialOf("no_such_tool", nil)

	entries := s.actions.Tail(1, "")
	if len(entries) != 1 {
		t.Fatalf("logged %d entries, want 1", len(entries))
	}
	if entries[0].Kind != string(denialUnknown) {
		t.Errorf("logged kind %q, want %q", entries[0].Kind, denialUnknown)
	}
	if entries[0].Denied == "" {
		t.Error("logged a kind with no human reason beside it")
	}
	if entries[0].OK {
		t.Error("a refused call was logged as OK")
	}
}

// TestNoRoomDoesNotKillTheCatalogue covers a regression found while adding the
// denial kinds: callRoom claimed every tool name when SetRoom had not been
// called, so a Server without a room answered "this build has no room attached"
// to the entire catalogue. The daemon always calls SetRoom, so it never showed
// there — it showed the moment anything else embedded the server.
func TestNoRoomDoesNotKillTheCatalogue(t *testing.T) {
	s := testServer(t)
	if s.room != nil {
		t.Fatal("this test needs a Server with no room")
	}
	c := newSession(t, s)

	// `wait` only sleeps, so it needs no display and must simply succeed. It
	// also sits in the main switch, AFTER callRoom in the dispatch chain — the
	// tools handled before callRoom never reached the bug and would pass this
	// test either way.
	res := c.call("tools/call", map[string]any{
		"name": "wait", "arguments": map[string]any{"ms": 1},
	})
	if isErr, _ := res["isError"].(bool); isErr {
		t.Errorf("wait failed with no room attached: %v", res["content"])
	}

	// The room tools themselves still report the missing room, as before.
	if got := c.denialOf("room_state", nil); got != string(denialToolError) {
		t.Errorf("room_state kind %q, want %q", got, denialToolError)
	}
}

func TestSuccessCarriesNoDenial(t *testing.T) {
	c := newSession(t, testServer(t))
	if got := c.denialOf("tool_search", map[string]any{"query": "take a screenshot"}); got != "" {
		t.Errorf("a successful call reported kind %q", got)
	}
}

// --- cancellation ------------------------------------------------------------

func TestInflightBookkeeping(t *testing.T) {
	f := newInflight()
	_, c1 := context.WithCancel(context.Background())
	ctx2, c2 := context.WithCancel(context.Background())

	var answers int
	note := func(map[string]any) { answers++ }
	f.add("1", c1, note)
	f.add("2", c2, note)

	if !f.cancel("1", "stop") {
		t.Error("cancel reported nothing to cancel")
	}
	// Cancelling answers as well as stopping: the client is waiting and the
	// tool may not be listening.
	if answers != 1 {
		t.Errorf("cancel produced %d answers, want 1", answers)
	}
	// A second cancel for the same id is the normal race with a call that has
	// just finished, and must not be treated as an error or answered twice.
	if f.cancel("1", "stop") {
		t.Error("cancel reported a second stop for the same id")
	}
	if f.cancel("nope", "stop") {
		t.Error("cancel reported stopping a request that never existed")
	}
	if answers != 1 {
		t.Errorf("stray cancels produced %d answers, want 1", answers)
	}

	f.cancelAll("connection closed")
	select {
	case <-ctx2.Done():
	default:
		t.Error("cancelAll left a call running")
	}
	if answers != 2 {
		t.Errorf("cancelAll produced %d answers in total, want 2", answers)
	}
	if len(f.calls) != 0 {
		t.Errorf("cancelAll left %d entries behind", len(f.calls))
	}
}

// TestDoneDoesNotAnswer: a call that finishes normally is answered by its own
// handler. If done() answered as well, every successful call would produce two
// responses for one id.
func TestDoneDoesNotAnswer(t *testing.T) {
	f := newInflight()
	_, cancel := context.WithCancel(context.Background())
	answered := false
	f.add("1", cancel, func(map[string]any) { answered = true })
	f.done("1")
	if answered {
		t.Error("done answered a call the handler was about to answer itself")
	}
	if len(f.calls) != 0 {
		t.Error("done left the entry behind")
	}
}

// TestRequestKeyKeepsTypesApart: JSON-RPC allows a string or a number id and
// they are different requests, so cancelling id "2" must not stop id 2.
func TestRequestKeyKeepsTypesApart(t *testing.T) {
	if requestKey(json.RawMessage(`2`)) == requestKey(json.RawMessage(`"2"`)) {
		t.Error("id 2 and id \"2\" share a key")
	}
	if requestKey(json.RawMessage(" 7 ")) != requestKey(json.RawMessage("7")) {
		t.Error("whitespace changed the key")
	}
}

// TestCancelStopsARunningCommand is the one that matters: before this, closing
// a client or cancelling a run left the work running, because dispatch took no
// context and every tool that needed a deadline built one from Background.
//
// The command touches a file and then sleeps. Waiting for the file means the
// process really started and the call is really registered, so the cancellation
// cannot arrive too early — the alternative is a sleep and a flaky test.
func TestCancelStopsARunningCommand(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}
	started := filepath.Join(t.TempDir(), "started")
	c := newSession(t, testServer(t))

	id := c.send("tools/call", map[string]any{
		"name": "run_command",
		"arguments": map[string]any{
			"command":    "touch " + started + "; sleep 30",
			"timeout_ms": 30000,
		},
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the command never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	begin := time.Now()
	c.notify("notifications/cancelled", map[string]any{
		"requestId": id, "reason": "test",
	})

	res := c.read()
	elapsed := time.Since(begin)

	// The command was going to sleep for thirty seconds. Coming back at all is
	// the result; coming back quickly is what says the process was killed
	// rather than waited out. The couple of seconds it does take are
	// cmd.WaitDelay, not the sleep.
	if elapsed > 10*time.Second {
		t.Errorf("the call took %v after cancelling", elapsed)
	}

	// And it has to SAY it was cancelled. The first version of this passed the
	// timing check and still reported success: a killed process is just a
	// process with a non-zero exit status, so run_command answered
	// {"exit_code": -1} with no error, which is true about the process and a
	// lie about the request.
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Errorf("a cancelled command reported success: %v", res["content"])
	}
	meta, _ := res["_meta"].(map[string]any)
	if kind, _ := meta["sentineldesk/denial"].(string); kind != string(denialCancelled) {
		t.Errorf("kind %q, want %q", kind, denialCancelled)
	}
}

// TestCancelAnswersWithoutWaitingForTheTool is the point of the second half of
// this work. handleToolCall blocks on dispatch, so a tool that is not listening
// to its context used to hold the response back until it finished — the client
// asked to stop and then waited out the full duration to be told it had, unable
// to tell "still stopping" from "ignored you".
//
// A thirty-second wait, cancelled immediately, has to come back immediately.
func TestCancelAnswersWithoutWaitingForTheTool(t *testing.T) {
	c := newSession(t, testServer(t))
	id := c.send("tools/call", map[string]any{
		"name": "wait", "arguments": map[string]any{"ms": 30000},
	})

	begin := time.Now()
	c.notify("notifications/cancelled", map[string]any{
		"requestId": id, "reason": "user pressed stop",
	})
	gotID, res := c.readFull()
	elapsed := time.Since(begin)

	if gotID != id {
		t.Errorf("answered request %d, want %d", gotID, id)
	}
	if elapsed > 5*time.Second {
		t.Errorf("the cancellation took %v to be acknowledged", elapsed)
	}
	meta, _ := res["_meta"].(map[string]any)
	if kind, _ := meta["sentineldesk/denial"].(string); kind != string(denialCancelled) {
		t.Errorf("kind %q, want %q", kind, denialCancelled)
	}
	// The client's reason comes back, because the model reading the transcript
	// was not the one that pressed stop.
	content, _ := res["content"].([]any)
	first, _ := content[0].(map[string]any)
	if text, _ := first["text"].(string); !strings.Contains(text, "user pressed stop") {
		t.Errorf("the reason did not survive: %q", text)
	}

	// And exactly one response for that id. The handler goroutine wakes up
	// afterwards and tries to reply too; if that got through, this next call
	// would read the stale answer instead of its own.
	pingID := c.send("ping", nil)
	if gotID, _ := c.readFull(); gotID != pingID {
		t.Errorf("a second response arrived for the cancelled call (id %d)", gotID)
	}
}

// TestCancelUnknownRequestIsHarmless: a cancellation that names a request which
// already finished is a normal race, not a reason to break the connection.
func TestCancelUnknownRequestIsHarmless(t *testing.T) {
	c := newSession(t, testServer(t))
	c.notify("notifications/cancelled", map[string]any{"requestId": 999, "reason": "nothing"})
	c.notify("notifications/cancelled", nil)

	// The connection still works.
	if got := c.denialOf("no_such_tool", nil); got != string(denialUnknown) {
		t.Errorf("kind %q after a stray cancellation, want %q", got, denialUnknown)
	}
}

// --- connection identity -------------------------------------------------------

// initialize announces a client and returns the connection id the server gave
// it. The id has to come back to the client, or nothing that supervises it can
// name the connection it wants stopped.
func (c *session) initialize(name string) uint64 {
	c.t.Helper()
	res := c.call("initialize", map[string]any{
		"clientInfo": map[string]any{"name": name, "version": "1.0"},
	})
	meta, ok := res["_meta"].(map[string]any)
	if !ok {
		c.t.Fatal("initialize returned no _meta, so the client cannot learn its id")
	}
	id, ok := meta["sentineldesk/connectionId"].(float64)
	if !ok {
		c.t.Fatalf("no connection id in %v", meta)
	}
	return uint64(id)
}

func TestConnectionsAreNumberedAndNamed(t *testing.T) {
	s := testServer(t)
	a := newSession(t, s)
	b := newSession(t, s)

	idA := a.initialize("agent-runtime")
	idB := b.initialize("claude-code")
	if idA == idB {
		t.Fatalf("both connections got id %d", idA)
	}

	// The name and the number both reach the audit trail. Without them every
	// entry reads "the agent did this", which stops being a useful sentence the
	// moment a runtime fans out across several connections.
	a.call("tools/call", map[string]any{"name": "wait", "arguments": map[string]any{"ms": 1}})
	entries := s.actions.Tail(1, "")
	if len(entries) != 1 {
		t.Fatalf("logged %d entries, want 1", len(entries))
	}
	if entries[0].Conn != idA {
		t.Errorf("logged connection %d, want %d", entries[0].Conn, idA)
	}
	if entries[0].Client != "agent-runtime 1.0" {
		t.Errorf("logged client %q", entries[0].Client)
	}
}

// TestHaltStopsOneConnectionOnly is the whole point of the identity. An
// emergency stop for the agent runtime must not stop an operator's own MCP
// session, and it must not stop the desktop.
func TestHaltStopsOneConnectionOnly(t *testing.T) {
	s := testServer(t)
	agent := newSession(t, s)
	operator := newSession(t, s)

	agentID := agent.initialize("agent-runtime")
	operator.initialize("claude-code")

	s.HaltConnection(agentID, "emergency stop")

	if got := agent.denialOf("wait", map[string]any{"ms": 1}); got != string(denialEmergency) {
		t.Errorf("halted connection got kind %q, want %q", got, denialEmergency)
	}
	if got := operator.denialOf("wait", map[string]any{"ms": 1}); got != "" {
		t.Errorf("the other connection was refused with kind %q", got)
	}

	s.ResumeConnection(agentID)
	if got := agent.denialOf("wait", map[string]any{"ms": 1}); got != "" {
		t.Errorf("resume did not lift the halt: kind %q", got)
	}
}

// TestHaltOutranksEverything: a halted connection is not being told about the
// catalogue, it is being told to stop. Answering "unknown tool" first would let
// a client that is supposed to be doing nothing map what exists.
func TestHaltOutranksEverything(t *testing.T) {
	s := testServer(t)
	c := newSession(t, s)
	id := c.initialize("agent-runtime")
	s.HaltConnection(id, "emergency stop")

	for _, name := range []string{"no_such_tool", "run_command", "wait"} {
		if got := c.denialOf(name, nil); got != string(denialEmergency) {
			t.Errorf("%s: kind %q, want %q", name, got, denialEmergency)
		}
	}
}

func TestHaltIsLoggedWithItsConnection(t *testing.T) {
	s := testServer(t)
	c := newSession(t, s)
	id := c.initialize("agent-runtime")
	s.HaltConnection(id, "emergency stop: operator")
	c.denialOf("wait", map[string]any{"ms": 1})

	entries := s.actions.Tail(1, "")
	if len(entries) != 1 {
		t.Fatalf("logged %d entries, want 1", len(entries))
	}
	if entries[0].Kind != string(denialEmergency) {
		t.Errorf("logged kind %q, want %q", entries[0].Kind, denialEmergency)
	}
	if entries[0].Conn != id {
		t.Errorf("logged connection %d, want %d", entries[0].Conn, id)
	}
	if !strings.Contains(entries[0].Denied, "operator") {
		t.Errorf("the reason did not reach the log: %q", entries[0].Denied)
	}
}

// TestUnhaltedConnectionsNeedNoInitialize: a client that never sends
// clientInfo still gets an id and still works. The name is nice to have; the
// number is what the halt needs.
func TestConnectionWorksWithoutClientInfo(t *testing.T) {
	c := newSession(t, testServer(t))
	if got := c.denialOf("wait", map[string]any{"ms": 1}); got != "" {
		t.Errorf("a connection that never introduced itself was refused: %q", got)
	}
}

// --- progress --------------------------------------------------------------------

// readMessage returns the whole outbound message, so a test can look at
// notifications as well as answers.
func (c *session) readMessage() map[string]any {
	c.t.Helper()
	_ = c.conn.SetDeadline(time.Now().Add(20 * time.Second))
	raw, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		c.t.Fatalf("decode: %v", err)
	}
	return msg
}

// callCollecting runs a tool and returns its result plus every notification
// that arrived first.
func (c *session) callCollecting(params map[string]any) (map[string]any, []map[string]any) {
	c.t.Helper()
	c.send("tools/call", params)
	var notes []map[string]any
	for {
		msg := c.readMessage()
		if _, isResponse := msg["result"]; isResponse {
			res, _ := msg["result"].(map[string]any)
			return res, notes
		}
		if _, isErr := msg["error"]; isErr {
			c.t.Fatalf("error response: %v", msg["error"])
		}
		notes = append(notes, msg)
	}
}

func TestProgressReachesTheClientWhileACommandRuns(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}
	restore := progressInterval
	progressInterval = 50 * time.Millisecond
	t.Cleanup(func() { progressInterval = restore })

	c := newSession(t, testServer(t))
	res, notes := c.callCollecting(map[string]any{
		"name": "run_command",
		"arguments": map[string]any{
			"command":    "echo first; sleep 0.4; echo second; sleep 0.2",
			"timeout_ms": 10000,
		},
		"_meta": map[string]any{"progressToken": "tok-1"},
	})

	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("the command failed: %v", res["content"])
	}
	if len(notes) == 0 {
		t.Fatal("a command that ran for most of a second sent no progress at all")
	}

	sawToken, sawOutput := false, false
	for _, n := range notes {
		if n["method"] != "notifications/progress" {
			t.Errorf("unexpected notification %v", n["method"])
			continue
		}
		params, _ := n["params"].(map[string]any)
		if params["progressToken"] == "tok-1" {
			sawToken = true
		}
		if _, ok := params["progress"]; !ok {
			t.Error("a progress notification with no progress in it")
		}
		// The command's own output is the only honest progress it has, so it
		// should be what the message carries.
		if msg, _ := params["message"].(string); strings.Contains(msg, "first") ||
			strings.Contains(msg, "second") {
			sawOutput = true
		}
	}
	if !sawToken {
		t.Error("the client's own token did not come back")
	}
	if !sawOutput {
		t.Errorf("no notification carried the command's output: %v", notes)
	}
}

// TestNoProgressWithoutAToken: a client that did not ask must not be given a
// stream of messages it has to discard.
func TestNoProgressWithoutAToken(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}
	restore := progressInterval
	progressInterval = 50 * time.Millisecond
	t.Cleanup(func() { progressInterval = restore })

	c := newSession(t, testServer(t))
	_, notes := c.callCollecting(map[string]any{
		"name": "run_command",
		"arguments": map[string]any{
			"command": "echo hello; sleep 0.4", "timeout_ms": 10000,
		},
	})
	if len(notes) != 0 {
		t.Errorf("sent %d notifications to a client that asked for none: %v", len(notes), notes)
	}
}

func TestProgressTokenParsing(t *testing.T) {
	for _, tc := range []struct {
		name, params, want string
	}{
		{"string", `{"_meta":{"progressToken":"abc"}}`, `"abc"`},
		{"number", `{"_meta":{"progressToken":7}}`, `7`},
		{"absent", `{"name":"wait"}`, ""},
		{"null", `{"_meta":{"progressToken":null}}`, ""},
		{"no meta", `{}`, ""},
		{"malformed", `not json`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := progressToken(json.RawMessage(tc.params))
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", string(got), tc.want)
			}
		})
	}
}

func TestTailWriterKeepsTheLastLine(t *testing.T) {
	w := &tailWriter{}
	w.Write([]byte("one\ntwo\n"))
	if got := w.line(); got != "two" {
		t.Errorf("got %q, want %q", got, "two")
	}
	// A partial line is not reported until it is complete.
	w.Write([]byte("thr"))
	if got := w.line(); got != "two" {
		t.Errorf("a partial line was reported: %q", got)
	}
	w.Write([]byte("ee\n"))
	if got := w.line(); got != "three" {
		t.Errorf("got %q, want %q", got, "three")
	}
	// Blank lines do not overwrite the last real one.
	w.Write([]byte("\n   \n"))
	if got := w.line(); got != "three" {
		t.Errorf("a blank line overwrote the last one: %q", got)
	}
	// And a command that never emits a newline cannot grow the buffer forever.
	w.Write(bytes.Repeat([]byte("x"), 100_000))
	if len(w.buf) > 8192 {
		t.Errorf("buffer grew to %d bytes", len(w.buf))
	}
}

func TestEveryToolHasACategory(t *testing.T) {
	for _, tool := range catalogue(t) {
		if categoryOf(tool.Name) == "" {
			t.Errorf("%s has no category", tool.Name)
		}
	}
}

// --- argument validation ------------------------------------------------------

func TestEveryToolDeclaresItsArguments(t *testing.T) {
	// The index is built from the published schema, so a tool whose schema and
	// dispatcher disagree would start refusing arguments it actually reads.
	// This does not catch that — nothing static can — but it does catch a tool
	// whose schema failed to parse at all, which would silently accept
	// everything again.
	idx := buildArgIndex(catalogue(t))
	for _, tool := range catalogue(t) {
		if _, ok := idx[tool.Name]; !ok {
			t.Errorf("%s has no entry in the argument index", tool.Name)
		}
	}
}

func TestUnknownArgumentsAreNamed(t *testing.T) {
	idx := buildArgIndex(catalogue(t))

	// The case that motivated this: ui_tree takes depth, and max_depth was
	// accepted and ignored three calls running while the caller believed the
	// depth was changing.
	bad := idx.unknownArgs("ui_tree", map[string]any{"max_depth": 1})
	if len(bad) != 1 || bad[0] != "max_depth" {
		t.Fatalf("unknownArgs = %v, want [max_depth]", bad)
	}
	if got := idx.unknownArgs("ui_tree", map[string]any{"depth": 1}); len(got) != 0 {
		t.Fatalf("a declared argument was rejected: %v", got)
	}
}

func TestMetaIsNotAToolArgument(t *testing.T) {
	// _meta is the protocol's extension slot, not the tool's, and rejecting it
	// would break progress reporting for every tool at once.
	idx := buildArgIndex(catalogue(t))
	if got := idx.unknownArgs("wait", map[string]any{"ms": 5, "_meta": map[string]any{}}); len(got) != 0 {
		t.Fatalf("_meta was treated as a tool argument: %v", got)
	}
}

func TestToolsWithNoArgumentsRefuseAll(t *testing.T) {
	// An empty schema means no arguments, not any argument. Defaulting the
	// other way would leave exactly the tools with the simplest contracts
	// accepting anything.
	idx := buildArgIndex(catalogue(t))
	got := idx.unknownArgs("get_screen_info", map[string]any{"width": 100})
	if len(got) != 1 || got[0] != "width" {
		t.Fatalf("unknownArgs = %v, want [width]", got)
	}
}

func TestDeclaredListsWhatTheToolTakes(t *testing.T) {
	// The refusal quotes this, and a caller who is told only "bad argument"
	// has to go back to tools/list to find out which one they meant.
	idx := buildArgIndex(catalogue(t))
	names := idx.declared("wait")
	if len(names) != 1 || names[0] != "ms" {
		t.Fatalf("declared(wait) = %v, want [ms]", names)
	}
}
