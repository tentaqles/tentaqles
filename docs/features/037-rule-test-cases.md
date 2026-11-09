# 037: Test cases on every rule

**Status** not started · **Stage** Blocking · **Entries** E-03 (all but its use by `approve`), E-07 (the row
for failed tests), C-04 (the `pattern` check moves) · **Needs** 036 · **Feeds** 038, 039, 041

## Result

Every `avoid` block carries examples: calls its pattern must match (`must_match`) and calls it must not
(`must_not_match`). Anyone can run them and see each rule pass or fail, with the failing example named. A rule
that fails its own examples is moved to `trial` at once, where it is silent in Claude Code and in Codex, and
`tentaqles lobe rules` shows the reason `tests_failed`.

```bash
cd "$W/clients/acme"
tentaqles lobe rules test                        # every rule with an avoid block; exit 1 when any fails
tentaqles lobe rules test l-4c7d19e2             # one rule
tentaqles lobe rules test l-4c7d19e2 --replay    # also against the client's stored events
tentaqles lobe rules --json                      # "stage":"trial" after a failure
```

## Scope

**In**

- Package `internal/lobe/rulecheck`: `check.go` (`Run(avoid, level) Result`), `everyday.go` (the embedded list
  of 50 everyday calls), `replay.go`.
- E-03 steps 1 to 3 (shape, examples, breadth) and step 5 (the whole call text), with E-03's numbers. All of
  them are proposed.
- E-03 step 4 (replay over stored events) as code, reachable through `--replay`. The level that asks for at
  least 2 examples of each kind exists in `Run`; nothing calls it yet.
- The gate (C-04) calls `rulecheck.Run` for its `pattern` check. The gate's own copy of that check and its
  `safe_commands.txt` go away; the list lives in `everyday.go`.
- `ladder.Settle` runs steps 1 to 3 after every turn for every lesson with an `avoid` block.
- `tentaqles lobe rules test [<id>] [--replay]`, for anyone, exit 1 when any test fails.
- The E-07 row "E-03 tests fail": the move to `trial` with the reason `tests_failed`, seen by `Settle` and by
  `tentaqles lobe rules test`, and the failing example in `ladder-log.jsonl`.
- Both hosts: a rule moved to `trial` is silent in a Claude Code and in a Codex session.

**Out**

- `tentaqles lobe rules approve` running steps 1 to 4 before it asks, and "at least 2 of each" as a condition
  of the block step (feature [038](038-approve-to-block.md)).
- Removing an approval when tests fail (038; no approval exists before it).
- A sure-fail rule's examples run through `surefail.Decide` (feature [039](039-sure-fail-block.md)).
- The benchmark row for a rule's own examples (feature [041](041-enforcement-accuracy.md)).
- Matching only what a command runs, so that a quoted mention is not a match. E-01 leaves it for a later
  version; no feature takes it.

## What it builds on

- 002: the sandbox and the standard tester setup. 003: the store, its lock, `sense.CallText`, S-05's scrubber
  and the event day files. 011: the retention window that replay reads inside.
- 005: `internal/lobe/cli`, where commands that anyone may run live, and the docs test for commands.
- 014: the starter catalog in `internal/lobe/starters`, whose entries carry `must_match` and `must_not_match`
  and are checked by `TestLobeStartersCompileAndSelfTest`.
- 016: `internal/lobe/lesson`, `internal/lobe/gate` with the `pattern` check and the embedded
  `safe_commands.txt`, and `tentaqles lobe lessons gate`. 029: the Portuguese word lists, untouched here.
- 017: `internal/lobe/ladder` up to `advice` (`ladder.go`, `enter.go`, `apply.go`), R-04's matcher, and the
  code that tries an `avoid.pattern` on the call text for R-04's order. E-03 calls it
  `ladder.Match(pattern, callText)`. The earlier briefs do not name it: task 1 finds it, and this feature
  exports it under that name if it is not.
