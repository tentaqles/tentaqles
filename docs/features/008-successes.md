# 008: Successes are recorded, minimally

**Status** not started · **Stage** Sense · **Entries** S-03; F-03 (the `PostToolUse` plugin entry); F-02 (the
`post-tool-use` event, its first step) · **Needs** 004 · **Feeds** 009

## Result

After any tool call succeeds in a client's Claude Code session, the client's event log gains one small `ok`
line: which tool ran, when, and the part of its input that identifies the call. The tool's output is in no
file. Before this feature the log knew only failures; now it knows "this failed, then this worked".

From the sandbox of [002](002-sandbox.md), with acme's login exported and `ACME` set to acme's folder:

```bash
printf '%s' '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"'"$ACME"'","tool_name":"Write",
"tool_use_id":"toolu_01","tool_input":{"file_path":"'"$ACME"'/notes.txt","content":"CONTENT-MARKER-1"},
"tool_response":{"filePath":"'"$ACME"'/notes.txt","note":"OUTPUT-MARKER-2"}}' | tr -d '\n' |
  tentaqles lobe-hook claude post-tool-use
```

prints nothing and adds one line with `"kind":"ok"`, `"tool":"Write"` and an `input` that holds only the file
path. A search of the client's `lobe` folder for either marker finds nothing.

## Scope

**In**

- The lobe's `PostToolUse` entry in `plugin/hooks/hooks.json`: every tool, timeout 5 (F-03 step 2).
- The `post-tool-use` event of `tentaqles lobe-hook claude`, with one step: Sense.
- `sense.FromClaudePostTool` in `internal/lobe/sense/claude.go` and `sense.Project` in `project.go`.
- The rules of S-03: the output is dropped, the input is projected, a command that only went to the
  background writes nothing, and the success that follows a failure keeps its scrubbed input in full.
- The `ok` line that [004](004-any-tool-failure.md) already writes for a search that found nothing (S-01 step
  4) now takes the same path as every other `ok` line: the exception, the projection, the scrub.
- The skip rules of S-01 steps 2 and 3 on the new event, the lobe's own tools included.
- The part of phase 0 question 2 that S-03 asks for: the field names of a `PostToolUse` payload (task 2).

**Out**

- Reading `tool_response` for a failure the host reported as a success ([009](009-hidden-failures.md)).
  Until 009 such a call is stored as `ok`.
- Codex, whose successes arrive through S-02 ([010](010-shell-failure-codex.md)).
- Everything other pages run after Sense on this event: E-02's verdict
  ([023](023-fires-and-verdicts.md)), F-05's tamper check ([034](034-teammate-lesson-intake.md)), G-08's mark
  ([048](048-graph-stays-current.md)), G-09's list ([049](049-blast-radius.md)) and E-05's note
  ([051](051-argument-check.md)). Here the event prints nothing, ever.
- Pairing a failure with the success that repaired it ([018](018-repair-pairing.md)).
- The fill level that stops `ok` lines first when a day's file reaches 8 MiB (S-07 step 4), with
  `TestLobeLogCaps` ([011](011-retention-and-sweep.md)). Until then nothing bounds a day's file.
- Measuring what a line for every success costs (M-06 step 7, [012](012-hook-latency-and-stall.md)), and the
  fallback of logging only some successes, which would be a change to the core page.

## What it builds on

- [002](002-sandbox.md): the sandbox, the standard tester setup and its `lobeset` helper.
- [003](003-shell-failure-claude-code.md): `plugin/scripts/lobe_hook.sh`, which already accepts all seven
  event names; the plugin's `PostToolUseFailure` entry; the changed pinned test
  `test_the_plugin_holds_only_the_guard_hook` with its list of lobe events, exactly `["PostToolUseFailure"]`
  so far; the `test_lobe_hook_*` tests; `internal/lobe/hook` (`Main`, the step table, the client rule, the
  cutoffs, the watchdog), which already knows the event name `post-tool-use` and gives it no step;
  `internal/lobe/store`; `internal/lobe/scrub` (`Input`, `Clean`); `internal/lobe/eventlog`;
  `internal/lobe/sense` (`Record`, `Outcome`, `place.go`, `calltext.go`, the reader of `tool_input` as an
  object or as text); the written result `docs/prd/spikes/phase0-q2-claude-code-hooks.md`, which has one row
  for a successful command.
