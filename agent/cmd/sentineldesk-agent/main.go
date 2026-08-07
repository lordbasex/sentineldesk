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

// sentineldesk-agent drives the desktop without an AI host in the middle.
//
// Stage 2.1a is the transport and nothing above it: connect, read the
// catalogue, and prove the four server behaviours a naive client drops. No
// model is involved yet, deliberately — a loop built on an unproven connection
// fails in two places at once and neither of them says which.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/lordbasex/sentineldesk/agent/internal/mcpclient"
)

// version is stamped in at build time, like the desktop's.
var version = "dev"

const usage = `sentineldesk-agent — the SentinelDesk agent runtime

  sentineldesk-agent doctor          check the connection and what it can do
  sentineldesk-agent tools [query]   the catalogue, or a search of it

Reaching the desktop:
  --sock PATH        a unix socket, when running beside the desktop
  --container NAME   docker exec into a container (default: sentineldesk)
  --bin PATH         the sentineldesk binary inside it

`

func main() {
	fs := flag.NewFlagSet("sentineldesk-agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }

	sock := fs.String("sock", "/run/user/1000/sentineldesk-mcp.sock", "MCP socket inside the desktop")
	container := fs.String("container", "sentineldesk", "container to docker exec into; empty to connect directly")
	bin := fs.String("bin", "sentineldesk", "the sentineldesk binary")
	timeout := fs.Duration("timeout", 90*time.Second, "how long any one step may take")
	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println(version)
		return
	}
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}

	// Ctrl-C cancels the context rather than killing the process, so the
	// cancellation reaches the server as notifications/cancelled and the tool
	// in flight actually stops. Exiting instead would leave it running with
	// nobody to answer, which is the failure the server's cancellation was
	// built to end.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := connect(ctx, *container, *bin, *sock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not reach the desktop: %v\n", err)
		fmt.Fprintf(os.Stderr, "\nIs it running?  make up\n")
		os.Exit(1)
	}
	defer client.Close()

	runCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	switch args[0] {
	case "doctor":
		os.Exit(doctor(runCtx, client))
	case "tools":
		os.Exit(listTools(runCtx, client, strings.Join(args[1:], " ")))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		fs.Usage()
		os.Exit(2)
	}
}

// connect opens the transport and performs the handshake.
func connect(ctx context.Context, container, bin, sock string) (*mcpclient.Client, error) {
	var transport mcpclient.Transport
	var err error
	if container != "" {
		// The path an AI host takes. Testing through it is testing what ships.
		transport, err = mcpclient.DialStdio("docker", "exec", "-i", "-u", "sentineldesk",
			container, bin, "-mcp-stdio", "-mcp-sock", sock)
	} else {
		transport, err = mcpclient.DialStdio(bin, "-mcp-stdio", "-mcp-sock", sock)
	}
	if err != nil {
		return nil, err
	}
	client := mcpclient.New(transport)
	if err := client.Start(ctx, "sentineldesk-agent", version); err != nil {
		transport.Close()
		return nil, err
	}
	return client, nil
}

// --- doctor -----------------------------------------------------------------

type checks struct{ passed, failed int }

func (c *checks) ok(name string, pass bool, detail string) {
	if pass {
		c.passed++
		fmt.Printf("  \033[32m✓\033[0m %s\n", name)
		return
	}
	c.failed++
	fmt.Printf("  \033[31m✗\033[0m %s\n", name)
	if detail != "" {
		fmt.Printf("      %s\n", detail)
	}
}

func (c *checks) section(name string) { fmt.Printf("\n\033[1m%s\033[0m\n", name) }

