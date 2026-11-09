# 059: Token and correctness benchmark

**Status** not started · **Stage** Graph · **Entries** M-02 (the real runs: steps 1 to 8 for the question and
pull-request sets, arms `graph` and `all`); M-08; M-01 (step 7, the estimate beside the measurement); D-15, A-26,
A-28 · **Needs** 058, 022 · **Feeds** none

## Result

The same tasks are run by a real agent with the graph off and with the graph on, and the cost of the two is
compared. The owner starts each real run; the builder prepares everything up to that point and nothing after it
needs a guess. When the runs are in, the repository holds two run folders and a report:

```bash
lobebench report --run bench/results/<run-id>   # report.md, with a receipt marker on every figure
tentaqles lobe bench check-claims --root .      # exit 0: no figure without a receipt
```

M-02 names the action `report` and not its arguments; `--run <dir>` is the form 021 chose.

The report gives the figure of promise 2: the ratio of total billed cost of arm `graph` to arm `identity` with
its 95% interval, beside the difference in correctness, with the graph's build time in a column of its own. It
names the host (Claude Code first, A-26), the model id and the date. A run that misses its target is committed
like any other and the claim is not made. The SWE-bench Verified run does not start here: the owner gets one
request for approval with a checked cost, and answers yes or no (D-15).

## Scope

**In**

- The two cheaper task sets under `bench/tasks/`: 10 questions and 5 merged pull requests for each of 3 public
  repositories pinned by commit (proposed), PocketBase among them, plus tentaqles' own repository, reported
  apart (M-02 step 1, M-08 rule 8).
- The question grader (an answer key written before any run) and the pull-request grader (the repository reset
  to the base commit; the pull request's tests pass on the agent's patch and no earlier test breaks).
- Arms `graph` and `all` with a real agent in `bench/internal/arm`: the graph built in the fresh clone before
  the agent starts, its time recorded as `graph_build_ms` and kept out of every total (A-28, M-02 steps 3 and
  5).
- Arms `lessons` and `all` for these task sets: the frozen copy of a store warmed by fixed warm-up tasks that
  are not measured, the warm-up tasks committed first, and a cold-store result beside it (M-02 step 3 and
  risks). This is M-02's comparison of `lessons` against `identity`, which 021 leaves to this feature.
- The `invalid` outcome for a run in the wrong state, with its count printed and the run repeated.
- In the report: the cost ratio of `graph` against `identity`, the paired statistics of M-02 step 6, the build
  time apart, and the estimate of `tentaqles lobe status --savings` beside the measured difference.
- The gate, the tasks and the metrics committed before the first measured run (M-08 rule 9).
- The run commands for the owner, and the two real runs themselves, which the owner starts.
- For SWE-bench Verified: the draw of 150 of its 500 instances (proposed) with a recorded seed, its grader (the
  official harness), the second interval that resamples whole repositories (M-02 step 6, for a set that spans
  10 or more), the cost note, and the approval gate. Not the run.

**Out**

- The harness itself, the stub agent, the statistics, the results layout and `check-claims`
  ([021](021-benchmark-harness.md)).
- The repeat-mistake benchmark and its traps ([022](022-repeat-mistake-benchmark.md)), and enforcement accuracy
  ([041](041-enforcement-accuracy.md)).
- The SWE-bench Verified run. It waits for the owner's one approval; if given, it is started by the owner with
  what this feature prepared. It holds back a published number, never this feature (M-02 step 8).
- A Codex adapter and any Codex number. M-02 step 8 lets it follow "when the usage fields Codex prints in
  headless mode are confirmed". No feature in the index takes it.
- Publishing a sentence in the README. The owner decides that after reading the report; `check-claims` guards it.

## What it builds on

