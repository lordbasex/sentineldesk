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

package loop

// The loop, against a scripted model and a fake server.
//
// Everything here is deterministic on purpose. The behaviours that matter — a
// refusal it must retry, a refusal it must not, an interruption landing between
// two calls of one batch, a tool swapped because somebody is watching — are
// things a real model produces only by luck, and a GOOD model hides loop
// defects by reaching the right answer despite them. Testing the loop against a
// live model measures the model.

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lordbasex/sentineldesk/agent/internal/mcpclient"
	"github.com/lordbasex/sentineldesk/agent/internal/provider"
)

// fakeMCP answers tools/call from a table, and records what it was asked.
type fakeMCP struct {
	t    *testing.T
	conn net.Conn
	enc  *json.Encoder

	mu      sync.Mutex
	answers map[string]map[string]any // tool -> result payload
	called  []string
}

func newFakeMCP(t *testing.T) (*mcpclient.Client, *fakeMCP) {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()
	f := &fakeMCP{t: t, conn: serverEnd, enc: json.NewEncoder(serverEnd),
		answers: map[string]map[string]any{}}

	go f.serve()
	c := mcpclient.New(clientEnd)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx, "loop-test", "0"); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { c.Close(); serverEnd.Close() })
	return c, f
}

// answer sets what a tool returns.
func (f *fakeMCP) answer(tool, text string, isErr bool, denial mcpclient.Denial) {
	out := map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isErr,
	}
	if denial != mcpclient.DenialNone {
		out["_meta"] = map[string]any{"sentineldesk/denial": string(denial)}
	}
	f.mu.Lock()
	f.answers[tool] = out
	f.mu.Unlock()
}

func (f *fakeMCP) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.called...)
}

func (f *fakeMCP) serve() {
	dec := json.NewDecoder(f.conn)
	for {
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := dec.Decode(&req); err != nil {
			return
		}
		switch req.Method {
		case "initialize":
			f.reply(req.ID, map[string]any{
				"protocolVersion": mcpclient.ProtocolVersion,
				"serverInfo":      map[string]any{"name": "fake", "version": "0"},
			})
		case "tools/call":
			var p struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &p)
			f.mu.Lock()
			f.called = append(f.called, p.Name)
			out, ok := f.answers[p.Name]
			f.mu.Unlock()
			if !ok {
				out = map[string]any{
					"content": []any{map[string]any{"type": "text", "text": "ok"}},
					"isError": false,
				}
			}
			f.reply(req.ID, out)
		}
	}
}

func (f *fakeMCP) reply(id *int64, result any) {
	raw, _ := json.Marshal(result)
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = f.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": json.RawMessage(raw)})
}

func catalogue(names ...string) []mcpclient.Tool {
	out := make([]mcpclient.Tool, len(names))
	for i, n := range names {
		out[i] = mcpclient.Tool{Name: n, Description: n}
	}
	return out
}

// --- the shape of a run -----------------------------------------------------

func TestARunCallsToolsAndFinishes(t *testing.T) {
	c, f := newFakeMCP(t)
	f.answer("screenshot", "[image/png]", false, mcpclient.DenialNone)

	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "screenshot", nil)),
		provider.Says("There is a terminal open."),
	)
	r := New(c, Options{Model: model, Tools: catalogue("screenshot")})

	res, err := r.Run(context.Background(), "what is on screen?")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if res.Answer != "There is a terminal open." {
		t.Errorf("answer is %q", res.Answer)
	}
	if res.Turns != 2 || res.Calls != 1 {
		t.Errorf("%d turns, %d calls", res.Turns, res.Calls)
	}
	if got := f.calls(); len(got) != 1 || got[0] != "screenshot" {
		t.Errorf("the server was asked for %v", got)
	}
}

// TestSeveralCallsInOneTurnCostOneRoundTrip. The cost model is round trips
// first (§3.3): a turn asking for three tools must run all three and answer
// once, not become three turns.
func TestSeveralCallsInOneTurnCostOneRoundTrip(t *testing.T) {
	c, _ := newFakeMCP(t)
	model := provider.NewScripted(
		provider.Calls(
			provider.Use("1", "list_windows", nil),
			provider.Use("2", "get_active_window", nil),
			provider.Use("3", "room_state", nil),
		),
		provider.Says("done"),
	)
	r := New(c, Options{Model: model,
		Tools: catalogue("list_windows", "get_active_window", "room_state")})

	res, err := r.Run(context.Background(), "orient yourself")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if res.Calls != 3 {
		t.Errorf("%d calls, want 3", res.Calls)
	}
	if res.Turns != 2 {
		t.Errorf("%d turns for three calls in one batch, want 2", res.Turns)
	}
	// And all three results come back in one message, paired to their calls.
	last := model.Seen[len(model.Seen)-1]
	final := last.Messages[len(last.Messages)-1]
	if len(final.Results) != 3 {
		t.Errorf("the model was handed %d results, want 3", len(final.Results))
	}
}

