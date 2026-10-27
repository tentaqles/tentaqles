# Claude identity settings, status line and worktrees

tq keeps every Claude identity (`~/.tentaqles/identities/<ws>/claude`) on one
baseline, so identities stop drifting apart. Three commands do the work, and
`tq doctor` reports when something has drifted.

## `tq settings render`

```sh
tq settings render --all --dry-run   # show what would change
tq settings render --all             # write it
tq settings render dirtybird         # one workspace
```

It writes the tq-owned part of each identity's `settings.json`:

| Key | Value |
|---|---|
| `permissions.defaultMode` | the manifest's `claude.permission_mode`. Empty means `auto`; `bypass` becomes `bypassPermissions` only after `tq allow --bypass <ws>`, otherwise `acceptEdits` (same rule as `tq run`) |
| `permissions.allow` | common read-only commands (git status/diff/log, gh pr view/checks, ls/cat/grep/rg, test runners, …) so auto mode does not stop on them |
| `permissions.deny` | credential stores (SSH keys, cloud/git credentials, the tq catalog), `printenv`, `gh repo delete` — a second layer behind the PreToolUse guard |
| `env` | `ENABLE_STOP_REVIEW=0`, `SECURITY_REVIEW_MODEL=claude-sonnet-5-5` and `SG_AGENTIC_MODEL=claude-sonnet-5-5`: the security-guidance plugin keeps its commit/push review (on Sonnet; its agentic path reads `SG_AGENTIC_MODEL`) but stops running a headless Opus review after every turn |
| `statusLine` | `tq statusline`, but only when there is none, it is an unpinned `npx …@latest` / ccstatusline, or tq set it before. A custom script is left alone. |

`skipDangerousModePermissionPrompt` is removed: tq manages the permission
posture.

**Only what tq wrote is ever taken back.** The entries tq added are recorded
in `<identity>/.tq-settings-state.json`; the next render replaces exactly
those, so rules you add by hand stay. An entry you already had is never
claimed by tq.

Per-workspace additions live in the manifest (and need `tq allow` after an
edit, like any manifest change):

```yaml
claude:
  permission_mode: auto        # default|acceptEdits|plan|auto|dontAsk|bypass
  permissions:
    allow: ["Bash(make:*)"]
    ask:   ["Bash(az webapp deploy:*)"]
    deny:  ["Bash(terraform apply:*)"]
  env:
    SOME_FLAG: "1"             # secret-shaped values make the manifest fail to load
```

A deny always wins over an allow for the same entry.

Settings changes apply to running sessions immediately, including the
permission mode.

## `tq statusline`

The status line command Claude Code runs. It prints:

```
tentaqles · reach@tentaqles.ai · Opus 5.5 · ctx 312k (31%) · $4.20
```

The context size turns yellow at 300k tokens and red at 400k ("compact or
wrap up"). It is taken from Claude Code's `context_window` input when present,
otherwise from the last assistant usage in the transcript. `NO_COLOR=1`
disables colour.

## `tq worktrees`

```sh
tq worktrees list                 # every linked worktree in every workspace repo
tq worktrees prune                # dry run
tq worktrees prune --yes          # remove
```

`prune` only removes a worktree that is clean, not locked, whose HEAD is
already in the default branch, and whose last commit is older than
`--older-than` days (default 3), or one whose directory is already gone. It
uses `git worktree remove` without `--force`, so git still refuses anything
with uncommitted work. Branches that were squash-merged show as unmerged and
are kept; delete those by hand once you have checked them.

## Codex

`tq unattended on` also prepares Codex CLI for the same scope, using
`approval_policy = "never"` and the manifest's `codex_sandbox` (default
`workspace-write`).

tq's guard does not run inside Codex: there are no tq deny rules or confirms
there, and Codex's own sandbox (`codex_sandbox`) is the only limit. So tq
writes the Codex config only while bypass trust holds (`tq allow --bypass
<ws>` for the current manifest), for both the workspace block and the
profile file. `tq deny <ws>`, or a plain `tq allow <ws>` after the manifest
changed, removes them again. Where it lands depends on the scope:

- **Workspace-wide**: a marked block at the head of `$CODEX_HOME/config.toml`
  (the workspace's Codex identity home):

  ```
  # >>> managed by tq (unattended): edit with tq unattended, not by hand
  approval_policy = "never"
  sandbox_mode = "workspace-write"
  # <<< managed by tq (unattended)
  ```

- **Projects only**: a tq-owned profile file `$CODEX_HOME/tq-unattended.config.toml`
  (current Codex layers `<name>.config.toml` files with `--profile <name>`).
  `tq run <ws> -- codex` adds `--profile tq-unattended` only inside a covered
  project, and only when the workspace has bypass trust, a Codex identity and
  the profile file is tq's. A plain `codex` started outside `tq run` does not
  pick the profile up in a projects-only setup.

**Conflicts.** If you already set `approval_policy` or `sandbox_mode` in
`config.toml`, or own a file called `tq-unattended.config.toml`, tq reports a
conflict and never touches your keys or file. `tq unattended off`,
`tq deny` and losing bypass trust remove only what tq wrote.

## `tq doctor` checks

| Code | Meaning |
|---|---|
| `settings-drift` | an identity's settings.json differs from what `tq settings render` would write |
| `unpinned-package` | settings.json or .claude.json runs `npx`/`uvx`/`bunx` with `@latest` or no version |
| `plugin-version-skew` | the same plugin is installed at different versions in different identities |
| `plugins-dir-linked` | an identity's `claude/plugins` is a symlink or junction to a shared directory (e.g. `~/.claude/plugins`, often shared with another identity): every plugin install from any identity using it rewrites the same `installed_plugins.json` and cache. Give the identity its own directory, then `tq bundle sync` |
| `plugin-foreign-path` | an identity's `installed_plugins.json` points at another identity's plugin cache or at `~/.claude` (a config dir copied from a sibling); hooks then run another identity's files. Reinstall the plugin inside the identity |
| `memory-stale` | a workspace's `.claude/memory.db` was last written more than 48 h before its newest Claude transcript: the plugin hooks are not capturing |
| `data-block-missing` | schema or fixture files name personal-data columns (email, cpf, phone, ...) and the manifest has no `data:` block; run /tentaqles:classify-data |
| `python-store-alias` | `python3` is the Microsoft Store alias (slower hook start); set `TENTAQLES_PY` |
| `unattended-not-effective` | `guard.unattended` is configured but `tq allow --bypass <ws>` was not run: confirms are still asked |
| `unattended-unknown-rule` | `guard.unattended.keep_asking` names a rule id tq does not know |
| `unattended-cloud` | unattended mode is effective in a workspace with a cloud identity: Claude can run cloud CLIs without asking |
