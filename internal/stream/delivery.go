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

package stream

// Delivering a file from the container to the person watching.
//
// Screenshots and recordings can end up in two different places, and which one
// is right depends entirely on who asked:
//
//   - container: the file stays on the desktop's disk. This is what an agent
//     wants — it has no browser, and it may well want to keep working with the
//     file (re-encode it, ship it over SSH, attach it somewhere).
//   - download: the browser saves it on the machine of whoever is watching.
//     This is what a person wants — a screenshot that lands inside a container
//     they then have to go and fetch is barely a screenshot at all.
//
// Both capture from the same source, so the quality is identical; only the
// destination differs. The delivery itself reuses the file manager's one-use
// ticket: the browser is told a URL that works once, for sixty seconds, for
// that one file.

import (
	"encoding/json"
	"path/filepath"
)

// Delivery hands a file on the desktop's disk to the connected browsers.
type Delivery struct {
	files *FileServer
	room  *Room
}

func NewDelivery(files *FileServer, room *Room) *Delivery {
	return &Delivery{files: files, room: room}
}

// Deliver tells the browsers to download the file at absPath.
//
// It goes to whoever holds control, because that is the person who asked — or,
// when nobody does, to everyone present. The count of clients told comes back
// so the caller can say "saved on the desktop, nobody was watching" instead of
// silently doing nothing.
func (d *Delivery) Deliver(absPath, name string) int {
	if d == nil || d.files == nil || d.room == nil {
		return 0
	}
	if name == "" {
		name = filepath.Base(absPath)
	}

	// The ticket is minted against the real path, bypassing FILES_ROOT: this is
	// the server handing over a file it just produced, not the browser reaching
	// into the filesystem, so the confinement that protects the file manager
	// does not apply here.
	ticket := d.files.newTicket(absPath)
	msg, err := json.Marshal(map[string]any{
		"t":    "download",
		"url":  "/files/download?t=" + ticket,
		"name": name,
	})
	if err != nil {
		return 0
	}

	d.room.mu.RLock()
	targets := d.room.snapshotMembers()
	controller := d.room.controller
	d.room.mu.RUnlock()

	sent := 0
	for _, m := range targets {
		if controller != "" && m.id != controller {
			continue
		}
		m.session.sendOnChannel(string(msg))
		sent++
	}
	return sent
}
