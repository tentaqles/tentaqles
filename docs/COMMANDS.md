# Tentaqles commands

Every Tentaqles command in one place, with what it does, plus a test walkthrough
you can follow to check that an install works.

Tentaqles has two parts:

- **`tq`**: the command-line tool. It picks the right identity for each client
  folder (git email, cloud login, agent config), blocks a command under the
  wrong identity, and keeps secrets out of the agent's transcript and commits.
- **The Claude Code plugin** (`/tentaqles:...`): the session memory and
  identity preamble, the knowledge graph, skills that agents follow (prd,
  feature, fix, ship, the reviews) and five read-only reviewer agents.

**Human-only** means the command refuses to run from an agent session. You run
it yourself in your own terminal. That keeps decisions about trust and secrets
with a person.

For more detail on a command, run `tq <command> --help`.

---

## 1. `tq` commands

### Setup and workspaces

| Command | What it does |
|---|---|
| `tq init <base>` | Registers a base folder (for example `C:\repos`). Each first-level subfolder becomes one client identity. |
| `tq add <name>` | Creates a workspace folder with its manifest (`.tentaqles.yaml`), identity folders and git identity. |
| `tq onboard` | A wizard that runs `tq add`, then records the company's tracker, chat, email, data platform and agent CLIs. It never asks for a token. It prints the `tq allow` and login lines for you to run. |
| `tq setup` | Creates several client workspaces at once from a plan file. |
| `tq list` | Lists every workspace, whether it is trusted, its git email and its identities. |
| `tq allow <name>` | **Human-only.** Trusts the workspace's manifest as it is now (by hash). Until then, tq exports nothing for that folder and blocks remote commands in it. Editing the manifest drops the trust. |
| `tq deny <name>` | **Human-only.** Removes trust from a workspace. |
| `tq login <cli>` | Runs a CLI's own login (gh, az, aws, ...) with its config stored in the workspace's private config folder. |
| `tq run <ws> -- <cmd>` | Runs one command with another workspace's identity, without `cd`-ing into it. `tq run --off -- claude` starts Claude with no tq at all. |
| `tq migrate` | Moves a hand-made tentaqles setup under tq's management. It only shows the plan; add `--apply` to do it. Every step is written to a journal. |
| `tq uninstall --restore` | Undoes a migration by replaying its journal backwards. It lists the steps and changes nothing until you add `--yes`. |
| `tq version` | Prints the tq version. |

### Shell activation

| Command | What it does |
|---|---|
| `tq activate <shell>` | Prints the code your shell profile runs at startup. It switches identity every time you change folder, and defines the `claude` and `codex` launchers. |
| `tq hooks install\|remove\|status` | Adds the activation block to your shell profiles, removes it, or shows where it is installed. |
| `tq env` | Prints the environment changes for the current folder. The shell hook uses it; you rarely run it yourself. |
| `claude` / `codex` | Launchers from the activation block. They start the agent with the folder's identity and with permission prompts skipped; tq blocks only identity drift and secret reads. |
| `claude-off` / `codex-off` | Start the agent with no tq: no identity env, main logins, the plugin switched off. |

### Health checks

| Command | What it does |
|---|---|
| `tq doctor` | Checks hooks, trust, git identity, plugins, settings and the shell launcher against every manifest. It changes nothing. Each problem comes with the fix to run. A manifest that still carries a removed block (`guard:`, `decision:`, `tracker:`, `packs:`, `data:`) gets a warning, never a failure. |
| `tq providers list\|show\|check\|add` | The catalog of supported tools (cloud, git host, data, agents, chat). `check` shows which CLIs are installed. |
| `tq secrets audit` | Finds secret-shaped values in agent config files, and `.env` files that git would commit. It shows only the path and line, never the value. |
| `tq worktrees list` / `prune --yes` | Lists leftover git worktrees from agent runs. `prune` removes the ones that are clean, merged and old, and never forces. |

### Agent settings and plugins

