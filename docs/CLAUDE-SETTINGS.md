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
| `permissions.defaultMode` | the manifest's `claude.permission_mode`. Empty means `auto`; `bypass` becomes `bypassPermissions` only when the workspace has a mode grant (`tq allow --mode <ws> relaxed\|custom`; a legacy bypass grant counts), otherwise `acceptEdits`. It applies to sessions started outside the launcher (IDE, desktop, the hub runner): the `claude` launcher always starts in bypass (see `CLAUDE-HOOK.md`, "Launchers") |
| `permissions.allow` | common read-only commands (git status/diff/log, gh pr view/checks, ls/cat/grep/rg, test runners, …) so auto mode does not stop on them |
| `permissions.deny` | credential stores (SSH keys, cloud/git credentials, the tq catalog), `printenv`, `gh repo delete` — a second layer behind the PreToolUse guard |
| `permissions.ask` | `Bash(gh repo delete:*)` instead of the deny above when any layer puts `git-history` (or `tq/gh-repo-delete`) below deny: a relaxed workspace, project or global `guard.yaml`, a custom table with git-history `ask` or `skip`, or a `rules_override`. A settings deny blocks in every Claude mode and would override relaxed's confirm. The per-launch `--tq-mode` cannot be rendered into settings, so a relaxed launch in a workspace that is not relaxed keeps the deny |
| `skipDangerousModePermissionPrompt` | `true`: the launcher always starts Claude in bypass, so Claude's bypass warning is skipped |
| `env` | `ENABLE_STOP_REVIEW=0`, `SECURITY_REVIEW_MODEL=claude-sonnet-5-5` and `SG_AGENTIC_MODEL=claude-sonnet-5-5`: the security-guidance plugin keeps its commit/push review (on Sonnet; its agentic path reads `SG_AGENTIC_MODEL`) but stops running a headless Opus review after every turn |
| `statusLine` | `tq statusline`, but only when there is none, it is an unpinned `npx …@latest` / ccstatusline, or tq set it before. A custom script is left alone. |

