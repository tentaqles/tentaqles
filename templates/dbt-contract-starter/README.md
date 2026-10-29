# dbt-contract-starter

A small dbt project that passes the tq data pack's dbt gates. Copy it into a client repo as a starting point.

It runs on synthetic `acme` orders: customers are named `tester`, with `@acme.test` emails. It contains:

- `models/staging/`: `stg_customers` and `stg_orders` (views), with key tests on both.
- `models/marts/fct_orders.sql`: an incremental mart with a `unique_key` and a 3-day lookback. Its contract is enforced: every column has a `data_type`, and `order_id` has a `primary_key` constraint plus `unique` and `not_null` tests. A dbt unit test checks it with `is_incremental` overridden to false.
- `contracts/fct_orders.odcs.yaml`: an ODCS v3.0.2 data contract for the mart. `customProperties` `dbtModel: fct_orders` links the contract to the model.
- `dbt_tests/`: one singular data test. `tests/` holds this template's own pytest suite.
- `.sqlfluff`: the Postgres dialect with the jinja templater, which needs no warehouse.
- `tq-packs.yaml`: the repo layer of `packs.data`. It sets `contract_paths` to the marts and declares `dbt` and `sqlfluff`.
- `profiles.example.yml`: a dev profile that reads credentials from the environment.

## Use it

1. Copy the folder into the repo and rename `acme_orders` in `dbt_project.yml` and the profile.
2. Copy `profiles.example.yml` to `~/.dbt/profiles.yml`. Never commit a `profiles.yml` with values. Run dbt as `tq dotenv run -- dbt ...`, so credentials come from the environment.
3. Run `tq pack check --pack data`. Before any commit, the dbt gates must report no error findings.

## What the gates hold it to

| Check | What the template does |
|---|---|
| `data/dbt-new-model-tests` | Every model has a YAML entry and a description. Each primary key has `unique` and `not_null` tests. |
| `data/dbt-contract-enforced` | Models under `models/marts/**` enforce their contract (`+contract` in `dbt_project.yml` and in `_marts.yml`). Every column has a `data_type`. |
| `data/dbt-incremental` | `fct_orders` has `unique_key='order_id'` and a `- interval '3 days'` lookback. |
| `data/sql-select-star`, `data/sql-hardcoded-relation` | Columns are listed explicitly. Every relation comes through `ref()`. |
| `data/dbt-parse`, `data/sqlfluff` | They need `dbt` (1.8 or later) and `sqlfluff` installed. Both are declared in `tq-packs.yaml`, so a missing tool fails. |

Agents may not run dbt against `prod` or `production` (`tq/dbt-prod-target`). A `--full-refresh` asks first (`tq/dbt-full-refresh`). See `docs/packs/data-dbt.md`.

## Tests

```powershell
python -m pytest templates/dbt-contract-starter/tests
```

The tests use only the standard library: no dbt, no tq, no warehouse. The Go test `TestTemplateDBTContractStarter` in `internal/packs/data` runs the real built-in checks over this folder.
