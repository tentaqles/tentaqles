"""Guards that the declared runtime dependencies are actually usable.

`pathspec` is declared in pyproject.toml and imported at module scope by
tentaqles.graph.native.detect, but for a while no test imported that module —
so CI stayed green without pathspec installed at all. A dependency nothing
exercises can rot out of the CI install list unnoticed; these tests make that
a failure instead.

fastembed used to be listed here as a declared-but-exempt dependency. It is now
declared where it belongs — the ``embeddings`` extra — so no exemption list is
needed: every name in ``[project].dependencies`` must import, full stop. What
that extra promises instead (writes still succeed with a NULL embedding) is
pinned by tests/test_embeddings_optional.py.
"""

from __future__ import annotations

import tomllib
from pathlib import Path

import pytest


PLUGIN_ROOT = Path(__file__).resolve().parent.parent

# pyproject names -> import names, where they differ.
IMPORT_NAME = {"pyyaml": "yaml"}


def _pyproject() -> dict:
    return tomllib.loads((PLUGIN_ROOT / "pyproject.toml").read_text(encoding="utf-8"))


def _normalise(specs) -> set[str]:
    out = set()
    for spec in specs:
        name = spec.split("[")[0].split(">")[0].split("<")[0].split("=")[0].split(";")[0]
        out.add(name.strip().lower())
    return out


def _declared_dependencies() -> set[str]:
    return _normalise(_pyproject()["project"]["dependencies"])


def _optional_dependencies() -> dict[str, set[str]]:
    extras = _pyproject()["project"].get("optional-dependencies", {})
    return {group: _normalise(specs) for group, specs in extras.items()}


def test_required_dependencies_are_importable() -> None:
    """Every declared dependency must import — there are no exemptions."""
    import importlib

    missing = []
    for dep in sorted(_declared_dependencies()):
        module = IMPORT_NAME.get(dep, dep)
        try:
            importlib.import_module(module)
        except ImportError:
            missing.append(f"{dep} (import {module})")
    assert not missing, (
        "declared runtime dependencies are not installed: " + ", ".join(missing)
        + " — add them to the pip install step in .github/workflows/test.yml"
    )


def test_pathspec_backed_ignore_matching_works(tmp_path: Path) -> None:
    """Exercise the code path that actually needs pathspec.

    Importing detect.py is what fails first when pathspec is absent, and its
    ignore matching is the only consumer, so drive it end to end rather than
    just asserting the import.
    """
    detect = pytest.importorskip(
        "tentaqles.graph.native.detect",
        reason="graph.native unavailable",
    )

    (tmp_path / ".gitignore").write_text("*.log\n!keep.log\n", encoding="utf-8")
    matcher = detect._IgnoreTree(tmp_path)

    assert matcher.is_ignored(tmp_path / "debug.log") is True
    # Negation patterns must still win — that is why a real GitIgnoreSpec is
    # used rather than fnmatch.
    assert matcher.is_ignored(tmp_path / "keep.log") is False
    assert matcher.is_ignored(tmp_path / "main.py") is False


def test_fastembed_is_declared_optional_not_required() -> None:
    """fastembed must stay out of the hard dependency list.

    CI never installs it and the memory store degrades to a NULL embedding
    without it, so declaring it required would be a lie that nothing catches —
    `pip install tentaqles` would pull onnxruntime and model weights for a
    feature the plugin does not actually require.
    """
    assert "fastembed" not in _declared_dependencies()

    extras = _optional_dependencies()
    assert "fastembed" in extras.get("embeddings", set()), (
        "fastembed should be installable via the `embeddings` extra"
    )
    assert "fastembed" in extras.get("all", set()), (
        "the `all` extra should still pull fastembed in"
    )


def test_numpy_stays_a_hard_dependency() -> None:
    """numpy is not just an embeddings concern.

    tentaqles.memory.store, memory.query_helpers, memory.pattern_detector and
    metagraph.cross_link all import numpy at module scope on paths that run
    with no embedding backend at all, so it cannot follow fastembed into an
    extra.
    """
    assert "numpy" in _declared_dependencies()

    import importlib

    for module in (
        "tentaqles.memory.query_helpers",
        "tentaqles.memory.store",
    ):
        importlib.import_module(module)
