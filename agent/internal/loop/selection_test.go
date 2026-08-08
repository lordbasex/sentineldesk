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

import (
	"strings"
	"testing"

	"github.com/lordbasex/sentineldesk/agent/internal/mcpclient"
)

func bigCatalogue() []mcpclient.Tool {
	return []mcpclient.Tool{
		{Name: "tool_search", Description: "find the tools for a task"},
		{Name: "screenshot", Description: "capture the screen as PNG"},
		{Name: "list_windows", Description: "the open windows"},
		{Name: "ui_click", Description: "press a button by its ref"},
		{Name: "list_installed_apps", Description: "the graphical applications installed"},
		{Name: "start_recording", Description: "record a video of the desktop"},
		{Name: "ssh_connect", Description: "log in to a remote machine over ssh"},
		{Name: "install_packages", Description: "install packages with apt"},
	}
}

func offeredNames(sel Selection) []string {
	out := make([]string, len(sel.Tools))
	for i, t := range sel.Tools {
		out[i] = t.Name
	}
	return out
}

func has(sel Selection, name string) bool {
	return strings.Contains(strings.Join(offeredNames(sel), " "), name)
}

// TestTheGoalPullsInWhatItNeeds.
func TestTheGoalPullsInWhatItNeeds(t *testing.T) {
	sel := Select(bigCatalogue(), "record a video of the desktop", 3, false)
	if !has(sel, "start_recording") {
		t.Errorf("the goal's own tool was not offered: %v", offeredNames(sel))
	}
	if !has(sel, "tool_search") {
		t.Error("tool_search must always be offered — it is how anything missed is reached")
	}
	if sel.RankingFailed {
		t.Error("a goal that matched was reported as a ranking failure")
	}
}

// TestAGoalThatMatchesNothingGetsEverything.
//
// The case that cost a real run two turns and three times the money: a goal
// matched zero of a hundred and twenty tools, the run started with the core set
// alone as though the ranking had recommended it, and the model searched,
// searched again, then gave up on tools and shelled out.
//
// A ranking with no opinion must not be mistaken for a ranking that chose the
// core set. Turns cost more than schemas.
//
// The example used to be "¿qué aplicaciones están instaladas?", which matched
// nothing because the vocabulary was English only. That is fixed — there is a
// Spanish vocabulary now and it matches list_installed_apps — so the example had
// to change or the test would have been asserting a bug back into existence.
// The MECHANISM is what is being tested and it is unchanged: whatever the
// language, a ranking that found nothing has to say so rather than pass its
// silence off as a recommendation.
func TestAGoalThatMatchesNothingGetsEverything(t *testing.T) {
	catalogue := bigCatalogue()
	sel := Select(catalogue, "qwzx plimf zorbnak", 3, false)

	if !sel.RankingFailed {
		t.Fatal("a goal that matched nothing was not reported as a ranking failure")
	}
	if len(sel.Tools) != len(catalogue) {
		t.Errorf("%d tools offered after the ranking failed, want all %d",
			len(sel.Tools), len(catalogue))
	}
	// And it says so, because a run that costs more for this reason should not
	// do it quietly.
	if !strings.Contains(sel.Describe(), "matched") {
		t.Errorf("the narration does not explain why: %q", sel.Describe())
	}
}

// TestZeroOffersEverything is the escape hatch: the day a run fails, the first
// question is whether the selection caused it.
func TestZeroOffersEverything(t *testing.T) {
	catalogue := bigCatalogue()
	if sel := Select(catalogue, "anything", 0, false); len(sel.Tools) != len(catalogue) {
		t.Errorf("--tools 0 offered %d of %d", len(sel.Tools), len(catalogue))
	}
}

// TestTheOfferedSetIsStable. Caching matches a byte-identical prefix, so two
// runs of the same goal must produce the same order — left to map iteration it
// would differ and the second run would quietly pay full price.
func TestTheOfferedSetIsStable(t *testing.T) {
	first := offeredNames(Select(bigCatalogue(), "record a video", 4, false))
	for i := 0; i < 20; i++ {
		if got := offeredNames(Select(bigCatalogue(), "record a video", 4, false)); !equal(first, got) {
			t.Fatalf("the order changed between runs:\n%v\n%v", first, got)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAnExplicitCapIsHonouredEvenWhenTheRankingFails.
//
// Offering everything is the right recovery against a hosted model — the prefix
// is cached, and a wrong small set costs turns, which cost more than schemas.
// It is the wrong recovery against a model on a CPU, where a hundred and twenty
// schemas is nineteen thousand tokens to process before one is generated:
// minutes, not cents. Somebody who typed --tools 6 meant it.
func TestAnExplicitCapIsHonouredEvenWhenTheRankingFails(t *testing.T) {
	catalogue := bigCatalogue()
	sel := Select(catalogue, "¿qué ventanas están abiertas?", 2, true)

	if !sel.RankingFailed {
		t.Fatal("a goal that matched nothing was not reported as a ranking failure")
	}
	if len(sel.Tools) == len(catalogue) {
		t.Errorf("an explicit cap was ignored: all %d offered", len(sel.Tools))
	}
	// And it still says why, because a run about to go badly for this reason
	// should say so rather than let somebody discover it.
	if !strings.Contains(sel.Describe(), "matched") {
		t.Errorf("the narration does not explain the failure: %q", sel.Describe())
	}
	if !strings.Contains(sel.Describe(), "cap") {
		t.Errorf("the narration does not say the cap was respected: %q", sel.Describe())
	}
}
