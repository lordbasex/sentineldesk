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

# SentinelDesk — a Linux desktop streamed to the browser over WebRTC.
#
# Debian 13 (trixie). A single Go binary (Pion WebRTC + go-gst) drives Xvfb,
# Openbox, PulseAudio and GStreamer; the desktop itself is styled after
# Raspberry Pi OS (PIXEL).
#
# Build from the repository root:
#     docker build -f deploy/Dockerfile -t sentineldesk .

# ---------- build stage ----------
FROM golang:1.25-trixie AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config gcc libc6-dev \
    libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
    && rm -rf /var/lib/apt/lists/*

# The version travels in as build args and out as -ldflags -X, so the binary
# can always say what it is (`sentineldesk -version`, and the rail's footer).
# The defaults keep a bare `docker build` honest: it IS a development build.
ARG VERSION=0.0.0
ARG GIT_HASH=development
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# internal/ carries the embedded browser client too (internal/webui/assets),
# and deploy/ is embedded so the binary can serve its own installation files.
COPY cmd/ cmd/
COPY internal/ internal/
COPY deploy/ deploy/
RUN go build -trimpath -ldflags="-s -w \
      -X github.com/lordbasex/sentineldesk/internal/version.Version=${VERSION} \
      -X github.com/lordbasex/sentineldesk/internal/version.GitHash=${GIT_HASH} \
      -X github.com/lordbasex/sentineldesk/internal/version.BuildDate=${BUILD_DATE}" \
      -o /sentineldesk ./cmd/sentineldesk

# ---------- bin: just the binary, for buildx --output ----------
# `make release-binaries` builds this target per platform and copies the file
# out. Scratch, so the local export contains exactly one file.
FROM scratch AS bin
COPY --from=build /sentineldesk /sentineldesk

# ---------- runtime: engine ----------
# Kept as its own layer so that changing the desktop applications below does not
# invalidate the (large, slow) engine layer.
FROM debian:trixie-slim AS engine

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    # Virtual X server and utilities (wmctrl backs the MCP window tools)
    xvfb x11-utils x11-xserver-utils xdotool wmctrl xfonts-base \
    # Window manager
    openbox xterm fonts-dejavu-core \
    # Virtual audio
    pulseaudio pulseaudio-utils \
    # GStreamer: capture and codecs (VP8/H.264/Opus plus the va and nvcodec plugins)
    gstreamer1.0-tools \
    gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good \
    gstreamer1.0-plugins-bad \
    gstreamer1.0-plugins-ugly \
    gstreamer1.0-pulseaudio \
    gstreamer1.0-libav \
    # VA-API (Intel/AMD). On NVIDIA the drivers come from nvidia-container-toolkit.
    libva2 vainfo mesa-va-drivers \
    # Administration utilities
    mc screen vim htop \
    # SSH client (ssh-keygen backs the ssh_* tools)
    openssh-client \
    # FTP/SFTP client
    filezilla \
    # Two-way clipboard (browser <-> desktop)
    xclip \
    # OCR for the read_screen_text / find_text tools, in the three languages the
    # interface itself ships. A language pack does not make OCR good at reading a
    # desktop — that is a different problem, and read_screen_text says so — but
    # without one an error dialog in Portuguese comes back as noise.
    tesseract-ocr tesseract-ocr-eng tesseract-ocr-spa tesseract-ocr-por \
    # gdk-pixbuf SVG loader. Without it GTK cannot use SVG icon themes (Papirus).
    librsvg2-common \
    # System D-Bus. Chromium's CEF uses it to detect the network: without it
    # Steam sits forever on "Waiting for network…" even with connectivity.
    dbus dbus-x11 \
    # AT-SPI: the accessibility tree. This is what lets the ui_* tools KNOW what
    # is on screen (roles, names, states) and invoke actions directly, instead of
    # groping around with screenshots and coordinate clicks.
    # libatk-adaptor is essential: without it GTK apps never register.
    at-spi2-core libatk-adaptor python3-pyatspi gir1.2-atspi-2.0 \
    # Process supervision
    supervisor procps ca-certificates \
    # Privilege escalation: sudo/su for the terminal, polkit so graphical
    # administration tools can request permission instead of failing.
    sudo polkitd pkexec \
    && if [ "$(dpkg --print-architecture)" = "amd64" ]; then \
         apt-get install -y --no-install-recommends intel-media-va-driver; \
       fi \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -m -u 1000 -s /bin/bash sentineldesk \
    && (usermod -aG video,render sentineldesk || usermod -aG video sentineldesk)

# --- Privileges --------------------------------------------------------------
# The container IS the sandbox: the security boundary is the WebSocket login,
# not the inside of the desktop. Blocking root in here protects nothing and does
# prevent what anyone expects from a real desktop — installing a package,
# restarting a service, editing /etc. So `sentineldesk` gets passwordless sudo,
# and `su` works with the password the entrypoint sets.
RUN usermod -aG sudo sentineldesk \
    # SETENV matters: without that tag `sudo -E` is rejected, and a graphical app
    # launched as root loses DISPLAY / PULSE_SERVER and never starts.
    && printf 'sentineldesk ALL=(ALL:ALL) NOPASSWD:SETENV: ALL\nDefaults:sentineldesk !requiretty\nDefaults env_keep += "DISPLAY XAUTHORITY XDG_RUNTIME_DIR PULSE_SERVER DBUS_SESSION_BUS_ADDRESS"\n' \
         > /etc/sudoers.d/010-sentineldesk \
    && chmod 0440 /etc/sudoers.d/010-sentineldesk \
    && visudo -cf /etc/sudoers.d/010-sentineldesk

# polkit: let graphical administration tools (gparted, synaptic, a file manager
# running as root) obtain permission instead of opening a dialog nobody can
# answer — this desktop has no authentication agent.
COPY deploy/config/polkit.rules /etc/polkit-1/rules.d/49-sentineldesk.rules

# NOTE: the sentineldesk binary is NOT copied here. See the end of the desktop
# stage — it changes on every commit, and anything below it in the file would be
# rebuilt with it.
COPY deploy/config/pulse-daemon.pa /etc/pulse/sentineldesk.pa
COPY deploy/config/pulse-client.conf /etc/pulse/client.conf.d/sentineldesk.conf
COPY deploy/config/entrypoint.sh \
     deploy/config/wait-x11.sh \
     deploy/config/wait-wm.sh \
     deploy/config/mkinput.sh \
     deploy/config/a11y.py \
     /usr/local/bin/
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/wait-x11.sh \
             /usr/local/bin/wait-wm.sh /usr/local/bin/mkinput.sh /usr/local/bin/a11y.py

# ---------- runtime: desktop ----------
FROM engine AS desktop

ENV DEBIAN_FRONTEND=noninteractive

# The desktop and the tools that make it worth sitting at.
#
# WHAT gets installed lives in deploy/packages/, not here. Dockerfile has no
# include, and a forty-line RUN with the reasoning wedged into shell comments is
# a list nobody edits twice. A plain text file per variant, one package per
# line, comments where the choice needs defending — and this instruction only
# has to know how to read one.
#
# "Lite" does not mean headless. The desktop IS the product; lite means the
# smallest set that leaves nobody reaching for apt on their first afternoon.
COPY deploy/packages/desktop.txt /tmp/packages/desktop.txt
RUN apt-get update \
    && sed -e 's/#.*//' -e '/^[[:space:]]*$/d' /tmp/packages/desktop.txt \
       | xargs apt-get install -y --no-install-recommends \
    && rm -rf /var/lib/apt/lists/*

