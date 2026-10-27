"""Mechanical PRD status: is docs/prd.md planning-ready?

Planning-ready (spec §4.2): section 10 exists and holds no NEEDS-DECISION item,
and every FR has at least one acceptance check. Stdlib only.
"""

from __future__ import annotations

import re
from pathlib import Path

PRD_REL = "docs/prd.md"
H2_RE = re.compile(r"^## (\d+)[.)]?\s+(.*)$")
FR_RE = re.compile(r"^### (FR-\d+)\b")
CHECKS_HEAD = "**Acceptance checks**"
ITEM_RE = re.compile(r"^\s*[-*]\s+(\[[ xX]\]\s*)?\S")
TAG_ITEM_RE = re.compile(r"^\s*[-*]\s+(\[[ xX]\]\s*)?NEEDS-DECISION\b")


def _sections(lines: list[str]) -> dict[int, list[str]]:
    out: dict[int, list[str]] = {}
    cur: int | None = None
    for ln in lines:
        m = H2_RE.match(ln)
        if m:
            cur = int(m.group(1))
            out[cur] = []
            continue
        if cur is not None:
            out[cur].append(ln)
    return out


def _frs(section: list[str]) -> tuple[list[str], list[str]]:
    frs: list[str] = []
    without: list[str] = []
    cur: str | None = None
    checks = 0
    in_checks = False
    for ln in section:
        m = FR_RE.match(ln)
        if m:
            if cur is not None and checks == 0:
                without.append(cur)
            cur, checks, in_checks = m.group(1), 0, False
            frs.append(cur)
            continue
        if cur is None:
            continue
        if ln.strip().startswith(CHECKS_HEAD):
            in_checks = True
        elif in_checks and ITEM_RE.match(ln):
            checks += 1
        elif in_checks and ln.strip():
            in_checks = False
    if cur is not None and checks == 0:
        without.append(cur)
    return frs, without


def open_decision_lines(text: str) -> list[str]:
    """The open NEEDS-DECISION lines of section 10 (stripped), in file order."""
    secs = _sections(text.replace("\r\n", "\n").split("\n"))
    return [ln.strip() for ln in secs.get(10, []) if TAG_ITEM_RE.match(ln)]


def prd_status(root: Path) -> dict:
    base = {"exists": False, "path": PRD_REL, "frs": 0, "frs_without_checks": [],
            "open_decisions": 0, "has_section_10": False, "planning_ready": False}
    try:
        text = (Path(root) / PRD_REL).read_text(encoding="utf-8-sig", errors="replace")
    except OSError:
        return base
    secs = _sections(text.replace("\r\n", "\n").split("\n"))
    frs, without = _frs(secs.get(4, []))
    has10 = 10 in secs
    open_items = sum(1 for ln in secs.get(10, []) if TAG_ITEM_RE.match(ln)) if has10 else 0
    ready = has10 and open_items == 0 and bool(frs) and not without
    return {"exists": True, "path": PRD_REL, "frs": len(frs), "frs_without_checks": without,
            "open_decisions": open_items, "has_section_10": has10, "planning_ready": ready}
