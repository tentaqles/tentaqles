# Changelog

Notable changes to the `tq` CLI and the docs of this repository. The Claude Code plugin
keeps its own log in [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md). Releases before 0.9.0
are described on the [GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

### The signed integrity record (ADR-0004)

- **Trust is a signed record over the files that make the hook run.** `tq allow` writes
  `~/.tentaqles/trust/<ws>.json`: the manifest hash and the sha256 of every covered file
  (the protected manifest and the root marker, `settings.json#tq` (the `hooks`,
  `disableAllHooks`, `enabledPlugins`, `enabledMcpjsonServers`, `disableAllMcpServers`,
  `env`, `statusLine`, `apiKeyHelper`, `otelHeadersHelper`, `awsAuthRefresh`,
  `awsCredentialExport` and `forceLoginMethod` keys of the identity's Claude
  `settings.json`, and any key ending in `Helper` or `Command`; not `permissions`, and a
  theme or model change is no change), `installed_plugins.json#tq` (the tentaqles
  `installPath` and version), every file of the tentaqles plugin folder Claude Code loads
  (from `installPath`, else the highest cached version) except `skills/`, `agents/`,
  `templates/`, `tests/` and `__pycache__`, so its `bin/` is covered, the Codex
  `hooks.json` and the tq-owned block of its `config.toml`, every file under
  `~/.tentaqles/shims/`) under an HMAC-SHA256 signature with the id of the key. The tq
  binary on PATH is not covered. A covered file that is absent is recorded as absent and is
  a change when it appears. Keys are canonical (cleaned, links resolved, forward slashes,
  lower-cased on Windows), so another spelling of `TQ_HOME` verifies; only the roots are
  resolved, so a prompt in a signed workspace costs about 25 ms over a neutral folder on
  Windows. The key is 32 random bytes in the OS keychain (service `tentaqles`, account
  `integrity-key`, made the first time a human signs, never by a check); when the keychain is
  unavailable the key is the file `~/.tentaqles/integrity.key` (0600) and `tq doctor` warns
  `integrity-key-file`. A record signed with another key reports "integrity key changed".
- **Only a human signs.** Every signature (`tq allow`, `tq hooks verify --accept`, `tq deny`,
  `tq add` and `tq setup`, the re-sign after `tq settings render` or `tq bundle sync`,
  `tq migrate manifests`) passes the human-only check of ADR-0003 inside `internal/trust`;
  `tq allow` and `--accept` also confirm. `tq allow` renders the identity files first, for a
  workspace not yet trusted too, and signs once, after.
- **A mismatch is at least as strict as untrusted.** Every command and the hook verify
  through `resolve`, once per command: a changed, added or removed covered file, a record
  whose key or signature does not verify, makes the workspace untrusted with the reason
  naming the first changed file and `tq hooks verify <ws>`. The hook refuses every mutating
  git and every remote command (`git push`, `gh`, cloud CLIs) for it. `tq env` exports no
  identity and prints one fixed line at every prompt, "tq: integrity mismatch: run tq hooks
  verify <ws>" (a workspace never allowed: "tq: untrusted: run tq allow <ws>"; no path ever
  reaches the shell line); `tq run <ws>` and `tq run .` refuse and launch nothing; the
  session-start preamble and `tq doctor` report `integrity` and `untrusted` with every
  change.
