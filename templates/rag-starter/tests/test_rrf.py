import pytest

from rag_starter.rrf import rrf_fuse


def test_rrf_scores_match_formula():
    fused = rrf_fuse({"fts": ["a", "b", "c"], "vec": ["b", "d"]}, k=60)
    scores = {f.item: f.score for f in fused}
    assert scores["a"] == pytest.approx(1 / 61)
    assert scores["b"] == pytest.approx(1 / 62 + 1 / 61)
    assert scores["c"] == pytest.approx(1 / 63)
    assert scores["d"] == pytest.approx(1 / 62)
    assert [f.item for f in fused] == ["b", "a", "d", "c"]
    assert next(f for f in fused if f.item == "b").ranks == {"fts": 2, "vec": 1}


def test_agreement_beats_single_first_place():
    fused = rrf_fuse({"fts": ["solo", "both"], "vec": ["x", "both"]})
    assert fused[0].item == "both"


def test_weights_and_k():
    fused = rrf_fuse({"fts": ["a"], "vec": ["b"]}, k=0, weights={"fts": 2.0, "vec": 1.0})
    assert fused[0].item == "a" and fused[0].score == pytest.approx(2.0)
    assert fused[1].score == pytest.approx(1.0)


def test_duplicate_in_one_list_votes_once():
    fused = rrf_fuse({"fts": ["a", "a", "b"]}, k=60)
    scores = {f.item: f.score for f in fused}
    assert scores["a"] == pytest.approx(1 / 61)
    assert scores["b"] == pytest.approx(1 / 63)  # rank keeps counting positions


def test_ties_are_deterministic_and_limit():
    fused = rrf_fuse({"fts": ["a", "b"], "vec": ["b", "a"]}, limit=1)
    assert len(fused) == 1 and fused[0].item == "a"  # equal scores: first seen wins


def test_empty_and_bad_k():
    assert rrf_fuse({}) == []
    with pytest.raises(ValueError):
        rrf_fuse({"a": ["x"]}, k=-1)
