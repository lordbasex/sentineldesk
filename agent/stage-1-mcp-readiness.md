# Stage 1 — making the MCP server ready for SentinelDesk Agent

Status: current as of `3f1ee86` (6 August 2026).

Every claim in this document was checked against the code in this repository at
that commit. Where an earlier document in this directory says otherwise, this one
is right and section 8 lists the differences. Nothing here was carried over on
trust.

---

## 1. Scope: two projects, one contract

There are two deliverables and they are deliberately separate.

**SentinelDesk** — this repository. A Linux desktop in a container, streamed to
browsers over WebRTC, with an MCP server that exposes the same desktop to an
agent. It is finished software that already works. It ships as one CGO binary
because it links GStreamer.

**SentinelDesk Agent** — lives in `agent/`. A second binary,
`sentineldesk-agent`: the brain. LLM providers, an agent loop, planning,
conversation memory, skills, tool selection, a permission overlay and an audit
trail. The point of building it is to stop depending on Claude Desktop or any
other host to drive the desktop, and to be able to put any model behind it.

The dependency runs one way and only one way:

```
sentineldesk-agent  ──MCP JSON-RPC over a 0600 unix socket──▶  sentineldesk
```

The agent is an MCP client. It is not privileged, it does not import the
dispatch layer, and it gets no capability that an external host would not get.
That constraint is the whole design: **if the agent can only do what Claude Code
can do through the same socket, then the socket is the security boundary and it
is one boundary rather than two.** Anything the agent needs that it cannot
express as an MCP call is a hole in the MCP server, and it gets fixed here in
stage 1 rather than worked around there in stage 2.

**Stage 1** — this document — is the work inside SentinelDesk that has to happen
first. **Stage 2** is building the agent.

### The invariant: one desktop, shared

Everything below serves this and nothing may break it.

SentinelDesk is not a desktop with an agent bolted on, and the agent is not a
tenant with its own screen. It is **one operating system that people and an AI
occupy at the same time**, seeing the same pixels, and coordinating over who
drives. MCP is the agentic half of that — the way the AI reaches the same X
display the humans are watching — not a side door into a private machine.

Concretely, and all of it is already true in the code:

- **The agent is a room member like anyone else.** Stable id `agent`, a name in
  the participant list, a pointer marker on screen. What it does is visible
  while it does it, in everyone's stream and in the recording.
- **Control is claimed, never assumed — by either plane.** With the controls
  free the agent still asks, and `request_control` grants immediately, so the
  cost is one call and what it buys is that no handover is invisible. Releasing
  leaves control free rather than passing it to somebody.
- **Holding control is not permission.** While it drives, the agent can do
  exactly what `MCP_POLICY` allows and nothing beyond it. The desktop being
  under its hand does not widen the catalogue.
- **Several agents can work inside the one held session.** Every MCP connection
  shares the identity `agent`, `mayInject` tests the room's controller rather
  than the connection, and each request runs in its own goroutine. So a runtime
  can fan out sub-agents across connections and they all act under one claim,
  as one participant, with one turn at the controls.

**Stage 2 must not weaken any of these to make the runtime simpler.** Three
consequences follow from the last point that the runtime owns, because the
server deliberately will not:

- *Nothing serialises concurrent agents.* Two sub-agents typing at once
  interleave keystrokes into the same X display. Deciding what may run in
  parallel is the runtime's job.
- *One `release_control` releases for all of them.* There is a single claim, so
  a sub-agent finishing its part can pull the desktop out from under the others.
- *The action log cannot yet tell them apart.* That is §5.3, and this is why it
  matters more than the emergency gate it was listed for.

### Why stage 1 exists at all

Because the agent's quality is bounded by the catalogue's. The runtime's
permission overlay has nothing to overlay on if tools do not declare their risk.
Its state machine cannot distinguish *waiting for approval* from *waiting for
room control* if both arrive as an English sentence. Its cancel button lies if
`tools/call` cannot be interrupted. None of those are agent bugs; they are
server gaps that only become visible when a first-party client tries to be
precise about what happened.

---

## 2. Verified baseline

What the MCP server is today.

| | |
|---|---|
| Transport | Unix socket, mode `0600`, path from `MCP_SOCK` |
| Bridge | `sentineldesk -mcp-stdio` — a stdio↔socket pipe, nothing more |
| Declared protocol | `2024-11-05` (`internal/mcp/mcp.go:44`) |
| Methods | `initialize`, `notifications/initialized`, `initialized`, `ping`, `tools/list`, `tools/call`, `sentineldesk/policy` |
| Catalogue | **115 tools** — 47 `read`, 38 `write`, 30 `danger` |
| Concurrency | one goroutine per request; responses correlated by JSON-RPC id |
| Policy ceiling | `MCP_POLICY=full\|safe\|readonly`, plus `MCP_DENY` / `MCP_ALLOW` |
| Per-connection | `sentineldesk/policy` may only narrow, never widen |
| Room gate | `injectsInput()` tools + `start_restream` / `stop_restream` pass `mayInject()` |
| Audit | in-memory ring of 2000 entries, optional JSONL via `ACTION_LOG` |

Two controls apply to every call, in this order, in `handleToolCall`:

1. `Policy.Allowed` — may this connection ever use this tool?
2. `mayInject` — may the agent drive the desktop right now? (only for the tools
   the server classifies as interactive)

The agent is an ordinary room member with the stable id `agent`. It has no
WebRTC tracks. It never takes control implicitly, not even in an empty room.
It joins the room lazily, the first time it calls `room_state`,
`request_control` or `release_control`.

---

## 3. What is already done

Commits `e49062f` and `3f1ee86`, both on `main`.

**Risk is declared on the tool.** Every `toolDef` carries a
`Risk` — `riskRead`, `riskWrite` or `riskDanger` — beside its schema. The
`readOnlyTools` / `dangerousTools` maps are gone; the policy reads a index
derived from the catalogue. A tool defined without a risk stops the daemon from
starting, and `internal/mcp/registry_test.go` catches it at `make test`.

