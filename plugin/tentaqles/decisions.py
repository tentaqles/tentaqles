"""Detect decisions in a conversation transcript.

Sessions and pending items were already captured automatically at session end;
decisions were not. They were written only when the session-wrap skill ran,
which happens only if someone remembers to say "done" before closing the
terminal. On this machine that meant nineteen days in which every session was
logged and not one reason for anything was -- the what recorded, the why lost.

This mirrors threads.detect_open_threads: same transcript, same session-end
hook, same never-crash discipline. It fires when the terminal closes, so
nothing has to be remembered.

Precision is chosen over recall, deliberately. A first version also matched
directives like "use X instead of Y", and against real transcripts it produced
mid-sentence fragments -- "the brand's actual vocabulary, rhythm, and
connectors" -- stored as decisions. Writing that into a store somebody relies
on is worse than writing nothing, so this now captures a decision only where
someone said outright that they were making one. Most sessions will yield
none, and that is the correct outcome: the model-driven capture in the Stop
hook is what produces the good entries.

Everything written here is marked confidence "low" and tagged "auto", so a
regex guess is never mistaken for a decision a person chose to record.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

# Group 1 is the decision itself in every pattern: the caller takes m.group(1),
# so a pattern that captures something else silently records nonsense.
DECISION_PATTERNS: list[re.Pattern] = [
    re.compile(
        r"(?:\A|\n)\s*(?:\*\*)?(?:ruling|decision|decided)(?:\*\*)?\s*:\s*(.{15,240})",
        re.IGNORECASE,
    ),
    re.compile(
        r"(?:\A|\n)\s*(?:\*\*)?(?:decisão|decisao|decidido)(?:\*\*)?\s*:\s*(.{15,240})",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:I|we)\s+(?:have\s+)?(?:decided to|chose to|settled on)\s+(.{15,240})",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:decidi|decidimos|optamos por|ficou decidido)\s+(.{15,240})",
        re.IGNORECASE,
    ),
]

# Why a choice was made. Group 1 is the reason.
RATIONALE_PATTERNS: list[re.Pattern] = [
    re.compile(r"\b(?:because|since|so that|given that)\s+(.{10,240})", re.IGNORECASE),
    re.compile(
        r"\b(?:porque|pois|já que|uma vez que|visto que)\s+(.{10,240})",
        re.IGNORECASE,
    ),
]

# Text that looks like a decision but is not: questions, hypotheticals, and
# proposals. Without these, "should we use X instead of Y?" is recorded as a
# decision to use X.
REJECT_PATTERNS: list[re.Pattern] = [
    re.compile(r"\?\s*$"),
    re.compile(
        r"\b(?:should we|shall we|do you want|would you like|devo|devemos|quer que)\b",
        re.IGNORECASE,
    ),
    re.compile(
        r"\b(?:if we|we could|we might|one option|alternatively|poderíamos|talvez)\b",
        re.IGNORECASE,
    ),
]

MAX_PER_SESSION = 5
MIN_LEN = 15


def _messages(transcript_path: str) -> list[tuple[int, str, str]]:
    """Return (turn_index, role, text) for user and assistant messages.

    Open-thread detection reads only what the human typed. A decision is
    usually stated by whoever concluded it, which is as often the assistant, so
    both roles are read and the role is kept on the result.
    """
    path = Path(transcript_path)
    if not path.exists() or not path.is_file():
        return []
    out: list[tuple[int, str, str]] = []
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as fh:
            for i, line in enumerate(fh):
                line = line.strip()
                if not line:
                    continue
                try:
                    entry = json.loads(line)
                except (json.JSONDecodeError, ValueError):
                    continue
                msg = entry.get("message") or {}
                role = entry.get("type") or msg.get("role") or ""
                if role not in ("user", "assistant"):
                    continue
                content = msg.get("content", entry.get("content"))
                text = ""
                if isinstance(content, str):
                    text = content
                elif isinstance(content, list):
                    text = " ".join(
                        c.get("text", "")
                        for c in content
                        if isinstance(c, dict) and c.get("type") == "text"
                    )
                text = text.strip()
                if text:
                    out.append((i, role, text))
    except OSError:
        return []
    return out


def _clean(s: str) -> str:
    """Trim a captured span to something worth storing."""
    s = re.sub(r"\s+", " ", s).strip()
    m = re.search(r"[.;!?]\s", s)
    if m and m.start() >= MIN_LEN:
        s = s[: m.start()]
    return s.strip(" .,:;-—")


def _rejected(text: str) -> bool:
    return any(p.search(text) for p in REJECT_PATTERNS)


def detect_decisions(transcript_path: str) -> list[dict]:
    """Scan a transcript for explicitly stated decisions.

    Returns dicts of {chosen, rationale, confidence, tags, role}, at most
    MAX_PER_SESSION, most recent first -- the end of a session is where the
    conclusions are, and an early exploratory aside is rarely the decision.
    """
    msgs = _messages(transcript_path)
    if not msgs:
        return []

    found: list[dict] = []
    seen: set[str] = set()

    for _turn, role, text in reversed(msgs):
        if len(found) >= MAX_PER_SESSION:
            break
        # Paragraph by paragraph: a whole turn is far too much context for one
        # regex, and a single reject phrase would suppress a good match
        # elsewhere in the same turn.
        for para in re.split(r"\n\s*\n", text):
            if len(found) >= MAX_PER_SESSION:
                break
            para = para.strip()
            if len(para) < MIN_LEN or _rejected(para):
                continue
            for pat in DECISION_PATTERNS:
                m = pat.search(para)
                if not m:
                    continue
                chosen = _clean(m.group(1))
                if len(chosen) < MIN_LEN:
                    continue
                key = chosen.lower()[:80]
                if key in seen:
                    continue
                rationale = ""
                for rp in RATIONALE_PATTERNS:
                    rm = rp.search(para)
                    if rm:
                        rationale = _clean(rm.group(1))
                        break
                seen.add(key)
                found.append(
                    {
                        "chosen": chosen,
                        "rationale": rationale,
                        "confidence": "low",
                        "tags": ["auto", "src:" + role],
                        "role": role,
                    }
                )
                break
    return found


def deduplicate_decisions(candidates: list[dict], existing: list[dict]) -> list[dict]:
    """Drop candidates that repeat a decision already stored.

    Session end can run more than once over overlapping transcripts (a resumed
    session replays earlier turns), so without this the same sentence gains a
    new row every time a terminal closes.
    """
    known: set[str] = set()
    for e in existing or []:
        c = (e.get("chosen") or "").strip().lower()
        if c:
            known.add(c[:80])
    out: list[dict] = []
    for c in candidates:
        key = (c.get("chosen") or "").strip().lower()[:80]
        if not key or key in known:
            continue
        known.add(key)
        out.append(c)
    return out
