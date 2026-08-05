# SentinelDesk
# A collaborative operating system for people and AI agents.
#
# Copyright 2026 Federico Pereira <fpereira@cnsoluciones.com>
# Co-authored by Nicolas Pereira <npereira@cnsoluciones.com>
#
# Licensed under the Apache License, Version 2.0.
#
# This product's name and logo are trademarks of Federico Pereira and are not
# covered by the license above. See the README for the trademark policy.
#
# SPDX-License-Identifier: Apache-2.0

# SentinelDesk — build, version, release.
#
# One constraint shapes everything here: the binary uses CGO (go-gst links
# GStreamer), so it CANNOT be cross-compiled with a bare GOOS/GOARCH the way a
# static Go project can. Every Linux binary is built inside Docker on a Debian
# 13 base — the same image the desktop runs on — and buildx provides the other
# architecture through emulation. What comes out of `release-binaries` is
# byte-for-byte the binary the container runs, which is exactly what a native
# install on Debian 13 or Raspberry Pi OS wants.

BINARY  := sentineldesk
DIST    := dist
GO      := go
DOCKER  ?= docker
# The local tag (what the compose file runs) and the registry name (what
# `make push` publishes). Override REGISTRY_IMAGE for another registry.
# Two variants from one Dockerfile: lite is the desktop plus the tools people
# need in it; full adds what is too large or too niche to hand everybody. Both
# carry the version in the tag, because "latest" answers no question worth
# asking six months from now — and both keep a moving tag so the compose files
# do not have to be edited on every build.
IMAGE          ?= sentineldesk:latest
IMAGE_LITE     ?= sentineldesk:lite
IMAGE_FULL     ?= sentineldesk:full
REGISTRY_IMAGE ?= lordbasex/sentineldesk
# Pinned on purpose. Compose otherwise derives the project name from the
# directory holding the file, so moving the compose file orphans every running
# container: the old project keeps the fixed container_name, and the new one
# cannot take a name it does not own. That failure surfaces as a "name already
# in use" conflict with no hint that a rename caused it.
PROJECT ?= sentineldesk
COMPOSE ?= $(DOCKER) compose -p $(PROJECT) -f deploy/docker-compose.yml

# ─── Version, derived from git ───────────────────────────────────────────────
# The patch auto-increments (with carry: .9 bumps the minor, .9.9 the major)
# every time the git hash changes, persisted in version.txt — local and
# git-ignored, so each machine counts its own builds. Stamped into the binary
# with -ldflags -X; `sentineldesk -version` prints it and the rail shows it.
VERSION_PKG     := github.com/lordbasex/sentineldesk/internal/version
INITIAL_VERSION ?= 1.0.0
VERSION_FILE    := version.txt

git_hash   := $(shell git rev-parse --short HEAD 2>/dev/null || echo development)
build_date := $(shell date +%Y%m%d-%H%M%S)

ifeq ($(wildcard $(VERSION_FILE)),)
  last_version  := $(INITIAL_VERSION)
  last_git_hash :=
else
  last_version  := $(shell awk -F': ' '/initial_version:/ {print $$2}' $(VERSION_FILE) | xargs)
  last_git_hash := $(shell awk -F': ' '/git_hash:/ {print $$2}' $(VERSION_FILE) | xargs)
endif

ifeq ($(strip $(last_git_hash)),)
  next_version := $(last_version)
else ifeq ($(strip $(git_hash)),$(strip $(last_git_hash)))
  next_version := $(last_version)
else
  next_version := $(shell \
    major=$$(echo $(last_version) | awk -F. '{print $$1}'); \
    minor=$$(echo $(last_version) | awk -F. '{print $$2}'); \
    patch=$$(echo $(last_version) | awk -F. '{print $$3}'); \
    if [ "$$patch" -ge 9 ]; then \
      if [ "$$minor" -ge 9 ]; then echo "$$((major + 1)).0.0"; \
      else echo "$$major.$$((minor + 1)).0"; fi; \
    else echo "$$major.$$minor.$$((patch + 1))"; fi)
endif

VERSION_LDFLAGS := -X $(VERSION_PKG).Version=$(next_version) \
                   -X $(VERSION_PKG).GitHash=$(git_hash) \
                   -X $(VERSION_PKG).BuildDate=$(build_date)
LDFLAGS := -s -w $(VERSION_LDFLAGS)

VERSION_ARGS := --build-arg VERSION=$(next_version) \
                --build-arg GIT_HASH=$(git_hash) \
                --build-arg BUILD_DATE=$(build_date)

.PHONY: build image image-lite image-full up down logs shell test fmt vet help \
        _version version release-binaries checksums push release

# _version persists version.txt and prints the version. One target, so make
# runs it once even when several builds depend on it.
_version:
	@echo "initial_version: $(next_version)" > $(VERSION_FILE)
	@echo "git_hash: $(git_hash)" >> $(VERSION_FILE)
	@echo "▶ version: v$(next_version) ($(git_hash)) · build $(build_date)"

## version: show the version a build would get, without building
version:
	@echo "v$(next_version) ($(git_hash)) · build $(build_date)"

## build: compile for the host — a fast type check (needs local GStreamer)
build: _version
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" ./...

