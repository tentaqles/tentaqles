"""Tests for tentaqles.memory.pattern_detector.CrossWorkspacePatternDetector.

All tests bypass fastembed by injecting pre-built numpy embeddings directly.
"""

from __future__ import annotations

import json
import os
import sqlite3
import time
import uuid
from pathlib import Path
from unittest.mock import MagicMock

import numpy as np
import pytest

from tentaqles.memory.pattern_detector import CrossWorkspacePatternDetector


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_SCHEMA = """
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    started_at TEXT NOT NULL,
    ended_at TEXT,
    duration_s INTEGER,
    summary TEXT,
    embedding BLOB,
    tags TEXT DEFAULT '[]',
    metadata TEXT DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS decisions (
    id              TEXT PRIMARY KEY,
    session_id      TEXT REFERENCES sessions(id),
    created_at      TEXT NOT NULL,
    node_ids        TEXT NOT NULL DEFAULT '[]',
    chosen          TEXT NOT NULL,
    rejected        TEXT DEFAULT '[]',
    rationale       TEXT NOT NULL,
    confidence      TEXT DEFAULT 'medium',
    status          TEXT DEFAULT 'active',
    superseded_by   TEXT,
    embedding       BLOB,
    tags            TEXT DEFAULT '[]'
);
"""


def _make_db(path: Path, decisions: list[dict]) -> None:
    """Create a minimal memory.db with the given decisions."""
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(path))
    conn.executescript(_SCHEMA)
    # Insert a dummy session so FK doesn't fail (FK not enforced by default)
    conn.execute(
        "INSERT OR IGNORE INTO sessions (id, started_at) VALUES (?, ?)",
        ("sess-test", "2025-01-01T00:00:00Z"),
    )
    for d in decisions:
        embedding_blob = (
            np.array(d["embedding"], dtype=np.float32).tobytes()
            if d.get("embedding") is not None
            else None
        )
        conn.execute(
            "INSERT INTO decisions (id, session_id, created_at, chosen, rationale, embedding) "
            "VALUES (?, ?, ?, ?, ?, ?)",
            (
                d.get("id", str(uuid.uuid4())),
                "sess-test",
                "2025-01-01T00:00:00Z",
                d["chosen"],
                d.get("rationale", ""),
                embedding_blob,
            ),
        )
    conn.commit()
    conn.close()


def _mock_emb_service(dim: int = 8) -> MagicMock:
    """Return an EmbeddingService mock that produces deterministic embeddings."""
    svc = MagicMock()
    rng = np.random.default_rng(0)

    def fake_embed(texts):
        return rng.random((len(texts), dim), dtype=np.float64).astype(np.float32)

    svc.embed.side_effect = fake_embed
    return svc


def _make_detector(tmp_path: Path, emb_service=None) -> CrossWorkspacePatternDetector:
    """Build a detector that writes to tmp_path instead of ~/.tentaqles."""
    svc = emb_service or _mock_emb_service()
    detector = CrossWorkspacePatternDetector(emb_service=svc)
    detector._data_dir = tmp_path
    return detector


# ---------------------------------------------------------------------------
# JWT cluster test — 3 workspaces, same decision embedding space
# ---------------------------------------------------------------------------

