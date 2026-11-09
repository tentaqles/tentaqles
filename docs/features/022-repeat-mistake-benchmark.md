# 022: Repeat-mistake benchmark

**Status** not started · **Stage** Lessons · **Entries** M-04 (traps, episodes, the trap's own log, the numbers,
the stub episode in CI; the real run is a separate step the owner starts) · **Needs** 019, 021 · **Feeds** 059

## Result

`lobebench repeat` runs episodes. In an episode an agent meets the same trap in four sessions, each with a
fresh context and a different task, and then meets a look-alike task where the avoided pattern is right. The
summary reports, per arm, how often the agent fell into the trap at the second meeting. That repeat rate is
the headline (decision D-9). With the scripted stub agent the answer is fixed: every time without the lobe,
never with it.

```bash
lobebench repeat --agent stub --fixture sql-merge --arms identity,lessons-model-off --trials 1 --out "$W/m04"
cat "$W/m04/summary.json"
cat "$W/m04/lessons-model-off/trial1/trap.log"
```

Arm `identity` shows `hits_by_session` `[1,1,1,1]` and `repeat_rate_2` 1.0. Arm `lessons-model-off` shows
`[1,0,0,0]` and 0.0, and its trap log has one line.

## Scope

**In**

- The action `lobebench repeat` and the package `bench/internal/trap`, which runs episodes.
- The trap format `bench/traps/<family>/<trap>/trap.yaml`, the stub programs in `bench/traps/bin/`, and the
  trap set: four families, 5 traps each (proposed).
- The embedded fixture `sql-merge` and the stub agent that drives the real lobe hooks.
- The arm `lessons-model-off` beside 021's `identity` and `lessons`.
- The numbers of M-04 step 5 and their intervals, which resample traps.
- The CI tests on the stub agent. They are this feature's gate.
- The runbook and the target for the real run, committed before any measured run (M-08 step 9).

**Out**

- The real run itself. It needs a paid agent, the owner starts it, and it gates only the published sentence of
  promise 3, never this feature or phase 2 (M-04, M-02 step 8). See "The real run" below.
- Arm `all`, the graph built before session 1, and the graph's path for the argument-count family (G-04,
  E-05). M-04 step 4 puts them in phase 4. The index does not say which feature adds them to `repeat`; this
  file assumes feature [059](059-token-benchmark.md), which needs this one.
- Denies. Every arm here caps `enforce` at `advice`, so no call is denied and harm is a failed task D. Wrong
  blocks and misses are feature [041](041-enforcement-accuracy.md).
- Codex. Episodes run in Claude Code only until the Codex adapter exists (feature
  [021](021-benchmark-harness.md), risks).
- A longer gap between two meetings, which would test fading (C-05). M-04 leaves it out of the first version.

## What it builds on

- **021**: `lobebench`, `bench/internal/arm` (`Build` and the settings table), `bench/internal/host` (the
  adapter interface, `stub`, the Claude Code adapter), `bench/internal/stat` (the bootstrap), the run folder,
  `summary.json` with `publishable` and `why`, `bench/prices.json`, the refusal of a used `--out`, and the
  receipt rules with `tentaqles lobe bench check-claims`.
- **002**: `tentaqles lobe sandbox` for each episode's home and its client `acme`.
- **003 to 009**: the capture hooks. The failed trapped call and the corrected call that follows become a
  `fail` line and a full `ok` line in the event log.
