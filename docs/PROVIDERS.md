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
- `linear` and `asana`: no CLI. Their notes say to store a token with `tq secret set`; the
  MCP connector covers a Claude session.
- New categories `chat` and `email`, with MCP-only providers `slack` and `teams` (chat),
  `gmail` and `outlook` (email). Each has an `mcp:` name. No bundle catalog entries ship
  for them: you add the entry to the tq bundle catalog and to `claude.bundle.mcp`, run
  `tq bundle sync <ws>`, then authenticate with `/mcp`.

## Company services in the manifest

A workspace's `.tentaqles.yaml` may name the company's tools. Every block is optional and
holds names only; a token-shaped value makes the whole file fail to load. Editing the
manifest changes its trust hash, so run `tq allow <ws>` after.

```yaml
chat:  { provider: slack }  # slack | teams | none (MCP only)
email: { provider: gmail }  # gmail | outlook | none (MCP only)
data_platform: { provider: snowflake }   # any catalog data id, or none
agents: [claude, codex]     # catalog agent ids, each once
```

The loader checks provider names against these lists and the catalog. `tq onboard` and the
`tq setup` plan write these blocks. A top-level block the loader does not know is not read:
`tq doctor` reports it as `manifest-dropped-block` (a warning), and removing it changes the
manifest hash, so run `tq allow <ws>` afterwards.

### `tq secret`

`tq secret` keeps one git-host or issue-tracker token per workspace and provider in the OS keychain (Windows
Credential Manager, macOS Keychain, Linux Secret Service), service `tentaqles`, account
`<ws>/<provider>`. tq reads a stored token only to tell whether it is there (`tq secret
status`); it never prints one or sends it anywhere. The keychain entry is yours to use.

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
  every registered workspace, for every provider; with a workspace, every provider, or the
  one named. Columns: WORKSPACE PROVIDER KEYCHAIN. `--json` gives
  `{workspace, provider, account, keychain, error?}` per row.
- All three are human-only: an agent session is refused ("run it in your own terminal").
- Not to be confused with `tq secrets audit` (plural), which lists agent-config files holding
  secret-shaped values and `.env` files git would commit, by path and line only.

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
