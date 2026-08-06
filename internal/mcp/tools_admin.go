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

// Administration tools: privileges, packages and services.
//
// A real desktop is not just moving the mouse around. It is being able to
// install what is missing, restart what has wedged, and edit what lives in /etc.
// None of that is possible without root, and an agent without it ends up staring
// at a screen it cannot fix.
//
// The container is the sandbox: the security boundary is the WebSocket login,
// not the inside of the desktop. See docs/mcp.md, Security.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// --- privilege escalation ----------------------------------------------------

// sudoAvailable reports whether escalation without a password is possible. It is
// resolved once, because the answer cannot change while the container lives.
var sudoAvailable = func() bool {
	if _, err := exec.LookPath("sudo"); err != nil {
		return false
	}
	// -n makes sudo fail rather than prompt on a terminal that is not there.
	return exec.Command("sudo", "-n", "true").Run() == nil
}()

// elevate builds the final command. With asRoot=false it is an ordinary
// `sh -c`; with asRoot=true it is wrapped in `sudo -n -E`, where -E preserves
// DISPLAY and friends so a graphical app launched as root can still find the
// screen.
func elevate(ctx context.Context, command string, asRoot bool) (*exec.Cmd, error) {
	if !asRoot {
		return exec.CommandContext(ctx, "sh", "-c", command), nil
	}
	if !sudoAvailable {
		return nil, fmt.Errorf("this image has no passwordless sudo: rebuild it " +
			"(make image) to pick up the privilege layer")
	}
	return exec.CommandContext(ctx, "sudo", "-n", "-E", "sh", "-c", command), nil
}

// runElevated executes and returns stdout, stderr and the exit code.
func (s *Server) runElevated(ctx context.Context, command string, asRoot bool, timeoutMs int) (map[string]any, error) {
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	cmd, err := elevate(ctx, command, asRoot)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(os.Environ(), "DISPLAY="+s.display, "DEBIAN_FRONTEND=noninteractive")
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	res := map[string]any{
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"as_root":   asRoot,
	}
	if ctx.Err() == context.DeadlineExceeded {
		res["timed_out"] = true
	}
	return res, nil
}

// --- privileged file access --------------------------------------------------
//
// The daemon runs unprivileged, so /etc/shadow or /root are out of reach for
// os.ReadFile. These helpers do the same work by delegating to utilities under
// sudo. The path is always passed as an argument, never interpolated into a
// `sh -c`, so a name containing spaces or quotes breaks nothing.

func requireSudo() error {
	if !sudoAvailable {
		return fmt.Errorf("this image has no passwordless sudo: rebuild it (make image)")
	}
	return nil
}

func rootRead(path string) ([]byte, error) {
	if err := requireSudo(); err != nil {
		return nil, err
	}
	cmd := exec.Command("sudo", "-n", "cat", "--", path)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(firstNonEmpty(stderr.String(), err.Error())))
	}
	return out, nil
}

// rootWrite hands the content to `tee` over stdin, so the text never travels on
// a command line — not ours, and not anyone's `ps` output — and needs no
// escaping.
func rootWrite(path, content string, appendMode bool, mode string) (int, error) {
	if err := requireSudo(); err != nil {
		return 0, err
	}
	args := []string{"-n", "tee"}
	if appendMode {
		args = append(args, "-a")
	}
	args = append(args, "--", path)
	cmd := exec.Command("sudo", args...)
	cmd.Stdin = strings.NewReader(content)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = nil // drop tee's echo
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("%s", strings.TrimSpace(firstNonEmpty(stderr.String(), err.Error())))
	}
	if mode != "" {
		if strings.ContainsAny(mode, "'\"$`;&|<> \n") {
			return 0, fmt.Errorf("invalid mode: %q", mode)
		}
		if err := exec.Command("sudo", "-n", "chmod", mode, "--", path).Run(); err != nil {
			return len(content), fmt.Errorf("written, but chmod %s failed: %v", mode, err)
		}
	}
	return len(content), nil
}

