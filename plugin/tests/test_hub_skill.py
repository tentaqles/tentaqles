"""/tentaqles:hub and ship's PR comment (spec A §13.3): bounded tq hub calls, and the
human-only rule (the skill prints the answer command; it never runs it)."""

from __future__ import annotations

import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
HUB = ROOT / "skills" / "hub" / "SKILL.md"
SHIP = ROOT / "skills" / "ship" / "SKILL.md"


def _frontmatter(path: Path) -> dict:
    parts = re.split(r"^---\s*$", path.read_text(encoding="utf-8"), maxsplit=2, flags=re.MULTILINE)
    return yaml.safe_load(parts[1])


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, flags=re.DOTALL)


def test_hub_skill_frontmatter():
    fm = _frontmatter(HUB)
    assert fm["name"] == "hub"
    desc = fm["description"]
    assert 40 <= len(desc) <= 1024
    for phrase in ("/tentaqles:hub", "waiting", "tq hub answer", "never runs it"):
        assert phrase in desc, phrase


def test_hub_skill_bash_blocks_are_bounded_and_never_answer():
    blocks = _bash_blocks(HUB.read_text(encoding="utf-8"))
    assert len(blocks) >= 3
    for block in blocks:
        for line in block.splitlines():
            if "tq hub" in line:
                assert "--timeout 2s" in line, line
        assert "tq hub answer" not in block
        assert "build-gate" not in block
        assert "--budget" not in block
        assert "move" not in block


def test_hub_skill_prints_the_answer_command_for_the_user():
    text = HUB.read_text(encoding="utf-8")
    assert "tq hub answer <card> <decision> --choice <option>" in text
    assert "Never run `tq hub answer`" in text
    assert "tq hub list --waiting --json --timeout 2s" in text


def test_hub_skill_knows_board_sync():
    """Spec B §9: linked cards, the outbox send/skip decisions (human-only) and tq hub push."""
    text = HUB.read_text(encoding="utf-8")
    assert "## 5. Board sync" in text
    sync = text[text.index("## 5. Board sync"):text.index("## Never")]
    for phrase in ("linked card", "decision.data.outbox", "`send`", "`skip`", "tq hub push <card>",
                   "human-only", "HUB.md#board-sync", "/tentaqles:sync"):
        assert phrase in sync, phrase
    never = text[text.index("## Never"):]
    assert "tq hub push" in never
    assert "send/skip" in never


def test_ship_posts_the_pr_link_when_the_hub_launched_the_run():
    text = SHIP.read_text(encoding="utf-8")
    blocks = [b for b in _bash_blocks(text) if "tq hub comment" in b]
    assert len(blocks) == 1
    block = blocks[0]
    assert "TQ_HUB_TASK" in block and "--timeout 2s" in block and "|| true" in block and 'tq hub comment --timeout 2s - "PR: ' in block
    assert text.index("gh pr create --base") < text.index("tq hub comment")
    assert text.index("tq hub comment") < text.index("## 8. Hand off")
