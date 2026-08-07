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

package provider

// A provider that answers from a script.
//
// Not a mock. It implements the same interface the real one does, so the loop
// under test is the loop that ships — nothing is stubbed out beneath it.
//
// It exists because the loop's behaviour has to be checked against things a
// real model would produce only by luck: a refusal it must retry, a refusal it
// must not, an interruption arriving between two calls of one batch. Against a
// live model those tests are flaky, and worse, a GOOD model hides loop defects
// by getting the right answer despite them.

import (
	"context"
	"fmt"
	"sync"
)

// Scripted returns prepared turns in order.
type Scripted struct {
	mu    sync.Mutex
	turns []Response
	at    int

	// Seen records every request, so a test can assert on what the loop said to
	// the model — which is how the interruption notice and the role's system
	// prompt are checked at all.
	Seen []Request
}

// NewScripted builds a provider that will answer with these turns, in order.
func NewScripted(turns ...Response) *Scripted { return &Scripted{turns: turns} }

func (s *Scripted) Name() string { return "scripted" }

func (s *Scripted) Complete(_ context.Context, req Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Seen = append(s.Seen, req)
	if s.at >= len(s.turns) {
		// Louder than returning an empty turn. A loop that asked for one turn
		// more than the script has is a loop doing something the test did not
		// predict, and that is the finding.
		return Response{}, fmt.Errorf("the script has %d turns and the loop asked for %d",
			len(s.turns), s.at+1)
	}
	out := s.turns[s.at]
	s.at++
	return out, nil
}

// Turns returns how many the loop actually took.
func (s *Scripted) Turns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at
}

// --- builders, so a script reads as what it means ---------------------------

// Says builds a turn that ends the run with a sentence.
func Says(text string) Response {
	return Response{Message: Message{Role: RoleAssistant, Text: text}, Stop: StopEnd}
}

// Calls builds a turn that asks for tools to be run.
func Calls(calls ...ToolCall) Response {
	return Response{
		Message: Message{Role: RoleAssistant, ToolCalls: calls},
		Stop:    StopToolUse,
	}
}

// Use names one tool call. The id only has to be unique within a turn.
func Use(id, name string, args map[string]any) ToolCall {
	return ToolCall{ID: id, Name: name, Args: args}
}
