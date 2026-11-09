# 038: Approve a rule to block

**Status** not started · **Stage** Blocking · **Entries** E-01 (the block step, the deny, the override, the
caps), E-07 (the rows that name `block`, an approval or an override), E-02 (`act` `deny` and `pass`), R-06
(a stale rule never denies) · **Needs** 037 · **Feeds** 039, 040, 041, 051, 055

## Result

A lesson with a clean record at `advice` can be made to block. The person runs `tentaqles lobe rules approve`
in their own terminal, reads the pattern, the `when` block and the words that will be enforced, and types
`yes`. From then on a matching call is denied before it runs, in Claude Code and in Codex. The agent is told
which rule stopped it, why, the usual fix, and the one command with which the person lets one call through.
One wrong block sends the rule back to `advice` and removes the approval.

```bash
tentaqles lobe rules approve l-4c7d19e2       # a person only: prints what will be enforced, asks for yes
tentaqles lobe rules allow-once l-4c7d19e2    # a person only: the next matching call within 10 minutes passes
tentaqles lobe rules --json                   # "stage":"block"
```

## Scope

**In**

- `internal/lobe/ladder/approve.go` and `deny.go`; the file `approvals.json`.
- `tentaqles lobe rules approve <id>` for a lesson: the person check with the typed `yes` read from the
  console, the refusal of `anyway: true`, the whole gate again, the five conditions, E-03 steps 1 to 4.
- The deny at PreToolUse on both hosts and its text of at most 600 characters.
- `tentaqles lobe rules allow-once <id>` and the grant in `override_until`.
- The caps on a stored `block` rule: `enforce`, `lessons`, `recall` (a deny survives `recall: off`) and
  stale (a stale rule never denies, and a missing flag counts as stale).
- E-07's rows for `block`: a false alarm at `block`, failed tests removing the approval, a changed pattern or
  `when` block, 90 idle days, `demote` and `retire` from `block`, and an undone replacement of a lesson that
  held `block`.
- E-02's part: a fire records `act` `deny` or `pass`, and an overridden call is judged.
- Removing the phase 2 stand-ins: the `approve` that only refused, `TestLobeLadder_BlockNotAvailable`, and
  the reminder of `tentaqles lobe set` that `enforce: block` acts as advice.

**Out**

- A deny with no typed yes, for a built-in sure-fail rule (feature [039](039-sure-fail-block.md)).
- `approve` for a starter rule that is not sure-fail. C-08 step 8 allows it "like a lesson", but no entry
  says what `approvals.json` holds for a catalog entry. Here `approve <catalog id>` refuses in one line. No
  feature takes it; see the final report.
- The check before a commit, which never denies (feature [040](040-check-before-commit.md)).
- Measuring wrong blocks (feature [041](041-enforcement-accuracy.md)). The stale mark by symbol (feature
  [055](055-stale-by-symbol.md)).
- An approval that ends after a number of denies: an open question of E-01, not built.

## What it builds on

- 002, 003, 010, 036: the sandbox, `internal/lobe/hook` (`Answer{Context, Deny}`, `emit`, the client rule),
  the store lock, `sense.CallText`, the scrubber, and the Codex hooks.
- 005: the `enforce` setting and `tentaqles lobe set` with its reminders (F-06 step 6).
- 014 and 017: `internal/lobe/safety` (`Wrap`, the frame), R-04's matcher and note, `internal/lobe/ladder`
  (`ladder.go`, `enter.go`, `moves.go` up to `advice`), `ladder.Match`, and the caps as they act up to
  `advice`. 016: the gate and the `anyway` key.
- 023: fires in `sessions/<session>.json`, the verdict table, `trials.jsonl`, `ladder.Counts`,
  `ladder.Settle`.
- 024: `demote.go`, `apply` with its move `{lesson, from, to, why, by}`, `ladder-log.jsonl`,
  `internal/lobe/person` (terminals, `CLAUDECODE`, the walk over 12 parent processes, with
  `CreateToolhelp32Snapshot` on Windows), and the verbs `rules demote` and `rules retire` in
  `internal/cmd/lobe_rules.go`. From 017, kept by 024: the `approve` that prints
  `Blocking is not available yet`, and `TestLobeLadder_BlockNotAvailable`.
