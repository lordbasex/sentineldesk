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

// Package provider is the model behind the loop.
//
// One interface, not a fork per vendor. Everything above it — the loop, the
// roles, tool selection — is written against Complete and knows nothing about
// who answers, which is what makes a scripted provider a real provider rather
// than a mock: the loop under test is the loop that ships.
//
// HTTP only, which is part of why this binary needs no CGO.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Role is who said something.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn. A turn from the assistant may contain text, tool calls,
// or both; a turn from the user may contain text or the results of tool calls.
type Message struct {
	Role      Role
	Text      string
	ToolCalls []ToolCall
	Results   []ToolResult
}

// ToolCall is the model asking for a tool to be run.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResult is what came back, paired to the call by ID.
type ToolResult struct {
	CallID string
	Text   string
	IsErr  bool

	// Images the tool returned. Sent only to a provider whose Capabilities say
	// Vision; the rest get Text, which names the mime type where a picture was.
	Images []Image
}

// Image is a picture, as MCP delivers one.
type Image struct {
	MimeType string
	Data     string // base64
}

// Tool is what the model is told it can call. It is built from the MCP
// catalogue, so the schema is the server's own — there is no second description
// of a tool to drift from the first.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// Request is one turn's input.
type Request struct {
	System    string
	Messages  []Message
	Tools     []Tool
	MaxTokens int

	// CacheStable says the system prompt and the tool catalogue will be
	// byte-identical on the next turn of this conversation, so a provider that
	// can cache them should.
	//
	// A hint about the SHAPE of the request rather than an instruction to any
	// particular API, because there is no standard here and there is not going
	// to be one. Anthropic wants explicit markers on the blocks to cache;
	// OpenAI caches long prefixes automatically and wants nothing; Gemini wants
	// a cached-content object with its own lifetime; a local model has a KV
	// cache and none of the above. What they all reward is the same thing —
	// the stable part first, unchanged — so that is what gets expressed here
	// and each adapter does what its provider understands.
	//
	// It is a hint and never a promise. A provider that ignores it is correct,
	// just more expensive.
	CacheStable bool
}

// StopReason is why the model stopped.
type StopReason string

const (
	// StopEnd — it finished its turn with nothing more to say.
	StopEnd StopReason = "end"

	// StopToolUse — it wants tools run and the results handed back. The loop's
	// normal state, not an exception.
	StopToolUse StopReason = "tool_use"

	// StopLength — it hit the output limit mid-thought. Distinct from StopEnd
	// because the answer is truncated rather than complete, and a loop that
	// treats them alike reports a half-finished plan as a finished one.
	StopLength StopReason = "length"
)

// Response is one turn's output.
type Response struct {
	Message    Message
	Stop       StopReason
	InputToks  int
	OutputToks int

	// CacheWriteToks and CacheReadToks are tokens the provider billed at a
	// different rate because of caching: written into a cache this turn, or
	// read from one.
	//
	// Reported separately rather than folded into InputToks because they are
	// priced differently, and because they are the only honest way to know
	// whether caching is working. A cache that silently stopped matching would
	// otherwise look exactly like a cache that was never there — the same
	// answers, quietly at ten times the price.
	CacheWriteToks int
	CacheReadToks  int
}

// Capabilities is what a provider can actually do, so the runtime can report
// honestly instead of assuming.
type Capabilities struct {
	// Caching says a repeated prefix will be billed at a reduced rate.
	Caching bool

	// CachingIsExplicit distinguishes "we mark the blocks" from "it happens by
	// itself". It changes nothing the loop does and it changes what a
	// diagnostic can promise: with automatic caching there is nothing to
	// verify, and with explicit caching a marker in the wrong place is a
	// silent doubling of the bill.
	CachingIsExplicit bool

	// Vision says the model can be shown a picture and reason about it.
	//
	// It is here rather than assumed because the runtime has to TELL the model
	// which one it is. Measured before it was written: asked what colour the
	// window buttons were, an agent with no vision spent twenty-five turns
	// probing single pixels and never answered; asked whether anything looked
	// broken, it announced that the layout was fine and that a particular
	// button was highlighted. Neither was true and it had not looked at
	// anything — it rebuilt the screen from the accessibility tree and then
	// made claims about how it LOOKED.
	//
	// False today for every provider here: the desktop already returns a
	// screenshot as an MCP image block, and the runtime discards it. When that
	// changes this becomes true for the models that support it, the prompt
	// fragment keyed off it swaps, and nothing else has to move.
	Vision bool
}

// Provider is a model that can be asked for the next turn.
type Provider interface {
	// Name identifies it in logs and in the audit trail.
	Name() string

	// Complete asks for one turn. It does not loop; the loop is the caller's.
	Complete(ctx context.Context, req Request) (Response, error)

	// Capabilities describes what this one supports.
	Capabilities() Capabilities
}

// Unavailable is what a provider with no key returns.
//
// A named type rather than a string, so the runtime can tell "this provider is
// not configured" from "the provider is configured and failed". The first means
// tell the operator how to configure it; the second means retry or report an
// outage, and they read the same in a log.
type Unavailable struct {
	Provider string
	Reason   string
	HowToFix string
}

func (e *Unavailable) Error() string {
	if e.HowToFix != "" {
		return fmt.Sprintf("%s is unavailable: %s. %s", e.Provider, e.Reason, e.HowToFix)
	}
	return fmt.Sprintf("%s is unavailable: %s", e.Provider, e.Reason)
}

// Open builds a provider by name.
//
// The one place that knows which adapters exist, so adding one is a change here
// and a preset, rather than a change in every caller.
func Open(name, model string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "anthropic":
		return NewAnthropic(model)
	}
	if preset, ok := FindPreset(name); ok {
		return NewOpenAICompat(preset, model)
	}
	return nil, &Unavailable{
		Provider: name, Reason: "no such provider",
		HowToFix: "Known: anthropic, " + strings.Join(presetIDs(), ", ") +
			". `sentineldesk-agent providers` lists them with what each needs.",
	}
}

func presetIDs() []string {
	out := make([]string, 0, len(Presets))
	for _, p := range Presets {
		out = append(out, p.ID)
	}
	return out
}

// KeySourced is a provider that can say where its credential came from, which
// is a path or an environment variable's name and never the credential.
type KeySourced interface {
	KeySource() string
}

// IsUnavailable reports whether an error means "not configured" rather than
// "broken".
func IsUnavailable(err error) bool {
	var u *Unavailable
	return errors.As(err, &u)
}
