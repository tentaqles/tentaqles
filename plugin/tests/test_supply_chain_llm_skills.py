# plugin/tests/test_supply_chain_llm_skills.py
"""Content checks for the supply-chain and llm-security-review skills and the ci-pipeline wording."""

from __future__ import annotations

from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def _flat(name: str) -> str:
    return " ".join(_body(name).split())


def test_supply_chain_handles_no_actions() -> None:
    body = _body("supply-chain")
    for token in ("references/checklist.md", "SBOM", "CycloneDX", "lockfile", "secret scanning", "Scorecard",
                  "tq/gha-branch-ref", "no GitHub Actions", "GitLab", "TENTAQLES_HEADLESS", "AskUserQuestion"):
        assert token in body, token
    assert "add .github/workflows/<changed-file>" in body and "add .github" + chr(10) not in body


def test_llm_security_review_skill_feeds_the_reviewer() -> None:
    body = _body("llm-security-review")
    for token in ("llm-security-reviewer", "references/checklist.md", "agentic", "record-review", "MCP", "hooks",
                  "tentaqles itself", "prompts.allow", "TENTAQLES_HEADLESS", "AskUserQuestion", "RESULT: PASS"):
        assert token in body, token


def test_llm_skill_judge_is_human_only_and_bundle_complete() -> None:
    body = _flat("llm-security-review")
    assert "judge is human-only: never run it" in body
    assert "build-gate ask <slug> --kind judge" in body
    for token in ("manifest `data:` block", "prompts.allow", "tool and MCP definitions", "model-call site",
                  "memory, the vault and hook model calls always mask", "triage_exit", "--range"):
        assert token in body, token


def test_supply_chain_per_ecosystem_and_resolution() -> None:
    body = _flat("supply-chain")
    for token in ("yarn install --immutable", "--require-hashes", "uv sync --locked", "poetry install --sync",
                  "go mod verify", "cargo build --locked", "MUTATING", "NEEDS-DECISION",
                  "docker buildx imagetools inspect", "crane digest", "git/ref/tags/", "git/tags/",
                  "Human-stop cases", "omit any path you did not change", "Never `git add -A`"):
        assert token in body, token


def test_record_command_follows_bootstrap_snippet() -> None:
    body = _body("llm-security-review")
    idx = body.index("build-gate.py")
    block_start = body.rfind("```bash", 0, idx)
    assert block_start != -1 and "_tqe=" in body[block_start:idx]


def test_ci_pipeline_says_branch_refs_are_refused() -> None:
    body = _body("ci-pipeline")
    assert "tq/gha-branch-ref" in body and "denies" in body


GUARD = 'br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on \'$br\': do not commit"; exit 1;; esac'


def _commit_block(body: str) -> str:
    import re
    blocks = [b for b in re.findall(r"```bash\n(.*?)```", body, re.S) if " commit " in b]
    assert len(blocks) == 1, len(blocks)
    return blocks[0]


def test_supply_chain_commit_has_the_main_master_guard() -> None:
    body = _body("supply-chain")
    block = _commit_block(body)
    assert GUARD in block and block.index(GUARD) < block.index(" commit ")
    assert "never on main or master" in body and "leave the changes\nuncommitted" in body


def test_llm_security_review_build_marker_and_record_ownership() -> None:
    body = _body("llm-security-review")
    flat = " ".join(body.split())
    assert "[checkpoint <slug>#<id> review]" in body
    assert "Inside a build the build orchestrator runs `record-review`" in flat
    assert "this skill runs it when standalone with a slug" in flat
    assert "data.prompts.allow" in body
    assert body.count("prompts.allow") == body.count("data.prompts.allow")
