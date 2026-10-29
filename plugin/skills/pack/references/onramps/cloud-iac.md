# cloud: IaC lint and review

## kickoff

Only when the stack ADR names Terraform, OpenTofu, Bicep or ARM, CDK,
CloudFormation or Pulumi (this section is read whether or not the cloud pack is
enabled; never assume it is):
- At human stop 1, propose the `packs.cloud` block from the plugin's
  `templates/packs/cloud/packs-cloud.yaml`: the `iac` paths and kinds the ADR
  names, `environments` with `prod: {apply: approve, destroy: deny}` (and
  `staging` when there is one), `default_environment: prod`, and in `tools`
  only the tools that `command -v checkov conftest tflint trivy cfn-lint` finds
  on PATH. It is a proposal for the human, who edits the manifest and runs the
  trust step; you never edit `.tentaqles.yaml`. Headless: write it to
  `docs/privacy-manifest-proposal.yaml`, never the manifest.
- Add the rows of `templates/packs/cloud/gitignore.txt` (`*.tfplan`,
  `*.tfstate*`, `.terraform/`, `cdk.out/`) to `.gitignore` in the first
  checkpoint.
- When the project ships its own Rego, add `policy/` with
  `templates/packs/cloud/policy-example.rego` and its test as a checkpoint item.

## feature

- The work raises the `iac` flag when it touches an IaC path (and `iam` for
  policy and role files). Before any diff exists, pass those flags to
  `tq pack crew --flags iac,iam` and expect `iac-reviewer` (plus
  `security-reviewer` for `iam`) in the crew; take the crew from what it prints.
- Spec slice: name the environments the change reaches, every stateful
  resource it creates, changes or removes, and every new grant or trust.
- Acceptance checks to add: "the dev plan shows no stateful delete" (from
  `tq pack cloud show <plan-id>` on a dev plan), "`cloud/iac-policy` and
  `cloud/iam` report no error", and, for a grant, "the role names its
  resources; no `*` action".

## fix

- The regression test may be a fixture plan JSON (with `acme` names and zeroed
  ids) under the project's IaC tests that reproduces the finding, checked by
  `tq pack check --pack cloud`, or a Conftest/`opa test` case for a project
  Rego rule.
- Never fix a `cloud/*` finding by suppressing it without a reason that holds,
  and never by editing a baseline file.

## dod

- List one row per `pack:cloud/*` item the gate recorded: `cloud/iac-policy`,
  `cloud/iam`, `cloud/no-plan-files`, `cloud/plan-fresh` when `require_plan` is
  on, and each declared external check. `MISSING` for a required fail, with the
  first findings; `skip` for advisory and skipped checks.
- A declared tool that is not installed goes through the `pack` skill's step 4;
  never remove it from `tools:` yourself.
