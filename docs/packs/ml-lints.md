# ML pack: leakage and reproducibility lints

The ML pack (`packs.ml`) lints training code before it reaches review. It covers spec C
§4.2 (the leakage lint) and §4.3 (the reproducibility checklist). The model card, drift
and the promotion gate are documented in [ml-promotion.md](ml-promotion.md).

Every check runs under `tq pack check` and in the definition of done as `pack:ml/<name>`.
Built-in checks are always required. An external check is required only when its tool is
listed in `packs.ml.tools`; otherwise it is advisory.

## Config

```yaml
packs:
  ml:
    enabled: true
    tools: [ruff, uv]                       # dvc and poetry when the project uses them
    paths:
      train: ["src/train/**", "notebooks/**"]
      features: ["src/features/**"]
      eval: ["src/eval/**"]
    seed_helper: src/common/seed.py:set_seed  # a call to set_seed(...) counts as every seed
    data_manifest: data/snapshots.yaml        # or dvc.lock
```

| Key | Meaning | `tq-packs.yaml` merge |
|---|---|---|
| `paths.train` | Training files. When it is empty, a training file is any changed `.py` or `.ipynb` that calls `.fit(`, `optimizer.step(` or `trainer.train(` | union |
| `paths.features` | Feature code: L5 runs here, and L7 reads its `groupby` columns | union |
| `paths.eval` | Evaluation code, scanned by the leakage lint | union |
| `seed_helper` | `<path>.py:<function>`. A call to the function sets every global seed | trusted (the repo may add it, never change it) |
| `data_manifest` | A YAML file mapping each name to `{uri, sha256}` (a list of `{name, uri, sha256}` or a `snapshots:` block also works), or a `dvc.lock` | trusted |

The validator rejects unknown keys under `paths`, globs that are absolute or contain `..`,
a `seed_helper` that is not `<path>.py:<function>`, and an absolute `data_manifest`.

Notebooks (`.ipynb`, Fabric `notebook-content.py`, and Synapse notebook JSON under
`paths`) are scanned cell by cell in order, as one file. A notebook finding has the cell's
file line, and its message starts with `cell N, line M:`.

## `ml/leakage` (built-in)

This is a heuristic over call order and names, not a dataflow analysis. The
`ml-reviewer` agent judges what the lint can't see.

| Rule | Level | Flags |
|---|---|---|
| `L1-fit-before-split` | error | `.fit(` or `.fit_transform(` of a preprocessing object before the first split, on an argument whose name doesn't contain `train`. The objects: `StandardScaler`, `MinMaxScaler`, `RobustScaler`, `MaxAbsScaler`, `OneHotEncoder`, `OrdinalEncoder`, `LabelEncoder`, `SimpleImputer`, `KNNImputer`, `IterativeImputer`, `PCA`, `TfidfVectorizer`, `CountVectorizer`, `SelectKBest`, `SelectPercentile`, `TargetEncoder`, `QuantileTransformer`, `PowerTransformer`, `KBinsDiscretizer`. A split is `train_test_split`, `random_split`, a splitter class, or `.split(X, …)` on a splitter. A string `.split(",")` doesn't count |
| `L2-test-in-fit` | error | `.fit(`, `.fit_transform(` or `.partial_fit(` with an argument named like `test`, `holdout`, `val` or `valid` (`X_test`, `xval`, `holdout_df`). Keywords whose own name says eval, val or test (`eval_set=`, `validation_data=`) are allowed |
| `L3-cv-outside-pipeline` | error | `cross_val_score`, `cross_validate`, `cross_val_predict` (and the curve helpers), or `<search>.fit` on a `GridSearchCV`/`RandomizedSearchCV`/`Halving*`/`BayesSearchCV`, run on an array that an earlier `fit_transform`, or a `fit` then `transform`, produced from the full data |
| `L4-target-in-features` | error | `y = df["t"]` (or `df[TARGET]`) with `X = df` and no drop, pop or `del` of `t` anywhere in the file, or `X = df[[…, "t"]]` / `X = df[FEATURES]` with `t` in the list |
| `L5-lookahead` | warn | in feature files: `.shift(-n)`, `rolling(…, center=True)`, `bfill()`/`backfill()`, `fillna(method="bfill")`. A write to a target column (`target`, `label`, `y`) is skipped. With `paths.features` empty, every scanned file counts |
| `L6-timeseries-shuffle` | warn | `train_test_split` without `shuffle=False` in a file that sorts or indexes by a datetime-like column (`sort_values("order_date")`, `set_index`, `index_col`, `parse_dates` with `sort_index`), or that uses `TimeSeriesSplit` |
| `L7-group-leak` | warn | `train_test_split` or a non-group splitter (`KFold`, `StratifiedKFold`, `ShuffleSplit`, …) with no `groups=`, when the file mentions an id column (`customer_id`, `*_id`, `uuid`) that `paths.features` code groups by. With `paths.features` empty, the file's own `groupby` calls count. At most 200 feature files are read |
| `L8-torch-eval` | warn | in a file importing torch: a `for … in <val/test loader>` loop whose enclosing function (with its decorators) or body lacks `model.eval()`, or `torch.no_grad()`/`inference_mode()` |

