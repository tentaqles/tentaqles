---
name: dod
description: "Run the definition of done for a feature, fix or kickoff branch as a checklist and turn its MISSING items into work. It runs the project's test, lint and typecheck commands, and checks the obligations the diff created: an ADR when an architecture path changed, docs/privacy.md when the data a change handles changed, a migration plan when migration files changed, the spec slice kept in step, the changelog entry, no PRD decision left open. Use when the user says \"/tentaqles:dod\", \"are we done\", \"definition of done\", or \"what is missing before we ship\"."
argument-hint: "[<base branch>]"
---

# Definition of done

"Done" is a record you ran and quote, never a sentence in the transcript.
Every row is a command you ran or a file you read; a row you did not check is
`MISSING`, not `ok`.

## 1. The diff and the project config

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
base=<base branch, default main>
git -C "$root" branch --show-current
git -C "$root" diff --name-only "$base"...HEAD
echo "adr_template=$_tqe/templates/process/adr-0000-template.md"
```

Read `## Project config` of the project's `CLAUDE.md`: the **Checks** line
(the test, lint and typecheck commands), the architecture paths and the
changelog file. With no `CLAUDE.md` section, read `package.json`,
`pyproject.toml`, `go.mod` or the CI workflow for the real commands; never
guess one.

## 2. The checklist

Run or read each row and write its result down:

| Item | Check | `ok` when |
|---|---|---|
| `tests` | run the test command (in the background or with the `Monitor` tool when it is slow; `tq dotenv run -- <cmd>` when it needs a `.env`) | exit 0; quote the last line |
| `lint` | run the lint command | exit 0 (`skip` when the project has none) |
| `typecheck` | run the typecheck command | exit 0 (`skip` when the project has none) |
| `adr` | a changed file matches an architecture path | a new `docs/adr/NNNN-<slug>.md` is in the diff |
| `privacy` | the diff reads, writes, logs or exports personal data, or changes a retention | `docs/privacy.md` changed in the diff |
| `migration` | migration files changed | the spec slice has a `## Migration plan` (expand/contract, backfill, rollback) |
| `spec` | the branch implements a spec slice or a fix spec | every acceptance check has a test in the diff, open questions are decided, `Status` is `implemented` (or `fixed`) |
| `learnings` | the branch is a fix | the `L<n>` line is in `docs/LEARNINGS.md` |
| `changelog` | the project keeps a changelog | it changed |
| `prd-decisions` | `docs/prd.md` §10 | no `NEEDS-DECISION` line this branch opened is still open (pre-existing ones are listed, not failed) |

Print the record, one head line and one line per row:

```
DoD <branch>: PASS | FAIL
- tests: ok — python -m pytest -q → 42 passed in 3.1s
- lint: skip — no lint command
- adr: MISSING — src/core/auth.py matches src/core/**, no ADR in the diff
```

`PASS` only when no row says `MISSING`.

## 3. Work the MISSING items

| Item | Who fixes it |
|---|---|
| `tests`, `lint`, `typecheck` | you: fix the code, never the test that fails to make it pass |
| `adr` | write `docs/adr/NNNN-<slug>.md` from `templates/process/adr-0000-template.md` |
| `privacy` | run `/tentaqles:privacy-review` (if that skill is not installed, review the change against `docs/privacy.md` by hand) and update the record |
| `migration` | add the section to the spec slice |
| `spec`, `learnings`, `changelog` | write the missing lines |
| `prd-decisions` | the human settles it in the PRD, or accepts it in writing; list it in your reply |
| an item the user decides to leave undone | their call, in their words: it goes under Notes in the PR body and the head line stays `FAIL` |

Re-run step 2 after each fix until the head line says `PASS`. Then
`/tentaqles:ship` quotes the record in the PR body.

## Never

- write `PASS` while a row says `MISSING`, or `ok` for a command you did not run
- skip a row because the command is slow: run it in the background and wait
- edit a test to turn the `tests` row green
