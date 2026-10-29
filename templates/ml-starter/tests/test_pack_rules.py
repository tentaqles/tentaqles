"""The template keeps to the ml pack's rules (spec C §4.2, §4.3), checked here without tq.

These are text checks over the template's own code, so the template stays clean while the
Go lints evolve. When a tq with the ml pack is on PATH and TQ_ML_STARTER_PACK_CHECK=1, the
last test also runs `tq pack check --all --builtin-only --pack ml` over a copy.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest
import yaml

TEMPLATE = Path(__file__).resolve().parents[1]
CODE = sorted([*TEMPLATE.glob("src/**/*.py"), *TEMPLATE.glob("scripts/*.py")])
TRAINING = [TEMPLATE / "src" / "ml_starter" / name for name in ("train.py", "features.py", "evaluate.py")]
ESTIMATORS = ("LogisticRegression", "DummyClassifier", "RandomForestClassifier", "KFold", "StratifiedKFold")


def _src(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def test_code_files_found() -> None:
    assert len(CODE) >= 8


@pytest.mark.parametrize("path", CODE, ids=lambda p: p.name)
def test_no_unsafe_deserialization(path: Path) -> None:
    text = _src(path)
    for call in ("pickle.load", "joblib.load", "cloudpickle.load", "torch.load("):
        assert call not in text, (path.name, call)


@pytest.mark.parametrize("path", CODE, ids=lambda p: p.name)
def test_no_fit_transform_outside_a_pipeline(path: Path) -> None:
    assert ".fit_transform(" not in _src(path)


@pytest.mark.parametrize("path", CODE, ids=lambda p: p.name)
def test_every_split_and_estimator_gets_random_state(path: Path) -> None:
    text = _src(path)
    for name in ("train_test_split", *ESTIMATORS):
        for m in re.finditer(rf"\b{name}\((.*?)\)", text, re.S):
            assert "random_state=" in m.group(1), (path.name, name)


@pytest.mark.parametrize("path", CODE, ids=lambda p: p.name)
def test_no_legacy_numpy_global_seed(path: Path) -> None:
    assert "np.random.seed(" not in _src(path)


def test_training_calls_the_declared_seed_helper() -> None:
    cfg = yaml.safe_load(_src(TEMPLATE / "tq-packs.yaml"))["packs"]["ml"]
    module, func = cfg["seed_helper"].split(":")
    assert f"def {func}(" in _src(TEMPLATE / module)
    assert f"{func}(" in _src(TEMPLATE / "src" / "ml_starter" / "train.py")


def test_promotion_api_only_in_the_promotion_script() -> None:
    for path in CODE:
        if path.name == "promote.py":
            continue
        text = _src(path)
        for api in ("set_registered_model_alias", "transition_model_version_stage", "registered-models/alias"):
            assert api not in text, (path.name, api)


def test_training_reads_only_pinned_snapshots() -> None:
    manifest = yaml.safe_load(_src(TEMPLATE / "data" / "snapshots.yaml"))
    for entry in manifest.values():
        assert re.fullmatch(r"[0-9a-f]{64}", entry["sha256"])
    for path in TRAINING:
        text = _src(path)
        assert "read_csv(" not in text and "read_parquet(" not in text, path.name  # only via data.snapshot_path
    assert "snapshot_path(" in _src(TEMPLATE / "src" / "ml_starter" / "train.py")


def test_lock_file_is_present_and_covers_the_dependencies() -> None:
    lock = _src(TEMPLATE / "uv.lock")
    for dep in ("numpy", "pandas", "scikit-learn", "skops", "pyyaml"):
        assert f'name = "{dep}"' in lock, dep


def test_run_config_is_written_next_to_the_metrics() -> None:
    text = _src(TEMPLATE / "src" / "ml_starter" / "train.py")
    assert "run.json" in text and "metrics.json" in text


@pytest.mark.skipif(shutil.which("ruff") is None, reason="ruff not installed")
def test_ruff_numpy_and_pandas_rules_are_clean() -> None:
    r = subprocess.run(["ruff", "check", "--select", "NPY,PD", "--output-format", "json", "--no-cache",
                        "src", "scripts", "tests"], cwd=TEMPLATE, capture_output=True, text=True, timeout=60)
    assert r.returncode == 0, json.loads(r.stdout or "[]")


@pytest.mark.skipif(os.environ.get("TQ_ML_STARTER_PACK_CHECK") != "1" or shutil.which("tq") is None,
                    reason="set TQ_ML_STARTER_PACK_CHECK=1 with an ml-pack tq on PATH")
def test_tq_builtin_ml_checks_have_no_error_findings(project: Path) -> None:
    for args in (["init", "-q"], ["add", "-A"], ["-c", "user.email=tester@acme.test", "-c", "user.name=tester",
                                                 "commit", "-q", "-m", "init"]):
        subprocess.run(["git", *args], cwd=project, check=True, capture_output=True, timeout=30)
    r = subprocess.run(["tq", "pack", "check", "--all", "--builtin-only", "--pack", "ml", "--json", "--budget", "60s"],
                       cwd=project, capture_output=True, text=True, timeout=90)
    out = json.loads(r.stdout)
    errors = [(res["id"], f) for res in out["results"] for f in res.get("findings") or [] if f["level"] == "error"]
    assert errors == [], errors
