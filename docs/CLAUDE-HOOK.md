# The Claude Code hook

The tentaqles plugin (`plugin/`) registers two hooks in a client's Claude Code config. Both run
`plugin/scripts/tq_hook.sh`, which finds the `tq` binary (on PATH, then in the install folders; never from an
environment variable) and runs `tentaqles claude-hook <event>` with the hook's JSON on stdin.

## `session-start`

Prints a short preamble that Claude Code adds to the session: the folder's client; its language, with the
instruction to write every report, document, email and message for the client's people in it; its git provider, user
and email; its clouds, the identity and its `CLAUDE_CONFIG_DIR`, the client's tools, the findings of `tentaqles doctor` for
this folder, and one line on what the hook blocks. In a neutral folder it says so. It never fails: a session always
starts.

## `pre-tool-use`

Runs before each `Bash` and `PowerShell` tool call (the plugin's matcher is `^(Bash|PowerShell)$`). Any other tool
is not checked. tq reads the command's text, resolves the call's folder to its client, and applies these rules in
order; the first that matches denies:

| Rule | Denies |
|---|---|
| `blocked-command` | a command that starts with one of the client's `blocked_commands`, `git.blocked_commands` or `cloud.blocked_commands` |
| `wrong-cloud` | a cloud CLI (`az`, `aws`, `gcloud`, `gsutil`, `bq`, `doctl`) of a cloud other than the client's `cloud.provider` and `cloud.also` |
| `neutral-remote` | outside every client folder: a remote git command (`push`, `fetch`, `pull`, `clone`), `gh`, or a cloud CLI. Local commands pass |
| `env-drift` | a git command that writes, when the shell's `TQ_WS` is not the folder's client (a stale shell) |
| `claude-config-drift` | a remote git command, `gh` or a cloud CLI, when `CLAUDE_CONFIG_DIR` is not the folder's client's |
| `git-email-drift` | an inline identity override (`git -c user.email=...`, `--config-env`), or a git command that writes when the effective `user.email` differs from the client's |
| `gh-user` | a `gh` command when the active `gh` user differs from the client's `git.user` (looked up with a 4-second limit) |

Read-only git (`status`, `log`, `diff`, `show` and the like) is never denied for drift.

A deny exits 2 and prints `BLOCKED [identity/<rule>]: <reason>` on stderr, which Claude Code shows to the model; the
reason names what to do. `--json` prints `{"block","rule","reason"}` on stdout as well.

The check fails closed: a payload tq cannot read, a client marker without a readable manifest, a panic, or a
decision slower than 6 seconds is denied as `BLOCKED [tq/guard-error]`. The process stops itself at 7 seconds, and
`tq_hook.sh` stops it at 7 and kills it at 8, all under the plugin's 15-second hook timeout, because Claude Code
runs a call whose hook it killed. When tq is missing or cannot run, the script denies every shell command and says
how to install it.

The hook reads text: a command spelled to hide what it runs (a variable, an alias, `sh -c`, a script file) gets
past it. It catches mistakes, not intent.

## The plugin is required

`tentaqles run <client> -- claude` refuses to start a session when the client's Claude config has no enabled tentaqles
plugin whose PreToolUse hook runs `tq_hook.sh pre-tool-use` for Bash and PowerShell, or when `disableAllHooks` is
set in the client's settings or in the folder's `.claude/settings.json`. `tentaqles doctor` reports the same state as
`guard-hook-missing`. `tentaqles reach` installs the plugin for a new client.
