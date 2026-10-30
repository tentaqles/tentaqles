# Changelog

Notable changes to the `tq` CLI, the desktop app and the docs of this repository.
The Claude Code plugin keeps its own log in [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md).
Releases before this file are described in the GitHub releases (up to `v0.7.0`).

## [Unreleased]

### Packs (specs C and D, releases 3 and 4)

`tq pack`: checks, guards, reviewers and approvals for data, ml, cloud and db work. See
[`docs/PACKS.md`](docs/PACKS.md) and [`docs/packs/`](docs/packs/).

- **`tq pack`** `check`, `list`, `doctor`, `crew`, `approvals`, `baseline`, and the
  `data` (`cost estimate`), `ml` (`promote request|apply|status`, `drift`), `cloud`
  (`plan`, `show`, `approve`, `prune`) and `db` (`approve`, `drill`) subcommands. Only the
  trusted manifest's `packs:` block enables a pack; a repo `tq-packs.yaml` adds to it under
  per-key merge rules and never loosens it.
- **Guard modes.** Every pack guard and pack rule has a category: cloud apply/destroy
  `cloud-destructive`, db migrate/restore `db-destructive`, `tq/iac-identity-drift`
  `identity`, `tq/plan-read` and `tq/plan-commit` `secrets`, the rest `packs`.
- **New core rules, always on with no pack enabled, in the locked groups:** `tq/hub-store-write`
  (now a core rule; `tq guard list` no longer shows it as owned by cloud or db),
  `tq/human-answer`, `tq/pack-config-change` and `tq/secret-store-shell`. A hook-time
  check denies agent shell and file access to `$TQ_HOME` `hub/`, `packs/`, `build-gate/`,
  `trust/`, `guard/`, `guard.yaml`, `config.yaml` and `audit.jsonl`, in any spelling; only
  a call of the running tq with a subcommand that never runs a child is exempt.
- **Pack approvals are answered from the terminal only:** `tq hub answer` needs a TTY
  and the typed binding, the answer is signed with a key in the OS keychain, and the board
  refuses it. `packs-keychain-missing` in `tq pack doctor` means no approval can be
  granted on that machine.
- **Unknown global flags floor to the strictest environment** in the cloud and db gates
  (the supabase CLI; terraform/tofu, pulumi, cdk/cdktf, az, aws and sam). Put the flag
  after the subcommand, or report the missing flag (see `docs/PACKS.md`, Known behaviours).
- **Telemetry opt-outs:** every pack tool gets `DO_NOT_TRACK=1`; dbt, tflint, trivy, dvc
  and dagster also get their own switches. sqlfluff, squawk, conftest, cfn-lint, bicep,
  datacontract, ruff, uv and poetry have no opt-out tq could confirm and get only
  `DO_NOT_TRACK=1`.
- **Build-gate approve now runs `tq pack list`,** even with no pack configured: it is
  refused when the manifest is untrusted or invalid, or `tq-packs.yaml` has problems.
- **Tools get a filtered environment;** a credential reaches a tool only through a name its
  check declares, and never a repo-local binary.

### Guard modes (spec E)