- [021](021-benchmark-harness.md): `bench/cmd/lobebench` with `run` and `report --run <dir>`; the task file
  format (one YAML file per task under `bench/tasks/`) and one committed question; `bench/internal/arm`
  (`Build(arm, dir)` and the settings table, with `graph` and `all` as settings and scripted fixture arms
  only), `host` (the Claude Code adapter and `stub`), `grade` (the question grader) and `stat`
  (`PairedBootstrap`, `McNemarExact`, `RatioOfTotals`); `bench/results/<run-id>/` with `manifest.json`,
  `runs.jsonl`, `summary.json`, `report.md` and `transcripts/`; `bench/prices.json`; the `publishable` flag;
  `tentaqles lobe bench check-claims`; `bench/phase0/q5-result.md`, which says which usage fields Claude Code
  prints and how a headless session is started with the plugin's hooks. Its tests:
  `TestLobeBenchCostIsBilledCost`, `TestLobeBenchCountsFailedRuns`, `TestLobeBenchMcNemarExact`,
  `TestLobeBenchPairedBootstrap`, `TestLobeBenchRefusesUsedOutputFolder`,
  `TestLobeBenchStubIsNeverPublishable`, `TestLobeBenchTranscriptsStayOutOfGit`.
- [022](022-repeat-mistake-benchmark.md): arm `lessons` driven through the real hooks by the stub, and
  `lobebench repeat`. Its traps and stores are its own; the warm store for these task sets is built here.
- [043](043-graph-of-one-repository.md) and [048](048-graph-stays-current.md): `tentaqles lobe graph build` and
  `tentaqles lobe graph status --json`, by which the harness builds and then confirms a finished graph.
- [058](058-token-savings.md): `tentaqles lobe status --savings --json`, read from each run's home for "the
  lobe's injected bytes" and for the estimate in the report.
- [057](057-graph-accuracy.md): the pins of the three public repositories, if that feature made them.
- [002](002-sandbox.md): `tentaqles lobe sandbox`, by which every arm's home is built.

## What it leaves for later features

- The task sets and graders under `bench/`, which any later rerun uses (a new model, a new release, a Codex
  adapter).
- The run folders under `bench/results/`, which are the receipts for any sentence about tokens or cost.
- The SWE-bench instance list, the harness wiring and the approval file, ready for the day the owner says yes.
- No later feature in the index depends on this one.

## How it works

1. **Task sets** (M-02 step 1). A question passes when the answer names every file and symbol in its answer
   key. A pull-request task is a merged pull request that added or changed tests: the clone is reset to the base
   commit, and the run passes when those tests pass on the agent's patch and no earlier test breaks. Each task
   file names its repository, its commit and its kind. Results of tentaqles' own repository go in a table of
   their own, and no claim rests on it alone.
2. **Arms.** The settings of each arm are the table of M-02 step 2, written into the sandbox manifest as a
   fixture; no person-only command runs. All five arms are prepared. `identity` and `graph` give the figure of
   promise 2, and the index asks for that pair. `lessons` against `identity` is M-02's part of promise 3. All
   arms of one table must come from one run folder (M-08 rule 4), so the owner chooses the arms of a run before
   it starts: two arms are 270 runs over both sets at 3 trials, four arms are 540. This file proposes
   `identity` and `graph` for the first runs.
3. **The warm store.** Arms `lessons` and `all` start from a frozen copy of a store warmed by fixed warm-up
   tasks on the same repositories. The warm-up tasks are committed before any run and are not measured. The
   copy is made once and every trial starts from the same bytes. A cold-store result is published beside the
   warm one, so the warm store cannot flatter the arm unseen (M-02 risks).
4. **Same conditions** (M-02 step 3). Every arm gets the same host version, model id, prompt, built-in tools,
   permission mode, turn limit and time limit (proposed: 10 minutes for a question, 30 for a pull request), a
   fresh clone and a fresh `TQ_HOME`, `HOME` and agent config folder. In every arm but `off` that config folder
   is the client's own login folder and the agent starts inside the client's folder, or no lobe hook would act.
   The provider key comes from one environment variable of the person who runs the benchmark and is never
   written to a result file.
5. **The graph before the run** (A-28). For `graph` and `all` the harness runs `tentaqles lobe graph build` in
   the fresh clone, times it, and confirms with `tentaqles lobe graph status --json` that nothing is building,
   changed or dirty before the agent's first turn. The time goes to `graph_build_ms` on the run's line and to a
   column of its own in the summary. It enters no total of cost, tokens or wall time.
