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
estructura, y barato de resolver.

Sub-commands (JSON on stdout):
    tree     [--app NOMBRE] [--depth N] [--interactive]
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


def cmd_waitfor(args):
    deadline = time.time() + args.timeout_ms / 1000.0
    while time.time() < deadline:
        res = cmd_find(args)
        if res["count"]:
            return {"found": True, "elements": res["elements"][:3],
                    "waited_ms": int(args.timeout_ms - (deadline - time.time()) * 1000)}
        time.sleep(0.25)
    return {"found": False, "error": "timed out waiting for the element"}


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