This was not cosmetic. The old maps had drifted badly: **46 of 114 tools
appeared in neither**, which meant refused under `readonly` and allowed under
`safe`, silently, in both directions. `terminal_run` and `terminal_open` type a
command line into a shell and press Return, and they were on the permitted side
of the level whose entire promise is that it does not run code.

**The classification is published.** `tools/list` now carries the specification's
`readOnlyHint` and `destructiveHint` annotations, derived from `Risk`.

**`tool_search` exists** (server-side), and `MCP_DISCOVERY=1` trims `tools/list`
to a core set of twelve. Discovery narrows what is *advertised*, never what is
*permitted* — a hidden tool still runs when named.

**`make test` is a signal again.** Fourteen tests over the catalogue and the
JSON-RPC path, none of which need X or GStreamer.

None of this is enough on its own. What follows is what is still missing.

---

## 4. Blockers

The runtime cannot be built *correctly* without these. Each one, left alone,
forces stage 2 to either hardcode something the server knows or lie to the user.

### 4.1 The runtime cannot tell which tools need Room control — **fixed**

Step 2 of §6 is done. `RequiresControl` is a field on the `toolDef`, the gate
reads an index derived from the catalogue, and `tools/list` publishes
`sentineldesk/requiresControl`. `registry_test.go` holds a frozen copy of the
old switch statement as a parity test, so the set the room arbitrates is
provably unchanged. Kept here as the record of why.

The room gate is not an obstacle to route around — it is the invariant in §1,
and the runtime has to cooperate with it deliberately. That means sequencing
three distinct things for an interactive action: approve the call, acquire the
desktop, then execute. To do that it must know, *before* calling, whether a tool
is one the server will gate. It could not:

- `injectsInput()` in `internal/mcp/mcp.go` is a `switch` statement, not data.
- `tools/list` does not publish it. The annotations added in `3f1ee86` carry
  `readOnlyHint` and `destructiveHint` and nothing about Room.
- Risk does not stand in for it. `ui_click` is `write` and gated; `set_volume`
  is `write` and not. `start_restream` is `danger` and gated; `write_file` is
  `danger` and not.

So the runtime's only options are to hardcode the list — which drifts the moment
a tool is added, exactly the failure we just spent two commits removing — or to
call blind and interpret the failure, which turns a predictable step into an
error path.

**Fix.** Move `injectsInput` onto the `toolDef` as a boolean field, derive the
function from the catalogue the same way the risk maps now are, and publish it
in `tools/list` under a namespaced annotation:

```json
"annotations": {
  "readOnlyHint": false,
  "destructiveHint": false,
  "sentineldesk/requiresControl": true
}
```

The specification allows extra annotation keys; namespacing keeps it from
colliding with a future standard one. This also drops adding a tool from three
edits to two.

**Acceptance.** A test asserting the derived set equals today's `injectsInput`
list exactly, then a second asserting `tools/list` publishes it.

### 4.2 Denial reasons are strings, not kinds — **fixed**

Step 3 of §6 is done. `tools/call` now carries
`_meta["sentineldesk/denial"]` — `policy`, `room`, `unknown_tool` or
`tool_error` — beside the unchanged prose, and the kind is written into the
action log too. `unknown_tool` is decided before policy so that a nonexistent
name reports the same thing at every level. Kept here as the record of why.

Three unrelated things failed as prose, all with `isError: true`:

| What happened | What the client receives |
|---|---|
| Policy refused | `"denied by the server policy: MCP_POLICY=safe: …"` |
| Room refused | `"nobody is driving, but control is not taken automatically — …"` |
| The tool itself failed | whatever the tool returns |

The runtime has to react differently to each: a policy denial is terminal and
should be reported to the model as a capability it lacks; a room refusal means
*ask a human and retry*; a tool failure may be retryable. Distinguishing them by
substring match is fragile, and it breaks the moment one of those sentences is
reworded — which is a thing this repository does routinely, because the
sentences are written for humans.

**Fix.** Add a machine-readable field to the `tools/call` result alongside the
existing content, so nothing already parsing it breaks:

```json
{
  "content": [...],
  "isError": true,
  "_meta": { "sentineldesk/denial": "policy" }
}
```

**Kinds implemented:** `policy`, `room`, `unknown_tool`, `tool_error`. The human
sentence stays exactly as it was — it is what reaches the model, and it is good.

Two kinds from the original sketch were deliberately left out.
`invalid_arguments` would mean touching all 115 tools, since each validates its
own arguments and reports in prose; it is folded into `tool_error` until
something actually needs it split. `emergency` arrives with the gate in §5.3.

**Found while doing this.** `callRoom` reported `handled` for *every* tool name
when `SetRoom` had not been called, and it sits early in the dispatch chain — so
a `Server` built without a room answered "this build has no room attached" to
the whole catalogue. The daemon always calls `SetRoom`, so it never fired
there; it fires immediately anywhere else the server is embedded, which stage 2
will do. Fixed and covered, and it contradicted this project's own rule that an
optional capability degrades rather than taking everything with it.

### 4.3 `tools/call` cannot be cancelled — **fixed**

Steps 4 and 5 of §6 are done.

*Step 4:* `dispatch` and every dispatcher take a `context.Context`,
`notifications/cancelled` reaches the call, and closing the connection cancels
everything that connection had running.

*Step 5:* two things, and the first turned out to matter more than the audit.

**The acknowledgement no longer waits for the tool.** `handleToolCall` blocks on
`dispatch`, so a tool that ignored its context held the response back until it
finished — the client asked to stop and then waited out the full duration to be
told it had, unable to tell *still stopping* from *ignored you*. Cancelling now
answers from the connection's goroutine and the tool's eventual result is
dropped, guarded by one atomic per request. **The wait always stops even when
the work does not**, and that is the guarantee a runtime can build on.

