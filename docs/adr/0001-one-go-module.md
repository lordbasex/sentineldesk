# ADR-001 — One Go module, with `agent/` as a subtree

**Status:** accepted, 6 August 2026.
**Decides:** §7.1 of [agent/stage-1-mcp-readiness.md](../../agent/stage-1-mcp-readiness.md).

## Context

`sentineldesk-agent` is a second binary and a separate deliverable: it has its
own lifecycle, its own release cadence, and the desktop has to keep working when
it is not running. "Two projects" is a true description of how they are operated.

It does not follow that they need two Go modules, and the choice is not
symmetric. Go's `internal` rule is scoped to the module root: a package under
`internal/` can be imported by anything in the same module and by nothing
outside it. So `agent/` inside this module can import
`github.com/lordbasex/sentineldesk/internal/...`; a separate module cannot, and
would need whatever the two share lifted into a published, versioned package.

That matters because there is something to share and it is not incidental. The
tool catalogue's risk levels, categories and search ranking are the contract
between the two, and the whole point of stage 1 was to stop that contract living
in two places that drift apart.

## Decision

One module. `agent/` is a subtree of this repository, with the binary under
`agent/cmd/sentineldesk-agent/`.

Shared code goes in `internal/` as usual. When something is genuinely shared —
the search ranking is the first candidate — it moves to a package of its own at
that point, not before.

## Consequences

- The agent can import the catalogue's metadata directly, so there is one
  definition of what a tool's risk means rather than a copy on each side.
- `go build ./...` builds both. `make test` covers both.
- Splitting later costs a directory move and a `go.mod`. Starting split and
  merging later would cost undoing a published API, which is worse — and the
  asymmetry is the whole reason for choosing this way round.
- The repository is no longer "a single Go binary". `README.md` and `CLAUDE.md`
  say so in their opening lines and will need updating when the second binary
  exists, not before.
- Vendoring and dependency updates are shared. A dependency the agent needs is a
  dependency the desktop's build sees, which is an argument for keeping the
  agent's dependency list short — see [ADR-002](0002-agent-without-cgo.md).
