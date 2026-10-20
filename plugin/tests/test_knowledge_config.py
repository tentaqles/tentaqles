"""Where the vault lives and what gets exported is configuration, resolved once.

Precedence: the workspace manifest's ``knowledge:`` section, then the
identity's plugin-data ``knowledge.yaml``, then the shared
``~/.tentaqles/knowledge.yaml``, then ``TENTAQLES_VAULT_DIR``. Ingest dirs are
per client: a manifest lists its own, the shared file maps client -> dirs.
"""

from __future__ import annotations

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


def test_manifest_section_wins_over_files(homes):
    _, tq_home = homes
    (tq_home / "knowledge.yaml").write_text("vault_dir: /shared\ningest:\n  acme: [/shared-clips]\n", encoding="utf-8")
    manifest = {
        "client": "acme",
        "knowledge": {"vault_dir": "/mine", "export_fixes": False, "ingest_dirs": ["/my-clips"]},
    }

    cfg = load_knowledge_config(manifest)

    assert cfg.vault_dir == Path("/mine")
    assert cfg.export_fixes is False
    # manifest dirs come first, shared ones are still included (deduplicated)
    assert cfg.ingest_dirs == [Path("/my-clips"), Path("/shared-clips")]


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
