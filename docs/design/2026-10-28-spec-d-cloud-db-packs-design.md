# Spec D: the cloud and database packs

**Parent:** `../../../docs/superpowers/specs/2026-10-20-development-process-design.md`, release 4 (§13) and the guard rules of §8.2. **Framework:** `2026-10-28-spec-c-data-ml-packs-design.md` §2 (the shared pack framework), including the §2.15 contract changes. This spec conforms to spec C §2 and describes only what the `cloud` and `db` packs add.
**Status:** draft for approval, 2026-10-28. Nothing here is implemented.

## 1. Decisions

| Topic | Decision (human, 2026-10-28) |
|---|---|
| Tooling | **Built-in plus external.** tq ships pure-Go lints for the core rules, so they always run: migration SQL lint, and the IAM least-privilege lint over policy JSON and plan JSON. Established tools run when they are installed: Checkov, Trivy, Conftest/OPA, tflint, bicep lint, cfn-lint, squawk, sqlfluff, and pgTAP through `pg_prove`. A tool becomes a REQUIRED definition-of-done check once the manifest lists it under `packs.<pack>.tools`. A tool that is declared but missing fails. A tool that is not declared and not installed is skipped. |
| Targets | All four: **Terraform / OpenTofu** (policy checks over plan JSON; apply and destroy gated per environment). **Azure Bicep / ARM** (`az deployment … what-if`, bicep lint, Azure RBAC least privilege). **AWS CDK / CloudFormation / Pulumi** (synth and preview gates, IAM policy lint). **Postgres/Supabase plus SQL Server/Azure SQL**: migration SQL lint for both dialects; an RLS gate and pgTAP RLS tests for Postgres/Supabase; a backup and restore drill for both. |
| Framework | Spec C §2 owns these:<br>• the `packs:` manifest block (`PackConfig.Raw`, `manifest.RegisterPackValidator`) and the `tq-packs.yaml` repo layer;<br>• `internal/packs`: `Register`, `RegisterPack`, `RegisterGuard`, `Input` with `Workspace()`, `WorkspaceRoot()` and `StateDir()`, the tool runner, the baseline ratchet, the approvals API and ledger, and `packs.Count`;<br>• the CLI: `tq pack list|check|doctor|crew|approvals|baseline`, and the mount for `tq pack <pack>`;<br>• the DoD items `pack:<pack>/<check>`, `pack:config` and `pack:run`;<br>• the triage and crew wiring, the guard wiring in `claudehook_policy.go`, the generic on-ramp step, and the `docs/PACKS.md` index. |

Designer's decisions inside that frame. Correct any that are wrong:

| Topic | Decision |
|---|---|
| Production apply | It needs a pack approval (spec C §2.11: a hub `permission` decision with `data.pack: cloud`, options `apply`/`reject`) that the human answered `apply`. Its evidence binds the hash of a saved plan, the environment and the identity, and `ValidFor` is 60 min by default, 24 h at most. The guard calls `Verify` again when the apply command runs. Without a matching, unexpired, unused approval the call is **denied**, not asked, so unattended mode (`guard.unattended`) cannot skip it. *Amended 2026-10-29 (guard modes, spec E; ruling after the final review; corrected after the r3 review):* in a workspace the human granted `relaxed` (`tq allow --mode <ws> relaxed`), or a custom table, the pack gates follow the mode: in relaxed, every pack guard outside the locked groups: cloud apply/destroy (`tq/iac-apply-env`), db migrate (`tq/db-migrate-env`), restore target (`tq/restore-target`), model promote (`tq/model-promote`), dbt prod target (`tq/dbt-prod-target`), dbt full refresh (`tq/dbt-full-refresh`), pipeline run (`tq/pipeline-run`), bq unbounded (`tq/bq-unbounded-query`), warehouse resize (`tq/warehouse-resize`) and warehouse query unbounded (`tq/warehouse-query-unbounded`) are skipped, their denies included, so no approval is required. Without the grant, relaxed runs at default. Only `rule` (or `deny`) keeps a gate: `ask` turns an unapproved prod apply or migration, which the gate denies, into a prompt the human can click through, with no approval checked, and `skip` removes it. One exception (since the r2-m2 fix): `tq/iac-apply-env` and `tq/db-migrate-env` supersede the core `tq/iac-apply` and `tq/remote-migration`, so when the mode lowers their verdict the `prod-changes` value lifts it back, up to the gate's own verdict; a custom table that skips `cloud-destructive` but keeps `prod-changes` at `rule` keeps those two gates. The locked groups (`tq/human-answer`, `tq/hub-store-write`, `tq/plan-read`, `tq/plan-secret-print`, `tq/iac-identity-drift`) hold in every mode. |
| Unresolved target | A command whose target environment tq cannot resolve is treated as `packs.cloud.default_environment`, which defaults to the strictest declared environment. The guard fails closed. |
| Plan files | They live outside the repository, under the cloud pack's `StateDir()` (`$TQ_HOME/packs/<ws>/cloud/plans/`, 0700 directory, 0600 files). The agent sees only a redacted summary. Reading or committing plan or state files is denied. |
| Pure Go | There is no new Go dependency. The SQL lexer, the plan readers and the IAM lint are written in Go over `encoding/json` and the `gopkg.in/yaml.v3` already in `go.mod`. HCL source is not parsed: Terraform source is covered by external tools, and the built-in rules read plan JSON. `pg_query_go` was rejected because it needs cgo. |
| Rego | The policy bundles shipped for Conftest are written fresh in Rego v1. They extend the Go built-ins (tags, logging, TLS, backup retention); they do not re-implement them. |
| Reviewers | Two new read-only agents: `iac-reviewer` and `db-reviewer`. The existing Jev flags `schema`, `migration`, `rls` and `data_loss` select no reviewer today; they now select `db-reviewer`. |

## 2. Goals, non-goals, success criteria

Goals
- Every IaC or migration change is linted before review by rules that need no network and no credentials. Declared tools add depth.
- No agent applies to or destroys a protected environment without a human approval that is bound to the exact plan being applied.
- Least-privilege mistakes (wildcards, admin roles, open trust) are caught from the files and plans. Nobody has to remember to look for them.
- A migration that would lock a hot table, drop live columns, or leave a new exposed table without RLS fails the definition of done with `file:line`.
- The backups are proven restorable on a schedule the manifest sets, with evidence in the repository.

Non-goals
- Running `terraform plan`, `what-if` or `synth` against a real cloud from tests or CI. In CI they are the project's own steps; tq only reads their output.
- Drift detection between state and the live cloud, cost estimation (Infracost), or Kubernetes manifests and Helm. `kubectl apply` and `helm upgrade` keep the existing `tq/iac-apply` ask; a later pack can cover them.
- MySQL, Oracle, Snowflake DDL (the data pack, spec C, owns warehouses), and NoSQL.
- Automatic secret rotation (standing rule: prevention only).
- Taking backups. The drill proves that a backup restores; scheduling the backups belongs to the provider or the project.

Success criteria
- Using golden fixtures only, `tq pack check --pack cloud` reports each built-in rule at `file:line` for Terraform plan JSON, ARM/what-if JSON, CloudFormation/CDK templates and Pulumi preview JSON.
- `tq pack check --pack db` reports each PG and MS rule, and every RLS rule, on fixture migrations. It skips tables created in the same migration.
- In a workspace with `prod: {apply: approve}`, `terraform apply prod.tfplan` is denied, then allowed once after the human answers the hub decision, then denied again: the approval is single-use, and a changed plan hash, an expired TTL or a different subscription each deny again.
- A declared tool that is not on PATH turns its DoD item into `MISSING` with the install hint.
- `go test ./...` and the plugin suite stay green, and no test touches a real cloud, database server or network.

## 3. What exists today (build on it, don't duplicate it)

| Piece | Where | Used how |
|---|---|---|
| `tq/iac-apply` (ask), `tq/iac-destroy` (ask), `tq/cloud-delete` (ask) | `internal/policy/builtin.go` | Kept as the fallback when `packs.cloud` is absent. When the pack is enabled, the environment gate (§9) supersedes them for the calls it handles. |
| `tq/remote-migration` (ask, except localhost/`--local`), `tq/migration-edit`, `tq/mcp-apply-migration`, `tq/sql-destructive-shell`, `tq/mcp-sql-write` | same | Extended in §14 with environment resolution. The regexes stay. |
| Decision functions with I/O, merged after the regex rules | `internal/cmd/claudehook_policy.go` `policyDecision` (the `tq/commit-pii` list pattern) | Not edited by this spec. The new gates register through `packs.RegisterGuard` (spec C §2.10), and F's generic wiring runs them under the pack guard bounds. |
| `guard.Input.CloudProvider/ExtraClouds`, the `wrong-cloud` block | `internal/guard/guard.go`, `internal/cmd/claudehook.go` | Unchanged. The identity check in §9.6 adds the subscription and account, not the provider. |
| `identities.<cli>.expected_subscription / expected_account / profile` | `internal/manifest/manifest.go`, `internal/doctor/verify.go` | Read by the environment gate as the expected identity. The guard never runs the network verify; it reads only local config (§9.6). |
| `cloud:` and `database:` (loose maps) | `manifest.go` | Left as they are: they describe identity and connection names. Pack settings go under `packs.cloud` and `packs.db`. |
| Triage flags `schema`, `migration`, `rls`, `data_loss`, `destructive` | `internal/decide/triage.go`; crew in `plugin/skills/build/SKILL.md` step d.3 | Through the pack's registered flags, `tq pack crew` maps them to `db-reviewer`, and `destructive` on IaC paths to `iac-reviewer` (§15). No triage or build-skill edit. |
| DoD `migration` item ("## Migration plan" in the spec) and `MIGRATION_DIR_RE` | `plugin/tentaqles/workflow/gate.py` `cmd_dod` | Kept. Pack results are added beside it by spec C's hook as `pack:db/...`. |
| Hub decisions of kind `permission`, human-only answers, `Data` map | `internal/hub/store/decisions.go`, `docs/HUB.md` | Used through spec C's approvals API (`internal/packs/approval`, §2.11) for apply, destroy and migration approvals (§9.4). There is no schema change. |
| `db-migration`, `auth-security`, `index-advisor`, `supply-chain`, `ci-pipeline` skills | `plugin/skills/` | Linked from the new skills. `db-migration` §4 to §7 gains links to the lint rule ids. |
| Fake CLIs (a copy of the test binary named `gh(.exe)`) | `internal/hub/sync/github/fake_test.go` | The same pattern for `terraform`, `az`, `checkov`, `pg_prove` and the rest (§21). |

## 4. Manifest: `packs.cloud` and `packs.db`

