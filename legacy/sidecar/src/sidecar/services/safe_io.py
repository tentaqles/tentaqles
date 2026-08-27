from __future__ import annotations

import json
import os
import shutil
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path

# ---------------------------------------------------------------------------
# atomic_write
# ---------------------------------------------------------------------------


def atomic_write(path: str | Path, content: str, encoding: str = "utf-8") -> None:
    """Write *content* to *path* atomically via a temp file in the same directory.

    Parent directories are created if they do not exist.
    The temp file is cleaned up on failure.
    """
    target = Path(path)
    target.parent.mkdir(parents=True, exist_ok=True)

    fd, tmp_path = tempfile.mkstemp(dir=target.parent)
    try:
        with os.fdopen(fd, "w", encoding=encoding) as fh:
            fh.write(content)
            fh.flush()
            os.fsync(fh.fileno())
        os.replace(tmp_path, target)
    except Exception:
        try:
            os.unlink(tmp_path)
        except OSError:
            pass
        raise


# ---------------------------------------------------------------------------
# BackupManager
# ---------------------------------------------------------------------------


@dataclass
class BackupInfo:
    backup_id: str
    label: str
    timestamp: int  # nanoseconds
    files: dict[str, str]  # original_path -> backup_filename
    absent: list[str]  # original paths that did not exist at backup time


class BackupManager:
    """Manages file backups in *backup_dir*."""

    def __init__(self, backup_dir: str | Path) -> None:
        self.backup_dir = Path(backup_dir)

    # ------------------------------------------------------------------
    def create(self, label: str, files: list[str | Path]) -> str:
        """Snapshot *files* and return the backup_id."""
        ts = time.time_ns()
        backup_id = f"backup-{ts}"
        backup_path = self.backup_dir / backup_id
        backup_path.mkdir(parents=True, exist_ok=True)

        file_map: dict[str, str] = {}
        absent: list[str] = []

        for i, fp in enumerate(files):
            src = Path(fp)
            backup_name = f"file_{i:04d}"
            if src.exists():
                shutil.copy2(src, backup_path / backup_name)
                file_map[str(src)] = backup_name
            else:
                absent.append(str(src))

        manifest = {
            "label": label,
            "timestamp": ts,
            "files": file_map,
            "absent": absent,
        }
        atomic_write(backup_path / "manifest.json", json.dumps(manifest, indent=2))
        return backup_id

    # ------------------------------------------------------------------
    def restore(self, backup_id: str) -> None:
        """Restore files from a backup.  Files listed in *absent* are deleted."""
        backup_path = self.backup_dir / backup_id
        manifest_path = backup_path / "manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

        for original, backup_name in manifest["files"].items():
            src = backup_path / backup_name
            atomic_write(original, src.read_text(encoding="utf-8"))

        for original in manifest.get("absent", []):
            p = Path(original)
            if p.exists():
                p.unlink()

    # ------------------------------------------------------------------
    def prune(self, keep: int = 10) -> None:
        """Delete oldest backups, keeping the newest *keep*."""
        backups = self.list_backups()  # already sorted newest-first
        for old in backups[keep:]:
            shutil.rmtree(self.backup_dir / old.backup_id, ignore_errors=True)

    # ------------------------------------------------------------------
    def list_backups(self) -> list[BackupInfo]:
        """Return all backups sorted newest first."""
        if not self.backup_dir.exists():
            return []

        result: list[BackupInfo] = []
        for entry in self.backup_dir.iterdir():
            if not entry.is_dir():
                continue
            manifest_path = entry / "manifest.json"
            if not manifest_path.exists():
                continue
            try:
                data = json.loads(manifest_path.read_text(encoding="utf-8"))
                result.append(
                    BackupInfo(
                        backup_id=entry.name,
                        label=data["label"],
                        timestamp=data["timestamp"],
                        files=data["files"],
                        absent=data.get("absent", []),
                    )
                )
            except (KeyError, json.JSONDecodeError):
                continue

        result.sort(key=lambda b: b.timestamp, reverse=True)
        return result


# ---------------------------------------------------------------------------
# FileTransaction
# ---------------------------------------------------------------------------

def _default_backup_dir() -> Path:
    """Resolve the default backup directory at call time (not import time)."""
    return Path.home() / ".tentaqles" / "backups"


class FileTransaction:
    """Context manager for multi-file writes with automatic rollback on error.

    Usage::

        with FileTransaction("my-label") as tx:
            tx.write("/some/file.txt", "new content")
            tx.delete("/some/other.txt")
        # On clean exit: backup → execute writes/deletes
        # On exception from user code: nothing is written

    If any write/delete fails *during execution* (after backup), the backup is
    automatically restored.
    """

    def __init__(self, label: str, backup_dir: str | Path | None = None) -> None:
        self.label = label
        self._backup_dir = Path(backup_dir) if backup_dir is not None else _default_backup_dir()
        self._writes: list[tuple[Path, str]] = []
        self._deletes: list[Path] = []
        self._backup_id: str | None = None

    # ------------------------------------------------------------------
    @property
    def backup_id(self) -> str | None:
        return self._backup_id

    # ------------------------------------------------------------------
    def write(self, path: str | Path, content: str) -> None:
        """Queue a file write."""
        self._writes.append((Path(path), content))

    def delete(self, path: str | Path) -> None:
        """Queue a file deletion."""
        self._deletes.append(Path(path))

    # ------------------------------------------------------------------
    def __enter__(self) -> FileTransaction:
        return self

    def __exit__(self, exc_type, exc_val, exc_tb) -> bool:  # noqa: ANN001
        if exc_type is not None:
            # Exception from user code — do nothing, let exception propagate
            return False

        # Gather all target paths for backup
        all_targets = [str(p) for p, _ in self._writes] + [str(p) for p in self._deletes]

        mgr = BackupManager(self._backup_dir)
        self._backup_id = mgr.create(self.label, all_targets)

        try:
            for target_path, content in self._writes:
                atomic_write(target_path, content)
            for target_path in self._deletes:
                if target_path.exists():
                    target_path.unlink()
        except Exception:
            # Restore from backup, then re-raise
            try:
                mgr.restore(self._backup_id)
            except Exception as restore_err:
                raise RuntimeError(f"Transaction failed AND restore failed: {restore_err}") from restore_err
            raise

        return False