- [004](004-any-tool-failure.md): failure lines for every tool, and so the tool names the projection is for;
  `internal/lobe/shellwords`; `sense/own.go` with `IsOwnTool` (name proposed there); the rule that a search
  that found nothing is written as `ok`, with the `exit` the host stated and, so far, its full input.
- [005](005-status-and-capture-switch.md): the `capture` switch; `eventlog.Tail`; `tentaqles lobe events`;
  `tentaqles lobe status`, whose count of `ok` events now grows with every call.
- [006](006-error-fingerprints.md): `fp` on failure lines. An `ok` line has none.
- [007](007-duplicate-filter.md): `eventlog.Check` (an `ok` line passes through it unchanged) and the outcome
  states `ref` and `replay`.

## What it leaves for later features

- The plugin's `PostToolUse` entry and the `post-tool-use` row of the event table: 009, 023, 034, 048, 049 and
  051 add their steps after Sense, in the order of F-02 step 2.
- `sense.FromClaudePostTool`: 009 puts its check in front of the place where the response is dropped.
- `sense.Project(tool string, input json.RawMessage) json.RawMessage`: 010 (Codex successes), 018 (C-01
  compares a failure and a success key by key) and 037 (E-03's replay reads projected inputs).
- `ok` lines: 018, 023, 028, 035 and 058 read them.
- The rule "a success after a failure of the same tool in the same session keeps its full input": 018 depends
  on it, and 009 widens "failure" to `hidden-fail`.

## How it works

1. **The plugin entry.** `plugin/hooks/hooks.json` gains a `PostToolUse` group with one handler:
   `bash "${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh" post-tool-use`, `"timeout": 5`, no matcher. The guard
   has no group on this event. The guard's two entries stay byte for byte. The wrapper needs no change: it
   has known the event since 003. The change ships as F-03 says: a line in `plugin/CHANGELOG.md`, then the
   release tag and sha pin (`TestRootMarketplacePin`).
2. **The engine.** `hook.Main` treats the event as "capture, then inject", cut off at 1.5 seconds (F-02 step
   2). Everything of F-02 steps 1 to 5 runs as it does for the failure event: the watchdog, the 1 MiB read,
   the client from the payload's `cwd`, the login rule, the settings, the store. In this feature the event has
   one step, Sense, which prints nothing. The first thing printed on this event comes with a later feature.
3. **The payload** (S-03 step 1). Read `session_id`, `cwd`, `tool_name`, `tool_use_id`, `tool_input` and
   `tool_response`. A missing `tool_name` is `unknown` and a missing `session_id` an empty `session` (S-01).
4. **Skips.** Nothing is written when `capture` is `off`, or when the tool is one of the lobe's own (S-01
   step 3, `IsOwnTool` from 004): a shell call whose every statement runs `tentaqles lobe ...`, or an MCP tool
   whose own name starts with `lobe_`. There is then no line and no outcome.
5. **A background start** (S-03 step 2). When the result only says the command went to the background, no
   event is written and there is no outcome: it started, it did not yet work. The two texts Claude Code
   writes for this are in the Trail-code source named below; keep them in one table with the version they
   were seen on.
6. **The output is dropped** (S-03 step 3). After step 5 `tool_response` is let go. No later step sees it and
   no byte of it is written. Feature 009 adds its check between steps 5 and 6.
