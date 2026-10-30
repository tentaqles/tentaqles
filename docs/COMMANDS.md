# Tentaqles commands

Every Tentaqles command in one place, with what it does, plus a test walkthrough
you can follow to check that an install works.

Tentaqles has three parts:

- **`tq`**: the command-line tool. It picks the right identity for each client
  folder (git email, cloud login, agent config), guards what agents run, and
  hosts the hub board and the packs.
- **The Claude Code plugin** (`/tentaqles:...`): skills that agents follow
  (build, fix, ship, review), reviewer agents, and the memory hooks.
- **`build-gate`**: the script behind `/tentaqles:build` that records test and
  review evidence for each checkpoint.

**Human-only** means the command refuses to run from an agent session. You run
it yourself in your own terminal. That keeps decisions about trust, approvals
and secrets with a person.

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
| `tq allow <name>` | **Human-only.** Trusts the workspace's manifest as it is now (by hash). Until then, tq exports nothing for that folder. |
| `tq allow --mode <name> relaxed\|custom` | **Human-only.** Lets the workspace use a guard mode looser than `default`. The grant is tied to the manifest hash, so editing the manifest drops it. |
| `tq deny <name>` | Removes trust, and any mode grant, from a workspace. |
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
| `tq off` / `tq on` | Shell functions. `tq off` clears the identity and turns tq off in that terminal; `tq on` turns it back on. |
| `claude` / `codex` | Launchers from the activation block. They always start the agent with permission prompts skipped, because tq's guard mode decides what is allowed. |
| `claude-off` / `codex-off` | Start the agent with no tq, no identity and no guard. |

### Health checks

| Command | What it does |
|---|---|
| `tq doctor` | Checks hooks, trust, git identity, plugins, settings and the shell launcher against every manifest. It changes nothing. Each problem comes with the fix to run. |
| `tq providers list\|show\|check\|add` | The catalog of supported tools (cloud, git host, data, agents, chat). `check` shows which CLIs are installed. |
| `tq secrets audit` | Finds secret-shaped values in agent config files, and `.env` files that git would commit. It shows only the path and line, never the value. |
| `tq worktrees list` / `prune --yes` | Lists leftover git worktrees from agent runs. `prune` removes the ones that are clean, merged and old, and never forces. |
| `tq insights` | A read-only report across identities: Claude Code cost and tokens, the largest sessions, shell failure rates, guard blocks per rule and Jev results. |

### Agent settings and plugins

| Command | What it does |
|---|---|
| `tq settings render [--all]` | Writes tq's part of each identity's Claude `settings.json`: baseline permissions, env, status line and hooks. It prints what changed. |
| `tq bundle catalog` | Shows the shared catalog of plugins, skills and MCP servers. |
| `tq bundle diff <ws>` | Shows how a workspace's installed plugins differ from its `claude.bundle`. |
| `tq bundle sync <ws>` | Installs or removes plugins so the identity matches its bundle. |
| `tq bundle capture` | Builds a bundle entry from an identity that is already set up. |

### Guard: what agents may run

Every tool call an agent makes passes through tq's guard. Each rule **allows** a
call, **asks** you to confirm it, or **blocks** it. Rules are grouped into
categories. Four categories are **locked** and always apply: identity,
secrets, self-protection and privacy.

| Command | What it does |
|---|---|
| `tq mode` | Shows the current guard mode and the action for each category. |
| `tq mode set default\|relaxed\|strict\|custom` | **Human-only.** Changes the mode for a workspace, a project or globally. |
| `tq mode toggle <category> skip\|ask\|deny\|rule` | **Human-only.** Changes one category; the mode becomes `custom`. |
| `tq mode override <rule> skip\|ask\|deny\|rule\|clear` | **Human-only.** Changes one rule. |
| `tq mode edit` | **Human-only.** Changes the mode and its categories in an interactive editor. |
| `tq mode log` | Lists the confirms and blocks the current mode skipped or softened. |
| `tq guard list` | Lists every rule with its default action, its current action and what changed it. |
| `tq guard off\|on <rule>` | Turns single rules off or back on, for one workspace or all of them. |
| `tq guard set <rule> ask\|deny\|default` | Changes a rule between ask and deny. |
| `tq unattended ...` | Old name, kept for compatibility. Use `tq mode`. |

