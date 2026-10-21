---
name: kickoff
description: Start a new project the tentaqles way. Requires a planning-ready PRD (runs /tentaqles:prd otherwise), then writes the stack ADR and the project CLAUDE.md with its fixed sections, the threat model, the data classification and privacy record, the CI ratchet and supply-chain guardrails, plans the first slice from the release-1 FRs, runs the gated build engine and a direction audit. Use when the user says "new project", "start project X", "kick off", "greenfield", "bootstrap the repo", or "/tentaqles:kickoff".
argument-hint: '<idea | path to docs/prd.md> [--headless]'
---

# Kickoff

Two human stops: (1) data classes, threat-model accepted risks and the first
checkpoint plan together; (2) the result. Everything in between is decided and
logged as an assumption.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. Before a build slug exists
(the PRD check, the project shape, the threat model, the data classification,
the guardrails) a human stop or an unanswerable question is reported as your
final output and the turn ends. Once the build plan for the slug exists, every
human stop is a decision record written with `build-gate ask <slug>` exactly as
`skills/build/SKILL.md` ("Headless mode") describes, and the turn ends; the
`build-gate` prefix is defined there too.

## 1. Branch, then PRD, planning-ready or nothing

**The branch.** Choose the slug now (kebab-case from the project,
`billing-portal`). You never commit on `main` or `master`, and `/tentaqles:prd`
commits too, so open the branch before the PRD check and before anything
below is written:  run `/tentaqles:build` Phase 0 for the slug (it
creates the worktree and branch) and do every edit and commit of steps 1 to 5
inside that worktree, `/tentaqles:prd` included. If Phase 0 asks you to reopen Claude Code in the
worktree, stop there as it says; in the new session, re-run `/tentaqles:kickoff`
with the same `$ARGUMENTS`. Phase 0 step 3 sees the worktree already exists, so
the skill continues at the PRD check below (it is cheap and the files already
written are kept). `/tentaqles:build resume <slug>` is only for an existing
plan, which does not exist yet. If a build worktree is not wanted, use
`git switch -c feat/<slug>` (or switch to the existing branch) instead.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
```

Every later bash block uses `_tqe`, `root` and `TENTAQLES_PY` from the snippet
above. Blocks may run in fresh shells, so put its first two lines (the `_tqe=`
line and the `root=` line) at the top of each block.

Report `prd_exit` and quote the grader's JSON in your reply. The gate is
strict: only `prd_exit=0` (planning-ready) proceeds.

- `prd_exit=4` (no PRD): run `/tentaqles:prd` with `$ARGUMENTS` (inside the
  branch), then run the check again. Headless: report that a PRD is needed as your final output and
  end the turn.
- `prd_exit=3` (not planning-ready): stop here. List the open `NEEDS-DECISION`
  items and the FRs without acceptance checks, with how each could be settled
  (ask, research, prototype), and run `/tentaqles:prd` to settle them.
  Kickoff continues only at `prd_exit=0`. Headless: report the list as your
  final output and end the turn.
- `prd_exit=0`: continue.
- Any other exit code: report it and the output verbatim and stop.

If the repository does not exist yet, create it inside the client workspace
with `/tentaqles:add-project` first (it inherits the git identity from the
manifest; verify `git config user.email`).

## 2. Project shape

Read PRD §9 and the repository (`package.json`, `pyproject.toml`, `go.mod`,
lockfiles). Never assume a default stack; when §9 is silent, ask once
(headless: report it as your final output and end the turn).

- Write `docs/adr/0001-stack-and-layout.md` from
  `"$_tqe/templates/process/adr-0000-template.md"`: the stack, the layout,
  why, the alternatives. Status accepted.
- Write `CLAUDE.md` from `"$_tqe/templates/process/CLAUDE-project.md"`. The
  **Reading path** stays as the template says. **Must not trade off** starts
  from PRD §6 and §7: one line per property with the test that will enforce
  it, and every one of those tests is a case in the first checkpoint. **Project
  config**: fill the prose and the ```` ```json tq-project ```` block with the
  real commands (test, lint, typecheck, build, run), `architecture_paths`,
  `changelog`. Never write a placeholder as a command; leave no `<...>` in the
  block.
- Copy `"$_tqe/templates/process/LEARNINGS.md"` to `docs/LEARNINGS.md` and
  create `docs/specs/`, `docs/checkpoints/`, `docs/reviews/`.

## 3. Threat model

Run `/tentaqles:threat-model`: the four questions (what are we building, what
can go wrong, what will we do about it, did we do a good job) per trust
boundary from PRD §6 with STRIDE; it writes `docs/threat-model.md`. Any
unmitigated high-risk row becomes an FR (add it to the PRD with acceptance
checks) or a `NEEDS-DECISION` item for stop 1. If that skill is not installed,
tell the user, add the bullet line `- NEEDS-DECISION: threat model pending (settle by: run /tentaqles:threat-model when installed)` to `docs/prd.md` §10 (open questions), and continue.

