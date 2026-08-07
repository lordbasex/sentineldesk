# Tool sweep — every tool called against a real desktop

Run on 07 August 2026, 07:02 against `sentineldesk v1.2.7 (9828087) · build 20260806-231042`.

This is the human-readable half of stage 1's verification. [`tools/stage1-check.py`](../tools/stage1-check.py) proves the mechanisms — annotations, denial kinds, the room gate, cancellation, progress. This proves the tools, by calling every one of them through the same stdio bridge an AI host uses and writing down what was sent.

Regenerate it with:

```bash
make up
python3 tools/tool-sweep.py
```

| | Meaning |
|---|---|
| ✅ | The tool ran and answered |
| ✅ ✔︎ | Its effect was confirmed from outside MCP — the file read back with the container's own cat, the window measured with wmctrl, the recording decoded with ffprobe |
| ⚠️ | It answered with a failure the environment explains — no gamepad device, no OCR language pack, nothing listening |
| ⏭️ | Not run, with the reason given |
| ❌ | It failed for a reason the environment does not explain |

**113 ran · 2 degraded · 1 skipped · 0 failed — 116 tools in 123 calls.**

**10 of 116 had their effect verified from outside MCP.** The rest ran and answered, which is a weaker statement: every silent success found in this project so far returned cleanly and did something other than what it reported. A tick without a ✔︎ means nothing checked the claim.

A tool called more than once counts once, at its worst result.

The results below are shortened to keep the table readable. [`tool-sweep.txt`](tool-sweep.txt) has every call in full, numbered the same way, including the output this table cuts off.

## Every tool, alphabetically

