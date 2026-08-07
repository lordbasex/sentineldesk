#!/usr/bin/env python3
"""Call every tool in the catalogue against a running desktop, and write it up.

`stage1-check.py` proves the mechanisms — annotations, denial kinds, the room
gate, cancellation, progress. This proves the tools: all 116 of them, called in
an order that gives each one the state it needs, with the arguments recorded so
a person can read what was actually done rather than trust a tick.

It is not a unit test. Several tools only mean something in sequence — you
cannot read a terminal you have not opened, or exec on an SSH session you have
not connected — so the sweep opens a window, takes the controls, starts a shell,
connects to itself over SSH, and tidies up after.

Destructive tools are run against things the sweep created, never against
anything it found. Where that is impossible the tool is skipped with the reason
written down, not quietly passed.

Usage:
    ./tool-sweep.py                       # run it and write docs/tool-sweep.md
    ./tool-sweep.py --out report.md
    ./tool-sweep.py --skip-packages       # no apt, so no SSH phase either
"""

import argparse
import datetime
import importlib.util
import json
import os
import re
import shlex
import subprocess
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("mcpcli", os.path.join(HERE, "mcp-cli.py"))
_mcpcli = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_mcpcli)

# The verdict on each tool's METHOD, kept apart from the generated files so it
# survives the next run. Missing entries are reported as not yet reviewed rather
# than silently omitted — an unreviewed tool should be visible.
sys.path.insert(0, HERE)
try:
    from tool_reviews import REVIEWS
except ImportError:
    REVIEWS = {}
try:
    import tool_sweep_es as ES
    from tool_reviews_es import REVIEWS_ES
except ImportError:
    ES, REVIEWS_ES = None, {}

OK, TOLERATED, SKIPPED, FAILED = "ok", "tolerated", "skipped", "failed"

BADGE = {
    OK: "✅",
    TOLERATED: "⚠️",
    SKIPPED: "⏭️",
    FAILED: "❌",
}


