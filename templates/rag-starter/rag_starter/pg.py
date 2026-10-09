"""Postgres / Supabase backends (psycopg 3). Install with `pip install -e .[pg]`.

The connection string comes from `DATABASE_URL`, injected via
`tq dotenv run -- ...`. Use separate URLs for the two roles:
- **Ingestion:** the service role or owner. It writes chunks and the record manager.
- **Agent:** `rag_agent_readonly`. It can only SELECT and call `hybrid_search`.
"""

from __future__ import annotations

import json
import os
from typing import Any

from .models import Candidate, Chunk


def connect(url_env: str = "DATABASE_URL", **kwargs: Any):
    import psycopg  # lazy: the core package has no DB dependency

    url = os.environ.get(url_env, "")
    if not url:
        raise RuntimeError(f"{url_env} is not set (run with `tq dotenv run -- ...`)")
    return psycopg.connect(url, **kwargs)


def _vec(v: list[float]) -> str:
    return "[" + ",".join(f"{x:.7g}" for x in v) + "]"


class PostgresStore:
    """Talks to the tables and function created by `migrations/`."""

    def __init__(self, conn, namespace: str = "default"):
        self.conn = conn
        self.namespace = namespace

    def upsert_chunks(self, chunks: list[Chunk]) -> None:
        if not chunks:
            return
        with self.conn.cursor() as cur:
            by_source: dict[str, list[Chunk]] = {}
            for c in chunks:
                by_source.setdefault(c.source_id, []).append(c)
            for source_id, group in by_source.items():
                cur.execute(
                    """
                    insert into public.documents (namespace, source_id, title, metadata)
                    values (%s, %s, %s, %s::jsonb)
                    on conflict (namespace, source_id)
                    do update set title = excluded.title, metadata = excluded.metadata, updated_at = now()
                    returning id
                    """,
                    (self.namespace, source_id, group[0].metadata.get("title", ""), json.dumps(group[0].metadata)),
                )
                doc_id = cur.fetchone()[0]
                cur.executemany(
                    """
                    insert into public.chunks (document_id, namespace, chunk_index, content, content_hash, metadata, embedding)
                    values (%s, %s, %s, %s, %s, %s::jsonb, %s::extensions.halfvec)
                    """,
                    [
                        (doc_id, self.namespace, c.index, c.content, c.content_hash, json.dumps(c.metadata), _vec(c.embedding or []))
                        for c in group
                    ],
                )
        self.conn.commit()

    def delete_source(self, source_id: str) -> None:
        with self.conn.cursor() as cur:
            # chunks cascade from documents
            cur.execute(
                "delete from public.documents where namespace = %s and source_id = %s",
                (self.namespace, source_id),
            )
        self.conn.commit()

    def hybrid_search(
        self,
        query_text: str,
        query_embedding: list[float],
        match_count: int,
        filters: dict[str, Any] | None = None,
    ) -> list[Candidate]:
        with self.conn.cursor() as cur:
            cur.execute(
                """
                select chunk_id, source_id, content, metadata, similarity, fts_rank, rrf_score
                from public.hybrid_search(%s, %s::extensions.halfvec, %s, %s::jsonb, %s)
                """,
                (query_text, _vec(query_embedding), match_count, json.dumps(filters or {}), self.namespace),
            )
            rows = cur.fetchall()
        return [
            Candidate(
                chunk_id=str(r[0]),
                source_id=r[1],
                content=r[2],
                metadata=r[3] or {},
                similarity=r[4],
                fts_rank=r[5],
                rrf_score=float(r[6]),
            )
            for r in rows
        ]


class PostgresRecordManager:
    def __init__(self, conn):
        self.conn = conn

    def get(self, namespace: str, source_id: str) -> str | None:
        with self.conn.cursor() as cur:
            cur.execute(
                "select content_hash from public.record_manager where namespace = %s and source_id = %s",
                (namespace, source_id),
            )
            row = cur.fetchone()
        return row[0] if row else None

    def upsert(self, namespace: str, source_id: str, content_hash: str) -> None:
        with self.conn.cursor() as cur:
            cur.execute(
                """
                insert into public.record_manager (namespace, source_id, content_hash)
                values (%s, %s, %s)
                on conflict (namespace, source_id)
                do update set content_hash = excluded.content_hash, updated_at = now()
                """,
                (namespace, source_id, content_hash),
            )
        self.conn.commit()

    def delete(self, namespace: str, source_id: str) -> None:
        with self.conn.cursor() as cur:
            cur.execute(
                "delete from public.record_manager where namespace = %s and source_id = %s",
                (namespace, source_id),
            )
        self.conn.commit()

    def list_sources(self, namespace: str) -> set[str]:
        with self.conn.cursor() as cur:
            cur.execute("select source_id from public.record_manager where namespace = %s", (namespace,))
            return {r[0] for r in cur.fetchall()}
