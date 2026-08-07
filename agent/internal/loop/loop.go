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

// Package loop runs a goal to completion: ask the model, run what it asks for,
// hand back what happened, repeat.
//
// Three things make this more than a while-loop around an API call, and each of
// them exists because the server was taught something in stage 1 that a naive
// runtime would waste:
//
//   - A refusal is not a failure. `room` means ask a person and try again;
//     `policy` means stop. Collapsing them turns "wait your turn" into
//     "abandon the task".
//   - An interruption has to be said out loud. When a person takes the controls
//     mid-task, the next turn is told the tools may have half-run — because a
//     cancelled `run_command` may have installed half a package, and a model
//     planning its next step from the opposite assumption will make it worse.
//   - Cost is round trips first. A turn that calls three tools and gets three
//     answers costs one round trip; three turns of one tool each cost three.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lordbasex/sentineldesk/agent/internal/mcpclient"
	"github.com/lordbasex/sentineldesk/agent/internal/provider"
	"github.com/lordbasex/sentineldesk/agent/internal/store"
)

// Role is how observable the agent's work has to be. See §12.2 of the stage 1
// document: this is runtime policy, not a judgement the model makes per call.
type Role string

const (
	// RoleEfficient — nobody is watching and nobody asked for evidence. The
	// invisible path is correct; making a desktop flicker for a package install
	// is theatre.
	RoleEfficient Role = "efficient"

	// RoleWitnessed — somebody asked for a recording, screenshots or a
	// demonstration. The invisible path is CLOSED: where a visible equivalent
	// exists the runtime substitutes it. Enforced here rather than requested of
	// the model, because evidence cannot depend on a model remembering to be
	// observable.
	RoleWitnessed Role = "witnessed"
)

// visibleEquivalent maps an invisible tool to the visible one that does the
// same job, for RoleWitnessed.
//
// Deliberately tiny. A pair belongs here only when the two tools really do the
// same work and differ only in whether a person sees it — which is true of
// exactly one pair today. Anything broader would be the runtime silently
// choosing a different capability, which is a different thing from choosing a
// different way to show the same one.
var visibleEquivalent = map[string]string{
	"run_command": "terminal_run",
}

// Options configures a run.
type Options struct {
	Role  Role
	Model provider.Provider

	// MaxTurns bounds a run. Not a safety property — cancellation is that — but
	// a loop that has stopped making progress should say so rather than spend
	// somebody's credit discovering it.
	MaxTurns int

	// Tools is what the model is offered. Narrowed by the caller through
	// Select, which is where the reasoning about it lives.
	Tools []mcpclient.Tool

	// Catalogue is everything the connection can call, whether offered or not.
	// It is what a tool_search result is resolved against when the model finds
	// something outside the offered set. Empty means Tools is everything.
	Catalogue []mcpclient.Tool

	// OnEvent reports what the loop is doing, for a console or a log.
	OnEvent func(Progress)

	// Recorder keeps the run. Optional: a runtime whose database would not open
	// still works, unrecorded, and says so — losing the accounting is worse
	// than not having it and better than a task that will not start because of
	// a log.
	Recorder Recorder
}

// Recorder is what the loop writes its history to. An interface so the loop
// does not import a database, and so a test can watch what it would have
// written without one.
type Recorder interface {
	RecordTurn(store.Turn) error
	RecordCall(store.Call) error
}

// Progress is one thing the loop did.
type Progress struct {
	Kind    string // turn | text | call | result | interrupted | done
	Turn    int
	Tool    string
	Detail  string
	Elapsed time.Duration
}

// Result is how a run ended.
type Result struct {
	Answer string
	Turns  int
	Calls  int

	// Interrupted is set when a person took the controls mid-run. Distinct from
	// an error: nothing went wrong, somebody else needed the desktop.
	Interrupted bool

	InputToks, OutputToks int

	// What caching did. Kept apart from InputToks because they are billed
	// differently, and because a cache that stopped matching looks exactly like
	// one that was never there unless these are counted.
	CacheWriteToks, CacheReadToks int
}

