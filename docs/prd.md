# Tentaqles: product requirements

Status: planning-ready (section 10 is empty; every FR has acceptance checks)
Owner: the repo owner (reach@tentaqles.ai)    Updated: 2026-11-04
Baseline: tq v0.9.0, plugin 0.16.0, main 5ed3074

Tentaqles is an identity isolator with secrets protection for developers who work across
several clients or companies. One folder per client; the terminal, the agent sessions
(Claude Code, Codex) and the external tools (git and its hosts, the cloud CLIs, the Claude
and Codex config homes) carry that client's identity and nothing else. A command under the
wrong identity is blocked with the reason; a `.env` read, an environment dump, a
credential-store read and a commit that would record a secret are blocked; trusting a
manifest and storing a credential are human-only. The Claude Code plugin adds the session
memory, the identity preamble, the knowledge graph, 40 skills and five read-only reviewer
agents.

How to read it: every requirement is a `### FR-nn` in section 4 with one statement that
names its evidence (a file, command, skill or test on `main`), a **Status** line
(`shipped` with its release, or `planned`) and binary **Acceptance checks**; for shipped
work the check is the test or command that proves it. Decisions are `D-nn` in §13,
backlog items `B-nn` in §12. `plugin/scripts/prd-status.py` grades this file
(planning-ready: no `NEEDS-DECISION` item in section 10, a check on every FR);
`/tentaqles:direction` audits the repo against it. Reading order for the repo: this file,
`docs/threat-model.md`, `docs/adr/0001-guard-narrows-to-identity-and-secrets.md`, then
`docs/CLAUDE-HOOK.md` and `docs/COMMANDS.md`.

## 1. Problem and evidence

A developer who works for several companies or clients at once shares one terminal, one
set of CLIs, one AI-harness login and one browser profile across all of them. Wrong-account
pushes, cross-client secret exposure and manual re-login are routine, and an AI coding
agent makes each slip faster and at scale: it runs whatever its shell is logged in to,
reads any `.env` it is pointed at, and commits what it reads.

Evidence: the owner's vision note (`docs/IDEATION.md`) names the daily failure modes and
the need for per-client Claude Code accounts, since two clients each supply their own
subscription; `docs/threat-model.md` records that the main adversary is the agent itself,
steered by text it reads or simply mistaken, and the second is the supply chain; D-29
(2026-11-03) records that the two properties used every day are identity isolation and
secrets protection.

## 2. Users and roles

| Role | Who they are | Can | Cannot |
|---|---|---|---|
| Developer (the human) | One freelance or multi-employer developer, one machine; Windows first, then macOS and Linux | Trust manifests (`tq allow`, `tq deny`), store the issue-tracker credential (`tq secret`), log in, start a no-tq session, release | Be impersonated: those actions refuse any session that carries an agent marker |
| Agent session (Claude Code, Codex; OpenCode and Kimi on the roadmap) | A harness session started in a workspace terminal or by `tq run` | Read and edit the project; run git, gh and cloud commands under the workspace's identity; use `.env` values through `tq dotenv run` | Trust a manifest, write a secret, read `.env` or the keychain, print the environment, commit a secret, use another client's MCP server, act under another client's identity, start a no-tq session |
| Nested agent | An agent started by an agent session (`claude -p`, `codex`, an `Agent` tool call) | Everything its parent can | Loosen anything: it inherits the parent's config homes and blocked commands and the hook runs in it too |
| Client (the company) | The client's git host, cloud, issue tracker, chat and email | Receive pushes, cloud changes and messages made from its workspace | See another client's identity, secrets or memory |
| Maintainer / releaser | The repo owner shipping tq and the plugin | Merge with green checks, tag, pin the marketplace | Move a release tag; merge red |

## 3. Goals

- G1 **Isolation.** From any client folder, every git, cloud and harness action runs under
  that client's identity, in the terminal and inside every agent session; a command that
  would use another client's credential is blocked with the reason. Measure: every drift
  case in `internal/guard/guard_test.go` is blocked; `tq doctor` reports no drift.
- G2 **Secrets never leave.** A `.env` value, a keychain entry or the environment never
  reaches a transcript, a commit or a tool's output; `tq dotenv run` is the only way to
  use a `.env`. Measure: `TestPreToolUse_SecretRules`, `TestBuiltinIsTheFiveSecretRules`
  and `TestDotenvRunLoadsAndMasks` pass; `tq secrets audit` is clean.
- G3 **Memory per workspace.** One memory store per workspace, never mixed; a global store
  with names, stats and signals only. Measure: `plugin/tests/test_search_memory.py` passes
  and no row of one workspace's `memory.db` names a path under another workspace.
- G4 **Safe distribution.** Every install is checked against the release's checksums and
  build provenance; the plugin installs from a tag pinned to a commit. Measure: the
  installers refuse a tampered archive; `gh attestation verify` passes for every asset;
  `TestRootMarketplacePin` passes.

Non-goals: multi-user or team sharing of identities (one developer, one machine); hosting
any state on a remote service; replacing the harness's own permission system (tq sets no
permission mode beyond the launcher's skip, D-30, and the hook allows or blocks, never
asks, D-31: a force push, a cloud delete or an infrastructure apply is the harness's own
permission settings' business); defending against a program already running as the
developer's OS user (AR1) or a determined adversary rewording shell commands past the text
filters (AR2); automatic secret rotation.

## 4. Functional requirements

Areas A to D are v0.9.0; area E is the roadmap (v0.10).

### Area A: identity isolation

### FR-01 Register client folders and create a workspace with a trusted manifest
G1. `tq init <base>` registers a base folder (`~/.tentaqles/config.yaml`); `tq add <name>`
scaffolds the workspace, `.tentaqles.yaml`, the identity folders and the git identity
(`internal/workspace/scaffold.go`); `tq allow <name>` pins the manifest by SHA-256 under
`$TQ_HOME/trust` (`internal/trust`); the loader rejects any value that looks like a secret.
Status: shipped (v0.9.0)
**Acceptance checks**
- `TestHashChangesWhenFileChanges` and `TestEditedManifestLosesTrust` pass: after an edit,
  `tq list` prints `TRUSTED false` until a human runs `tq allow` again.
