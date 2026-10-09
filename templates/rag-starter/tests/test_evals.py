import json

import pytest
from conftest import FakeDecider

from rag_starter.app import EXAMPLE_CORPUS
from rag_starter.evals import (
    CallableCheck,
    GoldenItem,
    JevFaithfulness,
    TieredFaithfulness,
    evaluate,
    hit_at_k,
    load_golden,
    recall_at_k,
    reciprocal_rank,
)
from rag_starter.evals.__main__ import main
from rag_starter.models import Candidate

GOLDEN = EXAMPLE_CORPUS.parent / "golden.jsonl"


def test_recall_at_k():
    assert recall_at_k(["a", "b", "c"], ["a", "c"], k=2) == 0.5
    assert recall_at_k(["a", "b", "c"], ["a", "c"], k=3) == 1.0
    assert recall_at_k(["x"], [], k=3) == 1.0
    # duplicates (several chunks of one doc) don't push others out of top-k
    assert recall_at_k(["a", "a", "a", "b"], ["b"], k=2) == 1.0


def test_mrr_and_hit():
    assert reciprocal_rank(["x", "a"], ["a"]) == 0.5
    assert reciprocal_rank(["x", "y", "a"], ["a"], k=2) == 0.0
    assert reciprocal_rank(["a"], ["a", "b"]) == 1.0
    assert hit_at_k(["x", "a"], ["a"], 1) == 0.0 and hit_at_k(["x", "a"], ["a"], 2) == 1.0


def cands(*ids):
    return [Candidate(chunk_id=f"{i}#0", source_id=i, content=f"text of {i}") for i in ids]


def test_evaluate_aggregates_and_gates():
    golden = [
        GoldenItem("q1", "?", ("a",)),
        GoldenItem("q2", "?", ("b",)),
        GoldenItem("q3", "?", ()),  # unanswerable: excluded from recall/MRR
    ]
    results = {"q1": cands("a", "x"), "q2": cands("x", "y", "b"), "q3": cands("z")}
    lookup = {g.question + g.id: g.id for g in golden}
    rep = evaluate(lambda q, f: results[lookup[q]], [GoldenItem(g.id, "?" + g.id, g.relevant) for g in golden], k=2)
    assert rep.recall_at_k == pytest.approx(0.5)
    assert rep.mrr == pytest.approx(0.5)
    assert rep.passed

    gated = evaluate(
        lambda q, f: results[lookup[q]],
        [GoldenItem(g.id, "?" + g.id, g.relevant) for g in golden],
        k=2,
        min_recall=0.9,
        min_mrr=0.4,
    )
    assert not gated.passed and len(gated.failures) == 1 and "recall@2" in gated.failures[0]


def test_retrieval_errors_fail_the_gate():
    def boom(q, f):
        raise RuntimeError("db down")

    rep = evaluate(boom, [GoldenItem("q", "?", ("a",))], k=3)
    assert not rep.passed and rep.items[0].error.startswith("RuntimeError")


def test_faithfulness_hook_and_threshold():
    golden = [GoldenItem("q1", "?", ("a",)), GoldenItem("q2", "?", ("a",))]
    check = CallableCheck(lambda q, ans, ctx: 1.0 if "text of a" in ctx else 0.0)
    rep = evaluate(
        lambda q, f: cands("a"),
        golden,
        k=1,
        answers={"q1": "x", "q2": "y"},
        faithfulness=check,
        min_faithfulness=0.9,
    )
    assert rep.faithfulness == 1.0 and rep.passed

    missing = evaluate(lambda q, f: cands("a"), golden, k=1, min_faithfulness=0.5)
    assert not missing.passed  # a threshold with nothing scored is a failure, not a pass


def test_jev_faithfulness_and_tiering():
    d = FakeDecider(lambda s, q: {"supported": {"noul": 0.5}})
    jev = JevFaithfulness(d)
    assert jev.score("q", "the answer", ["ctx"]) == 0.5
    state, questions = d.calls[0]
    assert "untrusted" in state["notice"] and questions["supported"]["type"] == "noul"
    judge_calls = []
    tier = TieredFaithfulness(jev, CallableCheck(lambda *a: judge_calls.append(1) or 0.1))
    assert tier.score("q", "a", ["c"]) == 0.1 and judge_calls == [1]


def test_load_golden_validates(tmp_path):
    items = load_golden(GOLDEN)
    assert len(items) == 10 and items[0].id == "refund-window"
    bad = tmp_path / "bad.jsonl"
    bad.write_text('{"id": "x", "question": "?"}\n', encoding="utf-8")
    with pytest.raises(ValueError, match="relevant"):
        load_golden(bad)
    dup = tmp_path / "dup.jsonl"
    dup.write_text('{"id":"x","question":"?","relevant":[]}\n' * 2, encoding="utf-8")
    with pytest.raises(ValueError, match="duplicate"):
        load_golden(dup)


def test_cli_passes_on_example(tmp_path, capsys):
    out = tmp_path / "report.json"
    code = main(["--golden", str(GOLDEN), "--k", "3", "--min-recall", "0.8", "--min-mrr", "0.6", "--json", str(out)])
    assert code == 0
    assert "PASS" in capsys.readouterr().out
    assert json.loads(out.read_text())["passed"] is True


def test_cli_exits_nonzero_below_threshold(capsys):
    code = main(["--golden", str(GOLDEN), "--k", "1", "--min-recall", "0.999", "--min-mrr", "0.999"])
    assert code == 1
    assert "FAIL" in capsys.readouterr().out


def test_cli_bad_input_exits_2(tmp_path, capsys):
    assert main(["--golden", str(tmp_path / "missing.jsonl")]) == 2
