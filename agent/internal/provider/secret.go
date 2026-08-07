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

package provider

// Where the key comes from, and where it must never end up.
//
// A file is preferred over an environment variable, and that ordering is the
// point rather than a style choice. An environment variable is readable from
// `docker inspect`, from `/proc/<pid>/environ` by anything running as the same
// user, from a crash dump, and by every child process the runtime ever spawns —
// and this runtime spawns children. A file is readable by whatever can read the
// file.
//
// The default path is outside the repository on purpose. A key that is not in
// the tree cannot be committed by a mistake, a rebase, a `git add -A`, or a
// well-meaning script. Everything else here — the mode check, the redaction, the
// scan — is a second line for a first line that should hold on its own.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DefaultKeyDir is where keys live: outside any checkout, one file per
// provider, readable by the user that runs the agent and nobody else.
const DefaultKeyDir = ".sentineldesk"

// Secret is an API key that knows not to print itself.
//
// A string would work and would eventually be interpolated into a log line, an
// error, or a %v of some struct that happens to contain it. This makes the safe
// thing the default one: fmt prints a redaction, and reading the real value is
// an explicit call that reads like what it is.
type Secret struct {
	value string
	from  string // where it came from, for diagnostics that must not leak it
}

// Reveal returns the actual key. Call it at the point of use — building a
// request header — and nowhere else.
func (s Secret) Reveal() string { return s.value }

// Source says where the key was loaded from. Safe to log.
func (s Secret) Source() string { return s.from }

func (s Secret) Empty() bool { return s.value == "" }

// String, GoString and Format all redact. Three methods because there are three
// ways a value gets printed by accident, and covering two of them is the same
// as covering none.
func (s Secret) String() string   { return s.redacted() }
func (s Secret) GoString() string { return s.redacted() }

func (s Secret) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte(s.redacted()))
	_ = verb
}

// redacted shows enough to tell two keys apart and not enough to use one.
func (s Secret) redacted() string {
	if s.value == "" {
		return "(no key)"
	}
	if len(s.value) < 12 {
		return "(key)"
	}
	return s.value[:7] + "…" + s.value[len(s.value)-4:]
}

// LoadKey finds a provider's key, preferring a file.
//
// In order:
//
//  1. <NAME>_API_KEY_FILE — a path. The Docker secret convention, and the one
//     to use in production.
//  2. ~/.sentineldesk/<name>.key — the default, outside any checkout.
//  3. <NAME>_API_KEY — the environment variable. Convenient, leakier, and last.
//
// A missing key is not an error here. The runtime starts without a provider and
// says the Agent Console is unavailable; it does not refuse to boot, for the
// same reason no /dev/uinput disables the gamepad rather than the desktop.
func LoadKey(name string) (Secret, error) {
	env := strings.ToUpper(name)

	if path := os.Getenv(env + "_API_KEY_FILE"); path != "" {
		return readKeyFile(path, true)
	}

	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, DefaultKeyDir, name+".key")
		if _, err := os.Stat(path); err == nil {
			return readKeyFile(path, true)
		}
	}

	if v := strings.TrimSpace(os.Getenv(env + "_API_KEY")); v != "" {
		return Secret{value: v, from: env + "_API_KEY (environment)"}, nil
	}
	return Secret{}, nil
}

func readKeyFile(path string, checkMode bool) (Secret, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Secret{}, fmt.Errorf("key file %s: %w", path, err)
	}
	// Group or world readable is worth refusing rather than warning about. A
	// warning is read once, at a moment when the person is trying to get
	// something working, and then never again.
	if checkMode && info.Mode().Perm()&0o077 != 0 {
		return Secret{}, fmt.Errorf(
			"key file %s is mode %04o: readable by others. chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Secret{}, fmt.Errorf("key file %s: %w", path, err)
	}
	// A file written by `echo` ends in a newline, and a key with a newline in it
	// fails authentication with a message about the key rather than about the
	// newline — an hour lost to a whitespace character.
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return Secret{}, fmt.Errorf("key file %s is empty", path)
	}
	return Secret{value: value, from: path}, nil
}

// keyShaped matches the credential formats that must never reach the tree. It
// is deliberately narrow: a pattern that matches too much gets ignored, and one
// that is ignored is worth nothing.
var keyShaped = regexp.MustCompile(`(sk-ant-[A-Za-z0-9_-]{16,}|sk-[A-Za-z0-9]{32,}|AIza[A-Za-z0-9_-]{20,})`)

// ScanForKeys reports credential-shaped strings found in the given text.
//
// The second line, not the first. The first is that the key lives outside the
// checkout; this catches the case where somebody pastes one into a config file,
// a test fixture or a comment while getting something working, and then forgets.
func ScanForKeys(text string) []string {
	var found []string
	for _, m := range keyShaped.FindAllString(text, -1) {
		// Report the shape, never the value. A tool that helpfully prints the
		// leaked key into CI output has moved the leak rather than found it.
		found = append(found, m[:min(10, len(m))]+"… ("+fmt.Sprint(len(m))+" chars)")
	}
	return found
}