6. **Invalid runs.** A run in the wrong state gets the outcome `invalid`, stays out of the totals and is rerun:
   lobe text in arm `off`, no lobe hook fired in arm `all`, no finished graph in an arm that uses it. The count
   of such runs is printed.
7. **Trials and order.** At least 3 trials. All runs are shuffled with a recorded seed.
8. **One line per run** in `runs.jsonl` (M-02 step 5), with the four billed token counts, dollars from the dated
   `bench/prices.json`, tool calls, wall time, the lobe's injected bytes and `graph_build_ms`. A failed or
   timed-out run keeps its tokens, cost and time in every total.
9. **Statistics** (M-02 step 6), trials averaged per task. Correctness: the difference in mean pass rate with a
   95% interval from a paired bootstrap, plus McNemar's exact test. Cost, tokens, calls and time: the ratio of
   totals with the same interval. With 3 repositories each repository is also listed apart.
10. **The estimate beside the measurement** (M-01 step 7). For the same runs the report prints the sum of
   `saved_low_tokens_est` and `saved_high_tokens_est` from each run's home next to the measured difference in
   billed tokens, labeled as an estimate and never as a result.
11. **Reporting rules** (M-08). `lobebench report` writes `report.md` with a marker
    `<!-- bench:<run-id>/summary.json#<key> -->` on every figure. Each figure is a mean or a ratio of totals
    with its interval and count; the baseline arm, the host, the model id and the date are in the sentence; the
    sentence says the graph was built before the run and gives the build's time. Copying from the report is the
    normal way to publish.
12. **Decided before the run** (M-08 rule 9). One commit holds the tasks, the answer keys, the metrics and the
    gate, and the manifest of each run names it. M-02 gives no number for the gate of promise 2. This file
    proposes: the claim is made only if the 95% interval of the cost ratio `graph` over `identity` lies below
    1 and the interval of the correctness difference does not lie wholly below 0. The owner confirms or
    changes it before the commit. A change after seeing results starts a new run.
13. **SWE-bench Verified, prepared only** (D-15). The instance list is drawn once with a recorded seed and
    committed. The official harness grades on Linux x86_64 with 120 GB of free disk (web, not machine-checked).
    The cost note gives: runs as instances times arms times trials (150 x 2 x 3 = 900 for promise 2 alone); the
    measured mean cost of a pull-request run from this feature's own run folder, with its interval, as the
    checked figure; the dated prices; and beside them the requirements' unchecked estimate (about 1,700 dollars
    for 2 arms, about 870 for each further arm). `lobebench run` on that set with a real agent refuses unless
    the approval file exists (proposed here: `bench/tasks/swebench/approval.json`, written by the owner, with
    the date and the ceiling in dollars). With the stub agent it runs on a two-instance fixture.

**Who does what**

| Step | The builder prepares, with no paid agent | The owner starts or decides |
|---|---|---|
| Tasks | Task files, answer keys, base commits, the pins, the warm-up tasks, the pull-request grader, each proved on the stub agent | Reads a sample of the answer keys; chooses the arms |
| Gate | The proposed gate and the pre-run commit | Confirms the gate before that commit |
| Dry run | The whole of each set through `--agent stub`, which is never publishable | Nothing |
| Question run | The exact command line, the seed, the machine notes | Sets the provider key and runs it |
| Pull-request run | The same, plus the toolchains each repository's tests need | Sets the provider key and runs it |
| Report | `lobebench report`, `check-claims`, a drafted sentence or a recorded miss | Decides whether a sentence is published |
| SWE-bench Verified | The draw, the wiring, the cost note, the approval gate | One yes or no; on yes, runs it |

## Where to read

