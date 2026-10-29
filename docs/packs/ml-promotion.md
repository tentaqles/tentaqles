# ML pack: model promotion, model cards and drift

This page covers the part of the `ml` pack that gates how a model reaches
production (spec C §4.4 to §4.6):

- the promotion gate, `tq pack ml promote request|apply|status`;
- the model card check `ml/model-card`;
- the drift check, `tq pack ml drift` and `ml/drift`;
- the code check `ml/promotion-in-code`;
- the guard `tq/model-promote`.

The lints and the reproducibility checks are on the ml-lints page.

## Config

These keys live under `packs.ml` in the manifest (the trusted layer). A repo's
`tq-packs.yaml` may add to them or tighten them, never loosen them.

```yaml
packs:
  ml:
    enabled: true
    paths: { train: ["src/train/**"], features: ["src/features/**"], eval: ["src/eval/**"] }
    data_manifest: data/snapshots.yaml
    models:
      - name: churn
        card: models/churn/MODEL_CARD.md
        metrics: reports/churn/metrics.json
        paths: ["src/churn/**"]            # optional; default: paths.train + features + eval
        drift:                             # optional per-model inputs
          reference: data/profiles/churn-train.json
          current: .tq/drift/churn-current.json
          current_command: "uv run python -m drift_profile --model {model}"
    promotion:
      protected: [production, champion]
      command: "uv run python scripts/promote.py {model} {version} {target}"
      valid_for: 7d
      require: [model_card, drift, repro]
    drift:
      reference: data/profiles/churn-train.json
      current: .tq/drift/churn-current.json
      current_command: "uv run python -m drift_profile --out .tq/drift/churn-current.json"
      metric: psi            # psi | js
      warn: 0.1
      fail: 0.25
      max_warn_share: 0.3
      features: { tenure_days: { fail: 0.2 } }
      metrics: { auc: { min: 0.78 } }
      in_dod: false
```

| Key | Meaning | Repo layer (`tq-packs.yaml`) |
|---|---|---|
| `models[]` | `name` (`[A-Za-z0-9._-]`), `card` (required), `metrics`, `paths`, `drift` | union: a repo may add a model; for a duplicate name, the manifest's entry wins |
| `promotion.protected` | the stages or aliases that need an approval (matched without case) | union |
| `promotion.command` | the project's promotion command. The placeholders are `{model}`, `{version}` and `{target}`. It runs without a shell, and `\| & ; < > $` and backticks are refused | may set it. It runs only after the human types `yes`, and the approval is bound to its hash |
| `promotion.valid_for` | how long an approval stays valid: default 7d, at most 30d | minimum |
| `promotion.require` | `model_card`, `drift`, `repro` (all three when unset) | union |
| `drift.reference`, `drift.current` | repo paths, each a CSV or a profile JSON | trusted: the repo may add one only when the manifest has none |
| `drift.current_command` | refreshes `current` when it is missing or older than 24 h. The placeholder is `{model}`, and the timeout is 600 s | may set it, but a repo command needs a typed `yes` (see below) |
| `drift.metric` | `psi` (default) or `js` | trusted |
| `drift.warn`, `drift.fail`, `drift.features.<f>.warn\|fail`, `drift.max_warn_share` | thresholds | minimum |
| `drift.metrics.<m>.min` | lower bounds on the model's metrics file | maximum |
| `drift.in_dod` | run `ml/drift` in the definition of done | OR (a repo can switch it on, never off) |

The keys `paths` and `data_manifest` are shared with the lint checks.

## Promotion gate

1. **Request.** An agent or a human runs
   `tq pack ml promote request <model> <version> --to <target>` (add `--json` for
   `tq.ml-promote/v1`).
   - If the target is not in `promotion.protected`, it prints
     `<target> is not protected, no approval needed` and exits 0.
   - Otherwise it refuses on a dirty tree. Paths under `.tq/drift/` are ignored, so
     gitignore that folder.
   - It runs the `require` items:
     - `model_card`: the `ml/model-card` rules on that model;
     - `drift`: `tq pack ml drift <model>`, where a `fail` refuses;
     - `repro`: `ml/seeds`, `ml/env-lock` and `ml/data-snapshot` over every tracked
       file under `paths.train`. A check missing from the build fails closed.
   - It builds the evidence, then calls the approvals API (`action: promote`,
     `subject: <model>@<version>`, `target: <target>`). The decision lands on the
     run's card, or on a new chore card tagged `ml-approval`.
   - It prints the decision id, the card and the evidence names.
