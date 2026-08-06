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

# --- launching, running, recording -------------------------------------------

review(
    "launch_app",
    "Started a program detached through setsid, so closing the MCP connection "
    "does not take the application down with it, with as_root going through "
    "sudo -E to keep DISPLAY.",
    4,
    "Detaching is right and the reason is written down. What it cannot say is "
    "whether the program actually started: it returns once the shell has forked "
    "and a command that dies immediately looks identical to one that ran. "
    "open_app_and_wait exists because of that gap, which is a sign this should "
    "at least report the pid and whether it was still alive a moment later.",
)
review(
    "activate_window",
    "Focused and raised a window by id through wmctrl.",
    3,
    "Another shell-out for a single EWMH message. _NET_ACTIVE_WINDOW sent "
    "directly would be one X call with no process and no output to parse, and "
    "would also let it report whether the window manager honoured the request "
    "— which wmctrl's exit status does not distinguish from having been asked "
    "about a window that no longer exists.",
)
review(
    "run_command",
    "Ran a command through sh -c with a deadline, capturing stdout, stderr and "
    "the exit code, killing the process when the call is cancelled and "
    "reporting its output as progress while it runs.",
    5,
    "Everything a shell tool should be: a real deadline, a real kill, and the "
    "output streamed rather than held until the end. It is the most dangerous "
    "tool in the catalogue and it is classified as such. The only thing left is "
    "an argv form alongside the string one, so a caller can pass a filename "
    "with a space in it without thinking about quoting.",
)
review(
    "start_recording",
    "Started a recording by spawning gst-launch-1.0 in its own process group, "
    "capturing and encoding the screen a second time alongside the live "
    "stream.",
    4,
    "The second encode is a deliberate trade, not waste, and measuring it says "
    "so. Reading the framebuffer twice costs 1% of a core — the capture is "
    "nearly free and the encoder is the entire bill. Teeing off the live H.264 "
    "the way start_restream does would make recording almost free, but the "
    "recording would inherit the live stream's keyframe spacing (10s, against "
    "the 2s a file wants for seeking) and its bitrate, which the congestion "
    "estimator pins to the worst viewer's network. An archive whose quality "
    "depends on who happened to be watching is the wrong default; it belongs "
    "behind an explicit option for callers who would rather have the CPU. What "
    "was genuinely wrong was everything nobody had stated a preference about. "
    "ximagesrc hands out BGRx, x264enc takes Y444 as readily as I420, so "
    "videoconvert picked the conversion cheapest for itself and every recording "
    "came out High 4:4:4 Predictive — twice the chroma to encode, in a profile "
    "hardly any hardware decoder accepts, so the file played back in software "
    "or not at all on the devices most likely to open it. The thread count had "
    "the same shape: x264enc's default is derived from the host's core count "
    "and sized for finishing each frame quickly, which a file on disk has no "
    "use for. Together, on a 20-core host recording a scrolling terminal, they "
    "cost 281% of a core against 98% with the format pinned to I420 and threads "
    "pinned to 2 — same frames out, and now a file that plays anywhere. What "
    "remains is the gst-launch child: GStreamer runs in-process everywhere "
    "else, and a child process means no bus, so a pipeline that fails "
    "mid-recording is indistinguishable from one that is working.",
)
review(
    "stop_recording",
    "Signalled the pipeline's process group so the container is finalised "
    "properly, and reported the path and size.",
    4,
    "Stopping cleanly rather than killing is what makes the mp4 playable, and "
    "that detail is easy to get wrong. It very nearly was: the wait for the "
    "child to drain polled cmd.ProcessState while the reaping goroutine wrote "
    "it, so the two raced, and the way that race lost was Stop failing to "
    "notice an exit that had already happened and killing gst partway through "
    "writing the index — producing exactly the unplayable file the SIGINT "
    "handshake exists to prevent. It now waits on a channel closed by the "
    "reaper. What is left is that the wait exists at all: nothing here can tell "
    "a pipeline still flushing from one that hung, because a gst-launch child "
    "offers no bus to ask.",
)
review(
    "get_recording_status",
    "Reported whether a recording is running with elapsed seconds, current size "
    "and path.",
    4,
    "Size on disk is a good proxy for 'is it actually writing', which a boolean "
    "would not give. It cannot say whether frames are being dropped, and a "
    "recording that is running but starving is indistinguishable from a healthy "
    "one until you play it back.",
)
review(
    "list_recordings",
    "Listed the finished files with size and modification time.",
    4,
    "Right shape. It reports what is on disk and not what is playable — a file "
    "left behind by a recording that never stopped cleanly looks the same as a "
    "good one. Probing the container's duration would separate them.",
)

