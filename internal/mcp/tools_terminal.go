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
// round. This closes it: the command goes into the real terminal so the person
// sees it happen, and the output comes back as the exact characters the
// terminal holds, not OCR of a screenshot.
//
// Both halves go through tmux, running inside the graphical terminal, rather
// than through the X server and the accessibility tree. The person sees no
// difference — tmux writes to the same pty the emulator is drawing — but three
// failure modes disappear with the old route:
//
//	typing      xdotool needed the window focused and the keyboard layout to
//	            hold every character of the command line. tmux send-keys needs
//	            neither: it writes to the pty, so a person clicking elsewhere
//	            mid-command no longer redirects half of it into another window.
//	addressing  accessibility refs are positional paths, so a closed window did
//	            not invalidate its ref — the path started resolving to whatever
//	            moved into its place and returned somebody else's text forever.
//	            A pane id is a handle: it dies with the pane it names.
//	reading     each read spawned a fresh python3, imported the AT-SPI bindings
//	            and walked the tree — 99ms, fired four times a second for the
//	            whole length of every command. capture-pane is 3.75ms.
//
// The waiting is the part that matters. Reading right after pressing Return
// returns the command line and nothing else, so this waits for the pane to go
// back to running the shell itself and for the text to stop changing.
//
// Reading somebody ELSE'S terminal still goes through accessibility, and has
// to: a terminal a person opened from the menu is not under tmux, and refusing
// to read it would give up the case this file exists for. Typing into one is a
// different matter — see terminal_run.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
			Visibility:      visInjects,
			Risk:            riskDanger,
			RequiresControl: true,
			Description: "Run a command in a terminal window ON THE DESKTOP, wait " +
				"for it to finish, and return what it printed. Use this instead of " +
				"run_command when a person is watching: they see the command and its " +
				"output exactly as if you had typed it. The text returned is the " +
				"exact characters the terminal holds, not OCR.",
			InputSchema: schema(map[string]any{
				"command":    pStr("the command line to run"),
				"timeout_ms": pInt("how long to wait for the prompt (default 120000)"),
			}, "command"),
		},
		{
			Name:            "terminal_open",
			Visibility:      visVisible,
			Risk:            riskDanger,
			RequiresControl: true,
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

// --- the tmux control channel -----------------------------------------------

// tmuxSession is the one session every terminal opened here attaches to. A
// named session on the default socket rather than a private one (-L): a person
// who wants to join from their own shell types `tmux attach -t sentineldesk`
// and is in, which is the same collaborative property the graphical window has.
const tmuxSession = "sentineldesk"

// shellCommands are the process names that mean the pane is idle — the shell is
// the foreground process, so whatever was running has finished. This is the
// signal the old code approximated by matching a prompt against a regex, and it
// is strictly better: a command that prints something ending in `$` no longer
// reads as a returned prompt, and a prompt that does not end in one of the
// expected characters no longer reads as a command still running.
//
// It is still paired with a settled-text check below rather than trusted alone,
// because a person who types `bash` gets a pane whose foreground process is a
// shell without the previous command having finished in any meaningful sense.
var shellCommands = map[string]bool{
	"bash": true, "sh": true, "dash": true, "zsh": true, "fish": true,
}

func (s *Server) tmux(args ...string) (string, error) {
	out, err := s.output("tmux", args...)
	return strings.TrimRight(out, "\n"), err
}

// activePane is the pane the person is looking at: the active pane of the
// session's active window. Deliberately not "the most recently created" — when
// somebody splits the window, the one they are working in is the one an agent
// should be reading and typing into.
func (s *Server) activePane() (string, error) {
	pane, err := s.tmux("display-message", "-p", "-t", tmuxSession, "#{pane_id}")
	if err != nil || strings.TrimSpace(pane) == "" {
		return "", fmt.Errorf("no terminal is open under tmux")
	}
	return strings.TrimSpace(pane), nil
}

// capturePane returns what the pane holds. scrollback asks for that many lines
// of history above the visible area; 0 is the visible area alone.
//
// Trailing newlines go, for the same reason the accessibility reader stripped
// them: the pane is padded to its full height and those empty rows would defeat
// every "has the output stopped changing" comparison below.
func (s *Server) capturePane(pane string, scrollback int) (string, error) {
	args := []string{"capture-pane", "-p", "-t", pane}
	if scrollback > 0 {
		args = append(args, "-S", "-"+strconv.Itoa(scrollback))
	}
	out, err := s.tmux(args...)
	if err != nil {
		return "", fmt.Errorf("could not read the terminal: %v", err)
	}
	return strings.TrimRight(out, " \t\n"), nil
}

// paneIdle reports whether the pane's foreground process is a shell.
func (s *Server) paneIdle(pane string) bool {
	out, err := s.tmux("display-message", "-p", "-t", pane, "#{pane_current_command}")
	if err != nil {
		return false
	}
	return shellCommands[strings.TrimSpace(out)]
}

// sessionAlive reports whether the tmux session still exists. It is how a
// command that ended the shell is told apart from one that is merely slow:
// `exit`, `logout` and anything else that closes the last pane takes the
// session with it, and there is then no prompt coming to wait for.
func (s *Server) sessionAlive() bool {
	_, err := s.tmux("has-session", "-t", tmuxSession)
	return err == nil
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

// rcPathFor is where a particular pane's shell leaves its status. Panes get a
// file each because the record is last-writer-wins: with one shared file, a
// person running something in a split pane would overwrite the status of the
// command the agent just ran, and the agent would report their exit code as its
// own. That is the mute-failure class this file exists to close, so it does not
// get to come back through the side door.
//
// The unsuffixed path stays as the fallback, and is what a shell outside tmux
// still writes — a terminal opened from the panel menu keeps reporting.
func rcPathFor(pane string) string {
	if pane == "" {
		return rcPath
	}
	return rcPath + "." + strings.TrimPrefix(pane, "%")
}

// readExitCode returns the status the shell recorded, the command it belonged
// to, and whether the record is fresh enough to trust.
//
// The hook is installed for every interactive shell on the desktop, so this
// works for what a PERSON typed just as well as for what the agent typed. That
// symmetry is the whole point: somebody hits an error, asks the agent to sort it
// out, and the agent reads what actually happened rather than a retelling.
func (s *Server) readExitCode(pane string, since time.Time) (int, string, bool) {
	path := rcPathFor(pane)
	fi, err := os.Stat(path)
	if err != nil && pane != "" {
		// A shell that predates the per-pane hook, or one started outside tmux
		// and later attached. Its status is in the shared file and is still
		// worth reading; it is only ambiguous when several panes are busy.
		path = rcPath
		fi, err = os.Stat(path)
	}
	if err != nil || fi.ModTime().Before(since) {
		return 0, "", false
	}
	raw, err := os.ReadFile(path)
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
		// Every stale status file, not just the shared one: a pane id is reused
		// once its predecessor is gone, so a leftover file would hand the next
		// pane the exit code of a command that ran before this terminal existed.
		if stale, err := filepath.Glob(rcPath + "*"); err == nil {
			for _, f := range stale {
				_ = os.Remove(f)
			}
		}

		// Already showing? tmux knows without asking X: a session with an
		// attached client is a session somebody can see. Reporting it rather
		// than stacking a second window matches browser_open, and a second
		// client would only mirror the first anyway — both would be forced to
		// the smaller of the two window sizes.
		if s.sessionAlive() {
			if clients, err := s.tmux("list-clients", "-t", tmuxSession); err == nil &&
				strings.TrimSpace(clients) != "" {
				pane, _ := s.activePane()
				return map[string]any{
					"opened": true, "already_open": true, "exit_codes": true,
					"pane": pane,
					"note": "a terminal was already open; this is it. Use `sudo -E su` " +
						"rather than `sudo su` to keep exit codes across a root shell.",
				}, false, true
			}
		} else {
			if _, err := s.tmux("new-session", "-d", "-s", tmuxSession); err != nil {
				return textContent("could not start the terminal session: %v", err), true, true
			}
			// Give the window back its title. The emulator names the window
			// after the process it was told to run, which is now `tmux` — so
			// every terminal on the desktop was called "tmux" regardless of what
			// it was doing. That is a loss for the person reading the taskbar
			// and for the agent reading list_windows, where the title is often
			// the only thing distinguishing two windows of the same class.
			//
			// Failure here is not fatal: a plainly-titled terminal still works,
			// and refusing to open one over a cosmetic setting would be worse
			// than the cosmetic problem.
			_, _ = s.tmux("set-option", "-t", tmuxSession, "set-titles", "on")
			_, _ = s.tmux("set-option", "-t", tmuxSession, "set-titles-string",
				"#{pane_current_command} — #{pane_current_path}")
		}

		// The emulator attaches to that session rather than running a shell of
		// its own. What the person sees is unchanged; what it buys is that the
		// shell is addressable by a handle instead of by its position in the
		// accessibility tree, and readable without walking that tree at all.
		cmd := exec.Command("setsid", "lxterminal", "-e",
			"tmux", "attach", "-t", tmuxSession)
		cmd.Env = append(os.Environ(), "DISPLAY="+s.display)
		if err := cmd.Start(); err != nil {
			return textContent("could not open a terminal: %v", err), true, true
		}
		go cmd.Wait()

		// Wait for the shell to draw its first prompt, otherwise the caller
		// types into a window that is not ready yet. Polled at 100ms rather
		// than the 400ms this used to need: each check is one capture-pane, so
		// the loop costs less now at four times the rate.
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if !sleepCtx(ctx, 100*time.Millisecond) {
				break
			}
			pane, err := s.activePane()
			if err != nil {
				continue
			}
			if txt, err := s.capturePane(pane, 0); err == nil && promptTail.MatchString(txt) {
				return map[string]any{
					"opened": true, "exit_codes": true, "pane": pane,
					"note": "exit codes are reported. Use `sudo -E su` rather than " +
						"`sudo su` to keep them across a root shell.",
				}, false, true
			}
		}
		return textContent("the terminal did not show a prompt in time"), true, true
	case "check_errors":
		// One walk of the whole tree rather than a find per role, because the
		// question cannot be answered from the dialog element alone.
		//
		// A toolkit puts the message in a child label. zenity --error --text=…
		// produces a dialog whose own name is the title and whose text is
		// empty, so an application that titles its error box with its own name
		// — which is most of them — was invisible here: the wording that says
		// it failed was one level down, in a child this never looked at.
		//
		// Refs are paths ("2/0/3"), so a descendant is a ref with the dialog's
		// as its prefix. That makes the subtree free once the tree is in hand,
		// and it is why this is one subprocess now instead of two.
		out, err := s.a11yRaw("tree", "--depth", "14", "--limit", "1200")
		if err != nil {
			return textContent("could not read the accessibility tree: %v", err), true, true
		}
		var tree struct {
			Elements []struct {
				Ref  string `json:"ref"`
				Role string `json:"role"`
				Name string `json:"name"`
				Text string `json:"text"`
			} `json:"elements"`
		}
		if json.Unmarshal([]byte(out), &tree) != nil {
			return textContent("the accessibility bridge did not return a tree: %s",
				strings.TrimSpace(out)), true, true
		}

		found := []map[string]any{}
		seen := map[string]bool{}
		for _, e := range tree.Elements {
			if e.Role != "alert" && e.Role != "dialog" {
				continue
			}
			if seen[e.Ref] {
				continue
			}
			// Everything printed inside this dialog, in tree order, so the
			// message reads the way it does on screen.
			var parts []string
			if t := strings.TrimSpace(e.Text); t != "" {
				parts = append(parts, t)
			}
			prefix := e.Ref + "/"
			for _, d := range tree.Elements {
				if !strings.HasPrefix(d.Ref, prefix) {
					continue
				}
				for _, s := range []string{d.Name, d.Text} {
					if s = strings.TrimSpace(s); s != "" && !slices.Contains(parts, s) {
						parts = append(parts, s)
					}
				}
			}
			body := strings.TrimSpace(e.Name + " " + strings.Join(parts, " "))
			// An alert is worth reporting whatever it says; a dialog only when
			// its wording suggests a failure, or every open file chooser would
			// look like a problem.
			if e.Role != "alert" && !errorish.MatchString(body) {
				continue
			}
			seen[e.Ref] = true
			found = append(found, map[string]any{
				"ref": e.Ref, "role": e.Role, "title": e.Name,
				"text": strings.Join(parts, "\n"),
			})
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
		n := argInt(args, "lines")
		if n <= 0 {
			n = 40
		}

		// tmux first, accessibility second, and the fallback is not a leftover:
		// a terminal a person opened from the menu is not under tmux, and this
		// tool exists precisely for "look at the error I just hit". Refusing to
		// read a window that is plainly on the screen would be the wrong answer
		// to the question it was built to answer.
		var (
			text string
			pane string
			err  error
		)
		if pane, err = s.activePane(); err == nil {
			// One screen of scrollback beyond whatever was asked for, so a
			// request for more lines than the window is tall still gets them.
			text, err = s.capturePane(pane, n)
		}
		if err != nil {
			pane = ""
			ref, ferr := s.findTerminal()
			if ferr != nil {
				return textContent("%v", ferr), true, true
			}
			if text, err = s.readTerminal(ref); err != nil {
				return textContent("%v", err), true, true
			}
		}

		out := map[string]any{"text": lastLines(text, n)}
		if pane != "" {
			out["pane"] = pane
		}
		// Any interactive shell records this, so the last command may well be
		// one a person typed. That is deliberate.
		if code, cmd, ok := s.readExitCode(pane, time.Time{}); ok {
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

		pane, err := s.activePane()
		if err != nil {
			// Say which of the two situations this is. "No terminal is open"
			// while one is plainly on the screen reads as a broken tool and
			// sends the model looking in the wrong place; the real answer is
			// that this one is not ours to type into.
			if refs, ferr := s.terminalRefs(); ferr == nil && len(refs) > 0 {
				return textContent("there is a terminal on the desktop, but it was " +
					"not opened by terminal_open, so there is no reliable way to type " +
					"into it or read its exit codes. Call terminal_open for one this " +
					"can drive, or terminal_read to see what that one is showing."), true, true
			}
			return textContent("no terminal window is open — call terminal_open first"), true, true
		}
		before, err := s.capturePane(pane, 0)
		if err != nil {
			return textContent("%v", err), true, true
		}
		started := time.Now()

		// send-keys rather than xdotool: it writes to the pty instead of the X
		// server, so the command does not depend on the window holding focus or
		// on the keyboard layout having a key for every character in it. Both
		// were real failure modes — a person clicking another window mid-command
		// used to split it across two applications.
		//
		// -l sends the text literally, which matters more than it looks: without
		// it tmux reads its arguments as key names, so a command containing the
		// word `Enter` or a token like `C-c` would be delivered as keystrokes
		// rather than as the characters somebody asked for.
		if _, err := s.tmux("send-keys", "-t", pane, "-l", "--", command); err != nil {
			return textContent("could not send the command: %v", err), true, true
		}
		if _, err := s.tmux("send-keys", "-t", pane, "Enter"); err != nil {
			return textContent("could not press Return: %v", err), true, true
		}

		// Wait for the shell to be the foreground process again AND for the text
		// to settle. Neither alone is enough: the pane is briefly running the
		// shell in the instant between Return and the command starting, and the
		// text is briefly unchanged during any pause in a command's output.
		//
		// Polled at 100ms rather than 250ms. Each round is a capture-pane and a
		// display-message — a few milliseconds against the ~99ms an accessibility
		// read cost — so the loop is both finer-grained and cheaper than the one
		// it replaces.
		deadline := time.Now().Add(timeout)
		var last string
		stable := 0
		settled := false
		closed := false
		for time.Now().Before(deadline) {
			if !sleepCtx(ctx, 100*time.Millisecond) {
				break
			}

			now, err := s.capturePane(pane, 0)
			if err != nil {
				// The pane is gone. Some commands end the shell — `exit`,
				// `logout`, anything that closes the last pane — and those leave
				// no prompt to wait for; without noticing, this would spend the
				// whole timeout waiting for one that is never coming.
				//
				// A pane id is a handle, so its disappearance IS the signal.
				// The accessibility route could not do this: refs are positional
				// paths, so a closed window did not produce an error, it started
				// resolving to whatever moved into its place — which is why the
				// old code had to count terminals on a side channel instead.
				if !s.sessionAlive() {
					closed = true
					break
				}
				continue
			}
			if now == last {
				stable++
			} else {
				stable = 0
				last = now
			}
			// Two quiet rounds with the shell back in the foreground: a fifth of
			// a second of nothing happening, which a finished command always
			// produces.
			if stable >= 2 && s.paneIdle(pane) && now != before {
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
		if code, _, ok := s.readExitCode(pane, started); ok {
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
				"shell left to confirm against, so `finished` stays false."
		case !settled:
			res["note"] = "timed out waiting for the command to finish; it may still " +
				"be running. Call terminal_read to check on it."
		}
		if _, ok := res["exit_code"]; !ok && !closed {
			// Without instrumentation the text is all there is, and "no error
			// message" is not the same as success. Say so rather than let it
			// pass for one.
			//
			// Not when the shell closed, though. A shell that exits never runs
			// its prompt hook again, so there is no status to find and that is
			// expected — printing "this terminal was not opened with
			// terminal_open" there is simply false, and a false explanation is
			// worse than none: it sends the caller to fix something that is not
			// broken. The `note` above already says what happened.
			res["hint"] = "No exit code available — this terminal was not opened " +
				"with terminal_open, so judge by the output alone."
		}
		return res, false, true
	}
	return nil, false, false
}
