# Setup plans

`tq setup` scaffolds several client workspaces — plus shell hooks — from a
single declarative plan, so onboarding a new machine or a batch of companies
doesn't mean running `tq add` and `tq activate` by hand for each one. It has
two modes:

- No flags: an interactive wizard walks through the plan step by step and
  applies it at the end.
- `--from <file>`: load a plan YAML and preview or apply it non-interactively
  (`--dry-run`, `--yes`, `--json`).

`tq setup --example` prints a starting-point plan you can copy, edit, and
feed back in with `--from`.

## Example plan

This is the exact output of `tq setup --example`:

```yaml
# tq setup plan — edit this file, then run:
#   tq setup --from <this file> --dry-run
#   tq setup --from <this file> --yes
#
# base: the folder each company gets a subdirectory under.
# companies: one entry per client; git_name/git_email are required.
# identities: CLI tools to isolate per workspace (defaults to claude, gh).
# hooks: shells to install tq's activation hook into.
# trust: whether newly scaffolded workspaces are auto-trusted.
base: C:/repos
companies:
    - name: acme
      display_name: Acme Corp
      color: blue
      git_name: Jane Doe
      git_email: jane@acme.com
      identities:
        - claude
        - gh
      permission_mode: acceptEdits
    - name: globex
      display_name: Globex Corporation
      color: green
      git_name: Jane Doe
      git_email: jane@globex.com
      identities:
        - claude
        - gh
      permission_mode: default
hooks:
    - bash
    - zsh
    - pwsh
trust: true
```

## Field reference

