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

package mcp

// Advanced MCP tools: windows, desktops, processes, fine-grained mouse,
// screen, OCR, gamepad, files, audio and re-streaming.

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lordbasex/sentineldesk/internal/desktop"
	"github.com/lordbasex/sentineldesk/internal/media"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func (s *Server) buildAdvancedTools() []toolDef {
	return []toolDef{
		// ---- windows ----
		{
			Name:        "get_active_window",
			Risk:        riskRead,
			Description: "Get the currently focused window: id, title, class and geometry.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "move_window",
			Risk:        riskWrite,
			Description: "Move a window to absolute screen coordinates.",
			InputSchema: schema(map[string]any{
				"id": pStr("window id from list_windows"), "x": pInt("X"), "y": pInt("Y"),
			}, "id", "x", "y"),
		},
		{
			Name:        "resize_window",
			Risk:        riskWrite,
			Description: "Resize a window to the given width and height in pixels.",
			InputSchema: schema(map[string]any{
				"id": pStr("window id"), "width": pInt("width"), "height": pInt("height"),
			}, "id", "width", "height"),
		},
		{
			Name:        "close_window",
			Risk:        riskWrite,
			Description: "Close a window gracefully (like clicking its X button).",
			InputSchema: schema(map[string]any{"id": pStr("window id")}, "id"),
		},
		{
			Name:        "minimize_window",
			Risk:        riskWrite,
			Description: "Minimize (iconify) a window.",
			InputSchema: schema(map[string]any{"id": pStr("window id")}, "id"),
		},
		{
			Name:        "maximize_window",
			Risk:        riskWrite,
			Description: "Maximize a window (both directions).",
			InputSchema: schema(map[string]any{"id": pStr("window id")}, "id"),
		},
		{
			Name:        "restore_window",
			Risk:        riskWrite,
			Description: "Un-maximize a window back to its previous size.",
			InputSchema: schema(map[string]any{"id": pStr("window id")}, "id"),
		},
		{
			Name:        "fullscreen_window",
			Risk:        riskWrite,
			Description: "Toggle fullscreen on a window.",
			InputSchema: schema(map[string]any{"id": pStr("window id")}, "id"),
		},
		{
			Name:        "set_window_desktop",
			Risk:        riskWrite,
			Description: "Move a window to another virtual desktop (workspace).",
			InputSchema: schema(map[string]any{
				"id": pStr("window id"), "desktop": pInt("desktop number, 0-based"),
			}, "id", "desktop"),
		},
		{
			Name:        "wait_for_window",
			Risk:        riskRead,
			Description: "Wait until a window whose title or class contains the given text appears. Returns its info, or an error on timeout. Use after launch_app instead of guessing a wait time.",
			InputSchema: schema(map[string]any{
				"match": pStr("substring of the title or class"), "timeout_ms": pInt("timeout (default 15000)"),
			}, "match"),
		},
		// ---- escritorios ----
		{
			Name:        "list_desktops",
			Risk:        riskRead,
			Description: "List the virtual desktops (workspaces): number, name and which one is current.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "switch_desktop",
			Risk:        riskWrite,
			Description: "Switch to another virtual desktop (workspace) by number.",
			InputSchema: schema(map[string]any{"desktop": pInt("desktop number, 0-based")}, "desktop"),
		},
		// ---- procesos ----
		{
			Name:        "list_processes",
			Risk:        riskRead,
			Description: "List running processes with pid, cpu%, mem% and command. Optionally filter by a substring.",
			InputSchema: schema(map[string]any{"filter": pStr("optional substring to filter by")}),
		},
		{
			Name:        "kill_process",
			Risk:        riskDanger,
			Description: "Terminate a process by pid, or every process matching a name.",
			InputSchema: schema(map[string]any{
				"pid": pInt("process id"), "name": pStr("process name (pkill)"), "force": pBool("send SIGKILL instead of SIGTERM"),
			}),
		},
		{
			Name:        "is_running",
			Risk:        riskRead,
			Description: "Check whether a process matching the given name is running; returns the matching pids.",
			InputSchema: schema(map[string]any{"name": pStr("process name")}, "name"),
		},
		{
			Name:        "list_installed_apps",
			Risk:        riskRead,
			Description: "List the graphical applications installed on the desktop (from .desktop entries): name and command.",
			InputSchema: schema(map[string]any{}),
		},
		// ---- mouse fino ----
		{
			Name:            "mouse_drag",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Drag with the mouse from one point to another (press, move, release).",
			InputSchema: schema(map[string]any{
				"x1": pInt("start X"), "y1": pInt("start Y"), "x2": pInt("end X"), "y2": pInt("end Y"),
				"button": pInt("button, default 1"),
			}, "x1", "y1", "x2", "y2"),
		},
		{
			Name:            "mouse_scroll",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Scroll the mouse wheel. Positive dy scrolls down, negative up.",
			InputSchema: schema(map[string]any{
				"dy": pInt("vertical clicks"), "dx": pInt("horizontal clicks"),
			}),
		},
		{
			Name:        "get_mouse_position",
			Risk:        riskRead,
			Description: "Get the current mouse pointer position.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:            "mouse_down",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Press and hold a mouse button (pair with mouse_up).",
			InputSchema:     schema(map[string]any{"button": pInt("1=left 2=middle 3=right")}),
		},
		{
			Name:            "mouse_up",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Release a mouse button.",
			InputSchema:     schema(map[string]any{"button": pInt("1=left 2=middle 3=right")}),
		},
		// ---- screen ----
		{
			Name:        "screenshot_region",
			Risk:        riskRead,
			Description: "Capture only a rectangular region of the screen as PNG. Cheaper than a full screenshot when you only need part of the screen.",
			InputSchema: schema(map[string]any{
				"x": pInt("left"), "y": pInt("top"), "width": pInt("width"), "height": pInt("height"),
			}, "x", "y", "width", "height"),
		},
		{
			Name:        "get_screen_info",
			Risk:        riskRead,
			Description: "Get screen resolution, colour depth and the number of virtual desktops.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "get_pixel_color",
			Risk:        riskRead,
			Description: "Read the RGB colour of a single pixel on screen (useful to assert UI state cheaply).",
			InputSchema: schema(map[string]any{"x": pInt("X"), "y": pInt("Y")}, "x", "y"),
		},
		// ---- OCR ----
		{
			Name:        "read_screen_text",
			Risk:        riskRead,
			Description: "OCR the screen (or a region) and return the text found. Use it to read what is on screen without sending an image.",
			InputSchema: schema(map[string]any{
				"x": pInt("optional region left"), "y": pInt("optional region top"),
				"width": pInt("optional region width"), "height": pInt("optional region height"),
				"lang": pStr("tesseract language, default eng"),
			}),
		},
		{
			Name:        "find_text",
			Risk:        riskRead,
			Description: "OCR the screen and return the screen coordinates of every occurrence of the given text, so you can click on it.",
			InputSchema: schema(map[string]any{
				"text": pStr("text to look for (case-insensitive)"),
				"lang": pStr("tesseract language, default eng"),
			}, "text"),
		},
		// ---- gamepad ----
		{
			Name:            "gamepad_button",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Press or release a virtual gamepad button. Index follows the W3C Gamepad API: 0=A 1=B 2=X 3=Y 4=LB 5=RB 6=LT 7=RT 8=Select 9=Start 10=L3 11=R3 12=Up 13=Down 14=Left 15=Right 16=Guide.",
			InputSchema: schema(map[string]any{
				"index": pInt("button index 0-16"), "down": pBool("true=press, false=release"),
			}, "index"),
		},
		{
			Name:            "gamepad_tap",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Press and immediately release a gamepad button (a single 'tap', e.g. to confirm a menu).",
			InputSchema: schema(map[string]any{
				"index": pInt("button index 0-16"), "hold_ms": pInt("hold time, default 80"),
			}, "index"),
		},
		{
			Name:            "gamepad_axis",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Move a virtual gamepad stick. axis: 0=left X, 1=left Y, 2=right X, 3=right Y. value between -1 and 1.",
			InputSchema: schema(map[string]any{
				"axis": pInt("0=LX 1=LY 2=RX 3=RY"), "value": map[string]any{"type": "number", "description": "-1..1"},
			}, "axis", "value"),
		},
		{
			Name:            "gamepad_state",
			Risk:            riskWrite,
			RequiresControl: true,
			Description:     "Set the full gamepad state at once: array of button values (0/1) and array of axis values (-1..1).",
			InputSchema: schema(map[string]any{
				"buttons": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "button values"},
				"axes":    map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "axis values"},
			}),
		},
		// ---- files ----
		{
			Name:        "read_file",
			Risk:        riskRead,
			Description: "Read a text file from the desktop filesystem. Set as_root:true for files the desktop user cannot read (/etc/shadow, another user's home).",
			InputSchema: schema(map[string]any{
				"path": pStr("absolute path"), "max_bytes": pInt("truncate after N bytes (default 100000)"),
				"as_root": pBool("read with root privileges (default false)"),
			}, "path"),
		},
		{
			Name:        "write_file",
			Risk:        riskDanger,
			Description: "Write (or create) a text file on the desktop filesystem. Set as_root:true to write outside the home directory (/etc, /usr/local/bin, a systemd or supervisor unit).",
			InputSchema: schema(map[string]any{
				"path": pStr("absolute path"), "content": pStr("file content"), "append": pBool("append instead of overwrite"),
				"as_root": pBool("write with root privileges (default false)"),
				"mode":    pStr("permissions in octal to apply afterwards, e.g. 0755 (only with as_root)"),
			}, "path", "content"),
		},
		{
			Name:        "list_directory",
			Risk:        riskRead,
			Description: "List the entries of a directory with size and type. Set as_root:true for directories the desktop user cannot enter.",
			InputSchema: schema(map[string]any{
				"path": pStr("absolute path"), "as_root": pBool("list with root privileges (default false)"),
			}, "path"),
		},
		// ---- audio ----
		{
			Name:        "get_audio_state",
			Risk:        riskRead,
			Description: "Get the default audio sink, its volume and whether it is muted.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        "set_volume",
			Risk:        riskWrite,
			Description: "Set the desktop output volume (0-150 percent) and/or mute state.",
			InputSchema: schema(map[string]any{
				"percent": pInt("volume percent"), "mute": pBool("mute on/off"),
			}),
		},
		// ---- re-streaming ----
		{
			Name:            "start_restream",
			Risk:            riskDanger,
			RequiresControl: true,
			Description: "Also send the desktop to an external destination: rtmp:// or rtmps:// " +
				"for YouTube/Twitch/Facebook, srt:// or udp:// for a VLC or OBS you run " +
				"yourself. It forwards the picture the room is already encoding, so the " +
				"live session is not interrupted and no second capture is started. " +
				"This publishes what is on screen — ask the people in the room first.",
			InputSchema: schema(map[string]any{
				"url":      pStr("destination, e.g. rtmp://a.rtmp.youtube.com/live2/KEY"),
				"platform": pStr("youtube | twitch | facebook | custom (default custom)"),
				"audio":    pBool("include audio, default true"),
				"keyframes": pBool("force a keyframe every 2s. The platforms need this and get " +
					"it regardless; for a custom destination leave it off unless viewers " +
					"join mid-stream, because keyframes cost bitrate that would otherwise " +
					"go to keeping text sharp."),
				"bitrate": pInt("standalone fallback only, ignored when a room is running"),
				"fps":     pInt("standalone fallback only, ignored when a room is running"),
			}, "url"),
		},
		{
			Name:            "stop_restream",
			Risk:            riskDanger,
			RequiresControl: true,
			Description:     "Stop sending to an external destination.",
			InputSchema: schema(map[string]any{
				"id": pStr("which destination (its platform name); omit to stop them all"),
			}),
		},
		{
			Name: "list_restreams",
			Risk: riskRead,
			Description: "Which external destinations this desktop is currently being sent to. " +
				"Stream keys come back redacted.",
			InputSchema: schema(map[string]any{}),
		},
		// ---- info ----
		{
			Name:        "get_desktop_info",
			Risk:        riskRead,
			Description: "Overall desktop status: window manager, resolution, uptime, load, memory, video encoder in use and whether joystick/recording are available.",
			InputSchema: schema(map[string]any{}),
		},
	}
}

