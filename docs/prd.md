# Tentaqles: product requirements

Status: planning-ready (the 23 decisions of 2026-10-30 and the 3 of 2026-10-31 are settled; outcomes in §17)
Owner: the repo owner (reach@tentaqles.ai)    Updated: 2026-10-31
Baseline audited: `main` f66b3e9, tq v0.8.2, plugin 0.15.2

This document replaces the draft at `Tentaqles — Product Requirements Document.md`
(repo root, 2026-10-30, untracked; called "the original PRD" below) as the source of
truth for what Tentaqles is, what is built, and what is still missing. It was written by
reading the original PRD, `docs/IDEATION.md` (the owner's vision note), every document
under `docs/`, and the code under `cmd/`, `internal/`, `plugin/` and `desktop/`.

How to read it:

- Every requirement is a `### FR-nn` in section 4 with a **Status** line: `done`,
  `partial` or `not started` (37, 22 and 9 of the 68 on 2026-10-30). A part decided
  against is marked **Dropped** inside its FR with the reason (one today: the build-gate
  key move, FR-27). Each FR cites its evidence (a file, command, skill or test) and lists
  what is missing. Acceptance checks are binary; for shipped work the check is the test
  that pins it.
- Where the original PRD and the code disagree, the FR says **CONFLICT** and names the
  decision (`D-nn`) the repo owner took on 2026-10-30; the outcomes are in §17. Nothing
  was resolved silently, and section 10 is empty because every question was answered.
- `[placeholder]` marks a detail neither the documents nor the code confirm.
- Names follow the code, not the original PRD. The mapping is in section 15 (the
  original PRD's own discovery rule asks for exactly this).
- Section 12 maps every section of the original PRD to its FRs. Section 13 is the
  prioritized backlog of what is missing. Section 14 lists every conflict and flag.
  Sections 16 and 17 index the runbooks and the recorded decisions.
- Statuses were verified on 2026-10-30 by reading code and tests, not by running the
  product end to end against live services. Live-service claims (tracker APIs, cloud
  plans, restore drills) are marked as such.

Tooling: `plugin/scripts/prd-status.py` grades this file (planning-ready means section 10
is empty and every FR has a check); `/tentaqles:plan-cards` turns a release's FRs into hub
cards; `/tentaqles:direction` audits the repo against it.

## 1. Problem and evidence

A developer who works for several companies or clients at once shares one terminal, one
set of CLIs, one AI-harness login and one browser profile across all of them. Wrong-account
pushes, cross-client secret exposure and manual re-login are routine, and an AI coding
agent makes each of those slips faster and at scale. The same developer also wants the
agent to pull assigned work from each client's board, run tasks through a trustworthy
build process, and hand back results for review, without any client's data, credentials or
sessions leaking into another's.

Evidence, from the repo's own history:

- The owner's vision note (`docs/IDEATION.md`) names the daily failure modes: one terminal
  and one login for every client, and the need for per-client Claude Code accounts because
  two clients each supply their own subscription.
- `docs/TEST_RESULTS_RUN01.md` (2026-10-29) recorded an install where two identities shared
  one plugins folder and ran plugin 0.11.1 against a current 0.15.0, and where `tq doctor`
  scanned agent worktrees and produced 185 false personal-data hits; fixed in PRs #51–#53.
- `docs/threat-model.md` (2026-10-30) records that the main adversary is the agent itself,
  steered by text it reads or simply mistaken, and the second is the supply chain.
- The development-process spec (`docs/design/2026-10-20-development-process-design.md`)
  states the quality problem: a card's "done" is only trustworthy if a gate, not the
  builder, proves it.

## 2. Users and roles

| Role | Who they are | What they need from this | Can | Cannot |
|---|---|---|---|---|
| Developer (the human) | One freelance or multi-employer developer, one machine; Windows first, then macOS and Linux | One folder per client that is logged in, tooled and guarded for that client only; a board of assigned work; agents that run tasks and ask when they must | Trust manifests, grant modes, approve, answer decisions, store tokens, push cards to the company board, release | Be impersonated by an agent: every one of those actions refuses an agent session |
| Agent session (Claude Code; Codex best effort) | An interactive harness started from a workspace folder | Identity preamble, memory, skills, a guard that blocks slips with a reason it can act on | Read and edit the project, add and comment on cards, ask for approvals, run gated builds | Change its own mode or trust, read `.env`, use another client's credential, answer decisions, push to the tracker |
| Headless run (orchestrator) | `claude -p` rounds started by `tq hub` for one card | Task text, budgets, a worktree, decision records the human answers | Everything an agent session can, under `guard.headless_mode` | Change the mode, loosen a nested agent, answer its own decisions |
| Console session | The human's interactive top-level `claude` in a workspace terminal (class `console`, D-24) | `/tentaqles:start`, `sync`, `refine`, `run` and `hub` instead of typed tq commands; decisions as multiple-choice questions | Answer decisions, start, move, edit and refine cards, push and retry sync, as the human's proxy after a question the human answered | Change mode or trust, bind the hub off loopback, read hub keys, pass its class to a nested agent |
| Company (client) | The client's tracker, git host, cloud and chat | Only the developer's assigned items mirrored; only approved writes back | Receive lane moves, result comments, pushed cards (per outbox policy) | See other clients' data or any local-only card |
| Maintainer / releaser | The repo owner shipping tq and the plugin | One tag releases everything, pinned and verifiable | Merge with green checks, tag, pin the marketplace | Move a release tag; merge red |

## 3. Goals

- G1 **Isolation.** From any client folder, every git, cloud, harness and tracker action
  runs under that client's identity, and a command that would use another client's
  credential is blocked. Measure: the identity guard blocks 100% of the drift cases in
  `internal/guard/guard_test.go` and `tq doctor` reports no drift on a configured machine.
- G2 **Guarded autonomy.** The developer can let an agent skip confirmations for routine
  work while identity, secrets, self-protection and privacy stay enforced, and can see
  afterwards what was skipped. Measure: the four locked groups are never loosened in any
  mode (`TestLockedNeverChange`), and `tq mode log` lists every softened decision.
- G3 **Board to review, end to end.** An assigned card can become a project, be refined,
  run headless through gated checkpoints, and come back as a review page the developer
  approves or sends back, with the company board updated. Measure: the end-to-end hub test
  passes with a fake Claude (`TestHubEndToEndWithFakeClaude`), and one real card completes
  the loop against a real tracker (human check, not yet recorded; see FR-58).
- G4 **Trustworthy "done".** No checkpoint passes without locked tests, a recorded test
  run against the exact code, and an independent review; approvals are signed and
  rollbacks are refused. Measure: the build-gate tests in `plugin/tests/test_build_gate.py`.
- G5 **Safe distribution.** Every install is checked against the release's checksums and
  build provenance, and the plugin installs from a tag pinned to a commit. Measure: the
  installers refuse a tampered archive (verified 2026-10-30), `gh attestation verify`
  passes for every v0.8.2 asset.

### 3.2 Non-goals

- Multi-user or team sharing of identities: one developer, one machine (original PRD §14;
  hub spec A §2 lists multi-user access as deferred, not planned).
- Hosting the board, the review page or any state on a remote service (A §15).
- Replacing the harness's own permission system; the guard is a layer on top (E §1).
- Defending against a program already running as the developer's OS user (threat model
  AR1), or against a determined adversary rewording shell commands (AR2). The guards catch
  slips and prompt-injected mistakes.
- Automatic secret rotation (development-process spec §2; standing rule).
- Replacing CI (DP §2).
- Mixing clients' data: no query, table, export or model call ever joins two
  workspaces. Seeing clients side by side is allowed (D-03, 2026-10-30): `tq hub serve
  --all` (a switcher plus a combined waiting list over one database per workspace) and
  `/tentaqles:dashboard` stay; the original PRD's "no cross-identity views" is narrowed
  to this.

## 4. Functional requirements

Each FR has a **Status** line, **Evidence**, **Missing** (or **CONFLICT**), and binary
**Acceptance checks**. Groups A to K follow the product's shape, not the original PRD's
order; §12 maps the two.

### Area A: workspaces and identity isolation

### FR-01 Register client folders and create a workspace with a trusted manifest
Why: G1. Everything else keys off the folder and its manifest.
Status: done
Evidence: `tq init <base>` registers a base folder (`internal/cmd/init.go`); `tq add
<name>` scaffolds the workspace, `.tentaqles.yaml` (schema `tentaqles-client-v1|v2`),
identity folders and the git identity (`internal/workspace/scaffold.go`); `tq allow <ws>`
pins the manifest by SHA-256 under `$TQ_HOME/trust` (`internal/trust`); any edit untrusts
it and drops its mode grant. The manifest loader rejects any value that looks like a
secret.
Missing: `tq add` refuses an existing manifest with "already exists" but does not point to
an edit command; there is no `tq edit` for manifests (the original PRD §4.1 asks for one).
**Acceptance checks**
- `TestHashChangesWhenFileChanges` and `TestPlainAllowAfterEditDropsTheGrant` pass.
- Running `tq add <name>` in a folder that already holds `.tentaqles.yaml` exits non-zero
  without writing.
- `tq list` shows the workspace with `TRUSTED true` only after a human ran `tq allow`.

### FR-02 The identity follows the folder in every supported shell
Why: G1. Opening a terminal in a client folder must yield that client's environment.
Status: partial
Evidence: `tq activate <shell>` prints the hook; templates for bash, zsh, fish,
PowerShell, pwsh and cmd in `internal/shell/hooks/*.tmpl`; the hook runs `tq env`, which
applies `envplan.Desired` (`internal/envplan/plan.go`): `TQ_WS`, `TQ_WS_ROOT`, the git
identity and one config-dir variable per identity. `tq hooks install|remove|status`
manages the profiles. `tq run <ws>|. -- <cmd>` applies an identity without `cd`.
`TestHook_Golden`, `TestDesired_SharedCLIVariableKeepsTheFirstIdentity`.
Missing: only environment variables change: PATH, shell profile and tool discovery are
untouched (see FR-06). cmd.exe has no automatic per-prompt hook (call it after `cd`, or
use `tq run`). Git Bash is served by the bash template [placeholder: not confirmed on a
Git Bash profile].
**Acceptance checks**
- In a trusted workspace, `git config user.email` prints the manifest's email and
  `$env:CLAUDE_CONFIG_DIR` points under `~/.tentaqles/identities/<ws>/claude`; in a
  neutral folder both are unset (COMMANDS.md §4 step 3).
- `TestHook_Golden` passes for every shell template.

### FR-03 An unambiguous identity indicator in the terminal and the harness
Why: G1. The developer must see which client a window acts for.
Status: partial
Evidence: `tq statusline` (`internal/statusline`) shows the client and the mode in the
Claude Code status bar, including `tq: off`. The identity preamble prints at session start
(`tq claude-hook session-start`).
Missing: the shell templates do not change the prompt or the window title; the original
PRD §3.1 and §4.2 ask for a prompt prefix or title, and §13.2 for a visible "raw" marker.
Decided (D-01): a short `[client]` prompt prefix and the client name in the window
title in every supported shell, `[off]` after `tq off`; the banner (FR-11) shows once, at
onboarding. TEST_RESULTS_RUN01 reported no preamble visible in one session; cause
unconfirmed.
**Acceptance checks**
- A new terminal opened in a trusted workspace shows the workspace name in the prompt or
  the window title before the first command.
- A window after `tq off` shows an "off" marker in the same place.

### FR-04 One harness login and config per workspace
Why: G1. Two clients that each supply a Claude Code subscription must each be logged in to
their own account.
Status: partial
Evidence: catalog entries redirect the config home per identity: Claude
`CLAUDE_CONFIG_DIR`, Codex `CODEX_HOME`, OpenCode `OPENCODE_CONFIG_DIR` and
`XDG_DATA_HOME` (`internal/providers/catalog/{claude,codex,opencode}.yaml`), under
`~/.tentaqles/identities/<ws>/<id>`; `tq login <ws> <id>` logs in there. Extra agents in
the catalog: gemini, cursor, copilot, goose, cline, continue. The global install is never
written; only the variable is redirected.
Missing: Kimi Code is not in the catalog (original PRD §3.2 lists it); decided (D-11):
it comes after OpenCode, once its hook support is checked. OpenCode has a catalog entry
but no hook or guard (see FR-28). No test proves the global `~/.claude`
stays untouched [placeholder].
**Acceptance checks**
- Starting `claude` in workspace A and in workspace B writes credentials under two
  different `CLAUDE_CONFIG_DIR` folders and leaves `~/.claude` unchanged (byte-compare
  before and after).
- A `kimi` catalog entry exists with its config-dir variable [placeholder: after the
  OpenCode work of FR-28].

### FR-05 Per-workspace provider CLI configuration
Why: G1. git, cloud, data and tracker CLIs must each act under the client's account.
Status: done
Evidence: 62 catalog entries (`internal/providers/catalog/`): gh `GH_CONFIG_DIR`, glab
`GLAB_CONFIG_DIR`, az and azure-devops `AZURE_CONFIG_DIR`, aws `AWS_CONFIG_FILE` /
`AWS_SHARED_CREDENTIALS_FILE` / `AWS_PROFILE`, gcloud `CLOUDSDK_CONFIG`, databricks
`DATABRICKS_CONFIG_FILE`, snowflake `SNOWFLAKE_HOME`, plus docker, kube, helm, terraform,
pulumi, vercel, supabase, stripe and others. `tq providers list|show|check|add`.
`cloud.also` lets a trusted manifest declare extra clouds (PR #38).
Missing: no identity variable for Bitbucket, Slack, Teams, Gmail, Outlook, Jira, Linear or
Asana (chat and email go through MCP only; tracker tokens through FR-09). `az` and
`azure-devops` share one `AZURE_CONFIG_DIR`, so they share one login. Not in the catalog:
Google Colab, Discord, Zoom, Google Meet, Trello, GitHub Projects as its own entry
(GitHub Projects v2 is covered by the github tracker adapter). The top-level `providers/`
folder holds only a README; CLAUDE.md still calls it "(planned)".
**Acceptance checks**
- `tq providers check` lists every declared CLI with installed yes/no and version.
- `TestInlineIdentityOverride` and `TestWriteWorkspace` pass.
- A `gh auth status` run in workspace A names A's account and never B's.

### FR-06 CLIs not selected for a workspace are not reachable from its terminal
Why: G1. An AWS-only client's terminal must not resolve `az` (IDEATION.md, original PRD
§3.1).
Status: not started
Evidence: `envplan` edits no PATH and installs no shims. The guard blocks identity drift
(`tq/wrong-cloud`, `guard/env-drift`), which is a different property.
Missing: everything. Decided (D-05): a shim folder per workspace holding launchers for
the selected tools only, placed first on PATH by the shell hook and removed by `tq off`;
tools that only install globally are recorded in the manifest with their shim strategy.
**Acceptance checks**
- In a workspace whose manifest declares only `aws`, `Get-Command az` (PowerShell) and
  `command -v az` (bash) fail, while they succeed in a neutral folder.
- The same commands succeed again after `tq off` in that window.

### FR-07 Tech stack and preferences per workspace
Why: Original PRD §3.4: runtimes, linters, formatter configs and editor settings declared
in the manifest and resolved per identity; the scaffolder reads the preferred stack.
Status: partial
Evidence: `Manifest.Stack []string` (`stack:`); the plugin reads it
(`plugin/tentaqles/manifest/loader.py`) and `add-project` and `kickoff` use it for the
ADR and project CLAUDE.md. `language:` drives pt-BR/en in skills.
Missing: tq does not resolve runtimes, package managers, linters, formatter configs or
editor settings per workspace. The manifest has no field for them [placeholder: exact
field design].
**Acceptance checks**
- A manifest `stack:` value appears in the generated project `CLAUDE.md` of
  `/tentaqles:add-project`.
- [placeholder] a declared formatter config is applied in the workspace terminal and not
  outside it, once the field exists.

### FR-08 One browser profile per workspace, used by every URL open and browser automation
Why: G1, IDEATION.md. A terminal, harness or agent opening a URL must open the client's
Chrome, Edge, Safari or Opera profile.
Status: not started
Evidence: the only browser code is `openBrowser` in `internal/cmd/hub_service.go`, which
uses the OS default opener for loopback URLs. No manifest field, no profile handling, no
wiring for Playwright, Puppeteer or Claude in Chrome.
Missing: everything. Decided (D-06): Chrome and Edge first, for tq's own pages
(`tq hub open`, `tq hub review --open`) and for browser automation started from the
workspace (Claude in Chrome, Playwright); Safari and Opera later.
**Acceptance checks**
- The manifest holds a `browser:` block with the browser and profile name (shape
  [placeholder]).
- `tq hub open` and `tq hub review --open` start the declared profile (process arguments
  name the profile directory), not the default profile.
- A Playwright or Claude-in-Chrome session started from the workspace uses that profile
  directory.

### FR-09 Credentials live per workspace, never in shared dotfiles
Why: G1. A command issued from workspace A must not be able to use B's credential.
Status: done (wording conflict settled by D-04)
Evidence: CLI logins live in per-identity config dirs (FR-05); tracker tokens live in the
OS keychain under service `tentaqles`, account `<ws>/<provider>` (`tq secret set|rm|status`,
human-only, `internal/secret`; `TestSecretSetStatusRm`); `.env` values are used only
through `tq dotenv run`, masked in output (FR-25); pack ledger keys are Ed25519 in the
keychain (service `tentaqles-packs`); the hub holds no credentials (A §15).
**CONFLICT:** the original PRD §3.3 says credentials must never be "in the global OS
keychain under a shared key". The code uses the OS keychain with per-workspace account
names, which is what the OS offers; a same-user process can read any of it (threat model
AR1). Decided (D-04): this is the design; "trust store" in tq means manifest trust
(`tq allow`), and the original PRD's credential-store meaning is retired; see §15.
**Acceptance checks**
- `TestSecretSetStatusRm`, `TestChainNeverPrintsAValue` and `TestToolEnvIsFiltered` pass.
- `tq secret status <ws>` reports the token's presence without printing it.
- A `gh` or `az` call in workspace A with B's credential present on the machine uses A's
  config dir (process environment shows A's `GH_CONFIG_DIR`).

### Area B: onboarding, setup and migration

### FR-10 An onboarding wizard that provisions a workspace without further manual steps
Why: IDEATION.md and original PRD §4. One command in a new client folder yields a
provisioned identity.
Status: partial
Evidence: `tq onboard [<name>]` (`internal/cmd/onboard.go`, `onboard_wizard.go`) runs
`tq add` then a wizard: name, base, email, git name, display name, git provider (github,
gitlab, azure-devops, bitbucket), git user, primary cloud plus "also" (azure, aws, gcp,
digitalocean), data platform, agent CLIs (multi-select), tracker (github, azure-devops,
jira, linear, asana), chat (slack, teams), email (gmail, outlook); every prompt offers
Skip; `--from <answers.yaml>` is non-interactive; it opens a new terminal via `termopen`
and leaves the workspace untrusted until the human runs `tq allow`.
`TestOnboard_FullAnswersAndTerminal`, `TestOnboard_FromWritesValidManifestWithSkippedBlocks`.
Missing vs original PRD §4.3–4.5: prompts for browser and profile, permission mode and
preferred shell; "Other" with free text; options Trello, GitHub Projects, Discord, Zoom,
Google Meet, Colab, Kimi; data platform is single-select (PRD: multi); a `blocked_commands`
list and the human-only list are not written (the latter is built in); no resume after a
failed step; `--from` takes an answers file, not a manifest.
**CONFLICT, decided (D-07):** the original PRD §4.4 step 7 requires closing the terminal
the wizard ran in; the code refuses on purpose today. The owner chose to close it, only
after the new terminal opened successfully; if the new terminal fails, the old one stays
and prints the failing step. The original PRD's `tq init` is the code's `tq init` (base
folder) plus `tq onboard`/`tq add` (workspace); see §15.
**Acceptance checks**
- `tq onboard --from <answers>` in a new folder writes a valid manifest and exits 0
  (`TestOnboard_FromWritesValidManifestWithSkippedBlocks`).
- The wizard offers browser, mode and shell prompts, each with Skip, and writes the
  answers to the manifest [placeholder: field names].
- Re-running `tq onboard` in a provisioned folder exits non-zero and names the existing
  manifest.
- After the new identity terminal opens, the terminal that ran `tq onboard` ends; when
  the new terminal cannot be opened, the old one stays open and prints the failing step.

### FR-11 A startup banner in `tq onboard`, identity terminals and off sessions
Why: Original PRD §4.2: a large ASCII "Tentaqles" banner with version, folder and a mode
label, as an identity indicator; `--no-banner` and `TQ_NO_BANNER` for scripts.
Status: not started
Evidence: no banner code; `tq run --off` prints one line ("tq: off — main logins, no
guard").
Missing: the banner asset, the UTF-8 fallback and the flags. Decided (D-01): the banner
shows once, at onboarding, not on every terminal; the indicator is FR-03. Glyph set and
colors [placeholder: a single text asset the owner can replace].
**Acceptance checks**
- `tq onboard` prints the banner, then one line with the tq version and the working
  directory, before the first prompt; `tq onboard --no-banner` and `TQ_NO_BANNER=1` print
  neither.
- In a terminal that reports no UTF-8, the plain-text fallback prints with no replacement
  characters.

### FR-12 Install or link the selected CLIs into an identity-scoped location
Why: Original PRD §4.4 step 3 and IDEATION.md: the setup installs the CLIs and
dependencies for the selected providers.
Status: not started
Evidence: `setup.ToolCheck` (`internal/setup/toolcheck.go`) and `tq providers check` only
probe whether a CLI is installed; `tq pack doctor` prints install hints. The catalog
carries install hints per provider.
Missing: installation or linking. Decided (D-05): install or link into the workspace's
shim folder (FR-06); tools that only install globally are recorded in the manifest with
their shim strategy.
**Acceptance checks**
- After onboarding with `aws` selected on a machine without it, `tq providers check`
  shows `aws` installed, and the binary resolves only inside that workspace's terminal.
- A tool that only installs globally is recorded in the manifest with its shim strategy
  [placeholder: field].

### FR-13 The plugin and MCP set are pre-registered per identity
Why: Original PRD §5.1. The first harness session already has the Tentaqles plugin.
Status: partial
Evidence: a shared catalog (`$TQ_HOME/bundles/catalog.yaml`) of marketplaces, plugins,
skills and MCP servers; `tq bundle catalog|diff|sync|capture` materialize the manifest's
`claude.bundle` into the identity's Claude settings (`internal/bundle`); unsafe sources
are refused (`TestValidate_PlainTextTransport`, `TestValidate_GitURLTransport`); another
client's MCP server is blocked (`tq/cross-client-mcp`); the plugin itself installs from
the repo's root marketplace pinned to a release tag and commit (`TestRootMarketplacePin`;
`docs/RELEASING.md`). `TestBundleSync_RefusesUntrusted`.
Missing: Claude Code only; nothing for Codex or OpenCode plugins. `tq onboard` does not
run `tq bundle sync`; the human runs it after `tq allow`. No one-line install hint on
first harness launch where pre-registration is impossible [placeholder: unconfirmed].
**Acceptance checks**
- `tq bundle diff <ws>` prints no drift right after `tq bundle sync <ws>` on a trusted
  workspace.
- `TestBundleSync_RefusesUntrusted` and `TestRootMarketplacePin` pass.
- After `tq onboard` plus `tq allow`, the first `claude` session in the workspace lists
  `tentaqles@tentaqles` in `/plugin` without a manual install step.

### FR-14 An onboarding skill that connects and verifies every declared provider
Why: Original PRD §5.2: drive authentication per provider (OAuth device flow, then PAT,
then MCP), store in the identity only, register the provider's MCP server, verify with a
read-only call, re-runnable, never prompts for undeclared providers.
Status: partial, different design
Evidence: `plugin/skills/onboard/SKILL.md` reads the manifest, prints the `tq login`,
`tq secret set` and `acli` lines for the human to run, guides `/mcp`, then verifies with
`tq doctor --json` plus read-only calls; `check` re-runs the verification; undeclared
blocks are skipped. `tq doctor` service checks: tracker-ok/needs-session/no-adapter/
auth-failed, chat-ok/mcp-missing, email-ok/mcp-missing.
**CONFLICT:** the skill never runs a login or touches a token; logins and `tq secret` are
human-only by design (B §8). The original PRD has the skill drive the auth. The tracker's
credential order is cli, keychain, env, MCP (B §7), not OAuth, PAT, MCP. It does not open
the identity's browser profile (FR-08) or register MCP servers (the human adds
`claude.bundle.mcp` and runs `tq bundle sync`). Decided (D-08): logins and `tq secret`
stay human-only and the skill keeps printing them; once FR-08 exists it opens each login
page in the workspace's browser profile, still never touching a token.
**Acceptance checks**
- `/tentaqles:onboard` on a workspace with an unreachable tracker reports `tracker-*`
  as failed and names the fix; with every service reachable it reports each as ok.
- The skill never executes `tq secret set` or a login itself (grep of the skill shows
  them only inside printed instructions; `test_hub_skill_prints_the_answer_command_for_the_user`
  is the analogous pin for `hub`).

### FR-15 Machine setup from a plan, and a desktop setup app
Why: Setting up several clients on a new machine in one pass.
Status: done
Evidence: `tq setup --example|--from <plan> [--dry-run|--yes]` scaffolds several
workspaces and prints the login lines (`docs/SETUP.md`); the Wails desktop app
(`desktop/`, `docs/DESKTOP.md`) wraps the same `setupapi` with steps Welcome, Work
folder, Companies, Services, Tools, Bundles, Preview, Log in, plus Board and Guard tabs.
Missing: the desktop installers are unsigned (users click through OS warnings); CLAUDE.md
still calls `desktop/` "(planned)".
**Acceptance checks**
- `tq setup --from <plan> --dry-run` prints the plan and writes nothing; with `--yes` it
  creates every workspace listed.
- The desktop job in `.github/workflows/release.yml` produces an installer per OS and
  `gh attestation verify` passes on each (true for v0.8.2).

### FR-16 Adopt a hand-built setup, and undo it
Why: Existing users had their own dotfile and shim setups before tq.
Status: done
Evidence: `tq migrate` (plan, then `--apply`) with a journal and byte-exact `.tq-backup`
files; `tq uninstall --restore [latest]` replays the journal backwards and resumes an
interrupted restore; `TQ_ENABLED=0` stops shell activation (`docs/MIGRATE.md`).
`TestRecordNeverLeavesJournalAbsent`, `TestApproveMigrateFlow`.
Missing (documented rough edges, MIGRATE.md "Known rough edges"): PATH shims survive
removing AutoRun; backup sidecars survive a restore; a failed apply leaves a partial
migration; `--force` can leave a half-copied credential store; `uninstall` is not an
uninstaller of tq itself.
**Acceptance checks**
- `tq migrate` without `--apply` changes nothing (diff of `$HOME` before and after is
  empty).
- After `tq migrate --apply` then `tq uninstall --restore latest --yes`, every file the
  journal names is byte-identical to its backup.

### Area C: the guard, permission modes and the no-tq escape

### FR-17 Every agent tool call passes through tq's guard
Why: G1, G2. The guard decides allow, ask or block for each tool call with a reason the
agent can act on.
Status: done
Evidence: plugin PreToolUse hook (`plugin/hooks/hooks.json`) runs `tq claude-hook
pre-tool-use` for Bash, PowerShell, Read, Edit, Write, Grep, Agent and `mcp__*` tools;
`guard.Decide` (`internal/guard/guard.go`) plus the policy rules (`internal/policy`);
exit 2 blocks, JSON `permissionDecision: ask` asks; 13 categories and 75 rule ids
(`internal/guardcfg/categories.go`; `TestEveryRuleHasCategory`,
`TestCategoriesClassifyEveryBuiltinAndAreValid`). Codex gets the same hook through
`$CODEX_HOME/hooks.json` with `--agent codex` (`internal/codexcfg`), where every ask
becomes a deny (`TestPreToolUseAgentCodexAsksDeny`). A guard panic becomes an ask in
Claude and a deny in Codex (`TestPreToolUseGuardPanicAsks`). Hook timeouts are 45/30/15/30 s
and the gh lookup is capped at 5 s.
Missing: the rules are text filters over shell languages, so rewordings (`env git`,
`sh -c`, globs, escapes, interpreters) get past them; documented in `docs/GUARD-SCOPE.md`
and `docs/CLAUDE-HOOK.md` and accepted as AR2 in the threat model. A hook timeout or a tq
parse error fails open. With neither tq nor Python installed every shell command is
blocked.
**Acceptance checks**
- The six simulated calls of `docs/COMMANDS.md` §4 step 6 give the listed results in
  relaxed and in default mode.
- `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`, `TestHookHubStoreWrite`,
  `TestPreToolUseGuardPanicAsks` pass.

### FR-18 Guard modes: default, relaxed, strict, custom, and off per launch
Why: G2. IDEATION.md items 2 and 3: an "unattended" mode that skips everything but the
locked groups, and the earlier ask/deny behaviour, plus customizable modes.
Status: done for behaviour; rename and two cell changes planned (D-09a)
Evidence: `internal/guardcfg/presets.go` (`TestPresetsMatchSpecTable`): `default` (each
rule's own action), `relaxed` (skips every unlocked category except git-history and
system, which ask), `strict` (default plus deny prod-changes, cloud-destructive,
git-history; external-writes asks), `custom` (a base preset plus per-category values per
layer: global, workspace, project). `off` is never stored (FR-23). Any mode looser than
default needs a hash-keyed `tq allow --mode <ws> relaxed|custom` grant; without it the
mode is clamped to default. Precedence global, workspace, project, launch; the repo-level
file can only tighten. `tq mode` shows the table; `tq mode --json` is a stable interface
(E §16). Spec E replaced "unattended mode"; `tq unattended` remains a deprecated alias.
**CONFLICT, decided (D-09a):** the modes are renamed to the original PRD's names and
`guarded` is loosened: `off` → `bypass`, `default` → `supervised`, `relaxed` →
`guarded`, `strict` and `custom` stay; `guarded` skips git-history and system as well
(today they ask). The old names are accepted as aliases for one release with a
deprecation note; `tq mode --json` keeps both spellings for that release. This document
uses the current code names in evidence and the new names in checks. Unchanged by the
decision: three self-protection rules (`tq/guard-change`, `tq/pack-config-change`,
`tq/guard-error`) and two Jev secrets rules ask instead of deny; `custom` has no name and
no `tq mode create` (`tq mode edit` is the editor); locked categories cannot be set to
ask (D-09c).
**Acceptance checks**
- `TestPresetsMatchSpecTable`, `TestRelaxedLockedDeniesStillBlock`, `TestRunTqMode` pass.
- `tq mode set relaxed` on a workspace without a grant leaves `tq mode` reporting
  `default` with a "needs a grant" note (clamp).
- A repo `tq-packs.yaml` or project layer can only make a category stricter: a looser
  value is ignored and reported.
- `tq mode` lists `bypass`, `supervised`, `guarded`, `strict` and `custom`; `tq mode set
  relaxed` still works for one release and prints the new name.
- In `guarded` with a grant, `git push --force origin main` and a write under a system
  directory are skipped and appear in `tq mode log`, while every locked rule still blocks.

### FR-19 The locked groups are never loosened by any mode, override or file
Why: G2. "Otherwise an agent could widen unattended mode on its own" (IDEATION.md).
Status: done
Evidence: `guardcfg.Locked` = identity, secrets, self-protection, privacy
(`categories.go`; privacy was added at spec review, E §19 Q1); `never_disable.go` ignores
`guard.disable` for locked ids; `TestLockedNeverChange`, `TestRelaxedKeepsPrivacy`,
`TestRelaxedLockedDeniesStillBlock`.
Missing: nothing. Decided (D-09c): locked stays locked; a custom table cannot set a
locked category to `ask` (the original PRD §12.3 would have allowed it).
**Acceptance checks**
- In every preset and in a custom table that tries `identity: skip`, `tq mode` shows the
  four locked rows as `rule locked`.
- A `.env` read is blocked (`tq/env-file-read`, exit 2) in relaxed mode with a grant
  (COMMANDS.md §4 step 6).

### FR-20 Mode changes, trust and approvals are human-only
Why: G2, G4. An agent must not be able to grant itself a looser mode or approve its own
work.
Status: done, with gaps
Evidence: `requireHuman` (`internal/cmd/humanonly.go`) refuses when any agent marker is
set (`CLAUDECODE`, `CLAUDE_PLUGIN_ROOT`, `TENTAQLES_HEADLESS`, `TQ_HUB_TASK`,
`CODEX_SANDBOX*`) and requires a terminal on stdin and stdout; applied to `tq mode
set|toggle|override|edit`, `tq allow --mode|--global|--bypass`, `tq unattended on|off`,
`tq secret`, `tq hub answer`, `tq hub push`, `tq hub sync retry`, `tq pack baseline`,
`tq pack ml promote apply` (typed `yes`). In tool calls `tq/mode-change` and
`tq/human-answer` deny, `tq/guard-change` asks. `TestAllowModeIsHumanOnly`.
Missing: plain `tq allow <ws>` and `tq deny` are protected only by the `tq/guard-change`
ask, not by `requireHuman` (COMMANDS.md says human-only: doc mismatch); `tq guard
off|on|set` is not human-only (locked ids are still never disabled); `tq mode toggle` and
`override` have no typed confirmation (original PRD §12.3 asks for one); switching to
`off` checks agent markers only, no terminal (FR-23). Decided (D-10): close all of
these, P0. Decided (D-24, 2026-10-31): `tq hub answer`, `tq hub push`, `tq hub sync retry`
and the pack `apply` verbs also accept class `console` (FR-70); everything else here
stays terminal-only (D-26).
**Acceptance checks**
- `TestAllowModeIsHumanOnly` passes; in a Claude session, `tq secret status` exits 1 with
  "human-only" (COMMANDS.md §4 step 9).
- `tq allow <ws>`, `tq deny <ws>` and `tq guard off|on|set` run from an agent session are
  refused by the binary, not only asked by the hook.
- `tq off`, `claude-off`, `codex-off`, `tq run --off` and `tq mode toggle|override`
  require a terminal on stdin and stdout and a typed `yes`.

### FR-21 Permission categories the original PRD names that the code lacks
Why: Original PRD §12.1 lists 11 categories; the code has 13, of which 4 PRD categories
have no counterpart.
Status: not started (for the missing four); done for the rest
Evidence: code categories: identity, secrets, self-protection, privacy, git-history,
prod-changes, cloud-destructive, db-destructive, system, external-writes,
ci-supply-chain, hygiene, packs. Not in the code: filesystem-destructive (closest:
`tq/outside-path-write` under system), package-install, network-egress,
browser-actions. `prod-changes` matches by command type; the manifest has no production
tags (original PRD §12.1 row 5).
Missing: the four categories and their rules. Decided (D-09b): add all four, each
asking in `supervised` and skipped only under a `guarded` or custom grant; browser-actions
lands after FR-08; production environment tags reuse `envgate` (the cloud and db packs
already resolve environments through it).
**Acceptance checks**
- `tq guard list` shows at least one rule in each of filesystem-destructive,
  package-install, network-egress and browser-actions, each with a test in
  `internal/policy`.
- A recursive delete outside the project folder asks in default mode and is skipped
  only under a relaxed grant.

### FR-22 An `ask` with no human present never hangs: deny, record a decision, retry
Why: Original PRD §12.4 and §8.2. Headless runs must turn asks into decisions on the card
and resume after the answer.
Status: partial
Evidence: the hub classifies Claude's `permission_denials` as `VerdictPermission`
(`internal/hub/runner/classify.go`), writes an `allow-once|stop` decision on the card, and
on `allow-once` re-queues one round with `--settings {"permissions":{"allow":[tool]}}`
(`TestAllowOnceAppliesToOneRound`). Headless runs use `guard.headless_mode` (default
`default`), set by the hub through `TQ_MODE` (`TestLaunchEnvHeadlessMode`). A headless
definition-of-done stop writes a `build-gate ask` question mirrored to the card.
Missing: the loop is not guard-aware: the decision names the tool, not the tq rule id; no
test sends a real hook ask through it; whether a settings allow rule overrides a
PreToolUse hook ask is unverified (spec E assumption A1). Codex never asks (asks are
denies). Decided (D-12): make the loop rule-aware and prove the retry with a real hook
ask, P1.
**Acceptance checks**
- A headless round whose tool call the guard would ask ends the round, puts a
  `permission` decision naming the rule id on the card, and the run shows `waiting`.
- After `tq hub answer <card> <decision> allow-once`, the next round runs the same call
  without the ask, and a second call of the same rule asks again.

### FR-23 Run a harness or a window with no tq at all, for that session only
Why: IDEATION.md item 1 and original PRD §13: "pure claude code, pure terminal", without
losing the identity for new terminals in the same folder.
Status: done, with gaps
Evidence: `tq off` / `tq on` shell functions call `tq env --off|--on` and set `TQ_MODE=off`
for the window; `claude-off` / `codex-off` and `tq run --off -- claude|codex` start the
harness with the identity environment and config-home redirect removed
(`internal/envplan/off.go`), the main `~/.claude`/`~/.codex`, the plugin switched off and
every hook exiting 0 as its first statement (`plugin/scripts/tq_hook.sh`, `tq_run.sh`,
`internal/cmd/claudehook_off.go`). `off` is never stored, so a new terminal in the folder
is the identity terminal again. `TestRunOff`.
Missing vs original PRD §13: no plain-shell escape (`tq run --off` starts only claude or
codex; `tq off` changes the current window instead); the off launchers check agent
markers only, not a terminal (§13.2 asks human-only; decided D-10: require a terminal
and a typed confirm); no prompt or title marker besides the statusline `tq: off` and one
printed line (decided D-01: `[off]` in the prompt and title, FR-03). The name `tq raw` was not adopted;
`tq off` is the shell function (original §13.1 allows `tq off`). The optional persisted
toggle (§13.4) is deliberately absent.
**Acceptance checks**
- `TestRunOff` and `TestNestedAgentLoosened` pass.
- After `tq off` in window A, `claude` in A uses `~/.claude` (process environment has no
  `CLAUDE_CONFIG_DIR`) while a new window B in the same folder has the workspace's
  `CLAUDE_CONFIG_DIR`.
- `claude-off` invoked from inside an agent session, the console included, is refused.

### FR-24 A nested agent cannot start with looser settings than its parent
Why: G2. A session must not escape its mode by starting another agent.
Status: done (heuristic)
Evidence: `tq/nested-agent-loosened`, a locked deny rule (`internal/policy/builtin_agents.go`):
loosening or clearing `TQ_MODE`, `TQ_ENABLED=0`, `env -i` before an agent, reassigning
`CLAUDE_CONFIG_DIR` or `CODEX_HOME`, the off launchers, `--tq-mode off|relaxed|custom`,
loosened claude/codex flags; `TQ_MODE=default|strict` is compared category by category
with the parent; `tq run` from an agent refuses a looser mode and adds no bypass flag.
`TestNestedAgentLoosened`, `TestHookNestedAgentNeverLoosensAStrictParent`.
Missing: it is a regex heuristic (a script or `sh -c` evades it; AR2). Hub runs are not
tool calls, so the rule does not apply to them; the hub sets the mode itself (FR-49).
**Acceptance checks**
- `TestNestedAgentLoosened` passes.
- In a default-mode session, `TQ_MODE=relaxed claude -p "x"` is blocked with the rule id.

### FR-25 Secrets never reach the transcript, a commit or a tool's environment
Why: G1. IDEATION.md group 2.
Status: done
Evidence: `.env` reads and env dumps are locked deny rules (`tq/env-file-read`,
`tq/env-file-shell`, `tq/env-dump`, `tq/secret-store-read|shell`,
`tq/plan-read|commit|secret-print`); `tq dotenv run [--file F] -- <cmd>` loads a `.env`
and masks each value as `[REDACTED:NAME]` in the output (`TestDotenvRunLoadsAndMasks`);
`tq/commit-secret` scans staged diffs (`internal/commitscan`, `TestScanDiffLineNumbers`);
`tq secrets audit` finds secret-shaped values in agent config and committable `.env`
files, printing only path and line; external tools get an allow-listed environment with
secret-shaped values dropped (`TestToolEnvIsFiltered`); the manifest loader rejects
secret-looking values. Standing rule: no automatic rotation.
Missing: the rules are text filters (`python -c` reads are not seen); the env-dump rule
has false positives on read-only greps that mention "env" (noted during the 2026-10-30
audit).
**Acceptance checks**
- `tq dotenv run --file demo.env -- bash -c 'echo token is $DEMO_TOKEN'` prints
  `token is [REDACTED:DEMO_TOKEN]` (COMMANDS.md §4 step 8).
- `TestPolicy_ReadEnvDenied_WindowsPath`, `TestPolicy_PowerShellEnvDumpDenied`,
  `TestScanDiffLineNumbers` pass.

### FR-26 Personal data may be read locally; it never leaves through an exit
Why: Owner rule "read freely, guard the exits" (DP §1, §11.2). Client work needs the data;
memory, commits, pushes, PR text, fixtures and model calls must not carry it.
Status: done
Evidence: the manifest `data:` block drives the four locked privacy rules `tq/commit-pii`,
`tq/push-pii`, `tq/text-pii`, `tq/pii-fixture-dump` (`internal/cmd/claudehook_pii*.go`;
`TestCommitPII_DeniedWithCPFEvenWithoutDataBlock`, `TestPushPII_DeadlineAsks`); `tq redact`
masks emails, phones, CPF, CNPJ, cards, IBANs and secrets, inside a workspace only the
declared classes plus secrets; plugin exits (memory writes, vault, ingest, signals, Jev,
model prompts) pass `exit_classes(manifest)` and fail closed to the strictest set; an
allow marker works only inside test folders (`TestCommitPII_MarkerOnlyInsideTestDirs`);
scans that run out of budget ask instead of hanging. `/tentaqles:classify-data` writes the
block; `/tentaqles:privacy-review` reviews changes against LGPD and GDPR.
Missing: `tq decide` redacts secret patterns only, so personal data in the state it sends
is not masked (DECIDE.md); TEST_RESULTS_RUN01 reported locked PII rules logged as
auto-allowed under Claude's bypass permission mode, unconfirmed and still open.
**Acceptance checks**
- `printf 'contact tester@acme.com, card <a 16-digit test card number>\n' | tq redact`
  outside a workspace prints both values redacted (COMMANDS.md §4 step 8).
- `TestCommitPII_DeniedWithCPFEvenWithoutDataBlock` passes.
- A hook run with Claude's `bypassPermissions` still blocks `tq/commit-pii`, shown in a
  real session (decided D-13: verify and fix, P0).

### FR-27 tq protects its own state, signs what must not be forged, and documents its scope
Why: G2, G4. Self-protection is a locked group; a written threat model decides what the
guards must guarantee.
Status: done
Evidence: `tq/hub-store-write` protects `hub/`, `packs/`, `trust/`, `guard/`,
`build-gate/`, `guard.yaml`, `config.yaml`, `audit.jsonl` under `$TQ_HOME` for shell
commands and file tools (`TestHookHubStoreWrite`, `TestStateDirTqExemption`);
`tq/settings-widen`, `tq/settings-shell-write`, `tq/trust-store-write`,
`tq/guard-file-write` (`TestGuardProtectsItsOwnSettings`); pack ledger answers are
Ed25519-signed with a keychain key and single-use (`TestSignedAnswersOnly`,
`TestHighWaterMark`); build-gate records are HMAC-signed with signed generation records
(`test_an_older_signed_approval_copied_back_counts_as_rolled_back`); hub web tokens are
random and stored hashed; `docs/GUARD-SCOPE.md` and `docs/threat-model.md` (nine
boundaries, AR1, AR2, decisions) are on `main`; the "Must not trade off" list in
`CLAUDE.md` names the test behind each property.
Dropped: moving the build-gate HMAC key into the OS keychain (TM-3) was planned and then
dropped on 2026-10-30: the agent's own build steps must sign, so the key would gain little
over AR1 and AR2 (threat model, Decisions). Not re-proposed without new facts.
Missing: the audit logs have no hash chain (a same-user process can rewrite them, AR1);
signing `trust/`, `guard.yaml` and `config.yaml` is a possible follow-up, not planned.
**Acceptance checks**
- `TestHookHubStoreWrite`, `TestSignedAnswersOnly`,
  `test_an_older_signed_approval_copied_back_counts_as_rolled_back` pass.
- `docs/threat-model.md` §4 answers "yes" to every check and no accepted risk is past
  its expiry date (next review 2027-01-28).

### FR-28 Harness coverage: Claude Code, Codex, OpenCode, Kimi Code
Why: IDEATION.md names four harnesses; each needs the identity redirect, the launcher and
the guard hook.
Status: partial
Evidence: Claude Code: full (hook, launcher that always adds
`--dangerously-skip-permissions` so tq's mode is the single policy layer, E Q3; status
line; bundles). Codex: `CODEX_HOME`, `config.toml` block, `rules/tq.rules`, hooks file the
user trusts once in `/hooks`, flags `-a on-request -s workspace-write` or `-a never -s
<sandbox>`; every ask is a deny (E §12, best effort). OpenCode: catalog entry with config
dirs only, no hook or guard. Kimi Code: absent (E §15 lists it as future work).
Missing: OpenCode and Kimi hooks and launchers; a Codex hub adapter (A §2 deferred);
the launchers are shell functions in bash, zsh, fish and PowerShell, not in cmd.exe.
Decided (D-11): OpenCode next (hook, launcher, guard), Kimi Code later once its hook
support is checked; Codex stays best effort; Claude Code auto mode (`--enable-auto-mode`,
the original PRD's "guarded-auto") is not used: tq's modes stay the single policy layer.
**Acceptance checks**
- `TestPreToolUseAgentCodexAsksDeny` passes; `tq mode --agent codex` lists the rules that
  deny in Codex.
- An OpenCode session started from a workspace runs the tq PreToolUse hook: a `.env`
  read is blocked with `tq/env-file-read`.

### FR-29 What the guard skipped or softened is logged and reportable
Why: G2. Relaxed mode is only acceptable if the developer can see what it let through.
Status: done
Evidence: `tq mode log [--since]` reads `guard/mode.jsonl` (`internal/guardlog`;
`TestModeAuditsMCPContent`): one row per confirm skipped or block softened, with scope,
mode, rule ids, tool and command (masked); the session preamble counts them ("47 confirms
and 2 blocks skipped in 24h"); `tq insights` reports guard denies and asks per rule, hook
latency, cost and tokens per identity, Jev stats, build-gate and hub stats as stable JSON
(`tq.insights/v1`, `docs/INSIGHTS.md`); `audit.jsonl` records identity switches.
Missing: INSIGHTS.md lists two unmeasured targets: time from PRD to merge, and hotfix debt.
**Acceptance checks**
- After the step-6 simulated calls in relaxed mode, `tq mode log --since 24h` shows one
  row per softened decision (COMMANDS.md §4 step 7).
- `tq insights --json` validates against `tq.insights/v1` [placeholder: schema location].

### Area D: the developer's board (`tq hub`) and tracker sync

### FR-30 One local board per workspace, with cards, lanes, decisions, runs and activity
Why: G3. The "DCB" of IDEATION.md: the developer's own mirror plus local-only cards.
Status: done, with a storage CONFLICT
Evidence: `tq hub serve` (default `127.0.0.1:7470`; `--all` for every trusted workspace)
over one SQLite database per workspace at `$TQ_HOME/hub/<ws>/hub.db` (WAL, pure Go, no
cgo, A §5); tables lanes, cards, comments, decisions, attachments (references only),
runs, activity, settings, sync_state, sync_outbox, lane_map (`internal/hub/store/schema.go`);
card kinds project, feature, fix, analysis, hotfix, chore; tags as a JSON column;
`tq hub add|list|show|edit|move|run|cancel|comment|answer|review|open|push|status` and
`tq hub sync ...`; agents may add and comment, and change tags on a planning card; only a
human sets a budget, moves to done, answers or pushes (A §9.2). The web page is FR-38.
**CONFLICT:** original PRD §6.1 wants plain files inside the identity root (one file per
card plus an index, YAML or Markdown) so the developer can version them and agents read
them without an API call. The code keeps the board out of every repo by design ("never
inside a repository", A §1) and the guard blocks agents from the store folder; agents read
through `tq hub show|list --json`. Decided (D-02): the database stays the source of
truth and the hub writes a read-only Markdown mirror into the client folder, one file per
card plus an index, refreshed on every change; editing a mirror file changes nothing on
the board.
**Acceptance checks**
- `tq hub add "x"` then `tq hub list` shows the card; the page at `127.0.0.1:7470` shows it
  (COMMANDS.md §4 step 12).
- `TestMoveIntoActiveEnqueuesAgentReadyOnly` and `TestHubEndToEndWithFakeClaude` pass.
- After a card changes, its mirror file under the client folder [placeholder: path, for
  example `<client_root>/.tentaqles/board/<card-id>.md`] is refreshed within one poll,
  and a hand edit to that file is overwritten on the next refresh without touching the
  board.

### FR-31 Mirror the company tracker: assigned items, every provider, polling and on demand
Why: G3, IDEATION.md: "syncs all tasks assigned to him from the task provider".
Status: done (live APIs not fully verified)
Evidence: five adapters linked into the binary: Asana, Azure DevOps, GitHub (Issues and
Projects v2), Jira Cloud, Linear (`cmd/tq/adapters.go`; `TestBinaryRegistersTrackerAdapters`);
`tracker.assigned_to: me|anyone` with `me` default (`TestListChangedAssignedToMe`);
polling with an updated-since watermark while `tq hub serve` runs, default 300 s, minimum
60 s (`TestSyncSettingsDefaultsAndValidation`); on demand with `tq hub sync now` or the
Connections page (`TestSyncNowChecksAndPolls`); credential chain cli, keychain, env
(`tq dotenv`), MCP (`TestChainTriesCLIThenKeychainThenEnvThenMCP`); outbound text redacted
with the workspace's exit classes (`TestRedactionOfOutboundText`); no webhooks by decision
(B §3).
Missing: no daemon, so nothing syncs while the hub is down; children of assigned items are
mirrored even when not assigned to the developer; an item reassigned away is not handled
[placeholder: unconfirmed]; assignee, attachments, tracker comments and labels are read
but not stored; GitHub `subIssuesSummary`/`rateLimit`, ADO `asOf`/`$expand` and Asana
parent `modified_at` are unverified against live APIs (spec B build notes); Trello,
ClickUp, Monday and Notion are deferred (B §3). Root `CLAUDE.md` still lists only the
jira and linear adapters.
**Acceptance checks**
- `TestPullImportsMapsLanesAndWatermark` and `TestBinaryRegistersTrackerAdapters` pass.
- With `tq secret set <ws> github` done, `tq hub sync now` then `tq hub sync status`
  shows the assigned items on the board and an empty last error (human check,
  COMMANDS.md §4 step 15).

### FR-32 Map every lane the provider exposes, without hardcoding lane names
Why: Original PRD §6.1: pull backlog, todo, in progress, test, QA, done and any custom
lane; map them in the manifest.
Status: partial
Evidence: `buildLaneMap`/`laneOf` (`internal/hub/sync/inbound.go`) read the adapter's
statuses; `tracker.lanes` in the manifest overrides the map (`lane_map` table); the
tracker's status name is kept as `external_lane`.
Missing: the local board has five fixed lanes seeded in `schema.go` (kinds backlog, todo,
active, review, done); no code creates a lane, so a provider's Test or QA column collapses
onto one local lane. Decided (D-14): the five typed lanes stay, because runs and
approvals key off them; the card tile shows the tracker's own status name.
**Acceptance checks**
- A linked card whose tracker status is `QA` sits in the review lane and its tile shows
  `QA`.
- `TestPullImportsMapsLanesAndWatermark` passes.

### FR-33 Conflict rules between the company board and the local board
Why: Original PRD §6.1: the company wins title, description, assignee, lane, attachments
and acceptance criteria; the local board wins agent status, decision requests and
sub-task links.
Status: partial, with a CONFLICT
Evidence: lane: the tracker wins (`applyItem`; `TestConflictTrackerWinsAndStaleOutboundSkipped`);
runs, decisions, `parent_id` and tags are never overwritten by a pull (`updateLinked`
writes only `external_*`, title and body); acceptance criteria are appended to the body
under `## Acceptance`.
**CONFLICT:** title and description: the company wins only until someone edits locally;
after that the local text is kept on every pull and pushed back as `edit_text` under the
outbox policy (`TestLocalTextEditSurvivesTwoPulls`). Decided (D-15): this stays the
rule (a refined card is not overwritten by the next pull, and tq asks before pushing the
edit); assignee, attachments and tracker comments, read but not stored today, are stored
and shown read-only on the card.
**Acceptance checks**
- `TestConflictTrackerWinsAndStaleOutboundSkipped` passes.
- After a local title edit and a tracker title edit, the board shows the local text and
  `tq hub sync outbox` holds an `edit_text` row waiting for an answer.
- A linked card shows the tracker's assignee, attachment names and comments, read-only.

### FR-34 Writes back to the company board go through a policy: off, ask or auto
Why: G3. Nothing reaches the client's board without a rule the developer set.
Status: done
Evidence: six outbound actions, each `off|ask|auto` (`DefaultSyncPolicy`): lane moves and
result comments default auto; create card, edit text and close item default ask (B §5);
`ask` creates a hub decision first (`TestPolicyAskCreatesDecisionThenSendOrSkip`); partial
or uncertain creates never duplicate; `tq hub sync outbox|retry` (retry human-only);
`tq/tracker-cli-write` guards direct CLI writes by agents.
Missing: back-off state is in memory only; Jira acli and Linear "created, no id" are
plain errors (spec B notes).
**Acceptance checks**
- `TestPolicyAutoSendsMoveOnStart` and `TestPolicyAskCreatesDecisionThenSendOrSkip` pass.
- With `create_card: ask`, `tq hub push <card>` produces a decision and sends nothing
  until it is answered.

### FR-35 Publish a local card to the company board, choosing lane and tags, keeping the link
Why: Original PRD §6.2. Local cards stay local unless the developer publishes them.
Status: partial
Evidence: `tq hub push <card>` (human-only) queues `create_card`; `deliver` links the
created item back (`LinkCard`; `TestPushCreatesChildOfParentItem`); the board has a
"Push to company board" action.
Missing: no lane or tag choice: `createPayload` carries title, body and parent only,
although the adapters already support `NewItem.Status`, `Labels` and `SetLabels`.
**Acceptance checks**
- `tq hub push <card> --lane <name> --tag a --tag b` creates the item in that status with
  those labels [placeholder: flag names].
- `TestPushCreatesChildOfParentItem` passes.

### FR-36 Lane moves the system performs on the company board
Why: Original PRD §6.3 and §10.3: starting a project moves the mirrored card to in
progress; when every local sub-task is done the parent moves to the company's review lane.
Status: partial
Evidence: a human move of a linked card into an active lane queues `move_on_start`
(`TestPolicyAutoSendsMoveOnStart`); when the last child is done `completeParent` moves the
local parent to review (`TestParentCompletionMovesParentToReview`); approving a result
queues the review move and result comment for a published card, and a result comment on
the parent for a local-only card (`TestApproveQueuesReviewMoveAndResultComment`); a card
entering done queues `close_item` (ask).
Missing: the parent's move to review is local only: the outbox queues nothing for it, so
the company-board parent does not move (original PRD §10.3 step 4). There is no
project-start command yet (FR-40). The code also performs moves §6.3 leaves manual
(`close_item`, `comment_results`, `edit_text`), each under the policy.
**Acceptance checks**
- When the last child of a linked parent is approved, `tq hub sync outbox` shows a
  `move_on_review` row for the parent and the tracker item changes status after delivery.
- `TestParentCompletionMovesParentToReview` passes.

### FR-37 Sync through the agent's MCP connector when tq holds no token
Why: Some trackers are reachable only through the harness's MCP connector.
Status: done
Evidence: `tq hub sync import|pending|ack` plus `/tentaqles:sync`, which reads the tracker
through the connector and reports back (`test_sync_skill_runs_the_fallback_loop_in_order`);
MCP is the last step of the credential chain and session-only (B §7).
Missing: none known. Name clash: `tq hub sync pending` lists fallback rows, not decisions
(the original PRD's `pending` is `tq hub list --waiting`).
**Acceptance checks**
- `test_sync_skill_runs_the_fallback_loop_in_order` passes.
- With no stored token, `/tentaqles:sync` imports the assigned items and `tq hub sync
  status` shows the MCP source.

### FR-38 A local web board: lanes, drag and drop, cards, decisions, runs, reviews, activity
Why: Original PRD §11 minimum: a browser-based board where a drag triggers the same
transition as the CLI.
Status: done
Evidence: `internal/hub/web`: board with drag and drop (`POST /api/cards/{id}/move` runs
the same `Service.Move` as the CLI; moving a running card asks for confirmation,
`TestAPIMoveRunningNeedsConfirm`), new-card dialog, card page with live log, tags and
comments, `/waiting`, `/reviews`, `/runs`, `/activity`, `/guard` (read-only),
`/connections` (sync now, settings, push), `/settings`, `/events` (SSE, ids only,
`TestSSEChangeCarriesIDsOnly`), theme toggle, keyboard move menu; auth by Host allow-list,
Origin/Referer/Sec-Fetch-Site checks and a hashed token (`TestRebindingHostAndOriginAreRefused`,
`TestNoAllowListAndNoTokenFailsClosed`); requests marked `X-TQ-Hub-Agent: 1` cannot answer,
approve, close or set budgets (`TestAPIAgentHeaderRefusesHumanOnly`).
Missing: the review page is linked, not inline; cards show run status, not gate status;
pack approvals cannot be answered on the board until WebAuthn exists (D §4); the page
does not open in the identity's browser profile (FR-08).
**Acceptance checks**
- `TestAPIMoveRunningNeedsConfirm`, `TestRebindingHostAndOriginAreRefused`,
  `TestAPIAgentHeaderRefusesHumanOnly` pass.
- Dragging an `agent-ready` card into the active lane starts a run visible under `/runs`.

### FR-39 One hub for every trusted workspace, with a switcher and a combined waiting list
Why: The developer has one machine and several clients; one page to see what is waiting.
Status: done, with a CONFLICT against the original PRD's non-goal
Evidence: `tq hub serve --all` opens one database per trusted workspace, never an
untrusted one (`TestServeAllUntrustedNeverOpened`), with a workspace switcher and a
combined `/waiting` (`TestWaitingCombinesWorkspaces`); databases are never joined.
`/tentaqles:dashboard` (plugin) shows every workspace's sessions, memory and pending items.
**CONFLICT:** original PRD §11 and §14 call cross-identity views out of scope. The owner
chose both topologies behind a switch in spec A (A §1). Decided (D-03): both stay; the
non-goal is mixing clients' data (§3.2).
**Acceptance checks**
- `TestServeAllUntrustedNeverOpened` and `TestWaitingCombinesWorkspaces` pass.
- No SQL in `internal/hub` joins two workspaces' databases (grep for `ATTACH`).

### Area E: project bootstrap and planning

### FR-40 Start a project from a mirrored card
Why: IDEATION.md and original PRD §7.1: picking an assigned card creates the project
folder, links it to the card, and moves the company card to in progress.
Status: not started
Evidence: the pieces exist separately: `/tentaqles:add-project` creates
`<client_root>/<slug>/` with `CLAUDE.md` (from `CLAUDE-project.md` plus a Client Context
section), `docs/adr`, `docs/specs`, `LEARNINGS.md`, an optional `brief.md` from a ticket
URL, and `git init`; `/tentaqles:kickoff` adds the stack ADR, the `tq-project` block,
`docs/checkpoints` and `docs/reviews`, the threat model, data classification and CI; the
hub links a card to a folder one way (`cards.project_dir`) and a hub-started run carries
the card in `TQ_HUB_TASK`; `move_on_start` moves the tracker item when a human moves a
linked card into an active lane (FR-36).
Missing: a command that takes a card, creates the project from it, records the link both
ways and performs the move. Mirrored cards are always imported as kind `feature` with no
tags and kind cannot be edited, so they run `/tentaqles:feature`, never `kickoff`.
**Acceptance checks**
- `tq hub start <card>` (name [placeholder]) creates `<client_root>/<slug>/` with the
  scaffold of FR-43, sets `cards.project_dir`, writes the card id into the project's
  `tq-project` block, and queues `move_on_start` for a linked card.
- A card imported from the tracker can be given kind `project` before it starts.

### FR-41 A PRD per project, drafted from the card and accepted by the developer
Why: Original PRD §7.2: `docs/PRD.md` with functional and technical specification,
iterated until an explicit accept command.
Status: done, with a CONFLICT on vocabulary and acceptance
Evidence: `/tentaqles:prd` writes `docs/prd.md` with 11 fixed sections, `### FR-nn`
requirements with binary acceptance checks, open items as `NEEDS-DECISION` in §10 and
releases in §11 (`plugin/templates/process/prd.md`); `prd-status.py` grades it
(`planning-ready` when §10 is empty and every FR has a check; `test_ready_prd`,
`test_open_decision_blocks`); `kickoff` and `feature` refuse to plan on a PRD that is not
planning-ready; `/tentaqles:direction` audits the repo against it; `/tentaqles:grill-me`
interviews on every business-rule edge; `feature` writes a spec slice at
`docs/specs/<slug>.md` and `build-planner` a checkpoint plan at
`docs/checkpoints/<slug>.json`.
**CONFLICT:** no MUST/SHOULD/OPEN vocabulary (original PRD §0) and no developer accept
command: "accepted" is the mechanical `planning-ready` grade. The skill's input is an
idea, brief or ticket URL; it does not read a hub card. Decided (D-16): keep the FR
format and the mechanical grade, and add a human-only `accept` command that stamps the
PRD, so planning starts only after the developer said so.
**Acceptance checks**
- `prd-status.py --cwd <project>` exits 0 on a PRD with no open decision and a check on
  every FR, and 3 otherwise (`test_ready_prd`, `test_open_decision_blocks`).
- `/tentaqles:prd` started from a project linked to a card pre-fills §1 from the card's
  body and acceptance criteria [placeholder: once FR-40 exists].
- `tq prd accept` [placeholder: name] run by a human stamps `Status: accepted` with the
  date; `kickoff` and `feature` refuse a PRD that is planning-ready but not accepted.

### FR-42 Break the PRD into cards, refine them, and mark them agent-ready
Why: Original PRD §7.3: one local card per feature with title, scope, acceptance
criteria, dependencies and a ready flag; refinement by skill; only `agent-ready` cards are
eligible for orchestration.
Status: done, with gaps
Evidence: `/tentaqles:plan-cards` makes one backlog `feature` card per FR of a release,
titled `<FR-id> <title>`, body = FR text plus checks plus a `Source:` line, and writes
the card ids back into PRD §11 inside `<!-- tq:cards -->` so re-runs skip them
(`test_plan_cards_release_1_then_2_makes_six_cards_and_is_idempotent`);
`/tentaqles:refine` runs grill-me on one card, the human saves the body
(`tq hub edit --body-file`, human-only by amendment B §9; the console saves it itself
once FR-72 ships, D-24), then the skill adds the
`agent-ready` tag (`store.TagAgentReady`); a move into an active lane starts a run only
for tagged cards (`TestMoveIntoActiveEnqueuesAgentReadyOnly`).
Missing: no dependencies between cards (only `parent_id`); no `ready` field (the tag is
the flag); a human's `tq hub run <card>` or "Run now" bypasses the tag check (run and
nested sessions cannot run any card; a console can, FR-70). No test of its own for `refine` [placeholder].
**Acceptance checks**
- `test_plan_cards_release_1_then_2_makes_six_cards_and_is_idempotent` passes.
- A card body or tag can declare `depends_on` another card id, and the hub refuses to
  start it while that card is not done [placeholder: field shape].

### FR-43 The project scaffold
Why: Original PRD §7.1: harness instruction files for every declared harness
(`CLAUDE.md`, `AGENTS.md`) pre-filled with identity, card and mode; plugin hooks; `docs/`,
`src/`, `tests/`, `tasks/`; a `project.yaml` linking to the card.
Status: partial
Evidence: `add-project` and `kickoff` write `CLAUDE.md` with the Client Context, the
`tq-project` JSON block (commands, paths, changelog, `pack_budget`; frozen at plan
approval), `docs/adr`, `docs/specs`, `docs/checkpoints`, `docs/reviews`, `LEARNINGS.md`;
the build gate installs its hooks per project into `.claude/settings.local.json`.
Missing: nothing writes `AGENTS.md` (Codex) or pre-fills identity, card or mode; no
`src/`, `tests/` or `tasks/` folders; no `project.yaml` (the card link is one-way, FR-40).
**CONFLICT, decided:** `project.yaml` vs the code's `tq-project` block plus
`.tentaqles.yaml` (D-20 keeps the block); the PRD's `tasks/` folder of card files is
served by the read-only Markdown mirror of D-02.
**Acceptance checks**
- A project created by FR-40 contains `CLAUDE.md` with the workspace name, the card id
  and the guard mode in its Client Context, and an `AGENTS.md` when the manifest declares
  a codex identity.
- The `tq-project` block of that project names the card id and the hub reads it back
  (`cards.project_dir` and the block agree).

### Area F: orchestration (hub runs)

### FR-44 Moving an agent-ready card into an active lane starts one headless run for it
Why: IDEATION.md and original PRD §8: one orchestrator per card, parallel across cards,
serialized on shared files.
Status: done, with CONFLICTS on shape
Evidence: `Service.Move` enqueues when the card enters an active lane and carries
`agent-ready` (`internal/hub/runner/human.go`); atomic checkout, one run per card; each
round is `claude -p --output-format stream-json --verbose` with the prompt and allowed
tools on stdin and a `--settings` file, never argv (`internal/hub/adapter/claude.go`;
`TestClaudeArgvAndPermissionWrapper`); budgets in USD, minutes and rounds
(`TestEachBudgetLimitStopsTheRun`); concurrency per workspace default 2, range 1–16, FIFO
(`TestConcurrencyLimitAndFIFO`); a per-folder project lock until the build relocates to
its worktree `../<project>-build-<slug>` on branch `build/<slug>`
(`TestProjectLockWaitsUntilRelocation`); statuses queued, running, waiting, finished,
failed, cancelled, over-budget (A §11, stable); `tq hub run|cancel`; `TQ_HUB_TASK`
`{hub, workspace, kind, external}` and `TQ_HUB_RELOCATE <worktree> <slug>` are the wire
formats (A §13). `TestHubEndToEndWithFakeClaude`.
**CONFLICT:** the control plane is `tq hub serve` (started by the human or the desktop
app), not the developer's main Claude session; each round is a separate process, not a
subagent of that session (original PRD §8 "the main harness session is the control
plane"). The worktree is named from the feature slug, not the card id. Decided (D-17):
the hub stays the runner (runs outlive the session, budgets and concurrency live in one
place, a run never inherits a session's approvals); worktrees keep the slug, with the
card id recorded in the plan's `task`. D-24 (2026-10-31) keeps D-17: the console asks the
hub to start the run and never runs a round itself.
**Acceptance checks**
- `TestMoveIntoActiveEnqueuesAgentReadyOnly`, `TestConcurrencyLimitAndFIFO`,
  `TestProjectLockWaitsUntilRelocation`, `TestEachBudgetLimitStopsTheRun` pass.
- Two agent-ready cards moved to active within a minute produce two runs that finish
  without a git index conflict in the shared project folder.

### FR-45 Workers inherit the identity, the mode and the guard
Why: Original PRD §8.1: the orchestrator may spawn researcher, code writer, UI, UX, test
writer and security reviewer workers; all inherit identity, mode and guard.
Status: done, with a different crew
Evidence: inside a round, `/tentaqles:build` uses `general-purpose` subagents to write
tests and implementation and the plugin's 13 agents as planner, critics, fixer, arbiter,
breaker and reviewers (`plugin/agents/*.md`; reviewers are read-only,
`test_reviewers_are_read_only`; `breaker` is the only one with Bash, bound to 127.0.0.1);
subagents run inside the round's process and inherit `adapter.LaunchEnv` (workspace
environment plus `TQ_MODE`) and the tq and gate hooks (`TestLaunchEnvironmentReachesTheAdapter`).
Missing: no researcher, UI or UX roles (the original PRD lists them as examples, not
requirements).
**Acceptance checks**
- `TestLaunchEnvironmentReachesTheAdapter` and `test_reviewers_are_read_only` pass.
- A `.env` read attempted by a subagent inside a headless round is blocked with
  `tq/env-file-read` in the round's transcript.

### FR-46 Progress is written to the card at every lane change and at a steady cadence
Why: Original PRD §8.1: progress at every lane change and at least every N minutes
(default 15, configurable).
Status: partial
Evidence: lane moves are `card.move` activity; the hub comments on the card when the
build relocates, retries, halts, hits a budget, fails or is cancelled; the run log gets a
cost line at most every 30 s (`DefaultProgressEvery`) shown live on the card page; the
newest five human comments are quoted into every run prompt (PR #42).
Missing: a periodic progress write on the card itself every N minutes, configurable.
**Acceptance checks**
- A run longer than N minutes leaves at least one progress comment per N minutes on the
  card, with N read from settings (default 15) [placeholder: setting name].
- Every `card.move` of a running card appears in `/activity` with actor and time.

### FR-47 Decisions: a run that needs the developer records a decision, waits, and resumes
Why: IDEATION.md and original PRD §8.2: question, options, recommendation, what is
blocked; pause that task only; answer by editing the card or from a `pending` command;
timestamped before resuming.
Status: done, with gaps and one CONFLICT
Evidence: decision kinds `plan-approval`, `result`, `judge`, `question` from the gate,
`permission` (and pack approvals as `permission` with `data.pack`) from the hub
(`internal/hub/store/decisions.go`; `TestUpsertDecisionMirrorsGateRecords`); a decision
holds kind, text, options, checkpoint, reviewer and data; the run goes to `waiting` and
frees its slot while other runs continue (`TestAnswerResumesTheRun`,
`TestPreSlugQuestionWaitsOnTheHuman`); `tq hub list --waiting`, `/waiting`, and
`/tentaqles:hub` list and explain them; `tq hub answer <card> <decision>` (human-only,
signed; pack approvals need a terminal and a typed binding) records `answered_at`, actor
`hub:<user>`, choice and note before re-queueing (`TestTerminalAnswerIsNoticedByThePoll`);
answers from the board page call the same path; an OS notification fires on a pending
result (`TestSendIsBoundedToTwoSeconds`).
Missing: no separate recommendation and blocked fields (they live in the text); editing
the card does not answer; a comment reaches the next prompt but does not resume.
**CONFLICT:** the original PRD's `pending` answers inline from the main harness session;
`tq hub answer` refuses every Claude session on purpose (the human is the only authority,
A §1), so `/tentaqles:hub` prints the command for the human. Decided (D-18), superseded on
2026-10-31 by D-24: the inline `pending` path returns as FR-71, a multiple-choice question
in the console; `tq hub answer` from a run, nested or unknown session is still refused.
**Acceptance checks**
- `TestAnswerResumesTheRun`, `TestUpsertDecisionMirrorsGateRecords`,
  `TestTerminalAnswerIsNoticedByThePoll` pass.
- `tq hub list --waiting --json` shows every open decision with `recommendation` and
  `blocked` fields [placeholder: once added], and a run resumes within one poll after
  `tq hub answer`.

### FR-48 The card's kind selects the workflow
Why: Original PRD §8.4: feature, bugfix, analysis and hotfix cards follow the same
contract with a workflow chosen by type.
Status: done, with gaps
Evidence: `OnRampFor` (`internal/hub/runner/prompt.go`; `TestPromptPerKind`): project →
`/tentaqles:kickoff`; feature, analysis, chore → `/tentaqles:feature`; fix →
`/tentaqles:fix`; hotfix → `/tentaqles:fix --hotfix` (skips the critic, never the
regression test). Headless skills propose manifest changes as
`docs/privacy-manifest-proposal.yaml` and never write `.tentaqles.yaml`
(`headless-manifest-guard.py`).
Missing: cards imported from the tracker always get kind `feature` and no tags; a move
synced in from the tracker never starts a run, so assignment alone triggers nothing (by
decision, B §4); the original PRD's trigger-by-tag is kind-based here.
**Acceptance checks**
- `TestPromptPerKind` passes.
- An imported card can be given kind `fix` or `hotfix` before it is tagged `agent-ready`
  (through `tq hub edit` or a `tracker.kind_map` rule [placeholder]).

### FR-49 Headless runs are guarded like sessions and cannot widen their own permissions
Why: G2. A run must not escape the mode or carry a human's approval token.
Status: done
Evidence: `LaunchEnv` sets `TQ_MODE` to `guard.headless_mode` (default `default`) and
drops inherited `TQ_MODE`, `TQ_ENABLED`, `TQ_BUILD_TOKEN` and `TQ_BUILD_ACTOR`
(`internal/hub/adapter/adapter.go`; `TestLaunchEnvHeadlessMode`); the hub adds no
permission flag and never uses the bypass launcher (`claudeArgs`); bypass only after a
human `tq allow --mode`; the hub only calls `build-gate answer`, never writes gate state
(A §1); hub actor tokens are HMAC over `["hub", action, slug, actor]` and refused after a
rollback (`test_forged_hub_token_is_refused`, `TestTokenMatchesPythonVectors`).
Missing: a run without a worktree works in the live project tree until relocation (the
project lock serializes it).
**Acceptance checks**
- `TestLaunchEnvHeadlessMode`, `TestClaudeArgvAndPermissionWrapper`,
  `test_forged_hub_token_is_refused` pass.
- A headless round's environment (from its transcript preamble) shows the headless mode
  and no `TQ_BUILD_TOKEN`.

### Area G: quality gates, packs and decisions

### FR-50 The gated build engine: locked tests, implementer, recorded gates, review, fixer, DoD
Why: G4. "A card's done is trustworthy only if a gate, not the builder, proves it"
(DP §1: one engine, three on-ramps).
Status: done
Evidence: `/tentaqles:build` per checkpoint: failing tests at the lowest layer, locked
(`build-gate lock-tests`), an implementer who cannot edit them, `build-gate verify` runs
the frozen commands against the exact code on disk and records evidence, a review crew
sized by `tq decide triage` with `build-critic` always present
(`test_review_pass_needs_a_recorded_critic_pass`), a fixer loop (three tries, then split,
then halt) with `arbiter` for disputes, then the definition of done (`cmd_dod`: tests,
lint, typecheck, pack checks, locked tests, reviews, ADR/privacy/migration/changelog, PRD
decisions; `test_dod_passes_when_everything_is_recorded`); records are HMAC-signed with
generation records (FR-27); `build-gate serve` shows a read-only progress page;
`TENTAQLES_HEADLESS=1` turns human stops into decision records (DP §5.4). Verdicts PASS,
FAIL — reason, CANNOT JUDGE; `RESULT: PASS|FAIL|BLOCKED` (DP §4.3).
Missing: gates cannot be added, removed or reordered per project (FR-53); `build-planner.md`
documents `tests` as objects while the gate accepts strings only (known defect, spec A
notes).
**Acceptance checks**
- `test_failed_verify_blocks_pass`, `test_review_pass_needs_a_recorded_critic_pass`,
  `test_dod_passes_when_everything_is_recorded` pass.
- A checkpoint whose locked test is edited by the implementer fails `verify` with the
  lock reason.

### FR-51 Gate coverage: build, review, tests and coverage, secrets and dependencies, lint and format
Why: Original PRD §9 table. Each stage has a pass condition.
Status: partial
Evidence: build runs when a checkpoint's verify commands include it, and new dependencies
are shown at plan approval; code review is FR-50; tests are FR-50; coverage may not drop
below the baseline only in CI (`ratchet.py` installed by `/tentaqles:ci-pipeline`;
`test_regression_fails`; null baselines are bound from CI by `babysit-pr`, PR #40);
secrets at commit by the guard (FR-25) and gitleaks plus dependency audits in the CI
templates (`ci-pipeline`, `supply-chain`); lint and typecheck in the DoD.
Missing: coverage, secrets scan and dependency scan are not part of the DoD itself; no
format step; a failing gate attaches findings to the plan and `docs/reviews/`, and the
card gets a comment only when the run halts or fails (original PRD §9 wants the failure
on the card).
**Acceptance checks**
- `cmd_dod` records a `coverage`, a `secrets-scan` and a `format` item, each PASS or
  FAIL with its command [placeholder: item ids].
- A gate failure writes a comment on the card naming the gate and the finding before the
  fixer starts.

### FR-52 An evals gate for cards that declare evals
Why: Original PRD §9: if the card declares evals, they run and metrics are recorded on the
card and the review page. The vision also wants checklist evals for non-code work (DP §13
backlog).
Status: not started
Evidence: nothing declares evals on a card or records results; `/tentaqles:loop` keeps a
change only when a test score improves (a related mechanism, not a gate).
Missing: the card field, the gate, the record, the review-page section. Decided (D-19):
one mechanism for code and non-code cards: an `evals` block holding either commands with
thresholds or a checklist; the gate runs it, retries a configured number of times, and
records results on the card and the review page; cards without the block skip the gate.
A visual review gate (D-23) builds on this later, after evals ship.
**Acceptance checks**
- A card with an `evals:` block [placeholder: shape] produces an `evals` gate record with
  metrics, shown on the review page.
- A card without one shows "no evals declared" and the gate is skipped, not failed.

### FR-53 Gate definitions declared per project
Why: Original PRD §9: a project can add, remove or reorder gates in its config; hotfixes
may skip evals when the project allows.
Status: partial
Evidence: the `tq-project` block (`plugin/tentaqles/workflow/project.py`) carries
commands, paths, changelog and `pack_budget`, frozen at plan approval; pack checks come
from the manifest `packs:` block and a tighten-only `tq-packs.yaml` (C §2.3);
`--hotfix` skips the critic, never the regression test (DP §5.3).
**CONFLICT, decided (D-20):** optional gates (evals, format, coverage threshold) may be
added or dropped through the `tq-project` block; tests, review and the definition of done
cannot be removed; a hotfix skips the review and evals, never the regression test. No
`project.yaml`.
**Acceptance checks**
- The `tq-project` block accepts a `gates:` list that adds or removes an optional gate
  (evals, format, coverage) and the DoD honours it [placeholder: shape]; required gates
  (tests, review, DoD) cannot be removed and the gate says so.
- `/tentaqles:fix --hotfix` records the review and evals gates as skipped and the
  regression test as run.

### FR-54 Gate results reach the card and the review page; the developer is notified
Why: Original PRD §9: results on the card and in the review page; a notification when a
task reaches review.
Status: partial
Evidence: the review page is built from gate records (`BuildReview`;
`TestReviewPageIsBuiltFromGateRecords`); a pending result moves the card to review and
sends an OS notification (`internal/hub/notify`; `TestWaitingOnAResultMovesTheCardToReview`,
`TestSendIsBoundedToTwoSeconds`).
Missing: the card itself only links to the review page (no gate status on the card tile);
no terminal bell. Decided (D-22): OS notification plus a terminal bell in the hub's
terminal; chat and phone messages not planned.
**Acceptance checks**
- `TestWaitingOnAResultMovesTheCardToReview` and `TestReviewPageIsBuiltFromGateRecords`
  pass.
- The card tile on the board shows the last gate's PASS/FAIL state.
- The terminal running `tq hub serve` rings the bell when a card reaches review.

### FR-55 Packs: checks, guards, reviewers and approvals for data, ML, cloud and database work
Why: One engine, optional overlays per kind of work (DP §1; specs C and D).
Status: done (live-tool paths are human-verified only)
Evidence: `tq pack list|check|doctor|crew|approvals|baseline` (`internal/packs`):
built-in Go lints plus declared external tools as required checks and undeclared ones as
advisory (C §1.1); guards are pure functions under 50 ms and supersede core rules
(C §2.10, §2.14); only the manifest `packs:` block enables a pack and the repo file can
only tighten; config hash frozen at plan approval; approvals are signed, single-use,
bound to evidence and answered in a terminal (C §2.11; D §4); `envgate` resolves
environments and an unresolved target counts as the strictest (D §1). data: ODCS
contracts, dbt gates, pipeline review, warehouse cost estimate and guards. ml: leakage
lint, reproducibility, model card, drift, promotion gate (`tq pack ml promote
request|apply|status`). cloud: IaC policy, IAM lint, `tq pack cloud plan|show|approve|prune`
with plans stored outside the repo and only a redacted summary shown. db: migration lint,
RLS rules plus pgTAP, `tq pack db drill`, `tq pack db approve <env>`. Reviewers
`pipeline-reviewer`, `ml-reviewer`, `iac-reviewer`, `db-reviewer`; skills `pack`,
`db-migration`, `index-advisor`, `rls-tests`, `restore-drill`, `iac-policy`,
`data-contract`, `pipeline-review`, `warehouse-cost`, `ml-review`, `model-card`,
`drift-check`, `ml-promote`. Rule ids are stable (D §16). `docs/PACKS.md`, `docs/packs/*`.
Missing (documented limits): relaxed mode waives pack gates including denies
(`framework-security.md`); dbt `retry` and Python-launched dbt are not seen; queries in
scripts and Spark are not estimated; `dvc.lock` snapshots not compared; WebAuthn for board
answers deferred; Kubernetes/Helm, Infracost, state drift, MySQL/Oracle/NoSQL DDL deferred
(D §2, §24); lineage, running pipelines or training deferred (C §1.3).
**Acceptance checks**
- `tq pack check` on a repo with `packs: { db: { enabled: true } }` and a migration that
  drops a column exits 1 with a db finding (COMMANDS.md §4 step 11).
- `TestSignedAnswersOnly`, `TestRepoLocalFailsClosed`, `TestTimeoutKillsTree` pass.
- A `tq pack cloud plan` against a real Terraform project prints a summary and never the
  plan (human check, step 15).

### FR-56 Jev decisions: risk triage, routing, evidence checks, skill picker, memory gates
Why: Small yes/no model calls make the engine's depth proportional to risk (DP §9).
Status: partial (shadow only)
Evidence: `tq decide status|triage|explore|ask|eval|log` (`internal/decide`;
`docs/DECIDE.md`): triage rates a staged diff low or high with flags (auth, money, schema,
secrets, classified data, agentic, pack flags) and exits 0 or 3; judgment rules on tool
calls (four built-in, capped at ask, plus manifest rules); subagent model routing; Stop
check that a "tests pass" claim is backed by the transcript; skill picker on prompt
submit; memory gates; everything redacted with secret patterns and framed as untrusted;
the key goes only to `https://api.typesafe.ai` or localhost
(`TestRedactsKeysAndQuestions`, `TestCacheIsPerEndpointAndValidated`); cache 7 days;
`eval` requires 0.90 precision; seed set of 26 cases (`docs/decide/seed-cases.jsonl`).
Missing: all gates are shadow (log only); nothing may be enforced until each family has
about 100 cases and two weeks of shadow logs (DECIDE.md); no key means every diff is
high risk; routing needs a live check that the routed model runs; the Stop check can be
fooled (`go test | tail`); memory gates have no enforce switch; TypeSafe is US-hosted with
no zero-retention option.
**Acceptance checks**
- `tq decide status` prints backend, mode and `key: found|not found` without the value
  (COMMANDS.md §4 step 10).
- `tq decide eval` on a family with ≥100 labelled cases reports precision ≥ 0.90 before
  that family's mode may be set to `enforce`.

### Area H: developer review and approval

### FR-57 A review page with Submit (send back) and Approve
Why: IDEATION.md and original PRD §10: goal, approach, what was done, where it stands,
evals, links, a feedback field with Submit, an Approve button, served locally.
Status: done, with gaps
Evidence: `tq hub review <card> --open|--html` and `GET /cards/{id}/review`
(`internal/hub/web/review.go`, `templates/review.html`): verdict band, what was asked,
approach, what was done (PR link from comments, commits, `git diff --stat`), where it
stands (checkpoints, gates, reviewers, DoD), evidence and cost; Submit =
`POST .../review/request-changes` posts the feedback as a comment, answers the result
decision `request-changes`, moves the card back to active and resumes the run, which
first asks for a new plan approval (`TestRequestChangesCommentsAnswersAndResumes`);
Approve = `POST .../review/approve` (`finishApproved`) moves the card to done, comments on
the parent and exports `runs/<run>.review.html`
(`TestApproveFinishesMovesDoneAndCommentsTheParent`); "Mark done" closes the tracker item
after asking; transport is local HTTP with Host and Origin checks
(`TestRebindingHostAndOriginAreRefused`).
Missing: links to the branch, the diff and the card; an "open items" section; evals
(FR-52); the page opens in the default browser, not the identity's profile (FR-08).
**Acceptance checks**
- `TestRequestChangesCommentsAnswersAndResumes` and
  `TestApproveFinishesMovesDoneAndCommentsTheParent` pass.
- The review page shows a branch link, a diff link and a card link, and an "Open items"
  section listing unanswered NEEDS-DECISION lines and skipped gates.

### FR-58 Approval propagates to the company board, including the parent when all children are done
Why: Original PRD §10.3 steps 1–4; G3 end to end.
Status: partial
Evidence: steps 1–3 are done through the outbox: a published card queues the review move
and a results comment; a local-only child queues a results comment on its parent
(`TestApproveQueuesReviewMoveAndResultComment`).
Missing: step 4: when every child is done the local parent moves to review
(`completeParent`), but the outbox queues nothing for that move, so the company-board
parent stays put (FR-36). The whole loop has not been recorded end to end against a real
tracker (spec B: "real-tracker E2E is the human's").
**Acceptance checks**
- After approving the last child of a linked parent, `tq hub sync outbox` shows a move
  row for the parent and the tracker shows it in its review status after delivery.
- One real card on one real tracker goes assigned → project → run → review → approve →
  tracker review lane, and the run is recorded in `docs/` [placeholder: where].

### Area I: GUI

### FR-59 A native desktop app for setup, the board and the guard
Why: Original PRD §11 optional native GUI; the vision's "tentaqles GUI".
Status: done
Evidence: `desktop/` (Wails v2, shares `internal/` through `setupapi`): Setup wizard
(Welcome, Work folder, Companies, Services, Tools, Bundles, Preview, Log in), Board tab
(reuses a running hub or starts `tq hub serve` as a child, then navigates to it;
`TestOpenBoard_StartsTQHubServeAsAChild`), Guard tab (shows each workspace's mode, sets
it, grants looser modes); installers for Windows, macOS and Linux with build provenance.
`docs/DESKTOP.md`.
Missing: the board is navigated to, not embedded (the hub page forbids framing); the
installers are unsigned; no update mechanism in the app; CLAUDE.md calls `desktop/`
"(planned)".
**Acceptance checks**
- `TestOpenBoard_StartsTQHubServeAsAChild` passes.
- Each desktop installer of the latest release passes `gh attestation verify`.

### FR-60 Identity switching and manifest editing in the GUI
Why: Original PRD §11: the native GUI adds identity switching and manifest editing.
Status: partial
Evidence: the desktop picks a workspace for the Board tab and writes manifests in the
Setup wizard; `/tentaqles:client-settings` and `/tentaqles:switch-client` cover the same
from a session.
Missing: editing an existing manifest in the GUI (then re-trusting it with `tq allow`,
which stays human-only), and a switcher that opens a terminal or harness in the chosen
workspace. The GUI does not open in the identity's browser profile (FR-08).
**Acceptance checks**
- The desktop app edits a field of an existing manifest, shows that trust was dropped,
  and offers the `tq allow` command to run.
- A "Open terminal here" action starts the OS terminal in the workspace with its identity
  active (`termopen`).

### Area J: memory and knowledge (not in the original PRD)

### FR-61 Per-workspace memory, search, decay, vault export, knowledge graph, signals, dashboard
Why: Agents should remember a client's decisions and history without ever mixing clients.
Status: done
Evidence: `plugin/tentaqles/memory/`: per-workspace SQLite (`memory.db`: sessions,
touches, decisions, pending, semantic and procedural memories, atoms, reference docs) with
FTS5 BM25 search and a recency boost (`tests/test_search_memory.py`), decay and
compaction, Obsidian vault export and markdown ingest with a watermark, a knowledge graph
(graphify or native), cross-workspace patterns and signals through a global `meta.db`
that holds only display names, stats and signals, snapshots and rollback, and a live
dashboard; hooks SessionStart (preamble, recall), UserPromptSubmit, PostToolUse
(knowledge capture), Stop, SessionEnd (save, export), all with short timeouts and slow
work in detached workers; every write passes `privacy.redact_text` with the workspace's
exit classes. Skills: `query-memory`, `decision-history`, `file-history`, `explore`,
`build-graph`, `compact-memory`, `cross-patterns`, `profile-refresh`, `export-vault`,
`emit-signal`, `dashboard`, `session-wrap`, `wrap`. `plugin/CLAUDE.md`.
Missing: `scripts/pre-compact.py` has no hook entry (possible orphan, unconfirmed); the
dashboard shares port 8765 with `build-gate serve`; the vault mirror of PRD status and
NEEDS-DECISION items (DP §14 "later minor release") is unconfirmed.
**Acceptance checks**
- `tests/test_search_memory.py` and `tests/test_embeddings_optional.py` pass (the second
  with `fastembed` absent).
- No row in workspace A's `memory.db` names a path under workspace B's root after a
  session in each.

### FR-62 A knowledge-base ingestion pipeline with distillation and hybrid retrieval
Why: Owner backlog (DP §13; memory note of 2026-10-20): one connector per source into the
client's `memory.db`, distillation into question/summary/resolution/systems/code refs
before embedding, bursting of same-author runs, hybrid retrieval (full text, embeddings,
IDF, age decay) fused by RRF (k=60) then a reranker, planner → executor → synthesis with
citations, incremental code chunking, with/without-KB eval.
Status: not started
Evidence: the markdown connector, FTS5 search and the vault export (FR-61) are the
starting points the backlog names.
Missing: the pipeline. Depends on FR-61 and on credentials through `tq dotenv` and the
redaction exits. Decided (D-21): docs folders and the tracker first (both already have a
connector or arrive through the board); Slack, Teams and email after, once the exits are
proven on them.
**Acceptance checks**
- Tracker items and a docs folder are ingested into the workspace's `memory.db` with
  per-item source and date, and `query-memory` answers a question with a citation to one
  of them.
- The with/without-KB eval reports precision on a labelled set before retrieval is made
  the default.

### FR-63 Low-friction capture into cards, and checklist evals for non-code work
Why: Owner backlog (DP §13): hotkey or phone capture into hub cards; evals as checklists
with retry before "waiting on human", so the board serves non-code tasks too.
Status: not started
Evidence: none; `tq hub add` and the board's new-card dialog are the only capture paths.
Missing: a capture entry point outside a terminal (hotkey, phone, email-in [placeholder])
and a checklist-eval card type. Depends on FR-52 for the eval record shape.
**Acceptance checks**
- A card can be created from outside a terminal and a session and appears on the board
  within one poll [placeholder: mechanism].
- A card with a checklist eval runs the checklist, retries on failure up to a configured
  count, then waits on the human with the checklist result on the card.

### Area K: distribution, operations and documentation (not in the original PRD)

### FR-64 One tag releases everything, pinned and verifiable
Why: G5.
Status: done
Evidence: `.github/workflows/release.yml` on a `v*` tag: checks the tag is on `main` and
the plugin versions agree, goreleaser builds tq for every platform plus the GitHub
release, Homebrew tap and Scoop bucket with `checksums.txt`, the desktop job builds the
installers after checking the archive's checksum, both jobs attest build provenance
(first in v0.8.2, all nine assets verified); `installers/install.ps1` and `install.sh`
check the checksum and, with a logged-in `gh`, `gh attestation verify` against the
release workflow as signer, refusing on mismatch (verified 2026-10-30); the root
marketplace pins `plugin/` to the tag and its commit (`TestRootMarketplacePin`, CI job
`marketplace-pin`); tags `v*` cannot be moved or deleted and `main` requires nine green
checks including `govulncheck`; every action is pinned by commit sha; Dependabot proposes
weekly minor updates (major action upgrades are done by hand and checked with a release).
`docs/RELEASING.md`.
Missing: the installers are served from mutable `main` (raw.githubusercontent), so their
own integrity rests on TLS and the GitHub account; desktop installers are unsigned at the
OS level.
**Acceptance checks**
- `TestRootMarketplacePin` and `TestDevMarketplaceMatchesPlugin` pass.
- The installer run against a deliberately altered archive exits non-zero and installs
  nothing (verified 2026-10-30 on Windows and Git Bash).

### FR-65 Health checks with a fix per finding
Why: A misconfigured identity must be visible before it causes a wrong-account action.
Status: done
Evidence: `tq doctor` (read-only; every finding ends with the fix) covers trust, hooks,
git identity, identity dirs and logins, settings drift, plugins and bundles, guard mode and
grants, migration leftovers, services (tracker, chat, email), hub and packs; check ids in
`internal/doctor` (for example untrusted, env-drift, claude-config-drift,
git-email-drift, identity-wrong-account, settings-drift, plugin-version-skew,
plugins-dir-linked, mode-not-granted, codex-hooks-untrusted, tracker-auth-failed,
hub-port-busy; pack checks in `tq pack doctor`); `tq providers check`, `tq secrets
audit`, `tq worktrees list|prune --yes`, `tq pack doctor`.
Missing: about half the check ids are not listed in any doc (the docs cover the
settings, guard, migration, services and pack groups; the core group is documented only by
its output).
**Acceptance checks**
- On a configured machine `tq doctor` prints `[ok]` lines and no `[warn]` about a
  `claude` alias shadowing the launcher (COMMANDS.md §4 step 4).
- Every doctor check id in `internal/doctor` appears in a doc with its fix [placeholder:
  doc].

### FR-66 Settings, status line and Codex configuration rendered per identity
Why: Each identity's harness settings must carry tq's hooks, permissions and status line
without clobbering the user's own edits.
Status: done
Evidence: `tq settings render [--all]` writes tq's part of each identity's Claude
`settings.json` (baseline permissions, env, status line, hooks), keeps hand edits and
prints what changed (`docs/CLAUDE-SETTINGS.md`); `tq statusline` (hidden); Codex
`config.toml` block, `rules/tq.rules` and `hooks.json` through `internal/codexcfg`;
`tq worktrees` prunes leftover agent worktrees safely.
Missing: `statusline` is hidden in the CLI and documented only in CLAUDE-SETTINGS.md.
**Acceptance checks**
- `tq settings render --all` twice in a row prints `up to date` for every identity on the
  second run (COMMANDS.md §4 step 13).
- A hand-added key in an identity's `settings.json` survives `tq settings render`.

### FR-67 Standalone client-system templates
Why: Clients' data, ML and automation systems should start from tested templates that
already pass the packs.
Status: done
Evidence: `templates/`: `dbt-contract-starter` and `ml-starter` (both pass the pack
checks, CI job `templates`), `rag-starter`, `n8n-jev-decision`, `pandas-jev-labeller`;
plugin process templates (PRD, ADR, spec slice, fix spec, LEARNINGS, project CLAUDE.md,
review contract, threat model, privacy record, incident playbook, MODEL_CARD, ODCS) and
pack templates (cloud and db examples, Rego policy, RLS test helpers).
Missing: `rag-starter`, `n8n-jev-decision` and `pandas-jev-labeller` are not described in
any doc under `docs/`.
**Acceptance checks**
- The `templates` CI job passes for every template with tests.
- Each template has a README or a `docs/` entry naming what it is for [placeholder].

### FR-68 Documentation stays current with the code
Why: The docs are the operator's runbook; stale statements cost real time.
Status: partial
Evidence: `docs/COMMANDS.md` (every command plus a 15-step walkthrough), `CLAUDE-HOOK.md`,
`CLAUDE-SETTINGS.md`, `DECIDE.md`, `INSIGHTS.md`, `MIGRATE.md`, `PACKS.md` and
`docs/packs/*`, `PROVIDERS.md`, `SETUP.md`, `DESKTOP.md`, `RELEASING.md`,
`GUARD-SCOPE.md`, `threat-model.md`, the six design specs and their plans.
Missing (found 2026-10-30): root `CLAUDE.md` calls `providers/` and `desktop/`
"(planned)" and lists only the jira and linear adapters; `docs/design/README.md` stops at
spec A; COMMANDS.md says plain `tq allow <ws>` is human-only (it is not), shows `tq login
<cli>` (code: `tq login <ws> <id>`) and `tq hub cancel <card>` (code: `cancel <run>`);
`CLAUDE-HOOK.md` has a stale "planned for plugin 0.12.0" note; COMMANDS.md,
TEST_RESULTS_RUN01.md, IDEATION.md and the original PRD are untracked; the walkthrough's
step 8 uses `mktemp` (fails in PowerShell) and step 3 expects a git email in a folder that
is not a repo.
**Acceptance checks**
- A doc-check test or CI step fails when a `tq` command in `docs/COMMANDS.md` does not
  match a cobra `Use:` string in `internal/cmd` [placeholder: mechanism].
- Root `CLAUDE.md` names the five tracker adapters and no longer marks `providers/` or
  `desktop/` as planned.

### Area L: the console (the human's session as the control surface)

Decided on 2026-10-31 (D-24 to D-26): the human drives the board from one interactive
`claude` started in the workspace terminal. These requirements replace the parts of D-10,
D-18 and the hub's "agent session" rule that treated every Claude session alike. The threat
model records the new boundary B10 and the accepted risk AR3.

### FR-69 Session classes: terminal, console, run, nested, unknown
Why: G2, D-24. One boolean, `packs.AgentSession()` (`internal/packs/human.go`), cannot
tell the human's own `claude` from a headless run; `CLAUDECODE=1` marks both, so a command
typed with the `!` prefix is refused like an agent's.
Status: not started
Evidence: `TestHubAgentSignals` pins the one-class rule (`CLAUDECODE=1`, `CLAUDE_PLUGIN_ROOT`,
`TENTAQLES_HEADLESS=1` and `TQ_HUB_TASK` are all "agent").
Missing: a package (`internal/session`) that classifies the calling process from the
environment plus the chain of parent processes: `terminal` (no claude/codex above),
`console` (exactly one claude above, started by the human, no headless marker), `run`
(`TENTAQLES_HEADLESS=1`, `TQ_HUB_TASK`, `TENTAQLES_HEADLESS_CHILD`, or `tq hub serve`
above), `nested` (two or more claude/codex above, or Codex sandbox markers), `unknown`
(the walk failed while an agent marker is set: refused with the cause named). One process
snapshot per OS (Windows Toolhelp32 process snapshot through `golang.org/x/sys/windows`,
never the PowerShell `Get-CimInstance` path; Linux `/proc`; macOS `sysctl`), at most 16
hops and 200 ms, memoised, with a fake-table seam for tests. The class travels to a
served hub as `X-TQ-Hub-Session`; the server maps any unlisted value to `run` and the
legacy `X-TQ-Hub-Agent: 1` still means `run`. `tq doctor` reports class `unknown`.
**Acceptance checks**
- `TestSessionClassMatrix` passes: each row of the environment × ancestry table yields the
  class above, including `CLAUDECODE=1` plus one interactive claude ancestor → `console`,
  `TENTAQLES_HEADLESS=1` plus the same ancestor → `run`, two claude ancestors → `nested`,
  `CLAUDECODE=1` with no claude ancestor or a failed walk → `unknown`, a failed walk with no
  marker → `terminal`.
- `TestWalkSelfHasParent` passes on Windows, macOS and Linux CI: the real walk finds the
  test binary's parent within 200 ms.
- `TestModeWritesRefuseAgents` and `TestAllowModeIsHumanOnly` pass unchanged: `CLAUDECODE=1`
  alone still refuses every mode write, `tq allow --mode` and `tq unattended`.
- `TestAPIClassHeaderPolicy`: the hub API refuses human-only actions for `run`, `nested`,
  `unknown`, an unlisted header value and the legacy agent header;
  `TestAPIAgentHeaderRefusesHumanOnly` passes unchanged.
- `tq doctor` prints a warning row when the class is `unknown`, naming the cause.

### FR-70 The console drives the hub as the human's proxy
Why: D-24, D-25: the human never types tq commands for board work; a `!`-prefixed command
and a skill acting after a multiple-choice question are both the human's.
Status: not started
Evidence: the hub refuses every write beyond add, comment, planning-lane moves and tags
from any Claude session (`TestHubAgentMayAddAndCommentOnly`, `TestHubEditByAnAgent`); the
locked rule `tq/human-answer` (`internal/policy/builtin_framework.go`) denies `tq hub
answer` from every Claude shell; pack approvals need a terminal and a typed code.
Missing: a hub policy per class: `console` gets every human action (add in any lane, move
between any lanes, `run`, `cancel`, `answer` for gate decisions, hub questions, send/skip
and pack approvals, review approve / request changes / done, edit title, body, project
and tags, budgets, settings, `push`, `sync retry`, sync import and ack as a human, the
pack `apply` executors); `run`, `nested` and `unknown` keep today's limits; still
terminal-only: `serve --bind` off loopback, `key`, `token`, `serve-key` (D-26). The rule
`tq/human-answer` splits in two with the same id: the unconditional half keeps denying
key/token/serve-key, HTTP calls to the decision endpoints and `serve-*.json` reads; the
`ConsoleExempt` half covers `tq hub answer` and the pack `apply` verbs and is skipped for
class `console`; the rule stays locked. Attribution: actor `console:<os user>`, actor kind
`console`, the Claude session id in the activity data, shown on the card page and
`/activity`; gate answers keep `hub:<os user>` inside the HMAC token. Pack answers from the
console are keychain-signed by tq without the typed code; the board page still refuses
them. Console comments stay agent comments.
**Acceptance checks**
- `TestConsoleDrivesTheBoard`: under the console seam, `tq hub answer`, `move <card> "in
  progress"` (enqueues a run for an agent-ready card), `edit --body-file|--title`, `run`,
  `cancel`, `push`, `sync retry` and a pack approval answer succeed; `TestSignedAnswersOnly`
  passes unchanged.
- `TestRunClassCannotAnswer`: classes `run`, `nested` and `unknown` are refused `tq hub
  answer|run|cancel|push|sync retry`, moves into active lanes and budgets, with the class
  named in the error.
- `TestConsoleStaysOutOfTheTerminalOnly`: the console seam is refused `tq hub serve --bind
  0.0.0.0`, `tq hub key|token|serve-key`, `tq mode set`, `tq allow --mode`, `tq guard off`,
  `tq unattended on` and `tq secret set`.
- `TestHumanAnswerRuleIsClassAware` (hook): from a console shell `tq hub answer c d --choice
  approve` is allowed while `tq hub key`, `curl …/answer` and a read of
  `~/.tentaqles/hub/serve-1.json` are denied; from a run shell all four are denied;
  `TestLockedNeverChange` passes.
- `TestConsoleAnswerActorAndSession`: a console answer's actor is `console:<user>`, its
  activity row carries the session id, and the card page shows it.
- `TestHubCommentFromAnAgentSessionIsNeverHuman` passes with a console case: a console
  comment is stored as an agent comment.

### FR-71 Decisions reach the console and are answered there
Why: original PRD §8.2 (`pending` inline), dropped by D-18 and restored by D-24 as a
multiple-choice question the human answers in the session they are already in.
Status: not started
Evidence: decisions have ids (`d` plus 8 hex from the gate, `d-` plus 12 base32 from the
hub) and no number; `tq hub list --waiting` is one-shot; `/tentaqles:hub` prints the
`tq hub answer` command for the human; an OS notification fires
(`TestSendIsBoundedToTwoSeconds`).
Missing: a per-workspace decision number `seq` (unique, assigned in the insert transaction,
backfilled by rowid) in JSON, `tq hub list --waiting`, `show`, the page and the
notification text `Card <id> · <title> · decision #<seq> (<kind>)`; `tq hub wait --json
[--card <id>] [--timeout <dur>] [--session <id>]`, which blocks until a new pending
decision appears or a watched run ends (SSE `/events` plus `/api/waiting` against a served
hub, `ChangeStamp` polling in direct mode; exit 0 on an event, 3 on timeout, 4 when another
waiter holds the session lock, 5 without a hub or board; read-only, every class); a plugin
watcher hook (`plugin/scripts/hub-watch.py`, `async` and `asyncRewake` on `SessionStart`
and `UserPromptSubmit`) that exits 0 at once in run, nested and headless sessions and
otherwise wakes the console with one paragraph when `wait` returns; a prompt-submit line
`hub: N decisions waiting (#seq card title · kind, …)` in console sessions; a deterministic
renderer (`plugin/scripts/hub-decisions.py`) that turns the waiting list into the exact
AskUserQuestion blocks (header `hub #<seq>`, the Card line first, options exactly the
decision's own, the recommended one first, which options need a note, the exact `tq hub
answer` argv); `/tentaqles:hub` asks one decision at a time in `seq` order and answers only
after the human picked; the board stays equivalent (first answer wins).
**Acceptance checks**
- `TestDecisionSeqMonotonic`: numbers are unique and increasing per workspace, survive a
  mirror and a re-read, and `tq hub list --waiting --json` and `show` carry `seq`.
- `TestHubWaitReturnsOnANewDecisionAndOnTheDeadline`: `tq hub wait --timeout 2s` exits 3 at
  the deadline and 0 within one poll of a new decision, printing its id and `seq`; a second
  waiter for the same `--session` exits 4.
- `test_hub_decisions.py`: the same decision JSON renders byte-identical blocks; labels equal
  the decision's options; the Card line is first; `note` is `required` for `answer`, `edit`
  and `request-changes`; the argv is exact; a fifth option goes to an overflow page.
- `test_hub_watch.py`: the watcher exits 0 silently under `TENTAQLES_HEADLESS=1`,
  `TQ_HUB_TASK` or `TENTAQLES_HEADLESS_CHILD=1` and with no hub; on a decision event it
  prints one paragraph naming card, title, `#seq` and kind to stderr and exits 2.
- `test_hooks_json.py`: the watcher appears once in `SessionStart` and once in
  `UserPromptSubmit` with `async`, `asyncRewake` and a `timeout`.
- `TestPromptSubmitHubLine`: in a console session with two pending decisions the
  prompt-submit hook adds one line naming both within 300 ms.
- `TestAnswerRaceWithTerminal` passes: a board answer followed by a console answer reports
  "already answered" and the run resumes once.

### FR-72 The one-command day: `/tentaqles:start`, `/tentaqles:run`, refine writes the card
Why: D-24: start, refine, run and answer from the console; the hub stays the runner (D-17);
the hub must outlive the session that started it.
Status: not started
Evidence: `tq hub serve` runs in the human's own terminal because a Claude session can reap
its background tasks (HUB.md "Quick start"); the desktop app starts it as a child
(`TestOpenBoard_StartsTQHubServeAsAChild`); `/tentaqles:refine` hands the body edit to the
human (FR-42, B §9); there is no `tq hub stop`.
Missing: `tq hub serve --background` (re-exec detached with a scrubbed environment, log at
`$TQ_HOME/hub/serve-<port>.log`, waits for its serve record, prints the address; survives
the caller's exit), `tq hub stop` (a shutdown route authenticated by the local key, then
poll until the ping fails), `tq hub ensure --json` (find or start; prints `{addr, port,
pid, started, waiting}`); all three refuse `run`, `nested` and `unknown`. Skills: `start`
(ensure, address, running cards, the waiting list through the renderer, pending decisions
answered at once), `run <card>` (ensure; an untagged card goes through the refine steps
first; `tq hub move <card> "in progress"`; the card URL; never waits), `refine` step 4
writes the body with `tq hub edit --body-file` and tags the card itself, falling back to
printing the command on a `human-only` refusal; `feature` and `build` headless texts say
the answer is given in the console or on the card.
**Acceptance checks**
- `TestHubEnsureFindsOrStartsABackgroundHub`: with no hub, `ensure` starts one and prints
  its address; a second `ensure` prints the same address and starts nothing.
- `TestBackgroundHubSurvivesTheParentExit`: a `--background` hub keeps serving after its
  starting process exits; `tq hub stop` ends it and removes its serve record;
  `TestShutdownNeedsLocalKey` refuses a shutdown without the local key.
- `TestConsoleRefineWritesBodyAndTag`: under the console seam `tq hub edit <card>
  --body-file f --add-tag agent-ready` succeeds in a planning lane and is refused for a card
  in an active lane.
- `test_console_skills.py`: `start`, `run`, `refine` and `hub` pass the frontmatter test,
  call `tq hub` with `--timeout`, `run` moves an untagged card only after the human chose
  to, and `feature` and `build` never tell the human to run a command.
- COMMANDS.md walkthrough step: one card goes `/tentaqles:start` → `/tentaqles:refine` →
  `/tentaqles:run` → a decision answered in the console → resumed, with no tq command typed
  by the human (verified by hand on this repo).


## 5. Data

| Data | Class | Source | Store | Retention |
|---|---|---|---|---|
| Workspace manifest `.tentaqles.yaml` | internal (names only; loader rejects secret-shaped values) | `tq add`, `tq onboard`, human edits | the client folder (versionable) | life of the workspace; trust hash under `$TQ_HOME/trust` dropped on edit |
| Harness and CLI logins | secrets | the CLIs' own login flows | per-identity config dirs under `~/.tentaqles/identities/<ws>/<id>` | until the CLI logs out; never copied by tq |
| Tracker tokens | secrets | `tq secret set` (human) | OS keychain, service `tentaqles`, account `<ws>/<provider>` | until `tq secret rm` |
| Pack ledger keys and high-water marks | secrets | first human pack answer | OS keychain, service `tentaqles-packs` | life of the workspace |
| Build-gate HMAC key, generation records, evidence | secrets / internal | `build-gate setup` | `$TQ_HOME/build-gate` (key file 0600; a same-user process can read it, AR1) | life of the install |
| `.env` values | secrets | the project | the project; read only through `tq dotenv run`, masked in output | never stored by tq |
| Hub board (cards, decisions, runs, comments, activity, outbox) | internal; may carry client text and personal data from tracker items | tracker mirror, humans, agents | `$TQ_HOME/hub/<ws>/hub.db`, one per workspace, outside every repo | until deleted or archived; Done lane capped at 20 on the board |
| Outbound tracker text | internal, redacted with the workspace's exit classes | hub | the company tracker | the tracker's |
| Memory (`memory.db`), vault export, knowledge graph | internal; personal data masked per `data:` block at every write | sessions, hooks, ingest | per-workspace plugin data; Obsidian vault folder; `meta.db` holds only names, stats, signals | decay and compaction; sessions kept [placeholder: days] |
| Client repos' personal data (CPF, emails, ...) | pii / sensitive / financial per the manifest `data:` block | the client | the client's systems; read locally | never in memory, commits, pushes, PR text, fixtures or model prompts (locked privacy rules) |
| Guard and mode logs, audit log | internal (commands masked) | hooks, `tq env` | `$TQ_HOME/guard/mode.jsonl`, `audit.jsonl` | [placeholder: no retention policy today] |
| Cloud plans | secrets (plans hold values) | `tq pack cloud plan` | outside the repo under `$TQ_HOME` [placeholder: exact path]; agents see a redacted summary | `tq pack cloud prune` |
| Jev (`tq decide`) requests | internal, secret patterns redacted; personal data not masked | tq | TypeSafe (US-hosted, no zero-retention option) or a localhost backend; cache 7 days | vendor policy |
| Telemetry | none | | tq sends none; external tools get `DO_NOT_TRACK=1` and their own switches | |

## 6. Security

The threat model is `docs/threat-model.md` (2026-10-30; next review 2027-01-28). It is
the checklist the `security-reviewer` reads on every diff.

- Boundaries (one STRIDE table each in the threat model): B1 agent session → tq hooks and
  `$TQ_HOME`; B2 human → human-only actions; B3 plugin → build-gate records; B4 pack
  checks → external tools; B5 browser or local process → hub web API; B6 hub runner →
  headless run; B7 tq → trackers and the decide endpoint; B8 GitHub releases → the user's
  machine; B9 contributors and CI → the repo; B10 console session → hub (D-24).
- Authentication: terminal-only actions require a terminal and no agent marker (plus a typed
  binding for pack approvals and `ml promote apply`); from D-24 the console class answers,
  moves and approves packs as the human's proxy (FR-69, FR-70), keychain-signed where today; the hub page uses a Host allow-list,
  Origin checks and a random token stored hashed, loopback by default; hub actor tokens
  are HMAC with the gate key; pack approvals are Ed25519-signed and single-use; the
  tracker credential chain is cli → keychain → env → MCP; release artifacts carry
  checksums and GitHub build provenance; the plugin installs from a tag pinned to a sha.
- Roles: see §2. Agents may add and comment on cards, ask for approvals and run gated
  builds; only humans trust, grant, store tokens and release; a console session answers,
  moves, pushes and approves packs as the human's proxy (D-24, AR3); runs and nested
  sessions never do.
- Abuse cases the guards are built for: an agent steered by text it read edits a ledger,
  reads `.env`, pushes under the wrong account, commits personal data, loosens its own
  mode, starts a nested agent with looser flags, forges an approval, copies back an older
  approval, or installs a plugin from a plain-text URL. Out of scope (accepted risks): a
  program already running as the user (AR1); a determined adversary rewording shell
  commands (AR2).
- Known gaps the model records as open or accepted: the guards are text filters (AR2); a
  hook timeout or parse error fails open; `off` needs no terminal; `tq guard` is not
  human-only; the audit logs have no hash chain; the build-gate key is a file (TM-3
  dropped by decision); the installers are served from mutable `main`; a console answer is
  bound to the human's choice only by the skill protocol until TM-7 (AR3).

## 7. Non-functional requirements

- Platforms: Windows 11 first (cmd.exe, PowerShell 5.1, pwsh, Git Bash), then macOS and
  Linux (WSL2 included). Go 1.26.x; Python 3 for the plugin (stdlib plus pyyaml,
  pathspec, numpy; fastembed optional); Node 22 only to build the desktop app.
- Hooks: every hook declares a timeout (45 s PreToolUse, 30 s SessionStart, 15 s
  UserPromptSubmit, 30 s Stop); capture-only hooks are async; nothing in a hook waits on
  the network or a model; a stalled hook is a P0.
- Guard latency: each pack guard is a pure function budgeted at 50 ms; latency tests
  check linear scaling and a load-tolerant ceiling (testutil.CheckLatency); the gh lookup
  is capped at 5 s; PII scans that exceed their budget ask rather than hang.
- Hub: binds `127.0.0.1:7470`; any other bind needs a token; SSE carries ids only; sync
  interval default 300 s, minimum 60 s; run concurrency default 2, range 1–16; budgets in
  USD, minutes and rounds; OS notification bounded to 2 s.
- External tools: default timeout 120 s, maximum 1800 s; version probe 5 s; attach 30 s;
  output capped at 1 MiB per stream; filtered environment.
- Tests: `go test ./...` green on Windows, macOS and Linux CI; the nine required checks on
  `main` (three Go OSes, plugin, desktop, opa, templates, marketplace-pin, govulncheck);
  the fuzz workflow runs on demand.
- Cost: `tq insights` reports cost and tokens per identity; budgets stop headless runs;
  `tq decide` caches 7 days.
- Offline: everything except tracker sync, decide calls and provider logins works
  offline (original PRD §14 constraint; not separately tested [placeholder]).
- Accessibility: the board has a keyboard alternative to drag (Move menu) and a theme
  toggle; no WCAG level is claimed [placeholder].

## 8. Data model sketch

A **base folder** holds **workspaces** (client folders), each with one manifest and
several **identities** (one per CLI or harness login, each a config directory). A
workspace's manifest is **trusted** by hash and may carry a **mode grant**. A workspace
owns one **hub board** with **lanes**, **cards** (kind, tags, parent, external link to a
tracker item, project folder), **comments**, **decisions** (kind, options, answer, actor,
time), **runs** (budget, rounds, status), **activity**, **sync state** and an **outbox**.
A card may be linked to a tracker item (mirrored) or local only; a local card may be
pushed and then linked. A run executes an **on-ramp** chosen by the card's kind inside a
**project** (a folder with a `tq-project` block), producing a **checkpoint plan**,
**gate records** (verify, review, DoD; signed), **decision records** mirrored to the card,
and a **review** rendered from those records. **Packs** add checks, guards, a signed
**approval ledger** per workspace and **environments** resolved by envgate. The plugin
keeps one **memory store** per workspace and a global **meta** store of names, stats and
signals only.

## 9. Stack and constraints

- `tq`: Go (1.26.x), cobra CLI, pure-Go SQLite (no cgo: `pg_query_go` was rejected for
  that reason), go-keyring for the OS keychain; no npm in the hub (server-rendered pages
  plus small static files); Job Objects on Windows for tool trees.
- Plugin: Python, runtime bootstrap through `tq_run.sh` → `tq_env.sh`; stdlib-only for
  the workflow package (`gate.py`, `loop.py`, `project.py`, `prd.py`); pyyaml, pathspec,
  numpy required; fastembed optional and never installed in CI (so it stays optional).
- Desktop: Wails v2, React frontend, shares `internal/` through `setupapi`; NSIS, DMG and
  AppImage installers.
- Releases: goreleaser, Homebrew tap and Scoop bucket by deploy keys, GitHub attestations,
  actions pinned by sha, Dependabot without major action bumps, `govulncheck` required.
- Decisions model: Jev (TypeSafe, `/v1/systemone`) direct, not through a gateway; Laya
  was evaluated and discarded (owner decision, 2026-10-09).
- Constraints that hold everywhere: credentials never in manifests; nothing copied from
  source kits (templates written fresh under MIT); hooks never stall; mode changes and
  approvals are human-only; one database per workspace and never joined; `off` is never
  stored in a file; the repo-level pack file can only tighten; never say "unattended" for
  the modes (the word names the replaced feature).

## 10. Open questions

None. The 23 questions this document raised (D-01 to D-23) were put to the repo owner on
2026-10-30 and answered; each FR they touch names its decision, and §17 lists the
outcomes. A new question goes here as `- NEEDS-DECISION: ...` and makes the PRD not
planning-ready until it is settled.

## 11. Releases

Shipped releases are listed with the FRs they delivered (an FR marked partial in §4
appears again in the planned release that completes it). "Done when" for a shipped release
is what its spec required; for a planned release it is the acceptance checks of its FRs.

| Release | FRs | Done when |
|---|---|---|
| Release 0 | FR-01, FR-02, FR-04, FR-05, FR-09, FR-13, FR-15, FR-16, FR-17, FR-25, FR-29, FR-56, FR-64, FR-65, FR-66 | tq core through v0.7.0 (shipped): identity per folder, trust, guard, bundles, migrate, doctor, settings render, insights, decide in shadow |
| Release 1 | FR-24, FR-26, FR-41, FR-45, FR-50, FR-51 | Development-process spine plus security and privacy, plugin 0.12.0 (shipped 2026-10-21, PRs #28–#30) |
| Release 2 | FR-10, FR-14, FR-30, FR-31, FR-32, FR-33, FR-34, FR-35, FR-36, FR-37, FR-38, FR-39, FR-42, FR-44, FR-46, FR-47, FR-48, FR-49, FR-54, FR-57, FR-58, FR-59 | Hub core (spec A, PRs #31, #33, #34) and board sync (spec B, PR #46), plugin 0.13.0–0.14.0 (shipped 2026-10-21 to 2026-10-28) |
| Release 3 | FR-55, FR-67 | Data and ML packs (spec C) and cloud and db packs (spec D, counted as release 4 by the specs), one PR #50, tq v0.8.0, plugin 0.15.0 (shipped 2026-10-29) |
| Release 4 | FR-18, FR-19, FR-20, FR-22, FR-23, FR-27, FR-28 | Guard modes (spec E, PR #49, tq v0.8.0) and the hardening of v0.8.1–v0.8.2: guard clear-cut fixes, threat model, checksums and provenance (shipped 2026-10-29 to 2026-10-30) |
| Release 5 | FR-69, FR-70, FR-71, FR-72, FR-40, FR-36, FR-58, FR-47, FR-42, FR-48, FR-54, FR-57, FR-22, FR-20, FR-18, FR-26, FR-68 | The console drives the board (session classes, console answers, decisions reach the console, the one-command day); close the loop: a card becomes a project and comes back approved on the company board; decisions carry recommendation and blocked; headless asks are rule-aware; the human-only gaps are closed; the modes are renamed and `guarded` skips force-push and system changes; the PII-under-bypass report is verified and fixed; the docs match the code |
| Release 6 | FR-06, FR-12, FR-08, FR-03, FR-11, FR-10, FR-21, FR-28, FR-30, FR-32, FR-33, FR-35, FR-41, FR-51, FR-53 | Isolation completeness: shim folders and CLI provisioning, Chrome and Edge profiles, prompt and title indicator, banner at onboarding, full onboarding prompts and the terminal close, the four new categories with production tags, OpenCode, the Markdown card mirror, tracker status on tiles, read-only assignee/attachments/comments, publish with lane and tags, the PRD accept command, optional gates configurable |
| Release 7 | FR-52, FR-63, FR-62, FR-56, FR-07, FR-60, FR-04 | Evals and knowledge: evals gate and checklist evals (then the visual review gate on top), capture, the KB pipeline starting with docs folders and the tracker, Jev families promoted to enforce where the eval allows, stack preferences resolved, GUI manifest editing and switching, Kimi Code |

## 12. Status map against the original PRD

| Original PRD section | Requirement in short | FRs | Status |
|---|---|---|---|
| §0 Discovery-first gate | Read the repo, map requirements, adopt codebase names, stop on permission conflicts | this document; §15 | done (this document is the discovery output; the permission conflicts were settled as D-09a to D-09c, D-10, D-12 and D-18, see §17) |
| §1 Product summary | Identity manager; one folder per client; board; orchestrators; review; modes | all | partial overall |
| §2 Glossary | Terms | §15 | done, mapped to code names |
| §3.1 Terminal | Identity-scoped session; unselected CLIs unreachable; indicator | FR-02, FR-06, FR-03 | partial / not started / partial |
| §3.2 Harness sessions | Separate login per identity for four harnesses | FR-04, FR-28 | partial (Claude done, Codex best effort, OpenCode dirs only, Kimi absent) |
| §3.3 Platform connections | Per-identity credentials for git, cloud, data, email, chat, tasks | FR-05, FR-09 | done with gaps (chat, email through MCP; wording conflict) |
| §3.4 Stack and preferences | Declared and resolved per identity | FR-07 | partial |
| §3.5 Browser profiles | One profile per identity, used by all URL opens and automation | FR-08 | not started |
| §4.1 Preconditions | Run in the client folder; refuse an existing manifest | FR-01, FR-10 | done (no `tq edit`) |
| §4.2 Banner | ASCII banner, flags | FR-11 | not started |
| §4.3 Prompt schema | Ten prompts with Skip and Other | FR-10 | partial |
| §4.4 Provisioning | Manifest, credential store, CLI install, browser profile, registration, new terminal, close old | FR-10, FR-12, FR-08 | partial (install, browser, close missing) |
| §4.5 Non-interactive | `--from` | FR-10 | done (answers file, on `tq onboard`) |
| §5.1 Plugin availability | Pre-registered per harness | FR-13 | partial (Claude only) |
| §5.2 Onboarding skill | Drives auth, MCP registration, verification | FR-14 | partial, different design |
| §5.3 Capabilities | Read chat and email, read and write tracker, git | FR-05, FR-31, FR-34, FR-37 | done through MCP and adapters [placeholder: chat and email read paths not audited] |
| §6.1 Sync rules | Lanes, schedule, conflicts, plain-file storage | FR-31, FR-32, FR-33, FR-30 | done / partial / partial / conflict |
| §6.2 Card types | Mirrored vs local; publish | FR-30, FR-35 | done / partial |
| §6.3 Lane transitions | Start and all-done moves | FR-36 | partial |
| §7.1 Scaffold | Project from a card | FR-40, FR-43 | not started / partial |
| §7.2 PRD | Draft and accept | FR-41 | done, vocabulary conflict |
| §7.3 Task breakdown | Cards, refine, agent-ready | FR-42 | done with gaps |
| §8.1 Orchestrator contract | One per card, workers, progress, mode | FR-44, FR-45, FR-46, FR-49 | done / done / partial / done |
| §8.2 Decision requests | Record, pause, `pending`, timestamp | FR-47 | done with gaps; inline answering conflict |
| §8.3 Concurrency | Parallel runs, serialized files, worktrees | FR-44 | done |
| §8.4 Other triggers | Type selects workflow | FR-48 | done with gaps |
| §9 Gates | Build, review, tests, evals, security, quality; per-project; on card; notify | FR-50, FR-51, FR-52, FR-53, FR-54 | done / partial / not started / partial / partial |
| §10 Review | Page, feedback, approve, transport | FR-57, FR-58 | done with gaps / partial |
| §11 GUI | Browser board; native GUI; identity profile; no cross-identity | FR-38, FR-59, FR-60, FR-08, FR-39 | done / done / partial / not started / conflict |
| §12.1 Categories | Eleven categories | FR-21 | partial (four missing, five extra) |
| §12.2 Modes | bypass, guarded, supervised, custom | FR-18 | done under other names; two cells differ |
| §12.3 Locked | Never skip; human-only changes | FR-19, FR-20 | done / done with gaps |
| §12.4 Ask without a human | Deny, record, retry | FR-22 | partial |
| §12.5 Harness flags | Shims inject flags | FR-28 | partial (Claude and Codex shims; no auto mode) |
| §13 Raw escape | Session-scoped no-tq, human-only, marker | FR-23 | done with gaps |
| §14 Non-goals and questions | | §3.2, §17 | carried over; the nine original questions were settled on 2026-10-30 as D-01, D-02, D-22, D-18, D-03, D-09a, D-09c, D-11 (`tq raw` name settled as `tq off`) |
| §15 Execution plan | Phases 0–8 | §11, §13 | phase 1 (modes) and most of 4, 6, 7, 8 shipped; phases 2, 3, 5 partial |

## 13. Prioritized backlog

Priority: P0 closes the end-to-end loop or a security gap; P1 completes isolation, the
product's core promise; P2 depth and breadth; P3 vision items with no dependency on the
rest. Each row names its FR, why it sits where it does, and what it depends on.

| # | Priority | Item | FR | Why this priority | Depends on |
|---|---|---|---|---|---|
| B-01 | P0 | Start a project from a card (scaffold, two-way link, `move_on_start`) | FR-40, FR-43 | The one missing step between "assigned card" and "run"; everything downstream exists | none (D-02 decided: the card mirror serves as `tasks/`) |
| B-02 | P0 | Queue the parent's review move on the company board when all children are done | FR-36, FR-58 | Last step of the approve path; small change in the outbox | none |
| B-03 | P0 | Close the human-only gaps: plain `tq allow`/`tq deny`/`tq guard` refuse agents in the binary; terminal plus typed confirm for `off` and `mode toggle|override` | FR-20, FR-23 | Security-sensitive; the original PRD §12.3 requires it; cheap | decided (D-10) |
| B-04 | P0 | Verify locked PII rules under Claude `bypassPermissions` in a real session; fix | FR-26 | An open report from the first test run on a locked group | decided (D-13) |
| B-05 | P0 | Fix the doc-vs-code mismatches and stale statements; track COMMANDS.md and IDEATION.md | FR-68 | Operators act on the docs; three mismatches found today | none |
| B-06 | P0 | Rule-aware headless asks: rule id on the decision, proven retry | FR-22 | Without it headless runs either hang on a confirm or deny silently; many cards run at once from the console (D-24) | prototype of settings-allow vs hook ask (D-12 decided) |
| B-07 | P1 | Decision fields `recommendation` and `blocked`; imported cards can get kind and tags | FR-47, FR-48 | The original PRD contract for decisions; imported cards today can only run `feature` | none |
| B-08 | P0 | Card dependencies and a hub check that refuses to start a blocked card | FR-42 | Needed before many cards run in parallel from one PRD, which the console makes routine (D-24) | none |
| B-09 | P1 | Gate status on the card tile; gate failure comment on the card; terminal bell; review page links (branch, diff, card) and open items | FR-54, FR-57 | Makes the board and the page the single place to look | none (D-22 decided) |
| B-10 | P1 | Record one real end-to-end run against one real tracker | FR-58 | Proves G3; today proven only with a fake Claude | B-01, B-02, a real tracker token |
| B-11 | P1 | Browser profile per workspace: Chrome and Edge first, for `tq hub open|review` and Claude in Chrome / Playwright | FR-08 | Named in the vision's first paragraph; blocks GUI-in-profile and login pages in the client's browser | decided (D-06) |
| B-12 | P1 | Shim folder per workspace so unselected CLIs are unreachable; CLI install or link into it | FR-06, FR-12 | The vision's first bullet; also what makes "AWS-only terminal" true | a Windows prototype (D-05 decided) |
| B-13 | P1 | Prompt prefix and window title; the onboarding banner with `--no-banner` | FR-03, FR-11 | Low effort, high clarity | decided (D-01) |
| B-14 | P1 | Onboarding prompts for browser, mode and shell; Other with free text; multi-select data platforms; resume after a failed step; close the originating terminal after the new one opens | FR-10 | Completes §4.3 and §4.4 of the original PRD | B-11 for the browser prompt to mean something (D-07 decided) |
| B-15 | P1 | Missing categories: filesystem-destructive, package-install, network-egress, browser-actions; production environment tags via envgate | FR-21 | Four of the original PRD's eleven categories; browser-actions needs B-11 | decided (D-09b) |
| B-16 | P2 | Tracker status name on card tiles; store assignee, attachments and tracker comments read-only | FR-32, FR-33 | Fidelity of the mirror without changing the lane and edit rules the owner kept | none (D-14, D-15 decided) |
| B-17 | P2 | `tq hub push --lane --tag` | FR-35 | Adapters already support it; small | none |
| B-18 | P2 | DoD items for coverage, secrets scan and format; `gates:` list in `tq-project` for optional gates; hotfix skips review and evals | FR-51, FR-53 | Moves CI-only checks into the gate the card trusts | decided (D-20) |
| B-19 | P2 | OpenCode hook, launcher and guard; then Kimi Code once its hook support is checked; Codex hub adapter | FR-28, FR-04 | Vision lists four harnesses; spec E defers these | decided (D-11) |
| B-20 | P2 | Periodic progress comment on the card every N minutes | FR-46 | Original PRD §8.1; small | none |
| B-21 | P2 | Evals gate on cards: commands with thresholds or a checklist, with retries | FR-52 | Original PRD §9; needed before checklist evals and the visual review gate | decided (D-19) |
| B-22 | P2 | Jev families to enforce where the eval allows; grow seed sets to ~100 per family | FR-56 | Shadow since 2026-10-09; the gate to enforce is written | two weeks of shadow logs per family |
| B-23 | P2 | Stack preferences resolved per workspace; GUI manifest editing and terminal switcher | FR-07, FR-60 | Completeness of §3.4 and §11 | none |
| B-24 | P3 | Capture from outside a terminal; checklist evals for non-code cards | FR-63 | Owner backlog; extends the board to all work | B-21 |
| B-25 | P3 | Knowledge-base ingestion pipeline with distillation and hybrid retrieval, docs folders and the tracker first | FR-62 | Owner backlog; largest new build; needs its own spec | FR-61 (D-21 decided) |
| B-26 | P3 | Signing `trust/`, `guard.yaml`, `config.yaml`; hash-chained audit logs; installers served from a tag | FR-27, FR-64 | Threat-model follow-ups; AR1 and AR2 accepted until 2027-01-28 | none |
| B-27 | P3 | Deferred pack scope: Kubernetes/Helm, Infracost, state drift, lineage, Spark estimates, WebAuthn answers on the board | FR-55 | Breadth; each is its own small spec | none |
| B-28 | P0 | Rename the modes (`off`→`bypass`, `default`→`supervised`, `relaxed`→`guarded`, keep `strict`, `custom`) with one release of aliases; `guarded` also skips git-history and system | FR-18 | Owner decision; a behaviour change in a security layer, so it ships with tests for every cell | decided (D-09a) |
| B-29 | P1 | Read-only Markdown mirror of cards in the client folder | FR-30, FR-43 | Versioning and direct reads without a second source of truth | none (D-02 decided) |
| B-30 | P2 | Human-only `accept` command that stamps a project PRD; planning refuses an unaccepted PRD | FR-41 | Keeps the FR format and adds the explicit developer sign-off the original PRD wanted | none (D-16 decided) |
| B-31 | P3 | Visual review gate on top of the evals mechanism | FR-52 | A loose end between two specs; parked after evals | B-21 (D-23 decided) |
| B-32 | P0 | Session classes in tq: environment plus process ancestry, the test seam, the class header; `unknown` refused like `run` | FR-69 | Everything in D-24 keys off it; a wrong class is a security bug, so it ships with the matrix test | none (D-24 decided) |
| B-33 | P0 | Console policy in the hub and the API; class-aware `tq/human-answer`; pack approvals answered from the console; actor `console:<user>` and the session id on the page | FR-70, FR-20 | The human stops typing tq commands while the terminal-only list (D-26) stays closed | B-32, B-03 |
| B-34 | P0 | Decision `seq`; `tq hub wait`; the asyncRewake watcher; the renderer script; `/tentaqles:hub` answers after AskUserQuestion; the prompt-submit line | FR-71 | Decisions reach the console in minutes, not when the human remembers to look | B-32, B-33 |
| B-35 | P0 | `tq hub serve --background`, `ensure`, `stop`; skills `start` and `run`; `refine` writes body and tag | FR-72 | The one-command day end to end | B-33, B-34 |
| B-36 | P1 | TM-7: bind each console answer to a signed AskUserQuestion record; deny `tq claude-hook` from the Bash tool | FR-70 | Turns the skill protocol into a check before AR3 expires (2027-01-28) | B-34 |

## 14. Conflicts and flags

Every place the original PRD and the code disagree, or a doc contradicts the code. Each
row names the decision (taken 2026-10-30, outcomes in §17) or the backlog item that
settles it.

| # | Flag | Where | Settled by |
|---|---|---|---|
| C-01 | "Identity" means workspace in the code; the code's identity is one CLI login | §15 | adopted here |
| C-02 | "Trust store" means a credential store in the original PRD; the code's trust store is the manifest hash | FR-09, §15 | D-04 |
| C-03 | Mode names differ; relaxed asks for git-history and system; three self-protection rules ask | FR-18, FR-19 | D-09 |
| C-04 | Four PRD categories do not exist; five code categories are not in the PRD | FR-21 | D-09 |
| C-05 | Plain `tq allow` is not human-only in the binary (COMMANDS.md says it is); `tq guard` is not human-only; `off` needs no terminal | FR-20, FR-23, FR-68 | D-10 |
| C-06 | Board is SQLite outside the repo, not files in the identity root; agents read through `tq hub` | FR-30, FR-43 | D-02 |
| C-07 | Local text edits beat the company board and are pushed back | FR-33 | D-15 |
| C-08 | Five fixed lanes; tracker statuses collapse by kind | FR-32 | D-14 |
| C-09 | The parent's all-children-done move reaches the company board only on approval, and not at all today for the parent | FR-36, FR-58 | B-02 |
| C-10 | Control plane is `tq hub serve`, rounds are processes, worktree named by slug | FR-44 | D-17 |
| C-11 | `tq hub answer` refuses Claude sessions; no inline `pending` answers | FR-47 | D-18, superseded by D-24 (FR-71) |
| C-12 | Headless asks are tool-level allow-once, not guard-aware | FR-22 | D-12 |
| C-13 | PRD vocabulary is FR-nn and NEEDS-DECISION; "accepted" is a grade | FR-41 | D-16 |
| C-14 | No `project.yaml`; `tq-project` block plus `.tentaqles.yaml`; gate order fixed; hotfix skips the critic, not evals | FR-43, FR-53 | D-20 |
| C-15 | `tq onboard` never closes the terminal it ran in | FR-10 | D-07 |
| C-16 | The onboarding skill prints logins instead of driving them; credential order differs | FR-14 | D-08 |
| C-17 | Cross-workspace hub and dashboard exist against the original non-goal | FR-39, §3.2 | D-03 |
| C-18 | Kimi Code absent; OpenCode unguarded; no Claude auto mode | FR-04, FR-28 | D-11 |
| C-19 | `tq login <cli>` and `tq hub cancel <card>` in COMMANDS.md differ from the code's arguments | FR-68 | B-05 |
| C-20 | Root CLAUDE.md marks `providers/` and `desktop/` planned and lists two adapters | FR-68 | B-05 |
| C-21 | Spec placement of the UI gate differs between DP §13 and spec C | §17 | D-23 |
| C-22 | TEST_RESULTS_RUN01: preamble not visible in one session; PII rules under bypass | FR-03, FR-26 | D-13; [placeholder: preamble cause] |
| C-23 | `tq hub sync pending` is the sync fallback queue, not decision requests | FR-37, FR-47 | naming kept; documented here |

## 15. Naming map and glossary

Code names are used throughout this document. Original PRD names are listed for mapping.

| Original PRD | Code | Note |
|---|---|---|
| Identity | workspace (`TQ_WS`, the client folder with `.tentaqles.yaml`) | the code's **identity** is one CLI login inside a workspace (`identities.<id>`, `tq login <ws> <id>`) |
| Identity root | workspace root (`TQ_WS_ROOT`), a child of a base folder (`tq init <base>`) | |
| Manifest | `.tentaqles.yaml` (`tentaqles-client-v1|v2`) | top-level blocks: client, git, identities, claude (permission_mode, bundle, permissions, env), blocked_commands, cloud, database, stack, guard, decision, data, tracker, chat, email, data_platform, agents, packs |
| Trust store | (credentials) per-identity config dirs plus the OS keychain (`tq secret`); (code's trust store) `$TQ_HOME/trust/<hash>` written by `tq allow` | D-04 |
| Mode: bypass / guarded / supervised / custom | today off / relaxed / default / custom, plus strict; renamed to the PRD's names by D-09a (B-28), `strict` kept | `tq mode`, `guard.mode`; grant by `tq allow --mode` |
| Guard | tq PreToolUse hook: `guard.Decide` plus policy rules, 13 categories, 75 rule ids | `tq/...`, `guard/...`, `jev/...`, `plugin/...` ids |
| Harness | agent CLI: claude, codex, opencode (catalog), plus gemini, cursor, copilot, goose, cline, continue | Kimi absent |
| Tentaqles plugin | `plugin/`, installed as `tentaqles@tentaqles` from the root marketplace | 59 skills, 13 agents |
| Onboarding skill | `/tentaqles:onboard` | prints logins; `tq onboard` is the CLI wizard |
| Company board | tracker (`tracker:` block; adapters asana, azdo, github, jira, linear) | |
| DCB | hub board (`tq hub`, `hub.db` per workspace) | |
| Mirrored card / local card | linked card (`external_provider`, `external_id`) / card without a link | `publish` = `tq hub push` |
| Project | a folder with a `tq-project` block in its `CLAUDE.md`; `cards.project_dir` | no `project.yaml` |
| Orchestrator | hub run (rounds of `claude -p` executing an on-ramp skill and `/tentaqles:build`) | statuses queued, running, waiting, finished, failed, cancelled, over-budget |
| Decision request | decision (kinds plan-approval, result, judge, question, permission) | |
| `pending` | `tq hub list --waiting`, `/waiting`, `/tentaqles:hub` | `tq hub sync pending` is unrelated |
| Review page | `tq hub review`, `GET /cards/{id}/review` | |
| Human-only command | terminal-only (`requireHuman`, class `terminal`) versus console-allowed (FR-70); `tq/human-answer` becomes class-aware | D-24, D-26 |
| `tq init` | `tq init <base>` (register a base folder) plus `tq onboard` / `tq add` (create a workspace) | |
| `tq raw` / `tq off` | `tq off` / `tq on` shell functions; `claude-off`, `codex-off`; `tq run --off`; `TQ_MODE=off` | `TQ_ENABLED=0` stops only shell activation |
| Agent-ready | the `agent-ready` tag (`store.TagAgentReady`) | the original PRD's `ready` flag |
| Gate / quality gate | build-gate records (verify, review, DoD), pack checks, CI ratchet | |
| Worker subagents | plugin agents (planner, critics, fixer, arbiter, breaker, reviewers) plus `general-purpose` subagents | |

Other terms used here: **on-ramp** (`/kickoff`, `/feature`, `/fix`); **headless** (a run with
`TENTAQLES_HEADLESS=1`); **checkpoint** (a plan unit: tests → lock → implement → verify →
review → fixer); **critic** / **fixer** / **arbiter** (reviewer roles); **crew** (reviewers
selected by triage flags; `tq pack crew` adds pack reviewers); **triage flags** (from
`tq decide triage`); **DoD** (definition of done); **outbox** (queued tracker writes under
`off|ask|auto`); **watermark** (sync cursor); **high-water mark** (keychain value retiring
older pack approvals); **ledger** (append-only approval events); **baseline** (ratchet
file for external-tool findings); **envgate** (environment resolution for cloud and db
gates); **exit classes** (the data classes redacted at every exit, from the `data:` block).

## 16. Runbooks and operations index

| Task | Where |
|---|---|
| Install tq (one-liners, Homebrew, Scoop, desktop) and verify a download | `docs/DESKTOP.md` Install; `docs/RELEASING.md` What the tag publishes |
| Set up a machine from a plan | `docs/SETUP.md` Preview, tool check and apply |
| Onboard one client | `docs/SETUP.md` One client; `docs/COMMANDS.md` §1 Setup and workspaces |
| Keep identities current after an upgrade | `docs/CLAUDE-SETTINGS.md` (`tq settings render --all`, `claude plugin update`, Codex `/hooks`) |
| Check an install end to end (15 steps) | `docs/COMMANDS.md` §4 Test walkthrough; `docs/TEST_RESULTS_RUN01.md` for a recorded run |
| Migrate a hand-built setup; undo it | `docs/MIGRATE.md` Steps; Undo |
| Turn tq off for one window or launch | `docs/CLAUDE-HOOK.md` Launchers, `tq off` and `tq on` |
| Choose and grant a guard mode; see what it skipped | `docs/SETUP.md` Guard modes; `docs/CLAUDE-HOOK.md` Guard modes; `tq mode log` |
| Use a `.env` or redact output | `docs/CLAUDE-HOOK.md` Using a .env without printing it; `tq redact` |
| Run the board; sync a tracker; answer decisions; push a card | `docs/HUB.md`; `docs/COMMANDS.md` §1 Hub board |
| Gated production change (plan, approve, apply); db migration gate; restore drill; model promotion | `docs/packs/cloud-apply-gate.md`; `docs/packs/db-migrations.md`; `docs/packs/db-restore-drill.md`; `docs/packs/ml-promotion.md` |
| Build a feature, fix a bug, ship a PR, babysit CI | skills `prd`, `kickoff`, `feature`, `fix`, `build`, `ship`, `babysit-pr` (`docs/COMMANDS.md` §2) |
| Release tq and the plugin | `docs/RELEASING.md` Steps (release PR, merge, tag, workflow, pin PR) |
| Review the security posture | `docs/threat-model.md`; `docs/GUARD-SCOPE.md`; `docs/packs/framework-security.md` |
| Read cost and health across identities | `docs/INSIGHTS.md` |

## 17. Decisions record

Decisions taken in the design specs and in the sessions of 2026-10-29 and 2026-10-30, in
one place. Sources: DP = development-process design; A, B, C, D, E = the hub core, board
sync, data/ml packs, cloud/db packs and guard modes specs; TM = threat model.

| Decision | Reason | Source |
|---|---|---|
| One engine, three on-ramps; packs are optional overlays | one gate proves every change | DP §1 |
| Security and privacy fail closed; quality is evidence-gated with a logged human override | controls a model cannot argue with | DP §1–2 |
| Reproduce first; `--hotfix` skips the critic, never the test | no fix without a repro | DP §5.3 |
| PII: read freely, guard the exits | client-data work must stay practical | DP §11.2 |
| No automatic secret rotation | prevention only | DP §2 |
| Hub modelled on Paperclip's product stance, written in Go, not adopted or forked | legal reference only | DP §13; A §1 |
| Decisions and approvals as records; headless mode exits on a human stop and the hub resumes | the board reads records, never transcripts | DP §5.4 |
| One database per workspace, outside the repo; hub holds no credentials; never joined | isolation | A §1, §15 |
| The human is the only authority: hub only calls `build-gate answer`; agents may add and comment | engine rules stay in one place | A §1, §9.2 |
| Two topologies, `serve` per workspace and `--all`, behind a switch | owner decision | A §1 |
| Trackers ADO, Jira Cloud, Linear, Asana, GitHub; credential order cli → keychain → env → MCP | owner decision | B §2, §7 |
| Outbox per write `off|ask|auto`; moves and result comments auto, creates/edits/closes ask | owner decision | B §5 |
| Polling with a watermark; no webhooks; sync moves never start a run; tracker wins lanes | no public URL; safety | B §3–4, §11 |
| `tq secret` and card title/body edits human-only; `refine` hands the body edit to the human | integrity of the board | B §8–9 |
| Only the manifest enables a pack; the repo file tightens only; config frozen at approval | the repo layer is untrusted | C §2.3 |
| Pack approvals signed (Ed25519, keychain), single-use, terminal-only until WebAuthn | board answers refused until a human proof exists | C §2.11; D §4 |
| No new Go dependency needing cgo (`pg_query_go` rejected) | portability | C §2.4 |
| Prod apply needs a bound single-use approval (60 min default, 24 h max); unresolved target is strictest | fail closed | D §1, §24 |
| Five guard modes; `default` when unset; `off` never stored | no file edit can turn a workspace off | E §1, §4.1 |
| Locked groups identity, secrets, self-protection, privacy | privacy added at spec review | E §19 Q1 |
| Relaxed asks for git-history and system; strict denies prod, cloud, git-history | owner ruling | E Q2 |
| Looser modes need a hash-keyed grant; precedence global → workspace → project → launch | owner ruling | E Q6 |
| The launcher always starts Claude in bypass so tq's mode is the single policy layer | owner's standing preference | E Q3 |
| Codex is best effort; its asks become denies | Codex hooks fail open on ask | E §12 |
| The state-folder guard is a seatbelt for honest slips, not a security boundary; fix only clear small holes with tests | five review rounds each found new bypasses; carve-outs failed open | GUARD-SCOPE; session 2026-10-30 |
| Build-gate key stays a file; TM-3 dropped | agent build steps must sign; 0.2 s per call; AR1 covers the rest | TM Decisions |
| AR1 (same-user programs) and AR2 (text guards) accepted until 2027-01-28 | out of scope; slips, not adversaries | TM Accepted risks |
| Checksums first, then provenance; installers verify both | cheap first, then a seal | TM; session 2026-10-30 |
| Actions pinned by sha; `govulncheck` required; Dependabot skips major action bumps | release steps run only on a tag, so PR checks cannot exercise them | session 2026-10-30 |
| Jev stays (Laya discarded); TypeSafe direct, not through a gateway | owner decision 2026-10-09 | memory note |
| Dev workspace loads the plugin from the local folder; every other identity from the pinned release | development loop vs stability | session 2026-10-29 |

Decisions of 2026-10-30 and 2026-10-31 on this document (the repo owner answered each
question in §10 as it stood; the FRs they touch carry the same `D-nn`):

| # | Decision | Outcome |
|---|---|---|
| D-01 | Identity indicator and banner | Prompt prefix `[client]` plus the window title in every shell, `[off]` after `tq off`; the banner shows once, at onboarding |
| D-02 | Board storage | SQLite stays the source of truth; a read-only Markdown mirror (one file per card plus an index) in the client folder |
| D-03 | Cross-client views | Keep `tq hub serve --all` and the dashboard; the non-goal is mixing clients' data |
| D-04 | Credential store | Per-identity config dirs plus the OS keychain with per-workspace names is the design; "trust store" means manifest trust |
| D-05 | Unselected CLIs and provisioning | A shim folder per workspace first on PATH, removed by `tq off`; global-only tools recorded in the manifest |
| D-06 | Browser profiles | Chrome and Edge first, for tq's pages and for Claude in Chrome / Playwright; Safari and Opera later |
| D-07 | Terminal after onboarding | Close the originating terminal, only after the new one opened; on failure keep it and print the failing step |
| D-08 | Onboarding skill | Logins and `tq secret` stay human-only; the skill prints and verifies, and later opens login pages in the client's browser profile |
| D-09a | Mode names and cells | Rename to bypass / supervised / guarded / custom, keep strict; `guarded` also skips git-history and system; aliases for one release |
| D-09b | Missing categories | Add filesystem-destructive, package-install, network-egress, browser-actions, and production environment tags via envgate |
| D-09c | Locked groups in custom | Locked stays locked; no `ask` for locked groups |
| D-10 | Human-only gaps | The binary refuses `allow`, `deny` and `guard` changes from agents; `off` and `mode toggle|override` need a terminal and a typed yes; since D-24 "agent" here means every class but `terminal`, the console included |
| D-11 | Harness scope | OpenCode next, Kimi Code later; Codex best effort; no Claude auto mode |
| D-12 | Headless asks | Rule-aware decisions and a proven retry |
| D-13 | PII rules under bypass | Verify in a real session and fix, P0 |
| D-14 | Lanes | Five typed lanes stay; the tile shows the tracker's own status |
| D-15 | Card text conflicts | Local edits win and tq asks before pushing; assignee, attachments and comments stored read-only |
| D-16 | PRD format | Keep FR-nn and NEEDS-DECISION; add a human-only `accept` command |
| D-17 | Control plane | The hub stays the runner; worktrees keep the slug with the card id in the plan |
| D-18 | Answering decisions | Never from inside an agent session; superseded by D-24 (2026-10-31): never from a run, nested or unknown session, while the console answers |
| D-19 | Evals | One `evals` mechanism for commands with thresholds and for checklists, with retries |
| D-20 | Per-project gates | Optional gates configurable, core gates fixed; hotfix skips review and evals, never the regression test |
| D-21 | Knowledge base | Docs folders and the tracker first |
| D-22 | Notifications | OS notification plus a terminal bell |
| D-23 | Visual review gate | Later, after evals |
| D-24 | Console sessions | The human's interactive top-level `claude` (class `console`) answers decisions and starts, moves, edits and refines cards as the human's proxy after a multiple-choice question; supersedes D-18 and the parts of D-10 and B §9 that treated every Claude session alike; console comments stay agent comments; the hub stays the runner (D-17); residual risk AR3, hardening TM-7 |
| D-25 | `!` commands | A command typed with Claude Code's `!` prefix runs in the console and counts as the human for board work; it is not a terminal for D-26 |
| D-26 | What stays at the terminal | `tq allow`, `tq mode`, `tq guard`, `tq unattended`, logins (D-08), `tq secret` writes, `tq hub serve --bind` off loopback, `tq hub key|token|serve-key`: refused from every class but `terminal`. Pack approvals are answered from the console too, keychain-signed by tq; the board page still refuses them |

Decisions of 2026-10-31 (D-24 to D-26) were taken while planning the console; they
supersede D-18 and the agent-session wording of D-10, whose refusals stand for `console`,
`run`, `nested` and `unknown` as D-26 lists them. The threat model's B10 and AR3 record the
boundary and the accepted risk.
