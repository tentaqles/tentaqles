"""Mask secret-shaped values before text leaves the process.

A Python port of the `tq` secret patterns (internal/secrets). It is heuristic:
use it to reduce what reaches a third-party model, never to prove text is safe.
"""

from __future__ import annotations

import re
from typing import Any

_PATTERNS: list[tuple[str, re.Pattern[str]]] = [
    ("aws_access_key", re.compile(r"\b(?:AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASCA)[0-9A-Z]{16}\b")),
    ("github_token", re.compile(r"\b(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}\b")),
    ("anthropic_key", re.compile(r"\bsk-ant-[A-Za-z0-9_\-]{20,}")),
    ("openai_key", re.compile(r"\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_\-]{20,}")),
    ("stripe_key", re.compile(r"\b(?:sk|rk)_live_[A-Za-z0-9]{16,}")),
    ("slack_token", re.compile(r"\bxox[baprs]-[A-Za-z0-9\-]{10,}")),
    ("google_api_key", re.compile(r"\bAIza[0-9A-Za-z_\-]{35}\b")),
    ("azure_storage_key", re.compile(r"(?i)AccountKey=[A-Za-z0-9+/=]{40,}")),
    ("jwt", re.compile(r"\beyJ[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}")),
    ("bearer_token", re.compile(r"(?i)\bBearer\s+[A-Za-z0-9._\-+/=]{16,}")),
    (
        "connection_string",
        re.compile(
            r"\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|rediss|mssql|sqlserver|amqp|amqps)"
            r"://[^\s:/@]+:[^\s/@]+@[^\s/]+"
        ),
    ),
    (
        "private_key",
        re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)"),
    ),
    (
        "api_key_assignment",
        re.compile(
            r"(?i)\b[a-z0-9_.-]*(?:api[_-]?key|secret|token|password|passwd|pwd|credential)s?\b"
            r"[\"']?\s*[:=]\s*[\"']?[A-Za-z0-9_\-./+=!@#$%^&*]{12,}"
        ),
    ),
]


def redact(text: str) -> str:
    """Replace every secret-shaped span with `[REDACTED:<pattern>]`."""
    for name, rx in _PATTERNS:
        text = rx.sub(f"[REDACTED:{name}]", text)
    return text


def redact_obj(value: Any) -> Any:
    """Redact every string inside a JSON-like value; keys are kept as they are."""
    if isinstance(value, str):
        return redact(value)
    if isinstance(value, dict):
        return {k: redact_obj(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [redact_obj(v) for v in value]
    return value
