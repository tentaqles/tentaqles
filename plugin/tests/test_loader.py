"""Tests for tentaqles.manifest.loader schema handling."""

import textwrap

import pytest

from tentaqles.manifest import loader


def _write_manifest(tmp_path, schema):
    (tmp_path / ".tentaqles.yaml").write_text(
        textwrap.dedent(
            f"""\
            schema: {schema}
            client: acme
            display_name: Acme Corp
            language: en
            """
        ),
        encoding="utf-8",
    )
    return tmp_path


def test_load_manifest_accepts_v1(tmp_path):
    _write_manifest(tmp_path, "tentaqles-client-v1")
    data = loader.load_manifest(tmp_path)
    assert data is not None
    assert data["client"] == "acme"


def test_load_manifest_accepts_v2(tmp_path):
    _write_manifest(tmp_path, "tentaqles-client-v2")
    data = loader.load_manifest(tmp_path)
    assert data is not None
    assert data["client"] == "acme"
    assert data["_client_root"] == str(tmp_path)


def test_load_manifest_rejects_v3(tmp_path):
    _write_manifest(tmp_path, "tentaqles-client-v3")
    assert loader.load_manifest(tmp_path) is None


def test_no_manifest_returns_unknown_without_touching_home(tmp_path, monkeypatch):
    """No manifest anywhere -> empty context; the registry fallback is gone."""
    fake_home = tmp_path / "home"
    fake_home.mkdir()
    monkeypatch.setenv("HOME", str(fake_home))
    monkeypatch.setenv("USERPROFILE", str(fake_home))

    workdir = tmp_path / "home" / "work"
    workdir.mkdir()

    ctx = loader.get_client_context(workdir)
    assert ctx["client"] == "unknown"
    assert ctx["display_name"] == "Unknown"
    assert ctx["manifest_path"] == ""
    assert ctx["cloud"] == {}
    assert ctx["stack"] == []
    # registry fallback removed entirely
    assert not hasattr(loader, "_fallback_from_registry")


def test_get_client_context_reads_v2(tmp_path):
    _write_manifest(tmp_path, "tentaqles-client-v2")
    ctx = loader.get_client_context(tmp_path)
    assert ctx["client"] == "acme"
    assert ctx["display_name"] == "Acme Corp"
