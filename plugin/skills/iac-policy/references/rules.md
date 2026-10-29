# Cloud pack rules and checks

What `tq pack check --pack cloud` reports. tq's `docs/packs/cloud-iac.md` is the
full page; this is the agent's quick reference.

## Checks

| Check | Kind | Runs when | Reads |
|---|---|---|---|
| `cloud/iac-policy` | built-in | a changed path is under `packs.cloud.iac[].path`, or a plan was recorded for the IaC tree | recorded plan JSON, ARM JSON from `bicep build`, CFN and `cdk.out` templates, Pulumi preview JSON |
| `cloud/iam` | built-in | same, plus `*.json` policy files with a `Statement` under IaC paths | same, plus the policy files |
| `cloud/plan-fresh` | built-in | an IaC path changed and `require_plan: true` | the plan records: a plan whose inputs hash matches the current tree |
| `cloud/no-plan-files` | built-in | always, while the pack is enabled | tracked files: no `*.tfplan`, `*.tfstate*`, `.terraform/` or drill dumps |
| `cloud/checkov` | external | `checkov` declared in `packs.cloud.tools` | source dirs, or the plan JSON (`--framework terraform_plan`) |
| `cloud/trivy` | external | `trivy` declared | source dirs (`trivy config`); `LOW`/`MEDIUM` warn, `HIGH`/`CRITICAL` fail |
| `cloud/conftest` | external | `conftest` declared | the shipped Rego bundle plus `packs.cloud.conftest.policy`, namespaces `tq.terraform`, `tq.arm`, `tq.cfn` |
| `cloud/tflint` | external | `tflint` declared, Terraform or OpenTofu paths | each Terraform dir |
| `cloud/bicep-lint` | external | `bicep` declared (run as `az bicep lint`), Bicep paths | each `.bicep` file |
| `cloud/cfn-lint` | external | `cfn-lint` declared, CloudFormation or CDK paths | templates |

A declared tool that is missing fails its check (`declared in packs.cloud.tools
but not installed`); an undeclared tool that is missing is `skip`; an installed
but undeclared one is advisory. External findings that the baseline lists are
downgraded to `warn`; a diff that grows the baseline adds `baseline-grew` and
raises the `baseline_change` flag.

## Built-in IaC rules

| Id | Rule | Level |
|---|---|---|
| `IAC001` | delete or replace of a stateful resource | fail in an `approve` environment, else warn |
| `IAC002` | ingress from `0.0.0.0/0` or `::/0` to 22, 3389, 1433, 3306, 5432, 6379 or 27017 | fail |
| `IAC003` | public object storage (S3 ACL or public-access-block off, `allowBlobPublicAccess: true`, GCS `allUsers`) | fail |
| `IAC004` | encryption at rest switched off | fail |
| `IAC005` | database open to the internet (Azure SQL all-IP firewall rule, `publicNetworkAccess: Enabled`, RDS `publicly_accessible`) | fail in prod, warn elsewhere |
| `IAC006` | deletion protection off on a stateful resource in prod | fail |
| `IAC007` | ARM/Bicep `--mode Complete` | warn in the lint; the gate treats it as a destroy |

## Built-in IAM rules

| Id | Rule | Level |
|---|---|---|
| `IAM001` | `Allow` with `Action: "*"` or `"<service>:*"` | fail |
| `IAM002` | `Allow` with `Resource: "*"` on a write action | fail; warn for read-only verbs |
| `IAM003` | `Allow` with `NotAction` or `NotResource` | fail |
| `IAM004` | `AdministratorAccess`, `PowerUserAccess` or `IAMFullAccess` attached | fail |
| `IAM005` | escalation actions on `*` or without conditions | fail |
| `IAM006` | open trust, a foreign account without `sts:ExternalId`, OIDC without `aud`, a GitHub `sub` wildcarded at the repo level | fail |
| `IAM007` | resource policy with `Principal: "*"` and no condition | fail |
| `AZR001` | Owner, Contributor, User Access Administrator or RBAC Administrator at subscription or management-group scope | fail; warn at resource-group scope |
| `AZR002` | custom role with `*` actions, or a write role assignable at `/` or a whole subscription | fail |
| `AZR003` | custom role granting `roleAssignments/write` or `roleDefinitions/write` | fail |
| `AZR004` | role assignment to a principal in a tenant outside `packs.cloud.tenants` | warn |
| `GCP001` | `roles/owner` or `roles/editor`, or any role to `allUsers`/`allAuthenticatedUsers` (Terraform plans only) | fail |

## Suppressing a built-in finding

Only in the source, with a reason, as a tag on the resource:

```hcl
tags = {
  "tq-allow" = "IAM002: the log shipper must list every bucket in the account"
}
```

In JSON and YAML templates it is the same tag key and value. A suppression
without a reason is itself an error finding, and every suppression is counted.
Never suppress to make a check pass without the reason being true; the
`iac-reviewer` judges it (row `I12`).
