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

package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestARunRoundTrips covers the shape the operator actually reads back.
func TestARunRoundTrips(t *testing.T) {
	db := open(t)
	run, err := db.StartRun("task-1", "install nginx", "anthropic/claude-sonnet-5", "witnessed")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if err := run.RecordTurn(Turn{
		N: 1, Elapsed: 2 * time.Second, System: "you are driving a desktop",
		Request: []string{"install nginx"}, Response: map[string]any{"text": "ok"},
		Stop: "tool_use", InputTokens: 38814, OutputTokens: 220,
		ToolsOffered: 120, ToolsBytes: 67649,
	}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := run.RecordCall(Call{
		TurnN: 1, Tool: "terminal_run", AskedFor: "run_command",
		Args: map[string]any{"command": "df -h"}, Result: "Filesystem…",
		Elapsed: 1667 * time.Millisecond,
	}); err != nil {
		t.Fatalf("%v", err)
	}
	if err := run.Finish("finished", "done", 4, 4, 78159, 387); err != nil {
		t.Fatalf("%v", err)
	}

	recent, err := db.Recent(10)
	if err != nil || len(recent) != 1 {
		t.Fatalf("recent: %v, %d rows", err, len(recent))
	}
	if recent[0].InputTokens != 78159 || recent[0].Status != "finished" {
		t.Errorf("run came back as %+v", recent[0])
	}

	// The substitution is recorded, so the witnessed role can be audited rather
	// than believed.
	calls, err := db.Calls(run.ID)
	if err != nil || len(calls) != 1 {
		t.Fatalf("calls: %v, %d rows", err, len(calls))
	}
	if calls[0].Tool != "terminal_run" || calls[0].AskedFor != "run_command" {
		t.Errorf("the substitution is not in the record: %+v", calls[0])
	}
}

// TestTheSystemPromptIsKept. Somebody asking "what did you actually send it"
// has to get the text, not a description of the text.
func TestTheSystemPromptIsKept(t *testing.T) {
	db := open(t)
	run, _ := db.StartRun("t", "g", "m", "efficient")
	_ = run.RecordTurn(Turn{N: 1, System: "RULE: control is claimed, never assumed"})

	turns, err := db.Turns(run.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns: %v", err)
	}
	if turns[0].System != "RULE: control is claimed, never assumed" {
		t.Errorf("the system prompt came back as %q", turns[0].System)
	}
}

// TestTheCatalogueCostIsMeasurable is the reason half the schema exists: making
// the loop cheaper is a claim, and a claim about cost needs a before.
func TestTheCatalogueCostIsMeasurable(t *testing.T) {
	db := open(t)
	run, _ := db.StartRun("t", "g", "m", "efficient")
	_ = run.RecordTurn(Turn{N: 1, ToolsOffered: 120, ToolsBytes: 67649, InputTokens: 38814})
	_ = run.RecordTurn(Turn{N: 2, ToolsOffered: 120, ToolsBytes: 67649, InputTokens: 39345})

	c, err := db.CatalogueCost()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if c.Turns != 2 || c.AvgOffered != 120 {
		t.Errorf("catalogue cost came back as %+v", c)
	}
	// The share it takes of a turn is the number to move.
	if share := (c.AvgBytes / 4) / c.AvgInputToks; share < 0.3 {
		t.Errorf("the catalogue is %.0f%% of a turn, which does not match the measurement", share*100)
	}
}

// TestTheDatabaseIsNotReadableByOthers. It holds conversations and goals, which
// are nobody's business but the operator's.
func TestTheDatabaseIsNotReadableByOthers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	defer db.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("the database is mode %04o", info.Mode().Perm())
	}
}

// TestAnUnknownModelIsNotFree. Pricing zero for a model nobody listed would
// report a day's work as costing nothing, which is the worst way to be wrong
// about a bill.
func TestAnUnknownModelIsNotFree(t *testing.T) {
	p := LoadPricing()
	if _, known := p.Estimated("some-model-nobody-listed", 1_000_000, 1_000_000); known {
		t.Error("an unknown model was priced")
	}
	cost, known := p.Estimated("anthropic/claude-sonnet-5", 1_000_000, 0)
	if !known || cost <= 0 {
		t.Errorf("a known family was not priced: %v %v", cost, known)
	}
	// A point release inherits its family rather than falling to zero.
	if _, known := p.Estimated("claude-sonnet-5-20260801", 1000, 1000); !known {
		t.Error("a dated model id lost its family's rate")
	}
}