These are names only, never secrets. Subscription and account ids are identifiers, not credentials. The block lives inside spec C's `packs:` block:
- Spec C's loader validates the framework keys `enabled`, `tools`, `timeouts`, `tool_paths` and `baseline`.
- The pack decodes every other key below from `PackConfig.Raw`. Its validators are registered through `packs.RegisterPack` (`ConfigKeys`), which feeds `manifest.RegisterPackValidator`.
- A repo's `tq-packs.yaml` may add to these keys under spec C §2.3's stricter-wins rules:
  - a repo file can add a new environment but cannot loosen one. It cannot extend the `match` lists of an environment the manifest defines, because that would route an unlisted target into a looser environment (amended 2026-10-28, ruling S1). A new environment brings its own `match` (rule `environments.*.match: TrustedAdd()`, no `Union` on `match.*`). A missing `apply`/`destroy`/`migrate` decodes as the rule's Default, which is the strictest value (`deny`). Every key the repo may set has its own merge rule; unruled keys are trusted-layer only;
  - pack approvals (prod apply/destroy, migrations, promotions) are answered from the human's terminal only: TTY plus a typed binding, with the answer signed (Ed25519) by a key in the OS keychain. The board shows the decision and the command, and it refuses the answer until WebAuthn lands (amended 2026-10-28, security review of F; signature scheme amended 2026-10-29);
  - for a policy value, the stricter wins (`deny` > `approve` > `ask` > `auto`);
  - for `approval_ttl` and `freshness`, the minimum wins;
  - `default_environment` and `accounts` are honoured from the manifest only.

```yaml
packs:
  cloud:
    enabled: true
    tools: [checkov, conftest, tflint]        # declared => required DoD checks
    iac:                                      # where IaC lives; kind drives readers and gates
      - {path: infra/terraform, kind: terraform}      # terraform | tofu | bicep | arm | cdk | cloudformation | pulumi
      - {path: infra/bicep, kind: bicep}
    accounts: [111111111111]                  # AWS accounts that are "ours" (cross-account trust rule)
    tenants: [00000000-0000-0000-0000-000000000000]
    conftest: {policy: [policy/]}             # extra project Rego, added to the shipped bundle
    baseline: .tq/iac-baseline.json           # framework key (spec C §2.6a): ratchet for external-tool findings
    require_plan: false                       # true: an IaC change without a recorded plan fails DoD
    default_environment: prod                 # what an unresolved target counts as (default: strictest)
    environments:
      dev:
        match:                                # any match selects the environment
          workspaces: [dev]                   # terraform/tofu workspace (TF_WORKSPACE or .terraform/environment)
          dirs: [infra/envs/dev]              # -chdir / cwd
          var_files: [envs/dev.tfvars]
          stacks: [acme-dev]                  # pulumi stack, cdk stack, cfn stack name
          resource_groups: [rg-acme-dev]
          subscriptions: [11111111-1111-1111-1111-111111111111]
          accounts: [111111111111]
          profiles: [acme-dev]                # AWS_PROFILE / --profile
          dotenv: [.env.dev]                  # tq dotenv run --file
        apply: auto                           # auto | ask | approve | deny
        destroy: ask
      prod:
        match: {workspaces: [prod], dirs: [infra/envs/prod], subscriptions: [22222222-2222-2222-2222-222222222222]}
        apply: approve
        destroy: deny
        approval_ttl: 60m                     # default 60m, max 24h; single-use
        require_merged: true                  # default true for prod: the plan's commit must be on the default branch
  db:
    enabled: true
    tools: [squawk, pg_prove]
    migrations:
      - {path: supabase/migrations, engine: postgres, migrator: supabase}   # migrator: supabase|prisma|drizzle|golang-migrate|dbmate|flyway|sqitch|alembic|efcore|dacpac|raw
      - {path: db/mssql, engine: sqlserver, migrator: flyway}
    small_tables: [public.feature_flags]      # lock rules downgrade to warn for these
    rls:
      exposed_schemas: [public]               # default for Supabase: from supabase/config.toml [api] schemas, else [public]
      tests: supabase/tests/database          # pgTAP files
    test_db:                                  # local or ephemeral only (§12)
      url_env: TEST_DATABASE_URL              # variable NAME, resolved through tq dotenv
      start: supabase start                   # optional: command that brings it up
    drill:
      - name: main
        engine: postgres
        source: {kind: file, path_env: BACKUP_DUMP_PATH}   # file | supabase-dump | azure-sql-bacpac | command
        verify: [public.orders, public.customers]          # tables whose row counts are recorded
        freshness: 30d
    environments:                             # same shape as cloud; match adds hosts / project_refs / servers
      dev:  {match: {dotenv: [.env], hosts: [localhost]}, migrate: auto}
      prod: {match: {dotenv: [.env.prod], project_refs: [abcdefghijklmnop], servers: [acme-sql.database.windows.net]}, migrate: approve}
```

Validation fails the manifest load, with the key path in the error, when:
- an environment has an unknown policy value, or a `match` key that is not listed above;
- the same subscription, account, stack or dotenv file selects two environments;
- `default_environment` names an environment that is not declared;
- `approval_ttl` is over 24h;
- a `drill.source.kind` or a `migrator` is unknown;
- `test_db` has neither `url_env` nor `start`.

A manifest change re-trusts the workspace as today (`tq allow`, human-only). Headless sessions write `docs/privacy-manifest-proposal.yaml` and never edit `.tentaqles.yaml`.

## 5. Check catalogue

Every check is registered through spec C's `internal/packs` registry. "Built-in" checks always run when they apply. "External" checks are required when they are declared in `tools:`.

| Check id | Kind | Applies when | Reads |
|---|---|---|---|
| `cloud/iac-policy` | built-in | a changed path is under `packs.cloud.iac[].path`, or a plan was recorded for the current IaC tree | recorded plan JSON, ARM JSON (from `bicep build`), CFN/CDK templates, Pulumi preview JSON (§7) |
| `cloud/iam` | built-in | same, plus `*.json` IAM policy files under IaC paths | same, plus standalone policy JSON (§8) |
| `cloud/plan-fresh` | built-in | an IaC path changed and `require_plan: true` | the plan records (§9.4): a plan whose `inputs_hash` matches the current IaC tree |
| `cloud/no-plan-files` | built-in | always, when the pack is enabled | `git ls-files` (via `Input.Git.ListFiles`): no tracked `*.tfplan`, `*.tfstate*`, `.terraform/` or drill dumps |
| `cloud/checkov`, `cloud/trivy`, `cloud/conftest`, `cloud/tflint`, `cloud/bicep-lint`, `cloud/cfn-lint` | external | declared, and the IaC kind matches | source dirs or the recorded plan JSON (§7.4) |
| `db/migration-lint` | built-in | a changed file is under `packs.db.migrations[].path` | added or changed migration files (§10) |
| `db/migration-rollback` | built-in | same | down/undo partner files, or `## Rollback` in the spec's migration plan |
| `db/rls` | built-in | a Postgres migration changed | all Postgres migrations, replayed in order (§11) |
| `db/rls-tests` | built-in | `db/rls` found tables in exposed schemas | `packs.db.rls.tests` (§12) |
| `db/squawk`, `db/sqlfluff` | external | declared, and the engine matches | changed migration files |
| `db/pgtap` | external (`pg_prove`) | declared, and a Postgres migration or test file changed | runs the RLS tests against `test_db` (§12) |
| `db/pgtap-supabase` | external (`supabase`) | the same, in Supabase mode (amended 2026-10-29) | runs the same tests through `supabase test db` (§12) |
| `db/restore-drill` | built-in | `packs.db.drill` declared, and a migration changed | `docs/ops/restore-drills.jsonl` (§13) |

Every result follows spec C §2.4: `ok | fail | skip`, `required`, a detail line, and findings as `file:line level rule message`. A finding at level `warn` never fails a check. External findings go through the framework baseline (`Input.Baseline`, spec C §2.6a). Built-in findings are suppressed in the source, never in the manifest:
- SQL: `-- tq:allow <rule>: <reason>` on the line before the statement.
- JSON plans and templates: a tag on the resource, `tq-allow = "IAM002: <reason>"`. *Assumption: the tag survives into the plan JSON under `change.after.tags`.*

A suppression without a reason is itself an `error` finding. Suppressions are counted through `packs.Count` (§19).

## 6. Shared model inside the cloud pack

The readers turn each input into one normalised list, so that the rules are written once:

```go
type Resource struct {
    Addr     string            // tf address, ARM resource id/name, CFN logical id, Pulumi URN
    Type     string            // provider type: aws_iam_policy, Microsoft.Authorization/roleAssignments, AWS::IAM::Role, aws:iam/policy:Policy
    Actions  []string          // create|update|delete|replace|no-op (plans/what-if/change sets); nil for plain templates
    After    map[string]any    // planned attributes (sensitive values already replaced by "[REDACTED:sensitive]")
    Env      string            // resolved environment, "" when reading source
    Pos      Pos               // file:line in the source JSON/YAML, or the plan file plus a JSON pointer
}
```

