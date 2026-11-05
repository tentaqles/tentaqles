# CLAUDE.md — tentaqles plugin

## What this is

Open-source Claude Code plugin for multi-workspace orchestration. Lives in `plugin/` of `github.com/tentaqles/tentaqles` and ships from the root marketplace (`.claude-plugin/marketplace.json`, git-subdir pinned to a release tag and its commit sha, updated after each release; `plugin/.claude-plugin/marketplace.json` is a local directory marketplace for development). See `docs/RELEASING.md`. Installed users get it at `<config dir>/plugins/cache/tentaqles/tentaqles/<version>/`, where the config dir is `~/.claude` by default and `$CLAUDE_CONFIG_DIR` (one per identity, under `~/.tentaqles/identities/<id>/claude`) when tq manages the session.

tq is an identity isolator with secrets protection; the plugin adds session memory, the identity preamble, the knowledge graph and the skills. The dev-process skills are plain checklists and the reviewers are five read-only agents.

## Architecture

```
tentaqles/              Python package (identity, memory, embeddings, privacy, graph)
  memory/store.py       MemoryStore — per-workspace SQLite (sessions, touches, decisions, pending, semantic/procedural memories, atoms, reference_docs); FTS5 search
  memory/vault_export.py Obsidian mirror: <vault>/<client>/{context,decisions,plan,fixes}.md + sessions/ (frontmatter for Bases)
  memory/ingest.py      Markdown folders -> reference_docs (path+mtime watermark; note's own date)
  knowledge.py          KnowledgeConfig: manifest knowledge: > <plugin data>/knowledge.yaml > ~/.tentaqles/knowledge.yaml > TENTAQLES_VAULT_DIR
  manifest/loader.py    Walks up from cwd to find .tentaqles.yaml
  privacy.py            Secret and checksummed-id redaction before any disk write
  prd.py                PRD planning-ready check (prd_status, text_status, open_decision_lines) + find_git_root; used by scripts/prd-status.py and the SessionStart preamble
  embeddings/           fastembed wrapper + graphify integration
scripts/                Hook and utility scripts (all use _path.py for bootstrap)
  tq_env.sh             Runtime bootstrap — resolves interpreter + PYTHONPATH
  tq_run.sh             Wrapper: sources tq_env.sh then exec's target script
  tq_hook.sh            Bridge to `tq claude-hook <event>`; falls back to identity-guard.py (fail-closed) without tq
  memory-bridge.py      Stdin JSON → MemoryStore dispatch (touch, decision, session_end, etc.)
  vault-export.py       On-demand export + ingest (the export-vault skill); SessionEnd's worker does the same automatically
  knowledge-capture.py  PostToolUse: file touches, decision-pattern scan, and fix atoms (failed Bash command + the matching success that followed; classify_bash_result)
  prd-status.py         The planning-ready grader the prd, kickoff, feature and direction skills run
  bootstrap.py          First-run dep installer (pyyaml, pathspec, fastembed, numpy → $CLAUDE_PLUGIN_DATA/lib); installs in a detached --worker
  _path.py              sys.path setup via __file__ (used by all Python scripts)
  _detach.py            spawn_detached(): hand slow work to a process that outlives the hook
skills/                 40 skill directories, each with SKILL.md (some with references/, scripts/, templates/; the set and the frontmatter are pinned by tests/test_skill_frontmatter.py)
agents/                 5 read-only reviewer subagents: critic, spec-critic, security-reviewer, privacy-reviewer, llm-security-reviewer (tools Read, Glob, Grep; each reads the diff the caller names plus the spec slice)
templates/process/      process templates (PRD, ADR, spec slice, fix spec, LEARNINGS, project CLAUDE.md, review contract, threat model, privacy record, incident playbook)
skills/<name>/references/  checklists with stable row ids (STRIDE, data dictionary, privacy LGPD/GDPR, supply chain SLSA/SBOM, LLM security OWASP LLM 2025 / Agentic 2026, migration review)
hooks/hooks.json        Hook definitions (SessionStart, PreToolUse, PostToolUse, Stop, SessionEnd)
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
  _tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && printf '%s\n' "${_d%/}"; done | awk -F/ '{print $NF" "$0}' | sort -s -t. -k1,1nr -k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
  ```
  Then uses `"$TENTAQLES_PY"` instead of `python`. `CLAUDE_PLUGIN_ROOT` is never set inside the Bash tool, so the search is what actually runs; it must look in `$CLAUDE_CONFIG_DIR` first and pick the highest installed version, since Claude Code keeps old versions in the cache and a text sort puts 0.10.0 before 0.12.0 (both pinned by `tests/test_skill_bootstrap_snippet.py`).

