# ML pack: review and the `ml-starter` template

Part of the ml pack (spec C §4.7, §5.5). The leakage lint and the reproducibility checks
are on [ML lints](ml-lints.md); the promotion gate, `MODEL_CARD.md` and the drift check
are on [Promotion, card, drift](ml-promotion.md). The framework is in
[`docs/PACKS.md`](../PACKS.md).

## The `ml-reviewer` agent

`plugin/agents/ml-reviewer.md` is a read-only reviewer (tools Read, Glob, Grep; no
edit, write, shell or sub-agent tools). It returns the answer-key verdict of the review
contract (`RESULT: PASS | FAIL | BLOCKED`, `BUILT-BY-ME:`, F rows with `file:line`, U
rows for what only a human can settle), plus a `JUDGED:` line naming every checklist row.

**Selected on** the triage flags `ml` (training, feature or eval files under
`packs.ml.paths`, or a changed `.py`/`.ipynb` calling `.fit(`, `optimizer.step(` or
`trainer.train(`) and `model_promotion` (the promotion script, `packs.ml.promotion` or a
model card changed). `tq pack crew --flags ml` prints `ml-reviewer`; the build engine and
the `feature` skill add whatever `tq pack crew` prints. The build gate accepts the name
because it matches `*-reviewer` and the agent file exists.

**Input:** the diff or review bundle, the `tq pack check --pack ml --json` results, the
spec slice (Models touched, Data snapshot, Evaluation plan), each touched model's card
and metrics file, the `data_manifest`, `docs/privacy.md` and the manifest `data:` block.

### Checklist (`plugin/skills/ml-review/references/checklist.md`)

| Row | Question |
|---|---|
| M1 Leakage | the L1–L8 findings of `ml/leakage` judged, plus leakage the lint can't see: duplicates across splits, features computed after the label time, target encoding without folds |
| M2 Split strategy | a time, group or stratified split that matches how the model is used; the holdout untouched until the final evaluation |
| M3 Metrics | the metric fits the decision, a baseline is compared, intervals are given, calibration is checked when scores are read as probabilities |
| M4 Reproducibility | seeds, snapshot hash, lock and a recorded command; the `ml/*` repro results judged |
| M5 Training/serving skew | the same feature code at training and inference; no notebook-only transforms |
| M6 Personal data as features | classified columns as features: purpose and legal basis in `docs/privacy.md`, proxies for protected attributes, slices in the card |
| M7 Model card | updated, accurate against the metrics file, no personal data |
| M8 Serialization and supply chain | `safetensors`/`skops`, `weights_only=True`, pretrained weights pinned by hash |
| M9 Promotion path | only through `tq pack ml promote`; no promotion API calls outside the promotion script |
| M10 Cost and resources | GPU or cluster hours bounded, early stopping, bounded search |
| M11 PyTorch specifics | `eval()`/`no_grad()`, determinism notes, DataLoader worker seeding |

Severity: a metric that can't be trusted (confirmed L1–L4, the test set used for fitting
or selection), a promotion path outside `tq pack ml promote`, or an unsafe load of a
downloaded file is a BLOCKER; a confirmed `error` finding of any `ml/*` check or a card
number that disagrees with the metrics file is MAJOR; a confirmed `warn` is MINOR.

## The `ml-review` skill

`plugin/skills/ml-review/SKILL.md` runs `tq decide triage` and `tq pack crew`, then
`tq pack check --pack ml --json --budget 600s` over the same change set (outer timeout
630 s), quotes `ml/leakage` and the reproducibility results (`ml/seeds`, `ml/env-lock`,
`ml/data-snapshot`, `ml/run-tracking`) to the reviewer, and records the verdict with
`build-gate record-review ... --reviewer ml-reviewer` inside a build. Standalone it
reports the findings and fixes them. It never runs `tq pack ml promote apply` (human
only), never edits `tq-packs.yaml` or a baseline to make a check pass, and never reads
`.env` files. Headless (`TENTAQLES_HEADLESS=1`), U rows go to `build-gate ask`.

