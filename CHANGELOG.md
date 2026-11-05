# Changelog

Notable changes to the `tq` CLI and the docs of this repository. The Claude Code plugin
keeps its own log in [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md). Releases before 0.9.0
are described on the [GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

### The manifest moves under `~/.tentaqles` (ADR-0002)

- **Protected manifest location.** `tq add`, `tq onboard` and `tq setup` write the manifest
  to `~/.tentaqles/identities/<ws>/manifest.yaml` (0600 in a 0700 folder) and leave a
  name-only marker at `<root>/.tentaqles.yaml` (`schema: tentaqles-marker-v1`,
  `client: <ws>`). The guard and every command read only the protected copy; a marker
  whose `client` is not the folder name, or whose protected copy is missing, resolves
  neutral with the reason, and so does a root file that is not a regular file (a symlink)
  or a manifest whose `client` is not the folder name: a trusted manifest copied into
  another folder lends it nothing. `tq allow` and `tq deny` work as before; editing the
  marker never changes the hash.
- **Trust is keyed by workspace name and content.** A record is
  `~/.tentaqles/trust/<ws>/<hash>`; the same bytes trusted for one workspace are not
  trusted under another name. A record of an older tq (`trust/<hash>`) still counts for
  its workspace until `tq migrate manifests` rekeys it (`rekey-trust`, journaled);
  `tq deny` removes both forms.
- **`TQ_HOME` is ignored in an agent session.** Under an agent marker tq reads
  `~/.tentaqles` whatever `TQ_HOME` says, so a settings `env` block cannot point the hook at
  a forged state folder; `tq doctor` warns `tq-home-override` when `TQ_HOME` points away
  from the default.
- **Existing identities keep working.** A full manifest still at `<root>/.tentaqles.yaml`
  resolves exactly as before; `tq doctor` warns `manifest-in-root` naming the migration,
  and `manifest-root-stale` when both files exist (the protected copy is the one read).
- **`tq migrate manifests`** (also `tq migrate --steps manifests`; never a default step):
  a dry run by default; `--apply` is human-only, copies each manifest byte for byte (same
  hash, so trust carries over), rewrites the root file as the marker and journals both
  writes; `tq uninstall --restore` puts the manifest back in the root.
- **The state folder is private to the user.** 0700 directories (an existing
  `identities/<ws>/` is tightened when its manifest moves in) and 0600 files on macOS and
  Linux; on Windows `tq init`, `tq add` and `tq migrate manifests --apply` run `icacls`
  on `~/.tentaqles` once, granting the user and SYSTEM, record the outcome in
  `protected-acl.json`, and `tq doctor` reports `protected-acl`; an icacls failure never
  fails a command.
- Plugin: see `plugin/CHANGELOG.md` (the loader resolves the marker; the snapshot guard
  reads `tool_input.file_path`).
### Human-only commands (PRD v2 §8.1, ADR-0003)

- **Three detectors, one confirmation.** Every command that writes under `~/.tentaqles`
  or an identity config dir is human-only: `tq init`, `tq add`, `tq allow`, `tq deny`,
  `tq hooks install|remove`, `tq settings render`, `tq bundle sync|capture`, `tq migrate`,
  `tq uninstall`, `tq setup` (wizard and `--from`), `tq onboard` (wizard and `--from`),
  `tq secret set|rm`, `tq providers add`, `tq login`, `tq run --off` and
  `tq worktrees prune`. Each refuses, with no prompt, when stdin or stdout is not a
  terminal, when a harness or subagent (`claude`, `codex`, `opencode`, `kimi`, or a shell or
  runtime such as `node`, `bun`, `deno`, `npx`, `bash`, `pwsh` or `cmd` whose command line
  names one) is in the process tree, when the chain cannot be followed to a terminal, a
  login shell or the desktop shell (an orphaned child of a dead harness, a reused pid, an
  unreadable command line, a service), or when the environment carries a harness
  marker (the earlier list plus `TENTAQLES_HEADLESS_CHILD`, `CLAUDE_CODE_ENTRYPOINT`,
  `CLAUDE_CODE_SSE_PORT`, `CODEX_THREAD_ID`, `OPENCODE`, `KIMI_CODE`). The error names the
  command and the detector. A human then sees one line saying what is about to be written
  and types `yes` at `Type yes to continue:`; anything else exits 1 with nothing written.
  The logic lives in the new `internal/humanonly` package.
- **Denial log.** Every refusal appends one JSON line to `~/.tentaqles/denials.jsonl`
  (time, kind `human-only`, command, reason, pid, cwd; never an environment value).
- **`--yes` no longer skips the confirmation.** `tq setup --yes` is accepted for older
  scripts and asks anyway; `tq uninstall --restore --yes` and `tq worktrees prune --yes`
  still select the replay or the removal over the listing, then ask.
- **`tq add` no longer scaffolds from a session.** It used to create the folder untrusted;
  it now refuses, and a confirmed human run trusts the folder. `tq onboard --from` and
  `tq setup --from` refuse a session the same way.
- **`tq secret set`** reads the token from the hidden prompt only (a piped stdin is not a
  terminal). **`tq secret status`** is free to run from a session: it prints presence,
  never a value.
- **`tq doctor`** reports a `human-only` check naming the detectors this OS has; a warning
  on an OS whose process-tree walker is stubbed.

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
