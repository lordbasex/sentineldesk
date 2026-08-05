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
#
# That overwrite is only safe in a home the desktop owns. A native install can
# be pointed at somebody's existing account, and there this would replace their
# lxpanel, lxterminal and GTK settings with the defaults on every boot: not a
# resync, a nightly reset of a machine they configured. DESKTOP_OWN_HOME says
# which case this is — the installer sets it when it CREATED the account, and
# leaves it unset when it was handed one.
#
# Unset, the copy still runs but with -n, so files that are not there yet are
# filled in and files the person already has are left exactly alone.
if [ -d /etc/skel.sentineldesk ]; then
    if [ "${DESKTOP_OWN_HOME:-1}" = 1 ]; then
        cp -r /etc/skel.sentineldesk/. "$HOME/" 2>/dev/null || true
    else
        cp -rn /etc/skel.sentineldesk/. "$HOME/" 2>/dev/null || true
    fi
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

# --- The XDG user directories ------------------------------------------------
#
# Debian creates these from xdg-user-dirs on a real first login, through PAM.
# Nothing here logs in — supervisord starts the session directly — so the home
# came up without them, and pcmanfm and lxpanel each said so on every boot:
#
#   The directory '~/Templates' doesn't exist, ignoring it
#
# Which is fair enough: "New file from template" is a right-click menu entry
# with nowhere to read templates from. Creating the directories is both the fix
# for the warning and the fix for the missing feature, and it costs eight empty
# directories in a home that already persists.
#
# mkdir -p, so a home carried over from an older container keeps whatever the
# person put in these, and one that already has them is left alone.
for d in Desktop Documents Downloads Music Pictures Public Templates Videos; do
    mkdir -p "$HOME/$d" 2>/dev/null || true
done

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
