"""Training: pinned snapshot, leakage-free pipeline, reproducible metrics that match the card."""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest
import yaml

from ml_starter import data, synth, train

TEMPLATE = Path(__file__).resolve().parents[1]


def test_generated_data_matches_the_pinned_snapshot(project: Path) -> None:
    manifest = yaml.safe_load((project / "data" / "snapshots.yaml").read_text(encoding="utf-8"))
    entry = manifest["churn-train"]
    assert entry["uri"] == "data/raw/churn.csv"
    assert data.sha256_file(project / entry["uri"]) == entry["sha256"]


def test_a_changed_snapshot_is_refused(project: Path) -> None:
    csv = project / "data" / "raw" / "churn.csv"
    csv.write_text(csv.read_text(encoding="utf-8") + "c99999,1,1.0,0,basic,north,0\n", encoding="utf-8")
    with pytest.raises(data.SnapshotMismatch):
        data.snapshot_path(project, "churn-train")


def test_an_unknown_snapshot_is_refused(project: Path) -> None:
    with pytest.raises(KeyError):
        data.snapshot_path(project, "latest")


def test_synthetic_data_has_no_personal_data(project: Path) -> None:
    header = (project / "data" / "raw" / "churn.csv").read_text(encoding="utf-8").splitlines()[0]
    assert header.split(",") == synth.COLUMNS
    assert "@" not in (project / "data" / "raw" / "churn.csv").read_text(encoding="utf-8")


def test_preprocessing_lives_inside_the_pipeline() -> None:
    from sklearn.pipeline import Pipeline

    model = train.build_pipeline(seed=7)
    assert isinstance(model, Pipeline)
    assert [name for name, _ in model.steps] == ["prep", "clf"]
    assert model.named_steps["clf"].random_state == 7


def test_the_scaler_sees_only_the_training_split(project: Path) -> None:
    result = train.train(project)
    scaler = result.model.named_steps["prep"].named_transformers_["num"]
    assert scaler.n_samples_seen_ == result.rows_train
    assert result.rows_train + result.rows_test == synth.ROWS


def test_training_writes_metrics_run_config_and_a_skops_model(project: Path) -> None:
    train.train(project)
    metrics = json.loads((project / "reports" / "churn" / "metrics.json").read_text(encoding="utf-8"))
    for key in ("auc", "auc_ci_low", "auc_ci_high", "brier", "accuracy", "baseline_auc", "baseline_brier",
                "rows_test", "slices", "snapshot_sha256"):
        assert key in metrics, key
    assert metrics["auc_ci_low"] <= metrics["auc"] <= metrics["auc_ci_high"]
    assert metrics["auc"] > metrics["baseline_auc"]
    assert metrics["brier"] < metrics["baseline_brier"]
    run = json.loads((project / "reports" / "churn" / "run.json").read_text(encoding="utf-8"))
    for key in ("params", "seed", "snapshot", "snapshot_sha256", "lock_sha256", "git_commit", "command", "python"):
        assert key in run, key
    assert (project / "models" / "churn" / "model.skops").is_file()


def test_two_runs_give_the_same_metrics(project: Path) -> None:
    train.train(project)
    first = (project / "reports" / "churn" / "metrics.json").read_text(encoding="utf-8")
    train.train(project)
    assert (project / "reports" / "churn" / "metrics.json").read_text(encoding="utf-8") == first
    assert b"\r" not in (project / "reports" / "churn" / "metrics.json").read_bytes()  # same bytes on every OS


def test_committed_metrics_are_reproduced(project: Path) -> None:
    committed = json.loads((TEMPLATE / "reports" / "churn" / "metrics.json").read_text(encoding="utf-8"))
    train.train(project)
    fresh = json.loads((project / "reports" / "churn" / "metrics.json").read_text(encoding="utf-8"))
    for key in ("auc", "brier", "accuracy", "baseline_brier"):
        assert fresh[key] == pytest.approx(committed[key], abs=2e-3), key
    assert fresh["snapshot_sha256"] == committed["snapshot_sha256"]


def test_model_card_numbers_match_the_metrics_file() -> None:
    card = (TEMPLATE / "models" / "churn" / "MODEL_CARD.md").read_text(encoding="utf-8")
    metrics = json.loads((TEMPLATE / "reports" / "churn" / "metrics.json").read_text(encoding="utf-8"))
    for key in ("auc", "brier", "accuracy"):
        row = re.search(rf"^\| {key} \| ([0-9.]+) \|", card, re.M)
        assert row, key
        assert float(row.group(1)) == pytest.approx(metrics[key], abs=5e-4), key
    assert metrics["snapshot_sha256"] in card


def test_the_saved_model_loads_with_a_reviewed_type_list_and_scores(project: Path) -> None:
    from ml_starter import serve

    train.train(project)
    model = serve.load_model(project / "models" / "churn" / "model.skops")
    frame = data.load_frame(project / "data" / "raw" / "churn.csv").head(5)
    scores = serve.predict_scores(model, frame)
    assert len(scores) == 5 and all(0.0 <= s <= 1.0 for s in scores)


def test_an_untrusted_type_in_the_model_file_is_refused(project: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    from ml_starter import serve

    train.train(project)
    monkeypatch.setattr(serve.sio, "get_untrusted_types", lambda **_: ["builtins.eval"])
    with pytest.raises(serve.UntrustedModel, match="builtins.eval"):
        serve.load_model(project / "models" / "churn" / "model.skops")
