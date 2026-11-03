# Tentaqles: product requirements

Status: planning-ready (the cut of 2026-11-03 is settled as D-29 to D-31; section 10 is empty)
Owner: the repo owner (reach@tentaqles.ai)    Updated: 2026-11-03
Baseline audited: main 93d7c82, tq v0.8.2, plugin 0.15.2; this document describes tq v0.9.0

This document replaces the 74-requirement PRD of 2026-11-03 (`main` 93d7c82, called "v1"
below) as the source of truth for what Tentaqles is. On 2026-11-03 the repo owner decided
that Tentaqles becomes an identity isolator plus secrets protection and nothing else (D-29):
one folder per client; the terminal, the AI agent sessions and the external tools carry
that client's identity; a command under the wrong identity is blocked with the reason; a
secret never leaves through a transcript, a commit or a tool. Everything built on top of
that in v0.7 to v0.8.2 (the board, headless runs, tracker sync, packs, the build gate, the
guard modes, Jev, insights, the desktop app, the templates) is deleted on `main` after
v0.8.2, which stays as a tag. The cut ships as tq v0.9.0 and plugin 0.16.0.

How to read it:

- Every requirement is a `### FR-nn` in section 4 with a **Status** line: `done`, `partial`
  or `not started`. Each FR cites its evidence (a file, command, skill or test that exists
  today on `main`) and lists what is missing. For the FRs whose status is `partial`, the
  missing part is the cut itself: the code still carries a mode, a rule or a block that
  v0.9.0 removes. Acceptance checks are binary; for shipped work the check is the test
  that pins it; a test named "(new)" does not exist yet.
- `[placeholder]` marks a detail neither the documents nor the code confirm.
- Names follow the code. v1's decisions that survive keep their `D-nn` ids (§13).
- Statuses were verified on 2026-11-03 by reading code and tests, not by running the
  product end to end against live services.

Tooling: `plugin/scripts/prd-status.py` grades this file (planning-ready means section 10
holds no `NEEDS-DECISION` item and every FR has a check); `/tentaqles:direction` audits the
repo against it.

## 1. Problem and evidence

A developer who works for several companies or clients at once shares one terminal, one
set of CLIs, one AI-harness login and one browser profile across all of them. Wrong-account
pushes, cross-client secret exposure and manual re-login are routine, and an AI coding
agent makes each of those slips faster and at scale: it runs whatever its shell is logged
in to, reads any `.env` it is pointed at, and commits what it reads.

Evidence, from the repo's own history:

- The owner's vision note (`docs/IDEATION.md`) names the daily failure modes: one terminal
  and one login for every client, and the need for per-client Claude Code accounts because
  two clients each supply their own subscription.
- `docs/TEST_RESULTS_RUN01.md` (2026-10-29) recorded an install where two identities shared
  one plugins folder and ran plugin 0.11.1 against a current 0.15.0; fixed in PRs #51 to #53.
- `docs/threat-model.md` (2026-10-31) records that the main adversary is the agent itself,
  steered by text it reads or simply mistaken, and the second is the supply chain.
- The owner's decision of 2026-11-03 (D-29) records that the board, the packs, the gate,
  the decision layer and the guard modes added surface, state and human-only ceremony the
  daily problem does not need, and that the two properties used every day are identity
  isolation and secrets protection. v1 counted 74 requirements, 75 guard rule ids in 13
  categories, five guard modes and ten trust boundaries; this document keeps 31
  requirements, 15 rule ids and four boundaries.

## 2. Users and roles

| Role | Who they are | What they need from this | Can | Cannot |
|---|---|---|---|---|
| Developer (the human) | One freelance or multi-employer developer, one machine; Windows first, then macOS and Linux | One folder per client that is logged in and tooled for that client only, in the terminal and in every agent session | Trust manifests (`tq allow`, `tq deny`), store the project-management credential (`tq secret`), log in, start a no-tq session, release | Be impersonated: those actions refuse any session that carries an agent marker |
| Agent session (Claude Code, Codex; OpenCode and Kimi on the roadmap) | A harness session started in a workspace terminal or by `tq run` | The identity preamble, memory, skills, and a hook that blocks identity drift and secret exits with a reason it can act on | Read and edit the project; run git, gh and cloud commands under the workspace's identity; use `.env` values through `tq dotenv run` | Trust a manifest, write a secret, read `.env` or the keychain, print the environment, commit a secret, use another client's MCP server, act under another client's identity, start a no-tq session |
| Nested agent | An agent started by an agent session (`claude -p`, `codex`, an `Agent` tool call) | The same identity and the same hook as its parent | Everything its parent can | Loosen anything: it inherits the parent's config homes and blocked commands and the hook runs in it too |
| Client (the company) | The client's git host, cloud, project-management tool, chat and email | Only the developer's actions under that client's own accounts | Receive pushes, cloud changes and messages made from its workspace | See another client's identity, secrets or memory |
| Maintainer / releaser | The repo owner shipping tq and the plugin | One tag releases everything, pinned and verifiable | Merge with green checks, tag, pin the marketplace | Move a release tag; merge red |

## 3. Goals

- G1 **Isolation.** From any client folder, every git, cloud, harness and project-management
  action runs under that client's identity, in the terminal and inside every agent session,
  and a command that would use another client's credential is blocked with the reason.
  Measure: the identity guard blocks 100% of the drift cases in `internal/guard/guard_test.go`
  and `tq doctor` reports no drift on a configured machine.
- G2 **Secrets never leave.** A `.env` value, a keychain entry or the environment never
  reaches a transcript, a commit or a tool's output; the only way to use a `.env` is
  `tq dotenv run`, which masks the values. Measure: the secret rules in `internal/policy`
  and `TestDotenvRunLoadsAndMasks` pass; `tq secrets audit` is clean on a configured machine.
- G3 **Memory per workspace.** Agents remember a client's decisions and history without ever
  mixing clients: one memory store per workspace, a global store that holds names, stats
  and signals only. Measure: `plugin/tests/test_search_memory.py` passes and no row of one
  workspace's `memory.db` names a path under another workspace.
- G4 **Safe distribution.** Every install is checked against the release's checksums and
  build provenance, and the plugin installs from a tag pinned to a commit. Measure: the
  installers refuse a tampered archive (verified 2026-10-30) and `gh attestation verify`
  passes for every asset of the release.

### Non-goals

What v0.9.0 removes, and why (D-29):

- The hub board, headless runs, decisions, the review page, the web API and the desktop
  app: a task system the owner did not need beside the client's own tracker; it brought
  session classes, a console protocol and six trust boundaries with it.
- Tracker sync and its credential chain: the agent reads a tracker through the harness's
  own MCP connector; tq keeps only the credential (`tq secret`) and a presence check.
- Packs (data, ml, cloud, db), their checks, guards, signed ledgers and approvals: domain
  review belongs in the client's CI, not in the identity tool.
- The build gate, locked tests, signed records and the gated `/tentaqles:build` engine:
  the dev-process skills stay as plain checklists with read-only review agents.
- Guard modes (`off`, `default`, `relaxed`, `strict`, `custom`), mode grants, the mode log
  and every guard rule beyond identity and secrets (git-history, prod-changes, cloud, db,
  iac, mcp writes, ci, system, pack, personal-data exit, self-protection and nested-agent
  rules): confirmations were the cost; the hook now allows or blocks and never asks (D-31).
- `tq decide` (Jev), `tq insights`, `tq redact`, the data classes and the `data:` block.
- The manifest blocks `decision:`, `guard:`, `tracker:`, `packs:`, `data:`; the root
  `templates/`; the `desktop/` app and its installers.

