# Changelog

All notable changes to the Tentaqles plugin. Versions follow [semver](https://semver.org/).

## [Unreleased]

Hook diet and reliability: fewer, cheaper hook interruptions, and memory
features that actually run.

### Fixed

- **The Stop hook's "record decisions" command works.** Its instruction told
  the model to run `bash "$CLAUDE_PLUGIN_ROOT/scripts/tq_run.sh" memory-bridge.py`,
  but `CLAUDE_PLUGIN_ROOT` is only expanded in hooks.json — it is not set in
  the Bash tool — so the command failed with exit 127. The instruction now
  embeds the plugin's absolute path (forward slashes) and a properly quoted
  JSON payload.
- **Compaction context reaches the model.** PreCompact hook output is never
  shown to the model, so the state block `pre-compact.py` printed was dropped.
  `session-preamble.py` now prints it (recent decisions, hot nodes, open
  pending) when SessionStart fires with `source: "compact"`. The `PreCompact`
  registration is removed (it saved no state); `pre-compact.py` remains as a
  manual/debug entry point. The block is only built when the workspace's
  `memory.db` already exists, so reading it never creates one.
- **First-run dependency install no longer blocks session start.** The
  SessionStart `bootstrap.py` hook used to run `pip install` (fastembed
  included) inline with a 600s timeout. It now starts a detached
  `bootstrap.py --worker` (via `_detach.py`), prints one notice line and
  returns; a `.bootstrap.lock` in the data dir (stale after 20 minutes) stops
  concurrent sessions from installing twice, and the worker logs to
  `bootstrap.log`. The hook timeout drops from 600s to 30s. `tq_env.sh`
  discards the notice when it runs bootstrap itself so it cannot corrupt
  another hook's JSON output.

### Changed

- **The decision prompt fires far less often.** Besides once per session
  (markers now live in `$CLAUDE_PLUGIN_DATA/stop-capture/<session>.done`,
  pruned after 30 days), never when `stop_hook_active`, and at least 12 tool
  calls, it now also requires the conversation itself — user prompts and
  assistant prose, never tool output — to show a choice between alternatives
  ("instead of", "rather than", "trade-off", "decided", "vs", "option A",
  "em vez de", "decidimos", "optei", ...). Sessions that only did work are no
  longer interrupted.
- **Session-size nudge.** When the last assistant turn's context
  (`input_tokens + cache_read_input_tokens + cache_creation_input_tokens`)
  exceeds 350k tokens, the Stop hook shows a one-time, non-blocking
  `systemMessage` suggesting `/compact` or the session-wrap skill. Override
  the threshold with `TENTAQLES_CONTEXT_NUDGE_TOKENS` (`0` disables).
- The SessionStart `session-preamble.py` hook now declares a 30s timeout.

### Added

- **Semantic facts are extracted.** The fact-extraction step in memory
  consolidation never ran because no model was passed. The detached
  session-end worker and `compaction-cron.py` now use
  `tentaqles.memory.llm.get_semantic_llm()`, which runs
  `claude -p --model haiku --output-format json` (prompt on stdin, redacted
  with `privacy.redact_text`, 60s timeout) under the current environment, so
  `CLAUDE_CONFIG_DIR` — the active identity — is respected. Missing `claude`,
  errors and timeouts yield no facts rather than failures. Never runs in a
  foreground hook. Opt out with `TENTAQLES_SEMANTIC_FACTS=0`. The headless
  child runs with `TENTAQLES_HEADLESS_CHILD=1`, which the plugin's own
  SessionEnd and Stop hooks honour by doing nothing.

## [0.7.0] — 2026-10-08

### Changed

- **The `PreToolUse` hook now covers PowerShell, file tools and MCP calls,
  not just Bash.** The matcher is
  `^(Bash|PowerShell|Read|Edit|Write|MultiEdit|NotebookEdit|Grep)$|^mcp__`.
  `tq claude-hook pre-tool-use` runs the identity guard on PowerShell
  commands too (a `git push` through the PowerShell tool used to skip it)
  and applies tq's policy rules to every call: deny reading SSH keys and
  credential stores, ask before reading `.env`, deny `printenv` dumps and
  force-pushes to main, ask before cloud deletes, `--admin` merges, n8n
  publishes, database writes through MCP, and edits to existing
  migrations, and refuse a `git commit` that would record a secret. See
  `docs/CLAUDE-HOOK.md` → "Policy rules". Requires `tq` 0.2.0 or
  later; with an older `tq` the extra tools are simply allowed.
- **Fallback with neither `tq` nor Python only blocks shell tools.**
  `tq_hook.sh` still fails closed for Bash/PowerShell, but lets Read,
  Edit and MCP payloads through instead of blocking every file read.

## [0.6.0] — 2026-10-01

Session saving at exit was failing far more often than it appeared to, in
several independent ways. This release fixes them and adds a tool to recover
what was already lost.

### Added

- **`scripts/repair-sessions.py` backfills half-written session rows.** Sessions
  whose `SessionEnd` hook was killed part-way left rows with a start time but no
  end and no summary. The script re-reads the transcript each row names, through
  the same parser the hook uses, and fills them in. It is a dry run by default;
  `--apply` writes, after copying each database to `.pre-repair`. `--prune`
  deletes empty rows nothing else references. Run it with
  `bash "$CLAUDE_PLUGIN_ROOT/scripts/tq_run.sh" repair-sessions.py --all`.

### Changed

- **`fastembed` is an optional dependency.** It moved from the required
  dependencies to an `embeddings` extra, so `pip install tentaqles` no longer
  pulls onnxruntime and model downloads. Memory writes without it store a NULL
  embedding and otherwise succeed. Nothing changes for plugin installs:
  `bootstrap.py` still installs it, so semantic search works out of the box.

### Fixed

- **`SessionEnd` no longer loses the session to Claude Code's exit budget.**
  Claude Code aborts every SessionEnd hook still running after 1500ms and
  reports it as `Hook cancelled`; the `timeout` a plugin declares in
  `hooks.json` does not raise that. The save takes about that long on its own
  for a large transcript, before counting the launcher, so sessions were being
  dropped or left started-but-never-ended.

  `session-end.py` now hands its payload to a detached copy of itself
  (`--worker`) and returns. The hook is back in roughly 0.6s; the save lands a
  second or two later, after Claude Code has gone. If the worker cannot be
  started the hook saves inline, as before.

  One consequence: after `/clear`, the next session's preamble can run before
  the worker has finished, so "last session" may lag by one.

- **The hook launcher no longer re-discovers Python on every hook.**
  `tq_env.sh` probed for an interpreter and re-checked dependencies on each
  invocation, two interpreter startups before the hook script began. Both
  answers are now cached in the plugin data directory and revalidated cheaply,
  so a warm run spawns nothing. The cache is invalidated by a plugin upgrade, a
  missing interpreter, or a deleted `lib` directory.

- **Session summaries reflect the work that was done.** Two transcript readers
  assumed a flat entry shape Claude Code never writes, so they matched nothing:
  every automatic summary read "Session with no file changes", no file touches
  were recorded at session end, and no open threads were detected. Parsing now
  lives in `tentaqles/transcript.py` and reads tool calls and user text from
  where they actually are. Tool output and model reasoning are not treated as
  something the user said.

- **No memory row references a session that does not exist.** With no active
  session, decisions and pending items were stamped with the shared `untracked`
  session id without that row being created. Every writer now goes through one
  helper that creates the placeholder first.

## [0.5.0] — 2026-08-31

### Added

- **Decisions are captured without being asked for.** Sessions and pending items
  were already recorded automatically at session end; decisions were written
  only when the `session-wrap` skill ran, which needs somebody to remember to
  say "done" before closing the terminal. In practice that produced nineteen
  days in which every session was logged and not one reason for anything was —
  the what recorded, the why lost.

  A new `Stop` hook asks the model to record what was decided, once per
  session, while it still holds the context. It fires only after a session has
  done real work, writes a marker so a conversation is never asked twice, and
  respects `stop_hook_active` — ignoring that flag is how a stop hook becomes an
  infinite loop.

- **A fallback for terminals that are simply closed.** `session-end.py` now also
  scans the transcript for decisions, the same way it already scans for open
  threads. It matches only text explicitly labelled as a decision, so it
  usually finds nothing. That is deliberate: an earlier version matched
  directives like "use X instead of Y" and, run against two real transcripts,
  stored mid-sentence fragments as decisions. Writing that into a store people
  rely on is worse than writing nothing. Anything it does capture is marked
  `confidence: low` and tagged `auto`.

## [0.4.0] — 2026-08-28

tq is the source of truth: hooks call `tq claude-hook`; Python guard is
fallback-only; preamble no longer switches gh/az/git; client-registry.json
removed; requires tq ≥ 0.1.0.

- `blocked_commands` entries now match as a whole-word prefix (or phrase) at
  the start of a command/segment; the old substring match is gone — an entry
  like `gh ` still matches `gh pr merge`, but `git` no longer matches
  `github`.

## [0.3.1] — 2026-04-16 — "Portable Runtime"

Full cross-platform compatibility for macOS, Linux, and Windows. Fixes plugin breakage on machines where `python` doesn't exist (macOS), resolves to a broken venv shim, or points to a Windows Store stub.

### Added

- **`scripts/tq_env.sh`** — POSIX-compatible runtime bootstrap. Resolves `CLAUDE_PLUGIN_ROOT` (env var → `$BASH_SOURCE` → filesystem search), finds a working Python interpreter (`py -3` → `python3` → `python`, validated via `sys.executable`), exports `PYTHONPATH`, and runs `bootstrap.py` if core deps are missing. Works in bash, zsh, and dash.
- **`scripts/tq_run.sh`** — thin wrapper: sources `tq_env.sh`, then exec's the target script with the resolved interpreter. Used by all hooks and skills.
- **`.gitattributes`** — forces LF line endings on `*.sh`, `*.py`, `*.json`, `*.md` to prevent CRLF "bad interpreter" errors when cloning from Windows to Unix.
- **Touch event acknowledgment.** The `touch` handler in `memory-bridge.py` now prints `{"touch_id": "..."}` on success, matching `decision` and `pending` handler behavior.

### Fixed

- **Duplicate hooks load error.** `plugin.json` no longer declares `"hooks": "./hooks/hooks.json"` — Claude Code auto-loads `hooks/hooks.json`, and the explicit reference caused `Failed to load hooks ... Duplicate hooks file detected`, disabling every hook. Version fields in `plugin.json`, `marketplace.json`, and `pyproject.toml` now all read 0.3.1.
- **macOS compatibility.** Hooks now use `bash tq_run.sh` instead of bare `python` (which doesn't exist on macOS Catalina+). The interpreter probe finds `python3` automatically.
- **POSIX shell compatibility.** Removed `${BASH_SOURCE[0]}` array subscript (breaks dash/sh on Ubuntu/Debian). Uses `$BASH_SOURCE` without subscript, guarded by `$BASH_VERSION` check, with glob fallback for non-bash shells.
- **Windows Store Python.** `bootstrap.py` detects the `WindowsApps/` stub executable and resolves the real interpreter via the `py` launcher before calling `pip install --target`.
- **Plugin cache discovery.** Skill preludes now check `.claude-plugin/plugin.json` (not `plugin.json` at root) and use a marketplace-agnostic glob (`*/tentaqles/*/` instead of `tentaqles/tentaqles/*/`).
- **`session_end` without prior `session_start`.** `MemoryStore.end_session()` auto-creates a session if none is active (lazy upsert).
- **Naive/aware datetime mixing.** `end_session` now treats legacy naive timestamps as UTC, preventing `TypeError` on databases migrated from pre-v0.3 versions.
- **XDG-compliant data paths.** Fallback plugin data dir uses `~/Library/Application Support/tentaqles` on macOS and `~/.local/share/tentaqles` on Linux (was `~/.tentaqles` everywhere).
- **Executable permissions.** `.sh` files are committed with `+x` bit so they work on macOS/Linux without manual `chmod`.
- All 17 skills updated: `"$TENTAQLES_PY"` instead of bare `python`, `PYTHONPATH` instead of `sys.path.insert` hacks.

### Breaking

None. All changes are backward compatible.

## [0.3.0] — 2026-04-13 — "Memory Matures"

Six architectural features that deepen how the plugin remembers, reasons about, and shares knowledge across workspaces.

### Added

- **4-tier memory consolidation with decay.** Brain-inspired tiers: Working → Episodic → Semantic → Procedural. Sessions auto-promote to Episodic on close; important facts climb to Semantic and Procedural over time. Ebbinghaus decay evicts stale entries automatically. New tables `semantic_memories` and `procedural_memories` in `memory.db`; new column `sessions.memory_tier`. Skill: `/tentaqles:compact-memory`. Cron: `scripts/compaction-cron.py`.
- **Contradiction detection and supersession.** When a new decision is recorded, it is embedded and compared cosine-similarity against active decisions. If similarity > 0.82 and the text disagrees, the old decision is auto-superseded — the chain is preserved, not deleted. New column `decisions.contradiction_score`. Skill: `/tentaqles:decision-history`. Programmatic: `MemoryStore.get_decision_lineage()`.
- **Time-travel snapshots.** Append-only JSON snapshots at `{workspace}/.claude/snapshots/{utc_iso}.json` capture the manifest, memory stats, and git identity at a point in time. Auto-fires on identity auto-switch and any Write to `.tentaqles.yaml` via the `snapshot-guard.py` PreToolUse hook. Keeps last 30; older pruned. Skill: `/tentaqles:rollback`.
- **Workspace profiles (learned, not declared).** Auto-generated profile from `memory.db` — hot files, session frequency, top concepts — written to `{workspace}/.claude/profile.json` and injected into the SessionStart preamble. Regenerates automatically when >7 days old. Skill: `/tentaqles:profile-refresh`.
- **Cross-workspace pattern detection.** Weekly background job reads decisions from all registered workspace memory.dbs (read-only), embeds them, clusters them, and surfaces patterns that span two or more workspaces. Output at `{data_dir}/metagraph/patterns.json`, surfaced in `MetaMemory.get_cross_workspace_context()`. Skill: `/tentaqles:cross-patterns`. Cron: `scripts/pattern-cron.py`.
- **Inter-workspace signals (pub/sub).** A `signals` table in the global `meta.db` lets Workspace A emit a message to Workspace B; B reads it on next session start. 48-hour TTL, acknowledge-once. Opt-in per workspace via `signals:` manifest block. Skill: `/tentaqles:emit-signal`.

### Schema

All additions backward compatible (`ALTER TABLE ADD COLUMN` with `OperationalError` catch). Existing v0.2 databases migrate on first open.

- `sessions.memory_tier` column (values: `working`, `episodic`)
- `decisions.contradiction_score` column
- New `semantic_memories` table
- New `procedural_memories` table
- New `signals` table in global `meta.db`

### Manifest

New optional section:

```yaml
signals:
  enabled: true
  subscribe_to: [dirtybird, acme-corp]
```

Missing section = feature disabled. Fully backward compatible.

### Build

- Tests: 60 → 238 (178 new)
- Pip dependencies added: 0
- New modules: 10
- New scripts: 4
- New skills: 6

## [0.2.0] — "Foundation"

The first public release after the rebrand. Six Tier 1 quick wins on top of the workspace identity / temporal memory / knowledge graph foundation.

### Added

- **PreCompact hook re-injection.** Critical state (manifest summary, decisions, hot nodes) is re-injected into the conversation before Claude Code auto-compacts, so important context survives compaction.
- **Privacy filter.** Every observation is scanned for secrets (API keys, JWTs, OAuth tokens, connection strings, private keys, cross-client emails) before touching disk. Integrated into store, session-end, knowledge-capture, and memory-bridge.
- **File history skill.** `/tentaqles:file-history` shows everything the plugin has recorded about a specific file — touches, decisions, pending items, across sessions.
- **Open thread auto-detection.** End-of-session parser detects unresolved threads from the transcript and records them as pending items automatically.
- **Self-improving skills.** User corrections are recorded to the skill's own definition at `{client_root}/.claude/skills/{name}/SKILL.md`. Per-client isolation — one client's corrections never affect another.
- **Real-time dashboard.** Runs at `http://localhost:8765` with a live grid of workspaces, hot nodes, and pending items. Pure stdlib, self-contained HTML, works offline.

Plus the v0.1 foundation: workspace auto-detection via `.tentaqles.yaml`, auto-switching git/gh/az/aws identity, preflight checks, per-client SQLite temporal memory, pluggable knowledge graph engine (graphify or native), and cross-workspace semantic search via the meta-graph.
