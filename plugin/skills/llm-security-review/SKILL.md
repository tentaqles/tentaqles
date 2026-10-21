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
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
tq decide triage --staged; echo "triage_exit=$?"
echo "checklist=$_tqe/skills/llm-security-review/references/checklist.md"
```

## Feed the reviewer

Launch `llm-security-reviewer` with the diff (bundle path or a redacted diff
file under your scratchpad), the changed files, the spec slice, `CLAUDE.md`,
`docs/LEARNINGS.md`, `docs/threat-model.md` and the checklist path. Point it
at the trust boundaries the change crosses (model provider, MCP servers, hooks,
memory store). Ask for the review contract output (first line `RESULT: PASS`,
`RESULT: FAIL` or `RESULT: BLOCKED`).

Data leaving through model calls is in scope: classified data
(`data.classes`) sent to a model outside `prompts.allow`, and secrets in
prompts. `prompts.allow` governs model calls only; memory, the vault and hook
model calls always mask declared pii/financial classes, so a class listed in
`prompts.allow` is still a finding if it reaches memory or the vault unmasked.

## Record and act

Inside a build, record the verdict (the snippet opens the block; blocks run in
fresh shells):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" record-review <slug> <id> --reviewer llm-security-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
```

then the fixer loop or `build-gate judge` as usual. Standalone: fix the
findings; a `U` row about trust in a provider or an MCP server is a decision
for the user (record it as an ADR when taken). A finding against hooks or the
guard is a BLOCKER in tentaqles' own repositories: hooks must fail closed and
never widen permissions.
