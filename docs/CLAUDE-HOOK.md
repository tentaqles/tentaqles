# `tq claude-hook` — Claude Code hook adapter

`tq claude-hook` is what the Claude Code plugin's `SessionStart` and
`PreToolUse` hooks actually call (via `plugin/scripts/tq_hook.sh`). It is
the source of truth for identity reporting and enforcement in 0.4.0+; the
plugin's Python code no longer switches gh/az/git identity itself.

## Subcommands

- `tq claude-hook session-start` — prints a short identity/status preamble. Never blocks (always exits 0).
- `tq claude-hook pre-tool-use` — decides whether to allow, ask about, or block a tool call: shell commands (Bash, PowerShell), file reads and edits, and MCP tool calls. Exits 0 (allow, or ask via JSON on stdout) or 2 (block).
- `tq claude-hook stop` — Stop hook: the Jev completion-evidence check. Always exits 0; in enforce mode it may print `{"decision":"block","reason":...}` once per session. See `docs/DECIDE.md`.
- `tq claude-hook prompt-submit` — UserPromptSubmit hook: the Jev skill picker. Always exits 0 and never blocks; in enforce mode it may add a one-line `additionalContext` hint. See `docs/DECIDE.md`.

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

If the guard itself fails while it analyses a call (a panic in tq's own
code), `pre-tool-use` fails safe: it asks, with the reason
`CONFIRM [tq/guard-error]: tq guard error; please review the command yourself (…)`,
and writes `tq: guard panic: …` to stderr. It never allows the call and
never crashes (a Go crash exits 2 with a stack trace, which Claude Code
reads as a block). The unattended mode never skips this ask.

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

## Unattended mode

Unattended mode lets a bypass or auto session run without tq's *confirms*
(the `ask` decisions of PreToolUse) in the workspace or folders you choose.
It is for sandboxed demo folders and long agent runs, not for production
work.

**What it skips.** Only the `ask` branch, and only when all of these hold:

- the manifest has a `guard.unattended` block that covers the session's cwd;
- for a file tool (Edit, Write, ...), the target file's folder is covered too:
  editing outside the covered folder still asks;
- the workspace's current manifest is trusted, and `tq allow --bypass <ws>`
  was run for it (a separate trust step);
- the session's permission mode is exactly `bypassPermissions` or `auto`
  (any other mode asks as before);
- none of the matched rules is in the always-ask set or in `keep_asking`.

**What it never skips.**

