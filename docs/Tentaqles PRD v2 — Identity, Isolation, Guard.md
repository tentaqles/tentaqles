# Tentaqles PRD v2 — Identity, Isolation, Guard

Nov 4, 2026 · @Renato Nery Domingues

Revised Nov 6, 2026:
- Claude Code is the only AI harness.
- The plugin holds only the onboarding skill.
- Project bootstrap, PRD and task generation, orchestration, and quality gates are removed.
- The CLI is `tentaqles`, with `tq` as an alias.
- `tentaqles reach` adds a client folder and starts its onboarding.
- `tentaqles off` turns an identity's guard off, for the current and every new session, until `tentaqles on`.

## 0. How to read this document

This PRD is the instruction set for an implementing AI agent working inside the existing Tentaqles repository. Treat every section as a requirement, not as a narrative. It supersedes PRD v1. The product is now only three things:

- an identity manager;
- an environment isolator;
- a deterministic guard;

plus one Claude Code plugin that holds the onboarding skill. Everything v1 said about review pages, GUIs, permission modes, project workflows, and harnesses other than Claude Code is out of scope (Section 10).

**Conventions**

- MUST / MUST NOT: hard requirement. Violating one fails the task.
- SHOULD: default behavior. Deviate only with a stated reason in the implementation notes.
- MAY: optional. Implement only if cheap and not in conflict with a MUST.
- OPEN: a decision the developer has not made. Surface it as a question; do not resolve it silently.
- The CLI is `tentaqles`. `tq` is an alias installed with it. Both names MUST work in every supported shell and run the same binary. Docs, messages, and examples use `tentaqles`.
- "Deterministic" means enforced by a script, a hook, a PATH, an environment variable, or a config file that Claude Code reads, never by the model's own reasoning.

**Discovery-first gate (MUST run before any code change)**

1. Read the repository: CLI entry points, the manifest schema, the guard, the trust store, the settings module, terminal interception, and every Claude Code hook already installed. Record findings in `DISCOVERY.md` under the task folder.
2. Map every requirement in Sections 3–9 to one of: implemented, partial, missing. Extend what exists; do not re-implement it.
3. List every term in Section 2 the codebase names differently. Keep the codebase name and record the mapping.
4. Identify code that implements features now out of scope: the v1 review page, GUI, and permission modes; workflow, scaffolding, and PRD or task generation; support for harnesses other than Claude Code; and plugin features other than the onboarding skill. List it; do not delete it. The developer decides what to remove.
5. Stop and ask the developer before touching anything in Section 7 (guard categories) or Section 8 (self-protection) if the current guard wiring conflicts with the requirement. Permission behavior is security-sensitive; do not guess.
6. Only then produce an implementation plan ordered as in Section 11.

**Scope guard**

Prefer narrow, reversible changes. Every new behavior MUST ship behind a manifest flag so an existing identity does not change behavior on upgrade.

## 1. Product summary

Tentaqles is an identity manager for developers who work for several companies or clients at once. It gives each client an isolated development environment: terminal, CLIs, Claude Code login, platform credentials, and browser profile. It adds a deterministic guard that stops a tool, an agent, or the developer from acting under the wrong identity.

**User:** a freelance or multi-employer developer who works with Claude Code and needs every CLI, Claude Code session, and browser action bound to the correct client.

**Problem:** one shared terminal, one set of CLIs, one Claude Code login, and one browser profile across clients lead to wrong-account pushes, cross-client credential use, and agents reaching for tools that belong to another client. An LLM asked to "create a VM" on an Azure-only client may reach for the AWS CLI it can see on the machine. Reasoning alone does not prevent this.

**Outcome:** one folder per client. A terminal opened there, or a Claude Code session started there, sees only that client's tools and credentials. A tool that belongs to another identity is not on the PATH and not in Claude Code's MCP config. If an agent reaches it by any other route, a hook blocks it before it runs. Agents cannot change the hooks, the guard, or the identity without a human.

**Three responsibilities, nothing else**

1. Identity: create and hold per-client manifests, credentials, and sessions.
2. Isolation: make each identity's terminal, Claude Code session, CLIs, and browser profile see only its own tools.
3. Guard: deterministically deny three things, and only three: acting outside the identity's scope, exposing secrets, and tampering with Tentaqles itself. Every other permission decision belongs to Claude Code's own permission system.

Onboarding (Section 5) is how an identity gets its CLIs, credentials, and connections. It serves responsibilities 1 and 2 and is not a fourth.

**Platforms:** Windows first (cmd.exe, PowerShell, pwsh, Git Bash), then macOS and Linux. Browser profiles: Chrome, Edge, Safari (macOS only), Opera.

## 2. Glossary

Use these terms exactly. Where the codebase already uses a different name, keep the codebase name and record the mapping during discovery.