- **018**: `tentaqles lobe consolidate`, which pairs the failure with the retry that worked.
- **019**: the template lesson filed from that pair, and `ladder.Enter`, which puts it at `advice` in
  `ladder.json` with no confirmation (E-01's enter rule, decision D-9).
- **017**: the note before the matching call in the next session.
- **014**: the starter rules, for the cross-shell family.

## What it leaves for later features

- `lobebench repeat`, the trap set and `bench/internal/trap` (059 adds arm `all` and the graph path).
- The stub agent that sends scripted calls through `tentaqles lobe-hook` (041 replays labeled cases the same
  way).
- The summary keys `hits_by_session`, `repeat_rate_2`, `repeat_rate_3`, `repeat_rate_4`, `first_hit_rate`,
  `harm`, `advice_on_d` and `tokens_by_session` (the published sentence of promise 3 cites `repeat_rate_2`).
- The committed target for the real run, in `bench/traps/README.md`.
- A standing check for features [023](023-fires-and-verdicts.md), [024](024-automatic-demotion.md) and
  [030](030-reinforcement-and-fading.md): the stub episode stays at `[1,0,0,0]` after each of them, which shows
  that recording fires, demotion and fading do not silence a lesson that keeps being right.

## How it works

1. **A trap** is a rule of the environment that an agent breaks by default. It lives in
   `bench/traps/<family>/<trap>/` with a `trap.yaml` that holds the rule, the fixed error text, five tasks and
   the path of the stub's log. Tasks A, B, C and E lead into the trap. Task D is the look-alike, where the
   avoided pattern is the right thing to do. Each task has a prompt and a pass check. For the stub agent each
   task also has a script: the trapped call, the corrected call and, for D, the look-alike call.
2. **Four families** (M-04 step 1), 5 traps each (proposed).
   - Cross-shell syntax: PowerShell text sent to Git Bash and the reverse. Traps a starter rule covers and traps
     none covers are reported as two groups, because a starter can speak at the first meeting and a learned
     lesson only from the second.
   - SQL dialect rule: a stub warehouse CLI rejects one pattern with a fixed error, for example the
     `MERGE ... USING (` form of the sample lesson in core page 5.4.
   - Wrong argument count: a fixture repository whose function takes an unusual number of arguments.
   - Misused API: a stub command-line API and a stub MCP tool that reject one argument form.
3. **The stubs** are small Go programs in `bench/traps/bin/`, built with the harness. They use no network and
   hold no real client data. Each rule is invented on purpose, so that a model cannot know it in advance and
   session 1 does fall into it. A stub appends one line to the trap's log for every call it rejects.
4. **An episode** is one trap in one arm in one trial. The harness builds a fresh home with
   `tentaqles lobe sandbox`, writes the arm's settings into the manifest of `acme` as a fixture, and puts the
   trap's files in a repository inside the client's folder. It then runs sessions 1 to 4 on tasks A, B, C and
   E, each with a fresh context and a new session id, and runs `tentaqles lobe consolidate` in the client's
   folder after each. That verb runs now what the `stop` hook starts, so before the next session begins the
   lesson is filed and stands at its stage. Session 5 runs task D. Nobody confirms anything and the harness
   writes no ladder entry: there is one mode, the one a new user gets.
5. **Folders.** For a fixture, which holds one trap, an episode lives in `<out>/<arm>/trial<N>/`: `home/` is
   its `TQ_HOME`, `trap.log` is the stub's log, and the transcripts sit beside them. A run over several traps
   puts each trap's episodes under `<out>/<trap>/<arm>/trial<N>/` (this file's choice).
6. **Hits come from the trap's log**, never from the lobe's own records. The harness counts the lines the log
   gained during each session. Deleting the lobe's event log changes no reported number.
7. **Arms** (M-04 step 4): `identity` (no lobe), `lessons`, and `lessons-model-off`, which is `lessons` with
   `model: off`. Until feature [025](025-model-drafting.md) the two lesson arms file the same template lesson.
