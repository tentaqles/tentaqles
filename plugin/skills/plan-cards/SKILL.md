---
name: plan-cards
description: Turn a planning-ready PRD release into child feature cards on the tq hub board - one card per FR of the chosen release (title, FR text and acceptance checks as the body), under a project card and a project folder, with the card ids written back into PRD section 11 so a re-run skips them. Never tags a card agent-ready; that is /tentaqles:refine. Use when the user says "plan the cards", "make cards from the PRD", "cards for release 1", "break the PRD into cards", "PRD to board", or "/tentaqles:plan-cards".
argument-hint: '<release> [--parent <project card>] [--project <folder>] [--prd <path>]'
---

# Plan cards

A project card becomes a PRD, then one child `feature` card per FR of a release, then
agent runs. This skill makes the child cards. It is safe to run again: an FR whose card
id is already in PRD §11 is skipped (spec B §9).

`$ARGUMENTS` is the release (`1`, `Release 1`, `release-1`, or `all` for every FR in
§4), optionally `--parent <card>`, `--project <folder>` and `--prd <path>`.

## 1. Find the PRD, the parent card and the project folder

Run from the project folder that holds `docs/prd.md`.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
tq hub list --kind project --json --timeout 2s 2>&1; echo "hub_exit=$?"
```

Every later block starts with the same first two lines (`_tqe=` and `root=`); blocks may
run in fresh shells.

- `prd_exit=4`: there is no PRD. Say so and offer `/tentaqles:prd`. Stop.
- `prd_exit=3`: the PRD is not planning-ready (open `NEEDS-DECISION` items in §10, or an
  FR without acceptance checks). Quote the JSON and offer `/tentaqles:prd`. Stop.
- `hub_exit` not 0: no hub for this workspace; quote the first line and stop.
- **Parent card.** Use `--parent` when given. Otherwise pick the `project` card from the
  list whose title or `project_dir` matches this project. If there is none, or more than
  one fits, ask the user which card it is (or offer to add one:
  `tq hub add "<project> release <n>" --kind project --project <folder> --timeout 2s`).
- **Project folder.** Use `--project` when given, else the parent card's `project_dir`.
  It is relative to the workspace root (`demo-todo`, `apps/billing`), never absolute. If
  neither has one, ask.
- **Release.** If `$ARGUMENTS` names none, read §11 and ask which release.

## 2. Show the plan

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$root" && "$TENTAQLES_PY" "$_tqe/scripts/plan-cards.py" --prd docs/prd.md --release "<release>" \
  --parent "<card>" --project "<folder>" --dry-run; echo "plan_exit=$?"
```

The JSON lists `planned` (FR, card title, body, the `tq hub add` command) and `skipped`
(FRs that already have a card). Show the user a short table: FR, title, number of
acceptance checks, and the skipped ones with their card ids. Exit 2 is a usage or PRD
problem (an unknown release, an FR that §4 does not define, a bad `--parent`): quote the
message and fix the argument with the user. If nothing is planned, say every FR of the
release already has a card and stop.

Ask once: "Create these N cards under <parent>?" In headless mode (`TENTAQLES_HEADLESS=1`)
do not ask; go on.

## 3. Create the cards

The same command without `--dry-run`:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$root" && "$TENTAQLES_PY" "$_tqe/scripts/plan-cards.py" --prd docs/prd.md --release "<release>" \
  --parent "<card>" --project "<folder>"; echo "plan_exit=$?"
```

For each FR without a card it runs
`tq hub add "<FR-id> <title>" --kind feature --parent <card> --project <folder> --body-file <tmp> --json`
and records the new id in PRD §11 right away, in a block between `<!-- tq:cards -->` and
`<!-- /tq:cards -->` (one `- FR-n: c-…` line per FR). A §11 line that names exactly one FR
and one card id (a card filed by hand) also counts as that FR's card.

- `plan_exit=0`: report the created cards (FR, card id) and the skipped ones.
- `plan_exit=1`: a `tq hub add` failed. The cards made before it are recorded; quote the
  error, fix the cause, and run step 3 again: it resumes where it stopped.

The cards land in the backlog lane without `agent-ready`. The PRD edit is a normal file
change in the project: say it should be committed with the next change (`/tentaqles:ship`).

## 4. Next

Tell the user the next step per card: `/tentaqles:refine <card>` interviews them on the
card, tightens its body and acceptance checks, then tags it `agent-ready`. Moving a card
into an active lane, which starts its run, stays the user's.

## Never

- tag a card `agent-ready`, set a budget, or move a card out of the backlog
- create a card for an FR that already has one in §11, or edit the §11 card block by hand
  when the script can write it
- run `tq hub add` per FR yourself instead of the script (it skips the §11 write-back)
- copy PRD or card text into another tool
