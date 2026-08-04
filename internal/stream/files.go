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

// The browser's file manager: download from the desktop to the machine of
// whoever is watching, and upload the other way.
//
// Until this existed files only moved over SFTP through the ssh_* tools, which
// serves the agent but not a person. This exposes the same work over HTTP,
// behind the same authentication as the WebSocket.
//
// On tokens in URLs: a download is triggered by the browser navigating, and a
// navigation cannot carry headers. Rather than putting the session token in the
// query string — where it lands in logs, history and referers — the client asks
// for a single-use TICKET bound to one path and valid for 60 seconds. If such a
// ticket leaks it has already been spent or expired, and it only ever unlocked
// that one file.

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	ticketTTL     = 60 * time.Second
	maxUploadSize = 8 << 30 // 8 GiB per request
)

// FileServer serves the desktop's file tree beneath a fixed root.
type FileServer struct {
	root string
	auth *Auth

	mu      sync.Mutex
	tickets map[string]fileTicket
}

type fileTicket struct {
	path    string
	expires time.Time
}

func NewFileServer(root string, auth *Auth) *FileServer {
	abs, err := filepath.Abs(root)
	if err != nil || abs == "" {
		abs = "/home/sentineldesk"
	}
	return &FileServer{root: filepath.Clean(abs), auth: auth, tickets: map[string]fileTicket{}}
}

// resolve turns a client path into a real one, confined to the root.
//
// Symlinks are resolved BEFORE the comparison: without that, a link inside the
// home pointing at / would be a back door straight out of the confinement.
func (f *FileServer) resolve(p string) (string, error) {
	if p == "" {
		p = "/"
	}
	// The client path is always relative to the root, even when it starts "/".
	joined := filepath.Join(f.root, filepath.Clean("/"+strings.TrimPrefix(p, f.root)))
	real, err := filepath.EvalSymlinks(joined)
	if err != nil {
		// It does not exist yet — an upload, a mkdir. Validate the parent instead.
		parent, err2 := filepath.EvalSymlinks(filepath.Dir(joined))
		if err2 != nil {
			return "", fmt.Errorf("ruta inexistente: %s", p)
		}
		if !withinRoot(parent, f.root) {
			return "", fmt.Errorf("outside the permitted root")
		}
		return joined, nil
	}
	if !withinRoot(real, f.root) {
		return "", fmt.Errorf("outside the permitted root")
	}
	return real, nil
}

func withinRoot(p, root string) bool {
	if p == root {
		return true
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}

// rel returns the path as the client sees it: relative to the root, "/"-rooted.
func (f *FileServer) rel(abs string) string {
	r, err := filepath.Rel(f.root, abs)
	if err != nil || r == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(r)
}

// --- authentication ----------------------------------------------------------

// authorized validates the session token the client sends in a header. With
// authentication disabled (development mode) everything is allowed through.
func (f *FileServer) authorized(r *http.Request) bool {
	if !f.auth.Enabled() {
		return true
	}
	return f.auth.ValidToken(r.Header.Get("X-SentinelDesk-Token"))
}

func (f *FileServer) newTicket(path string) string {
	b := make([]byte, 24)
	rand.Read(b)
	tk := hex.EncodeToString(b)
	f.mu.Lock()
	defer f.mu.Unlock()
	// Sweep the expired ones so the map cannot grow without bound.
	now := time.Now()
	for k, v := range f.tickets {
		if now.After(v.expires) {
			delete(f.tickets, k)
		}
	}
	f.tickets[tk] = fileTicket{path: path, expires: now.Add(ticketTTL)}
	return tk
}

// claimTicket redeems a ticket: it returns the path and invalidates it.
func (f *FileServer) claimTicket(tk string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tickets[tk]
	if !ok {
		return "", false
	}
	delete(f.tickets, tk)
	if time.Now().After(t.expires) {
		return "", false
	}
	return t.path, true
}

// --- rutas -------------------------------------------------------------------

func (f *FileServer) Register(mux *http.ServeMux) {
	mux.HandleFunc("/files/list", f.handleList)
	mux.HandleFunc("/files/ticket", f.handleTicket)
	mux.HandleFunc("/files/download", f.handleDownload)
	mux.HandleFunc("/files/upload", f.handleUpload)
	mux.HandleFunc("/files/op", f.handleOp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, format string, args ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf(format, args...)})
}

type fileEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // dir | file | link
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Mode     string `json:"mode"`
}