Standing non-goals carried from v1: multi-user or team sharing of identities (one
developer, one machine); hosting any state on a remote service; replacing the harness's
own permission system (tq sets no permission mode beyond the launcher's skip, D-30);
defending against a program already running as the developer's OS user (AR1) or a
determined adversary rewording shell commands (AR2); automatic secret rotation.

## 4. Functional requirements

Each FR has a **Status** line, **Evidence**, **Missing** and binary **Acceptance checks**.
Areas A to D are v0.9.0; area E is the roadmap (v0.10).

### Area A: identity isolation

### FR-01 Register client folders and create a workspace with a trusted manifest
Why: G1. Everything else keys off the folder and its manifest.
Status: done
Evidence: `tq init <base-folder>` registers a base folder (`internal/cmd/init.go`); `tq add
<name>` scaffolds the workspace, `.tentaqles.yaml` (schema `tentaqles-client-v1` or `v2`),
the identity folders and the git identity (`internal/workspace/scaffold.go`); `tq allow
<name>` pins the manifest by SHA-256 under `$TQ_HOME/trust` (`internal/trust`); any edit
untrusts it. The manifest loader rejects any value that looks like a secret.
Missing: nothing for the trust itself. `tq add` refuses an existing manifest with "already
exists" but does not point to an edit command; there is no `tq edit` for manifests. The
`--mode` and `--global` grants of `tq allow` leave with the modes (FR-11).
**Acceptance checks**
- `TestHashChangesWhenFileChanges` passes, and the edited-manifest test (today
  `TestPlainAllowAfterEditDropsTheGrant`, renamed when grants go) shows `tq list` printing
  `TRUSTED false` after an edit until a human runs `tq allow` again.
- Running `tq add <name>` in a folder that already holds `.tentaqles.yaml` exits non-zero
  without writing.
- `tq list` shows the workspace with `TRUSTED true` only after a human ran `tq allow`.

### FR-02 The identity follows the folder in every supported shell
Why: G1. Opening a terminal in a client folder must yield that client's environment.
Status: done
Evidence: `tq activate <shell>` prints the hook; templates for bash, zsh, fish, PowerShell,
pwsh and cmd in `internal/shell/hooks/*.tmpl`; the hook runs `tq env`, which applies
`envplan.Desired` (`internal/envplan/plan.go`): `TQ_WS`, `TQ_WS_ROOT`, the git identity
and one config-dir variable per identity; leaving a workspace undoes them
(`TestEnviron_UndoesOtherWorkspaceShellState`). `tq hooks install|remove|status` manages
the profiles. `tq run <ws>|. -- <cmd>` applies an identity without `cd`. `TestHook_Golden`,
`TestDesired_SharedCLIVariableKeepsTheFirstIdentity`.
Missing: nothing for v0.9.0. Only environment variables change: PATH and tool discovery are
untouched (FR-26); cmd.exe has no automatic per-prompt hook (call it after `cd`, or use
`tq run`). Git Bash is served by the bash template [placeholder: not confirmed on a Git
Bash profile].
**Acceptance checks**
- In a trusted workspace, `git config user.email` prints the manifest's email and
  `$env:CLAUDE_CONFIG_DIR` points under `~/.tentaqles/identities/<ws>/claude`; in a
  neutral folder both are unset (COMMANDS.md walkthrough, identity step).
- `TestHook_Golden` passes for every shell template.
- `TestEnviron_UndoesOtherWorkspaceShellState` passes: moving from workspace A to B leaves
  no A variable set.

### FR-03 One harness login and config home per workspace (Claude, Codex)
Why: G1. Two clients that each supply a Claude Code subscription must each be logged in to
their own account, and the agent must act under it.
Status: done
Evidence: catalog entries redirect the config home per identity: Claude `CLAUDE_CONFIG_DIR`,
Codex `CODEX_HOME` (`internal/providers/catalog/claude.yaml`, `codex.yaml`), under
`~/.tentaqles/identities/<ws>/<id>`; `tq login <workspace> <identity>` logs in there; the
global install is never written, only the variable is redirected. A session whose
`CLAUDE_CONFIG_DIR` is not the workspace's is blocked on `git push` and `gh`
(`claude-config-drift`); `tq doctor` reports `identity-wrong-account`,
`identity-wrong-subscription` and `identity-logged-out` (`TestRunForCwd_ClaudeConfigDrift`,
`TestVerifyFindings_WrongAccountIsAnError`).
Missing: nothing for v0.9.0. OpenCode has a catalog entry (`opencode.yaml`) with config dirs
only and no hook; Kimi has no entry: both are FR-31. No test proves the global `~/.claude`
stays untouched [placeholder].
**Acceptance checks**
- Starting `claude` in workspace A and in workspace B writes credentials under two
  different `CLAUDE_CONFIG_DIR` folders and leaves `~/.claude` unchanged (byte-compare
  before and after).
- `TestRunForCwd_ClaudeConfigDrift` and `TestVerifyFindings_WrongAccountIsAnError` pass.
- `tq login <ws> codex` writes under `~/.tentaqles/identities/<ws>/codex` and `CODEX_HOME`
  points there in the workspace shell.

### FR-04 Per-workspace provider CLI config homes (gh, cloud)
Why: G1. git hosts, clouds, data platforms and other CLIs must each act under the client's
account.
Status: done
Evidence: 62 catalog entries (`internal/providers/catalog/`): gh `GH_CONFIG_DIR`, glab
`GLAB_CONFIG_DIR`, az and azure-devops `AZURE_CONFIG_DIR`, aws `AWS_CONFIG_FILE`,
`AWS_SHARED_CREDENTIALS_FILE` and `AWS_PROFILE`, gcloud `CLOUDSDK_CONFIG`, databricks
`DATABRICKS_CONFIG_FILE`, snowflake `SNOWFLAKE_HOME`, plus docker, kube, helm, terraform,
pulumi, vercel, supabase, stripe and others. `tq providers list|show|check|add`.
`cloud.also` lets a trusted manifest declare extra clouds. `TestAllProviderVars_CoversCatalog`,
`TestInlineIdentityOverride`, `TestWriteWorkspace`, `TestPreToolUse_UntrustedManifestCannotAddClouds`.
Missing: no identity variable for Bitbucket, Slack, Teams, Gmail, Outlook, Jira, Linear or
Asana (chat, email and project management go through MCP, with the credential in the
keychain, FR-14). `az` and `azure-devops` share one `AZURE_CONFIG_DIR`, so they share one
login. The top-level `providers/` folder holds only a README while CLAUDE.md calls it
"(planned)" (FR-24).
**Acceptance checks**
- `tq providers check` lists every declared CLI with installed yes/no and version.
- `TestInlineIdentityOverride`, `TestWriteWorkspace` and `TestAllProviderVars_CoversCatalog`
  pass.
- A `gh auth status` run in workspace A names A's account and never B's.

### FR-05 Stack and preferences per workspace
Why: G1. Runtimes, linters, formatter configs and editor settings declared in the manifest
and resolved per identity, so a project scaffold starts with the client's stack.
Status: partial
Evidence: `Manifest.Stack []string` (`stack:`); the plugin reads it
(`plugin/tentaqles/manifest/loader.py`) and `/tentaqles:add-project` writes it into the
project `CLAUDE.md` (`test_add_project_scaffolds_the_fixed_sections_and_points_to_kickoff`);
`language:` drives pt-BR or en in the skills.
Missing: tq does not resolve runtimes, package managers, linters, formatter configs or
editor settings per workspace, and the manifest has no field for them [placeholder: exact
field design]. v1 tied the stack to the `kickoff` stack ADR of the gated flow; the gate
goes, the `kickoff` checklist keeps the ADR step as prose (B-08).
**Acceptance checks**
- A manifest `stack:` value appears in the generated project `CLAUDE.md` of
  `/tentaqles:add-project` (`test_add_project_scaffolds_the_fixed_sections_and_points_to_kickoff`).
- [placeholder] a declared formatter config is applied in the workspace terminal and not
  outside it, once the field exists.

### FR-06 The plugin and MCP set are pre-registered per identity (bundles)
Why: G1. The first harness session already has the Tentaqles plugin and only this client's
MCP servers.
Status: done
Evidence: a shared catalog (`$TQ_HOME/bundles/catalog.yaml`) of marketplaces, plugins,
skills and MCP servers; `tq bundle catalog|diff|sync|capture` materialize the manifest's
`claude.bundle` into the identity's Claude settings (`internal/bundle`; `TestSync_EndToEnd`);
unsafe sources are refused (`TestValidate_PlainTextTransport`, `TestValidate_GitURLTransport`);
another client's MCP server is blocked by `tq/cross-client-mcp`
(`TestPolicy_CrossClientMCPDenied`); the plugin installs from the repo's root marketplace
pinned to a release tag and commit (`TestRootMarketplacePin`; `docs/RELEASING.md`).
`TestBundleSync_RefusesUntrusted`.
Missing: nothing for v0.9.0. Claude Code only; nothing for Codex plugins. `tq onboard` does
not run `tq bundle sync`; the human runs it after `tq allow`.
**Acceptance checks**
- `tq bundle diff <ws>` prints no drift right after `tq bundle sync <ws>` on a trusted
  workspace.
- `TestBundleSync_RefusesUntrusted`, `TestPolicy_CrossClientMCPDenied` and
  `TestRootMarketplacePin` pass.
- After `tq onboard`, `tq allow` and `tq bundle sync`, the first `claude` session in the
  workspace lists `tentaqles@tentaqles` in `/plugin` without a manual install step.

### FR-07 Onboarding verifies every provider login (project management: credential check only, no sync)
Why: G1. After the wizard, every declared provider is logged in under the workspace's
identity, verified by a read-only call, and nothing undeclared is prompted for.
Status: partial
Evidence: `plugin/skills/onboard/SKILL.md` reads the manifest, prints the `tq login` and
`tq secret set` lines for the human to run, guides `/mcp`, then verifies with `tq doctor
--json` plus read-only calls; `check` re-runs the verification; undeclared blocks are
skipped (`test_onboard_skill_covers_every_provider_kind`,
`test_onboard_skill_runs_no_human_only_command`). `tq doctor` service checks: `tracker-ok`,
`tracker-needs-session`, `tracker-no-adapter`, `tracker-auth-failed`, `chat-ok`,
`chat-mcp-missing`, `email-ok`, `email-mcp-missing` (`TestTracker_OKNamesAccountAndScope`).
Missing: the tracker check runs through the sync adapters under `internal/hub/sync`, which
the cut removes; v0.9.0 keeps a credential-only check under the renamed
`project_management:` block (`tq secret status` plus one read-only call through the
provider's CLI or MCP server) and the skill drops its `acli` and sync steps. D-08 stands:
logins and `tq secret` stay human-only; the skill prints them and, once FR-27 exists, opens
each login page in the workspace's browser profile.
**Acceptance checks**
- `/tentaqles:onboard` on a workspace whose project-management credential is missing
  reports it as failed and names `tq secret set <ws> <provider>`; with every service
  reachable it reports each as ok.
- `test_onboard_skill_runs_no_human_only_command` passes: the skill never executes
  `tq secret set` or a login itself.
- The skill text names no `tq hub`, `acli` or sync step (grep is empty).

### FR-08 Machine setup from a plan (`tq setup`, no desktop)
Why: G1. Setting up several clients on a new machine in one pass.
Status: partial
Evidence: `tq setup --example|--from <plan> [--dry-run|--yes]` scaffolds several workspaces
and prints the login lines (`internal/setup`, `docs/SETUP.md`); `TestApply_EndToEnd`,
`TestApply_WritesServiceBlocks`, `TestLoadPlan_TrustDefaultsTrueWhenAbsent`,
`TestToolCheck_DedupsPerCompany`, `TestLoad_RefusesUnknownKeysAndSecrets`.
Missing: the cut removes the Wails app under `desktop/`, the `setupapi` surface it shared,
the desktop job of `release.yml`, the `desktop` CI job and `docs/DESKTOP.md`; the plan
format drops the `guard`, `packs`, `tracker` and `data` keys and refuses them like any
unknown key.
**Acceptance checks**
- `tq setup --from <plan> --dry-run` prints the plan and writes nothing; with `--yes` it
  creates every workspace listed (`TestApply_EndToEnd`).
- A plan with a `guard:` or `packs:` key is refused with the key named
  (`TestLoad_RefusesUnknownKeysAndSecrets`).
- `desktop/` is absent from the tree and `.github/workflows/release.yml` has no desktop job.

### FR-09 Adopt a hand-built setup, and undo it (`tq migrate`, `tq uninstall --restore`, no mode step)
Why: G1. Existing users had their own dotfile and shim setups before tq.
Status: partial
Evidence: `tq migrate` (plan, then `--apply`) with a journal (`journal.json`) and byte-exact
`.tq-backup` files; `tq uninstall --restore [latest]` replays the journal backwards and
resumes an interrupted restore; `TQ_ENABLED=0` stops shell activation (`docs/MIGRATE.md`).
`TestRecordNeverLeavesJournalAbsent`, `TestApproveMigrateFlow`,
`TestRestoreResumesAfterMiddleFailure`,
`TestShellApplyMakesStatusInstalledAndRestoreIsByteExact`.
Missing: the steps that turn a legacy `unattended:` block or bypass alias into a mode grant
(`TestGuardWriterConvertsBypassToMode`, `TestRewriteConvertsLegacyBypass`) leave with the
modes; `tq migrate` then leaves such keys in place and `tq doctor` reports them as dropped
blocks (FR-16). Documented rough edges remain (MIGRATE.md "Known rough edges"): PATH shims
survive removing AutoRun; backup sidecars survive a restore; a failed apply leaves a
partial migration; `uninstall` is not an uninstaller of tq itself.
**Acceptance checks**
- `tq migrate` without `--apply` changes nothing (diff of `$HOME` before and after is
  empty).
- After `tq migrate --apply` then `tq uninstall --restore latest --yes`, every file the
  journal names is byte-identical to its backup
  (`TestShellApplyMakesStatusInstalledAndRestoreIsByteExact`).
- `tq migrate` on a profile with a legacy bypass alias plans the shell step and no mode
  step: the plan output has no `mode` line.

### FR-10 Every agent tool call passes the hook: identity drift and secret exits only, allow or block, never ask
Why: G1, G2. Allow or block each tool call with a reason the agent can act on; never ask.
Status: partial
Evidence: the plugin PreToolUse hook (`plugin/hooks/hooks.json`, 15 s) runs `tq claude-hook
pre-tool-use` for Bash, PowerShell, Read, Edit, Write, MultiEdit, NotebookEdit, Grep, Agent
and `mcp__*`; `guard.Decide` (`internal/guard/guard.go`) blocks with exit 2 and the fix on
`blocked-command`, `wrong-cloud`, `neutral-remote`, `untrusted`, `env-drift`,
`claude-config-drift`, `git-email-drift` and `gh-user`; the rules `tq/cross-client-mcp`,
`tq/secret-store-read`, `tq/secret-store-shell`, `tq/env-file-read`, `tq/env-file-shell`,
`tq/env-dump` and `tq/commit-secret` deny (`internal/policy/builtin.go`); a guard panic
is `tq/guard-error` (`TestPreToolUseGuardPanicAsks`); Codex gets the same hook through
`$CODEX_HOME/hooks.json` (`internal/codexcfg`); with tq missing the hook falls back to the
Python identity guard, with no Python it blocks shell tools (`test_tq_hook_blocks_when_no_python`).
Missing: the hook still evaluates modes and about 60 other rules (75 ids in 13 categories
today); v0.9.0 removes them and every `ask`: `tq/guard-error` and the commit scan's "took
too long" outcome become denies and the `permissionDecision: ask` output goes. The rules
stay text filters (AR2); a hook timeout still fails open.
**Acceptance checks**
- `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`, `TestPolicy_ReadEnvDenied_WindowsPath`,
  `TestPolicy_CrossClientMCPDenied` and `TestPolicy_CommitWithSecretDenied` pass.
- `TestHookNeverAsks` (new): over every built-in rule and a forced guard panic, the hook
  exits 0 or 2 and never emits `permissionDecision: ask`, for Claude and for Codex.
- `TestEveryRuleHasCategory` lists exactly the 15 ids above and the hook reads no mode;
  `test_hooks_json_pretooluse_matcher_covers_shell_file_and_mcp_tools` passes.

### FR-11 Trust and `tq allow`/`tq deny` are human-only; no modes, no approvals
Why: G1. An agent must not be able to trust a manifest it edited or store a credential.
Status: partial
Evidence: `refuseAgent` and `requireHuman` (`internal/cmd/humanonly.go`) refuse when an
agent marker is set (`CLAUDECODE`, `CLAUDE_PLUGIN_ROOT`, `TENTAQLES_HEADLESS`, `TQ_HUB_TASK`,
`CODEX_SANDBOX*`); applied to `tq secret set|rm|status` (`TestSecretIsHumanOnly`), `tq allow
--mode`, `tq mode ...` and `tq unattended` (`TestAllowModeIsHumanOnly`,
`TestModeWritesRefuseAgents`). Plain `tq allow <name>` and `tq deny <name>` are protected
only by the `tq/guard-change` ask in the hook.
Missing: v0.9.0 removes `tq allow --mode|--global`, `tq mode`, `tq unattended`, `tq guard`,
the rules `tq/guard-change`, `tq/mode-change` and `tq/human-answer`, the pack approvals and
the build-gate approvals; plain `tq allow` and `tq deny` then refuse an agent session by
environment marker in the binary (B-01). `TQ_HUB_TASK` and `TENTAQLES_HEADLESS` leave the
marker list with the hub; `CLAUDECODE`, `CLAUDE_PLUGIN_ROOT` and the Codex sandbox markers
stay. `tq secret` writes keep `TestSecretIsHumanOnly`.
**Acceptance checks**
- `TestAllowIsHumanOnly` (new, replaces `TestAllowModeIsHumanOnly`): with `CLAUDECODE=1`
  alone, `tq allow <ws>` and `tq deny <ws>` exit 1 with "human-only" and `$TQ_HOME/trust`
  is unchanged.
- `TestSecretIsHumanOnly` passes: each agent marker alone refuses `tq secret set|rm|status`.
- `tq --help` lists no `mode`, `guard`, `unattended`, `hub`, `pack`, `decide`, `insights`
  or `redact` command.

### FR-12 A session with no tq at all (`claude-off`, `codex-off`), human-only
Why: G1. "Pure claude code, pure terminal" for one session, without losing the identity for
new terminals in the same folder.
Status: partial
Evidence: `claude-off`, `codex-off` and `tq run --off -- claude|codex`
start the harness with the identity environment and the config-home redirect removed
(`internal/envplan/off.go`), so the main `~/.claude` or `~/.codex` is used, the plugin is
switched off and every hook exits 0 as its first statement (`plugin/scripts/tq_hook.sh`,
`tq_run.sh`, `internal/cmd/claudehook_off.go`); `off` is never stored, so a new terminal in
the folder is the identity terminal again; the off launchers refuse agent markers.
`TestRunOff`, `TestRunOffRefusesAgents`, `TestClaudeHookOffExitsSilently`,
`test_tq_hook_exits_zero_when_off`.
Missing: v0.9.0 removes the `tq off` and `tq on` shell functions, `tq env --off|--on`,
`TQ_MODE` and the `--tq-mode` flag of `tq run`; the off launchers keep working through the
off environment alone (`envplan.OffEnviron`: no identity variables, `TQ_ENABLED=0`), which
every tq hook honours as its first statement. The off launchers check agent markers only,
not a terminal; B-01 adds the terminal check. No prompt or title marker besides the statusline `tq: off` (FR-25).
**Acceptance checks**
- `TestRunOff`, `TestRunOffRefusesAgents` and `test_tq_hook_exits_zero_when_off` pass.
- `claude-off` in window A starts Claude with no `CLAUDE_CONFIG_DIR` in its environment
  while a new window B in the same folder has the workspace's `CLAUDE_CONFIG_DIR`.
- `claude-off` invoked from inside an agent session is refused with the marker named.

### FR-13 A nested agent cannot loosen identity or secrets (config homes and blocked commands follow the environment)
Why: G1, G2. A session must not escape the identity or the secret rules by starting
another agent.
Status: partial
Evidence: `tq/nested-agent-loosened` (`internal/policy/builtin_agents.go`) denies loosening
or clearing `TQ_MODE`, `TQ_ENABLED=0`, `env -i` before an agent, reassigning
`CLAUDE_CONFIG_DIR` or `CODEX_HOME`, the off launchers and loosened claude or codex flags
(`TestNestedAgentLoosened`, `TestHookNestedAgentNeverLoosensAStrictParent`); `tq run` from
an agent refuses `--off` and a looser mode (`TestRunFromAgentSession`,
`TestAgentRunTqModeComparesTables`).
Missing: the cut removes the mode comparison and the rule itself. What remains is
structural: a nested session inherits the parent's environment, so its `CLAUDE_CONFIG_DIR`,
`CODEX_HOME`, `TQ_WS` and the manifest's `blocked_commands` are the parent's, and the same
hook with the same identity and secret rules runs in it; the off launchers refuse agent
markers (FR-12); `tq run` from an agent session keeps refusing `--off` and another
workspace. A nested agent that re-exports `CLAUDE_CONFIG_DIR` to another identity hits
`claude-config-drift` on its first `git push` or `gh` call, not at launch (AR2).
**Acceptance checks**
- `TestRunFromAgentSession` passes: with `CLAUDECODE=1`, `tq run --off` and `tq run` into
  another workspace are refused.
- `TestNestedSessionKeepsIdentity` (new): in a workspace session, `CLAUDE_CONFIG_DIR=~/.claude
  claude -p "git push"` is blocked by `claude-config-drift` in the child's hook.
- `TestReadOnlyGitExemptFromEnvDriftAndUntrusted` passes.

### Area B: secrets

### FR-14 Credentials live per workspace, never in shared dotfiles
Why: G1, G2. A command issued from workspace A must not be able to use B's credential.
Status: done
Evidence: CLI logins live in per-identity config dirs (FR-04); the project-management token
lives in the OS keychain under service `tentaqles`, account `<ws>/<provider>` (`tq secret
set|rm|status`, human-only, `internal/secret`; `TestSecretSetStatusRm`,
`TestErrorsNeverCarryTheValue`); `.env` values are used only through `tq dotenv run`, masked
in output (FR-15); the manifest loader rejects secret-shaped values
(`TestValidate_RejectsTokenShapedServiceValues`). D-04: per-identity config dirs plus the OS
keychain with per-workspace account names is the design; "trust store" in tq means
manifest trust (`tq allow`). A same-user process can read the keychain (AR1).
Missing: nothing. The pack ledger keys (service `tentaqles-packs`) and the build-gate HMAC
key leave with the cut, so the keychain holds only the project-management credential;
`TestToolEnvIsFiltered` (the filtered environment of external tools) leaves with the packs.
**Acceptance checks**
- `TestSecretSetStatusRm` and `TestErrorsNeverCarryTheValue` pass.
- `tq secret status <ws>` reports the token's presence without printing it.
- A `gh` or `az` call in workspace A with B's credential present on the machine uses A's
  config dir (process environment shows A's `GH_CONFIG_DIR`).

### FR-15 Secrets never reach the transcript, a commit or a tool's environment
Why: G2. IDEATION.md group 2.
Status: done
Evidence: `.env` reads and environment dumps are deny rules (`tq/env-file-read`,
`tq/env-file-shell`, `tq/env-dump`, `tq/secret-store-read`, `tq/secret-store-shell`);
`tq dotenv run [--file F]... -- <cmd>` loads a `.env` and masks each value as
`[REDACTED:NAME]` in the output (`TestDotenvRunLoadsAndMasks`,
`TestMaskerMasksSplitValuesAndKeepsShortOnes`); `tq/commit-secret` scans staged diffs
(`internal/commitscan`, `TestScanDiffLineNumbers`, `TestPolicy_CommitWithSecretDenied`);
`tq secrets audit` finds secret-shaped values in agent config and committable `.env` files,
printing only path and line (`TestAudit`); the manifest loader rejects secret-looking
values. Standing rule: no automatic rotation.
Missing: nothing for v0.9.0. The rules are text filters (`python -c` reads are not seen,
AR2); the env-dump rule has false positives on read-only greps that mention "env". The
commit scan's "took too long" outcome asks today; v0.9.0 makes it a deny with the reason
(FR-10).
**Acceptance checks**
- `tq dotenv run --file demo.env -- bash -c 'echo token is $DEMO_TOKEN'` prints
  `token is [REDACTED:DEMO_TOKEN]` (COMMANDS.md walkthrough, secrets step).
- `TestPolicy_ReadEnvDenied_WindowsPath`, `TestPolicy_PowerShellEnvDumpDenied`,
  `TestScanDiffLineNumbers` and `TestAudit` pass.
- `TestPolicy_CommitWithSecretDenied` passes.

### FR-16 tq protects its trust store: an edited manifest loses trust; `tq doctor` reports dropped manifest blocks and unused state folders; no signed records
Why: G1. Trust must be a human act that no agent edit can keep.
Status: partial
Evidence: trust is by file hash (`internal/trust`; `TestHashChangesWhenFileChanges`,
`TestPreToolUse_UntrustedManifestCannotAddClouds`); `tq/hub-store-write` protects `hub/`,
`packs/`, `trust/`, `guard/`, `build-gate/`, `guard.yaml`, `config.yaml` and `audit.jsonl`
under `$TQ_HOME` (`TestHookHubStoreWrite`); `tq/settings-widen`, `tq/trust-store-write` and
`tq/guard-file-write` (`TestGuardProtectsItsOwnSettings`); pack and gate records are signed.
Missing: the cut removes the self-protection rules, the signed ledgers and the gate records.
What remains is the hash: a manifest edited by anyone, agent included, is untrusted until
a human runs `tq allow`, and every git, gh and cloud command in it is blocked (`untrusted`).
`tq doctor` gains `manifest-dropped-block` (a `guard:`, `decision:`, `tracker:`, `packs:`
or `data:` key still in a manifest, with the lines to delete) and `state-dir-unused`
(`$TQ_HOME/hub`, `packs`, `guard`, `build-gate` or `guard.yaml` still on disk, with the path
to remove). `audit.jsonl` keeps recording identity switches; the threat model keeps B1, B2,
B8 and B9 and drops B3 to B7 and B10.
**Acceptance checks**
- `TestHashChangesWhenFileChanges` passes; an edit to a trusted manifest makes `tq list`
  print `TRUSTED false` and `tq doctor` print `untrusted` with the `tq allow` fix.
- `TestDoctorDroppedBlocks` (new): a manifest with a `guard:` key yields
  `manifest-dropped-block` naming the key; a `$TQ_HOME/hub` folder yields
  `state-dir-unused` naming the path.
- `docs/threat-model.md` has one STRIDE table per remaining boundary (B1, B2, B8, B9) and
  names no removed rule or command.

### Area C: memory and knowledge

### FR-17 Per-workspace memory, search, decay, vault export, knowledge graph, signals, dashboard
Why: G3. Agents remember a client's decisions and history without ever mixing clients.
Status: done
Evidence: `plugin/tentaqles/memory/`: per-workspace SQLite (`memory.db`: sessions, touches,
decisions, pending, semantic and procedural memories, atoms, reference docs) with FTS5 BM25
search and a recency boost (`tests/test_search_memory.py`), decay and compaction, Obsidian
vault export and markdown ingest with a watermark (`test_vault_export.py`), a knowledge
graph (graphify or native), cross-workspace patterns and signals through a global `meta.db`
that holds only display names, stats and signals (`test_signals.py`), snapshots and
rollback, and a live dashboard (`test_dashboard.py`); hooks SessionStart, UserPromptSubmit,
PostToolUse (async), Stop and SessionEnd, all with short timeouts and slow work in detached
workers; every write passes `privacy.redact_text`. Thirteen memory skills, from
`query-memory` to `wrap` (`plugin/CLAUDE.md`).
Missing: nothing for v0.9.0. With the `data:` block gone, writes mask secret patterns only
(`redact_str` keeps its checksummed baseline); the Jev memory-gate worker leaves with
`tq decide`; port 8765 is free once `build-gate serve` goes. `scripts/pre-compact.py` has
no hook entry (possible orphan, unconfirmed).
**Acceptance checks**
- `tests/test_search_memory.py` and `tests/test_embeddings_optional.py` pass (the second
  with `fastembed` absent).
- No row in workspace A's `memory.db` names a path under workspace B's root after a session
  in each.
- `test_export_writes_one_folder_per_client_with_the_four_notes` and
  `test_context_describes_the_workspace_without_secrets` pass.

### FR-18 A PRD per project through `/tentaqles:prd` with the planning-ready grader (no cards)
Why: G3. A project starts from written, checkable requirements the developer accepted.
Status: partial
Evidence: `/tentaqles:prd` writes `docs/prd.md` with 11 fixed sections, `### FR-nn`
requirements with binary acceptance checks, open items as `NEEDS-DECISION` in §10 and
releases in §11 (`plugin/templates/process/prd.md`); `prd-status.py` grades it
(`planning-ready` when §10 is empty and every FR has a check; `test_ready_prd`,
`test_open_decision_blocks`); `kickoff` refuses to plan on a PRD that is not planning-ready
(`test_kickoff_requires_a_planning_ready_prd_and_scaffolds_everything`);
`/tentaqles:direction` audits the repo against it; `/tentaqles:grill-me` interviews.
Missing: `prd.py` still carries the hub-card half (`parse_releases`, `fr_cards`,
`record_cards`, the managed card block in §11) read by `plan-cards.py`, `refine` and `hub`;
v0.9.0 removes `plan-cards`, `refine`, `hub`, `sync`, `guard`, `pack` and the pack skills,
and `prd.py` keeps `prd_status`, `text_status` and `parse_frs` only. D-16's human-only
`accept` command is dropped with the cut: planning-ready stays the grade.
**Acceptance checks**
- `prd-status.py --cwd <project>` exits 0 on a PRD with no open decision and a check on
  every FR, and 3 otherwise (`test_ready_prd`, `test_open_decision_blocks`,
  `test_cli_exit_codes`).
- `plugin/tests/test_plan_cards.py` and `plugin/skills/plan-cards` are absent; `prd.py` has
  no `record_cards` function.
- `/tentaqles:prd` on a fresh project writes a file this grader marks planning-ready and
  that holds no managed card block.

### FR-19 Project scaffold with CLAUDE.md fixed sections and checklist skills (no `tq-project` block, no gate)
Why: G3. A project folder starts with the client context and the process skills, no gate.
Status: partial
Evidence: `/tentaqles:add-project` creates `<client_root>/<slug>/` with `CLAUDE.md` (from
`plugin/templates/process/CLAUDE-project.md` plus a Client Context section), `docs/adr`,
`docs/specs`, `LEARNINGS.md`, an optional `brief.md` from a ticket URL, and `git init`
(`test_add_project_scaffolds_the_fixed_sections_and_points_to_kickoff`); `kickoff` adds the
stack ADR, the `tq-project` JSON block, `docs/checkpoints`, `docs/reviews`, the threat model,
the privacy record and CI; the build gate installs per-project hooks; 13 agents.
Missing: v0.9.0 removes the build gate (`build-gate.py`, `gate.py`, `loop.py`,
`loopguard.py`, `project.py`, the `tq-project` block, `docs/checkpoints`, `docs/reviews`),
the agents `build-planner`, `build-fixer`, `arbiter`, `breaker` and the four pack reviewers,
`classify-data`, `privacy-review` and the pack skills; it keeps the dev-process skills as
plain checklists that run no gate and write no signed record, and five read-only review
agents: `build-critic`, `spec-critic`, `security-reviewer`, `privacy-reviewer`,
`llm-security-reviewer` [placeholder: the exact skill list is settled in the cut PR]. The
scaffold writes no `tq-project` block; `AGENTS.md` for a codex identity stays unbuilt.
**Acceptance checks**
- `test_add_project_scaffolds_the_fixed_sections_and_points_to_kickoff` passes and the
  generated `CLAUDE.md` holds no `tq-project` fence.
- `test_reviewers_are_read_only` passes for the five remaining agents and `plugin/agents/`
  holds no other file.
- `plugin/scripts/build-gate.py` and `plugin/tentaqles/workflow/gate.py` are absent and no
  skill text names `build-gate`.

### FR-20 A knowledge-base ingestion pipeline, parked
Why: G3. One connector per source into the client's memory, distilled before embedding,
with hybrid retrieval and citations (owner backlog, memory note of 2026-10-20).
Status: not started
Evidence: the markdown connector (`plugin/tentaqles/memory/ingest.py`), FTS5 search and the
vault export (FR-17) are the starting points the backlog names.
Missing: the pipeline (connectors, distillation into question, summary, resolution,
systems and code refs, hybrid retrieval fused by RRF then a reranker, incremental code
chunking, a with/without-KB eval). D-21 named docs folders and the tracker first; with the
sync gone, tracker items would arrive through the harness's MCP connector. Parked: no
release in §11 and no backlog row carries it; it returns with its own spec.
**Acceptance checks**
- A docs folder is ingested into the workspace's `memory.db` with per-item source and
  date, and `query-memory` answers a question with a citation to one of them.

### Area D: distribution and operations

### FR-21 One tag releases everything, pinned and verifiable
Why: G4.
Status: done
Evidence: `.github/workflows/release.yml` on a `v*` tag checks the tag is on `main` and the
plugin versions agree, goreleaser builds tq for every platform plus the GitHub release,
Homebrew tap and Scoop bucket with `checksums.txt`, and attests build provenance (first in
v0.8.2, all assets verified); `installers/install.ps1` and `install.sh` check the checksum
and, with a logged-in `gh`, run `gh attestation verify` against the release workflow as
signer, refusing on mismatch (verified 2026-10-30); the root marketplace pins `plugin/` to
the tag and its commit (`TestRootMarketplacePin`, `TestDevMarketplaceMatchesPlugin` in
`test/integration`; CI job `marketplace-pin`); tags `v*` cannot be moved or deleted and
`main` requires the CI checks including `govulncheck`; every action is pinned by commit
sha; Dependabot proposes weekly minor updates. `docs/RELEASING.md`.
Missing: the installers are served from mutable `main` (raw.githubusercontent), so their
own integrity rests on TLS and the GitHub account (B-09). The desktop job and its
installers leave with the cut, and the required checks on `main` drop `desktop`, `opa` and
`templates`.
**Acceptance checks**
- `TestRootMarketplacePin` and `TestDevMarketplaceMatchesPlugin` pass.
- The installer run against a deliberately altered archive exits non-zero and installs
  nothing (verified 2026-10-30 on Windows and Git Bash).
- `gh attestation verify` passes for every asset of the v0.9.0 release.

### FR-22 `tq doctor` health checks minus hub, packs and modes
Why: G1. A misconfigured identity must be visible before it causes a wrong-account action.
Status: partial
Evidence: `tq doctor` (read-only; every finding ends with the fix) covers trust, hooks, git
identity, identity dirs and logins, settings drift, plugins and bundles, guard mode and
grants, migration leftovers, services, hub and packs; checks registered in
`internal/doctor` (`registry.go`): `untrusted`, `env-drift`, `claude-config-drift`,
`git-email-drift`, `identity-wrong-account`, `identity-wrong-subscription`,
`identity-logged-out`, `identity-dir-linked`, `settings-drift`, `bundle-drift`,
`plugin-version-skew`, `plugins-dir-linked`, `claude-alias-shadow`, `cli-shadowed`,
`codex-hooks-untrusted`, `hook-missing`, `legacy-active`, `memory-stale` and the service ids
of FR-07; `tq doctor --json` (`TestDoctor_JSONFlagPrintsFindings`); `tq secrets audit`.
Missing: v0.9.0 removes the `mode`, `hub`, `packs` and `data` check groups
(`internal/doctor/mode.go`, `hub.go`, `packs.go`, `data.go`), `tq pack doctor`,
`tq worktrees` and the tracker checks that need a sync adapter, keeping a credential
presence check; it adds `manifest-dropped-block` and `state-dir-unused` (FR-16). About
half the check ids are not listed in any doc (FR-24).
**Acceptance checks**
- On a configured machine `tq doctor` prints `[ok]` lines and no `[warn]` about a `claude`
  alias shadowing the launcher (COMMANDS.md walkthrough, health step).
- `TestRunForCwd_EmailDrift`, `TestRunForCwd_ClaudeConfigDrift`, `TestRun_HookMissing`,
  `TestClaudeAliasShadow` and `TestMemoryStale` pass; `internal/doctor/hub.go`, `packs.go`,
  `mode.go` and `data.go` are absent.
- Every doctor check id in `internal/doctor` appears in `docs/COMMANDS.md` with its fix.

### FR-23 Settings render, statusline and Codex render without modes
Why: G1. Each identity's harness settings must carry tq's hooks, permissions and status
line without clobbering the user's own edits.
Status: partial
Evidence: `tq settings render [--all]` writes tq's part of each identity's Claude
`settings.json` (baseline permissions, env, status line, hooks), keeps hand edits and prints
what changed (`internal/claudesettings`; `TestApplyKeepsUserEntriesAndTakesBackOwned`,
`TestApplyLeavesCustomStatusLine`; `docs/CLAUDE-SETTINGS.md`); `tq statusline` (hidden)
shows the client and the mode, including `tq: off` (`internal/statusline`;
`TestRenderModeSegment`, `TestRenderOff`); Codex `config.toml` block, `rules/tq.rules` and
`hooks.json` through `internal/codexcfg` (`TestRenderHooksMergesWithUserHooks`,
`TestRenderRulesFile`, `TestRenderConfigConflictStillWritesRulesAndHook`).
Missing: v0.9.0 drops the mode segment from the status line (client name plus `off` only),
the mode line of the session preamble (`internal/cmd/claudehook_modeline.go`), the
`--tq-mode` launcher flag and the sandbox-by-mode mapping of the Codex render (`guard.codex`),
keeping the launcher's `-a` and `-s` Codex flags as they are today; `tq worktrees` leaves
with the build gate. `statusline` stays hidden and documented only in CLAUDE-SETTINGS.md.
**Acceptance checks**
- `tq settings render --all` twice in a row prints `up to date` for every identity on the
  second run (COMMANDS.md walkthrough, settings step).
- A hand-added key in an identity's `settings.json` survives `tq settings render`
  (`TestApplyKeepsUserEntriesAndTakesBackOwned`).
- `TestRenderOff` passes and the statusline golden output holds no mode segment;
  `TestRenderHooksMergesWithUserHooks` passes.

### FR-24 Documentation stays current with the slim command list
Why: The docs are the operator's runbook; stale statements cost real time.
Status: partial
Evidence: `docs/COMMANDS.md` (every command plus a 15-step walkthrough), `CLAUDE-HOOK.md`,
`CLAUDE-SETTINGS.md`, `MIGRATE.md`, `PROVIDERS.md`, `SETUP.md`, `RELEASING.md`,
`GUARD-SCOPE.md`, `threat-model.md`, `README.md`, the root and plugin `CLAUDE.md`; today
also `DECIDE.md`, `INSIGHTS.md`, `HUB.md`, `PACKS.md` with `docs/packs/`, `DESKTOP.md`,
`docs/design/` and `docs/decide/`.
Missing: v0.9.0 removes `DECIDE.md`, `INSIGHTS.md`, `HUB.md`, `PACKS.md`, `docs/packs/`,
`DESKTOP.md`, `docs/decide/` and the hub, pack, mode, Jev and gate sections of
`COMMANDS.md` (its walkthrough shrinks to the identity, secrets, settings and doctor
steps); rewrites `CLAUDE-HOOK.md` around the identity and secret rules, `GUARD-SCOPE.md`
around AR2, the root `CLAUDE.md` (layout, "Must not trade off", no `tq-project` block) and
`plugin/CLAUDE.md`; fixes the known mismatches: plain `tq allow <ws>` becomes human-only
(B-01), `tq login <cli>` is `tq login <workspace> <identity>`, `providers/` is not
"(planned)"; `docs/design/` specs A to E stay as history with a one-line note that they
describe v0.8.2 (B-02).
**Acceptance checks**
- `TestCommandsDocMatchesCobra` (new): every `tq` command named in `docs/COMMANDS.md`
  matches a cobra `Use:` string in `internal/cmd`.
- A grep for `tq hub`, `tq pack`, `tq decide`, `tq mode`, `tq insights` and `tq redact`
  over `docs/*.md`, `README.md`, `CLAUDE.md` and `plugin/CLAUDE.md` is empty.
- The root `CLAUDE.md` no longer marks `providers/` as planned and names no `desktop/`,
  `templates/`, `internal/hub/` or `internal/packs/`.

### Area E: roadmap, isolation completeness

### FR-25 An unambiguous identity indicator in the terminal and the harness
Why: G1. The developer must see which client a window acts for.
Status: partial
Evidence: `tq statusline` (`internal/statusline`) shows the client in the Claude Code
status bar, including `tq: off`. The identity preamble prints at session start
(`tq claude-hook session-start`).
Missing: the shell templates do not change the prompt or the window title. Decided (D-01):
a short `[client]` prompt prefix and the client name in the window title in every
supported shell, `[off]` after `tq off`; the banner (FR-29) shows once, at onboarding.
TEST_RESULTS_RUN01 reported no preamble visible in one session; cause unconfirmed.
**Acceptance checks**
- A new terminal opened in a trusted workspace shows the workspace name in the prompt or
  the window title before the first command.
- A window after `tq off` shows an "off" marker in the same place.

### FR-26 CLIs not selected for a workspace are not reachable from its terminal
Why: G1. An AWS-only client's terminal must not resolve `az` (IDEATION.md).
Status: not started
Evidence: `envplan` edits no PATH and installs no shims. The guard blocks identity drift
(`wrong-cloud`, `env-drift`), which is a different property.
Missing: everything. Decided (D-05): a shim folder per workspace holding launchers for the
selected tools only, placed first on PATH by the shell hook and removed by `tq off`; tools
that only install globally are recorded in the manifest with their shim strategy.
**Acceptance checks**
- In a workspace whose manifest declares only `aws`, `Get-Command az` (PowerShell) and
  `command -v az` (bash) fail, while they succeed in a neutral folder.
- The same commands succeed again after `tq off` in that window.

### FR-27 One browser profile per workspace, used by every URL open and browser automation
Why: G1, IDEATION.md. A terminal, harness or agent opening a URL must open the client's
Chrome, Edge, Safari or Opera profile.
Status: not started
Evidence: the only browser code today is `openBrowser` in `internal/cmd/hub_service.go`,
which leaves with the hub. No manifest field, no profile handling, no wiring for
Playwright, Puppeteer or Claude in Chrome.
Missing: everything. Decided (D-06): Chrome and Edge first, for the login pages the
onboarding skill prints (FR-07) and for browser automation started from the workspace
(Claude in Chrome, Playwright); Safari and Opera later. The hub-page targets D-06 named
are gone.
**Acceptance checks**
- The manifest holds a `browser:` block with the browser and profile name (shape
  [placeholder]).
- A URL opened by tq from the workspace (the onboarding login page) starts the declared
  profile: the process arguments name the profile directory, not the default profile.
- A Playwright or Claude-in-Chrome session started from the workspace uses that profile
  directory.

### FR-28 An onboarding wizard that provisions a workspace without further manual steps
Why: G1. One command in a new client folder yields a provisioned identity.
Status: partial
Evidence: `tq onboard [<name>]` (`internal/cmd/onboard.go`, `onboard_wizard.go`) runs
`tq add` then a wizard: name, base, email, git name, display name, git provider, git user,
primary cloud plus "also", data platform, agent CLIs (multi-select), tracker (github,
azure-devops, jira, linear, asana), chat, email; every prompt offers Skip; `--from
<answers.yaml>` is non-interactive; it opens a new terminal via `termopen` and leaves the
workspace untrusted until the human runs `tq allow`. `TestOnboard_FullAnswersAndTerminal`,
`TestOnboard_FromWritesValidManifestWithSkippedBlocks`.
Missing: the tracker prompt becomes the `project_management` prompt and records the
provider only (the credential comes through `tq secret`); no mode, guard, pack or board
prompt is added (D-30); the browser prompt waits for FR-27. Still missing against v1:
"Other" with free text; more provider options; multi-select data platforms; a
`blocked_commands` list; resume after a failed step. Decided (D-07): close the originating
terminal only after the new one opened; on failure keep it and print the failing step.
**Acceptance checks**
- `tq onboard --from <answers>` in a new folder writes a valid manifest and exits 0
  (`TestOnboard_FromWritesValidManifestWithSkippedBlocks`).
- The wizard offers no mode, guard, pack or board prompt, and its `project_management`
  prompt records only a provider (`TestOnboard_FullAnswersAndTerminal`).
- Re-running `tq onboard` in a provisioned folder exits non-zero and names the manifest.
- After the new identity terminal opens, the terminal that ran `tq onboard` ends; when the
  new terminal cannot be opened, the old one stays open and prints the failing step.

### FR-29 A startup banner, once, at onboarding
Why: G1. A large ASCII "Tentaqles" banner with version and folder as an identity marker;
`--no-banner` and `TQ_NO_BANNER` for scripts.
Status: not started
Evidence: no banner code; `tq run --off` prints one line ("tq: off, main logins, no guard").
Missing: the banner asset, the UTF-8 fallback and the flags. Decided (D-01): the banner
shows once, at onboarding, not on every terminal; the indicator is FR-25. Glyph set and
colors [placeholder: a single text asset the owner can replace].
**Acceptance checks**
- `tq onboard` prints the banner, then one line with the tq version and the working
  directory, before the first prompt; `tq onboard --no-banner` and `TQ_NO_BANNER=1` print
  neither.
- In a terminal that reports no UTF-8, the plain-text fallback prints with no replacement
  characters.

### FR-30 Install or link the selected CLIs into an identity-scoped location
Why: G1. The setup installs the CLIs and dependencies for the selected providers.
Status: not started
Evidence: `setup.ToolCheck` (`internal/setup/toolcheck.go`) and `tq providers check` only
probe whether a CLI is installed; the catalog carries install hints per provider.
Missing: installation or linking. Decided (D-05): install or link into the workspace's shim
folder (FR-26); tools that only install globally are recorded in the manifest with their
shim strategy.
**Acceptance checks**
- After onboarding with `aws` selected on a machine without it, `tq providers check` shows
  `aws` installed, and the binary resolves only inside that workspace's terminal.
- A tool that only installs globally is recorded in the manifest with its shim strategy
  [placeholder: field].

### FR-31 Harness coverage: OpenCode, Kimi with the minimal hook
Why: G1. Each harness needs the identity redirect, the launcher and the hook of FR-10,
nothing more.
Status: partial
Evidence: Claude Code: full (hook; a launcher that always adds
`--dangerously-skip-permissions`, `TestRunClaudeAlwaysBypass`; status line; bundles).
Codex: `CODEX_HOME`, `config.toml` block, `rules/tq.rules`, a hooks file the user trusts
once in `/hooks`, launcher flags `-a on-request -s workspace-write`; every ask is a deny
(`TestPreToolUseAgentCodexAsksDeny`), which a hook that never asks makes moot. OpenCode: a
catalog entry with config dirs only, no hook or launcher. Kimi Code: absent.
Missing: OpenCode and Kimi hooks and launchers; the launchers are shell functions in bash,
zsh, fish and PowerShell, not in cmd.exe. Decided (D-11): OpenCode next, Kimi Code later
once its hook support is checked; Codex stays best effort.
**Acceptance checks**
- An OpenCode session started from a workspace runs the tq PreToolUse hook: a `.env` read
  is blocked with `tq/env-file-read`.
- A `kimi` catalog entry exists with its config-dir variable, and `tq login <ws> kimi`
  writes under `~/.tentaqles/identities/<ws>/kimi`.
- `TestHookNeverAsks` (FR-10) covers the Codex agent: only allow or deny is emitted.

## 5. Data

| Data | Class | Source | Store | Retention |
|---|---|---|---|---|
| Workspace manifest `.tentaqles.yaml` | internal (names only; the loader rejects secret-shaped values) | `tq add`, `tq onboard`, `tq setup`, human edits | the client folder (versionable) | life of the workspace; the trust hash under `$TQ_HOME/trust` is dropped on edit |
| Harness and CLI logins | secrets | the CLIs' own login flows (`tq login`) | per-identity config dirs under `~/.tentaqles/identities/<ws>/<id>` | until the CLI logs out; never copied by tq |
| Project-management token | secrets | `tq secret set` (human) | OS keychain, service `tentaqles`, account `<ws>/<provider>` | until `tq secret rm` |
| `.env` values | secrets | the project | the project; read only through `tq dotenv run`, masked in output | never stored by tq |
| Bundle catalog | internal | the human | `$TQ_HOME/bundles/catalog.yaml` | life of the install |
| Memory (`memory.db`), vault export, knowledge graph | internal; secret patterns masked at every write | sessions, hooks, ingest | per-identity plugin data; the Obsidian vault folder; `meta.db` holds only names, stats and signals | decay and compaction; sessions kept [placeholder: days] |
| Client repos' personal data | the client's | the client | the client's systems; read locally | tq v0.9.0 keeps no personal-data rule of its own (the exit rules left with D-29); memory writes mask secrets only |
| Audit log | internal (commands masked) | `tq env` | `$TQ_HOME/audit.jsonl` | [placeholder: no retention policy today] |
| Migration journal and backups | internal | `tq migrate` | `journal.json` and `.tq-backup` files beside the migrated files | until `tq uninstall --restore` |
| Telemetry | none | | tq sends none | |

## 6. Security

The threat model is `docs/threat-model.md` (2026-10-31; next review 2027-01-28); v0.9.0
rewrites it to the four boundaries below. It is the checklist the `security-reviewer`
reads on every diff.

- Boundaries (one STRIDE table each): B1 agent session to tq hooks and `$TQ_HOME`; B2
  human to the human-only actions (trust: `tq allow`, `tq deny`; secrets: `tq secret`
  writes, logins; the no-tq launchers); B8 GitHub releases and repo to the user's machine
  (installers, tq binary, plugin); B9 contributors and CI to the repo and its releases.
  B3 to B7 and B10 of v1 leave with the build gate, the packs, the hub, the trackers, the
  decide endpoint and the console.
- Authentication: human-only actions refuse any session that carries an agent marker
  (`CLAUDECODE`, `CLAUDE_PLUGIN_ROOT`, the Codex sandbox markers) and `tq secret` writes
  also need a terminal; manifests are trusted by SHA-256 hash; release artifacts carry
  checksums and GitHub build provenance; the plugin installs from a tag pinned to a sha;
  bundle sources over plain-text or git transports are refused.
- Roles: see §2. Agents act under the workspace's identity and may use `.env` values only
  through `tq dotenv run`; only humans trust, store the credential, log in, start a no-tq
  session and release; a nested agent inherits everything and loosens nothing.
- Abuse cases the hook is built for: an agent steered by text it read pushes under the
  wrong account, runs a cloud command for another client, reads `.env` or the keychain,
  prints the environment, commits a secret, adds another client's MCP server, or trusts a
  manifest it edited. Out of scope (accepted risks): a program already running as the
  user, which can read the keychain and `$TQ_HOME` and drive a pty (AR1); a determined
  adversary rewording shell commands past the text filters, and a hook timeout or parse
  error that lets a call through (AR2). Both accepted by the repo owner on 2026-10-30
  until 2027-01-28.
- Known gaps the model records: the guards are text filters (AR2); the off launchers check
  markers but not a terminal (B-01); the audit log has no hash chain (AR1); the installers
  are served from mutable `main` (B-09); `tq decide`'s AR3 and TM-7 are closed by the cut.

## 7. Non-functional requirements

- Platforms: Windows 11 first (cmd.exe, PowerShell 5.1, pwsh, Git Bash), then macOS and
  Linux (WSL2 included). Go 1.26.x; Python 3 for the plugin (stdlib plus pyyaml, pathspec,
  numpy; fastembed optional). No Node.
- Hooks: every hook declares a timeout (`plugin/hooks/hooks.json`: PreToolUse 15 s,
  SessionStart 30 s, 15 s and 30 s, UserPromptSubmit 5 s, PostToolUse 10 s async, Stop
  15 s and 10 s, SessionEnd 45 s); capture-only hooks are async; nothing in a hook waits
  on the network or a model; a stalled hook is a P0.
- Guard latency: the identity guard runs one git config read and a bounded gh lookup
  (capped at 5 s); no rule waits on a model or a network service.
- Tests: `go test ./...` green on Windows, macOS and Linux CI; the required checks on
  `main` are the three Go OSes, `plugin`, `marketplace-pin` and `govulncheck`; the fuzz
  workflow runs on demand.
- Offline: everything except provider logins and the plugin's optional model calls works
  offline (not separately tested [placeholder]).
- Size: one binary, one plugin; no daemon, no server, no database in tq.

## 8. Data model sketch

A **base folder** (`tq init`) holds **workspaces** (client folders), each with one
**manifest** and several **identities** (one per CLI or harness login, each a config
directory under `~/.tentaqles/identities/<ws>/<id>`). A workspace's manifest is **trusted**
by hash. The manifest blocks that remain in v0.9.0:

- `schema` (`tentaqles-client-v1` or `v2`), `client`, `display_name`, `language`;
- `git` (name, email, provider, user, host, expected_user);
- `cloud` (the primary provider, `also` for extra ones, expected account or subscription);
- `database`; `data_platform`; `chat`; `email` (each a provider name and nothing else);
- `project_management` (credential only: the provider; the token lives in the keychain;
  replaces v1's `tracker:` block, whose `lanes`, `assigned_to` and sync keys go);
- `stack` (a list; FR-05 may extend it);
- `claude` (`permission_mode`, `bundle` with marketplaces, plugins, skills and mcp,
  `permissions` with allow, deny and ask, `env`);
- `blocked_commands` (the manifest's own blocked list, enforced by `blocked-command`);
- `agents` (the harness CLIs to provision);
- `identities` (one entry per CLI login; `tq add` derives it from the chosen providers,
  `claude` and `gh` by default).

Removed: `decision`, `guard`, `tracker`, `packs`, `data`, and the `unattended` block.

The **state folder** `$TQ_HOME` (`~/.tentaqles`): `identities/<ws>/<id>/` (config homes and
the plugin data of each identity, including its `memory.db`), `trust/` (one hash file per
trusted manifest), `bundles/catalog.yaml`, `audit.jsonl`, the registered bases
[placeholder: file name], and the plugin's global `meta.db`. Removed: `hub/`, `packs/`,
`guard/`, `build-gate/`, `guard.yaml`.

## 9. Stack and constraints

- `tq`: Go (1.26.x), cobra CLI, go-keyring for the OS keychain; no cgo; the pure-Go SQLite
  dependency leaves with the hub [placeholder: unless a surviving doctor check reads
  `memory.db` directly].
- Plugin: Python, runtime bootstrap through `tq_run.sh` and `tq_env.sh`; the workflow
  package shrinks to `prd.py` (stdlib only); pyyaml, pathspec, numpy required; fastembed
  optional and never installed in CI, so it stays optional.
- Releases: goreleaser, Homebrew tap and Scoop bucket by deploy keys, GitHub attestations,
  actions pinned by sha, Dependabot without major action bumps, `govulncheck` required.
- Constraints that hold everywhere: credentials never in manifests; hooks never stall;
  trust, secret writes and logins are human-only; the hook allows or blocks and never asks
  (D-31); tq sets no permission mode of its own beyond the launcher's skip (D-30); `off` is
  never stored in a file; one memory store per workspace, never joined; no automatic
  secret rotation; nothing copied from source kits.

## 10. Open questions

None. The cut of 2026-11-03 (D-29 to D-31) answered every question v1 left to a release;
a new question goes here as `- NEEDS-DECISION: ...` and makes the PRD not planning-ready
until it is settled.

## 11. Releases

| Release | FRs | Done when |
|---|---|---|
| v0.8.2 and earlier | FR-01 to FR-24 as shipped, plus the removed surfaces, see CHANGELOG | Shipped; v0.8.2 stays as a tag and is the last full release |
| v0.9.0 | FR-01 to FR-24 | The cut is merged on `main`: the removed commands, rules, skills, agents, docs and manifest blocks are gone, the plugin is 0.16.0, and every acceptance check of these FRs passes |
| v0.10 | FR-25 to FR-31 | Isolation completeness: prompt and title indicator, shim folders and CLI provisioning, browser profiles, the onboarding prompts and terminal close, the banner, OpenCode then Kimi; every acceptance check of these FRs passes |

## 12. Prioritized backlog

Priority: P0 closes a security gap or makes the cut consistent; P1 completes isolation,
the product's core promise; P2 depth; P3 follow-ups with no dependency on the rest.

| # | Priority | Item | FR | Why this priority | Depends on |
|---|---|---|---|---|---|
| B-01 | P0 | Plain `tq allow` and `tq deny` refuse agent sessions in the binary; the `off` launchers (`claude-off`, `codex-off`, `tq run --off`) need a terminal as well as no agent marker | FR-11, FR-12 | The one human-only gap v1 found that survives the cut (old B-03 without the mode and `tq guard` parts); cheap | D-10 as reduced by D-29 |
| B-02 | P0 | Doc fixes: rewrite COMMANDS.md, CLAUDE-HOOK.md, GUARD-SCOPE.md, the root and plugin CLAUDE.md and the threat model for the slim command list; fix `tq login` and `providers/`; mark `docs/design/` as v0.8.2 history | FR-24, FR-16 | Operators act on the docs; every removed surface is still described today (old B-05) | the cut PR |
| B-03 | P1 | Browser profile per workspace: Chrome and Edge first, for the onboarding login pages and Claude in Chrome or Playwright | FR-27 | Named in the vision's first paragraph; the last identity surface not isolated (old B-11) | decided (D-06) |
| B-04 | P1 | Shim folder per workspace so unselected CLIs are unreachable; CLI install or link into it | FR-26, FR-30 | The vision's first bullet; also what makes "AWS-only terminal" true (old B-12) | a Windows prototype (D-05 decided) |
| B-05 | P1 | Prompt prefix and window title; the onboarding banner with `--no-banner` | FR-25, FR-29 | Low effort, high clarity (old B-13) | decided (D-01) |
| B-06 | P1 | Onboarding prompts: browser, Other with free text, multi-select data platforms, resume after a failed step, close the originating terminal after the new one opens | FR-28 | Completes the wizard (old B-14) | B-03 for the browser prompt to mean something (D-07 decided) |
| B-07 | P2 | OpenCode hook, launcher and catalog entry; then Kimi Code once its hook support is checked | FR-31, FR-03 | The vision lists four harnesses (old B-19) | decided (D-11) |
| B-08 | P2 | Stack preferences resolved per workspace; manifest editing with re-trust (`tq edit`, GUI parked) | FR-05, FR-01 | Completeness of the stack block and the `tq add` "already exists" dead end (old B-23) | none |
| B-09 | P3 | Signing `trust/`, `config.yaml` and `audit.jsonl`; installers served from a tag instead of `main` | FR-16, FR-21 | Threat-model follow-ups; AR1 and AR2 accepted until 2027-01-28 (old B-26) | none |

## 13. Decisions record

Decisions of v1 that still hold keep their ids and outcomes; D-29 to D-31 are the cut.

| # | Decision | Outcome |
|---|---|---|
| D-01 | Identity indicator and banner | Prompt prefix `[client]` plus the window title in every shell, `[off]` after `tq off`; the banner shows once, at onboarding |
| D-04 | Credential store | Per-identity config dirs plus the OS keychain with per-workspace names is the design; "trust store" means manifest trust |
| D-05 | Unselected CLIs and provisioning | A shim folder per workspace first on PATH, removed by `tq off`; global-only tools recorded in the manifest |
| D-06 | Browser profiles | Chrome and Edge first, for tq's pages and for Claude in Chrome / Playwright; Safari and Opera later |
| D-07 | Terminal after onboarding | Close the originating terminal, only after the new one opened; on failure keep it and print the failing step |
| D-08 | Onboarding skill | Logins and `tq secret` stay human-only; the skill prints and verifies, and later opens login pages in the client's browser profile |
| D-11 | Harness scope | OpenCode next, Kimi Code later; Codex best effort |
| D-21 | Knowledge base | Docs folders and the tracker first |
| D-29 | The cut (2026-11-03) | Board, packs, build gate, Jev, insights, guard modes, personal-data rules, data classes, desktop and templates removed; last full release v0.8.2 |
| D-30 | Permission modes (2026-11-03) | tq sets no permission mode of its own beyond the launcher's skip; Claude Code's settings decide confirmations |
| D-31 | Guard scope (2026-11-03) | No guard beyond identity and secrets: the hook blocks identity drift and secret exits and never asks |

Superseded by D-29: D-02 and D-03 (board storage and cross-client hub views; the board is
gone and the plugin dashboard is the only cross-workspace view), D-09a to D-09c (mode
names, missing categories, locked groups), D-10 (reduced to B-01: plain `tq allow` and
`tq deny` refuse agent sessions and the `off` launchers need a terminal), D-12 to D-15
(headless asks, PII under bypass, lanes, card text conflicts), D-16 (the PRD `accept`
command; planning-ready stays the grade), D-17 to D-20 (control plane, answering decisions,
evals, per-project gates), D-22 and D-23 (notifications, the visual review gate), D-24 to
D-28 (session classes, the console, `!` commands, the terminal-only list, card numbers,
budgets). The spec-level decisions of v1 §17 (the development-process design and specs A
to E) describe v0.8.2 and are history; the standing rules among them that survive are
restated in §9 (no secret rotation, hooks never stall, one memory store per workspace,
credentials never in manifests, the launcher's permission skip).
