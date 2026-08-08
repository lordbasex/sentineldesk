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

// Package tui draws a run while it happens.
//
// It consumes exactly the same loop.Progress stream the line-based output does,
// which is why this could be added without the run loop learning anything about
// a screen. The loop reports what it did; how that is shown is somebody else's
// problem, and now there are two answers to it.
//
// Both answers are kept, on purpose:
//
//	default   a live view — what is running now, what it returned, what it has
//	          cost so far — for a person watching a task they asked for.
//	-debug    one line per event, the shape this had before, for a person
//	          debugging the runtime rather than watching the task.
//
// And a third case that is neither: stdout not being a terminal. A TUI writes
// cursor movements, and a pipe or a CI log receives them as garbage, so a
// non-terminal falls back to the line output whether or not -debug was passed.
// That check is not a nicety — every measurement in this project's history was
// taken with the agent piped into something.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"

	"github.com/lordbasex/sentineldesk/agent/internal/loop"
)

// Usable reports whether the live view can be drawn at all.
func Usable() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// The palette is the desktop's, so a terminal running beside the browser rail
// looks like it belongs to the same product: #3FD68C for anything live, #93A09A
// for context, #E7EDEA for what is being said.
var (
	live    = lipgloss.NewStyle().Foreground(lipgloss.Color("#3FD68C"))
	dim     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7671"))
	context = lipgloss.NewStyle().Foreground(lipgloss.Color("#93A09A"))
	ink     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E7EDEA"))
	bad     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F57"))
	rule    = lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3936"))
)

// Header is what does not change during a run.
type Header struct {
	Model, Key, Tools, Task, Goal string
}

// Totals is what the runtime knows when the run ends.
type Totals struct {
	Status                        string
	Turns, Calls                  int
	InputToks, OutputToks         int
	CacheWriteToks, CacheReadToks int
	Cost                          float64
	CostKnown                     bool
	Elapsed                       time.Duration
	HistoryID                     int64
	Answer                        string
}

type line struct {
	kind string // turn | text | call | result | error | note
	text string
	took time.Duration
}

type model struct {
	header  Header
	lines   []line
	spin    int
	current string
	started time.Time

	turns, calls int
	done         bool
	totals       Totals
	width        int

	events <-chan loop.Progress
	finish <-chan Totals
}

type progressMsg loop.Progress
type finishedMsg Totals
type tickMsg time.Time

var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m model) Init() tea.Cmd {
	return tea.Batch(waitProgress(m.events), waitFinish(m.finish), tick())
}

func waitProgress(ch <-chan loop.Progress) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return nil
		}
		return progressMsg(p)
	}
}

func waitFinish(ch <-chan Totals) tea.Cmd {
	return func() tea.Msg {
		t, ok := <-ch
		if !ok {
			return nil
		}
		return finishedMsg(t)
	}
}

func tick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		// Ctrl-C is NOT handled here. The run's own signal handler cancels the
		// context, the cancellation reaches the server, and the tool in flight
		// actually stops — quitting the UI first would leave that half-done and
		// tell the person it was finished.
		if msg.String() == "q" && m.done {
			return m, tea.Quit
		}
		return m, nil

	case tickMsg:
		m.spin++
		if m.done {
			return m, nil
		}
		return m, tick()

	case progressMsg:
		m.absorb(loop.Progress(msg))
		return m, waitProgress(m.events)

	case finishedMsg:
		m.totals = Totals(msg)
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) absorb(p loop.Progress) {
	switch p.Kind {
	case "turn":
		m.turns = p.Turn
		m.lines = append(m.lines, line{kind: "turn", text: fmt.Sprintf("turn %d", p.Turn)})
		m.current = "thinking"
	case "text":
		m.lines = append(m.lines, line{kind: "text", text: p.Detail})
		m.current = "thinking"
	case "call":
		m.calls++
		m.lines = append(m.lines, line{kind: "call", text: p.Tool + " " + p.Detail})
		m.current = p.Tool
	case "result":
		if n := len(m.lines); n > 0 && m.lines[n-1].kind == "call" {
			m.lines[n-1].took = p.Elapsed
		}
		m.lines = append(m.lines, line{kind: "result", text: p.Detail, took: p.Elapsed})
	case "widened":
		m.lines = append(m.lines, line{kind: "note", text: p.Detail})
	case "interrupted":
		m.lines = append(m.lines, line{kind: "error", text: p.Detail})
	}
}

