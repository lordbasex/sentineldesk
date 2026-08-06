#!/usr/bin/env python3
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
"""AT-SPI bridge for the MCP's ui_* tools.

Queries and drives the desktop's accessibility tree: which widgets exist, what
they are called, where they are and which actions they accept. This is what lets
applications be operated by their STRUCTURE — invoking the "Save" button —
instead of guessing with screenshots and clicks at coordinates.

Every element is identified by a `ref`: the path of indices from the desktop,
for example "2/0/3/1". It stays stable as long as the window does not change
its structure, and it is cheap to resolve.

Sub-commands (JSON on stdout):
    tree     [--app NAME] [--depth N] [--interactive]
    find     [--role R] [--name N] [--text T] [--app A] [--limit N]
    click    --ref R [--action click]
    settext  --ref R --text T
    gettext  --ref R
    focus    --ref R
    waitfor  [--role R] [--name N] [--text T] [--timeout-ms N]
"""

import argparse
import json
import sys
import time

try:
    import pyatspi
except ImportError:
    print(json.dumps({"error": "pyatspi is not installed"}))
    sys.exit(1)


def safe(fn, default=None):
    try:
        return fn()
    except Exception:
        return default


def describe(obj, ref):
    """One element: role, name, state, geometry and actions."""
    info = {
        "ref": ref,
        "role": safe(obj.getRoleName, "?"),
        "name": safe(lambda: obj.name or "", ""),
    }
    desc = safe(lambda: obj.description or "", "")
    if desc:
        info["description"] = desc

    comp = safe(obj.queryComponent)
    if comp:
        ext = safe(lambda: comp.getExtents(pyatspi.DESKTOP_COORDS))
        if ext and (ext.width or ext.height):
            info.update({"x": ext.x, "y": ext.y, "width": ext.width, "height": ext.height,
                         "center_x": ext.x + ext.width // 2,
                         "center_y": ext.y + ext.height // 2})

    act = safe(obj.queryAction)
    if act:
        names = safe(lambda: [act.getName(i) for i in range(act.nActions)], [])
        if names:
            info["actions"] = names

    txt = safe(obj.queryText)
    if txt:
        content = safe(lambda: txt.getText(0, 200), "")
        if content and content.strip():
            info["text"] = content.strip()

    val = safe(obj.queryValue)
    if val:
        info["value"] = safe(lambda: val.currentValue)

    states = safe(lambda: obj.getState().getStates(), [])
    flags = []
    for st, label in ((pyatspi.STATE_FOCUSED, "focused"),
                      (pyatspi.STATE_SELECTED, "selected"),
                      (pyatspi.STATE_CHECKED, "checked"),
                      (pyatspi.STATE_ENABLED, "enabled"),
                      (pyatspi.STATE_VISIBLE, "visible"),
                      (pyatspi.STATE_SHOWING, "showing"),
                      (pyatspi.STATE_EDITABLE, "editable")):
        if st in states:
            flags.append(label)
    if flags:
        info["state"] = flags
    return info


# The roles that almost always matter when driving an interface.
INTERACTIVE_ROLES = {
    "push button", "button", "toggle button", "radio button", "check box",
    "menu item", "check menu item", "radio menu item", "menu", "combo box",
    "text", "entry", "password text", "link", "list item", "table cell",
    "tab", "page tab", "slider", "spin button", "tree item", "icon",
}


def walk(obj, ref, depth, max_depth, out, interactive_only=False):
    if depth > max_depth:
        return
    info = describe(obj, ref)
    role = info.get("role", "")
    keep = True
    if interactive_only and depth > 1:
        # Keep whatever is actionable, or carries useful text or a name
        keep = (role in INTERACTIVE_ROLES or "actions" in info
                or bool(info.get("text")) or bool(info.get("name")))
    if keep:
        out.append(info)
    for i, child in enumerate(obj):
        if child is None:
            continue
        walk(child, f"{ref}/{i}" if ref else str(i), depth + 1, max_depth, out,
             interactive_only)


def resolve(ref):
    """Turns "2/0/3" into the matching object."""
    node = pyatspi.Registry.getDesktop(0)
    for part in str(ref).split("/"):
        if part == "":
            continue
        node = node[int(part)]
    return node


def apps(app_filter=None):
    desktop = pyatspi.Registry.getDesktop(0)
    for i, app in enumerate(desktop):
        if app is None:
            continue
        name = safe(lambda: app.name or "", "")
        if app_filter and app_filter.lower() not in name.lower():
            continue
        yield i, app


def cmd_tree(args):
    out = []
    for i, app in apps(args.app):
        walk(app, str(i), 1, args.depth, out, args.interactive)
    return {"count": len(out), "elements": out[:args.limit]}


def matches(info, args):
    if args.role and args.role.lower() not in info.get("role", "").lower():
        return False
    if args.name and args.name.lower() not in info.get("name", "").lower():
        return False
    if args.text and args.text.lower() not in (info.get("text", "") + info.get("name", "")).lower():
        return False
    return True


def cmd_find(args):
    found = []
    for i, app in apps(args.app):
        collected = []
        walk(app, str(i), 1, args.depth, collected, False)
        for info in collected:
            if matches(info, args):
                found.append(info)
                if len(found) >= args.limit:
                    return {"count": len(found), "elements": found}
    return {"count": len(found), "elements": found}


def cmd_click(args):
    obj = resolve(args.ref)
    act = obj.queryAction()
    names = [act.getName(i) for i in range(act.nActions)]
    idx = 0
    if args.action:
        for i, n in enumerate(names):
            if args.action.lower() in n.lower():
                idx = i
                break
        else:
            return {"error": f"action {args.action!r} is not available", "actions": names}
    act.doAction(idx)
    return {"ok": True, "action": names[idx] if names else "", "ref": args.ref}


def cmd_settext(args):
    obj = resolve(args.ref)
    try:
        editable = obj.queryEditableText()
    except Exception:
        return {"error": "the element is not editable"}
    editable.setTextContents(args.text)
    return {"ok": True, "ref": args.ref, "chars": len(args.text)}


def cmd_gettext(args):
    obj = resolve(args.ref)
    info = describe(obj, args.ref)
    # describe() caps text at 200 characters to keep the tree small, which is
    # right for a label and useless for a terminal. Read it again in full here.
    text = info.get("text", "")
    txt = safe(obj.queryText)
    if txt:
        n = safe(lambda: txt.characterCount, 0) or 0
        if n:
            full = safe(lambda: txt.getText(0, min(n, args.max_chars)), "")
            if full:
                text = full
    return {"ref": args.ref, "text": text, "name": info.get("name", ""),
            "role": info.get("role", "")}


def cmd_focus(args):
    obj = resolve(args.ref)
    ok = False
    comp = safe(obj.queryComponent)
    if comp:
        ok = bool(safe(comp.grabFocus, False))
    return {"ok": ok, "ref": args.ref}


# The events that can bring a matching element into existence. children-changed
# covers a widget being added, state-changed covers one that existed but was not
# showing, and the window and document ones catch the wholesale replacements —
# a dialog opening, a page finishing — where the individual node events arrive
# too fast to be worth counting.
WAIT_EVENTS = (
    "object:children-changed",
    "object:state-changed",
    "window:activate",
    "window:create",
    "document:load-complete",
)

# How long to let a burst settle before searching. An application drawing itself
# emits hundreds of children-changed in a few milliseconds, and searching on
# each one would be slower than the polling this replaces. Coalescing them into
# one search after the burst keeps the cost proportional to what happened rather
# than to how loudly it was announced.
WAIT_DEBOUNCE_MS = 120


def cmd_waitfor(args):
    """Wait for an element by listening, not by asking.

    This used to walk every application's accessibility tree four times a
    second and filter the result. Each node in that walk is a D-Bus round trip,
    so waiting fifteen seconds for a dialog meant sixty full traversals of every
    open application to be told fifty-nine times that nothing had changed — and
    on a desktop where nothing is happening, every one of those was wasted.

    AT-SPI already announces the changes the walk was looking for. Registering
    for them means a still desktop costs nothing at all, and an element that
    appears is found as it appears rather than up to 250ms afterwards.
    """
    start = time.time()

    # Look before listening. Between deciding to wait and the listener being
    # live there is a gap in which the element can appear, and a wait that
    # registered first would sleep through it to the timeout.
    res = cmd_find(args)
    if res["count"]:
        return {"found": True, "elements": res["elements"][:3],
                "waited_ms": 0, "via": "already present"}

    from gi.repository import GLib

    result = {}
    pending = [False]

    # search only reports; stopping the loop is the caller's business, because
    # the last call happens after the loop has already ended and stopping a
    # registry that is not running is not something to find out in production.
    def search(via):
        found = cmd_find(args)
        if not found["count"]:
            return False
        result.update({"found": True, "elements": found["elements"][:3],
                       "waited_ms": int((time.time() - start) * 1000), "via": via})
        return True

    def debounced():
        pending[0] = False
        if search("event"):
            pyatspi.Registry.stop()
        return False  # one shot

    def on_event(_event):
        # Schedule rather than drop. Ignoring events while one is pending would
        # lose the last of a burst, which is the one most likely to be the
        # change being waited for.
        if not pending[0]:
            pending[0] = True
            GLib.timeout_add(WAIT_DEBOUNCE_MS, debounced)

    def on_timeout():
        pyatspi.Registry.stop()
        return False

    pyatspi.Registry.registerEventListener(on_event, *WAIT_EVENTS)
    GLib.timeout_add(int(args.timeout_ms), on_timeout)
    try:
        pyatspi.Registry.start()
    finally:
        try:
            pyatspi.Registry.deregisterEventListener(on_event, *WAIT_EVENTS)
        except Exception:
            pass

    if result:
        return result

    # One last look. The loop can end with a debounce still outstanding, and an
    # element that arrived in those final milliseconds is there whether or not
    # anything got round to noticing.
    if search("final check"):
        return result
    return {"found": False, "error": "timed out waiting for the element",
            "waited_ms": int((time.time() - start) * 1000)}


def main():
    ap = argparse.ArgumentParser(description="AT-SPI bridge for the ui_* tools")
    sub = ap.add_subparsers(dest="cmd", required=True)

    def common(p, with_filters=True):
        p.add_argument("--app")
        p.add_argument("--depth", type=int, default=12)
        p.add_argument("--limit", type=int, default=200)
        if with_filters:
            p.add_argument("--role")
            p.add_argument("--name")
            p.add_argument("--text")

    p = sub.add_parser("tree")
    common(p, with_filters=False)
    p.add_argument("--interactive", action="store_true")

    p = sub.add_parser("find")
    common(p)

    p = sub.add_parser("click")
    p.add_argument("--ref", required=True)
    p.add_argument("--action")

    p = sub.add_parser("settext")
    p.add_argument("--ref", required=True)
    p.add_argument("--text", required=True)

    p = sub.add_parser("gettext")
    p.add_argument("--ref", required=True)
    p.add_argument("--max-chars", type=int, default=20000)

    p = sub.add_parser("focus")
    p.add_argument("--ref", required=True)

    p = sub.add_parser("waitfor")
    common(p)
    p.add_argument("--timeout-ms", type=int, default=15000)

    args = ap.parse_args()
    handlers = {"tree": cmd_tree, "find": cmd_find, "click": cmd_click,
                "settext": cmd_settext, "gettext": cmd_gettext,
                "focus": cmd_focus, "waitfor": cmd_waitfor}
    try:
        print(json.dumps(handlers[args.cmd](args), ensure_ascii=False))
    except Exception as exc:
        print(json.dumps({"error": f"{type(exc).__name__}: {exc}"}))
        sys.exit(1)


if __name__ == "__main__":
    main()
