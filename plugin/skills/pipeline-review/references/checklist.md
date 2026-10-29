# Pipeline review checklist (answer-key rows)

Judge each row from the diff and the files it touches. Cite file:line. The lint
column names the `tq pack check` rule that already looks for part of the row; the
reviewer judges the rest.

| # | Row | Question | Lint |
|---|---|---|---|
| D1 | Idempotency | Does a rerun of the same interval give the same result: MERGE or upsert on a key, partition overwrite, or delete-insert in one transaction? | `data/pipeline-idempotency` (`append-no-dedupe`) |
| D2 | Backfill | Is the job parameterised by interval (logical date), free of wall-clock time, safe to run for past intervals in any order, and is the backfill command documented? | `data/pipeline-backfill` (`wall-clock-filter`), `data/airflow-dag` (`airflow-dynamic-start-date`) |
| D3 | Late and out-of-order data | Is there a lookback or watermark, and a stated lateness bound, so a late row is merged and not dropped or duplicated? | `data/dbt-incremental` (lookback warn) |
| D4 | Schema evolution | Does the change follow the contract version rule? Are downstream models and consumers listed? Is `on_schema_change` set for incremental models? | `data/contract-breaking` |
| D5 | Personal data into outputs | Does any `pii`/`sensitive`/`financial`/`children` column (manifest `data.paths`, contract classification) reach an output, export, log or metric without masking? Is the output classified and does `docs/privacy.md` say why? | `data/pipeline-pii-output` (`pii-to-output`), `data/notebook-outputs` |
| D6 | Tests | Do new models have key tests, do changed logic paths have dbt unit tests or pytest, and are the fixtures synthetic (`tester`, `acme`)? | `data/dbt-new-model-tests` |
| D7 | Cost | Partition and cluster pruning, no full scans in incremental paths, warehouse or pool sizing within `packs.data.cost`, and an estimate (`tq pack data cost estimate`) for heavy queries | `data/bq-job-max-bytes`, `data/cost-estimate` |
| D8 | Failure handling | Retries with backoff, timeouts, side effects (emails, API calls) exactly once or guarded, alerts on failure | `data/airflow-dag` (`airflow-no-retries`), `data/fabric-artifacts` (`fabric-copy-no-retry`) |
| D9 | Observability | Row counts, freshness, and a recorded run id per interval | — |
| D10 | Secrets and environments | Connections from the orchestrator, Key Vault or env; no literal endpoints or workspace GUIDs; dev and prod split by parameter | `data/synapse-artifacts` (`synapse-inline-secret`, `synapse-hardcoded-pool`), `data/fabric-artifacts` (`fabric-literal-onelake-path`, `fabric-pinned-lakehouse`) |
| D11 | Orchestrator specifics | Airflow (static `start_date`, explicit `catchup`, no top-level work), Dagster (partitions and asset checks), Prefect (retries, cache keys), Fabric and Synapse (parameter cells, pipeline retry policy, no pinned lakehouse) | `data/airflow-dag`, `data/dagster-validate`, `data/fabric-artifacts` (`fabric-no-parameters-cell`) |

## Severity

| Severity | When |
|---|---|
| BLOCKER | a rerun or backfill duplicates, drops or reorders rows (D1, D2, D3); personal data reaches an unclassified output, an export or a log unmasked (D5) |
| MAJOR | a breaking contract change without a major version bump or consumer migration (D4); wall-clock time in a filter (D2); a new model without key tests (D6); a heavy query with no cost bound (D7); a side effect that can repeat on retry (D8); a literal secret, endpoint or workspace GUID (D10) |
| MINOR | missing run-id logging or freshness check (D9); naming and style |

## Rows that need a human (U rows)

- The real lateness bound of a source (D3), when no doc or contract states it.
- Whether a consumer tolerates a reorder or a short outage during a backfill (D2, D4).
- Whether an output may legally hold a classified column (D5): that is the DPO's call, through `docs/privacy.md`.
