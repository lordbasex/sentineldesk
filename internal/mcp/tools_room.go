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
// The rule that makes it work is narrow on purpose:
//
//	Control is required only when a human is present.
//
// With an empty room there is nobody to take turns with, and demanding that the
// agent ask permission from nobody would break every headless run for no gain.
// The moment somebody opens a browser, the agent has to ask.

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
			Description: "Who else is on this desktop right now: participants, " +
				"who holds control, and whether you may inject input. Call this " +
				"before acting when you might be sharing the session with a person.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name: "request_control",
			Description: "Ask the people watching for control of the desktop so you " +
				"can move the mouse and type. THEY DECIDE: a prompt appears on their " +
				"screen and this waits for the answer. No answer means no. With an " +
				"empty room you simply take it, since there is nobody to ask.",
			InputSchema: schema(map[string]any{
				"timeout_ms": pInt("how long to wait for an answer (default 45000)"),
			}),
		},
		{
			Name: "release_control",
			Description: "Hand control back to the people watching. Do this when you " +
				"finish a task, so a person does not have to take it from you.",
			InputSchema: schema(map[string]any{}),
		},
	}
}

func (s *Server) callRoom(name string, args map[string]any) (any, bool, bool) {
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
			"may_inject": !humans || s.room.IsController(AgentID),
			"note": "Input is arbitrated only while a human is connected. " +
				"With humans present, call request_control first.",
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
		_, ctlName := s.room.Controller()
		if ctlName == "" {
			return textContent("control released; nobody is driving"), false, true
		}
		return textContent("control released to %s", ctlName), false, true
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
	if !s.room.HumansPresent() {
		return nil // nobody watching: the desktop is yours
	}
	s.room.JoinAgent(s.agentName)
	if s.room.IsController(AgentID) {
		return nil
	}
	_, who := s.room.Controller()
	if who == "" {
		who = "somebody else"
	}
	return fmt.Errorf(
		"a person is using this desktop and %s holds control — "+
			"call request_control first, or room_state to see who is here", who)
}