# --- clipboard ----------------------------------------------------------------

review(
    "get_clipboard",
    "Read the X CLIPBOARD selection through xclip, treating an empty clipboard "
    "and an unowned selection as ordinary rather than as errors.",
    3,
    "The distinction it makes is right: nobody owning the selection is not a "
    "failure. But it is a subprocess per read for something the X connection "
    "can do, and text only — an image or a file path on the clipboard is "
    "invisible to it, which is exactly what a person copying something for the "
    "agent is most likely to have.",
)

# --- windows ------------------------------------------------------------------

review(
    "wait_for_window",
    "Blocked on an X event until a window whose title or class matches exists, "
    "with a deadline, and stops when the call is cancelled.",
    5,
    "It used to poll: wmctrl every 300ms, about fifty processes across a "
    "fifteen-second wait to be told nothing had happened forty-nine times, with "
    "the answer arriving up to a third of a second late. The window manager was "
    "publishing the change on _NET_CLIENT_LIST the whole time. It now selects "
    "PropertyChangeMask on the root window and blocks, measured at the same "
    "process count as a plain sleep of the same length — 33 spawns became zero "
    "— and detection within the noise of the window's own startup. One honest "
    "limit remains and is why a one-second backstop tick sits alongside the "
    "events: a window that is already open and renames itself writes "
    "_NET_WM_NAME on its own window, not on the root, so a root watcher cannot "
    "see it. Covering that means tracking the client list, selecting events on "
    "each new window, and handling the ones destroyed in between.",
)
review(
    "move_window",
    "Moved a window through wmctrl -e.",
    3,
    "Shell-out and a geometry string. It also cannot express 'move without "
    "resizing' except by passing -1 sentinels internally, which is a sign the "
    "underlying call is the wrong shape. A direct ConfigureWindow takes the "
    "fields that are being set and nothing else.",
)
review(
    "resize_window",
    "Resized through the same wmctrl -e path.",
    3,
    "Same as move_window, and shares its helper. Worth fixing together rather "
    "than separately: one direct geometry call replaces both.",
)
review(
    "minimize_window",
    "Minimised through xdotool windowminimize.",
    3,
    "A second shell tool for what the others do through wmctrl, so the family "
    "now depends on two external programs to do one kind of thing. "
    "_NET_WM_STATE_HIDDEN through the same path as the rest would remove the "
    "xdotool dependency entirely.",
)
review(
    "maximize_window",
    "Added the maximized_vert and maximized_horz EWMH states through wmctrl.",
    4,
    "Correct mechanism — it asks the window manager rather than resizing to the "
    "screen, so a maximised window stays maximised when the resolution "
    "changes. Only the shell-out separates it from a five.",
)
review(
    "restore_window",
    "Removed both maximised states.",
    4,
    "The proper inverse of maximise, and it does not try to remember a previous "
    "geometry the window manager already knows. Same shell-out caveat.",
)
review(
    "fullscreen_window",
    "Toggled _NET_WM_STATE_FULLSCREEN.",
    3,
    "Toggling is the problem: an agent that cannot see the current state does "
    "not know which way it went, so 'make this full screen' takes a read and a "
    "guess. It should take the state it wants — add, remove or toggle — the way "
    "window_set_state already does.",
)
review(
    "set_window_desktop",
    "Moved a window to a virtual desktop through wmctrl -t.",
    4,
    "Right mechanism, and it accepts the desktop index the other tools report, "
    "so the numbers line up across the family. Shell-out again.",
)
review(
    "switch_desktop",
    "Switched the current virtual desktop through wmctrl -s.",
    4,
    "Same. The addition worth having is switching by name rather than index, "
    "since _NET_DESKTOP_NAMES is already what list_desktops reads.",
)
review(
    "window_properties",
    "Read one window's raw X properties.",
    5,
    "This is the one in the family that goes to the source, and it is the most "
    "useful of them for an agent trying to understand a window it did not "
    "open. Nothing to change.",
)
review(
    "window_hierarchy",
    "Walked the X window tree, reporting parents, children and "
    "override-redirect.",
    5,
    "The right answer for questions the EWMH list cannot express — tooltips, "
    "menus and popups are override-redirect and never appear in list_windows. "
    "Pairing it with the accessibility tree would be the next step, but that is "
    "a new tool rather than a change to this one.",
)
review(
    "window_set_state",
    "Set an EWMH state — above, below, sticky, shaded, skip_taskbar and the "
    "rest — with an explicit add, remove or toggle.",
    4,
    "The best-shaped tool in the window family: it names the state and the "
    "action instead of hiding both behind a verb, which is what "
    "fullscreen_window should have done. It could replace maximize, restore and "
    "fullscreen outright, leaving one tool where there are four.",
)