- `TestAddDoesNotTrustFromAgent` passes: `tq add` from a session is refused before
  anything is written (PRD v2 §8.1, ADR-0003); a human confirms and gets it trusted.

### FR-02 The identity follows the folder in every supported shell
G1. `tq activate <shell>` prints the hook (bash, zsh, fish, PowerShell, pwsh, cmd in
`internal/shell/hooks/`); it runs `tq env`, which applies `TQ_WS`, `TQ_WS_ROOT`, the git
identity and one config-dir variable per identity (`internal/envplan/plan.go`), undone on
leaving. `tq hooks install|remove|status` manages the profiles; `tq run <ws>|. -- <cmd>`
applies an identity without `cd`; `TQ_ENABLED=0` switches activation off and nothing else.
Status: shipped (v0.9.0)
**Acceptance checks**
- In a trusted workspace `git config user.email` prints the manifest's email and
  `$env:CLAUDE_CONFIG_DIR` points under `~/.tentaqles/identities/<ws>/claude`; in a
  neutral folder both are unset (COMMANDS.md walkthrough, step 3).
- `TestHook_Golden`, `TestEnviron_UndoesOtherWorkspaceShellState`,
  `TestDesired_SharedCLIVariableKeepsTheFirstIdentity` and
  `TestGitIdentity_InsideWorkspaceUsesEmail_OutsideRefused` pass.

### FR-03 One harness login and config home per workspace (Claude, Codex)
G1. Catalog entries redirect `CLAUDE_CONFIG_DIR` and `CODEX_HOME` per identity
(`internal/providers/catalog/claude.yaml`, `codex.yaml`) under
`~/.tentaqles/identities/<ws>/<id>`; `tq login <workspace> <identity>` logs in there. A
session whose `CLAUDE_CONFIG_DIR` is not the workspace's is blocked on `git push` and `gh`
(`claude-config-drift`); `tq doctor` reports `identity-wrong-account`,
`identity-wrong-subscription` and `identity-logged-out`.
Status: shipped (v0.9.0)
**Acceptance checks**
- `claude` in workspace A and in workspace B writes credentials under two different
  `CLAUDE_CONFIG_DIR` folders and leaves `~/.claude` unchanged (byte-compare).
- `TestRunForCwd_ClaudeConfigDrift` and `TestVerifyFindings_WrongAccountIsAnError` pass.

### FR-04 Per-workspace provider CLI config homes (gh, cloud)
G1. 62 catalog entries (`internal/providers/catalog/`): gh `GH_CONFIG_DIR`, glab, az and
azure-devops `AZURE_CONFIG_DIR`, aws `AWS_CONFIG_FILE` and `AWS_PROFILE`, gcloud
`CLOUDSDK_CONFIG`, databricks, snowflake, docker, kube, helm, terraform, pulumi, vercel,
supabase, stripe and others; `tq providers list|show|check|add`; `cloud.also` lets a
trusted manifest declare extra clouds. Chat, email and issue-tracker tools go through the
harness's MCP connectors with the credential in the keychain (FR-14).
Status: shipped (v0.9.0)
**Acceptance checks**
- `tq providers check` lists every declared CLI with installed yes/no and version;
  `gh auth status` in workspace A names A's account and never B's.
- `TestInlineIdentityOverride`, `TestWriteWorkspace`, `TestExtraClouds` and
  `TestAllProviderVars_CoversCatalog` pass.

### FR-05 Stack and preferences per workspace
G1. `stack:` and `language:` in the manifest (`internal/manifest/manifest.go`), read by
the plugin (`plugin/tentaqles/manifest/loader.py`), shown and changed by
`/tentaqles:client-settings`, written into the project `CLAUDE.md` by
`/tentaqles:add-project`. Runtimes and formatter configs per workspace are B-08.
Status: shipped (v0.9.0)
**Acceptance checks**
- `test_get_client_context_reads_v2` and
  `test_add_project_scaffolds_the_fixed_sections_and_points_to_the_process_skills` pass.
- `/tentaqles:client-settings` lists `stack` and `language`; a change it writes drops the
  trust until `tq allow` runs.

### FR-06 The plugin and MCP set are pre-registered per identity (bundles)
G1. A shared catalog (`$TQ_HOME/bundles/catalog.yaml`); `tq bundle catalog|diff|sync|capture`
materialize the manifest's `claude.bundle` into the identity's Claude settings
(`internal/bundle`); unsafe sources are refused; another client's MCP server is blocked by
`tq/cross-client-mcp`; the plugin installs from the root marketplace pinned to a release
tag and commit (`.claude-plugin/marketplace.json`, `docs/RELEASING.md`). Claude Code only.
Status: shipped (v0.9.0)
**Acceptance checks**
- `tq bundle diff <ws>` prints no drift right after `tq bundle sync <ws>` on a trusted
  workspace (`TestSync_EndToEnd`).
- `TestBundleSync_RefusesUntrusted`, `TestValidate_PlainTextTransport`,
  `TestValidate_GitURLTransport`, `TestPolicy_CrossClientMCPDenied` and
  `TestRootMarketplacePin` pass.

### FR-07 Onboarding verifies every provider login and the issue-tracker credential
G1. `plugin/skills/onboard/SKILL.md` reads the manifest, prints the `tq login` and
`tq secret set` lines for the human, guides `/mcp`, then verifies with `tq doctor --json`
plus read-only calls (`chat-ok`, `chat-mcp-missing`, `email-ok`, `email-mcp-missing` in
`internal/doctor/services.go`; `tq secret status <ws>` for the token); undeclared blocks
are skipped. Logins and `tq secret` stay human-only (D-08).
Status: shipped (v0.9.0)
**Acceptance checks**
- `/tentaqles:onboard` on a workspace whose issue-tracker credential is missing reports it
  as failed and names `tq secret set <ws> <provider>`; with every service reachable it
  reports each as ok.
- `test_onboard_skill_covers_every_provider_kind`,
  `test_onboard_skill_runs_no_human_only_command` and `TestChatEmail_MCPEntryPresent` pass.

### FR-08 Machine setup from a plan (`tq setup`)
G1. `tq setup --example|--from <plan> [--dry-run|--yes]` scaffolds several workspaces and
prints the login lines (`internal/setup`, `pkg/setupapi`, `docs/SETUP.md`); a plan with an
unknown key or a secret-shaped value is refused; every new workspace is left untrusted.
Status: shipped (v0.9.0)
**Acceptance checks**
- `--dry-run` prints the plan and writes nothing; `--yes` creates every workspace listed,
  all `TRUSTED false` in `tq list` (`TestApply_EndToEnd`, `TestApply_WritesServiceBlocks`).