class TestJWTPatternDetection:
    """Seed 3 temporary memory.dbs with JWT decisions that share a tight embedding cluster."""

    def test_run_finds_pattern_spanning_three_workspaces(self, tmp_path):
        # Build three workspace directories
        ws_registry = {}
        jwt_vec = np.array([1.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0], dtype=np.float32)

        for i in range(1, 4):
            ws_root = tmp_path / f"ws{i}"
            db_path = ws_root / ".claude" / "memory.db"
            _make_db(db_path, [
                {
                    "id": f"dec-jwt-{i}",
                    "chosen": "Use sliding window refresh tokens for JWT expiration",
                    "rationale": "Prevents forced logout while maintaining security",
                    "embedding": jwt_vec.tolist(),
                },
                # Extra decision to meet min_cluster_size=3 across three workspaces
                {
                    "id": f"dec-jwt-extra-{i}",
                    "chosen": "JWT access token lifetime 15 minutes with refresh",
                    "rationale": "Short lived JWT tokens reduce risk on compromise",
                    "embedding": (jwt_vec * 0.99 + 0.01).tolist(),
                },
            ])
            ws_registry[f"workspace_{i}"] = {"root_path": str(ws_root)}

        # Use a mock that returns the stored embeddings (bypasses fastembed)
        # Because stored blobs are set, embed() should never be called for these rows.
        svc = _mock_emb_service()
        detector = _make_detector(tmp_path, svc)

        result = detector.run(ws_registry, min_cluster_size=3, min_workspaces=3, n_clusters=2)

        assert result["patterns_found"] >= 1

        patterns = detector.load_patterns()
        assert len(patterns) >= 1

        # At least one pattern must span all three workspaces
        three_ws_patterns = [
            p for p in patterns
            if len(p["workspaces"]) >= 3
        ]
        assert three_ws_patterns, "Expected at least one pattern spanning 3 workspaces"

        # The label should contain JWT-related tokens
        labels = " ".join(p["label"].lower() for p in three_ws_patterns)
        assert any(tok in labels for tok in ("jwt", "token", "refresh", "sliding", "window"))

    def test_patterns_json_schema(self, tmp_path):
        """Output file must conform to tentaqles-patterns-v1 schema."""
        ws_root = tmp_path / "ws1"
        db_path = ws_root / ".claude" / "memory.db"
        vec = np.ones(4, dtype=np.float32).tolist()
        _make_db(db_path, [
            {"id": f"d{i}", "chosen": f"Decision {i}", "rationale": "r", "embedding": vec}
            for i in range(4)
        ])
        registry = {"workspace_a": {"root_path": str(ws_root)}}
        detector = _make_detector(tmp_path)
        detector.run(registry, min_cluster_size=1, min_workspaces=1, n_clusters=2)

        out_path = tmp_path / "metagraph" / "patterns.json"
        assert out_path.exists()
        data = json.loads(out_path.read_text(encoding="utf-8"))
        assert data["schema"] == "tentaqles-patterns-v1"
        assert "generated_at" in data
        assert isinstance(data["patterns"], list)
        if data["patterns"]:
            p = data["patterns"][0]
            for key in ("id", "label", "workspaces", "decision_count",
                        "representative_decision", "similarity_score"):
                assert key in p, f"Pattern missing key '{key}'"


# ---------------------------------------------------------------------------
# _load_all_decisions — missing db graceful skip
# ---------------------------------------------------------------------------

class TestLoadAllDecisions:
    def test_skips_missing_db(self, tmp_path):
        registry = {
            "missing_ws": {"root_path": str(tmp_path / "does_not_exist")},
        }
        detector = _make_detector(tmp_path)
        decisions = detector._load_all_decisions(registry)
        assert decisions == []

    def test_skips_ws_with_no_decisions_table(self, tmp_path):
        ws_root = tmp_path / "empty_ws"
        db_path = ws_root / ".claude" / "memory.db"
        db_path.parent.mkdir(parents=True, exist_ok=True)
        # Create DB but don't create decisions table
        conn = sqlite3.connect(str(db_path))
        conn.execute("CREATE TABLE foo (id TEXT)")
        conn.commit()
        conn.close()

        registry = {"empty_ws": {"root_path": str(ws_root)}}
        detector = _make_detector(tmp_path)
        decisions = detector._load_all_decisions(registry)
        assert decisions == []

    def test_loads_decisions_from_valid_db(self, tmp_path):
        ws_root = tmp_path / "valid_ws"
        db_path = ws_root / ".claude" / "memory.db"
        _make_db(db_path, [
            {"id": "d1", "chosen": "Use Postgres", "rationale": "Performance", "embedding": None},
        ])

        registry = {"valid_ws": {"root_path": str(ws_root)}}
        detector = _make_detector(tmp_path)
        decisions = detector._load_all_decisions(registry)

        assert len(decisions) == 1
        assert decisions[0]["workspace_id"] == "valid_ws"
        assert decisions[0]["decision_id"] == "d1"
        assert decisions[0]["chosen"] == "Use Postgres"

    def test_mixed_registry_returns_only_valid(self, tmp_path):
        ws_root = tmp_path / "valid2"
        db_path = ws_root / ".claude" / "memory.db"
        _make_db(db_path, [
            {"id": "x1", "chosen": "Redis cache", "rationale": "Speed", "embedding": None},
        ])

        registry = {
            "missing": {"root_path": str(tmp_path / "missing")},
            "valid2": {"root_path": str(ws_root)},
        }
        detector = _make_detector(tmp_path)
        decisions = detector._load_all_decisions(registry)
        assert len(decisions) == 1
        assert decisions[0]["workspace_id"] == "valid2"


# ---------------------------------------------------------------------------
# load_patterns — missing file returns []
# ---------------------------------------------------------------------------

