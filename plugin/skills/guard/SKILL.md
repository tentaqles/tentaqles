---
name: guard
description: See or explain tq's guard mode (default, relaxed, strict, custom) and its rules, and change single rules (switch off, switch on, ask vs deny) for one workspace or for every workspace. Use when the user says "which mode is this workspace in", "make this demo folder relaxed", "make acme strict", "stop asking me so much here", "turn off the cloud delete check", "stop asking me about force push", "make X a hard block", "why was this blocked", "which guards are on", "disable that rule for this client", "enable it again", "run claude without tq", or names a rule id like tq/cloud-delete or jev/destructive-sql. Mode changes are human-only, so this skill shows the state and hands the user the exact command to run.
---

# Guard modes and rules

Show and explain tq's guard at the user's request, and change single rules
when they ask. Run every command with the Bash tool. Never edit
`~/.tentaqles/guard.yaml`, a `.tentaqles.yaml` guard block, the trust store or
`.claude/tq-rules.yaml` by hand: tq denies direct edits to the global file and
the trust store, and a hand-edited manifest stops applying until it is
re-trusted.

tq has two kinds of settings:

- the **guard mode** of a workspace, a project folder or every workspace:
  `default`, `relaxed`, `strict` or `custom`. It decides what tq does with each
  *category* of rules. Changing it is **human-only**: you never run it, you
  give the user the command.
- **single rules** (`tq guard off|on|set`): you may run these, and the user
  confirms each one.

## 1. Show the current state

```bash
tq mode                       # the mode for the current folder, read-only
tq mode --json                # the same, machine-readable
tq mode --ws <name>           # another workspace
tq mode log --since 24h       # what the mode skipped or lowered
tq guard list [--ws <name>]   # every rule, its action and what changed it
```

`tq mode` prints the mode name (`relaxed`, `custom (relaxed + 2)`), each
category's value (`skip`, `ask`, `deny` or `rule` = the rule's own action) and
where it came from, the `rules_override` entries, the ceiling (the grant, or
`default`), and anything clamped or ignored. The SessionStart line
(`Guard: …`) says the same in one line.

If the user described a rule in words ("the cloud delete one"), find the id
in `tq guard list` and say which one you mean.

## 2. Explain the modes

| Mode | What it does |
|---|---|
| `default` | every rule at its own action (what applies when nothing is set) |
| `relaxed` | skips every toggleable category; `git-history` (force push to main, repo delete) and `system` (writes outside the workspace or to system folders, elevation, registry) still ask. Needs a one-time grant |
| `strict` | default, plus `prod-changes`, `cloud-destructive` and `git-history` deny, and `external-writes` asks. No grant needed |
| `custom` | a base preset plus per-category values; a looser table needs a grant for that table |

Four categories are **locked** and hold in every mode: `identity`,
`secrets`, `self-protection` and `privacy` (the PII exit rules). Nothing
toggles them, and `tq guard off` of one of their rules is ignored.

`off` is not a mode you set: it means no tq at all for one launch
(`claude-off`, `codex-off`) or one window (`tq off`, back with `tq on`).
Main logins, no guard. Tell the user it is for tq-free work, not client work.

## 3. Changing the mode: hand the user the command

When the user asks for a different mode, pick the scope with them (this
workspace, one project folder, or every workspace) and print the command for
them to run in their own terminal:

```
tq mode set relaxed --ws <name>              # the workspace
tq mode set relaxed --project <dir>          # one folder inside it
tq mode set strict --global                  # every workspace
tq mode toggle db-destructive ask --ws <name>
tq mode override tq/iac-apply ask --ws <name>
tq mode edit --ws <name>                     # an interactive form
tq allow --mode <name> relaxed               # the grant, if they said no to the prompt
```

Never run these yourself, and never try another way in (editing the
manifest, `TQ_MODE=…`, `--tq-mode relaxed`, the trust store, a nested
`claude` or `codex` with loosened flags). They are human-only: the binary
refuses agent sessions and needs a terminal, and the hook denies them in a
tool call (`tq/mode-change`, `tq/nested-agent-loosened`). That is how the
guard makes sure an agent cannot loosen it. A mode looser than `default` also
needs the user's grant; without it the session runs at `default`.

Open sessions keep the mode they started with when it came from a launch
flag; a changed manifest applies on the next tool call.

## 4. Single rules: `tq guard`

Pick the scope with the user:

- **This workspace only** (`--ws <name>`, or no flag inside the workspace's
  folder): edits that workspace's manifest and re-trusts it. Wins over the
  global setting.
- **Every workspace** (`--all`): writes `~/.tentaqles/guard.yaml`.

If the user did not say, ask. Default to the narrower one when they only
care about the current task.

```bash
tq guard off <rule-id>... [--ws <name> | --all]
tq guard on  <rule-id>... [--ws <name> | --all]
tq guard set <rule-id> ask|deny|default [--ws <name> | --all]
```

- `off` / `on` switch a rule off or back on.
- `set` changes ask ↔ deny. `default` removes the override.

These commands ask the user to confirm (rule `tq/guard-change`). That is
expected. Never try to get around the prompt.

A locked rule (`tq/secret-store-*`, `tq/env-*`, `tq/commit-secret`,
`tq/*-pii`, `tq/guard-*`, `tq/settings-*`, `tq/trust-store-write`,
`tq/mode-change`, the identity guard) cannot be switched off or loosened:
tq ignores the entry and `tq doctor` warns `guard-locked-override`. Tell the
user so instead of running the command.

## 5. Report

Show the `before -> after` lines tq prints and the scope, or the command you
handed the user. Mention that open sessions pick a rule change up on their
next tool call.

## Notes

- A repo's `.claude/tq-rules.yaml` can only add rules and tighten the mode.
  Nothing in a cloned repo can loosen the guard, and this skill should never
  suggest it.
- Jev rules (`jev/...`) can be switched off the same way unless they are
  locked (`jev/service-role-client` and `jev/n8n-inline-credentials` are in
  `secrets`). `set jev/... deny` lets that rule block outright when the
  workspace's Jev mode is `enforce`; the default is ask.
- In Codex, a confirm that Codex cannot prompt for becomes a block.
  `tq mode --agent codex` lists those rules.