- `TestLoadPlan_TrustDefaultsTrueWhenAbsent` and `TestToolCheck_DedupsPerCompany` pass.

### FR-09 Adopt a hand-built setup, and undo it (`tq migrate`, `tq uninstall --restore`)
G1. `tq migrate` (plan, then `--apply`) with a journal and byte-exact `.tq-backup` files;
`tq uninstall --restore [latest]` replays the journal backwards and resumes an interrupted
restore (`internal/migrate`, `docs/MIGRATE.md` with its "Known rough edges").
Status: shipped (v0.9.0)
**Acceptance checks**
- `tq migrate` without `--apply` changes nothing (diff of `$HOME` before and after is
  empty); `TestCmdApplyRefusesStalePlan` passes.
- `TestShellApplyMakesStatusInstalledAndRestoreIsByteExact`,
  `TestMigrate_IdentityAndGit_ApplyThenRestore`, `TestRecordNeverLeavesJournalAbsent` and
  `TestRestoreResumesAfterMiddleFailure` pass.

### FR-10 Every agent tool call passes the hook: identity drift and secret exits, allow or block
G1, G2. The PreToolUse hook (`plugin/hooks/hooks.json`, 15 s) runs `tq claude-hook
pre-tool-use` for Bash, PowerShell, Read, Edit, Write, MultiEdit, NotebookEdit, Grep,
Agent and `mcp__*`, in every Claude permission mode. The identity guard
(`internal/guard/guard.go`) blocks with exit 2 and the fix on `blocked-command`,
`wrong-cloud`, `neutral-remote`, `untrusted`, `env-drift`, `claude-config-drift`,
`git-email-drift` and `gh-user`; the five secret rules of `internal/policy/builtin.go`
(`tq/secret-store-read`, `tq/secret-store-shell`, `tq/env-file-read`, `tq/env-file-shell`,
`tq/env-dump`) plus `tq/commit-secret` and `tq/cross-client-mcp` deny; nothing a manifest,
a global file or a repository file adds or removes. Every rule decision is allow or block;
tq never asks, and a call it cannot judge (an unreadable payload, a guard panic, the 6 s
deadline, an unreadable registry or manifest) is denied as `tq/guard-error`, an incomplete
commit scan as `tq/commit-secret` (ADR-0006). Codex gets the
same hook through `$CODEX_HOME/hooks.json` (`internal/codexcfg`); with tq missing the hook
falls back to `plugin/scripts/identity-guard.py`, with no Python it blocks shell tools.
The rules are text filters (AR2); tq denies before the harness's hook timeout, and the
plugin turns any exit of tq other than allow or deny into a deny.
Status: shipped (v0.9.0)
**Acceptance checks**
- `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`, `TestPreToolUse_SecretRules`,
  `TestBuiltinIsTheFiveSecretRules`, `TestPolicy_CrossClientMCPDenied`,
  `TestPolicy_CommitWithSecretDenied`, `TestPreToolUseGuardPanicDenies`, `TestNoAskCodePath` and
  `TestPreToolUseIncompleteScanDeniesBothAgents` pass.
- `test_hooks_json_pretooluse_matcher_covers_shell_file_and_mcp_tools` and
  `test_tq_hook_blocks_when_no_python` pass; in the COMMANDS.md walkthrough step 5 table,
  `git push --force` exits 0 and `cat .env` exits 2.

### FR-11 Trust and `tq allow`/`tq deny` are human-only
G1. `internal/humanonly` (ADR-0003, PRD v2 §8.1) refuses with no prompt when stdin or
stdout is not a terminal, when a harness or subagent (`claude`, `codex`, `opencode`,
`kimi`, or `node` running one) is in the process tree, or when an agent environment
marker is set (`CLAUDECODE`, `CLAUDE_PLUGIN_ROOT`, `CLAUDE_CODE_ENTRYPOINT`, the Codex
sandbox and thread markers, `OPENCODE`, `KIMI_CODE`, `TENTAQLES_HEADLESS[_CHILD]`,
`TQ_HUB_TASK`); each refusal is one line in `$TQ_HOME/denials.jsonl`. A human then types
`yes` at a confirmation that names what is about to be written. Applied to every command
that writes under `$TQ_HOME` or an identity dir: `tq init`, `tq add`, `tq allow`,
`tq deny`, `tq hooks install|remove`, `tq settings render`, `tq bundle sync|capture`,
`tq migrate`, `tq uninstall`, `tq setup`, `tq onboard`, `tq secret set|rm`,
`tq providers add`, `tq login`, `tq run --off`, `tq worktrees prune`. `tq secret status`
is free (presence only). Skills print the line for the human and never run it.
Status: shipped (v0.9.0: environment marker on `tq allow`, `tq deny`, `tq secret`,
`tq run --off`, the `tq onboard` wizard); three detectors, the confirmation, the denial log
and the full command list on the Phase 1 branch (2026-11-05)
**Acceptance checks**
- `TestAllowAndDenyRefuseAgents` passes: with `CLAUDECODE=1` alone, `tq allow <ws>` and
  `tq deny <ws>` exit 1 with "human-only" and `$TQ_HOME/trust` is unchanged.
- `TestHumanOnlyCommandsRefuseWithMarkerAndLog` passes: every command listed refuses under a
  marker, and the only file the home gains is `denials.jsonl`;
  `TestHumanOnlyCommandsRefuseWithoutTTYOrUnderAHarnessProcess`, `TestTTYDetector`,
  `TestProcessTreeDetector`, `TestEnvMarkerDetector`, `TestRefusalIsLogged` cover the
  detectors; `TestAddConfirmsBeforeWriting`, `TestGateConfirms`,
  `TestSetup_YesDoesNotSkipConfirm` cover the confirmation.
- `TestSecretIsHumanOnly` and `TestOnboard_InteractiveRefusedInAgentSession` pass; in a
  Claude session, "run `tq allow tentaqles`" answers `tq: tq allow: human-only`, exit 1
  (COMMANDS.md walkthrough, step 7).

