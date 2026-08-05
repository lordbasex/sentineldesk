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

// A room: one capture, shared by everyone watching.
//
// Each client used to raise its own pair of GStreamer pipelines, so two people
// watching meant encoding the same screen twice — double the CPU for the same
// result. Here the pipelines belong to the room: they start when the first
// person arrives, stop when the last one leaves, and every RTP packet is fanned
// out to all the tracks.
//
// That raises the question of who is in charge. The model is a shared console:
// exactly ONE participant holds control at a time and the rest watch. Control is
// asked for and handed over; if the holder disappears it passes to the next.
// Everyone's pointer is broadcast so it is visible what each person is doing —
// which is what turns this into working together rather than taking blind
// turns.

import (
	"encoding/json"
	"fmt"
	"github.com/lordbasex/sentineldesk/internal/config"
	"github.com/lordbasex/sentineldesk/internal/desktop"
	"github.com/lordbasex/sentineldesk/internal/media"
	"log"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// ControlFree is the controller id when nobody is driving. Named rather than a
// bare empty string: "free" is a state the room can be in for as long as it
// likes — everybody watching, nobody at the controls — and code that compares
// against a literal "" reads like it is handling a missing value instead.
const ControlFree = ""

// roomMember is one participant together with their outbound tracks.
type roomMember struct {
	id       string
	name     string
	session  *Session // nil for the agent: it has no WebRTC connection
	agent    bool
	video    *webrtc.TrackLocalStaticRTP
	audio    *webrtc.TrackLocalStaticRTP
	joinedAt time.Time

	// Last known pointer position, so the others can draw it.
	ptrX, ptrY  int
	lastPtrSent time.Time
}

// Room shares one capture among several participants.
type Room struct {
	cfg      config.Config
	strategy media.EncoderStrategy

	mu         sync.RWMutex
	members    map[string]*roomMember
	order      []string // arrival order: decides who inherits control
	controller string
	seq        int

	videoPipe *media.MediaPipeline
	audioPipe *media.MediaPipeline

	// Other people's pointers, drawn onto the X display so that recordings,
	// screenshots and everyone else's stream show them too. Optional: without
	// the SHAPE extension this stays nil and only the browser overlays remain.
	pointers *desktop.PeerPointers

	// A control request from the agent, waiting for somebody to answer it.
	pending   *controlRequest
	requestNo int

	// The agreed bitrate: the minimum of what each network can carry. Encoding
	// for the best link would break the worst one; the other way round only
	// costs some quality.
	bitrates map[string]int
	lastRate int
}

func NewRoom(cfg config.Config, strategy media.EncoderStrategy) *Room {
	pointers, err := desktop.NewPeerPointers(cfg.Display)
	if err != nil {
		log.Printf("room: peer pointers unavailable, browser overlays only: %v", err)
		pointers = nil
	}
	return &Room{
		pointers: pointers,
		cfg:      cfg, strategy: strategy,
		members:  map[string]*roomMember{},
		bitrates: map[string]int{},
		lastRate: cfg.VideoKbps,
	}
}

// Join adds a session together with its tracks. It returns the assigned id and
// whether that participant ends up holding control.
func (r *Room) Join(s *Session, video, audio *webrtc.TrackLocalStaticRTP) (string, bool, error) {
	r.mu.Lock()

	// Evict members whose connection is already dead before counting. Without
	// this, a few tabs closed without a clean goodbye hold their slots for the
	// full keepalive window — up to 90 seconds — and the room reports itself
	// full to someone standing right there trying to get in.
	for id, m := range r.members {
		// The agent has no WebRTC connection to be alive or dead; it leaves
		// when the MCP plane says so.
		if m.session == nil {
			continue
		}
		if !m.session.connectionAlive() {
			log.Printf("room: %s had a dead connection, freeing its slot", id)
			delete(r.members, id)
			delete(r.bitrates, id)
			for i, v := range r.order {
				if v == id {
					r.order = append(r.order[:i], r.order[i+1:]...)
					break
				}
			}
			if r.controller == id {
				r.controller = ControlFree
			}
			if r.pointers != nil {
				r.pointers.Remove(id)
			}
		}
	}
	// Nothing is promoted into the empty seat: free is a state the room is
	// allowed to sit in, for as long as nobody wants the controls.

	if len(r.members) >= r.cfg.MaxViewers {
		r.mu.Unlock()
		return "", false, fmt.Errorf("the session is full (%d of %d viewers)",
			len(r.members), r.cfg.MaxViewers)
	}

	r.seq++
	id := fmt.Sprintf("u%d", r.seq)

	// The visible number is the lowest one free, not the join counter. With a
	// monotonic counter two people in the room end up as "Viewer 1" and
	// "Viewer 20" after a few reconnects, which reads like a leak even though
	// nothing leaked.
	slot := 1
	for {
		taken := false
		for _, m := range r.members {
			if m.name == fmt.Sprintf("Viewer %d", slot) {
				taken = true
				break
			}
		}
		if !taken {
			break
		}
		slot++
	}
	m := &roomMember{
		id: id, name: fmt.Sprintf("Viewer %d", slot), session: s,
		video: video, audio: audio, joinedAt: time.Now(),
	}
	r.members[id] = m
	r.order = append(r.order, id)

	// First in holds control; later arrivals watch until they ask for it.
	//
	// But control must never sit with a dead session. On reload the browser
	// opens the new connection BEFORE the old one's close is detected — the
	// keepalive can take 90 seconds — so without this you end up watching your
	// own ghost: the clicks do nothing and there is no way to tell why.
	//
	// A dead connection does not get to keep the controls — but they go FREE
	// rather than to whoever happened to walk in next. Arriving is not the same
	// as asking, and somebody opening the desktop to watch a colleague work
	// should not find themselves holding it.
	if r.controller != ControlFree && !r.memberAlive(r.controller) {
		log.Printf("room: %s held control on a dead connection; the controls are free",
			r.controller)
		r.controller = ControlFree
	}
	// Whether capture has to start is about the PIPELINE, not the head count.
	// The agent is a member with no video track, so counting members would
	// leave the first real viewer looking at a black screen: with the agent
	// already in the room it is no longer "the first" and capture never began.
	first := r.videoPipe == nil
	isController := r.controller == id
	r.mu.Unlock()

	if first {
		if err := r.startPipelines(); err != nil {
			r.Leave(id)
			return "", false, err
		}
	} else {
		// A newcomer cannot wait for the next keyframe in the GOP: they would
		// stare at garbage or black for seconds.
		r.ForceKeyFrame()
	}
	r.broadcastPresence()
	return id, isController, nil
}

// agentID is fixed: there is one agent plane, and giving it a stable identity
// means humans always see the same name in the participant list instead of a
// new one appearing after every reconnection of the AI host.
const agentID = "agent"

// JoinAgent puts the MCP plane in the room as an ordinary participant.
//
// Before this the agent was invisible: it moved the pointer and typed, and the
// people watching saw a cursor move on its own with no way to tell whether a
// colleague or the model was driving. Being a member gives it a name in the
// list, a marker on screen, and a turn in the control rotation.
//
// It takes no video or audio track — it has no WebRTC connection at all. What it
// shares with a human member is identity and the right to hold control.
func (r *Room) JoinAgent(name string) string {
	r.mu.Lock()
	if _, ok := r.members[agentID]; ok {
		r.mu.Unlock()
		return agentID
	}
	if name == "" {
		name = "AI agent"
	}
	r.members[agentID] = &roomMember{
		id: agentID, name: name, agent: true, joinedAt: time.Now(),
	}
	r.order = append(r.order, agentID)
	// Never made controller on arrival, not even alone in the room. The agent
	// asks for control every time it needs it — request_control grants it at
	// once when nothing is driving, so the cost is one call, and in exchange
	// there is no state in which the agent holds the desktop without having
	// said so.
	r.mu.Unlock()
	log.Printf("room: %s joined (agent)", name)
	r.broadcastPresence()
	return agentID
}

// LeaveAgent takes the agent out of the room.
func (r *Room) LeaveAgent() {
	r.mu.Lock()
	if _, ok := r.members[agentID]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.members, agentID)
	for i, v := range r.order {
		if v == agentID {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	if r.controller == agentID {
		r.controller = ControlFree
	}
	r.mu.Unlock()
	if r.pointers != nil {
		r.pointers.Remove(agentID)
	}
	log.Printf("room: agent left")
	r.broadcastPresence()
}

// HumansPresent reports whether anybody is connected through a browser.
//
// This is what decides whether control arbitration applies to the agent at all:
// with nobody watching there is no one to take turns with, and requiring the
// agent to ask permission from an empty room would break every headless run.
func (r *Room) HumansPresent() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.members {
		if !m.agent && m.session != nil && m.session.connectionAlive() {
			return true
		}
	}
	return false
}

// hasViewers reports whether anybody is actually receiving video. Callers must
// hold the lock.
func (r *Room) hasViewers() bool {
	for _, m := range r.members {
		if m.video != nil {
			return true
		}
	}
	return false
}

// controlRequest is one outstanding "may I drive?" from the agent.
type controlRequest struct {
	id     int
	who    string
	answer chan bool
}

// AskForControl asks the people in the room to hand control to the agent.
//
// Taking it silently is what a human does, and between humans that is right:
// everybody arrived with the same credential and can see each other's names. An
// agent is different — it can act faster than anyone can react, and the person
// watching did not necessarily invite it. So it asks, and waits.
//
// A timeout is a refusal, not an approval. Nobody answering means nobody is
// looking, and that is the worst moment to start moving somebody's mouse.
func (r *Room) AskForControl(timeout time.Duration) (bool, string) {
	r.mu.Lock()
	if _, ok := r.members[agentID]; !ok {
		r.mu.Unlock()
		return false, "the agent is not in the room"
	}
	if r.controller == agentID {
		r.mu.Unlock()
		return true, "you already had control"
	}
	// Nobody is driving: taking it interrupts no one, so there is nothing to
	// ask permission for.
	if r.controller == "" {
		r.mu.Unlock()
		r.TakeControl(agentID)
		return true, "nobody was driving"
	}
	if r.pending != nil {
		r.mu.Unlock()
		return false, "a request is already waiting for an answer"
	}
	r.requestNo++
	req := &controlRequest{id: r.requestNo, who: r.members[agentID].name,
		answer: make(chan bool, 1)}
	r.pending = req
	targets := r.snapshotMembers()
	r.mu.Unlock()

	msg, err := json.Marshal(map[string]any{
		"t": "control_request", "id": req.id, "who": req.who,
		"seconds": int(timeout.Seconds()),
	})
	if err == nil {
		for _, m := range targets {
			if m.session != nil {
				m.session.sendOnChannel(string(msg))
			}
		}
	}

	var granted bool
	var reason string
	select {
	case granted = <-req.answer:
		if granted {
			reason = "a person granted it"
		} else {
			reason = "a person refused"
		}
	case <-time.After(timeout):
		granted, reason = false, "nobody answered in time"
	}

	r.mu.Lock()
	if r.pending == req {
		r.pending = nil
	}
	r.mu.Unlock()

	// Tell the browsers the prompt is over, whichever way it went, so a stale
	// dialog does not sit on somebody's screen.
	if done, err := json.Marshal(map[string]any{
		"t": "control_request_done", "id": req.id, "granted": granted,
	}); err == nil {
		for _, m := range targets {
			if m.session != nil {
				m.session.sendOnChannel(string(done))
			}
		}
	}

	if granted {
		r.TakeControl(agentID)
	}
	return granted, reason
}

// AnswerControlRequest records a person's decision.
func (r *Room) AnswerControlRequest(id int, granted bool) {
	r.mu.Lock()
	req := r.pending
	r.mu.Unlock()
	if req == nil || (id != 0 && req.id != id) {
		return
	}
	select {
	case req.answer <- granted:
	default: // already answered by somebody else; first reply wins
	}
}

// memberAlive reports whether a member can still act. The agent always can:
// it has no connection to lose. Callers must hold the lock.
func (r *Room) memberAlive(id string) bool {
	m, ok := r.members[id]
	if !ok {
		return false
	}
	if m.agent {
		return true
	}
	return m.session != nil && m.session.connectionAlive()
}

// Controller returns the id and name of whoever is driving.
func (r *Room) Controller() (string, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.members[r.controller]; ok {
		return m.id, m.name
	}
	return "", ""
}

// Leave removes a participant, and shuts the capture down if they were last.
func (r *Room) Leave(id string) {
	r.mu.Lock()
	if _, ok := r.members[id]; !ok {
		r.mu.Unlock()
		return
	}
	delete(r.members, id)
	delete(r.bitrates, id)
	if r.pointers != nil {
		r.pointers.Remove(id)
	}
	for i, v := range r.order {
		if v == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	// The controller left, so the controls are free. They used to pass to the
	// longest-present member, which handed the desktop to somebody who never
	// asked for it; FREE is a state anyone can claim when they actually want it.
	if r.controller == id {
		r.controller = ControlFree
	}
	// Likewise on the way out: the agent alone in the room is nobody to encode
	// for, so capture stops when the last member holding a video track leaves.
	empty := !r.hasViewers()
	r.mu.Unlock()

	if empty {
		if r.pointers != nil {
			r.pointers.Clear()
		}
		r.stopPipelines()
		return
	}
	r.broadcastPresence()
}

// --- pipelines ---------------------------------------------------------------

func (r *Room) startPipelines() error {
	var err error
	r.videoPipe, err = media.NewMediaPipeline("video", r.videoDesc(), r.writeVideo)
	if err != nil {
		return err
	}
	r.videoPipe.Strategy = r.strategy
	r.videoPipe.AudioDevice = r.cfg.AudioDevice
	// A destination that fails on its own — a rejected key, a receiver that went
	// away — has to reach the toolbar, or the badge says "live" to nobody.
	r.videoPipe.OnRestreamError = func(id string, err error) {
		r.broadcastRestreams(fmt.Sprintf("%s: %v", id, err))
	}
	if err := r.videoPipe.Start(); err != nil {
		return fmt.Errorf("video pipeline: %w", err)
	}

	r.audioPipe, err = media.NewMediaPipeline("audio", r.audioDesc(), r.writeAudio)
	if err != nil {
		log.Printf("room: audio unavailable: %v", err) // the video carries on
		r.audioPipe = nil
	} else if err := r.audioPipe.Start(); err != nil {
		log.Printf("room: audio unavailable: %v", err)
		r.audioPipe = nil
	}
	log.Printf("room: capture started (encoder %s)", r.strategy.Name)
	return nil
}

func (r *Room) stopPipelines() {
	if r.videoPipe != nil {
		r.videoPipe.Stop()
		r.videoPipe = nil
	}
	if r.audioPipe != nil {
		r.audioPipe.Stop()
		r.audioPipe = nil
	}
	log.Printf("room: empty, capture stopped")
}

// writeVideo fans each RTP packet out to every track.
//
// A write error is deliberately not propagated: if one client is falling over,
// everyone else must keep watching. The broken session cleans itself up through
// its own PeerConnection state change.
func (r *Room) writeVideo(pkt []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.members {
		if m.video != nil {
			m.video.Write(pkt)
		}
	}
}

func (r *Room) writeAudio(pkt []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.members {
		if m.audio != nil {
			m.audio.Write(pkt)
		}
	}
}

func (r *Room) ForceKeyFrame() {
	r.mu.RLock()
	p := r.videoPipe
	r.mu.RUnlock()
	if p != nil {
		p.ForceKeyFrame()
	}
}

// ReportBitrate records one participant's network estimate and applies the
// minimum across everyone to the shared encoder.
func (r *Room) ReportBitrate(id string, kbps int) {
	r.mu.Lock()
	r.bitrates[id] = kbps
	min := 0
	for _, v := range r.bitrates {
		if min == 0 || v < min {
			min = v
		}
	}
	// A floor. The estimator reads a client that cannot DECODE fast enough as a
	// congested network and keeps lowering the target; below this the picture is
	// unusable and dropping further helps nobody — it only guarantees that the
	// client which was struggling now has nothing worth decoding either.
	if floor := r.cfg.MinVideoKbps; floor > 0 && min < floor {
		min = floor
	}
	pipe := r.videoPipe
	last := r.lastRate
	// Hysteresis: a change under 10% is not worth disturbing the encoder for.
	if min == 0 || pipe == nil || abs(min-last)*10 < last {
		r.mu.Unlock()
		return
	}
	r.lastRate = min
	r.mu.Unlock()

	pipe.SetBitrateKbps(min)
	log.Printf("room: bitrate %d kbps (minimum across %d participants)", min, len(r.bitrates))
}

// --- external destinations ----------------------------------------------------

// StartRestream sends this desktop somewhere else as well.
//
// It reuses the encode the room is already producing, so going live costs a mux
// and a socket rather than a second capture. Everyone in the room is told, on
// purpose: a session being broadcast to the internet is not something to find
// out about afterwards.
func (r *Room) StartRestream(t media.RestreamTarget) error {
	r.mu.RLock()
	pipe := r.videoPipe
	r.mu.RUnlock()
	if pipe == nil {
		return fmt.Errorf("nothing is being captured yet; someone has to be watching first")
	}
	if err := pipe.StartRestream(t); err != nil {
		return err
	}
	// The destination expects a picture immediately rather than at the next
	// scheduled keyframe, which on this encoder could be ten seconds away.
	pipe.ForceKeyFrame()
	r.broadcastRestreams("")
	return nil
}

func (r *Room) StopRestream(id string) error {
	r.mu.RLock()
	pipe := r.videoPipe
	r.mu.RUnlock()
	if pipe == nil {
		return fmt.Errorf("nothing is being captured")
	}
	if err := pipe.StopRestream(id); err != nil {
		return err
	}
	r.broadcastRestreams("")
	return nil
}

// Restreams reports the destinations currently attached.
func (r *Room) Restreams() []media.RestreamInfo {
	r.mu.RLock()
	pipe := r.videoPipe
	r.mu.RUnlock()
	return pipe.Restreams()
}

// CanRestream reports whether the running encoder can feed an external
// destination at all. VP8 cannot, and the toolbar should say so before someone
// pastes a stream key.
func (r *Room) CanRestream() bool {
	r.mu.RLock()
	pipe := r.videoPipe
	r.mu.RUnlock()
	return pipe.CanRestream()
}

func (r *Room) broadcastRestreams(problem string) {
	list := r.Restreams()
	r.mu.RLock()
	targets := r.snapshotMembers()
	r.mu.RUnlock()

	msg := map[string]any{"t": "restreams", "list": list}
	if problem != "" {
		msg["error"] = problem
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for _, m := range targets {
		if m.session == nil {
			continue // the agent asks for this through its own tool
		}
		m.session.sendOnChannel(string(payload))
	}
}

// --- control ------------------------------------------------------------------

func (r *Room) IsController(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.controller == id
}

// TakeControl hands control to whoever asks. This is deliberately cooperative:
// there is no hierarchy, because everyone got in with the same credential.
// Whoever was watching sees the change in their toolbar immediately.
func (r *Room) TakeControl(id string) bool {
	r.mu.Lock()
	if _, ok := r.members[id]; !ok {
		r.mu.Unlock()
		return false
	}
	if r.controller == id {
		r.mu.Unlock()
		return true
	}
	previous := r.controller
	r.controller = id
	r.mu.Unlock()
	// The new controller's marker goes away (their pointer is now the real X
	// pointer); the previous one gets a marker as soon as they move.
	if r.pointers != nil {
		r.pointers.Remove(id)
		_ = previous
	}
	r.broadcastPresence()
	return true
}

// ReleaseControl passes it to the next member, or to nobody if alone.
func (r *Room) ReleaseControl(id string) {
	r.mu.Lock()
	if r.controller != id {
		r.mu.Unlock()
		return
	}
	// Released means FREE, not handed on. Passing it to the next member made
	// "I am done with this" indistinguishable from "you are up now", and put
	// the desktop in the hands of somebody who might only have been watching.
	// Whoever wants it next asks for it — including the agent, which is the
	// whole point: it releases when it finishes a task and asks again for the
	// next one, instead of holding the controls between errands.
	r.controller = ControlFree
	r.mu.Unlock()
	r.broadcastPresence()
}

// pointerRate throttles the broadcast of other people's pointers. The client
// sends up to 120 positions a second; relaying all of them to every participant
// floods the
// DataChannel for what is, in the end, an ornament. At 25/s the movement
// already reads as fluid.
const pointerRate = 40 * time.Millisecond

// UpdatePointer records where a participant's pointer is and broadcasts it to
// the others. This is what makes it visible what someone else is pointing at.
func (r *Room) UpdatePointer(id string, x, y int) {
	r.mu.Lock()
	m, ok := r.members[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	m.ptrX, m.ptrY = x, y
	if time.Since(m.lastPtrSent) < pointerRate {
		r.mu.Unlock()
		return
	}
	m.lastPtrSent = time.Now()
	name := m.name
	isController := r.controller == id
	isAgent := m.agent
	// With a single participant there is nobody to tell.
	if len(r.members) < 2 {
		r.mu.Unlock()
		return
	}
	targets := r.snapshotMembers()
	r.mu.Unlock()

	// Draw it on the desktop itself. For people the rule is "only when NOT
	// driving": the controller's pointer already IS the X pointer, so a marker
	// on top would be a duplicate.
	//
	// The agent is the deliberate exception — it gets a marker even while
	// driving. A duplicate is a small price; a pointer moving on its own with
	// nothing to say who is behind it reads as a fault, and that is exactly the
	// moment somebody needs to know a model is at the controls.
	if p := r.pointers; p != nil {
		switch {
		case isAgent:
			p.SetColoured(id, name, x, y, desktop.AgentColour)
		case isController:
			p.Remove(id)
		default:
			p.Set(id, name, x, y)
		}
	}

	msg, err := json.Marshal(map[string]any{
		"t": "peer_cursor", "id": id, "name": name, "x": x, "y": y,
	})
	if err != nil {
		return
	}
	for _, t := range targets {
		if t.id == id || t.session == nil {
			continue
		}
		t.session.sendOnChannel(string(msg))
	}
}

// --- presencia ----------------------------------------------------------------

type MemberInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Controller bool   `json:"controller"`
	Agent      bool   `json:"agent"`
	Seconds    int    `json:"seconds"`
}

func (r *Room) Members() []MemberInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]MemberInfo, 0, len(r.members))
	for _, id := range r.order {
		m := r.members[id]
		if m == nil {
			continue
		}
		out = append(out, MemberInfo{
			ID: m.id, Name: m.name, Controller: r.controller == m.id,
			Agent:   m.agent,
			Seconds: int(time.Since(m.joinedAt).Seconds()),
		})
	}
	return out
}