func (m model) View() string {
	w := m.width
	if w <= 0 {
		w = 90
	}
	var b strings.Builder

	b.WriteString(ink.Render("sentineldesk-agent") + dim.Render("  "+m.header.Model) + "\n")
	if m.header.Tools != "" {
		b.WriteString(dim.Render(m.header.Tools) + "\n")
	}
	if m.header.Goal != "" {
		b.WriteString(context.Render("› "+truncate(m.header.Goal, w-2)) + "\n")
	}
	b.WriteString(rule.Render(strings.Repeat("─", min(w, 80))) + "\n")

	// Only the tail is drawn. A long run produces more lines than a terminal
	// has, and Bubble Tea redraws the whole view every frame — printing all of
	// it would make the screen scroll on its own while somebody is reading it.
	show := m.lines
	if budget := maxLines(m.done); len(show) > budget {
		show = show[len(show)-budget:]
		b.WriteString(dim.Render(fmt.Sprintf("  … %d earlier lines\n", len(m.lines)-budget)))
	}
	for _, l := range show {
		switch l.kind {
		case "turn":
			b.WriteString(dim.Render("── "+l.text) + "\n")
		case "text":
			b.WriteString(ink.Render(wrap(l.text, w)) + "\n")
		case "call":
			b.WriteString(live.Render("  → ") + context.Render(truncate(l.text, w-6)))
			if l.took > 0 {
				b.WriteString(dim.Render("  " + l.took.Round(time.Millisecond).String()))
			}
			b.WriteString("\n")
		case "result":
			b.WriteString(dim.Render("    "+truncate(l.text, w-6)) + "\n")
		case "note":
			b.WriteString(live.Render("  + ") + context.Render(truncate(l.text, w-6)) + "\n")
		case "error":
			b.WriteString(bad.Render("  ! "+truncate(l.text, w-6)) + "\n")
		}
	}

	if !m.done {
		b.WriteString("\n" + live.Render(frames[m.spin%len(frames)]) + " " +
			context.Render(m.current) +
			dim.Render(fmt.Sprintf("   %d turns · %d calls · %s",
				m.turns, m.calls, time.Since(m.started).Round(time.Second))) + "\n")
		b.WriteString(dim.Render("ctrl-c to stop") + "\n")
		return b.String()
	}

	t := m.totals
	b.WriteString(rule.Render(strings.Repeat("─", min(w, 80))) + "\n")
	status := live.Render(t.Status)
	if t.Status != "finished" {
		status = bad.Render(t.Status)
	}
	b.WriteString(status + dim.Render(fmt.Sprintf(" · %d turns · %d calls · %s · %s in / %s out",
		t.Turns, t.Calls, t.Elapsed.Round(time.Second),
		comma(t.InputToks), comma(t.OutputToks))) + "\n")
	if t.CacheWriteToks > 0 || t.CacheReadToks > 0 {
		b.WriteString(dim.Render("cache: "+comma(t.CacheWriteToks)+" written, ") +
			live.Render(comma(t.CacheReadToks)+" read") + "\n")
	}
	if t.CostKnown {
		b.WriteString(dim.Render(fmt.Sprintf("est. cost: USD %.4f", t.Cost)))
	}
	if t.HistoryID > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("   ·   sentineldesk-agent history %d", t.HistoryID)))
	}
	b.WriteString("\n")
	return b.String()
}

// maxLines keeps the view inside a screen. Generous while running, because the
// recent calls are what somebody is watching; unbounded at the end would push
// the totals off the top, and the totals are the reason they waited.
func maxLines(done bool) int {
	if done {
		return 24
	}
	return 20
}

// Run draws the run and returns when it ends.
//
// events is closed by the caller when the loop is done; totals arrives once.
func Run(h Header, events <-chan loop.Progress, totals <-chan Totals) error {
	m := model{header: h, events: events, finish: totals, started: time.Now()}
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if n < 8 {
		n = 8
	}
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// wrap breaks prose at the terminal's width. Naive on purpose — it splits on
// spaces and counts runes — because the alternative pulls in a text-layout
// dependency to lay out a paragraph nobody will measure.
func wrap(s string, w int) string {
	if w < 20 {
		w = 20
	}
	var out []string
	for _, para := range strings.Split(strings.TrimSpace(s), "\n") {
		cur := ""
		for _, word := range strings.Fields(para) {
			if cur == "" {
				cur = word
			} else if len([]rune(cur))+1+len([]rune(word)) <= w {
				cur += " " + word
			} else {
				out = append(out, cur)
				cur = word
			}
		}
		out = append(out, cur)
	}
	return strings.Join(out, "\n")
}

func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, parts...), ",")
}
