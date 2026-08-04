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

# Report every command's exit status, whoever ran it.
#
# Sourced by every interactive shell on the desktop, so the record does not
# depend on who opened the terminal. That symmetry is the point: a person hits an
# error, asks the agent to look at it, and the agent can read what actually
# happened instead of being told about it second-hand.
#
# It writes to a file rather than printing, so nothing changes on screen. The
# alternative — appending `; echo $?` to commands — would put bookkeeping in
# front of the person sharing the session.
#
# $? is captured first and restored last, so a command's status still reaches
# whatever the person types next ($?, ||, &&) exactly as it would have.

__sd_report() {
    local rc=$?
    local last
    last=$(HISTTIMEFORMAT= history 1 2>/dev/null | sed 's/^ *[0-9]* *//')
    printf '%s\t%s\n' "$rc" "$last" > /tmp/sentineldesk-rc 2>/dev/null
    return $rc
}

case "$PROMPT_COMMAND" in
    *__sd_report*) ;;                       # already installed
    "")  PROMPT_COMMAND="__sd_report" ;;
    *)   PROMPT_COMMAND="__sd_report; $PROMPT_COMMAND" ;;
esac
export PROMPT_COMMAND

# Root shells started with `su` load root's own bashrc, which does not have
# this. Exporting the function lets `sudo -E su` and `su -p` keep reporting.
export -f __sd_report 2>/dev/null || true