class Sweep:
    def __init__(self, client, verbose=False, container=None):
        self.c = client
        self.container = container or _mcpcli.DEFAULT_CONTAINER
        self.verbose = verbose
        self.rows = []
        self.state = {}       # ids picked up along the way: window, shell, ssh, ref
        self.covered = set()
        self.seq = 0          # every call is numbered, in the order it happened

    # --- running one tool ----------------------------------------------------

    # --- verifying from outside the MCP path ---------------------------------

    def sh(self, cmd, timeout=20):
        """Run a command inside the container, without going through MCP.

        This is the whole point of the expect mechanism. Checking a tool's claim
        by calling another tool proves nothing when both sit on the same code:
        read_file confirming what write_file wrote says only that the two agree.
        Every silent success found so far was caught by looking through a
        different channel — ffprobe on the recording rather than the tool's
        "recording to /path", the page's own event counter rather than "typed
        into #f", elementFromPoint rather than "clicked #b". So the verifier
        gets its own way in.
        """
        try:
            out = subprocess.run(
                ["docker", "exec", self.container, "sh", "-c", cmd],
                capture_output=True, text=True, timeout=timeout)
            return out.stdout.strip()
        except Exception as exc:                       # noqa: BLE001
            return f"__ERROR__ {exc}"

    def file_is(self, path, want):
        """The file exists and contains want."""
        got = self.sh(f"cat {shlex.quote(path)} 2>/dev/null")
        if got.startswith("__ERROR__"):
            return f"could not read {path}: {got}"
        if want not in got:
            return f"{path} does not contain {want!r} (got {got[:80]!r})"
        return None

    def file_exists(self, path):
        if self.sh(f"test -e {shlex.quote(path)} && echo yes") != "yes":
            return f"{path} does not exist"
        return None

    def window_titled(self, needle):
        out = self.sh(f"DISPLAY=:0 wmctrl -l 2>/dev/null")
        if needle.lower() not in out.lower():
            return f"no window titled {needle!r} on the display"
        return None

    def window_geometry_near(self, wid, x=None, y=None, w=None, h=None, slack=8):
        """The window really is where the tool said it put it.

        Through xwininfo rather than window_properties, so the answer cannot be
        confirmed by the code that produced it — and rather than wmctrl -lG,
        which reports something else again.

        The distinction cost a false failure before it was understood. A request
        to put a window at y=120 came back from wmctrl -lG as y=176, which reads
        like the move being ignored; the same request made with wmctrl itself
        produces the same 176, so it was never about the tool. wmctrl reports the
        CLIENT area, and the client sits inside a frame whose title bar and
        border are what the window manager actually positioned. xwininfo gives
        both: absolute is the client, relative is its offset inside the frame, so
        absolute minus relative is the frame origin — the number that was asked
        for. 148 - 28 = 120, exactly.
        """
        info = self.sh(f"DISPLAY=:0 xwininfo -id {shlex.quote(wid)} 2>/dev/null")
        if "Absolute upper-left" not in info:
            return f"window {wid} could not be inspected"

        def field(label):
            m = re.search(rf"{label}:\s+(-?\d+)", info)
            return int(m.group(1)) if m else None

        ax, ay = field("Absolute upper-left X"), field("Absolute upper-left Y")
        rx, ry = field("Relative upper-left X"), field("Relative upper-left Y")
        gw, gh = field("Width"), field("Height")
        if None in (ax, ay, rx, ry):
            return f"could not read the geometry of {wid}"
        fx, fy = ax - rx, ay - ry   # where the frame is, which is what was set

        for want, got, label in ((x, fx, "x"), (y, fy, "y"), (w, gw, "width"), (h, gh, "height")):
            if want is not None and got is not None and abs(want - got) > slack:
                return f"{label} is {got}, asked for {want}"
        return None

    def window_gone(self, wid):
        out = self.sh("DISPLAY=:0 wmctrl -l 2>/dev/null")
        if wid.lower() in out.lower():
            return f"window {wid} is still on the display"
        return None

    def playable_video(self, out):
        """The recording exists, decodes, and is in a profile things can play.

        This is the check that would have caught the recorder shipping High
        4:4:4 Predictive for as long as it existed: every tool call succeeded,
        the file grew, and nothing looked at what was in it. ffprobe is the
        different channel.
        """
        m = re.search(r'(/[^\s"]+\.(?:mp4|webm|mkv))', out or "")
        if not m:
            return "no file path in the reply to check"
        path = m.group(1)
        if problem := self.file_exists(path):
            return problem
        info = self.sh(
            "ffprobe -v error -select_streams v:0 "
            "-show_entries stream=codec_name,pix_fmt,nb_frames "
            f"-of default=nw=1 {shlex.quote(path)} 2>/dev/null")
        if not info or "codec_name" not in info:
            return f"{path} has no readable video stream"
        if "pix_fmt=yuv420p" not in info:
            got = next((l for l in info.splitlines() if l.startswith("pix_fmt")), "pix_fmt=?")
            return f"{path} is not 4:2:0 ({got}); most hardware decoders will refuse it"
        return None

    def window_gone_titled(self, needle):
        out = self.sh("DISPLAY=:0 wmctrl -l 2>/dev/null")
        if needle.lower() in out.lower():
            return f"a window titled {needle!r} is still on the display"
        return None

    def process_alive(self, pattern, want=True):
        n = self.sh(f"pgrep -fc {shlex.quote(pattern)} 2>/dev/null || echo 0")
        alive = n.isdigit() and int(n) > 0
        if alive != want:
            return f"{pattern} is {'running' if alive else 'not running'}, expected the opposite"
        return None

    # --- running one tool ----------------------------------------------------

    def run(self, name, description, args=None, tolerate=None, timeout=60,
            capture=None, note="", expect=None):
        """Calls one tool and records the result.

        tolerate: a substring that turns a failure into a warning, for the cases
        where the environment and not the tool is what is missing.

        expect: a callable checking, from outside MCP, that the tool did what it
        said. It returns None when satisfied or a sentence describing the
        mismatch. A tool with no expect is recorded as ran-but-unverified rather
        than as passing, because "did not raise" and "did what it claimed" are
        different statements and this file used to make only the first.
        """
        args = args or {}
        self.covered.add(name)
        try:
            out, is_error = self.c.call(name, args, timeout=timeout)
        except Exception as exc:                       # noqa: BLE001 - report it
            self._record(name, description, args, FAILED, str(exc)[:300], note)
            return None

        status = OK
        if is_error:
            status = TOLERATED if (tolerate and tolerate.lower() in out.lower()) else FAILED
        if not is_error and capture:
            try:
                capture(out)
            except Exception as exc:                   # noqa: BLE001
                note = (note + " " if note else "") + f"(capture failed: {exc})"

        verified = None
        if expect is not None and status == OK:
            try:
                problem = expect(out)
            except Exception as exc:                   # noqa: BLE001
                problem = f"the check itself failed: {exc}"
            if problem:
                status = FAILED
                verified = False
                note = (note + " " if note else "") + f"VERIFICATION: {problem}"
            else:
                verified = True

        self._record(name, description, args, status, out, note, verified)
        return out

    def skip(self, name, description, why, args=None):
        self.covered.add(name)
        self._record(name, description, args or {}, SKIPPED, "", why)

    def _record(self, name, description, args, status, out, note, verified=None):
        full = str(out or "")
        summary = " ".join(full.split())[:180]
        self.seq += 1
        self.rows.append({
            "n": self.seq,
            "tool": name, "description": description, "args": args,
            "status": status, "output": summary, "full": full, "note": note,
            # True  - the effect was confirmed from outside MCP
            # False - it was checked and the check failed
            # None  - nothing checked it, which is not the same as passing
            "verified": verified,
        })
        if self.verbose or status in (FAILED,):
            print(f"  {BADGE[status]} {name:24s} {summary[:70]}")
        elif status != OK:
            print(f"  {BADGE[status]} {name:24s} {note or summary[:60]}")

    def section(self, title):
        print(f"\n\033[1m{title}\033[0m")
        self.rows.append({"section": title})


def first_json(text):
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return None


# --- the phases --------------------------------------------------------------

def phase_observe(s):
    s.section("Seeing the desktop")
    s.run("get_desktop_info", "Resolution, session and what the desktop is running")
    s.run("get_screen_info", "Screen geometry and how many virtual desktops there are")
    s.run("screenshot", "Capture the whole screen as a PNG")
    s.run("screenshot_region", "Capture one rectangle instead of the whole screen",
          {"x": 0, "y": 0, "width": 320, "height": 200})
    s.run("get_pixel_color", "The colour of one pixel, to assert state without an image",
          {"x": 10, "y": 10})
    s.run("read_screen_text", "OCR the screen into text", {"lang": "eng"},
          tolerate="tesseract", timeout=90)
    s.run("find_text", "OCR, but return where a string is so it can be clicked",
          {"text": "Files"}, tolerate="not found", timeout=90)
    s.run("get_mouse_position", "Where the pointer is now")
    s.run("get_active_window", "Which window has focus", tolerate="no active window")
    s.run("list_windows", "Every open window with its id and geometry")
    s.run("list_desktops", "The virtual desktops and which one is current")
    s.run("list_processes", "Running processes", {"filter": "python"})
    s.run("is_running", "Whether a named process is alive", {"name": "Xvfb"})
    s.run("list_commands", "The command-line programs available, by category",
          {"category": "vcs"},
          expect=lambda out: None if '"git"' in out or '"command": "git"' in out
          else "the vcs category does not contain git")
    s.run("list_installed_apps", "Applications with a desktop entry")
    s.run("get_audio_state", "Sink, volume and whether it is muted")
    s.run("check_errors", "Any error dialog or alert currently on screen")
    s.run("wait", "Sleep, to let the interface settle", {"ms": 50})
    s.run("wait_for_idle", "Wait until the screen stops changing and the CPU settles",
          {"timeout_ms": 4000, "quiet_ms": 400, "ignore_cpu": True}, timeout=30)


