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

// Package mcpclient talks to the SentinelDesk MCP server.
//
// It is an ordinary MCP client and holds no privilege the socket does not
// grant. That is the whole security argument for the second binary: if the
// agent can only do what any other host can do through the same socket, then
// the socket is the boundary, and it is one boundary rather than two. Nothing
// here imports the server's dispatch, and anything the runtime needs that
// cannot be said as an MCP call is a gap to fix in the server.
//
// What separates this from a naive client is the four things the server was
// taught during stage 1 that most hosts ignore:
//
//   - progress, so a tool that runs for minutes is not indistinguishable from
//     one that has hung;
//   - events, so the agent can be TOLD a person took the controls rather than
//     discovering it when its next click is refused;
//   - cancellation that actually interrupts a call in flight;
//   - denial kinds, so "wait your turn" and "you may never do this" are not the
//     same sentence.
//
// A client that drops any of those turns a server capability into a server
// behaviour nobody can act on.
package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocolVersion is what this client declares in initialize. It matches the
// server's, deliberately: two halves of one project disagreeing about their own
// protocol version is a bug report waiting to be filed against the wrong half.
const ProtocolVersion = "2024-11-05"

// Client is one connection to the MCP server.
//
// Safe for concurrent use. Requests are matched by id and answered on whichever
// goroutine sent them; notifications go to the handlers. One reader owns the
// stream, because two goroutines reading a JSON-RPC connection is how a reply
// ends up delivered to the wrong caller.
type Client struct {
	transport Transport
	enc       *json.Encoder
	dec       *bufio.Reader

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	waiting map[int64]chan *response
	closed  bool
	closeMu sync.Once
	done    chan struct{}

	// What the server said about itself, kept from initialize.
	serverInfo   ServerInfo
	connectionID uint64

	// Handlers, set before Start and read afterwards without a lock. Both are
	// optional: a client that wants neither is still correct, it is only
	// blinder.
	OnProgress func(Progress)
	OnEvent    func(Event)

	// Provenance stamped onto every call's _meta. It is what turns a scatter of
	// rows in the server's action log into one auditable job — see the server's
	// section 12.3. Guarded because a runtime sets it per task.
	provMu sync.RWMutex
	task   string
	goal   string
}

// Transport is where the JSON-RPC lines go.
//
// Two exist, and one of them is production. The runtime ships as a supervised
// process inside the container, beside the daemon and under the same user
// (ADR-004), so it opens the socket directly — no bridge process, nothing to
// reap, one fewer thing between the loop and the desktop.
//
// The stdio bridge is for developing from a machine that is not the desktop.
// It spawns `docker exec … sentineldesk -mcp-stdio`, which is the path an
// external AI host takes, so it is worth keeping and worth not defaulting to.
type Transport interface {
	io.ReadWriteCloser
}

// New wraps an already-open transport. Start must be called before use.
func New(t Transport) *Client {
	return &Client{
		transport: t,
		enc:       json.NewEncoder(t),
		dec:       bufio.NewReaderSize(t, 64*1024),
		waiting:   map[int64]chan *response{},
		done:      make(chan struct{}),
	}
}

