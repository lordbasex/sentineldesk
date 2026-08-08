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

// Package skills finds the instructions somebody wrote for a kind of task.
//
// A skill is a directory with a SKILL.md in it, carrying YAML frontmatter with
// a name and a description and Markdown instructions after it. That layout is
// not ours: it is what Claude Code and opencode both look for, and following it
// means a skill somebody already wrote works here without being rewritten, and
// one written here works there. A convention with two implementations is worth
// more than a better one with a single implementation, and this project is not
// the place to spend that difference.
//
// So the same directories are searched, theirs included — walking UP from the
// working directory to the top of the git worktree, then the home directory:
//
//	<dir>/.sentineldesk/skills/          ours
//	<dir>/.opencode/skills/              opencode's
//	<dir>/.claude/skills/                Claude Code's
//	<dir>/.agents/skills/                the neutral one
//	~/.sentineldesk/skills/              everywhere, ours
//	~/.config/opencode/skills/           everywhere
//	~/.claude/skills/                    everywhere
//	~/.agents/skills/                    everywhere
//
// Nearest wins: a skill in the directory being worked in beats one further up,
// which beats one in the home directory. That is deliberately NOT the rule the
// v2 specification states — it says later sources override earlier ones — and
// the two published versions contradict each other on it anyway. Specific
// beating general is the rule people already expect from every other
// configuration on their machine, and it is the direction that makes checking a
// skill into a repository worth doing.
//
// # Why only the descriptions go in the prompt
//
// The whole body of every skill would be the catalogue problem again, and this
// project already measured that one: the tool catalogue is 98% of what a turn
// costs, which is why a run offers a core set and reaches the rest through
// tool_search. Skills get the same treatment for the same reason. The model is
// told the name and one line for each, and asks for the body of the one it
// wants — so ten skills cost ten lines rather than ten documents, and the one
// it opens is the one it needed.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill is one directory's worth of instructions.
type Skill struct {
	// Name is the identity, and it comes from the PATH rather than from the
	// frontmatter: <dir>/SKILL.md is named for its directory, a flat foo.md for
	// its filename. The published v1 and v2 specifications disagree here — v1
	// requires a `name` field matching the directory, v2 derives the id from the
	// path and treats `name` as an optional label — and the path is the version
	// that cannot be internally inconsistent. A frontmatter name that disagrees
	// with its directory is a skill somebody will call by the wrong word.
	Name        string
	Label       string   // the frontmatter's `name`, when it said something else
	Description string   // the one line that decides whether it gets opened
	Body        string   // the Markdown after the frontmatter
	Path        string   // where it came from, so `skills` can say
	Dir         string   // what paths inside the body are relative to
	Files       []string // what else is in there, for the model to read on request
	Project     bool     // found beside the work rather than in the home directory
}

// projectDirs are the per-project directory names, nearest-first within one
// level. `.sentineldesk` leads because it is ours; the rest are in the order the
// published convention uses, so a repository carrying skills for more than one
// agent resolves the same way here as it does there.
var projectDirs = []string{".sentineldesk", ".opencode", ".claude", ".agents"}

// searchRoots is where to look, in the order that decides ties.
//
// It WALKS UP from the working directory to the top of the git worktree rather
// than looking only where it was invoked. That matters more than it sounds:
// somebody running the agent from a subdirectory of their project would
// otherwise silently lose every skill the project ships, and the failure looks
// like the feature not working rather than like being in the wrong folder.
//
// The walk stops at the worktree root — found by the .git that marks it — and
// not at the filesystem root. Continuing past it would start picking up skills
// from whatever unrelated directory happens to be above the checkout, which on
// a machine with a flat ~/src is somebody else's project.
func searchRoots() []string {
	var roots []string

	if wd, err := os.Getwd(); err == nil {
		dir := wd
		for {
			for _, d := range projectDirs {
				roots = append(roots, filepath.Join(dir, d, "skills"))
			}
			// The top of the worktree, or the top of the filesystem.
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			filepath.Join(home, ".sentineldesk", "skills"),
			filepath.Join(home, ".config", "opencode", "skills"),
			filepath.Join(home, ".claude", "skills"),
			filepath.Join(home, ".agents", "skills"))
	}
	return roots
}

