# Data pack: warehouse cost guardrail

Spec C §3.6. Two parts: **guards** bound what an agent runs against a warehouse, and
**`tq pack data cost estimate`** measures a query before it runs. Two checks carry the
same rules into a change: `data/bq-job-max-bytes` (built-in) and `data/cost-estimate`
(external, opt-in). The `warehouse-cost` skill drives all of it.

## Config

```yaml
packs:
  data:
    engines: [bigquery, snowflake, databricks, postgres, fabric, synapse]   # rules apply per listed engine
    cost:
      bigquery:   { max_bytes: 50GB }
      snowflake:  { max_warehouse_size: MEDIUM, statement_timeout_s: 900, max_bytes: 200GB }
      databricks: { max_workers: 8, warehouse_id: 0123abcd, max_bytes: 100GB }   # an id, not a secret
      fabric:     { max_capacity_sku: F8 }
      synapse:    { max_dwu: DW500c, max_spark_nodes: 10 }
      postgres:   { max_plan_cost: 10000000, statement_timeout_s: 300 }
      estimate_in_dod: false
```

| Key | Value | `tq-packs.yaml` merge |
|---|---|---|
| `engines` | list of `bigquery`, `snowflake`, `databricks`, `postgres`, `fabric`, `synapse` | union |
| `cost.<engine>.max_bytes` | byte size: `50GB` (decimal), `512MiB` (binary) or a byte count | minimum |
| `cost.<engine>.statement_timeout_s` | positive integer | minimum |
| `cost.databricks.max_workers`, `cost.synapse.max_spark_nodes` | positive integer | minimum |
| `cost.postgres.max_plan_cost` | positive number (planner cost units) | minimum |
| `cost.snowflake.max_warehouse_size` | `XSMALL` … `X6LARGE` (`X-LARGE`, `2X-LARGE` … accepted) | trusted: the repo may add it, not change it |
| `cost.synapse.max_dwu` | `DW100c` … `DW30000c` | trusted |
| `cost.fabric.max_capacity_sku` | `F2` … `F2048` | trusted |
| `cost.databricks.warehouse_id` | `[A-Za-z0-9_-]{1,64}` | trusted |
| `cost.estimate_in_dod` | `true` / `false` | OR (the repo may switch it on) |

An unknown engine, cost key or a value that does not parse is a manifest error. The
ordered scales (Snowflake sizes, DWU, Fabric SKUs) are not numbers to the framework's
merge, so a repo cannot tighten them; tighten them in the manifest.

`tq doctor` warns **`data-cost-cap-missing`** for an engine in `engines` with no cap
key in `cost.<engine>`.

## Guards

Active when the data pack is enabled and the engine is listed in `engines`. They are
pure functions over the command (no process, no file read, under 50 ms on a 64 KiB
command); a panic is a deny. They honour `guard.disable` and `guard.actions`.

| Id | Engine | Matches | Action |
|---|---|---|---|
| `tq/bq-unbounded-query` | BigQuery | `bq query` without `--maximum_bytes_billed`, with a non-literal value, or above `cost.bigquery.max_bytes`. `--dry_run` is exempt (`--dry_run=false` and `--nodry_run` are not). Global flags before `query`, `tq dotenv run -- bq …` and `bq.cmd` paths are recognised. | deny, naming the flag to add |
| `tq/warehouse-resize` | Snowflake | `ALTER`/`CREATE [OR REPLACE] WAREHOUSE … WAREHOUSE_SIZE` in `snow sql -q`/`snowsql -q`, or in any MCP call's input | deny above `max_warehouse_size`; ask at or below, for an unknown size, or with no cap |
| | Databricks | `databricks clusters create\|edit\|resize`: `--num-workers`, inline `--json` `num_workers` / `autoscale.max_workers` | deny above `max_workers`; ask otherwise, and on `--json @file` |
| | Databricks | `databricks warehouses create\|edit` | ask (SQL warehouses size by cluster size and count, which have no cap key) |
| | Synapse | `az synapse sql pool create\|update\|restore --performance-level`; `az synapse spark pool create\|update --node-count/--max-node-count` | deny above `max_dwu` / `max_spark_nodes`; ask otherwise |
| | Fabric | `az fabric capacity create\|update --sku` (shorthand `{name:F64,tier:Fabric}`, or `--set sku.name=…`) | deny above `max_capacity_sku`; ask otherwise and on `--sku @file` |
| `tq/warehouse-query-unbounded` | Snowflake | `snow sql -q`/`snowsql -q` with a SELECT and no `LIMIT`, not preceded in the same call by `ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=<n>` with `n` above 0 | ask |
| | Postgres | `psql -c` with a SELECT and no `LIMIT`, not preceded in the same call by `SET statement_timeout = <n>` / `TO <n>` with `n` above 0, and without a non-zero `PGOPTIONS='-c statement_timeout=…'` on the call or exported earlier (`export`, `$env:`, `set`) | ask |
| | Databricks | `databricks api post …/sql/statements` whose body lacks `wait_timeout` (not `0s`) with `on_wait_timeout: CANCEL`, or is a file | ask |
| | Fabric, Synapse | `sqlcmd` against `*.datawarehouse.fabric.microsoft.com` / `*.sql.azuresynapse.net` with a SELECT and no `TOP` | ask |

