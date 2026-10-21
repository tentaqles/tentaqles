"""Tentaqles privacy filter — redacts secrets and personal data before text leaves.

Shared primitive with zero heavy dependencies (stdlib only; ``workspace_classes``
imports the manifest loader lazily). Every feature that writes text to disk
(memory.db, the vault, the audit log) or sends it to a model routes it through
``redact_str`` / ``redact_text`` first. Reading personal data locally is allowed;
only the exits are guarded.

Secrets are always redacted. ``redact_text(text)`` with no ``classes`` is
secrets only: that is what read paths (reviewer bundle, test tails, context
injection, dashboard) use, so personal data stays readable locally. Exits pass
the workspace's classes, mirroring tq's ``internal/redact``: with any class
list the checksummed detectors (CPF, CNPJ, card, IBAN, each validated by its
check digits) run; ``pii`` / ``sensitive`` / ``children`` add e-mail and phone;
``financial`` adds the financial detectors; ``all`` is everything; ``public``,
``internal`` and ``secrets`` add nothing. An unknown class or an empty class
list is a ``ValueError``; ``redact_str`` then falls back to the strictest
masking, never to the raw text. A line carrying ``tq:allow-pii`` keeps its
personal data (secrets are still redacted).

Pattern library is ordered from most-specific to most-generic so that e.g. an AWS access
key is caught by its dedicated pattern before the generic "API key" fallback fires.
"""

from __future__ import annotations

import bisect
import json
import math
import re
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable, Iterable

__all__ = [
    "REDACTION_PATTERNS",
    "EMAIL_PATTERN",
    "PII_PATTERNS",
    "CLASS_DETECTORS",
    "KNOWN_CLASSES",
    "STRICTEST_CLASSES",
    "ALLOW_PII_MARKER",
    "entropy",
    "exit_classes",
    "has_secrets",
    "redact_str",
    "redact_text",
    "redact_value",
    "EXIT_BASELINE_CLASSES",
    "workspace_classes",
]


# ---------------------------------------------------------------------------
# Pattern library
# ---------------------------------------------------------------------------
#
# Order matters: more specific patterns come first so they "win" before a
# generic pattern (like API_KEY=...) has a chance to redact the same span with
# a less-informative label.

REDACTION_PATTERNS: list[tuple[str, re.Pattern]] = [
    # AWS access key id — always starts with AKIA/ASIA/AGPA/AIDA/AROA/AIPA/ANPA/ANVA/ASCA
    (
        "aws_access_key",
        re.compile(r"\b(?:AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASCA)[0-9A-Z]{16}\b"),
    ),
    # GCP service account JSON fragment — the distinctive "private_key_id" or service account email
    (
        "gcp_service_account",
        re.compile(
            r'"type"\s*:\s*"service_account"|'
            # Bounded and anchored so a long run of local-part characters
            # cannot make the search quadratic.
            r"(?<![a-zA-Z0-9._-])[a-zA-Z0-9._-]{1,64}@[a-zA-Z0-9-]{1,63}\.iam\.gserviceaccount\.com"
        ),
    ),
    # GitHub personal access token / fine-grained / OAuth / app / refresh
    (
        "github_pat",
        re.compile(r"\b(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}\b"),
    ),
    # JSON Web Token — three base64url segments separated by dots, starting with eyJ
    (
        "jwt",
        re.compile(r"\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b"),
    ),
    # HTTP Authorization bearer token (case-insensitive header, captures the token value)
    (
        "bearer_token",
        re.compile(r"(?i)\bBearer\s+[A-Za-z0-9._\-+/=]{16,}"),
    ),
    # Connection string with embedded credentials: scheme://user:pass@host[...]
    # Requires the user:pass@ form so bare `redis://localhost` does NOT match.
    (
        "connection_string",
        re.compile(
            r"\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|rediss|mssql|sqlserver|amqp|amqps)"
            r"://[^\s:/@]+:[^\s/@]+@[^\s/]+"
        ),
    ),
    # Whole private-key blocks: any PEM "... PRIVATE KEY" / "PRIVATE KEY BLOCK",
    # SSH2 and PuTTY .ppk. Unterminated blocks are redacted to the end of the
    # text. Mirrors internal/secrets in tq.
    (
        "private_key",
        re.compile(
            r"-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----"
            r"(?:.*?-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----|.*)"
            r"|---- BEGIN SSH2 [A-Z ]*PRIVATE KEY ----(?:.*?---- END SSH2 [A-Z ]*PRIVATE KEY ----|.*)"
            r"|PuTTY-User-Key-File-\d+:(?:.*?Private-MAC: *[0-9a-fA-F]+|.*)",
            re.S,
        ),
    ),
    # Generic API key / secret / token assignment: KEY=value or "key": "value"
    # Intentionally last so specific vendors above win first.
    (
        "api_key",
        re.compile(
            r"(?i)\b(?:api[_-]?key|secret(?:[_-]?key)?|access[_-]?token|auth[_-]?token|"
            r"client[_-]?secret|private[_-]?token)\b"
            r"\s*[:=]\s*"
            r'["\']?([A-Za-z0-9_\-./+=]{12,})["\']?'
        ),
    ),
]