The modes:

- **default**: every rule uses its own action.
- **relaxed**: skips every category that can be turned off. git-history and system still ask.
- **strict**: blocks prod-changes, cloud-destructive and git-history; asks before external writes.
- **custom**: a preset plus your own per-category values.
- **off**: never stored. Use `claude-off`, `codex-off` or `tq off`.

### Secrets and personal data

| Command | What it does |
|---|---|
| `tq dotenv run [--file F] -- <cmd>` | Runs a command with a `.env` file loaded and replaces each value with `[REDACTED:NAME]` in the output. This is the only allowed way for an agent to use a `.env`. |
| `tq redact` | Reads text and masks emails, phone numbers, Brazilian tax IDs (CPF, CNPJ), card numbers, IBANs and secrets. `tq redact run -- <cmd>` does the same to a command's output. |
| `tq secret set\|rm\|status` | **Human-only.** Stores tracker tokens (GitHub, Azure DevOps, Jira, Linear, Asana) in the OS keychain. It never prints them. |

### Jev decisions (`tq decide`)

Jev is a fast yes/no model (TypeSafe) that tq asks small questions. In
`shadow` mode it only logs its answers. In `enforce` mode its answers count.

| Command | What it does |
|---|---|
| `tq decide status` | Shows the workspace's Jev settings and whether a key was found. It never shows the key. |
| `tq decide triage --staged` | Rates a diff as low or high risk (auth, money, schema, secrets, ...) to choose how deep the review goes. |
| `tq decide explore "<question>"` | Finds the few `file:line` ranges that answer a question about the code. |
| `tq decide ask` | Asks Jev one batch of questions about some state. |
| `tq decide eval` | Scores Jev on labelled cases and picks thresholds. |
| `tq decide log` | Records a judgment from another tool, so all of them are measured in one place. |

### Hub board (`tq hub`)

A local task board for each workspace, at `http://127.0.0.1:7470`. Cards
that are ready for an agent run headless through `/tentaqles:build`. Each
decision a run waits on appears on the board.

| Command | What it does |
|---|---|
| `tq hub serve [--all]` | Starts the board for this workspace, or for every trusted one. |
| `tq hub status` | Shows the hubs that are running. |
| `tq hub open [card]` | Opens the board or one card in the browser. |
| `tq hub add` | Adds a card. Agents may add cards; only a human sets a budget. |
| `tq hub list [--waiting]` | Lists cards, or with `--waiting`, the decisions waiting on you. |
| `tq hub show <card>` | Shows a card's body, decisions, runs and comments. |
| `tq hub edit <card>` | Edits a card. Agents may change only the tags, on a planning card. |
| `tq hub move <card> <lane>` | Moves a card. Moving an agent-ready card into an active lane starts a run. |
| `tq hub run <card>` | Starts or resumes a run now. |
| `tq hub cancel <card>` | Stops a run. `tq hub run` resumes it later. |
| `tq hub comment <card> "text"` | Comments on a card. `ship` posts the PR link this way. |
| `tq hub answer <card> <decision>` | **Human-only.** Answers a decision a run is waiting on, signed. |
| `tq hub review <card>` | The review page for a run's gate records. |
| `tq hub push <card>` | **Human-only.** Sends a local card to the company tracker. |
| `tq hub sync now\|status\|outbox` | Mirrors the company tracker into the board and sends queued updates back. |
| `tq hub sync import\|pending\|ack` | Used when tq has no stored token: the agent reads the tracker through its MCP connector and reports back with these. |
| `tq hub sync retry <row>` | **Human-only.** Resends a failed update. |

### Packs (`tq pack`)

Packs add checks, guard rules and reviewers for one kind of work: **data**,
**ml**, **cloud** and **db**. Only the manifest's `packs:` block turns a pack
on. A repo's `tq-packs.yaml` can make a pack stricter, never looser.

