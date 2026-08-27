"""Analytics router — aggregate usage stats across workspaces."""

from __future__ import annotations

import dataclasses
from collections import defaultdict

from fastapi import APIRouter

from sidecar.schemas import ScanRequest
from sidecar.services.hierarchy import scan_hierarchy
from sidecar.services.session_history import SessionEntry, discover_sessions

router = APIRouter()


@router.post("/overview")
def overview(req: ScanRequest):
    """Get aggregate analytics across all workspaces."""
    tree = scan_hierarchy(req.base_path)

    total_sessions = 0
    total_cost = 0.0
    total_messages = 0
    model_usage: dict[str, int] = defaultdict(int)
    client_stats: list[dict] = []

    for client in tree.clients:
        client_sessions = 0
        client_cost = 0.0
        client_messages = 0

        # Gather sessions from client path
        paths_to_scan = [client.path]
        for project in client.projects:
            paths_to_scan.append(project.path)

        for path in paths_to_scan:
            try:
                sessions = discover_sessions(path, limit=50)
                for s in sessions:
                    client_sessions += 1
                    client_cost += s.cost_usd
                    client_messages += s.message_count
                    if s.model:
                        model_usage[s.model] += 1
            except Exception:
                pass

        total_sessions += client_sessions
        total_cost += client_cost
        total_messages += client_messages

        client_stats.append(
            {
                "name": client.name,
                "project_count": len(client.projects),
                "session_count": client_sessions,
                "total_cost": round(client_cost, 4),
                "message_count": client_messages,
            }
        )

    return {
        "total_sessions": total_sessions,
        "total_cost": round(total_cost, 4),
        "total_messages": total_messages,
        "client_count": len(tree.clients),
        "project_count": sum(len(c.projects) for c in tree.clients),
        "model_usage": dict(model_usage),
        "client_stats": client_stats,
    }
