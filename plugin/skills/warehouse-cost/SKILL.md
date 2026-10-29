---
name: warehouse-cost
description: Estimate what a warehouse query or dbt model will cost before it runs, with tq pack data cost estimate (BigQuery dry run, Snowflake EXPLAIN USING JSON, Databricks EXPLAIN COST, Synapse EXPLAIN, Fabric SHOWPLAN_XML, Postgres EXPLAIN), explain partition and cluster pruning for the engine, and propose the cap flags. Use when the user asks "how much will this query cost", "dry run this", "how many bytes does this scan", "is this model expensive", when a bq query is denied by tq/bq-unbounded-query, when tq/warehouse-resize or tq/warehouse-query-unbounded asks, when data/bq-job-max-bytes or data/cost-estimate fails, or when triage raised warehouse_cost. Never raises a cap.
argument-hint: "[--engine <bigquery|snowflake|databricks|postgres|fabric|synapse>] [<file.sql>]"
---

# Warehouse cost

The data pack bounds what an agent runs against a warehouse (spec C §3.6, tq's
`docs/packs/data-cost.md`). You measure a query with its engine's plan-only form,
explain the number, and change the query or the command so it stays under the cap.

Hard rules:

- **Never raise a cap.** The caps live in the manifest (`packs.data.cost.*`), which only
  the human edits. Never add a larger cap to `tq-packs.yaml` either (the repo layer can
  only tighten, and the guard asks before an agent edits it).
- **Never run the query to see what it costs.** Only `tq pack data cost estimate`, which
  refuses DML, DDL, several statements and `ANALYZE`, and sends the plan-only form.
- **Never print, paste or log the query's results or the data.** The estimate prints
  numbers only. Quote the SQL you wrote, never rows.
- **Credentials** come from the engine CLI's own login or from `tq dotenv run`, which
  the human sets up. Never read `.env` files or print the environment.
- A guard deny is final for that command. Change the command (add the cap flag, a
  `LIMIT`, a timeout) instead of looking for another way to run it.

## 1. Which engines and caps apply

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 10"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 10"
$_to tq pack list --enabled --json; echo "packs_exit=$?"
```

Read `packs[name=data].config`: `engines` lists the warehouses whose rules apply, and
`cost.<engine>` holds the caps:

| Engine | Cap keys |
|---|---|
| bigquery | `max_bytes` (bytes billed per query, for example `50GB`) |
| snowflake | `max_warehouse_size` (XSMALL … X6LARGE), `statement_timeout_s`, `max_bytes` (bytes assigned) |
| databricks | `max_workers`, `warehouse_id` (the SQL warehouse the estimate uses), `max_bytes` |
| fabric | `max_capacity_sku` (F2 … F2048) |
| synapse | `max_dwu` (DW100c … DW30000c), `max_spark_nodes` |
| postgres | `max_plan_cost` (planner cost units), `statement_timeout_s` |

No data pack, or the engine is not in `engines`: say so. You can still estimate, but
there is no cap to judge against (verdict `ask`).

## 2. Estimate

Write the SELECT to a file (one statement, no Jinja: for a dbt model, run `dbt compile`
against the dev target and use the compiled SQL from its target path), then:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 90"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 90"
$_to tq pack data cost estimate --engine <engine> --file <query.sql> --json; echo "estimate_exit=$?"
```

- Synapse and Fabric need the endpoint: add `--server <name>.sql.azuresynapse.net` or
  `--server <name>.datawarehouse.fabric.microsoft.com` and `--database <db>`. Postgres
  takes `--server`/`--database` or the CLI's own `PG*` settings. Databricks uses
  `cost.databricks.warehouse_id` or `--warehouse-id`.
- When the CLI needs a token the human keeps in the workspace dotenv, the human runs it
  as `tq dotenv run -- tq pack data cost estimate ...`. Ask for that; do not read the
  dotenv yourself.

