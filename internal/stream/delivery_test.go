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

// Regressions for Delivery.Deliver across the four states the room can be in.
//
// Both defects these cover were live: a member without a Session took the whole
// daemon down, and one single-use ticket shared between browsers meant one
// download and the rest dead links. Neither needed WebRTC to happen, and
// neither needs it to test — a Session with no data channel counts as a
// recipient and sends nothing, which is exactly the part under test.

import (
	"testing"
)

// room builds a room without going through NewRoom, which would try to open the
// X display for peer pointers. Members are added directly because Join wants a
// real Session with tracks, and none of that is what these tests are about.
func testRoom(controller string, humans int, withAgent bool) *Room {
	r := &Room{members: map[string]*roomMember{}, controller: controller}
	for i := 1; i <= humans; i++ {
		id := string(rune('a' + i - 1))
		r.members[id] = &roomMember{id: id, session: &Session{}}
		r.order = append(r.order, id)
	}
	if withAgent {
		// The agent as the room really holds it: a member with no Session.
		r.members[agentID] = &roomMember{id: agentID, agent: true}
		r.order = append(r.order, agentID)
	}
	return r
}

func testDelivery(t *testing.T, r *Room) (*Delivery, *FileServer) {
	t.Helper()
	files := NewFileServer(t.TempDir(), nil)
	return NewDelivery(files, r), files
}

func TestDeliverToHumanController(t *testing.T) {
	// "a" holds control with a second person watching: the file is theirs.
	d, files := testDelivery(t, testRoom("a", 2, true))
	if got := d.Deliver("/tmp/shot.png", ""); got != 1 {
		t.Fatalf("delivered to %d, want 1 (the controller)", got)
	}
	if n := len(files.tickets); n != 1 {
		t.Errorf("minted %d tickets, want 1", n)
	}
}

// TestDeliverWithAgentControlling is the panic. The agent holds the controls and
// has no Session, so a loop that sends to the controller dereferences nil.
func TestDeliverWithAgentControlling(t *testing.T) {
	d, files := testDelivery(t, testRoom(agentID, 2, true))
	got := d.Deliver("/tmp/shot.png", "")
	// The agent asked, but "download" can only mean the people watching.
	if got != 2 {
		t.Fatalf("delivered to %d, want 2 (both humans)", got)
	}
	if n := len(files.tickets); n != 2 {
		t.Errorf("minted %d tickets, want 2 — one per recipient", n)
	}
}

// TestDeliverWithControlFree is the same panic by an easier route, and the one
// the earlier audit missed: with no controller the loop reaches every member,
// so the agent only has to be present.
func TestDeliverWithControlFree(t *testing.T) {
	d, files := testDelivery(t, testRoom("", 3, true))
	if got := d.Deliver("/tmp/shot.png", ""); got != 3 {
		t.Fatalf("delivered to %d, want 3 (every browser)", got)
	}
	if n := len(files.tickets); n != 3 {
		t.Errorf("minted %d tickets, want 3 — one per recipient", n)
	}
}

// TestDeliverWithNobodyWatching covers the room holding only the agent. There is
// no browser to tell, so nothing should be minted: a ticket nobody can use just
// sits in the map until it expires.
func TestDeliverWithNobodyWatching(t *testing.T) {
	for _, tc := range []struct {
		name string
		room *Room
	}{
		{"agent alone, controlling", testRoom(agentID, 0, true)},
		{"agent alone, control free", testRoom("", 0, true)},
		{"empty room", testRoom("", 0, false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, files := testDelivery(t, tc.room)
			if got := d.Deliver("/tmp/shot.png", ""); got != 0 {
				t.Errorf("delivered to %d, want 0", got)
			}
			if n := len(files.tickets); n != 0 {
				t.Errorf("minted %d tickets with nobody watching, want 0", n)
			}
		})
	}
}

// TestDeliverTicketsAreDistinct is the second defect stated on its own: a ticket
// is good exactly once, so sharing one between browsers is one download and the
// rest dead links.
func TestDeliverTicketsAreDistinct(t *testing.T) {
	d, files := testDelivery(t, testRoom("", 3, false))
	if got := d.Deliver("/tmp/shot.png", ""); got != 3 {
		t.Fatalf("delivered to %d, want 3", got)
	}
	seen := map[string]bool{}
	for tk, entry := range files.tickets {
		if seen[tk] {
			t.Errorf("duplicate ticket %s", tk)
		}
		seen[tk] = true
		if entry.path != "/tmp/shot.png" {
			t.Errorf("ticket points at %q", entry.path)
		}
	}
	if len(seen) != 3 {
		t.Errorf("%d distinct tickets for 3 recipients", len(seen))
	}
}

func TestDeliverNilReceiver(t *testing.T) {
	var d *Delivery
	if got := d.Deliver("/tmp/shot.png", ""); got != 0 {
		t.Errorf("nil Delivery returned %d", got)
	}
}
