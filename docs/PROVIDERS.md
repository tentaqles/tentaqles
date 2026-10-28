# Provider catalog

`tq` keeps each workspace's terminal identity for a given CLI (`gh`, `aws`,
`claude`, ...) pointed at a private, per-workspace config directory by
setting that CLI's own env vars. The mapping of "which CLI, which env vars,
how to install it, how to log in" is data: the **provider catalog**.

## Where it lives

- Built-in providers: `internal/providers/catalog/*.yaml`, embedded into the
  `tq` binary at build time (`//go:embed catalog/*.yaml`).
- User overrides / custom providers: `~/.tentaqles/providers/*.yaml` (or
  `$TQ_HOME/providers/*.yaml`). A file here with the same id as a built-in
  provider replaces it entirely; a new id adds a new provider. Written by
  `tq providers add` or by hand.

Load order: embedded catalog first, then user files layered on top by id.

## Schema

Each YAML file describes exactly one provider. The filename (without
`.yaml`) **must** equal the provider's `id`.

```yaml
id: gh                       # ^[a-z0-9][a-z0-9-]*$
name: GitHub                 # display name
category: vcs                # one of: cloud, vcs, data, deploy, pm, agent, chat, email, other
docs: https://cli.github.com # optional docs URL

cli:                         # omit or set to `null` for providers with no CLI
  command: gh
  version_args: ["--version"]

install:                     # per-OS install hints; all fields optional
  windows:
    winget: "GitHub.cli"
    scoop: "gh"
    note: "unverified package id"   # flag anything not personally verified
  macos:
    brew: "gh"
  linux:
    apt: "gh"

identity:                    # omit entirely for providers with no identity env
  env:
    GH_CONFIG_DIR: "{dir}"   # "{dir}" is replaced with the workspace's private config dir

login:                       # command tq runs for `tq login <id>`
  args: ["auth", "login"]    # command defaults to cli.command when omitted

verify:                      # command tq runs to check the identity is active
  args: ["auth", "status"]

blocked_commands_suggested:  # optional list of commands to warn/block on
  - "gh repo delete"

mcp: github                  # optional: the tq bundle catalog MCP entry (and mcpServers key)
                             # the agent reaches this provider through; default: the id
notes: "..."                 # optional prose for onboarding and doctor (how it authenticates)
```

### Rules

- `id` must match `^[a-z0-9][a-z0-9-]*$` and equal the filename.
- `category` must be one of `cloud, vcs, data, deploy, pm, agent, chat, email, other`.
- `mcp`, when set, must match `^[A-Za-z0-9][A-Za-z0-9._-]*$` (a server name, never a path
  or a command). `notes` must not hold control characters.
- Two entries may declare the same `identity.env` variable only when they are the same CLI
  with the same template (`az` and `azure-devops` both set `AZURE_CONFIG_DIR`); a workspace
  that enables both gets one directory, owned by the first id in sorted order.
- If `cli` is present, `cli.command` is required.
- `identity.env` values may use the literal placeholder `{dir}` (e.g.
  `{dir}/config`); it is replaced with the workspace's private identity
  directory and the result is normalized for the current OS
  (`filepath.FromSlash`). Values must not contain `..`.
- A provider with `identity.env` is one `tq` will export a private, isolated
  config home for. A provider without it (e.g. `jira`, `postgres`) is
  informational only — useful for `tq providers list/show`, but `tq` does not
  manage its identity.
- Unverified install package ids (found via research, not personally
  confirmed) still get an `id` field plus a `note: "unverified package id"`
  rather than being omitted.

## Company tools in the catalog

- `azure-devops`: the `az` CLI, with `AZURE_CONFIG_DIR` as its identity variable (shared
  with `az`). Login is `az login` (`az devops login` only stores a PAT), verify is
  `az account show` (account `user.name`, subscription `name`). Install hints add
  `az extension add --name azure-devops`.
- `jira`: the `acli` CLI (Atlassian CLI), login `acli jira auth login`, verify
  `acli jira auth status`, `mcp: atlassian`. It has no identity block: acli keeps one global
  login per OS user, not one per workspace, so tq does not claim to isolate it.
- `linear` and `asana`: no CLI. Their notes say to store a token with `tq secret set` (auth
  `keychain`) or name a `tracker.env` variable (auth `env`); the MCP connector covers a
  Claude session.
