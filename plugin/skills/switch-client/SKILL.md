---
name: switch-client
description: Show all registered client workspaces with their identity status and help switch context safely. Use when the user says "switch client", "change workspace", "go to client X", "show my clients", "which client am I in", "list workspaces", or mentions needing to work on a different client. Also triggers when the user asks about git identity, cloud subscription status, or says something like "am I logged into the right account".
---

# Switch Client

Show all registered workspaces with their identity status, and help the
user switch safely. `tq` owns identity switching — this skill's job is to
surface `tq`'s own view (`tq list`, `tq doctor`) and tell the user how to
move between workspaces.

The core safety principle: never let the user accidentally run commands
against the wrong client's infrastructure. `tq claude-hook pre-tool-use`
(wired into the `PreToolUse` hook) already blocks that at the Bash-command
level; this skill is about visibility and voluntary switching.

## List Workspaces

```bash
tq list --json
```

Render the JSON as a table:

| Client | Base | Trusted | Git Identity |
|--------|------|---------|-------------|
| acme-corp | C:\repos | yes | dev@acme.com |
| globex | C:\repos | no | dev@globex.io |

If the list is empty: "No client workspaces registered yet. Run
`/tentaqles:add-client` to set up your first one."

Highlight the **current workspace** (detected from cwd) if the user is
inside one.

## Switching

`tq` does not have a `cd`-replacing shell command in this release — actual
identity switching happens via the shell activation hook (`tq activate`,
installed by `tq hooks install`) that fires on every `cd`. So switching is:

1. **cd into the folder.** The `tq` shell hook (from `tq activate`) detects
   the directory change and switches the exported identity env vars
   (`CLAUDE_CONFIG_DIR`, git identity, etc.) automatically. On `cmd.exe`
   (no hook support), use `tq run <workspace> -- <command>` to run a single
   command under that workspace's identity instead.
2. **Verify with `tq doctor`** after cd'ing, to confirm everything lines up.

## Run Preflight / Status Checks

For the target client:

```bash
tq doctor
```

Or, for scripting / structured output:

```bash
tq doctor --json
```

`tq doctor` never mutates — it reports whether hooks are installed, the
workspace is trusted, git identity matches the manifest, and env vars are
consistent with cwd.

## Provide Fix Commands

For common `tq doctor` findings:

| Finding | Fix |
|---------|-----|
| Workspace untrusted | `tq allow <name>` (ask the user before running this — see add-client's rule: never auto-trust) |
| Git identity drift | `tq doctor` explains the mismatch; identity is managed by `tq`, don't hand-edit `git config user.email` |
| Env drift (TQ_WS stale) | Open a new shell, or `eval "$(tq env --shell <shell>)"` |
| Claude config dir drift | Start Claude from a tq-activated shell in the workspace, or `tq run <name> -- claude` |

Present these clearly and ask before running anything that changes trust
state.

## Report

If `tq doctor` is clean: "You're set up for **{client name}**. All checks
passed." If issues remain, list them with the fix commands above and note
which ones require manual login (e.g., `gh auth login`, `az login` — `tq`
can't do interactive auth for you).

## Error Handling

- If a workspace's folder no longer exists on disk, `tq list`/`tq doctor` will flag it — tell the user and suggest they remove or recreate it.
- If `tq` is not installed or not on PATH: fall back to running `git config user.email`, `gh auth status`, etc. directly and comparing manually, and tell the user identity enforcement is running in fallback mode until `tq` is installed.
