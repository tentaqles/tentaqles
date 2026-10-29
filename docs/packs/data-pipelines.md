# Data pack: pipelines, Fabric and Synapse

Part of the data pack (spec C §3.4, §3.5). The checks lint Python pipelines
(Airflow, Dagster, Prefect, pandas, polars, PySpark), Microsoft Fabric and Azure
Synapse git-integration items and notebooks. They are pure Go, never connect to a
warehouse or a workspace, and run in `tq pack check` and the definition of done.
`docs/PACKS.md` covers the framework: config layers, merge rules, CLI and DoD.

## What is scanned

- Every changed file under `packs.data.pipelines.paths` (default: `dags/**`,
  `pipelines/**`, `**/*.Notebook/**`, `notebook/**`, `pipeline/**`).
- Every changed `.py` or `.ipynb` outside those paths that imports `airflow`,
  `dagster` or `prefect`.
- Notebooks are read through their code cells: `.ipynb`, Fabric
  `notebook-content.py` and Synapse notebook JSON. Magics (`%`, `!`) and non-Python
  cells are skipped.
- Fabric items: `<name>.Notebook/` (`.platform`, `notebook-content.<ext>`) and
  `<name>.DataPipeline/pipeline-content.json`. Synapse resources: one JSON file per
  resource under `notebook/`, `pipeline/`, `linkedService/`, `sqlscript/`,
  `dataflow/`, `trigger/`, `sparkJobDefinition/`.
- Files over 4 MiB are skipped and counted in the result detail.

## Config

```yaml
packs:
  data:
    enabled: true
    tools: [dagster]                 # makes data/dagster-validate required
    pipelines:
      paths: ["dags/**", "etl/**"]   # union with tq-packs.yaml
      masking: [acme.mask_email]     # extra masking functions; manifest only
    dbt:
      contract_paths: ["models/marts/**"]   # pipeline-backfill is an error here
```

| Key | Merge with `tq-packs.yaml` | Meaning |
|---|---|---|
| `pipelines.paths` | union | globs of pipeline files (`**` spans folders; a glob without `/` matches the base name) |
| `pipelines.masking` | trusted layer only (the repo value is ignored) | function or macro names that mask a column, on top of the built-in list |

`pipelines` accepts only these two keys; anything else fails `tq allow`.

## Checks

| Id | Kind | Rule ids | Level |
|---|---|---|---|
| `data/airflow-dag` | built-in | `airflow-dynamic-start-date` | error |
| | | `airflow-catchup-unset`, `airflow-no-retries`, `airflow-top-level-io` | warn |
| `data/pipeline-backfill` | built-in | `wall-clock-filter` | warn; error in a file under `dbt.contract_paths` |
| `data/pipeline-idempotency` | built-in | `append-no-dedupe` | warn |
| `data/pipeline-pii-output` | built-in | `pii-to-output` | error |
| `data/fabric-artifacts` | built-in | `fabric-literal-onelake-path` | error |
| | | `fabric-no-parameters-cell`, `fabric-pinned-lakehouse`, `fabric-copy-no-retry` | warn |
| `data/synapse-artifacts` | built-in | `synapse-inline-secret` | error |
| | | `synapse-hardcoded-pool` | warn |
| | | `synapse-unparsed` | warn |
| `data/notebook-outputs` | built-in | `notebook-output-pii` | error |
| | | `notebook-outputs` | warn |
| `data/dagster-validate` | external `dagster` (timeout 120 s) | the tool's exit code | fail on a non-zero exit |

### `data/airflow-dag`

Files that import `airflow` and build a `DAG(...)` or `@dag(...)`.

- `airflow-dynamic-start-date`: `start_date` from `datetime.now()`, `utcnow()`,
  `today()`, `date.today()`, `days_ago(...)`, `pendulum.now()` or
  `timezone.utcnow()`, as a keyword or in `default_args`, directly or through
  a name assigned from one of those (`YESTERDAY = datetime.now() - timedelta(1)`,
  then `"start_date": YESTERDAY`). Use a fixed `pendulum.datetime(...)`.
- `airflow-catchup-unset`: no `catchup=` on the DAG. Airflow 3 defaults to `False`
  and Airflow 2 to `True`, so say which one you mean.
- `airflow-no-retries`: no `retries` in `default_args` or on any task.
- `airflow-top-level-io`: at module level, `requests.*`, `urlopen`,
  `pd.read_*`/`pl.read_*`, `<driver>.connect(...)` or `create_engine(...)`. The
  scheduler runs that code on every parse.

### `data/pipeline-backfill`

Wall-clock time where the run's logical date belongs:

- Python: `datetime.now()`, `utcnow()`, `today()`, `date.today()`, `pendulum.now()`,
  `current_date()`, `current_timestamp()`, `getdate()` or `now()` inside
  `.filter(...)`, `.where(...)`, `.query(...)`, `.isin(...)`, `.between(...)`,
  `.loc[...]`, a pandas boolean mask (`df[df.order_date == date.today()]`: a
  subscript holding a comparison or `&`, `|`, `~`) or a `partition...=` value. A
  variable assigned from one of those and used in a filter counts too. A column
  pick (`df["col"]`), a slice and a column assignment are not filters.
