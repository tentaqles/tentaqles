---
name: query-memory
description: Search workspace memory and knowledge graphs semantically. Use when the user asks about past work, decisions, or concepts.
---

# Query Memory

Search across temporal memory (sessions, decisions) and knowledge graph nodes using semantic embeddings.

The user's query is: $ARGUMENTS

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest
from tentaqles.memory.store import MemoryStore
from tentaqles.embeddings.graphify_hook import semantic_search
from pathlib import Path

cwd = os.getcwd()
manifest = load_manifest(cwd)
client_root = manifest.get('_client_root', cwd) if manifest else cwd

# Search memory
store = MemoryStore(client_root)
results = store.search_memory('$ARGUMENTS', limit=3)
if results:
    print('## Memory matches')
    for r in results:
        print(f'  [{r[\"type\"]}] {r[\"text\"][:100]} (score: {r[\"score\"]:.3f})')

# Search graph
graph_path = Path(client_root) / 'graphify-out' / 'graph.json'
if graph_path.exists():
    hits = semantic_search('$ARGUMENTS', str(graph_path), top_k=3)
    if hits:
        print('## Graph matches')
        for h in hits:
            print(f'  {h[\"label\"]} [{h[\"source_file\"]}] (score: {h[\"score\"]})')

store.close()
"
```

Present the results and offer to explore further.
