# DISCOVERY.md — PRD v2 Phase 0

**Superseded in part, 2026-11-06.** The PRD was revised that day (Claude Code only, plugin
cut down, `tentaqles reach`/`edit`/`off`/`on`), and the owner's decisions recorded in
`docs/tickets/002-*` to `015-*` replace §7.1 and §9 of this document where they differ. The
PRD now lives at `docs/Tentaqles PRD v2 — Identity, Isolation, Guard.md`. Everything below is
the record as written on 2026-11-05.

Date: 2026-11-05. Baseline: `main` at e859b76 (tq v0.9.0, plugin 0.16.0).
Source PRD: `Tentaqles PRD v2 — Identity, Isolation, Guard.md` (repo root, untracked).
Written by the implementing agent for the developer to confirm before Phase 1 starts.
Nothing was deleted or changed in code for this document.

Decisions the developer has already given are marked **DECIDED**. Everything else under
"Open questions" waits for an answer.

## 1. Existing components

Non-test line counts on main. "v2 role" says which PRD v2 section the component serves,
or `v1 only` when no v2 section asks for it.

### 1.1 The `tq` CLI (Go)

| Package | Lines | What it does today | v2 role |
| --- | --- | --- | --- |
| `internal/cmd` | 5035 | every command; `root.go` registers them | all |
| `internal/cmd/claudehook*.go` | (in cmd) | `tq claude-hook pre-tool-use` / `session-start`; runs `guard.Decide` for Bash and PowerShell, then the secret rules, `tq/commit-secret`, `tq/cross-client-mcp`; `tq/guard-error` fail-safe | §6.2 layer 4, §6.3, §7 |
| `internal/guard` | 1562 | identity rules: `blocked-command`, `wrong-cloud`, `neutral-remote`, `untrusted`, `env-drift`, `claude-config-drift`, `git-email-drift`, `gh-user`; shell tokenizer | §6.3 identity category |
| `internal/policy` | 319 | five secret rules (`tq/secret-store-read|shell`, `tq/env-file-read|shell`, `tq/env-dump`) | §6.3 secrets category |
| `internal/commitscan` | 903 | `tq/commit-secret` (commit-time secret scan) | §6.3 secrets (not named in v2; see §4 below) |
| `internal/trust` | 52 | manifest trust by sha256; empty marker file under `~/.tentaqles/trust/` | §8.3 (partial) |
| `internal/manifest` | 410 | `.tentaqles.yaml` schema; `claude.permission_mode`, `claude.permissions`, `claude.bundle`, `blocked_commands`, `cloud`, `database`, `stack`, `identities` | §2 Manifest |
| `internal/resolve`, `registry`, `workspace`, `paths` | 183+115+139+73 | folder → workspace by walking up for `.tentaqles.yaml`; base folders in `~/.tentaqles/config.yaml`; scaffold | §3, §4 |
| `internal/envplan`, `shell`, `hooks` | 312+148+1242 | per-identity env vars from the provider catalog; `tq activate` / `tq env`; shell-profile blocks for bash, zsh, pwsh, fish (`cmd.tmpl` manual) | §3.1, §6.2 layer 2 |
| `internal/claudesettings`, `codexcfg` | 364+542 | `tq settings render`: Claude `settings.json` (defaultMode, allow/deny/ask, env, statusLine, skipDangerousModePermissionPrompt); Codex `config.toml` block + `hooks.json` | §6.2 layer 3, §6.4 (contains v1 mode code, see §5) |
| `internal/providers` | 549 | embedded catalog, 62 YAML files (`internal/providers/catalog/*.yaml`): cli, install, identity env, login, verify | §3.3, candidate format for §6.3 match lists |
| `internal/secret`, `secrets`, `dotenv` | 175+280+218 | OS keychain (`tentaqles` / `<ws>/<provider>`), `tq secrets audit`, `tq dotenv run` | §2 Trust store (credentials) |
| `internal/doctor`, `verify`, `detect` | 1728+245+174 | `tq doctor` checks, provider whoami, CLI detection | `tq status` (§5.1, §7.3) |
| `internal/setup`, `onboard`, `pkg/setupapi`, `termopen` | 543+336+761+114 | `tq setup --from <plan>`, `tq onboard` wizard, opens a new terminal | §4 |
| `internal/bundle` | 1769 | marketplaces, plugins, skills, MCP from `~/.tentaqles/bundles/catalog.yaml` into the identity's Claude home | §3.2 MCP list (partial) |
| `internal/migrate`, `uninstall` | 2816 | adopt an existing install, journal, restore | v1 only (upgrade path) |
| `internal/gitcfg`, `audit`, `statusline`, `worktrees` | 403+37+168+? | git includes per workspace; identity-switch audit log; status line; prune agent worktrees | §3.3 git; `audit.jsonl` is not a denial log |