def phase_registry(s):
    s.section("The catalogue, and the room")
    s.run("tool_search", "Find tools from a plain-words description of a task",
          {"query": "give someone remote access over ssh", "limit": 5})
    s.run("action_log", "The audit trail: every call with its connection and result",
          {"limit": 3})
    s.run("room_state", "Who is in the room, who is driving, may this connection act")


def phase_control(s):
    s.section("Taking the controls")
    out = s.run("request_control", "Ask the room for the desktop before touching it",
                {"timeout_ms": 8000}, timeout=25)
    got = bool(out) and '"granted": true' in out.replace("'", '"')
    if not got:
        print("      could not take control — the input tools will be reported as skipped")
    return got


def phase_input(s, has_control):
    s.section("Pointer and keyboard")
    if not has_control:
        for name, desc in [
            ("mouse_move", "Move the pointer"), ("mouse_click", "Click a button"),
            ("mouse_down", "Press and hold"), ("mouse_up", "Release"),
            ("mouse_drag", "Drag between two points"), ("mouse_scroll", "Scroll the wheel"),
            ("type_text", "Type a string"), ("key_combo", "Press a key combination"),
        ]:
            s.skip(name, desc, "the room did not grant control")
        return
    s.run("mouse_move", "Move the pointer to an absolute position", {"x": 400, "y": 300})
    s.run("mouse_click", "Click, optionally moving there first",
          {"x": 400, "y": 300, "button": 1})
    s.run("mouse_down", "Press a button and hold it", {"button": 1})
    s.run("mouse_up", "Release the held button", {"button": 1})
    s.run("mouse_drag", "Drag from one point to another",
          {"x1": 300, "y1": 300, "x2": 360, "y2": 340, "button": 1})
    s.run("mouse_scroll", "Scroll the wheel", {"dy": -2})
    s.run("type_text", "Type a string into whatever has focus", {"text": "sweep"})
    s.run("key_combo", "Press a key or combination by X keysym name", {"keys": "Escape"})


def phase_clipboard(s):
    s.section("Clipboard")
    s.run("set_clipboard", "Put text on the X clipboard", {"text": "sentineldesk-sweep"},
          expect=lambda out: None
          if "sentineldesk-sweep" in s.sh("DISPLAY=:0 xclip -o -selection clipboard 2>/dev/null")
          else "the X clipboard does not hold what was set")
    s.run("get_clipboard", "Read the X clipboard back")


def phase_windows(s):
    s.section("Windows and desktops")
    s.run("launch_app", "Start a program on the desktop, detached",
          {"command": "xterm -T SWEEPWIN -e sleep 600"},
          expect=lambda out: s.window_titled("SWEEPWIN"))
    s.run("wait_for_window", "Wait until a window matching a title appears",
          {"match": "SWEEPWIN", "timeout_ms": 15000}, timeout=25)

    def grab(out):
        for w in (first_json(out) or []):
            if "SWEEPWIN" in (w.get("title") or ""):
                s.state["window"] = w["id"]
    s.run("list_windows", "List windows, to find the one just opened", capture=grab)

    wid = s.state.get("window")
    if not wid:
        for n, d in [("activate_window", "Focus and raise a window"),
                     ("move_window", "Move a window"), ("resize_window", "Resize a window"),
                     ("minimize_window", "Minimise"), ("restore_window", "Restore"),
                     ("maximize_window", "Maximise"), ("fullscreen_window", "Full screen"),
                     ("set_window_desktop", "Send a window to another desktop"),
                     ("window_properties", "Raw X properties"),
                     ("window_set_state", "Set an EWMH state"),
                     ("close_window", "Close a window")]:
            s.skip(n, d, "the test window did not open")
        s.run("window_hierarchy", "The raw X window tree")
        s.run("switch_desktop", "Switch virtual desktop", {"desktop": 0})
        return

    s.run("activate_window", "Focus and raise a window by id", {"id": wid})
    s.run("window_properties", "The raw X properties of one window", {"id": wid})
    s.run("window_hierarchy", "The raw X window tree, parents and children")
    s.run("move_window", "Move a window to a position", {"id": wid, "x": 120, "y": 120},
          expect=lambda out: s.window_geometry_near(wid, x=120, y=120))
    s.run("resize_window", "Resize a window", {"id": wid, "width": 640, "height": 400},
          expect=lambda out: s.window_geometry_near(wid, w=640, h=400))
    s.run("minimize_window", "Minimise a window", {"id": wid})
    s.run("restore_window", "Restore a minimised window", {"id": wid})
    s.run("maximize_window", "Maximise a window", {"id": wid})
    s.run("fullscreen_window", "Put a window full screen", {"id": wid})
    s.run("window_set_state", "Change an EWMH state such as 'above'",
          {"id": wid, "state": "above", "action": "add"})
    s.run("set_window_desktop", "Send a window to a virtual desktop",
          {"id": wid, "desktop": 0})
    s.run("switch_desktop", "Switch to another virtual desktop", {"desktop": 0})
    s.run("close_window", "Close the window the sweep opened", {"id": wid},
          expect=lambda out: s.window_gone(wid))


