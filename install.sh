#!/bin/bash
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
# SentinelDesk installer — Debian 13 (trixie) and Raspberry Pi OS, amd64 / arm64.
#
#   sudo ./install.sh            # asks: Docker or native
#   sudo ./install.sh auto       # Docker if present, native otherwise
#   sudo ./install.sh docker     # container + TURN, via docker compose
#   sudo ./install.sh native     # systemd + supervisor on the host itself
#
# Variant — lite unless you say otherwise:
#   sudo ./install.sh docker lite   # the desktop plus the tools to work in it
#   sudo ./install.sh docker full   # adds LibreOffice, Firefox, GIMP,
#                                   # Wireshark, a compiler, and on amd64
#                                   # Steam and Wine. Roughly twice the size.
#
# Options:
#   --version vX.Y.Z   install that release instead of the latest
#   --binary PATH      use a local binary instead of downloading one
#   --user NAME        native only: run the desktop as NAME instead of creating
#                      a dedicated sentineldesk account
#   --tls-domain NAME  native only: put Caddy in front and get a real Let's
#                      Encrypt certificate for NAME. Needs ports 80 and 443
#                      reachable, and a NAME that resolves here — a bare IP
#                      will not do; 203-0-113-45.sslip.io style names will.
#   --tls-email ADDR   expiry notices from Let's Encrypt (optional)
#
# The script downloads ONE artifact: the binary. Everything else — compose
# files, supervisor config, the desktop scripts — the binary carries embedded
# and writes back out with `-extract-deploy`, so the configuration always
# matches the code, at the commit it was built from.

set -euo pipefail

REPO="${SENTINELDESK_REPO:-lordbasex/sentineldesk}"
IMAGE="${SENTINELDESK_IMAGE:-lordbasex/sentineldesk}"
BIN=/usr/local/bin/sentineldesk
OPT=/opt/sentineldesk
MODE=""
WANT_VERSION=""
LOCAL_BINARY=""
# Native mode only. Empty means "create a dedicated sentineldesk account", which
# is the default and the safer one — see install_native_mode for what changes
# when this names an account that already exists.
WANT_USER=""
# Native only. A name Let's Encrypt can validate — NOT a bare IP, which ACME
# cannot issue for. Empty leaves the backend on its own self-signed certificate.
TLS_DOMAIN=""
TLS_EMAIL=""
# lite is the default on purpose: it is the desktop plus the tools somebody
# needs in it, and it is what most installations want. full adds LibreOffice,
# Firefox, GIMP, Wireshark, a compiler and — on amd64 — Steam and Wine, at
# roughly twice the download.
VARIANT="lite"

say()  { printf '\033[32m▶\033[0m %s\n' "$*"; }
warn() { printf '\033[33m!\033[0m %s\n' "$*"; }
die()  { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

# --- arguments ---------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    auto|docker|native) MODE="$1" ;;
    lite|full)          VARIANT="$1" ;;
    --install|install)  MODE="${MODE:-auto}" ;;
    --version) WANT_VERSION="$2"; shift ;;
    --binary)  LOCAL_BINARY="$2"; shift ;;
    --user)    WANT_USER="$2"; shift ;;
    --tls-domain) TLS_DOMAIN="$2"; shift ;;
    --tls-email)  TLS_EMAIL="$2"; shift ;;
    -h|--help) sed -n '14,37p' "$0"; exit 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
  shift
done

