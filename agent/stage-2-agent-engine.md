# Stage 2 — the engine

The second binary: `sentineldesk-agent`, the brain that drives the desktop
without Claude Desktop or any other host in the middle.

This document covers the **engine** — everything that has to work before there
is an interface to put on it. It is deliberately headless. The console lives in
stage 2.2 and is out of scope here for a reason given in §6.

It follows [`stage-1-mcp-readiness.md`](stage-1-mcp-readiness.md), which closed
the server-side work this depends on. Read §1 of that document for the scope
boundary and §7 for the four decisions already made; neither is repeated here.

---

## 1. The transport question, settled

The engine talks to the MCP server over a **Unix socket**. Line-delimited
JSON-RPC 2.0, `/run/user/1000/sentineldesk-mcp.sock`, mode `0600`. There is no
WebRTC anywhere in the engine, no DataChannel, no HTTP, no WebSocket.

This needs stating because an earlier planning draft
(`sentineldesk-agent-prompt-datachannel.md`, superseded — see §8 of the stage 1
document, and not in git) reads as though the agent communicates over a
DataChannel. It conflated two different edges of the system, because it assumed
the runtime lived *inside* `sentineldesk` rather than beside it:

```
browser  ──WebRTC DataChannel "agent"──▶  sentineldesk-agent     ← stage 2.2
sentineldesk-agent  ──JSON-RPC / unix socket──▶  sentineldesk    ← the engine
```

They are not alternatives. The DataChannel is how a *person* reaches the
runtime; the socket is how the *runtime* reaches the desktop. Stage 1 §7.5
already decided the first and called it sound — it is simply not a question the
engine has to answer.

The consequence worth keeping: **the engine is testable from a shell with no
browser, no WebRTC session and no room.** That is what makes stage 2.1 possible
as a stage of its own.

### What the engine is not allowed to do

From stage 1 §1, unchanged and load-bearing:

> If the agent can only do what Claude Code can do through the same socket, then
> the socket is the security boundary, and it is one boundary rather than two.

The engine is an ordinary MCP client. It does not import `internal/mcp`'s
dispatch, it holds no privilege the socket does not grant, and any capability it
needs that cannot be expressed as an MCP call is a **server** gap to be fixed in
`sentineldesk` — never worked around here. §2 exists because applying that rule
to stage 1 §12 turned up five such gaps.

---

## 2. Stage 2.0 — the server changes the engine depends on

Five changes in `sentineldesk`, all small and all understood. They come first
because building the runtime against a server that cannot record what the
runtime does means the audit trail and the role modes have nothing to attach to.

These are the deferred `§5.4` metadata, finally triggered. Note that the field
it turned out to need is **not** one of the five that section speculated about,
which is the argument for having waited.

| | Change | Why it cannot live in the agent |
|---|---|---|
| 2.0.1 | `ACTION_LOG` on by default, via `config.Config`, with a row in the README table | The log is written by the server, at the one point policy is applied. Today the variable is read straight from the environment, is set nowhere, and the trail dies on restart. |
| 2.0.2 | `Visibility` on `toolDef`: `hidden` / `visible` / `injects` | Only the tool's author knows whether it puts pixels on a screen. `RequiresControl` is not a proxy for it — see stage 1 §12.1. |
| 2.0.3 | Accept a `task_id` in `_meta` and store it on the action entry | Grouping calls into a task requires the server to persist the id the runtime supplies. |
| 2.0.4 | Store the result, or a bounded summary, beside the arguments | "How did you install it" is answerable today; "what did apt print" is not. |
| 2.0.5 | An `ask_human` tool | Reaching a person's browser requires the room, which the engine has no access to by design. `request_control` already does exactly this — it blocks and prompts — so this generalises an existing path rather than opening a new one. |

**2.0.2 in detail.** The catalogue currently answers *what can this do*
(`Risk`) and *does it need the desktop* (`RequiresControl`). It cannot answer
*will a person see this happen*, and the distinction is not academic:
`run_command` and `terminal_run` do the same job, one invisible and ungated, one
visible and gated. A model choosing between them takes the first, because it is
simpler and returns output more cleanly. Without this field the runtime cannot
implement §3.5 at all.

Validated at startup like `Risk`, so a new tool without one fails the build
rather than defaulting into whichever answer the surrounding code implies.

---

## 3. Stage 2.1 — the engine

`agent/cmd/sentineldesk-agent/`, one Go module (ADR-001), **no CGO** (ADR-002).

### 3.1 The MCP client

