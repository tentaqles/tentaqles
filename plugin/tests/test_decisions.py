"""Tests for automatic decision detection.

The first version of this detector matched directives like "use X instead of
Y" and, run against two real transcripts, produced mid-sentence fragments
recorded as decisions. These tests pin the trade that fixed it: capture only
what is explicitly stated as a decision, and stay silent otherwise.
"""

import json

from tentaqles.decisions import deduplicate_decisions, detect_decisions


def _transcript(tmp_path, turns):
    """Write a JSONL transcript of (role, text) pairs."""
    p = tmp_path / "transcript.jsonl"
    with open(p, "w", encoding="utf-8") as fh:
        for role, text in turns:
            fh.write(json.dumps({"type": role, "message": {"role": role, "content": text}}) + "\n")
    return str(p)


def test_captures_an_explicitly_stated_decision(tmp_path):
    t = _transcript(tmp_path, [
        ("user", "which database should we use here"),
        ("assistant", "Decision: store the score sheet in Postgres rather than a spreadsheet, "
                      "because the polo data already lives there and the join is the whole point."),
    ])
    got = detect_decisions(t)
    assert len(got) == 1, got
    assert "Postgres" in got[0]["chosen"]
    assert "polo data" in got[0]["rationale"]
    # Never presented as something a person chose to record.
    assert got[0]["confidence"] == "low"
    assert "auto" in got[0]["tags"]


def test_captures_portuguese(tmp_path):
    t = _transcript(tmp_path, [
        ("assistant", "Decisão: usar o dropdown alimentado por blog_post_tags, "
                      "porque syncPostTags faz upsert por nome."),
    ])
    got = detect_decisions(t)
    assert len(got) == 1, got
    assert "blog_post_tags" in got[0]["chosen"]
    assert "syncPostTags" in got[0]["rationale"]


def test_first_person_past_tense_counts(tmp_path):
    t = _transcript(tmp_path, [
        ("assistant", "I decided to keep the include file whole rather than removing single keys."),
    ])
    got = detect_decisions(t)
    assert len(got) == 1
    assert "include file" in got[0]["chosen"]


# The failures that made the first version unusable.
def test_ignores_questions_and_proposals(tmp_path):
    t = _transcript(tmp_path, [
        ("assistant", "Should we use Postgres instead of a spreadsheet for this?"),
        ("assistant", "We could use Redis instead of Postgres if latency matters."),
        ("user", "use the flex layout instead of the grid one"),
        ("assistant", "One option is to settle on a single shared config."),
    ])
    assert detect_decisions(t) == [], "proposals, questions and bare directives are not decisions"


def test_ignores_mid_sentence_prose(tmp_path):
    # Shaped after a real false positive: prose containing "instead of".
    t = _transcript(tmp_path, [
        ("user", "capture the brand's actual vocabulary, rhythm, and connectors "
                 "instead of the generic AI phrasing"),
    ])
    assert detect_decisions(t) == []


def test_missing_transcript_is_silent(tmp_path):
    assert detect_decisions(str(tmp_path / "nope.jsonl")) == []


def test_caps_and_prefers_the_end_of_the_session(tmp_path):
    turns = [("assistant", "Decision: choice number %d is what we are going with here." % i)
             for i in range(12)]
    got = detect_decisions(_transcript(tmp_path, turns))
    assert len(got) == 5, "a single session must not flood the store"
    # Read from the end backwards: the last decisions are the ones that stuck.
    assert "11" in got[0]["chosen"]


def test_deduplicates_against_what_is_already_stored(tmp_path):
    t = _transcript(tmp_path, [
        ("assistant", "Decision: keep the journal write-ahead ordering as it is."),
    ])
    got = detect_decisions(t)
    assert len(got) == 1
    existing = [{"chosen": got[0]["chosen"]}]
    # A resumed session replays earlier turns, so this runs against overlapping
    # transcripts more than once.
    assert deduplicate_decisions(got, existing) == []
    assert len(deduplicate_decisions(got, [])) == 1
