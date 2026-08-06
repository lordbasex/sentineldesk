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

// Running a command in the terminal the PEOPLE are looking at, and reading what
// it printed.
//
// There were two ways for an agent to run something, and they had opposite
// weaknesses:
//
//	run_command / shell_exec  — full stdout, stderr and exit code, but invisible:
//	                            it happens off-screen, and the person sharing the
//	                            desktop has no idea it happened at all.
//	typing into a terminal    — visible, collaborative, and completely blind.
//	                            `type_text` reports "typed 23 chars", which says
//	                            the keys were delivered, not that the command
//	                            worked. An agent could watch a command fail and
//	                            report success in the same breath.
//
// The blind one is precisely the collaborative one, which is the wrong way
// round. This closes it: the command is typed into the real terminal so the
// person sees it happen, and the output comes back through AT-SPI — the exact
// characters the terminal holds, not OCR of a screenshot.
//
// The waiting is the part that matters. Reading right after pressing Return
// returns the command line and nothing else, so this polls until the shell's
// prompt comes back and the text stops changing.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A shell prompt at the very end of the buffer: the shell is idle and waiting.
// Covers the common endings — $ or # for sh/bash, > for continuation — followed
// by optional trailing whitespace that terminals pad the last line with.
var promptTail = regexp.MustCompile(`[$#>]\s*$`)

func (s *Server) terminalTools() []toolDef {
	return []toolDef{
		{
			Name:            "terminal_run",
			Risk:            riskDanger,
			RequiresControl: true,
			Description: "Type a command into a terminal window ON THE DESKTOP, wait " +
				"for it to finish, and return what it printed. Use this instead of " +
				"run_command when a person is watching: they see the command and its " +
				"output exactly as if you had typed it. Reads the terminal through " +
				"accessibility, so the text is exact, not OCR.",
			InputSchema: schema(map[string]any{
				"command":    pStr("the command line to run"),
				"timeout_ms": pInt("how long to wait for the prompt (default 120000)"),
			}, "command"),
		},
		{
			Name: "terminal_open",
			Risk: riskDanger,
			Description: "Open a terminal window on the desktop, visible to anyone " +
				"watching. Every interactive shell here reports its exit status, so " +
				"terminal_run can tell a silent failure from a success — and so can " +
				"terminal_read, even for commands a PERSON typed. Use `sudo -E su` " +
				"rather than plain `sudo su` to keep that across a root shell.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name: "check_errors",
			Risk: riskRead,
			Description: "Look for anything on the desktop that is reporting a " +
				"failure: error dialogs, alerts, and message boxes, with their text " +
				"and buttons. A graphical program does not fail with an exit code — " +
				"it puts a box on the screen — so call this after launching " +
				"something, or whenever a step did not do what you expected.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name: "terminal_read",
			Risk: riskRead,
			Description: "Read what a terminal on the desktop is showing right now, " +
				"without typing anything, plus the exit status of the last command — " +
				"including one a person ran. Use this when somebody asks you to look " +
				"at an error they hit.",
			InputSchema: schema(map[string]any{
				"lines": pInt("how many trailing lines to return (default 40)"),
			}),
		},
	}
}

