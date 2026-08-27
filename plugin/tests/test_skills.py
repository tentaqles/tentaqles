"""Tests for the self-improving skills module."""

from __future__ import annotations

from pathlib import Path

import pytest

from tentaqles.skills import (
    LEARNED_SECTION_HEADER,
    append_to_skill,
    find_skill_md,
    record_skill_correction,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_plugin_skill(plugin_root: Path, name: str, body: str = "# Foo skill\n\nSome body.\n") -> Path:
    path = plugin_root / "skills" / name / "SKILL.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")
    return path


def _make_client_skill(client_root: Path, name: str, body: str = "# Foo skill (client)\n") -> Path:
    path = client_root / ".claude" / "skills" / name / "SKILL.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body, encoding="utf-8")
    return path


# ---------------------------------------------------------------------------
# find_skill_md
# ---------------------------------------------------------------------------


def test_find_plugin_shared(tmp_path: Path) -> None:
    plugin_root = tmp_path / "plugin"
    expected = _make_plugin_skill(plugin_root, "foo")
    assert find_skill_md("foo", plugin_root) == expected


def test_find_client_local_wins(tmp_path: Path) -> None:
    plugin_root = tmp_path / "plugin"
    client_root = tmp_path / "client"
    _make_plugin_skill(plugin_root, "foo")
    expected = _make_client_skill(client_root, "foo")
    assert find_skill_md("foo", plugin_root, client_root) == expected


def test_find_not_found(tmp_path: Path) -> None:
    assert find_skill_md("ghost", tmp_path / "plugin") is None
    assert find_skill_md("ghost", tmp_path / "plugin", tmp_path / "client") is None


# ---------------------------------------------------------------------------
# append_to_skill
# ---------------------------------------------------------------------------


def test_append_creates_section_in_new_file(tmp_path: Path) -> None:
    path = tmp_path / "SKILL.md"
    path.write_text("# Foo\n\nOriginal body.\n", encoding="utf-8")

    ok = append_to_skill(path, "Always prefer ISO dates", timestamp="2026-04-12")
    assert ok is True

    text = path.read_text(encoding="utf-8")
    assert "Original body." in text
    assert LEARNED_SECTION_HEADER in text
    assert "- [2026-04-12] Always prefer ISO dates" in text


def test_append_to_existing_section(tmp_path: Path) -> None:
    path = tmp_path / "SKILL.md"
    path.write_text(
        "# Foo\n\nBody.\n\n"
        f"{LEARNED_SECTION_HEADER}\n\n- [2026-01-01] First lesson\n",
        encoding="utf-8",
    )
    ok = append_to_skill(path, "Second lesson", timestamp="2026-04-12")
    assert ok is True

    text = path.read_text(encoding="utf-8")
    assert "- [2026-01-01] First lesson" in text
    assert "- [2026-04-12] Second lesson" in text
    # Ordering: first lesson comes before second.
    assert text.index("First lesson") < text.index("Second lesson")


def test_append_idempotent_exact(tmp_path: Path) -> None:
    path = tmp_path / "SKILL.md"
    path.write_text("# Foo\n", encoding="utf-8")
    assert append_to_skill(path, "use RS256 not HS256", timestamp="2026-04-12") is True
    assert append_to_skill(path, "use RS256 not HS256", timestamp="2026-04-12") is False

    text = path.read_text(encoding="utf-8")
    assert text.count("use RS256 not HS256") == 1


def test_append_idempotent_similar(tmp_path: Path) -> None:
    path = tmp_path / "SKILL.md"
    path.write_text("# Foo\n", encoding="utf-8")
    assert append_to_skill(path, "use RS256 not HS256", timestamp="2026-04-12") is True
    # Near-paraphrase — Jaccard on normalized tokens should exceed 0.85.
    assert append_to_skill(path, "use rs256 instead of hs256", timestamp="2026-04-13") is False


def test_append_dissimilar_both_kept(tmp_path: Path) -> None:
    path = tmp_path / "SKILL.md"
    path.write_text("# Foo\n", encoding="utf-8")
    assert append_to_skill(path, "Always prefer ISO dates", timestamp="2026-04-12") is True
    assert append_to_skill(path, "Never commit to main branch directly", timestamp="2026-04-13") is True

    text = path.read_text(encoding="utf-8")
    assert "Always prefer ISO dates" in text
    assert "Never commit to main branch directly" in text


# ---------------------------------------------------------------------------
# record_skill_correction
# ---------------------------------------------------------------------------


def test_record_skill_correction_redacts(tmp_path: Path) -> None:
    plugin_root = tmp_path / "plugin"
    client_root = tmp_path / "client"
    _make_plugin_skill(plugin_root, "foo")

    result = record_skill_correction(
        "foo",
        "use the API key ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345 always",
        plugin_root,
        client_root,
    )
    assert result["status"] == "appended"
    text = Path(result["path"]).read_text(encoding="utf-8")
    assert "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345" not in text
    assert "[REDACTED:github_pat]" in text


def test_record_skill_correction_client_local_creates_override(tmp_path: Path) -> None:
    plugin_root = tmp_path / "plugin"
    client_root = tmp_path / "client"
    plugin_path = _make_plugin_skill(plugin_root, "foo", body="# Foo\n\nShared body.\n")
    plugin_original = plugin_path.read_text(encoding="utf-8")

    result = record_skill_correction("foo", "Prefer tabs over spaces", plugin_root, client_root)
    assert result["status"] == "appended"

    override = client_root / ".claude" / "skills" / "foo" / "SKILL.md"
    assert override.is_file()
    override_text = override.read_text(encoding="utf-8")
    assert "Shared body." in override_text
    assert "Prefer tabs over spaces" in override_text

    # Plugin copy untouched.
    assert plugin_path.read_text(encoding="utf-8") == plugin_original


def test_record_skill_correction_not_found(tmp_path: Path) -> None:
    result = record_skill_correction("ghost", "whatever", tmp_path / "plugin")
    assert result == {"status": "not_found", "path": None, "skill_name": "ghost"}


def test_append_preserves_existing_content(tmp_path: Path) -> None:
    path = tmp_path / "SKILL.md"
    original = (
        "---\n"
        "name: foo\n"
        "description: a test skill\n"
        "---\n"
        "\n"
        "# Foo skill\n"
        "\n"
        "This is the body. It has multiple paragraphs.\n"
        "\n"
        "## Usage\n"
        "\n"
        "Call foo().\n"
        "\n"
        f"{LEARNED_SECTION_HEADER}\n"
        "\n"
        "- [2026-01-01] Old lesson\n"
    )
    path.write_text(original, encoding="utf-8")

    ok = append_to_skill(path, "Fresh lesson", timestamp="2026-04-12")
    assert ok is True

    text = path.read_text(encoding="utf-8")
    # Frontmatter preserved.
    assert text.startswith("---\nname: foo\n")
    # Body sections preserved.
    assert "# Foo skill" in text
    assert "## Usage" in text
    assert "Call foo()." in text
    # Both learned entries present.
    assert "- [2026-01-01] Old lesson" in text
    assert "- [2026-04-12] Fresh lesson" in text
