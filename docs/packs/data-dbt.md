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
| `incremental-no-unique-key`: no `unique_key` and no strategy that needs none: `append`, `microbatch`, or `insert_overwrite` with `partition_by`. Any `unique_key` value counts (a literal, a list, `var('k')`, a `{% set %}` name); only `none` is no key | error |
| `incremental-no-lookback`: a `{% if is_incremental() %}` block compares to `max(...)` with no lookback. Any of `interval`, `dateadd`, `date_add`, `timestamp_sub`, `date_sub`, `datetime_sub`, `timestampadd`, `datediff` in the block counts as one. | warn (late rows) |

### `data/sql-select-star` (built-in)

`select *` and `select t.*` in a changed `.sql` model. The level is error under `contract_paths` and warn elsewhere. A staging model (under a `staging/` folder or named `stg_*`) may `select *` straight from `{{ source(...) }}`. `count(*)` and `2 * 3` are not matches.

### `data/sql-hardcoded-relation` (built-in)

A `FROM` or `JOIN` target written as a literal `schema.table` or `db.schema.table` in a changed `.sql` model is an error. Use `ref()` or `source()`. Targets are read at any depth: CTE bodies, derived tables, and `IN (...)`/`EXISTS (...)` subqueries. A Jinja expression followed by `.name` (`{{ target.schema }}.orders`, `{{ var('db') }}.raw.orders`) is a literal too; only `ref()`, `source()` and `this` resolve through the DAG. CTE names, aliases, table functions and the `FROM` inside `EXTRACT(... FROM ...)` or `SUBSTRING(... FROM ...)` are not relations.

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

Both guards are active while the data pack is enabled. They read Bash and PowerShell commands, split into segments on `;`, `&`, `&&`, `|`, newlines, parentheses and braces.

- **What is a dbt call:** a segment is a dbt call when any word in it runs `dbt` (a bare `dbt`, a path to `dbt` or `dbt.exe`, or `dbt.cli.main` after `python -m`). So `timeout 600 dbt run`, `uv run dbt`, `cmd /c dbt run`, `pwsh -c dbt run`, `nice`/`xargs`/`then`/`!` before dbt, `docker compose run dbt dbt run` and `Start-Process dbt -ArgumentList '...'` are all calls. The exception is a segment whose command only prints or searches text (`echo`, `printf`, `Write-Host`, `Write-Output`, `git`, `gh`, `grep`, `rg`, `Select-String`, `cat`, `ls` and similar), so `echo "dbt run --target prod"` is not a call.
- **Nested command lines:** a quoted string with spaces that mentions dbt (`bash -c "..."`, `pwsh -Command '...'`, `ssh host "..."`) is read as a command line too. Variables set earlier in the outer command (`DBT_TARGET=prod bash -c 'dbt run'`, `export DBT_FULL_REFRESH=true; pwsh -Command 'dbt build'`) carry into it.
- **The subcommand:** it is the first positional word that is a dbt subcommand. The values of the options that take one (`--log-level debug`, `--log-format`, `--select`, `--vars`, `--state`, `--threads` and the rest) are skipped. To fail closed, a call also counts as writing when any positional word is a writing subcommand.

| Id | Matches | Action |
|---|---|---|
| `tq/dbt-prod-target` | `dbt build\|run\|seed\|snapshot\|run-operation\|test\|clone\|retry` whose target is in `dbt.prod_targets`, or cannot be known. The target is `--target`/`-t`/`-t<value>`/`--target=<value>`, or `DBT_TARGET` set in the same command. With neither, it is the default target of the profile in a `profiles.yml` inside the repo (`--profiles-dir`, else the dbt project dir, else the repo root). | deny |
| `tq/dbt-full-refresh` | `--full-refresh`/`-f` (or `DBT_FULL_REFRESH=1\|true\|yes` set in the command) on any subcommand that can rebuild a relation (`build`, `run`, `seed`, `clone`, ...; not `compile`, `ls`, `docs` and the other read-only ones), on any target | ask: "drops and rebuilds incremental tables" |

- **Variables:** `$NAME`, `${NAME}`, `$env:NAME` and `%NAME%` in the target are replaced by a value set earlier in the same command (`T=prod; dbt run --target $T` is denied). A value the shell still computes when it runs (an unset variable, `$(...)`, a backtick) is denied with "pass a literal --target".
- **Default target:** the guard reads only `profiles.yml` and `dbt_project.yml` (`profile:`), through the bounded `ReadLocal`, and only inside the repository. A `profiles.yml` outside the repo, `~/.dbt` included, is never read.
  - A `--profiles-dir` or `--project-dir` outside the repository (an absolute path outside it, `~`, `$HOME`, `$env:USERPROFILE`, `%USERPROFILE%`, a config dir) on a writing call with no `--target` is denied ("outside the repository ... pass --target dev"). With an explicit `--target` the directory does not matter.
  - A refused read fails closed too (deny, "pass --target"): a `.env` path, a file over 1 MiB, or a blown budget.
  - With no `--profiles-dir`, no readable `profiles.yml` in the repo and no `--target`, the call is allowed, because dbt then uses `~/.dbt`, which the guard does not read. Keep the default target of every profile on dev.
- **Not covered:** `dbt retry` replays the previous invocation, including a `--full-refresh` it had. The full-refresh guard cannot see that and does not ask; the prod-target guard still checks the retry's own target. A dbt call hidden behind a script file, an alias or a shell function is not seen either.
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
