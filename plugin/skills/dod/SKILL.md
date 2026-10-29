---
name: dod
description: Run the definition of done for a feature, fix or kickoff build and turn its MISSING items into work. The gate runs the project's frozen test, lint and typecheck commands, checks the locked tests, every checkpoint and reviewer record, and the obligations the diff created (ADR for architecture paths, docs/privacy.md for classified paths, a migration plan for migration files, the changelog). Use when the user says "/tentaqles:dod <slug>", "are we done", "definition of done", "what is missing before we ship", or when the Stop hook reports "definition of done not proven". Only a human may waive an item, by running the override in their own terminal, and the waiver is logged.
argument-hint: "<slug> [--headless]"
---

# Definition of done

"Done" is a record the gate wrote, never a sentence in the transcript. This skill
runs the record and works through what it lists.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. At every human stop you write
the question with `build-gate ask` and end your turn (see step 3).

## Packs

Before step 1, run the on-ramp step of the `pack` skill (`skills/pack/SKILL.md`,
"On-ramp step"): `tq pack list --enabled --json` (timeout 10 s); for each enabled pack
read every `skills/pack/references/onramps/<pack>-*.md` and follow its `## dod`
section at the step it names. No pack enabled, or a tq without the `pack` command:
skip this. The `pack:` items are explained in the `pack` skill (step 5). Reviewers: a fix
round that re-runs the review gate adds every reviewer that `tq pack crew --flags
<the flags triage printed>` prints.

## 1. Run it

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" dod <slug>
```

Below, `build-gate <cmd>` means that second line with `<cmd>`. Use the Bash tool
(Git Bash on Windows). The output is one head line
(`DoD <slug>: PASS|FAIL (tree ...)`) and one line per check: `ok`, `skip` or
`MISSING`. Nothing else is read from logs: the gate already recorded exit codes.

## 2. Work the MISSING items

| Item | What it means | Who fixes it |
|---|---|---|
| `project_config` | no valid ```` ```json tq-project ```` block in `CLAUDE.md` at approval time | you: add the block from `templates/process/CLAUDE-project.md`, then the human re-approves the plan in their terminal (`build-gate approve <slug>`; headless: `build-gate ask <slug> --kind plan-approval ...`) so the commands are frozen |
| `tests`, `lint`, `typecheck` | the frozen command exited non-zero (last 20 lines are in the detail) | a fresh `build-fixer` subagent with the detail; never edit the locked tests |
| `locked_tests` | a locked test changed | restore it (`git checkout -- <file>` is fine for the human; you report it) |
| `checkpoints` | a checkpoint is not `passed` | finish it through `/tentaqles:build resume <slug>` |
| `reviews` | a reviewer's last record is FAIL or BLOCKED | FAIL: fixer round; BLOCKED: show the `U` rows to the human, they run `build-gate judge` (headless: `build-gate ask <slug> --kind judge ...`) |
| `adr` | an `architecture_paths` file changed without a new ADR | write `docs/adr/NNNN-<slug>.md` from `templates/process/adr-0000-template.md` |
| `privacy` | a `classified_paths` file changed and `docs/privacy.md` did not | run `/tentaqles:privacy-review` (if that skill is not installed, review the change against `docs/privacy.md` by hand), update the record |
| `migration` | migration files changed and the spec has no `## Migration plan` | add the section (expand/contract, backfill, rollback) to the spec slice |
| `changelog` | the project keeps a changelog and it did not change | add the entry |
| `prd-decisions` | a `NEEDS-DECISION` line in `docs/prd.md` section 10 is open and was not open at the build's base (the detail names it; pre-existing ones are listed as "still open (pre-existing)" and do not fail). With no recorded base every open one counts as new | the human settles it in the PRD (or accepts it at the final review); list it in the stop-2 summary |
| any item that cannot be fixed | only a human may waive it | step 3 (the human runs the override) |

Re-run step 1 after each fix until the head line says PASS. The Stop hook blocks once while the definition of done is unproven; the next stop goes through, so
the hook is no substitute for a PASS line. Once it says PASS, `/tentaqles:ship` quotes the record in the PR body.

## 3. Override (human only)

Only a human waives an item, and only by running the override themselves. You never run `build-gate dod <slug> --override`; the hook refuses it to the agent
with exit 2. If an item cannot be fixed:

1. Stop. Tell the human the exact command to run in their own terminal, with
   absolute paths (run `echo "$TENTAQLES_PY" "$_tqe/scripts/build-gate.py"` after
   the prefix to get them), and which items it waives:
   `build-gate dod <slug> --override "<your reason>"`. Never write the reason
   for them and never paraphrase it.
2. Wait. Do not claim done in the transcript while the record says FAIL.
3. After they say it is run, confirm with `build-gate state <slug> --json`:
   `dod.ok` is true and the `dod.override` field is set. Re-running a plain
   `build-gate dod <slug>` is safe: the gate keeps a human override while the
   code is unchanged. Any code change drops it, and you ask again.

Headless: do not wait on a person. Write a decision record and end your turn:

```bash
build-gate ask <slug> --kind question --text "override: <what is missing and why>" --options override,stop
```

The human answers with `build-gate answer <slug> <id> --choice override --note "<their reason>"`
(or a hub actor token does). Your final output names the record and never tells
the human to run that command: when `TQ_HUB_TASK` is set, say the answer goes on
the hub card (`/waiting`) and the hub resumes the run (the build skill,
"What headless output tells the human"). On resume, read `build-gate state <slug> --json`.

The gate records the override, appends a `D<n>` line to `docs/LEARNINGS.md` and
logs an event `tq insights` counts.
