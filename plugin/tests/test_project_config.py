# plugin/tests/test_project_config.py
"""The ```json tq-project block in CLAUDE.md is the machine-readable project config."""

from __future__ import annotations

from pathlib import Path

from tentaqles.workflow.project import load_project_config

BLOCK = '''# Demo

## Project config

```json tq-project
{"commands": {"test": "pytest -q", "lint": "ruff check ."},
 "architecture_paths": ["src/core/**"], "classified_paths": [], "changelog": "CHANGELOG.md"}
```
'''


def test_reads_commands_and_paths(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_text(BLOCK, encoding="utf-8")
    cfg, warn = load_project_config(tmp_path)
    assert warn == ""
    assert cfg["commands"]["test"] == "pytest -q"
    assert cfg["architecture_paths"] == ["src/core/**"]
    assert cfg["changelog"] == "CHANGELOG.md"


def test_missing_file_or_block_is_empty_without_warning(tmp_path: Path) -> None:
    assert load_project_config(tmp_path) == ({}, "")
    (tmp_path / "CLAUDE.md").write_text("# nothing here\n", encoding="utf-8")
    assert load_project_config(tmp_path) == ({}, "")


def test_malformed_block_returns_empty_and_warning(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_text("```json tq-project\n{\"commands\": {\"test\": 1,}}\n```\n", encoding="utf-8")
    cfg, warn = load_project_config(tmp_path)
    assert cfg == {}
    assert "tq-project" in warn and "invalid" in warn.lower()


def test_wrong_types_and_unknown_keys_are_dropped_with_warning(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_text(
        '```json tq-project\n{"commands": {"test": "pytest", "lint": 3}, "architecture_paths": "src/**", "colour": 1}\n```\n',
        encoding="utf-8")
    cfg, warn = load_project_config(tmp_path)
    assert cfg["commands"] == {"test": "pytest"}
    assert "architecture_paths" not in cfg
    assert "commands.lint" in warn and "architecture_paths" in warn and "colour" in warn


def test_crlf_and_bom_are_tolerated(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_bytes(b"\xef\xbb\xbf# x\r\n```json tq-project\r\n{\"commands\": {\"test\": \"go test ./...\"}}\r\n```\r\n")
    cfg, _ = load_project_config(tmp_path)
    assert cfg["commands"]["test"] == "go test ./..."


def test_non_utf8_file_yields_warning(tmp_path: Path) -> None:
    (tmp_path / "CLAUDE.md").write_bytes(b"# caf\xe9\n")
    cfg, warn = load_project_config(tmp_path)
    assert cfg == {} and "utf-8" in warn.lower()


def test_empty_block_yields_warning(tmp_path: Path) -> None:
    for body in ("```json tq-project\n```\n", "```json tq-project\n\n```\n"):
        (tmp_path / "CLAUDE.md").write_text(body, encoding="utf-8")
        cfg, warn = load_project_config(tmp_path)
        assert cfg == {} and "empty" in warn.lower() and "tq-project" in warn
