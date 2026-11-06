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


# ---------------------------------------------------------------------------
# Resolution through the registry, like tq (no nested-marker shadowing)
# ---------------------------------------------------------------------------


def _register(tq_home: Path, *bases: Path) -> None:
    lines = "".join(f"  - {b}\n" for b in bases)
    (tq_home / "config.yaml").write_text(f"bases:\n{lines}", encoding="utf-8")


def test_nested_root_file_cannot_shadow_the_registered_workspace(tmp_path, tq_home):
    """base/acme/beta/.tentaqles.yaml naming beta is a planted file: tq
    resolves acme (the base's first-level folder) and so must the plugin."""
    base = tmp_path / "base"
    root = base / "acme"
    _marker(root)
    pp = _protected(tq_home)
    _register(tq_home, base)
    nested = root / "beta"
    nested.mkdir()
    (nested / ".tentaqles.yaml").write_text(
        "schema: tentaqles-client-v2\nclient: beta\ngit:\n  email: beta@evil.test\n", encoding="utf-8"
    )
    (nested / "src").mkdir()

    for cwd in (nested, nested / "src", root):
        data = loader.load_manifest(cwd)
        assert data is not None, cwd
        assert data["client"] == "acme"
        assert data["git"]["email"] == "dev@acme.test"
        assert Path(data["_manifest_path"]) == pp.resolve()
        assert Path(data["_client_root"]) == root.resolve()
        assert loader.find_manifest(cwd) == pp.resolve()
    assert loader.get_client_context(nested)["client"] == "acme"


def test_registry_resolution_is_neutral_at_the_base_root_and_outside_bases(tmp_path, tq_home):
    base = tmp_path / "base"
    _marker(base / "acme")
    _protected(tq_home)
    _register(tq_home, base)
    assert loader.load_manifest(base) is None
    elsewhere = tmp_path / "elsewhere" / "acme"
    _marker(elsewhere)
    assert loader.load_manifest(elsewhere) is None, "a folder outside every base is neutral, marker or not"
    plain = base / "plain" / "deep"
    plain.mkdir(parents=True)
    assert loader.load_manifest(plain) is None, "a first-level folder without a root file is neutral"


def test_registry_resolution_reads_the_old_layout_too(tmp_path, tq_home):
    base = tmp_path / "base"
    root = base / "acme"
    root.mkdir(parents=True)
    rf = root / ".tentaqles.yaml"
    rf.write_text(MANIFEST, encoding="utf-8")
    _register(tq_home, base)
    data = loader.load_manifest(root / "repo")
    assert data is not None and data["client"] == "acme"
    assert Path(data["_manifest_path"]) == rf.resolve()


def test_without_a_registry_the_walk_up_still_applies(tmp_path, tq_home):
    """No config.yaml and no agent marker: the plugin without tq keeps
    finding the nearest root file."""
    root = tmp_path / "anywhere" / "acme"
    root.mkdir(parents=True)
    (root / ".tentaqles.yaml").write_text(MANIFEST, encoding="utf-8")
    (root / "deep").mkdir()
    assert not (tq_home / "config.yaml").exists()
    data = loader.load_manifest(root / "deep")
    assert data is not None and data["client"] == "acme"


def test_agent_session_without_a_registry_is_neutral(tmp_path, tq_home, monkeypatch):
    """Under an agent marker with no config.yaml the walk-up is off: tq itself
    would resolve "outside any base", so a nested root file is unknown."""
    root = tmp_path / "anywhere" / "acme"
    (root / "deep").mkdir(parents=True)
    (root / ".tentaqles.yaml").write_text(MANIFEST, encoding="utf-8")
    assert not (tq_home / "config.yaml").exists()
    monkeypatch.setenv("CLAUDECODE", "1")
    assert loader.load_manifest(root / "deep") is None
    assert loader.load_manifest(root) is None
    assert loader.find_manifest(root / "deep") is None
    assert loader.get_client_context(root / "deep")["client"] == "unknown"
    # Registering the base brings it back, as on a machine with tq.
    _register(tq_home, tmp_path / "anywhere")
    data = loader.load_manifest(root / "deep")
    assert data is not None and data["client"] == "acme"


# ---------------------------------------------------------------------------
# TQ_HOME under an agent marker
# ---------------------------------------------------------------------------


def test_tq_home_is_ignored_under_an_agent_marker(tmp_path, monkeypatch):
    from tentaqles import knowledge, tqhome

    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setattr(Path, "home", classmethod(lambda cls: home))
    forged = tmp_path / "forged"
    monkeypatch.setenv("TQ_HOME", str(forged))
    for k in tqhome.AGENT_MARKERS:
        monkeypatch.delenv(k, raising=False)
    monkeypatch.setattr(tqhome, "ALLOW_HOME_OVERRIDE", False)

    assert tqhome.tq_home() == forged, "no marker: TQ_HOME applies"
    monkeypatch.setenv("CLAUDECODE", "1")
    assert tqhome.tq_home() == home / ".tentaqles"
    assert loader.tq_home() == home / ".tentaqles"
    assert knowledge.tq_home() == home / ".tentaqles"
    monkeypatch.setattr(tqhome, "ALLOW_HOME_OVERRIDE", True)
    assert tqhome.tq_home() == forged, "the tests' override seam"


def test_forged_tq_home_cannot_supply_the_manifest_under_a_marker(tmp_path, monkeypatch):
    from tentaqles import tqhome

    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.setattr(Path, "home", classmethod(lambda cls: home))
    root = tmp_path / "base" / "acme"
    _marker(root)
    real = _protected(home / ".tentaqles")
    _register(home / ".tentaqles", tmp_path / "base")
    forged = tmp_path / "forged"
    _protected(forged, body=MANIFEST.replace("dev@acme.test", "forged@acme.test"))
    _register(forged, tmp_path / "base")
    monkeypatch.setenv("TQ_HOME", str(forged))
    monkeypatch.setenv("CLAUDECODE", "1")
    monkeypatch.setattr(tqhome, "ALLOW_HOME_OVERRIDE", False)

    data = loader.load_manifest(root)
    assert data is not None
    assert data["git"]["email"] == "dev@acme.test"
    assert Path(data["_manifest_path"]) == real.resolve()