// dispatchAdvanced runs the window, process, OCR, gamepad, file and streaming
// tools. The third return value says whether the name was recognised, so that
// dispatch() can fall through to the next group when it was not.
func (s *Server) dispatchAdvanced(ctx context.Context, name string, args map[string]any) ([]map[string]any, bool, bool) {
	switch name {
	// ---- windows ----
	case "get_active_window":
		c, e := s.toolActiveWindow()
		return c, e, true
	case "move_window":
		c, e := s.wmctrlGeom(argStr(args, "id"), argInt(args, "x"), argInt(args, "y"), -1, -1, "moved")
		return c, e, true
	case "resize_window":
		c, e := s.wmctrlGeom(argStr(args, "id"), -1, -1, argInt(args, "width"), argInt(args, "height"), "resized")
		return c, e, true
	case "close_window":
		c, e := s.simpleRun("closed window", "wmctrl", "-i", "-c", argStr(args, "id"))
		return c, e, true
	case "minimize_window":
		c, e := s.simpleRun("minimized", "xdotool", "windowminimize", argStr(args, "id"))
		return c, e, true
	case "maximize_window":
		c, e := s.simpleRun("maximized", "wmctrl", "-i", "-r", argStr(args, "id"), "-b", "add,maximized_vert,maximized_horz")
		return c, e, true
	case "restore_window":
		c, e := s.simpleRun("restored", "wmctrl", "-i", "-r", argStr(args, "id"), "-b", "remove,maximized_vert,maximized_horz")
		return c, e, true
	case "fullscreen_window":
		c, e := s.simpleRun("toggled fullscreen", "wmctrl", "-i", "-r", argStr(args, "id"), "-b", "toggle,fullscreen")
		return c, e, true
	case "set_window_desktop":
		c, e := s.simpleRun("moved to desktop", "wmctrl", "-i", "-r", argStr(args, "id"), "-t", strconv.Itoa(argInt(args, "desktop")))
		return c, e, true
	case "wait_for_window":
		c, e := s.toolWaitForWindow(ctx, argStr(args, "match"), argInt(args, "timeout_ms"))
		return c, e, true
	// ---- escritorios ----
	case "list_desktops":
		c, e := s.toolListDesktops()
		return c, e, true
	case "switch_desktop":
		c, e := s.simpleRun("switched desktop", "wmctrl", "-s", strconv.Itoa(argInt(args, "desktop")))
		return c, e, true
	// ---- procesos ----
	case "list_processes":
		c, e := s.toolListProcesses(argStr(args, "filter"))
		return c, e, true
	case "kill_process":
		c, e := s.toolKillProcess(args)
		return c, e, true
	case "is_running":
		c, e := s.toolIsRunning(argStr(args, "name"))
		return c, e, true
	case "list_installed_apps":
		c, e := s.toolListInstalledApps()
		return c, e, true
	// ---- mouse ----
	case "mouse_drag":
		c, e := s.toolMouseDrag(args)
		return c, e, true
	case "mouse_scroll":
		s.injector.Wheel(argInt(args, "dy"), argInt(args, "dx"))
		return textContent("scrolled dy=%d dx=%d", argInt(args, "dy"), argInt(args, "dx")), false, true
	case "get_mouse_position":
		x, y, err := s.injector.Pointer()
		if err != nil {
			return textContent("pointer failed: %v", err), true, true
		}
		return jsonContent(map[string]int{"x": x, "y": y}), false, true
	case "mouse_down", "mouse_up":
		btn := argInt(args, "button")
		if btn == 0 {
			btn = 1
		}
		s.injector.Button(btn, name == "mouse_down")
		return textContent("%s button %d", name, btn), false, true
	// ---- screen ----
	case "screenshot_region":
		b64, err := desktop.GrabRegionPNG(s.display, argInt(args, "x"), argInt(args, "y"), argInt(args, "width"), argInt(args, "height"))
		if err != nil {
			return textContent("screenshot_region failed: %v", err), true, true
		}
		return imageContent(b64, "image/png"), false, true
	case "get_screen_info":
		c, e := s.toolScreenInfo()
		return c, e, true
	case "get_pixel_color":
		r, g, b, err := s.injector.Pixel(argInt(args, "x"), argInt(args, "y"))
		if err != nil {
			return textContent("get_pixel_color failed: %v", err), true, true
		}
		return jsonContent(map[string]any{
			"r": r, "g": g, "b": b, "hex": fmt.Sprintf("#%02x%02x%02x", r, g, b),
		}), false, true
	// ---- OCR ----
	case "read_screen_text":
		c, e := s.toolReadScreenText(args)
		return c, e, true
	case "find_text":
		c, e := s.toolFindText(args)
		return c, e, true
	// ---- gamepad ----
	case "gamepad_button":
		down := true
		if v, ok := args["down"].(bool); ok {
			down = v
		}
		if err := s.joystick.Button(argInt(args, "index"), down); err != nil {
			return textContent("gamepad_button failed: %v", err), true, true
		}
		return textContent("button %d %s", argInt(args, "index"), map[bool]string{true: "down", false: "up"}[down]), false, true
	case "gamepad_tap":
		idx := argInt(args, "index")
		hold := argInt(args, "hold_ms")
		if hold <= 0 {
			hold = 80
		}
		if err := s.joystick.Button(idx, true); err != nil {
			return textContent("gamepad_tap failed: %v", err), true, true
		}
		time.Sleep(time.Duration(hold) * time.Millisecond)
		s.joystick.Button(idx, false)
		return textContent("tapped button %d", idx), false, true
	case "gamepad_axis":
		val, _ := args["value"].(float64)
		if err := s.joystick.Axis(argInt(args, "axis"), val); err != nil {
			return textContent("gamepad_axis failed: %v", err), true, true
		}
		return textContent("axis %d = %.2f", argInt(args, "axis"), val), false, true
	case "gamepad_state":
		if !s.joystick.Available() {
			return textContent("gamepad not available (no /dev/uinput)"), true, true
		}
		s.joystick.Apply(floatSlice(args["buttons"]), floatSlice(args["axes"]))
		return textContent("gamepad state applied"), false, true
	// ---- files ----
	case "read_file":
		asRoot, _ := args["as_root"].(bool)
		c, e := s.toolReadFile(argStr(args, "path"), argInt(args, "max_bytes"), asRoot)
		return c, e, true
	case "write_file":
		c, e := s.toolWriteFile(args)
		return c, e, true
	case "list_directory":
		asRoot, _ := args["as_root"].(bool)
		c, e := s.toolListDirectory(argStr(args, "path"), asRoot)
		return c, e, true
	// ---- audio ----
	case "get_audio_state":
		c, e := s.toolAudioState()
		return c, e, true
	case "set_volume":
		c, e := s.toolSetVolume(args)
		return c, e, true
	// ---- re-streaming ----
	case "start_restream":
		c, e := s.toolStartRestream(args)
		return c, e, true
	case "stop_restream":
		c, e := s.toolStopRestream(args)
		return c, e, true
	case "list_restreams":
		c, e := s.toolListRestreams()
		return c, e, true
	// ---- info ----
	case "get_desktop_info":
		c, e := s.toolDesktopInfo()
		return c, e, true
	}
	return nil, false, false
}

