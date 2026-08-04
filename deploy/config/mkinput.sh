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
# Creates the /dev/input/eventN node for the virtual gamepad (uinput).
#
# Inside a container /dev is not devtmpfs: when sentineldesk creates the gamepad
# through uinput the kernel registers /sys/class/input/eventN, but the device
# node never appears on its own. sentineldesk runs unprivileged and cannot
# mknod, so this helper — launched as root from the entrypoint — watches sysfs
# and creates the node, so that SDL/evdev, and therefore games and Steam,
# can see the controller.
mkdir -p /dev/input
while true; do
    for d in /sys/class/input/event*; do
        [ -e "$d/device/name" ] || continue
        [ "$(cat "$d/device/name" 2>/dev/null)" = "sentineldesk-gamepad" ] || continue
        ev=$(basename "$d")
        [ -e "/dev/input/$ev" ] && continue
        mm=$(cat "$d/dev" 2>/dev/null) || continue   # ej. 13:65
        major=${mm%:*}
        minor=${mm#*:}
        if mknod "/dev/input/$ev" c "$major" "$minor" 2>/dev/null; then
            chmod 666 "/dev/input/$ev"
            echo "mkinput: created the gamepad node /dev/input/$ev ($mm)"
        fi
    done
    sleep 2
done
