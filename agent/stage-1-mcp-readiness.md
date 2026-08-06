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

### 4.2 Denial reasons are strings, not kinds

Three unrelated things fail as prose today, all with `isError: true`:

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

Kinds: `policy`, `room`, `unknown_tool`, `invalid_arguments`, `emergency`,
`tool_error`. The human sentence stays exactly as it is — it is what reaches the
model, and it is good.

**Acceptance.** A test per kind, asserting both the string and the code.

### 4.3 `tools/call` cannot be cancelled

`dispatch()` takes no `context.Context`. Tools that need a deadline build their
own from `context.Background()` — `internal/mcp/tools.go:514`,
`tools_admin.go:67`, `tools_admin.go:420`. Each request runs in its own
goroutine and nothing holds a cancel function.

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

### 5.1 No progress notifications

`install_packages` and `snapshot_create` can run for minutes and say nothing
until they finish. The Agent Console timeline wants `agent.tool.progress`, and
there is nothing to feed it. The 2024-11-05 protocol has
`notifications/progress` with a `progressToken`; it is not implemented.

Worth doing for the handful of genuinely long tools rather than all 115.

### 5.2 No `tools/list_changed`

The server neither declares the capability nor emits the notification, so a
client cannot know the catalogue moved. It matters more than it looks: the
visible set changes when a connection restricts itself, and the runtime pins a
catalogue hash into every approval record. Without a signal it must refresh on
reconnect and poll.

The catalogue is static per process today, so the honest MVP answer is: keep
refreshing on reconnect, and do not invent the capability. Revisit when
something can actually change the catalogue at runtime.

### 5.3 Connections have no identity

Two things need this, and the second is the one that makes it worth doing.

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
| 3 | Structured denial kinds in `tools/call` | Blocks the loop's state machine (§4.2) | small |
| 4 | Thread `context.Context` through `dispatch`; honour `notifications/cancelled` | Blocks honest cancel (§4.3) | medium |
| 5 | Audit which tools ignore cancellation; publish the list | Without it, step 4 is a half-truth | medium |
| 6 | Connection identity from `initialize`, carried into the action log | Emergency gate, and attribution once sub-agents run concurrently (§5.3) | small |
| 7 | `notifications/progress` for the long tools | Timeline quality | medium |

Steps 1–3 are a day. They are also the ones that unblock the most of stage 2.

**Not in stage 1.** Do not build the agent's tool search, policy overlay, skills
or persistence in this repository. They belong to the runtime, and putting them
here would make the desktop depend on the brain — which is the wrong direction
and would end the property that the desktop survives the runtime crashing.

---

## 7. Decisions to make before stage 2

These are architectural and cannot be derived from the code. Write each as an
ADR under `docs/adr/` once decided.

### 7.1 One Go module or two?

`agent/` inside this module can import `github.com/lordbasex/sentineldesk/internal/...`
— Go's `internal` rule allows it anywhere under the module root. A separate
module cannot, and would need the shared contract published as a non-internal
package.

**Recommendation: one module, `agent/` as a subtree, `agent/cmd/sentineldesk-agent/`.**
It keeps the option of splitting later at the cost of one directory move,
whereas starting split and merging later means undoing a published API. "Two
projects" is a statement about lifecycle and deployment, and one module does not
prevent it.

### 7.2 The agent binary must not link CGO

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

`3f1ee86` added `tool_search` to the MCP server. The runtime should **not** use
it: after `tools/list` the catalogue is already in memory, and searching it over
JSON-RPC is a round trip to ask itself a question. The server-side tool exists
for external hosts — Claude Code, Claude Desktop — that have no runtime.

Two consequences:

- The ranking logic (`searchTools`, the category rules and aliases in
  `internal/mcp/registry.go`) should move somewhere both can import, so the two
  rank identically and there is one place to improve.
- The runtime should deny itself `tool_search` in its `sentineldesk/policy`
  handshake, or its model will spend calls on a tool the runtime answers
  locally.

### 7.4 Does the runtime run under supervisord, and does it degrade?

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
