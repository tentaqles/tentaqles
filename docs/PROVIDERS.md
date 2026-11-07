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
  human: the folder is under the protected tq home). See "A user file that does
  not load" below.

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
  hosts: [api.github.com, uploads.github.com, github.com/login/oauth/]
  mcp: [github, gh]
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

## Installing a provider's CLI

`tentaqles install <provider>` (and the last step of `tentaqles reach`, unless `--no-install`)
puts a provider's CLI on the machine. It reads only the **embedded** catalog: a file under
`~/.tentaqles/providers/` can add a provider or widen what the guard denies, never name a
package tq runs.

The methods of the `install:` block of the current OS are tried in this order, and the first
whose package manager is on the identity's PATH runs:

1. `winget`
2. `scoop`
3. `brew`
4. `apt` (runs `apt-get install -y`, never with `sudo`: without the rights it fails and says so)
5. `npm` (`npm install -g`)
6. `pip` (`pip install`, or `pip3`)

Then the catalog's `version_args` run and one line is printed:

| Line | When |
|---|---|
| `<provider>: installed <version>` | a package manager ran and the CLI now answers |
| `<provider>: already present <version>` | the CLI already resolves on the identity's PATH: nothing runs |
| `<provider>: manual, download from <docs URL>` | no method of this OS has a package manager here |
| `<provider>: install failed: <reason>` | the package manager failed (exit 1); the reason is its error line, not a progress line or a warning |
| `<provider>: no CLI, nothing to install` | `cli: null` |

Rules:

- The package id comes from the catalog and is one argv element; there is no shell and no
  argument of the command line reaches the package manager.
- A value under `url` is documentation. tq never opens or runs it, and **never launches an
  installer file (a `.msi`, an `.exe`, a script) itself**. `winget` may run a vendor installer
  behind the scenes: that is the package manager's own work.
- **tq accepts the vendor's licence through winget's flags** (`--accept-package-agreements`,
  `--accept-source-agreements`, `--disable-interactivity`). Run the install only for software
  you agree to.
- A run lasts at most 10 minutes. At the limit the package manager and everything it started are
  stopped, and the line reads `install failed: ... did not finish within 10m0s and was stopped`.
- The package manager and the version check run with the identity's environment. After a Windows
  install the CLI is looked up on the user and machine PATH read from the registry, since a
  package manager's PATH change does not reach a running process.
