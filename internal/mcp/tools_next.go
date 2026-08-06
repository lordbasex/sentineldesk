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

package mcp

// Second-generation tools: resolution, smart waits, macro-actions, semantic
// screen diffing, restore points and the action log.
//
// The common idea is cutting round trips. Opening an app and waiting for it used
// to be four calls and a `wait` guessed by eye; seeing what changed on screen
// used to mean sending a whole screenshot every time. Here each of those is one
// call that also returns far less data.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/lordbasex/sentineldesk/internal/desktop"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// homeDir returns the desktop user's home. The path is derived rather than
// hard-coded: the username changed once already, and a fixed path failed
// silently — tarring a directory that no longer existed and producing an empty
// snapshot that restored nothing.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Clean(h)
	}
	return "/home/sentineldesk"
}

func snapshotDirPath() string { return filepath.Join(homeDir(), ".sentineldesk-snapshots") }

func (s *Server) buildNextTools() []toolDef {
	return []toolDef{
		{
			Name:        "set_resolution",
			Risk:        riskDanger,
			Description: "Change the desktop resolution WITHOUT restarting anything. Use a smaller one for vision tasks (screenshots above ~1280 wide lose detail when the model rescales them) and the full size for real work. It can only shrink below the size the X server reserved at boot; get_screen_info reports that maximum.",
			InputSchema: schema(map[string]any{
				"width": pInt("width in pixels"), "height": pInt("height in pixels"),
			}, "width", "height"),
		},
		{
			Name:        "wait_for_idle",
			Risk:        riskRead,
			Description: "Wait until the desktop actually settles: the screen stops changing AND the CPU calms down. Use this instead of guessing a `wait` after launching an app, loading a page or starting an install — it returns as soon as things are quiet, and reports why it stopped.",
			InputSchema: schema(map[string]any{
				"timeout_ms": pInt("give up after this long (default 15000)"),
				"quiet_ms":   pInt("how long the screen must stay still (default 1200)"),
				"max_cpu":    pInt("consider the CPU calm below this percent (default 40)"),
				"ignore_cpu": pBool("only look at the screen, not the CPU (default false)"),
			}),
		},
		{
			Name:        "open_app_and_wait",
			Risk:        riskDanger,
			Description: "Launch a program, wait for its window to appear, focus it and wait for it to finish drawing — all in ONE call instead of launch_app + wait_for_window + activate_window + wait. Returns the window that appeared.",
			InputSchema: schema(map[string]any{
				"command":    pStr("command line, e.g. 'lxterminal' or 'chromium https://example.com'"),
				"match":      pStr("window title or class to wait for (default: derived from the command)"),
				"timeout_ms": pInt("give up after this long (default 25000)"),
				"as_root":    pBool("launch with root privileges (default false)"),
			}, "command"),
		},
		{
			Name:            "fill_form",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Fill several fields of a dialog or form in one call, by accessibility name — no clicking or tabbing between them. Optionally press a button at the end. Far more reliable than typing blind.",
			InputSchema: schema(map[string]any{
				"fields": map[string]any{
					"type":                 "object",
					"description":          "field name -> value, e.g. {\"Username\":\"admin\",\"Password\":\"secret\"}",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"submit": pStr("name of the button to press afterwards, e.g. 'Sign in'"),
			}, "fields"),
		},
		{
			Name:        "ui_diff",
			Risk:        riskRead,
			Description: "Report ONLY what changed in the accessibility tree since the last call: widgets that appeared, vanished, or whose text or state changed. Use it instead of re-reading the whole tree (or taking a screenshot) after every action — the answer is a fraction of the size, so you can check the screen constantly instead of occasionally. The first call just records the baseline.",
			InputSchema: schema(map[string]any{
				"reset": pBool("discard the baseline and start over (default false)"),
			}),
		},
		{
			Name:        "action_log",
			Risk:        riskRead,
			Description: "Read the log of MCP calls made so far: time, tool, arguments, whether it succeeded and how long it took. While a recording is running each entry also carries its position inside the video, so the .mp4 is indexed by action and can be audited or replayed.",
			InputSchema: schema(map[string]any{
				"limit":  pInt("how many recent entries (default 50)"),
				"filter": pStr("only tools whose name contains this"),
				"clear":  pBool("empty the log after reading (default false)"),
			}),
		},
		{
			Name:        "snapshot_create",
			Risk:        riskWrite,
			Description: "Save a restore point of the desktop: the home directory plus the list of installed packages. Take one before anything risky — installing a driver, editing /etc, running an installer — so snapshot_restore can undo it.",
			InputSchema: schema(map[string]any{
				"name": pStr("short name, e.g. 'before-driver'"),
				"note": pStr("what you were about to do"),
			}, "name"),
		},
		{
			Name:        "snapshot_list",
			Risk:        riskRead,
			Description: "List the restore points with their size, date and note.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "snapshot_restore",
			Risk:        riskDanger,
			Description: "Roll the home directory back to a restore point. Reports which packages were installed after the snapshot so you can remove them too. Does NOT touch files outside the home.",
			InputSchema: schema(map[string]any{
				"name": pStr("snapshot name"),
			}, "name"),
		},
		{
			Name:        "snapshot_delete",
			Risk:        riskDanger,
			Description: "Delete a restore point.",
			InputSchema: schema(map[string]any{"name": pStr("snapshot name")}, "name"),
		},
	}
}

func (s *Server) dispatchNext(ctx context.Context, name string, args map[string]any) ([]map[string]any, bool, bool) {
	switch name {
	case "set_resolution":
		return s.toolSetResolution(ctx, args)
	case "wait_for_idle":
		return s.toolWaitForIdle(ctx, args)
	case "open_app_and_wait":
		return s.toolOpenAppAndWait(ctx, args)
	case "fill_form":
		return s.toolFillForm(ctx, args)
	case "ui_diff":
		return s.toolUIDiff(args)
	case "action_log":
		return s.toolActionLog(args)
	case "snapshot_create":
		return s.toolSnapshotCreate(ctx, args)
	case "snapshot_list":
		return s.toolSnapshotList()
	case "snapshot_restore":
		return s.toolSnapshotRestore(ctx, args)
	case "snapshot_delete":
		return s.toolSnapshotDelete(args)
	}
	return nil, false, false
}

// --- resolution --------------------------------------------------------------

func (s *Server) toolSetResolution(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	w, h := argInt(args, "width"), argInt(args, "height")
	if w < 320 || h < 240 {
		return textContent("invalid resolution: %dx%d", w, h), true, true
	}
	mode := fmt.Sprintf("%dx%d", w, h)

	// Order matters. Shrinking the framebuffer while the output still occupies
	// the old size gives BadValue, so move the output to the new mode first —
	// adding that mode if it does not exist — and only then resize the buffer.
	steps := []string{
		fmt.Sprintf("xrandr --newmode %s 0 %d 0 0 0 %d 0 0 0 2>/dev/null", mode, w, h),
		fmt.Sprintf("xrandr --addmode screen %s 2>/dev/null", mode),
		fmt.Sprintf("xrandr --output screen --mode %s 2>/dev/null", mode),
		fmt.Sprintf("xrandr --fb %s", mode),
	}
	out, _ := s.runElevated(ctx, strings.Join(steps, "; ")+"; xrandr --query | head -2", false, 15000)

	// Check what actually happened rather than trusting the exit code: xrandr
	// complains on stderr about things it went ahead and applied anyway.
	got, _ := s.output("sh", "-c", `xdpyinfo | awk '/dimensions:/{print $2}'`)
	got = strings.TrimSpace(got)
	if got != mode {
		return jsonContent(map[string]any{
			"applied": false, "requested": mode, "current": got,
			"hint":   "it can only shrink below the size reserved at start (DISPLAY_WIDTH/DISPLAY_HEIGHT)",
			"xrandr": strings.TrimSpace(fmt.Sprint(out["stdout"])),
		}), true, true
	}
	return jsonContent(map[string]any{"applied": true, "resolution": got}), false, true
}

// --- esperas -----------------------------------------------------------------

// screenFingerprint returns a hash of the screen. The capture is deliberately
// cheap: detecting that something changed does not need full fidelity.
func (s *Server) screenFingerprint() string {
	tmp := filepath.Join(os.TempDir(), "sentineldesk-idle.png")
	if err := desktop.GrabToFile(s.display, tmp, 0, 0, 0, 0); err != nil {
		return ""
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// cpuBusy returns CPU usage as a percentage, summed across all processes.
func (s *Server) cpuBusy() float64 {
	out, err := s.output("sh", "-c", `ps -eo pcpu --no-headers | awk '{s+=$1} END {print s+0}'`)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(out), 64)
	return v
}

func (s *Server) toolWaitForIdle(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	timeout := argInt(args, "timeout_ms")
	if timeout <= 0 {
		timeout = 15000
	}
	quiet := argInt(args, "quiet_ms")
	if quiet <= 0 {
		quiet = 1200
	}
	maxCPU := argInt(args, "max_cpu")
	if maxCPU <= 0 {
		maxCPU = 40
	}
	ignoreCPU, _ := args["ignore_cpu"].(bool)

	start := time.Now()
	deadline := start.Add(time.Duration(timeout) * time.Millisecond)
	last := ""
	stableSince := time.Now()
	lastCPU := 0.0

	for time.Now().Before(deadline) {
		fp := s.screenFingerprint()
		if fp != last {
			last = fp
			stableSince = time.Now()
		}
		lastCPU = s.cpuBusy()
		screenQuiet := time.Since(stableSince) >= time.Duration(quiet)*time.Millisecond
		cpuQuiet := ignoreCPU || lastCPU <= float64(maxCPU)
		if screenQuiet && cpuQuiet {
			return jsonContent(map[string]any{
				"idle": true, "waited_ms": time.Since(start).Milliseconds(),
				"cpu_percent": int(lastCPU), "reason": "the screen went still and the CPU settled",
			}), false, true
		}
		if !sleepCtx(ctx, 200*time.Millisecond) {
			break
		}
	}
	reason := "timed out with the screen still changing"
	if time.Since(stableSince) >= time.Duration(quiet)*time.Millisecond {
		reason = fmt.Sprintf("the screen went still but the CPU is still at %d%%", int(lastCPU))
	}
	return jsonContent(map[string]any{
		"idle": false, "waited_ms": time.Since(start).Milliseconds(),
		"cpu_percent": int(lastCPU), "reason": reason,
	}), false, true
}

// --- macro-acciones ----------------------------------------------------------

func (s *Server) toolOpenAppAndWait(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	command := strings.TrimSpace(argStr(args, "command"))
	if command == "" {
		return textContent("`command` is missing"), true, true
	}
	timeout := argInt(args, "timeout_ms")
	if timeout <= 0 {
		timeout = 25000
	}
	match := strings.ToLower(argStr(args, "match"))
	if match == "" {
		// With no explicit hint, the binary's name is usually in the WM class.
		bin := strings.Fields(command)[0]
		match = strings.ToLower(filepath.Base(bin))
	}
	asRoot, _ := args["as_root"].(bool)

	before := map[string]bool{}
	for _, w := range s.listWindows() {
		before[w.ID] = true
	}

	if content, isErr := s.toolLaunchApp(command, asRoot); isErr {
		return content, true, true
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Millisecond)
	for time.Now().Before(deadline) {
		if !sleepCtx(ctx, 300*time.Millisecond) {
			break
		}
		for _, w := range s.listWindows() {
			if before[w.ID] {
				continue // already there: not the window we opened
			}
			haystack := strings.ToLower(w.Class + " " + w.Title)
			if !strings.Contains(haystack, match) {
				continue
			}
			s.run("wmctrl", "-i", "-a", w.ID)
			// Let it finish drawing before handing control back to the caller.
			s.toolWaitForIdle(ctx, map[string]any{
				"timeout_ms": 6000, "quiet_ms": 700, "ignore_cpu": true,
			})
			return jsonContent(map[string]any{
				"opened": true, "window": w,
				"waited_ms": timeout - int(time.Until(deadline).Milliseconds()),
			}), false, true
		}
	}
	return jsonContent(map[string]any{
		"opened": false,
		"hint": fmt.Sprintf("no new window appeared containing %q; "+
			"try an explicit `match`, or check whether the program failed to start", match),
	}), true, true
}

func (s *Server) toolFillForm(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	raw, ok := args["fields"].(map[string]any)
	if !ok || len(raw) == 0 {
		return textContent("`fields` is missing (an object of name -> value)"), true, true
	}
	// A stable order matters: otherwise each run fills the fields in a different
	// sequence, and forms that validate as you type behave differently.
	names := make([]string, 0, len(raw))
	for k := range raw {
		names = append(names, k)
	}
	sort.Strings(names)

	results := make([]map[string]any, 0, len(names))
	failed := 0
	for _, name := range names {
		value := fmt.Sprint(raw[name])
		out, err := s.a11yRaw("settext", "--name", name, "--text", value)
		entry := map[string]any{"field": name}
		switch {
		case err != nil:
			entry["ok"] = false
			entry["error"] = err.Error()
			failed++
		case strings.Contains(strings.ToLower(out), "error") ||
			strings.Contains(strings.ToLower(out), "not found"):
			entry["ok"] = false
			entry["error"] = strings.TrimSpace(out)
			failed++
		default:
			entry["ok"] = true
		}
		results = append(results, entry)
	}

	res := map[string]any{"fields": results, "filled": len(names) - failed, "failed": failed}
	if submit := argStr(args, "submit"); submit != "" {
		out, err := s.a11yRaw("click", "--name", submit)
		if err != nil || strings.Contains(strings.ToLower(out), "error") {
			res["submitted"] = false
			res["submit_error"] = strings.TrimSpace(out + fmt.Sprint(err))
		} else {
			res["submitted"] = true
			s.toolWaitForIdle(ctx, map[string]any{"timeout_ms": 8000, "quiet_ms": 800})
		}
	}
	return jsonContent(res), failed > 0, true
}

// --- semantic screen diff ----------------------------------------------------

// uiNode is what gets compared between calls: the minimum that makes a widget
// recognisable and that changes when something interesting happens.
type uiNode struct {
	Role  string `json:"role"`
	Name  string `json:"name"`
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"`
}

// flattenUI walks whatever a11y.py returned and flattens it to ref -> node.
func flattenUI(v any, out map[string]uiNode) {
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			flattenUI(item, out)
		}
	case map[string]any:
		ref, _ := t["ref"].(string)
		if ref != "" {
			n := uiNode{}
			n.Role, _ = t["role"].(string)
			n.Name, _ = t["name"].(string)
			n.Text, _ = t["text"].(string)
			if st, ok := t["states"].([]any); ok {
				parts := make([]string, 0, len(st))
				for _, s := range st {
					parts = append(parts, fmt.Sprint(s))
				}
				sort.Strings(parts)
				n.State = strings.Join(parts, ",")
			}
			out[ref] = n
		}
		// a11y.py returns {"count":N,"elements":[…]} with the list ALREADY
		// flattened; the other keys cover nested shapes should the bridge
		// change. Missing "elements" here once made every diff come back empty.
		for _, key := range []string{"elements", "children", "apps", "windows", "nodes"} {
			if child, ok := t[key]; ok {
				flattenUI(child, out)
			}
		}
	}
}