| Command | What it does |
|---|---|
| `tq settings render [--all]` | Writes tq's part of each identity's Claude `settings.json`: baseline permissions, env, status line and hooks. It prints what changed. |
| `tq bundle catalog` | Shows the shared catalog of plugins, skills and MCP servers. |
| `tq bundle diff <ws>` | Shows how a workspace's installed plugins differ from its `claude.bundle`. |
| `tq bundle sync <ws>` | Installs or removes plugins so the identity matches its bundle. |
| `tq bundle capture` | Builds a bundle entry from an identity that is already set up. |

### Secrets

| Command | What it does |
|---|---|
| `tq dotenv run [--file F] -- <cmd>` | Runs a command with a `.env` file loaded and replaces each value with `[REDACTED:NAME]` in the output. This is the only allowed way for an agent to use a `.env`. |
| `tq secret set\|rm\|status` | **Human-only.** Stores tracker tokens (GitHub, Azure DevOps, Jira, Linear, Asana) in the OS keychain. It never prints them. |

### Hooks the agent calls (you don't run these)

| Command | What it does |
|---|---|
| `tq claude-hook pre-tool-use` | Decides allow or block for each tool call: identity drift, an untrusted folder, `.env` and secret-store reads, environment dumps, a commit that would record a secret, a cross-client MCP server. |
| `tq claude-hook session-start` | Prints the workspace and identity preamble at session start. |

---

## 2. Plugin skills (`/tentaqles:<name>`)

Type them in Claude Code, or just describe the task; the skill's description
makes it start on its own.

### Plan, build and ship

Plain checklists: nothing is recorded outside the repository, and every stop
is a question to you.

| Skill | What it does |
|---|---|
| `prd` | Writes or revises `docs/prd.md` through a short interview: problem, users, numbered requirements with pass/fail checks. |
| `kickoff` | Starts a new project: stack decision, CLAUDE.md, threat model, privacy record, CI, then the first slice through `feature`. |
| `feature` | Adds one PRD requirement: interview, a spec slice you approve, tests first, the code, the reviewer agents on the diff, definition of done, ship. |
| `fix` | Fixes a bug reproduce-first: a failing test first, then the root cause, then the smallest fix; the regression test is never edited. |
| `grill-me` | Asks about every business-rule edge case (dates, money, permissions, failures) before code is written. |
| `dod` | Runs the definition of done as a checklist and turns missing items into work. No override. |
| `ship` | Turns finished work into a pull request: identity check, branch, a self-assessed risk line (the reviewer agents on a high-risk diff), tests, commit, push, PR. It never merges. |
| `babysit-pr` | Watches a PR until CI is green and every review comment is answered. |
| `direction` | Read-only check of a repo against its PRD: each requirement is aligned, partial, missing or contradicted. |

### Reviews

| Skill | What it does |
|---|---|
| `system-design-review` | Reviews a design, RFC or ADR: requirements, failure modes, retries, security, cost, rollback. |
| `threat-model` | Writes `docs/threat-model.md` with STRIDE threats for each trust boundary. |
| `auth-security` | Designs or audits login, sessions, tokens, authorization and RLS. |
| `llm-security-review` | Reviews LLM, prompt, agent tool, MCP and hook code against the OWASP LLM and Agentic Top 10. |
| `privacy-review` | Reviews changes that touch personal data against LGPD and GDPR. |
| `classify-data` | Classifies the data a project handles and writes `docs/privacy.md` and `docs/incident-playbook.md`. |
| `supply-chain` | Hardens CI: actions pinned to commit hashes, least permissions, lockfile installs, an SBOM. |
| `ci-pipeline` | Sets up GitHub Actions CI with a quality gate that fails when any metric gets worse. |

### Database and automation

| Skill | What it does |
|---|---|
| `db-migration` | Plans a safe schema change for Postgres or SQL Server: expand/contract, up and down scripts, a rollback plan, a review checklist. |
| `index-advisor` | Diagnoses a slow query and recommends indexes. |
| `n8n-triage` | Diagnoses a failing n8n workflow read-only, then fixes it on an inactive draft copy. |

### Workspaces and identity