class TestLoadPatterns:
    def test_returns_empty_list_when_file_missing(self, tmp_path):
        detector = _make_detector(tmp_path)
        result = detector.load_patterns()
        assert result == []

    def test_returns_patterns_after_run(self, tmp_path):
        ws_root = tmp_path / "ws"
        db_path = ws_root / ".claude" / "memory.db"
        vec = np.array([1.0, 0.0, 0.0, 0.0], dtype=np.float32).tolist()
        _make_db(db_path, [
            {"id": f"d{i}", "chosen": "Cache decision", "rationale": "perf", "embedding": vec}
            for i in range(5)
        ])
        registry = {"ws": {"root_path": str(ws_root)}}
        detector = _make_detector(tmp_path)
        detector.run(registry, min_cluster_size=1, min_workspaces=1, n_clusters=2)

        patterns = detector.load_patterns()
        assert isinstance(patterns, list)

    def test_returns_empty_list_on_corrupt_json(self, tmp_path):
        out_dir = tmp_path / "metagraph"
        out_dir.mkdir(parents=True, exist_ok=True)
        (out_dir / "patterns.json").write_text("not json", encoding="utf-8")
        detector = _make_detector(tmp_path)
        assert detector.load_patterns() == []


# ---------------------------------------------------------------------------
# Read-only isolation — source DBs must not be modified during detection
# ---------------------------------------------------------------------------

class TestIsolation:
    def test_source_dbs_not_written_during_detection(self, tmp_path):
        ws_root = tmp_path / "source_ws"
        db_path = ws_root / ".claude" / "memory.db"
        vec = np.array([1.0, 0.0, 0.0, 0.0], dtype=np.float32).tolist()
        _make_db(db_path, [
            {"id": "iso1", "chosen": "Do not modify", "rationale": "isolation", "embedding": vec},
            {"id": "iso2", "chosen": "Do not modify", "rationale": "isolation", "embedding": vec},
            {"id": "iso3", "chosen": "Do not modify", "rationale": "isolation", "embedding": vec},
        ])

        mtime_before = db_path.stat().st_mtime

        registry = {"source_ws": {"root_path": str(ws_root)}}
        detector = _make_detector(tmp_path)
        detector.run(registry, min_cluster_size=1, min_workspaces=1, n_clusters=2)

        mtime_after = db_path.stat().st_mtime
        assert mtime_after == mtime_before, (
            "Source memory.db was modified during detection — read-only mode not enforced"
        )


# ---------------------------------------------------------------------------
# _cluster — basic shape / assignment contract
# ---------------------------------------------------------------------------

class TestCluster:
    def test_returns_correct_shape(self, tmp_path):
        detector = _make_detector(tmp_path)
        rng = np.random.default_rng(0)
        embeddings = rng.random((10, 4), dtype=np.float64).astype(np.float32)
        assignments = detector._cluster(embeddings, n_clusters=3)
        assert assignments.shape == (10,)
        assert set(assignments).issubset({0, 1, 2})

    def test_fewer_points_than_clusters(self, tmp_path):
        detector = _make_detector(tmp_path)
        embeddings = np.eye(2, dtype=np.float32)
        assignments = detector._cluster(embeddings, n_clusters=5)
        assert len(assignments) == 2

    def test_identical_embeddings_same_cluster(self, tmp_path):
        detector = _make_detector(tmp_path)
        vec = np.array([[1.0, 0.0, 0.0], [1.0, 0.0, 0.0], [1.0, 0.0, 0.0]], dtype=np.float32)
        assignments = detector._cluster(vec, n_clusters=2)
        # All three identical vectors must land in the same cluster
        assert len(set(assignments.tolist())) == 1


# ---------------------------------------------------------------------------
# _label_cluster
# ---------------------------------------------------------------------------

class TestLabelCluster:
    def test_returns_string(self, tmp_path):
        detector = _make_detector(tmp_path)
        decisions = [
            {"chosen": "Use JWT refresh tokens", "rationale": "security"},
            {"chosen": "JWT sliding window refresh", "rationale": "ux"},
            {"chosen": "Refresh tokens with JWT", "rationale": "perf"},
        ]
        label = detector._label_cluster(decisions)
        assert isinstance(label, str)
        assert len(label) > 0

    def test_label_contains_dominant_token(self, tmp_path):
        detector = _make_detector(tmp_path)
        decisions = [
            {"chosen": "Always use Redis caching", "rationale": "speed"},
            {"chosen": "Redis based caching layer", "rationale": "scale"},
            {"chosen": "Redis cluster for caching", "rationale": "ha"},
        ]
        label = detector._label_cluster(decisions)
        assert "redis" in label.lower() or "caching" in label.lower()

    def test_fallback_for_stopword_only_text(self, tmp_bp=None):
        # If all tokens are stopwords, fall back to first 60 chars
        import tempfile
        with tempfile.TemporaryDirectory() as tmp:
            detector = _make_detector(Path(tmp))
            decisions = [
                {"chosen": "to be or not to be", "rationale": ""},
                {"chosen": "to be or not to be", "rationale": ""},
            ]
            label = detector._label_cluster(decisions)
            assert isinstance(label, str)
