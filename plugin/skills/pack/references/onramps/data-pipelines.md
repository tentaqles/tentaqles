# data: pipelines

Python pipelines (Airflow, Dagster, Prefect, pandas, polars, PySpark), Microsoft
Fabric and Azure Synapse items, notebooks, and the `pipeline-reviewer`. The checks
are `data/airflow-dag`, `data/pipeline-backfill`, `data/pipeline-idempotency`,
`data/pipeline-pii-output`, `data/fabric-artifacts`, `data/synapse-artifacts`,
`data/notebook-outputs` and `data/dagster-validate`; tq's
`docs/packs/data-pipelines.md` explains each rule.

## build

- A checkpoint that changes a DAG, job, notebook, Fabric or Synapse item raises the
  flag `pipeline`; add the reviewers `tq pack crew --flags <the flags triage
  printed>` prints (it prints `pipeline-reviewer` for `pipeline`, `data_contract`
  and `warehouse_cost`). The reviewer runs through the `pipeline-review` skill.
- Tests for a transform run on a synthetic frame (pytest, people named `tester`,
  company `acme`) or a dbt unit test. A checkpoint never runs the pipeline itself:
  `tq/pipeline-run` asks before `airflow dags trigger|backfill`, `airflow backfill
  create`, `dagster job launch|execute`, `dagster asset materialize`, `dg launch`,
  `prefect deployment run` and `az datafactory|synapse pipeline create-run`, and a
  backfill waits for the human.

## feature

- The spec slice gains **Outputs and classes**: each table, file or export the
  feature writes, its write mode (MERGE on a key, partition overwrite,
  delete-insert, append) and the data classes of the columns it carries (from the
  manifest `data.paths` and the contracts).
- When the feature changes a schedule or a backfill path, the slice states the
  interval parameter (logical date, partition key) and the backfill command.
- The expected crew adds what `tq pack crew --flags pipeline` prints.
- Acceptance checks name the test that proves a rerun of the same interval gives
  the same rows (D1), and one per personal-data column that must stay masked (D5).

## fix

- The regression test sits at the lowest data layer: a pytest over a synthetic
  frame for a transform, a dbt singular or unit test for a model, a fixture
  notebook or pipeline JSON for a Fabric or Synapse rule.
- A duplicate-rows or missing-rows bug gets a test that runs the job twice for the
  same interval, and once for a past interval, and compares the output.
- A fix never adds a `tq:allow <rule>: <reason>` comment to silence a pipeline lint
  without the human's agreement on the reason.

## kickoff

Only when the stack has Airflow, Dagster or Prefect code, PySpark, pandas or polars
batch jobs, or Microsoft Fabric (`*.Notebook/`, `*.DataPipeline/` with `.platform`)
or Azure Synapse (`notebook/`, `pipeline/`, `linkedService/` JSON) items in git. The
data pack may be off: this is a proposal for the human, never a manifest edit.

- Propose, in the `packs.data` block (headless: in
  `docs/privacy-manifest-proposal.yaml`), `pipelines.paths` for the folders that hold
  the jobs (the default is `dags/**`, `pipelines/**`, `**/*.Notebook/**`,
  `notebook/**`, `pipeline/**`), and `dagster` in `tools` when the project uses
  Dagster.
- If the project has a masking macro, propose it under `pipelines.masking`; only the
  manifest can set it.
- Add to the ADR the write mode of each output (MERGE, partition overwrite,
  delete-insert) and how backfills run (command, interval parameter, order).
- Classify the tables the jobs read and write in the manifest `data.paths`, so
  `data/pipeline-pii-output` can follow personal data to the outputs.

## dod

| Item | Who fixes it |
|---|---|
| `pack:data/airflow-dag` | the implementer: a fixed `start_date` (error); catchup, retries and top-level I/O are warnings to fix or explain |
| `pack:data/pipeline-backfill` | the implementer: take the interval from the logical date; an error only in files under `dbt.contract_paths` |
| `pack:data/pipeline-idempotency` | the implementer (warning): MERGE, partition overwrite or delete-insert, or the reviewer's D1 judgement |
| `pack:data/pipeline-pii-output` | the implementer masks the column; classifying the output is the human's manifest and `docs/privacy.md` change |
| `pack:data/fabric-artifacts` | the implementer: no literal OneLake path with a workspace GUID (error); parameters cell, pinned lakehouse and copy retry are warnings |
| `pack:data/synapse-artifacts` | the implementer: move inline passwords and keys to Azure Key Vault references (error); parameterise pool names |
| `pack:data/notebook-outputs` | the implementer clears the outputs; an output that hits the redaction detectors is an error |
| `pack:data/dagster-validate` | the implementer fixes the definitions; declared but not installed: the human installs dagster or removes it from `tools` |

## ship

- The PR body quotes the data pack results summary (`tq pack check --pack data`), and
  says "pipeline-reviewed" only with a recorded `pipeline-reviewer` PASS for the
  current code.
- When the change alters a write mode, a schedule or a backfill path, the PR body
  names the backfill command and whether a backfill is needed after the merge.
