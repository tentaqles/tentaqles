# Tentaqles

> Multi-workspace orchestration for developers who work across multiple clients with AI coding assistants.

Tentaqles is a Claude Code plugin that keeps identity, memory, and knowledge isolated per client while surfacing useful patterns across them. Built for freelancers, consultants, and anyone juggling more than one codebase at a time.

**See [CHANGELOG.md](CHANGELOG.md) for release notes.**

## Why

Working across multiple clients with an AI coding assistant creates failure modes that a single-project workflow never encounters: pushing code with the wrong git email, querying the wrong database, leaking one client's context into another's session, losing track of decisions made weeks ago in a different workspace. Tentaqles addresses these directly.

## Features

**Identity isolation.** Prevents pushing code with the wrong git email, running CLI commands against the wrong cloud subscription, or querying the wrong database. Identity switching is owned by `tq` — the `tq` shell hook swaps git identity, env vars, and per-workspace CLI config dirs on `cd`. The plugin does not switch anything itself: `PreToolUse` calls `tq claude-hook pre-tool-use` to verify the Bash command matches the active workspace's identity and blocks it (exit 2) on drift or an untrusted workspace. If `tq` isn't installed, the hook falls back to a dependency-free Python guard that still blocks remote git/gh/cloud commands (fail-closed); if no Python interpreter can be found either, every Bash command is blocked until `tq` or Python is installed.

**Persistent temporal memory.** Tracks sessions, touches, decisions, and pending work per client in a local SQLite database. Survives terminal close, Ctrl+C, `/exit`, and context compaction: right after a compaction the `SessionStart` hook (`source: compact`) re-injects critical state.

**Four-tier memory with decay.** Brain-inspired tiers — Working → Episodic → Semantic → Procedural — with Ebbinghaus decay and auto-eviction. Sessions auto-promote to Episodic on close; important facts climb over time via the `/tentaqles:compact-memory` skill or the `compaction-cron.py` script.

**Contradiction detection.** When a new decision is recorded, it is embedded and compared against active decisions. If similarity exceeds 0.82 and the chosen text disagrees, the old decision is automatically superseded — the chain is preserved, queryable via `/tentaqles:decision-history` or `MemoryStore.get_decision_lineage()`.

**Knowledge graphs with cross-workspace search.** Pluggable graph engine (graphify or native) builds per-client knowledge graphs and embeds every node with fastembed. A meta-graph merges concepts across clients while keeping source code isolated. Ask "have I solved this problem before?" and find answers from other engagements.

**Cross-workspace pattern detection.** A weekly background job reads decisions from all registered workspaces (read-only), clusters them, and surfaces patterns that span two or more workspaces — for example, "you've solved JWT expiration three different ways across three clients." Results appear in the session preamble and via `/tentaqles:cross-patterns`.

**Learned workspace profiles.** Each workspace grows an auto-generated profile — hot files, session frequency, top concepts — that is injected into the SessionStart preamble. No manual tagging required. Regenerates when stale (>7 days) or on demand via `/tentaqles:profile-refresh`.

**Time-travel snapshots.** Before every identity switch (tq switches identity via its shell hook; the plugin verifies and records it) and every write to `.tentaqles.yaml`, the plugin captures an append-only JSON snapshot (manifest, memory stats, git identity). The last 30 are kept and pruned automatically. Restore any prior state interactively with `/tentaqles:rollback`.

**Inter-workspace signals (opt-in pub/sub).** A small global table lets Workspace A emit a message to Workspace B that appears in B's next session preamble. 48-hour TTL, acknowledge-once, workspace-level payloads only (deploy failed, CI passed, PR merged) — never code or credentials. Opt-in per workspace via a `signals:` block in the manifest.

**Privacy-safe capture.** Every observation is scanned for secrets — API keys, JWTs, OAuth tokens, connection strings, private keys, cross-client emails — before touching disk. Secrets are replaced with `[REDACTED:{pattern_name}]` in memory, dashboard output, and correction records.

