#!/usr/bin/env python3
"""Compaction cron — runs memory consolidation for all registered workspaces.

Iterates every workspace in the metagraph config, instantiates MemoryStore,
calls MemoryConsolidator.maybe_compact(), and appends a log line to
{data_dir}/compaction.log.

Intended for use as a scheduled task or manual CLI trigger.

Usage:
    python scripts/compaction-cron.py
    python scripts/compaction-cron.py --every-n 5
"""

from __future__ import annotations

import argparse
import os
import sys
from datetime import datetime, timezone
from pathlib import Path

# Bootstrap sys.path for plugin imports (tentaqles.* + bootstrapped deps)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
plugin_root, plugin_data = setup_paths()


def _now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _log(log_path: Path, line: str) -> None:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with open(log_path, "a", encoding="utf-8") as f:
        f.write(line + "\n")


def run(every_n: int = 10) -> None:
    from tentaqles.metagraph.config import list_workspaces
    from tentaqles.memory.store import MemoryStore
    from tentaqles.memory.consolidator import MemoryConsolidator
    from tentaqles.config import data_dir

    log_path = data_dir() / "compaction.log"
    workspaces = list_workspaces()

    if not workspaces:
        msg = f"{_now_iso()} [compaction-cron] no workspaces registered — nothing to compact"
        print(msg)
        _log(log_path, msg)
        return

    for ws_id, ws_info in workspaces.items():
        root_path = ws_info.get("root_path", "")
        if not root_path or not Path(root_path).exists():
            msg = f"{_now_iso()} [compaction-cron] SKIP {ws_id}: path not found ({root_path!r})"
            print(msg)
            _log(log_path, msg)
            continue

        try:
            store = MemoryStore(root_path)
            consolidator = MemoryConsolidator(store)
            result = consolidator.maybe_compact(every_n_sessions=every_n)
            store.close()

            msg = (
                f"{_now_iso()} [compaction-cron] {ws_id}: "
                f"compacted={result['compacted']} "
                f"facts_added={result['facts_added']} "
                f"patterns_found={result['patterns_found']} "
                f"evicted={result['evicted']}"
            )
            print(msg)
            _log(log_path, msg)

        except Exception as exc:  # noqa: BLE001
            msg = f"{_now_iso()} [compaction-cron] ERROR {ws_id}: {exc}"
            print(msg, file=sys.stderr)
            _log(log_path, msg)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Run memory compaction for all registered workspaces."
    )
    parser.add_argument(
        "--every-n",
        type=int,
        default=10,
        help="Compact when unsourced episodic session count is a multiple of N (default: 10).",
    )
    args = parser.parse_args()
    run(every_n=args.every_n)


if __name__ == "__main__":
    main()