// --- implementaciones ------------------------------------------------------

func (s *Server) simpleRun(okMsg, bin string, args ...string) ([]map[string]any, bool) {
	if err := s.run(bin, args...); err != nil {
		return textContent("%s failed: %v", bin, err), true
	}
	return textContent("%s", okMsg), false
}

// wmctrlGeom mueve y/o redimensiona (-1 = no cambiar).
func (s *Server) wmctrlGeom(id string, x, y, w, h int, verb string) ([]map[string]any, bool) {
	geom := fmt.Sprintf("0,%d,%d,%d,%d", x, y, w, h)
	if err := s.run("wmctrl", "-i", "-r", id, "-e", geom); err != nil {
		return textContent("%s failed: %v", verb, err), true
	}
	return textContent("%s %s (%s)", verb, id, geom), false
}

func (s *Server) toolActiveWindow() ([]map[string]any, bool) {
	// One property read where this used to be three xdotool processes, and the
	// geometry comes back as numbers rather than as the paragraph xdotool
	// prints.
	if e, err := s.windows(); err == nil {
		info, ok, err := e.ActiveWindow()
		if err == nil {
			if !ok {
				// Nothing focused is an answer. It used to be reported as an
				// error, which left a caller unable to tell "the desktop is
				// idle" from "the query broke".
				return jsonContent(map[string]any{
					"active": nil,
					"note":   "no window currently has focus",
				}), false
			}
			return jsonContent(info), false
		}
	}

	id, err := s.output("xdotool", "getactivewindow")
	if err != nil {
		return textContent("no active window: %v", err), true
	}
	idNum := strings.TrimSpace(id)
	n, _ := strconv.Atoi(idNum)
	hexID := fmt.Sprintf("0x%08x", n)
	name, _ := s.output("xdotool", "getwindowname", idNum)
	geom, _ := s.output("xdotool", "getwindowgeometry", idNum)
	return jsonContent(map[string]any{
		"id":       hexID,
		"id_dec":   idNum,
		"title":    strings.TrimSpace(name),
		"geometry": strings.TrimSpace(geom),
	}), false
}

