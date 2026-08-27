"""Tentaqles real-time dashboard package.

Exports:
    run_server             -- HTTP/SSE server entry point
    get_dashboard_snapshot -- build a point-in-time snapshot across workspaces
"""

from __future__ import annotations

from tentaqles.dashboard.server import run_server
from tentaqles.dashboard.snapshot import get_dashboard_snapshot

__all__ = ["run_server", "get_dashboard_snapshot"]
