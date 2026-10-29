# plugin/tests/test_pack_pipeline_review.py
"""Content checks for the data pack's pipeline review (spec C §3.5, §6): the read-only
pipeline-reviewer agent, the pipeline-review skill and its D1-D11 checklist, and the
data-pipelines on-ramp note."""

from __future__ import annotations

import re
from pathlib import Path

PLUGIN = Path(__file__).resolve().parents[1]
SKILL = PLUGIN / "skills" / "pipeline-review"
AGENT = PLUGIN / "agents" / "pipeline-reviewer.md"
ONRAMP = PLUGIN / "skills" / "pack" / "references" / "onramps" / "data-pipelines.md"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'
GUARD = 'br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on \'$br\': do not commit"; exit 1;; esac'
ROWS = {
    "D1": "Idempotency", "D2": "Backfill", "D3": "Late and out-of-order data", "D4": "Schema evolution",
    "D5": "Personal data into outputs", "D6": "Tests", "D7": "Cost", "D8": "Failure handling",
    "D9": "Observability", "D10": "Secrets and environments", "D11": "Orchestrator specifics",
}


def _read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def _frontmatter(text: str) -> dict[str, str]:
    m = re.match(r"---\n(.*?)\n---\n", text, re.S)
    assert m, "front matter"
    out = {}
    for line in m.group(1).splitlines():
        k, _, v = line.partition(":")
        out[k.strip()] = v.strip()
    return out


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, re.S)


def test_agent_is_read_only_and_named_for_the_gate() -> None:
    fm = _frontmatter(_read(AGENT))
    assert fm["name"] == "pipeline-reviewer"
    assert re.fullmatch(r"[a-z][a-z0-9-]*-reviewer", fm["name"])  # gate.py accepts *-reviewer with an agent file
    assert [t.strip() for t in fm["tools"].split(",")] == ["Read", "Glob", "Grep"]
    disallowed = {t.strip() for t in fm["disallowedTools"].split(",")}
    assert {"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "PowerShell", "Agent"} <= disallowed
    for flag in ("pipeline", "data_contract", "warehouse_cost"):
        assert flag in fm["description"], flag


def test_agent_output_contract() -> None:
    body = _read(AGENT)
    for token in ("RESULT: PASS | FAIL | BLOCKED", "BUILT-BY-ME:", "JUDGED:", "BLOCKER", "MAJOR", "MINOR",
                  "CANNOT JUDGE", "file:line", "skills/pipeline-review/references/checklist.md",
                  "tq pack check --pack data --json", "docs/privacy.md", "data:"):
        assert token in body, token
    assert "D1" in body and "D11" in body
    assert "Never copy data values" in body
    assert "You only read." in body


def test_checklist_has_every_row_once() -> None:
    body = _read(SKILL / "references" / "checklist.md")
    for row, name in ROWS.items():
        lines = [l for l in body.splitlines() if l.startswith(f"| {row} |")]
        assert len(lines) == 1, row
        assert name in lines[0], (row, name)
    assert "| D12 |" not in body
    # the lints the rows lean on
    for check in ("data/pipeline-idempotency", "data/pipeline-backfill", "data/pipeline-pii-output", "data/airflow-dag",
                  "data/fabric-artifacts", "data/synapse-artifacts", "data/notebook-outputs", "data/dagster-validate"):
        assert check in body, check


def test_skill_selection_and_feeding() -> None:
    text = _read(SKILL / "SKILL.md")
    fm = _frontmatter(text)
    assert fm["name"] == "pipeline-review"
    for token in ("pipeline-reviewer", "references/checklist.md", "tq pack crew --flags", "tq decide triage",
                  "tq pack check --pack data", "--json", "pipeline", "data_contract", "warehouse_cost",
                  "[checkpoint <slug>#<id> review]", "BUILT-BY-ME", "Do not\npre-redact", "as-is",
                  "git diff <a>..<b>", "git diff HEAD", "docs/packs/data-pipelines.md"):
        assert token in text, token
    assert "never hard-codes" in text  # the crew comes from tq pack crew


def test_skill_records_and_routes_decisions() -> None:
    text = _read(SKILL / "SKILL.md")
    assert ("--reviewer pipeline-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N"
            in text)
    assert "TENTAQLES_HEADLESS" in text and "build-gate ask" in text and "AskUserQuestion" in text
    assert "judge flow" in text
    assert "standalone" in text.lower()


def test_skill_bash_blocks_follow_plugin_conventions() -> None:
    text = _read(SKILL / "SKILL.md")
    blocks = _bash_blocks(text)
    assert blocks
    for block in blocks:
        if "$TENTAQLES_PY" in block or "$_tqe" in block or re.search(r"\btq ", block):
            assert block.lstrip().startswith(SNIPPET), block[:80]
        for line in block.splitlines():
            # every tq call has an explicit timeout
            for m in re.finditer(r"(?:^|[;&|]\s*)(\S+\s+)?tq (decide|pack|hub)\b", line):
                assert "_to " in line, line
        assert "python " not in block and "python3 " not in block


def test_skill_never_runs_human_only_or_loosens_config() -> None:
    text = _read(SKILL / "SKILL.md")
    for forbidden in ("tq pack baseline", "tq hub answer", "build-gate approve", "promote apply"):
        assert forbidden not in text, forbidden
    assert "never\n  edit the manifest" in text or "never edit the manifest" in text
    assert "tq dotenv run" in text and "Never wrap" in text
    assert "tq/pipeline-run" in text


def test_skill_commit_has_the_main_master_guard() -> None:
    text = _read(SKILL / "SKILL.md")
    blocks = [b for b in _bash_blocks(text) if " commit " in b]
    assert len(blocks) == 1
    assert GUARD in blocks[0] and blocks[0].index(GUARD) < blocks[0].index(" commit ")
    assert "never `git add -A`" in text
    assert "never on main or master" in text and "uncommitted and say so" in text


def test_onramp_note_follows_the_convention() -> None:
    text = _read(ONRAMP)
    assert ONRAMP.name.startswith("data-")
    assert re.fullmatch(r"data-[a-z0-9-]+\.md", ONRAMP.name)
    sections = re.findall(r"^## (\S+)\s*$", text, re.M)
    assert sections and set(sections) <= {"build", "feature", "fix", "kickoff", "dod", "ship"}
    assert len(sections) == len(set(sections))
    kickoff = text.split("## kickoff", 1)[1].split("\n## ", 1)[0].strip()
    assert kickoff.startswith("Only when the stack has"), kickoff[:60]
    assert "may be off" in kickoff and "never a manifest edit" in kickoff
    for forbidden in ("tq pack baseline", "tq pack ml promote apply", "tq hub answer", "build-gate approve", ".env"):
        assert forbidden not in text, forbidden
    assert "tq pack crew --flags" in text
    for check in ("pack:data/airflow-dag", "pack:data/pipeline-backfill", "pack:data/pipeline-idempotency",
                  "pack:data/pipeline-pii-output", "pack:data/fabric-artifacts", "pack:data/synapse-artifacts",
                  "pack:data/notebook-outputs", "pack:data/dagster-validate"):
        assert check in text, check
    for block in _bash_blocks(text):
        assert block.lstrip().startswith(SNIPPET)
