"""Content checks for the data pack's contract parts: the data-contract skill,
the ODCS contract template, the data-contracts on-ramp notes and the docs page."""

from __future__ import annotations

import re
from pathlib import Path

import yaml

PLUGIN = Path(__file__).resolve().parents[1]
REPO = PLUGIN.parent
SKILL = PLUGIN / "skills" / "data-contract" / "SKILL.md"
TEMPLATE = PLUGIN / "templates" / "process" / "data-contract.odcs.yaml"
ONRAMP = PLUGIN / "skills" / "pack" / "references" / "onramps" / "data-contracts.md"
DOC = REPO / "docs" / "packs" / "data-contracts.md"
REGISTER = REPO / "internal" / "packs" / "data" / "register_contract.go"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'
ONRAMP_SECTIONS = {"build", "feature", "fix", "kickoff", "dod", "ship"}
HUMAN_ONLY = ("tq pack baseline", "promote apply", "tq hub answer", "build-gate approve", "tq allow ")


def _text(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, re.S)


def _check_ids() -> list[str]:
    return sorted(set(re.findall(r'ID: "(data/[a-z0-9-]+)"', _text(REGISTER))))


# --- skill -------------------------------------------------------------------

def test_skill_frontmatter_and_triggers() -> None:
    text = _text(SKILL)
    fm = yaml.safe_load(text.split("---", 2)[1])
    assert fm["name"] == "data-contract"
    for phrase in ("data contract", "ODCS", "breaking change", "data_contract", "pack:data/contract-"):
        assert phrase in fm["description"], phrase


def test_skill_bash_blocks_bootstrap_and_timeouts() -> None:
    blocks = _bash_blocks(_text(SKILL))
    assert len(blocks) >= 2
    for b in blocks:
        assert b.lstrip().startswith(SNIPPET), b[:80]
        for line in b.splitlines():
            if re.search(r"(^|\s)tq\s", line) and "command -v" not in line:
                assert "$_to tq " in line, f"tq call without a timeout: {line}"
        assert "python " not in b and "python3 " not in b


def test_skill_covers_the_workflow() -> None:
    text = _text(SKILL)
    for token in ("templates/process/data-contract.odcs.yaml", "version rule", "MAJOR", "## Consumer migration",
                  "BREAKING CHANGE:", "tq pack check --pack data", "tq:allow-pii", "contracts.consumers",
                  "dbtModel", "contracts.enum_added", "classify-data", "status: retired"):
        assert token in text or token.lower() in text.lower(), token
    for cid in _check_ids():
        assert cid in text, cid


def test_skill_never_sends_the_agent_to_human_only_commands() -> None:
    text = _text(SKILL)
    for cmd in HUMAN_ONLY:
        assert cmd not in text, cmd
    assert ".env" not in text
    assert "Never edit `tq-packs.yaml`" in text


# --- template ----------------------------------------------------------------

def test_template_is_an_odcs_v3_contract() -> None:
    doc = yaml.safe_load(_text(TEMPLATE))
    assert re.fullmatch(r"v3\.\d+\.\d+", doc["apiVersion"])
    assert doc["kind"] == "DataContract"
    for key in ("id", "version", "status", "schema"):
        assert doc.get(key), key
    assert re.fullmatch(r"\d+\.\d+\.\d+", doc["version"])
    ids = []
    for obj in doc["schema"]:
        assert obj.get("name") and obj.get("id")
        ids.append(obj["id"])
        models = [c["value"] for c in obj.get("customProperties", []) if c.get("property") == "dbtModel"]
        assert models, "the template shows the dbtModel custom property"
        for prop in obj["properties"]:
            assert prop.get("name") and prop.get("id") and prop.get("logicalType")
            ids.append(prop["id"])
    assert len(ids) == len(set(ids))
    assert any(p.get("classification") for o in doc["schema"] for p in o["properties"])


def test_template_holds_no_real_looking_personal_data() -> None:
    text = _text(TEMPLATE)
    for email in re.findall(r"[\w.+-]+@[\w-]+(?:\.[\w-]+)+", text):
        assert email.endswith(".test"), email
    assert "acme" in text


# --- on-ramp notes -----------------------------------------------------------

def test_onramp_file_follows_the_convention() -> None:
    assert re.fullmatch(r"data-[a-z0-9-]+\.md", ONRAMP.name)
    text = _text(ONRAMP)
    assert text.startswith("# data: contracts\n")
    sections = re.findall(r"^## (\S+)\s*$", text, re.M)
    assert sections and set(sections) <= ONRAMP_SECTIONS, sections
    assert len(sections) == len(set(sections))
    kickoff = " ".join(text.split("## kickoff", 1)[1].split("\n## ", 1)[0].split())
    assert kickoff.strip().startswith("Only when"), "kickoff must start with the stack condition"
    assert "never assume it is" in kickoff
    assert "you never edit `.tentaqles.yaml`" in kickoff


def test_onramp_names_flags_not_a_crew() -> None:
    text = _text(ONRAMP)
    assert "tq pack crew --flags data_contract" in text
    assert "pipeline-reviewer" not in text, "reviewers come from tq pack crew"
    for cmd in HUMAN_ONLY:
        assert cmd not in text, cmd
    assert ".env" not in text
    for cid in ("data/contract-valid", "data/contract-breaking", "data/contract-pii", "data/contract-dbt-sync"):
        assert f"pack:{cid}" in text, cid
    assert "BREAKING CHANGE:" in text and "## Consumer migration" in text


# --- docs ----------------------------------------------------------------------

def test_doc_lists_every_check_and_rule() -> None:
    text = _text(DOC)
    ids = _check_ids()
    assert len(ids) == 6, ids
    for cid in ids:
        assert f"`{cid}`" in text, cid
    for rule in ("contract-deleted", "object-removed", "object-renamed", "property-removed", "property-renamed",
                 "logical-type-changed", "physical-type-narrowed", "required-loosened", "unique-loosened",
                 "primary-key-changed", "type-options-loosened", "enum-value-removed", "enum-value-added",
                 "quality-removed", "quality-loosened", "sla-loosened", "server-removed", "server-changed",
                 "partitioning-changed", "classification-changed", "retired-with-consumers",
                 "version-not-bumped", "major-without-breaking"):
        assert f"`{rule}`" in text, rule
    for key in ("contracts.paths", "contracts.enum_added", "contracts.consumers", "data_contract", "data-contracts-none"):
        assert key in text, key


def test_doc_lists_every_breaking_rule_the_code_emits() -> None:
    """Every rule id data/contract-breaking can emit is documented, so a new
    row can't ship undocumented."""
    src = _text(REPO / "internal" / "packs" / "data" / "contract_breaking.go")
    rules = set(re.findall(r'd\.(?:breaking|warn)\([^,]+, "([a-z-]+)"', src))
    rules |= set(re.findall(r'Rule: "([a-z-]+)"', src))
    assert {"contract-old-unreadable", "contract-id-changed", "version-decreased"} <= rules, rules
    text = _text(DOC)
    missing = sorted(r for r in rules if f"`{r}`" not in text)
    assert not missing, missing