2. **Decide.** A human answers `approve` or `reject` on the board, in `/waiting` or
   with `tq hub answer` in their own terminal. Agents never answer.
3. **Apply.** The human runs `tq pack ml promote apply <decision>` (human only).
   - It refuses an agent session (`CLAUDECODE`, `TENTAQLES_HEADLESS`,
     `CLAUDE_PLUGIN_ROOT`, `TQ_HUB_TASK`).
   - It recomputes the evidence and verifies the approval. The approval must be
     answered by a human with `approve`, be unexpired and unused, and match the
     evidence. When the evidence changed, the refusal names the changed entries.
   - It prints the filled command (values limited to `[A-Za-z0-9._@-]`, never
     starting with `-`) and asks for a typed `yes` on a terminal.
   - It runs the command with the workspace identity's environment and a 300 s timeout.
   - On exit 0 it marks the approval used and appends a row to the card's Promotion
     history (date, version, target, decision id, `human`). It also comments on the
     hub card. Commit the card change.
   - On a failure or a timeout the approval stays unused, and the failure is
     commented on the card.
4. **Status.** `tq pack ml promote status [<model>] [--json]` lists the requests as
   `pending`, `approved`, `rejected`, `used` or `expired`. A rejected or expired
   request needs a new one.

Evidence (name -> value):

| Name | Value |
|---|---|
| `model_card` | sha256 of the card |
| `metrics` | sha256 of the metrics file (when the model has one) |
| `drift` | sha256 of `.tq/drift/<model>-result.json` (when `require` has `drift`) |
| `checks` | sha256 of the model card and repro check outcomes (ids, statuses, findings) |
| `git_head` | `commit:<sha>` |
| `clean_tree` | `clean` (`dirty` at apply time refuses) |
| `command` | sha256 of the filled promotion command argv |

## `MODEL_CARD.md` and `ml/model-card`

The template is `plugin/templates/process/MODEL_CARD.md`, and the skill
`model-card` writes from it. The check runs when a declared model's card, code,
metrics file or the data manifest changed, and on every model with `--all`.

