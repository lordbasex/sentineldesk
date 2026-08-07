#!/usr/bin/env python3
"""Exercise the MCP server against a running desktop.

`make test` checks the catalogue against itself and the JSON-RPC path over an
in-memory pipe. Neither needs an X display, which is what makes them cheap and
also what they cannot tell you: whether any of it works against the real thing.

This does. It speaks the protocol the way an AI host does, through the stdio
bridge into a running container, and checks what stage 1 built:

    the catalogue and its annotations       (risk, and which tools need control)
    connection identity                     (a number per client, in the log)
    tool_search                             (ranking, schemas, category)
    the denial kinds                        (policy, room, unknown_tool, …)
    the room gate                           (refused, then granted, then released)
    cancellation                            (answered at once, work stopped)
    progress                                (opt-in, and carrying real output)
    policy narrowing                        (a connection may not widen)

Usage:
    ./stage1-check.py                       # against the `sentineldesk` container
    ./stage1-check.py --container other
    ./stage1-check.py -v                    # show every check, not just failures

Exit status is 0 when everything passed, 1 otherwise.
"""

import argparse
import importlib.util
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))

# mcp-cli.py has a dash in its name, so it cannot be imported by name.
_spec = importlib.util.spec_from_file_location("mcpcli", os.path.join(HERE, "mcp-cli.py"))
_mcpcli = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_mcpcli)
MCPClient = _mcpcli.MCPClient

# Pinned rather than derived from the catalogue on purpose: reading the number
# from the thing being checked would make the check agree with any drift. These
# are updated by hand when a tool is added, which is the point — adding
# list_commands moved both and the mismatch is how anyone found out.
EXPECTED_TOOLS = 120
EXPECTED_READ = 51
EXPECTED_DANGER = 30
EXPECTED_CONTROL = 19


class Checks:
    def __init__(self, verbose=False):
        self.verbose, self.passed, self.failed = verbose, 0, []

    def ok(self, name, condition, detail=""):
        if condition:
            self.passed += 1
            if self.verbose:
                print(f"  \033[32m✓\033[0m {name}")
        else:
            self.failed.append((name, detail))
            print(f"  \033[31m✗\033[0m {name}" + (f"\n      {detail}" if detail else ""))
        return bool(condition)

    def section(self, title):
        print(f"\n\033[1m{title}\033[0m")

    def report(self):
        total = self.passed + len(self.failed)
        if self.failed:
            print(f"\n\033[31m{len(self.failed)} of {total} checks failed\033[0m")
            for name, detail in self.failed:
                print(f"  - {name}" + (f": {detail}" if detail else ""))
            return 1
        print(f"\n\033[32mall {total} checks passed\033[0m")
        return 0


def annotation(tool, key):
    return tool.get("annotations", {}).get(key)


def denial_of(client, name, args=None, timeout=30):
    """Returns the machine-readable reason a call failed, or '' when it worked."""
    params = {"name": name, "arguments": args or {}}
    resp = client.request("tools/call", params, timeout)
    result = resp.get("result", {})
    if not result.get("isError"):
        return ""
    return result.get("_meta", {}).get("sentineldesk/denial", "<none>")


def check_catalogue(c, client):
    c.section("Catalogue and annotations")
    tools = client.list_tools()
    c.ok(f"tools/list returns {EXPECTED_TOOLS} tools",
         len(tools) == EXPECTED_TOOLS, f"got {len(tools)}")

    missing = [t["name"] for t in tools if "annotations" not in t]
    c.ok("every tool carries annotations", not missing, f"missing on {missing[:5]}")

    read = [t for t in tools if annotation(t, "readOnlyHint")]
    danger = [t for t in tools if annotation(t, "destructiveHint")]
    control = [t for t in tools if annotation(t, "sentineldesk/requiresControl")]
    c.ok(f"{EXPECTED_READ} tools are read-only", len(read) == EXPECTED_READ, f"got {len(read)}")
    c.ok(f"{EXPECTED_DANGER} tools are destructive", len(danger) == EXPECTED_DANGER,
         f"got {len(danger)}")
    c.ok(f"{EXPECTED_CONTROL} tools require room control", len(control) == EXPECTED_CONTROL,
         f"got {len(control)}: {sorted(t['name'] for t in control)}")

    # Risk does not imply the room gate, in either direction. This is the whole
    # reason requiresControl is published separately.
    names = {t["name"]: t for t in tools}
    for gated, free in [("ui_click", "set_volume"), ("start_restream", "write_file")]:
        if gated in names and free in names:
            c.ok(f"{gated} needs control and {free} does not",
                 annotation(names[gated], "sentineldesk/requiresControl") is True
                 and annotation(names[free], "sentineldesk/requiresControl") is False)

    c.ok("no tool is both read-only and destructive",
         not [t["name"] for t in tools
              if annotation(t, "readOnlyHint") and annotation(t, "destructiveHint")])
    c.ok("nothing that needs control claims to be read-only",
         not [t["name"] for t in tools
              if annotation(t, "sentineldesk/requiresControl")
              and annotation(t, "readOnlyHint")])
    return tools


