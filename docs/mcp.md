# MCP — driving the WebRTC desktop from an AI

The backend exposes an **MCP server** so a model (Claude Code, Claude Desktop, an
agent) can use the desktop the way a person does: see the screen, move the mouse,
type, open programs, manage windows, run commands.

The daemon opens a **local Unix socket** and the `-mcp-stdio` sub-command is a
thin stdio↔socket bridge that the AI host spawns.

```
AI host (Claude Code)
  └─ spawns:  sentineldesk -mcp-stdio -mcp-sock <socket>   (JSON-RPC bridge over stdin/stdout)
                    │  local Unix socket, 0600 (user sentineldesk)
                    ▼
             sentineldesk (daemon)  ── MCP_SOCK=/run/user/1000/sentineldesk-mcp.sock
```

The daemon opens the socket by itself because supervisord passes it
`MCP_SOCK=/run/user/1000/sentineldesk-mcp.sock`. No extra flag is needed.

## Connecting Claude Code (or Claude Desktop)

The bridge runs **inside the container**, so the AI host launches it with
`docker exec`. Add this to your MCP configuration:

```json
{
  "mcpServers": {
    "sentineldesk": {
      "command": "docker",
      "args": [
        "exec", "-i", "-u", "sentineldesk", "sentineldesk",
        "/usr/local/bin/sentineldesk", "-mcp-stdio",
        "-mcp-sock", "/run/user/1000/sentineldesk-mcp.sock"
      ]
    }
  }
}
```

- `-i` keeps stdin open (that is the MCP transport).
- `-u sentineldesk` runs as the socket's owner (uid 1000).
- The container has to be named `sentineldesk` (or adjust the name).

## Available tools (106)

**👁️ Seeing the screen**

| Tool | What it does |
|---|---|
| `screenshot` | Capture the screen as PNG |
| `screenshot_region` | Capture one rectangle only (cheaper) |
| `get_screen_info` | Resolution, display, number of desktops |
| `get_pixel_color` | RGB of one pixel — assert state without an image |
| `read_screen_text` | **OCR**: the text on screen, or in a region |
| `find_text` | **OCR**: screen coordinates of a string, ready to click |

**🖱️ Mouse and ⌨️ keyboard**

| Tool | What it does |
|---|---|
| `mouse_move`, `mouse_click` | Move and click (button, double click) |
| `mouse_down`, `mouse_up` | Press / release a button |
| `mouse_drag` | Drag in steps, for applications that ignore jumps |
| `mouse_scroll` | Vertical and horizontal wheel |
| `get_mouse_position` | Current pointer position |
| `type_text` | Type text, accented characters included |
| `key_combo` | A key or a combination (`ctrl+c`, `alt+Tab`, `super+d`) |

**🪟 Windows and desktops**

| Tool | What it does |
|---|---|
| `list_windows` | id, desktop, geometry, class and title |
| `get_active_window` | The focused window |
| `activate_window` | Focus and raise |
| `move_window`, `resize_window` | Move / resize |
| `maximize_window`, `restore_window`, `minimize_window`, `fullscreen_window` | States |
| `close_window` | Close |
| `set_window_desktop` | Move to another desktop |
| `wait_for_window` | **Wait** for a window to appear, instead of guessing a `wait` |
| `list_desktops`, `switch_desktop` | Workspaces |

**🚀 Applications, processes and shell**

| Tool | What it does |
|---|---|
| `launch_app` | Run a program (detached) — `as_root` for administration GUIs |
| `list_installed_apps` | Applications with a `.desktop` entry |
| `list_processes` | pid, cpu%, mem%, command, with an optional filter |
| `kill_process`, `is_running` | Kill / check |
| `run_command` | Shell command with stdout/stderr/exit and a timeout — `as_root` for privileges |
| `read_file`, `write_file`, `list_directory` | Files — `as_root` for `/etc`, `/root`, and so on |

**🔑 Administration (root)** — a real desktop can be administered

