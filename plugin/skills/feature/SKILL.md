---
name: feature
description: Add a feature from a PRD requirement through the gated build engine. Locates the FR (or adds one first), interviews the user on every business-rule branch with grill-me, writes a spec slice with binary acceptance checks and the data classes and trust boundaries it touches, pre-selects the reviewer crew by risk, plans checkpoints, runs the engine and the definition of done. Use when the user says "add feature", "implement FR-07", "build X", "new endpoint", "/tentaqles:feature", or describes behaviour the product should gain.
argument-hint: '<description | FR-nn> [--headless]'
---

# Feature

Two human approval stops: approve the checkpoint plan; accept the result. The
interview and any `NEEDS-DECISION` question are inputs you collect, not stops.
Anything else the PRD and the user are silent on is decided and logged as an
assumption in the spec slice.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. Before a build slug exists
(the PRD check, the interview, the spec slice, the first plan) a human stop or
an unanswerable question is reported as your final output and the turn ends.
Once the build plan for the slug exists, every human stop is a decision record
written with `build-gate ask <slug>` exactly as `skills/build/SKILL.md`
("Headless mode") describes, and the turn ends; the `build-gate` prefix is
defined there too. Headless output never tells the human to run a terminal
command or to re-run this skill (the build skill, "What headless output tells
the human"): when `TQ_HUB_TASK` is set, end with the question itself and say the
answer goes on the hub card (`/waiting`). The hub resumes this run with the
answer in the prompt, under "Notes from the human on this card".

**Hub task.** When `TQ_HUB_TASK` is set, the hub launched this run for one card. Read it with
`printf '%s\n' "${TQ_HUB_TASK:-}"` and pass `task=<that JSON>` to the planner together with
`workflow=feature`; the planner copies it verbatim as the plan's top-level `task`. When
`TQ_HUB_TASK` is not set, the plan has no `task`: if the planner wrote one anyway,
reject the plan and re-run the planner without it (the gate refuses such a plan too).

## Packs

Before step 1, run the on-ramp step of the `pack` skill (`skills/pack/SKILL.md`,
"On-ramp step"): `tq pack list --enabled --json` (timeout 10 s); for each enabled pack
read every `skills/pack/references/onramps/<pack>-*.md` and follow its `## feature`
section at the step it names. No pack enabled, or a tq without the `pack` command:
skip this. Reviewers: in step 3, add every reviewer that `tq pack crew --flags
<the flags the on-ramp files say this feature raises>` prints to the expected crew.

## 1. Locate it in the PRD

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
grep -n "^### FR-" "$root/docs/prd.md" 2>/dev/null
```

Every later bash block uses `_tqe`, `root` and `TENTAQLES_PY` from the snippet
above. Blocks may run in fresh shells, so put its first two lines (the `_tqe=`
line and the `root=` line) at the top of each block.

Report `prd_exit` and quote the grader's JSON in your reply.

**The feature branch.** Choose the slug now (kebab-case from the feature,
`export-csv`). You never commit on `main` or `master`. Before the first commit
below, open the feature branch: run `/tentaqles:build` Phase 0 for the slug
(it creates the worktree and branch) and do every edit and commit of steps 1 to
3 inside that worktree. If Phase 0 asks you to reopen Claude Code in the
worktree, stop there as it says; in the new session, re-run `/tentaqles:feature`
with the same `$ARGUMENTS`. Phase 0 step 3 sees the worktree already exists, so
the skill continues at this step 1 (the PRD check is cheap and the files already
written are kept). `/tentaqles:build resume <slug>` is only for an existing
plan, which does not exist yet.
If a build worktree is not wanted, use `git switch -c feat/<slug>` (or switch
to the existing branch) instead. "The feature branch" below means this branch.

- `prd_exit` 0 or 3 (a PRD exists): continue, planning-ready or not. Find the
  FR in `$ARGUMENTS` or by reading §4. **A feature with no FR is first a PRD
  change, never code**: add `### FR-nn` with acceptance checks (`-` or `*`
  bullet lines directly under a `**Acceptance checks**` heading, as the prd
  skill describes) to `docs/prd.md` on the feature branch, cite the goal it
  serves, and commit it (`docs(prd): FR-nn <title>`). If the PRD has open
  `NEEDS-DECISION` items that touch this feature, settle them first (ask the
  user, one at a time; headless: report them as your final output and end the
  turn). An open item touches this feature when it names this FR or its goal,
  or concerns a rule, role, data class, trust boundary or module the slice
  lists; when unsure, treat it as touching. Other open decisions do not block
  it: `/tentaqles:kickoff` keeps the strict planning-ready gate.
