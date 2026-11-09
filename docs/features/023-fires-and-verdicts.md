# 023: Every fire is recorded and judged

**Status** not started · **Stage** Lessons · **Entries** E-02 (fires, the verdict table, `trials.jsonl`,
`ladder.Counts`, `ladder.Settle` without stage moves) · **Needs** 019 · **Feeds** 024, 030, 035, 037, 038, 041

## Result

Each time a rule matches a call that is about to run, the lobe notes it. When the call's result arrives, the
lobe judges the match and appends one line to `trials.jsonl`: a `hit` when the call failed as the lesson said
it would, a `false_alarm` when it worked. A lesson on trial stays silent the whole time. `tentaqles lobe rules`
shows each rule's current counts. Nothing moves a stage yet; feature [024](024-automatic-demotion.md) does.

```bash
cd "$W/clients/acme"
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
echo "$PRE"  | tentaqles lobe-hook claude pre-tool-use            # a call the lesson's pattern matches
echo "$FAIL" | tentaqles lobe-hook claude post-tool-use-failure   # the same call, failed
cat "$TQ_HOME/identities/acme/lobe/trials.jsonl"                  # one line, "verdict":"hit"
tentaqles lobe rules --json                                       # "hits":1 for that lesson
```

## Scope

**In**

- Package `internal/lobe/trial`: `fire.go`, `judge.go`, `journal.go`, `settle.go`.
- Opening a fire at PreToolUse for a lesson stored at `trial`, `advice` or `block`, and for a starter rule.
- Judging it on PostToolUse and PostToolUseFailure, after the event line is appended.
- `trials.jsonl` and the key `open_fires` of `sessions/<session>.json`.
- `ladder.Counts`, and `tentaqles lobe rules` showing live counts.
- `ladder.Settle` in the detached pass and in `tentaqles lobe consolidate`: it closes old fires, redoes the
  stored counts, and gives a first entry to a lesson file that has none. It makes no stage move here.
- The switches: nothing fires under `enforce: off` or `lessons: off`; no fire is opened under `capture: off`;
  `recall: off` silences the note and not the record.
- The time limits of E-02 (proposed) and the widening of feature 012's latency check for a post-tool event
  that has a fire to judge.

**Out**

- Every stage move, the one function that writes a stage, and `ladder-log.jsonl` (feature 024). A false alarm
  is recorded and counted here and demotes nothing until then.
- The step from `trial` to `advice` by record. Feature 024 applies it from `Settle`; feature
  [035](035-backfill.md) brings the lessons that enter at `trial` on their own, see "What it leaves".
- `act` values `deny` and `pass`, the fire of an overridden call, and hits counted toward `block` (feature
  [038](038-approve-to-block.md)).
- Leaving out the verdicts of a session that F-05 marked as tampered. The mark is built by feature
  [034](034-teammate-lesson-intake.md) per the index; that feature adds the exclusion to `ladder.Counts`.
- What happens after a working fix that matches a lesson's `avoid.pattern`, beyond the `false_alarm` verdict:
  no reinforcement (feature [030](030-reinforcement-and-fading.md)), the demotion (024), the successor lesson
  (features [025](025-model-drafting.md) and [031](031-replacing-a-lesson.md)).
- `tentaqles lobe lessons --waiting` and the brief's waiting line (features 035 and
  [026](026-session-brief.md)).
- Codex's one post-tool event (feature [036](036-lessons-in-codex.md)).
- The second measure of enforcement accuracy read from `trials.jsonl` (feature
  [041](041-enforcement-accuracy.md)).

## What it builds on

- **003, 004, 008, 009**: `sense.Record`, which appends the event line first and returns
  `sense.Outcome{Session, ID, Tool, CallID, Call, Kind, FP, State}`, and `sense.CallText`, which builds the
  same call text before and after a call. An outcome whose `State` is `dropped` still has `Kind` and `FP`.
- **003**: the fail-open engine `internal/lobe/hook` (the client rule, the cutoffs, `Answer`, the order of
  steps per event), the store (`store.Append`, `store.Update`, the 50 ms lock wait for a hook, `ErrBusy`),
  and `sessions/<session>.json`, where each entry owns its keys.
- **006**: the fingerprint `fp` on a failure.
- **014**: the starter catalog and its matcher. From the day the ladder ships a starter holds an ordinary
  entry at `advice` (C-08 step 7).