| What | Where |
|---|---|
| Requirements | [M-02](../prd/lobe-benchmarks.md), all of it; [M-08](../prd/lobe-benchmarks.md), all of it; [M-01](../prd/lobe-benchmarks.md) step 7; the claims table and "What Graft did well" at the top of that page; [core page](../prd/vertical-lobe.md) section 8 (what a real run gates; who starts a real run) and decisions D-7, D-15, A-26, A-28 |
| Idea source | Graft 0.17.0 `README.md` L128, L130, L522 and L570: arms of one agent, cache-aware cost, 3 trials; 10 questions and 5 merged pull requests per repository; transcripts audited |
| Idea source | [`eval/README.md` L79](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L79), L80 and [L84](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L84): a manifest with fingerprints, one record per case, existing output folders refused; [`eval/README.md` L107](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L107): what a public claim needs |
| Used as it is | <https://www.swebench.com/SWE-bench/reference/harness/> and <https://huggingface.co/datasets/princeton-nlp/SWE-bench_Verified>: 500 instances; x86_64 host with 120 GB free disk. Web, not machine-checked |
| Existing code | `bench/` as 021 and 022 left it; `internal/lobe/benchcheck/claims`; `test/integration/lobeclaims_test.go`; `.goreleaser.yaml` (the release builds `./cmd/tentaqles` only) |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: which arms, graders and adapter
   021 and 022 already built; how a task set is chosen on the command line (M-02 names no flag for it); where
   the three public repositories are pinned. Put to the owner: the arms of the first runs, the gate of step 12
   and the approval file of step 13. Files: none. Check: a written list the owner approves.
2. The three pins and the question set: 30 task files and answer keys under `bench/tasks/`, written before any
   run and by an agent that will not run the benchmark. Test: `TestLobeBenchTaskSetsArePinned`. Can run beside
   tasks 3 and 4.
3. The pull-request set: 15 task files, each with its base commit and the tests the merged change added or
   changed, and the grader in `bench/internal/grade`. Test: `TestLobeBenchPullRequestGrader`.
4. Arms `graph` and `all` in `bench/internal/arm`, the build before the run, `graph_build_ms`, and the `invalid`
   rules. Tests: `TestLobeBenchGraphBuiltBeforeTheRun`, `TestLobeBenchArmsUseTheSettingsKeys`,
   `TestLobeBenchInvalidRunsAreRerun`.
5. The warm store for arms `lessons` and `all`: the warm-up tasks, the frozen copy, the cold-store variant. The
   store is warmed with the stub agent for the tests; the real warm-up is part of the owner's run when those
   arms are chosen. Test: `TestLobeBenchWarmStoreIsFrozen`. Can run beside task 4.
6. The report: the promise 2 key in `summary.json`, the build-time column, the estimate of 058 beside the
   measurement. Test: `TestLobeBenchReportShowsEstimateApart`. Needs task 4.
7. A dry run of both sets with `--agent stub`, to prove the plumbing end to end. Check: two run folders with
   `"publishable":false` and a `why` that holds `stub-agent`; they are not committed.
8. The pre-run commit: tasks, keys, warm-up tasks, metrics and the gate the owner confirmed. Check: the manifest
   of the dry run names that commit.
9. The run sheet for the owner: the two command lines, the environment variable to set, the machine and disk
   needs, the expected number of runs and a cost ceiling the owner may stop at. Check: the owner reads it.
10. SWE-bench Verified, prepared: the draw, the grader wiring, the interval that resamples whole repositories,
    the two-instance stub fixture, the approval gate. Tests: `TestLobeBenchSWEBenchWaitsForApproval`,
    `TestLobeBenchRepositoryBootstrap`. Can run beside tasks 2 to 6.
11. After the owner's two runs: `lobebench report --run <dir>`, the transcript check, `check-claims`, the commit
    of the run folders, the cost note, and the request for the owner's one approval.
12. Docs and changelog: one line in `CHANGELOG.md` under `[Unreleased]`. A sentence with a figure enters the
    README only by the owner's decision and only with its marker. No `tentaqles` command is added.
13. The tester run of "Evaluation", by an agent that built none of this.

## Tests that stay in the repository

