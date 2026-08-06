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

import (
	"encoding/json"
	"fmt"
	"github.com/lordbasex/sentineldesk/internal/config"
	"github.com/lordbasex/sentineldesk/internal/desktop"
	"github.com/lordbasex/sentineldesk/internal/media"
	"github.com/lordbasex/sentineldesk/internal/version"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor/pkg/cc"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// wsMsg is the signalling envelope that travels over the WebSocket.
type wsMsg struct {
	Type          string  `json:"type"`
	SDP           string  `json:"sdp,omitempty"`
	Candidate     string  `json:"candidate,omitempty"`
	SDPMLineIndex *uint16 `json:"sdpMLineIndex,omitempty"`

	// Authentication: the mandatory first frame, of type "auth".
	User   string `json:"user,omitempty"`
	Pass   string `json:"pass,omitempty"`
	Token  string `json:"token,omitempty"`
	OK     *bool  `json:"ok,omitempty"`
	Reason string `json:"reason,omitempty"`

	// Post-auth configuration (type "config"). Nothing sensitive over HTTP.
	IceServers   []iceServer `json:"iceServers,omitempty"`
	Encoder      string      `json:"encoder,omitempty"`
	RemoteCursor *bool       `json:"remoteCursor,omitempty"`
	Version      string      `json:"version,omitempty"`
}

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// WebSocket limits. An auth frame is around 100 bytes, so nothing larger than
// 4 KB is accepted before authentication, and a short deadline stops sockets
// from hanging around without ever identifying themselves.
const (
	wsAuthReadLimit = 4 << 10
	wsReadLimit     = 512 << 10
	wsAuthDeadline  = 10 * time.Second
	wsPingEvery     = 30 * time.Second
	wsPongWait      = 90 * time.Second
)

// inputEvent is one event received over the DataChannel: keyboard, mouse,
// clipboard or gamepad.
type inputEvent struct {
	T      string    `json:"t"`
	X      int       `json:"x"`
	Y      int       `json:"y"`
	B      int       `json:"b"`
	D      int       `json:"d"`
	Dy     int       `json:"dy"`
	Dx     int       `json:"dx"`
	K      string    `json:"k"`
	Clip   string    `json:"clip"`   // clipboard text (browser -> desktop)
	Action string    `json:"action"` // capture: shot | rec_start | rec_stop
	Format string    `json:"format"` // capture: mp4 | webm | mkv
	GB     []float64 `json:"gb"`     // gamepad button state
	GA     []float64 `json:"ga"`     // gamepad axis state
	ReqID  int       `json:"req"`    // control_answer: which request is answered
	Grant  bool      `json:"grant"`  // control_answer: allowed or refused

	RS *restreamCmd `json:"rs"` // restream: where to send the desktop
}

// restreamCmd asks for an external destination to be attached or detached.
type restreamCmd struct {
	Action   string `json:"action"`   // start | stop | list
	ID       string `json:"id"`       // which destination to stop
	Platform string `json:"platform"` // youtube | twitch | facebook | custom
	URL      string `json:"url"`
	Audio    bool   `json:"audio"`

	// Keyframes is the answer to "can a viewer show up mid-stream?", asked of
	// the person who knows: whoever typed the address. It only has a say for a
	// custom destination — the platforms decide for themselves below.
	Keyframes bool `json:"kf"`
}

// Event types that are not input but room control:
//   take_control    — ask for control; cooperative between humans, so granted
//   release_control — pass it to the next participant
//   control_answer  — allow or refuse the agent's request

// Session is one client's WebRTC connection: its own PeerConnection and
// DataChannel, but the capture is shared with everyone else through the room.
type Session struct {
	cfg      config.Config
	strategy media.EncoderStrategy
	room     *Room
	memberID string
	recorder *media.Recorder // shared with the MCP: one recording at a time
	delivery *Delivery
	upstream *media.Upstream // the browser's microphone into the desktop
	injector *desktop.InputInjector
	cursors  *desktop.CursorTracker // may be nil (no XFixes, or a remote cursor)
	clip     *desktop.Clipboard
	joystick *desktop.Joystick // may be nil (no /dev/uinput)
	auth     *Auth
	gate     *IPGate
	GateKey  string
	ws       *websocket.Conn
	peer     string

	wsMu       sync.Mutex
	pc         *webrtc.PeerConnection
	estimatorC chan cc.BandwidthEstimator
	cursorCh   chan desktop.CursorState
	lastClip   string
	closeOnce  sync.Once
	done       chan struct{}

	chanMu  sync.Mutex
	channel *webrtc.DataChannel // the input channel; it also carries presence
}

// connectionAlive reports whether this session is still usable.
//
// `connecting` counts as alive: that is a legitimate session still negotiating,
// and taking control from it would be stealing from someone about to arrive.
// Only `failed`, `closed` and `disconnected` are unambiguously dead.
func (s *Session) connectionAlive() bool {
	if s == nil || s.pc == nil {
		return true // not started yet: it cannot be declared dead
	}
	switch s.pc.ConnectionState() {
	case webrtc.PeerConnectionStateFailed,
		webrtc.PeerConnectionStateClosed,
		webrtc.PeerConnectionStateDisconnected:
		return false
	}
	return true
}

// sendOnChannel sends a message over the DataChannel if it is already open. The
// room uses it to broadcast presence and other people's pointers.
func (s *Session) sendOnChannel(text string) {
	s.chanMu.Lock()
	ch := s.channel
	s.chanMu.Unlock()
	if ch != nil && ch.ReadyState() == webrtc.DataChannelStateOpen {
		ch.SendText(text)
	}
}

func NewSession(cfg config.Config, strategy media.EncoderStrategy, room *Room, up *media.Upstream, rec *media.Recorder, deliver *Delivery, injector *desktop.InputInjector, cursors *desktop.CursorTracker, clip *desktop.Clipboard, joystick *desktop.Joystick, auth *Auth, gate *IPGate, GateKey string, ws *websocket.Conn, peer string) *Session {
	return &Session{
		cfg:      cfg,
		strategy: strategy,
		room:     room,
		recorder: rec,
		delivery: deliver,
		upstream: up,
		injector: injector,
		cursors:  cursors,
		clip:     clip,
		joystick: joystick,
		auth:     auth,
		gate:     gate,
		GateKey:  GateKey,
		ws:       ws,
		peer:     peer,
		done:     make(chan struct{}),
	}
}

func (s *Session) logf(format string, args ...any) {
	log.Printf("[%s] "+format, append([]any{s.peer}, args...)...)
}

func (s *Session) send(msg wsMsg) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	if err := s.ws.WriteJSON(msg); err != nil {
		s.logf("error sending over the websocket: %v", err)
	}
}

