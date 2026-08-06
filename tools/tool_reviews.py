#!/usr/bin/env python3
"""A judgement on the METHOD each tool uses, kept beside the run that exercised it.

The sweep answers "did it work". This answers the harder question: is the way it
works the right way, and what would make it excellent.

Scoring is about the mechanism, not the outcome. A tool can answer correctly and
still score poorly if it gets there by parsing another program's output, and a
tool that fails in this container can score well if the approach is sound and
the environment is what is missing.

    5  The right mechanism. Nothing meaningfully better exists on this platform.
    4  The right mechanism with a real gap — a missing option, a lost detail.
    3  Works, but a better mechanism exists and is reachable.
    2  Works by accident of the environment, or is fragile under load or locale.
    1  The wrong mechanism for the job.
    0  Does not work.

`better` is the specific change that would take it to 5 — written to be
actionable, not aspirational. Where a tool is already at 5, it says what would
have to change about the platform for it to matter.

This file is data, read by tools/tool-sweep.py when it writes the transcript.
It lives apart from the generated files so a review survives the next run.
"""

REVIEWS = {}


def review(tool, did, score, better):
    REVIEWS[tool] = {"did": did, "score": score, "better": better}


# --- seeing the desktop -------------------------------------------------------

review(
    "get_desktop_info",
    "Read the display, encoder, resolution, window manager, uptime and memory "
    "in one call, from /proc and the running configuration rather than by "
    "asking X.",
    4,
    "It mixes facts that never change (display, window manager) with ones that "
    "change every second (memory, load). A caller polling for memory re-reads "
    "the constants each time. Split the volatile half out, or say when each "
    "field was sampled.",
)
review(
    "get_screen_info",
    "Reported geometry and the virtual desktop count straight from the X "
    "connection.",
    5,
    "Already the primary source — X is asked, nothing is parsed. It would only "
    "improve if it reported per-monitor geometry, which this single-headed "
    "Xvfb has no way to have.",
)
review(
    "screenshot",
    "Grabbed the framebuffer natively through desktop.GrabScreenshotPNG and "
    "returned PNG, with the same capture available inline, to a file, or "
    "pushed to a watching browser.",
    5,
    "No subprocess, no compression loss, and one capture path for three "
    "destinations. To go further it would need damage tracking — return only "
    "what changed since the last call — which would cut the bytes a model reads "
    "on a mostly-static desktop by an order of magnitude.",
)
review(
    "screenshot_region",
    "Cropped at capture time rather than grabbing the screen and trimming, so "
    "the cost is the rectangle and not the display.",
    5,
    "The right shape already. The improvement is at the caller's level: pair it "
    "with ui_find so a region can be named ('the dialog') instead of measured.",
)
review(
    "get_pixel_color",
    "Read one pixel through the X connection — the cheapest possible way to "
    "assert state, with no image crossing the wire.",
    5,
    "Nothing to improve in the mechanism. The useful addition is a companion "
    "that waits for a pixel to change, which would turn the cheapest read into "
    "the cheapest synchronisation primitive.",
)
review(
    "read_screen_text",
    "Captured at 2x and ran tesseract over it. The upscale is what makes OCR "
    "usable on 11px UI type at all.",
    2,
    "The output in this very run shows the problem: 'SAVRANaAAAA SS', 'oO xXx', "
    "a button read as 'Go' only because it happened to be large. OCR is the "
    "wrong instrument for a desktop — it is trained on documents and a desktop "
    "is icons and gradients. It earns its 2 because it is the only thing that "
    "works on an application with no accessibility support at all. To be "
    "excellent it should try the AT-SPI tree first and fall back to OCR, "
    "reporting which one answered so the caller knows how much to trust it.",
)
review(
    "find_text",
    "OCR with word boxes, mapping a string back to screen coordinates a click "
    "can use.",
    2,
    "Inherits every weakness of read_screen_text and adds one: a misread "
    "character means coordinates for the wrong thing, and the caller cannot "
    "tell. It should return tesseract's per-word confidence, and prefer an "
    "AT-SPI match when the text exists in the tree — where it does, the answer "
    "is exact rather than probable.",
)
review(
    "get_mouse_position",
    "Queried the pointer through the X connection.",
    5,
    "Authoritative and free. Nothing to add.",
)
review(
    "get_active_window",
    "Asked EWMH which window has focus, and reported it degraded when nothing "
    "did — which is a real desktop state, not a failure.",
    4,
    "'no active window: exit status 1' leaks the shell that produced it. An "
    "empty focus is an answer, not an error: it should return null with a note "
    "and isError false, so a caller does not have to decide whether an error "
    "means broken or means nobody is focused.",
)
review(
    "list_windows",
    "Listed every window with id, desktop, geometry, class and title through "
    "wmctrl.",
    3,
    "It shells out to wmctrl and parses columns, which is a locale and format "
    "dependency for data the X connection already holds. The same EWMH "
    "properties it reads are available directly, and going direct would also "
    "remove the fixed-width parsing that breaks on a title containing two "
    "spaces.",
)
review(
    "list_desktops",
    "Listed the virtual desktops and marked the current one, via wmctrl.",
    3,
    "Same shell-out as list_windows, same fix: _NET_DESKTOP_NAMES and "
    "_NET_CURRENT_DESKTOP are one X call away and cannot be mangled by a "
    "locale.",
)
review(
    "list_processes",
    "Ran ps and filtered by substring.",
    3,
    "A substring filter over a text table finds a process whose argument merely "
    "mentions the name. Reading /proc directly would give exact matching on "
    "comm and argv, plus the fields a caller usually wants next — rss, start "
    "time, parent — without a second call.",
)
review(
    "is_running",
    "Asked whether a named process exists.",
    4,
    "Answers the question asked, but a bare true/false makes the caller run "
    "list_processes to learn anything more. Returning the count and the pids "
    "would cost nothing and remove that second call.",
)
review(
    "list_installed_apps",
    "Read the .desktop entries, so the answer is what a person would see in the "
    "menu rather than what dpkg installed.",
    5,
    "The right source: it lists what can be launched, not what exists on disk. "
    "It could carry each entry's icon and categories, which is the difference "
    "between a list and something an agent can reason about.",
)
review(
    "get_audio_state",
    "Asked PulseAudio for the sink, volume and mute state.",
    4,
    "Reports the sink the desktop records from and not what a per-application "
    "stream is doing, so an agent cannot tell which program is making noise. "
    "Listing sink inputs would answer that.",
)
review(
    "check_errors",
    "Walked the accessibility tree for alerts and dialogs, then for anything "
    "whose text reads like a failure — structure first, heuristic second.",
    4,
    "Exactly the right instinct: a graphical program does not fail with an exit "
    "code, it puts a box on the screen. The heuristic half is English-shaped "
    "though, so a Spanish or Portuguese error dialog is invisible to it — the "
    "same three languages the interface already ships would close that.",
)
review(
    "wait",
    "Slept, and since stage 1 it stops when the call is cancelled instead of "
    "sleeping through it.",
    3,
    "It is the tool a model reaches for when it does not know what it is "
    "waiting for, and a guessed duration is either too short or wasted. Every "
    "use of it that could name a condition should be ui_wait_for, "
    "wait_for_window or wait_for_idle instead. Its description should say so.",
)
review(
    "wait_for_idle",
    "Waited for the screen to stop changing and the CPU to settle, sampling "
    "both, and honours cancellation.",
    4,
    "The right answer to the problem `wait` guesses at. It samples the whole "
    "framebuffer, so a blinking cursor or a clock keeps it awake — restricting "
    "the quiet check to a region, or ignoring pixels that change periodically, "
    "would make it usable on a desktop that is never completely still.",
)

