---
name: ml-promote
description: Request a model promotion to a protected stage or alias (production, champion) through the tq ML pack's approval gate. Runs tq pack ml promote request, which checks the model card, drift and reproducibility, binds the evidence (card, metrics, drift result, check results, git HEAD, clean tree, the exact command) and asks a human on the hub; then hands the human the apply line. Never runs apply, never answers the decision, never calls a registry promotion API. Use when the user says "promote the model", "move to production", "set champion", "register this version as prod", "ship the model", or asks why a registry promotion command was denied by tq/model-promote.
argument-hint: "<model> <version> --to <target> | status [<model>]"
---

# Model promotion

A promotion to a target listed in `packs.ml.promotion.protected` reaches the registry
only through a recorded human decision. You **request** it; the human **answers** it
and **applies** it in their own terminal. The guard `tq/model-promote` denies every
registry promotion command an agent tries (MLflow REST alias or stage calls, Python
calling `transition_model_version_stage` or `set_registered_model_alias`, `aws
sagemaker update-model-package --model-approval-status Approved`, `gcloud ai models
... --version-aliases`, `databricks registered-models set-alias`, `databricks
model-registry transition-stage`, `az ml model update --stage`, the declared
`promotion.command` itself, and `tq pack ml promote apply`). Do not look for another
way around it.

Rules:

- Never run `tq pack ml promote apply`. It is human-only and refuses agent sessions.
- Never answer the decision (`tq hub answer` from an agent is refused) and never ask
  the human to let you.
- Never say a model "was promoted" unless `tq pack ml promote status` shows the
  request as `used`.
- Never edit `tq-packs.yaml`, the manifest or a threshold to make a requirement pass.

## 1. Before the request

The request refuses unless the tree is clean and every `promotion.require` item
passes (default: `model_card`, `drift`, `repro`). Prepare them first:

- `model_card`: the card at `packs.ml.models[].card` passes `ml/model-card`. Use the
  `model-card` skill to write or update it.
- `drift`: `tq pack ml drift <model>` is not `fail`. Use the `drift-check` skill.
- `repro`: `ml/seeds`, `ml/env-lock` and `ml/data-snapshot` pass over the training
  paths (the `ml-review` and `pack` skills explain their findings).
- Commit everything. `.tq/drift/` (the drift result) is ignored by the clean check;
  it should be in `.gitignore`.

## 2. Request

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 900"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 900"
$_to tq pack ml promote request <model> <version> --to <target>; echo "request_exit=$?"
```

- `<target> is not protected, no approval needed`: the target is outside
  `promotion.protected`. Tell the user; the project's normal registry flow applies.
- `asked: decision <id> on card <card>`: report the decision id, the card and the
  evidence names it printed, then give the human this line to run **in their own
  terminal** after they approve there with `tq hub answer <card> <id> --choice approve`
  (the board shows the decision but does not answer pack approvals):

  ```
  tq pack ml promote apply <id>
  ```

- `promotion of ... refused; fix these first:` each line names the item
  (`model_card:`, `drift:`, `repro:`) and the finding. Fix the cause, commit, and
  request again. `repro: ml/... is not available in this tq build` means tq lacks the
  lint checks: report it, do not work around it.
- `a promotion needs a clean tree`: commit or stash, then request again.
- `packs.ml.promotion.command is not set`: the human adds the project's promotion
  command to the manifest (with `{model}`, `{version}` and `{target}`). You may
  propose the line; you never edit the manifest.
- `request_exit=124`: tq did not finish in 900 s (the drift `current_command` can take
  up to 600 s). Report it.

## 3. After the request

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 10"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 10"
$_to tq pack ml promote status <model>; echo "status_exit=$?"
```

| State | Meaning | What you do |
|---|---|---|
| `pending` | asked, not answered | wait; remind the human of the decision id |
| `approved` | a human approved; not applied yet | give the human the apply line again |
| `used` | the human applied it; `promote apply` recorded it in the card's Promotion history | report the promotion; commit the card change if the human asks |
| `rejected` | not granted | report it; a new request is needed after the reason is addressed |
| `expired` | past `promotion.valid_for` (default 7 days, at most 30) | a new request is needed |

Apply refuses when anything bound in the evidence changed since the request: the
card, the metrics file, the drift result, the check results, the commit, a dirty
tree, or the promotion command. It names the changed entries. The fix is a new
request at the new commit, never a reverted file.
