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
  exactly the one that render wrote: the value `settings.json` still holds
  when it has not changed since that render wrote it and its state file (an
  older render rewrote `defaultMode` on every render), else the one it wrote
  for the manifest's old `claude.permission_mode` (`auto` for none or `auto`,
  `bypassPermissions` for `bypass`, any other mode as it was); a different
  value is yours and stays. The value tq wrote is kept in the state file
  (`legacy_default_mode`) until tq has removed it, so removing
  `permission_mode` from the manifest first, as `tq doctor` advises, loses
  nothing;
- `skipDangerousModePermissionPrompt: true`, on the same evidence;
- the `permissions` object, when this leaves it empty.

With no state file, or one that does not parse, nothing of these is
touched: a corrupt state file claims nothing. `--dry-run` lists each
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

### MCP servers and the PATH mask under `guard: v2`

For a workspace whose manifest sets `guard: v2` (ADR-0007), the render also
brings the identity's harness config in line with its provider scope:

- **MCP servers.** Every server tq can tell is out of scope is removed from
  `<identity>/claude/.claude.json` (the top-level `mcpServers`, user scope,
  and every `projects[<path>].mcpServers`, local scope) and from
  `<identity>/codex/config.toml` (`[mcp_servers.<name>]` and its sub-tables,
  or a one-line `name = { ... }` under `[mcp_servers]`). Out of scope means a
  token of the name (split on `_ - .`) is an out-of-scope provider's `mcp`
  entry and no in-scope provider's, the package or image its `command`/`args`
  run is one of an out-of-scope provider's `mcp_packages` or `images`, one of
  its `env` keys is an out-of-scope provider's variable, its `url` is an
  out-of-scope API host, or its `command` is an out-of-scope CLI. Every other
  server is kept only when (the owner's hybrid list, ADR-0007) the catalog
  attributes it to an in-scope provider or `claude.bundle.mcp` declares it,
  it is a general-purpose server of `internal/providers/mcp_general.yaml`
  (playwright, context7, memory, filesystem, fetch, git, sequential-thinking,
  time, everything, postgres, sqlite, puppeteer, chrome-devtools,
  microsoft-learn; matched by what it runs, not its name), or its exact name
  is in the manifest's `mcp.allow`:

  ```yaml
  mcp:
    allow: [team-tool, plugin_tools_srv]   # exact server names; letters, digits, . _ -
  ```

  `mcp.allow` never keeps a server attributed to an out-of-scope provider.
  The hook judges `mcp__<server>__*` the same way for every same-named entry
  (a claude.ai connector by its name). `.claude.json` is edited in place: its indentation and
  key order stay. Each removed entry is appended to `mcp-out-of-scope.json` in
  the same folder (a file there tq cannot read is moved aside to
  `.corrupt-<time>`, never overwritten); to bring one back, add its provider to the manifest's
  `scope:`, `tq allow`, and copy it back. `--dry-run` lists what would go.
  Claude Code rewrites `.claude.json` while a session runs: render with the
  sessions of that identity closed (the render warns when one looks open).
- **The PATH mask.** `~/.tentaqles/identities/<ws>/path-mask/`, one stub per
  out-of-scope executable name in every spelling (extensionless, and on
  Windows `.exe`/`.com` hard links to `tq-stub.exe`, a link to the tq binary,
  plus `.bat`, `.cmd`, `.ps1`, `.vbs`, `.js`), which `tq env` puts first on the
  identity PATH when a folder shared with other tools holds such a CLI.
  Removed when the flag is off. The hook judges an `mcp__<server>__*` call by
  the same server entries.

Not rendered, and judged by the hook alone: a project `.mcp.json`, plugin MCP
servers, claude.ai connectors, a machine-wide `managed-mcp.json`,
`--mcp-config`, Codex's project `.codex/config.toml`, and the global
`~/.claude.json` an identity without a `claude` identity loads.

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

The launcher (`tq run . -- codex`, the `codex` function) starts Codex with
its full-bypass flag `--dangerously-bypass-approvals-and-sandbox` (PRD v2
§7.2): tq never sets how Codex asks, and its hook still denies the three
categories. A flag you type yourself about asking or the sandbox (`-a`,
`-s` and the glued `-anever`/`-sread-only`, `--yolo`, the bypass flag, a `-c`
override of `approval_policy` or `sandbox_mode`, glued ones included) goes
through as typed and the launcher adds nothing. The flag goes right after
`exec`, `e`, `resume` or `fork` (which take it themselves in codex 0.154),
first otherwise. The launch is refused when `tq` is not on the PATH Codex will
run with, since the Codex hook is a bare `tq claude-hook pre-tool-use
--agent codex`. `codex-off`
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
