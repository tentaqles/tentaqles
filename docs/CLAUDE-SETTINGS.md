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
| `permissions.defaultMode` | the manifest's `claude.permission_mode` as it is. Empty means `auto`; `bypass` becomes `bypassPermissions` (the manifest is hash-trusted by `tq allow`, so the value is the human's own choice). It applies to sessions started outside the launcher (an IDE): the `claude` launcher always starts in bypass (see `CLAUDE-HOOK.md`, "Turning rules on and off"). tq sets no permission mode of its own beyond that skip |
| `permissions.allow` | common read-only commands (git status/diff/log, gh pr view/checks, ls/cat/grep/rg, test runners, …) so auto mode does not stop on them |
| `permissions.deny` | credential stores (SSH keys, cloud/git credentials, the tq catalog), `printenv`, `gh repo delete` — a second layer behind the PreToolUse guard |
| `skipDangerousModePermissionPrompt` | `true`: the launcher always starts Claude in bypass, so Claude's bypass warning is skipped |
| `env` | `ENABLE_STOP_REVIEW=0`, `SECURITY_REVIEW_MODEL=claude-sonnet-5-5` and `SG_AGENTIC_MODEL=claude-sonnet-5-5`: the security-guidance plugin keeps its commit/push review (on Sonnet; its agentic path reads `SG_AGENTIC_MODEL`) but stops running a headless Opus review after every turn |
| `statusLine` | `tq statusline`, but only when there is none, it is an unpinned `npx …@latest` / ccstatusline, or tq set it before. A custom script is left alone. |

After an upgrade that changes what tq renders (0.9.0 did, for Codex),
`tq doctor` reports `settings-drift` for every identity until you run
`tq settings render --all` once.

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

The first segment is the workspace's client (`no workspace` in a neutral
folder), the second the git email in effect there.

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

tq gives Codex CLI the same two things it gives Claude: a private config
home per workspace and the PreToolUse hook (identity drift and secrets).
Codex's own approvals and sandbox stay Codex's: tq pins one pair and sets no
permission of its own beyond it.

**What tq writes**, into the workspace's Codex identity home (`CODEX_HOME`),
on `tq settings render`, `tq allow` and `tq deny`. A workspace without a
Codex identity gets nothing; when the identity is dropped, the next render
removes what tq wrote.

- A marked block at the head of `config.toml`, the same for every workspace:

  ```
  # >>> managed by tq: edit with tq settings render, not by hand
  approval_policy = "on-request"
  sandbox_mode = "workspace-write"
  # <<< managed by tq
  ```

- tq's entries in `hooks.json`: PreToolUse runs
  `tq claude-hook pre-tool-use --agent codex` and SessionStart runs
  `tq claude-hook session-start --agent codex` (matcher `*`, timeout 30 s).
  Your entries and keys stay as they are. **Codex runs a hook only after you
  trust it once in `/hooks`**, and skips a changed hook until you trust it
  again. tq never passes `--dangerously-bypass-hook-trust`.

The launcher (`tq run . -- codex`, the `codex` function) passes the same
pair as flags, `-a on-request -s workspace-write`; a flag you type yourself
(`-a`, `-s`, `-c approval_policy=…`, `--yolo`) wins. `codex-off`
(`tq run --off -- codex`) starts Codex with its bypass flag, the main
`~/.codex` and no tq.

What older tq versions wrote is removed on the next render when tq owns it:
the old config block (its markers are replaced by the ones above), the
`rules/tq.rules` exec-policy file and the old profile file. Your own
`rules/*.rules` files, and a `tq.rules` tq did not write, are never touched.

**Codex and the hook.** Codex parses a hook `ask` but does not support it:
it reports an error and continues the call. So tq never sends one: the one
confirm the hook can raise (the commit secret scan that could not finish, or
the `tq/guard-error` fail-safe) is a block in Codex, with the reason
`tq: this needs your confirm; Codex hooks cannot ask. Run it yourself, or use claude`.
An identity-guard block (wrong git email, blocked command, untrusted
workspace) answers Codex with a deny as well as exit 2.

**`apply_patch`.** Codex sends the patch text, not a file path. The hook
reads its file headers (`*** Add File:`, `*** Update File:`,
`*** Delete File:`, `*** Move to:`) and runs the secret file rules on every
file the patch writes. A patch whose file list tq cannot read is blocked
(`tq/guard-error`).

**Conflicts.** A user-owned `approval_policy` or `sandbox_mode` in
`config.toml` is left alone and reported on each render ("not changed"); an
invalid `hooks.json`, or a hook group that mixes tq's handler with yours, is
a conflict, and tq never touches your keys or files. The config block and
the hook entries are independent: a conflict in one still writes the other,
so the hook keeps running and the identity guard keeps blocking.

## `tq doctor` checks

| Code | Meaning |
|---|---|
| `settings-drift` | an identity's settings.json differs from what `tq settings render` would write |
| `unpinned-package` | settings.json or .claude.json runs `npx`/`uvx`/`bunx` with `@latest` or no version |
| `plugin-version-skew` | the same plugin is installed at different versions in different identities |
| `plugins-dir-linked` | an identity's `claude/plugins` is a symlink or junction to a shared directory (e.g. `~/.claude/plugins`, often shared with another identity): every plugin install from any identity using it rewrites the same `installed_plugins.json` and cache. Give the identity its own directory, then `tq bundle sync` |
| `plugin-foreign-path` | an identity's `installed_plugins.json` points at another identity's plugin cache or at `~/.claude` (a config dir copied from a sibling); hooks then run another identity's files. Reinstall the plugin inside the identity |
| `memory-stale` | a workspace's `.claude/memory.db` was last written more than 48 h before its newest Claude transcript: the plugin hooks are not capturing |
| `python-store-alias` | `python3` is the Microsoft Store alias (slower hook start); set `TENTAQLES_PY` |
| `permission-mode-overridden` | (info) `claude.permission_mode` is not bypass, and the `claude` launcher starts in bypass anyway; the value still drives `defaultMode` |
| `claude-alias-shadow` | a shell profile defines something that stands in for tq's `claude`/`codex` launcher: a PowerShell alias, an alias that runs another command, or a function of that name after tq's activation (warn: delete it), or an alias that calls the launcher with extra flags (info) |
| `codex-hooks-missing` | tq's PreToolUse entry is not in the codex identity's `hooks.json`: Codex runs without tq's guard. Fix: `tq settings render <ws>` |
| `codex-hooks-disabled` | the codex identity's `config.toml` sets `[features] hooks = false` |
| `codex-hooks-untrusted` | no trust record for tq's hook was found: trust it once in Codex's `/hooks`. The check sees a missing record, not a stale one; `/hooks` shows both |
