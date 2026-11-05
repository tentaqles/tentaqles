"""The protected manifest location (ADR-0002) in the plugin's loader.

A workspace root holds a name-only marker (schema tentaqles-marker-v1); the
manifest lives at $TQ_HOME/identities/<name>/manifest.yaml. The loader must
read the protected copy, keep ``_client_root`` on the folder, and keep
working for the old layout (the full manifest in the root).
"""

from __future__ import annotations

import os
import textwrap
from pathlib import Path

import pytest

from tentaqles.manifest import loader

MANIFEST = textwrap.dedent(
    """\
    schema: tentaqles-client-v2
    client: acme
    display_name: Acme Corp
    git:
      email: dev@acme.test
    """
)


def _marker(root: Path, name: str = "acme") -> Path:
    root.mkdir(parents=True, exist_ok=True)
    p = root / ".tentaqles.yaml"
    p.write_text(
        f"# tentaqles workspace marker\nschema: tentaqles-marker-v1\nclient: {name}\n",
        encoding="utf-8",
    )
    return p


def _protected(tq_home: Path, name: str = "acme", body: str = MANIFEST) -> Path:
    p = tq_home / "identities" / name / "manifest.yaml"
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(body, encoding="utf-8")
    return p


@pytest.fixture
def tq_home(tmp_path, monkeypatch):
    home = tmp_path / "tqhome"
    home.mkdir()
    monkeypatch.setenv("TQ_HOME", str(home))
    return home


def test_marker_resolves_to_protected_manifest(tmp_path, tq_home):
    root = tmp_path / "base" / "acme"
    _marker(root)
    pp = _protected(tq_home)
    (root / "repo" / "src").mkdir(parents=True)

    data = loader.load_manifest(root / "repo" / "src")
    assert data is not None
    assert data["client"] == "acme"
    assert data["git"]["email"] == "dev@acme.test"
    assert Path(data["_manifest_path"]) == pp.resolve()
    assert Path(data["_client_root"]) == root.resolve()
    assert loader.find_manifest(root / "repo") == pp.resolve()

    ctx = loader.get_client_context(root)
    assert ctx["client"] == "acme"
    assert Path(ctx["manifest_path"]) == pp.resolve()
    assert Path(ctx["client_root"]) == root.resolve()


def test_marker_client_must_match_the_folder(tmp_path, tq_home):
    root = tmp_path / "base" / "acme"
    _marker(root, name="other")
    _protected(tq_home, name="other")
    _protected(tq_home, name="acme")
    assert loader.load_manifest(root) is None
    assert loader.find_manifest(root) is None
    assert loader.get_client_context(root)["client"] == "unknown"


def test_marker_without_protected_copy_is_unknown(tmp_path, tq_home):
    root = tmp_path / "base" / "acme"
    _marker(root)
    assert loader.load_manifest(root) is None
    assert loader.find_manifest(root) is None
    assert loader.get_client_context(root)["client"] == "unknown"


def test_old_layout_still_loads(tmp_path, tq_home):
    root = tmp_path / "base" / "acme"
    root.mkdir(parents=True)
    rf = root / ".tentaqles.yaml"
    rf.write_text(MANIFEST, encoding="utf-8")
    data = loader.load_manifest(root)
    assert data is not None and data["client"] == "acme"
    assert Path(data["_manifest_path"]) == rf.resolve()
    assert Path(data["_client_root"]) == root.resolve()
    assert loader.find_manifest(root) == rf.resolve()


def test_both_files_prefer_the_protected_copy(tmp_path, tq_home):
    root = tmp_path / "base" / "acme"
    root.mkdir(parents=True)
    (root / ".tentaqles.yaml").write_text(MANIFEST.replace("dev@acme.test", "stale@acme.test"), encoding="utf-8")
    pp = _protected(tq_home)
    data = loader.load_manifest(root)
    assert data is not None
    assert data["git"]["email"] == "dev@acme.test"
    assert Path(data["_manifest_path"]) == pp.resolve()
    assert Path(data["_client_root"]) == root.resolve()


def test_default_home_is_dot_tentaqles(tmp_path, monkeypatch):
    """Without TQ_HOME the protected copy is under ~/.tentaqles."""
    monkeypatch.delenv("TQ_HOME", raising=False)
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setenv("USERPROFILE", str(home))
    monkeypatch.setattr(Path, "home", classmethod(lambda cls: home))
    root = tmp_path / "base" / "acme"
    _marker(root)
    pp = _protected(home / ".tentaqles")
    data = loader.load_manifest(root)
    assert data is not None
    assert Path(data["_manifest_path"]) == pp.resolve()


def test_protected_manifest_path_helper(tq_home):
    assert loader.protected_manifest_path("acme") == tq_home / "identities" / "acme" / "manifest.yaml"
    assert loader.is_protected_manifest_path(tq_home / "identities" / "acme" / "manifest.yaml")
    assert not loader.is_protected_manifest_path(tq_home / "identities" / "acme" / "claude" / "manifest.yaml")
    assert not loader.is_protected_manifest_path(Path("/elsewhere/identities/acme/manifest.yaml"))


def test_workspace_root_for_scans_the_registered_bases(tmp_path, tq_home, monkeypatch):
    base = tmp_path / "base"
    root = base / "acme"
    _marker(root)
    _protected(tq_home)
    (tq_home / "config.yaml").write_text(f"bases:\n  - {base}\n", encoding="utf-8")
    monkeypatch.delenv("TQ_WS", raising=False)
    monkeypatch.delenv("TQ_WS_ROOT", raising=False)
    assert loader.workspace_root_for("acme") == root.resolve()
    assert loader.workspace_root_for("nobody") is None
    # The cwd wins when it already is inside the workspace.
    assert loader.workspace_root_for("acme", cwd=root / "deep") == root.resolve()
    # And the identity shell's own variables are trusted before the scan.
    monkeypatch.setenv("TQ_WS", "acme")
    monkeypatch.setenv("TQ_WS_ROOT", str(root))
    assert loader.workspace_root_for("acme") == root.resolve()


def test_no_marker_anywhere_is_unknown(tmp_path, tq_home):
    assert loader.load_manifest(tmp_path) is None
    assert loader.find_manifest(tmp_path) is None
    assert os.environ.get("TQ_HOME") == str(tq_home)
