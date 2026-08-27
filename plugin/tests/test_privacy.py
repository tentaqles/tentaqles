"""Tests for tentaqles.privacy — the shared redaction primitive."""

from __future__ import annotations

import json
from pathlib import Path

from tentaqles.privacy import entropy, has_secrets, redact_text


# ---------------------------------------------------------------------------
# Pattern-level redaction
# ---------------------------------------------------------------------------


def test_aws_key_redacted():
    text = "creds: AKIAIOSFODNN7EXAMPLE and more"
    out, events = redact_text(text)
    assert "AKIAIOSFODNN7EXAMPLE" not in out
    assert "[REDACTED:aws_access_key]" in out
    assert "aws_access_key" in events


def test_jwt_redacted():
    jwt = (
        "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
        ".eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4ifQ"
        ".SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
    )
    out, events = redact_text(f"token = {jwt}")
    assert jwt not in out
    assert "[REDACTED:jwt]" in out
    assert "jwt" in events


def test_github_pat_redacted():
    # ghp_ + 36 chars
    pat = "ghp_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8"
    out, events = redact_text(f"export GH_TOKEN={pat}")
    assert pat not in out
    assert "github_pat" in events


def test_connection_string_redacted():
    text = "db = postgresql://user:pass@host.example.com/db"
    out, events = redact_text(text)
    assert "user:pass" not in out
    assert "connection_string" in events

    # Bare redis URL with NO credentials must pass through untouched.
    clean = "cache = redis://localhost:6379/0"
    out2, events2 = redact_text(clean)
    assert out2 == clean
    assert events2 == []


def test_private_key_header_redacted():
    text = "key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA..."
    out, events = redact_text(text)
    assert "BEGIN RSA PRIVATE KEY" not in out
    assert "private_key" in events


def test_bearer_token_redacted():
    text = "Authorization: Bearer abc123DEF456ghi789JKL"
    out, events = redact_text(text)
    assert "abc123DEF456ghi789JKL" not in out
    assert "bearer_token" in events


def test_api_key_pattern_redacted():
    text = "API_KEY=xyz123abcdef456ghi"
    out, events = redact_text(text)
    assert "xyz123abcdef456ghi" not in out
    assert "api_key" in events


def test_clean_text_unchanged():
    text = "the quick brown fox jumps over the lazy dog"
    out, events = redact_text(text)
    assert out == text
    assert events == []


# ---------------------------------------------------------------------------
# Cross-client email detection
# ---------------------------------------------------------------------------


def test_cross_client_email_flagged():
    text = "ping other@globex.com about this"
    out, events = redact_text(text, authorized_emails=["me@acme.com"])
    assert "other@globex.com" not in out
    assert "cross_client_email" in events


def test_authorized_email_not_flagged():
    text = "from me@acme.com to the team"
    out, events = redact_text(text, authorized_emails=["me@acme.com"])
    assert "me@acme.com" in out
    assert "cross_client_email" not in events


# ---------------------------------------------------------------------------
# has_secrets
# ---------------------------------------------------------------------------


def test_has_secrets_true():
    assert has_secrets("token AKIAIOSFODNN7EXAMPLE here") is True


def test_has_secrets_false():
    assert has_secrets("the quick brown fox") is False


# ---------------------------------------------------------------------------
# Entropy
# ---------------------------------------------------------------------------


def test_entropy_high():
    # Random-ish base64 blob
    blob = "aZ9kLmQp2Xr7Ys4Tn6Vb8Wc1Dg3Hj5K"
    assert entropy(blob) > 4.0


def test_entropy_low():
    assert entropy("aaaaaaaaaaaaaaaaaaaa") < 2.0


# ---------------------------------------------------------------------------
# Audit log
# ---------------------------------------------------------------------------


def test_audit_log_written(tmp_path: Path):
    log = tmp_path / "audit.jsonl"
    text = "creds: AKIAIOSFODNN7EXAMPLE trailing"
    _out, events = redact_text(text, audit_log_path=log)
    assert events == ["aws_access_key"]
    assert log.exists()
    lines = log.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 1
    record = json.loads(lines[0])
    assert record["pattern"] == "aws_access_key"
    assert "occurred_at" in record
    assert "context" in record
    assert "[REDACTED:aws_access_key]" in record["context"]
    assert "AKIAIOSFODNN7EXAMPLE" not in record["context"]


# ---------------------------------------------------------------------------
# API shape
# ---------------------------------------------------------------------------


def test_redact_returns_both_values():
    result = redact_text("the quick brown fox")
    assert isinstance(result, tuple)
    assert len(result) == 2
    text, events = result
    assert isinstance(text, str)
    assert isinstance(events, list)


def test_empty_and_none_inputs():
    assert redact_text("") == ("", [])
    assert redact_text(None) == ("", [])