func (s *Server) toolUIDiff(args map[string]any) ([]map[string]any, bool, bool) {
	// An explicit, high limit: a diff is only meaningful if both snapshots cover
	// the same ground. With the default limit of 200 a busy screen gets
	// truncated, and phantom changes appear every time the cut-off moves.
	out, err := s.a11yRaw("tree", "--limit", "4000", "--depth", "14")
	if err != nil {
		return textContent("ui_diff: could not read the tree: %v", err), true, true
	}
	var parsed any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return textContent("ui_diff: non-JSON reply from the accessibility bridge"), true, true
	}
	current := map[string]uiNode{}
	flattenUI(parsed, current)

	s.uiMu.Lock()
	previous := s.uiLast
	if reset, _ := args["reset"].(bool); reset {
		previous = nil
	}
	s.uiLast = current
	s.uiMu.Unlock()

	if previous == nil {
		return jsonContent(map[string]any{
			"baseline": true, "nodes": len(current),
			"note": "first call: the reference snapshot was stored; the next one returns only the changes",
		}), false, true
	}

	var appeared, vanished, changed []map[string]any
	for ref, now := range current {
		before, existed := previous[ref]
		if !existed {
			appeared = append(appeared, map[string]any{
				"ref": ref, "role": now.Role, "name": now.Name, "text": now.Text,
			})
			continue
		}
		delta := map[string]any{}
		if before.Name != now.Name {
			delta["name"] = []string{before.Name, now.Name}
		}
		if before.Text != now.Text {
			delta["text"] = []string{before.Text, now.Text}
		}
		if before.State != now.State {
			delta["state"] = []string{before.State, now.State}
		}
		if len(delta) > 0 {
			delta["ref"] = ref
			delta["role"] = now.Role
			changed = append(changed, delta)
		}
	}
	for ref, before := range previous {
		if _, ok := current[ref]; !ok {
			vanished = append(vanished, map[string]any{
				"ref": ref, "role": before.Role, "name": before.Name,
			})
		}
	}

	total := len(appeared) + len(vanished) + len(changed)
	return jsonContent(map[string]any{
		"changes": total, "nodes": len(current),
		"appeared": appeared, "vanished": vanished, "changed": changed,
	}), false, true
}

