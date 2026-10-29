# Changelog

Notable changes to the `tq` CLI, the desktop app and the docs of this repository.
The Claude Code plugin keeps its own log in [`plugin/CHANGELOG.md`](plugin/CHANGELOG.md).
Releases before this file are described in the GitHub releases (up to `v0.7.0`).

## [Unreleased]

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
  identity's `settings.json` and `settings.local.json`.
- After the upgrade `tq doctor` reports `settings-drift` until `tq settings render --all`
  runs once.
- The SessionStart guard line appears in every workspace session.
- The docs' built-in rule table now lists `tq/env-file-read` and `tq/env-file-shell` as
  deny, which is what tq does.
