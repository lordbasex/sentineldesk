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

package media

// Sending the desktop somewhere else while people are watching it.
//
// The obvious way to stream to YouTube is to open a second capture: another
// ximagesrc, another encoder, another copy of the same screen. It works, and it
// costs a whole CPU core to produce a picture we already have.
//
// So instead the live pipeline carries a tee right after the encoder, and a
// destination is a BRANCH off it: the encoded frames that WebRTC is already
// sending to everyone in the room are muxed a second time and pushed to the
// destination. One capture, one encode, as many destinations as the uplink can
// carry — an extra branch is a mux and a socket, a couple of percent of a core.
//
// Two details make this safe to attach to a session people are using:
//
//   - Each branch starts with a leaky queue. If the destination stalls — a
//     hotel wifi, YouTube having a bad minute — the branch drops frames instead
//     of applying backpressure through the tee and freezing the desktop for
//     everybody watching. The stream to the platform suffers; the session does
//     not.
//
//   - Branches are added and removed on a running pipeline. Starting a stream
//     does not interrupt the picture, which is the entire point: you decide to
//     go live in the middle of what you were already doing.

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tinyzimmer/go-gst/gst"
)

// RestreamTarget is one destination as the caller describes it.
type RestreamTarget struct {
	ID       string // caller's handle, used to stop this one destination
	Platform string // youtube | twitch | facebook | custom — labelling only
	URL      string
	Audio    bool

	// KeyframeSec is how often this destination needs a keyframe.
	//
	// It is a property of the destination, not of the encoder, because it
	// answers a question about the audience: can a viewer arrive at an arbitrary
	// moment? YouTube, Twitch and Facebook serve people who click play whenever
	// they like, and a viewer cannot see anything until the next keyframe, so
	// they mandate one every two seconds. A single VLC or OBS that you start
	// yourself can simply wait, so it needs nothing and gets a sharper picture
	// for it — keyframes are ten to fifty times the size of a normal frame, and
	// every bit they take is a bit not spent on the text on screen.
	//
	// 0 means "ask for nothing": keyframes keep happening only when a browser
	// requests one through PLI, which is how the session behaves with no
	// destination attached at all.
	KeyframeSec int
}

// RestreamInfo is a running destination, for reporting.
type RestreamInfo struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	URL      string `json:"url"`
	Audio    bool   `json:"audio"`
	Seconds  int    `json:"seconds"`
}

type restream struct {
	target  RestreamTarget
	bin     *gst.Bin
	teePad  *gst.Pad
	binSink *gst.Pad
	started time.Time
}

// destSpec is how one URL scheme wants to be muxed and written.
//
// Neither fragment carries its own name=: the unique per-destination name is
// added where the branch is assembled. Setting it twice parses without
// complaint but leaves gst_parse indexing the element under the first name it
// saw, so a later "rsmux_x." reference silently finds nothing.
type destSpec struct {
	mux   string
	sink  string
	vcaps string
	acaps string
	// apply sets the address on the sink after the branch is built, so a stream
	// key never has to survive gst-launch quoting rules.
	apply func(*gst.Element) error
}

