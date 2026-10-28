"""Mechanical PRD status: is docs/prd.md planning-ready? And PRD to cards.

Planning-ready (spec §4.2): section 10 exists and holds no NEEDS-DECISION item,
and every FR has at least one acceptance check. The second half parses the §4
FRs and the §11 release membership, and records hub card ids back into §11
(spec B §9, used by scripts/plan-cards.py). Stdlib only.
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
    return [normalize_decision(ln) for ln in secs.get(10, []) if TAG_ITEM_RE.match(ln)]


_LEAD_RE = re.compile(r"^\s*[-*]\s+(\[[ xX]\]\s*)?")


def normalize_decision(line: str) -> str:
    """A decision line without its bullet, checkbox and outer/duplicate whitespace, so a
    marker or spacing change does not make it look new. Wording is kept."""
    return " ".join(_LEAD_RE.sub("", line, count=1).split())


def prd_status(root: Path) -> dict:
    base = {"exists": False, "path": PRD_REL, "frs": 0, "frs_without_checks": [],
            "open_decisions": 0, "has_section_10": False, "planning_ready": False}
    try:
        text = (Path(root) / PRD_REL).read_text(encoding="utf-8-sig", errors="replace")
    except OSError:
        return base
    return {"exists": True, "path": PRD_REL, **text_status(text)}


def text_status(text: str) -> dict:
    """prd_status for PRD text already read: frs, frs_without_checks, open_decisions,
    has_section_10 and planning_ready."""
    secs = _sections(text.replace("\r\n", "\n").split("\n"))
    frs, without = _frs(secs.get(4, []))
    has10 = 10 in secs
    open_items = sum(1 for ln in secs.get(10, []) if TAG_ITEM_RE.match(ln)) if has10 else 0
    ready = has10 and open_items == 0 and bool(frs) and not without
    return {"frs": len(frs), "frs_without_checks": without, "open_decisions": open_items,
            "has_section_10": has10, "planning_ready": ready}


# --- PRD to cards (spec B §9: /tentaqles:plan-cards and /tentaqles:refine) ----------------
#
# parse_frs reads the §4 requirements, parse_releases and release_frs read §11 release
# membership ("Release 1: FR-1 to FR-5", lists, tables), fr_cards finds the hub card ids
# already recorded in §11, and record_cards writes new ones into one managed block there.

FR_HEAD_RE = re.compile(r"^### (FR-(\d+))\b[ \t]*[:.–—-]?[ \t]*(.*?)\s*$")
CARD_ID_RE = re.compile(r"\bc-[a-z0-9]{12}\b")
FR_REF_RE = re.compile(
    r"\bFR-(\d+)\b(?:\s*(?:to|through|thru|\.{2,3}|[–—-])\s*(?:FR-)?(\d+)\b)?", re.IGNORECASE)
CARDS_START = "<!-- tq:cards -->"
CARDS_END = "<!-- /tq:cards -->"
CARDS_NOTE = ("Hub cards (written by `/tentaqles:plan-cards`; an FR listed here is skipped "
              "on the next run):")
CARD_ENTRY_RE = re.compile(r"^\s*[-*]\s+(FR-\d+)\s*:\s*(c-[a-z0-9]{12})\b")
MAX_RANGE = 500


def _lines(text: str) -> list[str]:
    return text.lstrip("﻿").replace("\r\n", "\n").split("\n")


def parse_frs(text: str) -> list[dict]:
    """The §4 requirements in file order: {id, num, title, body, checks}. body is the FR text
    without its heading and acceptance checks; checks are the check items, bullet and checkbox
    removed. A non-FR ### heading ends the FR before it."""
    out: list[dict] = []
    cur: dict | None = None
    in_checks = False
    for ln in _sections(_lines(text)).get(4, []):
        m = FR_HEAD_RE.match(ln)
        if m:
            cur = {"id": m.group(1), "num": int(m.group(2)), "title": m.group(3), "body": [], "checks": []}
            out.append(cur)
            in_checks = False
            continue
        if cur is None:
            continue
        if ln.startswith("### "):
            cur, in_checks = None, False
            continue
        s = ln.strip()
        if s.startswith(CHECKS_HEAD):
            in_checks = True
            continue
        if in_checks:
            if ITEM_RE.match(ln):
                cur["checks"].append(_LEAD_RE.sub("", ln, count=1).strip())
                continue
            if not s:
                continue
            if ln[:1] in " \t" and cur["checks"]:  # an indented continuation of the last check
                cur["checks"][-1] += " " + s
                continue
            in_checks = False
        cur["body"].append(ln.rstrip())
    for fr in out:
        fr["body"] = "\n".join(fr["body"]).strip("\n").strip()
    return out


