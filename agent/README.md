# sentineldesk-agent

The runtime that drives the desktop without an AI host in the middle. It is an
ordinary MCP client and holds no privilege the socket does not grant — if it can
only do what Claude Code can do through the same socket, then the socket is the
security boundary and there is one boundary rather than two.

This is how to use it. [`providers.md`](providers.md) covers the models,
[`stage-2-agent-engine.md`](stage-2-agent-engine.md) covers why it is built this
way.

---

## Getting going

**1. The key, outside any checkout.**

```bash
mkdir -p ~/.sentineldesk && chmod 700 ~/.sentineldesk
cat > ~/.sentineldesk/anthropic.key      # paste, Enter, Ctrl-D
chmod 600 ~/.sentineldesk/anthropic.key
```

`cat >` rather than `echo`, so it does not land in shell history. A key file
readable by others is refused, not warned about.

**2. Build it.**

```bash
make agent          # this machine
make agent-linux    # linux/amd64 and linux/arm64, which is where it ships
```

**3. Check the connection before spending anything.**

```bash
./bin/sentineldesk-agent -container sentineldesk doctor
```

Fifteen checks, no model involved. If this fails the problem is not the key.

**4. Run something.**

```bash
./bin/sentineldesk-agent -container sentineldesk run "what windows are open?"
```

### Where it runs

`--container` is the development path: the binary runs on your machine, reaches
the desktop through `docker exec`, and the API call goes out from here. **The
key never enters the container.**

Without it, the binary opens the daemon's socket directly — which is how it
ships, as a supervised process inside the container beside the daemon (ADR-004).

---

## The commands

| | |
|---|---|
| `doctor` | fifteen checks against a real desktop: the catalogue, the annotations the runtime needs, denial kinds, events, progress, cancellation, the audit trail. No model. |
| `run "goal"` | work towards a goal |
| `tools [query]` | the catalogue, or the goal-ranked search of it |
| `providers` | which models can be reached and whether their keys are present |
| `costs` | what has been spent, and what the catalogue costs |
| `history [id]` | past runs, or one run in full — including the system prompt as sent |

`costs`, `history` and `providers` read the local database and need no desktop.

---

## The flags

They go **before** the subcommand.

```
--provider   anthropic | ollama | ollama-cloud | openai | openrouter
--model      which model (default: the provider's)
--role       efficient | witnessed
--tools      goal-matched tools on top of the core set (default 12; 0 = all)
--max-turns  stop a run after this many (default 25)
--container  reach the desktop through docker exec instead of the socket
```

### `--role`

`efficient` is the default: nobody is watching and nobody asked for evidence, so
the invisible path is correct. Making a desktop flicker for a package install is
theatre.

`witnessed` is for when somebody asked to **see** it happen — a demonstration, a
recording, a screenshot. The invisible path is closed: where a visible
equivalent exists the runtime substitutes it, so `run_command` becomes
`terminal_run` and the work happens in a terminal on screen.

That substitution is enforced by the runtime, not requested of the model.
Evidence cannot depend on a model remembering to be observable.

```bash
./bin/sentineldesk-agent -container sentineldesk \
  --role witnessed run "open a terminal and show the free disk space"
```

Watch it at `localhost:8080` while it runs.

### `--tools`

The catalogue was 98% of what a turn cost. A run now starts with a core set —
see, orient, act, ask, take the controls — plus whatever the goal ranks highest,
and reaches everything else through `tool_search`, which is always offered.

`--tools 0` offers all 120. That is the escape hatch for the day a run fails and
the first question is whether the selection caused it.

---

## Stopping it

Ctrl-C cancels the run rather than killing the process, so the cancellation
reaches the server and the tool in flight actually stops. Exiting instead would
leave a command running with nobody to answer.

A person taking the controls also ends a run — the runtime is told, rather than
finding out when its next click is refused — and the exit code says so:

| | |
|---|---|
| `0` | finished |
| `1` | failed |
| `3` | the provider is not configured (the message says what to do) |
| `4` | interrupted: somebody took the controls |

---

## Reading what it cost

```bash
./bin/sentineldesk-agent costs
```

```
MODEL                     RUNS TURNS CALLS       IN      OUT  EST. USD
anthropic/claude-sonnet-5    6    12     6   45,506    1,414    0.1247

What the catalogue costs
  17 tools per turn · ~3,476 tokens · 76% of every turn's input
```

Tokens are recorded exactly as the provider reported them; money is derived from
`~/.sentineldesk/pricing.json`, which you correct against your own invoice.
Correcting it re-prices the whole history rather than only what comes next.

**The built-in rates are estimates.** A model with no rate prints `?` rather
than zero — reporting a day's work as free is the worst available way to be
wrong about a bill.

### What one run actually did

```bash
./bin/sentineldesk-agent history 5
```

Turn by turn: the system prompt as it was sent, how many tools were offered,
every call with its arguments and result, and any tool the runtime substituted
alongside what the model asked for. The full request and response JSON is in the
database if a summary is not enough.

Everything lives in `~/.sentineldesk/agent.db`, mode 0600, outside any checkout.
It holds conversations; it is nobody's business but yours.

---

## What it costs, measured

The same question, four runs, as each piece landed:

| | tools offered | est. USD |
|---|---|---|
| as it first worked | 120 | **0.0776** |
| caching the prefix | 120 | 0.0098 |
| tool selection, cold cache | 17 | 0.0117 |
| both, warm cache | 17 | **0.0037** |

Twenty-one times cheaper. Two things did it, and they compose: caching stops
paying full price to **re-send** the catalogue, selection makes the catalogue
**smaller**.

The cache matches a byte-identical prefix, so the tool set is chosen once at the
start of a run and left alone. A new goal selects different tools and therefore
pays one cache write — which is why two runs of the same question are cheaper
than two different ones.

---

## When something goes wrong

**`could not reach the desktop`** — `make up`, then `doctor`. Without
`--container` the binary expects to be running beside the daemon as the desktop
user; from another machine, pass it.

**`anthropic is unavailable: no API key`** — the message names the file to
create. Exit code 3, distinct from a failure, because not configured and broken
want different things from you.

**A run says `interrupted`** — somebody took the controls. Nothing went wrong;
the desktop is shared and that is what sharing means. The next turn was told the
tools may have partly run, because a cancelled command may have installed half a
package.

**A tool was refused** — read the kind in `history`. `room` means ask a person
and retry; `policy` means the server will never allow it and no one in the room
can widen it. The loop tells the model which, because collapsing them turns
"wait your turn" into "give up".

---

## Testing it without spending anything

The loop's tests run against a scripted model and a fake server, deterministically
and offline:

```bash
go test ./agent/...
```

A scripted provider is not a mock — it implements the same interface, so the loop
under test is the loop that ships. It exists because the behaviours that matter
(a refusal it must retry, a refusal it must not, an interruption landing between
two calls of one batch) are things a real model produces only by luck, and
because a *good* model hides loop defects by reaching the right answer despite
them.

For a real desktop and no bill, run a model locally — see
[`providers.md`](providers.md).
