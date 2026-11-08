# Tentaqles plugin

The Claude Code plugin of [Tentaqles](https://github.com/tentaqles/tentaqles). It holds one thing: the hook,
`scripts/tq_hook.sh`, which runs `tentaqles claude-hook`:

- at session start, to state the folder's client, its git and cloud identity and its Claude config home;
- before each Bash or PowerShell command, to block a `git`, `gh` or cloud command that would run under another
  client's identity, with the reason.

It denies a shell command when tq is missing, fails or hangs, because Claude Code runs a call whose hook did not
answer. `tentaqles reach` installs the plugin into each client's Claude config; install the `tentaqles` CLI first.
