---
name: switch-client
description: Show all registered client workspaces with their identity status and help switch context safely. Use when the user says "switch client", "change workspace", "go to client X", "show my clients", "which client am I in", "list workspaces", or mentions needing to work on a different client. Also triggers when the user asks about git identity, cloud subscription status, or says something like "am I logged into the right account".
---

# Switch Client

Show all registered workspaces with their identity/cloud/database status, and help the user switch safely by running preflight checks and providing fix commands for any mismatches.

The core safety principle: never let the user accidentally run commands against the wrong client's infrastructure. This skill exists to make context switching explicit and verified.

## Load Workspace Registry

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
import json

workspaces = {}
# Try metagraph config
try:
    from tentaqles.metagraph.config import list_workspaces
    workspaces = list_workspaces()
except Exception:
    pass

# Try cross-workspace memory
memory_ctx = ''
try:
    from tentaqles.memory.meta import MetaMemory
    m = MetaMemory()
    memory_ctx = m.get_cross_workspace_context()
    m.close()
except Exception:
    pass

print('WORKSPACES=' + json.dumps(workspaces))
print('---MEMORY---')
print(memory_ctx)
"
```

If no workspaces are registered, tell the user: "No client workspaces registered yet. Run `/tentaqles:add-client` to set up your first one."

## Display Workspace Overview

Show a table with all workspaces:

| Client | Cloud | Database | Git Identity | Last Active |
|--------|-------|----------|-------------|-------------|
| Acme Corp | azure | postgresql | dev@acme.com (github) | 2 hours ago |
| Globex | aws | snowflake | dev@globex.io (gitlab) | 3 days ago |

Also show the cross-workspace memory summary if available (recent activity, hot nodes per workspace).

Highlight the **current workspace** (detected from cwd) if you're inside one.

## Target Client Check

If `$ARGUMENTS` contains a client name (e.g., the user said `/tentaqles:switch-client acme`), focus on that specific client. Otherwise, show all workspaces and ask which one they want to switch to.

## Run Preflight Checks

For the target client, load its manifest and run all checks:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
from tentaqles.manifest.loader import load_manifest, get_client_context, run_preflight_checks, format_context_summary

manifest = load_manifest('{client_root_path}')
ctx = get_client_context('{client_root_path}')
checks = run_preflight_checks(manifest or ctx)
print(format_context_summary(ctx, checks))
"
```

## Provide Fix Commands

For each failing check, provide the exact command to fix it:

| Check | Fix Command |
|-------|------------|
| Git email mismatch | `git config user.email "{expected_email}"` |
| GitHub user wrong | `gh auth switch --user {expected_user}` |
| Azure subscription wrong | `az account set --subscription "{expected_sub}"` |
| AWS profile wrong | `export AWS_PROFILE={expected_profile}` |
| GitLab user wrong | `glab auth login` |

Present these clearly and ask: "Want me to run these fix commands?"

If the user says yes, run each fix command one at a time, confirming success after each:

```bash
git config user.email "{expected}"
# Then verify:
git config user.email
```

## Re-verify After Fixes

After running fix commands, re-run the preflight checks to confirm everything is green:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
"$TENTAQLES_PY" -c "
from tentaqles.manifest.loader import load_manifest, run_preflight_checks
manifest = load_manifest('{client_root_path}')
checks = run_preflight_checks(manifest)
all_pass = all(c['passed'] for c in checks)
for c in checks:
    status = 'PASS' if c['passed'] else 'FAIL'
    print(f'  [{status}] {c[\"section\"]}: {c[\"expected\"]}')
if all_pass:
    print('All checks passed — safe to work.')
else:
    print('Some checks still failing.')
"
```

## Report

If all checks pass: "You're now set up for **{client name}**. All identities verified. `cd {client_root}` to start working."

If some checks still fail: list what's still wrong and what the user needs to do manually (some things like cloud logins require interactive auth that Claude can't do — suggest `! az login` or `! gh auth login`).

## Error Handling

- If a workspace root path no longer exists on disk: note it and suggest removing it from the registry.
- If Python modules aren't available: fall back to running the preflight commands directly (git config, gh auth status, az account show) and comparing manually.
- If a preflight command times out (e.g., az account show when not logged in): report "not logged in" rather than a cryptic error.
