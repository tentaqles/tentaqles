---
name: pipeline-review
description: "Run the pipeline-reviewer agent on a data pipeline change (dbt models, Airflow, Dagster or Prefect jobs, pandas, polars or PySpark transforms, Microsoft Fabric or Synapse notebooks and pipelines) and turn its findings into fixes. Used by the build engine when triage raises the data pack flags pipeline, data_contract or warehouse_cost, and standalone when the user says \"review this pipeline\", \"review this DAG\", \"review this dbt model\", \"is this backfill safe\", \"is this job idempotent\", or before shipping a pipeline change."
argument-hint: '[--staged | --range A..B | <path>... | <slug> <checkpoint id>] [--headless]'
---

# Pipeline review

The reviewer is `agents/pipeline-reviewer.md`; its checklist is
`references/checklist.md` (rows `D1`...`D11`: idempotency, backfill, late data,
schema evolution, personal data into outputs, tests, cost, failure handling,
observability, secrets and environments, orchestrator specifics). This skill decides
when it runs, feeds it, and records the verdict. tq's `docs/packs/data-pipelines.md`
explains each lint rule the review builds on.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. With an active build slug,
put a question for the human in a decision record with
`build-gate ask <slug> --kind judge --checkpoint <id> --reviewer pipeline-reviewer --text "<the U rows>" --options PASS,FAIL`
and end the turn (the `build-gate` prefix is in `skills/build/SKILL.md`).
Without a slug, report what needs the human as the final output and end the turn.

## When it runs

- In the build engine: when `tq pack crew --flags <the flags triage printed>` prints
  `pipeline-reviewer`. With the data pack enabled that happens for the flags
  `pipeline` (dbt model paths, `packs.data.pipelines.paths`, Fabric `*.Notebook/` and
  `*.DataPipeline/` items, Synapse `notebook/`, `pipeline/`, `linkedService/` and
  `sqlscript/` JSON), `data_contract` (a contract file changed) and `warehouse_cost`.
  The build step adds every reviewer the crew prints; this skill never hard-codes
  the crew.
- Standalone: on request, over `--staged` (`git diff --staged`), a `--range`
  (`git diff <a>..<b>`), named paths (`git diff HEAD -- <path>...`), or the working
  tree (`git diff HEAD`).
- Not for a change that touches no pipeline, model, notebook or contract file: say so
  and stop.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to(){ if command -v timeout >/dev/null 2>&1; then timeout "$@"; elif command -v gtimeout >/dev/null 2>&1; then gtimeout "$@"; else shift; "$@"; fi; }
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
_to 60 tq decide triage --staged; echo "triage_exit=$?"
_to 10 tq pack crew --flags pipeline,data_contract,warehouse_cost --json; echo "crew_exit=$?"
git -C "$root" diff --staged --name-only
echo "checklist=$_tqe/skills/pipeline-review/references/checklist.md"
```

Swap `--staged` for the scope you review (`--base <ref>` for a range). If `crew_exit`
is not 0 or the crew does not list `pipeline-reviewer`, the data pack is off: tell
the user the review still runs, but without the pack lints.

## Run the data pack lints first

The reviewer starts from the lint results, so run them over the same scope:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to(){ if command -v timeout >/dev/null 2>&1; then timeout "$@"; elif command -v gtimeout >/dev/null 2>&1; then gtimeout "$@"; else shift; "$@"; fi; }
out="${TMPDIR:-/tmp}/tq-pipeline-review-$$.json"
_to 630 tq pack check --pack data --staged --json --budget 600s > "$out"; echo "check_exit=$? results=$out"
```

- `check_exit=0`: no required data check failed. `1`: at least one did; the failing
  `data/*` results are findings the reviewer confirms. `2`: a usage or config error;
  report it. `124`: the outer timeout hit; report it and review without the lints.
- The built-in pipeline checks never connect to a warehouse. `data/dagster-validate`
  runs `dagster definitions validate` only when dagster is installed; it is required
  only when `dagster` is declared in `packs.data.tools`.
