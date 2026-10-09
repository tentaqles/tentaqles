#!/usr/bin/env python3
"""PreToolUse guard for the overnight loop's headless claude child.

Installed by loop-runner.py into the run's own --settings file (never into the
plugin's hooks.json); reads its config from $TQ_LOOP_GUARD. Exit 2 blocks.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths  # noqa: E402

setup_paths()

from tentaqles.workflow.loopguard import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