// terminalRefs lists every terminal in the accessibility tree, most recently
// mapped last. The count matters as much as the refs: a ref is a positional
// path, so when a window closes the path does not break — it starts resolving
// to whatever moved into its place. Watching how many terminals exist is the
// way to notice one going away.
func (s *Server) terminalRefs() ([]string, error) {
	out, err := s.a11yRaw("find", "--role", "terminal")
	if err != nil {
		return nil, err
	}
	var found struct {
		Elements []struct {
			Ref string `json:"ref"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		return nil, fmt.Errorf("accessibility bridge returned non-JSON")
	}
	refs := make([]string, 0, len(found.Elements))
	for _, e := range found.Elements {
		refs = append(refs, e.Ref)
	}
	return refs, nil
}

func (s *Server) findTerminal() (string, error) {
	refs, err := s.terminalRefs()
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return "", fmt.Errorf("no terminal window is open — launch one first " +
			"(open_app_and_wait with lxterminal)")
	}
	// The last one is the most recently mapped, which is the one just opened.
	return refs[len(refs)-1], nil
}

// readTerminal returns the terminal's current contents, trailing blank lines
// stripped: terminals pad the buffer to the window height and those empty rows
// would defeat any "has the output stopped changing" comparison.
func (s *Server) readTerminal(ref string) (string, error) {
	out, err := s.a11yRaw("gettext", "--ref", ref)
	if err != nil {
		return "", err
	}
	var got struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		return "", fmt.Errorf("accessibility bridge returned non-JSON")
	}
	return strings.TrimRight(got.Text, " \t\n"), nil
}

func lastLines(text string, n int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// errorish matches the wording dialogs use when something went wrong. Roles
// alone are not enough: many toolkits report a plain "dialog" for an error and
// an "alert" for a harmless confirmation.
var errorish = regexp.MustCompile(`(?i)\b(error|failed|failure|cannot|could not|unable|denied|invalid|not found|no such|warning|problem)\b`)

// rcPath is where the instrumented shell leaves the last exit status.
//
// Out of band on purpose. Appending `; echo $?` to each command would work on
// any shell, but the person sharing the screen would watch the agent type
// bookkeeping after everything — the collaboration is worth more than the
// convenience. PROMPT_COMMAND writes it invisibly instead.
const rcPath = "/tmp/sentineldesk-rc"

// readExitCode returns the status the shell recorded, the command it belonged
// to, and whether the record is fresh enough to trust.
//
// The hook is installed for every interactive shell on the desktop, so this
// works for what a PERSON typed just as well as for what the agent typed. That
// symmetry is the whole point: somebody hits an error, asks the agent to sort it
// out, and the agent reads what actually happened rather than a retelling.
func (s *Server) readExitCode(since time.Time) (int, string, bool) {
	fi, err := os.Stat(rcPath)
	if err != nil || fi.ModTime().Before(since) {
		return 0, "", false
	}
	raw, err := os.ReadFile(rcPath)
	if err != nil {
		return 0, "", false
	}
	parts := strings.SplitN(strings.TrimRight(string(raw), "\n"), "\t", 2)
	code, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, "", false
	}
	cmd := ""
	if len(parts) > 1 {
		cmd = strings.TrimSpace(parts[1])
	}
	return code, cmd, true
}

func (s *Server) callTerminal(ctx context.Context, name string, args map[string]any) (any, bool, bool) {
	switch name {
	case "terminal_open":
		_ = os.Remove(rcPath)
		cmd := exec.Command("setsid", "lxterminal")
		cmd.Env = append(os.Environ(), "DISPLAY="+s.display)
		if err := cmd.Start(); err != nil {
			return textContent("could not open a terminal: %v", err), true, true
		}
		go cmd.Wait()
		// Wait for the shell to draw its first prompt, otherwise the caller
		// types into a window that is not ready yet.
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if !sleepCtx(ctx, 400*time.Millisecond) {
				break
			}
			if ref, err := s.findTerminal(); err == nil {
				if txt, err := s.readTerminal(ref); err == nil && promptTail.MatchString(txt) {
					return map[string]any{
						"opened": true, "exit_codes": true,
						"note": "exit codes are reported. Use `sudo -E su` rather than " +
							"`sudo su` to keep them across a root shell.",
					}, false, true
				}
			}
		}
		return textContent("the terminal did not show a prompt in time"), true, true
	case "check_errors":
		// Alerts first — that is what a well-behaved toolkit uses — then any
		// dialog whose text reads like a failure.
		found := []map[string]any{}
		seen := map[string]bool{}
		for _, role := range []string{"alert", "dialog"} {
			out, err := s.a11yRaw("find", "--role", role)
			if err != nil {
				continue
			}
			var res struct {
				Elements []struct {
					Ref  string `json:"ref"`
					Role string `json:"role"`
					Name string `json:"name"`
					Text string `json:"text"`
				} `json:"elements"`
			}
			if json.Unmarshal([]byte(out), &res) != nil {
				continue
			}
			for _, e := range res.Elements {
				if seen[e.Ref] {
					continue
				}
				body := strings.TrimSpace(e.Name + " " + e.Text)
				// An alert is worth reporting whatever it says; a dialog only
				// when its wording suggests a failure, or every open file
				// chooser would look like a problem.
				if e.Role != "alert" && !errorish.MatchString(body) {
					continue
				}
				seen[e.Ref] = true
				found = append(found, map[string]any{
					"ref": e.Ref, "role": e.Role, "title": e.Name, "text": e.Text,
				})
			}
		}
		if len(found) == 0 {
			return map[string]any{
				"errors_on_screen": false,
				"note": "nothing is reporting a failure. This only sees graphical " +
					"dialogs — a command that failed silently in a terminal shows up " +
					"in terminal_read, and a program that died at launch in the " +
					"stderr that launch_app returns.",
			}, false, true
		}
		return map[string]any{
			"errors_on_screen": true,
			"dialogs":          found,
			"hint": "use ui_click with the ref of a button to dismiss one, " +
				"after reading what it says",
		}, false, true
	case "terminal_read":
		ref, err := s.findTerminal()
		if err != nil {
			return textContent("%v", err), true, true
		}
		text, err := s.readTerminal(ref)
		if err != nil {
			return textContent("%v", err), true, true
		}
		n := argInt(args, "lines")
		if n <= 0 {
			n = 40
		}
		out := map[string]any{"text": lastLines(text, n)}
		// Any interactive shell records this, so the last command may well be
		// one a person typed. That is deliberate.
		if code, cmd, ok := s.readExitCode(time.Time{}); ok {
			out["last_command"] = cmd
			out["last_exit_code"] = code
			out["last_succeeded"] = code == 0
			if code != 0 {
				out["note"] = "the last command in this terminal failed — it may " +
					"have been run by a person, not by you"
			}
		}
		return out, false, true

	case "terminal_run":
		command := argStr(args, "command")
		if strings.TrimSpace(command) == "" {
			return textContent("`command` is missing"), true, true
		}
		timeout := time.Duration(argInt(args, "timeout_ms")) * time.Millisecond
		if timeout <= 0 {
			timeout = 120 * time.Second
		}

		ref, err := s.findTerminal()
		if err != nil {
			return textContent("%v", err), true, true
		}
		before, err := s.readTerminal(ref)
		if err != nil {
			return textContent("%v", err), true, true
		}
		started := time.Now()

		// How many terminals exist before the command is sent. Some commands end
		// the shell — `exit`, `logout`, anything that takes the emulator down
		// with it — and those leave no prompt to wait for; without noticing, the
		// loop below spends the whole timeout waiting for one that is never
		// coming and then reports "it may still be running" about a command that
		// finished long ago.
		//
		// Counted BEFORE typing, deliberately. `echo x; exit` closes the window
		// faster than one query to the accessibility bridge takes, so counting
		// afterwards read zero and quietly disabled the check it was meant to arm.
		//
		// Counting rather than watching for a read error, because the read does
		// not fail: refs are positional paths, so when the window closes the path
		// starts resolving to whatever took its place and returns somebody
		// else's text forever.
		terminalsBefore := 0
		if refs, err := s.terminalRefs(); err == nil {
			terminalsBefore = len(refs)
		}

		// xdotool rather than raw XTEST: it remaps keycodes on the fly for
		// characters the active layout has no key for, which command lines are
		// full of (pipes, braces, quotes).
		if err := s.xdo("type", "--clearmodifiers", "--", command); err != nil {
			return textContent("could not type the command: %v", err), true, true
		}
		// xdotool for Return too. InputInjector.Key looks the keysym up in the
		// live keymap and returns silently when it is not there, so a missing
		// mapping produces a command that is typed and never run — exactly the
		// kind of mute failure this whole tool exists to eliminate.
		if err := s.xdo("key", "--clearmodifiers", "Return"); err != nil {
			return textContent("could not press Return: %v", err), true, true
		}

		// Wait for the prompt to come back AND the text to settle. The prompt
		// alone is not enough: it is still on screen from the previous command
		// during the instant before the new one echoes.
		deadline := time.Now().Add(timeout)
		var last string
		stable := 0
		settled := false
		closed := false
		tick := 0
		for time.Now().Before(deadline) {
			if !sleepCtx(ctx, 250*time.Millisecond) {
				break
			}
			tick++

			// Once a second: cheap next to the poll it guards, and a second of
			// extra waiting is invisible beside the two minutes it replaces.
			if terminalsBefore > 0 && tick%4 == 0 {
				if refs, err := s.terminalRefs(); err == nil && len(refs) < terminalsBefore {
					closed = true
					break
				}
			}

			now, err := s.readTerminal(ref)
			if err != nil {
				continue
			}
			if now == last {
				stable++
			} else {
				stable = 0
				last = now
			}
			// Two quiet rounds with an idle prompt: half a second of nothing
			// happening, which a finished command always produces.
			if stable >= 2 && promptTail.MatchString(now) && now != before {
				settled = true
				break
			}
		}

		output := strings.TrimPrefix(last, before)
		output = strings.TrimSpace(output)
		res := map[string]any{
			"command":  command,
			"output":   output,
			"finished": settled,
		}
		if code, _, ok := s.readExitCode(started); ok {
			res["exit_code"] = code
			res["succeeded"] = code == 0
		}
		switch {
		case closed:
			// Not a timeout, and not a failure either: the command did what it
			// was asked and the shell it was asked in no longer exists. Saying
			// which of the two happened is the whole point — "may still be
			// running" would be false, and silence would be worse.
			res["terminal_closed"] = true
			res["note"] = "the terminal closed while the command ran, which is what " +
				"`exit`, `logout` and anything that ends the shell do. What it " +
				"printed before the window went away is in `output`; there is no " +
				"prompt left to confirm against, so `finished` stays false."
		case !settled:
			res["note"] = "timed out waiting for the prompt; the command may still " +
				"be running. Call terminal_read to check on it."
		}
		if _, ok := res["exit_code"]; !ok {
			// Without instrumentation the text is all there is, and "no error
			// message" is not the same as success. Say so rather than let it
			// pass for one.
			res["hint"] = "No exit code available — this terminal was not opened " +
				"with terminal_open, so judge by the output alone."
		}
		return res, false, true
	}
	return nil, false, false
}
