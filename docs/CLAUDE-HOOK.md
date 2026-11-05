# `tq claude-hook` — Claude Code hook adapter

`tq claude-hook` is what the Claude Code plugin's `SessionStart` and
`PreToolUse` hooks actually call (via `plugin/scripts/tq_hook.sh`). It is
the source of truth for identity reporting and enforcement; the plugin's
Python code never switches gh/az/git identity itself.

tq guards three categories and nothing else (PRD v2 §7.1): **identity** (the
identity a command runs under, the manifest's blocked commands),
**secrets**, and **self-protection** (tq's own state, hooks, plugin and
binary, and the human-only tq commands; ADR-0005). It has no modes and no
other rules; every decision is allow or block, in every Claude permission
mode.

## Subcommands

- `tq claude-hook session-start` — prints a short identity/status preamble. Never blocks (always exits 0).
- `tq claude-hook pre-tool-use` — decides whether to allow or block a tool call: shell commands (Bash, PowerShell), file reads and edits, and MCP tool calls. Exits 0 (allow) or 2 (block). `--agent codex` answers in Codex's format (see `CLAUDE-SETTINGS.md`, "Codex").

Both subcommands read a single JSON payload from stdin — the same shape
Claude Code sends its own hooks.

## Protocol

### Input (stdin)

Both subcommands accept the hook JSON Claude Code provides. The fields
`tq` reads:

```json
{
  "tool_name": "Bash",
  "cwd": "C:\\repos\\acme\\billing-api",
  "tool_input": {
    "command": "git push origin main"
  }
}
```

- `tool_name` — `Bash` and `PowerShell` go through the identity guard, the
  secret rules and the self-protection rules; every other tool (Read, Edit,
  Write, MultiEdit, NotebookEdit, Grep, `mcp__*`) goes through the secret
  and self-protection rules. A payload without `tool_name` is allowed.
- `cwd` — the directory to resolve a workspace from. Falls back to the
  process's actual working directory if omitted or empty.
- `tool_input` — `command` for shell tools; `file_path` / `notebook_path` /
  `path` plus `content` / `new_string` / `edits[].new_string` /
  `new_source` for file tools; the whole object (as JSON text) for MCP
  tools. Ignored by `session-start`.

Any other fields in the payload (session id, etc.) are ignored.

The payload is capped at 1 MiB. A larger payload is a protocol violation,
not a parse error: `pre-tool-use` prints
`BLOCKED: hook payload exceeds 1 MiB` to stderr and exits 2 (fail closed),
while `session-start` prints its "could not resolve" line and exits 0.

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Allow. `session-start` always exits 0 (it never blocks a session). |
| `2` | Block (`pre-tool-use` only). A `BLOCKED [<category>/<rule>]: ...` message is written to **stderr** (`BLOCKED [identity/wrong-cloud]: ...`, `BLOCKED [secrets/tq/env-file-read]: ...`, `BLOCKED [self-protection/tq/protected-path]: ...`); stdout is not used for the block message. Every block is also one line in the denial log (see [Self-protection](#self-protection-pre-tool-use)). |

If the guard itself fails while it analyses a call (a panic in tq's own
code), `pre-tool-use` fails safe: it asks, printing the PreToolUse decision
JSON to stdout with the reason
`CONFIRM [tq/guard-error]: tq guard error; please review the command yourself (…)`,
and writes `tq: guard panic: …` to stderr. It never allows the call and
never crashes (a Go crash exits 2 with a stack trace, which Claude Code
reads as a block). A payload that names no tool for Codex, or a file edit
tq cannot judge, is denied under the same id. This is the only ask tq ever
prints: every other decision is allow or block.

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse",
  "permissionDecision": "ask",
  "permissionDecisionReason": "CONFIRM [tq/guard-error]: tq guard error; please review the command yourself (…)"}}
```

`session-start` writes its preamble to **stdout** and always returns 0,
even when it can't resolve a workspace — a hook that fails a session start
would be worse than a hook that reports "unknown".

### `--json`

`tq doctor --json`, `tq list --json`, and `tq bundle diff --json` all
support machine-readable output for skills/scripts that want to parse
results instead of scraping text.

`tq claude-hook pre-tool-use --json` also supports it: with the flag, the
decision is written to **stdout** as one JSON object, in addition to the
usual stderr message and exit code.

```json
{"block": true, "category": "identity", "rule": "git-email-drift", "reason": "BLOCKED [identity/git-email-drift]: ..."}
```

- `block` — `true` when the command is refused (the process also exits 2).
- `category` — `identity`, `secrets` or `self-protection`; omitted when the
  call is allowed and for `tq/guard-error`.
- `rule` — the rule that matched: an identity rule (`git-email-drift`, …),
  a secret rule id (`tq/env-dump`, …) or a self-protection rule id
  (`tq/protected-path`, `tq/human-only-command`); `""` when the call is
  simply allowed. For the fail-safe ask, `block` is `false` and `rule` is
  `tq/guard-error`.
- `reason` — the full multi-line message (`""` when allowed).

With `--json`, the fail-safe ask is reported in this object instead of the
`hookSpecificOutput` JSON, so the flag is for scripts, not for the hook.

`session-start` has no `--json` flag: its output is the fixed preamble
format described below.

## Rule precedence (`pre-tool-use`)

The categories are evaluated in the order of PRD v2 §6.3 and the first one
that denies is the one reported:

1. **identity** — the manifest's `blocked_commands`, then the identity guard
   below (shell tools only), and `tq/cross-client-mcp` (MCP tools);
2. **secrets** — the secret rules, `tq/commit-secret`;
3. **self-protection** — `tq/protected-path`, `tq/human-only-command`; run
   only when nothing above denied.

A command that matches both a blocked command and a protected path reports
`blocked-command`; one that runs a wrong cloud CLI on the manifest reports
`wrong-cloud`.

Inside the identity category, `internal/guard/guard.go`'s `Decide` function
applies rules in this exact order — the **first** matching rule wins and
produces the block:

1. **`blocked-command`** — the command starts with (as a whole word, after
   any of `&& || ; | & \n \r $( \` ( { }`) an entry in the manifest's
   effective blocked-command list. Checked before anything else, even
   before cloud/identity checks.
2. **`wrong-cloud`** — the command invokes a cloud CLI (`az`, `aws`,
   `gcloud`, `gsutil`, `bq`, `doctl`) whose provider doesn't match the
   workspace's configured cloud provider. E.g. running `az ...` in an
   AWS-configured workspace. Providers listed in the trusted manifest's
   `cloud.also` (e.g. `also: [aws]`; names `azure`, `aws`, `gcp`, `digitalocean` (aliases: `az`, `gcloud`, `do`, `doctl`)) are allowed too; the block message
   then names the set ("azure + aws"). A repo manifest cannot add clouds.
3. **`neutral-remote`** — cwd resolves to **no** trusted tq workspace
   (`Neutral`), and the command is a remote mutation (`gh` anything, any
   cloud CLI, or `git push|fetch|pull|clone`). Everything else in a
   neutral cwd is allowed (there's no identity to protect for local-only
   commands).
4. **`untrusted`** — non-read-only git only. The workspace resolved but isn't trusted
   (`tq allow <name>` hasn't been run, or the manifest changed since).
5. **`env-drift`** — non-read-only git only. The shell's exported `TQ_WS` doesn't match
   cwd (stale shell — the `tq` activation hook hasn't re-run since the
   last `cd`).
6. **`claude-config-drift`** — remote mutation (`git push`/`gh`) only. The
   running Claude session isn't under the workspace's expected
   `CLAUDE_CONFIG_DIR`.
7. **`git-email-drift`** — two ways to match. (a) Any git invocation
   carrying an inline identity override (`-c user.email=...`,
   `-c user.name=...`, `--config-env=user.email=...`,
   `--config-env=user.name=...`) in a workspace that pins an email is
   blocked outright, whatever the configured email is: such a command
   supplies its own identity, so comparing the configured one proves
   nothing. (b) Otherwise: non-read-only git only, and only when both the
   expected and actual emails are known. `git status`, `log`, `diff`,
   `show`, `rev-parse`, `ls-files`, read-only `branch`/`remote` invocations
   are exempt (see **Read-only git exemption** below).
8. **`gh-user`** — `gh` only, and only when both the expected and actual
   GitHub usernames are known.

If no rule matches, the command is allowed (`Decision{}`, zero value,
`Block: false`).

### Read-only git exemption

`git-email-drift` (rule 7) does not fire for git invocations classified as
read-only by `IsReadOnlyGit`: `status`, `log`, `diff`, `show`, `rev-parse`,
`ls-files`, and read-only forms of `branch` (no `-d/-D/-m/-M/-c/-C/-f`,
`--delete/--move/--copy/--force/--set-upstream-to`, `-u`,
`--unset-upstream`, and at most one positional arg) and `remote` (no args,
or `-v`/`show`/`get-url`). A command with **multiple** chained git
invocations (`git status && git push`) is read-only only if **every**
segment is read-only — one mutating segment makes the whole command
subject to the email check. The exemption covers rules 4 (`untrusted`),
5 (`env-drift`) and 7 (`git-email-drift`): a read-only git command neither
writes history nor touches a remote, so a stale shell or an untrusted
manifest is no reason to refuse it. Rules 1-3, 6 and 8 are unaffected by
whether the git command is read-only, and so is the inline-identity-override
case of rule 7 (`git -c user.email=... status` is still blocked).

### What this guard does not catch

Segment splitting and prefix matching are string heuristics, not a shell
parser. They are trivially bypassable by anyone who wants to: `env git push`,
`command git push`, `sh -c "git push"`, `... | xargs git push`, `\git push`,
`"git" push`, a shell alias or function named `git`, a `$GIT push` variable
expansion, or homoglyph/encoding tricks all slip past. Quoting, escaping and
nesting are not modelled either.

This is deliberate. The guard is **defense-in-depth on top of tq's env
isolation**, not a sandbox: the real protection is that each workspace runs
with its own private `GH_CONFIG_DIR` / `AZURE_CONFIG_DIR` / git config, so a
command that dodges the guard still does not get another client's
credentials. The guard's job is to catch the honest mistake - the wrong cwd,
the stale shell, the forgotten `tq allow` - not to contain an adversary.

Everything else an agent can do (a force push, a cloud delete, a migration
against production, an edit to the project's `.claude/settings.local.json`)
is Claude Code's own permission settings' business, not tq's. tq's own
files, hooks, plugin and binary are the self-protection category's (below).

## `session-start` output

Sample for a trusted workspace:

```
Client: Acme (en)
Git: github.com as acme-bot (dev@acme.com)
Cloud: azure (acme-prod subscription)
Identity: acme · CLAUDE_CONFIG_DIR=/home/acme/.tentaqles/identities/acme/claude

tq doctor:
- [ok] all checks passed

Rules: tq blocks git/gh/cloud commands on identity drift (exit 2) and denies .env and secret-store reads. Run `tq doctor` for details.
```

Sample for a neutral cwd (outside any registered base):

```
Client: none (neutral cwd: outside any base)

Rules: remote git, gh and cloud CLI commands are blocked here until you cd into a trusted workspace (tq allow <name>).
```

## Fallback mode (tq not installed)

`plugin/scripts/tq_hook.sh` is what the plugin's hooks actually invoke. It
resolves a `tq` binary (env var → plugin-bundled binary → `PATH` → known
install dirs) and runs `tq claude-hook <event>`, passing stdin through and
propagating the exit code.

If no `tq` binary can be found or run:

- `session-start` prints a one-line notice ("tq is not installed —
  identity enforcement is in fallback mode...") and exits 0.
- `pre-tool-use` falls back to `plugin/scripts/identity-guard.py`, a
  dependency-free Python script that still blocks remote git/gh/cloud
  commands when identity can't be verified (fail-closed). It deliberately
  does not go through the plugin's normal Python bootstrap
  (`tq_run.sh`/`tq_env.sh`), since that path can synchronously
  `pip install` and blow the `PreToolUse` timeout.
- **If no Python interpreter can be found either** (checked via `py -3` on
  Windows, then `python3`, then `python`), `pre-tool-use` blocks (exit 2)
  outright: on a box with neither `tq` nor Python, **every Bash and
  PowerShell command is blocked** until one of them is installed. This is
  intentional — it is the only way to guarantee no unverified remote
  command slips through. Payloads that clearly name another tool (Read,
  Edit, an MCP tool, …) are let through, since the widened `PreToolUse`
  matcher would otherwise block every file read too.
- The fallback has **no secret rules**: those live in the `tq` binary. It
  keeps a coarse **self-protection**: a Write, Edit, MultiEdit or
  NotebookEdit under `~/.tentaqles` or the installed plugin
  (`<config dir>/plugins/cache/tentaqles`), a Read or Grep under
  `~/.tentaqles`, and a shell command that names either one (a text match on
  the expanded home, `~`, `$HOME`, `$env:USERPROFILE`, `%USERPROFILE%`) are
  blocked with `BLOCKED [self-protection/tq/protected-path]`. It does not
  know the root marker or the human-only tq commands (tq is not installed).

The off launchers (`claude-off`, `codex-off`, `tq run --off -- claude`)
start the agent outside tq with the plugin switched off, so neither the
hook nor the fallback runs there.

## Hand-testing

From a POSIX shell (bash/zsh, or Git Bash on Windows):

```sh
echo '{"tool_name":"Bash","cwd":"'$PWD'","tool_input":{"command":"git push"}}' | tq claude-hook pre-tool-use; echo $?
```

From PowerShell:

```powershell
'{"tool_name":"Bash","cwd":"' + ($PWD.Path -replace '\\','\\\\') + '","tool_input":{"command":"git push"}}' | tq claude-hook pre-tool-use; $LASTEXITCODE
```

`"tool_name"` is required: a payload without it always looks like an
allow. The JSON must be valid — a mistyped backslash in a Windows path
makes the payload unparseable, which is also an allow. To test a secret
rule on a file tool:

```sh
echo '{"tool_name":"Read","cwd":"'$PWD'","tool_input":{"file_path":"'$PWD'/.env"}}' | tq claude-hook pre-tool-use --json
```

A block prints `BLOCKED [<category>/<rule>]: ...` to stderr and the exit
code is `2`; an allowed command prints nothing and exits `0`. Every block
also appends a line to `~/.tentaqles/denials.jsonl` (`$TQ_HOME` outside an
agent session).

To test `session-start`:

```sh
echo '{"tool_name":"Bash","cwd":"'$PWD'"}' | tq claude-hook session-start
```

## Secret rules (`pre-tool-use`)

After the identity guard (shell tools only), every call is checked against
the built-in secret rules (`internal/policy/builtin.go`). A matching rule
denies the call (exit 2); every matching rule is listed in the reason. There
are no other secret rules: nothing a manifest, a global file or a repository
file adds or removes.

| id | catches |
|---|---|
| `tq/secret-store-read`, `tq/secret-store-shell` | reading, from a file tool or the shell, SSH private keys, `~/.aws/credentials`, `.git-credentials`, `.netrc`, docker/gh/azure token files, the tq catalog |
| `tq/env-file-read`, `tq/env-file-shell` | reading or printing `.env*` (templates such as `.env.example` exempt) |
| `tq/env-dump` | `printenv`, `env`, `set`, `export -p`, `declare -p`, `typeset -p`, `cmd /c set`, `Get-ChildItem`/`gci`/`dir`/`ls env:`, each as the command word of a segment (start, or after `;` `&` `\|` `(` a backtick or a newline, or as the first word of a quoted `-c` script: `bash -c 'set'`) with no arguments; and `[Environment]::GetEnvironmentVariables(` anywhere. `set -e`, `echo set` or `git commit -m "set the flag"` are not dumps |
| `tq/commit-secret` | before a `git commit` runs, scans what it would record: the staged diff, plus the working tree and untracked files when the command also runs `git add` or `commit -a`, so `git add -A && git commit` is covered. Committing a `.env`, private key or `.pem` is refused by name. The reason lists `file:line (pattern)` and never the value. A known-fake fixture line can carry `tq:allow-secret` (or `gitleaks:allow`) |
| `tq/cross-client-mcp` | an MCP server that another client's bundle declares, and this workspace's bundle does not |
| `tq/guard-error` | the fail-safe: a guard panic asks (see Exit codes); a call tq cannot judge is denied |

### Limits

- The rules read tool input as text. Like the identity guard they catch
  honest mistakes, not an adversary: `python -c "open('.env').read()"`
  is not a `cat`.
- A command that runs `git -C other-repo commit` is scanned in the hook's
  cwd, not in `other-repo`.
- A malformed payload is allowed; only an oversized one fails closed.

## Self-protection (`pre-tool-use`)

The third category (PRD v2 §8, ADR-0005) keeps a session away from what
makes the other two hold: tq's state, the hook, the plugin, the binary and
the commands that change them. It runs after identity and secrets, only when
neither denied.

### The protected paths

The set is computed per call (`internal/protect/pathset.go`) from tq's state
folder, the registered bases, the resolved identity, `$CLAUDE_CONFIG_DIR`,
`$CODEX_HOME` and the running binary:

| What | Paths | Denied |
|---|---|---|
| tq's state | everything under `~/.tentaqles`: `config.yaml`, `trust/`, `identities/<ws>/manifest.yaml` and the identity's CLI homes, `denials.jsonl`, `audit.jsonl`, `protected-acl.json`, `anchors.yaml`, `integrity.key`, `bundles/`, `providers/`, `shims/`, `backups/`. In an identity's `claude` or `codex` home only the hook registration: `settings.json`, `settings.local.json`, `hooks.json`, `config.toml` (Claude's memory files, projects and todos there stay writable). The same Codex files under `$CODEX_HOME` | read and write |
| the root marker | `<base>/<ws>/.tentaqles.yaml` of every registered base (it holds the name only) | write |
| the plugin | `<config dir>/plugins/cache/tentaqles/**`, `plugins/marketplaces/tentaqles/**` (the marketplace clone, P1.4's trust anchor for batch accepts; git -C into it is denied too), `plugins/installed_plugins.json`, `plugins/known_marketplaces.json`, for the identity's Claude home, `$CLAUDE_CONFIG_DIR` and `~/.claude` | write |
| the binary | the running `tq`, its directory, and tq-named files beside it; the whole directory when it is tentaqles' own (`%LOCALAPPDATA%\tentaqles\bin`) | write |
| the keychain | tq's entry in the OS keychain (service `tentaqles`: the integrity key and the `tq secret` tokens) | write |

A path is made absolute against the cwd (or a preceding `cd`), cleaned,
symlink-resolved and compared without case on Windows and macOS; `~`,
`$HOME`, `${HOME}`, `$USERPROFILE`, `$env:USERPROFILE`, `%USERPROFILE%`,
`$TQ_HOME`, `$CLAUDE_CONFIG_DIR` and `$CODEX_HOME` are expanded in command
text, and a glob matches what it could expand to (`rm -rf ~/.tentaqles/*`).

### The rules

| id | denies |
|---|---|
| `tq/protected-path` | a Read, Grep (a Grep with no path searches the cwd) or NotebookRead of tq's state; a Write, Edit, MultiEdit or NotebookEdit of any protected path, also file by file in a Codex `apply_patch`; an `mcp__*` call whose input carries a string that resolves to a protected path; in the shell, a redirect (`>`, `>>`, `1>`, `2>`, `&>`) to a protected path, a copy, move, delete, `mkdir`/`New-Item`, permission or ACL change (`chmod`, `chown`, `icacls`, `attrib`, `chattr`, `chflags`), link (`ln`, `mklink`), `truncate`, `tee`, `Out-File`, `Set-Content`, `Add-Content`, `sed -i` or `perl -i` with a protected argument, a read (`cat`, `type`, `Get-Content`, `less`, `head`, `tail`, `grep`, `findstr`, `source`, a `<` redirect) of tq's state, `python -c`, `node -e`, `perl -e` and the like whose code names a protected path, a PowerShell `[IO.File]::...` call on a line that names one, `git -C`/`--git-dir`/`--work-tree` at a protected path or a protected path argument of a git sub-command that writes, a `cd` into a protected folder followed by a write (`cd ~/.tentaqles && git init`), and `cmdkey /generic:`/`/add:`/`/delete:`, `security add-generic-password`/`delete-generic-password -s` or `secret-tool store`/`clear service` naming `tentaqles` |
| `tq/human-only-command` | a shell line that runs tq (`tq`, `tq.exe`, any path to it, `go run ./cmd/tq`, also behind `sudo`, `env` or a nested `bash -c`/`cmd /c`) with a human-only verb: `init`, `add`, `edit`, `off`, `allow`, `deny`, `trust`, `hooks install`/`remove`, `hooks verify --accept`, `settings render` (not `--dry-run`), `settings anchors add`/`remove`, `bundle sync`/`capture`, `migrate --apply`, `uninstall`, `setup`, `onboard`, `secret set`/`rm`, `providers add`, `login`, `run --off`, `worktrees prune`. The commands refuse a session on their own too (ADR-0003); the hook denies them before they run |

Allowed: reading the marker, the plugin's files and the binary; `ls` of the
state folder; `tq doctor`, `tq list`, `tq env`, `tq hooks status`/`verify`,
`tq secret status` and every other tq read; dry runs (`tq migrate`,
`tq migrate manifests`, `tq settings render --dry-run`).

The shell rules read the command through the guard's tokenizer
(`guard.Commands`: quote-aware segments, wrappers, nested shells, `cd`
tracking, data heredocs skipped). Like the other categories they are a text
filter: a path built at run time (`python script.py` that writes there, an
alias, a variable tq does not expand) gets past them; the human-only commands'
own detectors, the private state folder and the signed integrity record are
the controls behind them.

### The denial log

Every deny of every category appends one JSON line to
`~/.tentaqles/denials.jsonl`, the same file the human-only commands log
their refusals to:

```json
{"time":"2026-11-05T10:00:00Z","kind":"self-protection","layer":"hook","tool":"Bash","command":"rm -rf ~/.tentaqles/trust","rule":"tq/protected-path","reason":"BLOCKED [self-protection/tq/protected-path]: ...","workspace":"acme","pid":4242,"cwd":"C:\\repos\\acme"}
```

`kind` is the category (`tq/guard-error` for a call tq could not judge),
`command` is the shell line or the file path; for an MCP call it is the tool
name and for a patch the file it is denied on, never the input or the
content. No environment value is ever written. A log that cannot be written
changes nothing: the deny stands. The file is under the protected paths, so
a session cannot read or rewrite it.

## Using a .env without printing it

Reading or printing a `.env` puts its secrets in the session transcript, so
`tq/env-file-read` (the Read and Grep tools) and `tq/env-file-shell` (`cat`,
`type`, `Get-Content`, `grep`, … on a `.env`) **deny** it. The refusal tells
the agent what to do instead, and so does the session-start message, so it
retries without asking you:

```
tq dotenv run -- npm run dev
tq dotenv run --file .env.local -- bash -c 'psql "$DATABASE_URL" -c "select 1"'
```

`tq dotenv run` loads the file into that one command's environment
(variables already set win unless `--override`) and replaces every loaded
value of 6+ characters with `[REDACTED:NAME]` in the command's stdout and
stderr. The agent only ever writes `$NAME`. Templates (`.env.example`,
`.sample`, `.template`, …) stay readable. `tq dotenv run` runs an arbitrary
command, so it is never pre-approved: it goes through the permission mode
like the command it wraps.

## Turning rules on and off

There is nothing to turn. The identity rules and the secret rules are always
on, in every Claude permission mode, and no file an agent can write changes
them. The only switches are trust and the off launchers:

- `tq allow <ws>` (**human-only**) trusts the manifest as it is now, by
  hash; until a workspace is trusted, non-read-only git, gh and cloud
  commands are blocked in it, and an edited manifest loses its trust until
  `tq allow` runs again. `tq deny <ws>` (**human-only**) removes the trust.
  Both refuse a session (an agent marker, a harness in the process tree, a
  non-terminal stdin or stdout) and ask you to type `yes` before the trust
  changes, so a session cannot trust a folder itself (ADR-0003).
- `claude-off` / `codex-off` (`tq run --off -- claude`) start the agent
  outside tq with the plugin switched off: main logins, no identity env, no
  hook. Use them for tq-free work, never for client work.
