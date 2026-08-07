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

// The event channel, over the wire.
//
// These go through serve() rather than calling the hub directly, because the
// thing being tested is that a notification the client never asked for arrives
// on the same socket as the replies, interleaved with them, and is recognisable
// when it does. A unit test of eventHub.publish would prove none of that.
//
// The room here is a fake with a controller that the test moves by hand. That
// is the whole scenario: the agent is driving, a person takes the controls, and
// the agent has to be told rather than finding out when its next click fails.

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lordbasex/sentineldesk/internal/stream"
)

// movableRoom is a Rooms whose controller can be changed from the test, with
// the presence watchers the real Room calls on every change.
type movableRoom struct {
	Rooms

	mu         sync.Mutex
	controller string
	name       string
	subs       map[int]func()
	seq        int

	// What ask_human put to the room, and what to answer with.
	asked        string
	askedOptions []string
	reply        string
	replyErr     error
}

func newMovableRoom(controller, name string) *movableRoom {
	return &movableRoom{controller: controller, name: name, subs: map[int]func(){}}
}

func (r *movableRoom) JoinAgent(string) string { return AgentID }

func (r *movableRoom) Controller() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controller, r.name
}

func (r *movableRoom) IsController(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controller == id
}

func (r *movableRoom) HumansPresent() bool { return true }

func (r *movableRoom) Members() []stream.MemberInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []stream.MemberInfo{
		{ID: AgentID, Name: "AI agent", Controller: r.controller == AgentID},
		{ID: "viewer-1", Name: "Ana", Controller: r.controller == "viewer-1"},
	}
}

func (r *movableRoom) WatchPresence(fn func()) func() {
	r.mu.Lock()
	id := r.seq
	r.seq++
	r.subs[id] = fn
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.subs, id)
		r.mu.Unlock()
	}
}

// moveControl is what a person clicking "take control" does to the real room.
func (r *movableRoom) moveControl(to, name string) {
	r.mu.Lock()
	r.controller, r.name = to, name
	subs := make([]func(), 0, len(r.subs))
	for _, fn := range r.subs {
		subs = append(subs, fn)
	}
	r.mu.Unlock()
	for _, fn := range subs {
		fn()
	}
}

// awaitEvent reads messages until one is a sentineldesk event on the given
// topic, or the deadline passes. Replies to earlier requests are skipped: the
// point of an unsolicited notification is that it arrives whenever it arrives.
func (c *session) awaitEvent(topic string, within time.Duration) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		_ = c.conn.SetDeadline(deadline)
		msg := c.readMessage()
		if msg["method"] != eventMethod {
			continue
		}
		params, _ := msg["params"].(map[string]any)
		if params["topic"] == topic {
			return params
		}
	}
	c.t.Fatalf("no %q event arrived within %v", topic, within)
	return nil
}

// subscribeTo calls the tool and returns what it says it subscribed to.
func (c *session) subscribeTo(topics ...string) map[string]any {
	c.t.Helper()
	args := map[string]any{}
	if len(topics) > 0 {
		list := make([]any, len(topics))
		for i, t := range topics {
			list[i] = t
		}
		args["topics"] = list
	}
	res := c.call("tools/call", map[string]any{"name": "subscribe_events", "arguments": args})
	if isErr, _ := res["isError"].(bool); isErr {
		c.t.Fatalf("subscribe_events failed: %v", res["content"])
	}
	return decodeJSONContent(c.t, res)
}

// decodeJSONContent pulls the JSON object a tool returned as its text content.
func decodeJSONContent(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in %v", res)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("content is not JSON: %v\n%s", err, text)
	}
	return out
}

// TestControlTakenAwayReachesTheAgent is the case this whole file exists for.
// The agent is driving, a person takes the controls, and the agent finds out
// because it was told — not because its next injection was refused.
func TestControlTakenAwayReachesTheAgent(t *testing.T) {
	room := newMovableRoom(AgentID, "AI agent")
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)

	got := c.subscribeTo("control")
	subs, _ := got["subscribed"].([]any)
	if len(subs) != 1 || subs[0] != "control" {
		t.Fatalf("subscribed to %v, want [control]", got["subscribed"])
	}

	room.moveControl("viewer-1", "Ana")

	ev := c.awaitEvent("control", 5*time.Second)
	// The named transition is the field an agent can act on without comparing
	// two ids itself, and "taken from you" is a different situation from "you
	// released it" even though both end with somebody else driving.
	if ev["change"] != "taken_from_you" {
		t.Errorf("change is %q, want \"taken_from_you\": %v", ev["change"], ev)
	}
	if ev["controller"] != "viewer-1" {
		t.Errorf("controller is %v, want viewer-1", ev["controller"])
	}
	if ev["previous"] != AgentID {
		t.Errorf("previous is %v, want %s", ev["previous"], AgentID)
	}
	if held, _ := ev["you_have_it"].(bool); held {
		t.Error("the event says the agent still has the controls it just lost")
	}
	if ev["controller_name"] != "Ana" {
		t.Errorf("controller_name is %v, want Ana", ev["controller_name"])
	}
}