// Run serves the connection: authentication first — without it there is no
// WebRTC handshake at all — then signalling until the client goes away.
func (s *Session) Run() {
	defer s.Close()
	s.logf("client connected")

	if !s.authenticate() {
		return
	}

	// Keepalive: periodic ping, with the read deadline renewed by each pong.
	// Without it a NAT that drops the connection leaves the session hanging
	// forever, holding a slot and possibly the control token.
	s.ws.SetReadDeadline(time.Now().Add(wsPongWait))
	s.ws.SetPongHandler(func(string) error {
		return s.ws.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	go func() {
		ticker := time.NewTicker(wsPingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.done:
				return
			case <-ticker.C:
				s.wsMu.Lock()
				s.ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
				s.wsMu.Unlock()
			}
		}
	}()

	s.sendConfig()

	if err := s.start(); err != nil {
		s.logf("could not start the session: %v", err)
		// Say why. Retrying blindly against a full room just produces a
		// reconnect loop with no explanation on screen.
		s.send(wsMsg{Type: "fatal", Reason: err.Error()})
		return
	}

	for {
		var msg wsMsg
		if err := s.ws.ReadJSON(&msg); err != nil {
			s.logf("client disconnected: %v", err)
			return
		}
		switch msg.Type {
		case "answer":
			desc := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: msg.SDP}
			if err := s.pc.SetRemoteDescription(desc); err != nil {
				s.logf("could not apply the answer: %v", err)
			}
		case "renegotiate":
			// The browser wants to start publishing its microphone.
			//
			// replaceTrack alone is not enough. When the session was negotiated
			// the browser had nothing to send, so it answered that media line
			// `inactive` and it stayed switched off. We have to offer again so
			// the browser can answer `sendonly` — this time with the track
			// actually attached.
			if err := s.renegotiate(); err != nil {
				s.logf("renegotiation failed: %v", err)
			}
		case "ice":
			if msg.Candidate == "" {
				continue
			}
			init := webrtc.ICECandidateInit{
				Candidate:     msg.Candidate,
				SDPMLineIndex: msg.SDPMLineIndex,
			}
			if err := s.pc.AddICECandidate(init); err != nil {
				s.logf("candidato ICE rechazado: %v", err)
			}
		}
	}
}

