# ml-starter

A small churn model built the way tq's ml pack checks for (spec C §4): a leakage-free
scikit-learn `Pipeline`, one seed helper, a lock file, a data snapshot pinned by sha256,
a model card whose numbers match the metrics file, drift profiles, and a promotion script
for MLflow aliases. The data is synthetic and generated at run time; nothing here talks
to a real registry in tests.

## Layout

| Path | What |
|---|---|
| `src/ml_starter/synth.py` | deterministic synthetic data (no names, e-mails or dates) |
| `src/ml_starter/data.py` | `snapshot_path`: reads a snapshot only when its sha256 matches `data/snapshots.yaml` |
| `src/ml_starter/features.py` | the feature list and the unfitted preprocessor, shared by training and serving |
| `src/ml_starter/train.py` | split (stratified, seeded), `Pipeline.fit` on the training split, metrics, run config, `model.skops` |
| `src/ml_starter/evaluate.py` | AUC with a bootstrap interval, Brier score, prevalence baseline, per-plan slices |
| `src/ml_starter/serve.py` | `load_model` with a reviewed skops type list, `predict_scores` |
| `src/ml_starter/seed.py` | `set_seed`, the declared `packs.ml.seed_helper` |
| `src/drift_profile.py` | writes `tq.drift-profile/v1` profiles for `tq pack ml drift` |
| `scripts/promote.py` | `packs.ml.promotion.command`: points an MLflow alias at a version |
| `data/snapshots.yaml` | `packs.ml.data_manifest`: snapshot name -> uri + sha256 |
| `data/profiles/churn-train.json` | the drift reference profile |
| `models/churn/MODEL_CARD.md` | the model card (`ml/model-card`) |
| `reports/churn/metrics.json` | the holdout metrics the card quotes |
| `tq-packs.yaml` | the repo layer of `packs.ml` (add-only) |

## Run it

```powershell
uv sync --extra test
uv run python -m ml_starter.synth --out data/raw/churn.csv
uv run python -m ml_starter.train
uv run python -m drift_profile --data data/raw/churn.csv --exclude customer_id,churned --source churn-train --created-at 2026-10-28T00:00:00Z --out data/profiles/churn-train.json
uv run python -m ml_starter.synth --out data/raw/churn-current.csv --seed 8 --shift 0.5
tq pack ml drift churn
uv run pytest
```

`data/raw/` is gitignored. The generator is deterministic, so the file it writes has the
sha256 pinned in `data/snapshots.yaml`; a different file is refused by training. With
real data, pin each snapshot (or use DVC), and update the card when the hash changes.

## Promotion

`tq pack ml promote request churn <version> --to champion` checks the card, the drift
result and the repro checks, then asks the human on the hub. After approval, the human
runs `tq pack ml promote apply <decision>`, which runs `scripts/promote.py` with
`MLFLOW_TRACKING_URI` (and `MLFLOW_TRACKING_TOKEN` or `MLFLOW_TRACKING_USERNAME`/
`MLFLOW_TRACKING_PASSWORD` when the server needs auth) from the environment:
`tq dotenv run -- tq pack ml promote apply <decision>`. Agents never run the script or
`apply`; the guard `tq/model-promote` denies it. The script checks the version is
`READY`, sets the alias over the MLflow REST API (`POST /api/2.0/mlflow/registered-models/alias`),
verifies it, never prints credentials or server messages, and times out each call after
`MLFLOW_HTTP_REQUEST_TIMEOUT` seconds (default 30).

## Adapting it

- Rename the model in `src/ml_starter/__init__.py`, the card path, the metrics path and
  `tq-packs.yaml`.
- Keep preprocessing inside the `Pipeline`, pass `random_state` to every estimator and
  splitter, read data only through `snapshot_path`, and save models with `skops`.
- When a feature comes from a column classified in the manifest `data:` block, record its
  purpose in `docs/privacy.md`, pass it to `drift_profile.py --classified` so its labels
  are hashed, and add its slices to the card.
- Parquet input needs the `parquet` extra (`uv sync --extra parquet`).
