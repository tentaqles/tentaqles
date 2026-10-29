"""Fixtures: a copy of the template in a temp dir, with synthetic data generated at test time."""

from __future__ import annotations

import shutil
import sys
from pathlib import Path

import pytest

TEMPLATE = Path(__file__).resolve().parents[1]
SRC = TEMPLATE / "src"
if str(SRC) not in sys.path:  # runnable without `pip install -e`
    sys.path.insert(0, str(SRC))

IGNORE = shutil.ignore_patterns(".venv", "__pycache__", ".pytest_cache", ".ruff_cache", "*.egg-info",
                                "*.csv", "*.parquet", "*.skops", ".tq")


@pytest.fixture(autouse=True)
def _isolate_home(tmp_path_factory: pytest.TempPathFactory, monkeypatch: pytest.MonkeyPatch) -> None:
    home = tmp_path_factory.mktemp("home")
    for var in ("HOME", "USERPROFILE", "TQ_HOME"):
        monkeypatch.setenv(var, str(home))
    for var in ("MLFLOW_TRACKING_URI", "MLFLOW_TRACKING_TOKEN", "MLFLOW_TRACKING_USERNAME",
                "MLFLOW_TRACKING_PASSWORD", "MLFLOW_HTTP_REQUEST_TIMEOUT"):
        monkeypatch.delenv(var, raising=False)


@pytest.fixture
def project(tmp_path: Path) -> Path:
    """A fresh copy of the template with data/raw/churn.csv generated from the pinned recipe."""
    from ml_starter import synth

    root = tmp_path / "ml-starter"
    shutil.copytree(TEMPLATE, root, ignore=IGNORE)
    synth.write_csv(root / "data" / "raw" / "churn.csv", rows=synth.ROWS, seed=synth.SEED)
    return root