**Self-improving skills.** When you correct the agent, the correction is recorded to the skill's own definition at `{client_root}/.claude/skills/{name}/SKILL.md`. Per-client isolation — one client's corrections never affect another.

**Real-time dashboard.** `http://localhost:8765` — a live grid of all workspaces with session counts, hot nodes, pending items, and trend indicators. Pure stdlib, self-contained HTML, works offline.

## Quick start

### Install the plugin

```bash
# From the marketplace
/plugin marketplace add tentaqles/tentaqles-plugin
/plugin install tentaqles@tentaqles-tentaqles-plugin

# Or test locally
claude --plugin-dir /path/to/tentaqles-plugin
```

### First-run dependencies

On the first session after installing, the plugin's bootstrap hook automatically installs Python dependencies (`pyyaml`, `pathspec`, `fastembed`, `numpy`) into the plugin's data directory (`${CLAUDE_PLUGIN_DATA}/lib`). This is isolated from your system Python and only happens once.

**Requirements:**
- **Python 3.10+** available via `py -3` (Windows launcher), `python3`, or `python`
- **pip** available (`python -m pip --version`)
- **git** and, optionally, **gh**, **az**, **aws**, **doctl** — whichever CLIs your client manifests use for preflight checks

The plugin never uses bare `python` directly. All hooks and skills go through `scripts/tq_env.sh`, which probes interpreter candidates in order (`py -3` → `python3` → `python`), validates each via `sys.executable` to filter out broken venv shims, and resolves the absolute path. This means the plugin works even when the current directory has a `.venv/` with a broken or incomplete Python — the bootstrap finds a working system interpreter automatically.

If the auto-install fails (no network, pip issues), the plugin runs in degraded mode and prints the manual install command:

```bash
pip install pyyaml pathspec numpy   # required
pip install fastembed               # optional — enables semantic search
```

Without `fastembed` everything still works: memory writes succeed and the
`embedding` column is stored as NULL; memory search still works (SQLite full-text search with recency weighting), only knowledge-graph similarity search is unavailable.

**Optional extras:**
- `pip install tentaqles[embeddings]` — semantic search (adds fastembed)
- `pip install tentaqles[graph]` — native knowledge graph engine (adds tree-sitter)
- `pip install graphifyy` — use graphify as the graph engine instead
- `pip install docling` — rich PPTX/PDF parsing

### Try the demo

```
/tentaqles:setup-demo ~/tentaqles-demo
```

This creates two mock client workspaces (Acme Corp and Globex Inc) with sample Python code, documentation, and `.tentaqles.yaml` manifests. Use them to explore every feature without touching real projects.

### Set up a real client workspace

The easiest way: use the skill.

```
/tentaqles:add-client
```

It interviews you for client name, git identity, cloud provider, database, language, and PM tool, then creates the workspace with a `.tentaqles.yaml` manifest, identity rules, and a CLAUDE.md skeleton.

Or drop a `.tentaqles.yaml` manually at your client root:

```yaml
schema: tentaqles-client-v1
client: my-client
display_name: "My Client"
language: en

cloud:
  provider: azure
  also: [aws]            # optional: extra clouds this workspace may use
  subscription_name: "My Sub"
  preflight: "az account show --query name -o tsv"
  expected: "My Sub"

database:
  provider: postgresql
  dialect: postgresql
  access: mcp
  mcp_server: postgres

git:
  provider: github
  email: "me@client.com"
  user: my-github-user
  host: github
  preflight: "gh auth status --active"
  expected_user: my-github-user

stack: [python, flask, postgresql]

# Optional — inter-workspace signals (F12)
signals:
  enabled: true
  subscribe_to: [dirtybird, acme-corp]

# Optional — what data this client's projects handle (written by /tentaqles:classify-data)
data:
  classes: [pii, financial]
  regions: [BR]
  paths:
    - path: db/schema/customers.sql
      classes: [pii]
  incident: {authority: ANPD, contact: dpo@example.test}
```