# --- the accessibility tree ---------------------------------------------------

review(
    "ui_tree",
    "Read the desktop through AT-SPI as roles, names, states and coordinates — "
    "structure rather than pixels — filtered to the actionable parts.",
    4,
    "The right instrument, and the reason read_screen_text scores a two. What "
    "keeps it off five is the cost: every ui_* call spawns python3 and imports "
    "pyatspi, which is a few hundred milliseconds of process before any work "
    "happens. a11y.py should be a small daemon holding one AT-SPI connection, "
    "the way the MCP socket already is.",
)
review(
    "ui_find",
    "Searched by role, name or text and returned each match with its ref, its "
    "actions, its states and screen coordinates.",
    4,
    "Returning coordinates alongside the ref is what lets a caller fall back to "
    "a click when an action is missing, and that is good design. It should also "
    "say which AT-SPI interfaces each element implements: this sweep found "
    "Chromium reporting an entry as `editable` while implementing no "
    "EditableText, so ui_set_text failed on something that looked writable. "
    "That is knowable before the call, and only this tool can say it.",
)
review(
    "ui_get_text",
    "Read one element's text by ref, straight from the accessibility interface.",
    4,
    "Exact where OCR is probable, and cheap where a screenshot is not. Same "
    "per-call Python cost as the rest of the family.",
)
review(
    "ui_click",
    "Invoked an element's own action by ref. The pointer never moves, so it "
    "cannot miss, and a partly covered window does not matter.",
    4,
    "This is the best approach to clicking in the catalogue — mouse_click at "
    "coordinates is a guess by comparison, and this is why. Only the per-call "
    "subprocess separates it from five. It could also report which action it "
    "invoked when an element has several, since 'the first one' is a decision "
    "the caller cannot currently see.",
)
review(
    "ui_set_text",
    "Wrote text into a field by ref through AT-SPI, without depending on which "
    "window has focus.",
    4,
    "The right interface, and better than typing when it works. What it cannot "
    "do is tell you in advance that it will not: Chromium exposes its entries "
    "as editable and implements no EditableText, so this refuses correctly and "
    "the caller only finds out by trying. Publishing the interface list in "
    "ui_find fixes it there rather than here. Inside a page, browser_type is "
    "the answer.",
)
review(
    "ui_focus",
    "Gave keyboard focus to an element by ref.",
    4,
    "The right pairing for type_text — focus by structure, then type — and it "
    "avoids the click-to-focus dance that moves the pointer somewhere the user "
    "did not expect. Same subprocess cost.",
)
review(
    "ui_wait_for",
    "Polled the tree until an element matching role, name or text appeared, with "
    "a deadline.",
    3,
    "AT-SPI emits events for exactly this — object:children-changed, "
    "object:state-changed — and polling means both a delay before noticing and a "
    "Python process per poll. A bridge holding one connection could block on the "
    "event and answer the instant it fires.",
)
review(
    "ui_diff",
    "Returned only what changed in the tree since the last call, keeping the "
    "previous snapshot server-side.",
    5,
    "The best answer in the catalogue to the problem that actually limits an "
    "agent: context. A full ui_tree after every action is most of a model's "
    "budget spent re-reading what it already knew. Keeping the snapshot on this "
    "side is what makes it possible. Nothing to change.",
)

# --- terminal -----------------------------------------------------------------