| | Tool | Calls |
|---|---|---|
| ✅ | `action_log` | #21 |
| ✅ | `activate_window` | #37 |
| ✅ | `browser_click` | #62 |
| ✅ | `browser_eval` | #63 |
| ✅ | `browser_goto` | #59 |
| ✅ | `browser_open` | #56 |
| ✅ | `browser_tabs` | #58 |
| ✅ | `browser_text` | #60 |
| ✅ | `browser_type` | #61 |
| ✅ | `browser_wait_for` | #57 |
| ✅ | `check_errors` | #17 |
| ✅ | `close_window` | #49 |
| ⚠️ | `fill_form` | #116 |
| ✅ | `find_text` | #7 |
| ✅ | `fullscreen_window` | #45 |
| ✅ | `gamepad_axis` | #85 |
| ✅ | `gamepad_button` | #83 |
| ✅ | `gamepad_state` | #86 |
| ✅ | `gamepad_tap` | #84 |
| ✅ | `get_active_window` | #9 |
| ✅ | `get_audio_state` | #16, #74 |
| ✅ | `get_clipboard` | #33 |
| ✅ | `get_desktop_info` | #1 |
| ✅ | `get_mouse_position` | #8 |
| ✅ | `get_pixel_color` | #5 |
| ✅ | `get_recording_status` | #76 |
| ✅ | `get_screen_info` | #2, #118 |
| ✅ | `install_packages` | #98 |
| ✅ | `is_running` | #13 |
| ✅ | `key_combo` | #31 |
| ✅ | `kill_process` | #121 |
| ✅ | `launch_app` | #34 |
| ✅ | `list_commands` | #14 |
| ✅ | `list_desktops` | #11 |
| ✅ | `list_directory` | #55 |
| ✅ | `list_installed_apps` | #15 |
| ✅ | `list_processes` | #12 |
| ✅ | `list_recordings` | #79 |
| ✅ | `list_restreams` | #81 |
| ✅ | `list_windows` | #10, #36 |
| ✅ | `maximize_window` | #44 |
| ✅ | `minimize_window` | #42 |
| ✅ | `mouse_click` | #25 |
| ✅ | `mouse_down` | #26 |
| ✅ | `mouse_drag` | #28 |
| ✅ | `mouse_move` | #24 |
| ✅ | `mouse_scroll` | #29 |
| ✅ | `mouse_up` | #27 |
| ✅ | `move_window` | #40 |
| ✅ | `open_app_and_wait` | #120 |
| ✅ | `read_file` | #54 |
| ✅ | `read_screen_text` | #6 |
| ✅ | `release_control` | #123 |
| ✅ | `remove_packages` | #122 |
| ✅ | `request_control` | #23 |
| ✅ | `resize_window` | #41 |
| ✅ | `restore_window` | #43 |
| ✅ | `room_state` | #22 |
| ✅ | `run_command` | #94 |
| ✅ | `screenshot` | #3 |
| ✅ | `screenshot_region` | #4 |
| ✅ | `search_packages` | #96 |
| ✅ | `service_control` | #97 |
| ✅ | `set_clipboard` | #32 |
| ✅ | `set_resolution` | #117, #119 |
| ✅ | `set_volume` | #73 |
| ✅ | `set_window_desktop` | #47 |
| ✅ | `shell_close` | #93 |
| ✅ | `shell_exec` | #88 |
| ✅ | `shell_input` | #89 |
| ✅ | `shell_list` | #92 |
| ✅ | `shell_open` | #87 |
| ✅ | `shell_read` | #91 |
| ✅ | `snapshot_create` | #112 |
| ✅ | `snapshot_delete` | #115 |
| ✅ | `snapshot_list` | #113 |
| ⏭️ | `snapshot_restore` | #114 |
| ✅ | `ssh_connect` | #101 |
| ✅ | `ssh_copy_id` | #110 |
| ✅ | `ssh_disconnect` | #111 |
| ✅ | `ssh_download` | #104 |
| ✅ | `ssh_exec` | #102 |
| ✅ | `ssh_keygen` | #99 |
| ✅ | `ssh_list` | #100 |
| ✅ | `ssh_list_remote` | #105 |
| ✅ | `ssh_tunnel_close` | #109 |
| ✅ | `ssh_tunnel_local` | #106 |
| ✅ | `ssh_tunnel_remote` | #108 |
| ✅ | `ssh_tunnels` | #107 |
| ✅ | `ssh_upload` | #103 |
| ✅ | `start_recording` | #75 |
| ✅ | `start_restream` | #80 |
| ✅ | `stop_recording` | #78 |
| ✅ | `stop_restream` | #82 |
| ✅ | `sudo_status` | #95 |
| ✅ | `switch_desktop` | #48 |
| ✅ | `terminal_open` | #50 |
| ✅ | `terminal_read` | #52 |
| ✅ | `terminal_run` | #51 |
| ✅ | `tool_search` | #20 |
| ✅ | `type_text` | #30 |
| ✅ | `ui_click` | #72 |
| ✅ | `ui_diff` | #68 |
| ✅ | `ui_find` | #65, #66 |
| ✅ | `ui_focus` | #70 |
| ✅ | `ui_get_text` | #69 |
| ⚠️ | `ui_set_text` | #71 |
| ✅ | `ui_tree` | #64 |
| ✅ | `ui_wait_for` | #67 |
| ✅ | `wait` | #18, #77, #90 |
| ✅ | `wait_for_idle` | #19 |
| ✅ | `wait_for_window` | #35 |
| ✅ | `window_hierarchy` | #39 |
| ✅ | `window_properties` | #38 |
| ✅ | `window_set_state` | #46 |
| ✅ | `write_file` | #53 |