func (s *Server) toolWaitForWindow(ctx context.Context, match string, timeoutMs int) ([]map[string]any, bool) {
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	needle := strings.ToLower(match)
	for time.Now().Before(deadline) {
		out, err := s.output("wmctrl", "-l", "-x")
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(strings.ToLower(line), needle) {
					f := strings.Fields(line)
					if len(f) >= 4 {
						return jsonContent(map[string]any{
							"id": f[0], "class": f[2], "title": strings.Join(f[4:], " "), "found": true,
						}), false
					}
				}
			}
		}
		if !sleepCtx(ctx, 300*time.Millisecond) {
			break
		}
	}
	return textContent("timeout: no window matching %q after %d ms", match, timeoutMs), true
}

func (s *Server) toolListDesktops() ([]map[string]any, bool) {
	// _NET_DESKTOP_NAMES rather than the column arithmetic below, which assumed
	// the name began at field 8 and lost a desktop called "Build 2 of 3".
	if e, err := s.windows(); err == nil {
		if list, err := e.Desktops(); err == nil {
			return jsonContent(list), false
		}
	}

	out, err := s.output("wmctrl", "-d")
	if err != nil {
		return textContent("list_desktops failed: %v", err), true
	}
	var desks []map[string]any
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		num, _ := strconv.Atoi(f[0])
		desks = append(desks, map[string]any{
			"number": num, "current": f[1] == "*", "name": strings.Join(f[min(len(f), 8):], " "),
		})
	}
	return jsonContent(desks), false
}