- New categories `chat` and `email`, with MCP-only providers `slack` and `teams` (chat),
  `gmail` and `outlook` (email). Each has an `mcp:` name. No bundle catalog entries ship
  for them: you add the entry to the tq bundle catalog and to `claude.bundle.mcp`, run
  `tq bundle sync <ws>`, then authenticate with `/mcp`.

## Company services in the manifest

A workspace's `.tentaqles.yaml` may name the company's tools. Every block is optional and
holds names only; a token-shaped value makes the whole file fail to load. Editing the
manifest changes its trust hash, so run `tq allow <ws>` after.

```yaml
tracker:
  provider: jira            # github | azure-devops | jira | linear | asana
  scope:                    # provider-specific keys; unknown keys are kept and passed on
    org: acme               # ado org / jira site / github owner / asana workspace / linear team
    project: PLAT
    board: Platform Team    # optional (ado team, github project number, asana project)
  assigned_to: me           # me (default) | anyone
  auth: cli                 # cli (default) | keychain | env | mcp: where the order starts
  env: ACME_JIRA_TOKEN      # the variable tq dotenv resolves; required with auth: env
  lanes:                    # optional overrides: tracker status -> hub lane (a name or a kind)
    "In Review": review
chat:  { provider: slack }  # slack | teams | none (MCP only)
email: { provider: gmail }  # gmail | outlook | none (MCP only)
data_platform: { provider: snowflake }   # any catalog data id, or none
agents: [claude, codex]     # catalog agent ids, each once
```

The loader checks provider names against these lists and the catalog, `assigned_to` and
`auth` against their values, `env` against `^[A-Za-z_][A-Za-z0-9_]*$`, and refuses control
characters in scope keys and values and lane overrides. `tq onboard`, the `tq setup` plan
and the desktop Services step write these blocks.

### Tracker scope per provider

