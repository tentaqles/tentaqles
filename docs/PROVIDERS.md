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
  provider replaces it, except its `match:` lists, which are added to the
  built-in ones (an override never shortens what the guard recognises); a
  new id adds a new provider. Written by `tq providers add` or by hand (a
  human: the folder is under the protected tq home).

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

match:                       # what the guard denies when an identity's scope leaves gh out
  executables: [gh]
  env: [GH_TOKEN, GITHUB_TOKEN, GH_CONFIG_DIR]
  paths: ["~/.config/gh", "%APPDATA%/GitHub CLI"]
  hosts: [api.github.com, uploads.github.com]
  mcp: [github]
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

## Match lists

Every catalog file carries a `match:` block: the literal spellings the guard's
identity category recognises as this provider when an identity's scope
(`manifest.Scope()`, see [CLAUDE-HOOK.md](CLAUDE-HOOK.md#identity-scope-guard-v2-pre-tool-use))
leaves it out and the manifest sets `guard: v2` (ADR-0007). All six lists are
optional; validated at load.

| List | Entries | Matched as |
|---|---|---|
| `executables` | `^[A-Za-z0-9][A-Za-z0-9._+-]*$`, no suffix | the command word's last path element, lower-cased, suffix stripped; behind `sudo`/`env`/`xargs`/`wsl`/`find -exec`/`cmd start`; a runner's package (`npx`, `uvx`, `pipx run`, `uv tool run`, ...); `Start-Process`/`Get-Command`; a backslash-joined word (`a\ws`) |
| `subcommands` | `exe -> [first-subcommand, ...]` | a part of a CLI another provider owns wholesale (`az: [devops, repos, ...]`). The most precise in-scope claim wins |
| `images` | docker image names (`amazon/aws-cli`) | the image of `docker run`/`podman run` (basename or full) |
| `modules` | dotted Python names (`awscli`, `azure.cli`) | `python -m <module>` (past `-X`/`-W` option values), or a module named in `-c` code (`run_module('awscli')`, `import boto3`) |
| `env` | variable names; a trailing `*` is a prefix (`TF_TOKEN_*`) | a reference: `$NAME`, `${NAME}`, `${#NAME}`, `$env:NAME`, `%NAME%`, `Env:NAME`, `Env:NAME*`, `NAME=`, `export`/`unset`/`set`/`setx`/`printenv`/`declare -p NAME`, `[Environment]::SetEnvironmentVariable('NAME')`, `environ['NAME']`, `getenv("NAME")`, `process.env.NAME`, `const {NAME}=process.env`; case-insensitive. Not matched inside a single-quoted bash string, a grep pattern, a commit message or a here-string (those are text) |
| `paths` | `~/...`, `%APPDATA%/...`, `%LOCALAPPDATA%/...`, forward slashes, no `..` or glob | the path and everything under it; `%APPDATA%`/`%LOCALAPPDATA%` expanded in file-tool paths and shell words; `tar -C <dir>` and a preceding `cd` set the base; a Glob/Grep `pattern`/`glob`/`path` and a recursive search above the tree. A root whose variable is unset is skipped |
| `hosts` | lower-case DNS names with a dot | the host and every subdomain; use the registrable domain where the vendor owns it all (`googleapis.com`, `githubusercontent.com`) |
| `mcp` | `^[a-z0-9]+$` tokens | a token of the MCP server name (split on `_ - .`, adjacent pairs joined: `google-cloud` → `googlecloud`). An MCP server is allowed only when its exact name is the identity's own `claude.bundle.mcp`; any out-of-scope token otherwise denies |

Rules of thumb for a new entry:

- List the vendor's **own** CLI, variables, credential files, API hosts and MCP
  server names. Leave out what an application uses at run time (an
  `OPENAI_API_KEY`, a `STRIPE_SECRET_KEY`, an app's `*.supabase.co` URL) and
  documentation sites.
- A generic cross-cloud tool (docker, helm, kubectl, terraform, pulumi, npm,
  psql, dbt) lists no executable, and no credential `paths` either: a
  kubeconfig, an npmrc or a docker config is the developer's own cross-cloud
  file and stays readable under every identity (so an AKS developer needs no
  `scope: [kube]`). Only its SaaS endpoints and vendor-specific tokens are
  claimed. A command name too generic to
  deny (cursor's `agent`) is left out too.
- An entry another provider also lists is fine: whatever an in-scope provider
  claims is never denied (`az` is az's and azure-devops's).
- To extend a built-in provider, copy its file to `~/.tentaqles/providers/`
  and add the entries (the rest of the file still replaces the built-in entry,
  so keep its `identity`, `cli` and `login`; the `match:` lists are merged
  either way). To stop the guard matching a provider, add it to the
  identity's `scope:`.

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
tq secret set <workspace> <provider>          # hidden prompt on your terminal (a piped stdin is refused)
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
- `set` and `rm` are human-only (ADR-0003): a session is refused ("run it in your own
  terminal"), by its agent marker, by a harness in the process tree or by a non-terminal
  stdin or stdout, and you type `yes` before the keychain changes. `status` prints
  presence only, so it runs anywhere.
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