def phase_accessibility(s):
    s.section("The accessibility tree")
    s.run("ui_tree", "Read the desktop as structure rather than pixels",
          {"interactive": True, "limit": 40}, timeout=60)

    # Pick elements by what they can DO, not by what comes first. Addressing the
    # first thing on screen meant ui_click landed on something with no action
    # and ui_set_text on something not editable — a coherent refusal from the
    # bridge, and no evidence at all about the tool.
    def grab(out):
        data = first_json(out) or {}
        for it in data.get("elements", []) or []:
            if not it.get("ref"):
                continue
            s.state.setdefault("ref", it["ref"])
            if it.get("actions"):
                s.state.setdefault("ref_actionable", it["ref"])
            if "editable" in (it.get("state") or []):
                s.state.setdefault("ref_editable", it["ref"])
    s.run("ui_find", "Find elements by role, name or text",
          {"limit": 20}, capture=grab, timeout=60)
    s.run("ui_find", "Find the editable fields, to write into one by ref",
          {"role": "entry", "limit": 5}, capture=grab, timeout=60)
    s.run("ui_wait_for", "Wait until a matching element exists",
          {"role": "frame", "timeout_ms": 6000}, tolerate="timed out", timeout=25)
    s.run("ui_diff", "Only what changed in the tree since the last call",
          {"reset": True}, timeout=60)

    ref = s.state.get("ref")
    editable = s.state.get("ref_editable")
    actionable = s.state.get("ref_actionable", ref)
    if not ref:
        for n, d in [("ui_get_text", "Read an element's text by ref"),
                     ("ui_focus", "Give an element keyboard focus"),
                     ("ui_click", "Invoke an element's action directly"),
                     ("ui_set_text", "Write into a field by ref")]:
            s.skip(n, d, "nothing was on screen to address")
        return
    s.run("ui_get_text", "Read an element's text by ref, without OCR", {"ref": ref})
    if editable:
        s.run("ui_focus", "Give an editable field keyboard focus", {"ref": editable})
        # Chromium reports its entries as editable through AT-SPI and does not
        # implement the EditableText interface on them, so the bridge resolves
        # the ref, asks, and is told no. The tool is working; the target cannot
        # do what was asked. Use browser_type for fields inside a page.
        s.run("ui_set_text", "Write text straight into a field by ref, no typing",
              {"ref": editable, "text": "sentineldesk-sweep"},
              tolerate="not editable",
              note="Chromium exposes the entry as editable but implements no "
                   "AT-SPI EditableText on it — use browser_type inside a page")
    else:
        s.skip("ui_focus", "Give an element keyboard focus",
               "no editable element was on screen")
        s.skip("ui_set_text", "Write into a field by ref",
               "no editable element was on screen")
    s.run("ui_click", "Invoke an element's action directly, no pointer involved",
          {"ref": actionable}, tolerate="notimplemented")


def phase_terminal(s):
    s.section("Terminal")
    s.run("terminal_open", "Open a terminal window a person can watch", timeout=40)
    s.run("terminal_run", "Type a command into that terminal and wait for the prompt",
          {"command": "echo sweep-terminal-ok", "timeout_ms": 30000}, timeout=60)
    s.run("terminal_read", "Read back what the terminal shows", {"lines": 10}, timeout=30)


def phase_browser(s):
    s.section("Browser, over DevTools")
    page = "/tmp/sweep.html"
    s.c.call("write_file", {"path": page, "content":
              "<html><body><h1 id='t'>Sweep</h1>"
              "<input id='i'><button id='b' onclick=\"document.getElementById('t')"
              ".textContent='clicked'\">Go</button></body></html>"})
    s.run("browser_open", "Start Chromium with DevTools and wait for it to answer",
          {"url": "file://" + page}, timeout=90, tolerate="did not answer")
    s.run("browser_wait_for", "Wait for a CSS selector to exist",
          {"selector": "#t", "timeout_ms": 15000}, timeout=30, tolerate="timed out")
    s.run("browser_tabs", "List the open tabs", tolerate="connection refused")
    s.run("browser_goto", "Navigate the current tab", {"url": "file://" + page},
          tolerate="connection refused")
    s.run("browser_text", "Read the page's visible text", {"max_chars": 200},
          tolerate="connection refused")
    s.run("browser_type", "Type into a field by selector",
          {"selector": "#i", "text": "sweep"}, tolerate="connection refused")
    s.run("browser_click", "Click an element by selector", {"selector": "#b"},
          tolerate="connection refused")
    s.run("browser_eval", "Evaluate JavaScript against the real DOM",
          {"expression": "document.getElementById('t').textContent"},
          tolerate="connection refused")


def phase_files(s):
    s.section("Files")
    s.run("write_file", "Write a file, optionally as root",
          {"path": "/tmp/sweep.txt", "content": "sentineldesk sweep\n"},
          expect=lambda out: s.file_is("/tmp/sweep.txt", "sentineldesk sweep"))
    # Not "read_file agrees with write_file" — that only proves the two share
    # whatever they share. The container's own cat is the reference.
    s.run("read_file", "Read a file back", {"path": "/tmp/sweep.txt"},
          expect=lambda out: None if "sentineldesk sweep" in out else
          "read_file returned something other than the file's contents")
    s.run("list_directory", "List a directory", {"path": "/tmp"})


