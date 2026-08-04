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

// A minimal Chrome DevTools Protocol client.
//
// With CDP the browser stops being a picture: the real DOM can be queried and
// operated — read text, click by selector, run JavaScript. It is to web pages
// what AT-SPI is to native applications.
//
// Tab discovery goes over HTTP (/json); commands go over a WebSocket to the
// chosen tab.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const cdpEndpoint = "http://127.0.0.1:9222"

type cdpTarget struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Debugger string `json:"webSocketDebuggerUrl"`
}

// cdpTargets lists the available tabs.
func cdpTargets() ([]cdpTarget, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(cdpEndpoint + "/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var all []cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, err
	}
	var pages []cdpTarget
	for _, t := range all {
		if t.Type == "page" && t.Debugger != "" && !strings.HasPrefix(t.URL, "devtools://") {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// cdpEval runs JavaScript in the active tab and returns the result.
func cdpEval(expression string) (string, error) {
	targets, err := cdpTargets()
	if err != nil {
		return "", fmt.Errorf("CDP unavailable (did you open the browser with browser_open?): %w", err)
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("no tabs are open")
	}

	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	conn, _, err := dialer.Dial(targets[0].Debugger, nil)
	if err != nil {
		return "", fmt.Errorf("CDP connection: %w", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	req := map[string]any{
		"id":     1,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expression,
			"returnByValue": true,
			"awaitPromise":  true,
			// Unlocks APIs that demand a "user gesture" — autoplay and friends.
			"userGesture": true,
		},
	}
	if err := conn.WriteJSON(req); err != nil {
		return "", err
	}

	for {
		var raw map[string]any
		if err := conn.ReadJSON(&raw); err != nil {
			return "", err
		}
		if id, ok := raw["id"].(float64); !ok || int(id) != 1 {
			continue // a protocol event, not our reply
		}
		if e, ok := raw["error"].(map[string]any); ok {
			return "", fmt.Errorf("CDP: %v", e["message"])
		}
		result, _ := raw["result"].(map[string]any)
		if ex, ok := result["exceptionDetails"].(map[string]any); ok {
			return "", fmt.Errorf("JS: %v", ex["text"])
		}
		inner, _ := result["result"].(map[string]any)
		if v, ok := inner["value"]; ok {
			switch typed := v.(type) {
			case string:
				return typed, nil
			default:
				b, _ := json.Marshal(typed)
				return string(b), nil
			}
		}
		if d, ok := inner["description"].(string); ok {
			return d, nil
		}
		return "(no value)", nil
	}
}
