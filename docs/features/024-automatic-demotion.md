# 024: A wrong note is taken back

**Status** not started · **Stage** Lessons · **Entries** E-07 (the phase 2 rows, the one writer of every stage
move, `ladder-log.jsonl`), E-01 (the person check, `rules demote`, `rules retire` as the mute, the step from
`trial` to `advice` by record, the `approve` that only refuses) · **Needs** 023 · **Feeds** 026, 030, 031, 033,
035, 038

## Result

A lesson that speaks and proves wrong once goes back to the silent trial, inside the hook that saw it, before
the next call. A second such demotion retires it. Nobody is asked and nothing is printed into the session.
`tentaqles lobe rules` shows where each rule stands and why it last moved. A person, and only a person, can
demote or retire a rule by hand.

```bash
cd "$W/clients/acme"
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
echo "$PRE" | tentaqles lobe-hook claude pre-tool-use     # the note is shown: the lesson is at advice
echo "$OK"  | tentaqles lobe-hook claude post-tool-use    # the call worked: a false alarm
tentaqles lobe rules                                      # the lesson is at trial, reason false_alarm
cat "$TQ_HOME/identities/acme/lobe/ladder-log.jsonl"      # one line for the move
tentaqles lobe rules retire l-4c7d19e2                    # in a person's own terminal only
```

## Scope

**In**

- `internal/lobe/ladder/demote.go` (the pure trigger table) and `apply.go` (lock, write, log): the only code
  that writes a stage. Every earlier writer of a stage is moved onto it.
- `ladder-log.jsonl`, one line per move, up or down.
- The moves of E-07's table that phase 2 can trigger: a false alarm at `advice`; the second false-alarm
  demotion; 3 false alarms at `trial` with 0 hits; a person's `demote` and `retire`.
- The move up from `trial` to `advice` on a clean record (E-01), applied by `ladder.Settle`.
- The catch-up in `Settle` for a demotion the hook could not write because the lock was busy.
- `ladder.Counts` limited to the lines since the lesson's last move.
- The pure rows `faded`, `replaced` and `replacement_undone` in `demote.go`, with their tests. Their triggers
  are wired by later features.
- `internal/lobe/person` and the person-only verbs `tentaqles lobe rules demote <id>` and
  `tentaqles lobe rules retire <id>` in `internal/cmd/lobe_rules.go`.
- `tentaqles lobe rules approve <id>` as phase 2 has it: it refuses everyone and changes nothing.
- `tentaqles lobe rules` showing the reason of each rule's last move.

**Out**

- The row "E-03 tests fail" (`tests_failed`): feature [037](037-rule-test-cases.md).
- The rows that name `block`, an approval or an override: a false alarm at `block`
  (`override_succeeded`, or `false_alarm` under a cap), `pattern_changed`, `idle`, the write of
  `approvals.json`, `approved_at`, the stale cap on a deny, the real `approve`, `allow-once` and the console
  read of `yes` (feature [038](038-approve-to-block.md); the rule tests themselves are feature
  [037](037-rule-test-cases.md)).
- `tentaqles lobe lessons confirm <id>` and its move with `why` `confirmed`, and the brief's lines about rules
  that moved (feature [026](026-session-brief.md)).
- The trigger for `faded`: `Settle` reading C-05's strength (feature
  [030](030-reinforcement-and-fading.md)).
- The triggers for `replaced` and `replacement_undone`: `Settle` reading C-06's chain (feature
  [031](031-replacing-a-lesson.md)).
- `retire` as the private mute of a shared lesson, with the file in the repository untouched (feature
  [033](033-share-a-lesson.md); the verb is the same, shared lessons do not exist yet).
- Lessons that enter at `trial` or `seen` on their own, and `tentaqles lobe lessons --waiting` (feature
  [035](035-backfill.md)).
- `tentaqles lobe lessons forget <id>` and what `Settle` does with an entry whose lesson file is gone. No
  feature in the index names the verb, and E-07's table has no row for it; see Risks.
