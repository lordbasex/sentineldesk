---
name: deploy-desktop
description: Rebuild the container image and restart the desktop, the way this repository expects it.
---

## Instructions

- `make up` builds both image variants and restarts the compose stack. It does
  not run tests; run `make test` first if you changed Go.
- The desktop takes about 15 seconds to come back. Wait for the MCP socket at
  `/run/user/1000/sentineldesk-mcp.sock` rather than sleeping a fixed amount.
- A viewer that was connected is dropped by the restart. Say so before doing it
  if anybody is in the room — `desktop_state` reports who.