// authenticate requires the FIRST frame to be a valid "auth", under a short
// deadline and a small read limit. Anything else — a WebRTC offer, an unknown
// type, silence — ends the connection. There is no second chance on the same
// socket, which is what gives the per-origin gate (IPGate) its meaning.
func (s *Session) authenticate() bool {
	s.ws.SetReadLimit(wsAuthReadLimit)
	s.ws.SetReadDeadline(time.Now().Add(wsAuthDeadline))

	deny := func(reason string) bool {
		no := false
		s.send(wsMsg{Type: "auth", OK: &no, Reason: reason})
		s.wsMu.Lock()
		s.ws.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "auth failed"),
			time.Now().Add(time.Second))
		s.wsMu.Unlock()
		return false
	}

	// A banned origin usually never reaches this point — it is cut off at the
	// upgrade — but a ban can land while the connection is already accepted.
	if s.gate.Banned(s.GateKey) {
		return deny("locked")
	}

	var msg wsMsg
	if err := s.ws.ReadJSON(&msg); err != nil {
		return false // silencio o basura: ni respuesta merece
	}
	if msg.Type != "auth" {
		// Anyone opening with something else is not one of our browsers. They
		// get exactly the same answer as a wrong password: nothing in the
		// response tells them which part failed.
		s.gate.Fail(s.GateKey)
		return deny("invalid credentials")
	}
	if s.auth.Enabled() {
		ok := s.auth.ValidToken(msg.Token) ||
			(msg.User != "" && s.auth.ValidCredentials(msg.User, msg.Pass))
		if !ok {
			s.gate.Fail(s.GateKey)
			s.logf("access denied")
			return deny("invalid credentials")
		}
	}
	s.gate.pass(s.GateKey)
	s.logf("access granted")

	s.ws.SetReadLimit(wsReadLimit)
	s.ws.SetReadDeadline(time.Time{})
	yes := true
	s.send(wsMsg{Type: "auth", OK: &yes, Token: s.auth.NewToken()})
	return true
}

// sendConfig hands the authenticated client what /config.json used to expose
// over HTTP. The ICE servers carry TURN credentials, which is exactly why they
// must not be reachable before authentication.
func (s *Session) sendConfig() {
	servers := []iceServer{{URLs: []string{s.cfg.ClientStun}}}
	if len(s.cfg.ClientTurnURL) > 0 {
		servers = append(servers, iceServer{
			URLs:       s.cfg.ClientTurnURL,
			Username:   s.cfg.TurnUser,
			Credential: s.cfg.TurnPass,
		})
	}
	remote := s.cfg.RemoteCursor
	s.send(wsMsg{
		Type:         "config",
		IceServers:   servers,
		Encoder:      s.strategy.Name,
		RemoteCursor: &remote,
		// So the rail can say which build this is. After auth on purpose: a
		// version string is a gift to whoever is fingerprinting servers.
		Version: version.Short(),
	})
}

