# What to take from OpenClaw

OpenClaw is a personal-assistant runtime in TypeScript: a Gateway wiring models,
tools, channels and companion apps, with a reusable `@openclaw/agent-core`
holding the loop, compaction and session storage. It is the same *kind* of thing
stage 2.1 is building, several years further along, so it is worth reading for
design rather than for code — none of it ports, all of it argues.

This is not a summary of that repository. It is the short list of places where
reading it changed what we should do, plus the places where it confirms
something we arrived at the hard way and should now write down as a rule instead
of rediscovering.

---

## 1. The rule we found eight times and never wrote down

From their product doctrine:

> Severity order: **silent failure > crash > missing feature**. Every user or
> agent action ends in a visible outcome or a recorded, intentional non-outcome.
> An action that produces nothing, with nothing explaining why, is the worst bug
> class in this repo.

We reached that conclusion empirically and expensively. Across three passes of a
catalogue that was green every time: the recorder writing a profile no hardware
decodes, `browser_type` writing where the page could not see, `find_text` unable
to match a phrase, unknown arguments silently ignored, `ssh_tunnel_local`
returning an id for a forward the server refused, `activate_window` accepting a
`match` it did not use, `check_errors` unable to read the message, and
`fill_form` which had **never filled anything**.

Every one of them returned `ok`.

Adopt the severity order as stated, and adopt its consequence: **a test that
asserts a tool returned successfully has asserted almost nothing.** Ours already
work this way — the integration suite checks the artifact, not the reply — but
the rule belongs in CLAUDE.md where it governs new work rather than in a
postmortem where it explains old work.

## 2. "Latency is model round-trips, not milliseconds"

This one corrects us.

> Latency is model round-trips, not milliseconds. Collapse act-then-observe
> pairs into one tool result; keep expensive resources warm across a session.

Stage 1 §11.5 measured perception in milliseconds and tokens and built a budget
out of both. That budget is still right as far as it goes, and it is measuring
the wrong unit for the thing that actually costs: a 30 ms tool that forces the
model to call again to find out what happened is more expensive than a 770 ms
tool that answers completely.

It also explains, after the fact, why `open_app_and_wait` is a better tool than
`launch_app` followed by `wait_for_window`, and why `terminal_run` returning the
output *and* the exit code is worth more than its latency suggests.

**For 2.1:** the loop's cost model is round-trips first, tokens second,
milliseconds third. And for the catalogue: a tool whose result does not say what
happened next is a design defect, not a terse implementation.

## 3. "Record facts where they happen"

> Record facts where they happen; read them where they are needed. Answering
> "did X happen?" by combining several indirect signals rots as sibling paths
> evolve; prefer a recorded fact at the boundary that owns it.

This names, precisely, the mistake made while writing stage 1 §12 — classifying
the catalogue by visibility using `RequiresControl` as a proxy, and producing a
list that called `browser_open` invisible. The fix was the `Visibility` field:
a recorded fact at the boundary that owns it.

The same line is the argument for the `task_id` and `goal` in the action log,
and against every future temptation to infer a session from timestamps and
connection numbers.

## 4. Tool results are prompts

> The model's experience is the product. Capability that prompt/tool text does
> not mention — or contradicts — does not exist for users. Tool results are
> prompts: return what the model needs next, not a bare ack. Review prompt and
> description text with the same rigor as code.

We have two proofs of this already. `tool_search` recall went from 76% to 100%
by changing *descriptions and vocabulary*, no ranking algorithm involved — and
`ui_find`'s role examples said "push button" and "entry" while GTK says "button"
and "text", so the tool was correct and its documentation sent every caller to
the wrong place.

**For 2.1:** tool descriptions are reviewed like code, and the search corpus in
`internal/mcp/search_test.go` is the mechanism that keeps them honest.

## 5. Never dead-end the agent

> failure text states what to try next; unavailable tools are hidden by gating,
> not left to fail; missing pieces provision automatically where safe.

We do the middle one: `tools/list` is filtered by policy so a tool that would be
refused is never advertised. We partly do the first: denial *kinds* let a client
tell "give up" from "ask a person and retry", which is the distinction that
matters most.

What we do not do consistently is the sentence. Some refusals say what to try
(`sudo_status`'s hint, `fill_form`'s new one about matching a caption); most say
what went wrong and stop. That is a catalogue-wide pass worth doing, and it is
cheap: it is text.

## 6. Interrupted turns have to be *told*

From `packages/agent-core/src/turn-interruption.ts`, injected into the next turn
after an abort:

> The previous turn was interrupted. Any running background processes may still
> be active. If any tools or commands were aborted, they may have partially
> executed.

