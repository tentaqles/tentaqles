# The provider catalog

A client's terminal points each CLI at a config home of the client's own by setting that CLI's own variables
(`GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`, ...), and hides the CLIs of providers the client doesn't use. Which CLI,
which variables, how to install it, how to log in and what to hide is data: the provider catalog.

## Where it lives

- The built-in providers: `internal/providers/catalog/<id>.yaml`, embedded in the binary.
- Your own: `~/.tentaqles/providers/<id>.yaml` (`$TQ_HOME/providers`), written by `tq providers add` or by hand.
  A file with the id of a built-in replaces it, except its `match` lists, which are added to the built-in ones: an
  override can hide more, never less. A file with a new id adds a provider, and may not claim a CLI, subcommand or
  variable a built-in lists (extend the built-in instead, with a file named for its id).

`tq providers list` shows the catalog and any file that did not load, with the reason; `tq providers show <id>`
prints one provider; `tq providers check` tells which CLIs are installed.

## Schema

One provider per file; the file name without `.yaml` is the `id`. Unknown keys are refused, with the nearest known
key named.

```yaml
id: gh                         # ^[a-z0-9][a-z0-9-]*$
name: GitHub
category: vcs                  # cloud, vcs, data, deploy, pm, agent, chat, email, other
docs: https://cli.github.com   # optional

cli:                           # omit, or null, for a provider with no CLI
  command: gh
  version_args: ["--version"]

install:                       # per-OS install hints, shown by tq doctor when the CLI is missing
  windows: { winget: "GitHub.cli", scoop: "gh" }
  macos:   { brew: "gh" }
  linux:   { apt: "gh" }       # also pip, npm, url, note

identity:                      # omit for a CLI with no per-client config home
  env:
    GH_CONFIG_DIR: "{dir}"     # {dir} is ~/.tentaqles/identities/<client>/<id>
  caveat: "..."                # optional: what the variable does not move

login:                         # what tq login <client> <id> runs; command defaults to cli.command
  args: ["auth", "login"]

notes: "..."                   # optional, shown by tq providers show

match:                         # what a client's terminal hides when this provider is outside its scope
  executables: [gh, hub]       # masked on the client's PATH; no Windows suffix
  subcommands:                 # a claim on part of another provider's CLI
    az: [devops, repos]        # (azure-devops owns these az subcommands, so az stays on the PATH)
  env: [GH_TOKEN, GITHUB_TOKEN, "GH_*"]   # unset in the client's environment; a trailing * is a prefix
```

Two providers may set the same `identity.env` variable only when they are the same CLI (`az` and `azure-devops`
both set `AZURE_CONFIG_DIR`, so both log in to one folder).

## The scope and the PATH

A client's scope is the providers its manifest names: its `identities`, the clouds of its `cloud` block, its
`git.provider`, its `email.provider` and its `scope` list (what `tentaqles reach` writes). For every provider outside
the scope:

- each of its `executables` is hidden from the client's PATH: a PATH folder that holds only such CLIs is dropped,
  and one shared with other tools gets a stub of the same name in the client's mask folder, earlier on the PATH,
  that prints `<name>: command not found` and exits 127. `tq run` refuses the name however it is spelled;
- each of its `env` variables is unset, unless a provider in scope claims the same variable.

A provider in scope always wins: a CLI it claims, even by one subcommand, stays. A generic tool, whose CLI is not in
its own `match.executables` (docker, kubectl, terraform, npm, helm), is never hidden and its variables are never
unset: it talks to whatever the project configures. A few short names that other programs also use (`cn`, `tea`,
`snow`, `render`, `vc`, `fly`, `atlas`, `copilot`, `goose`) are never hidden either.

What a variable does not move is the provider's `caveat`: `aws sso login` tokens stay in the shared
`~/.aws/sso/cache`, and the token `terraform login` stores stays in the shared `terraform.d` folder.