| Skill | What it does |
|---|---|
| `add-client` | Creates a new client workspace with its identity, cloud, database and git config. |
| `add-project` | Creates a project inside the current client, with its context inherited. |
| `onboard` | Connects and checks every company tool of a workspace. It prints the logins for you to run. |
| `switch-client` | Lists the client workspaces and helps you switch safely. |
| `client-settings` | Shows or changes manifest fields: cloud, database, git, stack, language. |
| `workspace-status` | Shows which workspace you are in and its preflight results. |
| `rollback` | Restores a workspace manifest from an earlier snapshot. |
| `setup-demo` | Creates demo workspaces with sample code. |

### Memory and knowledge

| Skill | What it does |
|---|---|
| `query-memory` | Searches past sessions, decisions, pending items and facts. |
| `decision-history` | Traces why a decision was made and how it changed. |
| `file-history` | Every session that touched a file and why. |
| `build-graph` | Builds a knowledge graph of the workspace for semantic search. |
| `compact-memory` | Condenses recent sessions into lasting facts and removes stale entries. |
| `cross-patterns` | Recurring decisions across all client workspaces. |
| `profile-refresh` | Refreshes the workspace profile: hot files and activity. |
| `export-vault` | Refreshes the Obsidian copy of the memory. |
| `emit-signal` | Sends a named signal to another workspace. |
| `dashboard` | Opens the live dashboard of workspaces, sessions and pending items. |
| `session-wrap` | Saves what was done, decided and left open at the end of a session. |
| `wrap` | End-of-day summary from git and GitHub (commits, PRs, CI), saved to memory. |

### Reviewer agents

The `feature`, `fix` and `ship` skills hand these the diff and the spec. You
can also ask for one by name. All five only read.

| Agent | Role |
|---|---|
| `critic` | Hostile, read-only quality review of a diff against its spec. |
| `spec-critic` | Checks the diff does exactly what the spec asks, no more and no less. |
| `security-reviewer` | Application security (ASVS 5.0, OWASP Top 10). |
| `privacy-reviewer` | Data protection (LGPD, GDPR). |
| `llm-security-reviewer` | LLM and agent security. |

---

## 3. Test walkthrough

Follow these steps in order, in a **new** PowerShell or Git Bash window.

The expected results below are real output from this machine, in the
`tentaqles` workspace. Names, versions and counts will differ on your machine;
the shape should match.

Steps marked **(you)** are human-only or touch real systems, so only you can
run them.

### Step 1: tq is installed and current

```powershell
tq version
```

**Expected:** a version line (`tq dev` for a local build).
**If it fails:** `tq` isn't on PATH. Reinstall with `installers/install.ps1`.

### Step 2: shell activation and the launcher

```powershell
tq hooks status
Get-Command claude | Format-List CommandType, Definition
```