- 026: `tentaqles lobe lessons confirm` and the brief, whose first line can say `<B> can block`.
- 031: the `replaced` and `replacement_undone` moves. 032: R-06's stale flag per lesson in `index/`, set by
  the detached pass. 034: a pulled lesson enters at `advice` with no approval, and the function that gives
  the effective width of a shared lesson (held to its repository until the person confirms it).
- 037: `rulecheck.Run` with its promotion level and replay, and the `tests_failed` move.

## What it leaves for later features

- `approvals.json` with E-01's shape, and one exported function that builds the approval record for a
  lesson: 041 writes the same files as fixtures and tests them against that function.
- `ladder` deny decision and the deny form through F-02's writer on both hosts: 039 reuses the form; 040
  drops its findings when a deny goes out; 051 must never produce one.
- The stale cap reading the cached flag: 055 changes how the flag is computed, not how it is read.
- `tentaqles lobe rules allow-once`, and the console read in `internal/lobe/person`.
- Fires with `act` `deny` and `pass` in `trials.jsonl`: 041's field measure reads them.

## How it works

Shapes, thresholds and the files are in E-01's table; this section gives the order and this slice's details.

1. **`approve`, in this order.** Any refusal prints one line that names the reason, exits 1 and changes no
   file.
   1. The person check: `CLAUDECODE` is unset, stdin and stdout are terminals, and none of the 12 nearest
      parent processes is an agent's program, Codex's included.
   2. The id names a lesson file with a ladder entry at `advice`. A catalog id is refused (see Scope).
   3. A lesson with `anyway: true` is refused. The whole gate (C-04) runs again with no bypass.
   4. The record: at least 5 hits at `advice` in at least 2 sessions and 0 false alarms, from
      `ladder.Counts`. The setting `enforce` is `block`.
   5. The lesson has an `avoid` block, and `rulecheck.Run` passes at the promotion level: steps 1 to 3 with
      at least 2 examples of each kind, and step 4, replay.
   6. It prints the pattern, the `when` block, the title, the first sentence under `## Why` and under
      `## Do`, then the test results, and asks. The word `yes` is read from the console device (`CONIN$` on
      Windows, `/dev/tty` elsewhere), never from stdin. There is no `--yes`.
   7. `apply` writes `ladder.json` (stage `block`, `approved_at`), then `approvals.json`, then a
      `ladder-log.jsonl` line with `why` `approved` and `by` `person`.
2. **`approvals.json`** holds, per rule, `lesson`, `pattern`, `when`, `title`, `why`, `do`, `approved_at` and
   `override_until`. A lesson with no `when` block stores `"when":{}`. It is compact JSON, local to this
   machine. An approval with no `block` entry in `ladder.json` is ignored. A shared lesson is approved the
   same way, and its approval never leaves this machine (F-04 step 10).
3. **The deny decision** runs first on `pre-tool-use` and ends the event (F-02 step 2). A stored `block` rule
   denies only when all of these hold:
   - `lessons` is on and `enforce` is `block`;
   - the call is where the lesson's `applies` says it counts, by 034's effective width for a pulled lesson;
   - the lesson's `when` block matches the call, when it has one (R-04's matcher);
   - the lesson's `avoid.pattern` equals the stored pattern byte for byte, and its `when` block equals the
     stored one as the same keys and values;
   - the stored pattern matches the whole call text, and that text was not cut at 4,000 characters;
   - the rule is not stale: it has no `touches` entry, or its cached flag in `index/` is present and clear.
   It reads lesson headers from the `index/` cache, as R-04 does, and no lesson prose. No check of the code
   runs here. When several rules would deny, the first in R-04's order (step 10) is the one named.