### FR-12 A session with no tq at all (`claude-off`, `codex-off`), human-only
G1. `claude-off`, `codex-off` and `tq run --off -- claude|codex` start the harness with the
identity environment and the config-home redirect cleaned (`internal/envplan/off.go`), so
the main `~/.claude` or `~/.codex` is used and the plugin is switched off through Claude's
`--settings`; no variable is set and nothing is stored, so a new terminal in the folder is
the identity terminal again; the off launchers refuse agent markers.
Status: shipped (v0.9.0)
**Acceptance checks**
- `TestRunOff`, `TestRunOffRefusesAgents` and `test_scripts_have_no_off_switch` pass.
- `claude-off` in window A starts Claude with no `CLAUDE_CONFIG_DIR` in its environment
  while a new window B in the same folder has the workspace's `CLAUDE_CONFIG_DIR`.

### FR-13 A nested agent cannot loosen identity or secrets
G1, G2. Structural: a nested session inherits the parent's environment, so its
`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `TQ_WS` and the manifest's `blocked_commands` are the
parent's and the same hook runs in it; the off launchers refuse agent markers (FR-12);
`tq run` from an agent session refuses `--off` and another workspace; a re-exported
`CLAUDE_CONFIG_DIR` hits `claude-config-drift` on the first `git push` or `gh` call (AR2).
Status: shipped (v0.9.0)
**Acceptance checks**
- `TestRunFromAgentSession` passes: with `CLAUDECODE=1`, `tq run --off` and `tq run` into
  another workspace are refused.
- `TestRunForCwd_ClaudeConfigDrift` and `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`
  pass.

### Area B: secrets

### FR-14 Credentials live per workspace, never in shared dotfiles
G1, G2. CLI logins live in per-identity config dirs (FR-04); the issue-tracker token
(github, azure-devops, jira, linear, asana) lives in the OS keychain under service
`tentaqles`, account `<ws>/<provider>` (`tq secret set|rm|status`, human-only,
`internal/secret`, go-keyring); the manifest loader rejects secret-shaped values. D-04:
per-identity config dirs plus the OS keychain with per-workspace account names; "trust
store" in tq means manifest trust. A same-user process can read the keychain (AR1).
Status: shipped (v0.9.0)
**Acceptance checks**
- `TestSecretSetStatusRm`, `TestSecretSetHiddenPromptOnATerminal` and
  `TestErrorsNeverCarryTheValue` pass; `tq secret status <ws>` reports presence only.
- `TestValidate_RejectsTokenShapedServiceValues` passes; a `gh` call in workspace A uses
  A's `GH_CONFIG_DIR`.

### FR-15 Secrets never reach the transcript, a commit or a tool's environment
G2. `.env` reads and environment dumps are deny rules (`tq/env-file-read`,
`tq/env-file-shell`, `tq/env-dump`, `tq/secret-store-read`, `tq/secret-store-shell`; a
`.env.example` is allowed); `tq dotenv run [--file F]... -- <cmd>` loads a `.env` and masks
each value in the output (`internal/dotenv`); `tq/commit-secret` scans what a commit would
record, the working tree included when the command also stages (`internal/commitscan`),
with a deadline; `tq secrets audit` prints only path and line (`internal/secrets`). No
automatic rotation. The rules are text filters: `python -c` reads are not seen (AR2).
Status: shipped (v0.9.0)
**Acceptance checks**
- `tq dotenv run --file demo.env -- bash -c 'echo setting is $DEMO_SETTING'` prints
  `setting is [REDACTED:DEMO_SETTING]` (COMMANDS.md walkthrough, step 6);
  `TestDotenvRunLoadsAndMasks` and `TestMaskerMasksSplitValuesAndKeepsShortOnes` pass.
- `TestPolicy_ReadEnvDenied_WindowsPath`, `TestPolicy_PowerShellEnvDumpDenied`,
  `TestBuiltinDecisions`, `TestAudit`, `TestPolicy_CommitWithSecretDenied`,
  `TestAdded_StagedAndChainedAdd`, `TestScanDiffLineNumbers` and
  `TestScanCtx_DeadlineIsAnError` pass.

### FR-16 Trust is by file hash; `tq doctor` reports what tq does not read
G1. A manifest edited by anyone, agent included, is untrusted until a human runs
`tq allow`, and every git, gh and cloud command in it is blocked (`untrusted`); a
repository manifest cannot add clouds. `tq doctor` warns `manifest-dropped-block` for a
top-level manifest key tq does not read and `state-dir-unused` for a folder under
`$TQ_HOME` tq does not use; tq deletes nothing. `audit.jsonl` records identity switches
(`internal/audit`), with no hash chain (AR1).
Status: shipped (v0.9.0)
**Acceptance checks**
- `TestEditedManifestLosesTrust` and `TestPreToolUse_UntrustedManifestCannotAddClouds`
  pass; an edit to a trusted manifest makes `tq doctor` print `untrusted` with the fix.
- `TestManifestDroppedBlock_ListsTheBlocks`, `TestStateDirUnused_ListsOnlyWhatExists` and
  `TestRun_OldManifestWarnsAndExitsZero` pass.

### Area C: memory and knowledge

### FR-17 Per-workspace memory, search, decay, vault export, knowledge graph, signals
G3. `plugin/tentaqles/memory/`: per-workspace SQLite (`memory.db`) with FTS5 BM25 search
and a recency boost, decay and compaction, Obsidian vault export, a knowledge graph
(graphify or native), cross-workspace patterns and signals through a global `meta.db` that
holds only display names, stats and signals, snapshots and rollback; hooks SessionStart,
PreToolUse, PostToolUse (async), Stop and SessionEnd with short timeouts and slow work in
detached workers; every write passes `plugin/tentaqles/privacy.py`. The identity preamble
prints at session start (`tq claude-hook session-start`, `session-preamble.py`). Twelve
memory skills, `query-memory` to `wrap`.
Status: shipped (v0.9.0)
**Acceptance checks**
- `plugin/tests/test_search_memory.py`, `test_embeddings_optional.py` (with `fastembed`
  absent), `test_signals.py`, `test_export_writes_one_folder_per_client_with_the_four_notes`,
  `test_context_describes_the_workspace_without_secrets` and
  `test_default_output_contains_client_header` pass.
- No row in workspace A's `memory.db` names a path under workspace B's root after a session
  in each.

### FR-18 A PRD per project through `/tentaqles:prd` with the planning-ready grader
G3. `/tentaqles:prd` writes `docs/prd.md` with 11 fixed sections, `### FR-nn` requirements
with binary checks, open items as `NEEDS-DECISION` in §10 and releases in §11
(`plugin/templates/process/prd.md`); `plugin/scripts/prd-status.py` grades it
(`plugin/tentaqles/prd.py`); `kickoff` refuses a PRD that is not planning-ready;
`/tentaqles:direction` audits the repo against it; `/tentaqles:grill-me` interviews; the
session preamble names the PRD's reading path when one exists.
Status: shipped (v0.9.0)
**Acceptance checks**
- `prd-status.py --cwd <project>` exits 0 on a PRD with no open decision and a check on
  every FR, and 3 otherwise (`test_ready_prd`, `test_open_decision_blocks`,
  `test_fr_without_checks_blocks`, `test_cli_exit_codes`).