| Term | Definition |
| --- | --- |
| Identity | One client or company context: an email, a root folder, a manifest, a trust store, and the isolated tools and sessions bound to it. |
| Identity root | The folder created for a client under the main clients directory. Everything inside inherits the identity. |
| Manifest | The YAML file written by `tentaqles reach` that declares the identity's providers, tools, browser profile, `blocked_commands`, and the provider scope. |
| Provider scope | The set of providers the manifest declares for the identity. Anything outside it is denied, not merely absent. |
| Trust store | The per-identity store of OAuth tokens, PATs, and MCP credentials. Never shared across identities. |
| Guard | The runtime component that evaluates a command or tool call against the manifest and returns allow or deny. |
| Hook | A Claude Code `PreToolUse` hook that calls the guard before a tool runs. Hooks are the enforcement point inside a Claude Code session. |
| Shim | An identity-scoped wrapper placed on the identity PATH in front of a real CLI or of `claude`. Shims inject flags, the config home, and the guard. |
| Guard state | On or off per identity. On by default; off from `tentaqles off` until `tentaqles on` (Section 9). There are no permission modes. |
| Harness | Claude Code, the AI coding agent CLI Tentaqles supports. No other harness is in scope. |
| Tentaqles plugin | The Claude Code plugin Tentaqles installs into each identity's config home. It holds the onboarding skill and nothing else. |
| Onboarding skill | The plugin skill that reads the identity's setup and, for each provider in scope, installs a missing CLI, authenticates, and connects the provider's MCP server and other connections to Claude Code (Section 5). |
| Human-only command | A `tentaqles` command the guard refuses when the calling process is an agent rather than an interactive human shell on a TTY. |
| Raw session | A terminal or Claude Code session of an identity whose guard is off: no identity environment, no guard, and the user's global Claude Code login (Section 9). |

## 3. Identity isolation requirements

Each identity MUST hold its own instance of every item below. Nothing listed here is shared between identities unless the developer explicitly links it.

**3.1 Terminal**

- Opening a terminal (cmd.exe, PowerShell, pwsh, Git Bash, or another shell declared in the manifest) inside an identity root MUST launch an identity-scoped session: PATH, environment variables, shell profile, and tool discovery are limited to that identity. The only exception is an identity whose guard is off: its terminals start as raw sessions (Section 9).
- CLIs outside the provider scope MUST NOT resolve from the identity terminal. An AWS-only client's terminal MUST NOT resolve `az`, even if `az` is installed globally on the machine. Implement by building the identity PATH from scratch rather than prepending to the user's PATH.
- The identity terminal MUST show the banner and status line from Section 4.1 as its first output, so the active identity and guard state are always visible.

**3.2 Claude Code sessions**

- Each identity MUST keep its own Claude Code login and session state. Two clients that each supply a Claude subscription MUST each be logged in to their own account from their own identity terminal.
- The Claude Code config home MUST be redirected per identity through the shim, never shared from the user's home.
- The user's global Claude Code install and login MUST remain untouched.
- The identity's MCP server list MUST contain only servers the provider scope allows (Section 6.2). A Claude Code session started from an identity MUST NOT load MCP servers from the user's global config.

**3.3 Platform connections**

Isolate credentials and sessions for each category the manifest declares:

- git providers: GitHub, GitLab, Azure DevOps, Bitbucket;
- cloud providers: AWS, Azure, GCP;
- data platforms: Databricks, Snowflake, Google Colab;
- email providers: Google, Microsoft;
- team and meeting platforms: Teams, Slack, Discord, Zoom, Google Meet;
- task providers: Asana, Linear, GitHub Projects, Azure DevOps, Trello, Jira.

- Credentials MUST live in the identity's trust store, never in the global OS keychain under a shared key or in the user's home dotfiles.
- A command issued from identity A MUST NOT be able to use a credential belonging to identity B. The guard enforces this (Section 6).
- Provider CLIs that keep their own credential files (`~/.aws`, `~/.azure`, `~/.config/gcloud`, `~/.databricks`) MUST be pointed at identity-scoped locations through their own environment variables (`AWS_CONFIG_FILE`, `AZURE_CONFIG_DIR`, `CLOUDSDK_CONFIG`, and equivalents). Verify each variable name during discovery.

**3.4 Tech stack and preferences**

- Language runtimes, package managers, linters, formatter configs, and editor settings SHOULD be declared in the manifest and resolved per identity.
- Global package installs from an identity terminal are governed by Claude Code's own permission settings, not by Tentaqles.

**3.5 Browser profiles**

- Each identity MUST be bound to one browser profile (Chrome, Edge, Safari, or Opera).
- When a terminal, Claude Code, or an agent in an identity opens a URL or drives a browser, it MUST open that identity's profile. Implement this with:
  - the `BROWSER` variable;
  - the OS default-handler override for the identity session;
  - shims for `xdg-open`, `start`, and `open`.