# Standalone email pattern — only flagged when authorized_emails is provided
# (cross-client leak detection). Intentionally NOT in REDACTION_PATTERNS because
# plain emails are not inherently secret.
# Bounded (RFC 5321 lengths) and anchored by a lookbehind: a match can only
# start where the local part starts, so a long run of `1.1.1...` is linear.
EMAIL_PATTERN: re.Pattern = re.compile(
    r"(?<![A-Za-z0-9._%+-])[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9.-]{1,253}\.[A-Za-z]{2,24}\b"
)


# ---------------------------------------------------------------------------
# Personal and financial data (mirror of tq's internal/redact)
# ---------------------------------------------------------------------------


def _digits(s: str) -> str:
    return "".join(ch for ch in s if ch.isdigit())


def _all_same(d: str) -> bool:
    return len(set(d)) == 1


def _valid_cpf(s: str) -> bool:
    d = _digits(s)
    if len(d) != 11 or _all_same(d):
        return False
    for n in (9, 10):
        total = sum(int(d[i]) * (n + 1 - i) for i in range(n))
        r = (total * 10) % 11
        if (0 if r == 10 else r) != int(d[n]):
            return False
    return True


def _valid_cnpj(s: str) -> bool:
    d = _digits(s)
    if len(d) != 14 or _all_same(d):
        return False
    w1 = [5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2]
    w2 = [6] + w1
    for w, n in ((w1, 12), (w2, 13)):
        r = sum(int(d[i]) * x for i, x in enumerate(w)) % 11
        if (0 if r < 2 else 11 - r) != int(d[n]):
            return False
    return True


def _luhn(s: str) -> bool:
    d = _digits(s)
    if not 13 <= len(d) <= 19 or _all_same(d):
        return False
    total, alt = 0, False
    for ch in reversed(d):
        n = int(ch)
        if alt:
            n = n * 2 - 9 if n > 4 else n * 2
        total += n
        alt = not alt
    return total % 10 == 0


def _valid_iban(s: str) -> bool:
    s = s.replace(" ", "").upper()
    if not 15 <= len(s) <= 34:
        return False
    r = s[4:] + s[:4]
    try:
        return int("".join(str(int(c, 36)) for c in r)) % 97 == 1
    except ValueError:
        return False


def _valid_phone(s: str) -> bool:
    d = _digits(s)
    return 10 <= len(d) <= 15 and not _all_same(d)


def _always(_s: str) -> bool:
    return True


# (name, pattern, validator). Order is the tie-break when two detectors match
# the same span: checksummed identifiers first, then the looser shapes.
# Identifiers are delimited by anything that is not a letter or digit (so
# `id_52998224725` matches, `abc52998224725xyz` does not). A candidate that
# fails its checksum is retried shorter at the same start (see _pii_spans).
_L = r"(?<![0-9A-Za-z])"
_R = r"(?![0-9A-Za-z])"
PII_PATTERNS: list[tuple[str, re.Pattern, Callable[[str], bool]]] = [
    ("cpf", re.compile(_L + r"\d{3}[. -]?\d{3}[. -]?\d{3}[. -]?\d{2}" + _R), _valid_cpf),
    ("cnpj", re.compile(_L + r"\d{2}[. -]?\d{3}[. -]?\d{3}[/. -]?\d{4}[. -]?\d{2}" + _R), _valid_cnpj),
    ("card", re.compile(
        _L + r"(?:[3-6]\d{3}(?:[ -]\d{4}){3}(?:[ -]\d{1,4})?"  # 4-4-4-4 (+ a short tail)
        r"|3[47]\d{2}[ -]\d{6}[ -]\d{5}"  # Amex 4-6-5
        r"|[3-6]\d{12,18})" + _R  # contiguous
    ), _luhn),
    ("iban", re.compile(
        _L + r"[A-Za-z]{2}\d{2}(?:[ ]?[A-Za-z0-9]{4}){2,7}(?:[ ]?[A-Za-z0-9]{1,4})?" + _R
    ), _valid_iban),
    ("email", EMAIL_PATTERN, _always),
    ("phone", re.compile(
        r"(?:\+\d{1,3}[\s.-]?)?(?:\(\d{2,3}\)|\b\d{2,3})[\s.-]?\d{4,5}[\s.-]?\d{4}\b|\+\d{8,15}\b"
    ), _valid_phone),
]
_PII_BY_NAME = {name: (rx, ok) for name, rx, ok in PII_PATTERNS}
_PII_ORDER = [name for name, _rx, _ok in PII_PATTERNS]

