---
name: build-graph
description: Build a knowledge graph from the current workspace and embed all nodes for semantic search. Supports two engines — graphify (external) or native (built-in). Use when the user wants to analyze codebase structure, map architecture, understand relationships between files/modules, or says "build graph", "map the codebase", "analyze this project".
---

# Build Knowledge Graph

Build a knowledge graph from the current directory, then embed all nodes for semantic search across sessions.

## Check Engine Availability

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
from tentaqles.graph import get_engine
try:
    engine = get_engine()
    print(f'Engine: {engine.name}')
except RuntimeError as e:
    print(f'ERROR: {e}')
"
```

If no engine is available, tell the user their options:
- `pip install graphifyy` — uses the graphify package (recommended, most mature)
- `pip install tentaqles[graph]` — uses the native Tentaqles engine (built-in, includes all enhancements)

## Build the Graph

**If using graphify engine**: Run `/graphify` on the current directory. The graphify skill handles the full pipeline (detection, AST extraction, semantic extraction with subagents, clustering, reporting, HTML visualization). After it completes, continue to the embedding step below.

**If using native engine**:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
import os
from tentaqles.graph import get_engine
from pathlib import Path

engine = get_engine()
result = engine.build(Path(os.getcwd()))
print(f'Graph built: {result.get(\"nodes\", 0)} nodes, {result.get(\"edges\", 0)} edges, {result.get(\"communities\", 0)} communities')
print(f'Output: {result.get(\"output_dir\", \"graphify-out/\")}')
"
```

## Embed Nodes for Semantic Search

After the graph is built (by either engine), embed all nodes:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
from tentaqles.graph import get_engine

engine = get_engine()
result = engine.embed('graphify-out/graph.json')
print(f'Embedded {result[\"nodes_embedded\"]} nodes ({result[\"file_size_kb\"]} KB)')
print('Semantic search is now active for /tentaqles:query-memory')
"
```

## Report

Tell the user:
- How many nodes, edges, communities were found
- Where the outputs are (graphify-out/)
- That semantic search is now active
- Suggest: "Try `/tentaqles:query-memory` to search the graph by meaning"
- If GRAPH_REPORT.md exists, highlight the God Nodes and Surprising Connections sections
