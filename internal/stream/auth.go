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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// Auth validates credentials and issues HMAC-signed session tokens.
//
// Authentication happens over the WebSocket: the first message must be a hello
// carrying either a username and password or a previously issued token. Until it
// validates there is no WebRTC handshake at all. With AUTH_USER/AUTH_PASS unset
// it is
// deshabilitado (modo desarrollo).
type Auth struct {
	user    string
	pass    string
	secret  []byte
	ttl     time.Duration
	enabled bool
}

func NewAuth(user, pass, secret string, ttl time.Duration) *Auth {
	a := &Auth{user: user, pass: pass, ttl: ttl, enabled: user != "" && pass != ""}
	if secret != "" {
		a.secret = []byte(secret)
	} else {
		a.secret = make([]byte, 32)
		if _, err := rand.Read(a.secret); err != nil {
			log.Fatalf("could not generate the session secret: %v", err)
		}
	}
	if !a.enabled {
		log.Printf("WARNING: no authentication (set AUTH_USER and AUTH_PASS before exposing this)")
	}
	return a
}

// Enabled reports whether authentication is switched on.
func (a *Auth) Enabled() bool { return a.enabled }

// ValidCredentials compares username and password in constant time, so the
// duration of a failure never reveals how much of the guess was right.
func (a *Auth) ValidCredentials(user, pass string) bool {
	if !a.enabled {
		return false
	}
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(a.user)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(a.pass)) == 1
	return userOK && passOK
}

func (a *Auth) sign(payload string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// NewToken issues a signed session token, so a reload or a network blip can
// reconnect without asking for the password again.
func (a *Auth) NewToken() string {
	if !a.enabled {
		return ""
	}
	exp := time.Now().Add(a.ttl).Unix()
	payload := fmt.Sprintf("%s|%d", a.user, exp)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + a.sign(payload)
}

// ValidToken checks a session token's signature and expiry.
func (a *Auth) ValidToken(token string) bool {
	if !a.enabled || token == "" {
		return false
	}
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload := string(raw)
	if subtle.ConstantTimeCompare([]byte(a.sign(payload)), []byte(parts[1])) != 1 {
		return false
	}
	fields := strings.SplitN(payload, "|", 2)
	if len(fields) != 2 || fields[0] != a.user {
		return false
	}
	exp, err := strconv.ParseInt(fields[1], 10, 64)
	return err == nil && time.Now().Unix() < exp
}
