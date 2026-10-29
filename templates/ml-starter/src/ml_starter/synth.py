"""Synthetic churn data, generated at test time and pinned by sha256 in data/snapshots.yaml.

The generator uses the stdlib Mersenne Twister and fixed formatting, so the same
(rows, seed) gives the same bytes on every platform and Python version. No personal
data: ids are `c00001`-style counters, there are no names, e-mails or dates.

    python -m ml_starter.synth --out data/raw/churn.csv
    python -m ml_starter.synth --out data/raw/churn-current.csv --seed 8 --shift 0.5
"""

from __future__ import annotations

import argparse
import csv
import math
import random
from pathlib import Path

ROWS = 2000
SEED = 7
COLUMNS = ["customer_id", "tenure_days", "monthly_spend", "support_tickets", "plan", "region", "churned"]
PLANS = ["basic", "plus", "pro"]
REGIONS = ["north", "south", "east", "west"]


def generate(n: int, seed: int, shift: float = 0.0) -> list[list[str]]:
    """`shift` moves the population (shorter tenure, more tickets) to simulate drift."""
    rnd = random.Random(seed)
    out = []
    for i in range(n):
        plan = PLANS[min(2, int(rnd.random() * 3))]
        region = REGIONS[min(3, int(rnd.random() * 4))]
        tenure = max(1, int(rnd.expovariate(1 / (400.0 * (1 - 0.5 * shift)))))
        spend = round(10.0 + 20.0 * PLANS.index(plan) + rnd.random() * 15.0, 2)
        tickets = min(10, int(rnd.expovariate(1 / (1.0 + 2.0 * shift))))
        z = 0.8 - 0.004 * tenure + 0.45 * tickets - 0.5 * PLANS.index(plan) + 0.01 * (spend - 30.0)
        churned = 1 if rnd.random() < 1 / (1 + math.exp(-z)) else 0
        out.append([f"c{i + 1:05d}", str(tenure), f"{spend:.2f}", str(tickets), plan, region, str(churned)])
    return out


def write_csv(path: Path, rows: int = ROWS, seed: int = SEED, shift: float = 0.0) -> Path:
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as fh:
        w = csv.writer(fh, lineterminator="\n")
        w.writerow(COLUMNS)
        w.writerows(generate(rows, seed, shift))
    return path


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--rows", type=int, default=ROWS)
    ap.add_argument("--seed", type=int, default=SEED)
    ap.add_argument("--shift", type=float, default=0.0)
    a = ap.parse_args(argv)
    write_csv(a.out, a.rows, a.seed, a.shift)
    print(f"wrote {a.rows} rows to {a.out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
