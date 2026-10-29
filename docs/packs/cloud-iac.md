# Cloud: IaC policy checks

The cloud pack (release 4, spec D §5-§8) lints infrastructure as code before it
is applied: Terraform and OpenTofu, Bicep and ARM, CDK, CloudFormation and
Pulumi. The apply gate is on its own page, [Apply gate](cloud-apply-gate.md).

## Config

```yaml
packs:
  cloud:
    enabled: true
    tools: [checkov, conftest, tflint]   # declared => required checks
    iac:
      - {path: infra/terraform, kind: terraform}   # terraform | tofu | bicep | arm | cdk | cloudformation | pulumi
    accounts: [111111111111]             # our AWS accounts (cross-account trust rule)
    tenants: [00000000-0000-0000-0000-000000000000]
    tags: [owner, cost-center]           # tags every taggable resource needs
    conftest: {policy: [policy/]}        # project Rego, added to the shipped bundle
    baseline: .tq/iac-baseline.json      # external findings; only a human writes it
    require_plan: false
```

The plugin's `templates/packs/cloud/packs-cloud.yaml` is a full example. A
`tq-packs.yaml` may add `iac` paths, `conftest.policy` directories and `tags`
(union) and turn `require_plan` on; environments follow the rules on the
[Apply gate](cloud-apply-gate.md) page; any other key is trusted-layer only.

## Checks

| Check | Kind | What it does |
|---|---|---|
| `cloud/iac-policy` | built-in | the `IAC0xx`, `AZR0xx` and `GCP001` rules over the IaC sources and recorded plans: public ingress, encryption, deletion protection, stateful deletes, missing tags |
| `cloud/iam` | built-in | the `IAM0xx` rules: wildcard actions and resources, cross-account trust outside `accounts`, OIDC trust not pinned to a repo and branch |
| `cloud/no-plan-files` | built-in | fails when a `*.tfplan`, `*.tfstate*`, `.terraform/` or `cdk.out/` file is tracked |
| `cloud/plan-fresh` | built-in | with `require_plan`, the changed IaC has a recorded plan made from the current inputs |
| `cloud/checkov`, `cloud/trivy`, `cloud/conftest`, `cloud/tflint`, `cloud/bicep-lint`, `cloud/cfn-lint` | external | the tool over the IaC paths; required when the tool is in `tools` |

The rule list and their fixes are in the `iac-policy` skill
(`plugin/skills/iac-policy/references/rules.md`). Conftest runs the shipped
Rego bundle (Rego v1, namespaces `tq.terraform`, `tq.arm`, `tq.cfn`) plus the
`conftest.policy` directories.

- **Suppression:** a built-in finding is suppressed in the source only, as a tag
  on the resource with a reason: `tq-allow = "IAM002: <reason>"`.
- **Baseline:** external findings use the framework baseline, which only a
  human writes (`tq pack baseline --pack cloud`). A diff that grows it raises
  `baseline_change`.

## Guard rules

These are static rules, active while the cloud pack is on:

| Rule | Action | What |
|---|---|---|
| `tq/plan-read` | deny | reading or printing a plan file, a state file, the plan store or a CDK asset manifest |
| `tq/plan-print` | ask | plain `terraform show` or `terraform output` with no name into the transcript (sensitive values masked); use `tq pack cloud show` |
| `tq/plan-secret-print` | ask (every guard mode) | `state pull`, `state show`, `show -json`, `output -json` / `-raw`, `output <name>`, OpenTofu `-show-sensitive`, `pulumi ... --show-secrets` or `pulumi config get`: state or secret values in clear |
| `tq/plan-commit` | deny | `git add` of a plan, state, `.terraform/` or a path under the plan store |

## Review

The `iac` flag (an IaC path changed) and `baseline_change` bring in
`iac-reviewer`; `iam` also brings in `security-reviewer`. `tq pack crew --flags
iac,iam` prints the crew.
