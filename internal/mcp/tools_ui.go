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

// The ui_* tools operate the desktop through the interface's STRUCTURE (AT-SPI)
// rather than through pixels. Knowing which widgets exist, what they are called
// and what they accept,
// and invoke their actions directly: no screenshots, no OCR, no coordinates.
//
// The AT-SPI query itself lives in a11y.py (pyatspi); this file exposes it as
// tools and normalises the JSON.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const a11yScript = "/usr/local/bin/a11y.py"
const sessionBus = "unix:path=/run/user/1000/bus"

func (s *Server) buildUITools() []toolDef {
	return []toolDef{
		{
			Name:        "ui_tree",
			Risk:        riskRead,
			Description: "Read the ACCESSIBILITY TREE of the desktop: every window and widget with its role, name, text, state, screen coordinates and the actions it accepts. This is how you SEE what is on screen as structured data instead of taking a screenshot and guessing. Prefer this over `screenshot` whenever you need to operate an application. Use interactive=true to keep only the parts you can act on.",
			InputSchema: schema(map[string]any{
				"app":         pStr("only this application (substring of its name)"),
				"interactive": pBool("keep only actionable/labelled elements (recommended)"),
				"depth":       pInt("max depth (default 12)"),
				"limit":       pInt("max elements (default 200)"),
			}),
		},
		{
			Name:        "ui_find",
			Risk:        riskRead,
			Description: "Find UI elements by role, name or text — e.g. the button called 'Sign in', or every text entry. Returns each match with its `ref` (use it with ui_click / ui_set_text) plus its screen coordinates. This replaces OCR + find_text for real applications.",
			InputSchema: schema(map[string]any{
				"role":  pStr("role, e.g. 'push button', 'entry', 'menu item', 'link'"),
				"name":  pStr("accessible name (substring, case-insensitive)"),
				"text":  pStr("visible text (substring)"),
				"app":   pStr("restrict to this application"),
				"limit": pInt("max results (default 20)"),
			}),
		},
		{
			Name:            "ui_click",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Invoke an element's action DIRECTLY by its ref (from ui_find/ui_tree) — presses the button, opens the menu, toggles the checkbox. The pointer never moves, so it cannot miss and it does not matter if the window is partly covered.",
			InputSchema: schema(map[string]any{
				"ref":    pStr("element ref, e.g. '2/0/3/1'"),
				"action": pStr("action name if the element has several (default: the first, usually 'click')"),
			}, "ref"),
		},
		{
			Name:            "ui_set_text",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Write text straight into an editable field by ref, replacing its content. Unlike type_text this does not depend on which window has focus.",
			InputSchema: schema(map[string]any{
				"ref": pStr("element ref of the entry/text field"), "text": pStr("text to set"),
			}, "ref", "text"),
		},
		{
			Name:        "ui_get_text",
			Risk:        riskRead,
			Description: "Read the text/label of an element by ref (no OCR involved).",
			InputSchema: schema(map[string]any{"ref": pStr("element ref")}, "ref"),
		},
		{
			Name:            "ui_focus",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Give keyboard focus to an element by ref (then type_text goes where you want).",
			InputSchema:     schema(map[string]any{"ref": pStr("element ref")}, "ref"),
		},
		{
			Name:        "ui_wait_for",
			Risk:        riskRead,
			Description: "Wait until a UI element matching role/name/text exists — the reliable way to wait for a dialog, a page or a button to appear, instead of guessing a `wait` duration.",
			InputSchema: schema(map[string]any{
				"name": pStr("accessible name (substring)"), "role": pStr("role"),
				"text": pStr("visible text"), "app": pStr("restrict to this application"),
				"timeout_ms": pInt("timeout, default 15000"),
			}),
		},
	}
}

// a11yRaw invokes the accessibility bridge with the right environment — display
// and session bus — and returns its raw output, for the tools that process the
// JSON themselves (ui_diff, fill_form) rather than passing it straight through.
func (s *Server) a11yRaw(args ...string) (string, error) {
	cmd := exec.Command("python3", append([]string{a11yScript}, args...)...)
	cmd.Env = append(os.Environ(),
		"DISPLAY="+s.display,
		"XDG_RUNTIME_DIR=/run/user/1000",
		"DBUS_SESSION_BUS_ADDRESS="+sessionBus)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("a11y %s: %v", args[0], err)
	}
	return string(out), nil
}

func (s *Server) a11y(args ...string) ([]map[string]any, bool) {
	cmd := exec.Command("python3", append([]string{a11yScript}, args...)...)
	cmd.Env = append(os.Environ(),
		"DISPLAY="+s.display,
		"XDG_RUNTIME_DIR=/run/user/1000",
		"DBUS_SESSION_BUS_ADDRESS="+sessionBus)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return textContent("a11y %s failed: %v", args[0], err), true
	}
	var parsed any
	if e := json.Unmarshal(out, &parsed); e != nil {
		return textContent("%s", strings.TrimSpace(string(out))), err != nil
	}
	if m, ok := parsed.(map[string]any); ok {
		if msg, bad := m["error"].(string); bad {
			return textContent("%s", msg), true
		}
	}
	return jsonContent(parsed), false
}

