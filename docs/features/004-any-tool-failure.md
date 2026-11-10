# 004: A failed call from any tool is logged

**Status** built, reviewed and evaluated on Windows on 2026-11-09 (tests, tester brief 9 of 9); CI and the owner's checks pending · **Stage** Sense · **Entries** S-01 (the rest: every tool, the skip rules, the lobe's
own tools, the grep rule, the worktree key), F-02 (`internal/lobe/shellwords`), F-01 (the worktree half of the
repository key) · **Needs** 003 · **Feeds** 005, 006, 008

## Result

A failed file edit, web fetch or MCP call in a client's Claude Code session adds one scrubbed line to the same
event log that 003 filled for shell commands. A call the person refused or interrupted leaves no line. A
command of the lobe itself leaves no line. A `grep` that found nothing is written as `ok`, not as a failure.

The steps that show it, in the sandbox, with `CLAUDE_CONFIG_DIR` set to acme's login folder:

```bash
printf '%s' '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"'"$W"'/clients/acme",
"tool_name":"mcp__acme__ping","tool_use_id":"toolu_01","tool_input":{"n":4411},
"error":"widget 4411 is not frobnicated"}' | tr -d '\n' |
  tentaqles lobe-hook claude post-tool-use-failure; echo "exit=$?"
cat "$TQ_HOME"/identities/acme/lobe/events/events-*.jsonl
```

The first command prints only `exit=0`. The second prints one line with `"tool":"mcp__acme__ping"` and
`"kind":"fail"`.

## Scope

**In**

- Every tool name is recorded: `Edit`, `Write`, `MultiEdit`, `NotebookEdit`, `Read`, `Grep`, `Glob`,
  `WebFetch`, `WebSearch`, `mcp__<server>__<tool>` and any other. A missing `tool_name` is stored as `unknown`
  (S-01 "How to build it"). The shell-only check of 003 is removed.
- `sense.CallText` for tools that are not shell tools.
- The skip rules for an interrupted call and for a call the host refused (S-01 step 2).
- The lobe's own tools, shell and MCP (S-01 step 3), and `internal/lobe/shellwords` with its parity test
  against the guard (F-02 "How to build it").
- A `grep` that found nothing is `ok` (S-01 step 4).
- A linked git worktree takes the key of its main repository (S-01 step 6 second paragraph, F-01 step 5
  second paragraph).

**Out**

- `capture: off` (005). The seventh row of `TestLobeSenseFailureSkips` arrives there.
- `fp` (006); `dup` and the replay check (007).
- Successes in general and the projection of an `ok` input (008). The one `ok` line written here, for a grep
  with no match, keeps its scrubbed input in full; 008's projection then applies to it like any other.
- A failed MCP call that Claude Code reports as a success, on `PostToolUse` (009). See Risks: if the phase 0
  result of 003 shows that every failed MCP call arrives that way, the MCP rows of this feature move to 009.
- Codex (010).
- A note of a refused or interrupted call for C-02's evidence rule (c). S-01's risks say it is not in the
  first build; nothing is written for such a call, in the log or anywhere else.
- The graph folder of a linked worktree, `graph/<repo-key>/wt-<8 hex>/` (048). Only the key is built here.

## What it builds on

- **003**: `hook.Main` and its step table, the client rule, `store.Open`, `store.RepoKey` (plain form),
  `scrub.Input` and `scrub.Text`, `eventlog.ID` and `eventlog.Append`, `sense.FromClaudeFailure`,
  `sense.Record`, `sense.CallText` (shell form), `place.go` (the walk to `.git`), the `PostToolUseFailure`
  entry in the plugin, which already has no matcher, and the phase 0 result
  `docs/prd/spikes/phase0-q2-claude-code-hooks.md` with its rows for a failed edit, an MCP error result, an
  unknown MCP tool, a declined call and an interrupted call.
- **003's tests**, all kept green: the house-rule tests named there.
- Existing tentaqles code, read and not changed: `internal/guard/gitinv.go` L242 (`splitWords`).

## What it leaves for later features