// --- the wire ---------------------------------------------------------------

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Params json.RawMessage `json:"params"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc %d: %s", e.Code, e.Message) }

// Start begins reading and performs the handshake.
func (c *Client) Start(ctx context.Context, clientName, version string) error {
	go c.read()

	var init struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      ServerInfo `json:"serverInfo"`
		Meta            struct {
			ConnectionID uint64 `json:"sentineldesk/connectionId"`
		} `json:"_meta"`
	}
	if err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": version},
	}, &init); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	c.serverInfo = init.ServerInfo
	c.connectionID = init.Meta.ConnectionID

	// A notification, so nothing is waited for. Skipping it leaves a
	// specification-following server waiting for a handshake step that never
	// arrives.
	return c.notify("notifications/initialized", nil)
}

// ServerInfo is what the server calls itself.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (c *Client) ServerInfo() ServerInfo { return c.serverInfo }

// ConnectionID is the number the server gave this connection, from initialize's
// _meta. Whatever supervises the runtime quotes it back to halt this connection
// and no others — an emergency stop has to be able to name somebody.
func (c *Client) ConnectionID() uint64 { return c.connectionID }

// read owns the stream. Every line is either a reply to somebody waiting or a
// notification for a handler; anything else is dropped rather than guessed at.
func (c *Client) read() {
	defer c.failWaiters()
	for {
		line, err := c.dec.ReadBytes('\n')
		if len(line) > 0 {
			var msg response
			if json.Unmarshal(line, &msg) == nil {
				c.deliver(&msg)
			}
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) deliver(msg *response) {
	// A notification has no id. Filing those by id would put every one of them
	// under the same key, each overwriting the last.
	if msg.ID == nil {
		c.handleNotification(msg)
		return
	}
	c.mu.Lock()
	ch, ok := c.waiting[*msg.ID]
	delete(c.waiting, *msg.ID)
	c.mu.Unlock()
	if ok {
		ch <- msg
	}
}

// failWaiters unblocks everyone still waiting when the connection ends.
// Without it a host that died mid-call leaves its callers parked until their
// own timeouts, each of which reports a timeout for what was a disconnection.
func (c *Client) failWaiters() {
	c.mu.Lock()
	c.closed = true
	waiting := c.waiting
	c.waiting = map[int64]chan *response{}
	c.mu.Unlock()
	for _, ch := range waiting {
		ch <- &response{Error: &rpcError{Code: -32000, Message: "the connection closed"}}
	}
	c.closeMu.Do(func() { close(c.done) })
}

func (c *Client) send(r request) error {
	r.JSONRPC = "2.0"
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.enc.Encode(r)
}

func (c *Client) notify(method string, params any) error {
	return c.send(request{Method: method, Params: params})
}

// request sends and waits, decoding the result into out.
//
// A cancelled context sends notifications/cancelled before returning. That is
// the difference between a cancel that stops work and one that only stops
// waiting for it: the server interrupts the call in flight, and a tool that was
// installing packages does not carry on for another minute with nobody left to
// answer.
func (c *Client) request(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	ch := make(chan *response, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("the connection is closed")
	}
	c.waiting[id] = ch
	c.mu.Unlock()

	if err := c.send(request{ID: &id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		_ = c.notify("notifications/cancelled", map[string]any{
			"requestId": id, "reason": ctx.Err().Error(),
		})
		// Still wait for the reply the server owes, briefly. It arrives as a
		// cancellation result and clears the entry; giving up here instead
		// would leak the waiter for the life of the connection.
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			c.mu.Lock()
			delete(c.waiting, id)
			c.mu.Unlock()
		}
		return ctx.Err()
	case msg := <-ch:
		if msg.Error != nil {
			return msg.Error
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}
}

// Close ends the connection.
func (c *Client) Close() error { return c.transport.Close() }

// Done is closed when the connection ends, however it ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// --- provenance -------------------------------------------------------------

// SetTask stamps every subsequent call with a task id and the goal behind it.
//
// The server records both and groups by them. It cannot derive either: it sees
// what was called and by which connection, and a job a person would describe in
// one sentence arrives as unrelated rows. Only the caller knows where a job
// starts and why it started, so the caller is asked.
func (c *Client) SetTask(task, goal string) {
	c.provMu.Lock()
	defer c.provMu.Unlock()
	c.task, c.goal = task, goal
}

func (c *Client) provenance() map[string]any {
	c.provMu.RLock()
	defer c.provMu.RUnlock()
	if c.task == "" && c.goal == "" {
		return nil
	}
	meta := map[string]any{}
	if c.task != "" {
		meta["sentineldesk/taskId"] = c.task
	}
	if c.goal != "" {
		meta["sentineldesk/goal"] = c.goal
	}
	return meta
}

// --- tools ------------------------------------------------------------------

// Tool is one entry in the catalogue, including the annotations the server
// publishes that the specification does not define.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations struct {
		ReadOnly    bool `json:"readOnlyHint"`
		Destructive bool `json:"destructiveHint"`

		// Whether the call is held at the room gate until the agent holds the
		// controls. Not derivable by a client, which is why it is published.
		RequiresControl bool `json:"sentineldesk/requiresControl"`

		// hidden | visible | injects — whether a person sharing the desktop
		// sees this happen. The runtime reads it to substitute a visible tool
		// for an invisible one when its role calls for evidence, and it is not
		// the same question as RequiresControl: browser_open puts a page on a
		// screen people are watching and is ungated.
		Visibility string `json:"sentineldesk/visibility"`
	} `json:"annotations"`
}

// ListTools reads the catalogue. Called on connect and again on reconnect.
//
// There is nothing to subscribe to: the server declares no tools/list_changed
// capability because its catalogue is static per process, and declaring one
// would be announcing something that does not happen.
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.request(ctx, "tools/list", nil, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// Content is one block of a tool's answer.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

// Result is what a tool call returned.
type Result struct {
	Content []Content
	IsError bool

	// Denial is the machine-readable reason a call was refused, from the
	// result's _meta. Empty when the call was not refused.
	//
	// This is the field that decides what to do next, and collapsing it into
	// "it failed" is the specific mistake it exists to prevent: DenialPolicy
	// means stop, DenialRoom means ask a person and try again. An agent that
	// treats them alike either gives up when it should wait or retries forever
	// against a rule.
	Denial Denial
}

// Text joins the text blocks, which is what a model reads.
func (r Result) Text() string {
	var parts []string
	for _, block := range r.Content {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "image":
			parts = append(parts, "["+block.MimeType+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// Denial is why a call was refused.
type Denial string

const (
	DenialNone Denial = ""

	// DenialPolicy — the server will never allow this. Stop; do not retry, and
	// do not ask a person, because no person in the room can widen MCP_POLICY.
	DenialPolicy Denial = "policy"

	// DenialRoom — somebody else is driving. Ask for the controls and retry.
	// This is the one that must not be confused with the one above.
	DenialRoom Denial = "room"

	DenialUnknownTool Denial = "unknown_tool"
	DenialToolError   Denial = "tool_error"
	DenialCancelled   Denial = "cancelled"
	DenialEmergency   Denial = "emergency"

	// DenialBadArgs — an argument the tool does not take. Retryable after
	// fixing it, and the message names both what was wrong and what it accepts.
	DenialBadArgs Denial = "bad_arguments"
)

// Retryable reports whether trying the same call again could succeed. A room
// denial can, once somebody hands over the controls; a policy denial never can,
// and neither does an emergency stop, which is a person saying stop.
func (d Denial) Retryable() bool {
	return d == DenialRoom || d == DenialToolError
}

// Call runs a tool.
//
// A refused call is not an error here. It returns a Result with IsError set and
// a Denial saying why, because the refusal is information the caller has to act
// on — flattening it into a Go error would throw away the one field that says
// what to do next.
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (Result, error) {
	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}

	meta := c.provenance()
	if meta == nil {
		meta = map[string]any{}
	}
	// A progress token on every call, always.
	//
	// The server sends nothing unless asked, which is right — a host that did
	// not opt in should not be handed a stream to discard. But a *runtime*
	// always wants it: a tool that runs for minutes and says nothing is
	// indistinguishable from one that has hung, and that is the difference
	// between waiting and reporting a hang. Asking costs one field, and a
	// runtime with no OnProgress set drops the notifications at no cost.
	//
	// The token is this call's request id, which is unique per connection by
	// construction and needs no counter of its own.
	token := fmt.Sprintf("call-%d", c.nextID.Load()+1)
	meta["progressToken"] = token
	params["_meta"] = meta

	var raw struct {
		Content []Content `json:"content"`
		IsError bool      `json:"isError"`
		Meta    struct {
			Denial string `json:"sentineldesk/denial"`
		} `json:"_meta"`
	}
	if err := c.request(ctx, "tools/call", params, &raw); err != nil {
		return Result{}, err
	}
	return Result{
		Content: raw.Content,
		IsError: raw.IsError,
		Denial:  Denial(raw.Meta.Denial),
	}, nil
}

// Restrict narrows this connection's policy. It may only ever narrow: the
// server refuses a level above its own ceiling, so a runtime cannot widen
// itself by asking nicely, and a sub-agent given a smaller catalogue really has
// one rather than being trusted to stay inside it.
func (c *Client) Restrict(ctx context.Context, level string, deny, allow []string) (map[string]any, error) {
	params := map[string]any{}
	if level != "" {
		params["level"] = level
	}
	if len(deny) > 0 {
		params["deny"] = strings.Join(deny, ",")
	}
	if len(allow) > 0 {
		params["allow"] = strings.Join(allow, ",")
	}
	var out map[string]any
	err := c.request(ctx, "sentineldesk/policy", params, &out)
	return out, err
}

// --- transports -------------------------------------------------------------

// stdioTransport runs a command whose stdin and stdout are the connection.
type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	once   sync.Once
}

func (t *stdioTransport) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *stdioTransport) Write(p []byte) (int, error) { return t.stdin.Write(p) }

func (t *stdioTransport) Close() error {
	t.once.Do(func() {
		_ = t.stdin.Close()
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		// Reaped, or the bridge stays a zombie for the life of the runtime.
		// The same mistake was already made once in internal/shell.
		go func() { _ = t.cmd.Wait() }()
	})
	return nil
}

// DialUnix opens the daemon's socket directly.
//
// The production path. The socket is mode 0600 and owned by the desktop user,
// so this works because the runtime runs as that user — and fails as a
// permission error rather than silently doing something else if it does not,
// which is the right way round.
func DialUnix(path string) (Transport, error) {
	conn, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("mcp socket %s: %w", path, err)
	}
	return conn, nil
}

// DialStdio spawns a bridge process and speaks to its stdin and stdout.
//
// This is the path an AI host takes — `sentineldesk -mcp-stdio` under
// `docker exec` — so a runtime that works through it works through the arrangement
// that actually ships. Killing the bridge never touches the desktop, which is
// the reason the sub-command exists at all.
func DialStdio(name string, args ...string) (Transport, error) {
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	return &stdioTransport{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}