| Command | What it does |
|---|---|
| `tq pack list` | Lists each pack, whether it is on, and its checks. |
| `tq pack check [--staged\|--base ref\|--paths ...]` | Runs the checks of the enabled packs on the changed files. Exit 0 means all passed, 1 means a required check failed, 2 means a config error. |
| `tq pack doctor` | For each tool: declared, installed, version and minimum version. |
| `tq pack crew` | The reviewer agents to use for the current risk flags. |
| `tq pack approvals` | Pack approvals: pending, approved, used and expired. |
| `tq pack baseline` | **Human-only.** Records the current findings of external tools as the baseline, so only new ones fail. |
| `tq pack cloud plan` | Runs `terraform`/`tofu` plan, `az ... what-if`, `cdk synth`, a CloudFormation change set or `pulumi preview`. It stores the plan outside the repo and prints only a summary. |
| `tq pack cloud show <plan>` | Shows a stored plan's redacted summary and check results. |
| `tq pack cloud approve <plan>` | Asks a human to approve applying a plan. Agents may ask; only a human answers. |
| `tq pack cloud prune` | Deletes old stored plans. |
| `tq pack db approve <env>` | Asks a human to approve running migrations against an environment. It records the migration files and the newest restore drill. |
| `tq pack db drill <name>` | Restores a backup into a local scratch database, records row counts and the migration version, then drops the scratch database. |
| `tq pack data cost estimate` | Estimates what a warehouse query will cost without running it. |
| `tq pack ml drift` | Compares a model's current data with its reference and reports which features moved. |
| `tq pack ml promote request` | Checks the model card, drift and reproducibility, then asks a human to approve a promotion. |
| `tq pack ml promote apply` | **Human-only.** Re-checks the approval and runs the promotion after a typed yes. |
| `tq pack ml promote status` | Lists promotion requests and their state. |

### Hooks the agent calls (you don't run these)

| Command | What it does |
|---|---|
| `tq claude-hook pre-tool-use` | Decides allow, ask or block for each tool call. |
| `tq claude-hook session-start` | Prints the workspace and identity preamble at session start. |
| `tq claude-hook prompt-submit` | Logs which skill Jev thinks fits the prompt. |
| `tq claude-hook stop` | Asks Jev whether a "tests pass" claim is backed by the transcript. |

---

## 2. Plugin skills (`/tentaqles:<name>`)

Type them in Claude Code, or just describe the task; the skill's description
makes it start on its own.

### Build and ship

| Skill | What it does |
|---|---|
| `prd` | Writes or revises `docs/prd.md` through a short interview: problem, users, numbered requirements with pass/fail checks. |
| `kickoff` | Starts a new project: stack decision, CLAUDE.md, threat model, data classification, CI, then the first build. |
| `feature` | Adds one PRD requirement: interview, spec slice, checkpoint plan, gated build, definition of done. |
| `fix` | Fixes a bug reproduce-first: a failing test first, then the root cause, then the smallest fix. `--hotfix` skips the critic, never the test. |
| `build` | The gated build engine. Each checkpoint gets locked tests, an implementer who can't edit them, a recorded test run and an isolated review. |
| `grill-me` | Asks about every business-rule edge case (dates, money, permissions, failures) before code is written. |
| `dod` | Runs the definition of done and turns missing items into work. |
| `ship` | Turns finished work into a pull request: identity check, branch, risk triage, review, tests, commit, push, PR. It never merges. |
| `babysit-pr` | Watches a PR until CI is green and every review comment is answered. |
| `direction` | Read-only check of a repo against its PRD: each requirement is aligned, partial, missing or contradicted. |
| `loop` | An overnight improvement loop for one feature with a test score. Keeps a change only when the score improves. |
| `rollback` | Restores a workspace manifest from an earlier snapshot. |

### Hub board

| Skill | What it does |
|---|---|
| `hub` | Shows what agent runs are waiting on, in plain language; adds or shows cards. |
| `plan-cards` | Turns a PRD release into one hub card per requirement. |
| `refine` | Makes one card ready for an agent run: interview, decided rules, pass/fail checks. |
| `sync` | Mirrors the company tracker into the board through the agent's connector. |

### Reviews