### 1.2 The plugin (Python, `plugin/`)

| Area | Lines | What it does today | v2 role |
| --- | --- | --- | --- |
| `hooks/hooks.json` | — | SessionStart (bootstrap, `tq_hook.sh session-start`, preamble), PreToolUse (`tq_hook.sh pre-tool-use`, snapshot-guard), PostToolUse (knowledge-capture, async), Stop (stop-capture), SessionEnd | §6.4 (Claude hook registration) |
| `scripts/tq_hook.sh`, `tq_run.sh`, `tq_env.sh` | — | bridge to `tq claude-hook`; fallback `identity-guard.py` without tq; honours `$TQ_BIN` | §6.4 |
| `tentaqles/memory`, `tentaqles/*.py` | 3508+1939 | per-workspace `memory.db`, vault export, ingest, profile, signals, privacy redaction, manifest loader | v1 only (D-29) |
| `tentaqles/graph`, `embeddings`, `metagraph` | 7016+396+590 | knowledge graph, fastembed, cross-workspace patterns | v1 only |
| `tentaqles/dashboard`, `scripts/dashboard_server.py`, `skills/dashboard` | 817 | localhost HTTP server + single-page app | v1 only, §13 non-goal |
| `tentaqles/demo`, `snapshots` | 888+222 | demo workspaces; manifest snapshots for the rollback skill | v1 only |
| `skills/` (40) | 4134 | see §6 of this document | §5 onboarding skill; most are v1 only |
| `agents/` (5) | 201 | read-only reviewers: critic, spec-critic, security-reviewer, privacy-reviewer, llm-security-reviewer | candidates for §11 `reviewer` / `security-reviewer` workers |

### 1.3 Terminal interception today

A profile block (`tq hooks install`) evaluates `tq activate` at shell start and `tq env` on
every prompt/cd, inside the existing shell. PATH is inherited, never rebuilt. `claude`,
`codex`, `claude-off`, `codex-off` and `tq` are shell functions, so a child process does not
see them. cmd.exe has no automatic block.

## 2. Term mapping (keep the codebase name, per §2)

| PRD v2 term | Codebase name | Where |
| --- | --- | --- |
| Identity | **workspace** | `internal/resolve/resolve.go:17-20`, every command |
| Identity root | workspace root (folder holding `.tentaqles.yaml`) | `resolve.go:45-80` |
| (no v2 term) | **identity** = the per-CLI config dir `~/.tentaqles/identities/<ws>/<cli>` and the manifest's `identities` map | `internal/paths/paths.go:24-25`, `manifest.go` |
| Manifest | `.tentaqles.yaml` | `internal/manifest` |
| Provider scope | no single field: `git`, `cloud` (+ `cloud.also`), `database`, `identities`, `chat`, `email`, `data_platform` | `manifest.go` |
| Trust store (credentials) | OS keychain via `tq secret` (`internal/secret`) plus per-CLI config dirs | — |
| "trust" in v1 | manifest hash trust, `tq allow` / `tq deny` (`internal/trust`), v1 decision D-04 | — |
| Guard | `guard.Decide` + `policy` rules, run by `tq claude-hook pre-tool-use` | `internal/guard`, `internal/policy` |
| Hook (harness pre-execution) | plugin `hooks.json` PreToolUse → `tq_hook.sh` → `tq claude-hook`; Codex `hooks.json` written by `codexcfg` | — |
| `tq hooks *` (v2: guard hooks, human-only) | `tq hooks status|install|remove` = **shell-profile blocks**, not guard hooks | `internal/cmd/hooks.go` |
| Shim | none; shell functions in the profile block | `internal/shell/*.tmpl` |
| Guard state on/off | `tq run --off`, `claude-off`, `codex-off` (plugin disabled through `--settings`); `TQ_ENABLED=0` = skip shell activation only | `internal/cmd/off.go`, `docs/MIGRATE.md` |
| `tq off` | does not exist | — |
| `tq init` / `tq add` | `tq init` registers a base folder; `tq add <name>` creates a workspace from flags; `tq onboard` is the wizard; `tq setup --from <plan>` batch | `init.go`, `add.go`, `onboard.go`, `setup.go` |
| `tq edit` | does not exist (client-settings skill edits the file) | — |
| `tq trust *` | `tq allow`, `tq deny` | `allow.go`, `deny.go` |
| `tq status` | `tq doctor`, `tq list`, `tq doctor --json` | `internal/doctor` |
| `tq status --denials` | does not exist; `audit.jsonl` records identity switches only | `internal/audit` |
| Human-only command | `refuseAgent` (env markers only) on `allow`, `deny`, `run --off`, `secret set/rm/status`, `onboard` wizard | `internal/cmd/humanonly.go` |
| Harness marker variables | `TENTAQLES_HEADLESS`, `CLAUDE_PLUGIN_ROOT`, `CLAUDECODE`, `TQ_HUB_TASK`, `CODEX_SANDBOX`, `CODEX_SANDBOX_NETWORK_DISABLED` | `humanonly.go` |
| Onboarding skill | `skills/onboard` (prints logins, verifies; never runs a login) | — |
| Project | nearest: `skills/add-project` output (`CLAUDE.md`, `brief.md`, `docs/adr`, `docs/specs`, `LEARNINGS.md`) | — |
| Task spec | nearest: `docs/specs/<slug>.md` from the feature skill | — |
| Orchestrator / Worker / Stage skill / Gate hook | none | — |
| Integrity record | manifest sha256 only, unsigned | `internal/trust/trust.go:14-25` |
| Denial log | none | — |