review(
    "terminal_open",
    "Opened a terminal emulator on the desktop, visible to anyone watching, "
    "with a shell that reports its exit status.",
    4,
    "The point is not that an agent needs a terminal — run_command exists — but "
    "that a person watching can see what it is doing. That is a product "
    "decision worth the cost. It cannot reuse a terminal a person opened "
    "themselves, so an agent and a person end up with two.",
)
review(
    "terminal_run",
    "Typed a command into the terminal with xdotool, waited for the prompt to "
    "come back, and reported the exit status — counting the terminals first so "
    "that a command which closes the shell is not waited out to its timeout.",
    3,
    "The care in it is real: xdotool rather than raw XTEST because it remaps "
    "keycodes for the characters command lines are full of, and the "
    "terminal-count check exists because a positional ref silently starts "
    "resolving to another window. But it is still typing into a screen and "
    "reading a prompt back. Echoing a sentinel with the exit code and waiting "
    "for that exact string would remove the prompt heuristic entirely.",
)
review(
    "terminal_read",
    "Read the terminal's visible text back through the accessibility tree.",
    4,
    "Reading the emulator's own text rather than OCR of its pixels is the right "
    "choice and the reason this is usable at all. It sees only what is on "
    "screen, so output that scrolled past is gone — which is the difference "
    "between this and shell_read.",
)

# --- browser ------------------------------------------------------------------

review(
    "browser_open",
    "Started Chromium with the debugging port and polled until CDP answered, up "
    "to forty seconds.",
    3,
    "Waiting for the port rather than assuming is right. Forty seconds of "
    "polling is not: the browser writes its DevTools endpoint to a file when it "
    "is ready, and watching for that would turn a poll into an answer. It also "
    "cannot reuse a Chromium a person opened without the flag.",
)
review(
    "browser_tabs",
    "Listed the open targets over CDP's HTTP endpoint.",
    4,
    "The authoritative source — this is what the browser says about itself. It "
    "opens a fresh HTTP client per call, which is cheap here but part of a "
    "pattern the family shares.",
)
review(
    "browser_goto",
    "Navigated by setting location.href through CDP.",
    3,
    "Setting location.href is the blunt version: Page.navigate is the protocol's "
    "own command, distinguishes a failed load from a successful one, and "
    "returns a frame id to wait on. This returns as soon as the assignment is "
    "made, so 'navigated' means 'asked to', not 'arrived'.",
)
review(
    "browser_eval",
    "Evaluated JavaScript against the live DOM over CDP.",
    5,
    "The most authoritative tool in the browser family: it asks the page "
    "itself rather than anything's picture of it. Correctly classified as "
    "dangerous, since it is arbitrary code in whatever origin is loaded. A "
    "fresh WebSocket per call is the family's shared cost, not a flaw in this "
    "one.",
)
review(
    "browser_click",
    "Clicked an element by CSS selector through the DOM.",
    4,
    "Addressing by selector cannot miss the way coordinates can, which is the "
    "same reasoning that makes ui_click better than mouse_click. It dispatches "
    "the click in JavaScript rather than through Input.dispatchMouseEvent, so a "
    "page that distinguishes a trusted event from a synthetic one — payment "
    "flows, some anti-automation checks — will not accept it.",
)
review(
    "browser_type",
    "Typed into a field by selector.",
    4,
    "This is what ui_set_text cannot do inside a page, and the two together "
    "cover the whole desktop. Same trusted-event caveat as browser_click: "
    "setting a value in JavaScript does not always fire the events a framework "
    "listens for.",
)
review(
    "browser_text",
    "Read the page's visible text through the DOM.",
    5,
    "Exact where OCR of a browser window is guesswork, and it respects what is "
    "actually rendered rather than what is in the markup. Nothing to change at "
    "this level.",
)
review(
    "browser_wait_for",
    "Polled for a CSS selector to exist, with a deadline, stopping when the "
    "call is cancelled.",
    3,
    "Polling with a 300ms tick where the platform has MutationObserver and CDP "
    "has DOM events. The same criticism as ui_wait_for and wait_for_window, and "
    "the same fix: wait on the event, not on the clock.",
)

# --- files --------------------------------------------------------------------

review(
    "read_file",
    "Read a file with os.ReadFile, or through cat under sudo when as_root is "
    "asked for, since the daemon itself runs unprivileged.",
    5,
    "The split is exactly right: the ordinary path is a direct read with no "
    "process, and privilege is an explicit request rather than something the "
    "daemon holds. max_bytes stops a caller filling its own context with a log "
    "file. Nothing to improve.",
)
review(
    "write_file",
    "Wrote a file directly, or through a privileged helper for as_root, with "
    "append and mode as options.",
    5,
    "Same shape as read_file and the same reasoning. Correctly classified as "
    "dangerous. The one thing it cannot do is write atomically — a partial "
    "write is visible to anything watching the file — which matters for "
    "configuration a service is reading.",
)
review(
    "list_directory",
    "Listed a directory with names, sizes, types and modification times.",
    5,
    "Direct, and it returns the fields a caller would otherwise need a second "
    "call for. Nothing to change.",
)

