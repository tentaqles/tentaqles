# CLAUDE.md — tentaqles-plugin

## What this is

Open-source Claude Code plugin for multi-workspace orchestration. Published at `github.com/tentaqles/tentaqles-plugin`. Installed users get it at `<config dir>/plugins/cache/tentaqles/tentaqles/<version>/`, where the config dir is `~/.claude` by default and `$CLAUDE_CONFIG_DIR` (one per identity, under `~/.tentaqles/identities/<id>/claude`) when tq manages the session.

## Architecture

```
tentaqles/              Python package (identity, memory, embeddings, privacy, graph)
  memory/store.py       MemoryStore — per-workspace SQLite (sessions, touches, decisions, pending, semantic/procedural memories, atoms, reference_docs); FTS5 search
  memory/vault_export.py Obsidian mirror: <vault>/<client>/{context,decisions,plan,fixes}.md + sessions/ (frontmatter for Bases)
  memory/ingest.py      Markdown folders -> reference_docs (path+mtime watermark; note's own date)
  knowledge.py          KnowledgeConfig: manifest knowledge: > <plugin data>/knowledge.yaml > ~/.tentaqles/knowledge.yaml > TENTAQLES_VAULT_DIR
  manifest/loader.py    Walks up from cwd to find .tentaqles.yaml
  privacy.py            Secret redaction before any disk write
  embeddings/           fastembed wrapper + graphify integration
scripts/                Hook and utility scripts (all use _path.py for bootstrap)
  tq_env.sh             Runtime bootstrap — resolves interpreter + PYTHONPATH
  tq_run.sh             Wrapper: sources tq_env.sh then exec's target script
  memory-bridge.py      Stdin JSON → MemoryStore dispatch (touch, decision, session_end, etc.)
  vault-export.py       On-demand export + ingest (the export-vault skill); SessionEnd's worker does the same automatically
  knowledge-capture.py  PostToolUse: file touches, decision-pattern scan, and fix atoms (failed Bash command + the matching success that followed; classify_bash_result)
  bootstrap.py          First-run dep installer (pyyaml, pathspec, fastembed, numpy → $CLAUDE_PLUGIN_DATA/lib); installs in a detached --worker
  _path.py              sys.path setup via __file__ (used by all Python scripts)
  _detach.py            spawn_detached(): hand slow work to a process that outlives the hook
  jev-memory-gate.py    Detached worker for the shadow Jev memory gates (tentaqles/memory/jev_gate.py)
skills/                 43 skill directories, each with SKILL.md (some with references/, scripts/, templates/; frontmatter checked by tests/test_skill_frontmatter.py)
agents/                 9 subagents for /tentaqles:build (build-planner, build-critic [read-only], build-fixer, spec-critic [read-only], arbiter, security-reviewer [read-only], privacy-reviewer [read-only], llm-security-reviewer [read-only], breaker)
templates/process/      process templates (PRD, ADR, spec slice, fix spec, LEARNINGS, project CLAUDE.md, review contract, threat model, privacy record, incident playbook)
skills/<name>/references/  checklists with stable row ids (STRIDE, data dictionary, privacy LGPD/GDPR, supply chain SLSA/SBOM, LLM security OWASP LLM 2025 / Agentic 2026)
tentaqles/workflow/     stdlib-only: build gate (gate.py), overnight loop (loop.py, loopguard.py),
                        project.py (tq-project config block), prd.py (PRD planning-ready check)
hooks/hooks.json        Hook definitions (SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Stop)
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
  _tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
  ```
  Then uses `"$TENTAQLES_PY"` instead of `python`. `CLAUDE_PLUGIN_ROOT` is never set inside the Bash tool, so the search is what actually runs; it must look in `$CLAUDE_CONFIG_DIR` first (pinned by `tests/test_skill_bootstrap_snippet.py`).

- **Python scripts** under `scripts/`: start with `from _path import setup_paths; setup_paths()`. Never inline `sys.path.insert` hacks.