def check_identity(c, client, container, sock):
    c.section("Connection identity")
    c.ok("initialize hands back a connection id", client.connection_id is not None,
         f"got {client.server_info.get('_meta')}")

    other = MCPClient(container=container, sock=sock)
    try:
        c.ok("a second connection gets a different id",
             other.connection_id != client.connection_id,
             f"both got {client.connection_id}")
    finally:
        other.close()

    # The number and the name reach the audit trail, which is what makes an
    # audit readable once a runtime fans several sub-agents out at once.
    client.call("wait", {"ms": 1})
    log, _ = client.call("action_log", {"limit": 5})
    try:
        entries = json.loads(log).get("entries", [])
    except (json.JSONDecodeError, AttributeError):
        c.ok("action_log is JSON with entries", False, log[:200])
        return
    recent = [e for e in entries if isinstance(e, dict) and e.get("tool") == "wait"]
    c.ok("the action log names the connection",
         bool(recent) and recent[-1].get("conn") == client.connection_id,
         f"entry: {recent[-1] if recent else None}")
    c.ok("the action log names the client",
         bool(recent) and recent[-1].get("client", "").startswith("mcp-cli"),
         f"client: {recent[-1].get('client') if recent else None}")


def check_tool_search(c, client):
    c.section("tool_search")
    out, err = client.call("tool_search", {"query": "give someone remote access"})
    c.ok("tool_search answers", not err, out[:200])
    try:
        found = json.loads(out)
    except json.JSONDecodeError:
        c.ok("tool_search returns JSON", False, out[:200])
        return
    names = [t["name"] for t in found.get("tools", [])]
    c.ok("a plain-words query finds the ssh tools",
         any(n.startswith("ssh_") for n in names), f"got {names}")
    c.ok("each hit carries its schema",
         all("inputSchema" in t for t in found.get("tools", [])))
    c.ok("each hit carries its risk",
         all(t.get("risk") in ("read", "write", "danger") for t in found.get("tools", [])))

    out, _ = client.call("tool_search", {"category": "ssh", "limit": 5})
    found = json.loads(out)
    c.ok("a category lists only that category",
         all(t["category"] == "ssh" for t in found.get("tools", [])),
         str([t["category"] for t in found.get("tools", [])]))


def check_denials(c, client, container, sock):
    c.section("Denial kinds")
    c.ok("a tool that does not exist reports unknown_tool",
         denial_of(client, "no_such_tool") == "unknown_tool")
    c.ok("a broken call reports tool_error",
         denial_of(client, "read_file", {"path": "/no/such/file/at/all"}) == "tool_error")
    c.ok("a call that works reports nothing",
         denial_of(client, "wait", {"ms": 1}) == "")

    # A restricted connection, so the ceiling is not touched for anyone else.
    ro = MCPClient(container=container, sock=sock)
    try:
        ro.request("sentineldesk/policy", {"level": "readonly"})
        c.ok("a readonly connection reports policy for run_command",
             denial_of(ro, "run_command", {"command": "true"}) == "policy")
        c.ok("a readonly connection still reads the screen",
             denial_of(ro, "get_screen_info") == "")
        c.ok("an unknown tool is unknown at every level",
             denial_of(ro, "no_such_tool") == "unknown_tool")

        # Narrowing only: asking for more must not grant it.
        applied = ro.request("sentineldesk/policy", {"level": "full"}).get("result", {})
        c.ok("a connection cannot widen its own policy",
             applied.get("level") == "readonly", f"became {applied.get('level')}")
    finally:
        ro.close()


