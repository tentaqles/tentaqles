---
name: setup-demo
description: Create demo workspaces with sample code to showcase Tentaqles features. Use when the user wants to try Tentaqles with mock data.
---

# Setup Demo

Create two mock client workspaces with sample Python code, documentation, and `.tentaqles.yaml` manifests.

Run the demo setup:
```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -m tentaqles.cli demo "${ARGUMENTS:-./tentaqles-demo}"
```

After creation, tell the user:
1. Two client workspaces were created: Acme Corp (Azure/PostgreSQL/GitHub) and Globex Inc (AWS/Snowflake/GitLab)
2. Each has sample code, docs, and a `.tentaqles.yaml` manifest
3. Try: `cd` into one and run `/tentaqles:workspace-status` to see the context detection
4. Try: `/graphify` in each workspace to build knowledge graphs
5. The manifests have different git identities — the identity guard will warn about mismatches