- A CLI installs machine-wide; the identity's PATH layer decides who reaches it. A provider
  outside the identity's scope is refused (`<provider> is outside the scope of identity <ws>:
  <scope>`, exit 1, one `identity` line in the denial log with layer `command`).
- An agent may run it for a provider in scope (it is not human-only; ADR-0015). Azure DevOps'
  `az extension add --name azure-devops` stays a printed note: an install `note:` that starts
  with `then: ` is printed after the provider's line (`then: az extension add --name
  azure-devops`) and never run; any other note is an install alternative and is not printed.
- The failure and refusal lines are printed as they are worded (no `tq:` in front) and the
  command exits 1. The failure reason is the package manager's error line, looked for in stderr
  then stdout; the last 48 KiB of each stream are kept, so a verdict after a long log is found.
- On Windows the CLI is also looked up on the registry PATH before any package manager runs, so
  a CLI an earlier install added (the same `az` under Azure and Azure DevOps in one `reach`, or
  a retry from the same terminal) reads `already present`.

## Match lists

Every catalog file carries a `match:` block: the literal spellings the guard's
identity category recognises as this provider when an identity's scope
(`manifest.Scope()`, see [CLAUDE-HOOK.md](CLAUDE-HOOK.md#identity-scope-pre-tool-use))
leaves it out (ADR-0007). Every list is
optional; validated at load.

| List | Entries | Matched as |
|---|---|---|
| `executables` | `^[A-Za-z0-9][A-Za-z0-9._+-]*$`, no suffix | the command word's last path element, lower-cased, suffix stripped; behind `sudo`/`env`/`xargs`/`wsl`/`find -exec`/`cmd start`; a runner's package (`npx`, `uvx`, `pipx run`, `uv tool run`, ...); `Start-Process`/`Get-Command`; a backslash-joined word (`a\ws`) |
| `subcommands` | `exe -> [first-subcommand, ...]` | a part of a CLI another provider owns wholesale (`az: [devops, repos, ...]`). The most precise in-scope claim wins |
| `images` | docker image names (`amazon/aws-cli`) | the image of `docker run`/`podman run` (basename or full) |
| `modules` | dotted Python names (`awscli`, `azure.cli`, `google.cloud`) | `python -m <module>` (past `-X`/`-W` option values), or a module named in `-c` code (`run_module('awscli')`, `import boto3`, `from google.cloud import storage`); a dotted child is covered |
| `psmodules` | PowerShell module names (`AWS.Tools`, `Az`) | an argument of `Import-Module`/`ipmo`, `Install-Module`, `Get-Module` and the other `*-Module`/`*-PSResource` verbs, or `using module`; a dotted child (`AWS.Tools.S3`) is covered |
| `cmdlets` | `Verb-Noun`, or `*-<NounPrefix>*` | a command verb: the exact name (`Set-AWSCredential`), or any verb whose noun starts with the prefix (`*-S3*` matches `Get-S3Bucket`); also `Get-Command <name>`; case-insensitive. Pick prefixes no core PowerShell noun starts with |
| `env` | variable names; a trailing `*` is a prefix (`TF_TOKEN_*`) | a reference: `$NAME`, `${NAME}`, `${#NAME}`, `$env:NAME`, `%NAME%`, `Env:NAME`, `Env:NAME*`, `NAME=`, `export`/`unset`/`set`/`setx`/`printenv`/`declare -p NAME`, `[Environment]::SetEnvironmentVariable('NAME')`, `environ['NAME']`, `getenv("NAME")`, `process.env.NAME`, `const {NAME}=process.env`, `Deno.env.get('NAME')`, `Bun.env.NAME`, Ruby `ENV["NAME"]`/`ENV.fetch("NAME")`, Perl `$ENV{NAME}`; case-insensitive. Not matched inside a single-quoted bash string, a grep pattern, a commit message or a here-string (those are text) |
| `paths` | `~/...`, `%APPDATA%/...`, `%LOCALAPPDATA%/...`, forward slashes, no `..` or glob | the path and everything under it, in a file tool (Read, Write, Edit, Glob, Grep, LS, ...); `%APPDATA%`/`%LOCALAPPDATA%` expanded in file-tool paths and shell words; a quoted segment (`$env:APPDATA\'GitHub CLI'`) read without its quotes; `tar -C <dir>` and a preceding `cd` set the base; a Glob/Grep `pattern`/`glob`/`path` and a recursive search above the tree. A root whose variable is unset is skipped |
| `hosts` | lower-case DNS names with a dot, optionally followed by a path prefix (`gitlab.com/api/`) | the host and every subdomain, in a URL anywhere in a shell line, a network verb's bare host, a WebFetch URL and a URL-shaped field of an MCP tool input (url, uri, endpoint, href, link, `*Url`, or a value that is one URL); with a path prefix, only URLs under it, after the path is normalised (dot segments, repeated slashes, percent-encoded unreserved characters). List **API hosts**, never a whole registrable domain that also serves public content: `api.github.com`, not `githubusercontent.com`; named `*.googleapis.com` API hosts, not `googleapis.com` with the public content in `public`; `gitlab.com/api/`, not `gitlab.com` |
| `public` | exact hosts, optionally with a path prefix (`fonts.googleapis.com`, `storage.googleapis.com/`) | exceptions to `hosts`: the vendor's public content under its API domain (fonts, a public GCS object, an installer CDN such as `awscli.amazonaws.com`). It wins over a `hosts` entry it matches at least as precisely (a longer host, then a longer path), so `storage.googleapis.com/storage/` stays an API path. A user file cannot add one |
| `git_hosts` | git hosts (`github.com`, `gitlab.com`) | `ssh`/`scp`/`sftp` to the host, and a git command (`push`, `fetch`, `pull`, `ls-remote`, `clone`, `remote`, `submodule`, `archive`) with a URL on it over the ssh transport (`git@host:path`, `ssh://`), or a `git push` over https. An anonymous https clone, fetch, pull or ls-remote is allowed |
| `mcp` | `^[a-z0-9]+$` tokens | a token of the MCP server name (split on `_ - .`, adjacent pairs joined: `google-cloud` → `googlecloud`). An MCP server is allowed only when its exact name is the identity's own `claude.bundle.mcp`; any out-of-scope token otherwise denies |

The same lists drive the layers before the hook (ADR-0008):

| List | Layer | Effect for a provider outside the scope |
|---|---|---|
| `executables` | PATH (`tq env`, `tq run`) | a PATH folder holding only these is dropped; a shared folder is kept and the identity's `path-mask/` stub answers `command not found` (exit 127) |
| `env` | environment (`tq env`, `tq run`) | unset in the identity terminal and launches, restored on leaving (`TF_TOKEN_*` unsets every match) |
| `mcp`, `mcp_packages`, `images`, `hosts`, `env`, `executables` | harness config (`tq settings render`) and the hook | an MCP server whose name token, package or image (`command`/`args`), `env` key, `url` host or `command` is the provider's is removed from the identity's `.claude.json` and Codex `config.toml` (kept in `mcp-out-of-scope.json`), and its `mcp__<server>__*` tools are denied |

`mcp_packages` (ADR-0008) lists prefixes of what an MCP server's config runs:
`awslabs.`, `@aws/`, `public.ecr.aws/awslabs/`, `@modelcontextprotocol/server-github`,
`ghcr.io/github/github-mcp-server`; matched case-insensitively against the command and
each arg (a version suffix included, a script under `node_modules/<pkg>/` by its package,
`-`, `_` and `.` alike, a `git+`/URL scheme removed, a path also by its file name).
`mcp_hosts` lists a vendor's remote MCP endpoints (`mcp.vercel.com`,
`mcp.cloudflare.com` and its subdomains): an MCP server whose url, or a URL among
its args (`mcp-remote <url>`), is on one is the provider's. It is read only by the
MCP layer and the hook's MCP rule, never by the network rules of `hosts`.

