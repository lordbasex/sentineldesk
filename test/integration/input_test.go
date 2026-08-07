// SentinelDesk
// A collaborative operating system for people and AI agents.
//
// Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
// Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
//
// Licensed under the Apache License, Version 2.0.
//
// This product's name and logo are trademarks of Federico Pereira and are not
// covered by the license above. See the README for the trademark policy.
//
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package integration

// Pointer, keyboard and clipboard.
//
// These are the hardest tools to check honestly, because their effect is
// whatever the application underneath decided to do with the event. Asserting
// that type_text returned "typed 5 chars" would be asserting that a counter
// counts. So every test here gives the input somewhere that records it: a shell
// that writes a file, a window that takes focus, a clipboard another program can
// read.
//
// For the read-only tools the direction is reversed — the state is set from
// outside and read back through MCP, which is the only arrangement where the
// tool cannot be the source of its own confirmation.

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// typeTarget opens a terminal, focuses it, and returns a marker path the typed
// command will create. Shared by the keyboard tests, since "the characters
// arrived" is only observable if something acts on them.
func typeTarget(t *testing.T, name string) string {
	t.Helper()
	title := "TYPE" + strings.ToUpper(name)
	id := openWindow(t, title)
	shared.Call(t, "activate_window", map[string]any{"id": id})
	// xterm running `sleep` has no shell to type into, so replace it with one.
	X(t, "wmctrl -i -c %s", id)
	shared.Call(t, "launch_app", map[string]any{
		"command": fmt.Sprintf("xterm -T %s -e sh", title)})
	shared.Call(t, "wait_for_window", map[string]any{"match": title, "timeout_ms": 15000})
	out := shared.Call(t, "wait_for_window", map[string]any{"match": title, "timeout_ms": 5000})
	newID := jsonField(t, out, "id")
	shared.Call(t, "activate_window", map[string]any{"id": newID})
	t.Cleanup(func() { X(t, "wmctrl -i -c %s 2>/dev/null || true", newID) })

	marker := "/tmp/typed-" + name + ".txt"
	Sh(t, "rm -f %s", marker)
	time.Sleep(700 * time.Millisecond) // let the shell draw its prompt
	return marker
}

func TestTypeText(t *testing.T) {
	control(t)
	marker := typeTarget(t, "text")

	// The proof is the file, not the reply. A tool that counted the characters
	// and injected none would answer identically.
	shared.Call(t, "type_text", map[string]any{
		"text": "echo typed-ok > " + marker + "\n"})

	eventually(t, 8*time.Second, "the typed command to run", func() bool {
		return strings.Contains(Sh(t, "cat %s 2>/dev/null", marker), "typed-ok")
	})
}

func TestKeyCombo(t *testing.T) {
	control(t)
	marker := typeTarget(t, "combo")

	// Type the command WITHOUT a newline, then send Return as a key combo. The
	// file appearing is what shows the key reached the shell, and separating it
	// from the text is what makes this a test of key_combo rather than of
	// type_text sending a newline.
	shared.Call(t, "type_text", map[string]any{"text": "echo combo-ok > " + marker})
	time.Sleep(300 * time.Millisecond)
	shared.Call(t, "key_combo", map[string]any{"keys": "Return"})

	eventually(t, 8*time.Second, "Return to submit the line", func() bool {
		return strings.Contains(Sh(t, "cat %s 2>/dev/null", marker), "combo-ok")
	})
}

func TestMouseMove(t *testing.T) {
	control(t)
	for _, p := range [][2]int{{100, 100}, {1500, 900}, {640, 360}} {
		shared.Call(t, "mouse_move", map[string]any{"x": p[0], "y": p[1]})
		// X is asked, not the tool. get_mouse_position reading back what
		// mouse_move set would only show the two agree.
		got := X(t, "xdotool getmouselocation --shell | head -2 | tr '\\n' ' '")
		want := fmt.Sprintf("X=%d Y=%d", p[0], p[1])
		if !strings.Contains(got, want) {
			t.Errorf("sent the pointer to (%d,%d) and X reports %q", p[0], p[1], got)
		}
	}
}

