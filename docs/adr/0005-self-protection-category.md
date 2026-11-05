# ADR-0005: The hook's third category, self-protection: protected paths and human-only tq commands

Status: accepted
Date: 2026-11-05
Cites: PRD v2 §6.3 (rule order), §7.1 (guard categories), §7.3 (deny message, denial log), §8.1 (human-only commands), §8.2 (protected paths); DISCOVERY.md §7.1 and §9 (Phase 1); threat model boundaries B1 (the hook and the state folder), B2 (human-only actions); ADR-0002 (the protected manifest), ADR-0003 (human-only detection)

## Context
PRD v2 §7.1 gives the guard exactly three categories, identity, secrets and
self-protection, always with the verdict deny; §6.3 orders them
`blocked_commands` → identity → secrets → self-protection, first match denies.
Until now the hook had the first two. ADR-0002 moved the manifest under
`~/.tentaqles` and ADR-0003 made every tq command that writes there refuse a
session, but a session could still write the state folder directly with a file
tool or a shell command (`echo x > ~/.tentaqles/config.yaml`, a Write to the
trust record), read the protected manifest, edit the plugin's `hooks.json` or
`tq_hook.sh`, or run `tq allow` and rely on the command's own refusal alone.
§8.2: "The hook MUST deny any Read, Write, Edit, shell redirect, copy, move, or
delete that targets a protected path whenever tq is on. Reading the manifest's
non-secret fields MAY be allowed through tq status only." §7.3 asks the deny to
name the category and every denial to be logged under the protected paths.

## Decision
We add the self-protection category to `tq claude-hook pre-tool-use` as two
rules, evaluated after identity and secrets.

**The protected path set** (`internal/protect/pathset.go`, `protect.New`) is
computed per hook run from `paths.Home()`, the registry's bases, the resolved
workspace, `$CLAUDE_CONFIG_DIR`, `$CODEX_HOME` and `os.Executable()`:

| Kind | Paths | Denied |
|---|---|---|
| state | everything under `~/.tentaqles` (`config.yaml`, `trust/`, `identities/<ws>/manifest.yaml` and the identity's CLI homes, `denials.jsonl`, `audit.jsonl`, `protected-acl.json`, `anchors.yaml`, `integrity.key`, `bundles/`, `providers/`, `shims/`, `backups/`); in an identity's `claude` or `codex` home only `settings.json`, `settings.local.json`, `hooks.json`, `config.toml`; the same Codex files under `$CODEX_HOME` | read and write |
| marker | `<base>/<ws>/.tentaqles.yaml` of every registered base | write |
| plugin | `<config dir>/plugins/cache/tentaqles/**`, `plugins/installed_plugins.json`, `plugins/known_marketplaces.json`, for the identity's Claude home, `$CLAUDE_CONFIG_DIR` and `~/.claude` | write |
| binary | the running tq binary, its directory, and tq-named files beside it; the whole directory when it is tentaqles' own (`%LOCALAPPDATA%\tentaqles\bin`) | write |

A harness's own working data inside an identity's config home (Claude's
memory files, projects, todos, sessions) is not protected: the harness writes
it through the same tools. Paths are canonical: made absolute against the cwd
(or a preceding `cd`), cleaned, symlinks in the existing prefix resolved, and
compared without case on Windows and macOS. `~`, `$HOME`, `${HOME}`,
`$USERPROFILE`, `$env:USERPROFILE`, `%USERPROFILE%`, `$TQ_HOME`,
`$CLAUDE_CONFIG_DIR` and `$CODEX_HOME` are expanded in command text, and an
MSYS drive (`/c/...`) is mapped. A glob matches when a protected path starts
with its literal prefix (`rm -rf ~/.tentaqles/*`).

**`tq/protected-path`** denies:
- Read, Grep (a Grep with no path searches the cwd), NotebookRead of a state
  path; Write, Edit, MultiEdit, NotebookEdit of any protected path; a Codex
  `apply_patch` per file it names;
- an `mcp__*` call whose input carries a string that resolves under a
  protected path (best effort);
- in Bash and PowerShell, through the guard's one tokenizer (`guard.Commands`,
  `guard.Verb`: quote-aware segments, wrappers such as `sudo` and `env`,
  nested `bash -c`, `cmd /c`, `pwsh -c`, cd tracking, data heredocs skipped):
  a redirect (`>`, `>>`, `1>`, `2>`, `&>`) to a protected path; a write verb
  (`cp`, `copy`, `Copy-Item`, `mv`, `move`, `Move-Item`, `ren`, `rm`, `del`,
  `Remove-Item`, `rmdir`, `rd`, `mkdir`, `New-Item`, `touch`, `dd`, `chmod`,
  `chown`, `icacls`, `attrib`, `chattr`, `chflags`, `takeown`, `ln`, `mklink`,
  `truncate`, `tee`, `Out-File`, `Set-Content`, `Add-Content`, `tar`, ... and
  `sed -i`, `perl -i`, `find -delete`) with any protected argument; a read
  verb (`cat`, `type`, `Get-Content`, `less`, `head`, `tail`, `grep`,
  `findstr`, `source`, ...) or a `<` redirect from a state path; `python -c`,
  `node -e`, `perl -e`, `ruby -e`, `php -r`, `deno eval`, `bun -e` whose code
  names a protected path, and a PowerShell .NET call (`[IO.File]::...`) on a
  line that names one; `git` with `-C`, `--git-dir` or `--work-tree` at a
  protected path, or a path argument of a sub-command that writes; a `cd`
  into a protected folder followed by a write (`cd ~/.tentaqles && git init`);
