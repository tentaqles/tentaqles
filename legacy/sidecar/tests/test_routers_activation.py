"""Basic router integration tests for activation and health-check endpoints."""

from __future__ import annotations

import json

from fastapi.testclient import TestClient


def _get_test_client():
    from sidecar.main import app

    return TestClient(app)


def test_activation_active_returns_null_when_no_workspace(tmp_path, monkeypatch):
    """GET /api/activation/active returns null when no workspace is active."""
    # Use a fresh state dir so tests don't read the real ~/.tentaqles state
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setattr("pathlib.Path.home", lambda: tmp_path)

    client = _get_test_client()
    response = client.get("/api/activation/active")
    assert response.status_code == 200
    assert response.json() is None


def test_health_check_endpoint(tmp_path):
    """POST /api/health-check/workspace returns health report for a minimal workspace."""
    ws = tmp_path / "client"
    ws.mkdir()
    (ws / ".workspace-profile.json").write_text(
        json.dumps(
            {
                "$schema": "workspace-profile-v1",
                "client_name": "test",
                "git": {"platform": "github", "host": "github.com", "account": "t"},
                "cloud": {"provider": "none"},
                "database": {"type": "none"},
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )

    client = _get_test_client()
    response = client.post("/api/health-check/workspace", json={"workspace_path": str(ws)})
    assert response.status_code == 200
    data = response.json()
    assert data["client_name"] == "test"
    assert data["overall"] in ("healthy", "degraded", "broken")
