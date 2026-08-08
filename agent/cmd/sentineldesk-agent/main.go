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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/lordbasex/sentineldesk/agent/internal/loop"
	"github.com/lordbasex/sentineldesk/agent/internal/mcpclient"
	"github.com/lordbasex/sentineldesk/agent/internal/provider"
	"github.com/lordbasex/sentineldesk/agent/internal/skills"
	"github.com/lordbasex/sentineldesk/agent/internal/store"
	"github.com/lordbasex/sentineldesk/agent/internal/tui"
	"github.com/lordbasex/sentineldesk/agent/prompts"
	"github.com/lordbasex/sentineldesk/internal/toolsearch"
)

// version is stamped in at build time, like the desktop's.
var version = "dev"

const usage = `sentineldesk-agent — the SentinelDesk agent runtime

  sentineldesk-agent doctor          check the connection and what it can do
  sentineldesk-agent tools [query]   the catalogue, or a search of it
  sentineldesk-agent run "goal"      work towards a goal
  sentineldesk-agent providers       which models can be reached, and what each needs
  sentineldesk-agent costs           what has been spent, and on what
  sentineldesk-agent history [id]    past runs, or one run in full
  sentineldesk-agent roles           the ways of working available, and what each sets
  sentineldesk-agent skills          the skills found, and where each came from
  sentineldesk-agent skills install  add one from the ecosystem (needs npx)

The model:
  --provider   anthropic | ollama | ollama-cloud | openai | openrouter
  --role       how the work should be done; the choices are the files in
               agent/prompts/roles, plus anything in ~/.sentineldesk/prompts/roles
  --model      which model to use
  --max-turns  stop after this many (default 25)
  --tools      how many goal-matched tools on top of the core set (0 = all of them)

The catalogue is 98% of what a turn costs, so a run starts with a core set plus
whatever the goal ranks highest, and reaches the rest through tool_search.

The API key is read from ~/.sentineldesk/anthropic.key (chmod 600), or from
ANTHROPIC_API_KEY_FILE. Never pass it on the command line.

Continuing a conversation:
  --resume <id>   pick a past run back up; 0 is the most recent, and the ids
                  come from the history command. The model is told how long ago
                  it stopped, and that the desktop kept running without it.

How it is shown:
  (default)    a live view of the run
  --debug      one line per event instead — also what a pipe or a CI log gets,
               with or without the flag, because a live view writes cursor
               movements and a pipe receives them as garbage

Reaching the desktop:
  --sock PATH        the daemon's MCP socket (the default; this runs beside it)
  --container NAME   develop from another machine, through docker exec

`

func main() {
	fs := flag.NewFlagSet("sentineldesk-agent", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }

	sock := fs.String("sock", "/run/user/1000/sentineldesk-mcp.sock", "the daemon's MCP socket")
	container := fs.String("container", "", "develop from another machine: docker exec into this container instead of opening the socket")
	bin := fs.String("bin", "sentineldesk", "the sentineldesk binary, for -container")
	timeout := fs.Duration("timeout", 90*time.Second, "how long any one step may take")
	// The choices come from the prompts directory rather than from a literal
	// here, so a role somebody dropped in ~/.sentineldesk/prompts/roles is one
	// --help lists. A flag that documents only what was compiled in makes a
	// working feature look like it does not exist.
	role := fs.String("role", "efficient", strings.Join(prompts.Roles(), " | "))
	providerName := fs.String("provider", "anthropic", "which provider (see `providers`)")
	model := fs.String("model", "", "which model (default: the provider's)")
	maxTurns := fs.Int("max-turns", 25, "stop a run after this many turns")
	toolLimit := fs.Int("tools", 12, "how many goal-matched tools to add to the core set; 0 offers all of them")
	// Whether --tools was chosen or inherited. It decides what happens when the
	// ranking finds nothing: offering everything is the right recovery against
	// a cached hosted model and the wrong one against a CPU, so a number the
	// caller actually asked for is honoured either way.
	toolsCapped := false
	// The live view is the default face. -debug is the old one: one line per
	// event, which is what somebody debugging the RUNTIME wants rather than
	// somebody watching the TASK.
	debug := fs.Bool("debug", false, "print one line per event instead of drawing the live view")
	// Continue a conversation instead of starting one. 0 means the most recent
	// run; a number names one from `history`.
	resume := fs.Int64("resume", -1, "continue a past run: an id from `history`, or 0 for the most recent")
	showVersion := fs.Bool("version", false, "print the version and exit")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println(version)
		return
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tools" {
			toolsCapped = true
		}
	})
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

	// These read the local database and never touch the desktop. Requiring a
	// running container to ask what something cost would be an odd thing to
	// insist on.
	switch args[0] {
	case "costs":
		os.Exit(showCosts())
	case "history":
		os.Exit(showHistory(strings.Join(args[1:], " ")))
	case "providers":
		os.Exit(showProviders())
	case "roles":
		os.Exit(showRoles())
	case "skills":
		if len(args) > 1 && args[1] == "install" {
			os.Exit(installSkill(args[2:]))
		}
		os.Exit(showSkills())
	}

	client, how, err := connect(ctx, *container, *bin, *sock)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not reach the desktop over %s: %v\n", how, err)
		if *container == "" {
			fmt.Fprintf(os.Stderr, "\nThis expects to run beside the daemon, as the desktop user.\n"+
				"From another machine, use  -container sentineldesk\n")
		} else {
			fmt.Fprintf(os.Stderr, "\nIs it running?  make up\n")
		}
		os.Exit(1)
	}
	defer client.Close()

	runCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	switch args[0] {
	case "doctor":
		os.Exit(doctor(runCtx, client, how))
	case "tools":
		os.Exit(listTools(runCtx, client, strings.Join(args[1:], " ")))
	case "run":
		goal := strings.TrimSpace(strings.Join(args[1:], " "))
		if goal == "" {
			fmt.Fprintln(os.Stderr, "run needs a goal, in quotes")
			os.Exit(2)
		}
		// A run is bounded by the person at the keyboard, not by the -timeout
		// meant for one step. Ctrl-C still stops it, and stops it properly:
		// the cancellation reaches the server and the tool in flight ends.
		os.Exit(runGoal(ctx, client, goal, *providerName, *role, *model,
			*maxTurns, *toolLimit, toolsCapped, *debug, *resume))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		fs.Usage()
		os.Exit(2)
	}
}

