# Data pack: dbt gates

The data pack (`packs.data`) runs five built-in checks over a dbt project's changed models, plus three external checks that wrap `dbt` and `sqlfluff`. It also adds two guards on the dbt commands an agent runs. This page covers the part from spec C §3.3; [PACKS.md](../PACKS.md) has the framework (config layers, `tq pack check`, the DoD).

## Config: `packs.data.dbt`

```yaml
packs:
  data:
    enabled: true
    tools: [dbt, sqlfluff]        # declared tools become required checks
    timeouts: { dbt: 600s }       # optional override (cap 1800s)
    dbt:
      project: analytics          # dir holding dbt_project.yml; auto-detected when absent
      contract_paths: ["models/marts/**"]
      new_model_tests: [unique, not_null]
      build: off                  # off | dev
      dev_target: dev
      prod_targets: [prod, production]
      state: artifacts/prod       # dir with the production manifest.json (build: dev)
```

| Key | Default | Meaning | `tq-packs.yaml` merge |
|---|---|---|---|
| `project` | auto | The dbt project dir, relative to the repo. When absent, each changed file's nearest `dbt_project.yml` is used. | trusted: the repo file may add it when the manifest lacks it, never change it |
| `contract_paths` | none | Globs for the models whose contract must be enforced. They match relative to the dbt project, or to the repo. | union |
| `new_model_tests` | `[unique, not_null]` | The tests a new model's primary key needs. Only `unique` and `not_null` are allowed. | union |
| `build` | `off` | `dev` turns on `data/dbt-build-modified`. | trusted |
| `dev_target` | `dev` | The target the build check and the guard messages use. | trusted |
| `prod_targets` | `[prod, production]` | The targets agents may never run against. Names compare case-insensitively. | union |
| `state` | none | A dir with the production `manifest.json`, for `state:modified`. | trusted |

Validation rejects any key other than these, a `build` value other than `off` or `dev`, a test name other than `unique` or `not_null`, a `..` in `project` or `state`, and a target name with characters outside `[A-Za-z0-9_.-]`. Every error names its line.

## Checks

The dbt project's `model-paths` come from `dbt_project.yml` (default `models`). A model is a `.sql` or `.py` file under one of them. Every model entry in the project's `.yml` files is read, so a check can find a model's YAML even when that file did not change. The walk is bounded at 5,000 files and 1 MiB per file.

A model's config is resolved the way dbt resolves it: the `dbt_project.yml` folder config (`+materialized`, `+contract`, `+unique_key`, `+incremental_strategy`, under `models.<project>.<dir>...`), then the YAML `config:`, then the model's own `{{ config(...) }}` (or `dbt.config(...)` in a Python model).

### `data/dbt-new-model-tests` (built-in)

It looks at new models (status `A`; with `--all`, every model counts as new).

| Finding | Level |
|---|---|
| `missing-yaml`: the model has no YAML entry | error |
| `missing-primary-key`: no primary key can be identified | error |
| `missing-key-tests`: the key lacks a test from `new_model_tests`, or a composite key has no `dbt_utils.unique_combination_of_columns` | error |
| `missing-description` | warn |

The primary key is, in order:

1. the column with `constraints: [{type: primary_key}]`, or the model-level `primary_key` constraint's columns;
2. the `primaryKey` properties of an ODCS contract (found at `contracts.paths`, default `contracts/**/*.odcs.yaml` and `**/*.odcs.yml`) whose object maps to the model. The mapping is `customProperties` `dbtModel` on the object or the contract, else the object's `physicalName`, else its `name`;
3. the first column named `id`, `<model>_id`, or `<entity>_id`. The entity is the model name without a layer prefix (`stg_`, `base_`, `int_`, `fct_`, `fact_`, `dim_`, `mart_`), as written and in the singular: `stg_orders` accepts `order_id`, `dim_countries` accepts `country_id`.

Tests count from `data_tests:` and the older `tests:`. A test may be written as a name or a mapping, and package-qualified names count. Any of these satisfies the key:

- `dbt_utils.unique_combination_of_columns` at model level;
- a `primary_key` constraint under an enforced contract;
- with an enforced contract, `not_null`/`unique` constraints, which count as the matching tests.

### `data/dbt-contract-enforced` (built-in)

It runs only when `contract_paths` is set. It looks at:

- changed models under `contract_paths`;
- the models of changed YAML files whose model file is under `contract_paths`;
- every contracted model when `dbt_project.yml` changed.

| Finding | Level |
|---|---|
| `contract-not-enforced`: no `contract: {enforced: true}` in YAML, `config()` or `+contract` (a later layer with `enforced: false` wins) | error |
| `enforced-no-columns`: an enforced contract with no YAML columns | error |
| `column-no-data-type`: a column of an enforced model has no `data_type` | error |

### `data/dbt-incremental` (built-in)

It looks at changed incremental models, and the models of changed YAML files.

| Finding | Level |
|---|---|
| `incremental-no-unique-key`: no `unique_key` and no `incremental_strategy: append` | error |
| `incremental-no-lookback`: a `{% if is_incremental() %}` block compares to `max(...)` with no lookback. Any of `interval`, `dateadd`, `date_add`, `timestamp_sub`, `date_sub`, `datetime_sub`, `timestampadd`, `datediff` in the block counts as one. | warn (late rows) |

### `data/sql-select-star` (built-in)

`select *` and `select t.*` in a changed `.sql` model. The level is error under `contract_paths` and warn elsewhere. A staging model (under a `staging/` folder or named `stg_*`) may `select *` straight from `{{ source(...) }}`. `count(*)` and `2 * 3` are not matches.