## 3. Requirement map (§3–§12)

Grades: implemented / partial / missing / contradicted (code does the opposite or
violates §13). Evidence is `file:line` on main.

| § | Requirement | Status | Evidence / gap |
| --- | --- | --- | --- |
| 3.1 | identity terminal: PATH from scratch, banner, new session | partial | env only (`envplan/plan.go:16-40`); no PATH code; shell functions, not shims; no banner; cmd.exe unsupported |
| 3.2 | harness sessions: config home, MCP from manifest | partial | `CLAUDE_CONFIG_DIR` only when `claude` in `identities` (`catalog/claude.yaml:18-20`); MCP from bundle catalog (`bundle/mcp.go:10-88`), project `.mcp.json` still loads; opencode, kimi absent |
| 3.3 | platform connections | partial | gh, glab, az, aws, gcloud, databricks, snowflake, claude, codex, gemini, copilot isolated; bitbucket, Colab, Discord, Zoom, Meet, GitHub Projects, Trello, kimi, cursor, opencode not; AWS SSO cache shared; credential env vars never unset |
| 3.4 | stack | missing | `manifest.go:109` informational |
| 3.5 | browser profiles | missing | no BROWSER / xdg-open code |
| 4.1 | banner, `--no-banner`, `TQ_NO_BANNER` | missing | `run.go:55-56` one-line "tq: off" only |
| 4.2–4.3 | preconditions, prompt schema | partial | `onboard_wizard.go:64-163`; no "Other", no task/browser/shell prompts; re-run fails (`scaffold.go:72-74`) |
| 4.4 | provisioning order | partial | manifest last (`scaffold.go:78-111`); no CLI install, no PATH, plugin not pre-registered, invoking terminal never closed (`termopen.go:5-6`), new terminal untrusted (`onboard.go:87`) |
| 4.5 | `tq init --from` human-only | contradicted | `onboard --from`, `setup --from` run in agent sessions (`onboard.go:266-267`, `setup.go:225-236`) |
| 5.1 | plugin changes nothing about identity/guard; `tq status` | contradicted | add-client runs `tq add`/`tq allow` (`skills/add-client/SKILL.md:40,98`); client-settings rewrites tq fields (`:73-75,97-127`); kickoff runs `tq settings render` (`:95-98`); no `tq status` |
| 5.2 | onboarding skill | partial | `skills/onboard/SKILL.md:42-118`; no device-flow order, no browser profile, no MCP registration, no refusal rule |
| 5 | `/tq-project … /tq-report` | missing | no `commands/`; tests forbid the names (`tests/test_process_templates.py:37,56`, `tests/test_agents.py:59-60`) |
| 6.2 | layer 1 PATH | missing | — |
| 6.2 | layer 2 environment | partial | `exec.go:44-62` cleans catalog vars; prompt diff restores only tq vars (`plan.go:74-106`) |
| 6.2 | layer 3 harness config | partial | `claudesettings.go:105-168`; no `enabledMcpjsonServers`, `disableAllMcpServers`, hooks key |
| 6.2 | layer 4 hook | partial | identity rules only for Bash/PowerShell; `wrong-cloud` by first word for six CLIs; `env aws`, `sh -c` bypass admitted (`docs/CLAUDE-HOOK.md:170-180`); no match-list files, no hostnames |
| 6.2 | layer 5 filesystem | missing | — |
| 6.3 | order, no ask, fail closed | contradicted | ask on incomplete commit scan (`claudehook_policy.go:171-173`), ask on panic (`claudehook.go:330-335`), malformed payload allows (`claudehook.go:404-426`) |
| 6.4 | hook per harness, scripts outside agent-writable paths | partial | Claude hook from plugin cache (agent-writable); Codex hook via `codexcfg/hooks.go` in `CODEX_HOME` (agent-writable); `tq_hook.sh:68-82` honours `$TQ_BIN` |
| 7.1 | three categories always deny | partial | identity + secrets exist; self-protection category absent |
| 7.2 | shim = full bypass; tq sets no permission settings | contradicted | claude launcher adds the flag (`run.go:33-49`) but `tq settings render` writes defaultMode/allow/deny/ask/skipDangerousModePermissionPrompt; codex gets `-a on-request -s workspace-write` (`run_codex.go:15-23`); preamble prints `permission_mode=` (`claudehook.go:179-187`) |
| 7.3 | deny message, denial log, `tq status --denials` | missing | `emitDecision` logs nothing; messages `BLOCKED [<rule>]` (`policy.go:218-223`) |
| 8.1 | human-only: TTY, process tree, marker, confirm | partial | env markers only; no confirm (`allow.go:48-58`); init, add, hooks install/remove, settings render, bundle sync, migrate, uninstall, setup --yes, onboard --from not human-only |
| 8.2 | protected paths outside identity root | contradicted | manifest in root, 0644 (`scaffold.go:70-71,111`); trust = empty creatable file (`trust.go:27,36-44`); only `bundles/catalog.yaml` protected (`policy/builtin.go`); no ACLs. **DECIDED 2026-11-05: follow §8.2** (see §7) |
| 8.3 | signed integrity record, `tq hooks verify --accept` | partial | manifest hash only; mismatch → `tq run .` warns and still starts claude in bypass without identity (`run.go:170-173,207-212`); no verify command |
| 9 | `tq off`, `TQ_OFF=1`, hooks exit 0 | contradicted | `tq run --off` for claude/codex only (`run.go:139-148`); no `TQ_OFF` (`off.go:8-11`); `plugin/tests/test_tq_off.py:1-9` pins "no off switch" |
| 10 | project, PRD, tasks | missing | nearest add-project, prd (`docs/prd.md`, FR-nn, infers acceptance, commits itself `skills/prd/SKILL.md:95-98,111-112`); earlier gate/loop removed in 854d15d |
| 11 | orchestrator, workers, worktrees, decisions, confinement | missing | agents are read-only reviewers (`agents/critic.md:4-5`) |
| 12 | stage skills, gate hooks, `.tq/gates` | missing | dod rows and CI ratchet are advisory/remote; no SubagentStop hook; ship pushes (`skills/ship/SKILL.md:159,178`) |
| 13 | non-goals | contradicted | dashboard (`dashboard/server.py:13,176-208`); `graph.html`; add-project ticket fetch (`skills/add-project/SKILL.md:45-61`); permission modes in manifest and `add-client:29` |

