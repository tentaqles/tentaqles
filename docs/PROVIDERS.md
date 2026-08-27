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
category: vcs                # one of: cloud, vcs, data, deploy, pm, agent, other
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
```

### Rules

- `id` must match `^[a-z0-9][a-z0-9-]*$` and equal the filename.
- `category` must be one of `cloud, vcs, data, deploy, pm, agent, other`.
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
