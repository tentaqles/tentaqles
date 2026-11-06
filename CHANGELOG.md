# Changelog

Notable changes to the `tq` CLI and the docs of this repository. The Claude Code plugin
keeps its own log in [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md). Releases before 0.9.0
are described on the [GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

### Added
- The build loop, in `.claude/`: this repo's own development process, which doesn't ship. A
  router session dispatches tickets, each ticket is built in its own worktree and judged by
  agents that never saw the build, and a gate keeps the checks fixed once approved. It's
  portable to other projects: see `.claude/README.md` and `.claude/PORTING.md`.

### Fixed
- The `tq dotenv run` masking test finds a working bash on Windows (Git for Windows) even
  when the WSL launcher comes first on `PATH`, and skips with a reason when none works.

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
