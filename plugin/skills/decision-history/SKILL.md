---
name: decision-history
description: Trace the history of a decision topic. Use when the user asks "why did we change X?", "what decided Y?", "show me the decision lineage for Z", or uses /tentaqles:decision-lineage.
triggers:
  - decision history
  - why did we change
  - decision lineage
  - /tentaqles:decision-lineage
---

# Decision History

Answer "why did we change X?" by searching decisions semantically and traversing the full supersession lineage.

The user's query / topic is: $ARGUMENTS

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
import sys, json, os
from tentaqles.manifest.loader import load_manifest
from tentaqles.memory.store import MemoryStore

cwd = os.getcwd()
manifest = load_manifest(cwd)
client_root = manifest.get('_client_root', cwd) if manifest else cwd

store = MemoryStore(client_root)
topic = '$ARGUMENTS'

# 1. Semantic search for the most relevant active decision.
hits = store.search_memory(topic, limit=5)
decision_hits = [h for h in hits if h['type'] == 'decision']

if not decision_hits:
    print('No decisions found related to:', topic)
    store.close()
    sys.exit(0)

# 2. Take the top-scoring decision and traverse its lineage.
top = decision_hits[0]
print(f'## Best match (score {top[\"score\"]:.3f})')
print(f'   {top[\"text\"][:120]}')
print()

lineage = store.get_decision_lineage(top['id'])
chain = lineage.get('chain', [])

if len(chain) <= 1:
    print('No supersession history — this decision has never changed.')
else:
    print(f'## Lineage ({len(chain)} steps, oldest → newest)')
    for i, node in enumerate(chain):
        marker = '* current *' if node['status'] == 'active' else 'superseded'
        print(f'  [{i+1}] {node[\"created_at\"][:10]}  [{marker}]')
        print(f'       chosen:    {node[\"chosen\"]}')
        print(f'       rationale: {(node[\"rationale\"] or \"\")[:100]}')
        print()

# 3. Show other decision hits as alternatives.
if len(decision_hits) > 1:
    print('## Other related decisions')
    for h in decision_hits[1:]:
        print(f'  - {h[\"text\"][:100]}  (score {h[\"score\"]:.3f})')

store.close()
"
```

Present the lineage in chronological order.  For each superseded step, explain the transition context using the `rationale` field.  If the lineage has only one entry, say the decision has never been changed.
