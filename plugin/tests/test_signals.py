"""Tests for tentaqles.memory.signals.SignalBus."""

from __future__ import annotations

import sqlite3
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from tentaqles.memory.meta import MetaMemory
from tentaqles.memory.signals import SignalBus


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def meta_db(tmp_path) -> Path:
    """Create a fresh meta.db with schema initialised via MetaMemory."""
    db_path = tmp_path / "meta.db"
    meta = MetaMemory(db_path=db_path)
    meta.close()
    return db_path


@pytest.fixture
def bus(meta_db) -> SignalBus:
    return SignalBus(meta_db_path=meta_db)


@pytest.fixture
def bus_with_workspaces(meta_db) -> SignalBus:
    """SignalBus where workspace_a and workspace_b are registered."""
    meta = MetaMemory(db_path=meta_db)
    meta.update_workspace("workspace_a", "Workspace A", "/a", "summary a", [])
    meta.update_workspace("workspace_b", "Workspace B", "/b", "summary b", [])
    meta.close()
    return SignalBus(meta_db_path=meta_db)


# ---------------------------------------------------------------------------
# emit() and read_pending()
# ---------------------------------------------------------------------------


class TestEmitAndReadPending:
    def test_emit_writes_row_and_read_pending_returns_it(self, bus):
        """emit() persists the signal; read_pending() returns it."""
        sig_id = bus.emit(
            from_workspace="ws_a",
            to_workspace="ws_b",
            event_type="deploy",
            message="Deploy complete",
            payload={"env": "staging"},
        )
        assert sig_id  # non-empty hex string

        pending = bus.read_pending("ws_b")
        assert len(pending) == 1
        s = pending[0]
        assert s["id"] == sig_id
        assert s["from_workspace"] == "ws_a"
        assert s["event_type"] == "deploy"
        assert s["message"] == "Deploy complete"
        assert s["payload"] == {"env": "staging"}
        assert s["emitted_at"]

    def test_emit_returns_unique_ids(self, bus):
        id1 = bus.emit("ws_a", "ws_b", "alert", "msg1")
        id2 = bus.emit("ws_a", "ws_b", "alert", "msg2")
        assert id1 != id2

    def test_read_pending_no_signals(self, bus):
        assert bus.read_pending("ws_b") == []

    def test_payload_defaults_to_empty_dict(self, bus):
        bus.emit("ws_a", "ws_b", "ci", "CI passed")
        pending = bus.read_pending("ws_b")
        assert pending[0]["payload"] == {}

    def test_emit_validates_target_when_workspaces_registered(self, bus_with_workspaces):
        """emit() raises ValueError for unknown target when workspace_status is non-empty."""
        with pytest.raises(ValueError, match="Unknown target workspace"):
            bus_with_workspaces.emit("workspace_a", "nonexistent", "alert", "oops")

    def test_emit_accepts_any_target_when_table_empty(self, bus):
        """When workspace_status is empty, emit() skips validation (first-run smoke test)."""
        sig_id = bus.emit("ws_a", "ws_b", "alert", "hello")
        assert sig_id

    def test_emit_to_known_workspace_works(self, bus_with_workspaces):
        sig_id = bus_with_workspaces.emit(
            "workspace_a", "workspace_b", "pr", "PR merged"
        )
        pending = bus_with_workspaces.read_pending("workspace_b")
        assert len(pending) == 1
        assert pending[0]["id"] == sig_id


# ---------------------------------------------------------------------------
# acknowledge()
# ---------------------------------------------------------------------------


class TestAcknowledge:
    def test_acknowledge_removes_from_pending(self, bus):
        sig_id = bus.emit("ws_a", "ws_b", "alert", "hello")
        assert len(bus.read_pending("ws_b")) == 1

        bus.acknowledge(sig_id, "ws_b")
        assert bus.read_pending("ws_b") == []

    def test_acknowledge_sets_read_by_and_read_at(self, meta_db, bus):
        sig_id = bus.emit("ws_a", "ws_b", "alert", "hello")
        bus.acknowledge(sig_id, "ws_b")

        conn = sqlite3.connect(str(meta_db))
        row = conn.execute(
            "SELECT read_by, read_at FROM signals WHERE id = ?", (sig_id,)
        ).fetchone()
        conn.close()

        assert row[0] == "ws_b"
        assert row[1] is not None

    def test_multiple_signals_acknowledge_one(self, bus):
        id1 = bus.emit("ws_a", "ws_b", "alert", "first")
        id2 = bus.emit("ws_a", "ws_b", "alert", "second")

        bus.acknowledge(id1, "ws_b")
        pending = bus.read_pending("ws_b")
        assert len(pending) == 1
        assert pending[0]["id"] == id2

    def test_acknowledge_is_idempotent(self, bus):
        sig_id = bus.emit("ws_a", "ws_b", "alert", "hello")
        bus.acknowledge(sig_id, "ws_b")
        # Second acknowledge should not raise
        bus.acknowledge(sig_id, "ws_b")
        assert bus.read_pending("ws_b") == []


# ---------------------------------------------------------------------------
# Expiry / TTL
# ---------------------------------------------------------------------------


class TestExpiry:
    def test_expired_signals_not_in_read_pending(self, meta_db):
        """Signals with expires_at in the past never appear in read_pending()."""
        conn = sqlite3.connect(str(meta_db))
        conn.execute("PRAGMA journal_mode=WAL")
        past = (datetime.now(timezone.utc) - timedelta(hours=1)).strftime("%Y-%m-%d %H:%M:%S.%f")
        conn.execute(
            """INSERT INTO signals
               (id, from_workspace, to_workspace, event_type,
                payload, message, emitted_at, expires_at)
               VALUES ('expired_sig', 'ws_a', 'ws_b', 'alert',
               '{}', 'old news', ?, ?)""",
            (past, past),
        )
        conn.commit()
        conn.close()

        bus = SignalBus(meta_db_path=meta_db)
        pending = bus.read_pending("ws_b")
        assert all(s["id"] != "expired_sig" for s in pending)

    def test_non_expired_signal_appears_in_read_pending(self, bus):
        sig_id = bus.emit("ws_a", "ws_b", "ci", "passed", ttl_hours=48.0)
        pending = bus.read_pending("ws_b")
        assert any(s["id"] == sig_id for s in pending)


