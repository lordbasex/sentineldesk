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

// One adapter for everything that speaks the OpenAI chat-completions shape.
//
// Which is most things. Ollama serves it locally, Ollama's cloud serves it,
// OpenAI serves it by definition, and OpenRouter, Groq and Together all chose
// it so that existing clients would work. Writing one adapter and four presets
// is not a shortcut: the wire format really is the same, and four near-identical
// files would be four places for a fix to be applied three times.
//
// The differences that remain are honest ones and live in Preset: where the
// endpoint is, whether a key is needed, and what the provider does about
// caching. A local model bills nothing and caches in its own KV; OpenAI caches
// long prefixes by itself and reports how many tokens it reused. Neither wants
// a marker, which is why CacheStable is a hint rather than an instruction.

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

// Preset is a service that speaks this protocol.
type Preset struct {
	// ID is what --provider takes.
	ID string

	// BaseURL is the API root, without /chat/completions.
	BaseURL string

	// KeyName is the credential to look for, or empty when none is needed.
	// A local Ollama needs none, which is most of why it is worth having.
	KeyName string

	// DefaultModel is used when --model says nothing.
	DefaultModel string

	// Caps describes what it does about caching, so the runtime reports what is
	// true rather than what it hopes.
	Caps Capabilities

	// Note is shown by `providers`, for the thing about it somebody has to know.
	Note string
}

// Presets are the services this adapter knows how to reach.
//
// Adding one is a row here. That is the whole point of the shared adapter, and
// the reason a row is enough is that the protocol is genuinely identical — the
// moment one of them needs different code it stops being a preset and becomes
// its own file, the way Anthropic is.
var Presets = []Preset{
	{
		ID: "ollama", BaseURL: "http://localhost:11434/v1",
		DefaultModel: "qwen3:8b",
		Caps:         Capabilities{Caching: false},
		Note: "A model on this machine. No key, no bill, no network — which makes it " +
			"the right place to develop the loop, since a run costs nothing to repeat.",
	},
	{
		ID: "ollama-cloud", BaseURL: "https://ollama.com/v1",
		KeyName: "ollama", DefaultModel: "qwen3:480b-cloud",
		Caps: Capabilities{Caching: false},
		Note: "Ollama's hosted models. Same protocol as the local one; the key is the only difference.",
	},
	{
		ID: "openai", BaseURL: "https://api.openai.com/v1",
		KeyName: "openai", DefaultModel: "gpt-5.2",
		Caps: Capabilities{Caching: true, CachingIsExplicit: false},
		Note: "Caches long prefixes automatically — nothing to mark, and it reports " +
			"how many tokens it reused.",
	},
	{
		ID: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		KeyName: "openrouter", DefaultModel: "anthropic/claude-sonnet-5",
		Caps: Capabilities{Caching: false},
		Note: "One key, many vendors' models. Useful for comparing them without " +
			"an account each; caching depends on whichever model is behind it.",
	},
}

func FindPreset(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == strings.ToLower(strings.TrimSpace(id)) {
			return p, true
		}
	}
	return Preset{}, false
}

// OpenAICompat talks to anything using the chat-completions shape.
type OpenAICompat struct {
	preset Preset
	model  string
	key    Secret
	http   *http.Client
}