Doing that surfaced a race worth recording: the call was registered inside the
handler goroutine, so a client sending `tools/call` and `notifications/cancelled`
back to back could have the cancellation arrive first, find nothing, and be
dropped silently. Registration moved into the connection's own goroutine, before
the handler is spawned, which makes the order on the wire the order in the map.

**The audit.** `sleepCtx` replaced the bare `time.Sleep` in every polling loop
that can run for seconds. What stops now: the shell-based tools (the process is
killed), `wait`, `terminal_run`, `terminal_read`, `browser_open`,
`browser_wait_for`, `wait_for_window`, `wait_for_idle`, `open_app_and_wait`,
`fill_form`. What carries on: the accessibility bridge behind `ui_*`, OCR, a CDP
request already sent, the persistent shells and SSH sessions, and the `xdotool` /
`wmctrl` invocations — those finish in milliseconds, so there is nothing to
interrupt. Both lists are in `docs/mcp.md` and neither is a claim of a clean
stop.

The original problem, kept as the record:

`dispatch()` took no `context.Context`. Tools that needed a deadline built their
own from `context.Background()` — `internal/mcp/tools.go:514`,
`tools_admin.go:67`, `tools_admin.go:420`. Each request ran in its own
goroutine and nothing held a cancel function.

Consequences the runtime cannot hide:

- Closing the connection does not stop work already dispatched.
- `agent.run.cancel` cannot honestly claim the current tool stopped.
- Emergency Stop cannot interrupt an `install_packages` that is four minutes
  into a 240-second window.

**Fix, in two steps.** They are separable and the first is worth doing alone.

*Step one* — thread a `context.Context` from `handleToolCall` through
`dispatch()` to the dispatchers, and honour `notifications/cancelled` from the
2024-11-05 protocol by cancelling that request's context. Tools that already
build a timeout take the parent instead of `Background`. This makes the
long-running, `exec`-based tools genuinely interruptible, which is most of the
ones anybody would want to cancel.

*Step two* — audit the remaining tools for ones that ignore their context, and
either fix them or mark them `cancellable: false` in the catalogue so the
runtime can say "this one will finish" instead of "cancelling…" forever.

**Do not skip step two and claim cancellation works.** Partial cancellation
reported as total is the exact dishonesty the whole design is trying to avoid.

**Acceptance.** A test that cancels a `run_command "sleep 30"` and observes the
process die; a documented list of tools that cannot be cancelled.

### 4.4 `Delivery.Deliver` panics when the agent is in the room — **fixed**

Step 1 of §6 is done. `internal/stream/delivery_test.go` covers the four room
states; against the previous code the agent-controlling case aborts the run with
`SIGSEGV` at `delivery.go:86`. Kept here as the record of what it was.

The agent member is created with a nil `session`
(`internal/stream/room.go:241`), and the delivery loop called a method that
dereferences it:

```go
for _, m := range targets {
    if controller != "" && m.id != controller { continue }
    m.session.sendOnChannel(string(msg))   // agent member: session == nil
}
```

`sendOnChannel` locks `s.chanMu` on a nil receiver, so this panics and takes the
whole daemon down — desktop, WebRTC, everybody's session.

The earlier audit says this happens "if the agent controls". **It is worse than
that.** When control is free, `controller == ""`, the `continue` never fires and
*every* member is sent to, including the agent. So:

> agent calls `room_state` (which the planned `room-coordination` skill requires
> at the start of every interactive block) → agent is now a room member → any
> `destination:download` while control is free kills the process.

That is on the happy path of the planned design, not an edge case.

There is a second defect in the same function: one single-use ticket is minted
outside the loop and the same URL is sent to every recipient, so with two
browsers watching the first consumes it and the second gets a dead link.

**Fix applied.** Members without a session are skipped; one ticket is minted per
recipient; nothing is minted when there are no recipients. A human at the
controls receives it alone, because they are the one who asked; with the agent
holding them or nobody holding them it goes to every browser present, since
there is no browser behind the agent to send it to.

Every other delivery loop in `room.go` already guarded `session == nil` — this
one was the only place that did not, so there are no further latent panics of
this shape.

---

## 5. Gaps

The runtime can be built without these. It will be worse, and each of them
becomes harder to add once there is a client depending on current behaviour.

### 5.1 No progress notifications — **fixed**

Step 7 of §6 is done. A client that puts a `progressToken` in the call's `_meta`
gets `notifications/progress` while the tool runs, carrying the command's own
last line of output — the only honest progress a shell command has. Nothing is
sent to a client that did not ask.

The reporter rides on the `context` rather than on the signatures, so it reached
every tool without touching one of the 115. Only the shell-based tools report,
which is the same set that can be cancelled and for the same reason: they are
the ones that run long enough for it to matter.

Two things worth carrying into stage 2. The `message` field is an extension —
the declared protocol version defines `progressToken`, `progress` and `total`
only. And `progress` counts reports rather than seconds, because the
specification wants it to increase and elapsed time stalls on a command that
goes quiet.

### 5.2 No `tools/list_changed`

The server neither declares the capability nor emits the notification, so a
client cannot know the catalogue moved. It matters more than it looks: the
visible set changes when a connection restricts itself, and the runtime pins a
catalogue hash into every approval record. Without a signal it must refresh on
reconnect and poll.

The catalogue is static per process today, so the honest MVP answer is: keep
refreshing on reconnect, and do not invent the capability. Revisit when
something can actually change the catalogue at runtime.

### 5.3 Connections have no identity — **fixed**

Step 6 of §6 is done. Every connection is numbered, `initialize` hands the
number back in `_meta["sentineldesk/connectionId"]` and records `clientInfo`,
and both reach every `action_log` entry as `conn` and `client`.

`Server.HaltConnection(id, reason)` / `ResumeConnection(id)` refuse `tools/call`
from one connection with the `emergency` kind, ahead of the catalogue check —
a client that is supposed to be doing nothing should not be able to map what
exists. Other connections and the desktop are untouched. It is not a kill: calls
already running end under their own cancellation.

