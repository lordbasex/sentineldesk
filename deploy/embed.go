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

// Package deploy embeds the deployment tree into the binary itself.
//
// The binary is the one artifact a release actually ships; everything else —
// compose files, supervisor config, the desktop scripts, the Dockerfiles — used
// to require cloning the repository at the matching commit. Embedding them
// closes that gap: whatever binary you hold carries exactly the configuration
// it was built with, and `-install` (an HTTP server) or the install script can
// get them back out. There is no version skew to have, because there is no
// second download.
//
// certs/ and wallpaper/ are deliberately absent: one holds private keys, the
// other is content, and neither is configuration.
package deploy

import "embed"

//go:embed config desktop
//go:embed Caddyfile.auto Caddyfile.custom Caddyfile.selfsigned Caddyfile.wildcard
//go:embed Dockerfile Dockerfile.caddy
//go:embed docker-compose.yml docker-compose.cpu.yml docker-compose.nvidia.yml docker-compose.vaapi.yml
var FS embed.FS
