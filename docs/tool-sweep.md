# Tool sweep — every tool called against a real desktop

Run on 06 August 2026, 14:25 against `sentineldesk v1.1.7 (ca70992) · build 20260806-131740`.

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

**112 ran · 2 degraded · 1 skipped · 0 failed — 115 tools in 122 calls.**

A tool called more than once counts once, at its worst result.

The results below are shortened to keep the table readable. [`tool-sweep.txt`](tool-sweep.txt) has every call in full, numbered the same way, including the output this table cuts off.

## Every tool, alphabetically

| | Tool | Calls |
|---|---|---|
| ✅ | `action_log` | #20 |
| ✅ | `activate_window` | #36 |
| ✅ | `browser_click` | #61 |
| ✅ | `browser_eval` | #62 |
| ✅ | `browser_goto` | #58 |
| ✅ | `browser_open` | #55 |
| ✅ | `browser_tabs` | #57 |
| ✅ | `browser_text` | #59 |
| ✅ | `browser_type` | #60 |
| ✅ | `browser_wait_for` | #56 |
| ✅ | `check_errors` | #16 |
| ✅ | `close_window` | #48 |
| ⚠️ | `fill_form` | #115 |
| ✅ | `find_text` | #7 |
| ✅ | `fullscreen_window` | #44 |
| ✅ | `gamepad_axis` | #84 |
| ✅ | `gamepad_button` | #82 |
| ✅ | `gamepad_state` | #85 |
| ✅ | `gamepad_tap` | #83 |
| ✅ | `get_active_window` | #9 |
| ✅ | `get_audio_state` | #15, #73 |
| ✅ | `get_clipboard` | #32 |
| ✅ | `get_desktop_info` | #1 |
| ✅ | `get_mouse_position` | #8 |
| ✅ | `get_pixel_color` | #5 |
| ✅ | `get_recording_status` | #75 |
| ✅ | `get_screen_info` | #2, #117 |
| ✅ | `install_packages` | #97 |
| ✅ | `is_running` | #13 |
| ✅ | `key_combo` | #30 |
| ✅ | `kill_process` | #120 |
| ✅ | `launch_app` | #33 |
| ✅ | `list_desktops` | #11 |
| ✅ | `list_directory` | #54 |
| ✅ | `list_installed_apps` | #14 |
| ✅ | `list_processes` | #12 |
| ✅ | `list_recordings` | #78 |
| ✅ | `list_restreams` | #80 |
| ✅ | `list_windows` | #10, #35 |
| ✅ | `maximize_window` | #43 |
| ✅ | `minimize_window` | #41 |
| ✅ | `mouse_click` | #24 |
| ✅ | `mouse_down` | #25 |
| ✅ | `mouse_drag` | #27 |
| ✅ | `mouse_move` | #23 |
| ✅ | `mouse_scroll` | #28 |
| ✅ | `mouse_up` | #26 |
| ✅ | `move_window` | #39 |
| ✅ | `open_app_and_wait` | #119 |
| ✅ | `read_file` | #53 |
| ✅ | `read_screen_text` | #6 |
| ✅ | `release_control` | #122 |
| ✅ | `remove_packages` | #121 |
| ✅ | `request_control` | #22 |
| ✅ | `resize_window` | #40 |
| ✅ | `restore_window` | #42 |
| ✅ | `room_state` | #21 |
| ✅ | `run_command` | #93 |
| ✅ | `screenshot` | #3 |
| ✅ | `screenshot_region` | #4 |
| ✅ | `search_packages` | #95 |
| ✅ | `service_control` | #96 |
| ✅ | `set_clipboard` | #31 |
| ✅ | `set_resolution` | #116, #118 |
| ✅ | `set_volume` | #72 |
| ✅ | `set_window_desktop` | #46 |
| ✅ | `shell_close` | #92 |
| ✅ | `shell_exec` | #87 |
| ✅ | `shell_input` | #88 |
| ✅ | `shell_list` | #91 |
| ✅ | `shell_open` | #86 |
| ✅ | `shell_read` | #90 |
| ✅ | `snapshot_create` | #111 |
| ✅ | `snapshot_delete` | #114 |
| ✅ | `snapshot_list` | #112 |
| ⏭️ | `snapshot_restore` | #113 |
| ✅ | `ssh_connect` | #100 |
| ✅ | `ssh_copy_id` | #109 |
| ✅ | `ssh_disconnect` | #110 |
| ✅ | `ssh_download` | #103 |
| ✅ | `ssh_exec` | #101 |
| ✅ | `ssh_keygen` | #98 |
| ✅ | `ssh_list` | #99 |
| ✅ | `ssh_list_remote` | #104 |
| ✅ | `ssh_tunnel_close` | #108 |
| ✅ | `ssh_tunnel_local` | #105 |
| ✅ | `ssh_tunnel_remote` | #107 |
| ✅ | `ssh_tunnels` | #106 |
| ✅ | `ssh_upload` | #102 |
| ✅ | `start_recording` | #74 |
| ✅ | `start_restream` | #79 |
| ✅ | `stop_recording` | #77 |
| ✅ | `stop_restream` | #81 |
| ✅ | `sudo_status` | #94 |
| ✅ | `switch_desktop` | #47 |
| ✅ | `terminal_open` | #49 |
| ✅ | `terminal_read` | #51 |
| ✅ | `terminal_run` | #50 |
| ✅ | `tool_search` | #19 |
| ✅ | `type_text` | #29 |
| ✅ | `ui_click` | #71 |
| ✅ | `ui_diff` | #67 |
| ✅ | `ui_find` | #64, #65 |
| ✅ | `ui_focus` | #69 |
| ✅ | `ui_get_text` | #68 |
| ⚠️ | `ui_set_text` | #70 |
| ✅ | `ui_tree` | #63 |
| ✅ | `ui_wait_for` | #66 |
| ✅ | `wait` | #17, #76, #89 |
| ✅ | `wait_for_idle` | #18 |
| ✅ | `wait_for_window` | #34 |
| ✅ | `window_hierarchy` | #38 |
| ✅ | `window_properties` | #37 |
| ✅ | `window_set_state` | #45 |
| ✅ | `write_file` | #52 |