## image: build the container image, version stamped in
image: _version
	@echo "▶ lite…"
	$(DOCKER) build $(VERSION_ARGS) -f deploy/Dockerfile --target desktop \
	  -t $(IMAGE) -t $(IMAGE_LITE) -t sentineldesk:$(next_version) \
	  -t sentineldesk:$(next_version)-lite .
	@echo "▶ full…"
	$(DOCKER) build $(VERSION_ARGS) -f deploy/Dockerfile --target full \
	  -t $(IMAGE_FULL) -t sentineldesk:$(next_version)-full .
	@echo "✓ sentineldesk:$(next_version)-lite  (also :latest, :lite)"
	@echo "✓ sentineldesk:$(next_version)-full  (also :full)"

## image-lite: only the lite variant, when the full one is not needed
image-lite: _version
	$(DOCKER) build $(VERSION_ARGS) -f deploy/Dockerfile --target desktop \
	  -t $(IMAGE) -t $(IMAGE_LITE) -t sentineldesk:$(next_version)-lite .

## image-full: only the full variant
image-full: _version
	$(DOCKER) build $(VERSION_ARGS) -f deploy/Dockerfile --target full \
	  -t $(IMAGE_FULL) -t sentineldesk:$(next_version)-full .

## up: build the image and start the desktop
up: image
	$(COMPOSE) up -d

## down: stop everything
down:
	$(COMPOSE) down --remove-orphans

## logs: follow the desktop's logs
logs:
	$(COMPOSE) logs -f sentineldesk

## shell: a root shell inside the running desktop
shell:
	$(DOCKER) exec -it -u root sentineldesk bash

## release-binaries: Linux amd64 + arm64 binaries into dist/, named with the version
#
# Built INSIDE the Docker build stage (Go on Debian 13) and exported with
# buildx, one platform at a time. The foreign architecture runs under QEMU —
# slow, but it is a real build against real Debian 13 libraries, which is the
# only kind CGO permits. The result runs on any Debian 13: a VPS, a Raspberry
# Pi 5 on Raspberry Pi OS (trixie), a cloud instance.
release-binaries: _version
	@mkdir -p $(DIST)
	@for arch in amd64 arm64; do \
	  echo "▶ linux/$$arch…"; \
	  $(DOCKER) buildx build $(VERSION_ARGS) \
	    --platform linux/$$arch --target bin \
	    --output type=local,dest=$(DIST)/.stage-$$arch \
	    -f deploy/Dockerfile . || exit 1; \
	  cp $(DIST)/.stage-$$arch/sentineldesk \
	     $(DIST)/$(BINARY)-v$(next_version)-linux-$$arch || exit 1; \
	  rm -rf $(DIST)/.stage-$$arch; \
	  echo "✓ $(DIST)/$(BINARY)-v$(next_version)-linux-$$arch"; \
	done

## checksums: SHA256SUMS.txt over the binaries in dist/
checksums:
	@cd $(DIST) && if command -v sha256sum >/dev/null 2>&1; \
	  then sha256sum $(BINARY)-* > SHA256SUMS.txt; \
	  else shasum -a 256 $(BINARY)-* > SHA256SUMS.txt; fi
	@echo "✓ $(DIST)/SHA256SUMS.txt"

## push: build and push the multi-arch image (:latest and :<version>)
#
# One buildx invocation for both platforms, so the registry holds a single
# manifest list and `docker pull` picks the right architecture on its own.
# Needs `docker login` first; override REGISTRY_IMAGE for another registry.
push: _version
	$(DOCKER) buildx build $(VERSION_ARGS) \
	  --platform linux/amd64,linux/arm64 \
	  -f deploy/Dockerfile --target desktop \
	  -t $(REGISTRY_IMAGE):latest -t $(REGISTRY_IMAGE):$(next_version) \
	  -t $(REGISTRY_IMAGE):lite -t $(REGISTRY_IMAGE):$(next_version)-lite \
	  --push .
	$(DOCKER) buildx build $(VERSION_ARGS) \
	  --platform linux/amd64,linux/arm64 \
	  -f deploy/Dockerfile --target full \
	  -t $(REGISTRY_IMAGE):full -t $(REGISTRY_IMAGE):$(next_version)-full \
	  --push .
	@echo "✓ pushed $(REGISTRY_IMAGE) :latest :lite :full and $(next_version){,-lite,-full}"

## release: binaries + checksums + GitHub Release (tag v<version>), via gh
release: release-binaries checksums
	@command -v gh >/dev/null 2>&1 || { echo "✗ gh CLI not installed/authenticated: https://cli.github.com"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "✗ uncommitted changes — commit before releasing:"; git status --short; exit 1; }
	gh release create v$(next_version) \
	  $(DIST)/$(BINARY)-v$(next_version)-linux-amd64 \
	  $(DIST)/$(BINARY)-v$(next_version)-linux-arm64 \
	  $(DIST)/SHA256SUMS.txt \
	  --title "SentinelDesk v$(next_version)" \
	  --notes "commit $(git_hash), built $(build_date)"
	@echo "✓ released v$(next_version)"

test:
	$(GO) test ./...

fmt:
	gofmt -w cmd internal deploy

vet:
	$(GO) vet ./...

## help: list the targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
