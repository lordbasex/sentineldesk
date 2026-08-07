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

package mcpclient

// The client, against a server made of a pipe.
//
// These run anywhere: no desktop, no socket, no container. What they cover is
// the decisions this client makes about what the server said — which denial
// means retry, which event means stop, whether a cancellation reaches the wire
// — because those are the parts a runtime is built on and the parts that look
// fine while being wrong.
//
// `sentineldesk-agent doctor` covers the other half, against a real desktop.
// Neither replaces the other: one proves the client reads correctly, the other
// proves there is something to read.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is one end of a pipe, speaking JSON-RPC the way the real server
// does. Handlers are registered per method and answer on their own goroutine.
type fakeServer struct {
	t    *testing.T
	conn net.Conn
	enc  *json.Encoder

	mu       sync.Mutex
	handlers map[string]func(id *int64, params json.RawMessage)
}

func newFakeServer(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()

	f := &fakeServer{t: t, conn: serverEnd, enc: json.NewEncoder(serverEnd),
		handlers: map[string]func(*int64, json.RawMessage){}}

	// initialize and the handshake notification, so every test does not have
	// to restate them.
	f.on("initialize", func(id *int64, _ json.RawMessage) {
		f.reply(id, map[string]any{
			"protocolVersion": ProtocolVersion,
			"serverInfo":      map[string]any{"name": "fake", "version": "0"},
			"_meta":           map[string]any{"sentineldesk/connectionId": 7},
		})
	})
	f.on("notifications/initialized", func(*int64, json.RawMessage) {})

	go f.serve()
	c := New(clientEnd)
	t.Cleanup(func() { c.Close(); serverEnd.Close() })
	return c, f
}

func (f *fakeServer) on(method string, fn func(id *int64, params json.RawMessage)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[method] = fn
}

func (f *fakeServer) serve() {
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
		f.mu.Lock()
		fn := f.handlers[req.Method]
		f.mu.Unlock()
		if fn != nil {
			go fn(req.ID, req.Params)
		}
	}
}

func (f *fakeServer) reply(id *int64, result any) {
	raw, _ := json.Marshal(result)
	f.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": json.RawMessage(raw)})
}

func (f *fakeServer) notify(method string, params any) {
	f.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (f *fakeServer) write(msg map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = f.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = f.enc.Encode(msg)
}

// toolResult builds the shape the server returns from tools/call.
func toolResult(text string, isErr bool, denial Denial) map[string]any {
	out := map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isErr,
	}
	if denial != DenialNone {
		out["_meta"] = map[string]any{"sentineldesk/denial": string(denial)}
	}
	return out
}

func start(t *testing.T, c *Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx, "test", "0"); err != nil {
		t.Fatalf("start: %v", err)
	}
}

// --- what the server said ---------------------------------------------------

func TestHandshakeKeepsWhatTheServerSaid(t *testing.T) {
	c, _ := newFakeServer(t)
	start(t, c)

	if got := c.ServerInfo().Name; got != "fake" {
		t.Errorf("server name is %q", got)
	}
	// The connection id is how an emergency stop names this connection and no
	// others. Losing it means the only way to stop one agent is to stop them
	// all.
	if got := c.ConnectionID(); got != 7 {
		t.Errorf("connection id is %d, want 7", got)
	}
}

// TestARefusalIsNotAnError. A refused call comes back as a Result with a
// Denial, not as a Go error: the refusal carries the one field that says what
// to do next, and flattening it into an error throws that away.
func TestARefusalIsNotAnError(t *testing.T) {
	c, f := newFakeServer(t)
	f.on("tools/call", func(id *int64, _ json.RawMessage) {
		f.reply(id, toolResult("somebody else is driving", true, DenialRoom))
	})
	start(t, c)

	res, err := c.Call(context.Background(), "mouse_click", map[string]any{"x": 1, "y": 1})
	if err != nil {
		t.Fatalf("a refusal came back as a transport error: %v", err)
	}
	if !res.IsError || res.Denial != DenialRoom {
		t.Fatalf("IsError=%v Denial=%q", res.IsError, res.Denial)
	}
	if !res.Denial.Retryable() {
		t.Error("a room denial is not retryable, so the runtime would give up on \"wait your turn\"")
	}
}

