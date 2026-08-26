---
name: cross-patterns
description: Show recurring decision patterns detected across all registered client workspaces. Use when the user asks about cross-workspace patterns, repeated solutions, or shared architectural decisions.
triggers:
  - /tentaqles:patterns
  - show cross-workspace patterns
  - what patterns have been detected
---

# Cross-Workspace Patterns

Loads and displays detected recurring decision patterns from all registered workspaces.

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
import json
from tentaqles.memory.pattern_detector import CrossWorkspacePatternDetector

detector = CrossWorkspacePatternDetector.__new__(CrossWorkspacePatternDetector)
from tentaqles.config import data_dir
detector._data_dir = data_dir()
patterns = detector.load_patterns()

if not patterns:
    print('No cross-workspace patterns detected yet.')
    print('Run: python scripts/pattern-cron.py  (or trigger detection via session-end)')
else:
    print(f'## Cross-Workspace Patterns ({len(patterns)} found)\n')
    for i, p in enumerate(patterns, 1):
        ws_list = ', '.join(p.get('workspaces', []))
        print(f'{i}. **{p[\"label\"]}**')
        print(f'   Workspaces: {ws_list}')
        print(f'   Decisions: {p[\"decision_count\"]}  |  Similarity: {p[\"similarity_score\"]}')
        print(f'   Representative: {p[\"representative_decision\"][:120]}')
        print()
"
```

If patterns exist, summarise the key recurring themes and offer to investigate any specific pattern in depth.
If no patterns exist, explain that pattern detection runs weekly (or can be triggered manually with `python scripts/pattern-cron.py`).
