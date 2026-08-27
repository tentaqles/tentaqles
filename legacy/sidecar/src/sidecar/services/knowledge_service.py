"""Knowledge service — Obsidian-style cross-project knowledge discovery."""

from __future__ import annotations

import re
import shutil
import uuid
from datetime import date
from pathlib import Path

from sidecar.models import KnowledgeEntry, KnowledgeLevel, KnowledgeType
from sidecar.parsers import parse_yaml_frontmatter, read_file_content


def _global_knowledge_dir() -> Path:
    return Path.home() / ".tentaqles" / "knowledge"


def _client_knowledge_dir(client_path: str) -> Path:
    return Path(client_path) / ".claude" / "knowledge"


def _project_knowledge_dir(project_path: str) -> Path:
    return Path(project_path) / ".claude" / "knowledge"


def _knowledge_dir_for_level(level: str, workspace_path: str | None, client_path: str | None) -> Path:
    if level == "global":
        return _global_knowledge_dir()
    elif level == "client" and client_path:
        return _client_knowledge_dir(client_path)
    elif level == "project" and workspace_path:
        return _project_knowledge_dir(workspace_path)
    raise ValueError(f"Cannot resolve knowledge dir for level={level}")


def _parse_knowledge_file(file_path: Path, level: KnowledgeLevel) -> KnowledgeEntry | None:
    content = read_file_content(str(file_path))
    if content is None:
        return None

    frontmatter, body = parse_yaml_frontmatter(content)

    tags = frontmatter.get("tags", [])
    if isinstance(tags, str):
        tags = [t.strip() for t in tags.split(",") if t.strip()]

    linked = frontmatter.get("linked", [])
    if isinstance(linked, str):
        linked = [item.strip() for item in linked.split(",") if item.strip()]

    type_str = frontmatter.get("type", "learning")
    try:
        entry_type = KnowledgeType(type_str)
    except ValueError:
        entry_type = KnowledgeType.LEARNING

    return KnowledgeEntry(
        id=frontmatter.get("id", file_path.stem),
        title=frontmatter.get("title", file_path.stem),
        tags=tags,
        source_project=frontmatter.get("source_project"),
        source_client=frontmatter.get("source_client"),
        author=frontmatter.get("author"),
        date=frontmatter.get("date", ""),
        linked=linked,
        type=entry_type,
        level=level,
        file_path=str(file_path),
        body=body,
    )


def _scan_knowledge_dir(knowledge_dir: Path, level: KnowledgeLevel) -> list[KnowledgeEntry]:
    results: list[KnowledgeEntry] = []
    if not knowledge_dir.is_dir():
        return results
    for f in sorted(knowledge_dir.iterdir()):
        if f.is_file() and f.suffix == ".md":
            entry = _parse_knowledge_file(f, level)
            if entry:
                results.append(entry)
    return results


