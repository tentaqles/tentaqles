import pytest
from conftest import FakeDecider

from rag_starter.corpus import load_dir
from rag_starter.embeddings import HashEmbedder
from rag_starter.ingest import Ingestor
from rag_starter.jev import JevError
from rag_starter.loop import Step, ToolCall, run_tool_loop
from rag_starter.record_manager import InMemoryRecordManager
from rag_starter.retrieval import Retriever
from rag_starter.router import ModelRouter
from rag_starter.store import InMemoryStore
from rag_starter.app import EXAMPLE_CORPUS


def router(decider, **kw):
    return ModelRouter(decider, cheap_model="small", strong_model="big", **kw)


def answer(tier, conf):
    return lambda s, q: {"tier": {"choice": tier, "confidence": conf, "probabilities": {tier: conf}}}


# ---- router ---------------------------------------------------------------


def test_router_confident_cheap():
    d = FakeDecider(answer("cheap", 0.9))
    r = router(d).route("hi, what are your hours?")
    assert (r.model, r.tier, r.reason) == ("small", "cheap", "jev")
    state, questions = d.calls[0]
    assert questions["tier"]["type"] == "choice" and set(questions["tier"]["criteria"]) == {"cheap", "strong"}
    assert "untrusted" in state["notice"]


def test_router_low_confidence_falls_back_to_strong():
    r = router(FakeDecider(answer("cheap", 0.55))).route("q")
    assert (r.model, r.reason) == ("big", "low_confidence")


def test_router_error_falls_back():
    r = router(FakeDecider(error=JevError("down", status=500))).route("q")
    assert (r.model, r.reason) == ("big", "error")


def test_router_malformed_and_unknown_tier():
    assert router(FakeDecider(lambda s, q: {})).route("q").reason == "error"
    assert router(FakeDecider(answer("medium", 0.99))).route("q").reason == "error"


def test_router_configurable_fallback_tier():
    r = router(FakeDecider(error=JevError("x")), fallback_tier="cheap").route("q")
    assert r.model == "small"


# ---- retrieval --------------------------------------------------------------


@pytest.fixture
def retriever():
    store, emb = InMemoryStore(), HashEmbedder()
    Ingestor(store, emb, InMemoryRecordManager()).sync(load_dir(EXAMPLE_CORPUS))
    return Retriever(store, emb, top_n=3)


def test_hybrid_search_finds_the_doc(retriever):
    res = retriever.retrieve("How many days to request a refund?")
    assert res.candidates[0].source_id == "refund-policy"
    assert res.trace()[0]["rrf"] > 0


def test_filter_that_matches_is_kept(retriever):
    res = retriever.retrieve("refund", {"document_type": "policy"})
    assert not res.filter_fallback
    assert {c.metadata["document_type"] for c in res.candidates} == {"policy"}


def test_filter_with_no_results_falls_back(retriever):
    res = retriever.retrieve("API rate limit", {"document_type": "article"})
    assert res.filter_fallback and res.filters_applied is None
    assert res.candidates[0].source_id == "api-rate-limits"


def test_empty_query_returns_nothing(retriever):
    assert retriever.retrieve("   ").candidates == []


# ---- loop guards --------------------------------------------------------------


class ScriptedLLM:
    def __init__(self, steps):
        self.steps = list(steps)
        self.calls = []

    def __call__(self, messages, tools_enabled):
        self.calls.append(tools_enabled)
        if not tools_enabled:
            return Step(answer="final synthesis")
        return self.steps.pop(0) if self.steps else Step(tool_calls=[ToolCall("search", {"q": str(len(self.calls))})])


def test_loop_returns_answer():
    llm = ScriptedLLM([Step(tool_calls=[ToolCall("search", {"q": "a"})]), Step(answer="done")])
    res = run_tool_loop(llm, {"search": lambda q: f"results for {q}"}, [], max_steps=5)
    assert (res.answer, res.steps, res.stop_reason) == ("done", 2, "answer")
    assert res.tool_log[0]["tool"] == "search" and res.tool_log[0]["ok"]


def test_loop_stops_at_max_steps_and_forces_synthesis():
    llm = ScriptedLLM([])  # searches forever with new args
    res = run_tool_loop(llm, {"search": lambda q: "r"}, [], max_steps=3)
    assert res.stop_reason == "max_steps" and res.steps == 3
    assert res.answer == "final synthesis"
    assert llm.calls == [True, True, True, False]  # last call has tools disabled


def test_loop_repeat_guard():
    same = Step(tool_calls=[ToolCall("search", {"q": "x"})])
    llm = ScriptedLLM([same, same, same])
    res = run_tool_loop(llm, {"search": lambda q: "r"}, [], max_steps=10)
    assert res.stop_reason == "repeat" and res.steps == 2 and res.answer == "final synthesis"


def test_loop_tool_errors_do_not_crash():
    def broken(q):
        raise RuntimeError("db down")

    llm = ScriptedLLM([Step(tool_calls=[ToolCall("search", {"q": "a"}), ToolCall("nope", {})]), Step(answer="ok")])
    res = run_tool_loop(llm, {"search": broken}, [], max_steps=3)
    assert res.answer == "ok"
    assert [e["ok"] for e in res.tool_log] == [False, False]


def test_loop_rejects_zero_steps():
    with pytest.raises(ValueError):
        run_tool_loop(ScriptedLLM([]), {}, [], max_steps=0)