| Name | What it is | Used by |
|---|---|---|
| `internal/lobe/shellwords` | the lobe's own word splitter, with `testdata/cases.json` | 009 (S-04), 014 (C-08), 017 (R-04), 034 (F-05), 039 (E-04) |
| `sense.CallText(tool, input)` | one text per call for every tool | 023 (E-02), 038 (E-01) |
| `sense/refused.go` | the table of host refusal texts, each with the version it was seen on | 010 adds Codex's texts if any |
| `sense/own.go` | `IsOwnTool(tool, input)` (name proposed here) | 008 (a success of the lobe's own tool is not recorded either) |
| `store.RepoKey` with worktrees | a linked worktree gives its main repository's key | 016, 043, 048 |
| An `ok` line in the log | `kind` `ok`, no `error` | 005 (counts by kind), 008 |

## How it works

1. **Every tool.** The check "the tool is `Bash` or `PowerShell`" leaves `sense`. `tool` is the payload's
   `tool_name` as it arrived. It is never used as a path.
2. **The input.** `input` is the compact JSON of `tool_input` with sorted keys, as in 003. S-05's key pass now
   matters: a text value under a key that names a secret becomes `[redacted:key]` (S-05 step 1). The input is
   cut to 4000 characters ending in `[...]` (S-05 step 4); a reader must treat such an `input` as text.
3. **The error.** The `Exit code N` rule stays for `Bash` and `PowerShell` only. For every other tool the
   `error` is stored whole after scrubbing, and there is no `exit`.
4. **The call text.** `sense.CallText` gives the command string for a shell tool (003). For any other tool it
   gives the compact JSON of the tool input with sorted keys, scrubbed by S-05 and cut at 4000 characters
   (Sense page, "Where Sense ends"). It is built from the whole input. Sense never stores it; it travels in
   `Outcome.Call`.
5. **Interrupted.** When `is_interrupt` is true, nothing is written (S-01 step 2).
6. **Refused.** When `error` starts with one of the texts Claude Code writes for a call that the person or a
   hook refused, nothing is written: the tool never ran. The texts live in one table in `refused.go`, each
   with the Claude Code version it was seen on. The first rows are ported from Trail-code (see Where to
   read) and checked against the declined row of the phase 0 result.
7. **The lobe's own tools** (S-01 step 3) are not recorded.
   - A shell call (`Bash`, `PowerShell`) whose program is `tentaqles` or `tq` with `lobe` as its first word.
     The command is split with `internal/lobe/shellwords`. The program is compared without its folder and
     `.exe`, case ignored. A command of several statements is the lobe's own only when every statement is, a
     leading `cd` aside (proposed): `cd app && tq lobe graph map` is skipped, `npm test && tentaqles lobe
     status` is recorded.
   - An MCP tool whose own name, the part after the server in `mcp__<server>__<tool>`, starts with `lobe_`
     (proposed). The server part is whatever the person registered.
8. **A grep that found nothing** (S-01 step 4). A shell call whose error is empty or only `Exit code 1`, and
   whose command runs `grep`, `egrep`, `fgrep`, `zgrep`, `rg`, `ag`, `ack` or `git grep`, is written with
   `kind` `ok`, no `error` and the `exit` the host stated. The program is found with `shellwords`.
9. **The order** inside `FromClaudeFailure`: interrupt, refusal, own tool, grep rule, then the event of 003.
   Each skip ends Sense with no line and no outcome; the hook still exits 0 with no output.
10. **A linked worktree** (S-01 step 6, F-01 step 5). When the `.git` the walk found is a file, read its
    `gitdir:` line, then the `commondir` file in the folder that line names. That leads to the main
    repository's `.git` folder; the key is the one of the folder that holds it. When the `gitdir:` folder
    has no `commondir` file (a submodule), or the main repository lies outside the client folder (proposed),
    the folder where the walk stopped is keyed by its own path. `cwd` stays relative to the folder where the
    walk stopped. No git command runs.
11. **`shellwords`.** The package is the lobe's own copy of the guard's splitter. One data file,
    `internal/lobe/shellwords/testdata/cases.json`, is read by a test in that package and by a new test
    file in `internal/guard` that runs the guard's `splitWords` over it. Both must give the same words. No
    guard source file changes, and no lobe package imports `internal/guard` (core page 5.1).

## Where to read

| What | Where |
|---|---|
| Requirements | [S-01](../prd/lobe-sense.md), all of it, and "Where Sense ends" at the top of that page; [S-05](../prd/lobe-sense.md) steps 1 and 4 |
| Requirements | [F-01](../prd/lobe-foundation.md) step 5; [F-02](../prd/lobe-foundation.md) "How to build it", the `shellwords` bullet |
| Shared rules | [core page](../prd/vertical-lobe.md) 5.1 (import rules), 5.3 (the worktree sentence), 5.4 (event), decision D-3 and A-24 |
| Phase 0 result | `docs/prd/spikes/phase0-q2-claude-code-hooks.md` (written in 003) |
| Source to port: refusal texts and the grep rule | [`src/trace/run-calls.ts` L114](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/run-calls.ts#L114) (L246 is the rule for a grep that found nothing) |
| Source to port: interrupted call | [`src/hooks/post-tool-failure.ts` L31](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L31) |
| Source to port: every tool | [`plugin/hooks/hooks.json` L48](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/plugin/hooks/hooks.json#L48) (registered with no `matcher` key) |
| Host facts (web, not machine-checked) | <https://code.claude.com/docs/en/hooks>: PostToolUseFailure carries `tool_name`, `tool_input` and an `error` text |
| Existing tentaqles code | `internal/guard/gitinv.go` L242 (`splitWords`, to copy and to test against); `internal/lobe/sense/` and `internal/lobe/store/repokey.go` from 003 |

## Build tasks

Tasks 2, 3 and 4 can run side by side. Task 5 needs task 2.

1. **Check this brief against the code as it stands** and against the phase 0 result of 003. List what no
   longer fits: the event that carries a failed edit and a failed MCP call, the field that marks an
   interrupt, the text of a declined call. If a failed MCP call does not arrive on `PostToolUseFailure`,
   stop and ask the owner (Risks).
2. **`internal/lobe/shellwords`**: the splitter, `testdata/cases.json`, its test, and the new test file in
   `internal/guard`. Test: `TestLobeShellwordsMatchTheGuard`. Check: `git diff` shows no change to a
   non-test file under `internal/guard`; `TestLobeImportsOnlyTheFour` and `TestLobeGuardNeverImportsLobe`
   stay green.
3. **Every tool and the call text.** Remove the shell-only check; widen `calltext.go`. Files:
   `internal/lobe/sense/claude.go`, `calltext.go`, `sense.go`. Tests: `TestLobeSenseFailureEveryTool`
   (6 lines), `TestLobeSenseOutcome` (the non-shell row).
4. **The worktree key.** Files: `internal/lobe/store/repokey.go`, `internal/lobe/sense/place.go`. Tests:
   `TestLobeRepoKey` (5 of 5), `TestLobeSenseWorktreeKey`.
5. **The skip rules.** Files: `internal/lobe/sense/refused.go`, `own.go`, and the grep rule in `claude.go`.
   Tests: `TestLobeSenseFailureSkips` (6 rows), `TestLobeSenseOwnTools`, `TestLobeSenseGrepNoMatch`.
6. **Docs.** `CHANGELOG.md` under `[Unreleased]`. `docs/reference/commands.md`: the section
   `## tentaqles lobe-hook claude` says that every tool is recorded and names the four skip rules. No plugin
   file changes, so `plugin/CHANGELOG.md` gets no line. No new "Must not trade off" line.
7. **The tester run** (Evaluation), by a fresh agent, then the owner's Person checks.

## Tests that stay in the repository

Names are the requirements'.

- `TestLobeSenseFailureEveryTool`: failures for `Bash`, `PowerShell`, `Edit`, `Write`, `WebFetch` and
  `mcp__acme__ping` write 6 lines, all with `kind` `fail`.
- `TestLobeSenseFailureSkips`: interrupt, refusal, bad JSON, no client, another client's login folder, no
  login variable: 0 lines in any store, exit 0 and empty stdout in 6 of 6. 005 adds `capture: off`, the
  seventh.
- `TestLobeSenseOwnTools`: `tentaqles lobe graph find x`, `tq lobe status`, `cd app && tentaqles.exe lobe
  graph map` and the tool `mcp__graph__lobe_find` write 0 lines; `tentaqles doctor`, `npm test && tentaqles
  lobe status`, `echo tentaqles lobe` and `mcp__acme__ping` write 1 line each. 8 of 8.
- `TestLobeSenseGrepNoMatch`: `grep` with `Exit code 1` and no other text gives `ok`; with `Exit code 2`,
  `fail`.
- `TestLobeSenseWorktreeKey`: an event from a folder whose `.git` is a file with a `gitdir:` line, behind
  which a `commondir` file leads to the repository `app`, has the `repo` of an event from `app` itself; with
  no `commondir` file it has the key of its own folder. 2 of 2, with an empty `PATH`.
- `TestLobeRepoKey`: the four rows of 003, and a linked worktree at `wt/fix`, whose `.git` file leads
  through `commondir` to `app`, gives `r_a172cedc`. 5 of 5. The graph folder half of that row waits for 048.
- `TestLobeSenseOutcome`: as in 003, and `CallText` gives one text for the payloads before and after a call
  of an MCP tool whose input keys arrive in two orders.
- `TestLobeShellwordsMatchTheGuard`: every case in `cases.json` gives the same words on both sides.
- A scrub row for a non-shell input: `{"password":"hunter2","note":"hi"}` is stored as
  `{"note":"hi","password":"[redacted:key]"}`. It joins `TestLobeScrubParity`'s table; no new test name.

House-rule tests kept green, unedited: `TestLobeHookAlwaysExitsZero`, `TestLobeStall_SpinningHandler`,
`TestLobeHookServesOnlyItsLogin`, `TestLobeHookLoginUnsetDoesNothing`, the `TestLobeScrub*` tests,
`TestLobeHookErrorsHoldNoContent`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
`TestLobeSenseStartsNoProcess` (the worktree read starts no process either), and every test under "Must not
trade off" in `CLAUDE.md`. The plugin's pinned test still lists exactly `PostToolUseFailure`.

## Evaluation

The tester is a fresh agent with the built program called by its full path (read every `tentaqles` below as that
path: in an activated shell `tentaqles` is a shell function that runs the installed program) and an empty
working folder. Use bash (Git Bash on Windows). No network. Run the standard setup of feature 002 first:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
```

Stop and report FAIL when a line starts with `SETUP FAIL`. Then:

```bash
ID="$TQ_HOME/identities"; ACME="$W/clients/acme"
LOG="$ID/acme/lobe/events/events-$(date -u +%Y%m%d).jsonl"
F="tentaqles lobe-hook claude post-tool-use-failure"
export CLAUDE_CONFIG_DIR="$ID/acme/claude"     # the login: without it a lobe hook does nothing
n() { wc -l < "$LOG" | tr -d ' '; }
cd "$ACME"
```

Pipe each JSON to `$F` with `ACME` replaced by the value of `$ACME`. A JSON printed here over two lines is
one line: join the lines with a space. Every call must exit 0 with empty stdout. "Line k" is line k of `LOG`
read as JSON. No line in this feature has an `fp`.

1. `{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping",
   "tool_use_id":"toolu_01","tool_input":{"n":4411},"error":"widget 4411 is not frobnicated"}`
   `n` prints `1`. Line 1 has `v` 1, `id` `evt_9bde2e09`, `session` `s1`, `host` `claude`, `tool`
   `mcp__acme__ping`, `kind` `fail`, `cwd` `.`, `input` `{"n":4411}`, `error`
   `widget 4411 is not frobnicated`, and no `exit`, no `repo` and no `fp`.
2. The same with `toolu_02` and `"is_interrupt":true` added, then with `toolu_03` and the error
   `The user doesn't want to proceed with this tool use.`, then the plain text `not json`. `n` still prints
   `1` each time.
3. `{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash",
   "tool_use_id":"toolu_04","tool_input":{"command":"grep -n zzz notes.txt"},"error":"Exit code 1"}`
   `n` prints `2`. Line 2 has `id` `evt_20ed2862`, `kind` `ok`, and no `error`. The same with `toolu_05` and
   the error `Exit code 2\ngrep: notes.txt: No such file or directory` gives `n` of `3`, and line 3 has
   `kind` `fail` and `exit` 2.
4. Check 3 with `toolu_06` and the command `tentaqles lobe graph find zzz`, then with `toolu_07` and the
   command `cd app && tq lobe status`, then with `toolu_08`, the tool `mcp__graph__lobe_find` and the input
   `{"q":"zzz"}`. `n` still prints `3` each time. Then check 3 with `toolu_09`, the command
   `npm test && tentaqles lobe status` and the error `Exit code 1\n1 failing`: `n` prints `4`.
5. Three more tools, each with the error `nope` and its own id: `Edit` with `toolu_10` and the input
   `{"file_path":"ACME/notes.txt","old_string":"a","new_string":"b"}`; `WebFetch` with `toolu_11` and the
   input `{"url":"https://example.test/x","prompt":"read"}`; `mcp__acme__ping` with `toolu_12` and the input
   `{"password":"hunter2","note":"hi"}`. `n` prints `7`. Lines 5 and 6 have `tool` `Edit` and `WebFetch`,
   `kind` `fail` and no `exit`. Line 7 has `input` `{"note":"hi","password":"[redacted:key]"}`, and
   `grep -c hunter2 "$LOG"` prints `0`.
6. Check 1 with `"session_id":"s3"`, `toolu_13` and `"cwd":"ACME/app"`. `n` prints `8`. Line 8 has `cwd` `.`
   and `repo` `r_a172cedc`.
7. Check 1 with `toolu_14`, once with `CLAUDE_CONFIG_DIR="$ID/globex/claude"` set for that one command and
   once through `env -u CLAUDE_CONFIG_DIR`. `n` still prints `8` each time, and
   `ls "$ID/globex/lobe/events" 2>/dev/null | wc -l` prints `0`.
8. A linked worktree, made by hand with no git. `mkdir -p "$ACME/app/.git/worktrees/wt" "$ACME/app-wt"`,
   then `printf '../..\n' > "$ACME/app/.git/worktrees/wt/commondir"` and
   `printf 'gitdir: %s/app/.git/worktrees/wt\n' "$ACME" > "$ACME/app-wt/.git"`. Then check 1 with
   `"session_id":"s4"`, `toolu_15` and `"cwd":"ACME/app-wt"`. `n` prints `9`. Line 9 has `cwd` `.` and the
   same `repo` as line 8.
9. A submodule shape. `mkdir -p "$ACME/sub" "$ACME/subgit"`, then
   `printf 'gitdir: %s/subgit\n' "$ACME" > "$ACME/sub/.git"`. Check 1 with `"session_id":"s5"`, `toolu_16`
   and `"cwd":"ACME/sub"`. `n` prints `10`. Line 10 has a `repo` of `r_` plus 8 hex digits that differs from
   line 8's.

**Person checks**, for the owner, in a real Claude Code session of one client, loaded as in 003:

1. Ask the agent to edit a file with an `old_string` that is not in it. Expect one new line with `tool`
   `Edit` and `kind` `fail`.
2. Ask for a command that needs a permission prompt and answer no. Expect no new line.
3. Start a long command (`sleep 60`) and interrupt it. Expect no new line.
4. If the client has an MCP server, call one of its tools with an argument it rejects. Expect one new line
   whose `tool` starts with `mcp__`, or, if none appears, tell the builder: that is the case of Risks.

**Scoring.** PASS needs 9 of 9. "Could not run" is a FAIL. Report anything surprising; fix nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI on three
  systems: `go vet ./...`, `go test ./... -count=1`, `go build ./...`. The plugin suite stays green; the
  plugin did not change.
- [ ] The tester brief passes, 9 of 9.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeScrub*`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, and the guard's tests
  unedited. No non-test file under `internal/guard` changed.
- [ ] `CHANGELOG.md` and `docs/reference/commands.md` are updated.
- [ ] The owner has seen a failed edit of their own in the log, and no line for a refused call (Person
  checks 1 and 2).

## Risks and unknowns

- **Which event carries a failed MCP call** is not documented (core page 5.2, S-01 risks). The phase 0
  result of 003 decides. If it arrives on `PostToolUseFailure`, this file stands. If it arrives on
  `PostToolUse` as a result that says it failed, this feature still records edits and fetches, the MCP rows
  of checks 1 and 5 stay valid for payloads that do arrive, and the real capture of MCP failures moves to
  009 (S-04). That is a change to the index; ask the owner.
- **The `error` text per tool** was not captured live before 003. Fixtures use made-up texts.
- **Refusal texts** follow the host's wording and change with releases. A missed refusal is stored as a
  failure and can later mislead pairing (018). Keep the table in one place with the version seen.
- **How a payload marks a declined or interrupted call**: `is_interrupt` is from the host's page (web). If
  the phase 0 result shows that no `PostToolUseFailure` fires at all for a declined call, rule 6 has nothing
  to do and stays as a guard.
- **Proposed rules**: the "every statement, a leading `cd` aside" rule, the `lobe_` prefix, and the key of a
  worktree whose main repository lies outside the client folder. The MCP server that would carry `lobe_`
  tools ships in 062; until then that rule matches nothing.
- **The grep line.** The requirements say it is "recorded as `ok` (S-03)" and do not say what its `exit`
  holds. This file keeps the stated 1. If 008 decides otherwise, that feature changes it.
- **Nested clones and submodules** are keyed by their own folder and have no graph behind the key (S-01
  risks). The event is still logged.

## Result of the build, the review and the tester run

Built on 2026-11-09 by four Haiku builders from written briefs (the tokenizer, the worktree key, every tool, the
skip rules), reviewed as one change by a Sonnet reviewer, then fixed in one round by a Haiku builder and rerun by
the main session.

- Brief check (task 1): the code fit. Claude Code's hooks page says a declined call fires `PermissionDenied` and a
  cancelled tool fires no failure event, so rules 5 and 6 are guards for hosts or versions that report a refusal
  as a failure. Which event carries a failed MCP call is still from the page only; the owner's capture session
  settles it.
- The review found one blocking bug that no test had caught: `tq lobe status 2>&1` split at the `&`, so the
  memory's own command would have been recorded. Redirections are now stripped before the own-tool and grep rules
  (`shellwords.StripRedirects`). It also found that a skipped call still created the client's store (the skip
  decision now runs before the store opens), a worktree of a bare repository keyed wrongly (the common folder must
  be named `.git`), and the missing versions in the refusal table (the source names none; the comment says so).
- Tests: the whole repository is green on Windows (`go vet`, `go build`, `go test ./...`); no plugin change.
  `TestLobeShellwordsMatchTheGuard` runs 54 shared cases through both tokenizers; no non-test file under
  `internal/guard` changed.
- Tester brief: 9 of 9 on Windows 11 in Git Bash against a program built from the working tree. After the four
  own-tool and two skip payloads, acme's store held only its three files (`store.json`, `lock`, the log).

Known narrow cases, stated in the code: the grep rule reads the first statement that is not a `cd`, so
`grep x f; npm test` with `Exit code 1` is `ok`; the redirect strip ignores quotes, so `grep "x>y" f` loses `>y`
for those two rules only; a pipe after the memory's own command (`tq lobe status | head`) is recorded, because
every statement must be the memory's own.

Left open: CI on Linux and macOS; the owner's person checks (a failed edit, a refused call, an interrupted call,
an MCP failure); the capture session, which decides whether MCP failures arrive on `PostToolUseFailure`.

## Result of the owner's real session and the capture, 2026-11-09

In a yduqs repository, with the checkout's plugin and the development build as `tq`:

- `git push` rejected: one line, `PowerShell`, `fail`, `exit` 1, with the repository's key. Correct.
- `sleep` interrupted with Esc: no line. Correct.
- `ls` on a missing folder: one line with the repository's key. Correct.
- `grep` with no match: Claude used its own Grep tool, which succeeded, so nothing was written. Correct, but the
  shell grep rule was not exercised live; the fixtures cover it.
- **A failed edit wrote nothing, and no code of this feature can change that.** The capture session settled it
  (`docs/prd/spikes/phase0-q2-claude-code-hooks.md`): when `old_string` is not in the file, Claude Code rejects
  the call at its input check, before any hook runs. No `PreToolUse`, no `PostToolUseFailure`, nothing. The
  failure exists only in the session transcript, as a `tool_result` with `is_error` true. This feature still
  records an edit that fails while running, and every other tool's failure that reaches the hook.

This is a gap in the owner's requirement "capture every tool error": failures rejected at Claude Code's input
check reach no hook. The way to close it is to read the transcript, whose path every hook payload carries, at a
later event (for example `Stop`) and record each `tool_result` with `is_error` true that no hook event
covered. No feature in the index does that for live sessions yet (035 reads old transcripts). Where it goes is
the owner's call.

CI green on Linux, macOS and Windows for commit `caa90d03` on 2026-11-10.

Not yet seen live: a declined call (the owner's sessions run in `bypassPermissions`), any MCP failure, a failed
`WebFetch`.