Spawns `sentineldesk -mcp-stdio` or connects to the socket directly, speaks
JSON-RPC 2.0, and handles the four things stage 1 built that a naive client
ignores:

- `notifications/progress` — a long tool reporting while it runs.
- `notifications/sentineldesk/event` — the subscription from stage 1 §11.2.
  The engine subscribes to `control` on startup and **must** treat
  `change: "taken_from_you"` as an interrupt, not a log line. A person taking
  the desktop mid-task invalidates the plan built on holding it.
- `notifications/cancelled` — outbound, when the user stops a task.
- `_meta["sentineldesk/denial"]` — the denial *kind*. `policy` means give up;
  `room` means ask a person and retry. Collapsing them into "it failed" turns
  "wait your turn" into "abandon the task", which is the specific failure the
  kinds were added to prevent.

`tools/list` is read on connect and re-read on reconnect. The server declares no
`tools/list_changed` capability and the catalogue is static per process, so
there is nothing to subscribe to — stage 1 §11.2 explains why inventing it would
be declaring something that does not happen.

### 3.2 Providers

An interface, not a fork per vendor. HTTP only, which is part of why the binary
does not need CGO. A missing key makes that provider unavailable; it does not
stop the engine from starting (ADR-004's spirit, applied one level down).

### 3.3 The loop

Goal → plan → act → observe → decide.

**The cost model is round-trips first, tokens second, milliseconds third.** That
ordering is a correction: stage 1 §11.5 measured perception in milliseconds and
tokens, and both are real, but a 30 ms tool that forces the model to call again
to learn what happened costs more than a 770 ms tool that answers completely.
See [`practices-from-openclaw.md`](practices-from-openclaw.md) §2. It is also
why `open_app_and_wait` beats `launch_app` followed by `wait_for_window`.

**An interrupted turn must be told it was interrupted.** After any abort the
next turn carries a notice that tools may have partially executed — a cancelled
`run_command` may have installed half a package, and the model has to know that
before planning the next step. A *deliberate* stop is marked so it does not
carry one: telling the model things may be half-done after a clean handoff is
its own kind of lie. Same shape as the denial kinds. See §6 of the practices
document.

Within that, the observation step is budgeted according to stage 1 §11.5. That section is the one to read before writing this
part: the measured finding is that **latency and context cost are almost
uncorrelated** — `ui_find` and a full `ui_tree` both cost ~770 ms and differ by
2,500× in tokens — so a loop that optimises for either number alone will get the
other badly wrong.

Concretely, the perception defaults:

- Orient with `list_windows` / `get_active_window` — 30 ms, ~100 tokens.
- Ask narrow questions with `ui_find`, not `ui_tree`. A full tree is ~20,400
  tokens for one glance; it is a debugging tool, not a perception primitive.
- Prefer a screenshot where the accessibility tree is thin — Chromium, a canvas,
  a video. Full-screen vision is ~2,800 tokens in 68 ms against the tree's
  20,400 in 774. Accessibility-first is right because the tree is *exact and
  small when it is good*, not because it is cheap.

### 3.4 Tool selection

The runtime answers its own tool search, locally, from the catalogue it holds
after `tools/list` (ADR-003). It does not forward the server's `tool_search` to
its model — two tools with the same name doing the same thing is a choice the
model should not have to make.

The ranking is measured: stage 1 §11.1 took it from 76% to **100% in the top
ten, 93% top three, 82% top one**, with a corpus of one plain-English query per
tool. The engine can rely on the right tool being in the visible window. It
should not rely on rank one.

When the ranking implementation moves out of `internal/mcp` into a shared
package, it moves *because* there are two consumers — not before.

### 3.5 Roles and observability

From stage 1 §12.2. This is runtime policy, not a judgement the model makes per
call:

| Role | Behaviour |
|---|---|
| `efficient` | Nobody watching, no evidence asked for. The invisible path is correct; making a person's desktop flicker for a package install is theatre. |
| `witnessed` | Somebody asked for a recording, screenshots or a demonstration. The invisible path is **closed**: where a visible equivalent exists the runtime substitutes it — `run_command` becomes `terminal_run`. |
| `ask` | A person granted control while working. Ask whether they want to watch or have it run in the background, via `ask_human` (2.0.5). |

The substitution under `witnessed` is enforced by the runtime, not requested of
the model. **Evidence cannot depend on the model remembering to be observable.**
This is what 2.0.2 exists for.

### 3.6 Permissions

A policy overlay with `allow` / `ask` / `deny`, layered on the server's `Risk`
annotations. It may only ever be *more* restrictive than `MCP_POLICY` — the
server enforces its ceiling regardless, and `sentineldesk/policy` narrows a
connection but never widens it. An overlay that appeared to grant something the
socket refuses would be a lie told to the operator.

Approvals bind to an exact call — tool name and arguments — not to a tool name,
and not to a session.

### 3.7 The room

The engine joins as `agent`, claims control before injecting, and releases when
a task ends. Three consequences it owns, because the server deliberately will
not (stage 1 §1):

- Nothing serialises concurrent sub-agents. Two typing at once interleave
  keystrokes into one X display; deciding what may run in parallel is the
  runtime's job.
- One `release_control` releases for all of them.
- The control event from §3.1 is the only warning it gets that the desktop left
  its hands.

### 3.8 Persistence and the trail

`modernc.org/sqlite` (ADR-002). Conversations, tasks, events, memory.

**Compaction keeps the structured facts as structure.** The transcript is
compressible; the list of what was actually done is not. Which windows were
opened, which packages were installed, whether the controls are held, what was
tried and failed — those cross a compaction boundary as state, never as a
sentence in a summary that the next summary will compress again.

The runtime generates the `task_id` that 2.0.3 threads into the server's action
log, so a person asking "what did you do" gets one trail rather than a scatter
of calls. It also records the **goal** alongside them — the server knows what
was called, only the runtime knows why, and a trail with the why is worth more
than one without.

### 3.9 Plugins: skills, agents and commands

**Decision: the Claude Code plugin format, verbatim.** No dialect of our own.
A plugin written for Claude Code, Codex or Cowork — or installed from a
marketplace — loads unchanged, and anything we add rides in the namespaced slot
the format already provides.

The layout, grounded against two independent implementations rather than
recalled:

```
.claude-plugin/plugin.json        manifest: name, version, description, author,
                                  and optional explicit agents[]/skills[]/commands[]
.claude-plugin/marketplace.json   for a repository that publishes several
agents/*.md
skills/<name>/SKILL.md            + scripts/  references/  assets/
commands/*.md
hooks/hooks.json
```

Frontmatter, with the required keys in bold:

| | keys |
|---|---|
| skill | **`name`**, **`description`**, `homepage`, `license`, `allowed-tools`, `user-invocable`, `metadata` |
| agent | **`name`**, **`description`**, `model`, `tools` |
| command | **`name`**, **`description`**, `allowed-tools` |

One wart to absorb rather than fix: a skill spells its tool allowlist
`allowed-tools` and an agent spells the same thing `tools`. Both are the format;
the loader accepts each where it belongs and normalises internally. Inventing a
third spelling to be consistent would break the interop the whole decision is
for.

**Three forms, one mechanism.** A skill, a command and an agent are each a
markdown file whose frontmatter is what the model sees while choosing and whose
body loads once chosen. What differs is only the trigger and the scope: a skill
fires when the model judges it relevant, a command when a person types its name
(`user-invocable: true` is the same thing said in a skill's frontmatter, which
is the proof they are not two systems), and an agent when a sub-task is
delegated, with a narrower tool set. Build one loader.

Progressive disclosure as the format intends — the same argument as
`MCP_DISCOVERY`, one layer up.

#### The real interop problem: tool names

Community plugins declare allowlists in **Claude Code's** vocabulary:

```yaml
tools: ["Read", "Write", "Edit", "Bash", "Grep", "Glob"]
```

Not one of those exists in our catalogue. This is the actual work, and no
marketplace metadata standard addresses it.

- `Read` / `Write` / `Edit` → `read_file` / `write_file`. Direct.
- `Grep` / `Glob` → nothing of ours. They resolve through `run_command`, or the
  plugin is told the capability is absent. Declaring absence beats silently
  mapping a search tool onto arbitrary command execution.
- `Bash` → `run_command`, **except under the `witnessed` role, where it is
  `terminal_run`.** So the mapping is not a table, it is a function of the role
  — which is exactly what §3.5 and the `Visibility` field exist for. A community
  plugin gets observability for free without knowing the concept exists.

**An unknown name never widens anything.** The allowlist goes through
`Policy.Restrict`, which can only narrow, and the server enforces it on that
connection regardless of what any markdown claimed. That is the difference worth
keeping: elsewhere `tools:` is a list a host honours by convention; here it
becomes a policy the socket applies.

#### Concurrent agents are serialised by Visibility

A plugin advertising eight specialised agents is describing eight system
prompts. Here they are also participants sharing one claim on one X display, and
stage 1 §1 is explicit that the server does not serialise them — the runtime
does.

The field makes it mechanical rather than a judgement: agents whose work is
`hidden` may run concurrently, and anything reaching `injects` takes a turn.
Two sub-agents typing at once interleave keystrokes into one display, and
finding that out empirically is not a good use of a demo.

#### What we accept and do not enforce

Some published plugins carry skill-as-*function* fields — `parameters` with
enums and validation regexes, `retry_config`, `observability` — alongside the
standard ones. The Anthropic format is skill-as-*prompt*: frontmatter to decide
whether to trigger, body to load after. Unknown keys are ignored, so those
plugins load; nothing validates a `validation: "^(ubuntu|debian)$"` and nothing
retries on a `retry_config`. Written down so it is a known limit rather than a
silent one.

The same applies to marketplace-specific metadata such as `sasmp_version` or
`bond_type`: ignoring a declarative field *is* compatibility with it, since
nothing reads it in its own repository either.

#### Our one extension

`metadata.sentineldesk.requires`, listing binaries a skill needs. A skill about
administering Linux is useless if what it names is absent, and `list_commands`
answers that before the model spends a turn discovering it. Optional, always: a
skill without it loads and works.

### 3.10 Stopping

Pause, cancel and emergency stop with honest semantics. A cancel that returns
before the tool actually stopped is worse than no cancel, because it tells the
operator something untrue at the moment they most need the truth. The server
side is real — `notifications/cancelled` interrupts a call in flight, and
`HaltConnection` refuses one connection without touching the others — so the
runtime's job is to not weaken it.

---

## 4. The command-line surface

The engine's interface for stage 2.1. It is the whole product until 2.2, and it
is also how the tests drive it.

```
sentineldesk-agent run "install nginx and show me it working"
sentineldesk-agent chat                       # interactive
sentineldesk-agent tools [query]              # what it can see, and its own ranking
sentineldesk-agent audit <task-id>            # the trail for one task
sentineldesk-agent doctor                     # can it reach the socket, the room, a provider

  --role      efficient | witnessed | ask
  --policy    readonly | safe | full          # narrows, never widens
  --provider  --model
  --sock      --container
```

`audit` is not a convenience. It is the acceptance test for §3.8: if a person
can ask how something was done and get an exact answer, the trail works.

---

## 5. How it is tested

The same way the catalogue was: against a real container, with `_test.go`.

- **Unit** — the loop, the ranking, the policy overlay, the role substitution.
  No socket.
- **Protocol** — against a real `sentineldesk`, covering the four behaviours in
  §3.1. The denial-kind test matters most: a client that treats `room` as
  terminal is broken in a way that only shows up when a human is present.
- **Integration** — real goals against a real desktop, under `-tags integration`
  like `test/integration/`. "Install a package and prove it is serving" is a
  good first one; it is the demo that produced stage 1 §12.
- **Role** — the same goal under `efficient` and `witnessed`, asserting that the
  second one is *visible*: a window appeared, the recording contains it. This is
  the test that catches the substitution silently not happening.

The lesson from stage 1 §10 applies with full force here: **a tool that returns
`ok` has told you it did not throw.** Whether it did the job is a separate
question, answerable only by opening the artifact. That mistake cost eight
silent failures across three passes of a catalogue that was green every time.

---

## 6. What is deliberately not here

**The console (stage 2.2).** A React island in the existing client, over a
second DataChannel named `agent` — decided in stage 1 §7.5 and not in doubt as a
transport.

Two things to settle when it starts rather than now:

- The repository has **no build step** today. `internal/webui/assets/` is
  vanilla ES modules embedded with `go:embed`, and CLAUDE.md documents that as a
  convention. React adds node, npm, bundling and a compiled artifact to version.
  That may still be the right trade for a console; it should be a decision.
- What the console shows is a function of what the engine turns out to produce.
  Designing the surface before the loop exists means guessing.

Building the interface second is not sequencing for its own sake. The loop is
the part that can be wrong in ways that are hard to see, and a UI on top of an
unproven loop hides failures instead of surfacing them.

---

## 7. Order of work

1. **2.0** — the five server changes. Small, well understood, and everything
   below leans on them.
2. **2.1a** — the MCP client and `doctor`. ✅ Done. `agent/internal/mcpclient`
   plus `agent/cmd/sentineldesk-agent`, `CGO_ENABLED=0`, cross-compiling to
   linux/amd64, linux/arm64, darwin/arm64 and windows/amd64 from one machine
   (ADR-002 earning its keep). `make agent`, `make agent-doctor`.

   `doctor` runs 15 checks against a real desktop and all of them pass. Two
   failed on the first run and both were client defects rather than test
   defects: the client never asked for progress — the server sends none unless
   a `progressToken` is present, which is right for a host that did not opt in
   and wrong for a runtime, since a tool running for minutes in silence is
   indistinguishable from one that has hung — and the provenance check read the
   action log as its first call after `SetTask`, asking the log to contain an
   entry the server had not finished writing. `action_log` cannot see itself.
3. **2.1b** — providers and the loop, with `run` against a trivial goal.
4. **2.1c** — tool selection, the policy overlay, the roles, and the plugin
   loader with its tool-name mapping (§3.9). The mapping depends on the roles,
   so it lands with them rather than before.
5. **2.1d** — persistence, `audit`, and the stopping semantics.
6. **2.2** — the console.

Steps 2 through 5 each end with tests that run against the container, so there
is never a point where the engine is believed to work rather than known to.

---

## 8. Later: teaching a workflow by demonstration

Not stage 2. Recorded here because two decisions belong in the *server*, and one
of them is hard to add afterwards.

The goal: a person does a task once — "create an invoice and email it" — and the
system produces a skill the agent can then run, using the whole MCP catalogue.
The same idea as teaching a browser assistant a workflow, on a whole desktop.

### 8.1 Why it fits what is already here

More than incidentally.

- **`Visibility` is the same axis, run backwards.** It exists so the agent can
  act the way a person would when somebody is watching. Teaching is the person
  acting while the system watches. One concept, two directions.
- **The action log is already the right shape.** `task_id`, `goal`, tool,
  arguments, result — that is a demonstration trace, and it exists because §12.3
  wanted an audit. The two turn out to be the same record.
- **The output format is decided.** A taught workflow is a `SKILL.md` (§3.9), so
  it is portable, reviewable, diffable and editable by hand rather than an opaque
  blob. A person can correct what the agent learned.
- **The room already knows who is driving**, which is where a demonstration
  starts and stops.

### 8.2 The asymmetry to close

| | what is recorded |
|---|---|
| the agent acts | MCP → the trail: tool, arguments, result, task, goal |
| a person acts | DataChannel → XTEST → **nothing** but pixels in the video |

We record perfectly what the agent did and nothing of what a person did.
Teaching needs the second.

The tap itself is not the problem: `Session.handleInput` in
`internal/stream/session.go` is already a single chokepoint — one switch, after
the `IsController` gate, where every injection happens. A hook there is local.

### 8.3 The decision that is hard to add later

**A trace of raw input is worthless.** "Click at 847,392" breaks the moment a
window moves, a font changes, or the screen resizes. What a skill needs is
"clicked the button named *New Invoice* in GnuCash" — and that has to be
resolved **at the instant of the click**, because a second later the interface
has moved on. It cannot be recovered afterwards from a video or a coordinate
log.

Our tool for that today is `ui_find`, at ~770 ms, because it walks the whole
AT-SPI tree. That is unusable in a path carrying up to 120 events a second.

The primitive is **`getAccessibleAtPoint`** — AT-SPI's point lookup on the
Component interface, `O(depth)` rather than `O(tree)`. Verified present in the
container and never used by this project. Chromium's `elementFromPoint` is the
same thing for a page, and `cdpClick` already uses it.

**It pays rent now, which is what makes this a decision rather than speculation.**
A point-to-element tool collapses *screenshot → `ui_tree` → guess which element*
into one round trip, and §3.3 is explicit that round trips are the cost that
matters. The agent looks at a screenshot, sees a button, and needs its ref;
today that costs a full tree walk and ~20,400 tokens.

So: add it as a tool because it is useful, and teaching inherits it.

### 8.4 The decision that is irreversible

Recording what a person does on a shared desktop captures keystrokes.
**Keystrokes include passwords.**

This has to be settled before any tap exists, because "we will filter it later"
is exactly how this goes wrong:

- Teaching mode is **explicit, bounded and never on by default**.
- The tap is **absent unless teaching is active**, not present and filtered. A
  filter is one bug away from a keylogger; an absent code path is not.
- Everyone in the room is shown that it is on, the way a restream is. A person
  who did not notice cannot have consented.
- A demonstration is reviewable before it becomes a skill. The artifact is
  markdown for this reason among others.

### 8.5 What not to build yet

The recorder, segmentation into steps, generalising one run into a skill,
turning coordinates into intent. All of it waits until the engine exists and
there is something to teach.

Our own convention applies: extra layers and generality for imagined inputs are
defects, not rigor. Two primitives and one privacy rule are what this section is
for; the rest is a later stage.