// TestControlGrantedIsDistinctFromTaken covers the other direction, because an
// agent that treats every control event as a loss would stop working the moment
// somebody handed it the desktop.
func TestControlGrantedIsDistinctFromTaken(t *testing.T) {
	room := newMovableRoom("viewer-1", "Ana")
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)
	c.subscribeTo("control")

	room.moveControl(AgentID, "AI agent")

	ev := c.awaitEvent("control", 5*time.Second)
	if ev["change"] != "granted_to_you" {
		t.Errorf("change is %q, want \"granted_to_you\": %v", ev["change"], ev)
	}
	if held, _ := ev["you_have_it"].(bool); !held {
		t.Error("the agent was given the controls and the event says otherwise")
	}
}

// TestNothingArrivesBeforeSubscribing is the property that makes it safe to
// publish this to every client: a host that does not know about the extension
// asks for nothing and is sent nothing.
func TestNothingArrivesBeforeSubscribing(t *testing.T) {
	room := newMovableRoom(AgentID, "AI agent")
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)

	room.moveControl("viewer-1", "Ana")
	time.Sleep(300 * time.Millisecond)

	// A request whose reply proves the socket is working. If an event had been
	// sent it would be sitting in front of this answer.
	c.send("ping", nil)
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	msg := c.readMessage()
	if msg["method"] == eventMethod {
		t.Fatalf("an event arrived for a client that never subscribed: %v", msg["params"])
	}
	if _, ok := msg["result"]; !ok {
		t.Fatalf("expected the ping's reply, got %v", msg)
	}
}

// TestUnsubscribeStopsEvents — the subscription has to be revocable, or an
// agent that finishes a task is stuck with a stream it no longer reads.
func TestUnsubscribeStopsEvents(t *testing.T) {
	room := newMovableRoom(AgentID, "AI agent")
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)
	c.subscribeTo("control")

	// One event first, so the test proves the channel was live before it was
	// closed rather than never having worked.
	room.moveControl("viewer-1", "Ana")
	c.awaitEvent("control", 5*time.Second)

	res := c.call("tools/call", map[string]any{"name": "unsubscribe_events"})
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("unsubscribe_events failed: %v", res["content"])
	}

	room.moveControl(AgentID, "AI agent")
	time.Sleep(300 * time.Millisecond)

	c.send("ping", nil)
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	msg := c.readMessage()
	if msg["method"] == eventMethod {
		t.Fatalf("an event arrived after unsubscribing: %v", msg["params"])
	}
}

// TestResubscribingReplacesRatherThanAdds. Two subscriptions to the same source
// would deliver every event twice, and the leak would be invisible until an
// agent that re-subscribed on each task found its log filling up.
func TestResubscribingReplacesRatherThanAdds(t *testing.T) {
	room := newMovableRoom(AgentID, "AI agent")
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)

	c.subscribeTo("control")
	c.subscribeTo("control")
	c.subscribeTo("control")

	room.moveControl("viewer-1", "Ana")
	c.awaitEvent("control", 5*time.Second)

	// A second control event would be a duplicate of the first. The ping's
	// reply is the marker for "nothing else was queued".
	c.send("ping", nil)
	_ = c.conn.SetDeadline(time.Now().Add(5 * time.Second))
	for {
		msg := c.readMessage()
		if _, ok := msg["result"]; ok {
			return
		}
		if msg["method"] == eventMethod {
			params, _ := msg["params"].(map[string]any)
			if params["topic"] == "control" {
				t.Fatalf("the same control change was delivered twice: %v", params)
			}
		}
	}
}

// TestSubscribeRejectsUnknownTopics. Silently ignoring a topic it does not
// recognise would leave the agent waiting forever on an event that was never
// going to come, which is the exact failure this feature removes.
func TestSubscribeRejectsUnknownTopics(t *testing.T) {
	s := testServer(t)
	s.SetRoom(newMovableRoom(AgentID, "AI agent"), "AI agent")
	c := newSession(t, s)

	res := c.call("tools/call", map[string]any{
		"name":      "subscribe_events",
		"arguments": map[string]any{"topics": []any{"control", "telepathy"}},
	})
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("an unknown topic was accepted: %v", res)
	}
}