// Load returns every skill that can be found, deduplicated by name.
//
// A directory that cannot be read is skipped rather than fatal: most of these
// will not exist on any given machine, and that is the normal case rather than
// a problem. A SKILL.md that exists and cannot be PARSED is different — it was
// written on purpose and is being ignored — so it comes back as a problem the
// caller can report.
func Load() ([]Skill, []error) {
	var out []Skill
	var problems []error
	seen := map[string]bool{}

	for _, root := range searchRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		project := !strings.HasPrefix(root, homePrefix())
		for _, e := range entries {
			// Both shapes the convention accepts: a directory holding a
			// SKILL.md, and a flat Markdown file. The directory form is the one
			// worth writing — it is the only one that can carry the scripts and
			// references a real skill needs beside it — but rejecting the flat
			// one would reject skills that work everywhere else.
			var path, name, dir string
			switch {
			case e.IsDir():
				path = filepath.Join(root, e.Name(), "SKILL.md")
				name, dir = e.Name(), filepath.Join(root, e.Name())
			case strings.EqualFold(filepath.Ext(e.Name()), ".md"):
				path = filepath.Join(root, e.Name())
				name, dir = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), root
			default:
				continue
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			s, err := parse(string(raw))
			if err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", path, err))
				continue
			}
			if s.Name != "" && s.Name != name {
				s.Label = s.Name
			}
			s.Name = name
			if seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			s.Path, s.Dir, s.Project = path, dir, project
			if e.IsDir() {
				s.Files = supporting(dir)
			}
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, problems
}

// supporting lists what else is in a skill's directory, up to a sample.
//
// The contents are NOT read. A skill that ships a script and a reference table
// would otherwise put both in every prompt of every turn, which is the same
// mistake the catalogue taught this project once already. The model is told
// what is there and reads one when the instructions tell it to.
func supporting(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || strings.EqualFold(e.Name(), "SKILL.md") {
			continue
		}
		out = append(out, e.Name())
		if len(out) == 10 {
			break
		}
	}
	sort.Strings(out)
	return out
}

func homePrefix() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "\x00never"
	}
	return home
}

// parse splits the frontmatter from the body.
//
// Deliberately not a YAML library. The frontmatter this format uses is two
// scalar fields, and the two agents that read it in the wild accept exactly
// that; pulling in a parser to read `name:` and `description:` would add a
// dependency to the binary for a shape that fits in twenty lines. If the
// frontmatter ever grows nesting, this should become a real parser rather than
// grow special cases — that is the line, and it is written down so somebody
// notices when it is crossed.
func parse(raw string) (Skill, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return Skill{}, fmt.Errorf("no YAML frontmatter: a SKILL.md starts with a --- line")
	}
	rest := raw[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Skill{}, fmt.Errorf("the frontmatter is never closed by a --- line")
	}
	head, body := rest[:end], rest[end+4:]

	var s Skill
	for _, line := range strings.Split(head, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		switch strings.TrimSpace(key) {
		case "name":
			s.Name = value
		case "description":
			s.Description = value
		}
	}
	// Unknown fields are ignored rather than rejected — license, compatibility
	// and metadata are all in the published schema and none of them change what
	// this runtime does with a skill.
	if s.Description == "" {
		return Skill{}, fmt.Errorf("no description: without one the agent has no way to know when to use this")
	}
	s.Body = strings.TrimSpace(strings.TrimPrefix(body, "\n"))
	return s, nil
}

// Catalogue is the block that goes in the system prompt: one line each, and how
// to ask for more. Empty when there are no skills, so a machine with none pays
// nothing for the feature.
func Catalogue(list []Skill) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Skills\n\nInstructions somebody wrote for specific kinds of work. " +
		"Only the summaries are here. When one of them covers what you are about to do, " +
		"call `skill_read` with its name to get the instructions BEFORE starting — they " +
		"exist because somebody already learned how this goes wrong.\n\n")
	for _, s := range list {
		fmt.Fprintf(&b, "- **%s** — %s\n", s.Name, s.Description)
	}
	return b.String()
}

// Find returns one by name.
func Find(list []Skill, name string) (Skill, bool) {
	for _, s := range list {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return Skill{}, false
}