- Deny rules: in Claude Code a blocked command stays blocked, in every mode.
  (tq's guard does not run inside Codex; see `CLAUDE-SETTINGS.md`, "Codex".)
- `tq/guard-change`: this confirm is always asked. It covers `tq guard`,
  `tq allow`, `tq deny` and `tq unattended on|off`, so an agent cannot widen
  its own permissions.
- Anything in `keep_asking`. The default is `tq/iac-apply` and
  `tq/remote-migration`; set `keep_asking: []` to keep nothing, or list your
  own ids (`tq guard list` shows them; unknown ids are a `tq doctor` warning).

**Manifest block** (edit with `tq unattended`, or by hand and then `tq allow`):

```yaml
guard:
  unattended:
    workspace: false            # true covers the whole workspace
    projects: [demo-todo]       # or only these folders, relative to the root
    keep_asking: [tq/iac-apply, tq/remote-migration]
    codex_sandbox: workspace-write   # read-only|workspace-write|danger-full-access
```

**Commands.**

| Command | What it does |
|---|---|
| `tq unattended on [--project <dir>] [--codex-sandbox <mode>]` | turn it on for the workspace, or for one folder |
| `tq unattended off [--project <dir>]` | turn it off for one folder (an error if it is not listed), or for the workspace and every folder; `keep_asking` and `codex_sandbox` stay for the next `on` |
| `tq unattended status` | what is covered and whether it is effective |
| `tq unattended log [--since 24h]` | the confirms it skipped (reads the current and the rotated file) |

`on` and `off` re-trust the manifest like `tq guard --ws` does, so they print
only the bypass hint when `tq allow --bypass` is still missing.

**Audit.** Every skipped confirm is appended to
`$TQ_HOME/guard/unattended.jsonl` (time, workspace, scope, mode, rule ids,
tool, command). The command is masked with the workspace's data classes and
cut to 200 characters; the file rotates to `unattended.jsonl.1` at 5 MB.
Write errors never fail the hook.

**Visibility.** SessionStart adds one line for a workspace with the block:
`Unattended: on (demo-todo) · 2 confirms skipped in 24h · applies in bypass
and auto sessions`, or `Unattended: configured, not effective (needs tq allow
--bypass <ws>)`, or `Unattended: on for demo-todo (not this folder)` (`on for
the workspace (not this folder)` when the cwd is outside the workspace root).
`tq doctor` warns with `unattended-not-effective`, `unattended-unknown-rule`
and `unattended-cloud` (unattended plus a cloud identity).

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
| `tq/env-dump` | deny | `printenv`, `env`, `set`, `export -p`, `declare -p`, `typeset -p`, `cmd /c set`, `Get-ChildItem`/`gci`/`dir`/`ls env:`, each as the command word of a segment (start, or after `;` `&` `\|` `(` a backtick or a newline, or as the first word of a quoted `-c` script: `bash -c 'set'`) with no arguments; and `[Environment]::GetEnvironmentVariables(` anywhere. `set -e`, `echo set` or `git commit -m "set the flag"` are not dumps. Bash heredoc bodies in the data shapes below are not read |
| `tq/force-push-main` | deny | any force push (incl. `--force-with-lease`, `+main`) to main/master |
| `tq/force-push` | ask | plain `--force`/`-f` push elsewhere (`--force-with-lease` is fine) |
| `tq/gh-admin-merge` | ask | `gh pr merge --admin` |
| `tq/gh-repo-delete` | deny | `gh repo delete` |
| `tq/cloud-delete`, `tq/iac-destroy` | ask | `az/aws/gcloud/doctl/databricks … delete/rm/purge/…`, `terraform/cdk/pulumi destroy` |
| `tq/iac-apply` | ask | applying infrastructure to a live environment: `terraform/tofu apply`, `pulumi up`, `cdk/cdktf deploy`, `kubectl apply`, `helm install/upgrade`, `sam/serverless/sls deploy`, CloudFormation, Azure and Deployment Manager deployments (`--dry-run` exempt) |
| `tq/remote-migration` | ask | running migrations (`prisma migrate deploy`, `alembic upgrade`, `flyway migrate`, `supabase db push`, `rails db:migrate`, `manage.py migrate`, …) against a connection that is not local; a command naming `localhost`, `127.0.0.1` or `--local` is exempt |
| `tq/settings-widen` | deny | Edit/Write of Claude Code settings (`.claude/settings[.local].json`, `.claude.json`, or a bare `settings[.local].json`) that adds `Bash(*)`, `bypassPermissions`, `disableAllHooks: true`, empty `hooks`, or `skipDangerousModePermissionPrompt: true`; permission changes go through `tq settings render` and the manifest |
| `tq/settings-shell-write` | deny | writing `.claude/settings[.local].json`, `.claude/tq-rules.yaml` or `.claude.json` from the shell (redirects, `tee`, `sed -i`, `cp`/`mv` onto them) |
| `tq/registry-write` | ask | `reg add/delete`, registry writes from PowerShell |
| `tq/sql-destructive-shell` | ask | DROP/TRUNCATE/ALTER/DELETE/UPDATE/GRANT through psql, sqlcmd, snowsql, supabase db, bq, … |
| `tq/migration-edit` | ask | editing an existing file under a migrations directory |
| `tq/mcp-sql-write`, `tq/mcp-apply-migration` | ask | write/DDL statements or migration/branch operations through a database MCP server |
| `tq/mcp-n8n-write` | ask | creating, updating, publishing, executing or deleting n8n workflows/agents |
| `tq/mcp-github-write`, `tq/mcp-hosting-exec` | ask | GitHub writes and hosting-provider operations through MCP |
| `tq/gha-branch-ref` | deny | workflow actions pinned to a branch: `@main` or `@master` |
| `tq/gha-unpinned` | ask | workflow actions pinned to `@latest` or `@head` |
| `tq/gha-pr-target` | ask | `pull_request_target` in a workflow |

More checks need I/O and are not regex rules (each can be disabled by id in
the manifest):

- `tq/commit-secret` (deny) — before a `git commit` runs, scans what it
  would record: the staged diff, plus the working tree and untracked files
  when the command also runs `git add` or `commit -a`, so
  `git add -A && git commit` is covered. Committing a `.env`, private key
  or `.pem` is refused by name. The reason lists `file:line (pattern)`
  and never the value. A known-fake fixture line can carry
  `tq:allow-secret` (or `gitleaks:allow`).
- `tq/commit-pii`, `tq/push-pii`, `tq/text-pii` (deny) — personal and
  financial data at the exits. `tq/commit-pii` scans the lines a commit would
  record (same scope as `tq/commit-secret`); `tq/push-pii` scans the commits a
  push would publish; `tq/text-pii` scans commit and tag messages and `gh pr`,
  `issue` and `release` text, including `--body-file`, and `gh api` fields
  (`-f`, `-F key=@file`, `--input`); text built by a command substitution
  asks. The reason lists
  `file:line (detector)` and never the value. Checksum-validated detectors
  (the `checksummed` class: CPF, CNPJ, card, IBAN) always run; e-mail and
  phone run only when the manifest's `data.classes` declares `pii`,
  `sensitive` or `children`. Declaring `financial` adds no detector beyond
  the checksummed ones. A known-synthetic line carries `tq:allow-pii`, but
  the marker only counts in test files (`*_test.go`, `test_*.py`,
  `*_test.py`, `conftest.py`, `*.test.*`, `*.spec.*`) and inside test and
  fixture directories (`tests/`, `fixtures/`, `seeds/`, `testdata/`, …),
  never under `docs/` or `migrations/` and never in published text. The
  scans run in the repository the command acts on (a leading `cd <dir>`,
  then `git -C <dir>`), cover `git commit <paths>`, and decode UTF-16 files.
  All of them share one 8 s budget per hook call (`TQ_HOOK_SCAN_BUDGET`, at
  most 12 s). If a scan cannot finish or vouch for the commit (a directory
  tq cannot resolve, a text file over 1 MiB, more than 8 MiB in total, the
  budget running out, an oversized push, unparseable quoting) the rule asks
  instead of denying. `tq redact` is the helper for a shareable copy; see
  [Using classified data without printing it](#using-classified-data-without-printing-it).
- `tq/pii-fixture-dump` (deny) — with a `data:` block in the manifest, a shell
  command that reads a classified path or table (`data.paths`, matched
  case-insensitively and in absolute or relative form) and writes into
  `tests/`, `fixtures/` or `seeds/`, including PowerShell writers
  (`Set-Content`, `Out-File`, `Export-Csv`, `Copy-Item`, ...). Fixtures must be synthetic. Its message
  points to `docs/privacy.md`, the per-project record of classified data that
  the planned development process asks for; tq only points at it.
- `tq/commit-message` (ask) — a `git commit` whose message (read from `-m`,
  `-F` or a heredoc) is not Conventional Commits: `type(scope): summary` with
  a known type, a summary of at most 100 characters, and a body when the
  change is breaking (`!` or `BREAKING CHANGE`). It also asks when it cannot
  read the message safely: an unreadable or oversized (over 64 KiB) `-F` file,
  or an unterminated quote. Commits with no readable message are skipped:
  the editor, `--amend`, `-C`, `--squash`, `--fixup`, `--no-edit` without
  `-m`/`-F`, `-F -`, and a message built by a shell substitution.
- `tq/cross-client-mcp` (deny) — an MCP server that another client's
  bundle declares, and this workspace's bundle does not, is refused.

### Limits

- The policy reads tool input as text. Like the identity guard it catches
  honest mistakes, not an adversary: `python -c "open('.env').read()"`
  is not a `cat`.
- A command that runs `git -C other-repo commit` is scanned in the hook's
  cwd, not in `other-repo`.
- A malformed payload is allowed; only an oversized one fails closed.

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

## Data classification (`data:` block)

Reading, querying and processing personal data locally is allowed: Claude can
read files, query tables and run scripts over data that holds personal
information. The guards sit at the exits, where data would be recorded or
published. tq guards commits, pushes, PR and issue text, and fixtures. tq has
no rule for the memory, vault and model exits; the Claude Code plugin's PII
redaction for those exits is planned for plugin 0.12.0.

A client workspace declares what it handles in its manifest:

```yaml
data:
  classes: [pii, financial]          # public, internal, pii, sensitive, financial, children, secrets
  regions: [BR]                      # where subjects live (two-letter codes)
  prompts:
    allow: [public, internal]        # classes that may appear in model context unmasked
  paths:                             # where classified data lives
    - path: db/schema/customers.sql
      classes: [pii]
    - table: public.payments
      classes: [financial]
  retention_default: 24m
  incident:
    authority: ANPD                  # ANPD | GDPR | none
    contact: dpo@example.test
```

Each `paths` entry has exactly one of `path` (a glob) or `table`, plus
`classes`. An unknown class, or `paths`/`prompts` without `classes`, makes the
manifest fail to load. The block drives which detectors the exit rules run
(without one, only the checksum-validated detectors run) and
`tq/pii-fixture-dump`. Changing the manifest changes its trust hash: run
`tq allow <ws>`. `tq doctor` warns with `data-block-missing` when a repo has
personal-data columns and no block (see `docs/CLAUDE-SETTINGS.md`).

## Using classified data without printing it

Reading personal data locally is allowed. What the guard refuses is recording
it: the exit rules `tq/commit-pii`, `tq/push-pii` and `tq/text-pii` (commit
messages and diffs, pushes, PR and issue text) point here. `tq redact` is the
helper for making a shareable copy without the values:

```
tq redact --classes pii < export.csv
tq redact --count < export.csv
tq redact --file a.csv --file b.csv
tq redact run --classes pii,financial -- psql "$DATABASE_URL" -c "select * from customers limit 5"
```

`tq redact` reads stdin (or each `--file`) and replaces CPF, CNPJ, e-mail,
phone, card and IBAN values (checksum-validated where one exists) and secrets
with `[REDACTED:<kind>]`; `--count` prints findings per detector and never
the values. `tq redact run` runs a command directly (not through a shell) and
masks its stdout and stderr, propagating its exit code.

Default classes: the trusted workspace manifest's `data.classes` minus
`data.prompts.allow`, plus `secrets`. Outside a workspace, or without a
`data:` block, every detector runs (`all`). `--classes` overrides this; an
unknown class or an empty list is an error. Whatever the classes, the
checksum-validated detectors (CPF, CNPJ, card, IBAN) always run, exactly as
in the exit rules, so a copy made with `tq redact` passes `tq/text-pii`:
`pii`, `sensitive` and `children` add e-mail and phone, `secrets` adds the
secret patterns, `all` runs everything. A card number followed by its expiry
(`MM/YY`) or its CVV is still masked.

## Turning rules on and off

Every rule has an id (`tq guard list` shows them all with their current
state). Change them with `tq guard`, or ask Claude with the `guard` skill:

```
tq guard list [--ws NAME]
tq guard off tq/cloud-delete --all              # every workspace
tq guard on  tq/cloud-delete --ws acme          # back on, for acme only
tq guard set tq/force-push deny --ws acme       # ask -> deny
tq guard set tq/force-push default --ws acme    # remove the override
```

Three trusted layers decide, most specific last:

1. tq's built-in rules (including the Jev `jev/*` rules);
2. `~/.tentaqles/guard.yaml`, written by `--all`: `disable`, `actions`
   (`ask`/`deny` per id) and extra `rules`, for every workspace;
3. the workspace manifest's `guard` block, written by `--ws`: `disable`,
   `enable` (switches back on what the global file turned off), `actions`
   and `rules`. It wins over the global file.

A repo's `.claude/tq-rules.yaml` is not a layer here. It is repo content,
so it can only add rules and can never turn one off.

`--ws` edits only a manifest that is currently trusted, checks that the
result still loads, and re-trusts it; a manifest with unreviewed edits is
refused. A global file that does not parse is ignored as a whole, so every
rule it changed is back to its default.

The guard protects its own settings: `tq guard off|on|set`, `tq allow` and
`tq deny` ask before they run (`tq/guard-change`), and direct edits to
`guard.yaml` from file tools or the shell are denied
(`tq/guard-file-write`). An agent can propose a change; you confirm it.
The shell rule denies any command line that names the file, but not a
heredoc body that is only data and mentions it (see below).

### Heredocs: data and code

For the **Bash** tool only, `tq/guard-file-write` (shell rule) and
`tq/env-dump` skip heredoc bodies when the *whole command* is a single
command (no `;`, `&&`, `||`, `|`, `&` or second line outside the bodies)
of one of these shapes, and only then:

- `cat` or `tee` reading a heredoc: `cat > f <<EOF`, `cat <<EOF > f`,
  `cat <<EOF`, `tee f <<EOF`, where no word is a path into a `.git`
  directory or a `hooks/` directory, or is named like a git config
  (`config`, `.gitconfig`, `.gitmodules`) or a git hook (`pre-commit`,
  `commit-msg`, …), and no word holds a `$` expansion;
- `git commit|tag|notes … -F -` (or `-F-`, `--file=-`, `--file -`)
  reading a heredoc, where commit/tag/notes is git's first non-option word
  (only `-C <dir>` and `--no-pager` may come before it; `-c` never);
- `git commit|tag … -m|--message "$(cat <<'EOF' … EOF)"` and
  `gh pr|issue create|edit|comment … --body|--title "$(cat <<'EOF' … EOF)"`,
  where the `$( )` holds only `cat <<WORD` and closes on the line after
  the delimiter.

Anything else and **every body is read, as before**:

- a second command, chained or on another line (`cd docs && cat > f <<EOF`,
  `cat > f <<EOF … EOF` then `ls`, two heredoc commands in a row);
- any pipe (`cat <<EOF | bash`, `| sudo -u root bash`, `| busybox sh`,
  `| tee`);
- any other `$( )`, backtick or `<( )` (`find -exec sh -c "$(cat …)"`,
  `su -c "$(…)"`, `eval "$(…)"`);
- a leading variable assignment, a wrapper (`sudo`, `env`, `xargs`, `wsl`,
  `su`, `find`, `make`, …) or any other command word (`bash x.sh`,
  `./x.sh`, `make -f mk`);
- an unterminated heredoc, a delimiter with a quote or backslash other than
  one pair around the whole word (`<<E"O"F`, `<<\EOF`), or an opener line
  that ends inside a quote or in a `\` continuation.

The command lines are always read, so `cat > ~/.tentaqles/guard.yaml <<EOF`
is still denied. A `<<` inside quotes (including `$'…'`), inside a `${ … }`
expansion or after a `#` comment does not start a heredoc. **PowerShell** commands are always read whole; a here-string
(`@' … '@`) is never data.
