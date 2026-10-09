"""Paragraph-first chunking with overlap, plus the content hash the record manager uses."""

from __future__ import annotations

import hashlib
import re

from .models import Chunk, SourceDocument

_SENTENCE = re.compile(r"(?<=[.!?])\s+")


def normalize(text: str) -> str:
    return re.sub(r"\s+", " ", text).strip()


def content_hash(text: str) -> str:
    """Stable hash of whitespace-normalised text. Formatting-only edits do not re-embed."""
    return hashlib.sha256(normalize(text).encode("utf-8")).hexdigest()


def _pieces(text: str, max_chars: int) -> list[str]:
    """Split into paragraphs, then sentences, then hard windows, each <= max_chars."""
    out: list[str] = []
    for para in re.split(r"\n\s*\n", text):
        para = normalize(para)
        if not para:
            continue
        if len(para) <= max_chars:
            out.append(para)
            continue
        for sent in _SENTENCE.split(para):
            while len(sent) > max_chars:
                out.append(sent[:max_chars])
                sent = sent[max_chars:]
            if sent:
                out.append(sent)
    return out


def split_text(text: str, max_chars: int = 1200, overlap: int = 150) -> list[str]:
    if max_chars <= 0 or overlap < 0 or overlap >= max_chars:
        raise ValueError("need max_chars > overlap >= 0")
    chunks: list[str] = []
    current = ""
    for piece in _pieces(text, max_chars):
        if current and len(current) + 1 + len(piece) > max_chars:
            chunks.append(current)
            tail = current[-overlap:] if overlap else ""
            current = f"{tail} {piece}".strip() if tail and len(tail) + 1 + len(piece) <= max_chars else piece
        else:
            current = f"{current} {piece}".strip()
    if current:
        chunks.append(current)
    return chunks


def chunk_document(doc: SourceDocument, max_chars: int = 1200, overlap: int = 150) -> list[Chunk]:
    return [
        Chunk(
            chunk_id=f"{doc.source_id}#{i}",
            source_id=doc.source_id,
            index=i,
            content=piece,
            content_hash=content_hash(piece),
            metadata={**doc.metadata, "title": doc.title} if doc.title else dict(doc.metadata),
        )
        for i, piece in enumerate(split_text(doc.text, max_chars, overlap))
    ]
