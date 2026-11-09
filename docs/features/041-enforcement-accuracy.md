# 041: Enforcement accuracy benchmark

**Status** not started · **Stage** Blocking · **Entries** M-05; A-27 (who labels); M-08 (how the result is
kept) · **Needs** 039, 040 · **Feeds** 051

## Result

`lobebench enforce` replays a labeled set of tool calls through the real hook, for Claude Code and for Codex,
and writes how often the lobe was wrong. A wrong block is a correct call that was denied. A miss is a real
mistake that got neither a deny nor advice. A call that only quotes a mistake is counted in a row of its own.
The run fails when one correct call is denied. This is the gate that says whether blocking may be on.

```bash
go build ./bench/cmd/lobebench                      # the harness is not in a release
lobebench enforce --fixture merge --out "$W/m05"    # 7 cases, exit 0
lobebench enforce --out bench/results/<run-id>      # the labeled set, exit 1 on any wrong block
cat "$W/m05/summary.json"                           # good, wrong_blocks, bad, blocked, misses, mentions
```

## Scope

**In**

- `bench/enforce/cases.jsonl`, `bench/enforce/rules/*.md` (the lessons under test) and the package
  `bench/internal/enforce`; the action `lobebench enforce`.
- The labeled set at its proposed size: at least 300 good cases, 150 bad cases and 60 mentions across the
  four families of M-04, plus cases for the sure-fail block (E-04).
