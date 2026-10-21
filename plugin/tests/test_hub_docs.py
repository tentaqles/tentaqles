"""The hub ships with its docs: README row, changelog entry, skill count."""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_readme_lists_the_hub_skill():
    assert "| `/tentaqles:hub` |" in (ROOT / "README.md").read_text(encoding="utf-8")


def test_changelog_unreleased_mentions_the_hub():
    text = (ROOT / "CHANGELOG.md").read_text(encoding="utf-8")
    unreleased = text.split("## [Unreleased]", 1)[1].split("\n## [", 1)[0]
    for needle in ("/tentaqles:hub", "TQ_HUB_TASK", "TQ_HUB_RELOCATE", "--records"):
        assert needle in unreleased, needle


def test_claude_md_skill_count_matches_the_tree():
    n = len([p for p in (ROOT / "skills").iterdir() if (p / "SKILL.md").is_file()])
    m = re.search(r"(\d+) skill directories", (ROOT / "CLAUDE.md").read_text(encoding="utf-8"))
    assert m and int(m.group(1)) == n, (m and m.group(1), n)
