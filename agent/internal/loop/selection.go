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

package loop

// Deciding which tools the model is shown.
//
// A hundred and twenty schemas is nineteen thousand tokens, re-sent on every
// turn, and measured against a real run it was ninety-eight per cent of what
// the turn cost. The conversation itself was three hundred tokens. Somebody
// paid eight cents to ask which windows were open, and almost all of it was
// describing a hundred and nineteen tools that were never called.
//
// It is also the reason a local model is unusable on a CPU: generating is slow,
// and processing nineteen thousand tokens of JSON schema before generating
// anything is much slower.
//
// Three rules, and the third is the one that is easy to get wrong.
//
// A CORE set is always offered — look, orient, click, type, ask, take the
// controls. An agent that could not see the screen without first searching for
// a way to see the screen would spend a round trip on every task.
//
// The goal RANKS the rest. The same ranking the server exposes as tool_search,
// shared through internal/toolsearch (ADR-003), measured at 100% top ten. It is
// used here rather than called over the socket because the runtime already
// holds the catalogue and asking the server would be a round trip to have it
// answer a question this side can answer from what it is carrying.
//
// And the set STAYS STILL. Caching is the other half of the saving and it works
// by matching a byte-identical prefix, so a catalogue that changed shape each
// turn would invalidate the cache every time and cost more than sending
// everything. Tools are chosen once, at the start of a run. When the model asks
// for something outside the set — that is what tool_search is for, and it is
// always offered — the answer is added and the cache is rebuilt once, which is
// a price worth paying to reach a tool rather than fail.

import (
	"fmt"
	"strings"

	"github.com/lordbasex/sentineldesk/agent/internal/mcpclient"
	"github.com/lordbasex/sentineldesk/internal/toolsearch"
)

// coreTools are offered on every run whatever the goal.
//
// Not "the most useful tools" — the ones without which the model cannot take a
// first step or recover from a wrong one. Seeing, orienting, acting, asking,
// and the two that decide whether it may act at all.
//
// tool_search is the important one. It is what makes the rest of this safe:
// the model is not restricted to the selection, it is *started* with it, and
// anything the ranking missed is one call away.
var coreTools = []string{
	"tool_search",

	// Seeing and orienting. list_windows and get_active_window are 30 ms and a
	// hundred tokens; ui_at_point answers "what is that" without walking the
	// tree. Starting anywhere else is the expensive way to begin.
	"screenshot", "list_windows", "get_active_window", "ui_find", "ui_at_point",

	// Acting.
	"ui_click", "mouse_click", "type_text", "key_combo",

	// Turn-taking and asking. Without request_control nothing that touches the
	// desktop works at all; without ask_human the model guesses at answers that
	// were somebody else's to give.
	"room_state", "request_control", "release_control", "ask_human",

	// Waiting, and being told. Both are how a run stops polling.
	"wait", "wait_for_idle", "subscribe_events",
}

// Selection is what a run will be offered, and why.
type Selection struct {
	Tools []mcpclient.Tool

	// Core and Ranked count how the set was arrived at, for the narration —
	// so an operator can see whether the ranking is pulling its weight rather
	// than take it on faith.
	Core, Ranked int

	// Dropped is how many the model will not see unless it searches.
	Dropped int

	// RankingFailed says the goal matched nothing, so everything was offered
	// rather than narrowing on an opinion that did not exist. Worth surfacing:
	// it is the signal that the goal is in a language the vocabulary does not
	// cover, and a run that quietly costs more for that reason should say so.
	RankingFailed bool
}

