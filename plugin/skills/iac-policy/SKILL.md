---
name: iac-policy
description: "Check infrastructure-as-code with the tq cloud pack and take a change through the plan and approve flow. Runs tq pack check --pack cloud, explains each finding by rule id (IAC, IAM, AZR, GCP and the declared Checkov, Trivy, Conftest, tflint, bicep lint and cfn-lint checks), writes the fixes, records a plan with tq pack cloud plan, shows its redacted summary, and asks the human for a production apply approval. Use when the user says \"check my terraform\", \"is this IaC safe\", \"review this bicep\", \"least privilege\", \"apply to prod\", when a diff touches Terraform/OpenTofu, Bicep/ARM, CDK, CloudFormation or Pulumi, when /tentaqles:feature raises the iac flag, or when a pack:cloud/* item fails."
argument-hint: "[check | plan --env <env> -- <plan command> | approve <plan-id> | review]"
---

# IaC policy

The cloud pack lints IaC offline (no cloud call, no credentials) and gates
applies and destroys per environment. This skill runs the checks, explains and
fixes what they find, and drives the plan and approve flow. The rule and check
tables are in [references/rules.md](references/rules.md); the review rows
(`I1`…`I12`) are in [references/checklist.md](references/checklist.md), used by
the `iac-reviewer` agent.

You never answer an approval, never apply to an `approve` environment without
one, and never read a plan or state file. Plans and state hold secrets in
clear: `tq/plan-read` denies reading them, `tq/plan-print` asks before
`terraform show` or `terraform output` prints one, and `tq/plan-commit` denies
staging them. Use `tq pack cloud show` instead.

## 1. Run the checks

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 630"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 630"
$_to tq pack check --pack cloud --budget 600s; echo "check_exit=$?"
```

- `check_exit=0`: no required check failed. `1`: one did. `2`: a usage or config
  error (the message names the key). `124`: the outer timeout hit; report it.
- The built-in checks are `cloud/iac-policy`, `cloud/iam`, `cloud/plan-fresh`
  and `cloud/no-plan-files`; they always run when they apply. The external ones
  (`cloud/checkov`, `cloud/trivy`, `cloud/conftest`, `cloud/tflint`,
  `cloud/bicep-lint`, `cloud/cfn-lint`) are required only when their tool is in
  `packs.cloud.tools`. A missing declared tool is the `pack` skill's step 4.
- `--builtin-only` skips the external tools; `--all` judges every tracked file.

## 2. Explain and fix

For each `file:line level rule message` line, name the rule from
[references/rules.md](references/rules.md), say why it matters in this
environment, and change the source: narrow the CIDR, turn on deletion
protection, scope the IAM action and resource, pin the OIDC `sub` to the repo
and branch. Then run step 1 again.

- A built-in finding that is a deliberate exception is suppressed in the source
  only, as a tag on the resource with a reason: `tq-allow = "IAM002: <reason>"`
  (quote the key in an HCL map). Write one only when the user agrees the reason
  is true; the reviewer judges it (row `I12`).
- External findings use the framework baseline, which only the human writes.
  You never run `tq pack baseline` and never add an entry to make a finding go
  away. A diff that grows the baseline shows `baseline-grew` and raises the
  `baseline_change` flag, which brings in the `iac-reviewer`.
- A finding under `cloud/no-plan-files` means a plan, state or `.terraform/`
  file is tracked: `git rm --cached` it, add the `.gitignore` rows from the
  `supply-chain` skill, and tell the user that state may hold secrets that were
  pushed (never rotate them yourself).

## 3. Record a plan

A plan is recorded through tq, never run bare, so the plan file lands outside
the repository (0600, under the cloud pack's state directory) and only a
redacted summary reaches you:

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 630"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 630"
$_to tq pack cloud plan --env <env> -- terraform -chdir=<dir> plan -out=-; echo "plan_exit=$?"
```

The plan command per tool: `terraform`/`tofu plan -out=-`, `az deployment group
what-if …`, `cdk synth`, `aws cloudformation create-change-set …`, `pulumi
preview …`. A plan needs cloud credentials: when the identity environment does
not provide them, ask the human to wrap the call in `tq dotenv run --`;
never read `.env` files yourself. A plan that would take longer than 10 minutes takes
`--timeout`, and the outer timeout grows with it.

The output is the plan id and the summary: counts per action, the stateful
deletes and replaces, the environment and the identity. Show it again, with
the `cloud/*` results over that plan, with `$_to tq pack cloud show <plan-id>`
(10 s). Stored plans past their age go with `$_to tq pack cloud prune --older 7d`.

## 4. Apply

The environment's policy decides (`packs.cloud.environments.<env>.apply`, and
`destroy` for a destroy). The `tq/iac-apply-env` guard enforces it, and
`tq/iac-identity-drift` denies first when the subscription, account or profile
disagrees with the environment.

| Policy | What happens to your apply |
|---|---|
| `auto` | allowed |
| `ask` | the human confirms the prompt |
| `approve` | denied unless a matching human approval exists: step 5 |
| `deny` | denied; the human runs it in their own terminal, never you |

In an `approve` environment apply only the saved plan: `terraform apply
<the plan path tq printed>`, `pulumi up --plan <file>`, `cdk deploy --app
cdk.out <one stack>`, `aws cloudformation execute-change-set …`. A bare apply,
`cdk deploy --all`, or a direct `create-stack`/`update-stack` is denied.

## 5. Ask for an approval

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
_to=""; command -v timeout >/dev/null 2>&1 && _to="timeout 30"; command -v gtimeout >/dev/null 2>&1 && _to="gtimeout 30"
$_to tq pack cloud approve <plan-id>; echo "approve_exit=$?"
$_to tq pack approvals --pack cloud; echo "approvals_exit=$?"
```

- `approve` only asks: it opens a hub decision bound to the plan hash, the
  environment and the identity, valid for `approval_ttl` (60 min by default)
  and good for one apply. Add `--destroy` for a destroy. With `require_merged`
  (the prod default) the plan's commit must already be on the default branch.
- If a check failed, the request offers only `reject`. Only the human can pass
  `--allow-failing`, from their own terminal; never suggest you will.
- The human answers in their own terminal (`tq hub answer`); the board shows
  the decision but does not answer pack approvals. You never answer it and
  never run `tq hub answer`. Tell the user the card and wait; then apply the
  saved plan once. A changed plan, an expired or used approval, or another
  subscription is denied again: re-plan and ask again.

## 6. Review mode

In a build, `tq pack crew` adds the `iac-reviewer` for the `iac`, `iam`,
`baseline_change` and (on IaC paths) `destructive` flags. Standalone, go
through [references/checklist.md](references/checklist.md) and report each row
as pass, fail (severity, file:line, fix) or cannot judge.
