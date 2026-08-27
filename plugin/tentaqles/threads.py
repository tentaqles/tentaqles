"""Open-thread detection from conversation transcripts."""

from __future__ import annotations

import json
import re
from pathlib import Path

try:
    from tentaqles.privacy import redact_text as _redact_text
except Exception:  # pragma: no cover - graceful fallback
    _redact_text = None


def _safe_redact(text: str) -> str:
    """Apply redact_text if available; fall back to identity on any failure."""
    if _redact_text is None or not text:
        return text
    try:
        result = _redact_text(text)
        if isinstance(result, tuple):
            return result[0]
        return result
    except Exception:
        return text


# Pattern library — detects "unfinished work" phrases in human-turn messages.
# Ordered from most specific to most generic; dedup logic prefers earlier (more specific) matches.
OPEN_THREAD_PATTERNS: list[re.Pattern] = [
    re.compile(r"\bTODO\b"),
    re.compile(r"\bFIXME\b"),
    re.compile(r"\bHACK\b"),
    re.compile(r"\bcome back to this\b", re.IGNORECASE),
    re.compile(r"\bstill need(?:s)? to\b", re.IGNORECASE),
    re.compile(r"\bneed to (figure|revisit|fix|check|investigate)\b", re.IGNORECASE),
    re.compile(r"\bnext (session|time)\b", re.IGNORECASE),
    re.compile(r"\bfollow[- ]?up\b", re.IGNORECASE),
    re.compile(r"\bleft (unresolved|open|incomplete)\b", re.IGNORECASE),
    re.compile(r"\bopen question\b", re.IGNORECASE),
    re.compile(r"\bI'?ll come back\b", re.IGNORECASE),
    re.compile(r"\bdefer(?:red)? (to|for) later\b", re.IGNORECASE),
]

PRIORITY_BUMP_PATTERNS: list[re.Pattern] = [
    re.compile(r"\burgent\b", re.IGNORECASE),
    re.compile(r"\bcritical\b", re.IGNORECASE),
    re.compile(r"\bblocking\b", re.IGNORECASE),
    re.compile(r"\basap\b", re.IGNORECASE),
]


def _extract_human_text(transcript_path: str) -> list[tuple[int, str]]:
    """Parse JSONL transcript, return list of (turn_index, text) from human messages only.

    Handles two content formats:
    - entry['content'] as a string
    - entry['content'] as a list of {type, text} dicts
    Skips malformed JSON lines silently. Returns [] if file is missing/unreadable.
    """
    path = Path(transcript_path)
    if not path.exists() or not path.is_file():
        return []
    out: list[tuple[int, str]] = []
    try:
        with path.open("r", encoding="utf-8") as fh:
            lines = fh.readlines()
    except OSError:
        return []

    for idx, raw in enumerate(lines):
        raw = raw.strip()
        if not raw:
            continue
        try:
            entry = json.loads(raw)
        except (json.JSONDecodeError, ValueError):
            continue
        if not isinstance(entry, dict):
            continue
        if entry.get("type") != "human":
            continue
        content = entry.get("content", "")
        text = ""
        if isinstance(content, str):
            text = content
        elif isinstance(content, list):
            parts: list[str] = []
            for block in content:
                if isinstance(block, dict):
                    t = block.get("text")
                    if isinstance(t, str):
                        parts.append(t)
                elif isinstance(block, str):
                    parts.append(block)
            text = "\n".join(parts)
        if text:
            out.append((idx, text))
    return out


def _context_snippet(text: str, match_start: int, match_end: int, window: int = 100) -> str:
    """Return ~200 chars of context around a match, trimmed to clean boundaries."""
    start = max(0, match_start - window)
    end = min(len(text), match_end + window)
    snippet = text[start:end]
    # Trim to word boundaries if we cut mid-word on either side
    if start > 0:
        space = snippet.find(" ")
        if 0 < space < 20:
            snippet = snippet[space + 1 :]
    if end < len(text):
        space = snippet.rfind(" ")
        if space > len(snippet) - 20 and space > 0:
            snippet = snippet[:space]
    return snippet.strip()


def detect_open_threads(
    transcript_path: str,
    extra_patterns: list[str] | None = None,
) -> list[dict]:
    """Scan a JSONL transcript for open-thread phrases in human messages."""
    human_messages = _extract_human_text(transcript_path)
    if not human_messages:
        return []

    patterns: list[tuple[str, re.Pattern]] = [
        (p.pattern, p) for p in OPEN_THREAD_PATTERNS
    ]
    if extra_patterns:
        for ep in extra_patterns:
            try:
                patterns.append((ep, re.compile(ep, re.IGNORECASE)))
            except re.error:
                continue

    results: list[dict] = []

    for turn_index, text in human_messages:
        # For each turn, collect matches per pattern, then dedup: keep only
        # one result per turn (the most-specific / earliest pattern match).
        best: tuple[int, int, int, str] | None = None  # (pattern_order, match_start, match_end, pattern_name)
        for order, (name, pattern) in enumerate(patterns):
            m = pattern.search(text)
            if m is None:
                continue
            if best is None or order < best[0]:
                best = (order, m.start(), m.end(), name)

        if best is None:
            continue

        _order, m_start, m_end, pattern_name = best
        raw_context = _context_snippet(text, m_start, m_end)

        # Priority bump: scan context (or full text) for urgency markers
        priority = "medium"
        scan_target = text
        for bump in PRIORITY_BUMP_PATTERNS:
            if bump.search(scan_target):
                priority = "critical"
                break

        description = raw_context[:150]

        # Apply redaction
        description = _safe_redact(description)
        raw_context_redacted = _safe_redact(raw_context)

        results.append(
            {
                "description": description,
                "priority": priority,
                "source_turn": turn_index,
                "raw_text": raw_context_redacted,
                "pattern": pattern_name,
            }
        )

    return results


def _jaccard(a: str, b: str) -> float:
    """Token-based Jaccard similarity. Tokenize by non-word split, lowercase."""
    tokens_a = {t for t in re.split(r"\W+", a.lower()) if t}
    tokens_b = {t for t in re.split(r"\W+", b.lower()) if t}
    if not tokens_a and not tokens_b:
        return 1.0
    if not tokens_a or not tokens_b:
        return 0.0
    inter = tokens_a & tokens_b
    union = tokens_a | tokens_b
    return len(inter) / len(union)


def deduplicate_pending(
    candidates: list[dict],
    existing: list[dict],
    similarity_threshold: float = 0.8,
) -> list[dict]:
    """Filter out candidates too similar to existing pending items.

    Uses Jaccard token similarity (no embedding dependency).
    Caps existing at 50 most recent to bound cost.
    """
    if not candidates:
        return []
    capped = existing[-50:] if existing else []
    existing_descs = [
        e.get("description", "") for e in capped if isinstance(e, dict)
    ]

    out: list[dict] = []
    for cand in candidates:
        desc = cand.get("description", "") if isinstance(cand, dict) else ""
        duplicate = False
        for ex_desc in existing_descs:
            if _jaccard(desc, ex_desc) >= similarity_threshold:
                duplicate = True
                break
        if not duplicate:
            out.append(cand)
    return out
