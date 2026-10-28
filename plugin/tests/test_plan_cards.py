# plugin/tests/test_plan_cards.py
"""PRD to cards: the FR and section-11 parsers, the card-id writer, and plan-cards.py against a fake tq."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from tentaqles.workflow.prd import (
    card_body,
    fr_cards,
    normalize_release,
    parse_frs,
    parse_releases,
    record_cards,
    release_frs,
)

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "plan-cards.py"
FIXTURES = Path(__file__).resolve().parent / "fixtures"
DEMO = (FIXTURES / "prd_demo.md").read_text(encoding="utf-8")
PARENT = "c-parent000001"


# --- FR parser -------------------------------------------------------------


def test_parse_frs_reads_id_title_body_checks() -> None:
    frs = parse_frs(DEMO)
    assert [f["id"] for f in frs] == ["FR-1", "FR-2", "FR-3", "FR-4", "FR-5", "FR-6"]
    fr1 = frs[0]
    assert fr1["title"] == "Add a todo"
    assert fr1["body"].startswith("`todo add <text>` appends a todo")
    assert "Acceptance checks" not in fr1["body"]
    assert fr1["checks"] == [
        '`todo add "buy milk"` prints `1` on an empty store and exits 0.',
        "A second add prints `2`; ids never repeat even after a remove.",
        "An empty text exits 2 with `todo: add needs a text` on stderr and stores nothing.",
    ]
    assert len(frs[3]["checks"]) == 2


def test_parse_frs_template_shape_and_checkboxes() -> None:
    text = ("## 4. Functional requirements\n### FR-01 Export CSV\nWhy: g1\n**Acceptance checks**\n"
            "- [ ] ten rows give 11 lines\n- [x] the header is first\n### FR-02: Delete account\n"
            "**Acceptance checks**\n- the row is gone\n## 5. Data\n### FR-09 Not a requirement\n")
    frs = parse_frs(text)
    assert [(f["id"], f["title"]) for f in frs] == [("FR-01", "Export CSV"), ("FR-02", "Delete account")]
    assert frs[0]["body"] == "Why: g1"
    assert frs[0]["checks"] == ["ten rows give 11 lines", "the header is first"]
    assert frs[1]["body"] == ""


def test_parse_frs_crlf_and_bom() -> None:
    text = "﻿" + DEMO.replace("\n", "\r\n")
    frs = parse_frs(text)
    assert len(frs) == 6 and not any("\r" in c for f in frs for c in f["checks"])


# --- releases ----------------------------------------------------------------


@pytest.mark.parametrize("raw,want", [("1", "1"), ("Release 1", "1"), ("release-1", "1"), ("R1", "1"),
                                      ("**Release 2**", "2"), ("MVP", "mvp"), (" release 1.1 ", "1.1")])
def test_normalize_release(raw: str, want: str) -> None:
    assert normalize_release(raw) == want


def test_bullet_releases_expand_ranges() -> None:
    rels = parse_releases(DEMO)
    assert [(r["key"], r["frs"]) for r in rels] == [
        ("1", ["FR-1", "FR-2", "FR-3", "FR-4", "FR-5"]), ("2", ["FR-6"])]
    assert release_frs(DEMO, "1") == ["FR-1", "FR-2", "FR-3", "FR-4", "FR-5"]
    assert release_frs(DEMO, "release-2") == ["FR-6"]
    assert release_frs(DEMO, "all") == ["FR-1", "FR-2", "FR-3", "FR-4", "FR-5", "FR-6"]


@pytest.mark.parametrize("spec,want", [
    ("FR-1, FR-3 and FR-6", ["FR-1", "FR-3", "FR-6"]),
    ("FR-2..FR-4", ["FR-2", "FR-3", "FR-4"]),
    ("FR-2–FR-4", ["FR-2", "FR-3", "FR-4"]),
    ("FR-1 through FR-3, FR-6", ["FR-1", "FR-2", "FR-3", "FR-6"]),
    ("FR-4 to 6", ["FR-4", "FR-5", "FR-6"]),
    ("FR-01 to FR-03", ["FR-1", "FR-2", "FR-3"]),
    ("FR-3, FR-1, FR-3", ["FR-3", "FR-1"]),
])
def test_release_lists_and_ranges(spec: str, want: list[str]) -> None:
    text = DEMO.split("## 11. Releases")[0] + "## 11. Releases\n- Release 3: " + spec + ".\n"
    assert release_frs(text, "3") == want


def test_table_releases_map_to_section_4_spelling() -> None:
    text = ("## 4. Functional requirements\n### FR-01 A\n**Acceptance checks**\n- a\n"
            "### FR-02 B\n**Acceptance checks**\n- b\n### FR-03 C\n**Acceptance checks**\n- c\n"
            "## 11. Releases\n| Release | FRs | Done when |\n|---|---|---|\n"
            "| 1 | FR-1 to FR-2 | it ships |\n| Release 2 | FR-03 | later |\n")
    assert release_frs(text, "1") == ["FR-01", "FR-02"]
    assert release_frs(text, "2") == ["FR-03"]
    assert [r["key"] for r in parse_releases(text)] == ["1", "2"]


def test_unknown_release_and_unknown_fr() -> None:
    with pytest.raises(KeyError, match="release 9"):
        release_frs(DEMO, "9")
    text = DEMO.replace("Release 2: FR-6", "Release 2: FR-6, FR-8")
    with pytest.raises(ValueError, match="FR-8"):
        release_frs(text, "2")


def test_lines_without_a_release_label_are_not_releases() -> None:
    text = DEMO.replace("- Release 2: FR-6, built through the feature path.",
                        "- FR-6 (edit a todo): its own hub card (c-g3zhbrwruaao), built through the feature path.")
    assert [r["key"] for r in parse_releases(text)] == ["1"]


# --- card ids in section 11 ------------------------------------------------


def test_inline_card_id_on_a_single_fr_line_counts() -> None:
    text = DEMO.replace("- Release 2: FR-6, built through the feature path.",
                        "- FR-6 (edit a todo): its own hub card (c-g3zhbrwruaao).")
    assert fr_cards(text) == {"FR-6": "c-g3zhbrwruaao"}
    # A line naming several FRs does not say which one the card is.
    assert fr_cards(DEMO.replace("FR-1 to FR-5, built", "FR-1 to FR-5 (project c-g3zhbrwruaao), built")) == {}


def test_record_cards_is_idempotent_and_merges() -> None:
    once = record_cards(DEMO, {"FR-2": "c-bbbbbbbbbbbb", "FR-1": "c-aaaaaaaaaaaa"})
    assert record_cards(once, {"FR-1": "c-aaaaaaaaaaaa"}) == once
    assert fr_cards(once) == {"FR-1": "c-aaaaaaaaaaaa", "FR-2": "c-bbbbbbbbbbbb"}
    sec11 = once.split("## 11. Releases", 1)[1]
    assert sec11.index("- FR-1: c-aaaaaaaaaaaa") < sec11.index("- FR-2: c-bbbbbbbbbbbb")
    twice = record_cards(once, {"FR-6": "c-ffffffffffff"})
    assert twice.count("<!-- tq:cards -->") == 1 and twice.count("<!-- /tq:cards -->") == 1
    assert fr_cards(twice) == {"FR-1": "c-aaaaaaaaaaaa", "FR-2": "c-bbbbbbbbbbbb", "FR-6": "c-ffffffffffff"}
    # The rest of the document is untouched, and the releases still parse.
    assert twice.split("## 11. Releases")[0] == DEMO.split("## 11. Releases")[0]
    assert release_frs(twice, "1") == ["FR-1", "FR-2", "FR-3", "FR-4", "FR-5"]


def test_record_cards_keeps_following_sections_and_line_endings() -> None:
    text = DEMO.replace("## 11. Releases", "## 11. Releases").replace("\n", "\r\n") + "## 12. Appendix\r\nx\r\n"
    out = record_cards(text, {"FR-3": "c-cccccccccccc"})
    assert "\n" not in out.replace("\r\n", "")
    assert out.index("<!-- /tq:cards -->") < out.index("## 12. Appendix")
    assert out.endswith("## 12. Appendix\r\nx\r\n")


def test_record_cards_needs_section_11() -> None:
    with pytest.raises(ValueError, match="11"):
        record_cards("## 4. Functional requirements\n### FR-1 A\n", {"FR-1": "c-aaaaaaaaaaaa"})


def test_card_body_carries_text_checks_and_source() -> None:
    fr = parse_frs(DEMO)[3]
    body = card_body(fr, "docs/prd.md", "1")
    assert body.startswith("`todo remove <id>` deletes the todo from the store.")
    assert "**Acceptance checks**\n- `todo remove 2` exits 0" in body
    assert "PRD docs/prd.md §4 FR-4, release 1" in body


# --- plan-cards.py ---------------------------------------------------------


@pytest.fixture
def fake_tq(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    """Put a fake tq first on PATH (never replacing PATH) and return its call log."""
    bindir = tmp_path / "bin"
    bindir.mkdir()
    fake = FIXTURES / "fake_tq.py"
    if sys.platform == "win32":
        (bindir / "tq.cmd").write_text(f'@echo off\r\n"{sys.executable}" "{fake}" %*\r\n', encoding="utf-8")
    else:
        sh = bindir / "tq"
        sh.write_text(f'#!/bin/sh\nexec "{sys.executable}" "{fake}" "$@"\n', encoding="utf-8")
        sh.chmod(0o755)
    log = tmp_path / "calls.jsonl"
    monkeypatch.setenv("PATH", str(bindir) + os.pathsep + os.environ.get("PATH", ""))
    monkeypatch.setenv("FAKE_TQ_LOG", str(log))
    monkeypatch.delenv("FAKE_TQ_FAIL_AT", raising=False)
    monkeypatch.delenv("TQ_BIN", raising=False)
    for k in ("HOME", "USERPROFILE", "TQ_HOME"):
        monkeypatch.setenv(k, str(tmp_path / "home"))
    return log


def _calls(log: Path) -> list[dict]:
    if not log.exists():
        return []
    return [json.loads(ln) for ln in log.read_text(encoding="utf-8").splitlines() if ln.strip()]


def _project(tmp_path: Path, text: str = DEMO) -> Path:
    proj = tmp_path / "demo-todo"
    (proj / "docs").mkdir(parents=True)
    prd = proj / "docs" / "prd.md"
    prd.write_text(text, encoding="utf-8")
    return prd


def _run(prd: Path, *extra: str) -> tuple[int, dict, str]:
    p = subprocess.run([sys.executable, str(SCRIPT), "--prd", str(prd), "--parent", PARENT, "--project", "demo-todo",
                        *extra], capture_output=True, text=True, timeout=120, env=os.environ.copy())
    out = json.loads(p.stdout) if p.stdout.strip().startswith("{") else {}
    return p.returncode, out, p.stderr


def test_plan_cards_release_1_then_2_makes_six_cards_and_is_idempotent(tmp_path: Path, fake_tq: Path) -> None:
    prd = _project(tmp_path)
    code, out, err = _run(prd, "--release", "1")
    assert code == 0, err
    assert [c["fr"] for c in out["created"]] == ["FR-1", "FR-2", "FR-3", "FR-4", "FR-5"]
    calls = _calls(fake_tq)
    assert len(calls) == 5
    first = calls[0]["argv"]
    assert first[:3] == ["hub", "add", "FR-1 Add a todo"]
    for flag, want in (("--kind", "feature"), ("--parent", PARENT), ("--project", "demo-todo")):
        assert first[first.index(flag) + 1] == want
    assert "--json" in first and "--tag" not in first
    assert "**Acceptance checks**" in calls[0]["body"] and "`todo add \"buy milk\"`" in calls[0]["body"]
    assert all("agent-ready" not in " ".join(c["argv"]) for c in calls)

    code, out, err = _run(prd, "--release", "2")
    assert code == 0, err
    assert [c["card"] for c in out["created"]] == ["c-fake00000006"]
    assert fr_cards(prd.read_text(encoding="utf-8")) == {f"FR-{n}": "c-fake%08d" % n for n in range(1, 7)}

    before = prd.read_bytes()
    for rel in ("1", "2", "all"):
        code, out, err = _run(prd, "--release", rel)
        assert code == 0 and out["created"] == [], err
        assert len(out["skipped"]) == (5 if rel == "1" else 1 if rel == "2" else 6)
    assert len(_calls(fake_tq)) == 6 and prd.read_bytes() == before


def test_plan_cards_dry_run_calls_nothing_and_writes_nothing(tmp_path: Path, fake_tq: Path) -> None:
    prd = _project(tmp_path, record_cards(DEMO, {"FR-2": "c-bbbbbbbbbbbb"}))
    before = prd.read_bytes()
    code, out, err = _run(prd, "--release", "1", "--dry-run")
    assert code == 0, err
    assert out["dry_run"] is True
    assert [p["fr"] for p in out["planned"]] == ["FR-1", "FR-3", "FR-4", "FR-5"]
    assert out["planned"][0]["title"] == "FR-1 Add a todo"
    assert "--body-file" in out["planned"][0]["command"]
    assert out["skipped"] == [{"fr": "FR-2", "card": "c-bbbbbbbbbbbb"}]
    assert _calls(fake_tq) == [] and prd.read_bytes() == before


def test_plan_cards_records_each_card_as_it_goes(tmp_path: Path, fake_tq: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    prd = _project(tmp_path)
    monkeypatch.setenv("FAKE_TQ_FAIL_AT", "3")
    code, out, err = _run(prd, "--release", "1")
    assert code == 1 and "FR-3" in err and "not reachable" in err
    assert fr_cards(prd.read_text(encoding="utf-8")) == {"FR-1": "c-fake00000001", "FR-2": "c-fake00000002"}
    monkeypatch.delenv("FAKE_TQ_FAIL_AT")
    code, out, err = _run(prd, "--release", "1")
    assert code == 0, err
    assert [c["fr"] for c in out["created"]] == ["FR-3", "FR-4", "FR-5"]
    assert len(fr_cards(prd.read_text(encoding="utf-8"))) == 5


def test_plan_cards_skips_an_fr_with_an_inline_card(tmp_path: Path, fake_tq: Path) -> None:
    text = DEMO.replace("- Release 2: FR-6, built through the feature path.",
                        "- Release 2: FR-6, built through the feature path.\n- FR-6 has its own hub card (c-g3zhbrwruaao).")
    prd = _project(tmp_path, text)
    code, out, err = _run(prd, "--release", "2")
    assert code == 0 and out["created"] == [] and out["skipped"] == [{"fr": "FR-6", "card": "c-g3zhbrwruaao"}], err
    assert _calls(fake_tq) == []


def test_plan_cards_preserves_crlf_and_bom(tmp_path: Path, fake_tq: Path) -> None:
    prd = _project(tmp_path)
    prd.write_bytes(b"\xef\xbb\xbf" + DEMO.replace("\n", "\r\n").encode("utf-8"))
    code, _, err = _run(prd, "--release", "2")
    assert code == 0, err
    raw = prd.read_bytes()
    assert raw.startswith(b"\xef\xbb\xbf") and b"\n" not in raw.replace(b"\r\n", b"")
    assert b"- FR-6: c-fake00000001" in raw


@pytest.mark.parametrize("args,needle", [
    (["--release", "9"], "release 9"),
    (["--release", "1", "--parent", "not-a-card"], "--parent"),
    (["--release", "1", "--project", "../outside"], "--project"),
])
def test_plan_cards_usage_errors(tmp_path: Path, fake_tq: Path, args: list[str], needle: str) -> None:
    prd = _project(tmp_path)
    code, _, err = _run(prd, *args)
    assert code == 2 and needle in err
    assert _calls(fake_tq) == []


def test_plan_cards_without_a_prd(tmp_path: Path, fake_tq: Path) -> None:
    code, _, err = _run(tmp_path / "docs" / "prd.md", "--release", "1")
    assert code == 2 and "prd" in err.lower()


def test_plan_cards_refuses_a_prd_that_is_not_planning_ready(tmp_path: Path, fake_tq: Path) -> None:
    prd = _project(tmp_path, DEMO.replace("- DECIDED (accepted", "- NEEDS-DECISION: which store? (settle by: ask tester)\n- DECIDED (accepted"))
    code, _, err = _run(prd, "--release", "1")
    assert code == 3 and "planning-ready" in err
    assert _calls(fake_tq) == []