// snapshotMembers copies the member list under the caller's existing lock.
func (r *Room) snapshotMembers() []*roomMember {
	out := make([]*roomMember, 0, len(r.members))
	for _, m := range r.members {
		out = append(out, m)
	}
	return out
}

// broadcastPresence tells everyone who is present and who holds control.
func (r *Room) broadcastPresence() {
	members := r.Members()
	r.mu.RLock()
	targets := r.snapshotMembers()
	r.mu.RUnlock()

	for _, m := range targets {
		if m.session == nil {
			continue // the agent hears about the room through room_state
		}
		payload, err := json.Marshal(map[string]any{
			"t": "presence", "you": m.id, "members": members,
		})
		if err != nil {
			continue
		}
		m.session.sendOnChannel(string(payload))
	}
}

// --- pipeline descriptions -------------------------------------------------

func (r *Room) videoDesc() string {
	showPointer := "false"
	if r.cfg.RemoteCursor {
		showPointer = "true"
	}
	// Damage tracking on. With it off, ximagesrc re-reads the whole 1080p
	// framebuffer every frame even when not a pixel changed — which on a desktop
	// that is mostly still is the single largest waste in the pipeline. This is
	// the real version of "only send what changed": the codec already sends
	// differences, but the CAPTURE was doing full reads regardless.
	//
	// USE_DAMAGE=0 turns it off for a driver where it misbehaves.
	damage := config.Int("USE_DAMAGE", 1)
	return fmt.Sprintf(
		"ximagesrc display-name=%s use-damage=%d show-pointer=%s "+
			"! video/x-raw,framerate=%d/1 "+
			"! queue max-size-buffers=2 leaky=downstream "+
			"! %s",
		r.cfg.Display, damage, showPointer, r.cfg.FPS, r.strategy.Fragment(r.cfg.VideoKbps, r.cfg.FPS))
}

func (r *Room) audioDesc() string {
	return fmt.Sprintf(
		"pulsesrc device=%s ! audio/x-raw,rate=48000,channels=2 "+
			"! audioconvert ! audioresample "+
			"! opusenc bitrate=%d inband-fec=true "+
			"! rtpopuspay pt=97 "+
			"! application/x-rtp,media=audio,encoding-name=OPUS,payload=97",
		r.cfg.AudioDevice, r.cfg.AudioBitrate)
}
