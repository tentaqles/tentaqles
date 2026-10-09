---
name: guard
description: See, switch off, switch on, or change (ask vs deny) tq's guard rules, for one workspace or for every workspace. Use when the user says "turn off the cloud delete check", "stop asking me about force push", "make X a hard block", "why was this blocked", "which guards are on", "disable that rule for this client", "enable it again", or names a rule id like tq/cloud-delete or jev/destructive-sql.
---

# Guard settings

Change which of tq's guard rules apply, at the user's request. Run every
command with the Bash tool. Never edit `~/.tentaqles/guard.yaml` or a
`.tentaqles.yaml` guard block by hand: tq refuses direct edits to the
global file, and a hand-edited manifest stops applying until it is
re-trusted.

## 1. Show the current state

```bash
tq guard list                 # the workspace of the current folder
tq guard list --ws <name>     # another workspace
```

Each row shows the rule id, its kind (builtin, hook, jev, global,
manifest), its default action, what it does now (`ask`, `deny` or `off`),
and what changed it (`global`, or the workspace name).

If the user described a rule in words ("the cloud delete one"), find the
matching id in this list and say which one you will change.

## 2. Pick the scope with the user

- **This workspace only** (`--ws <name>`, or no flag inside the workspace's
  folder): edits that workspace's manifest and re-trusts it. Wins over the
  global setting.
- **Every workspace** (`--all`): writes `~/.tentaqles/guard.yaml`.

If the user did not say, ask which scope they want. Default to the
narrower one (this workspace) when they only care about the current task.

## 3. Make the change

```bash
tq guard off <rule-id>... [--ws <name> | --all]
tq guard on  <rule-id>... [--ws <name> | --all]
tq guard set <rule-id> ask|deny|default [--ws <name> | --all]
```

- `off` / `on` switch a rule off or back on.
- `set` changes ask ↔ deny. `default` removes the override.

These commands ask the user to confirm (rule `tq/guard-change`). That is
expected: it is how the guard makes sure an agent cannot turn a check off
on its own. Never try to get around the prompt.

## 4. Report

Show the `before -> after` lines tq prints and the scope. Mention that
open sessions pick the change up on their next tool call.

Warn the user before switching off a rule that protects secrets
(`tq/secret-store-*`, `tq/env-*`, `tq/commit-secret`) or the guard's own
settings (`tq/guard-change`, `tq/guard-file-write`). Do it only if they
confirm after the warning.

## Notes

- A repo's `.claude/tq-rules.yaml` can only add rules. Nothing in a cloned
  repo can switch a rule off, and this skill should never suggest it.
- Jev rules (`jev/...`) can be switched off the same way. `set jev/... deny`
  lets that rule block outright when the workspace's Jev mode is `enforce`;
  the default is ask.