func (s *Server) toolListProcesses(filter string) ([]map[string]any, bool) {
	out, err := s.output("ps", "-eo", "pid,pcpu,pmem,comm,args", "--sort=-pcpu")
	if err != nil {
		return textContent("list_processes failed: %v", err), true
	}
	var procs []map[string]any
	needle := strings.ToLower(filter)
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if i == 0 {
			continue // encabezado
		}
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		cmdline := strings.Join(f[4:], " ")
		if len(cmdline) > 120 {
			cmdline = cmdline[:120] + "…"
		}
		procs = append(procs, map[string]any{
			"pid": pid, "cpu": f[1], "mem": f[2], "name": f[3], "command": cmdline,
		})
		if len(procs) >= 60 {
			break
		}
	}
	return jsonContent(procs), false
}

func (s *Server) toolKillProcess(args map[string]any) ([]map[string]any, bool) {
	force, _ := args["force"].(bool)
	sig := "-TERM"
	if force {
		sig = "-KILL"
	}
	if pid := argInt(args, "pid"); pid > 0 {
		if err := s.run("kill", sig, strconv.Itoa(pid)); err != nil {
			return textContent("kill %d failed: %v", pid, err), true
		}
		return textContent("killed pid %d", pid), false
	}
	if name := argStr(args, "name"); name != "" {
		if err := s.run("pkill", sig, "-f", name); err != nil {
			return textContent("no process matched %q", name), true
		}
		return textContent("killed processes matching %q", name), false
	}
	return textContent("give a pid or a name"), true
}

func (s *Server) toolIsRunning(name string) ([]map[string]any, bool) {
	out, err := s.output("pgrep", "-f", name)
	pids := []int{}
	for _, line := range strings.Fields(out) {
		if n, e := strconv.Atoi(line); e == nil {
			pids = append(pids, n)
		}
	}
	return jsonContent(map[string]any{"running": err == nil && len(pids) > 0, "pids": pids}), false
}

func (s *Server) toolListInstalledApps() ([]map[string]any, bool) {
	dirs := []string{"/usr/share/applications", "/usr/local/share/applications"}
	var apps []map[string]any
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			data, err := os.ReadFile(dir + "/" + e.Name())
			if err != nil {
				continue
			}
			var appName, execCmd string
			noDisplay := false
			for _, line := range strings.Split(string(data), "\n") {
				switch {
				case strings.HasPrefix(line, "Name=") && appName == "":
					appName = strings.TrimPrefix(line, "Name=")
				case strings.HasPrefix(line, "Exec=") && execCmd == "":
					execCmd = strings.TrimPrefix(line, "Exec=")
				case strings.HasPrefix(line, "NoDisplay=true"):
					noDisplay = true
				}
			}
			if appName != "" && execCmd != "" && !noDisplay {
				apps = append(apps, map[string]any{"name": appName, "exec": execCmd})
			}
		}
	}
	return jsonContent(apps), false
}