- 023: `ladder.Settle`, `ladder.Counts`, `trials.jsonl`. 024: `demote.go` with `TestLobeDemote_Table` at 7
  rows, `apply` (the only writer of a stage, taking a move `{lesson, from, to, why, by}`),
  `ladder-log.jsonl`, and the reason of the last move in `tentaqles lobe rules`.
- 034: intake runs the gate on a pulled lesson, so it follows the gate onto `rulecheck` with no change.
- 036: the Codex hooks, used by the evaluation.

## What it leaves for later features

- `rulecheck.Run(avoid, level) Result`: 038 calls the promotion level from `approve`; 041 treats a case whose
  rule fails it as an error in the set.
- `rulecheck` replay (`replay.go`): 038 makes it a condition of the step to `block`.
- `everyday.go`: the one list of everyday calls for every later check of a pattern (038, 039).
- `tentaqles lobe rules test`: 039 adds the sure-fail path for a catalog id; 041 replays a rule's own
  examples in a row of their own.
- The E-07 move `tests_failed`: 038 widens it to remove the approval.

## How it works

1. **One matcher.** `rulecheck` has no matcher of its own. Every step calls `ladder.Match(pattern, callText)`,
   so a test and the hook cannot disagree (E-03, How to build it).
2. **What an example is.** A call text as E-01 step 1 defines it: the command string for a shell tool, the
   compact JSON of the tool input with sorted keys for any other tool. The same shape serves both hosts,
   because Codex's shell tool is also named `Bash` (S-02).
3. **Step 1, shape.** The pattern compiles with Go's `regexp`, has at most 200 characters and does not match
   the empty string. Each example has at most 1,000 characters and comes out of S-05's scrubber unchanged. An
   example that holds a secret fails here (E-03 step 1).
4. **Step 2, examples.** The pattern matches every `must_match` and no `must_not_match`. At most 5 of each are
   read. The creation level needs at least 1 of each; the promotion level needs at least 2 of each (E-03
   step 2). C-04's `form` check already refuses a block with none.
5. **Step 3, breadth.** The pattern matches none of the 50 everyday calls in `everyday.go`. The list and the
   200 characters are the ones the gate shipped in 016, moved, not changed (E-03 step 3).
6. **Step 5, whole call text.** Every step tries the pattern on the whole text. A `must_not_match` line that
   only quotes the avoided form is therefore a match and fails step 2 (E-03 step 5).
7. **The gate moves.** `gate.Check` calls `rulecheck.Run` at the creation level for its `pattern` row and
   reports the result under the same name, `pattern:`. Nothing else in the gate changes, so `lessons add`,
   drafting (C-03), `share` and intake (F-04) all run the new code with no edit of their own.
8. **After every turn.** `ladder.Settle` runs the creation level for every lesson that has an `avoid` block.
   That covers an edit by hand and a lesson pulled through git. In Settle's order the tests come after
   replaced, replacement undone and faded, and before pattern, idle and counts (E-07, How to build it). A
   lesson that fails is not promoted by its counts in the same run.
9. **On request.** `tentaqles lobe rules test [<id>]` reads the lesson file itself, not the hooks' index, so it
   sees an edit at once. With no id it runs every lesson that has an `avoid` block. It prints one line per
   rule, `<id>: ok` or `<id>: <step>: <message>`, as the gate prints `<check>: <message>`. The message names
   the key (`must_match`, `must_not_match`), the failing example or everyday call, and what was expected. A
   lesson with no `avoid` block passes. A catalog id (a starter rule) runs that entry's examples through the
   starter's own matcher, `starters.MatchCommand`, the code 014's self-test uses. An entry with no `pattern`
   has no examples and passes.
