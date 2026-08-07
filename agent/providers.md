# Providers

Which models `sentineldesk-agent` can drive, and how to run each one.

```bash
sentineldesk-agent providers
```

prints this table with your own keys' state filled in. That is the version to
trust: this file says what a key is called, that one says whether you have it.

| `--provider` | key | default model | caching |
|---|---|---|---|
| `anthropic` *(default)* | `~/.sentineldesk/anthropic.key` | `claude-sonnet-5` | explicit markers, set by this runtime |
| `ollama` | none | `qwen3:8b` | none (server-side KV) |
| `ollama-cloud` | `~/.sentineldesk/ollama.key` | `qwen3:480b-cloud` | none |
| `openai` | `~/.sentineldesk/openai.key` | `gpt-5.2` | automatic, on long prefixes |
| `openrouter` | `~/.sentineldesk/openrouter.key` | any vendor's | depends on the model behind it |

Flags go **before** the subcommand:

```bash
sentineldesk-agent --provider ollama --model qwen3:4b run "…"
```

---

## Keys

One file per provider, outside any checkout, readable by nobody else:

```bash
mkdir -p ~/.sentineldesk && chmod 700 ~/.sentineldesk
cat > ~/.sentineldesk/anthropic.key      # paste, Enter, Ctrl-D
chmod 600 ~/.sentineldesk/anthropic.key
```

`cat >` rather than `echo`, so the key does not land in shell history.

A file rather than an environment variable, and the ordering is deliberate: an
environment variable is readable from `docker inspect`, from
`/proc/<pid>/environ` by anything running as the same user, from a crash dump,
and by every child process — and this runtime spawns children.
`<NAME>_API_KEY_FILE` (the Docker secret convention) overrides the path;
`<NAME>_API_KEY` works and is the leakiest, so it is checked last.

A key file readable by others is **refused**, not warned about. A warning is
read once, while somebody is trying to get something working.

---

# Anthropic

The default, and the one the catalogue was written for: the tool annotations
this project publishes are the MCP specification's own hints, and the
descriptions were tuned against a model that reads them.

```bash
cat > ~/.sentineldesk/anthropic.key && chmod 600 ~/.sentineldesk/anthropic.key

sentineldesk-agent -container sentineldesk run "what windows are open?"
```

### Choosing a model

```bash
# The default. Good tool use, sensible price.
sentineldesk-agent -container sentineldesk run "…"

# Cheapest. Worth trying for anything mechanical — read a file, list windows,
# check whether something is running.
sentineldesk-agent -container sentineldesk \
  --model claude-haiku-4-5-20251001 run "is chromium running?"

# When the task needs real planning: several applications, recovering from a
# failure, deciding what to do when the screen is not what was expected.
sentineldesk-agent -container sentineldesk \
  --model claude-opus-5 run "set up a python project and run its tests"
```

Model ids change. `costs` groups by whatever you passed, so running the same
goal on two of them and comparing the rows is the way to decide — the numbers
are in your own database, not in this file.

### Caching

The only provider here that takes explicit markers, and this runtime sets them:
the system prompt and the tool catalogue are cached together and re-read at a
fraction of the rate. Measured on one real question:

| | input | cache | est. USD |
|---|---|---|---|
| before | 38,814 | — | 0.0776 |
| caching | 1,140 | 37,796 read | 0.0098 |
| + tool selection, warm | 1,140 | 6,952 read | **0.0037** |

A **new goal** selects different tools and therefore pays one cache write. Two
runs of the same question are cheaper than two different ones, and the run
summary says which happened:

```
cache: 0 written, 6,952 read
```

`0 written` means the previous run's cache was still warm.

---

# Ollama — on this machine

No key, no bill, no network. Which makes it the right place to develop against:
a run can be repeated as many times as it takes.

```bash
brew install ollama
ollama serve                    # leave it running
ollama pull qwen3:4b

sentineldesk-agent -container sentineldesk \
  --provider ollama --model qwen3:4b run "what windows are open?"
```

### The model has to support tool calling

This is the first thing that goes wrong, and it fails confusingly: a model
without tool support answers in prose about what it *would* do, the loop sees no
tool calls, and the run ends after one turn having done nothing.

Known to work: the `qwen3` family, `llama3.1` and later, `mistral-nemo`.
`ollama show <model>` lists `tools` under capabilities when it has them.

```bash
ollama show qwen3:4b | grep -A3 Capabilities
```

### What is actually slow: the model thinking, not the catalogue

Measured rather than assumed, and the measurement contradicted the assumption.

`qwen3:4b` on an i9-10900, one question, two turns:

| turn | seconds | in | out | tok/s |
|---|---|---|---|---|
| 1 | 138.5 | 2,414 | 678 | 4.90 |
| 2 | 213.2 | 2,598 | 1,252 | 5.87 |

1,930 output tokens at 5.5 tok/s is 352 seconds — **the entire run**. Processing
the prompt was noise. And turn 2 spent 1,252 tokens to write two lines of
answer: the rest was qwen3 reasoning, which Ollama's `/v1` endpoint discards
before the runtime ever sees it.

So the machine spent minutes generating text nobody reads.

**Prefer a model that does not think.** The loop already plans, acts and
observes explicitly; a reasoning model's internal deliberation is duplicated
work that is then thrown away. `qwen2.5`, `llama3.2` and `mistral-nemo` support
tools without it.

`think: false` does not help — checked. It does not remove the reasoning, it
moves it into the answer, which is worse:

```
think=true   →  158 tokens, 678 chars of thinking in its own field, content "OK"
think=false  →  105 tokens, no thinking field, content "Hmm, the user just said…"
```

