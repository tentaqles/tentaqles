"""Tests for tentaqles.memory.decay pure functions."""

import math
import sqlite3

import pytest

from tentaqles.memory.decay import decay_sql_expr, ebbinghaus_score, strengthen_memory


class TestEbbinghausScore:
    def test_at_t0_equals_strength(self):
        # At day 0 with 0 recalls: score == strength * exp(0) * 1.0 == strength
        result = ebbinghaus_score(1.0, 0.0, 0)
        assert abs(result - 1.0) < 1e-6

    def test_at_half_life_approximately_half(self):
        # At 30 days with 0 recalls: exp(-1) ≈ 0.368, not 0.5.
        # The formula uses natural exp, not base-2.
        result = ebbinghaus_score(1.0, 30.0, 0)
        expected = math.exp(-1.0)
        assert abs(result - expected) < 1e-6

    def test_recall_boost(self):
        # More recalls raise the score.
        score_no_recall = ebbinghaus_score(1.0, 30.0, 0)
        score_with_recall = ebbinghaus_score(1.0, 30.0, 5)
        assert score_with_recall > score_no_recall

    def test_clamp_to_one(self):
        # Extreme recall count should not exceed 1.0
        result = ebbinghaus_score(1.0, 0.0, 1000)
        assert result <= 1.0

    def test_clamp_to_zero(self):
        result = ebbinghaus_score(0.0, 999.0, 0)
        assert result == 0.0

    def test_partial_strength(self):
        result = ebbinghaus_score(0.5, 0.0, 0)
        assert abs(result - 0.5) < 1e-6


class TestDecaySqlExpr:
    def test_returns_string(self):
        expr = decay_sql_expr()
        assert isinstance(expr, str)

    def test_contains_julianday(self):
        expr = decay_sql_expr()
        assert "julianday" in expr

    def test_custom_params_reflected(self):
        expr = decay_sql_expr(half_life_days=14.0, recalled_col="my_col", strength_col="s")
        assert "14.0" in expr
        assert "my_col" in expr
        assert "s" in expr

    def test_evaluates_in_sqlite(self):
        # Smoke test: use the expression in an actual SQLite query.
        conn = sqlite3.connect(":memory:")
        conn.execute(
            "CREATE TABLE mem (id TEXT PRIMARY KEY, strength REAL, last_recalled TEXT)"
        )
        conn.execute(
            "INSERT INTO mem VALUES ('a', 1.0, datetime('now', '-30 days'))"
        )
        conn.commit()
        expr = decay_sql_expr()
        row = conn.execute(f"SELECT {expr} FROM mem WHERE id='a'").fetchone()
        assert row is not None
        assert 0.0 < row[0] < 1.0


class TestStrengthenMemory:
    def _make_db(self):
        conn = sqlite3.connect(":memory:")
        conn.execute(
            """CREATE TABLE semantic_memories (
                id TEXT PRIMARY KEY,
                strength REAL DEFAULT 1.0,
                recall_count INTEGER DEFAULT 0,
                last_recalled TEXT
            )"""
        )
        conn.execute("INSERT INTO semantic_memories VALUES ('x', 0.5, 2, NULL)")
        conn.commit()
        return conn

    def test_strength_increases(self):
        conn = self._make_db()
        strengthen_memory(conn, "semantic_memories", "x")
        row = conn.execute(
            "SELECT strength, recall_count FROM semantic_memories WHERE id='x'"
        ).fetchone()
        assert row[0] == pytest.approx(0.6)
        assert row[1] == 3

    def test_strength_capped_at_one(self):
        conn = self._make_db()
        conn.execute("UPDATE semantic_memories SET strength=0.99 WHERE id='x'")
        conn.commit()
        strengthen_memory(conn, "semantic_memories", "x")
        row = conn.execute("SELECT strength FROM semantic_memories WHERE id='x'").fetchone()
        assert row[0] == pytest.approx(1.0)

    def test_last_recalled_set(self):
        conn = self._make_db()
        strengthen_memory(conn, "semantic_memories", "x")
        row = conn.execute(
            "SELECT last_recalled FROM semantic_memories WHERE id='x'"
        ).fetchone()
        assert row[0] is not None

    def test_unknown_table_raises(self):
        conn = self._make_db()
        with pytest.raises(ValueError, match="unknown table"):
            strengthen_memory(conn, "bad_table", "x")