| Skill | What it does |
|---|---|
| `system-design-review` | Reviews a design, RFC or ADR: requirements, failure modes, retries, security, cost, rollback. |
| `threat-model` | Writes `docs/threat-model.md` with STRIDE threats for each trust boundary. |
| `auth-security` | Designs or audits login, sessions, tokens, authorization and RLS. |
| `llm-security-review` | Reviews LLM, prompt, agent tool, MCP and hook code against the OWASP LLM and Agentic Top 10. |
| `privacy-review` | Reviews changes that touch personal data against LGPD and GDPR. |
| `classify-data` | Classifies the data a project handles and writes the manifest's `data:` block. |
| `supply-chain` | Hardens CI: actions pinned to commit hashes, least permissions, lockfile installs, an SBOM. |
| `ci-pipeline` | Sets up GitHub Actions CI with a quality gate that fails when any metric gets worse. |

### Database, cloud, data and ML (packs)

| Skill | What it does |
|---|---|
| `pack` | Runs and explains the pack checks of this workspace and picks the reviewers. |
| `db-migration` | Plans a safe schema change for Postgres or SQL Server: expand/contract, up and down scripts, a rollback plan. |
| `index-advisor` | Diagnoses a slow query and recommends indexes. |
| `rls-tests` | Writes and runs pgTAP tests that prove row-level security. |
| `restore-drill` | Proves a backup restores, with `tq pack db drill`. |
| `iac-policy` | Checks infrastructure code and takes a change through plan and approval. |
| `data-contract` | Writes and evolves ODCS data contracts and flags breaking changes. |
| `pipeline-review` | Reviews dbt, Airflow, Dagster, Prefect, pandas, PySpark and Fabric changes. |
| `warehouse-cost` | Estimates a query's cost before it runs. |
| `ml-review` | Reviews training, features, evaluation and promotion changes. |
| `model-card` | Writes or updates `MODEL_CARD.md` for a model. |
| `drift-check` | Checks a model for data drift. |
| `ml-promote` | Requests promotion of a model to production through the approval gate. |
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
| `guard` | Explains the guard mode and its rules, and changes single rules. |
| `setup-demo` | Creates demo workspaces with sample code. |

### Memory and knowledge

| Skill | What it does |
|---|---|
| `query-memory` | Searches past sessions, decisions, pending items and facts. |
| `decision-history` | Traces why a decision was made and how it changed. |
| `file-history` | Every session that touched a file and why. |
| `explore` | Finds where a question is answered in the code before reading files. |
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

The build engine picks these automatically. You can also ask for one by name.

| Agent | Role |
|---|---|
| `build-planner` | Turns a spec into a checkpoint plan. |
| `build-critic` | Hostile, read-only review of one checkpoint. |
| `build-fixer` | Fixes failed findings at the root cause, or disputes them with evidence. |
| `spec-critic` | Checks the diff does exactly what the spec asks, no more and no less. |
| `arbiter` | Settles a finding that the fixer and critic disagree on. |
| `breaker` | Starts the app locally and attacks it like a hostile user. |
| `security-reviewer` | Application security (ASVS 5.0, OWASP Top 10). |
| `privacy-reviewer` | Data protection (LGPD, GDPR). |
| `llm-security-reviewer` | LLM and agent security. |
| `db-reviewer`, `iac-reviewer`, `pipeline-reviewer`, `ml-reviewer` | Pack reviewers for database, infrastructure, data pipelines and ML. |

---

## 3. `build-gate`

`build-gate` records the evidence behind `/tentaqles:build`. It lives at
`plugin/scripts/build-gate.py`.

| Command | What it does |
|---|---|
| `setup` / `ping` | Installs the gate hooks for the repo / checks they are active. |
| `status` / `state <slug>` | Shows a build's progress, for people / as JSON for the hub. |
| `lock-tests` / `unlock-tests` | Locks a checkpoint's tests so the implementer can't change them. |
| `verify` | Runs the tests and records the result against the exact code on disk. |
| `review-bundle` / `record-review` | Builds the review package / records a reviewer's verdict. |
| `dod <slug>` | Runs the definition of done. |
| `ask` | Records a decision a human has to make. |
| `approve`, `judge`, `answer`, `finish`, `token` | **Human-only.** Approve the plan, settle a disputed review, answer a decision, close the build, mint a hub token. |
| `serve` | A local read-only progress page. |