| Tool | What it does |
|---|---|
| `sudo_status` | Which escalation paths exist: passwordless sudo, `su`, pkexec, groups |
| `install_packages` | `apt install` whatever is missing; reports the version that landed |
| `remove_packages`, `search_packages` | Uninstall / search the Debian archive |
| `service_control` | supervisor: status, start, stop, restart of X, audio, WM, AT-SPI, sentineldesk |

On top of that, `as_root: true` on `run_command`, `launch_app`, `read_file`,
`write_file` and `list_directory`, and `user: "root"` on `shell_open` for a
persistent root terminal.

**📋 Clipboard · 🔊 audio · ⏱️ state**

| Tool | What it does |
|---|---|
| `get_clipboard`, `set_clipboard` | The desktop's clipboard |
| `get_audio_state`, `set_volume` | Sink, volume and mute |
| `wait` | Sleep N ms |
| `get_desktop_info` | WM, resolution, uptime, memory, encoder, joystick, recording |

**🖥️ Persistent terminal** — `run_command` is one-shot; this is a real shell

| Tool | What it does |
|---|---|
| `shell_open` | Opens a shell on a real PTY (keeps cwd, variables, history); `user:"root"` for a root terminal |
| `shell_exec` | Runs a command; **state persists** between calls |
| `shell_input` | Sends keys without Enter: answering prompts, passwords, Ctrl+C |
| `shell_read` | Reads the accumulated output, to follow a long command |
| `shell_list`, `shell_close` | Session management |

This is for interactive programs `run_command` cannot handle: `sudo`, `vim`,
`top`, installers that ask yes/no.

**🔐 SSH** — connections, transfers and tunnels

| Tool | What it does |
|---|---|
| `ssh_connect` | Connects with **a password or a private key** (optional passphrase) |
| `ssh_exec` | Remote command with stdout, stderr and exit code |
| `ssh_upload`, `ssh_download`, `ssh_list_remote` | Transfers over SFTP |
| `ssh_tunnel_local` | Forward tunnel (`-L`): reach a service only the remote can see |
| `ssh_tunnel_remote` | **Reverse tunnel** (`-R`): publish this desktop from behind NAT |
| `ssh_tunnels`, `ssh_tunnel_close` | Inspect and close tunnels, with connections served |
| `ssh_keygen`, `ssh_copy_id` | Generate a key and install it on the server |
| `ssh_list`, `ssh_disconnect` | Session management |