def phase_audio(s):
    s.section("Audio")
    s.run("set_volume", "Set the volume, or mute", {"percent": 60})
    s.run("get_audio_state", "Read the volume and mute state back")


def phase_capture(s):
    s.section("Recording and re-streaming")
    s.run("start_recording", "Record the screen to a file alongside the live stream",
          {"container": "mp4", "fps": 15, "audio": False}, timeout=30)
    s.run("get_recording_status", "Whether a recording is running, and how big it is")
    s.run("wait", "Let the recording collect a second of video", {"ms": 1200})
    s.run("stop_recording", "Stop and finalise the file", timeout=60,
          expect=lambda out: s.playable_video(out))
    s.run("list_recordings", "The recordings on disk")
    s.run("start_restream", "Send the live encode to an external destination",
          {"url": "udp://127.0.0.1:9999", "platform": "udp"},
          timeout=40, tolerate="")
    s.run("list_restreams", "Where the desktop is currently being published")
    s.run("stop_restream", "Stop publishing", tolerate="not streaming")


def phase_gamepad(s, has_control):
    s.section("Gamepad")
    if not has_control:
        for n, d in [("gamepad_button", "Press a gamepad button"),
                     ("gamepad_tap", "Tap a button for a moment"),
                     ("gamepad_axis", "Move a stick"),
                     ("gamepad_state", "Set the whole pad at once")]:
            s.skip(n, d, "the room did not grant control")
        return
    miss = "no gamepad"
    s.run("gamepad_button", "Hold or release a button by W3C index",
          {"index": 0, "down": True}, tolerate=miss)
    s.run("gamepad_tap", "Press and release a button", {"index": 0, "hold_ms": 40},
          tolerate=miss)
    s.run("gamepad_axis", "Move one stick axis", {"axis": 0, "value": 0.5}, tolerate=miss)
    s.run("gamepad_state", "Set every button and axis in one call",
          {"buttons": [0, 0, 0, 0], "axes": [0, 0, 0, 0]}, tolerate=miss)


def phase_shell(s):
    s.section("Persistent shells")

    def grab(out):
        data = first_json(out)
        if isinstance(data, dict) and data.get("id"):
            s.state["shell"] = data["id"]
        else:
            m = re.search(r'"id"\s*:\s*"([^"]+)"', out)
            if m:
                s.state["shell"] = m.group(1)
    s.run("shell_open", "Start a shell session that survives between calls",
          {"shell": "/bin/bash"}, capture=grab, timeout=30)
    sid = s.state.get("shell")
    if not sid:
        for n, d in [("shell_exec", "Run a command in a session"),
                     ("shell_input", "Send keystrokes to a session"),
                     ("shell_read", "Read a session's new output"),
                     ("shell_close", "End a session")]:
            s.skip(n, d, "no shell session could be opened")
        s.run("shell_list", "The open shell sessions")
        return
    s.run("shell_exec", "Run a command in that session and wait for it",
          {"id": sid, "command": "echo sweep-shell-ok", "timeout_ms": 10000}, timeout=30)
    s.run("shell_input", "Send raw keystrokes, without waiting",
          {"id": sid, "text": "echo second", "enter": True})
    s.run("wait", "Give the shell a moment to produce output", {"ms": 400})
    s.run("shell_read", "Read and clear what the session has produced", {"id": sid})
    s.run("shell_list", "The open shell sessions")
    s.run("shell_close", "End the session", {"id": sid})


def phase_packages(s, enabled):
    s.section("Packages and services")
    s.run("run_command", "Run a shell command and return stdout, stderr and exit code",
          {"command": "echo sweep-run-ok && uname -s", "timeout_ms": 10000})
    s.run("sudo_status", "Whether passwordless sudo is available in this image")
    s.run("search_packages", "Search apt without installing anything",
          {"query": "openssh-server", "limit": 3}, timeout=120)
    s.run("service_control", "Ask supervisord about the desktop's services",
          {"action": "status"}, timeout=60)
    if not enabled:
        s.skip("install_packages", "Install packages with apt", "--skip-packages was given")
        s.skip("remove_packages", "Remove packages with apt", "--skip-packages was given")
        return False
    out = s.run("install_packages", "Install a package with apt, reporting progress",
                {"packages": ["openssh-server"], "update": True, "timeout_ms": 300000},
                timeout=400)
    return bool(out) and "exit_code\": 0" in (out or "").replace("'", '"')


