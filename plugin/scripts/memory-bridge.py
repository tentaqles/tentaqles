#!/usr/bin/env python3
"""Tentaqles memory bridge — wires hook events into the SQLite MemoryStore.

Called from skills or standalone. Accepts JSON on stdin:
    {"cwd": "...", "event": "touch|decision|session_start|session_end|pending|context|skill_correction", "data": {...}}

Exits silently on any error to avoid breaking the hook chain.
"""

from __future__ import annotations

import os
import sys

# Bootstrap sys.path for plugin imports (tentaqles.* + bootstrapped deps)
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _path import setup_paths
setup_paths()

import json
from pathlib import Path


# Also add plugin data dir (where bootstrap installs extra deps)
plugin_data = os.environ.get("CLAUDE_PLUGIN_DATA", "")
if plugin_data:
    sys.path.insert(0, os.path.join(plugin_data, "lib"))


try:
    from tentaqles.privacy import redact_text

    def _redact(text):
        if text is None:
            return None
        try:
            return redact_text(str(text))[0]
        except Exception:
            return text
except Exception:
    def _redact(text):
        return text


def main() -> None:
    try:
        raw = sys.stdin.read()
    except Exception:
        return

    if not raw.strip():
        return

    try:
        payload = json.loads(raw)
    except (json.JSONDecodeError, ValueError):
        print(json.dumps({"error": "invalid JSON input"}), file=sys.stderr)
        return

    cwd = payload.get("cwd", ".")
    event = payload.get("event", "")
    data = payload.get("data", {})

    if not event:
        print(json.dumps({"error": "missing event field"}), file=sys.stderr)
        return

    manifest = None
    client_root = cwd
    client_name = "unknown"
    try:
        from tentaqles.manifest.loader import load_manifest
        manifest = load_manifest(cwd)
        if manifest:
            client_root = manifest.get("_client_root", cwd)
            client_name = manifest.get("client", "unknown")
    except Exception:
        pass

    try:
        from tentaqles.memory.store import MemoryStore
        store = MemoryStore(client_root)
    except Exception as exc:
        print(json.dumps({"error": f"failed to init MemoryStore: {exc}"}), file=sys.stderr)
        return

    try:
        _dispatch(event, data, store, manifest, client_root, client_name)
    except Exception as exc:
        print(json.dumps({"error": f"event handler failed: {exc}"}), file=sys.stderr)
    finally:
        try:
            store.close()
        except Exception:
            pass


def _dispatch(
    event: str,
    data: dict,
    store,
    manifest: dict | None,
    client_root: str,
    client_name: str,
) -> None:
    """Route event to the appropriate MemoryStore method."""

    if event == "session_start":
        sid = store.start_session(
            tags=data.get("tags"),
            metadata={"client": client_name},
        )
        print(json.dumps({"session_id": sid}))

    elif event == "session_end":
        summary = _redact(data.get("summary", ""))
        tags = data.get("tags")

        try:
            result = store.end_session(summary, tags=tags)
        except Exception as exc:
            result = {"error": f"end_session failed: {exc}"}

        try:
            from tentaqles.memory.meta import MetaMemory
            meta = MetaMemory()
            active = store.get_active_nodes(limit=10)
            stats = store.stats()
            display = (
                manifest.get("display_name", client_name) if manifest else client_name
            )
            meta.update_workspace(
                client_name,
                display,
                str(client_root),
                summary,
                [n["node_id"] for n in active],
                session_count=stats.get("sessions", 0),
                total_touches=stats.get("touches", 0),
            )
            meta.close()
        except Exception:
            pass

        print(json.dumps(result))

    elif event == "touch":
        node_id = data.get("node_id", "unknown")
        store.touch(
            node_id,
            data.get("node_type", "file"),
            data.get("action", "edit"),
            data.get("weight", 1.0),
        )
        print(json.dumps({"touch_id": node_id}))

    elif event == "decision":
        try:
            result = store.record_decision_checked(
                chosen=_redact(data.get("chosen", "")),
                rationale=_redact(data.get("rationale", "")),
                node_ids=data.get("node_ids"),
                rejected=[_redact(r) for r in (data.get("rejected") or [])],
                confidence=data.get("confidence", "medium"),
                tags=data.get("tags"),
            )
            result["decision_id"] = result.pop("id")  # backward compat key
            print(json.dumps(result))
        except Exception as exc:
            print(
                json.dumps({"error": f"record_decision failed: {exc}"}),
                file=sys.stderr,
            )

    elif event == "signal":
        from tentaqles.memory.signals import SignalBus
        bus = SignalBus()
        sig_id = bus.emit(
            from_workspace=data["from"],
            to_workspace=data["to"],
            event_type=data.get("type", "custom"),
            message=data.get("message", ""),
            payload=data.get("payload"),
            ttl_hours=float(data.get("ttl_hours", 48.0)),
        )
        print(json.dumps({"signal_id": sig_id}))

    elif event == "read_signals":
        from tentaqles.memory.signals import SignalBus
        print(json.dumps({"signals": SignalBus().read_pending(data["workspace_id"])}))

    elif event == "pending":
        pid = store.add_pending(
            description=_redact(data.get("description", "")),
            node_ids=data.get("node_ids"),
            priority=data.get("priority", "medium"),
        )
        print(json.dumps({"pending_id": pid}))

    elif event == "context":
        summary = store.get_context_summary()
        print(summary)

    elif event == "skill_correction":
        # Self-improving skills: record a user correction against a SKILL.md file
        skill_name = data.get("skill_name", "")
        correction = _redact(data.get("correction", ""))
        if not skill_name or not correction:
            print(json.dumps({"error": "skill_correction requires skill_name and correction"}), file=sys.stderr)
            return

        plugin_root = os.environ.get("CLAUDE_PLUGIN_ROOT", "")
        if not plugin_root:
            # Fall back: resolve from this script's location
            plugin_root = str(Path(__file__).parent.parent)

        try:
            from tentaqles.skills import record_skill_correction
            result = record_skill_correction(
                skill_name=skill_name,
                correction=correction,
                plugin_root=plugin_root,
                client_root=client_root,
            )
            print(json.dumps(result))
        except Exception as exc:
            print(json.dumps({"error": f"skill_correction failed: {exc}"}), file=sys.stderr)

    else:
        print(
            json.dumps({"error": f"unknown event: {event}"}),
            file=sys.stderr,
        )


if __name__ == "__main__":
    main()