// parseDest works out what to build from the destination URL.
//
// The three schemes cover the two audiences separately. RTMP is what the
// platforms accept and nothing else; it is elderly and awkward but not
// optional. SRT and plain UDP are for a receiver you run yourself — VLC and OBS
// take either, with less latency and no handshake to get wrong.
func parseDest(raw string) (*destSpec, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("that destination is not a URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "rtmp", "rtmps":
		// FLV wants H.264 in AVC form and raw AAC. h264parse and aacparse
		// convert; the encoder does not have to know where its output is going.
		return &destSpec{
			mux:   "flvmux streamable=true",
			sink:  "rtmpsink sync=false async=false",
			vcaps: "video/x-h264,stream-format=avc,alignment=au",
			acaps: "audio/mpeg,mpegversion=4,stream-format=raw",
			apply: func(e *gst.Element) error { return e.SetProperty("location", raw) },
		}, nil

	case "srt":
		return &destSpec{
			mux:   "mpegtsmux",
			sink:  "srtsink sync=false async=false wait-for-connection=false",
			vcaps: "video/x-h264,stream-format=byte-stream,alignment=au",
			acaps: "audio/mpeg,mpegversion=4,stream-format=adts",
			apply: func(e *gst.Element) error { return e.SetProperty("uri", raw) },
		}, nil

	case "udp":
		host, portStr, err := net.SplitHostPort(u.Host)
		if err != nil {
			return nil, fmt.Errorf("udp needs host:port, e.g. udp://192.168.1.20:5000: %w", err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("%q is not a port", portStr)
		}
		return &destSpec{
			mux:   "mpegtsmux",
			sink:  "udpsink sync=false async=false",
			vcaps: "video/x-h264,stream-format=byte-stream,alignment=au",
			acaps: "audio/mpeg,mpegversion=4,stream-format=adts",
			apply: func(e *gst.Element) error {
				if err := e.SetProperty("host", host); err != nil {
					return err
				}
				return e.SetProperty("port", port)
			},
		}, nil
	}
	return nil, fmt.Errorf("unsupported destination %q: use rtmp://, rtmps://, srt:// or udp://", u.Scheme)
}

// CanRestream reports whether this pipeline can feed an external destination.
//
// It cannot when the encoder is VP8: every container the platforms and players
// accept here carries H.264, and re-encoding to get it would reintroduce the
// second encode this whole design exists to avoid. Setting ENCODER=x264 (or
// leaving it on auto, which prefers x264 over VP8) is the answer.
func (mp *MediaPipeline) CanRestream() bool {
	return mp != nil && mp.tee != nil
}

// StartRestream attaches a destination to the running pipeline.
//
// It returns as soon as the branch is playing. Whether the far end actually
// accepted the connection is not known yet — a wrong stream key fails a second
// or two later — so failures arrive through OnRestreamError instead of here.
func (mp *MediaPipeline) StartRestream(t RestreamTarget) error {
	if !mp.CanRestream() {
		return fmt.Errorf("this session is encoding VP8; external streaming needs H.264 (set ENCODER=x264)")
	}
	if t.ID == "" {
		return fmt.Errorf("a destination needs an id")
	}
	spec, err := parseDest(t.URL)
	if err != nil {
		return err
	}

	mp.rsMu.Lock()
	defer mp.rsMu.Unlock()
	if _, exists := mp.restreams[t.ID]; exists {
		return fmt.Errorf("already streaming to %s", t.ID)
	}

	// Every element in the branch carries the destination's name. Bus messages
	// identify their source by element name alone, so without this a failure on
	// one destination would be indistinguishable from a failure on another and
	// could tear down the wrong stream.
	tag := elementTag(t.ID)
	desc := fmt.Sprintf(
		// A deep leaky queue is what keeps a bad uplink from becoming everyone
		// else's problem: buffers pile up here and are dropped here, and the tee
		// upstream never blocks.
		"queue name=rsvq_%s max-size-buffers=0 max-size-bytes=0 max-size-time=2000000000 leaky=downstream "+
			"! h264parse config-interval=-1 ! %s "+
			"! %s name=rsmux_%s "+
			"! queue max-size-time=3000000000 "+
			"! %s name=rssink_%s",
		tag, spec.vcaps, spec.mux, tag, spec.sink, tag)
	if t.Audio {
		// A second read of the same monitor rather than a tee off the Opus
		// branch, because the platforms want AAC and nothing on the way to Opus
		// is reusable. Encoding AAC costs about one percent of a core, so the
		// duplication that matters is the video one, and that is the one the tee
		// removes.
		desc += fmt.Sprintf(
			" pulsesrc name=rsasrc_%s ! audio/x-raw,rate=48000,channels=2 "+
				"! audioconvert ! audioresample "+
				"! queue max-size-time=2000000000 leaky=downstream "+
				"! avenc_aac bitrate=128000 ! aacparse ! %s ! rsmux_%s.",
			tag, spec.acaps, tag)
	}

	bin, err := gst.NewBinFromString(desc, true)
	if err != nil {
		return fmt.Errorf("could not build the branch for %s: %w", t.URL, err)
	}

	sinkElem, err := bin.GetElementByName("rssink_" + tag)
	if err != nil {
		return fmt.Errorf("branch has no sink: %w", err)
	}
	if err := spec.apply(sinkElem); err != nil {
		return fmt.Errorf("could not set the destination: %w", err)
	}
	if t.Audio && mp.AudioDevice != "" {
		if src, err := bin.GetElementByName("rsasrc_" + tag); err == nil {
			_ = src.SetProperty("device", mp.AudioDevice)
		}
	}

	binSink := binSinkPad(bin)
	if binSink == nil {
		return fmt.Errorf("branch has no input pad")
	}

	if err := mp.pipeline.Add(bin.Element); err != nil {
		return fmt.Errorf("could not attach the branch: %w", err)
	}
	teePad := mp.tee.GetRequestPad("src_%u")
	if teePad == nil {
		_ = mp.pipeline.Remove(bin.Element)
		return fmt.Errorf("the tee would not give out a pad")
	}
	if ret := teePad.Link(binSink); ret != gst.PadLinkOK {
		mp.tee.ReleaseRequestPad(teePad)
		_ = mp.pipeline.Remove(bin.Element)
		return fmt.Errorf("could not link the branch to the encoder (%s)", ret)
	}
	if !bin.SyncStateWithParent() {
		mp.detach(&restream{bin: bin, teePad: teePad, binSink: binSink})
		return fmt.Errorf("the branch would not start")
	}

	mp.restreams[t.ID] = &restream{
		target: t, bin: bin, teePad: teePad, binSink: binSink,
		started: time.Now(),
	}
	mp.retuneKeyframes()
	go mp.watchBranch(t.ID)
	log.Printf("restream: %s → %s (audio=%v, keyframes every %ds)",
		t.ID, redact(t.URL), t.Audio, t.KeyframeSec)
	return nil
}

// StopRestream detaches one destination and leaves everything else running.
func (mp *MediaPipeline) StopRestream(id string) error {
	mp.rsMu.Lock()
	rs, ok := mp.restreams[id]
	if !ok {
		mp.rsMu.Unlock()
		return fmt.Errorf("not streaming to %s", id)
	}
	delete(mp.restreams, id)
	mp.retuneKeyframes()
	mp.rsMu.Unlock()

	mp.detach(rs)
	log.Printf("restream: %s stopped", id)
	return nil
}

// StopAllRestreams is what the room calls when the last person leaves.
func (mp *MediaPipeline) StopAllRestreams() {
	mp.rsMu.Lock()
	all := make([]*restream, 0, len(mp.restreams))
	for id, rs := range mp.restreams {
		all = append(all, rs)
		delete(mp.restreams, id)
	}
	mp.retuneKeyframes()
	mp.rsMu.Unlock()

	for _, rs := range all {
		mp.detach(rs)
	}
}

// Restreams lists what is running, newest information first-hand rather than
// remembered, so the toolbar and the agent see the same thing.
func (mp *MediaPipeline) Restreams() []RestreamInfo {
	if mp == nil {
		return nil
	}
	mp.rsMu.Lock()
	defer mp.rsMu.Unlock()
	out := make([]RestreamInfo, 0, len(mp.restreams))
	for _, rs := range mp.restreams {
		out = append(out, RestreamInfo{
			ID: rs.target.ID, Platform: rs.target.Platform,
			URL: redact(rs.target.URL), Audio: rs.target.Audio,
			Seconds: int(time.Since(rs.started).Seconds()),
		})
	}
	return out
}

// detach takes a branch off the pipeline.
//
// The order matters. Unlinking first stops the tee pushing into a branch that
// is about to disappear; the EOS afterwards is what lets flvmux finish the file
// and rtmpsink say goodbye, so the platform sees the stream end rather than
// time out.
func (mp *MediaPipeline) detach(rs *restream) {
	if rs.teePad != nil && rs.binSink != nil {
		rs.teePad.Unlink(rs.binSink)
	}
	if rs.binSink != nil {
		rs.binSink.SendEvent(gst.NewEOSEvent())
		time.Sleep(300 * time.Millisecond)
	}
	if rs.bin != nil {
		_ = rs.bin.SetState(gst.StateNull)
		_ = mp.pipeline.Remove(rs.bin.Element)
	}
	if rs.teePad != nil {
		mp.tee.ReleaseRequestPad(rs.teePad)
	}
}

// watchBranch reports a destination that refuses the stream.
//
// A wrong key or an unreachable host does not fail at link time — it fails on
// the network a moment later, as a bus error from inside the branch. Without
// this the toolbar would happily show "streaming" to nowhere.
func (mp *MediaPipeline) watchBranch(id string) {
	suffix := "_" + elementTag(id)
	bus := mp.pipeline.GetPipelineBus()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		msg := bus.TimedPopFiltered(500*time.Millisecond, gst.MessageError)
		if msg == nil {
			continue
		}
		gerr := msg.ParseError()
		if gerr == nil {
			continue
		}
		// Only this destination's own elements. Anything else on this bus is
		// either the capture or another destination, and neither is ours to act
		// on — stopping the wrong stream on somebody else's error would be worse
		// than missing the report.
		if !strings.HasSuffix(msg.Source(), suffix) {
			continue
		}
		err := fmt.Errorf("%s", gerr.Message())
		log.Printf("restream: %s failed: %v", id, err)
		_ = mp.StopRestream(id)
		if mp.OnRestreamError != nil {
			mp.OnRestreamError(id, err)
		}
		return
	}
}

