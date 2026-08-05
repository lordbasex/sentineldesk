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
# Initial desktop setup, run once at start.

# The image's user configuration. The home is a persistent volume, so this is
# resynchronised on every start — otherwise an old copy would stay frozen there
# and image updates would never reach an existing installation.
if [ -d /etc/skel.sentineldesk ]; then
    cp -r /etc/skel.sentineldesk/. "$HOME/" 2>/dev/null || true
fi

# --- Keyboard layout ---------------------------------------------------------
#
# X decides which key produces which character, and it defaults to US. On a
# Spanish keyboard that turns ñ, á and the whole punctuation row into something
# else — and the person typing has no way to tell it is the server's fault.
#
# KEYBOARD_LAYOUT takes an X layout code: us, es (Spain), latam (Latin America),
# pt, fr, de… KEYBOARD_VARIANT is optional and passed through untouched.
#
# Applied to the running X server rather than baked in, so one image serves
# every keyboard, and reported either way: a layout that silently failed to
# apply is the kind of thing people spend an afternoon on.
LAYOUT="${KEYBOARD_LAYOUT:-us}"
if [ -n "$LAYOUT" ]; then
    if setxkbmap -layout "$LAYOUT" ${KEYBOARD_VARIANT:+-variant "$KEYBOARD_VARIANT"} 2>/dev/null; then
        echo "sentineldesk: keyboard $LAYOUT${KEYBOARD_VARIANT:+ ($KEYBOARD_VARIANT)}"
    else
        echo "sentineldesk: could not apply keyboard layout '$LAYOUT' — staying on us" >&2
        setxkbmap -layout us 2>/dev/null || true
    fi
fi

# Browser locks left by the previous container. The home persists but the
# hostname changes, so Chromium and Firefox conclude the profile is open "on
# another computer" and refuse to start.
rm -f "$HOME/.config/chromium/Singleton"* 2>/dev/null
rm -f "$HOME"/.mozilla/firefox/*/lock "$HOME"/.mozilla/firefox/*/.parentlock 2>/dev/null

# Exit-status reporting in interactive shells. /etc/profile.d only covers login
# shells and a terminal emulator opens a non-login one, so the hook has to be
# named from .bashrc as well. Appended once, and left alone afterwards.
if [ -f /etc/profile.d/99-sentineldesk-report.sh ] &&
   ! grep -q 99-sentineldesk-report "$HOME/.bashrc" 2>/dev/null; then
    printf '\n. /etc/profile.d/99-sentineldesk-report.sh\n' >> "$HOME/.bashrc"
fi

until xdpyinfo -display "${DISPLAY:-:0}" >/dev/null 2>&1; do
    sleep 0.2
done

# Wallpaper: $WALLPAPER wins, then the mounted ./wallpaper/, then the built-in.
# The window manager has nothing to do with this — Openbox draws frames, not the
# desktop. pcmanfm owns the root window and reads the path from this config.
pick_wallpaper() {
    [ -n "$WALLPAPER" ] && [ -f "$WALLPAPER" ] && { echo "$WALLPAPER"; return; }
    for f in /wallpaper/*.png /wallpaper/*.jpg /wallpaper/*.jpeg /wallpaper/*.webp; do
        [ -f "$f" ] && { echo "$f"; return; }
    done
    echo /usr/share/backgrounds/sentineldesk.png
}

wp=$(pick_wallpaper)
mkdir -p "$HOME/.config/pcmanfm/LXDE"
cat > "$HOME/.config/pcmanfm/LXDE/desktop-items-0.conf" <<EOF
[*]
wallpaper_mode=crop
wallpaper_common=1
wallpaper=$wp
desktop_bg=#d6dae4
desktop_fg=#1d1d1d
desktop_shadow=#ffffff
desktop_font=Roboto 11
show_wm_menu=0
sort=mtime;ascending;
show_documents=0
show_trash=1
show_mounts=1
EOF
echo "wallpaper: $wp"
exit 0