def normalize_release(name: str) -> str:
    """A release name as a key: "Release 1", "release-1", "R1" and "1" are all "1"."""
    s = name.strip().strip("*_` ").strip().lower()
    s = re.sub(r"^(?:release|rel\.?|r)(?=[\s#_-]*\d)[\s#_-]*", "", s)
    s = re.sub(r"^release[\s#_-]+", "", s)
    return s.strip(" :.*_`")


def _fr_nums(text: str) -> list[int]:
    nums: list[int] = []
    for m in FR_REF_RE.finditer(text):
        a = int(m.group(1))
        b = int(m.group(2)) if m.group(2) else a
        if b < a or b - a > MAX_RANGE:
            raise ValueError(f"§11: bad FR range {m.group(0)!r}")
        for n in range(a, b + 1):
            if n not in nums:
                nums.append(n)
    return nums


def _section_bounds(lines: list[str], num: int) -> tuple[int, int] | None:
    start = None
    for i, ln in enumerate(lines):
        m = H2_RE.match(ln)
        if not m:
            continue
        if start is not None:
            return start, i
        if int(m.group(1)) == num:
            start = i
    return (start, len(lines)) if start is not None else None


def _block_bounds(lines: list[str], lo: int, hi: int) -> tuple[int, int] | None:
    s = next((i for i in range(lo, hi) if lines[i].strip() == CARDS_START), None)
    if s is None:
        return None
    e = next((i for i in range(s + 1, hi) if lines[i].strip() == CARDS_END), None)
    return (s, e) if e is not None else None


def _section_11(text: str) -> tuple[list[str], tuple[int, int] | None]:
    """§11's lines outside the managed card block, and that block's own lines (or None)."""
    lines = _lines(text)
    b = _section_bounds(lines, 11)
    if b is None:
        return [], None
    blk = _block_bounds(lines, b[0] + 1, b[1])
    if blk is None:
        return lines[b[0] + 1:b[1]], None
    return lines[b[0] + 1:blk[0]] + lines[blk[1] + 1:b[1]], (blk[0], blk[1])


def _canon(text: str) -> dict[int, str]:
    return {fr["num"]: fr["id"] for fr in parse_frs(text)}


def _release_label(content: str) -> tuple[str, str] | None:
    m = re.search(r":|\s[–—-]\s", content)
    if not m:
        return None
    label = content[:m.start()]
    if not re.match(r"^[\s*_`]*(?:release\b|rel\.?\s*\d|r\d)", label, re.IGNORECASE):
        return None
    return label, content[m.end():]


def parse_releases(text: str) -> list[dict]:
    """§11 release rows in file order: {name, key, frs, line}. A row is a bullet whose label
    names a release ("- Release 1: FR-1 to FR-5, ...", "- **R2** - FR-6") or a table row
    ("| 1 | FR-1, FR-2 | ... |"). Ranges and lists are expanded; ids use the §4 spelling."""
    canon = _canon(text)
    lines, _ = _section_11(text)
    out: list[dict] = []
    for ln in lines:
        s = ln.strip()
        name = rest = None
        if s.startswith("|") and s.endswith("|"):
            cells = [c.strip() for c in s.strip("|").split("|")]
            if not cells or all(re.fullmatch(r":?-{2,}:?", c) for c in cells if c):
                continue
            if normalize_release(cells[0]) in ("", "release", "releases"):
                continue
            name, rest = cells[0], " ".join(cells[1:])
        else:
            m = re.match(r"^\s*[-*]\s+(.*)$", ln)
            lab = _release_label(m.group(1)) if m else None
            if lab is None:
                continue
            name, rest = lab
        key = normalize_release(name)
        if not key:
            continue
        frs = [canon.get(n, f"FR-{n}") for n in _fr_nums(rest)]
        out.append({"name": name.strip().strip("*_` ").strip(), "key": key, "frs": frs, "line": s})
    return out