- a write to tq's OS keychain entry (service `tentaqles`: the integrity key
  of P1.4 and the `tq secret` tokens): `cmdkey /generic:`, `/add:` or
  `/delete:` naming it, `security add-generic-password` or
  `delete-generic-password -s tentaqles`, `secret-tool store` or `clear`
  with `service tentaqles`.

**`tq/human-only-command`** denies a shell line that runs tq (`tq`, `tq.exe`,
any path ending in `tq` or `tq.exe`, `go run ./cmd/tq`) with a human-only
verb: `init`, `add`, `edit`, `off`, `allow`, `deny`, `trust`, `hooks
install|remove`, `hooks verify --accept`, `settings render` (not
`--dry-run`), `settings anchors add|remove`, `bundle sync|capture`, `migrate
--apply` (also `migrate manifests --apply`), `uninstall`, `setup`,
`onboard`, `secret set|rm`, `providers add`, `login`, `run --off`,
`worktrees prune`. `tq status`, `tq doctor`, `tq list`, `tq env` and every
other read stay allowed: they are tq, not a file read. Dry runs are reads and
free: `tq migrate` without `--apply` and `tq settings render --dry-run` no
longer refuse a session in the commands themselves either.

**Order and message.** `pre-tool-use` runs the identity guard (the manifest's
`blocked_commands` first), then the secret rules with the commit scan and the
cross-client MCP check, then self-protection only when nothing denied; the
first category that denies is the one reported. Every deny reads
`BLOCKED [<category>/<rule>]: <reason>`: `identity/wrong-cloud`,
`secrets/tq/env-file-read`, `self-protection/tq/protected-path`.
`tq/cross-client-mcp` is in the identity category (a server outside the
identity's scope) and `tq/commit-secret` in secrets; `tq/guard-error` stays
outside the categories (`BLOCKED [tq/guard-error]` / `CONFIRM
[tq/guard-error]`), and it and the commit scan's ask are unchanged (Phase 2
turns them into denies). `--json` adds a `category` field.

**The denial log.** Every deny of every category appends one line to
`~/.tentaqles/denials.jsonl` through `audit.AppendDenial`: time, `kind` (the
category, or `tq/guard-error`), `layer` (`hook`), `tool`, `command` (the shell
line, the file path, or for an MCP call or a patch the tool name and the file
it is denied on, never the input or the content), `rule`, `reason`,
`workspace`, pid and cwd; never an environment value. A log that cannot be
written changes nothing: the deny stands.

**One marker list.** `internal/markers` holds the harness environment markers;
`internal/paths` (TQ_HOME ignored under a marker) and `internal/humanonly`
(the env-marker detector) both read it, so the two lists cannot drift.

**The fallback.** `plugin/scripts/identity-guard.py`, which runs when tq is
not installed, denies a file tool under `~/.tentaqles` (writes and reads) or
under the plugin cache (writes), and a shell command that names either, by a
text match on the expanded home.

## Consequences
- The hook is a text filter, a speed bump, not a sandbox (the owner's bound on
  this work; AR2). It catches the literal and common spellings of §8.2's
  routes across the file tools, an MCP input, the shell and Codex's
  `apply_patch`: file-tool writes and reads; a Codex patch per file; an MCP
  string that resolves to a protected path; and in the shell a redirect
  (glued `x>path` included), a copy/move/delete/mkdir/permission/link verb,
  `truncate`, `tee`, `Set-Content`, `sed -i`, `awk -i inplace`, `perl -i`,
  `ruby -i`, an editor (`vim`, `code`, ...), a downloader or converter
  (`curl -o`, `wget -O`, `7z -o`, `Invoke-WebRequest -OutFile`,
  `Start-BitsTransfer`, `Expand-Archive`, `Export-* -Path`, `certutil`,
  `fsutil file`, `patch`, `sort -o`, `unzip -d`), `git -C`/`--git-dir` or a
  writing git sub-command's path argument, a `find` with a destructive action
  rooted at or above a protected path, `… | xargs rm`, `eval`/`iex` of a
  literal string, a `cd` into a protected folder then a write or an
  interpreter, an interpreter (`python3.12`, `node20`, `ruby`, `php`, …) whose
  `-c`/`-e` code, path argument, or here-doc/stdin/pipe body names a protected
  path, a recursive search (`grep -r`, `rg`, `findstr /s`, `Get-ChildItem
  -Recurse`, the Grep/Glob tools) rooted at the home folder above the state,
  a PowerShell `[IO.File]::...` call, a `-EncodedCommand` (decoded; any
  accepted prefix `-e`…`-enco`, and `-ec`),
  `~`/`$HOME`/`${HOME}`/`$env:USERPROFILE`/
  `%USERPROFILE%`/`$TQ_HOME`/`$CLAUDE_CONFIG_DIR`/`$CODEX_HOME` expansion, an
  MSYS `/c/` path, an 8.3 short name, a case variant and a glob (`.t*`, `*`,
  `~/.tent*/…`) that can expand to a protected path. `TestProtectedPaths_Table`
  drives one case per write route and path kind; `TestProtectedPaths_RuleOrder`
  pins §6.3; `TestProtectedPaths_Codex`, `_MCPInput`, `_GlobTool`,
  `_EncodedCommand`, `_OtherWorkspaceAndNeutralCwd`, `TestDenialLogRedaction`,
  `TestKeychainWrite`, `TestHumanOnlyInvocation`, `TestMatchGlob`,
  `TestMatchKinds`, `TestNormalizeExpandsAndPlaces` and
  `TestSelfProtectionShellAndFileTools` cover the parts, and the `routes.py`
  corpus is the regression list.