4. **The deny** is one JSON object on stdout with exit code 0, the same on both hosts (F-02's table). Its
   reason is E-01's sentence, at most 600 characters. TITLE, WHY and DO come from `approvals.json`, never from
   the lesson file, and `safety.Wrap` puts them in. N is the lesson's `seen`. When the text would pass 600
   characters, WHY is cut first, then DO, then TITLE; the rule id and the `allow-once` command are never cut.
   A deny is the only output of that run: no note, no commit finding.
5. **Caps.** A cap changes what the rule does on this call and rewrites nothing. Under `enforce: advice`, or
   while stale, the rule gives R-04's note, with R-06's mark when stale. Under `enforce: off` or
   `lessons: off` it does nothing and no fire is recorded. `recall: off` silences notes and leaves the deny.
6. **The fire.** After the answer is written, the fire is recorded as 023 records it, with the stored stage
   `block` and `act` `deny`, `note` (a cap held it) or `pass` (an override). A denied call never runs, so
   `Settle` closes its fire as `unknown`.
7. **`allow-once`.** After the person check (no typed yes), and only for a rule at `block` with an approval,
   it writes `override_until` as now plus 10 minutes. At the next call the rule would deny, while that time
   is in the future, the hook prints nothing at all, sets `override_until` to null under the store lock and
   records `act` `pass`. If the lock stays busy for 50 ms (proposed) the call still goes ahead and the grant
   stays. A call a cap already let through does not use the grant.
8. **Down from `block`** (E-07's table). The post-tool hook applies the first two under the lock; `Settle`
   applies them when the lock was busy, and the rest.
   - An overridden call that succeeds is a false alarm: to `advice`, approval removed, `override_succeeded`.
     One that fails is a hit, and the rule stays.
   - A call the rule only advised on under a cap, and that succeeds: to `advice`, approval removed,
     `false_alarm`. Both count toward "second false-alarm demotion: retired".
   - Tests fail (037): to `trial`, approval removed, `tests_failed`.
   - The pattern or the `when` block differs from the stored one: no deny at once, then at `Settle` to
     `advice`, approval removed, `pattern_changed`.
   - No deny and no hit for 90 days: to `advice`, approval removed, `idle`.
   - `rules demote` from `block` goes to `advice`; `rules retire` to `retired`. Leaving `block` always sets
     `approved_at` to null and removes the approval.
   - A lesson that held `block`, was replaced and comes back when the replacement is undone returns at its
     enter stage with no approval.
9. **Stale is not a move.** The rule keeps its stage and approval and denies again when the flag clears.
10. **Stand-ins removed.** `enforce: block` and a stored `block` stage no longer act as `advice`. The line
    `Blocking is not available yet` and F-06's reminder about it are deleted. The reminder that
    `recall: off` with `enforce: block` still denies stays. The checks in the tester briefs of 017 and 024
    that expect the refusal line were phase 2 checks and are retired with it.

## Where to read

| What | Where |
|---|---|
| Requirements | [E-01](../prd/lobe-enforce.md), the table rows from "`advice` to `block`" down, steps 1 to 4, "How to build it"; [E-07](../prd/lobe-enforce.md), whole table and steps 1 to 4; [E-02](../prd/lobe-enforce.md) steps 1 to 4 and the edge cases; [R-06](../prd/lobe-recall.md) steps 7 and 9; [F-05](../prd/lobe-foundation.md) step 6; [F-02](../prd/lobe-foundation.md) steps 2 and 6 and the output table; [F-06](../prd/lobe-foundation.md) step 6; [core page 5.4 to 5.6](../prd/vertical-lobe.md) |
| Source to port (Trail-code, MIT) | the person check: [`src/skills/person.ts` L98](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L98), 12 parents at [L29](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L29); the form of a deny: [`src/trace/guard-fast.ts` L25](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L25); a yes in chat never counts: [`src/claude/pre-bash-skills.ts` L231](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-skills.ts#L231); a changed rule stops enforcing: [`src/trace/guard-fast.ts` L79](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L79) |
| Weakness not taken | on Windows the source check cannot pass: [L70](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L70), [L102](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L102); the agent passes a stop by running again: [`src/claude/pre-bash-check.ts` L89](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L89) |
| Fact to verify | https://code.claude.com/docs/en/hooks (web, not machine-checked): the deny shape for Claude Code |
| Existing tentaqles code | `internal/cmd/run.go` L283 (`insideAgent`; L282 calls it "a convenience check, not a boundary"), `internal/cmd/reach.go` L437 (`interactive`), `internal/cmd/codexhook.go` L64 to L89 (the deny object Codex reads), `internal/cmd/run_test.go` L277 and L325, `go.mod` (`golang.org/x/sys`, for `CreateToolhelp32Snapshot`) |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: where 024 put the person
   check and the refusing `approve`, how 023 names `act`, whether `safety.Wrap` already has a form for a
   deny's words, what 032 named the stale flag. The owner approves changes before code is written.
2. `approve.go`: the pure builder of an approval record from a lesson, and the five conditions as one pure
   function. Proof: `TestLobeLadder_BlockNeedsAllFive`, `TestLobeLadder_AnywayNeverBlocks`,
   `TestLobeLadder_Thresholds`.
3. Side by side with 2: `deny.go`, the decision of step 3 and the text of step 4, with `safety.Wrap` widened
   for a deny's words. Proof: `TestLobeLadder_WholeCallText`, `_WhenMustMatch`, `_WhenKeysNarrow`,
   `_AppliesLimitsTheDeny`, `_ChangedWhenEndsApproval`, `TestLobeDenyReadsNoProse`.
4. After 3: the caps and the stale flag. Proof: `TestLobeLadder_Caps`, `TestLobeLadder_MissingStaleFlag`,
   `TestLobeStaleNeverDenies`, `TestLobeDemote_StaleNeverDenies`.
5. After 3: wire the decision into `pre-tool-use` for both hosts, ahead of the notes; record `act`. Proof:
   `TestLobeTrial_EveryStageRecorded`, `TestLobeHookNeverPrintsAllow`, `TestLobeHookAlwaysExitsZero`.
6. Side by side with 5: the grant. Hook side in `deny.go`; the verb `allow-once` in
   `internal/cmd/lobe_rules.go`. Proof: `TestLobeOverride_OneCall`.
7. After 2: the console read in `internal/lobe/person`, and the `approve` verb in
   `internal/cmd/lobe_rules.go`. Delete `TestLobeLadder_BlockNotAvailable`, as E-01 marks it "phase 2 only".
   Proof: `TestLobePerson_RefusesAgents`, `TestLobePersonVerbsRefuseInAgent`.
8. After 5: E-07's rows of step 8 in `demote.go` and `Settle`, with the write of `approvals.json` placed in
   `apply` between its two files. Proof: `TestLobeDemote_Table` with all 11 rows,
   `TestLobeDemote_UndoComesBackAtMostAdvice`, `TestLobeDemote_OneWriter`.
9. Remove F-06's reminder and the "acts as advice" branch of the `enforce` cap. Give 026's brief test a case
   in which one rule acts at `block` and line 1 says `1 can block` (R-05 step 4).
10. Docs: `approve` and `allow-once` in the `tentaqles lobe rules` section of `docs/reference/commands.md`;
    `docs/threat-model.md` gains the limits of the person check; `CHANGELOG.md`. Propose one "Must not trade
    off" line for `CLAUDE.md` (nothing learned denies without a typed yes here, and a wrong block is undone
    by one event) with its tests; the owner approves the wording. The plugin does not change.
11. Tester run of the Evaluation below, then the person checks with the owner.

## Tests that stay in the repository

- `TestLobeLadder_Thresholds`: 8 records around the limits give the 8 expected stages.
- `TestLobeLadder_BlockNeedsAllFive`: dropping each of the five conditions in turn gives 5 refusals.
- `TestLobeLadder_AnywayNeverBlocks`: refused with `anyway: true`, and after the key is deleted: 2 of 2.
- `TestLobeLadder_WholeCallText`: the approved pattern inside `echo "..."`, in a matching call, is denied.
- `TestLobeLadder_WhenMustMatch`: 1 deny and 2 passes (another tool; a folder outside `when.place`).
- `TestLobeLadder_WhenKeysNarrow`: 2 passes and 1 deny.
- `TestLobeLadder_AppliesLimitsTheDeny`: 2 denies and 2 passes.
- `TestLobeLadder_ChangedWhenEndsApproval`: no deny and no approval after `Settle`; an empty stored `when`
  denies: 2 of 2.
- `TestLobeLadder_MissingStaleFlag`: with `touches` and no flag, a note and no deny; without `touches`, a deny.
- `TestLobeLadder_Caps`: `enforce: advice`, `enforce: off`, `lessons: off` and a set stale flag each give 0
  denies and leave both JSON files byte-identical; `recall: off` alone gives 1 deny, and so does clearing the
  flag.
- `TestLobePerson_RefusesAgents`: `CLAUDECODE` set, piped stdin or an agent parent (a Claude Code and a Codex
  one) each refuse `approve` and `allow-once`; 0 bytes change.
- `TestLobeOverride_OneCall`: call 1 after a grant is not denied, call 2 is; an 11-minute-old grant passes 0.
- `TestLobeDemote_Table` (all 11 rows; this feature adds the last 3), `TestLobeDemote_StaleNeverDenies`,
  `TestLobeDemote_UndoComesBackAtMostAdvice`, `TestLobeDemote_OneWriter` (50 moves, 1 log line each).
- `TestLobeTrial_EveryStageRecorded`: fires at `trial`, `advice` and `block` give `act` `none`, `note`, `deny`.
- `TestLobeStaleNeverDenies`: 3 of 3 (flag set, no flag, flag cleared), approval unchanged throughout.
- `TestLobeDenyReadsNoProse`: editing `## Do` changes 0 decisions and 0 characters of the deny in 1,000 cases
  (proposed).
- `TestLobeGateAnywayStaysPrivate` gains its two `rules approve` cases.
- `TestLobeDenyOnBothHosts` (this file's name): the same approved rule gives the same deny object from
  `lobe-hook claude` and `lobe-hook codex`, and nothing under the other client's login.
- House rules kept green: `TestLobeHookNeverPrintsAllow`, `TestLobeHookAlwaysExitsZero` (a held lock and a
  slow decision give no deny), `TestLobeHookServesOnlyItsLogin`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, `TestLobeTrial_NoCallText`, `TestLobeCommandTable`,
  `TestDocsReferenceEveryCommand`, the latency and stall checks of 012, the leak suite of 013, and every test
  under "Must not trade off" in `CLAUDE.md` unedited.

## Evaluation

Run the standard tester setup of [002](002-sandbox.md), which is F-07's block.
`<acme>` is `$W/clients/acme` and `<lobe>` is `$TQ_HOME/identities/acme/lobe`. Run every command inside
`<acme>` after `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"` and
`export CODEX_HOME="$TQ_HOME/identities/acme/codex"`. Backticked text split over two lines is one line.
"Set `enforce` to X" means `lobeset acme enforce X`. No tester runs `tentaqles lobe set` or any other
person-only command, except to see it refuse. Write `<lobe>/lessons/l-4c7d19e2.md`:

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

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if not, stop and report FAIL. Run
`tentaqles lobe rebuild` now and after every change to a lesson file. Set `enforce` to `block`. Write the
approved state by hand. `<lobe>/ladder.json`: `{"v":1, "entries":[{"lesson":"l-4c7d19e2", "stage":"block",
"hits":5, "false_alarms":0, "sessions":2, "approved_at":"2026-11-09T10:00:00Z"}]}`. `<lobe>/approvals.json`:
`{"v":1, "approvals":[{"lesson":"l-4c7d19e2", "pattern":"(?i)merge into \\S+ using \\(",
"when":{"tool":["Bash"], "command":["dbsql"]}, "title":"Alias the MERGE source", "why":"The warehouse rejects
it because the source has no alias.", "do":"Give the source an alias.",
"approved_at":"2026-11-09T10:00:00Z", "override_until":null}]}`. Keep a copy of both files as "the approved
state". Input A, piped to `tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1",
"hook_event_name":"PreToolUse", "cwd":"<acme>", "tool_name":"Bash", "tool_use_id":"t1",
"tool_input":{"command":"dbsql -e 'MERGE INTO t USING (SELECT 1)'"}}`. Use a new `session_id` in each check.

1. Input A: exit code 0; stdout has `"permissionDecision":"deny"`, `The warehouse rejects it` and
   `tentaqles lobe rules allow-once l-4c7d19e2`, and the reason has at most 600 characters.
2. Input A piped to `tentaqles lobe-hook codex pre-tool-use`: the same three texts in stdout.
3. Input A with the command `dbsql -e 'MERGE INTO t USING s'`: exit code 0 and no deny decision.
4. Input A with the command `dbsql -e 'SELECT 1'; echo 'MERGE INTO t USING (SELECT 1)'`: a deny decision.
   Then with the command `echo 'MERGE INTO t USING (SELECT 1)'` alone: no deny decision, because no
   statement runs `dbsql`.
5. `CLAUDECODE=1 tentaqles lobe rules allow-once l-4c7d19e2`: exit code not 0; `approvals.json` is unchanged.
6. `echo yes | tentaqles lobe rules approve l-4c7d19e2`: exit code not 0; both JSON files are unchanged; the
   output does not contain `Blocking is not available yet`.
7. Set `enforce` to `advice`, then input A: no deny decision; stdout contains `Alias the MERGE source`. Set
   `enforce` to `off`, then input A: empty stdout. Both JSON files still equal the approved state.
8. Set `enforce` to `block` and run `lobeset acme recall off`, then input A: a deny decision. Run
   `lobeset acme recall on` and `lobeset acme lessons off`, then input A: empty stdout. Run
   `lobeset acme lessons on`.
9. Change the line under `## Do` to `Run it twice.`, then input A: a deny decision with
   `Give the source an alias` and without `Run it twice`.
10. Change `using` to `usin` in the lesson's pattern, then input A: exit code 0 and no deny decision.
11. Restore the lesson file. Input A with `tool_name` `Read` and `tool_input`
    `{"file_path":"MERGE INTO t USING (SELECT 1)"}`: no deny decision.
12. Set `when: {tool: [Bash], command: [dbsql, databricks]}` in the lesson file, then input A: no deny
    decision. Then `tentaqles lobe consolidate`: `ladder.json` has `"stage":"advice"`, `approvals.json` has no
    entry for the lesson, and `ladder-log.jsonl` has a line with `pattern_changed`.
13. Restore the lesson file and the approved state. With
    `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude"`, input A: exit code 0 and empty stdout.
14. An override that succeeds. Use the one `session_id` `ov` for this whole check. Delete `ladder-log.jsonl`
    and `trials.jsonl`. In `approvals.json` set `override_until` to the time 5 minutes from now in UTC, in
    the form of `approved_at`. Input A with `tool_use_id` `t3`: empty stdout. Input A again with
    `tool_use_id` `t4`: a deny decision, and `override_until` is `null`. Then pipe input A with
    `tool_use_id` `t3`, `hook_event_name` `PostToolUse` and the added key
    `"tool_response":{"stdout":"1 row"}` to `tentaqles lobe-hook claude post-tool-use`: `ladder.json` has
    `"stage":"advice"`, `approvals.json` has no entry for the lesson, and `ladder-log.jsonl` has a line with
    `override_succeeded`.
15. `<lobe>/trials.jsonl` holds a line with `"act":"pass"` and `"verdict":"false_alarm"`. Neither that file
    nor `<lobe>/sessions/ov.json` contains the text `SELECT 1`.

For checks 16 to 18, `K` is `r_a172cedc`, the key of the `app` repository in the standard setup (F-07). Write
`<lobe>/lessons/l-5d8e2a13.md` like the first lesson, with these differences: `id: l-5d8e2a13`,
`title: Quote the table name in COPY`, a line `repo: <K>`, a line `touches: ["etl/load.py"]`, the pattern
`'(?i)copy [a-z]+-[a-z]+ from'`, `must_match: ["COPY my-table FROM stdin", "copy a-b from stdin"]`,
`must_not_match: ["COPY t FROM stdin", "COPY \"my-table\" FROM stdin"]`, and the three sections
`Quote the table name.`, `A bare name with a dash.` and
`The loader fails because it reads the dash as a minus.` Its gate line must print `ok`. Add a `block` entry
for it to `ladder.json` and an approval to `approvals.json`, with its pattern, the same `when`, its title and
its two sentences. Input S is input A with `cwd` `<acme>/app` and the command
`dbsql -e 'COPY my-table FROM stdin'`. Run these three checks inside `<acme>/app`.

16. Delete the folder `<lobe>/index`. Input S: no deny decision, and stdout contains
    `Quote the table name in COPY`. A rule with a `touches` entry and no cached flag counts as stale.
17. `mkdir etl; touch etl/load.py; tentaqles lobe consolidate`, then input S: a deny decision.
18. `rm etl/load.py; tentaqles lobe consolidate`, then input S: no deny decision, and stdout contains
    `may be stale`. The entry for `l-5d8e2a13` in `ladder.json` still has `"stage":"block"` and its approval is
    still in `approvals.json`.

**Person checks**, for the owner, in a terminal of their own with the tester's `HOME`, `USERPROFILE`,
`TQ_HOME` and `CLAUDE_CONFIG_DIR` exported, inside `<acme>`. The tester prepares each state and pipes the
inputs; the owner types the commands. Checks 1 to 4 are run on Windows at least, where the source's check
cannot pass; the unit tests cover the other systems.

1. The tester restores the first lesson, deletes `approvals.json`, `trials.jsonl` and the second lesson,
   sets `enforce` to `block` and writes `ladder.json` with the entry at `"stage":"advice"`, the three counts 0
   and `"approved_at":null`. Input B is input A with `hook_event_name` `PostToolUseFailure` and the added key
   `"error":"source needs an alias"`, piped to `tentaqles lobe-hook claude post-tool-use-failure`. The tester
   pipes input A and then input B five times, with `tool_use_id` `p1` to `p5`, the first three pairs with
   `session_id` `pa` and the last two with `pb`. `tentaqles lobe rules approve l-4c7d19e2` prints the pattern,
   the `when` block, the title, the two sentences and the test results, and asks. Type `yes`: exit code 0;
   `ladder.json` has `"stage":"block"`; `approvals.json` holds the pattern and the `when` block.
2. Input A with `session_id` `pc`: a deny decision. `tentaqles lobe rules allow-once l-4c7d19e2`, then input A
   twice: the first has no deny decision, the second has one.
3. `tentaqles lobe rules retire l-4c7d19e2`: `ladder.json` has `"stage":"retired"`, and input A with
   `session_id` `pd` gives exit code 0 and empty stdout.
4. `tentaqles lobe set enforce block --yes`: the output does not say that blocking acts as advice.
5. In a real Claude Code session and a real Codex session of a client the owner names, with one rule
   approved there: ask the agent to run a matching command. The agent reports that the call was stopped and
   names the `allow-once` command. This is the only check of the deny shape against a live host.

PASS needs 18 of 18 from the tester and 5 of 5 from the owner.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) on the three systems; the plugin suite is untouched.
- [ ] The tester brief passes, 18 of 18, and the owner's person checks pass, 5 of 5.
- [ ] The house rules hold: `TestLobeHookNeverPrintsAllow`, `TestLobeHookAlwaysExitsZero`,
  `TestLobeHookServesOnlyItsLogin`, `TestLobeGuardNeverImportsLobe`, `TestLobeTrial_NoCallText`, the latency
  and stall checks, the leak suite at zero leaks, the guard's tests unedited.
- [ ] `docs/reference/commands.md`, `docs/threat-model.md` and `CHANGELOG.md` are updated, and the owner has
  decided on the `CLAUDE.md` line.
- [ ] The owner has typed `yes` once and has seen an agent stopped and let through.

## Risks and unknowns

- **The person check is a hint, not a boundary** (E-01, F-05). An agent with a shell can write
  `approvals.json` by hand. That sets the tamper mark of its session and does not undo the write.
- **Windows.** The parent walk sees program names only, so `node.exe` hides an agent. Person check 1 on
  Windows is what shows the check can pass there at all; in Trail-code it cannot (read, not run).
- **No Codex marker is confirmed.** Until 036's capture names one, a Codex agent is known by its parent
  process and by stdin not being a terminal. If neither holds on some system, stop and ask the owner.
- **The deny shape for Claude Code** is from a web page, not machine-checked. Person check 5 verifies it.
- **Every threshold is proposed**: 5 hits, 2 sessions, 10 minutes, 600 characters, 50 ms, 90 days.
- **This file's own choices**, where the entries are silent: the console device names; the order in which a
  long deny is cut; which rule is named when several match; that a call a cap let through does not use a
  grant; that `approve` refuses a catalog id. Each is in the final report.
- **Import order.** `approve` needs the gate and `rulecheck`, and the gate reaches `ladder` through
  `rulecheck`. Keep the gate and test calls in the verb (`internal/cmd/lobe_rules.go`) and the pure builder
  in `ladder`, as 037's risk note says.
- A `block` rule earns a false alarm only through an override or under a cap, so a wrong one can deny for a
  long time before anyone overrides it. Feature 041 is the measure of how often that happens.
- A stale `block` rule only advises until the next detached pass after the code is back (R-06, risks).