func (f *FileServer) handleList(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(r) {
		writeErr(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	abs, err := f.resolve(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusForbidden, "%v", err)
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeErr(w, http.StatusNotFound, "could not list: %v", err)
		return
	}
	items := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		kind := "file"
		switch {
		case e.IsDir():
			kind = "dir"
		case info.Mode()&os.ModeSymlink != 0:
			kind = "link"
			// A link to a directory should be browsable as a directory.
			if st, err := os.Stat(filepath.Join(abs, e.Name())); err == nil && st.IsDir() {
				kind = "dir"
			}
		}
		items = append(items, fileEntry{
			Name: e.Name(), Type: kind, Size: info.Size(),
			Modified: info.ModTime().Format(time.RFC3339),
			Mode:     info.Mode().Perm().String(),
		})
	}
	// Directories first, then by name — the order Midnight Commander uses.
	sort.Slice(items, func(i, j int) bool {
		if (items[i].Type == "dir") != (items[j].Type == "dir") {
			return items[i].Type == "dir"
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	parent := ""
	if abs != f.root {
		parent = f.rel(filepath.Dir(abs))
	}
	writeJSON(w, map[string]any{
		"path": f.rel(abs), "parent": parent, "root": f.root, "entries": items,
	})
}

func (f *FileServer) handleTicket(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(r) {
		writeErr(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	abs, err := f.resolve(body.Path)
	if err != nil {
		writeErr(w, http.StatusForbidden, "%v", err)
		return
	}
	if _, err := os.Stat(abs); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, map[string]any{"ticket": f.newTicket(abs), "expires_in": int(ticketTTL.Seconds())})
}

func (f *FileServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	abs, ok := f.claimTicket(r.URL.Query().Get("t"))
	if !ok {
		http.Error(w, "invalid or expired ticket", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	name := filepath.Base(abs)

	if info.IsDir() {
		// A folder comes down as .tar.gz: a browser has no way to receive a tree.
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf("attachment; filename=%q", name+".tar.gz"))
		gz := gzip.NewWriter(w)
		defer gz.Close()
		tw := tar.NewWriter(gz)
		defer tw.Close()
		filepath.Walk(abs, func(p string, fi os.FileInfo, err error) error {
			if err != nil || !fi.Mode().IsRegular() {
				return nil // skip sockets, fifos and anything unreadable
			}
			rel, err := filepath.Rel(filepath.Dir(abs), p)
			if err != nil {
				return nil
			}
			hdr, err := tar.FileInfoHeader(fi, "")
			if err != nil {
				return nil
			}
			hdr.Name = filepath.ToSlash(rel)
			if tw.WriteHeader(hdr) != nil {
				return nil
			}
			src, err := os.Open(p)
			if err != nil {
				return nil
			}
			defer src.Close()
			io.Copy(tw, src)
			return nil
		})
		return
	}

	file, err := os.Open(abs)
	if err != nil {
		http.Error(w, "could not open", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Content-Type", "application/octet-stream")
	// ServeContent gives us Range and Content-Length, which is what makes the
	// browser show a progress bar and lets an interrupted download resume.
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (f *FileServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(r) {
		writeErr(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "usar POST")
		return
	}
	dir, err := f.resolve(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusForbidden, "%v", err)
		return
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		writeErr(w, http.StatusBadRequest, "the destination is not a directory")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	// 32 MiB in memory; anything larger spills to a temp file rather than
	// taking the RAM with it.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid upload: %v", err)
		return
	}
	var saved []map[string]any
	for _, headers := range r.MultipartForm.File {
		for _, fh := range headers {
			// filepath.Base discards any path the client put in the filename:
			// "../../etc/passwd" collapses to "passwd".
			name := filepath.Base(fh.Filename)
			if name == "." || name == "/" || name == "" {
				continue
			}
			src, err := fh.Open()
			if err != nil {
				continue
			}
			dst, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				src.Close()
				writeErr(w, http.StatusInternalServerError, "could not write %s: %v", name, err)
				return
			}
			n, _ := io.Copy(dst, src)
			dst.Close()
			src.Close()
			saved = append(saved, map[string]any{"name": name, "size": n})
		}
	}
	writeJSON(w, map[string]any{"uploaded": saved, "path": f.rel(dir)})
}

// handleOp covers Midnight Commander's function-key operations:
// F7 make directory, F8 delete, F6 rename/move.
func (f *FileServer) handleOp(w http.ResponseWriter, r *http.Request) {
	if !f.authorized(r) {
		writeErr(w, http.StatusUnauthorized, "no autorizado")
		return
	}
	var body struct {
		Op   string `json:"op"`
		Path string `json:"path"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	abs, err := f.resolve(body.Path)
	if err != nil {
		writeErr(w, http.StatusForbidden, "%v", err)
		return
	}
	switch body.Op {
	case "mkdir":
		if err := os.MkdirAll(abs, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "%v", err)
			return
		}
	case "delete":
		if abs == f.root {
			writeErr(w, http.StatusForbidden, "the root itself cannot be deleted")
			return
		}
		if err := os.RemoveAll(abs); err != nil {
			writeErr(w, http.StatusInternalServerError, "%v", err)
			return
		}
	case "rename":
		dst, err := f.resolve(body.To)
		if err != nil {
			writeErr(w, http.StatusForbidden, "%v", err)
			return
		}
		if err := os.Rename(abs, dst); err != nil {
			writeErr(w, http.StatusInternalServerError, "%v", err)
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "unknown operation: %q", body.Op)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "op": body.Op, "path": f.rel(abs)})
}