// rootList usa `find -printf`: salida tabulada y estable, a diferencia de `ls`.
func rootList(path string) ([]map[string]any, error) {
	if err := requireSudo(); err != nil {
		return nil, err
	}
	out, err := exec.Command("sudo", "-n", "find", path,
		"-maxdepth", "1", "-mindepth", "1", "-printf", `%y\t%s\t%T@\t%f\n`).Output()
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for _, ln := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.SplitN(ln, "\t", 4)
		if len(f) < 4 {
			continue
		}
		kind := "file"
		switch f[0] {
		case "d":
			kind = "dir"
		case "l":
			kind = "link"
		}
		size := 0
		fmt.Sscanf(f[1], "%d", &size)
		var epoch float64
		fmt.Sscanf(f[2], "%f", &epoch)
		items = append(items, map[string]any{
			"name": f[3], "type": kind, "size": size,
			"modified": time.Unix(int64(epoch), 0).Format(time.RFC3339),
		})
	}
	return items, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// --- catalogue ---------------------------------------------------------------

func (s *Server) buildRootTools() []toolDef {
	return []toolDef{
		{
			Name:        "sudo_status",
			Risk:        riskRead,
			Description: "Report what privilege escalation is available inside the desktop: passwordless sudo, su with a root password, pkexec/polkit, and the current user's groups. Call it first when an action needs root and you want to know how to get there.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "install_packages",
			Risk:        riskDanger,
			Description: "Install Debian packages with apt (as root). Use it to add whatever the task needs — an editor, a compiler, a game, a driver. Returns the apt log and the version actually installed for each package.",
			InputSchema: schema(map[string]any{
				"packages":   pStrArray("package names, e.g. [\"gimp\",\"inkscape\"]"),
				"update":     pBool("run apt-get update first (default true)"),
				"timeout_ms": pInt("timeout in ms (default 300000 — installs are slow)"),
			}, "packages"),
		},
		{
			Name:        "remove_packages",
			Risk:        riskDanger,
			Description: "Uninstall Debian packages with apt (as root). purge=true also deletes their configuration.",
			InputSchema: schema(map[string]any{
				"packages": pStrArray("package names"),
				"purge":    pBool("also remove configuration files (default false)"),
			}, "packages"),
		},
		{
			Name:        "search_packages",
			Risk:        riskRead,
			Description: "Search the Debian archive by name/description and report, for each hit, whether it is already installed and which version is available. Use before install_packages to pick the right package name.",
			InputSchema: schema(map[string]any{
				"query": pStr("search terms"),
				"limit": pInt("max results (default 15)"),
			}, "query"),
		},
		{
			Name:        "service_control",
			Risk:        riskDanger,
			Description: "Manage the desktop's own services through supervisor: X server, PulseAudio, the window manager, the accessibility bus, the WebRTC server. action: status (default), start, stop, restart. Omit `name` to act on everything. Restarting sentineldesk-server drops the live WebRTC session.",
			InputSchema: schema(map[string]any{
				"name":   pStr("service: xvfb, pulseaudio, dbus-session, at-spi, openbox, sentineldesk-server… (omit for all)"),
				"action": pStr("status | start | stop | restart (default status)"),
			}),
		},
	}
}

// pStrArray describes a string-list parameter.
func pStrArray(desc string) map[string]any {
	return map[string]any{
		"type": "array", "description": desc,
		"items": map[string]any{"type": "string"},
	}
}