// doctor proves the connection can do what the runtime will need of it.
//
// Every check here is about a server behaviour a naive client silently drops.
// They are worth proving separately from the loop because when a loop
// misbehaves the first question is whether the transport underneath it is
// sound, and answering that by reading the loop's logs is how a morning goes.
func doctor(ctx context.Context, c *mcpclient.Client) int {
	ck := &checks{}
	info := c.ServerInfo()
	fmt.Printf("Connected to %s %s (connection %d)\n", info.Name, info.Version, c.ConnectionID())

	ck.section("The catalogue")
	tools, err := c.ListTools(ctx)
	if err != nil {
		ck.ok("tools/list answers", false, err.Error())
		return report(ck)
	}
	ck.ok(fmt.Sprintf("tools/list returns a catalogue (%d tools)", len(tools)), len(tools) > 0, "")

	// The three annotations the runtime is built on. A server that publishes
	// none of them still works as an MCP server and cannot be driven by this
	// one: the role substitution has nothing to read, and asking for control at
	// the right moment becomes guesswork.
	var gated, byVisibility = 0, map[string]int{}
	for _, t := range tools {
		if t.Annotations.RequiresControl {
			gated++
		}
		byVisibility[t.Annotations.Visibility]++
	}
	ck.ok("tools declare whether they need the room's controls", gated > 0,
		"no tool published sentineldesk/requiresControl")
	ck.ok(fmt.Sprintf("tools declare visibility (hidden %d, visible %d, injects %d)",
		byVisibility["hidden"], byVisibility["visible"], byVisibility["injects"]),
		byVisibility[""] == 0,
		fmt.Sprintf("%d tools published no sentineldesk/visibility", byVisibility[""]))

	// The pair the whole role mechanism turns on. If these two ever stop being
	// classified apart, `witnessed` silently degrades to `efficient` and
	// nothing says so.
	var runCmd, terminalRun string
	for _, t := range tools {
		switch t.Name {
		case "run_command":
			runCmd = t.Annotations.Visibility
		case "terminal_run":
			terminalRun = t.Annotations.Visibility
		}
	}
	ck.ok("run_command and terminal_run are classified apart",
		runCmd == "hidden" && terminalRun == "injects",
		fmt.Sprintf("run_command is %q and terminal_run is %q", runCmd, terminalRun))

	ck.section("Denial kinds")
	// A tool that does not exist. The kind matters more than the failure: it is
	// what tells a runtime that retrying is pointless.
	res, err := c.Call(ctx, "no-such-tool-exists", nil)
	ck.ok("an unknown tool is refused with a kind",
		err == nil && res.IsError && res.Denial == mcpclient.DenialUnknownTool,
		fmt.Sprintf("got %q", res.Denial))

	// An argument the tool does not take. Refused rather than ignored — which
	// it silently was until stage 1, so a call could report success having
	// done something other than what was asked.
	res, err = c.Call(ctx, "wait", map[string]any{"ms": 1, "no_such_argument": true})
	ck.ok("an unknown argument is refused rather than ignored",
		err == nil && res.IsError && res.Denial == mcpclient.DenialBadArgs,
		fmt.Sprintf("got %q: %s", res.Denial, trunc(res.Text(), 120)))

	ck.section("Events")
	sub, err := c.Subscribe(ctx, mcpclient.TopicControl, mcpclient.TopicWindows)
	ck.ok("subscribe_events accepts a subscription", err == nil, errText(err))
	if err == nil {
		ck.ok("control events will actually be delivered",
			sub.Delivers(mcpclient.TopicControl),
			"no room on this desktop: "+strings.Join(sub.Unavailable, ", "))
		ck.ok("the event method is the one this client listens on",
			sub.Method == "notifications/sentineldesk/event",
			"server says "+sub.Method)
	}

	// A window opening is the cheapest real event to provoke, and provoking one
	// is the difference between "the server accepted a subscription" and "events
	// arrive".
	events := make(chan mcpclient.Event, 16)
	c.OnEvent = func(e mcpclient.Event) {
		select {
		case events <- e:
		default:
		}
	}
	if _, err := c.Call(ctx, "launch_app", map[string]any{
		"command": "xterm -T DOCTORPROBE -e sleep 12"}); err == nil {
		got := waitForEvent(events, mcpclient.TopicWindows, 12*time.Second)
		ck.ok("opening a window delivers an event", got, "nothing arrived in 12s")
		_, _ = c.Call(ctx, "run_command", map[string]any{
			"command": "pkill -f DOCTORPROBE || true"})
	}

	ck.section("Progress")
	// A command that runs for seconds must report while it runs, or a long tool
	// is indistinguishable from a hung one for as long as it takes.
	progress := make(chan mcpclient.Progress, 32)
	c.OnProgress = func(p mcpclient.Progress) {
		select {
		case progress <- p:
		default:
		}
	}
	_, _ = c.Call(ctx, "run_command", map[string]any{
		"command": "echo one; sleep 3; echo two", "timeout_ms": 20000})
	ck.ok("a command that ran for seconds reported while it ran",
		len(progress) > 0,
		"no progress arrived — the client is not asking for it, or the server is not sending it")
	// The command's own output is the only honest progress it has, so that is
	// what the message should carry. A tick with no content says "still alive"
	// and nothing else, which is worth less than it looks.
	sawOutput := false
	for len(progress) > 0 {
		if p := <-progress; strings.Contains(p.Message, "one") || strings.Contains(p.Message, "two") {
			sawOutput = true
		}
	}
	ck.ok("progress carries the command's own output", sawOutput,
		"the reports arrived empty of anything the command printed")

	ck.section("Cancellation")
	// A cancel that returns before the tool stopped is worse than no cancel: it
	// tells the operator something untrue at the moment they most need the
	// truth. This proves the call comes back promptly; the server's own tests
	// prove the process really dies.
	cancelCtx, cancelIt := context.WithCancel(ctx)
	started := time.Now()
	done := make(chan struct{})
	go func() {
		_, _ = c.Call(cancelCtx, "run_command", map[string]any{
			"command": "sleep 30", "timeout_ms": 60000})
		close(done)
	}()
	time.Sleep(700 * time.Millisecond)
	cancelIt()
	select {
	case <-done:
		ck.ok("a cancelled call returns promptly",
			time.Since(started) < 10*time.Second,
			fmt.Sprintf("took %v", time.Since(started)))
	case <-time.After(15 * time.Second):
		ck.ok("a cancelled call returns promptly", false, "it did not return at all")
	}

	ck.section("Provenance")
	// The audit trail groups by these. The server cannot derive either.
	task := "doctor-" + fmt.Sprint(c.ConnectionID())
	c.SetTask(task, "checking the connection")
	// A call FIRST, then read the log. Reading it as the first call after
	// SetTask asks the log to contain an entry that is still being made: the
	// server writes it after dispatch returns, so action_log cannot see itself.
	_, _ = c.Call(ctx, "wait", map[string]any{"ms": 1})
	res, err = c.Call(ctx, "action_log", map[string]any{"limit": 10})
	ck.ok("the action log is readable", err == nil && !res.IsError, errText(err))
	if err == nil && !res.IsError {
		ck.ok("calls are recorded with this run's task id",
			strings.Contains(res.Text(), task),
			"the log has no entry carrying the task id just set")
	}

	return report(ck)
}

