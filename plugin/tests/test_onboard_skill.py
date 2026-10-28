"""/tentaqles:onboard (spec B §9): offers every login the manifest needs and verifies it, never
handles a token, and ends with a table of what is connected."""

from __future__ import annotations

import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
SKILL = ROOT / "skills" / "onboard" / "SKILL.md"


def _frontmatter(path: Path) -> dict:
    parts = re.split(r"^---\s*$", path.read_text(encoding="utf-8"), maxsplit=2, flags=re.MULTILINE)
    return yaml.safe_load(parts[1])


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, flags=re.DOTALL)


def test_onboard_skill_frontmatter():
    fm = _frontmatter(SKILL)
    assert fm["name"] == "onboard"
    desc = fm["description"]
    assert 40 <= len(desc) <= 1024
    for phrase in ("/tentaqles:onboard", "tq login", "tq secret set", "never handles token", "connected"):
        assert phrase in desc, phrase


def test_onboard_skill_covers_every_provider_kind():
    text = SKILL.read_text(encoding="utf-8")
    for phrase in (
        "tq login <ws> <id>",
        "tq secret set <ws> <provider>",
        "tq doctor --json",
        "tracker-ok",
        "tracker-needs-session",
        "chat-mcp-missing",
        "email-mcp-missing",
        "tq bundle catalog",
        "/mcp",
        "read-only",
        "acli jira auth login",
    ):
        assert phrase in text, phrase
    for connector in ("Slack", "Teams", "Gmail", "Outlook", "Linear", "Asana", "Atlassian", "Notion"):
        assert connector in text, connector


def test_onboard_skill_ends_with_the_connected_table():
    text = SKILL.read_text(encoding="utf-8")
    assert "| Service | Provider | Connected through | Status | Next step |" in text
    assert text.index("tq doctor --json") < text.index("| Service | Provider |")


def test_onboard_skill_runs_no_human_only_command():
    text = SKILL.read_text(encoding="utf-8")
    blocks = _bash_blocks(text)
    assert blocks, "the skill runs tq doctor in a bash block"
    for block in blocks:
        for forbidden in ("tq login", "tq allow", "tq secret set", "tq bundle sync", "auth login", "az login"):
            assert forbidden not in block, forbidden
        assert "env" not in re.findall(r"\b\w+\b", block)[:1], "never dump the environment"
        assert "printenv" not in block and "set |" not in block
    assert "Never run `tq login`" in text
    assert "never ask for" in text.lower() and "token" in text.lower()


def test_onboard_skill_never_opens_files_that_can_hold_connector_tokens():
    # Bundle MCP entries are verbatim mcpServers objects whose env can hold tokens, and
    # .claude.json holds the same. Presence comes from doctor's names-only findings, never from
    # reading those files or listing the catalog.
    text = SKILL.read_text(encoding="utf-8")
    for block in _bash_blocks(text):
        assert "bundle catalog" not in block
        assert ".claude.json" not in block and "catalog.yaml" not in block
    never = text[text.index("## Never"):]
    assert "bundle catalog" in never and ".claude.json" in never
    assert "Read" in never
