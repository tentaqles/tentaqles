"""Compaction survival: the state block is emitted at SessionStart(source=compact).

Claude Code does not show PreCompact hook output to the model, so the block
pre-compact.py used to print there never arrived. SessionStart output does.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import textwrap
from pathlib import Path

PLUGIN_ROOT = Path(__file__).resolve().parents[1]
PREAMBLE = PLUGIN_ROOT / "scripts" / "session-preamble.py"
HOOKS = PLUGIN_ROOT / "hooks" / "hooks.json"


def _workspace(tmp_path: Path) -> Path:
    (tmp_path / ".tentaqles.yaml").write_text(
        textwrap.dedent(
            """\
            schema: tentaqles-client-v2
            client: acme
            display_name: Acme Corp
            language: en
            """
        ),
        encoding="utf-8",
    )
    sys.path.insert(0, str(PLUGIN_ROOT))
    from tentaqles.memory.store import MemoryStore

    store = MemoryStore(tmp_path)
    store.record_decision(chosen="Use SQLite for the cache", rationale="zero ops vs Redis")
    store.add_pending(description="wire the retry policy", priority="high")
    store.close()
    return tmp_path


def _run(cwd: Path, source: str) -> str:
    env = dict(os.environ)
    extra = [p for p in sys.path if p and ("site-packages" in p or p == str(PLUGIN_ROOT))]
    env["PYTHONPATH"] = os.pathsep.join([str(PLUGIN_ROOT), *extra])
    proc = subprocess.run(
        [sys.executable, str(PREAMBLE), "--memory-only"],
        input=json.dumps({"cwd": str(cwd), "source": source, "hook_event_name": "SessionStart"}),
        capture_output=True,
        text=True,
        timeout=60,
        env=env,
    )
    assert proc.returncode == 0, proc.stderr
    return proc.stdout


def test_compact_source_reinjects_state_block(tmp_path: Path) -> None:
    out = _run(_workspace(tmp_path), "compact")
    assert "restored after compaction" in out
    assert "Use SQLite for the cache" in out
    assert "wire the retry policy" in out
    assert "Acme Corp" in out


def test_startup_source_keeps_regular_preamble(tmp_path: Path) -> None:
    out = _run(_workspace(tmp_path), "startup")
    assert "restored after compaction" not in out


def test_compact_without_memory_prints_nothing_and_creates_no_db(tmp_path: Path) -> None:
    (tmp_path / ".tentaqles.yaml").write_text(
        "schema: tentaqles-client-v2\nclient: acme\n", encoding="utf-8"
    )
    assert _run(tmp_path, "compact").strip() == ""
    assert not (tmp_path / ".claude" / "memory.db").exists()


def test_hooks_json_routes_compaction_through_session_start() -> None:
    hooks = json.loads(HOOKS.read_text(encoding="utf-8"))["hooks"]
    # PreCompact stdout never reaches the model; nothing should rely on it.
    assert "PreCompact" not in hooks
    preamble = [
        (group.get("matcher"), h)
        for group in hooks["SessionStart"]
        for h in group["hooks"]
        if "session-preamble.py" in h["command"]
    ]
    assert preamble, "session-preamble.py must be registered on SessionStart"
    # It must run for source=compact: no matcher, or one that matches "compact".
    import re

    for matcher, _ in preamble:
        assert not matcher or re.search(matcher, "compact")