# --- macro actions ------------------------------------------------------------

review(
    "open_app_and_wait",
    "Launched a program, waited for its window to appear, focused it and waited "
    "for the paint to settle — the four calls an agent would otherwise make, as "
    "one.",
    4,
    "This exists because launch_app cannot say whether the program started, and "
    "compressing four round trips into one is worth real context. It inherits "
    "wait_for_window's polling, so fixing that fixes this.",
)
review(
    "fill_form",
    "Filled several fields by accessible name and optionally pressed a button, "
    "reporting per-field success.",
    4,
    "Reporting each field separately rather than one pass/fail is the right "
    "choice — a form where three of four fields took is a different problem "
    "from one that did nothing. It writes through the same AT-SPI interface as "
    "ui_set_text and inherits its limitation: on Chromium it cannot, and cannot "
    "say so in advance.",
)

# --- closing, killing ---------------------------------------------------------

review(
    "close_window", "Asked the window manager to close a window through wmctrl.", 3,
    "The polite close — the application gets to ask about unsaved work — which "
    "is right. Shell-out like the rest of the family, and it cannot report "
    "whether the window actually went, since a program with a confirmation "
    "dialog stays open and this returns success either way.")
review(
    "kill_process", "Ended a process by name or pid, with force as an option.", 4,
    "Signalling rather than always using SIGKILL is the correct default: a "
    "process that can clean up should be allowed to. Matching by name has the "
    "same substring problem as list_processes, so 'sleep' can end more than "
    "intended — returning what it matched before acting would make that "
    "visible.")

# --- gamepad ------------------------------------------------------------------

review(
    "gamepad_button", "Pressed or released a button on a real uinput device.", 5,
    "A virtual device in the kernel, not synthetic X events: the application "
    "reads it through evdev exactly as it would a plugged-in controller, and "
    "cannot tell the difference. That is the strongest possible answer here, "
    "and the reason the whole family works in games that ignore fake input.")
review(
    "gamepad_tap", "Pressed and released with a hold in between.", 4,
    "The convenience that stops a caller having to time two calls across the "
    "wire, which is where a tap becomes a hold. It blocks for the duration, so "
    "a long hold occupies the call; that is the honest trade for accuracy.")
review(
    "gamepad_axis", "Moved one stick axis through the same uinput device.", 5,
    "Absolute axis values on a real device. Nothing better exists short of a "
    "physical controller.")
review(
    "gamepad_state", "Set every button and axis in one call.", 5,
    "The right shape for a game loop: one call per frame rather than a dozen, "
    "and a consistent snapshot instead of a race between separate events.")

# --- audio, re-streaming ------------------------------------------------------

review(
    "set_volume", "Set the volume or mute through pactl.", 3,
    "Shells out per call for something PulseAudio exposes over its own socket. "
    "It also sets the sink the desktop records from, so it changes what a "
    "recording captures as well as what a listener hears — worth saying in the "
    "description, since those are different intentions.")
review(
    "start_restream",
    "Attached an external destination to the live H.264 output through the "
    "pipeline's tee, encoding nothing a second time.", 5,
    "This is the exemplar the rest of the media path should follow. A second "
    "viewer costs bandwidth, not CPU, and so does a second destination. It is "
    "also correctly gated by the room: publishing what is on everyone's screen "
    "to somewhere outside it is not a decision an agent makes alone. "
    "start_recording is the tool that should be reading this one's source.")
review(
    "stop_restream", "Detached a destination from the tee.", 4,
    "Clean removal without disturbing the live encode, which is what the tee "
    "buys. Stopping by id when several are running is right; stopping all of "
    "them needs a loop the caller has to write.")
review(
    "list_restreams", "Reported where the desktop is currently being published.", 4,
    "The audit answer to a question that matters — this is the tool that says "
    "whether the screen is leaving the room. It reports the destinations but "
    "not how they are doing, so a stalled push looks like a healthy one.")

# --- persistent shells --------------------------------------------------------

review(
    "shell_open",
    "Started a shell on a real PTY, sized in rows and columns.", 5,
    "A pseudo-terminal rather than a pipe, which is the difference between a "
    "shell that behaves and one that turns off its prompt, its colours and its "
    "line editing because it thinks nobody is watching. Interactive programs "
    "work here for the same reason.")