## 4. Verified hook names and harness flags

Verified on this machine on 2026-11-05 unless marked otherwise.

| Harness | Installed | Full-bypass flag | Pre-execution hook | Config home variable | Notes |
| --- | --- | --- | --- | --- | --- |
| Claude Code | 2.1.318 | `--dangerously-skip-permissions` (`claude --help`) | `PreToolUse` with matchers `Bash`, `Read`, `Write`, `Edit`, `MultiEdit`, `NotebookEdit`, `Grep`, `Agent`, `mcp__*` (`plugin/hooks/hooks.json`), `permissionDecision: deny` or exit 2 | `CLAUDE_CONFIG_DIR` | Registered today through the plugin's `hooks.json`, not a settings `hooks` key written by tq. A subagent cannot spawn subagents; the Agent tool offers `isolation: worktree`; `claude -p` is the headless form (used by `plugin/tentaqles/memory/llm.py:6,43`). Typed `--permission-mode` replaces the launcher's flag rather than following it (`run.go:33-49`), so precedence in the harness itself is still unobserved. |
| Codex CLI | 0.154.0 | `--dangerously-bypass-approvals-and-sandbox` (`codex --help`) | `hooks.json` `PreToolUse` (`codexcfg/hooks.go:15-28`, matcher `*`); runs only after the user trusts it in `/hooks`; `--dangerously-bypass-hook-trust` exists, tq never passes it; ask is unsupported (`docs/CLAUDE-SETTINGS.md`) | `CODEX_HOME` | Web sources (third-party, April 2026) say PreToolUse may be Bash-only on some versions and must be enabled with a `codex_hooks` feature flag; `apply_patch` handling exists in tq (`docs/CLAUDE-SETTINGS.md`). Subagents and managed worktrees reported in 2026 blogs; not verified here. Codex today gets `-a on-request -s workspace-write`, never the bypass flag. |
| OpenCode | not installed | none found in web sources | plugin event `tool.execute.before` (deny via exit 2 or `permissionDecision`); `permission.ask` hook; GitHub issue #5894 reports subagent calls bypass `tool.execute.before` | none in tq | **Unverified.** Per §6.4, if no CLI flag and no hook reach subagents, layer 4 is unavailable and the shim must rely on layers 1–3. |
| Kimi Code | not installed | `--yolo` / `-y` (docs); `--auto` exists and is mutually exclusive | `[[hooks]]` TOML blocks in `config.toml`, 16 events (docs) | unknown | **Unverified.** Config path reported inconsistently (`~/.config/kimi` vs `~/.kimi-code`). |