- **Not caught — the named limits after round 3** (a path or command built at
  run time; the owner's bound says not to chase these, and P1.4's signed
  integrity record makes any change they land visible at the next terminal and
  harness start):
  - *a path held in a variable* — `D=~/.tentaqles; rm $D/config.yaml`,
    `T=tq; $T allow x`, a PowerShell `$p = Join-Path …; Set-Content $p`;
  - *a path built by string concatenation or a cmdlet at run time* —
    `node -e "...homedir()+'/.tent'+'aqles/...'"`, `Join-Path $HOME
    '.tentaqles\config.yaml'` passed to `[IO.File]` or `Copy-Item`;
  - *a command assembled then piped into a shell* — `printf 'tq allow x' | sh`
    (the string is not an argument the tokenizer reads);
  - *an alias or shell function* renamed over `tq` or a write verb;
  - *a write program tq does not list* run with a protected path argument;
  - *a script on disk* the agent wrote, then ran (`python evil.py`);
  - *base64 or another encoding* other than the one PowerShell
    `-EncodedCommand` case.
  The real controls behind the filter are the per-identity config homes
  (ADR-0001), the state folder private to the user (ADR-0002), the human-only
  commands' own detectors (ADR-0003), the signed integrity record (P1.4), and
  the harness's own sandbox and permission settings; a same-user program stays
  AR1.
- A session can no longer read the protected manifest, the trust store,
  `config.yaml` or an identity's `settings.json`. The `client-settings` and
  `rollback` skills now draft the manifest and ask the human to write it and
  run `tq allow`; reading a manifest's fields is `tq doctor` and `tq list`
  until `tq status` exists (Phase 5).
- An agent that asks the harness to edit its own identity settings
  (`settings.json` in the identity's Claude home) is denied; the project's
  `.claude/settings.local.json` is not tq's. The Claude plugin's PreToolUse
  matcher gains `Glob`, so a Glob into the state folder reaches the hook.
- Every deny message starts `BLOCKED [<category>/...`, and every deny is one
  line in `~/.tentaqles/denials.jsonl` whose `command` carries no arguments:
  the verb (the first word, after `sudo`/`env` and `NAME=value` assignments
  are stripped, skipping a PowerShell `$env:X=`/`$x=` assignment) plus the one
  path or token the denial named, never the rest of the line — so a secret in
  an argument, a here-doc body, an assignment value, a flag or a URL never
  reaches the log. Scripts that matched `BLOCKED:` or `BLOCKED [tq/...` change
  with the prefix.
- `internal/protect`, `internal/markers` and `internal/policy/selfprotect.go`
  join the paths that cross a trust boundary; so does `plugin/hooks/hooks.json`
  (the matcher) and `plugin/scripts/identity-guard.py` (the fallback).

## Alternatives considered
| Option | Why not |
|---|---|
| Express the protected paths as regex rules in `builtin.go` | The set depends on the run (home, bases, the identity's config home, the binary) and needs canonical paths (symlinks, case, `~`, relative to a `cd`); a regex over text cannot place a relative path |
| A second shell parser for the self-protection rules | Two tokenizers drift; the guard's quote-aware splitter, wrapper table and nested-shell reader are exported instead (`guard.Commands`, `guard.Verb`) |
| Deny every command that mentions `.tentaqles` | Denies `ls ~/.tentaqles`, a note about the folder, `tq doctor`, and a read of the marker the PRD allows |
| Protect the whole identity config home | Claude writes its memory files, projects and todos there through the same tools; only the files that register the hook and the plugin are tq's |
| Protect the binary's whole directory always | `~/.local/bin` or `/usr/local/bin` hold the user's other tools; only tentaqles' own bin folder is protected whole |
| A separate rule id for keychain writes | The keychain entry is one more protected location; one rule keeps the category's surface at two ids |

Records are never edited after acceptance. A changed mind is a new record that
supersedes this one.
