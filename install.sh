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
    -h|--help) sed -n '14,37p' "$0"; exit 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
  shift
done

# --- preconditions -----------------------------------------------------------
[ "$(id -u)" = 0 ] || die "run as root: sudo $0 ${MODE:-}"

case "$(uname -m)" in
  x86_64)  ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m) (amd64 and arm64 only)" ;;
esac

# Debian 13 is what the binary is built against (the same base as the Docker
# image), and what the package names below belong to. Raspberry Pi OS is Debian
# under a different name.
if [ -r /etc/os-release ]; then
  . /etc/os-release
  case "${ID:-}:${VERSION_ID:-}" in
    debian:13|raspbian:13) ;;
    *) warn "this targets Debian 13 (trixie); found ${PRETTY_NAME:-unknown}."
       read -rp "  Continue anyway? [y/N] " a; [ "${a,,}" = y ] || exit 1 ;;
  esac
fi

# --- mode --------------------------------------------------------------------
if [ -z "$MODE" ]; then
  echo "How should SentinelDesk run on this machine?"
  echo "  1) Docker    — isolated, easy to remove, the recommended default"
  echo "  2) Native    — straight on the host: systemd + supervisor, no container"
  read -rp "Choice [1/2]: " c
  case "$c" in 2) MODE=native ;; *) MODE=docker ;; esac
elif [ "$MODE" = auto ]; then
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

# The address to print at the end. Reads the scheme back from what was actually
# configured rather than assuming: an existing /etc/sentineldesk/env is left
# untouched by design, so a machine installed before TLS became the default is
# still on http:// and should be told so.
desktop_url() {
  local scheme=https ip
  ip=$(hostname -I 2>/dev/null | awk '{print $1}')
  if [ "$MODE" = native ] && [ -r /etc/sentineldesk/env ]; then
    grep -q '^TLS_SELFSIGNED=1' /etc/sentineldesk/env || scheme=http
  fi
  echo "$scheme://${ip:-localhost}:8080"
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
    say "credentials generated in $OPT/.env (user admin, password $pass)"
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
  say "SentinelDesk is up: $(desktop_url)"
  say "credentials: $OPT/.env · full compose reference: $OPT/deploy/"
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
    say "credentials generated in /etc/sentineldesk/env (user admin, password $pass)"
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

  systemctl daemon-reload
  systemctl enable sentineldesk.service
  # restart, not `enable --now`. --now starts a stopped service and does
  # NOTHING to a running one, so re-running this script to pick up a new release
  # rewrote the binary, the unit and every config file — and then left the old
  # process running while printing "SentinelDesk is up". Re-running is the
  # update path, so it has to end with the new version actually running.
  systemctl restart sentineldesk.service
  say "SentinelDesk is up: $(desktop_url)"
  say "credentials: /etc/sentineldesk/env"
  say "service:     systemctl status sentineldesk · journalctl -u sentineldesk -f"
}

# --- go ----------------------------------------------------------------------
detect_previous_version
install_binary
case "$MODE" in
  docker) install_docker_mode ;;
  native) install_native_mode ;;
esac
