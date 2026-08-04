#!/usr/bin/env python3
"""A command-line MCP client for the WebRTC desktop.

It speaks the same protocol an AI host does (JSON-RPC over the stdio bridge), so
it drives the desktop without configuring anything in Claude Code or Claude
Desktop — useful for diagnosis, automation and testing.

Usage:
    ./mcp-cli.py list                        # the tool catalogue
    ./mcp-cli.py call screenshot             # call a tool with no arguments
    ./mcp-cli.py call mouse_click '{"x":100,"y":200}'
    ./mcp-cli.py call run_command '{"command":"uname -a"}'
    ./mcp-cli.py batch steps.json            # several calls in one session

Images are saved as PNG and the path is reported (--out chooses where).

Options:
    --container NAME     Docker container (default: sentineldesk)
    --sock PATH          the MCP socket inside the container
    --out DIR            where to save images (default: /tmp)
    --raw                print the raw JSON-RPC reply
"""

import argparse
import base64
import json
import os
import subprocess
import sys
import threading
import time

DEFAULT_CONTAINER = "sentineldesk"
DEFAULT_SOCK = "/run/user/1000/sentineldesk-mcp.sock"


class MCPClient:
    """An MCP session against the desktop daemon, over the stdio bridge."""

    def __init__(self, container=DEFAULT_CONTAINER, sock=DEFAULT_SOCK, out_dir="/tmp"):
        self.out_dir = out_dir
        self._id = 0
        self._resps = {}
        self.proc = subprocess.Popen(
            ["docker", "exec", "-i", "-u", "sentineldesk", container,
             "/usr/local/bin/sentineldesk", "-mcp-stdio", "-mcp-sock", sock],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        threading.Thread(target=self._reader, daemon=True).start()
        self.request("initialize", {})
        self.notify("notifications/initialized")

    def _reader(self):
        for line in self.proc.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
                self._resps[msg.get("id")] = msg
            except json.JSONDecodeError:
                pass

    def _send(self, payload):
        self.proc.stdin.write((json.dumps(payload) + "\n").encode())
        self.proc.stdin.flush()

    def notify(self, method, params=None):
        self._send({"jsonrpc": "2.0", "method": method, "params": params or {}})

    def request(self, method, params=None, timeout=90):
        self._id += 1
        rid = self._id
        self._send({"jsonrpc": "2.0", "id": rid, "method": method, "params": params or {}})
        deadline = time.time() + timeout
        while rid not in self._resps and time.time() < deadline:
            if self.proc.poll() is not None:
                raise RuntimeError("the MCP bridge exited: " +
                                   self.proc.stderr.read().decode()[:400])
            time.sleep(0.03)
        if rid not in self._resps:
            raise TimeoutError(f"no reply to {method} after {timeout}s")
        return self._resps.pop(rid)

    def list_tools(self):
        return self.request("tools/list")["result"]["tools"]

    def call(self, name, args=None, timeout=90):
        """Calls a tool; returns text, or the PNG path when it returns an image."""
        resp = self.request("tools/call", {"name": name, "arguments": args or {}}, timeout)
        if "error" in resp:
            return f"ERROR: {resp['error'].get('message')}", True
        result = resp["result"]
        is_error = bool(result.get("isError"))
        parts = []
        for item in result.get("content", []):
            if item.get("type") == "image":
                path = os.path.join(
                    self.out_dir, f"mcp-{name}-{int(time.time()*1000)}.png")
                with open(path, "wb") as fh:
                    fh.write(base64.b64decode(item["data"]))
                parts.append(f"[image saved: {path}]")
            else:
                parts.append(item.get("text", ""))
        return "\n".join(parts), is_error

    def close(self):
        try:
            self.proc.stdin.close()
        except Exception:
            pass


def main():
    ap = argparse.ArgumentParser(description="MCP client for the WebRTC desktop")
    ap.add_argument("action", choices=["list", "call", "batch"])
    ap.add_argument("target", nargs="?", help="tool name, or a .json file in batch mode")
    ap.add_argument("args", nargs="?", default="{}", help="the tool's JSON arguments")
    ap.add_argument("--container", default=DEFAULT_CONTAINER)
    ap.add_argument("--sock", default=DEFAULT_SOCK)
    ap.add_argument("--out", default="/tmp")
    ap.add_argument("--raw", action="store_true")
    opts = ap.parse_args()

    client = MCPClient(opts.container, opts.sock, opts.out)
    try:
        if opts.action == "list":
            tools = client.list_tools()
            print(f"{len(tools)} tools:\n")
            for tool in tools:
                print(f"  {tool['name']:24} {tool['description'][:96]}")
            return 0

        if opts.action == "call":
            if not opts.target:
                ap.error("the tool name is missing")
            try:
                args = json.loads(opts.args)
            except json.JSONDecodeError as exc:
                ap.error(f"invalid JSON arguments: {exc}")
            if opts.raw:
                print(json.dumps(client.request(
                    "tools/call", {"name": opts.target, "arguments": args}), indent=2))
                return 0
            text, is_error = client.call(opts.target, args)
            print(text)
            return 1 if is_error else 0

        # batch: [{"tool": "...", "args": {...}}, ...]
        with open(opts.target) as fh:
            steps = json.load(fh)
        failures = 0
        for i, step in enumerate(steps, 1):
            name = step["tool"]
            text, is_error = client.call(name, step.get("args"))
            flag = "ERROR " if is_error else ""
            print(f"[{i:02d}] {flag}{name}: {text[:160]}")
            failures += int(is_error)
        print(f"\n{len(steps)} pasos, {failures} errores")
        return 1 if failures else 0
    finally:
        client.close()


if __name__ == "__main__":
    sys.exit(main())