The hub has background adapters for all five trackers (see [HUB.md](HUB.md#board-sync)).

**GitHub** (`provider: github`):

- `org`: the owner (user or organization) of the repository or project. Required.
- `project`: the repository name, without the owner. Required without a `board`.
- `board`: a Projects v2 number, optional. With it, an item's status is the project's
  single-select status field; without it, Open or Closed.
- `status_field`: the name of that field (default `Status`).

Item ids are `owner/repo#number`. Writes stay inside the scope: an owner other than
`scope.org`, or (without a board) a repository other than `scope.project`, fails before
any request. With `auth: cli`, tq runs `gh api` under the identity's `GH_CONFIG_DIR`, so
the token stays in gh's own store; gh never inherits the hub process's login, and is not
run at all when the identity sets no `GH_CONFIG_DIR`. Otherwise a keychain or env token is
sent to the API over HTTP.

**Azure DevOps** (`provider: azure-devops`):

- `org` and `project`: required.
- `board`: optional, the team the work item query runs under.
- `type`: the work item type for a top-level create (default `Task`).

With `auth: cli`, the token is `az account get-access-token` for the Azure DevOps resource,
run under the identity's `AZURE_CONFIG_DIR` (run `az login` for that identity). A keychain
or env PAT is sent as Basic auth.

**Asana** (`provider: asana`):

- `org`: the workspace gid.
- `board` (or `project`): the project gid. Its sections are the statuses: a move adds the
  task to the section named like the status and sets completed to whether that section is
  done.

There is no CLI: the credential is a keychain or env personal access token, sent as
`Bearer`. Comments are stories, children are subtasks, labels are workspace tags.

**Jira** (`provider: jira`):

- `org` (or `site`): the Atlassian Cloud site, `acme` or `acme.atlassian.net`. Required.
  Anything other than a `*.atlassian.net` site is refused, so the token only goes there.
- `project`: a project key, optional. Without it the site's assigned items are mirrored,
  and creating a card needs a parent.
- `email`: the account email, needed when the stored token is the API token alone.
- `issue_type`: the type for a top-level create (default `Task`); with a parent it is a
  sub-task in the parent's project.
- `acceptance_field`: a custom field id such as `customfield_10050` for acceptance criteria.
  Without it, acceptance is appended to the description under "## Acceptance".

A keychain or env credential is `email:token` (or the token alone plus `scope.email`), sent
as Basic auth. With `auth: cli`, tq uses `acli` only when it is installed, logged in
(`acli jira auth status`) and logged in to this workspace's site: it compares acli's
`Site:` with the host of `scope.org`. On a mismatch, or when acli names no site, every call
fails with a message saying to run `acli jira auth login` for the right site or to set
`tracker.auth` to `keychain` or `env`; `tq doctor` and `tq hub sync status` show it. Since
acli has one login per OS user, prefer keychain or env in a multi-client setup. On Windows
only `acli.exe` runs (a `.bat` or `.cmd` wrapper is refused and the next source is tried).
Under acli a command line over 30,000 characters is refused, and the tracker's status list
is unavailable, so lanes come from each item's status category plus `tracker.lanes`.

Behaviour: items assigned to you (`assigned_to: anyone` drops that clause), updated since
the watermark minus a 5-minute overlap; the first sync reads open items only. Comments and
descriptions are converted from markdown to Atlassian's document format. A move runs the
transition whose target status matches, case-insensitively; with none, the error lists
the available transitions. Labels with spaces are refused. Status category `new`,
`indeterminate` and `done` map to `todo`, `active` and `done`; anything else to `backlog`.

**Linear** (`provider: linear`):

- `team` (or `org`): a team key, name or id; a key match wins over a name. Required for
  creating cards, reading statuses and `assigned_to: anyone`, which without a team is
  refused (the whole workspace is never mirrored).

There is no CLI: the credential is a keychain or env token. A personal API key
(`lin_api_…`) is sent as is; an OAuth token as `Bearer`. With `assigned_to: me` (default)
the issues assigned to you are mirrored, plus their children and grandchildren; with
`anyone`, every issue of the team. The first sync reads open issues only. One read is
capped at 40 pages of 50 issues; past the cap the newest issues seen set the watermark and
the rest come in when they change again. Workflow state types map: triage and backlog →
`backlog`, unstarted → `todo`, started → `active` (`review` when the name contains
"review"), completed, canceled and duplicate → `done`. Acceptance criteria are appended to
the description as "## Acceptance criteria". A wait the server asks for is capped at 1 hour.

### Credentials and `tq secret`

Board sync resolves the tracker credential starting at `tracker.auth` and continuing in
the fixed order **cli → keychain → env → mcp**:

| Source | Where the token comes from |
|---|---|
| `cli` | the tracker CLI's own login: `gh` (GitHub), `az` (Azure DevOps), `acli` (Jira) |
| `keychain` | the OS keychain (Windows Credential Manager, macOS Keychain, Linux Secret Service), service `tentaqles`, account `<ws>/<provider>` |
| `env` | the `tracker.env` variable, resolved as `tq dotenv run` does: the workspace identity env, then the process environment, then `<workspace root>/.env` |
| `mcp` | none in the background: sync reports `needs-session`; use `/tentaqles:sync` in a Claude session |

A token is fetched for each request and never stored, logged or shown by tq.

```
tq secret set <workspace> <provider>          # hidden prompt on a terminal, or stdin when piped
tq secret rm <workspace> <provider>           # aliases: remove, delete
tq secret status [<workspace> [<provider>]] [--json]
```

- Providers: `github`, `azure-devops`, `jira`, `linear`, `asana`. The workspace must be
  registered (it shows in `tq list`); it need not be trusted yet.
- `set` reads up to 16 KiB, trims surrounding whitespace, refuses an empty token or one with
  control characters, and replaces any earlier token.
- `rm` is an error when no token is stored.
- `status` shows `present`, `absent` or `error`, never a value. With no arguments it covers
  every registered workspace with a tracker block; with a workspace, its tracker (or all
  five when it names none); with a provider, that one. Columns: WORKSPACE PROVIDER KEYCHAIN
  AUTH. `--json` gives `{workspace, provider, account, keychain, auth?, error?}` per row.
- All three are human-only: an agent session is refused ("run it in your own terminal").
- Not to be confused with `tq secrets audit` (plural), which is unchanged.

## Adding a provider

Either:

1. Run `tq providers add <id> --name "Name" --category <cat> [--command cmd]
   [--env KEY=VALUE ...] [--login "arg1 arg2"] [--verify "arg1 arg2"]` — this
   validates and writes `~/.tentaqles/providers/<id>.yaml` for you, or
2. Hand-write a YAML file following the schema above and drop it in
   `~/.tentaqles/providers/<id>.yaml` (user override) or
   `internal/providers/catalog/<id>.yaml` (built-in, requires a rebuild).

Run `go test ./internal/providers/...` after editing the embedded catalog to
confirm every file still parses and validates.
