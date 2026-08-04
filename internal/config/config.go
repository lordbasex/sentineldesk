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

// Package config holds every setting the server reads from the environment.
//
// Configuration is deliberately environment-only: the container is the unit of
// deployment, and a second configuration file would just be another thing that
// can disagree with docker-compose.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved configuration for one server process.
type Config struct {
	HTTPPort     int
	Display      string
	FPS          int
	Encoder      string // auto | nvenc | vaapi | h264 | vp8
	MinVideoKbps int
	VideoKbps    int
	AudioBitrate int
	AudioDevice  string
	RemoteCursor bool
	StunServer   string
	MinPort      uint16
	MaxPort      uint16
	NAT1To1IP    string

	ClientStun    string
	ClientTurnURL []string
	TurnUser      string
	TurnPass      string

	AuthUser   string
	AuthPass   string
	AuthSecret string
	AuthTTL    time.Duration

	TLSCert       string
	TLSKey        string
	TLSSelfSigned bool
	TLSDir        string
	TLSHosts      string

	// FilesRoot bounds what the browser's file manager can reach. The home
	// directory is the sensible default: it is where people keep the things
	// they want to download. FILES_ROOT=/ opens up the whole container.
	FilesRoot string

	// MaxViewers caps how many people can share one desktop at a time.
	MaxViewers int
}

// Load reads the environment and fills in defaults.
func Load() Config {
	cfg := Config{
		HTTPPort:  Int("HTTP_PORT", 8080),
		Display:   Str("DISPLAY", ":0"),
		FPS:       Int("FPS", 30),
		Encoder:   strings.ToLower(Str("ENCODER", "auto")),
		VideoKbps: Int("VIDEO_BITRATE_KBPS", 4000),
		// The floor the shared encoder never goes under, however bad one
		// participant's estimate gets.
		MinVideoKbps:  Int("MIN_VIDEO_BITRATE_KBPS", 1200),
		AudioBitrate:  Int("AUDIO_BITRATE", 96000),
		AudioDevice:   Str("AUDIO_DEVICE", "sentineldesk.monitor"),
		RemoteCursor:  Bool("REMOTE_CURSOR", false),
		StunServer:    Str("STUN_SERVER", "stun:stun.l.google.com:19302"),
		MinPort:       uint16(Int("WEBRTC_MIN_PORT", 0)),
		MaxPort:       uint16(Int("WEBRTC_MAX_PORT", 0)),
		NAT1To1IP:     Str("NAT1TO1_IP", ""),
		ClientStun:    Str("CLIENT_STUN", "stun:stun.l.google.com:19302"),
		TurnUser:      Str("TURN_USER", ""),
		TurnPass:      Str("TURN_PASS", ""),
		AuthUser:      Str("AUTH_USER", ""),
		AuthPass:      Str("AUTH_PASS", ""),
		AuthSecret:    Str("AUTH_SECRET", ""),
		AuthTTL:       time.Duration(Int("AUTH_TTL_HOURS", 12)) * time.Hour,
		TLSCert:       Str("TLS_CERT", ""),
		TLSKey:        Str("TLS_KEY", ""),
		TLSSelfSigned: Bool("TLS_SELFSIGNED", false),
		TLSDir:        Str("TLS_DIR", "/home/sentineldesk/.tls"),
		TLSHosts:      Str("TLS_HOSTS", ""),
		FilesRoot:     Str("FILES_ROOT", "/home/sentineldesk"),
		MaxViewers:    Int("MAX_VIEWERS", 4),
	}
	if urls := Str("CLIENT_TURN_URLS", ""); urls != "" {
		for _, u := range strings.Split(urls, ",") {
			if u = strings.TrimSpace(u); u != "" {
				cfg.ClientTurnURL = append(cfg.ClientTurnURL, u)
			}
		}
	}
	if cfg.MaxViewers < 1 {
		cfg.MaxViewers = 1
	}
	return cfg
}

// Str returns an environment variable, or def when it is unset or empty.
func Str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Int returns an environment variable parsed as an integer, or def.
func Int(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Bool reads the usual spellings of yes and no. Unrecognised values fall back
// to def rather than silently meaning false.
func Bool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "yes":
		return true
	case "0", "false", "no":
		return false
	}
	return def
}