// Select decides which tools a run starts with.
//
// limit is how many ranked tools to add on top of the core. Zero offers
// everything, which is what --tools 0 does: the honest escape hatch for the day
// a run fails and the first question is whether the selection caused it.
//
// capped says the caller chose that number rather than inheriting the default.
// It decides what happens when the ranking finds nothing, and the two answers
// are opposite for good reasons — see below.
func Select(catalogue []mcpclient.Tool, goal string, limit int, capped bool) Selection {
	if limit <= 0 {
		return Selection{Tools: catalogue, Core: 0, Ranked: 0, Dropped: 0}
	}

	byName := make(map[string]mcpclient.Tool, len(catalogue))
	flat := make([]toolsearch.Tool, 0, len(catalogue))
	for _, t := range catalogue {
		byName[t.Name] = t
		flat = append(flat, toolsearch.Tool{Name: t.Name, Description: t.Description})
	}

	chosen := map[string]bool{}
	var out []mcpclient.Tool
	add := func(name string) bool {
		if chosen[name] {
			return false
		}
		tool, ok := byName[name]
		if !ok {
			// A core tool this server does not have. Not an error: a
			// connection restricted to readonly legitimately has no
			// request_control, and a build without a room has no ask_human.
			return false
		}
		chosen[name] = true
		out = append(out, tool)
		return true
	}

	core := 0
	for _, name := range coreTools {
		if add(name) {
			core++
		}
	}

	ranked := 0
	for _, hit := range toolsearch.Rank(flat, goal, limit+len(coreTools)) {
		if ranked >= limit {
			break
		}
		if add(hit.Name) {
			ranked++
		}
	}

	// Nothing matched. That is not "the core set is enough" — it is the ranking
	// having no opinion, and the two must not produce the same behaviour.
	//
	// It happens whenever the goal is not in English, because the vocabulary is
	// English keywords: "¿qué aplicaciones están instaladas?" matched zero of a
	// hundred and twenty tools, the run started with the core set alone, and the
	// model spent two turns searching before it could do anything — then gave up
	// on tools and shelled out. Six turns and USD 0.0651 for a question that
	// costs three turns and a fifth of that when the right tool is on offer.
	//
	// So a ranking that says nothing hands back everything. The saving is worth
	// having when the ranking is confident and worth losing when it is not: a
	// wrong small set costs turns, and turns cost more than schemas.
	if ranked == 0 {
		// Unless the caller asked for a specific number, in which case they
		// meant it.
		//
		// Offering everything is the right recovery against a hosted model: the
		// prefix is cached, and a wrong small set costs turns, which cost more
		// than schemas. It is the wrong recovery against a model on a CPU,
		// where a hundred and twenty schemas is nineteen thousand tokens to
		// process before a single one is generated — minutes, not cents. So
		// `--tools 6` is honoured even here, and the narration says the ranking
		// had nothing to offer, because a run that is about to go badly for
		// this reason should say so rather than discover it.
		if capped {
			sortTools(out)
			return Selection{Tools: out, Core: core, Ranked: 0,
				Dropped: len(catalogue) - len(out), RankingFailed: true}
		}
		return Selection{Tools: catalogue, Core: core, Ranked: 0, Dropped: 0,
			RankingFailed: true}
	}

	// Stable order, so two runs with the same goal produce a byte-identical
	// prefix and the cache from the first is still warm for the second. Left in
	// ranking order it would depend on map iteration and quietly stop matching.
	sortTools(out)

	return Selection{
		Tools: out, Core: core, Ranked: ranked,
		Dropped: len(catalogue) - len(out),
	}
}

// Describe is one line for the narration.
func (s Selection) Describe() string {
	if s.RankingFailed && s.Dropped == 0 {
		return fmt.Sprintf(
			"nothing in the goal matched the catalogue, so all %d are offered "+
				"(the ranking's vocabulary is English)", len(s.Tools))
	}
	if s.RankingFailed {
		return fmt.Sprintf(
			"nothing in the goal matched the catalogue; %d offered because you asked "+
				"for a cap · %d reachable through tool_search (the ranking's vocabulary is English)",
			len(s.Tools), s.Dropped)
	}
	if s.Dropped == 0 {
		return ""
	}
	return fmt.Sprintf(
		"%d tools offered (%d core, %d matched the goal) · %d more reachable through tool_search",
		len(s.Tools), s.Core, s.Ranked, s.Dropped)
}

// discovered pulls tool names out of a tool_search result, so the runtime can
// widen the offered set to whatever the model just found.
//
// Widening rather than replacing: the model asked for one more thing, not for a
// different toolbox, and dropping what it already had would break the plan it
// is in the middle of.
func discovered(text string, catalogue []mcpclient.Tool) []mcpclient.Tool {
	var found []mcpclient.Tool
	for _, t := range catalogue {
		// The result is JSON with "name": "<tool>" per hit. Matching on the
		// quoted field rather than the bare word so that a tool merely
		// MENTIONED in another's description does not get added.
		if strings.Contains(text, `"name": "`+t.Name+`"`) ||
			strings.Contains(text, `"name":"`+t.Name+`"`) {
			found = append(found, t)
		}
	}
	return found
}
