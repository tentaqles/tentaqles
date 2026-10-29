"""Spec E (guard modes) ships with its docs.

The docs must describe what was built: the two layers, the five modes, every
categorised rule id with its category, the launchers, the Codex limits, the
migration, and the changelogs. The word "unattended" survives only where the
docs describe the deprecated alias and the migration from it.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

import pytest

PLUGIN = Path(__file__).resolve().parents[1]
REPO = PLUGIN.parent
DOCS = REPO / "docs"

MODES = ["off", "default", "relaxed", "strict", "custom"]
LOCKED = ["identity", "secrets", "self-protection", "privacy"]
TOGGLEABLE = [
    "git-history", "prod-changes", "cloud-destructive", "db-destructive", "system",
    "external-writes", "ci-supply-chain", "hygiene", "packs",
]

OWNED = [
    DOCS / "CLAUDE-HOOK.md", DOCS / "SETUP.md", DOCS / "CLAUDE-SETTINGS.md",
    DOCS / "MIGRATE.md", DOCS / "HUB.md", DOCS / "DESKTOP.md",
    PLUGIN / "CLAUDE.md", PLUGIN / "skills" / "guard" / "SKILL.md",
]


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def section(text: str, heading: str) -> str:
    """The body under the first heading line that starts with `heading`,
    up to the next heading of the same or a higher level."""
    lines = text.splitlines()
    for i, line in enumerate(lines):
        if line.startswith(heading):
            level = len(line) - len(line.lstrip("#"))
            out = []
            fence = False
            for nxt in lines[i + 1:]:
                if nxt.startswith("```"):
                    fence = not fence
                if not fence and re.match(r"^#{1,%d} " % level, nxt):
                    break
                out.append(nxt)
            return "\n".join(out)
    raise AssertionError(f"no heading {heading!r}")


def categories_from_code() -> dict[str, str]:
    """rule id -> category, parsed from internal/guardcfg/categories.go."""
    pkg = REPO / "internal" / "guardcfg"
    src = read(pkg / "categories.go")
    every = "\n".join(read(p) for p in pkg.glob("*.go") if not p.name.endswith("_test.go"))
    consts = dict(re.findall(r"\b(\w+ID)\s*=\s*\"([^\"]+)\"", every))
    cats = dict(re.findall(r"\b(Cat\w+)\s+Category\s*=\s*\"([^\"]+)\"", src))
    body = src.split("var Categories", 1)[1].split("\n}", 1)[0]
    out = {}
    for key, cat in re.findall(r"^\s*(\"[^\"]+\"|\w+)\s*:\s*(Cat\w+),", body, re.M):
        rid = key.strip('"') if key.startswith('"') else consts[key]
        out[rid] = cats[cat]
    assert len(out) > 50, out
    return out


# ---- CLAUDE-HOOK.md -----------------------------------------------------------

HOOK = read(DOCS / "CLAUDE-HOOK.md")
try:
    MODES_DOC = section(HOOK, "## Guard modes")
except AssertionError:
    MODES_DOC = ""  # every test below then fails on its own


def test_hook_has_the_two_layer_table():
    two = section(MODES_DOC, "### Two layers")
    assert "Claude Code permission mode" in two and "tq guard mode" in two
    for v in ("bypassPermissions", "acceptEdits", "auto"):
        assert v in two
    assert "--dangerously-skip-permissions" in two


@pytest.mark.parametrize("mode", MODES)
def test_hook_describes_each_mode(mode):
    assert re.search(r"^#### `%s`" % mode, MODES_DOC, re.M), mode


def test_hook_preset_table_rows():
    table = section(MODES_DOC, "### Presets")
    rows = {}
    for line in table.splitlines():
        cells = [c.strip().strip("`") for c in line.strip().strip("|").split("|")]
        if len(cells) == 4 and cells[0] in LOCKED + TOGGLEABLE:
            rows[cells[0]] = cells[1:]
    for c in LOCKED:
        assert rows[c] == ["rule", "rule", "rule"], c
    assert rows["git-history"] == ["rule", "ask", "deny"]
    assert rows["system"] == ["rule", "ask", "rule"]
    assert rows["prod-changes"] == ["rule", "skip", "deny"]
    assert rows["cloud-destructive"] == ["rule", "skip", "deny"]
    assert rows["external-writes"] == ["rule", "skip", "ask"]
    for c in ("db-destructive", "ci-supply-chain", "hygiene", "packs"):
        assert rows[c] == ["rule", "skip", "rule"], c


def test_hook_categories_table_matches_the_code():
    table = section(MODES_DOC, "### Categories")
    rows = [l for l in table.splitlines() if l.startswith("|")]
    missing = []
    for rid, cat in categories_from_code().items():
        hit = [r for r in rows if f"`{rid}`" in r]
        if not hit or not any(re.match(r"^\|\s*`?%s`?\s*\|" % re.escape(cat), r) for r in hit):
            missing.append(f"{rid} ({cat})")
    assert not missing, missing


def test_hook_privacy_is_locked_in_the_categories_table():
    table = section(MODES_DOC, "### Categories")
    for c in LOCKED:
        row = next(l for l in table.splitlines() if re.match(r"^\|\s*`?%s`?\s*\|" % c, l))
        assert "locked" in row.lower(), c


@pytest.mark.parametrize("needle", [
    "tq allow --mode", "$TQ_HOME/trust/", ".mode", "hash", "tighten",
    "TQ_MODE", "--tq-mode", "guard.yaml", "rules_override", "global.mode",
])
def test_hook_precedence_and_grants(needle):
    assert needle in section(MODES_DOC, "### Precedence and grants"), needle


@pytest.mark.parametrize("needle", [
    "tq off", "tq on", "claude-off", "codex-off", "tq run --off", "tq run .",
    "--dangerously-skip-permissions", "enabledPlugins", "TQ_MODE=off",
])
def test_hook_launchers(needle):
    assert needle in section(MODES_DOC, "### Launchers"), needle


def test_hook_nested_agent_rule():
    nested = section(MODES_DOC, "### Nested agents")
    for needle in ("tq/nested-agent-loosened", "claude -p", "CLAUDECODE", "--bare", "--yolo"):
        assert needle in nested, needle


@pytest.mark.parametrize("needle", [
    "tq mode", "tq mode set", "tq mode toggle", "tq mode override", "tq mode edit",
    "tq mode log", "tq/mode-change", "Guard:", "tq: relaxed", "/guard", "Guard tab",
])
def test_hook_controls(needle):
    assert needle in section(MODES_DOC, "### Controls"), needle


def test_hook_builtin_env_file_rules_are_denies():
    # The table once said ask; the code denies.
    row = next(l for l in HOOK.splitlines() if l.startswith("| `tq/env-file-read`"))
    assert "| deny |" in row


def test_hook_lists_the_new_system_and_mode_rules():
    for rid in ("tq/outside-path-write", "tq/system-dir-write", "tq/elevation", "tq/mode-change",
                "tq/nested-agent-loosened"):
        assert any(l.startswith("| `%s`" % rid) or l.startswith("- `%s`" % rid)
                   for l in HOOK.splitlines()), rid


def test_hook_has_no_unattended_section():
    assert not re.search(r"^## Unattended mode", HOOK, re.M)


# ---- CLAUDE-SETTINGS.md --------------------------------------------------------

SETTINGS = read(DOCS / "CLAUDE-SETTINGS.md")


@pytest.mark.parametrize("needle", [
    "asks become blocks", "Codex hooks cannot ask", "rules/tq.rules", "hooks.json",
    "tq mode --agent codex", "/hooks", "approval_policy", "prompt", "forbidden",
    "managed by tq (guard mode)",
])
def test_settings_codex_limits(needle):
    assert needle in section(SETTINGS, "## Codex"), needle


@pytest.mark.parametrize("code", [
    "mode-not-granted", "mode-unknown-rule", "mode-cloud", "guard-locked-override",
    "claude-alias-shadow", "permission-mode-overridden", "codex-hooks-missing",
    "codex-hooks-disabled", "codex-hooks-untrusted",
])
def test_settings_doctor_codes(code):
    assert f"| `{code}` |" in section(SETTINGS, "## `tq doctor` checks"), code


def test_settings_doctor_drops_unattended_codes():
    assert "unattended-not-effective" not in section(SETTINGS, "## `tq doctor` checks")


def test_settings_render_keys():
    render = section(SETTINGS, "## `tq settings render`")
    assert "skipDangerousModePermissionPrompt" in render
    assert "gh repo delete" in render


def test_settings_statusline_mode_segment():
    assert "tq: relaxed" in section(SETTINGS, "## `tq statusline`")
    assert "tq: off" in section(SETTINGS, "## `tq statusline`")


# ---- MIGRATE.md, SETUP.md, HUB.md, DESKTOP.md ---------------------------------

MIGRATE = read(DOCS / "MIGRATE.md")


@pytest.mark.parametrize("needle", [
    "guard.unattended", "workspace: true", "keep_asking", "codex_sandbox", "guard.codex.sandbox",
    "tq unattended on", "tq mode set relaxed", "deprecated", ".bypass", "mode.jsonl",
    "tq/commit-pii", "guard-locked-override", "tq allow --bypass", "tq off",
])
def test_migrate_from_unattended(needle):
    assert needle in section(MIGRATE, "## Migrating from unattended mode"), needle


def test_migrate_behaviour_changes():
    changes = section(MIGRATE, "### Behaviour changes").lower()
    for needle in ("every claude", "denies too", "privacy", "bypass", "tq/mode-change",
                   "tq/outside-path-write", "codex"):
        assert needle in changes, needle


def test_setup_points_at_guard_modes():
    setup = read(DOCS / "SETUP.md")
    sec = section(setup, "## Guard modes")
    for needle in ("tq mode set relaxed", "tq allow --mode", "claude-off", "tq off"):
        assert needle in sec, needle
    assert "## Unattended runs" not in setup


def test_hub_headless_mode():
    hub = read(DOCS / "HUB.md")
    for needle in ("guard.headless_mode", "TQ_MODE", "/guard", "tq allow --mode"):
        assert needle in hub, needle
    assert "unless\n`tq allow --bypass` was run" not in hub


def test_desktop_guard_tab():
    sec = section(read(DOCS / "DESKTOP.md"), "## Guard tab")
    for needle in ("locked", "privacy", "grant", "tq mode"):
        assert needle in sec.lower() or needle in sec, needle


# ---- plugin: CLAUDE.md, guard skill, CHANGELOG ---------------------------------

def test_plugin_claude_md_mentions_modes_and_off():
    text = read(PLUGIN / "CLAUDE.md")
    assert "guard mode" in text
    assert "TQ_MODE=off" in text
    for c in LOCKED:
        assert c in text


def test_guard_skill_covers_modes():
    text = read(PLUGIN / "skills" / "guard" / "SKILL.md")
    for needle in ("tq mode", "tq mode set", "tq allow --mode", "human-only", "relaxed",
                   "strict", "locked", "privacy", "tq/mode-change", "claude-off"):
        assert needle in text, needle
    fm = text.split("---", 2)[1]
    assert "mode" in fm


def test_plugin_changelog_unreleased_is_filled_without_a_bump():
    text = read(PLUGIN / "CHANGELOG.md")
    unreleased = text.split("## [Unreleased]", 1)[1].split("\n## [", 1)[0]
    for needle in ("guard", "TQ_MODE=off", "tq_hook.sh", "tq_run.sh"):
        assert needle in unreleased, needle
    version = json.loads(read(PLUGIN / ".claude-plugin" / "plugin.json"))["version"]
    assert f"## [{version}]" in text, "plugin version must not be bumped past the changelog"


def test_root_changelog():
    text = read(REPO / "CHANGELOG.md")
    unreleased = text.split("## [Unreleased]", 1)[1].split("\n## [", 1)[0]
    for needle in ("tq mode", "relaxed", "strict", "tq off", "claude-off", "privacy",
                   "tq/nested-agent-loosened", "tq/mode-change", "deprecated", "Codex",
                   "Behaviour changes"):
        assert needle in unreleased, needle


# ---- the word "unattended" ------------------------------------------------------

ALLOWED_CONTEXT = re.compile(r"deprecat|legacy|migrat|alias|replac|renam|formerly|was |old ", re.I)


def unattended_offenders(text: str) -> list[str]:
    """Lines naming "unattended" outside a deprecated/migration context: the
    line itself, or any heading above it (the whole heading path), must say so."""
    bad = []
    path: list[str] = []  # headings, index = level - 1
    fence = False
    for n, line in enumerate(text.splitlines(), 1):
        if line.startswith("```"):
            fence = not fence
        m = None if fence else re.match(r"^(#{1,6}) ", line)
        if m:
            level = len(m.group(1))
            path = path[: level - 1] + [""] * (level - 1 - len(path)) + [line]
        if "unattended" in line.lower():
            if not (ALLOWED_CONTEXT.search(line) or any(ALLOWED_CONTEXT.search(h) for h in path)):
                bad.append(f"{n}: {line.strip()}")
    return bad


@pytest.mark.parametrize("path", OWNED, ids=lambda p: p.name if p.name != "SKILL.md" else "guard-skill")
def test_unattended_only_as_deprecated_or_migration(path):
    assert not unattended_offenders(read(path)), unattended_offenders(read(path))


def test_unattended_in_changelog_unreleased_only_as_deprecated():
    for p in (PLUGIN / "CHANGELOG.md", REPO / "CHANGELOG.md"):
        text = read(p)
        unreleased = text.split("## [Unreleased]", 1)[1].split("\n## [", 1)[0]
        assert not unattended_offenders(unreleased), (p.name, unattended_offenders(unreleased))