**Expected:** `installed` for bash, pwsh and powershell (zsh and fish say
`no profile` when they aren't installed). `claude` is a **Function** defined by
tq's activation block.
**If it fails:** if `claude` shows your own function or alias, `tq doctor`
names the profile line to delete.

### Step 3: identity follows the folder

```powershell
cd C:\repos\tentaqles\tentaqles
git config user.email
$env:CLAUDE_CONFIG_DIR
cd C:\repos\personal
git config user.email
```

**Expected:** `reach@tentaqles.ai` and
`...\.tentaqles\identities\tentaqles\claude` in the first folder. In
`personal`, the email changes to that workspace's own email.
**If it fails:** run `tq list`; the workspace must show `TRUSTED true`. If it
doesn't, run `tq allow <name>` **(you)**.

### Step 4: workspace list and health

```powershell
tq list
tq doctor
```

**Expected:**

```
NAME       TRUSTED  EMAIL                          IDENTITIES        ROOT
tentaqles  true     reach@tentaqles.ai             claude,gh         C:\repos\tentaqles
...
```

`tq doctor` prints `[ok]`, `[info]` and `[warn]` lines. Each warning ends with
`→` and the fix. There should be **no** warning about a `claude` function or
alias shadowing the launcher. A manifest that still carries a `guard:`,
`decision:`, `tracker:`, `packs:` or `data:` block gets a `[warn]` line that
says the block is ignored; remove it and run `tq allow <name>` **(you)**.

### Step 5: the guard decides (simulated tool calls)

This step feeds the guard the same JSON Claude Code sends, without running the
commands. Save each payload as a file, then pipe it in:

```bash
# Git Bash
cat > /tmp/p.json <<'EOF'
{"cwd":"C:\\repos\\tentaqles\\tentaqles","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"}}
EOF
tq claude-hook pre-tool-use < /tmp/p.json; echo "exit $?"
```

Change `tool_name` and `tool_input` for each row:

| Tool call | Expected |
|---|---|
| `Bash`: `git status` | nothing printed, `exit 0` (allowed) |
| `Read`: `file_path` = `...\.env` | `BLOCKED [tq/env-file-read]`, `exit 2` |
| `Bash`: `cat .env` | `BLOCKED [tq/env-file-shell]`, `exit 2` |
| `Bash`: `printenv` | `BLOCKED [tq/env-dump]`, `exit 2` |
| `Bash`: `cat ~/.aws/credentials` | `BLOCKED [tq/secret-store-shell]`, `exit 2` |
| `Bash`: `git push --force origin main` | nothing printed, `exit 0`: tq does not guard it; Claude's own permission settings do |
| `Bash`: `git push` with `cwd` in an untrusted folder | `BLOCKED` with the `untrusted` reason, `exit 2` |

**Exit codes:** 0 with no output means allow; 2 means block. There is no ask.

### Step 6: secrets stay masked

```bash
cd "$(mktemp -d)"
printf 'DEMO_SETTING=hello\n' > demo.env
tq dotenv run --file demo.env -- bash -c 'echo setting is $DEMO_SETTING'
rm demo.env
```

**Expected:** `setting is [REDACTED:DEMO_SETTING]`, exit 0 (every value in the file is
masked, whatever its name).

### Step 7: human-only commands refuse agents

Ask Claude in a session: "run `tq secret status`" (or `tq allow tentaqles`).

**Expected:**
`tq: human-only: run it in your own terminal ...`, exit 1.
Run it yourself in a normal terminal **(you)** and it works.

### Step 8: agent settings

```powershell
tq settings render --all
```

**Expected:** one line per identity, `up to date` or `updated` with what
changed. A second run right after should print `up to date` for every
identity.

### Step 9: inside Claude Code

Start `claude` in `C:\repos\tentaqles\tentaqles` and check:

1. The session opens with the identity preamble: `Client: Tentaqles ...`,
   `Git: github as tentaqles (reach@tentaqles.ai)`, `tq doctor: [ok]`, and no
   mode line.
2. Ask it to read `.env`. **Expected:** blocked, and it suggests
   `tq dotenv run`.
3. Ask it to run `printenv`. **Expected:** blocked.
4. Type `/tentaqles:workspace-status`. **Expected:** the workspace, client
   context and preflight results.
5. Type `/tentaqles:feature` on a repo with a PRD. **Expected:** it runs as a
   checklist (PRD check, interview, spec slice, a yes from you) with no gate
   or hub step.

### Step 10: real systems **(you)**

These need real accounts, so only you can run them:

- **Codex:** repeat step 9 with `codex`. A block in Claude is a block in
  Codex too.
- **Tracker:** run `tq secret set <ws> github`, then `tq secret status`.
  **Expected:** the token is listed as stored, and its value is never printed.
- **Wrong identity:** in a Claude session inside a workspace, edit
  `.tentaqles.yaml`, then ask it to `git push`. **Expected:** blocked as
  `untrusted` until you run `tq allow <ws>` **(you)**.

### Quick troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| The identity doesn't change when you `cd` | The workspace isn't trusted, or activation isn't loaded | `tq list`, then `tq allow <ws>` **(you)**; open a new terminal |
| `claude` skips tq | An old function or alias in a profile | `tq doctor` names the line; delete it |
| A command is blocked with "human-only" | It's meant for you | Run it yourself in a normal terminal |
| A command is blocked as `untrusted` | The manifest changed since `tq allow` | `tq doctor`, then `tq allow <ws>` **(you)** |
| The plugin is out of date for an identity | That identity's marketplace points at the old repo | `claude plugin update tentaqles@tentaqles` with that identity active |