- `prd_exit=4` (legacy repository): continue; the spec slice says
  `derived_from: no PRD: legacy repository, see kickoff later` and you mention
  once that `/tentaqles:kickoff` can backfill a PRD later. If `CLAUDE.md` has
  no ```` ```json tq-project ```` block, add one from
  `templates/process/CLAUDE-project.md` with the real test command (read
  `package.json`, `pyproject.toml`, `go.mod`, the CI workflow; never guess).
  If no real test command can be found, ask the user (headless: report it as
  your final output and end the turn); never write a placeholder as a command.
  Delete every optional key you cannot fill (`lint`, `typecheck`, `build`,
  `run`), keep `architecture_paths` and `classified_paths` as `[]` unless you
  know them, and leave no `<...>` in the block. Add `CLAUDE.md` to the spec
  commit in step 3 so the definition of done can run the command.
- Any other exit code: report it and the output verbatim and stop.

## 2. Interview

Run the `grill-me` skill on this feature: business-rule branches, invalid
states, permissions, time and locale, money, concurrency, failure handling.
Answers become the rules table and the acceptance checks of the spec slice.
If an answer changes a rule, amend the FR bullets in `docs/prd.md` and commit
the amendment before planning. Anything the user cannot decide is a
`NEEDS-DECISION` line in the slice and **blocks planning** until settled
(offer: ask someone, research, prototype). Headless: report the unanswered
questions as your final output and end the turn.

## 3. Spec slice and expected crew

Write `docs/specs/<slug>.md` from `"$_tqe/templates/process/spec-slice.md"`:
`Status: draft`, `derived_from: PRD §4 FR-nn`, `data_classes_touched` (from the
manifest `data:` block; `tq doctor` warns when PII-looking columns have no
block), `trust_boundaries_crossed` (from `docs/threat-model.md`), Problem,
behaviour (the rules table), binary acceptance checks (`- [ ]` bullets), out of
scope, Open questions (`NEEDS-DECISION`), migration plan when schema files
will change, Assumptions.

Pre-select the reviewer crew now, from the files you expect to touch and the
slice's data classes, so the plan already lists who will review: `spec-critic`
always for a feature; `security-reviewer` when the feature touches auth,
secrets, an API, state, destructive operations, workflows or dependencies, or
crosses a trust boundary; `privacy-reviewer` when `data_classes_touched` is not
empty or the expected files match `classified_paths`; `llm-security-reviewer`
when the feature adds prompts, model calls, tools, MCP servers, hooks or
skills; `breaker` when the plan will mark a checkpoint `runnable` and Project
config has a `run` command. Write the expected crew into the slice under
Assumptions. `tq decide triage` runs later, in build Phase 2, on the real diff
of each checkpoint, where its flags apply as the build skill describes; it is
not run now, because the tree holds only documents.

Commit (add `CLAUDE.md` only if you changed it for a legacy repository):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" add docs/specs/<slug>.md
git -C "$root" commit -m "spec(<slug>): slice for FR-nn"
```

## 4. Plan and engine

Hand off to `/tentaqles:build` Phase 1 with the spec path and
`workflow=feature` (Phase 0 is already done). The planner writes top-level
`"workflow": "feature"`, `"hotfix": false` (`hotfix=no`; a feature is never a
hotfix) and `"spec": "<path>"` (`spec=<path>`, the slice's path); pass exactly
those. The planner reads the spec slice; every acceptance check must appear in
some checkpoint's `tests`. If you write any test before the planner runs, the
orchestrator adds that test file path to the checkpoint's `tests` in
`docs/checkpoints/<slug>.json` before the human approves: the gate locks only
files the approved plan lists, and refuses `lock-tests` until the plan is
approved, so run `lock-tests` right after approval, before any implementer
starts. **Human stop 1** is the plan approval in their terminal
(`build-gate approve <slug>`; you never run it, and the hook refuses it to you). Headless: the
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

- write code for a feature that has no FR in `docs/prd.md`, or no committed spec slice (on a legacy repository, exit 4, the slice's `derived_from` note stands in for the FR)
- commit on `main` or `master`
- mark an acceptance check done without a test the gate ran
- `git add -A`, `git add .`, `--no-verify`, reading `.env*` (`tq dotenv run -- <cmd>`)
