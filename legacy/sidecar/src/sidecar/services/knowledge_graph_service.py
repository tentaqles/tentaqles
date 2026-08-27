"""Knowledge graph service — workspace file scanner, link parser, and file reader."""

from __future__ import annotations

import base64
import os
import re
import time
from datetime import UTC, datetime
from pathlib import Path

# ------------------------------------------------------------------
# Constants
# ------------------------------------------------------------------

SKIP_DIRS: set[str] = {
    ".git",
    "node_modules",
    ".venv",
    "venv",
    "__pycache__",
    ".next",
    "out",
    "dist",
    "resources",
    ".superpowers",
    ".claude",
    ".tentaqles",
}

# Directories whose files are config, not knowledge content
SKIP_NAMES: set[str] = {
    "commands", "rules", "shared-memory", "skills",
    "worktrees", "hooks", "permissions", "patterns",
}

MAX_DEPTH = 15
MAX_FILE_SIZE = 1_048_576  # 1 MB

EXT_TYPE_MAP: dict[str, str] = {}
for _ext in (".md", ".mdx"):
    EXT_TYPE_MAP[_ext] = "markdown"
for _ext in (
    ".py", ".ts", ".tsx", ".js", ".jsx", ".rs", ".go", ".java",
    ".c", ".cpp", ".h", ".css", ".html", ".json", ".yaml", ".yml",
    ".toml", ".sql", ".sh", ".ps1",
):
    EXT_TYPE_MAP[_ext] = "code"
for _ext in (".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico"):
    EXT_TYPE_MAP[_ext] = "image"
EXT_TYPE_MAP[".pdf"] = "pdf"

# Wiki-link: [[target]] or [[target|label]]
_WIKI_LINK_RE = re.compile(r"\[\[([^\]]+)\]\]")
# Markdown link: [label](target) — we filter out http/https, #, mailto later
_MD_LINK_RE = re.compile(r"\[([^\]]*)\]\(([^)]+)\)")

# ------------------------------------------------------------------
# Cache
# ------------------------------------------------------------------

_cache: dict[str, tuple[dict, float, float]] = {}  # client_path -> (result, newest_mtime, cached_at)
_CACHE_TTL = 30  # seconds


def _file_type(ext: str) -> str:
    return EXT_TYPE_MAP.get(ext.lower(), "other")


def _should_skip(dir_name: str, rel_parts: tuple[str, ...]) -> bool:
    """Check if a directory should be skipped during scanning."""
    if dir_name.startswith("."):
        return True  # skip all hidden/dot directories
    if dir_name in SKIP_DIRS or dir_name in SKIP_NAMES:
        return True
    return False


# ------------------------------------------------------------------
# Link parsing
# ------------------------------------------------------------------

def _resolve_wiki_link(
    target: str, file_dir: Path, workspace_root: Path, md_stems: dict[str, list[Path]]
) -> str | None:
    """Resolve a wiki-link target to a relative path.

    md_stems values are relative Paths. file_dir is absolute.
    """
    stem_lower = target.lower().strip()
    candidates = md_stems.get(stem_lower)
    if not candidates:
        return None

    # Convert file_dir to relative for comparison
    try:
        file_dir_rel = file_dir.relative_to(workspace_root)
    except ValueError:
        file_dir_rel = file_dir

    # Prefer same directory
    for c in candidates:
        if c.parent == file_dir_rel:
            return c.as_posix()

    # Pick first match (closest by path depth)
    return candidates[0].as_posix()


def _parse_links(
    content: str,
    file_path: Path,
    workspace_root: Path,
    md_stems: dict[str, list[Path]],
) -> list[dict]:
    """Parse wiki-links and markdown links from file content.

    file_path and workspace_root are absolute Paths.
    Returns list of {"target": relative_path, "label": link_text}.
    """
    file_dir = file_path.parent
    links: list[dict] = []

    # Wiki-links: [[target]] or [[target|label]]
    for m in _WIKI_LINK_RE.finditer(content):
        raw = m.group(1)
        if "|" in raw:
            target_part, label = raw.split("|", 1)
        else:
            target_part, label = raw, raw
        resolved = _resolve_wiki_link(target_part.strip(), file_dir, workspace_root, md_stems)
        if resolved:
            links.append({"target": resolved, "label": label.strip()})

    # Markdown links: [label](target) — filter out external/anchor/mailto
    for m in _MD_LINK_RE.finditer(content):
        label = m.group(1)
        target = m.group(2).strip()
        # Strip anchor fragments from target (e.g., "file.md#section" → "file.md")
        if "#" in target:
            target = target.split("#")[0]
        if not target or target.startswith(("http://", "https://", "mailto:")):
            continue
        # Resolve relative to current file's directory
        try:
            resolved_path = (file_dir / target).resolve()
            rel = resolved_path.relative_to(workspace_root)
            links.append({"target": rel.as_posix(), "label": label})
        except (ValueError, OSError):
            continue

    return links


# ------------------------------------------------------------------
# Scanner
# ------------------------------------------------------------------

