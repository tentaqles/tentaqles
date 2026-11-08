# The Claude Code hook

For anyone who runs Claude Code in a client's folder and wants to know exactly what the tentaqles plugin checks,
what it blocks, what it prints, and what it lets through.

The tentaqles plugin (`plugin/`) registers two hooks in a client's Claude Code config. `tentaqles reach` installs it
for a new client. Both hooks run `plugin/scripts/tq_hook.sh`, which finds the `tq` binary and runs
`tentaqles claude-hook <event>` with the hook's JSON on stdin. The script looks for `tq` on PATH, then in
`~/.tentaqles/bin`, `~/.local/bin` and, on Windows, `%LOCALAPPDATA%\tentaqles\bin`. No environment variable and
nothing in the plugin folder picks the binary.

| Hook | Event | Matcher | Timeout |
|---|---|---|---|
| `tq_hook.sh session-start` | SessionStart | every session | 15 s |
| `tq_hook.sh pre-tool-use` | PreToolUse | `^(Bash\|PowerShell)$` | 15 s |

## `session-start`

Prints a preamble that Claude Code adds to the session's context. For a folder of a client named acme whose language
is Portuguese (Brazil):

```text
Client: acme
Language: Portuguese (Brazil) (pt-BR). Write every report, document, email and message for this client's people (clients and teammates) in Portuguese (Brazil).
Git: github as acme-dev (dev@example.com)
Cloud: azure, aws
Identity: acme · CLAUDE_CONFIG_DIR=/home/me/.tentaqles/identities/acme/claude
Tools: aws, az, claude, gh. Other providers' CLIs are not on this terminal's PATH.

tentaqles doctor:
- [ok] all checks passed

Rules: tq blocks git, gh and cloud commands that would run under another identity (exit 2). Run `tentaqles doctor` for details.
```

| Line | From |
|---|---|
| `Client` | the manifest's `client` |
| `Language` | the manifest's `language` (English (United States) when it names none), with the instruction to write in it. The client's Claude `settings.json` also carries it as Claude Code's `language` setting (see [the manifest](manifest.md)) |
| `Git` | `git.provider`, `git.user` and `git.email`; left out when all three are empty |
| `Cloud` | `cloud.provider` then `cloud.also`; left out when there is no cloud |
| `Identity` | the client, and the `CLAUDE_CONFIG_DIR` its sessions must run with |
| `Tools` | the client's scope: the providers whose CLIs its terminal keeps (see [providers](providers.md)) |
| `tentaqles doctor` | the findings for this folder: `env-drift`, `hook-missing`, `git-email-drift`, `claude-config-drift`, each as `- [level] code: message (→ fix)`; `- [ok] all checks passed` when there are none. See [troubleshooting](../troubleshooting.md) |
| `Rules` | one line on what the PreToolUse hook blocks |

In a folder that belongs to no client it prints:

```text
Client: none (neutral folder: outside any base)

Rules: remote git, gh and cloud CLI commands are blocked here; cd into a client folder first.
```

It never fails: a session always starts. When tq cannot resolve the folder (a payload over 1 MiB, or a registry it
cannot read) it prints `Tentaqles: tq could not resolve this folder (<reason>)`. When tq is not installed, the script
prints that every shell command will be blocked and where to install it. Both exit 0.

## `pre-tool-use`

Runs before each `Bash` and `PowerShell` tool call; no other tool is checked. tq reads the command's text, resolves
the call's folder (`cwd` from the payload) to its client, gathers what it needs (the folder's doctor findings, the
effective `git config user.email` there and, for a `gh` command only, the active `gh` user), and applies the rules
below in order. The first that matches denies.

### Rules

A command "starts with" a word when the word comes at the start of the command line or right after a separator:
`&&`, `||`, `;`, `|`, `&`, a line break, `$(`, a backtick, `(`, `)`, `{` or `}`. So `cd app && gh pr create` and
`(az vm list)` count as starting with `gh` and `az`.

