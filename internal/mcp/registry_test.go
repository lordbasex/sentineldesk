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
	"encoding/json"
	"net"
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

func (c *session) call(method string, params any) map[string]any {
	c.t.Helper()
	c.id++
	req := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method}
	if params != nil {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("%v", err)
	}
	_ = c.conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.conn.Write(append(line, '\n')); err != nil {
		c.t.Fatalf("write %s: %v", method, err)
	}
	raw, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("read %s: %v", method, err)
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		c.t.Fatalf("decode %s: %v", method, err)
	}
	if resp.Error != nil {
		c.t.Fatalf("%s: %s", method, resp.Error.Message)
	}
	return resp.Result
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

func TestEveryToolHasACategory(t *testing.T) {
	for _, tool := range catalogue(t) {
		if categoryOf(tool.Name) == "" {
			t.Errorf("%s has no category", tool.Name)
		}
	}
}
