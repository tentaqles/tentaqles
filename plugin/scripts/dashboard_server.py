#!/usr/bin/env python
"""CLI entry point for the Tentaqles real-time dashboard.

Boots sys.path via _path.setup_paths(), discovers workspace roots from
MetaMemory (falling back to the current manifest), picks an available port
in the 8765-8770 range, and starts the HTTP/SSE server. A background
thread publishes a fresh snapshot to all SSE clients every 5 seconds.
"""

from __future__ import annotations

import os
import threading
import time

from _path import setup_paths

setup_paths()


def _discover_workspace_roots() -> list[str]:
    """Prefer MetaMemory rows; fall back to the current manifest's client_root."""
    roots: list[str] = []
    seen: set[str] = set()

    # Try MetaMemory first
    try:
        from tentaqles.memory.meta import MetaMemory
        meta = MetaMemory()
        try:
            for ws in meta.get_all_status():
                # MetaMemory rows don't always carry root_path directly —
                # we stored it via update_workspace so try the DB column.
                row = meta._conn.execute(
                    "SELECT root_path FROM workspace_status WHERE workspace_id=?",
                    (ws.get("workspace_id", ""),),
                ).fetchone()
                rp = row[0] if row and row[0] else None
                if rp and rp not in seen and os.path.isdir(rp):
                    roots.append(rp)
                    seen.add(rp)
        finally:
            meta.close()
    except Exception:
        pass

    if roots:
        return roots

    # Fallback: current manifest
    try:
        from tentaqles.manifest.loader import get_client_context
        ctx = get_client_context(os.getcwd())
        cr = ctx.get("client_root")
        if cr and os.path.isdir(cr) and cr not in seen:
            roots.append(cr)
            seen.add(cr)
    except Exception:
        pass

    # Last resort: current working directory
    if not roots:
        cwd = os.getcwd()
        if os.path.isdir(cwd):
            roots.append(cwd)

    return roots


def _snapshot_publisher_loop(interval: float = 5.0) -> None:
    """Background thread: publish a fresh snapshot every ``interval`` seconds."""
    from tentaqles.dashboard.server import get_workspace_roots
    from tentaqles.dashboard.snapshot import get_dashboard_snapshot
    from tentaqles.dashboard.sse import get_broker

    broker = get_broker()
    while True:
        try:
            snap = get_dashboard_snapshot(get_workspace_roots())
            broker.publish(snap)
        except Exception:
            pass
        time.sleep(interval)


def main() -> None:
    from tentaqles.dashboard.server import run_server, set_workspace_roots

    roots = _discover_workspace_roots()
    set_workspace_roots(roots)
    print(f"Tentaqles dashboard: tracking {len(roots)} workspace(s)", flush=True)
    for r in roots:
        print(f"  - {r}", flush=True)

    # Background publisher
    t = threading.Thread(
        target=_snapshot_publisher_loop, args=(5.0,), daemon=True
    )
    t.start()

    run_server(host="127.0.0.1", port=8765)


if __name__ == "__main__":
    main()
