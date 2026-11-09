# 021: Benchmark harness and reporting rules

**Status** not started · **Stage** Lessons · **Entries** M-02 (the harness, the stub agent, arms `off` and
`identity`, the statistics, the output folder, where the shipped checks live), M-08, core page section 8
(phase 0 question 5) · **Needs** 013 · **Feeds** 022, 041, 057, 059

## Result

A second program, `lobebench`, is built from the checkout. It runs the same task several times in different
arms, writes one line per run, and writes a summary that compares each task with itself across arms. With the
scripted stub agent it needs no network and is never publishable. The released program gains
`tentaqles lobe bench check-claims`, which fails on a figure in the docs that no results file backs.

```bash
go build ./bench/cmd/lobebench
lobebench run --agent stub --fixture two-tasks --arms off,all --trials 3 --out "$W/r1"
cat "$W/r1/summary.json"
tentaqles lobe bench check-claims --root .
```

The first `run` writes 12 run lines and a summary whose cost ratio of totals is 0.829. `check-claims` exits 0
on the repository and exits 1 on a document that says a number without a receipt.

## Scope

**In**

- The program `bench/cmd/lobebench` with the actions `run` and `report`, and the packages `bench/internal/arm`,
  `bench/internal/host` (`stub` and the Claude Code adapter), `bench/internal/grade` (the question grader) and
  `bench/internal/stat`.
- The task file format under `bench/tasks/`, one committed question task, and the embedded fixture `two-tasks`
  in `bench/testdata`.
- The four arms' settings table (M-02 step 2) in `bench/internal/arm`. Only `off` and `identity` are run with a
  real agent here.
- The run line, the summary, the manifest, the report with its markers, the transcripts folder and
  `bench/prices.json` (M-02 steps 5 to 7, M-08 step 1).
- The paired statistics of M-02 step 6: paired bootstrap, McNemar's exact test, ratio of totals.
- `"publishable":false` with its reasons for a stub run, a fixture run and a run with fewer than 3 trials.
- Phase 0 question 5: a throwaway Go test under `bench/` that runs one task in arms `off` and `identity` with
  the Claude Code adapter, and its written result. The owner starts the one real run.
- M-08 whole: `internal/lobe/benchcheck/claims`, the action `tentaqles lobe bench check-claims --root <dir>`,
  `test/integration/lobeclaims_test.go`, `bench/results/.gitignore` and the transcript tests.
- `TestLobeBenchShipsOnlyTheFourChecks`, which can pass only now that the fourth shipped check exists.

**Out**

- Any paid run beyond the one phase 0 run. Real repeat-mistake runs are feature
  [022](022-repeat-mistake-benchmark.md); real token runs are feature [059](059-token-benchmark.md).
- The question and pull-request task sets (10 and 5 per repository, 3 public repositories pinned by commit), the
  pull-request grader, the SWE-bench Verified set and its grader, and the second interval that resamples whole
  repositories (feature 059). The SWE-bench run also waits for the owner's approval (decision D-15).
- Arms `graph` and `all` with a real agent, the graph build before a run, `graph_build_ms` and
  `TestLobeBenchGraphBuiltBeforeTheRun` (feature 059, after feature
  [043](043-graph-of-one-repository.md)). Here the two arms exist as settings and as scripted fixture arms only.
