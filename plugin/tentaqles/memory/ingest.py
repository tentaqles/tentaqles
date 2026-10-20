"""Ingest Markdown folders (an Obsidian vault, a Clippings dir) as reference items.

A connector in the knowledge-base sense: it pulls only what changed since the
last run. Each file is a watermark -- resolved path plus mtime -- so a run over
an unchanged folder costs one stat per file. The item's date is the note's own
``published`` or ``ingested_date`` frontmatter when present, so recency
weighting ranks by when the source appeared, not by when we happened to index
it. Text is redacted by the store like every other write.
"""

from __future__ import annotations

import re
from datetime import date, datetime, timezone
from pathlib import Path

import yaml

MAX_TEXT_CHARS = 50_000
_FRONTMATTER_RE = re.compile(r"\A---[ \t]*\r?\n(.*?)\r?\n---[ \t]*\r?\n?", re.S)
_HEADING_RE = re.compile(r"^#\s+(.+?)\s*$", re.M)


def split_frontmatter(raw: str) -> tuple[dict, str]:
    m = _FRONTMATTER_RE.match(raw)
    if not m:
        return {}, raw
    try:
        meta = yaml.safe_load(m.group(1))
    except yaml.YAMLError:
        meta = {}
    return (meta if isinstance(meta, dict) else {}), raw[m.end():]


def _iso(value) -> str | None:
    if isinstance(value, datetime):
        return value.isoformat()
    if isinstance(value, date):
        return value.isoformat()
    if isinstance(value, str) and value.strip():
        return value.strip()
    return None


def item_date(meta: dict, mtime: float) -> str:
    for key in ("published", "ingested_date", "created", "date"):
        v = _iso(meta.get(key))
        if v:
            return v
    return datetime.fromtimestamp(mtime, tz=timezone.utc).isoformat()


def ingest_markdown_dirs(store, dirs) -> dict:
    """Index every ``*.md`` under each dir. Returns added/updated/skipped counts."""
    result = {"added": 0, "updated": 0, "skipped": 0}
    for d in dirs or []:
        root = Path(d)
        if not root.is_dir():
            continue
        try:
            root_real = root.resolve()
        except OSError:
            continue
        for f in sorted(root.rglob("*.md")):
            # Never follow a link out of the folder: a symlinked file or a
            # symlinked directory could pull anything readable into memory.db.
            try:
                if f.is_symlink() or not f.is_file():
                    continue
                real = f.resolve()
                if not (real == root_real or root_real in real.parents):
                    continue
                mtime = f.stat().st_mtime
            except OSError:
                continue
            key = real.as_posix()
            prev = store.reference_mtime(key)
            if prev is not None and abs(prev - mtime) < 1e-6:
                result["skipped"] += 1
                continue
            try:
                raw = f.read_text(encoding="utf-8", errors="replace")
            except OSError:
                continue
            meta, body = split_frontmatter(raw)
            heading = _HEADING_RE.search(body)
            title = str(meta.get("title") or (heading.group(1) if heading else "") or f.stem)
            desc = meta.get("description")
            text = (f"{desc}\n\n" if isinstance(desc, str) and desc else "") + body.strip()
            url = meta.get("url")
            store.upsert_reference(
                path=key,
                mtime=mtime,
                title=title,
                url=str(url) if url else None,
                created_at=item_date(meta, mtime),
                text=text[:MAX_TEXT_CHARS],
            )
            result["added" if prev is None else "updated"] += 1
    return result