The exported halt is the one piece here built slightly ahead of its consumer,
and deliberately: it is the instrument stage 2's Emergency Stop needs, and
adding an identity to a protocol after several clients depend on it is far
worse than adding it now.

The problem, kept as the record:

**Emergency Stop** is supposed to block the Agent Runtime's calls at the MCP
boundary while leaving external MCP clients alone. There is nothing to
distinguish one connection from another — `serve()` holds a policy and nothing
else.

**Attribution across concurrent agents.** Every MCP connection shares the room
identity `agent`, which is what lets a runtime fan sub-agents out under one
claim (§1). The cost is that the action log records "the agent did X" with no
way to say which one, so an audit of a run with four sub-agents is four
interleaved streams with no thread to pull. That gets worse the more the runtime
parallelises, and it cannot be reconstructed after the fact.

**Fix.** Let `initialize` record its `clientInfo`, give each connection an id,
carry that id into the action log entry, and let the daemon refuse `tools/call`
from a nominated connection. This is small, and doing it before there are
several concurrent clients is much easier than after.

Note what this does **not** change: the connection id is for auditing and the
emergency gate. It must not become a second room identity, or the property that
several agents act as one participant is gone.

### 5.4 Catalogue metadata stops at risk and category

`3f1ee86` established the pattern — declare it on the tool, derive everything
else — but only carries `Risk` and a derived category. The runtime's overlay
wants more: `idempotent` (may a failed call be retried?),
`handles_credentials` (redact the arguments in the audit log),
`external_side_effect` (does this leave the machine?), `default_timeout`,
`result_compaction`.

Add them **when the overlay needs them**, on the same principle: a field on the
`toolDef`, derived indexes, a startup check. Do not build a speculative
`internal/toolmeta` with twelve unused fields first.

### 5.5 The declared protocol version predates the annotations we send

`mcpProtocolVersion` is `2024-11-05`. Tool annotations arrived in a later
revision of the specification. Extra JSON fields are ignored by clients in
practice, so nothing is broken today, but the server is describing itself as
older than it is.

Decide deliberately: bump the declared version after checking what else that
revision obliges, or keep 2024-11-05 and document annotations as an extension.
Either is fine; drifting is not.

---

## 6. Stage 1 work plan

Ordered by dependency, not by size. Each step lands on `main` with tests.

| # | Work | Why now | Size |
|---|---|---|---|
| ~~1~~ | ~~Fix `Delivery.Deliver` — nil session, ticket per recipient~~ | **Done.** Six regressions in `internal/stream/delivery_test.go` | small |
| ~~2~~ | ~~`injectsInput` becomes a field, derived + published~~ | **Done.** Parity test freezes the pre-refactor set | small |
| ~~3~~ | ~~Structured denial kinds in `tools/call`~~ | **Done.** `_meta["sentineldesk/denial"]`, plus the same kind in the action log | small |
| ~~4~~ | ~~Thread `context.Context` through `dispatch`; honour `notifications/cancelled`~~ | **Done.** Per-call context, `cancelled` kind, connection close cancels | medium |
| ~~5~~ | ~~Audit which tools ignore cancellation; publish the list~~ | **Done.** Cancellation is acknowledged immediately whatever the tool does; both lists published | medium |
| ~~6~~ | ~~Connection identity from `initialize`, carried into the action log~~ | **Done.** Numbered connections, `conn`/`client` in the log, per-connection halt | small |
| ~~7~~ | ~~`notifications/progress` for the long tools~~ | **Done.** Opt-in by token; the message carries the command's own output | medium |

**Stage 1 is complete.** Everything §4 called a blocker and both gaps in §5 that
had a consumer are closed. What remains open is deliberate: §5.2
(`tools/list_changed`) waits until something can change the catalogue at
runtime, §5.4 (more catalogue metadata) waits until the overlay needs a field,
and §5.5 (the declared protocol version) is a decision rather than work.

Stage 2 can start. The decisions in §7 should be written up as ADRs first —
they are cheap now and expensive once `sentineldesk-agent` exists.

Steps 1–3 are a day. They are also the ones that unblock the most of stage 2.

**Not in stage 1.** Do not build the agent's tool search, policy overlay, skills
or persistence in this repository. They belong to the runtime, and putting them
here would make the desktop depend on the brain — which is the wrong direction
and would end the property that the desktop survives the runtime crashing.

---

## 7. Decisions made before stage 2

These are architectural and cannot be derived from the code. All four are
decided and written up under [`docs/adr/`](../docs/adr/):

| | Decision |
|---|---|
| [ADR-001](../docs/adr/0001-one-go-module.md) | One Go module, `agent/` as a subtree |
| [ADR-002](../docs/adr/0002-agent-without-cgo.md) | The agent does not link CGO; pure-Go SQLite |
| [ADR-003](../docs/adr/0003-tool-search-on-both-sides.md) | Tool search on both sides; the runtime answers its own |
| [ADR-004](../docs/adr/0004-runtime-lifecycle.md) | The runtime is supervised separately; the desktop outlives it |

The summaries below are what each ADR was decided from.

### 7.1 One Go module or two? — [ADR-001](../docs/adr/0001-one-go-module.md)

`agent/` inside this module can import `github.com/lordbasex/sentineldesk/internal/...`
— Go's `internal` rule allows it anywhere under the module root. A separate
module cannot, and would need the shared contract published as a non-internal
package.

**Recommendation: one module, `agent/` as a subtree, `agent/cmd/sentineldesk-agent/`.**
It keeps the option of splitting later at the cost of one directory move,
whereas starting split and merging later means undoing a published API. "Two
projects" is a statement about lifecycle and deployment, and one module does not
prevent it.

### 7.2 The agent binary must not link CGO — [ADR-002](../docs/adr/0002-agent-without-cgo.md)