| Field | Required | Description |
|---|---|---|
| `base` | yes | The folder each company gets a subdirectory under, e.g. `C:/repos` or `~/work`. Created if missing (the wizard offers to create it; `--from` expects it to already exist unless a company scaffold creates it as a side effect). |
| `companies` | yes, at least one | List of client/company entries to scaffold as `tq` workspaces. A plan with zero companies fails validation with `plan has no companies`. |
| `companies[].name` | yes | Short workspace id. Must match the workspace naming rule (lowercase, alphanumeric/hyphen) and be unique within the plan. Becomes the subfolder name under `base`. |
| `companies[].display_name` | no | Human-friendly name shown in listings; defaults to `name` if omitted. |
| `companies[].color` | no | Cosmetic color hint for terminals/prompts that support it. |
| `companies[].git_name` | yes | Git `user.name` for this workspace's `.gitconfig-tentaqles`. |
| `companies[].git_email` | yes | Git `user.email` for this workspace. Must look like an email. |
| `companies[].git_user` | no | Optional platform username (e.g. GitHub handle), stored alongside git identity but not used by git itself. |
| `companies[].identities` | no | List of provider ids (from the provider catalog, e.g. `claude`, `gh`, `aws`) to isolate per workspace. Defaults to `[claude, gh]` if omitted. Each id must be known to the catalog (`tq providers list`) or `Validate` rejects the plan. |
| `companies[].permission_mode` | yes | Claude Code permission mode for this workspace. Must be one of the values in `manifest.PermissionModes` (e.g. `default`, `acceptEdits`, `bypass`). |
| `hooks` | no | List of shells (`bash`, `zsh`, `fish`, `pwsh`, `powershell`) to install the `tq activate` line into. Shells already carrying a hand-installed activation line are detected as "present (unmanaged)" and left untouched — never duplicated. |
| `companies[].tracker`, `companies[].chat`, `companies[].email` | no | The company's tracker, chat and email blocks, written into the new workspace's manifest with the same checks as a hand-written one (see [PROVIDERS.md](PROVIDERS.md#company-services-in-the-manifest)). A token-shaped value in any field is refused, naming the field and never the value. |
| `trust` | no, defaults to `true` | Whether newly scaffolded workspaces are auto-trusted with `tq allow` as part of apply. If the key is absent from the YAML it defaults to `true`; set it to `false` explicitly to scaffold without trusting. |

## Preview, tool-check, and apply

With `--from`, `tq setup` always runs a preview and a tool-check before
touching anything:

- **Preview** lists every change the plan would make (`workspace-create`,
  `workspace-skip` for a company folder that already exists with a matching
  manifest, `hook-install`, etc.) as `KIND  TARGET  DETAIL` rows.
- **Tool-check** reports, per company, whether each identity's CLI is
  installed (`[ok]`), missing with an install hint (`[missing]`), or not
  applicable (`[n/a]`, no CLI for that provider).

`--dry-run` stops after preview + tool-check and writes nothing. Without
`--dry-run`, `tq setup --from` asks `Apply these changes? [y/N]` unless
`--yes` is passed; on a non-interactive stdin without `--yes` it refuses to
apply rather than silently doing nothing or silently applying.

`--json` switches preview, tool-check, and (once applied) the report to a
single JSON object (`{"preview": [...], "toolcheck": {...}, "report": {...}}`)
instead of the human-readable tables — useful for driving `tq setup` from
scripts or the desktop app.

`--write-plan <path>` additionally saves the wizard's answers to that path
as a plan file (on top of the always-written
`~/.tentaqles/last-setup.yaml`), so you can inspect what the wizard decided
or replay it non-interactively later with `--from`.

## The login step

`tq setup` scaffolds workspaces and installs hooks, but it does not log any
CLI in — that stays a separate, explicit step so credentials are never
handled implicitly by a setup script. After `apply` finishes, the report
prints a "Next — run these logins" section listing one `tq login <workspace>
<identity>` command per provider that has a login flow, e.g.:

```
Next — run these logins:
  tq login acme gh
  tq login acme claude
```

Run each of those yourself (interactively) to have that CLI's own login flow
write credentials into the workspace's private, isolated config home. This
mirrors `tq login`'s normal behavior outside of `setup` — see the main
README's "Quick start" and "Where things live" sections for how per-workspace
credential isolation works.

### AWS login

`tq login <workspace> aws` picks the flow from the workspace's private AWS
config (`~/.tentaqles/identities/<workspace>/aws/config`):

- one `[sso-session NAME]` section: `aws sso login --sso-session NAME`;
- several sections: pass `--sso-session NAME` (the error lists the names);
- `--sso`: `aws configure sso` (first-time SSO setup), whatever the file holds;
- otherwise: `aws configure` (static access keys).

`--sso-session NAME` always wins. SSO tokens are cached in the shared
`~/.aws/sso/cache`, which `AWS_CONFIG_FILE` does not move, so workspaces using
the same SSO start URL share that login; tq prints a note about it.

## One client: `tq onboard`

`tq onboard [<name>]` sets up one client workspace and records its company tools. It is
`tq add` plus a wizard (the same look as `tq setup`) that asks, in order:

1. the identity email (git) and display name;
2. the git provider;
3. the cloud, plus `cloud.also`;
4. the data platform;
5. the agent CLIs;
6. the tracker: provider, scope, auth source (`cli`, `keychain`, `env` or `mcp`; the
   default is `cli` for github, azure-devops and jira, else `keychain`), and the variable
   name when auth is `env` (suggested as `<WS>_<PROVIDER>_TOKEN`);
7. chat (`slack`, `teams` or none);
8. email (`gmail`, `outlook` or none).

Every question can be skipped. The manifest is written with names only: an answer that
looks like a token is refused. The workspace is left untrusted.

Flags:

- `--from <yaml>`: answers from a file instead of the wizard. Keys follow the manifest
  (`name`, `base`, `display_name`, `git: {email, name, provider, user}`, `identities`,
  `cloud: {provider, also}`, `data_platform`, `agents`, `tracker`, `chat`, `email`, plus
  `color` and `permission_mode`). Unknown keys and token-shaped values are refused.
- `--base <dir>`: the base folder; the default is the only registered base.
- `--open-terminal`: with `--from`, open a new terminal in the folder afterwards. The
  wizard asks instead.

The wizard refuses to start in an agent session (`TENTAQLES_HEADLESS`, `CLAUDECODE`,
`CLAUDE_PLUGIN_ROOT`, `TQ_HUB_TASK`); use `--from` there. A new terminal opens with Windows
Terminal (`wt -d <folder>`) when present, otherwise with the desktop app's terminal logic.
tq never closes or kills a terminal, and opens none from an agent session.

Then it prints the next steps, for you to run:

```
Trust the manifest (a human step): tq allow acme
Log in: tq login acme claude
Log in: tq login acme az
  (azure-devops shares the az login: one sign-in covers both)
Tracker Jira: store a token in the OS keychain with tq secret set acme jira (it reads the token from a hidden prompt)
Connect Slack (chat, MCP only): add the "slack" MCP entry to the tq bundle catalog and to claude.bundle.mcp, run tq bundle sync acme, then authenticate it with /mcp in a Claude session
Then open Claude Code in the folder and run /tentaqles:onboard to verify every connection
```

- `tq allow <ws>` first.
- One `tq login <ws> <id>` per CLI identity; identities that share a config home print one
  login.
- The tracker credential: the CLI login printed above, `tq secret set <ws> <provider>`
  (hidden prompt), the variable where `tq dotenv` resolves it, or, for `mcp`, no background
  sync and `/tentaqles:sync` instead.
- For each chat or email provider: add its `mcp:` entry to the tq bundle catalog and to
  `claude.bundle.mcp`, run `tq bundle sync <ws>`, then authenticate with `/mcp`.
- Finally `/tentaqles:onboard` in a Claude session in the folder.

### `/tentaqles:onboard`

The plugin skill reads the manifest and walks through every provider in it. It prints the
login lines for you (`tq login`, `tq secret set`, `acli jira auth login`) and never runs a
login, handles a token, or opens `.claude.json` or the bundle catalog. It verifies with
`tq doctor --json` from the workspace folder, then one read-only call per loaded MCP
connector, and ends with a table of what is connected.

`tq doctor` checks the services:

- `tracker-ok`, `tracker-needs-session`, `tracker-no-adapter`, `tracker-auth-failed`: it
  resolves the credential and asks the adapter who is signed in. It shows the provider,
  the manifest's scope, the auth source and the account. It runs for the workspace of the
  current folder (`--verify all` for every workspace; skipped under `--verify off`, on the
  hook path, and for untrusted workspaces), with a 10 s budget per check ("timed out after
  10s"). An auth failure is redacted and cut to one line; the fix names the right login.
  If no `tracker-*` finding shows, run doctor from the workspace folder or use
  `--verify all`.
- `chat-ok` / `chat-mcp-missing`, `email-ok` / `email-mcp-missing` (warn): whether the
  provider's MCP server is listed, by name only, in the identity's `.claude.json` (top level
  and this folder's project entries), the identity's `.mcp.json`, or the workspace folder's
  `.mcp.json`. A name matches the catalog `mcp:` name, or contains the provider id as a
  whole token (`slack-mcp`). Ignore the warning for claude.ai connectors.

## Unattended runs

To let a bypass or auto Claude session run without tq's confirms in a demo
folder (see `CLAUDE-HOOK.md`, "Unattended mode"):

1. Optional: set `claude.permission_mode: bypass` in the manifest, so the
   workspace's sessions start in bypass mode.
2. `tq allow --bypass <ws>` - the separate trust step for bypass.
3. `tq unattended on [--project <dir>]` - the whole workspace, or one folder.

Check with `tq unattended status`; `tq unattended off` undoes step 3.

## Saved plan files and permissions

Because a plan file contains git identity metadata (name and email) for
every company, `tq setup`/the wizard write plan files (both `--write-plan`
and `~/.tentaqles/last-setup.yaml`) with owner-only permissions — `0600` for
the file, `0700` for any directory created to hold it — on platforms that
enforce POSIX permission bits. Windows does not enforce these bits, so the
file is still written there but without the same OS-level restriction.

## Extra clouds

A workspace allows the cloud CLI of `cloud.provider` only; any other cloud CLI
is blocked as wrong-cloud. To use a second cloud, add it to the manifest
afterwards and run `tq allow <ws>`:

```yaml
cloud:
  provider: azure
  also: [aws]
```

`also` must be a list. Accepted names are `azure`, `aws`, `gcp` and
`digitalocean`; the aliases `az`, `gcloud`, `do` and `doctl` work in both
`provider` and `also`. An unknown name, a scalar (`also: aws`) or the primary
cloud repeated is a load error, as is a repeat within `also`, or `also` with no
real `cloud.provider` (missing or `none`); `cloud.provider` itself must be one
of the names above or `none`. Each extra cloud (`digitalocean` via `doctl`)
gets its own private config home under `~/.tentaqles/identities/<ws>/`. If the
manifest has an `identities:` block that does not list that cloud's identity
(`az`, `aws`, `gcloud`, `doctl`), tq adds it implicitly, so the extra cloud is
never left on the shared default config. See
[CLAUDE-HOOK.md](CLAUDE-HOOK.md) for the guard rules.

## Data classification

A client manifest can carry a `data:` block that names the classes of personal
or financial data the client handles (`pii`, `financial`, `secrets`, ...). It
is not part of the setup plan: add it to the workspace's `.tentaqles.yaml`
afterwards and run `tq allow <ws>`, since editing the manifest changes its
trust hash. The block widens what the commit, push and PR-text guards detect.
See the `data:` section of [CLAUDE-HOOK.md](CLAUDE-HOOK.md#data-classification-data-block).