- Codex (feature [036](036-lessons-in-codex.md)).

## What it builds on

- **023**: `trials.jsonl`; `trial.Judge` and the post-tool step that writes a verdict; `ladder.Counts`;
  `ladder.Settle` in the detached pass and in `tentaqles lobe consolidate` (it closes old fires, enters
  hand-written lessons and rewrites counts, and moves nothing yet).
- **017**: `internal/lobe/ladder` (`ladder.go`, `enter.go`, `moves.go` up to `advice`), `ladder.json`, the
  caps, the list `tentaqles lobe rules` with `--json`, and the rule that a lesson at `trial`, `seen` or
  `retired` is not shown.
- **019**: `ladder.Enter`, called when a lesson is filed. It writes a stage today; here it is moved onto
  `apply`.
- **014**: the starter rules, which hold ordinary ladder entries and move down like any rule (C-08 step 7).
- **003**: the store lock with its 50 ms wait for a hook and 5 s for a command or the pass, `ErrBusy`,
  `store.Update`, `store.Append`.
- **018**: `tentaqles lobe consolidate` and its `--json` action lines, which already name `retire`.
- **016**: the weak person rule for `lessons add` (refuse when `CLAUDECODE` is set). The stronger check is
  new here.
- Existing tentaqles code: `insideAgent` (`internal/cmd/run.go` L283) and `interactive`
  (`internal/cmd/reach.go` L437). Both are unexported, which is why the person-only verbs live in
  `internal/cmd/lobe_rules.go` and not in `internal/lobe/cli`.

## What it leaves for later features

- `apply`, the one writer: 026 calls it for `confirmed`, 030 for `faded`, 031 for `replaced` and
  `replacement_undone`, 038 for `approved` and the block rows. Feature 038 adds the write of `approvals.json`
  between the two files it writes today.
- `ladder.Demote(entry, why) Move`, pure: 038 adds its four rows, which brings `TestLobeDemote_Table` to the
  11 rows of E-07.
- `ladder-log.jsonl`: 026's brief lists the moves since the last brief; 031 reads whether a lesson was retired
  as `replaced`; 038 counts `override_succeeded` among the false-alarm demotions.
- `internal/lobe/person`: 026 (`lessons confirm`) and 038 (`approve`, `allow-once`) use the same check; 038
  adds the `yes` read from the console device.
- `internal/cmd/lobe_rules.go`: 038 replaces the refusing `approve` and adds `allow-once`.
- For feature 035: the move from `trial` to `advice` on a clean record is live from here. 035 adds the mined
  lessons that start at `trial`.

## How it works

Shared rules are cited, not repeated: the ladder is core page 5.5, the thresholds are E-01's table, a hit and
a false alarm are defined in E-02, the files are E-01's "files" row.

1. **One writer.** `apply` takes a move `{lesson, from, to, why, by}`. Under the store lock it reads
   `ladder.json`, does nothing unless the stored stage is `from`, sets the stage, sets `hits`, `false_alarms`
   and `sessions` to 0, replaces `ladder.json`, and then appends one line to `ladder-log.jsonl`:
   `{"at","lesson","from","to","why","by"}`. Applying a move twice changes nothing. No other code writes a
   stage: `ladder.Enter` now builds its first entry through `apply` with `why` `entered`, and the hook, the
   pass and the verbs below all call it. The requirements write the name in lower case; it is called from
   other packages, so the builder exports it or wraps it.
2. **`why` and `by`.** For a move down `why` is one of E-07's reasons: here `false_alarm`, `proved_wrong`,
   `person`, and for the rows wired later `faded`, `replaced`, `replacement_undone`. For a move up it is
   `entered` or `record` here (`confirmed` and `approved` come with features 026 and 038). `by` is `hook`,
   `settle` or `person`.