- Browser automation tools used by Claude Code (Claude in Chrome, Playwright, Puppeteer) MUST be launched with the identity's profile directory. Launching any other profile from an identity is an `identity` category breach and is denied.

## 4. `tentaqles reach` and `tentaqles edit`

`tentaqles reach [<name>]` adds a client. It creates the folder `<name>` under the clients directory, or uses the current folder when no name is given. It makes that folder an identity and starts its onboarding through the CLI. `tentaqles edit` changes an existing identity by re-running the prompts the developer selects. Both MUST be runnable from any terminal. Both MUST leave a fully provisioned identity, with no manual steps beyond the logins of Section 5.

**4.1 Startup banner**

`tentaqles reach`, `tentaqles edit`, every identity terminal, and every raw session MUST open with the banner below, in the style of the Hermes and Claude Code launch screens. Ship it as one text asset so the developer can replace it.

Default (UTF-8 terminals; 77 columns, fits an 80-column cmd.exe window):

```text
████████╗███████╗███╗   ██╗████████╗ █████╗  ██████╗ ██╗     ███████╗███████╗
╚══██╔══╝██╔════╝████╗  ██║╚══██╔══╝██╔══██╗██╔═══██╗██║     ██╔════╝██╔════╝
   ██║   █████╗  ██╔██╗ ██║   ██║   ███████║██║   ██║██║     █████╗  ███████╗
   ██║   ██╔══╝  ██║╚██╗██║   ██║   ██╔══██║██║▄▄ ██║██║     ██╔══╝  ╚════██║
   ██║   ███████╗██║ ╚████║   ██║   ██║  ██║╚██████╔╝███████╗███████╗███████║
   ╚═╝   ╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝ ╚══▀▀═╝ ╚══════╝╚══════╝╚══════╝

  tentaqles 0.1.0  ·  identity: dirtybird  ·  guard: on
  C:\clients\dirtybird
```

Fallback (terminal reports no UTF-8):

```text
  ______           __              __
 /_  __/__  ____  / /_____ _____ _/ /__  _____
  / / / _ \/ __ \/ __/ __ `/ __ `/ / _ \/ ___/
 / / /  __/ / / / /_/ /_/ / /_/ / /  __(__  )
/_/  \___/_/ /_/\__/\__,_/\__, /_/\___/____/
                            /_/
```

- The status line under the banner MUST show version, identity name, guard state, and working directory. In a raw session it shows `guard: off` and the warning line of Section 9.2.
- Respect `--no-banner` and `TQ_NO_BANNER=1` for scripts and non-interactive mode.
- Colors are OPEN; propose one scheme and keep it in the same asset.

**4.2 Preconditions**

- `tentaqles reach`: the target folder is not an identity yet. If it is, refuse and point to `tentaqles edit`.
- `tentaqles edit`: the folder is an identity. Re-run only the prompts the developer selects.

**4.3 Interactive prompt schema**

Present the prompts in this order. Every prompt MUST offer `Skip`. Multi-select prompts MUST offer `Other` with a free-text follow-up. Claude Code is always set up, so there is no harness prompt.

| Prompt | Input type | Options |
| --- | --- | --- |
| Identity email | Text, validated as email | — |
| Git provider | Single select | GitHub, GitLab, Bitbucket, Azure DevOps, Other, Skip |
| Cloud providers | Multi select | Azure, AWS, Google Cloud Platform, Other, Skip |
| Team / meeting platforms | Multi select | Microsoft Teams, Slack, Discord, Zoom, Google Meet, Other, Skip |
| Data platforms | Multi select | Databricks, Snowflake, Google Colab, Other, Skip |
| Task management provider (login only) | Single select | Asana, Linear, GitHub Projects, Azure DevOps, Trello, Jira, Other, Skip |
| Browser and profile | Single select + profile name | Google Chrome, Microsoft Edge, Safari, Opera, Other |
| Preferred shell | Single select | PowerShell, pwsh, cmd.exe, Git Bash, Other |

The agent MAY add prompts for anything meaningful to a daily developer workflow (container runtime, secrets manager, VPN). Each addition MUST be justified in the implementation notes and MUST default to Skip.

**4.4 Provisioning steps (in order)**

1. Write the manifest from the answers: providers (this is the provider scope), browser profile, an empty `blocked_commands` list, and the human-only command list.
2. Create the trust store, empty.
3. Install the missing CLIs for every provider in scope. Use the command-line package manager the provider catalog embedded in `tentaqles` lists for the OS (winget, scoop, Homebrew, apt, npm, or pip), then verify each with the catalog's version command. Tentaqles never runs a desktop installer (`.exe`, `.msi`, `.pkg`, `.dmg`) or a GUI setup.
   - Print one line per provider: installed, already present, or manual. Manual means the catalog has no package-manager method for the OS, and the line gives the vendor's download page.
   - A failed install does not stop provisioning; the onboarding skill retries it (Section 5.2).
   - `--no-install` skips this step.
