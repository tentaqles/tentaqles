---
name: feature
description: Add a feature from a PRD requirement through the gated build engine. Locates the FR (or adds one first), interviews the user on every business-rule branch with grill-me, writes a spec slice with binary acceptance checks and the data classes and trust boundaries it touches, pre-selects the reviewer crew by risk, plans checkpoints, runs the engine and the definition of done. Use when the user says "add feature", "implement FR-07", "build X", "new endpoint", "/tentaqles:feature", or describes behaviour the product should gain.
argument-hint: '<description | FR-nn> [--headless]'
---

# Feature

Two human stops: approve the checkpoint plan; accept the result. Everything
else is decided and logged as an assumption.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. Before a build slug exists
(the PRD check, the interview, the spec slice, the first plan) a human stop or
an unanswerable question is reported as your final output and the turn ends.
Once the build plan for the slug exists, every human stop is a decision record
written with `build-gate ask <slug>` exactly as `skills/build/SKILL.md`
("Headless mode") describes, and the turn ends; the `build-gate` prefix is
defined there too.

## 1. Locate it in the PRD

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
grep -n "^### FR-" "$root/docs/prd.md" 2>/dev/null
```

Every later bash block uses `_tqe`, `root` and `TENTAQLES_PY` from the snippet
above. Blocks may run in fresh shells, so put its first two lines (the `_tqe=`
line and the `root=` line) at the top of each block.

Note: the grader's exit code decides whether a feature may start; you never judge
the PRD's readiness yourself. Quote its JSON output in your reply.

- `prd_exit=0` (planning-ready): continue.
- `prd_exit=3` (a PRD exists but is not planning-ready): do not start the
  feature. Report the blockers from the JSON and point to `/tentaqles:prd` to
  settle them and grade again. Headless: report them as your final output and
  end the turn.
- `prd_exit=4` (legacy repository, no PRD): continue; the spec slice says
  `derived_from: no PRD: legacy repository` and you mention once that
  `/tentaqles:kickoff` can backfill a PRD later. If `CLAUDE.md` has no
  ```` ```json tq-project ```` block, add one now from
  `templates/process/CLAUDE-project.md` with the real test command (read
  `package.json`, `pyproject.toml`, `go.mod`, the CI workflow; never guess) so
  the definition of done can run it.
- Any other exit code: report it and the output verbatim and stop.

With exit 0, find the FR in `$ARGUMENTS` or by reading §4. **A feature with no
FR is first a PRD change, never code**: add `### FR-nn` with acceptance checks
to `docs/prd.md` on the feature branch, cite the goal it serves, run the grader
again (it must still exit 0) and commit it (`docs(prd): FR-nn <title>`). If the
PRD has open `NEEDS-DECISION` items that touch this feature, the grader will
not exit 0 until they are settled: send the user to `/tentaqles:prd`.

## 2. Interview

Run the `grill-me` skill on this feature: business-rule branches, invalid
states, permissions, time and locale, money, concurrency, failure handling.
Answers become the rules table and the acceptance checks of the spec slice.
Anything the user cannot decide is a `NEEDS-DECISION` line in the slice and
**blocks planning** until settled (offer: ask someone, research, prototype).
Headless: report the unanswered questions as your final output and end the turn.

## 3. Spec slice and pre-check

Slug: kebab-case from the feature (`export-csv`). Write `docs/specs/<slug>.md`
from `"$_tqe/templates/process/spec-slice.md"`: `derived_from: PRD §4 FR-nn`,
`data_classes_touched` (from the manifest `data:` block; `tq doctor` warns when
PII-looking columns have no block), `trust_boundaries_crossed` (from
`docs/threat-model.md`), behaviour, binary acceptance checks, out of scope,
migration plan when schema files will change.

Pre-select the reviewer crew before any code exists, so the plan already lists
who will review: list the files you expect to touch and run

```bash
tq decide triage; echo "triage_exit=$?"
```

on the current tree (the PRD and spec edits), then apply the crew rule from
the build skill: `spec-critic` always for a feature; `security-reviewer` on
auth, secrets, api, state, destructive flags, workflow or dependency changes;
`privacy-reviewer` when `data_classes_touched` is not empty or the files match
`classified_paths`; `llm-security-reviewer` when the feature adds LLM calls,
tools, MCP servers, hooks or skills; `breaker` when the plan marks a checkpoint
`runnable` and Project config has a `run` command. Write the expected crew
into the spec slice under Assumptions.

Commit (omit `docs/prd.md` if it did not change):

```bash
git -C "$root" add docs/specs/<slug>.md docs/prd.md
git -C "$root" commit -m "spec(<slug>): slice for FR-nn"
```

## 4. Plan and engine

Hand off to `/tentaqles:build` Phase 0 (worktree) and Phase 1 with the spec
path and `workflow=feature`. The planner writes top-level `"workflow":
"feature"`, `"hotfix": false` (`hotfix=no`; a feature is never a hotfix) and
`"spec": "<path>"` (`spec=<path>`, the slice's path); pass exactly those. The
planner reads the spec slice; every acceptance check must appear in some
checkpoint's `tests`. If you write any test before the planner runs, the
orchestrator adds that test file path to the checkpoint's `tests` in
`docs/checkpoints/<slug>.json` before the human approves: the gate locks only
files the approved plan lists, and refuses `lock-tests` until the plan is
approved, so run `lock-tests` right after approval, before any implementer
starts. **Human stop 1** is the plan approval in their terminal
(`build-gate approve <slug>`; you cannot approve). Headless: the
`plan-approval` record once the plan exists, the final output before. Then
Phases 2 and 3 as the build skill describes: tests first and locked,
implementer, behaviour gate, crew review with every verdict recorded, fixer
loop.

## 5. Definition of done and stop 2

`/tentaqles:dod <slug>` at the last checkpoint. The gate requires an ADR under
`docs/adr/` when files matching `architecture_paths` changed, `docs/privacy.md`
and the manifest when a data class or retention changed, a `## Migration plan`
in the spec when migration files changed, and the changelog entry. If an item
cannot be fixed, you never run `build-gate dod <slug> --override`: the human
runs it in their own terminal (the dod skill, step 3). **Human stop 2**
(engine Phase 4; headless: the `result` record), then `/tentaqles:ship`; the PR
body quotes the DoD record.

## Never

- write code for a feature that has no FR and no spec slice
- start a feature while the PRD grader exits 3, or declare a PRD ready yourself
- mark an acceptance check done without a test the gate ran
- `git add -A`, `git add .`, `--no-verify`, reading `.env*` (`tq dotenv run -- <cmd>`)
