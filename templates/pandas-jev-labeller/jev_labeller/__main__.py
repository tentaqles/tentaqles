"""Label a CSV from the command line.

    python -m jev_labeller examples/tickets.csv --task examples/task.json --dry-run
    tq dotenv run -- python -m jev_labeller examples/tickets.csv --task examples/task.json \
        --out labelled.csv --review review.csv --cache labels.sqlite
"""

from __future__ import annotations

import argparse
import logging
import sys

import pandas as pd

from .cache import MemoryCache, SqliteCache
from .jev import JevClient
from .labeller import Labeller, LabelTask


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="python -m jev_labeller")
    p.add_argument("csv")
    p.add_argument("--task", required=True, help="LabelTask JSON")
    p.add_argument("--out", help="labelled CSV (default: print a preview)")
    p.add_argument("--review", help="CSV for low-confidence / failed rows")
    p.add_argument("--cache", help="SQLite cache file (default: in-memory)")
    p.add_argument("--batch-size", type=int, default=20)
    p.add_argument("--threshold", type=float, default=0.7, help="review below this confidence")
    p.add_argument("--dry-run", action="store_true", help="estimate requests and cost, call nothing")
    a = p.parse_args(argv)
    logging.basicConfig(level=logging.WARNING, format="%(levelname)s %(message)s")

    df = pd.read_csv(a.csv)
    task = LabelTask.from_json(a.task)
    labeller = Labeller(
        JevClient(),
        task,
        batch_size=a.batch_size,
        review_threshold=a.threshold,
        cache=SqliteCache(a.cache) if a.cache else MemoryCache(),
    )
    res = labeller.label(df, dry_run=a.dry_run, review_path=a.review)
    print(res.stats.summary())
    if a.dry_run:
        return 0
    if a.out:
        res.df.to_csv(a.out, index=False)
    else:
        print(res.df.head(10).to_string())
    return 1 if res.stats.failed else 0


if __name__ == "__main__":
    sys.exit(main())
