"""Archive service — soft-archive projects by moving to .archived/ directory."""

import json
import shutil
from datetime import datetime, timezone
from pathlib import Path

ARCHIVE_DIR = ".archived"
ARCHIVE_META = ".archive-meta.json"


def archive_project(project_path: str, client_path: str) -> dict:
    """Move a project to the client's .archived/ directory."""
    project = Path(project_path)
    client = Path(client_path)

    if not project.exists():
        return {"error": f"Project not found: {project_path}"}

    archive_dir = client / ARCHIVE_DIR
    archive_dir.mkdir(exist_ok=True)

    dest = archive_dir / project.name
    if dest.exists():
        return {"error": f"Already archived: {project.name}"}

    # Save metadata
    meta = {
        "original_path": str(project),
        "archived_at": datetime.now(timezone.utc).isoformat(),
        "project_name": project.name,
    }

    shutil.move(str(project), str(dest))

    meta_file = dest / ARCHIVE_META
    meta_file.write_text(json.dumps(meta, indent=2))

    return {"ok": True, "archived_path": str(dest), **meta}


def restore_project(project_path: str, client_path: str) -> dict:
    """Restore an archived project back to its original location."""
    client = Path(client_path)
    archive_dir = client / ARCHIVE_DIR

    # project_path could be the archived path or just the project name
    archived = Path(project_path)
    if not archived.exists():
        archived = archive_dir / Path(project_path).name

    if not archived.exists():
        return {"error": f"Archived project not found: {project_path}"}

    # Read metadata for original path
    meta_file = archived / ARCHIVE_META
    if meta_file.exists():
        meta = json.loads(meta_file.read_text())
        original_path = Path(meta.get("original_path", str(client / archived.name)))
        meta_file.unlink()  # Remove meta before moving
    else:
        original_path = client / archived.name

    if original_path.exists():
        return {"error": f"Cannot restore — path already exists: {original_path}"}

    shutil.move(str(archived), str(original_path))

    return {"ok": True, "restored_path": str(original_path)}


def list_archived(client_path: str) -> list[dict]:
    """List all archived projects for a client."""
    client = Path(client_path)
    archive_dir = client / ARCHIVE_DIR

    if not archive_dir.exists():
        return []

    results = []
    for entry in sorted(archive_dir.iterdir()):
        if entry.is_dir() and entry.name != "__pycache__":
            meta_file = entry / ARCHIVE_META
            meta = {}
            if meta_file.exists():
                try:
                    meta = json.loads(meta_file.read_text())
                except Exception:
                    pass

            results.append({
                "name": entry.name,
                "path": str(entry),
                "archived_at": meta.get("archived_at", "unknown"),
                "original_path": meta.get("original_path", ""),
            })

    return results