Sources for the unverified rows: [developers.openai.com/codex/hooks](https://developers.openai.com/codex/hooks), [Codex CLI hooks guide](https://codex.danielvaughan.com/2026/04/15/codex-cli-hooks-complete-guide-events-policy-patterns/), [codex-hooks best practice](https://raw.githubusercontent.com/shanraisshan/codex-cli-best-practice/main/best-practice/codex-hooks.md), [OpenCode issue #5894](https://github.com/anomalyco/opencode/issues/5894), [OpenCode hooks tutorial](https://learnopencode.com/en/5-advanced/12c-hooks), [Kimi `kimi` command reference](https://www.kimi.com/code/docs/en/kimi-code-cli/reference/kimi-command), [Kimi agents](https://www.kimi.com/code/docs/en/kimi-code-cli/customization/agents.html), [Codex worktree parallel development](https://codex.danielvaughan.com/2026/03/26/codex-cli-worktree-parallel-development/).

Hook payload formats verified in tq: Claude sends `tool_name`, `tool_input`, `cwd`; result as `tool_response` (`plugin/CLAUDE.md`). Codex sends the same shape with `apply_patch` text instead of a file path.

Harness marker variables for §8.1 (enumerated from `humanonly.go` and hook code): `CLAUDECODE`, `CLAUDE_PLUGIN_ROOT`, `CLAUDE_CONFIG_DIR` (set by tq), `CODEX_SANDBOX`, `CODEX_SANDBOX_NETWORK_DISABLED`, `CODEX_HOME` (set by tq), `TENTAQLES_HEADLESS`, `TENTAQLES_HEADLESS_CHILD`. OpenCode and Kimi markers unknown.

## 5. v1 code now out of scope (listed, not deleted)

### 5.1 Permission-mode code (§13 non-goal, v1 D-30 already agrees)

- `internal/manifest/manifest.go:40,70,178-179` — `PermissionModes`, `Claude.PermissionMode`, validation.
- `internal/manifest` `Claude.Permissions{Allow,Deny,Ask}`.
- `internal/claudesettings/claudesettings.go:28-34,88-107,160-169,249-268` — `DefaultMode`, `ModeFor`, `skipDangerousModePermissionPrompt`, the ~45-entry allow baseline (`:55-75`).
- `internal/cmd/settings.go:33-36`, `internal/cmd/claudehook.go:179-187` (`permission_mode=` in the preamble), `internal/cmd/add.go:69` (`--permission-mode`), `internal/cmd/setup_wizard.go:30,60-64,87,196-197`.
- `internal/onboard/onboard.go:49,138-139,275`, `internal/setup/plan.go:29,144-145,177,186`, `internal/setup/apply.go:118`, `internal/workspace/scaffold.go:31,82`, `pkg/setupapi/api.go:54,166`.
- `internal/doctor/doctor.go:133-134` (`bypass-cloud` warning), `internal/doctor/launcher.go:17,195-209` (`permission-mode-overridden`).
- `internal/codexcfg/codexcfg.go:4,39,58,154`, `internal/codexcfg/plan.go:16-17,39-68` — `approval_policy` / `sandbox_mode` block and flags.
- `internal/hooks/adopt.go:51` — migrate carry-over comment.
- Docs: `docs/CLAUDE-SETTINGS.md:19-40,101-155`, `docs/SETUP.md:40-70,157`, `docs/MIGRATE.md:361-365`, `docs/prd.md:419-421,598`, `docs/threat-model.md:65`, `docs/adr/0001-*.md:32-34,60`, `CHANGELOG.md:33-34`.
- Plugin: `skills/add-client/SKILL.md:29` (`--permission-mode`), `skills/client-settings/SKILL.md:54,75,134-139` (`blocked_commands` editing stays; it is §6.3), skills citing retired ask rules (`db-migration:99-104`, `ci-pipeline:69-71`, `supply-chain:40,77-78`).
- The two remaining ask paths: `claudehook_policy.go:171-173` (`tq/commit-secret` incomplete scan) and `claudehook.go:330-335,357-377` (`tq/guard-error`).

### 5.2 GUI and review-page code (§13 non-goal)

- `plugin/tentaqles/dashboard/` (html.py, server.py, snapshot.py, sse.py), `plugin/scripts/dashboard_server.py`, `plugin/skills/dashboard/`, README references (`plugin/README.md:41,254,331-339`), pinned in `tests/test_skill_frontmatter.py`.
- `graph.html` output of build-graph (`tentaqles/graph/graphify_backend.py:82`, `graph/native/export.py:392`).

### 5.3 Outside the four responsibilities (§1), developer decides

- Session memory, vault, ingest, profile, signals, pattern cron, stop-capture block: `plugin/tentaqles/memory`, `tentaqles/*.py`, `scripts/session-end.py`, `knowledge-capture.py`, `stop-capture.py`, `session-preamble.py` memory part; skills session-wrap, wrap, compact-memory, query-memory, decision-history, file-history, cross-patterns, emit-signal, export-vault, profile-refresh.
- Knowledge graph and embeddings: `plugin/tentaqles/graph`, `embeddings`, `metagraph`; skill build-graph.
- Self-modifying skills: `plugin/tentaqles/skills.py:1-20,73,304`, `memory-bridge.py:222-223`.
- Demo: `plugin/tentaqles/demo`, skill setup-demo.
- Identity helper skills overlapping the CLI: add-client, client-settings, switch-client, workspace-status, rollback (+ `tentaqles/snapshots`, `snapshot-guard.py`).
- Dev-process skills that §10–§12 replace: ship, feature, fix, kickoff, dod, direction, prd, babysit-pr, grill-me.
- Advisory engineering skills: auth-security, db-migration, index-advisor, system-design-review, n8n-triage, threat-model, classify-data, privacy-review, llm-security-review, supply-chain, ci-pipeline.
- Task-provider reads: `skills/add-project/SKILL.md:45-61` (gh issue / WebFetch), `skills/prd/SKILL.md:4,31-33`.
- Go: `internal/migrate`, `uninstall` (upgrade path), `internal/bundle`, `statusline`, `worktrees`.
- Leftovers to delete regardless: `plugin/tentaqles/workflow/__pycache__/` (untracked bytecode of the removed gate/loop), `.claude/worktrees/{prd,session-classes,statedir-gaps}` (stale copies).

### 5.4 Defects found while reading (not PRD items)

- `plugin/scripts/snapshot-guard.py:44-49` reads `file_path` from the payload top level; Claude nests it under `tool_input`, so the hook never fires in a real session.
- `tq run .` on an untrusted manifest warns and still launches Claude in bypass with no identity (`run.go:170-173,207-212`); only `tq run <ws>` refuses.

## 6. Plugin skills inventory (40)

| Group | Skills | v2 fate |
| --- | --- | --- |
| Onboarding (§5.2) | onboard | keep, extend |
| Identity helpers overlapping the CLI | add-client, client-settings, switch-client, workspace-status, rollback, setup-demo | developer decides; §5.1 forbids the ones that run `tq add`, `tq allow`, `tq settings render` |
| Dev process (§10–§12 replace) | ship, feature, fix, kickoff, dod, direction, prd, babysit-pr, grill-me | developer decides; babysit-pr pushes on its own (§11.5) |
| Memory and graph | session-wrap, wrap, compact-memory, query-memory, decision-history, file-history, cross-patterns, emit-signal, export-vault, profile-refresh, build-graph | developer decides (§1) |
| GUI | dashboard | §13 non-goal |
| Advisory engineering | auth-security, db-migration, index-advisor, system-design-review, n8n-triage, threat-model, classify-data, privacy-review, llm-security-review, supply-chain, ci-pipeline, add-project | developer decides; add-project is the nearest `/tq-project` |

## 7. Answers to §13 questions resolvable from code

| Open question | What the code says | Status |
| --- | --- | --- |
| Shim flag vs typed flag precedence (§7.2) | Claude: `launchClaudeArgs` replaces its flag when the user types `--permission-mode`, `--dangerously-skip-permissions`, help or version (`run.go:33-49`). Codex: a typed `-a`, `-s`, `-c approval_policy=`, `--yolo` wins (`codexcfg/plan.go:39-68`). Precedence inside the harness when both flags are present is not observed. | still OPEN (needs a run per harness) |
| Signing key: keychain or plain file (§8.3) | go-keyring is already a dependency and `tq secret` stores under service `tentaqles` (`internal/secret/secret.go:19-23`). The spike in `scripts/spike-isolation.sh` notes macOS keychain is shared across identities, which is acceptable for a per-user signing key. | **DECIDED 2026-11-05: keychain, plain-file fallback** under `~/.tentaqles`, reported by `tq doctor` |
| Per-provider match lists: coverage and format (§6.3) | The catalog already has one YAML per provider with `cli.command`, `identity.env` and credential paths (`internal/providers/catalog/*.yaml`, 62 files). Extending each with `match: {executables, env, paths, hosts, mcp}` reuses the loader. Initial coverage: the 62 catalog providers. | recommend catalog extension; confirm in Phase 2 |
| `tq off --for <minutes>` (§9) | No code. `tq run --off` has no timer. | **DECIDED 2026-11-05: no.** Session-scoped only |
| `/tq-prd` reading one referenced ticket (§10.2) | Today prd and add-project fetch tickets by URL. | **DECIDED 2026-11-05: one item the developer references, read-only, through an in-scope MCP only.** No listing, no search, no write-back |
| Orchestrator topology (§11.1) | `claude -p` headless runs exist (`memory/llm.py`), `_detach.spawn_detached` exists, `tq worktrees` prunes agent worktrees, Claude's Agent tool has worktree isolation. (b) is feasible for Claude; Codex reports its own subagents and worktrees (unverified). | implement (a) first as the PRD says; (b) stays OPEN until Phase 8 |
| Gate retry limit (§12.3) | babysit-pr uses a 5-iteration cap (`skills/babysit-pr/SKILL.md:35-37`) as the only precedent. | **DECIDED 2026-11-05: 3**, default in `project.yaml`, editable per project and task type |
| Delete now vs dormant (§0 step 4) | Lists in §5 above. v0.9.0 already removed modes, hub, packs, desktop, review pages. | **DECIDED 2026-11-05**, see §7.1 below |
| Manifest location (§8.2, raised by the audit) | **DECIDED 2026-11-05: follow §8.2.** The manifest, trust record, hook scripts, shims and denial log live under `~/.tentaqles/`; folder-to-identity lookup through `~/.tentaqles/config.yaml`; the walk-up for `.tentaqles.yaml` in `resolve.go` and `plugin/tentaqles/manifest/loader.py` is replaced. | decided |
| Marker in the identity root (sub-question) | — | **DECIDED 2026-11-05: name-only marker.** A small `.tentaqles.yaml` holding only the identity name stays in the folder; the guard reads only the protected copy |

### 7.1 Decisions given on 2026-11-05 for the remaining audit items

| Item | Decision | Consequence |
| --- | --- | --- |
| Ask paths (§6.3) | Deny both. `tq/commit-secret` on an incomplete scan and `tq/guard-error` on a crash become deny; a malformed payload denies | Phase 2 gate "no ask code path remains" is reachable. A guard crash blocks every shell command until tq is fixed |
| Permission settings (§7.2, §13) | Remove and clean existing files. `claude.permission_mode` and `claude.permissions` leave the manifest schema; `tq settings render` stops writing `defaultMode`, allow/deny/ask and `skipDangerousModePermissionPrompt`; the next render removes only keys tq wrote | Every item under §5.1 of this document is retired. `docs/CLAUDE-SETTINGS.md`, `docs/SETUP.md`, ADR 0001 updated |
| Codex launcher (§7.2) | Inject `--dangerously-bypass-approvals-and-sandbox` | `codexcfg` stops writing `approval_policy` / `sandbox_mode`; `docs/CLAUDE-SETTINGS.md` and ADR 0001 updated; `run_codex.go` changes |
| Off switch (§9) | `TQ_OFF` only. Retire `TQ_ENABLED=0`, `claude-off`, `codex-off` and the `--settings` plugin-disable; rewrite `plugin/tests/test_tq_off.py` | `docs/MIGRATE.md` kill-switch section rewritten |
| Invoking terminal (§4.4 step 8) | Close it after the new one opens; printed instruction when tq cannot | `termopen` package contract changes; v1 D-07 stands |
| Memory, vault, ingest, profile, signals, knowledge graph (§1) | Keep in the plugin; amend PRD v2 §1 with a fifth responsibility: per-identity session memory and knowledge | Nothing deleted; their 11 skills stay. PRD v2 needs the amendment before Phase 6 |
| Dashboard (§13) | Delete | `plugin/tentaqles/dashboard`, `scripts/dashboard_server.py`, `skills/dashboard`, README section, `tests/test_skill_frontmatter.py` entry |
| Skills | Keep: onboard, identity helpers, dev-process, memory and graph skills. **Remove: the twelve advisory engineering skills** (auth-security, db-migration, index-advisor, system-design-review, n8n-triage, threat-model, classify-data, privacy-review, llm-security-review, supply-chain, ci-pipeline, and add-project's advisory part once `/tq-project` exists) | Identity helpers may only print `tq add`, `tq allow`, `tq settings render`, never run them (§5.1). Dev-process skills coexist with `/tq-*` until Phase 6–8 replace them |
| Push on own (§11.5) | Keep ship and babysit-pr behind the push gate: push a task branch only when every gate passed on the current HEAD and the user started the skill; never a shared branch, never a merge | Depends on Phase 7 |
| Ticket read (§10.2) | One referenced item via in-scope MCP, read-only | add-project and prd keep the behaviour within that limit |
| Self-modifying skills | Remove. Corrections go to `memory.db` only | `tentaqles/skills.py` writer and `memory-bridge.py:222-223` retired |
| Gate retries (§12.3) | 3 | `project.yaml` default |
| v1 Go code | Keep and name in PRD v2: `doctor`/`verify` become `tq status`; `bundle` feeds the §3.2 MCP list; `migrate` is the upgrade path to the moved manifest; `dotenv run`, `secrets audit`, `statusline`, `worktrees` stay as supporting commands | PRD v2 amendment lists them |
| `tq onboard --from`, `tq setup --from` | Treated as the `tq init --from` of §4.5: human-only (MUST, no question raised) | Phase 1 |

## 8. Conflicts raised under §0 step 5, now resolved

1. §7.2 vs `tq settings render` and `codexcfg`: resolved, remove and clean (above).
2. §6.3 vs the two ask paths: resolved, deny both.
3. §8.2 vs the manifest location: resolved, move with a name-only marker.
4. §9 vs `claude-off` / `codex-off` and `test_tq_off.py`: resolved, `TQ_OFF` only.
5. §7.2 Codex bypass flag: resolved, inject it.

PRD v2 itself needs two amendments before Phase 6: the fifth responsibility (memory and knowledge) in §1 and §13, and the list of supporting v1 commands kept.

## 9. Implementation plan (ordered as §14)

Each phase on its own branch; gate passes before merge; every new behaviour behind a manifest flag.

| Phase | Work | Builds on | Flag |
| --- | --- | --- | --- |
| 1 Self-protection | move manifest, trust record, denial log, hook scripts and shims under `~/.tentaqles/` with folder lookup in `config.yaml`; human-only detection (TTY + process tree + markers + confirm) on init, add, edit, off, allow/deny, hooks *, settings render, bundle, migrate, uninstall, setup, onboard; protected-path rules in the hook for Read/Write/Edit/shell; signed integrity record over hooks, shims, manifest; `tq hooks verify --accept`; `tq run .` refuses on mismatch; Windows ACLs | `trust`, `humanonly.go`, `policy`, `resolve`, `scaffold` | `protection: v2` |
| 2 Guard and scope | provider scope field in the manifest; match lists in the catalog YAML; identity rules for Read/Write/Edit/MCP tools and hostnames; remove ask paths and mode code (§5.1 list); fail closed on malformed payload; denial log + `tq status --denials`; deny messages with category and scope; filesystem layer | `guard`, `policy`, `claudehook*`, `providers` | `guard: v2` |
| 3 Harness shims | shim folder on an identity PATH built from the manifest; `claude`, `codex`, `opencode`, `kimi` shims inject bypass flag, config home, hook; cmd.exe AutoRun; drop shell-function launchers; `tq settings render` stops writing permissions | `shell`, `hooks`, `envplan`, `run.go` | `shims: v2` |
| 4 `tq off` | `tq off [<shell>|<harness>]`, `TQ_OFF=1`, hooks and gates exit 0 on it, human-only, banner; retire `claude-off`/`codex-off` and the `--settings` disable; decide `TQ_ENABLED=0` | `off.go`, `tq_hook.sh` | `off: v2` |
| 5 Onboarding | `tq init`/`tq add`/`tq edit` with banner asset, prompt schema with Other, provisioning order (CLI install/link, plugin pre-registration, new terminal then terminate the invoking one if decided), `tq init --from` human-only; onboarding skill: device flow order, browser profile, MCP registration, refusal of out-of-scope providers; `tq status` | `onboard`, `setup`, `termopen`, `skills/onboard`, `doctor` | `onboarding: v2` |
| 6 Project bootstrap | `/tq-project`, `/tq-prd`, `/tq-tasks` as plugin commands; `project.yaml`, `tasks/`, `docs/decisions/`; agent definitions copied in; relax the tests that forbid the names | `skills/add-project`, `skills/prd` as reference only | — |
| 7 Gates | seven gate hook scripts under protected paths, registrations, push gate, `.tq/gates/NNN.json`, `TQ_OFF` honoured | Phase 1 protection | — |
| 8 Orchestration | nine agent definitions, orchestrator skill, decision requests, confinement hook, report and handoff; topology (a) | reviewers in `agents/` as a base | — |
| 9 Retirement | remove what the developer chose from §5 | — | — |

Open sub-questions block only the step that depends on them; everything else in a phase proceeds.