---

## 4. Test walkthrough

Follow these steps in order, in a **new** PowerShell or Git Bash window.

The expected results below are real output from this machine on 2026-10-29,
in the `tentaqles` workspace in `relaxed` mode. Names, versions and counts will
differ on your machine; the shape should match.

Steps marked **(you)** are human-only or touch real systems, so only you can
run them.

### Step 1: tq is installed and current

```powershell
tq version
tq pack --help
```

**Expected:** a version line (`tq dev` for a local build), then the pack help
starting with "Packs add checks, guard rules, reviewers and commands for one
kind of work."
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
alias shadowing the launcher.

### Step 5: guard mode

```powershell
tq mode
```

**Expected (relaxed):**

```
guard mode for tentaqles: relaxed (set by workspace)
ceiling: relaxed grant, granted 2026-10-29T21:15:20Z
  identity           rule  locked
  secrets            rule  locked
  self-protection    rule  locked
  privacy            rule  locked
  git-history        ask   tentaqles
  prod-changes       skip  tentaqles
  cloud-destructive  skip  tentaqles
  ...
```

The four locked categories always show `rule locked`.

### Step 6: the guard decides (simulated tool calls)

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

| Tool call | Expected in relaxed | Expected with `TQ_MODE=default` |
|---|---|---|
| `Bash`: `git status` | nothing printed, `exit 0` (allowed) | same |
| `Read`: `file_path` = `...\.env` | `BLOCKED [tq/env-file-read]`, `exit 2` | same (locked) |
| `Bash`: `printenv` | `BLOCKED [tq/env-dump]`, `exit 2` | same (locked) |
| `Bash`: `tq allow --mode tentaqles relaxed` | `BLOCKED [tq/mode-change]`, `exit 2` | same (locked) |
| `Bash`: `git push --force origin main` | JSON with `"permissionDecision":"ask"` and `CONFIRM [tq/force-push-main]`, `exit 0` | `BLOCKED [tq/force-push-main]`, `exit 2` |
| `Bash`: `terraform destroy -auto-approve` | nothing printed, `exit 0` (skipped) | JSON with `"ask"` and `CONFIRM [tq/iac-destroy]` |

To try the default column, put `TQ_MODE=default` before the command:
`TQ_MODE=default tq claude-hook pre-tool-use < /tmp/p.json`.

**Exit codes:** 0 with no output means allow; 0 with JSON means ask; 2 means
block.

### Step 7: the mode log records what was softened

```powershell
tq mode log --since 24h
```

**Expected:** one row per confirm the mode skipped or block it softened, for
example:

```
TIME                  SCOPE      MODE     ACTION        RULES               TOOL  COMMAND
2026-10-29T21:26:00Z  workspace  relaxed  deny -> ask   tq/force-push-main  Bash  git push --force origin main
2026-10-29T21:25:59Z  workspace  relaxed  ask -> allow  tq/iac-destroy      Bash  terraform destroy -auto-approve
```

### Step 8: secrets stay masked

```bash
cd "$(mktemp -d)"
printf 'DEMO_SETTING=hello\n' > demo.env
tq dotenv run --file demo.env -- bash -c 'echo setting is $DEMO_SETTING'
rm demo.env
```

**Expected:** `setting is [REDACTED:DEMO_SETTING]`, exit 0 (every value in the file is
masked, whatever its name).

```bash
printf 'contact tester@acme.com, card <number>\n' | tq redact
```

Type a 16-digit test card number in place of `<number>` (any card network's published
test number works). It is not written here because tq's own commit guard refuses card
numbers in a public repo.

**Expected, outside a classified workspace:**
`contact [REDACTED:email], card [REDACTED:card]`.

Inside a workspace, `tq redact` masks only the classes in the manifest's
`data:` block (plus secrets). A workspace with no `data:` block passes the
email through unchanged, which is expected.

### Step 9: human-only commands refuse agents

Ask Claude in a session: "run `tq secret status`".

**Expected:**
`tq: human-only: run it in your own terminal or on the hub page ...`, exit 1.
Run it yourself in a normal terminal **(you)** and it works.

