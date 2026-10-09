"""Load a folder of .md/.txt files as SourceDocuments (source_id = file stem).

Optional front matter with flat `key: value` lines becomes metadata:

    ---
    title: Refund policy
    document_type: policy
    ---
"""

from __future__ import annotations

from pathlib import Path

from .models import SourceDocument


def parse(source_id: str, text: str) -> SourceDocument:
    meta: dict[str, str] = {}
    if text.startswith("---"):
        head, sep, body = text[3:].partition("\n---")
        if sep:
            for line in head.strip().splitlines():
                key, colon, value = line.partition(":")
                if colon:
                    meta[key.strip()] = value.strip()
            text = body.lstrip("\n")
    title = meta.pop("title", source_id)
    return SourceDocument(source_id=source_id, text=text, title=title, metadata=meta)


def load_dir(path: str | Path) -> list[SourceDocument]:
    root = Path(path)
    files = sorted(p for p in root.rglob("*") if p.suffix.lower() in {".md", ".txt"} and p.is_file())
    return [parse(p.stem, p.read_text(encoding="utf-8")) for p in files]
