# data: warehouse cost

The cost guardrail of the data pack (tq's `docs/packs/data-cost.md`): guards
`tq/bq-unbounded-query`, `tq/warehouse-resize`, `tq/warehouse-query-unbounded`, checks
`data/bq-job-max-bytes` and `data/cost-estimate`, triage flag `warehouse_cost`, and the
`warehouse-cost` skill.

## kickoff

Only when the stack runs queries on a warehouse: BigQuery, Snowflake, Databricks SQL,
Microsoft Fabric or Synapse, or Postgres used as a warehouse (dbt `profiles` adapter,
`google-cloud-bigquery`, `snowflake-connector-python`, `databricks-sql-connector`,
`.Warehouse`/`sqlPool` artifacts, or the user names one). Otherwise skip this section.

- Propose, never assume, the `packs.data` keys for it (headless: in
  `docs/privacy-manifest-proposal.yaml`, as for the rest of the proposal):

  ```yaml
  packs:
    data:
      engines: [bigquery]              # only the engines the project uses
      cost:
        bigquery: { max_bytes: 50GB }  # one cap per engine in engines
        estimate_in_dod: false         # true only with a dev connection in the DoD
  ```

  Cap keys per engine: bigquery `max_bytes`; snowflake `max_warehouse_size`,
  `statement_timeout_s`, `max_bytes`; databricks `max_workers`, `warehouse_id`,
  `max_bytes`; fabric `max_capacity_sku`; synapse `max_dwu`, `max_spark_nodes`;
  postgres `max_plan_cost`, `statement_timeout_s`. The human picks the numbers.
- The stack ADR for the warehouse records the engine, the caps chosen and why (budget,
  data volume), and whether `estimate_in_dod` is on.

## feature

- When the slice adds or changes warehouse queries, models or compute sizing, the
  spec slice gains **Cost**: the engine, the cap key that bounds it, and the estimate
  verdict for the main new query (`warehouse-cost` skill, `tq pack data cost estimate`).
  A model whose estimate is `over` is redesigned in the slice, not shipped.
- Acceptance checks for a BigQuery job in code name the cap it sets
  (`maximum_bytes_billed`), which `data/bq-job-max-bytes` enforces.
- Such a change raises `warehouse_cost` (a changed `packs.data.cost`, a model's
  materialization changed to `table`, or a pool or cluster sizing file); its reviewers
  come from `tq pack crew --flags warehouse_cost`.

## fix

- A cost bug (a query that scans too much, a missing cap) gets a regression case at
  the lowest layer: a lint fixture that `data/bq-job-max-bytes` flags without the fix,
  or a unit test over the job config. Never a test that queries the warehouse.
- The fix never raises a cap. If the right fix is a larger cap, stop and hand the
  number and the cap key to the human.

## dod

| Item | Meaning | Who fixes it |
|---|---|---|
| `pack:data/bq-job-max-bytes` | a BigQuery job in a changed file has no byte cap, or one above `cost.bigquery.max_bytes` | the agent adds the cap at the `file:line`; an exception carries `# tq:allow <rule>: <reason>` and the reviewer judges it |
| `pack:data/cost-estimate` (only with `estimate_in_dod: true`) | `cost-over-cap`: a changed dbt model's plan-only estimate is above the cap; `cost-estimate-failed`: dbt compile or the engine CLI failed | the agent reduces the scan; a failed login or a missing CLI is the human's (it runs under `tq dotenv run`) |
| `pack:data/cost-estimate` skip | estimate_in_dod is off, or no changed model | nobody |

## ship

- When triage raised `warehouse_cost`, the PR body has a **Cost** line per main query
  or model: engine, metric and value, cap and cap key, verdict, from
  `tq pack data cost estimate --json`. Never query results, never the query's data.
- A change to `packs.data.cost` in `tq-packs.yaml` is called out in the PR body (it can
  only tighten; the human approved the edit when the guard asked).
