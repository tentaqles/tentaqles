---
name: compact-memory
description: Manually trigger memory consolidation — extract semantic facts from recent episodic sessions, detect procedural patterns, and evict stale entries. Use when the user says "compact memory", "consolidate memory", or "extract semantic facts".
---

# Compact Memory

Consolidate episodic session summaries into semantic facts, detect repeated workflow patterns (procedural memory), and evict stale entries below the Ebbinghaus decay threshold.

```python
import os
from tentaqles.manifest.loader import load_manifest, get_client_context
from tentaqles.memory.store import MemoryStore
from tentaqles.memory.consolidator import MemoryConsolidator

cwd = os.getcwd()
ctx = get_client_context(cwd)
client_root = ctx.get("client_root", cwd)
store = MemoryStore(client_root)

# Get recent episodic session IDs (up to 20)
conn = store._conn
rows = conn.execute(
    "SELECT id FROM sessions WHERE memory_tier = 'episodic' ORDER BY ended_at DESC LIMIT 20"
).fetchall()
recent_episodic_ids = [r[0] for r in rows]

# Optional: pass an llm_fn here for LLM-assisted fact extraction.
# Without it, only procedural pattern detection and eviction run.
consolidator = MemoryConsolidator(store, llm_fn=None)

# Run compaction on recent episodic sessions
new_fact_ids = consolidator.run_compaction(recent_episodic_ids)

# Detect procedural patterns from decision history
patterns = consolidator.detect_procedural_patterns(min_occurrences=3)

# Evict stale facts
evicted = consolidator.evict_stale()

print(f"Facts extracted: {len(new_fact_ids)}")
print(f"Procedural patterns detected: {len(patterns)}")
print(f"Stale facts evicted: {evicted}")

if patterns:
    print("\nNew procedural patterns:")
    for p in patterns:
        print(f"  - {p['workflow_name']} (x{p['occurrence_count']})")

if new_fact_ids:
    print("\nExtracted semantic facts:")
    facts = store.get_semantic_facts(limit=len(new_fact_ids))
    for f in facts:
        if f["id"] in new_fact_ids:
            print(f"  [{f['category']}] {f['fact']}")

store.close()
```

Report the results clearly: how many facts were extracted, which procedural patterns were found, and how many stale entries were evicted.

If no LLM function is wired (the default), explain to the user that semantic fact extraction requires an LLM integration and only procedural detection + eviction ran.
