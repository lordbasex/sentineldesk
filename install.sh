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
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
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
  say "installed: $($BIN -version)"
}

# --- the configuration the binary carries -----------------------------------
extract_deploy() {
  mkdir -p "$OPT"
  "$BIN" -extract-deploy "$OPT"
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
      # Read at start, so this one image serves every region. Edit and restart.
      - TZ=\${TZ:-UTC}
      - KEYBOARD_LAYOUT=\${KEYBOARD_LAYOUT:-us}
    ports:
      - "8080:8080"
      - "59000-59049:59000-59049/udp"
    volumes:
      - sentineldesk-home:/home/sentineldesk
    # A VPN client needs a tunnel device and the right to configure routes.
    # Without both, openvpn installs cleanly and fails when somebody needs it.
    # Remove both on a machine that never dials one — NET_ADMIN lets the
    # container manage its own interfaces, routes and firewall.
    cap_add: [ "NET_ADMIN" ]
    devices: [ "/dev/net/tun" ]
    shm_size: "2g"
    restart: unless-stopped
volumes:
  sentineldesk-home:
    name: sentineldesk-home
EOF
  # The gamepad needs /dev/uinput; add the device only where it exists, because
  # compose refuses to start when a mapped device is missing.
  [ -e /dev/uinput ] && sed -i 's|    shm_size: "2g"|    devices: [ "/dev/uinput:/dev/uinput" ]\n    shm_size: "2g"|' "$OPT/docker-compose.yml"

  say "starting…"
  docker compose -p sentineldesk -f "$OPT/docker-compose.yml" --env-file "$OPT/.env" up -d
  say "SentinelDesk is up: http://$(hostname -I 2>/dev/null | awk '{print $1}'):8080"
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
  # because the package lists live in it and the next step reads them.
  extract_deploy

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

  # uid 1000 is load-bearing: the supervisor config and the MCP socket path
  # both say /run/user/1000. If 1000 is somebody else, stop rather than run
  # the desktop as them.
  if ! id sentineldesk >/dev/null 2>&1; then
    if id -nu 1000 >/dev/null 2>&1; then
      die "uid 1000 is taken by '$(id -nu 1000)'. The desktop's config expects sentineldesk at uid 1000; free it or install in Docker mode."
    fi
    useradd -m -u 1000 -s /bin/bash sentineldesk
  fi
  usermod -aG video sentineldesk 2>/dev/null || true
  usermod -aG render sentineldesk 2>/dev/null || true

  local D="$OPT/deploy"

  # Helper scripts land where the supervisor config already points.
  install -m 0755 "$D"/config/wait-x11.sh "$D"/config/wait-wm.sh \
                  "$D"/config/mkinput.sh /usr/local/bin/
  install -m 0755 "$D"/desktop/desktop-init.sh /usr/local/bin/
  for f in "$D"/desktop/vnc-connect "$D"/desktop/rdp-connect; do
    [ -f "$f" ] && install -m 0755 "$f" /usr/local/bin/
  done
  install -m 0644 "$D"/config/supervisord.conf /etc/supervisor/sentineldesk.conf
  mkdir -p /etc/pulse
  install -m 0644 "$D"/config/pulse-daemon.pa /etc/pulse/sentineldesk.pa
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
MCP_SOCK=/run/user/1000/sentineldesk-mcp.sock
FILES_ROOT=/home/sentineldesk
EOF
    chmod 600 /etc/sentineldesk/env
    say "credentials generated in /etc/sentineldesk/env (user admin, password $pass)"
  fi

  # A host entrypoint, NOT the container's. The container one sets the root
  # password and rewrites /etc/machine-id — correct inside an image it owns,
  # unforgivable on somebody's actual machine.
  cat > /usr/local/bin/sentineldesk-session <<'EOF'
#!/bin/bash
set -e
mkdir -p /run/user/1000
chown sentineldesk:sentineldesk /run/user/1000
chmod 700 /run/user/1000
mkdir -p /tmp/.X11-unix && chmod 1777 /tmp/.X11-unix
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
ExecStart=/usr/local/bin/sentineldesk-session
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable --now sentineldesk.service
  say "SentinelDesk is up: http://$(hostname -I 2>/dev/null | awk '{print $1}'):8080"
  say "credentials: /etc/sentineldesk/env"
  say "service:     systemctl status sentineldesk · journalctl -u sentineldesk -f"
}

# --- go ----------------------------------------------------------------------
install_binary
case "$MODE" in
  docker) install_docker_mode ;;
  native) install_native_mode ;;
esac
