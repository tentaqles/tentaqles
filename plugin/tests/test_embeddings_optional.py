"""fastembed is optional: pin both the absent path and the present path.

fastembed lives in ``[project.optional-dependencies].embeddings``, not in
``[project].dependencies``, and CI deliberately never installs it (it pulls
onnxruntime and downloads model weights). That leaves two behaviours untested
by default, which is exactly how the graceful-degradation promise in CLAUDE.md
could quietly stop being true:

* absent  — a memory write must still succeed, with ``embedding`` stored NULL.
* present — a memory write must actually persist a float32 blob.

Both states are simulated, so these tests give the same verdict whether or not
fastembed happens to be installed on the machine running them.
"""

from __future__ import annotations

import importlib
import json
import os
import sqlite3
import subprocess
import sys
import textwrap
import types
from pathlib import Path

import numpy as np
import pytest

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SERVICE_PY = PLUGIN_ROOT / "tentaqles" / "embeddings" / "service.py"


# ---------------------------------------------------------------------------
# fastembed absent
# ---------------------------------------------------------------------------


def test_fastembed_is_only_imported_lazily(monkeypatch) -> None:
    """The service module must import with fastembed unavailable.

    fastembed is imported inside ``EmbeddingService._ensure_model``. If it ever
    moved to module scope, every consumer of the memory store would fail to
    import on a machine without it, so pin the import site both structurally
    and by actually importing under a poisoned ``sys.modules``.
    """
    src = SERVICE_PY.read_text(encoding="utf-8")
    top_level = [
        line
        for line in src.splitlines()
        if line.startswith(("import fastembed", "from fastembed"))
    ]
    assert not top_level, f"fastembed imported at module scope: {top_level}"

    monkeypatch.setitem(sys.modules, "fastembed", None)
    monkeypatch.delitem(sys.modules, "tentaqles.embeddings.service", raising=False)
    service = importlib.import_module("tentaqles.embeddings.service")
    assert service.EmbeddingService is not None


def test_service_embed_raises_when_fastembed_missing(monkeypatch, tmp_path: Path) -> None:
    """The service raises; the store is what swallows it.

    This documents where the boundary sits. If EmbeddingService ever started
    returning None instead of raising, ``MemoryStore._embed``'s
    ``except Exception`` would silently stop being what provides degradation.
    """
    monkeypatch.setitem(sys.modules, "fastembed", None)
    from tentaqles.embeddings.service import EmbeddingService

    svc = EmbeddingService(cache_dir=tmp_path / "embcache")
    with pytest.raises(ImportError):
        svc.embed(["hello"])


_PROBE = textwrap.dedent(
    """
    import json, sqlite3, sys
    from pathlib import Path

    # Poison the import so `import fastembed` raises ImportError even when the
    # package is installed on this machine.
    sys.modules["fastembed"] = None
    sys.path.insert(0, {plugin_root!r})

    from tentaqles.memory.store import MemoryStore

    ws = Path({workspace!r})
    store = MemoryStore(ws)
    sid = store.start_session(tags=["probe"])
    did = store.record_decision("use sqlite", "single file, no server")
    ended = store.end_session("wired up the parser")

    conn = sqlite3.connect(str(ws / ".claude" / "memory.db"))
    srow = conn.execute(
        "SELECT summary, embedding FROM sessions WHERE id=?", (sid,)
    ).fetchone()
    drow = conn.execute(
        "SELECT chosen, embedding FROM decisions WHERE id=?", (did,)
    ).fetchone()

    print(json.dumps({{
        "session_id": sid,
        "ended_id": ended.get("id"),
        "session_summary": srow[0],
        "session_embedding_is_null": srow[1] is None,
        "decision_chosen": drow[0],
        "decision_embedding_is_null": drow[1] is None,
    }}))
    """
)