- **016**: the lesson header with `when`, `avoid`, `applies` and `when.fingerprint`.
- **017**: `internal/lobe/ladder` up to `advice` (`ladder.json`, the stage a rule acts at after the caps,
  `tentaqles lobe rules` and its `--json`), and R-04's matcher and note in `internal/lobe/recall`.
- **018**: the detached pass in `internal/lobe/consolidate/run.go` and `tentaqles lobe consolidate` with
  `--session`, `--only` and `--now`.
- **019**: `ladder.Enter`, called when a lesson is filed.
- **012**: `tentaqles lobe bench latency`, whose "whole" row for a post-tool event gains a fire to judge.

## What it leaves for later features

- `trials.jsonl`, one line per fire at any stage: 024 reads it to demote, 038 to count hits toward `block`,
  037 for `rules test --replay`, 041 as its second measure.
- `trial.Judge(fire, outcome) Verdict`, the pure table (024 acts on `false_alarm` in the same hook).
- `ladder.Counts(lesson)`: 024 adds the cut at the lesson's last move, read from `ladder-log.jsonl`; 034 adds
  the tampered-session exclusion.
- `ladder.Settle`: 024 adds the moves by counts and the promotion from `trial` to `advice`; 030 adds faded;
  031 adds replaced and replacement undone; 038 adds tests, pattern and idle.
- The `act` field: 038 adds `deny` and `pass`.
- For feature 035: a lesson at `trial` is silent, its fires are recorded and its counts are visible. What 035
  adds is the lesson that gets there by itself (a `backfill` lesson with a `when` block enters at `trial`, one
  without enters at `seen`), the waiting list, and the end-to-end check that a mined lesson earns `advice` on
  its record through the move feature 024 applies.

## How it works

Shared rules are cited, not repeated: the hook table and budgets are core page 5.2, the session file is F-01
step 9, the `when` matcher is R-04 steps 3 to 5 and 7, the caps are E-01's table.

1. **When a fire opens.** In `pre-tool-use`, after the answer is written, the hook builds the call text with
   `sense.CallText` and looks at every lesson stored at `trial`, `advice` or `block` that counts here by its
   `applies`.
   - A lesson with an `avoid` block fires when its `avoid.pattern` matches the call text and its `when` block,
     when it has one, matches the call. The fire is `by: avoid`.
   - A lesson with no `avoid` block fires when R-04's matcher selects it for this call. The fire is `by: when`.
     "Selects" is read here as: the `when` block matches, before the once-per-session filter and the cut to 2
     notes, so a lesson already shown still fires.
   - A starter rule fires when C-08's own matcher selects it, and is recorded the same way under its catalog
     id.
   - A lesson at `seen` or `retired`, and a lesson with neither an `avoid` block nor a `when` block, never
     fires.
2. **What is stored.** The fire goes into `open_fires` of `sessions/<session>.json`: `lesson`, `stage` (the
   stored stage), `by`, `tool`, `tool_use_id`, `call`, and the two fields this file adds so the later steps can
   work, `at` and `act`. `call` is `c_` plus the first 16 hex characters of the SHA-256 of the call text. The
   text itself is never stored.
3. **`act`** is what the agent got for this call from this rule: `none` for a fire at `trial`, for a note that
   `recall: off` silenced, and for a lesson R-04 matched and did not print; `note` when its line was printed.
   A rule stored at `block` acts as `advice` until feature 038, so its fire reads `block` with `note`.
4. **Limits** (proposed): 5 fires per call, 20 `by: when` fires per lesson per session, 25 ms for the whole
   step. Past any of them the fire is dropped. The session file is written with `store.Update` under the lock;
   when the lock is busy the fire is dropped.
5. **Switches.** Under `enforce: off` or `lessons: off` nothing fires and no line is written. With
   `capture: off` no fire is opened (proposed), because no result would come back to judge it. With
   `recall: off` nothing is printed, the fire's `act` is `none`, and it is judged like any other.