func (s *Session) start() error {
	// Its own API per session: the congestion interceptor (GCC) hands back one
	// estimator per PeerConnection, so this keeps the association unambiguous.
	api, estimatorC, err := newPeerAPI(s.cfg)
	if err != nil {
		return err
	}
	s.estimatorC = estimatorC

	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{s.cfg.StunServer}}},
	})
	if err != nil {
		return fmt.Errorf("NewPeerConnection: %w", err)
	}
	s.pc = pc

	// --- pistas de salida -------------------------------------------------
	videoCaps := webrtc.RTPCodecCapability{MimeType: s.strategy.MimeType, ClockRate: 90000}
	if s.strategy.MimeType == webrtc.MimeTypeH264 {
		videoCaps.SDPFmtpLine = "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f"
	}
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(videoCaps, "video", "desktop")
	if err != nil {
		return err
	}
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio", "desktop",
	)
	if err != nil {
		return err
	}

	videoSender, err := pc.AddTrack(videoTrack)
	if err != nil {
		return err
	}
	audioSender, err := pc.AddTrack(audioTrack)
	if err != nil {
		return err
	}

	// Video RTCP: answer PLI/FIR with an immediate keyframe.
	go s.watchRTCP(videoSender)
	// Audio RTCP: just drain it so the interceptors can do their work.
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := audioSender.Read(buf); err != nil {
				return
			}
		}
	}()

	// --- INCOMING track: the browser's microphone ---------------------------
	// Declared recvonly so the offer reserves a slot for it; the browser turns
	// it on when the person presses the microphone button.
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		s.logf("could not offer audio reception: %v", err)
	}
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		s.handleRemoteTrack(track)
	})

	// --- join the room ------------------------------------------------------
	// The tracks belong to this session; the capture feeding them is shared.
	memberID, isController, err := s.room.Join(s, videoTrack, audioTrack)
	if err != nil {
		return err
	}
	s.memberID = memberID
	s.logf("room: %s (control=%v)", memberID, isController)

	// --- DataChannel de entrada ------------------------------------------
	channel, err := pc.CreateDataChannel("input", nil)
	if err != nil {
		return err
	}
	s.chanMu.Lock()
	s.channel = channel
	s.chanMu.Unlock()
	channel.OnMessage(func(msg webrtc.DataChannelMessage) {
		var ev inputEvent
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			return
		}
		s.handleInput(ev)
	})
	// The real pointer shape (resize arrows, text beam, hand…) travels to the
	// client over the same channel; the browser applies it as a CSS cursor.
	channel.OnOpen(func() {
		// Presence as soon as it opens: who is in the room and who has control.
		s.room.broadcastPresence()
		// And whether a recording is already running. Without this, a reload
		// leaves the button saying "Record" while the server is recording, and
		// the next click fails with a confusing error.
		s.sendCaptureState()
		// Clipboard synchronisation, desktop -> browser.
		go s.watchClipboard(channel)
		// The real pointer shape (resize arrows, text beam, hand…).
		if s.cursors != nil {
			current, updates := s.cursors.Subscribe()
			s.cursorCh = updates
			s.sendCursor(channel, current)
			go func() {
				for {
					select {
					case <-s.done:
						return
					case state := <-updates:
						s.sendCursor(channel, state)
					}
				}
			}()
		}
	})

	// --- signalling ---------------------------------------------------------
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		j := c.ToJSON()
		s.send(wsMsg{Type: "ice", Candidate: j.Candidate, SDPMLineIndex: j.SDPMLineIndex})
	})
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		s.logf("WebRTC state: %s", st)
		switch st {
		case webrtc.PeerConnectionStateConnected:
			// A keyframe the moment it connects: a full picture without waiting
			// for the GOP to come round.
			s.room.ForceKeyFrame()
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			s.Close()
		}
	})

	go s.adaptBitrate()

	// --- oferta -----------------------------------------------------------
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return err
	}
	s.send(wsMsg{Type: "offer", SDP: pc.LocalDescription().SDP})
	s.logf("SDP offer sent (encoder %s)", s.strategy.Name)
	return nil
}

func (s *Session) sendCursor(channel *webrtc.DataChannel, state desktop.CursorState) {
	if state.DataURL == "" {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"t": "cursor", "d": state.DataURL, "x": state.HotX, "y": state.HotY,
	})
	if err == nil {
		channel.SendText(string(payload))
	}
}