- `TestLobeBenchGraphBuiltBeforeTheRun`: in arms `graph` and `all` every run line has a `graph_build_ms`, the
  graph exists before the agent's first turn, and adding 1,000 ms to a build changes no total of cost, tokens or
  wall time. Arms `off`, `identity` and `lessons` have no such field.
- `TestLobeBenchArmsUseTheSettingsKeys`: each arm's manifest holds exactly the values of M-02 step 2's table,
  and `tentaqles lobe settings` reports none of them as invalid. Widened here to `graph` and `all`.
- `TestLobeBenchTaskSetsArePinned` (new here): every task names a repository and a 40-character commit; every
  question has an answer key; every pull-request task names its base commit and at least one test; our own
  repository's tasks are marked to be reported apart.
- `TestLobeBenchPullRequestGrader` (new here): on a fixture repository, the merged patch passes, an empty patch
  fails with `empty-patch`, and a patch that breaks an earlier test fails with `wrong`. 3 of 3.
- `TestLobeBenchInvalidRunsAreRerun` (new here): a run with lobe text in `off`, one with no lobe hook fired in
  `all` and one with no finished graph in `graph` each get `invalid`, stay out of the totals, are run again,
  and are counted in the summary. 3 of 3.
- `TestLobeBenchReportShowsEstimateApart` (new here): the report holds the estimate with the word `estimate`,
  with no receipt marker on it, and the measured figure with one.
- `TestLobeBenchSWEBenchWaitsForApproval` (new here): with a real agent and no approval file the run exits
  non-zero, names decision D-15 and creates no folder; with the stub agent the two-instance fixture runs.
- `TestLobeBenchWarmStoreIsFrozen` (new here): two trials of arm `lessons` start from byte-identical stores; a
  warm-up task appears in no line of `runs.jsonl`; the cold-store variant starts with 0 lessons.
- `TestLobeBenchRepositoryBootstrap` (new here): on a fixture of 10 repositories the summary holds a second
  interval that resamples repositories; on 3 repositories it holds none and lists each repository apart.
- Kept green from 021: `TestLobeBenchCostIsBilledCost`, `TestLobeBenchCountsFailedRuns`,
  `TestLobeBenchMcNemarExact`, `TestLobeBenchPairedBootstrap`, `TestLobeBenchRefusesUsedOutputFolder`,
  `TestLobeBenchHarnessIsInTheRepo`, `TestLobeBenchStubIsNeverPublishable`,
  `TestLobeBenchShipsOnlyTheFourChecks`, `TestLobeBenchTranscriptsStayOutOfGit`,
  `TestLobeClaims_RepositoryIsClean`, `TestLobeClaims_NotPublishableFails`.
