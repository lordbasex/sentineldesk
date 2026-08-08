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

// Package prompts holds what the agent is told, as Markdown rather than as Go
// string literals.
//
// The prompt is the highest-leverage thing in an agent runtime and it was the
// hardest thing here to change: a sentence lived in a `b.WriteString(...)` in
// the middle of the run loop, so trying a different wording meant a rebuild, and
// somebody who wanted their agent to behave differently had to fork the binary.
// Prose for a model is not code, and keeping it in code made it behave like
// code — reviewed as a diff of quoted strings, versioned with the compiler, and
// invisible to the person whose desktop it is.
//
// Embedded AND overridable, which is the same shape pricing.json already has in
// this project and for the same reason. Embedded, the binary works with no
// files beside it and the defaults cannot drift from the code that expects
// them. Overridable, a person can edit ~/.sentineldesk/prompts/system.md and
// see the change on the next run without building anything.
//
// The roles are a directory rather than a list, so adding a role is adding a
// file. That is the deliberate difference from deploy/embed.go, which names
// every file explicitly because a missing one there ships a broken tree in
// silence; here a missing role fails loudly at load, on the run that asked for
// it, naming the file it could not find.
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed system.md roles
var builtin embed.FS

// Dir is where an override may live. Empty means ~/.sentineldesk/prompts.
var Dir string

func overrideDir() string {
	if Dir != "" {
		return Dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".sentineldesk", "prompts")
}

// read returns the override if there is one, otherwise what was compiled in.
//
// A file on disk wins outright rather than being appended to the default. The
// alternative — merging — produces a prompt nobody wrote: the built-in rules
// plus somebody's replacement for them, contradicting each other, in an order
// neither of them chose.
func read(name string) (string, error) {
	if dir := overrideDir(); dir != "" {
		if raw, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return string(raw), nil
		}
	}
	raw, err := builtin.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("prompt %s: %w", name, err)
	}
	return string(raw), nil
}

// System is the base prompt every run starts from.
func System() (string, error) { return read("system.md") }

// Role is the block appended for a named role. An unknown role is an error and
// not an empty string: a typo in --role would otherwise run the agent with
// half the instructions it was meant to have and say nothing about it.
func Role(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if strings.ContainsAny(name, `/\.`) {
		return "", fmt.Errorf("role %q: a role is a name, not a path", name)
	}
	return read(filepath.Join("roles", name+".md"))
}

// Roles lists what can be asked for, from both places, so `--role` can say what
// the choices actually are on this machine rather than what was compiled in.
func Roles() []string {
	seen := map[string]bool{}
	add := func(name string) {
		if strings.HasSuffix(name, ".md") {
			seen[strings.TrimSuffix(name, ".md")] = true
		}
	}
	if entries, err := fs.ReadDir(builtin, "roles"); err == nil {
		for _, e := range entries {
			add(e.Name())
		}
	}
	if dir := overrideDir(); dir != "" {
		if entries, err := os.ReadDir(filepath.Join(dir, "roles")); err == nil {
			for _, e := range entries {
				add(e.Name())
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
