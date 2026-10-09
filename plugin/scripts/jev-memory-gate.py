#!/usr/bin/env python3
"""
Detached worker for the shadow Jev memory gates (tentaqles.memory.jev_gate).

Started by knowledge-capture.py (capture gate) and session-preamble.py
(recall gate) through _detach.spawn_detached, never by hooks.json: the
hook returns at once and this process makes the bounded `tq decide ask` /
`tq decide log` calls on its own time.

Reads one JSON payload on stdin ({"kind": "capture"|"recall", ...}). Logs
what Jev would decide; changes nothing. Always exits 0.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import json


def main() -> None:
    try:
        payload = json.loads(sys.stdin.read() or "{}")
    except (ValueError, TypeError):
        return
    if not isinstance(payload, dict):
        return
    from tentaqles.memory import jev_gate

    if not jev_gate.enabled():
        return
    kind = payload.get("kind")
    if kind == "capture":
        jev_gate.run_capture(payload)
    elif kind == "recall":
        jev_gate.run_recall(payload)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        pass
    sys.exit(0)
