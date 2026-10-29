# data: contracts

## kickoff

Only when the stack has a dbt project, a pipeline orchestrator (Airflow,
Dagster, Prefect), Fabric or Synapse, or the repo already holds `*.odcs.yaml`
files (this section is read whether or not the data pack is enabled; never
assume it is):
- At human stop 1, propose the `packs.data` block with a `contracts:` entry:
  `paths: ["contracts/**/*.odcs.yaml"]`, `enum_added: warn` (or `breaking` when
  consumers validate values), and `consumers:` keyed by contract id when the PRD
  names who reads the data. Put `datacontract` in `tools` only when
  `command -v datacontract` finds it. It is a proposal for the human, who edits
  the manifest and runs the trust step; you never edit `.tentaqles.yaml`.
  Headless: write it to `docs/privacy-manifest-proposal.yaml`, never the
  manifest.
- Create `contracts/` and write the first contract with the `data-contract`
  skill for each dataset the first slice publishes.
- Classified properties (`classification: pii` and the like) need their table
  in the manifest `data.paths` with that class: list the rows in the same
  proposal for the human (the `classify-data` skill owns that block).

## feature

- The work raises `data_contract` when it touches a file under
  `packs.data.contracts.paths`. Before any diff exists, pass it to
  `tq pack crew --flags data_contract` and take the crew it prints. A changed
  `classification` also needs the privacy review (`classified_data`).
- Spec slice: a **Contracts touched** list (contract file, change class:
  breaking or non-breaking, old and new version) and **Outputs and classes**
  (each output table and its classes). A breaking change needs a
  `## Consumer migration` section naming the consumers from
  `contracts.consumers` and what each must change.
- Acceptance checks to add: "`data/contract-valid` and `data/contract-breaking`
  are ok", "`data/contract-pii` is ok", and "`data/contract-dbt-sync` is ok"
  when the contract names a `dbtModel`.
- Evolve contracts with the `data-contract` skill. Never lower a check to get a
  change through: a breaking change bumps the major version.

## fix

- A bug in what a contract promises is fixed in the data first; the contract
  changes only when the promise was wrong, and then the version rule still
  applies (a breaking correction is a major bump with a `## Consumer migration`).
- The regression test sits at the lowest data layer: a dbt singular or unit
  test, or a pytest over a synthetic frame. When a contract check missed the
  bug, tell the human which rule should have caught it; never work around a
  check that did fire.

## dod

| Item | Who fixes it |
|---|---|
| `pack:data/contract-valid` | you: a missing ODCS field, a duplicate id, a file that does not parse |
| `pack:data/contract-breaking` | you: bump the major version and write `## Consumer migration`, or undo the breaking change |
| `pack:data/contract-pii` | the human adds the table to the manifest `data.paths` with the class; you replace a real-looking `examples` value with a synthetic one marked `tq:allow-pii` |
| `pack:data/contract-dbt-sync` | you: align the dbt model YAML (`columns`, `data_type`, a `not_null` test) or the contract |
| `pack:data/datacontract-lint`, `pack:data/datacontract-breaking` | you, from the tool's output; `MISSING` means `datacontract` is declared but not installed (the `pack` skill, step 4) |

## ship

- Quote the `data/contract-*` results in the PR body.
- When `data/contract-breaking` lists breaking changes (its detail says "with
  breaking changes and a major version bump"), the PR title gets `!` after the
  type (`feat(data)!: ...`) and the body ends with a `BREAKING CHANGE:` footer
  naming each contract id, its new major version and its consumers.
