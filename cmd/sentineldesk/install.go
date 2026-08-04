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

// The binary as its own installer.
//
// `sentineldesk -install` turns the binary into a small web server that hands
// out the deployment files it carries: compose files, supervisor config, the
// desktop scripts, the Dockerfiles. Point a browser or curl at it from any
// machine and take what you need — a tarball of everything, or one file.
//
// The point is provenance. A binary copied to a Raspberry Pi or a VPS brings
// its own configuration, at the commit it was built from; there is no "now go
// clone the repo and hope it matches" step, which is exactly the step where
// version skew creeps in.
//
// `-extract-deploy <dir>` is the same content without the server, for scripts:
// the install script downloads one binary and asks IT for the rest.

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lordbasex/sentineldesk/deploy"
	"github.com/lordbasex/sentineldesk/internal/version"
)

// runInstallServer serves the embedded deploy tree over HTTP until killed.
func runInstallServer(port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		writeInstallIndex(w)
	})

	// The whole tree in one download, which is what a script wants.
	mux.HandleFunc("/deploy.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="deploy.tar.gz"`)
		if err := writeDeployTar(w); err != nil {
			log.Printf("install: tarball: %v", err)
		}
	})

	// Individual files under /deploy/..., for browsing and curl.
	mux.Handle("/deploy/", http.StripPrefix("/deploy/",
		http.FileServer(http.FS(deploy.FS))))

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, version.String())
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("install server on http://0.0.0.0%s — serving the deploy tree of %s", addr, version.Short())
	log.Printf("  everything:  curl -O http://<this-host>%s/deploy.tar.gz", addr)
	log.Printf("  one file:    curl -O http://<this-host>%s/deploy/docker-compose.yml", addr)
	err := http.ListenAndServe(addr, mux)
	if port == 80 && err != nil && strings.Contains(err.Error(), "permission denied") {
		return fmt.Errorf("port 80 needs root (or CAP_NET_BIND_SERVICE); "+
			"run with sudo, or pick another port: -install -install-port 8081: %w", err)
	}
	return err
}

// writeInstallIndex lists what is on offer. Plain HTML on purpose: this page is
// read once per installation, over curl as often as over a browser.
func writeInstallIndex(w http.ResponseWriter) {
	var files []string
	fs.WalkDir(deploy.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "embed.go" {
			return nil
		}
		files = append(files, p)
		return nil
	})
	sort.Strings(files)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><title>SentinelDesk %s</title>",
		html.EscapeString(version.Short()))
	fmt.Fprintf(w, "<body style='font-family:ui-monospace,monospace;background:#0c0e0d;color:#dedede;padding:2em;line-height:1.7'>")
	fmt.Fprintf(w, "<h1 style='font-weight:400'>SentinelDesk %s</h1>", html.EscapeString(version.String()))
	fmt.Fprintf(w, "<p>The deployment files this binary was built with.</p>")
	fmt.Fprintf(w, "<p><a style='color:#3fd68c' href='/deploy.tar.gz'>deploy.tar.gz</a> — everything below in one download</p><ul>")
	for _, f := range files {
		fmt.Fprintf(w, "<li><a style='color:#3fd68c' href='/deploy/%s'>%s</a></li>",
			html.EscapeString(f), html.EscapeString(f))
	}
	fmt.Fprintf(w, "</ul></body>")
}

// writeDeployTar streams the embedded tree as deploy/... inside a tar.gz, so it
// unpacks next to the binary the way the repository lays it out.
func writeDeployTar(w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return fs.WalkDir(deploy.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "embed.go" {
			return nil
		}
		data, err := fs.ReadFile(deploy.FS, p)
		if err != nil {
			return err
		}
		mode := int64(0o644)
		// Scripts come back runnable. The embed FS does not keep permission
		// bits, so the extension is the only record of intent that survives.
		if strings.HasSuffix(p, ".sh") || !strings.Contains(path.Base(p), ".") && strings.HasPrefix(p, "desktop/") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: "deploy/" + p, Mode: mode, Size: int64(len(data)),
			ModTime: time.Now(),
		}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
}

// extractDeploy writes the embedded tree to disk, for the install script.
func extractDeploy(dir string) error {
	err := fs.WalkDir(deploy.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == "embed.go" {
			return err
		}
		data, err := fs.ReadFile(deploy.FS, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, "deploy", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if strings.HasSuffix(p, ".sh") || !strings.Contains(path.Base(p), ".") && strings.HasPrefix(p, "desktop/") {
			mode = 0o755
		}
		return os.WriteFile(dst, data, mode)
	})
	if err == nil {
		log.Printf("deploy tree written to %s/deploy (from %s)", dir, version.Short())
	}
	return err
}
