---
name: fix
description: Fix a bug reproduce-first. Captures the report as a summary with personal data replaced by placeholders, writes the smallest failing regression test at the lowest layer that shows the bug, then finds the root cause and makes the smallest change that turns it green, has the reviewer agents read the diff when it touches auth, personal data or model calls, and appends the lesson to docs/LEARNINGS.md. Use when the user says "fix this bug", "this fails", "it crashes", "/tentaqles:fix", pastes a stack trace, an error message or a failing command, or reports behaviour that does not match the spec.
argument-hint: "<report | pasted error | failing command>"
---

# Fix (reproduce first)

A bug is never fixed without a reproduction that stays in the suite. The
regression test is written before any source file changes, and the fix never
edits it. One human stop: approve the reproduction and the severity before
the fix; the result is reviewed by risk and shipped as a pull request.

## 1. Intake

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
mkdir -p "$root/docs/specs"
echo "template=$_tqe/templates/process/fix-spec.md"
git -C "$root" branch --show-current
```

Slug: kebab-case from the symptom (`export-dup-rows`); the slug comes from the
symptom, never from a name, e-mail or id (it also lands in a memory pending
item). Never commit on `main` or `master`: open `fix/<slug>` first
(`git switch -c fix/<slug>`, or switch to the existing branch). Write
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
flow broken), `medium`, `low`. Write it in the spec header.

## 2. Reproduce first

Write the smallest failing test at the lowest layer that shows the bug (unit
before API before CLI before UI), run it, and quote the failure line in your
reply. Commit the test on its own (`test(fix-<slug>): reproduce <symptom>`,
explicit paths), so the diff of the fix holds only the fix.

**No reproduction, no fix.** If you cannot make a test fail, do not touch
source code. Report what was tried, what to instrument (a log line, a data
sample, a flag), and stop at the human stop with the options: add logging and
wait for the next occurrence, provide data, or accept a reproduction at a
higher layer. Never write a fix "while we wait" without a reproduction.

**Human stop:** show the failing test and its output, the severity and the
fix you intend, and get a yes before changing any source file.

## 3. Root cause

Follow the systematic discipline: read the error completely, reproduce it
with the regression test, trace the data back to where the bad value is born,
compare with a working path, one hypothesis at a time, the smallest change
that makes the regression test pass without changing other behaviour. The
fix never edits the regression test; if you believe the test is wrong, show
file:line evidence and let the user decide. Run the project's test, lint and
typecheck commands (the **Checks** line of `## Project config` in `CLAUDE.md`)
before you call it green.

## 4. Review by risk

Write the diff to a file under your scratchpad directory
(`git diff <base>...HEAD > <file>`, `<base>` the branch you forked from) and
run the reviewer agents on it, each with the diff path, the fix spec,
`CLAUDE.md`, `docs/LEARNINGS.md` and `docs/threat-model.md`, when the fix
touches:

- auth or permissions, secrets, a public API, shared state or destructive
  commands: `security-reviewer`;
- personal data read, written, logged or exported: `privacy-reviewer`;
- prompts, model calls, tools, MCP servers, hooks or skills:
  `llm-security-reviewer`;
- files outside the failing path: `spec-critic` (the fix spec says why each
  one changed) and `critic`.

Fix every `F` row and re-run until `RESULT: PASS`; a `U` row is a question
for the user. A one-line fix on the failing path with none of the areas above
gets your own careful read of the diff instead.

## 5. Done

Fill `## Root cause` and `## Lesson` in the fix spec and append the `L<n>`
line to `docs/LEARNINGS.md` (format in `templates/process/LEARNINGS.md`). If
the lesson already appears there, promote it to a Rule. If the bug came from a
gap in the PRD, the threat model or the privacy record, change that document in
the same branch. Run the definition of done (`/tentaqles:dod`), then
`/tentaqles:ship`.

## Never

- fix without a failing reproduction, or edit the regression test to make it pass
- `git add -A`, `git add .`, `--no-verify`, or reading `.env*` (`tq dotenv run -- <cmd>`)
- call it fixed in the transcript before the regression test passes and the suite is green
