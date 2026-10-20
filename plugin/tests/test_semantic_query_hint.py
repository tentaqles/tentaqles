"""Semantic facts carry the question that would find them.

Raw facts are hard to hit with a keyword search ("Deploys go through a Vercel
preview first" does not contain "how do we deploy"). The consolidator therefore
asks the model for each fact *and* the question a teammate would type to find
it, stores the hint next to the fact, and search indexes both.
"""

from __future__ import annotations

import pytest

from tentaqles.memory.consolidator import MemoryConsolidator
from tentaqles.memory.store import MemoryStore


@pytest.fixture
def store(tmp_path, monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: None)

    def _no_backend(self):
        raise RuntimeError("fastembed is not installed")

    monkeypatch.setattr(MemoryStore, "_get_emb", _no_backend)
    s = MemoryStore(tmp_path)
    yield s
    s.close()


def _ended_session(store, summary: str) -> str:
    store.start_session()
    return store.end_session(summary)["id"]


def _llm(responses: list[str]):
    calls: list[str] = []

    def fn(prompt: str) -> str:
        calls.append(prompt)
        return responses.pop(0) if responses else ""

    fn.calls = calls  # type: ignore[attr-defined]
    return fn


def test_prompt_asks_for_a_query_hint_per_fact(store):
    sid = _ended_session(store, "Set up the Vercel preview deploy flow")
    llm = _llm(["Deploys go through a Vercel preview first || how do we deploy"])

    MemoryConsolidator(store, llm_fn=llm).run_compaction([sid])

    assert "||" in llm.calls[0]
    assert "question" in llm.calls[0].lower()


def test_fact_and_hint_are_split_and_stored(store):
    sid = _ended_session(store, "Set up the Vercel preview deploy flow")
    llm = _llm(["Deploys go through a Vercel preview first || how do we deploy to production"])

    (fid,) = MemoryConsolidator(store, llm_fn=llm).run_compaction([sid])

    fact, hint = store._conn.execute("SELECT fact, query_hint FROM semantic_memories WHERE id=?", (fid,)).fetchone()
    assert fact == "Deploys go through a Vercel preview first"
    assert hint == "how do we deploy to production"


def test_fact_without_hint_is_still_recorded(store):
    sid = _ended_session(store, "Timezone handling across services")
    llm = _llm(["Always store timestamps in UTC"])

    (fid,) = MemoryConsolidator(store, llm_fn=llm).run_compaction([sid])

    fact, hint = store._conn.execute("SELECT fact, query_hint FROM semantic_memories WHERE id=?", (fid,)).fetchone()
    assert fact == "Always store timestamps in UTC"
    assert hint is None


def test_search_finds_a_fact_through_its_hint(store):
    store.record_semantic_fact(
        "Deploys go through a Vercel preview first", ["s1"], query_hint="how do we ship to production"
    )
    store.record_semantic_fact("Always store timestamps in UTC", ["s1"])

    results = store.search_memory("ship production")

    assert results and results[0]["type"] == "semantic"
    assert results[0]["text"].startswith("Deploys go through a Vercel preview first")


def test_get_semantic_facts_exposes_the_hint(store):
    store.record_semantic_fact("Fact with hint", ["s1"], query_hint="find me")

    (fact,) = store.get_semantic_facts(limit=1)

    assert fact["query_hint"] == "find me"