3. **The moves this slice triggers.**

   | Trigger | Seen by | Move | `why` |
   |---|---|---|---|
   | a `false_alarm` verdict on a rule stored at `advice` | the post-tool hook | to `trial` | `false_alarm` |
   | the same, when `ladder-log.jsonl` already holds a false-alarm demotion of this lesson | the post-tool hook | to `retired` | `proved_wrong` |
   | 3 false alarms at `trial` with 0 hits | `Settle` | to `retired` | `proved_wrong` |
   | at least 2 hits in at least 2 sessions and 0 false alarms at `trial` | `Settle` | to `advice` | `record` |
   | `tentaqles lobe rules demote <id>` | a person | one stage down | `person` |
   | `tentaqles lobe rules retire <id>` | a person | to `retired` | `person` |

   Every number is E-01's and is proposed. The number of earlier false-alarm demotions is counted from the
   log, because the ladder entry has no field for it.
4. **In the hook.** The post-tool step of feature 023 writes the verdict line first. When the verdict is
   `false_alarm` and the fire's stored stage is `advice`, the same process calls `apply`, waiting at most 50 ms
   (proposed) for the lock. If the lock is busy the hook leaves the stage as it is and exits; the verdict line
   is already written. Nothing is printed for a move: the next session's brief lists it (feature 026) and
   `tentaqles lobe rules` shows the reason.
5. **In `Settle`.** After the parts feature 023 built, `Settle` checks each entry's counts and moves a lesson
   at most once per run. E-07 fixes the order of all its checks: replaced, replacement undone, faded, tests,
   pattern, idle, counts. Only `counts` is wired here. It applies the three count rows of the table, and it
   catches up a demotion the hook missed: an entry at `advice` with a false alarm since its last move.
   `tentaqles lobe consolidate --json` prints a `retire` line with its `why` for a move to `retired`.
6. **Counts since the last move.** `ladder.Counts` now reads only the lines of `trials.jsonl` that were judged
   after the lesson's last line in `ladder-log.jsonl` and recorded at the entry's current stage. A lesson with
   no log line counts all its lines, as in feature 023.
7. **The way back** (E-07 step 3) is the first climb again from 0. A lesson at `trial` after a false alarm
   returns to `advice` on a clean record, or when a person confirms it (feature 026). A retired lesson does not
   return here.