`sentineldesk` links GStreamer, so it cannot cross-compile and its release
builds go through Docker per architecture. `sentineldesk-agent` has no such
need: LLM calls are HTTP, MCP is a unix socket, and the only temptation is
SQLite.

**Use `modernc.org/sqlite`, not `mattn/go-sqlite3`.** The pure-Go driver keeps
`CGO_ENABLED=0`, which means the agent cross-compiles to every platform from any
machine and its release process is a `for` loop instead of a QEMU matrix. That
is a large, permanent difference in how much the second project costs to ship,
and it is decided by one import line at the start.

### 7.3 Where does tool search live?

**Both sides.** Neither is removed — see
[ADR-003](../docs/adr/0003-tool-search-on-both-sides.md).

The server keeps `tool_search` and `MCP_DISCOVERY`, because an external host —
Claude Code, Claude Desktop — has no runtime of ours and whatever help it gets
has to come over the protocol. That is half the value and it stays.

The runtime answers its own, locally, from the catalogue it already holds after
`tools/list`; calling the server's tool would be a round trip to ask itself a
question. It does not forward the server's `tool_search` to its model, because
two tools of the same name doing the same thing is a choice the model should not
have to make.

An earlier draft of this section read as "the runtime should not use tool
search", which was badly put and is corrected here: the narrow point was only
ever about which side *computes* the answer.

The ranking is one implementation shared by both, moved out of `internal/mcp`
when the runtime exists to import it — not before, since a shared package with
one consumer is a guess about the second.

### 7.4 Does the runtime run under supervisord, and does it degrade? — [ADR-004](../docs/adr/0004-runtime-lifecycle.md)

The desktop must survive the agent. Whatever the answer, the invariant is:
**restarting `sentineldesk-agent` does not disturb WebRTC, the Room, or anyone's
session.** A missing provider key makes the Agent Console unavailable; it does
not stop the desktop from booting.

### 7.5 Transport between the browser and the runtime

The existing prompt argues for a second DataChannel named `agent` on the
existing PeerConnection, with no new HTTP surface. That reasoning is sound and
consistent with how this project already works, and it is the one part of those
documents that needs no correction. It is listed here because it is a decision,
not because it is in doubt.

---

## 8. Corrections to the superseded planning drafts

Two earlier documents — a code audit and a master prompt for the Agent Console —
were written against an older snapshot of this repository. They are kept out of
git and will be deleted; this section is why nothing is lost when they go.

It is recorded rather than dropped because copies of those drafts exist outside
the repository, and fed to an agent as ground truth today they will produce
wrong work. The differences:

| Claim | Reality at `3f1ee86` |
|---|---|
| "114 tools" (both, ~8 places) | **115** |
| "El repositorio no contiene archivos `*_test.go`" / "Ausencia de tests" | `internal/mcp/registry_test.go` — 14 tests, run by `make test` |
| Wanted metadata `read_only`, `dangerous` in a new `internal/toolmeta` | Exists as `Risk` on the `toolDef` in `internal/mcp/registry.go` |
| Per-file counts (tools.go 16, desktop 39, …) | `registry.go` adds one |
| "Adding a tool touches four places" | Three, and step 2 of §6 makes it two |

And one that matters more than a count:

> **Acceptance criterion 50 — "refactorizar metadata conserva las matrices
> full/safe/readonly actuales" — can no longer be satisfied, and must not be.**

The old matrices were wrong. Preserving them would restore the bug: 46 tools
were unclassified, `terminal_run` was allowed under `safe`, `shell_read` and
`room_state` were refused under `readonly`. Rewrite the criterion as *parity is
measured against the declared `Risk` in `registry.go`, not against behaviour
before `e49062f`* — otherwise an agent implementing that prompt will write a
parity test against the old behaviour, watch it fail, and "fix" the code back.

The two documents also assume a single distributed binary with an
`-agent-runtime` mode. That is now superseded: `sentineldesk-agent` is a second
binary. Section 7.1 and 7.2 cover what that changes.

---

## 9. Verifying it against a real desktop

`make test` checks the catalogue against itself and the JSON-RPC path over an
in-memory pipe. That is what makes it cheap and runnable anywhere, and it is
also exactly what it cannot tell you: whether any of it holds against a running
container.

`tools/stage1-check.py` closes that gap. It speaks the protocol the way an AI
host does — through `sentineldesk -mcp-stdio` into a live container — and checks
the catalogue and its annotations, connection identity reaching the action log,
`tool_search`, every denial kind, the room gate refusing then granting then
closing again, cancellation answered at once, and progress arriving only for a
call that asked for it.

```bash
make up
python3 tools/stage1-check.py -v
```

**It earned its place on the first run.** Forty-one of forty-two checks passed;
the one that failed said a connection could **widen** its own policy — the exact
opposite of what `sentineldesk/policy` promises, and the property that makes it
safe to hand an agent a restricted endpoint.

`Restrict` was correct. `serve` called it on the *daemon's* policy every time,
so each request started afresh at the ceiling: a connection that had dropped
itself to `readonly` could ask for `full` and be given it. The unit test for
`Restrict` passed throughout, because the method was never the problem — the
caller was. Fixed by chaining from the connection's current policy, with two
tests that fail against the old behaviour.

The lesson is worth carrying into stage 2: a unit test on a security primitive
proves the primitive, not the system that uses it. The runtime will restrict its
own MCP connection at handshake, and that restriction was decorative for as long
as this bug existed.

## 10. The second full pass, and what measuring found that reading did not

Stage 1 closed once, on a green sweep. Reopening it to answer one question
about `start_recording` turned up three defects in a tool that had already been
called, already passed, and already been reviewed — which is worth recording,
because the difference was not more care. It was looking at the artifact the
tool produces instead of the result it returns.

