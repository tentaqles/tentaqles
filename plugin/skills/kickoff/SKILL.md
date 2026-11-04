---
name: kickoff
description: Start a new project the tentaqles way. Requires a planning-ready PRD (runs /tentaqles:prd otherwise), then writes the stack ADR and the project CLAUDE.md with its fixed sections, the threat model, the privacy record (classify-data), the CI ratchet and supply-chain guardrails, picks the first slice from the release-1 FRs and hands it to /tentaqles:feature, then runs a direction audit. Use when the user says "new project", "start project X", "kick off", "greenfield", "bootstrap the repo", or "/tentaqles:kickoff".
argument-hint: '<idea | path to docs/prd.md>'
---

# Kickoff

Two human stops: (1) the privacy record, the threat model's accepted risks
and the first slice together; (2) the result of the first feature. Everything
in between is decided and logged as an assumption.

## 1. Branch, then PRD, planning-ready or nothing

**The branch.** Choose the slug now (kebab-case from the project,
`billing-portal`). You never commit on `main` or `master`, and `/tentaqles:prd`
commits too, so open the branch before the PRD check and before anything
below is written: `git switch -c feat/<slug>` (or switch to the existing
branch) and do every edit and commit of steps 1 to 5 inside it,
`/tentaqles:prd` included.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
```

Every later bash block uses `_tqe`, `root` and `TENTAQLES_PY` from the snippet
above. Blocks may run in fresh shells, so put its first two lines (the `_tqe=`
line and the `root=` line) at the top of each block.

Report `prd_exit` and quote the grader's JSON in your reply. The gate is
strict: only `prd_exit=0` (planning-ready) proceeds.

- `prd_exit=4` (no PRD): run `/tentaqles:prd` with `$ARGUMENTS` (inside the
  branch), then run the check again.
- `prd_exit=3` (not planning-ready): stop here. List the open `NEEDS-DECISION`
  items and the FRs without acceptance checks, with how each could be settled
  (ask, research, prototype), and run `/tentaqles:prd` to settle them.
  Kickoff continues only at `prd_exit=0`.
- `prd_exit=0`: continue.
- Any other exit code: report it and the output verbatim and stop.

If the repository does not exist yet, create it inside the client workspace
with `/tentaqles:add-project` first (it inherits the git identity from the
manifest; verify `git config user.email`).

## 2. Project shape

Read PRD §9 and the repository (`package.json`, `pyproject.toml`, `go.mod`,
lockfiles). Never assume a default stack; when §9 is silent, ask once.

- Write `docs/adr/0001-stack-and-layout.md` from
  `"$_tqe/templates/process/adr-0000-template.md"`: the stack, the layout,
  why, the alternatives. Status accepted.
- Write `CLAUDE.md` from `"$_tqe/templates/process/CLAUDE-project.md"`. The
  **Reading path** stays as the template says. **Must not trade off** starts
  from PRD §6 and §7: one line per property with the test that will enforce
  it, and every one of those tests is in the first slice. **Project
  config**: fill the **Checks** line with the real commands (test, lint,
  typecheck), the architecture paths and the changelog file. Never write a
  placeholder as a command; leave no `<...>` in the section.
- Copy `"$_tqe/templates/process/LEARNINGS.md"` to `docs/LEARNINGS.md` and
  create `docs/specs/`.

## 3. Threat model

Run `/tentaqles:threat-model`: the four questions (what are we building, what
can go wrong, what will we do about it, did we do a good job) per trust
boundary from PRD §6 with STRIDE; it writes `docs/threat-model.md`. Any
unmitigated high-risk row becomes an FR (add it to the PRD with acceptance
checks) or a `NEEDS-DECISION` item for stop 1. If that skill is not installed,
tell the user, add the bullet line `- NEEDS-DECISION: threat model pending (settle by: run /tentaqles:threat-model when installed)` to `docs/prd.md` §10 (open questions), and continue.

## 4. Privacy record

Run `/tentaqles:classify-data` from PRD §5: it writes the data dictionary and
`docs/privacy.md` (the classes, where each lives, purpose, legal basis,
retention, the DSAR path, the incident authority). If any class is `pii`,
`sensitive`, `financial` or `children`, the record must name purpose, legal
basis, retention and the DSAR path before planning continues. If that skill
is not installed, tell the user, add the bullet line `- NEEDS-DECISION: data classification pending (settle by: run /tentaqles:classify-data when installed)` to `docs/prd.md` §10, and continue.

## 5. Guardrails before code

- `/tentaqles:ci-pipeline`: the ratchet. No code exists yet, so lint and
  complexity start at 0 and `coverage_pct` and `duplication_pct` start as
  `"value": null` (unmeasured), never a made-up 0: a placeholder that is not
  a real measurement fails the first CI run or makes the check a no-op. The
  first slice's PR runs `ratchet.py compare ... --update` and commits the
  measured baseline.
- `/tentaqles:supply-chain`: actions pinned to SHAs, `permissions:` on every
  workflow, lockfiles committed, SBOM on release, secret scanning as a
  required check. If that skill is not installed, tell the user, add the bullet line `- NEEDS-DECISION: supply-chain guardrails pending (settle by: run /tentaqles:supply-chain when installed)` to `docs/prd.md` §10, and continue.
- `tq settings render <workspace>` for the identity's settings. `<workspace>`
  is the workspace name in `.tentaqles.yaml` (see `tq status`); the command
  writes the identity's settings outside the repository, so there is nothing
  of it to stage.
- `.gitignore` entries: `.env*` (keep `.env.example`),
  `.claude/settings.local.json`.

Commit explicit paths, inside the branch from step 1 (never on `main` or
`master`):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
for p in CLAUDE.md docs/prd.md docs/adr docs/LEARNINGS.md docs/threat-model.md docs/privacy.md .gitignore .github; do
  [ -e "$root/$p" ] && git -C "$root" add -- "$p"  # skip what was not written (e.g. .github when CI was declined)
done
git -C "$root" commit -m "chore(kickoff): project shape, threat model, privacy record, guardrails"
```

Stage `.tentaqles.yaml` only if this repository is where the manifest lives
and you changed it (a changed manifest needs the user's `tq allow <workspace>`
in their own terminal; you never run it).

## 6. First slice and stop 1

Pick the first slice from the release-1 FRs of PRD §11: the thinnest slice
that proves the stack end to end, with the "Must not trade off" tests in it.
Name the FRs it covers and the ones that follow.

**Human stop 1** shows, in one `AskUserQuestion`: the privacy record summary,
the threat model's accepted risks, the first slice with its FRs, the
assumptions and every pending `NEEDS-DECISION` item of `docs/prd.md` §10,
listed as "settle or explicitly accept before approving"; the human's approval
note records the acceptance.

## 7. Feature

Run `/tentaqles:feature <FR-nn>` for the first slice, on this branch: it
writes the spec slice, the tests first, the code, runs the reviewers, the
definition of done and `/tentaqles:ship`.

## 8. Direction and stop 2

Run `/tentaqles:direction`: every release-1 FR graded, unplanned work listed,
`ON COURSE | DRIFTING | OFF COURSE`. Fix-code items go back to
`/tentaqles:feature`; Update-PRD and Decide items go to the user at
**stop 2**, together with the definition-of-done record.

## Never

- plan from a PRD that is not planning-ready
- commit on `main` or `master`
- assume a stack, a cloud or a database the PRD and the repo do not name
- stage with `git add -A` or `git add .`; commit `.env*`
- use `--no-verify`, or read `.env*` (`tq dotenv run -- <cmd>`)