// Runner holds what a run needs.
type Runner struct {
	mcp  *mcpclient.Client
	opts Options

	mu           sync.Mutex
	interrupted  bool
	interruptWhy string
}

func New(mcp *mcpclient.Client, opts Options) *Runner {
	if opts.MaxTurns <= 0 {
		opts.MaxTurns = 25
	}
	if opts.Role == "" {
		opts.Role = RoleEfficient
	}
	return &Runner{mcp: mcp, opts: opts}
}

// NoteEvent is what the client's OnEvent hands here.
//
// Exactly one event ends a run, and it is not "the controller changed" — that
// is also true when the agent is GIVEN the desktop, and stopping then would
// abandon a task the moment somebody helped.
func (r *Runner) NoteEvent(e mcpclient.Event) {
	if !e.InterruptsWork() {
		return
	}
	r.mu.Lock()
	r.interrupted = true
	r.interruptWhy = fmt.Sprintf("%s took the controls", displayName(e))
	r.mu.Unlock()
}

func displayName(e mcpclient.Event) string {
	if e.ControllerName != "" {
		return e.ControllerName
	}
	if e.Controller != "" {
		return e.Controller
	}
	return "somebody"
}

func (r *Runner) wasInterrupted() (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interrupted, r.interruptWhy
}

// Run works towards a goal.
func (r *Runner) Run(ctx context.Context, goal string) (Result, error) {
	var res Result
	messages := []provider.Message{{Role: provider.RoleUser, Text: goal}}
	offered := r.opts.Tools
	tools := toProviderTools(offered)

	for turn := 1; turn <= r.opts.MaxTurns; turn++ {
		res.Turns = turn
		r.report(Progress{Kind: "turn", Turn: turn})

		started := time.Now()
		system := r.systemPrompt()
		// The system prompt and the catalogue do not change across a run, and
		// they are ninety-eight per cent of what a turn costs. Saying so lets a
		// provider that can cache them do it; one that cannot ignores the hint
		// and is merely more expensive.
		req := provider.Request{System: system, Messages: messages, Tools: tools,
			CacheStable: true}
		reply, err := r.opts.Model.Complete(ctx, req)
		if err != nil {
			return res, err
		}
		res.InputToks += reply.InputToks
		res.OutputToks += reply.OutputToks
		res.CacheWriteToks += reply.CacheWriteToks
		res.CacheReadToks += reply.CacheReadToks

		// Recorded before anything is done with the answer, so a turn that ends
		// in an interruption is still on the record. The catalogue's size goes
		// with it: it is the largest line in the bill and the number the
		// tool-selection work has to move, so "it got cheaper" can be shown
		// rather than asserted.
		r.record(store.Turn{
			N: turn, Elapsed: time.Since(started), System: system,
			Request: messages, Response: reply.Message, Stop: string(reply.Stop),
			InputTokens: reply.InputToks, OutputTokens: reply.OutputToks,
			CacheWriteTokens: reply.CacheWriteToks, CacheReadTokens: reply.CacheReadToks,
			ToolsOffered: len(tools), ToolsBytes: toolBytes(tools),
			ProseChars: len(system) + jsonLen(messages),
		})

		if reply.Message.Text != "" {
			r.report(Progress{Kind: "text", Turn: turn,
				Detail: reply.Message.Text, Elapsed: time.Since(started)})
		}
		messages = append(messages, reply.Message)

		// Nothing to run: the model has said its piece and the run is over.
		if reply.Stop != provider.StopToolUse || len(reply.Message.ToolCalls) == 0 {
			res.Answer = reply.Message.Text
			if reply.Stop == provider.StopLength {
				// Truncated is not finished. Saying so beats presenting half a
				// plan as a whole one.
				res.Answer += "\n\n(the model ran out of output room mid-answer)"
			}
			r.report(Progress{Kind: "done", Turn: turn})
			return res, nil
		}

		results := make([]provider.ToolResult, 0, len(reply.Message.ToolCalls))
		for _, call := range reply.Message.ToolCalls {
			res.Calls++
			out := r.runOne(ctx, turn, call)
			results = append(results, out)

			// The model went looking for something it was not offered. Widen
			// the set so it can actually call what it just found — otherwise
			// tool_search is a tool that returns names nobody can use, which is
			// worse than not offering it.
			//
			// This costs one cache rebuild, because the prefix changed. Paying
			// it to reach a tool beats failing the task to keep a cache warm.
			if call.Name == "tool_search" && !out.IsErr && len(r.opts.Catalogue) > 0 {
				if extra := newTools(offered, discovered(out.Text, r.opts.Catalogue)); len(extra) > 0 {
					offered = append(offered, extra...)
					sortTools(offered)
					tools = toProviderTools(offered)
					r.report(Progress{Kind: "widened", Turn: turn,
						Detail: fmt.Sprintf("%s now offered", names(extra))})
				}
			}

			// Checked between calls, not only between turns. A person taking
			// the controls does not wait for the model, and running the rest of
			// a batch into a desktop that is no longer ours is the exact thing
			// the event channel was built to stop.
			if stop, _ := r.wasInterrupted(); stop {
				break
			}
		}

		next := provider.Message{Role: provider.RoleUser, Results: results}

		if stop, why := r.wasInterrupted(); stop {
			res.Interrupted = true
			r.report(Progress{Kind: "interrupted", Turn: turn, Detail: why})
			// The turn that would follow has to know the difference between a
			// clean stop and a messy one — tools in that batch may have
			// half-run. Told rather than inferred, because a model reasoning
			// from "nothing happened" after a partial batch makes it worse.
			next.Text = fmt.Sprintf(
				"<interrupted>%s. Anything already started may have partly run; "+
					"do not assume it did or did not. Say what you had done and stop."+
					"</interrupted>", why)
			messages = append(messages, next)
			res.Answer = why
			return res, nil
		}

		messages = append(messages, next)
	}

	res.Answer = fmt.Sprintf("stopped after %d turns without finishing", r.opts.MaxTurns)
	return res, nil
}