4. Set up Claude Code for the identity:
   - the redirected config home;
   - the hook registration (Section 6.4);
   - the `claude` shim that injects `--dangerously-skip-permissions` (Section 7.2);
   - an MCP server list with no server outside the scope;
   - the plugin, pre-registered with the onboarding skill (Section 5.1).
5. Create or register the browser profile.
6. Register the folder so a new terminal opened inside it resolves to the identity session (Section 3.1).
7. Write the signed integrity record (Section 8.3).
8. Spawn a new identity terminal in the folder.
9. In that terminal, start `claude` with the onboarding skill as its first prompt. The developer logs in to Claude Code and the skill continues from there (Section 5). `--no-onboard` skips this step.
10. Terminate the terminal session that invoked `tentaqles reach`.

Step 10 MUST happen only after step 8 succeeds. If any other step fails, except a CLI install (step 3), leave the folder in a state `tentaqles reach` can resume from and print the failing step.

**4.5 Non-interactive mode**

`tentaqles reach --from <manifest.yaml>` SHOULD accept a prepared manifest and skip the prompts. It is a human-only command (Section 8): an agent MUST NOT be able to create or alter an identity.

## 5. Plugin and onboarding skill

After `tentaqles reach`, Claude Code starts in the new identity terminal and prompts for its own login. The developer logs in with the client's account. That login MUST be stored in the identity's redirected config home (Section 3.2).

**5.1 The plugin**

- `tentaqles reach` MUST pre-register the Tentaqles plugin in the identity's Claude Code config home, so the first session already has it. Where that is not possible, the identity terminal MUST print the one-line install command on the first `claude` launch.
- The plugin holds the onboarding skill and nothing else. It has no other skills, slash commands, agent definitions, hooks, memory, or dashboards. It exposes nothing that changes the identity or the guard.
- The guard hook is not part of the plugin. The CLI registers it in the identity's Claude Code settings (Section 6.4), so removing or editing the plugin cannot remove the guard.

**5.2 Onboarding skill**

The skill reads the identity's setup through `tentaqles status --json`, a read-only command. It returns the identity, the provider scope, the declared providers and browser profile, and what is already verified. The skill never reads the manifest file itself, which is a protected path (Section 8.2).

For each provider in scope, in manifest order, the skill MUST:

1. **CLI.** `tentaqles reach` already installed the CLIs it could (Section 4.4 step 3). If the provider has a CLI that still does not resolve on the identity PATH, the skill runs `tentaqles install <provider>`. That command uses the same catalog method as `reach`, refuses any provider outside the scope, and verifies the result with the version command. The skill never chooses an install command itself. When the catalog has no package-manager method for the OS, the skill gives the developer the vendor's download page and checks again afterwards. It never downloads or runs a desktop installer.
2. **Credential.** Detect whether a valid credential already exists for the identity. If yes, skip. Otherwise authenticate, using the first method the provider supports in this order: OAuth device flow, personal access token, MCP server credential. Open any browser step in the identity's browser profile. The credential MUST land only in the identity's trust store or in the identity-scoped locations of Section 3.3.
3. **Connections.** Register the provider's MCP server, and any other Claude Code connection the provider offers, in the identity's Claude Code config. The skill cannot write Tentaqles' protected paths itself (Section 8), so it MUST register through a Tentaqles-controlled path that accepts only servers the scope allows. The mechanism is OPEN (Section 10). Where a connection can only be enabled by the developer, such as a claude.ai connector, the skill tells the developer the exact step and verifies the result afterwards.
4. **Verify.** Make a read-only call (list repos, list projects, whoami). Report one row per provider: CLI, credential, connections, verified.

The skill MUST be re-runnable at any time: to add a provider already in scope, to install a CLI removed since the last run, or to refresh an expired credential. It MUST NOT install a CLI for, authenticate, or connect any provider outside the manifest's scope. Adding a provider to scope is `tentaqles edit`, a human-only command.

**5.3 Task management provider**

Only the login is managed. The skill authenticates the developer to the task provider and registers its MCP server if one exists. Nothing beyond the credential is stored.

**5.4 Resulting capabilities**

When onboarding completes, Claude Code acting as the identity can:

- read Slack or Teams messages;
- read email from the declared email provider;
- read and write the task provider;
- clone, push, pull, and comment on the git provider.

It does all of this through identity-scoped credentials only, subject to the guard.

## 6. Provider scoping and the deterministic guard

