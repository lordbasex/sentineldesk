# ADR-004 — The runtime is a supervised process, and the desktop outlives it

**Status:** accepted, 6 August 2026.
**Decides:** §7.4 of [agent/stage-1-mcp-readiness.md](../../agent/stage-1-mcp-readiness.md).

## Context

The desktop is the product. The agent is something the desktop can offer, and it
brings with it every way a piece of software can fail that the desktop currently
cannot: a model provider that stops answering, an API key that expires, a loop
that will not terminate, a database that will not open.

None of those is a reason for somebody's screen to go away.

The MCP plane already works this way and it is worth naming why, because the
same reasoning applies. `sentineldesk -mcp-stdio` is a separate process on
purpose: killing the AI host never takes the desktop down. The runtime is the
same shape of thing, one layer up.

## Decision

`sentineldesk-agent` runs as its own supervised process, alongside the daemon
rather than inside it.

- **Under supervisord in the container**, as its own program with its own log,
  `autorestart=true`, and no `DISPLAY` unless something documented needs one.
  Its restarts do not restart Xvfb, the Room or the daemon.
- **Under systemd on a native install**, as a separate unit, so the same
  property holds where there is no supervisord.
- **It is optional.** `AGENT_ENABLED` off, a missing provider key, or a runtime
  that will not start, all mean the Agent Console is unavailable. None of them
  stops the desktop from booting or a person from working.
- **It connects; the daemon listens.** The daemon owns the socket and the
  authority to cut the connection — which is what
  `Server.HaltConnection` already provides — and the runtime reconnects as a
  client. The party that must survive is the party that holds the door.

The invariant, in one line: **restarting `sentineldesk-agent` does not disturb
WebRTC, the Room, or anyone's session.**

## Consequences

- A provider outage, a wedged loop or a corrupt database costs the Agent
  Console and nothing else.
- Both deployment paths change together. Fixing only Docker and leaving the
  native installer behind is the failure this repository has already had once,
  and the compose files, the embedded deployment tree and `install.sh` are all
  part of "done".
- Two processes need two log streams. `make logs` shows the desktop; the
  runtime's own log has to be findable without knowing where to look.
- In-process would have been less to wire. It would also have made every agent
  failure a desktop failure, which is the one trade this project does not make:
  optional capabilities degrade instead of taking everything with them, and that
  rule has already caught real bugs — `callRoom` claiming every tool when no
  room was attached was exactly this mistake at a smaller scale.
- The first vertical slice may run the runtime in-process for speed of
  development. That is allowed only if the protocol between browser and runtime
  is unchanged by moving it out, because otherwise the shortcut becomes the
  design.
