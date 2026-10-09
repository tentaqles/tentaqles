# CLAUDE.md — tentaqles-plugin

## What this is

Open-source Claude Code plugin for multi-workspace orchestration. Published at `github.com/tentaqles/tentaqles-plugin`. Installed users get it at `~/.claude/plugins/cache/tentaqles/tentaqles/<version>/`.

## Architecture

```
tentaqles/              Python package (identity, memory, embeddings, privacy, graph)
  memory/store.py       MemoryStore — per-workspace SQLite (sessions, touches, decisions, pending)
  manifest/loader.py    Walks up from cwd to find .tentaqles.yaml
  privacy.py            Secret redaction before any disk write
  embeddings/           fastembed wrapper + graphify integration
scripts/                Hook and utility scripts (all use _path.py for bootstrap)
  tq_env.sh             Runtime bootstrap — resolves interpreter + PYTHONPATH
  tq_run.sh             Wrapper: sources tq_env.sh then exec's target script
  memory-bridge.py      Stdin JSON → MemoryStore dispatch (touch, decision, session_end, etc.)
  bootstrap.py          First-run dep installer (pyyaml, pathspec, fastembed, numpy → $CLAUDE_PLUGIN_DATA/lib); installs in a detached --worker
  _path.py              sys.path setup via __file__ (used by all Python scripts)
  _detach.py            spawn_detached(): hand slow work to a process that outlives the hook
skills/                 27 skill directories, each with SKILL.md (some with references/, scripts/, templates/; frontmatter checked by tests/test_skill_frontmatter.py)
hooks/hooks.json        Hook definitions (SessionStart, SessionEnd, PreToolUse, PostToolUse, Stop)
.claude-plugin/         Plugin manifest (plugin.json)
tests/                  pytest suite
```

## Runtime contract

**Both hooks and skills** invoke Python through `tq_run.sh` → `tq_env.sh`. Hooks use `bash "${CLAUDE_PLUGIN_ROOT}/scripts/tq_run.sh"` (not `sh` — that causes "cannot execute binary file" on Windows; not bare `python` — that doesn't exist on macOS). The bootstrap chain:

1. `tq_env.sh` resolves `CLAUDE_PLUGIN_ROOT` (env → `$BASH_SOURCE` → filesystem search)
2. Probes interpreters: `python3` → `python` → `py -3`, each bounded to 5 s, validated via `sys.executable`; any `WindowsApps` (Store Python) path is skipped, also from the cache
3. Exports `PYTHONPATH` = `$CLAUDE_PLUGIN_ROOT` + `$CLAUDE_PLUGIN_DATA/lib`
4. Runs `bootstrap.py` if core deps (`yaml`, `pathspec`, `numpy`) fail to import; the deps stamp is keyed on plugin root + interpreter

## Key conventions

- **Skill bash blocks**: every block that calls Python starts with:
  ```bash
  _tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache/tentaqles/tentaqles"/*/; do [ -f "${_d}plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
  ```
  Then uses `"$TENTAQLES_PY"` instead of `python`.

- **Python scripts** under `scripts/`: start with `from _path import setup_paths; setup_paths()`. Never inline `sys.path.insert` hacks.

- **Privacy**: all text hitting `memory.db` passes through `tentaqles.privacy.redact_text()`. Secrets → `[REDACTED:pattern]`.

- **Lazy session**: `MemoryStore.end_session()` auto-starts a session if none is active. Don't assume `start_session()` was called.

- **Every hook in hooks.json declares a short `timeout`**; capture-only hooks are `async`. A hook without one waits up to 10 minutes when something hangs.

- **Hooks never wait on the network or a model.** Anything slow (pip installs, session saving, `claude -p` fact extraction) goes to a detached worker via `_detach.spawn_detached`. Model calls go through `tentaqles.memory.llm` only, from detached/background code, and honour `TENTAQLES_SEMANTIC_FACTS=0`.

- **Text a hook hands to the model must not use `$CLAUDE_PLUGIN_ROOT`**: it is expanded in hooks.json commands but not set in the Bash tool. Embed the absolute path resolved from `__file__` (forward slashes).

- **PreCompact stdout never reaches the model.** Context that must survive compaction is printed by the SessionStart hook when `source == "compact"` (`tentaqles.memory.compact_context`).

- **Headless child guard**: `TENTAQLES_HEADLESS_CHILD=1` marks the plugin's own `claude -p` subprocess; hooks that save or prompt (SessionEnd, Stop) must exit early when it is set.

- **No cross-client data**: each workspace has its own `memory.db`. The global `meta.db` stores only display names, stats, and signals — never code or decisions.

## Development

```bash
# Run tests
source scripts/tq_env.sh && "$TENTAQLES_PY" -m pytest tests/ -v

# Test a single skill's bash block manually
source scripts/tq_env.sh
"$TENTAQLES_PY" -c "from tentaqles.manifest.loader import load_manifest; print(load_manifest('.'))"

# Test memory bridge
echo '{"cwd": ".", "event": "touch", "data": {"node_id": "test.py", "action": "edit"}}' | "$TENTAQLES_PY" scripts/memory-bridge.py
```

## Dependencies

Required (`[project].dependencies`): `pyyaml`, `pathspec`, `numpy`
Optional: `fastembed` — lives in the `embeddings` extra, but `bootstrap.py` installs it anyway so semantic search works out of the box.
Semantic search degrades gracefully (writes still succeed, `embedding` stored as NULL) when `fastembed` is missing. That degradation is pinned by `tests/test_embeddings_optional.py`, and CI never installs `fastembed` — so it stays genuinely optional rather than optional-in-theory.
Optional: `graphifyy` (graph engine), `tree-sitter` (native graph), `docling` (PDF/PPTX parsing)

## Git

- Remote: `github.com/tentaqles/tentaqles-plugin`
- Identity: `reach@tentaqles.ai` / `tentaqles`
- Commit style: conventional commits
