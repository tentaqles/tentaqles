"""/tentaqles:sync (spec B §9, §12): the MCP fallback loop. Bounded tq calls, import/pending/ack
only, and never an answer to a decision or a tracker CLI write."""

from __future__ import annotations

import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
SYNC = ROOT / "skills" / "sync" / "SKILL.md"


def _frontmatter(path: Path) -> dict:
    parts = re.split(r"^---\s*$", path.read_text(encoding="utf-8"), maxsplit=2, flags=re.MULTILINE)
    return yaml.safe_load(parts[1])


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, flags=re.DOTALL)


def test_sync_skill_frontmatter():
    fm = _frontmatter(SYNC)
    assert fm["name"] == "sync"
    desc = fm["description"]
    assert 40 <= len(desc) <= 1024
    for phrase in ("/tentaqles:sync", "MCP", "tq hub sync import", "tq hub sync ack", "Never answers"):
        assert phrase in desc, phrase


def test_sync_skill_runs_the_fallback_loop_in_order():
    text = SYNC.read_text(encoding="utf-8")
    code = "\n".join(_bash_blocks(text))  # the commands the skill runs, in order
    order = ["tq hub sync status", "tq hub sync import -", "tq hub sync pending", "tq hub sync ack"]
    pos = [code.index(s) for s in order]
    assert pos == sorted(pos), pos
    assert "--sent" in text and "--failed" in text and "--item-id" in text and "base_status" in text


def test_sync_skill_bash_is_bounded_and_never_answers_or_writes_by_cli():
    blocks = _bash_blocks(SYNC.read_text(encoding="utf-8"))
    assert len(blocks) >= 4
    for block in blocks:
        for line in block.splitlines():
            if line.startswith("tq "):
                assert "--timeout" in line, line
        assert "tq hub answer" not in block
        assert "tq hub sync retry" not in block
        assert not re.search(r"\b(az\s+boards|gh\s+issue|gh\s+project|acli\s+jira)\b", block), block


def test_sync_skill_keeps_tokens_and_decisions_with_the_human():
    text = SYNC.read_text(encoding="utf-8")
    assert "Never answer a send/skip decision" in text
    assert "Never print, store or ask for a token" in text
