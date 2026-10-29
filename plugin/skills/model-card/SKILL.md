---
name: model-card
description: Write or update a model's MODEL_CARD.md for the tq ML pack from the metrics file, the data manifest and the git state, interviewing the human (one question at a time) for intended use, out-of-scope uses and limitations, and never inventing fairness results. Fixes ml/model-card findings (missing heading, TODO section, empty metrics table, snapshot sha256 mismatch, personal data in the card, card not updated with the code). Use when the user says "model card", "write the card", "update MODEL_CARD.md", when ml/model-card fails, or when a promotion request is refused on model_card.
argument-hint: "<model>"
---

# Model card

One card per model, at `packs.ml.models[].card`. The template is
`"$_tqe/templates/process/MODEL_CARD.md"`. `ml/model-card` fails when a required
heading is missing or holds only `TODO`, the Metrics table has no row, the Training
data snapshot sha256 differs from `packs.ml.data_manifest`, the text hits the
redaction detectors, or the model's training, feature or eval code changed and the
card did not.

## 1. Gather the facts (no guessing)

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 10"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 10"
$_to tq pack list --enabled --json; echo "list_exit=$?"
echo "template=$_tqe/templates/process/MODEL_CARD.md"
git rev-parse HEAD; git log -1 --format=%cs
```

From the `ml` pack's config in that output take the model's `card`, `metrics`, the
`data_manifest` path, the `drift` block and the `promotion` targets. Then read:

- the metrics file (numbers for the Metrics table; never invent a confidence interval:
  write the method that produced it, or `NEEDS-DECISION`);
- the data manifest entry of the training snapshot (name and sha256, copied exactly);
- the lock file (`uv.lock`, `poetry.lock`, ...) for Reproducibility, and the training
  command the project documents;
- the drift reference and thresholds for Monitoring and drift.

## 2. Ask the human

Interview in the style of the `grill-me` skill: one question at a time, with the
likely options and a recommended default, recorded as answered or as an open
question with an owner. Ask only what the files cannot tell you:

1. **Intended use**: who uses the predictions, for which decision?
2. **Out of scope**: which uses must the model not serve? Any known misuse?
3. **Limitations and risks**: failure modes, feedback loops, drift sensitivity.
4. **Fairness and slices**: which slices were analysed, by what run. If no analysis
   was run, write `NEEDS-DECISION` with who decides. Never write a fairness result
   that no analysis produced.
5. **Owner role** and **alert route**: roles or channels, never personal names or
   addresses.

Headless (`TENTAQLES_HEADLESS=1`): do not ask; fill what the files prove, write
`NEEDS-DECISION` for the rest, and report the open questions as your final output.

## 3. Write it

- Start from the template when the card is missing; keep every `##` heading.
- No personal data: no example rows, user ids, e-mails, names, or labels of
  classified columns. Data classes are manifest class names only.
- Leave Promotion history alone: `tq pack ml promote apply` appends its rows.
- Then check it:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 70"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 70"
$_to tq pack check --pack ml --builtin-only --budget 60s; echo "check_exit=$?"
```

| Finding | Fix |
|---|---|
| `card-missing` | write the card from the template |
| `card-heading-missing` | restore the heading exactly as in the template |
| `card-section-todo` (error) | the section holds only TODO: fill it, or `NEEDS-DECISION` with an owner |
| `card-section-todo` (warn) | some lines still say TODO |
| `card-metrics-empty` | add rows from the metrics file |
| `card-snapshot-missing` / `card-snapshot-mismatch` | copy the snapshot name and sha256 from the data manifest; if the model was trained on another snapshot, the manifest or the training run is wrong: tell the human |
| `card-pii` | remove the value the detector names; never paste data into a card |
| `card-not-updated` | the model's code changed: update the sections it affects (metrics, data, reproducibility) |
