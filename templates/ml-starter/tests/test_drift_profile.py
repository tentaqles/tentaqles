"""drift_profile.py writes tq.drift-profile/v1 profiles that reuse the reference edges."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

import pandas as pd
import pytest

import drift_profile
from ml_starter import synth

TEMPLATE = Path(__file__).resolve().parents[1]
SRC = TEMPLATE / "src"


def _frame() -> pd.DataFrame:
    return pd.DataFrame({
        "num": [float(i) for i in range(100)],
        "cat": ["a"] * 60 + ["b"] * 40,
        "who": [f"tester{i % 3}" for i in range(100)],
    })


def test_reference_profile_shape() -> None:
    prof = drift_profile.build_profile(_frame(), source="ref", created_at="2026-01-01T00:00:00Z")
    assert prof["schema"] == "tq.drift-profile/v1"
    assert prof["rows"] == 100 and prof["source"] == "ref" and prof["created_at"] == "2026-01-01T00:00:00Z"
    num = prof["features"]["num"]
    assert num["type"] == "numeric"
    assert len(num["edges"]) == 11 and len(num["counts"]) == 10
    assert sum(num["counts"]) == 100
    cat = prof["features"]["cat"]
    assert cat == {"type": "categorical", "counts": {"a": 60, "b": 40}, "hashed": False}


def test_current_profile_reuses_the_reference_edges() -> None:
    ref = drift_profile.build_profile(_frame(), source="ref")
    shifted = _frame().assign(num=lambda f: f["num"] + 500)
    cur = drift_profile.build_profile(shifted, reference=ref, source="cur")
    assert cur["features"]["num"]["edges"] == ref["features"]["num"]["edges"]
    # values above the last edge land in the last bin
    assert cur["features"]["num"]["counts"][-1] == 100


def test_classified_columns_are_hashed_and_never_printed() -> None:
    prof = drift_profile.build_profile(_frame(), classified={"who"})
    who = prof["features"]["who"]
    assert who["hashed"] is True
    text = json.dumps(prof)
    assert "tester" not in text
    want = "h:" + hashlib.sha256(b"tester0").hexdigest()[:16]
    assert want in who["counts"]


def test_more_than_fifty_labels_are_hashed() -> None:
    frame = pd.DataFrame({"code": [f"k{i}" for i in range(51)]})
    assert drift_profile.build_profile(frame)["features"]["code"]["hashed"] is True
    frame = pd.DataFrame({"code": [f"k{i}" for i in range(50)]})
    assert drift_profile.build_profile(frame)["features"]["code"]["hashed"] is False


def test_hashing_follows_the_reference() -> None:
    ref = drift_profile.build_profile(_frame(), classified={"cat"})
    cur = drift_profile.build_profile(_frame(), reference=ref)
    assert cur["features"]["cat"]["hashed"] is True
    assert cur["features"]["cat"]["counts"] == ref["features"]["cat"]["counts"]


def test_a_feature_missing_from_current_is_an_error() -> None:
    ref = drift_profile.build_profile(_frame())
    with pytest.raises(drift_profile.ProfileError, match="num"):
        drift_profile.build_profile(_frame().drop(columns=["num"]), reference=ref)


def test_excluded_columns_are_left_out() -> None:
    prof = drift_profile.build_profile(_frame(), exclude={"who"})
    assert set(prof["features"]) == {"num", "cat"}


def test_committed_reference_profile_is_rebuilt_exactly(tmp_path: Path) -> None:
    csv = tmp_path / "churn.csv"
    synth.write_csv(csv, rows=synth.ROWS, seed=synth.SEED)
    committed = json.loads((TEMPLATE / "data" / "profiles" / "churn-train.json").read_text(encoding="utf-8"))
    rebuilt = drift_profile.build_profile(
        drift_profile.read_frame(csv), source=committed["source"], created_at=committed["created_at"],
        exclude={"customer_id", "churned"})
    assert rebuilt == committed


def test_cli_writes_the_current_profile(tmp_path: Path) -> None:
    ref_csv, cur_csv = tmp_path / "ref.csv", tmp_path / "cur.csv"
    synth.write_csv(ref_csv, rows=500, seed=1)
    synth.write_csv(cur_csv, rows=500, seed=2, shift=1.0)
    ref_out, cur_out = tmp_path / "ref.json", tmp_path / ".tq" / "drift" / "cur.json"
    env = {**os.environ, "PYTHONPATH": str(SRC)}
    base = [sys.executable, "-m", "drift_profile", "--exclude", "customer_id,churned"]
    r = subprocess.run(base + ["--data", str(ref_csv), "--out", str(ref_out), "--source", "ref"],
                       capture_output=True, text=True, env=env, timeout=120)
    assert r.returncode == 0, r.stderr
    r = subprocess.run(base + ["--data", str(cur_csv), "--reference", str(ref_out), "--out", str(cur_out)],
                       capture_output=True, text=True, env=env, timeout=120)
    assert r.returncode == 0, r.stderr
    ref, cur = (json.loads(p.read_text(encoding="utf-8")) for p in (ref_out, cur_out))
    assert cur["features"]["tenure_days"]["edges"] == ref["features"]["tenure_days"]["edges"]
    assert cur["features"]["tenure_days"]["counts"] != ref["features"]["tenure_days"]["counts"]


def test_prediction_distribution_is_added_with_a_model(project: Path) -> None:
    from ml_starter import train

    train.train(project)
    frame = drift_profile.read_frame(project / "data" / "raw" / "churn.csv")
    prof = drift_profile.build_profile(frame, exclude={"customer_id", "churned"},
                                       model_path=project / "models" / "churn" / "model.skops")
    pred = prof["features"]["__prediction__"]
    assert pred["type"] == "numeric" and sum(pred["counts"]) == synth.ROWS