func (s *Server) dispatchUI(ctx context.Context, name string, args map[string]any) ([]map[string]any, bool, bool) {
	opt := func(flag, key string) []string {
		if v := argStr(args, key); v != "" {
			return []string{flag, v}
		}
		return nil
	}
	optInt := func(flag, key string) []string {
		if n := argInt(args, key); n > 0 {
			return []string{flag, strconv.Itoa(n)}
		}
		return nil
	}

	switch name {
	case "ui_tree":
		a := []string{"tree"}
		a = append(a, opt("--app", "app")...)
		a = append(a, optInt("--depth", "depth")...)
		a = append(a, optInt("--limit", "limit")...)
		if b, _ := args["interactive"].(bool); b {
			a = append(a, "--interactive")
		}
		c, e := s.a11y(a...)
		return c, e, true

	case "ui_find", "ui_wait_for":
		a := []string{"find"}
		if name == "ui_wait_for" {
			a = []string{"waitfor"}
		}
		a = append(a, opt("--role", "role")...)
		a = append(a, opt("--name", "name")...)
		a = append(a, opt("--text", "text")...)
		a = append(a, opt("--app", "app")...)
		a = append(a, optInt("--limit", "limit")...)
		if name == "ui_wait_for" {
			ms := argInt(args, "timeout_ms")
			if ms <= 0 {
				ms = 15000
			}
			a = append(a, "--timeout-ms", strconv.Itoa(ms))
		}
		c, e := s.a11y(a...)
		return c, e, true

	case "ui_click":
		a := []string{"click", "--ref", argStr(args, "ref")}
		a = append(a, opt("--action", "action")...)
		c, e := s.a11y(a...)
		return c, e, true

	case "ui_set_text":
		c, e := s.a11y("settext", "--ref", argStr(args, "ref"), "--text", argStr(args, "text"))
		return c, e, true

	case "ui_get_text":
		c, e := s.a11y("gettext", "--ref", argStr(args, "ref"))
		return c, e, true

	case "ui_focus":
		c, e := s.a11y("focus", "--ref", argStr(args, "ref"))
		return c, e, true
	}
	return nil, false, false
}

// --- CDP: driving the browser through the real DOM ------------------------

func (s *Server) buildBrowserTools() []toolDef {
	return []toolDef{
		{
			Name:        "browser_open",
			Risk:        riskWrite,
			Description: "Launch Chromium with the DevTools Protocol enabled (port 9222) so the other browser_* tools can drive the real DOM. Optionally opens a URL. If it is already running this just reports it.",
			InputSchema: schema(map[string]any{"url": pStr("optional URL to open")}),
		},
		{
			Name:        "browser_tabs",
			Risk:        riskRead,
			Description: "List the open browser tabs with their title and URL.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "browser_goto",
			Risk:        riskWrite,
			Description: "Navigate the active tab to a URL and wait for the load to finish.",
			InputSchema: schema(map[string]any{"url": pStr("URL")}, "url"),
		},
		{
			Name:        "browser_eval",
			Risk:        riskDanger,
			Description: "Run JavaScript in the page and return the result. The most powerful browser tool: you can read anything from the DOM without screenshots.",
			InputSchema: schema(map[string]any{"expression": pStr("JavaScript expression")}, "expression"),
		},
		{
			Name:        "browser_click",
			Risk:        riskWrite,
			Description: "Click an element in the page by CSS selector — exact, no coordinates involved.",
			InputSchema: schema(map[string]any{"selector": pStr("CSS selector, e.g. '#login-btn' or 'button.primary'")}, "selector"),
		},
		{
			Name:        "browser_type",
			Risk:        riskWrite,
			Description: "Type text into an input/textarea selected by CSS selector (fires the events a real page expects).",
			InputSchema: schema(map[string]any{"selector": pStr("CSS selector"), "text": pStr("text")}, "selector", "text"),
		},
		{
			Name:        "browser_text",
			Risk:        riskRead,
			Description: "Get the visible text of the page, or of the element matching a CSS selector. This is what replaces OCR for web content.",
			InputSchema: schema(map[string]any{
				"selector":  pStr("optional CSS selector (default: whole page)"),
				"max_chars": pInt("truncate (default 4000)"),
			}),
		},
		{
			Name:        "browser_wait_for",
			Risk:        riskRead,
			Description: "Wait until an element matching a CSS selector appears in the page.",
			InputSchema: schema(map[string]any{
				"selector": pStr("CSS selector"), "timeout_ms": pInt("timeout, default 15000"),
			}, "selector"),
		},
	}
}

