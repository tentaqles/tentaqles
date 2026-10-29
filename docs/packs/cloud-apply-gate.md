# Cloud: plan approvals and the apply gate

Every `terraform`/`tofu apply`, `pulumi up`, `cdk deploy`, `az deployment ...
create`, `aws cloudformation deploy` and `sam deploy` (and each tool's destroy)
an agent runs resolves to an environment and gets that environment's policy
(spec D §9). The IaC checks are on [IaC](cloud-iac.md).

## Environments

```yaml
packs:
  cloud:
    default_environment: prod        # what an unresolved target counts as
    environments:
      dev:
        match: {workspaces: [dev], dirs: [infra/envs/dev], subscriptions: [11111111-1111-1111-1111-111111111111]}
        apply: auto
        destroy: ask
      prod:
        match: {workspaces: [prod], dirs: [infra/envs/prod]}
        apply: approve
        destroy: deny
        approval_ttl: 60m            # max 24h; an approval is single use
        require_merged: true         # the plan's commit must be on the default branch
```

- **Policies:** `auto` (allowed), `ask`, `approve` (needs a human-approved
  recorded plan) and `deny` (a human runs it in their own terminal). A missing
  `apply`, `destroy` or `migrate` is `deny`.
- **Match keys:** `workspaces`, `dirs`, `var_files`, `stacks`,
  `resource_groups`, `subscriptions`, `accounts`, `profiles` and `dotenv`. The
  command's flags win over a `tq dotenv run --file` wrapper, then variables,
  then local files (`.terraform/environment`, the Azure CLI profile, the
  Pulumi stack). When several environments match, the stricter wins.
- **Unresolved targets** count as `default_environment`, or the strictest
  declared environment when it is unset. A command tq cannot read (a computed
  value, an unknown wrapper, a folder it cannot follow) is floored the same way.
- **The repo layer** (`tq-packs.yaml`) can add a new environment with its own
  `match`, never extend the `match` of one the manifest defines, and never
  loosen a policy.

## Plan, approve, apply

1. Record the plan through tq, never run bare:
   `tq pack cloud plan --env prod -- terraform -chdir=infra/envs/prod plan -out=-`.
   tq writes the plan outside the repository (0600, under its state directory),
   records its hash, inputs, identity and the `cloud/*` results, and prints a
   redacted summary and a plan id. `tq pack cloud show <plan-id>` prints the
   summary again; neither ever prints the plan.
2. Ask: `tq pack cloud approve <plan-id> [--destroy]`. Agents may ask. When a
   required check failed on the plan, the request is refused unless a human
   passes `--allow-failing` from their own terminal.
3. The human answers **in their own terminal**: `tq hub answer <card>
   <decision>`, typing back the pack, the action and a 12-hex code of the
   subject. The board shows the decision and the exact command, but does not
   answer pack approvals. The answer is signed with the workspace key in the OS
   keychain.
4. Apply the saved plan, unchanged, in a command of its own:
   `terraform -chdir=infra/envs/prod apply <plan path>`. The guard checks the
   plan's hash, the environment and the identity, and consumes the approval
   once. A bare `terraform apply` in an `approve` environment is denied with a
   reason that says to apply the saved plan.

`tq pack approvals --pack cloud` lists pending, approved, used and expired
approvals. `tq pack cloud prune --older <age>` (default 7d) deletes old stored plans
(records and approvals are kept).

## Guards

| Guard | Default | What |
|---|---|---|
| `tq/iac-apply-env` | the environment's policy | resolves each apply or destroy and applies `auto`, `ask`, `approve` or `deny`; it supersedes the core `tq/iac-apply`, `tq/iac-destroy` and `tq/cloud-delete` asks for the commands it reads |
| `tq/iac-identity-drift` | deny | an apply or destroy whose subscription, profile or AWS account disagrees with its environment or the manifest |

**Unknown global flags.** A flag tq does not know, written before the verb
and followed by another word (`terraform -foo x apply`, `pulumi --x a b up`,
`aws --x v cloudformation deploy`), counts as an apply to the strictest
environment with no approval to match. Put such a flag after the verb, or
report it (see [PACKS.md](../PACKS.md#known-behaviours)).
