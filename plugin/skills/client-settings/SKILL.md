---
name: client-settings
description: View and modify client workspace settings — cloud provider, database, git identity, stack, language, blocked commands, and any other manifest field. Use when the user says "change email", "update cloud", "set database", "add to stack", "change git provider", "update settings", "configure client", "show settings", "what's my config", or wants to modify any part of the .tentaqles.yaml manifest. Also triggers when the user says they just learned something about the client's infrastructure ("oh they actually use AWS not Azure") or wants to add technologies to the stack.
---

# Client Settings

View and modify any field in the current client's `.tentaqles.yaml` manifest. This is the single place to configure cloud, database, git, stack, language, and everything else about a client workspace.

## Detect Current Client

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest
manifest = load_manifest(os.getcwd())
if manifest:
    print('PATH=' + manifest['_manifest_path'])
    print('CLIENT=' + manifest['client'])
else:
    print('NO_CLIENT')
"
```

If `NO_CLIENT`: "You're not inside a client workspace. Run `/tentaqles:add-client` first."

## Two Modes

### Mode 1: Show current settings (no arguments or "show"/"list"/"what")

Read the `.tentaqles.yaml` and display it as a clean table:

```
Client: Acme Corp (en)

  Cloud:    azure (acme-prod subscription)
  Database: postgresql via mcp [postgres]
  Git:      github as alice-dev (alice@acme.example)
  PM:       asana
  Stack:    python, flask, postgresql, n8n

  Blocked:  gh repo delete, git push --force main, az group delete
```

Then say: "To change anything, just tell me — e.g., 'change email to x@y.com' or 'add fastapi to stack'."

### Mode 2: Modify settings (user provides what to change)

Parse the user's request and map it to the manifest field. Common patterns:

| User says | Manifest field | Action |
|-----------|---------------|--------|
| "change email to x@y.com" | `git.email` | Update |
| "set cloud to aws" | `cloud.provider` | Update + regenerate preflight |
| "change database to snowflake" | `database.provider` + `database.dialect` | Update both |
| "add fastapi to stack" | `stack` | Append |
| "remove django from stack" | `stack` | Remove |
| "set language to pt-BR" | `language` | Update |
| "change git user to newuser" | `git.user` + `git.expected_user` | Update both |
| "switch to gitlab" | `git.provider` + `git.host` + `git.preflight` | Update + regenerate |
| "add rate limit to blocked" | `git.blocked_commands` or `cloud.blocked_commands` | Append |
| "set subscription to X" | `cloud.subscription_name` | Update |
| "set pm to jira" | `project_management.provider` | Update |
| "set snowflake account to X" | `database.connection_info` | Update |
| "set database access to cli" | `database.access` | Update |

## Apply the Change

Read the current `.tentaqles.yaml`, modify the relevant field(s), write it back. Use Python with PyYAML to preserve structure:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
import os, yaml
from tentaqles.manifest.loader import find_manifest

manifest_path = find_manifest(os.getcwd())
with open(manifest_path, 'r') as f:
    data = yaml.safe_load(f)

# Apply the change — examples:
# data['git']['email'] = 'new@email.com'
# data['cloud']['provider'] = 'aws'
# data['stack'].append('fastapi')

with open(manifest_path, 'w') as f:
    yaml.dump(data, f, default_flow_style=False, sort_keys=False, allow_unicode=True)

print('Updated: {field} = {value}')
"
```

### Auto-update dependent fields

When certain fields change, related fields should update too:

**Cloud provider changes:**
| New provider | Set preflight to | Set blocked_commands to |
|-------------|-----------------|----------------------|
| azure | `az account show --query name -o tsv` | `["az group delete", "az keyvault delete", "az storage delete"]` |
| aws | `aws sts get-caller-identity --query Account --output text` | `["aws iam delete", "aws s3 rb"]` |
| digitalocean | `doctl account get --format Email --no-header` | `[]` |
| none | remove preflight | `[]` |

**Git provider changes:**
| New provider | Set host to | Set preflight to |
|-------------|------------|-----------------|
| github | `github` | `gh auth status` |
| gitlab | `gitlab` | `glab auth status` |
| azure-devops | `azure-devops` | (remove — use email check only) |

**Database provider changes:**
| New provider | Set dialect to |
|-------------|---------------|
| postgresql | `postgresql` |
| snowflake | `snowflake` |
| databricks | `spark-sql` |
| supabase | `postgresql` |

Ask the user to confirm the auto-updated fields: "I also updated the preflight command and blocked commands for AWS. Look right?"

### Also update the identity rule

If git email or provider changed, update `.claude/rules/identity.md` to match:

```bash
# Read, update, write the identity rule file
```

## After Any Change

Run preflight checks to verify the new settings work:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest, run_preflight_checks, format_context_summary, get_client_context
manifest = load_manifest(os.getcwd())
ctx = get_client_context(os.getcwd())
checks = run_preflight_checks(manifest or ctx)
print(format_context_summary(ctx, checks))
"
```

Report what changed and whether the new configuration passes preflight checks.

## Batch Mode

If the user provides multiple changes at once ("set cloud to aws, database to snowflake, email to x@y.com"), apply them all in one write to avoid multiple file rewrites.

## Error Handling

- If YAML write fails: show the user what the new content should be so they can paste it manually.
- If a field path doesn't exist yet in the manifest (e.g., `cloud` section is missing): create it.
- If the user provides an unrecognized field: ask what manifest section it belongs to, or offer to add it as a custom field under a `custom:` key.
