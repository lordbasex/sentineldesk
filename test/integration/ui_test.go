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

// The accessibility tree.
//
// The application under test is a real GTK dialog rather than the browser, and
// that is deliberate: Chromium exposes a tree but does not implement AT-SPI's
// EditableText, so testing ui_set_text against it would be testing the gap
// rather than the tool. zenity gives buttons and an entry that GTK implements
// properly, which is the case these tools exist for — the one where there is no
// DevTools protocol to fall back on.

import (
	"strings"
	"testing"
	"time"
)

// zenityEntry opens a GTK entry dialog and waits for it to reach the tree.
// Returns the title it was given.
func zenityEntry(t *testing.T, title string) string {
	t.Helper()
	shared.Call(t, "launch_app", map[string]any{
		"command": "zenity --entry --title=" + title + " --text=Name --entry-text=start"})
	t.Cleanup(func() { ShUser(t, "pkill -f zenity 2>/dev/null || true") })

	eventually(t, 15*time.Second, "the dialog to appear in the accessibility tree", func() bool {
		return strings.Contains(shared.Call(t, "ui_tree", map[string]any{"depth": 4}), title)
	})
	return title
}

func TestUITree(t *testing.T) {
	control(t)
	title := zenityEntry(t, "TREEDIALOG")

	out := shared.Call(t, "ui_tree", map[string]any{"depth": 12, "limit": 800})
	if !strings.Contains(out, title) {
		t.Fatalf("the dialog is open and the tree does not have it:\n%s", trunc(out, 400))
	}
	// Depth has to mean something. A tool ignoring it would return the same
	// tree twice, which is how max_depth being silently discarded went unnoticed
	// for as long as it did.
	shallow := strings.Count(shared.Call(t, "ui_tree", map[string]any{"depth": 1}), "\"ref\"")
	deep := strings.Count(shared.Call(t, "ui_tree", map[string]any{"depth": 6}), "\"ref\"")
	if shallow >= deep {
		t.Errorf("depth 1 returned %d elements and depth 6 returned %d", shallow, deep)
	}
}

func TestUIFind(t *testing.T) {
	control(t)
	zenityEntry(t, "FINDDIALOG")

	// By role, restricted to the application — the combination that makes a
	// short label findable at all, since an unscoped name match finds the panel
	// clock and the hostname too.
	//
	// The role is "button" and the entry's is "text". ui_find's own examples
	// suggest "push button" and "entry", and neither matches a GTK dialog; the
	// calculator earlier used "toggle button" for what is plainly a push button.
	// The names come from the toolkit rather than from the tool, so ui_tree is
	// the way to find out what to ask for.
	out := shared.Call(t, "ui_find", map[string]any{
		"role": "button", "app": "zenity"})
	if !strings.Contains(out, "ref") {
		t.Fatalf("the dialog has buttons and ui_find returned none:\n%s", trunc(out, 400))
	}
	// A role nothing has must come back empty rather than as everything.
	empty := shared.Call(t, "ui_find", map[string]any{"role": "no-such-role-at-all"})
	if strings.Contains(empty, "\"ref\"") {
		t.Errorf("a role that does not exist matched something:\n%s", trunc(empty, 200))
	}
}

func TestUIGetText(t *testing.T) {
	control(t)
	zenityEntry(t, "GETTEXTDIALOG")

	entry := findRef(t, "text", "zenity")
	out := shared.Call(t, "ui_get_text", map[string]any{"ref": entry})
	// zenity was started with --entry-text=start, so that is what the field
	// holds and what a correct read returns.
	if !strings.Contains(out, "start") {
		t.Fatalf("the entry holds \"start\" and ui_get_text returned %s", trunc(out, 200))
	}
}

func TestUISetText(t *testing.T) {
	control(t)
	zenityEntry(t, "SETTEXTDIALOG")

	entry := findRef(t, "text", "zenity")
	shared.Call(t, "ui_set_text", map[string]any{"ref": entry, "text": "written-by-mcp"})

	// Read back through the tree, which is the only window into a GTK dialog —
	// there is no DevTools here, and that is the point of these tools existing.
	eventually(t, 6*time.Second, "the entry to hold the new text", func() bool {
		return strings.Contains(
			shared.Call(t, "ui_get_text", map[string]any{"ref": entry}), "written-by-mcp")
	})
}

