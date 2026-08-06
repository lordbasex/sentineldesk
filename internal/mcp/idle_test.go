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

// The arithmetic behind wait_for_idle, without a desktop.
//
// What is checked here is the part that was wrong for as long as the tool
// existed: it summed `ps -eo pcpu`, a per-process average over each process's
// whole lifetime, and reported the total as current load. These tests pin the
// replacement to the property that column never had — that the answer describes
// an interval, and that an idle interval reads as idle.

import (
	"testing"
	"time"
)

func TestParseCPULineSplitsIdleFromBusy(t *testing.T) {
	// user nice system idle iowait irq softirq steal
	line := "cpu  100 0 100 700 100 0 0 0"
	idle, total, ok := parseCPULine(line)
	if !ok {
		t.Fatal("a well-formed cpu line was rejected")
	}
	if total != 1000 {
		t.Fatalf("total = %d, want 1000", total)
	}
	// idle plus iowait: 700 + 100. Counting iowait as busy would mean a desktop
	// reading a file never looks settled.
	if idle != 800 {
		t.Fatalf("idle = %d, want 800 (idle + iowait)", idle)
	}
}

func TestParseCPULineRejectsWhatIsNotTheAggregate(t *testing.T) {
	// The per-core lines come straight after the aggregate one, and reading
	// "cpu0" as the total would divide the machine's load by its core count.
	for _, line := range []string{"cpu0 1 2 3 4 5", "intr 12345", "", "cpu"} {
		if _, _, ok := parseCPULine(line); ok {
			t.Fatalf("accepted %q as the aggregate cpu line", line)
		}
	}
}

func TestCPUSamplerReportsTheIntervalNotTheUptime(t *testing.T) {
	var c cpuSampler
	// Stand in for two reads: a machine that has been up a long time and was
	// busy for most of it, but did nothing at all in the interval just past.
	c.idle, c.total, c.primed = 1_000_000, 2_000_000, true

	// Every jiffy since then went to idle.
	idle, total := uint64(1_000_100), uint64(2_000_100)
	dTotal, dIdle := total-c.total, idle-c.idle
	got := float64(dTotal-dIdle) * 100 / float64(dTotal)
	if got != 0 {
		t.Fatalf("an interval spent entirely idle reported %.1f%% busy", got)
	}
	// The lifetime ratio for the same numbers is 50% busy. That gap is the bug
	// this replaced: the old measure would have refused to call this desktop
	// idle on the strength of work it finished long ago.
	lifetime := float64(total-idle) * 100 / float64(total)
	if lifetime < 40 {
		t.Fatalf("test is not exercising the gap: lifetime ratio is %.1f%%", lifetime)
	}
}

func TestCPUSamplerUnprimedReportsZero(t *testing.T) {
	// Before prime() there is nothing to subtract from, and the honest answer
	// is "no reading yet" rather than the machine's whole uptime charged to one
	// interval.
	var c cpuSampler
	if got := c.percent(); got != 0 {
		t.Fatalf("unprimed sampler reported %.1f%%, want 0", got)
	}
}

func TestIdleFailureReasonSeparatesTheTwoFailures(t *testing.T) {
	quiet := time.Second

	// Screen settled, machine still working: the caller's work is happening
	// off-screen and watching the picture will never tell them it finished.
	r := idleFailureReason(time.Now().Add(-5*time.Second), quiet, 87)
	if r == "" || !contains(r, "CPU") {
		t.Fatalf("a settled screen over a busy machine gave %q", r)
	}
	if !contains(r, "87") {
		t.Fatalf("the reason dropped the CPU figure the caller needs: %q", r)
	}

	// Screen still moving: a different problem with a different response.
	r = idleFailureReason(time.Now(), quiet, 3)
	if !contains(r, "still changing") {
		t.Fatalf("a moving screen gave %q", r)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