## Seeing the desktop

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 1 | ✅ | `get_desktop_info` | Resolution, session and what the desktop is running | `—` | { "display": ":0", "encoder": "auto", "joystick": true, "memory_used": "1580/7833 MB", "recording": false, "resolution": |
| 2 | ✅ | `get_screen_info` | Screen geometry and how many virtual desktops there are | `—` | { "desktops": 4, "display": ":0", "height": 1080, "width": 1920 } |
| 3 | ✅ | `screenshot` | Capture the whole screen as a PNG | `—` | [image saved: /tmp/mcp-screenshot-1786037111914.png] |
| 4 | ✅ | `screenshot_region` | Capture one rectangle instead of the whole screen | `{"x": 0, "y": 0, "width": 320, "height": 200}` | [image saved: /tmp/mcp-screenshot_region-1786037111955.png] |
| 5 | ✅ | `get_pixel_color` | The colour of one pixel, to assert state without an image | `{"x": 10, "y": 10}` | { "b": 214, "g": 207, "hex": "#c3cfd6", "r": 195 } |
| 6 | ✅ | `read_screen_text` | OCR the screen into text | `{"lang": "eng"}` | fe] S By sentineldesk@7d4ffbbf6 sweep. html - Chromium By sentineldesk@7d4ffbbf6.. RJ sentineldesk@7d4ffbbf6.. Rjsentine |
| 7 | ✅ | `find_text` | OCR, but return where a string is so it can be clicked | `{"text": "Files"}` | no match for "files" on screen |
| 8 | ✅ | `get_mouse_position` | Where the pointer is now | `—` | { "x": 360, "y": 340 } |
| 9 | ✅ | `get_active_window` | Which window has focus | `—` | { "geometry": "Window 25165827\n Position: 10,46 (screen: 0)\n Geometry: 945x1024", "id": "0x01800003", "id_dec": "25165 |
| 10 | ✅ | `list_windows` | Every open window with its id and geometry | `—` | [ { "id": "0x00c00006", "desktop": -1, "x": 0, "y": 0, "w": 1920, "h": 36, "class": "panel.lxpanel", "title": "panel" }, |
| 11 | ✅ | `list_desktops` | The virtual desktops and which one is current | `—` | [ { "current": true, "name": "1920x1044 desktop 1", "number": 0 }, { "current": false, "name": "1920x1044 desktop 2", "n |
| 12 | ✅ | `list_processes` | Running processes | `{"filter": "python"}` | [ { "command": "/usr/bin/python3 /usr/bin/supervisord -c /etc/supervisor/supervisord.conf", "cpu": "0.0", "mem": "0.4",  |
| 13 | ✅ | `is_running` | Whether a named process is alive | `{"name": "Xvfb"}` | { "pids": [ 38 ], "running": true } |
| 14 | ✅ | `list_installed_apps` | Applications with a desktop entry | `—` | [ { "exec": "/usr/bin/chromium %U", "name": "Chromium Web Browser" }, { "exec": "uxterm", "name": "UXTerm" }, { "exec":  |
| 15 | ✅ | `get_audio_state` | Sink, volume and whether it is muted | `—` | { "mute": false, "sink": "sentineldesk", "volume": "Volume: front-left: 39321 / 60% / -13.31 dB, front-right: 39321 / 60 |
| 16 | ✅ | `check_errors` | Any error dialog or alert currently on screen | `—` | { "dialogs": [ { "ref": "3/0/0/0/5/3/0", "role": "alert", "text": "￼￼￼￼￼", "title": "Infobar" }, { "ref": "3/0/1", "role |
| 17 | ✅ | `wait` | Sleep, to let the interface settle | `{"ms": 50}` | waited 50 ms |
| 18 | ✅ | `wait_for_idle` | Wait until the screen stops changing and the CPU settles | `{"timeout_ms": 4000, "quiet_ms": 400, "ignore_cpu": true}` | { "cpu_percent": 51, "idle": false, "reason": "the screen went still but the CPU is still at 51%", "waited_ms": 4041 } |

## The catalogue, and the room

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 19 | ✅ | `tool_search` | Find tools from a plain-words description of a task | `{"query": "give someone remote access over ssh", "limit": 5}` | { "matched": 5, "of": 115, "tools": [ { "name": "ssh_list_remote", "category": "ssh", "risk": "read", "description": "Li |
| 20 | ✅ | `action_log` | The audit trail: every call with its connection and result | `{"limit": 3}` | { "count": 3, "entries": [ { "time": "2026-08-06T14:25:16.605-03:00", "tool": "wait", "args": "{\"ms\":50}", "ok": true, |
| 21 | ✅ | `room_state` | Who is in the room, who is driving, may this connection act | `—` | { "controller": "", "controller_id": "", "humans_present": true, "may_inject": false, "note": "Control is always claimed |

## Taking the controls

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 22 | ✅ | `request_control` | Ask the room for the desktop before touching it | `{"timeout_ms": 8000}` | { "granted": true, "reason": "nobody was driving" } |

## Pointer and keyboard

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 23 | ✅ | `mouse_move` | Move the pointer to an absolute position | `{"x": 400, "y": 300}` | moved to (400, 300) |
| 24 | ✅ | `mouse_click` | Click, optionally moving there first | `{"x": 400, "y": 300, "button": 1}` | clicked button 1 x1 |
| 25 | ✅ | `mouse_down` | Press a button and hold it | `{"button": 1}` | mouse_down button 1 |
| 26 | ✅ | `mouse_up` | Release the held button | `{"button": 1}` | mouse_up button 1 |
| 27 | ✅ | `mouse_drag` | Drag from one point to another | `{"x1": 300, "y1": 300, "x2": 360, "y2": 340, "button": 1}` | dragged (300,300) -> (360,340) |
| 28 | ✅ | `mouse_scroll` | Scroll the wheel | `{"dy": -2}` | scrolled dy=-2 dx=0 |
| 29 | ✅ | `type_text` | Type a string into whatever has focus | `{"text": "sweep"}` | typed 5 chars |
| 30 | ✅ | `key_combo` | Press a key or combination by X keysym name | `{"keys": "Escape"}` | pressed Escape |

## Clipboard

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 31 | ✅ | `set_clipboard` | Put text on the X clipboard | `{"text": "sentineldesk-sweep"}` | clipboard set |
| 32 | ✅ | `get_clipboard` | Read the X clipboard back | `—` | sentineldesk-sweep |

## Windows and desktops

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 33 | ✅ | `launch_app` | Start a program on the desktop, detached | `{"command": "xterm -T SWEEPWIN -e sleep 600"}` | { "as_root": false, "command": "xterm -T SWEEPWIN -e sleep 600", "note": "still running after 700 ms. A window may take  |
| 34 | ✅ | `wait_for_window` | Wait until a window matching a title appears | `{"match": "SWEEPWIN", "timeout_ms": 15000}` | { "class": "xterm.XTerm", "found": true, "id": "0x0220000c", "title": "SWEEPWIN" } |
| 35 | ✅ | `list_windows` | List windows, to find the one just opened | `—` | [ { "id": "0x00c00006", "desktop": -1, "x": 0, "y": 0, "w": 1920, "h": 36, "class": "panel.lxpanel", "title": "panel" }, |
| 36 | ✅ | `activate_window` | Focus and raise a window by id | `{"id": "0x0220000c"}` | activated window 0x0220000c |
| 37 | ✅ | `window_properties` | The raw X properties of one window | `{"id": "0x0220000c"}` | { "WM_CLASS": [ "xterm", "XTerm" ], "WM_CLIENT_MACHINE": "7d4ffbbf6685", "WM_COMMAND": [ "{ \"/usr/bin/xterm", "-T", "SW |
| 38 | ✅ | `window_hierarchy` | The raw X window tree, parents and children | `—` | xwininfo: Window id: 0x21f (the root window) (has no name) Root window id: 0x21f (the root window) (has no name) Parent  |
| 39 | ✅ | `move_window` | Move a window to a position | `{"id": "0x0220000c", "x": 120, "y": 120}` | moved 0x0220000c (0,120,120,-1,-1) |
| 40 | ✅ | `resize_window` | Resize a window | `{"id": "0x0220000c", "width": 640, "height": 400}` | resized 0x0220000c (0,-1,-1,640,400) |
| 41 | ✅ | `minimize_window` | Minimise a window | `{"id": "0x0220000c"}` | minimized |
| 42 | ✅ | `restore_window` | Restore a minimised window | `{"id": "0x0220000c"}` | restored |
| 43 | ✅ | `maximize_window` | Maximise a window | `{"id": "0x0220000c"}` | maximized |
| 44 | ✅ | `fullscreen_window` | Put a window full screen | `{"id": "0x0220000c"}` | toggled fullscreen |
| 45 | ✅ | `window_set_state` | Change an EWMH state such as 'above' | `{"id": "0x0220000c", "state": "above", "action": "add"}` | add above en 0x0220000c |
| 46 | ✅ | `set_window_desktop` | Send a window to a virtual desktop | `{"id": "0x0220000c", "desktop": 0}` | moved to desktop |
| 47 | ✅ | `switch_desktop` | Switch to another virtual desktop | `{"desktop": 0}` | switched desktop |
| 48 | ✅ | `close_window` | Close the window the sweep opened | `{"id": "0x0220000c"}` | closed window |

## Terminal

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 49 | ✅ | `terminal_open` | Open a terminal window a person can watch | `—` | { "exit_codes": true, "note": "exit codes are reported. Use `sudo -E su` rather than `sudo su` to keep them across a roo |
| 50 | ✅ | `terminal_run` | Type a command into that terminal and wait for the prompt | `{"command": "echo sweep-terminal-ok", "timeout_ms": 30000}` | { "command": "echo sweep-terminal-ok", "exit_code": 0, "finished": true, "output": "echo sweep-terminal-ok\nsweep-termin |
| 51 | ✅ | `terminal_read` | Read back what the terminal shows | `{"lines": 10}` | { "last_command": "echo sweep-terminal-ok", "last_exit_code": 0, "last_succeeded": true, "text": "sentineldesk@7d4ffbbf6 |

## Files

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 52 | ✅ | `write_file` | Write a file, optionally as root | `{"path": "/tmp/sweep.txt", "content": "sentineldesk sweep\n"}` | wrote 19 bytes to /tmp/sweep.txt |
| 53 | ✅ | `read_file` | Read a file back | `{"path": "/tmp/sweep.txt"}` | sentineldesk sweep |
| 54 | ✅ | `list_directory` | List a directory | `{"path": "/tmp"}` | [ { "modified": "2026-08-06T13:23:05-03:00", "name": ".X0-lock", "size": 11, "type": "file" }, { "modified": "2026-08-06 |

## Browser, over DevTools

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 55 | ✅ | `browser_open` | Start Chromium with DevTools and wait for it to answer | `{"url": "file:///tmp/sweep.html"}` | navigating |
| 56 | ✅ | `browser_wait_for` | Wait for a CSS selector to exist | `{"selector": "#t", "timeout_ms": 15000}` | #t appeared |
| 57 | ✅ | `browser_tabs` | List the open tabs | `—` | [ { "id": "D53D5CF552EF778DDBEB4C3929C4972F", "title": "sweep.html", "url": "file:///tmp/sweep.html" } ] |
| 58 | ✅ | `browser_goto` | Navigate the current tab | `{"url": "file:///tmp/sweep.html"}` | navigating to file:///tmp/sweep.html |
| 59 | ✅ | `browser_text` | Read the page's visible text | `{"max_chars": 200}` | Sweep Go |
| 60 | ✅ | `browser_type` | Type into a field by selector | `{"selector": "#i", "text": "sweep"}` | typed into #i |
| 61 | ✅ | `browser_click` | Click an element by selector | `{"selector": "#b"}` | clicked #b |
| 62 | ✅ | `browser_eval` | Evaluate JavaScript against the real DOM | `{"expression": "document.getElementById('t').textContent"}` | clicked |

## The accessibility tree

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 63 | ✅ | `ui_tree` | Read the desktop as structure rather than pixels | `{"interactive": true, "limit": 40}` | { "count": 457, "elements": [ { "name": "lxpanel", "ref": "0", "role": "application" }, { "center_x": 960, "center_y": 1 |
| 64 | ✅ | `ui_find` | Find elements by role, name or text | `{"limit": 20}` | { "count": 20, "elements": [ { "name": "lxpanel", "ref": "0", "role": "application" }, { "center_x": 960, "center_y": 18 |
| 65 | ✅ | `ui_find` | Find the editable fields, to write into one by ref | `{"role": "entry", "limit": 5}` | { "count": 2, "elements": [ { "actions": [ "activate", "showContextMenu" ], "center_x": 511, "center_y": 109, "height":  |
| 66 | ✅ | `ui_wait_for` | Wait until a matching element exists | `{"role": "frame", "timeout_ms": 6000}` | { "elements": [ { "center_x": 960, "center_y": 18, "height": 36, "name": "panel", "ref": "0/0", "role": "frame", "state" |
| 67 | ✅ | `ui_diff` | Only what changed in the tree since the last call | `{"reset": true}` | { "baseline": true, "nodes": 563, "note": "first call: the reference snapshot was stored; the next one returns only the  |
| 68 | ✅ | `ui_get_text` | Read an element's text by ref, without OCR | `{"ref": "0"}` | { "name": "lxpanel", "ref": "0", "role": "application", "text": "" } |
| 69 | ✅ | `ui_focus` | Give an editable field keyboard focus | `{"ref": "3/0/0/0/5/1/0/5/2"}` | { "ok": true, "ref": "3/0/0/0/5/1/0/5/2" } |
| 70 | ⚠️ | `ui_set_text` | Write text straight into a field by ref, no typing | `{"ref": "3/0/0/0/5/1/0/5/2", "text": "sentineldesk-sweep"}` | Chromium exposes the entry as editable but implements no AT-SPI EditableText on it — use browser_type inside a page |
| 71 | ✅ | `ui_click` | Invoke an element's action directly, no pointer involved | `{"ref": "3/0/0/0/5/1/0/5/2"}` | { "action": "activate", "ok": true, "ref": "3/0/0/0/5/1/0/5/2" } |

## Audio

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 72 | ✅ | `set_volume` | Set the volume, or mute | `{"percent": 60}` | volume 60% |
| 73 | ✅ | `get_audio_state` | Read the volume and mute state back | `—` | { "mute": false, "sink": "sentineldesk", "volume": "Volume: front-left: 39321 / 60% / -13.31 dB, front-right: 39321 / 60 |

## Recording and re-streaming

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 74 | ✅ | `start_recording` | Record the screen to a file alongside the live stream | `{"container": "mp4", "fps": 15, "audio": false}` | recording to /home/sentineldesk/Recordings/rec-20260806-142536.mp4 |
| 75 | ✅ | `get_recording_status` | Whether a recording is running, and how big it is | `—` | { "container": "mp4", "path": "/home/sentineldesk/Recordings/rec-20260806-142536.mp4", "recording": true, "seconds": 0,  |
| 76 | ✅ | `wait` | Let the recording collect a second of video | `{"ms": 1200}` | waited 1200 ms |
| 77 | ✅ | `stop_recording` | Stop and finalise the file | `—` | { "path": "/home/sentineldesk/Recordings/rec-20260806-142536.mp4", "size_bytes": 312968 } |
| 78 | ✅ | `list_recordings` | The recordings on disk | `—` | [ { "modified": "2026-08-06T13:10:55-03:00", "path": "/home/sentineldesk/Recordings/rec-20260806-131054.mp4", "size_byte |
| 79 | ✅ | `start_restream` | Send the live encode to an external destination | `{"url": "udp://127.0.0.1:9999", "platform": "udp"}` | streaming to udp (udp://•••, audio=true) — reusing the live encode, no second capture |
| 80 | ✅ | `list_restreams` | Where the desktop is currently being published | `—` | udp → udp://127.0.0.1:9999 (audio=true, 0s) |
| 81 | ✅ | `stop_restream` | Stop publishing | `—` | stopped 1 destination(s) |

## Gamepad

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 82 | ✅ | `gamepad_button` | Hold or release a button by W3C index | `{"index": 0, "down": true}` | button 0 down |
| 83 | ✅ | `gamepad_tap` | Press and release a button | `{"index": 0, "hold_ms": 40}` | tapped button 0 |
| 84 | ✅ | `gamepad_axis` | Move one stick axis | `{"axis": 0, "value": 0.5}` | axis 0 = 0.50 |
| 85 | ✅ | `gamepad_state` | Set every button and axis in one call | `{"buttons": [0, 0, 0, 0], "axes": [0, 0, 0, 0]}` | gamepad state applied |

## Persistent shells

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 86 | ✅ | `shell_open` | Start a shell session that survives between calls | `{"shell": "/bin/bash"}` | { "id": "sh7", "shell": "/bin/bash", "user": "" } |
| 87 | ✅ | `shell_exec` | Run a command in that session and wait for it | `{"id": "sh7", "command": "echo sweep-shell-ok", "timeout_ms": 10000}` | { "completed": true, "output": "sweep-shell-ok" } |
| 88 | ✅ | `shell_input` | Send raw keystrokes, without waiting | `{"id": "sh7", "text": "echo second", "enter": true}` | sent 12 bytes |
| 89 | ✅ | `wait` | Give the shell a moment to produce output | `{"ms": 400}` | waited 400 ms |
| 90 | ✅ | `shell_read` | Read and clear what the session has produced | `{"id": "sh7"}` | echo second second sentineldesk@7d4ffbbf6685:/$ |
| 91 | ✅ | `shell_list` | The open shell sessions | `—` | [ { "alive": true, "id": "sh7", "pending": 0, "seconds": 1, "user": "" } ] |
| 92 | ✅ | `shell_close` | End the session | `{"id": "sh7"}` | session closed |

## Packages and services

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 93 | ✅ | `run_command` | Run a shell command and return stdout, stderr and exit code | `{"command": "echo sweep-run-ok && uname -s", "timeout_ms": 10000}` | { "as_root": false, "exit_code": 0, "stderr": "", "stdout": "sweep-run-ok\nLinux\n", "timed_out": false } |
| 94 | ✅ | `sudo_status` | Whether passwordless sudo is available in this image | `—` | { "groups": [ "sentineldesk", "sudo", "video" ], "hint": "as_root:true en run_command / launch_app / write_file / read_f |
| 95 | ✅ | `search_packages` | Search apt without installing anything | `{"query": "openssh-server", "limit": 3}` | { "count": 2, "query": "openssh-server", "results": [ { "description": "secure shell (SSH) server, for secure access fro |
| 96 | ✅ | `service_control` | Ask supervisord about the desktop's services | `{"action": "status"}` | { "action": "status", "as_root": true, "exit_code": 3, "service": "all", "stderr": "", "stdout": "at-spi RUNNING pid 41, |
| 97 | ✅ | `install_packages` | Install a package with apt, reporting progress | `{"packages": ["openssh-server"], "update": true, "timeout_ms": 300000}` | { "as_root": true, "exit_code": 0, "installed": { "openssh-server": "1:10.0p1-7+deb13u4" }, "log": "Reading package list |

## SSH

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 98 | ✅ | `ssh_keygen` | Generate a key pair for the desktop's user | `{"path": "/home/sentineldesk/.ssh/sweep", "type": "ed25519", "comment": "sweep"}` | { "note": "the key already existed and was not overwritten", "path": "/home/sentineldesk/.ssh/sweep", "public_key": "ssh |
| 99 | ✅ | `ssh_list` | The open SSH sessions | `—` | null |
| 100 | ✅ | `ssh_connect` | Open an SSH session to a host | `{"host": "127.0.0.1", "user": "sentineldesk", "key_path": "/home/sentineldesk/.ssh/sweep"}` | { "host": "127.0.0.1:22", "id": "ssh7", "user": "sentineldesk" } |
| 101 | ✅ | `ssh_exec` | Run a command on the remote host | `{"id": "ssh7", "command": "echo sweep-ssh-ok", "timeout_sec": 20}` | { "exit_code": 0, "stderr": "", "stdout": "sweep-ssh-ok\n" } |
| 102 | ✅ | `ssh_upload` | Send a file over SFTP | `{"id": "ssh7", "local": "/tmp/sweep.txt", "remote": "/tmp/sweep-up.txt"}` | uploaded 19 bytes to /tmp/sweep-up.txt |
| 103 | ✅ | `ssh_download` | Fetch a file over SFTP | `{"id": "ssh7", "remote": "/tmp/sweep-up.txt", "local": "/tmp/sweep-down.txt"}` | downloaded 19 bytes to /tmp/sweep-down.txt |
| 104 | ✅ | `ssh_list_remote` | List a directory on the remote host | `{"id": "ssh7", "path": "/tmp"}` | [ { "modified": "2026-08-06T14:25:44-03:00", "name": "sweep-down.txt", "size": 19, "type": "file" }, { "modified": "2026 |
| 105 | ✅ | `ssh_tunnel_local` | Forward a local port to the remote side | `{"id": "ssh7", "local_addr": "127.0.0.1:18080", "remote_addr": "127.0.0.1:8080"}` | { "spec": "127.0.0.1:18080 → 127.0.0.1:8080 (via 127.0.0.1:22)", "tunnel_id": "ssh7-l1" } |
| 106 | ✅ | `ssh_tunnels` | The tunnels open on this session | `{"id": "ssh7"}` | [ { "connections": 0, "id": "ssh7-l1", "kind": "local", "spec": "127.0.0.1:18080 → 127.0.0.1:8080 (via 127.0.0.1:22)" }  |
| 107 | ✅ | `ssh_tunnel_remote` | Forward a remote port back to here | `{"id": "ssh7", "remote_addr": "127.0.0.1:18081", "local_addr": "127.0.0.1:8080"}` | { "spec": "127.0.0.1:22:127.0.0.1:18081 → 127.0.0.1:8080 (reverse)", "tunnel_id": "ssh7-r2" } |
| 108 | ✅ | `ssh_tunnel_close` | Close one tunnel by id | `{"id": "ssh7", "tunnel_id": "ssh7-l1"}` | tunnel closed |
| 109 | ✅ | `ssh_copy_id` | Install the public key on the remote host | `{"id": "ssh7", "key_path": "/home/sentineldesk/.ssh/sweep.pub"}` | key installed on sentineldesk@127.0.0.1:22: installed |
| 110 | ✅ | `ssh_disconnect` | End the SSH session | `{"id": "ssh7"}` | SSH session closed |

## Snapshots

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 111 | ✅ | `snapshot_create` | A restore point: the home plus the installed package list | `{"name": "sweep", "note": "created by tool-sweep"}` | { "created": "sweep", "note": "created by tool-sweep", "path": "/home/sentineldesk/.sentineldesk-snapshots/sweep.tar.gz" |
| 112 | ✅ | `snapshot_list` | The snapshots on disk | `—` | { "dir": "/home/sentineldesk/.sentineldesk-snapshots", "snapshots": [ { "created": "2026-08-06T14:25:48-03:00", "name":  |
| 113 | ⏭️ | `snapshot_restore` | Roll the home back to a snapshot | `—` | it would overwrite the live home directory; the only tool the sweep cannot make safe against something it created |
| 114 | ✅ | `snapshot_delete` | Delete a snapshot | `{"name": "sweep"}` | { "deleted": "sweep" } |

## Macro actions

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 115 | ⚠️ | `fill_form` | Fill several fields by accessible name and submit | `{"fields": {"Address and search bar": "sentineldesk-sweep"}, "submit": false}` | same AT-SPI limitation as ui_set_text: fill_form writes through the same interface |

## System

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 116 | ✅ | `set_resolution` | Change the resolution without restarting anything | `{"width": 1600, "height": 900}` | { "applied": true, "resolution": "1600x900" } |
| 117 | ✅ | `get_screen_info` | Confirm the new geometry | `—` | { "desktops": 4, "display": ":0", "height": 900, "width": 1600 } |
| 118 | ✅ | `set_resolution` | Put the resolution back | `{"width": 1920, "height": 1080}` | { "applied": true, "resolution": "1920x1080" } |
| 119 | ✅ | `open_app_and_wait` | Launch, wait for the window, focus it, in one call | `{"command": "xterm -T SWEEPKILL -e sleep 300", "match": "SWEEPKILL", "timeout_ms": 20000}` | { "opened": true, "waited_ms": 2780, "window": { "id": "0x0220000c", "desktop": 0, "x": 778, "y": 791, "w": 484, "h": 31 |
| 120 | ✅ | `kill_process` | Kill a process the sweep started, by name | `{"name": "sleep 300", "force": false}` | killed processes matching "sleep 300" |
| 121 | ✅ | `remove_packages` | Remove the package the sweep installed | `{"packages": ["openssh-server"], "purge": false}` | { "as_root": true, "exit_code": 0, "log": "Reading package lists...\nBuilding dependency tree...\nReading state informat |

## Handing back

| # | | Tool | What it does | Arguments sent | Result |
|---|---|---|---|---|---|
| 122 | ✅ | `release_control` | Give the desktop back to the people watching | `—` | control released — the controls are free for whoever claims them next |
