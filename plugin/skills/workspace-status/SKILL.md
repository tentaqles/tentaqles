---
name: workspace-status
description: Show current workspace detection, client context, and preflight check results. Use when the user asks about their current workspace context or identity status.
---

# Workspace Status

Detect the current client workspace and show `tq`'s view of its health —
`tq` is the source of truth for identity, trust, and hook state.

```bash
tq doctor --json
```

Render the JSON result: workspace name, trust state, whether hooks are
installed, git identity match/mismatch, and any drift codes
(`untrusted`, `env-drift`, `claude-config-drift`, `git-email-drift`, ...).

Also show bundle drift — whether the workspace's `claude.bundle` (plugins,
skills, MCP servers) matches what's actually materialized in its Claude
identity dir:

```bash
tq bundle diff <workspace> --json
```

If there's drift, mention it and point to `tq bundle sync <workspace>` to
reconcile it (don't run `sync` without the user's go-ahead — it writes to
the workspace's Claude identity dir).

## Report

Summarize in plain language:
- Which client workspace (if any) the cwd resolves to
- Whether it's trusted (`tq allow <name>` if not — ask before running)
- Any `tq doctor` findings and their one-line fixes
- Any `tq bundle diff` drift and whether `tq bundle sync` would fix it

## Error Handling

- If cwd is outside any registered base, `tq doctor` reports a neutral/untrusted result — tell the user they're not inside a tq workspace.
- If `tq` is not installed or not on PATH: tell the user identity enforcement is running in fallback mode (the plugin's Python guard) until `tq` is installed, and that bundle status isn't available without `tq`.