The guards read through nested shells (`bash|sh|zsh -c`, `-lc`, `pwsh -Command`,
`pwsh -EncodedCommand`, `cmd /c`, `eval`, `Invoke-Expression`, `wsl … sh -c`,
`xargs … sh -c`) up to four levels deep; a deeper or undecodable nest that names the
tool asks. Global flags before the subcommand (`databricks -p prod clusters …`,
`az --subscription s synapse …`, `snow --connection c sql …`) are skipped. An MCP
call's input is decoded as JSON first, so a `\n` in a multi-line statement does not
hide `WAREHOUSE_SIZE`; a `WAREHOUSE_SIZE` the guard cannot read asks.

A size compares on the engine's own ordered scale. The guards cannot see queries in
scripts, `-f` files or `.bigqueryrc` defaults: the lint and the reviewer cover those.
dbt targets and pipeline runs are guarded by `tq/dbt-prod-target`, `tq/dbt-full-refresh`
and `tq/pipeline-run` (docs/packs/data-dbt.md, data-pipelines.md).

## `tq pack data cost estimate`

```
tq pack data cost estimate --engine <e> (--file q.sql | --stdin) [--json]
                           [--server <host>] [--database <db>] [--warehouse-id <id>]
```

It reads one SELECT (or `WITH … SELECT`) of at most 1 MiB and refuses: DML, DDL,
`INTO`, `CALL`/`EXEC`, `EXPLAIN`/`ANALYZE`, several statements (any `;` inside),
Jinja (compile first), backslashes (quoting differs between engines), and sqlcmd
commands or variables (`:…`, `!!`, `GO`, `$(…)`). It then runs only the plan-only form
through the engine's CLI, with the identity's environment (wrap it in `tq dotenv run`
when the CLI needs a token), with a 60 s timeout:

| Engine | Plan-only call | Number compared |
|---|---|---|
| BigQuery | `bq query --dry_run --use_legacy_sql=false --format=json` (query on stdin) | `totalBytesProcessed` vs `cost.bigquery.max_bytes` |
| Snowflake | `snow sql --stdin --format json` with `EXPLAIN USING JSON …` | `GlobalStats.bytesAssigned` vs `cost.snowflake.max_bytes`; partitions assigned/total reported |
| Databricks SQL | `databricks api post /api/2.0/sql/statements` with `EXPLAIN COST …`, `wait_timeout 50s`, `on_wait_timeout CANCEL` on `warehouse_id` | the largest `sizeInBytes` of the optimized logical plan vs `cost.databricks.max_bytes` |
| Synapse dedicated | `sqlcmd -G -b -x -X1 -y 0 -i <file>` with `EXPLAIN …` | `total_cost` and `total_number_operations` reported |
| Fabric warehouse | `sqlcmd -G -b -x -X1 -y 0 -i <file>` with `SET SHOWPLAN_XML ON` then the query | `StatementSubTreeCost` reported |
| Postgres | `psql -X -q -A -t -v ON_ERROR_STOP=1 -c "EXPLAIN (FORMAT JSON) …"` | `Total Cost` vs `cost.postgres.max_plan_cost` |

Output: text, or `--json` schema `tq.packs.cost/v1`:
`{schema, engine, tool, metric, value, unit, cap, cap_key, cap_value, verdict, reported, note}`.
`verdict` is `within`, `over`, or `ask` (the value is unknown, or the engine has no
cap). The query text and the CLI's own output (which may echo it) are never printed;
on a failure the command prints the plan-only call with the query replaced by a
placeholder, for the human to run.

