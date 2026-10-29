# Packs

A pack is a set of checks, guard rules, reviewer agents, skills and templates for
one kind of work. Its checks run in the definition of done (`tq pack check`), its
guard rules run in the PreToolUse hook, and its reviewers join the build crew when
triage raises one of its flags.

| Pack | Release | What it covers |
|---|---|---|
| `data` | 3 | data contracts (ODCS v3), dbt gates, pipeline review, warehouse cost guardrail |
| `ml` | 3 | leakage lint, reproducibility checklist, model promotion gate, `MODEL_CARD.md`, drift check |
| `cloud` | 4 | IaC policy checks and the apply gate |
| `db` | 4 | migrations, RLS tests and restore drills |

A workspace with no `packs:` block has no packs: `tq pack check` prints
`no packs enabled` and exits 0.

**Always on, with or without a pack.** These core rules protect the pack
approvals and config. They apply in every workspace and are in the locked guard
groups: every guard mode applies them, `guard.disable` of them is ignored and
`guard.actions` may only make them stricter:

- `tq/hub-store-write`: agents cannot write tq's hub store, pack ledgers,
  claims or plans. A hook-time check also denies any agent shell or file access
  to `$TQ_HOME` `hub/`, `packs/`, `build-gate/`, `trust/`, `guard/`,
  `guard.yaml`, `config.yaml` and `audit.jsonl`, under any spelling of the path.
