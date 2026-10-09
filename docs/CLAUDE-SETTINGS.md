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
| `env` | `ENABLE_STOP_REVIEW=0` and `SECURITY_REVIEW_MODEL=claude-sonnet-5-5`: the security-guidance plugin keeps its commit/push review but stops running a headless Opus review after every turn |
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

## `tq doctor` checks

| Code | Meaning |
|---|---|
| `settings-drift` | an identity's settings.json differs from what `tq settings render` would write |
| `unpinned-package` | settings.json or .claude.json runs `npx`/`uvx`/`bunx` with `@latest` or no version |
| `plugin-version-skew` | the same plugin is installed at different versions in different identities |
| `python-store-alias` | `python3` is the Microsoft Store alias (slower hook start); set `TENTAQLES_PY` |