def phase_ssh(s, ready):
    s.section("SSH")
    names = [("ssh_connect", "Open an SSH session"), ("ssh_exec", "Run a command over SSH"),
             ("ssh_upload", "Send a file over SFTP"), ("ssh_download", "Fetch a file over SFTP"),
             ("ssh_list_remote", "List a remote directory"),
             ("ssh_tunnel_local", "Forward a local port to the remote side"),
             ("ssh_tunnel_remote", "Forward a remote port back here"),
             ("ssh_tunnels", "The tunnels on a session"),
             ("ssh_tunnel_close", "Close one tunnel"),
             ("ssh_copy_id", "Install a public key on the remote host"),
             ("ssh_disconnect", "End the session")]

    s.run("ssh_keygen", "Generate a key pair for the desktop's user",
          {"path": "/home/sentineldesk/.ssh/sweep", "type": "ed25519", "comment": "sweep"},
          timeout=40)
    s.run("ssh_list", "The open SSH sessions")

    if not ready:
        for n, d in names:
            s.skip(n, d, "no SSH server reachable in the container")
        return

    # Bring sshd up and allow the desktop user in with its own key.
    s.c.call("run_command", {"as_root": True, "timeout_ms": 60000, "command":
             "mkdir -p /run/sshd /home/sentineldesk/.ssh && "
             "cat /home/sentineldesk/.ssh/sweep.pub >> /home/sentineldesk/.ssh/authorized_keys && "
             "chown -R sentineldesk:sentineldesk /home/sentineldesk/.ssh && "
             "chmod 700 /home/sentineldesk/.ssh && chmod 600 /home/sentineldesk/.ssh/* && "
             "(pgrep sshd >/dev/null || /usr/sbin/sshd) && sleep 1 && pgrep sshd"}, timeout=90)

    def grab(out):
        m = re.search(r'"id"\s*:\s*"([^"]+)"', out)
        if m:
            s.state["ssh"] = m.group(1)
    s.run("ssh_connect", "Open an SSH session to a host",
          {"host": "127.0.0.1", "user": "sentineldesk",
           "key_path": "/home/sentineldesk/.ssh/sweep"},
          capture=grab, timeout=60, tolerate="")
    sid = s.state.get("ssh")
    if not sid:
        for n, d in names[1:]:
            s.skip(n, d, "the SSH session did not open")
        return

    s.run("ssh_exec", "Run a command on the remote host",
          {"id": sid, "command": "echo sweep-ssh-ok", "timeout_sec": 20}, timeout=40)
    s.run("ssh_upload", "Send a file over SFTP",
          {"id": sid, "local": "/tmp/sweep.txt", "remote": "/tmp/sweep-up.txt"}, timeout=40)
    s.run("ssh_download", "Fetch a file over SFTP",
          {"id": sid, "remote": "/tmp/sweep-up.txt", "local": "/tmp/sweep-down.txt"},
          timeout=40)
    s.run("ssh_list_remote", "List a directory on the remote host",
          {"id": sid, "path": "/tmp"}, timeout=40)
    s.run("ssh_tunnel_local", "Forward a local port to the remote side",
          {"id": sid, "local_addr": "127.0.0.1:18080", "remote_addr": "127.0.0.1:8080"},
          timeout=40)
    tunnels = s.run("ssh_tunnels", "The tunnels open on this session", {"id": sid})
    tid = None
    if tunnels:
        m = re.search(r'"id"\s*:\s*"([^"]+)"', tunnels)
        tid = m.group(1) if m else None
    s.run("ssh_tunnel_remote", "Forward a remote port back to here",
          {"id": sid, "remote_addr": "127.0.0.1:18081", "local_addr": "127.0.0.1:8080"},
          timeout=40, tolerate="")
    if tid:
        s.run("ssh_tunnel_close", "Close one tunnel by id", {"id": sid, "tunnel_id": tid})
    else:
        s.skip("ssh_tunnel_close", "Close one tunnel by id", "no tunnel id came back")
    s.run("ssh_copy_id", "Install the public key on the remote host",
          {"id": sid, "key_path": "/home/sentineldesk/.ssh/sweep.pub"}, timeout=40,
          tolerate="")
    s.run("ssh_disconnect", "End the SSH session", {"id": sid})


def phase_snapshots(s):
    s.section("Snapshots")
    s.run("snapshot_create", "A restore point: the home plus the installed package list",
          {"name": "sweep", "note": "created by tool-sweep"}, timeout=300)
    s.run("snapshot_list", "The snapshots on disk")
    s.skip("snapshot_restore", "Roll the home back to a snapshot",
           "it would overwrite the live home directory; the only tool the sweep "
           "cannot make safe against something it created")
    s.run("snapshot_delete", "Delete a snapshot", {"name": "sweep"})


def phase_system(s, installed):
    s.section("System")
    s.run("set_resolution", "Change the resolution without restarting anything",
          {"width": 1600, "height": 900}, timeout=30)
    s.run("get_screen_info", "Confirm the new geometry")
    s.run("set_resolution", "Put the resolution back", {"width": 1920, "height": 1080},
          timeout=30)
    s.run("open_app_and_wait", "Launch, wait for the window, focus it, in one call",
          {"command": "xterm -T SWEEPKILL -e sleep 300", "match": "SWEEPKILL",
           "timeout_ms": 20000}, timeout=40)
    s.run("kill_process", "Kill a process the sweep started, by name",
          {"name": "sleep 300", "force": False}, tolerate="no process",
          # The window, not the process. Checking with pgrep -f "sleep 300"
          # from a shell whose own command line contains "sleep 300" counts the
          # checker: the pattern matches the process asking the question, so the
          # answer is never zero and the pass is unreachable.
          expect=lambda out: s.window_gone_titled("SWEEPKILL"))
    if installed:
        s.run("remove_packages", "Remove the package the sweep installed",
              {"packages": ["openssh-server"], "purge": False, }, timeout=300)
    else:
        s.skip("remove_packages", "Remove packages with apt",
               "nothing was installed to remove")


def phase_form(s, has_control):
    s.section("Macro actions")
    if has_control:
        s.run("fill_form", "Fill several fields by accessible name and submit",
              {"fields": {"Address and search bar": "sentineldesk-sweep"},
               "submit": False},
              tolerate="a11y settext", timeout=60,
              note="same AT-SPI limitation as ui_set_text: fill_form writes "
                   "through the same interface")
    else:
        s.skip("fill_form", "Fill several fields by accessible name",
               "the room did not grant control")


def phase_release(s, has_control):
    s.section("Handing back")
    if has_control:
        s.run("release_control", "Give the desktop back to the people watching")
    else:
        s.skip("release_control", "Give the desktop back", "control was never granted")


# --- report ------------------------------------------------------------------