- SQL, in `.sql` files and in Python string literals: `CURRENT_DATE`,
  `CURRENT_TIMESTAMP`, `GETDATE()`, `GETUTCDATE()`, `SYSDATE`, `SYSDATETIME()`,
  `NOW()` or `TODAY()` after `WHERE`, `HAVING`, `QUALIFY`, `ON` or `PARTITION`.
- An audit column (`withColumn("loaded_at", current_timestamp())`,
  `UPDATE SET updated_at = CURRENT_TIMESTAMP`) is fine.

### `data/pipeline-idempotency`

An append (`.mode("append")`, `mode="append"`, `if_exists="append"`,
`if_table_exists="append"`, `.insertInto(...)` without `overwrite=True`,
`INSERT INTO ... SELECT`) in a file that has no dedupe anywhere. Dedupe means
a Delta merge (`whenMatched*`/`whenNotMatched*`), `replaceWhere`,
`partitionOverwriteMode`, an overwrite, replace or merge mode,
`dropDuplicates`/`drop_duplicates` or an upsert in code, and `MERGE`, `DELETE`,
`TRUNCATE`, `INSERT OVERWRITE`, `ON CONFLICT` or `QUALIFY ROW_NUMBER` in SQL (a
`.sql` file or a Python string literal). Comments, docstrings and SQL string
values never count, and a bare `.merge(` is a pandas join, not a dedupe. It is a warning: a log table may append by design, and the
reviewer's D1 row decides.

### `data/pipeline-pii-output`

A heuristic; the reviewer (D5) judges the rest.

1. **Inputs.** Tables the file reads: SQL `FROM`/`JOIN` relations, dbt
   `ref('x')`/`source('s', 'x')`, and `spark.table("x")`, `.read_table`,
   `.read_sql_table`, `.read_delta` or `.load`. An input is classified when the
   manifest `data.paths` lists its table with `pii`, `sensitive`, `financial` or
   `children`.
2. **Columns.** When an input is classified, the common personal-data names
   (`email`, `cpf`, `phone`, `birth_date`, `address`, `name`) are followed. So is
   any column that a contract under `contracts.paths` classifies with one of those
   classes, whatever the input. The default contract paths are
   `contracts/**/*.odcs.yaml` and `**/*.odcs.yml`, and at most 200 contracts are
   read.
3. **Outputs.** `saveAsTable`, `insertInto`, `to_sql`, `to_csv`/`to_parquet`/
   `to_json`/`to_excel`, `write_csv`/`write_parquet`/`write_delta`/`write_database`,
   `save`/`csv`/`parquet`/`json` writers, and SQL `INSERT INTO`, `INSERT OVERWRITE`,
   `CREATE TABLE|VIEW ... AS` and `MERGE INTO`. A dbt model (`.sql` with Jinja and
   no write statement) writes to the model named after the file.
4. **Finding.** A followed column is used unmasked, and an output is not
   classified with the column's class (by table in `data.paths`, or by path glob
   for files and models). Uses are quoted names in Python (`col("email")`,
   `df["email"]`) and words in SQL text; an `AS email` alias is not a use.
5. **Masked.** The use sits inside a call to `sha2`, `sha256`, `sha1`, `md5`, `hash`,
   `farm_fingerprint`, `xxhash64`, `mask` or `hashbytes`, at any depth, or to a
   function listed in `packs.data.pipelines.masking`. This is where a project's own
   masking macros go, for example a dbt macro `mask_email`. A dotted name
   (`acme.mask_email`) matches a call by its whole name or a dotted suffix, so
   `acme.mask_email(...)` and `udfs.acme.mask_email(...)` are both masked.

The finding names the column, its class and the output, never a value. To fix it,
mask the column or drop it. If the output may hold the data, classify it in the
manifest `data.paths` and record why in `docs/privacy.md`. That is the human's
change.

### `data/fabric-artifacts`

- `fabric-literal-onelake-path` (error): `abfss://<workspace GUID>@onelake.dfs.fabric.microsoft.com/...`
  or `onelake.dfs.fabric.microsoft.com/<workspace GUID>/...` in a changed
  pipeline, notebook, `.py` or `.ipynb` file. The all-zero placeholder GUID is
  fine. Use the default lakehouse, a relative `Tables/` or `Files/` path, or a
  parameter.
- `fabric-pinned-lakehouse` (warn): the notebook metadata sets
  `default_lakehouse_workspace_id` to a real workspace GUID.
- `fabric-no-parameters-cell` (warn): a pipeline's notebook activity references the
  notebook by its `.platform` `logicalId` (`notebookId`), and the notebook has no
  parameters cell. A parameters cell is a `# PARAMETERS CELL ****` marker, or cell
  metadata tagged `"parameters"`. tq reads at most 500 tracked `.platform` and
  `pipeline-content.json` files to resolve the references.
- `fabric-copy-no-retry` (warn): a `Copy` activity, at any depth (`ForEach`, `If`,
  `Until`), with `policy.retry` 0 or with no retry policy.

### `data/synapse-artifacts`