review(
    "shell_exec",
    "Ran a command in an open session and waited for the output to go quiet.", 4,
    "A quiet period is the honest way to know an interactive shell has finished "
    "when there is no exit status to read — and it is still a heuristic, so a "
    "command that pauses mid-output looks finished. Echoing a sentinel with $? "
    "would turn the guess into a fact, the same fix terminal_run needs.")
review(
    "shell_input", "Sent raw keystrokes to a session without waiting.", 4,
    "Necessary for anything shell_exec cannot express — answering a prompt, "
    "sending Ctrl-C, driving a full-screen program. Fire-and-forget is the "
    "point, and it means the caller has to pair it with shell_read themselves.")
review(
    "shell_read", "Read and cleared everything the session produced since the last read.", 4,
    "Read-and-clear is the right contract for polling a long command: nothing "
    "is delivered twice. It also means one caller's read hides that output from "
    "another, which matters now that several sub-agents can share a desktop.")
review(
    "shell_list", "Listed the open sessions with their age and pending bytes.", 4,
    "Reporting pending bytes is what makes it useful rather than decorative — a "
    "session with unread output is one somebody should read. It cannot say what "
    "is running in each.")
review(
    "shell_close", "Ended a session and released its PTY.", 4,
    "Explicit cleanup, which matters because a PTY and a shell process outlive "
    "the MCP connection that made them. Sessions have no idle timeout, so a "
    "forgotten one lives until the desktop restarts.")

# --- SSH ----------------------------------------------------------------------

review(
    "ssh_connect",
    "Opened a session with golang.org/x/crypto/ssh — the protocol in Go, not "
    "the ssh command driven from outside.", 5,
    "This is the difference between holding a connection and re-establishing "
    "one per call, and it is why exec, sftp and tunnels can share a session. "
    "Host key handling is the thing to look at before trusting it outside a "
    "container: convenience there is where SSH tooling usually goes wrong.")
review(
    "ssh_exec", "Ran a command over the open session, returning stdout, stderr and exit code.", 5,
    "A channel on an existing connection, so it costs a round trip rather than "
    "a handshake, and the exit code is the protocol's own rather than something "
    "parsed back. Nothing to improve at this level.")
review(
    "ssh_upload", "Sent a file over SFTP on the same connection.", 5,
    "SFTP through pkg/sftp rather than shelling out to scp: no second "
    "authentication, no quoting a remote path through a shell, and errors that "
    "name the operation. It reads the local file into memory, which is fine for "
    "what an agent moves and not for an image.")
review(
    "ssh_download", "Fetched a file over the same SFTP session.", 5,
    "Same reasoning and the same memory caveat.")
review(
    "ssh_list_remote", "Listed a remote directory over SFTP.", 5,
    "Structured entries from the protocol rather than parsed ls output, which "
    "is exactly the trap this avoids — ls output is for people.")
review(
    "ssh_tunnel_local", "Forwarded a local port to the remote side over the session.", 5,
    "A real forwarded channel, managed and closable, not a backgrounded ssh -L "
    "nobody can find later. The tunnel belongs to the session and dies with it.")
review(
    "ssh_tunnel_remote", "Forwarded a remote port back to this side.", 5,
    "The harder direction, and it works the same way. Whether the remote sshd "
    "allows it is the server's decision, and the error says so.")
review(
    "ssh_tunnels", "Listed the tunnels on a session, with their connection counts.", 4,
    "Connection counts make it an operational answer rather than an inventory. "
    "It cannot say whether a tunnel is failing, only whether anything has used "
    "it.")
review(
    "ssh_tunnel_close", "Closed one tunnel by id.", 4,
    "Right granularity — the session survives. Existing connections through it "
    "are cut without a way to drain them first, which is the correct default "
    "and worth documenting.")
review(
    "ssh_list", "Listed the open SSH sessions.", 4,
    "The inventory that makes the id-based tools usable after a restart of the "
    "client. Like shell_list it says nothing about health.")
review(
    "ssh_disconnect", "Closed a session and everything on it.", 4,
    "Explicit teardown, and the tunnels going with it is the right coupling.")
review(
    "ssh_keygen", "Generated a key pair by running ssh-keygen.", 3,
    "crypto/ed25519 and x/crypto/ssh can generate and marshal a key without "
    "leaving the process, which would also let it refuse to overwrite without "
    "parsing a prompt. It already declines to overwrite an existing key, which "
    "is the important part.")
