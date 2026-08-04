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

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lordbasex/sentineldesk/internal/stream"
)

func TestRequireSession(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("docs")) })

	// Auth off: everything through.
	open := stream.NewAuth("", "", "s", time.Hour)
	rec := httptest.NewRecorder()
	requireSession(open, ok).ServeHTTP(rec, httptest.NewRequest("GET", "/docs/", nil))
	if rec.Code != 200 {
		t.Fatalf("auth disabled should pass through, got %d", rec.Code)
	}

	a := stream.NewAuth("u", "p", "secret", time.Hour)

	// A navigation with no cookie lands on the login screen.
	req := httptest.NewRequest("GET", "/docs/", nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	rec = httptest.NewRecorder()
	requireSession(a, ok).ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("want redirect to /, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// A sub-resource with no cookie gets a status it can act on.
	rec = httptest.NewRecorder()
	requireSession(a, ok).ServeHTTP(rec, httptest.NewRequest("GET", "/docs/docs.css", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 for a sub-resource, got %d", rec.Code)
	}

	// A forged cookie is refused.
	req = httptest.NewRequest("GET", "/docs/docs.css", nil)
	req.AddCookie(&http.Cookie{Name: "sentineldesk_session", Value: "not-a-token"})
	rec = httptest.NewRecorder()
	requireSession(a, ok).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged cookie should be refused, got %d", rec.Code)
	}

	// A real token gets in.
	req = httptest.NewRequest("GET", "/docs/", nil)
	req.AddCookie(&http.Cookie{Name: "sentineldesk_session", Value: a.NewToken()})
	rec = httptest.NewRecorder()
	requireSession(a, ok).ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "docs" {
		t.Fatalf("valid token should pass, got %d %q", rec.Code, rec.Body.String())
	}
}