After an upgrade to guard modes, `tq doctor` reports `settings-drift` for
every identity until you run `tq settings render --all` once, because of the
new `skipDangerousModePermissionPrompt` key.

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
tentaqles · tq: relaxed · 4 skipped · reach@tentaqles.ai · Opus 5.5 · ctx 312k (31%) · $4.20
```

The `tq:` segment is the guard mode (see `CLAUDE-HOOK.md`, "Guard modes"):
dim for `default`, yellow for `relaxed` or a `custom` table looser than
default, cyan for `strict`. "4 skipped" counts the confirms and blocks the
mode skipped or lowered in this workspace in the last 24 h, read from the end
of the audit log only. Under `TQ_MODE=off` the line is `tq: off` in red,
without the client and e-mail.

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

tq compiles the guard mode into Codex CLI as far as Codex allows: launch
flags, a `config.toml` block, an exec-policy rules file and a PreToolUse hook.
Coverage is best effort. Claude Code has full coverage.

**What tq writes**, into the workspace's Codex identity home (`CODEX_HOME`),
on `tq settings render`, `tq allow`, `tq deny` and every `tq mode` write. A
workspace without a Codex identity gets nothing; when the identity is dropped,
the next render removes what tq wrote.

- A marked block at the head of `config.toml`:

  ```
  # >>> managed by tq (guard mode): edit with tq mode, not by hand
  approval_policy = "on-request"
  sandbox_mode = "workspace-write"
  # <<< managed by tq (guard mode)
  ```

- `rules/tq.rules`: one `prefix_rule(...)` per line between the same markers.
  A rule tq denies becomes `forbidden`; a confirm with a prefix form becomes
  `prompt`. Codex applies the most restrictive match, so a rule of yours
  cannot weaken tq's `forbidden`. Your own `rules/*.rules` files are never
  touched, and a `tq.rules` that tq did not write is a conflict.
- tq's entries in `hooks.json`: PreToolUse runs
  `tq claude-hook pre-tool-use --agent codex` and SessionStart runs
  `tq claude-hook session-start --agent codex` (matcher `*`, timeout 30 s).
  Your entries and keys stay as they are. **Codex runs a hook only after you
  trust it once in `/hooks`**, and skips a changed hook until you trust it
  again. tq never passes `--dangerously-bypass-hook-trust`.

**Per mode:**

| tq mode | Launch flags (`tq run . -- codex`) | `config.toml` | `rules/tq.rules` |
|---|---|---|---|
| off | `--dangerously-bypass-approvals-and-sandbox`, main `~/.codex` | not touched | not used |
| default | `-a on-request -s workspace-write` | `approval_policy = "on-request"`, `sandbox_mode = "workspace-write"` | confirms with a prefix form → `prompt`, denies → `forbidden` |
| relaxed | `-a never -s <guard.codex.sandbox>` (default `workspace-write`); the bypass flag only for `danger-full-access` | `approval_policy = "never"`, the sandbox | git-history and system confirms → `prompt`; locked denies → `forbidden` |
| strict | `-a on-request -s workspace-write` | as default | the deny categories → `forbidden` |
| custom | `-a on-request` when a toggleable rule outside git-history and system asks, else as relaxed | the same | from the table |

The launch flags follow the cwd, so a relaxed project gets its flags from
`tq run . -- codex` while the `config.toml` block carries the workspace-wide
mode (a bare `codex` outside the function gets that). A flag you type
yourself (`-a`, `-s`, `-c approval_policy=…`, `--yolo`) wins. Rendering never
uses a `TQ_MODE` from the shell it runs in. The old
`tq-unattended.config.toml` profile (from the deprecated mode) is removed when
tq owns it, and the old block is replaced.

**Codex limits: asks become blocks.** Codex parses a hook `ask` but does not
support it: it reports an error and continues the call. So tq never sends
one. A tq confirm becomes:

- a **block** with the reason
  `tq: this needs your confirm; Codex hooks cannot ask. Run it yourself, or use claude`
  for MCP tools, `apply_patch` and every shell command that no `prompt` rule
  covers;
- **Codex's own prompt** only when all of these hold: the call is a single
  shell command (no chain), not on Windows, Claude-style permission mode
  `default`, `acceptEdits` or `plan`, the workspace isolates Codex and
  `CODEX_HOME` is its identity dir, the sandbox is not `danger-full-access`,
  `rules/tq.rules` is byte for byte what tq would render, and the command
  matches one of tq's curated prefixes as `prompt`. Then the hook lets the
  call through and Codex's rule prompts.

So every mode is stricter in Codex than in Claude for confirms that cannot be
expressed as a prefix: they block. This is accepted. `tq mode --agent codex`
lists the affected rule ids for the cwd, and the SessionStart line in Codex
ends with `asks become blocks in Codex: N rules`.

Rules with a prefix form today: `tq/gh-repo-delete`, `tq/force-push`,
`tq/iac-destroy`, `tq/iac-apply`, `tq/remote-migration` and
`tq/registry-write`. Hook only, so a block in Codex whenever they ask:
`tq/force-push-main`, `tq/gh-admin-merge`, `tq/cloud-delete`, the SQL rules,
the file and path rules, and every locked rule.

What else Codex cannot express:

- rules that match a flag anywhere in the command, a regex, file content, a
  path, heredoc data or a cwd, and the commit and push scans (secret, PII)
  beyond what the hook does on the shell call;
- the file and path rules on `apply_patch` edits are not reliable yet, since
  Codex sends the patch text, not a file path;
- whether a `prompt` rule still prompts under `approval_policy = "never"`,
  and whether `forbidden` applies under
  `--dangerously-bypass-approvals-and-sandbox`, are not documented. Check
  both by hand with a throwaway `CODEX_HOME` before you rely on relaxed in
  Codex. The hook still blocks either way.

**Conflicts.** A user-owned `approval_policy` or `sandbox_mode` in
`config.toml` is left alone and reported on each render ("not changed"), a
foreign `rules/tq.rules`, invalid `hooks.json`, or a hook group that mixes
tq's handler with yours is a conflict, and tq never touches your keys or
files. `tq deny` and an untrusted manifest render the default plan, so the
hook keeps running and the identity guard keeps blocking.

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
| `mode-not-granted` | a layer asks for a mode looser than its ceiling (no grant, or a table looser than the grant): those categories run at the ceiling. Fix: `tq allow --mode <ws> <mode>` (or `--global`) |
| `mode-unknown-rule` | `rules_override` (or a legacy `keep_asking`) names a rule id tq does not know |
| `mode-cloud` | the mode skips `prod-changes` or `cloud-destructive` in a workspace with a cloud identity: Claude can change or delete cloud resources without asking (the identity guard, wrong-cloud included, still applies) |
| `guard-locked-override` | `guard.disable` or a looser `guard.actions` entry names a locked rule (identity, secrets, self-protection, privacy); it is ignored |
| `permission-mode-overridden` | (info) `claude.permission_mode` is not bypass, and the `claude` launcher starts in bypass anyway; the value still drives `defaultMode` |
| `claude-alias-shadow` | a shell profile defines an alias named `claude` or `codex`, which skips tq's launcher function; remove it |
| `codex-hooks-missing` | tq's PreToolUse entry is not in the codex identity's `hooks.json`: Codex runs without tq's guard. Fix: `tq settings render <ws>` |
| `codex-hooks-disabled` | the codex identity's `config.toml` sets `[features] hooks = false` |
| `codex-hooks-untrusted` | no trust record for tq's hook was found: trust it once in Codex's `/hooks`. The check sees a missing record, not a stale one; `/hooks` shows both |
