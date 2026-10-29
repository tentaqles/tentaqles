---
name: drift-check
description: Check a model for data drift with the tq ML pack - build or refresh the reference and current profiles (aggregates only, hashed labels for classified columns), run tq pack ml drift, explain which features moved and by how much (PSI or Jensen-Shannon), check the metric floors, and propose retraining or a threshold review for the human. Use when the user says "drift", "is the model still valid", "has the data changed", "check the model before promoting", when a promotion request is refused on drift, or when ml/drift fails in the definition of done.
argument-hint: "<model>"
---

# Drift check

tq never queries data for drift. It compares two local files from
`packs.ml.drift` (or the model's own `models[].drift`): `reference` and `current`.
Each is a CSV (tq builds the profile: numeric bins from the reference deciles,
categorical counts) or a profile JSON `tq.drift-profile/v1` that holds aggregates
only:

```json
{"schema": "tq.drift-profile/v1", "rows": 1000, "source": "...", "created_at": "...",
 "features": {
   "tenure_days": {"type": "numeric", "edges": [30, 90, 180], "counts": [200, 300, 300, 200]},
   "plan": {"type": "categorical", "counts": {"basic": 500, "pro": 500}},
   "region": {"type": "categorical", "hashed": true, "counts": {"<sha256 hex of the label>": 600}}}}
```

Numeric `counts` has one more entry than `edges` (open bins `(-inf, e0]`, ...,
`(en, inf)`), or one fewer (closed bins between the edges). The current profile must
use the reference's edges. `__prediction__` is the score distribution when the profile
has it.

## 1. Profiles

- Prefer the project's profile script (the `ml-starter` template ships
  `drift_profile.py`). Run Python through `tq_run.sh` or `$TENTAQLES_PY`, never bare
  `python`. It reuses the reference edges and hashes the labels of classified columns
  (`data.paths` in the manifest).
- Never put rows, ids or labels of classified columns in a profile, a message or a
  memory. Profiles are aggregates; the drift result names features and numbers only.
- `drift.current_command`, when set, refreshes `current` automatically when it is
  missing or older than 24 h (timeout 600 s). A command that comes from
  `tq-packs.yaml` rather than the manifest needs the human's typed `yes` in a terminal:
  in an agent session tq refuses and prints the command. Run that command yourself
  (it is the project's own script), then run the drift check again.

## 2. Run it

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 700"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 700"
$_to tq pack ml drift <model>; echo "drift_exit=$?"
```

- `drift_exit=0`: `ok` or `warn`. `1`: `fail` (edges that differ are a `fail` of that
  feature). `2`: an input or config error (a missing profile, a bad JSON, an
  unconfirmed repo-layer command); the message says which file. `124`: the outer
  timeout hit; report it.
- The result is written to `.tq/drift/<model>-result.json` (keep `.tq/drift/` in
  `.gitignore`). A promotion request hashes it into its evidence.

## 3. Explain it

Per feature the output gives the value, the `warn` and `fail` thresholds and the
status. PSI: below 0.1 is usually stable, 0.1 to 0.25 a moderate shift, above 0.25 a
large one (the project's thresholds decide). The overall result fails when any
feature fails, when the share of warned features exceeds `max_warn_share`, or when a
metric in `drift.metrics.<m>.min` is below its floor or missing from the metrics file.

Tell the user, in this order: which features moved (largest first), whether the
metric floors hold, and what that means for the model's intended use (from its card).

## 4. Propose, do not decide

- **Retrain** on a fresh snapshot when important features fail or a metric floor is
  missed. That is a new training run, a new card and a new promotion request.
- **Threshold review** when the human believes a moved feature is expected. Changing a
  threshold is a manifest edit by the human. `tq-packs.yaml` may only tighten a
  threshold, never loosen one, and you never edit either to make the check pass.
- **Edges differ**: rebuild the current profile with the reference edges.