10. **Replay.** With `--replay` the command also runs step 4: the pattern over the newest 5,000 stored events
    inside the retention window (S-07). The call text is rebuilt from each event's `input` (for a shell tool,
    its `command` value). An event whose `input` was cut at the 4,000-character cap is skipped, and so is a
    line that only refers to an earlier one (S-06). The pattern must match at least 1 `fail` event and 0 `ok`
    events. With no stored failure the line reads `no stored failure matches`. A `hidden-fail` event counts
    as neither. A replay failure sets the exit code and makes no ladder move: replay is a condition of
    promotion only.
11. **A failure moves the rule.** When steps 1 to 3 fail in `Settle` or in `rules test`, E-07's `apply` moves a
    rule at `advice` to `trial` with the reason `tests_failed`. A rule already at `seen`, `trial` or `retired`
    is not moved. The log line carries one more key, `detail`: the step, the key, the example after S-05's
    scrubber and cut at 1,000 characters, and `expected` (`match` or `no match`). 024's move gains that
    optional field. `by` is `settle` on both paths, because the command runs Settle's own check for that
    lesson.
12. **The way back** is the first climb again: E-01's record at `trial`, or `tentaqles lobe lessons confirm`.

## Where to read

| What | Where |
|---|---|
| Requirements | [E-03](../prd/lobe-enforce.md), whole entry; [E-07](../prd/lobe-enforce.md), the trigger table row "E-03 tests fail" and "How to build it"; [C-04](../prd/lobe-consolidate.md), the `pattern` row and the last "How to build it" items; [E-01](../prd/lobe-enforce.md) steps 1 and 2 (call text); [core page 5.4](../prd/vertical-lobe.md) |
| Idea only (BASE, no code, no identifiers) | [`src/domain/rules.rs` L517](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L517); [`src/domain/rule_test.rs` L422](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rule_test.rs#L422); the gap we close: [`src/domain/rule_test.rs` L314](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rule_test.rs#L314) |
| Fact to verify | https://pkg.go.dev/regexp (web, not machine-checked): matching time is linear in the input |
| Lobe code from earlier features | `internal/lobe/gate` (the `pattern` check, `safe_commands.txt`), `internal/lobe/ladder` (`Match`, `Settle`, `apply.go`, `demote.go`), `internal/lobe/sense/calltext.go`, `internal/lobe/cli` |
| Existing tentaqles code | `test/integration/docs_test.go` (`TestDocsReferenceEveryCommand`), `docs/reference/commands.md` |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: the name and package of the
   matcher, where `Settle` lives, how 024's `apply` takes a reason, how the gate's `pattern` check is shaped.
   The owner approves changes to this file before code is written.
2. `internal/lobe/rulecheck/check.go` and `everyday.go`: steps 1 to 3 and the two levels. Move the 50 calls
   out of the gate's `safe_commands.txt`. Proof: `TestLobeRulecheck_Examples`, `_Breadth`,
   `_SameMatcherAsHook`, `_QuotedMentionRefuses`, `_SecretInExample`.
3. Side by side with 2: `internal/lobe/rulecheck/replay.go`. Proof: `TestLobeRulecheck_ReplayFindsSuccess`.
4. After 2: the gate calls `rulecheck.Run`; delete the gate's copy. Proof: `TestLobeRulecheck_SameAsTheGate`,
   and every `TestLobeGate*` test of 016 and 029 passes unedited.
5. After 2: `Settle` runs the check and hands a failure to `apply` with `tests_failed` and the `detail` key.
   Files: `internal/lobe/ladder` (or where 023 put `Settle`), `demote.go`. Proof:
   `TestLobeRulecheck_SettleDemotes`, the `tests_failed` row of `TestLobeDemote_Table`.
6. After 3 and 5: `tentaqles lobe rules test` in `internal/lobe/cli`, registered in `internal/cmd/lobe.go`.
   Proof: `TestLobeCommandTable`, the evaluation's checks 1 to 9.
7. Docs: the `rules test` action in the `tentaqles lobe rules` section of `docs/reference/commands.md`; a line
   in `CHANGELOG.md` under `[Unreleased]`. The plugin does not change.
8. Tester run of the Evaluation below.

## Tests that stay in the repository

- `TestLobeRulecheck_Examples`: 6 patterns with known answers give 6 of 6 expected results.
- `TestLobeRulecheck_Breadth`: `(?i)git` fails and names `git status`; the core page's sample pattern (5.4)
  matches 0 of 50.
- `TestLobeRulecheck_SameMatcherAsHook`: for 50 pairs of pattern and text, `rulecheck` and the hook agree 50
  times.
- `TestLobeRulecheck_SameAsTheGate`: for 20 `avoid` blocks, the gate's `pattern` check and steps 1 to 3 give
  the same 20 answers.
- `TestLobeRulecheck_ReplayFindsSuccess`: 1 stored `ok` event that matches fails the promotion level: yes.
- `TestLobeRulecheck_QuotedMentionRefuses`: a `must_not_match` line that quotes the avoided form fails step 2.
- `TestLobeRulecheck_SecretInExample`: an example holding a cloud access key fails at step 1.
- `TestLobeRulecheck_SettleDemotes`: an edited `must_match` that the pattern misses leaves the rule at `trial`.
- `TestLobeRulecheck_LogHoldsNoSecret` (this file's name): after a failure on an example that holds a marker
  key, `ladder-log.jsonl` holds the marker 0 times.
- `TestLobeDemote_Table` gains the `tests_failed` row: 8 of E-07's 11. `TestLobeDemote_OneWriter` stays
  green: the move has exactly 1 log line.
- Kept green and unedited: `TestLobeStartersCompileAndSelfTest`, every `TestLobeGate*` test,
  `TestLobeTriggerCommandTable`, `TestDocsReferenceEveryCommand`, `TestLobeCommandTable`.
- House rules: `TestLobeGuardNeverImportsLobe` and `TestLobeImportsOnlyTheFour` (the new package has no `init`
  and no package-level call; the list is embedded data), `TestLobeHookAlwaysExitsZero`,
  `TestLobeHookServesOnlyItsLogin`, the leak suite of 013 at zero leaks, and every test under "Must not trade
  off" in `CLAUDE.md` unedited.

## Evaluation

Run the standard tester setup of [002](002-sandbox.md), which is F-07's block. It gives `$W`, the
clients `acme` and `globex`, and the helper `lobeset`. `<acme>` is `$W/clients/acme` and `<lobe>` is
`$TQ_HOME/identities/acme/lobe`. Run every command inside `<acme>`. Backticked text split over two lines is
one line: join it with a space. No network. Write `<lobe>/lessons/l-4c7d19e2.md`:

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
FAIL. "Reset" means: restore this file, write `<lobe>/ladder.json` as `{"v":1,
"entries":[{"lesson":"l-4c7d19e2", "stage":"advice", "hits":0, "false_alarms":0, "sessions":0,
"approved_at":null}]}` and delete `<lobe>/ladder-log.jsonl`. Reset before check 1 and after every check.

1. `tentaqles lobe rules test l-4c7d19e2`: exit code 0, and the output contains `ok`.
2. Set `must_not_match: ["MERGE INTO t USING (SELECT 2)"]`, run it again: exit code 1; the output names
   `must_not_match`. Then `tentaqles lobe rules --json` has `"stage":"trial"` for `l-4c7d19e2`, and
   `ladder-log.jsonl` has 1 line, which contains `tests_failed`.
3. Set `pattern: '(?i)git'` and `must_match: ["git push"]`, run it again: exit code 1; the output names
   `git status`. Then `tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md`: exit code 1 and a line that
   starts with `pattern:` and names `git status`. The gate and the command run the same check.
4. Set `pattern: 'merge ('`, run it again: exit code 1; the output contains `compile`.
5. Delete the `must_not_match` line, run it again: exit code 1; the output names `must_not_match`.
6. Set `must_not_match: ["echo 'MERGE INTO t USING (SELECT 1)'"]`, run it again: exit code 1; the output names
   `must_not_match`. A quoted mention is a match, because the pattern sees the whole call text.
7. `tentaqles lobe rules test l-4c7d19e2 --replay` on the restored file: exit code 1; the output contains
   `no stored failure matches`. Then `tentaqles lobe rules --json` still has `"stage":"advice"`: a replay
   failure moves nothing.
8. Add a third `must_match` entry made of `AKIA` and 16 upper-case letters and digits that you make up. Run
   `tentaqles lobe rules test l-4c7d19e2`: exit code 1. `grep -c AKIA <lobe>/ladder-log.jsonl` prints `0`.
9. The pass finds an edit. Set `must_match: ["MERGE INTO t USING (SELECT 1)", "MERGE INTO t USING s"]` and
   `must_not_match: ["MERGE INTO t USING src AS s"]`, then run `tentaqles lobe consolidate`. Expect
   `"stage":"trial"` in `tentaqles lobe rules --json` and a line with `tests_failed` in `ladder-log.jsonl`.
10. Do not reset after check 9. Run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"` and pipe
    `{"session_id":"s1", "hook_event_name":"PreToolUse", "cwd":"<acme>", "tool_name":"Bash",
    "tool_use_id":"t1", "tool_input":{"command":"dbsql -e 'MERGE INTO t USING (SELECT 1)'"}}` to
    `tentaqles lobe-hook claude pre-tool-use`: exit code 0 and empty stdout.
11. Run `export CODEX_HOME="$TQ_HOME/identities/acme/codex"` and pipe the same JSON with `session_id` `s2` to
    `tentaqles lobe-hook codex pre-tool-use`: exit code 0 and empty stdout.
12. Reset, then pipe check 10's JSON with `session_id` `s3` to the Claude Code hook: stdout contains
    `Alias the MERGE source`. The rule speaks again only because the reset put it back by hand.

**Person checks.** None. No step of this feature needs a person-only command.

PASS needs 12 of 12.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin suite is untouched.
- [ ] The tester brief passes, 12 of 12.
- [ ] The house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`, `TestLobeRulecheck_LogHoldsNoSecret`, the
  leak suite at zero leaks, and the guard's tests unedited.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen `tentaqles lobe rules test` fail a rule and `tentaqles lobe rules` show it at `trial`.

## Risks and unknowns

- **An import cycle as the entries name things.** E-03 puts the matcher in `ladder` and has `ladder.Settle`
  call `rulecheck`; E-07 lists `rulecheck` among the callers of `apply`; C-04 has the gate call `rulecheck`.
  `ladder` importing `rulecheck` while `rulecheck` imports `ladder` does not compile. Keep `rulecheck` a leaf
  that imports only the matcher, return a `Result`, and let the callers (`Settle`, the command) apply the
  move, with the check handed to `Settle` as a function value where needed. Task 1 settles the wiring against
  what 023 and 024 built.
- **`regexp` is linear** is a web fact, not machine-checked. `TestLobeHookAlwaysExitsZero` and the latency
  checks of 012 are the guard if it does not hold for some pattern.
- **Every number is proposed**: 200 and 1,000 characters, 5 examples, 50 everyday calls, 5,000 events.
- **The `detail` key** is this file's reading of E-03 step 7. E-01's log line has six keys and none for an
  example. A change of that shape changes task 5 and check 8.
- **Replay is strict**: one matching `ok` event anywhere in the client fails it, even in a call the lesson's
  `when` block does not match. E-03 step 4 says so; M-05 (041) will show the cost.
- **Replay can miss a success of a non-shell tool**, because S-03 stores a projection of the input (E-03,
  risks).
- **A starter's examples.** C-08 says they are run by a self-test "until E-03 ships"; E-03 does not say how a
  catalog entry is tested. This file keeps the starter's own matcher and reports the gap.
- Go's `regexp` has no lookahead, so some honest rules cannot be written and stay at `advice` (E-03, risks).