func (s *Server) toolMouseDrag(args map[string]any) ([]map[string]any, bool) {
	btn := argInt(args, "button")
	if btn == 0 {
		btn = 1
	}
	x1, y1 := argInt(args, "x1"), argInt(args, "y1")
	x2, y2 := argInt(args, "x2"), argInt(args, "y2")
	s.injector.Move(x1, y1)
	time.Sleep(60 * time.Millisecond)
	s.injector.Button(btn, true)
	time.Sleep(60 * time.Millisecond)
	// Move in steps: many applications ignore an instantaneous jump, because
	// they are watching for motion events rather than a final position.
	const steps = 12
	for i := 1; i <= steps; i++ {
		s.injector.Move(x1+(x2-x1)*i/steps, y1+(y2-y1)*i/steps)
		time.Sleep(15 * time.Millisecond)
	}
	time.Sleep(60 * time.Millisecond)
	s.injector.Button(btn, false)
	return textContent("dragged (%d,%d) -> (%d,%d)", x1, y1, x2, y2), false
}

func (s *Server) toolScreenInfo() ([]map[string]any, bool) {
	w, h := s.injector.Screen()
	desks, _ := s.output("wmctrl", "-d")
	return jsonContent(map[string]any{
		"width": w, "height": h, "display": s.display,
		"desktops": len(strings.Split(strings.TrimRight(desks, "\n"), "\n")),
	}), false
}

// --- OCR ------------------------------------------------------------------

// ocrImage captures — upscaled 2x, which is what makes tesseract reliable on
// small UI text — and runs OCR over it. mode "" gives plain text, "tsv" adds
// coordinates.
func (s *Server) ocrImage(args map[string]any, mode string) (string, error) {
	x, y := argInt(args, "x"), argInt(args, "y")
	w, h := argInt(args, "width"), argInt(args, "height")
	lang := argStr(args, "lang")
	if lang == "" {
		lang = "eng"
	}
	tmp, err := os.CreateTemp("", "sentineldesk-ocr-*.png")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	screenW, screenH := s.injector.Screen()
	if err := desktop.GrabForOCR(s.display, path, x, y, w, h, screenW, screenH); err != nil {
		return "", err
	}
	cmd := exec.Command("tesseract", path, "stdout", "-l", lang, mode)
	if mode == "" {
		cmd = exec.Command("tesseract", path, "stdout", "-l", lang)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tesseract: %w", err)
	}
	return string(out), nil
}

func (s *Server) toolReadScreenText(args map[string]any) ([]map[string]any, bool) {
	text, err := s.ocrImage(args, "")
	if err != nil {
		return textContent("read_screen_text failed: %v", err), true
	}
	return textContent("%s", strings.TrimSpace(text)), false
}

func (s *Server) toolFindText(args map[string]any) ([]map[string]any, bool) {
	needle := strings.ToLower(strings.TrimSpace(argStr(args, "text")))
	if needle == "" {
		return textContent("no text given"), true
	}
	tsv, err := s.ocrImage(args, "tsv")
	if err != nil {
		return textContent("find_text failed: %v", err), true
	}
	// OCR ran on a 2x capture, possibly of a sub-region. The coordinates handed
	// back must be SCREEN coordinates, or a mouse_click built from them lands in
	// the wrong place.
	offX, offY := argInt(args, "x"), argInt(args, "y")

	var hits []map[string]any
	for i, line := range strings.Split(tsv, "\n") {
		if i == 0 {
			continue // encabezado TSV
		}
		f := strings.Split(line, "\t")
		if len(f) < 12 {
			continue
		}
		word := strings.ToLower(strings.TrimSpace(f[11]))
		if word == "" || !strings.Contains(word, needle) {
			continue
		}
		left, _ := strconv.Atoi(f[6])
		top, _ := strconv.Atoi(f[7])
		width, _ := strconv.Atoi(f[8])
		height, _ := strconv.Atoi(f[9])
		conf, _ := strconv.ParseFloat(f[10], 64)
		// undo the 2x scale, then translate by the region's origin
		x := offX + left/2
		y := offY + top/2
		w := width / 2
		h := height / 2
		hits = append(hits, map[string]any{
			"text": strings.TrimSpace(f[11]),
			"x":    x, "y": y, "width": w, "height": h,
			"center_x": x + w/2, "center_y": y + h/2,
			"confidence": conf,
		})
	}
	if len(hits) == 0 {
		return textContent("no match for %q on screen", needle), false
	}
	return jsonContent(hits), false
}

// --- files --------------------------------------------------------------

func (s *Server) toolReadFile(path string, maxBytes int, asRoot bool) ([]map[string]any, bool) {
	if maxBytes <= 0 {
		maxBytes = 100000
	}
	var data []byte
	var err error
	if asRoot {
		// `cat` under sudo rather than os.ReadFile: the daemon runs unprivileged.
		data, err = rootRead(path)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return textContent("read_file failed: %v", err), true
	}
	truncated := false
	if len(data) > maxBytes {
		data = data[:maxBytes]
		truncated = true
	}
	txt := string(data)
	if truncated {
		txt += "\n…[truncated]"
	}
	return textContent("%s", txt), false
}