The on-ramp notes are `plugin/skills/pack/references/onramps/ml-review.md` (`build`,
`feature`, `fix`, `kickoff`, `ship`).

## The `ml-starter` template

`templates/ml-starter/` is a churn model that passes the ml pack's built-in checks:

| Pack rule | How the template meets it |
|---|---|
| `ml/leakage` | preprocessing (`StandardScaler`, `OneHotEncoder`) lives in a `Pipeline` fitted on the training split only; the id and the target are never features |
| `ml/seeds` | `ml_starter.seed.set_seed` (declared as `seed_helper`) and `random_state=` on the split and the estimator; no legacy `np.random.seed` |
| `ml/env-lock`, `ml/lock-check` | `pyproject.toml` with `uv.lock` (`uv lock --check`) |
| `ml/data-snapshot` | training reads only through `snapshot_path`, which checks the sha256 in `data/snapshots.yaml` |
| `ml/run-tracking` | `reports/churn/run.json` next to the metrics: params, seed, snapshot, lock hash, git commit, command |
| `ml/unsafe-deserialize` | the model is saved with `skops` and loaded with a reviewed type list |
| `ml/promotion-in-code` | registry calls only in `scripts/promote.py` |
| `ml/model-card` | `models/churn/MODEL_CARD.md` with every required heading; its numbers and snapshot hash match `reports/churn/metrics.json` and the manifest (tested) |

**`drift_profile.py`** writes `tq.drift-profile/v1`:

```json
{"schema": "tq.drift-profile/v1", "rows": 2000, "source": "churn-train", "created_at": "2026-10-28T00:00:00Z",
 "features": {"tenure_days": {"type": "numeric", "edges": [1.0, 43.0, 97.0, "..."], "counts": [196, 202, "..."]},
              "plan": {"type": "categorical", "counts": {"basic": 693, "plus": 650, "pro": 657}, "hashed": false}}}
```

- Numeric `edges` are the reference deciles (duplicates dropped), and `counts` has one
  entry per bin (`len(edges) - 1`). A value `v` falls in bin `i` when
  `edges[i] <= v < edges[i+1]`; values below the first edge go to the first bin, values
  at or above the last edge to the last. With `--reference`, the current profile reuses
  the reference edges and hashing. Missing values are counted in an optional `missing`.
- Categorical labels are hashed as `h:` plus the first 16 hex digits of the sha256 of the
  UTF-8 label when the column is passed with `--classified`, when `--hash-all` is set,
  when there are more than 50 labels, or when the reference hashed it. Hashed labels
  never appear in the output.
- `--model models/churn/model.skops` adds `__prediction__`, the score distribution.

**`scripts/promote.py <model> <version> <target>`** is the `promotion.command`. It reads
`MLFLOW_TRACKING_URI` (http or https only) and optional `MLFLOW_TRACKING_TOKEN` or
`MLFLOW_TRACKING_USERNAME`/`MLFLOW_TRACKING_PASSWORD` from the environment, checks the
version is `READY` (`GET /api/2.0/mlflow/model-versions/get`), sets the alias
(`POST /api/2.0/mlflow/registered-models/alias` with `name`, `alias`, `version`) and
verifies it. Arguments are limited to `[A-Za-z0-9._@-]`, the alias may not be `latest` or
`v<n>`, each request times out after `MLFLOW_HTTP_REQUEST_TIMEOUT` seconds (default 30,
max 120), and neither credentials nor server messages are printed. Exit 0 done, 2 usage,
3 registry error or timeout. Only the human runs it, through `tq pack ml promote apply`.

**Tests** (`templates/ml-starter/tests`, run by the CI templates loop): the snapshot
hash, the pipeline seeing only the training split, two runs giving the same metrics, the
card matching the metrics file, the skops type review, the drift profile shape, edge
reuse and hashing, the promotion script against a fake MLflow server (`http.server`:
success, token sent and never printed, missing or not-ready version, registry error, bad
arguments, timeout), and text checks for the pack rules. With
`TQ_ML_STARTER_PACK_CHECK=1` and an ml-pack `tq` on PATH, one more test runs
`tq pack check --all --builtin-only --pack ml` over a copy and expects no error findings.
