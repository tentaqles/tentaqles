#!/usr/bin/env python3
"""A made-up MCP server for the hook capture: three tools that answer with fixed text.

Usage (written into .mcp.json by `setup.py --mcp`): python mcp_fail_server.py

It speaks MCP over stdio (one JSON-RPC message per line) and touches nothing else:
no file, no network. Its tools:

  flag_error  a normal result with isError true (an MCP tool's own error)
  rpc_error   a JSON-RPC error answer (a protocol-level failure)
  ok_echo     a normal result

Every text carries the marker CAPTURE-FAIL-MARKER or CAPTURE-OK, so reduce.py can
count where the answers land without printing them.
"""

import json
import sys

TOOLS = [
    {
        "name": "flag_error",
        "description": "Always fails: returns a result with isError true.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "rpc_error",
        "description": "Always fails: answers with a JSON-RPC error.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "ok_echo",
        "description": "Always works: returns a short text.",
        "inputSchema": {"type": "object", "properties": {}},
    },
]


def send(message):
    sys.stdout.buffer.write(json.dumps(message).encode("utf-8") + b"\n")
    sys.stdout.buffer.flush()


def answer(request):
    method = request.get("method")
    rid = request.get("id")
    if rid is None:
        return  # a notification, such as notifications/initialized
    if method == "initialize":
        params = request.get("params") or {}
        send({
            "jsonrpc": "2.0",
            "id": rid,
            "result": {
                "protocolVersion": params.get("protocolVersion", "2025-06-18"),
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "capture", "version": "0.1.0"},
            },
        })
    elif method == "tools/list":
        send({"jsonrpc": "2.0", "id": rid, "result": {"tools": TOOLS}})
    elif method == "tools/call":
        name = (request.get("params") or {}).get("name")
        if name == "flag_error":
            send({"jsonrpc": "2.0", "id": rid, "result": {
                "content": [{"type": "text", "text": "CAPTURE-FAIL-MARKER the widget is not frobnicated"}],
                "isError": True,
            }})
        elif name == "rpc_error":
            send({"jsonrpc": "2.0", "id": rid, "error": {
                "code": -32000, "message": "CAPTURE-FAIL-MARKER the server refused",
            }})
        elif name == "ok_echo":
            send({"jsonrpc": "2.0", "id": rid, "result": {
                "content": [{"type": "text", "text": "CAPTURE-OK pong"}],
            }})
        else:
            send({"jsonrpc": "2.0", "id": rid, "error": {"code": -32602, "message": "unknown tool"}})
    elif method == "ping":
        send({"jsonrpc": "2.0", "id": rid, "result": {}})
    else:
        send({"jsonrpc": "2.0", "id": rid, "error": {"code": -32601, "message": "method not found"}})


def main():
    for raw in sys.stdin.buffer:
        line = raw.strip()
        if not line:
            continue
        try:
            request = json.loads(line)
        except ValueError:
            continue
        if isinstance(request, dict):
            answer(request)
    return 0


if __name__ == "__main__":
    sys.exit(main())
