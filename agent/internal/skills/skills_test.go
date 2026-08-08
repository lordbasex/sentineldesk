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

package skills

import "testing"

// The frontmatter parser is hand-written rather than a YAML dependency, which is
// a defensible trade only while something checks it against the shapes real
// files come in. These are those shapes.

func TestParseTakesTheDescriptionAndTheBody(t *testing.T) {
	s, err := parse("---\nname: git-release\ndescription: Cut a release the way this repo expects.\n---\n\n## Instructions\n\n- do the thing\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Description != "Cut a release the way this repo expects." {
		t.Errorf("description = %q", s.Description)
	}
	if s.Body != "## Instructions\n\n- do the thing" {
		t.Errorf("body = %q", s.Body)
	}
}

// Fields the published schema allows and this runtime does nothing with. They
// must not make a skill unloadable — a file that works in the other agents has
// to work here, which is the entire reason for following the convention.
func TestParseIgnoresFieldsItDoesNotUse(t *testing.T) {
	s, err := parse("---\nname: x\ndescription: d\nlicense: MIT\ncompatibility: opencode\nmetadata:\n  team: infra\n---\nbody\n")
	if err != nil {
		t.Fatalf("unknown frontmatter fields made a valid skill unloadable: %v", err)
	}
	if s.Description != "d" {
		t.Errorf("description = %q", s.Description)
	}
}

// A description is what the model sees before deciding to open the file. A skill
// without one can never be chosen, so it is a file somebody wrote that will
// silently never run — the failure class this project ranks worst.
func TestParseRefusesASkillWithNoDescription(t *testing.T) {
	if _, err := parse("---\nname: x\n---\nbody\n"); err == nil {
		t.Fatal("a skill with no description was accepted; nothing would ever invoke it")
	}
}

func TestParseRefusesWhatIsNotASkill(t *testing.T) {
	for _, raw := range []string{
		"# Just a readme\n",
		"---\nname: x\ndescription: d\n", // frontmatter never closed
	} {
		if _, err := parse(raw); err == nil {
			t.Errorf("accepted a file that is not a SKILL.md: %q", raw)
		}
	}
}

func TestParseAcceptsQuotedValuesAndWindowsLineEndings(t *testing.T) {
	s, err := parse("---\r\nname: \"x\"\r\ndescription: 'quoted, with a comma'\r\n---\r\nbody\r\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Description != "quoted, with a comma" {
		t.Errorf("description = %q", s.Description)
	}
}