// argStrList accepts a list, a single string, or a string separated by spaces or
// commas. Models send all three shapes, and rejecting two of them would just be
// a trap.
func argStrList(m map[string]any, k string) []string {
	var out []string
	switch v := m[k].(type) {
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case string:
		for _, s := range strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' }) {
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// --- despacho ---------------------------------------------------------------

func (s *Server) dispatchRoot(ctx context.Context, name string, args map[string]any) ([]map[string]any, bool, bool) {
	switch name {
	case "sudo_status":
		return s.toolSudoStatus()
	case "install_packages":
		return s.toolInstallPackages(ctx, args)
	case "remove_packages":
		return s.toolRemovePackages(ctx, args)
	case "search_packages":
		return s.toolSearchPackages(ctx, args)
	case "service_control":
		return s.toolServiceControl(ctx, args)
	}
	return nil, false, false
}

func (s *Server) toolSudoStatus() ([]map[string]any, bool, bool) {
	out := map[string]any{"sudo_nopasswd": sudoAvailable}

	who, _ := exec.Command("id", "-un").Output()
	out["user"] = strings.TrimSpace(string(who))
	groups, _ := exec.Command("id", "-Gn").Output()
	out["groups"] = strings.Fields(string(groups))

	// Is the root account unlocked? The second field of /etc/shadow is "!" or
	// "*" when there is no usable password, and then `su` is of no help.
	suOK := false
	if data, err := os.ReadFile("/etc/shadow"); err == nil {
		for _, ln := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(ln, "root:") {
				f := strings.SplitN(ln, ":", 3)
				suOK = len(f) > 1 && f[1] != "" && f[1] != "!" && f[1] != "*" && !strings.HasPrefix(f[1], "!")
			}
		}
	} else if sudoAvailable {
		// /etc/shadow is not readable unprivileged, so ask through sudo.
		b, _ := exec.Command("sudo", "-n", "sh", "-c",
			`passwd -S root 2>/dev/null | awk '{print $2}'`).Output()
		suOK = strings.TrimSpace(string(b)) == "P"
	}
	out["su_root"] = suOK

	if _, err := exec.LookPath("pkexec"); err == nil {
		out["pkexec"] = true
	} else {
		out["pkexec"] = false
	}

	// Confirmarlo de verdad en vez de deducirlo.
	if sudoAvailable {
		b, _ := exec.Command("sudo", "-n", "id", "-un").Output()
		out["sudo_resolves_to"] = strings.TrimSpace(string(b))
	}
	out["hint"] = "as_root:true en run_command / launch_app / write_file / read_file, " +
		"or shell_open with user:\"root\" for a persistent root terminal"
	return jsonContent(out), false, true
}

func (s *Server) toolInstallPackages(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	pkgs := argStrList(args, "packages")
	if len(pkgs) == 0 {
		return textContent("install_packages: the `packages` list is missing"), true, true
	}
	// Package names may only contain letters, digits and +-._: — nothing that
	// could close the quoting and smuggle another command into the sh -c.
	for _, p := range pkgs {
		if strings.ContainsAny(p, "'\"$`;&|<>()\n\\ ") {
			return textContent("invalid package name: %q", p), true, true
		}
	}
	timeout := argInt(args, "timeout_ms")
	if timeout <= 0 {
		timeout = 300000
	}
	update := true
	if v, ok := args["update"].(bool); ok {
		update = v
	}
	cmd := ""
	if update {
		cmd = "apt-get update -qq >/dev/null 2>&1; "
	}
	cmd += "apt-get install -y --no-install-recommends " + strings.Join(pkgs, " ")

	res, err := s.runElevated(ctx, cmd, true, timeout)
	if err != nil {
		return textContent("install_packages: %v", err), true, true
	}
	// What matters is not apt's log but which version actually landed.
	versions := map[string]string{}
	for _, p := range pkgs {
		b, _ := exec.Command("dpkg-query", "-W", "-f=${Version}", p).Output()
		v := strings.TrimSpace(string(b))
		if v == "" {
			v = "(no instalado)"
		}
		versions[p] = v
	}
	res["installed"] = versions
	res["log"] = tailLines(fmt.Sprint(res["stdout"]), 25)
	delete(res, "stdout")
	return jsonContent(res), res["exit_code"] != 0, true
}

func (s *Server) toolRemovePackages(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	pkgs := argStrList(args, "packages")
	if len(pkgs) == 0 {
		return textContent("remove_packages: the `packages` list is missing"), true, true
	}
	for _, p := range pkgs {
		if strings.ContainsAny(p, "'\"$`;&|<>()\n\\ ") {
			return textContent("invalid package name: %q", p), true, true
		}
	}
	verb := "remove"
	if v, ok := args["purge"].(bool); ok && v {
		verb = "purge"
	}
	res, err := s.runElevated(ctx, "apt-get "+verb+" -y "+strings.Join(pkgs, " "), true, 180000)
	if err != nil {
		return textContent("remove_packages: %v", err), true, true
	}
	res["log"] = tailLines(fmt.Sprint(res["stdout"]), 20)
	delete(res, "stdout")
	return jsonContent(res), res["exit_code"] != 0, true
}

func (s *Server) toolSearchPackages(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	q := strings.TrimSpace(argStr(args, "query"))
	if q == "" {
		return textContent("search_packages: `query` is missing"), true, true
	}
	limit := argInt(args, "limit")
	if limit <= 0 {
		limit = 15
	}
	ctx, cancel := context.WithTimeout(ctx, 240*time.Second)
	defer cancel()
	// apt-cache search touches no network and needs no root — but images delete
	// /var/lib/apt/lists to stay small, and then there is no index to search and
	// the answer comes back empty without explaining itself. If it is empty,
	// refresh once (which does need the network) and try again.
	search := func() ([]byte, error) {
		return exec.CommandContext(ctx, "apt-cache", "search", "--names-only", q).Output()
	}
	b, err := search()
	if err != nil {
		return textContent("search_packages failed: %v", err), true, true
	}
	refreshed := false
	if len(strings.TrimSpace(string(b))) == 0 && aptListsEmpty() && sudoAvailable {
		if _, e := s.runElevated(ctx, "apt-get update -qq", true, 200000); e == nil {
			refreshed = true
			b, _ = search()
		}
	}
	var results []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if ln == "" || len(results) >= limit {
			continue
		}
		name, desc, _ := strings.Cut(ln, " - ")
		inst, _ := exec.Command("dpkg-query", "-W", "-f=${Version}", name).Output()
		results = append(results, map[string]any{
			"package":     name,
			"description": desc,
			"installed":   strings.TrimSpace(string(inst)),
		})
	}
	out := map[string]any{"query": q, "count": len(results), "results": results}
	if refreshed {
		out["note"] = "the apt index was empty, so apt-get update was run"
	} else if len(results) == 0 && aptListsEmpty() {
		out["note"] = "no apt index and it could not be refreshed: check the container's connectivity"
	}
	return jsonContent(out), false, true
}

