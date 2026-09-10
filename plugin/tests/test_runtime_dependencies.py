"""Guards that the declared runtime dependencies are actually usable.

`pathspec` is declared in pyproject.toml and imported at module scope by
tentaqles.graph.native.detect, but for a while no test imported that module —
so CI stayed green without pathspec installed at all. A dependency nothing
exercises can rot out of the CI install list unnoticed; these tests make that
a failure instead.

fastembed is intentionally excluded: it is heavy and everything degrades
gracefully without it (embeddings are stored as NULL), so it is a declared
runtime dependency that CI deliberately does not install.
"""

from __future__ import annotations

import tomllib
from pathlib import Path

import pytest


PLUGIN_ROOT = Path(__file__).resolve().parent.parent

# Declared deps that CI is expected to install and that must be importable.
# fastembed is optional at runtime — see the module docstring.
OPTIONAL_AT_RUNTIME = {"fastembed"}

# pyproject names -> import names, where they differ.
IMPORT_NAME = {"pyyaml": "yaml"}


def _declared_dependencies() -> set[str]:
    data = tomllib.loads((PLUGIN_ROOT / "pyproject.toml").read_text(encoding="utf-8"))
    out = set()
    for spec in data["project"]["dependencies"]:
        name = spec.split("[")[0].split(">")[0].split("<")[0].split("=")[0].split(";")[0]
        out.add(name.strip().lower())
    return out


def test_required_dependencies_are_importable() -> None:
    """Every non-optional declared dependency must import."""
    import importlib

    missing = []
    for dep in sorted(_declared_dependencies() - OPTIONAL_AT_RUNTIME):
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