- **`tq hooks verify [<ws>|--all] [--accept]`.** Prints the record's state, the key id,
  "plugin tentaqles 0.16.0 -> 0.17.0 (N hook files changed)" when the plugin moved to a new
  version folder, and each change with the old and new hash prefixes; exit 1 on a mismatch.
  `--accept --all` signs again, after one confirmation, the workspaces whose only change is a
  new plugin version whose folder is the published release byte for byte (the commit the
  tentaqles marketplace clone pins; each file's git blob compared), listed first under
  "Matches the published release tentaqles <version> (<sha12>)"; a file that differs, a file
  the release lacks, a release file missing, or no clone or pin to compare with, and the
  workspace is "not accepted" with the reasons, left for `--accept <ws>`. With the `integrity`
  finding the hook's deny says "integrity mismatch: run tq hooks verify <ws>", not
  `tq allow`.
- **`tq deny` leaves a signed tombstone** (`revoked: true`); tq never deletes a record.
- **Older trust markers** (`trust/<hash>`, `trust/<ws>/<hash>`) count until the first
  signature on the machine, then only while the signed snapshot `trust/legacy.json` taken
  then lists them, and never for a workspace that has a signed record: a marker planted later,
  or a deleted record with a marker in its place, is untrusted. The first signature also
  writes `trust/.signed`, never deleted: with it, a missing key or snapshot makes no marker
  count, so deleting the record, the snapshot and the key does not bring a marker's trust
  back. While a marker counts,
  `tq doctor` warns `integrity-record-legacy` and every prompt prints "tq: legacy trust
  record: run tq allow <ws> to sign hooks and manifest". `tq migrate manifests --apply`
  replaces a counting marker by a signed record (`rekey-trust`, journaled with the snapshot,
  restorable) and signs again the record of a workspace it moves only when that record
  verifies in full (`sign-integrity`).
- **Plugin:** `tq_hook.sh` no longer honours `TQ_BIN`; see `plugin/CHANGELOG.md`.

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
- **Real terminals pass.** The process tree proves a human by reaching an anchor: a
  terminal emulator (Windows Terminal, ConEmu, mintty, Terminal.app, iTerm2, GNOME,
  Konsole, kitty, WezTerm and the rest), a login shell, the desktop shell and its helpers,
  a session manager (`systemd --user`, `gnome-session-binary`, `plasma_session`,
  `loginwindow`), `sshd`, `mosh-server`, `tmux`, `screen`, `getty`, WSL's `init`, or an
  editor's integrated terminal (VS Code, Zed, Fleet, the JetBrains IDEs, also as `java`);
  Cursor and Windsurf are not anchors. Above the first anchor a dead, reused, looping or
  unreadable link ends the chain as a human's (the desktop shell's own parent is a dead pid
  on every Windows box). Git Bash under mintty counts as a terminal: its MSYS2 / Cygwin pty
  pipes are recognised and the confirmation is read from them.
- **`tq settings anchors`** lists the accepted terminals; `tq settings anchors add <exe>` and
  `remove <exe>` (human-only, confirmed) keep yours in `~/.tentaqles/anchors.yaml`. A
  refusal that cannot establish a human ancestry names every process it saw and prints the
  exact `add` line for the top-most one that is yours.
- A denial log that cannot be written never lets a command through: the refusal stands and
  the error adds `refusal could not be logged: <err>`.

### Self-protection, the hook's third category (PRD v2 §8.2, ADR-0005)

- **Three categories, one order.** `tq claude-hook pre-tool-use` evaluates the manifest's
  `blocked_commands` and the identity guard, then the secret rules, then self-protection;
  the first category that denies is the one reported. Every deny now reads
  `BLOCKED [<category>/<rule>]: ...` (`identity/wrong-cloud`, `secrets/tq/env-file-read`,
  `self-protection/tq/protected-path`); `--json` adds `category`. `tq/cross-client-mcp` is
  in the identity category and `tq/commit-secret` in secrets; `tq/guard-error` and the
  commit scan's ask are unchanged.
- **`tq/protected-path`.** A session can no longer read or write tq's state
  (`~/.tentaqles`: `config.yaml`, `trust/`, the manifests, the logs, `anchors.yaml`,
  `integrity.key`, `bundles/`, `providers/`, `shims/`; in an identity's Claude or Codex
  home `settings.json`, `settings.local.json`, `hooks.json` and `config.toml`), nor write
  the root marker `.tentaqles.yaml`, the installed tentaqles plugin
  (`plugins/cache/tentaqles/**` and the plugin registration), the tq binary or tq's OS
  keychain entry (`cmdkey`, `security`, `secret-tool` on service `tentaqles`). It covers
  the file tools, Codex's `apply_patch`, `mcp__*` inputs that carry a path, and the shell:
  redirects, copies, moves, deletes, `mkdir`, ACL and mode changes, links, `truncate`,
  `tee`, `Set-Content`, `sed -i`, interpreters' inline code, `[IO.File]::...`, `git -C` and
  a `cd` into the folder. Paths are canonical (symlinks, case on Windows and macOS, `~`,
  `$HOME`, `$env:USERPROFILE`, `%USERPROFILE%`, `$TQ_HOME`, `$CLAUDE_CONFIG_DIR`,
  `$CODEX_HOME`). The marker, the plugin's files and the binary stay readable; Claude's
  memory files and projects in the identity's config home stay writable.
