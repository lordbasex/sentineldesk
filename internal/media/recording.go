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
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Recorder writes the screen (and audio) to a file using a separate gst-launch
// process, running alongside the WebRTC stream — ximagesrc happily serves more
// than one reader. The -e flag makes gst send EOS on SIGINT so the container is
// finalised properly (the mp4 moov atom, the webm index): without it the file
// queda corrupto.
type Recorder struct {
	display     string
	audioDevice string
	Dir         string

	mu        sync.Mutex
	cmd       *exec.Cmd
	path      string
	container string
	startedAt time.Time
}

func NewRecorder(display, audioDevice, dir string) *Recorder {
	if dir == "" {
		dir = "/home/sentineldesk/Recordings"
	}
	_ = os.MkdirAll(dir, 0o755)
	return &Recorder{display: display, audioDevice: audioDevice, Dir: dir}
}

type RecordOpts struct {
	Container string // mp4 | webm | mkv
	Codec     string // h264 | vp8 | vp9
	FPS       int
	Kbps      int
	Audio     bool
	Path      string // optional; generated inside dir when empty
}

// Start begins recording. It fails if one is already in progress.
func (r *Recorder) Start(o RecordOpts) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd != nil {
		return "", fmt.Errorf("a recording is already in progress: %s", r.path)
	}

	if o.Container == "" {
		o.Container = "mp4"
	}
	if o.FPS <= 0 {
		o.FPS = 30
	}
	if o.Kbps <= 0 {
		o.Kbps = 4000
	}
	path := o.Path
	if path == "" {
		name := "rec-" + time.Now().Format("20060102-150405") + "." + o.Container
		path = filepath.Join(r.Dir, name)
	}

	args, err := r.buildArgs(o, path)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("gst-launch-1.0", args...)
	cmd.Env = append(os.Environ(), "DISPLAY="+r.display)
	// Its own process group, so the signal reaches gst and nothing else.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("gst-launch: %w", err)
	}
	r.cmd = cmd
	r.path = path
	r.container = o.Container
	r.startedAt = time.Now()
	go cmd.Wait() // reap it when it exits
	return path, nil
}

func (r *Recorder) buildArgs(o RecordOpts, path string) ([]string, error) {
	var mux, venc, aenc string
	switch o.Container {
	case "webm":
		mux = "webmmux"
		venc = fmt.Sprintf("vp8enc deadline=1 cpu-used=4 target-bitrate=%d keyframe-max-dist=%d", o.Kbps*1000, o.FPS*2)
		aenc = "opusenc"
	case "mkv":
		mux = "matroskamux"
		venc = fmt.Sprintf("x264enc tune=zerolatency speed-preset=veryfast bitrate=%d key-int-max=%d ! h264parse", o.Kbps, o.FPS*2)
		aenc = "avenc_aac bitrate=128000"
	default: // mp4
		mux = "mp4mux"
		venc = fmt.Sprintf("x264enc tune=zerolatency speed-preset=veryfast bitrate=%d key-int-max=%d ! h264parse", o.Kbps, o.FPS*2)
		aenc = "avenc_aac bitrate=128000"
	}

	// gst-launch takes the pipeline description as separate arguments.
	desc := fmt.Sprintf(
		"-e %s name=mux ! filesink location=%s "+
			"ximagesrc display-name=%s show-pointer=true use-damage=0 "+
			"! video/x-raw,framerate=%d/1 ! videoconvert ! queue ! %s ! mux.",
		mux, path, r.display, o.FPS, venc)
	if o.Audio {
		desc += fmt.Sprintf(
			" pulsesrc device=%s ! audioconvert ! audioresample ! queue ! %s ! mux.",
			r.audioDevice, aenc)
	}
	return SplitArgs(desc), nil
}

// Stop ends the recording cleanly: SIGINT -> EOS -> the file is finalised.
func (r *Recorder) Stop() (string, int64, error) {
	r.mu.Lock()
	cmd := r.cmd
	path := r.path
	r.mu.Unlock()
	if cmd == nil {
		return "", 0, fmt.Errorf("no recording in progress")
	}

	// SIGINT to gst-launch: with -e it emits EOS and writes the container index.
	_ = cmd.Process.Signal(syscall.SIGINT)

	// Wait for it to finish, but not forever.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		_ = cmd.Process.Kill() // did not stop: kill it, and the file may be truncated
	}

	r.mu.Lock()
	r.cmd = nil
	r.path = ""
	r.mu.Unlock()

	var size int64
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	return path, size, nil
}

// Status reports whether a recording is running, and how far along it is.
func (r *Recorder) Status() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cmd == nil {
		return map[string]any{"recording": false}
	}
	var size int64
	if fi, err := os.Stat(r.path); err == nil {
		size = fi.Size()
	}
	return map[string]any{
		"recording":  true,
		"path":       r.path,
		"container":  r.container,
		"seconds":    int(time.Since(r.startedAt).Seconds()),
		"size_bytes": size,
	}
}

// SplitArgs splits a pipeline description into arguments on whitespace (paths and
// element names never contain spaces in the pipelines we build).
func SplitArgs(s string) []string {
	var out []string
	for _, tok := range splitFields(s) {
		out = append(out, tok)
	}
	return out
}

func splitFields(s string) []string {
	var fields []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if cur != "" {
				fields = append(fields, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		fields = append(fields, cur)
	}
	return fields
}
