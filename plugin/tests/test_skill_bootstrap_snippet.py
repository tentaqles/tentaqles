"""Skill bash blocks must find the plugin under a tq identity.

`CLAUDE_PLUGIN_ROOT` is expanded in hooks.json commands but is *not* set inside
the Bash tool the model uses, so every skill bash block carries a one-line
bootstrap that searches the plugin cache itself. Under tq each identity has its
own Claude config dir (`CLAUDE_CONFIG_DIR=~/.tentaqles/identities/<id>/claude`)
and the plugin cache lives there, not under `~/.claude`. A snippet that only
looks in `~/.claude/plugins/cache` finds nothing, sources `/scripts/tq_env.sh`
(which fails silently), and the Python that follows dies with
`ModuleNotFoundError: No module named 'tentaqles'`.

These tests pin the snippet's shape in every SKILL.md and prove, by running it
under bash, that it resolves the plugin from `CLAUDE_CONFIG_DIR`.
"""

from __future__ import annotations

import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SKILLS_DIR = PLUGIN_ROOT / "skills"
ENV_SH = PLUGIN_ROOT / "scripts" / "tq_env.sh"

CACHE_GLOB = '"${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/'
LEGACY_GLOB = '"$HOME/.claude/plugins/cache"/*/tentaqles/*/'
MANIFEST_CHECK = '[ -f "${_d}.claude-plugin/plugin.json" ]'

SNIPPET_LINE_RE = re.compile(r"^.*_tqe=\"\$\{CLAUDE_PLUGIN_ROOT:-\}\".*$", re.MULTILINE)


def _snippet_lines(path: Path) -> list[str]:
    return SNIPPET_LINE_RE.findall(path.read_text(encoding="utf-8"))


SKILLS_WITH_SNIPPET = sorted(p for p in SKILLS_DIR.glob("*/SKILL.md") if _snippet_lines(p))


def test_snippet_is_used_by_skills() -> None:
    assert len(SKILLS_WITH_SNIPPET) >= 10


@pytest.mark.parametrize("path", SKILLS_WITH_SNIPPET, ids=lambda p: p.parent.name)
def test_snippet_searches_the_identity_config_dir(path: Path) -> None:
    for line in _snippet_lines(path):
        assert CACHE_GLOB in line, f"{path.parent.name}: snippet must search CLAUDE_CONFIG_DIR first"
        assert LEGACY_GLOB in line, f"{path.parent.name}: snippet must still fall back to ~/.claude"
        assert MANIFEST_CHECK in line, f"{path.parent.name}: snippet must check .claude-plugin/plugin.json"
        assert line.index(CACHE_GLOB) < line.index(LEGACY_GLOB), (
            f"{path.parent.name}: the identity's own cache must win over ~/.claude"
        )


def test_tq_env_fallback_searches_the_identity_config_dir() -> None:
    text = ENV_SH.read_text(encoding="utf-8")
    assert '"${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/' in text


def test_plugin_claude_md_documents_the_current_snippet() -> None:
    text = (PLUGIN_ROOT / "CLAUDE.md").read_text(encoding="utf-8")
    assert CACHE_GLOB in text
    assert MANIFEST_CHECK in text


bash = shutil.which("bash")


def _fake_install(root: Path, name: str) -> Path:
    """Lay out <root>/plugins/cache/<marketplace>/tentaqles/<ver>/ like Claude Code."""
    plugin = root / "plugins" / "cache" / "tentaqles" / "tentaqles" / "9.9.9"
    (plugin / ".claude-plugin").mkdir(parents=True)
    (plugin / ".claude-plugin" / "plugin.json").write_text('{"name": "tentaqles"}', encoding="utf-8")
    (plugin / "scripts").mkdir()
    # A stand-in for tq_env.sh that records which plugin root was sourced.
    (plugin / "scripts" / "tq_env.sh").write_text(f'TQ_TEST_ROOT="{name}"\n', encoding="utf-8")
    return plugin


@pytest.mark.skipif(bash is None, reason="bash not available")
def test_snippet_resolves_plugin_from_claude_config_dir(tmp_path: Path) -> None:
    home = tmp_path / "home"
    config = tmp_path / "identity" / "claude"
    home.mkdir()
    config.mkdir(parents=True)
    _fake_install(home / ".claude", "home")
    _fake_install(config, "identity")

    snippet = _snippet_lines(SKILLS_DIR / "query-memory" / "SKILL.md")[0].strip()
    script = snippet + '\necho "root=$_tqe"\necho "sourced=$TQ_TEST_ROOT"\n'
    env = {k: v for k, v in os.environ.items() if k not in ("CLAUDE_PLUGIN_ROOT", "TQ_TEST_ROOT")}
    env["HOME"] = home.as_posix()
    env["CLAUDE_CONFIG_DIR"] = config.as_posix()
    out = subprocess.run([bash, "-c", script], env=env, capture_output=True, text=True, timeout=30)
    assert out.returncode == 0, out.stderr
    assert "sourced=identity" in out.stdout, out.stdout
    assert f"root={config.as_posix()}/plugins/cache/tentaqles/tentaqles/9.9.9" in out.stdout, out.stdout


@pytest.mark.skipif(bash is None, reason="bash not available")
def test_snippet_falls_back_to_home_claude_without_config_dir(tmp_path: Path) -> None:
    home = tmp_path / "home"
    home.mkdir()
    _fake_install(home / ".claude", "home")

    snippet = _snippet_lines(SKILLS_DIR / "query-memory" / "SKILL.md")[0].strip()
    script = snippet + '\necho "sourced=$TQ_TEST_ROOT"\n'
    env = {k: v for k, v in os.environ.items() if k not in ("CLAUDE_PLUGIN_ROOT", "CLAUDE_CONFIG_DIR", "TQ_TEST_ROOT")}
    env["HOME"] = home.as_posix()
    out = subprocess.run([bash, "-c", script], env=env, capture_output=True, text=True, timeout=30)
    assert out.returncode == 0, out.stderr
    assert "sourced=home" in out.stdout, out.stdout
