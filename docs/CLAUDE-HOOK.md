# `tq claude-hook` — Claude Code hook adapter

`tq claude-hook` is what the Claude Code plugin's `SessionStart` and
`PreToolUse` hooks actually call (via `plugin/scripts/tq_hook.sh`). It is
the source of truth for identity reporting and enforcement in 0.4.0+; the
plugin's Python code no longer switches gh/az/git identity itself.

## Subcommands

- `tq claude-hook session-start` — prints a short identity/status preamble. Never blocks (always exits 0).
- `tq claude-hook pre-tool-use` — decides whether to allow, ask about, or block a tool call: shell commands (Bash, PowerShell), file reads and edits, and MCP tool calls. Exits 0 (allow, or ask via JSON on stdout) or 2 (block).

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

- `tool_name` — `Bash` and `PowerShell` go through the identity guard *and*
  the policy rules; every other tool (Read, Edit, Write, MultiEdit,
  NotebookEdit, Grep, `mcp__*`) goes through the policy rules only. A
  payload without `tool_name` is allowed.
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
| `0` | Allow — or **ask**, when stdout carries the PreToolUse decision JSON below. `session-start` always exits 0 (it never blocks a session). |
| `2` | Block (`pre-tool-use` only). A `BLOCKED: ...` message is written to **stderr**; stdout is not used for the block message. |

An **ask** is printed to stdout so Claude Code shows the user a
confirmation prompt with the reason:

```json
{"hookSpecificOutput": {"hookEventName": "PreToolUse",
  "permissionDecision": "ask",
  "permissionDecisionReason": "CONFIRM [tq/gh-admin-merge]: --admin bypasses branch protection (source: builtin)"}}
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
{"block": true, "rule": "git-email-drift", "reason": "BLOCKED: ..."}
```

- `block` — `true` when the command is refused (the process also exits 2).
- `rule` — the rule that matched: an identity rule (`git-email-drift`, …)
  or a policy rule id (`tq/env-dump`, …). For an ask, `block` is `false`
  and `rule`/`reason` name the rule; `""` when the call is simply allowed.
- `reason` — the full multi-line message (`""` when allowed).

With `--json`, an ask is reported in this object instead of the
`hookSpecificOutput` JSON, so the flag is for scripts, not for the hook.

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
  outright: on a box with neither `tq` nor Python, **every Bash and
  PowerShell command is blocked** until one of them is installed. This is
  intentional — it is the only way to guarantee no unverified remote
  command slips through. Payloads that clearly name another tool (Read,
  Edit, an MCP tool, …) are let through, since the widened `PreToolUse`
  matcher would otherwise block every file read too.
- The fallback has **no policy rules**: those live in the `tq` binary.

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
makes the payload unparseable, which is also an allow. To test a policy
rule on a file tool:

```sh
echo '{"tool_name":"Read","cwd":"'$PWD'","tool_input":{"file_path":"'$PWD'/.env"}}' | tq claude-hook pre-tool-use --json
```

A block prints `BLOCKED: ...` to stderr and the exit code is `2`; an
allowed command prints nothing and exits `0`.

To test `session-start`:

```sh
echo '{"tool_name":"Bash","cwd":"'$PWD'"}' | tq claude-hook session-start
```

## Policy rules (`pre-tool-use`)

After the identity guard (shell tools only), every call is checked against
a rule set. The strictest matching rule wins: **deny** (exit 2) beats
**ask** (JSON on stdout) beats allow. Every matching rule at that level is
listed in the reason.

### Layers

| Layer | Where | Trusted? | Can do |
|---|---|---|---|
| Built-in | compiled into `tq` (`internal/policy/builtin.go`) | yes | default rules, ids `tq/...` |
| Manifest | `guard:` in `.tentaqles.yaml` (hash-pinned by `tq allow`) | yes | add rules, **disable** built-ins by id |
| Project | every `.claude/tq-rules.yaml` from cwd up to the workspace root | no (repo content) | **add** rules only |

