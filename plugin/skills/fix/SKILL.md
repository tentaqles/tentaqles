---
name: fix
description: Fix a bug reproduce-first. Captures the report as a summary with personal data replaced by placeholders, has a tester write the smallest failing test at the lowest layer that shows the bug, locks that regression test, then finds the root cause and makes the smallest change that turns it green, reviewed by risk, with the lesson appended to docs/LEARNINGS.md. Use when the user says "fix this bug", "this fails", "it crashes", "/tentaqles:fix", pastes a stack trace, an error message or a failing command, or reports behaviour that does not match the spec. --hotfix skips the critic for a production incident, never the regression test.
argument-hint: "<report | pasted error | failing command> [--hotfix] [--headless]"
---

# Fix (reproduce first)

A bug is never fixed without a reproduction that stays in the suite. The
regression test is written and locked before any source file changes; the
implementer cannot edit it. Everything runs through the build engine with
`workflow=fix`, so the evidence, review and definition of done are recorded.

Human stops: (1) approve the reproduction, the severity and the one-checkpoint
plan; (2) accept the result. With `--hotfix`, stop 1 is the same
`build-gate approve` of the same signed plan; only the critic is skipped, and
the debt is recorded.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. Before a build slug exists
(intake, the reproduction attempt, the first plan) a human stop is reported as
your final output and the turn ends. Once the build plan for `fix-<slug>`
exists, every human stop is a decision record written with `build-gate ask`
exactly as `skills/build/SKILL.md` ("Headless mode") describes, and the turn
ends; the `build-gate` prefix is defined there too.

**Hub task.** When `TQ_HUB_TASK` is set, the hub launched this run for one card. Read it with
`printf '%s\n' "${TQ_HUB_TASK:-}"` and pass `task=<that JSON>` to the planner together with
`workflow=fix`; the planner copies it verbatim as the plan's top-level `task`. When
`TQ_HUB_TASK` is not set, the plan has no `task`: if the planner wrote one anyway,
reject the plan and re-run the planner without it (the gate refuses such a plan too). The card's kind is
`hotfix` exactly when the hub passed `--hotfix`; never infer it.