// connect opens the transport and performs the handshake.
//
// The socket is the default because that is where this runs: a supervised
// process inside the container, beside the daemon, as the same user (ADR-004).
// Nothing is spawned and nothing has to be reaped.
//
// -container is the development path, for driving a desktop from a machine that
// is not it. It is opt-in rather than a fallback: a runtime that quietly
// reached for docker when its socket was missing would hide a misconfiguration
// on the box where it matters, and "it worked on the laptop" is exactly how.
func connect(ctx context.Context, container, bin, sock string) (*mcpclient.Client, string, error) {
	var transport mcpclient.Transport
	var err error
	how := "socket " + sock
	if container != "" {
		how = "docker exec " + container
		transport, err = mcpclient.DialStdio("docker", "exec", "-i", "-u", "sentineldesk",
			container, bin, "-mcp-stdio", "-mcp-sock", sock)
	} else {
		transport, err = mcpclient.DialUnix(sock)
	}
	if err != nil {
		return nil, how, err
	}
	client := mcpclient.New(transport)
	if err := client.Start(ctx, "sentineldesk-agent", version); err != nil {
		transport.Close()
		return nil, how, err
	}
	return client, how, nil
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
func doctor(ctx context.Context, c *mcpclient.Client, how string) int {
	ck := &checks{}
	info := c.ServerInfo()
	fmt.Printf("Connected to %s %s over %s (connection %d)\n",
		info.Name, info.Version, how, c.ConnectionID())

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
		// The real ranking, shared with the server through internal/toolsearch
		// (ADR-003) and measured at 100% top ten. Answered here rather than over
		// the socket because this side is already holding the catalogue.
		flat := make([]toolsearch.Tool, len(tools))
		byName := make(map[string]mcpclient.Tool, len(tools))
		for i, t := range tools {
			flat[i] = toolsearch.Tool{Name: t.Name, Description: t.Description}
			byName[t.Name] = t
		}
		var kept []mcpclient.Tool
		for _, hit := range toolsearch.Rank(flat, query, 15) {
			kept = append(kept, byName[hit.Name])
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

// --- run --------------------------------------------------------------------

// runGoal works towards a goal and narrates what it does.
//
// The narration is not decoration. This is the first command where something
// other than the operator decides what happens next, and a run whose steps are
// invisible is one nobody can trust or debug — which is the same argument the
// server's own Visibility field is built on, one layer up.
func runGoal(ctx context.Context, c *mcpclient.Client, goal, providerName, role, model string, maxTurns, toolLimit int, toolsCapped, debug bool, resume int64) int {
	// The role is read first because it may decide what runs. A role that names
	// a model and a policy is a saved way of working, not a paragraph — and the
	// point of the header is that asking for it configures the run rather than
	// asking the model to behave as if it had been configured.
	def, err := prompts.Role(role)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	var prior store.Resumable
	var priorMsgs []provider.Message
	if resume >= 0 {
		db, ok := openStore()
		if !ok {
			fmt.Fprintln(os.Stderr, "cannot resume: the run database would not open")
			return 1
		}
		prior, err = db.Resume(resume)
		db.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		if err := json.Unmarshal([]byte(prior.Messages), &priorMsgs); err != nil {
			fmt.Fprintf(os.Stderr, "run %d's history cannot be read back: %v\n", prior.RunID, err)
			return 1
		}
		// The response the request did not contain. Without it the model is
		// handed a conversation ending in tool results it never got to answer,
		// and repeats the answer it already gave.
		if prior.Answer != "" {
			priorMsgs = append(priorMsgs, provider.Message{
				Role: provider.RoleAssistant, Text: prior.Answer})
		}
		// A resumed run keeps what it was, unless the caller overrode it. The
		// cheapest way to make a continuation incoherent is to continue it with
		// a different model and a different policy.
		if model == "" && prior.Model != "" {
			if p, m, found := strings.Cut(prior.Model, "/"); found {
				providerName, model = p, m
			}
		}
		if role == "efficient" && prior.Role != "" {
			role = prior.Role
			def, err = prompts.Role(role)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				return 2
			}
		}
	}

	// An explicit flag beats the role. Somebody who typed --model meant it.
	if def.Model != "" && model == "" {
		if p, m, found := strings.Cut(def.Model, "/"); found {
			providerName, model = p, m
		} else {
			model = def.Model
		}
	}
	if def.Tools != 0 && !toolsCapped {
		toolLimit = def.Tools
		if def.Tools < 0 {
			toolLimit = 0
		}
		toolsCapped = true
	}
	// The live view by default, the line-by-line transcript when asked for it
	// — and the transcript regardless when stdout is not a terminal. A TUI
	// writes cursor movements, and a pipe receives them as garbage: every
	// measurement this project has ever taken was made with the agent piped
	// into something, and none of them would survive being asked to render.
	live := !debug && tui.Usable()

	llm, err := provider.Open(providerName, model)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if provider.IsUnavailable(err) {
			// Not configured is not broken, and the difference decides what the
			// operator does next.
			return 3
		}
		return 1
	}
	source := "?"
	if k, ok := llm.(provider.KeySourced); ok {
		source = k.KeySource()
	}
	hdr := tui.Header{Model: fmt.Sprintf("%s   key: %s", llm.Name(), source), Goal: goal}
	if !live {
		fmt.Printf("Model: %s   key: %s\n", llm.Name(), source)
	}

	catalogue, err := c.ListTools(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tools/list: %v\n", err)
		return 1
	}
	// Chosen once and left alone: caching matches a byte-identical prefix, so a
	// set that changed each turn would cost more than sending everything.
	selection := loop.Select(catalogue, goal, toolLimit, toolsCapped)
	if line := selection.Describe(); line != "" {
		hdr.Tools = line
		if !live {
			fmt.Printf("Tools: %s\n", line)
		}
	}

	// A task id, so every call this run makes is one group in the server's
	// audit trail rather than a scatter of rows. The goal goes with it: the
	// server can see what was called and never why.
	task := fmt.Sprintf("run-%d-%d", c.ConnectionID(), time.Now().Unix())
	c.SetTask(task, goal)

	// The record. A database that will not open costs the accounting and
	// nothing else: the run goes ahead, unrecorded, and says so.
	var recorder loop.Recorder
	var run *store.Run
	if db, ok := openStore(); ok {
		defer db.Close()
		if r, err := db.StartRun(task, goal, llm.Name(), role); err != nil {
			fmt.Fprintf(os.Stderr, "the run will not be recorded: %v\n", err)
		} else {
			run, recorder = r, r
		}
	}

	// Buffered rather than dropping. A dropped event is a line of the model's
	// own answer that nobody ever sees, and the screen drains far faster than
	// a provider produces turns — if this ever fills, blocking the loop for a
	// moment is the right thing to do with it.
	events := make(chan loop.Progress, 256)
	emit := narrate
	if live {
		emit = func(p loop.Progress) { events <- p }
	}

	// Skills, found beside the work and in the home directory. Problems are
	// reported and not fatal: a SKILL.md that will not parse is one skill
	// missing, and stopping the run over it would be a worse trade.
	found, problems := skills.Load()
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "skill ignored — %v\n", p)
	}
	if len(found) > 0 && !live {
		names := make([]string, len(found))
		for i, s := range found {
			names[i] = s.Name
		}
		fmt.Printf("Skills: %s\n", strings.Join(names, ", "))
	}

	runner := loop.New(c, loop.Options{
		Role:      loop.Role(role),
		Model:     llm,
		Tools:     selection.Tools,
		Catalogue: catalogue,
		MaxTurns:  maxTurns,
		OnEvent:   emit,
		Skills:    found,
		Resume:    priorMsgs,
		StaleFor:  staleFor(prior),
		Recorder:  recorder,
	})

	// Narrowed before the first turn rather than trusted to the prompt. This is
	// what makes a role like `readonly` a control instead of a request: the
	// server refuses the call, and it may only ever narrow — a role cannot
	// widen past the daemon's own ceiling, which is what makes handing one out
	// to somebody else safe.
	if def.Policy != "" || len(def.Deny) > 0 || len(def.Allow) > 0 {
		level := def.Policy
		if level == "" {
			level = "full"
		}
		if _, err := c.Restrict(ctx, level, def.Deny, def.Allow); err != nil {
			// Fatal. A run that asked to be constrained and was not is the one
			// case where carrying on is worse than stopping: whoever chose the
			// role chose it for a reason, and they would not learn it had been
			// ignored until something had already been changed.
			fmt.Fprintf(os.Stderr, "the role %q could not be applied: %v\n", role, err)
			return 1
		}
		if !live {
			fmt.Printf("Policy: %s (role %s)\n", level, role)
		}
	}

	// Losing the controls has to reach the loop while it is running, not at the
	// end. Subscribing before the first turn rather than after is the
	// difference between being told and finding out.
	c.OnEvent = runner.NoteEvent
	if sub, err := c.Subscribe(ctx, mcpclient.TopicControl); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not subscribe to control events: %v\n", err)
	} else if !sub.Delivers(mcpclient.TopicControl) {
		fmt.Fprintln(os.Stderr, "warning: control events will not fire on this desktop; "+
			"a person taking the controls will surface as a refused call instead")
	}

	hdr.Task = task
	if !live {
		fmt.Printf("Task:  %s\n\n", task)
	}
	started := time.Now()

	var res loop.Result

	if live {
		// The loop runs beside the screen rather than under it. Bubble Tea owns
		// the terminal and its own event loop, so the work cannot be done inside
		// Update() — and it must not be: a provider call that takes ninety
		// seconds would freeze the spinner that exists to say it is still going.
		totals := make(chan tui.Totals, 1)
		go func() {
			res, err = runner.Run(ctx, goal)
			// Closing the events channel before sending the totals, so the view
			// has drained every line before it is told the run is over.
			close(events)
			totals <- finishRun(run, llm.Name(), started, res, err)
		}()
		if uiErr := tui.Run(hdr, events, totals); uiErr != nil {
			fmt.Fprintf(os.Stderr, "the live view failed: %v\n", uiErr)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "the run failed: %v\n", err)
			return 1
		}
		if res.Interrupted {
			return 4
		}
		return 0
	}

	res, err = runner.Run(ctx, goal)

	status := "finished"
	switch {
	case err != nil:
		status = "failed"
	case res.Interrupted:
		status = "interrupted"
	}
	if run != nil {
		_ = run.Finish(status, res.Answer, res.Turns, res.Calls,
			res.InputToks, res.OutputToks, res.CacheWriteToks, res.CacheReadToks)
	}

	fmt.Printf("\n─────\n")
	if err != nil {
		fmt.Fprintf(os.Stderr, "the run failed: %v\n", err)
		fmt.Printf("%d turns, %d calls, %v\n", res.Turns, res.Calls, time.Since(started).Round(time.Second))
		return 1
	}
	// The answer is NOT printed here. narrate() already put every line of it on
	// screen as the model produced it, so this block repeated the whole thing
	// verbatim a few lines below itself — every run, twice, and more so now that
	// res.Answer carries every turn's prose rather than only the last one.
	//
	// What belongs under the rule is what the transcript above cannot say: how
	// long it took and what it cost. The answer is still stored on the run, and
	// `sentineldesk-agent history <id>` is where somebody goes to read it back.
	fmt.Printf("%s · %d turns · %d calls · %v · %s in / %s out tokens\n",
		status, res.Turns, res.Calls, time.Since(started).Round(time.Second),
		comma(res.InputToks), comma(res.OutputToks))
	if res.CacheWriteToks > 0 || res.CacheReadToks > 0 {
		fmt.Printf("cache: %s written, \033[32m%s read\033[0m "+
			"(read tokens are billed at a fraction of the rest)\n",
			comma(res.CacheWriteToks), comma(res.CacheReadToks))
	}
	if run != nil {
		pricing := store.LoadPricing()
		if cost, known := pricing.EstimatedWithCache(llm.Name(),
			res.InputToks, res.OutputToks, res.CacheWriteToks, res.CacheReadToks); known {
			fmt.Printf("est. cost: USD %.4f   ·   history: sentineldesk-agent history %d\n",
				cost, run.ID)
		} else {
			fmt.Printf("history: sentineldesk-agent history %d\n", run.ID)
		}
	}
	if res.Interrupted {
		return 4
	}
	return 0
}