def discover_knowledge(
    query: str = "",
    tags: list[str] | None = None,
    type_filter: str | None = None,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> list[KnowledgeEntry]:
    """Search knowledge across all levels."""
    all_entries: list[KnowledgeEntry] = []
    all_entries.extend(_scan_knowledge_dir(_global_knowledge_dir(), KnowledgeLevel.GLOBAL))
    if client_path:
        all_entries.extend(_scan_knowledge_dir(_client_knowledge_dir(client_path), KnowledgeLevel.CLIENT))
    if workspace_path:
        all_entries.extend(_scan_knowledge_dir(_project_knowledge_dir(workspace_path), KnowledgeLevel.PROJECT))

    # Filter
    results = all_entries
    if query:
        q = query.lower()
        results = [
            e for e in results if q in e.title.lower() or q in e.body.lower() or any(q in t.lower() for t in e.tags)
        ]
    if tags:
        tag_set = {t.lower() for t in tags}
        results = [e for e in results if tag_set & {t.lower() for t in e.tags}]
    if type_filter:
        results = [e for e in results if e.type == type_filter]

    return sorted(results, key=lambda e: e.date, reverse=True)


def pull_knowledge(
    id: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> KnowledgeEntry | None:
    """Get full content of a knowledge entry by ID."""
    all_entries = discover_knowledge(workspace_path=workspace_path, client_path=client_path)
    for entry in all_entries:
        if entry.id == id:
            return entry
    return None


def contribute_knowledge(
    title: str,
    content: str,
    tags: list[str],
    type: str = "learning",
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> KnowledgeEntry:
    """Create a new knowledge entry at project level (default)."""
    # Determine target dir
    if workspace_path:
        target_dir = _project_knowledge_dir(workspace_path)
        level = KnowledgeLevel.PROJECT
    elif client_path:
        target_dir = _client_knowledge_dir(client_path)
        level = KnowledgeLevel.CLIENT
    else:
        target_dir = _global_knowledge_dir()
        level = KnowledgeLevel.GLOBAL

    target_dir.mkdir(parents=True, exist_ok=True)

    slug = re.sub(r"[^a-z0-9-]", "", title.lower().replace(" ", "-"))[:50]
    entry_id = f"ke-{date.today().strftime('%Y%m%d')}-{slug}"
    filename = f"{entry_id}.md"

    # Avoid collisions
    if (target_dir / filename).exists():
        entry_id = f"{entry_id}-{uuid.uuid4().hex[:6]}"
        filename = f"{entry_id}.md"

    tags_str = ", ".join(tags)
    frontmatter = f"""---
id: {entry_id}
title: "{title}"
tags: [{tags_str}]
source_project: {Path(workspace_path).name if workspace_path else ""}
source_client: {Path(client_path).name if client_path else ""}
author: claude
date: {date.today().isoformat()}
linked: []
type: {type}
---

{content}
"""
    file_path = target_dir / filename
    file_path.write_text(frontmatter, encoding="utf-8")

    return KnowledgeEntry(
        id=entry_id,
        title=title,
        tags=tags,
        source_project=Path(workspace_path).name if workspace_path else None,
        source_client=Path(client_path).name if client_path else None,
        author="claude",
        date=date.today().isoformat(),
        type=KnowledgeType(type) if type in [t.value for t in KnowledgeType] else KnowledgeType.LEARNING,
        level=level,
        file_path=str(file_path),
        body=content,
    )


def promote_knowledge(
    id: str,
    from_level: str,
    to_level: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> str:
    """Copy a knowledge entry from one level to a higher one."""
    entry = pull_knowledge(id, workspace_path, client_path)
    if not entry:
        return f"Knowledge entry not found: {id}"

    src = Path(entry.file_path)
    if not src.exists():
        return f"Source file not found: {src}"

    dest_dir = _knowledge_dir_for_level(to_level, workspace_path, client_path)
    dest_dir.mkdir(parents=True, exist_ok=True)
    dest = dest_dir / src.name
    shutil.copy2(str(src), str(dest))
    return f"Promoted {id} from {from_level} to {to_level}"


def link_knowledge(
    source_id: str,
    target_id: str,
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> str:
    """Add bidirectional links between two knowledge entries."""
    source = pull_knowledge(source_id, workspace_path, client_path)
    target = pull_knowledge(target_id, workspace_path, client_path)
    if not source or not target:
        return "One or both entries not found"

    for entry, other_id in [(source, target_id), (target, source_id)]:
        if other_id not in entry.linked:
            path = Path(entry.file_path)
            content = path.read_text(encoding="utf-8")
            # Update linked field in frontmatter
            old_linked = f"linked: [{', '.join(entry.linked)}]"
            new_linked = f"linked: [{', '.join(entry.linked + [other_id])}]"
            content = content.replace(old_linked, new_linked, 1)
            path.write_text(content, encoding="utf-8")

    return f"Linked {source_id} \u2194 {target_id}"


def get_graph(
    workspace_path: str | None = None,
    client_path: str | None = None,
) -> dict:
    """Return nodes and edges for graph visualization."""
    entries = discover_knowledge(workspace_path=workspace_path, client_path=client_path)
    nodes = [{"id": e.id, "title": e.title, "tags": e.tags, "level": e.level, "type": e.type} for e in entries]
    edges = []
    for e in entries:
        for linked_id in e.linked:
            edges.append({"source": e.id, "target": linked_id})
        for tag in e.tags:
            for other in entries:
                if other.id != e.id and tag in other.tags:
                    edge = {"source": e.id, "target": other.id, "via_tag": tag}
                    if edge not in edges:
                        edges.append(edge)
    return {"nodes": nodes, "edges": edges}
