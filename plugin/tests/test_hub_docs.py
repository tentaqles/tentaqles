"""The hub ships with its docs: README row, changelog entry, skill count."""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_readme_lists_the_hub_skill():
    assert "| `/tentaqles:hub` |" in (ROOT / "README.md").read_text(encoding="utf-8")


def test_changelog_0_13_0_mentions_the_hub():
    text = (ROOT / "CHANGELOG.md").read_text(encoding="utf-8")
    section = text.split("## [0.13.0]", 1)[1].split("\n## [", 1)[0]
    for needle in ("/tentaqles:hub", "TQ_HUB_TASK", "TQ_HUB_RELOCATE", "--records", "headless-manifest-guard"):
        assert needle in section, needle


def test_claude_md_skill_count_matches_the_tree():
    n = len([p for p in (ROOT / "skills").iterdir() if (p / "SKILL.md").is_file()])
    m = re.search(r"(\d+) skill directories", (ROOT / "CLAUDE.md").read_text(encoding="utf-8"))
    assert m and int(m.group(1)) == n, (m and m.group(1), n)