6. **The verdict.** On `post-tool-use` and `post-tool-use-failure`, Sense runs first and returns its outcome.
   The step then finds the open fires of this call by the outcome's `CallID`, or by `Tool` and the hash of
   `Call`, oldest first, and judges each with the pure table of `judge.go`:

   | Fire | Result of the call | Verdict |
   |---|---|---|
   | `by: avoid` | `ok` | `false_alarm` |
   | `by: avoid` | `fail` or `hidden-fail`, and the lesson lists no fingerprint or lists this one | `hit` |
   | `by: when` | `fail` or `hidden-fail` with a fingerprint the lesson lists | `hit` |
   | `by: when` | `ok` | `pass` |
   | either | a failure with a fingerprint the lesson does not list | `unrelated` |
   | either | no result within 10 minutes (proposed) | `unknown` |

   A replay or a reference line (S-06) is judged too, and so is an outcome whose state is `dropped`. When
   Sense returns no outcome (a skip rule, a command sent to the background), the fire stays open.
7. **The line.** The hook appends the fire with its verdict to `trials.jsonl` and removes it from the session
   file, within 30 ms (proposed):
   `{"v":1,"id","at","session","host","lesson","stage","by","act","tool","call","verdict","event","judged_at"}`.
   `event` is the `id` of the event line and is read together with `session`. For a `dropped` outcome it names
   no stored line. Only `hit` and `false_alarm` count toward the ladder.
8. **Order and time.** The event line is whole in the log before the verdict line is written. The verdict
   runs after F-05's tamper check, once feature 034 has built it, and before the lesson for this error is
   printed (F-02 step 2). A hook cut off before step 1, 2 or 7 loses the fire: one data point and nothing else.
   A busy lock or a slow session write never delays the answer.