- `test_kickoff_requires_a_planning_ready_prd_and_scaffolds_everything` and
  `test_prd_reading_path_is_injected_when_prd_exists` pass.

### FR-19 Project scaffold with CLAUDE.md fixed sections, checklist skills and read-only reviewers
G3. `/tentaqles:add-project` creates `<client_root>/<slug>/` with `CLAUDE.md` (from
`plugin/templates/process/CLAUDE-project.md` plus a Client Context section), `docs/adr`,
`docs/specs`, `docs/LEARNINGS.md` and `git init`; `kickoff` adds the stack ADR, the threat
model, the privacy record and CI. The dev-process skills (`prd`, `kickoff`, `feature`,
`fix`, `grill-me`, `dod`, `ship`, `babysit-pr`, `direction`) are plain checklists: they
record nothing outside the repository and every stop is a question to the user. Five
read-only reviewer agents (`critic`, `spec-critic`, `security-reviewer`,
`privacy-reviewer`, `llm-security-reviewer`; tools Read, Glob, Grep) read the diff a skill
names and return the review contract. 40 skills, pinned by `test_skill_frontmatter.py`.
Status: shipped (v0.9.0)
**Acceptance checks**
- `test_add_project_scaffolds_the_fixed_sections_and_points_to_the_process_skills`,
  `plugin/tests/test_skill_frontmatter.py` and `test_process_skills.py` pass.
- `test_agent_set_is_exactly_the_five_reviewers` and `test_reviewers_are_read_only` pass:
  `plugin/agents/` holds exactly the five files and none has Bash.

### FR-20 Knowledge-base ingestion: markdown folders into the workspace memory
G3. `plugin/tentaqles/memory/ingest.py` indexes markdown folders into `reference_docs`
with a path-plus-mtime watermark and the note's own date; the vault folder comes from
`~/.tentaqles/knowledge.yaml` or the manifest (`plugin/tentaqles/knowledge.py`);
`query-memory` and FTS5 search read them; `export-vault` runs export and ingest on demand,
SessionEnd's worker does the same. Further connectors, distillation and hybrid retrieval
are D-21's next step and have no release yet.
Status: shipped (v0.9.0)
**Acceptance checks**
- `test_first_run_indexes_every_markdown_file`,
  `test_second_run_skips_unchanged_and_reindexes_changed` and
  `test_references_are_searchable_with_title_first` pass.
- `test_symlinks_out_of_the_folder_are_not_followed` and
  `test_missing_directory_is_reported_not_raised` pass.

### Area D: distribution and operations

### FR-21 One tag releases everything, pinned and verifiable
G4. `.github/workflows/release.yml` on a `v*` tag checks the tag is on `main` and the
plugin versions agree, goreleaser builds tq for every platform plus the release, Homebrew
tap and Scoop bucket with `checksums.txt`, and attests build provenance;
`installers/install.ps1` and `install.sh` check the checksum and, with a logged-in `gh`,
run `gh attestation verify` against the release workflow as signer; the root marketplace
pins `plugin/` to the tag and its commit (`test/integration`, CI job `marketplace-pin`);
tags `v*` cannot be moved or deleted; every action is pinned by commit sha; Dependabot
proposes weekly updates (`docs/RELEASING.md`). The installers are served from `main`, so
their own integrity rests on TLS and the account (B-09).
Status: shipped (v0.9.0)
**Acceptance checks**
- `TestRootMarketplacePin` and `TestDevMarketplaceMatchesPlugin` pass.
- The installer run against a deliberately altered archive exits non-zero and installs
  nothing; `gh attestation verify` passes for every asset of the v0.9.0 release.

### FR-22 `tq doctor` health checks
G1. `tq doctor` (read-only; every finding ends with the fix) covers trust, hooks, git
identity, identity dirs and logins, settings drift, plugins and bundles, the launcher,
migration leftovers, services and the state folder (`internal/doctor`: `untrusted`,
`env-drift`, `claude-config-drift`, `git-email-drift`, `identity-wrong-account`,
`identity-wrong-subscription`, `identity-logged-out`, `identity-dir-linked`,
`identity-dir-missing`, `settings-drift`, `bundle-drift`, `plugin-version-skew`,
`plugins-dir-linked`, `claude-alias-shadow`, `cli-shadowed`, `codex-hooks-untrusted`,
`hook-missing`, `legacy-active`, `memory-stale`, `manifest-dropped-block`,
`state-dir-unused` and the service ids of FR-07); `tq doctor --json`; `tq secrets audit`;
`tq worktrees list|prune --yes` for leftover agent worktrees (never forced).
Status: shipped (v0.9.0)
**Acceptance checks**
- On a configured machine `tq doctor` prints `[ok]` lines and no `[warn]` about a `claude`
  alias shadowing the launcher (COMMANDS.md walkthrough, step 4).
- `TestRunForCwd_EmailDrift`, `TestRun_HookMissing`, `TestClaudeAliasShadow`,
  `TestShimShadow`, `TestMemoryStale`, `TestDoctor_JSONFlagPrintsFindings` and
  `TestAllChecksPassedWithOnlyOKFindings` pass.