The manifest's provider scope is the single source of truth for what an identity may touch. Enforcement MUST be layered so that a wrong-provider action fails at the earliest layer, and every layer is a script, a config file, or a hook, never the model's judgment.

**6.1 Reference scenario (MUST pass as an acceptance test)**

Identity `company-x` declares Azure as its only cloud provider. The developer, by mistake, asks Claude Code to create a virtual machine on AWS. Expected behavior, in order:

1. The Claude Code instruction file generated for the identity (the user-level `CLAUDE.md` in its config home) states the scope, so a well-behaved model declines or asks. This layer is advisory and is not relied on.
2. `aws` is not on the identity PATH. A shell call to `aws ec2 run-instances` fails with command-not-found.
3. No AWS MCP server is registered in the identity's Claude Code config, so no AWS tool appears in the model's tool list.
4. The model may reach `aws` by another route: an absolute path to a global install, `python -m awscli`, a script that wraps it, an MCP server discovered from another identity's config, or a `curl` to an AWS endpoint with a credential. The pre-tool hook then calls the guard, the guard matches the `identity` category, and the call is denied before it runs.
5. The denial is logged with the identity, the command, the matched rule, and the layer that caught it. The model receives a short deny message that names the identity's scope so it can report the mistake to the developer instead of retrying.

**6.2 Enforcement layers (all MUST be implemented)**

| Layer | Mechanism | What it blocks |
| --- | --- | --- |
| PATH | Identity PATH built from scratch from the manifest; shims for in-scope CLIs and `claude` | Out-of-scope CLIs by name |
| Environment | Provider config variables (`AWS_CONFIG_FILE`, `AZURE_CONFIG_DIR`, `CLOUDSDK_CONFIG`, `GH_CONFIG_DIR`, the Claude Code config home) point to identity-scoped paths; out-of-scope variables are unset | Credential files of other identities and of the global user |
| Claude Code config | MCP server list generated from scope; global Claude Code config not loaded | Out-of-scope MCP tools from appearing at all |
| Hook | `PreToolUse` hook calls the guard with the full command or tool call; guard returns allow or deny | Everything the first three layers missed: absolute paths, wrappers, network calls to out-of-scope endpoints, cross-identity paths |
| Filesystem | Hook denies reads and writes under other identity roots and under the Tentaqles config and trust store | Credential and config leakage between identities; tampering (Section 8) |

**6.3 Guard rule evaluation**

- The guard receives: identity, calling process (human TTY, Claude Code, or subagent), tool name, and the full command or arguments.
- Rules are evaluated in order: `blocked_commands` from the manifest, then `identity`, `secrets`, `self-protection`. First match denies. No match allows. There is no ask verdict and no mode.
- Matching for the `identity` category MUST cover at least:
  - executable names and paths of out-of-scope provider CLIs;
  - environment variable names and credential file paths of out-of-scope providers;
  - MCP server names outside scope;
  - hostnames of out-of-scope provider APIs in network calls;
  - file paths under other identity roots.

  Ship the match lists as data files per provider so the developer can extend them.
- Matching MUST be deterministic and testable: every rule has a fixture of commands it must allow and deny. Tests run on every change to the guard.
- The guard MUST fail closed: if it cannot evaluate (crash, timeout, missing manifest), the hook denies.

**6.4 Hook installation**

- Claude Code: a `PreToolUse` hook for `Bash`, `Read`, `Write`, `Edit`, every other tool that takes a command, path, or URL, and every MCP tool. The CLI registers it in the identity's config home. The hook blocks on deny and returns the reason.
- Hook scripts MUST be installed outside the project folder and outside any path an agent can write to (Section 8). Project-level hooks in `.claude/` MAY exist for project purposes but MUST NOT be the only enforcement point.
- Verify the exact hook names and payload formats against the installed Claude Code version during discovery and record them in `DISCOVERY.md`.

## 7. Guard categories and Claude Code launch

Tentaqles has no permission modes. Whenever the guard is on, it enforces exactly three categories, always with the verdict deny. Every other permission decision belongs to Claude Code's own permission system and is configured there by the developer, not by Tentaqles. That covers destructive git, cloud deletes, system changes, package installs, network egress, and browser actions.

**7.1 Guard categories (always deny)**

| # | Category | What it covers |
| --- | --- | --- |
| 1 | identity | Any command, tool, credential, path, or endpoint outside the identity's provider scope; launching a non-identity browser profile; anything in the manifest's `blocked_commands` (Section 6.3) |
| 2 | secrets | Reading `.env` files, dumping the environment, reading secret stores or the trust store, committing files that match secret patterns |
| 3 | self-protection | Human-only `tentaqles` commands; any write to hooks, the guard, the trust store, the manifest, the shims, or Tentaqles settings (Section 8) |

