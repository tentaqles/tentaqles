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
| `env` | `ENABLE_STOP_REVIEW=0`, `SECURITY_REVIEW_MODEL=claude-sonnet-5-5` and `SG_AGENTIC_MODEL=claude-sonnet-5-5`: the security-guidance plugin keeps its commit/push review (on Sonnet; its agentic path reads `SG_AGENTIC_MODEL`) but stops running a headless Opus review after every turn |
| `statusLine` | `tq statusline`, but only when there is none, it is an unpinned `npx …@latest` / ccstatusline, or tq set it before. A custom script is left alone. |

tq writes no permission setting: no `permissions` rules, no `defaultMode`.
The `claude` launcher starts Claude with `--dangerously-skip-permissions`,
and tq's PreToolUse hook denies what the identity and secret rules forbid
(see `CLAUDE-HOOK.md`).

After an upgrade that changes what tq renders, `tq doctor` reports
`settings-drift` for every identity until you run `tq settings render --all`
once.

**Only what tq wrote is ever taken back.** What tq wrote is recorded in
`<identity>/.tq-settings-state.json`; the next render replaces exactly that,
so keys you add by hand stay. A value you already had is never claimed by tq.

**Cleaning what an older tq wrote.** Older versions also wrote
`permissions.defaultMode`, `permissions.allow`/`deny`/`ask` and
`skipDangerousModePermissionPrompt`. The first render after the upgrade
removes them, and only them:

- the `allow`, `deny` and `ask` entries the state file records as tq's;
  entries you added by hand stay;
- `defaultMode`, when the state file shows an older render and the value is
  one that render wrote (`auto`, `bypassPermissions`, `default`,
  `acceptEdits`, `plan`, `dontAsk`);
- `skipDangerousModePermissionPrompt: true`, on the same evidence;
- the `permissions` object, when this leaves it empty.

With no state file nothing of these is touched. `--dry-run` lists each
removal (`permissions.allow: removed 2 tq entries (…)`,
`permissions.defaultMode: removed (was auto)`, …). The new state file
records no permission entry, so a `defaultMode` you set by hand afterwards
stays.

Per-workspace additions live in the manifest (and need `tq allow` after an
edit, like any manifest change):

```yaml
claude:
  env:
    SOME_FLAG: "1"             # secret-shaped values make the manifest fail to load
```

A manifest that still carries `claude.permission_mode` or
`claude.permissions` loads; tq does not read them and `tq doctor` warns
`manifest-dropped-block`. Remove them, then `tq allow`.

Settings changes apply to running sessions immediately.

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
tq writes nothing into Codex's `config.toml`.

**What tq writes**, into the workspace's Codex identity home (`CODEX_HOME`),
on `tq settings render`, `tq allow` and `tq deny`. A workspace without a
Codex identity gets nothing; when the identity is dropped, the next render
removes what tq wrote.

- tq's entries in `hooks.json`: PreToolUse runs
  `tq claude-hook pre-tool-use --agent codex` and SessionStart runs
  `tq claude-hook session-start --agent codex` (matcher `*`, timeout 30 s).
  Your entries and keys stay as they are. **Codex runs a hook only after you
  trust it once in `/hooks`**, and skips a changed hook until you trust it
  again. tq never passes `--dangerously-bypass-hook-trust`.

The launcher (`tq run . -- codex`, the `codex` function) passes
`-a on-request -s workspace-write`; a flag you type yourself (`-a`, `-s`,
a `-c` override of either, `--yolo`) wins. `codex-off`
(`tq run --off -- codex`) starts Codex with its bypass flag, the main
`~/.codex` and no tq.

A render removes only what an older tq wrote: its marked `config.toml`
block (by its markers, with the two keys inside it), and a `rules/tq.rules`
or profile file that tq wrote. Keys you set in `config.toml` outside that
block, your own `rules/*.rules` files, and a `tq.rules` tq did not write,
are never touched.

**Codex and the hook.** tq never sends an ask, to any harness (ADR-0006).
Every deny under `--agent codex`, whatever the category, answers Codex with
the PreToolUse deny JSON on stdout and exits 2, so either half blocks; a call
tq cannot judge is a deny of `tq/guard-error`.

**`apply_patch`.** Codex sends the patch text, not a file path. The hook
reads its file headers (`*** Add File:`, `*** Update File:`,
`*** Delete File:`, `*** Move to:`) and runs the secret file rules on every
file the patch writes. A patch whose file list tq cannot read is blocked
(`tq/guard-error`).

**Conflicts.** An old tq block in `config.toml` without its end marker, an
invalid `hooks.json`, or a hook group that mixes tq's handler with yours, is
a conflict, reported on each render ("not changed"), and tq never touches
your keys or files. `config.toml` and the hook entries are independent: a
conflict in one still writes the other, so the hook keeps running and the
identity guard keeps blocking.

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
| `claude-alias-shadow` | a shell profile defines something that stands in for tq's `claude`/`codex` launcher: a PowerShell alias, an alias that runs another command, or a function of that name after tq's activation (warn: delete it), or an alias that calls the launcher with extra flags (info) |
| `codex-hooks-missing` | tq's PreToolUse entry is not in the codex identity's `hooks.json`: Codex runs without tq's guard. Fix: `tq settings render <ws>` |
| `codex-hooks-disabled` | the codex identity's `config.toml` sets `[features] hooks = false` |
| `codex-hooks-untrusted` | no trust record for tq's hook was found: trust it once in Codex's `/hooks`. The check sees a missing record, not a stale one; `/hooks` shows both |
