# ADR-0006: Deny only, fail closed, no permission settings, and a redacted denial log read by `tq status --denials`

Status: accepted
Date: 2026-11-05
Cites: PRD v2 §6.1 step 5, §6.3 (no ask, fail closed), §7.2 (harness launch), §7.3 (deny message, denial log), §8.1, §8.2, §13 (no permission modes), §14 (Phase 2 gate "no ask or mode code path remains"); DISCOVERY.md §5.1 (the removal list), §7.1 (the owner's decisions of 2026-11-05: deny both ask paths; remove the permission settings and clean them; Codex will get its bypass flag from the launcher); threat model boundary B1 (the hook and the state folder); supersedes the Codex `approval_policy`/`sandbox_mode` pair and the `claude.permission_mode` rendering of ADR-0001, and the argument-free `command` of ADR-0005's denial log

## Context
PRD v2 §6.3: "There is no ask verdict and no mode", and "The guard MUST fail
closed: if it cannot evaluate (crash, timeout, missing manifest), the hook
denies." Two ask paths were left after Phase 1: `tq/commit-secret` when the
commit scan could not finish, and `tq/guard-error` on a guard panic. Several
failures also allowed: a hook payload that did not parse was treated as "no
command" and let through, a registry or manifest tq could not read made the
cwd neutral (which denies only remote mutations), and a decision that ran past
the harness's own hook timeout (15 s for Claude, 30 s for Codex) was killed by
the harness, which then lets the call run.

§13 makes permission modes a non-goal and §7.2 leaves every permission decision
the guard does not make to the harness's own settings, which Tentaqles "does
not read, set, or override". tq still wrote `permissions.defaultMode`,
`permissions.allow/deny/ask` (a ~45-entry allow baseline and a deny baseline),
`skipDangerousModePermissionPrompt` into each identity's Claude
`settings.json`, `approval_policy`/`sandbox_mode` into Codex's `config.toml`,
read `claude.permission_mode` and `claude.permissions` from the manifest, took
`--permission-mode` in `tq add`, the setup wizard, `tq onboard` and setup
plans, printed `permission_mode=` in the session preamble and ran two doctor
checks about it (`bypass-cloud`, `permission-mode-overridden`).

§6.1 step 5 and §7.3 log every denial "with the identity, the command, the
matched rule, and the layer that caught it" and the calling process, readable
through `tq status --denials` from a terminal. Phase 1 (ADR-0005) logged the
verb and the matched path only, because a full command line can carry a secret
(a token in an argument or a header, a here-doc body, an assignment value).

## Decision
**Two verdicts.** `policy.Action` is `Allow` or `Deny`; a rule whose action is
anything else does not compile, and `Decision.Reason` only writes `BLOCKED
[<category>/<rule>]: ...`. `writeAsk`, `codexAnswer`, `codexAskPrefix` and the
`CONFIRM` prefix are gone. `commitscan`'s `AskError`/`IsAsk` are
`IncompleteError`/`IsIncomplete`.

**Fail closed.** `tq claude-hook pre-tool-use` runs the decision in a goroutine
that answers into buffers, and the command answers exactly one of: the
decision's buffers, or a deny of `tq/guard-error` when the decision panics or
misses `hookDeadline` (12 s, under both harness timeouts). The deny cases:

| Case | Rule | Test |
|---|---|---|
| A payload that cannot be read: a read error, more than 1 MiB, JSON that does not parse into the payload object (not JSON, truncated, an array, a string, a wrong type), `null`, empty, no `tool_name` | `tq/guard-error` | `TestPreToolUse_MalformedPayloadDenies`, `_OversizedPayloadDenies` |
| A panic anywhere in the analysis | `tq/guard-error` | `TestPreToolUseGuardPanicDenies`, the Codex case in `claudehook_codex_wire_test.go` |
| The decision runs past `hookDeadline` | `tq/guard-error` | `TestPreToolUse_TimeoutDenies` |
| `~/.tentaqles/config.yaml` cannot be read or parsed | `tq/guard-error` | `TestPreToolUse_UnreadableRegistryDenies` |
| The cwd's folder carries tq's marker or manifest but no usable manifest can be read (a marker without its protected copy, a file that is not a regular file, a manifest that does not parse, validate or hash): `resolve.Result.Unreadable` | `tq/guard-error`, every tool | `TestPreToolUse_MissingOrBrokenManifestDenies` |
| The commit scan could not finish (timeout, a size limit, an unreadable file, `--pathspec-from-file`, a repository it cannot place) | `secrets/tq/commit-secret` | `TestPreToolUseIncompleteScanDeniesBothAgents`, the `wantIncomplete` cases of `claudehook_finalfix_test.go` |
| A Codex patch tq cannot parse, or one too big for the budget | `tq/guard-error` | `claudehook_codex_wire_test.go` |

A folder with no marker, a marker that names another workspace and a manifest
whose `client` is another folder are not "unreadable": they are neutral as
before (ADR-0002), so the guard's must-not-trade-off tests of a replayed or
mismatched marker are unchanged. `TestNoAskCodePath` scans the non-test Go
sources of `internal/cmd`, `internal/policy`, `internal/guard` and
`internal/commitscan` for any ask answer, and `TestNoAskVerdict` makes an ask
rule fail to compile. Under `--agent codex` every deny writes the Codex deny
JSON and exits 2, so either half blocks. The plugin's `tq_hook.sh` turns any
exit of tq other than 0 and 2 (and the 126/127 fall-through) into exit 2 for
`pre-tool-use`, and the no-tq fallback `identity-guard.py` denies a payload it
cannot parse or that names no tool.

**Messages.** Every deny reads `BLOCKED [<category>/<rule>]: <reason>`; a call
tq cannot judge reads `BLOCKED [tq/guard-error]: <why>; tq denies a call it
cannot judge. Report this to the developer instead of retrying; ...`. An
identity deny ends with the identity's scope: `Scope of identity <name>: git
<host>; cloud <provider>[, <also>...]; MCP <servers>. Anything outside it is
denied; report this to the developer instead of retrying.` (or "no trusted
identity here"). `scopeNote`/`identityScope` in `claudehook_scope.go` is the
one place that reads it, the seam `Manifest.Scope()` replaces.

**No permission settings.** `claude.permission_mode` and `claude.permissions`
leave the manifest schema; an old manifest that still has them loads, and they
are recorded as dropped like an unknown block. `tq settings render` writes no
permission key and no `skipDangerousModePermissionPrompt`, and its next run
removes what tq wrote before and only that (the entries its state file
records; `defaultMode` and the prompt flag only when a previous render is
evidenced and the value is one tq wrote). Codex's `config.toml` gets no
`approval_policy`/`sandbox_mode`. `--permission-mode` leaves `tq add`, the
wizard, `tq onboard`, setup plans and `pkg/setupapi`; the preamble prints no
`permission_mode=`; doctor's `bypass-cloud` and `permission-mode-overridden`
checks are gone. The Codex launcher's full-bypass flag is Phase 3's shim.

**The denial log** (`~/.tentaqles/denials.jsonl`, a protected state path). One
JSON line per deny of every category and every human-only refusal:

| Field | Value |
|---|---|
| `time` | RFC 3339 |
| `category` | `identity`, `secrets`, `self-protection`, or `tq/guard-error` |
| `rule` | the rule id (`wrong-cloud`, `tq/env-dump`, `tq/protected-path`, ...) |
| `layer` | `hook`; `filesystem` for `tq/protected-path`; `human-only` for a command's own refusal |
| `caller` | `harness` (the main session), `subagent` (the payload carries `agent_id` or `agent_type`), or for a human-only refusal `no-tty` / `harness` |
| `harness` | `claude` or `codex` (the hook's `--agent`) |
| `tool` | the tool name |
| `command` | the shell line through `audit.RedactCommand`; a file tool's path; for an MCP call or a patch the tool name |
| `subject` | the one path or token the denial named (Phase 1's field) |
| `reason` | the message the model saw |
| `identity`, `cwd`, `pid` | the identity the cwd maps to (trusted or not), the folder, the process |

`RedactCommand` keeps the shape (verbs, flags, paths, operators) and drops what
can carry a value: here-doc and PowerShell here-string bodies; the body of
every quoted string; the value of `NAME=value`, `$env:NAME = value` and of a
flag whose name says credential (`--password`, `--token`, `-H`, `--header`,
`--auth`, `--cookie`, `--client-secret`, ...) and the password of `-u
user:password`; every span `internal/secrets` recognises (token prefixes,
JWTs, Bearer values, connection strings with a password, private keys,
key=value credentials); every remaining word of 24 or more characters mixing
letters and digits; then it cuts to 240 characters. `AppendDenial` masks
secret-shaped spans in every text field again, whatever the caller passed. Old
lines (`kind`, `workspace`) still read.

**`tq status`** is the successor name of `tq doctor` (both stay): without
`--denials` it is the doctor report; `tq status --denials [--last N]
[--identity X] [--category C] [--json]` prints the newest N entries (20; 0 for
all), oldest first. `--denials` is a human-only read: it refuses a harness
session by the env-marker and process-tree detectors (`humanonly.CheckRead`;
no terminal is required, so a human can pipe `--json`), logs the refusal, and
writes nothing.

## Consequences
- A guard crash or a broken manifest blocks every call in that folder until a
  human fixes tq or the manifest; the deny says so and names `tq doctor`. A
  commit too large to scan is the developer's to make from their own terminal.
- The harness's permission prompts are the harness's again: a session started
  from an identity runs with whatever the developer set there, and the hook
  still denies the three categories in its most permissive state.
- The log now says what was refused, not only its verb. **The redaction is a
  text filter and not a proof**: a short secret typed as a bare unquoted
  argument (`mysql -pS3cret`, `sshpass -p hunter2`) is kept. That is why the
  log stays a protected path the hook denies a session to read, and why
  `--denials` refuses an agent: the log also spans every identity (their
  folders and commands), which one identity's session must not see.
- Tests that pin all of this are in the table above, plus
  `TestRedactCommand`, `TestAppendDenialMasksEveryField`,
  `TestReadDenialsOldSchema`, `TestDenialLogSchema`, `TestDenialLogFailClosed`,
  `TestStatusDenials`, `TestStatusDenialsRefusesAgent`; CLAUDE.md "Must not
  trade off" lists them.

## Alternatives considered
| Option | Why not |
|---|---|
| Keep logging the verb and the path only (ADR-0005) | The PRD asks for the command; a verb alone does not tell the developer what the agent tried. The redaction plus the protected, human-only log keeps the residual risk to short bare secrets, which the developer typed to the agent already |
| Log the full command | A token in a header, a here-doc body or an assignment would land on disk in a file that outlives the session |
| Let an agent run `tq status --denials`, filtered to its identity | A carve-out in the reader; the log is also a map of what the guard catches. The model already gets the deny message for its own call |
| Ask on a guard error, as before | §6.3 has no ask; the owner decided deny for both |
| Treat a missing manifest as neutral (deny remote mutations only) | §6.3 names "missing manifest" as a deny; a neutral folder would let every non-remote call through under an identity tq cannot read |
| Rely on the harness's hook timeout | Claude Code and Codex continue the call when they kill a hook: that is fail open |

Records are never edited after acceptance. A changed mind is a new record that
supersedes this one.