// watchRTCP handles incoming RTCP on the video sender. A PLI or FIR means the
// browser lost its reference picture, so force a keyframe immediately rather
// than letting it stare at a smeared image until the next GOP.
func (s *Session) watchRTCP(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		n, _, err := sender.Read(buf)
		if err != nil {
			return
		}
		packets, err := rtcp.Unmarshal(buf[:n])
		if err != nil {
			continue
		}
		for _, pkt := range packets {
			switch pkt.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				// The keyframe reaches everyone, because there is only one
				// encoder. That is the price of sharing the capture, and it is
				// cheap: whoever did not ask for it gets one extra full frame,
				// not an interruption.
				s.room.ForceKeyFrame()
			}
		}
	}
}

// adaptBitrate consumes the bandwidth estimate (GCC over TWCC) and steers the
// encoder's bitrate at runtime.
func (s *Session) adaptBitrate() {
	var estimator cc.BandwidthEstimator
	select {
	case estimator = <-s.estimatorC:
	case <-s.done:
		return
	}

	const floorKbps = 300
	maxKbps := s.cfg.VideoKbps
	current := maxKbps
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		target := estimator.GetTargetBitrate() / 1000 // kbps
		if target > maxKbps {
			target = maxKbps
		}
		if target < floorKbps {
			target = floorKbps
		}
		if target == current {
			continue
		}
		current = target
		// The encoder is shared, so this does not set the bitrate directly. It
		// reports what THIS network can carry and the room applies the minimum
		// across everyone.
		s.room.ReportBitrate(s.memberID, target)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (s *Session) handleInput(ev inputEvent) {
	// Room control: one person drives, the rest watch. These are handled before
	// the control check, because asking for control is precisely what someone
	// who does not have it needs to be able to do.
	switch ev.T {
	case "control_answer":
		// A person answered the agent's request. Anyone in the room may answer:
		// they are all equally entitled, and requiring the current controller
		// would leave the agent stuck whenever that person stepped away.
		s.room.AnswerControlRequest(ev.ReqID, ev.Grant)
		return

	case "take_control":
		if s.room.TakeControl(s.memberID) {
			s.logf("took control")
		}
		return
	case "release_control":
		s.room.ReleaseControl(s.memberID)
		s.logf("released control")
		return
	case "capture":
		// Capture and recording go through the SERVER rather than the browser,
		// so the file comes out as MP4 — which opens anywhere without
		// installing a player — and at the quality of the original framebuffer,
		// without the stream's compression.
		s.handleCapture(ev)
		return
	case "restream":
		s.handleRestream(ev)
		return
	}

	// Screenshots and recording. These go through the SERVER rather than the
	// browser so the file comes out as MP4 — which opens on any operating system
	// with nothing installed — at the framebuffer's own quality, without the
	// stream's compression.
	if ev.T == "capture" {
		s.handleCapture(ev)
		return
	}

	if !s.room.IsController(s.memberID) {
		// A viewer injects nothing, but their pointer is still broadcast: that
		// is what lets the others see what they are looking at or pointing to.
		if ev.T == "mm" {
			s.room.UpdatePointer(s.memberID, ev.X, ev.Y)
		}
		return
	}

	switch ev.T {
	case "mm":
		s.injector.Move(ev.X, ev.Y)
		s.room.UpdatePointer(s.memberID, ev.X, ev.Y)
	case "mb":
		s.injector.Button(ev.B, ev.D == 1)
	case "mw":
		s.injector.Wheel(ev.Dy, ev.Dx)
	case "kb":
		s.injector.Key(ev.K, ev.D == 1)
	case "reset":
		s.injector.ReleaseAll()
	case "clip":
		// Clipboard, browser -> desktop. Remember the value so the watcher does
		// not immediately send it back to us as if it were new.
		if s.clip != nil {
			s.lastClip = ev.Clip
			if err := s.clip.Set(ev.Clip); err != nil {
				s.logf("clipboard: %v", err)
			}
		}
	case "gp":
		s.joystick.Apply(ev.GB, ev.GA)
	}
}

// handleCapture takes a screenshot or drives the recording, and hands the file
// to this browser. Only the controller may: a recording is a single shared
// resource, and two people starting one would fight over the same file.
// platformKeyframes says how often a destination has to send a keyframe.
//
// The platforms are not asked, because the answer is a property of what they
// do: they serve an audience that arrives whenever it likes, and a viewer sees
// nothing at all until the next keyframe. Two seconds is what all three of them
// require.
//
// A destination you point at yourself — VLC on the next desk, OBS on your own
// machine — has no such audience, so it keeps the sparse keyframes the desktop
// normally runs with and gets noticeably sharper text for it.
func platformKeyframes(platform string, wanted bool) int {
	switch platform {
	case "youtube", "twitch", "facebook":
		return 2
	}
	if wanted {
		return 2
	}
	return 0
}

// handleRestream starts or stops sending this desktop somewhere else.
//
// Publishing the session to the internet is held to the same rule as driving
// it: whoever has control decides. A viewer who could start a broadcast would
// be doing something to everyone else's session without holding the turn.
func (s *Session) handleRestream(ev inputEvent) {
	cmd := ev.RS
	if cmd == nil {
		return
	}
	if cmd.Action == "list" {
		s.sendRestreams("")
		return
	}
	if !s.room.IsController(s.memberID) {
		s.sendRestreams("needControl")
		return
	}

	switch cmd.Action {
	case "start":
		id := cmd.ID
		if id == "" {
			id = cmd.Platform
		}
		if id == "" {
			id = "custom"
		}
		err := s.room.StartRestream(media.RestreamTarget{
			ID:          id,
			Platform:    cmd.Platform,
			URL:         cmd.URL,
			Audio:       cmd.Audio,
			KeyframeSec: platformKeyframes(cmd.Platform, cmd.Keyframes),
		})
		if err != nil {
			s.logf("restream refused: %v", err)
			s.sendRestreams(err.Error())
			return
		}
		s.logf("streaming to %s", cmd.Platform)

	case "stop":
		if err := s.room.StopRestream(cmd.ID); err != nil {
			s.sendRestreams(err.Error())
		}
	}
}

// sendRestreams answers just the client that asked, which is what a rejected
// start needs: the error belongs to the person who typed the address, not to
// everyone in the room.
func (s *Session) sendRestreams(problem string) {
	msg := map[string]any{
		"t":    "restreams",
		"list": s.room.Restreams(),
		"able": s.room.CanRestream(),
	}
	if problem != "" {
		msg["error"] = problem
	}
	if payload, err := json.Marshal(msg); err == nil {
		s.sendOnChannel(string(payload))
	}
}

func (s *Session) handleCapture(ev inputEvent) {
	if s.recorder == nil || s.delivery == nil {
		s.sendOnChannel(`{"t":"capture_error","error":"capture unavailable"}`)
		return
	}
	if !s.room.IsController(s.memberID) {
		s.sendOnChannel(`{"t":"capture_error","error":"needControl"}`)
		return
	}

	switch ev.Action {
	case "shot":
		name := "screenshot-" + time.Now().Format("20060102-150405") + ".png"
		path := filepath.Join(s.recorder.Dir, name)
		if err := os.MkdirAll(s.recorder.Dir, 0o755); err != nil {
			s.captureError(err)
			return
		}
		if err := desktop.GrabToFile(s.cfg.Display, path, 0, 0, 0, 0); err != nil {
			s.captureError(err)
			return
		}
		s.delivery.Deliver(path, name)

	case "rec_start":
		container := ev.Format
		if container == "" {
			container = "mp4" // opens everywhere without installing a player
		}
		if _, err := s.recorder.Start(media.RecordOpts{Container: container, Audio: true}); err != nil {
			s.captureError(err)
			return
		}
		s.sendCaptureState()

	case "rec_stop":
		path, _, err := s.recorder.Stop()
		if err != nil {
			s.captureError(err)
			return
		}
		s.sendCaptureState()
		s.delivery.Deliver(path, filepath.Base(path))
	}
}

// sendCaptureState tells this client whether a recording is currently running.
func (s *Session) sendCaptureState() {
	if s.recorder == nil {
		return
	}
	active, _ := s.recorder.Status()["recording"].(bool)
	payload, _ := json.Marshal(map[string]any{"t": "capture_state", "recording": active})
	s.sendOnChannel(string(payload))
}

func (s *Session) captureError(err error) {
	payload, _ := json.Marshal(map[string]string{"t": "capture_error", "error": err.Error()})
	s.sendOnChannel(string(payload))
	s.logf("capture: %v", err)
}

// renegotiate emits a fresh offer over the already established connection.
//
// The client asks for it when switching the microphone on. It is
// another offer on the same PeerConnection: the video does not stop and ICE is
// not rebuilt, only
// the directions of the media lines change.
func (s *Session) renegotiate() error {
	offer, err := s.pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	if err := s.pc.SetLocalDescription(offer); err != nil {
		return err
	}
	s.send(wsMsg{Type: "offer", SDP: s.pc.LocalDescription().SDP})
	s.logf("renegotiating to receive the microphone")
	return nil
}

// handleRemoteTrack pours a track the browser sends into the desktop, as a
// PulseAudio source.
//
// Only the participant holding control may publish. Otherwise two open
// microphones would pile into the same sink and nobody could tell where the
// noise was coming from.
func (s *Session) handleRemoteTrack(track *webrtc.TrackRemote) {
	if s.upstream == nil {
		return
	}
	if !s.room.IsController(s.memberID) {
		s.logf("incoming track ignored: only the controller publishes")
		return
	}

	// The reader runs in its own goroutine and dies with the track.
	feed := func(push func([]byte)) {
		go func() {
			buf := make([]byte, 1600)
			for {
				n, _, err := track.Read(buf)
				if err != nil {
					s.upstream.Stop(s.memberID)
					return
				}
				pkt := make([]byte, n)
				copy(pkt, buf[:n])
				push(pkt)
			}
		}()
	}

	// Video on the return path is not accepted: it needs v4l2loopback, a host
	// kernel module that cannot be loaded from inside a container.
	if track.Kind() != webrtc.RTPCodecTypeAudio {
		s.logf("incoming %s track ignored: only audio travels upstream", track.Kind())
		return
	}
	if err := s.upstream.StartAudio(s.memberID, feed); err != nil {
		s.logf("could not publish %s: %v", track.Kind(), err)
		s.sendOnChannel(fmt.Sprintf(`{"t":"upstream_error","kind":%q,"error":%q}`,
			track.Kind().String(), err.Error()))
	}
}

// watchClipboard watches the desktop's CLIPBOARD selection and forwards changes
// to the browser, so something copied on the remote side can be pasted locally.
// Deduplicating against lastClip prevents echoing back what the browser
// acaba de enviar.
func (s *Session) watchClipboard(channel *webrtc.DataChannel) {
	if s.clip == nil {
		return
	}
	if text, ok := s.clip.Get(); ok && text != "" {
		s.lastClip = text // do not echo the initial contents back
	}
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		text, ok := s.clip.Get()
		if !ok || text == "" || text == s.lastClip {
			continue
		}
		s.lastClip = text
		payload, err := json.Marshal(map[string]string{"t": "clip", "d": text})
		if err == nil {
			channel.SendText(string(payload))
		}
	}
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.cursors != nil && s.cursorCh != nil {
			s.cursors.Unsubscribe(s.cursorCh)
		}
		// Only release the keys if this session was the one driving. Otherwise
		// it would be yanking the keyboard out from under whoever is working.
		if s.memberID != "" && s.room.IsController(s.memberID) {
			s.injector.ReleaseAll()
		}
		if s.memberID != "" {
			if s.upstream != nil {
				s.upstream.Stop(s.memberID)
			}
			s.room.Leave(s.memberID)
		}
		if s.pc != nil {
			s.pc.Close()
		}
		s.ws.Close()
		s.logf("session closed")
	})
}