- **Python scripts** under `scripts/`: start with `from _path import setup_paths; setup_paths()`. Never inline `sys.path.insert` hacks.

- **Privacy**: all text hitting `memory.db` passes through `tentaqles.privacy.redact_text()`. Secrets → `[REDACTED:pattern]`. `redact_text(classes=None)` (read paths) masks secrets only, while `redact_str` applies the exit baseline: secrets plus checksummed identifiers (CPF, CNPJ, card, IBAN). Exits (memory writes, vault, ingest, signals, model prompts) all use that baseline: `exit_classes`/`workspace_classes` return `["checksummed"]`. Use `redact_str` rather than a local wrapper.
- **Reviewer agents are read-only** (`critic`, `spec-critic`, `security-reviewer`, `privacy-reviewer`, `llm-security-reviewer`: tools Read, Glob, Grep; no agent has Bash). A skill hands a reviewer the diff (`git diff <base>...HEAD` written to a file), the spec slice, `CLAUDE.md`, `docs/LEARNINGS.md` and `docs/threat-model.md`, and gets the review contract back (`templates/process/review-contract.md`: `RESULT: PASS|FAIL|BLOCKED`, `F` rows with file:line, `U` rows for the human).
- **The dev-process skills are plain checklists**: `ship`, `feature`, `fix`, `kickoff`, `dod`, `direction` and the review skills record nothing, keep no state under `.claude/`, and never answer for the user: a question goes to the user. "Done" is the checklist `dod` ran and quotes, never a sentence in the transcript; risk is self-assessed in `ship` and a high-risk diff gets the reviewer agents.
- **Human-only commands**: `tq allow`, `tq deny` and `tq secret set` are run by the human in their own terminal; skills print the line, remind the user and never run it.

- **Lazy session**: `MemoryStore.end_session()` auto-starts a session if none is active. Don't assume `start_session()` was called.

- **tq's PreToolUse blocks identity drift, secrets and changes to tq itself, nothing else**: `tq claude-hook pre-tool-use` denies a git, gh or cloud command under the wrong identity or in an untrusted folder, `.env` and secret-store reads, environment dumps, a commit that would record a secret, a cross-client MCP server, any read or write of tq's state under `~/.tentaqles` (the manifest, trust, `config.yaml`, an identity's `settings.json`), a write to the root marker, this plugin's cache, the tq binary or tq's keychain entry, and a human-only tq verb in the shell (`BLOCKED [<category>/<rule>]`, ADR-0005), in every Claude permission mode (see `docs/CLAUDE-HOOK.md`). A skill never writes the manifest: it prints the change and the user writes it, then runs `tq allow`. It has no modes and asks nothing (the one confirm is the fail-safe `tq/guard-error` on a guard panic). Skills must not rely on a confirm or a block for anything else (a force push, a cloud delete, a migration): say what you are about to do and ask the user yourself. Skills never start `claude-off`/`codex-off` or a nested agent with the plugin switched off.

- **Every hook in hooks.json declares a short `timeout`**; capture-only hooks are `async`. A hook without one waits up to 10 minutes when something hangs.

- **Hooks never wait on the network or a model.** Anything slow (pip installs, session saving, `claude -p` fact extraction) goes to a detached worker via `_detach.spawn_detached`. Model calls go through `tentaqles.memory.llm` only, from detached/background code, and honour `TENTAQLES_SEMANTIC_FACTS=0`.

- **Hook payloads name the tool result `tool_response`** (a dict for Bash: stdout/stderr/interrupted). `tool_output` was never what Claude Code sends; accept it only as a fallback.

- **Text a hook hands to the model must not use `$CLAUDE_PLUGIN_ROOT`**: it is expanded in hooks.json commands but not set in the Bash tool. Embed the absolute path resolved from `__file__` (forward slashes).

- **PreCompact stdout never reaches the model.** Context that must survive compaction is printed by the SessionStart hook when `source == "compact"` (`tentaqles.memory.compact_context`).

- **Child-process guard**: `TENTAQLES_HEADLESS_CHILD=1` marks the plugin's own `claude -p` subprocess (semantic-fact extraction); hooks that save or prompt (SessionEnd, Stop) must exit early when it is set.

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

- Remote: `github.com/tentaqles/tentaqles` (monorepo, `plugin/`)
- Identity: `reach@tentaqles.ai` / `tentaqles`
- Commit style: conventional commits
