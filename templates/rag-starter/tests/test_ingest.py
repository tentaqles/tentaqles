import pytest

from rag_starter.chunking import content_hash, split_text
from rag_starter.embeddings import MAX_HNSW_HALFVEC_DIMS, HashEmbedder, OpenAICompatibleEmbedder, check_dim
from rag_starter.ingest import Ingestor
from rag_starter.models import SourceDocument
from rag_starter.record_manager import InMemoryRecordManager
from rag_starter.store import InMemoryStore


class CountingEmbedder(HashEmbedder):
    def __init__(self):
        super().__init__(dim=64)
        self.texts: list[str] = []

    def embed(self, texts):
        self.texts.extend(texts)
        return super().embed(texts)


def setup():
    store, emb, rm = InMemoryStore(), CountingEmbedder(), InMemoryRecordManager()
    return store, emb, rm, Ingestor(store, emb, rm, max_chars=200, overlap=20)


DOCS = [
    SourceDocument("a", "Alpha document about refunds.\n\nSecond paragraph."),
    SourceDocument("b", "Beta document about pricing."),
    SourceDocument("c", "Gamma document about SSO."),
]


def test_first_sync_adds_everything():
    store, emb, rm, ing = setup()
    r = ing.sync(DOCS)
    assert sorted(r.added) == ["a", "b", "c"] and not r.skipped and not r.deleted
    assert store.source_ids() == {"a", "b", "c"}
    assert rm.list_sources("default") == {"a", "b", "c"}


def test_resync_unchanged_skips_and_does_not_embed():
    store, emb, rm, ing = setup()
    ing.sync(DOCS)
    emb.texts.clear()
    r = ing.sync(DOCS)
    assert sorted(r.skipped) == ["a", "b", "c"]
    assert emb.texts == []  # no embedding spend on unchanged docs
    assert r.chunks_written == 0


def test_whitespace_only_change_is_skipped():
    _, emb, _, ing = setup()
    ing.sync(DOCS)
    edited = [SourceDocument("a", "Alpha   document about refunds.\n\n\nSecond paragraph.  "), *DOCS[1:]]
    assert ing.sync(edited).skipped == ["a", "b", "c"]


def test_changed_doc_replaces_its_chunks():
    store, emb, rm, ing = setup()
    ing.sync(DOCS)
    emb.texts.clear()
    changed = [SourceDocument("a", "Alpha now talks about invoices."), *DOCS[1:]]
    r = ing.sync(changed)
    assert r.updated == ["a"] and sorted(r.skipped) == ["b", "c"]
    a_chunks = [c for c in store.chunks.values() if c.source_id == "a"]
    assert [c.content for c in a_chunks] == ["Alpha now talks about invoices."]
    assert emb.texts == ["Alpha now talks about invoices."]


def test_metadata_change_triggers_reingest():
    _, _, _, ing = setup()
    ing.sync(DOCS)
    changed = [SourceDocument("a", DOCS[0].text, metadata={"document_type": "policy"}), *DOCS[1:]]
    assert ing.sync(changed).updated == ["a"]


def test_removed_doc_is_deleted_from_store_and_records():
    store, _, rm, ing = setup()
    ing.sync(DOCS)
    r = ing.sync(DOCS[:2])
    assert r.deleted == ["c"]
    assert store.source_ids() == {"a", "b"}
    assert rm.get("default", "c") is None


def test_partial_sync_keeps_missing_docs():
    store, _, _, ing = setup()
    ing.sync(DOCS)
    r = ing.sync(DOCS[:1], delete_missing=False)
    assert r.deleted == [] and store.source_ids() == {"a", "b", "c"}


def test_namespaces_are_isolated():
    _, _, rm, ing = setup()
    ing.sync(DOCS, namespace="acme")
    ing.sync(DOCS[:1], namespace="globex")
    assert rm.list_sources("acme") == {"a", "b", "c"}
    assert rm.list_sources("globex") == {"a"}


def test_duplicate_ids_rejected():
    _, _, _, ing = setup()
    with pytest.raises(ValueError, match="duplicate"):
        ing.sync([DOCS[0], DOCS[0]])


def test_runaway_doc_is_truncated():
    store, emb, rm = InMemoryStore(), HashEmbedder(dim=32), InMemoryRecordManager()
    ing = Ingestor(store, emb, rm, max_chars=100, overlap=10, max_doc_chars=250)
    r = ing.sync([SourceDocument("big", "word " * 1000)])
    assert r.truncated == ["big"]
    assert sum(len(c.content) for c in store.chunks.values()) < 400


def test_split_text_bounds_and_overlap():
    text = " ".join(f"Sentence number {i} is here." for i in range(60))
    chunks = split_text(text, max_chars=120, overlap=30)
    assert all(len(c) <= 120 for c in chunks)
    assert len(chunks) > 5
    assert content_hash("a  b\n") == content_hash("a b")


def test_hnsw_dimension_cap():
    assert check_dim(MAX_HNSW_HALFVEC_DIMS) == 4000
    with pytest.raises(ValueError, match="HNSW"):
        check_dim(4001)
    with pytest.raises(ValueError):
        OpenAICompatibleEmbedder(dim=3072 * 2)


def test_openai_embedder_batches_and_checks_dims():
    calls = []

    def post(texts):
        calls.append(len(texts))
        return [[0.0] * 8 for _ in texts]

    emb = OpenAICompatibleEmbedder(dim=8, batch_size=3, _post=post)
    assert len(emb.embed(["x"] * 7)) == 7
    assert calls == [3, 3, 1]
    bad = OpenAICompatibleEmbedder(dim=16, _post=post)
    with pytest.raises(ValueError, match="dims"):
        bad.embed(["x"])
