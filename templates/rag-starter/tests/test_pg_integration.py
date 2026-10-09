"""Optional: PostgresStore + PostgresRecordManager against a real pgvector database.

Skipped unless RAG_STARTER_TEST_DATABASE_URL points at a DISPOSABLE database
that already has migrations/ applied (see README "Testing the SQL").
The test deletes everything in the namespace it uses.
"""

import os

import pytest

from rag_starter.embeddings import HashEmbedder
from rag_starter.ingest import Ingestor
from rag_starter.models import SourceDocument
from rag_starter.retrieval import Retriever

URL = os.environ.get("RAG_STARTER_TEST_DATABASE_URL")
pytestmark = pytest.mark.skipif(not URL, reason="RAG_STARTER_TEST_DATABASE_URL not set")

NS = "pytest-integration"


@pytest.fixture
def conn():
    psycopg = pytest.importorskip("psycopg")
    c = psycopg.connect(URL)
    with c.cursor() as cur:
        cur.execute("delete from public.documents where namespace = %s", (NS,))
        cur.execute("delete from public.record_manager where namespace = %s", (NS,))
    c.commit()
    yield c
    c.close()


def test_ingest_search_resync(conn):
    from rag_starter.pg import PostgresRecordManager, PostgresStore

    store, rm, emb = PostgresStore(conn, NS), PostgresRecordManager(conn), HashEmbedder(dim=1536)
    ing = Ingestor(store, emb, rm)
    docs = [
        SourceDocument("refunds", "Refunds are allowed within 30 days.", metadata={"document_type": "policy"}),
        SourceDocument("pricing", "The Team plan costs 49 dollars per month.", metadata={"document_type": "reference"}),
    ]
    assert sorted(ing.sync(docs, NS).added) == ["pricing", "refunds"]
    assert sorted(ing.sync(docs, NS).skipped) == ["pricing", "refunds"]

    r = Retriever(store, emb, top_n=2)
    res = r.retrieve("refund within how many days", {"document_type": "article"})
    assert res.filter_fallback
    assert res.candidates[0].source_id == "refunds"
    assert res.candidates[0].similarity is not None and res.candidates[0].fts_rank is not None

    rep = ing.sync(docs[:1], NS)
    assert rep.deleted == ["pricing"]
    assert rm.list_sources(NS) == {"refunds"}