## Reproducibility checks

| Id | Kind | Rules |
|---|---|---|
| `ml/seeds` | built-in | `seed-missing` (error): a training file sets no seed. Any of these counts: `random.seed(n)`, `np.random.seed(n)`, `default_rng(n)`/`RandomState(n)`, `torch.manual_seed(n)`, `seed_everything`, `set_seed`, `tf.random.set_seed`, or the `seed_helper`. `seed-torch` (error): torch is imported but `torch.manual_seed` is not set. `seed-cuda` (error): `cuda` appears but `torch.cuda.manual_seed_all` is not set. `seed-unseeded-rng` (error): `default_rng()` with no seed. `random-state` (error): a randomised estimator or splitter (random forests, boosting, trees, SGD, MLP, KMeans, TSNE, shuffle splitters, `RandomizedSearchCV`, XGBoost, LightGBM, CatBoost, `train_test_split` unless `shuffle=False`, `KFold`/`StratifiedKFold` with `shuffle=True`) built without `random_state`. A `**kwargs` call is skipped. `torch-deterministic` (warn): `torch.use_deterministic_algorithms` is not called. `dataloader-seed` (warn): a `DataLoader` has neither `worker_init_fn` nor `generator` |
| `ml/env-lock` | built-in | `lock-stale` (error): the dependency tables of a `pyproject.toml` changed (`[project] dependencies` and `requires-python`, optional dependencies, `[dependency-groups]`, `[tool.uv]`, `[tool.poetry.*dependencies]`, `[tool.pixi.*dependencies]`, `[build-system]`), or a `requirements*.in`, `environment.yml` or `pixi.toml` changed, and no lock next to it changed (`uv.lock`, `poetry.lock`, `pdm.lock`, `pixi.lock`, the same-stem `.txt`, `conda-lock.yml`). A change to other tables, such as `[tool.ruff]`, is ignored. `lock-missing` (error): no lock exists. `unpinned` (error): a changed `requirements*.txt` line without `==` (a direct reference with `@` or a URL counts as pinned; `-r`/`-c`/`-e` and other options are skipped). `no-hashes` (warn): a `requirements*.txt` without `--hash=` |
| `ml/data-snapshot` | built-in | `snapshot-unpinned` (error): a training-file read (`read_csv`, `read_parquet`, `read_json`, `read_excel`, `read_feather`, `read_table`, `read_orc`, `read_hdf`, polars `scan_*`, `load_dataset`, `np.loadtxt`, `spark.read.<fmt>(…)`) of a path that `data_manifest` does not pin with a sha256, that no hashed `dvc.lock` entry covers, and that has no `.dvc` file next to it or to a parent directory. `snapshot-moving` (error): a path naming `latest`; a table read (`spark.table`, `spark.read.table`, `read_sql_table`, `read_delta`, a Delta `load`) with no `versionAsOf`/`timestampAsOf`/snapshot id; a `SELECT` with no as-of bound (`FOR SYSTEM_TIME AS OF`, `VERSION AS OF`, `TIMESTAMP AS OF`, Snowflake `AT(…)`/`BEFORE(…)`). `snapshot-unresolved` (warn): the source is not a literal or a module constant |
| `ml/run-tracking` | built-in, warn only | `run-tracking`: a training file has no `mlflow.autolog`/`<flavor>.autolog`, no `log_params` together with `log_metric(s)` (MLflow, dvclive and similar), no wandb `init(config=)` with `log`, no ClearML `Task.init`, and no run config dumped next to a metrics file |
| `ml/unsafe-deserialize` | built-in | `unsafe-deserialize`: `pickle`/`cPickle`/`dill`/`cloudpickle`/`joblib` `load(s)`, `torch.load` without `weights_only=True`, `pd.read_pickle`, or `np.load(allow_pickle=True)`, on a source that comes from a download, lies outside the repo (absolute, `~`, a URI or `..`), or can't be traced to a repo-relative literal. The source is traced through `with open(…) as f`, assignments, constants, `open(…)` and `Path(…)`. This is an error in code and a warning in tests (`tests/`, `test_*.py`, `*_test.py`, `conftest.py`) |
| `ml/notebook-outputs` | built-in | `notebook-outputs` (warn): a changed `.ipynb` keeps stored outputs. `notebook-output-pii` (error): an output matches the redaction detectors (every class, because a commit is an exit). The message names the cell and the detector, never the text |
| `ml/ruff` | external `ruff` ≥ 0.6.0, 60 s | `ruff check --no-cache --exit-zero --select NPY,PD --output-format json <changed .py/.ipynb>` (the repo root when more than 200 files changed). Each finding is an error with ruff's code as its rule. `NPY002` (legacy `np.random`) points to `ml/seeds`. Findings can be baselined (`tq pack baseline --pack ml`, human only) |
| `ml/lock-check` | external `uv`, 60 s | `uv lock --check` in each directory where a changed `pyproject.toml` or `uv.lock` sits next to a `uv.lock`. A non-zero exit is a `lock-outdated` error on that lock |
| `ml/lock-check-poetry` | external `poetry`, 60 s | `poetry check --lock`, the same way, next to a `poetry.lock` |
| `ml/dvc-status` | external `dvc`, 60 s | `dvc status --json`, local only (never `-c`/`--cloud`). It runs when a `.dvc`, `dvc.yaml` or `dvc.lock` changed, or when the repo has a `.dvc/` directory. `changed outs` is a `dvc-changed-outs` error and `changed deps` a `dvc-changed-deps` warning; `always changed` stages are ignored. Output that doesn't parse fails the check, with the redacted tail |