8. **The stub agent** follows each task's script and talks to the lobe the way a host does. For a call it pipes
   a `PreToolUse` object into `tentaqles lobe-hook claude pre-tool-use`, with `CLAUDE_CONFIG_DIR` set to the
   login folder of `acme` and `cwd` inside the client's folder. It runs the command for real, so the stub
   program accepts or rejects it. Then it pipes a `PostToolUse` or a `PostToolUseFailure` object into the
   matching hook. The payloads have the shape of the fixtures of features 003 and 008. Its one rule: it runs
   the trapped call and then the corrected one, unless the `pre-tool-use` hook printed context for the trapped
   call; then it runs only the corrected one. It sends no `stop` event, because the harness runs
   `tentaqles lobe consolidate` itself. With `--agent stub` the harness runs that verb with `--draft template`
   in every arm, so no test ever starts an agent CLI.
9. **Numbers**, per family and arm (M-04 step 5).
   - Repeat rate at the second meeting, `repeat_rate_2`: episodes with a trap hit in session 2 divided by all
     episodes. This is the headline. An episode in which session 1 never hit the trap stays in it, so the
     denominator is always traps times trials.
   - The same rate at the third and fourth meeting. They show whether a lesson was demoted (feature 024) or
     faded (feature 030).
   - First-hit rate; harm rate (task D failed, or its correct call was denied); advice shown on task D; each
     session's billed tokens.
   - A session that ends without finishing its task counts as a failed task, and its trap hits still count.
10. **Intervals** resample traps, not trials, because trials of one trap are not independent (M-04 step 6).
    The 95% interval of the difference in repeat rate between two arms uses 021's paired bootstrap with traps
    as the unit.