def check_room(c, client):
    c.section("The room gate")
    out, err = client.call("room_state")
    c.ok("room_state answers", not err, out[:200])
    try:
        state = json.loads(out)
    except json.JSONDecodeError:
        state = {}

    if state.get("you_have_control"):
        client.call("release_control")

    # Control is claimed, never assumed — not even with the room empty.
    c.ok("an input tool is refused before control is asked for",
         denial_of(client, "mouse_move", {"x": 10, "y": 10}) == "room")

    # With a person watching, request_control asks them and waits. That is the
    # design working, not a failure, so say so rather than reporting a red cross
    # for somebody having a browser open.
    if state.get("humans_present"):
        print("      (someone is watching — answer the prompt in the browser, "
              "or close it and run this again)")

    out, err = client.call("request_control", {"timeout_ms": 8000}, timeout=25)
    granted = not err
    c.ok("request_control is granted when nothing is driving", granted, out[:200])
    if granted:
        c.ok("the same input tool now works",
             denial_of(client, "mouse_move", {"x": 10, "y": 10}) == "")
        out, err = client.call("release_control")
        c.ok("release_control hands the desktop back", not err, out[:200])
        c.ok("and the gate closes again",
             denial_of(client, "mouse_move", {"x": 10, "y": 10}) == "room")


def check_cancellation(c, client):
    c.section("Cancellation")
    rid = client._id + 1
    client._send({"jsonrpc": "2.0", "id": rid, "method": "tools/call", "params": {
        "name": "run_command",
        "arguments": {"command": "sleep 30", "timeout_ms": 30000},
    }})
    client._id = rid
    time.sleep(0.7)  # let the process actually start

    begin = time.time()
    client.cancel(rid, "stage1-check")
    deadline = begin + 15
    while rid not in client._resps and time.time() < deadline:
        time.sleep(0.02)
    elapsed = time.time() - begin

    if not c.ok("a cancelled call comes back", rid in client._resps,
                f"nothing after {elapsed:.1f}s"):
        return
    result = client._resps.pop(rid).get("result", {})
    c.ok(f"and comes back at once (took {elapsed:.1f}s)", elapsed < 5,
         f"{elapsed:.1f}s — the wait is supposed to end before the work does")
    kind = result.get("_meta", {}).get("sentineldesk/denial")
    c.ok("it reports cancelled rather than a result", kind == "cancelled", f"kind {kind}")
    text = " ".join(i.get("text", "") for i in result.get("content", []))
    c.ok("the client's reason comes back", "stage1-check" in text, text[:200])

    c.ok("the connection still works afterwards",
         denial_of(client, "wait", {"ms": 1}) == "")


def check_progress(c, client):
    c.section("Progress")
    before = len(client.notifications)
    # Long enough for several ticks at the two-second interval, so a single
    # missed one does not decide the result.
    out, err = client.call(
        "run_command",
        {"command": "echo starting; sleep 6; echo done", "timeout_ms": 30000},
        timeout=60, progress_token="stage1-progress")
    c.ok("the command ran", not err, out[:200])

    notes = client.notifications[before:]
    progress = [n for n in notes if n.get("method") == "notifications/progress"]
    c.ok("a command that ran for seconds sent progress", progress,
         f"{len(notes)} notifications, none of them progress")
    if progress:
        params = progress[0].get("params", {})
        c.ok("the token comes back as it was sent",
             params.get("progressToken") == "stage1-progress", str(params))
        c.ok("progress carries a number", "progress" in params, str(params))
        messages = " ".join(p.get("params", {}).get("message", "") for p in progress)
        c.ok("the message carries the command's own output",
             "starting" in messages or "done" in messages, messages[:200])

    # And nothing at all for a client that did not ask. Long enough that the
    # interval would certainly have fired, so silence means the opt-in works
    # rather than that the command was too quick to report on.
    before = len(client.notifications)
    client.call("run_command", {"command": "sleep 5", "timeout_ms": 30000}, timeout=60)
    quiet = [n for n in client.notifications[before:]
             if n.get("method") == "notifications/progress"]
    c.ok("no progress is sent to a call that did not ask for it", not quiet,
         f"{len(quiet)} unasked-for notifications")


