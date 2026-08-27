# Tentaqles — How-To Guide

Practical walkthroughs for using the plugin day to day. Assumes you've already installed it per the [README](README.md).

---

## Table of contents

1. [Onboarding a new client](#1-onboarding-a-new-client)
2. [Starting a new project inside a client](#2-starting-a-new-project-inside-a-client)
3. [Switching between clients safely](#3-switching-between-clients-safely)
4. [Inspecting and modifying client settings](#4-inspecting-and-modifying-client-settings)
5. [Querying memory across sessions](#5-querying-memory-across-sessions)
6. [Looking up a file's history](#6-looking-up-a-files-history)
7. [Building and searching knowledge graphs](#7-building-and-searching-knowledge-graphs)
8. [Using the dashboard](#8-using-the-dashboard)
9. [Recording decisions and pending items](#9-recording-decisions-and-pending-items)
10. [Teaching the plugin your preferences](#10-teaching-the-plugin-your-preferences)
11. [Troubleshooting identity mismatches](#11-troubleshooting-identity-mismatches)
12. [Recovering from a broken manifest](#12-recovering-from-a-broken-manifest)
13. [Privacy: what's captured and what isn't](#13-privacy-what-is-captured-and-what-isnt)
14. [Memory consolidation and decay](#14-memory-consolidation-and-decay)
15. [Decision history and contradiction detection](#15-decision-history-and-contradiction-detection)
16. [Time-travel snapshots and rollback](#16-time-travel-snapshots-and-rollback)
17. [Workspace profiles](#17-workspace-profiles)
18. [Cross-workspace pattern detection](#18-cross-workspace-pattern-detection)
19. [Inter-workspace signals](#19-inter-workspace-signals)
20. [Scheduled background jobs](#20-scheduled-background-jobs)

---

## 1. Onboarding a new client

**Scenario**: You signed a new client "Acme Corp" that uses Azure, PostgreSQL, and GitHub.

```
/tentaqles:add-client
```

The skill will ask you for:
- Client name (required)
- Display name (defaults to slugified name)
- Language (en / pt-BR / other)
- Cloud provider (azure / aws / digitalocean / none)
- Database provider (postgresql / snowflake / databricks / supabase / none)
- Git provider (github / gitlab / azure-devops)
- Git email
- Git username
- PM tool (asana / jira / github-projects / azure-devops / none)
- Workspace root path (where the client folder will live)

After you answer, it:
1. Creates the directory at `{workspaces_root}/{slug}/`
2. Generates `.tentaqles.yaml` with all your answers + the right preflight commands for your chosen providers
3. Creates `.claude/rules/identity.md` with git identity enforcement rules
4. Creates a `CLAUDE.md` skeleton at the client root
5. Configures git identity (via `git includeIf` in your global config, so any repo under that path inherits the right email automatically)
6. Registers the workspace in the Tentaqles meta-graph config
7. Runs initial preflight checks so you know what's already correct and what needs manual setup (e.g., `gh auth login`, `az login`)

**After running it**, you'll be told exactly what's left to configure manually — usually one or two login commands.

---

## 2. Starting a new project inside a client

**Scenario**: Acme wants an inventory API. You're already in `~/repos/acme/`.

```
/tentaqles:add-project
```

The skill detects the current client from the nearest `.tentaqles.yaml` and asks:
- Project name
- Description / goal
- Tech stack (optional)
- External ticket URL (optional — Asana, Jira, GitHub issue, etc.)

If you provide a ticket URL, it fetches the ticket (via `gh issue view` for GitHub, WebFetch otherwise) and extracts title, description, acceptance criteria, assignees, and due date.

Then it creates:
- `{client_root}/{project-slug}/` directory
- `CLAUDE.md` with the project's goal + inherited client context (cloud, database, git)
- `brief.md` if you provided a description or ticket (skipped otherwise — no empty briefs)
- Git repo initialized with the correct email/name from the client manifest (only if the directory isn't already a repo)

**If the parent is a mono-repo** (the client root itself is a git repo), the project folder stays as a subfolder inside that repo — no new git init.

---

## 3. Switching between clients safely

**Scenario**: You've been working on Acme (Azure) and need to switch to Globex (AWS + Snowflake).

**Option A — automatic**: Just `cd` to the Globex workspace and open Claude Code there. The `SessionStart` hook auto-switches git identity, gh account, cloud CLI, etc. You'll see a summary in the session preamble like:

```
Client: Globex Inc (en)
Cloud: aws (globex-prod)
Database: snowflake via python-connector
Git: github as globex-dev (dev@globex.io)
PM: jira
Stack: python, fastapi, snowflake

Auto-switch:
  * gh: acme-dev -> globex-dev
  * aws: profile -> globex
```

**Option B — explicit with safety verification**:

```
/tentaqles:switch-client globex
```

This shows you the target client's full state, runs all preflight checks, and tells you exactly what's still wrong (e.g., "AWS not logged in — run `aws sso login --profile globex`"). You can choose whether to apply fix commands interactively.

**Option C — just ask what's active**:

```
/tentaqles:workspace-status
```

Shows current client, cloud, database, git, active pending items, memory stats, and any preflight failures.

---

## 4. Inspecting and modifying client settings

**See the current settings**:

```
/tentaqles:client-settings
```

or

```
/tentaqles:client-settings show
```

**Change something**:

```
/tentaqles:client-settings change email to new@email.com
/tentaqles:client-settings set cloud to aws
/tentaqles:client-settings add fastapi to stack
/tentaqles:client-settings change database to snowflake
/tentaqles:client-settings switch to gitlab
```

The skill parses your natural-language request, edits `.tentaqles.yaml`, and auto-updates dependent fields (e.g., changing cloud provider regenerates the preflight command and blocked commands). It then re-runs preflight checks so you can confirm the change worked.

---

## 5. Querying memory across sessions

**Scenario**: You're debugging an auth bug in Dirtybird and vaguely remember you already solved something similar.

```
/tentaqles:query-memory JWT signature validation
```

The skill embeds your query via fastembed and semantically searches:
- Session summaries from past sessions
- Recorded decisions and their rationale
- Knowledge graph nodes (if a graph exists for this workspace)

It returns the top matches with scores, dates, and enough context for you to decide whether to pull up the full session.

**Tips**:
- Use natural language — "how did I handle X" works better than "X"
- The query is scoped to the **current** client workspace by default; to search across all workspaces, mention "across all clients"

---

## 6. Looking up a file's history

**Scenario**: You open `src/auth.py` and want to know when it was last touched, by which sessions, and why.

```
/tentaqles:file-history src/auth.py
```

Shows:
- **Touches** — every session that touched this file, with action (read/edit/debug/create), weight, session date, and session summary
- **Related decisions** — decisions whose `node_ids` include this file

Works with absolute or relative paths. If the file was renamed, the history is split — the old path and the new path are separate entries.

**Good for**:
- Understanding why code is the way it is before changing it
- Debugging a regression ("when did this start?")
- Onboarding to unfamiliar code in your own workspace

---

## 7. Building and searching knowledge graphs

**Build a graph for the current workspace**:

```
/tentaqles:build-graph
```

This runs either graphify (if installed) or the native Tentaqles graph engine on the current workspace, producing:
- `graphify-out/graph.json` — the full graph
- `graphify-out/graph.html` — offline-viewable interactive graph
- `graphify-out/GRAPH_REPORT.md` — audit trail with god nodes, clusters, surprising connections

After the graph is built, all nodes are embedded with fastembed so they're searchable semantically.

**Query the graph**:

```
/tentaqles:query-memory show me the authentication flow
```

This searches both memory AND graph nodes, returning matches ranked by semantic similarity.

**Which engine should I use?**

| Engine | Install | Best for |
|--------|---------|----------|
| graphify | `pip install graphifyy` | Most mature, broader language support via tree-sitter + subagent semantic extraction |
| native | `pip install tentaqles[graph]` | Built-in, no extra dependency, includes `.gitignore` support and docling integration |

Set preference via plugin config:

```
graph_engine: native   # or graphify, or empty for auto-detect
```

---

## 8. Using the dashboard

**Launch**:

```
/tentaqles:dashboard
```

Opens a local server at `http://localhost:8765` (falls back to 8766-8770 if occupied). The skill checks if a server is already running before spawning a new one.

**What you'll see**:
- Grid of cards, one per known workspace
- Each card shows: session count, touch count, decision count, open pending count
- Hot nodes with trend arrows (↑ attention rising / → stable / ↓ falling)
- Open pending items sorted by priority

**Live updates**: every 5 seconds via Server-Sent Events. Open it in any browser — no plugin state needed.

**Privacy**: all summaries and descriptions are run through the privacy filter before being served, so secrets don't appear in the HTML.

**Stop the server**: just close the terminal that spawned it, or kill the `dashboard_server.py` process.

---

## 9. Recording decisions and pending items

**Most of the time you don't need to do this manually** — the `SessionEnd` hook automatically:
- Parses your conversation transcript
- Extracts file touches from tool calls
- Detects open threads (TODO, "come back to this", "still need to", etc.) from your messages
- Saves a session summary

**When you want to record something explicit**, use:

```
/tentaqles:session-wrap
```

The skill will ask you (or infer from the conversation):
- What was accomplished (summary)
- Any decisions made, with rationale and rejected alternatives
- Any pending items with priority

Everything goes through the privacy filter, then into the workspace's `memory.db` via `memory-bridge.py`.

**Recording a decision mid-session** via the bridge directly:

```bash
echo '{"cwd": "'$PWD'", "event": "decision", "data": {"chosen": "RS256", "rationale": "allows public key verification in microservice arch", "rejected": ["HS256", "ES256"], "node_ids": ["src/auth.py"]}}' | bash "${CLAUDE_PLUGIN_ROOT}/scripts/tq_run.sh" memory-bridge.py
```

The skill is easier, but this is handy in scripts. `tq_run.sh` resolves the right Python interpreter automatically — never use bare `python` directly.

---

## 10. Teaching the plugin your preferences

Tentaqles has a self-improving skills feature. When you correct the agent during a session (e.g., "no, we always use RS256 here" or "remember that the staging env is called 'qa'"), the `/tentaqles:session-wrap` skill picks up the correction and appends it to the relevant skill's `SKILL.md` file.

**Where corrections are stored**: corrections go to client-local skills at `{client_root}/.claude/skills/{skill_name}/SKILL.md` (not the shared plugin directory). This means corrections never bleed across clients — Acme's preferences don't affect Globex's sessions.

**Idempotent**: if you tell it the same rule twice, it's only recorded once. Jaccard similarity > 0.85 triggers deduplication.

**What counts as a correction**: explicit rules that apply to future behavior. Things like:
- "actually we always..."
- "remember for next time..."
- "that's wrong — do X not Y"
- "correction: ..."

Not: casual questions, reformulations, or polite disagreement that doesn't set a rule.

**Example flow**:
1. Claude makes a code suggestion
2. You say: "no, in Acme we always use black for formatting, not ruff"
3. Session wraps up (either by `/tentaqles:session-wrap` or auto on terminal close — wait, the auto path doesn't detect corrections; only the skill does)
4. Next time you work in Acme, the skill's `## Learned from user feedback` section includes your rule, so Claude picks it up automatically

---

## 11. Troubleshooting identity mismatches

**Symptom**: Your git push fails, or `gh pr create` errors out, or `az` says you're in the wrong subscription.

**Most of the time**, the `SessionStart` hook auto-switches these for you. If it didn't:

1. **Check what Tentaqles thinks is correct**:
   ```
   /tentaqles:workspace-status
   ```
   Shows expected git email, gh user, cloud subscription, and the current values of each.

2. **See all failing preflight checks** and get the exact fix commands:
   ```
   /tentaqles:switch-client {current-client-name}
   ```

3. **Manual fix patterns**:

   | Mismatch | Fix |
   |----------|-----|
   | Git email wrong | `cd {client_root} && git config user.email "expected@email.com"` (or, better, verify `includeIf` is set up globally) |
   | gh account wrong | `gh auth switch --user expected-user` |
   | gh user not authenticated | `gh auth login` then switch |
   | Azure sub wrong | `az account set --subscription "expected-sub"` |
   | AWS profile wrong | `export AWS_PROFILE=expected-profile` (env var, must be set in shell) |
   | DO context wrong | `doctl auth switch --context expected` |

4. **Persistent mismatch across sessions**: Your `.tentaqles.yaml` might have the wrong `expected_user`. Common mistake: putting the email in `expected_user` instead of the username. Fix:
   ```
   /tentaqles:client-settings
   ```
   and check the git section. `expected_user` must be the CLI username (e.g., `renatond`), never the email.

---

## 12. Recovering from a broken manifest

**Symptom**: Session preamble prints errors, or `workspace-status` can't load the manifest.

1. **Check the YAML syntax**:
   ```bash
   python3 -c "import yaml; yaml.safe_load(open('.tentaqles.yaml'))"
   ```
   If it prints an error, fix the syntax (common issues: unescaped colons, missing quotes, wrong indentation).

2. **Check required fields**:
   ```yaml
   schema: tentaqles-client-v1    # required
   client: <slug>                  # required
   display_name: <string>          # optional but recommended
   language: en                    # optional, defaults to en
   ```

3. **If the manifest is unsalvageable**: delete it and recreate with the skill:
   ```bash
   rm .tentaqles.yaml
   ```
   Then in a new Claude Code session at the client root:
   ```
   /tentaqles:add-client
   ```
   and re-answer the questions.

4. **If memory.db is corrupted**: it's at `{client_root}/.claude/memory.db`. Back up first (`copy memory.db memory.db.bak`), then delete it. The next session will create a fresh one. You'll lose temporal history but not your manifest or skills.

---

## 13. Privacy: what's captured and what isn't

**What Tentaqles captures**:
- Session start/end timestamps and duration
- Files touched with action type (read/edit/create/debug)
- Bash commands run (only to detect decision patterns, not stored verbatim)
- Explicit decisions you record via `/tentaqles:session-wrap` or `memory-bridge` event
- Pending items you record or that are auto-detected from "TODO" phrases in your messages
- Session summaries auto-generated from transcript
- A subset of your user messages (the ones flagged as containing decision patterns or open-thread phrases)

**What it does NOT capture**:
- Full conversation transcripts (only summaries and extracted items)
- Full file contents (only paths)
- Tool outputs (scanned for decisions but not stored)
- Private key material, API keys, JWTs, OAuth tokens, connection strings with credentials (redacted before storage via the privacy filter)

**Where it's stored**:
- `{client_root}/.claude/memory.db` — SQLite file, local only, gitignored by default
- `{CLAUDE_PLUGIN_DATA}/meta.db` — cross-workspace meta-memory
- `{CLAUDE_PLUGIN_DATA}/cache/` — embedding cache

**What crosses client boundaries**:
- Workspace status summaries in the meta-memory (so the dashboard and `switch-client` can show all workspaces)
- Client display names and stats

**What never crosses client boundaries**:
- Source code or file contents
- Decisions and their rationale (per-client)
- Pending items (per-client)
- Skill corrections (client-local overrides)
- Cross-client email leaks (detected and redacted when `authorized_emails` is set in the manifest)

**To audit redactions**, enable the audit log by setting an `audit_log_path` in the privacy section of your manifest (planned, not yet wired into config).

---

## 14. Memory consolidation and decay

**What it is**: Tentaqles models memory across four tiers inspired by human memory research. Sessions start in **Working** and auto-promote to **Episodic** when they end. Over time, the consolidator extracts durable facts into **Semantic** (declarative knowledge, e.g. "this team always deploys on Fridays") and **Procedural** (how-to patterns, e.g. "migration steps for this client"). Facts that haven't been accessed or reinforced decay via Ebbinghaus scoring and are eventually evicted.

**Normally automatic**: consolidation runs via `scripts/compaction-cron.py` (see section 20). The memory tier of each session is stored in `sessions.memory_tier` and you can see it in the dashboard or via `query-memory`.

**Manual trigger**:

```
/tentaqles:compact-memory
```

The skill runs consolidation immediately for the current workspace, prints a summary of what was promoted, decayed, or evicted, and shows the resulting tier distribution.

**Expected output**:

```
Memory consolidation complete (acme):
  Promoted to episodic:   3 sessions
  Promoted to semantic:   7 facts
  Promoted to procedural: 2 patterns
  Decayed / evicted:      4 stale entries

Tier distribution:
  working:    1   (current session)
  episodic:  34
  semantic:  22
  procedural: 9
```

**Gotchas**:
- Consolidation is per-workspace. Running it in one workspace does not affect others.
- Semantic and Procedural entries live in `semantic_memories` and `procedural_memories` tables in `memory.db` — they are just as private and gitignored as the rest.
- If `compaction-cron.py` is running on schedule, the manual trigger is additive — running it twice is safe (idempotent for promotion, not for decay timing).

---

## 15. Decision history and contradiction detection

**What it is**: Every time you record a decision, the plugin embeds it and checks cosine similarity against all currently active decisions for the workspace. If a candidate scores above 0.82 similarity AND the text disagrees, the old decision is automatically superseded and the `contradiction_score` column is updated. The chain is preserved — nothing is deleted.

**Viewing the supersession chain**:

```
/tentaqles:decision-history JWT signing algorithm
```

The skill embeds the topic query, finds the most relevant decision, and walks the full supersession chain from oldest to newest.

**Expected output**:

```
Decision chain: JWT signing algorithm (acme)

[superseded] 2025-11-03 — Use HS256 for simplicity
  Rejected: RS256, ES256
  Superseded by: dec_a3f2 (score: 0.91)

[superseded] 2026-01-15 — Use RS256 for microservice arch
  Rejected: HS256
  Superseded by: dec_b7c1 (score: 0.88)

[active]     2026-03-28 — Use RS256 with key rotation every 90 days
  Rejected: HS256
  Rationale: allows public-key verification in each service; rotation reduces blast radius
```

**Programmatic access**:

```python
from tentaqles.memory.store import MemoryStore
store = MemoryStore("/path/to/workspace")
chain = store.get_decision_lineage("dec_b7c1")
```

**Gotchas**:
- The 0.82 threshold catches near-synonymous decisions but ignores genuinely different topics. If you record two unrelated decisions that happen to have high word overlap, inspect the chain with `/tentaqles:decision-history` to confirm no false supersession occurred.
- `contradiction_score` is `NULL` on decisions recorded before v0.3. That's normal — the column is added by migration and old rows are not backfilled.

---

## 16. Time-travel snapshots and rollback

**What snapshots contain**: Each snapshot is an append-only JSON file at `{workspace}/.claude/snapshots/{utc_iso}.json`. It captures the manifest contents, memory stats (session/decision/touch counts per tier), and the active git identity at the moment it was taken. It does NOT contain source code or full memory contents.

**When snapshots fire automatically**:
1. On every identity auto-switch at session start.
2. On any Write to `.tentaqles.yaml` (via the `scripts/snapshot-guard.py` PreToolUse hook).

The last 30 snapshots per workspace are retained. Older ones are pruned automatically on each write.

**Listing and restoring**:

```
/tentaqles:rollback
```

Without arguments, the skill lists all available snapshots with timestamps and a one-line summary (memory stats + git email at that point).

```
/tentaqles:rollback 2026-03-15T09-22-04
```

With a timestamp prefix, the skill shows the full snapshot contents and asks you to confirm before restoring. Restoring writes the manifest back from the snapshot — it does NOT roll back `memory.db`.

**Expected listing output**:

```
Snapshots for: acme (30 available)

  2026-04-12T08-11-03  identity-switch  git=dev@acme.com  sessions=47 decisions=113
  2026-04-10T14-55-19  manifest-write   git=dev@acme.com  sessions=46 decisions=111
  2026-03-28T11-02-44  identity-switch  git=dev@acme.com  sessions=41 decisions=98
  ...
```

**Gotchas**:
- Snapshots are gitignored by default (`.claude/snapshots/` is in the default `.gitignore` scaffold).
- Restoring a manifest snapshot does not undo `memory.db` changes. If you need to roll back memory, use the backup approach in section 12.
- The snapshot directory is created lazily on first write — it will not exist until the first auto-switch or manifest edit.

---

## 17. Workspace profiles

**What it is**: A learned profile generated from `memory.db`, written to `{workspace}/.claude/profile.json`. It summarises: hot files (by decay-weighted touch score), session frequency, and top concepts (most-connected nodes in the knowledge graph, if one exists). It is injected automatically into every SessionStart preamble under a `## Workspace profile` heading.

**When it regenerates**: automatically when the profile is more than 7 days old. You will see a note in the preamble: `Profile refreshed.`

**Manual refresh**:

```
/tentaqles:profile-refresh
```

Forces an immediate regeneration regardless of age. Useful after a big sprint where the hot files have changed significantly.

**Expected preamble injection**:

```
## Workspace profile
Hot files (decay-weighted):
  src/auth.py        1.00  (last touched: today)
  src/models/user.py 0.83
  tests/test_auth.py 0.71

Session frequency: 4.2 sessions/week (last 30d)

Top concepts: authentication, JWT, user-model, rate-limiting
```

**Gotchas**:
- `profile.json` is gitignored by default. It is derived data and can be regenerated at any time.
- If no knowledge graph has been built for the workspace, the "Top concepts" section is omitted.
- Profile generation reads `memory.db` directly — it does not require an active session.

---

## 18. Cross-workspace pattern detection

**What it is**: A background job (`scripts/pattern-cron.py`) loads decisions from all registered workspace `memory.db` files, embeds them, clusters them, and writes patterns that span two or more workspaces to `{data_dir}/metagraph/patterns.json`. The session preamble includes a cross-workspace context section when patterns exist (`MetaMemory.get_cross_workspace_context()`).

All reads of remote `memory.db` files use the SQLite URI `?mode=ro` — the job never writes to another workspace's database.

**Viewing detected patterns**:

```
/tentaqles:cross-patterns
```

The skill reads `patterns.json` and prints each cluster with the workspaces it spans, representative decisions, and a suggested generalization.

**Expected output**:

```
Cross-workspace patterns (last run: 2026-04-10)

Pattern 1 — Authentication strategy
  Workspaces: acme, globex
  Decisions:  "RS256 with 90-day rotation" (acme), "RS256 preferred for API tokens" (globex)
  Suggestion: Consider a shared JWT standards doc across both clients.

Pattern 2 — Database migration approach
  Workspaces: acme, dirtybird
  Decisions:  "Alembic for migrations" (acme), "Alembic preferred over manual SQL" (dirtybird)
  Suggestion: You default to Alembic across multiple clients — worth codifying in a shared runbook.
```

**Gotchas**:
- Patterns only appear after the cron job has run at least once. Running it manually: `python scripts/pattern-cron.py`.
- The job reads across ALL registered workspaces. If a workspace `memory.db` is on a different machine or drive that is not mounted, the job skips it and logs a warning — it does not fail.
- Cross-workspace patterns are a read-only view. No data moves between workspace databases.

---

## 19. Inter-workspace signals

**What it is**: A lightweight pub/sub mechanism backed by a `signals` table in the GLOBAL `meta.db`. Workspace A emits a signal addressed to B; B reads it on next session start and sees it in the preamble. Signals have a 48-hour TTL and are acknowledge-once (reading a signal marks it consumed for that workspace).

Signals are designed for workspace-level events — deploy failed, CI passed, PR merged. They must never carry code, credentials, or client-confidential data.

**Opting in**: add a `signals` block to `.tentaqles.yaml`:

```yaml
signals:
  enabled: true
  subscribe_to: [dirtybird, acme-corp]
```

`subscribe_to` lists the workspace slugs whose signals you want to receive. Without this block, the workspace neither emits nor receives signals.

**Emitting a signal from a session**:

```
/tentaqles:emit-signal
```

The skill asks for: target workspace, signal type (deploy_failed / ci_passed / pr_merged / custom), and a short message (max 280 chars). All content passes through the privacy filter before being written.

**Example interaction**:

```
You: /tentaqles:emit-signal
Plugin: Target workspace slug: dirtybird
You: dirtybird
Plugin: Signal type [deploy_failed / ci_passed / pr_merged / custom]: deploy_failed
You: deploy_failed
Plugin: Message (280 chars max): acme staging deploy failed — migration 0042 errored
You: acme staging deploy failed — migration 0042 errored
Plugin: Signal emitted. Dirtybird will see it on next session start (TTL: 48h).
```

**What the receiver sees** (in their next preamble):

```
Signals (1 unread):
  [deploy_failed] from acme — 2h ago
  "acme staging deploy failed — migration 0042 errored"
  (acknowledged on read)
```

**Gotchas**:
- Signals are stored in the GLOBAL `meta.db`, not in any per-workspace `memory.db`. The isolation boundary is at the storage layer: no workspace database is accessed by another workspace.
- A workspace will only receive signals from workspaces listed in its `subscribe_to`. The sender does not need to list the receiver.
- If a signal is not read within 48 hours it expires automatically. Signals are not retried.
- Disable signals entirely by removing the `signals` block from the manifest (or setting `enabled: false`).

---

## 20. Scheduled background jobs

Two Python scripts support long-running background work. Neither requires a running Claude Code session.

### Memory compaction (`scripts/compaction-cron.py`)

Runs 4-tier consolidation and Ebbinghaus decay across all registered workspaces. Promotes sessions to episodic, extracts semantic and procedural memories, and evicts stale entries.

**Run manually**:

```bash
bash scripts/tq_run.sh compaction-cron.py
```

**Schedule with cron (Linux/macOS)**:

```
0 3 * * * cd /path/to/tentaqles-plugin && bash scripts/tq_run.sh compaction-cron.py >> ~/.claude/logs/compaction.log 2>&1
```

**Schedule with Task Scheduler (Windows)**: point the action to `sh` with arguments `C:\path\to\scripts\tq_run.sh compaction-cron.py`. The runner resolves the correct Python automatically.

**Recommended cadence**: daily, during off-hours. Running it more often is safe but not useful.

### Cross-workspace pattern detection (`scripts/pattern-cron.py`)

Loads decisions from all registered workspaces (read-only), clusters them, and writes `{data_dir}/metagraph/patterns.json`.

**Run manually**:

```bash
bash scripts/tq_run.sh pattern-cron.py
```

**Recommended cadence**: weekly. Pattern detection is computationally heavier (embeddings for every decision across all workspaces) — running it daily is fine but rarely produces meaningfully different output.

**Both scripts**:
- Write logs to stdout (redirect to a file if scheduling via cron or Task Scheduler)
- Exit non-zero on hard failure, zero on success or skipped-workspace warnings
- Do not require or modify `.tentaqles.yaml` — they read the plugin's workspace registry directly

---

## One more thing

If you find something unclear or want a new workflow documented, run:

```
/tentaqles:session-wrap
```

and say "record a pending item: document X workflow in HOW-TO.md". The next session will pick up the pending item and you (or the plugin) can knock it out.

---

See [README.md](README.md) for architecture overview and [ROADMAP.md](ROADMAP.md) (local, not committed) for future features.