def write_report(rows, path, meta):
    # Counted per tool, not per call: several are called more than once on
    # purpose — get_screen_info before and after a resize, wait wherever the
    # desktop needs a moment — and counting rows would inflate the total past
    # the size of the catalogue.
    worst = {}
    rank = {OK: 0, TOLERATED: 1, SKIPPED: 2, FAILED: 3}
    for r in rows:
        if "tool" not in r:
            continue
        prev = worst.get(r["tool"])
        if prev is None or rank[r["status"]] > rank[prev]:
            worst[r["tool"]] = r["status"]
    counts = {OK: 0, TOLERATED: 0, SKIPPED: 0, FAILED: 0}
    for status in worst.values():
        counts[status] += 1
    total = len(worst)
    calls = sum(1 for r in rows if "tool" in r)
    # A tool counts as verified when at least one of its calls had its effect
    # confirmed from outside MCP. Counted separately from the status because
    # "ran" and "did what it said" are different claims and the file used to
    # publish only the first under a tick that read like both.
    verified_tools = len({r["tool"] for r in rows
                          if r.get("tool") and r.get("verified") is True})

    lines = [
        "# Tool sweep — every tool called against a real desktop",
        "",
        f"Run on {meta['when']} against `{meta['version']}`.",
        "",
        "This is the human-readable half of stage 1's verification. "
        "[`tools/stage1-check.py`](../tools/stage1-check.py) proves the mechanisms — "
        "annotations, denial kinds, the room gate, cancellation, progress. This "
        "proves the tools, by calling every one of them through the same stdio "
        "bridge an AI host uses and writing down what was sent.",
        "",
        "Regenerate it with:",
        "",
        "```bash",
        "make up",
        "python3 tools/tool-sweep.py",
        "```",
        "",
        "| | Meaning |",
        "|---|---|",
        f"| {BADGE[OK]} | The tool ran and answered |",
        f"| {BADGE[OK]} ✔︎ | Its effect was confirmed from outside MCP — the file read "
        "back with the container's own cat, the window measured with wmctrl, the "
        "recording decoded with ffprobe |",
        f"| {BADGE[TOLERATED]} | It answered with a failure the environment explains — "
        "no gamepad device, no OCR language pack, nothing listening |",
        f"| {BADGE[SKIPPED]} | Not run, with the reason given |",
        f"| {BADGE[FAILED]} | It failed for a reason the environment does not explain |",
        "",
        f"**{counts[OK]} ran · {counts[TOLERATED]} degraded · "
        f"{counts[SKIPPED]} skipped · {counts[FAILED]} failed — {total} tools "
        f"in {calls} calls.**",
        "",
        f"**{verified_tools} of {total} had their effect verified from outside MCP.** "
        "The rest ran and answered, which is a weaker statement: every silent "
        "success found in this project so far returned cleanly and did something "
        "other than what it reported. A tick without a ✔︎ means nothing checked "
        "the claim.",
        "",
        "A tool called more than once counts once, at its worst result.",
        "",
        "The results below are shortened to keep the table readable. "
        "[`tool-sweep.txt`](tool-sweep.txt) has every call in full, numbered the "
        "same way, including the output this table cuts off.",
        "",
    ]
    if counts[FAILED]:
        lines += ["> Some tools failed. They are listed in their sections below.", ""]

    # An alphabetical index, so a specific tool can be found without reading the
    # run in order. The call numbers are the ones in tool-sweep.txt.
    calls_by_tool = {}
    for r in rows:
        # A tool the sweep never calls is listed with no call number, so the
        # index has to tolerate the absence rather than assume every row came
        # from a run. It had never come up because every tool was covered.
        if "tool" in r and "n" in r:
            calls_by_tool.setdefault(r["tool"], []).append(r["n"])
    lines += [
        "## Every tool, alphabetically",
        "",
        "| | Tool | Calls |",
        "|---|---|---|",
    ]
    for name in sorted(worst):
        where = ", ".join(f"#{n}" for n in calls_by_tool.get(name, []))
        lines.append(f"| {BADGE[worst[name]]} | `{name}` | {where} |")
    lines.append("")

    for r in rows:
        if "section" in r:
            lines += ["", f"## {r['section']}", "",
                      "| # | | Tool | What it does | Arguments sent | Result |",
                      "|---|---|---|---|---|---|"]
            continue
        args = json.dumps(r["args"], ensure_ascii=False) if r["args"] else "—"
        if len(args) > 90:
            args = args[:87] + "…"
        detail = r["note"] or r["output"] or ""
        detail = detail.replace("|", "\\|")[:120] or "—"
        lines.append(
            f"| {r['n']} | {BADGE[r['status']]}{' ✔︎' if r.get('verified') else ''} | `{r['tool']}` | {r['description']} | "
            f"`{args}` | {detail} |")

    with open(path, "w") as fh:
        fh.write("\n".join(lines) + "\n")
    return counts, total


RULE = "-" * 88

def wrap(text, width=84, indent="  "):
    words, line, lines = text.split(), "", []
    for w in words:
        if len(line) + len(w) + 1 > width:
            lines.append(indent + line)
            line = w
        else:
            line = f"{line} {w}".strip()
    if line:
        lines.append(indent + line)
    return lines


def review_block(tool, es=False):
    """What was done, how good the method is, and what would make it excellent."""
    r = REVIEWS.get(tool)
    if not r:
        return ["", say("Review: not yet reviewed.", es)]
    did, better = r["did"], r["better"]
    if es:
        # Spanish where it exists, English where it does not yet — a gap should
        # read as behind rather than as no judgement at all.
        local = REVIEWS_ES.get(tool)
        if local:
            did, better = local["did"], local["better"]
    bar = "●" * r["score"] + "○" * (5 - r["score"])
    return (
        ["", say("Summary:", es)] + wrap(did) +
        ["", f"{say('Method score', es)}: {r['score']}/5  {bar}"] +
        ["", say("To take it further:", es)] + wrap(better)
    )


