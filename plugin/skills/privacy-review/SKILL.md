---
name: privacy-review
description: "Run the privacy-reviewer agent on a diff that touches classified data (manifest data block paths or tables, or new log fields, events and exports with personal-data shapes) and turn its findings into fixes and privacy-record updates. Used by the build engine's review gate and standalone when the user says \"privacy review\", \"LGPD check\", \"GDPR check\", \"is this compliant\", or before shipping a change to personal data handling."
argument-hint: '[--staged | --range A..B | <slug> <checkpoint id>] [--headless]'
---

# Privacy review

The reviewer is `agents/privacy-reviewer.md`; its checklist is
`references/checklist.md` (rows `P1`...`P11`). This skill decides when it runs,
feeds it, and records the verdict.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. With an active build slug,
put a question for the human in a decision record with
`build-gate ask <slug> --kind judge --checkpoint <id> --reviewer privacy-reviewer --text "<the U rows>" --options PASS,FAIL`
and end the turn (the `build-gate` prefix is in `skills/build/SKILL.md`).
Without a slug, report what needs the human as the final output and end the turn.

## When it runs

- In the build engine: when triage prints the `classified_data` flag, or the
  checkpoint's `changed_files` match the `tq-project` block's `classified_paths`
  or a manifest `data:` table, or the diff adds log statements, analytics
  events or exports whose fields look personal (e-mail, CPF, phone, address,
  birth date, card). A documentation-only diff that changes only docs/privacy.md
  does not select it.
- Standalone: on request, over `--staged` (`git diff --staged`), a `--range` (`git diff <a>..<b>`), or the working tree (`git diff HEAD`).

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
tq decide triage --staged; echo "triage_exit=$?"
git -C "$root" diff --staged --name-only
echo "checklist=$_tqe/skills/privacy-review/references/checklist.md"
```

## Feed the reviewer

Launch the `privacy-reviewer` agent with the diff as-is: the review bundle path
inside a build, or `git diff --staged` written to a file under your scratchpad
directory. Personal data may be read and processed locally, and the reviewer
needs the real field shapes to judge P5 and P6, so do not pre-redact and do not
refuse to read it. The guards sit at the exits: commits, pushes, PR text,
memory, fixtures. `tq redact` is for copies that will leave the session (PR
text, bug reports), never for the reviewer's input. Also pass the list of
changed files, the spec slice (`data_classes_touched`), the paths
`docs/privacy.md`, `.tentaqles.yaml`, `CLAUDE.md`, `docs/LEARNINGS.md`, and the
checklist path, and say which changed files matched `classified_paths` or the `data:` block (list them in the bundle). Inside a build, the reviewer prompt starts with the marker `[checkpoint <slug>#<id> review]` (shape required by `skills/build/SKILL.md`; the gate matches it). Ask for the review contract output. Never paste `.env*`
contents.

## Record and act

Inside a build (`<slug> <id>` given), the verdict is recorded with the
reviewer's RESULT line (`RESULT: PASS`, `RESULT: FAIL` or `RESULT: BLOCKED`)
and its counts (`--blockers`, `--majors`, `--minors` are the F rows by severity; `--unjudged` is the number of U rows). Inside a build the build orchestrator runs `record-review` (per `skills/build/SKILL.md`); this skill runs it when standalone with a slug:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" record-review <slug> <id> --reviewer privacy-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
```

Then the engine's fixer loop handles FAIL and the human settles BLOCKED with
`build-gate judge`. Headless BLOCKED: `record-review` first, then `build-gate ask <slug> --kind judge ...`. If the reviewer reports `BUILT-BY-ME: yes`, discard the
verdict and run a fresh instance.

Standalone (no build slug): nothing is recorded, because `record-review` needs a
build run. Report the verdict and list the findings with `file:line`, fix the
code ones, and update `docs/privacy.md` and the manifest `data:` block for the record ones, then
remind the user to run `tq allow <workspace>` (the manifest's trust hash changed). Standalone, a `U` row is a question for the DPO or product
owner: write it as a `NEEDS-DECISION` line under `## Open decisions` in
`docs/privacy.md`. Inside a build, U rows go to the judge flow, not to `## Open decisions`.

Never call a change privacy-reviewed in a PR body unless a recorded
`privacy-reviewer` PASS exists for the current code (or the human's judge).
When you commit the fixes, stage only the files this run changed, named
explicitly (never `git add -A` or `git add .`). Commit on the feature branch,
never on main or master; headless on main or master, leave the changes
uncommitted and say so:

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on '$br': do not commit"; exit 1;; esac
git -C "$root" add <each file this run changed>
git -C "$root" commit -m "fix(privacy): <what the review found>" -- <each file this run changed>
```