And the refinement that makes it correct:

> Aborts that end a turn as an intentional handoff mark it with
> `turnHandoff: true`. Interruption guidance is skipped for them: the next turn
> would otherwise be told tools may have partially executed after a clean,
> deliberate stop.

Our cancellation works at the protocol level — `notifications/cancelled`
interrupts a call in flight, and the result that arrives afterwards is dropped
rather than answered. Nothing says what the *runtime* tells the model next, and
the honest answer is uncomfortable: a cancelled `run_command` may have installed
half a package. The model has to know that before it plans its next step.

This is the same shape as our denial kinds — a deliberate stop is a different
situation from a messy one — and it is the strongest single thing to copy.

**For 2.1:** after any abort, the next turn carries an interruption notice, and
a deliberate stop is marked so it does not carry one.

## 7. Compaction must not drop the structured facts

`agent-core`'s compaction summarises the transcript but carries a separate
`CompactionDetails` across the boundary — the files read and the files modified
— and merges it forward from the previous compaction. The prose is compressible;
the list of what was touched is not.

Our equivalents are not files. They are: which windows were opened, which
packages were installed, whether the controls are held, which task id is in
flight, and what was already tried and failed. **Those survive compaction as
structured state, never as a sentence in a summary.**

## 8. Skills: the standard format, with our extras namespaced

The requirement is that community skills work — the ones written for Claude
Code, Codex or Cowork, including whatever somebody publishes on a marketplace.

OpenClaw already solves this and the solution is unremarkable, which is the
point. Their skills are `SKILL.md` files with the standard Anthropic Agent
Skills frontmatter:

```yaml
---
name: node-inspect-debugger
description: Debug Node.js with node inspect, --inspect, breakpoints, CDP, heap, and CPU profiles.
metadata: { "openclaw": { "emoji": "🪲", "requires": { "bins": ["node"] } } }
---
```

`name` and `description` are the contract every host understands. Everything
OpenClaw-specific — an emoji, required binaries, install steps, a primary
environment variable — lives under `metadata.openclaw`, where another host
ignores it.

That is exactly the pattern we already use one layer down: `readOnlyHint` and
`destructiveHint` are the specification's, `sentineldesk/requiresControl` and
`sentineldesk/visibility` are ours and namespaced so they cannot collide.

**For 2.1:**

- Read plain `SKILL.md` with standard frontmatter. A skill from any host loads.
- Put anything of ours under `metadata.sentineldesk` — and the obvious candidate
  is `requires`, since a skill about administering Linux is only useful if the
  binaries it names exist, and `list_commands` can answer that before the model
  wastes a turn finding out.
- Never require our extensions. A skill without them loads and works.
- Progressive disclosure as the format intends: the frontmatter is what the
  model sees while choosing, the body loads when it is chosen. That is the same
  argument as `MCP_DISCOVERY`, applied to skills.

## 9. Two conventions worth stealing outright

**Production lines are a constraint; test lines are not.** They count them
separately and prefer net-neutral or net-negative production changes, requiring
a named justification — a capability, an ownership boundary, a security
invariant, a public contract — for growth. Their agent loop is 1,614 lines
against 2,931 lines of test for it.

**The pathfinder rule.** *Leave touched code better than found. Never silently
walk past an unrelated issue discovered mid-task — fix it in the same change
when small and bounded, otherwise record it as a named follow-up.* This is how
this project has actually been working; naming it makes it a rule rather than a
habit.

## 10. What not to copy

Their `auto` runtime selection resolves a harness from provider and model routes
through several layers of precedence. It exists because they support many
runtimes and many providers, and it is a large amount of machinery for a
question we do not have: there is one desktop and one MCP server.

Their Gateway multiplexes messaging channels — Telegram, Desktop, and others.
Our equivalent surface is the room, which already exists and is not a channel
abstraction.

Both are the right design there and would be speculative generality here. Their
own doctrine says so better than this section does: *extra layers, guards, and
generality for imagined inputs are defects, not rigor.*

---

## Actions

Ordered by what they change.

| | Where |
|---|---|
| Interruption notice after an abort, suppressed for a deliberate stop | 2.1, the loop |
| Cost model is round-trips first | 2.1, `stage-2-agent-engine.md` §3.3 |
| Structured facts survive compaction, never as prose | 2.1, persistence |
| `SKILL.md` standard frontmatter, extras under `metadata.sentineldesk` | 2.1, skills |
| Severity order and the pathfinder rule as written conventions | `CLAUDE.md` |
| A refusal says what to try next | catalogue-wide text pass, this repo |
