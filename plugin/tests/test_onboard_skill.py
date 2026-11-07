"""Text checks of the onboarding skill (ticket 021, ADR-0014, ADR-0015, ADR-0017).

The skill is text, so these tests read it and check what it says. How a model reads it is not
provable here; the owner's U-02 check proves the walk works once. The guard hook is what
enforces, and the skill never changes it.
"""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKILL = ROOT / "skills" / "onboard" / "SKILL.md"

# Names a skill step must never read (REQ-1). A line may name one only to forbid it.
PROTECTED = [".tentaqles.yaml", "manifest.yaml", ".claude.json", "~/.tentaqles"]
NEGATION = re.compile(r"\b(never|not|don't|do not|cannot)\b", re.IGNORECASE)


def body():
    return SKILL.read_text(encoding="utf-8")


def lines():
    return body().splitlines()


def section(start, end):
    """The text from the heading that starts with start, up to the heading that starts with end."""
    src = body()
    i = src.index(start)
    j = src.index(end, i + len(start))
    return src[i:j]


def code_blocks(src):
    return re.findall(r"^```[^\n]*\n(.*?)^```", src, re.M | re.S)


def frontmatter():
    ls = lines()
    assert ls and ls[0] == "---", "the skill starts with frontmatter"
    end = ls.index("---", 1)
    fields = {}
    for line in ls[1:end]:
        key, sep, value = line.partition(":")
        assert sep and re.fullmatch(r"[a-z-]+", key), "one key: value per line: %r" % line
        fields[key] = value.strip()
    assert fields.get("name") == "onboard", fields
    desc = fields.get("description", "")
    assert desc and desc[0].isalpha(), "a plain description scalar"
    assert ": " not in desc and " #" not in desc, "the description parses as a YAML scalar"
    return fields


def test_onboard_skill_walks_every_step():
    src = body()
    for name in ("tentaqles status --json", "tentaqles install", "tentaqles connect"):
        assert name in src, name
    cred = section("### b. Credential", "### c. Connection")
    positions = [cred.index(m) for m in ("OAuth", "Token", "MCP")]
    assert positions == sorted(positions), "the credential methods run in the order OAuth, token, MCP"
    assert re.search(r"^\s*! \S", src, re.M), "a login is printed with the ! prefix"
    assert "! gh auth login --web" in src
    assert "| Provider | CLI | Credential | Connections | Verified |" in src, "the five table columns"
    desc = frontmatter()["description"]
    for trigger in ("onboard", "connect my tools", "log me in", "is everything connected", "first session", "new client"):
        assert trigger in desc, trigger


def test_onboard_skill_stops_when_not_a_client():
    step1 = section("## 1.", "## 2.")
    assert "neutral" in step1 and "untrusted" in step1
    assert "tentaqles reach" in step1 and "tq hooks verify" in step1
    assert re.search(r"\bstop\b", step1, re.IGNORECASE), "it stops in both cases"


def test_onboard_skill_never_picks_an_installer():
    src = body()
    for pat in (r"\bwinget\b", r"\bbrew\b", r"\bapt(-get)?\b", r"npm install", r"pip install",
                r"\.msi\b", r"\.exe\b", r"\bcurl\b", r"\bwget\b"):
        assert not re.search(pat, src, re.IGNORECASE), pat
    assert "manual, download from" in src
    assert "vendor" in src and "page" in src, "a manual install gives the vendor's page"


def test_onboard_skill_handles_no_catalog_server():
    src = body()
    assert "no MCP server in the catalog" in src
    assert "claude.ai" in src and "connector" in src
    conn = section("### c. Connection", "## 3.")
    assert "claude.ai" in conn and "connector" in conn
    assert "done" in conn and "check" in conn, "the developer does the step, then it is checked"


def test_onboard_skill_is_short():
    assert len(lines()) <= 150, len(lines())
    frontmatter()


def test_onboard_skill_has_no_old_flow():
    src = body()
    for old in ("tq doctor --json", "tq bundle sync", "tq allow", "tq onboard", "tq login"):
        assert old not in src, old


def test_onboard_skill_reads_no_protected_path():
    src = body()
    for block in code_blocks(src):
        for name in PROTECTED:
            assert name not in block, "a command block names %s" % name
    for line in lines():
        for name in PROTECTED:
            if name in line:
                assert NEGATION.search(line), "a line names %s without forbidding it: %r" % (name, line)
    step1 = section("## 1.", "## 2.")
    assert "tentaqles status --json" in step1
    assert "Never read" in src


def test_onboard_skill_never_handles_a_token():
    src = body()
    low = src.lower()
    assert re.search(r"never ask for[^\n]*token", low), "it never asks for a token"
    assert re.search(r"never[^\n]*repeat", low) and "repeat" in low, "it never repeats a token"
    assert re.search(r"never[^\n]*store", low), "it never stores a token"
    assert "pasted" in low and "not be shared here" in low, "a pasted token is named and not used"
    assert "do not use" in low or "not use it" in low
    assert "tq secret set" in src


def test_onboard_skill_keeps_scope_and_browser():
    src = body()
    assert re.search(r"outside[^\n]*\bscope\b", src), "a provider outside scope is named"
    assert "refuse" in src.lower()
    assert "tentaqles open" in src
    for pat in (r"start http", r"open http", r"\bchrome\b", r"msedge"):
        assert not re.search(pat, src, re.IGNORECASE), pat


def test_onboard_skill_says_other_is_not_isolated():
    src = body()
    assert "other_providers" in src
    assert "not isolated per client" in src
    assert "guard cannot check" in src