- **`tq/human-only-command`.** The hook denies a shell line that runs a human-only tq verb
  (`tq allow`, `C:\tools\tq.exe hooks install`, `./tq init`, `go run ./cmd/tq deny`, behind
  `sudo`, `env` or a nested shell) before it runs; `tq doctor`, `tq list`, `tq env` and
  the other reads stay allowed.
- **Dry runs are free.** `tq migrate` without `--apply` and `tq settings render --dry-run`
  no longer refuse a session; `--apply` and a real render still do.
- **Every hook deny is logged** to `~/.tentaqles/denials.jsonl` (kind = category, layer
  `hook`, tool, command, rule, reason, workspace, pid, cwd); never an environment value or
  file content, and for a shell command no arguments at all: the `command` field is the
  verb (the first word, after `sudo`/`env` and `NAME=value`/`$env:` assignments are
  stripped) plus the one protected path or token the denial named, never the rest of the
  line — so a pasted password, an inline token, a flag value or a URL never reaches the log.
- **The path rule catches common spellings** of the §8.2 routes (it stays a text filter,
  a speed bump, not a sandbox): glued redirects (`x>path`), `${HOME}` expansion, a
  destructive `find` rooted at or above a protected path, `… | xargs rm`, `eval`/`iex` of
  a literal string, interpreters matched by prefix (`python3.12`, `node20`, `ruby`, `perl`,
  `php`, `deno`, `bun`) — their `-c`/`-e` code, any path argument, `ruby -i`/`perl -i`, and
  a here-doc, stdin or piped body that names a protected path; download, archive, export
  and convert programs (`curl -o`, `wget -O`, `7z -o`, `unzip -d`, `Invoke-WebRequest
  -OutFile`, `Invoke-RestMethod`, `Start-BitsTransfer`, `Expand-Archive`, `Export-* -Path`,
  `certutil`, `fsutil file`, `patch`, `sort -o`); an editor (`vim`, `code`); a recursive
  search (`grep -r`, `rg`, `findstr /s`, `Get-ChildItem -Recurse`, the Grep/Glob tools)
  rooted at the home folder above the state; and a PowerShell `-EncodedCommand` (any
  accepted prefix `-e`…`-enco`, and `-ec`; decoded from base64 UTF-16LE, an undecodable
  one denied). A glob matches a protected path only when it could expand to one (`rm *.log`
  in a workspace is free; `rm .t*`, `rm *` and `~/.tent*/config.yaml` are denied). A path
  built at run time (a `$var`, string concatenation, `Join-Path`, a pipe into a shell, an
  alias, a script on disk, or a write program tq does not list) is not caught; P1.4's
  signed record makes a change it misses visible at the next start.
- Plugin: the PreToolUse matcher gains `Glob` (guarded like `Grep`).
- **The tentaqles marketplace clone is write-protected.**
  `<config dir>/plugins/marketplaces/tentaqles/**`, P1.4's trust anchor for batch accepts,
  joins the plugin cache: writes, deletes and `git -C` into it are denied, reads allowed. A
  recursive search's `/`-prefixed root (`grep -r x /home/me`, an MSYS `/c/Users/me`) is now
  checked; only a `findstr`/`dir` switch (`/s`, `/i`, `/c:text`) is skipped.
- **One marker list.** `internal/markers` holds the harness environment markers;
  `TQ_HOME` is now ignored under any of the twelve (it used to be six).
- Plugin: see `plugin/CHANGELOG.md` (the fallback guard protects `~/.tentaqles` and the
  plugin cache; `client-settings` and `rollback` no longer write the manifest).

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