def scan_workspace(client_path: str) -> dict:
    """Walk a client workspace and build a node/edge graph of markdown files and their links.

    Returns {"nodes": [...], "edges": [...]}.
    """
    root = Path(client_path).resolve()
    if not root.is_dir():
        return {"nodes": [], "edges": []}

    # Check cache
    now = time.monotonic()
    if client_path in _cache:
        cached_result, cached_mtime, cached_at = _cache[client_path]
        if now - cached_at < _CACHE_TTL:
            # Quick mtime check — scan only .md files for newer mtime
            newest = _newest_md_mtime(root)
            if newest <= cached_mtime:
                return cached_result

    # Phase 1: collect all files
    nodes_by_path: dict[str, dict] = {}
    md_files: list[Path] = []
    md_stems: dict[str, list[Path]] = {}  # lowercase stem -> list of relative Paths

    for dirpath, dirnames, filenames in os.walk(root):
        current = Path(dirpath)
        depth = len(current.relative_to(root).parts)
        if depth > MAX_DEPTH:
            dirnames.clear()
            continue

        # Filter skipped directories in-place
        rel_parts = current.relative_to(root).parts
        dirnames[:] = [
            d for d in dirnames
            if not _should_skip(d, (*rel_parts, d))
        ]

        for fname in filenames:
            fpath = current / fname
            ext = fpath.suffix.lower()
            ftype = _file_type(ext)
            rel = fpath.relative_to(root).as_posix()

            if ftype == "markdown":
                nodes_by_path[rel] = {
                    "id": rel,
                    "name": fpath.name,
                    "type": ftype,
                    "directory": str(Path(rel).parent),
                    "backlink_count": 0,
                }
                md_files.append(fpath)
                stem_lower = fpath.stem.lower()
                md_stems.setdefault(stem_lower, []).append(fpath.relative_to(root))

    # Phase 2: parse links from each markdown file, build edges
    edges: list[dict] = []
    seen_edges: set[tuple[str, str]] = set()

    for fpath in md_files:
        rel_source = fpath.relative_to(root).as_posix()
        try:
            content = fpath.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue

        links = _parse_links(content, fpath, root, md_stems)

        for link in links:
            target = link["target"]

            # Add secondary node if target exists on disk but isn't already a node
            if target not in nodes_by_path:
                target_abs = root / target
                if target_abs.exists():
                    ext = target_abs.suffix.lower()
                    nodes_by_path[target] = {
                        "id": target,
                        "name": target_abs.name,
                        "type": _file_type(ext),
                        "directory": str(Path(target).parent),
                        "backlink_count": 0,
                    }
                else:
                    continue  # target doesn't exist on disk — skip edge

            # Deduplicate edges
            edge_key = (rel_source, target)
            if edge_key in seen_edges:
                continue
            seen_edges.add(edge_key)

            edges.append({
                "source": rel_source,
                "target": target,
                "label": link["label"],
            })

            # Increment backlink count on target
            nodes_by_path[target]["backlink_count"] += 1

    result = {
        "nodes": list(nodes_by_path.values()),
        "edges": edges,
    }

    # Update cache
    newest_mtime = _newest_md_mtime(root)
    _cache[client_path] = (result, newest_mtime, now)

    return result


def _newest_md_mtime(root: Path) -> float:
    """Find the newest mtime among .md files in the workspace."""
    newest = 0.0
    for dirpath, dirnames, filenames in os.walk(root):
        current = Path(dirpath)
        depth = len(current.relative_to(root).parts)
        if depth > MAX_DEPTH:
            dirnames.clear()
            continue
        rel_parts = current.relative_to(root).parts
        dirnames[:] = [d for d in dirnames if not _should_skip(d, (*rel_parts, d))]
        for fname in filenames:
            if fname.lower().endswith((".md", ".mdx")):
                try:
                    mt = (current / fname).stat().st_mtime
                    if mt > newest:
                        newest = mt
                except OSError:
                    pass
    return newest


# ------------------------------------------------------------------
# File reader
# ------------------------------------------------------------------

def read_file_content(client_path: str, file_path: str) -> dict:
    """Read a single file's content safely.

    Returns {"path", "name", "content", "type", "size_bytes", "modified_iso", "encoding"}.
    """
    root = Path(client_path).resolve()
    target = (root / file_path).resolve()

    # Prevent path traversal
    if not str(target).startswith(str(root)):
        raise ValueError(f"Path traversal detected: {file_path}")

    if not target.is_file():
        raise FileNotFoundError(f"File not found: {file_path}")

    stat = target.stat()
    ext = target.suffix.lower()
    ftype = _file_type(ext)
    modified = datetime.fromtimestamp(stat.st_mtime, tz=UTC).isoformat()

    if stat.st_size > MAX_FILE_SIZE:
        return {
            "path": file_path,
            "name": target.name,
            "content": None,
            "type": ftype,
            "size_bytes": stat.st_size,
            "modified_iso": modified,
            "encoding": "utf-8",
        }

    if ftype == "image":
        raw = target.read_bytes()
        return {
            "path": file_path,
            "name": target.name,
            "content": base64.b64encode(raw).decode("ascii"),
            "type": ftype,
            "size_bytes": stat.st_size,
            "modified_iso": modified,
            "encoding": "base64",
        }

    if ftype == "pdf":
        return {
            "path": file_path,
            "name": target.name,
            "content": None,
            "type": ftype,
            "size_bytes": stat.st_size,
            "modified_iso": modified,
            "encoding": "utf-8",
        }

    content = target.read_text(encoding="utf-8", errors="replace")
    return {
        "path": file_path,
        "name": target.name,
        "content": content,
        "type": ftype,
        "size_bytes": stat.st_size,
        "modified_iso": modified,
        "encoding": "utf-8",
    }
