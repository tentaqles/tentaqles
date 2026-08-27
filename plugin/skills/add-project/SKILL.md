---
name: add-project
description: Create a new project inside the current client workspace. Scaffolds CLAUDE.md, brief.md, and git config inherited from the client manifest. Use when the user says "new project", "start a project", "create a project folder", or mentions starting work on a new feature/service/app within an existing client. Also triggers when the user shares an Asana/Jira/GitHub issue URL and wants to start working on it.
---

# Add Project

Create a new project folder inside the current client workspace with inherited context from `.tentaqles.yaml`. Projects are subfolders of clients — they don't need their own manifest.

## Detect Client Workspace

First, find the parent client:

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest
manifest = load_manifest(os.getcwd())
if manifest:
    print(f'client={manifest[\"client\"]}')
    print(f'root={manifest[\"_client_root\"]}')
    print(f'display={manifest.get(\"display_name\", manifest[\"client\"])}')
    print(f'git_email={manifest.get(\"git\", {}).get(\"email\", \"\")}')
    print(f'git_name={manifest.get(\"display_name\", \"Developer\")}')
    print(f'language={manifest.get(\"language\", \"en\")}')
else:
    print('NO_CLIENT')
"
```

If `NO_CLIENT` is returned, tell the user: "You're not inside a client workspace. Run `/tentaqles:add-client` first to set one up, then cd into it."

## Gather Project Info

Ask for what's needed. Project name is required; the rest is optional and can be filled in later.

| Field | Required | Example |
|-------|----------|---------|
| Project name | Yes | "inventory-api" |
| Description/goal | No | "REST API for inventory management" |
| Tech stack | No | python, fastapi, postgresql |
| External ticket URL | No | Asana task, Jira ticket, GitHub issue URL |

If the user provides a ticket URL, fetch context from it before creating files.

## Fetch External Context (if URL provided)

Depending on the URL type:

**GitHub Issue:**
```bash
gh issue view {number} --repo {owner/repo} --json title,body,labels,assignees,milestone
```

**Asana/Jira/Other URL:**
Use WebFetch to grab the page content and extract: title, description, acceptance criteria, assignees, due dates, subtasks.

Parse out structured fields. If extraction fails, note that the URL was provided and the user can add details manually.

## Create the Project

### 1. Create directory

```bash
mkdir -p "{client_root}/{project-slug}"
```

### 2. Create CLAUDE.md

Write `{client_root}/{project-slug}/CLAUDE.md` with inherited client context:

```markdown
# {Project Name}

## Overview
{description from user or ticket, or placeholder}

## Tech Stack
{user-provided stack, or "TBD — update as the project takes shape"}

## Client Context
- Client: {client display_name}
- Cloud: {cloud provider from manifest}
- Database: {database provider + dialect from manifest}
- Git: {git provider} ({git email})
- Language: {language}

## Development
<!-- Add as you go: -->
<!-- - Build: ... -->
<!-- - Test: ... -->
<!-- - Deploy: ... -->
```

### 3. Create brief.md (if there's enough context)

Only create this if the user provided a description, goal, or ticket. Don't create an empty brief — it's just noise.

```markdown
---
project: {name}
status: active
created: {YYYY-MM-DD}
source: {ticket URL if any}
---

# {Project Name}

## Goal
{from user input or extracted from ticket}

## Deliverables
{from user or ticket subtasks}
- [ ] {deliverable 1}
- [ ] {deliverable 2}

## Acceptance Criteria
{from ticket or user}

## Notes
{any additional context from ticket — assignees, due date, labels}
```

### 4. Initialize git (if needed)

Check if the project or client root is already a git repo:

```bash
git rev-parse --git-dir 2>/dev/null
```

If not a git repo, ask the user: "Initialize a git repo for this project?" If yes:

```bash
cd "{client_root}/{project-slug}"
git init
git config user.email "{git_email_from_manifest}"
git config user.name "{display_name_from_manifest}"
```

If the client root IS a git repo (mono-repo pattern), skip init — the project inherits the parent repo's config.

### 5. Record the touch in memory

```bash
# Load tentaqles runtime
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true

echo '{"cwd": "{client_root}", "event": "touch", "data": {"node_id": "{project-slug}", "node_type": "module", "action": "create", "weight": 2.0}}' | "$TENTAQLES_PY" "${CLAUDE_PLUGIN_ROOT}/scripts/memory-bridge.py" 2>/dev/null || true
```

### 6. Report

Tell the user:
- What was created (project dir, CLAUDE.md, brief.md if applicable)
- The inherited client context (cloud, database, git — so they know it's connected)
- Git configuration (email set to match client manifest)
- Next steps: "You can start coding, or run `/tentaqles:build-graph` after you have some files to analyze."

## Error Handling

- If the project directory already exists: warn and ask before overwriting any files.
- If WebFetch fails on a ticket URL: note the failure, continue creating the project without ticket context.
- If git config fails: note it and move on — the identity guard hook will catch mismatches later anyway.
- If memory bridge fails: ignore silently — memory is optional.