- `synapse-inline-secret` (error): a property named `password`, `pwd`,
  `accountKey`, `servicePrincipalKey`, `servicePrincipalCredential`,
  `clientSecret`, `sasToken`, `sasUri`, `accessKey`, `secretAccessKey`, `apiKey` or
  `credential` holds a literal or `SecureString` value. Or a `connectionString`
  carries `Password=`, `Pwd=`, `AccountKey=`, `SharedAccessKey=`,
  `SharedAccessSignature=` or `sig=`. Values of type `AzureKeyVaultSecret` or
  `Expression`, `@...` expressions and `****` masks are fine.
- `synapse-hardcoded-pool` (warn): `sqlPool.referenceName` in a pipeline, or
  `currentConnection.poolName` in a SQL script, names a pool literally (not `@...`
  and not the serverless `Built-in` pool).
- `synapse-unparsed` (warn): a resource file does not parse as JSON, so its
  credentials and pools were not checked. Valid JSON without `properties` is not
  a Synapse resource and is not counted.

### `data/notebook-outputs`

Changed `.ipynb` files and Synapse notebook JSON. Fabric stores no outputs in git.

- `notebook-outputs` (warn): cells with stored outputs. Clear them with
  `jupyter nbconvert --clear-output --inplace` or `nbstripout`.
- `notebook-output-pii` (error): a stored output's text hits the redaction
  detectors (every class). The finding names the detectors and counts, never the
  value. `tq:allow-pii` does not exempt outputs: a commit is an exit.

### `data/dagster-validate`

Runs `dagster definitions validate` in the repo root when the diff changes a `.py`
file that imports `dagster`, `workspace.yaml`, `dagster.yaml`, `dg.toml`, or a
`pyproject.toml` with `[tool.dagster]` or `[tool.dg]`. Otherwise it is a skip. The
timeout is 120 s, and `packs.data.timeouts.dagster` overrides it. On a failure the
detail carries the last 20 lines of the tool's output, redacted. The check is
required only when `dagster` is in `packs.data.tools`. Dagster now recommends the
`dg` CLI (`dg check defs`) for new projects. The legacy `dagster` command still
ships, and the check uses it.

## Suppressions

A built-in finding in Python or SQL source is silenced by a comment on its line or
the line above:

```python
df.write.mode("append").saveAsTable("audit.events")  # tq:allow append-no-dedupe: audit log, one row per run id
```

The reason after the colon is required. JSON items (Fabric pipelines, Synapse
resources) take no comments, so fix them instead.

## Triage flag and reviewer

| Flag | Raised when | Reviewer |
|---|---|---|
| `pipeline` | a changed path under `pipelines.paths`, the dbt model paths (`<dbt.project>/models/**`, default `models/**`), Fabric `*.Notebook/`/`*.DataPipeline/` items or Synapse resource folders. The Jev question: "Does this change alter a data pipeline, dbt model or scheduled job in a way that could duplicate, drop or reorder rows on a rerun or backfill?" | `pipeline-reviewer` |

`pipeline-reviewer` (plugin `agents/pipeline-reviewer.md`, skill `pipeline-review`)
is read-only. It judges the checklist rows D1 to D11 and returns the review contract
verdict. The `data_contract` and `warehouse_cost` flags of the other data workstreams
map to it too.

## Guard `tq/pipeline-run`

The guard asks before an agent starts an orchestrator run (Bash and PowerShell):

| Command | Reason given |
|---|---|
| `airflow dags trigger` | starts a run: confirm a dev environment |
| `airflow dags backfill` (Airflow 2), `airflow backfill create` (Airflow 3) | "check D1 and D2 idempotency first" |
| `dagster job launch`, `dagster job execute`, `dagster asset materialize`, `dg launch` | starts a run |
| `prefect deployment run` | starts a run |
| `az datafactory pipeline create-run`, `az synapse pipeline create-run` | starts a run |

`--help` and `-h` are allowed. The guard is pure: it splits the command into shell
segments and words, and it reads no files. A bash `\` or PowerShell backtick
before a newline joins the two lines. It looks through wrappers:

- launchers and runners: `sudo`, `env`, `nohup`, `VAR=value`, `uv run`,
  `poetry run`, `python -m` (any `python3.N`, `py`, `pypy3`);
- shells: `bash|sh|zsh -c '...'`, `pwsh|powershell -Command ...`, `cmd /c ...`,
  `eval`, whose script is split again;
- `astro dev run <args>`, which is the Airflow CLI;
- container and remote wrappers, whose inner command is matched at every later
  word: `docker|podman [compose] exec|run`, `docker-compose exec|run`,
  `kubectl|oc exec|run|debug`, `ssh host '...'` and `tq dotenv run [--file f] --`.

Nesting is bounded (4 levels and a step budget). A command nested deeper than that
fails closed: the guard asks. `guard.disable` and `guard.actions` (for
example `deny`) apply to it as to every guard. Detection hints for `tq doctor`
(`packs-suggest`) are `from airflow`, `import dagster`, `from prefect`, a Fabric
`.platform` file, Synapse `notebook/*.json` with `nbformat` and `pipeline/*.json`
with `activities`.