COPY deploy/desktop/openbox-themerc /usr/share/themes/SentinelDesk/openbox-3/themerc
COPY deploy/config/supervisord.conf /etc/supervisor/supervisord.conf
COPY deploy/desktop/desktop-init.sh /usr/local/bin/desktop-init.sh
COPY deploy/desktop/wallpaper-rotate.sh /usr/local/bin/wallpaper-rotate.sh
COPY deploy/desktop/vnc-connect deploy/desktop/rdp-connect deploy/desktop/sentineldesk-hint deploy/desktop/openvpn-connect /usr/local/bin/
COPY deploy/desktop/chromium-flags.conf /etc/chromium.d/90-sentineldesk
# GTK3/4 read system settings from the XDG paths (/etc/xdg/gtk-*), not /etc/gtk-*
COPY deploy/desktop/gtk-settings.ini /etc/xdg/gtk-3.0/settings.ini
COPY deploy/desktop/gtk-settings.ini /etc/xdg/gtk-4.0/settings.ini
COPY deploy/desktop/gtkrc-2.0 /etc/gtk-2.0/gtkrc
COPY deploy/desktop/wallpaper.svg /tmp/wallpaper.svg

# Per-user configuration lives in /etc/skel.sentineldesk; desktop-init.sh syncs
# it into the home on every boot, because the home is a persistent volume.
# Exit-status reporting for EVERY interactive shell, not only the ones the agent
# opens. Dropped into profile.d and sourced from the user's bashrc, so a person's
# failed command is as readable to the agent as its own.
COPY deploy/desktop/shell-report.sh /etc/profile.d/99-sentineldesk-report.sh
COPY deploy/desktop/lxpanel-panel  /etc/skel.sentineldesk/.config/lxpanel/LXDE/panels/panel
COPY deploy/desktop/lxpanel-config /etc/skel.sentineldesk/.config/lxpanel/LXDE/config
COPY deploy/desktop/lxterminal.conf /etc/skel.sentineldesk/.config/lxterminal/lxterminal.conf
COPY deploy/desktop/gtk-settings.ini /etc/skel.sentineldesk/.config/gtk-3.0/settings.ini
COPY deploy/desktop/gtkrc-2.0 /etc/skel.sentineldesk/.gtkrc-2.0

