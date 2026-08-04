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

// Package webui serves the browser client.
//
// The assets are embedded in the binary rather than read from disk: the server
// ships as a single file, and there is no way for the container to end up
// serving a client that disagrees with the protocol the server speaks.
package webui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"strings"
)

//go:embed assets
var embedded embed.FS

// FS returns the client's files rooted at the web directory, so that "/" maps
// to index.html.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "assets")
	if err != nil {
		// Impossible unless the embed directive above is wrong, which would
		// have failed the build.
		panic(err)
	}
	return sub
}

// Handler serves the embedded client with validation caching.
//
// This exists because of a specific, repeatedly confusing failure. Embedded
// files carry no modification time, so http.FileServer sends neither
// Last-Modified nor ETag. With nothing to validate against, browsers fall back
// to heuristic freshness and keep serving a copy of their own choosing — which
// meant a rebuilt image kept showing the OLD interface until somebody thought
// to hard-reload. The bug looked like "the change did not apply".
//
// An ETag over the file's own bytes fixes it at the source: the browser may
// cache as much as it likes, but it has to ask, and the answer changes the
// moment the content does.
func Handler() http.Handler {
	root := FS()
	files := http.FileServer(http.FS(root))
	tags := map[string]string{}

	// Hashed once at startup: the contents are baked into the binary and cannot
	// change while it runs.
	_ = fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		tags["/"+path] = `"` + hex.EncodeToString(sum[:8]) + `"`
		return nil
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		if name == "/" || strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		if tag, ok := tags[name]; ok {
			w.Header().Set("ETag", tag)
			// no-cache means "reuse it, but check first" — not "do not store".
			w.Header().Set("Cache-Control", "no-cache")
			if match := r.Header.Get("If-None-Match"); match == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}

// Language is one translation available to the client.
type Language struct {
	Code string `json:"code"` // file name without .json, e.g. "es"
	Name string `json:"name"` // the language's own name, e.g. "Deutsch"
}

// Languages lists the translations found in assets/lang.
//
// The list is discovered, not declared: dropping another file into assets/lang
// makes that language appear in the picker with no code change anywhere. The
// display name comes from "_name" inside the file itself, so adding a language
// never means updating a table in a second place — which is exactly the kind of
// duplication that goes stale.
func Languages() []Language {
	entries, err := fs.ReadDir(embedded, "assets/lang")
	if err != nil {
		return []Language{{Code: "en", Name: "English"}}
	}

	out := make([]Language, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := fs.ReadFile(embedded, "assets/lang/"+e.Name())
		if err != nil {
			continue // unreadable: skip it rather than offer a broken option
		}
		var doc struct {
			Name string `json:"_name"`
		}
		code := strings.TrimSuffix(e.Name(), ".json")
		name := code
		if json.Unmarshal(data, &doc) == nil && doc.Name != "" {
			name = doc.Name
		}
		out = append(out, Language{Code: code, Name: name})
	}

	// English first, then the rest alphabetically. English is both the default
	// and the fallback, so it belongs at the top of the menu.
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Code == "en") != (out[j].Code == "en") {
			return out[i].Code == "en"
		}
		return out[i].Name < out[j].Name
	})
	return out
}