## 1. Intake

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
mkdir -p "$root/docs/specs"
echo "template=$_tqe/templates/process/fix-spec.md"
```

Slug: kebab-case from the symptom (`export-dup-rows`); the slug comes from the
symptom, never from a name, e-mail or id (it also lands in a memory pending
item). Write
`docs/specs/fix-<slug>.md` from the template: the report summarised with its
meaning kept and personal data replaced by placeholders (`<customer>`, `<email>`),
never pasted verbatim (the spec is committed; leave the original in the tracker
or a local file),
observed, expected (cite the PRD FR or the spec rule when one exists),
environment, `data_classes_touched`. If the bug exposed personal data to
someone who should not see it, this is also an incident: open
`docs/incident-playbook.md` now, start its clock, and tell the user before
anything else.

Severity: `critical` (data loss or exposure, everyone blocked), `high` (a core
flow broken), `medium`, `low`. `--hotfix` applies only when production is
affected now and the fix path is obvious. Set `hotfix=yes` only when the user
passed `--hotfix`; never infer it from severity. Without the flag, `hotfix=no`.
Write the user's reason in the spec header.

## 2. Reproduce first, then lock

Hand off to the engine: run `/tentaqles:build` Phase 0 with slug `fix-<slug>`,
then Phase 1 with the spec path, `workflow=fix` and `hotfix=yes|no`. The
planner writes exactly one checkpoint whose first test is the regression test,
with top-level `"workflow": "fix"` and `"hotfix"` set from the flag.

Before human stop 1, a tester subagent (`[checkpoint fix-<slug>#1 tests]`)
writes the smallest failing test at the lowest layer that shows the bug (unit
before API before CLI before UI), runs it, and reports the failure line.
The planner ran before the tester, so the orchestrator adds the tester's test
file path to the checkpoint's `tests` in `docs/checkpoints/fix-<slug>.json`
(Edit, or re-run the planner with the path) before the human approves: the gate
locks only files the approved plan lists. The gate also refuses `lock-tests`
until the plan is approved, so run `lock-tests` right after approval:

```bash
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" lock-tests fix-<slug> 1 <test file>
```

Run it as soon as `build-gate status fix-<slug>` shows `approved=yes`, before
any implementer starts.

**No reproduction, no fix.** If the tester cannot make a test fail, do not
touch source code. Report what was tried, what to instrument (a log line, a
data sample, a flag), and stop at human stop 1 with the options: add logging
and wait for the next occurrence, provide data, or accept a reproduction at a
higher layer. Never write a fix "while we wait" without a reproduction.

**Human stop 1** (build Phase 1 stop 1; headless: the `plan-approval` record
once the plan exists, the final output before): show the failing test and its
output, the severity, the one-checkpoint plan and its verify command. The
human approves in their terminal (`build-gate approve fix-<slug>`); you never run it (the hook refuses it to you).
For `--hotfix` the plan carries `"hotfix": true` with the review gate still
`pending`, and the gate freezes that flag into the signed approval; the
approval itself switches the review gate to `n/a` (the hook refuses `n/a` on a
plan the human has not approved as a hotfix). Tell the human plainly that they
are approving the plan knowing the review gate becomes `n/a` (no critic runs);
the regression test still has to pass.

## 3. Root cause

The implementer subagent (`[checkpoint fix-<slug>#1 implement]`) follows the
systematic discipline: read the error completely, reproduce it with the locked
test, trace the data back to where the bad value is born, compare with a
working path, one hypothesis at a time, the smallest change that makes the
locked test pass without changing other behaviour. It may not edit the locked
test (the hook blocks it); if it believes the test is wrong, it reports
file:line evidence and the human decides.

## 4. Review by risk

Steps c (behavior gate) and d (review gate) of `skills/build/SKILL.md` "Phase 2: Checkpoint loop". `tq decide triage` on the diff picks the depth:
low risk gets the light critic; high risk gets the full crew; files outside the
failing path add `spec-critic`. With an approved `--hotfix` plan the review
gate may be `n/a` and the critic is skipped, **never the behaviour gate**: the
locked regression test still has to pass through `build-gate verify`. Only
when the plan has `hotfix` true, after `build-gate verify` passes, record the
debt so nobody forgets (substitute `$root` for `<root>` and the slug):

```bash
echo '{"cwd": "<root>", "event": "pending", "data": {"description": "post-hotfix review of fix-<slug>: run the full crew on the diff and close the LEARNINGS entry", "priority": "high", "node_ids": ["docs/specs/fix-<slug>.md"]}}' | "$TENTAQLES_PY" "$_tqe/scripts/memory-bridge.py"
```

`wrap` and the session preamble surface it until resolved.

## 5. Done

Fill `## Root cause` and `## Lesson` in the fix spec and append the `L<n>`
line to `docs/LEARNINGS.md` (format in `templates/process/LEARNINGS.md`). If
the lesson already appears there, promote it to a Rule. If the bug came from a
gap in the PRD, the threat model or the privacy record, change that document in
the same branch. Run the definition of done (`/tentaqles:dod fix-<slug>`); if an
item cannot be fixed, you never run `build-gate dod fix-<slug> --override`:
the human runs it in their own terminal (the dod skill, step 3). Then
**human stop 2** (engine Phase 4; headless: the `result` record), then
`/tentaqles:ship`.

## Never

- fix without a locked failing reproduction, or edit the locked test
- skip the regression test for a hotfix (only the critic is skipped)
- `git add -A`, `git add .`, `--no-verify`, or reading `.env*` (`tq dotenv run -- <cmd>`)
- call it fixed in the transcript before `build-gate verify` and `build-gate dod` say so