// TestPolicyAndRoomAreNotTheSame is the distinction the denial kinds exist for.
// Confusing them turns "ask a person and try again" into "abandon the task", or
// worse, retries forever against a rule no one in the room can change.
func TestPolicyAndRoomAreNotTheSame(t *testing.T) {
	for _, tc := range []struct {
		denial    Denial
		retryable bool
	}{
		{DenialRoom, true},
		{DenialToolError, true},
		{DenialPolicy, false},
		{DenialUnknownTool, false},
		{DenialEmergency, false},
		{DenialBadArgs, false},
	} {
		if got := tc.denial.Retryable(); got != tc.retryable {
			t.Errorf("%s: retryable=%v, want %v", tc.denial, got, tc.retryable)
		}
	}
}

// --- being told -------------------------------------------------------------

// TestOnlyLosingControlInterruptsWork. Every control event says the controller
// changed; only one of them means the plan just became invalid. Treating them
// alike makes a runtime abandon a task the moment somebody HANDS it the desktop.
func TestOnlyLosingControlInterruptsWork(t *testing.T) {
	for _, tc := range []struct {
		change    ControlChange
		interrupt bool
	}{
		{TakenFromYou, true},
		{GrantedToYou, false},
		{Released, false},
		{Moved, false},
	} {
		e := Event{Topic: TopicControl, Change: tc.change}
		if got := e.InterruptsWork(); got != tc.interrupt {
			t.Errorf("%s: interrupts=%v, want %v", tc.change, got, tc.interrupt)
		}
	}
	// And nothing on another topic does, however alarming it sounds.
	if (Event{Topic: TopicWindows}).InterruptsWork() {
		t.Error("a window appearing was treated as losing the controls")
	}
}

