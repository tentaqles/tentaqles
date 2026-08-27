"""Dashboard snapshot builder.

Given a list of workspace root paths, produces the JSON-serializable
structure consumed by the dashboard UI. All user-facing text is routed
through ``redact_text`` before leaving the client boundary.
"""

from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path
from typing import Any

try:
    from tentaqles.privacy import redact_text
except Exception:  # pragma: no cover - graceful degradation
    def redact_text(text, *args, **kwargs):  # type: ignore[misc]
        return (text or "", [])


def _redact(text: Any) -> Any:
    """Apply redact_text to a string, returning the original on error."""
    if text is None:
        return None
    if not isinstance(text, str):
        return text
    try:
        redacted, _ = redact_text(text)
        return redacted
    except Exception:
        return text


def _iso_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _workspace_id_from_root(root_path: str) -> str:
    """Derive a stable workspace id from a root path."""
    try:
        return Path(root_path).resolve().name or root_path
    except Exception:
        return root_path


def _workspace_snapshot(
    root_path: str, max_nodes: int
) -> dict[str, Any]:
    """Build a single workspace entry. Never raises — returns error marker."""
    entry: dict[str, Any] = {
        "id": _workspace_id_from_root(root_path),
        "display_name": _workspace_id_from_root(root_path),
        "root_path": str(root_path),
        "last_active": None,
        "stats": {
            "sessions": 0,
            "touches": 0,
            "active_decisions": 0,
            "open_pending": 0,
        },
        "hot_nodes": [],
        "open_pending": [],
    }

    try:
        from tentaqles.memory.store import MemoryStore
    except Exception:
        return entry

    db_path = Path(root_path) / ".claude" / "memory.db"
    if not db_path.exists():
        return entry

    store: MemoryStore | None = None
    try:
        store = MemoryStore(root_path)

        # Stats
        try:
            stats = store.stats()
            entry["stats"] = {
                "sessions": int(stats.get("sessions", 0) or 0),
                "touches": int(stats.get("touches", 0) or 0),
                "active_decisions": int(stats.get("active_decisions", 0) or 0),
                "open_pending": int(stats.get("open_pending", 0) or 0),
            }
        except Exception:
            pass

        # Last active session
        try:
            last = store.get_last_session()
            if last:
                entry["last_active"] = last.get("ended_at") or last.get(
                    "started_at"
                )
                last_summary = last.get("summary")
                if last_summary:
                    entry["last_summary"] = _redact(last_summary)
        except Exception:
            pass

        # Hot nodes
        try:
            nodes = store.get_active_nodes(limit=max_nodes) or []
            hot = []
            for n in nodes[:max_nodes]:
                trend = n.get("trend", "stable")
                hot.append(
                    {
                        "node_id": _redact(n.get("node_id", "")),
                        "score": float(n.get("activity_score", 0.0) or 0.0),
                        "trend": trend,
                    }
                )
            entry["hot_nodes"] = hot
        except Exception:
            entry["hot_nodes"] = []

        # Open pending (top 10)
        try:
            pending = store.get_open_pending() or []
            open_pending = []
            for p in pending[:10]:
                open_pending.append(
                    {
                        "description": _redact(p.get("description", "")),
                        "priority": p.get("priority", "medium"),
                    }
                )
            entry["open_pending"] = open_pending
        except Exception:
            entry["open_pending"] = []

    except Exception:
        # Any unexpected error: keep the stub entry.
        pass
    finally:
        if store is not None:
            try:
                store.close()
            except Exception:
                pass

    return entry


def get_dashboard_snapshot(
    workspace_roots: list[str],
    max_nodes_per_workspace: int = 5,
) -> dict[str, Any]:
    """Build the full dashboard snapshot across all workspaces.

    Parameters
    ----------
    workspace_roots:
        List of workspace root paths (strings or Paths) to query.
    max_nodes_per_workspace:
        Max hot nodes to include per workspace.
    """
    workspaces = []
    for root in workspace_roots or []:
        try:
            workspaces.append(
                _workspace_snapshot(str(root), max_nodes_per_workspace)
            )
        except Exception:
            continue

    return {
        "generated_at": _iso_now(),
        "workspaces": workspaces,
    }