Readers: `tfplan` (Terraform and OpenTofu share a format: `resource_changes[].change.{actions,after,after_sensitive}`), `arm` (an ARM template's `resources[]`, nested deployments flattened), `whatif` (`changes[].{changeType,after}`), `cfn` (JSON or YAML templates with short-form tags kept as nodes; also `cdk.out/*.template.json`), `changeset` (`describe-change-set` JSON: `Changes[].ResourceChange.{Action,Replacement}`), and `pulumi` (preview JSON: `steps[].{op,urn,newState.inputs}`). Each reader is a pure function over bytes, tested against golden fixtures.

*Assumption (verify at build): the field names above, for the what-if `--result-format FullResourcePayloads` output, the change-set JSON and `pulumi preview --json`.*

## 7. `iac-policy`

### 7.1 How the inputs are produced

tq never runs a cloud call inside `tq pack check`, so the DoD stays offline. Inputs come from three places:

| Target | Offline input (always) | Plan input (when recorded) | Recorded by |
|---|---|---|---|
| Terraform / OpenTofu | external tools over the source dirs | `terraform show -json <saved plan>` | `tq pack cloud plan --env E -- terraform plan …` (§9.8) |
| Bicep / ARM | `az bicep build --stdout` or `bicep build --stdout` to ARM JSON (offline) | `az deployment {group|sub|mg} what-if --result-format FullResourcePayloads --no-pretty-print -o json` | `tq pack cloud plan --env E -- az deployment group what-if …` |
| CDK | `cdk.out/*.template.json` when present (synth is the project's step) | `cdk diff` is not read; a change set is | `tq pack cloud plan --env E -- cdk synth` records the assembly hash |
| CloudFormation | template files under the IaC path | `aws cloudformation create-change-set` then `describe-change-set -o json` | `tq pack cloud plan --env E -- aws cloudformation create-change-set …` |
| Pulumi | none built-in (the program is code) | `pulumi preview --json [--save-plan F]` | `tq pack cloud plan --env E -- pulumi preview …` |

`tq pack cloud plan` runs the command with the workspace's identity environment, the environment check of §9.6, and a timeout (default 10 min). It captures stdout to the plan store and writes a redacted JSON copy plus a summary record. The summary holds counts per action, the stateful deletes and replaces, and the inputs hash. The agent sees only the summary. *Assumption: `cdk synth` may need credentials for context lookups when `cdk.context.json` is missing; the doc says so, and tq does not try to avoid it.*

### 7.2 Built-in rules (pure Go, over `Resource`)

| Id | Rule | Level |
|---|---|---|
| `IAC001` | Delete or replace of a stateful resource (databases, buckets and storage accounts, key vaults and KMS keys, disks and volumes, DNS zones, Cosmos/Dynamo tables) | fail in an `approve` env, else warn |
| `IAC002` | Ingress from `0.0.0.0/0` or `::/0` to admin or database ports (22, 3389, 1433, 3306, 5432, 6379, 27017) | fail |
| `IAC003` | Public object storage: S3 public ACL or public-access-block disabled; storage account `allowBlobPublicAccess: true`; GCS `allUsers` | fail |
| `IAC004` | Encryption at rest switched off where the provider allows it (RDS/Azure SQL TDE/storage) | fail |
| `IAC005` | Database open to the internet: Azure SQL firewall `0.0.0.0–255.255.255.255`, `publicNetworkAccess: Enabled` on a prod database, RDS `publicly_accessible` | fail in prod, warn elsewhere |
| `IAC006` | Deletion protection off (RDS `deletion_protection`, key vault purge protection, a CFN `DeletionPolicy` other than Retain/Snapshot on a stateful resource) in prod | fail |
| `IAC007` | ARM/Bicep `--mode Complete` deployment, which deletes resources not in the template | treated as a destroy by the gate; warn in the lint |

### 7.3 External tools

They run through spec C's runner (`internal/packs/toolrun`: probe order, version and minimum, timeout with process-tree kill, no environment dump, `.env` never read). Each parser maps the tool's findings to `file:line rule level`.

| Check | Invocation | Parsed from |
|---|---|---|
| `cloud/checkov` | `checkov -d <path> -o json --quiet --compact` (source); `checkov -f <plan.json> --framework terraform_plan -o json` (plan) | `results.failed_checks[].{check_id,file_path,file_line_range}` |
| `cloud/trivy` | `trivy config --format json --exit-code 0 <path>` | `Results[].Misconfigurations[].{ID,Severity,CauseMetadata.StartLine}` |
| `cloud/conftest` | `conftest test --output json --policy <bundle> [--policy <project>] --namespace tq.<kind> <input>` | `[].failures[].msg`; the position comes from `metadata.addr` that our Rego emits |
| `cloud/tflint` | `tflint --format json --chdir <path>` | `issues[].{rule.name,range.filename,range.start.line}` |
| `cloud/bicep-lint` | `az bicep lint --file <f> --diagnostics-format sarif` | SARIF `runs[].results[]` |
| `cloud/cfn-lint` | `cfn-lint -f json <templates>` | `[].{Rule.Id,Filename,Location.Start.LineNumber}` |

*Assumption: every flag and output field in this table is to be verified against the current release of each tool at build time; the parsers are pinned by fixtures captured from a real run that the human makes once, after replacing any real names with `acme` and `tester`.*

Severity: Checkov's open-source output has no severity, so every failed check fails unless the baseline lists it. Trivy findings `LOW` and `MEDIUM` are warn, `HIGH` and `CRITICAL` fail.

The baseline is the framework's (spec C §2.6a). `packs.cloud.baseline` names the file; `tq pack baseline --pack cloud` writes it, and it is human-only. For IaC, an entry's `location` is the resource address. A matching finding is downgraded to warn, and a new one keeps its level. When the file grows, the result is the framework's `baseline-grew` finding and its `baseline_change` flag, which the cloud pack maps to `iac-reviewer`.

### 7.4 Rego bundle (written fresh)

The bundle is embedded under `internal/packs/cloud/policy/` and extracted to `StateDir()/policy/<tq-version>/` on first use. It is Rego v1 (`import rego.v1`, `deny contains msg if …`), and every message carries `{"addr": …, "rule": …}`. Namespaces: `tq.terraform` (plan JSON), `tq.arm`, `tq.cfn`. Rules: required tags (from `packs.cloud.tags` when set), diagnostic logging on, TLS 1.2 minimum, backup retention of at least 7 days on prod databases, no plain-HTTP listeners, key rotation on. Each rule has an `opa test` unit test with a fixture. *Assumption: the installed Conftest embeds OPA 1.x (Rego v1 by default); `tq pack doctor` checks `conftest --version` and fails the check with a clear message on an older one.*

## 8. IAM least-privilege lint (`cloud/iam`)

Sources:
- AWS: `aws_iam_policy.policy`, `aws_iam_role_policy`, `aws_iam_role.assume_role_policy`, `aws_iam_policy_document` (as rendered JSON in the plan), `AWS::IAM::{Policy,ManagedPolicy,Role}` documents, Pulumi `aws:iam/*` inputs, resource policies (`aws_s3_bucket_policy`, KMS, SQS, SNS) and standalone `*.json` with a `Statement` array under IaC paths.
- Azure: `azurerm_role_assignment` and `azurerm_role_definition` from the plan, and `Microsoft.Authorization/roleAssignments` and `roleDefinitions` from ARM or what-if.
- GCP, cheap and Terraform-plan only: `google_*_iam_member` and `google_*_iam_binding`.

The policy JSON inside a string attribute is decoded; a `Statement` that is an object or a list, and an `Action` that is a string or a list, are both handled.

| Id | Rule | Level |
|---|---|---|
| `IAM001` | `Allow` with `Action: "*"` or `"<service>:*"` | fail |
| `IAM002` | `Allow` with `Resource: "*"` on any action outside the read-only verbs (`Get*`, `List*`, `Describe*`, `Head*`), from an embedded verb list | fail; warn for read-only |
| `IAM003` | `Allow` with `NotAction` or `NotResource` | fail |
| `IAM004` | Managed admin policies attached: `AdministratorAccess`, `PowerUserAccess`, `IAMFullAccess` | fail |
| `IAM005` | Escalation actions on `*` or without conditions: `iam:PassRole`, `iam:CreatePolicyVersion`, `iam:SetDefaultPolicyVersion`, `iam:Attach*Policy`, `iam:Put*Policy`, `iam:UpdateAssumeRolePolicy`, `iam:CreateAccessKey`, `sts:AssumeRole` | fail |
| `IAM006` | A trust policy with `Principal: "*"` or `{"AWS": "*"}`; a principal in an account not in `packs.cloud.accounts` without an `sts:ExternalId` condition; an OIDC federated principal without an `aud` condition, or a GitHub `sub` condition that is missing or wildcarded at the repo level (`repo:*`, `repo:acme/*`) | fail |
| `IAM007` | A resource policy with `Principal: "*"` and no condition (public) | fail |
| `AZR001` | Assignment of Owner, Contributor, User Access Administrator or Role Based Access Control Administrator at management group or subscription scope | fail; warn at resource group scope |
| `AZR002` | A custom role with `actions: ["*"]` or `"<Provider>/*"`, or `assignableScopes` that include `/` or a whole subscription while the actions write | fail |
| `AZR003` | A custom role granting `Microsoft.Authorization/roleAssignments/write` or `roleDefinitions/write` | fail |
| `AZR004` | A role assignment to a principal in a tenant not in `packs.cloud.tenants` (cross-tenant), when the plan shows the tenant | warn |
| `GCP001` | `roles/owner` or `roles/editor` granted; any role granted to `allUsers` or `allAuthenticatedUsers` | fail |

Built-in role names and ids live in one table in the package. *Assumption: the Azure built-in role definition GUIDs (Owner `8e3af657-…`, Contributor `b24988ac-…`, User Access Administrator `18d7d88d-…`) are verified against the Azure docs at build time; both the name and the GUID form are matched.*

## 9. Apply and destroy gate

### 9.1 Detection

The new `internal/packs/cloud/gate` package parses a shell command into segments, using the guard's separators (`guard.sepPattern`), and recognises these forms:
- the tool word, bare, path-prefixed or quoted;
- the tool behind wrappers: `tq dotenv run [--file F]… --`, `npx`, `pnpm exec`, `yarn`, `bunx`, `env VAR=…`, PowerShell `&`;
- flags between the tool and the verb (the existing `iacFlags` shape).

| Tool | Apply | Destroy |
|---|---|---|
| `terraform`, `tofu` | `apply` | `destroy`, `apply -destroy`, or an `apply <plan>` whose recorded summary has a stateful delete or replace |
| `pulumi` | `up`, `update` | `destroy`, `stack rm` |
| `az` | `deployment {group,sub,mg,tenant} create`, `stack … create` | `deployment … create --mode Complete`, `deployment … delete`, `group delete` |
| `cdk`, `cdktf` | `deploy` | `destroy` |
| `aws` | `cloudformation deploy`, `create-stack`, `update-stack`, `execute-change-set` | `cloudformation delete-stack` |
| `sam` | `deploy` | `delete` |

A command that matches the existing `tq/iac-apply` or `tq/iac-destroy` regex but that the parser cannot read (an odd wrapper, `eval`, a script file) counts as an apply to the default environment, which means fail closed. Dry runs are not applies: `--dry-run`, `what-if`, `plan`, `preview`, `synth`, `diff`, and `deploy --no-execute-changeset` (that one creates a change set; it is recorded, not gated).

### 9.2 Resolving the environment

These signals are read in order, and the first environment whose `match` hits wins:
1. explicit flags: `-chdir=`, `-var-file=`, `--stack/-s`, `--resource-group/-g`, `--subscription`, `--profile`, `--stack-name`, `--context env=` (cdk);
2. a `tq dotenv run --file F` wrapper (`match.dotenv`);
3. the variables in the command prefix and in the hook's own environment: `TF_WORKSPACE`, `AWS_PROFILE`, `ARM_SUBSCRIPTION_ID`, `PULUMI_STACK` (names only; values are compared, never printed);
4. local files: `<dir>/.terraform/environment` (the selected Terraform workspace) and `Pulumi.yaml` plus the selected stack.

*Assumption: the location of the Pulumi selected-stack file is verified at build; if it is not stable, step 4 skips Pulumi and the flag is required.*

There is no network call and no subprocess. If two environments match, the stricter one wins. If none matches, the result is `default_environment`.

Step 4 reads its small local files through `GuardCtx.ReadLocal` (§22 A, adopted). That call is limited to the workspace root and the identity's config dirs, reads at most 64 KiB per file, and never reads `.env*`. A file that is missing or unreadable is simply no signal.

### 9.3 Policy values

| Value | Guard action | Notes |
|---|---|---|
| `auto` | allow | for dev sandboxes |
| `ask` | ask (rule `tq/iac-apply-env`) | unattended mode may skip it, as with any ask |
| `approve` | **deny** unless a valid approval matches (§9.4) | not skippable; the reason names the `tq pack cloud approve` command |
| `deny` | deny | the human runs it in their own terminal, outside Claude |

*Amended 2026-10-29 (guard modes, spec E; ruling after the final review; corrected after the r3 review):* in a workspace the human granted `relaxed` (`tq allow --mode <ws> relaxed`), or a custom table, the pack gates follow the mode: in relaxed, every pack guard outside the locked groups: cloud apply/destroy (`tq/iac-apply-env`), db migrate (`tq/db-migrate-env`), restore target (`tq/restore-target`), model promote (`tq/model-promote`), dbt prod target (`tq/dbt-prod-target`), dbt full refresh (`tq/dbt-full-refresh`), pipeline run (`tq/pipeline-run`), bq unbounded (`tq/bq-unbounded-query`), warehouse resize (`tq/warehouse-resize`) and warehouse query unbounded (`tq/warehouse-query-unbounded`) are skipped, their denies included, so no approval is required. Without the grant, relaxed runs at default. Only `rule` (or `deny`) keeps a gate: `ask` turns an unapproved prod apply or migration, which the gate denies, into a prompt the human can click through, with no approval checked, and `skip` removes it. One exception (since the r2-m2 fix): `tq/iac-apply-env` and `tq/db-migrate-env` supersede the core `tq/iac-apply` and `tq/remote-migration`, so when the mode lowers their verdict the `prod-changes` value lifts it back, up to the gate's own verdict; a custom table that skips `cloud-destructive` but keeps `prod-changes` at `rule` keeps those two gates. The locked groups (`tq/human-answer`, `tq/hub-store-write`, `tq/plan-read`, `tq/plan-secret-print`, `tq/iac-identity-drift`) hold in every mode.

The destroy policy applies whenever the command is a destroy (§9.1). When both apply, the stricter of `apply` and `destroy` wins.

Defaults when the manifest gives no value (decided at spec review):
- An environment named `prod` gets `apply: approve`, `destroy: deny` and `require_merged: true`.
- One named `staging` gets `apply: ask` and `destroy: approve`.
- Any other environment gets `apply: ask` and `destroy: ask`.

With `require_merged`, `tq pack cloud approve` refuses to ask unless the plan record's commit (`git rev-parse HEAD`, taken at plan time with a clean IaC path) is an ancestor of the default branch's remote ref.

### 9.4 Approval records

This flow is built on spec C §2.11 (`internal/packs/approval`: `Ask`, `Verify`, `MarkUsed`, the ledger at `StateDir()/ledger.jsonl`). The cloud pack adds plan records and the evidence it binds.

1. The agent, or the human, runs `tq pack cloud plan --env prod -- terraform plan -out=-`. tq gives the tool a plan path inside `StateDir()/plans/` (it rewrites `-out=-`, or injects `-out` when it is missing), runs it, and appends a **plan record** to `StateDir()/plans.jsonl`. This is a separate file from the framework's approval ledger. The record holds:
   - `id` and `env`;
   - `tool`, `dir`, and the Terraform workspace or stack;
   - `plan_sha256`;
   - `inputs_hash`: a git tree hash of the IaC path plus the var files;
   - `identity`: the subscription, account or profile resolved in §9.6;
   - `summary`: counts per action, stateful deletes and replaces;
   - `checks`: the `cloud/*` results over this plan;
   - `ts`.
2. `tq pack cloud approve <plan-id> [--destroy]` calls `approval.Ask` with `Request{Pack: "cloud", Action: "apply"|"destroy", Subject: <dir or stack>, Target: <env>, Options: ["apply","reject"], ValidFor: approval_ttl, CardID: $TQ_HUB_TASK card}`. Without a card, the framework adds a `chore` card in `todo` tagged `cloud-approval`.
   - **Evidence:** `{plan_sha256, inputs_hash, env, identity, plan_id}`.
   - **Summary:** the redacted plan summary, the failing checks (if any), the environment and the identity.
   - **If any check fails**, the request is still made, but the summary leads with the failures and the request offers only `reject`, unless the human passes `--allow-failing` from their own terminal.
   - *Amended 2026-10-29 (final review m8):* stricter in the code. With a failing check, `tq pack cloud approve` (and `tq pack db approve`) refuses to make the request at all unless `--allow-failing` is passed from the human's own terminal; no reject-only request is created.
3. The human answers with `tq hub answer <card> <decision>` in their own terminal. F's runner hunk ignores `permission` decisions whose `data.pack` is set, so they never resume or allow a run.
   - *Amended 2026-10-29 (final approval API, see spec C §2.11's amendment):* terminal only. The answer needs a TTY, refuses agent sessions and asks for the typed binding (pack, action, a 12-hex code of the subject); `--choice` alone is refused. The board shows the decision but refuses the answer. The answered line is signed with the workspace key in the OS keychain. The guard in step 4 matches the approval with `approval.FindGuard(g, Binding{Pack, Action, Subject, Target}, evidence)`, and a human-run apply follows FromCommand → HumanOnly → Verify → Confirm → Claim → run → MarkUsed → Comment.
4. At PreToolUse, the `tq/iac-apply-env` guard (§16) rebuilds the evidence from the command and calls `Verify`. The command must:
   - name a saved plan file inside `StateDir()/plans/` (bare `terraform apply` in an `approve` environment is denied, and the reason says to apply the saved plan);
   - have a plan file whose sha256 equals the evidence;
   - resolve to the same environment and identity;
   - run within `ValidFor`, on an approval that is not yet used.

   On a match the guard calls `MarkUsed` and allows the call. Terraform also refuses a stale saved plan on its own, which is a second guarantee. *Assumption: verify that OpenTofu behaves the same.*

   Hashing and answer lookup inside the guard bounds work as adopted in §22 B and C:
   - The guard hashes the plan with `GuardCtx.HashState(name)`: a streaming sha256 over a `StateDir()` file of at most 64 MiB, with a 40 ms budget. A file over the cap, or a hash that runs over the budget, is a **deny**. The reason tells the user to re-plan into a smaller plan or split the stack.
   - The hub's decision-answer handler writes the human's answer into `ledger.jsonl`, so `Verify` at guard time reads only the ledger.
   - As a note: `tq pack cloud plan` also records the file's size and modification time, as a cheap first check before hashing.

### 9.5 What "the plan" is, per tool

| Tool | Bound artifact | Apply form required in `approve` envs |
|---|---|---|
| Terraform / OpenTofu | the saved plan file | `apply <planfile>` |
| Pulumi | `preview --save-plan F` output, or, when update plans are unavailable, the preview JSON hash plus the program's tree hash and `Pulumi.<stack>.yaml` (decided: this weaker binding is accepted, and Pulumi is not denied) | `up --plan F`, or with the fallback `up --yes --stack S` with an unchanged tree. The doc says the fallback binding is weaker. *Assumption: update plans may still be behind `PULUMI_EXPERIMENTAL`.* |
| az deployment | the template and parameter file hashes, the scope, and the what-if JSON hash | `create` with the same `--template-file` / `--parameters` files (hashes recomputed) and the same scope |
| CDK | the `cdk.out` assembly tree hash plus **one stack name**: a multi-stack deploy needs one approval per stack (decided) | `deploy --app cdk.out <stack>` (pre-synthesised), one stack per command. `--all` and a wildcard are denied in `approve` envs. *Assumption: `--app` accepts the directory.* |
| CloudFormation | the change-set name, stack and template hash | `execute-change-set --change-set-name N --stack-name S`; a direct `deploy`, `create-stack` or `update-stack` is denied, and the reason says to create a change set |

### 9.6 Identity drift

This check runs before any policy value, for every environment that declares `match.subscriptions`, `accounts` or `profiles`, and for every identity with `expected_subscription`. It reads only local state, through `GuardCtx.ReadLocal` (§22 A).
- Azure: the default subscription in `$AZURE_CONFIG_DIR/azureProfile.json` (BOM-tolerant), or `--subscription`, or `ARM_SUBSCRIPTION_ID`. *Assumption: the `subscriptions[].{id,isDefault}` layout is verified at build.*
- AWS: `--profile` or `AWS_PROFILE`, compared with `match.profiles` and `identities.aws.profile`. The account comes from the plan record, which was resolved at plan time with `aws sts get-caller-identity` (a network call that is fine there, never in the hook).
- Pulumi and CDK: the stack or profile flags.

A mismatch with the resolved environment, or with the manifest's expectation, is a deny under `tq/iac-identity-drift`. The reason gives both values by name (subscription id or profile) and points to `tq doctor --verify`. The existing `wrong-cloud` rule still blocks the wrong provider's CLI first.

### 9.7 Relation to the existing rules

The gate's guard registers with `Supersedes: ["tq/iac-apply", "tq/iac-destroy", "tq/cloud-delete"]` (spec C §2.10). When it returns `handled` for a call, F's wiring drops those core matches before merging, so an `auto` environment really allows the call. The cloud pack returns `handled` for `tq/cloud-delete` only on CloudFormation and az deployment deletes. Without the pack, or for calls the gate does not handle (kubectl, helm, gcloud deployment-manager), the regex rules apply unchanged.

### 9.8 Commands (registered through `Pack.Commands`, mounted under `tq pack cloud`)

| Command | Who | Does |
|---|---|---|
| `tq pack cloud plan --env E [--timeout 10m] -- <plan cmd>` | agent or human | runs the plan with the identity checks, stores the plan (0600), appends the plan record, prints the summary only |
| `tq pack cloud show <plan-id> [--json]` | agent or human | the redacted summary and the check results; never the raw plan |
| `tq pack cloud approve <plan-id> [--destroy]` | agent or human (it only *asks*) | `approval.Ask` (§9.4) |
| `tq pack cloud prune [--older 7d]` | anyone | deletes stored plans past age; plan records and ledger lines are kept |

Framework commands cover the rest: `tq pack approvals --pack cloud` (pending, answered, used and expired) and `tq pack baseline --pack cloud` (human-only).

## 10. Migration SQL lint (`db/migration-lint`)

**Lexer** (`internal/packs/db/sqllex`): comments (`--`, nested `/* */`), single-quoted strings, quoted identifiers (`"x"` and `[x]`), Postgres dollar quoting (`$tag$ … $tag$`), statement splitting on `;`, and for T-SQL on `GO` lines. Each statement keeps its file and line. The rules match token patterns per statement; there is no full grammar. The lexer replays the migrations in order to know which tables already exist: a table created in the same file, or by an earlier migration in this diff that has not been applied yet, is new, and lock rules skip new tables. Partitioned tables and `small_tables` downgrade the lock rules to warn.

Postgres (`engine: postgres`):

| Id | Rule | Fix the finding points to |
|---|---|---|
| `PG001` | `CREATE INDEX` without `CONCURRENTLY` on an existing table | `CREATE INDEX CONCURRENTLY`, outside a transaction |
| `PG002` | `ADD COLUMN … NOT NULL` without `DEFAULT` on an existing table | add it nullable, backfill, `CHECK … NOT VALID`, `VALIDATE`, then `SET NOT NULL` |
| `PG003` | `ADD COLUMN` with a volatile default (`now()`, `random()`, `gen_random_uuid()`, `clock_timestamp()`) on an existing table | add without the default, then set the default for new rows only |
| `PG004` | `ALTER COLUMN … TYPE` on an existing table (a rewrite), except the known safe widenings (`varchar(n)` to larger or to `text`) | a new column, expand and contract |
| `PG005` | `SET NOT NULL` with no validated `CHECK (col IS NOT NULL)` earlier | the check-then-set sequence |
| `PG006` | `ADD CONSTRAINT … FOREIGN KEY` or `CHECK` without `NOT VALID` on an existing table | `NOT VALID`, then `VALIDATE CONSTRAINT` in a later statement |
| `PG007` | `ADD … UNIQUE` or `PRIMARY KEY` not `USING INDEX` | a concurrent unique index, then `USING INDEX` |
| `PG008` | `DROP COLUMN`, `DROP TABLE`, `RENAME COLUMN`, `RENAME TO` with no `-- tq:contract <expand ref>` marker | expand and contract (`db-migration` §3) |
| `PG009` | a file with any locking statement and no `SET lock_timeout` | `SET lock_timeout = '5s'` at the top |
| `PG010` | `CONCURRENTLY` in a file that also has `BEGIN`, or that the migrator wraps in a transaction (per `migrator`) | the migrator's no-transaction marker |
| `PG011` | `UPDATE` or `DELETE` with no `WHERE` in a schema migration | a batched backfill job |
| `PG012` | `VACUUM FULL`, `CLUSTER`, `LOCK TABLE`, `REINDEX` without `CONCURRENTLY` | an online alternative |

SQL Server / Azure SQL (`engine: sqlserver`):

| Id | Rule |
|---|---|
| `MS001` | `CREATE INDEX` or `ALTER INDEX … REBUILD` on an existing table without `ONLINE = ON` (a warn when `packs.db.edition` says the edition has no online operations) |
| `MS002` | `ADD <col> … NOT NULL` without `DEFAULT` on an existing table |
| `MS003` | `ALTER COLUMN` that changes the type or shrinks the size (a size-of-data operation) |
| `MS004` | `ADD CONSTRAINT` FK or CHECK without `WITH NOCHECK` on an existing table (then `CHECK CONSTRAINT` in a later step) |
| `MS005` | `DROP COLUMN`, `DROP TABLE`, `sp_rename` without the `tq:contract` marker |
| `MS006` | a file with a locking statement and no `SET LOCK_TIMEOUT` |
| `MS007` | `UPDATE` or `DELETE` without `WHERE` |
| `MS008` | DDL inside `BEGIN TRAN` that spans a `GO` (a batch boundary, almost always a mistake) |

`db/migration-rollback`:
- For paired migrators it requires the partner: golang-migrate `*.down.sql`, dbmate `-- migrate:down`, Flyway `U<ver>__*.sql` (warn only: undo is a paid Flyway feature), sqitch `revert/`, alembic `downgrade()` with a body other than `pass`, EF Core `Down()` with statements.
- For forward-only migrators (supabase, prisma, drizzle, dacpac) it requires a `### Rollback` heading under the spec's `## Migration plan`, which is the same file the existing DoD `migration` item reads.

**External tools:** `squawk --reporter json <files>` (Postgres) and `sqlfluff lint --format json --dialect postgres|tsql <files>`. Squawk findings fail. Sqlfluff findings are style warnings unless `packs.db.sqlfluff_fail: true`. *Assumption: the squawk JSON reporter flag and fields are verified at build.*

## 11. RLS gate (`db/rls`, Postgres and Supabase)

The pack replays every migration under the Postgres paths in order and builds the state of each table in the exposed schemas: whether RLS is enabled or forced, its policies per command and role, its grants, and the views and functions in those schemas. It then judges the tables that the diff creates or changes, and every existing table when `--all` is passed.

| Id | Rule | Level |
|---|---|---|
| `RLS001` | A new table in an exposed schema without `ENABLE ROW LEVEL SECURITY` in the same migration | fail |
| `RLS002` | RLS enabled with no policy, unless the line is marked `-- tq:rls-deny-all <reason>` (service-only tables) | fail |
| `RLS003` | `DISABLE ROW LEVEL SECURITY`, `NO FORCE ROW LEVEL SECURITY`, or `DROP POLICY` with no replacement in the same migration | fail |
| `RLS004` | A policy for `anon` or `authenticated` with `USING (true)` or `WITH CHECK (true)` on `INSERT`, `UPDATE` or `DELETE` | fail; warn on `SELECT` |
| `RLS005` | A policy that reads `auth.jwt() -> 'user_metadata'` or `raw_user_meta_data` (users can edit these) | fail |
| `RLS006` | `GRANT ALL` or a write grant to `anon`; `GRANT … TO public` in an exposed schema; `ALTER DEFAULT PRIVILEGES` that grants to `anon` | fail |
| `RLS007` | A view in an exposed schema without `WITH (security_invoker = true)`, which bypasses RLS | fail |
| `RLS008` | A `SECURITY DEFINER` function in an exposed schema without `SET search_path`, or one that `anon` can execute without a stated reason | fail |
| `RLS009` | `auth.uid()` called per row instead of `(select auth.uid())` in a policy | warn (performance) |
| `RLS010` | A `service_role` reference in a policy (it bypasses RLS anyway, so the policy is misleading) | warn |

Supabase specifics: the exposed schemas come from `supabase/config.toml` `[api] schemas`, read with a tiny line reader (no TOML dependency). *Assumption: the key name and the `extra_search_path` semantics are verified at build.* The `storage` schema's `objects` policies are judged like tables when migrations touch them. The service-role key handling stays in `auth-security` and the existing `jev/service-role-client` judgment rule.

## 12. pgTAP RLS tests

**Template:** the `rls-tests` skill writes `templates/rls_<table>.test.sql`, fresh, to `packs.db.rls.tests`. One file per table:

```sql
begin;
select plan(6);
-- two synthetic users; never real data
select tests_tq.create_user('tester-a'), tests_tq.create_user('tester-b');
select tests_tq.as_service();  insert into public.notes(owner_id, body) values (tests_tq.uid('tester-a'), 'a'), (tests_tq.uid('tester-b'), 'b');
select tests_tq.as_user('tester-a');
select results_eq('select count(*)::int from public.notes', array[1], 'a sees only own rows');
select throws_ok($$insert into public.notes(owner_id, body) values (tests_tq.uid('tester-b'), 'x')$$, '42501', null, 'a cannot write as b');
select is_empty($$update public.notes set body = 'y' where owner_id = tests_tq.uid('tester-b') returning 1$$, 'a cannot update b');
select tests_tq.as_anon();
select is_empty('select 1 from public.notes', 'anon sees nothing');
select tests_tq.as_service(); select isnt_empty('select 1 from public.notes', 'service sees all');
select ok((select relrowsecurity from pg_class where oid = 'public.notes'::regclass), 'rls enabled');
select * from finish();
rollback;
```

`tests_tq` is a small helper schema, also written fresh. It is installed by a test-only migration under the tests directory, never under the app's migrations. It provides `create_user` (it inserts into `auth.users` on Supabase, or a stub `auth.users` and an `auth.uid()` reading `request.jwt.claims` on plain Postgres), `as_user` (`set local role authenticated` plus `set_config('request.jwt.claims', …, true)`), `as_anon` and `as_service`.

**How the tests run** (`db/pgtap`):
1. The target is `packs.db.test_db`. tq resolves `url_env` through `tq dotenv` (the value is never printed), and the host must be `localhost`, `127.0.0.1`, `::1`, a Unix socket, or a docker-compose service name listed in `test_db.hosts`. Anything else makes the check `fail` with "test_db is not local", before anything connects.
2. If `start` is set and the database does not answer a 2 s TCP probe, tq runs `start` with a timeout (default 180 s).
3. Supabase: `supabase test db`. Others: `pg_prove --ext .sql -r <tests dir>` with `PG*` variables built from the URL in the child's environment only. The TAP output is parsed for `not ok` lines, and each maps to `file:test-number`. *Assumption: `supabase test db` runs the files in `supabase/tests` against the local stack; verify the path and the exit codes.*

**Two check ids** (amended 2026-10-29, final review M4). The tests run as `db/pgtap` (pg_prove) or as `db/pgtap-supabase` (`supabase test db`). Supabase mode (`supabaseMode`) applies when `supabase` is declared in `tools`, or when a Postgres migrations entry uses the `supabase` migrator and `pg_prove` is not declared; otherwise the check is `db/pgtap`. The DoD item is `pack:db/pgtap` or `pack:db/pgtap-supabase` accordingly.

**DoD:** the pgTAP check (`db/pgtap` or `db/pgtap-supabase`) is required when `pg_prove` (or `supabase`) is declared in `tools`. `db/rls-tests`, built-in, always checks the coverage: every table in an exposed schema with RLS needs at least one test file that names it (word match). It fails with the list of untested tables and the skill command that writes them.

## 13. Backup and restore drill

**Skill `restore-drill`, command `tq pack db drill <name> [--keep]`, check `db/restore-drill`.**

1. **Source:** read-only.
   - `file`: a dump path from `path_env`.
   - `supabase-dump`: `supabase db dump` against the linked project. This is a read; the `--linked` target is resolved and must match an environment.
   - `azure-sql-bacpac`: an exported `.bacpac` path.
   - `command`: the project's own fetch command, run with a timeout.

   The dump lands in the db pack's `StateDir()/drills/tmp/` (0600) and is deleted after the drill, kept only with `--keep`.
2. **Target:** a scratch database on the local `test_db` server, named `tq_drill_<name>_<ts>`. The guard (§14) and the command itself refuse any non-local target. The drill never writes to a database that matches a declared environment.
3. **Restore:**
   - Postgres: `pg_restore --no-owner --no-acl -d <scratch>`, or `psql -f` for a plain dump.
   - SQL Server: `sqlpackage /a:Import` for a bacpac, or `RESTORE DATABASE … WITH MOVE` for a `.bak` on a local container.

   The restore runs with a timeout (`drill.timeout`, default 30 min).
4. **Verify:**
   - the migration version table matches the newest migration file, or the gap is recorded;
   - `select count(*)` for each table in `verify`;
   - a tq-chosen query per engine that proves the schema loaded (number of tables and indexes).

   **Only numbers and names are recorded**, never row content.
5. **Evidence:** one JSON line appended to `docs/ops/restore-drills.jsonl`, committed with the work, holding:
   - `name` and `engine`;
   - `backup_taken_at` (from the dump header, or the file's modified time);
   - `restored_at`, `duration_s` and `ok`;
   - `tables`, as name and count;
   - `migration_version`;
   - `tool_versions`;
   - `host_kind: local`.

   The scratch database is then dropped.
6. **Check:** `db/restore-drill` passes when the newest `ok` line for each declared drill is within `freshness`, and the backup it restored was at most `freshness` old when it was restored. Otherwise it fails with the age and the command to run.

## 14. Database environment gate

`tq/remote-migration` keeps its regex. When `packs.db.environments` exists, a decision function (`tq/db-migrate-env`) resolves the target in this order:
1. `--db-url` or `--connection` flags (the host is parsed, never printed);
2. a `tq dotenv run --file F` wrapper (`match.dotenv`);
3. `supabase db push` and `migration up` read the linked project ref from `supabase/.temp/project-ref`. *Assumption: verify that path.* The file is read through `GuardCtx.ReadLocal` (§22 A).
4. a server named in a `sqlcmd -S` flag or a `Server=` fragment on the command line.

There is no match when none of these is present, and the gate never reads `.env` itself. Policy values are the same as §9.3, with `migrate:` in place of `apply:`.

For `approve`, `tq pack db approve --env prod` calls `approval.Ask` with `Request{Pack: "db", Action: "migrate", Target: <env>, Options: ["apply","reject"], ValidFor: approval_ttl}` (60 min by default, 24 h at most). Without a run card, the framework adds a card tagged `db-approval`.
- **Evidence:** the git tree hash of the migrations path, the hash of the `db/*` results, and the newest drill line. The request is refused when the drill is stale, unless the human passes `--no-drill`.
- **At PreToolUse:** `tq pack db approve` stores the migrations tree hash in `StateDir()`. The guard compares it with the hash that the `db/*` checks computed. It can't recompute a tree hash itself, because that would need git, which is a process. `Verify` then checks the answer and the single use, and `MarkUsed` records it.

New rule `tq/restore-target` (decision function, deny): `pg_restore`, `psql -f <dump>`, `sqlpackage /a:Import`, `RESTORE DATABASE` or `supabase db reset --linked` against a target that resolves to a declared environment, or to a non-local host. `supabase db reset` without `--linked` is local and allowed.

## 15. Triage flags and reviewers

There are new flags that need no Jev. Each is registered as a `packs.Flag{Name, Reviewers, Raise}` through `RegisterPack` (spec C §2.9). `Raise` looks only at paths and config. The flags force `high`, as `classified_data` does today, and `tq pack crew` prints the reviewers:

| Flag | Raised when | Selects |
|---|---|---|
| `iac` | a changed path is under `packs.cloud.iac[].path`, or matches `*.tf`, `*.bicep`, `cdk.json`, `Pulumi*.yaml` or `template.ya?ml` with `AWSTemplateFormatVersion` | `iac-reviewer` |
| `iam` | a changed path is an IAM policy JSON under an IaC path, or its name matches `*iam*`, `*role*` or `*policy*` under an IaC path (the content-based finding comes from `cloud/iam` and the reviewer reads it) | `iac-reviewer` and `security-reviewer` |
| `db_migration` | a changed path is under `packs.db.migrations[].path` or matches `MIGRATION_DIR_RE` | `db-reviewer` |
| `rls_change` | a changed Postgres migration or pgTAP test, or `supabase/config.toml` (whether the content touches policies is judged by `db/rls` and the Jev `rls` flag) | `db-reviewer` and `security-reviewer` |

The existing Jev flags `schema`, `migration`, `rls` and `data_loss` select `db-reviewer`, and `destructive` selects `iac-reviewer` when the `iac` flag is also raised. This closes today's gap, where those flags selected nobody. The mapping is registered as pack `Flag`s that have a core flag name and no `Raise` (§22 D, adopted).

Both agents match the gate's `*-reviewer` rule (spec C §2.8), so `REVIEWERS` in `gate.py` is not edited.

**Agents** (`plugin/agents/`, read-only: tools Read, Glob, Grep; the same format and answer-key output as `security-reviewer`):

| Agent | Checklist | Judges |
|---|---|---|
| `iac-reviewer` | `skills/iac-policy/references/checklist.md` (rows `I1`…`I12`) | blast radius per environment, stateful resources and their deletion protection, network exposure, IAM against the least-privilege rows, the identity and environment separation (dev resources never in the prod state), secrets in variables or outputs (`sensitive = true`), and whether the plan summary in the bundle matches the intent of the spec. It reads the recorded summary and the `cloud/*` results, never a raw plan. |
| `db-reviewer` | `skills/db-migration/references/review-checklist.md` (rows `D1`…`D12`, new) | lock safety, expand and contract across releases, rollback, backfill batching, index justification (links `index-advisor`), RLS and grants, pgTAP coverage, deploy order against the code, and the restore drill's freshness for risky changes |

## 16. Guard rules (new and extended)

All are fail-closed: a panic or a read error is a deny. They are bounded as spec C §2.14 requires (under 50 ms, no process, no network) and active only when their pack is enabled. The ids are stable, and `guard.disable` and `guard.actions` apply to them (spec C §2.10).
- Decision guards register through `packs.RegisterGuard` from `internal/cmd/claudehook_iac.go` and `internal/cmd/claudehook_db.go`. The guard bodies live in the packs' `gate/` packages.
- Static rules go in `internal/policy/builtin_packs.go`, with `Pack` set. F creates that file empty, and D-GR fills it.

| Rule | Kind | Action | What |
|---|---|---|---|
| `tq/iac-apply-env` | decision guard (cloud), `Supersedes: [tq/iac-apply, tq/iac-destroy, tq/cloud-delete]` | auto / ask / deny per §9.3 | an apply or destroy to a resolved environment |
| `tq/iac-identity-drift` | decision guard (cloud) | deny | the target's subscription, account or profile disagrees with the environment or the manifest (§9.6) |
| `tq/db-migrate-env` | decision guard (db), `Supersedes: [tq/remote-migration]` when handled | per §14 | extends `tq/remote-migration` |
| `tq/restore-target` | decision guard (db) | deny | a restore into a declared environment or a non-local host |
| `tq/plan-read` | static (cloud) | deny | Read, Grep or a shell print of `*.tfplan`, `*.tfstate*`, `$TQ_HOME/packs/*/cloud/plans/**`, `cdk.out/**/*.assets.json` |
| `tq/plan-print` | static (cloud) | ask | `terraform show [-json]`, `tofu show`, `terraform output [-json]`, or `pulumi stack output --show-secrets` written to the transcript; the reason points to `tq pack cloud show` |
| `tq/plan-commit` | static (cloud) | deny | `git add` naming `*.tfplan`, `*.tfstate*`, `.terraform/`, a path under `$TQ_HOME/packs/` or a drill dump. A staged-name scan would need git, which a pack guard may not run, so `git add -A` and `git add .` are covered by `.gitignore` and by the `cloud/no-plan-files` check (§5). |
| `tq/hub-store-write` | static, two entries with the same id, one per pack (`Pack: cloud` and `Pack: db`), so it is active when either pack is enabled; `tq/guard-file-write` already uses one id twice | deny | a shell write (`sqlite3`, redirect, `cp` or `mv` destination) or an Edit/Write targeting `$TQ_HOME/hub/**` or `$TQ_HOME/packs/**`. It protects the approvals and plans; the key-isolation caveat applies. *Amended 2026-10-29 (integration round 3):* the two pack entries are dropped. The framework's core `tq/hub-store-write` (no `Pack`, always active) and its hook-time state-dir check deny every form they denied, under every `$TQ_HOME` spelling. |

`tq/iac-apply-env` in deny form and `tq/iac-identity-drift` are denies, so `guard.unattended` never skips them. *Amended 2026-10-29 (guard modes, spec E; ruling after the final review; corrected after the r3 review):* in a workspace the human granted `relaxed` (`tq allow --mode <ws> relaxed`), or a custom table, the pack gates follow the mode: in relaxed, every pack guard outside the locked groups: cloud apply/destroy (`tq/iac-apply-env`), db migrate (`tq/db-migrate-env`), restore target (`tq/restore-target`), model promote (`tq/model-promote`), dbt prod target (`tq/dbt-prod-target`), dbt full refresh (`tq/dbt-full-refresh`), pipeline run (`tq/pipeline-run`), bq unbounded (`tq/bq-unbounded-query`), warehouse resize (`tq/warehouse-resize`) and warehouse query unbounded (`tq/warehouse-query-unbounded`) are skipped, their denies included, so no approval is required. Without the grant, relaxed runs at default. Only `rule` (or `deny`) keeps a gate: `ask` turns an unapproved prod apply or migration, which the gate denies, into a prompt the human can click through, with no approval checked, and `skip` removes it. One exception (since the r2-m2 fix): `tq/iac-apply-env` and `tq/db-migrate-env` supersede the core `tq/iac-apply` and `tq/remote-migration`, so when the mode lowers their verdict the `prod-changes` value lifts it back, up to the gate's own verdict; a custom table that skips `cloud-destructive` but keeps `prod-changes` at `rule` keeps those two gates. The locked groups (`tq/human-answer`, `tq/hub-store-write`, `tq/plan-read`, `tq/plan-secret-print`, `tq/iac-identity-drift`) hold in every mode. `tq/hub-store-write` goes on a new never-disable list in `internal/guardcfg`, which is spec D's own hunk (decided: the manifest can't disable it). That hunk is needed because pack guards honour `guard.disable` by default.

## 17. Skills and templates

| Skill | Trigger | Output |
|---|---|---|
| `iac-policy` (new) | "check my terraform", "is this IaC safe", an IaC diff, `/tentaqles:feature` with `iac` | runs `tq pack check --pack cloud`, explains findings by rule id, writes fixes; the plan and approve flow (`tq pack cloud plan/approve`, `tq pack approvals --pack cloud`); never answers |
| `rls-tests` (new) | a new table in an exposed schema, "test RLS", `db/rls-tests` failing | pgTAP files from the template, the `tests_tq` helper, runs `db/pgtap` |
| `restore-drill` (new) | "test the backup", "restore drill", `db/restore-drill` stale | the drill run, the evidence line, guidance for each source kind |
| `db-migration` (changed) | existing | links §4 to §7 to rule ids `PG*`/`MS*`, adds the `tq:contract` marker convention and the `### Rollback` subsection, and the `review-checklist.md` reference |
| `auth-security` (changed) | existing | its Supabase RLS rows point to `db/rls` and `rls-tests` |
| `ci-pipeline` (changed) | existing | an optional CI step `tq pack check --base origin/<default> --json` with the declared tools installed; the plan stays the project's step |
| `supply-chain` (changed) | existing | `.gitignore` rows for `*.tfplan`, `*.tfstate*`, `.terraform/`, `cdk.out/` |

Docs go in `docs/packs/cloud-iac.md`, `docs/packs/cloud-apply-gate.md`, `docs/packs/db-migrations.md`, `docs/packs/db-rls.md` and `docs/packs/db-restore-drill.md`, which F's `docs/PACKS.md` index links (spec C §2.12).

Templates (fresh, MIT): `templates/packs/cloud/policy-example.rego`, `templates/packs/db/rls_table.test.sql`, `templates/packs/db/tests_tq_helpers.sql`, `templates/packs/db/restore-drills.jsonl` (empty, with a header comment in the doc, not the file), the manifest snippets of §4, and `iac-policy/references/checklist.md` and `db-migration/references/review-checklist.md`. Nothing comes from `sources/`.

## 18. On-ramps

No on-ramp `SKILL.md` is edited. F's generic step reads `plugin/skills/pack/references/onramps/<pack>-*.md` for each enabled pack (spec C §6). The cloud pack writes `cloud-iac.md` and `cloud-apply.md`; the db pack writes `db-migrations.md`, `db-rls.md` and `db-drill.md`. Each file has one section per on-ramp, with this content:

| On-ramp | Change |
|---|---|
| `kickoff` | When the stack ADR names Terraform/OpenTofu, Bicep, CDK, CloudFormation, Pulumi, Postgres/Supabase or SQL Server, it proposes the `packs.cloud` / `packs.db` block (environments with `prod: approve`, the tools found on PATH) at human stop 1, adds the `.gitignore` rows, and plans the first `restore-drill` and the `tests_tq` helper as checkpoint items. Headless: the proposal file, never the manifest. |
| `feature` | The expected crew comes from `tq pack crew` (`iac-reviewer`, `db-reviewer`). The file adds acceptance checks: "the dev plan shows no stateful delete", "every new exposed table has RLS and a pgTAP file". |
| `fix` | The regression test may be a pgTAP test or a fixture plan JSON. `--hotfix` skips the critic, never the apply approval: a hotfix to prod is still `approve`. |
| `dod` | It lists `pack:cloud/*` and `pack:db/*` items, with the plan summary id when one exists. |
| `ship` | The PR body gains a "Packs" section: check ids and results, the plan summary (counts, stateful changes, environment) and the drill age. It never includes a plan or values. |

## 19. `tq doctor`, `tq pack doctor`, `tq insights`

`tq pack doctor` and `tq doctor` (spec C §2.13) call each pack's `Pack.Doctor`, which lives in `internal/packs/{cloud,db}/doctor.go`. The codes are `cloud-*` and `db-*`, and the checks are:
- the declared tools: found, version, minimum version (Conftest/OPA 1.x);
- each environment's `match` is resolvable, with no overlaps;
- the plan store is 0600 and outside the repository;
- `*.tfstate`, `*.tfplan` and `.terraform/` are gitignored, and none is tracked (`git ls-files`, local);
- the Supabase linked ref belongs to a declared environment;
- `test_db` is local and answers;
- each drill's age against its freshness.

The "IaC or migrations exist but the pack is off" hint is F's `packs-suggest` code. The packs supply the detection hints: `*.tf`, `*.bicep`, `cdk.json`, `Pulumi.yaml`, CloudFormation templates, and the migration directories of `MIGRATION_DIR_RE`. They are registered through `Pack.Hints` (§22 E).

`tq insights` needs no edit. F's Packs section already shows the check pass and fail counts, the declared-missing count, the guard hits and the approvals (asked, approved, rejected, expired, median time to answer). The packs add these counters through `packs.Count`, which hold names only and never paths or values:

| Counter | Meaning |
|---|---|
| `cloud: apply_denied:<reason>` | an apply was denied; the reason is `no-approval`, `hash-mismatch`, `expired`, `used`, `identity-drift` or `unresolved-env` |
| `cloud: apply_allowed:<policy>` | an apply was allowed, by the policy value that allowed it |
| `cloud: suppression:<rule>` and `db: suppression:<rule>` | a `tq:allow` suppression |
| `db: migrate_denied:<reason>` | a migration was denied |
| `db: drill:ok` and `db: drill:fail` | a drill outcome |
| `db: drill_age_days:<name>` | the age of a drill, as a gauge written at check time |

## 20. Security

- **Credentials:** tq never reads `.env`. Connection strings and tokens reach the tools only through `tq dotenv run` in the child's environment. External tools get a filtered environment (spec C's runner). A tool's stderr passes `redact` before it enters a finding, a log or the transcript.
- **Plan files:**
  - Terraform plans and state hold sensitive values in clear, and so can Pulumi plans and what-if payloads.
  - Raw plans live only in the cloud pack's `StateDir()/plans/` (`$TQ_HOME/packs/<ws>/cloud/plans/`, 0600, per workspace, never cross-client) and are pruned after 7 days by default.
  - The redacted JSON that the rules read replaces every value marked in `after_sensitive` or `before_sensitive`, every value under a key that looks like a secret (`password`, `secret`, `key`, `token`, `connection_string`), and anything `internal/secrets` matches, with `[REDACTED:…]`.
  - Summaries hold addresses, types and counts only.
  - `tq/plan-read`, `tq/plan-print` and `tq/plan-commit` keep plans and state out of the transcript and git.
- **Approvals:**
  - The hub decision is human-only to answer, and its text is the redacted summary.
  - The binding (hash, environment, identity, TTL, single use) is checked by the guard when the command runs, not when the request is made.
  - The ledger and the hub store are protected by `tq/hub-store-write`. Like the build gate, this stops a careless agent, not one set on defeating it, until tq isolates the key at the OS level. The doc says so.
- **PII at the exits:**
  - The drill restores real data into a local scratch database. That is local processing, which the PII rule allows.
  - The evidence holds counts only, and the dump and the scratch database are deleted.
  - pgTAP fixtures are synthetic (`tester-a`, `tester-b`), and `tq/pii-fixture-dump` still applies to the tests directory.
  - Findings never quote row data. A finding that quotes a SQL literal is passed through `redact` with the workspace's `exit_classes`.
- **No network in hooks:** the guards read only `ReadState` and `ReadLocal` files, and `ReadLocal` never reads `.env*`. The identity checks that need a network run in `tq pack cloud plan`, a command with a timeout, never in PreToolUse.
- **Hooks never stall:** the pack guards keep spec C §2.14's bound of under 50 ms, with no process. A plan hash that runs over its 40 ms budget is a deny (§9.4). `tq pack check` uses the framework's per-tool timeouts: a default of 120 s for the cloud and db tools, `timeouts.<tool>` to override it, 1800 s at most, and a process-tree kill when it expires.

## 21. Testing

- **No real cloud, database server or network in tests.**
  - Fake CLIs use the existing copy-of-the-test-binary pattern (`TestMain` with `TQ_FAKE_<TOOL>=1`) for `terraform`, `tofu`, `az`, `aws`, `cdk`, `pulumi`, `checkov`, `trivy`, `conftest`, `tflint`, `cfn-lint`, `squawk`, `sqlfluff`, `pg_prove`, `pg_restore`, `psql`, `sqlcmd`, `sqlpackage` and `supabase`. Each fake prints fixture output and records its argv.
  - Any HTTP goes through `httptest`.
  - Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`.
- **Golden fixtures** under `internal/packs/cloud/testdata/` and `internal/packs/db/testdata/`: plan JSON (create, update, delete, replace, sensitive values), what-if JSON, ARM and Bicep-built JSON, CFN YAML with short tags, CDK templates, a change-set JSON, Pulumi preview JSON, IAM documents (each rule, plus negatives), Postgres and T-SQL migrations (each rule, plus a safe twin of each, dollar quotes and `GO` batches), Supabase `config.toml`, and TAP output. The names are `acme` and `tester`, and the ids are all zeros or repeated digits.
- **Gate tests** run table-driven over commands: wrappers, PowerShell forms, the unresolved environment, an expired, consumed or mismatched approval, identity drift from a fake `azureProfile.json`, and `auto` really allowing (the regex rules superseded).
- **Redaction tests:** a plan with a sensitive output never shows the value in a summary, a finding, the decision text or a log.
- **Rego:** `opa test` runs in CI for the bundle (a CI job like `templates`); Go tests that need `conftest` use the fake.
- **Real-database tests** (pgTAP against a real Postgres, a drill against a container) are opt-in and manual: `TQ_PACKS_REAL_DB=1` plus a local URL. They are never run by CI or an agent. An end-to-end apply against a real cloud is a manual step for the human only.
- The plugin suite covers the skills' frontmatter, the bootstrap snippet, and the crew mapping in `build/SKILL.md`.

## 22. Workstreams

Spec C's framework workstream **F** goes first, and one framework PR serves both specs. F owns every shared seam that spec C §2.15 lists:
- the manifest `packs:` struct and `RegisterPackValidator`, and the repo layer;
- `internal/packs` core: `Register`, `RegisterPack`, `RegisterGuard`, `Input`, `StateDir()`, `Workspace()`, `WorkspaceRoot()`, `scan/`, `toolrun/`, `approval/` (with the ledger) and `baseline/`;
- the CLI: `list`, `check`, `doctor`, `crew`, `approvals`, `baseline`, and the `tq pack <pack>` mount;
- `cmd/tq/packs.go`, which already imports `cloud` and `db`, and the stub `internal/packs/{cloud,db}/doc.go`;
- `internal/policy/builtin_packs.go` (empty `packRules()`) and the `Builtin()` append;
- the generic guard wiring in `claudehook_policy.go`, with `Supersedes`;
- the hub runner hunk (ignore `permission` decisions with `data.pack`) and the decision view's evidence list;
- triage and crew wiring, and the gate's `*-reviewer` acceptance;
- the generic on-ramp step and the `pack` skill;
- doctor and insights wiring, including `packs.Count` and `packs-suggest`;
- the `docs/PACKS.md` index.

Spec D's workstreams never edit those files. Each registers its checks, config keys, flags, guards, doctor probes and commands from its own `register_*.go`, using `init`. Nobody edits `doc.go`.

| WS | Scope | Owns (new; disjoint) | Depends on |
|---|---|---|---|
| **D-CM** cloud model and readers | `Resource` model; the readers `tfplan`, `arm`, `whatif`, `cfn`, `changeset`, `pulumi`; sensitive-value redaction | `internal/packs/cloud/model/`, `internal/packs/cloud/readers/`, `internal/packs/cloud/testdata/readers/` | F |
| **D-CP** iac-policy | `IAC0xx`, `cloud/iac-policy`, `cloud/no-plan-files`, the external checks and parsers (checkov, trivy, conftest, tflint, bicep-lint, cfn-lint, using `Input.Tools` and `Input.Baseline`), the Rego bundle and `opa test`, the cloud config keys `iac`, `conftest` and `tags`, the flags `iac` and `baseline_change`, the static rules `tq/plan-read`, `tq/plan-print` and `tq/plan-commit` | `internal/packs/cloud/policy/` (Go + `*.rego`), `internal/packs/cloud/tools/`, `internal/packs/cloud/register_policy.go` | F, D-CM |
| **D-CI** IAM lint | `IAM0xx`, `AZR0xx`, `GCP001`, `cloud/iam`, the role and verb tables, the config keys `accounts` and `tenants`, the flag `iam` | `internal/packs/cloud/iam/`, `internal/packs/cloud/register_iam.go` | F, D-CM |
| **D-CG** apply/destroy gate | command detection, environment resolution (shared with D-DG), identity drift, the plan store and plan records, `tq pack cloud plan/show/approve/prune` (through `Pack.Commands`), `approval.Ask`/`Verify`/`MarkUsed` with the cloud evidence, `cloud/plan-fresh`, the config keys `environments`, `default_environment` and `require_plan`, the guards `tq/iac-apply-env` and `tq/iac-identity-drift` | `internal/packs/envgate/`, `internal/packs/cloud/gate/`, `internal/packs/cloud/plans/`, `internal/packs/cloud/commands.go`, `internal/packs/cloud/register_gate.go`, `internal/cmd/claudehook_iac.go` (`RegisterGuard` calls only) | F, D-CM |
| **D-DL** migration lint | `sqllex`, `PG0xx`/`MS0xx`, `db/migration-lint`, `db/migration-rollback`, `db/squawk`, `db/sqlfluff`, the config keys `migrations`, `small_tables` and `edition`, the flag `db_migration` with the Jev-flag mapping to `db-reviewer` | `internal/packs/db/sqllex/`, `internal/packs/db/lint/`, `internal/packs/db/tools/`, `internal/packs/db/register_lint.go` | F |
| **D-DR** RLS and pgTAP | the state replay, `RLS0xx`, `db/rls`, `db/rls-tests`, `db/pgtap` (local-only target check, `start`, the TAP parser), the Supabase `config.toml` reader, the config keys `rls` and `test_db`, the flag `rls_change` | `internal/packs/db/rls/`, `internal/packs/db/pgtap/`, `internal/packs/db/register_rls.go` | F, D-DL (lexer) |
| **D-DG** db gate and drill | `tq/db-migrate-env`, `tq/restore-target`, `tq pack db drill/approve` (through `Pack.Commands`), `db/restore-drill`, the evidence writer, the config keys `drill` and `environments` | `internal/packs/db/drill/`, `internal/packs/db/gate/`, `internal/packs/db/commands.go`, `internal/packs/db/register_gate.go`, `internal/cmd/claudehook_db.go` (`RegisterGuard` calls only) | F, D-CG (`envgate`), D-DL |
| **D-GR** guard hardening | `tq/hub-store-write` (one entry per pack) and the never-disable list | the `tq/hub-store-write` entries in `internal/policy/builtin_packs.go` (D-CP adds the `tq/plan-*` entries to the same file in its own commit; the two touch disjoint, appended blocks), `internal/guardcfg/never_disable.go` (new) | F |
| **D-PL** plugin | the agents `iac-reviewer` and `db-reviewer`; the skills `iac-policy`, `rls-tests` and `restore-drill`; the changed `db-migration`, `auth-security`, `ci-pipeline` and `supply-chain`; templates; checklists; on-ramp reference files | `plugin/agents/iac-reviewer.md`, `plugin/agents/db-reviewer.md`, `plugin/skills/{iac-policy,rls-tests,restore-drill}/`, `plugin/templates/packs/{cloud,db}/`, `plugin/skills/db-migration/references/review-checklist.md`, `plugin/skills/pack/references/onramps/{cloud-iac,cloud-apply,db-migrations,db-rls,db-drill}.md`, `plugin/tests/test_pack_cloud_db.py` | F (flag names) |
| **D-OI** doctor probes and counters | the `cloud-*` and `db-*` doctor codes through `Pack.Doctor`, the detection hints, the `packs.Count` counters of §19 (the counter calls live in the owning check and guard files; D-OI owns only the doctor files and a test that pins the counter names) | `internal/packs/cloud/doctor.go`, `internal/packs/db/doctor.go`, `internal/packs/cloud/counters_test.go`, `internal/packs/db/counters_test.go` | F, D-CG, D-DG |
| **D-DOC** docs | `docs/packs/{cloud-iac,cloud-apply-gate,db-migrations,db-rls,db-restore-drill}.md`, the `HUB.md` row for pack approvals, the `CLAUDE-HOOK.md` rule rows | those files and rows | all |

Several workstreams register keys, flags or commands for the same pack. `RegisterPack` merges entries by pack name and panics on a duplicate key or command name (spec C §2.4), so the `register_*.go` files never collide.

Small hunks that spec D still makes in shared files (spec C §2.15, last paragraph); none overlaps a spec C workstream:

| File | Hunk | WS |
|---|---|---|
| `internal/guardcfg` | one call that consults `never_disable.go`, so that `tq/hub-store-write` ignores `guard.disable` (decided at spec review). It is spec D's only Go edit outside its own files. | D-GR |
| `docs/HUB.md` | one row in the decisions table: `permission` with `data.pack` set, for pack approvals | D-DOC |
| `docs/CLAUDE-HOOK.md` | the rule rows of §16 | D-DOC |
| `plugin/skills/{db-migration,auth-security,ci-pipeline,supply-chain}/SKILL.md` | the links and rows of §17 (these skills are not touched by spec C) | D-PL |

**Framework contract: what spec C adopted.** Spec C §2.15 resolved every request from the first draft of this section:
1. **Per-pack config:** `PackConfig.Raw` and `manifest.RegisterPackValidator`, fed by `RegisterPack` `ConfigKeys`. Adopted.
2. **Run context:** `StateDir()`, `Workspace()` and `WorkspaceRoot()` on `Input`, and the same through `GuardCtx`. Adopted.
3. **Pack subcommands:** `Pack.Commands` mounted under `tq pack <pack>`. Adopted.
4. **Baseline:** in the framework (§2.6a). `tq pack baseline --pack cloud` replaces `tq pack cloud baseline`, and `cloud/baseline.go` is gone. Adopted.
5. **Finding `Level`:** already in the framework.

**Further contract additions, adopted 2026-10-28.** The controller adopted these as written, and spec C §2 carries them:
- **A. `GuardCtx.ReadLocal(path)`:** limited to the workspace root and the identity's config dirs (from `envplan`), 64 KiB per file, inside the 50 ms bound, and it **never reads `.env*`**. It is used by §9.2 step 4, §9.6 and §14 step 3 for `.terraform/environment`, the Pulumi selected stack, `$AZURE_CONFIG_DIR/azureProfile.json` and `supabase/.temp/project-ref`.
- **B. `GuardCtx.HashState(name)`:** a streaming sha256 over a `StateDir()` file of at most 64 MiB, with a 40 ms budget. **Going over the cap or the budget is a deny.** The migration tree hash is not recomputed in the guard; it comes from the stored check output (§14).
- **C. The ledger holds the human's answer.** The hub's decision-answer handler (`tq hub answer` and the web handler, through `approval`) appends the decision id, choice, actor kind and answer time to the pack's `StateDir()/ledger.jsonl`, so the guard-time `Verify` reads only the ledger.
- **D. Core Jev flags in a pack's crew map.** A pack may register a `Flag` with a core flag name and no `Raise`, meaning "add these reviewers when core triage raises it". §15 uses this for `schema`, `migration`, `rls`, `data_loss` and `destructive`.
- **E. `Pack.Hints []string`:** globs plus optional content regexes, feeding F's `packs-suggest`. D-OI does not edit F's doctor code.
- **F. Per-pack `StateDir()`** (`$TQ_HOME/packs/<ws>/<pack>/`) is confirmed. The `db` guards share the `envgate` code, not cloud state.

A note on the earlier fallbacks: skipping the local signals for A, and comparing size and modification time for B, are no longer the behaviour. The only remnant is that the size and modification time recorded at plan time serve as a cheap first check before hashing (§9.4).

## 23. Done when

- Every rule in §7.2, §8, §10 and §11 has a positive and a negative fixture test, and the readers pass every golden file.
- A declared tool that is missing makes its DoD item `MISSING`, an undeclared one is `skip`, and a tool that hangs is killed at its timeout and reported as `fail`.
- The approval flow of §2's success criteria passes against fakes, for Terraform, az deployment, CDK, CloudFormation change sets and Pulumi (the plan form, and the weaker hash binding).
- `tq pack db drill` against the fakes writes a correct evidence line and refuses a non-local target, and `db/restore-drill` reacts to `freshness`.
- The redaction tests pass, and no plan value appears in any output.
- `go test ./...`, the plugin suite, `opa test` and CI are green.

## 24. Assumptions to verify at build, and decisions

The assumptions are marked inline. In summary: the output formats of every external tool (§7.3, §10, §12); the field names of what-if, the change set and Pulumi preview (§6); Pulumi update plans and the selected-stack file (§9.2, §9.5); `cdk deploy --app <dir>` (§9.5); the `azureProfile.json` layout (§9.6); the Azure role GUIDs (§8); OpenTofu's stale-plan refusal (§9.4); the Supabase `config.toml` key, `supabase test db` and `.temp/project-ref` (§11, §12, §14); Rego v1 in the installed Conftest (§7.4); IaC tags surviving into plan JSON (§5).

Decided at spec review (2026-10-28):
1. **Approvals are single-use**, with a 60 min default and a 24 h maximum. A multi-stack CDK deploy needs one approval per stack (§9.5).
2. **`require_merged` defaults to true for prod** (§4, §9.3).
3. **The restore drill blocks the DoD** only when a migration changed and a drill is declared (§5, §13). Otherwise `tq pack doctor` warns.
4. **Staging defaults to `ask`**, and prod uses `approve` through a hub decision (§9.3).
5. **Pulumi in `approve` environments** uses the weaker hash binding when update plans are unavailable. It is not denied (§9.5).
6. **The manifest can't disable `tq/hub-store-write`** (§16, the `guardcfg` hunk in §22).
7. **GCP is linted from Terraform plans only** for now (`GCP001`, §8).
