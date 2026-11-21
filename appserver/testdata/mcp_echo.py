#!/usr/bin/env python3
"""Local MCP stdio server used by the app-server end-to-end test.

It speaks one JSON-RPC message per line, which is what the 0.7.8 client writes.
The only tool is echo, which returns the
text it was given after the marker sdk-echo-marker:.
"""

import json
import sys


TOOL = {
    "name": "echo",
    "description": "Return the given text unchanged.",
    "inputSchema": {
        "type": "object",
        "properties": {"text": {"type": "string"}},
        "required": ["text"],
    },
}


def read_message():
    line = sys.stdin.buffer.readline()
    if not line:
        return None
    line = line.strip()
    if not line:
        return read_message()
    return json.loads(line.decode())


def write_message(payload):
    sys.stdout.buffer.write(json.dumps(payload).encode() + b"\n")
    sys.stdout.buffer.flush()


def handle(message):
    method = message.get("method")
    message_id = message.get("id")
    if method == "initialize":
        params = message.get("params") or {}
        write_message(
            {
                "jsonrpc": "2.0",
                "id": message_id,
                "result": {
                    "protocolVersion": params.get("protocolVersion", "2024-11-05"),
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "sdk-echo", "version": "0.0.1"},
                },
            }
        )
        return
    if method == "notifications/initialized":
        return
    if method == "ping":
        write_message({"jsonrpc": "2.0", "id": message_id, "result": {}})
        return
    if method == "tools/list":
        write_message({"jsonrpc": "2.0", "id": message_id, "result": {"tools": [TOOL]}})
        return
    if method == "tools/call":
        params = message.get("params") or {}
        arguments = params.get("arguments") or {}
        text = str(arguments.get("text", ""))
        write_message(
            {
                "jsonrpc": "2.0",
                "id": message_id,
                "result": {
                    "content": [{"type": "text", "text": "sdk-echo-marker:" + text}],
                    "isError": False,
                },
            }
        )
        return
    if message_id is not None:
        write_message(
            {
                "jsonrpc": "2.0",
                "id": message_id,
                "error": {"code": -32601, "message": "method not found"},
            }
        )


def main():
    while True:
        message = read_message()
        if message is None:
            return
        handle(message)


if __name__ == "__main__":
    main()