9. **`ladder.Counts(lesson)`** reads the lesson's lines in `trials.jsonl` and returns `hits`, `false_alarms`
   and `sessions`. `sessions` is the number of distinct sessions that hold a hit (this file's reading of "at
   least 2 hits in at least 2 sessions"). Here it counts all the lesson's lines; feature 024 limits it to the
   lines since the lesson's last move. `tentaqles lobe rules` calls it, so the list is current before `Settle`
   has run.
10. **`ladder.Settle`** runs in the detached pass after consolidation and before the graph build, and in
    `tentaqles lobe consolidate`, also with `--only` or `--session`. It works on the whole store, not on one
    session. In this slice it does four things: closes every fire open for 10 minutes as `unknown`, a dead
    session's included; calls `ladder.Enter` for a lesson file that has no entry yet, such as one written by
    hand; writes each entry's three counts from `ladder.Counts`; and drops lines of `trials.jsonl` older than
    180 days (proposed). It takes the store lock only around each write.
11. **Edge cases.** Of two identical calls, the oldest open fire is judged first. A denied call never runs, so
    its fire would close as `unknown` (nothing denies before 038). A count written by hand into `ladder.json`
    with no trial lines behind it is replaced by the recount at the next `Settle`.
12. **The limit of the record** (E-02). A lesson with neither an `avoid` block nor a listed fingerprint cannot
    earn a hit, and a lesson with no `avoid` block cannot earn a false alarm. At `advice` such a lesson is
    shown all the same.

## Where to read

| What | Where |
|---|---|
| Requirements | [E-02](../prd/lobe-enforce.md) in full; [E-01](../prd/lobe-enforce.md), the table rows "enter", "caps" and "files", step 1 (the call text) and the paragraph on `ladder.Enter`; [E-07](../prd/lobe-enforce.md), only to see what feature 024 will hang on this |
| Requirements, shared | [core page](../prd/vertical-lobe.md) 5.2 (the two post-tool rows, the budgets), 5.3 (`trials.jsonl`, `sessions/`), 5.4 (event, ladder entry), 5.5, 5.6; "Where Sense ends" at the top of [the Sense page](../prd/lobe-sense.md); [F-01](../prd/lobe-foundation.md) steps 6 to 9; [F-02](../prd/lobe-foundation.md) step 2; [R-04](../prd/lobe-recall.md) steps 1 and 3 to 5; [C-08](../prd/lobe-consolidate.md) step 7; [M-06](../prd/lobe-benchmarks.md) step 4 |
| Source, idea only (BASE: no code, no identifiers) | [`src/shadow/run.rs` L55](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/run.rs#L55): a candidate runs beside the live matcher and only what it would have changed is recorded; [`src/usage.rs` L391](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/usage.rs#L391): what happened next is the judge |
| Source, weakness not taken (BASE) | [`src/shadow/report.rs` L211](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/report.rs#L211): a gate that only fills when prompt text is stored |
| Existing code | `internal/lobe/sense` (`Record`, `Outcome`, `CallText`), `internal/lobe/hook`, `internal/lobe/store`, `internal/lobe/ladder`, `internal/lobe/recall`, `internal/lobe/consolidate/run.go`, `internal/lobe/benchcheck/latency`, as features 003 to 019 left them |

## Build tasks

1. **Check this brief against the code.** Read the packages in the last row above. Confirm that both payloads
   of a call carry `tool_use_id` (the phase 0 result of feature 003 says so or not). List what no longer fits,
   in particular the names of the hook's step list and of the ladder's entry type.
2. **`trial/judge.go`.** The pure table. Proof: `TestLobeTrial_VerdictTable`. Can run beside task 3.
3. **`trial/journal.go`.** The line type, append, read by lesson, the 180-day cut. Can run beside task 2.
4. **`trial/fire.go` and the `pre-tool-use` step.** When a fire opens, the limits, the session file. Proof:
   `TestLobeTrial_NoCallText`, `TestLobeTrial_OffRecordsNothing`, `TestLobeTrial_StarterFireRecorded`,
   `TestLobeTrial_BudgetDropsFire`.
5. **The post-tool step.** Find, judge, append, remove. Proof: `TestLobeTrial_AfterTheAppend`,
   `TestLobeTrial_DroppedOutcomeIsJudged`, `TestLobeTrial_EveryStageRecorded`,
   `TestLobeTrial_WorkingFixThatMatchesAvoid`, `TestLobeTrial_NoHitWithoutAvoidOrFingerprint`.
6. **`ladder.Counts` and the list.** `tentaqles lobe rules` and its `--json` show live counts. Proof:
   `TestLobeLadder_CountsFromTheJournal`.
7. **`trial/settle.go` and `ladder.Settle`.** The four parts of step 10, wired into the pass and the verb.
   Proof: `TestLobeSettle_ClosesOldFires`, `TestLobeSettle_EntersAHandWrittenLesson`.
8. **Latency.** Give `tentaqles lobe bench latency` a post-tool payload with one open fire, and run it. Proof:
   with no open fire the 95th percentile of the append rises by less than 2 ms (proposed, M-06); no run passes
   the 1.5 second cutoff.
9. **Docs.** `docs/reference/commands.md`: the `tentaqles lobe rules` section says where the counts come from;
   a line in `CHANGELOG.md`. The plugin does not change.
10. **Tester run.** A fresh agent runs the Evaluation below.

## Tests that stay in the repository

- `TestLobeTrial_VerdictTable`: the 6 outcomes of the table give the 6 expected verdicts.
- `TestLobeTrial_NoCallText`: after 100 fires on commands holding a marker, the marker is stored 0 times, in
  `trials.jsonl` and in the session files.
- `TestLobeTrial_EveryStageRecorded`: one fire each at `trial`, `advice` and `block` gives 3 lines. Here their
  `act` is `none`, `note` and `note`, the last because a stored `block` acts as advice in phase 2. Feature 038
  changes the third to `deny`, which is what E-02 states.
- `TestLobeTrial_OffRecordsNothing`: under `enforce: off`, and again under `lessons: off`, 10 matching calls
  open 0 fires and write 0 lines.
- `TestLobeTrial_AfterTheAppend`: on both post-tool events the event line is whole in the log before the
  verdict line is written: 2 of 2.
- `TestLobeTrial_DroppedOutcomeIsJudged`: a `fail` outcome whose state is `dropped` closes its open fire with
  the verdict `hit`, and no event line exists.
- `TestLobeTrial_StarterFireRecorded`: a starter rule that C-08's matcher selects writes 1 trial line; under
  `recall: off` it prints nothing and still writes the line.
- `TestLobeTrial_WorkingFixThatMatchesAvoid`: after a failure with a fingerprint the lesson lists, a call that
  matches the lesson's `avoid.pattern` and works is judged `false_alarm`. Feature 024 adds the second half of
  E-02's test, that the lesson is demoted.
- `TestLobeTrial_NoHitWithoutAvoidOrFingerprint`: a lesson with neither an `avoid` block nor a fingerprint
  earns 0 hits over 10 matching failed calls and stays at its entry stage.
- `TestLobeTrial_BudgetDropsFire`: with the session write stalled 200 ms, the hook exits within 50 ms of its
  answer.
- `TestLobeLadder_CountsFromTheJournal` (named here): 3 hits in 2 sessions, 1 false alarm, 1 `pass` and 1
  `unknown` give `hits` 3, `false_alarms` 1 and `sessions` 2, with `ladder.json` byte-identical.
- `TestLobeSettle_ClosesOldFires` (named here): a fire 11 minutes old becomes 1 line with `unknown`; one 9
  minutes old stays open; a fire of a session that never came back is closed too.
- `TestLobeSettle_EntersAHandWrittenLesson` (named here): a lesson file with no entry gets one at its enter
  stage; an existing entry's stage is byte-identical after `Settle`.
- House rules, kept green or added:
  - Never stalls: the stall tests of feature 012 on `pre-tool-use` and both post-tool events, and
    `TestLobeTrial_BudgetDropsFire`.
  - Wrong client gets nothing: the client-rule tests of feature 003 and the leak suite of feature 013 at zero
    leaks. A hook that fails the client rule ends before this step, so it opens no fire in either store.
  - No secret on disk: `TestLobeTrial_NoCallText`; the call text passes S-05's scrubber before it is hashed.
  - The guard untouched: the import-rule tests of feature 003 (`internal/lobe/trial` imports no guard package
    and has no `init`), and every test under "Must not trade off" in `CLAUDE.md`, unedited.
  - No model in a hook: nothing here starts a process.

## Evaluation

Start from the standard tester setup (feature [002](002-sandbox.md)). `<acme>` is `$W/clients/acme` and
`<lobe>` is `$TQ_HOME/identities/acme/lobe`. Run every command inside `<acme>`, after
`export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`. Backticked text split over two lines is one
line: join it with a space. Write `<lobe>/lessons/l-4c7d19e2.md`:

```markdown
---
id: l-4c7d19e2
title: Alias the MERGE source
kind: lesson
source: person
scope: private
applies: client
origin: {by: me, host: person, drafted: person}
when: {tool: [Bash], command: [dbsql]}
avoid:
  pattern: '(?i)merge into \S+ using \('
  must_match: ["MERGE INTO t USING (SELECT 1)", "merge into dim using (select 2)"]
  must_not_match: ["MERGE INTO t USING s", "MERGE INTO t USING src AS s"]
strength: 1.0
seen: 3
created: 2026-11-09
---
## Do
Give the source an alias.
## Avoid
A subquery as the source.
## Why
The warehouse rejects it because the source has no alias.
```

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if it does not, stop and report
FAIL. A lesson that a person wrote enters at `advice`, so put it on trial by hand. Write `<lobe>/ladder.json`:
`{"v":1, "entries":[{"lesson":"l-4c7d19e2", "stage":"trial", "hits":0, "false_alarms":0, "sessions":0,
"approved_at":null}]}`.

- Input A, piped to `tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1",
  "hook_event_name":"PreToolUse", "cwd":"<acme>", "tool_name":"Bash", "tool_use_id":"t1",
  "tool_input":{"command":"dbsql -e 'MERGE INTO t USING (SELECT 1)'"}}`.
- Input B, piped to `tentaqles lobe-hook claude post-tool-use-failure`, is input A with `hook_event_name`
  `PostToolUseFailure` and the added key `"error":"source needs an alias"`.
- Input C, piped to `tentaqles lobe-hook claude post-tool-use`, is input A with `hook_event_name`
  `PostToolUse`, `tool_use_id` `t2` and the added key `"tool_response":{"stdout":"1 row"}`.

1. Input A: exit code 0, empty stdout; `<lobe>/sessions/s1.json` has 1 entry in `open_fires`, with
   `"by":"avoid"`.
2. Input B: exit code 0; `<lobe>/trials.jsonl` has exactly 1 line, with `"verdict":"hit"`; `open_fires` is
   empty. That line's `event` value is the `id` of a line with `"kind":"fail"` and `"session":"s1"` in today's
   file under `<lobe>/events/`.
3. Input A with `tool_use_id` `t2`, then input C: line 2 of `trials.jsonl` has `"verdict":"false_alarm"`.
4. Neither `trials.jsonl` nor `sessions/s1.json` contains the text `SELECT 1`.
5. `tentaqles lobe rules --json`: the entry for `l-4c7d19e2` has `"stage":"trial"`, `"hits":1` and
   `"false_alarms":1`.
6. Run `lobeset acme enforce off`, the setup's helper (`tentaqles lobe set` answers only to a person and no
   tester runs it). Input A with `session_id` `s2` and `tool_use_id` `t3`, then input B with the same two
   values: both exit with code 0 and empty stdout; `trials.jsonl` still has exactly 2 lines, and
   `<lobe>/sessions/s2.json` is absent or has no entry in `open_fires`. Then run `lobeset acme enforce advice`.
7. Input A with `session_id` `s3` and `tool_use_id` `t4`: `<lobe>/sessions/s3.json` has 1 entry in
   `open_fires`. Then `tentaqles lobe consolidate --session s3 --now "$(date -u -d '+11 minutes'
   +%Y-%m-%dT%H:%M:%SZ)"`: exit code 0; `trials.jsonl` has exactly 3 lines, the last with
   `"verdict":"unknown"`; `sessions/s3.json` has no entry in `open_fires`; `ladder.json` still has
   `"stage":"trial"`.
8. Pipe input A with `session_id` `s4` into `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles
   lobe-hook claude pre-tool-use`, so that this one process runs under the login of `globex`: exit code 0 and
   empty stdout; there is no `s4.json` under `<lobe>/sessions/` or under
   `$TQ_HOME/identities/globex/lobe/sessions/`, and `trials.jsonl` still has exactly 3 lines.
9. In `<lobe>/ladder.json` set `"stage":"advice"`. Input A with `session_id` `s5` and `tool_use_id` `t5`:
   stdout contains `Alias the MERGE source`. Then input B with the same two values: the last line of
   `trials.jsonl` has `"stage":"advice"`, `"act":"note"` and `"verdict":"hit"`, and `ladder.json` still has
   `"stage":"advice"`.

**Person checks**

None. No step of this feature answers only to a person.

Scoring: PASS needs 9 of 9. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`). The plugin did not change, so its suite only has to stay green.
- [ ] The tester brief passes, 9 of 9.
- [ ] The house rules hold: `TestLobeTrial_BudgetDropsFire` and feature 012's stall tests (never stalls),
  feature 013's leak suite at zero leaks (wrong client gets nothing), `TestLobeTrial_NoCallText` (no secret on
  disk), the import-rule tests and the unedited "Must not trade off" tests (the guard untouched).
- [ ] `tentaqles lobe bench latency` was run and its post-tool rows are inside the gates of core page 5.2.
- [ ] The stub episode of feature [022](022-repeat-mistake-benchmark.md) still reads `[1,0,0,0]`.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a `hit` line and a `false_alarm` line in `trials.jsonl` and the counts in
  `tentaqles lobe rules`.

## Risks and unknowns

- **`tool_use_id` on both sides of a call** is not confirmed for Claude Code (E-02, risks; phase 0 question
  2). Without it the fire is found by `tool` and the call hash, which is why both are stored.
- **The judge trusts Sense.** If feature 009 reads a failure as a success, a good rule gets a false alarm.
  Feature 041 counts it.
- **With no fingerprint listed, any failure of a matching call is a hit** for a `by: avoid` fire. The drafted
  lessons of features 019 and 025 should always fill `when.fingerprint`.
- **Every number here is proposed**: 5 fires per call, 20 per lesson per session, 25 ms, 30 ms, 10 minutes,
  180 days, 2 ms. Feature 012's check measures the two post-tool events both ways.
- **Readings made in this file**, each to confirm in task 1: "selects" in step 1; `act` for a lesson that was
  matched and not printed; `sessions` as distinct sessions with a hit; the fields `at` and `act` in an open
  fire; `Settle` as the place that trims `trials.jsonl`; the package split, in which `ladder` reads the journal
  through `trial` and `trial` never imports `ladder`.
- **The tampered-session exclusion** needs F-05's mark. If an earlier feature already built the mark, add the
  exclusion here instead of waiting for feature 034.
- **A false alarm changes nothing before feature 024.** Between the two features a wrong note is recorded and
  still shown. The index builds 024 next for that reason.
- What would force a change to this file: a phase 0 result showing that a failed call's result does not reach
  a post-tool event the lobe is registered for, or a `sense.Outcome` that differs from the shape cited above.
