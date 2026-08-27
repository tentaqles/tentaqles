"""Tests for the tentaqles.dashboard package."""

from __future__ import annotations

import queue
import re

import pytest

from tentaqles.dashboard.html import DASHBOARD_HTML
from tentaqles.dashboard.snapshot import get_dashboard_snapshot
from tentaqles.dashboard.sse import SSEBroker
from tentaqles.memory.store import MemoryStore


# ---------------------------------------------------------------------------
# SSEBroker
# ---------------------------------------------------------------------------


def test_sse_broker_subscribe_publish():
    broker = SSEBroker()
    q = broker.subscribe()
    broker.publish({"type": "test", "value": 42})
    event = q.get(timeout=1.0)
    assert event == {"type": "test", "value": 42}


def test_sse_broker_unsubscribe():
    broker = SSEBroker()
    q = broker.subscribe()
    broker.unsubscribe(q)
    broker.publish({"type": "test"})
    with pytest.raises(queue.Empty):
        q.get(timeout=0.1)


# ---------------------------------------------------------------------------
# Snapshot
# ---------------------------------------------------------------------------


def test_snapshot_structure_empty():
    snap = get_dashboard_snapshot([])
    assert isinstance(snap, dict)
    assert "generated_at" in snap
    assert snap["workspaces"] == []


def test_snapshot_with_mock_data(tmp_path, monkeypatch):
    # Avoid loading the real embedding model.
    monkeypatch.setattr(
        MemoryStore, "_embed", lambda self, text: b"\x00" * 4
    )
    store = MemoryStore(tmp_path)
    store.start_session()
    store.touch(node_id="src/foo.py", node_type="file", action="edit")
    store.touch(node_id="src/bar.py", node_type="file", action="read")
    store.add_pending("ship the thing", priority="high")
    store.end_session(summary="did some stuff")
    store.close()

    snap = get_dashboard_snapshot([str(tmp_path)])
    assert isinstance(snap, dict)
    assert len(snap["workspaces"]) == 1
    ws = snap["workspaces"][0]
    assert ws["root_path"] == str(tmp_path)
    assert ws["stats"]["touches"] >= 2
    assert ws["stats"]["open_pending"] >= 1
    assert isinstance(ws["hot_nodes"], list)
    assert isinstance(ws["open_pending"], list)
    assert any("ship the thing" in p["description"] for p in ws["open_pending"])


# ---------------------------------------------------------------------------
# HTML constant
# ---------------------------------------------------------------------------


def test_html_constant_has_title():
    assert "Tentaqles" in DASHBOARD_HTML


def test_html_constant_no_external_urls():
    # Allow only localhost http/https URLs.
    for match in re.finditer(r"https?://([^\s\"'<>)]+)", DASHBOARD_HTML):
        host = match.group(1)
        assert host.startswith("localhost") or host.startswith("127.0.0.1"), (
            f"external URL found in DASHBOARD_HTML: {match.group(0)}"
        )