# --- the catalogue, and the room ----------------------------------------------

review(
    "tool_search",
    "Ranked the catalogue by keyword over name, category and description, with "
    "stopwords removed and about twenty category aliases, and returned each hit "
    "with its schema and risk so it can be called without a second round trip.",
    4,
    "Deliberately dumb matching, which is right for a corpus of 115 short "
    "strings — and it still needs 'ssh' to be reachable from the query, which "
    "is why the aliases exist. It cannot answer 'the thing that types into a "
    "field without moving the mouse'. Ranking by the schemas a model actually "
    "called next, learned from the action log, would beat any hand-written "
    "alias list.",
)
review(
    "action_log",
    "Returned the audit ring, and since stage 1 each entry names the connection "
    "and client that made the call and carries the denial kind.",
    4,
    "In memory and capped at 2000 entries, with JSONL only when ACTION_LOG is "
    "set. An agent that wants to know what it did an hour ago finds it gone. "
    "Making the file the default, rotated, would cost nothing and make the log "
    "something to rely on rather than something to catch.",
)
review(
    "room_state",
    "Reported who is present, who holds control and whether this connection may "
    "inject — the whole arbitration state in one read.",
    5,
    "The correct primitive for the invariant this project is built on. What is "
    "missing is not in this tool but next to it: there is no way to be told "
    "when it changes, so an agent that loses control finds out by being "
    "refused. That is a notification, not a better read.",
)