7. **The exception** (S-03 step 5, decision A-18). Before projecting, read the tail of today's file with
   `eventlog.Tail` (64 KiB, proposed), with no lock. If it holds an earlier `fail` line of the same `session`
   and `tool`, the input is kept in full. If the tail cannot be read, the input is projected (owner, 2026-11-10: keeping it in full would store every
   success in full, a Write's whole content included, whenever the log's end is unreadable). An empty
   `session` never matches (this file's reading; S-03 does not say). A reference line in the tail counts: it
   carries `session`, `tool` and `kind`.
8. **The projection** (S-03 step 4), otherwise. One table drives it:

   | Tools | Fields kept |
   |---|---|
   | `Bash`, `PowerShell` | `command` |
   | `Edit`, `Write`, `MultiEdit`, `NotebookEdit`, `Read` | `file_path` or `notebook_path` |
   | `Grep`, `Glob` | `pattern`, `path` |
   | `WebFetch`, `WebSearch` | `url` or `query` |
   | any other tool, MCP tools included | every top-level key; text cut to 200 characters (proposed); a nested value becomes `{}` or `[]` |

   A known tool that has none of its fields is treated as any other tool.
9. **The event.** `kind` `ok`, `host` `claude`, no `error`, no `fp`. `exit` is written only when the host
   states a code. `repo`, `cwd`, `id` and `at` as for a failure. `input` is compact JSON with sorted keys,
   the same form a failure has, so the two can be compared key by key.
10. **The rest of `Record`**: S-05 scrubs what is left (a command line can hold a token), S-06 checks for a
    replay or a repeat, S-07 appends. The order is fixed: background check, drop, tail, projection, scrub.
11. **A search that found nothing** (S-01 step 4, built in 004). It arrives on `PostToolUseFailure` and is
    written as `ok`. From this feature on that line goes through steps 7 to 10 like any other success: its
    `input` is the projection (for a shell tool the `command` alone) unless the exception of step 7 applies.
    Its `exit` stays as 004 wrote it.

### Brief check (task 1), 2026-11-10

- The pinned plugin test lists `PostToolUseFailure` and `Stop` (feature 063 added `Stop` after this brief);
  this feature makes it `PostToolUseFailure`, `PostToolUse` and `Stop`.
- The hook's steps are `S-01` (`post-tool-use-failure`) and `S-01R` (`stop`); `post-tool-use` has none yet.
  The `capture` gate in `work()` already covers every event.
- The grep rule sits in `sense.FromClaudeFailure` (`grepFoundNothing`) and writes the full scrubbed input;
  the own-tool check is `sense.IsOwnTool(tool, input)`, called through `sense.Skip`.
- `tentaqles lobe status` and `lobe-hooks-missing` look only for a command naming `lobe_hook.sh`, so a new
  entry changes neither. `sense.Record` now also fingerprints (006) and runs the duplicate check (007); an `ok`
  line gets no `fp` and passes through the check unchanged.

## Where to read

| What | Where |
|---|---|
| Requirements | [S-03](../prd/lobe-sense.md), whole entry; [S-01](../prd/lobe-sense.md) steps 2 to 4; "Where Sense ends" at the top of the [Sense page](../prd/lobe-sense.md); [F-02](../prd/lobe-foundation.md) step 2, the `post-tool-use` row; [F-03](../prd/lobe-foundation.md) steps 2, 3 and 5 and "How to build it"; [core page 5.2 and 5.4](../prd/vertical-lobe.md); decision A-18 in [core page section 10](../prd/vertical-lobe.md) |
| Source to port | agentmemory (Apache-2.0): [`src/hooks/post-tool-use.ts` L40](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-use.ts#L40): the success path reads the input whole; L56 passes it on with no cap (port, changed). Trail-code (MIT): [`src/trace/run-calls.ts` L138](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/run-calls.ts#L138): the two texts Claude Code writes for a command that went to the background. [`src/trace/run-calls.ts` L114](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/run-calls.ts#L114): L246 is the rule for a grep that found nothing. [`src/claude/settings-merge.ts` L99](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/settings-merge.ts#L99): a hook entry at timeout 5 |
| Not taken, and a weakness | agentmemory: [`src/hooks/_capture-filter.ts` L60](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture-filter.ts#L60): output is kept up to 8000 characters by default; we keep none. [`src/hooks/post-tool-failure.ts` L55](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L55): a failure's input is stored as cut text while a success keeps the object |
| Host documents (web, not machine-checked) | <https://code.claude.com/docs/en/hooks>: an omitted matcher selects all tools |
| Existing tentaqles code | `plugin/hooks/hooks.json` (the guard's entries at L16 and L21); `plugin/tests/test_tq_hook.py` (the pinned test, L199 before 003 changed it; the matcher test, L189); `plugin/scripts/lobe_hook.sh` and `plugin/scripts/tq_hook.sh` L66 to L85 (how a wrapper finds the binary); `plugin/CLAUDE.md`; `internal/lobe/hook` and `internal/lobe/sense` from 003 and 004; `spike/hook-capture/` from 003 |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits. Look at: the list of lobe
   events in the pinned plugin test; the step table of `internal/lobe/hook`; where 004 put the grep rule and
   what `input` it writes; the name and shape of the own-tool check; what `tentaqles lobe status` and
   `lobe-hooks-missing` read from `hooks.json`. The owner approves changes first.
2. **Phase 0, question 2, the success side.** 003's result has one row for a successful command. S-03 needs
   more. Extend the capture scripts under `spike/hook-capture/` to record `PostToolUse` for a shell command,
   an edit, a write, a read, a search, a web fetch, an MCP call and a command started in the background. The
   owner runs them in one client they name and hands back the reducer's output: counts and field names with
   their JSON types, never content. Add to `docs/prd/spikes/phase0-q2-claude-code-hooks.md`: the field names
   of `tool_input` per tool; the form of `tool_response` per tool; whether any field states an exit code; how
   a background start is told apart. The builder writes the fixtures from those names, with made-up values.
   If the owner's run shows field names other than those in the table of step 8, stop and ask the owner
   before changing the table.
3. **The projection.** Create `internal/lobe/sense/project.go` with `Project`, one table, one test row per
   table row. Proof: `TestLobeSenseOkProjection`. Can run beside tasks 4 and 6.
4. **The success path.** Add `FromClaudePostTool` to `internal/lobe/sense/claude.go`: steps 3 to 10 above.
   Proof: `TestLobeSenseOkDropsOutput`, `TestLobeSenseOkFullAfterFailure`,
   `TestLobeSenseBackgroundStartWritesNothing`, `TestLobeSenseOkEveryCall`, `TestLobeSenseOkSkips` and the
   success half of `TestLobeSenseOwnTools`.
5. **The event.** Give the `post-tool-use` row of the host `claude` its step in the step table of
   `internal/lobe/hook`. Proof: `TestLobeHookAlwaysExitsZero` and `TestLobeHookServesOnlyItsLogin` with the
   event now doing work, and `TestLobeNoHooksSwitch`.
6. **The search rule.** Route 004's `ok` line through the success path. File: `internal/lobe/sense/claude.go`.
   Proof: `TestLobeSenseGrepNoMatch`, widened to check the projected `input`.
7. **The plugin.** Add the group to `plugin/hooks/hooks.json`; extend the list of lobe events in
   `test_the_plugin_holds_only_the_guard_hook` to `PostToolUseFailure` and `PostToolUse`; update
   `plugin/CLAUDE.md` where it lists the lobe's entries. Proof: the plugin suite
   (`PYTHONPATH=plugin python -m pytest plugin/tests -q`) and `TestLobeHookTimeoutsRegistered`. Can run beside
   tasks 3 to 6.
8. **Docs.** `docs/reference/commands.md`: the `lobe-hook` section says what `post-tool-use` now does and what
   an `ok` line holds. One line in `CHANGELOG.md` and one in `plugin/CHANGELOG.md`, both under
   `[Unreleased]`. No "Must not trade off" line is added.
9. **Tester run.** A fresh agent runs the Evaluation below on the built binary, with the repository's
   `plugin` folder to read.

## Tests that stay in the repository

- `TestLobeSenseOkDropsOutput`: a response that holds `OUTPUT-MARKER-7731` is processed, and the marker appears
  0 times in any file under the `lobe` folder.
- `TestLobeSenseOkProjection`: 9 tools, one per name group of the table plus one MCP tool. 9 of 9 match.
- `TestLobeSenseOkFullAfterFailure`: after a failure on the same tool in the same session, a success holds the
  full input. The same success in another session holds the projection.
- `TestLobeSenseBackgroundStartWritesNothing`: 0 lines.
- `TestLobeSenseOkEveryCall`: 100 successful calls in a session with no failure write 100 lines. On a fixture
  of 100 mixed successful calls, the median `ok` line is under 300 bytes (proposed).
- `TestLobeSenseGrepNoMatch` (from 004, widened): `grep` with `Exit code 1` and no other text gives `ok` with
  the projected `input`; with `Exit code 2`, `fail`.
- `TestLobeSenseOwnTools` (from 004, widened): the four own-tool cases also write 0 lines as successes.
- `TestLobeSenseOkSkips` (new name, not in the requirements): bad JSON, no client, another client's login
  folder, no login variable and `capture: off` on `post-tool-use`: 0 lines in any store, exit 0 and empty
  stdout in all 5 cases.
- `TestLobeHookTimeoutsRegistered`: every lobe entry in `hooks.json` has timeout 5 and the guard's two entries
  still have 15.
- `TestLobeNoHooksSwitch`: with `TQ_LOBE_NO_HOOKS=1` the new event writes 0 bytes to the store and to stdout.
- Plugin suite: `test_the_plugin_holds_only_the_guard_hook` (the guard's two entries present and unchanged
  with timeout 15; every other entry is `lobe_hook.sh <event>` with timeout 5, over exactly the events
  `PostToolUseFailure` and `PostToolUse`); `test_lobe_hook_passes_stdin_and_stdout` and
  `test_lobe_hook_always_exits_zero` run with the new event too.
- House rules kept green and unedited: `test_tq_hook_deadline_is_under_the_hook_timeout`,
  `test_hooks_json_pretooluse_matcher_covers_the_shell_tools_only`, `TestHookDeadlinesUnderHarnessTimeouts`,
  `TestRun_RefusesWithNoGuardHook`, `TestRun_ForeignHookIsNoHook`, `TestRootMarketplacePin`,
  `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, `TestLobeScrubParity`, and the rest of "Must
  not trade off" in `CLAUDE.md`.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows), an empty
working folder and the repository's `plugin` folder, which it only reads (`PLUGIN` is its path). It uses no
network and never reads or writes the real `~/.tentaqles`. Setup:

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
J="$PLUGIN/hooks/hooks.json"; K="$PLUGIN/scripts/lobe_hook.sh"
A250="$(printf 'a%.0s' $(seq 1 250))"; A200="$(printf 'a%.0s' $(seq 1 200))"
export CLAUDE_CONFIG_DIR="$ID/acme/claude"; cd "$ACME"
ok()   { printf '%s' "${1//ACME/$ACME}" | tentaqles lobe-hook claude post-tool-use; echo "exit=$?"; }
fail() { printf '%s' "${1//ACME/$ACME}" | tentaqles lobe-hook claude post-tool-use-failure; echo "exit=$?"; }
```

Stop and report FAIL if a line starts with `SETUP FAIL`. Run the checks in order, within 5 minutes. Every `ok`
and `fail` call must print only `exit=0`.

1. A write. Expect 1 line in `LOG` with `id` `evt_9bde2e09`, `kind` `ok`, `tool` `Write`, an `input` that is
   exactly `{"file_path":"<ACME>/notes.txt"}` with acme's folder in place of `<ACME>`, no `error` and no `fp`.
   `grep -r -c -E 'CONTENT-MARKER-1|OUTPUT-MARKER-2' "$ID/acme/lobe" | grep -v ':0$' | wc -l` prints `0`.

   ```
   ok '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Write","tool_use_id":"toolu_01","tool_input":{"file_path":"ACME/notes.txt","content":"CONTENT-MARKER-1"},"tool_response":{"filePath":"ACME/notes.txt","note":"OUTPUT-MARKER-2"}}'
   ```

2. An MCP tool with a long value. Expect 2 lines. The second has `id` `evt_4d2e23b5` and an `input` whose `q`
   is the letter `a` exactly 200 times.

   ```
   ok '{"session_id":"s2","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"mcp__acme__ping","tool_use_id":"toolu_02","tool_input":{"q":"'"$A250"'"},"tool_response":"pong"}'
   ```

3. A failure of that tool in that session, then the success again. Expect 4 lines. The third has `kind`
   `fail`. The fourth has `id` `evt_b2e04476`, `kind` `ok`, no `dup`, and an `input` whose `q` is the letter
   `a` exactly 250 times: after a failure the input is kept in full.

   ```
   fail '{"session_id":"s2","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping","tool_use_id":"toolu_03","tool_input":{"q":"x"},"error":"widget 4411 is not frobnicated"}'
   ok '{"session_id":"s2","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"mcp__acme__ping","tool_use_id":"toolu_04","tool_input":{"q":"'"$A250"'"},"tool_response":"pong"}'
   ```

4. A background start. Expect still 4 lines.

   ```
   ok '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_05","tool_input":{"command":"npm run dev"},"tool_response":"Command running in background with ID: bash_1"}'
   ```

5. A shell success, then the same call again under another call id. Expect 6 lines. The fifth has `id`
   `evt_b0419005`, `kind` `ok` and `input` `{"command":"git status"}`. The sixth has `kind` `ok`, `dup`
   `evt_b0419005` and no `input`. `grep -r -c 'On branch main' "$ID/acme/lobe" | grep -v ':0$' | wc -l`
   prints `0`.

   ```
   ok '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_06","tool_input":{"command":"git status"},"tool_response":{"stdout":"On branch main\n","stderr":"","interrupted":false}}'
   ```

   The second call is the same JSON with `toolu_07`.
6. The lobe's own tool. The JSON of check 5 with `toolu_08` and the command `tentaqles lobe status`. Expect
   still 6 lines.
7. A search that found nothing, sent as a failure. Expect 7 lines; the seventh has `id` `evt_2c9b194b`, `kind`
   `ok`, `input` `{"command":"grep -n zzz notes.txt"}`, no `error` and no `fp`. Then the same with `toolu_10`
   and the error `Exit code 2`: 8 lines, and the eighth has `kind` `fail` and `exit` 2.

   ```
   fail '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_09","tool_input":{"command":"grep -n zzz notes.txt"},"error":"Exit code 1"}'
   ```

8. Capture off. `lobeset acme capture off`, then the JSON of check 1 with `toolu_11`. Expect still 8 lines.
   Then `lobeset acme capture on`.
9. Another client's login, and none. The JSON of check 1 with `toolu_12`, once with
   `CLAUDE_CONFIG_DIR="$ID/globex/claude"` set for that one command and once through
   `env -u CLAUDE_CONFIG_DIR`. Each prints only `exit=0`. Expect still 8 lines, and
   `ls "$ID/globex/lobe/events" 2>/dev/null | wc -l` prints `0`.
10. The plugin. `grep -c 'lobe_hook.sh\\" post-tool-use"' "$J"` prints `1`; `grep -c tq_hook.sh "$J"` prints
    `2`; `grep -c '"timeout": 15' "$J"` prints `2`. Then put the built program first on `PATH` under the name
    the wrapper looks for: copy the built `tentaqles` into `"$(cd "$W" && pwd)/bin"` under the name `tq`
    (`tq.exe` on Windows) and put that folder first on `PATH`. Always do this, even when `command -v tq` finds a
    `tq`: an installed release has no success step. Use the POSIX form of the folder: in Git Bash a `C:/...`
    entry in `PATH` splits at its colon. Pipe the JSON of check 1, with `ACME` replaced and `toolu_13`,
    to `bash "$K" post-tool-use; echo "exit=$?"`. It prints only `exit=0`, and `LOG` has 9 lines.

**Person checks** (the owner, at a terminal of their own):

1. In one real client, with the plugin of this build installed, start Claude Code through tentaqles and let
   the agent run one command that succeeds. Then run `tentaqles lobe events` in that folder. Expect one new
   `ok` line for the command, with no output text in it.

PASS needs 10 of 10 from the tester. The person check is the owner's view of the result.

## Done when

- [ ] the tests named above are in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`, and the plugin suite `PYTHONPATH=plugin python -m pytest plugin/tests -q`)
- [ ] the written phase 0 result names the `PostToolUse` fields this feature reads, and the fixtures match it
- [ ] the tester brief passes, 10 of 10
- [ ] the house rules hold: the guard's two `hooks.json` entries are byte-identical before and after;
  `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`, `TestLobeSenseOkDropsOutput`,
  `TestLobeScrubParity`, `TestLobeGuardNeverImportsLobe`, and every test under "Must not trade off" in
  `CLAUDE.md` unedited
- [ ] `docs/reference/commands.md`, `CHANGELOG.md`, `plugin/CHANGELOG.md` and `plugin/CLAUDE.md` are updated
- [ ] the owner has seen a successful call leave one `ok` line with no output in it

## Risks and unknowns

- The field names per tool come from use, not from a schema (S-03, "How to build it"). Task 2 confirms them
  before the table is trusted. Confirmed by the owner's capture of 2026-11-10 (phase 0 page). That capture
  also showed a background start arrives as an object with `backgroundTaskId`, not as the text in check 4;
  the check now reads that field first. A run moved to the background after its timeout was not captured.
- This hook now runs on every tool call. If starting Bash for the wrapper on Windows breaks the capture
  budget, F-03 names the fallback: the direct command `tentaqles lobe-hook claude <event> || exit 0`, which
  loses the install-folder lookup. [012](012-hook-latency-and-stall.md) measures it; nothing is changed here.
- A line for every success makes successes most of the log (S-03, Risks). The fill levels of S-07 step 4 are
  what bound it, and they arrive only with 011. Between this feature and 011 a day's file can grow without
  limit; the owner should not run a long real session on a build between the two.
- The kept full input (step 7) holds up to 4000 characters of a successful command, query or edit for the
  retention period. It is scrubbed and private, but closer to client data than a projection, and S-05 removes
  secrets, not rows or names (decision A-18).
- When the earlier failure is older than the tail, the success is a projection and 018 sees less. For an MCP
  tool that means values cut to 200 characters, which C-01 drops as uncomparable.
- A payload over F-02's size cap of 1 MiB leaves no event at all, so a success with a very large result
  cannot be paired.
- Until 009, a failure the host reports as a success is stored as `ok`. Nothing reads `ok` lines to learn
  from them before 018, so the wrong `ok` teaches nothing yet.
- One reading is this file's own: an empty `session` never matches the exception (step 7). The owner may
  overturn it at task 1.
- The plugin change reaches people only through a release and the marketplace pin.

## Result of the build, the review and the tester run

Built on 2026-11-10 by Haiku builders, reviewed as one change by a Sonnet reviewer.

- **Brief changes.** The owner chose that a tail that cannot be read keeps only the projection (step 7), so a
  Write's whole content is never stored because the log's end was unreadable; that one call loses only its
  pairing. The success path is `sense.FromClaudePostTool` (background check, drop, exception, projection,
  scrub), wired as the hook's `recordSuccess` on a new plugin `PostToolUse` entry.
- **The capture** (owner, 2026-11-10, recorded on the [phase 0 page](../prd/spikes/phase0-q2-claude-code-hooks.md))
  confirmed every field name the projection keeps. It also showed that a `run_in_background` command's start
  arrives as an object with `backgroundTaskId`, not as the text the brief named; `BackgroundStart` now reads a
  non-empty `backgroundTaskId` first and keeps the texts as a second check, with tests on the captured shape.
- **The review** found no blocking defect. Fixed from it: a doc claim about grep and the full input.
- Tests: `go vet`, `go build` and `go test ./... -count=1` green on Windows; the plugin suite 40 passed.
- Tester brief: 10 of 10 on Windows 11 in Git Bash against a program built from the working tree.

The owner's check, 2026-11-10, in a real yduqs session with this checkout's plugin: `git --version` left one
`ok` line whose input is `{"command":"git --version"}`, with no error, no exit and no output; a command sent
to the background left no line; the event count went from 5 to 6.

CI green on Linux, macOS and Windows for `801b3974` on 2026-11-10. Nothing is left open.
