# Tentaqles

> Multi-workspace orchestration for developers who work across multiple clients with AI coding assistants.

Tentaqles is a Claude Code plugin that keeps identity, memory, and knowledge isolated per client while surfacing useful patterns across them. Built for freelancers, consultants, and anyone juggling more than one codebase at a time.

**See [CHANGELOG.md](CHANGELOG.md) for release notes.**

## Why

Working across multiple clients with an AI coding assistant creates failure modes that a single-project workflow never encounters: pushing code with the wrong git email, querying the wrong database, leaking one client's context into another's session, losing track of decisions made weeks ago in a different workspace. Tentaqles addresses these directly.

## Features

**Identity isolation.** Prevents pushing code with the wrong git email, running CLI commands against the wrong cloud subscription, or querying the wrong database. Preflight checks run automatically before every external operation, and the right account is auto-switched on session start (git via `includeIf`, gh via `auth switch`, Azure via `account set`, DigitalOcean via `doctl auth switch`).

**Persistent temporal memory.** Tracks sessions, touches, decisions, and pending work per client in a local SQLite database. Survives terminal close, Ctrl+C, `/exit`, and context auto-compaction via a `PreCompact` hook that re-injects critical state.

**Four-tier memory with decay.** Brain-inspired tiers — Working → Episodic → Semantic → Procedural — with Ebbinghaus decay and auto-eviction. Sessions auto-promote to Episodic on close; important facts climb over time via the `/tentaqles:compact-memory` skill or the `compaction-cron.py` script.

**Contradiction detection.** When a new decision is recorded, it is embedded and compared against active decisions. If similarity exceeds 0.82 and the chosen text disagrees, the old decision is automatically superseded — the chain is preserved, queryable via `/tentaqles:decision-history` or `MemoryStore.get_decision_lineage()`.

**Knowledge graphs with cross-workspace search.** Pluggable graph engine (graphify or native) builds per-client knowledge graphs and embeds every node with fastembed. A meta-graph merges concepts across clients while keeping source code isolated. Ask "have I solved this problem before?" and find answers from other engagements.

**Cross-workspace pattern detection.** A weekly background job reads decisions from all registered workspaces (read-only), clusters them, and surfaces patterns that span two or more workspaces — for example, "you've solved JWT expiration three different ways across three clients." Results appear in the session preamble and via `/tentaqles:cross-patterns`.

**Learned workspace profiles.** Each workspace grows an auto-generated profile — hot files, session frequency, top concepts — that is injected into the SessionStart preamble. No manual tagging required. Regenerates when stale (>7 days) or on demand via `/tentaqles:profile-refresh`.

**Time-travel snapshots.** Before every identity auto-switch and every write to `.tentaqles.yaml`, the plugin captures an append-only JSON snapshot (manifest, memory stats, git identity). The last 30 are kept and pruned automatically. Restore any prior state interactively with `/tentaqles:rollback`.

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
pip install pyyaml pathspec fastembed numpy
```

**Optional extras:**
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
```

Tentaqles will detect this file from any subfolder and enforce the correct identity.

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

### Auto-switching identity

On every session start, Tentaqles reads the manifest and ensures:
- **git email** — configured via `git includeIf` so any repo under the client root uses the right email automatically
- **gh account** — switched with `gh auth switch --user` if the active account doesn't match
- **Azure subscription** — switched with `az account set` if on Azure
- **DigitalOcean context** — switched with `doctl auth switch` if using DO

If a required account isn't authenticated yet, the preamble prints a clear instruction (e.g., `gh auth login`).

### Preflight checks

Before any git, cloud CLI, or database operation, Tentaqles verifies:

| Operation | Check | Blocked if wrong |
|-----------|-------|-----------------|
| `git commit/push` | Git email matches manifest | Yes |
| `gh pr create` | GitHub user matches manifest | Yes |
| `az storage list` | Azure subscription matches | Yes |
| `aws s3 ls` | AWS account matches | Yes |
| Any blocked command | Pattern from manifest | Yes |

### Temporal memory

Each client workspace gets a SQLite database tracking:
- **Sessions** — start/end, duration, summary, embedded for search
- **Touches** — which files/functions were accessed, with decay scoring
- **Decisions** — what was chosen, what was rejected, and why
- **Pending items** — open work items that carry forward

Activity scores use exponential decay (30-day half-life). Files touched today score 1.0, a month ago 0.5, six months ago ~0.015.

Memory survives session end regardless of how the session ends — clean exit, Ctrl+C, terminal close, or auto-compaction (via a `PreCompact` hook that re-injects critical state).

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
| `/tentaqles:session-wrap` | End-of-session save: summary, decisions, pending items, corrections |
| `/tentaqles:build-graph` | Build the knowledge graph for the current workspace + embed nodes |
| `/tentaqles:query-memory` | Semantic search over memory and knowledge graphs |
| `/tentaqles:file-history` | Show everything Tentaqles has recorded about a specific file |
| `/tentaqles:dashboard` | Launch the real-time dashboard at localhost:8765 |
| `/tentaqles:setup-demo` | Create mock client workspaces to explore the plugin safely |
| `/tentaqles:compact-memory` | Manually trigger 4-tier memory consolidation and decay eviction |
| `/tentaqles:decision-history` | Surface the supersession chain for a topic; show contradiction scores |
| `/tentaqles:rollback` | List snapshots, preview one, and restore it interactively |
| `/tentaqles:profile-refresh` | Regenerate the learned workspace profile from `memory.db` |
| `/tentaqles:cross-patterns` | Display cross-workspace patterns detected by the pattern cron job |
| `/tentaqles:emit-signal` | Emit an inter-workspace signal to one or more registered workspaces |

## Hooks

All hooks are automatic and run silently.

| Hook | Fires on | What it does |
|------|----------|-------------|
| `SessionStart` | Session begins | `bootstrap.py` (one-time deps install), then `session-preamble.py` (detect workspace, auto-switch identity, inject context + memory) |
| `PreToolUse` | Before Bash commands | `identity-guard.py` — verify git/gh/az/aws identity, block wrong-context operations |
| `PreCompact` | Before context auto-compaction | `pre-compact.py` — re-inject critical state (decisions, hot nodes, open pending) |
| `PostToolUse` | After Bash/Edit/Write | `knowledge-capture.py` — scan output for decisions, record file touches |
| `SessionEnd` | Session ends (any reason) | `session-end.py` — parse transcript, detect open threads, save summary to memory |

All hooks and skills use `tq_run.sh` → `tq_env.sh` to resolve a working Python interpreter, bypassing broken venv shims and machines where only `python3` exists (macOS). POSIX-compatible, tested on Windows (Git Bash), macOS, and Linux.

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
- Required deps: `pyyaml`, `pathspec`, `fastembed`, `numpy` (auto-installed)
- Optional: `graphifyy` or `tentaqles[graph]` for knowledge graphs, `docling` for rich doc parsing

## License

MIT