def say(text, es):
    """The sweep's own prose, translated. Never a tool's answer."""
    return ES.t(text) if (es and ES) else text


STATUS_WORD = {
    OK: "ok",
    TOLERATED: "degraded — the environment explains it",
    SKIPPED: "not run",
    FAILED: "FAILED",
}


def write_transcript(rows, path, meta, es=False):
    """The same run, in full: one block per call, nothing cut off.

    The markdown table has to stay readable, so it shortens what came back. This
    is the other half — what a person needs when the question is not "did it
    answer" but "what did it actually say".
    """
    out = [
        say("SentinelDesk — every tool called against a real desktop", es),
        say("Run on {when} against {version}", es).format(
            when=ES.date(meta["when"]) if (es and ES) else meta["when"],
            version=meta["version"]),
        "",
        say("Generated by tools/tool-sweep.py. The summary table, with the same", es),
        say("numbering, is in docs/tool-sweep.md.", es),
        "",
        say("Result is the tool's reply verbatim. `not run` blocks say why instead.", es),
        "",
    ]
    section = None
    for r in rows:
        if "section" in r:
            section = say(r["section"], es)
            out += ["", "=" * 88, f"== {section}", "=" * 88, ""]
            continue
        args = json.dumps(r["args"], ensure_ascii=False, sort_keys=True) if r["args"] \
            else say("none", es)
        # The tool's own answer is never translated: that is the desktop
        # talking, and a transcript that paraphrases its evidence is not one.
        body = r["full"].rstrip() or (say(r["note"], es) or say("(no output)", es))
        if r["status"] == SKIPPED:
            body = say(r["note"], es) or say("no reason recorded", es)
        elif r["note"] and r["status"] != OK:
            body = f"{body}\n\n    {say('note', es)}: {say(r['note'], es)}"
        out += [
            RULE,
            f"tool: #{r['n']}   [{say(STATUS_WORD[r['status']], es)}]"
            + (say("   [effect verified outside MCP]", es) if r.get("verified") else ""),
            f"{r['tool']}: {say(r['description'], es)}",
            f"{say('Arguments sent', es)}: {args}",
            "Result: " + body,
        ]
        out += review_block(r["tool"], es)
        out += [RULE, ""]
    with open(path, "w") as fh:
        fh.write("\n".join(out) + "\n")


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--container", default=_mcpcli.DEFAULT_CONTAINER)
    ap.add_argument("--sock", default=_mcpcli.DEFAULT_SOCK)
    ap.add_argument("--out", default=os.path.join(HERE, "..", "docs", "tool-sweep.md"))
    ap.add_argument("--skip-packages", action="store_true",
                    help="do not touch apt, and skip the SSH phase that depends on it")
    ap.add_argument("-v", "--verbose", action="store_true")
    args = ap.parse_args()

    try:
        client = _mcpcli.MCPClient(container=args.container, sock=args.sock)
    except Exception as exc:                            # noqa: BLE001
        print(f"could not reach the desktop: {exc}\nIs it running?  make up")
        return 1

    s = Sweep(client, args.verbose, args.container)
    catalogue = [t["name"] for t in client.list_tools()]
    print(f"Sweeping {len(catalogue)} tools in container '{args.container}'")

    try:
        phase_observe(s)
        phase_registry(s)
        has_control = phase_control(s)
        phase_input(s, has_control)
        phase_clipboard(s)
        phase_windows(s)
        phase_terminal(s)
        phase_files(s)
        phase_browser(s)
        # After the terminal and the browser, so the tree has real widgets in it.
        phase_accessibility(s)
        phase_audio(s)
        phase_capture(s)
        phase_gamepad(s, has_control)
        phase_shell(s)
        installed = phase_packages(s, not args.skip_packages)
        phase_ssh(s, installed)
        phase_snapshots(s)
        phase_form(s, has_control)
        phase_system(s, installed)
        phase_release(s, has_control)
    finally:
        client.close()

    missing = sorted(set(catalogue) - s.covered)
    if missing:
        s.rows.append({"section": "Not covered by the sweep"})
        for name in missing:
            s.rows.append({"tool": name, "description": "—", "args": {},
                           "status": SKIPPED, "output": "",
                           "note": "no step in the sweep calls this"})

    when = datetime.datetime.now().strftime("%d %B %Y, %H:%M")
    version = os.popen(
        f"docker exec {args.container} /usr/local/bin/sentineldesk -version 2>/dev/null"
    ).read().strip() or "unknown build"
    out_path = os.path.abspath(args.out)
    meta = {"when": when, "version": version}
    counts, total = write_report(s.rows, out_path, meta)
    stem = os.path.splitext(out_path)[0]
    txt_path = stem + ".txt"
    es_path = stem + "-es.txt"
    write_transcript(s.rows, txt_path, meta)
    write_transcript(s.rows, es_path, meta, es=True)

    print(f"\n{counts[OK]} ran · {counts[TOLERATED]} degraded · "
          f"{counts[SKIPPED]} skipped · {counts[FAILED]} failed  ({total} tools)")
    if missing:
        print(f"not covered: {missing}")
    print(f"written to {out_path}\n           {txt_path}\n           {es_path}")
    return 1 if counts[FAILED] else 0


if __name__ == "__main__":
    sys.exit(main())
