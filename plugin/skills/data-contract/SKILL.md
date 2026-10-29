---
name: data-contract
description: Write and evolve ODCS v3 data contracts under the tentaqles data pack. Drafts a contract from a dbt model or a table description using the data-contract.odcs.yaml template, classifies the change (breaking or not) with the pack's breaking-change rules, applies the version rule (a breaking change bumps the major version), writes the consumer migration note into the spec slice, and runs tq pack check --pack data. Use when the user says "data contract", "ODCS", "write a contract for this model", "is this a breaking change", "bump the contract version", when a contract file is in the diff, when triage raises data_contract, or when pack:data/contract-* fails.
argument-hint: "[new <dbt model or table> | evolve <contract file> | check]"
---

# Data contract

A contract is a YAML file under `packs.data.contracts.paths` (default
`contracts/**/*.odcs.yaml`) with `apiVersion: v3.x.x` and `kind: DataContract`
(ODCS v3). It is the promise a dataset makes to its consumers: columns, types,
nullability, keys, allowed values, quality thresholds, SLAs and where the data
lives. The data pack checks it on every change (tq's `docs/packs/data-contracts.md`
has every rule):

| Check | Fails when |
|---|---|
| `data/contract-valid` | the file does not parse, an ODCS field (`apiVersion`, `kind`, `id`, `version`, object and property `name`) is missing, or the `id` is used by another contract |
| `data/contract-breaking` | a breaking change without a major version bump |
| `data/contract-pii` | a classified property's table is not in the manifest `data.paths` with that class, or an `examples` value looks like personal data |
| `data/contract-dbt-sync` | the dbt model named by `customProperties: [{property: dbtModel, value: <model>}]` disagrees with the contract |
| `data/datacontract-lint`, `data/datacontract-breaking` | `datacontract lint` / `datacontract breaking` (when the tool is installed; required when declared in `tools`) |

Personal data may be read locally to understand a table. It never goes into a
contract: `examples` hold synthetic values only (people named `tester`, company
`acme`, `tester@acme.test`), each marked `# tq:allow-pii`.

Never edit `tq-packs.yaml`, a baseline or the manifest to make a check pass, and
never lower `contracts.enum_added` or drop a consumer to get a change through.
The manifest (`contracts.consumers`, `data.paths`) is the human's: propose the
change and let the human make it.