def test_memory_writes_degrade_to_null_embedding(tmp_path: Path) -> None:
    """The CLAUDE.md claim, end to end, where fastembed genuinely cannot import.

    A subprocess rather than monkeypatch, so the real ``MemoryStore._get_emb``
    construction path runs (default cache dir included) with
    ``TENTAQLES_DATA_DIR`` redirected into tmp_path — nothing touches the
    developer's data directory.
    """
    workspace = tmp_path / "ws"
    workspace.mkdir()

    script = _PROBE.format(plugin_root=str(PLUGIN_ROOT), workspace=str(workspace))

    env = dict(os.environ)
    env.pop("CLAUDE_PLUGIN_DATA", None)
    env["TENTAQLES_DATA_DIR"] = str(tmp_path / "data")

    proc = subprocess.run(
        [sys.executable, "-c", script],
        capture_output=True,
        text=True,
        env=env,
        timeout=180,
    )
    assert proc.returncode == 0, f"probe failed:\n{proc.stdout}\n{proc.stderr}"

    result = json.loads(proc.stdout.strip().splitlines()[-1])
    assert result["ended_id"] == result["session_id"]
    assert result["session_summary"] == "wired up the parser"
    assert result["decision_chosen"] == "use sqlite"
    assert result["session_embedding_is_null"] is True
    assert result["decision_embedding_is_null"] is True


# ---------------------------------------------------------------------------
# fastembed present (stubbed)
# ---------------------------------------------------------------------------


class _StubTextEmbedding:
    """Stand-in for fastembed.TextEmbedding with deterministic 4-d output."""

    dim = 4

    def __init__(self, model_name: str, *args, **kwargs):
        self.model_name = model_name

    def embed(self, texts):
        for i, text in enumerate(texts):
            base = float(len(text))
            yield [base, base + 1.0, float(i), 0.5]


def _install_stub_fastembed(monkeypatch) -> None:
    module = types.ModuleType("fastembed")
    module.TextEmbedding = _StubTextEmbedding
    monkeypatch.setitem(sys.modules, "fastembed", module)


def test_service_embeds_through_fastembed_when_present(monkeypatch, tmp_path: Path) -> None:
    """The embedding path itself — previously covered by nothing."""
    _install_stub_fastembed(monkeypatch)
    from tentaqles.embeddings.service import EmbeddingService

    svc = EmbeddingService(cache_dir=tmp_path / "embcache")
    out = svc.embed(["alpha", "beta"])

    assert isinstance(out, np.ndarray)
    assert out.shape == (2, _StubTextEmbedding.dim)
    assert out.dtype == np.float32
    assert svc.dimension == _StubTextEmbedding.dim

    # Repeat call must be served from cache, and the cache must round-trip.
    again = svc.embed(["alpha"])
    assert np.array_equal(again[0], out[0])
    assert svc._cache.stats()["cached_embeddings"] == 2


def test_memory_writes_store_a_blob_when_fastembed_present(
    monkeypatch, tmp_path: Path
) -> None:
    """With a working backend the same writes must persist a float32 blob."""
    _install_stub_fastembed(monkeypatch)
    from tentaqles.embeddings.service import EmbeddingService
    from tentaqles.memory.store import MemoryStore

    workspace = tmp_path / "ws"
    workspace.mkdir()
    store = MemoryStore(workspace)
    # Inject a service with a redirected cache dir; _get_emb() returns it as-is.
    store._emb = EmbeddingService(cache_dir=tmp_path / "embcache")

    sid = store.start_session()
    did = store.record_decision("use sqlite", "single file, no server")
    store.end_session("wired up the parser")

    conn = sqlite3.connect(str(workspace / ".claude" / "memory.db"))
    session_blob = conn.execute(
        "SELECT embedding FROM sessions WHERE id=?", (sid,)
    ).fetchone()[0]
    decision_blob = conn.execute(
        "SELECT embedding FROM decisions WHERE id=?", (did,)
    ).fetchone()[0]

    for blob in (session_blob, decision_blob):
        assert blob is not None
        assert np.frombuffer(blob, dtype=np.float32).shape == (_StubTextEmbedding.dim,)