// narrate prints what the loop is doing, one line per thing.
func narrate(p loop.Progress) {
	switch p.Kind {
	case "turn":
		fmt.Printf("\033[2m── turn %d\033[0m\n", p.Turn)
	case "text":
		fmt.Printf("%s\n", p.Detail)
	case "call":
		fmt.Printf("  \033[36m→\033[0m %s %s\n", p.Tool, dim(p.Detail))
	case "result":
		fmt.Printf("  \033[2m  %s (%v)\033[0m\n", trunc(p.Detail, 100), p.Elapsed.Round(time.Millisecond))
	case "widened":
		fmt.Printf("  \033[35m+\033[0m %s\n", p.Detail)
	case "interrupted":
		fmt.Printf("\n\033[33m■ %s\033[0m\n", p.Detail)
	}
}

func dim(s string) string {
	if s == "" {
		return ""
	}
	return "\033[2m" + s + "\033[0m"
}

// --- what it cost -----------------------------------------------------------

func openStore() (*store.DB, bool) {
	db, err := store.Open("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "the run will not be recorded: %v\n", err)
		return nil, false
	}
	return db, true
}

// showCosts prints the ledger.
//
// Tokens first and money second, in that order and never the other way round.
// The counts are what the provider reported and are exact; the money is derived
// from a rate this program cannot be authoritative about, so it is labelled and
// the operator is told where to correct it. A number presented as more certain
// than it is, about somebody's bill, is worse than no number.
func showCosts() int {
	db, ok := openStore()
	if !ok {
		return 1
	}
	defer db.Close()

	totals, err := db.Totals()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if len(totals) == 0 {
		fmt.Println("Nothing recorded yet.")
		return 0
	}
	pricing := store.LoadPricing()

	fmt.Printf("\033[1m%-28s %6s %6s %6s %12s %12s %10s\033[0m\n",
		"MODEL", "RUNS", "TURNS", "CALLS", "IN", "OUT", "EST. USD")
	var grandIn, grandOut int
	var grandCost float64
	unpriced := false
	for _, t := range totals {
		cost, known := pricing.EstimatedWithCache(t.Model, t.InputTokens, t.OutTokens,
			t.CacheWrite, t.CacheRead)
		money := fmt.Sprintf("%10.4f", cost)
		if !known {
			money = "         ?"
			unpriced = true
		}
		fmt.Printf("%-28s %6d %6d %6d %12s %12s %s\n",
			t.Model, t.Runs, t.Turns, t.Calls,
			comma(t.InputTokens), comma(t.OutTokens), money)
		grandIn += t.InputTokens
		grandOut += t.OutTokens
		grandCost += cost
	}
	fmt.Printf("\033[1m%-28s %6s %6s %6s %12s %12s %10.4f\033[0m\n",
		"total", "", "", "", comma(grandIn), comma(grandOut), grandCost)

	fmt.Printf("\nRates: %s", pricing.Source())
	if pricing.Source() == "built-in estimates" {
		fmt.Printf("  \033[33m(estimates — put the real ones in ~/.sentineldesk/pricing.json)\033[0m")
	}
	fmt.Println()
	if unpriced {
		fmt.Println("\033[33mSome models have no rate, so the total is lower than the truth.\033[0m")
	}

	// The number the tool-selection work has to move, which is the reason half
	// of this table exists.
	if c, err := db.CatalogueCost(); err == nil && c.Turns > 0 {
		fmt.Printf("\n\033[1mWhat the catalogue costs\033[0m\n")
		fmt.Printf("  %.0f tools per turn  ·  ~%s tokens  ·  \033[1m%.0f%% of every turn's input\033[0m\n",
			c.AvgOffered, comma(int(c.AvgCatalogueToks)), c.Share()*100)
		fmt.Printf("  the conversation itself is ~%s tokens; the rest is schema, re-sent each turn\n",
			comma(int(c.AvgProseToks)))
		if cost, known := pricing.Estimated(totals[0].Model,
			int(c.AvgCatalogueToks)*c.Turns, 0); known {
			fmt.Printf("  \033[33m~USD %.4f of what is above was spent re-sending it\033[0m\n", cost)
		}
	}

	fmt.Printf("\n%s\n", db.Path())
	return 0
}