- `tq/human-answer`: an agent cannot answer an approval
  (`tq hub answer`, the hub's answer endpoints) or run an apply
  (`tq pack <pack> ... apply`).
- `tq/pack-config-change`: asks before an agent edits `tq-packs.yaml`, a
  baseline file or the manifest's `packs:` block.
- `tq/secret-store-shell`: the pack ledger key in the OS keychain is never
  read from the shell.

See [CLAUDE-HOOK.md](CLAUDE-HOOK.md) for every rule.

## Config

### The manifest `packs:` block (trusted)

```yaml
packs:
  data:
    enabled: true
    tools: [dbt, sqlfluff]          # external tools that become required checks
    timeouts: { dbt: 600s }         # optional per-tool override (cap 1800s)
    tool_paths: { dbt: C:/tools/dbt/dbt.exe }   # optional; trusted layer only
    baseline: .tq/sqlfluff-baseline.json        # optional external-tool baseline
    # ...pack-specific keys, documented on each pack's page
  ml: { enabled: true, tools: [ruff] }
```

- The block is part of `.tentaqles.yaml`, so `tq allow` pins it. An unknown pack
  name, an unknown pack key or a tool name outside `[a-z0-9_-]` is a load error. The
  secret scan runs over it as over the rest of the manifest: it holds names, never
  values.
- A non-empty block without `enabled` is enabled. `enabled: false` turns a pack off.

### Tools: built-in, external, required, advisory

| Check | Tool | Result when the tool is missing | When it fails |
|---|---|---|---|
| built-in (pure Go) | none | always runs | required: fails the DoD |
| external, tool declared in `tools:` | installed | `fail`: `declared in packs.<p>.tools but not installed; install it or remove it from tools` | required: fails the DoD |
| external, tool not declared | installed | runs | **advisory**: recorded, never fails |
| external, tool not declared | missing | `skip` | — |

tq looks for a tool in `tool_paths.<tool>` (manifest only), then the project's
`.venv`, then `PATH`. A finding is `error` or `warn`; only `error` fails a check.

### The repo layer `tq-packs.yaml` (untrusted, add-only)

A repo inside the workspace may hold a `tq-packs.yaml` at its root with the same
schema. When it is merged with the manifest, the stricter value always wins:

| Key kind | Merge rule |
|---|---|
| `enabled` | trusted layer only: only the manifest enables a pack. A `tq-packs.yaml` block for a pack the manifest does not configure is ignored and reported |
| `tools` | union |
| path lists | union |
| upper bounds (cost caps, drift `warn`/`fail`, `valid_for`) | minimum |
| lower bounds (`drift.metrics.<m>.min`) | maximum |
| `tool_paths`, `baseline` | ignored: trusted-layer only |
| any key without a merge rule | ignored: trusted-layer only |
| commands (`ml.promotion.command`, `ml.drift.current_command`) | allowed; they run only from a human-confirmed command or a frozen DoD run |

A key that tries to loosen a trusted value is ignored and reported by `tq doctor`
(`packs-repo-file-invalid`). The guard `tq/pack-config-change` asks before an agent
edits `tq-packs.yaml` or a baseline file.

## CLI

| Command | What it does | Exit |
|---|---|---|
| `tq pack list [--enabled] [--json]` | the packs, the merged config and its `config_hash`, and each check with kind, tool and `required` | 0 |
| `tq pack check [--pack X]… [--staged \| --paths … \| --base <ref> \| --all] [--json] [--builtin-only] [--budget 600s]` | runs the checks that apply to the changed files | 0 no required check failed; 1 one did; 2 usage or config error |
| `tq pack doctor [--json]` | per tool: declared, installed, path, version, minimum; plus each pack's probes | 0, or 1 when a declared tool is missing |
| `tq pack crew --flags a,b [--json]` | the reviewer agents the triage flags map to | 0 |
| `tq pack approvals [--pack X] [--json]` | pending, answered, used and expired pack approvals | 0 |
| `tq pack baseline [--pack X]` | human only: write or refresh the external-tool baseline | 0 |
| `tq pack <pack> <subcommand>` | pack subcommands, for example `tq pack data cost estimate`, `tq pack ml promote`, `tq pack cloud plan` | per command |

Without a selection flag, `tq pack check` takes the working tree against `HEAD`
plus untracked files. `--all` treats every tracked file as changed (diff checks
skip). A tool gets a filtered environment, not the parent one: a base list (`PATH`,
`HOME`, `TEMP`, locale and a few more) without anything named like a secret,
plus the names its check declares (dbt's profile variables, the cloud CLI's
config homes and so on). A check that needs warehouse or cloud credentials gets
them only through a declared name, for example `tq dotenv run -- tq pack check`.
A tool resolved inside the repository (the project `.venv`, `node_modules/.bin`)
never gets a secret-looking name; the check gets a `tq/tool-env` warning naming
it. Keep such a tool in `tool_paths` or outside the repo when it needs a
credential. Every tool also gets `DO_NOT_TRACK=1`, plus the known telemetry
opt-outs of dbt, tflint, trivy, dvc and dagster.

`--json` prints schema `tq.packs/v1`:

```json
{
  "schema": "tq.packs/v1",
  "workspace": "acme",
  "base": "HEAD",
  "config_hash": "<sha256 of the merged config, canonical JSON>",
  "results": [
    {"id": "data/contract-breaking", "pack": "data", "status": "ok", "required": true,
     "detail": "2 contracts, no breaking change", "findings": []}
  ],
  "summary": {"ok": 1, "fail": 0, "skip": 0, "required_fail": 0}
}
```

A result's `status` is `ok`, `fail` or `skip`; each finding has `file`, `line`,
`level`, `rule` and `message`, and never a value from the scanned file.

## Definition of done

The build gate (`/tentaqles:build`, `/tentaqles:dod`) ties packs to the plan:

- **At approval** it runs `tq pack list --enabled --json` (10 s) and freezes the
  enabled packs and the `config_hash` with the plan. Without tq, or with a tq
  that has no `pack` command, it freezes no packs and no hash. An approval is
  refused when tq is there but the pack config can't be read (a config error, a
  timeout, output that is not JSON), and when `tq pack list` reports
  `problems`, such as a `tq-packs.yaml` that tried to loosen or enable
  something. Since this runs even with no pack configured, approval also fails
  closed in a workspace whose manifest is untrusted or invalid, or whose tq
  registry cannot be read: fix the workspace (`tq doctor`, `tq allow`) first.
- **In the definition of done** it runs
  `tq pack check --json --base <approval base> --budget <B>s` with a subprocess
  timeout of `B + 30` s. `B` is 600, or the `pack_budget` (seconds, at most 1800)
  of the project's `tq-project` block. It records:

