"""The SessionEnd worker writes the vault and ingests configured folders.

Runs the worker exactly as the hook spawns it (`session-end.py --worker`),
with TENTAQLES_VAULT_DIR pointing at tmp and the shared knowledge.yaml naming
an ingest folder for this client. Semantic facts are disabled so no model is
called. The vault export script used by the skill is exercised the same way.
"""

from __future__ import annotations

import json
import os
import sqlite3
import subprocess
import sys
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SCRIPTS = PLUGIN_ROOT / "scripts"


def _env(tmp_path: Path, vault: Path, tq_home: Path) -> dict:
    env = dict(os.environ)
    env["CLAUDE_PLUGIN_ROOT"] = str(PLUGIN_ROOT)
    env["CLAUDE_PLUGIN_DATA"] = str(tmp_path / "data")
    env["TQ_HOME"] = str(tq_home)
    env["PYTHONPATH"] = str(PLUGIN_ROOT)
    env["TENTAQLES_VAULT_DIR"] = str(vault)
    env["TENTAQLES_SEMANTIC_FACTS"] = "0"
    env["TENTAQLES_JEV_MEMORY"] = "0"
    env.pop("TENTAQLES_HEADLESS_CHILD", None)
    return env


def _workspace(tmp_path: Path) -> tuple[Path, Path, Path]:
    ws = tmp_path / "ws"
    ws.mkdir()
    (ws / ".tentaqles.yaml").write_text("schema: tentaqles-client-v2\nclient: acme\ndisplay_name: Acme\n", encoding="utf-8")
    clips = tmp_path / "clips"
    clips.mkdir()
    (clips / "note.md").write_text("---\ntitle: Queue retries\n---\nRetry webhooks from a queue.\n", encoding="utf-8")
    tq_home = tmp_path / "tq-home"
    tq_home.mkdir()
    (tq_home / "knowledge.yaml").write_text(f"ingest:\n  acme: ['{clips.as_posix()}']\n", encoding="utf-8")
    return ws, clips, tq_home


def test_worker_exports_vault_and_ingests_clippings(tmp_path: Path) -> None:
    ws, clips, tq_home = _workspace(tmp_path)
    vault = tmp_path / "vault"
    payload = json.dumps({"session_id": "s1", "cwd": str(ws), "transcript_path": "", "reason": "prompt_input_exit"}).encode()

    done = subprocess.run(
        [sys.executable, str(SCRIPTS / "session-end.py"), "--worker", str(ws)],
        input=payload, capture_output=True, env=_env(tmp_path, vault, tq_home), cwd=str(tmp_path), timeout=180,
    )

    assert done.returncode == 0, done.stderr.decode(errors="replace")
    client_dir = vault / "acme"
    assert (client_dir / "context.md").exists(), sorted(p.name for p in vault.rglob("*"))
    assert (client_dir / "decisions.md").exists() and (client_dir / "plan.md").exists()
    assert list((client_dir / "sessions").glob("*.md")), "the session just ended gets its note"
    db = sqlite3.connect(str(ws / ".claude" / "memory.db"))
    try:
        assert db.execute("SELECT COUNT(*) FROM reference_docs").fetchone()[0] == 1
    finally:
        db.close()


def test_export_script_reports_what_it_did(tmp_path: Path) -> None:
    ws, clips, tq_home = _workspace(tmp_path)
    vault = tmp_path / "vault"

    done = subprocess.run(
        [sys.executable, str(SCRIPTS / "vault-export.py"), "--cwd", str(ws)],
        capture_output=True, text=True, env=_env(tmp_path, vault, tq_home), cwd=str(tmp_path), timeout=180,
    )

    assert done.returncode == 0, done.stderr
    assert "acme" in done.stdout and str(vault) in done.stdout
    assert "ingested 1" in done.stdout.lower() or "added 1" in done.stdout.lower()
    assert (vault / "acme" / "context.md").exists()


def test_export_script_says_so_when_nothing_is_configured(tmp_path: Path) -> None:
    ws = tmp_path / "ws"
    ws.mkdir()
    (ws / ".tentaqles.yaml").write_text("schema: tentaqles-client-v2\nclient: acme\n", encoding="utf-8")
    env = _env(tmp_path, tmp_path / "unused", tmp_path / "tq-home")
    env.pop("TENTAQLES_VAULT_DIR")

    done = subprocess.run(
        [sys.executable, str(SCRIPTS / "vault-export.py"), "--cwd", str(ws)],
        capture_output=True, text=True, env=env, cwd=str(tmp_path), timeout=120,
    )

    assert done.returncode == 0, done.stderr
    assert "not configured" in done.stdout.lower()
    assert "knowledge.yaml" in done.stdout