`start_recording` returns a path. The sweep asserted the path existed and the
file grew, both true, both true the entire time the tool was broken. Running
`ffprobe` over that file said `profile=High 4:4:4 Predictive`: every recording
this project has ever made was encoded at full chroma resolution in a profile
almost no hardware decoder accepts, so the files play back in software or not at
all on the devices most likely to open them. Nothing selected that. `ximagesrc`
emits BGRx, `x264enc` takes Y444 as readily as I420, and `videoconvert` — asked
for no format in particular — picked the conversion cheapest for itself, which
is the one that discards nothing.

The second defect had the same shape. Both encoders document `threads=0` as
"automatic", and what they compute is sized for finishing each frame as quickly
as possible — a property a file on disk has no use for, bought with real CPU
spent on coordination. Neither defect is visible in the tool's output. Both are
visible in one `ffprobe` and one `/proc` read.

Measured against a scrolling terminal, on a 20-core host, with a viewer
attached:

| | CPU (one core) | frames | profile |
|---|---|---|---|
| as it shipped | 281% | 449/450 | High 4:4:4 Predictive |
| threads pinned to 2 | 148% | 447/450 | High 4:4:4 Predictive |
| + chroma pinned to I420 | 98% | 450/450 | High |

The third defect was a data race, found by reading rather than measuring:
`Start` reaped the child in a goroutine while `Stop` polled `cmd.ProcessState`,
which `Wait` writes. Losing that race means `Stop` misses an exit that already
happened and kills gst partway through writing the container index — producing
exactly the unplayable file the SIGINT handshake exists to prevent. It now waits
on a channel the reaper closes.

### On the measurements themselves

Four of the test designs used to reach those numbers were wrong, and each would
have produced a confident figure:

- `videoconvert` in front of a `fakesink` measured 1% — identical to capture
  alone — because `fakesink` accepts any caps, so the element negotiated BGRx to
  BGRx and did nothing. Reported as "conversion is free" it would have been a
  finding.
- A worst-case run piped fullscreen noise to `ximagesink`, which is not in the
  image. `wmctrl` showed no such window ever existed; that run measured the same
  idle desktop twice and its numbers were void.
- Comparing encoder settings in blocks — all of A, then all of B — charged
  whatever the live user happened to be doing to whichever variant ran at the
  time. The same settings measured 134% and 162% in consecutive runs, a spread
  wider than every difference under test. Interleaving fixed it.
- `constrained-baseline` was credited with a 42-point saving attributed to
  CABAC. Baseline *mandates* 4:2:0, so that row changed chroma and entropy
  coding together; separated, chroma accounts for 32 of the 42 and CABAC never
  had to be given up.

An earlier round in this same investigation concluded that `use-damage=1` saved
8% in the recorder, from a single pair of runs. Three repetitions showed the
within-group spread exceeded the between-group difference entirely: it was
noise, and the capture it was meant to optimise turned out to cost 1% of a core
against the encoder's 134%.

### Where the sweep now stands

**112 ran · 2 degraded · 1 skipped · 0 failed**, across 115 tools in 122 calls,
against `v1.2.0`. The two degraded are `ui_set_text` and `fill_form`, both
writing through AT-SPI `EditableText`, which Chromium advertises on its entries
and does not implement — inside a page, `browser_type` is the working path. The
skip is `snapshot_restore`, which would overwrite the live home directory: the
one tool the sweep cannot make safe against something it created itself.

The lesson for stage 2 is narrow and practical. A tool that reports success has
told the runtime one thing: that it did not throw. Whether it did the job is a
separate question, and for anything producing an artifact — a file, a rendered
page, an encoded stream — it is answerable only by opening the artifact. The
runtime should treat "the tool returned ok" as the weakest evidence it has.

## 11. The third pass: closing the runway to stage 2

Five items stood between the catalogue and writing `sentineldesk-agent`. Four
were work; the fifth was a decision. What follows is what each turned out to be
once it was measured rather than described, because in three of the five the
description was wrong.

### 11.1 Tool discovery was worse than reported, and unmeasured

The figure carried into this pass was 74% recall, and it was an estimate. The
first honest measurement — one plain-English query per tool, phrased as the goal
rather than as the tool — put it at **76% in the top ten, 55% in the top three,
33% at rank one**.

Twenty-eight tools were returned by *no* phrasing at all. That is not a ranking
problem. An agent that does not already know a name cannot call it, so those
were unreachable tools, and the fact that they had all passed the sweep only
says the sweep called them by name.

Three causes, none of them a matter of tuning:

- A tool entered the results only on a name or category match; a description
  match was discarded rather than ranked low. Defensible as a ranking rule,
  wrong as a filter, and it is why "who else is connected" returned nothing at
  all for `room_state`.
- The category alias prefix test ran one way, so a query saying "application"
  could not reach the alias "app".
- Two-letter terms matched as substrings. "which workspace **am** I on" hit
  every gamepad tool through the "am" in "gamepad", and four tools with nothing
  to do with the question outranked the one that answered it.

Beyond those, categories cannot choose between tools inside a family and do
nothing at all for a tool whose own name is the obstacle. `launch_app` is the
canonical case: "open the calculator application" returned `browser_open`,
`terminal_open`, `shell_open` and `open_app_and_wait`, because those four hold
the query's one distinctive word and `launch_app` holds it nowhere.

Now **100% top ten, 93% top three, 82% top one**. Top ten is enforced as an
invariant rather than a score — a tool no phrasing reaches should fail the
build — and the other two are floors with headroom. The eight queries that land
at rank four to seven are all *found*; pushing them higher would mean tuning the
searcher against a corpus written in the same file, which measures only how well
it was tuned.

**For stage 2:** the runtime can rely on `tool_search` returning the right tool
in the visible window. It should not rely on rank one. Read the ten.

### 11.2 The agent can now be told things

Level 1 was already done — no wait polls. But that lives *inside* the tools, and
between two calls the agent was still blind. It could not be notified.

Control is the case that forced it. An agent holding the controls can have them
taken by a person at any moment; that is what a shared desktop is for. Until
now it found out by having its next injection refused — a denial where there
should have been a notice, arriving in the middle of a plan built on the
opposite assumption. There was no way to ask, either: `room_state` answers about
*now*, and nothing reported a transition.

