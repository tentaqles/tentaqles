"""Tests for scripts/session-preamble.py: no mutation, --memory-only flag."""

import json
import os
import subprocess
import sys
import textwrap
from pathlib import Path

import pytest

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
SCRIPT = PLUGIN_ROOT / "scripts" / "session-preamble.py"

MUTATION_STRINGS = [
    "gh auth switch",
    "az account set",
    "git config --global",
    "doctl auth switch",
]


def _workspace(tmp_path: Path) -> Path:
    (tmp_path / ".tentaqles.yaml").write_text(
        textwrap.dedent(
            """\
            schema: tentaqles-client-v2
            client: acme
            display_name: Acme Corp
            language: en
            stack:
              - python
            """
        ),
        encoding="utf-8",
    )
    return tmp_path


def _run_preamble(cwd: Path, *args: str) -> str:
    env = dict(os.environ)
    extra = [p for p in sys.path if p and ("site-packages" in p or p == str(PLUGIN_ROOT))]
    env["PYTHONPATH"] = os.pathsep.join([str(PLUGIN_ROOT), *extra])
    proc = subprocess.run(
        [sys.executable, str(SCRIPT), *args],
        input=json.dumps({"cwd": str(cwd)}),
        capture_output=True,
        text=True,
        timeout=60,
        env=env,
    )
    assert proc.returncode == 0, proc.stderr
    return proc.stdout


def test_source_has_no_mutation_calls():
    src = SCRIPT.read_text(encoding="utf-8")
    for needle in MUTATION_STRINGS:
        assert needle not in src, f"mutation string still present: {needle}"
    assert "includeIf" not in src


def test_default_output_contains_client_header(tmp_path):
    ws = _workspace(tmp_path)
    out = _run_preamble(ws)
    assert "Client:" in out


def test_memory_only_omits_client_header(tmp_path):
    ws = _workspace(tmp_path)
    out = _run_preamble(ws, "--memory-only")
    assert "Client:" not in out
    assert "Auto-switch" not in out