CLASS_DETECTORS: dict[str, list[str]] = {
    "checksummed": ["cpf", "cnpj", "card", "iban"],
    "pii": ["cpf", "email", "phone"],
    "financial": ["card", "cnpj", "iban"],
}
_CLASS_ALIASES = {"sensitive": "pii", "children": "pii"}
STRICTEST_CLASSES: list[str] = ["checksummed", "pii", "financial"]
# Classes that add no detectors of their own (spec §11.1 `public`, `internal`;
# `secrets` because secrets are always redacted).
_NO_DETECTOR_CLASSES = frozenset({"public", "internal", "secrets"})
KNOWN_CLASSES = frozenset(CLASS_DETECTORS) | frozenset(_CLASS_ALIASES) | _NO_DETECTOR_CLASSES | {"all"}
ALLOW_PII_MARKER = "tq:allow-pii"
# The baseline for an exit that has no workspace classes at hand.
EXIT_BASELINE_CLASSES: list[str] = ["checksummed"]


def _detectors(classes: Iterable[str] | None) -> list[str]:
    """Detector names for *classes*, in pattern order. Raises ValueError.

    ``None`` means secrets only (read paths): no personal-data detectors. For
    an explicit list the checksummed detectors always run. An unknown class, a
    bare string, or an empty list is an error (fail closed, like tq's
    ``ParseClasses``).
    """
    if classes is None:
        return []
    if isinstance(classes, str):
        raise ValueError("data classes must be a list, not a string")
    classes = list(classes)
    if not classes:
        raise ValueError("empty data class list")
    wanted = set(CLASS_DETECTORS["checksummed"])
    for c in classes:
        if not isinstance(c, str) or c not in KNOWN_CLASSES:
            raise ValueError(f"unknown data class {c!r}")
        if c == "all":
            expanded = STRICTEST_CLASSES
        elif c in _NO_DETECTOR_CLASSES:
            expanded = []
        else:
            expanded = [_CLASS_ALIASES.get(c, c)]
        for cls in expanded:
            wanted.update(CLASS_DETECTORS[cls])
    return [n for n in _PII_ORDER if n in wanted]


def exit_classes(manifest: dict | None) -> list[str]:
    """Classes to mask at the exits for a workspace manifest.

    ``["checksummed"]`` without a ``data:`` block (or with no classes), plus
    ``pii`` for pii/sensitive/children and ``financial`` for financial. A data
    block that cannot be read, or that names an unknown class, gives the
    strictest set.
    """
    if manifest is None:
        return ["checksummed"]
    if not isinstance(manifest, dict):
        return list(STRICTEST_CLASSES)
    data = manifest.get("data")
    if data is None:
        return ["checksummed"]
    if not isinstance(data, dict):
        return list(STRICTEST_CLASSES)
    classes = data.get("classes")
    if classes is None or classes == []:
        return ["checksummed"]
    if not isinstance(classes, list):
        return list(STRICTEST_CLASSES)
    try:
        _detectors(classes)
    except ValueError:
        return list(STRICTEST_CLASSES)
    if "all" in classes:
        return list(STRICTEST_CLASSES)
    out = ["checksummed"]
    if any(c in ("pii", "sensitive", "children") for c in classes):
        out.append("pii")
    if "financial" in classes:
        out.append("financial")
    return out


def workspace_classes(workspace_path: str | Path) -> list[str]:
    """``exit_classes`` of the manifest governing *workspace_path*.

    A manifest that exists but cannot be loaded gives the strictest set, so a
    typo in .tentaqles.yaml never lets personal data through.
    """
    try:
        import yaml

        from tentaqles.manifest.loader import find_manifest, load_manifest
    except ImportError:
        return list(STRICTEST_CLASSES)
    try:
        manifest = load_manifest(workspace_path)
        if manifest is None and find_manifest(workspace_path) is not None:
            return list(STRICTEST_CLASSES)
        return exit_classes(manifest)
    except (OSError, ValueError, yaml.YAMLError):
        return list(STRICTEST_CLASSES)


