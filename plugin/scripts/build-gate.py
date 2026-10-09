#!/usr/bin/env python3
"""Evidence-based checkpoint gate for /tentaqles:build.

As a hook (PreToolUse + Stop, installed into the project's
.claude/settings.local.json by `build-gate setup`):

    bash "<plugin>/scripts/tq_run.sh" build-gate.py hook

As a CLI (run by the orchestrator through Bash):

    build-gate.py setup | ping
    build-gate.py approve <slug>                      (the hook asks the human)
    build-gate.py lock-tests <slug> <id> <files...> [--none] [--action ask|deny]
    build-gate.py unlock-tests <slug> <files...>      (the hook asks the human)
    build-gate.py verify <slug> <id> [--timeout S]
    build-gate.py status <slug>
    build-gate.py review-bundle <slug> <id>
    build-gate.py serve <slug> [--port N]             (127.0.0.1 only)
    build-gate.py finish <slug>                       (the hook asks the human)

Standard library only; see tentaqles/workflow/gate.py.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths  # noqa: E402

setup_paths()

from tentaqles.workflow.gate import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