### FR-23 Settings render, status line and Codex render
G1. `tq settings render [--all]` writes tq's part of each identity's Claude `settings.json`
(env, status line), writes no permission setting, keeps hand edits and prints what changed
(`internal/claudesettings`, `docs/CLAUDE-SETTINGS.md`); the first render after an upgrade
removes the permission settings an older tq wrote (the entries its state file records, its
`defaultMode` and `skipDangerousModePermissionPrompt`) and leaves the user's own; `tq
statusline` (hidden) shows the client and the git email; the Codex identity gets a hooks
file merged with the user's own and nothing in its `config.toml`, where the block an older
tq wrote is removed (`internal/codexcfg`).
Status: shipped (v0.9.0)
**Acceptance checks**
- `tq settings render --all` twice in a row prints `up to date` for every identity on the
  second run (COMMANDS.md walkthrough, step 8).
- `TestApplyKeepsUserEntries`, `TestApplyLeavesCustomStatusLine`,
  `TestRenderCleansWhatTheOldRenderWrote`, `TestRenderFreshWritesNoPermissionKey`,
  `TestRenderHotBandAndNeutral`, `TestRenderHooksMergesWithUserHooks`,
  `TestRenderRemovesOldBlocks`, `TestPinnedFlags` and `TestLaunchArgs` pass.

### FR-24 Documentation matches the command list
The docs are the operator's runbook: `docs/COMMANDS.md` (every command and skill plus a
10-step walkthrough), `CLAUDE-HOOK.md`, `CLAUDE-SETTINGS.md`, `MIGRATE.md`,
`PROVIDERS.md`, `SETUP.md`, `RELEASING.md`, `threat-model.md`, the ADR, `README.md`, the
root and plugin `CLAUDE.md`.
Status: shipped (v0.9.0)
**Acceptance checks**
- Every `tq` command in section 1 of `docs/COMMANDS.md` is registered in
  `internal/cmd/root.go`, and every registered command except the hidden `statusline` is
  in that section; section 2 names every directory of `plugin/skills/` and every file of
  `plugin/agents/`, and nothing else.
- Every rule id in `internal/policy/builtin.go` and `internal/cmd/claudehook_policy.go`
  appears in the rule table of `docs/CLAUDE-HOOK.md`.

### Area E: roadmap, isolation completeness

### FR-25 An unambiguous identity indicator in the terminal and the harness
G1. Today `tq statusline` shows the client and the git email and the preamble prints at
session start; the shell hooks change neither the prompt nor the window title. D-01: a
`[client]` prompt prefix and the client name in the window title in every supported shell.
Status: planned (v0.10)
**Acceptance checks**
- A new terminal opened in a trusted workspace shows the workspace name in the prompt or
  the window title before the first command.
- A `claude-off` window shows an "off" marker in the same place.

### FR-26 CLIs not selected for a workspace are not reachable from its terminal
G1. An AWS-only client's terminal must not resolve `az` (IDEATION.md). Today `envplan`
edits no PATH and installs no shims. D-05: a shim folder per workspace holding launchers
for the selected tools only, first on PATH by the shell hook.
Status: planned (v0.10)
**Acceptance checks**
- In a workspace whose manifest declares only `aws`, `Get-Command az` (PowerShell) and
  `command -v az` (bash) fail, while they succeed in a neutral folder.
- The same commands succeed again in a `claude-off` window.

### FR-27 One browser profile per workspace, used by every URL open and browser automation
G1, IDEATION.md. A terminal, harness or agent opening a URL must open the client's Chrome,
Edge, Safari or Opera profile; today there is no manifest field and no profile handling.
D-06: Chrome and Edge first, for the login pages the onboarding skill prints (FR-07) and
for browser automation started from the workspace; Safari and Opera later.
Status: planned (v0.10)
**Acceptance checks**
- The manifest holds a `browser:` block with the browser and profile name, and a URL
  opened by tq from the workspace starts that profile: the process arguments name the
  profile directory, not the default one.
- A Playwright or Claude-in-Chrome session started from the workspace uses that profile.

### FR-28 An onboarding wizard that provisions a workspace without further manual steps
G1. Today `tq onboard [<name>]` (`internal/cmd/onboard.go`, `onboard_wizard.go`) runs
`tq add` then a wizard for the git identity, cloud, data platform, chat, email and agent
CLIs, every prompt with Skip; `--from <answers.yaml>` skips the prompts but still needs a
terminal and a typed yes (human-only, ADR-0003); it opens a new
terminal (`internal/termopen`), leaves the workspace untrusted until `tq allow`, and never
asks for a token. Missing: "Other" with free text, more providers, multi-select data
platforms, a `blocked_commands` list, resume after a failed step, the browser prompt
(FR-27). D-07: close the originating terminal only after the new one opened.
Status: planned (v0.10); the wizard ships, the remaining prompts and the terminal handover are the work
**Acceptance checks**
- `tq onboard --from <answers>` in a new folder writes a valid manifest and exits 0, and
  a second run in a provisioned folder exits non-zero naming the manifest
  (`TestOnboard_FromWritesValidManifestWithSkippedBlocks`,
  `TestLoad_RefusesUnknownKeysAndSecrets`, `TestOnboard_FullAnswersAndTerminal`).
- After the new identity terminal opens, the terminal that ran `tq onboard` ends; when the
  new terminal cannot be opened, the old one stays open and prints the failing step.

### FR-29 A startup banner, once, at onboarding
G1. A large ASCII "Tentaqles" banner with version and folder as an identity marker, with
`--no-banner` and `TQ_NO_BANNER` for scripts; today there is no banner code. D-01: once,
at onboarding, not on every terminal; one text asset the owner can replace.
Status: planned (v0.10)
**Acceptance checks**
- `tq onboard` prints the banner, then one line with the tq version and the working
  directory, before the first prompt; `--no-banner` and `TQ_NO_BANNER=1` print neither.
- In a terminal that reports no UTF-8, the plain-text fallback prints with no replacement
  characters.

### FR-30 Install or link the selected CLIs into an identity-scoped location
G1. Today `internal/setup/toolcheck.go` and `tq providers check` only probe whether a CLI
is installed; the catalog carries install hints. D-05: install or link into the
workspace's shim folder (FR-26); global-only tools recorded in the manifest with their
shim strategy.
Status: planned (v0.10)
**Acceptance checks**
- After onboarding with `aws` selected on a machine without it, `tq providers check` shows
  `aws` installed, and the binary resolves only inside that workspace's terminal.
- A tool that only installs globally is recorded in the manifest with its shim strategy.

### FR-31 Harness coverage: OpenCode, Kimi with the minimal hook
G1. Each harness needs the identity redirect, the launcher and the hook of FR-10. Today
Claude Code is full (a launcher that always adds `--dangerously-skip-permissions`, the
hook, the status line, bundles); Codex has `CODEX_HOME`, the `config.toml` block, a hooks
file the user trusts once in `/hooks` and a launcher that adds
`--dangerously-bypass-approvals-and-sandbox` (ADR-0006; typed approval or sandbox flags win);
OpenCode has a catalog entry (`opencode.yaml`) with config dirs only; Kimi Code has none.
D-11: OpenCode next, Kimi Code later once its hook support is checked; Codex best effort.
Status: planned (v0.10)
**Acceptance checks**
- An OpenCode session started from a workspace runs the tq PreToolUse hook: a `.env` read
  is blocked with `tq/env-file-read`; a `kimi` catalog entry exists and `tq login <ws>
  kimi` writes under `~/.tentaqles/identities/<ws>/kimi`.
- `TestRunClaudeAlwaysBypass` and `TestPreToolUseIncompleteScanDeniesBothAgents` keep passing.

## 5. Data

| Data | Class | Source | Store | Retention |
|---|---|---|---|---|
| Workspace manifest `.tentaqles.yaml` | internal (names only; the loader rejects secret-shaped values) | `tq add`, `tq onboard`, `tq setup`, human edits | the client folder (versionable) | life of the workspace; the trust hash under `$TQ_HOME/trust` is dropped on edit |
| Harness and CLI logins | secrets | the CLIs' own login flows (`tq login`) | per-identity config dirs under `~/.tentaqles/identities/<ws>/<id>` | until the CLI logs out; never copied by tq |
| Issue-tracker token | secrets | `tq secret set` (human) | OS keychain, service `tentaqles`, account `<ws>/<provider>` | until `tq secret rm` |
| `.env` values | secrets | the project | the project; read only through `tq dotenv run`, masked in output | never stored by tq |
| Bundle catalog | internal | the human | `$TQ_HOME/bundles/catalog.yaml` | life of the install |
| Memory (`memory.db`), vault export, knowledge graph | internal; secret patterns and checksummed identifiers masked at every write (`plugin/tentaqles/privacy.py`) | sessions, hooks, ingest | per-identity plugin data; the Obsidian vault folder; `meta.db` holds only names, stats and signals | decay and compaction |
| Client repos' personal data | the client's | the client | the client's systems; read locally, never written by tq | n/a |
| Audit log | internal (commands masked) | `tq env` | `$TQ_HOME/audit.jsonl` | no retention policy; append-only, no hash chain (AR1) |
| Migration journal and backups | internal | `tq migrate` | `journal.json` and `.tq-backup` files beside the migrated files | until `tq uninstall --restore` |
| Telemetry | none | | tq sends none | |

## 6. Security

The threat model is `docs/threat-model.md` (next review 2027-02-01), the checklist the
`security-reviewer` agent reads on every diff. Boundaries, one STRIDE table each: B1 agent
session to the tq hook and `$TQ_HOME`; B2 human to the human-only actions (`tq allow`,
`tq deny`, `tq secret` writes, `tq run --off`, the `tq onboard` wizard, logins); B8 the
release host and repo to the user's machine (installers, tq binary, plugin); B9
contributors and CI to the repo and its releases.

Authentication: human-only actions refuse any session that carries an agent marker
(`CLAUDECODE`, `CLAUDE_PLUGIN_ROOT`, the Codex sandbox markers), and `tq secret set` and
the wizard also need a terminal on stdin; manifests are trusted by SHA-256 hash; release
artifacts carry checksums and build provenance; the plugin installs from a tag pinned to a
sha; bundle sources over plain-text or git transports are refused.

Abuse cases the hook is built for: an agent steered by text it read pushes under the wrong
account, runs a cloud command for another client, reads `.env` or the keychain, prints the
environment, commits a secret, adds another client's MCP server, or trusts a manifest it
edited. Accepted until 2027-02-01: a program already running as the user, which can read
the keychain and `$TQ_HOME` and drive a pty (AR1); a determined adversary rewording shell
commands past the text filters, a hook timeout or parse error that lets a call through,
and every action the owner left to the harness's permission settings (AR2). Known gaps:
the off launchers check markers but not a terminal (B-01); the audit log has no hash chain
(AR1); the installers are served from `main` (B-09).

## 7. Non-functional requirements

- Platforms: Windows 11 first (cmd.exe, PowerShell 5.1, pwsh, Git Bash), then macOS and
  Linux (WSL2 included). Go 1.26; Python 3 for the plugin (stdlib plus pyyaml, pathspec,
  numpy; fastembed optional). No Node.
- Hooks: every hook declares a timeout (`plugin/hooks/hooks.json`: SessionStart 30 s, 15 s
  and 30 s; PreToolUse 15 s, and 10 s for the snapshot guard on Write; PostToolUse 10 s
  async; Stop 15 s; SessionEnd 45 s); nothing in a hook waits on the network or a model; a
  stalled hook is a P0.
- Guard latency: one git config read and a bounded gh lookup under a 5 s context
  (`internal/cmd/claudehook.go`); the commit scan has an 8 s budget.
- Tests: `go test ./...` green on Windows, macOS and Linux CI; the required checks on
  `main` are the `go` job on three OSes, `govulncheck`, `marketplace-pin` and `plugin`
  (`.github/workflows/test.yml`).
- Offline: everything except provider logins and the plugin's optional model calls works
  offline. One binary, one plugin; no daemon, no server, no database in tq.

## 8. Data model sketch

A **base folder** (`tq init`) holds **workspaces** (client folders), each with one
**manifest** and several **identities** (one per CLI or harness login, each a config
directory under `~/.tentaqles/identities/<ws>/<id>`). A manifest is **trusted** by hash.
Its blocks (`internal/manifest/manifest.go`): `schema`, `client`, `display_name`,
`language`, `color`; `git` (name, email, provider, user, host, expected_user); `cloud`
(the primary provider, `also`, expected account or subscription, profile); `database`,
`data_platform`, `chat`, `email` (a provider name each); `stack`; `claude`
(`bundle`, `env`); `blocked_commands` (enforced by `blocked-command`); `agents`;
`identities` (one entry per CLI login, derived by `tq add`). The retired
`claude.permission_mode` and `claude.permissions` keys, like a top-level key outside this
list, are ignored on load and reported by `tq doctor` (`manifest-dropped-block`). The issue-tracker credential has no
manifest block: the provider is the `tq secret` account name.

The **state folder** `$TQ_HOME` (`~/.tentaqles`): `identities/<ws>/<id>/` (config homes
and each identity's plugin data, `memory.db` included), `trust/` (one hash file per
trusted manifest), `bundles/catalog.yaml`, `config.yaml` (the registered bases),
`audit.jsonl`, `knowledge.yaml` (the vault folder) and the plugin's global `meta.db`.

## 9. Stack and constraints

- `tq`: Go 1.26, cobra CLI, go-keyring for the OS keychain; no cgo. Plugin: Python,
  bootstrapped through `tq_run.sh` and `tq_env.sh`; `prd.py` stdlib only; pyyaml,
  pathspec, numpy required; fastembed optional and never installed in CI.
- Releases: goreleaser, Homebrew tap and Scoop bucket by deploy keys, build attestations,
  actions pinned by sha, Dependabot without major action bumps, `govulncheck` required.
- Constraints that hold everywhere: credentials never in manifests; hooks never stall;
  trust, secret writes and logins are human-only; the hook allows or blocks and never asks
  (D-31); tq sets no permission mode of its own beyond the launcher's skip (D-30); an off
  launch sets no variable and stores nothing; one memory store per workspace, never
  joined; no automatic secret rotation.

## 10. Open questions

None. A new question goes here as `- NEEDS-DECISION: ...` and makes the PRD not
planning-ready until it is settled.

## 11. Releases

| Release | FRs | Done when |
|---|---|---|
| v0.9.0 | FR-01 to FR-24 | Shipped 2026-11-04: tq v0.9.0, plugin 0.16.0, the root marketplace pinned to the tag and its sha; every acceptance check of these FRs passes |
| v0.10 | FR-25 to FR-31 | Isolation completeness: prompt and title indicator, shim folders and CLI provisioning, browser profiles, the remaining onboarding prompts and the terminal handover, the banner, OpenCode then Kimi; every acceptance check of these FRs passes |

## 12. Prioritized backlog

Priority: P0 closes a security gap; P1 completes isolation, the product's core promise;
P2 depth; P3 follow-ups with no dependency on the rest.

| # | Priority | Item | FR | Why this priority | Depends on |
|---|---|---|---|---|---|
| B-01 | P0 | Done 2026-11-05 (ADR-0003): `tq run --off` and every other human-only command need a terminal on stdin and stdout, no harness in the process tree and no agent marker, then a typed `yes` | FR-11, FR-12 | The one human-only gap the threat model recorded (B2) | none |
| B-03 | P1 | Browser profile per workspace: Chrome and Edge first, for the onboarding login pages and Claude in Chrome or Playwright | FR-27 | Named in the vision's first paragraph; the last identity surface not isolated | D-06 |
| B-04 | P1 | Shim folder per workspace so unselected CLIs are unreachable; CLI install or link into it | FR-26, FR-30 | The vision's first bullet; what makes "AWS-only terminal" true | a Windows prototype (D-05) |
| B-05 | P1 | Prompt prefix and window title; the onboarding banner with `--no-banner` | FR-25, FR-29 | Low effort, high clarity | D-01 |
| B-06 | P1 | Onboarding prompts: browser, Other with free text, multi-select data platforms, resume after a failed step, close the originating terminal after the new one opens | FR-28 | Completes the wizard | B-03 for the browser prompt to mean something (D-07) |
| B-07 | P2 | OpenCode hook, launcher and catalog entry; then Kimi Code once its hook support is checked | FR-31, FR-03 | The vision lists four harnesses | D-11 |
| B-08 | P2 | Stack preferences resolved per workspace; manifest editing with re-trust (`tq edit`, GUI parked) | FR-05, FR-01 | Completeness of the stack block and the `tq add` "already exists" dead end | none |
| B-09 | P3 | Signing `trust/`, `config.yaml` and `audit.jsonl`; installers served from a tag instead of `main` | FR-16, FR-21 | Threat-model follow-ups; AR1 and AR2 accepted until 2027-02-01 | none |

## 13. Decisions record

| # | Date | Decision | Outcome |
|---|---|---|---|
| D-01 | 2026-10-30 | Identity indicator and banner | Prompt prefix `[client]` plus the window title in every shell, an `[off]` marker in a `claude-off` window; the banner shows once, at onboarding |
| D-04 | 2026-10-30 | Credential store | Per-identity config dirs plus the OS keychain with per-workspace account names; "trust store" means manifest trust |
| D-05 | 2026-10-30 | Unselected CLIs and provisioning | A shim folder per workspace first on PATH; global-only tools recorded in the manifest with their shim strategy |
| D-06 | 2026-10-30 | Browser profiles | Chrome and Edge first, for the onboarding login pages and for Claude in Chrome / Playwright; Safari and Opera later |
| D-07 | 2026-10-30 | Terminal after onboarding | Close the originating terminal, only after the new one opened; on failure keep it and print the failing step |
| D-08 | 2026-10-30 | Onboarding skill | Logins and `tq secret` stay human-only; the skill prints and verifies, and later opens login pages in the client's browser profile |
| D-11 | 2026-10-30 | Harness scope | OpenCode next, Kimi Code later; Codex best effort |
| D-21 | 2026-10-30 | Knowledge base | Docs folders first (the markdown ingest of FR-20); further connectors and hybrid retrieval come with their own spec |
| D-29 | 2026-11-03 | Product scope | tentaqles is an identity isolator with secrets protection and nothing else; the plugin adds memory, preamble, knowledge graph and skills |
| D-30 | 2026-11-03 | Permission modes | tq sets no permission mode of its own beyond the launcher's skip; Claude Code's own settings govern confirmations |
| D-31 | 2026-11-03 | Hook scope | The hook blocks identity drift and secret exits and never asks; nothing a manifest, a global file or a repository file adds or removes |
