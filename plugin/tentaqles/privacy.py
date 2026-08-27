"""Tentaqles privacy filter — redacts secrets from text before it leaves the client boundary.

Shared primitive with zero heavy dependencies (stdlib only). All other Tentaqles features
that send text to external services (LLMs, remote graphs, exports) must route through
``redact_text`` first.

Pattern library is ordered from most-specific to most-generic so that e.g. an AWS access
key is caught by its dedicated pattern before the generic "API key" fallback fires.
"""

from __future__ import annotations

import json
import math
import re
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable

__all__ = [
    "REDACTION_PATTERNS",
    "EMAIL_PATTERN",
    "entropy",
    "has_secrets",
    "redact_text",
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
            r"[a-zA-Z0-9._-]+@[a-zA-Z0-9-]+\.iam\.gserviceaccount\.com"
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
    # PEM private key header (RSA / EC / OPENSSH / generic)
    (
        "private_key",
        re.compile(r"-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY-----"),
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
EMAIL_PATTERN: re.Pattern = re.compile(
    r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b"
)


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
) -> tuple[str, list[str]]:
    """Redact secrets from *text*.

    Returns a tuple of ``(redacted_text, events)`` where ``events`` is a list of
    pattern names that fired (one entry per redaction, in order).

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
        ``context`` (up to 80 chars of surrounding text with the secret
        replaced by its redaction marker).
    """
    if not text:
        return text or "", []

    events: list[str] = []
    # Collect (start, end, name, replacement) spans first so we can write the
    # audit log with accurate "context" snippets that reference the *redacted*
    # form, not the raw secret.
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

    if not spans:
        return text, []

    # Resolve overlaps: sort by (start, -length) so the longest span at each
    # start wins; then greedily drop any later span that intersects an accepted one.
    spans.sort(key=lambda s: (s[0], -(s[1] - s[0])))
    accepted: list[tuple[int, int, str, str]] = []
    cursor = -1
    for span in spans:
        start, end, _name, _repl = span
        if start >= cursor:
            accepted.append(span)
            cursor = end

    # Build the redacted string in one pass, in order.
    accepted.sort(key=lambda s: s[0])
    out_parts: list[str] = []
    last = 0
    audit_records: list[dict] = []
    for start, end, name, repl in accepted:
        out_parts.append(text[last:start])
        out_parts.append(repl)
        events.append(name)
        if audit_log_path is not None:
            ctx_start = max(0, start - 30)
            ctx_end = min(len(text), end + 30)
            context = text[ctx_start:start] + repl + text[end:ctx_end]
            context = context.replace("\n", " ").replace("\r", " ")
            if len(context) > 80:
                context = context[:80]
            audit_records.append(
                {
                    "occurred_at": datetime.now(timezone.utc).isoformat(),
                    "pattern": name,
                    "context": context,
                }
            )
        last = end
    out_parts.append(text[last:])
    redacted = "".join(out_parts)

    if audit_log_path is not None and audit_records:
        path = Path(audit_log_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("a", encoding="utf-8") as fh:
            for rec in audit_records:
                fh.write(json.dumps(rec) + "\n")

    return redacted, events
