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

**Fail closed.** A harness that kills a hook at its own timeout (15 s for
Claude in the plugin's `hooks.json`, 30 s for Codex) runs the call, and so
does one whose hook exits with anything but 0 or 2. Three watchdogs answer
first, each with a deny: the decision runs in a goroutine that answers into
buffers and misses `hookDeadline` at 9 s; `RunMain`, the binary's entry
point, denies a `pre-tool-use` run still going after 10 s and turns any
command error (an unknown flag) into a deny with exit 2, which is the
guarantee Codex's bare `tq claude-hook pre-tool-use --agent codex` hook
gets; the plugin's `tq_hook.sh` runs tq in the background under a watchdog
of bash builtins, `sleep` and `kill` (no GNU `timeout`; Git Bash, macOS,
Linux), stops it after 11 s and denies, and turns any status other than 0
and 2 into exit 2. `TestHookDeadlinesUnderHarnessTimeouts` pins scan budget
< 9 s < 10 s < 11 s + 1 s < the shortest harness timeout - 2 s. The Codex
launcher refuses to start when `tq` is not on the PATH Codex will run with
(`TestCodexHookResolves`). The deny cases:

| Case | Rule | Test |
|---|---|---|
| A payload that cannot be read: a read error, more than 1 MiB, invalid UTF-8, JSON that does not parse into the payload object (not JSON, truncated, an array, a string, a wrong type), `null`, empty, no `tool_name` | `tq/guard-error` | `TestPreToolUse_MalformedPayloadDenies`, `_OversizedPayloadDenies`, `_FieldTypesDeny` |
| A field tq cannot judge: Bash/PowerShell with no, an empty or a non-string `command`; a `command`/`file_path`/`notebook_path`/`path`/`pattern` that is not a string; a Read naming no file; a tool tq does not know (a lower-case `bash` included) whose input carries one of those fields. An unknown tool with none of them is allowed | `tq/guard-error` | `TestPreToolUse_FieldTypesDeny` |
| A usage error of the hook command; a process still running after 10 s; a tq that `tq_hook.sh` stops after 11 s or that exits with another status | `tq/guard-error` | `TestRunMainHookErrorsDeny`, `TestRunMainWatchdogDenies`, `test_tq_hook_stops_a_hanging_tq_and_denies`, `test_tq_hook_turns_a_tq_without_decision_into_a_deny` |
| A panic anywhere in the analysis | `tq/guard-error` | `TestPreToolUseGuardPanicDenies`, the Codex case in `claudehook_codex_wire_test.go` |
| The decision runs past `hookDeadline` | `tq/guard-error` | `TestPreToolUse_TimeoutDenies` |
| `~/.tentaqles/config.yaml` cannot be read or parsed | `tq/guard-error` | `TestPreToolUse_UnreadableRegistryDenies` |
| The cwd's folder carries tq's marker or manifest but no usable manifest can be read (a marker without its protected copy, a file that is not a regular file, a manifest that does not parse, validate or hash): `resolve.Result.Unreadable` | `tq/guard-error`, every tool | `TestPreToolUse_MissingOrBrokenManifestDenies` |
| tq's own evidence says the folder is an identity whose manifest is gone: a registered folder with no marker or manifest while `trust/<name>.json` (not a tombstone) or the protected manifest exists; a folder under no registered base that a signed record names as its root (a missing `config.yaml`) | `tq/guard-error`, every tool | `TestPreToolUse_GoneManifestWithEvidenceDenies` |
| The commit scan could not finish (timeout, a size limit, an unreadable file, `--pathspec-from-file`, a repository it cannot place) | `secrets/tq/commit-secret` | `TestPreToolUseIncompleteScanDeniesBothAgents` (the real `--pathspec-from-file` branch), the `wantIncomplete` cases of `claudehook_finalfix_test.go` |
| A Codex patch tq cannot parse, or one too big for the budget | `tq/guard-error` | `claudehook_codex_wire_test.go` |

A folder with no marker, a marker that names another workspace and a manifest
whose `client` is another folder are not "unreadable": they are neutral as
before (ADR-0002), so the guard's must-not-trade-off tests of a replayed or
mismatched marker are unchanged, and a tombstone (`tq deny`) is no
evidence. `TestNoAskCodePath` scans the non-test sources of `internal/cmd`,
`internal/policy`, `internal/guard`, `internal/commitscan`,
`internal/codexcfg`, `internal/hooks`, `plugin/scripts` and `plugin/hooks`
for any ask answer; `TestNoAskAnswerInAnyOutput` runs a payload corpus
through every output mode and fails on any answer that is not a deny (an ask
assembled from split strings included); `test_fallback_never_answers_ask`
does the same for the fallback guard; `TestNoAskVerdict` makes an ask rule
fail to compile. Under `--agent codex` every deny writes the Codex deny
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
checks are gone. The Codex launcher (`tq run . -- codex`) starts Codex with
`--dangerously-bypass-approvals-and-sandbox` (the owner's decision); a flag
the developer typed about asking or the sandbox (`-a`, `-s`, `--full-auto`,
`--yolo`, the bypass flag, a `-c` override of `approval_policy` or
`sandbox_mode`) goes through as typed and nothing is added
(`TestLaunchArgsInjectsTheBypassFlag`). The shim folder on an identity PATH is
Phase 3. The cleaning of old settings decides from evidence only: the
allow/deny/ask entries a state file that parses records; the `defaultMode`
equal to what an older render wrote for this manifest's old
`claude.permission_mode` (the manifest keeps that value as
`OldPermissionMode`, never applied), not any value in a list of modes; the
skip flag of an older render's state file. A corrupt state file claims
nothing (`TestRenderKeepsADefaultModeTheEvidenceDoesNotName`,
`TestRenderCorruptStateClaimsNothing`). Codex's marked block is found with
surrounding white space ignored; a marker line tq cannot match, or an end
without a begin, is a conflict that leaves the file alone
(`TestRenderMarkerVariants`). Nothing tq writes into a shell profile names a
permission mode; the comment an older migrate wrote stays only as text to
recognise and remove (`TestNothingWrittenNamesAPermissionMode`).

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

`RedactCommand` is structural, not a list of secret shapes: a word is kept
only when its position says it cannot be a value. Kept are operators; the
command word of each segment (after `NAME=` assignments and wrappers such as
`sudo`, `env`, `xargs`), unless it mixes a capital with a digit; flag names
(`--name`, `--name=…`, a two-character `-x`, a known single-dash option such
as `-Recurse`; any other single-dash word keeps its first two characters,
`-pS3cret` is `-p…`); assignment names (conventional capitals or lower case);
variable references; the words of the subject the deny matched; and a short
list of subcommand words in the subcommand position only (right after the
command word or another kept subcommand, before any flag, never for `echo`,
`printf` and the like). Every other word is `…`, a quoted word (typographic
quotes included) `"…"`, here-doc and here-string bodies one placeholder line,
every control character dropped, cut to 240 characters. The leak corpus
(`TestRedactCommandLeakCorpus`: the round-1 critic's 40 commands, which the
pattern redaction leaked 16 of, and 38 new ones: lower-case passwords after
`-p`, glued values, secrets in subcommand-looking positions, `$(...)` and
backticks, PowerShell assignments and here-strings, URLs with userinfo,
fragments and webhooks, control characters around a value) holds 0 leaks.
`AppendDenial` also masks secret-shaped spans and drops control characters in
every text field, whatever the caller passed. The log rotates at 1 MiB into
`denials.jsonl.1` (one previous file kept), and the reader streams a line at
a time, skipping a runaway line (`TestDenialLogRotatesAndStreams`). Old
lines (`kind`, `workspace`) still read.

**`tq status`** is the successor name of `tq doctor` (both stay): without
`--denials` it is the doctor report; `tq status --denials [--last N]
[--identity X] [--category C] [--json]` prints the newest N entries (20; 0 for
all), oldest first. `--denials` is a human-only read: it refuses a harness
session by the env-marker and process-tree detectors (`humanonly.CheckRead`;
no terminal is required, so a human can pipe `--json`), logs the refusal, and
writes nothing. Every control character in every field is printed escaped
(`\x1b`, `\u009b`), in text and in `--json`, so a logged OSC 52 clipboard
write or a line erase cannot reach the terminal
(`TestStatusDenialsEscapesControlCharacters`).

## Consequences
- A guard crash or a broken manifest blocks every call in that folder until a
  human fixes tq or the manifest; the deny says so and names `tq doctor`. A
  commit too large to scan is the developer's to make from their own terminal.
- The harness's permission prompts are the harness's again: a session started
  from an identity runs with whatever the developer set there, and the hook
  still denies the three categories in its most permissive state.
- The log now says what was tried in shape (command words, flags, the
  matched path), not the values. A secret survives the structural rule only
  as a command word without a capital-and-digit mix, or as one of the listed
  subcommand words in the subcommand position. The log still stays a
  protected path the hook denies a session to read, and `--denials` refuses
  an agent: the log spans every identity (their folders and commands), which
  one identity's session must not see.
- **The old deny baseline is gone.** `tq settings render` wrote a
  `permissions.deny` baseline into every identity's `settings.json`
  (`Read(~/.ssh/id_*)`, `Read(~/.aws/credentials)`, `Bash(printenv)`, `Bash(gh
  repo delete:*)`, ...) and removes it now. What replaces it is the hook,
  which runs whatever the harness's permission setting: the secret rules
  `tq/secret-store-read` and `tq/secret-store-shell` (SSH keys, cloud and git
  credentials, the tq catalog), `tq/env-file-read`/`-shell`, `tq/env-dump`
  (`printenv` and the like), and self-protection for tq's state. `gh repo
  delete` and other destructive in-scope commands are the harness's own
  permission settings' business (PRD v2 §7), which the developer sets.
- A Codex session starts with the full-bypass flag: Codex no longer asks on
  request or sandboxes the workspace unless the developer types the flag.
  The hook is what holds the three categories.
- Tests that pin all of this are in the table above, plus
  `TestRedactCommandLeakCorpus`, `TestRedactCommandKeepsTheShape`, `TestAppendDenialMasksEveryField`,
  `TestReadDenialsOldSchema`, `TestDenialLogSchema`, `TestDenialLogFailClosed`,
  `TestStatusDenials`, `TestStatusDenialsRefusesAgent`; CLAUDE.md "Must not
  trade off" lists them.

## Alternatives considered
| Option | Why not |
|---|---|
| Keep logging the verb and the path only (ADR-0005) | The PRD asks for the command; a verb alone does not tell the developer what the agent tried. It stays the fallback if a structural leak is found that the rule cannot close |
| Redact by secret patterns (the first version of this ADR) | A list of shapes always lags: the round-1 critic leaked 16 of 40 commands through it (`mysql -pX`, `sqlcmd -P X`, URL userinfo, `sshpass -p`, curly quotes, short base64). The structural rule masks by position instead, so a new secret shape needs no new pattern |
| Log the full command | A token in a header, a here-doc body or an assignment would land on disk in a file that outlives the session |
| Let an agent run `tq status --denials`, filtered to its identity | A carve-out in the reader; the log is also a map of what the guard catches. The model already gets the deny message for its own call |
| Ask on a guard error, as before | §6.3 has no ask; the owner decided deny for both |
| Treat a missing manifest as neutral (deny remote mutations only) | §6.3 names "missing manifest" as a deny; a neutral folder would let every non-remote call through under an identity tq cannot read |
| Rely on the harness's hook timeout | Claude Code and Codex continue the call when they kill a hook: that is fail open |

Records are never edited after acceptance. A changed mind is a new record that
supersedes this one.
