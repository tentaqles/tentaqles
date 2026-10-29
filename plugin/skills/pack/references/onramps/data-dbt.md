# data: dbt

The dbt gates of the data pack: `data/dbt-new-model-tests`, `data/dbt-contract-enforced`,
`data/dbt-incremental`, `data/sql-select-star`, `data/sql-hardcoded-relation` (built-in),
`data/dbt-parse`, `data/dbt-build-modified` and `data/sqlfluff` (external). The guards are
`tq/dbt-prod-target` (deny) and `tq/dbt-full-refresh` (ask). Full reference:
`docs/packs/data-dbt.md`.

## build

- Each checkpoint that adds a model adds its YAML entry in the same checkpoint: a description,
  its columns, and `unique` + `not_null` on the primary key (or
  `dbt_utils.unique_combination_of_columns` for a composite key). Write the YAML entry and the
  key tests first, as the failing test, then the model.
- A model under `packs.data.dbt.contract_paths` enforces its contract. Give every column a
  `data_type` before the implementer writes the SQL.
- Run dbt only against `dev_target` (`--target dev` unless the manifest says otherwise). Never
  pass a production target, and never add `--full-refresh` to make a checkpoint pass. The guards
  deny or ask; ask the human instead.
- Point dbt's artifacts away from the tree when you run it yourself
  (`--target-path` and `--log-path` under the scratch dir), so the DoD's `stable_tree` stays
  valid. `tq pack check` already does this for its own dbt calls.

## feature

- The spec slice's **Outputs and classes** lists each model it adds or changes, its
  materialization, and whether it is under `dbt.contract_paths`.
- Acceptance checks name the dbt tests that prove each one: a dbt unit test (`unit_tests:` in
  the model's YAML, with `is_incremental` overridden for an incremental model), a generic data
  test, or a singular test under the project's test path.
- An incremental model's acceptance checks name its `unique_key` (or a strategy that needs
  none: `append`, `microbatch`, or `insert_overwrite` with `partition_by`) and its lookback for
  late rows.
- Changed files under the dbt model paths raise the `pipeline` flag. The crew comes from
  `tq pack crew --flags <triage flags>`; never hard-code it.

## fix

- The regression test is at the lowest data layer: a dbt unit test over the model with the
  smallest `given` rows that show the bug, or a singular data test that returns the bad rows.
  Fixtures are synthetic (`tester`, `acme`).
- A fix that changes a contracted model's columns still follows the contract version rule (see
  the `data-contracts` notes): a removed or narrowed column is breaking.
- A duplicate-row bug in an incremental model is fixed with a `unique_key` or a dedupe, not
  with `--full-refresh`.

## kickoff

- Applies only when the stack has a dbt project (`dbt_project.yml`). Otherwise skip this
  section. Never assume the data pack is enabled.
- Propose the `packs.data` block for the human to add to the manifest (headless: write it to
  `docs/privacy-manifest-proposal.yaml`):

  ```yaml
  packs:
    data:
      enabled: true
      tools: [dbt, sqlfluff]
      dbt:
        project: <dir holding dbt_project.yml, or omit to auto-detect>
        contract_paths: ["models/marts/**"]
        new_model_tests: [unique, not_null]
        build: off
        dev_target: dev
        prod_targets: [prod, production]
  ```

- Set `contract_paths` to the public layer (usually the marts). Add `+contract: {enforced:
  true}` for that folder in `dbt_project.yml`.
- Add a `.sqlfluff` with the warehouse's dialect and `templater = jinja`. tq never uses the dbt
  templater, which needs a live connection.
- `templates/dbt-contract-starter/` is a working layout to copy from.

## dod

| DoD item | Who fixes it |
|---|---|
| `pack:data/dbt-new-model-tests` | the implementer: add the YAML entry, the description, and the key tests on the primary key |
| `pack:data/dbt-contract-enforced` | the implementer: enforce the contract and give every column a `data_type` |
| `pack:data/dbt-incremental` | the implementer: set `unique_key`, or a strategy that needs none (`append`, `microbatch`, `insert_overwrite` with `partition_by`); a lookback warning is for the reviewer |
| `pack:data/sql-select-star`, `pack:data/sql-hardcoded-relation` | the implementer: list the columns and use `ref()`/`source()`. A `-- tq:allow <rule>: <reason>` comment is the reviewer's call, never a way to pass |
| `pack:data/dbt-parse` | the implementer, from the redacted tail. A declared-missing `dbt` means install it, or the human removes it from `tools` |
| `pack:data/dbt-build-modified` | opt-in (`dbt.build: dev`). A missing `dbt.state` manifest is the human's export |
| `pack:data/sqlfluff` | the implementer: fix the lint. A baseline refresh is human-only |

## ship

- The PR body quotes the data pack lines of the `tq pack check` summary, with every dbt gate
  that warned (lookback, `select *` outside contracted models, missing descriptions).
- Name each new or changed model, its materialization, and whether its contract is enforced.
- Never claim a production dbt run happened. Agents cannot run against `prod_targets`.