`subscribe_events` opens the channel: `control`, `room`, `windows`, `focus`,
`desktop`, delivered as `notifications/sentineldesk/event`. The `control` topic
names the transition, because `taken_from_you` is a different situation from
`released` even though both end with somebody else driving, and an agent that
reads only that one field behaves correctly.

Subscription is a tool call rather than a capability, deliberately. The protocol
has no negotiation for a server pushing something the client did not name, and
a private one would have to be taught to every host. A tool is the mechanism
already here — discoverable, filtered by policy, and silent until called, which
is the correct default since a general-purpose host would treat an unsolicited
notification as noise.

Topics with no source on this desktop say so rather than reporting a
subscription that will never deliver, and an unknown topic is refused: silently
accepting one leaves the agent waiting on something that was never coming, which
is the failure the feature exists to remove.

**This also settles §5.2.** `tools/list_changed` stays unimplemented and
undeclared — the catalogue is still static per process, and the honest answer is
still to refresh on reconnect. The event channel does not change that, and
adding the capability now would be declaring something that does not happen.

### 11.3 Two tools that had never worked

`fill_form` has been invoking `settext --name` since it was written, and
`settext` only ever accepted `--ref`. argparse refused the call, exited 2 with an
empty stdout, and the Go side turned the empty output into a failed field. Every
field, every time. The submit button had the same problem. **The tool had never
filled anything**, and the sweep recorded it as degraded for an unrelated
reason — Chromium's unimplemented `EditableText` — which is how it survived
two full passes.

Underneath that was the gap this pass set out to close: GTK does not name an
entry after the caption beside it. The caption is a separate object joined by a
`LABELLED_BY` relation, so a form reading "Name:" on screen has an entry whose
own name is empty, and a match by name lands on the half that cannot be typed
into. The bridge follows the relation now, and `settext` prefers an editable
candidate when a label matches more than one thing.

`check_errors` had the same shape of blindness. A toolkit puts the message in a
child label, so `zenity --error --text=…` produces a dialog whose name is the
title and whose text is empty. Only the dialog element's own name and text were
read, so the only error boxes it caught were the ones that repeated the word
"error" in their *title* — and an application that titles its error box with its
own name, which is most of them, was invisible.

Both are verified against the container, not asserted: a GTK entry reports its
label through the relation, `settext --name` writes to it, reading back returns
what was written, and an error dialog titled "Archiver" with the failure in its
body is now found and its message returned.

### 11.4 The `gst-launch` child stays, and the docs were wrong

The contradiction was real and it was in the documentation. Four places spawn a
`gst-launch` child — two screenshot paths, the recorder, and the roomless
`start_restream` fallback — and the README and CLAUDE.md both said GStreamer
never runs as one.

The claim is true of the live pipeline and wrong about the side ones, and the
asymmetry is deliberate rather than left over. Those pipelines are assembled
from what a caller asked for — a codec, a container, a bitrate, a URL — and
`start_recording` is an MCP tool, so those parameters come from an agent. A
combination this host cannot satisfy, or a disk that fills halfway through, ends
the child and nothing else. In-process, the same fault would be in the address
space serving every viewer, and a recording nobody is watching would take down
the stream they are. It is the same reasoning that makes `-mcp-stdio` a separate
process.

The docs now say which is which and why. The code was not churned to match a
sentence that was too short.

### 11.5 Perception: the budget, measured

The instruction for stage 2 was to budget for perception being the ceiling.
Measuring it produced something more useful than a warning.

Against a desktop with a terminal open, best of three, on the running container:

| | latency | returned | ≈ context |
|---|---|---|---|
| `get_active_window` | 30 ms | 127 chars | ~31 tokens |
| `list_windows` | 30 ms | 629 chars | ~157 tokens |
| `screenshot_region` 640×480 | 31 ms | PNG | ~410 tokens (vision) |
| `screenshot` full 1920×1080 | 68 ms | 30 KB PNG | ~2,800 tokens (vision) |
| `ui_tree` depth 4 | 190 ms | 7.7 KB | ~1,900 tokens |
| `ui_find` one role | 758 ms | 34 chars | **~8 tokens** |
| `ui_diff` | 768 ms | 1.9 KB | ~481 tokens |
| `read_screen_text` | 770 ms | 625 chars | ~156 tokens |
| `ui_tree` depth 12 | 774 ms | 82 KB | **~20,400 tokens** |

The finding is that **latency and context cost are almost uncorrelated**.
`ui_find` and a full `ui_tree` cost the same 770 ms and differ by a factor of
2,500 in tokens. The expensive-to-run calls are not the expensive-to-return
ones, and a runtime that optimises for either number alone will get the other
badly wrong.

What follows for the design:

- **Never walk the tree to look around.** A full `ui_tree` is a fifth of a small
  model's context for one glance. It is a debugging tool and a last resort, not
  a perception primitive.
- **Pay the walk once, ask a narrow question.** The ~770 ms is the cost of
  traversing AT-SPI at depth, and it is charged whether eight tokens come back
  or twenty thousand. `ui_find` is the right default.
- **The cheap-to-run calls are cheap to return too.** `list_windows` and
  `get_active_window` are 30 ms and a hundred-odd tokens. Orientation should
  start there and descend, not start with a screenshot.
- **A screenshot is not the expensive option.** Full-screen vision is ~2,800
  tokens against `ui_tree`'s 20,400, and it arrives in 68 ms rather than 774.
  Where the accessibility tree is thin — Chromium, a canvas, a video — the
  picture is both faster and cheaper, which is the opposite of the assumption
  that led to an accessibility-first design.

That last point is the one worth carrying forward. Accessibility-first is right
when the tree is *good*, because it is exact and it is small. It is not right
because it is cheap.

### 11.6 What is still open, deliberately