Nothing in a repository can loosen the guard: a project file's `disable`
key is ignored, and an untrusted manifest contributes no rules at all. A
rule that fails to compile is skipped; the other rules still apply.

### Rule format

```yaml
# .tentaqles.yaml
guard:
  disable: [tq/mcp-n8n-write]        # this client's n8n is a sandbox
  rules:
    - id: acme/no-prod-host
      action: deny                   # deny | ask
      command: 'prod-db\.acme\.internal'
      reason: never touch the prod database host from an agent
```

Matchers (all regexes, case-insensitive; every one present must match):

- `tool` — the tool name, anchored (`Edit|Write`, `mcp__.*n8n.*__.*`).
- `command` — the shell command (Bash/PowerShell only).
- `path` — the file path, with `\` normalized to `/`, so one rule works on
  Windows and POSIX.
- `content` — the text being written (Write `content`, Edit `new_string`,
  MultiEdit edits) or an MCP call's input JSON.
- `except` — cut out of each subject before matching, e.g. exempt
  `.env.example` without exempting a `.env` in the same command.
- `existing_only: true` — only when the path already exists (edits to a
  migration that was already written).

### Built-in rules

| id | action | catches |
|---|---|---|
| `tq/secret-store-read`, `tq/secret-store-shell` | deny | SSH private keys, `~/.aws/credentials`, `.git-credentials`, `.netrc`, docker/gh/azure token files, the tq catalog |
| `tq/env-file-read`, `tq/env-file-shell` | ask | reading or printing `.env*` (templates such as `.env.example` exempt) |
| `tq/env-dump` | deny | `printenv`, bare `env`/`set`, `Get-ChildItem Env:` |
| `tq/force-push-main` | deny | any force push (incl. `--force-with-lease`, `+main`) to main/master |
| `tq/force-push` | ask | plain `--force`/`-f` push elsewhere (`--force-with-lease` is fine) |
| `tq/gh-admin-merge` | ask | `gh pr merge --admin` |
| `tq/gh-repo-delete` | deny | `gh repo delete` |
| `tq/cloud-delete`, `tq/iac-destroy` | ask | `az/aws/gcloud/doctl/databricks … delete/rm/purge/…`, `terraform/cdk/pulumi destroy` |
| `tq/registry-write` | ask | `reg add/delete`, registry writes from PowerShell |
| `tq/sql-destructive-shell` | ask | DROP/TRUNCATE/ALTER/DELETE/UPDATE/GRANT through psql, sqlcmd, snowsql, supabase db, bq, … |
| `tq/migration-edit` | ask | editing an existing file under a migrations directory |
| `tq/mcp-sql-write`, `tq/mcp-apply-migration` | ask | write/DDL statements or migration/branch operations through a database MCP server |
| `tq/mcp-n8n-write` | ask | creating, updating, publishing, executing or deleting n8n workflows/agents |
| `tq/mcp-github-write`, `tq/mcp-hosting-exec` | ask | GitHub writes and hosting-provider operations through MCP |
| `tq/gha-unpinned`, `tq/gha-pr-target` | ask | workflow actions pinned to `@main/@master/@latest`; `pull_request_target` |

Two more checks need I/O and are not regex rules (both can be disabled by
id in the manifest):

- `tq/commit-secret` (deny) — before a `git commit` runs, scans what it
  would record: the staged diff, plus the working tree and untracked files
  when the command also runs `git add` or `commit -a`, so
  `git add -A && git commit` is covered. Committing a `.env`, private key
  or `.pem` is refused by name. The reason lists `file:line (pattern)`
  and never the value. A known-fake fixture line can carry
  `tq:allow-secret` (or `gitleaks:allow`).
- `tq/cross-client-mcp` (deny) — an MCP server that another client's
  bundle declares, and this workspace's bundle does not, is refused.

### Limits

- The policy reads tool input as text. Like the identity guard it catches
  honest mistakes, not an adversary: `python -c "open('.env').read()"`
  is not a `cat`.
- A command that runs `git -C other-repo commit` is scanned in the hook's
  cwd, not in `other-repo`.
- A malformed payload is allowed; only an oversized one fails closed.
