# 020: The lesson for this exact error

**Status** not started · **Stage** Lessons · **Entries** R-04 (step 8, and steps 9 to 12 on the two post-tool
events), core page 5.2 (one answer with a starter rule's fix) · **Needs** 019, 015 · **Feeds** 022, 023, 028, 032, 036

## Result

A call fails with an error the lobe has a lesson for. In the same moment, beside the error, the agent reads
that lesson, so it can use it for the retry. If a starter rule also knows the error, its fix and the lesson
arrive together, in one block. A failure that looks like a success (feature 009) is answered the same way.

```sh
# a lesson of this client lists the fingerprint fp_... of "the MERGE source needs an alias"
tentaqles lobe-hook claude post-tool-use-failure < fail.json
# {"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":
#  "[tentaqles lobe] Notes from earlier work here. ...\n- Alias the merge source. Do: Give the source an
#  alias. Avoid: A subquery as the source. Why: The warehouse rejects it because the source has no
#  alias\n[tentaqles lobe] End of notes."}}
tentaqles lobe-hook claude post-tool-use-failure < fail.json    # the same session again: no output
```

## Scope

**In**

- R-04 step 8: after a `fail` outcome on `post-tool-use-failure`, and after a `hidden-fail` outcome on
  `post-tool-use`, select the eligible lessons that list the failure's fingerprint and print them at once.
- R-04 steps 9 to 12 for these two events: the shared session record, the order, the cap of 2, the budget,
  the time limits.
- One answer: a starter rule's fix (feature 015) and the lessons in one framed block, the fix first.
- What features 012 and 013 left for this one: the `whole` row of `tentaqles lobe bench latency` with a
  failure whose fingerprint a lesson lists, the stall case `blocked-read` (M-06 step 9), and leak attack
  L-01 (M-03).

**Out**

- The match before a call, the `fingerprint` key included (R-04 steps 3 to 7): feature 017 built it.
- Codex's one post-tool event (R-04 step 8, S-02): feature 036.
- E-02's verdict on these events, which runs between the append and this answer: feature 023.
- F-05's tamper check on these events: feature 034.
- Stale lessons ranked after fresh ones (R-04 step 10, R-06): feature 032. Until then every lesson is fresh.
- M-01's stats line after the print: feature 058.
- Argument notes and the blast list on `post-tool-use` (E-05, G-09): features 051 and 049. They are another
  block and never hold a lesson.

## What it builds on

Task 1 checks each name against the code.

- [003](003-shell-failure-claude-code.md), [004](004-any-tool-failure.md): the `post-tool-use-failure`
  step table, `sense.Record` and `sense.Outcome` (kind, `fp`, state), the rule "append first".
- [006](006-error-fingerprints.md): the `fp` in the outcome. [007](007-duplicate-filter.md): the outcome
  states `replay` and `ref`. [009](009-hidden-failures.md): the `hidden-fail` outcome on `post-tool-use`.
- [014](014-starter-rule-before.md): `safety.Wrap`. [015](015-starter-rule-after-failure.md):
  `starters.MatchError` and the fix line `tentaqles recognizes this error: <fix>` on
  `post-tool-use-failure`.
- [016](016-lesson-by-hand.md): lesson files with a `when.fingerprint` entry, and the gate.
- [017](017-lesson-before-call.md): `internal/lobe/recall` (`trigger.go` with the eligibility rule of R-04
  step 1 and `applies`, `session.go` with the `shown` record, `format.go` with the lesson line and the
  budget), the header cache in `index/`, the ladder stage of a lesson and the caps.
- [019](019-lesson-from-repair.md): lessons learned from a repair, which carry the failure's fingerprint.

**What this feature widens.** `recall` gains `postfail.go`. Feature 015 put an inject step after Sense on
both post-tool events, which prints the starter's fix line inside the frame. This feature adds the lesson
lines to that same block, after the fix line, so that one step builds the whole answer. The step now also
runs when no starter matches. Nothing is registered and no plugin file changes.

## What it leaves for later features

- `recall.PostFail(ctx, outcome, call) hook.Answer`: the one step that answers a failure. Feature 036 calls it
  from Codex's post-tool event. Feature 032 adds the stale mark and order inside it.
- The rule that the answer step runs last: feature 023 puts E-02's verdict before it, feature 034 the tamper
  check.
- `sessions/<session>.json` `shown` entries written after a failure, which the pre-call hook honors.
- A lesson shown at the moment of the failure: what feature 022 measures at the second meeting.

## How it works

1. **Where.** `tentaqles lobe-hook claude post-tool-use-failure`, and `tentaqles lobe-hook claude
   post-tool-use` when Sense's outcome is `hidden-fail`. Both are "capture, then inject" (core page 5.2).
   The event line is appended first, against the capture budget. This step starts only after the append has
   returned (F-02 step 2), and the whole process is cut off at 1.5 seconds. At the cut-off nothing is
   printed and the exit code is 0; the event line is already written.
2. **Last in the process.** The order is Sense's append, then the steps later features add (the tamper
   check, E-02's verdict), then this answer (R-04, "How to build it"). After an `ok` outcome the step does
   nothing: `post-tool-use` prints no lesson.
3. **The fingerprint comes from memory.** The step takes the `fp` from `sense.Outcome`; it does not read the
   log back. So it also answers when the duplicate filter wrote a reference line (state `ref`), wrote
   nothing (state `replay`) or the line could not be written (state `dropped`): kind and `fp` are known in
   each case. With `capture` off there is no outcome and the step does not run.
4. **Who is eligible (R-04 step 1).** `recall` on, `lessons` on, `enforce` not `off`; the lesson passed the
   gate, sits in `lessons/` and not in quarantine, and acts at `advice` or `block` after the `enforce` cap;
   the call is where the lesson's `applies` says it counts. Feature 017's function decides this; nothing is
   decided twice.
5. **The match.** Of the eligible lessons, those whose `when.fingerprint` lists the outcome's `fp`. No other
   key is tested: a lesson whose `tool` or `command` does not fit this call still matches (core page 5.4,
   "`fingerprint` matches on its own").
6. **The session record (R-04 step 9).** A lesson already under `shown` in `sessions/<session>.json` with
   the same line hash is skipped, unless this fingerprint match comes 10 minutes or more after its `at`.
   The record is shared with the pre-call hook: a lesson shown after the failure is not repeated before the
   next call, and a lesson shown before the call is not repeated when that call fails, inside 10 minutes.
7. **Order and cap (R-04 step 10).** All matches here are fingerprint matches, so the order is `strength`,
   then `seen`, then `id`. Keep 2 (proposed).
8. **One block.** The step asks `starters.MatchError` for the fix line (feature 015's rules: catalog order,
   first match wins, its switches). The block is: the fix line when there is one, then the lesson lines, in
   feature 017's form `- <title>. Do: ... Avoid: ... Why: ...`, each cut at 300 characters. Everything
   inside the frame is at most 700 characters (both proposed); a line that does not fit whole is left out,
   the last lesson first. `safety.Wrap` adds the frame once. With no fix line and no lesson the step prints
   nothing.
9. **Output.** One JSON object, one `Write`:
   `{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"<block>"}}`, or the same
   with `PostToolUse` on that event. Never `permissionDecision` (principle 3). Exit 0.
10. **After the print.** The shown ids are written under `shown` with the time and the line hash, newest 40
    kept, under the store lock. When the lock is busy for 50 ms the write is skipped and the lesson may
    show once more (R-04, "How to build it").
11. **Nothing is printed** when the client rule fails (core page 5.1), when a switch of step 4 is off, when
    the lesson is at `seen`, `trial` or `retired`, or when no lesson lists the fingerprint and no starter
    matches.

## Where to read

| What | Where |
|---|---|
| Requirements | [R-04](../prd/lobe-recall.md) steps 1, 8 to 12, and in "How to build it" the paragraphs on running last, the output and the session record; [F-02](../prd/lobe-foundation.md) step 2 (the two post-tool rows and the paragraph under the table) and step 6; [C-08](../prd/lobe-consolidate.md) steps 1 to 4 and 7; [S-04](../prd/lobe-sense.md) step 6; [S-06](../prd/lobe-sense.md) step 4; [F-05](../prd/lobe-foundation.md) step 4; [core page](../prd/vertical-lobe.md) 5.2 and 5.4 |
| Source to port (Trail-code, MIT) | [`src/claude/format.ts` L168](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L168): 2 learnings per tool call; [L203](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L203): the seen filter before that cap |
| Idea only (BASE; no code, no identifiers) | [`src/domain/session.rs` L539](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/session.rs#L539): due again when never shown, when the text changed, or after a throttle of 10 minutes |
| Own design | the fingerprint trigger after a failure, and the 700-character budget (R-04's Source table) |
| Existing tentaqles code | `internal/lobe/recall`, `internal/lobe/starters`, `internal/lobe/hook/steps.go` and `internal/lobe/sense` as features 003 to 017 left them; `internal/testutil/latency.go` for the timing test |

## Build tasks

1. **Check this brief against the code.** Read `recall`, `starters`, `sense` and the hook's step table as
   built. List what no longer fits: the fields of `sense.Outcome`, whether feature 017 exposes the
   eligibility rule, the budget code and the session record as functions, whether feature 015's fix line is
   framed and whether it is wired on `post-tool-use`. The owner approves changes before code is written.
2. **Does the host show it?** Read the written phase 0 result (feature 003, F-03 step 1) for "does Claude
   Code show the agent context a hook adds after a failed call". Feature 015 was the first to need it. If
   the answer is no, stop and tell the owner: this feature then has no effect in Claude Code, and R-04
   step 7 (the note before the next call) is the only path.
3. **The step.** `internal/lobe/recall/postfail.go`: `PostFail`. Proof: `TestLobeFailureShowsLesson`,
   `TestLobeFailureLessonByFingerprintOnly`, `TestLobeFailureLessonAfterAFoldedLine`,
   `TestLobeFailureLessonBudget`.
4. **Wiring and one block.** `steps.go`: feature 015's inject step on `post-tool-use-failure`, and on
   `post-tool-use` for `hidden-fail`, now builds its block through `PostFail`. Proof:
   `TestLobeHiddenFailureShowsLesson`, `TestLobeFailureAnswerIsOneBlock`, `TestLobeEveryInjectionIsFramed`,
   `TestLobeCaptureSurvivesALateStep`.
5. **Record, switches, isolation.** Side by side with task 4. Proof:
   `TestLobeFailureLessonSharesTheSessionRecord`, `TestLobePreToolEnforceOff`,
   `TestLobeRecallStageAndSwitches`, `TestLobeFailureLessonAppliesAndClient`.
6. **Bench and leak.** Side by side with task 5. In `internal/lobe/benchcheck`: the `whole` rows of the two
   post-tool events get a store with a lesson that lists the failure's fingerprint; the stall case
   `blocked-read` (the lesson file cannot be read: a named pipe nobody writes, or a file held open with no
   sharing on Windows); attack L-01 in `attacks.go` (the same failing command and error in both clients,
   acme has a lesson for it, globex's hook prints nothing of acme's). Proof:
   `TestLobeStall_BlockedReadKeepsTheEvent`, `TestLobeLeak_NothingSkippedSilently` with L-01 under `ran`.
7. **Docs.** `docs/reference/commands.md`: the `tentaqles lobe-hook` section says what the two post-tool
   events can print, and `tentaqles lobe bench stall` lists `blocked-read`. One line in `CHANGELOG.md`. No
   plugin change.
8. **Tester run.** A fresh agent runs "Evaluation" below.

## Tests that stay in the repository

From the requirements:

- `TestLobeFailureShowsLesson`: a failure whose `fp` an eligible lesson lists prints that lesson with
  `hookEventName` `PostToolUseFailure`; the same failure again in that session, and a failure with another
  `fp`, print 0 bytes; the event line is in the log each time: 3 of 3.
- `TestLobeHiddenFailureShowsLesson`: a `hidden-fail` outcome whose `fp` an eligible lesson lists prints
  that lesson on `post-tool-use` with `hookEventName` `PostToolUse`, inside F-05's frame; an `ok` outcome
  prints no lesson: 2 of 2 here. The third case, Codex, arrives with feature 036.
- `TestLobePreToolEnforceOff`: the after-failure half. With `enforce: off` a matching lesson gives 0 bytes
  after a failure; back at `advice` it prints.
- `TestLobeRecallStageAndSwitches`: now also after a failure. A lesson at `seen` or `trial`, or `lessons:
  off` or `recall: off`, gives 0 bytes; at `advice` it prints: 5 of 5.
- `TestLobeEveryInjectionIsFramed` (F-05): still 3 of the 5 events, as feature 015 left it. On
  `post-tool-use-failure` and on `post-tool-use` after a hidden failure the framed block now holds lesson
  text, and a block with a lesson and no starter line is framed too.
- `TestLobeStall_BlockedReadKeepsTheEvent` (M-06): with the lesson that lists the fingerprint unreadable,
  `post-tool-use-failure` exits 0 with no output and 1 new event line: yes or no.
- `TestLobeCaptureSurvivesALateStep` (F-02): with this step sleeping past the cut-off, the event line is in
  the log, exit 0, 0 bytes of stdout.

New for this slice:

- `TestLobeFailureAnswerIsOneBlock`: an error that a starter rule and a lesson both know gives one JSON
  object with one frame; the fix line stands before the lesson line; at most 700 characters inside the
  frame; no `permissionDecision`.
- `TestLobeFailureLessonByFingerprintOnly`: a lesson with `tool: [Edit]` that lists the `fp` of a `Bash`
  failure prints; a lesson whose `tool` and `command` fit and whose `fp` differs prints 0 bytes: 2 of 2.
- `TestLobeFailureLessonAfterAFoldedLine`: the same failure twice within 300 seconds, the lesson filed
  between them: the second call writes a reference line and prints the lesson.
- `TestLobeFailureLessonBudget`: 5 lessons list the `fp`; the block has 2 lesson lines, in the order
  strength, seen, id.
- `TestLobeFailureLessonSharesTheSessionRecord`: shown after the failure, not shown before the next call;
  shown before a call, not shown when it fails; with the test clock 10 minutes on, shown again: 3 of 3.
- `TestLobeFailureLessonAppliesAndClient`: an `applies: repo` lesson prints 0 bytes in the client's other
  repository; under another client's login the hook prints 0 bytes and both stores are byte-identical.
- `TestLobeFailureLessonCaptureOff`: with `capture` off, 0 bytes and no event.

House rules this feature keeps green: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
`TestLobeHookNeverPrintsAllow`, `TestLobeQuarantineIsNeverRecalled`, `TestLobeGuardNeverImportsLobe`,
feature 012's `tentaqles lobe bench latency` and `stall` for the two post-tool events (the append inside the
capture budget, the whole process inside 1.5 seconds), feature 013's leak suite with L-01 now run
(`TestLobeLeak_NothingSkippedSilently` moves one id from `not_run` to `ran`), feature 015's starter tests,
and every "Must not trade off" test in `CLAUDE.md`, unedited.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows) and an empty
working folder. It uses no network, never clears `CLAUDECODE` and never runs a person-only command.

**Setup.** Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
lobeset() { m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"; }
A="$W/clients/acme"; ID="$TQ_HOME/identities"; L="$ID/acme/lobe"; C=acme; cd "$A"; mkdir -p "$L/lessons"
run() { CLAUDE_CONFIG_DIR="$ID/$C/claude" tentaqles lobe-hook claude "$1"; echo " exit=$?"; }
fail() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"%s"},"error":"%s"}' \
  "$1" "$A" "$2" "$3" | run post-tool-use-failure; }