- **Privacy**: all text hitting `memory.db` passes through `tentaqles.privacy.redact_text()`. Secrets → `[REDACTED:pattern]`. `redact_text(classes=None)` (read paths) masks secrets only, while `redact_str` applies the checksummed baseline; exits (memory writes, vault, ingest, signals, Jev, model prompts) pass the workspace's data classes (`exit_classes(manifest)`, from the manifest `data:` block; an unknown or empty class list fails closed to the strictest set). Use `redact_str` rather than a local wrapper.
- **Reviewer agents are read-only** (`security-reviewer`, `privacy-reviewer`, `llm-security-reviewer`: tools Read, Glob, Grep). `breaker` is the only agent with Bash and it binds to 127.0.0.1; its scratch dir is `<root>/.tq-breaker/<slug>/` (data files only, deleted by `build-gate finish`).
- **Human-only commands**: `tq allow`, `build-gate approve/judge/answer/finish/token` and `dod --override` are run by the human; skills remind the user and never run them.
- **Key isolation caveat**: the build gate's text checks guard against a careless agent, not one set on defeating it, until tq isolates the signing key at the OS level.

- **Lazy session**: `MemoryStore.end_session()` auto-starts a session if none is active. Don't assume `start_session()` was called.

- **Every hook in hooks.json declares a short `timeout`**; capture-only hooks are `async`. A hook without one waits up to 10 minutes when something hangs.

- **Hooks never wait on the network or a model.** Anything slow (pip installs, session saving, `claude -p` fact extraction) goes to a detached worker via `_detach.spawn_detached`. Model calls go through `tentaqles.memory.llm` only, from detached/background code, and honour `TENTAQLES_SEMANTIC_FACTS=0`.

- **Jev calls** (`tq decide ask`) follow the same rule: hooks only spawn `scripts/jev-memory-gate.py` detached; the gates are shadow-only (log via `tq decide log`, never change behaviour) and honour `TENTAQLES_JEV_MEMORY=0`.

- **Hook payloads name the tool result `tool_response`** (a dict for Bash: stdout/stderr/interrupted). `tool_output` was never what Claude Code sends; accept it only as a fallback.

- **Text a hook hands to the model must not use `$CLAUDE_PLUGIN_ROOT`**: it is expanded in hooks.json commands but not set in the Bash tool. Embed the absolute path resolved from `__file__` (forward slashes).

- **PreCompact stdout never reaches the model.** Context that must survive compaction is printed by the SessionStart hook when `source == "compact"` (`tentaqles.memory.compact_context`).

- **Headless child guard**: `TENTAQLES_HEADLESS_CHILD=1` marks the plugin's own `claude -p` subprocess; hooks that save or prompt (SessionEnd, Stop) must exit early when it is set.

- **Skill-owned hooks stay out of `hooks.json`**: `/tentaqles:build` installs its gate per project (`build-gate.py setup` → `.claude/settings.local.json`, 20 s timeout), and the loop passes its guard to its child via `--settings`. Both hook scripts are stdlib-only and never touch the network.

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
Optional: `fastembed` — lives in the `embeddings` extra, but `bootstrap.py` installs it anyway so graph search works out of the box.
`MemoryStore.search_memory` is SQLite FTS5 (BM25) with a recency boost over sessions, decisions, pending and semantic facts; it needs no embeddings (`_has_fts5()` falls back to word matching on a SQLite without FTS5; pinned by `tests/test_search_memory.py`). The `memory_fts` table is derived: `_sync_fts()` rebuilds it when its row count disagrees with the base tables, so write paths never touch it.
Embedding writes degrade gracefully (writes still succeed, `embedding` stored as NULL) when `fastembed` is missing. That degradation is pinned by `tests/test_embeddings_optional.py`, and CI never installs `fastembed` — so it stays genuinely optional rather than optional-in-theory.
Optional: `graphifyy` (graph engine), `tree-sitter` (native graph), `docling` (PDF/PPTX parsing)

## Git

- Remote: `github.com/tentaqles/tentaqles-plugin`
- Identity: `reach@tentaqles.ai` / `tentaqles`
- Commit style: conventional commits