func report(ck *checks) int {
	fmt.Println()
	if ck.failed == 0 {
		fmt.Printf("\033[32mall %d checks passed\033[0m\n", ck.passed)
		return 0
	}
	fmt.Printf("\033[31m%d of %d checks failed\033[0m\n", ck.failed, ck.passed+ck.failed)
	return 1
}

func waitForEvent(ch <-chan mcpclient.Event, topic mcpclient.EventTopic, within time.Duration) bool {
	deadline := time.After(within)
	for {
		select {
		case e := <-ch:
			if e.Topic == topic {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// --- tools ------------------------------------------------------------------

// listTools prints the catalogue, or searches it.
//
// The search runs here rather than through the server's tool_search: the
// runtime already holds the catalogue after tools/list, and asking the server
// would be a round trip to have it answer a question this side can answer from
// what it is already holding. See ADR-003 — the server keeps its own for the
// hosts that have no runtime of ours.
func listTools(ctx context.Context, c *mcpclient.Client, query string) int {
	tools, err := c.ListTools(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tools/list: %v\n", err)
		return 1
	}
	if query != "" {
		// Placeholder ranking until the server's is shared out (ADR-003): a
		// substring match, which is honest about being one. It is here so
		// `tools <query>` exists and is measured against the real ranking in
		// 2.1c rather than being invented twice.
		var kept []mcpclient.Tool
		q := strings.ToLower(query)
		for _, t := range tools {
			if strings.Contains(strings.ToLower(t.Name+" "+t.Description), q) {
				kept = append(kept, t)
			}
		}
		tools = kept
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	for _, t := range tools {
		risk := "write"
		switch {
		case t.Annotations.ReadOnly:
			risk = "read"
		case t.Annotations.Destructive:
			risk = "danger"
		}
		gate := " "
		if t.Annotations.RequiresControl {
			gate = "*"
		}
		fmt.Printf("%s %-22s %-7s %-8s %s\n",
			gate, t.Name, risk, t.Annotations.Visibility, trunc(t.Description, 78))
	}
	fmt.Printf("\n%d tools   (* needs the room's controls)\n", len(tools))
	return 0
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