Tunnels live inside the process (Go's native SSH library), so they can be listed
and closed — no stray `ssh` processes are left behind.

**🪟 Low-level windows (EWMH/X11)**

| Tool | What it does |
|---|---|
| `window_properties` | Every EWMH property: type, `_NET_WM_STATE`, pid, class, allowed actions |
| `window_set_state` | above, below, sticky, shaded, fullscreen, skip_taskbar, modal… |
| `window_hierarchy` | The raw X11 tree (parents, children, override-redirect) |

**⚡ Fewer round trips**

| Tool | What it does |
|---|---|
| `set_resolution` | Changes the resolution **without restarting anything** (it can only shrink below the size reserved at start) |
| `wait_for_idle` | Waits for the screen to stop changing **and** the CPU to settle, instead of a guessed `wait` |
| `open_app_and_wait` | Launch + wait for the window + focus + wait for the paint, in **one** call |
| `fill_form` | Fills several fields by accessibility name and optionally presses a button |
| `ui_diff` | Returns **only what changed** in the tree since the last call — a fraction of the size of `ui_tree` |

**📼 Auditing and restore points**

| Tool | What it does |
|---|---|
| `action_log` | A record of every call: time, arguments, result, duration. While recording it also carries the **minute within the video** |
| `snapshot_create` | Restore point: the home plus the list of installed packages |
| `snapshot_list`, `snapshot_delete` | Management |
| `snapshot_restore` | Returns the home to the saved state and reports which packages were installed afterwards |

**🎮 Gamepad · 🎥 recording and streaming**

| Tool | What it does |
|---|---|
| `gamepad_button`, `gamepad_tap` | Buttons (W3C Gamepad API indices) |
| `gamepad_axis`, `gamepad_state` | Sticks / full state |
| `start_recording`, `stop_recording` | Record to **mp4 / webm / mkv**, closed cleanly |
| `get_recording_status`, `list_recordings` | Status and files |
| `start_restream`, `stop_restream`, `list_restreams` | Also send the desktop to **RTMP** (YouTube/Twitch/Facebook), **SRT** or **UDP** (VLC/OBS), reusing the live encode |

Full checklist and design notes: [mcp-tools-checklist.md](mcp-tools-checklist.md).

## Trying it without Claude Code

You can speak the protocol by hand over the socket:

```bash
printf '%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
| docker exec -i -u sentineldesk sentineldesk \
    /usr/local/bin/sentineldesk -mcp-stdio -mcp-sock /run/user/1000/sentineldesk-mcp.sock
```

## Sharing the session with the agent

Now that capture lives in a **room**, the desktop takes several participants at
once and **one of them drives**. That changes how it is worth working with the
MCP: the agent and the person can be on the same desktop, seeing the same thing,
and hand control back and forth without restarting anything.

Mind one deliberate asymmetry: **MCP tools do not go through the room's control
arbitration**. They inject straight into X, because the MCP arrives over the
daemon's local socket rather than over the web. Room control arbitrates between
*browsers*. If you want the agent to touch nothing while you watch, the tool for
that is the policy (`-mcp-policy readonly`), not room control.

## Security

- The socket is local and `0600` (the `sentineldesk` user only).
- The browser's file manager (`/files/*`) requires the same session token the
  WebSocket login issues, and is confined to `FILES_ROOT` (`/home/sentineldesk`
  by default; `FILES_ROOT=/` opens the whole container). Symlinks are resolved
  **before** comparing against the root, so a link pointing outside is no way
  out. Downloads use a one-use ticket with a 60-second life instead of carrying
  the token in the URL.
- `run_command` grants full control of the container: it is reachable only over
  the daemon's local socket, never over the web.

### The permission model

With root available the MCP has complete power over the container. That is right
when a person is driving it and wrong when an unsupervised agent is, so there are
three levels and two lists. The **daemon sets the ceiling** through the
environment:

```
MCP_POLICY=full       (default) everything allowed
MCP_POLICY=safe       everything except running code or touching the system; as_root is out
MCP_POLICY=readonly   observation only: see the screen, read the tree, list things

MCP_DENY=run_command,ssh_*    additionally deny these (a * suffix matches by prefix)
MCP_ALLOW=screenshot,ui_*     when set, ONLY these
```

And **each connection can restrict itself further**, never widen:

```bash
sentineldesk -mcp-stdio -mcp-sock … -mcp-policy readonly
```

This is how you hand an agent a read-only endpoint against the same daemon you
use with full permissions. `tools/list` returns only what is allowed: offering a
forbidden tool is inviting the model to walk into a wall.

Denied attempts land in `action_log` with the reason.

### About root inside the desktop

The `sentineldesk` user has **passwordless sudo** and a working `su`. That is
deliberate: the container *is* the sandbox, and the security boundary is the
WebSocket login (`AUTH_USER`/`AUTH_PASS` plus per-IP rate limiting), not
something inside the desktop. Anyone who already holds a graphical session in a
container can read everything that matters to that container; denying them
`apt install` protects nothing while blocking what a real desktop is for.

What still holds:

- The container **is not root on the host**: the real limits are Docker's
  (capabilities, seccomp, mounts, network). That is where to tighten things.
- **Do not mount sensitive host sockets or directories** (`/var/run/docker.sock`,
  `/`, the host's SSH keys) unless you trust whoever will use the desktop.
- The root password comes from `ROOT_PASSWORD`; without it `AUTH_PASS` is reused.
  On an instance published to the internet, set both.

For a hardened deployment, run the container with `--read-only`, `--cap-drop
ALL` and a user without `sudo`: the administration tools then return a clear
error ("this image has no passwordless sudo") instead of failing opaquely.