| `estimate_exit` | Verdict | What you do |
|---|---|---|
| 0 | `within` | report the number and the cap; go ahead |
| 1 | `over` | do not run it; reduce the scan (section 3) and estimate again |
| 2 | usage, or the query was refused | fix the input: one read-only SELECT, compiled, no `;` inside |
| 3 | the plan-only call failed | the CLI's output is hidden because it may echo the query; run the printed command yourself or ask the human to check the login |
| 4 | `ask`: the estimate is unknown, or the engine has no cap | show the number (or say it is unknown) and ask the human before running it |
| 124 | timeout | report it; do not retry in a loop |

The JSON (`tq.packs.cost/v1`) has `metric`, `value`, `unit`, `cap`, `cap_key`,
`verdict`, `reported` (for example Snowflake partitions assigned and total) and `note`.
It never contains the query text.

## 3. Bring the scan down

Explain the plan in the engine's terms, then propose the smallest change. The details
per engine are in `references/pruning.md`. In short:

- **BigQuery** bills bytes scanned: select only the columns you need (no `SELECT *`),
  filter on the partition column with a constant or a parameter, and on clustering
  columns; prefer a partitioned, clustered intermediate table over re-scanning a raw one.
- **Snowflake** prunes micro-partitions: compare `partitions_assigned` to
  `partitions_total`; filter on the clustering key, avoid functions over the filter
  column, and keep the warehouse at the smallest size that meets the deadline.
- **Databricks** reads files: filter on partition columns, use Z-ordered or liquid
  clustered columns in predicates, and keep table statistics fresh (`ANALYZE TABLE` is
  the human's or the pipeline's job; the estimate refuses it).
- **Synapse dedicated** pays for data movement: `total_cost` and the operation count
  rise with shuffle and broadcast moves; align joins on the distribution column.
- **Fabric warehouse**: the plan's `StatementSubTreeCost` is relative; compare the
  before and after of your change rather than reading it absolutely.
- **Postgres**: `Total Cost` is in planner units; an index on the filter column and a
  narrower select list are the usual fixes.

## 4. Propose the cap flags

Every command an agent runs carries its bound. The guards enforce these:

| Engine | Bound in the command |
|---|---|
| BigQuery (`tq/bq-unbounded-query`, deny) | `bq query --maximum_bytes_billed=<bytes at or below cost.bigquery.max_bytes> ...`; `--dry_run` is exempt |
| Python BigQuery client (`data/bq-job-max-bytes`) | `bigquery.QueryJobConfig(maximum_bytes_billed=<bytes>)` passed as `job_config=`, or a client with `default_query_job_config`; `read_gbq(..., configuration={"query": {"maximumBytesBilled": "<bytes>"}})`; `%%bigquery --maximum_bytes_billed=<bytes>` |
| Snowflake (`tq/warehouse-query-unbounded`, ask) | `ALTER SESSION SET STATEMENT_TIMEOUT_IN_SECONDS=<s>;` in the same `snow sql -q`, or a `LIMIT` |
| Postgres (ask) | `psql -c "SET statement_timeout = '60s'" -c "..."`, `PGOPTIONS='-c statement_timeout=60000'`, or a `LIMIT` |
| Databricks SQL API (ask) | `"wait_timeout": "30s", "on_wait_timeout": "CANCEL"` in the statement body |
| Fabric, Synapse via sqlcmd (ask) | `SELECT TOP <n> ...` |
| Resizes (`tq/warehouse-resize`) | at or below `max_warehouse_size`, `max_workers`, `max_dwu`, `max_spark_nodes`, `max_capacity_sku`; above the cap is denied, at or below asks the human |

If the work truly needs more than the cap, stop and tell the human: the number, the
cap key, and why. Raising it is their manifest edit (then `tq allow`), never yours.

## 5. In a change

- `data/bq-job-max-bytes` fails on a job without a cap: add the cap from section 4 at
  the `file:line`. A justified exception carries an inline
  `# tq:allow <rule>: <reason>` on the line or the line above, and the reviewer sees it.
- `data/cost-estimate` (only with `cost.estimate_in_dod: true`) fails with
  `cost-over-cap`: reduce the model's scan (section 3). `cost-unknown` is a warning:
  report the number in the PR.
- When triage raised `warehouse_cost`, attach the estimate (verdict, metric, value,
  cap) to the PR body, never the query results.
