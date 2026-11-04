"""Content checks for the supply-chain and llm-security-review skills and the ci-pipeline wording."""

from __future__ import annotations

import re
from pathlib import Path

SKILLS = Path(__file__).resolve().parents[1] / "skills"
SNIPPET = '_tqe="${CLAUDE_PLUGIN_ROOT:-}"'


def _body(name: str) -> str:
    return (SKILLS / name / "SKILL.md").read_text(encoding="utf-8")


def _flat(name: str) -> str:
    return " ".join(_body(name).split())


def test_supply_chain_handles_no_actions() -> None:
    body = _body("supply-chain")
    for token in ("references/checklist.md", "SBOM", "CycloneDX", "lockfile", "secret scanning", "Scorecard",
                  "tq/gha-branch-ref", "no GitHub Actions", "GitLab"):
        assert token in body, token
    assert "add .github/workflows/<changed-file>" in body and "add .github" + chr(10) not in body
    for forbidden in ("TENTAQLES_HEADLESS", "--headless", "build-gate", "tq decide", "build engine", "AskUserQuestion"):
        assert forbidden not in body, forbidden
    assert "headless" not in body.lower()


def test_llm_security_review_skill_feeds_the_reviewer() -> None:
    body = _body("llm-security-review")
    for token in ("llm-security-reviewer", "references/checklist.md", "MCP", "hooks", "tentaqles itself",
                  "RESULT: PASS", "tool and MCP definitions", "model-call site", "--range", "--staged",
                  "docs/threat-model.md", "docs/privacy.md"):
        assert token in body, token
    for forbidden in ("record-review", "build-gate", "tq decide", "triage", "prompts.allow", "manifest `data:`",
                      "TENTAQLES_HEADLESS", "AskUserQuestion", "[checkpoint", "judge"):
        assert forbidden not in body, forbidden
    assert "Nothing is recorded by this skill" in _flat("llm-security-review")


def test_llm_security_review_diff_block_follows_bootstrap_snippet() -> None:
    body = _body("llm-security-review")
    blocks = re.findall(r"```bash\n(.*?)```", body, re.S)
    assert blocks and all(b.lstrip().startswith(SNIPPET) for b in blocks if "$_tqe" in b)
    assert "git -C \"$root\" diff --staged >" in body


def test_supply_chain_per_ecosystem_and_resolution() -> None:
    body = _flat("supply-chain")
    for token in ("yarn install --immutable", "--require-hashes", "uv sync --locked", "poetry install --sync",
                  "go mod verify", "cargo build --locked", "MUTATING", "NEEDS-DECISION",
                  "docker buildx imagetools inspect", "crane digest", "git/ref/tags/", "git/tags/",
                  "Human-stop cases", "omit any path you did not change", "Never `git add -A`",
                  "run the `security-reviewer` agent"):
        assert token in body, token


def test_ci_pipeline_says_branch_refs_are_refused_and_knows_no_packs() -> None:
    body = _body("ci-pipeline")
    assert "tq/gha-branch-ref" in body and "denies" in body
    assert "`security-reviewer`" in body
    for forbidden in ("tq pack", "tq decide", "packs."):
        assert forbidden not in body, forbidden


GUARD = 'br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on \'$br\': do not commit"; exit 1;; esac'


def _commit_block(body: str) -> str:
    blocks = [b for b in re.findall(r"```bash\n(.*?)```", body, re.S) if " commit " in b]
    assert len(blocks) == 1, len(blocks)
    return blocks[0]


def test_supply_chain_commit_has_the_main_master_guard() -> None:
    body = _body("supply-chain")
    block = _commit_block(body)
    assert GUARD in block and block.index(GUARD) < block.index(" commit ")
    flat = _flat("supply-chain")
    assert "never on main or master" in flat and "on main or master, leave the changes uncommitted and say so" in flat
