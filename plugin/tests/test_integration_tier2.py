"""Wave 3 integration tests — wire-up verification for Tier 2 features.

Tests verify that new modules are correctly wired into existing hooks,
store, meta, manifest loader, and the memory-bridge dispatcher.

NOTE: We do NOT dynamically import scripts/*.py as modules because
_path.setup_paths() replaces sys.stdout on Windows (TextIOWrapper wrapping),
which corrupts pytest's stdout capture. Instead we test the underlying
library functions directly.
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from tentaqles.memory.meta import MetaMemory
from tentaqles.memory.signals import SignalBus
from tentaqles.memory.store import MemoryStore


# ---------------------------------------------------------------------------
# Shared fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def tmp_store(tmp_path, monkeypatch):
    """MemoryStore with embedding stubbed out."""
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: b"\x00" * 16)
    s = MemoryStore(tmp_path)
    s.start_session()
    yield s, tmp_path
    s.close()


# ---------------------------------------------------------------------------
# 1. Signals round-trip: emit -> read_pending via SignalBus
# ---------------------------------------------------------------------------


def test_signal_roundtrip_via_signal_bus(tmp_path, monkeypatch):
    """Emit a signal via SignalBus, then read_pending returns it."""
    db_path = tmp_path / "meta.db"
    monkeypatch.setattr("tentaqles.config.meta_db_path", lambda: db_path)

    meta = MetaMemory(db_path=db_path)
    meta.update_workspace("ws_a", "WS A", str(tmp_path), "summary", [])
    meta.update_workspace("ws_b", "WS B", str(tmp_path), "summary", [])
    meta.close()

    bus = SignalBus(db_path)
    sig_id = bus.emit("ws_a", "ws_b", "info", "hello from a")
    assert sig_id

    pending = bus.read_pending("ws_b")
    assert len(pending) == 1
    assert pending[0]["message"] == "hello from a"
    assert pending[0]["from_workspace"] == "ws_a"


# ---------------------------------------------------------------------------
# 2. record_decision_checked returns dict with expected keys
# ---------------------------------------------------------------------------


def test_record_decision_checked_return_shape(tmp_store):
    """record_decision_checked returns dict with id, superseded, contradiction_scores."""
    store, _ = tmp_store
    result = store.record_decision_checked(
        chosen="use pytest",
        rationale="it is the standard",
    )
    assert "id" in result
    assert "superseded" in result
    assert "contradiction_scores" in result
    assert isinstance(result["superseded"], list)
    assert isinstance(result["contradiction_scores"], dict)


# ---------------------------------------------------------------------------
# 3. memory-bridge decision event returns backward-compat decision_id key
#    Tested at the store level (bridge calls record_decision_checked and renames key)
# ---------------------------------------------------------------------------


def test_decision_checked_id_to_decision_id_rename(tmp_store):
    """Simulate bridge's decision handler: record_decision_checked result has id,
    and renaming to decision_id preserves other keys."""
    store, _ = tmp_store
    result = store.record_decision_checked(
        chosen="option A",
        rationale="best fit",
    )
    # bridge does: result["decision_id"] = result.pop("id")
    result["decision_id"] = result.pop("id")
    assert "decision_id" in result
    assert "id" not in result
    assert "superseded" in result
    assert "contradiction_scores" in result


# ---------------------------------------------------------------------------
# 4. session-end: maybe_compact runs without error on fresh store
# ---------------------------------------------------------------------------


def test_session_end_maybe_compact(tmp_store):
    """MemoryConsolidator.maybe_compact runs without raising on a fresh store."""
    from tentaqles.memory.consolidator import MemoryConsolidator
    store, _ = tmp_store
    MemoryConsolidator(store).maybe_compact()


# ---------------------------------------------------------------------------
# 5. session-preamble: semantic facts available via store.get_semantic_facts
# ---------------------------------------------------------------------------


def test_store_get_semantic_facts_returns_facts(tmp_store):
    """store.get_semantic_facts returns the expected fact after recording."""
    store, _ = tmp_store
    store.record_semantic_fact("Python is used for scripting", source_sessions=["s1"])
    facts = store.get_semantic_facts(limit=5)
    assert len(facts) >= 1
    texts = [f["fact"] for f in facts]
    assert "Python is used for scripting" in texts


# ---------------------------------------------------------------------------
# 6. manifest loader includes signals section
# ---------------------------------------------------------------------------


def test_manifest_loader_signals_section(tmp_path):
    """get_client_context includes signals dict with enabled and subscribe_to."""
    manifest_path = tmp_path / ".tentaqles.yaml"
    manifest_path.write_text(
        "schema: tentaqles-client-v1\nclient: acme\ndisplay_name: Acme\n"
        "signals:\n  enabled: true\n  subscribe_to:\n    - ws_b\n",
        encoding="utf-8",
    )

    from tentaqles.manifest.loader import get_client_context
    ctx = get_client_context(str(tmp_path))

    assert "signals" in ctx
    assert ctx["signals"]["enabled"] is True
    assert "ws_b" in ctx["signals"]["subscribe_to"]


def test_manifest_loader_signals_defaults_when_absent(tmp_path):
    """get_client_context returns enabled=False when signals section is absent."""
    manifest_path = tmp_path / ".tentaqles.yaml"
    manifest_path.write_text(
        "schema: tentaqles-client-v1\nclient: acme\ndisplay_name: Acme\n",
        encoding="utf-8",
    )

    from tentaqles.manifest.loader import get_client_context
    ctx = get_client_context(str(tmp_path))

    assert "signals" in ctx
    assert ctx["signals"]["enabled"] is False
    assert ctx["signals"]["subscribe_to"] == []


# ---------------------------------------------------------------------------
# 7. hooks.json is valid JSON and contains snapshot-guard entry
# ---------------------------------------------------------------------------


def test_hooks_json_valid_and_contains_snapshot_guard():
    """hooks.json is valid JSON and includes snapshot-guard in PreToolUse."""
    hooks_path = Path(__file__).parent.parent / "hooks" / "hooks.json"
    with open(hooks_path, encoding="utf-8") as f:
        data = json.load(f)

    pre_tool = data["hooks"]["PreToolUse"]
    matchers = [entry.get("matcher") for entry in pre_tool]
    assert "Write" in matchers

    write_entry = next(e for e in pre_tool if e.get("matcher") == "Write")
    commands = [h["command"] for h in write_entry["hooks"]]
    assert any("snapshot-guard" in cmd for cmd in commands)


# ---------------------------------------------------------------------------
# 8. end_session now sets memory_tier='episodic'
# ---------------------------------------------------------------------------


def test_end_session_sets_memory_tier_episodic(tmp_store):
    """end_session sets memory_tier='episodic' on the closed session row."""
    store, _ = tmp_store
    store.end_session("finished work")
    row = store._conn.execute(
        "SELECT memory_tier FROM sessions WHERE ended_at IS NOT NULL "
        "ORDER BY ended_at DESC LIMIT 1"
    ).fetchone()
    assert row is not None
    assert row[0] == "episodic"


# ---------------------------------------------------------------------------
# 9. MetaMemory.get_pending_signals delegates to SignalBus
# ---------------------------------------------------------------------------


def test_meta_get_pending_signals(tmp_path, monkeypatch):
    """MetaMemory.get_pending_signals returns signals emitted to a workspace."""
    db_path = tmp_path / "meta.db"
    monkeypatch.setattr("tentaqles.config.meta_db_path", lambda: db_path)

    meta = MetaMemory(db_path=db_path)
    meta.update_workspace("sender", "Sender", str(tmp_path), "s", [])
    meta.update_workspace("receiver", "Receiver", str(tmp_path), "r", [])
    meta.close()

    bus = SignalBus(db_path)
    bus.emit("sender", "receiver", "ping", "test message")

    meta2 = MetaMemory(db_path=db_path)
    signals = meta2.get_pending_signals("receiver")
    meta2.close()

    assert len(signals) == 1
    assert signals[0]["message"] == "test message"