def _allowed_lines(text: str) -> list[tuple[int, int]]:
    """(start, end) of every line carrying the allow marker, in order."""
    out: list[tuple[int, int]] = []
    pos = text.find(ALLOW_PII_MARKER)
    while pos != -1:
        start = text.rfind("\n", 0, pos) + 1
        end = text.find("\n", pos)
        end = len(text) if end == -1 else end
        out.append((start, end))
        pos = text.find(ALLOW_PII_MARKER, end)
    return out


def _shorter_valid(rx: re.Pattern, ok, text: str, m: re.Match):
    """The longest valid candidate at ``m.start()`` that ends before ``m.end()``.

    Only cuts at a real delimiter (the next character is not a letter or a
    digit) are tried, so a shorter candidate is never carved out of a longer
    number. Bounded by the match length (patterns are bounded).
    """
    start = m.start()
    for end in range(m.end() - 1, start, -1):
        if text[end].isalnum() or not text[end - 1].isalnum():
            continue
        cand = rx.fullmatch(text, start, end)
        if cand is not None and ok(cand.group(0)):
            return cand
    return None


def _pii_spans(text: str, names: list[str]) -> list[tuple[int, int, str, str]]:
    allowed = _allowed_lines(text) if ALLOW_PII_MARKER in text else []
    allowed_starts = [a[0] for a in allowed]
    spans: list[tuple[int, int, str, str]] = []
    for name in names:
        rx, ok = _PII_BY_NAME[name]
        pos = 0
        while True:
            m = rx.search(text, pos)
            if m is None:
                break
            if not ok(m.group(0)):
                # The pattern may have pulled in the next token (an expiry
                # date, a CVV, a word): try shorter candidates at the same
                # start, then look again one character further on so a failed
                # checksum never hides a valid value inside it.
                shorter = _shorter_valid(rx, ok, text, m)
                if shorter is None:
                    pos = m.start() + 1
                    continue
                m = shorter
            pos = max(m.end(), m.start() + 1)
            if allowed:
                i = bisect.bisect_right(allowed_starts, m.start()) - 1
                if i >= 0 and m.start() <= allowed[i][1]:
                    continue
            spans.append((m.start(), m.end(), name, f"[REDACTED:{name}]"))
    return spans


def _resolve(spans: list[tuple[int, int, str, str]]) -> list[tuple[int, int, str, str]]:
    """Longest span at each start wins; a later span that intersects is dropped.

    ``sorted`` is stable, so equal spans keep detector order (cpf before card).
    """
    spans = sorted(spans, key=lambda s: (s[0], -(s[1] - s[0])))
    accepted: list[tuple[int, int, str, str]] = []
    cursor = -1
    for span in spans:
        if span[0] >= cursor:
            accepted.append(span)
            cursor = span[1]
    return accepted


def _outside(accepted: list[tuple[int, int, str, str]], spans: list[tuple[int, int, str, str]]):
    """Spans that do not intersect any of the sorted, disjoint *accepted* spans."""
    if not accepted:
        return list(spans)
    starts = [a[0] for a in accepted]
    out = []
    for span in spans:
        i = bisect.bisect_right(starts, span[0]) - 1
        if i >= 0 and accepted[i][1] > span[0]:
            continue
        if i + 1 < len(accepted) and accepted[i + 1][0] < span[1]:
            continue
        out.append(span)
    return out


# ---------------------------------------------------------------------------
# Entropy
# ---------------------------------------------------------------------------


def entropy(s: str) -> float:
    """Return the Shannon entropy (in bits per character) of *s*.

    Random base64 blobs score > 4.0; repeated characters score near 0.
    Empty strings return 0.0.
    """
    if not s:
        return 0.0
    counts = Counter(s)
    length = len(s)
    result = 0.0
    for count in counts.values():
        p = count / length
        result -= p * math.log2(p)
    return result


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------


def has_secrets(text: str, strict: bool = False) -> bool:
    """Fast boolean check: does *text* contain anything the redactor would match?

    Short-circuits on the first match. Safe to call on empty / None input.
    """
    if not text:
        return False
    for _name, pattern in REDACTION_PATTERNS:
        if pattern.search(text):
            return True
    if strict:
        # In strict mode, flag any high-entropy token >= 20 chars as suspicious.
        for token in re.findall(r"[A-Za-z0-9_\-./+=]{20,}", text):
            if entropy(token) >= 4.0:
                return True
    return False


