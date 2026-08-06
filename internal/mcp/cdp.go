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

// cdpConn is one WebSocket to one tab.
//
// CDP multiplexes two things down this socket: replies, which carry the id of
// the request they answer, and events, which carry a method and no id. A client
// that only ever wants replies can discard everything without an id, which is
// what this one did — and is why every browser wait in the catalogue was a
// poll. Separating the two is what makes it possible to ask the browser to say
// when something happens rather than asking it fifty times whether it has.
type cdpConn struct {
	ws   *websocket.Conn
	next int
}

// cdpOpen connects to the first page target.
func cdpOpen() (*cdpConn, error) {
	targets, err := cdpTargets()
	if err != nil {
		return nil, fmt.Errorf("CDP unavailable (did you open the browser with browser_open?): %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no tabs are open")
	}
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	ws, _, err := dialer.Dial(targets[0].Debugger, nil)
	if err != nil {
		return nil, fmt.Errorf("CDP connection: %w", err)
	}
	return &cdpConn{ws: ws}, nil
}

func (c *cdpConn) Close() { c.ws.Close() }

// send writes a command and returns the id to await it by.
func (c *cdpConn) send(method string, params map[string]any) (int, error) {
	c.next++
	req := map[string]any{"id": c.next, "method": method}
	if params != nil {
		req["params"] = params
	}
	return c.next, c.ws.WriteJSON(req)
}

// reply reads until the answer to id arrives, discarding events on the way.
func (c *cdpConn) reply(id int, deadline time.Time) (map[string]any, error) {
	c.ws.SetReadDeadline(deadline)
	for {
		var raw map[string]any
		if err := c.ws.ReadJSON(&raw); err != nil {
			return nil, err
		}
		got, ok := raw["id"].(float64)
		if !ok || int(got) != id {
			continue // an event, or an answer to something else
		}
		if e, ok := raw["error"].(map[string]any); ok {
			return nil, fmt.Errorf("CDP: %v", e["message"])
		}
		result, _ := raw["result"].(map[string]any)
		return result, nil
	}
}

// event reads until one of the named events arrives, discarding replies.
//
// The names are plural because a page can finish in more than one way and a
// waiter that knew only the happy one would sit until the deadline on every
// other: a navigation that is cancelled, or answered by a download, never fires
// a load event at all.
func (c *cdpConn) event(deadline time.Time, methods ...string) (string, map[string]any, error) {
	want := map[string]bool{}
	for _, m := range methods {
		want[m] = true
	}
	c.ws.SetReadDeadline(deadline)
	for {
		var raw map[string]any
		if err := c.ws.ReadJSON(&raw); err != nil {
			return "", nil, err
		}
		method, ok := raw["method"].(string)
		if !ok || !want[method] {
			continue
		}
		params, _ := raw["params"].(map[string]any)
		return method, params, nil
	}
}

// evalResult turns Runtime.evaluate's reply into the string tools report.
func evalResult(result map[string]any) (string, error) {
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

// cdpEval runs JavaScript in the active tab and returns the result.
func cdpEval(expression string) (string, error) {
	return cdpEvalTimeout(expression, 30*time.Second)
}

// cdpEvalTimeout is cdpEval with the read deadline under the caller's control.
//
// It matters for expressions that deliberately take their time. A page-side
// wait resolves its promise when a node appears, which can be a minute away,
// and a fixed thirty-second deadline would abandon the socket long before the
// answer the caller asked for.
func cdpEvalTimeout(expression string, timeout time.Duration) (string, error) {
	c, err := cdpOpen()
	if err != nil {
		return "", err
	}
	defer c.Close()

	id, err := c.send("Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
		// Unlocks APIs that demand a "user gesture" — autoplay and friends.
		"userGesture": true,
	})
	if err != nil {
		return "", err
	}
	result, err := c.reply(id, time.Now().Add(timeout))
	if err != nil {
		return "", err
	}
	return evalResult(result)
}

// cdpNavigate goes to a URL and waits until the page has actually loaded.
//
// browser_goto used to assign location.href and report "navigating" — true at
// the instant it was said and useless a moment later, because the caller was
// then holding a success message for a page that did not exist yet. Every tool
// called after it raced the load, and the usual repair was for the model to
// guess at a sleep.
//
// Page.navigate reports failures location.href cannot: a bad scheme, a blocked
// URL, a host that does not resolve, all of which are silent when assigned to
// href. Page.loadEventFired then says when the document is done, which is the
// question the caller was really asking.
func cdpNavigate(url string, timeout time.Duration) (string, error) {
	c, err := cdpOpen()
	if err != nil {
		return "", err
	}
	defer c.Close()
	deadline := time.Now().Add(timeout)

	// Page.enable first, or the load events never arrive at all.
	id, err := c.send("Page.enable", nil)
	if err != nil {
		return "", err
	}
	if _, err := c.reply(id, deadline); err != nil {
		return "", fmt.Errorf("enable page events: %w", err)
	}

	id, err = c.send("Page.navigate", map[string]any{"url": url})
	if err != nil {
		return "", err
	}
	result, err := c.reply(id, deadline)
	if err != nil {
		return "", err
	}
	if msg, ok := result["errorText"].(string); ok && msg != "" {
		return "", fmt.Errorf("navigation refused: %s", msg)
	}

	method, _, err := c.event(deadline, "Page.loadEventFired", "Page.frameStoppedLoading")
	if err != nil {
		// The navigation was accepted; only the confirmation is missing. Saying
		// so is more useful than an error, because the page may well be there.
		return fmt.Sprintf("navigated to %s, but it had not finished loading after %s", url, timeout), nil
	}
	if method == "Page.frameStoppedLoading" {
		return fmt.Sprintf("navigated to %s (frame stopped loading)", url), nil
	}
	return fmt.Sprintf("loaded %s", url), nil
}