// --- action log --------------------------------------------------------------

func (s *Server) toolActionLog(args map[string]any) ([]map[string]any, bool, bool) {
	limit := argInt(args, "limit")
	if limit <= 0 {
		limit = 50
	}
	entries := s.actions.Tail(limit, argStr(args, "filter"))
	res := map[string]any{"count": len(entries), "entries": entries}
	if rec := videoOffset(s.recorder); rec != "" {
		res["recording_at"] = rec
	}
	if clear, _ := args["clear"].(bool); clear {
		res["cleared"] = s.actions.Clear()
	}
	return jsonContent(res), false, true
}

// --- restore points ----------------------------------------------------------

// safeName keeps a snapshot name from escaping its directory.
func safeName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" {
		return "", fmt.Errorf("the name is missing")
	}
	if strings.ContainsAny(n, "/\\.\x00 '\"$`;&|<>") {
		return "", fmt.Errorf("invalid name %q: letters, digits, - and _ only", n)
	}
	return n, nil
}

func (s *Server) toolSnapshotCreate(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	name, err := safeName(argStr(args, "name"))
	if err != nil {
		return textContent("%v", err), true, true
	}
	dir := snapshotDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return textContent("could not create %s: %v", dir, err), true, true
	}
	tarPath := filepath.Join(dir, name+".tar.gz")
	pkgPath := filepath.Join(dir, name+".packages")

	home := homeDir()
	// Exclude the snapshot directory itself: otherwise each snapshot swallows
	// every earlier one and they grow quadratically.
	cmd := fmt.Sprintf(
		"tar czf %q --warning=no-file-changed --exclude=%q -C %q %q 2>/dev/null; "+
			"dpkg-query -W -f='${Package}\\n' > %q; true",
		tarPath, ".sentineldesk-snapshots",
		filepath.Dir(home), filepath.Base(home), pkgPath)
	if _, err := s.runElevated(ctx, cmd, false, 600000); err != nil {
		return textContent("snapshot failed: %v", err), true, true
	}
	info, err := os.Stat(tarPath)
	if err != nil {
		return textContent("the snapshot was not written: %v", err), true, true
	}
	// A tar of a real home weighs kilobytes at minimum. If it comes out absurdly
	// small then nothing was packed, and failing loudly beats storing a "restore
	// point" that turns out to restore nothing.
	if info.Size() < 512 {
		os.Remove(tarPath)
		return textContent("the snapshot came out empty (%d bytes): could not archive %s",
			info.Size(), home), true, true
	}
	if note := argStr(args, "note"); note != "" {
		os.WriteFile(filepath.Join(dir, name+".note"), []byte(note), 0o644)
	}
	return jsonContent(map[string]any{
		"created": name, "size": info.Size(), "path": tarPath,
		"note": argStr(args, "note"),
	}), false, true
}

