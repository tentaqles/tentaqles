---
name: llm-security-review
description: "Run the llm-security-reviewer agent on a diff that adds or changes LLM calls, prompts, agent tool definitions, MCP servers, RAG or vector indexing, or Claude Code hooks and skills, against the OWASP Top 10 for LLM Applications (2025) and for Agentic Applications (2026), and turn its findings into fixes. Used by the build engine on the agentic triage flag and standalone when the user says \"LLM security\", \"prompt injection review\", \"review this agent\", \"is this MCP server safe\", including for tentaqles itself."
argument-hint: '[--staged | --range A..B | <slug> <checkpoint id>] [--headless]'
---

# LLM and agent security review

Reviewer: `agents/llm-security-reviewer.md`; checklist:
`references/checklist.md` (rows `L1`…`L12`). Applies to this plugin and `tq`
as much as to client code: every hook, skill, memory write and Jev call here is
in scope (tentaqles itself).

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. Use
`build-gate ask <slug> --kind question --text "<the question>" --options <a>,<b>`
only with an active slug from a build run; otherwise report what is needed as
your final output and end the turn.

## When

- In the build engine: triage printed the `agentic` flag, or the diff touches
  prompt files, tool or MCP definitions, `hooks.json`, `skills/`, `agents/`,
  RAG or embedding code.
- Standalone on request: with no slug the skill reports findings and records
  nothing.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
# default mode is --staged; for --range A..B or a build run use --range <base>..HEAD
tq decide triage --staged; echo "triage_exit=$?"
echo "checklist=$_tqe/skills/llm-security-review/references/checklist.md"
```

Triage exit codes: `0` is light, `3` is full (flags such as `agentic` are
printed); any other code, or tq missing, means review fully. For `--range A..B`
or `<slug> <id>` run `tq decide triage --range <base>..HEAD` instead of
`--staged` and take the diff from `git -C "$root" diff <base>..HEAD`. The
reviewer reads the diff as-is (personal data may be read locally); never copy
real secrets or personal data into reports, commits or memory.

## Feed the reviewer

Launch `llm-security-reviewer` with this bundle, each item found as shown:

- the diff and the changed files (`git -C "$root" diff --name-only`);
- every prompt file or string: `grep -rnE "system_prompt|SYSTEM_PROMPT|messages *[=:\[]|prompts?/" "$root" --include=*.py --include=*.ts --include=*.md`;
- tool and MCP definitions: `grep -rnE "mcpServers|\.mcp\.json|tools *[=:\[]|tool_use|@tool" "$root"`, plus `hooks/hooks.json`;
- every model-call site in the diff: `grep -nE "anthropic|openai|messages\.create|chat\.completions|generateText|claude -p|tq decide" <changed files>`;
- the manifest `data:` block (`data.classes` and `data.prompts.allow`) from `.tentaqles.yaml`;
- the spec slice, `CLAUDE.md`, `docs/LEARNINGS.md`, `docs/threat-model.md` and the checklist path.

Point it at the trust boundaries the change crosses (model provider, MCP
servers, hooks, memory store). Inside a build, the reviewer prompt starts with the marker
`[checkpoint <slug>#<id> review]` (shape required by `skills/build/SKILL.md`;
the gate matches it). Ask for the review contract output (first line
`RESULT: PASS`, `RESULT: FAIL` or `RESULT: BLOCKED`).

Data leaving through model calls is in scope: classified data
(`data.classes`) sent to a model outside `data.prompts.allow`, and secrets in
prompts. `data.prompts.allow` governs model calls only; memory, the vault and hook
model calls always mask declared pii/financial classes, so a class listed in
`data.prompts.allow` is still a finding if it reaches memory or the vault unmasked.

## Record and act

The verdict is recorded with the reviewer's RESULT line and its counts.
Inside a build the build orchestrator runs `record-review` (per
`skills/build/SKILL.md`); this skill runs it when standalone with a slug (the
snippet opens the block; blocks run in fresh shells):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" record-review <slug> <id> --reviewer llm-security-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
```

then the fixer loop (see `skills/build/SKILL.md`). A BLOCKED result goes to
the human with the `U` rows; the human settles it with `build-gate judge` in
their own terminal (judge is human-only: never run it yourself). Headless with
an active slug:
`build-gate ask <slug> --kind judge --checkpoint <id> --reviewer llm-security-reviewer --text "<the U rows>" --options PASS,FAIL`,
then end the turn. Standalone: fix the findings; a `U` row about trust in a
provider or an MCP server is a decision for the user (record it as an ADR when
taken). A finding against hooks or the guard is a BLOCKER in tentaqles' own
repositories: hooks must fail closed and never widen permissions.
