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
    # OCR for the read_screen_text / find_text tools
    tesseract-ocr tesseract-ocr-eng tesseract-ocr-spa \
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

COPY --from=build /sentineldesk /usr/local/bin/sentineldesk
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

# contrib + non-free (needed by Steam) and the i386 architecture (amd64 only)
RUN sed -i 's/^Components: .*/Components: main contrib non-free non-free-firmware/' \
        /etc/apt/sources.list.d/debian.sources \
    && if [ "$(dpkg --print-architecture)" = "amd64" ]; then \
         dpkg --add-architecture i386; \
       fi \
    && apt-get update && apt-get install -y --no-install-recommends \
    # Raspberry Pi OS style shell
    lxpanel pcmanfm feh librsvg2-bin \
    # Light flat theme, icons and fonts
    adwaita-icon-theme papirus-icon-theme fonts-roboto fonts-noto-core \
    # Applications
    chromium lxterminal zenity dbus-x11 \
    # Media player. Doubles as a way to test the upstream audio path: VLC can
    # record from the virtual microphone the browser publishes.
    vlc \
    # Remote access clients
    tigervnc-viewer freerdp3-x11 \
    # Software OpenGL/Vulkan for Chromium, apps and games
    libgl1-mesa-dri mesa-vulkan-drivers vulkan-tools \
    # Steam + Steam Play (Proton) — amd64 only, Steam has no ARM build
    && if [ "$(dpkg --print-architecture)" = "amd64" ]; then \
         echo "steam steam/question select I AGREE" | debconf-set-selections \
         && echo "steam steam/license note ''" | debconf-set-selections \
         && apt-get install -y --no-install-recommends \
              steam-installer \
              libgl1:i386 libgl1-mesa-dri:i386 mesa-vulkan-drivers:i386; \
       fi \
    && rm -rf /var/lib/apt/lists/*

COPY deploy/desktop/openbox-themerc /usr/share/themes/SentinelDesk/openbox-3/themerc
COPY deploy/config/supervisord.conf /etc/supervisor/supervisord.conf
COPY deploy/desktop/desktop-init.sh /usr/local/bin/desktop-init.sh
COPY deploy/desktop/wallpaper-rotate.sh /usr/local/bin/wallpaper-rotate.sh
COPY deploy/desktop/vnc-connect deploy/desktop/rdp-connect deploy/desktop/run-steam /usr/local/bin/
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

RUN chmod +x /usr/local/bin/desktop-init.sh /usr/local/bin/wallpaper-rotate.sh \
    /usr/local/bin/vnc-connect \
             /usr/local/bin/rdp-connect /usr/local/bin/run-steam \
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
    && fc-cache -f >/dev/null

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