def redact_text(
    text: str | None,
    strict: bool = False,
    authorized_emails: Iterable[str] | None = None,
    audit_log_path: str | Path | None = None,
    *,
    classes: Iterable[str] | None = None,
) -> tuple[str, list[str]]:
    """Redact secrets and personal data from *text*.

    Returns a tuple of ``(redacted_text, events)`` where ``events`` is a list of
    pattern names that fired (one entry per redaction, in text order; personal
    data events are the detector names, e.g. ``cpf`` or ``email``).

    Parameters
    ----------
    text:
        Input text. ``None`` or empty strings pass through unchanged with an
        empty events list.
    strict:
        If True, additionally redact any high-entropy token >= 20 chars long
        that isn't already covered by a pattern.
    authorized_emails:
        If provided, any email address in *text* that is NOT in this allow-list
        is treated as a cross-client leak and redacted with the
        ``cross_client_email`` label.
    audit_log_path:
        If provided, append one JSON line per redaction event to this file.
        Each record has keys ``occurred_at`` (ISO-8601 UTC), ``pattern``, and
        ``context`` (up to 80 chars of the *redacted* output around the
        marker, so neither secrets nor personal data reach the log).
    classes:
        Data classes whose personal-data detectors run (see the module
        docstring). ``None`` means secrets only (read paths); exits pass a list.
        Raises ``ValueError`` on an unknown class or an empty list.
    """
    names = _detectors(classes)  # validate first: a bad class list fails even on empty text
    if not text:
        return text or "", []

    # Collect (start, end, name, replacement) spans first so the audit log can
    # quote the fully redacted form, never the raw secret or personal data.
    spans: list[tuple[int, int, str, str]] = []

    for name, pattern in REDACTION_PATTERNS:
        for match in pattern.finditer(text):
            spans.append((match.start(), match.end(), name, f"[REDACTED:{name}]"))

    if authorized_emails is not None:
        allowed = {e.lower() for e in authorized_emails}
        for match in EMAIL_PATTERN.finditer(text):
            if match.group(0).lower() not in allowed:
                spans.append(
                    (match.start(), match.end(), "cross_client_email", "[REDACTED:cross_client_email]")
                )

    if strict:
        for match in re.finditer(r"[A-Za-z0-9_\-./+=]{20,}", text):
            if entropy(match.group(0)) >= 4.0:
                spans.append(
                    (match.start(), match.end(), "high_entropy", "[REDACTED:high_entropy]")
                )

    # Secrets win: personal-data spans that touch an accepted secret are dropped.
    accepted = _resolve(spans)
    if names:
        accepted = sorted(accepted + _resolve(_outside(accepted, _pii_spans(text, names))))

    if not accepted:
        return text, []

    # Build the redacted string in one pass, in order, remembering where each
    # marker landed in the output.
    out_parts: list[str] = []
    events: list[str] = []
    marks: list[tuple[int, int]] = []
    last = 0
    pos = 0
    for start, end, name, repl in accepted:
        gap = text[last:start]
        out_parts.append(gap)
        out_parts.append(repl)
        pos += len(gap)
        marks.append((pos, pos + len(repl)))
        pos += len(repl)
        events.append(name)
        last = end
    out_parts.append(text[last:])
    redacted = "".join(out_parts)

    if audit_log_path is not None:
        now = datetime.now(timezone.utc).isoformat()
        path = Path(audit_log_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("a", encoding="utf-8") as fh:
            for (ostart, oend), name in zip(marks, events):
                context = redacted[max(0, ostart - 30):oend + 30]
                context = context.replace("\n", " ").replace("\r", " ")[:80]
                fh.write(json.dumps({"occurred_at": now, "pattern": name, "context": context}) + "\n")

    return redacted, events


def redact_str(text, classes: Iterable[str] | None = None):
    """Redacted *text* for an exit, without the events. Never raises.

    ``None`` and ``""`` pass through. ``classes=None`` is the exit baseline
    (``EXIT_BASELINE_CLASSES``: secrets plus checksummed identifiers); callers
    with a workspace pass its classes. An invalid class list falls back to the
    strictest masking; any other failure returns ``[REDACTED:error]`` rather
    than the input.
    """
    if text is None or text == "":
        return text
    if classes is None:
        classes = EXIT_BASELINE_CLASSES
    try:
        return redact_text(text, classes=classes)[0]
    except ValueError:
        try:
            return redact_text(text, classes=STRICTEST_CLASSES)[0]
        except Exception:
            return "[REDACTED:error]"
    except Exception:
        return "[REDACTED:error]"


def redact_value(value, classes: Iterable[str] | None = None):
    """``redact_str`` applied to every string (keys included) in JSON-shaped *value*."""
    if isinstance(value, str):
        return redact_str(value, classes)
    if isinstance(value, dict):
        return {redact_str(str(k), classes): redact_value(v, classes) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [redact_value(v, classes) for v in value]
    return value
