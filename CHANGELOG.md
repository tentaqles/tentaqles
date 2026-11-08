# Changelog

Notable changes to the `tq` CLI and the docs of this repository. The Claude Code plugin
keeps its own log in [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md). Releases before 0.9.0
are described on the [GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

## [0.12.0] - 2026-11-08

- **`tentaqles drop <client>`** undoes a client as if it never existed: its identity folder (manifest, Claude config,
  logins), its browser shims, the marker and git identity in its folder, its git rule, and the clients folder's
  registration when no other client is left there. Your files stay; an emptied folder is deleted. It asks for the
  client's name typed back (`--yes` skips it) and refuses to run inside Claude Code.

## [0.11.0] - 2026-11-08

- **`tentaqles add` and `tentaqles remove`** change one part of a client's setup without reach's menu:
  `tentaqles add git azure-devops`, `tentaqles add cloud aws`, `tentaqles remove gh`, `tentaqles add browser chrome
  "Profile 2"`, `tentaqles set email dev@acme.example`. Categories: email, git, cloud, team, data, task, mail,
  browser, other; a value alone finds its category. With no value, the category's part of the form opens. They work
  on the current folder's client or `--client`, ask for a typed `yes` (`--yes` skips it), and refuse to run inside
  Claude Code.
- A manifest with no `identities:` list (the oldest format) implies `claude` and `gh`. The first change through
  reach, `add` or `remove` now writes that list out, so changing the git host (or `tentaqles remove gh`) really takes
  `gh` out of the client.

## [0.10.0] - 2026-11-08

Tentaqles is now an identity isolator and nothing more. The work done on top of 0.9.0 toward an agent guard and
tool onboarding was cut before it was released: connecting each client's tools through the agent could not keep up
with how often vendors change their URLs and sign-in methods.

### What it does

- **`tentaqles reach [<path>]`** makes a folder a client: a banner, a form (email, git host, clouds, team, data and
  task tools, email provider, browsers and profiles), and after a typed `yes` the client's manifest under
  `~/.tentaqles/identities/<client>/`, a marker in the folder, the folder's git identity, the client's Claude
  settings, PATH mask and browser shims, the tentaqles plugin in the client's Claude config, and tq's block in your
  shell profiles. In a folder that already is a client, reach changes its providers, browser or email through the
  same questions. It refuses to run from inside Claude Code.
- **A client's terminal**, in its folder and every subfolder, carries `CLAUDE_CONFIG_DIR` (its own Claude account),
  each CLI's config home, `BROWSER`, and a PATH without the CLIs of providers outside its scope, whose variables are
  unset. Leaving the folder restores what was there.
- **Every command is `tentaqles <command>`**, with `tq <command>` as its alias: the release ships both binaries
  (the same bytes), the installers install both, the shell hook defines both functions, and every help text,
  message and fix line names the `tentaqles` form. The profile block calls `tentaqles activate`; `tentaqles hooks
  install` (and `reach`) rewrites a block an older version wrote in place, keeping a backup of the profile.
- **The plugin's hook** runs on Bash and PowerShell calls only, and blocks a command under the wrong identity: a
  manifest's blocked commands, the wrong cloud, a remote command outside every client folder, a stale shell, the
  wrong `CLAUDE_CONFIG_DIR`, git email or gh user.
- **`tq run`** refuses a Claude session without the plugin, and from inside a session runs only the folder's client.

### Removed

- The signed trust record, `tq allow`, `tq deny` and `tq hooks verify`; the human-only checks.
- The policy layer: the provider-scope, secret and self-protection rules of the hook, the MCP and browser rules, and
  the denial log (`tq status --denials`).
- `tentaqles connect`, `tq status --json`, `tentaqles install`, the onboarding skill, and the terminal handover of
  reach.
- `tq bundle`, `tq migrate`, `tq uninstall`, `tq secret`, `tq secrets`, `tq dotenv`, `tq settings anchors`.
- From the catalog: the match lists other than `executables`, `subcommands` and `env`, and `connect`, `verify`,
  `mcp` and `blocked_commands_suggested`. From the manifest: `display_name`, `language`, `color`, `database`,
  `stack`, `chat`, `data_platform`, `agents`, `claude.bundle` and `mcp`, which tq no longer reads.
- A client whose full manifest still sits in its folder (the layout before 0.9.0) is no longer read: run
  `tentaqles reach` for it, or move the manifest to `~/.tentaqles/identities/<client>/manifest.yaml` and leave in
  the folder a `.tentaqles.yaml` that reads `schema: tentaqles-marker-v1` and `client: <client>`.
- Files an earlier version wrote under `~/.tentaqles` and no longer read (`trust/`, `denials.jsonl`, `bundles/`,
  `mcp-out-of-scope.json` and the like) can be deleted.

## [0.9.0] — 2026-11-04

tq is an identity isolator with secrets protection for developers who work across several
clients. One folder per client; the terminal, the agent sessions and the external tools
carry that client's identity and nothing else. The requirements are in `docs/prd.md`, the
trust boundaries in `docs/threat-model.md`, the hook's rule set in ADR-0001.

### The hook

- **One rule set, in every Claude permission mode:** the identity-drift block (a `git push`,
  `gh` or cloud command under the wrong workspace, in an untrusted folder or from a stale
  shell), the five secret rules (`.env` reads, environment dumps, credential-store reads, in
  file tools and in the shell), `tq/commit-secret` and `tq/cross-client-mcp`, with
  `tq/guard-error` as the fail-safe. Every rule decision is allow or block; the only
  confirmation is the fail-safe when the guard itself fails. Nothing a manifest, a global
  file or a repository file adds or removes.
- **Trust is human-only.** `tq allow <ws>` and `tq deny <ws>` refuse an agent session by its
  environment marker and never prompt. `tq add` and `tq setup` scaffold a workspace from a
  session but leave it untrusted and print the `tq allow` line. An edited manifest loses its
  trust until `tq allow` runs again.
- **Launchers.** `tq activate` defines `claude` and `codex` (always `tq run . -- ...`) and
  `claude-off` / `codex-off` (`tq run --off`: the plugin switched off through `--settings`,
  the identity env cleaned, no variable set; refused from an agent session). `TQ_ENABLED=0`
  switches shell activation off and nothing else.
- **Codex.** The identity's `config.toml` block pins `approval_policy = "on-request"` and
  `sandbox_mode = "workspace-write"`; a render replaces only what tq owns.

### Commands and doctor

- `tq doctor` warns `manifest-dropped-block` for a top-level manifest block tq does not read
  (remove it, then `tq allow <ws>`) and `state-dir-unused` for folders under `~/.tentaqles`
  tq does not use; both are warnings, never failures, and tq deletes nothing.
- The status line shows the client and the git email.
- `tq secret` keeps git-host and issue-tracker tokens in the OS keychain and never prints
  them; `tq secrets audit` lists secret-shaped values in agent config files; `tq dotenv run`
  runs a command with a `.env` loaded and every value masked in the output.

### Install and release

- With a logged-in `gh`, `install.ps1` and `install.sh` check the archive's checksum and its
  build provenance (`gh attestation verify`, signer: the release workflow) and refuse to
  install when either fails. Without `gh` they check the checksum only.
- The plugin installs from this repo's root marketplace, pinned to a release tag and its
  commit (`docs/RELEASING.md`). CI runs `go` on three OSes, `govulncheck`,
  `marketplace-pin` and `plugin`; every GitHub Action is pinned by commit sha.

### Upgrading an existing install

See `docs/MIGRATE.md`, "Upgrading an existing install": manifests need nothing, run
`tq settings render --all` once, and `tq doctor` tells you what else to tidy.