The agent MUST NOT add categories. A new need is either an extension of one of these three (a new match list or pattern) or out of scope.

**7.2 Claude Code launch**

- In an identity terminal, `claude` is a shim. It runs `claude --dangerously-skip-permissions` with the identity's config home and the guard hook registered. Verify the flag during discovery and record it in `DISCOVERY.md`.
- The developer MAY change how Claude Code asks for permission through Claude Code's own means: its settings file, its auto mode, or flags typed after the shim's. Tentaqles does not read, set, or override those. Document the observed precedence between the shim's flag and a developer-typed flag.
- The guard hook runs regardless of Claude Code's permission setting. Claude Code in its most permissive state is still bound by the three categories.

**7.3 Deny behavior**

- A denied call returns a short message naming the category and, for `identity`, the identity's scope, so the model reports the mistake instead of retrying. These categories have no ask verdict, no pending list, and no human-approval path at runtime.
- Every denial is logged with identity, calling process, command, matched rule, and enforcement layer. The log lives under the protected paths (Section 8.2) and is readable through `tentaqles status --denials` from a terminal.

## 8. Self-protection

AI agents MUST NOT be able to change hooks, the guard, the manifest, the trust store, the shims, the mode, or any identity unless a human allows it. This is the third locked category and the reason the other two hold.

**8.1 Human-only commands**

The following are human-only:

- `tentaqles reach`, `tentaqles edit`, `tentaqles off`;
- `tentaqles trust *`, `tentaqles hooks *`;
- any command that writes under the Tentaqles config area, except `tentaqles on` (Section 9.1).

The guard matches the `tq` alias the same way. The guard MUST refuse these commands when:

- the calling process is not attached to an interactive TTY, or
- the process tree contains a Claude Code or subagent process, or
- the environment carries a Claude Code marker (the variables Claude Code sets in its child processes; enumerate them during discovery).

On refusal the guard logs the attempt and returns a message naming the command as human-only. A human running the same command on a TTY MUST confirm interactively before any write lands.

**8.2 Protected paths**

- These MUST live outside every identity root and outside the user's project folders:
  - the Tentaqles config area;
  - hook scripts and shims;
  - the guard binary;
  - manifests and trust stores;
  - the denial log.
- The hook MUST deny any `Read`, `Write`, `Edit`, shell redirect, copy, move, or delete that targets a protected path whenever the guard is on. Reading the manifest's non-secret fields MAY be allowed through `tentaqles status` only.
- On Windows, protected paths SHOULD be ACL-restricted so that only the user's interactive session can write them; on macOS and Linux use file modes and, where available, immutable flags. This is defense in depth, not the primary control.

**8.3 Integrity verification**

- Every identity terminal start and every Claude Code start MUST verify the hashes of the hook scripts, the shims, and the manifest. The reference is a signed record written by `tentaqles reach` or at the last human-approved change.
- On mismatch: refuse to start Claude Code, print what changed, and require a human to run `tentaqles hooks verify --accept` on a TTY.
- The signed record lives under the protected paths. Its key is OPEN; prefer an OS keychain entry bound to the user, with a plain-file fallback the implementation notes call out.

**8.4 What a human must do to change the guard**

1. Open an identity terminal (or any terminal) on a TTY.
2. Run the human-only command.
3. Confirm the interactive prompt.
4. The command writes the change and updates the signed record.

No other path exists. An agent that wants a change writes a request to its output; the developer decides.

## 9. `tentaqles off` and `tentaqles on`

Every identity starts with its guard on. `tentaqles off` turns the guard off for one identity, and the identity stays off until `tentaqles on`. While an identity is off, its terminals and Claude Code sessions run as raw sessions: no identity environment and no guard. The developer accepts that the guard does not protect that identity while it is off. Other identities are never affected.

**9.1 Off state (MUST)**

- `tentaqles off` records an off state for the identity under the protected paths (Section 8.2). The record names the identity and the time, and it is signed like the integrity record (Section 8.3). An off state that is missing, unreadable, or fails its signature counts as on: the guard stays on.
- `tentaqles off` and `tentaqles on` act on the identity of the folder they run in. Both MAY take an identity name to act on another identity.
- Turning the guard off is human-only (Section 8.1). An agent MUST NOT be able to run `tentaqles off` or write the off state by any route.
- `tentaqles on` deletes the off state. Any caller MAY run it, an agent included, because it only restores protection.

**9.2 What off changes**

While the identity is off:

1. **The terminal that ran `tentaqles off`** becomes a raw session right away, at the next prompt at the latest:
   - PATH is restored to the user's global PATH;
   - identity variables and provider config variables are unset;
   - the Claude Code config-home redirect is unset, so `claude` uses the user's global login.
