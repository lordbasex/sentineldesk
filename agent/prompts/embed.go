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
	"strconv"
	"strings"

	"github.com/lordbasex/sentineldesk/agent/internal/frontmatter"
)

//go:embed system.md roles perception
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

// RoleDef is a role: prose for the model, plus the settings a run should start
// from when somebody asks for it.
//
// The settings are the reason this grew a header. A role was a paragraph
// appended to the prompt, which meant "run read-only" was a sentence ASKING the
// model not to touch anything — and a request is not a control. Policy here is
// handed to the server, which refuses the call; the model's cooperation stops
// being part of the mechanism.
type RoleDef struct {
	Name        string
	Description string // shown by `roles`, so a choice can be made without opening files

	// Model overrides which model runs, as provider/model — "anthropic/claude-sonnet-5",
	// or just "qwen2.5:3b" to keep the provider and change the model.
	Model string

	// Policy narrows what the connection may call: readonly | safe | full.
	// Deny and Allow narrow it further, by tool name. All three are sent to the
	// server, which may only ever narrow — a role cannot widen past the
	// daemon's own ceiling, which is what makes handing one out safe.
	Policy string
	Deny   []string
	Allow  []string

	// Tools is how many goal-matched tools to offer on top of the core set.
	// Zero means "not set by this role"; -1 means "offer everything".
	Tools int

	Body string
}

// Role reads one. An unknown role is an error and not an empty string: a typo
// in --role would otherwise run the agent with half the instructions it was
// meant to have and say nothing about it.
//
// A role file with no frontmatter is still a role. The header carries settings,
// and a role that only wants to say something to the model should not have to
// write an empty one.
func Role(name string) (RoleDef, error) {
	if name == "" {
		return RoleDef{}, nil
	}
	if strings.ContainsAny(name, `/\.`) {
		return RoleDef{}, fmt.Errorf("role %q: a role is a name, not a path", name)
	}
	raw, err := read(filepath.Join("roles", name+".md"))
	if err != nil {
		return RoleDef{}, err
	}
	def := RoleDef{Name: name}
	fields, body, ferr := frontmatter.Split(raw)
	if ferr != nil {
		def.Body = strings.TrimSpace(raw)
		return def, nil
	}
	def.Body = body
	def.Description = fields["description"]
	def.Model = fields["model"]
	def.Policy = fields["policy"]
	def.Deny = frontmatter.List(fields["deny"])
	def.Allow = frontmatter.List(fields["allow"])
	if v := fields["tools"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			def.Tools = n
		} else if strings.EqualFold(v, "all") {
			def.Tools = -1
		}
	}
	switch def.Policy {
	case "", "readonly", "safe", "full":
	default:
		return RoleDef{}, fmt.Errorf("role %q: policy %q is not one of readonly, safe, full",
			name, def.Policy)
	}
	return def, nil
}

// Perception is the block that tells the model what it can and cannot see.
//
// Keyed off a capability rather than written into the base prompt, because the
// answer changes per provider and will change again for all of them: the
// desktop already returns a screenshot as an MCP image block and the runtime
// discards it, so the day that stops being true this switches without a word of
// the base prompt moving.
//
// Two files rather than one-with-a-conditional. A prompt that says "if you can
// see, do X, otherwise do Y" spends tokens telling a model about a situation it
// is not in, and invites it to reason about which one applies — which is a
// question the runtime already knows the answer to.
func Perception(canSee bool) (string, error) {
	name := "blind.md"
	if canSee {
		name = "sighted.md"
	}
	return read(filepath.Join("perception", name))
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
