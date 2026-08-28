# `tq claude-hook` — Claude Code hook adapter

`tq claude-hook` is what the Claude Code plugin's `SessionStart` and
`PreToolUse` hooks actually call (via `plugin/scripts/tq_hook.sh`). It is
the source of truth for identity reporting and enforcement in 0.4.0+; the
plugin's Python code no longer switches gh/az/git identity itself.

## Subcommands

- `tq claude-hook session-start` — prints a short identity/status preamble. Never blocks (always exits 0).
- `tq claude-hook pre-tool-use` — decides whether to allow or block a Bash command. Exits 0 (allow) or 2 (block).

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

- `tool_name` — must be `"Bash"` for `pre-tool-use` to inspect anything.
  Any other tool is allowed unconditionally.
- `cwd` — the directory to resolve a workspace from. Falls back to the
  process's actual working directory if omitted or empty.
- `tool_input.command` — the Bash command about to run. Only read by
  `pre-tool-use`; ignored by `session-start`.

Any other fields in the payload (session id, etc.) are ignored.

The payload is capped at 1 MiB. A larger payload is a protocol violation,
not a parse error: `pre-tool-use` prints
`BLOCKED: hook payload exceeds 1 MiB` to stderr and exits 2 (fail closed),
while `session-start` prints its "could not resolve" line and exits 0.

### Exit codes

| Code | Meaning |
|------|---------|
| `0` | Allow. `session-start` always exits 0 (it never blocks a session). |
| `2` | Block (`pre-tool-use` only). A `BLOCKED: ...` message is written to **stderr**; stdout is not used for the block message. |

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
{"block": true, "rule": "git-email-drift", "reason": "BLOCKED: ..."}
```

- `block` — `true` when the command is refused (the process also exits 2).
- `rule` — the rule that matched (`""` when `block` is false).
- `reason` — the full multi-line stderr message (`""` when `block` is false).

`session-start` has no `--json` flag: its output is the fixed preamble
format described below.

## Rule precedence (`pre-tool-use`)

`internal/guard/guard.go`'s `Decide` function applies rules in this exact
order — the **first** matching rule wins and produces the block:

1. **`blocked-command`** — the command starts with (as a whole word, after
   any of `&& || ; | & \n \r $( \` ( { }`) an entry in the manifest's
   effective blocked-command list. Checked before anything else, even
   before cloud/identity checks.
2. **`wrong-cloud`** — the command invokes a cloud CLI (`az`, `aws`,
   `gcloud`, `gsutil`, `bq`, `doctl`) whose provider doesn't match the
   workspace's configured cloud provider. E.g. running `az ...` in an
   AWS-configured workspace.
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

## `session-start` output

Sample for a trusted workspace:

```
Client: Acme (en)
Git: github.com as acme-bot (dev@acme.com)
Cloud: azure (acme-prod subscription)
Identity: acme · CLAUDE_CONFIG_DIR=/home/acme/.tentaqles/identities/acme/claude · permission_mode=default

tq doctor:
- [ok] all checks passed

Rules: tq blocks git/gh/cloud commands on identity drift (exit 2). Run `tq doctor` for details.
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
  outright: on a box with neither `tq` nor Python, **every Bash command is
  blocked** until one of them is installed. This is intentional — it is
  the only way to guarantee no unverified remote command slips through.

## Hand-testing

From a POSIX shell (bash/zsh, or Git Bash on Windows):

```sh
echo '{"tool_name":"Bash","cwd":"'$PWD'","tool_input":{"command":"git push"}}' | tq claude-hook pre-tool-use; echo $?
```

From PowerShell:

```powershell
'{"tool_name":"Bash","cwd":"' + ($PWD.Path -replace '\\','\\\\') + '","tool_input":{"command":"git push"}}' | tq claude-hook pre-tool-use; $LASTEXITCODE
```

`"tool_name":"Bash"` is required: `pre-tool-use` only inspects Bash tool
calls and exits 0 for anything else, so a payload without it always looks
like an allow.

A block prints `BLOCKED: ...` to stderr and the exit code is `2`; an
allowed command prints nothing and exits `0`.

To test `session-start`:

```sh
echo '{"tool_name":"Bash","cwd":"'$PWD'"}' | tq claude-hook session-start
```

## Not gated in 0.4.0

MCP tool calls are **not** gated by `tq claude-hook` in this release — only
the `PreToolUse` hook's `Bash` matcher is wired up. A workspace's
blocked-command list, cloud checks, and identity drift checks apply only
to Bash commands Claude runs, not to MCP server tool invocations.