8. **A person, not an agent** (E-01's table). `demote` and `retire` run only when all three hold: `CLAUDECODE`
   is unset; stdin and stdout are terminals; none of the 12 nearest parent processes is an agent's program.
   The verb in `internal/cmd/lobe_rules.go` gets the first two from `insideAgent` and `interactive` and hands
   them to `internal/lobe/person`, which adds the parent walk and returns the refusal. On Windows the walk uses
   `CreateToolhelp32Snapshot` from `golang.org/x/sys/windows`, already a dependency. When the process table
   cannot be read, the check refuses. There is no `--yes` flag and no typed `yes` for these two verbs. A
   refused verb exits non-zero, changes 0 bytes and says to run it in the person's own terminal.
9. **`demote`** moves one stage down: `advice` to `trial`, `trial` to `seen`. At `seen` or `retired` it changes
   nothing, exits 1 and names `tentaqles lobe rules retire`. **`retire`** moves any stage to `retired`: the
   lesson is silent on this machine from the next call, and its file is not touched. The id is a lesson id or
   a starter's catalog id. A lesson file with no entry yet is entered first and then moved.
10. **`approve` in phase 2** exists and refuses everyone, before any other check, the person check included.
    It prints `Blocking is not available yet in this version; <id> stays at advice.`, exits 1 and changes no
    file.
11. **The list.** `tentaqles lobe rules` adds to each rule the reason and the time of its last move, read
    from the log; `--json` carries them as `why` and `moved_at` (key names chosen here).
12. **Stale is not a move** (E-07 step 4). Nothing here writes a stage for staleness. The stale mark arrives
    with feature [032](032-stale-lessons.md).

## Where to read

| What | Where |
|---|---|
| Requirements | [E-07](../prd/lobe-enforce.md) in full; [E-01](../prd/lobe-enforce.md): the table rows "`trial` to `advice`", "`approve` in phase 2", "a person, not an agent", "mute", "down or out" and "files", and the "How to build it" paragraphs on packages and on `ladder.Enter`; [E-02](../prd/lobe-enforce.md) steps 3 to 5 |
| Requirements, shared | [core page](../prd/vertical-lobe.md) 5.1 (the fourth import rule), 5.3, 5.4 (ladder entry), 5.5, section 9 rule 7; [F-07](../prd/lobe-foundation.md) step 2 and the rows for `rules demote`, `rules retire` and `rules approve`; [F-01](../prd/lobe-foundation.md) steps 6 to 8 |
| Source to port (Trail-code, MIT): the person check | [`src/skills/person.ts` L98](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L98): terminals, `CLAUDECODE` and no agent among 12 parents ([L29](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L29)). Weak, not taken: [L70](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L70) gives no process table on Windows and [L102](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L102) reads that as "agent", so the check cannot pass there (read, not run) |
| Source, idea only (BASE: no code, no identifiers) | [`src/shadow/promote.rs` L222](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/promote.rs#L222): a promotion is undone by itself, with no question. Weakness not taken: [`src/shadow/promote.rs` L22](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/promote.rs#L22): a rate over a window is noisy, so we move on single events |
| Existing tentaqles code | `internal/cmd/run.go` L283 (`insideAgent`; L282 calls it "a convenience check, not a boundary"); `internal/cmd/reach.go` L437 (`interactive`); `internal/cmd/run_test.go` L277 and L325 (`TestRunOtherClient_RefusedForAgents`, `TestRunOtherClient_HumanAllowed`); `internal/testutil/fakeprog.go` (a test program under another name, for the agent-parent case); `internal/lobe/ladder`, `internal/lobe/trial` and `internal/lobe/cli` as features 017 to 023 left them |

## Build tasks

1. **Check this brief against the code.** Find every place that writes a stage today (`ladder.Enter`, anything
   in `moves.go`, test helpers aside) and list it. Check whether feature 017 already built the refusing
   `approve` and `TestLobeLadder_BlockNotAvailable`; if so, drop step 10 and its check from this feature. List
   what no longer fits.
2. **`ladder/demote.go`.** The pure table for the rows of Scope. Proof: `TestLobeDemote_Table`. Can run beside
   task 4.
3. **`ladder/apply.go`.** Lock, write, log, the from-stage rule. Move `ladder.Enter` onto it. Proof:
   `TestLobeDemote_OneWriter`, `TestLobeDemote_MoveTwiceChangesNothing`.
4. **`internal/lobe/person`.** The parent walk for Windows, Linux and macOS and the composed check. Proof:
   `TestLobePerson_RefusesAgents`. Can run beside task 2.
5. **The hook move.** The post-tool step calls `apply` on a `false_alarm` at `advice`. Proof:
   `TestLobeDemote_InTheHook`, `TestLobeDemote_BusyLock`, `TestLobeDemote_SecondFalseAlarmRetires`, and the
   second half of `TestLobeTrial_WorkingFixThatMatchesAvoid`.
6. **`Settle` and `Counts`.** The counts check, the catch-up, the cut at the last move. Proof:
   `TestLobeLadder_Thresholds`, `TestLobeSettle_PromotesOnRecord`, `TestLobeLadder_CountsSinceLastMove`.
7. **The verbs.** `demote`, `retire`, the refusing `approve`, and the reason in the list. Proof:
   `TestLobeDemote_AgentRefused`, `TestLobeLadder_RetireMutesHere`, `TestLobeLadder_BlockNotAvailable`,
   `TestLobeCommandTable` and `TestLobePersonVerbsRefuseInAgent` extended to the new rows.
8. **Latency.** Run `tentaqles lobe bench latency`; the post-tool event that demotes stays under the 1.5
   second cutoff and the event line is in the log.
9. **Docs.** `docs/reference/commands.md`: `demote`, `retire` and `approve` in the `tentaqles lobe rules`
   section, each marked as answering only to a person where it does; a line in `CHANGELOG.md`. The plugin does
   not change.
10. **Tester run.** A fresh agent runs the Evaluation below; the owner runs the person checks.

## Tests that stay in the repository

Names are the requirements' unless marked "named here".

- `TestLobeDemote_Table`: here the 7 rows this slice holds (false alarm at `advice`; second false-alarm
  demotion; 3 false alarms at `trial`; `faded`; `replaced`; `replacement_undone`; a person's `demote` and
  `retire`, counted as one row) give the 7 expected stages. Feature 037 adds the `tests_failed` row and
  feature 038 the last 3 of E-07's 11.
- `TestLobeDemote_InTheHook`: after 1 `false_alarm` at `advice`, the stage is `trial` before the hook exits.
- `TestLobeDemote_BusyLock`: with the lock held 500 ms the stage is unchanged at hook exit and `trial` after
  `Settle`.
- `TestLobeDemote_AgentRefused`: with `CLAUDECODE` set, `tentaqles lobe rules demote` changes 0 bytes.
- `TestLobeDemote_OneWriter`: over 50 moves, up and down, every stage change has exactly 1 line in
  `ladder-log.jsonl`.
- `TestLobePerson_RefusesAgents`: `CLAUDECODE` set, piped stdin, or an agent parent each refuse; 0 bytes
  change.
- `TestLobeLadder_Thresholds`: the records around the limits this slice applies (2 hits in 1 session, 2 hits
  in 2 sessions, 2 hits and 1 false alarm, 2 and 3 false alarms with 0 hits, 3 false alarms with 1 hit) give
  the expected stages. Feature 038 adds the records around the `advice` to `block` limit, up to E-01's 8.
- `TestLobeLadder_RetireMutesHere`: after `retire`, 0 notes on this machine and the lesson file is
  byte-identical. Here the lesson is private; feature 033 adds the shared file in a repository.
- `TestLobeLadder_BlockNotAvailable` (phase 2 only): `approve` prints its line, exits 1 and changes 0 bytes,
  and a hand-written `block` stage gives a note and no deny.
- `TestLobeTrial_WorkingFixThatMatchesAvoid`, second half: the lesson is demoted.
- `TestLobeDemote_SecondFalseAlarmRetires` (named here): demoted once, back at `advice`, a second false alarm
  gives `retired` with `proved_wrong`.
- `TestLobeDemote_MoveTwiceChangesNothing` (named here): the same move applied twice leaves `ladder.json`
  byte-identical after the first and adds 1 log line, not 2.
- `TestLobeSettle_PromotesOnRecord` (named here): 2 hits in 2 sessions and 0 false alarms at `trial` give
  `advice` with `why` `record` and `by` `settle`, and the three counts are 0.
- `TestLobeLadder_CountsSinceLastMove` (named here): lines judged before a move are not counted after it.
- House rules, kept green or added:
  - Never stalls: `TestLobeDemote_BusyLock`, and feature 012's stall tests on the post-tool events.
  - Wrong client gets nothing: feature 003's client-rule tests and feature 013's leak suite at zero leaks; a
    hook that fails the client rule writes no stage and no log line.
  - No secret on disk: a log line holds a lesson id, two stages, a reason and who moved it, and nothing of a
    call.
  - The guard untouched: the import-rule tests (`internal/lobe/person` and `internal/lobe/ladder` import no
    guard package and have no `init`); `TestRunOtherClient_RefusedForAgents` and
    `TestRunOtherClient_HumanAllowed` unedited; every test under "Must not trade off" in `CLAUDE.md`.
  - Testers are not people: `TestLobePersonVerbsRefuseInAgent`.

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
FAIL. "Write the ladder at STAGE" means: write `<lobe>/ladder.json` as `{"v":1,
"entries":[{"lesson":"l-4c7d19e2", "stage":"STAGE", "hits":0, "false_alarms":0, "sessions":0,
"approved_at":null}]}`. "Start clean" means: delete `<lobe>/ladder-log.jsonl` and `<lobe>/trials.jsonl`.

- Input A, piped to `tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1",
  "hook_event_name":"PreToolUse", "cwd":"<acme>", "tool_name":"Bash", "tool_use_id":"t1",
  "tool_input":{"command":"dbsql -e 'MERGE INTO t USING (SELECT 1)'"}}`.
- Input B, piped to `tentaqles lobe-hook claude post-tool-use`, is input A with `hook_event_name`
  `PostToolUse` and the added key `"tool_response":{"stdout":"1 row"}`.
- Input F, piped to `tentaqles lobe-hook claude post-tool-use-failure`, is input A with `hook_event_name`
  `PostToolUseFailure` and the added key `"error":"source needs an alias"`.
- "A then B as (S, T)" means: pipe input A and then input B, both with `session_id` S and `tool_use_id` T.

Write the ladder at `advice`, then set `"hits":3` and `"sessions":2` in it.

1. Input A: exit code 0; stdout contains `Alias the MERGE source` and no deny decision.
2. Input B: exit code 0; `ladder.json` has `"stage":"trial"`; `ladder-log.jsonl` has 1 line, with
   `false_alarm` and `"by":"hook"`.
3. `tentaqles lobe rules --json | grep -c '"why":"false_alarm"'` prints `1`: the list shows why the rule
   moved.
4. Input A with `session_id` `s2` and `tool_use_id` `t2`: exit code 0 and empty stdout. The lesson is silent
   on trial.
5. `CLAUDECODE=1 tentaqles lobe rules demote l-4c7d19e2`: exit code not 0, and `ladder.json` is unchanged.
   The same with `retire` in place of `demote`: exit code not 0, and `ladder.json` is unchanged.
6. `tentaqles lobe rules approve l-4c7d19e2`: exit code 1; the output contains `Blocking is not available
   yet`; `ladder.json` is unchanged and there is no `approvals.json`.
7. In `ladder.json` set `"stage":"advice"` by hand. A then B as (`s3`, `t3`): input A prints the title;
   after input B `ladder.json` has `"stage":"retired"`, and `ladder-log.jsonl` has 2 lines, the last with
   `proved_wrong`. Then input A with `session_id` `s4` and `tool_use_id` `t4`: exit code 0 and empty stdout.
8. Start clean and write the ladder at `trial`. A then B as (`s5`, `t5`), (`s6`, `t6`) and (`s7`, `t7`):
   every input A gives empty stdout, and after the third pair `ladder.json` still has `"stage":"trial"`. Then
   `tentaqles lobe consolidate --session s7 --json`: exit code 0; stdout has a line with `"action":"retire"`
   and `proved_wrong`; `ladder.json` has `"stage":"retired"`; `ladder-log.jsonl` has a line that holds
   `l-4c7d19e2`, `proved_wrong` and `"by":"settle"`.
9. Start clean and write the ladder at `trial`. Pipe input A and then input F, both as (`s8`, `t8`), then
   both as (`s9`, `t9`). `tentaqles lobe rules --json`: the entry for `l-4c7d19e2` has `"stage":"trial"` and
   `"hits":2`. Then `tentaqles lobe consolidate --session s9`: exit code 0; in `ladder.json` the entry for
   `l-4c7d19e2` has `"stage":"advice"` and `"hits":0`; `ladder-log.jsonl` has a line that holds
   `l-4c7d19e2`, `"why":"record"` and `"by":"settle"`.
10. Input A with `session_id` `s10` and `tool_use_id` `t10`: stdout contains `Alias the MERGE source`. The
    lesson earned its way back.

**Person checks**, for the owner to run in a terminal of their own with the tester's `HOME`, `USERPROFILE`
and `TQ_HOME` exported (`demote` and `retire` answer only to a person). The tester prepares each state and
pipes the inputs; the owner types the commands.

1. The tester writes the ladder at `advice`. `tentaqles lobe rules demote l-4c7d19e2`: exit code 0;
   `ladder.json` has `"stage":"trial"` and the three counts 0; the last line of `ladder-log.jsonl` has
   `"why":"person"`.
2. `tentaqles lobe rules retire l-4c7d19e2`: `ladder.json` has `"stage":"retired"`. The tester then pipes
   input A with `session_id` `s11`: exit code 0 and empty stdout.

Scoring: PASS needs 10 of 10 from the tester and 2 of 2 from the owner. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) on the three systems; the parent walk has a test on each. The plugin did not change, so
  its suite only has to stay green.
- [ ] The tester brief passes, 10 of 10, and the owner's two person checks pass.
- [ ] The house rules hold: `TestLobeDemote_BusyLock` and feature 012's stall tests (never stalls), feature
  013's leak suite at zero leaks, the import-rule tests, `TestLobePersonVerbsRefuseInAgent`, and the unedited
  "Must not trade off" tests.
- [ ] A search of the code finds one writer of a stage: `apply`. `TestLobeDemote_OneWriter` is green.
- [ ] The stub episode of feature [022](022-repeat-mistake-benchmark.md) still reads `[1,0,0,0]`.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a note taken back after one false alarm, and the reason in `tentaqles lobe rules`.

## Risks and unknowns

- **The person check is a hint, not a boundary** (E-01, risks). An agent with a shell can write `ladder.json`
  by hand. Doing so will set F-05's tamper mark once feature 034 builds it.
- **The parent walk** sees program names only on Windows, where `node.exe` hides an agent, and no marker for a
  Codex session is confirmed yet (E-01 and F-07, risks). Which names count as an agent's program is read from
  the Trail-code file above and from what features 003 and 010 captured in phase 0. Trail-code's Windows path
  is "read, not run": ours is new code and needs its own test on Windows.
- **A walk that fails refuses.** That is this file's choice, in line with Trail-code. It means a person on a
  system where the process table cannot be read cannot demote by hand; the hook's own demotion still works.
- **One false alarm can be chance** (E-07, risks): Sense misreads, or the call works once. The requirements
  accept a slow climb over a wrong note. Whether the second false-alarm demotion is the right point to retire,
  or the third, is an open question of E-07.
- **A lesson with no `avoid` block is never demoted by a hook** (E-02). The template lessons of feature 019
  have none. Such a note stays, right or wrong, until a person runs `demote` or `retire`. This is why the two
  verbs ship in the same feature as the automatic move.
- **An agent can cause a false alarm on purpose** and so silence a note (E-07, risks).
- **`demote` below `trial`.** E-07 says "one stage down" and no more. This file reads it as `trial` to `seen`
  and a refusal at `seen`. The owner may prefer `trial` to `retired`.
- **`lessons forget`.** F-07 says E-07 retires the ladder entry of a deleted lesson in the next pass, but the
  index names no feature for the verb and E-07's table has no row or reason for it. Until that is settled,
  `Settle` leaves an entry whose file is gone as it is; the lesson cannot be shown anyway.
- **A crash between the two writes** leaves a stage with no log line. The log is then one line short; the
  stage is still right. `Counts` falls back to the earlier cut and may count a few old lines.
- **Names and readings chosen here**: `why` and `moved_at` in the list, the `from` of a first entry (empty),
  the `by` of an entry made in the pass (`settle`), and `Counts` cutting by judged time and stage.
- What would force a change to this file: feature 017 having already built `apply`, the log or the person
  check; or an owner answer on `demote` below `trial`.