// TestSubscribeSaysWhatItCannotDeliver. A server with no room cannot report
// control changes; claiming the subscription succeeded would be the same
// silence in a different place.
func TestSubscribeSaysWhatItCannotDeliver(t *testing.T) {
	c := newSession(t, testServer(t)) // no room, no display
	got := c.subscribeTo("control", "room")

	unavailable, _ := got["unavailable"].([]any)
	if len(unavailable) != 2 {
		t.Fatalf("a server with no room reported %v as undeliverable, want both topics",
			got["unavailable"])
	}
}

// TestEventsDieWithTheConnection. The subscription holds a watcher on the room
// and, on a real desktop, on X. A client that goes away without unsubscribing
// is the normal case, not the exception.
func TestEventsDieWithTheConnection(t *testing.T) {
	room := newMovableRoom(AgentID, "AI agent")
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)
	c.subscribeTo("control")

	room.mu.Lock()
	live := len(room.subs)
	room.mu.Unlock()
	if live != 1 {
		t.Fatalf("%d presence watchers after subscribing, want 1", live)
	}

	c.conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		room.mu.Lock()
		live = len(room.subs)
		room.mu.Unlock()
		if live == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the connection closed and %d presence watcher(s) are still registered", live)
}

// --- asking a person ------------------------------------------------------------

func (r *movableRoom) AskHuman(text string, options []string, timeout time.Duration) (string, error) {
	r.mu.Lock()
	r.asked = text
	r.askedOptions = options
	reply, fail := r.reply, r.replyErr
	r.mu.Unlock()
	if fail != nil {
		return "", fail
	}
	return reply, nil
}

// TestAskHumanReturnsTheAnswer. The straightforward case, over the wire, so the
// tool's shape is what a runtime will actually parse.
func TestAskHumanReturnsTheAnswer(t *testing.T) {
	room := newMovableRoom(AgentID, "AI agent")
	room.reply = "the second one"
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)

	res := c.call("tools/call", map[string]any{
		"name": "ask_human",
		"arguments": map[string]any{
			"question": "which invoice did you mean?",
			"options":  []any{"the first one", "the second one"},
		},
	})
	if isErr, _ := res["isError"].(bool); isErr {
		t.Fatalf("ask_human failed: %v", res["content"])
	}
	got := decodeJSONContent(t, res)
	if answered, _ := got["answered"].(bool); !answered {
		t.Errorf("answered is false: %v", got)
	}
	if got["answer"] != "the second one" {
		t.Errorf("answer is %v", got["answer"])
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.asked != "which invoice did you mean?" {
		t.Errorf("the room was asked %q", room.asked)
	}
	if len(room.askedOptions) != 2 {
		t.Errorf("options reached the room as %v", room.askedOptions)
	}
}

// TestSilenceIsNotAnAnswer is the one that matters.
//
// A tool that returned a default on timeout would make "nobody was looking"
// indistinguishable from "somebody chose this", and the entire reason to ask is
// that the answer was not the agent's to assume. It has to come back as a
// failure, and the failure has to say so.
func TestSilenceIsNotAnAnswer(t *testing.T) {
	room := newMovableRoom(AgentID, "AI agent")
	room.replyErr = errors.New("nobody answered in 2s")
	s := testServer(t)
	s.SetRoom(room, "AI agent")
	c := newSession(t, s)

	res := c.call("tools/call", map[string]any{
		"name":      "ask_human",
		"arguments": map[string]any{"question": "shall I delete it?"},
	})
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Fatalf("a question nobody answered came back as a success: %v", res)
	}
	got := decodeJSONContent(t, res)
	if answered, _ := got["answered"].(bool); answered {
		t.Error("answered is true for a question nobody answered")
	}
	if _, present := got["answer"]; present {
		t.Errorf("an unanswered question came back with an answer: %v", got)
	}
}

// TestAskHumanNeedsAQuestion. An empty prompt on somebody's screen is worse than
// no prompt: they cannot answer it and they cannot tell what went wrong.
func TestAskHumanNeedsAQuestion(t *testing.T) {
	s := testServer(t)
	s.SetRoom(newMovableRoom(AgentID, "AI agent"), "AI agent")
	c := newSession(t, s)

	res := c.call("tools/call", map[string]any{
		"name": "ask_human", "arguments": map[string]any{"question": "   "}})
	if isErr, _ := res["isError"].(bool); !isErr {
		t.Errorf("a blank question was accepted: %v", res)
	}
}