// aptListsEmpty reports whether the image was left without a package index, the
// usual consequence of the `rm -rf /var/lib/apt/lists/*` that shrinks images.
func aptListsEmpty() bool {
	entries, err := os.ReadDir("/var/lib/apt/lists")
	if err != nil {
		return true
	}
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), "_Packages") {
			return false
		}
	}
	return true
}

func (s *Server) toolServiceControl(ctx context.Context, args map[string]any) ([]map[string]any, bool, bool) {
	action := strings.ToLower(strings.TrimSpace(argStr(args, "action")))
	if action == "" {
		action = "status"
	}
	switch action {
	case "status", "start", "stop", "restart":
	default:
		return textContent("invalid action %q: use status, start, stop or restart", action), true, true
	}
	name := strings.TrimSpace(argStr(args, "name"))
	if name == "" {
		name = "all"
	}
	if strings.ContainsAny(name, "'\"$`;&|<>()\n\\ ") {
		return textContent("invalid service name: %q", name), true, true
	}
	// The config lives at a different path depending on how this was installed:
	// the image puts it at supervisord.conf, install.sh writes it as
	// sentineldesk.conf so it sits beside whatever else the host supervises.
	// Hardcoding the container's path meant this tool — one of the 114 — failed
	// on every native install, pointing at a file that was not there.
	//
	// First readable wins, and the container's path is checked first because
	// that is the common case. supervisord runs as root with a 0700 socket, so
	// this needs sudo either way.
	conf := "/etc/supervisor/supervisord.conf"
	if _, err := os.Stat(conf); err != nil {
		if _, err := os.Stat("/etc/supervisor/sentineldesk.conf"); err == nil {
			conf = "/etc/supervisor/sentineldesk.conf"
		}
	}
	res, err := s.runElevated(ctx,
		fmt.Sprintf("supervisorctl -c %s %s %s", conf, action, name),
		true, 60000)
	if err != nil {
		return textContent("service_control: %v", err), true, true
	}
	res["action"] = action
	res["service"] = name
	// supervisorctl writes some errors to stdout and still exits 0 — for
	// instance "Error: .ini file does not include supervisorctl section" — so
	// the exit code alone is not enough to tell success from failure.
	out := fmt.Sprint(res["stdout"]) + fmt.Sprint(res["stderr"])
	// `status` exits non-zero when any program is not running, and desktop-init
	// exits on purpose. That is not a failure of the tool.
	failed := strings.HasPrefix(strings.TrimSpace(out), "Error:") ||
		strings.Contains(out, "ERROR (no such process)") ||
		(action != "status" && res["exit_code"] != 0)
	if failed {
		res["error"] = strings.TrimSpace(out)
	}
	return jsonContent(res), failed, true
}

// tailLines keeps the last n lines: with apt, the informative part is the end.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return "…\n" + strings.Join(lines[len(lines)-n:], "\n")
}