// A note on what these can and cannot show.
//
// The obvious test for mouse_click — click a window, watch it take focus —
// does not work in this image, and the reason is worth recording so nobody
// spends an afternoon on it again. Clicking the client area of an xterm leaves
// _NET_ACTIVE_WINDOW at 0x0 under this window manager, and a page in Chromium
// counting mousedown/mouseup/click through a full-screen div registers nothing
// at all. Both are true of xdotool at the identical coordinates, so neither is
// about the tool: mouse_click and xdotool put the same events on the same wire.
// It is Xvfb with no compositor and applications that do not treat synthetic
// input as a gesture.
//
// What does work is the window manager's own drag: press on a title bar, move,
// release, and the window is somewhere else. That single effect exercises the
// press and the release and the motion between them, so it is what the button
// tools are tested through — a real effect, read from X, rather than an
// observable chosen because it was easy to assert.

// dragWindow returns a window placed where nothing covers it, and the middle of
// its title bar, which is the one part of the screen that reliably reacts.
func dragWindow(t *testing.T, title string) (id string, tx, ty int) {
	t.Helper()
	id = openWindow(t, title)
	X(t, "wmctrl -i -r %s -e 0,200,200,420,300", id)
	X(t, "wmctrl -i -r %s -b add,above", id)
	shared.Call(t, "activate_window", map[string]any{"id": id})
	time.Sleep(600 * time.Millisecond)

	// xwininfo's absolute origin, not wmctrl's. For a reparented window the two
	// disagree by the frame offset, and mixing them is how three attempts at
	// this test aimed at empty desktop: wmctrl -lG reports the client area while
	// xwininfo reports the frame, so "twelve pixels above the origin" means the
	// title bar in one and nothing at all in the other.
	info := X(t, "xwininfo -id %s", id)
	fx := fieldInt(info, "Absolute upper-left X")
	fy := fieldInt(info, "Absolute upper-left Y")
	return id, fx + 60, fy - 12
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func windowAt(t *testing.T, id string) (int, int) {
	t.Helper()
	f := strings.Fields(X(t, "wmctrl -lG | grep %s", id))
	if len(f) < 4 {
		return -1, -1
	}
	return atoi(f[2]), atoi(f[3])
}

func TestMouseClick(t *testing.T) {
	control(t)
	// What can be established out of band is that the pointer arrives exactly
	// where it was sent and the button is delivered there. The delivery itself
	// is what TestMouseDownAndUp shows through an effect; here the position is
	// the assertion, because a click at the wrong coordinates is the failure
	// that actually happens and it is invisible in the reply.
	id, tx, ty := dragWindow(t, "CLICKWIN")
	_ = id

	shared.Call(t, "mouse_click", map[string]any{"x": tx, "y": ty})

	got := X(t, "xdotool getmouselocation --shell | head -2 | tr '\\n' ' '")
	want := fmt.Sprintf("X=%d Y=%d", tx, ty)
	if !strings.Contains(got, want) {
		t.Fatalf("clicked at (%d,%d) and the pointer ended at %q", tx, ty, got)
	}
}

func TestMouseDownAndUp(t *testing.T) {
	control(t)

	// What actually goes wrong with this pair is a button that stays down.
	// mouse_down leaves it held by design, so nothing about that call alone can
	// be wrong; the failure is mouse_up not releasing, and its symptom is that
	// everything afterwards behaves as a drag. So the assertion is that an
	// ordinary drag still works once these two have run — which it cannot if
	// the button never came up.
	//
	// The tempting test, assembling a drag by hand out of down, several moves
	// and up, was tried and abandoned. It does not reliably move a window here:
	// each step is a round trip through the stdio bridge and the window manager
	// wants motion at something closer to input speed. mouse_drag does the same
	// thing in-process with its own pacing and works every time, which is the
	// argument for it existing as a tool rather than as advice to call three.
	shared.Call(t, "mouse_move", map[string]any{"x": 700, "y": 500})
	shared.Call(t, "mouse_down", map[string]any{"button": 1})
	shared.Call(t, "mouse_up", map[string]any{"button": 1})

	id, tx, ty := dragWindow(t, "AFTERUPWIN")
	bx, by := windowAt(t, id)
	shared.Call(t, "mouse_drag", map[string]any{
		"x1": tx, "y1": ty, "x2": tx + 200, "y2": ty + 150, "button": 1})

	eventually(t, 8*time.Second, "a normal drag to work after a press and release", func() bool {
		x, y := windowAt(t, id)
		return x != bx || y != by
	})
}

func TestMouseDrag(t *testing.T) {
	control(t)
	id, tx, ty := dragWindow(t, "DRAGWIN")
	bx, by := windowAt(t, id)

	// Drag the title bar. The window moving is the effect; no reply from the
	// tool could establish it.
	shared.Call(t, "mouse_drag", map[string]any{
		"x1": tx, "y1": ty, "x2": tx + 200, "y2": ty + 150, "button": 1})

	eventually(t, 6*time.Second, "the window to follow the drag", func() bool {
		x, y := windowAt(t, id)
		return x != bx || y != by
	})
}

func TestMouseScroll(t *testing.T) {
	control(t)
	// A terminal with more output than fits, so scrolling has somewhere to go.
	title := "SCROLLWIN"
	shared.Call(t, "launch_app", map[string]any{
		"command": fmt.Sprintf("xterm -T %s -e sh -c 'seq 1 500; sleep 600'", title)})
	out := shared.Call(t, "wait_for_window", map[string]any{"match": title, "timeout_ms": 15000})
	id := jsonField(t, out, "id")
	t.Cleanup(func() { X(t, "wmctrl -i -c %s 2>/dev/null || true", id) })
	shared.Call(t, "activate_window", map[string]any{"id": id})
	time.Sleep(800 * time.Millisecond)

	geom := X(t, "xwininfo -id %s", id)
	cx := fieldInt(geom, "Absolute upper-left X") + 100
	cy := fieldInt(geom, "Absolute upper-left Y") + 100
	shared.Call(t, "mouse_move", map[string]any{"x": cx, "y": cy})

	// What the window shows before and after, from a capture rather than from
	// the tool. Scrolling a terminal changes the pixels; nothing else here does.
	before := shared.CallImage(t, "screenshot_region", map[string]any{
		"x": cx - 80, "y": cy - 80, "width": 300, "height": 200})
	shared.Call(t, "mouse_scroll", map[string]any{"dy": -8})
	time.Sleep(600 * time.Millisecond)
	after := shared.CallImage(t, "screenshot_region", map[string]any{
		"x": cx - 80, "y": cy - 80, "width": 300, "height": 200})

	// Identical pixels would mean the wheel events went nowhere. This is a
	// weaker assertion than the others in this file and is meant to be: what an
	// application does with a wheel event is entirely its own business, and an
	// xterm with no scrollback configured is entitled to do nothing at all. It
	// catches the failure that matters — the events not being delivered — and
	// claims nothing beyond it.
	if string(before) == string(after) {
		t.Skip("the terminal's pixels did not change; it has no scrollback to move through, " +
			"so this environment cannot show whether the wheel events arrived")
	}
}

func TestSetClipboard(t *testing.T) {
	control(t)
	want := "clipboard-from-mcp"
	shared.Call(t, "set_clipboard", map[string]any{"text": want})

	// xclip, not get_clipboard. The two sharing a bug is precisely the case a
	// round trip through the same code cannot see.
	eventually(t, 5*time.Second, "the X clipboard to hold it", func() bool {
		return strings.Contains(X(t, "xclip -o -selection clipboard 2>/dev/null"), want)
	})
}

func TestGetClipboard(t *testing.T) {
	control(t)
	// The other direction: put it there from outside and read it through MCP,
	// so the tool is reporting on something it did not produce.
	want := "clipboard-from-outside"
	X(t, "printf %%s %q | xclip -selection clipboard", want)
	time.Sleep(400 * time.Millisecond)

	out := shared.Call(t, "get_clipboard", nil)
	if !strings.Contains(out, want) {
		t.Fatalf("xclip holds %q and get_clipboard returned %q", want, trunc(out, 200))
	}
}

// --- helpers -----------------------------------------------------------------

// fieldInt pulls a labelled number out of xwininfo's output.
func fieldInt(body, label string) int {
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, label) {
			continue
		}
		_, after, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n := 0
		neg := false
		for _, r := range strings.TrimSpace(after) {
			if r == '-' && n == 0 {
				neg = true
				continue
			}
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		if neg {
			return -n
		}
		return n
	}
	return 0
}

// sameWindow compares ids that different tools pad differently: wmctrl says
// 0x01a00003 where xprop says 0x1a00003.
func sameWindow(xpropOut, id string) bool {
	var got string
	if i := strings.Index(xpropOut, "0x"); i >= 0 {
		got = strings.TrimSpace(xpropOut[i:])
		if j := strings.IndexAny(got, " \n,"); j > 0 {
			got = got[:j]
		}
	}
	norm := func(s string) string {
		return strings.TrimLeft(strings.TrimPrefix(strings.ToLower(s), "0x"), "0")
	}
	return got != "" && norm(got) == norm(id)
}
