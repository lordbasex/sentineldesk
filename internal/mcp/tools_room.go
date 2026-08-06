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

// Sharing the desktop with the people who are also using it.
//
// The agent used to be invisible and unaccountable in equal measure: it injected
// straight into X, so a person watching saw the pointer move on its own with no
// way to tell whether a colleague or the model was driving, and the agent had no
// way to find out that anybody was there at all.
//
// These tools close both halves. The agent joins the room as an ordinary
// participant — a name in the list, a marker on screen, a turn in the control
// rotation — and can read the room before acting.
//
// The rule that makes it work is one line:
//
//	Control is claimed, never assumed.
//
// That holds whether or not anybody is watching. An empty room used to be a
// free pass, on the reasoning that asking permission from nobody is theatre —
// but it left the room unable to say who had the desktop, and it meant the
// agent behaved differently depending on who happened to be connected.
// request_control answers instantly when the controls are free, so asking costs
// one call, and the room always knows the answer.
//
// Releasing hands the controls to nobody. Control goes FREE and stays there
// until somebody claims it, so "I have finished" never reads as "you are up".

import (
	"fmt"
	"time"

	"github.com/lordbasex/sentineldesk/internal/media"
	"github.com/lordbasex/sentineldesk/internal/stream"
)

// Rooms is what the MCP server needs from the room. An interface rather than
// the concrete type because a bridge process has no room at all, and the tools
// have to degrade politely instead of panicking.
type Rooms interface {
	JoinAgent(name string) string
	LeaveAgent()
	AskForControl(timeout time.Duration) (bool, string)
	Members() []stream.MemberInfo
	HumansPresent() bool
	Controller() (string, string)
	IsController(id string) bool
	TakeControl(id string) bool
	ReleaseControl(id string)
	UpdatePointer(id string, x, y int)

	// The live capture can be forwarded to an external destination without
	// encoding it a second time, so the agent uses the same path the toolbar
	// does rather than raising a capture of its own.
	StartRestream(t media.RestreamTarget) error
	StopRestream(id string) error
	Restreams() []media.RestreamInfo
	CanRestream() bool
}

// AgentID is the room identity of the MCP plane.
const AgentID = "agent"

func (s *Server) roomTools() []toolDef {
	return []toolDef{
		{
			Name: "room_state",
			Risk: riskRead,
			Description: "Who else is on this desktop right now: participants, " +
				"who holds control, and whether you may inject input. Call this " +
				"before acting when you might be sharing the session with a person.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name: "request_control",
			Risk: riskWrite,
			Description: "Ask the people watching for control of the desktop so you " +
				"can move the mouse and type. THEY DECIDE: a prompt appears on their " +
				"screen and this waits for the answer. No answer means no. With the " +
				"controls free — nobody driving, or nobody here at all — it is " +
				"granted immediately. Ask every time: control is never automatic.",
			InputSchema: schema(map[string]any{
				"timeout_ms": pInt("how long to wait for an answer (default 45000)"),
			}),
		},
		{
			Name: "release_control",
			Risk: riskWrite,
			Description: "Hand control back to the people watching. Do this when you " +
				"finish a task, so a person does not have to take it from you.",
			InputSchema: schema(map[string]any{}),
		},
	}
}

