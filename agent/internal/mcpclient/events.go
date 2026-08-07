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

// The half of the protocol that arrives without being asked for.
//
// Everything else here is a question and an answer. These are the server
// speaking first, and there are only two kinds: progress from a call in flight,
// and events about the desktop and the room.
//
// The one that matters is control. An agent holding the controls can have them
// taken by a person at any moment — that is what a shared desktop is for — and
// before the event channel existed it found out by having its next injection
// refused. A denial where there should have been a notice, arriving in the
// middle of a plan built on the opposite assumption.
//
// So a runtime that ignores these is not merely less informed. It is one that
// will confidently keep typing into a desktop that is no longer its own.

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
)

// Progress is one report from a call that is still running.
type Progress struct {
	// Token is the caller's own handle, echoed back exactly as it was sent.
	Token   json.RawMessage
	Amount  float64
	Message string
}

// EventTopic is one thing the server can be asked to report.
type EventTopic string

const (
	// TopicControl — who is driving changed. The one a runtime cannot work
	// without.
	TopicControl EventTopic = "control"

	// TopicRoom — somebody joined or left.
	TopicRoom EventTopic = "room"

	// TopicWindows — a window appeared or closed, which is how a dialog
	// interrupts whatever was being done.
	TopicWindows EventTopic = "windows"

	TopicFocus   EventTopic = "focus"
	TopicDesktop EventTopic = "desktop"
)

// ControlChange names a transition, so a runtime does not have to work it out
// by comparing two ids. The distinction is not cosmetic: TakenFromYou and
// Released both end with somebody else driving, and only one of them means a
// plan just became invalid.
type ControlChange string

const (
	// TakenFromYou — a person took the controls while the agent held them.
	// Whatever the agent was in the middle of, it cannot finish it now.
	TakenFromYou ControlChange = "taken_from_you"

	// GrantedToYou — the agent now holds them.
	GrantedToYou ControlChange = "granted_to_you"

	// Released — nobody is driving.
	Released ControlChange = "released"

	// Moved — between two other participants; nothing to do with the agent.
	Moved ControlChange = "moved"
)

// Event is one unsolicited message from the server.
type Event struct {
	Topic EventTopic
	Raw   json.RawMessage

	// Control, populated for TopicControl.
	Change         ControlChange
	Controller     string
	ControllerName string
	Previous       string
	YouHaveIt      bool
}

// InterruptsWork reports whether this event invalidates whatever the agent was
// doing. Exactly one thing does: losing the controls mid-task.
//
// A method rather than a comment, because the check is easy to get subtly
// wrong — "the controller changed" is true when the agent is GIVEN the desktop
// too, and treating that as an interruption would make the runtime abandon a
// task the moment somebody helped it.
func (e Event) InterruptsWork() bool {
	return e.Topic == TopicControl && e.Change == TakenFromYou
}

const eventMethod = "notifications/sentineldesk/event"

// Subscribe asks to be told about the given topics. Nothing arrives until this
// is called, which is what makes the extension safe to ship to hosts that know
// nothing about it — and what makes calling it the runtime's job rather than
// something it inherits.
//
// Calling it again replaces the subscription rather than adding to it.
func (c *Client) Subscribe(ctx context.Context, topics ...EventTopic) (SubscribeResult, error) {
	args := map[string]any{}
	if len(topics) > 0 {
		names := make([]string, len(topics))
		for i, t := range topics {
			names[i] = string(t)
		}
		args["topics"] = names
	}
	res, err := c.Call(ctx, "subscribe_events", args)
	if err != nil {
		return SubscribeResult{}, err
	}
	var out SubscribeResult
	if res.IsError {
		return out, &rpcError{Code: -32001, Message: res.Text()}
	}
	// The tool answers in JSON as its text content, the same as every other
	// tool that returns structure.
	_ = json.Unmarshal([]byte(res.Text()), &out)
	return out, nil
}

// SubscribeResult is what the server agreed to.
type SubscribeResult struct {
	Subscribed []string `json:"subscribed"`
	Method     string   `json:"method"`

	// Unavailable names topics with no source on this desktop — no room, or no
	// X connection. They will never fire, and being told so beats waiting for
	// an event that was never coming.
	Unavailable []string `json:"unavailable"`
	Note        string   `json:"note"`
}

// Delivers reports whether a topic will actually produce events here.
func (s SubscribeResult) Delivers(topic EventTopic) bool {
	return slices.Contains(s.Subscribed, string(topic)) &&
		!slices.Contains(s.Unavailable, string(topic))
}

// handleNotification routes what the server sent unprompted.
//
// Handlers run on the reader's goroutine, so one that blocks stalls every reply
// behind it. Both are documented as "hand off and return", and the runtime's
// do exactly that.
func (c *Client) handleNotification(msg *response) {
	switch msg.Method {
	case "notifications/progress":
		if c.OnProgress == nil {
			return
		}
		var p struct {
			Token   json.RawMessage `json:"progressToken"`
			Amount  float64         `json:"progress"`
			Message string          `json:"message"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		c.OnProgress(Progress{Token: p.Token, Amount: p.Amount, Message: p.Message})

	case eventMethod:
		if c.OnEvent == nil {
			return
		}
		var raw struct {
			Topic          string `json:"topic"`
			Change         string `json:"change"`
			Controller     string `json:"controller"`
			ControllerName string `json:"controller_name"`
			Previous       string `json:"previous"`
			YouHaveIt      bool   `json:"you_have_it"`
		}
		if json.Unmarshal(msg.Params, &raw) != nil {
			return
		}
		c.OnEvent(Event{
			Topic: EventTopic(raw.Topic), Raw: msg.Params,
			Change:     ControlChange(raw.Change),
			Controller: raw.Controller, ControllerName: raw.ControllerName,
			Previous: raw.Previous, YouHaveIt: raw.YouHaveIt,
		})

	default:
		// An unknown notification is ignored rather than logged loudly. The
		// server may grow one before this client learns about it, and a client
		// that complains about the future is a client somebody silences.
		_ = strings.TrimSpace(msg.Method)
	}
}