## Seeing the desktop

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 1 | ✅ | `get_desktop_info` | Resolution, session and what the desktop is running | `—` | { "display": ":0", "encoder": "auto", "joystick": true, "memory_used": "2458/7833 MB", "recording": false, "resolution": |
| 2 | ✅ | `get_screen_info` | Screen geometry and how many virtual desktops there are | `—` | { "desktops": 4, "display": ":0", "height": 1080, "width": 1920 } |
| 3 | ✅ | `screenshot` | Capture the whole screen as a PNG | `—` | [image saved: /tmp/mcp-screenshot-1786096926192.png] |
| 4 | ✅ | `screenshot_region` | Capture one rectangle instead of the whole screen | `{"x": 0, "y": 0, "width": 320, "height": 200}` | [image saved: /tmp/mcp-screenshot_region-1786096926224.png] |
| 5 | ✅ | `get_pixel_color` | The colour of one pixel, to assert state without an image | `{"x": 10, "y": 10}` | { "b": 214, "g": 207, "hex": "#c3cfd6", "r": 195 } |
| 6 | ✅ | `read_screen_text` | OCR the screen into text | `{"lang": "eng"}` | { "elements": 153, "text": "panel\nChromium\n07:02\nsentineldesk@134dcd53d3a8: /\nsweep.html - Chromium\nsweep.html\nNew |
| 7 | ✅ | `find_text` | OCR, but return where a string is so it can be clicked | `{"text": "Files"}` | no match for "files" on screen |
| 8 | ✅ | `get_mouse_position` | Where the pointer is now | `—` | { "x": 360, "y": 340 } |
| 9 | ✅ | `get_active_window` | Which window has focus | `—` | { "id": "0x01e00003", "desktop": 0, "x": 0, "y": 36, "w": 1920, "h": 1044, "class": "Chromium", "title": "sweep.html - C |
| 10 | ✅ | `list_windows` | Every open window with its id and geometry | `—` | [ { "id": "0x00c00006", "desktop": -1, "x": 0, "y": 0, "w": 1920, "h": 36, "class": "lxpanel", "title": "panel" }, { "id |
| 11 | ✅ | `list_desktops` | The virtual desktops and which one is current | `—` | [ { "number": 0, "name": "desktop 1", "current": true }, { "number": 1, "name": "desktop 2", "current": false }, { "numb |
| 12 | ✅ | `list_processes` | Running processes | `{"filter": "python"}` | [ { "command": "/usr/bin/python3 /usr/bin/supervisord -c /etc/supervisor/supervisord.conf", "cpu": "0.0", "mem": "0.3",  |
| 13 | ✅ | `is_running` | Whether a named process is alive | `{"name": "Xvfb"}` | { "pids": [ 38 ], "running": true } |
| 14 | ✅ ✔︎ | `list_commands` | The command-line programs available, by category | `{"category": "vcs"}` | { "commands": [ { "category": "vcs", "command": "git", "package": "git", "path": "/usr/bin/git" }, { "category": "vcs",  |
| 15 | ✅ | `list_installed_apps` | Applications with a desktop entry | `—` | [ { "exec": "/usr/bin/chromium %U", "name": "Chromium Web Browser" }, { "exec": "uxterm", "name": "UXTerm" }, { "exec":  |
| 16 | ✅ | `get_audio_state` | Sink, volume and whether it is muted | `—` | { "mute": false, "sink": "sentineldesk", "volume": "Volume: front-left: 39321 / 60% / -13.31 dB, front-right: 39321 / 60 |
| 17 | ✅ | `check_errors` | Any error dialog or alert currently on screen | `—` | { "dialogs": [ { "ref": "3/0/0/0/5/3/0", "role": "alert", "text": "￼￼￼￼￼", "title": "Infobar" }, { "ref": "3/0/1", "role |
| 18 | ✅ | `wait` | Sleep, to let the interface settle | `{"ms": 50}` | waited 50 ms |
| 19 | ✅ | `wait_for_idle` | Wait until the screen stops changing and the CPU settles | `{"timeout_ms": 4000, "quiet_ms": 400, "ignore_cpu": true}` | { "cpu_percent": 0, "idle": true, "reason": "the screen went still and the CPU settled", "waited_ms": 0 } |

## The catalogue, and the room

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 20 | ✅ | `tool_search` | Find tools from a plain-words description of a task | `{"query": "give someone remote access over ssh", "limit": 5}` | { "matched": 5, "of": 116, "tools": [ { "name": "ssh_list_remote", "category": "ssh", "risk": "read", "description": "Li |
| 21 | ✅ | `action_log` | The audit trail: every call with its connection and result | `{"limit": 3}` | { "count": 3, "entries": [ { "time": "2026-08-07T07:02:11.475-03:00", "tool": "wait", "args": "{\"ms\":50}", "ok": true, |
| 22 | ✅ | `room_state` | Who is in the room, who is driving, may this connection act | `—` | { "controller": "", "controller_id": "", "humans_present": true, "may_inject": false, "note": "Control is always claimed |

## Taking the controls

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 23 | ✅ | `request_control` | Ask the room for the desktop before touching it | `{"timeout_ms": 8000}` | { "granted": true, "reason": "nobody was driving" } |

## Pointer and keyboard

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 24 | ✅ | `mouse_move` | Move the pointer to an absolute position | `{"x": 400, "y": 300}` | moved to (400, 300) |
| 25 | ✅ | `mouse_click` | Click, optionally moving there first | `{"x": 400, "y": 300, "button": 1}` | clicked button 1 x1 |
| 26 | ✅ | `mouse_down` | Press a button and hold it | `{"button": 1}` | mouse_down button 1 |
| 27 | ✅ | `mouse_up` | Release the held button | `{"button": 1}` | mouse_up button 1 |
| 28 | ✅ | `mouse_drag` | Drag from one point to another | `{"x1": 300, "y1": 300, "x2": 360, "y2": 340, "button": 1}` | dragged (300,300) -> (360,340) |
| 29 | ✅ | `mouse_scroll` | Scroll the wheel | `{"dy": -2}` | scrolled dy=-2 dx=0 |
| 30 | ✅ | `type_text` | Type a string into whatever has focus | `{"text": "sweep"}` | typed 5 chars |
| 31 | ✅ | `key_combo` | Press a key or combination by X keysym name | `{"keys": "Escape"}` | pressed Escape |

## Clipboard

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 32 | ✅ ✔︎ | `set_clipboard` | Put text on the X clipboard | `{"text": "sentineldesk-sweep"}` | clipboard set |
| 33 | ✅ | `get_clipboard` | Read the X clipboard back | `—` | sentineldesk-sweep |

## Windows and desktops

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 34 | ✅ ✔︎ | `launch_app` | Start a program on the desktop, detached | `{"command": "xterm -T SWEEPWIN -e sleep 600"}` | { "as_root": false, "command": "xterm -T SWEEPWIN -e sleep 600", "note": "still running after 700 ms. A window may take  |
| 35 | ✅ | `wait_for_window` | Wait until a window matching a title appears | `{"match": "SWEEPWIN", "timeout_ms": 15000}` | { "class": "XTerm", "found": true, "id": "0x0180000c", "title": "SWEEPWIN" } |
| 36 | ✅ | `list_windows` | List windows, to find the one just opened | `—` | [ { "id": "0x00c00006", "desktop": -1, "x": 0, "y": 0, "w": 1920, "h": 36, "class": "lxpanel", "title": "panel" }, { "id |
| 37 | ✅ | `activate_window` | Focus and raise a window by id | `{"id": "0x0180000c"}` | activated window 0x0180000c |
| 38 | ✅ | `window_properties` | The raw X properties of one window | `{"id": "0x0180000c"}` | { "WM_CLASS": [ "xterm", "XTerm" ], "WM_CLIENT_MACHINE": "134dcd53d3a8", "WM_COMMAND": [ "{ \"/usr/bin/xterm", "-T", "SW |
| 39 | ✅ | `window_hierarchy` | The raw X window tree, parents and children | `—` | xwininfo: Window id: 0x21f (the root window) (has no name) Root window id: 0x21f (the root window) (has no name) Parent  |
| 40 | ✅ ✔︎ | `move_window` | Move a window to a position | `{"id": "0x0180000c", "x": 120, "y": 120}` | moved |
| 41 | ✅ ✔︎ | `resize_window` | Resize a window | `{"id": "0x0180000c", "width": 640, "height": 400}` | resized |
| 42 | ✅ | `minimize_window` | Minimise a window | `{"id": "0x0180000c"}` | minimized |
| 43 | ✅ | `restore_window` | Restore a minimised window | `{"id": "0x0180000c"}` | restored |
| 44 | ✅ | `maximize_window` | Maximise a window | `{"id": "0x0180000c"}` | maximized |
| 45 | ✅ | `fullscreen_window` | Put a window full screen | `{"id": "0x0180000c"}` | toggled fullscreen |
| 46 | ✅ | `window_set_state` | Change an EWMH state such as 'above' | `{"id": "0x0180000c", "state": "above", "action": "add"}` | add above en 0x0180000c |
| 47 | ✅ | `set_window_desktop` | Send a window to a virtual desktop | `{"id": "0x0180000c", "desktop": 0}` | moved to desktop |
| 48 | ✅ | `switch_desktop` | Switch to another virtual desktop | `{"desktop": 0}` | switched desktop |
| 49 | ✅ ✔︎ | `close_window` | Close the window the sweep opened | `{"id": "0x0180000c"}` | closed window |

## Terminal

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 50 | ✅ | `terminal_open` | Open a terminal window a person can watch | `—` | { "exit_codes": true, "note": "exit codes are reported. Use `sudo -E su` rather than `sudo su` to keep them across a roo |
| 51 | ✅ | `terminal_run` | Type a command into that terminal and wait for the prompt | `{"command": "echo sweep-terminal-ok", "timeout_ms": 30000}` | { "command": "echo sweep-terminal-ok", "exit_code": 0, "finished": true, "output": "echo sweep-terminal-ok\nsweep-termin |
| 52 | ✅ | `terminal_read` | Read back what the terminal shows | `{"lines": 10}` | { "last_command": "echo sweep-terminal-ok", "last_exit_code": 0, "last_succeeded": true, "text": "sentineldesk@134dcd53d |

## Files

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 53 | ✅ ✔︎ | `write_file` | Write a file, optionally as root | `{"path": "/tmp/sweep.txt", "content": "sentineldesk sweep\n"}` | wrote 19 bytes to /tmp/sweep.txt |
| 54 | ✅ ✔︎ | `read_file` | Read a file back | `{"path": "/tmp/sweep.txt"}` | sentineldesk sweep |
| 55 | ✅ | `list_directory` | List a directory | `{"path": "/tmp"}` | [ { "modified": "2026-08-06T23:16:36-03:00", "name": ".X0-lock", "size": 11, "type": "file" }, { "modified": "2026-08-06 |

## Browser, over DevTools

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 56 | ✅ | `browser_open` | Start Chromium with DevTools and wait for it to answer | `{"url": "file:///tmp/sweep.html"}` | loaded file:///tmp/sweep.html |
| 57 | ✅ | `browser_wait_for` | Wait for a CSS selector to exist | `{"selector": "#t", "timeout_ms": 15000}` | #t appeared |
| 58 | ✅ | `browser_tabs` | List the open tabs | `—` | [ { "id": "2DC04267670620BD4FBEC8D6954A12C9", "title": "sweep.html", "url": "file:///tmp/sweep.html" } ] |
| 59 | ✅ | `browser_goto` | Navigate the current tab | `{"url": "file:///tmp/sweep.html"}` | loaded file:///tmp/sweep.html |
| 60 | ✅ | `browser_text` | Read the page's visible text | `{"max_chars": 200}` | Sweep Go |
| 61 | ✅ | `browser_type` | Type into a field by selector | `{"selector": "#i", "text": "sweep"}` | typed into #i |
| 62 | ✅ | `browser_click` | Click an element by selector | `{"selector": "#b"}` | clicked #b |
| 63 | ✅ | `browser_eval` | Evaluate JavaScript against the real DOM | `{"expression": "document.getElementById('t').textContent"}` | clicked |

## The accessibility tree

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 64 | ✅ | `ui_tree` | Read the desktop as structure rather than pixels | `{"interactive": true, "limit": 40}` | { "count": 373, "elements": [ { "name": "lxpanel", "ref": "0", "role": "application" }, { "center_x": 960, "center_y": 1 |
| 65 | ✅ | `ui_find` | Find elements by role, name or text | `{"limit": 20}` | { "count": 20, "elements": [ { "name": "lxpanel", "ref": "0", "role": "application" }, { "center_x": 960, "center_y": 18 |
| 66 | ✅ | `ui_find` | Find the editable fields, to write into one by ref | `{"role": "entry", "limit": 5}` | { "count": 2, "elements": [ { "actions": [ "activate", "showContextMenu" ], "center_x": 1006, "center_y": 99, "height":  |
| 67 | ✅ | `ui_wait_for` | Wait until a matching element exists | `{"role": "frame", "timeout_ms": 6000}` | { "elements": [ { "center_x": 960, "center_y": 18, "height": 36, "name": "panel", "ref": "0/0", "role": "frame", "state" |
| 68 | ✅ | `ui_diff` | Only what changed in the tree since the last call | `{"reset": true}` | { "baseline": true, "nodes": 443, "note": "first call: the reference snapshot was stored; the next one returns only the  |
| 69 | ✅ | `ui_get_text` | Read an element's text by ref, without OCR | `{"ref": "0"}` | { "name": "lxpanel", "ref": "0", "role": "application", "text": "" } |
| 70 | ✅ | `ui_focus` | Give an editable field keyboard focus | `{"ref": "3/0/0/0/5/1/0/5/2"}` | { "ok": true, "ref": "3/0/0/0/5/1/0/5/2" } |
| 71 | ⚠️ | `ui_set_text` | Write text straight into a field by ref, no typing | `{"ref": "3/0/0/0/5/1/0/5/2", "text": "sentineldesk-sweep"}` | Chromium exposes the entry as editable but implements no AT-SPI EditableText on it — use browser_type inside a page |
| 72 | ✅ | `ui_click` | Invoke an element's action directly, no pointer involved | `{"ref": "3/0/0/0/5/1/0/5/2"}` | { "action": "activate", "ok": true, "ref": "3/0/0/0/5/1/0/5/2" } |

## Audio

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 73 | ✅ | `set_volume` | Set the volume, or mute | `{"percent": 60}` | volume 60% |
| 74 | ✅ | `get_audio_state` | Read the volume and mute state back | `—` | { "mute": false, "sink": "sentineldesk", "volume": "Volume: front-left: 39321 / 60% / -13.31 dB, front-right: 39321 / 60 |

## Recording and re-streaming

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 75 | ✅ | `start_recording` | Record the screen to a file alongside the live stream | `{"container": "mp4", "fps": 15, "audio": false}` | recording to /home/sentineldesk/Recordings/rec-20260807-070228.mp4 |
| 76 | ✅ | `get_recording_status` | Whether a recording is running, and how big it is | `—` | { "container": "mp4", "path": "/home/sentineldesk/Recordings/rec-20260807-070228.mp4", "recording": true, "seconds": 0,  |
| 77 | ✅ | `wait` | Let the recording collect a second of video | `{"ms": 1200}` | waited 1200 ms |
| 78 | ✅ ✔︎ | `stop_recording` | Stop and finalise the file | `—` | { "path": "/home/sentineldesk/Recordings/rec-20260807-070228.mp4", "size_bytes": 75005 } |
| 79 | ✅ | `list_recordings` | The recordings on disk | `—` | [ { "modified": "2026-08-06T22:09:15-03:00", "path": "/home/sentineldesk/Recordings/rec-20260806-220914.mp4", "size_byte |
| 80 | ✅ | `start_restream` | Send the live encode to an external destination | `{"url": "udp://127.0.0.1:9999", "platform": "udp"}` | streaming to udp (udp://•••, audio=true) — reusing the live encode, no second capture |
| 81 | ✅ | `list_restreams` | Where the desktop is currently being published | `—` | udp → udp://127.0.0.1:9999 (audio=true, 0s) |
| 82 | ✅ | `stop_restream` | Stop publishing | `—` | stopped 1 destination(s) |

## Gamepad

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 83 | ✅ | `gamepad_button` | Hold or release a button by W3C index | `{"index": 0, "down": true}` | button 0 down |
| 84 | ✅ | `gamepad_tap` | Press and release a button | `{"index": 0, "hold_ms": 40}` | tapped button 0 |
| 85 | ✅ | `gamepad_axis` | Move one stick axis | `{"axis": 0, "value": 0.5}` | axis 0 = 0.50 |
| 86 | ✅ | `gamepad_state` | Set every button and axis in one call | `{"buttons": [0, 0, 0, 0], "axes": [0, 0, 0, 0]}` | gamepad state applied |

## Persistent shells

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 87 | ✅ | `shell_open` | Start a shell session that survives between calls | `{"shell": "/bin/bash"}` | { "id": "sh4", "shell": "/bin/bash", "user": "" } |
| 88 | ✅ | `shell_exec` | Run a command in that session and wait for it | `{"id": "sh4", "command": "echo sweep-shell-ok", "timeout_ms": 10000}` | { "completed": true, "output": "sweep-shell-ok" } |
| 89 | ✅ | `shell_input` | Send raw keystrokes, without waiting | `{"id": "sh4", "text": "echo second", "enter": true}` | sent 12 bytes |
| 90 | ✅ | `wait` | Give the shell a moment to produce output | `{"ms": 400}` | waited 400 ms |
| 91 | ✅ | `shell_read` | Read and clear what the session has produced | `{"id": "sh4"}` | echo second second sentineldesk@134dcd53d3a8:/$ |
| 92 | ✅ | `shell_list` | The open shell sessions | `—` | [ { "alive": true, "id": "sh4", "pending": 0, "seconds": 1, "user": "" } ] |
| 93 | ✅ | `shell_close` | End the session | `{"id": "sh4"}` | session closed |

## Packages and services

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 94 | ✅ | `run_command` | Run a shell command and return stdout, stderr and exit code | `{"command": "echo sweep-run-ok && uname -s", "timeout_ms": 10000}` | { "as_root": false, "exit_code": 0, "stderr": "", "stdout": "sweep-run-ok\nLinux\n", "timed_out": false } |
| 95 | ✅ | `sudo_status` | Whether passwordless sudo is available in this image | `—` | { "groups": [ "sentineldesk", "sudo", "video" ], "hint": "as_root:true en run_command / launch_app / write_file / read_f |
| 96 | ✅ | `search_packages` | Search apt without installing anything | `{"query": "openssh-server", "limit": 3}` | { "count": 2, "query": "openssh-server", "results": [ { "description": "secure shell (SSH) server, for secure access fro |
| 97 | ✅ | `service_control` | Ask supervisord about the desktop's services | `{"action": "status"}` | { "action": "status", "as_root": true, "exit_code": 3, "service": "all", "stderr": "", "stdout": "at-spi RUNNING pid 41, |
| 98 | ✅ | `install_packages` | Install a package with apt, reporting progress | `{"packages": ["openssh-server"], "update": true, "timeout_ms": 300000}` | { "as_root": true, "exit_code": 0, "installed": { "openssh-server": "1:10.0p1-7+deb13u4" }, "log": "Reading package list |

## SSH

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 99 | ✅ | `ssh_keygen` | Generate a key pair for the desktop's user | `{"path": "/home/sentineldesk/.ssh/sweep", "type": "ed25519", "comment": "sweep"}` | { "note": "the key already existed and was not overwritten", "path": "/home/sentineldesk/.ssh/sweep", "public_key": "ssh |
| 100 | ✅ | `ssh_list` | The open SSH sessions | `—` | null |
| 101 | ✅ | `ssh_connect` | Open an SSH session to a host | `{"host": "127.0.0.1", "user": "sentineldesk", "key_path": "/home/sentineldesk/.ssh/sweep"}` | { "host": "127.0.0.1:22", "id": "ssh4", "user": "sentineldesk" } |
| 102 | ✅ | `ssh_exec` | Run a command on the remote host | `{"id": "ssh4", "command": "echo sweep-ssh-ok", "timeout_sec": 20}` | { "exit_code": 0, "stderr": "", "stdout": "sweep-ssh-ok\n" } |
| 103 | ✅ | `ssh_upload` | Send a file over SFTP | `{"id": "ssh4", "local": "/tmp/sweep.txt", "remote": "/tmp/sweep-up.txt"}` | uploaded 19 bytes to /tmp/sweep-up.txt |
| 104 | ✅ | `ssh_download` | Fetch a file over SFTP | `{"id": "ssh4", "remote": "/tmp/sweep-up.txt", "local": "/tmp/sweep-down.txt"}` | downloaded 19 bytes to /tmp/sweep-down.txt |
| 105 | ✅ | `ssh_list_remote` | List a directory on the remote host | `{"id": "ssh4", "path": "/tmp"}` | [ { "modified": "2026-08-07T07:02:38-03:00", "name": "sweep-down.txt", "size": 19, "type": "file" }, { "modified": "2026 |
| 106 | ✅ | `ssh_tunnel_local` | Forward a local port to the remote side | `{"id": "ssh4", "local_addr": "127.0.0.1:18080", "remote_addr": "127.0.0.1:8080"}` | { "spec": "127.0.0.1:18080 → 127.0.0.1:8080 (via 127.0.0.1:22)", "tunnel_id": "ssh4-l1" } |
| 107 | ✅ | `ssh_tunnels` | The tunnels open on this session | `{"id": "ssh4"}` | [ { "connections": 0, "id": "ssh4-l1", "kind": "local", "spec": "127.0.0.1:18080 → 127.0.0.1:8080 (via 127.0.0.1:22)" }  |
| 108 | ✅ | `ssh_tunnel_remote` | Forward a remote port back to here | `{"id": "ssh4", "remote_addr": "127.0.0.1:18081", "local_addr": "127.0.0.1:8080"}` | { "spec": "127.0.0.1:22:127.0.0.1:18081 → 127.0.0.1:8080 (reverse)", "tunnel_id": "ssh4-r2" } |
| 109 | ✅ | `ssh_tunnel_close` | Close one tunnel by id | `{"id": "ssh4", "tunnel_id": "ssh4-l1"}` | tunnel closed |
| 110 | ✅ | `ssh_copy_id` | Install the public key on the remote host | `{"id": "ssh4", "key_path": "/home/sentineldesk/.ssh/sweep.pub"}` | key installed on sentineldesk@127.0.0.1:22: installed |
| 111 | ✅ | `ssh_disconnect` | End the SSH session | `{"id": "ssh4"}` | SSH session closed |

## Snapshots

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 112 | ✅ | `snapshot_create` | A restore point: the home plus the installed package list | `{"name": "sweep", "note": "created by tool-sweep"}` | { "created": "sweep", "note": "created by tool-sweep", "path": "/home/sentineldesk/.sentineldesk-snapshots/sweep.tar.gz" |
| 113 | ✅ | `snapshot_list` | The snapshots on disk | `—` | { "dir": "/home/sentineldesk/.sentineldesk-snapshots", "snapshots": [ { "created": "2026-08-07T07:02:43-03:00", "name":  |
| 114 | ⏭️ | `snapshot_restore` | Roll the home back to a snapshot | `—` | it would overwrite the live home directory; the only tool the sweep cannot make safe against something it created |
| 115 | ✅ | `snapshot_delete` | Delete a snapshot | `{"name": "sweep"}` | { "deleted": "sweep" } |

## Macro actions

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 116 | ⚠️ | `fill_form` | Fill several fields by accessible name and submit | `{"fields": {"Address and search bar": "sentineldesk-sweep"}, "submit": false}` | same AT-SPI limitation as ui_set_text: fill_form writes through the same interface |

## System

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 117 | ✅ | `set_resolution` | Change the resolution without restarting anything | `{"width": 1600, "height": 900}` | { "applied": true, "resolution": "1600x900" } |
| 118 | ✅ | `get_screen_info` | Confirm the new geometry | `—` | { "desktops": 4, "display": ":0", "height": 900, "width": 1600 } |
| 119 | ✅ | `set_resolution` | Put the resolution back | `{"width": 1920, "height": 1080}` | { "applied": true, "resolution": "1920x1080" } |
| 120 | ✅ | `open_app_and_wait` | Launch, wait for the window, focus it, in one call | `{"command": "xterm -T SWEEPKILL -e sleep 300", "match": "SWEEPKILL", "timeout_ms": 20000}` | { "opened": true, "waited_ms": 916, "window": { "id": "0x0180000c", "desktop": 0, "x": 1291, "y": 92, "w": 484, "h": 316 |
| 121 | ✅ ✔︎ | `kill_process` | Kill a process the sweep started, by name | `{"name": "sleep 300", "force": false}` | killed processes matching "sleep 300" |
| 122 | ✅ | `remove_packages` | Remove the package the sweep installed | `{"packages": ["openssh-server"], "purge": false}` | { "as_root": true, "exit_code": 0, "log": "Reading package lists...\nBuilding dependency tree...\nReading state informat |

## Handing back

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 123 | ✅ | `release_control` | Give the desktop back to the people watching | `—` | control released — the controls are free for whoever claims them next |