// showHistory lists past runs, or opens one.
func showHistory(arg string) int {
	db, ok := openStore()
	if !ok {
		return 1
	}
	defer db.Close()

	if arg == "" {
		runs, err := db.Recent(20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		if len(runs) == 0 {
			fmt.Println("Nothing recorded yet.")
			return 0
		}
		pricing := store.LoadPricing()
		fmt.Printf("\033[1m%4s %-20s %-11s %5s %5s %10s %9s  %s\033[0m\n",
			"ID", "WHEN", "STATUS", "TURNS", "CALLS", "TOKENS", "EST. USD", "GOAL")
		for _, r := range runs {
			cost, _ := pricing.EstimatedWithCache(r.Model, r.InputTokens, r.OutTokens,
				r.CacheWrite, r.CacheRead)
			fmt.Printf("%4d %-20s %-11s %5d %5d %10s %9.4f  %s\n",
				r.ID, shortTime(r.StartedAt), r.Status, r.Turns, r.Calls,
				comma(r.InputTokens+r.OutTokens), cost, trunc(r.Goal, 44))
		}
		fmt.Printf("\nOne run in full:  sentineldesk-agent history <id>\n")
		return 0
	}

	var id int64
	if _, err := fmt.Sscanf(arg, "%d", &id); err != nil {
		fmt.Fprintf(os.Stderr, "history takes a run id from `history` with no argument\n")
		return 2
	}
	turns, err := db.Turns(id)
	if err != nil || len(turns) == 0 {
		fmt.Fprintf(os.Stderr, "run %d has no turns recorded\n", id)
		return 1
	}
	calls, _ := db.Calls(id)

	for _, t := range turns {
		fmt.Printf("\n\033[1m── turn %d\033[0m  %s  %dms  %s in / %s out  (%d tools offered)\n",
			t.N, shortTime(t.At), t.ElapsedMS,
			comma(t.InputTokens), comma(t.OutTokens), t.ToolsOffered)
		if t.N == 1 && t.System != "" {
			fmt.Printf("\n\033[2m  system prompt (%d chars):\033[0m\n", len(t.System))
			for _, line := range strings.Split(strings.TrimSpace(t.System), "\n") {
				fmt.Printf("  \033[2m│ %s\033[0m\n", line)
			}
		}
		for _, c := range calls {
			if c.TurnN != t.N {
				continue
			}
			from := ""
			if c.AskedFor != "" {
				from = fmt.Sprintf(" \033[33m(asked for %s)\033[0m", c.AskedFor)
			}
			mark := "→"
			if c.IsError {
				mark = "\033[31m✗\033[0m"
			}
			fmt.Printf("  %s %s %s%s\n", mark, c.Tool, dim(trunc(c.Args, 100)), from)
			if c.Denial != "" {
				fmt.Printf("      \033[33mdenied: %s\033[0m\n", c.Denial)
			}
			fmt.Printf("      \033[2m%s\033[0m\n", trunc(c.Result, 160))
		}
	}
	fmt.Printf("\n\033[2mFull request and response JSON is in the database:\n"+
		"  sqlite3 %s \"SELECT request, response FROM turns WHERE run_id=%d\"\033[0m\n",
		db.Path(), id)
	return 0
}

func comma(n int) string {
	s := fmt.Sprint(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func shortTime(rfc string) string {
	t, err := time.Parse(time.RFC3339Nano, rfc)
	if err != nil {
		return rfc
	}
	return t.Format("2006-01-02 15:04:05")
}

// --- providers --------------------------------------------------------------

// showProviders lists what can be reached and what each one needs.
//
// It says whether a key is actually present, not merely whether one is
// required. "You need a key" and "the key is missing" are the same sentence to
// somebody who has already put one in the wrong place.
func showProviders() int {
	fmt.Printf("\033[1m%-14s %-9s %-26s %s\033[0m\n", "PROVIDER", "KEY", "DEFAULT MODEL", "CACHING")

	// Anthropic is its own adapter rather than a preset: its wire format
	// differs, and its caching is explicit.
	anthropicKey, _ := provider.LoadKey("anthropic")
	fmt.Printf("%-14s %-9s %-26s %s\n", "anthropic",
		keyState(anthropicKey), provider.DefaultAnthropicModel, "explicit markers")

	for _, p := range provider.Presets {
		state := "\033[32mnot needed\033[0m"
		if p.KeyName != "" {
			key, _ := provider.LoadKey(p.KeyName)
			state = keyState(key)
		}
		caching := "none"
		if p.Caps.Caching {
			caching = "automatic"
		}
		fmt.Printf("%-14s %-9s %-26s %s\n", p.ID, state, p.DefaultModel, caching)
	}

	fmt.Println()
	fmt.Printf("  \033[1manthropic\033[0m     Explicit cache markers, which this runtime sets: the prompt and\n")
	fmt.Printf("                catalogue are cached and re-read at a fraction of the rate.\n")
	for _, p := range provider.Presets {
		fmt.Printf("  \033[1m%-13s\033[0m %s\n", p.ID, wrap(p.Note, 64, 16))
	}
	fmt.Printf("\nKeys live in ~/.sentineldesk/<name>.key, chmod 600, outside any checkout.\n")
	fmt.Printf("Example:  sentineldesk-agent --provider ollama --model qwen3:8b run \"…\"\n")
	return 0
}

func keyState(k provider.Secret) string {
	if k.Empty() {
		return "\033[33mmissing\033[0m"
	}
	return "\033[32mpresent\033[0m"
}

// wrap breaks a note across lines, indented to line up under the first.
func wrap(s string, width, indent int) string {
	var out, line strings.Builder
	for _, word := range strings.Fields(s) {
		if line.Len()+len(word)+1 > width {
			out.WriteString(line.String() + "\n" + strings.Repeat(" ", indent))
			line.Reset()
		}
		if line.Len() > 0 {
			line.WriteString(" ")
		}
		line.WriteString(word)
	}
	out.WriteString(line.String())
	return out.String()
}

// finishRun records how a run ended and packages what the live view shows once
// it is over. The same numbers the line output prints under its rule — kept in
// one place so the two faces cannot disagree about what a run cost.
func finishRun(run *store.Run, modelName string, started time.Time, res loop.Result, err error) tui.Totals {
	status := "finished"
	switch {
	case err != nil:
		status = "failed"
	case res.Interrupted:
		status = "interrupted"
	}
	t := tui.Totals{
		Status: status, Turns: res.Turns, Calls: res.Calls,
		InputToks: res.InputToks, OutputToks: res.OutputToks,
		CacheWriteToks: res.CacheWriteToks, CacheReadToks: res.CacheReadToks,
		Elapsed: time.Since(started), Answer: res.Answer,
	}
	if run != nil {
		_ = run.Finish(status, res.Answer, res.Turns, res.Calls,
			res.InputToks, res.OutputToks, res.CacheWriteToks, res.CacheReadToks)
		t.HistoryID = int64(run.ID)
		pricing := store.LoadPricing()
		if cost, known := pricing.EstimatedWithCache(modelName,
			res.InputToks, res.OutputToks, res.CacheWriteToks, res.CacheReadToks); known {
			t.Cost, t.CostKnown = cost, true
		}
	}
	return t
}

// showSkills lists what was found and where, because "it is not being picked
// up" is the whole failure mode of a convention with seven search paths. Saying
// which file won is the difference between a working feature and one somebody
// gives up on.
func showSkills() int {
	found, problems := skills.Load()
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "\033[31m✗\033[0m %v\n", p)
	}
	if len(found) == 0 {
		fmt.Println("No skills found.")
		fmt.Println("\nA skill is a directory with a SKILL.md in it. Searched, in this order:")
		fmt.Println("  ./.sentineldesk/skills/<name>/SKILL.md      this project")
		fmt.Println("  ./.claude/skills/<name>/SKILL.md            this project, Claude Code's layout")
		fmt.Println("  ./.agents/skills/<name>/SKILL.md            this project")
		fmt.Println("  ./.opencode/skills/<name>/SKILL.md          this project, opencode's layout")
		fmt.Println("  ~/.sentineldesk/skills/<name>/SKILL.md      every project")
		fmt.Println("  ~/.claude/skills/<name>/SKILL.md            every project")
		fmt.Println("  ~/.config/opencode/skills/<name>/SKILL.md   every project")
		fmt.Println("\nThe file starts with YAML frontmatter carrying name and description,")
		fmt.Println("then the instructions as Markdown. The description is what the agent")
		fmt.Println("sees; it reads the rest only when it decides the skill applies.")
		return 0
	}
	for _, s := range found {
		where := "global"
		if s.Project {
			where = "project"
		}
		fmt.Printf("\033[1m%s\033[0m  \033[2m(%s)\033[0m\n", s.Name, where)
		fmt.Printf("  %s\n", s.Description)
		fmt.Printf("  \033[2m%s · %d bytes of instructions\033[0m\n\n", s.Path, len(s.Body))
	}
	fmt.Printf("%d skill(s). Only these summaries go in the prompt; the agent calls\n", len(found))
	fmt.Printf("skill_read to get the instructions for the one it decides it needs.\n")
	return 0
}

// skillsRegistry is where the ecosystem's skills are catalogued, and the CLI
// that installs from it.
const (
	skillsRegistry = "https://skills.sh"
	skillsCLI      = "skills@latest"
	findSkills     = "https://github.com/vercel-labs/skills"
)

// installSkill hands the work to `npx skills`, which is the CLI the ecosystem
// already has, rather than growing a second one here.
//
// Writing our own installer would mean re-implementing repository fetching,
// version pinning and the per-agent path map for seventy-odd agents — to end up
// putting a Markdown file in a directory we already read. The whole point of
// following the convention was not having to own that.
//
// It is not run silently. A skill is instructions the agent will follow with
// whatever permissions it holds, which is exactly the property that makes the
// ecosystem useful and exactly the one worth being deliberate about — so the
// command that is about to run is printed first, in full.
func installSkill(args []string) int {
	if _, err := exec.LookPath("npx"); err != nil {
		fmt.Fprintln(os.Stderr, "`npx` was not found. The skills CLI is distributed through npm,")
		fmt.Fprintln(os.Stderr, "so installing skills needs Node. Skills can also just be written:")
		fmt.Fprintln(os.Stderr, "a directory with a SKILL.md in it, in any of the paths `skills` lists.")
		return 1
	}

	if len(args) == 0 {
		found, _ := skills.Load()
		if _, have := skills.Find(found, "find-skills"); have {
			fmt.Println("`find-skills` is already installed, so the agent can look for")
			fmt.Println("the rest itself — ask it for what you want and it will search.")
			fmt.Println()
		} else {
			fmt.Printf("Skills are catalogued at %s and installed with the `skills` CLI.\n\n", skillsRegistry)
			fmt.Println("Start with find-skills, which teaches the agent to search for the others:")
			fmt.Printf("\n  sentineldesk-agent skills install %s --skill find-skills\n\n", findSkills)
		}
		fmt.Printf("Anything else, by repository:\n\n")
		fmt.Printf("  sentineldesk-agent skills install <owner>/<repo> [--skill <name>]\n")
		fmt.Printf("  sentineldesk-agent skills install <owner>/<repo> --global\n\n")
		fmt.Println("Installed project-wide by default into .agents/skills — the neutral")
		fmt.Println("path in the convention. This agent reads that, .claude/skills,\n.opencode/skills and its own .sentineldesk/skills.")
		return 0
	}

	// --agent universal, which the CLI maps to .agents/skills — the neutral
	// path in the convention, and one this runtime reads.
	//
	// Not claude-code, though that also lands somewhere visible here. Claiming
	// to be another agent to get a file into a directory is the kind of small
	// lie that is true until it is not: the day their path map changes, skills
	// installed "as Claude Code" go where Claude Code now keeps them and this
	// runtime is looking somewhere else, for a reason nobody wrote down.
	//
	// `sentineldesk` is not in their map — there are seventy-odd agents in it
	// and we are not one — so `universal` is the honest identifier available.
	// Getting added is a change to their repository, not ours.
	//
	// A caller who passes their own --agent overrides this and is on their own
	// about whether the result is visible here.
	full := append([]string{"-y", skillsCLI, "add"}, args...)
	if !slices.Contains(args, "--agent") && !slices.Contains(args, "-a") {
		full = append(full, "--agent", "universal")
	}

	fmt.Printf("\033[2m$ npx %s\033[0m\n\n", strings.Join(full, " "))
	fmt.Println("\033[33mA skill is instructions this agent will follow with whatever permissions")
	fmt.Println("it holds. Read one before trusting it.\033[0m")
	fmt.Println()

	global := slices.Contains(args, "-g") || slices.Contains(args, "--global")
	staging, ours := installPaths(global)
	before := listDirs(staging)

	cmd := exec.Command("npx", full...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\nthe install did not finish: %v\n", err)
		return 1
	}

	moved := adopt(staging, ours, before)
	if len(moved) > 0 {
		fmt.Printf("\n\033[2mmoved into %s: %s\033[0m\n", ours, strings.Join(moved, ", "))
	}
	fmt.Println("\n\033[2mRun `sentineldesk-agent skills` to see what this runtime now reads.\033[0m")
	return 0
}

// writeNotice records that a skill came from somewhere else.
//
// The installer copies a SKILL.md and nothing around it — not the LICENSE, not
// the repository it came from. Once that file is sitting in this project's own
// directory beside skills somebody here wrote, there is no way left to tell them
// apart, and the difference matters: one of them is ours to change and the other
// arrives under somebody else's terms.
//
// So the provenance goes in beside it, written by the thing that moved the file
// rather than by whoever remembers. skills-lock.json already carries the source
// and a hash; this puts the same facts where a person will actually see them,
// which is next to the file.
func writeNotice(dir, name string) {
	source, hash := lockEntry(name)
	if source == "" {
		return
	}
	body := fmt.Sprintf(`# Third-party skill

%s was installed from %s and is NOT part of this project. It is used
under the terms its authors published with it — see that repository's LICENSE.

    source  %s
    hash    %s

Written by `+"`sentineldesk-agent skills install`"+`. Edit the skill if you like;
this file is what says where it started.
`, name, source, source, hash)
	_ = os.WriteFile(filepath.Join(dir, "NOTICE.md"), []byte(body), 0o644)
}

// lockEntry reads what the installer recorded about a skill.
func lockEntry(name string) (source, hash string) {
	raw, err := os.ReadFile("skills-lock.json")
	if err != nil {
		return "", ""
	}
	var doc struct {
		Skills map[string]struct {
			Source       string `json:"source"`
			ComputedHash string `json:"computedHash"`
		} `json:"skills"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return "", ""
	}
	e, ok := doc.Skills[name]
	if !ok {
		return "", ""
	}
	return e.Source, e.ComputedHash
}

// installPaths is where the CLI drops a skill and where it belongs afterwards.
func installPaths(global bool) (staging, ours string) {
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", ""
		}
		return filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".sentineldesk", "skills")
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", ""
	}
	return filepath.Join(wd, ".agents", "skills"),
		filepath.Join(wd, ".sentineldesk", "skills")
}

func listDirs(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out
}

// adopt moves what the install just produced into this runtime's own directory.
//
// The CLI writes as the `universal` agent, into .agents/skills, because that is
// the honest identifier available to us in its path map. That is fine as a drop
// point and wrong as a home: a skill somebody installed FOR this agent should
// live under this agent's name, not under a shared one it happens to be able to
// write to. Where the file lives is who owns it, and depending on somebody
// else's directory for our own installs makes their layout our problem forever.
//
// THIS IS A BRIDGE, not the end state. The end state is `sentineldesk` being in
// that path map, at which point the CLI writes to .sentineldesk/skills directly
// and this function deletes itself. Until then a move is the honest way to get
// there — and it is a move rather than a copy precisely so there is never a
// moment where the same skill exists in two places and somebody edits the one
// that is not being read.
//
// This does NOT touch anything that was already there, and it does not change
// what gets READ. Every other location stays in the search — a skill installed
// for Claude Code or opencode still works here, which is most of what following
// the convention bought. What moves is only what this command just created.
func adopt(staging, ours string, before map[string]bool) []string {
	if staging == "" || ours == "" {
		return nil
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return nil
	}
	var moved []string
	for _, e := range entries {
		if !e.IsDir() || before[e.Name()] {
			continue
		}
		if err := os.MkdirAll(ours, 0o755); err != nil {
			return moved
		}
		dst := filepath.Join(ours, e.Name())
		if _, err := os.Stat(dst); err == nil {
			// Already ours, and newer is not obviously better than what
			// somebody may have edited. Leave both and say nothing rather than
			// overwrite a file they changed.
			continue
		}
		if os.Rename(filepath.Join(staging, e.Name()), dst) == nil {
			moved = append(moved, e.Name())
			writeNotice(dst, e.Name())
		}
	}
	// Tidy the drop point when this command is what created it. A directory
	// left behind empty is a place somebody will later look for skills that are
	// not there.
	if len(before) == 0 {
		_ = os.Remove(staging)
		_ = os.Remove(filepath.Dir(staging))
	}
	return moved
}

// showRoles lists what can be asked for, with what each one does.
//
// A role is now a way of working rather than a paragraph — it can pick the
// model and narrow what the run may call — so a bare list of names stopped
// being enough to choose from.
func showRoles() int {
	names := prompts.Roles()
	if len(names) == 0 {
		fmt.Println("No roles found, which should not happen: some are built in.")
		return 1
	}
	for _, n := range names {
		def, err := prompts.Role(n)
		if err != nil {
			fmt.Printf("\033[31m✗ %s\033[0m  %v\n", n, err)
			continue
		}
		fmt.Printf("\033[1m%s\033[0m\n", n)
		if def.Description != "" {
			fmt.Printf("  %s\n", def.Description)
		}
		var settings []string
		if def.Model != "" {
			settings = append(settings, "model "+def.Model)
		}
		if def.Policy != "" {
			settings = append(settings, "policy "+def.Policy)
		}
		if len(def.Deny) > 0 {
			settings = append(settings, "denies "+strings.Join(def.Deny, ", "))
		}
		if len(def.Allow) > 0 {
			settings = append(settings, "allows only "+strings.Join(def.Allow, ", "))
		}
		if def.Tools != 0 {
			settings = append(settings, fmt.Sprintf("tools %d", def.Tools))
		}
		if len(settings) > 0 {
			fmt.Printf("  \033[2m%s\033[0m\n", strings.Join(settings, " · "))
		}
		fmt.Println()
	}
	fmt.Printf("%d role(s). They are Markdown with a YAML header, in agent/prompts/roles\n", len(names))
	fmt.Printf("and ~/.sentineldesk/prompts/roles — adding one is adding a file.\n")
	return 0
}

// staleFor is how long the resumed conversation has been sitting. Zero when
// this is not a resume, which the loop reads as "no gap to warn about".
func staleFor(p store.Resumable) time.Duration {
	if p.RunID == 0 || p.EndedAt.IsZero() {
		return 0
	}
	return time.Since(p.EndedAt)
}