# --- our own binary, last ----------------------------------------------------
#
# Deliberately the final layer of this stage, and it is worth saying why: the
# operating system underneath changes about once a month, and this changes on
# every commit. Docker invalidates a layer and everything after it, so putting
# the binary early — where it used to be, up in the engine stage — meant that
# editing one line of Go threw away a gigabyte of installed packages and
# reinstalled them.
#
# Same reasoning, opposite direction, as the package lists sitting in their own
# files: the slow, stable thing goes underneath; the fast, volatile thing goes
# on top.
COPY --from=build /sentineldesk /usr/local/bin/sentineldesk

RUN chmod +x /usr/local/bin/desktop-init.sh /usr/local/bin/wallpaper-rotate.sh \
    /usr/local/bin/vnc-connect \
             /usr/local/bin/rdp-connect /usr/local/bin/sentineldesk-hint /usr/local/bin/openvpn-connect \
    # Fallback wallpaper: rendered SVG -> PNG at build time, no external downloads
    && mkdir -p /usr/share/backgrounds /wallpaper \
    && rsvg-convert -w 1920 -h 1080 -o /usr/share/backgrounds/sentineldesk.png /tmp/wallpaper.svg \
    && rm /tmp/wallpaper.svg \
    # Select the theme in Openbox's global configuration
    && sed -i 's|<name>Clearlooks</name>|<name>SentinelDesk</name>|' /etc/xdg/openbox/rc.xml \
    # Title bars in the same monospace as the control layer. Matched by place
    # because rc.xml carries several <font> blocks and only these two draw a
    # window title.
    && python3 -c "import re; p='/etc/xdg/openbox/rc.xml'; s=open(p).read(); s=re.sub(r'(<font place=\"(?:Active|Inactive)Window\">.*?<name>)[^<]*(</name>.*?<size>)[^<]*(</size>.*?<weight>)[^<]*(</weight>)', r'\g<1>monospace\g<2>9\g<3>normal\g<4>', s, flags=re.S); open(p,'w').write(s)" \
    && fc-cache -f >/dev/null \
    # The MIME-type -> application map. Built here rather than left to a package
    # trigger, because --no-install-recommends means nothing else runs it.
    && update-desktop-database /usr/share/applications

# Defaults; override from docker-compose or -e
ENV DISPLAY=:0 \
    DISPLAY_WIDTH=1920 \
    DISPLAY_HEIGHT=1080 \
    FPS=30 \
    ENCODER=auto \
    VIDEO_BITRATE_KBPS=4000 \
    AUDIO_BITRATE=96000 \
    HTTP_PORT=8080 \
    STUN_SERVER=stun:stun.l.google.com:19302 \
    CLIENT_STUN=stun:stun.l.google.com:19302 \
    NVIDIA_DRIVER_CAPABILITIES=all

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# ---------- runtime: full ----------
#
# Everything lite has, plus what is too large or too niche to hand somebody who
# only wanted a desktop. Built FROM desktop rather than beside it, so the two
# images share every layer up to this point: a registry holding both stores the
# difference once, and a machine that has pulled lite pulls only the extra.
#
# Steam is what forces the awkward part — contrib/non-free components, i386
# multiarch and a licence accepted in advance — so that setup lives here rather
# than in the base everybody pays for.
FROM desktop AS full

ENV DEBIAN_FRONTEND=noninteractive

COPY deploy/packages/full.txt deploy/packages/full-amd64.txt /tmp/packages/

RUN sed -i 's/^Components: .*/Components: main contrib non-free non-free-firmware/' \
        /etc/apt/sources.list.d/debian.sources \
    && lists="/tmp/packages/full.txt" \
    && if [ "$(dpkg --print-architecture)" = "amd64" ]; then \
         dpkg --add-architecture i386 \
         && echo "steam steam/question select I AGREE" | debconf-set-selections \
         && echo "steam steam/license note ''" | debconf-set-selections \
         && lists="$lists /tmp/packages/full-amd64.txt"; \
       fi \
    && apt-get update \
    && sed -e 's/#.*//' -e '/^[[:space:]]*$/d' $lists \
       | xargs apt-get install -y --no-install-recommends \
    && if [ "$(dpkg --print-architecture)" = "amd64" ]; then \
         apt-get install -y --no-install-recommends \
           libgl1:i386 libgl1-mesa-dri:i386 mesa-vulkan-drivers:i386; \
       fi \
    && rm -rf /var/lib/apt/lists/*
