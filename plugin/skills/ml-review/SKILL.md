---
name: ml-review
description: "Run the ml-reviewer agent on a change to model training, features, evaluation, a model card or the promotion path, quoting the ml pack results (ml/leakage and the reproducibility checks), and turn its verdict into fixes. Used by the build engine's crew when triage raises the ml or model_promotion flag, and standalone when the user says \"review the training code\", \"check for leakage\", \"is this evaluation sound\", \"ML review\", or before a promotion request."
argument-hint: '[--staged | --range A..B | <path>... | <slug> <checkpoint id>] [--headless]'
---

# ML review

The reviewer is `agents/ml-reviewer.md`; its checklist is `references/checklist.md`
(rows `M1`...`M11`). This skill decides when it runs, gathers the ml pack results it
judges, feeds it, and records the verdict. It never promotes a model: promotion is
`tq pack ml promote request` (the `ml-promote` skill) and the human's
`tq pack ml promote apply`, which you never run.

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. With an active build slug, put a
question for the human in a decision record with
`build-gate ask <slug> --kind judge --checkpoint <id> --reviewer ml-reviewer --text "<the U rows>" --options PASS,FAIL`
and end the turn (the `build-gate` prefix is in `skills/build/SKILL.md`). Without a
slug, report what needs the human as the final output and end the turn.

## When it runs

- In the build engine: when `tq decide triage` prints `ml` (training, feature or eval
  files under `packs.ml.paths`, or any changed `.py`/`.ipynb` that calls `.fit(`,
  `optimizer.step(` or `trainer.train(`) or `model_promotion` (the promotion script,
  `packs.ml.promotion` or a model card changed). `tq pack crew --flags <flags>` then
  prints `ml-reviewer`, and the engine adds it to the crew.
- Standalone: on request, over `--staged` (`git diff --staged`), a `--range`
  (`git diff <a>..<b>`), named paths, or the working tree (`git diff HEAD`).
- The ml pack must be enabled (`tq pack list --enabled --json`). When it is not,
  the reviewer still runs on request, but say that there are no pack results to
  quote and that `ml/*` rows are judged from the files alone.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 60"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 60"
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
$_to tq decide triage --staged; echo "triage_exit=$?"
$_to tq pack crew --flags ml,model_promotion --json; echo "crew_exit=$?"
git -C "$root" diff --staged --name-only
echo "checklist=$_tqe/skills/ml-review/references/checklist.md"
```

Use the flags triage actually printed in the `crew` line. `triage_exit` or
`crew_exit` of `124` means tq did not answer in time: report it, do not retry in a
loop.

## Gather the pack results

Run the ml pack over the same change set the reviewer will read. Use `--staged`,
`--base <ref>` (a range or a build's base) or `--paths <files...>` to match it.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 630"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 630"
out="$(mktemp -d)/ml-pack.json"
$_to tq pack check --pack ml --staged --json --budget 600s > "$out"; echo "check_exit=$? results=$out"
```

- `check_exit=0` or `1`: the file holds `tq.packs/v1` results. Quote, per check, the
  status and the findings as `file:line level rule message`: `ml/leakage` (rules L1 to
  L8), then the reproducibility checks `ml/seeds`, `ml/env-lock`, `ml/data-snapshot`,
  `ml/run-tracking`, then `ml/unsafe-deserialize`, `ml/promotion-in-code`,
  `ml/model-card` and the external `ml/ruff`, `ml/lock-check`, `ml/dvc-status`.
- `check_exit=2`: a usage or config error. Report the message; the human fixes the
  manifest, an agent may only propose a `tq-packs.yaml` change.
- `check_exit=124`: the run passed its budget. Report it; the reviewer then marks the
  rows it can't back with results as `U` rows.
- Never edit `tq-packs.yaml`, a baseline file or the manifest to make a check pass,
  and never run `tq pack baseline` (human only).

## Feed the reviewer

Launch the `ml-reviewer` agent with:

- the diff as-is (the review bundle path inside a build, or `git diff --staged`
  written to a file under your scratchpad directory) and the list of changed files;
- the pack results file from the step above;
- the spec slice (**Models touched**, **Data snapshot**, **Evaluation plan**) when there
  is one;
- the paths of each touched model's card and metrics file (`packs.ml.models`), the
  `data_manifest`, `docs/privacy.md`, `.tentaqles.yaml`, `CLAUDE.md`,
  `docs/LEARNINGS.md` and the checklist.

Personal data may be read locally; the reviewer names columns, never values. Never
read or paste `.env*` contents, and never pass credentials or the registry tracking
token. Inside a build, the reviewer prompt starts with the marker
`[checkpoint <slug>#<id> review]` (shape required by `skills/build/SKILL.md`; the gate
matches it). Ask for the review contract output.

## Record and act

Inside a build the build orchestrator runs `record-review` (per
`skills/build/SKILL.md`); this skill runs it when standalone with a slug. The counts
are the F rows by severity, and `--unjudged` is the number of U rows:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" record-review <slug> <id> --reviewer ml-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
```

Then the engine's fixer loop handles FAIL and the human settles BLOCKED with
`build-gate judge`. Headless BLOCKED: `record-review` first, then
`build-gate ask <slug> --kind judge ...`. If the reviewer reports `BUILT-BY-ME: yes`,
discard the verdict and run a fresh instance.

Standalone (no build slug): nothing is recorded, because `record-review` needs a build
run. Report the verdict, then:

- list each finding with its `file:line`, its M-row and the pack check it confirms;
- for a leakage or reproducibility fix, add a test (and, when it is a lint gap, tell
  the user which `ml/leakage` rule it falls under);
- for a card mismatch, update the card from the metrics file with the `model-card`
  skill, never by hand-typed numbers;
- put each U row to the human as a question (the model owner for M2 and M3, the DPO for
  M6).

Never call a model reviewed in a PR body unless a recorded `ml-reviewer` PASS exists
for the current code (or the human's judge), and never call it promoted unless
`tq pack ml promote status` shows the request applied.