## 4. Data classification

Run `/tentaqles:classify-data` from PRD §5: it writes the manifest `data:`
block, mirrors its paths into `classified_paths` of the `tq-project` block,
and writes `docs/privacy.md`. If any class is `pii`, `sensitive`, `financial`
or `children`, the privacy record must name purpose, legal basis, retention
and the DSAR path before planning continues. If that skill is not installed,
tell the user, add the bullet line `- NEEDS-DECISION: data classification pending (settle by: run /tentaqles:classify-data when installed)` to `docs/prd.md` §10, and continue. Changing `.tentaqles.yaml` changes its trust hash:
the human runs `tq allow <workspace>` as part of stop 1 (`<workspace>` is the
workspace name in `.tentaqles.yaml`, see `tq status`).

## 5. Guardrails before code

- `/tentaqles:ci-pipeline`: the ratchet with a zero baseline (cheapest now).
- `/tentaqles:supply-chain`: actions pinned to SHAs, `permissions:` on every
  workflow, lockfiles committed, SBOM on release, secret scanning as a
  required check. If that skill is not installed, tell the user, add the bullet line `- NEEDS-DECISION: supply-chain guardrails pending (settle by: run /tentaqles:supply-chain when installed)` to `docs/prd.md` §10, and continue.
- `tq settings render <workspace>` for deny-first permissions. `<workspace>` is
  the workspace name in `.tentaqles.yaml` (see `tq status`); the command writes
  the identity's settings outside the repository, so there is nothing of it to
  stage.
- `.gitignore` entries: `.env*` (keep `.env.example`), `.claude/build/`,
  `.claude/settings.local.json`.

Commit explicit paths, inside the branch from step 1 (never on `main` or
`master`):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" add CLAUDE.md docs .github .gitignore
git -C "$root" commit -m "chore(kickoff): project shape, threat model, privacy record, guardrails"
```

Stage `.tentaqles.yaml` only if this repository is where the manifest lives.

## 6. First slice and stop 1

Hand off to `/tentaqles:build` Phase 1 (Phase 0 is already done) with the PRD
path as the spec and `workflow=kickoff`. The planner writes top-level
`"workflow": "kickoff"`, `"hotfix": false` (a kickoff is never a hotfix) and
`"spec": "<path>"` (`spec=<path>`, the PRD path); pass exactly those. Tell the
planner to derive checkpoints from the release-1 FRs of PRD §11, checkpoint 1
being the thinnest slice that proves the stack end to end, and to include the
"Must not trade off" tests in it. If you write any test before the planner
runs, the orchestrator adds that test file path to the checkpoint's `tests` in
`docs/checkpoints/<slug>.json` before the human approves: the gate locks only
files the approved plan lists, and refuses `lock-tests` until the plan is
approved, so run `lock-tests` right after approval, before any implementer
starts.

**Human stop 1** shows, in one `AskUserQuestion`: the data classes and the
privacy record summary, the threat model's accepted risks, the checkpoint plan
with its verify commands, the assumptions and every pending `NEEDS-DECISION`
item of `docs/prd.md` §10, listed as "settle or explicitly accept before approving"; the human's approval note records the acceptance. On approve, the human runs `build-gate approve <slug>` and
`tq allow <workspace>` in their terminal; you cannot approve. Headless: the
`plan-approval` record once the plan exists (its text lists the same pending
items), the final output before.

## 7. Engine

Phases 2 and 3 of the build skill. The crew for a kickoff always includes
`spec-critic`; `security-reviewer` and `privacy-reviewer` join by triage flags
and classified paths.

## 8. Direction, stop 2, ship

Run `/tentaqles:direction <slug>`, passing the build slug: every release-1 FR graded, unplanned work listed,
`ON COURSE | DRIFTING | OFF COURSE`. Fix-code items go back into the engine as
new checkpoints; Update-PRD and Decide items go to the user at **stop 2**
(engine Phase 4; headless: record the verdict and these items in the `result`
decision record with `build-gate ask <slug> --kind result`, as the build skill
describes, and end the turn) together with the DoD record
(`/tentaqles:dod <slug>`). If a DoD item cannot be fixed, you never run `build-gate dod <slug> --override`:
the human runs it in their own terminal (the dod skill, step 3). Then `/tentaqles:ship`.

## Never

- plan from a PRD that is not planning-ready
- commit on `main` or `master`
- assume a stack, a cloud or a database the PRD and the repo do not name
- stage with `git add -A` or `git add .`; commit `.env*` or `.claude/build/`
- use `--no-verify`, or read `.env*` (`tq dotenv run -- <cmd>`)
