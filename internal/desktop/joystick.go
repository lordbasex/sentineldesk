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
	"fmt"
	"os"
	"sync"

	"github.com/bendahl/uinput"
)

// Standard Gamepad API button indices mapped to uinput button codes.
// https://w3c.github.io/gamepad/#remapping
var gamepadButtonMap = map[int]int{
	0:  uinput.ButtonSouth,       // A
	1:  uinput.ButtonEast,        // B
	2:  uinput.ButtonWest,        // X
	3:  uinput.ButtonNorth,       // Y
	4:  uinput.ButtonBumperLeft,  // LB
	5:  uinput.ButtonBumperRight, // RB
	6:  uinput.ButtonTriggerLeft, // LT, reported as a button
	7:  uinput.ButtonTriggerRight,
	8:  uinput.ButtonSelect, // Back/Select
	9:  uinput.ButtonStart,
	10: uinput.ButtonThumbLeft,  // L3
	11: uinput.ButtonThumbRight, // R3
	12: uinput.ButtonDpadUp,
	13: uinput.ButtonDpadDown,
	14: uinput.ButtonDpadLeft,
	15: uinput.ButtonDpadRight,
	16: uinput.ButtonMode, // Guide
}

// Joystick is a virtual gamepad fed by the browser's Gamepad API.
//
// Every {t:"gp", b:[...], a:[...]} event from the client becomes uinput events.
// It is optional: without /dev/uinput this stays nil and the desktop works
// exactly as before, minus the joystick. Note that Docker Desktop's LinuxKit VM
// does not expose /dev/input, so this only works on a real Linux host.
type Joystick struct {
	dev     uinput.Gamepad
	mu      sync.Mutex
	pressed map[int]bool // previous button state, so we can emit press/release
	lastLX  float32
	lastLY  float32
	lastRX  float32
	lastRY  float32
	inited  bool
}

// NewJoystick creates the virtual device when /dev/uinput is present.
func NewJoystick() (*Joystick, error) {
	if _, err := os.Stat("/dev/uinput"); err != nil {
		return nil, err // no uinput: the joystick is simply disabled
	}
	dev, err := uinput.CreateGamepad(
		"/dev/uinput",
		[]byte("sentineldesk-gamepad"),
		0x045e, // Microsoft
		0x028e, // Xbox 360 Controller: the identity games recognise most widely
	)
	if err != nil {
		return nil, err
	}
	return &Joystick{dev: dev, pressed: map[int]bool{}, inited: true}, nil
}

// Apply translates a gamepad snapshot from the browser into uinput events.
func (j *Joystick) Apply(buttons []float64, axes []float64) {
	if j == nil || !j.inited {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	// Buttons are level-triggered on the wire but edge-triggered in uinput, so
	// compare against the previous state and emit only the transitions.
	for i, code := range gamepadButtonMap {
		if i >= len(buttons) {
			continue
		}
		down := buttons[i] > 0.5
		if down && !j.pressed[i] {
			j.dev.ButtonDown(code)
			j.pressed[i] = true
		} else if !down && j.pressed[i] {
			j.dev.ButtonUp(code)
			j.pressed[i] = false
		}
	}

	// Sticks: the Gamepad API reports [-1,1] and so does uinput, so this is a
	// straight pass-through — but skip unchanged values to avoid flooding.
	if len(axes) >= 2 {
		lx, ly := float32(axes[0]), float32(axes[1])
		if lx != j.lastLX || ly != j.lastLY {
			j.dev.LeftStickMove(lx, ly)
			j.lastLX, j.lastLY = lx, ly
		}
	}
	if len(axes) >= 4 {
		rx, ry := float32(axes[2]), float32(axes[3])
		if rx != j.lastRX || ry != j.lastRY {
			j.dev.RightStickMove(rx, ry)
			j.lastRX, j.lastRY = rx, ry
		}
	}
}

// Button presses or releases a button by its Gamepad API index.
func (j *Joystick) Button(index int, down bool) error {
	if j == nil || !j.inited {
		return fmt.Errorf("joystick unavailable (no /dev/uinput)")
	}
	code, ok := gamepadButtonMap[index]
	if !ok {
		return fmt.Errorf("invalid button index %d (expected 0-16)", index)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if down {
		j.dev.ButtonDown(code)
		j.pressed[index] = true
	} else {
		j.dev.ButtonUp(code)
		j.pressed[index] = false
	}
	return nil
}

// Axis moves one stick axis: 0=LX 1=LY 2=RX 3=RY, value in [-1,1].
func (j *Joystick) Axis(axis int, value float64) error {
	if j == nil || !j.inited {
		return fmt.Errorf("joystick unavailable (no /dev/uinput)")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	v := float32(value)
	switch axis {
	case 0:
		j.lastLX = v
		j.dev.LeftStickMove(j.lastLX, j.lastLY)
	case 1:
		j.lastLY = v
		j.dev.LeftStickMove(j.lastLX, j.lastLY)
	case 2:
		j.lastRX = v
		j.dev.RightStickMove(j.lastRX, j.lastRY)
	case 3:
		j.lastRY = v
		j.dev.RightStickMove(j.lastRX, j.lastRY)
	default:
		return fmt.Errorf("invalid axis %d (0=LX 1=LY 2=RX 3=RY)", axis)
	}
	return nil
}

// Available reports whether a usable virtual gamepad exists.
func (j *Joystick) Available() bool { return j != nil && j.inited }

func (j *Joystick) Close() {
	if j != nil && j.inited {
		j.dev.Close()
	}
}