| Exit | Meaning |
|---|---|
| 0 | within the cap |
| 1 | over the cap |
| 2 | usage or config error, or the query was refused |
| 3 | the plan-only call failed (CLI missing, login, timeout, non-zero exit) |
| 4 | ask: unknown estimate or no cap |

Each run adds a counter `cost_estimate:<verdict>` to the pack events.

## Checks

### `data/bq-job-max-bytes` (built-in, required)

Applies when `bigquery` is in `engines` and the change touches shell scripts (`.sh`,
`.ps1`, `.cmd`, Makefiles), YAML (CI workflows), Python or notebooks (`.ipynb`, Fabric
`notebook-content.py`, Synapse notebook JSON). Findings carry `file:line` and the rule,
never the query.

| Rule | Level | Finding |
|---|---|---|
| `bq-cli-no-cap` | error | `bq query` without `--maximum_bytes_billed` (also in Python string lists such as `subprocess.run(["bq", "query", …])`) |
| `bq-cap-above-max` | error | a literal cap above `cost.bigquery.max_bytes` |
| `bq-job-config-no-cap` | error | `QueryJobConfig(...)` without `maximum_bytes_billed`, never set on the variable later |
| `bq-query-no-cap` | error | `<client>.query(sql)` or `<client>.query_and_wait(sql)` with no `job_config`, on a `bigquery.Client` made without `default_query_job_config` (also chained: `bigquery.Client().query(sql)`) |
| `bq-read-gbq-no-cap` | error | `read_gbq(...)` without `configuration={"query": {"maximumBytesBilled": …}}` |
| `bq-operator-no-cap` | error | `BigQueryInsertJobOperator(...)` without `maximumBytesBilled` |
| `bq-magic-no-cap` | error | a `%%bigquery` cell without `--maximum_bytes_billed`, in a notebook that does not set `magics.context.default_query_job_config.maximum_bytes_billed` |
| `bq-config-unchecked` | warn | a `configuration` passed by name, which the lint cannot see |
| `bq-unreadable` | warn | a file over 2 MiB or a notebook that does not parse (a known bound: those files are not checked) |
| `bq-unreadable` | error | a changed file that cannot be read for any other reason |

Python rules apply to files that import `google.cloud.bigquery`, `pandas_gbq` or the
Airflow Google provider. Suppress one finding with a comment on its line or the line
above: `# tq:allow <rule>: <reason>` (the reason is required). At most 2,000 files are
read per run.

### `data/cost-estimate` (external, tool `dbt`, opt-in)

Runs only with `cost.estimate_in_dod: true` and a changed `.sql` file. It:

1. finds the dbt project (`packs.data.dbt.project`, or `dbt_project.yml` at the repo
   root) and the changed models under its `model-paths`, at most 20;
2. refuses when `dbt.dev_target` (default `dev`) is in `dbt.prod_targets`;
3. runs `dbt compile --target <dev> --target-path <scratch> --log-path <scratch>
   --select path:<model>…` in `$TQ_HOME/packs/<ws>/scratch/<run-id>/`, removed after;
4. reads `manifest.json` (`metadata.adapter_type`, each model's `compiled_code`) and
   runs the estimate above for each model on that engine.

| Rule | Level |
|---|---|
| `cost-over-cap` | error |
| `cost-estimate-failed` (CLI missing, login, timeout) | error |
| `cost-unknown` (no cap, or no number) | warn |
| `cost-not-compiled`, `cost-not-estimable` | warn |

It needs a dev connection: run the DoD under `tq dotenv run`. It is never on by default.
Its required bit follows the framework: required when `dbt` is in `packs.data.tools`.

## Triage flag `warehouse_cost`

Raised when `packs.data.cost` changes in `tq-packs.yaml`, a changed model or
`dbt_project.yml` now sets `materialized: table` (it did not before), or a compute
sizing file changes (`databricks.yml`, Synapse `bigDataPool/*.json`, `sqlPool/*.json`).
Jev question while the pack is enabled: "Does this change add or alter warehouse
queries, models or compute sizing so that it could scan much more data or cost more to
run?". Reviewer: `pipeline-reviewer`.

## Not covered

- Queries inside scripts the guards cannot see (`-f` files, `.bigqueryrc` defaults,
  generated SQL): the lint and the reviewer.
- Spark jobs are bounded by cluster and pool size, not estimated.
- The per-engine plan-only calls and output fields were checked against the vendors'
  documentation; the human's manual run per engine against a dev account confirms them
  (spec C §9, "Manual").