review(
    "ssh_copy_id", "Appended the public key to the remote authorized_keys.", 3,
    "Builds a shell command and runs it remotely, so it depends on the remote "
    "having a POSIX shell and on the quoting surviving. Writing the file over "
    "SFTP — read, append, write, chmod — uses the connection this tool already "
    "holds and works on hosts with an unusual shell.")

# --- packages and services ----------------------------------------------------

review(
    "sudo_status", "Reported whether passwordless sudo is available in this image.", 4,
    "The right thing to ask before offering an agent a privileged path, and "
    "cheap. It answers about the capability rather than about a specific "
    "command, so a narrowly configured sudoers looks the same as a full one.")
review(
    "install_packages",
    "Installed with apt under a deadline, reporting the command's own output as "
    "progress and killing it on cancel.", 4,
    "Everything the long-running path should be, and the progress it streams is "
    "apt's own text rather than a spinner. It cannot roll back a partial "
    "install, which is what snapshot_create is for — worth saying so in the "
    "description, since the two belong together.")
review(
    "remove_packages", "Removed packages, with purge as an option.", 4,
    "Purge as an explicit choice rather than a default is right: configuration "
    "is the part people miss. It does not report what else apt would remove as "
    "a consequence, which is the number that matters before saying yes.")
review(
    "search_packages", "Searched apt without installing anything.", 3,
    "Correctly read-only, which is why it survives MCP_POLICY=readonly. It "
    "parses apt's human-facing output, and apt says plainly that its CLI has no "
    "stable interface between versions. python-apt or the dpkg database would "
    "not move under it.")
review(
    "service_control",
    "Asked supervisord about the desktop's programs, and can start, stop or "
    "restart them.", 4,
    "Talking to the supervisor that actually owns these processes is right, and "
    "it recognises both the container and native configuration paths. Stopping "
    "the wrong program takes the desktop out from under everyone, and the tool "
    "does not distinguish the ones that can be safely bounced from the ones "
    "that cannot.")

# --- system -------------------------------------------------------------------

review(
    "set_resolution",
    "Changed the mode with xrandr, within the size reserved when the display "
    "started.", 4,
    "Changing resolution without restarting anything is genuinely useful, and "
    "the ceiling is honest — Xvfb reserves its framebuffer at start, so growing "
    "past it is not something this could fix. Reporting the available modes "
    "would let a caller pick rather than guess and be refused.")

# --- snapshots ----------------------------------------------------------------

review(
    "snapshot_create",
    "Tarred the home directory and recorded the installed package list, "
    "excluding the snapshot directory so they do not nest, and refusing a "
    "result too small to be real.", 3,
    "The two checks show someone thought about how this fails: excluding itself "
    "stops quadratic growth, and the size check catches a tar that packed "
    "nothing. But it copies the whole home every time — no incremental, no "
    "deduplication — so the second snapshot costs as much as the first. It also "
    "runs while files are being written, so a database in the home is captured "
    "mid-write.")
review(
    "snapshot_list", "Listed the snapshots with their size and date.", 4,
    "Enough to choose one. It does not show what a restore would change, which "
    "is the question somebody actually has before restoring.")
review(
    "snapshot_restore",
    "Unpacked a snapshot over the home and reported which packages were "
    "installed after it was taken.", 3,
    "Reporting the package difference rather than silently reverting it is the "
    "good part — packages and files are different kinds of state and it does "
    "not pretend otherwise. Unpacking over the live home leaves anything "
    "created since in place, so a restore is a merge rather than the rollback "
    "the name suggests. Saying which files it will overwrite, first, would make "
    "it something a person can agree to.")
review(
    "snapshot_delete", "Deleted a snapshot and its package list.", 4,
    "Removes both halves, which is the failure to avoid — a package list "
    "without its tar is worse than nothing. No confirmation, correctly: that "
    "belongs to whoever is calling.")

review(
    "list_windows",
    "Listed every window with id, desktop, geometry, class and title, read straight from _NET_CLIENT_LIST and each window's own properties.",
    5,
    'It used to shell out to wmctrl and split the output on whitespace, so a window called "Report  2026" — two spaces — parsed as a different window with a different geometry. internal/desktop/ewmh.go reads the properties X already holds: no subprocess, no locale, no column arithmetic. The one thing left is telling a caller when the window manager publishes no client list at all, which is a different failure from an empty desktop.',
)

