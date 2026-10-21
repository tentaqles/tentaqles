#!/usr/bin/env python3
"""Print docs/prd.md status as JSON. Exit 0 planning-ready, 3 not ready, 4 no PRD.

Used by the kickoff, feature and prd skills, and by the SessionStart preamble.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import argparse
import json
from pathlib import Path

from tentaqles.workflow.prd import prd_status


def main() -> int:
    ap = argparse.ArgumentParser(prog="prd-status")
    ap.add_argument("--cwd", default=os.getcwd(), help="repository root holding docs/prd.md")
    a = ap.parse_args()
    st = prd_status(Path(a.cwd))
    print(json.dumps(st, ensure_ascii=False))
    if not st["exists"]:
        return 4
    return 0 if st["planning_ready"] else 3


if __name__ == "__main__":
    raise SystemExit(main())
