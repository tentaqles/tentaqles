"""Train the churn model from the pinned snapshot.

    uv run python -m ml_starter.train

Writes reports/churn/metrics.json (the file packs.ml.models[].metrics points at),
reports/churn/run.json (params, seed, snapshot, lock hash, git commit, command) and
models/churn/model.skops (a safe format; load it with ml_starter.serve.load_model).
"""

from __future__ import annotations

import argparse
import hashlib
import json
import platform
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

import skops.io as sio
from sklearn.linear_model import LogisticRegression
from sklearn.model_selection import train_test_split
from sklearn.pipeline import Pipeline

from ml_starter import MODEL, SEED, data
from ml_starter.evaluate import evaluate
from ml_starter.features import build_preprocessor, features, target
from ml_starter.seed import set_seed

SNAPSHOT = "churn-train"
TEST_SIZE = 0.2
PARAMS = {"C": 1.0, "max_iter": 1000, "test_size": TEST_SIZE}
COMMAND = "uv run python -m ml_starter.train"


@dataclass
class Result:
    model: Pipeline
    metrics: dict
    rows_train: int
    rows_test: int


def build_pipeline(seed: int = SEED) -> Pipeline:
    return Pipeline([
        ("prep", build_preprocessor()),
        ("clf", LogisticRegression(C=PARAMS["C"], max_iter=PARAMS["max_iter"], random_state=seed)),
    ])


def _git_commit(root: Path) -> str:
    try:
        r = subprocess.run(["git", "rev-parse", "HEAD"], cwd=root, capture_output=True, text=True, timeout=10)
    except (OSError, subprocess.TimeoutExpired):
        return "unknown"
    return r.stdout.strip() if r.returncode == 0 else "unknown"


def _lock_sha256(root: Path) -> str:
    lock = root / "uv.lock"
    return hashlib.sha256(lock.read_bytes()).hexdigest() if lock.is_file() else "missing"


def _write_json(path: Path, obj: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes((json.dumps(obj, indent=2, sort_keys=True) + "\n").encode("utf-8"))  # LF on every OS


def train(root: Path = Path("."), seed: int = SEED, snapshot: str = SNAPSHOT) -> Result:
    root = Path(root)
    rng = set_seed(seed)
    path = data.snapshot_path(root, snapshot)
    frame = data.load_frame(path)
    x_all, y_all = features(frame), target(frame)
    x_train, x_holdout, y_train, y_holdout = train_test_split(
        x_all, y_all, test_size=TEST_SIZE, stratify=y_all, random_state=seed)

    model = build_pipeline(seed)
    model.fit(x_train, y_train)

    # The holdout is touched once, here, for the final numbers.
    scores = model.predict_proba(x_holdout)[:, 1]
    metrics = evaluate(scores, y_holdout, prevalence=float(y_train.mean()), slices=x_holdout["plan"], rng=rng)
    sha = data.sha256_file(path)
    metrics.update({"model": MODEL, "snapshot": snapshot, "snapshot_sha256": sha, "seed": seed})

    reports = root / "reports" / MODEL
    _write_json(reports / "metrics.json", metrics)
    _write_json(reports / "run.json", {
        "params": PARAMS, "seed": seed, "snapshot": snapshot, "snapshot_sha256": sha,
        "lock_sha256": _lock_sha256(root), "git_commit": _git_commit(root), "command": COMMAND,
        "python": platform.python_version(), "rows_train": int(len(y_train)),
    })
    out = root / "models" / MODEL / "model.skops"
    out.parent.mkdir(parents=True, exist_ok=True)
    sio.dump(model, out)
    return Result(model=model, metrics=metrics, rows_train=int(len(y_train)), rows_test=int(len(y_holdout)))


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description="Train the churn model from the pinned snapshot.")
    ap.add_argument("--root", type=Path, default=Path("."))
    ap.add_argument("--seed", type=int, default=SEED)
    a = ap.parse_args(argv)
    try:
        res = train(a.root, seed=a.seed)
    except (data.SnapshotMismatch, KeyError, FileNotFoundError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    m = res.metrics
    print(f"auc {m['auc']} [{m['auc_ci_low']}, {m['auc_ci_high']}] brier {m['brier']} "
          f"(baseline brier {m['baseline_brier']}) on {res.rows_test} holdout rows")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