ok() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"%s"},"tool_response":{"stdout":"%s","stderr":""}}' \
  "$1" "$A" "$2" "$3" | run post-tool-use; }
pre() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"%s"}}' \
  "$1" "$A" "$2" | run pre-tool-use; }
lastfp() { cat "$L"/events/*.jsonl | grep -o 'fp_[0-9a-f]\{16\}' | tail -1; }
lines() { cat "$L"/events/*.jsonl | wc -l; }
put() {  # put <id> <title> <when entries> <do> <avoid> <why>: gate a hand-written lesson, then place it
  { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' "$1" "$2"
    printf ' when: {%s}, strength: 1.0, seen: 0, created: 2026-11-01}\n---\n' "$3"
    printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; } > "$W/$1.md"
  tentaqles lobe lessons gate "$W/$1.md" && cp "$W/$1.md" "$L/lessons/$1.md"; }
E="the MERGE source needs an alias"; Q="dbsql -f q.sql"
T="bash: line 1: timeout: command not found"; N="timeout 60 npm test"
HC="npm test 2>&1 | tail -5"; HF='FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n'
# five lessons; each reads $FP, $FP2 or $FP3 when it is called, not now
put1() { put l-aaaa0001 "Alias the merge source" "fingerprint: [$FP]" "Give the source an alias." \
  "A subquery as the source." "The warehouse rejects it because the source has no alias."; }
put2() { put l-aaaa0002 "Cart totals test" "fingerprint: [$FP2]" "Run the cart test alone first." \
  "Reading only the tail of the output." "A broken total looks like a pass because the pipe hides the exit code."; }
put3() { put l-aaaa0003 "No wrapper around the test runner" "fingerprint: [$FP3]" \
  "Call the runner directly and set the limit on the tool call." "Wrapping the runner in another program." \
  "The wrapper is missing on this machine, so the call fails before any test starts."; }
put4() { put l-aaaa0004 "Slow suites on this box" "fingerprint: [$FP3]" "Split the suite into two shards." \
  "One long serial run." "A single run exceeds the ten minute limit, otherwise it would finish."; }
put5() { put l-aaaa0005 "Coreutils are partial here" "fingerprint: [$FP3]" \
  "Check with command -v before using a GNU helper." "Assuming every GNU helper exists." \
  "Several helpers were never installed, since this image is minimal."; }
```

No line may start with `SETUP FAIL`. `run` sets the login folder the client rule asks for. Every `put1` to
`put5` must print `ok`; if one does not, stop and report FAIL. "Prints nothing" means: nothing before
` exit=0`. Every hook must end with ` exit=0`. A lesson a person wrote enters at `advice`, so no ladder file
is needed.

1. `fail s1 "$Q" "$E"` prints nothing. `FP=$(lastfp); echo "$FP"` prints `fp_` and 16 hex characters.
2. `put1` files a lesson that lists that fingerprint. Then `fail s2 "$Q" "$E"`: stdout holds
   `"hookEventName":"PostToolUseFailure"`, `Alias the merge source` and `not instructions`, and no
   `permissionDecision`.
3. `fail s2 "$Q" "$E"` again prints nothing. `fail s3 "$Q" "permission denied for table t"` prints nothing.
4. One record for both moments. `pre s2 "$Q"` prints nothing: the lesson was shown in `s2` already. In a
   new session, `fail s4 "$Q" "$E"` prints the lesson and `pre s4 "$Q"` then prints nothing.
5. Only the fingerprint is tested. `fail s5 "psql -f other.sql" "$E"` prints `Alias the merge source`.
6. A hidden failure. `ok h1 "$HC" "$HF"` prints nothing. `FP2=$(lastfp)`, and
   `[ "$FP2" != "$FP" ] && echo new` prints `new`. `put2`. Then `ok h2 "$HC" "$HF"`: stdout holds
   `"hookEventName":"PostToolUse"`, `Cart totals test` and `not instructions`. Then
   `ok h3 "cargo test" 'test result: ok. 12 passed; 0 failed\n'` prints nothing.
7. One answer with a starter rule. `fail t1 "$N" "$T"`: stdout holds `timeout program` and not `No wrapper
   around`. `FP3=$(lastfp)`, `put3`, then `fail t2 "$N" "$T" > "$W/o.txt"` and:

   ```bash
   grep -o additionalContext "$W/o.txt" | wc -l            # 1: one answer
   grep -o 'Notes from earlier work' "$W/o.txt" | wc -l    # 1: one frame
   grep -c 'timeout program' "$W/o.txt"                    # 1: the starter rule's fix is there
   grep -c 'No wrapper around the test runner' "$W/o.txt"     # 1: the lesson is there
   o=$(cat "$W/o.txt"); a="${o%%timeout program*}"; b="${o%%No wrapper around*}"
   [ ${#a} -lt ${#b} ] && echo ordered                     # ordered: the starter's fix stands first
   ```

8. The cap. `put4` and `put5` file two more lessons that list `$FP3`. Then
   `fail t3 "$N" "$T" > "$W/o2.txt"`:
   `grep -o -E 'No wrapper around|Slow suites|Coreutils are partial' "$W/o2.txt" | sort -u | wc -l` prints
   `2`, the file holds `timeout program`, and `wc -c < "$W/o2.txt"` prints 1100 or less.
9. Stage. Write `$L/ladder.json` with the one line below, then `fail s6 "$Q" "$E"` prints nothing. Delete
   `$L/ladder.json`.

   ```json
   {"v":1,"entries":[{"lesson":"l-aaaa0001","stage":"trial","hits":0,"false_alarms":0,"sessions":0,"approved_at":null}]}
   ```

10. Switches. `lobeset acme enforce off; n=$(lines)`, then `fail s7 "$Q" "$E"` prints nothing and
    `lines` prints `n` plus 1: the failure was still recorded. `lobeset acme enforce advice; lobeset acme
    recall off`, then `fail s8 "$Q" "$E"` prints nothing. `lobeset acme recall on`, then `fail s9 "$Q" "$E"`
    prints `Alias the merge source`.
11. Another client's login. `n=$(lines); C=globex`, then `fail s10 "$Q" "$E"` prints nothing, `lines` still
    prints `n`, and `ls "$ID/globex/lobe/events" 2>/dev/null | wc -l` prints `0`. Set `C=acme`.
12. A lesson the lobe learned itself. `fail r1 "npx vitest run" "Error: Worker exited unexpectedly"`
    prints nothing; `ok r1 "npx vitest run --pool=forks" ok` prints nothing;
    `tentaqles lobe consolidate --session r1 --json` has a line with `"action":"file"`. Then
    `fail r2 "npx vitest run" "Error: Worker exited unexpectedly"`: stdout holds `PostToolUseFailure` and
    `use --pool=forks`.
13. The append comes first. `tentaqles lobe bench stall --case blocked-read --event post-tool-use-failure
    --out "$W/stall"; echo "exit=$?"` prints `exit=0`.
14. No leak. `tentaqles lobe bench leak --out "$W/leak"; echo "exit=$?"` prints `exit=0`;
    `$W/leak/summary.json` holds `"leaks":0` and `"controls_failed":0`, and
    `grep -c '"id":"L-01","needs"' "$W/leak/summary.json"` prints `0`: L-01 ran.

**Person checks** (the owner, in one real client, in a real Claude Code session)

1. With a lesson from feature 019 in the store, start a new session, tell the agent to run the failing form
   of the command whatever any note says, and after the error ask it what the note beside the error said.
   Expected: it quotes the lesson. This is the check that Claude Code shows context added after a failed
   call.

**Score.** PASS needs 14 of 14. "Could not run" is a FAIL.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems). The plugin suite is unchanged and green.
- [ ] The tester brief passes 14 of 14.
- [ ] The house rules hold: `TestLobeEveryInjectionIsFramed`, `TestLobeCaptureSurvivesALateStep`,
  `TestLobeFailureLessonAppliesAndClient`, `TestLobeHookNeverPrintsAllow`,
  `TestLobeStall_BlockedReadKeepsTheEvent`, feature 012's latency checks for both post-tool events, the leak
  suite at zero leaks with L-01 run, and every "Must not trade off" test unedited.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The written answer to task 2 is on file.
- [ ] The owner has seen a lesson appear right after a failure with a known fingerprint.

## Risks and unknowns

- **Whether a host shows context added after a failed call is not confirmed** (R-04 and C-08 risks; phase 0
  question 2). Task 2 reads the answer. Where the host does not show it, the note before the next call
  (feature 017, R-04 step 7) still brings the lesson.
- **Masked fingerprints merge different errors** (S-08's risks): `near '('` and `near 'SELECT'` share one
  fingerprint, so the first mistake's lesson is shown after the second one's error. Feature 022 is where it
  would show.
- **A lesson shown before a call and not followed is not repeated when the call fails**, unless 10 minutes
  have passed (R-04's risks). Feature 022 will show whether that costs repeats.
- **Two calls sent at once** in one session can both read the record before either writes; the lesson then
  shows twice (R-04's risks).
- **A `dropped` or `replay` outcome is answered too.** R-04 says the answer comes "once Sense has appended
  the event line" and that the `fp` is handed over in memory. This file reads that as: any outcome with a
  failure kind and an `fp` is answered. The owner may narrow it.
- **What is cut first** when the block is over 700 characters is this file's rule (the last lesson line);
  R-04 gives the rule only for the block after an edit. It must equal what feature 017 does before a call.
- **Proposed numbers:** 2 lessons, 300 and 700 characters, the 50 ms lock wait. The 10 minutes are R-04's.
- **What would change this file:** a `sense.Outcome` without an `fp` field; a feature 017 that did not
  factor eligibility, budget and record into functions; a phase 0 answer of "no" in task 2.
