#!/bin/sh
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
# Rotate the desktop wallpaper on a timer.
#
# The window manager plays no part in this. Openbox draws window frames and
# nothing else; the root window belongs to pcmanfm, running in desktop mode, and
# it is the only thing that can change what is on it. `pcmanfm --set-wallpaper`
# talks to that running instance, so the change lands immediately and survives
# in its configuration.
#
# Rotation is off unless it can do something useful:
#   - $WALLPAPER pinned to one image means somebody chose it deliberately
#   - fewer than two images leaves nothing to rotate between
# In both cases this exits cleanly rather than looping over a single picture.

set -eu

DIR="${WALLPAPER_DIR:-/wallpaper}"
EVERY="${WALLPAPER_ROTATE_SECS:-300}"
MODE="${WALLPAPER_MODE:-crop}"

# A pinned wallpaper is a decision, not a default. Respect it.
if [ -n "${WALLPAPER:-}" ] && [ -f "${WALLPAPER}" ]; then
    echo "wallpaper-rotate: WALLPAPER is pinned, not rotating"
    exit 0
fi

case "$EVERY" in
    ''|*[!0-9]*) echo "wallpaper-rotate: bad interval '$EVERY', using 300"; EVERY=300 ;;
    0) echo "wallpaper-rotate: disabled (interval 0)"; exit 0 ;;
esac

list_images() {
    find "$DIR" -maxdepth 1 -type f \
        \( -iname '*.png' -o -iname '*.jpg' -o -iname '*.jpeg' -o -iname '*.webp' \) \
        2>/dev/null | sort
}

count=$(list_images | wc -l)
if [ "$count" -lt 2 ]; then
    echo "wallpaper-rotate: $count image(s) in $DIR, nothing to rotate"
    exit 0
fi
echo "wallpaper-rotate: $count images in $DIR, every ${EVERY}s"

# pcmanfm has to be up before the first change: sending it a wallpaper while it
# is still starting writes the config but leaves the screen showing the old one.
until pgrep -x pcmanfm >/dev/null 2>&1; do
    sleep 1
done
sleep 2

current=""
while :; do
    # Re-read every round, so images dropped into the mounted folder are picked
    # up without a restart.
    images=$(list_images)
    n=$(printf '%s\n' "$images" | wc -l)

    if [ "$n" -gt 1 ] && [ -n "$current" ]; then
        # Excluding the current one guarantees a visible change. Without this a
        # random pick repeats often enough at small counts to look broken.
        candidates=$(printf '%s\n' "$images" | grep -vxF "$current" || true)
    else
        candidates="$images"
    fi
    [ -n "$candidates" ] || candidates="$images"

    next=$(printf '%s\n' "$candidates" | shuf -n 1)
    if [ -n "$next" ] && [ -f "$next" ]; then
        pcmanfm --set-wallpaper="$next" --wallpaper-mode="$MODE" >/dev/null 2>&1 || true
        current="$next"
    fi
    sleep "$EVERY"
done
