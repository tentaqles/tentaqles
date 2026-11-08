# Tentaqles plugin

The Claude Code plugin of [Tentaqles](https://github.com/tentaqles/tentaqles). It holds one thing: the hook,
`scripts/tq_hook.sh`, which runs `tentaqles claude-hook`:

- at session start, to give the session the folder's client: its language, with the instruction to write the
  client's reports, documents, emails and messages in it; its git and cloud identity; its Claude config home; the
  tools its terminal keeps; the findings of `tentaqles doctor` for the folder; and what the hook blocks;
- before each Bash or PowerShell command, to block a command that would run under another client's identity (the
  manifest's blocked commands, the wrong cloud, git email or gh user, a stale shell, a remote command outside every
  client folder), with the reason.

It denies a shell command when tq is missing, fails or hangs, because Claude Code runs a call whose hook did not
answer. `tentaqles reach` installs the plugin into each client's Claude config; install the `tentaqles` CLI first.
The rules are in [docs/reference/claude-hook.md](../docs/reference/claude-hook.md).
