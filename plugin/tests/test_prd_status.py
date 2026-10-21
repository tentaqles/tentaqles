# plugin/tests/test_prd_status.py
"""prd_status decides planning-ready mechanically from docs/prd.md."""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

from tentaqles.workflow.prd import prd_status

ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "prd-status.py"

READY = """# X: product requirements

## 1. Problem and evidence
p
## 4. Functional requirements
### FR-01 Export CSV
Why: g1
**Acceptance checks**
- a download of 10 rows produces 11 lines
### FR-02 Delete account
**Acceptance checks**
- the row is gone after 24h
## 10. Open questions
Each item starts with `NEEDS-DECISION`. Empty section = ready to plan.
## 11. Releases
"""


def _write(tmp_path: Path, text: str) -> Path:
    (tmp_path / "docs").mkdir(exist_ok=True)
    (tmp_path / "docs" / "prd.md").write_text(text, encoding="utf-8")
    return tmp_path


def test_ready_prd(tmp_path: Path) -> None:
    st = prd_status(_write(tmp_path, READY))
    assert st["exists"] and st["frs"] == 2 and st["frs_without_checks"] == []
    assert st["open_decisions"] == 0 and st["has_section_10"] and st["planning_ready"]


def test_open_decision_blocks(tmp_path: Path) -> None:
    text = READY.replace("## 11. Releases", "- NEEDS-DECISION: which DB? (settle by: research)\n## 11. Releases")
    st = prd_status(_write(tmp_path, text))
    assert st["open_decisions"] == 1 and not st["planning_ready"]


def test_fr_without_checks_blocks(tmp_path: Path) -> None:
    text = READY.replace("**Acceptance checks**\n- the row is gone after 24h\n", "")
    st = prd_status(_write(tmp_path, text))
    assert st["frs_without_checks"] == ["FR-02"] and not st["planning_ready"]


def test_prd_section_10_heading_variants(tmp_path: Path) -> None:
    for head in ("## 10 Open questions", "## 10. Open Questions", "## 10) Open questions"):
        st = prd_status(_write(tmp_path, READY.replace("## 10. Open questions", head)))
        assert st["has_section_10"], head


def test_prd_without_section_10_is_not_ready(tmp_path: Path) -> None:
    text = READY.replace("## 10. Open questions\nEach item starts with `NEEDS-DECISION`. Empty section = ready to plan.\n", "")
    st = prd_status(_write(tmp_path, text))
    assert not st["has_section_10"] and not st["planning_ready"]


def test_tag_in_prose_does_not_count(tmp_path: Path) -> None:
    text = READY.replace(
        "## 11. Releases",
        "Prose mentioning NEEDS-DECISION in a sentence.\n"
        "- Items are tagged `NEEDS-DECISION` when open\n"
        "- A bullet that says NEEDS-DECISION later\n## 11. Releases")
    assert prd_status(_write(tmp_path, text))["open_decisions"] == 0


def test_checks_heading_with_only_blanks_counts_as_none(tmp_path: Path) -> None:
    text = READY.replace("**Acceptance checks**\n- the row is gone after 24h\n", "**Acceptance checks**\n\n\n")
    st = prd_status(_write(tmp_path, text))
    assert st["frs_without_checks"] == ["FR-02"] and not st["planning_ready"]


def test_prose_interrupting_checks_list_stops_counting(tmp_path: Path) -> None:
    text = READY.replace("**Acceptance checks**\n- the row is gone after 24h\n",
                         "**Acceptance checks**\nsome prose first\n- the row is gone after 24h\n")
    st = prd_status(_write(tmp_path, text))
    assert st["frs_without_checks"] == ["FR-02"]


def test_fr_heading_outside_section_4_is_ignored(tmp_path: Path) -> None:
    text = READY.replace("## 11. Releases", "## 11. Releases\n### FR-09 Not a requirement\n")
    st = prd_status(_write(tmp_path, text))
    assert st["frs"] == 2 and st["frs_without_checks"] == [] and st["planning_ready"]


def test_checkbox_items_count_as_checks(tmp_path: Path) -> None:
    text = READY.replace("- the row is gone after 24h", "- [ ] the row is gone after 24h")
    st = prd_status(_write(tmp_path, text))
    assert st["frs_without_checks"] == [] and st["planning_ready"]


def test_crlf_and_bom_prd(tmp_path: Path) -> None:
    (tmp_path / "docs").mkdir()
    (tmp_path / "docs" / "prd.md").write_bytes(b"\xef\xbb\xbf" + READY.replace("\n", "\r\n").encode("utf-8"))
    st = prd_status(tmp_path)
    assert st["exists"] and st["frs"] == 2 and st["planning_ready"]


def test_non_utf8_prd_is_not_ready_and_does_not_raise(tmp_path: Path) -> None:
    (tmp_path / "docs").mkdir()
    (tmp_path / "docs" / "prd.md").write_bytes(b"# X\n\n## 1. Problem\ncaf\xe9\n")
    st = prd_status(tmp_path)
    assert st["exists"] and not st["planning_ready"]


def test_no_prd(tmp_path: Path) -> None:
    st = prd_status(tmp_path)
    assert st == {"exists": False, "path": "docs/prd.md", "frs": 0, "frs_without_checks": [],
                  "open_decisions": 0, "has_section_10": False, "planning_ready": False}


def test_cli_exit_codes(tmp_path: Path) -> None:
    def run(cwd: Path) -> tuple[int, dict]:
        p = subprocess.run([sys.executable, str(SCRIPT), "--cwd", str(cwd)], capture_output=True, text=True, timeout=60)
        return p.returncode, json.loads(p.stdout)
    code, out = run(tmp_path)
    assert code == 4 and not out["exists"]
    code, out = run(_write(tmp_path, READY))
    assert code == 0 and out["planning_ready"]
    code, _ = run(_write(tmp_path, READY.replace("## 11. Releases", "- NEEDS-DECISION: x\n## 11. Releases")))
    assert code == 3