2. **Every terminal opened in that identity afterwards** starts as a raw session.
3. **Other terminals of that identity that were already open** keep the state they had until they are closed. A terminal decides on or off when it opens, and only `tentaqles off` or `tentaqles on` typed in that terminal changes it.
4. **The marker:** every raw session carries an off marker with a session id. `tentaqles off` records that id in the identity's off state, and so does every terminal that starts while the identity is off. A marker counts only while the off state exists and lists its session. A marker alone never turns the guard off, so an agent cannot switch the guard off by setting a variable or starting a new `claude` with one.
5. **`claude` in a raw session** still runs `claude --dangerously-skip-permissions`, exactly as in an identity terminal, but with the global login and no guard hook.
6. **Hooks follow their own terminal.** A Tentaqles hook script exits 0 immediately only when its Claude Code session belongs to a raw session listed in the off state. A Claude Code session in an identity terminal opened before `tentaqles off` keeps the guard on.
7. **Banner and warning:** every raw session prints the banner with `guard: off` in the status line, plus one warning line that names the identity and says `tentaqles on` restores it. The warning repeats in every new terminal of that identity until it is on again.

**9.3 What on changes**

After `tentaqles on`, the terminal that ran it re-enters the identity at the next prompt. Every terminal of that identity opened afterwards is an identity terminal with the guard on.

Raw terminals that are still open keep their raw environment until they are closed. Their guard comes back on at once, because `tentaqles on` deletes the off state and every marker stops counting. Turning the guard off is per terminal; turning it back on is immediate everywhere, which is the safe direction. Claude Code sessions in those terminals keep the global login until they are restarted.

**9.4 Known limitations (document in the implementation notes)**

- **Sessions that keep running:** a Claude Code session started in the terminal that ran `tentaqles off` keeps the identity's login until it is restarted, and its guard stops at once. A session started while off keeps the global login until it is restarted after `tentaqles on`.
- **Marker reach:** an agent that could read another session's id from the off state could reuse it. The off state is a protected path that the hook denies reading (Section 8.2), and the guard stays a text filter for routes it cannot see.
- **Project instruction files** (`CLAUDE.md`) still load. Acceptable; they are project context, not identity enforcement.
- **Global login:** the user's global Claude Code login must exist. If it does not, Claude Code prompts for login and Tentaqles MUST NOT intercept.
- **Terminal interception** is the shell profile hook on every platform, never a Windows Terminal profile, an Explorer context-menu entry, or any other GUI integration. Confirm during discovery that an off identity's new terminals start raw under it.

## 10. Non-goals, constraints, and open questions

**Non-goals (do not implement, do not extend)**

- Permission modes of any kind. The guard has three categories and one verdict. Dangerous-but-in-scope actions (force push, cloud delete, recursive delete, global installs) are governed by Claude Code's own permission settings, which Tentaqles never touches.
- Harnesses other than Claude Code, including OpenAI Codex, OpenCode, and Kimi Code. Tentaqles ships no shims, hooks, config, or onboarding for them.
- Development workflow of any kind:
  - project scaffolding;
  - PRD or task-spec generation;
  - task orchestration, worker agents, decision requests;
  - quality gates.
- Plugin features other than the onboarding skill: memory, knowledge graph, session preambles, dashboards, engineering or review skills, agent definitions, and slash commands.
- Any integration with a task provider beyond its login and MCP registration.
- Review pages, approve buttons, or any web UI.
- Any GUI. Tentaqles is a CLI plus a Claude Code plugin that holds the onboarding skill. This rules out:
  - desktop apps, tray icons, and settings windows;
  - Windows Terminal profiles and Explorer context-menu entries.
- Desktop installers. Tentaqles itself ships only as the CLI binary, installed from the command line (a package manager or a one-line install script); there is no `.exe`, `.msi`, `.pkg`, or `.dmg` setup. Provider CLIs are installed only through command-line package managers (Section 4.4).
- Multi-user or team sharing of identities. One developer, one machine.
- Replacing Claude Code's own permission system. The guard adds a layer.

**Constraints**

- MUST NOT modify the user's global Claude Code install, global login, or global shell profile beyond the single hook that routes terminals into identities.
- MUST NOT store any credential outside the identity's trust store.
- MUST keep every new behavior behind a manifest flag so existing identities do not change on upgrade.
- MUST work offline for everything except provider auth, CLI installs (`tentaqles reach` and `tentaqles install`), and Claude Code's own network use.
- Enforcement MUST be deterministic (Section 0). Instruction-file text is advisory only and is never the sole control for any requirement in Sections 6–8.

**Open questions (surface to the developer before implementing the affected part)**