def release_frs(text: str, release: str) -> list[str]:
    """The §4 FR ids of one release, in §11 order, duplicates dropped. "all" (when no release has
    that name) is every §4 FR. KeyError: no such release; ValueError: it names an FR §4 lacks."""
    key = normalize_release(release)
    rels = parse_releases(text)
    rows = [r for r in rels if r["key"] == key]
    if not rows and key == "all":
        return [fr["id"] for fr in parse_frs(text)]
    if not rows:
        names = ", ".join(dict.fromkeys(r["key"] for r in rels)) or "none"
        raise KeyError(f"no release {release} in PRD §11 (releases: {names})")
    known = set(_canon(text).values())
    ids: list[str] = []
    for r in rows:
        for fr in r["frs"]:
            if fr not in ids:
                ids.append(fr)
    missing = [fr for fr in ids if fr not in known]
    if missing:
        raise ValueError(f"release {release} names {', '.join(missing)}, which PRD §4 does not define")
    return ids


def fr_cards(text: str) -> dict[str, str]:
    """FR id -> hub card id already recorded in §11: the managed block's entries, plus any other
    §11 line that names exactly one FR and exactly one card id (a card filed by hand)."""
    canon = _canon(text)
    lines = _lines(text)
    out: dict[str, str] = {}
    rest, blk = _section_11(text)
    for ln in rest:
        cards = set(CARD_ID_RE.findall(ln))
        try:
            nums = _fr_nums(ln)
        except ValueError:
            continue
        if len(cards) == 1 and len(nums) == 1:
            out[canon.get(nums[0], f"FR-{nums[0]}")] = cards.pop()
    if blk is not None:
        for ln in lines[blk[0] + 1:blk[1]]:
            m = CARD_ENTRY_RE.match(ln)
            if m:
                n = int(m.group(1)[3:])
                out[canon.get(n, f"FR-{n}")] = m.group(2)
    return out


def record_cards(text: str, cards: dict[str, str]) -> str:
    """text with cards (FR id -> card id) merged into §11's managed block, created at the end of
    §11 on first use. Idempotent: recording what is already there returns text unchanged. Line
    endings and a BOM are kept. ValueError when the PRD has no §11 or a card id is malformed."""
    for fr, card in cards.items():
        if not re.fullmatch(r"FR-\d+", fr) or not CARD_ID_RE.fullmatch(card):
            raise ValueError(f"not an FR and card id pair: {fr!r} {card!r}")
    bom = "﻿" if text.startswith("﻿") else ""
    nl = "\r\n" if "\r\n" in text else "\n"
    lines = _lines(text)
    had_nl = lines[-1] == "" if lines else False
    if had_nl:
        lines = lines[:-1]
    b = _section_bounds(lines, 11)
    if b is None:
        raise ValueError("the PRD has no section 11 (Releases) to record card ids in")
    canon = _canon(text)
    entries: dict[int, tuple[str, str]] = {}
    blk = _block_bounds(lines, b[0] + 1, b[1])
    if blk is not None:
        for ln in lines[blk[0] + 1:blk[1]]:
            m = CARD_ENTRY_RE.match(ln)
            if m:
                n = int(m.group(1)[3:])
                entries[n] = (canon.get(n, m.group(1)), m.group(2))
    for fr, card in cards.items():
        n = int(fr[3:])
        entries[n] = (canon.get(n, fr), card)
    block = [CARDS_START, CARDS_NOTE] + [f"- {fr}: {card}" for _, (fr, card) in sorted(entries.items())] + [CARDS_END]
    if blk is not None:
        lines = lines[:blk[0]] + block + lines[blk[1] + 1:]
    else:
        head, sec, tail = lines[:b[0] + 1], lines[b[0] + 1:b[1]], lines[b[1]:]
        while sec and not sec[-1].strip():
            sec.pop()
        lines = head + sec + [""] + block + ([""] if tail else []) + tail
    return bom + nl.join(lines) + (nl if had_nl else "")


def card_body(fr: dict, prd_rel: str, release: str) -> str:
    """A feature card's body for one FR: its text, its acceptance checks and where it came from."""
    parts = []
    if fr["body"]:
        parts.append(fr["body"])
    if fr["checks"]:
        parts.append(CHECKS_HEAD + "\n" + "\n".join(f"- {c}" for c in fr["checks"]))
    parts.append(f"Source: PRD {prd_rel} §4 {fr['id']}, release {release}.")
    return "\n\n".join(parts) + "\n"