| Rule | Level | When |
|---|---|---|
| `card-missing` | error | the card file does not exist |
| `card-heading-missing` | error | a required `##` heading is missing. The headings are Model details, Intended use / Out of scope, Training data, Evaluation data, Metrics, Fairness and slices, Limitations and risks, Reproducibility, Monitoring and drift, and Promotion history |
| `card-section-todo` | error / warn | a section is empty or holds only `TODO` (error), or still has `TODO` lines (warn). Promotion history may stay empty. `NEEDS-DECISION` counts as content |
| `card-metrics-empty` | error | the Metrics table has no row with a value |
| `card-snapshot-missing` | error | Training data names no snapshot or sha256 while `data_manifest` is set |
| `card-snapshot-mismatch` | error | the snapshot is not in the data manifest, or its sha256 differs |
| `card-pii` | error | a line matches the redaction detectors (the workspace's data classes plus secrets, or all detectors). The finding names the detector, never the value |
| `card-not-updated` | error | the model's code changed in the diff and its card did not |

The data manifest is read as `snapshots: {<name>: {sha256}}`, as
`snapshots: [{name, sha256}]`, or as the same shape at the top level. A
`dvc.lock` data manifest is not compared.

## Drift

`tq pack ml drift <model> [--json]` exits 0 on `ok` or `warn`, 1 on `fail`, and 2 on
an input or config error. It writes `.tq/drift/<model>-result.json`, schema
`tq.drift-result/v1`. The file is deterministic (it has no timestamps), so its hash
is stable evidence.

**Inputs.** Each input is a CSV or a profile JSON:

- **CSV:** a header row. A column whose non-empty values all parse as numbers is
  numeric. Its bins use the reference profile's edges, or the reference's deciles
  when the reference itself is a CSV. Every other column is categorical.
- **Profile JSON:** `tq.drift-profile/v1`:

  ```json
  {"schema": "tq.drift-profile/v1", "rows": 1000, "source": "...", "created_at": "...",
   "features": {
     "<numeric>": {"type": "numeric", "edges": [e0, e1, ...], "counts": [...]},
     "<categorical>": {"type": "categorical", "counts": {"<label>": n}, "hashed": false}}}
  ```

  - Numeric `counts` has `len(edges)+1` entries for open bins
    `(-inf, e0], (e0, e1], ..., (en, inf)`, or `len(edges)-1` entries for closed
    bins between the edges.
  - Hashed labels are the lowercase hex sha256 of the UTF-8 label. A `sha256:`
    prefix is accepted.
  - A categorical feature is compared on hashed labels when either side is hashed,
    when it has more than 50 labels, or when the CSV path is classified
    `pii`/`sensitive`/`financial`/`children` in `data.paths`.

**Metric.**

- PSI is `Σ (aᵢ − eᵢ)·ln(aᵢ/eᵢ)` over the bin proportions, with empty bins floored
  at 1e-4.
- `js` is the Jensen-Shannon distance with log base 2 (0 to 1).

**Status.**

- Each feature is `ok` below `warn`, `warn` from `warn`, and `fail` from `fail`.
  The per-feature overrides apply.
- A feature is also a `fail` when it is missing from current, has another type, has
  edges that differ from the reference ("rebuild the current profile with the
  reference edges"), or has no rows on one side.
- The overall result fails when any feature fails, when the share of warned features
  is above `max_warn_share`, or when a `drift.metrics.<m>.min` is not met. A
  missing metric counts as not met.
- The metrics file can be `{"auc": 0.81}`, `{"metrics": {"auc": 0.81}}` or
  `{"auc": {"value": 0.81}}`.

**`current_command`.**

- tq runs it when current is missing or older than 24 h.
- A command from the manifest runs directly.
- A command that comes from `tq-packs.yaml` runs only after the human types `yes` on
  a terminal. An agent session gets the command printed, so it can run it itself
  under the guards.
- The `ml/drift` check never runs the command: it reports a stale or missing current.

**`ml/drift`** runs in the definition of done only with `drift.in_dod: true`. Each
reason becomes an error finding `drift-fail`, a warned feature becomes a warn finding
`drift-warn`, and an input problem becomes `drift-input`.

**Privacy.** The output names features and numbers only, never category labels. tq
never queries data.

## `ml/promotion-in-code`

The check scans changed `.py` and `.ipynb` files for calls to registry promotion
APIs:

- `transition_model_version_stage` (the target is `stage=` or the third argument);
- `set_registered_model_alias` (the target is `alias=` or the second argument);
- `update_model_package(..., ModelApprovalStatus="Approved")`.

Levels:

- **error:** the target is a string literal in `promotion.protected`, or a SageMaker
  package is approved.
- **warn:** the target is not a literal, so tq cannot read it.
- In test files (`test_*.py`, `*_test.py`, `tests/`), every finding is a warn.

`scripts/promote.py` and the scripts that `promotion.command` runs are exempt.

## Guard `tq/model-promote` (deny)

The guard covers agent Bash, PowerShell and MCP tool calls:

- `tq pack ml promote apply` (human only);
- MLflow REST:
  - `POST .../model-versions/transition-stage`;
  - `POST` or `DELETE .../registered-models/alias` (a `GET` that reads an alias is
    allowed);
  - Databricks `transition-requests/approve` and Unity Catalog alias writes;
  - Vertex `:mergeVersionAliases`;
- Python (`python`, `py`, `uv run`, `poetry run`, ...) whose code calls
  `transition_model_version_stage`, `set_registered_model_alias`,
  `add_version_aliases` or `merge_version_aliases`, or
  `update_model_package ... Approved`;
- `aws sagemaker update-model-package ... --model-approval-status Approved`;
- `gcloud ai models ... --version-aliases`;
- `databricks registered-models set-alias`;
- `databricks model-registry transition-stage` and `approve-transition-request`;
- `az ml model update|create ... --stage`;
- the declared `promotion.command`, matched by its script path, its `-m module`, or
  its leading words;
- MCP tools whose name contains a promotion API name.

The human can still run these commands in their own terminal: the guard only sees
agent tool calls. `guard.disable` and `guard.actions` apply as for every guard.

## Triage and doctor

The triage flag `model_promotion` is raised when the promotion script, a model card
or `tq-packs.yaml` changes. Its reviewer is `ml-reviewer`.

Doctor findings:

- `ml-card-missing` (warn): a declared model has no card.
- `ml-drift-reference-missing` (warn): a configured reference is absent.
- `ml-promotion-command-missing` (warn): `protected` is set without a `command`.

Insights counters:

- `ml` `drift:<status>`;
- `promote_requested`;
- `promotions_applied`.