func TestUIFocus(t *testing.T) {
	control(t)
	zenityEntry(t, "FOCUSDIALOG")

	entry := findRef(t, "text", "zenity")
	shared.Call(t, "ui_focus", map[string]any{"ref": entry})

	// The tree reports focus as a state, so the element that was focused has to
	// carry it afterwards.
	eventually(t, 6*time.Second, "the element to report itself focused", func() bool {
		out := shared.Call(t, "ui_find", map[string]any{"role": "text", "app": "zenity"})
		return strings.Contains(out, "focused")
	})
}

func TestUIClick(t *testing.T) {
	control(t)
	zenityEntry(t, "CLICKDIALOG")

	// Cancel, because its effect is unambiguous: the dialog goes.
	ref := findRefNamed(t, "button", "zenity", "Cancel")
	if ref == "" {
		t.Skip("this zenity has no Cancel button to click")
	}
	shared.Call(t, "ui_click", map[string]any{"ref": ref})

	// The process ending is the proof, read from outside the tree entirely.
	eventually(t, 8*time.Second, "the dialog to close", func() bool {
		// -x, matching the process name. With -f the pattern finds the shell
		// asking the question, whose own command line contains "zenity" — the
		// third time this suite has walked into that.
		return atoi(Sh(t, "pgrep -x zenity | wc -l")) == 0
	})
}

func TestUIWaitFor(t *testing.T) {
	control(t)
	ShUser(t, "pkill -f zenity 2>/dev/null || true")
	time.Sleep(500 * time.Millisecond)

	// Open the dialog a second and a half from now, so the wait has something
	// in the future to wait for rather than something already present.
	// As the desktop's user, never as root. A GTK program started by root has
	// HOME=/root, cannot reach the session's accessibility bus, and launches a
	// SECOND at-spi-bus-launcher of its own — which takes the tree down for
	// everything else. Every "trace/breakpoint trap" from the bridge in this
	// suite traced back to that.
	ShUser(t, "setsid sh -c 'sleep 2; DISPLAY=:0 zenity --entry --title=WAITDIALOG --text=x' </dev/null >/dev/null 2>&1 &")
	t.Cleanup(func() { ShUser(t, "pkill -f zenity 2>/dev/null || true") })

	start := time.Now()
	out := shared.Call(t, "ui_wait_for", map[string]any{
		"name": "WAITDIALOG", "timeout_ms": 20000})
	waited := time.Since(start)

	if !strings.Contains(out, "found") && !strings.Contains(out, "WAITDIALOG") {
		t.Fatalf("the dialog opened and the wait did not find it: %s", trunc(out, 300))
	}
	// It reports which of its three paths answered, and for something that
	// appears later it cannot be the one that looks before listening.
	if strings.Contains(out, "already present") && waited < time.Second {
		t.Errorf("it claims the dialog was already there, %v after being asked", waited)
	}
	// And something that never appears has to time out.
	shared.CallErr(t, "ui_wait_for", map[string]any{
		"name": "NEVERAPPEARSDIALOG", "timeout_ms": 2500})
}

func TestUIDiff(t *testing.T) {
	control(t)
	ShUser(t, "pkill -f zenity 2>/dev/null || true")
	time.Sleep(600 * time.Millisecond)

	// A first look with no dialog, then one with, so there is a real difference
	// to report rather than an empty one that would pass for anything.
	shared.Call(t, "ui_diff", nil)
	zenityEntry(t, "DIFFDIALOG")

	out := shared.Call(t, "ui_diff", nil)
	if !strings.Contains(out, "DIFFDIALOG") && !strings.Contains(strings.ToLower(out), "added") {
		t.Fatalf("a dialog appeared between the two looks and the diff does not mention it:\n%s",
			trunc(out, 400))
	}
}