- How labels are made (M-05 step 5, author's call A-27): two labeling agents that wrote neither the rule
  under test nor its pattern each label every case; a person checks a random sample of 50 (proposed), drawn
  with a recorded seed, and every case on which the two agents disagree; the person's label stands; the
  report says who labeled what.
- Rule sources: learned (an approved `block` rule, or a lesson at `advice`), starter, sure-fail.
- Both hosts. A case names its host, because the JSON differs.
- The numbers of M-05 step 8, the mention row, the gate of step 9, and the row for a rule's own examples.
- The fixtures `merge` and `merge-mislabeled`.
- The second measure, from use (step 10): per rule, fires before a call that failed against fires before a
  call that succeeded, read from `trials.jsonl` of benchmark runs.

**Out**

- The graph's argument check (E-05). It joins this benchmark in feature
  [051](051-argument-check.md): cases that carry the PostToolUse JSON of an edit, replayed through
  `post-tool-use`, where a deny counts as a wrong block whatever the label, and E-05's two numbers
  (proposed). 041 builds the case shape and the summary so that 051 can add a rule source.
- How often a finding of the check before a commit (040) is wrong, and how often the gate's shape lists
  (F-05) reject an honest lesson. E-06 and F-05 expect both from M-05, and M-05 has no step for either. No
  feature takes them; see the final report.
- Runs with a real agent (features [022](022-repeat-mistake-benchmark.md) and
  [059](059-token-benchmark.md)). This benchmark needs none.
- The decision to release `enforce: block`. That is the owner's, with this result in hand.

## What it builds on

- 002: `tentaqles lobe sandbox`, which builds `acme` (with the Codex agent) and `globex` in a fresh home.
- 021: `lobebench` in `bench/cmd/lobebench`, built with `go build ./bench/cmd/lobebench`; `--fixture` and
  fixtures embedded from `bench/testdata`; an existing `--out` folder is refused; an `--out` inside the real
  `TQ_HOME` or a client's folder is refused; `"publishable":false` for a fixture run; `bench/internal/arm`,
  which writes settings into a sandbox manifest as a fixture; `tentaqles lobe bench check-claims`.
- 022: `bench/traps/<family>/<trap>/trap.yaml` (the rule and the error text of each of the 20 traps) and
  `lobebench repeat`.
- 023: `trials.jsonl`. 036: the form of a note on Codex.
- 037: `rulecheck.Run`, used to refuse a set whose rule fails its own tests.
- 038: the shapes of `ladder.json` and `approvals.json`, the deny, and the exported function that builds an
  approval record for a lesson. 039: the sure-fail rule and its behavior under `block` and `advice`.
- 040: nothing is replayed from it (see Scope); it is a need in the index because both ship before the gate.

## What it leaves for later features

- `bench/enforce/cases.jsonl` and the case shape: 051 adds argument-check cases; every wrong block found
  later, by anyone, is added as a permanent case (M-05 step 11).
- `bench/internal/enforce` (`Replay`, `Classify`, `Summarize`, `Field`): 051 adds the `post-tool-use` path;
  059 calls `Field` on the real runs of M-02.
- `summary.json` of an enforce run as a receipt for `tentaqles lobe bench check-claims` (M-08).
- E-05's rule that a language's notes are switched on only after M-05's two numbers are met (051).

## How it works

1. **A case** is one line of `bench/enforce/cases.jsonl`: `{"id":"sql-014","rule":"l-4c7d19e2",
   "stage":"block","host":"claude","label":"mention","why":"pattern inside a commit message",
   "hook":{...}}`. `hook` is the full PreToolUse JSON for that host, and `label` is `bad`, `good` or
   `mention`. `rule` is a lesson id under `bench/enforce/rules/` or a catalog id.
2. **Good cases are written to be hard** (step 3): the corrected form the lesson recommends, a near miss one
   token away, the same statement sent to another command or tool for which it is right, a folder outside
   `when.place`, a dry-run flag. **A mention** holds the avoided text without running it: in a quoted string,
   a commit message, a comment, a file body passed to `Write`, a search pattern for `grep` (step 4).
3. **Held out.** The cases are written before a rule's pattern is final and are never used to tune it. The
   rule writer does not see the cases; the case writer sees each rule's title and sections and the trap's
   description, not its pattern. The label of every case is fixed before the pattern is final, and a later
   change of label is listed in the report.
4. **Labels.** `bench/enforce/labels.jsonl` holds one line per case: `{"id","a","b","person","final"}`. `a`
   and `b` are the two agents' labels, made without sight of each other. `person` is null or the person's
   label. `final` is the agreed label, or the person's where one exists. `bench/enforce/review.json` records
   the seed; one function in `bench/internal/enforce` draws the sample of 50 from it, and the test calls the
   same function, so the sample cannot be picked by hand. The `label` in `cases.jsonl` equals `final`.
5. **Replay**, per case (step 6). The harness makes a fresh `TQ_HOME` with `tentaqles lobe sandbox`, sets
   `enforce` to `block` in the sandbox manifest as a fixture, and installs the rule at the case's stage by
   writing the lesson file, `ladder.json` and `approvals.json` directly, with E-01's shapes. The words in the
   approval come from the lesson under test, so a deny can be recognized by them. It never runs
   `tentaqles lobe rules approve` or any person-only command. A starter rule and the sure-fail rule need no
   files. It sets the host's login variable to the client's identity folder (`CLAUDE_CONFIG_DIR` or
   `CODEX_HOME`), pipes `hook` into `tentaqles lobe-hook <host> pre-tool-use` and keeps the exit code,
   stdout and stderr in `cases/<id>.json`.
6. **Classify.** `deny`, `advice` or `nothing`, from the hook's exit code, stdout and stderr only, by the
   output forms of F-02. It never reads a lobe log. A hook that does not answer in time is `nothing`, which
   is a miss for a bad case.
7. **Sure-fail cases run twice** (step 7): under `enforce: block`, where a bad case is denied, and under
   `enforce: advice`, where it gets a note and a deny is an error. The harness gives these cases a `PATH` it
   controls, as E-04's tester setup does, so the result does not depend on the machine.
8. **Numbers**, per stage and per rule source: the wrong-block rate (good cases denied over good cases), the
   miss rate (bad cases with neither deny nor advice over bad cases) and advice noise (good cases that got
   advice over good cases). `wrong_blocks` and `misses` each give a count and the case ids. The mention row
   gives mentions denied over mentions at stage `block` and mentions that got a note over mentions at stage
   `advice`. A rule's own `must_match` and `must_not_match` lines are replayed in a row of their own and
   never enter the headline row.
9. **The gate** (step 9). Exit 1 when one good case is denied. With 0 of 300 the true rate is below 1.0% at
   95% confidence (the rule of three). Misses and the mention row are reported and not gated. A run in which
   no bad case at stage `block` is denied is an error and also exits 1, so a hook that never denies cannot
   pass. A case whose rule fails its own E-03 tests is an error in the set, not a result.
10. **Mentions are not wrong blocks.** A mention in a call the lesson's `when` block matches is denied at
    stage `block`. That is the rule as designed (E-01 step 2). The row also says how many mentions sat in a
    call the `when` block does not match, and how many were not denied because the call text was cut at the
    4,000-character cap.
11. **The measure from use** (step 10). `enforce.Field` reads a run home's `trials.jsonl` and gives, per rule,
    the fires before a call that then failed and before a call that then succeeded. `lobebench repeat` (022)
    writes it as a `field` block of its summary for every arm where `enforce` is not `off`; 059 does the same
    for M-02's real runs.
12. **Keeping the result** (M-08). The set, the metrics and the gate are committed before the first measured
    run. A run of the labeled set goes to a new folder under `bench/results/`, and its `summary.json` names
    the commit, the hosts, the seed of the sample, and the label counts: agreed, disagreed, checked by a
    person.

## Where to read

| What | Where |
|---|---|
| Requirements | [M-05](../prd/lobe-benchmarks.md), whole entry; [M-02](../prd/lobe-benchmarks.md), "How to build it"; [M-04](../prd/lobe-benchmarks.md) step 1 (the four families); [M-08](../prd/lobe-benchmarks.md) steps 1, 9 and 10; [E-01](../prd/lobe-enforce.md) steps 1 and 2 and the files row; [E-04](../prd/lobe-enforce.md) steps 3 and 4; [core page section 10](../prd/vertical-lobe.md), A-6 and A-27 |
| Sources (nothing ported) | The warning from Trail-code: [`src/claude/pre-bash-check.ts` L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L42). Idea only, BASE: [`src/domain/rule_test.rs` L314](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rule_test.rs#L314) |
| Fact to respect | https://code.claude.com/docs/en/hooks.md : when several PreToolUse hooks disagree, deny wins. Web, not machine-checked |
| Harness code from earlier features | `bench/cmd/lobebench`, `bench/internal/arm`, `bench/internal/stat`, `bench/testdata`, `bench/traps/`, `bench/results/.gitignore` |
| Product code this replays | `internal/lobe/ladder` (`approve.go`, `deny.go`), `internal/lobe/surefail`, `internal/lobe/starters`, `internal/cmd/lobe_sandbox.go` |

## Build tasks

Tasks 2 to 5 are done by different agents, and that separation is part of the feature. Record in the pull
request which agent did which task.

1. Check this brief against the code as it stands and list what no longer fits: the options of `lobebench`,
   what 022's `trap.yaml` holds, the name of 038's approval builder, the note form on Codex. The owner
   approves changes before anything is written.
2. The harness: `bench/internal/enforce` (replay, classify, summarize, the sample function), the action
   `lobebench enforce`, and the fixtures `merge` and `merge-mislabeled` in `bench/testdata`. Proof:
   `TestLobeEnforceCountsAMiss`, `TestLobeEnforceTimeoutIsNothing`, `TestLobeEnforceMentionsReportedApart`,
   `TestLobeEnforceOwnExamplesReportedApart`, `TestLobeEnforceInstalledRuleIsAnApprovedRule`,
   `TestLobeEnforceSureFailFollowsTheSetting`.
3. Side by side with 2, the rule writer: one lesson per trap of 022 under `bench/enforce/rules/`, each with
   an `avoid` block that passes `tentaqles lobe rules test`. This agent never opens `cases.jsonl`.
4. Side by side with 3, the case writer: `bench/enforce/cases.jsonl` at the proposed sizes, for both hosts,
   with the sure-fail cases. This agent never opens a rule's pattern.
5. After 3 and 4, two labeling agents, apart: each fills its column of `labels.jsonl` for every case. Neither
   did task 3. Then the sample is drawn and `bench/enforce/review.md` lists the 50 cases and every
   disagreement for the owner.
6. The owner labels the listed cases (Person check 1). Then `final` and the `label` of each case are written.
   Proof: `TestLobeEnforceLabelsReviewed`, `TestLobeEnforceSetSize`.
7. After 2: `enforce.Field` and the `field` block in `lobebench repeat`. Proof: `TestLobeEnforceField`.
8. After 6: the first measured run of the labeled set, committed under `bench/results/`. Proof:
   `TestLobeEnforceZeroWrongBlocks` on the three systems.
9. Docs: `bench/README.md` gains how to add a case and the rule that a wrong block found later becomes a
   permanent case; `CHANGELOG.md`. No command of `tentaqles` is added, so `docs/reference/commands.md` does
   not change. The plugin does not change.
10. Tester run of the Evaluation below.

## Tests that stay in the repository

- `TestLobeEnforceZeroWrongBlocks`: wrong blocks 0 on the good cases of the labeled set.
- `TestLobeEnforceCountsAMiss`: a bad case the pattern does not match is counted as a miss, not dropped.
- `TestLobeEnforceTimeoutIsNothing`: a hook that never answers is classified as nothing.
- `TestLobeEnforceSetSize`: at least 300 good cases, 150 bad cases and 60 mentions (proposed sizes).
- `TestLobeEnforceOwnExamplesReportedApart`: E-03 lines never enter the headline row.
- `TestLobeEnforceMentionsReportedApart`: a denied mention is in the mention row, is not in `wrong_blocks`
  and does not change the exit code.
- `TestLobeEnforceSureFailFollowsTheSetting`: one sure-fail bad case is `deny` under `enforce: block` and
  `advice` under `enforce: advice`.
- `TestLobeEnforceInstalledRuleIsAnApprovedRule`: the files the harness writes load as the same approved rule
  the function behind `tentaqles lobe rules approve` produces for that lesson. The test calls the function;
  no test runs the person-only verb.
- This file's names, for parts M-05 states without naming a test: `TestLobeEnforceLabelsReviewed` (every case
  has two agent labels; every case of the seeded sample and every disagreement has a person's label; `label`
  equals `final`); `TestLobeEnforceBothHosts` (every rule has cases for `claude` and for `codex`);
  `TestLobeEnforceNoDenyIsAnError` (a run with no denied bad case at `block` exits 1);
  `TestLobeEnforceField` (a fixture `trials.jsonl` gives the expected two counts per rule).
- House rules kept green: `TestLobeBenchShipsOnlyTheFourChecks` (the released program gains no `enforce`
  action and imports nothing under `bench/`), `TestLobeBenchRefusesUsedOutputFolder`,
  `TestLobeBenchStubIsNeverPublishable` (a fixture run is not publishable), `TestLobeClaims_RepositoryIsClean`,
  `TestLobeBenchTranscriptsStayOutOfGit`, the leak suite of 013, and every test under "Must not trade off" in
  `CLAUDE.md` unedited. No test and no harness step touches the real `~/.tentaqles`.

## Evaluation

Run the standard tester setup of [002](002-sandbox.md), which is F-07's block. No
network. This brief also needs the harness program `lobebench`, built from the same commit with
`go build ./bench/cmd/lobebench` and put on `PATH` beside `tentaqles`, and a checkout of the repository at
that commit for checks 8 to 10 (`REPO` is its path). No step runs a person-only command.

Fixture `merge` has one rule: a lesson with the title `Use MERGE with a source alias on this warehouse`, the
`avoid.pattern` of core page 5.4 and no `when` block, at stage `block`, installed for the harness's own
client `acme` by writing `ladder.json` and `approvals.json` directly (the approval stores `"when":{}`). It has
7 cases, all for the host `claude`. 3 are bad: `b1` upper case, `b2` lower case across two lines, `b3`
written `USING(` with no space. 2 are good: `g1` `MERGE INTO t USING src AS s`, `g2` the same in lower case
across two lines. 2 are mentions: `m1` the text of `b1` inside `git commit -m "..."`, `m2` the same text as
the search pattern of a `grep` command.

1. `lobebench enforce --fixture merge --out "$W/m05"`: exit 0.
2. `$W/m05/summary.json`, stage `block`: `good` 2, `wrong_blocks` 0.
3. Same block: `bad` 3, `blocked` 2, and `misses` names only `b3`: the pattern needs white space before the
   parenthesis.
4. Same block: `mentions` 2 and `mentions_denied` 2. The pattern is tested on the whole call text and this
   rule has no `when` block to narrow it, so both quoted mentions are denied. Neither `m1` nor `m2` is named
   under `wrong_blocks`.
5. `$W/m05/cases/b1.json` holds the hook's exit code, stdout and stderr; `classified` is `deny`, and the words
   `source alias`, from the title in the fixture's approval record, appear in stdout or stderr.
6. `$W/m05/cases/g1.json`: `classified` is `advice` or `nothing`, not `deny`.
7. `lobebench enforce --fixture merge-mislabeled --out "$W/m05b"`, where `b1` is labeled `good`: exit 1, and
   `wrong_blocks` is 1 with `b1` named. One wrong block turns the gate red.
8. In `REPO`: `lobebench enforce --out "$W/full"`: exit 0. `$W/full/summary.json` has `wrong_blocks` 0 at
   stage `block`; at least 300 good cases, 150 bad cases and 60 mentions over all stages; cases for the host
   `claude` and for the host `codex`; a sure-fail source with one result under `block` and one under
   `advice`; and no case of the argument check.
9. The same summary's label counts: every case has two agent labels, the number checked by a person is at
   least 50, and the number of disagreements not checked by a person is 0.
10. In `$W/full/cases/`, pick the first case whose host is `codex`: its file holds an exit code of 0 and a
    `classified` value. Then run check 8's command again with the same `--out`: exit 1, a message that names
    `$W/full`, and no file changed.
11. `tentaqles lobe bench enforce --out "$W/x"; echo "exit=$?"` prints a line that is not `exit=0`, and `$W/x`
    does not exist: the released program has no `enforce` action.
12. Before check 1, save `(cd "$TQ_HOME" && find . -type f -exec cksum {} + | sort -k3)` to `$W/before`. Now
    run it again into `$W/after`: `cmp "$W/before" "$W/after"` is silent. The harness worked in homes of its
    own under each `--out` and wrote nothing in the tester's home.

**Person checks**, for the owner.

1. Label the cases listed in `bench/enforce/review.md`: the sample of 50 and every case the two agents
   disagree on. Write `bad`, `good` or `mention` in the `person` field of `labels.jsonl`. Your label stands.
2. Read the summary of the first measured run: the wrong-block count, the miss rate per source, the mention
   row and the label counts. Say whether `enforce: block` may be used on your own clients.

PASS needs 12 of 12 from the tester, after Person check 1 is done.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`, and `go build ./bench/cmd/lobebench`) on the three systems; the plugin suite is untouched.
- [ ] The labeled set has its proposed size, two agent labels per case, and the owner's labels on the sample
  and on every disagreement.
- [ ] The tester brief passes, 12 of 12.
- [ ] The house rules hold: `TestLobeBenchShipsOnlyTheFourChecks`, `TestLobeClaims_RepositoryIsClean`,
  `TestLobeBenchTranscriptsStayOutOfGit`, the leak suite at zero leaks, the guard's tests unedited.
- [ ] The first measured run is committed under `bench/results/`, and `bench/README.md` and `CHANGELOG.md`
  are updated.
- [ ] The owner has read the result and said whether blocking may be on.

## Risks and unknowns

- **M-05's fixture cannot hold as its brief words it.** The brief calls the rule "the lesson of core page
  5.4" and says its `when` block matches the two mentions. With that `when` block (`command: [databricks,
  dbsql]`), `git commit` and `grep` calls are not denied, so check 4 fails. With any `when` block that
  matches `b3`, R-04 prints the lesson as a note, `b3` is `advice`, and check 3 fails. A rule with no `when`
  block makes all seven expected values true, and this file uses it. The owner should confirm; it is in the
  final report.
- **Labels.** A wrong label both agents agree on, outside the sample, is not caught here. Step 11 of M-05 is
  the net: a wrong block found later becomes a permanent case.
- **Zero in 300 leaves room for a rate under 1%.** Real use decides, through E-07 and the permanent cases.
- **The line between a good case and a mention is a judgment.** A case moved to the mention row to turn the
  gate green would hide a wrong block, which is why labels are fixed before patterns are final.
- **Order.** Blocking ships in 038 and its gate arrives here. M-05 leaves open whether a rule may reach
  `block` in a release before the labeled set has its full size. It is a question for the owner.
- **Cost of a fresh home per case.** Over 500 sandboxes per run, on three systems in CI. If the run is too
  slow, the owner decides whether one home per rule is acceptable; M-05 step 6 says one per case.
- **Every size is proposed**: 300, 150, 60 and the sample of 50.
- **This file's own choices**: the files `labels.jsonl`, `review.json` and `review.md`; that the field
  measure is written by `lobebench repeat`; that a run with no denied bad case exits 1. Each is in the final
  report.
- **The field measure cannot see a mistake no rule fired on**, and it trusts Sense: a failure S-04 reads as a
  success shows as a false alarm of a good rule (E-02, risks).
