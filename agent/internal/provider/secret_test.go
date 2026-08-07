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

// A leaked key is not a bug you fix; it is a key you rotate and an audit you
// run. So the tests here are about the accidents rather than the happy path:
// printing a struct that happens to contain one, a file somebody chmod 644'd
// while debugging, a trailing newline from `echo`.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeKey = "sk-ant-api03-NOTAREALKEY000000000000000000000000000"

// TestASecretDoesNotPrintItself covers the accident that actually happens: not
// somebody logging the key deliberately, but a %v of a struct that contains one.
func TestASecretDoesNotPrintItself(t *testing.T) {
	s := Secret{value: fakeKey, from: "test"}

	// Every way a value reaches a log.
	for _, got := range []string{
		fmt.Sprintf("%v", s),
		fmt.Sprintf("%s", s),
		fmt.Sprintf("%q", s),
		fmt.Sprintf("%#v", s),
		fmt.Sprint(s),
		fmt.Sprintf("%v", struct{ Key Secret }{s}),
		fmt.Sprintf("%+v", []Secret{s}),
	} {
		if strings.Contains(got, "NOTAREALKEY") {
			t.Errorf("the key printed itself: %s", got)
		}
	}
	// And enough survives to tell two keys apart in a diagnostic.
	if !strings.Contains(s.String(), "sk-ant-") {
		t.Errorf("the redaction says nothing at all: %s", s)
	}
	if s.Reveal() != fakeKey {
		t.Error("Reveal did not return the key")
	}
}

func TestAKeyFileMustNotBeReadableByOthers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic.key")
	if err := os.WriteFile(path, []byte(fakeKey), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ANTHROPIC_API_KEY_FILE", path)
	_, err := LoadKey("anthropic")
	if err == nil {
		t.Fatal("a world-readable key file was accepted")
	}
	// The message has to say what to do. A refusal that leaves somebody
	// guessing is one they work around with an environment variable.
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("the refusal does not say how to fix it: %v", err)
	}
	if strings.Contains(err.Error(), "NOTAREALKEY") {
		t.Error("the error leaked the key it was refusing to load")
	}
}

// TestTrailingWhitespaceIsStripped. A file written with `echo` ends in a
// newline, and a key with a newline fails authentication with a message about
// the key rather than about the newline — an hour lost to whitespace.
func TestTrailingWhitespaceIsStripped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic.key")
	if err := os.WriteFile(path, []byte(fakeKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY_FILE", path)

	got, err := LoadKey("anthropic")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if got.Reveal() != fakeKey {
		t.Errorf("the key came back as %q", got.Reveal())
	}
}

// TestTheFileWinsOverTheEnvironment. An environment variable is readable from
// docker inspect, /proc/<pid>/environ, and every child process. When both
// exist, the less leaky one has to be the one used.
func TestTheFileWinsOverTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic.key")
	if err := os.WriteFile(path, []byte("from-the-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY_FILE", path)
	t.Setenv("ANTHROPIC_API_KEY", "from-the-environment")

	got, err := LoadKey("anthropic")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if got.Reveal() != "from-the-file" {
		t.Errorf("the environment won: %q", got.Reveal())
	}
}

// TestNoKeyIsNotAnError. The runtime starts without a provider and says the
// Agent Console is unavailable; it does not refuse to boot, for the same reason
// a missing /dev/uinput disables the gamepad rather than the desktop.
func TestNoKeyIsNotAnError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY_FILE", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("HOME", t.TempDir())

	got, err := LoadKey("anthropic")
	if err != nil {
		t.Fatalf("a missing key was an error: %v", err)
	}
	if !got.Empty() {
		t.Error("a key appeared from nowhere")
	}
}

// TestUnavailableSaysHowToFixIt. "It is unavailable" should never require
// reading the source to act on.
func TestUnavailableSaysHowToFixIt(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY_FILE", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("HOME", t.TempDir())

	_, err := NewAnthropic("")
	if err == nil {
		t.Fatal("a provider with no key was built anyway")
	}
	if !IsUnavailable(err) {
		t.Errorf("not configured came back as broken: %v", err)
	}
	if !strings.Contains(err.Error(), ".sentineldesk/anthropic.key") {
		t.Errorf("the message does not name the file to create: %v", err)
	}
}

// TestScanFindsKeyShapesWithoutPrintingThem. The scan is the second line of
// defence; a scanner that helpfully prints the key it found has moved the leak
// rather than caught it.
func TestScanFindsKeyShapesWithoutPrintingThem(t *testing.T) {
	found := ScanForKeys("ANTHROPIC_API_KEY=" + fakeKey + " and nothing else")
	if len(found) == 0 {
		t.Fatal("a key in a config line was not found")
	}
	if strings.Contains(strings.Join(found, " "), "NOTAREALKEY") {
		t.Errorf("the scanner printed the key: %v", found)
	}
	if len(ScanForKeys("nothing to see here, sk- is not a key")) != 0 {
		t.Error("the scanner matched something that is not a key")
	}
}