review(
    "list_desktops",
    'Listed the virtual desktops and marked the current one, from _NET_NUMBER_OF_DESKTOPS, _NET_CURRENT_DESKTOP and _NET_DESKTOP_NAMES.',
    5,
    'This was not merely inelegant before, it was wrong. The old parser took every field from index 8 onward as the name, so each desktop came back called "1920x1044 desktop 1" with the work-area size glued to the front — for as long as the tool had existed, because nobody read the output closely. Reading the names property gives the name.',
)

review(
    "get_active_window",
    'Read _NET_ACTIVE_WINDOW and described that window: id, geometry, class and title as fields.',
    5,
    'One property read where this used to be three xdotool processes returning a paragraph of text to parse. Nothing focused is now an answer with a note rather than an error, so a caller can tell an idle desktop from a broken query. Coordinates are translated to the root, so they are the ones a click can use even under a reparenting window manager.',
)

review(
    "set_clipboard",
    'Wrote text to the X CLIPBOARD selection and reported whether the write actually happened.',
    4,
    "It used to discard the result, so a failed write was reported as success and the agent went on to paste something that was never there. Getting the fix right took three attempts: capturing stderr made Go create a pipe that xclip's daemonised child inherited and never closed, so it hung for sixty seconds; adding WaitDelay fixed that and made a successful write report as broken, because ErrWaitDelay is the child holding the pipe rather than a failed command. Still a subprocess per write, and still text only — owning the selection from inside the daemon is what would take it to five.",
)


# --- revised after the window tools were rewritten against X -------------------
#
# review() assigns into a dict, so an entry here replaces the one above. Kept as
# an addition rather than an edit in place: the earlier text described a real
# version of these tools, and the two together say what changed and why. When
# the older half stops being interesting, delete it — do not silently overwrite
# the record of what the code used to be.

review(
    'activate_window',
    'Focused and raised a window with a _NET_ACTIVE_WINDOW client message.',
    5,
    'Asking the window manager rather than raising the window behind its back, which is what makes it work under a manager that reparents and decorates. The message carries a source indication, so focus-stealing rules treat an agent the way they treat anything the user asked for.',
)

review(
    'move_window',
    'Moved a window with _NET_MOVERESIZE_WINDOW, marking only the fields being set.',
    5,
    "The flags are what makes 'move without resizing' expressible: the old path built a geometry string and passed -1 sentinels the manager then had to ignore. Verified against a real desktop — a move followed by a resize keeps the position it was given.",
)

review(
    'resize_window',
    'Resized through the same message, leaving position alone.',
    5,
    'Shares MoveResize with move_window, which is right: one call that says which fields it means beats two that each pretend to set everything.',
)

review(
    'close_window',
    "Asked the window to close with _NET_CLOSE_WINDOW — the titlebar button's own request.",
    4,
    "The polite close: the application gets to save, object or put up a dialog. It still cannot report whether the window actually went, because it is a request and the answer arrives later. Waiting briefly and re-reading the client list would turn 'asked' into 'closed'.",
)

review(
    'minimize_window',
    'Iconified with a WM_CHANGE_STATE message carrying IconicState.',
    5,
    'The correct request, and the subtle part: _NET_WM_STATE_HIDDEN is what a manager SETS to report a window is minimised, not something a client asks for. Using it would have been the plausible wrong answer. This also removed the last xdotool dependency from the window family.',
)

review(
    'maximize_window',
    'Added both maximised states in one _NET_WM_STATE message.',
    5,
    'Two states per message is what the protocol allows and what this case needs — one change the manager can act on rather than two it has to reconcile. Restore returned the window to the exact geometry it had before, because the manager remembers and this asked rather than resized.',
)

review(
    'restore_window',
    'Removed both maximised states.',
    5,
    'The proper inverse, and it does not try to remember a geometry the window manager already has.',
)

review(
    'fullscreen_window',
    'Set, cleared or toggled _NET_WM_STATE_FULLSCREEN, with action defaulting to toggle.',
    5,
    'It only toggled before, so an agent that wanted a window full screen had to read the state and guess which way a toggle would go. Naming the action fixes that without changing what an existing caller gets.',
)

review(
    'set_window_desktop',
    'Moved a window to a virtual desktop with _NET_WM_DESKTOP.',
    5,
    'Native, and the indices line up with what list_desktops reports because both read the same properties now.',
)

review(
    'switch_desktop',
    'Switched the current desktop with _NET_CURRENT_DESKTOP.',
    4,
    'Native. What is still missing is switching by name — list_desktops reads _NET_DESKTOP_NAMES already, so the names exist and only this end cannot take one.',
)
