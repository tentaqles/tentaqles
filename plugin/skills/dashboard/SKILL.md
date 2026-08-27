---
name: dashboard
description: Launch the Tentaqles real-time dashboard showing all workspaces, session activity, memory stats, and open pending items. Use when the user says "dashboard", "show me the dashboard", "open the dashboard", "launch dashboard", "command center", or wants a visual view of their Tentaqles state.
---

# Dashboard

Launch the Tentaqles dashboard at http://localhost:8765 (or next available port).

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
import urllib.request
import subprocess
import sys
import os

# Check if already running
for port in (8765, 8766, 8767, 8768, 8769, 8770):
    try:
        urllib.request.urlopen(f'http://localhost:{port}/api/health', timeout=1)
        print(f'Dashboard already running at http://localhost:{port}')
        break
    except Exception:
        continue
else:
    # Not running — start it in the background
    plugin_root = os.environ.get('CLAUDE_PLUGIN_ROOT', '.')
    script = os.path.join(plugin_root, 'scripts', 'dashboard_server.py')
    subprocess.Popen(
        [sys.executable, script],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    print('Dashboard started at http://localhost:8765 (or next available port)')
    print('Open in your browser to see live workspace activity.')
"
```
