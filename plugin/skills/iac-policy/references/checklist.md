# IaC review checklist (cloud pack)

Used by the `iac-reviewer` agent and by the `iac-policy` skill's review mode.
Judge each row from the diff, the files it touches, the `cloud/*` results and
the redacted plan summary (`tq pack cloud show <plan-id>`). Never from a raw
plan or state file. Cite file:line. Rows that need the live cloud or a human
decision are `U` rows. Row ids are stable: reviewers copy them into their
answers.

Severity guide: **BLOCKER** = data loss, public exposure, privilege escalation
or a secret in code in a protected environment; **MAJOR** = a control missing
or weakened (no deletion protection, a wide grant with a reason that does not
hold); **MINOR** = hardening (tags, logging, naming).

## Answer-key rows (used by the iac-reviewer agent)

| # | Check | Rule ids and source |
|---|---|---|
| I1 | Blast radius: the plan summary's creates, updates, deletes and replaces per environment match what the spec says the change does; nothing outside the stated stacks or modules moves | plan summary; `cloud/plan-fresh` |
| I2 | Stateful resources (databases, buckets and storage accounts, key vaults and KMS keys, disks and volumes, DNS zones, Cosmos/Dynamo tables) are never deleted or replaced unless the spec names it and a restore path exists | `IAC001`; `IAC007` (`--mode Complete`) |
| I3 | Deletion protection is on for stateful resources in prod: RDS `deletion_protection`, key vault purge protection, CFN `DeletionPolicy: Retain` or `Snapshot` | `IAC006` |
| I4 | Network exposure: no ingress from `0.0.0.0/0` or `::/0` to admin or database ports; databases are not public; no plain-HTTP listener | `IAC002`, `IAC005`; Rego bundle (TLS, listeners) |
| I5 | Storage is private and encrypted at rest: no public ACL, public-access-block on, `allowBlobPublicAccess: false`, no `allUsers` | `IAC003`, `IAC004` |
| I6 | IAM least privilege: no `Action: "*"` or `"<service>:*"`, no `Resource: "*"` on write actions, no `NotAction`/`NotResource`, no managed admin policies | `IAM001` to `IAM004` |
| I7 | No escalation path: `iam:PassRole`, policy-version, attach/put policy, `UpdateAssumeRolePolicy`, `CreateAccessKey` and `sts:AssumeRole` are scoped to named resources and conditioned | `IAM005` |
| I8 | Trust and resource policies: no `Principal: "*"`; foreign accounts need `sts:ExternalId`; OIDC trust has an `aud` condition and a GitHub `sub` pinned to the repo and branch or environment | `IAM006`, `IAM007` |
| I9 | Azure and GCP roles: no Owner, Contributor, User Access Administrator or RBAC Administrator at subscription or management-group scope; no wildcard custom roles; no role-assignment write in custom roles; no `roles/owner`, `roles/editor` or `allUsers` grant | `AZR001` to `AZR004`, `GCP001` |
| I10 | Environment and identity separation: dev resources never in the prod state or stack; each environment's `match` (workspace, dir, var file, stack, subscription, account, profile) selects only it; prod is never the accidental default | `packs.cloud.environments`; `tq/iac-identity-drift` |
| I11 | Secrets: none in variable defaults, tfvars, parameter files, tags or outputs; outputs that carry secrets have `sensitive = true`; values come from a secret store by name | Rego bundle; `tq/plan-print` |
| I12 | Suppressions and baseline: every `tq-allow` tag has a reason that holds; a baseline that grew (`baseline-grew`) is explained in the spec; no plan, state or `.terraform/` file is tracked | `cloud/no-plan-files`; `tq/plan-commit` |

## Report format

```markdown
| # | Severity | Finding [row, rule] | Location | Fix |
|---|---|---|---|---|
| 1 | BLOCKER | [I4, IAC002] SSH open to 0.0.0.0/0 on the bastion security group | infra/terraform/net.tf:41 | restrict to the VPN CIDR or use SSM Session Manager |
```
