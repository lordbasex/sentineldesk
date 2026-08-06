# Tool sweep — every tool called against a real desktop

Run on 06 August 2026, 13:24 against `sentineldesk v1.1.7 (ca70992) · build 20260806-131740`.

This is the human-readable half of stage 1's verification. [`tools/stage1-check.py`](../tools/stage1-check.py) proves the mechanisms — annotations, denial kinds, the room gate, cancellation, progress. This proves the tools, by calling every one of them through the same stdio bridge an AI host uses and writing down what was sent.

Regenerate it with:

```bash
make up
python3 tools/tool-sweep.py
```

| | Meaning |
|---|---|
| ✅ | The tool ran and answered |
| ⚠️ | It answered with a failure the environment explains — no gamepad device, no OCR language pack, nothing listening |
| ⏭️ | Not run, with the reason given |
| ❌ | It failed for a reason the environment does not explain |

**111 ran · 3 degraded · 1 skipped · 0 failed — 115 tools in 122 calls.**

A tool called more than once counts once, at its worst result.


## Seeing the desktop

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `get_desktop_info` | Resolution, session and what the desktop is running | `—` | { "display": ":0", "encoder": "auto", "joystick": true, "memory_used": "1309/7833 MB", "recording": false, "resolution": |
| ✅ | `get_screen_info` | Screen geometry and how many virtual desktops there are | `—` | { "desktops": 4, "display": ":0", "height": 1080, "width": 1920 } |
| ✅ | `screenshot` | Capture the whole screen as a PNG | `—` | [image saved: /tmp/mcp-screenshot-1786033422692.png] |
| ✅ | `screenshot_region` | Capture one rectangle instead of the whole screen | `{"x": 0, "y": 0, "width": 320, "height": 200}` | [image saved: /tmp/mcp-screenshot_region-1786033422742.png] |
| ✅ | `get_pixel_color` | The colour of one pixel, to assert state without an image | `{"x": 10, "y": 10}` | { "b": 51, "g": 33, "hex": "#1b2133", "r": 27 } |
| ✅ | `read_screen_text` | OCR the screen into text | `{"lang": "eng"}` | Pry mf Ms NN e. lameocees CM + Ne ve SSH SN MAASASBAQLS A SEY Tg =x < 4 “= os : by P 3 SES . dlovwapy ei Awl ys \ : Mall |
| ✅ | `find_text` | OCR, but return where a string is so it can be clicked | `{"text": "Files"}` | no match for "files" on screen |
| ✅ | `get_mouse_position` | Where the pointer is now | `—` | { "x": 10, "y": 10 } |
| ⚠️ | `get_active_window` | Which window has focus | `—` | no active window: exit status 1 |
| ✅ | `list_windows` | Every open window with its id and geometry | `—` | [ { "id": "0x00c00006", "desktop": -1, "x": 0, "y": 0, "w": 1920, "h": 36, "class": "panel.lxpanel", "title": "panel" }, |
| ✅ | `list_desktops` | The virtual desktops and which one is current | `—` | [ { "current": true, "name": "1920x1044 desktop 1", "number": 0 }, { "current": false, "name": "1920x1044 desktop 2", "n |
| ✅ | `list_processes` | Running processes | `{"filter": "python"}` | [ { "command": "/usr/bin/python3 /usr/bin/supervisord -c /etc/supervisor/supervisord.conf", "cpu": "0.4", "mem": "0.3",  |
| ✅ | `is_running` | Whether a named process is alive | `{"name": "Xvfb"}` | { "pids": [ 38 ], "running": true } |
| ✅ | `list_installed_apps` | Applications with a desktop entry | `—` | [ { "exec": "/usr/bin/chromium %U", "name": "Chromium Web Browser" }, { "exec": "uxterm", "name": "UXTerm" }, { "exec":  |
| ✅ | `get_audio_state` | Sink, volume and whether it is muted | `—` | { "mute": false, "sink": "sentineldesk", "volume": "Volume: front-left: 65536 / 100% / 0.00 dB, front-right: 65536 / 100 |
| ✅ | `check_errors` | Any error dialog or alert currently on screen | `—` | { "errors_on_screen": false, "note": "nothing is reporting a failure. This only sees graphical dialogs — a command that  |
| ✅ | `wait` | Sleep, to let the interface settle | `{"ms": 50}` | waited 50 ms |
| ✅ | `wait_for_idle` | Wait until the screen stops changing and the CPU settles | `{"timeout_ms": 4000, "quiet_ms": 400, "ignore_cpu": true}` | { "cpu_percent": 97, "idle": true, "reason": "the screen went still and the CPU settled", "waited_ms": 1101 } |

## The catalogue, and the room

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `tool_search` | Find tools from a plain-words description of a task | `{"query": "give someone remote access over ssh", "limit": 5}` | { "matched": 5, "of": 115, "tools": [ { "name": "ssh_list_remote", "category": "ssh", "risk": "read", "description": "Li |
| ✅ | `action_log` | The audit trail: every call with its connection and result | `{"limit": 3}` | { "count": 3, "entries": [ { "time": "2026-08-06T13:23:46.548-03:00", "tool": "wait", "args": "{\"ms\":50}", "ok": true, |
| ✅ | `room_state` | Who is in the room, who is driving, may this connection act | `—` | { "controller": "", "controller_id": "", "humans_present": true, "may_inject": false, "note": "Control is always claimed |

## Taking the controls

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `request_control` | Ask the room for the desktop before touching it | `{"timeout_ms": 8000}` | { "granted": true, "reason": "nobody was driving" } |

## Pointer and keyboard

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `mouse_move` | Move the pointer to an absolute position | `{"x": 400, "y": 300}` | moved to (400, 300) |
| ✅ | `mouse_click` | Click, optionally moving there first | `{"x": 400, "y": 300, "button": 1}` | clicked button 1 x1 |
| ✅ | `mouse_down` | Press a button and hold it | `{"button": 1}` | mouse_down button 1 |
| ✅ | `mouse_up` | Release the held button | `{"button": 1}` | mouse_up button 1 |
| ✅ | `mouse_drag` | Drag from one point to another | `{"x1": 300, "y1": 300, "x2": 360, "y2": 340, "button": 1}` | dragged (300,300) -> (360,340) |
| ✅ | `mouse_scroll` | Scroll the wheel | `{"dy": -2}` | scrolled dy=-2 dx=0 |
| ✅ | `type_text` | Type a string into whatever has focus | `{"text": "sweep"}` | typed 5 chars |
| ✅ | `key_combo` | Press a key or combination by X keysym name | `{"keys": "Escape"}` | pressed Escape |

## Clipboard

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `set_clipboard` | Put text on the X clipboard | `{"text": "sentineldesk-sweep"}` | clipboard set |
| ✅ | `get_clipboard` | Read the X clipboard back | `—` | sentineldesk-sweep |

## Windows and desktops

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `launch_app` | Start a program on the desktop, detached | `{"command": "xterm -T SWEEPWIN -e sleep 600"}` | { "as_root": false, "command": "xterm -T SWEEPWIN -e sleep 600", "note": "still running after 700 ms. A window may take  |
| ✅ | `wait_for_window` | Wait until a window matching a title appears | `{"match": "SWEEPWIN", "timeout_ms": 15000}` | { "class": "xterm.XTerm", "found": true, "id": "0x0160000c", "title": "SWEEPWIN" } |
| ✅ | `list_windows` | List windows, to find the one just opened | `—` | [ { "id": "0x00c00006", "desktop": -1, "x": 0, "y": 0, "w": 1920, "h": 36, "class": "panel.lxpanel", "title": "panel" }, |
| ✅ | `activate_window` | Focus and raise a window by id | `{"id": "0x0160000c"}` | activated window 0x0160000c |
| ✅ | `window_properties` | The raw X properties of one window | `{"id": "0x0160000c"}` | { "WM_CLASS": [ "xterm", "XTerm" ], "WM_CLIENT_MACHINE": "7d4ffbbf6685", "WM_COMMAND": [ "{ \"/usr/bin/xterm", "-T", "SW |
| ✅ | `window_hierarchy` | The raw X window tree, parents and children | `—` | xwininfo: Window id: 0x21f (the root window) (has no name) Root window id: 0x21f (the root window) (has no name) Parent  |
| ✅ | `move_window` | Move a window to a position | `{"id": "0x0160000c", "x": 120, "y": 120}` | moved 0x0160000c (0,120,120,-1,-1) |
| ✅ | `resize_window` | Resize a window | `{"id": "0x0160000c", "width": 640, "height": 400}` | resized 0x0160000c (0,-1,-1,640,400) |
| ✅ | `minimize_window` | Minimise a window | `{"id": "0x0160000c"}` | minimized |
| ✅ | `restore_window` | Restore a minimised window | `{"id": "0x0160000c"}` | restored |
| ✅ | `maximize_window` | Maximise a window | `{"id": "0x0160000c"}` | maximized |
| ✅ | `fullscreen_window` | Put a window full screen | `{"id": "0x0160000c"}` | toggled fullscreen |
| ✅ | `window_set_state` | Change an EWMH state such as 'above' | `{"id": "0x0160000c", "state": "above", "action": "add"}` | add above en 0x0160000c |
| ✅ | `set_window_desktop` | Send a window to a virtual desktop | `{"id": "0x0160000c", "desktop": 0}` | moved to desktop |
| ✅ | `switch_desktop` | Switch to another virtual desktop | `{"desktop": 0}` | switched desktop |
| ✅ | `close_window` | Close the window the sweep opened | `{"id": "0x0160000c"}` | closed window |

## Terminal

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `terminal_open` | Open a terminal window a person can watch | `—` | { "exit_codes": true, "note": "exit codes are reported. Use `sudo -E su` rather than `sudo su` to keep them across a roo |
| ✅ | `terminal_run` | Type a command into that terminal and wait for the prompt | `{"command": "echo sweep-terminal-ok", "timeout_ms": 30000}` | { "command": "echo sweep-terminal-ok", "exit_code": 0, "finished": true, "output": "echo sweep-terminal-ok\nsweep-termin |
| ✅ | `terminal_read` | Read back what the terminal shows | `{"lines": 10}` | { "last_command": "echo sweep-terminal-ok", "last_exit_code": 0, "last_succeeded": true, "text": "sentineldesk@7d4ffbbf6 |

## Files

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `write_file` | Write a file, optionally as root | `{"path": "/tmp/sweep.txt", "content": "sentineldesk sweep\n"}` | wrote 19 bytes to /tmp/sweep.txt |
| ✅ | `read_file` | Read a file back | `{"path": "/tmp/sweep.txt"}` | sentineldesk sweep |
| ✅ | `list_directory` | List a directory | `{"path": "/tmp"}` | [ { "modified": "2026-08-06T13:23:05-03:00", "name": ".X0-lock", "size": 11, "type": "file" }, { "modified": "2026-08-06 |

## Browser, over DevTools

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `browser_open` | Start Chromium with DevTools and wait for it to answer | `{"url": "file:///tmp/sweep.html"}` | browser open with CDP (1 tabs) |
| ✅ | `browser_wait_for` | Wait for a CSS selector to exist | `{"selector": "#t", "timeout_ms": 15000}` | #t appeared |
| ✅ | `browser_tabs` | List the open tabs | `—` | [ { "id": "D53D5CF552EF778DDBEB4C3929C4972F", "title": "sweep.html", "url": "file:///tmp/sweep.html" } ] |
| ✅ | `browser_goto` | Navigate the current tab | `{"url": "file:///tmp/sweep.html"}` | navigating to file:///tmp/sweep.html |
| ✅ | `browser_text` | Read the page's visible text | `{"max_chars": 200}` | Sweep Go |
| ✅ | `browser_type` | Type into a field by selector | `{"selector": "#i", "text": "sweep"}` | typed into #i |
| ✅ | `browser_click` | Click an element by selector | `{"selector": "#b"}` | clicked #b |
| ✅ | `browser_eval` | Evaluate JavaScript against the real DOM | `{"expression": "document.getElementById('t').textContent"}` | clicked |

## The accessibility tree

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `ui_tree` | Read the desktop as structure rather than pixels | `{"interactive": true, "limit": 40}` | { "count": 283, "elements": [ { "name": "lxpanel", "ref": "0", "role": "application" }, { "center_x": 960, "center_y": 1 |
| ✅ | `ui_find` | Find elements by role, name or text | `{"limit": 20}` | { "count": 20, "elements": [ { "name": "lxpanel", "ref": "0", "role": "application" }, { "center_x": 960, "center_y": 18 |
| ✅ | `ui_find` | Find the editable fields, to write into one by ref | `{"role": "entry", "limit": 5}` | { "count": 2, "elements": [ { "actions": [ "activate", "showContextMenu" ], "center_x": 511, "center_y": 109, "height":  |
| ✅ | `ui_wait_for` | Wait until a matching element exists | `{"role": "frame", "timeout_ms": 6000}` | { "elements": [ { "center_x": 960, "center_y": 18, "height": 36, "name": "panel", "ref": "0/0", "role": "frame", "state" |
| ✅ | `ui_diff` | Only what changed in the tree since the last call | `{"reset": true}` | { "baseline": true, "nodes": 317, "note": "first call: the reference snapshot was stored; the next one returns only the  |
| ✅ | `ui_get_text` | Read an element's text by ref, without OCR | `{"ref": "0"}` | { "name": "lxpanel", "ref": "0", "role": "application", "text": "" } |
| ✅ | `ui_focus` | Give an editable field keyboard focus | `{"ref": "3/0/0/0/5/1/0/5/2"}` | { "ok": true, "ref": "3/0/0/0/5/1/0/5/2" } |
| ⚠️ | `ui_set_text` | Write text straight into a field by ref, no typing | `{"ref": "3/0/0/0/5/1/0/5/2", "text": "sentineldesk-sweep"}` | Chromium exposes the entry as editable but implements no AT-SPI EditableText on it — use browser_type inside a page |
| ✅ | `ui_click` | Invoke an element's action directly, no pointer involved | `{"ref": "3/0/0/0/5/1/0/5/2"}` | { "action": "activate", "ok": true, "ref": "3/0/0/0/5/1/0/5/2" } |

## Audio

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `set_volume` | Set the volume, or mute | `{"percent": 60}` | volume 60% |
| ✅ | `get_audio_state` | Read the volume and mute state back | `—` | { "mute": false, "sink": "sentineldesk", "volume": "Volume: front-left: 39321 / 60% / -13.31 dB, front-right: 39321 / 60 |

## Recording and re-streaming

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `start_recording` | Record the screen to a file alongside the live stream | `{"container": "mp4", "fps": 15, "audio": false}` | recording to /home/sentineldesk/Recordings/rec-20260806-132357.mp4 |
| ✅ | `get_recording_status` | Whether a recording is running, and how big it is | `—` | { "container": "mp4", "path": "/home/sentineldesk/Recordings/rec-20260806-132357.mp4", "recording": true, "seconds": 0,  |
| ✅ | `wait` | Let the recording collect a second of video | `{"ms": 1200}` | waited 1200 ms |
| ✅ | `stop_recording` | Stop and finalise the file | `—` | { "path": "/home/sentineldesk/Recordings/rec-20260806-132357.mp4", "size_bytes": 577324 } |
| ✅ | `list_recordings` | The recordings on disk | `—` | [ { "modified": "2026-08-06T13:10:55-03:00", "path": "/home/sentineldesk/Recordings/rec-20260806-131054.mp4", "size_byte |
| ✅ | `start_restream` | Send the live encode to an external destination | `{"url": "udp://127.0.0.1:9999", "platform": "udp"}` | streaming to udp (udp://•••, audio=true) — reusing the live encode, no second capture |
| ✅ | `list_restreams` | Where the desktop is currently being published | `—` | udp → udp://127.0.0.1:9999 (audio=true, 0s) |
| ✅ | `stop_restream` | Stop publishing | `—` | stopped 1 destination(s) |

## Gamepad

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `gamepad_button` | Hold or release a button by W3C index | `{"index": 0, "down": true}` | button 0 down |
| ✅ | `gamepad_tap` | Press and release a button | `{"index": 0, "hold_ms": 40}` | tapped button 0 |
| ✅ | `gamepad_axis` | Move one stick axis | `{"axis": 0, "value": 0.5}` | axis 0 = 0.50 |
| ✅ | `gamepad_state` | Set every button and axis in one call | `{"buttons": [0, 0, 0, 0], "axes": [0, 0, 0, 0]}` | gamepad state applied |

## Persistent shells

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `shell_open` | Start a shell session that survives between calls | `{"shell": "/bin/bash"}` | { "id": "sh1", "shell": "/bin/bash", "user": "" } |
| ✅ | `shell_exec` | Run a command in that session and wait for it | `{"id": "sh1", "command": "echo sweep-shell-ok", "timeout_ms": 10000}` | { "completed": true, "output": "sweep-shell-ok" } |
| ✅ | `shell_input` | Send raw keystrokes, without waiting | `{"id": "sh1", "text": "echo second", "enter": true}` | sent 12 bytes |
| ✅ | `wait` | Give the shell a moment to produce output | `{"ms": 400}` | waited 400 ms |
| ✅ | `shell_read` | Read and clear what the session has produced | `{"id": "sh1"}` | echo second second sentineldesk@7d4ffbbf6685:/$ |
| ✅ | `shell_list` | The open shell sessions | `—` | [ { "alive": true, "id": "sh1", "pending": 0, "seconds": 1, "user": "" } ] |
| ✅ | `shell_close` | End the session | `{"id": "sh1"}` | session closed |

## Packages and services

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `run_command` | Run a shell command and return stdout, stderr and exit code | `{"command": "echo sweep-run-ok && uname -s", "timeout_ms": 10000}` | { "as_root": false, "exit_code": 0, "stderr": "", "stdout": "sweep-run-ok\nLinux\n", "timed_out": false } |
| ✅ | `sudo_status` | Whether passwordless sudo is available in this image | `—` | { "groups": [ "sentineldesk", "sudo", "video" ], "hint": "as_root:true en run_command / launch_app / write_file / read_f |
| ✅ | `search_packages` | Search apt without installing anything | `{"query": "openssh-server", "limit": 3}` | { "count": 2, "note": "the apt index was empty, so apt-get update was run", "query": "openssh-server", "results": [ { "d |
| ✅ | `service_control` | Ask supervisord about the desktop's services | `{"action": "status"}` | { "action": "status", "as_root": true, "exit_code": 3, "service": "all", "stderr": "", "stdout": "at-spi RUNNING pid 41, |
| ✅ | `install_packages` | Install a package with apt, reporting progress | `{"packages": ["openssh-server"], "update": true, "timeout_ms": 300000}` | { "as_root": true, "exit_code": 0, "installed": { "openssh-server": "1:10.0p1-7+deb13u4" }, "log": "…\nSetting up openss |

## SSH

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `ssh_keygen` | Generate a key pair for the desktop's user | `{"path": "/home/sentineldesk/.ssh/sweep", "type": "ed25519", "comment": "sweep"}` | { "note": "the key already existed and was not overwritten", "path": "/home/sentineldesk/.ssh/sweep", "public_key": "ssh |
| ✅ | `ssh_list` | The open SSH sessions | `—` | null |
| ✅ | `ssh_connect` | Open an SSH session to a host | `{"host": "127.0.0.1", "user": "sentineldesk", "key_path": "/home/sentineldesk/.ssh/sweep"}` | { "host": "127.0.0.1:22", "id": "ssh1", "user": "sentineldesk" } |
| ✅ | `ssh_exec` | Run a command on the remote host | `{"id": "ssh1", "command": "echo sweep-ssh-ok", "timeout_sec": 20}` | { "exit_code": 0, "stderr": "", "stdout": "sweep-ssh-ok\n" } |
| ✅ | `ssh_upload` | Send a file over SFTP | `{"id": "ssh1", "local": "/tmp/sweep.txt", "remote": "/tmp/sweep-up.txt"}` | uploaded 19 bytes to /tmp/sweep-up.txt |
| ✅ | `ssh_download` | Fetch a file over SFTP | `{"id": "ssh1", "remote": "/tmp/sweep-up.txt", "local": "/tmp/sweep-down.txt"}` | downloaded 19 bytes to /tmp/sweep-down.txt |
| ✅ | `ssh_list_remote` | List a directory on the remote host | `{"id": "ssh1", "path": "/tmp"}` | [ { "modified": "2026-08-06T13:24:08-03:00", "name": "sweep-down.txt", "size": 19, "type": "file" }, { "modified": "2026 |
| ✅ | `ssh_tunnel_local` | Forward a local port to the remote side | `{"id": "ssh1", "local_addr": "127.0.0.1:18080", "remote_addr": "127.0.0.1:8080"}` | { "spec": "127.0.0.1:18080 → 127.0.0.1:8080 (via 127.0.0.1:22)", "tunnel_id": "ssh1-l1" } |
| ✅ | `ssh_tunnels` | The tunnels open on this session | `{"id": "ssh1"}` | [ { "connections": 0, "id": "ssh1-l1", "kind": "local", "spec": "127.0.0.1:18080 → 127.0.0.1:8080 (via 127.0.0.1:22)" }  |
| ✅ | `ssh_tunnel_remote` | Forward a remote port back to here | `{"id": "ssh1", "remote_addr": "127.0.0.1:18081", "local_addr": "127.0.0.1:8080"}` | { "spec": "127.0.0.1:22:127.0.0.1:18081 → 127.0.0.1:8080 (reverse)", "tunnel_id": "ssh1-r2" } |
| ✅ | `ssh_tunnel_close` | Close one tunnel by id | `{"id": "ssh1", "tunnel_id": "ssh1-l1"}` | tunnel closed |
| ✅ | `ssh_copy_id` | Install the public key on the remote host | `{"id": "ssh1", "key_path": "/home/sentineldesk/.ssh/sweep.pub"}` | key installed on sentineldesk@127.0.0.1:22: installed |
| ✅ | `ssh_disconnect` | End the SSH session | `{"id": "ssh1"}` | SSH session closed |

## Snapshots

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `snapshot_create` | A restore point: the home plus the installed package list | `{"name": "sweep", "note": "created by tool-sweep"}` | { "created": "sweep", "note": "created by tool-sweep", "path": "/home/sentineldesk/.sentineldesk-snapshots/sweep.tar.gz" |
| ✅ | `snapshot_list` | The snapshots on disk | `—` | { "dir": "/home/sentineldesk/.sentineldesk-snapshots", "snapshots": [ { "created": "2026-08-06T13:24:11-03:00", "name":  |
| ⏭️ | `snapshot_restore` | Roll the home back to a snapshot | `—` | it would overwrite the live home directory; the only tool the sweep cannot make safe against something it created |
| ✅ | `snapshot_delete` | Delete a snapshot | `{"name": "sweep"}` | { "deleted": "sweep" } |

## Macro actions

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ⚠️ | `fill_form` | Fill several fields by accessible name and submit | `{"fields": {"Address and search bar": "sentineldesk-sweep"}, "submit": false}` | same AT-SPI limitation as ui_set_text: fill_form writes through the same interface |

## System

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `set_resolution` | Change the resolution without restarting anything | `{"width": 1600, "height": 900}` | { "applied": true, "resolution": "1600x900" } |
| ✅ | `get_screen_info` | Confirm the new geometry | `—` | { "desktops": 4, "display": ":0", "height": 900, "width": 1600 } |
| ✅ | `set_resolution` | Put the resolution back | `{"width": 1920, "height": 1080}` | { "applied": true, "resolution": "1920x1080" } |
| ✅ | `open_app_and_wait` | Launch, wait for the window, focus it, in one call | `{"command": "xterm -T SWEEPKILL -e sleep 300", "match": "SWEEPKILL", "timeout_ms": 20000}` | { "opened": true, "waited_ms": 3978, "window": { "id": "0x0220000c", "desktop": 0, "x": 1363, "y": 441, "w": 484, "h": 3 |
| ✅ | `kill_process` | Kill a process the sweep started, by name | `{"name": "sleep 300", "force": false}` | killed processes matching "sleep 300" |
| ✅ | `remove_packages` | Remove the package the sweep installed | `{"packages": ["openssh-server"], "purge": false}` | { "as_root": true, "exit_code": 0, "log": "Reading package lists...\nBuilding dependency tree...\nReading state informat |

## Handing back

| | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|
| ✅ | `release_control` | Give the desktop back to the people watching | `—` | control released — the controls are free for whoever claims them next |