func TestControlEventsReachTheHandler(t *testing.T) {
	c, f := newFakeServer(t)
	events := make(chan Event, 4)
	c.OnEvent = func(e Event) { events <- e }
	start(t, c)

	f.notify("notifications/sentineldesk/event", map[string]any{
		"topic": "control", "change": "taken_from_you",
		"controller": "u3", "controller_name": "Ana",
		"previous": "agent", "you_have_it": false,
	})

	select {
	case e := <-events:
		if !e.InterruptsWork() {
			t.Errorf("the agent lost the controls and the event does not say so: %+v", e)
		}
		if e.ControllerName != "Ana" {
			t.Errorf("controller name is %q", e.ControllerName)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event arrived")
	}
}

func TestProgressReachesTheHandler(t *testing.T) {
	c, f := newFakeServer(t)
	reports := make(chan Progress, 4)
	c.OnProgress = func(p Progress) { reports <- p }
	start(t, c)

	f.notify("notifications/progress", map[string]any{
		"progressToken": "call-2", "progress": 1, "message": "unpacking",
	})
	select {
	case p := <-reports:
		if p.Message != "unpacking" {
			t.Errorf("message is %q", p.Message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no progress arrived")
	}
}

// TestEveryCallAsksForProgress. The server sends none unless asked, so a client
// that never asks makes a long tool indistinguishable from a hung one — for as
// long as it takes.
func TestEveryCallAsksForProgress(t *testing.T) {
	c, f := newFakeServer(t)
	got := make(chan json.RawMessage, 1)
	f.on("tools/call", func(id *int64, params json.RawMessage) {
		got <- params
		f.reply(id, toolResult("ok", false, DenialNone))
	})
	start(t, c)

	if _, err := c.Call(context.Background(), "wait", map[string]any{"ms": 1}); err != nil {
		t.Fatalf("%v", err)
	}
	var sent struct {
		Meta map[string]any `json:"_meta"`
	}
	if err := json.Unmarshal(<-got, &sent); err != nil {
		t.Fatalf("%v", err)
	}
	if sent.Meta["progressToken"] == nil {
		t.Errorf("no progressToken in _meta: %v", sent.Meta)
	}
}

// --- provenance -------------------------------------------------------------

// TestProvenanceRidesOnEveryCall. The server groups the audit trail by these
// and cannot derive either: it sees what was called, not that seven calls were
// one job, and never why.
func TestProvenanceRidesOnEveryCall(t *testing.T) {
	c, f := newFakeServer(t)
	got := make(chan json.RawMessage, 1)
	f.on("tools/call", func(id *int64, params json.RawMessage) {
		got <- params
		f.reply(id, toolResult("ok", false, DenialNone))
	})
	start(t, c)
	c.SetTask("task-1", "install nginx and show it working")

	if _, err := c.Call(context.Background(), "wait", nil); err != nil {
		t.Fatalf("%v", err)
	}
	var sent struct {
		Meta map[string]any `json:"_meta"`
	}
	_ = json.Unmarshal(<-got, &sent)
	if sent.Meta["sentineldesk/taskId"] != "task-1" {
		t.Errorf("task id is %v", sent.Meta["sentineldesk/taskId"])
	}
	if sent.Meta["sentineldesk/goal"] != "install nginx and show it working" {
		t.Errorf("goal is %v", sent.Meta["sentineldesk/goal"])
	}
}

// --- stopping ---------------------------------------------------------------

// TestCancellingSendsItToTheServer is the difference between a cancel that
// stops work and one that only stops waiting for it. Without the notification
// the tool runs on with nobody left to answer — a package half installed and a
// runtime that has already moved on.
func TestCancellingSendsItToTheServer(t *testing.T) {
	c, f := newFakeServer(t)
	f.on("tools/call", func(*int64, json.RawMessage) {
		// Never answers, like a tool still running.
	})
	cancelled := make(chan json.RawMessage, 1)
	f.on("notifications/cancelled", func(_ *int64, params json.RawMessage) {
		cancelled <- params
	})
	start(t, c)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	_, err := c.Call(ctx, "run_command", map[string]any{"command": "sleep 60"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Call returned %v, want context.Canceled", err)
	}
	select {
	case params := <-cancelled:
		var p struct {
			RequestID int64 `json:"requestId"`
		}
		_ = json.Unmarshal(params, &p)
		if p.RequestID == 0 {
			t.Errorf("the cancellation names no request: %s", params)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the call was abandoned without telling the server, so the tool is still running")
	}
}

// TestClosingUnblocksEveryoneWaiting. A server that dies mid-call must not leave
// callers parked until their own timeouts, each reporting a timeout for what
// was a disconnection.
func TestClosingUnblocksEveryoneWaiting(t *testing.T) {
	c, f := newFakeServer(t)
	f.on("tools/call", func(*int64, json.RawMessage) {})
	start(t, c)

	done := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "wait", nil)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	f.conn.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a call on a closed connection reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the caller is still waiting on a connection that is gone")
	}
}

// --- reading the answer -----------------------------------------------------

// TestImagesAreNamedNotPasted. A screenshot is tens of kilobytes of base64, and
// the text a model reads should say one arrived, not contain it.
func TestImagesAreNamedNotPasted(t *testing.T) {
	res := Result{Content: []Content{
		{Type: "text", Text: "here it is"},
		{Type: "image", MimeType: "image/png", Data: strings.Repeat("A", 40000)},
	}}
	got := res.Text()
	if strings.Contains(got, "AAAA") {
		t.Error("base64 ended up in the text a model reads")
	}
	if !strings.Contains(got, "image/png") || !strings.Contains(got, "here it is") {
		t.Errorf("text is %q", got)
	}
}

// TestDeliversDistinguishesSubscribedFromDeliverable. A topic the server
// accepted but has no source for will never fire, and waiting on it forever is
// the failure the whole event channel was built to remove.
func TestDeliversDistinguishesSubscribedFromDeliverable(t *testing.T) {
	s := SubscribeResult{
		Subscribed:  []string{"control", "windows"},
		Unavailable: []string{"control"},
	}
	if s.Delivers(TopicControl) {
		t.Error("a topic with no source was reported as deliverable")
	}
	if !s.Delivers(TopicWindows) {
		t.Error("a topic with a source was reported as undeliverable")
	}
	if s.Delivers(TopicFocus) {
		t.Error("a topic never subscribed to was reported as deliverable")
	}
}