One knob for how strict tq's guard is: a **guard mode** per workspace, project folder
or machine. See [`docs/CLAUDE-HOOK.md`](docs/CLAUDE-HOOK.md#guard-modes).

- **Five modes.** `default` (every rule at its own action, what applies when nothing is
  set), `relaxed` (every toggleable category skipped; `git-history` and `system` ask),
  `strict` (`prod-changes`, `cloud-destructive` and `git-history` deny; `external-writes`
  asks), `custom` (a base preset plus per-category values) and `off` (no tq at all, for
  one launch or one window; never stored in a file).
- **Thirteen categories.** Every rule, built-in, Jev, hook, custom or pack, has one. Four
  are locked and hold in every mode: `identity`, `secrets`, `self-protection` and
  `privacy`. Custom rules may declare `category:`; without one they always apply.
- **Precedence and grants.** Global `guard.yaml`, then the workspace manifest, then
  `guard.projects.<dir>`, then the per-launch `TQ_MODE` / `--tq-mode`; a repo's
  `.claude/tq-rules.yaml` only tightens. A mode looser than default needs a one-time,
  hash-keyed grant: `tq allow --mode <ws> relaxed|custom` (`--global` for `guard.yaml`).
  Without it the session runs at default.
- **`tq mode`.** Show the resolved mode (`--json`, `--agent codex`), and the human-only
  `tq mode set|toggle|override|edit` (they refuse agent sessions and need a terminal), plus
  `tq mode log`.
- **Launchers.** `tq activate` now defines `claude`, `codex`, `claude-off`, `codex-off` and
  a `tq` function with `tq off` / `tq on` (bash, zsh, fish, PowerShell 7 and 5.1).
  `tq run .` uses the current folder's workspace; `tq run --tq-mode <m>` and
  `claude --tq-mode <m>` set the launch mode; `tq run --off -- claude|codex` starts with
  no tq (main install, tentaqles plugin switched off, bypass). `tq env --off` / `--on`
  print the shell code. Every tq hook exits at once under `TQ_MODE=off`.
- **Always bypass.** The `claude` launcher starts Claude with
  `--dangerously-skip-permissions`, so tq's mode is the single policy layer.
  `claude.permission_mode` still drives `permissions.defaultMode` for sessions started
  elsewhere. `tq settings render` writes `skipDangerousModePermissionPrompt: true`, and moves
  `gh repo delete` from the settings deny list to ask when git-history is loosened.
- **New rules.** `tq/nested-agent-loosened` (deny, self-protection) stops an agent from
  starting another agent in off mode or with loosened flags; `tq/mode-change` (deny,
  self-protection) blocks the mode writers in tool calls; `tq/outside-path-write` (ask),
  `tq/elevation` (ask) and `tq/system-dir-write` (deny) form the `system` category.
- **Controls.** A SessionStart line (`Guard: relaxed (workspace, granted) · … · locked:
  identity, secrets, self-protection, privacy`), a statusline segment
  (`tq: relaxed · 4 skipped`, `tq: off`), a desktop **Guard** tab, and a read-only `/guard`
  page on the hub, linked from the nav.
- **Hub runs** use `guard.headless_mode` (default when absent), clamped by the grant.
- **Codex.** The mode is compiled into the Codex identity: launch flags, a `config.toml`
  block, `rules/tq.rules` (exec-policy `prompt` / `forbidden`) and tq's `hooks.json`
  entries (`tq claude-hook … --agent codex`). Codex hooks cannot ask, so a confirm Codex
  cannot prompt for becomes a block. Trust tq's hook once in Codex's `/hooks`. The hook
  runs the file and path rules on every file an `apply_patch` writes, and blocks a patch
  it cannot read; a guard error is a block in Codex.
- **Doctor.** `mode-not-granted`, `mode-unknown-rule`, `mode-cloud`,
  `guard-locked-override`, `claude-alias-shadow`, `permission-mode-overridden`,
  `codex-hooks-missing`, `codex-hooks-disabled`, `codex-hooks-untrusted`.
- **Audit.** Skipped and lowered decisions go to `$TQ_HOME/guard/mode.jsonl`, with the tq
  mode, Claude's mode and the action applied.

### Plugin distribution from release tags

- **Root marketplace.** `.claude-plugin/marketplace.json` (marketplace `tentaqles`) installs
  the plugin from `plugin/` of this repo at a release tag and its commit sha (a `git-subdir`
  source; with both set Claude Code checks out the sha). It is added after the release, in a
  post-release PR, so `main` only names a tag that exists and passed. Then
  `/plugin marketplace add tentaqles/tentaqles` and `/plugin install tentaqles@tentaqles`
  replace the `tentaqles/tentaqles-plugin` repo. CI (`marketplace-pin`) fails unless the
  pinned sha is the tag's commit and on `main`; the release workflow fails a tag that is not
  on `main` or whose plugin versions disagree. Steps in [`docs/RELEASING.md`](docs/RELEASING.md).
- **Bundle catalog marketplace fields.** A catalog marketplace takes `ref` (branch or tag) and
  `path` (marketplace.json inside the repo) for `github`, the new `git` kind (`url`, `ref`,
  `path`), and `autoUpdate: true`, written beside `source` in `extraKnownMarketplaces`.
  `tq bundle capture` reads them back (autoUpdate from `settings.json`), `tq bundle diff`
  reports a changed ref or autoUpdate as `marketplace-source` drift
  (`github tentaqles/tentaqles@v0.8.0 autoUpdate`), and the catalog check warns on a github
  source without `repo` or a git source without `url`, and on a `git`/`url` URL carrying
  userinfo or a token. It refuses (and `sync` will not write) a `git` url other than
  `https://`, `ssh://` or `user@host:path`, and a `ref` git could read as an option or a
  revision expression. Drift details strip URL userinfo.
- **Bundle marketplace aliases.** A `claude.bundle.marketplaces` item `key=catalogName`
  writes catalog entry `catalogName` under settings key `key`, so one workspace can load a
  plugin from a local folder under the same plugin id (`tentaqles=tentaqles-dev`, plugins
  still `tentaqles@tentaqles`). An unknown `catalogName` is an error naming both.

### Deprecated

- Unattended mode is replaced by `relaxed`. `guard.unattended` is still read (in memory;
  rewritten by the next `tq mode` write), `tq unattended on|off|status|log` and
  `tq allow --bypass` are deprecated aliases of `tq mode set relaxed|default`, `tq mode`,
  `tq mode log` and `tq allow --mode <ws> relaxed`, and legacy `.bypass` grants are read as
  relaxed grants. A custom rule's `unattended: enforce` is read as "no category". Steps in
  [`docs/MIGRATE.md`](docs/MIGRATE.md#migrating-from-unattended-mode).

### Behaviour changes

- A tq mode applies in every Claude permission mode; the deprecated unattended mode applied
  only in bypass and auto sessions.
- Relaxed skips denies as well as confirms (the deprecated unattended mode skipped only
  confirms), except `git-history` and `system`, which ask.
- `privacy` is locked: the PII exit rules hold in every mode. A `guard.disable` or looser
  `guard.actions` entry for any locked rule is ignored, with a doctor warning.
- The `claude` launcher starts in bypass whatever `claude.permission_mode` says.
- `tq allow --mode`, and the deprecated `tq unattended on|off` and `tq allow --bypass`, are
  denied in a tool call by `tq/mode-change` (they used to ask), and they refuse agent sessions.
- `tq/outside-path-write`, `tq/elevation`, `tq/system-dir-write` and
  `tq/nested-agent-loosened` apply in every mode, default included.
- Every workspace with a Codex identity is rendered (`config.toml` block, `rules/tq.rules`,
  `hooks.json`), also with no mode set; the old `tq-unattended.config.toml` profile is
  removed when tq owns it. In Codex, confirms that cannot be prompted for become blocks.
- `tq/trust-store-write` also denies agent writes to the files tq renders into an
  identity: the codex identity's `rules/`, `hooks.json` and `config.toml`, and the Claude
  identity's `settings.json` and `settings.local.json` (also through `$CLAUDE_CONFIG_DIR`
  and `$CODEX_HOME`), and the Codex config layers (`.codex/` in a repo and `~/.codex`).
- Only `TQ_MODE=off` (exactly `off`) switches the hooks and plugin scripts off. `TQ_ENABLED=0`
  keeps its one meaning, the shell-activation kill switch: a `claude` started there is guarded.
- `tq run` reads its own flags only before the command word: after `<workspace>` only
  `--tq-mode MODE` and an optional `--` may come first, so the command's flags are its own
  (the PowerShell `tq` function puts back the `--` PowerShell removes).
- `tq activate bash|zsh` defines `function claude { ...; }`, so an earlier `alias claude=...`
  no longer aborts the block.
- `tq migrate` no longer carries the `claude` wrapper over, and removes one an older migrate
  carried when it is unchanged. `claude-alias-shadow` also reports functions that replace the
  launcher.
- `tq allow --mode` refuses a manifest that is not trusted yet (run `tq allow <ws>` first).
  Grants record the workspace root and do not apply to a copied manifest elsewhere.
- The global grant applies only outside every workspace; a folder under a base with a missing,
  invalid or untrusted manifest gets the default ceiling.
- In a `strict` session `TQ_MODE=default` before a nested agent is denied, and so is `env -i`
  before `claude` or `codex`. Settings that disable the plugin, set `TQ_MODE`/`TQ_ENABLED` in
  `env` or a looser `defaultMode` are denied in files and in inline `--settings`.
- An `apply_patch` with more than 256 files is denied ("split the patch"); patch paths are
  judged where they land, relative to the session folder.
- Writes to Claude's memory, plans and todos under `CLAUDE_CONFIG_DIR` (and Codex's sessions)
  no longer ask under `tq/outside-path-write`.
- After the upgrade `tq doctor` reports `settings-drift` until `tq settings render --all`
  runs once.
- The SessionStart guard line appears in every workspace session.
- The docs' built-in rule table now lists `tq/env-file-read` and `tq/env-file-shell` as
  deny, which is what tq does.