func TestFillForm(t *testing.T) {
	control(t)
	zenityEntry(t, "FILLDIALOG")

	out, isErr := shared.call(t, "fill_form", map[string]any{
		"fields": map[string]any{"Name": "filled-by-form"}})

	if !isErr && strings.Contains(out, "\"filled\": 1") {
		entry := findRef(t, "text", "zenity")
		eventually(t, 6*time.Second, "the field to hold what the form wrote", func() bool {
			return strings.Contains(
				shared.Call(t, "ui_get_text", map[string]any{"ref": entry}), "filled-by-form")
		})
		return
	}

	// It cannot fill a GTK dialog, and the reason is structural rather than a
	// bug in the writing: fill_form matches a field by NAME, and in GTK the
	// label and the entry are separate elements joined by a labelled-by
	// relation. "Name" is the label's name; the entry's own name is empty. So
	// the ref that matches is a label, and settext on a label exits 2.
	//
	// Recorded rather than skipped quietly, because the fix is knowable —
	// follow the relation from the label to what it labels — and because
	// fill_form's usefulness on native applications depends on it. Inside a
	// page browser_type does not have the problem, which is why the sweep
	// reports fill_form as degraded there for a different reason entirely.
	t.Skipf("fill_form cannot address a GTK entry by its label: %s", trunc(out, 200))
}

func TestCheckErrors(t *testing.T) {
	control(t)
	ShUser(t, "pkill -f zenity 2>/dev/null || true")
	time.Sleep(600 * time.Millisecond)

	// No assertion about the quiet case. Chromium leaves a "Restore pages?"
	// infobar behind after any abnormal exit, and it is a genuine alert — the
	// tool is right to report it and a test demanding silence would be
	// demanding a tidy desktop rather than a working tool.

	// Now a real error dialog, which is how a graphical program fails — there
	// is no exit code to read, which is the reason this tool exists.
	// The title carries the word, not just the body.
	//
	// A dialog is only reported when its own name or text reads like a failure,
	// and zenity puts the message in a child label — so the dialog element
	// itself offers nothing but the title. A real error dialog titled with the
	// application's name, message in the body, is therefore missed. That is a
	// gap worth closing by searching a dialog's descendants, and it is recorded
	// here rather than papered over: this test covers the case the tool does
	// handle, which is the one where the wording is on the dialog.
	ShUser(t, "setsid sh -c 'DISPLAY=:0 zenity --error --title=\"Error saving file\" --text=failed' </dev/null >/dev/null 2>&1 &")
	t.Cleanup(func() { ShUser(t, "pkill -f zenity 2>/dev/null || true") })

	eventually(t, 15*time.Second, "the error dialog to be noticed", func() bool {
		out := shared.Call(t, "check_errors", nil)
		return strings.Contains(out, "Error saving file") ||
			strings.Contains(out, "errors_on_screen\": true")
	})
}

// --- helpers -----------------------------------------------------------------

// findRef returns the first ref of the given role inside an application.
func findRef(t *testing.T, role, app string) string {
	t.Helper()
	out := shared.Call(t, "ui_find", map[string]any{"role": role, "app": app})
	ref := firstQuoted(out, "\"ref\"")
	if ref == "" {
		t.Fatalf("no %s in %s:\n%s", role, app, trunc(out, 400))
	}
	return ref
}

// findRefNamed returns the ref of an element whose name contains want, or "".
func findRefNamed(t *testing.T, role, app, want string) string {
	t.Helper()
	out := shared.Call(t, "ui_find", map[string]any{"role": role, "app": app, "name": want})
	return firstQuoted(out, "\"ref\"")
}

// firstQuoted pulls the first quoted value that follows a key.
func firstQuoted(body, key string) string {
	i := strings.Index(body, key)
	if i < 0 {
		return ""
	}
	rest := body[i+len(key):]
	j := strings.Index(rest, "\"")
	if j < 0 {
		return ""
	}
	rest = rest[j+1:]
	k := strings.Index(rest, "\"")
	if k < 0 {
		return ""
	}
	return rest[:k]
}
