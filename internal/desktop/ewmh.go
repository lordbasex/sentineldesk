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

// Windows, desktops and their states, read from X rather than from another
// program's output.
//
// A dozen tools used to answer window questions by running wmctrl or xdotool
// and parsing the columns that came back. That works until it does not: the
// output is laid out in fixed columns, so a window titled "Report  2026" — two
// spaces — parses as a different window with a different geometry, and every
// field after it shifts. It is also a locale dependency and a process per call
// for data this connection already holds.
//
// The same properties wmctrl reads are EWMH properties on the root window and
// on each client. Reading them directly is fewer moving parts, not more: no
// subprocess, no text, no column arithmetic, and errors that say which property
// failed instead of an exit status.
//
// This talks the X protocol through jezek/xgb, the same pure-Go binding the
// input injector uses. No CGo, and no new dependency.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// WindowInfo is one client window, in the shape the tools already report.
type WindowInfo struct {
	ID      string `json:"id"`      // 0x01800003, the form every tool takes back
	Desktop int    `json:"desktop"` // -1 when the window is on all of them
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Class   string `json:"class"`
	Title   string `json:"title"`
}

// DesktopInfo is one virtual desktop.
type DesktopInfo struct {
	Number  int    `json:"number"`
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// EWMH answers window and desktop questions over its own X connection.
//
// Its own rather than the injector's: the injector serialises XTEST calls
// behind a mutex, and a window query has no reason to wait behind a keystroke.
// X connections are cheap and this one is read-mostly.
type EWMH struct {
	conn *xgb.Conn
	root xproto.Window

	mu    sync.Mutex
	atoms map[string]xproto.Atom // interned once; the server never renumbers them
}

// NewEWMH connects to the display, retrying briefly while Xvfb settles.
//
// Five seconds rather than the injector's sixty: the injector is built during
// startup and has to outwait supervisord's ordering, while this is opened the
// first time a tool asks a window question, by which point X is either up or
// not coming.
func NewEWMH(display string) (*EWMH, error) {
	var conn *xgb.Conn
	var err error
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err = xgb.NewConnDisplay(display)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("X display %s: %w", display, err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	setup := xproto.Setup(conn)
	return &EWMH{
		conn:  conn,
		root:  setup.DefaultScreen(conn).Root,
		atoms: map[string]xproto.Atom{},
	}, nil
}

func (e *EWMH) Close() {
	if e != nil && e.conn != nil {
		e.conn.Close()
	}
}

// atom interns a name once and remembers it. Atom ids are stable for the life
// of the server, so the second lookup is a map read rather than a round trip.
func (e *EWMH) atom(name string) (xproto.Atom, error) {
	e.mu.Lock()
	if a, ok := e.atoms[name]; ok {
		e.mu.Unlock()
		return a, nil
	}
	e.mu.Unlock()

	reply, err := xproto.InternAtom(e.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("intern %s: %w", name, err)
	}
	e.mu.Lock()
	e.atoms[name] = reply.Atom
	e.mu.Unlock()
	return reply.Atom, nil
}

// prop reads a property, asking for enough words to cover the long ones — a
// client list on a busy desktop, or the desktop names.
func (e *EWMH) prop(win xproto.Window, name string) (*xproto.GetPropertyReply, error) {
	a, err := e.atom(name)
	if err != nil {
		return nil, err
	}
	return xproto.GetProperty(e.conn, false, win, a,
		xproto.GetPropertyTypeAny, 0, 4096).Reply()
}

func (e *EWMH) propInts(win xproto.Window, name string) ([]uint32, error) {
	r, err := e.prop(win, name)
	if err != nil || r == nil || r.Format != 32 {
		return nil, err
	}
	out := make([]uint32, 0, r.ValueLen)
	for i := 0; i+3 < len(r.Value); i += 4 {
		out = append(out, uint32(r.Value[i])|uint32(r.Value[i+1])<<8|
			uint32(r.Value[i+2])<<16|uint32(r.Value[i+3])<<24)
	}
	return out, nil
}

// propText reads a string property. _NET_WM_NAME is UTF8_STRING and WM_NAME is
// Latin-1, which is why the title falls back rather than assuming one.
func (e *EWMH) propText(win xproto.Window, name string) (string, error) {
	r, err := e.prop(win, name)
	if err != nil || r == nil {
		return "", err
	}
	return string(r.Value), nil
}

// --- reads -------------------------------------------------------------------

// Windows lists the managed client windows.
//
// _NET_CLIENT_LIST is what the window manager publishes, so this is the same
// set a taskbar shows: real windows, not the tooltips and menus that
// override-redirect puts on the screen. WindowTree is the tool for those.
func (e *EWMH) Windows() ([]WindowInfo, error) {
	ids, err := e.propInts(e.root, "_NET_CLIENT_LIST")
	if err != nil {
		return nil, fmt.Errorf("_NET_CLIENT_LIST: %w", err)
	}
	out := make([]WindowInfo, 0, len(ids))
	for _, id := range ids {
		if info, err := e.describe(xproto.Window(id)); err == nil {
			out = append(out, info)
		}
		// A window that vanished between the list and the query is normal on a
		// live desktop, and not a reason to fail the whole call.
	}
	return out, nil
}

func (e *EWMH) describe(win xproto.Window) (WindowInfo, error) {
	info := WindowInfo{ID: fmt.Sprintf("0x%08x", uint32(win)), Desktop: -1}

	if d, err := e.propInts(win, "_NET_WM_DESKTOP"); err == nil && len(d) > 0 {
		// 0xFFFFFFFF means "on every desktop", which is -1 to a caller.
		if d[0] != 0xFFFFFFFF {
			info.Desktop = int(d[0])
		}
	}

	// UTF-8 first, then the Latin-1 property every window has had since before
	// EWMH existed.
	if t, _ := e.propText(win, "_NET_WM_NAME"); t != "" {
		info.Title = t
	} else if t, _ := e.propText(win, "WM_NAME"); t != "" {
		info.Title = t
	}

	// WM_CLASS is two NUL-terminated strings: instance, then class. The class
	// is the useful half and the one wmctrl printed.
	if c, _ := e.propText(win, "WM_CLASS"); c != "" {
		parts := strings.Split(strings.TrimRight(c, "\x00"), "\x00")
		info.Class = parts[len(parts)-1]
	}

	geom, err := xproto.GetGeometry(e.conn, xproto.Drawable(win)).Reply()
	if err != nil {
		return info, fmt.Errorf("geometry: %w", err)
	}
	info.W, info.H = int(geom.Width), int(geom.Height)

	// The window's own x/y are relative to its parent, and a reparenting window
	// manager makes that the frame rather than the root. Translating gives the
	// screen coordinates a click can use, which is the only kind worth
	// reporting.
	tr, err := xproto.TranslateCoordinates(e.conn, win, e.root, 0, 0).Reply()
	if err == nil {
		info.X, info.Y = int(tr.DstX), int(tr.DstY)
	} else {
		info.X, info.Y = int(geom.X), int(geom.Y)
	}
	return info, nil
}

// ActiveWindow returns the focused window, or ok=false when nothing is focused.
//
// Nothing focused is an ordinary state — an empty desktop, or a window that
// just closed — so it is reported as an answer rather than as an error.
func (e *EWMH) ActiveWindow() (WindowInfo, bool, error) {
	ids, err := e.propInts(e.root, "_NET_ACTIVE_WINDOW")
	if err != nil {
		return WindowInfo{}, false, fmt.Errorf("_NET_ACTIVE_WINDOW: %w", err)
	}
	if len(ids) == 0 || ids[0] == 0 {
		return WindowInfo{}, false, nil
	}
	info, err := e.describe(xproto.Window(ids[0]))
	if err != nil {
		return WindowInfo{}, false, err
	}
	return info, true, nil
}

// Desktops lists the virtual desktops and marks the current one.
func (e *EWMH) Desktops() ([]DesktopInfo, error) {
	count, err := e.propInts(e.root, "_NET_NUMBER_OF_DESKTOPS")
	if err != nil {
		return nil, fmt.Errorf("_NET_NUMBER_OF_DESKTOPS: %w", err)
	}
	n := 0
	if len(count) > 0 {
		n = int(count[0])
	}
	cur, _ := e.propInts(e.root, "_NET_CURRENT_DESKTOP")
	current := -1
	if len(cur) > 0 {
		current = int(cur[0])
	}

	// _NET_DESKTOP_NAMES is a run of NUL-terminated UTF-8 strings, and a window
	// manager may publish fewer names than desktops. The unnamed ones get the
	// number, which is what a person sees in a pager anyway.
	var names []string
	if raw, _ := e.propText(e.root, "_NET_DESKTOP_NAMES"); raw != "" {
		names = strings.Split(strings.TrimRight(raw, "\x00"), "\x00")
	}

	out := make([]DesktopInfo, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%d", i)
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		out = append(out, DesktopInfo{Number: i, Name: name, Current: i == current})
	}
	return out, nil
}
