# 009: A failure that looks like a success is caught

**Status** built · **Stage** Sense · **Entries** S-04 (for Claude Code); S-03 (step 2, the call into S-04)
· **Needs** 006, 008 · **Feeds** 010, 018

## Result

Some failures reach the lobe marked as successes: a pipe such as `npm test | tail -5` reports the exit code of
`tail`, a test runner prints failures behind a wrapper that exits 0, and an MCP tool returns a normal result
with an error flag. Such a call no longer lands in the log as `ok`. It lands as `hidden-fail`, with its full
input, the failing part of the output as `error`, and a fingerprint.

From the sandbox of [002](002-sandbox.md), with acme's login exported and `ACME` set to acme's folder:

```bash
printf '%s' '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"'"$ACME"'","tool_name":"Bash",
"tool_use_id":"toolu_01","tool_input":{"command":"npm test 2>&1 | tail -5"},"tool_response":
{"stdout":"FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n","stderr":""}}' |
  tr -d '\n' | tentaqles lobe-hook claude post-tool-use
```

prints nothing and adds one line with `"kind":"hidden-fail"`, an `error` that starts with
`FAIL src/cart.test.ts`, and `"fp":"fp_90da432fb402b004"`.

## Scope

**In**

- `internal/lobe/sense/hidden.go`: `Hidden(tool, input, response) (errText string, hidden bool)`, pure: no
  disk, no clock. `pipes.go`: `hidesExit(command string) bool` and `firstProgram`.
- The three rules of S-04: A (an error flag, any tool), B (a test runner's own summary, shell tools) and C (a
  hidden exit code, `Bash` only), with the reading window of step 1.
- The call from `sense.FromClaudePostTool`, before the response is dropped (S-03 step 2).
- The `hidden-fail` event: full input, `error` from the deciding line, `fp` from
  [006](006-error-fingerprints.md), and the outcome with `Kind` `hidden-fail`.
- The widening of S-03 step 5: a success after a `hidden-fail` of the same tool in the same session keeps its
  input in full, as after a `fail`.

**Out**

- The answer to a hidden failure in the same hook: a starter rule's fix
  ([015](015-starter-rule-after-failure.md)) and the lesson for this fingerprint
  ([020](020-lesson-after-failure.md)). Here the event still prints nothing.
- E-02's verdict by this `kind` ([023](023-fires-and-verdicts.md)).
- Codex, where S-02 hands the body of a call that exited 0 to `Hidden`
  ([010](010-shell-failure-codex.md)). `Hidden` is written so that 010 calls it unchanged.
- Pairing a hidden failure with its repair ([018](018-repair-pairing.md)) and the backfill's use of `Hidden`
  on old transcripts ([035](035-backfill.md)).
- PowerShell pipelines: rule C reads `Bash` commands only (S-04, Risks). A recorded limit, not a later
  feature.
- Measuring wrong `ok` and wrong `hidden-fail` readings ([041](041-enforcement-accuracy.md), M-05).

## What it builds on

- [002](002-sandbox.md): the sandbox, the standard tester setup and its `lobeset` helper.
- [003](003-shell-failure-claude-code.md): `internal/lobe/scrub`; `sense.Record`; the written result
  `docs/prd/spikes/phase0-q2-claude-code-hooks.md`, with its rows for an MCP error result and an unknown MCP
  tool.
- [004](004-any-tool-failure.md): `internal/lobe/shellwords`, the lobe's own word splitter, with its shared
  case file `testdata/cases.json` and `TestLobeShellwordsMatchTheGuard`; the program lookup of `own.go`. 004
  also says that if every failed MCP call turns out to arrive on `PostToolUse`, its MCP rows move here: rule
  A is then the main path for MCP failures, not a side path.
- [005](005-status-and-capture-switch.md): `tentaqles lobe status`, whose `hidden_fail` count stays 0 until
  this feature.
- [006](006-error-fingerprints.md): `fingerprint.Of`. `Record` already gives every `fail` its `fp`; this
  feature makes it do the same for `hidden-fail`.