// runOne calls a tool and turns whatever happened into something the model can
// act on.
func (r *Runner) runOne(ctx context.Context, turn int, call provider.ToolCall) provider.ToolResult {
	name := r.substitute(call.Name)
	askedFor := ""
	if name != call.Name {
		askedFor = call.Name
	}
	if name != call.Name {
		r.report(Progress{Kind: "call", Tool: name,
			Detail: fmt.Sprintf("(substituted for %s: this run is being watched)", call.Name)})
	} else {
		r.report(Progress{Kind: "call", Tool: name, Detail: summarize(call.Args)})
	}

	started := time.Now()
	out, err := r.mcp.Call(ctx, name, call.Args)
	if err != nil {
		r.recordCall(store.Call{TurnN: turn, Tool: name, AskedFor: askedFor,
			Args: call.Args, Result: err.Error(), IsError: true,
			Elapsed: time.Since(started)})
		return provider.ToolResult{CallID: call.ID, IsErr: true,
			Text: fmt.Sprintf("the call could not be made: %v", err)}
	}
	r.recordCall(store.Call{TurnN: turn, Tool: name, AskedFor: askedFor,
		Args: call.Args, Result: out.Text(), Denial: string(out.Denial),
		IsError: out.IsError, Elapsed: time.Since(started)})
	r.report(Progress{Kind: "result", Tool: name,
		Detail: trunc(out.Text(), 200), Elapsed: time.Since(started)})

	text := out.Text()
	// A refusal is told to the model as a refusal, with what to do about it.
	// The kind is the whole point: without it every failure reads the same and
	// the model either gives up on a queue or retries against a rule.
	switch out.Denial {
	case mcpclient.DenialRoom:
		text += "\n\nSomebody else is driving. Call request_control and try again."
	case mcpclient.DenialPolicy:
		text += "\n\nThe server's policy forbids this. Do not retry and do not " +
			"ask anyone — nobody in the room can widen it. Find another way or stop."
	case mcpclient.DenialBadArgs:
		text += "\n\nFix the arguments and call it again."
	case mcpclient.DenialEmergency:
		text += "\n\nA person stopped this agent. Stop."
	}
	return provider.ToolResult{CallID: call.ID, Text: text, IsErr: out.IsError}
}

