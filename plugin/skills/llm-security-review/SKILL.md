---
name: llm-security-review
description: "Run the llm-security-reviewer agent on a diff that adds or changes LLM calls, prompts, agent tool definitions, MCP servers, RAG or vector indexing, or Claude Code hooks and skills, against the OWASP Top 10 for LLM Applications (2025) and for Agentic Applications (2026), and turn its findings into fixes. Use from /tentaqles:feature, /tentaqles:fix and /tentaqles:ship when the diff touches those, and standalone when the user says \"LLM security\", \"prompt injection review\", \"review this agent\", \"is this MCP server safe\", including for tentaqles itself."
argument-hint: '[--staged | --range A..B]'
---

# LLM and agent security review

Reviewer: `agents/llm-security-reviewer.md`; checklist:
`references/checklist.md` (rows `L1`…`L12`). Applies to this plugin and `tq`
as much as to client code: every hook, skill, memory write and model call here
is in scope (tentaqles itself).

## When

- The diff touches prompt files, tool or MCP definitions, `hooks.json`,
  `skills/`, `agents/`, RAG or embedding code, or a model-call site.
- On request, over `--staged` (the default), a `--range A..B`
  (`git diff A..B`), or the working tree (`git diff HEAD`).

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
# default mode is --staged; for --range A..B use: git -C "$root" diff A..B
git -C "$root" diff --staged > "<scratchpad>/llm-review.diff"
git -C "$root" diff --staged --name-only
echo "checklist=$_tqe/skills/llm-security-review/references/checklist.md"
```

The reviewer reads the diff as-is (personal data may be read locally); never
copy real secrets or personal data into reports, commits or memory.

## Feed the reviewer

Launch `llm-security-reviewer` with this input, each item found as shown:

- the diff file and the changed files (`git -C "$root" diff --name-only`);
- every prompt file or string: `grep -rnE "system_prompt|SYSTEM_PROMPT|messages *[=:\[]|prompts?/" "$root" --include=*.py --include=*.ts --include=*.md`;
- tool and MCP definitions: `grep -rnE "mcpServers|\.mcp\.json|tools *[=:\[]|tool_use|@tool" "$root"`, plus `hooks/hooks.json`;
- every model-call site in the diff: `grep -nE "anthropic|openai|messages\.create|chat\.completions|generateText|claude -p" <changed files>`;
- the spec slice, `CLAUDE.md`, `docs/LEARNINGS.md`, `docs/threat-model.md`,
  `docs/privacy.md` when it exists, and the checklist path.

Point it at the trust boundaries the change crosses (model provider, MCP
servers, hooks, memory store). Ask for the review contract output (first line
`RESULT: PASS`, `RESULT: FAIL` or `RESULT: BLOCKED`).

Data leaving through model calls is in scope: personal data that
`docs/privacy.md` does not allow to leave sent to a model, and secrets in
prompts. Memory, the vault and hook model calls always mask secrets and
checksummed identifiers, so personal data that reaches memory or the vault
unmasked is a finding whatever the record says about model calls.

## Report and act

Report the verdict as rows: the `RESULT` line, then every `F` row
(`file:line`, the problem, the fix) and every `U` row. Fix the `F` rows and
run the reviewer again until it returns `RESULT: PASS`. A `U` row about trust
in a provider or an MCP server is a decision for the user (record it as an ADR
when taken). A finding against hooks or the guard is a BLOCKER in tentaqles'
own repositories: hooks must fail closed and never widen permissions. Nothing
is recorded by this skill; the PR body quotes the verdict (the ship skill).
