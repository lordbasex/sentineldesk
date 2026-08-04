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

package desktop

import (
	"bytes"
	"os/exec"
	"strings"
)

// Clipboard reads and writes the X CLIPBOARD selection through xclip.
//
// Deduplication — not echoing back what the browser just sent — is a per-session
// concern and lives with the session, not here.
type Clipboard struct {
	display string
}

func NewClipboard(display string) *Clipboard {
	return &Clipboard{display: display}
}

// Get reads the CLIPBOARD selection.
func (c *Clipboard) Get() (string, bool) {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-o", "-display", c.display)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		// Either the clipboard is empty or nothing currently owns the
		// selection; both are ordinary, not errors worth reporting.
		return "", false
	}
	return out.String(), true
}

// Set puts text on the CLIPBOARD selection.
func (c *Clipboard) Set(text string) {
	cmd := exec.Command("xclip", "-selection", "clipboard", "-i", "-display", c.display)
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}