### Step 10: Jev and triage

```powershell
tq decide status
```

**Expected:**

```
workspace: tentaqles
backend:   typesafe
mode:      shadow
key:       found (TYPESAFE_API_KEY)
```

`key: not found` means Jev calls are skipped and triage treats every diff as
high risk.

With a staged change in a repo, run:

```powershell
tq decide triage --staged; echo $LASTEXITCODE
```

**Expected:** `low` with exit 0, or `high (flags...)` with exit 3.

### Step 11: packs

```powershell
tq pack list
tq pack check
```

**Expected:** `tq pack list` shows each pack (`cloud disabled`, `data
disabled`, ...) and its checks marked `builtin required` or `external
advisory (tool)`. With no pack enabled, `tq pack check` prints `no packs
enabled` with exit 0.

To test a pack for real **(you)**, use a throwaway project:

1. Add this to its `.tentaqles.yaml`: `packs: { db: { enabled: true } }`.
2. Run `tq allow <ws>`. Editing the manifest drops trust until you re-allow it.
3. Add a migration that drops a column, then run `tq pack check`.
   **Expected:** a db finding and exit 1.
4. Run `tq pack doctor`. **Expected:** each declared tool with its installed
   version, or `missing` plus an install hint.

### Step 12: hub board

```powershell
tq hub status
tq hub serve
```

**Expected:** `no hub is running; start one with: tq hub serve`. Then
`tq hub serve` starts the board at `http://127.0.0.1:7470`.

In a second terminal:

```powershell
tq hub add "walkthrough test card"
tq hub list
tq hub open
```

**Expected:** the card appears in the list and on the page in the browser.
Delete or archive it on the page afterwards.

### Step 13: agent settings

```powershell
tq settings render --all
```

**Expected:** one line per identity, `up to date` or `updated` with what
changed. A second run right after should print `up to date` for every
identity.

### Step 14: inside Claude Code

Start `claude` in `C:\repos\tentaqles\tentaqles` and check:

1. The session opens with the identity preamble: `Client: Tentaqles ...`,
   `Git: github as tentaqles (reach@tentaqles.ai)`, `tq doctor: [ok]`.
2. Ask it to read `.env`. **Expected:** blocked, and it suggests
   `tq dotenv run`.
3. Ask it to run `git push --force origin main`. **Expected:** a confirm
   prompt in relaxed, a block in default.
4. Type `/tentaqles:workspace-status`. **Expected:** the workspace, client
   context and preflight results.
5. Type `/tentaqles:guard`. **Expected:** the mode and rules explained in
   plain words.

### Step 15: real systems **(you)**

These need real accounts, so only you can run them:

- **Codex:** repeat step 14 with `codex`. A rule that asks in Claude
  becomes a block in Codex; `tq mode --agent codex` lists those rules.
- **Cloud:** in a test project with Terraform, run `tq pack cloud plan`.
  **Expected:** a summary plus the apply form the gate accepts, never the plan
  itself. Then run `tq pack cloud approve` and answer it with
  `tq hub answer`.
- **Database:** declare a restore drill and run `tq pack db drill <name>`
  against a local test server. **Expected:** one new line in
  `docs/ops/restore-drills.jsonl`.
- **Tracker:** run `tq secret set <ws> github`, then `tq hub sync now` and
  `tq hub sync status`. **Expected:** the tracker's items on the board, and
  `last error` empty.
- **CI fuzzing:** on GitHub, open Actions → `fuzz` → **Run workflow**.
  **Expected:** a green run.

### Quick troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| The identity doesn't change when you `cd` | The workspace isn't trusted, or activation isn't loaded | `tq list`, then `tq allow <ws>` **(you)**; open a new terminal |
| `claude` skips tq | An old function or alias in a profile | `tq doctor` names the line; delete it |
| A guard asks for everything | The mode is `default` with no relaxed grant | `tq allow --mode <ws> relaxed` **(you)** |
| A command is blocked with "human-only" | It's meant for you | Run it yourself in a normal terminal |
| The plugin is out of date for an identity | That identity's marketplace points at the old repo | `claude plugin update tentaqles@tentaqles` with that identity active |