Tentaqles will detect this file from any subfolder and enforce the correct identity.

The `data:` block declares which data classes the client's projects handle (`public`, `internal`, `pii`, `sensitive`, `financial`, `children`, `secrets`; `checksummed` and `all` are detector groups). Run `/tentaqles:classify-data` to build it from the PRD and the schema: it also writes `docs/privacy.md` and `docs/incident-playbook.md`. The plugin masks those classes (CPF, CNPJ, card, IBAN, e-mail, phone) in everything it stores or sends: memory, the Obsidian vault, ingest, signals and model prompts. Without a `data:` block only the checksummed identifiers and secrets are masked. Reading personal data locally is never blocked; only the exits are guarded.

## How it works

### Workspace detection

When a Claude Code session starts, Tentaqles walks up from the current directory looking for `.tentaqles.yaml`. The first one it finds defines the client context. Every subfolder inherits it.

```
~/repos/my-client/
  .tentaqles.yaml          <- defines the client
  project-a/               <- inherits my-client context
    src/
      app.py               <- still my-client context
  project-b/               <- also inherits
```

### Identity is managed by `tq`, not the plugin

`tq` — the companion CLI, not this plugin — owns identity switching: its
shell hook sets git email (via `includeIf`), the active `gh`/`az`/`aws`
CLI config, and `CLAUDE_CONFIG_DIR` when you `cd` into a workspace. The
plugin's `SessionStart` hook calls `tq claude-hook session-start`, which
only *reports* the resolved identity (client, git identity, cloud
subscription, `tq doctor` findings) — it does not switch anything.

If a required account isn't authenticated yet, `tq doctor` prints a clear
instruction (e.g., `gh auth login`).

### Preflight / enforcement checks

Before any git, gh, or cloud CLI command, the plugin's `PreToolUse` hook
calls `tq claude-hook pre-tool-use`, which verifies:

| Operation | Check | Blocked if wrong |
|-----------|-------|-----------------|
| `git commit/push` | Git email matches manifest | Yes |
| `gh pr create` | GitHub user matches manifest | Yes |
| `az storage list` | Azure subscription matches | Yes |
| `aws s3 ls` | AWS account matches | Yes |
| Any blocked command | Pattern from manifest | Yes |

A workspace allows the cloud CLI of `cloud.provider` only. To use more than
one cloud, list the extras in `cloud.also` (a list; accepted names:
`azure`, `aws`, `gcp`, `digitalocean` (aliases: `az`, `gcloud`, `do`, `doctl`)), e.g. `also: [aws]`. An unknown name, a scalar, or the
primary cloud repeated is a manifest load error. Every other cloud CLI is still blocked
as wrong-cloud, and the block message names the allowed set ("azure + aws").
Only the trusted manifest (pinned by `tq allow`) can add clouds; a manifest
file in a repo cannot. Each extra cloud also gets its own private config home
under `~/.tentaqles/identities/<workspace>/`, so its login stays apart from
other clients; if the manifest has an `identities:` block that does not list
that cloud's identity (`az`, `aws`, `gcloud`, `doctl`), tq adds it implicitly.
`cloud.also` needs a real `cloud.provider` (not missing or `none`), and a name
listed twice is a load error.

### Temporal memory

Each client workspace gets a SQLite database tracking:
- **Sessions** — start/end, duration, summary, embedded for search
- **Touches** — which files/functions were accessed, with decay scoring
- **Decisions** — what was chosen, what was rejected, and why
- **Pending items** — open work items that carry forward

Activity scores use exponential decay (30-day half-life). Files touched today score 1.0, a month ago 0.5, six months ago ~0.015.

Memory survives session end regardless of how the session ends — clean exit, Ctrl+C, terminal close, or compaction (the `SessionStart` hook re-injects critical state when `source` is `compact`).

### Knowledge graphs