## 1. Where the contracts are

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 10"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 10"
$_to tq pack list --enabled --json; echo "packs_exit=$?"
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" ls-files '*.odcs.yaml' '*.odcs.yml'
echo "template=$_tqe/templates/process/data-contract.odcs.yaml"
```

- The `data` pack's `config.contracts` holds `paths`, `enum_added` (`warn` by
  default, `breaking` when consumers validate values) and `consumers` (contract
  id to the list of consumers).
- No `data` pack in the list: the checks will not run. Say so; the human enables
  the pack (the kickoff on-ramp proposes the block). You can still write the
  contract.

## 2. A new contract

1. Copy the template to `contracts/<dataset>.odcs.yaml`
   (`cp "$_tqe/templates/process/data-contract.odcs.yaml" "$root/contracts/<dataset>.odcs.yaml"`).
2. Fill it from the source:
   - **From a dbt model:** read the model's YAML entry (`models:` under the
     project's `model-paths`). One property per column, `physicalType` from
     `data_type`, `required: true` where the column has a `not_null` test or
     constraint, `primaryKey` from a `primary_key` constraint, `unique` from a
     `unique` test, valid values from `accepted_values` as an `invalidValues`
     quality rule with `arguments.validValues`. Set the object's
     `customProperties: [{property: dbtModel, value: <model>}]` and
     `physicalName` to the relation the model builds.
   - **From a table description:** one property per column with `logicalType`
     (an ODCS type: string, date, timestamp, time, number, integer, object,
     array, boolean, map, vector) and `physicalType` as the warehouse spells it.
3. Give every object and property an `id`: renames are only detectable with
   stable ids.
4. `classification` from the manifest `data.paths` and the `classify-data` skill:
   `pii`, `sensitive`, `financial` or `children` (ODCS values such as
   `restricted` map onto them). A classified property's table must be in the
   manifest `data.paths` with that class; when it is not, write the proposed
   `data.paths` row for the human.
5. Start at `version: 1.0.0` and `status: active` (or `draft` until a consumer
   reads it).
6. Run step 4.

## 3. Evolving a contract

Classify every change from the consumer's view before you edit the version:

| Change | Class |
|---|---|
| contract deleted; object removed or renamed; `physicalName` changed | breaking |
| property removed, or renamed (same `id`, new `name`) | breaking |
| `logicalType` changed, except `integer` to `number` and `date` to `timestamp` | breaking |
| `physicalType` narrowed (shorter `varchar`, `bigint` to `int`, less precision or scale) | breaking |
| `required: true` to `false`; `unique: true` to `false`; primary key changed or reordered | breaking |
| `logicalTypeOptions` loosened (`maxLength` up, `minimum` down, `maximum` up, `pattern` or `format` removed) | breaking |
| enum value removed; a quality rule removed or loosened (including a changed sql `query`); `validValues` gained a value | breaking |
| an SLA loosened (latency or frequency up, retention or availability down) | breaking |
| a server removed, or its host, database, schema, table or location changed | breaking |
| `status: retired` while `contracts.consumers` lists anyone | breaking |
| enum value added | `contracts.enum_added` (`warn` by default) |
| `partitioned` or partition key changed | warn (consumer query cost) |
| a new property or optional field; a tighter quality rule or SLA; `required: false` to `true` | non-breaking |
| `classification` changed | non-breaking, but the privacy review runs and `data/contract-pii` checks the manifest |

**Version rule:** any breaking row needs the MAJOR part of `version` bumped
(`1.4.2` to `2.0.0`). A non-breaking change bumps MINOR (new fields) or PATCH
(descriptions). A major bump with no breaking change is a warning, not a pass
for later changes.

For a breaking change:
- Prefer the expand path when it exists: add the new property next to the old
  one, mark the old one `deprecated` in its description, ship, and remove it in a
  later major version once the consumers moved.
- Write `## Consumer migration` into the spec slice: each consumer from
  `contracts.consumers` (the `tq pack list` config), what it must change, and
  from which version. No consumers listed: say so in the section, and ask the
  human whether the list is complete.
- Retiring a contract: set `status: retired` and ship that first; delete the
  file only after the human removed its consumers from the manifest.

## 4. Check

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 630"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 630"
$_to tq pack check --pack data --budget 600s; echo "check_exit=$?"
```

- `check_exit=0`: no required check failed. Read the `warn` findings anyway: an
  `enum-value-added` or `partitioning-changed` warn belongs in the PR body.
- `check_exit=1`: fix each `error` finding at its `file:line`. For
  `version-not-bumped`, either bump the major version and write the consumer
  migration, or undo the breaking change. Never reword the contract to hide a
  breaking change (for example dropping an `id` so a rename looks like a new
  property: that is a removal, still breaking).
- `check_exit=2`: a config error in the manifest or `tq-packs.yaml`; explain it
  to the human.
- `124`: the outer timeout hit; report it, do not loop.
- `data/datacontract-*` skipped: the tool is not installed and not declared.

The `pack` skill explains every result line and the DoD items.

## 5. Shipping a breaking change

When `data/contract-breaking` says "with breaking changes and a major version
bump", the `ship` skill titles the PR with `!` after the type
(`feat(data)!: orders contract v2`) and ends the body with a footer:

```text
BREAKING CHANGE: contract acme-orders 2.0.0 removes orders.ordered_at; consumers: acme-finance-dashboard
```

Name every breaking contract id, its new major version and its consumers. Never
say a consumer was migrated unless its change is merged.