- [ ] Observed precedence between the shim's `--dangerously-skip-permissions` and a flag the developer types after it (Section 7.2).
- [ ] Signing key for the integrity record: OS keychain or plain file (Section 8.3).
- [ ] Per-provider match lists for the `identity` category: initial coverage and file format (Section 6.3).
- [ ] Whether `tentaqles off` should also accept a `--for <minutes>` time box, after which the identity turns itself back on (Section 9).
- [ ] How the onboarding skill registers MCP servers and other connections without writing protected paths. The proposal is `tentaqles connect <provider>`, the one write an agent may call: it writes only catalog-defined entries for in-scope providers and logs each change (Section 5.2).
- [ ] Which out-of-scope code paths (review, GUI, permission modes, workflow, non-Claude harnesses, plugin features other than onboarding) to delete now versus leave dormant (Section 0, discovery step 4).

## 11. Agent execution plan

Work in this order. Do not start a phase until the previous phase's gate passes. Each phase ends with a short report to the developer and a pause for feedback.

| Phase | Deliverable | Gate to pass before next phase |
| --- | --- | --- |
| 0 Discovery | `DISCOVERY.md`: existing components, term mapping, verified Claude Code hook names and launch flag, out-of-scope code to retire (including mode code, workflow code, non-Claude harness code, and plugin parts other than onboarding), answers to Section 10 questions resolvable from code | Developer confirms the discovery and answers the remaining open questions |
| 1 Self-protection | Human-only command detection, protected paths, hook deny on protected paths, integrity record and verification (Section 8) | An agent cannot write to hooks, manifest, trust store, or shims; tests prove it for every write route in Section 8.2 |
| 2 Guard and scope | Provider scope in the manifest; layered enforcement (PATH, env, Claude Code config, hook, filesystem); three-category rule evaluation with per-provider match lists; fail-closed; deny logging (Sections 6, 7) | The Section 6.1 scenario is blocked at layer 2, at layer 3, and at layer 4 in separate tests; guard fixtures pass; no ask or mode code path remains |
| 3 Claude Code shim | The `claude` shim injects `--dangerously-skip-permissions`, the config home, and the hook (Section 7.2) | Claude Code started from an identity runs with the bypass flag, the identity login, and the hook active; developer-typed flags pass through |
| 4 `tentaqles off` / `on` | Per-identity off state, signed, under protected paths; `off` human-only; terminals and hooks follow it (Section 9) | After `tentaqles off`:<br>- the current terminal and every new terminal of that identity are raw sessions with the global Claude Code login and no hook;<br>- terminals that were already open, and their Claude Code sessions, keep the guard on;<br>- other identities are unaffected;<br>- an agent cannot turn the guard off, whether by command or by setting the off marker;<br>- a missing or forged off state counts as on.<br><br>After `tentaqles on`:<br>- the current and every new terminal are identity terminals with the guard on;<br>- every open raw terminal has its guard back on at once. |
| 5 Onboarding | `tentaqles reach` and `tentaqles edit` with banner, prompt schema, CLI installs from the catalog, and provisioning order (Section 4); `tentaqles install <provider>`; the plugin with the onboarding skill (Section 5); `tq` as an alias of `tentaqles` | `tentaqles reach` on a fresh folder installs the missing in-scope CLIs and reaches a working Claude Code session in which the skill has connected and verified every declared provider; out-of-scope providers cannot be installed, authenticated, or connected; `tq` and `tentaqles` run every command identically |
| 6 Retirement | Remove or disable the out-of-scope code the developer chose to drop in phase 0, including the plugin parts other than the onboarding skill and the support for harnesses other than Claude Code | No v1 command, mode, hook, workflow, or non-Claude harness path remains reachable from an identity terminal or Claude Code |

**Rules for every phase**

- Write tests before wiring the guard or the hooks. They are the security boundary; untested changes there are not acceptable.
- Keep each phase on its own branch. Do not merge a phase before its gate passes.
- When a requirement here conflicts with what you find in the code, stop and report; do not pick silently.
- Report in the format: what changed, what is behind a flag, what the developer must do next, what is OPEN.

**Acceptance for the whole PRD**

- A developer can run `tentaqles reach <client>`, log in to Claude Code, and let the onboarding skill install and connect the in-scope providers. From then on they work with no credential, CLI, MCP tool, or file from another identity reachable at any step.
- An agent on an Azure-only identity asked to act on AWS is stopped by PATH, by the MCP tool list, and by the hook, and reports the mistake instead of retrying.
- Claude Code runs with `--dangerously-skip-permissions` by default; anything the developer wants it to ask about is configured in Claude Code, and Tentaqles never changes that.
- The developer can turn an identity's guard off with `tentaqles off` and back on with `tentaqles on`, for the current and every new session of that identity. No agent can turn it off or alter the guard, the hooks, the manifest, or the trust store.
- `tentaqles` and `tq` are the same command in every supported shell.
