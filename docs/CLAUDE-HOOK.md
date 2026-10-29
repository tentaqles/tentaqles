# `tq claude-hook` — Claude Code hook adapter

`tq claude-hook` is what the Claude Code plugin's `SessionStart` and
`PreToolUse` hooks actually call (via `plugin/scripts/tq_hook.sh`). It is
the source of truth for identity reporting and enforcement in 0.4.0+; the
plugin's Python code no longer switches gh/az/git identity itself.

## Subcommands

- `tq claude-hook session-start` — prints a short identity/status preamble. Never blocks (always exits 0).
- `tq claude-hook pre-tool-use` — decides whether to allow, ask about, or block a tool call: shell commands (Bash, PowerShell), file reads and edits, and MCP tool calls. Exits 0 (allow, or ask via JSON on stdout) or 2 (block). The [guard mode](#guard-modes) decides what each matching rule does. `--agent codex` answers in Codex's format, where a confirm becomes a block (see `CLAUDE-SETTINGS.md`, "Codex").
- `tq claude-hook stop` — Stop hook: the Jev completion-evidence check. Always exits 0; in enforce mode it may print `{"decision":"block","reason":...}` once per session. See `docs/DECIDE.md`.
- `tq claude-hook prompt-submit` — UserPromptSubmit hook: the Jev skill picker. Always exits 0 and never blocks; in enforce mode it may add a one-line `additionalContext` hint. See `docs/DECIDE.md`.

Both subcommands read a single JSON payload from stdin — the same shape
Claude Code sends its own hooks. Under `TQ_MODE=off` or `TQ_ENABLED=0` every
subcommand exits 0 at once with no output (see [Guard modes](#guard-modes)).

## Protocol

### Input (stdin)

Both subcommands accept the hook JSON Claude Code provides. The fields
`tq` reads:

```json
{
  "tool_name": "Bash",
  "cwd": "C:\\repos\\acme\\billing-api",
  "tool_input": {
    "command": "git push origin main"
  }
}
```

- `tool_name` — `Bash` and `PowerShell` go through the identity guard *and*
  the policy rules; every other tool (Read, Edit, Write, MultiEdit,
  NotebookEdit, Grep, `mcp__*`) goes through the policy rules only. A
  payload without `tool_name` is allowed.
- `cwd` — the directory to resolve a workspace from. Falls back to the
  process's actual working directory if omitted or empty.
- `tool_input` — `command` for shell tools; `file_path` / `notebook_path` /
  `path` plus `content` / `new_string` / `edits[].new_string` /
  `new_source` for file tools; the whole object (as JSON text) for MCP
  tools. Ignored by `session-start`.

Any other fields in the payload (session id, etc.) are ignored.

The payload is capped at 1 MiB. A larger payload is a protocol violation,
not a parse error: `pre-tool-use` prints
`BLOCKED: hook payload exceeds 1 MiB` to stderr and exits 2 (fail closed),
while `session-start` prints its "could not resolve" line and exits 0.

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Allow — or **ask**, when stdout carries the PreToolUse decision JSON below. `session-start` always exits 0 (it never blocks a session). |
| `2` | Block (`pre-tool-use` only). A `BLOCKED: ...` message is written to **stderr**; stdout is not used for the block message. |

An **ask** is printed to stdout so Claude Code shows the user a
confirmation prompt with the reason:

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse",
  "permissionDecision": "ask",
  "permissionDecisionReason": "CONFIRM [tq/gh-admin-merge]: --admin bypasses branch protection (source: builtin)"}}
```

If the guard itself fails while it analyses a call (a panic in tq's own
code), `pre-tool-use` fails safe: it asks, with the reason
`CONFIRM [tq/guard-error]: tq guard error; please review the command yourself (…)`,
and writes `tq: guard panic: …` to stderr. It never allows the call and
never crashes (a Go crash exits 2 with a stack trace, which Claude Code
reads as a block). No guard mode skips this ask: `tq/guard-error` is in the
locked self-protection group.

`session-start` writes its preamble to **stdout** and always returns 0,
even when it can't resolve a workspace — a hook that fails a session start
would be worse than a hook that reports "unknown".

### `--json`

`tq doctor --json`, `tq list --json`, and `tq bundle diff --json` all
support machine-readable output for skills/scripts that want to parse
results instead of scraping text.

`tq claude-hook pre-tool-use --json` also supports it: with the flag, the
decision is written to **stdout** as one JSON object, in addition to the
usual stderr message and exit code.

```json
{"block": true, "rule": "git-email-drift", "reason": "BLOCKED: ..."}
```

- `block` — `true` when the command is refused (the process also exits 2).
- `rule` — the rule that matched: an identity rule (`git-email-drift`, …)
  or a policy rule id (`tq/env-dump`, …). For an ask, `block` is `false`
  and `rule`/`reason` name the rule; `""` when the call is simply allowed.
- `reason` — the full multi-line message (`""` when allowed).

With `--json`, an ask is reported in this object instead of the
`hookSpecificOutput` JSON, so the flag is for scripts, not for the hook.

`session-start` has no `--json` flag: its output is the fixed preamble
format described below.

## Rule precedence (`pre-tool-use`)

`internal/guard/guard.go`'s `Decide` function applies rules in this exact
order — the **first** matching rule wins and produces the block:

1. **`blocked-command`** — the command starts with (as a whole word, after
   any of `&& || ; | & \n \r $( \` ( { }`) an entry in the manifest's
   effective blocked-command list. Checked before anything else, even
   before cloud/identity checks.
2. **`wrong-cloud`** — the command invokes a cloud CLI (`az`, `aws`,
   `gcloud`, `gsutil`, `bq`, `doctl`) whose provider doesn't match the
   workspace's configured cloud provider. E.g. running `az ...` in an
   AWS-configured workspace. Providers listed in the trusted manifest's
   `cloud.also` (e.g. `also: [aws]`; names `azure`, `aws`, `gcp`, `digitalocean` (aliases: `az`, `gcloud`, `do`, `doctl`)) are allowed too; the block message
   then names the set ("azure + aws"). A repo manifest cannot add clouds.
3. **`neutral-remote`** — cwd resolves to **no** trusted tq workspace
   (`Neutral`), and the command is a remote mutation (`gh` anything, any
   cloud CLI, or `git push|fetch|pull|clone`). Everything else in a
   neutral cwd is allowed (there's no identity to protect for local-only
   commands).
4. **`untrusted`** — non-read-only git only. The workspace resolved but isn't trusted
   (`tq allow <name>` hasn't been run, or the manifest changed since).
5. **`env-drift`** — non-read-only git only. The shell's exported `TQ_WS` doesn't match
   cwd (stale shell — the `tq` activation hook hasn't re-run since the
   last `cd`).
6. **`claude-config-drift`** — remote mutation (`git push`/`gh`) only. The
   running Claude session isn't under the workspace's expected
   `CLAUDE_CONFIG_DIR`.
7. **`git-email-drift`** — two ways to match. (a) Any git invocation
   carrying an inline identity override (`-c user.email=...`,
   `-c user.name=...`, `--config-env=user.email=...`,
   `--config-env=user.name=...`) in a workspace that pins an email is
   blocked outright, whatever the configured email is: such a command
   supplies its own identity, so comparing the configured one proves
   nothing. (b) Otherwise: non-read-only git only, and only when both the
   expected and actual emails are known. `git status`, `log`, `diff`,
   `show`, `rev-parse`, `ls-files`, read-only `branch`/`remote` invocations
   are exempt (see **Read-only git exemption** below).
8. **`gh-user`** — `gh` only, and only when both the expected and actual
   GitHub usernames are known.

If no rule matches, the command is allowed (`Decision{}`, zero value,
`Block: false`).

### Read-only git exemption

`git-email-drift` (rule 7) does not fire for git invocations classified as
read-only by `IsReadOnlyGit`: `status`, `log`, `diff`, `show`, `rev-parse`,
`ls-files`, and read-only forms of `branch` (no `-d/-D/-m/-M/-c/-C/-f`,
`--delete/--move/--copy/--force/--set-upstream-to`, `-u`,
`--unset-upstream`, and at most one positional arg) and `remote` (no args,
or `-v`/`show`/`get-url`). A command with **multiple** chained git
invocations (`git status && git push`) is read-only only if **every**
segment is read-only — one mutating segment makes the whole command
subject to the email check. The exemption covers rules 4 (`untrusted`),
5 (`env-drift`) and 7 (`git-email-drift`): a read-only git command neither
writes history nor touches a remote, so a stale shell or an untrusted
manifest is no reason to refuse it. Rules 1-3, 6 and 8 are unaffected by
whether the git command is read-only, and so is the inline-identity-override
case of rule 7 (`git -c user.email=... status` is still blocked).

### What this guard does not catch

Segment splitting and prefix matching are string heuristics, not a shell
parser. They are trivially bypassable by anyone who wants to: `env git push`,
`command git push`, `sh -c "git push"`, `... | xargs git push`, `\git push`,
`"git" push`, a shell alias or function named `git`, a `$GIT push` variable
expansion, or homoglyph/encoding tricks all slip past. Quoting, escaping and
nesting are not modelled either.

This is deliberate. The guard is **defense-in-depth on top of tq's env
isolation**, not a sandbox: the real protection is that each workspace runs
with its own private `GH_CONFIG_DIR` / `AZURE_CONFIG_DIR` / git config, so a
command that dodges the guard still does not get another client's
credentials. The guard's job is to catch the honest mistake - the wrong cwd,
the stale shell, the forgotten `tq allow` - not to contain an adversary.

## Guard modes

A guard mode decides what tq does with each rule that matches a tool call.
Every rule sits in one **category**, and a mode gives each category one of
four **values**: `skip` (allow, no prompt), `ask` (confirm), `deny` (block)
or `rule` (the rule's own action, after `guard.actions`). There are five
modes: `off`, `default`, `relaxed`, `strict` and `custom`. With nothing
configured, the mode is `default`: every rule at its own action.

Four categories are **locked**: `identity`, `secrets`, `self-protection` and
`privacy`. They are enforced in every mode tq is on in, no setting can toggle
them, and a `guard.disable` or a looser `guard.actions` entry naming one of
their rules is ignored (with a `guard-locked-override` doctor warning).

### Two layers

Two independent layers decide what happens to a tool call:

| | Claude Code permission mode | tq guard mode |
|---|---|---|
| Who owns it | Claude Code (`--permission-mode`, `--dangerously-skip-permissions`, `permissions.defaultMode`) | tq (`guard.mode`, `TQ_MODE`, `tq mode`) |
| Values | `default`, `acceptEdits`, `plan`, `auto`, `dontAsk`, `bypassPermissions` | `off`, `default`, `relaxed`, `strict`, `custom` |
| What it decides | when Claude asks you about calls no rule covers | allow, ask or deny for each tq rule that matches |
| Per rule | settings allow, ask and deny lists; auto mode adds a classifier you can only partly tune | yes: categories and `rules_override` |
| In bypass | settings deny and ask rules still apply; allow rules have no effect | yes: the PreToolUse hook runs in every Claude mode |

The recommended setup is **Claude in `bypassPermissions`, tq in the mode
you want**. tq is then the single policy layer: a tq confirm is the only
prompt you see, apart from the few calls Claude Code never auto-approves
(critical-path `rm`, `AskUserQuestion`, MCP tools that require user
interaction). The `claude` launcher (see [Launchers](#launchers-tq-off-and-tq-on))
always starts Claude with `--dangerously-skip-permissions` for this reason.
Auto mode only adds classifier prompts that tq cannot configure.

The tq mode applies whatever Claude's permission mode is. A `relaxed`
workspace run in Claude's `default` mode skips tq's confirms, and Claude
itself still prompts for calls it does not allow.

Two things still need a manual check in a real bypass session before you
rely on them: that a hook `ask` still prompts in `bypassPermissions`, and
that a hook deny blocks there. The Claude Code docs say a blocking hook
stops the call before permission rules run, but do not say it for a hook ask
in bypass.

### The modes

#### `off`

No tq at all for one launch or one terminal window: no identity env
(`CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`, `CODEX_HOME`, …),
the main user install (`~/.claude`, `~/.codex`), no guard, no SessionStart
preamble, no memory hooks, the tentaqles plugin switched off and the session
in bypass. You get it from `claude-off`, `codex-off`, `tq run --off` or
`tq off` (see [Launchers](#launchers-tq-off-and-tq-on)).

`off` is never stored. It is not a valid `guard.mode` in any file (a
manifest or `guard.yaml` with `mode: off` fails to load), so no edit or sync
can make a workspace off.

**What off gives up.** git, gh, cloud CLIs and MCP servers use your main
logins and the main install's credentials. A command in a client folder can
push with the wrong email, deploy to the wrong subscription or call another
client's MCP server, and nothing blocks a secret read, a force push to main
or a repo delete. Use off for tq-free work and debugging, not for client
work. Every off launch prints
`tq: off — main logins, no guard (git, cloud and MCP use your default accounts)`.

#### `default`

Every rule at its own action: confirms for prod changes, cloud deletes, IaC
apply and destroy, admin merges, force pushes and destructive SQL; blocks for
a force push to main, a repo delete and the rest of the deny rules below.
It applies when no layer sets a mode, in untrusted and unmanaged folders, and
for headless hub runs unless the workspace opts in.

#### `relaxed`

Every toggleable category is `skip`, except `git-history` and `system`,
which `ask`:

- **git-history**: the blocks `tq/force-push-main` and `tq/gh-repo-delete`
  become confirms, and `tq/force-push` and `tq/gh-admin-merge` stay confirms.
- **system**: `tq/system-dir-write` (a block by default) becomes a confirm,
  and `tq/outside-path-write`, `tq/elevation` and `tq/registry-write` stay
  confirms.

Everything else that is not locked is skipped, blocks included: destructive
SQL, cloud deletes, IaC apply and destroy, migrations, MCP and tracker
writes, GitHub Actions pins and the commit-message check. The locked groups
stay, so the PII denies, the `.env` and secret-store denies and tq's own
protections still hold. When a skipped rule hid a weaker rule that still
applies, that rule decides: `git push --force origin main; tq guard off`
still asks through `tq/guard-change`.

Relaxed is looser than default, so it needs a grant (`tq allow --mode <ws>
relaxed`). Without one the session runs at `default` and SessionStart says
so.

#### `strict`

Default plus: `prod-changes`, `cloud-destructive` and `git-history` deny, and
`external-writes` asks. These four are pinned: a looser `rules_override`
entry for one of their rules does not loosen it under strict, while a
stricter one still applies. Strict needs no grant.

#### `custom`

A base preset plus per-category values:

```yaml
guard:
  mode: custom
  base: relaxed                # default | relaxed | strict (default: default)
  categories:
    db-destructive: ask
    external-writes: rule
```

`tq mode toggle` on a preset turns it into `custom` with that preset as the
base. `base` and `categories` are only valid with `mode: custom`, and a
locked category in `categories` is a load error. A custom table that is
looser than default anywhere needs a grant for that exact table.

### Presets

`rule` means the rule's own action.

| Category | default | relaxed | strict |
|---|---|---|---|
| `identity` | rule | rule | rule |
| `secrets` | rule | rule | rule |
| `self-protection` | rule | rule | rule |
| `privacy` | rule | rule | rule |
| `git-history` | rule | ask | deny |
| `prod-changes` | rule | skip | deny |
| `cloud-destructive` | rule | skip | deny |
| `db-destructive` | rule | skip | rule |
| `system` | rule | ask | rule |
| `external-writes` | rule | skip | ask |
| `ci-supply-chain` | rule | skip | rule |
| `hygiene` | rule | skip | rule |
| `packs` | rule | skip | rule |

### Categories

Every rule has exactly one category (`internal/guardcfg/categories.go`; a
test fails on a built-in rule without one). The own action of each rule is
in brackets.

| Category | Locked | Rules |
|---|---|---|
| `identity` | locked | the identity guard: `guard/blocked-command`, `guard/wrong-cloud`, `guard/neutral-remote`, `guard/untrusted`, `guard/env-drift`, `guard/claude-config-drift`, `guard/git-email-drift`, `guard/gh-user` (deny; it runs before any mode logic); `plugin/identity-guard` (the fallback with no tq binary, deny); `tq/cross-client-mcp` (deny); `tq/iac-identity-drift`¹ |
| `secrets` | locked | `tq/secret-store-read`, `tq/secret-store-shell`, `tq/env-file-read`, `tq/env-file-shell`, `tq/env-dump`, `tq/commit-secret` (deny); `jev/service-role-client`, `jev/n8n-inline-credentials` (ask) |
| `self-protection` | locked | `tq/guard-change` (ask), `tq/guard-file-write`, `tq/settings-widen`, `tq/settings-shell-write`, `tq/trust-store-write` (deny), `tq/guard-error` (the fail-safe ask), `tq/nested-agent-loosened`, `tq/mode-change` (deny), `plugin/headless-manifest-guard` (deny); `tq/hub-store-write`, `tq/pack-config-change`, `tq/human-answer`¹ |
| `privacy` | locked | `tq/commit-pii`, `tq/push-pii`, `tq/text-pii`, `tq/pii-fixture-dump` (deny) |
| `git-history` | | `tq/force-push-main` (deny), `tq/force-push` (ask), `tq/gh-admin-merge` (ask), `tq/gh-repo-delete` (deny) |
| `prod-changes` | | `tq/iac-apply`, `tq/remote-migration` (ask); `tq/dbt-prod-target`, `tq/model-promote`, `tq/pipeline-run`, `tq/iac-apply-env`, `tq/db-migrate-env`, `tq/restore-target`¹ |
| `cloud-destructive` | | `tq/cloud-delete`, `tq/iac-destroy` (ask) |
| `db-destructive` | | `tq/sql-destructive-shell`, `tq/migration-edit`, `tq/mcp-sql-write`, `tq/mcp-apply-migration` (ask); `jev/destructive-sql`, `jev/rls-weakened` (Jev ask); `tq/dbt-full-refresh`¹ |
| `system` | | `tq/registry-write`, `tq/outside-path-write`, `tq/elevation` (ask); `tq/system-dir-write` (deny) |
| `external-writes` | | `tq/mcp-github-write`, `tq/mcp-n8n-write`, `tq/mcp-hosting-exec`, `tq/tracker-cli-write` (ask) |
| `ci-supply-chain` | | `tq/gha-unpinned`, `tq/gha-pr-target` (ask); `tq/gha-branch-ref` (deny) |
| `hygiene` | | `tq/commit-message` (ask) |
| `packs` | | the fallback for a pack guard that fits no other category: `tq/warehouse-resize`, `tq/bq-unbounded-query`, `tq/warehouse-query-unbounded`¹ |

¹ Rules of the packs work, classified ahead of its merge.

**Custom rules** (`guard.rules` in the manifest or `guard.yaml`, and
`.claude/tq-rules.yaml`) may declare `category:` with one of the thirteen
names. A rule without one is applied at its own action in every mode (fail
closed). In the manifest and `guard.yaml`, a locked category is allowed and
means "always applied".

```yaml
guard:
  rules:
    - id: acme/no-prod-db
      action: deny
      command: '\bprod-db\b'
      reason: the production database is off limits
      category: prod-changes      # strict denies it, relaxed skips it
```

### Precedence and grants

**Layers**, from least to most specific. A later layer replaces the mode of
an earlier one:

1. built-in: `default`;
2. global `~/.tentaqles/guard.yaml`: `mode`, `base`, `categories`,
   `rules_override` (every workspace and neutral folders);
3. the workspace manifest's `guard:` block, the same keys;
4. a project: `guard.projects.<dir>` in the manifest (`<dir>` relative to the
   root; the deepest match wins). For a file tool the target's folder must be
   inside the project too, otherwise the workspace layer applies;
5. the per-launch override, `TQ_MODE` in the agent process's env, set by
   `claude --tq-mode <m>`, `tq run <ws> --tq-mode <m> -- claude` or the hub
   runner;
6. the repo file `.claude/tq-rules.yaml`: `mode`, `categories` and
   `rules_override`, which can **only tighten**. A value looser than the
   result of the layers above is ignored, and SessionStart names it.

Then `rules_override` (id to `skip|ask|deny|rule`, never a locked id) is
applied per rule, each rule is clamped to the ceiling, and the locked groups
get their own action. `guard.disable` and `guard.actions` keep working: they
change the rule's own action, which is what `rule` means.

The hook reads `TQ_MODE` from its own process env, inherited from the Claude
process, never from the command being checked: `TQ_MODE=relaxed git push
--force` in a Bash call changes nothing (and is denied by
`tq/nested-agent-loosened`). An untrusted manifest contributes no workspace
layer and no grant; a global `strict` still applies there. An unreadable or
invalid mode setting resolves to `default`, never looser.

**Grants.** A mode looser than `default` needs a one-time grant from you:

```
tq allow --mode acme relaxed     # or custom: grants the table tq mode shows now
tq allow --mode acme default     # removes the grant
tq allow --mode --global relaxed # for the mode in guard.yaml
```

- The grant is stored in `$TQ_HOME/trust/<manifest-hash>.mode` as JSON
  (`mode`, `table`, `overrides`, `granted_at`). For `custom` it holds the
  loosest table across the workspace and its projects, exactly what
  `tq mode` shows.
- It is the **ceiling**: the loosest the workspace may go. With no grant the
  ceiling is `default`, so a manifest that asks for relaxed runs at default.
  A custom grant that skips `db-destructive` does not cover a later edit that
  also skips `system`.
- It is **hash-keyed**, like trust. A hand edit of `.tentaqles.yaml` and a
  reflex `tq allow acme` drop it, and `tq allow` says
  `mode grant dropped (was relaxed): run tq allow --mode acme relaxed`.
  tq's own writers (`tq mode set|toggle|override|edit`, `tq guard … --ws`)
  carry the grant to the new hash only when the result is no looser than it.
- A global grant, `$TQ_HOME/trust/global.mode`, carries a SHA-256 of the
  mode keys of `guard.yaml`; an edit of those keys voids it. It is the
  ceiling for neutral folders only. Inside a workspace the workspace's own
  grant is the ceiling, so a global relaxed loosens only workspaces that have
  a grant too.
- `tq deny <ws>` removes the grant. `strict` never needs one
  (`tq allow --mode acme strict` is an error).
- The trust store is protected by `tq/trust-store-write` (locked), so an agent
  cannot write a grant.

**Headless hub runs** use the workspace's `guard.headless_mode` (`default`
when absent; `strict`, `relaxed` or `custom` allowed). A looser value still
needs the workspace grant. See `HUB.md`.

### Launchers, `tq off` and `tq on`

`tq activate` defines shell functions in bash, zsh, fish, PowerShell 7 and
Windows PowerShell 5.1:

| Function | What it runs |
|---|---|
| `claude …`, `codex …` | `tq run . -- claude …` (the folder's identity and guard), or the off launch when the window is off |
| `claude-off …`, `codex-off …` | `tq run --off -- claude …`: no tq for that process only |
| `tq off` | clears the identity env in this window (each variable back to its pre-tq value), sets `TQ_MODE=off` and pauses the prompt hook; `claude` then starts the off launch |
| `tq on` | unsets `TQ_MODE` and re-applies the folder's identity at once |
| `tq …` | anything else goes to the binary |

`tq off` prints
`tq: off in this window - identity cleared, guard off, main logins in use. Run: tq on`.
No window has to be reopened. cmd.exe has no functions: use
`tq run . -- claude` and `tq run --off -- claude` there.

| Action | Identity env | Prompt hook | Guard in the next `claude` |
|---|---|---|---|
| `tq off` | cleared now | paused | none: `claude` starts the off launch |
| `cd` while off | stays cleared | paused | none |
| `tq on` | re-applied for the cwd | resumed | the resolved mode |
| `claude-off` from a normal window | cleared for that process only | unaffected | none |
| `claude --tq-mode strict` | the folder's | unaffected | strict (any mode, clamped by the grant) |

A `claude` session started before `tq off` keeps its guard: its env was
fixed at launch.

**`tq run <ws>|. [--tq-mode <m>] -- <cmd>`.** `.` is the current folder's
workspace (an untrusted one gives a warning and a neutral launch). For
`claude`:

- `--tq-mode <m>` is also stripped from the claude arguments, so
  `claude --tq-mode strict` works through the function. It sets `TQ_MODE`;
  `off` is refused here (use `--off`). With no mode, the layers decide.
- The launcher **always adds `--dangerously-skip-permissions`**, whatever the
  manifest's `claude.permission_mode` says. Only a `--permission-mode` or
  `--dangerously-skip-permissions` you type on that command line replaces it,
  and claude subcommands, `--help` and `--version` get no flag. Bypass no
  longer needs bypass trust: tq's mode is the policy layer, and the grant
  gates what is looser than default. `claude.permission_mode` still becomes
  `permissions.defaultMode` for sessions started outside the launcher (IDE,
  desktop, the hub runner); `tq doctor` notes the override as
  `permission-mode-overridden`.
- Started from an agent session, `tq run` passes the parent's `TQ_MODE`
  through, refuses a `--tq-mode` looser than the parent's mode, and adds no
  bypass flag.

**`tq run --off -- claude|codex [args]`** starts the agent with
`envplan.Clean` of your env plus `TQ_MODE=off`, without `CLAUDE_CONFIG_DIR`
and `CODEX_HOME` (so the main install is used). Claude gets
`--dangerously-skip-permissions --settings '{"enabledPlugins":{"tentaqles@tentaqles":false}}'`,
Codex gets `--dangerously-bypass-approvals-and-sandbox`. It starts only
`claude` or `codex`, and it refuses inside an agent session (`CLAUDECODE`,
`CLAUDE_PLUGIN_ROOT`, `TENTAQLES_HEADLESS`, `TQ_HUB_TASK`, `CODEX_SANDBOX*`).
Whether `--settings` merges `enabledPlugins` per plugin (so your other
plugins stay on) is still a manual check; either way nothing tq does runs,
because of the next point.

**Every tq hook exits at once under `TQ_MODE=off` or `TQ_ENABLED=0`**, with
no output: `tq claude-hook <event>`, `tq env` (prints nothing),
`plugin/scripts/tq_hook.sh` and `tq_run.sh` (before they look for tq or
Python, so the fail-closed "no tq, no Python" block never fires in off), and
`tq statusline` (prints `tq: off`). Skill scripts that run through
`tq_run.sh` therefore do nothing in an off or kill-switch window.

`tq env --off --shell <sh>` prints the shell code of `tq off` and
`tq env --on` prints `unset TQ_MODE`; the functions call them, and
`--off` refuses agent sessions.

The functions are defined as `function claude { ...; }` in bash and zsh,
after the prompt hook is registered, so an `alias claude=...` defined
earlier in the profile can never abort the block. Such an alias, when it
calls `claude` itself (`alias claude='claude --dangerously-skip-permissions'`),
expands to the launcher with extra flags: `tq doctor` notes it
(`claude-alias-shadow`, info). An alias that runs something else, any
PowerShell alias (aliases resolve before functions), or a function named
`claude` or `codex` defined after tq's activation (for example the wrapper
an older `tq migrate` carried over) replaces the launcher: `tq doctor`
warns with `claude-alias-shadow` and names the line to delete.

PowerShell's parameter binder removes a literal `--` from a function's
arguments, so the `tq` function puts it back before the command word of
`tq run`, `tq dotenv run` and `tq redact run`. `tq run` itself stops reading
its own flags at the workspace: after `<workspace>` only `--tq-mode MODE`
and an optional `--` may come before the command, and everything after the
command word is the command's own.

### Nested agents

**`tq/nested-agent-loosened`** (deny, self-protection, locked) stops an agent
from starting another agent in off mode or with loosened settings from a
Bash or PowerShell call:

- assigning `TQ_MODE` to anything but exactly `default` or `strict`
  (`TQ_MODE=off cmd`, `export`, `$env:TQ_MODE=…`, `set -x`, `env TQ_MODE=…`,
  an empty value, `unset`, `Remove-Item Env:TQ_MODE`), or `TQ_ENABLED=0`;
- `claude-off`, `codex-off`, `tq off`, `tq run --off`, `tq env --off`, and
  `--tq-mode off|relaxed|custom` on `tq run` or `claude`;
- `claude` with `--dangerously-skip-permissions`,
  `--allow-dangerously-skip-permissions`,
  `--permission-mode bypassPermissions|auto|dontAsk`, `--bare`,
  `--safe-mode`, `--setting-sources` without `user`, `--settings` with inline
  JSON that switches tentaqles off or empties hooks (a `--settings` file is
  always denied, because the rule cannot read it), and
  `CLAUDE_CODE_SIMPLE=1` or `CLAUDE_CODE_SAFE_MODE=1`;
- clearing or reassigning `CLAUDE_CONFIG_DIR` or `CODEX_HOME`;
- `codex` with `--dangerously-bypass-approvals-and-sandbox`, `--yolo`,
  `-a never`, `-s danger-full-access`, `--disable hooks`,
  `-c features.hooks=false`, `--ignore-rules` or
  `--dangerously-bypass-hook-trust`.

A plain nested `claude -p "…"` or `codex exec "…"` is allowed: it inherits
the parent's identity env and `TQ_MODE`, and so the parent's guard. Commit
messages and heredoc data that only mention these words are allowed too.

Like every shell rule this is a heuristic (`sh -c`, a script written to disk,
an encoded `Start-Process`, aliases and variables evade it). The second line
is that `tq run --off`, `tq env --off` and the `tq mode` writers refuse agent
sessions, and the third that a Claude child of an agent inherits
`CLAUDECODE=1`. Headless hub runs are started by tq, not by a tool call, so
the rule does not apply to them. The Agent tool (subagents) runs in the same
session and the same hooks and needs no rule.

### Controls

**CLI: `tq mode`.**

| Command | What it does |
|---|---|
| `tq mode [--ws NAME] [--json] [--agent codex]` | the resolved mode for the cwd: its name (`relaxed`, `custom (relaxed + 2)`), each category's value and source, `rules_override`, the ceiling (the grant or `default`), clamped and ignored values, and the 24h skip counts. `--agent codex` also lists the confirms that become blocks in Codex. Read-only, so agents may run it |
| `tq mode set default\|relaxed\|strict\|custom [--ws NAME \| --project DIR \| --global] [--base P]` | writes the layer and re-trusts the manifest, carrying a grant forward when it is not loosened. When the result is looser than the ceiling it asks `grant relaxed for acme? [y/N]` and grants on yes; otherwise it prints the `tq allow --mode` line |
| `tq mode toggle CATEGORY skip\|ask\|deny\|rule [scope]` | sets one category; a preset becomes `custom` with it as the base. A locked category is an error |
| `tq mode override RULE-ID skip\|ask\|deny\|rule\|clear [scope]` | edits `rules_override`. A locked id is an error |
| `tq mode edit [scope]` | an interactive form: the preset, then one four-way choice per category (locked rows shown and fixed), then a diff and a confirm |
| `tq mode log [--ws NAME] [--since 24h]` | the confirms and blocks the mode skipped or lowered, with an action column (`deny -> ask`, `ask -> allow`) |

`set`, `toggle`, `override`, `edit` and `tq allow --mode` are **human-only**:
they refuse agent sessions and need an interactive terminal. In a tool call
they are denied first by **`tq/mode-change`** (deny, self-protection), which
also covers `tq allow --bypass` and `tq unattended on|off` (the deprecated
alias), so the agent gets a clear block instead of a terminal error. Writes
to the codex identity follow each workspace write (see `CLAUDE-SETTINGS.md`,
"Codex").

**SessionStart** prints one line in every workspace (and in a neutral folder
when the mode is not default):

```
Guard: relaxed (workspace, granted) · 3 confirms and 1 block skipped in 24h · locked: identity, secrets, self-protection, privacy
Guard: default (relaxed requested, not granted: tq allow --mode acme relaxed) · locked: …
Guard: strict (launch override) · locked: …
Guard: custom (relaxed + db-destructive ask; workspace, granted) · repo file tried to loosen: system · locked: …
```

It adds `Claude mode: auto (adds classifier prompts tq cannot configure)`
when Claude is not in bypass, and `asks become blocks in Codex: N rules` in
Codex.

**Statusline**: `tq: relaxed · 4 skipped` after the client (dim for
default, yellow when looser than default, cyan for strict), and `tq: off` in
red under `TQ_MODE=off`. See `CLAUDE-SETTINGS.md`.

**Desktop app**: the Guard tab changes the mode, the categories and the
overrides, and grants or revokes (see `DESKTOP.md`). It is a human GUI with
no agent path, so it may grant.

**Hub page**: `/guard` shows the resolved mode, the table and the 24h counts
read-only, with no form and no POST route (see `HUB.md`).

### Audit

Every call whose confirm or block the mode skipped or lowered is appended to
`$TQ_HOME/guard/mode.jsonl`: time, workspace, scope (`workspace`,
`project:<dir>`, `launch`, `global`), the tq mode, Claude's permission mode,
the action skipped, the action applied (`to`), rule ids, tool and command.
The command is masked with the workspace's data classes and cut to 200
characters; the file rotates to `mode.jsonl.1` at 5 MB, and write errors
never fail the hook. `tq mode log` and the counts also read the
legacy `unattended.jsonl`, until it ages out.

`tq doctor` warns with `mode-not-granted`, `mode-unknown-rule`, `mode-cloud`
(a mode that skips prod-changes or cloud-destructive in a workspace with a
cloud identity) and `guard-locked-override`; see `CLAUDE-SETTINGS.md`.

Coming from unattended mode? See `MIGRATE.md`, "Migrating from unattended
mode".

## `session-start` output

Sample for a trusted workspace:

```
Client: Acme (en)
Git: github.com as acme-bot (dev@acme.com)
Cloud: azure (acme-prod subscription)
Identity: acme · CLAUDE_CONFIG_DIR=/home/acme/.tentaqles/identities/acme/claude · permission_mode=default

tq doctor:
- [ok] all checks passed

Rules: tq blocks git/gh/cloud commands on identity drift (exit 2). Run `tq doctor` for details.
Guard: default · locked: identity, secrets, self-protection, privacy
```

The `Guard:` line is described in [Controls](#controls).

Sample for a neutral cwd (outside any registered base):

```
Client: none (neutral cwd: outside any base)

Rules: remote git, gh and cloud CLI commands are blocked here until you cd into a trusted workspace (tq allow <name>).
```

## Fallback mode (tq not installed)

`plugin/scripts/tq_hook.sh` is what the plugin's hooks actually invoke. It
resolves a `tq` binary (env var → plugin-bundled binary → `PATH` → known
install dirs) and runs `tq claude-hook <event>`, passing stdin through and
propagating the exit code.

Under `TQ_MODE=off` or `TQ_ENABLED=0` the script exits 0 before it looks
for tq or Python, so none of the fallbacks below applies in an off window.

If no `tq` binary can be found or run:

- `session-start` prints a one-line notice ("tq is not installed —
  identity enforcement is in fallback mode...") and exits 0.
- `pre-tool-use` falls back to `plugin/scripts/identity-guard.py`, a
  dependency-free Python script that still blocks remote git/gh/cloud
  commands when identity can't be verified (fail-closed). It deliberately
  does not go through the plugin's normal Python bootstrap
  (`tq_run.sh`/`tq_env.sh`), since that path can synchronously
  `pip install` and blow the `PreToolUse` timeout.
- **If no Python interpreter can be found either** (checked via `py -3` on
  Windows, then `python3`, then `python`), `pre-tool-use` blocks (exit 2)
  outright: on a box with neither `tq` nor Python, **every Bash and
  PowerShell command is blocked** until one of them is installed. This is
  intentional — it is the only way to guarantee no unverified remote
  command slips through. Payloads that clearly name another tool (Read,
  Edit, an MCP tool, …) are let through, since the widened `PreToolUse`
  matcher would otherwise block every file read too.
- The fallback has **no policy rules**: those live in the `tq` binary.

## Hand-testing

From a POSIX shell (bash/zsh, or Git Bash on Windows):

```sh
echo '{"tool_name":"Bash","cwd":"'$PWD'","tool_input":{"command":"git push"}}' | tq claude-hook pre-tool-use; echo $?
```

From PowerShell:

```powershell
'{"tool_name":"Bash","cwd":"' + ($PWD.Path -replace '\\','\\\\') + '","tool_input":{"command":"git push"}}' | tq claude-hook pre-tool-use; $LASTEXITCODE
```

`"tool_name"` is required: a payload without it always looks like an
allow. The JSON must be valid — a mistyped backslash in a Windows path
makes the payload unparseable, which is also an allow. To test a policy
rule on a file tool:

```sh
echo '{"tool_name":"Read","cwd":"'$PWD'","tool_input":{"file_path":"'$PWD'/.env"}}' | tq claude-hook pre-tool-use --json
```

A block prints `BLOCKED: ...` to stderr and the exit code is `2`; an
allowed command prints nothing and exits `0`.

To test `session-start`:

```sh
echo '{"tool_name":"Bash","cwd":"'$PWD'"}' | tq claude-hook session-start
```

## Policy rules (`pre-tool-use`)

After the identity guard (shell tools only), every call is checked against
a rule set. The strictest matching rule wins: **deny** (exit 2) beats
**ask** (JSON on stdout) beats allow. Every matching rule at that level is
listed in the reason.

### Layers

| Layer | Where | Trusted? | Can do |
|---|---|---|---|
| Built-in | compiled into `tq` (`internal/policy/builtin.go`) | yes | default rules, ids `tq/...` |
| Manifest | `guard:` in `.tentaqles.yaml` (hash-pinned by `tq allow`) | yes | add rules, **disable** built-ins by id |
| Project | every `.claude/tq-rules.yaml` from cwd up to the workspace root | no (repo content) | **add** rules, and set a guard mode that only **tightens** |

Nothing in a repository can loosen the guard: a project file's `disable`
key is ignored, and an untrusted manifest contributes no rules at all. A
rule that fails to compile is skipped; the other rules still apply.

### Rule format

```yaml
# .tentaqles.yaml
guard:
  disable: [tq/mcp-n8n-write]        # this client's n8n is a sandbox
  rules:
    - id: acme/no-prod-host
      action: deny                   # deny | ask
      command: 'prod-db\.acme\.internal'
      reason: never touch the prod database host from an agent
```

Matchers (all regexes, case-insensitive; every one present must match):

- `tool` — the tool name, anchored (`Edit|Write`, `mcp__.*n8n.*__.*`).
- `command` — the shell command (Bash/PowerShell only).
- `path` — the file path, with `\` normalized to `/`, so one rule works on
  Windows and POSIX.
- `content` — the text being written (Write `content`, Edit `new_string`,
  MultiEdit edits) or an MCP call's input JSON.
- `except` — cut out of each subject before matching, e.g. exempt
  `.env.example` without exempting a `.env` in the same command.
- `existing_only: true` — only when the path already exists (edits to a
  migration that was already written).

### Built-in rules

| id | action | catches |
|---|---|---|
| `tq/secret-store-read`, `tq/secret-store-shell` | deny | SSH private keys, `~/.aws/credentials`, `.git-credentials`, `.netrc`, docker/gh/azure token files, the tq catalog |
| `tq/env-file-read`, `tq/env-file-shell` | deny | reading or printing `.env*` (templates such as `.env.example` exempt) |
| `tq/env-dump` | deny | `printenv`, `env`, `set`, `export -p`, `declare -p`, `typeset -p`, `cmd /c set`, `Get-ChildItem`/`gci`/`dir`/`ls env:`, each as the command word of a segment (start, or after `;` `&` `\|` `(` a backtick or a newline, or as the first word of a quoted `-c` script: `bash -c 'set'`) with no arguments; and `[Environment]::GetEnvironmentVariables(` anywhere. `set -e`, `echo set` or `git commit -m "set the flag"` are not dumps. Bash heredoc bodies in the data shapes below are not read |
| `tq/force-push-main` | deny | any force push (incl. `--force-with-lease`, `+main`) to main/master |
| `tq/force-push` | ask | plain `--force`/`-f` push elsewhere (`--force-with-lease` is fine) |
| `tq/gh-admin-merge` | ask | `gh pr merge --admin` |
| `tq/gh-repo-delete` | deny | `gh repo delete` |
| `tq/cloud-delete`, `tq/iac-destroy` | ask | `az/aws/gcloud/doctl/databricks … delete/rm/purge/…`, `terraform/cdk/pulumi destroy` |
| `tq/iac-apply` | ask | applying infrastructure to a live environment: `terraform/tofu apply`, `pulumi up`, `cdk/cdktf deploy`, `kubectl apply`, `helm install/upgrade`, `sam/serverless/sls deploy`, CloudFormation, Azure and Deployment Manager deployments (`--dry-run` exempt) |
| `tq/remote-migration` | ask | running migrations (`prisma migrate deploy`, `alembic upgrade`, `flyway migrate`, `supabase db push`, `rails db:migrate`, `manage.py migrate`, …) against a connection that is not local; a command naming `localhost`, `127.0.0.1` or `--local` is exempt |
| `tq/settings-widen` | deny | Edit/Write of Claude Code settings (`.claude/settings[.local].json`, `.claude.json`, or a bare `settings[.local].json`) that adds `Bash(*)`, `bypassPermissions`, `disableAllHooks: true`, empty `hooks`, or `skipDangerousModePermissionPrompt: true`; permission changes go through `tq settings render` and the manifest |
| `tq/settings-shell-write` | deny | writing `.claude/settings[.local].json`, `.claude/tq-rules.yaml` or `.claude.json` from the shell (redirects, `tee`, `sed -i`, `cp`/`mv` onto them) |
| `tq/registry-write` | ask | `reg add/delete`, registry writes from PowerShell |
| `tq/tracker-cli-write` | ask | a write to the company tracker from its CLI, outside the hub's sync engine (so the `sync.policy.*` settings and the outbound redaction do not apply): `az boards work-item create/update/delete/relation add\|remove`; `az rest` with a write method (`--method`, `-m`) on `dev.azure.com` or `visualstudio.com`; `az devops invoke --http-method` with a write; `gh issue edit/close/create/comment/reopen/delete/transfer/lock/unlock/pin/unpin/develop`; `gh project item-edit/add/create/delete/archive`; `gh api` with a write (`-X`/`--method` POST, PATCH, PUT or DELETE, also `-XPATCH`, or a field flag, which makes gh POST) on an issues or projects path; `gh api graphql` with a `mutation`, or with a query read from a file (`-F query=@…`, `--input`), which the rule cannot read; `acli jira workitem edit/transition/create/comment/delete/assign/clone/link/archive/unarchive`. The tool must be the command word (bare, path-prefixed or quoted; flags may sit before the subcommand, as in `gh -R acme/app issue edit`), so a PR title that only mentions "issue create" is not a write. Use `tq hub sync` or `/tentaqles:sync` instead |
| `tq/sql-destructive-shell` | ask | DROP/TRUNCATE/ALTER/DELETE/UPDATE/GRANT through psql, sqlcmd, snowsql, supabase db, bq, … |
| `tq/migration-edit` | ask | editing an existing file under a migrations directory |
| `tq/mcp-sql-write`, `tq/mcp-apply-migration` | ask | write/DDL statements or migration/branch operations through a database MCP server |
| `tq/mcp-n8n-write` | ask | creating, updating, publishing, executing or deleting n8n workflows/agents |
| `tq/mcp-github-write`, `tq/mcp-hosting-exec` | ask | GitHub writes and hosting-provider operations through MCP |
| `tq/gha-branch-ref` | deny | workflow actions pinned to a branch: `@main` or `@master` |
| `tq/gha-unpinned` | ask | workflow actions pinned to `@latest` or `@head` |
| `tq/gha-pr-target` | ask | `pull_request_target` in a workflow |
| `tq/system-dir-write` | deny | an Edit or Write under a system folder, and a shell write or delete there (redirect, `tee`, `rm`, `mv`, `Set-Content`, `Remove-Item`, …; a copy only when the system folder is the destination): `C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\ProgramData` (also as `/c/...`), `/etc`, `/usr`, `/bin`, `/sbin`, `/boot`, `/System`, `/Library`. Reads are fine |
| `tq/elevation` | ask | `sudo`, `doas`, `su -c`, `runas`, `gsudo`, `Start-Process … -Verb RunAs`, `pkexec` |
| `tq/mode-change` | deny | `tq mode set\|toggle\|override\|edit`, `tq allow --mode\|--bypass` and `tq unattended on\|off` (the deprecated alias) as a command word: they are human-only (see [Controls](#controls)). `tq mode`, `tq mode log` and an `echo` or commit message that mentions them stay allowed |
| `tq/nested-agent-loosened` | deny | starting an agent in off mode or with loosened settings (see [Nested agents](#nested-agents)) |

More checks need I/O and are not regex rules (each can be disabled by id in
the manifest):

- `tq/commit-secret` (deny) — before a `git commit` runs, scans what it
  would record: the staged diff, plus the working tree and untracked files
  when the command also runs `git add` or `commit -a`, so
  `git add -A && git commit` is covered. Committing a `.env`, private key
  or `.pem` is refused by name. The reason lists `file:line (pattern)`
  and never the value. A known-fake fixture line can carry
  `tq:allow-secret` (or `gitleaks:allow`).
- `tq/commit-pii`, `tq/push-pii`, `tq/text-pii` (deny) — personal and
  financial data at the exits. `tq/commit-pii` scans the lines a commit would
  record (same scope as `tq/commit-secret`); `tq/push-pii` scans the commits a
  push would publish; `tq/text-pii` scans commit and tag messages and `gh pr`,
  `issue` and `release` text, including `--body-file`, and `gh api` fields
  (`-f`, `-F key=@file`, `--input`); text built by a command substitution
  asks. The reason lists
  `file:line (detector)` and never the value. Checksum-validated detectors
  (the `checksummed` class: CPF, CNPJ, card, IBAN) always run; e-mail and
  phone run only when the manifest's `data.classes` declares `pii`,
  `sensitive` or `children`. Declaring `financial` adds no detector beyond
  the checksummed ones. A known-synthetic line carries `tq:allow-pii`, but
  the marker only counts in test files (`*_test.go`, `test_*.py`,
  `*_test.py`, `conftest.py`, `*.test.*`, `*.spec.*`) and inside test and
  fixture directories (`tests/`, `fixtures/`, `seeds/`, `testdata/`, …),
  never under `docs/` or `migrations/` and never in published text. The
  scans run in the repository the command acts on (a leading `cd <dir>`,
  then `git -C <dir>`), cover `git commit <paths>`, and decode UTF-16 files.
  All of them share one 8 s budget per hook call (`TQ_HOOK_SCAN_BUDGET`, at
  most 12 s). If a scan cannot finish or vouch for the commit (a directory
  tq cannot resolve, a text file over 1 MiB, more than 8 MiB in total, the
  budget running out, an oversized push, unparseable quoting) the rule asks
  instead of denying. `tq redact` is the helper for a shareable copy; see
  [Using classified data without printing it](#using-classified-data-without-printing-it).
- `tq/pii-fixture-dump` (deny) — with a `data:` block in the manifest, a shell
  command that reads a classified path or table (`data.paths`, matched
  case-insensitively and in absolute or relative form) and writes into
  `tests/`, `fixtures/` or `seeds/`, including PowerShell writers
  (`Set-Content`, `Out-File`, `Export-Csv`, `Copy-Item`, ...). Fixtures must be synthetic. Its message
  points to `docs/privacy.md`, the per-project record of classified data that
  the planned development process asks for; tq only points at it.
- `tq/commit-message` (ask) — a `git commit` whose message (read from `-m`,
  `-F` or a heredoc) is not Conventional Commits: `type(scope): summary` with
  a known type, a summary of at most 100 characters, and a body when the
  change is breaking (`!` or `BREAKING CHANGE`). It also asks when it cannot
  read the message safely: an unreadable or oversized (over 64 KiB) `-F` file,
  or an unterminated quote. Commits with no readable message are skipped:
  the editor, `--amend`, `-C`, `--squash`, `--fixup`, `--no-edit` without
  `-m`/`-F`, `-F -`, and a message built by a shell substitution.
- `tq/cross-client-mcp` (deny) — an MCP server that another client's
  bundle declares, and this workspace's bundle does not, is refused.
- `tq/outside-path-write` (ask) — a write or recursive delete on an
  **absolute** path outside the workspace root, the session's cwd, the
  registered bases (`tq` registry), the temp folders (`$TMPDIR`/`%TEMP%`,
  `/tmp`, `/var/tmp`) and the agents' own working folders (`projects/`,
  which holds the auto-memory, `plans/` and `todos/` under
  `$CLAUDE_CONFIG_DIR`, else `~/.claude`; `sessions/` and `memories/` under
  `$CODEX_HOME`, else `~/.codex`; the config dirs themselves are not roots,
  so their settings files keep the confirm): `rm -r`, `Remove-Item -Recurse`, `rmdir /s`, a
  redirect, `tee`, a `cp`/`mv` destination, `Set-Content`/`Out-File`/
  `Add-Content`, `sed -i`, and Edit or Write with such a `file_path`. `~` and
  `$HOME` count as absolute, and Git Bash `/c/...` paths are understood. It
  needs the roots, so the hook computes it instead of a regex.

Every rule, built-in or not, also has a category; the
[guard mode](#guard-modes) maps each category to skip, ask, deny or the
rule's own action.

### Limits

- The policy reads tool input as text. Like the identity guard it catches
  honest mistakes, not an adversary: `python -c "open('.env').read()"`
  is not a `cat`.
- A command that runs `git -C other-repo commit` is scanned in the hook's
  cwd, not in `other-repo`.
- A malformed payload is allowed; only an oversized one fails closed.

## Using a .env without printing it

Reading or printing a `.env` puts its secrets in the session transcript, so
`tq/env-file-read` (the Read and Grep tools) and `tq/env-file-shell` (`cat`,
`type`, `Get-Content`, `grep`, … on a `.env`) **deny** it. The refusal tells
the agent what to do instead, and so does the session-start message, so it
retries without asking you:

```
tq dotenv run -- npm run dev
tq dotenv run --file .env.local -- bash -c 'psql "$DATABASE_URL" -c "select 1"'
```

`tq dotenv run` loads the file into that one command's environment
(variables already set win unless `--override`) and replaces every loaded
value of 6+ characters with `[REDACTED:NAME]` in the command's stdout and
stderr. The agent only ever writes `$NAME`. Templates (`.env.example`,
`.sample`, `.template`, …) stay readable. `tq dotenv run` runs an arbitrary
command, so it is never pre-approved: it goes through the permission mode
like the command it wraps.

## Data classification (`data:` block)

Reading, querying and processing personal data locally is allowed: Claude can
read files, query tables and run scripts over data that holds personal
information. The guards sit at the exits, where data would be recorded or
published. tq guards commits, pushes, PR and issue text, and fixtures. tq has
no rule for the memory, vault and model exits; the Claude Code plugin's PII
redaction for those exits is planned for plugin 0.12.0.

A client workspace declares what it handles in its manifest:

```yaml
data:
  classes: [pii, financial]          # public, internal, pii, sensitive, financial, children, secrets
  regions: [BR]                      # where subjects live (two-letter codes)
  prompts:
    allow: [public, internal]        # classes that may appear in model context unmasked
  paths:                             # where classified data lives
    - path: db/schema/customers.sql
      classes: [pii]
    - table: public.payments
      classes: [financial]
  retention_default: 24m
  incident:
    authority: ANPD                  # ANPD | GDPR | none
    contact: dpo@example.test
```

Each `paths` entry has exactly one of `path` (a glob) or `table`, plus
`classes`. An unknown class, or `paths`/`prompts` without `classes`, makes the
manifest fail to load. The block drives which detectors the exit rules run
(without one, only the checksum-validated detectors run) and
`tq/pii-fixture-dump`. Changing the manifest changes its trust hash: run
`tq allow <ws>`. `tq doctor` warns with `data-block-missing` when a repo has
personal-data columns and no block (see `docs/CLAUDE-SETTINGS.md`).

## Using classified data without printing it

Reading personal data locally is allowed. What the guard refuses is recording
it: the exit rules `tq/commit-pii`, `tq/push-pii` and `tq/text-pii` (commit
messages and diffs, pushes, PR and issue text) point here. `tq redact` is the
helper for making a shareable copy without the values:

```
tq redact --classes pii < export.csv
tq redact --count < export.csv
tq redact --file a.csv --file b.csv
tq redact run --classes pii,financial -- psql "$DATABASE_URL" -c "select * from customers limit 5"
```

`tq redact` reads stdin (or each `--file`) and replaces CPF, CNPJ, e-mail,
phone, card and IBAN values (checksum-validated where one exists) and secrets
with `[REDACTED:<kind>]`; `--count` prints findings per detector and never
the values. `tq redact run` runs a command directly (not through a shell) and
masks its stdout and stderr, propagating its exit code.

Default classes: the trusted workspace manifest's `data.classes` minus
`data.prompts.allow`, plus `secrets`. Outside a workspace, or without a
`data:` block, every detector runs (`all`). `--classes` overrides this; an
unknown class or an empty list is an error. Whatever the classes, the
checksum-validated detectors (CPF, CNPJ, card, IBAN) always run, exactly as
in the exit rules, so a copy made with `tq redact` passes `tq/text-pii`:
`pii`, `sensitive` and `children` add e-mail and phone, `secrets` adds the
secret patterns, `all` runs everything. A card number followed by its expiry
(`MM/YY`) or its CVV is still masked.

## Turning rules on and off

Every rule has an id (`tq guard list` shows them all with their current
state). Change them with `tq guard`, or ask Claude with the `guard` skill:

```
tq guard list [--ws NAME]
tq guard off tq/cloud-delete --all              # every workspace
tq guard on  tq/cloud-delete --ws acme          # back on, for acme only
tq guard set tq/force-push deny --ws acme       # ask -> deny
tq guard set tq/force-push default --ws acme    # remove the override
```

Three trusted layers decide, most specific last:

1. tq's built-in rules (including the Jev `jev/*` rules);
2. `~/.tentaqles/guard.yaml`, written by `--all`: `disable`, `actions`
   (`ask`/`deny` per id) and extra `rules`, for every workspace;
3. the workspace manifest's `guard` block, written by `--ws`: `disable`,
   `enable` (switches back on what the global file turned off), `actions`
   and `rules`. It wins over the global file.

A repo's `.claude/tq-rules.yaml` is not a layer here. It is repo content,
so it can only add rules (and tighten the guard mode) and can never turn one
off.

The locked rules (categories `identity`, `secrets`, `self-protection` and
`privacy`, see [Categories](#categories)) cannot be switched off or loosened:
a `disable` entry or a looser `actions` entry naming one is ignored, and
`tq doctor` warns with `guard-locked-override`. Switching a whole category,
or the mode, is `tq mode` (see [Guard modes](#guard-modes)).

`--ws` edits only a manifest that is currently trusted, checks that the
result still loads, and re-trusts it; a manifest with unreviewed edits is
refused. A global file that does not parse is ignored as a whole, so every
rule it changed is back to its default.

The guard protects its own settings: `tq guard off|on|set`, `tq allow` and
`tq deny` ask before they run (`tq/guard-change`), the mode writers and
`tq allow --mode` are denied (`tq/mode-change`, human-only), and direct edits to
`guard.yaml` from file tools or the shell are denied
(`tq/guard-file-write`). An agent can propose a change; you confirm it.
The shell rule denies any command line that names the file, but not a
heredoc body that is only data and mentions it (see below).

The same goes for tq's trust store (`$TQ_HOME/trust/`, which holds the
trust markers and the mode grants), its registry (`$TQ_HOME/config.yaml`)
and the files tq renders into an identity (`identities/<ws>/codex/rules/`,
`hooks.json` and `config.toml`, and `identities/<ws>/claude/settings.json`
and `settings.local.json`): `tq/trust-store-write` denies Edit, Write, MultiEdit and NotebookEdit on
them, and shell writes: redirects, `tee`, `sed -i`, `rm`, `mv`, `cp` onto
them, `Set-Content`, `Add-Content`, `Out-File`, `Remove-Item`,
`Move-Item`, `Copy-Item` onto them, and `python -c` / `node -e` one-liners
that name them and call a write. Both the `~/.tentaqles` and the
`$TQ_HOME` (`${TQ_HOME}`, `$env:TQ_HOME`, `%TQ_HOME%`) spellings match; a
custom `TQ_HOME` written out as a literal path does not. Reads (`cat`,
`Get-Content`, Read, `cp` from them) stay allowed. `tq allow`, `tq deny`,
`tq init` and `tq settings render` change these files.

### Heredocs: data and code

For the **Bash** tool only, `tq/guard-file-write` and
`tq/trust-store-write` (shell rules) and `tq/env-dump` skip heredoc bodies when the *whole command* is a single
command (no `;`, `&&`, `||`, `|`, `&` or second line outside the bodies)
of one of these shapes, and only then:

- `cat` or `tee` reading a heredoc: `cat > f <<EOF`, `cat <<EOF > f`,
  `cat <<EOF`, `tee f <<EOF`, where no word is a path into a `.git`
  directory or a `hooks/` directory, or is named like a git config
  (`config`, `.gitconfig`, `.gitmodules`) or a git hook (`pre-commit`,
  `commit-msg`, …), and no word holds a `$` expansion;
- `git commit|tag|notes … -F -` (or `-F-`, `--file=-`, `--file -`)
  reading a heredoc, where commit/tag/notes is git's first non-option word
  (only `-C <dir>` and `--no-pager` may come before it; `-c` never);
- `git commit|tag … -m|--message "$(cat <<'EOF' … EOF)"` and
  `gh pr|issue create|edit|comment … --body|--title "$(cat <<'EOF' … EOF)"`,
  where the `$( )` holds only `cat <<WORD` and closes on the line after
  the delimiter.

Anything else and **every body is read, as before**:

- a second command, chained or on another line (`cd docs && cat > f <<EOF`,
  `cat > f <<EOF … EOF` then `ls`, two heredoc commands in a row);
- any pipe (`cat <<EOF | bash`, `| sudo -u root bash`, `| busybox sh`,
  `| tee`);
- any other `$( )`, backtick or `<( )` (`find -exec sh -c "$(cat …)"`,
  `su -c "$(…)"`, `eval "$(…)"`);
- a leading variable assignment, a wrapper (`sudo`, `env`, `xargs`, `wsl`,
  `su`, `find`, `make`, …) or any other command word (`bash x.sh`,
  `./x.sh`, `make -f mk`);
- an unterminated heredoc, a delimiter with a quote or backslash other than
  one pair around the whole word (`<<E"O"F`, `<<\EOF`), or an opener line
  that ends inside a quote or in a `\` continuation.

The command lines are always read, so `cat > ~/.tentaqles/guard.yaml <<EOF`
is still denied. A `<<` inside quotes (including `$'…'`), inside a `${ … }`
expansion or after a `#` comment does not start a heredoc. **PowerShell** commands are always read whole; a here-string
(`@' … '@`) is never data.