**The hybrid MCP list** (the owner's choice of 2026-11-05, ADR-0008). An
MCP server is kept only when the catalog attributes it to an
in-scope provider, when it is a general-purpose, provider-free server of
`internal/providers/mcp_general.yaml`, or when its exact name is in the
manifest's `mcp.allow`; every other server is moved to `mcp-out-of-scope.json`
at render and denied by the hook. `mcp_general.yaml` lists playwright, context7,
memory, filesystem, fetch, git, sequential-thinking, time, everything, postgres,
sqlite, puppeteer, chrome-devtools and microsoft-learn, each with the
`mcp_packages`, `mcp_hosts` and `images` its entries are matched by (a package
up to a version, path or tag; never a name alone) and `mcp` tokens for a
claude.ai connector. `mcp.allow` is a list of server names in the protected
manifest (letters, digits, `.`, `_`, `-`; each once), so only a human edits it.

An entry an in-scope provider also lists is never hidden, unset or removed. Two
rules of `internal/providers/generic.go` narrow the PATH and environment layers:
a generic tool (its CLI command is in none of its match lists: docker, kubectl,
terraform, npm, ...) never has its variables unset, and an ambiguous word
(`cn`, `tea`, `snow`, `render`, `vc`, `fly`, `atlas`, `copilot`, `goose`) is
never dropped from or masked on the PATH. After
editing a user provider file, run `tq settings render <ws>` so the PATH mask
holds the new names (until then the prompt says `tq: PATH mask missing`).

Rules of thumb for a new entry:

- List the vendor's CLI, its SDK modules (`boto3`, `google.cloud`, `stripe`), its
  PowerShell modules and cmdlets, its variables, credential files, API hosts
  (including per-project API hosts such as `*.supabase.co`, listed as
  `supabase.co`) and MCP server names. Leave out documentation sites and hosts
  that serve public downloads: a public download carries no credential, and the
  `env` and `paths` lists are what catch one sent with it.
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

### A user file that does not load

A file under `~/.tentaqles/providers/` that cannot be read, does not parse or
validate (a key the schema does not have included), sets another provider's
identity variable, or (for a new id) claims anything a built-in provider
lists, is not loaded. Every message names the file once, with the line and
column of a schema error, the first error and the fix:
`~/.tentaqles/providers/acme.yaml:5:3: unknown key "executable" in match (did
you mean "executables"?) (...); fix or delete it, then run tq providers list`.

- **It overrides a built-in id** (`gh.yaml`): the built-in entry and its match
  lists stay in force, and the hook prints a warning on stderr with every
  answer until the file is fixed.
- **It defines a new id**: calls are refused (`tq/guard-error`) only in a
  workspace whose scope names that id (its `scope:` list, an identity, the git
  provider); a manifest that names it reports the file in its load error.
  Other workspaces go on, with the same warning on stderr.
- **A new id may not claim a built-in's entries**: an executable, subcommand,
  image, module, PowerShell module or cmdlet, variable, path (or one under it),
  host (or one under it), git host, MCP host, MCP package (or one overlapping it)
  or MCP token a built-in provider lists. An in-scope
  provider's claims come from the built-in catalog, so such an entry would let
  an identity that scopes the new id use the built-in's CLI. To extend a
  built-in, name the file for its id instead.
- `tq providers list` prints the catalog, every overlap between providers (the
  same entry, or a host under another's host) and every file it did not load,
  and exits non-zero when one did not load. `tq doctor` reports each one as
  `provider-file-invalid` (a warning for an override, an error for a new id,
  naming the workspaces whose scope names it).

## Company tools in the catalog

- `azure-devops`: the `az` CLI, with `AZURE_CONFIG_DIR` as its identity variable (shared
  with `az`). Its match claims only part of `az`: `az devops|repos|pipelines|boards|
  artifacts|login|extension|account|config|version` and the bare `az --version`/`az -h`. So
  an identity with `azure-devops` but not `az` runs `az account show`, `az account
  get-access-token` (for any resource: Azure DevOps's, and also ARM's, so a logged-in
  account can mint an ARM token), `az config set` and `az login`, while `az vm`, `az group`
  and `~/.azure` stay denied. Login is `az login` (`az devops login` only stores a PAT), verify is
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