Uses a pluggable graph engine — either [graphify](https://github.com/safishamsi/graphify) or the native Tentaqles engine — to build per-client knowledge graphs, then embeds all nodes with fastembed for semantic search. A meta-graph merges concepts across clients while keeping source code isolated.

### Privacy filter

Every observation passes through a secret-detection pass before touching disk:

| Pattern | Example matched |
|---------|----------------|
| AWS key | `AKIAIOSFODNN7EXAMPLE...` |
| GitHub PAT | `ghp_...` (40 chars) |
| JWT | `eyJ...` three-part |
| Bearer token | `Authorization: Bearer ...` |
| Connection string | `postgres://user:pass@host/db` |
| Private key header | `-----BEGIN RSA PRIVATE KEY-----` |
| API key pattern | `API_KEY=xyz123abcdef...` |
| Cross-client email | `other@other-client.com` when manifest authorizes different |

Secrets are replaced with `[REDACTED:{pattern_name}]` in memory, dashboard output, and SKILL.md correction records.

## Skills

| Skill | What it does |
|-------|-------------|
| `/tentaqles:add-client` | Create a new client workspace with manifest + identity rules + CLAUDE.md |
| `/tentaqles:add-project` | Create a project inside the current client with inherited context |
| `/tentaqles:client-settings` | View or modify any field in the current client's manifest |
| `/tentaqles:switch-client` | Show all clients, verify identity, switch context safely |
| `/tentaqles:workspace-status` | Show current client context + preflight check results |
| `/tentaqles:onboard` | Connect and verify every company tool of this client (agent and cloud CLIs, git host, tracker, chat and email MCP connectors): prints each login for you to run (`tq login`, `tq secret set`, `acli jira auth login`), guides `/mcp`, verifies with `tq doctor --json` and read-only connector calls, ends with a connected table; never handles tokens |
| `/tentaqles:sync` | Board sync's MCP fallback when tq has no background credential (`needs-session`): reads assigned items with the connector into `tq hub sync import`, sends the outbox rows `tq hub sync pending` lists, acks each with `tq hub sync ack`; never answers a send/skip decision |
| `/tentaqles:session-wrap` | End-of-session save: summary, decisions, pending items, corrections |
| `/tentaqles:wrap` | End-of-day wrap: what landed (git/gh), open PRs + CI state, memory via session-wrap, a daily note |
| `/tentaqles:ship` | Working tree → PR: identity, branch, `tq decide triage` review depth, tests, commit, `gh pr create` |
| `/tentaqles:babysit-pr` | Loop a PR to green: watch checks, fix failures from logs, answer review threads, record a new project's first ratchet baseline from CI's own measurement; never `--admin` |
| `/tentaqles:n8n-triage` | Diagnose a failing n8n workflow read-only, fix a draft copy, promote only with confirmation |
| `/tentaqles:build-graph` | Build the knowledge graph for the current workspace + embed nodes |
| `/tentaqles:query-memory` | Full-text, recency-weighted search over memory, plus knowledge graphs |
| `/tentaqles:file-history` | Show everything Tentaqles has recorded about a specific file |
| `/tentaqles:explore` | Run `tq decide explore` to find the few file:line ranges that answer a question, then read only those |
| `/tentaqles:guard` | See and explain the guard mode (default, relaxed, strict, custom) and which tq guard rules are on; switch single rules off or on, or change ask/deny, for one workspace or all (you confirm each change). Mode changes are human-only: the skill hands you the `tq mode` command to run |
| `/tentaqles:dashboard` | Launch the real-time dashboard at localhost:8765 |
| `/tentaqles:setup-demo` | Create mock client workspaces to explore the plugin safely |
| `/tentaqles:compact-memory` | Manually trigger 4-tier memory consolidation and decay eviction |
| `/tentaqles:export-vault` | Write memory as Obsidian notes (context, decisions, plan, fixes, sessions) and ingest configured Markdown folders |
| `/tentaqles:decision-history` | Surface the supersession chain for a topic; show contradiction scores |
| `/tentaqles:rollback` | List snapshots, preview one, and restore it interactively |

### Engineering-practice skills

Stack-aware guidance that triggers on the work itself (no workspace state needed):

| Skill | What it does |
|-------|-------------|
| `/tentaqles:db-migration` | Safe schema changes for Postgres/Supabase and SQL Server: expand/contract, lock-safe DDL, up/down, pre-flight and rollback plan |
| `/tentaqles:index-advisor` | Read EXPLAIN / execution plans, find missing FK, unused and duplicate indexes, output lock-safe DDL plus how to verify it |
| `/tentaqles:ci-pipeline` | GitHub Actions templates (Node/TS, Python, Go) with pinned SHAs and a ratchet quality gate (`baseline.json` never gets worse; a metric not measured yet is `null` until CI's first measurement is recorded) and a gitleaks `secrets` job |
| `/tentaqles:auth-security` | IdP vs own auth, argon2id, sessions vs JWT, refresh rotation, CSRF, rate limits, MFA, RLS, secrets, ASVS-style checklist |
| `/tentaqles:system-design-review` | Review a design/RFC across SLOs, data, failure modes, idempotency, observability, security, cost, rollback; ranked findings |
| `/tentaqles:grill-me` | Interview the user on every business-rule branch before coding; write the spec and a "verifier first" test list |
| `/tentaqles:profile-refresh` | Regenerate the learned workspace profile from `memory.db` |
| `/tentaqles:cross-patterns` | Display cross-workspace patterns detected by the pattern cron job |
| `/tentaqles:emit-signal` | Emit an inter-workspace signal to one or more registered workspaces |
| `/tentaqles:build` | Build a feature through gated checkpoints in its own worktree: spec, approved plan, locked tests, an evidence-based test gate tied to the code on disk, risk-triaged reviewer crew with recorded verdicts, fixer loop, definition of done (agents `build-planner`, `build-critic`, `build-fixer`, plus the crew `spec-critic`, `security-reviewer`, `privacy-reviewer`, `llm-security-reviewer`, `breaker` and the `arbiter`) |
| `/tentaqles:prd` | Write or revise `docs/prd.md` through a short interview: numbered requirements with binary checks, data classes, security boundaries, open decisions |
| `/tentaqles:plan-cards` | Turn a PRD release into `feature` cards on the `tq hub` board, one per FR (title, FR text, acceptance checks), under a project card and folder; writes the card ids into PRD §11 between `<!-- tq:cards -->` markers so a re-run skips them; never tags `agent-ready` |
| `/tentaqles:refine` | Take one planning-lane card to `agent-ready`: grill-me on its FR, a refined body (rules decided, acceptance checks, open questions) you save with the printed `tq hub edit --body-file` line, then the `agent-ready` tag; moving the card stays yours |
| `/tentaqles:kickoff` | Start a new project from a planning-ready PRD: stack ADR, project CLAUDE.md, threat model, privacy record, CI ratchet, then the first checkpoints through the build engine |
| `/tentaqles:feature` | Add a feature from a PRD requirement (adds its own FR when missing): grill-me interview, spec slice, gated build, definition of done |
| `/tentaqles:fix` | Fix a bug reproduce-first: locked failing regression test, root cause, smallest change, risk-sized review (`--hotfix` skips the critic, never the test) |
| `/tentaqles:direction` | Read-only drift audit against the PRD: grades each requirement Aligned/Partial/Missing/Contradicted and returns ON COURSE, DRIFTING or OFF COURSE |
| `/tentaqles:dod` | Run the definition of done for a build (`build-gate dod`) and turn MISSING items into work; a waiver is human-only (`--override`, run by you in a terminal, or the override answer on the hub card in a hub run); fails on PRD §10 decisions the build opened and left open |
| `/tentaqles:threat-model` | Write or update `docs/threat-model.md`: STRIDE per trust boundary from the PRD and the code; unmitigated high-risk rows become requirements or open decisions; only you accept a risk |
| `/tentaqles:classify-data` | Classify the data a project handles: manifest `data:` block, `docs/privacy.md` (purpose, legal basis, retention) and `docs/incident-playbook.md` |
| `/tentaqles:privacy-review` | Run the `privacy-reviewer` (LGPD/GDPR) on a diff that touches classified data and turn its findings into fixes and privacy-record updates |
| `/tentaqles:supply-chain` | Pin actions to SHAs, least-privilege workflow permissions, lockfile-only installs, dependency audit, SBOM on release, secret scanning; extends `ci-pipeline` |
| `/tentaqles:llm-security-review` | Run the `llm-security-reviewer` on a diff that adds LLM calls, prompts, agent tools, MCP servers, RAG or hooks, against OWASP LLM 2025 and Agentic 2026 |
| `/tentaqles:loop` | Overnight Karpathy-style loop on one feature: approve locked checks by day; at night a runner keeps or undoes one headless round at a time in a worktree, under dollar/token/time/round budgets with MCP and network off; morning summary + signal |
| `/tentaqles:hub` | See what `tq hub` runs are waiting on, in plain language, add or show cards; prints the `tq hub answer` line for you (answering is yours); explains the hub's own question when a run stopped before its build existed |
| `/tentaqles:pack` | Run and explain the tq pack checks (data, ml, cloud, db): the enabled packs, `tq pack check`, each finding, the pack reviewers from `tq pack crew`, and how to fix a declared tool that is missing |
| `/tentaqles:data-contract` | Write and evolve ODCS v3 data contracts (data pack): draft from a dbt model or a table, classify columns, check breaking changes with `data/contract-breaking` |
| `/tentaqles:pipeline-review` | Run the `pipeline-reviewer` on a pipeline change (dbt, Airflow, Dagster, Prefect, pandas/polars/PySpark, Fabric or Synapse): idempotency, backfills, PII |
| `/tentaqles:warehouse-cost` | Estimate a query's or dbt model's cost before it runs with `tq pack data cost estimate` (BigQuery, Snowflake, Databricks, Synapse, Postgres) |
| `/tentaqles:ml-review` | Run the `ml-reviewer` on a training, feature, evaluation, model card or promotion change, quoting the ml pack results |
| `/tentaqles:model-card` | Write or update `MODEL_CARD.md` from the metrics, the data manifest and git, interviewing you for intended use and limits |
| `/tentaqles:ml-promote` | Request a model promotion through the ml pack's approval gate (`tq pack ml promote request`); you approve in your terminal and run `promote apply` yourself |
| `/tentaqles:drift-check` | Check a model for data drift: refresh the profiles (aggregates only), run `tq pack ml drift`, explain the result |
| `/tentaqles:iac-policy` | Check IaC with the cloud pack (`IAC`, `IAM`, `AZR`, `GCP` rules and the external linters) and take a change through plan, approve and apply; never answers an approval |
| `/tentaqles:rls-tests` | Write and run pgTAP tests that prove Postgres/Supabase row-level security against a local test database (`db/pgtap` or `db/pgtap-supabase`) |
| `/tentaqles:restore-drill` | Prove a backup restores: `tq pack db drill` into a local scratch database, record the evidence line, check its freshness |

## Hooks

All hooks are automatic and run silently.

| Hook | Fires on | What it does |
|------|----------|-------------|
| `SessionStart` | Session begins (and after compaction) | `bootstrap.py` (one-time deps install, started in the background — never blocks), `tq_hook.sh session-start` → `tq claude-hook session-start` (report resolved identity + `tq doctor`; no switching), then `session-preamble.py --memory-only` (inject memory context; after a compaction, re-inject decisions, hot nodes and open pending instead) |
| `PreToolUse` | Before Bash commands | `tq_hook.sh pre-tool-use` → `tq claude-hook pre-tool-use` — verify git/gh/cloud identity against the workspace manifest, block on drift (exit 2). Falls back to `identity-guard.py` (fail-closed) if `tq` isn't installed. MCP tool calls are not gated in 0.4.0. With a `data:` block, tq's data-protection exit rules also guard commits, pushes, PR text and fixtures that carry personal data (reading it stays allowed). |
| `PostToolUse` | After Bash/Edit/Write | `knowledge-capture.py` — scan output for decisions, record file touches |
| `Stop` | Claude finishes a turn | `stop-capture.py` — once per session, if the conversation weighed alternatives, ask Claude to record the decisions; once per session, if the context passes ~350k tokens, suggest `/compact` or session-wrap (non-blocking); `tq_hook.sh stop` → `tq claude-hook stop` — Jev completion-evidence check (only with a `decision:` block; shadow mode logs, enforce mode sends Claude back once per session when it claims verification the transcript does not show) |
| `UserPromptSubmit` | You send a prompt | `tq_hook.sh prompt-submit` → `tq claude-hook prompt-submit` — Jev skill picker (only with a `decision:` block; logs the pick, and in enforce mode adds a one-line "Skill that may fit" hint; never blocks) |
| `SessionEnd` | Session ends (any reason) | `session-end.py` — parse transcript, detect open threads, save summary to memory; the detached worker also extracts semantic facts with `claude -p --model haiku` (opt out: `TENTAQLES_SEMANTIC_FACTS=0`) |

`/tentaqles:build` and `/tentaqles:loop` bring their own hooks without touching `hooks.json`: `build-gate.py setup` writes the checkpoint gate into the project's `.claude/settings.local.json` (removed by `build-gate.py finish`), and `loop-runner.py` passes its guard to the headless child through `--settings`. The gate's checks are text checks, not isolation: the signing key stays readable by the agent's OS user until tq isolates it, so the gate guards against a careless agent, not one set on defeating it.

All hooks and skills use `tq_run.sh` → `tq_env.sh` to resolve a working Python interpreter, bypassing broken venv shims and machines where only `python3` exists (macOS). POSIX-compatible, tested on Windows (Git Bash), macOS, and Linux.

Under `TQ_MODE=off` (`tq off`, `claude-off`, exactly `off`), `tq_hook.sh` and `tq_run.sh` exit 0 before they look for tq or Python: every hook and every skill script silently does nothing in that window, with no error. The legacy `TQ_ENABLED=0` kill switch only skips shell activation; hooks and skill scripts still run under it.

## CLI

```bash
tentaqles demo [path]    # Create demo workspaces
tentaqles status [path]  # Show workspace detection + preflight results
tentaqles init           # Initialize Tentaqles in current workspace
```

## Dashboard

Launch with `/tentaqles:dashboard` or:

```bash
python3 -m tentaqles.dashboard.server
```

Opens at `http://localhost:8765` (falls back to 8766-8770 if port is busy). Shows a live grid of all known workspaces with:
- Session, touch, decision, and pending counts
- Hot nodes with trend indicators (↑ rising / → stable / ↓ falling)
- Open pending items sorted by priority
- Live updates every 5 seconds via Server-Sent Events

Zero external dependencies — pure stdlib, self-contained HTML, works offline.

## Documentation

See [HOW-TO.md](HOW-TO.md) for detailed walkthroughs:
- Onboarding a new client
- Starting a new project inside a client
- Switching between clients safely
- Querying memory across sessions
- Building and searching knowledge graphs
- Using the dashboard
- Recording decisions and pending items
- Teaching the plugin your preferences (self-improving skills)
- Troubleshooting identity mismatches
- Recovering from a broken manifest

## Requirements

- Python 3.10+
- Claude Code
- Required deps: `pyyaml`, `pathspec`, `numpy` (auto-installed)
- Optional: `fastembed` or `tentaqles[embeddings]` for semantic search (auto-installed by bootstrap), `graphifyy` or `tentaqles[graph]` for knowledge graphs, `docling` for rich doc parsing

## License

MIT
