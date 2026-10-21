# plugin/tests/test_supply_chain_llm_skills.py
"""Content checks for the supply-chain and llm-security-review skills and the ci-pipeline wording."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def test_supply_chain_handles_no_actions() -> None:
    body = _body("supply-chain")
    for token in ("references/checklist.md", "SBOM", "CycloneDX", "lockfile", "secret scanning", "Scorecard",
                  "tq/gha-branch-ref", "no GitHub Actions", "GitLab", "TENTAQLES_HEADLESS", "AskUserQuestion"):
        assert token in body, token
    assert "add .github/workflows/<file>" in body and "add .github" + chr(10) not in body


def test_llm_security_review_skill_feeds_the_reviewer() -> None:
    body = _body("llm-security-review")
    for token in ("llm-security-reviewer", "references/checklist.md", "agentic", "record-review", "MCP", "hooks",
                  "tentaqles itself", "prompts.allow", "TENTAQLES_HEADLESS", "AskUserQuestion", "RESULT: PASS"):
        assert token in body, token


def test_record_command_follows_bootstrap_snippet() -> None:
    body = _body("llm-security-review")
    idx = body.index("build-gate.py")
    block_start = body.rfind("```bash", 0, idx)
    assert block_start != -1 and "_tqe=" in body[block_start:idx]


def test_ci_pipeline_says_branch_refs_are_refused() -> None:
    body = _body("ci-pipeline")
    assert "tq/gha-branch-ref" in body and "denies from the next tq release" in body