### `data/sql-hardcoded-relation` (built-in)

A `FROM` or `JOIN` target written as a literal `schema.table` or `db.schema.table` in a changed `.sql` model is an error. Use `ref()` or `source()`. CTE names, subqueries and table functions are not relations.

### `data/dbt-parse` (external `dbt`, timeout 180 s)

It runs `dbt parse --target-path <scratch>/target --log-path <scratch>/logs` in each dbt project dir. The scratch dir is `$TQ_HOME/packs/<ws>/scratch/<run-id>/` and is removed after the run, so `target/` and `logs/` in the tree never change. A non-zero exit fails, with the last 20 lines of output redacted. `dbt parse` needs a profile but opens no warehouse connection.

### `data/dbt-build-modified` (external `dbt`, opt-in `dbt.build: dev`, timeout 1200 s)

It runs `dbt build --select state:modified+ --defer --state <dbt.state> --target <dev_target>` with the same scratch paths. It refuses to run, and fails, in three cases:

- `dev_target` is in `prod_targets`;
- `dbt.state` is unset, or `<dbt.state>/manifest.json` is missing: export the production manifest to `dbt.state`;
- several projects changed and `dbt.project` is not set.

dbt reports breaking changes to the enforced contracts of modified models during this build. This check needs a dev warehouse connection, so it is never on by default.

### `data/sqlfluff` (external `sqlfluff`, timeout 120 s)

It runs `sqlfluff lint --format json --templater jinja [--dialect <d>] <changed .sql files>` from the repo root, in batches of 100 files. Files under `target/`, `dbt_packages/` and `logs/` are skipped.

- **Dialect:** a `.sqlfluff` (repo root or dbt project dir) that sets `dialect` wins. Otherwise tq uses the adapter type of the default target in a `profiles.yml` inside the repo. Only the `type` is read, and a `profiles.yml` outside the repo is never read. After that it uses the first `packs.data.engines` entry. When none of these names a dialect, the check fails and asks for a `.sqlfluff`.
- **Templater:** tq always passes `--templater jinja`. Its dbt builtins (`ref`, `source`, `config`, `is_incremental`, `this`) need no warehouse. The dbt templater is never used.
- **Findings:** each violation is a finding. `warning: true` is a warn, anything else an error. Quoted text in a message is blanked, so a literal from the SQL never appears.
- **Exit codes:** exit 1 means violations. Exit 2 without JSON output fails with the redacted tail.
- **Baseline:** the findings use the pack baseline (`packs.data.baseline`, written by the human with `tq pack baseline --pack data`).

When `dbt` is declared, it must be version 1.8 or later. `data_tests:` and unit tests arrived in 1.8, and an older dbt fails the check as below the minimum.

## Inline suppressions (built-in checks)

A comment `tq:allow <rule>: <reason>` on the finding's line, or the line above it, drops that finding. `<rule>` is the check id (`data/sql-select-star`) or the finding's rule (`select-star`), and the reason is required. SQL takes `-- ...` or `{# ... #}`, YAML takes `# ...`. For a model-level YAML finding, put the comment above the model's `- name:` line. The reviewer judges every suppression.

## Guards

Both guards are active while the data pack is enabled. They read Bash and PowerShell commands: segments split on `;`, `&&`, `|` and newlines, wrappers such as `uv run`, `poetry run` and `python -m dbt`, a full path to `dbt.exe`, and command strings passed to `bash -c`, `pwsh -Command` or `cmd /c`. Text that only mentions dbt (`echo`, `git commit -m`) is not a call.

| Id | Matches | Action |
|---|---|---|
| `tq/dbt-prod-target` | `dbt build\|run\|seed\|snapshot\|run-operation\|test\|clone\|retry` whose target is in `dbt.prod_targets`. The target is `--target`/`-t`, or `DBT_TARGET` set in the same command. With neither, it is the default target of the profile in a `profiles.yml` inside the repo (`--profiles-dir`, else the dbt project dir, else the repo root). | deny |
| `tq/dbt-full-refresh` | `--full-refresh`/`-f` (or `DBT_FULL_REFRESH=true`) on `dbt build\|run\|seed`, on any target | ask: "drops and rebuilds incremental tables" |

- **Default target:** the guard reads only `profiles.yml` and `dbt_project.yml` (`profile:`), through the bounded `ReadLocal`, and never `~/.dbt`.
  - A refused read fails closed (deny, "pass --target"). That covers a `--profiles-dir` outside the workspace or a `.env` path, a file over 1 MiB, or a blown budget.
  - With no readable `profiles.yml` and no `--target`, the call is allowed, because the guard cannot see `~/.dbt`. Keep the default target of every profile on dev.
- **Extra subcommands:** `clone` and `retry` go beyond the spec's list. `clone` creates relations in the target, and `retry` replays the last invocation.
- **Hook layers:** `guard.disable` and `guard.actions` apply as for every guard. Each guard runs in well under the 50 ms budget on a 64 KiB command, with no process or network.
- **Counters:** `dbt_prod_target:denied` and `dbt_full_refresh:asked` appear in `tq insights` under the data pack.

## Doctor

`data-dbt-state-missing` (warn): `dbt.build` is `dev` and `dbt.state` is unset, or has no `manifest.json`.

## Template

`templates/dbt-contract-starter/` is a small project that passes every dbt gate. It has:

- staging and mart models on synthetic `acme` data;
- an incremental mart with an enforced contract and an ODCS v3.0.2 contract;
- key tests and a dbt unit test;
- `.sqlfluff` and `tq-packs.yaml`.

The Go test `TestTemplateDBTContractStarter` runs the built-in checks over it.