// NewOpenAICompat builds an adapter for a preset, or says why it cannot.
func NewOpenAICompat(preset Preset, model string) (*OpenAICompat, error) {
	var key Secret
	if preset.KeyName != "" {
		var err error
		key, err = LoadKey(preset.KeyName)
		if err != nil {
			return nil, &Unavailable{Provider: preset.ID, Reason: err.Error()}
		}
		if key.Empty() {
			return nil, &Unavailable{
				Provider: preset.ID, Reason: "no API key",
				HowToFix: fmt.Sprintf("Put it in ~/.sentineldesk/%s.key (chmod 600).",
					preset.KeyName),
			}
		}
	}
	if model == "" {
		model = preset.DefaultModel
	}
	return &OpenAICompat{
		preset: preset, model: model, key: key,
		http: &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (o *OpenAICompat) Name() string               { return o.preset.ID + "/" + o.model }
func (o *OpenAICompat) Capabilities() Capabilities { return o.preset.Caps }

// KeySource says where the key came from, or that none was needed.
func (o *OpenAICompat) KeySource() string {
	if o.preset.KeyName == "" {
		return "none needed"
	}
	return o.key.Source()
}

// --- the wire ---------------------------------------------------------------

type oaiRequest struct {
	Model     string       `json:"model"`
	Messages  []oaiMessage `json:"messages"`
	Tools     []oaiTool    `json:"tools,omitempty"`
	MaxTokens int          `json:"max_completion_tokens,omitempty"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
		// Arguments is a JSON *string*, not an object. A detail worth naming
		// because it is the one thing that differs from every other part of
		// this format and the one place a naive mapping breaks.
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type oaiResponse struct {
	Choices []struct {
		Message      oaiMessage `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		Prompt     int `json:"prompt_tokens"`
		Completion int `json:"completion_tokens"`
		Details    struct {
			Cached int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (o *OpenAICompat) Complete(ctx context.Context, req Request) (Response, error) {
	body := oaiRequest{Model: o.model, MaxTokens: req.MaxTokens}
	if body.MaxTokens <= 0 {
		body.MaxTokens = 8192
	}
	// The system prompt is a message here rather than its own field. First,
	// because everything after it is what a provider with automatic prefix
	// caching wants to see stay put.
	if strings.TrimSpace(req.System) != "" {
		body.Messages = append(body.Messages, oaiMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, toOpenAI(m)...)
	}
	for _, t := range req.Tools {
		var tool oaiTool
		tool.Type = "function"
		tool.Function.Name = t.Name
		tool.Function.Description = t.Description
		tool.Function.Parameters = t.InputSchema
		if len(tool.Function.Parameters) == 0 {
			tool.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		body.Tools = append(body.Tools, tool)
	}
	// CacheStable is deliberately not acted on. Nothing in this family takes a
	// marker: OpenAI caches long prefixes by itself, and a local model has a KV
	// cache that no request field reaches. Putting the stable part first is the
	// whole of what a client can do, and that is done above.

	raw, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.preset.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	if !o.key.Empty() {
		httpReq.Header.Set("authorization", "Bearer "+o.key.Reveal())
	}

	resp, err := o.http.Do(httpReq)
	if err != nil {
		// A local server that is not running is the most likely failure here,
		// and it is a configuration problem rather than an outage.
		if o.preset.KeyName == "" {
			return Response{}, &Unavailable{
				Provider: o.preset.ID, Reason: err.Error(),
				HowToFix: "Is it running?  ollama serve",
			}
		}
		return Response{}, fmt.Errorf("%s: %w", o.preset.ID, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Response{}, fmt.Errorf("%s: reading the reply: %w", o.preset.ID, err)
	}
	var out oaiResponse
	if err := json.Unmarshal(payload, &out); err != nil {
		return Response{}, fmt.Errorf("%s: HTTP %d, unreadable reply: %s",
			o.preset.ID, resp.StatusCode, trunc(string(payload), 300))
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if out.Error != nil {
			msg = out.Error.Message
		}
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return Response{}, &Unavailable{
				Provider: o.preset.ID, Reason: msg,
				HowToFix: "The key in " + o.KeySource() + " was rejected.",
			}
		}
		// A model that is not pulled is the other common local failure, and
		// "404" is a bad way to learn it.
		if resp.StatusCode == http.StatusNotFound && o.preset.KeyName == "" {
			return Response{}, &Unavailable{
				Provider: o.preset.ID, Reason: msg,
				HowToFix: "Is the model pulled?  ollama pull " + o.model,
			}
		}
		return Response{}, fmt.Errorf("%s: %s", o.preset.ID, msg)
	}
	if len(out.Choices) == 0 {
		return Response{}, fmt.Errorf("%s: the reply has no choices", o.preset.ID)
	}
	return fromOpenAI(out)
}

func toOpenAI(m Message) []oaiMessage {
	// Tool results are their own messages here, one per result, rather than
	// blocks inside a user turn as they are for Anthropic. Getting this wrong
	// produces a conversation the model cannot follow rather than an error.
	var out []oaiMessage
	for _, r := range m.Results {
		text := r.Text
		if r.IsErr {
			text = "ERROR: " + text
		}
		out = append(out, oaiMessage{Role: "tool", ToolCallID: r.CallID, Content: text})
	}
	if len(m.ToolCalls) > 0 || strings.TrimSpace(m.Text) != "" {
		msg := oaiMessage{Role: string(m.Role), Content: m.Text}
		for _, call := range m.ToolCalls {
			args := call.Args
			if args == nil {
				args = map[string]any{}
			}
			raw, _ := json.Marshal(args)
			var oc oaiToolCall
			oc.ID, oc.Type = call.ID, "function"
			oc.Function.Name = call.Name
			oc.Function.Arguments = string(raw)
			msg.ToolCalls = append(msg.ToolCalls, oc)
		}
		out = append(out, msg)
	}
	return out
}

func fromOpenAI(out oaiResponse) (Response, error) {
	choice := out.Choices[0]
	msg := Message{Role: RoleAssistant, Text: strings.TrimSpace(choice.Message.Content)}
	for _, call := range choice.Message.ToolCalls {
		args := map[string]any{}
		// Arguments arrive as a JSON string. A small model sometimes produces
		// one that does not parse, and dropping the call silently would leave
		// the loop waiting for a result nobody is going to produce — so an
		// unparseable argument list becomes an empty one and the tool refuses
		// it, which the model can read and correct.
		if call.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID: call.ID, Name: call.Function.Name, Args: args,
		})
	}

	stop := StopEnd
	switch choice.FinishReason {
	case "tool_calls", "function_call":
		stop = StopToolUse
	case "length":
		stop = StopLength
	}
	// Some servers report tool calls without setting finish_reason, and a loop
	// that trusted the field alone would end the run with work outstanding.
	if len(msg.ToolCalls) > 0 && stop == StopEnd {
		stop = StopToolUse
	}

	return Response{
		Message: msg, Stop: stop,
		// Cached tokens are reported inside the prompt count here, not beside
		// it. Subtracting keeps InputToks meaning the same thing it means for
		// every other provider: what was paid at the full rate.
		InputToks:     out.Usage.Prompt - out.Usage.Details.Cached,
		OutputToks:    out.Usage.Completion,
		CacheReadToks: out.Usage.Details.Cached,
	}, nil
}
