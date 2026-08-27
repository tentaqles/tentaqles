#!/usr/bin/env python3
"""Pattern detection cron — runs CrossWorkspacePatternDetector across all registered workspaces.

Designed for Task Scheduler / cron invocation. Safe to run weekly.
Logs a single summary line to {data_dir}/pattern-detection.log.
"""

import os
import sys

# Bootstrap sys.path so tentaqles.* resolves
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

from datetime import datetime, timezone
from pathlib import Path

from tentaqles.config import data_dir
from tentaqles.metagraph.config import list_workspaces
from tentaqles.memory.pattern_detector import CrossWorkspacePatternDetector


def main() -> int:
    workspaces = list_workspaces()
    detector = CrossWorkspacePatternDetector()
    result = detector.run(workspaces)

    log_path = Path(data_dir()) / "pattern-detection.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)

    ts = datetime.now(timezone.utc).isoformat()
    line = (
        f"{ts} | workspaces={len(workspaces)} "
        f"patterns_found={result['patterns_found']} "
        f"output={result['output_path']}\n"
    )
    with open(log_path, "a", encoding="utf-8") as fh:
        fh.write(line)

    print(line.rstrip())
    return 0


if __name__ == "__main__":
    sys.exit(main())