| Rule | Denies | Message (after `BLOCKED [identity/<rule>]: `) |
|---|---|---|
| `blocked-command` | a command that starts with an entry of the manifest's `blocked_commands`, `git.blocked_commands` or `cloud.blocked_commands` (an entry's trailing space is ignored, and it must match as whole words) | `Command '<entry>' is blocked by client manifest.` and the client |
| `wrong-cloud` | a command that starts with a cloud CLI (`az`, `aws`, `gcloud`, `gsutil`, `bq`, `doctl`) of a cloud other than `cloud.provider` and `cloud.also`. Only when `cloud.provider` is set and not `none`: a client with no cloud gets no `wrong-cloud` deny | `This workspace uses <clouds>, but you ran a <cloud> command ('<cli>').` |
| `neutral-remote` | in a folder that belongs to no client: a remote git command (`push`, `fetch`, `pull`, `clone`), or a command that starts with `gh` or a cloud CLI. Local commands pass | `this folder belongs to no client (<why>); refusing a remote/cloud command with no identity.` and the fix, `tentaqles reach` |
| `env-drift` | a git command that is not read-only, when the shell's `TQ_WS` is not the folder's client (a stale shell) | `shell identity (TQ_WS) does not match cwd; ...` |
| `claude-config-drift` | a remote command (a remote git command, `gh`, a cloud CLI) when the session's `CLAUDE_CONFIG_DIR` is not the folder's client's, or is unset while `TQ_WS` names the client | `this Claude session is not running under the <client> identity dir (CLAUDE_CONFIG_DIR drift).` and the fix, `tentaqles run <client> -- claude` |
| `git-email-drift` | when the manifest sets `git.email`: a git command with an inline identity override (`-c user.email=...`, `-c user.name=...`, `--config-env=user.email=...` or `--config-env=user.name=...`), read-only or not | `inline git identity override (-c user.email/--config-env) is not allowed; identity is managed by tq` |
| `git-email-drift` | when the manifest sets `git.email`: a git command that is not read-only, when the effective `user.email` in the folder differs from it (case ignored) | `Git email mismatch.` with the expected and actual email |
| `gh-user` | a command that starts with `gh`, when the active `gh` user differs from the client's (case ignored). The client's user is `git.user`, or, on a GitHub client (`git.provider` `github` or empty), `git.expected_user` when `git.user` is empty. The active user is looked up with `gh api user` under the client's `GH_CONFIG_DIR`, with a 4-second limit | `GitHub user mismatch.` with both users and the fix, `tentaqles login <client> gh` |

Read-only git is `status`, `log`, `diff`, `show`, `rev-parse`, `ls-files`, `branch` (listing, not `-d`, `-m`,
`--set-upstream-to` and the like) and `remote` (`-v`, `show`, `get-url`). A chain of git calls is read-only only when
every call in it is. Read-only git is never denied for a stale shell or a different email; an inline identity override
is denied even on it.

`env-drift` and `git-email-drift` look at a command that starts with `git`. A remote git command counts wherever it
is: inside a quoted string, or inside `sh -c`, `bash -c` (and `zsh`, `dash`, `ksh`, `fish`), `pwsh -Command`,
`powershell -Command`, `cmd /c` and `iex` (`Invoke-Expression`), read up to three levels deep. A call nested deeper
than that is taken for a commit and a push.

### Answers

- **Allow:** exit 0, nothing printed.
- **Deny:** exit 2, the reason on stderr, which Claude Code shows to the model. The reason names what to do.
- `tentaqles claude-hook pre-tool-use --json` also prints `{"block": ..., "rule": ..., "reason": ...}` on stdout, for
  an allow too.

### Fail closed

A call tq cannot judge is denied as `BLOCKED [tq/guard-error]: <why>; tq denies a call it cannot judge. ...`, exit 2:

- the payload cannot be read: stdin fails, it is over 1 MiB, it is not valid UTF-8, or it is not valid JSON;
- tq cannot read its registry of clients folders (`~/.tentaqles/config.yaml`);
- the folder holds a client marker whose manifest cannot be read or is not valid;
- the decision panics;
- the decision takes longer than 6 seconds;
- the tq process is still running 7 seconds after it started (its own watchdog), or the command fails before it
  decides (an unknown flag, say);
- the script sees tq exit with anything other than 0 or 2, or stops it: `tq_hook.sh` stops tq at 7 seconds and kills
  it at 8;
- tq is not installed or cannot run: the script denies every shell command and says where to install it.

Every deadline is under the plugin's 15-second hook timeout, because Claude Code runs a call whose hook it killed.

### What it lets through

The hook reads text. It catches mistakes, not intent:

- a command spelled to hide what it runs: a variable, an alias, a function, a script file;
- `gh` or a cloud CLI inside `sh -c`, `pwsh -Command`, `cmd /c` or `iex` (only git is read inside them);
- a `gh` command when the `gh` user lookup fails or times out: the call is allowed;
- any tool other than Bash and PowerShell: file edits, MCP tools, web fetches.

See the [threat model](../threat-model.md).

## The plugin is required

`tentaqles run <client> -- claude` refuses to start a session, with the reason and the lines that fix it, when:

- the client has no `claude` identity (no Claude config to guard);
- the client's `settings.json` cannot be read or is not JSON;
- `disableAllHooks` is set in it;
- the tentaqles plugin is not installed for the client;
- it is installed but not enabled (`enabledPlugins` in the client's `settings.json`);
- the plugin's `hooks/hooks.json` cannot be read, or has no PreToolUse hook that runs `tq_hook.sh pre-tool-use` for
  both Bash and PowerShell (the fix is a fresh install);
- a project settings file sets `disableAllHooks`, or sets `tentaqles@tentaqles` to anything but `true` under
  `enabledPlugins`, or cannot be read as JSON. tq reads `.claude/settings.json` and `.claude/settings.local.json` in
  the folder the session starts in and in every folder above it up to the client's folder, as Claude Code does,
  following links and junctions;
- the folder the session starts in cannot be resolved.

Only a session is checked: `claude` subcommands (`claude plugin install ...`), `--help` and `--version` run without
it, so the fix lines themselves can run.

`tentaqles doctor` reports the same state as `guard-hook-missing`, with two differences: it checks the project
settings of the client's folder only (not of its subfolders), and it skips a client with no `claude` identity. See
`tentaqles doctor` in [commands](commands.md).
