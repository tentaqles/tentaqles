"""Time-Travel Snapshot manager for Tentaqles workspaces.

Snapshots are JSON files stored at:
  {workspace}/.claude/snapshots/{utc_iso}.json

They capture the full manifest state before identity switches or manifest
edits, enabling rollback via the /tentaqles-rollback skill.
"""

from __future__ import annotations

import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


class SnapshotManager:
    """Manages workspace snapshots.

    Args:
        workspace_path: Path to the client workspace root (directory that
            contains .tentaqles.yaml and .claude/).
        max_snapshots: Maximum snapshots to retain. Older ones are pruned
            after each ``capture`` call.
    """

    def __init__(self, workspace_path: str | Path, max_snapshots: int = 30) -> None:
        self.workspace_path = Path(workspace_path)
        self.max_snapshots = max_snapshots
        self._snapshots_dir = self.workspace_path / ".claude" / "snapshots"

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def capture(
        self,
        reason: str,
        manifest_data: dict,
        context_data: dict | None = None,
    ) -> Path:
        """Serialize current workspace state to a new snapshot file.

        After writing, calls ``prune(self.max_snapshots)`` to enforce the
        retention limit.

        Args:
            reason: Human-readable reason for the snapshot (e.g.
                "auto-switch", "manifest-edit", "manual").
            manifest_data: The full ``.tentaqles.yaml`` content as a dict.
            context_data: Optional extra context (memory stats, etc.).

        Returns:
            Path to the written snapshot file.
        """
        self._snapshots_dir.mkdir(parents=True, exist_ok=True)

        timestamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H-%M-%S-%f")
        snapshot_path = self._snapshots_dir / f"{timestamp}.json"

        # Compute deterministic hash over the manifest content
        manifest_bytes = json.dumps(manifest_data, sort_keys=True).encode()
        manifest_hash = hashlib.sha256(manifest_bytes).hexdigest()

        payload: dict[str, Any] = {
            "schema": "tentaqles-snapshot-v1",
            "captured_at": datetime.now(timezone.utc).isoformat(),
            "reason": reason,
            "workspace": self.workspace_path.name,
            "manifest": manifest_data,
            "manifest_hash": manifest_hash,
        }
        if context_data:
            payload["context"] = context_data

        snapshot_path.write_text(
            json.dumps(payload, indent=2, ensure_ascii=False), encoding="utf-8"
        )

        self.prune(self.max_snapshots)
        return snapshot_path

    def list_snapshots(self) -> list[dict]:
        """Return snapshot metadata sorted newest-first.

        Returns:
            List of dicts with keys: ``path``, ``captured_at``, ``reason``,
            ``manifest_hash``.
        """
        if not self._snapshots_dir.exists():
            return []

        results: list[dict] = []
        for fp in sorted(self._snapshots_dir.glob("*.json"), reverse=True):
            try:
                data = json.loads(fp.read_text(encoding="utf-8"))
                results.append(
                    {
                        "path": str(fp),
                        "captured_at": data.get("captured_at", fp.stem),
                        "reason": data.get("reason", "unknown"),
                        "manifest_hash": data.get("manifest_hash", ""),
                    }
                )
            except (json.JSONDecodeError, OSError):
                continue

        return results

    def restore(self, identifier: str) -> dict:
        """Load a snapshot and return its manifest dict.

        Does NOT write anything to disk — the caller is responsible for
        writing the manifest back to ``.tentaqles.yaml``.

        Args:
            identifier: ISO timestamp prefix (e.g. ``"2026-04-12T14-30"``) or
                the full filename stem. The first file whose name starts with
                this prefix is used.

        Returns:
            The ``manifest`` dict from the snapshot.

        Raises:
            FileNotFoundError: If no matching snapshot is found.
            ValueError: If the snapshot file is malformed.
        """
        match = self._find_snapshot(identifier)
        if match is None:
            raise FileNotFoundError(
                f"No snapshot matching {identifier!r} in {self._snapshots_dir}"
            )
        try:
            data = json.loads(match.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError) as exc:
            raise ValueError(f"Could not read snapshot {match}: {exc}") from exc

        if "manifest" not in data:
            raise ValueError(f"Snapshot {match} has no 'manifest' key")
        return data["manifest"]

    def prune(self, keep_last: int = 30) -> int:
        """Delete oldest snapshots beyond ``keep_last``.

        Args:
            keep_last: Number of most-recent snapshots to retain.

        Returns:
            Number of snapshots deleted.
        """
        if not self._snapshots_dir.exists():
            return 0

        files = sorted(self._snapshots_dir.glob("*.json"), reverse=True)
        to_delete = files[keep_last:]
        for fp in to_delete:
            try:
                fp.unlink()
            except OSError:
                pass
        return len(to_delete)

    def diff(self, ts1: str, ts2: str) -> dict:
        """Return the diff between two snapshots' manifest dicts.

        Args:
            ts1: Timestamp prefix for the first (older) snapshot.
            ts2: Timestamp prefix for the second (newer) snapshot.

        Returns:
            Dict with keys ``added``, ``removed``, ``changed``:
            - ``added``: keys present in ts2 but not ts1
            - ``removed``: keys present in ts1 but not ts2
            - ``changed``: ``{key: (old_value, new_value)}`` for keys that exist
              in both but differ
        """
        m1 = self.restore(ts1)
        m2 = self.restore(ts2)
        return _dict_diff(m1, m2)

    # ------------------------------------------------------------------
    # Private helpers
    # ------------------------------------------------------------------

    def _find_snapshot(self, identifier: str) -> Path | None:
        """Return the first snapshot file whose stem starts with identifier."""
        if not self._snapshots_dir.exists():
            return None
        # Exact file match first
        exact = self._snapshots_dir / identifier
        if exact.is_file():
            return exact
        if not identifier.endswith(".json"):
            exact_json = self._snapshots_dir / f"{identifier}.json"
            if exact_json.is_file():
                return exact_json
        # Prefix match over sorted files (newest-first)
        for fp in sorted(self._snapshots_dir.glob("*.json"), reverse=True):
            if fp.stem.startswith(identifier) or fp.name.startswith(identifier):
                return fp
        return None


def _dict_diff(old: dict, new: dict) -> dict:
    """Shallow diff between two dicts."""
    old_keys = set(old)
    new_keys = set(new)
    added = {k: new[k] for k in new_keys - old_keys}
    removed = {k: old[k] for k in old_keys - new_keys}
    changed = {
        k: (old[k], new[k])
        for k in old_keys & new_keys
        if old[k] != new[k]
    }
    return {"added": added, "removed": removed, "changed": changed}