- **§5.4, catalogue metadata** (`idempotent`, `handles_credentials`,
  `external_side_effect`, `default_timeout`, `result_compaction`) — unchanged
  and still correct: add them when the overlay needs them. Building a
  speculative table with twelve unused fields first is how the risk maps went
  wrong.
- **§5.5, the declared protocol version.** `2024-11-05` predates the tool
  annotations this server already sends. Still a decision, not work, and it
  should be made deliberately rather than drifted into.
- **`docs/tool-sweep.*`** are stale against this pass.
- **`docs/architecture.png`** still says 106 tools; the catalogue is 118.
- **`tool_search` rank one at 82%** — good enough to build on, not good enough
  to trust blindly.

None of these blocks starting the runtime.

## 12. Three things the runtime has to decide that the catalogue cannot express

These came out of a live demo rather than a review, which is why they are worth
writing down: none of the three is a defect, and all three are decisions the
server currently makes by accident.

The demo was "install nginx and show it to somebody". Installing it took
`install_packages` and two `run_command` calls, and a person watching the shared
desktop at the time **saw nothing happen at all**. That is not wrong. It is that
nothing in the system expressed the alternative.

### 12.1 There is no visibility axis, and the obvious proxy is wrong

The first attempt to classify the catalogue by "will a human see this" used
`RequiresControl`, and produced a list that included `browser_open` as invisible
— a tool that had just opened Chromium on screen while somebody watched.

`RequiresControl` does not mean visible. It means **injects events through
XTEST**. The honest taxonomy is three-way, and the catalogue encodes a two-way
split that answers a different question:

| | | examples |
|---|---|---|
| 19 | injects input — seen, and gated by the room | `mouse_click`, `type_text`, `terminal_run`, `ui_click` |
| ~25 | changes the screen without injecting — **seen, ungated** | `launch_app`, `browser_open`, `close_window`, `move_window` |
| ~25 | changes real state, **zero pixels** | `run_command`, `install_packages`, `write_file`, `ssh_*`, `shell_*`, `service_control`, `snapshot_*` |

The third group is the answer to "how was that done invisibly". Those tools
never touch X, so they do not even reach the room gate — there is nothing to
claim and nobody to notice.

The case that makes it concrete: **`run_command` and `terminal_run` do the same
job.** One is invisible and ungated, the other is visible and gated. Nothing in
the catalogue, the policy or the room says which one is appropriate, and a model
choosing on its own will take the first — it is simpler and it returns the
output more cleanly.

**What stage 2 needs:** a third field on `toolDef`, beside `Risk` and
`RequiresControl` — `hidden` / `visible` / `injects` — validated at startup like
the other two. This is the trigger §5.4 was waiting for. Note that the field it
turns out to need is not one of the five listed there, which is the argument for
having waited.

### 12.2 Observability is a role, not a judgement call

Three situations, and they want different behaviour:

- **`efficient`** — nobody watching, no evidence asked for. The invisible path
  is the right one; making a person's desktop flicker for a package install is
  theatre.
- **`witnessed`** — somebody asked for a recording, screenshots, or a
  demonstration. Here the invisible path must be **closed**, not discouraged:
  where a visible equivalent exists, `run_command` becomes `terminal_run` and
  the runtime substitutes it. The evidence cannot depend on the model
  remembering to be observable.
- **`ask`** — a person granted control while they were working. The agent should
  ask whether they want to watch it happen or have it run in the background,
  because that is a question about their attention, not about the task.

The mechanism for the third already exists and was exercised in the demo:
`request_control` blocked and put a prompt in the human's browser. That is
agent-to-human questioning, already built and already wired to the room. An
`ask_human` generalises it — the same path, different text — and it runs in the
opposite direction to the event channel from §11.2, which completes the pair.

### 12.3 The audit trail exists, and it does not survive a restart

`action_log` recorded the whole demo, correctly and usefully — including the
things that went wrong, which is the part that matters:

```
12:03:24  tool_search      {"query":"install the nginx web server"}   ok
12:03:36  install_packages {"packages":["nginx"]}                     ok
12:06:06  run_command      nginx -t …            (no root, failed)    ok
12:06:22  run_command      {"as_root":true, …}                        ok
12:08:44  activate_window  {"match":"nginx"}                          FAILED
```

A person asking "how did you install it" can be answered from that, exactly.

Four limits, and the first is the serious one:

- **It is memory only.** `NewActionLog` keeps a ring of 2000 and writes to disk
  only when `ACTION_LOG` is set. That variable **is set nowhere** — not in the
  compose file, not in `config.Config`, not in the README's variable table. It
  is read straight from the environment, which is also why it escaped the
  convention that every knob has a row in that table. Restart the container and
  the audit is gone. For a property the user has asked for by name, the default
  is the wrong way round: this should be on, with the path a knob for moving it,
  not off with a knob for existing.
- **2000 entries is roughly two hours** at the rate of this session. An agent
  working a full day loses its own morning.
- **Arguments are recorded, results are not.** "How did you install it" is
  answerable; "what did apt actually print" is not. For an audit that is
  usually the more interesting half.
- **There is no task identity.** Entries carry `conn`, and this session shows 43
  distinct connections because the CLI spawns a bridge per call. Nothing groups
  "these seven calls were the nginx task". Per-call provenance is not the same
  as a trail, and the trail is what somebody asking for an audit wants.

**What stage 2 needs:** persistence on by default; a task or run id threaded
from the runtime through `_meta` so entries group; and the result, or a bounded
summary of it, stored beside the arguments. The runtime is also the only thing
that knows *why* a call was made, and a trail that records the goal alongside
the calls is worth more than one that records the calls alone.

### 12.4 Why these three belong together

They are one property seen from three sides. **A thing that happened should be
recoverable** — while it happens (visibly, if somebody is watching), by asking
(the role decides which), and afterwards (the log). Today the server does the
first by accident, has no mechanism for the second, and loses the third on
restart.

None of this blocks starting the runtime. All of it is easier to build into the
runtime than to retrofit, which is why it is written down before rather than
after.