// retuneKeyframes sets the forced-keyframe cadence to whatever the most
// demanding destination needs, and switches it off entirely when none of them
// need anything.
//
// This is the whole reason destinations declare an interval instead of the
// encoder being configured for the worst case: with nobody watching from a
// platform, the desktop goes back to spending its bitrate on detail.
//
// Called with rsMu held.
func (mp *MediaPipeline) retuneKeyframes() {
	want := 0
	for _, rs := range mp.restreams {
		if s := rs.target.KeyframeSec; s > 0 && (want == 0 || s < want) {
			want = s
		}
	}
	if want == mp.kfEvery {
		return
	}
	if mp.kfStop != nil {
		close(mp.kfStop)
		mp.kfStop = nil
	}
	mp.kfEvery = want
	if want <= 0 {
		log.Printf("restream: keyframes back on demand (PLI only)")
		return
	}
	stop := make(chan struct{})
	mp.kfStop = stop
	go func(every time.Duration) {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				mp.ForceKeyFrame()
			}
		}
	}(time.Duration(want) * time.Second)
	log.Printf("restream: forcing a keyframe every %ds while streaming", want)
}

// elementTag turns a destination id into something usable as an element name.
// GStreamer's parser takes a limited alphabet, and the id comes from whatever a
// person typed into the toolbar.
func elementTag(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "dest"
	}
	return b.String()
}

// binSinkPad finds the branch's input.
//
// gst_parse_bin_from_description ghosts whatever it leaves unlinked, and the
// only unlinked pad in a branch is the queue's input, but it names the ghost
// after the pad it wraps — so look it up by direction rather than trusting the
// name.
func binSinkPad(bin *gst.Bin) *gst.Pad {
	if p := bin.GetStaticPad("sink"); p != nil {
		return p
	}
	pads, err := bin.GetPads()
	if err != nil {
		return nil
	}
	for _, p := range pads {
		if p.Direction() == gst.PadDirectionSink {
			return p
		}
	}
	return nil
}

// redact hides the stream key.
//
// The last path segment of an RTMP URL IS the credential: anyone holding it can
// broadcast to that channel. It goes through logs and over the wire to every
// browser in the room, so it does not travel whole.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return raw
	}
	i := strings.LastIndex(u.Path, "/")
	if i < 0 || i == len(u.Path)-1 {
		return raw
	}
	key := u.Path[i+1:]
	if len(key) <= 4 {
		u.Path = u.Path[:i+1] + "•••"
	} else {
		u.Path = u.Path[:i+1] + key[:2] + "•••" + key[len(key)-2:]
	}
	return u.String()
}
