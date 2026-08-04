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

import (
	"fmt"
	"log"
	"sync"

	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
)

// MediaPipeline is a GStreamer pipeline whose RTP leaves through an appsink into
// a callback, which pushes it onto a Pion track. Unlike gst-launch, this keeps
// the encoder reachable at runtime: bitrate changes, keyframes on demand in
// answer to a PLI, and destinations attached and detached while it runs.
type MediaPipeline struct {
	pipeline *gst.Pipeline
	encoder  *gst.Element // nil on audio
	tee      *gst.Element // nil unless the encoder's output can be re-streamed
	src      *app.Source  // inbound pipelines only (the microphone)
	Strategy EncoderStrategy

	// AudioDevice is the PulseAudio source a re-stream should read, set by
	// whoever built the pipeline.
	AudioDevice string

	// OnRestreamError reports a destination that dropped out on its own —
	// a rejected key, a host that went away — so the people in the room find
	// out instead of watching a "live" badge that means nothing.
	OnRestreamError func(id string, err error)

	rsMu      sync.Mutex
	restreams map[string]*restream
	kfEvery   int // forced-keyframe interval in seconds; 0 = on demand only
	kfStop    chan struct{}
}

// NewMediaPipeline builds `desc ! appsink name=rtpsink` and delivers every
// RTP packet to the callback. The callback gets its own copy, because Pion's
// interceptors hold on to packets for NACK retransmission and the GStreamer
// buffer is unmapped as soon as we return.
func NewMediaPipeline(kind, desc string, onPacket func([]byte)) (*MediaPipeline, error) {
	full := desc + " ! appsink name=rtpsink emit-signals=false sync=false max-buffers=64 drop=true"
	pipeline, err := gst.NewPipelineFromString(full)
	if err != nil {
		return nil, fmt.Errorf("pipeline %s: %w", kind, err)
	}

	elem, err := pipeline.GetElementByName("rtpsink")
	if err != nil {
		return nil, fmt.Errorf("appsink %s: %w", kind, err)
	}
	sink := app.SinkFromElement(elem)
	sink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(s *app.Sink) gst.FlowReturn {
			sample := s.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}
			buffer := sample.GetBuffer()
			if buffer == nil {
				return gst.FlowOK
			}
			mapInfo := buffer.Map(gst.MapRead)
			if mapInfo == nil {
				return gst.FlowOK
			}
			packet := make([]byte, len(mapInfo.Bytes()))
			copy(packet, mapInfo.Bytes())
			buffer.Unmap()
			onPacket(packet)
			return gst.FlowOK
		},
	})

	mp := &MediaPipeline{pipeline: pipeline, restreams: map[string]*restream{}}
	if enc, err := pipeline.GetElementByName("venc"); err == nil {
		mp.encoder = enc
	}
	// Present only on the encoders whose output an external destination can
	// take. Its absence is what CanRestream reads.
	if tee, err := pipeline.GetElementByName("vtee"); err == nil {
		mp.tee = tee
	}
	return mp, nil
}

// NewAppSrcPipeline is the other direction: `appsrc ! …` fed from Go with the
// RTP packets arriving from the browser. The client's microphone and camera use
// it — they come in over WebRTC and go out to PulseAudio or v4l2.
func NewAppSrcPipeline(kind, desc string) (*MediaPipeline, error) {
	pipeline, err := gst.NewPipelineFromString(desc)
	if err != nil {
		return nil, fmt.Errorf("pipeline %s: %w", kind, err)
	}
	elem, err := pipeline.GetElementByName("src")
	if err != nil {
		return nil, fmt.Errorf("appsrc %s: %w", kind, err)
	}
	return &MediaPipeline{
		pipeline:  pipeline,
		src:       app.SrcFromElement(elem),
		restreams: map[string]*restream{},
	}, nil
}

// Push injects an RTP packet into the appsrc. On a capture pipeline there is no
// appsrc and this does nothing, so it is safe to call unconditionally.
func (mp *MediaPipeline) Push(pkt []byte) {
	if mp.src == nil {
		return
	}
	buf := gst.NewBufferFromBytes(pkt)
	if buf == nil {
		return
	}
	mp.src.PushBuffer(buf)
}

func (mp *MediaPipeline) Start() error {
	return mp.pipeline.SetState(gst.StatePlaying)
}

func (mp *MediaPipeline) Stop() {
	// Destinations first, so each one gets its EOS and closes properly instead
	// of being cut off with the rest of the pipeline.
	mp.StopAllRestreams()
	mp.pipeline.SetState(gst.StateNull)
}

// ForceKeyFrame asks the encoder for an immediate keyframe, which is how a PLI
// from the client gets answered without waiting for the next GOP.
func (mp *MediaPipeline) ForceKeyFrame() {
	if mp.encoder == nil {
		return
	}
	structure := gst.NewStructure("GstForceKeyUnit")
	structure.SetValue("all-headers", true)
	mp.encoder.SendEvent(gst.NewCustomEvent(gst.EventTypeCustomUpstream, structure))
}

// SetBitrateKbps adjusts the encoder's bitrate at runtime, driven by congestion
// control. Each encoder spells the property in its own unit, hence the strategy
// carrying both the name and whether it wants bits or kilobits.
func (mp *MediaPipeline) SetBitrateKbps(kbps int) {
	if mp.encoder == nil || kbps <= 0 {
		return
	}
	var err error
	if mp.Strategy.BitrateBPS {
		err = mp.encoder.SetProperty(mp.Strategy.BitrateProp, kbps*1000)
	} else {
		err = mp.encoder.SetProperty(mp.Strategy.BitrateProp, uint(kbps))
	}
	if err != nil {
		log.Printf("could not set the bitrate to %d kbps: %v", kbps, err)
	}
}
