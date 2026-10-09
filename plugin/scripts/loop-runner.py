#!/usr/bin/env python3
"""Overnight improvement loop for /tentaqles:loop.

    bash "<plugin>/scripts/tq_run.sh" loop-runner.py approve --feature F --checks checks/test_f.py \
        --score-cmd "{python} -m pytest checks/test_f.py -q -p no:cacheprovider --junitxml={xml}"
    bash "<plugin>/scripts/tq_run.sh" loop-runner.py run --feature F --workspace <ws> \
        --budget-usd 10 --max-hours 6 --max-rounds 10 --signal
    bash "<plugin>/scripts/tq_run.sh" loop-runner.py status --feature F

See tentaqles/workflow/loop.py for the safety rails.
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths  # noqa: E402

setup_paths()

from tentaqles.workflow.loop import main  # noqa: E402

if __name__ == "__main__":
    sys.exit(main())
