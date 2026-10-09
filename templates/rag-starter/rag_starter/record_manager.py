"""Record manager: one row per source document, keyed by (namespace, source_id).

Holding the content hash lets a re-sync skip unchanged documents (no embedding
spend), re-ingest changed ones and delete the ones that disappeared from the source.
`rag_starter.pg.PostgresRecordManager` is the same contract over `public.record_manager`.
"""

from __future__ import annotations

from typing import Protocol


class RecordManager(Protocol):
    def get(self, namespace: str, source_id: str) -> str | None: ...

    def upsert(self, namespace: str, source_id: str, content_hash: str) -> None: ...

    def delete(self, namespace: str, source_id: str) -> None: ...

    def list_sources(self, namespace: str) -> set[str]: ...


class InMemoryRecordManager:
    def __init__(self) -> None:
        self.rows: dict[tuple[str, str], str] = {}

    def get(self, namespace: str, source_id: str) -> str | None:
        return self.rows.get((namespace, source_id))

    def upsert(self, namespace: str, source_id: str, content_hash: str) -> None:
        self.rows[(namespace, source_id)] = content_hash

    def delete(self, namespace: str, source_id: str) -> None:
        self.rows.pop((namespace, source_id), None)

    def list_sources(self, namespace: str) -> set[str]:
        return {sid for ns, sid in self.rows if ns == namespace}