def check_events(c, client):
    """The channel that lets an agent be told rather than only asked.

    Nothing here is in the MCP specification, so the point of checking it from
    the outside is that the shape on the wire is what the runtime will be
    written against — a topic name or a field renamed on a whim breaks an agent
    that is not in this repository.
    """
    c.section("Events")

    # Silence first. A host that never subscribes must receive nothing, which
    # is what makes it safe to ship this to clients that do not know about it.
    before = len(client.notifications)
    client.call("wait", {"ms": 1200}, timeout=30)
    unasked = [n for n in client.notifications[before:]
               if n.get("method") == "notifications/sentineldesk/event"]
    c.ok("no events reach a connection that did not subscribe", not unasked,
         f"{len(unasked)} unasked-for events")

    out, err = client.call("subscribe_events", {"topics": ["control", "windows"]})
    c.ok("subscribe_events accepts a topic list", not err, out[:200])
    if not err:
        body = _json_body(out)
        c.ok("it reports what it subscribed to",
             sorted(body.get("subscribed", [])) == ["control", "windows"], out[:200])
        c.ok("it names the method the events will arrive as",
             body.get("method") == "notifications/sentineldesk/event", out[:200])

    # An unknown topic has to be refused. Accepting it silently would leave an
    # agent waiting on something that was never going to arrive, which is the
    # failure the whole feature removes.
    _, err = client.call("subscribe_events", {"topics": ["control", "telepathy"]})
    c.ok("an unknown topic is refused rather than ignored", err)

    # A window opening is a real event from a real source. This is the cheapest
    # of the five to provoke from outside; control needs a second participant.
    client.call("subscribe_events", {"topics": ["windows"]})
    before = len(client.notifications)
    out, err = client.call(
        "launch_app", {"command": "xterm -T STAGE1EVENT -e sleep 20"}, timeout=30)
    if err:
        c.ok("a window could be opened to provoke an event", False, out[:200])
    else:
        deadline = time.time() + 12
        seen = []
        while time.time() < deadline and not seen:
            # The client reads on a background thread, so notifications
            # accumulate on their own; this only gives them time to arrive.
            time.sleep(0.5)
            seen = [n for n in client.notifications[before:]
                    if n.get("method") == "notifications/sentineldesk/event"
                    and n.get("params", {}).get("topic") == "windows"]
        c.ok("opening a window delivers a windows event", seen,
             "nothing arrived in 12s")
        client.call("run_command", {"command": "pkill -f STAGE1EVENT || true"}, timeout=20)

    out, err = client.call("unsubscribe_events", {})
    c.ok("unsubscribe_events stops the subscription",
         not err and not _json_body(out).get("subscribed"), out[:200])


def _json_body(out):
    """The JSON object a tool returned as its text content, or {}."""
    try:
        return json.loads(out)
    except Exception:
        return {}


def main():
    ap = argparse.ArgumentParser(description=__doc__,
                                 formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--container", default=_mcpcli.DEFAULT_CONTAINER)
    ap.add_argument("--sock", default=_mcpcli.DEFAULT_SOCK)
    ap.add_argument("-v", "--verbose", action="store_true")
    args = ap.parse_args()

    c = Checks(args.verbose)
    print(f"Checking the MCP server in container '{args.container}'")

    try:
        client = MCPClient(container=args.container, sock=args.sock)
    except Exception as exc:
        print(f"\n\033[31mcould not reach the desktop: {exc}\033[0m")
        print("Is it running?  make up")
        return 1

    try:
        check_catalogue(c, client)
        check_identity(c, client, args.container, args.sock)
        check_tool_search(c, client)
        check_denials(c, client, args.container, args.sock)
        check_room(c, client)
        check_cancellation(c, client)
        check_events(c, client)
        check_progress(c, client)
    finally:
        client.close()
    return c.report()


if __name__ == "__main__":
    sys.exit(main())