11. **The summary** has, per arm, `hits_by_session` (four counts summed over the arm's episodes),
    `repeat_rate_2`, `repeat_rate_3`, `repeat_rate_4`, `first_hit_rate`, `harm`, `advice_on_d` and
    `tokens_by_session`, then the same per family, then per pair of arms the differences with their intervals.
    `hits_by_session`, `repeat_rate_2` and `harm` are the requirements' names; the others are named here. A
    stub or fixture run is marked `"publishable":false` as in feature 021.

### The real run

It is not part of this feature's gate. The builder prepares it; the owner starts it.

- Proposed size (M-04 step 4): 20 traps, 3 trials. With arm `all` that is 4 arms and 240 episodes; before
  phase 4 it is 3 arms and 180 episodes of 5 sessions each.
- Target (proposed): the 95% interval of the repeat-rate difference between `lessons` and `identity` lies
  below 0 at the second meeting, and the harm rate of `lessons` is not above that of `identity`.
- The traps, the metrics and this target are committed before the first measured run, and the manifest names
  that commit. A change after seeing results starts a new run (M-08 step 9).
- The run is on Claude Code first and its numbers name the host and the model (decision A-26).
- A run that misses the target means the claim is not made. Its run folder is committed like any other, and
  the miss is recorded (M-02 step 8).
- Consolidation with `model: on` calls the agent CLI, so the run needs network, and its drafting tokens are
  added to arm `lessons` (M-02 step 5).
- It should not start before features 023 to 025 have passed: without 024 a wrong note is never taken back, so
  the rates at the third and fourth meeting say nothing about demotion, and without 025 arm `lessons` equals
  arm `lessons-model-off`. The builder writes a cost estimate from the phase 0 result of feature 021 first.

## Where to read

| What | Where |
|---|---|
| Requirements | [M-04](../prd/lobe-benchmarks.md) in full; row 3 of "What we claim" on the same page; [M-02](../prd/lobe-benchmarks.md) steps 3, 5, 6 and 8; [M-08](../prd/lobe-benchmarks.md) steps 5, 9 and 10 |
| Requirements, shared | [core page](../prd/vertical-lobe.md) 5.5 (a new lesson speaks at once) and decision D-9; [E-01](../prd/lobe-enforce.md), the enter rule; [C-01](../prd/lobe-consolidate.md) and [C-03](../prd/lobe-consolidate.md) (`--draft template`, who may use the model); [F-07](../prd/lobe-foundation.md), the row for `tentaqles lobe consolidate` |
| Source, contrast | Trail-code [`src/skills/offer-repair.ts` L143](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/offer-repair.ts#L143): a repeated repair only decides when to make an offer to the person; no repeat rate is measured |
| Source, contrast | agentmemory [`eval/README.md` L109](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L109): its own list of what is still unmeasured includes coding tasks built from captures |
| Trap material (web, not machine-checked) | https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_pipeline_chain_operators : `&&` exists only from PowerShell 7 |
| Existing code | `bench/` as feature 021 left it; the hook fixtures of features 003 and 008; the starter catalog of feature 014 |

## Build tasks

1. **Check this brief against the code.** Read `bench/` as feature 021 left it and the lesson path of features
   017 to 019. Confirm by hand, in a sandbox, that one failed call and its corrected retry give a lesson at
   `advice` after `tentaqles lobe consolidate --draft template`, and that the note appears in a new session.
   List what no longer fits.
2. **The trap format and the `sql-merge` trap.** `trap.yaml`, the stub warehouse CLI, its log, the five tasks
   with their scripts and pass checks, and the fixture in `bench/testdata`. Proof: `TestLobeRepeatEveryTrapRejects`
   for this trap.
3. **`bench/internal/trap` and `lobebench repeat`.** Episodes, folders, the hit count from the trap's log, the
   summary. Proof: `TestLobeRepeatReadsTheTrapLog`, `TestLobeRepeatHeadlineKeepsEveryEpisode`,
   `TestLobeRepeatCountsHarm`.
4. **The stub agent over real hooks**, in `bench/internal/host`, and the arm `lessons-model-off`. Proof:
   `TestLobeRepeatStubEpisode`.
5. **The other traps.** The rest of the SQL family, then cross-shell, argument count and misused API, with
   their stubs. These can be written side by side, one agent per family. Proof: `TestLobeRepeatEveryTrapRejects`
   over all traps.
6. **The real-run runbook.** `bench/traps/README.md`: the command, the size, the target, the cost estimate, and
   what is committed before and after. No run is started.
7. **Docs.** A line in `CHANGELOG.md`; the `repeat` action in `bench/README.md`. No `tentaqles` command is added,
   so `docs/reference/commands.md` does not change.
8. **Tester run.** A fresh agent runs the Evaluation below.

## Tests that stay in the repository

- `TestLobeRepeatStubEpisode`: with the scripted agent and nothing confirmed, arm `identity` has repeat rate
  1.0 and arm `lessons` 0.0. The test also asserts that the harness itself wrote nothing to `ladder.json`:
  the entry at `advice` comes from the enter rule.
- `TestLobeRepeatReadsTheTrapLog`: deleting the lobe's event log changes no reported number.
- `TestLobeRepeatCountsHarm`: a deny on task D's correct call is counted in the harm rate. The deny is a
  scripted hook answer here, since nothing denies before feature 038.
- `TestLobeRepeatHeadlineKeepsEveryEpisode`: the headline denominator equals traps times trials, with one
  episode whose session 1 never hit the trap.
- `TestLobeRepeatEveryTrapRejects` (named here): for each committed trap, the stub rejects the trapped call
  with the trap's fixed error and adds 1 line to its log, and accepts the corrected call and the look-alike
  call with 0 lines added.
- `TestLobeRepeatStubStartsNoAgentCLI` (named here): with a fake `claude` first on `PATH` that records every
  start, a stub episode in arm `lessons` starts it 0 times.
- House rules kept green: the tests of feature 021 (a stub run is never publishable, a used `--out` is
  refused, the key is in no result file), the leak suite of feature 013 at zero leaks, and every test under
  "Must not trade off" in `CLAUDE.md`, unedited. Episodes run only in homes built by `tentaqles lobe sandbox`
  under `--out`; no test touches the real `~/.tentaqles`.

## Evaluation

The tester gets the built `tentaqles` and the built `lobebench` from the same commit, both first on `PATH`,
and an empty working folder. No network. Run the standard tester setup (feature [002](002-sandbox.md)). The
harness works with its own client `acme` in a home it builds under `--out`. Nothing is confirmed, no step runs
a person-only command, and arm `lessons-model-off` calls no agent CLI.

The scripted agent of fixture `sql-merge` runs the trapped command and then the corrected one, unless the
lobe's `pre-tool-use` hook printed context for that call; then it runs only the corrected one.

1. `lobebench repeat --agent stub --fixture sql-merge --arms identity,lessons-model-off --trials 1
   --out "$W/m04"`, typed as one line. Expect exit 0.
2. `$W/m04/summary.json`, arm `identity`: `hits_by_session` is `[1,1,1,1]` and `repeat_rate_2` is 1.0.
3. Arm `lessons-model-off`: `hits_by_session` is `[1,0,0,0]`, `repeat_rate_2` is 0.0 and `harm` is 0.
4. The stub's log `$W/m04/lessons-model-off/trial1/trap.log` has exactly 1 line; the one for `identity` has 4.
5. Under `$W/m04/lessons-model-off/trial1/home/` there is exactly one `.md` file directly in
   `identities/acme/lobe/lessons/`, and `identities/acme/lobe/ladder.json` holds `"stage":"advice"` for its id.
6. `$W/m04/summary.json` has `"publishable":false`, and its `why` holds `stub-agent` and `trials-under-3`.
7. Run the command of check 1 again. Expect exit 1 and no file changed under `$W/m04`.

**Person checks** (the owner)

None for this gate. The real run is described above; when the owner starts it, the run folder goes under
`bench/results/` and the sentence that cites it must pass `tentaqles lobe bench check-claims --root .`.

Scoring: PASS needs 7 of 7. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`).
- [ ] The tester brief passes, 7 of 7.
- [ ] The house rules hold: `TestLobeRepeatStubStartsNoAgentCLI`, the leak suite at zero leaks, the "Must not
  trade off" tests unedited and green.
- [ ] All committed traps pass `TestLobeRepeatEveryTrapRejects`.
- [ ] `bench/traps/README.md` holds the runbook and the target; `CHANGELOG.md` and `bench/README.md` are
  updated.
- [ ] The owner has seen the two rows of Result. No number from this feature is published.

## Risks and unknowns

- **Order.** M-04 lists E-02 and E-07 among its needs, and the index builds this feature before 023 and 024.
  The stub episode needs only features 017 to 019, so the gate holds. The real run is where the order matters,
  which is why it waits (see "The real run").
- **A lesson with no `avoid` block is never taken back by a hook** (E-02, E-07). The template lesson of
  feature 019 has none. If its note is wrong on task D, it stays until a person runs
  `tentaqles lobe rules demote` or `retire`; that shows in `advice_on_d` and in the harm rate.
- **Not every family can be learned from a repair pair yet.** A wrong argument count shows up as a failing
  check that later passes, which is the sign of feature [028](028-edit-and-check-signs.md), and the graph's
  path arrives in phase 4. Results are reported per family, and the target is on the whole set.
- **The stub MCP tool** needs the real agent's session to register a stub server. How the Claude Code adapter
  passes one is not in the requirements; it is checked before the real run, not for this gate.
- **Cross-shell traps depend on the system.** PowerShell text sent to Git Bash can be trapped wherever a bash
  tool runs; the reverse needs a PowerShell tool. The runbook says on which systems each trap is run.
- **Invented rules** show that the mechanism works, not how often real mistakes happen (M-04, risks). The
  pull-request tasks of feature 059 cover real work.
- **Names chosen here**: the summary keys other than `hits_by_session`, `repeat_rate_2` and `harm`, the folder
  layout for more than one trap, and the pass check in `trap.yaml`.
- What would force a change to this file: a template lesson from feature 019 whose `when` block does not match
  the trapped call of the next task (then check 3 cannot pass and the fixture or the template must change), or
  an owner decision to run episodes in Codex too.