// substitute swaps an invisible tool for its visible twin when the run is being
// watched. Enforced here on purpose: see RoleWitnessed.
func (r *Runner) substitute(name string) string {
	if r.opts.Role != RoleWitnessed {
		return name
	}
	visible, ok := visibleEquivalent[name]
	if !ok {
		return name
	}
	// Only if it is actually on offer. Substituting for a tool this connection
	// may not call would turn a working step into a refusal, which is worse
	// than doing it invisibly.
	for _, t := range r.opts.Tools {
		if t.Name == visible {
			return visible
		}
	}
	return name
}

// toProviderTools converts MCP tools into what the model is told.
//
// The schema is the server's own, passed through untouched. A second
// description of a tool is a second thing to drift from the first.
func toProviderTools(tools []mcpclient.Tool) []provider.Tool {
	out := make([]provider.Tool, 0, len(tools))
	for _, t := range tools {
		out = append(out, provider.Tool{
			Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
		})
	}
	return out
}

// newTools returns the ones not already offered.
func newTools(have, found []mcpclient.Tool) []mcpclient.Tool {
	seen := make(map[string]bool, len(have))
	for _, t := range have {
		seen[t.Name] = true
	}
	var out []mcpclient.Tool
	for _, t := range found {
		if !seen[t.Name] {
			out = append(out, t)
			seen[t.Name] = true
		}
	}
	return out
}

// sortTools keeps the offered set in a stable order, so two runs that end up
// with the same tools send a byte-identical prefix and the second finds the
// first one's cache still warm.
func sortTools(tools []mcpclient.Tool) {
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
}

func names(tools []mcpclient.Tool) string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Name
	}
	return strings.Join(out, ", ")
}

func (r *Runner) systemPrompt() string {
	var b strings.Builder
	b.WriteString(`You are driving a real Linux desktop that people are watching and can take back at any moment.

Rules that are not suggestions:
- Control is claimed, never assumed. Call request_control before anything that moves the pointer, types, or presses a key, and release_control when the task is done.
- A tool returning ok means it did not throw. It does not mean it did the job. Where there is an artifact — a file, a page, a window — open it and check.
- Prefer one tool call that answers completely over two that each answer half. Reading the whole accessibility tree to find one button is the expensive way to do a cheap thing: ui_find and ui_at_point answer the same question for a fraction of it.
- If a person is present and the answer is theirs to give rather than yours to guess, use ask_human.
`)
	if r.opts.Role == RoleWitnessed {
		b.WriteString(`
Somebody asked to SEE this happen. Do the work on screen where they can watch: open a terminal and type into it rather than running commands off-screen, and let windows be visible rather than working around them.
`)
	}
	return b.String()
}

func (r *Runner) record(t store.Turn) {
	if r.opts.Recorder != nil {
		_ = r.opts.Recorder.RecordTurn(t)
	}
}

func (r *Runner) recordCall(c store.Call) {
	if r.opts.Recorder != nil {
		_ = r.opts.Recorder.RecordCall(c)
	}
}

// toolBytes is what the catalogue costs on the wire. An approximation of what
// the provider will charge for it, and an exact measure of what changed when
// the offered set changes, which is the question it exists to answer.
// jsonLen is how much conversation was sent, for the same subtraction.
func jsonLen(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(raw)
}

func toolBytes(tools []provider.Tool) int {
	n := 0
	for _, t := range tools {
		n += len(t.Name) + len(t.Description) + len(t.InputSchema)
	}
	return n
}

func (r *Runner) report(p Progress) {
	if r.opts.OnEvent != nil {
		r.opts.OnEvent(p)
	}
}

func summarize(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return trunc(string(raw), 160)
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