func (s *Server) dispatchBrowser(ctx context.Context, name string, args map[string]any) ([]map[string]any, bool, bool) {
	switch name {
	case "browser_open":
		c, e := s.toolBrowserOpen(ctx, argStr(args, "url"))
		return c, e, true
	case "browser_tabs":
		targets, err := cdpTargets()
		if err != nil {
			return textContent("browser_tabs failed: %v", err), true, true
		}
		var tabs []map[string]any
		for _, t := range targets {
			tabs = append(tabs, map[string]any{"title": t.Title, "url": t.URL, "id": t.ID})
		}
		return jsonContent(tabs), false, true
	case "browser_goto":
		c, e := s.cdpEvalReport(fmt.Sprintf(
			"(()=>{location.href=%s; return 'navigating to '+%s})()",
			jsStr(argStr(args, "url")), jsStr(argStr(args, "url"))))
		return c, e, true
	case "browser_eval":
		c, e := s.cdpEvalReport(argStr(args, "expression"))
		return c, e, true
	case "browser_click":
		sel := argStr(args, "selector")
		c, e := s.cdpEvalReport(fmt.Sprintf(
			"(()=>{const el=document.querySelector(%s); if(!el) return 'ERROR: no such element '+%s; el.scrollIntoView({block:'center'}); el.click(); return 'clicked '+%s})()",
			jsStr(sel), jsStr(sel), jsStr(sel)))
		return c, e, true
	case "browser_type":
		sel, txt := argStr(args, "selector"), argStr(args, "text")
		c, e := s.cdpEvalReport(fmt.Sprintf(
			"(()=>{const el=document.querySelector(%s); if(!el) return 'ERROR: no such element '+%s;"+
				"el.focus(); el.value=%s;"+
				"el.dispatchEvent(new Event('input',{bubbles:true}));"+
				"el.dispatchEvent(new Event('change',{bubbles:true}));"+
				"return 'escrito en '+%s})()",
			jsStr(sel), jsStr(sel), jsStr(txt), jsStr(sel)))
		return c, e, true
	case "browser_text":
		sel := argStr(args, "selector")
		max := argInt(args, "max_chars")
		if max <= 0 {
			max = 4000
		}
		target := "document.body"
		if sel != "" {
			target = fmt.Sprintf("document.querySelector(%s)", jsStr(sel))
		}
		c, e := s.cdpEvalReport(fmt.Sprintf(
			"(()=>{const el=%s; if(!el) return 'ERROR: no element';"+
				"return (el.innerText||el.textContent||'').trim().slice(0,%d)})()", target, max))
		return c, e, true
	case "browser_wait_for":
		sel := argStr(args, "selector")
		ms := argInt(args, "timeout_ms")
		if ms <= 0 {
			ms = 15000
		}
		deadline := time.Now().Add(time.Duration(ms) * time.Millisecond)
		for time.Now().Before(deadline) {
			res, err := cdpEval(fmt.Sprintf("!!document.querySelector(%s)", jsStr(sel)))
			if err == nil && strings.Contains(fmt.Sprint(res), "true") {
				return textContent("%s appeared", sel), false, true
			}
			if !sleepCtx(ctx, 300*time.Millisecond) {
				break
			}
		}
		return textContent("timed out waiting for %s", sel), true, true
	}
	return nil, false, false
}

func (s *Server) toolBrowserOpen(ctx context.Context, url string) ([]map[string]any, bool) {
	if targets, err := cdpTargets(); err == nil && len(targets) > 0 {
		if url != "" {
			return s.cdpEvalReport(fmt.Sprintf("(()=>{location.href=%s; return 'navigating'})()", jsStr(url)))
		}
		return textContent("the browser is already open with CDP (%d tabs)", len(targets)), false
	}
	cmdline := "chromium --remote-debugging-port=9222 --remote-allow-origins=* " +
		"--no-first-run --no-default-browser-check --force-renderer-accessibility"
	if url != "" {
		cmdline += " " + url
	}
	cmd := exec.Command("setsid", "sh", "-c", cmdline)
	cmd.Env = append(os.Environ(), "DISPLAY="+s.display,
		"DBUS_SESSION_BUS_ADDRESS="+sessionBus)
	if err := cmd.Start(); err != nil {
		return textContent("browser_open failed: %v", err), true
	}
	go cmd.Wait()
	// Wait for the debugging port to answer
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if t, err := cdpTargets(); err == nil && len(t) > 0 {
			return textContent("browser open with CDP (%d tabs)", len(t)), false
		}
		if !sleepCtx(ctx, 700*time.Millisecond) {
			break
		}
	}
	return textContent("the browser started but CDP did not answer in time"), true
}

func (s *Server) cdpEvalReport(expr string) ([]map[string]any, bool) {
	res, err := cdpEval(expr)
	if err != nil {
		return textContent("browser eval failed: %v", err), true
	}
	return textContent("%s", res), false
}

// jsStr serialises a string for embedding in JavaScript source.
func jsStr(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}