- House rules: no client content may enter `bench/results/` (the harness refuses an `--out` inside a client's
  folder, the checkout's own `bench/results/` excepted); the provider key is in no result file
  (`TestLobeBenchKeyIsNeverWritten`, new here if 021 has no such test); the guard's tests untouched.

## Evaluation

Tester brief. Start from the standard tester setup of [002](002-sandbox.md) (F-07). No network, no provider
key, no real agent: every check uses the stub agent or expects a refusal. This brief needs the harness program
`lobebench`, built from the same commit with `go build ./bench/cmd/lobebench` and put on `PATH` beside
`tentaqles`. `graph-pair` is the fixture this feature adds: 2 tasks, arms `identity` and `graph`, scripted
usage.

1. `lobebench run --agent stub --fixture graph-pair --arms identity,graph --trials 3 --out "$W/r1"`. Expect exit
   0 and 12 lines in `$W/r1/runs.jsonl`.
2. Every line with `"arm":"graph"` holds a `graph_build_ms`; no line with `"arm":"identity"` does.
3. `$W/r1/summary.json` holds a cost ratio of `graph` to `identity` with a lower and an upper bound, a
   build-time total apart from `usd_total`, `"publishable":false`, and a `why` that holds `stub-agent`.
4. The sum of `usd` over the lines of one arm in `runs.jsonl` equals that arm's `usd_total` in the summary,
   failed runs included.
5. `lobebench report --run "$W/r1"` exits 0. `$W/r1/report.md` holds at least one marker that starts
   `<!-- bench:r1/summary.json#`, names the host, the model id and the date, and holds the word `estimate` on
   the line that shows the estimate of `status --savings`.
6. Copy `$W/r1` to `$W/c/bench/results/r1` and `$W/r1/report.md` to `$W/c/README.md`. Run
   `tentaqles lobe bench check-claims --root "$W/c"`. Expect exit 1 and `not-publishable`: a stub run can never
   back a claim.
7. Run check 1's command again. Expect exit 1, a message naming `$W/r1`, and no file changed.
8. With no provider key in the environment, run the real question set as the run sheet writes it, with
   `--out "$W/r9"`. Expect a non-zero exit, a message naming the missing variable, and no folder `$W/r9`.
9. With no approval file, start the SWE-bench Verified set with the real agent and `--out "$W/r10"`. Expect a
   non-zero exit, a message naming decision D-15, and no folder `$W/r10`.
10. `tentaqles lobe bench run --out "$W/r3"; echo "exit=$?"` prints a line that is not `exit=0`, and `$W/r3`
    does not exist: the released program has no `run` action.

**Person checks** (the owner; a builder or tester agent never starts a real run)

1. Before any run: read a sample of the answer keys and confirm the gate. The pre-run commit follows.
2. Set the provider key in the one environment variable the run sheet names, and start the question run.
3. The same for the pull-request run.
4. Read `report.md` of both. Run `tentaqles lobe bench check-claims --root .` in the checkout: exit 0. Decide
   whether a sentence is published; a miss is recorded and the run folder stays committed.
5. Read the cost note. Answer the one request for the SWE-bench Verified run with yes or no. On yes, write the
   approval file and start that run on a Linux x86_64 machine with 120 GB of free disk.

Scoring: PASS needs 10 of 10 tester checks. "Could not run" is a FAIL. Person checks 1 to 4 are part of the
gate below. Person check 5 is an answer, and either answer closes it.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`).
- [ ] The tester brief passes, 10 of 10.
- [ ] The pre-run commit exists and both real run folders (questions and pull requests, with at least arms
  `identity` and `graph`) are committed under `bench/results/`, pass or miss, with their manifests naming that
  commit and the SHA-256 of every transcript.
- [ ] `check-claims` passes on the repository; any published sentence carries its marker, host, model and date.
- [ ] The cost note was delivered and the owner's answer on SWE-bench Verified is recorded.
- [ ] The house rules hold: no client content under `bench/results/`, the key in no file, the guard's tests
  untouched.
- [ ] `CHANGELOG.md` updated.
- [ ] The owner has seen the result described under Result.

## Risks and unknowns

- Proposed numbers: 3 repositories, 150 instances, the two time limits, 9,999 bootstrap draws.
- Three things here are this file's proposals and need the owner's yes at task 1: `identity` and `graph` as the
  arms of the first runs, the gate of step 12, and the approval file of step 13.
- 45 tasks are few for correctness. The requirements estimate about 155 tasks to see a 10-point paired
  difference with 80% power (web, not machine-checked). The cheaper sets can show a cost ratio; a correctness
  difference will likely have a wide interval, and the report must say so.
- Cost of the cheaper sets is not estimated in the requirements. The run sheet gives a ceiling from the stub's
  run count and the dated prices; the first real trials correct it.
- Pull-request grading needs each repository's toolchain and a clean way to run its tests. A repository whose
  tests cannot run offline or in bounded time is replaced before the pre-run commit, never after.
- Models may have seen the public pull requests. Differences between arms are published, never a score.
- A warm store can flatter arms `lessons` and `all`: warm-up tasks are committed first and a cold-store result
  is published too (M-02 risks).
- Transcripts are not in git. The manifest's hashes prove a copy is the run's own; a transcript lost with the
  machine cannot be shown.
- Injected text is billed again as cached input on later turns. The measurement includes that; the estimate of
  058 does not, which is one reason the two are printed apart.
- Numbers are from Claude Code alone until a Codex adapter exists, so no sentence says anything about Codex.
