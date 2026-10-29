# Spec E: guard modes (off, default, relaxed, strict, custom)

**Replaces:** unattended mode (`guard.unattended`, `tq unattended`, docs/CLAUDE-HOOK.md "Unattended mode"). **Absorbs:** the unmerged branch `feat/unattended-skip-denies` (worktree `tentaqles-wt/unattended-denies`, commits `af50ba5`, `38719ed`, `ade3680`).
**Status:** draft for approval, 2026-10-29.

## 1. Decisions (from the human, binding)

| Topic | Decision |
|---|---|
| Modes | Five: `off`, `default`, `relaxed`, `strict`, `custom`. `default` applies when nothing is set. |
| off | No tq at all: no identity env, the main user install (`~/.claude`, `~/.codex`), no guard, the tentaqles plugin switched off for that launch, the session in bypass. Entry points: the launchers `claude-off` / `codex-off`, and the shell functions `tq off` / `tq on` defined by `tq activate`. Any tq hook that still fires under `TQ_MODE=off` exits 0 at once. |
| default | tq as it was before unattended mode: every rule at its own action. |
| relaxed | Replaces unattended. Every toggleable category is skipped (no ask, no deny), except `git-history` and `system`, which ask. So force-push to main, repo delete and writes outside the workspace or to system folders get a confirm. The locked groups stay. |
| strict | Like default, but `prod-changes`, `cloud-destructive` and `git-history` deny, and `external-writes` asks. |
| custom | Any preset plus per-category overrides. |
| Locked groups | `identity`, `secrets`, `self-protection` and `privacy` are enforced in every mode except off and cannot be toggled. (`privacy` was added at spec review, §19.) |
| Nested agents | A new self-protection rule: an agent may not launch another agent in off mode or with loosened settings. Headless hub runs launched by tq are not tool calls, so they are unaffected. |
| Categories | Every guard rule (built-in, Jev, hook rules, pack guards) has exactly one category (§5). A test fails on an unclassified rule. |
| Values | Each category takes `skip`, `ask`, `deny` or `rule` (the rule's own action). Rule-level overrides stay as `guard.rules_override: {id: skip\|ask\|deny\|rule}` and never touch the locked groups. |
| Precedence | Global `~/.tentaqles/guard.yaml`, then the workspace manifest, then the project, then a per-launch override. A repo file can only tighten. Anything looser than `default` needs a one-time human grant, `tq allow --mode <ws> <mode>`, which generalises bypass trust. Headless hub runs use `default` unless the workspace opts in. |
| Two layers | tq's mode is independent of Claude Code's permission mode. The coupling "applies only in bypass/auto sessions" is dropped. We recommend always launching Claude in bypass: tq's mode becomes the single policy layer. |
| Controls | CLI `tq mode` (human-only), a desktop Guard tab, a read-only section on the hub page, a statusline segment and a SessionStart line. `tq unattended on\|off` stays as a deprecated alias. |
| Agents | Claude Code: full coverage. Codex: best effort, through launch flags, `config.toml`, exec-policy `.rules` and Codex hooks. OpenCode and Kimi Code: a follow-up spec. |

## 2. Goals, non-goals, success criteria

**Goals**
- One knob a human understands: "this workspace runs `relaxed`", instead of permission modes, bypass trust, `guard.unattended`, `keep_asking`, `guard.disable` and `guard.actions` combined by hand.
- A clean way out of tq ("pure" Claude Code or Codex) without editing profiles, setting `TQ_ENABLED=0` and reopening a terminal.
- Identity, secrets, privacy (PII at the exits) and tq's own protections hold in every mode tq is active in.
- No mode change can come from an agent, from a repo file, or from a hand-edited manifest that a human re-trusts by reflex.

**Non-goals**
- A sandbox. The guard stays defense in depth on top of env isolation (CLAUDE-HOOK.md, "What this guard does not catch").
- Changing what each rule matches. Only the action applied to a match changes.
- OpenCode and Kimi Code support (§15).
- Per-tool-call mode switches from inside a session.

**Success criteria**
- With nothing configured, every existing test passes unchanged and the decisions equal today's `main` (default = pre-unattended behaviour).
- `TestEveryRuleHasCategory` fails when any rule from `policy.Builtin()`, `guardcfg.HookRules`, `decide.BuiltinJudgeRules()`, `guardcfg.GuardErrorID`, the identity guard ids or a registered pack guard has no category, or when the table names an unknown id.
- A relaxed session in a granted workspace skips every toggleable rule outside `git-history` and `system`. It asks on `git push --force origin main`, `gh repo delete`, `rm -rf` of an absolute path outside the workspace and a write under `C:\Windows`. It still denies `cat .env`, a commit that records PII, `tq guard off`, a write to `$TQ_HOME/trust/**` and `claude-off`.
- `tq off` in a PowerShell, bash, zsh and fish window clears the identity env and stops the per-prompt hook; `tq on` brings it back on the next prompt; no window is reopened.
- An agent cannot reach `relaxed`, `custom` or `off` by editing the manifest, `.claude/tq-rules.yaml`, `guard.yaml`, the trust store, an env var, or a nested launch.

## 3. The two-layer model

Two independent layers decide what happens to a tool call.

| | Claude Code permission mode | tq guard mode |
|---|---|---|
| Who owns it | Claude Code (`--permission-mode`, `--dangerously-skip-permissions`, `permissions.defaultMode`) | tq (`guard.mode`, `TQ_MODE`, `tq mode`) |
| Values | `default`, `acceptEdits`, `plan`, `auto`, `dontAsk`, `bypassPermissions` | `off`, `default`, `relaxed`, `strict`, `custom` |
| What it decides | When Claude asks the human for calls no rule covers | allow / ask / deny for each tq rule that matches |
| Configurable per rule | Settings allow/deny/ask lists; auto mode adds a classifier you can only partly tune | Yes: categories, `rules_override` |
| Runs in bypass | Settings deny and ask rules still apply; allow rules have no effect (verified in the Claude Code permissions docs) | Yes: the PreToolUse hook runs in every mode |

The recommended setup is **Claude in `bypassPermissions`, tq in the mode you want**. Then tq is the single policy layer: a tq `ask` is the only prompt you see, apart from the few calls Claude Code never auto-approves (critical-path `rm`, `AskUserQuestion`, MCP tools marked `requiresUserInteraction`). Auto mode only adds classifier prompts that tq cannot configure.

- *Assumption A1:* a PreToolUse hook `"permissionDecision": "ask"` still prompts in `bypassPermissions`. The permissions docs say a hook "can deny the tool call, force a prompt, or skip the prompt", and list ask rules among the actions no mode auto-approves, but they do not say it explicitly for hook asks in bypass. The current unattended code already relies on it. Verify by hand (§13, manual checks) before release. If it fails, tq's `ask` in bypass must be emitted as a settings ask rule, or the launcher must use `auto` for default and strict.
- *Assumption A2:* a PreToolUse hook exit 2 / `deny` blocks in `bypassPermissions`. The docs say a blocking hook "stops the tool call before permission rules are evaluated", which reads as mode-independent.

Dropping the coupling: today `guardcfg.Unattended` returns false unless the session's `permission_mode` is `bypassPermissions` or `auto`. In spec E the mode applies whatever Claude's permission mode is. A `relaxed` workspace run in Claude's `default` mode skips tq's asks, and Claude itself still prompts for unlisted calls.

## 4. The modes

The table gives each category's value per preset. `rule` means the rule's own action (after `guard.actions`).

| Category | default | relaxed | strict |
|---|---|---|---|
| identity (locked) | rule | rule | rule |
| secrets (locked) | rule | rule | rule |
| self-protection (locked) | rule | rule | rule |
| privacy (locked) | rule | rule | rule |
| git-history | rule | ask | deny |
| prod-changes | rule | skip | deny |
| cloud-destructive | rule | skip | deny |
| db-destructive | rule | skip | rule |
| system | rule | ask | rule |
| external-writes | rule | skip | ask |
| ci-supply-chain | rule | skip | rule |
| hygiene | rule | skip | rule |
| packs | rule | skip | rule |

### 4.1 off

- **What runs:** nothing from tq. No identity env (`CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`, `CODEX_HOME`, git includes from env, ...), the main user install, no PreToolUse guard, no SessionStart preamble, no memory hooks.
- **How the plugin is switched off:** the main `~/.claude` has the tentaqles plugin enabled (`tentaqles@tentaqles`). The off launcher passes `claude --dangerously-skip-permissions --settings '{"enabledPlugins":{"tentaqles@tentaqles":false}}'`.
  - *Assumption A3:* `--settings` merges `enabledPlugins` per key, so only tentaqles is turned off and the user's other plugins stay on. The CLI reference says "Values you set here override the same keys in your settings.json files … Keys you omit keep their file-based values", which does not say whether an object such as `enabledPlugins` is replaced or merged. If it is replaced, the launcher must read the effective `enabledPlugins` from `~/.claude/settings.json` and pass the full map with tentaqles set to false.
  - Belt and braces: the launcher sets `TQ_MODE=off`, and every tq hook entry point exits 0 at once when it sees it (§8.4). So even if A3 is wrong in the other direction (the plugin stays loaded), nothing tq does runs.
- **off is never stored.** It is not a valid value of `guard.mode` in any file. It exists only as a launch (`claude-off`, `codex-off`, `tq run --off`) or a window state (`tq off`). A manifest or `guard.yaml` with `mode: off` is a load error.
- **Residual risk**, stated in the docs and in the `tq off` banner: in off mode git, gh, cloud CLIs and MCP servers use the main logins and the main install's credentials. A command in a client folder can push with the wrong email, deploy to the wrong subscription, or call another client's MCP server. Nothing blocks a secret read, a force push to main or a repo delete. Use off for tq-free work and debugging, not for client work.

### 4.2 default

Every rule at its own action: asks on prod changes, cloud deletes, IaC apply/destroy, admin merges, force pushes and destructive SQL; denies force-push to main and repo delete. This is exactly `main` before unattended mode. It applies when no layer sets a mode, in untrusted or unmanaged folders, and for headless hub runs unless the workspace opts in.

### 4.3 relaxed

Every toggleable category is `skip`, except `git-history` and `system`, which are `ask`. That has two effects:
- **git-history:** the two hard denies `tq/force-push-main` and `tq/gh-repo-delete` become confirms (the human's call), and `tq/force-push` and `tq/gh-admin-merge` stay confirms. This resolves the branch report's concern 1 (both denies became skippable).
- **system:** `tq/system-dir-write` (a deny by default) becomes a confirm, and `tq/outside-path-write`, `tq/elevation` and `tq/registry-write` stay confirms.

The PII rules are in the locked `privacy` group, so relaxed keeps their denies.

Relaxed is looser than default, so it needs a grant (§6.3). Without a grant the session runs at `default` and SessionStart says so.

### 4.4 strict

Default plus: `prod-changes`, `cloud-destructive` and `git-history` deny; `external-writes` asks (its rules ask already, so this pins the ask against a looser `rules_override` or `guard.actions` lower down). Strict needs no grant: it is never looser than default.

### 4.5 custom

A base preset plus category overrides:

```yaml
guard:
  mode: custom
  base: relaxed                # default | relaxed | strict (default: default)
  categories:
    db-destructive: ask
    external-writes: rule
```

`tq mode toggle` on a preset turns it into `custom` with that preset as `base`. `categories` is only valid with `mode: custom`. A custom table with any value looser than default's needs a grant for that table (§6.3). Locked categories in `categories` are a load error.

## 5. Categories

### 5.1 Model

- `guardcfg.Category` is a string type with the thirteen values of §4. `Locked(c)` is true for `identity`, `secrets`, `self-protection` and `privacy`.
- `guardcfg.Categories map[string]Category` replaces `UnattendedGroups`. It classifies every built-in id. It lives in `internal/guardcfg/categories.go`.
- Custom rules (manifest `guard.rules`, `guard.yaml` `rules`, `.claude/tq-rules.yaml`) declare `category:`. A rule without one is applied at its own action in every mode (fail closed). The branch's `unattended: enforce` is read as "no category" (always applied) and is deprecated.
- Pack guards declare `Guard.Category` (§14).
- The identity guard checks in `internal/guard` (not policy rules) get ids too, so the table, the audit and the docs can name them. They are all `identity`.

### 5.2 Rule table

Built-in actions are today's. "Branch group" is the classification in `feat/unattended-skip-denies`.

| Rule id | Action | Branch group | Category |
|---|---|---|---|
| guard: `blocked-command` (manifest `blocked_commands`) | deny | identity guard | identity |
| guard: `wrong-cloud`, `neutral-remote`, `untrusted`, `env-drift`, `claude-config-drift`, `git-email-drift`, `gh-user` | deny | identity guard | identity |
| plugin fallback `identity-guard.py` (no tq binary) | deny | n/a | identity |
| tq/cross-client-mcp | deny (hook) | identity | identity |
| tq/secret-store-read | deny | secret | secrets |
| tq/secret-store-shell | deny | secret | secrets |
| tq/env-file-read | deny | secret | secrets |
| tq/env-file-shell | deny | secret | secrets |
| tq/env-dump | deny | secret | secrets |
| tq/commit-secret | deny (hook) | secret | secrets |
| jev/service-role-client | ask (Jev) | secret | secrets |
| jev/n8n-inline-credentials | ask (Jev) | secret | secrets |
| tq/guard-change | ask | self-protection | self-protection |
| tq/guard-file-write | deny | self-protection | self-protection |
| tq/settings-widen | deny | self-protection | self-protection |
| tq/settings-shell-write | deny | self-protection | self-protection |
| tq/trust-store-write | deny | self-protection | self-protection |
| tq/guard-error | ask | self-protection | self-protection |
| tq/nested-agent-loosened (new, §9) | deny | n/a | self-protection |
| tq/mode-change (new, §10.1) | deny | n/a | self-protection |
| plugin `headless-manifest-guard.py` | deny | n/a | self-protection |
| tq/force-push-main | deny | skippable | git-history |
| tq/force-push | ask | skippable | git-history |
| tq/gh-admin-merge | ask | skippable | git-history |
| tq/gh-repo-delete | deny | skippable | git-history |
| tq/iac-apply | ask | skippable (default keep_asking) | prod-changes |
| tq/remote-migration | ask | skippable (default keep_asking) | prod-changes |
| tq/cloud-delete | ask | skippable | cloud-destructive |
| tq/iac-destroy | ask | skippable | cloud-destructive |
| tq/sql-destructive-shell | ask | skippable | db-destructive |
| jev/destructive-sql | ask (Jev) | skippable | db-destructive |
| tq/migration-edit | ask | skippable | db-destructive |
| tq/mcp-sql-write | ask | skippable | db-destructive |
| tq/mcp-apply-migration | ask | skippable | db-destructive |
| jev/rls-weakened | ask (Jev) | skippable | db-destructive |
| tq/registry-write | ask | skippable | system |
| tq/outside-path-write (new) | ask | n/a | system |
| tq/system-dir-write (new) | deny | n/a | system |
| tq/elevation (new) | ask | n/a | system |
| tq/mcp-github-write | ask | skippable | external-writes |
| tq/mcp-n8n-write | ask | skippable | external-writes |
| tq/mcp-hosting-exec | ask | skippable | external-writes |
| tq/tracker-cli-write | ask | skippable | external-writes |
| tq/gha-unpinned | ask | skippable | ci-supply-chain |
| tq/gha-branch-ref | deny | skippable | ci-supply-chain |
| tq/gha-pr-target | ask | skippable | ci-supply-chain |
| tq/commit-pii | deny (hook) | skippable | privacy (locked) |
| tq/push-pii | deny (hook) | skippable | privacy (locked) |
| tq/text-pii | deny (hook) | skippable | privacy (locked) |
| tq/pii-fixture-dump | deny (hook) | skippable | privacy (locked) |
| tq/commit-message | ask (hook) | skippable | hygiene |

Rules from unmerged branches, classified now so the merge does not break the test:

| Rule id | Branch | Category |
|---|---|---|
| tq/hub-store-write, tq/pack-config-change, tq/human-answer | feat/packs-foundation (`builtin_framework.go`, today `NeverDisable`) | self-protection |
| tq/dbt-prod-target, tq/model-promote | packs data / ml | prod-changes |
| tq/pipeline-run, tq/iac-apply-env, tq/db-migrate-env, tq/restore-target | packs data / cloud / db | prod-changes |
| tq/dbt-full-refresh | packs data | db-destructive |
| tq/iac-identity-drift | packs cloud | identity |
| tq/warehouse-resize, tq/bq-unbounded-query, tq/warehouse-query-unbounded | packs data (cost guards) | packs |

The table follows the human's list, with `privacy` locked (decided at spec review). `packs` is only a fallback (decided at spec review): it is the category for a pack guard that fits none of the others (today: the cost guards). A pack guard that is a prod change, a destructive DB operation and so on declares that category instead, so `strict` and `relaxed` treat it like the matching built-in rule.

### 5.3 New `system` rules

All three live in `internal/policy/builtin_system.go`. Like every shell rule, they are regex heuristics.

- **`tq/outside-path-write`** (ask). `rm -r`/`rm -rf`/`Remove-Item -Recurse`/`rmdir /s`, or a redirect, `tee`, `cp`/`mv` destination, `Set-Content`/`Out-File`, `sed -i` on an **absolute** path that lies outside the workspace root, the session cwd, and the folders in `registry.Config` (the user's repos), and outside the temp dirs (`$TMPDIR`, `%TEMP%`, the Claude scratchpad). File tools (Edit, Write) with an absolute `file_path` outside those roots match too. Paths are resolved by the hook (it knows the roots), so this is a hook rule in `claudehook_system.go`, not a pure regex.
- **`tq/system-dir-write`** (deny). Any write or delete as above under `C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`, `C:\ProgramData`, `/etc`, `/usr`, `/bin`, `/sbin`, `/boot`, `/System`, `/Library`. Reads are fine.
- **`tq/elevation`** (ask). `sudo`, `doas`, `su -c`, `runas`, `gsudo`, `Start-Process … -Verb RunAs`, `pkexec`.

## 6. Precedence and trust

### 6.1 Layers

From least to most specific. A later layer replaces the mode of an earlier one, subject to the ceiling (§6.3):

1. **Built-in:** `default`.
2. **Global** `~/.tentaqles/guard.yaml`: `mode`, `base`, `categories`, `rules_override`. Applies to every workspace and to neutral folders.
3. **Workspace manifest** `.tentaqles.yaml` `guard:` the same keys.
4. **Project** (inside the manifest, so hash-pinned): `guard.projects.<dir>: {mode, base, categories, rules_override}`. `<dir>` is relative to the root, validated like today's `guard.unattended.projects`. The deepest matching project wins. For a file tool the target's folder must be covered too, as in the branch's `unattendedGateFor`: an edit outside the project falls back to the workspace layer.
5. **Repo file** `.claude/tq-rules.yaml` (untrusted): may set `mode`, `categories` and `rules_override`. They **only tighten**. For every rule, the repo value is applied only when it is stricter than the result of layers 1-4. Looser values are ignored, and SessionStart names them.
6. **Per-launch override:** `TQ_MODE` in the agent process's env. It is set by `claude --tq-mode <m>` (the activate function, §8), `tq run <ws> --tq-mode <m> -- claude`, or `claude-off`. It replaces the mode from layers 1-5 and is then clamped by the ceiling like every layer. `TQ_MODE=off` is handled before any of this (§8.4).

After the layers:

7. **`rules_override`** from layers 2-4 (and 5, tighten-only) is applied per rule id.
8. **Ceiling clamp:** each rule's action is raised to at least the ceiling's action for it.
9. **Locked groups** get their own action (`guard.actions` may only make them stricter; `guard.disable` of a locked id is ignored, generalising the packs branch's `NeverDisable`, with a `tq doctor` warning `guard-locked-override`).

`guard.disable` and `guard.actions` keep working. They change the rule's own action, which is what `rule` means. Ordering: built-in action → global `actions`/`disable` → workspace `actions`/`disable`/`enable` → category value → `rules_override` → clamp → locked.

The hook reads `TQ_MODE` from its own process env (inherited from the Claude process), never from the tool call's command line. `TQ_MODE=relaxed git push --force` in a Bash call changes nothing for the hook (and is denied by §9).

### 6.2 Effective action

```go
// internal/guardcfg/modes.go
type Value string // skip | ask | deny | rule
type Table map[Category]Value
type Resolved struct {
    Mode      string            // default|relaxed|strict|custom
    Base      string            // for custom
    Table     Table             // after layers 1-6
    Overrides map[string]Value  // rules_override, merged
    Ceiling   *Grant            // nil: default ceiling
    Sources   map[string]string // category/id -> "global"|ws|"project:<dir>"|"repo"|"launch"
    Clamped   []string          // categories/ids loosened past the ceiling and clamped
    Ignored   []string          // repo-file values that tried to loosen
}
// Effective returns the action for r (Allow = skip).
func (r Resolved) Effective(rule *policy.Rule) policy.Action
func Resolve(m *manifest.Manifest, ws, root, cwd, target string, g File, repo RepoFile, launch string, grant *Grant) Resolved
```

`Effective(rule)`:
- the category's value (`rule` → the rule's own action, `skip` → Allow),
- then `Overrides[rule.ID]`,
- then `stricter(that, ceilingAction(rule))`,
- then, for a locked category, the rule's own action (or stricter).

### 6.3 Grants (generalised bypass trust)

- **Command:** `tq allow --mode <ws> <mode>` (human-only, §10.1), with `<mode>` in `relaxed | custom`. `tq allow --mode <ws> default` removes the grant.
- **Store:** `$TQ_HOME/trust/<manifest-hash>.mode`, JSON `{ "mode": "relaxed", "table": {...}, "overrides": {...}, "granted_at": "..." }`. For `relaxed`, the table is the relaxed preset. For `custom`, it is the workspace's currently resolved custom table, including project layers: the human grants exactly what they see in `tq mode`.
- **Ceiling:** the grant is the loosest the workspace may go. With no grant, the ceiling is `default`. At hook time each rule's action is clamped to be no looser than the ceiling's. A manifest that asks for relaxed with no grant runs at default. A custom grant that skips `db-destructive` does not allow a later edit that also skips `system`.
- **Hash-keyed, like bypass trust today.** A hand edit of the manifest plus a reflex `tq allow <ws>` drops the grant: `tq allow` prints `mode grant dropped (was relaxed): run tq allow --mode <ws> relaxed`. tq's own manifest writers (`tq mode set`, `tq guard … --ws`, `tq unattended`) carry the grant forward to the new hash only when the new resolved table is no looser than the grant.
- **Global grant:** a `guard.yaml` mode looser than default needs `tq allow --mode --global <mode>`, stored as `$TQ_HOME/trust/global.mode` with a SHA-256 of `guard.yaml`'s `mode`/`base`/`categories`/`rules_override` keys. It is the ceiling for neutral folders. In a workspace, the workspace's own grant is the ceiling. A global relaxed therefore only loosens workspaces that have a grant too.
- **`tq allow --bypass <ws>`** becomes a deprecated alias of `tq allow --mode <ws> relaxed`. Existing `<hash>.bypass` files are read as a relaxed grant (§11).
- **`tq deny <ws>`** removes the `.mode` file too.
- **The trust store** (`$TQ_HOME/trust/**`) is already covered by `tq/trust-store-write` (self-protection, locked).

### 6.4 Headless hub runs

The runner (`internal/hub/adapter`) launches with `TQ_MODE` set to the workspace's `guard.headless_mode` (`default` when absent; `strict`, `relaxed` or `custom` allowed). A value looser than default still needs the workspace grant: the clamp applies as usual. `relaxed` for a hub run therefore needs both `guard.headless_mode: relaxed` in the trusted manifest and the grant. Hub runs are not agent tool calls, so §9 does not apply to the runner's own launch. The runner keeps passing Claude's permission flags through `claudeArgs`.

## 7. Configuration files

**Manifest** (validated in `internal/manifest/guardmode.go`):

```yaml
guard:
  mode: relaxed                 # default | relaxed | strict | custom ("off" is an error)
  base: default                 # only with mode: custom
  categories: {}                # only with mode: custom; locked categories are an error
  rules_override:               # id -> skip|ask|deny|rule; locked ids are an error
    tq/iac-apply: ask
  projects:
    demo-todo: { mode: relaxed }
  headless_mode: default
  codex:
    sandbox: workspace-write    # read-only|workspace-write|danger-full-access (was guard.unattended.codex_sandbox)
  # kept: disable, enable, actions, rules
```

- **Global `guard.yaml`:** the same `mode`, `base`, `categories` and `rules_override` keys, next to today's `disable`, `actions` and `rules`. `tq guard … --all` and `tq mode set --global` write it.
- **Repo `.claude/tq-rules.yaml`:** gains optional `mode`, `categories` and `rules_override` (tighten-only, §6.1).
- **Custom rules:** gain `category:` (one of the thirteen; a locked one is allowed in global and workspace files, and means "always applied").

## 8. Launchers, `tq off` / `tq on`

### 8.1 Binary side

- **`tq run <ws>|. [--tq-mode <m>] -- <cmd> [args]`.** `.` means the current folder's workspace (neutral when none). With `cmd` = `claude`:
  - It strips a `--tq-mode <m>` from the claude args too, so `claude --tq-mode strict` works through the shell function.
  - It sets `TQ_MODE=<m>` when a mode is given. Otherwise it leaves `TQ_MODE` unset, and the layers decide.
  - It **always** adds `--dangerously-skip-permissions`, whatever the manifest's `claude.permission_mode` says (decided at spec review: the human's standing preference). Only a `--permission-mode` or `--dangerously-skip-permissions` the user types on this launch's own command line replaces it. Bypass no longer needs bypass trust: tq's mode is the policy layer, and the grant gates what is looser than default.
  - The identity's settings get `skipDangerousModePermissionPrompt: true` from `tq settings render`. `claude.permission_mode` stays in the manifest and stays readable. `tq settings render` still writes it as `permissions.defaultMode`, which applies to sessions started outside the launcher (IDE, desktop, the hub runner's `claudeArgs`). `tq doctor` gains `permission-mode-overridden` (info): "claude.permission_mode is acceptEdits; the claude launcher starts in bypass anyway".
  - `claudesettings` drops `Bash(gh repo delete:*)` from the baseline **deny** in relaxed and custom-with-git-history-ask workspaces and adds it to **ask**. A settings deny blocks in every mode and would otherwise override relaxed's confirm. The secret-read entries stay in deny (locked).
- **`tq run --off -- <claude|codex> [args]`.** The off launch:
  - The env is `envplan.Clean(os.Environ())` plus `TQ_MODE=off`. `CLAUDE_CONFIG_DIR` and `CODEX_HOME` are removed, so the main install is used.
  - Claude gets `--dangerously-skip-permissions --settings '{"enabledPlugins":{"tentaqles@tentaqles":false}}'` (A3).
  - Codex gets `--dangerously-bypass-approvals-and-sandbox`.
  - It prints one stderr line: `tq: off — main logins, no guard (git, cloud and MCP use your default accounts)`.
  - It refuses when an agent marker is present (`CLAUDECODE=1`, `CLAUDE_PLUGIN_ROOT`, `TENTAQLES_HEADLESS=1`, `TQ_HUB_TASK`, and *Assumption A4:* `CODEX_SANDBOX` / `CODEX_SANDBOX_NETWORK_DISABLED`, set by Codex in its children).
- **`tq run` from an agent session** (markers present) refuses a `--tq-mode` looser than the parent session's resolved mode, and passes the parent's `TQ_MODE` through.
- **`tq env --off --shell <sh>`** prints the shell code for `tq off`:
  - the unsets/restores of `envplan.Clean` (every var in `__TQ_STATE` restored to its previous value or unset, every catalog provider var removed);
  - `__TQ_STATE` unset;
  - `TQ_MODE=off` set.

  It refuses when an agent marker is present. **`tq env --on`** prints `unset TQ_MODE`. The next `tq env` re-applies the folder identity.

### 8.2 Shell functions defined by `tq activate`

`internal/shell/hooks/<sh>.tmpl` gains four things:
- an early return in `__tq_hook` when `TQ_MODE` is `off` (or `TQ_ENABLED` is `0`);
- a `tq` function that handles `off` and `on` and passes everything else to the binary;
- `claude` and `codex` functions that route through `tq run .` (or `tq run --off` when the window is off);
- `claude-off` and `codex-off`.

`{{.Bin}}` is the absolute tq path, as today, so the `tq` function never recurses. The `claude`/`codex` functions call `tq run`, and `tq run` resolves the real `claude` from PATH through `exec.Command`, which never sees shell functions, so they do not recurse either.

**PowerShell (pwsh and powershell 5.1)**

```powershell
function global:__tq_hook { if ($env:TQ_MODE -eq 'off' -or $env:TQ_ENABLED -eq '0') { return }; try { $o = (& '{{.Bin}}' env --shell pwsh | Out-String); if ($o.Trim()) { Invoke-Expression $o } } catch { } }
function global:tq {
  if ($args.Count -ge 1 -and $args[0] -eq 'off') {
    $o = (& '{{.Bin}}' env --off --shell pwsh | Out-String); if ($o.Trim()) { Invoke-Expression $o }
    Write-Host 'tq: off in this window - identity cleared, guard off, main logins in use. Run: tq on'
    return
  }
  if ($args.Count -ge 1 -and $args[0] -eq 'on') {
    Remove-Item Env:TQ_MODE -ErrorAction SilentlyContinue
    __tq_hook
    Write-Host 'tq: on - identity re-applied for this folder'
    return
  }
  & '{{.Bin}}' @args
}
function global:claude { if ($env:TQ_MODE -eq 'off') { & '{{.Bin}}' run --off -- claude @args } else { & '{{.Bin}}' run . -- claude @args } }
function global:codex  { if ($env:TQ_MODE -eq 'off') { & '{{.Bin}}' run --off -- codex @args }  else { & '{{.Bin}}' run . -- codex @args } }
function global:claude-off { & '{{.Bin}}' run --off -- claude @args }
function global:codex-off  { & '{{.Bin}}' run --off -- codex @args }
```

`$LASTEXITCODE` is preserved because the binary is called last. The legacy `TQ_ENABLED=0` branch in a migrated profile (`hooks/adopt.go`) is left alone. `tq doctor` notes that `tq off` replaces it.

**bash and zsh**

```sh
__tq_hook() { case "${TQ_MODE:-}" in off) return 0;; esac; [ "${TQ_ENABLED:-1}" = "0" ] && return 0; local __o; __o="$("{{.Bin}}" env --shell bash 2>/dev/null)"; if [ -n "$__o" ]; then eval "$__o"; fi; return 0; }
tq() {
  case "${1:-}" in
    off) local __o; __o="$("{{.Bin}}" env --off --shell bash)" && eval "$__o"; echo 'tq: off in this window - identity cleared, guard off, main logins in use. Run: tq on'; return 0;;
    on)  unset TQ_MODE; __tq_hook; echo 'tq: on - identity re-applied for this folder'; return 0;;
    *)   "{{.Bin}}" "$@";;
  esac
}
claude() { if [ "${TQ_MODE:-}" = off ]; then "{{.Bin}}" run --off -- claude "$@"; else "{{.Bin}}" run . -- claude "$@"; fi; }
codex()  { if [ "${TQ_MODE:-}" = off ]; then "{{.Bin}}" run --off -- codex "$@";  else "{{.Bin}}" run . -- codex "$@";  fi; }
claude-off() { "{{.Bin}}" run --off -- claude "$@"; }
codex-off()  { "{{.Bin}}" run --off -- codex "$@"; }
```

zsh uses the same bodies (`--shell zsh`). Its `precmd_functions` registration is unchanged.

A user `alias claude=…` defined before the block wins in interactive bash (aliases expand before functions). This machine's `~/.bashrc` has `alias claude='claude --dangerously-skip-permissions'` on line 1. `tq doctor` gains `claude-alias-shadow` (warn: "alias claude bypasses tq's launcher; remove it"). The activate block does not `unalias` on its own.

**fish**

```fish
function __tq_hook --on-variable PWD
    test "$TQ_MODE" = off; and return 0
    test "$TQ_ENABLED" = 0; and return 0
    "{{.Bin}}" env --shell fish 2>/dev/null | source
end
function tq
    switch "$argv[1]"
        case off
            "{{.Bin}}" env --off --shell fish | source
            echo 'tq: off in this window - identity cleared, guard off, main logins in use. Run: tq on'
        case on
            set -e TQ_MODE
            __tq_hook
            echo 'tq: on - identity re-applied for this folder'
        case '*'
            "{{.Bin}}" $argv
    end
end
function claude;     if test "$TQ_MODE" = off; "{{.Bin}}" run --off -- claude $argv; else; "{{.Bin}}" run . -- claude $argv; end; end
function codex;      if test "$TQ_MODE" = off; "{{.Bin}}" run --off -- codex $argv;  else; "{{.Bin}}" run . -- codex $argv;  end; end
function claude-off; "{{.Bin}}" run --off -- claude $argv; end
function codex-off;  "{{.Bin}}" run --off -- codex $argv; end
```

fish's hook fires on `PWD` changes, not every prompt. `tq on` calls it once directly.

**cmd.exe:** no functions. `cmd.tmpl` documents `tq run --off -- claude` and `tq run . -- claude`.

### 8.3 Behaviour summary

| Action | Identity env | Prompt hook | Guard in the next `claude` |
|---|---|---|---|
| `tq off` | cleared now (restored to pre-tq values) | paused (`TQ_MODE=off`) | none: `claude` routes to the off launch |
| `cd` while off | stays cleared | paused | none |
| `tq on` | re-applied on the spot for the cwd | resumed | the resolved mode |
| `claude-off` from a normal window | cleared for that process only | unaffected | none |
| `claude --tq-mode strict` | the folder's | unaffected | strict (any mode, clamped) |

### 8.4 Hooks honour `TQ_MODE=off`

These entry points exit 0 with no output as their first statement, before reading stdin beyond what the protocol needs, when `TQ_MODE` is `off` or `TQ_ENABLED` is `0`:
- `tq claude-hook <event>`, in `internal/cmd/claudehook_off.go`, called first by every subcommand;
- `tq env` (prints nothing);
- `plugin/scripts/tq_hook.sh` and `tq_run.sh`, before resolving tq or Python (so the fail-closed "no tq, no Python" exit 2 never fires in off);
- `tq statusline` (prints `tq: off`).

The PreToolUse fast exit means a `claude` session started in a normal window keeps its guard even after the human runs `tq off` in that window, because the session's env was fixed at launch. This is the intended behaviour.

## 9. The nested-agent rule

**`tq/nested-agent-loosened`** (deny, self-protection, locked) is a shell rule in `internal/policy/builtin_agents.go`. It denies a Bash or PowerShell tool call that would start an agent, or change the env for one, in off mode or with loosened settings:

- assigning `TQ_MODE` to anything but `default` or `strict` (`TQ_MODE=off cmd`, `export TQ_MODE=…`, `$env:TQ_MODE=…`, `set -x TQ_MODE …`, `env TQ_MODE=…`), or `TQ_ENABLED=0`;
- `claude-off`, `codex-off`, `tq off`, `tq run --off`, `tq env --off`, `tq run … --tq-mode off|relaxed|custom`, `claude … --tq-mode off|relaxed|custom`;
- `claude` with `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions`, `--permission-mode bypassPermissions|auto|dontAsk`, `--bare`, `--safe-mode`, `--setting-sources` without `user`, or `--settings` whose inline JSON or named file mentions `enabledPlugins` with `tentaqles` and `false`, `disableAllHooks`, or `hooks: {}`. `CLAUDE_CODE_SIMPLE=1` and `CLAUDE_CODE_SAFE_MODE=1` count too;
- clearing or reassigning `CLAUDE_CONFIG_DIR` or `CODEX_HOME` (`env -u`, `unset`, `Remove-Item Env:`, `X= claude`, `X=~/.claude`). The identity guard's `claude-config-drift` already catches some of these; this rule names the agent case;
- `codex` with `--dangerously-bypass-approvals-and-sandbox`, `--yolo`, `-a never` / `--ask-for-approval never`, `-s danger-full-access` / `--sandbox danger-full-access`, `--disable hooks`, `-c features.hooks=false`, `--ignore-rules`, `--dangerously-bypass-hook-trust`.

A plain nested `claude -p "…"` or `codex exec "…"` with none of these is allowed. It inherits the parent's identity env and `TQ_MODE`, and so the parent's guard.

**Limits** (as for every shell rule): `sh -c`, scripts written to disk and run, `Start-Process` with an encoded command, aliases and variable expansion all evade it. The second line of defence is that the binaries refuse: `tq run --off`, `tq env --off` and `tq mode` refuse agent markers. The third is that a Claude child of an agent inherits `CLAUDECODE=1`.

*Assumption A5:* Claude Code sets `CLAUDECODE=1` in the env of the commands its Bash tool runs (the hub's `hubAgent()` already relies on it). *Assumption A4* (Codex markers) applies to Codex parents.

The Agent tool (Claude subagents) runs inside the same session and the same hooks, so it needs no rule.

## 10. Controls

### 10.1 CLI: `tq mode`

| Command | What it does |
|---|---|
| `tq mode [--ws NAME] [--json]` | Shows the resolved mode for the cwd: the name (`relaxed`, `custom (relaxed + 2)`), the category table with each value's source, `rules_override`, the ceiling (grant or `default`), clamped and ignored values, and skips in 24h. Read-only, so agents may run it. |
| `tq mode set <default\|relaxed\|strict\|custom> [--ws NAME \| --project DIR \| --global] [--base P]` | Writes the layer and re-trusts the manifest, carrying a grant forward when it is not loosened (§6.3). When the result is looser than the ceiling it asks on the TTY `grant relaxed for acme? [y/N]` and writes the grant on yes. Otherwise it prints the `tq allow --mode` line. |
| `tq mode toggle <category> <skip\|ask\|deny\|rule> [scope flags]` | Sets one category. A preset becomes `custom` with that preset as base. A locked category is an error. |
| `tq mode override <rule-id> <skip\|ask\|deny\|rule\|clear> [scope flags]` | Edits `rules_override`. A locked id is an error. |
| `tq mode edit [scope flags]` | An interactive list (charmbracelet `huh`, like `tq setup`): the preset first, then one row per category with a four-way select. Locked rows are shown and disabled. It ends with a diff and a confirm. |
| `tq mode log [--since 24h]` | Replaces `tq unattended log`: the skipped and changed decisions, with an action column. |

**Human-only.** Every verb except plain `tq mode` and `tq mode log`:
- refuses an agent marker (`hubAgent()` plus the Codex markers), with `ErrHumanOnly`-style text;
- needs stdin and stdout to be a TTY (`stdinIsTerminal`).

`tq allow --mode` follows the same rules, and `tq allow`, `tq deny` and `tq guard` keep their `tq/guard-change` ask. The new rule **`tq/mode-change`** (deny, self-protection) catches `tq mode set|toggle|override|edit`, `tq allow --mode|--bypass` and `tq unattended on|off` in a tool call, so the agent gets a clear block instead of a TTY error. The `tq/guard-change` regex keeps its `unattended` arm for older binaries.

### 10.2 Desktop app: Guard tab

- **Go side:** `desktop/app_guard.go` with bound methods over `internal/setup` (new `setup/guard.go`):
  - `GuardWorkspaces() []GuardView` (per workspace: resolved mode, table with sources, ceiling, 24h counts);
  - `SetMode(ws, scope, mode, base)`, `ToggleCategory(ws, scope, cat, value)`, `SetOverride(ws, scope, id, value)`;
  - `GrantMode(ws)`, `RevokeGrant(ws)`.

  These call the same functions as the CLI. The desktop app is a human GUI, with no agent tool path into the Wails bindings, so it may grant. It re-trusts the manifest like `tq mode set`.
- **Frontend:** `desktop/frontend/src/Guard.tsx`. A workspace picker, a preset segmented control, a category grid of four-way toggles (locked rows greyed with a lock icon and a tooltip naming the group), the ceiling banner ("Looser than your grant: Grant relaxed for acme" button with a confirm dialog), and the skip counts.

### 10.3 Hub web page

`/connections`, or a new `/guard` section, shows the resolved mode, the table and the 24h counts read-only, with the line "change it with `tq mode edit` or the desktop app". No form, no POST endpoint: web forms are scriptable from an agent's `curl`, and the hub API has no human proof.

### 10.4 Statusline and SessionStart

- **Statusline** (`internal/statusline`): a segment after the client, `tq: relaxed · 4 skipped`.
  - "4 skipped" counts the asks and denies skipped or lowered for this workspace in 24h (`guardlog`, tail-bounded read as in `skippedCounts`).
  - The colour is dim for `default`, yellow for `relaxed`/`custom` looser than default, cyan for `strict`. `tq: off` in red under `TQ_MODE=off`.
  - Facts gains `Mode string`, `Skipped int`. The command must stay under its current budget: no network, no git calls beyond today's.
- **SessionStart** (`modeLine`, replacing `unattendedLine`):
  - `Guard: relaxed (workspace, granted) · 3 confirms and 1 block skipped in 24h · locked: identity, secrets, self-protection, privacy`.
  - Variants: `Guard: default (relaxed requested, not granted: tq allow --mode acme relaxed)`, `Guard: strict (launch override)`, `Guard: custom (relaxed + db-destructive ask) · repo file tried to loosen: system`.
  - It also names Claude's permission mode when it is not bypass: `· Claude mode: auto (adds classifier prompts tq cannot configure)`.

## 11. Migration from unattended, and reuse of `feat/unattended-skip-denies`

### 11.1 Behaviour and data migration

- **Manifest `guard.unattended` blocks** are read as:
  - `workspace: true` → `guard.mode: relaxed`;
  - `projects: [p…]` → `guard.projects.<p>.mode: relaxed`;
  - `keep_asking: [ids]` → `rules_override: {id: rule}`. An absent `keep_asking` means the old default `[tq/iac-apply, tq/remote-migration]`, so those two become `rule` (ask), preserving today's behaviour. `keep_asking: []` means nothing.
  - `codex_sandbox` → `guard.codex.sandbox`.

  Reading is in memory. The file is untouched until a `tq mode` write, which rewrites the block in the new form (and re-trusts, carrying the grant).
- **Mixed blocks:** a manifest with both `guard.unattended` and `guard.mode` is a load error.
- **Grants:** `<hash>.bypass` is read as a relaxed grant. The next tq manifest writer converts it to `.mode`.
- **Deprecated aliases:**
  - `tq unattended on [--project D]` = `tq mode set relaxed [--project D]`;
  - `tq unattended off` = `tq mode set default` (for the scope);
  - `tq unattended status` = `tq mode`;
  - `tq unattended log` = `tq mode log`.

  Each prints `deprecated: use tq mode …` to stderr.
- **Logs:** `$TQ_HOME/guard/unattended.jsonl` is renamed by reading. New records go to `$TQ_HOME/guard/mode.jsonl`, and `mode log` and the counts read both until the old file ages out of the window.
- **Behaviour changes to call out in the CHANGELOG:**
  - unattended applied only in bypass/auto sessions, and relaxed applies in every Claude mode;
  - unattended (on main) skipped only asks, and relaxed skips denies too, except `git-history` and `system`, which ask;
  - `privacy` is locked. The PII rules (`tq/commit-pii`, `tq/push-pii`, `tq/text-pii`, `tq/pii-fixture-dump`) were skippable on the branch and are enforced in every mode now. A `keep_asking` entry naming one of them is dropped on migration (it is redundant). A `guard.disable` or a looser `guard.actions` entry for one of them is ignored, with a `guard-locked-override` doctor warning;
  - the `claude` launcher starts in bypass even when `claude.permission_mode` says otherwise (`permission-mode-overridden` doctor note).
- **Doctor checks:**
  - `unattended-not-effective` → `mode-not-granted`;
  - `unattended-unknown-rule` → `mode-unknown-rule` (for `rules_override` ids);
  - `unattended-cloud` → `mode-cloud` (relaxed or custom-skipping-prod-or-cloud in a workspace with a cloud identity);
  - new `guard-locked-override`, `claude-alias-shadow`, `codex-hooks-untrusted` (§12).

### 11.2 Code: what stays, what is renamed

| Branch code | Fate |
|---|---|
| `policy.Action.StricterThan` | Stays. |
| `policy.Set.EvaluateExcept(call, skip)` | Generalised to `EvaluateMapped(call, func(*Rule) Action)`. The mapper returns the effective action (Allow = skipped). Ranking happens after mapping, so a lowered deny no longer hides a weaker rule, which keeps the branch's property. It also returns the rules whose action changed, for the audit. `Evaluate` = `EvaluateMapped(call, nil)`. |
| `policy.SplitDecision` | Becomes `MapDecision(d, fn)` for one-source hook decisions (commit scan, PII, commit message, cross-client MCP, Jev). |
| `policy.Rule.Unattended`, `UnattendedEnforce` | Replaced by `Rule.Category`. `unattended: enforce` is read as no category (always applied). The manifest validation moves to `category`. |
| `guardcfg.UnattendedGroup`, `UnattendedGroups`, `GroupSkippable` | Renamed to `Category`, `Categories`, split into the thirteen categories (§5.2). The PII ids move from `GroupSkippable` to the locked `privacy`. |
| `guardcfg.GuardErrorID` | Stays. |
| `guardcfg.AlwaysEnforced` | Becomes `LockedIDs()`. |
| `guardcfg.Enforced` | Replaced by `Resolved.Effective`. |
| `guardcfg.Covers`, `normDir`, `within` | Stay. They are used for `guard.projects` coverage and for the file-tool target check. |
| `guardcfg.Unattended` (with the bypass/auto check) and `unattendedModes` | Removed (decision 6). |
| `guardcfg.UnknownKeepAsking` | Becomes `UnknownOverrides`. |
| `TestUnattendedGroupsClassifyEveryBuiltin` | Becomes `TestEveryRuleHasCategory`, extended to identity-guard ids and registered pack guards. |
| `cmd.unattendedGate` (`unattendedGateFor`, `skipFunc`, `note`, `filter`, `audit`) | Renamed `modeGate` in `claudehook_mode.go`: `modeGateFor(ws, cwd, call, launch)` returns the gate for every call (nil only when everything resolves to default with no overrides, the fast path). `skipFunc` → `mapFunc`, `filter` → `apply`, and `audit` also records lowered (not only skipped) decisions. |
| `skippedReason`, `skippedCounts`, `plural` | Stay. `skippedReason` renders `SKIPPED`/`LOWERED` lines. |
| `unattendedLine` | Replaced by `modeLine` (§10.4). |
| `guardlog.Record.Action`, `IsBlock` | Stay. New fields `Mode` (the tq mode; today's `Mode` field held Claude's permission mode, renamed `ClaudeMode` with a JSON alias read for old records) and `To` (the action applied: `allow` or a lower one). |
| `tq/trust-store-write` rules and `trust_store_test.go` | Stay as they are (self-protection). They also cover the new `.mode` grant files. |
| `internal/cmd/unattended.go` | Becomes the alias layer. Its logic moves to `mode.go`. |
| `internal/doctor/unattended.go` | Becomes `doctor/mode.go`. |
| Docs edits on the branch | Superseded by WS5. |

## 12. Codex

Verified against the Codex docs on 2026-10-29 (learn.chatgpt.com, redirected from developers.openai.com): the CLI reference, Rules and Hooks pages.

**What Codex offers**
- **Flags:**
  - `--ask-for-approval, -a` with `on-request | never`;
  - `--sandbox, -s` with `read-only | workspace-write | danger-full-access`;
  - `--dangerously-bypass-approvals-and-sandbox` (alias `--yolo`);
  - `--profile, -p`, `--config, -c key=value`, `--disable <feature>`, `--dangerously-bypass-hook-trust`;
  - `codex exec --ignore-rules`.
- **Exec-policy rules:**
  - `.rules` files under `rules/` next to each active config layer (`$CODEX_HOME/rules/*.rules`);
  - `prefix_rule(pattern=[...], decision="allow"|"prompt"|"forbidden", justification=..., match=[...], not_match=[...])`;
  - the most restrictive match wins;
  - linear `&&`/`||`/`;`/`|` chains are split, while scripts with redirection, substitution, variables or control flow are matched as one command;
  - the docs mark rules experimental and say they control "which commands Codex can run outside the sandbox".
- **Hooks:**
  - `hooks.json` or `[hooks]` in `config.toml` under `$CODEX_HOME`;
  - events include `SessionStart`, `PreToolUse`, `PermissionRequest`, `PostToolUse`, `UserPromptSubmit` and `Stop`;
  - PreToolUse covers shell (`Bash`), `apply_patch` (`apply_patch`/`Edit`/`Write`) and MCP tools (`mcp__…`);
  - PreToolUse can **deny** (`permissionDecision: "deny"`) or allow with `updatedInput`, but **`ask` is parsed and not supported: Codex reports an error and continues the call** (fails open);
  - non-managed hooks run only after the user trusts them in `/hooks`, and a changed hook is skipped until re-trusted;
  - hooks are on by default (`[features] hooks = false` turns them off).

**Mode compilation** (`internal/codexcfg`, rendered by `tq settings render`, `tq mode set` and `tq allow`, into the identity's `CODEX_HOME`):

| tq mode | Launch flags (`tq run . -- codex`) | `config.toml` managed block | `rules/tq.rules` | `hooks.json` (tq-managed entry) |
|---|---|---|---|---|
| off | `--dangerously-bypass-approvals-and-sandbox`, main `~/.codex` | not touched (main install) | not used | not used |
| default | `-a on-request -s workspace-write` | `approval_policy = "on-request"`, `sandbox_mode = "workspace-write"` | ask rules → `prompt`, deny rules → `forbidden` | PreToolUse → `tq claude-hook pre-tool-use --agent codex` |
| relaxed | `-a never -s <guard.codex.sandbox, default workspace-write>`; `--dangerously-bypass-approvals-and-sandbox` only when the sandbox is `danger-full-access` | `approval_policy = "never"`, `sandbox_mode = <sandbox>` | git-history and system → `prompt` (*A6*); locked deny rules → `forbidden` | same hook (it enforces the locked groups, privacy included) |
| strict | `-a on-request -s workspace-write` | same as default | deny categories → `forbidden` | same hook |
| custom | from the table: any `ask` → `-a on-request`, else `-a never` | same | from the table | same hook |

- **`--agent codex` on the hook.** It reads Codex's payload, which is assumed to be the same `hookSpecificOutput` shape (*Assumption A7*). It **never returns `ask`**, because Codex would continue the call. A tq `ask` becomes:
  - a `deny` with the reason `tq: this needs your confirm; Codex hooks cannot ask. Run it yourself, or use claude` for non-shell tools (MCP, `apply_patch`);
  - for shell, a deny too, unless the call matches a `prompt` prefix rule in `tq.rules`. Then the hook allows it and Codex's own rule prompts.

  So in Codex every mode is stricter than in Claude for asks that cannot be expressed as prefixes: they block instead of prompting. This was accepted at spec review. WS5 documents it in CLAUDE-SETTINGS.md ("Codex"), with the list of affected rule ids that `tq mode --agent codex` prints. SessionStart in Codex adds `asks become blocks in Codex: N rules` (best effort, A10).
- **The rule compiler.** It takes a curated table `codexcfg/prefixes.go`, mapping rule id to prefix patterns, for the rules that have a prefix form, for example:
  - `tq/gh-repo-delete` → `["gh","repo","delete"]`;
  - `tq/iac-destroy` → `["terraform","destroy"]`, `["pulumi","destroy"]`;
  - `tq/iac-apply` → `["terraform","apply"]`, `["kubectl","apply"]`;
  - `tq/gh-admin-merge` → no prefix (the flag sits anywhere), hook only.

  The file carries tq's begin/end markers and is tq-owned like `tq-unattended.config.toml` today. A user's own `rules/*.rules` files are never touched. Most-restrictive-wins means a user `allow` cannot weaken tq's `forbidden`.
- **The hooks entry** is written into `$CODEX_HOME/hooks.json`, merged with a marker and conflict handling like `codexcfg.Render`. The user must trust it once in `/hooks`. tq never passes `--dangerously-bypass-hook-trust`. `tq doctor` gains `codex-hooks-untrusted` (*Assumption A8:* Codex keeps hook trust in a readable file under `CODEX_HOME`; if not, the check can only say "trust it once in /hooks").
- **The profile file goes away.** `tq-unattended.config.toml` is removed when tq-owned: per-project modes are carried by the launch flags, which `tq run` computes from the cwd.
- **The top-level block** stays for workspace-wide modes, so a bare `codex` (outside the function) gets the workspace mode's approval and sandbox.

**What cannot be expressed in Codex**
- Asks for non-shell tools (MCP, `apply_patch`): they become denies.
- Rules that match a flag anywhere in the command, a regex, file content, a path (the `tq/*-file-*` and settings rules on `apply_patch` are handled by the hook only as denies), heredoc data or a cwd, and the commit/push scans (secret, PII) beyond what the hook does on the `Bash` call.
- `prompt` under `-a never`. *Assumption A6:* a `prompt` rule still prompts with `approval_policy = "never"`. If it is rejected instead, relaxed's git-history asks become denies in Codex.
- *Assumption A9:* `.rules` `forbidden` applies under `--dangerously-bypass-approvals-and-sandbox`. If not, off and danger-full-access relaxed have no rule layer (off has none by design).
- Exec-policy matching of shell-wrapped scripts that use redirection or variables is coarse, as in tq.
- The statusline and SessionStart line: Codex SessionStart hook output is *Assumption A10*. Best effort: a one-line context, same text as Claude.

## 13. Security

- **What off gives up.** Everything: identity isolation, secret denies, self-protection, audit. git, gh, cloud CLIs and MCP use the main logins. off is a human choice per launch or window. It is never stored in a file, so no edit or sync can make a workspace "off". An agent cannot start it (§9, plus the binary refusals). The banner names the risk on every off launch.
- **Agents trying to change the mode**, by vector:
  - editing the manifest: the hash moves, the workspace becomes untrusted, and the identity guard blocks. A human re-trust without the grant drops loosening;
  - editing `guard.yaml`: `tq/guard-file-write` denies it, and a global loosening needs `global.mode`, whose digest no longer matches;
  - editing `.claude/tq-rules.yaml`: `tq/settings-shell-write` denies the shell write, the file is tighten-only anyway, and an Edit-tool write is allowed but can only tighten;
  - the trust store: `tq/trust-store-write`;
  - Claude settings: `tq/settings-widen`, `tq/settings-shell-write`;
  - `TQ_MODE`/`TQ_ENABLED` in a command: the hook reads its own env, and §9 denies the assignment;
  - a nested launch: §9 and the binary refusals;
  - `tq mode`/`tq allow --mode`: `tq/mode-change` deny, plus marker refusal, plus the TTY check;
  - the hub page: read-only;
  - the desktop app: no agent path.
- **Locked groups in relaxed:** the identity guard runs first, before any mode logic, as on the branch. Secret, self-protection and privacy rules are always at their own action. `guard.disable` of a locked id is ignored.
- **Privacy is locked.** The PII commit/push/text/fixture denies hold in every mode tq is on, in line with the standing rule "PII allowed locally, guard the exits". Reading and querying PII locally is not a guard rule and is unaffected.
- **System asks in relaxed.** An `rm -rf` outside the workspace, a write under a system folder, an elevation and a registry write all get a confirm in relaxed. In strict and default they keep their own actions (`tq/system-dir-write` denies). Claude's critical-path `rm` prompt still applies in bypass as well.
- **Bypass by default.** Launching in bypass removes Claude's own prompts for calls tq does not cover. That is the point of the decision. The new `system` rules cover the largest gap (writes outside the workspace), and `default` keeps every rule.
- **The grant model** is hash-keyed, so it never survives a hand edit. tq's own writers carry it forward only when not loosening.
- **The hook still fails closed:** a panic still asks (`tq/guard-error`, locked). An unreadable or invalid mode configuration resolves to `default` (or stricter, if a parsable layer is stricter), never looser.
- **The audit** records the effective mode, the Claude mode, each skipped or lowered rule and the masked command (`guardlog.Append` masks with the workspace's data classes, as now).
- **Codex fails open on hook `ask`,** so tq never sends one (§12). An untrusted Codex hook is silently skipped by Codex. Doctor warns.

## 14. Integration note: packs (`feat/packs-foundation`)

- `packs.Guard` gains `Category guardcfg.Category`. `RegisterGuard` panics when it is empty, when it is unknown, or when it is a locked category and the guard's pack is not a framework pack. The framework rules in `builtin_framework.go` are policy rules and use the table.
- `TestEveryRuleHasCategory` iterates `packs.Guards()` after importing every pack (the same blank imports as `cmd/tq/adapters.go`).
- The pack guard runner (`packs` → hook) calls `modeGate.apply` on each guard's decision, like the other one-source hook decisions. `Supersedes` is unchanged. A superseding guard inherits nothing: its own category decides.
- `guardcfg.NeverDisable` is subsumed by "locked category". `tq/hub-store-write`, `tq/pack-config-change` and `tq/human-answer` are `self-protection`. `Unsupersedable(id)` becomes `Locked(Categories[id])`.
- The packs branch's `AlwaysAsk` list in `guardcfg/unattended.go` conflicts with this spec's rewrite of that file. The packs merge takes WS1's `categories.go` and drops `AlwaysAsk`.
- Suggested categories for the current pack guards are in §5.2. The pack authors confirm them in the packs PR.

## 15. Future work: OpenCode and Kimi Code (follow-up spec)

Research note, not verified:
- **OpenCode** (sst/opencode) has a `permission` config (`edit`, `bash`, `webfetch` with `allow`/`ask`/`deny`, and bash patterns) and a plugin system with `tool.execute.before` hooks that can throw to block. A tq bridge would be an OpenCode plugin that calls `tq claude-hook pre-tool-use --agent opencode`, plus compiling the mode into `opencode.json` `permission` patterns. The identity dir would be `OPENCODE_CONFIG_DIR`-style isolation (check the name).
- **Kimi Code** (Moonshot's Kimi CLI) has an approval/yolo switch and MCP support. Hook support and the config path need checking.
- The follow-up should reuse `Resolved.Effective` and the prefix table from §12.

## 16. Testing

Every test isolates `TQ_HOME`, `HOME` and `USERPROFILE` (`internal/testutil`). No test touches the network, a real `~/.claude`, `~/.codex`, a real shell profile, or a real Claude/Codex binary.

- **Classification:** `TestEveryRuleHasCategory` (§2). `TestLockedNeverChange`: for every preset, every custom table, every `rules_override`, launch override and repo file, each locked rule's effective action equals its own action or is stricter.
- **Presets:** a table test over every rule id × {default, relaxed, strict} gives the expected action. It includes:
  - `tq/force-push-main` and `tq/system-dir-write` going from deny to ask in relaxed;
  - `tq/outside-path-write`, `tq/elevation` and `tq/registry-write` asking in relaxed;
  - every PII rule still denying in relaxed;
  - `tq/force-push` going from ask to deny in strict.

  Default equals `Evaluate` on `main` for a corpus of commands (`claudehook_round*_test.go` fixtures reused).
- **Privacy migration:** a legacy `guard.unattended` block with `keep_asking: [tq/commit-pii]` loads without error and drops the entry. `guard.disable: [tq/push-pii]` is ignored, and doctor reports `guard-locked-override`. `tq mode toggle privacy skip` and `rules_override: {tq/text-pii: skip}` are errors.
- **Precedence:** global, workspace, project and launch; the repo file is tighten-only (a looser repo value is ignored and listed); the file-tool target outside the project falls back; `guard.actions` and `disable` keep working; locked disable is ignored.
- **Grants:** no grant clamps relaxed to default; a relaxed grant allows relaxed but not a custom table looser than relaxed; a hand edit plus `tq allow` drops it; `tq mode set` carries it forward when not loosening; `.bypass` files are read as relaxed; `tq deny` removes it; the global grant digest mismatch clamps.
- **The engine:** `EvaluateMapped` keeps the branch's property: a lowered or skipped stricter rule does not hide a weaker enforced one (`git push --force origin main; tq guard off` still asks via `tq/guard-change`). One-source hook decisions are mapped. Jev is mapped. The audit records skipped and lowered rules only when they changed the outcome.
- **Off:** `tq claude-hook` with `TQ_MODE=off` and with `TQ_ENABLED=0` exits 0 with empty stdout for every event, including a payload that would deny. `tq env` prints nothing. `tq env --off` emits the `Clean` ops for every shell (golden files in `internal/shell/testdata`). Both refuse with agent markers.
- **Shell functions:** golden tests for each template. For bash and zsh, a test that sources the hook in a subprocess shell, with a fake `tq` binary on PATH that records its argv, runs `tq off; claude x; tq on; claude y`, and checks the recorded calls and the env. The test skips when the shell is missing, and handles WSL `bash` on Windows by skipping, the same issue that makes `TestDotenvRunLoadsAndMasks` fail on this machine. PowerShell runs the same through `pwsh -NoProfile`, also with a skip. fish uses a skip when absent.
- **Launchers:** `claudeArgs` adds bypass, strips `--tq-mode`, respects a user `--permission-mode`. `tq run --off` builds a clean env, removes `CLAUDE_CONFIG_DIR`/`CODEX_HOME`, and passes the `--settings` JSON. The exec is replaced by a seam (`execCommand` var), so no real claude runs.
- **Nested-agent rule:** a positive and negative corpus in `builtin_agents_test.go` (like `trust_store_test.go`): every vector in §9 denied, and plain `claude -p`, `codex exec`, `echo TQ_MODE` and `git commit -m "docs: TQ_MODE=off"` (heredoc/message data) allowed.
- **System rules:** paths inside and outside the workspace, registry repos and temp; Windows and POSIX system dirs; elevation verbs.
- **CLI:** `tq mode set|toggle|override|edit` refuse markers and non-TTY (stdin seam). `tq mode --json` is stable. The `tq unattended` aliases map and print the deprecation.
- **claudesettings and the launcher:** relaxed moves `gh repo delete` from deny to ask; `skipDangerousModePermissionPrompt` is rendered; `defaultMode` still follows `claude.permission_mode`; `claudeArgs` adds bypass for every `permission_mode` value; doctor `permission-mode-overridden` fires for a non-bypass value.
- **Codex:** `codexcfg` renders the block, `rules/tq.rules` and the hooks entry per mode, with conflict handling for user-owned keys and files, and removes the old profile when tq-owned. The rules are checked by parsing with a small Starlark-subset parser in the test (no `codex execpolicy check` in CI). The hook in `--agent codex` never emits `ask`.
- **Statusline and SessionStart:** each variant string. The statusline stays under its time budget with a 5 MB log.
- **Desktop:** Go binding tests over a temp `TQ_HOME`; frontend unit tests for the toggle state (`guard.test.ts`), like `services.test.ts`.
- **Plugin:** `plugin/tests` for `tq_hook.sh` and `tq_run.sh` exit 0 under `TQ_MODE=off` without resolving tq or Python.
- **Manual checks by the human, never by an agent:**
  - A1/A2 in a real bypass session (a hook ask prompts, a deny blocks);
  - A3 (`--settings` `enabledPlugins` merge: other plugins stay enabled);
  - A6/A9 for Codex with a throwaway `CODEX_HOME`.

## 17. Workstreams

WS1 continues on `feat/unattended-skip-denies` (head `ade3680`) as `feat/guard-modes`. First it lands an **interface commit** (types and stubs only), tagged `guard-modes-iface`, and WS2-WS5 branch from that commit. Each WS branch merges back into `feat/guard-modes`.

Ownership is disjoint: only the owner edits a file in "Owns". "May touch" lists edits outside a workstream's own files that are allowed, and only of the kind stated (append-only registrations, or reading). A file nobody lists belongs to WS1.

**Interface commit (WS1, first):**
- `guardcfg.Category`, the constants, `Locked`, `Value`, `Table`, `Resolved` with a stub `Effective`;
- `trust.Grant` with stub `LoadGrant`/`SaveGrant`;
- `policy.Rule.Category`;
- `policy.RegisterBuiltin(rules ...Rule)`, with `Builtin()` concatenating registered parts;
- the stubs `cmd.hookOff() bool` (`claudehook_off.go`, body owned by WS2), `cmd.modeLine(...) string` (`claudehook_modeline.go`, body owned by WS3), and `cmd.renderCodex` moved to `codex_render.go` (owned by WS4);
- the manifest structs for `guard.mode`/`base`/`categories`/`rules_override`/`projects`/`headless_mode`/`codex` with no validation yet;
- `codexArgs` moved from `run.go` to `run_codex.go`;
- the stub `cmd.codexHookOutput` in `claudehook_codex.go`;
- a doctor check registry (`doctor.Register(check)`), so WS2-WS4 add checks in their own files.

| WS | Branch | Base | Owns (files) | May touch | Depends on |
|---|---|---|---|---|---|
| 1 Engine: categories (privacy locked), presets (relaxed: git-history and system ask), precedence and clamp, hash-keyed grants, `EvaluateMapped`/`MapDecision`, the mode gate, Jev and one-source mapping, the system rules, `tq/mode-change`, the legacy `guard.unattended` read (PII `keep_asking` entries dropped), `tq allow --mode`/`--bypass` alias, the audit, doctor `mode-*` and `guard-locked-override`, `TestEveryRuleHasCategory`, `TestLockedNeverChange` | `feat/guard-modes` | `feat/unattended-skip-denies` @ `ade3680` | `internal/guardcfg/*` (unattended.go → `modes.go`, plus `categories.go`, `presets.go`, `resolve.go`), `internal/policy/policy.go`, `internal/policy/builtin.go`, `internal/policy/builtin_system.go`, `internal/policy/*_test.go` (except `builtin_agents_test.go`), `internal/trust/*`, `internal/manifest/manifest.go`, `internal/manifest/unattended.go`, `internal/manifest/guardmode.go`, `internal/manifest/*_test.go`, `internal/guardlog/*`, `internal/cmd/claudehook.go`, `internal/cmd/claudehook_policy.go`, `internal/cmd/claudehook_jev.go`, `internal/cmd/claudehook_mode.go` (from `claudehook_unattended.go`), `internal/cmd/claudehook_system.go`, `internal/cmd/allow.go`, `internal/cmd/claudehook*_test.go` (except those of WS2/WS3/WS4 below), `internal/cmd/unattended_test.go`, `internal/doctor/doctor.go`, `internal/doctor/mode.go` (from `unattended.go`), and the interface-commit stubs until they are handed over | none | the branch |
| 2 Launchers and off: `tq run .`/`--tq-mode`/`--off`, `claudeArgs` always bypass, `tq env --off`/`--on`, the shell templates and functions, the hook fast exit under `TQ_MODE=off`/`TQ_ENABLED=0`, plugin scripts honouring off, `tq/nested-agent-loosened`, claudesettings (`skipDangerousModePermissionPrompt`, the `gh repo delete` deny → ask move, `defaultMode` still from `permission_mode`), headless `TQ_MODE` in the runner, doctor `claude-alias-shadow` and `permission-mode-overridden` | `feat/guard-modes-launch` | `feat/guard-modes` @ `guard-modes-iface` | `internal/cmd/run.go`, `internal/cmd/run_test.go`, `internal/cmd/env.go`, `internal/cmd/env_test.go`, `internal/cmd/claudehook_off.go` (+ test), `internal/shell/hooks.go`, `internal/shell/hooks/*.tmpl`, `internal/shell/emit.go`, `internal/shell/*_test.go`, `internal/shell/testdata/*`, `internal/envplan/*`, `internal/policy/builtin_agents.go` (+ `builtin_agents_test.go`), `internal/claudesettings/*`, `internal/hub/adapter/*`, `internal/doctor/launcher.go` (+ test), `plugin/scripts/tq_hook.sh`, `plugin/scripts/tq_run.sh`, `plugin/tests/test_tq_off.*` | `internal/cmd/root.go` (register flags/commands only); read-only use of `guardcfg`/`trust` APIs | interface |
| 3 Controls: `tq mode` show/set/toggle/override/edit/log, `tq unattended` alias, `modeLine` (locked list includes privacy), statusline segment, desktop Guard tab (privacy row locked), hub read-only guard view | `feat/guard-modes-controls` | `feat/guard-modes` @ `guard-modes-iface` | `internal/cmd/mode.go` (+ test), `internal/cmd/unattended.go`, `internal/cmd/claudehook_modeline.go` (+ test), `internal/statusline/*`, `internal/setup/guard.go` (+ test), `desktop/app_guard.go` (+ test), `desktop/frontend/src/Guard.tsx`, `desktop/frontend/src/guard.ts`, `desktop/frontend/src/guard.test.ts`, `internal/hub/web/guard.go` and its template (+ test) | `internal/cmd/root.go` (register command only); `internal/cmd/settings.go` (pass statusline facts only); `desktop/frontend/src/App.tsx` (add the tab entry only); `desktop/frontend/wailsjs/*` (regenerated bindings); `internal/hub/web` router file (one route line) | interface; WS1 for real values (it develops against the stub `Resolved`) |
| 4 Codex: mode → flags, `config.toml` block, `rules/tq.rules`, the `hooks.json` entry, the `--agent codex` output adapter (an ask becomes a deny or a `prompt` prefix rule; documented as accepted), tq-owned profile removal, doctor `codex-hooks-untrusted` | `feat/guard-modes-codex` | `feat/guard-modes` @ `guard-modes-iface` | `internal/codexcfg/*` (+ `prefixes.go`, `rules.go`, `hooks.go`), `internal/cmd/run_codex.go`, `internal/cmd/run_codex_test.go`, `internal/cmd/codex_render.go`, `internal/cmd/allow_codex_test.go`, `internal/cmd/claudehook_codex.go` (+ test), `internal/doctor/codex.go` (+ test) | none | interface; WS1 (`Resolved` values) |
| 5 Docs: the two-layer table, the modes and presets, the categories table with privacy locked, precedence and hash-keyed grants, launchers and `tq off`/`tq on`, the launcher always in bypass, the nested rule, controls, the Codex limits (asks become blocks), migration and behaviour changes, CHANGELOG, plugin guard skill | `feat/guard-modes-docs` | `feat/guard-modes` @ `guard-modes-iface` | `docs/CLAUDE-HOOK.md`, `docs/SETUP.md`, `docs/CLAUDE-SETTINGS.md`, `docs/MIGRATE.md`, `docs/HUB.md`, `docs/DESKTOP.md`, `plugin/CLAUDE.md`, `plugin/skills/guard/SKILL.md`, `plugin/CHANGELOG.md`, `CHANGELOG.md` | none (reads code only) | all (drafts from this spec in parallel; final pass after WS1-WS4 merge) |

Merge order: WS1 → WS2 → WS4 → WS3 → WS5, each green (`go test ./...`, the plugin suite, the desktop frontend tests). Then a single PR from `feat/guard-modes`.

## 18. Assumptions to verify (summary)

| Id | Assumption | How to verify |
|---|---|---|
| A1 | A PreToolUse hook `ask` prompts in `bypassPermissions` | Manual session with a test hook |
| A2 | A PreToolUse hook deny / exit 2 blocks in `bypassPermissions` | Same |
| A3 | `--settings '{"enabledPlugins":{…:false}}'` merges per plugin key | Manual: start with two plugins enabled, check `/plugin` |
| A4 | Codex sets `CODEX_SANDBOX*` env vars in commands it runs | Run `env` from a Codex session |
| A5 | Claude Code sets `CLAUDECODE=1` for its Bash tool commands | Already relied on by `hubAgent()`; re-check |
| A6 | A Codex `prompt` rule still prompts under `approval_policy = "never"` | Throwaway `CODEX_HOME` |
| A7 | Codex PreToolUse input and output match Claude's `hookSpecificOutput` shape closely enough for one adapter | Codex hooks docs (full page) and a fixture |
| A8 | Codex hook trust is readable from a file | Codex docs / source |
| A9 | `.rules` `forbidden` applies under `--dangerously-bypass-approvals-and-sandbox` | Throwaway `CODEX_HOME` |
| A10 | Codex SessionStart hook output is shown as context | Codex hooks docs |

## 19. Decided at spec review (2026-10-29)

| # | Question | Decision | By | Applied in |
|---|---|---|---|---|
| Q1 | Relaxed skipped the PII denies, against "guard the exits" | `privacy` is a **locked** group, next to identity, secrets and self-protection, and is always enforced while tq is on | human | §1, §2, §4 table, §4.3, §5.1, §5.2, §10.4, §11, §12, §13, §16, §17 |
| Q2 | Should system-folder writes stay blocked in relaxed? | `system` is `ask` in relaxed, the same as git-history | human | §1, §2, §4 table, §4.3, §12, §13, §16 |
| Q3 | An explicit `claude.permission_mode` and the launcher | The launcher always starts in bypass, whatever `permission_mode` says (the human's standing preference). `permission_mode` stays readable for the other tools (`defaultMode` for IDE, desktop and hub sessions). `tq doctor` notes the override (`permission-mode-overridden`) | controller | §8.1, §11.1, §16, §17 |
| Q4 | The `packs` category | Only a fallback, for pack guards that fit no other category. Every other pack guard declares a regular category | controller | §5.2, §14 |
| Q5 | Codex asks that cannot be expressed become denies (stricter than Claude) | Acceptable, and documented | controller | §12, §17 (WS4, WS5) |
| Q6 | Grant keying | Tied to the manifest hash | controller | §6.3 |
| Q7 | `jev/service-role-client` and `jev/n8n-inline-credentials` | Both stay in `secrets` (locked) | controller | §5.2 |
