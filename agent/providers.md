# Providers

Which models `sentineldesk-agent` can drive, what each one needs, and what each
one does about the thing that dominates the bill.

```
sentineldesk-agent providers
```

prints the same table with your own keys' state filled in, which is the version
to trust: this file says what a key is called, that one says whether you have it.

---

## The table

| `--provider` | key | default model | caching |
|---|---|---|---|
| `anthropic` *(default)* | `~/.sentineldesk/anthropic.key` | `claude-sonnet-5` | explicit markers, set by this runtime |
| `ollama` | none | `qwen3:8b` | none (server-side KV) |
| `ollama-cloud` | `~/.sentineldesk/ollama.key` | `qwen3:480b-cloud` | none |
| `openai` | `~/.sentineldesk/openai.key` | `gpt-5.2` | automatic, on long prefixes |
| `openrouter` | `~/.sentineldesk/openrouter.key` | any vendor's | depends on the model behind it |

```bash
sentineldesk-agent --provider ollama --model qwen3:8b run "…"
```

`--provider` and `--model` go **before** the subcommand.

---

## The keys

One file per provider, outside any checkout, readable by nobody else:

```bash
mkdir -p ~/.sentineldesk && chmod 700 ~/.sentineldesk
cat > ~/.sentineldesk/anthropic.key     # paste, Enter, Ctrl-D
chmod 600 ~/.sentineldesk/anthropic.key
```

`cat >` rather than `echo`, so the key does not land in shell history.

A file rather than an environment variable, and that ordering is deliberate: an
environment variable is readable from `docker inspect`, from
`/proc/<pid>/environ` by anything running as the same user, from a crash dump,
and by every child process — and this runtime spawns children. `ANTHROPIC_API_KEY_FILE`
(the Docker secret convention) overrides the default path; `ANTHROPIC_API_KEY`
works and is the leakiest of the three, so it is checked last.

A key file readable by others is **refused**, not warned about. A warning is
read once, while somebody is trying to get something working.

Nothing in the repository can carry a key — `make check-secrets` scans tracked
files and fails the build, and it was verified by planting one and watching it
fail.

---

## Two adapters, five providers

`anthropic` has its own adapter: a different wire format, and explicit cache
markers.

The other four share one, because they genuinely speak the same protocol —
chat-completions, as OpenAI defined it and everyone else adopted so that
existing clients would work. Four near-identical files would be four places for
a fix to be applied three times. What differs between them is a row in a table:
the URL, the credential's name, and what the service does about caching.

**Adding a provider that speaks this protocol is that row.** The moment one of
them needs different code it stops being a preset and gets its own file, the way
Anthropic has.

---

## Caching, and why there is no standard

The system prompt and the tool catalogue are byte-identical on every turn and
are **98% of what a turn costs**. Every provider that can charge less for
repeating them does it differently:

| | how |
|---|---|
| Anthropic | explicit `cache_control` markers on the blocks you choose |
| OpenAI | automatic on long prefixes; nothing to mark |
| Gemini | a cached-content object with its own lifetime and API |
| local | a KV cache in the server; no request field reaches it |

So `Request.CacheStable` is a hint about the **shape** of the request — "this
prefix will repeat unchanged" — and each adapter does what its provider
understands. A provider that ignores it is correct, just more expensive.

Measured on one real question, same desktop, same answer:

| | input | cache | cost |
|---|---|---|---|
| before | 38,814 | — | **$0.0800** |
| after (cold) | 1,152 | 18,898 written / 18,898 read | $0.0564 |
| after (warm) | 1,140 | 37,796 read | **$0.0126** |

Six times cheaper. Cache tokens are counted separately from input tokens
everywhere — in the store, in `costs`, in the run summary — because a cache
that quietly stopped matching produces identical answers at ten times the price,
and folded into one number that is invisible.

---

## Running a model locally

`ollama` needs no key and costs nothing, which makes it the right place to
develop the loop: a run can be repeated as many times as it takes.

```bash
brew install ollama
ollama serve            # leave it running
ollama pull qwen3:8b
sentineldesk-agent --provider ollama run "what windows are open?"
```

Two things worth knowing before spending an afternoon on it.

**Apple Silicon only, for the GPU.** Ollama accelerates through Metal on macOS
and enables it on Apple Silicon; AMD support goes through ROCm, which is Linux.
On an Intel Mac it runs on CPU whatever card is in the machine — verified on an
i9-10900 with a Radeon RX 580, where `discovering available GPUs` finds
`library=cpu` and nothing else. It works. It is not fast.

**The catalogue is the bottleneck, not the model.** Generating tokens on a CPU
is slow; *processing* 19,000 tokens of tool schemas before generating anything
is much slower. This is the same number that dominates the bill on a hosted
provider, and it is why tool selection is not only an economy — it is what makes
a local model usable at all.

`ollama-cloud` is the same protocol against Ollama's servers, so it is one key
and a different `--provider`. Worth having when the machine cannot do the work.

---

## What it costs

Tokens are recorded; money is derived.

```bash
sentineldesk-agent costs
```

A price changes, and a price stored beside each row would then be wrong for
every past run with no way to notice. Recomputing from a rate you control keeps
the history correct when the rate moves — correcting `pricing.json` re-prices
everything, not only what comes next.

```jsonc
// ~/.sentineldesk/pricing.json — dollars per million tokens
{
  "sonnet": { "input": 2.00, "output": 10.00 },
  "opus":   { "input": 15.00, "output": 75.00 },
  "haiku":  { "input": 1.00, "output": 5.00 }
}
```

The key is matched as a substring of the model id, longest match winning, so a
dated release inherits its family's rate instead of falling to zero. Cache
multipliers (`cache_write_multiplier`, `cache_read_multiplier`) default to 1.25
and 0.10 and are overridable in the same file.

**The built-in rates are estimates.** They are there so `costs` prints something
useful on the first day, not because this project is an authority on anybody's
billing. Calibrate against the console: take the token counts `costs` reports,
take what the invoice says, and adjust. A model with no rate prints `?` rather
than zero — reporting a day's work as free is the worst available way to be
wrong about a bill.

---

## Adding one

A provider that speaks chat-completions is a row in `Presets`
(`agent/internal/provider/openaicompat.go`): id, base URL, key name, default
model, what it does about caching, and one sentence for `providers` about the
thing somebody has to know.

One that does not gets its own file implementing `Provider` — three methods,
and `KeySource()` if it holds a credential. Look at `anthropic.go`; the mapping
between this package's types and the wire format is the whole job, which is also
why an SDK is not used: a dependency that changes its own types underneath the
loop is a dependency that owns the loop.