- Never wrap this in `tq dotenv run` for the review: the pipeline lints need no
  credentials.

## Feed the reviewer

Launch the `pipeline-reviewer` agent with the diff as-is: the review bundle path
inside a build, or the diff written to a file under your scratchpad directory. Do not
pre-redact: the reviewer needs the real column and table names to judge D5, and the
lints already keep values out of their findings. Also pass the list of changed files,
the spec slice (its **Contracts touched** and **Outputs and classes** sections), the
contracts the diff touches, the paths `.tentaqles.yaml`, `docs/privacy.md`,
`CLAUDE.md`, `docs/LEARNINGS.md`, the `tq pack check` results file and the checklist
path. Inside a build, the reviewer prompt starts with the marker
`[checkpoint <slug>#<id> review]` (shape required by `skills/build/SKILL.md`; the
gate matches it). Ask for the review contract output, with the `JUDGED:` line.
Never paste `.env*` contents or connection strings.

## Record and act

Inside a build (`<slug> <id>` given), the verdict is recorded with the reviewer's
RESULT line (`RESULT: PASS`, `RESULT: FAIL` or `RESULT: BLOCKED`) and its counts
(`--blockers`, `--majors`, `--minors` are the F rows by severity; `--unjudged` is the
number of U rows). The gate accepts `pipeline-reviewer` because its agent file ships
with the plugin. Inside a build the build orchestrator runs `record-review` (per
`skills/build/SKILL.md`); this skill runs it when standalone with a slug:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" record-review <slug> <id> --reviewer pipeline-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
```

Then the engine's fixer loop handles FAIL and the human settles BLOCKED with
`build-gate judge`. Headless BLOCKED: `record-review` first, then
`build-gate ask <slug> --kind judge ...`. If the reviewer reports `BUILT-BY-ME: yes`,
discard the verdict and run a fresh instance.

Standalone (no build slug): nothing is recorded, because `record-review` needs a build
run. Report the verdict and list the findings with `file:line`, then fix the code
ones. Standalone, a `U` row is a question for the data owner: put it in the report
under "Needs a decision". Inside a build, U rows go to the judge flow.

How to fix, by row:

- D1 and D2: replace an append with a MERGE on the key, a partition overwrite
  (`replaceWhere`, `partitionOverwriteMode=dynamic`) or a delete-insert of the
  interval; take the interval from the run's logical date (`ds`,
  `data_interval_start`, a Fabric or Synapse parameter cell), never the wall clock.
- D5: mask the column (`sha2`, `md5`, `hash`, `farm_fingerprint`, or a project
  macro the human listed in `packs.data.pipelines.masking`), or drop it. Classifying
  the output instead is a manifest and `docs/privacy.md` change for the human; never
  edit the manifest, `tq-packs.yaml` or a baseline file to make a finding go away.
- A finding the code cannot fix (a known exception) gets an inline
  `tq:allow <rule>: <reason>` comment only when the human agrees with the reason.
- Never run a pipeline, a backfill or a `dbt build` against a production target to
  check a fix: the `tq/pipeline-run` guard asks before `airflow dags trigger`,
  `airflow dags backfill`, `dagster job launch`, `dagster asset materialize`,
  `prefect deployment run` and the `az datafactory`/`az synapse pipeline create-run`
  commands, and the answer is the human's.

Never call a change pipeline-reviewed in a PR body unless a recorded
`pipeline-reviewer` PASS exists for the current code (or the human's judge).
When you commit the fixes, stage only the files this run changed, named
explicitly (never `git add -A` or `git add .`). Commit on the feature branch,
never on main or master; headless on main or master, leave the changes
uncommitted and say so:

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
br=$(git -C "$root" branch --show-current); case "$br" in main|master|"") echo "on '$br': do not commit"; exit 1;; esac
git -C "$root" add <each file this run changed>
git -C "$root" commit -m "fix(pipeline): <what the review found>" -- <each file this run changed>
```
