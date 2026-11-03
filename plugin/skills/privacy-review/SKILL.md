---
name: privacy-review
description: "Run the privacy-reviewer agent on a diff that touches personal data (fields, tables, log lines, analytics events or exports with personal-data shapes) and turn its findings into fixes and privacy-record updates. Use from /tentaqles:dod when the privacy row is missing, from /tentaqles:feature, /tentaqles:fix and /tentaqles:ship when the diff handles personal data, and standalone when the user says \"privacy review\", \"LGPD check\", \"GDPR check\", \"is this compliant\", or before shipping a change to personal data handling."
argument-hint: '[--staged | --range A..B]'
---

# Privacy review

The reviewer is `agents/privacy-reviewer.md`; its checklist is
`references/checklist.md` (rows `P1`...`P11`). This skill decides when it runs,
feeds it, and reports the verdict.

## When it runs

- From `/tentaqles:dod`, `/tentaqles:feature`, `/tentaqles:fix` or
  `/tentaqles:ship`: when the diff reads, writes, logs, exports or sends a
  field that looks personal (e-mail, CPF, phone, address, birth date, card),
  touches a path or table that `docs/privacy.md` lists, or changes a retention.
  A documentation-only diff that changes only docs/privacy.md does not select it.
- Standalone: on request, over `--staged` (`git diff --staged`), a `--range`
  (`git diff <a>..<b>`), or the working tree (`git diff HEAD`).

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" diff --staged > "<scratchpad>/privacy-review.diff"
git -C "$root" diff --staged --name-only
echo "checklist=$_tqe/skills/privacy-review/references/checklist.md"
```

## Feed the reviewer

Launch the `privacy-reviewer` agent with the diff as-is, written to a file
under your scratchpad directory. Personal data may be read and processed
locally, and the reviewer needs the real field shapes to judge P5 and P6, so
do not pre-redact and do not refuse to read it. The care is at the exits:
commits, pushes, PR text, memory, fixtures; a copy that leaves the session
(PR text, a bug report) carries placeholders, never the values. Also pass the
list of changed files, the spec slice (`data_classes_touched`), the paths
`docs/privacy.md`, `CLAUDE.md`, `docs/LEARNINGS.md`, and the checklist path,
and say which changed files matched a path or table of `docs/privacy.md`
(list them in the prompt). Ask for the review contract output. Never paste
`.env*` contents.

## Report and act

Report the verdict as rows: the `RESULT` line (`RESULT: PASS`,
`RESULT: FAIL` or `RESULT: BLOCKED`), then every `F` row (`file:line`, the
problem, the fix, its `P` row) and every `U` row. If the reviewer reports
`BUILT-BY-ME: yes`, discard the verdict and run a fresh instance.

Fix the code findings, update `docs/privacy.md` for the record ones, and run
the reviewer again until it returns `RESULT: PASS`. A `U` row is a question
for the DPO or product owner: write it as a `NEEDS-DECISION` line under
`## Open decisions` in `docs/privacy.md` and list it in your reply.

Never call a change privacy-reviewed in a PR body unless the reviewer
returned `PASS` for the current code (or the user answered every `U` row in
writing). When you commit the fixes, stage only the files this run changed,
named explicitly (never `git add -A` or `git add .`). Commit on the feature
branch, never on main or master; on main or master, leave the changes
uncommitted and say so:

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on '$br': do not commit"; exit 1;; esac
git -C "$root" add <each file this run changed>
git -C "$root" commit -m "fix(privacy): <what the review found>" -- <each file this run changed>
```
