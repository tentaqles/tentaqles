---
name: feature
description: Add a feature from a PRD requirement. Locates the FR (or adds one first), interviews the user on every business-rule branch with grill-me, writes a spec slice with binary acceptance checks and the data classes and trust boundaries it touches, writes the tests first, implements, has the reviewer agents read the diff, runs the definition of done and ships. Use when the user says "add feature", "implement FR-07", "build X", "new endpoint", "/tentaqles:feature", or describes behaviour the product should gain.
argument-hint: '<description | FR-nn>'
---

# Feature

One human stop: approve the spec slice before any test or code. The
interview and any `NEEDS-DECISION` question are inputs you collect, not stops.
Anything else the PRD and the user are silent on is decided and logged as an
assumption in the spec slice.

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
below, open the feature branch with `git switch -c feat/<slug>` (or switch to
the existing branch). "The feature branch" below means this branch.

- `prd_exit` 0 or 3 (a PRD exists): continue, planning-ready or not. Find the
  FR in `$ARGUMENTS` or by reading §4. **A feature with no FR is first a PRD
  change, never code**: add `### FR-nn` with acceptance checks (`-` or `*`
  bullet lines directly under a `**Acceptance checks**` heading, as the prd
  skill describes) to `docs/prd.md` on the feature branch, cite the goal it
  serves, and commit it (`docs(prd): FR-nn <title>`). If the PRD has open
  `NEEDS-DECISION` items that touch this feature, settle them first (ask the
  user, one at a time). An open item touches this feature when it names this
  FR or its goal, or concerns a rule, role, data class, trust boundary or
  module the slice lists; when unsure, treat it as touching. Other open
  decisions do not block it: `/tentaqles:kickoff` keeps the strict
  planning-ready gate.
- `prd_exit=4` (legacy repository): continue; the spec slice says
  `derived_from: no PRD: legacy repository, see kickoff later` and you mention
  once that `/tentaqles:kickoff` can backfill a PRD later. If `CLAUDE.md` has
  no `## Project config` section with a **Checks** line, add one from
  `templates/process/CLAUDE-project.md` with the real test command (read
  `package.json`, `pyproject.toml`, `go.mod`, the CI workflow; never guess).
  If no real test command can be found, ask the user; never write a
  placeholder as a command, and leave no `<...>` in the section. Add
  `CLAUDE.md` to the spec commit in step 3 so the definition of done can run
  the command.
- Any other exit code: report it and the output verbatim and stop.

## 2. Interview

Run the `grill-me` skill on this feature: business-rule branches, invalid
states, permissions, time and locale, money, concurrency, failure handling.
Answers become the rules table and the acceptance checks of the spec slice.
If an answer changes a rule, amend the FR bullets in `docs/prd.md` and commit
the amendment before the slice. Anything the user cannot decide is a
`NEEDS-DECISION` line in the slice and **blocks the slice** until settled
(offer: ask someone, research, prototype).

## 3. Spec slice and the human stop

Write `docs/specs/<slug>.md` from `"$_tqe/templates/process/spec-slice.md"`:
`Status: draft`, `derived_from: PRD §4 FR-nn`, `data_classes_touched` (the
personal-data classes the slice reads, writes, logs or exports, as
`docs/privacy.md` names them), `trust_boundaries_crossed` (from
`docs/threat-model.md`), Problem, behaviour (the rules table), binary
acceptance checks (`- [ ]` bullets), out of scope, Open questions
(`NEEDS-DECISION`), migration plan when schema files will change, Assumptions.

Pick the reviewers now, from the files you expect to touch and the slice's
data classes, and write them under Assumptions: `spec-critic` always for a
feature; `security-reviewer` when the feature touches auth, secrets, an API,
state, destructive operations, workflows or dependencies, or crosses a trust
boundary; `privacy-reviewer` when `data_classes_touched` is not empty;
`llm-security-reviewer` when the feature adds prompts, model calls, tools,
MCP servers, hooks or skills; `critic` when the change is larger than one
module or touches an architecture path of `CLAUDE.md`.

Commit (add `CLAUDE.md` only if you changed it for a legacy repository):

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" add docs/specs/<slug>.md
git -C "$root" commit -m "spec(<slug>): slice for FR-nn"
```

**Human stop:** show the slice (the rules, the acceptance checks, the
reviewers, the assumptions) and get a yes before any test or code. Set
`Status: approved` in the slice when they agree.

## 4. Tests first, then the code

For every acceptance check write the test at the lowest layer that proves it
(unit before API before CLI before UI), run it and see it fail. Then implement
the smallest change that makes the tests pass, one check at a time, and never
weaken a test to get there. Commit tests and code as you go, with explicit
paths. Run the project's test, lint and typecheck commands (the **Checks**
line of `## Project config` in `CLAUDE.md`) before you call it green. Record
every decision the spec left open under its Assumptions, and set
`Status: implemented` when every check has a passing test.

## 5. Review

Write the diff to a file under your scratchpad directory
(`git diff <base>...HEAD > <file>`, `<base>` the branch you forked from) and
run the reviewers chosen in step 3 on it, each with the diff path, the spec
slice, `CLAUDE.md`, `docs/LEARNINGS.md` and `docs/threat-model.md`. Fix every
`F` row (file, line, concrete fix) and re-run that reviewer until it returns
`RESULT: PASS`; a `U` row is a question for the user, never a guess. A
reviewer that answers `BUILT-BY-ME: yes` is discarded and run again fresh.

## 6. Definition of done, then ship

`/tentaqles:dod`: it requires an ADR under `docs/adr/` when files matching an
architecture path changed, `docs/privacy.md` when the data a change handles
changed, a `## Migration plan` in the spec when migration files changed, and
the changelog entry. Then `/tentaqles:ship`; the PR body quotes the record.

## Never

- write code for a feature that has no FR in `docs/prd.md`, or no committed spec slice (on a legacy repository, exit 4, the slice's `derived_from` note stands in for the FR)
- commit on `main` or `master`
- mark an acceptance check done without a test you ran and saw fail first
- `git add -A`, `git add .`, `--no-verify`, reading `.env*` (`tq dotenv run -- <cmd>`)
