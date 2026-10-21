---
name: threat-model
description: "Write or update docs/threat-model.md with the four threat-modelling questions answered per trust boundary using STRIDE, from the PRD's security section and the codebase. Unmitigated high-risk rows become PRD requirements or NEEDS-DECISION items; accepted risks get an owner and an expiry. Use when the user says \"threat model\", \"what can go wrong\", \"security design\", during /tentaqles:kickoff, or when a feature crosses a new trust boundary (new integration, new role, new data store)."
argument-hint: '[boundary name | "update"] [--headless]'
---

# Threat model

Four questions (what are we building, what can go wrong, what will we do
about it, did we do a good job), one STRIDE table per trust boundary. The
result is the checklist source the `security-reviewer` reads on every diff.

Run this inside the branch `/tentaqles:kickoff` or the feature flow already
opened. Never commit on main or master: if `git branch --show-current` says
`main` or `master`, stop and ask for a branch first (headless: write the file,
do not commit, and say so in the report).

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. Where a default exists, take
it and log the assumption at the end of `docs/threat-model.md`. Where none
exists, write a `NEEDS-DECISION` line (PRD §10) and list it in the final
report. Use
`build-gate ask <slug> --kind question --text "<the question>" --options <a>,<b>`
only when the caller passed the slug of an active build run; the `build-gate`
prefix is in `skills/build/SKILL.md`.

## 1. Inputs

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
ls "$root/docs/threat-model.md" 2>/dev/null || echo "no threat model yet: template=$_tqe/templates/process/threat-model.md"
echo "stride=$_tqe/skills/threat-model/references/stride.md"
```

Every later bash block uses `_tqe` and `root` from the snippet above. Blocks
may run in fresh shells, so put its first two lines (the `_tqe=` line and the
`root=` line) at the top of each block.

Read `docs/prd.md` §2 (roles), §5 (data), §6 (boundaries, authentication,
abuse cases), §9 (stack); the manifest `data:` block; the code's entry points
(routes, handlers, jobs, webhooks, CLI) and outbound calls (HTTP clients,
queues, third-party SDKs). Never assume a stack or a provider.

## 2. Boundaries

List every place data crosses between parties with different trust: browser to
API, API to database, API to each third party, job runner to storage, admin
tools, CI to cloud. For each: from, to, data classes crossing (from the
manifest), authentication in use. If the PRD §6 names a boundary the code does
not have yet, keep it (it is planned); if the code has one the PRD does not
name, add it and flag it for the PRD.

Headings follow the template: `B1 <from> -> <to>` with an ASCII arrow, one
heading per boundary, numbered in order. The same boundary is written
`browser->api` in a feature's `trust_boundaries_crossed`.

## 3. STRIDE per boundary

For each boundary, go through the six categories with the prompts in
`references/stride.md`. Write threats as things a tester can try. For each
threat: the mitigation, its row status, the owner. Row status is one of
`planned`, `done`, `accepted`, `superseded`, and Mitigation must hold what the
template says: `done` = file:line or test name; `planned` = the `FR-nn` that
will deliver it; `accepted` = who accepted and why, plus an entry under
Accepted risks; `superseded` = the row that replaces it. The header's
Document status (`draft | accepted | superseded`) is a different field: set it
to `draft` until the user accepts the document.

Ask the user one question at a time (`AskUserQuestion`) only where the code and
PRD leave a real choice (for example "is replaying a webhook within 5 minutes
acceptable?"); otherwise decide and log the assumption at the end of the file.
Headless: see the paragraph at the top.

## 4. Consequences

- Every `planned` mitigation for a high-risk threat (information disclosure of
  pii or financial data, elevation of privilege, tampering with money) becomes
  an FR with acceptance checks in `docs/prd.md`, or a `NEEDS-DECISION` line in
  PRD §10 when the user must choose.
- Every `done` mitigation that protects a property worth keeping becomes a
  line under `CLAUDE.md` "Must not trade off" naming its test.
- Accepted risks go in the table under Accepted risks with an expiry; an
  expired risk reopens as a `NEEDS-DECISION`.

## 5. Write and commit

Write `docs/threat-model.md` from the template (or update the existing one:
add boundaries and rows, never delete history; replaced rows get status
`superseded`). Then stage only the files this run changed, each named
explicitly: `docs/threat-model.md` always, plus `docs/prd.md` and `CLAUDE.md`
only if you edited them. Check with `git status --short` first.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" status --short
git -C "$root" add docs/threat-model.md   # add docs/prd.md and CLAUDE.md here only if this run edited them
git -C "$root" commit -m "docs(security): threat model <boundaries added or updated>"
```

Report: boundaries, high-risk rows and where each landed (FR, rule, accepted),
and the open `NEEDS-DECISION` count.