- Arm `lessons` with a real agent, its warm store and warm-up tasks (feature 022 for the repeat benchmark,
  feature 059 for M-02's comparison of `lessons` against `identity`).
- The actions `repeat` (feature 022), `enforce` (feature [041](041-enforcement-accuracy.md)) and `graphdiff`
  (feature [057](057-graph-accuracy.md)).
- The Codex adapter. It follows when the usage fields Codex prints in headless mode are confirmed (M-02 step 8,
  decision A-26); no feature in the index names it, see Risks.
- The estimate of M-01 printed beside a measured difference (feature [058](058-token-savings.md)).

## What it builds on

- **002**: `tentaqles lobe sandbox <folder>` builds the home with `acme` and `globex`. `lobebench` runs that
  command; it never calls `reach`, `add` or any person-only command.
- **005**: the settings under the manifest's `lobe` key and `tentaqles lobe settings`, which names an invalid
  value. The arm builder writes settings as a fixture, the way the `lobeset` helper of the standard setup does.
- **012 and 013**: the verb `tentaqles lobe bench` in `internal/lobe/cli` with the actions `latency`, `stall`
  and `leak`, their packages under `internal/lobe/benchcheck/`, and the rule that an `--out` inside the real
  `TQ_HOME` or inside a client's folder is refused. This feature adds the fourth package, `claims`.
- **003 onward**: the lobe hooks in the plugin. An arm other than `off` is a home where they are installed.
- Existing repository rules: `.goreleaser.yaml` builds `./cmd/tentaqles` under two names and nothing else;
  `test/integration` holds the repository-wide checks; `internal/testutil` isolates the home in Go tests.

## What it leaves for later features

- `lobebench run` and `lobebench report`, the run folder layout, and the keys of `runs.jsonl`, `summary.json`
  and `manifest.json` (022, 041, 057, 059).
- `bench/internal/host`: the adapter interface, `stub`, and the Claude Code adapter (022 widens the stub so
  that it drives the real hooks; 059 adds arms and, when confirmed, Codex).
- `bench/internal/arm`: `Build(arm, dir)` and the settings table (022 adds `lessons-model-off`; 059 adds the
  graph build and its timing).
- `bench/internal/stat`: `PairedBootstrap`, `McNemarExact`, `RatioOfTotals`, nearest-rank percentiles (022
  resamples traps with it; 041 and 059 use it as it is).
- `bench/internal/grade`: the grader interface and the question grader (059 adds the pull-request grader).
- `bench/prices.json` and the cost formula (022, 059).
- The receipt marker `<!-- bench:<run-id>/summary.json#<key> -->` and `check-claims` (every feature that
  publishes a number: 022, 041, 057, 059).
- `bench/phase0/q5-result.md`: which usage fields Claude Code really prints, and how a headless session is
  started with the plugin's hooks (022, 059).

## How it works

Shared rules are not repeated here: the arms and their settings are M-02 step 2, the settings keys are core
page 5.6, the sandbox is F-07 step 8, and what a real run gates is M-02 step 8 and core page section 8.

1. **Two homes for the code** (M-02, "How to build it"). `lobebench` lives in `bench/cmd/lobebench` in the
   same Go module and is built with `go build ./bench/cmd/lobebench`. Nothing under `cmd/` or `internal/`
   imports a package under `bench/`. The release keeps building `./cmd/tentaqles` only, so the harness, its
   tasks and its results are in no release archive. `lobebench` runs the `tentaqles` that is first on `PATH`.
2. **A task** is one YAML file under `bench/tasks/`. This slice defines the format for a question: an id, the
   repository URL, the pinned commit, the prompt, the time limit (proposed: 10 minutes for a question) and the
   answer key, a list of files and symbols written before any run. The question grader passes a run when the
   agent's final answer names every file and symbol of the key. One question task is committed here, on one
   public repository pinned by commit (M-02 names PocketBase; the choice is recorded in the phase 0 result).
3. **An arm** is a folder built by `bench/internal/arm`. `off` is a machine without tentaqles: a fresh `HOME`
   and agent config folder, no plugin, and a `PATH` on which `tentaqles` and `tq` do not resolve. Every other
   arm is a fresh home built with `tentaqles lobe sandbox`, with the arm's row of M-02's table written under
   `lobe` in the manifest of `acme`, `documents: off`, `share: private` and `retention_days` left at its
   default. The clone sits inside the client's folder and the agent's config folder is the client's own login
   folder, because a lobe hook does nothing unless the two belong to one client (core page 5.1).
4. **A run** gets a fresh clone at the task's commit, a fresh home, the same host version, model id, prompt,
   built-in tools, permission mode, turn limit and time limit in every arm (M-02 step 3). The provider key
   comes from one environment variable of the person who runs the benchmark. It is passed to the agent's
   process and written to no file under `--out`.
5. **The host adapter** has one job: start one agent session for a prompt in a folder with an environment, wait
   until it ends or the time limit cuts it, and return the transcript, the four billed token counts (input,
   cache write, cache read, output), the tool call count, the wall time and the final answer. Two adapters
   exist here. `stub` replays the scripted runs of a fixture and uses no network. `claude` starts Claude Code in
   headless mode. Its exact command line and the names of the usage fields are settled by the phase 0 test
   (step 12) and written into the adapter from that result.
6. **The order.** All runs of all arms and trials are shuffled with a seed. The seed is recorded in the
   manifest, and the same seed gives the same order. At least 3 trials are needed for a publishable run.
7. **The run line.** Each run appends one line to `runs.jsonl`. The requirements name `outcome` (`pass`, `fail`
   or `invalid`), `fail_reason` (`wrong`, `timeout`, `turn-limit`, `crash`, `empty-patch`), `usd` and
   `graph_build_ms` (feature 059). This file names the rest: `task`, `arm`, `trial`, `host`, `model`,
   `input_tokens`, `cache_write_tokens`, `cache_read_tokens`, `output_tokens`, `tool_calls`, `wall_ms` and
   `injected_bytes`. A failed run keeps its tokens, cost and time in every total.
8. **Cost.** `usd` is computed from `bench/prices.json`, a dated file that holds, per model id, the input and
   output prices per million tokens and the two multipliers: cache write at 1.25 times input and cache read at
   0.1 (M-02). No price and no multiplier is a constant in code.
9. **A run in the wrong state** gets the outcome `invalid`, stays out of the totals and is rerun; the count of
   such runs is printed. This slice checks the two arms it runs for real: lobe text found in a transcript of
   arm `off`, or of arm `identity` where every lobe setting is off. The checks for `lessons`, `graph` and `all`
   arrive with the features that run those arms.
10. **Statistics** (M-02 step 6), per pair of arms, with trials averaged per task. Correctness: the difference
    in mean pass rate with a 95% interval from a paired bootstrap (tasks resampled with replacement, the same
    draw for both arms, 9,999 times, proposed), and McNemar's exact test on each task's majority outcome, a tie
    counting as a fail. Cost, tokens, calls and time: the ratio of totals with the same interval. Figures over
    tasks both arms passed may appear only as an extra, labeled column.
11. **Output.** `--out` names a new folder; an existing one is refused with exit 1 and nothing is changed. The
    folder gets `manifest.json`, `runs.jsonl`, `summary.json`, `report.md` and `transcripts/`. The manifest holds
    what M-08 step 1 lists: the tentaqles commit, a dirty-tree flag, the harness version, the host and its
    version, the exact model id, the date, the system, the seeds, the task-set hash, each arm's settings and
    the SHA-256 of every transcript. `summary.json` holds per arm `passed`, `failed`, `invalid` and `usd_total`,
    per pair of arms the figures of step 10 as `{"mean","lo","hi","n"}` objects (`usd_ratio_of_totals` among
    them), and at the top `host`, `model`, `date`, `publishable` and `why`. `why` lists every reason that
    applies: `stub-agent`, `fixture`, `trials-under-3`. `report.md` is written with the receipt markers already
    in place. `lobebench report --run <dir>` rebuilds `summary.json` and `report.md` from the manifest and the
    run lines. `--out` is refused inside the real `TQ_HOME` and inside a client's folder; the one exception is
    `bench/results/` of the checkout the program was built from (M-08, risks).
12. **Phase 0 question 5.** A Go test under `bench/phase0/`, skipped unless an environment variable switches
    it on, runs the committed question task once in arm `off` and once in arm `identity` with the `claude`
    adapter and records tokens, calls, time and outcome for both. The builder runs it with the stub first. The
    owner then runs it once for real with their own key. The result, `bench/phase0/q5-result.md`, holds counts
    and field names only: the usage fields found, the command line that worked, how the plugin's hooks were
    loaded in the sandbox home, and the two run lines.
13. **The claims check** (M-08 step 10). `tentaqles lobe bench check-claims --root <dir>` reads `README.md`,
    `CHANGELOG.md`, the plugin's README and `docs/` except `docs/prd/` and `docs/features/` under the root. A figure
    is a number written with `%`, a multiplier such as `4x`, or a currency. Near means in the same sentence or the
    same table row (proposed). It prints one line per finding with the file, the line and the rule, and exits 1 on any
    finding. A marker's path is read under `<root>/bench/results/`.

    | Rule name | Fails when |
    |---|---|
    | `banned-phrase` | the text holds "up to", "as much as" or "best case" |
    | `no-receipt` | a figure stands near a claim word (tokens, cost, cheaper, faster, fewer, errors, saved) with no marker |
    | `number-mismatch` | the figure differs from the receipt's value rounded to the digits shown; signs are ignored (proposed) |
    | `no-interval` | the sentence or row gives no interval |
    | `no-model`, `no-date` | the sentence or row does not name the model id or the run date |
    | `not-publishable` | the receipt says `"publishable":false` |
    | `mixed-table` | the markers of one table name two runs |

    Figures inside code blocks are skipped. Material quoted from other projects is skipped only between
    `<!-- bench:quote -->` and `<!-- bench:end -->`. A receipt whose folder is missing is an error. The rule
    names `banned-phrase`, `no-receipt`, `number-mismatch`, `no-interval`, `not-publishable` and `mixed-table`
    are the requirements'; `no-model` and `no-date` are named here.
14. **Transcripts stay out of git.** `bench/results/.gitignore` holds the one line `*/transcripts/`. A test
    fails when a file under a `transcripts/` folder is tracked, or when a committed manifest names a transcript
    without its SHA-256.

## Where to read

| What | Where |
|---|---|
| Requirements | [M-02](../prd/lobe-benchmarks.md) in full; [M-08](../prd/lobe-benchmarks.md) in full; the two opening sections of that page ("What we claim", "What Graft did well") |
| Requirements, shared | [core page](../prd/vertical-lobe.md) section 8 (phase 0 question 5, what a real run gates), 5.6 (settings), decisions D-7, D-15, A-26, A-28; [F-07](../prd/lobe-foundation.md) steps 7 and 8 and the standard tester setup; [F-06](../prd/lobe-foundation.md) |
| Source, idea | Graft 0.17.0, the local copy at `sources/source-code` beside this repository: `README.md` L128, L130, L522 and L570 (arms of one agent, cache-aware cost, 3 trials, transcripts audited); L23 and L30 (what to avoid) |
| Source, idea | agentmemory [`eval/README.md` L79](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L79), L80 and [L84](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L84): a manifest with fingerprints, one record per case, existing output folders refused |
| Source, idea | agentmemory [`eval/README.md` L107](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L107) and [`test/website-meta.test.ts` L86](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/test/website-meta.test.ts#L86): receipts for each case; a test holds published figures equal to the receipt files |
| Source, numbers | Trail-code `src/context/price.ts` L50, L51 (the two cache multipliers) and L17 (input only; we price output too), at the commit pinned in core page section 7 |
| Existing tentaqles code | `.goreleaser.yaml` (both builds have `main: ./cmd/tentaqles`); `test/integration/release_names_test.go`; `test/integration/docs_test.go` (`documentedGroups`); `internal/testutil/home.go`; the `tentaqles lobe bench` verb and `internal/lobe/benchcheck/` as features 012 and 013 left them |

## Build tasks

1. **Check this brief against the code.** Read `internal/lobe/cli`, `internal/lobe/benchcheck/` and the sandbox
   command as features 002, 012 and 013 left them. List what no longer fits. Raise the open point of
   "Risks and unknowns" that needs the owner (the benchmark credential) before any code is written.
2. **Phase 0 question 5, the throwaway test.** Write `bench/phase0/` with the smallest adapter that starts
   Claude Code headless on one prompt, and the Go test of step 12. Run it with a fake `claude` program first.
   Hand the owner the one command for the real run. Write `bench/phase0/q5-result.md` from what comes back.
   Proof: the file exists and answers yes or no to "tokens, calls, time and outcome are recorded in both arms".
3. **`bench/internal/stat`.** Paired bootstrap with a seed, McNemar's exact test, ratio of totals, nearest-rank
   percentiles, no outside dependency. Tests: `TestLobeBenchMcNemarExact`, `TestLobeBenchPairedBootstrap`. Can
   run beside tasks 4 and 5.
4. **Prices and cost.** `bench/prices.json` and the cost function. Tests: `TestLobeBenchCostIsBilledCost`. Can
   run beside tasks 3 and 5.
5. **`internal/lobe/benchcheck/claims`.** `scan.go`, `rules.go`, `receipt.go`; the action `check-claims` on the
   existing verb. Tests: the five `TestLobeClaims_*` tests and `TestLobeBenchShipsOnlyTheFourChecks`. Can run
   beside tasks 3 and 4.
6. **`bench/internal/arm`.** The settings table and `Build`. Test: `TestLobeBenchArmsUseTheSettingsKeys`.
7. **`bench/internal/host` and `bench/internal/grade`.** The adapter interface, `stub`, the `claude` adapter
   written from task 2's result, the question grader, the fixture `two-tasks` in `bench/testdata`.
8. **`bench/cmd/lobebench`.** `run` and `report`: shuffle, run lines, wrong-state check, summary, manifest,
   report with markers, transcripts and their hashes, the `--out` refusals. Tests:
   `TestLobeBenchCountsFailedRuns`, `TestLobeBenchRefusesUsedOutputFolder`,
   `TestLobeBenchStubIsNeverPublishable`, `TestLobeBenchRefusesOutInsideAClient`,
   `TestLobeBenchKeyNeverInResults`, `TestLobeBenchSameSeedSameOrder`.
9. **Repository checks.** `bench/results/.gitignore`, `test/integration/lobeclaims_test.go`,
   `TestLobeBenchHarnessIsInTheRepo`, `TestLobeBenchTranscriptsStayOutOfGit`.
10. **Docs.** A `check-claims` part in the `tentaqles lobe bench` section of `docs/reference/commands.md`; a
    short `bench/README.md` that says how to build and run `lobebench` and how a number is published; a line in
    `CHANGELOG.md`; the `bench/` folder in the layout list of `CLAUDE.md`.
11. **Tester run.** A fresh agent runs the Evaluation below.

## Tests that stay in the repository

Names are the requirements' unless marked "named here".

- `TestLobeBenchCostIsBilledCost`: at fixture prices of 2 (input) and 10 (output) dollars per million, a run
  of 1000 input, 400 cache write, 10000 cache read and 200 output costs 0.007; one of 800, 400, 6000 and 200
  costs 0.0058.
- `TestLobeBenchCountsFailedRuns`: the fixture (2 tasks, arms `off` and `all` with those two usages, 3 trials,
  one `off` run timing out) totals 0.042 and 0.0348 dollars, the timed-out run included.
- `TestLobeBenchMcNemarExact`: 6 against 0 gives 0.03125; 8 against 2 gives 0.109375.
- `TestLobeBenchPairedBootstrap`: two identical arms give the interval 0 to 0; the same seed gives the same
  interval.
- `TestLobeBenchRefusesUsedOutputFolder`: a second run into the same folder exits 1 and changes no file.
- `TestLobeBenchHarnessIsInTheRepo`: `bench/cmd/lobebench` and `bench/tasks` exist; the test fails if they go.
- `TestLobeBenchStubIsNeverPublishable`: a run with `--agent stub` or `--fixture` writes `"publishable":false`
  and names the reason in `why`.
- `TestLobeBenchArmsUseTheSettingsKeys`: each arm's manifest holds exactly the values of M-02 step 2's table,
  and `tentaqles lobe settings` reports none of them as invalid.
- `TestLobeBenchShipsOnlyTheFourChecks`: `tentaqles lobe bench` offers exactly `leak`, `latency`, `stall` and
  `check-claims`, and the packages `./cmd/tentaqles` depends on include none under `bench/`.
- `TestLobeClaims_BannedPhraseFails`, `TestLobeClaims_MismatchFails`, `TestLobeClaims_MixedTableFails`,
  `TestLobeClaims_NotPublishableFails`: each bad document exits 1 with its rule (`banned-phrase`,
  `number-mismatch`, `mixed-table`, `not-publishable`; a receipt with fewer than 3 trials cannot back a claim).
- `TestLobeClaims_RepositoryIsClean`: the repository's own documents pass.
- `TestLobeBenchTranscriptsStayOutOfGit`: 0 tracked files under any `bench/results/*/transcripts/`, and every
  transcript a committed manifest names has a SHA-256 beside it.
- `TestLobeBenchRefusesOutInsideAClient` (named here): an `--out` inside the real `TQ_HOME` or inside a
  client's folder is refused with 0 bytes written; `bench/results/` of the checkout is accepted.
- `TestLobeBenchKeyNeverInResults` (named here): with the key variable set to a marker word, a stub run leaves
  that word in 0 files under `--out`. This is the "no secret on disk" rule for the harness.
- `TestLobeBenchSameSeedSameOrder` (named here): two runs with one seed give the same order of run lines.
- House rules kept green: every test under "Must not trade off" in `CLAUDE.md`, unedited;
  `TestGoreleaserBuildsOneProgramUnderTwoNames`; `TestDocsReferenceEveryCommand`; the import-rule tests of
  feature 003 (the `claims` package is a lobe package and keeps the four rules of core page 5.1); the leak
  suite of feature 013 at zero leaks. All bench tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`.

## Evaluation

The tester gets the built `tentaqles`, the built `lobebench` from the same commit (`go build
./bench/cmd/lobebench`), both first on `PATH`, and an empty working folder. No network.

Run the standard tester setup (feature [002](002-sandbox.md); it is the block in F-07's tester brief). It
exports `HOME`, `USERPROFILE` and `TQ_HOME` inside the working folder `$W`. Export nothing else: no login
variable and no provider key.

1. `lobebench run --agent stub --fixture two-tasks --arms off,all --trials 3 --out "$W/r1"`. Expect exit 0
   and 12 lines in `$W/r1/runs.jsonl`.
2. `$W/r1/summary.json`: arm `off` has `passed` 5, `failed` 1 and `usd_total` 0.042; arm `all` has `passed` 6,
   `failed` 0 and `usd_total` 0.0348; `usd_ratio_of_totals` is 0.829; `publishable` is `false` and `why` holds
   `stub-agent`.
3. Exactly one line of `runs.jsonl` has `"outcome":"fail"`, with `"fail_reason":"timeout"` and `usd` 0.007.
4. Run the command of check 1 again. Expect exit 1, a message naming `$W/r1`, and no file changed.
5. Run it with `--trials 2 --out "$W/r2"`. Expect exit 0, and in `$W/r2/summary.json` a `why` that holds both
   `stub-agent` and `trials-under-3`.
6. `tentaqles lobe bench run --out "$W/r3"; echo "exit=$?"` prints a line that is not `exit=0`, and `$W/r3`
   does not exist: the released program has no `run` action.
7. `tentaqles lobe bench --help | grep -c -E '^  (leak|latency|stall|check-claims) '` prints `4`, and the same
   command with `(run|report|repeat|enforce|graphdiff)` prints `0`.
8. `lobebench run --agent stub --fixture two-tasks --arms off,all --trials 3 --out "$W/clients/acme/r4";
   echo "exit=$?"` prints a line that is not `exit=0`, and `$W/clients/acme/r4` does not exist.

For checks 9 to 14 create `$W/c/bench/results/r1/summary.json` with the one line of the first block below.
The second block is the line for check 9 and the third is the line for check 10. After each change run
`tentaqles lobe bench check-claims --root "$W/c"`.

```text
{"publishable":true,"model":"m-1","date":"2026-11-20","usd_change_pct":{"mean":-17.1,"lo":-25.0,"hi":-9.2,"n":45}}
```

```text
The lobe is up to 4x cheaper.
```

```text
Cost fell 17% (95% interval 9% to 25%, 45 tasks) against arm identity, Claude Code, model m-1, 2026-11-20. <!-- bench:r1/summary.json#usd_change_pct -->
```

9. Create `$W/c/README.md` holding only the second block's line. Expect exit 1 and output naming `README.md:1`
   and `banned-phrase`.
10. Replace it with the third block's line, all on one line. Expect exit 0.
11. Change the first figure of that line from 17 to 27. Expect exit 1 and `number-mismatch`.
12. Restore it and delete the part in parentheses. Expect exit 1 and `no-interval`.
13. Restore the line of check 10 and delete the marker. Expect exit 1 and `no-receipt`.
14. Restore the marker and set `"publishable":false` in `summary.json`. Expect exit 1 and `not-publishable`.

**Person checks** (the owner, in a terminal of their own)

1. The phase 0 run. With the provider key exported and nothing of a real client in the environment, run the
   one command task 2 hands over. Expect two run lines, one for arm `off` and one for arm `identity`, each with
   four token counts, a tool call count, a wall time and an outcome. The cost of this run is the cost of two
   short sessions on one question. Hand back `bench/phase0/q5-result.md`.

Scoring: PASS needs 14 of 14. "Could not run" is a FAIL. The person check does not hold back the gate of this
feature's code, but task 2 is not finished until its result is written, and no later feature may start a real
run before it.

## Done when

- [ ] Every test above is in the repository and green in CI: `go vet ./...`, `go test ./... -count=1` (which
  now covers `./bench/...`) and `go build ./...` on the three systems.
- [ ] The tester brief passes, 14 of 14.
- [ ] The house rules hold: the "Must not trade off" tests are unedited and green, the leak suite reports zero
  leaks, `TestLobeBenchKeyNeverInResults` and `TestLobeBenchRefusesOutInsideAClient` are green.
- [ ] The release still builds only `./cmd/tentaqles`: `TestGoreleaserBuildsOneProgramUnderTwoNames` and
  `TestLobeBenchShipsOnlyTheFourChecks` are green.
- [ ] `bench/phase0/q5-result.md` is written from the owner's one real run.
- [ ] `docs/reference/commands.md`, `bench/README.md`, `CHANGELOG.md` and `CLAUDE.md` are updated.
- [ ] The owner has seen the stub run of Result and one `check-claims` refusal.

## Risks and unknowns

- **Claude Code's headless output is not verified.** Which flags give one JSON result, and what the token,
  cost, turn and time fields are called, comes from the web and from memory. The phase 0 test settles it and
  the adapter is written from its result, not before.
- **How a sandbox home gets the plugin's hooks is not settled by the requirements.** `tentaqles lobe sandbox`
  installs no plugin (F-07 step 8), yet an arm other than `off` needs the guard's and the lobe's hook entries.
  Whether the adapter loads the checkout's `plugin/` folder for one session, and whether the session is started
  through `tentaqles run` or directly, is decided in the phase 0 test and written into its result.
- **The benchmark credential.** M-02 step 3 says the provider key comes from one environment variable. A fresh
  login folder has no subscription login, so a real run needs an API key and is billed to it. The owner
  confirms this before the phase 0 run.
- **`docs/features/` and the claims scan.** These feature files quote the same example figures as the
  requirements pages, so the scan leaves out `docs/features/` as it leaves out `docs/prd/` (M-08 step 10).
- **The scan is a heuristic** (M-08, risks). It will miss a claim worded in a new way. A person still reads
  every release note. Whether inline code spans count as "code blocks" is not stated; this file reads fenced
  blocks only as skipped.
- **Work folders under a committed run folder.** A real run keeps its homes and clones under `--out`, and
  M-02 step 7 commits four files of that folder. The one-line `.gitignore` covers `transcripts/` only. Until
  the owner says otherwise, the person who commits adds the four files by name.
- **`lobebench report` has no syntax in the requirements.** `--run <dir>` is this file's choice, as are the
  run-line key names marked in step 7 and the rule names `no-model` and `no-date`.
- **No feature in the index names the Codex adapter.** Published numbers come from Claude Code first (decision
  A-26). Until the adapter exists no Codex number is published.
- **Cost of later runs** (M-02, risks) is an unchecked estimate. Nothing in this feature spends more than the
  one phase 0 run.
- What would force a change to this file: a phase 0 result that says Claude Code does not print the four token
  counts in headless mode, or an owner decision on the two points above.