| Item | Recorded as |
|---|---|
| `pack` | skip, when no config hash was frozen at approval: tq was missing or had no `pack` command then, or the plan was approved before the freeze existed |
| `pack:config` | ok when the current `config_hash` equals the frozen one; otherwise missing: "pack config changed since approval; re-approve the plan" |
| `pack:run` | missing when a config hash was frozen (even with no pack enabled) and tq is now missing, crashes, times out or prints invalid `tq.packs/v1` output (fail closed) |
| `pack:<pack>/<check>` | one per result: ok for `ok`; missing for a required `fail`; skip for an advisory `fail` or a `skip`; the detail carries the first 5 findings |

The Stop hook only reads the recorded `dod.json`; no hook ever runs `tq pack check`.

**Reviewers.** The gate accepts any reviewer name matching `<name>-reviewer` that has
an agent file in the plugin's `agents/`, so a pack reviewer needs no gate change.
`tq pack crew` prints which ones the triage flags call for, and the build skill adds
them to the crew.

**On-ramps.** `build`, `feature`, `fix`, `kickoff`, `dod` and `ship` each read the
on-ramp notes of the enabled packs, `plugin/skills/pack/references/onramps/<pack>-*.md`
(one `## <on-ramp>` section per skill they change). The `pack` skill explains the
results and how to fix a declared tool that is missing.

## Known behaviours

- **Unknown global flags floor to the strictest environment.** The cloud and db
  gates read a tool's documented global flags. A flag tq does not know, written
  before the subcommand and followed by another word, may take that word as its
  value or not, so tq cannot tell which subcommand runs. Such a command counts
  as an apply (or a migration) to the **strictest** declared environment, with
  no approval to match: `supabase --x v db push`, `terraform -foo x apply`,
  `pulumi --x a b up`, and the same for tofu, cdk/cdktf, az, aws and sam. With
  `approve` that is a deny. Put the flag after the subcommand, or report the
  missing flag (tool, flag, whether it takes a value, a link to the tool's
  docs) as an issue on the tentaqles repository so it can be added.
- **Pack approvals are answered in the terminal only.** `tq hub answer` needs a
  TTY and the typed binding, and the board refuses the answer. A machine with
  no OS keychain (`packs-keychain-missing` in `tq pack doctor`) cannot grant
  pack approvals.
- **`pwsh -File` and script files ask.** With data or ml enabled, a PowerShell
  script file, `pwsh -Command -` or an undecodable `-EncodedCommand` asks, since
  the guards cannot read it. A bash script file asks only when its content names
  the guard's tool.

## Index

Each pack workstream documents its checks, config keys, guard rules and reviewers
on its own page in [`docs/packs/`](packs/):

| Pack | Page | Covers |
|---|---|---|
| data | [Data contracts](packs/data-contracts.md) | ODCS v3 contracts and breaking-change rules |
| data | [dbt](packs/data-dbt.md) | dbt gates, `tq/dbt-prod-target`, `tq/dbt-full-refresh` |
| data | [Pipelines](packs/data-pipelines.md) | Python pipelines, Fabric, Synapse, `pipeline-reviewer` |
| data | [Warehouse cost](packs/data-cost.md) | cost guards and `tq pack data cost estimate` |
| ml | [ML lints](packs/ml-lints.md) | leakage lint and the reproducibility checklist |
| ml | [Promotion, card, drift](packs/ml-promotion.md) | the promotion gate, `MODEL_CARD.md`, the drift check |
| ml | [ML review](packs/ml-review.md) | `ml-reviewer` and the `ml-starter` template |
| cloud | [IaC](packs/cloud-iac.md) | IaC policy checks |
| cloud | [Apply gate](packs/cloud-apply-gate.md) | plan approvals and the apply gate |
| db | [Migrations](packs/db-migrations.md) | migration checks and `db-reviewer` |
| db | [RLS](packs/db-rls.md) | row-level security tests |
| db | [Restore drill](packs/db-restore-drill.md) | restore drills and their freshness |
| all | [Framework security](packs/framework-security.md) | the core rules, the state-dir check, approvals and the tool environment |