// --- refusals ---------------------------------------------------------------

// TestARoomRefusalTellsTheModelToAsk. This is the distinction the denial kinds
// exist for, seen from the loop: "wait your turn" must not read as "give up".
func TestARoomRefusalTellsTheModelToAsk(t *testing.T) {
	c, f := newFakeMCP(t)
	f.answer("mouse_click", "somebody else is driving", true, mcpclient.DenialRoom)

	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "mouse_click", map[string]any{"x": 1, "y": 1})),
		provider.Says("asked for control"),
	)
	r := New(c, Options{Model: model, Tools: catalogue("mouse_click", "request_control")})
	if _, err := r.Run(context.Background(), "click"); err != nil {
		t.Fatalf("%v", err)
	}

	handed := lastResultText(t, model)
	if !strings.Contains(handed, "request_control") {
		t.Errorf("a room refusal did not tell the model how to proceed: %q", handed)
	}
}

// TestAPolicyRefusalTellsTheModelToStop. The opposite advice, and getting it
// wrong is worse: the model would ask a person for something no person in the
// room can grant, and then retry forever.
func TestAPolicyRefusalTellsTheModelToStop(t *testing.T) {
	c, f := newFakeMCP(t)
	f.answer("run_command", "denied by the server policy", true, mcpclient.DenialPolicy)

	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "run_command", map[string]any{"command": "id"})),
		provider.Says("cannot"),
	)
	r := New(c, Options{Model: model, Tools: catalogue("run_command")})
	if _, err := r.Run(context.Background(), "run id"); err != nil {
		t.Fatalf("%v", err)
	}

	handed := lastResultText(t, model)
	if strings.Contains(handed, "request_control") {
		t.Errorf("a policy refusal suggested asking a person: %q", handed)
	}
	if !strings.Contains(handed, "Do not retry") {
		t.Errorf("a policy refusal did not say to stop: %q", handed)
	}
}

// --- being interrupted ------------------------------------------------------

// TestLosingTheControlsStopsTheRun. The case the event channel exists for.
func TestLosingTheControlsStopsTheRun(t *testing.T) {
	c, _ := newFakeMCP(t)
	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "type_text", map[string]any{"text": "hello"})),
		provider.Says("this turn should never be reached"),
	)
	r := New(c, Options{Model: model, Tools: catalogue("type_text")})

	// Arrives while the first batch is running, which is when it really would.
	go func() {
		time.Sleep(50 * time.Millisecond)
		r.NoteEvent(mcpclient.Event{
			Topic: mcpclient.TopicControl, Change: mcpclient.TakenFromYou,
			Controller: "u3", ControllerName: "Ana",
		})
	}()
	time.Sleep(60 * time.Millisecond)

	res, err := r.Run(context.Background(), "type something")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !res.Interrupted {
		t.Fatal("the controls were taken and the run did not notice")
	}
	if !strings.Contains(res.Answer, "Ana") {
		t.Errorf("the run does not say who took them: %q", res.Answer)
	}
	if model.Turns() != 1 {
		t.Errorf("the model was asked for %d turns after being interrupted", model.Turns())
	}
}

// TestBeingGivenTheControlsIsNotAnInterruption is the mistake this is easy to
// make: every control event says the controller changed, and stopping on all of
// them abandons a task the moment somebody HANDS the agent the desktop.
func TestBeingGivenTheControlsIsNotAnInterruption(t *testing.T) {
	c, _ := newFakeMCP(t)
	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "screenshot", nil)),
		provider.Says("carried on"),
	)
	r := New(c, Options{Model: model, Tools: catalogue("screenshot")})
	r.NoteEvent(mcpclient.Event{
		Topic: mcpclient.TopicControl, Change: mcpclient.GrantedToYou, YouHaveIt: true,
	})

	res, err := r.Run(context.Background(), "look")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if res.Interrupted {
		t.Fatal("being given the controls was treated as losing them")
	}
	if res.Answer != "carried on" {
		t.Errorf("answer is %q", res.Answer)
	}
}

// --- roles ------------------------------------------------------------------