func (s *Server) callRoom(name string, args map[string]any) (any, bool, bool) {
	// Claim the three room tools and nothing else.
	//
	// The nil check used to come first and report handled for EVERY name, so a
	// Server built without SetRoom answered "this build has no room attached"
	// to screenshot, run_command and all the rest — dispatch tries this
	// dispatcher early and stops at the first one that claims the call. The
	// whole catalogue was dead, and the message blamed the room.
	//
	// It never fired in the daemon, which always calls SetRoom, which is why it
	// went unnoticed. It fires immediately anywhere else the server is
	// embedded, and it contradicts the rule the rest of this project follows:
	// an optional capability degrades, it does not take everything with it. No
	// room means unarbitrated, not unusable.
	switch name {
	case "room_state", "request_control", "release_control":
	default:
		return nil, false, false
	}
	if s.room == nil {
		return textContent("this build has no room attached; input is unarbitrated"), true, true
	}

	switch name {
	case "room_state":
		s.room.JoinAgent(s.agentName)
		humans := s.room.HumansPresent()
		ctlID, ctlName := s.room.Controller()
		members := s.room.Members()

		people := make([]map[string]any, 0, len(members))
		for _, m := range members {
			people = append(people, map[string]any{
				"id": m.ID, "name": m.Name,
				"controller": m.Controller, "agent": m.Agent,
				"seconds": m.Seconds,
			})
		}
		return map[string]any{
			"participants":     people,
			"humans_present":   humans,
			"controller":       ctlName,
			"controller_id":    ctlID,
			"you_have_control": s.room.IsController(AgentID),
			// Spelled out rather than left for the model to infer: the whole
			// point is that it should not have to guess whether it may act.
			"may_inject": s.room.IsController(AgentID),
			"note": "Control is always claimed, never assumed — even with nobody " +
				"else here. call request_control (immediate when the controls " +
				"are free) and release_control when the task is done.",
		}, false, true

	case "request_control":
		s.room.JoinAgent(s.agentName)
		// With nobody watching there is nobody to ask, and blocking on an empty
		// room would stall every unattended run.
		if !s.room.HumansPresent() {
			if !s.room.TakeControl(AgentID) {
				return textContent("could not take control"), true, true
			}
			return textContent("control taken (nobody else is here)"), false, true
		}
		timeout := time.Duration(argInt(args, "timeout_ms")) * time.Millisecond
		if timeout <= 0 {
			timeout = 45 * time.Second
		}
		granted, why := s.room.AskForControl(timeout)
		if !granted {
			return map[string]any{
				"granted": false, "reason": why,
				"hint": "a person has to allow it from their toolbar. You can keep " +
					"working on anything that does not need the mouse or keyboard, " +
					"or ask again later.",
			}, true, true
		}
		return map[string]any{"granted": true, "reason": why}, false, true

	case "release_control":
		if !s.room.IsController(AgentID) {
			return textContent("you did not hold control"), false, true
		}
		s.room.ReleaseControl(AgentID)
		return textContent("control released — the controls are free for whoever " +
			"claims them next"), false, true
	}
	return nil, false, false
}

// reportPointer tells the room where the agent's pointer is.
//
// While the agent holds control its pointer IS the X pointer, so nothing extra
// is drawn — the same rule that applies to a human controller. Keeping the room
// informed anyway means the marker is already correct the moment control moves
// somewhere else.
func (s *Server) reportPointer(x, y int) {
	if s.room != nil {
		s.room.UpdatePointer(AgentID, x, y)
	}
}

// mayInject decides whether an input tool is allowed to run.
//
// This is the one place the arbitration is enforced, so it cannot be forgotten
// on a new tool: everything that reaches XTEST goes through here.
func (s *Server) mayInject() error {
	if s.room == nil {
		return nil // no room: nothing to arbitrate with
	}
	s.room.JoinAgent(s.agentName)
	if s.room.IsController(AgentID) {
		return nil
	}
	// An empty room used to be a free pass. It is not one any more: control is
	// held or it is not, and whether anybody happens to be watching does not
	// change that. The agent alone still asks — request_control answers
	// immediately when nothing is driving, so it costs one call and buys a room
	// whose state always says who has the desktop.

	// Never taken implicitly, not even when the controls are free. An input
	// tool that quietly seizes control makes the acquisition invisible: the
	// agent ends up holding the desktop without ever having said it wanted it,
	// and keeps holding it, because nothing gives back what nothing asked for.
	//
	// request_control is the one door, and it is cheap — it grants at once when
	// nothing is driving. What it buys is that every handover is deliberate and
	// visible in the room.
	if id, _ := s.room.Controller(); id == "" {
		return fmt.Errorf(
			"nobody is driving, but control is not taken automatically — " +
				"call request_control (it is granted immediately when the " +
				"controls are free), and release_control when you are done")
	}
	_, who := s.room.Controller()
	if who == "" {
		who = "somebody else"
	}
	return fmt.Errorf(
		"a person is using this desktop and %s holds control — "+
			"call request_control first, or room_state to see who is here", who)
}
