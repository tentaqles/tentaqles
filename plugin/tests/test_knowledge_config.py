"""Where the vault lives and what gets exported is configuration, resolved once.

Precedence: the workspace manifest's ``knowledge:`` section, then the
identity's plugin-data ``knowledge.yaml``, then the shared
``~/.tentaqles/knowledge.yaml``, then ``TENTAQLES_VAULT_DIR``. Ingest dirs are
per client: a manifest lists its own, the shared file maps client -> dirs.
"""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from tentaqles.knowledge import KnowledgeConfig, load_knowledge_config


@pytest.fixture
def homes(tmp_path, monkeypatch):
    plugin_data = tmp_path / "plugin-data"
    tq_home = tmp_path / "tq-home"
    plugin_data.mkdir()
    tq_home.mkdir()
    monkeypatch.setenv("CLAUDE_PLUGIN_DATA", str(plugin_data))
    monkeypatch.setenv("TQ_HOME", str(tq_home))
    monkeypatch.delenv("TENTAQLES_VAULT_DIR", raising=False)
    return plugin_data, tq_home


def test_nothing_configured_means_export_disabled(homes):
    cfg = load_knowledge_config({"client": "acme"})
    assert isinstance(cfg, KnowledgeConfig)
    assert cfg.vault_dir is None
    assert cfg.ingest_dirs == []
    assert cfg.export_sessions is True and cfg.export_fixes is True
    assert cfg.enabled is False


def test_shared_file_sets_vault_and_per_client_ingest(homes):
    _, tq_home = homes
    (tq_home / "knowledge.yaml").write_text(
        "vault_dir: C:/brain/tentaqles\nexport_sessions: false\ningest:\n  personal: [C:/brain/Clippings]\n",
        encoding="utf-8",
    )

    personal = load_knowledge_config({"client": "personal"})
    acme = load_knowledge_config({"client": "acme"})

    assert personal.vault_dir == Path("C:/brain/tentaqles")
    assert personal.export_sessions is False
    assert personal.ingest_dirs == [Path("C:/brain/Clippings")]
    assert acme.vault_dir == Path("C:/brain/tentaqles")
    assert acme.ingest_dirs == []
    assert acme.enabled is True


def test_plugin_data_file_overrides_shared_file(homes):
    plugin_data, tq_home = homes
    (tq_home / "knowledge.yaml").write_text("vault_dir: /shared\n", encoding="utf-8")
    (plugin_data / "knowledge.yaml").write_text("vault_dir: /identity\n", encoding="utf-8")

    assert load_knowledge_config({"client": "acme"}).vault_dir == Path("/identity")


def test_manifest_flags_win_but_its_paths_are_confined(homes, tmp_path):
    """A manifest travels with a repo, so it may not point the plugin at arbitrary paths.

    Its vault_dir is honoured only inside the user-configured vault; its
    ingest_dirs only inside the workspace itself. Flags are always honoured.
    """
    _, tq_home = homes
    shared_vault = tmp_path / "vault"
    root = tmp_path / "repo"
    (root / "docs").mkdir(parents=True)
    (tq_home / "knowledge.yaml").write_text(
        f"vault_dir: '{shared_vault.as_posix()}'\ningest:\n  acme: ['{(tmp_path / 'shared-clips').as_posix()}']\n",
        encoding="utf-8",
    )
    manifest = {
        "client": "acme",
        "_client_root": str(root),
        "knowledge": {
            "vault_dir": (shared_vault / "acme-notes").as_posix(),
            "export_fixes": False,
            "ingest_dirs": [(root / "docs").as_posix(), "/etc", str(tmp_path / "elsewhere")],
        },
    }

    cfg = load_knowledge_config(manifest)

    assert cfg.vault_dir == shared_vault / "acme-notes"
    assert cfg.export_fixes is False
    # the in-repo folder is kept (first), the two outside the workspace are dropped
    assert cfg.ingest_dirs == [root / "docs", tmp_path / "shared-clips"]


def test_manifest_vault_outside_the_shared_vault_is_ignored(homes, tmp_path):
    (homes[1] / "knowledge.yaml").write_text(f"vault_dir: '{(tmp_path / 'vault').as_posix()}'\n", encoding="utf-8")
    cfg = load_knowledge_config({"client": "acme", "knowledge": {"vault_dir": str(tmp_path / "ssh")}})
    assert cfg.vault_dir == tmp_path / "vault"


def test_manifest_vault_without_any_user_vault_is_ignored(homes, tmp_path):
    cfg = load_knowledge_config({"client": "acme", "knowledge": {"vault_dir": str(tmp_path / "anywhere")}})
    assert cfg.vault_dir is None
    assert cfg.enabled is False


def test_env_var_is_the_last_resort(homes, monkeypatch):
    monkeypatch.setenv("TENTAQLES_VAULT_DIR", "/from-env")
    assert load_knowledge_config({"client": "acme"}).vault_dir == Path("/from-env")
    (homes[1] / "knowledge.yaml").write_text("vault_dir: /shared\n", encoding="utf-8")
    assert load_knowledge_config({"client": "acme"}).vault_dir == Path("/shared")


def test_unreadable_or_invalid_file_is_ignored(homes):
    (homes[1] / "knowledge.yaml").write_text("vault_dir: [unclosed\n", encoding="utf-8")
    cfg = load_knowledge_config({"client": "acme"})
    assert cfg.vault_dir is None


def test_no_manifest_still_resolves_shared_settings(homes):
    (homes[1] / "knowledge.yaml").write_text("vault_dir: /shared\n", encoding="utf-8")
    cfg = load_knowledge_config(None)
    assert cfg.vault_dir == Path("/shared")
    assert cfg.ingest_dirs == []


def test_confinement_resolves_symlinks(homes, tmp_path):
    """A link inside the vault pointing elsewhere must not pass the check."""
    vault = tmp_path / "vault"
    outside = tmp_path / "outside"
    vault.mkdir()
    outside.mkdir()
    try:
        os.symlink(outside, vault / "escape", target_is_directory=True)
    except (OSError, NotImplementedError):
        pytest.skip("symlinks not permitted on this host")
    (homes[1] / "knowledge.yaml").write_text(f"vault_dir: '{vault.as_posix()}'\n", encoding="utf-8")

    cfg = load_knowledge_config({"client": "acme", "knowledge": {"vault_dir": str(vault / "escape")}})

    assert cfg.vault_dir == vault