// TestWitnessedSubstitutesTheVisibleTool. The role mechanism, enforced by the
// runtime rather than requested of the model: evidence cannot depend on a model
// remembering to be observable.
func TestWitnessedSubstitutesTheVisibleTool(t *testing.T) {
	c, f := newFakeMCP(t)
	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "run_command", map[string]any{"command": "ls"})),
		provider.Says("listed"),
	)
	r := New(c, Options{Model: model, Role: RoleWitnessed,
		Tools: catalogue("run_command", "terminal_run")})

	if _, err := r.Run(context.Background(), "list the files"); err != nil {
		t.Fatalf("%v", err)
	}
	got := f.calls()
	if len(got) != 1 || got[0] != "terminal_run" {
		t.Errorf("under witnessed the server was asked for %v, want terminal_run", got)
	}
}

// TestEfficientLeavesTheToolAlone. The substitution is for when somebody asked
// to see it; making a desktop flicker for every package install is theatre.
func TestEfficientLeavesTheToolAlone(t *testing.T) {
	c, f := newFakeMCP(t)
	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "run_command", map[string]any{"command": "ls"})),
		provider.Says("listed"),
	)
	r := New(c, Options{Model: model, Role: RoleEfficient,
		Tools: catalogue("run_command", "terminal_run")})

	if _, err := r.Run(context.Background(), "list the files"); err != nil {
		t.Fatalf("%v", err)
	}
	if got := f.calls(); got[0] != "run_command" {
		t.Errorf("efficient substituted anyway: %v", got)
	}
}

// TestWitnessedDoesNotSubstituteWhatIsNotOnOffer. Swapping in a tool this
// connection may not call would turn a working step into a refusal, which is
// worse than doing it invisibly.
func TestWitnessedDoesNotSubstituteWhatIsNotOnOffer(t *testing.T) {
	c, f := newFakeMCP(t)
	model := provider.NewScripted(
		provider.Calls(provider.Use("1", "run_command", map[string]any{"command": "ls"})),
		provider.Says("listed"),
	)
	// A readonly-ish connection: terminal_run is not in the catalogue.
	r := New(c, Options{Model: model, Role: RoleWitnessed, Tools: catalogue("run_command")})

	if _, err := r.Run(context.Background(), "list the files"); err != nil {
		t.Fatalf("%v", err)
	}
	if got := f.calls(); got[0] != "run_command" {
		t.Errorf("it substituted a tool that was not on offer: %v", got)
	}
}

// TestWitnessedSaysSoInThePrompt. The substitution handles the one pair we
// know; the prompt is what covers everything else.
func TestWitnessedSaysSoInThePrompt(t *testing.T) {
	c, _ := newFakeMCP(t)
	model := provider.NewScripted(provider.Says("ok"))
	r := New(c, Options{Model: model, Role: RoleWitnessed, Tools: catalogue("screenshot")})
	if _, err := r.Run(context.Background(), "do something"); err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(model.Seen[0].System, "SEE this happen") {
		t.Error("the witnessed role did not reach the system prompt")
	}
}

// --- bounds -----------------------------------------------------------------

// TestARunThatWillNotFinishStops. Not a safety property — cancellation is that —
// but a loop going nowhere should say so rather than spend somebody's credit
// discovering it.
func TestARunThatWillNotFinishStops(t *testing.T) {
	c, _ := newFakeMCP(t)
	turns := make([]provider.Response, 10)
	for i := range turns {
		turns[i] = provider.Calls(provider.Use("1", "screenshot", nil))
	}
	model := provider.NewScripted(turns...)
	r := New(c, Options{Model: model, Tools: catalogue("screenshot"), MaxTurns: 3})

	res, err := r.Run(context.Background(), "loop forever")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if res.Turns != 3 {
		t.Errorf("%d turns, want 3", res.Turns)
	}
	if !strings.Contains(res.Answer, "without finishing") {
		t.Errorf("it did not say why it stopped: %q", res.Answer)
	}
}

// TestATruncatedAnswerSaysSo. Running out of output room mid-thought is not the
// same as finishing, and a loop that reports both as done presents half a plan
// as a whole one.
func TestATruncatedAnswerSaysSo(t *testing.T) {
	c, _ := newFakeMCP(t)
	model := provider.NewScripted(provider.Response{
		Message: provider.Message{Role: provider.RoleAssistant, Text: "First I will"},
		Stop:    provider.StopLength,
	})
	r := New(c, Options{Model: model, Tools: catalogue("screenshot")})

	res, err := r.Run(context.Background(), "plan something long")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(res.Answer, "ran out of output room") {
		t.Errorf("a truncated answer was reported as complete: %q", res.Answer)
	}
}

// lastResultText returns the text of the last tool result handed to the model.
func lastResultText(t *testing.T, model *provider.Scripted) string {
	t.Helper()
	last := model.Seen[len(model.Seen)-1]
	for i := len(last.Messages) - 1; i >= 0; i-- {
		if len(last.Messages[i].Results) > 0 {
			return last.Messages[i].Results[0].Text
		}
	}
	t.Fatal("no tool result reached the model")
	return ""
}
