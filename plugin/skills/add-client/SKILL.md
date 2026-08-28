---
name: add-client
description: Create a new client workspace with identity configuration, cloud/database/git manifest, and preflight checks. Use when the user says "new client", "add a client", "set up a workspace", "onboard a client", or wants to start working with a new company/organization. Also triggers when the user mentions needing separate git identities, cloud subscriptions, or database connections for different clients.
---

# Add Client

Create a new client workspace by running `tq add`, which writes the
`.tentaqles.yaml` manifest, sets up the per-workspace git identity, and
creates the identity dirs for each requested CLI. `tq` — not this skill — is
the source of truth for identity/git/claude config; this skill just gathers
the answers and drives the CLI.

## Gather Information

Collect the following from the user. Client name is required; everything
else has sensible defaults. If the user provides partial info (e.g., "new
client Acme, they use AWS"), fill in what you can and ask only about the
gaps.

| Field | Required | Example | Notes |
|-------|----------|---------|-------|
| Client name / slug | Yes | `acme-corp` | Lowercase, hyphens; becomes the workspace folder name |
| Base folder | No | `C:\repos` | Where client folders live. Ask if not obvious from cwd; must already be registered with `tq init` |
| Git email | Yes | dev@acme.com | Passed to `--git-email` |
| Git name | No | Acme Dev | Passed to `--git-name` |
| Identities | No | `claude,gh` | Comma list: `claude,codex,gemini,cursor,gh,az,aws,gcloud,kube,npm` (default `claude,gh`) |
| Display name | No | "Acme Corp" | Passed to `--display-name` |
| Permission mode | No | default | `default\|acceptEdits\|plan\|bypass`, passed to `--permission-mode` |

Cloud, database, and PM-tool preferences are still useful context for the
client's `CLAUDE.md`, but `tq add` does not manage those sections — capture
them in the CLAUDE.md skeleton (below), not in `.tentaqles.yaml`.

## Create the Workspace

Run `tq add` with the gathered answers:

```bash
tq add {slug} --base "{base_folder}" --git-email "{git_email}" --git-name "{git_name}" --identities claude,gh[,az|aws|...] --display-name "{display_name}"
```

`tq add` creates the workspace folder, writes `.tentaqles.yaml` (this file
is generated and owned by `tq` — see note below), configures the workspace
git identity, and creates a private config dir per requested identity.

## What `tq add` Wrote

`.tentaqles.yaml`'s `git`, `identities`, and `claude` sections are written
and maintained by `tq` (`tq add`, `tq doctor`, `tq allow`). Don't hand-edit
those sections — if you need to change them, use `tq` commands or
`/tentaqles:client-settings`, which knows the boundary between fields `tq`
owns and fields you can edit by hand.

## Create CLAUDE.md skeleton

Write `CLAUDE.md` at the workspace root (still done by this skill, not by
`tq`):

```markdown
# {display_name}

## Overview
<!-- Describe what this client does, their industry, key products -->

## Tech Stack
<!-- Will be updated as projects are added -->

## Conventions
- Git: {git_email}
{if cloud_provider given:}
- Cloud: {cloud_provider}
{endif}
{if db_provider given:}
- Database: {db_provider}
{endif}

## Development
<!-- Add build commands, test commands, deployment notes as you learn them -->
```

## Verify with `tq doctor`

```bash
tq doctor
```

Show the result to the user. `tq doctor` never mutates — it only reports
what's wrong (untrusted manifest, git identity drift, missing hooks, etc.).

## Ask Before Trusting

`tq add` does not trust the workspace automatically, and neither does this
skill. **Always ask the user first**: "Run `tq allow {slug}` to trust this
workspace so it can export its identity? (Needed before you `cd` in and get
the right git/CLI identity.)"

Only run `tq allow {slug}` after the user confirms. Never auto-trust a new
workspace.

## Report

Summarize what was created:
- Workspace path
- `tq add` output (identities configured)
- `tq doctor` results
- Whether the user asked you to run `tq allow`
- Next steps: "Create your first project with `/tentaqles:add-project`"

## Error Handling

- If `tq add` fails because the base folder isn't registered, tell the user to run `tq init <base>` first.
- If the workspace already exists, `tq add` will report that — ask the user whether they want to edit settings instead via `/tentaqles:client-settings`.
- If `tq` is not installed or not on PATH, tell the user to install it (see the repo README) — this skill cannot create a properly-managed workspace without `tq`.