# --- taking the controls ------------------------------------------------------

review(
    "request_control",
    "Asked the room for the desktop; granted immediately because nothing was "
    "driving, and it would have put the question to the people watching if "
    "anybody had been.",
    5,
    "This is the design working: control is claimed, never assumed, and the "
    "asking is what makes every handover visible. The improvement is a reason "
    "field the requester can fill in, so the prompt a person sees says what the "
    "agent wants the desktop for rather than only that it wants it.",
)
review(
    "release_control",
    "Handed the desktop back, leaving the controls free rather than passing "
    "them to somebody.",
    5,
    "Right, including the part that looks like an omission: not transferring "
    "means 'free' is a state the room can sit in, and nobody inherits a desktop "
    "they did not ask for. A deferred release — hand back automatically after N "
    "idle seconds — would stop an agent that crashes mid-task from holding the "
    "controls until someone notices.",
)

# --- pointer and keyboard -----------------------------------------------------

review(
    "mouse_move",
    "Moved the pointer through XTEST, the same path the browser's DataChannel "
    "uses, so a person watching sees it move.",
    5,
    "One injection path for both planes is the whole point — an agent's pointer "
    "is not a second, invisible cursor. Nothing to improve at this level; "
    "smoothing a move into steps belongs in the caller.",
)
review(
    "mouse_click",
    "Clicked through XTEST, optionally moving first, with button and "
    "double-click as arguments.",
    5,
    "Correct and complete for what a click is. Clicking blind at coordinates is "
    "the weak part, and the fix is not here: ui_click addresses an element and "
    "cannot miss.",
)
review(
    "mouse_down",
    "Pressed and held a button, leaving the press open.",
    4,
    "Necessary for anything mouse_drag cannot express. It also leaves the "
    "desktop in a held state that survives the call, so an agent that fails "
    "between down and up leaves the button stuck — releasing held buttons when "
    "control is released would make that unrecoverable state impossible.",
)
review(
    "mouse_up",
    "Released a held button.",
    4,
    "Same pairing problem seen from the other end: nothing guarantees it runs. "
    "See mouse_down.",
)
review(
    "mouse_drag",
    "Pressed, moved and released as one call, which is what makes a drag a "
    "drag rather than three racing events.",
    4,
    "Right to be one call. It moves in a straight line at one speed, and some "
    "interfaces distinguish a flick from a drag by velocity — an optional "
    "duration or step count would cover those without complicating the common "
    "case.",
)
review(
    "mouse_scroll",
    "Scrolled by synthesising button 4/5 presses, which is how X expresses a "
    "wheel.",
    3,
    "Button 4/5 is the old encoding; XInput2 smooth scrolling is what modern "
    "toolkits expect, and applications that only listen for it will not move. "
    "Sending smooth scroll events with a button fallback would fix the "
    "applications this currently cannot scroll.",
)
review(
    "type_text",
    "Typed the string through the injector, remapping keycodes on the fly for "
    "characters the active layout has no key for.",
    5,
    "The remapping is what makes it work for accents and symbols rather than "
    "only ASCII, and it is the reason to prefer this over composing key_combo "
    "calls. For a long string it is still one synthetic key event per "
    "character; setting the clipboard and pasting would be faster, but changes "
    "what the desktop sees, so this is the honest default.",
)
review(
    "key_combo",
    "Pressed a combination by X keysym name, resolving each name against the "
    "live keymap.",
    4,
    "Naming keys by keysym is right — it is the layer that survives a layout "
    "change. A keysym the current map does not have is dropped, which is how a "
    "combination can silently do nothing; refusing the call with the missing "
    "name would turn a mute failure into a message.",
)