### Keeping the catalogue small still helps, just less

```bash
sentineldesk-agent -container sentineldesk \
  --provider ollama --model qwen3:4b --tools 6 \
  run "take a screenshot"
```

`--tools 6` offers the core set plus six, and if the model needs something else
it calls `tool_search` and the runtime widens the set. It matters here for a
different reason than on a hosted provider: not the seconds, but that a small
model given a hundred and twenty schemas chooses badly among them.

**An explicit `--tools` is honoured even when the ranking finds nothing.** A goal
the English vocabulary does not match normally falls back to offering
everything — the right recovery when the prefix is cached and a wrong small set
costs turns. On a CPU that is the wrong recovery, so a number you typed wins.

### The GPU

**On macOS, Ollama accelerates through Metal and enables it on Apple Silicon
only.** AMD goes through ROCm, which is Linux. On an Intel Mac it runs on CPU
whatever card is in the machine — verified on an i9-10900 with a Radeon RX 580,
where `discovering available GPUs` reports `library=cpu` and nothing else.

```bash
grep -i "inference compute" ~/.ollama/logs/server.log   # or wherever it logs
```

It works. It is not fast. RAM is usually the real limit and a workstation
usually has plenty.

### What `costs` will say

Nothing, which is correct: a local model reports no billing and has no rate, so
`costs` shows `?` rather than zero for it. Reporting a day's work as free is the
worst available way to be wrong about a bill, so an unknown rate says so.

---

# Ollama — the hosted models

The same protocol against Ollama's servers. One key and a different
`--provider`; worth having when the machine cannot do the work.

```bash
cat > ~/.sentineldesk/ollama.key && chmod 600 ~/.sentineldesk/ollama.key

sentineldesk-agent -container sentineldesk \
  --provider ollama-cloud --model qwen3:480b-cloud \
  run "open a terminal and show the free disk space"
```

Models with the `-cloud` suffix are the hosted ones. `ollama ls` after signing
in shows what your account can reach.

---

# OpenAI

```bash
cat > ~/.sentineldesk/openai.key && chmod 600 ~/.sentineldesk/openai.key

sentineldesk-agent -container sentineldesk \
  --provider openai --model gpt-5.2 run "what windows are open?"
```

Caching is **automatic** on long prefixes — nothing to mark, and it reports how
many tokens it reused. The runtime puts the system prompt and catalogue first
for exactly that reason, so the same shape that earns Anthropic's cache earns
this one's without a second code path.

Cached tokens arrive folded into the prompt count here rather than beside it, so
the adapter subtracts them: `InputToks` keeps meaning what it means everywhere
else, which is what was paid at the full rate.

---

# OpenRouter

One key, many vendors' models. The reason to have it is comparison — running the
same goal across three vendors without three accounts.

```bash
cat > ~/.sentineldesk/openrouter.key && chmod 600 ~/.sentineldesk/openrouter.key

for m in anthropic/claude-sonnet-5 openai/gpt-5.2 google/gemini-3-pro; do
  sentineldesk-agent -container sentineldesk \
    --provider openrouter --model "$m" run "what windows are open?"
done

sentineldesk-agent costs     # one row per model, same question, same desktop
```

Caching depends on whichever model is behind it and is not reported uniformly,
so `providers` says `none` rather than promising something it cannot verify.

---

## Prices

Tokens are recorded; money is derived.

```jsonc
// ~/.sentineldesk/pricing.json — dollars per million tokens
{
  "sonnet": { "input": 2.00,  "output": 10.00 },
  "opus":   { "input": 15.00, "output": 75.00 },
  "haiku":  { "input": 1.00,  "output": 5.00 },
  "gpt":    { "input": 1.25,  "output": 10.00 }
}
```

The key is matched as a substring of the model id, longest match winning, so a
dated release inherits its family's rate instead of falling to zero. Cache
multipliers (`cache_write_multiplier`, `cache_read_multiplier`) default to 1.25
and 0.10 and are overridable in the same file.

A price changes, and a price stored beside each row would then be wrong for
every past run with no way to notice. Correcting this file re-prices the whole
history, not only what comes next.

**The built-in rates are estimates**, there so `costs` prints something useful
on the first day. Calibrate against your own invoice: take the token counts
`costs` reports, take what the console says was spent, and adjust.

---

## Adding one

A provider that speaks chat-completions — most do — is a row in `Presets`
(`agent/internal/provider/openaicompat.go`): id, base URL, key name, default
model, what it does about caching, and one sentence for `providers` about the
thing somebody has to know. Groq, Together, DeepSeek and a local `llama.cpp`
server all fit there.

One that does not gets its own file implementing `Provider` — three methods, and
`KeySource()` if it holds a credential. `anthropic.go` is the example; the
mapping between this package's types and the wire format is the whole job, which
is also why no SDK is used. A dependency that changes its own types underneath
the loop is a dependency that owns the loop.

### Caching, and why there is no standard

| | how |
|---|---|
| Anthropic | explicit `cache_control` markers on the blocks you choose |
| OpenAI | automatic on long prefixes; nothing to mark |
| Gemini | a cached-content object with its own lifetime and API |
| local | a KV cache in the server; no request field reaches it |

So `Request.CacheStable` is a hint about the **shape** of the request — "this
prefix will repeat unchanged" — and each adapter does what its provider
understands. A provider that ignores it is correct, just more expensive.

Cache tokens are counted separately everywhere: in the store, in `costs`, in the
run summary. A cache that quietly stopped matching produces identical answers at
ten times the price, and folded into one number that is invisible.