- [007](007-duplicate-filter.md): `eventlog.Check`. A repeated hidden failure becomes a reference line with
  `kind` `hidden-fail`.
- [008](008-successes.md): the plugin's `PostToolUse` entry, the `post-tool-use` event,
  `sense.FromClaudePostTool` with its fixed order (background check, drop, tail, projection, scrub), the
  full-input rule after a `fail`, and the rows 008 added to the phase 0 result for `PostToolUse` payloads.

## What it leaves for later features

- `sense.Hidden(tool, input, response)`: 010 (Codex) and 035 (the backfill reads a call as failed when
  `is_error` is true or `Hidden` says so, C-07).
- `hidesExit` and `firstProgram` in `pipes.go`: 010 through `Hidden`; 015 and 039 build their own program
  lookups on `shellwords`, not on these.
- The `kind` `hidden-fail` in the log and in `Outcome.Kind`: 015, 018, 020, 023 and 028 treat it as a
  failure; "whatever event carries a `fail` or `hidden-fail` outcome also carries the answer to it" (Sense
  page, "Where Sense ends").
- The one pattern set for "this output says it failed". C-02 judges a check by the event's `kind`, never by a
  second set of text patterns (C-02), so 028 reads what this feature decided.

## How it works

1. **Where it runs.** In `sense.FromClaudePostTool`, after the background check and before `tool_response` is
   dropped (008, steps 5 and 6). `Hidden` reads the response once. If no rule fires, the call goes on as a
   success, exactly as in 008. The skip rules ran before: with `capture` off or one of the lobe's own tools,
   `Hidden` is never called.
2. **Result text** (S-04 step 1). `tool_response` itself when it is text. For a shell result object, `stdout`
   and `stderr`, each when not empty, joined by a line break. For a list of parts, the `text` values joined
   by line breaks. For any other object, its `content` read the same way. Of a text longer than 4000
   characters, only the first 1500 and the last 2500 are read. A character is one Unicode code point.
3. **Rule A, any tool** (S-04 step 2). The response is an object whose `isError` or `is_error` is true.
4. **Rule B, shell tools** (S-04 step 3). A test runner's own summary; the first format found decides, each
   with its zero guard. The formats are listed there: the `Test Files`, `Tests` and `Test Suites:` lines, TAP,
   `Ran N tests in`, `test result:`, Go output, and the `N failed`, `N errors`, `N failing` counts. Read the
   list in S-04; do not shorten it. Rule B, like rule C, skips a command whose first program only reads
   (owner, 2026-11-10).
