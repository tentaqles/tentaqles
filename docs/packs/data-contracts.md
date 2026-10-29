# Data pack: data contracts

The data pack checks [ODCS v3](https://bitol-io.github.io/open-data-contract-standard/)
data contracts on every change: that they are valid, that a breaking change
bumps the major version, that classified columns are classified in the manifest
too, and that a contract agrees with the dbt model it describes. The
`data-contract` skill writes and evolves contracts; the template is
`plugin/templates/process/data-contract.odcs.yaml`.

Field names were checked against the ODCS JSON Schema `odcs-json-schema-latest`
(v3.2.0, October 2026). tq reads v3.0.x, v3.1.x and v3.2.x contracts; every
field it reads has the same name in all three.

## Config (`packs.data.contracts`)

```yaml
packs:
  data:
    enabled: true
    tools: [datacontract]          # optional: makes data/datacontract-* required
    contracts:
      paths: ["contracts/**/*.odcs.yaml"]                # default: **/*.odcs.yaml and **/*.odcs.yml
      enum_added: warn                                   # warn (default) | breaking
      consumers: { acme-orders: [acme-finance-dashboard] }   # by contract id (or name, or file name)
```

| Key | Merge with `tq-packs.yaml` |
|---|---|
| `contracts.paths` | union: a repo may add paths |
| `contracts.consumers.<id>` | union: a repo may add consumers |
| `contracts.enum_added` | trusted: a repo may set it when the manifest does not, never change it |

An unknown key under `contracts` is a manifest error.

## What a contract is

A file matched by `contracts.paths`. It must have `apiVersion: v3.x.x` and
`kind: DataContract`; `data/contract-valid` reports a matched file that has
neither, and an ODCS v2 file (`apiVersion: v2.*`) is refused. The fields tq reads:

| Level | Fields |
|---|---|
| contract | `apiVersion`, `kind`, `id`, `name`, `version`, `status`, `servers[]`, `schema[]`, `slaProperties[]`, `customProperties[]` |
| object (`schema[]`) | `id`, `name`, `physicalName`, `properties[]`, `quality[]`, `customProperties[]` |
| property | `id`, `name`, `physicalName`, `logicalType`, `physicalType`, `required`, `unique`, `primaryKey`, `primaryKeyPosition`, `partitioned`, `partitionKeyPosition`, `classification`, `logicalTypeOptions`, `enum[]` (strings or `{value}`), `examples[]`, `quality[]`, nested `properties[]`, `items` |
| quality rule | `id`, `metric` (or the deprecated `rule`), `name`, `type`, `arguments` (`validValues`), the operators `mustBe`, `mustNotBe`, `mustBeGreaterThan`, `mustBeGreaterOrEqualTo`, `mustBeLessThan`, `mustBeLessOrEqualTo`, `mustBeBetween`, `mustNotBeBetween` |
| SLA | `property`, `value`, `unit`, `element` |
| server | `server`, `type`, and the address fields `host`, `port`, `account`, `database`, `catalog`, `project`, `dataset`, `schema`, `table`, `location`, `path`, `workspace`, `warehouse`, `endpointUrl` |

## Checks

| Id | Kind | Fails when |
|---|---|---|
| `data/contract-valid` | built-in | the file is not YAML; `apiVersion` is not v3; `kind` is not `DataContract`; `id` or `version` is empty; an object or property has no `name`; a `logicalType` is not an ODCS type; a boolean field is not a boolean; a server has no `server` or `type`; two objects (or two properties of one object) share an id or a name; the contract `id` is used by another contract file. A `version` that is not semver is a warn. |
| `data/contract-breaking` | built-in | a breaking change (table below) without a major version bump; a breaking change when a version is not semver; a version that went down |
| `data/contract-pii` | built-in | a property is classified (mapping below) while its object's table is not in the manifest `data.paths` with that class; an `examples` value of a classified property hits the redaction detectors without `tq:allow-pii` on its line (a hit on an unclassified property is a warn) |
| `data/contract-dbt-sync` | built-in | the dbt model named by `customProperties: [{property: dbtModel, value: <model>}]` has no YAML entry; a property has no column; `physicalType` and `data_type` differ; a `required` property has no `not_null` test or constraint (a column constraint, or a model-level `constraints: [{type: not_null | primary_key, columns: [...]}]`). A column the contract does not list, and a `not_null` column the contract does not mark required, are warns. |
| `data/datacontract-lint` | external `datacontract` | `datacontract lint --no-inline-references <file>` exits non-zero (timeout 60 s) |
| `data/datacontract-breaking` | external `datacontract` | `datacontract breaking --no-inline-references <old> <new>` exits non-zero on a modified contract without a major bump (with a bump it is a warn) |

All built-in checks run on the changed contract files (every contract with
`--all`); `data/contract-valid` reads every contract to find duplicate ids, and
`data/contract-dbt-sync` also runs when only a dbt YAML file changed.
`data/contract-breaking` and `data/datacontract-breaking` need an old side and
skip under `--all`. The external checks hand the tool copies of both sides in
the run's scratch directory, so `--staged` and renamed files work, and pass
`--no-inline-references` so the tool resolves nothing over the network.

### How changes are matched

The old side is the file at the base (`git show <base>:<path>`, the old path for
a rename). Objects and properties match by `id`, falling back to `name`
(recursively through nested properties and array `items`); quality rules by
`id`, falling back to `metric` + `name`; a rule with none of those (a
`type: sql` rule with only a `query` and an operator, a `text` or `custom`
rule) pairs with an identical rule first, then with a rule of the same `type`
and operator set, in order. SLA entries match by `property` + `element`;
servers by `server`. A rename is only detectable with a stable `id`: without
one it is a removal plus an addition, which is breaking anyway.

A modified, renamed or deleted contract exists at the base by definition, so
when its base side can't be read (git failed or timed out, a bad ref) the check
fails with `contract-old-unreadable` instead of passing. A base side that is
not YAML, or not a contract, is skipped (it was not a contract).

### Breaking-change rules

| Change | Class | Rule id |
|---|---|---|
| contract file deleted | breaking (warn when the old side was `status: retired` and no consumers are listed) | `contract-deleted` |
| object removed | breaking | `object-removed` |
| object renamed (same `id`) or `physicalName` changed | breaking | `object-renamed` |
| property removed (or `items` removed) | breaking | `property-removed` |
| property renamed (same `id`) or `physicalName` changed | breaking | `property-renamed` |
| `logicalType` changed, except `integer → number` and `date → timestamp` | breaking | `logical-type-changed` |
| `physicalType` narrowed or removed, or changed in a way tq cannot prove is a widening | breaking | `physical-type-narrowed` |
| `required: true → false` | breaking | `required-loosened` |
| `unique: true → false` | breaking | `unique-loosened` |
| primary key set changed or reordered | breaking | `primary-key-changed` |
| `logicalTypeOptions` loosened: `maxLength`, `maximum`, `exclusiveMaximum`, `maxItems`, `maxProperties` up or removed; `minLength`, `minimum`, `exclusiveMinimum`, `minItems`, `minProperties` down or removed; `pattern` or `format` removed or changed; `uniqueItems: true → false` or removed | breaking | `type-options-loosened` |
| enum value removed | breaking | `enum-value-removed` |
| enum value added, or the enum removed | `contracts.enum_added` (warn by default) | `enum-value-added` |
| quality rule removed; threshold loosened (`mustBeGreaterThan`/`OrEqualTo` down, `mustBeLessThan`/`OrEqualTo` up, `mustBeBetween` wider, `mustNotBeBetween` narrower, `mustBe`/`mustNotBe` changed, an operator removed); `metric` or `type` changed; the `query` (sql) or `engine`/`implementation` (custom) changed, compared with whitespace collapsed and never printed; `arguments.validValues` gained a value; another argument changed | breaking | `quality-removed`, `quality-loosened` |
| SLA removed or loosened: `latency`, `frequency`, `errorRate`, `timeToDetect`, `timeToNotify`, `timeToRepair` up; `retention`, `availability`, `throughput`, `endOfSupport`, `endOfLife` down (time units are converted: ms, s, m/min, h, d, w, mo/month, y; a value with a `%` suffix compares as a number when both sides have it) | breaking | `sla-loosened` |
| SLA changed where tq does not know the direction, or the units can't be compared | warn | `sla-changed` |
| server removed, or an address field changed | breaking | `server-removed`, `server-changed` |
| `partitioned` or `partitionKeyPosition` changed | warn (consumer query cost) | `partitioning-changed` |
| new property; new optional field; tighter quality or SLA; `required: false → true` | non-breaking | — |
| `classification` changed either way | warn; `data/contract-pii` checks the manifest and the privacy reviewer runs | `classification-changed` |
| `status` set to `retired` (any case) while `contracts.consumers` lists anyone | breaking | `retired-with-consumers` |
| the top-level contract `id` changed (consumers listed under the old id are still named) | warn | `contract-id-changed` |
| the base side of a modified, renamed or deleted contract can't be read | error | `contract-old-unreadable` |

`physicalType` widening, by family (case and spacing ignored): integers
`tinyint < smallint < int < bigint` (with `int2/int4/int8`, `int64`, `long`);
`decimal`/`numeric`/`number(p,s)`, where precision, scale and integer digits
may only grow and no arguments means unbounded; `real < double`; strings
(`varchar`, `char`, `text`, `string`, …) where a length may only grow and no
length means unbounded; `date < timestamp`, where a fractional-second
precision (`timestamp(6)`) may only grow when both sides state one; an
integer to a `decimal` with enough integer digits. Moving to a time-zone type, another family or an
unknown type is breaking.

### Version rule

`version` is semver (`MAJOR.MINOR.PATCH`, a leading `v` allowed). With any
breaking row and the major part not bumped, every breaking row is an error plus
`version-not-bumped` (naming the consumers). With the major bumped the breaking
rows stay as warns, so the PR can name them, and the detail says
"mark the PR as breaking". A major bump with no breaking row is a warn
(`major-without-breaking`). A breaking row with a `version` that is not semver
is `version-not-semver`, a version that goes down is `version-decreased`, and
a new side that does not parse is `contract-unreadable` (all errors). Findings never quote enum values, example values or
thresholds: they name the contract, the property path and the rule.

## Classification mapping (`data/contract-pii`)

| ODCS `classification` | tq data class |
|---|---|
| `pii`, `personal`, `personal-data`, `personal_data`, `pi`, `gdpr`, `lgpd` | `pii` |
| `sensitive`, `restricted`, `special-category`, `special_category`, `spi` | `sensitive` |
| `financial`, `pci` | `financial` |
| `children`, `minors` | `children` |
| anything else (`public`, `internal`, `confidential`, numeric levels) | not a personal-data class |

Values are compared case-insensitively. The object's table is its
`physicalName` (or `name`), looked up bare and qualified with each server's
`schema` among the manifest `data.paths` `table:` entries. The examples check
runs the same detectors as `tq redact` for the property's class (`pii` for an
unclassified property); `tq:allow-pii` on the value's line or on the
`examples:` line marks known-synthetic values.

## Triage, crew and doctor

- Flag `data_contract`: raised when a changed or deleted file matches
  `contracts.paths`. Reviewer: `pipeline-reviewer` (`tq pack crew --flags data_contract`).
- Detection hints for `packs-suggest`: `*.odcs.yaml`, `*.odcs.yml`.
- Doctor `data-contracts-none` (info): models (`.sql`, `.py`) exist under
  `packs.data.dbt.contract_paths` but no file matches `contracts.paths`. The
  globs are relative to the dbt project (`dbt.project`, else each directory
  holding a `dbt_project.yml`), and a repo-relative glob also works.

## Shipping

A PR with a breaking contract change is titled with `!` (`feat(data)!: …`) and
ends with `BREAKING CHANGE: contract <id> <new major> …; consumers: …`. The
spec slice carries `## Consumer migration`. See the `data-contract` skill.
