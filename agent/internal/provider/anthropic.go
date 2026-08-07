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

// The Anthropic Messages API.
//
// The first adapter, and the one the catalogue was written for: the tool
// annotations this project publishes are the MCP specification's own hints, and
// the descriptions were tuned against a model that reads them.
//
// Nothing here is clever. It maps this package's types onto the wire format and
// back, and the reason it is worth writing rather than pulling in an SDK is
// that the mapping is the whole job — a dependency that changes its own types
// underneath a loop is a dependency that owns the loop.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultAnthropicModel is what `run` uses when nothing says otherwise.
const DefaultAnthropicModel = "claude-sonnet-5"

const anthropicVersion = "2023-06-01"

// Anthropic is a Provider backed by the Messages API.
type Anthropic struct {
	key   Secret
	model string
	url   string
	http  *http.Client
}

// NewAnthropic builds the adapter, or reports why it cannot.
//
// A missing key is an *Unavailable rather than a plain error, so the runtime
// can tell "nobody configured this" from "this is broken" and say something
// useful about each. The message names the file to create, because the answer
// to "it is unavailable" should never require reading the source.
func NewAnthropic(model string) (*Anthropic, error) {
	key, err := LoadKey("anthropic")
	if err != nil {
		return nil, &Unavailable{Provider: "anthropic", Reason: err.Error()}
	}
	if key.Empty() {
		return nil, &Unavailable{
			Provider: "anthropic",
			Reason:   "no API key",
			HowToFix: "Put it in ~/.sentineldesk/anthropic.key (chmod 600), " +
				"or set ANTHROPIC_API_KEY_FILE to a path.",
		}
	}
	if model == "" {
		model = DefaultAnthropicModel
	}
	return &Anthropic{
		key:   key,
		model: model,
		url:   "https://api.anthropic.com/v1/messages",
		// Generous, because a model thinking about a hard step legitimately
		// takes a while. The loop's own context is what actually bounds a run;
		// this only stops a connection that has died quietly.
		http: &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (a *Anthropic) Name() string { return "anthropic/" + a.model }

// KeySource says where the key came from. Safe to print: it is a path or the
// name of an environment variable, never the key.
func (a *Anthropic) KeySource() string { return a.key.Source() }

// --- the wire ---------------------------------------------------------------

type antRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []antMessage `json:"messages"`
	Tools     []antTool    `json:"tools,omitempty"`
}

type antMessage struct {
	Role    string       `json:"role"`
	Content []antContent `json:"content"`
}

type antContent struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type antTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type antResponse struct {
	Content    []antContent `json:"content"`
	StopReason string       `json:"stop_reason"`
	Usage      struct {
		Input  int `json:"input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete asks for one turn.
func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	body := antRequest{
		Model:     a.model,
		MaxTokens: req.MaxTokens,
		System:    req.System,
	}
	if body.MaxTokens <= 0 {
		body.MaxTokens = 8192
	}
	for _, t := range req.Tools {
		schema := t.InputSchema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		body.Tools = append(body.Tools, antTool{
			Name: t.Name, Description: t.Description, InputSchema: schema,
		})
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, toAnthropic(m))
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(raw))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	// The one place the key is revealed. Everywhere else it is a Secret, which
	// prints as a redaction.
	httpReq.Header.Set("x-api-key", a.key.Reveal())

	resp, err := a.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Response{}, fmt.Errorf("anthropic: reading the reply: %w", err)
	}

	var out antResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return Response{}, fmt.Errorf("anthropic: HTTP %d, unreadable reply: %s",
			resp.StatusCode, trunc(string(payload), 300))
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if out.Error != nil {
			msg = fmt.Sprintf("%s: %s", out.Error.Type, out.Error.Message)
		}
		// A rejected key is a configuration problem, not an outage, and saying
		// so is the difference between the operator fixing it and the operator
		// waiting for it to come back.
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return Response{}, &Unavailable{
				Provider: "anthropic", Reason: msg,
				HowToFix: "The key in " + a.key.Source() + " was rejected. " +
					"Check it at console.anthropic.com, and that the account has credit.",
			}
		}
		return Response{}, fmt.Errorf("anthropic: %s", msg)
	}

	return fromAnthropic(out)
}

func toAnthropic(m Message) antMessage {
	out := antMessage{Role: string(m.Role)}
	// Results first: the API wants a tool_result to be the first thing in the
	// turn that answers a tool_use.
	for _, r := range m.Results {
		out.Content = append(out.Content, antContent{
			Type: "tool_result", ToolUseID: r.CallID,
			Content: r.Text, IsError: r.IsErr,
		})
	}
	if strings.TrimSpace(m.Text) != "" {
		out.Content = append(out.Content, antContent{Type: "text", Text: m.Text})
	}
	for _, call := range m.ToolCalls {
		args := call.Args
		if args == nil {
			args = map[string]any{}
		}
		raw, _ := json.Marshal(args)
		out.Content = append(out.Content, antContent{
			Type: "tool_use", ID: call.ID, Name: call.Name, Input: raw,
		})
	}
	// A turn with no content at all is rejected by the API. It happens when a
	// model answers with nothing, which is rare and not worth failing a whole
	// run over.
	if len(out.Content) == 0 {
		out.Content = append(out.Content, antContent{Type: "text", Text: "(empty)"})
	}
	return out
}

func fromAnthropic(out antResponse) (Response, error) {
	msg := Message{Role: RoleAssistant}
	var text []string
	for _, block := range out.Content {
		switch block.Type {
		case "text":
			text = append(text, block.Text)
		case "tool_use":
			args := map[string]any{}
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &args)
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID: block.ID, Name: block.Name, Args: args,
			})
		}
	}
	msg.Text = strings.TrimSpace(strings.Join(text, "\n"))

	stop := StopEnd
	switch out.StopReason {
	case "tool_use":
		stop = StopToolUse
	case "max_tokens":
		stop = StopLength
	}
	return Response{
		Message: msg, Stop: stop,
		InputToks: out.Usage.Input, OutputToks: out.Usage.Output,
	}, nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