func (s *Server) toolWriteFile(args map[string]any) ([]map[string]any, bool) {
	path := argStr(args, "path")
	content := argStr(args, "content")
	appendMode, _ := args["append"].(bool)
	if asRoot, _ := args["as_root"].(bool); asRoot {
		n, err := rootWrite(path, content, appendMode, argStr(args, "mode"))
		if err != nil {
			return textContent("write_file failed: %v", err), true
		}
		return textContent("wrote %d bytes to %s (as root)", n, path), false
	}
	flags := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return textContent("write_file failed: %v", err), true
	}
	defer f.Close()
	n, err := f.WriteString(content)
	if err != nil {
		return textContent("write_file failed: %v", err), true
	}
	return textContent("wrote %d bytes to %s", n, path), false
}

func (s *Server) toolListDirectory(path string, asRoot bool) ([]map[string]any, bool) {
	if asRoot {
		items, err := rootList(path)
		if err != nil {
			return textContent("list_directory failed: %v", err), true
		}
		return jsonContent(items), false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return textContent("list_directory failed: %v", err), true
	}
	var items []map[string]any
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		kind := "file"
		if e.IsDir() {
			kind = "dir"
		}
		items = append(items, map[string]any{
			"name": e.Name(), "type": kind, "size": info.Size(),
			"modified": info.ModTime().Format(time.RFC3339),
		})
	}
	return jsonContent(items), false
}

// --- audio -----------------------------------------------------------------

func (s *Server) toolAudioState() ([]map[string]any, bool) {
	sink, _ := s.output("pactl", "get-default-sink")
	vol, _ := s.output("pactl", "get-sink-volume", "@DEFAULT_SINK@")
	mute, _ := s.output("pactl", "get-sink-mute", "@DEFAULT_SINK@")
	return jsonContent(map[string]any{
		"sink":   strings.TrimSpace(sink),
		"volume": strings.TrimSpace(strings.SplitN(vol, "\n", 2)[0]),
		"mute":   strings.Contains(mute, "yes"),
	}), false
}

func (s *Server) toolSetVolume(args map[string]any) ([]map[string]any, bool) {
	var msgs []string
	if p, ok := args["percent"]; ok {
		pct := 0
		switch v := p.(type) {
		case float64:
			pct = int(v)
		case string:
			pct, _ = strconv.Atoi(v)
		}
		if pct < 0 {
			pct = 0
		}
		if pct > 150 {
			pct = 150
		}
		if err := s.run("pactl", "set-sink-volume", "@DEFAULT_SINK@", strconv.Itoa(pct)+"%"); err != nil {
			return textContent("set_volume failed: %v", err), true
		}
		msgs = append(msgs, fmt.Sprintf("volume %d%%", pct))
	}
	if m, ok := args["mute"].(bool); ok {
		val := "0"
		if m {
			val = "1"
		}
		if err := s.run("pactl", "set-sink-mute", "@DEFAULT_SINK@", val); err != nil {
			return textContent("set mute failed: %v", err), true
		}
		msgs = append(msgs, "mute="+val)
	}
	if len(msgs) == 0 {
		return textContent("nothing to set"), true
	}
	return textContent("%s", strings.Join(msgs, ", ")), false
}

// --- re-streaming ----------------------------------------------------------

func (s *Server) toolStartRestream(args map[string]any) ([]map[string]any, bool) {
	url := argStr(args, "url")
	if url == "" {
		return textContent("no url given"), true
	}
	audio := true
	if v, ok := args["audio"].(bool); ok {
		audio = v
	}

	// The room already has this desktop encoded and on the wire. Forwarding
	// that is the whole point of the tee: a destination costs a mux and a
	// socket instead of a second encoder competing with the session people are
	// watching.
	if s.room != nil {
		platform := strings.ToLower(strings.TrimSpace(argStr(args, "platform")))
		if platform == "" {
			platform = "custom"
		}
		kf, _ := args["keyframes"].(bool)
		if !s.room.CanRestream() {
			return textContent("this session is encoding VP8, which no streaming " +
				"destination accepts; restart the desktop with ENCODER=x264"), true
		}
		t := media.RestreamTarget{
			ID: platform, Platform: platform, URL: url, Audio: audio,
			KeyframeSec: restreamKeyframes(platform, kf),
		}
		if err := s.room.StartRestream(t); err != nil {
			return textContent("start_restream failed: %v", err), true
		}
		return textContent("streaming to %s (%s, audio=%v) — reusing the live encode, "+
			"no second capture", platform, redactKey(url), audio), false
	}

	// No room: nothing is being captured, so there is nothing to reuse and a
	// capture of our own is the only option. This is the standalone bridge
	// process, where nobody is watching a session to interrupt.
	s.restreamMu.Lock()
	defer s.restreamMu.Unlock()
	if s.restream != nil {
		return textContent("a re-stream is already running to %s", s.restreamURL), true
	}
	kbps := argInt(args, "bitrate")
	if kbps <= 0 {
		kbps = 3000
	}
	fps := argInt(args, "fps")
	if fps <= 0 {
		fps = 30
	}
	sink := "rtmpsink"
	if strings.HasPrefix(url, "srt://") {
		sink = "srtsink"
	}
	desc := fmt.Sprintf(
		"flvmux name=mux streamable=true ! %s location=%s "+
			"ximagesrc display-name=%s show-pointer=true use-damage=0 "+
			"! video/x-raw,framerate=%d/1 ! videoconvert ! queue "+
			"! x264enc tune=zerolatency speed-preset=veryfast bitrate=%d key-int-max=%d "+
			"! h264parse ! mux.",
		sink, url, s.display, fps, kbps, fps*2)
	if audio {
		desc += fmt.Sprintf(" pulsesrc device=%s ! audioconvert ! audioresample ! queue ! avenc_aac bitrate=128000 ! aacparse ! mux.", s.cfg.AudioDevice)
	}
	cmd := exec.Command("gst-launch-1.0", media.SplitArgs(desc)...)
	cmd.Env = append(os.Environ(), "DISPLAY="+s.display)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return textContent("start_restream failed: %v", err), true
	}
	go cmd.Wait()
	s.restream = cmd
	s.restreamURL = url
	return textContent("re-streaming to %s (%d kbps, %d fps)", url, kbps, fps), false
}