func (s *Server) toolSnapshotList() ([]map[string]any, bool, bool) {
	dir := snapshotDirPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return jsonContent(map[string]any{"snapshots": []any{}}), false, true
	}
	var out []map[string]any
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".tar.gz")
		info, err := e.Info()
		if err != nil {
			continue
		}
		item := map[string]any{
			"name": name, "size": info.Size(),
			"created": info.ModTime().Format(time.RFC3339),
		}
		if note, err := os.ReadFile(filepath.Join(dir, name+".note")); err == nil {
			item["note"] = strings.TrimSpace(string(note))
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["created"]) > fmt.Sprint(out[j]["created"])
	})
	return jsonContent(map[string]any{"snapshots": out, "dir": dir}), false, true
}

func (s *Server) toolSnapshotRestore(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	name, err := safeName(argStr(args, "name"))
	if err != nil {
		return textContent("%v", err), true, true
	}
	dir := snapshotDirPath()
	tarPath := filepath.Join(dir, name+".tar.gz")
	if _, err := os.Stat(tarPath); err != nil {
		return textContent("no such snapshot %q", name), true, true
	}

	// Which packages arrived after the snapshot. They are not removed
	// automatically — they may well have been installed on purpose — but they
	// are reported so whoever decides knows what is extra.
	var added []string
	if before, err := os.ReadFile(filepath.Join(dir, name+".packages")); err == nil {
		had := map[string]bool{}
		for _, p := range strings.Fields(string(before)) {
			had[p] = true
		}
		if now, err := exec.Command("dpkg-query", "-W", "-f=${Package}\n").Output(); err == nil {
			for _, p := range strings.Fields(string(now)) {
				if !had[p] {
					added = append(added, p)
				}
			}
		}
	}

	// The tar is unpacked over /home; --overwrite so that modified files
	// modificados vuelvan al estado guardado.
	res, err := s.runElevated(ctx,
		fmt.Sprintf("tar xzf %q -C /home --overwrite 2>&1 | tail -5; true", tarPath),
		true, 600000)
	if err != nil {
		return textContent("restore failed: %v", err), true, true
	}
	out := map[string]any{
		"restored": name,
		"note":     "only /home/sentineldesk was restored; changes outside the home are still there",
		"log":      strings.TrimSpace(fmt.Sprint(res["stdout"])),
	}
	if len(added) > 0 {
		sort.Strings(added)
		if len(added) > 40 {
			out["packages_added_since"] = append(added[:40], fmt.Sprintf("… and %d more", len(added)-40))
		} else {
			out["packages_added_since"] = added
		}
		out["hint"] = "those packages were installed after the snapshot; use remove_packages to take them out"
	}
	return jsonContent(out), false, true
}

func (s *Server) toolSnapshotDelete(args map[string]any) ([]map[string]any, bool, bool) {
	name, err := safeName(argStr(args, "name"))
	if err != nil {
		return textContent("%v", err), true, true
	}
	found := false
	for _, ext := range []string{".tar.gz", ".packages", ".note"} {
		if os.Remove(filepath.Join(snapshotDirPath(), name+ext)) == nil {
			found = true
		}
	}
	if !found {
		return textContent("no such snapshot %q", name), true, true
	}
	return jsonContent(map[string]any{"deleted": name}), false, true
}
