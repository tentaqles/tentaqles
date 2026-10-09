"""Incremental ingestion: hash -> skip unchanged | re-chunk changed | delete removed."""

from __future__ import annotations

import json
from dataclasses import dataclass, field

from .chunking import chunk_document, content_hash
from .embeddings import Embedder
from .models import SourceDocument
from .record_manager import RecordManager
from .store import SearchBackend


@dataclass
class IngestReport:
    added: list[str] = field(default_factory=list)
    updated: list[str] = field(default_factory=list)
    skipped: list[str] = field(default_factory=list)
    deleted: list[str] = field(default_factory=list)
    truncated: list[str] = field(default_factory=list)
    chunks_written: int = 0

    def summary(self) -> str:
        return (
            f"added={len(self.added)} updated={len(self.updated)} skipped={len(self.skipped)} "
            f"deleted={len(self.deleted)} chunks={self.chunks_written}"
        )


def document_hash(doc: SourceDocument) -> str:
    """Hash of everything that ends up in the index: text, title and metadata."""
    meta = json.dumps(doc.metadata, sort_keys=True, default=str)
    return content_hash(f"{doc.title}\n{meta}\n{doc.text}")


@dataclass
class Ingestor:
    store: SearchBackend
    embedder: Embedder
    records: RecordManager
    max_chars: int = 1200
    overlap: int = 150
    max_doc_chars: int = 2_000_000  # bound a single runaway document

    def sync(self, docs: list[SourceDocument], namespace: str = "default", delete_missing: bool = True) -> IngestReport:
        """Bring the index in line with `docs`.

        With `delete_missing=True`, `docs` is the full current set for the
        namespace, and anything the record manager knows about that is not in it
        gets deleted. Pass False for partial or append-only batches.
        """
        ids = [d.source_id for d in docs]
        dupes = {i for i in ids if ids.count(i) > 1}
        if dupes:
            raise ValueError(f"duplicate source_id in batch: {sorted(dupes)}")

        report = IngestReport()
        for doc in docs:
            if len(doc.text) > self.max_doc_chars:
                doc = SourceDocument(doc.source_id, doc.text[: self.max_doc_chars], doc.title, doc.metadata)
                report.truncated.append(doc.source_id)
            h = document_hash(doc)
            previous = self.records.get(namespace, doc.source_id)
            if previous == h:
                report.skipped.append(doc.source_id)
                continue
            chunks = chunk_document(doc, self.max_chars, self.overlap)
            vectors = self.embedder.embed([c.content for c in chunks]) if chunks else []
            for c, v in zip(chunks, vectors, strict=True):
                c.embedding = v
            # Delete-then-insert per document keeps chunk ids contiguous when a doc shrinks.
            self.store.delete_source(doc.source_id)
            self.store.upsert_chunks(chunks)
            self.records.upsert(namespace, doc.source_id, h)
            report.chunks_written += len(chunks)
            (report.updated if previous is not None else report.added).append(doc.source_id)

        if delete_missing:
            for sid in sorted(self.records.list_sources(namespace) - set(ids)):
                self.store.delete_source(sid)
                self.records.delete(namespace, sid)
                report.deleted.append(sid)
        return report
