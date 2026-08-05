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
set -e

# --- Half a login is a typo, not a configuration -----------------------------
#
# AUTH_USER and AUTH_PASS are both required to switch the login on. Setting
# exactly one of them says plainly that somebody MEANT to, and the classic way
# to end up there is a missing letter — UTH_USER instead of AUTH_USER — which
# used to leave the desktop open to anybody who could reach the port, with the
# only notice a warning buried between XGB's complaints about .Xauthority.
#
# The check is here, and not only in the binary, because supervisord restarts
# what it supervises. The server refusing to run just means it is started again
# two seconds later, forever, while the container reports itself perfectly up.
# Refusing BEFORE supervisord makes the container exit, which is what "it did
# not start" is supposed to look like from the outside.
#
# Both unset stays legal. That is the documented development mode: a deliberate
# choice rather than a slip.
if { [ -n "${AUTH_USER:-}" ] && [ -z "${AUTH_PASS:-}" ]; } \
   || { [ -z "${AUTH_USER:-}" ] && [ -n "${AUTH_PASS:-}" ]; }; then
    if [ -n "${AUTH_USER:-}" ]; then set_var=AUTH_USER; missing=AUTH_PASS
    else set_var=AUTH_PASS; missing=AUTH_USER; fi
    echo "sentineldesk: $set_var is set but $missing is empty: refusing to start with half a login." >&2
    echo "sentineldesk: set both to require authentication, or neither for an open desktop (development only)." >&2
    exit 1
fi

mkdir -p /run/user/1000
chown sentineldesk:sentineldesk /run/user/1000
chmod 700 /run/user/1000

mkdir -p /tmp/.X11-unix
chmod 1777 /tmp/.X11-unix

# --- Timezone ----------------------------------------------------------------
#
# One image, every timezone: TZ names a zone from tzdata and this points
# /etc/localtime at it. Applied here rather than baked into the image because a
# desktop somebody works at should show THEIR clock, and the same build serves
# a machine in Buenos Aires and one in Madrid.
#
# An unknown name is reported and ignored rather than left half-applied — a
# clock quietly running in UTC because of a typo is worse than one that says so.
if [ -n "${TZ:-}" ]; then
    if [ -f "/usr/share/zoneinfo/$TZ" ]; then
        ln -snf "/usr/share/zoneinfo/$TZ" /etc/localtime
        echo "$TZ" > /etc/timezone
        echo "sentineldesk: timezone $TZ ($(date '+%Z %z'))"
    else
        echo "sentineldesk: unknown TZ '$TZ' — staying on $(cat /etc/timezone 2>/dev/null || echo UTC)" >&2
    fi
fi

# Leftovers from the previous start. `docker run` never has them, but `docker
# restart` keeps the filesystem: the lock survives and Xvfb refuses to start
# with "Server is already active for display 0" — and the desktop never comes
# back. Nothing survives this boot, so removing them is safe by definition.
rm -f /tmp/.X[0-9]-lock /tmp/.X11-unix/X[0-9] 2>/dev/null || true
rm -f /tmp/supervisord.pid /tmp/supervisor.sock 2>/dev/null || true

# --- Privileges inside the desktop -------------------------------------------
# sudo is passwordless (the Dockerfile sets that up). `su`, on the other hand,
# needs a real root password: without one the account is locked and `su` always
# fails. ROOT_PASSWORD sets it; when it is not given the web login's password is
# reused rather than inventing a second credential, and "root" is the last
# resort.
ROOT_PASSWORD="${ROOT_PASSWORD:-${AUTH_PASS:-root}}"
echo "root:${ROOT_PASSWORD}" | chpasswd 2>/dev/null || true
echo "sentineldesk:${SENTINELDESK_PASSWORD:-${ROOT_PASSWORD}}" | chpasswd 2>/dev/null || true

# A safety net for images built before the privilege layer existed: if the
# sudoers file is missing, write it at start.
if command -v sudo >/dev/null 2>&1 && [ ! -f /etc/sudoers.d/010-sentineldesk ]; then
    printf 'sentineldesk ALL=(ALL:ALL) NOPASSWD:SETENV: ALL\n' > /etc/sudoers.d/010-sentineldesk
    chmod 0440 /etc/sudoers.d/010-sentineldesk
fi

# The system D-Bus. Chromium/CEF uses it to detect network state: without it
# Steam sits at "Waiting for network…" even with working connectivity — its own
# network test passes, but the UI, which is CEF, believes it is offline.
mkdir -p /run/dbus /var/lib/dbus
dbus-uuidgen --ensure=/etc/machine-id 2>/dev/null || true
dbus-uuidgen --ensure 2>/dev/null || true
if [ ! -S /run/dbus/system_bus_socket ]; then
    dbus-daemon --system --fork 2>/dev/null || true
fi

# Creates the virtual gamepad node once sentineldesk registers it (as root).
/usr/local/bin/mkinput.sh &

exec /usr/bin/supervisord -c /etc/supervisor/supervisord.conf