// restreamKeyframes mirrors the toolbar's rule so the agent and a person
// starting the same destination get the same stream. See platformKeyframes in
// the stream package for why the platforms are not asked.
func restreamKeyframes(platform string, wanted bool) int {
	switch platform {
	case "youtube", "twitch", "facebook":
		return 2
	}
	if wanted {
		return 2
	}
	return 0
}

// redactKey keeps the stream key out of the transcript. It is a credential:
// whoever reads it can broadcast to that channel.
func redactKey(raw string) string {
	i := strings.LastIndex(raw, "/")
	if i < 0 || i == len(raw)-1 {
		return raw
	}
	return raw[:i+1] + "•••"
}

func (s *Server) toolListRestreams() ([]map[string]any, bool) {
	if s.room == nil {
		s.restreamMu.Lock()
		defer s.restreamMu.Unlock()
		if s.restream == nil {
			return textContent("not streaming anywhere"), false
		}
		return textContent("streaming to %s (standalone capture)", redactKey(s.restreamURL)), false
	}
	list := s.room.Restreams()
	if len(list) == 0 {
		return textContent("not streaming anywhere"), false
	}
	lines := make([]string, 0, len(list))
	for _, d := range list {
		lines = append(lines, fmt.Sprintf("%s → %s (audio=%v, %ds)",
			d.Platform, d.URL, d.Audio, d.Seconds))
	}
	return textContent("%s", strings.Join(lines, "\n")), false
}

func (s *Server) toolStopRestream(args map[string]any) ([]map[string]any, bool) {
	if s.room != nil {
		list := s.room.Restreams()
		if len(list) == 0 {
			return textContent("no re-stream running"), true
		}
		if id := argStr(args, "id"); id != "" {
			if err := s.room.StopRestream(id); err != nil {
				return textContent("%v", err), true
			}
			return textContent("stopped streaming to %s", id), false
		}
		for _, d := range list {
			_ = s.room.StopRestream(d.ID)
		}
		return textContent("stopped %d destination(s)", len(list)), false
	}

	s.restreamMu.Lock()
	defer s.restreamMu.Unlock()
	if s.restream == nil {
		return textContent("no re-stream running"), true
	}
	_ = s.restream.Process.Signal(syscall.SIGINT)
	time.Sleep(500 * time.Millisecond)
	if s.restream.ProcessState == nil || !s.restream.ProcessState.Exited() {
		_ = s.restream.Process.Kill()
	}
	url := s.restreamURL
	s.restream = nil
	s.restreamURL = ""
	return textContent("stopped re-stream to %s", url), false
}

// --- info ------------------------------------------------------------------

func (s *Server) toolDesktopInfo() ([]map[string]any, bool) {
	w, h := s.injector.Screen()
	wm, _ := s.output("wmctrl", "-m")
	wmName := ""
	for _, line := range strings.Split(wm, "\n") {
		if strings.HasPrefix(line, "Name:") {
			wmName = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		}
	}
	uptime, _ := s.output("uptime")
	free, _ := s.output("sh", "-c", "free -m | awk 'NR==2{print $3\"/\"$2\" MB\"}'")
	s.restreamMu.Lock()
	restreaming := s.restream != nil
	s.restreamMu.Unlock()
	return jsonContent(map[string]any{
		"window_manager": wmName,
		"resolution":     fmt.Sprintf("%dx%d", w, h),
		"display":        s.display,
		"encoder":        s.cfg.Encoder,
		"uptime":         strings.TrimSpace(uptime),
		"memory_used":    strings.TrimSpace(free),
		"joystick":       s.joystick.Available(),
		"recording":      s.recorder.Status()["recording"],
		"restreaming":    restreaming,
	}), false
}

// --- utilidades ------------------------------------------------------------

func floatSlice(v any) []float64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, e := range arr {
		if f, ok := e.(float64); ok {
			out = append(out, f)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = json.Marshal