`ml/lock-check` and `ml/lock-check-poetry` are separate ids because a check wraps exactly one
tool. A project that uses uv declares `uv`; one that uses Poetry declares `poetry`.

## Suppressions

Built-in findings don't use the baseline. Suppress one on its line, or on a comment-only
line just above it:

```python
# tq:allow L2: the prefit calibrator is fitted on the validation split by design
calibrator.fit(X_val, y_val)
```

The key can be the rule id (`L2-test-in-fit`), its short form (`L2`) or name (`test-in-fit`),
or the check id (`ml/leakage`). The reason after the colon is required; a suppression
without one is ignored. A file-level finding (`seed-missing`, `run-tracking`) is
suppressed by a comment-only line anywhere in the file.

## Triage

The `ml` flag (reviewer `ml-reviewer`) is raised when a changed file matches `paths.*`. When
`paths.train` is empty, it is also raised by a changed `.py`/`.ipynb` that calls `.fit(`,
`optimizer.step(` or `trainer.train(`. That probe reads at most 50 files and 1 MiB each. The
Jev question is: "Does this change alter how a model is trained or evaluated, or which data
reaches its features?"

## Data protection

Findings carry `file:line`, column, feature and table names, and repo paths. A remote source
appears only as its scheme (`a s3:// source`). Query text, notebook output text and data
values never appear. Tool tails pass the redaction detectors.

## Bounds

A built-in check scans at most 500 changed files of up to 8 MiB each, within the
framework's 10 s built-in budget. External checks have a 60 s default, which
`packs.ml.timeouts.<tool>` can override (capped at 1800 s).