# --- preconditions -----------------------------------------------------------
#
# "run as root" is a complete answer only to somebody who already knows what
# that means. Whoever is reading this is following a guide on a fresh Raspberry
# Pi, so the refusal spells out the two ways forward and gives the exact line to
# paste for each — including the piped one, because that is the command the
# guide hands out and `$0` under `curl | bash` is the useless string `bash`.
if [ "$(id -u)" != 0 ]; then
  printf '\033[31m✗\033[0m %s\n' "this installer needs root: it installs packages, writes to /etc" >&2
  printf '  %s\n' "and registers a service, and none of that is possible as a normal user." >&2
  cat >&2 <<EOF

  Either become root first —

      sudo su
      curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | bash -s ${MODE:-auto}

  — or leave sudo in the command and stay where you are:

      curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sudo bash -s ${MODE:-auto}

  Both do exactly the same thing. If this script is already on disk, that is
  \`sudo $0 ${MODE:-auto}\`.

EOF
  exit 1
fi

# Caught here rather than three minutes later in Caddy's retry loop, because
# "I have a public IP" is exactly why people reach for this flag. ACME validates
# a NAME; there is no challenge that proves control of a bare address, so
# Let's Encrypt will simply refuse and Caddy will keep asking.
if printf '%s' "${TLS_DOMAIN:-}" | grep -qE '^([0-9]{1,3}\.){3}[0-9]{1,3}$'; then
  die "--tls-domain needs a name, not an IP. Let's Encrypt cannot certify a bare
  address. A wildcard DNS service gives you a name that points at yours for
  nothing — for $TLS_DOMAIN that would be $(printf '%s' "$TLS_DOMAIN" | tr . -).sslip.io"
fi

case "$(uname -m)" in
  x86_64)  ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m) (amd64 and arm64 only)" ;;
esac

# --- asking a question when stdin is the script -------------------------------
#
# `curl … | sudo bash -s auto` is the command the guide hands out, and under it
# the script IS standard input. A plain `read` therefore does not reach the
# keyboard: it consumes the next LINE OF THIS FILE and answers the question with
# it — measurably, not in theory. Piping a five-line script to bash shows line 4
# vanish and its text arrive as the answer.
#
# In this file that produced two failures nobody could have diagnosed from the
# output. The OS check answered itself with the string "  esac" and exited 1
# with no explanation on any machine that was not Debian 13 — every Raspberry Pi
# OS release before trixie. The mode question ate the `case` that assigns MODE,
# leaving it empty, so the installer downloaded the binary and then installed
# nothing at all, successfully.
#
# /dev/tty is the terminal itself, which a pipe does not take away, so that is
# where questions go and where answers come from. When there is genuinely no
# terminal — cron, a Dockerfile, CI — the answer comes back empty and each
# caller below decides what that means, out loud.
#
# Opened rather than tested. `[ -r /dev/tty ]` answers "does that node exist",
# which is yes inside a container with no terminal attached — and then the
# redirect fails with ENXIO, which under `set -e` ends the install on the spot
# instead of falling back. Opening it is the only test that means anything.
ask() {
  local prompt="$1" var="$2" reply=""
  if { exec 3<>/dev/tty; } 2>/dev/null; then
    printf '%s' "$prompt" >&3
    read -r reply <&3 || reply=""
    exec 3>&-
  fi
  printf -v "$var" '%s' "$reply"
}

# Debian 13 is what the binary is built against (the same base as the Docker
# image), and what the package names below belong to. Raspberry Pi OS is Debian
# under a different name — and Raspberry Pi OS trixie reports itself as plain
# debian:13, so a current Pi never reaches the question at all.
if [ -r /etc/os-release ]; then
  . /etc/os-release
  case "${ID:-}:${VERSION_ID:-}" in
    debian:13|raspbian:13) ;;
    *) warn "this targets Debian 13 (trixie); found ${PRETTY_NAME:-unknown}."
       warn "  Package names and versions come from Debian 13; on an older or"
       warn "  different base some of them will not exist and the install stops."
       if [ "${SENTINELDESK_ASSUME_YES:-0}" = 1 ]; then
         warn "  SENTINELDESK_ASSUME_YES=1 is set — continuing anyway."
       else
         ask "  Continue anyway? [y/N] " answer
         if [ -z "$answer" ]; then
           die "no terminal to ask on, so this is not something to guess at.
  Re-run with SENTINELDESK_ASSUME_YES=1 to accept the risk, or download the
  script and run it directly:

      curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh -o install.sh
      sudo bash install.sh ${MODE:-auto}"
         fi
         [ "${answer,,}" = y ] || die "stopped at your request. Nothing has been changed."
       fi ;;
  esac
fi

# --- mode --------------------------------------------------------------------
if [ -z "$MODE" ]; then
  echo "How should SentinelDesk run on this machine?"
  echo "  1) Docker    — isolated, easy to remove, the recommended default"
  echo "  2) Native    — straight on the host: systemd + supervisor, no container"
  ask "Choice [1/2]: " choice
  case "$choice" in
    2) MODE=native ;;
    1) MODE=docker ;;
    # No terminal and no argument. Falling through to `auto` is the same answer
    # the guide's own command gives, and saying so beats picking one in silence.
    *) MODE=auto
       say "no answer (not a terminal?) — deciding the way 'auto' would" ;;
  esac
fi
if [ "$MODE" = auto ]; then
  if command -v docker >/dev/null 2>&1; then MODE=docker; else MODE=native; fi
  say "auto: chose $MODE"
fi

# --- the binary --------------------------------------------------------------
# Installed under its plain name regardless of how it arrived; the versioned
# name only exists in the release listing. `-version` says what it is.
install_binary() {
  if [ -n "$LOCAL_BINARY" ]; then
    [ -f "$LOCAL_BINARY" ] || die "no such file: $LOCAL_BINARY"
    install -m 0755 "$LOCAL_BINARY" "$BIN"
  else
    command -v curl >/dev/null 2>&1 || { apt-get update -qq; apt-get install -y -qq curl; }
    local tag="$WANT_VERSION"
    if [ -z "$tag" ]; then
      tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
            | grep -m1 '"tag_name"' | cut -d'"' -f4) \
        || die "could not resolve the latest release of $REPO"
    fi
    local url="https://github.com/$REPO/releases/download/$tag/sentineldesk-$tag-linux-$ARCH"
    say "downloading $url"
    local tmp; tmp=$(mktemp)
    curl -fSL -o "$tmp" "$url" || die "download failed — does release $tag have a $ARCH binary?"
    # The checksum file is published beside the binaries; a truncated download
    # dies here rather than as a mysterious crash at first start.
    if curl -fsSL -o "$tmp.sums" \
         "https://github.com/$REPO/releases/download/$tag/SHA256SUMS.txt" 2>/dev/null; then
      ( cd "$(dirname "$tmp")" \
        && grep "sentineldesk-$tag-linux-$ARCH" "$tmp.sums" \
           | sed "s|sentineldesk-$tag-linux-$ARCH|$(basename "$tmp")|" \
           | sha256sum -c - ) || die "checksum mismatch — refusing to install"
    else
      warn "no SHA256SUMS.txt in the release; installing unverified"
    fi
    install -m 0755 "$tmp" "$BIN"
    rm -f "$tmp" "$tmp.sums"
  fi
  # Deliberately NOT `$BIN -version` here. The binary links GStreamer
  # dynamically, and at this point nothing has installed it: on a bare Debian
  # the very next line was
  #
  #   error while loading shared libraries: libgstreamer-1.0.so.0
  #
  # followed by "installed:" with nothing after it — an alarming way to report
  # success. Native mode says the version once the packages are in; Docker mode
  # never can, because in Docker mode the host is not supposed to have them.
  say "installed: $BIN"
}

# --- TLS, on by default -------------------------------------------------------
#
# An installed machine is on a network by definition, and without this the login
# and everything after it cross it in clear text. The README has always said to
# serve this over TLS before exposing it; an installer that ends on http:// is
# advice nobody follows.
#
# Self-signed, so it needs nothing from anybody: the server generates an ECDSA
# P-256 certificate on first start and keeps it in the home, which persists.
# The browser will warn once per machine — that is what self-signed means, and
# it is a better starting point than plain HTTP. Put Caddy in front, or set
# TLS_CERT/TLS_KEY, when there is a real name to get a certificate for.
#
# The certificate covers localhost, 127.0.0.1, ::1 and the hostname on its own.
# On a VPS nobody types any of those — they type the IP, which would then not
# match and turn a warning you can click through into an error you cannot. So
# every address this machine actually answers on goes in, discovered here.
host_addresses() {
  hostname -I 2>/dev/null \
    | tr ' ' '\n' \
    | grep -vE '^(127\.|::1|$)' \
    | paste -sd, - 2>/dev/null \
    || true
}

# Where the login lives, which is not the same file in the two modes. Both are
# mode 600 and both are read back rather than remembered — see env_value.
env_file() {
  if [ "$MODE" = docker ]; then echo "$OPT/.env"; else echo /etc/sentineldesk/env; fi
}

# Read a key back out of it instead of carrying the generated value around,
# because on a re-install nothing is generated at all: the file is written once
# and then belongs to whoever operates the machine. The closing summary still
# has to print the login the browser is about to ask for.
#
# awk rather than `sed | head`, because `head` closing the pipe early makes the
# left-hand side fail and this script runs under `set -o pipefail` — a password
# read would then take the whole install down on its last line.
env_value() {
  local f; f=$(env_file)
  [ -r "$f" ] || return 0
  awk -F= -v k="$1" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$f"
}

# The scheme to print. Read back from what was actually configured rather than
# assumed, and the two modes disagree on what "unset" means, which is why this
# is not one grep: the compose file written below passes TLS_SELFSIGNED=1 unless
# .env overrides it, while a native install has nothing in front of the binary
# and the binary's own default is off. So an env file written before TLS became
# the default is still on http:// and should be told so.
desktop_scheme() {
  local f; f=$(env_file)
  if [ "$MODE" = docker ]; then
    if [ -r "$f" ] && grep -q '^TLS_SELFSIGNED=0' "$f"; then echo http; else echo https; fi
    return
  fi
  if [ -r "$f" ] && grep -q '^TLS_SELFSIGNED=1' "$f"; then echo https; else echo http; fi
}

# Every address this machine answers on, not just the first one. Whoever
# installed this is on a Raspberry Pi over SSH and will open the desktop from a
# laptop, and the machine may well be on Ethernet and Wi-Fi at once with a
# Docker bridge beside them — guessing which of those the laptop can reach is
# not something the installer can do, so it lists them all and lets the reader
# pick. Same discovery as host_addresses(), which is what went into the
# certificate, so every URL here is one the certificate names.
desktop_urls() {
  # Caddy answers on 443 under the name it was given, and the backend is
  # confined to the loopback — so that name IS the address, and the only one.
  if [ -n "$TLS_DOMAIN" ]; then
    echo "https://$TLS_DOMAIN"
    return
  fi
  local scheme ip; scheme=$(desktop_scheme)
  hostname -I 2>/dev/null | tr ' ' '\n' | grep -vE '^(127\.|::1|$)' | while read -r ip; do
    case "$ip" in
      *:*) printf '%s://[%s]:8080\n' "$scheme" "$ip" ;;   # a literal IPv6 needs brackets in a URL
      *)   printf '%s://%s:8080\n'   "$scheme" "$ip" ;;
    esac
  done
  # Last, and always: on a machine with a desktop of its own this is the one
  # that works, and it is the fallback when hostname -I finds nothing.
  printf '%s://localhost:8080\n' "$scheme"
}

# --- what was here before ----------------------------------------------------
#
# Read BEFORE anything is overwritten, so the end of the run can say whether
# this was a first install, a reinstall of the same release, or a move to a new
# one. Re-running this script IS the update path, and an update that does not
# tell you what changed is one you have to go and verify by hand.
#
# Two places to look, because the two modes keep the version in different
# places. Native: the installed binary, which on a machine that already has the
# desktop can run — its libraries are in. Docker: the host binary usually
# CANNOT run (Docker mode never installs GStreamer, on purpose), so the answer
# comes from the container instead.
PREV_VERSION=""
detect_previous_version() {
  if [ -x "$BIN" ]; then
    PREV_VERSION=$("$BIN" -version 2>/dev/null | sed 's/^sentineldesk //') || PREV_VERSION=""
  fi
  if [ -z "$PREV_VERSION" ] && command -v docker >/dev/null 2>&1; then
    PREV_VERSION=$(docker exec sentineldesk /usr/local/bin/sentineldesk -version 2>/dev/null \
                   | sed 's/^sentineldesk //') || PREV_VERSION=""
  fi
  [ -n "$PREV_VERSION" ] && say "found installed: $PREV_VERSION"
  return 0
}

# Said once, at the end, in the three shapes this can take.
report_version() {
  local now="$1"
  [ -n "$now" ] || return 0
  if [ -z "$PREV_VERSION" ]; then
    say "installed $now"
  elif [ "$PREV_VERSION" = "$now" ]; then
    say "reinstalled $now (unchanged)"
  else
    say "updated: $PREV_VERSION  →  $now"
  fi
}

# --- the closing summary ------------------------------------------------------
#
# An install prints several minutes of apt output, and the two lines that
# actually matter — where to go and what to type when it asks who you are —
# used to be somewhere up in that scroll. This says them once, at the end, in
# full, on the assumption that the reader is following the guide and has no
# idea where a generated password would otherwise be found.
#
# English only, like every other line this script prints: it is one voice, and a
# half-translated installer is worse than an untranslated one.
GUIDE_URL="https://lordbasex.github.io/sentineldesk/docs/guide/"

rule() { printf '\033[90m  %s\033[0m\n' "──────────────────────────────────────────────────────────────────"; }

# --- does the panel have its icons? ------------------------------------------
#
# The one part of a native install that can fail without anything failing. The
# packages install, the service starts, the panel comes up — with empty squares
# where the launcher icons should be, and not a line about it anywhere. Somebody
# then has to guess between a missing theme, a renamed icon and a broken config,
# from a photograph.
#
# So it is checked against the panel the desktop will actually read, once, at
# the end of the install that put both there. Native only: in Docker the panel
# and the icons ship in the same image and were verified when it was built,
# while here they come from whatever apt resolved on this machine today.
#
# One `find` builds the index rather than one per name — /usr/share/icons holds
# about a hundred thousand files once Papirus is in, and twenty-six passes over
# that on an SD card is a minute nobody asked for.
check_panel_icons() {
  local panel=/etc/skel.sentineldesk/.config/lxpanel/LXDE/panels/panel
  [ -r "$panel" ] || return 0

  # Symlinks count. Papirus is 43,000 files and 43,000 symlinks pointing at
  # them — `applications-system.svg` is a link to `application-default-icon.svg`
  # and so are most of the freedesktop names — so `-type f` alone reports half
  # the theme as missing on a machine where everything works.
  local index; index=$(mktemp)
  find /usr/share/icons /usr/share/pixmaps \( -type f -o -type l \) \
       \( -name '*.svg' -o -name '*.png' -o -name '*.xpm' \) -printf '%f\n' 2>/dev/null \
    | sed -E 's/\.(svg|png|xpm)$//' | sort -u > "$index"

  local missing="" total=0 n d
  for n in $(sed -n 's/^[[:space:]]*image=\(..*\)$/\1/p' "$panel" | sort -u); do
    total=$((total + 1))
    case "$n" in
      # A path names one file and either it is there or it is not; a name goes
      # through the icon theme, which is why the panel uses names.
      /*) [ -e "$n" ] || missing="$missing $n" ;;
      *)  grep -qxF "$n" "$index" || missing="$missing $n" ;;
    esac
  done
  # The launchbar names .desktop files instead of icons, and a missing one is a
  # blank button in exactly the same way.
  for d in $(sed -n 's/^[[:space:]]*id=\(..*\.desktop\)$/\1/p' "$panel" | sort -u); do
    total=$((total + 1))
    [ -f "/usr/share/applications/$d" ] || missing="$missing $d"
  done
  rm -f "$index"

  if [ -n "$missing" ]; then
    warn "the panel names things this machine does not have, so those entries"
    warn "  will come up without an icon:$missing"
    warn "  the usual cause is a missing icon theme — check with:"
    warn "    dpkg -l papirus-icon-theme adwaita-icon-theme"
  else
    say "panel: all $total icons and launchers resolve"
  fi

  # And then the failure the check above cannot see, which is the one that
  # actually looks like "the panel has no icons".
  #
  # Papirus is SVG, and GTK reads SVG only through the gdk-pixbuf loader that
  # librsvg2-common registers. Without it every icon file is present, every name
  # resolves, the line above says all twenty-six are fine — and the panel comes
  # up with the same blue placeholder lozenge in every position. Photographed
  # side by side on one machine with nothing changed but that package.
  #
  # The glob covers the architecture in the path; with no match, grep -s is
  # quiet and simply fails, which is the answer we want.
  if ! grep -qs svg /usr/lib/*/gdk-pixbuf-2.0/2.10.0/loaders.cache; then
    warn "GTK has no SVG loader on this machine, so every icon will draw as a"
    warn "  placeholder even though the files are all installed. Repair with:"
    warn "    apt-get install --reinstall librsvg2-common"
  fi
}

summary() {
  local version="$1" f user pass urls
  f=$(env_file)
  user=$(env_value AUTH_USER)
  pass=$(env_value AUTH_PASS)
  urls=$(desktop_urls)

  printf '\n'
  rule
  # The version can be missing — in Docker mode it comes from asking a container
  # that was created seconds ago, and that can time out. Say the rest anyway
  # rather than printing a sentence with a hole in it.
  if [ -n "$version" ]; then
    printf '\033[32m  SentinelDesk %s is installed and running.\033[0m\n\n' "$version"
  else
    printf '\033[32m  SentinelDesk is installed and running.\033[0m\n\n'
  fi

  if [ "$(printf '%s\n' "$urls" | wc -l)" -gt 1 ]; then
    echo "  Open it in a browser — every address below reaches this machine, so"
    echo "  use whichever one the computer you are sitting at can see:"
  else
    echo "  Open it in a browser:"
  fi
  echo
  printf '%s\n' "$urls" | sed 's/^/    * /'
  echo

  # Both empty is a legal configuration (an open desktop) and has to read as a
  # deliberate one here, not as a summary that failed to find the password.
  if [ -n "$user" ] && [ -n "$pass" ]; then
    echo "  Sign in with the login generated for this machine:"
    echo
    printf '    user      %s\n' "$user"
    printf '    password  %s\n' "$pass"
    echo
    printf '  Both are kept in %s, which only root can read.\n' "$f"
    echo "  Change them there and restart to use a login of your own."
  else
    echo "  This desktop has NO login: AUTH_USER and AUTH_PASS are empty in"
    printf '  %s, so anyone who can reach the addresses above is in.\n' "$f"
    echo "  Set both and restart before this machine is on an untrusted network."
  fi
  echo

  if [ -z "$TLS_DOMAIN" ] && [ "$(desktop_scheme)" = https ]; then
    echo "  The certificate is self-signed, so the browser will warn about it the"
    echo "  first time on each machine. That is expected — continue past the"
    echo "  warning. The guide explains how to make the warning go away."
    echo
  fi

  if [ "$MODE" = docker ]; then
    echo "  Manage it:"
    printf '    docker compose -p sentineldesk -f %s/docker-compose.yml ps\n' "$OPT"
    printf '    docker compose -p sentineldesk -f %s/docker-compose.yml logs -f\n' "$OPT"
  else
    echo "  Manage it:"
    echo "    systemctl status sentineldesk"
    echo "    journalctl -u sentineldesk -f"
  fi
  echo
  printf '  User guide: %s\n' "$GUIDE_URL"
  echo
  echo "  Thank you for installing SentinelDesk."
  rule
  printf '\n'
}


# --- Caddy in front, for a real certificate ----------------------------------
#
# The backend's own TLS is self-signed: fine for a VPS you reach by IP, and it
# warns every visitor, which is not fine for anything with an audience. Caddy
# gets a Let's Encrypt certificate nobody questions and renews it on its own —
# and that renewal is the whole point. Certificates last 90 days and are renewed
# at 60; the backend reads its certificate once at startup, so doing this with
# certbot means a copy and a restart every two months, forever, and a hook that
# fails silently leaves an expired site.
#
# ACME validates a NAME. A bare IP cannot be certified this way, which is worth
# saying because "I have a public IP" is the usual reason people ask. A wildcard
# DNS service turns an IP into a name at no cost — 203-0-113-45.sslip.io — and
# Let's Encrypt is perfectly happy with that.
#
# The Caddyfile is the repository's own, extracted from the binary a moment ago,
# rather than a second copy invented here. It is the same reverse_proxy the
# Docker profile uses.
install_caddy_native() {
  say "putting Caddy in front for $TLS_DOMAIN…"
  apt-get install -y -qq caddy || die "could not install caddy"

  local src="$OPT/deploy/Caddyfile.auto"
  [ -f "$src" ] || die "missing $src (was the deploy tree extracted?)"
  # Somebody else's Caddyfile is not ours to throw away.
  if [ -f /etc/caddy/Caddyfile ] && [ ! -f /etc/caddy/Caddyfile.before-sentineldesk ]; then
    cp /etc/caddy/Caddyfile /etc/caddy/Caddyfile.before-sentineldesk
    say "kept the previous Caddyfile as /etc/caddy/Caddyfile.before-sentineldesk"
  fi
  {
    # Global options first, and only when there is something to put in them.
    [ -n "$TLS_EMAIL" ] && printf '{\n\temail %s\n}\n\n' "$TLS_EMAIL"
    cat "$src"
  } > /etc/caddy/Caddyfile

  # Caddyfile.auto expands {$DOMAIN}, and Debian's caddy.service reads no
  # environment file, so it arrives as a drop-in.
  mkdir -p /etc/systemd/system/caddy.service.d
  cat > /etc/systemd/system/caddy.service.d/sentineldesk.conf <<EOF
[Service]
Environment=DOMAIN=$TLS_DOMAIN
EOF
  systemctl daemon-reload
  systemctl enable caddy >/dev/null 2>&1 || true
  systemctl restart caddy

  say "Caddy is up. Ports 80 and 443 must reach this machine, or Let's Encrypt"
  say "  cannot validate $TLS_DOMAIN and will keep retrying: journalctl -u caddy -f"
}

# Upsert into /etc/sentineldesk/env. That file is written once and then belongs
# to whoever operates the machine, so it is never rewritten wholesale — but a
# flag that changes how the thing listens has to be able to correct it.
set_env() {
  local k="$1" v="$2" f=/etc/sentineldesk/env
  [ -f "$f" ] || return 0
  if grep -q "^$k=" "$f"; then
    sed -i "s|^$k=.*|$k=$v|" "$f"
  else
    printf '%s=%s\n' "$k" "$v" >> "$f"
  fi
}

# --- the wallpapers ----------------------------------------------------------
#
# Deliberately NOT embedded in the binary: they are ~23 MB of PNG against a
# 14 MB binary, so carrying them would nearly triple a download every install
# pulls over the network, in order to ship decoration. Fetched here instead,
# and best effort by design — the built-in fallback is rendered from an SVG at
# install time and the desktop is perfectly fine with only it.
#
# A directory that already has files is left alone: somebody may have put their
# own there, and re-running the installer must not overwrite them.
#
# Numbered rather than listed, so adding a seventh image needs no change here,
# and stopping after three consecutive misses so one gap cannot truncate the set.
fetch_wallpapers() {
  local dir="$1"
  mkdir -p "$dir"
  if [ -n "$(ls -A "$dir" 2>/dev/null)" ]; then
    say "wallpapers: $dir already has files, leaving them alone"
    return 0
  fi
  local base="https://raw.githubusercontent.com/$REPO/main/wallpaper"
  local got=0

  # Ask the repository what is actually in there, so a wallpaper added tomorrow
  # arrives without touching this script and WITHOUT having to be named to a
  # pattern. One unauthenticated API call, well inside the 60-per-hour limit.
  local urls
  urls=$(curl -fsSL "https://api.github.com/repos/$REPO/contents/wallpaper" 2>/dev/null \
         | grep -o '"download_url": *"[^"]*"' | cut -d'"' -f4 \
         | grep -iE '\.(png|jpg|jpeg|webp)$' || true)

  if [ -n "$urls" ]; then
    for u in $urls; do
      if curl -fsSL -o "$dir/$(basename "$u")" "$u" 2>/dev/null; then
        got=$((got + 1))
      else
        rm -f "$dir/$(basename "$u")"
      fi
    done
  else
    # The API was unreachable or rate-limited. Fall back to probing the naming
    # convention over raw.githubusercontent, which needs no API at all: three
    # consecutive misses end it, so one gap in the numbering cannot truncate
    # the set.
    local miss=0 i=1
    while [ "$i" -le 30 ] && [ "$miss" -lt 3 ]; do
      if curl -fsSL -o "$dir/sentineldesk-wallpaper-$i.png" \
              "$base/sentineldesk-wallpaper-$i.png" 2>/dev/null; then
        got=$((got + 1)); miss=0
      else
        rm -f "$dir/sentineldesk-wallpaper-$i.png"; miss=$((miss + 1))
      fi
      i=$((i + 1))
    done
  fi
  if [ "$got" -gt 0 ]; then
    say "wallpapers: $got images in $dir (rotating every WALLPAPER_ROTATE_SECS, default 300 s)"
  else
    warn "could not fetch the wallpapers; the built-in one is still installed"
  fi
}

# --- the configuration the binary carries -----------------------------------
# The deploy tree comes out of the binary, which means something has to be able
# to RUN the binary — and it links GStreamer dynamically. Native mode calls this
# after the engine packages are installed, so the host binary works. Docker mode
# never installs them, on purpose: the whole point of Docker mode is that the
# host stays clean. Running the host binary there failed outright.
#
#   /usr/local/bin/sentineldesk: error while loading shared libraries:
#   libgstreamer-1.0.so.0: cannot open shared object file
#
# and with `set -e` that ended the install, on any host without GStreamer —
# which is every host Docker mode is meant for. So Docker mode extracts from
# inside the image, which has the libraries by definition and is the same build.
extract_deploy() {
  mkdir -p "$OPT"
  if [ "$MODE" = docker ]; then
    docker run --rm --entrypoint /usr/local/bin/sentineldesk \
      -v "$OPT:/out" "$IMAGE:$IMAGE_TAG" -extract-deploy /out
  else
    "$BIN" -extract-deploy "$OPT"
  fi
}

# =============================================================================
# Docker
# =============================================================================
install_docker_mode() {
  # lite ships as :latest as well, so the common case pulls the tag people
  # already have cached; full has a tag of its own.
  IMAGE_TAG="latest"
  [ "$VARIANT" = full ] && IMAGE_TAG="full"
  say "variant: $VARIANT ($IMAGE:$IMAGE_TAG)"

  if ! command -v docker >/dev/null 2>&1; then
    say "installing Docker (get.docker.com)…"
    curl -fsSL https://get.docker.com | sh
  fi

  extract_deploy
  mkdir -p "$OPT"
  fetch_wallpapers "$OPT/wallpaper"

  # A compose file of our own rather than the repository's: that one BUILDS the
  # image from source, which needs the whole repo. An installed machine pulls.
  if [ ! -f "$OPT/.env" ]; then
    local pass; pass=$(head -c9 /dev/urandom | base64 | tr -d '/+=')
    cat > "$OPT/.env" <<EOF
AUTH_USER=admin
AUTH_PASS=$pass
TURN_USER=webrtc
TURN_PASS=$(head -c9 /dev/urandom | base64 | tr -d '/+=')
EOF
    chmod 600 "$OPT/.env"
    say "credentials generated in $OPT/.env"
  fi

  # The gamepad needs /dev/uinput and the VPN needs /dev/net/tun, and they go in
  # ONE list. They used to be two: the heredoc wrote /dev/net/tun and a sed added
  # /dev/uinput afterwards as a SECOND `devices:` key. That is not an override —
  # compose refuses a duplicate mapping key outright:
  #
  #   mapping key "devices" already defined at line 6
  #
  # So every host that has /dev/uinput, which is most of them, ended up with a
  # compose file that would not parse and an install that died on its last step.
  # The device is still only mapped where it exists, because compose also
  # refuses to start when a mapped device is missing.
  DEVICES='"/dev/net/tun"'
  [ -e /dev/uinput ] && DEVICES="$DEVICES, \"/dev/uinput:/dev/uinput\""

  cat > "$OPT/docker-compose.yml" <<EOF
name: sentineldesk
services:
  sentineldesk:
    image: $IMAGE:$IMAGE_TAG
    container_name: sentineldesk
    environment:
      - AUTH_USER=\${AUTH_USER}
      - AUTH_PASS=\${AUTH_PASS}
      - ENCODER=auto
      - VIDEO_BITRATE_KBPS=8000
      - WEBRTC_MIN_PORT=59000
      - WEBRTC_MAX_PORT=59049
      - CLIENT_STUN=stun:stun.l.google.com:19302
      # TLS by default — see host_addresses() for why the IPs are listed. Set
      # TLS_SELFSIGNED=0 in .env for plain HTTP on a trusted network.
      - TLS_SELFSIGNED=\${TLS_SELFSIGNED:-1}
      - TLS_HOSTS=\${TLS_HOSTS:-$(host_addresses)}
      # Read at start, so this one image serves every region. Edit and restart.
      - TZ=\${TZ:-America/Argentina/Buenos_Aires}
      - KEYBOARD_LAYOUT=\${KEYBOARD_LAYOUT:-us}
    ports:
      - "8080:8080"
      - "59000-59049:59000-59049/udp"
    volumes:
      - sentineldesk-home:/home/sentineldesk
      # Drop your own images in here and they join the rotation.
      - $OPT/wallpaper:/wallpaper:ro
    # A VPN client needs a tunnel device and the right to configure routes.
    # Without both, openvpn installs cleanly and fails when somebody needs it.
    # Remove both on a machine that never dials one — NET_ADMIN lets the
    # container manage its own interfaces, routes and firewall.
    cap_add: [ "NET_ADMIN" ]
    devices: [ $DEVICES ]
    shm_size: "2g"
    restart: unless-stopped
volumes:
  sentineldesk-home:
    name: sentineldesk-home
EOF

  # Pull before starting, and this is what makes re-running an update rather
  # than a no-op: `compose up -d` only fetches an image it does not already
  # have, so on a machine that already ran this, :latest meant "the latest I
  # downloaded in March" and the install reported success against the old one.
  say "pulling $IMAGE:$IMAGE_TAG…"
  docker compose -p sentineldesk -f "$OPT/docker-compose.yml" --env-file "$OPT/.env" pull \
    || warn "pull failed; starting whatever image is already here"

  say "starting…"
  docker compose -p sentineldesk -f "$OPT/docker-compose.yml" --env-file "$OPT/.env" up -d
  # From the container rather than the host binary, which in Docker mode has no
  # GStreamer to load. A few seconds' grace because compose returns as soon as
  # the container is created, not once it answers.
  local now="" i=0
  while [ "$i" -lt 10 ] && [ -z "$now" ]; do
    now=$(docker exec sentineldesk /usr/local/bin/sentineldesk -version 2>/dev/null \
          | sed 's/^sentineldesk //')
    [ -z "$now" ] && sleep 1
    i=$((i + 1))
  done
  report_version "$now"
  say "full compose reference: $OPT/deploy/"
  summary "$now"
}

# =============================================================================
# Native — the container's layout, reproduced on the host
# =============================================================================
install_native_mode() {
  say "installing packages (this is the long part)…"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  # The engine: X, audio, GStreamer and the accessibility bridge. This half
  # stays here because it is what the BINARY needs to run at all, and it has to
  # be installed before the deploy tree is even extracted.
  apt-get install -y -qq --no-install-recommends \
    xvfb x11-utils x11-xserver-utils xdotool wmctrl xfonts-base \
    openbox xterm fonts-dejavu-core \
    pulseaudio pulseaudio-utils \
    gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly \
    gstreamer1.0-pulseaudio gstreamer1.0-libav \
    libva2 mesa-va-drivers \
    openssh-client xclip librsvg2-common dbus dbus-x11 \
    at-spi2-core libatk-adaptor python3-pyatspi gir1.2-atspi-2.0 \
    supervisor procps ca-certificates sudo tzdata xkb-data

  # The deploy tree comes out of the binary now rather than further down,
  # because the package lists live in it and the next step reads them. This is
  # also the first moment the binary can run at all: it links GStreamer, which
  # the block above just installed.
  extract_deploy
  report_version "$("$BIN" -version 2>/dev/null | sed 's/^sentineldesk //')"

  # The desktop itself comes from the same lists the container image is built
  # from. Duplicating them here is what would let a native install and a
  # container install drift apart — silently, and only visible to whoever hits
  # the missing tool.
  local lists="$OPT/deploy/packages/desktop.txt"
  [ "$VARIANT" = full ] && lists="$lists $OPT/deploy/packages/full.txt"
  if [ "$VARIANT" = full ] && [ "$(dpkg --print-architecture)" = amd64 ]; then
    lists="$lists $OPT/deploy/packages/full-amd64.txt"
    sed -i 's/^Components: .*/Components: main contrib non-free non-free-firmware/' \
        /etc/apt/sources.list.d/debian.sources 2>/dev/null || true
    dpkg --add-architecture i386
    echo "steam steam/question select I AGREE" | debconf-set-selections
    echo "steam steam/license note ''" | debconf-set-selections
    apt-get update -qq
  fi
  say "installing the $VARIANT desktop…"
  # shellcheck disable=SC2046
  apt-get install -y -qq --no-install-recommends \
    $(sed -e 's/#.*//' -e '/^[[:space:]]*$/d' $lists)

  # The MIME-type -> application map. desktop-file-utils arrives with the list
  # above, but the cache it builds does not: with --no-install-recommends no
  # package trigger runs, exactly as in the Dockerfile, which builds it by hand
  # for the same reason. Without it lxpanel rejects the file manager on every
  # start — "the pcmanfm.desktop is not valid desktop id of file manager".
  update-desktop-database /usr/share/applications 2>/dev/null || true

  # The desktop's own user, on whatever uid is free.
  #
  # This used to demand uid 1000 and refuse the install when it was taken, which
  # in practice meant refusing on any machine with a person on it: useradd hands
  # 1000 to the first real user on Debian, Ubuntu and Raspberry Pi OS alike, so
  # native mode worked only on a box with nothing but root. Every Raspberry Pi
  # was excluded by a number.
  #
  # What was actually load-bearing is the PATH, /run/user/1000, which the
  # supervisor config now takes as %(ENV_RUNTIME_DIR)s. So the directory follows
  # the uid, and the uid is whatever useradd picks.
  #
  # Not, to be clear, by pointing a second user at /run/user/1000: systemd-logind
  # owns that directory for whoever really is uid 1000 and recreates it on their
  # every login. Sharing it would put PulseAudio and D-Bus in a directory another
  # process rearranges underneath them.
  # --user names an account to run as instead. On a Raspberry Pi that is usually
  # the point: it is your machine, and you want your files, your home and your
  # desktop rather than a second one beside it.
  #
  # Two things follow from handing the desktop an account somebody already uses,
  # and both are handled rather than hoped about:
  #
  #   OWN_HOME=0 stops desktop-init.sh overwriting their lxpanel, lxterminal and
  #   GTK configuration on every boot. It fills in what is missing instead.
  #
  #   enable-linger, because systemd-logind creates /run/user/<uid> when that
  #   person logs in and REMOVES it when their last session ends. Without
  #   linger, closing your last ssh connection takes PulseAudio's socket, D-Bus
  #   and the MCP socket with it, out from under a desktop that is still running.
  #
  # And one thing that cannot be handled, only stated: the desktop is reachable
  # over the network and hands out a shell. Running it as your account means
  # whoever reaches the desktop IS you, sudo and ssh keys included. That is why
  # a dedicated user stays the default.
  OWN_HOME=1
  if [ -n "$WANT_USER" ]; then
    DESKTOP_USER="$WANT_USER"
    if id "$DESKTOP_USER" >/dev/null 2>&1; then
      OWN_HOME=0
      warn "running the desktop as the existing account '$DESKTOP_USER'."
      warn "  anyone who reaches the desktop gets a shell as $DESKTOP_USER — its"
      warn "  sudo rights and ssh keys included. Ctrl-C now to use a dedicated user."
      sleep 5
    else
      useradd -m -s /bin/bash "$DESKTOP_USER"
    fi
  else
    DESKTOP_USER=sentineldesk
    id "$DESKTOP_USER" >/dev/null 2>&1 || useradd -m -s /bin/bash "$DESKTOP_USER"
  fi

  SD_UID=$(id -u "$DESKTOP_USER")
  DESKTOP_HOME=$(getent passwd "$DESKTOP_USER" | cut -d: -f6)
  [ -n "$DESKTOP_HOME" ] || die "could not resolve the home of '$DESKTOP_USER'"
  RUNTIME_DIR="/run/user/$SD_UID"
  say "desktop user: $DESKTOP_USER (uid $SD_UID) · home $DESKTOP_HOME · runtime $RUNTIME_DIR"

  # Linger, and ONLY for an account a person also logs into.
  #
  # It was applied to both cases on the reasoning that it "costs nothing" for an
  # account nobody logs into. It costs something: linger starts that user's
  # systemd manager, the manager socket-activates Debian's pulseaudio.socket,
  # and that PulseAudio takes the pid file before supervisord's does. The result
  # on a bare VPS was
  #
  #   E: [pulseaudio] pid.c: Daemon already running.
  #   gave up: pulseaudio entered FATAL state
  #
  # on a desktop where nothing else was wrong. An account we created has nobody
  # logging in, so logind never touches its runtime directory and linger buys
  # nothing at all.
  #
  # An existing account still needs it: there logind DOES create and destroy
  # /run/user/<uid> around that person's sessions, and without linger closing
  # the last ssh takes the desktop's sockets with it.
  if command -v loginctl >/dev/null 2>&1; then
    if [ "$OWN_HOME" = 0 ]; then
      loginctl enable-linger "$DESKTOP_USER" 2>/dev/null \
        || warn "could not enable linger for $DESKTOP_USER; if the desktop loses audio after you log out, that is why"
    else
      # Actively turned OFF, not merely left alone: an earlier version of this
      # script enabled it here, and a machine installed with that one keeps a
      # user manager running — and a PulseAudio with it — until something says
      # otherwise. Stopping the manager too, because masking a unit does not
      # stop the copy that is already running.
      loginctl disable-linger "$DESKTOP_USER" 2>/dev/null || true
      systemctl stop "user@$SD_UID.service" 2>/dev/null || true
    fi
  fi

  # And whichever case this is, make sure the desktop's PulseAudio is the only
  # one. On an existing account the person logging in starts their user manager
  # regardless of linger, so masking is what actually prevents the collision;
  # /dev/null symlinks are the mask, written directly because the account may
  # have no session for `systemctl --user` to talk to.
  install -d -o "$DESKTOP_USER" -g "$DESKTOP_USER" -m 700 "$DESKTOP_HOME/.config/systemd/user"
  for u in pulseaudio.socket pulseaudio.service; do
    ln -sf /dev/null "$DESKTOP_HOME/.config/systemd/user/$u"
    chown -h "$DESKTOP_USER:" "$DESKTOP_HOME/.config/systemd/user/$u" 2>/dev/null || true
  done
  usermod -aG video sentineldesk 2>/dev/null || true
  usermod -aG render sentineldesk 2>/dev/null || true

  local D="$OPT/deploy"

  # Helper scripts land where the supervisor config already points.
  install -m 0755 "$D"/config/wait-x11.sh "$D"/config/wait-wm.sh \
                  "$D"/config/mkinput.sh /usr/local/bin/
  install -m 0755 "$D"/desktop/desktop-init.sh "$D"/desktop/wallpaper-rotate.sh /usr/local/bin/
  for f in vnc-connect rdp-connect sentineldesk-hint openvpn-connect; do
    [ -f "$D/desktop/$f" ] && install -m 0755 "$D/desktop/$f" /usr/local/bin/
  done

  # --- the desktop's own appearance and behaviour ------------------------------
  #
  # Everything below is what the Dockerfile puts on the system and this script
  # did not, which made a native install a materially different product from the
  # container rather than the same one without a runtime. wallpaper-rotate.sh
  # above is the sharpest example: supervisord.conf lists it as a program, so
  # its absence was a FATAL service on every native install, every boot.
  #
  # The rest was quieter and worse. No chromium-flags.conf means no
  # --remote-debugging-port, which means all eight browser_* MCP tools fail on a
  # native install and only there. No skel tree means no panel layout, no
  # terminal profile and no GTK theme. No openbox theme means the window frames
  # are Clearlooks.
  install -D -m 0644 "$D"/desktop/openbox-themerc \
                     /usr/share/themes/SentinelDesk/openbox-3/themerc
  install -D -m 0644 "$D"/desktop/chromium-flags.conf /etc/chromium.d/90-sentineldesk
  # GTK reads system settings from the XDG paths, not /etc/gtk-*
  install -D -m 0644 "$D"/desktop/gtk-settings.ini /etc/xdg/gtk-3.0/settings.ini
  install -D -m 0644 "$D"/desktop/gtk-settings.ini /etc/xdg/gtk-4.0/settings.ini
  install -D -m 0644 "$D"/desktop/gtkrc-2.0        /etc/gtk-2.0/gtkrc

  # Per-user configuration, staged where desktop-init.sh looks for it. It is
  # copied into the home at every start — over the top when the desktop owns
  # that home, and with -n when it was handed somebody's existing account.
  install -D -m 0644 "$D"/desktop/lxpanel-panel \
                     /etc/skel.sentineldesk/.config/lxpanel/LXDE/panels/panel
  install -D -m 0644 "$D"/desktop/lxpanel-config \
                     /etc/skel.sentineldesk/.config/lxpanel/LXDE/config
  install -D -m 0644 "$D"/desktop/lxterminal.conf \
                     /etc/skel.sentineldesk/.config/lxterminal/lxterminal.conf
  install -D -m 0644 "$D"/desktop/gtk-settings.ini \
                     /etc/skel.sentineldesk/.config/gtk-3.0/settings.ini
  install -D -m 0644 "$D"/desktop/gtkrc-2.0 /etc/skel.sentineldesk/.gtkrc-2.0

  # Both halves are now on disk — the panel that names the icons and the themes
  # that are meant to carry them — which is the only moment the two can be
  # compared.
  check_panel_icons

  # The fallback wallpaper, rendered at install time exactly as the image builds
  # it, plus the directory wallpaper-rotate.sh reads. Without both, the desktop
  # comes up on whatever pcmanfm defaults to.
  mkdir -p /usr/share/backgrounds
  rsvg-convert -w 1920 -h 1080 -o /usr/share/backgrounds/sentineldesk.png \
               "$D"/desktop/wallpaper.svg 2>/dev/null \
    || warn "could not render the fallback wallpaper (librsvg2-bin missing?)"
  # /wallpaper is where wallpaper-rotate.sh looks by default. Empty, it exits
  # immediately and the desktop sits on the fallback above — which is what a
  # native install did until now.
  fetch_wallpapers /wallpaper

  # Openbox: select the theme, and put title bars in the same monospace as the
  # control layer. Matched by place because rc.xml carries several <font>
  # blocks and only these two draw a window title.
  if [ -f /etc/xdg/openbox/rc.xml ]; then
    sed -i 's|<name>Clearlooks</name>|<name>SentinelDesk</name>|' /etc/xdg/openbox/rc.xml
    python3 -c "import re; p='/etc/xdg/openbox/rc.xml'; s=open(p).read(); s=re.sub(r'(<font place=\"(?:Active|Inactive)Window\">.*?<name>)[^<]*(</name>.*?<size>)[^<]*(</size>.*?<weight>)[^<]*(</weight>)', r'\g<1>monospace\g<2>9\g<3>normal\g<4>', s, flags=re.S); open(p,'w').write(s)" \
      2>/dev/null || true
  fi
  fc-cache -f >/dev/null 2>&1 || true
  install -m 0644 "$D"/config/supervisord.conf /etc/supervisor/sentineldesk.conf
  mkdir -p /etc/pulse
  install -m 0644 "$D"/config/pulse-daemon.pa /etc/pulse/sentineldesk.pa
  # PulseAudio's client-side default, so a process that did not inherit
  # PULSE_SERVER from supervisord still finds the socket. The image gets this
  # from the Dockerfile as a fixed path; here it has to be written, because the
  # path follows whatever uid the desktop's user ended up on.
  mkdir -p /etc/pulse/client.conf.d
  printf 'default-server = unix:%s/pulse/native\n' "$RUNTIME_DIR" \
    > /etc/pulse/client.conf.d/sentineldesk.conf
  install -m 0644 "$D"/desktop/shell-report.sh /etc/profile.d/99-sentineldesk-report.sh

  # Defaults, all overridable here and nowhere else.
  mkdir -p /etc/sentineldesk
  if [ ! -f /etc/sentineldesk/env ]; then
    local pass; pass=$(head -c9 /dev/urandom | base64 | tr -d '/+=')
    cat > /etc/sentineldesk/env <<EOF
DISPLAY_WIDTH=1920
DISPLAY_HEIGHT=1080
FPS=30
ENCODER=auto
VIDEO_BITRATE_KBPS=8000
AUTH_USER=admin
AUTH_PASS=$pass
MCP_SOCK=$RUNTIME_DIR/sentineldesk-mcp.sock
FILES_ROOT=$DESKTOP_HOME
TLS_SELFSIGNED=1
TLS_DIR=$DESKTOP_HOME/.tls
TLS_HOSTS=$(host_addresses)
EOF
    chmod 600 /etc/sentineldesk/env
    say "credentials generated in /etc/sentineldesk/env"
  fi

  # Who the desktop runs as, in a file of its own and rewritten on every
  # install. Deliberately NOT in /etc/sentineldesk/env: that one is written once
  # and then belongs to whoever operates the machine — resolution, bitrate,
  # credentials, things they edit. This is identity, the installer owns it, and
  # a reinstall onto a different account has to be able to correct it.
  cat > /etc/sentineldesk/user <<EOF
DESKTOP_USER=$DESKTOP_USER
DESKTOP_HOME=$DESKTOP_HOME
DESKTOP_OWN_HOME=$OWN_HOME
EOF
  chmod 644 /etc/sentineldesk/user

  # A host entrypoint, NOT the container's. The container one sets the root
  # password and rewrites /etc/machine-id — correct inside an image it owns,
  # unforgivable on somebody's actual machine.
  cat > /usr/local/bin/sentineldesk-session <<'EOF'
#!/bin/bash
set -e

# Half a login is a typo, not a configuration — the same check the container's
# entrypoint makes, for the same reason and with the same wording. Setting one
# of AUTH_USER/AUTH_PASS and not the other used to mean "no authentication",
# which is never what somebody who typed one of them wanted. Both empty stays
# legal. Refusing here rather than in the binary matters because supervisord
# restarts what it supervises: the server alone would refuse and be restarted
# forever, while systemd reported the unit as running.
if { [ -n "${AUTH_USER:-}" ] && [ -z "${AUTH_PASS:-}" ]; } \
   || { [ -z "${AUTH_USER:-}" ] && [ -n "${AUTH_PASS:-}" ]; }; then
    if [ -n "${AUTH_USER:-}" ]; then set_var=AUTH_USER; missing=AUTH_PASS
    else set_var=AUTH_PASS; missing=AUTH_USER; fi
    echo "sentineldesk: $set_var is set but $missing is empty: refusing to start with half a login." >&2
    echo "sentineldesk: set both in /etc/sentineldesk/env, or neither for an open desktop." >&2
    exit 1
fi

# Who the desktop runs as, and where. supervisord.conf reads all three as
# %(ENV_…)s; without them it refuses to start and names the one it is missing.
. /etc/sentineldesk/user
export DESKTOP_USER DESKTOP_HOME DESKTOP_OWN_HOME

# The uid is looked up here rather than baked in when the installer wrote this
# file: if the account is ever recreated on a different number, the session
# follows it instead of pointing at a stale path.
RUNTIME_DIR="/run/user/$(id -u "$DESKTOP_USER")"
export RUNTIME_DIR
mkdir -p "$RUNTIME_DIR"
# "$USER:" gives the user's own default group, which is not always a group of
# the same name — an existing account may well have been put in another one.
chown "$DESKTOP_USER:" "$RUNTIME_DIR"
chmod 700 "$RUNTIME_DIR"
mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix

# lxpanel's stderr goes to a file here instead of the journal — see the comment
# on [program:lxpanel] in supervisord.conf. supervisord refuses to start a
# program whose logfile it cannot create, so without this directory the panel
# never comes up at all.
mkdir -p /var/log/sentineldesk
rm -f /tmp/.X0-lock /tmp/.X11-unix/X0 2>/dev/null || true
rm -f /tmp/supervisord.pid /tmp/supervisor.sock 2>/dev/null || true
/usr/local/bin/mkinput.sh &
exec /usr/bin/supervisord -n -c /etc/supervisor/sentineldesk.conf
EOF
  chmod 0755 /usr/local/bin/sentineldesk-session

  cat > /etc/systemd/system/sentineldesk.service <<'EOF'
[Unit]
Description=SentinelDesk — collaborative desktop for people and AI agents
After=network.target

[Service]
Type=exec
EnvironmentFile=/etc/sentineldesk/env
# Piped through cat on purpose, and this is not cosmetic.
#
# supervisord.conf sends every program's output to /dev/fd/1 so that `docker
# logs` shows it. Under systemd that file descriptor is a socket to journald,
# and open() on a socket through /proc/self/fd fails with ENXIO — so ALL TEN
# programs died at startup with "unknown error making dispatchers", on a
# service systemd itself reported as active and running.
#
# `| cat` makes fd 1 an ordinary pipe, which is exactly what it is inside a
# container, so the same config works in both places and journalctl still gets
# everything. The alternative was a second logging scheme for native installs;
# one shared supervisord.conf is worth more than avoiding a `cat`.
ExecStart=/bin/sh -c 'exec /usr/local/bin/sentineldesk-session 2>&1 | cat'
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

  # Caddy in front means the backend must stop terminating TLS itself AND stop
  # answering on every interface — otherwise :8080 stays open in the clear and
  # the proxy is decoration. HTTP_ADDR is what confines it to the loopback the
  # proxy reaches it on.
  if [ -n "$TLS_DOMAIN" ]; then
    install_caddy_native
    set_env TLS_SELFSIGNED 0
    set_env HTTP_ADDR 127.0.0.1
  fi

  systemctl daemon-reload
  systemctl enable sentineldesk.service
  # restart, not `enable --now`. --now starts a stopped service and does
  # NOTHING to a running one, so re-running this script to pick up a new release
  # rewrote the binary, the unit and every config file — and then left the old
  # process running while printing "SentinelDesk is up". Re-running is the
  # update path, so it has to end with the new version actually running.
  systemctl restart sentineldesk.service
  # Asked of the binary that is now installed, not of PREV_VERSION: this runs
  # after the restart, so it is the version actually serving.
  summary "$("$BIN" -version 2>/dev/null | sed 's/^sentineldesk //')"
}

# --- go ----------------------------------------------------------------------
detect_previous_version
install_binary
case "$MODE" in
  docker) install_docker_mode ;;
  native) install_native_mode ;;
esac