5. **Rule C, `Bash` only** (S-04 step 4). It runs only when the command hides its exit code: its last
   top-level pipe segment starts with `tail`, `head`, `less`, `more` or `cat` (Trail-code's list), `grep` or
   `tee` (proposed), or the command ends in `|| true`. It is skipped when the first program only reads (the
   list in S-04 step 4, proposed). Then one anchored line decides (the patterns of S-04 step 4). A command
   with `<<` counts as not hiding its exit code.
6. **In doubt, prefer `hidden-fail`** (S-04, "How to build it"): a wrong `ok` can produce a wrong lesson.
7. **The event** (S-04 step 5). `kind` `hidden-fail`, `host` `claude`. It keeps the full input, as a failure
   does: no projection. Its `error` is the text from the deciding line on, at most 20 lines (proposed), with
   line breaks at its end removed. The deciding line is: for rule A, the first line of the result text; for
   rule B, the first line starting `FAIL`, `--- FAIL` or `FAILED` when there is one, else the summary line;
   for rule C, the line that matched. No `exit` is written unless the host states one.
8. **Then `Record`**: S-05 scrubs input and error, S-08 gives the `fp` from the scrubbed `error`, S-06 checks,
   S-07 appends. The rest of the response is dropped as in 008: only the `error` cut survives, scrubbed.
9. **The outcome** has `Kind` `hidden-fail` and its `FP`. In this feature nothing reads it.
10. **After a hidden failure** the next success of the same tool in the same session keeps its full input
    (S-03 step 5 names `fail` or `hidden-fail`).
11. **Cost.** `Hidden` is capture work on the hook that runs after every tool call. It reads at most 4000
    characters whatever the response size, and its patterns compile on first use, not at package level (core
    page 5.1).

Where this differs from Trail-code, on purpose (S-04): one pattern set, not two that disagree; every rule C
pattern anchored at the start of a line, so a README that mentions `Permission denied` in a sentence does not
count; and `Tests 0 failed` is a pass.

## Where to read

| What | Where |
|---|---|
| Requirements | [S-04](../prd/lobe-sense.md), whole entry; [S-03](../prd/lobe-sense.md) steps 2 and 5; "Where Sense ends" at the top of the [Sense page](../prd/lobe-sense.md), the item "Order"; [core page 5.2 and 5.4](../prd/vertical-lobe.md); [F-02](../prd/lobe-foundation.md) "How to build it", the `shellwords` item |
| Source to port | Trail-code (MIT): [`src/trace/failure.ts` L46](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L46): head 1500 and (L47) tail 2500; L51 applies the window. [`src/trace/failure.ts` L10](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L10): `FAILED_TEXT`; rule C keeps the anchored part only (port, stricter). [`src/trace/checks.ts` L174](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/checks.ts#L174): the summary rule with its zero guard; L177 to L184 are the other runner formats; L185: with no summary, a piped command never passes. [`src/trace/shell.ts` L130](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/shell.ts#L130): the filter programs `tail`, `head`, `less`, `more` and `cat` |
| Weakness not to copy | [`src/trace/failure.ts` L41](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L41): `Tests\s+\d+ failed` has no zero guard, so `Tests 0 failed` reads as a failure |
| Web, not machine-checked | MCP specification, [tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools): a tool's own error is a normal result with `isError` true (rule A) |
| Existing tentaqles code | `internal/lobe/sense/claude.go` (`FromClaudePostTool`, from 008); `internal/lobe/shellwords` and its `testdata/cases.json` (from 004); `internal/guard/gitinv.go` L242 (`splitWords`, the splitter the lobe copied and never imports) |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits. Look at: the order inside
   `FromClaudePostTool`; whether `internal/lobe/shellwords` exists and tracks quotes, backticks and `$(`; how
   `Record` decides which kinds get an `fp`; how 008 wrote the full-input rule. The owner approves changes
   first.
2. **Phase 0 facts.** No new capture is planned. Read `docs/prd/spikes/phase0-q2-claude-code-hooks.md` as 003
   and 008 left it and note two facts in it under a heading for this feature: which event carries an MCP
   result with an error flag, and the form of a `Bash` and a `PowerShell` `tool_response` (the field names
   for standard output and standard error). If either is missing, extend the capture scripts under
   `spike/hook-capture/`; the owner runs them in one client they name and hands back counts and field names,
   never content. The fixtures use made-up values.
3. **The pipe reader.** Create `internal/lobe/sense/pipes.go` with `hidesExit` and `firstProgram` on
   `shellwords`. Proof: `TestLobeHiddenPipes`. Can run beside task 4.
4. **The rules.** Create `internal/lobe/sense/hidden.go` with `Hidden`: the result text, the window, rules A,
   B and C, the deciding line and the 20 line cut. Both files are ports of Trail-code: each starts with the
   header line F-07 asks of a ported file, and `NOTICE` lists them. Proof: `TestLobeHiddenFixtures`,
   `TestLobeHiddenMcpFlag`, `TestLobeHiddenWindow`.
5. **Wire it in.** Call `Hidden` from `FromClaudePostTool`; build the `hidden-fail` event; make `Record`
   fingerprint it; widen the full-input rule to `hidden-fail`. Files: `internal/lobe/sense/claude.go`,
   `sense.go`. Proof: `TestLobeSenseHiddenFailEvent`, and `TestLobeSenseOkFullAfterFailure` widened with a
   `hidden-fail` case.
6. **Docs.** `docs/reference/commands.md`: the `lobe-hook` section names the third `kind`. One line in
   `CHANGELOG.md` under `[Unreleased]`. The plugin does not change. No "Must not trade off" line is added.
7. **Tester run.** A fresh agent runs the Evaluation below on the built binary.

## Tests that stay in the repository

- `TestLobeHiddenFixtures`: 15 hidden failures and 15 passes that mention failure words, 30 of 30 (proposed).
  Among them: ` Test Files 1 failed | 12 passed (13)` is a failure; `Tests 0 failed` is a pass;
  `FAIL a.test.ts` with `0 failed` on another line is a failure.
- `TestLobeHiddenMcpFlag`: `{"isError":true,"content":[{"type":"text","text":"boom"}]}` is hidden, with the
  error `boom`.
- `TestLobeHiddenWindow`: a `FAIL` line at character 2000 of a 10,000 character output is not seen (a recorded
  limit). `Hidden` on a 1 MiB response returns in under 5 ms (proposed).
- `TestLobeHiddenPipes` (new name, not in the requirements): `hidesExit` is true for `npm test | tail -5`,
  `make 2>&1 | tee build.log`, `pytest || true` and `go test ./... | grep -v ok`; false for `npm test`,
  `echo "a | tail"`, a command with `<<`, and `npm test | tail -5 && echo done`. `firstProgram` gives `cat`
  for `cat deploy.log | tail -3` and `git` with `log` for `git log | head`. 10 of 10.
- `TestLobeSenseHiddenFailEvent` (new name): the three payloads of tester checks 1, 3 and 4 each write one
  line with `kind` `hidden-fail`, the full input, an `error` of at most 20 lines and the `fp` that
  `fingerprint.Of` gives for it; the response text outside that `error` is in no file under the `lobe` folder;
  `tentaqles lobe status --json` then counts 3 under `hidden_fail`.
- `TestLobeSenseOkFullAfterFailure` (from 008, widened): after a `hidden-fail` on the same tool in the same
  session, a success holds the full input.
- House rules kept green: `TestLobeSenseOkDropsOutput`, `TestLobeScrubParity`, `TestLobeHookAlwaysExitsZero`,
  `TestLobeHookServesOnlyItsLogin`, `TestLobeSenseOkSkips`, `TestLobeShellwordsMatchTheGuard`,
  `TestLobeGuardNeverImportsLobe` (no pattern compiled at package level), `TestLobeImportsOnlyTheFour`, and
  every test under "Must not trade off" in `CLAUDE.md`, unedited.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows) and an empty
working folder. It uses no network and never reads or writes the real `~/.tentaqles`. Setup:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
lobeset() {  # lobeset <client> <key> <value>: a fixture edit of a sandbox manifest
  m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"
}
ACME="$W/clients/acme"; ID="$TQ_HOME/identities"
LOG="$ID/acme/lobe/events/events-$(date -u +%Y%m%d).jsonl"
A250="$(printf 'a%.0s' $(seq 1 250))"
export CLAUDE_CONFIG_DIR="$ID/acme/claude"; cd "$ACME"
ok() { printf '%s' "${1//ACME/$ACME}" | tentaqles lobe-hook claude post-tool-use; echo "exit=$?"; }
```

Stop and report FAIL if a line starts with `SETUP FAIL`. Run the checks in order, within 5 minutes. Every `ok`
call must print only `exit=0`: no error below matches a starter rule and no lesson exists, so later features
add nothing to these outputs.

1. A piped test run. Expect 1 line in `LOG` with `id` `evt_9bde2e09`, `kind` `hidden-fail`, `input`
   `{"command":"npm test 2>&1 | tail -5"}`, an `error` that starts with `FAIL src/cart.test.ts`, and `fp`
   `fp_90da432fb402b004`.

   ```
   ok '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_01","tool_input":{"command":"npm test 2>&1 | tail -5"},"tool_response":{"stdout":"FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n","stderr":""}}'
   ```

2. A passing run with a zero count. Expect 2 lines. The second has `id` `evt_8618533e` and `kind` `ok`.

   ```
   ok '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_02","tool_input":{"command":"cargo test"},"tool_response":{"stdout":"test result: ok. 12 passed; 0 failed\n","stderr":""}}'
   ```

3. An MCP result with the error flag. Expect 3 lines. The third has `id` `evt_7fe44cf7`, `kind`
   `hidden-fail`, `input` `{"n":4411}`, `error` `widget 4411 is not frobnicated` and `fp`
   `fp_48622a622f4928cc`.

   ```
   ok '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"mcp__acme__ping","tool_use_id":"toolu_03","tool_input":{"n":4411},"tool_response":{"isError":true,"content":[{"type":"text","text":"widget 4411 is not frobnicated"}]}}'
   ```

4. A hidden exit code with an anchored error line. The JSON of check 1 with `toolu_04`, the command
   `git push 2>&1 | tail -3` and the stdout `fatal: no remote\n`. Expect 4 lines. The fourth has `id`
   `evt_20ed2862`, `kind` `hidden-fail` and `error` `fatal: no remote`.
5. A reading program in front. The JSON of check 1 with `toolu_05`, the command `cat deploy.log | tail -3` and
   the stdout `error: disk full\n`. Expect 5 lines. The fifth has `id` `evt_5cdeec27` and `kind` `ok`.
6. A sentence is not an error line. The JSON of check 1 with `toolu_06`, the command
   `./deploy.sh 2>&1 | tail -3` and the stdout `note: a Permission denied error would stop the run\nall good\n`.
   Expect 6 lines. The sixth has `kind` `ok`.
7. Not `Bash`. The JSON of check 4 with `toolu_07` and the tool `PowerShell`. Expect 7 lines. The seventh has
   `kind` `ok`: rule C reads `Bash` only.
8. The repeat. The JSON of check 1 with `toolu_08`. Expect 8 lines. The eighth has `dup` `evt_9bde2e09`,
   `kind` `hidden-fail`, `fp` `fp_90da432fb402b004`, no `input` and no `error`.
9. The success after a hidden failure. Expect 9 lines. The ninth has `kind` `ok` and an `input` whose `q` is
   the letter `a` exactly 250 times: check 3 was a failure of this tool in this session.

   ```
   ok '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"mcp__acme__ping","tool_use_id":"toolu_09","tool_input":{"q":"'"$A250"'"},"tool_response":"pong"}'
   ```

10. Only the failing part is kept. The JSON of check 1 with `"session_id":"s2"`, `toolu_10` and the stdout
    `SECRET-ROW-7731 loaded\nFAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n`. Expect 10
    lines; the tenth has `kind` `hidden-fail` and an `error` that starts with `FAIL src/cart.test.ts`.
    `grep -r -c 'SECRET-ROW-7731' "$ID/acme/lobe" | grep -v ':0$' | wc -l` prints `0`.
11. Capture off, another login. `lobeset acme capture off`, then the JSON of check 1 with `toolu_11`: still 10
    lines. `lobeset acme capture on`. Then the JSON of check 1 with `toolu_12` under
    `CLAUDE_CONFIG_DIR="$ID/globex/claude"` for that one command: only `exit=0`, still 10 lines, and
    `ls "$ID/globex/lobe/events" 2>/dev/null | wc -l` prints `0`.

There are no person checks. PASS needs 11 of 11.

## Done when

- [ ] `TestLobeHiddenFixtures`, `TestLobeHiddenMcpFlag`, `TestLobeHiddenWindow`, `TestLobeHiddenPipes`,
  `TestLobeSenseHiddenFailEvent` and the widened `TestLobeSenseOkFullAfterFailure` are in the repository and
  green in CI (`go vet ./...`, `go test ./... -count=1`, `go build ./...`) on the three systems
- [ ] the tester brief passes, 11 of 11
- [ ] the house rules hold: `TestLobeSenseOkDropsOutput`, `TestLobeHookAlwaysExitsZero`,
  `TestLobeHookServesOnlyItsLogin`, `TestLobeShellwordsMatchTheGuard`, `TestLobeGuardNeverImportsLobe`, and
  every test under "Must not trade off" in `CLAUDE.md` unedited
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated; `NOTICE` lists the two ported files
- [ ] the owner has seen `npm test | tail -5` with failing tests logged as `hidden-fail`

## Risks and unknowns

- Which hook event carries an MCP result with `isError` true is not documented (S-04, Risks). Rule A costs
  nothing if unused; task 2 writes down what 003's capture found.
- The field names of a shell `tool_response` (`stdout`, `stderr`) come from use, not from a schema. Task 2
  confirms them. If Claude Code hands the output as plain text, step 2's first form already covers it.
- A failure line in the middle of a long output is outside the window, as in Trail-code. PowerShell pipelines
  are not covered. The rule C lists (`grep`, `tee`, the reading programs) are ours and unmeasured; the
  backfill of 035 is the first real corpus to test them on (S-04, Risks).
- From 023 on, E-02 judges a rule by the `kind` decided here. A failure read as `ok` gives a good rule a false
  alarm, and a success read as `hidden-fail` gives a wrong rule a hit. 041 counts both.
- Check 7 pins a limit, not a wish: a hidden failure in a PowerShell pipeline is stored as `ok`. If the owner
  wants PowerShell covered, that is a change to S-04 first.
- Rule B's count (`N failed`, `N errors`, `N failing`) reads the output of every shell command that does not only
  read, so text that merely names a count is a `hidden-fail`: `git commit -m 'fix 3 errors'` echoes its message,
  and so do `gh pr view` or a script that prints a report. A command that starts with a reading program
  (`echo "== run ==" && npm test | tail -5`) is not judged at all, since only a leading `cd` is skipped (owner,
  2026-11-10). Both are recorded limits from the review; 041 measures them.
- Trail-code's remark that most failed commands in its authors' sessions exited 0 because of a pipe is their
  observation, not our measurement. No number about hidden failures is claimed from this feature.
- The fingerprint in check 4 is not stated, because S-08 prints no vector for that text. `fp_90da432fb402b004`
  and `fp_48622a622f4928cc` are S-04's and S-01's own vectors.

## Result of the build, the review and the tester run

Built on 2026-11-10 by two Haiku builders (the pipe reader and the three rules; the wiring and docs), reviewed as
one change by a Sonnet reviewer.

- **Brief changes.** `shellwords` gained `Parts`, a split that keeps which separator joined each part, since
  `Statements` drops them and rule C needs the last pipeline. The owner decided that rule B, like rule C, skips
  a command whose first program only reads (leading `cd` statements skipped), so a commit title or an old log
  read with `git log` or `cat` is never a hidden failure.
- **Phase 0 facts** (task 2, on the [phase 0 page](../prd/spikes/phase0-q2-claude-code-hooks.md)): the shell
  result is an object with `stdout` and `stderr`. A fourth capture with a made-up MCP server
  (`spike/hook-capture/mcp_fail_server.py`, at the owner's choice) showed both an `isError` result and a
  JSON-RPC error arrive on `PostToolUseFailure`, which 004 already records. Rule A is kept as written; on
  Claude Code it is a side path that the capture never saw fire.
- **The review** found no blocking defect. Fixed from it, with tests: a decimal such as `0.5 failed` read as a
  count; a flag with no text gave an empty `error` (now a fixed text). Recorded as limits under Risks: a count
  echoed by a command that does not only read (`git commit -m 'fix 3 errors'`), and a command that starts with
  a reading program.
- Also in this change: the selftest command test kills a run still going after 8 minutes, after one was left
  spinning for hours past its test binary's timeout.
- Tests: `go vet` and `go build` clean; `sense`, `shellwords` and `test/integration` green. The full suite on
  Windows ran while an unrelated training job held the CPU near 95%: the store-lock, cutoff and timing tests
  failed there in the same way on the last green commit, `801b3974`, so CI decides.
- Tester brief: 11 of 11 on Windows 11 in Git Bash against a program built from the working tree.

The owner's check, 2026-11-10, in a real yduqs session with this checkout's plugin: a `Bash` call of
`printf 'FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n' | tail -5` left one line with
`"kind":"hidden-fail"`, the full command and its `description` as input, an `error` from the `FAIL` line on and
`"fp":"fp_90da432fb402b004"`; `tentaqles lobe status` counted `hidden_fail 1`.