# ---------------------------------------------------------------------------
# prune_expired()
# ---------------------------------------------------------------------------


class TestPruneExpired:
    def test_prune_returns_count_of_deleted_rows(self, meta_db):
        conn = sqlite3.connect(str(meta_db))
        conn.execute("PRAGMA journal_mode=WAL")
        _fmt = "%Y-%m-%d %H:%M:%S"
        past = (datetime.now(timezone.utc) - timedelta(hours=1)).strftime(_fmt)
        future = (datetime.now(timezone.utc) + timedelta(hours=48)).strftime(_fmt)
        now_ts = datetime.now(timezone.utc).strftime(_fmt)

        conn.execute(
            "INSERT INTO signals (id, from_workspace, to_workspace, event_type, "
            "payload, message, emitted_at, expires_at) VALUES (?,?,?,?,?,?,?,?)",
            ("exp1", "ws_a", "ws_b", "alert", "{}", "old", past, past),
        )
        conn.execute(
            "INSERT INTO signals (id, from_workspace, to_workspace, event_type, "
            "payload, message, emitted_at, expires_at) VALUES (?,?,?,?,?,?,?,?)",
            ("exp2", "ws_a", "ws_b", "ci", "{}", "also old", past, past),
        )
        conn.execute(
            "INSERT INTO signals (id, from_workspace, to_workspace, event_type, "
            "payload, message, emitted_at, expires_at) VALUES (?,?,?,?,?,?,?,?)",
            ("live1", "ws_a", "ws_b", "pr", "{}", "fresh", now_ts, future),
        )
        conn.commit()
        conn.close()

        bus = SignalBus(meta_db_path=meta_db)
        count = bus.prune_expired()
        assert count == 2

    def test_prune_removes_expired_rows(self, meta_db):
        conn = sqlite3.connect(str(meta_db))
        conn.execute("PRAGMA journal_mode=WAL")
        past = (datetime.now(timezone.utc) - timedelta(hours=1)).strftime("%Y-%m-%d %H:%M:%S.%f")
        conn.execute(
            "INSERT INTO signals (id, from_workspace, to_workspace, event_type, "
            "payload, message, emitted_at, expires_at) VALUES (?,?,?,?,?,?,?,?)",
            ("dead", "ws_a", "ws_b", "alert", "{}", "msg", past, past),
        )
        conn.commit()
        conn.close()

        bus = SignalBus(meta_db_path=meta_db)
        bus.prune_expired()

        conn2 = sqlite3.connect(str(meta_db))
        row = conn2.execute("SELECT id FROM signals WHERE id='dead'").fetchone()
        conn2.close()
        assert row is None

    def test_prune_returns_zero_when_nothing_expired(self, bus):
        bus.emit("ws_a", "ws_b", "alert", "fresh", ttl_hours=48.0)
        count = bus.prune_expired()
        assert count == 0


# ---------------------------------------------------------------------------
# Isolation: workspace B signals do not bleed to workspace A
# ---------------------------------------------------------------------------


class TestIsolation:
    def test_workspace_b_pending_not_returned_for_workspace_a(self, bus):
        """Signals addressed to ws_b must not appear in ws_a's pending list."""
        bus.emit("ws_c", "ws_b", "deploy", "for B only")
        assert bus.read_pending("ws_a") == []

    def test_workspace_a_pending_not_returned_for_workspace_b(self, bus):
        bus.emit("ws_c", "ws_a", "deploy", "for A only")
        assert bus.read_pending("ws_b") == []

    def test_each_workspace_sees_only_its_own_signals(self, bus):
        id_a = bus.emit("ws_src", "ws_a", "ci", "for A")
        id_b = bus.emit("ws_src", "ws_b", "ci", "for B")

        pending_a = bus.read_pending("ws_a")
        pending_b = bus.read_pending("ws_b")

        assert len(pending_a) == 1 and pending_a[0]["id"] == id_a
        assert len(pending_b) == 1 and pending_b[0]["id"] == id_b


# ---------------------------------------------------------------------------
# list_recent()
# ---------------------------------------------------------------------------


class TestListRecent:
    def test_list_recent_includes_sent_and_received(self, bus):
        bus.emit("ws_a", "ws_b", "deploy", "outgoing from A")
        bus.emit("ws_c", "ws_a", "alert", "incoming to A")
        bus.emit("ws_b", "ws_c", "ci", "unrelated")

        recent = bus.list_recent("ws_a")
        ids = {r["id"] for r in recent}
        # Should contain the signal A sent and the signal A received
        assert len([r for r in recent if r["from_workspace"] == "ws_a" or r["to_workspace"] == "ws_a"]) == 2

    def test_list_recent_newest_first(self, bus):
        bus.emit("ws_a", "ws_b", "deploy", "first")
        bus.emit("ws_a", "ws_b", "ci", "second")
        recent = bus.list_recent("ws_b", limit=10)
        assert recent[0]["message"] == "second"
        assert recent[1]["message"] == "first"

    def test_list_recent_respects_limit(self, bus):
        for i in range(5):
            bus.emit("ws_a", "ws_b", "alert", f"msg {i}")
        recent = bus.list_recent("ws_b", limit=3)
        assert len(recent) == 3
