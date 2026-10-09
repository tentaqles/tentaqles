"""PEM private keys are redacted as whole blocks, not just the BEGIN line."""

from tentaqles.privacy import redact_text

BEGIN = "-----BEGIN " + "PRIVATE KEY-----"
END = "-----END " + "PRIVATE KEY-----"
BODY = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcw\nggSjAgEAAoIBAQC7VJTUt9Us8cKjMzEfYyji\n"


def _assert_hidden(out: str) -> None:
    assert "MIIEvQ" not in out and "ggSjAg" not in out, out
    assert "[REDACTED:private_key]" in out


def test_multiline_block_and_surrounding_text():
    out, events = redact_text("before\n" + BEGIN + "\n" + BODY + END + "\nafter")
    _assert_hidden(out)
    assert out.startswith("before\n") and out.endswith("\nafter")
    assert events == ["private_key"]


def test_json_escaped_block():
    escaped = BODY.replace("\n", "\n")
    out, _ = redact_text('{"private_key": "' + BEGIN + "\n" + escaped + END + '\n", "x": 1}')
    _assert_hidden(out)
    assert out.endswith('", "x": 1}')


def test_unterminated_block_redacts_to_end():
    out, _ = redact_text("before\n" + BEGIN + "\n" + BODY)
    _assert_hidden(out)
    assert out.startswith("before\n")


def test_other_private_key_formats():
    cases = {
        "pgp": "-----BEGIN PGP " + "PRIVATE KEY BLOCK-----\n" + BODY + "-----END PGP PRIVATE KEY BLOCK-----",
        "ssh2": "---- BEGIN SSH2 ENCRYPTED " + "PRIVATE KEY ----\n" + BODY + "---- END SSH2 ENCRYPTED PRIVATE KEY ----",
        "putty": "PuTTY-User-" + "Key-File-3: ssh-rsa\nEncryption: none\n" + BODY + "Private-MAC: 0a1b2c3d4e",
    }
    for name, key in cases.items():
        out, _ = redact_text("start\n" + key + "\nend")
        _assert_hidden(out)
        assert out.startswith("start\n"), name
