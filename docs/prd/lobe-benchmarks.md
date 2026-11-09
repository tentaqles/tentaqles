# Lobe: Benchmarks (M-01 to M-08)

This stage measures the product instead of describing it. M-08 sets the rules every published number follows. M-02
is the harness that runs the same tasks with parts of tentaqles switched on and off. M-03 to M-07 are five separate
tests, each for one claim or one safety rule. M-01 is what a person sees day to day: counters labeled as estimates
and checked against M-02. Section, principle and decision numbers on this page are those of the
[core page](vertical-lobe.md).

The code has two homes (M-02 gives the layout). The checks that need nothing but the program itself ship in the
released binary as actions of `tentaqles lobe bench`, the verb F-07 lists: `leak`, `latency`, `stall` and
`check-claims`. The task harness, its tasks and its results live in this repository's `bench/` folder, run as the
program `lobebench`, and are not part of a release.

## What we claim and how each claim is measured

| Promise (core page section 1) | The sentence we may publish | Measured by | Number reported |
|---|---|---|---|
| 1. One identity per client | The leak suite's attacks found nothing of one client in another client's session | M-03 | Attacks run and leaks found. Pass mark: 0 leaks |
| 2. Fewer tokens | The same tasks cost less with the graph on, with no loss of correctness; the graph was built before the runs, and its build time is reported apart from the tasks' cost | M-02 (arm `graph` against arm `identity`); M-07 (the graph is right); M-01 (the estimate shown in the product) | The figure M-02 step 6 computes: the ratio of total billed cost between the two arms with its 95% interval, beside the correctness difference; the graph's build time apart |
| 3. Fewer errors | A mistake an agent made in a client's work was learned there and came back less often the next time it was about to happen: the repeat rate at the second meeting was lower with the lobe than without | M-04 (repeat rate at the second meeting, the same rate at the third and fourth, and the tokens of each session); M-05 (blocks are right); M-02 (arm `lessons` against arm `identity`) | Repeat rate at the second meeting, with and without the lobe; the same rate at the third and fourth meeting; billed tokens per session; wrong blocks; missed mistakes |

Promise 3 says "learned there and not made twice" (core page section 1). A lesson learned from a failure and its
fix speaks at the next matching call (D-9, core page 5.5), so the measured claim in row 3 is the repeat rate at the
second meeting. The rates at the third and fourth meeting are still reported: they show whether a lesson was
demoted or faded.

No row is published before its tests have run. M-06 serves no promise: it holds the anti-goal that a hook never
stalls a session. M-08 applies to every row. A run with a real agent gates a published number, never a phase: a
phase ends on checks that need no paid agent (core page section 8), and a real run that misses its target means
the claim is not made and the miss is recorded. Published numbers come from Claude Code first, and each names its
host and model (M-02, step 8).

## What Graft did well and what we do differently

Graft 0.17.0 is the model for M-02. Its README does several things right, and we keep them: the same model, Docker
images and turn limits on both arms (`README.md` L148); the official SWE-bench grader with no judge model (L146,
L163); cost counted at cache prices (L128); 3 trials (L130); real merged pull requests rebuilt from their base commit
(L522); the questions and pull requests listed (L539 to L563); every transcript checked to confirm the tool ran in
one arm and not in the other (L570).

| What the Graft README shows | Where | What we do |
|---|---|---|
| The headline table mixes two tests. Tool calls, tokens and time (+46%, +42%, +60%) come from a question sweep. Correctness (54% to 66%) comes from SWE-bench. In the sweep itself correctness was equal, 93% and 93% | `README.md` L25 to L30, L135 to L138, L152 | One table holds one test (M-08) |
| Correctness was 33 against 27 of 50. The trial count and the per-instance outcomes are not given, so no paired test can be computed. If 6 instances flipped one way and none the other, the exact p-value is 0.031; at 8 against 2 it is 0.109 (our arithmetic) | `README.md` L152 | At least 3 trials, per-task outcomes published, a paired interval (M-02) |
| Tokens, cost and calls were counted only on the instances both arms resolved | `README.md` L163 | Every run counts, failed ones included (M-02) |
| "Up to 4x cheaper and 3x faster." No table shows either figure. The sweep is 1.47x on cost and 2.52x on time, PocketBase is 1.26x on cost, and the largest single figure quoted is 2.6x (our arithmetic) | `README.md` L23, L524, L134, L137, L530, L534 | The mean with its interval, never "up to" (M-08) |
| One of the sweep's two repositories is Graft itself. The other is not named, so it cannot be rerun | `README.md` L130 | Public repositories pinned by commit; our own repository reported apart (M-02) |
| The harness was removed from the published repository in version 0.7.0 | `CHANGELOG.md` L618 | `bench/` stays, and a test fails if it goes (M-02) |

Two more weak points. Questions were graded by a judge model with a keyword floor (L128). A pull request task passed
when the diff touched the same files as the merged one (L570), which does not show that the code works. M-02 grades
with tests.

## M-01 Token savings accounting

**Phase** 4 · **Origin** port: Trail-code · **Depends on** F-01, F-07, R-02, R-03, R-04, R-05, G-09, S-08, C-03,
C-08, E-01, E-04, E-05, E-06

**What it is.** The lobe keeps counters, per client, of what it put in front of the agent and what that probably
saved. `tentaqles lobe status --savings` shows them. Every token figure in the product is an estimate and says so.
Measured figures come only from M-02.

**Why it matters.** It serves promises 2 and 3: a person can see whether the lobe earns its place in their own work.
Without honest accounting the product would print large numbers nobody can check.

**Mechanism**

1. Each lobe code path that prints text for the agent appends one line to `stats/stats-YYYYMMDD.jsonl` after its
   output is flushed:
   `{"v":1,"at":"2026-11-09T14:03:11Z","session":"abc","part":"graph","sent":412,"low":1830,"high":9644}`.
   Those paths are the ones in the hook table of core page 5.2 that print (R-02, R-04, R-05, G-09, the notes of
   C-08, E-05 and E-06, a deny from E-01 or E-04) and R-03's commands under `tentaqles lobe graph`. A call that
   prints nothing, such as a prompt with only a weak match (R-02), writes no line.
2. `sent` is the exact UTF-8 byte length of everything the agent receives from that call, headers included.
3. For a graph answer, `high` is the summed size of the distinct files the answer names (Trail-code's baseline: each
   file read whole) and `low` is the size of only the symbol spans it points to. A lesson has no reading baseline;
   its value is counted in step 5.
4. Tokens are `(bytes + 2) / 4` in integer math, equal to Trail-code's `Math.round(chars / 4)`. A line saves
   `tokens(max(0, baseline - sent))`, shown as a range from low to high. `high` is capped at 400,000 bytes per line
   (proposed); a capped line is marked and counted at the cap.
5. Error counters: `failures`, `fingerprints` (S-08), `repeats_after_lesson` (a failure whose fingerprint a live
   lesson names), `advice_shown`, `advice_then_ok`, `blocks`, and the tokens of the two model steps of core page
   5.7: `draft_tokens` (C-03's drafting sessions) and, from phase 5, `document_tokens` (G-11's reading sessions).
6. `tentaqles lobe status --savings [--days N] [--json]` sums the last N days (default 7, proposed). It is a new
   flag on F-07's `status` verb. Nothing about savings is printed into the agent's context. Stats lines hold
   numbers and ids only and are kept 90 days (proposed).
7. Limits, printed with the numbers: bytes divided by 4 is not a tokenizer. `high` assumes whole files would have
   been read and `low` only the right spans; neither is a bound. `advice_then_ok` does not prove the advice
   prevented an error. Each M-02 report prints this estimate beside the measured difference for the same runs.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/context/savings.ts` L28](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/context/savings.ts#L28): tokens are characters divided by 4, rounded |
| Trail-code (MIT) | port, changed | [`src/context/savings.ts` L117](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/context/savings.ts#L117): the output is measured without the savings header that L156 puts in front of it |
| Trail-code (MIT) | not taken | [`src/graph/map.ts` L283](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/map.ts#L283): the map's baseline is every file in the graph, so the claim grows with the repository |
| Trail-code (MIT) | not taken | [`src/claude/hooks.ts` L272](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hooks.ts#L272): the session total is parsed back out of tool output (file-reading tools excepted), so any text of that form adds to it |
| Trail-code (MIT) | not taken | [`src/context/price.ts` L19](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/context/price.ts#L19): a price table fixed in code; an unknown model gets no dollars |
| Graft 0.17.0 (MIT) | idea | `scripts/tally-audit.mjs` L35: a turn of 1,000,000 tokens or more is called an estimator artifact and left out of the median |

**How to build it**

- Package `internal/lobe/stats`: `stats.go` (`Line`, `Append`, `Sum`) and `tokens.go` (`ToTokens`). Appends follow
  F-01; a failed append is dropped and the hook's result does not change.
- With `--json` the output gains a `savings` object: `{"injected":{"count","bytes","tokens_est"},"graph":{"answers",
  "sent_bytes","low_bytes","high_bytes","saved_low_tokens_est","saved_high_tokens_est","capped"},"errors":{...}}`.
- Deliberate differences from Trail-code. Counted where the text is produced, never parsed back. One unit, UTF-8
  bytes: Trail-code stores UTF-16 length at `src/graph/extract.ts` L379 and bytes at `src/graph/generic.ts` L247.
  No dollars, because a price table in code goes stale. The agent is never asked to recite a total: Trail-code
  asks for one (`src/context/savings.ts` L104), which costs output tokens.
- Edge cases: a file named twice counts once; a file missing from the graph counts 0; an answer larger than its
  baseline saves 0, never a negative number; an answer that names no file has no baseline.

**How we know it works**

- `TestLobeStatsTokensRound`: `ToTokens` of 0, 1, 2, 6 and 8000 gives 0, 0, 1, 2 and 2000.
- `TestLobeStatsSentCountsHeader`: a 40-byte body with a 20-byte header records `sent` 60.
- `TestLobeStatsNeverNegative`: baseline 100 bytes, sent 400 bytes, saving 0.
- `TestLobeStatsIgnoresSavingsText`: tool output holding `tokens saved ≈ 99,999` changes no counter (yes or no).
- `TestLobeStatsAppendFailureFailsOpen`: with the stats folder unwritable the hook still exits 0 with its output.

**Tester brief**

Start from the standard tester setup in F-07. Run `git init "$W/clients/acme/m01"`; `M` is that folder. Commit two
files in it: `a.py`, exactly 4,000 bytes, starting with the lines `def canary_upsert(a, b):` and `    return a` and
filled to size with lines of `#`; and `tiny.py`, holding the one line `def canary_tiny(): return 1`. In `M` run
`tentaqles lobe graph build`; expect exit 0. Export `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`. In each
JSON below, `<M>` stands for the path in `M`.

1. In `M`, pipe `{"session_id":"m01","hook_event_name":"UserPromptSubmit","cwd":"<M>",
   "prompt":"where is canary_upsert defined"}` into `tentaqles lobe-hook claude user-prompt-submit`. Expect exit 0
   and injected text containing `a.py` (the decoded `additionalContext` string if stdout is JSON, otherwise all of
   stdout). Call its UTF-8 byte length S.
2. `tentaqles lobe status --savings --json`, under `savings`: `injected.count` is 1, `injected.bytes` is S,
   `graph.high_bytes` is 4000 and `graph.saved_high_tokens_est` is `(4000 - S + 2) / 4` rounded down.
3. Repeat check 1 with the prompt `where is canary_tiny defined`. `graph.saved_high_tokens_est` is unchanged.
4. Pipe `{"session_id":"m01","hook_event_name":"PostToolUse","cwd":"<M>","tool_name":"Bash",
   "tool_input":{"command":"echo done"},"tool_response":{"stdout":"[trail] tokens saved ≈ 99,999","stderr":""}}` into
   `tentaqles lobe-hook claude post-tool-use`. Expect exit 0 and the `graph` and `injected` numbers unchanged.
5. In `$W/clients/globex/app`, with `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude"`,
   `tentaqles lobe status --savings --json` shows 0 for every counter under `savings`. Then set the variable back.
6. In `M`, every line of `tentaqles lobe status --savings` that shows tokens contains the word `estimate`.

**Risks and open questions**

- The range may be too wide to mean much, and people will quote the high figure. M-08 forbids quoting it.
- Injected text is billed again as cached input on every later turn. The estimate ignores that; M-02 does not.
- Open: whether 90 days of number-only stats lines is acceptable next to decision A-9.

## M-02 Benchmark harness

**Phase** 0 · **Origin** idea: Graft · **Depends on** F-06, F-07 (neither is needed in phase 0; see step 7)

**What it is.** The task harness, a program named `lobebench` that lives in this repository's `bench/` folder and
is not part of a release. It runs the same task several times under different settings and records what each run
cost and whether it succeeded. An arm is one group of settings; a trial is one repeat of a task in an arm. Results
are paired: each task is compared with itself across arms. This entry also fixes where the four checks that ship
in the binary (`tentaqles lobe bench leak`, `latency`, `stall`, `check-claims`) keep their code.

**Why it matters.** Promises 2 and 3 are comparisons, and decision D-7 says each claim needs a benchmark. Without
one harness kept in the repository, every number would be a one-time claim nobody can rerun.

**Mechanism**

1. Tasks are files under `bench/tasks/`, of three kinds. **Questions**: 10 per repository, passed when the answer
   names every file and symbol in an answer key written before any run. **Pull requests**: 5 per repository, each a
   merged one that added or changed tests; the repository is reset to the base commit, and the run passes when
   those tests pass on the agent's patch and no earlier test breaks. **SWE-bench Verified**: 150 of its 500
   instances (proposed), drawn once with a recorded seed, graded by the official harness on Linux x86_64. The first
   two kinds use 3 public repositories pinned by commit (proposed), PocketBase among them; ours is reported apart.
2. Arms. `off` is a machine without tentaqles. Every other arm has it installed and differs only in the client's
   `lobe` settings, which are the keys of core page 5.6 (F-06):

   | Arm | `capture` | `lessons` | `graph` | `recall` | `enforce` | `model` | Runs from |
   |---|---|---|---|---|---|---|---|
   | `identity` | off | off | off | off | off | off | phase 0 |
   | `lessons`, the "lobe only" arm | on | on | off | on | advice | on | phase 2 |
   | `graph` | off | off | on | on | off | off | phase 4 |
   | `all` | on | on | on | on | advice | on | phase 4 |

   In every arm `documents` is `off`, `share` is `private` and `retention_days` keeps its default. `enforce` never
   goes above `advice` here; blocking is measured by M-05. In arm `graph` the graph is built and served with
   `lessons` and `enforce` off, and in arm `lessons` lessons work with no graph: core page 5.6 allows each half
   alone, which is what lets promise 2 and promise 3 be measured apart.
3. Every arm gets the same host version, model id, prompt, built-in tools, permission mode, turn limit and time
   limit (proposed: 10 minutes for a question, 30 for the rest), a fresh clone and a fresh `TQ_HOME`, `HOME` and
   agent config folder. In every arm but `off` that config folder is the client's own login folder and the agent
   starts inside the client's folder, because a lobe hook does nothing unless the two belong to the same client
   (core page 5.1). The agent's provider key comes from one environment variable of the person who runs the
   benchmark and is never written to a result file. `lessons` and `all` start from a frozen copy of a store warmed
   by fixed warm-up tasks that are not measured. For an arm that uses the graph (`graph` and `all`) the harness
   runs `tentaqles lobe graph build` in the fresh clone before the agent starts, so no run begins on a missing or
   half-built graph. The build's time is recorded apart (step 5) and is not part of the task's cost, tokens or wall
   time; the published sentence says that the graph was built before the run and gives the build's time. A run in
   the wrong state (lobe text in `off`, no lobe hook fired in `all`, no finished graph in an arm that uses it) gets
   the outcome `invalid`, stays out of the totals and is rerun; the count of such runs is printed.
4. At least 3 trials. All runs are shuffled with a recorded seed, so provider load does not fall on one arm.
5. Each run appends one line to `runs.jsonl`: task, arm, trial, host, model, `outcome` (`pass`, `fail` or
   `invalid`), `fail_reason` (`wrong`, `timeout`, `turn-limit`, `crash`, `empty-patch`), the four billed token
   counts (input, cache write, cache read, output), dollars from the dated `bench/prices.json`, tool calls, wall
   time, the lobe's injected bytes and, in an arm that uses the graph, `graph_build_ms`, the time of the build
   that ran before the agent started (step 3). That time is summed in a column of its own and enters no total of
   cost, tokens or wall time. A failed run keeps its tokens, cost and time in every total. Tokens of drafting
   sessions (C-03) are added to their arm.
6. Statistics per pair of arms, with trials averaged per task. Correctness: the difference in mean pass rate with a
   95% interval from a paired bootstrap (tasks resampled with replacement, the same draw for both arms, 9,999
   times, proposed), plus McNemar's exact test on each task's majority outcome, a tie counting as a fail (a test
   on the tasks where the arms disagree). Cost, tokens, calls and time: the ratio of totals with the same
   interval. Promise 2's figure is this one: the ratio of total billed cost of arm `graph` to arm `identity`,
   with its interval, as the claims table states. Where a task set spans 10 or more repositories (proposed), a
   second interval resamples whole
   repositories; otherwise each repository is listed apart. Figures over tasks both arms passed may appear only as
   an extra, labeled column.
7. Output goes to a new folder `bench/results/<run-id>/`: `manifest.json`, `runs.jsonl`, `summary.json`, `report.md`
   and a `transcripts/` folder. An existing folder is refused. The four files are committed. `transcripts/` is
   large: it stays under `bench/results/` on the machine that ran it, git ignores it, and the manifest keeps the
   SHA-256 of each transcript (M-08). Phase 0 builds steps 1 to 5 for one task and the arms `off` and `identity`,
   with one host adapter, Claude Code's, started by a Go test under `bench/`: the program `lobebench`, F-07's
   standard setup and the settings (F-06) arrive in phase 1.
8. What a real-agent run gates. A phase ends on checks that need no paid agent: unit tests, the tester briefs,
   M-03, M-06 and this harness on its fixtures with the `stub` agent (core page section 8). A run with a real
   agent gates a published number, not a phase. A real run that misses its target means the claim is not made,
   and the miss is recorded: its run folder is committed like any other (M-08, step 9). Published numbers come
   from Claude Code first, and each names its host and model. The Codex adapter follows when the usage fields
   Codex prints in headless mode are confirmed; until then no Codex number is published.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Graft 0.17.0 (MIT) | idea | `README.md` L128, L130, L522 and L570: arms of one agent, cache-aware cost, 3 trials; 10 questions and 5 merged pull requests per repository; transcripts audited |
| agentmemory (Apache-2.0) | idea | [`eval/README.md` L79](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L79), L80 and [L84](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L84): a manifest with fingerprints, one record per case, existing output folders refused |
| SWE-bench (web) | used as it is | <https://www.swebench.com/SWE-bench/reference/harness/> and <https://huggingface.co/datasets/princeton-nlp/SWE-bench_Verified>: 500 instances; x86_64 host with 120 GB free disk. Web, not machine-checked |

**How to build it**

- The task harness is the program `bench/cmd/lobebench`, with the actions `run` and `report` (this entry), `repeat`
  (M-04), `enforce` (M-05) and `graphdiff` (M-07). It is built from a checkout with
  `go build ./bench/cmd/lobebench`. The release builds only `./cmd/tentaqles`, so the harness, its tasks and its
  results are in no release archive. Small fixtures in `bench/testdata` are embedded in it and chosen with
  `--fixture`.
- The four checks that need no agent, no task and no network ship in the binary as actions of
  `tentaqles lobe bench`: `leak` (M-03), `latency` and `stall` (M-06) and `check-claims` (M-08). Their code is in
  `internal/lobe/benchcheck/` (`leak`, `latency`, `stall`, `claims`). Those are lobe packages: they keep the
  import rules of core page 5.1 and reach the product only by starting the program's own file as a child.
  F-07's `internal/lobe/cli` holds the verb. Nothing under `cmd/` or `internal/` imports a package under `bench/`.
- Every action of either program works in a home it creates under `--out`, sets up its clients there with
  `tentaqles lobe sandbox`, the command behind the standard tester setup in F-07 (a shipped check calls the same
  builder as a function; `lobebench` runs the command), and refuses an `--out` inside the real `TQ_HOME` or
  inside a client's folder (M-08 names the one exception, the checkout's own `bench/results/`). No action runs a
  person-only command. A shipped check runs the program's own file as the hook; `lobebench` runs the `tentaqles`
  that is first on `PATH`.
- A run with `--agent stub` or `--fixture` writes `"publishable":false` and the reason in `why`, so a scripted
  result can never back a claim (M-08).
- `bench/internal/arm` builds each arm's home with `tentaqles lobe sandbox` and writes the arm's `lobe` settings
  into the sandbox manifest as a fixture, as F-07's `lobeset` helper does; it never runs `tentaqles lobe set` or
  another person-only command. For an arm that uses the graph it also runs `tentaqles lobe graph build` in the
  clone and times it (step 3). `bench/internal/host` has one adapter per host plus `stub`, which replays scripted
  runs with no network. The first adapter, and the only one phase 0 builds, is Claude Code's; the Codex adapter
  is added when its headless usage fields are confirmed (step 8). `bench/internal/grade` holds the graders and
  `bench/internal/stat` the bootstrap, McNemar's test and percentiles, with no outside dependency.
- Cache write is priced at 1.25 times input and cache read at 0.1 (Trail-code `src/context/price.ts` L50, L51).
  Trail-code prices input only (L17); we price output too. Both multipliers are fields of `bench/prices.json`,
  not constants in code.

**How we know it works**

- `TestLobeBenchCostIsBilledCost`: at fixture prices of 2 (input) and 10 (output) dollars per million, a run of 1000
  input, 400 cache write, 10000 cache read and 200 output costs 0.007; one of 800, 400, 6000 and 200 costs 0.0058.
- `TestLobeBenchCountsFailedRuns`: the fixture (2 tasks, arms `off` and `all` with those two usages, 3 trials, one
  `off` run timing out) totals 0.042 and 0.0348 dollars, the timed-out run included.
- `TestLobeBenchMcNemarExact`: 6 against 0 gives 0.03125; 8 against 2 gives 0.109375.
- `TestLobeBenchPairedBootstrap`: two identical arms give the interval 0 to 0; the same seed gives the same interval.
- `TestLobeBenchRefusesUsedOutputFolder`, `TestLobeBenchHarnessIsInTheRepo` and
  `TestLobeBenchStubIsNeverPublishable`: yes or no.
- `TestLobeBenchArmsUseTheSettingsKeys`: each arm's manifest holds exactly the values of step 2's table, and
  `tentaqles lobe settings` reports none of them as invalid (yes or no).
- `TestLobeBenchShipsOnlyTheFourChecks`: `tentaqles lobe bench` offers exactly `leak`, `latency`, `stall` and
  `check-claims`, and the packages `./cmd/tentaqles` depends on include none under `bench/` (yes or no).
- `TestLobeBenchGraphBuiltBeforeTheRun`: in arms `graph` and `all` every run line has a `graph_build_ms`, the graph
  exists before the agent's first turn, and adding 1,000 ms to a build changes no total of cost, tokens or wall
  time (yes or no). Arms `off`, `identity` and `lessons` have no such field.

**Tester brief**

Start from the standard tester setup in F-07. No network. This brief needs one file more than that setup: the
harness program `lobebench`, built from the same commit with `go build ./bench/cmd/lobebench` and put on `PATH`
beside `tentaqles`. It is not in a release.

1. `lobebench run --agent stub --fixture two-tasks --arms off,all --trials 3 --out "$W/r1"`. Expect exit 0 and 12
   lines in `$W/r1/runs.jsonl`.
2. `$W/r1/summary.json`: arm `off` has `passed` 5, `failed` 1 and `usd_total` 0.042; arm `all` has `passed` 6,
   `failed` 0 and `usd_total` 0.0348; `usd_ratio_of_totals` is 0.829; `publishable` is `false` and `why` holds
   `stub-agent`.
3. Exactly one line of `runs.jsonl` has `"outcome":"fail"`, with `"fail_reason":"timeout"` and `usd` 0.007.
4. Run the command of check 1 again. Expect exit 1, a message naming `$W/r1`, and no file changed.
5. Run it with `--trials 2 --out "$W/r2"`. Expect exit 0, and in `$W/r2/summary.json` a `why` that holds both
   `stub-agent` and `trials-under-3`.
6. `tentaqles lobe bench run --out "$W/r3"; echo "exit=$?"` prints a line that is not `exit=0`, and `$W/r3` does
   not exist: the released program has no `run` action.

**Risks and open questions**

- Cost, as an estimate. Graft's cold arm cost at least 1.94 dollars per SWE-bench instance: its README gives 52.34
  dollars over at most 27 instances, at its model's prices, and the division is ours (source: web, not
  machine-checked). 150 instances on 2 arms with 3 trials are 900 runs: about 1,700 dollars or more, and each
  further arm adds about 870. Promises 2 and 3 together need 3 arms, about 2,600 dollars. These figures are
  estimates and unchecked. The owner has decided the order (D-15): the question and pull request sets cost far
  less and run first, and the SWE-bench Verified run waits for one approval from the owner at phase 4, asked with
  a checked cost in hand. Until then no paid run of that set starts. No phase waits for that spending: it holds
  back only the published number (step 8).
- OpenAI stopped reporting SWE-bench Verified after finding flawed tasks (web, not machine-checked), and models may
  have seen the public pull requests. We publish differences between arms, never a score.
- The warm store can flatter `lessons`: warm-up tasks are committed first and a cold-store result is published too.
- A tester needs a second program, `lobebench`, beside the released one, so the briefs of M-02, M-04, M-05 and
  M-07 cannot be run from a release archive alone. That is the price of keeping the harness out of the release.
- About 155 tasks are needed to see a 10-point paired difference with 80% power when 20% of tasks disagree
  (Connor's approximation; web, not machine-checked).
- Numbers are from Claude Code alone until the Codex adapter exists (step 8), so a claim says nothing about Codex
  until then. Each published number names its host for that reason.

## M-03 Isolation leak suite

**Phase** 1 · **Origin** new · **Depends on** F-01, F-02, F-04, F-07, S-07, R-03, R-04, C-07, G-10

**What it is.** A set of deliberate attacks. Each one tries to make a lesson, an event, a graph node or a login of
one client show up in another client's session. A canary is a made-up word that exists in only one client's data, so
finding it anywhere else is proof of a leak.

**Why it matters.** It holds promise 1 and the first anti-goal. Every phase ends only when this suite reports zero
leaks (core page section 8).

**Mechanism**

1. Each attack starts in a fresh `TQ_HOME` with clients `acme` and `globex`, built as `tentaqles lobe sandbox`
   builds them (F-07). The canaries are `acmecanaryword` and
   `globexcanaryword`: plain letters, so scrubbing (S-05) and masking (S-08) leave them alone. Each is planted in an
   event, a private lesson, a symbol name in the client's repository, and the login file of a fake agent CLI.
2. Control first: the suite proves acme's canary can be recalled inside acme, or "nothing leaked" could mean
   "nothing works". A failed control fails the attack. The attack runs as globex, then with the clients swapped.
3. The scan covers the output of every command, every file under `identities/<other>/`, the other client's folder,
   a neutral folder and the temp folder (`TMP` and `TMPDIR` point inside the working folder). It scans bytes and
   also calls every read verb, because a cache may compress text.
4. One hit is a leak. The pass mark is zero. Attacks that need a later phase are listed as not run, never as passed.
5. L-04 and L-05 test the rule of core page 5.1, which F-02 owns: a lobe hook acts only when the folder in the
   hook's JSON resolves to a client and the session's login folder (`CLAUDE_CONFIG_DIR`, or `CODEX_HOME` for
   Codex) is that same client's identity folder. When the two differ, or the login variable is unset, the hook
   exits 0, prints nothing and writes nothing in any store. A session with the variable unset therefore gets
   nothing from the lobe. Between them the two attacks cover three forms, each run for both hosts: another
   client's login, the reverse through inherited variables, and no login variable at all.
6. L-09 tests the folder resolver, which is identity code the lobe calls and does not change
   (`internal/resolve`). It is an ordinary attack: the suite runs it in every phase, a hit is a leak counted in
   `leaks`, and the exit code follows it like any other. The resolver is to refuse a second folder with a
   client's name; that fix is outside the lobe and lands before phase 1 (D-12, core page section 8).

| # | Attack | Expected |
|---|---|---|
| L-01 | The same failing command and error in both clients, so the fingerprint id (S-08) is equal. Acme has a lesson for it | Globex's hooks print nothing of acme's |
| L-02 | The same repository cloned in both clients. Acme has a private lesson with `applies: repo` and a graph | Globex gets no lesson and no symbol that exists only in acme's working tree |
| L-03 | One `session_id` used by a session in each client | Two `sessions/` files; no shown-once state or recall crosses |
| L-04 | A session logged in as globex moves into acme's folder: the hook JSON's `cwd` is acme's while `CLAUDE_CONFIG_DIR` (for Codex, `CODEX_HOME`) is globex's. Then the same with the login variable unset | Exit 0, no output, nothing written in either store, in both forms. Acme's lessons never enter a session under another client's login or under no login (step 5) |
| L-05 | The reverse through inherited variables: `cwd` is globex's while `TQ_WS`, `TQ_WS_ROOT` and `CLAUDE_CONFIG_DIR` are acme's | The same: no output, no write. `TQ_WS` and `TQ_WS_ROOT` decide nothing for the lobe (step 5) |
| L-06 | `session_id` and `tool_name` hold `../../acme/lobe/lessons/x`, and the same with backslashes | No file is created or read outside `identities/globex/lobe/` |
| L-07 | Globex's `lobe/lessons`, or `.tentaqles/lessons` in its repository, is a symbolic link (a junction on Windows) to acme's private `lessons` | Exit 0, no output: a path that resolves outside the client's own folders is refused |
| L-08 | Acme's marker file is copied into a neutral folder and into a subfolder of globex | Neutral folder: no output, no write. Subfolder: still globex |
| L-09 | A second folder named `acme` under another registered base, with a copied marker. The hook JSON's `cwd` is that folder and the login is acme's, as it would be for a session started there if the resolver takes the folder for acme | No output and no write there. A write or any output in that folder is a leak, counted in `leaks` like any other (step 6) |
| L-10 | A Stop in acme starts the detached work; hooks then run in globex. A fake agent CLI records its environment | The child wrote only under acme and saw acme's `CLAUDE_CONFIG_DIR` and `TQ_LOBE_NO_HOOKS=1` |
| L-11 | Backfill (C-07) runs in acme while globex's canary sits in globex's Claude config and in `$HOME/.claude/projects` | Acme's store holds no globex canary: backfill reads only acme's own login folder, never `~/.claude` |
| L-12 | 200 hook calls per client, interleaved from 8 workers (proposed) | No canary in the wrong store; every log line parses |
| L-13 | In globex, inside an agent session, a `tentaqles lobe graph` command (R-03's agent tools) or another `tentaqles lobe` verb asks for acme with `--client acme` or a `..` path | Refused, as F-07 states; no acme data printed |
| L-14 | `tentaqles drop acme`, then a new client named `acme`. `drop` answers only to a person, so a run started inside an agent session lists this attack under `not_run` with `"needs":"person"` | No acme canary under `$TQ_HOME` or the temp folder; the new store is empty |
| L-15 | Inside an agent session in acme's folder, with `CLAUDE_CONFIG_DIR` (for Codex, `CODEX_HOME`) naming globex's identity folder, every `tentaqles lobe` command that reads or writes a store is run: the read verbs and R-03's agent tools. Then the same with the variable unset | Under globex's login: refused, as F-07 states, with exit 1, nothing of acme's store printed and nothing written in either store. With the variable unset the command works, in acme's store only |

**Source**

| Project | What we take | Where to read |
|---|---|---|
| tentaqles (new) | our design | The attacks and the canary scan are ours |
| tentaqles | pattern | `internal/resolve/resolve_test.go` L71: a copied marker or manifest lends a folder nothing (L-08 repeats it for the lobe); `internal/testutil/home.go` L18: `HomeRun` keeps a test away from the real `~/.tentaqles` |
| Trail-code (MIT) | not taken | [`src/notes/just-me.ts` L12](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/just-me.ts#L12): personal state is keyed by repository in one `~/.trail`, so two clients with one repository would share it. L-02 exists for this |

**How to build it**

- `internal/lobe/benchcheck/leak/attacks.go` holds the table (id, the features an attack needs, setup, attack
  and scan) and `canary.go` beside it the scanner. The package ships in
  the binary (M-02). `test/integration/lobeleak_test.go` runs every attack against the built program in CI on the
  three systems; `tentaqles lobe bench leak` runs the same table for a tester.
- `summary.json` has `leaks`, `controls_failed` and `not_run` (each entry with `needs`). The exit code follows
  `leaks` and `controls_failed`.
- `--plant <events|lesson|graph>` copies that part of acme's store into globex's by hand before the scan, inside
  the home the action created. It exists so a test can prove the suite turns red. No build of the product, tagged
  or not, keys a store by anything but the client.
- The test names join the "Must not trade off" list in `CLAUDE.md`.

**How we know it works**

- `TestLobeLeak_AllAttacksZeroLeaks`: leaks 0.
- `TestLobeLeak_ControlsRecallOwnCanary`: every attack's control found its own canary (yes or no).
- `TestLobeLeak_SuiteGoesRedOnAPlantedLeak`: each of the 3 plants reports at least 1 leak.
- `TestLobeLeak_NothingSkippedSilently`: every attack is run or listed with the feature it waits for.
- `TestLobeLeak_LoginMismatchDoesNothing`: L-04 and L-05 in their three forms, for both hosts: exit 0, 0 bytes of
  output and 0 files changed under `identities/` in 6 of 6 runs.
- `TestLobeLeak_CommandUnderAnotherLoginRefused`: L-15 for each host's variable: exit 1, no acme canary in the
  output and 0 files changed under `identities/`; with the variable unset the same commands exit 0 (yes or no).
- `TestLobeLeak_SecondFolderWithAClientsName`: L-09 runs, and a write or any output in the second folder is
  counted under `leaks` (yes or no).

**Tester brief**

Start from the standard tester setup in F-07. No network. `A` is `$W/clients/acme/app`, `G` is
`$W/clients/globex/app`, and `I` is `$TQ_HOME/identities`. In a JSON, `<A>` and `<G>` stand for those paths.

1. In `A`, with `CLAUDE_CONFIG_DIR="$I/acme/claude"`, pipe `{"session_id":"s1","hook_event_name":
   "PostToolUseFailure","cwd":"<A>","tool_name":"Bash","tool_input":{"command":"acmecanaryword --sync"},
   "error":"acmecanaryword: unknown option"}` into `tentaqles lobe-hook claude post-tool-use-failure`. Expect exit
   0, empty stdout, and exactly one line holding `acmecanaryword` in the files under `$I/acme/lobe/events/`.
2. Search every file under `$I/globex/` and `$W/clients/globex` for `acmecanaryword`. Expect no match.
3. Still in `A`, pipe that JSON with `"session_id":"s2"` and the command `acmecanaryword --again` to the same
   command with `CLAUDE_CONFIG_DIR="$I/globex/claude"`. Expect exit 0, empty stdout, still exactly one line holding
   `acmecanaryword` under `$I/acme/lobe/events/`, no line holding `--again` there, and no match under `$I/globex/`.
   The new session and command keep the duplicate filter (S-06) from hiding a write.
4. Still in `A`, with neither `CLAUDE_CONFIG_DIR` nor `CODEX_HOME` in the environment (the standard setup leaves
   both unset), pipe that JSON with `"session_id":"s3"` and the command `acmecanaryword --nologin` to the same
   command. Expect exit 0, empty stdout, no line holding `--nologin` in any file under `$I/`, and still exactly
   one line holding `acmecanaryword` under `$I/acme/lobe/events/`.
5. In `G`, with `CLAUDE_CONFIG_DIR="$I/globex/claude"`, pipe the JSON of check 1 with `"cwd":"<G>"`,
   `"session_id":"../../acme/lobe/sessions/evil"` and `globexcanaryword` in place of `acmecanaryword`. Expect exit
   0, no file named `evil*` under `$I/acme/`, and no match for `globexcanaryword` under `$I/acme/`.
6. In `A`, `CLAUDECODE=1 CLAUDE_CONFIG_DIR="$I/globex/claude" tentaqles lobe events; echo "exit=$?"` prints
   `exit=1` and no line holding `acmecanaryword`: a command under another client's login is refused (L-15). The
   same command with `CLAUDE_CONFIG_DIR` unset exits 0.
7. `tentaqles lobe bench leak --out "$W/leak"`. Expect exit 0, `"leaks":0` and `"controls_failed":0` in
   `$W/leak/summary.json`, and a `needs` entry for every attack under `not_run` (`L-14` among them, with
   `"needs":"person"`). `L-09` is not under `not_run`: it ran and counts in `leaks` like any other attack.
8. `tentaqles lobe bench leak --plant events --out "$W/leak2"`, which copies acme's event log into globex's store
   by hand before the scan. Expect exit 1 and `"leaks"` at least 1.

Person checks

- In a terminal of their own, the owner runs `tentaqles lobe bench leak --out <new folder>` and expects `L-14`
  to be absent from `not_run` and `"leaks":0`. L-14 needs `tentaqles drop`, which a tester never runs.

**Risks and open questions**

- The rule behind L-04, L-05 and L-15 trusts the login variable the host hands to its hooks and commands. It
  keeps one client's
  memory out of a session logged in as another; it is not a defense against a process that sets both the folder
  and the variable to acme's, which is simply an acme session.
- Until the resolver's fix lands, L-09 may show a hit and so a leak. The resolver keys a client by folder name
  (`internal/resolve/resolve.go` L125 to L137), and only `reach` refuses a second folder with the same name
  (`internal/cmd/reach_test.go` L214). The fix is identity work, outside the lobe, and lands before phase 1
  (D-12, core page section 8), so the phase 1 gate reads L-09 with the rest (step 6).
- A shared lesson committed to a repository reaches whoever clones it. That is the design (F-04), not a leak.

## M-04 Repeat-mistake benchmark

**Phase** 2 · **Origin** new · **Depends on** C-01, C-03, C-04, C-08, R-04, S-08, E-01, E-02, E-07, M-02

**What it is.** A trap is a rule of the environment that an agent breaks by default. The agent meets a trap in one
task, the lobe gets the chance to learn, and a new session meets the same kind of trap in a different task. The
number is the repeat rate: how often a later session falls into the trap, with the lobe and without it. The
headline is the rate at the second meeting of the same mistake. The rates at the third and fourth meeting are
reported beside it.

**Why it matters.** It is the direct test of promise 3. None of the sources measures it, so without this benchmark
"learned there and not made twice" would be an opinion, and nobody could say how often a learned mistake still
comes back.

**Mechanism**

1. Four families, 5 traps each (proposed). Every trap has four tasks that lead into it (A, B, C, E) and one
   look-alike task D where the avoided pattern is the right thing to do.
   - **Cross-shell syntax**: PowerShell text sent to Git Bash and the reverse. Traps covered by a starter rule (C-08)
     and traps not covered are reported as two groups: a starter rule is built in and enters the ladder at
     `advice` (E-01), so it can speak at the first meeting, while a learned lesson does not exist until after it
     and speaks from the second (D-9).
   - **SQL dialect rule**: a stub warehouse CLI in `bench/traps/bin/` rejects one pattern with a fixed error, for
     example the `MERGE ... USING (` form in the sample lesson of core page 5.4.
   - **Wrong argument count**: a fixture repository whose function takes an unusual number of arguments.
   - **Misused API**: a stub command-line API and a stub MCP tool that reject one argument form.
2. An episode runs sessions 1 to 4 on tasks A, B, C and E, each with a fresh context, and runs
   `tentaqles lobe consolidate` after each. That verb runs now what the `stop` hook starts (F-07): consolidation,
   then the ladder's moves (E-07). So before the next session begins, the lesson is filed and stands at its
   stage: a lesson paired from a repair enters at `advice` (E-01, D-9) and stays there unless its record moves
   it. Session 5 runs task D.
3. The trap keeps its own log of rejected calls. Hits are read from that log, never from the lobe's own records.
4. Arms from M-02: `identity` (no lobe), `lessons`, `all`, and `lessons-model-off` (`lessons` with `model: off`).
   There is one mode: nobody confirms anything, as a new user gets it, and the harness writes no ladder entry. A
   lesson paired from a repair enters the ladder at `advice` by the enter rule (E-01, D-9), so it is shown at
   the second meeting. Arm `all` and the graph's path for the argument-count family (G-04, E-05) join in
   phase 4. In arm `all` the harness builds the graph of the trap's repository before session 1 and reports
   the build's time apart from every session's cost (M-02, steps 3 and 5). Proposed size: 20 traps, 3 trials,
   4 arms, 240 episodes.
5. Numbers per family and arm. **Repeat rate**: episodes with a trap hit at the second meeting divided by all
   episodes. This is the headline: it is what promise 3's "not made twice" means in numbers. The same rate at the
   third and fourth meeting is still reported. Those two show whether a lesson was demoted (one false alarm at
   `advice` sends it back to `trial`, where it is silent, E-07) or faded (C-05). Also the first-hit rate, the
   harm rate (task D failed or its correct call was denied), advice shown on task D, and each session's billed
   tokens. Every arm here caps `enforce` at `advice`, so no call is denied and harm is a failed task D; denies
   are M-05's subject.
6. Intervals resample traps, not trials, because trials of one trap are not independent (M-02, step 6).

**Source**

| Project | What we take | Where to read |
|---|---|---|
| tentaqles (new) | our design | The episode, the trap's own log and the look-alike task are ours |
| Trail-code (MIT) | contrast | [`src/skills/offer-repair.ts` L143](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/offer-repair.ts#L143): a repeated repair only decides when to make an offer to the person; no repeat rate is measured |
| agentmemory (Apache-2.0) | contrast | [`eval/README.md` L109](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L109): its own list of what is still unmeasured includes coding tasks built from captures |
| Microsoft Learn (web) | trap material | https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_pipeline_chain_operators : `&&` exists only from PowerShell 7. Web, not machine-checked |

**How to build it**

- `bench/traps/<family>/<trap>/` holds `trap.yaml` (the rule, the error text, the five tasks), the stub and its log
  path. `bench/internal/trap` runs episodes; `lobebench repeat` is the entry point (M-02).
- Stubs are small Go programs built with the harness. They use no network and hold no real client data.
- An invented house rule is used on purpose: the model cannot know it in advance, so session 1 does fall into it.
- Edge cases: an episode where session 1 never hit the trap stays in the headline rate; a session that ends without
  finishing the task counts as a failed task and its trap hits still count.

**How we know it works**

- `TestLobeRepeatStubEpisode`: with the scripted agent and nothing confirmed, arm `identity` has repeat rate 1.0
  and arm `lessons` 0.0.
- `TestLobeRepeatReadsTheTrapLog`: deleting the lobe's event log changes no reported number (yes or no).
- `TestLobeRepeatCountsHarm`: a deny on task D's correct call is counted in the harm rate.
- `TestLobeRepeatHeadlineKeepsEveryEpisode`: the headline denominator equals traps times trials.
- Target for the real run (proposed): the 95% interval of the repeat-rate difference lies below 0 at the second
  meeting. The harm rate of `lessons` is not above that of `identity`. The real run needs a paid agent, so it
  gates the published sentence of promise 3 and not phase 2, which ends on the tests above and the tester brief.
  A real run that misses the target means the claim is not made, and the miss is recorded (M-02, step 8). The
  run is on Claude Code first, and its numbers name their host and model.

**Tester brief**

Start from the standard tester setup in F-07. No network. This brief also needs the harness program `lobebench`
on `PATH` (M-02's brief says how it is built). The scripted agent of fixture `sql-merge` runs the trapped command
and then the corrected one, unless the lobe's `pre-tool-use` hook printed context for that call; then it runs only
the corrected one. Arm `lessons-model-off` calls no agent CLI. Nothing is confirmed and the harness writes no
ladder entry: the lesson paired from session 1's repair enters at `advice` by the enter rule (E-01). No step of
this brief runs a person-only command. The harness works with its own client `acme` in a home under `--out`,
built with `tentaqles lobe sandbox` as the standard setup builds `$W/clients/acme`.

1. `lobebench repeat --agent stub --fixture sql-merge --arms identity,lessons-model-off --trials 1
   --out "$W/m04"`. Expect exit 0.
2. `$W/m04/summary.json`, arm `identity`: `hits_by_session` is `[1,1,1,1]` and `repeat_rate_2` is 1.0.
3. Arm `lessons-model-off`: `hits_by_session` is `[1,0,0,0]`, `repeat_rate_2` is 0.0 and `harm` is 0.
4. The stub's log `$W/m04/lessons-model-off/trial1/trap.log` has exactly 1 line; the one for `identity` has 4.
5. Under `$W/m04/lessons-model-off/trial1/home/` there is exactly one `.md` file directly in
   `identities/acme/lobe/lessons/`, and `identities/acme/lobe/ladder.json` holds `"stage":"advice"` for its id.

**Risks and open questions**

- A repair lesson speaks at the second meeting (D-9). What can silence it again is a false alarm: one at
  `advice` sends the lesson back to `trial` (E-07). A false alarm at the second or third meeting would show in
  the rates at the third and fourth. One on the look-alike task D, which runs last, would show in the advice
  shown on task D and in the harm rate. A lesson with no `avoid` block cannot earn a false alarm (E-02), so no
  hook demotes it: its note on task D stays until a person runs `tentaqles lobe rules demote` or `retire`.
- Invented rules show that the mechanism works, not how often real mistakes happen. M-02's pull request tasks cover
  real work.
- Consolidation with `model: on` calls the agent CLI, so the real run needs network and its cost is counted (M-02,
  step 5).
- Open: a longer gap, such as 20 unrelated tasks between two meetings, would test fading (C-05). Not in the first
  version.

## M-05 Enforcement accuracy

**Phase** 3 · **Origin** new · **Depends on** E-01, E-02, E-03, E-04, E-05, E-07, C-08, F-03

**What it is.** A set of tool calls, each labeled as a real mistake ("bad"), as correct work that looks
like one ("good"), or as a call that only quotes the mistake ("mention"). The set is replayed through the real
hook and two errors are counted. A wrong block is a good call that was denied. A miss is a bad call that got
neither a deny nor advice. Mentions are counted in a row of their own.

**Why it matters.** The third anti-goal is a wrongly learned rule that blocks correct work. Trail-code built a check
that could stop a commit and shipped it switched off because of false alarms. This entry is the gate that decides
whether our blocking may be on.

**Mechanism**

1. A case is one line of `bench/enforce/cases.jsonl`: `{"id":"sql-014","rule":"l-4c7d19e2","stage":"block",
   "host":"claude","label":"mention","why":"pattern inside a commit message","hook":{...}}`, where `hook` is the
   full PreToolUse JSON (step 7 names the one exception) and `label` is `bad`, `good` or `mention`.
2. Size (proposed): at least 300 good and 150 bad cases across the four families of M-04, and at least 60
   mentions, plus cases for the sure-fail block (E-04) and, from phase 4, the argument check (E-05).
3. Good cases are written to be hard. The rule must not touch them: the corrected form the lesson recommends; a
   near miss one token away from the avoided form; the same statement sent to another command or tool for which
   it is right; a folder outside `when.place`; a dry-run flag. The other-tool and outside-folder cases pass
   because a block rule denies only when the lesson's `when` block matches the call (when it has one) and its
   approved pattern matches the call text (E-01, core page 5.5).
4. A mention holds the avoided text without running it: inside a quoted string, a commit message, a comment, a
   file body passed to `Write`, a search pattern for `grep`. A block pattern is tested on the whole call text
   (E-01), so a mention in a call the lesson's `when` block matches also matches the pattern and, at stage
   `block`, is denied. That is the rule as designed, not a wrong block. A mention in a call the `when` block does
   not match is not denied, and the row says how many those were.
   Mentions are replayed and reported in their own row, are never counted as good cases, and are outside the
   gate. The guard for a pattern that is too wide is E-03's promotion test: a person who wants a given mention
   left alone writes it as a `must_not_match` line, and the step to `block` is then refused until the pattern no
   longer matches it.
5. Cases are held out: written before a rule's pattern is final and never used to tune it. A rule's own
   `must_match` and `must_not_match` lines (E-03) are replayed too, in their own row, because the rule was written
   to pass them. Labels may be drafted by agents: two labeling agents, neither of which wrote the rule under test
   or its pattern, each label every case. A person checks a random sample of 50 cases (proposed), drawn with a
   recorded seed, and every case where the two agents disagree; the person's label stands. The report says who
   labeled what: per case, each agent's label and whether a person checked it.
6. For each case the harness makes a fresh `TQ_HOME` with `tentaqles lobe sandbox`, sets `enforce` to `block` in
   the sandbox manifest as a fixture (as F-07's `lobeset` does), and installs the rule at the
   case's stage by writing the lesson file, `ladder.json` and `approvals.json` directly, as E-01's own tester
   brief does. The verb that normally writes them, `tentaqles lobe rules approve <id>`, answers only to a person
   at a console and needs a record of hits (E-01); a harness never pretends to be that person. Two sources need
   no such files: a starter rule (C-08) enters the ladder at `advice` by itself, and a sure-fail rule (E-04) is
   built in and needs no approval of its own. The harness then sets the host's login variable to the client's
   identity folder (core page 5.1), pipes `hook` into `tentaqles lobe-hook <host> pre-tool-use`, and classifies
   the result as deny, advice or nothing by the output forms of F-02.
7. Two sources are replayed differently. Sure-fail cases run twice: under `enforce: block`, where a bad case is
   denied, and under `enforce: advice`, where it gets a note and a deny is an error. An argument check case
   (E-05, from phase 4) carries the PostToolUse JSON of an edit and goes to `post-tool-use`, where that check
   runs; it only ever gives a note, so a deny from it is counted as a wrong block whatever the case's label.
8. Numbers per stage and per rule source (learned, starter, sure-fail, argument check): wrong-block rate (good cases
   denied over good cases), miss rate (bad cases with neither deny nor advice over bad cases), and advice noise
   (good cases that got advice over good cases). The mention row gives mentions denied over mentions at stage
   `block`, and mentions that got a note over mentions at stage `advice`.
9. Gate: 0 wrong blocks on the good cases. With 0 of 300 the true rate is below 1.0% at 95% confidence (the rule of
   three, 3 divided by n). Misses are reported and not gated: decision A-6 prefers a rule that fires rarely to one
   that fires wrongly. The mention row is reported and not gated. A run in which no bad case at stage `block` is
   denied is an error, so a hook that never denies cannot pass the gate.
10. A second measure comes from use: `trials.jsonl` (E-02) holds every fire at `trial`, `advice` and `block` with
    what the call then did. From the M-02 and M-04 runs in arms where `enforce` is not `off` it gives, per rule,
    how often it fired before a call that then failed, against a call that then succeeded.
11. Any wrong block found later, by anyone, becomes a permanent case.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| tentaqles (new) | our design | The labeled set, the hard good cases, the mention row and the zero gate are ours |
| Trail-code (MIT) | the warning | [`src/claude/pre-bash-check.ts` L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L42): the stop is off; the comment above it says the check flagged 4 word matches on one pull request and none was real |
| BASE (FSL-1.1) | idea only | [`src/domain/rule_test.rs` L314](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rule_test.rs#L314): its rule test feeds each stored example through the prompt path only, so matching on tool calls is never exercised. Ours replays the full tool-call JSON |
| Claude Code docs (web) | fact to respect | https://code.claude.com/docs/en/hooks.md : when several PreToolUse hooks disagree, deny wins. Web, not machine-checked |

**How to build it**

- `bench/enforce/cases.jsonl`, `bench/enforce/rules/*.md` (the lessons under test) and `bench/internal/enforce`.
  `lobebench enforce` (M-02) writes `summary.json` with the three rates per stage and source and the mention row,
  and exits 1 when the gate of step 9 fails.
- Classification never reads lobe logs. It reads only the hook's exit code, stdout and stderr.
- Both hosts are run. A case names its host because the JSON differs.
- The installed files have E-01's shapes: a `ladder.json` entry as in core page 5.4, and in `approvals.json` the
  rule's exact pattern and the words a deny prints. The words come from the lesson under test, so a deny can be
  recognized by them.
- Edge cases: a case whose rule fails its own E-03 tests is an error in the set, not a result; a hook that times out
  counts as nothing, which is a miss for a bad case; a mention whose call text was cut at E-01's length cap is
  not denied and is reported as such, not as a pass.

**How we know it works**

- `TestLobeEnforceZeroWrongBlocks`: wrong blocks 0 on the good cases of the labeled set.
- `TestLobeEnforceCountsAMiss`: a bad case the pattern does not match is counted as a miss, not dropped.
- `TestLobeEnforceTimeoutIsNothing`: a hook that never answers is classified as nothing.
- `TestLobeEnforceSetSize`: at least 300 good cases, 150 bad cases and 60 mentions (proposed sizes).
- `TestLobeEnforceOwnExamplesReportedApart`: E-03 lines never enter the headline row.
- `TestLobeEnforceMentionsReportedApart`: a denied mention is in the mention row, is not in `wrong_blocks` and does
  not change the exit code (yes or no).
- `TestLobeEnforceSureFailFollowsTheSetting`: one sure-fail bad case is `deny` under `enforce: block` and `advice`
  under `enforce: advice` (yes or no).
- `TestLobeEnforceInstalledRuleIsAnApprovedRule`: the files the harness writes load as the same approved rule the
  function behind `tentaqles lobe rules approve` produces for that lesson (yes or no). The test calls the
  function; no test runs the person-only verb.

**Tester brief**

Start from the standard tester setup in F-07. No network. This brief also needs the harness program `lobebench`
on `PATH` (M-02's brief says how it is built). Fixture `merge` has one rule, the `MERGE ... USING (` lesson of core
page 5.4 at stage `block`, installed for the harness's own client `acme` (built with `tentaqles lobe sandbox`, as
the standard setup builds `$W/clients/acme`) by writing `ladder.json` and `approvals.json` directly; no step of
this brief runs a
person-only command. It has 7 cases. 3 are bad: `b1` upper case, `b2` lower case across two lines, `b3` written
`USING(` with no space. 2 are good: `g1` `MERGE INTO t USING src AS s`, `g2` the same in lower case across two
lines. 2 are mentions: `m1` the text of `b1` inside `git commit -m "..."`, `m2` the same text as the search
pattern of a `grep` command.

1. `lobebench enforce --fixture merge --out "$W/m05"`. Expect exit 0.
2. `$W/m05/summary.json`, stage `block`: `good` 2, `wrong_blocks` 0.
3. Same block: `bad` 3, `blocked` 2, and `misses` names only `b3`: the pattern needs white space before the
   parenthesis.
4. Same block: `mentions` 2 and `mentions_denied` 2. Both are `Bash` calls, which the lesson's `when` block
   matches, and the pattern is tested on the whole command, so both quoted mentions are denied, and neither `m1`
   nor `m2` is named under `wrong_blocks`.
5. `$W/m05/cases/b1.json` holds the hook's exit code, stdout and stderr; `classified` is `deny`, and the words
   `source alias`, from the title in the fixture's approval record, appear in stdout or stderr.
6. `$W/m05/cases/g1.json`: `classified` is `advice` or `nothing`, not `deny`.
7. Run check 1 with `--fixture merge-mislabeled --out "$W/m05b"`, where `b1` is labeled `good`. Expect exit 1 and
   `wrong_blocks` 1 with `b1` named, which shows that one wrong block turns the gate red.

**Risks and open questions**

- Labels are drafted by agents that did not write the rule, and a person sees a sample of 50 and the cases the
  two agents disagree on (step 5). A wrong label both agents agree on, outside the sample, is not caught here;
  step 11 is the net for it.
- Zero wrong blocks in 300 cases leaves room for a rate under 1%. Real use decides, through E-07 and step 11.
- The field measure of step 10 cannot see a mistake no rule fired on.
- A rule with a correct pattern still denies a call that only quotes the mistake, such as a commit message that
  explains it, wherever the lesson's `when` block matches that call. The mention row shows how often. If that
  rate is high in real use, an agent cannot write about a
  blocked statement in a shell command without the person's `tentaqles lobe rules allow-once <id>`; E-03's
  `must_not_match` lines push a pattern off the mentions a person names, and off no others.
- The line between a good case and a mention is a judgment. A case moved to the mention row to turn the
  gate green would hide a wrong block, so the label of every case is fixed before the rule's pattern is final
  (step 5) and a change of label is listed in the report.
- Open: whether a rule may reach `block` in a release before the labeled set has its full proposed size.

## M-06 Hook latency budget

**Phase** 1 · **Origin** new · **Depends on** F-02, F-03, S-03, S-07, R-02, R-04, E-01

**What it is.** A measurement of how long each lobe hook takes, from the moment the host starts the process to the
moment it exits, on Windows, macOS and Linux. The 95th percentile is the time that 95 of 100 runs stay under. Stall
tests then break the store on purpose and check that the hook still returns in time.

**Why it matters.** The second anti-goal is a hook that stalls or slows a session, and principle 4 says hooks never
stall. The budgets of core page 5.2 are proposals until this entry has measured them.

**Mechanism**

1. `tentaqles lobe bench latency` starts the program again as a child for each host and event, with a fixed JSON on
   stdin, and times the whole process. Process start is included because the person waits for it. Claude Code
   starts the hook through `bash` and `lobe_hook.sh` (F-03), so with `--wrapper <path>` the same runs go through
   that script and both times are reported. The gates apply to the path the host really runs.
2. The runner sets the host's login variable (`CLAUDE_CONFIG_DIR`, or `CODEX_HOME` for Codex) to the client's
   identity folder. Without it a lobe hook does nothing (core page 5.1), and the run would time an empty process.
   That do-nothing path is timed on purpose too, with the variable unset, and reported as `floor`: process start
   and nothing else. Every other number sits on top of it.
3. Runs: 20 warm-up runs, then 1,000 measured runs per event (proposed). Store sizes (proposed): empty; typical
   (1,000 events, 50 lessons, a 5,000-file graph); large (100,000 events, 500 lessons, a 50,000-file graph). The
   measurement is repeated at the end of every phase, because each phase adds work to a hook.
4. The two post-tool events record first and may print afterwards, so each is measured both ways (core page 5.2).
   **Append**: the payload gives the hook nothing to print (a plain success; a failure that no lesson and no
   starter rule knows), so the run is the capture path alone and its whole time is held to the capture budget.
   That bounds the append from above, because the time also holds process start and exit. **Whole**: the payload
   gives the hook something to print (a failure whose fingerprint a lesson lists or a starter rule covers, an open
   rule fire to judge, and from phase 4 an edit of a function with callers in the graph). The run is held to the
   inject target and to the 1.5 second cutoff, and the runner then checks that the event line is in the log.
   Where a phase gives an event nothing to print yet, its whole row repeats the append row and says so.
5. Reported per event, system and store size: the 50th, 95th and 99th percentile and the maximum, by nearest rank.
   Every number in the table is proposed, the ones from the core page included.

   | Events | Class | Gate from core page 5.2 | Extra target (proposed) |
   |---|---|---|---|
   | `stop`, `session-end` | capture | 95th percentile at most 150 ms; no run past the 1 second cutoff | 50th at most 50 ms; 99th at most 500 ms |
   | `post-tool-use`, `post-tool-use-failure`, append | capture | 95th percentile at most 150 ms; no run past the 1.5 second cutoff | 50th at most 50 ms; 99th at most 500 ms |
   | `post-tool-use`, `post-tool-use-failure`, whole | capture, then inject | No run past the 1.5 second cutoff; the event line is in the log | 95th at most 300 ms, the inject target applied to the whole process; 99th at most 1 second |
   | `session-start`, `user-prompt-submit`, `pre-tool-use` | inject | 95th percentile at most 300 ms; no run past the 1.5 second cutoff | 99th at most 1 second |

   `stop` is timed until the hook exits. The detached pass it starts (consolidation, the ladder's moves, the graph
   build) is not hook time and has no budget here. `session-end` only sweeps.
6. A run is past its cutoff when the process is still alive 500 ms after it (proposed: F-02's watchdog fires 250 ms
   after the cutoff, and the process then has to exit).
7. What logging every success costs (core page 5.4, S-03). The append row of `post-tool-use` is run with three
   payloads: a plain success, stored as a projection of its input; the success that follows a failure in the same
   session, stored with its scrubbed input in full; and a success with a 100 KB input (proposed). For each, the
   report gives the percentiles and the bytes the call added to the log. It also gives the log's size after 1,000
   calls of which 50 fail (a proposed mix), because successes are most of the log.
8. Published numbers come from a quiet machine. CI checks two things that load cannot break: time grows no faster
   than the store (the method of `testutil.CheckLatency`), and no run passes its cutoff.
9. Stall tests, named as `--case` takes them; the sizes are proposed. In each the hook must exit 0 with empty
   stdout by its cutoff, and each runs with the login variable set as in step 2, so the hook reaches the store
   before it meets the fault. `held-lock`: another process holds the store lock (F-01) and never lets go.
   `blocked-file`: the day's event log is a named pipe nobody reads (Linux, macOS) or is held open with no sharing
   (Windows). `huge-payload`: 50 MB of JSON on stdin. `open-stdin`: stdin that never closes. `full-disk`: a 1 MB
   size-limited mount, on Linux CI only. `write-error`: a plain file put where the `events` folder should be, so
   every write fails. `crowd`: 50 capture hooks started at once on one store. `blocked-read` (from phase 2): on
   `post-tool-use-failure`, the lesson that lists the failure's fingerprint cannot be read (a named pipe nobody
   writes, or a file held open with no sharing); the event line must still be in the log, which shows that the
   append comes first. Spinning code is a Go test only: a child of the test binary loops forever in the handler
   and the watchdog must end it.
10. A test reads the lobe's registered timeout from the hook entries (F-03) and fails unless every cutoff plus
    slack is below it, as the guard's deadline test does. The registered timeout is 5 seconds, or the host's own
    cap where that is lower: Codex is reported to cap its session-end hooks at 3 seconds (phase 0 confirms), and
    the largest cutoff plus slack here is 2 seconds. `pre-tool-use` is also timed together with the guard's hook
    on a shell command, since both run on that call.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| tentaqles | pattern | `internal/testutil/latency.go` L47 and L38: `CheckLatency` tests growth and a ceiling; the comment records CI runners at four times a budget, hence `LoadFactor` 20 |
| tentaqles | pattern | `internal/cmd/claudehook_main_test.go` L25 and `internal/cmd/claudehook_watchdog_test.go` L78: deadlines kept under the host's timeout; a spinning decision ended on time |
| Trail-code (MIT) | idea | [`test/claude-hooks-perf.test.ts` L135](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/test/claude-hooks-perf.test.ts#L135): each hook under 300 ms at the 95th percentile on a 200 MB transcript. It times the handler in-process, not the process |
| agentmemory (Apache-2.0) | not taken | [`src/hooks/post-tool-failure.ts` L13](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L13): the failure hook may live 3,500 ms, which is over our whole budget |
| Claude Code docs (web) | fact to respect | https://code.claude.com/docs/en/hooks.md : a command hook's default timeout is 600 seconds, and a timed-out PreToolUse hook lets the call run. Web, not machine-checked |

**How to build it**

- `internal/lobe/benchcheck/latency` (runner, nearest-rank percentiles, store builders) and
  `internal/lobe/benchcheck/stall` (one function per case). Both ship in the binary (M-02). Go tests in
  `internal/lobe/hook/stall_test.go` run the stall cases in CI.
- `summary.json` has one entry per event and `mode`. `mode` is `whole` for every event; the two post-tool events
  have a second entry with `mode` `append`. An entry holds `runs`, `p50_ms`, `p95_ms`, `p99_ms`, `max_ms` and, for
  the post-tool events, `bytes_per_event`. `floor` holds the same four times for the do-nothing path of step 2.
- The spinning case follows the guard's child-process pattern, with the opposite result: exit 0 and no output.
- Differs from Trail-code: whole-process time, three systems, and a 99th percentile.

**How we know it works**

- The quiet-machine gate, run by hand with `tentaqles lobe bench latency` before a release and not in CI: on the
  typical store, `stop`, `session-end` and the append rows of the two post-tool events have a 95th percentile of
  at most 150 ms, and the three inject events one of at most 300 ms. Yes or no per system.
- `TestLobeLatency_PostToolMeasuredBothWays`: each post-tool event has an `append` and a `whole` entry, and a
  `whole` run whose event line is missing from the log fails the run (yes or no).
- `TestLobeLatency_FloorDoesNothing`: with the login variable unset the hook writes 0 bytes under `identities/`
  and its time is reported as `floor`.
- `TestLobeLatency_SuccessCostReported`: the three payloads of step 7 each report `bytes_per_event`, and the
  payload after a failure reports more bytes than the plain one (yes or no).
- `TestLobeLatency_GrowsNoFasterThanTheStore`: 8 times the events costs at most 24 times the time (`MaxGrowth`).
- `TestLobeStall_HungStoreFailsOpen`, `TestLobeStall_HugePayload`, `TestLobeStall_OpenStdin`,
  `TestLobeStall_WriteErrorFailsOpen`, `TestLobeStall_FullDisk` (Linux), `TestLobeStall_SpinningHandler`: each exits
  0 with no output by its cutoff plus 500 ms.
- `TestLobeStall_BlockedReadKeepsTheEvent` (from phase 2): exit 0, no output, and 1 new event line (yes or no).
- `TestLobeHookCutoffsUnderRegisteredTimeout`: every cutoff plus slack is under 5 seconds, and under 3 seconds
  for Codex's session-end entry (yes or no).
- `TestLobeStall_GuardStillDenies`: with the lobe's store hung, the guard still denies a wrong-identity `git push`.

**Tester brief**

Start from the standard tester setup in F-07. No network. Use a machine with no other heavy work running.

The actions build their own client `acme` in a home under `--out`, with the builder behind
`tentaqles lobe sandbox` as the standard setup builds `$W/clients/acme`, and set its login variable themselves.

1. `tentaqles lobe bench latency --host claude --runs 200 --store typical --out "$W/lat"`. Expect exit 0 and, in
   `$W/lat/summary.json`, nine entries: one with `"mode":"whole"` for each of the seven events, and one with
   `"mode":"append"` for `post-tool-use` and for `post-tool-use-failure`. Each has `runs` 200 and `p50_ms`,
   `p95_ms`, `p99_ms` and `max_ms`. There is also a `floor` object with the same four times.
2. There, `stop` and `session-end` have `p95_ms` at most 150 and `max_ms` at most 1500. The two `append` entries
   have `p95_ms` at most 150, `max_ms` at most 2000 and a `bytes_per_event` above 0.
3. There, the three inject events (`session-start`, `user-prompt-submit`, `pre-tool-use`) and the two `whole`
   entries of the post-tool events have `max_ms` at most 2000. The three inject events have `p95_ms` at most 300.
4. `tentaqles lobe bench stall --case open-stdin --event post-tool-use --out "$W/s1"`. Expect one line of JSON on
   stdout with `"case":"open-stdin"`, `"exit":0`, `"stdout_bytes":0`, `"pass":true` and an `ms` value at most 2000.
5. `tentaqles lobe bench stall --case huge-payload --event post-tool-use --out "$W/s2"`. Expect `exit` 0,
   `stdout_bytes` 0, `ms` at most 2000 and `log_growth_bytes` under 16384 (a proposed bound).
6. `tentaqles lobe bench stall --case held-lock --event pre-tool-use --out "$W/s3"`, then the same with
   `--case write-error` and `--out "$W/s4"`. Expect `exit` 0, `stdout_bytes` 0 and `pass` true for both, with `ms`
   at most 2000.
7. `tentaqles lobe bench stall --case open-stdin --event stop --out "$W/s5"`. Expect `exit` 0, `stdout_bytes` 0,
   `pass` true and `ms` at most 1500: `stop` keeps the 1 second cutoff.

**Risks and open questions**

- Process start on Windows, with a virus scanner, may alone use much of 150 ms, and Claude Code adds a `bash`
  start for the wrapper before it. Phase 1 measures the `floor` of step 2 first, with and without the wrapper. If
  the budget cannot hold, the owner changes it in the core page; it is not relaxed here.
- Failing open means a hung store switches blocking off for that call. That follows principle 2 and belongs in the
  docs.
- The append is bounded from outside, by timing a call with nothing to print, not read from a mark inside the
  hook. A hook that printed before it appended would still pass the append row; the `whole` row's check that the
  event line exists and the `blocked-read` stall case are what catch that order.
- The 300 ms inject target is new and unmeasured. On a large graph, `user-prompt-submit` (R-02) and the blast
  radius after an edit (G-09) are the likeliest to miss it; the first phase 4 run says whether it holds.
- If logging every success breaks the capture budget (step 7), S-03 names the fallback of logging a success only
  when the session has an earlier failure or a rule on trial. Choosing it is a change to the core page.

## M-07 Graph accuracy against Trail-code

**Phase** 4 · **Origin** new · **Depends on** G-02, G-03, G-04, G-12

**What it is.** We build our graph and Trail-code's graph on the same repositories at the same commits and compare
them node by node and edge by edge. Every difference gets a written verdict: our bug, their bug, or a deliberate
difference.

**Why it matters.** Promise 2 depends on the graph pointing at the right code. The graph features are ports of
Trail-code, so a port that drifts from its source without anyone noticing is the likely failure.

**Mechanism**

1. Repositories (proposed): one small fixture per language that the first release ships and Trail-code also
   parses, plus the three public repositories of M-02 at the same commits. G-01 lists 25 grammars: Trail-code's 24
   and `sql`. `sql` is ours alone and is not compared here, and a language G-01's spike drops has no fixture.
2. Trail-code at the commit in core page section 7 builds its structural graph in a container that holds only the
   public repository, with the network off, no language-server pass and no model. Its output, the file
   `wiring.json` in its `.graph` folder, is committed as `bench/graphdiff/theirs/<repo>@<commit>.json`, so the
   comparison reruns without Trail-code.
3. Both graphs first pass the soundness check (G-12).
4. Keys: a node is (path, kind, name, start line); an edge is (source key, relation, target key or outside name).
   `bench/graphdiff/map.yaml` maps kinds and relations that are named differently. Our six relations are the
   ones G-02 names, with `extends` and `implements` apart from `references`. Trail-code writes all three as
   `references`, so the map lets one of their `references` edges match an edge of ours with the same ends whose
   relation is `extends`, `implements` or `references`.
5. The diff gives three lists: only theirs, only ours, and same key with a different span or confidence.
6. Each difference needs a verdict in `bench/graphdiff/verdicts.yaml`: `ours-bug` (with an issue; it blocks the
   phase), `theirs-bug` (with the Trail-code file and line that explain it), or `deliberate` (with the entry that
   decided it). One verdict may cover a class by pattern.
7. Numbers per repository and relation: matched over theirs, matched over ours, the count per verdict, and the
   unexplained count. Gates: unexplained 0, and `ours-bug` 0 at the end of phase 4.
8. Agreement is not correctness. 200 edges (proposed) are drawn at random from the union with a recorded seed
   and labeled right or wrong against the source code. The labels may be drafted by agents: two labeling agents,
   neither of which wrote the graph code under test, each label every edge. A person checks a random sample of
   50 of them (proposed) and every edge where the two agents disagree; the person's label stands. The report
   gives each graph's precision on the 200 edges and says who labeled what.
9. Known before the first run, from reading Trail-code and not from running it. Kind `trait` is declared and
   emitted for PHP (`src/graph/extract.ts` L254) but is missing from the allowed set: `theirs-bug`. The header of
   `scripts/graph-quality.mjs` (L13) lists "no self-loop call edges" as a rule while the code only counts them,
   so their graph may hold a `calls` edge from a definition to itself. Ours never does: a recursive call
   produces no `calls` edge, because G-02 drops a call on a definition's own name, and G-12 expects 0 such
   edges. A self-loop `calls` edge that is only theirs: `deliberate` (G-02). The values `lsp_resolved` and
   `lsp_dispatch` never appear in ours (core page section 11): `deliberate`.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| tentaqles (new) | our design | The diff, the verdict file and the labeled sample are ours |
| Trail-code (MIT) | the reference graph | [`src/claude/stats.ts` L9](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/stats.ts#L9): where `wiring.json` is read from; [`src/graph/types.ts` L40](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/types.ts#L40): the four confidence values |
| Trail-code (MIT) | their bug | [`src/graph/types.ts` L24](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/types.ts#L24) declares kind `trait`; the allowed set at [`src/graph/invariants.ts` L26](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/invariants.ts#L26) has 11 kinds without it |
| Trail-code (MIT) | port, through G-12 | [`scripts/graph-quality.mjs` L19](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/scripts/graph-quality.mjs#L19): the allowed kinds, relations and confidence values |
| Graft 0.17.0 (MIT) | caution | `README.md` L113 says the structural graph never calls a model; L169 says both build passes use one. We compare only the structural file |

**How to build it**

- `bench/internal/graphdiff`: `load.go` (reads a `wiring.json`), `ours.go` (builds our graph in-process through
  `internal/lobe/graph`), `diff.go`, `verdict.go`. `lobebench graphdiff` is the entry point (M-02).
- A verdict pattern matches on side, kind, relation, language and path glob. A pattern that matches nothing is an
  error, so stale verdicts are removed.
- Edge cases: a file one side skips (generated, vendored, too large) is one difference per file, not one per node;
  line endings are normalized before spans are compared; node order never matters.
- Trail-code is run only on public code. No client repository is ever given to it.

**How we know it works**

- `TestLobeGraphDiff_FixturesFullyExplained`: unexplained 0 on the real graph of every fixture repository.
- `TestLobeGraphDiff_FindsAPlantedDifference`: one invented node in their file gives exactly 1 difference.
- `TestLobeGraphDiff_StaleVerdictFails`: a verdict that matches nothing exits non-zero.
- `TestLobeGraphDiff_OrderIndependent`: shuffled input gives the same report.
- Targets for the real run (proposed): at least 99% of their nodes and 95% of their `calls` edges with confidence
  `extracted` matched before verdicts.

**Tester brief**

Start from the standard tester setup in F-07. No network. This brief also needs the harness program `lobebench`
on `PATH` (M-02's brief says how it is built). Four built-in fixtures share a two-file Python repository: `py2`
(Trail-code's real graph of it), `py2-ghost` (that graph plus one invented node `ghost.py#ghost`),
`py2-ghost-explained` (the same with a verdict for that node) and `py2-stale` (the real graph with that verdict).
The harness builds our graph in-process on a copy of that repository under `--out`; it reads and writes no
client's store, so `$W/clients/acme` stays as the setup left it.

1. `lobebench graphdiff --fixture py2 --out "$W/g1"`. Expect exit 0, and in `$W/g1/summary.json`
   `"unexplained":0` and `matched_over_theirs` 1.0 for nodes.
2. The same with `--fixture py2-ghost --out "$W/g2"`. Expect exit 1, `"unexplained":1`, and one line in
   `$W/g2/diff.jsonl` with `"side":"theirs"` and `"node":"ghost.py#ghost"`.
3. The same with `--fixture py2-ghost-explained --out "$W/g3"`. Expect exit 0, `"unexplained":0` and
   `"theirs-bug":1`.
4. The same with `--fixture py2-stale --out "$W/g4"`. Expect a non-zero exit and a message that a verdict matches
   nothing.

**Risks and open questions**

- Trail-code is a reference, not the truth. Step 8 is the only check on real correctness, 200 edges is small, and
  a person sees 50 of them plus the ones the labeling agents disagree on.
- A different parser runtime (G-01) can yield different trees for the same grammar. Those differences appear here
  first and may be many.
- Building their graph needs Node, a network install and a pinned commit. The committed `wiring.json` files keep
  reruns offline, but they go stale if the repositories are re-pinned.
- Graphify's parts of the graph (SQL in G-05, documents in G-06) have no counterpart in Trail-code and are not
  covered here.

## M-08 Reporting rules

**Phase** 0 · **Origin** new · **Depends on** M-02

**What it is.** The rules every number about tentaqles follows before it appears in the README, the docs, the
changelog or a release note, and a check that enforces most of them by machine. A receipt is the results file a
published number was read from.

**Why it matters.** Principle 9 and decision D-7. Graft, agentmemory and Graphify each show a number that is mixed,
stale or impossible to rerun from their own repository. These rules keep us from doing the same.

**Mechanism**

1. **The harness and the raw results are published.** Each run's folder (M-02, step 7) is committed under
   `bench/results/`: the manifest, one line per run, the summary and the report. Its `transcripts/` folder is
   large and is not committed; it stays under `bench/results/` on the machine that ran it, and git ignores it.
   The manifest holds the tentaqles commit, a dirty-tree flag, the harness version, the host and its version, the
   exact model id, the date, the system, the seeds, the task-set hash, each arm's settings and the SHA-256 of
   every transcript, so a copy handed over later can be checked against the run.
2. **The mean with its interval.** Each figure is a mean or a ratio of totals with a 95% interval and the count it
   rests on. No best case, no single task, no "up to", "as much as" or "best case".
3. **Host, model and date.** Each table and each sentence with a figure names the host, the model id and the run
   date. Numbers come from Claude Code first (M-02, step 8).
4. **One table, one test.** All rows of a table come from one run folder. Two tests need two tables.
5. **Failures count.** Failed and timed-out runs are in the totals. Invalid runs are rerun. The counts of all three
   are printed (M-02).
6. **The baseline is named**: what the other arm was, in the sentence.
7. **Estimates are not results.** A figure from `tentaqles lobe status --savings` (M-01) is never quoted as a
   benchmark result.
8. **Our own repository is reported apart**, and no claim rests on it alone.
9. **Decided before the run.** Tasks, metrics and the gate are committed before the first measured run, and the
   manifest names that commit. A change after seeing results starts a new run. Results that go against us are
   published like the others.
10. **Receipts.** A published figure carries a marker, `<!-- bench:<run-id>/summary.json#<key> -->`. The action
    `check-claims` reads the README, `CHANGELOG.md`, the plugin's README and `docs/`, leaving out `docs/prd/`
    and `docs/features/`, which quote example figures. It fails
    on a banned phrase, on a figure near a claim word (tokens, cost, cheaper, faster, fewer, errors, saved) with no
    marker, on a figure that differs from its receipt after rounding, on a missing interval, model or date, on a
    receipt marked not publishable, and on a table whose markers name two runs. A figure is a number written with
    `%`, a multiplier (`4x`) or a currency, and near means in the same sentence or table row (proposed). A marker's
    path is read under `<root>/bench/results/`.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Graft 0.17.0 (MIT) | what to avoid | `README.md` L23 and L30: an "up to" headline over a table that joins two tests |
| agentmemory (Apache-2.0) | idea | [`eval/README.md` L107](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/eval/README.md#L107): a public claim needs the same data, budget and failure accounting on every system, receipts for each case and paired uncertainty. [`test/website-meta.test.ts` L86](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/test/website-meta.test.ts#L86): a test holds published figures equal to the receipt files |
| agentmemory (Apache-2.0) | what to avoid | [`plugin/opencode/README.md` L8](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/plugin/opencode/README.md#L8): still shows 95.2%, while the current evaluation page gives 96.2% for one measure and 84.7% for the strict one. We found no test that covers this file |
| Graphify (Apache-2.0) | what to avoid | [`worked/karpathy-repos/review.md` L27](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/worked/karpathy-repos/review.md#L27): 71.5x on the full corpus; L19 of the same file gives 8.8x for code only. The headline depends on the baseline |
| Graphify (Apache-2.0) | what to avoid | [`graphify/benchmark.py` L77](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/benchmark.py#L77): with no measured corpus size, 50 words per node is assumed |
| Graphify (Apache-2.0) | what to avoid | [`BENCHMARKS.md` L198](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/BENCHMARKS.md#L198): the command given to reproduce a published row runs `memory/runner.py`, which is not in the repository |

**How to build it**

- `internal/lobe/benchcheck/claims`: `scan.go` (finds figures and markers in Markdown), `rules.go` (the checks of
  step 10), `receipt.go` (reads `summary.json`). It ships in the binary (M-02), so anyone with a release can check
  a document against its receipts. `tentaqles lobe bench check-claims --root <dir>` is the entry point.
- `test/integration/lobeclaims_test.go` runs it on the repository, so CI fails on a claim with no receipt.
- `lobebench report` (M-02) writes `report.md` with the markers already in place. Copying from the report is the
  normal way to publish.
- `bench/results/.gitignore` holds the one line `*/transcripts/`. A test fails if a file under a `transcripts/`
  folder is tracked, or if a committed manifest names a transcript without its SHA-256.
- Rounding: a published figure must equal the receipt's value rounded to the digits shown. Signs are ignored
  (proposed): the sentence's verb carries the direction, and a person checks it.
- Edge cases: figures inside code blocks are skipped; material quoted from other projects is skipped only between
  `<!-- bench:quote -->` and `<!-- bench:end -->`; a receipt whose folder is missing is an error.
- Differs from agentmemory: its test covers the website's JSON only. Ours covers every user-facing document.

**How we know it works**

- `TestLobeClaims_BannedPhraseFails`: "up to 4x cheaper" exits 1 with rule `banned-phrase`.
- `TestLobeClaims_MismatchFails`: text says 27%, the receipt says 17.1; exit 1 with rule `number-mismatch`.
- `TestLobeClaims_MixedTableFails`: a table with markers from two runs exits 1 with rule `mixed-table`.
- `TestLobeClaims_RepositoryIsClean`: the repository's own documents pass (yes or no).
- `TestLobeClaims_NotPublishableFails`: a receipt with fewer than 3 trials cannot back a claim.
- `TestLobeBenchTranscriptsStayOutOfGit`: 0 tracked files under any `bench/results/*/transcripts/`, and every
  transcript a committed manifest names has a SHA-256 beside it.

**Tester brief**

Start from the standard tester setup in F-07. No network. Create `$W/c/bench/results/r1/summary.json` with
`{"publishable":true,"model":"m-1","date":"2026-11-20","usd_change_pct":{"mean":-17.1,"lo":-25.0,"hi":-9.2,"n":45}}`.
After each change below run `tentaqles lobe bench check-claims --root "$W/c"`.

1. Create `$W/c/README.md` with the line `The lobe is up to 4x cheaper.` Expect exit 1 and output naming
   `README.md:1` and `banned-phrase`.
2. Replace the line with `Cost fell 17% (95% interval 9% to 25%, 45 tasks) against arm identity, Claude Code,
   model m-1, 2026-11-20. <!-- bench:r1/summary.json#usd_change_pct -->`, all on one line. Expect exit 0.
3. Change `17%` to `27%`. Expect exit 1 and `number-mismatch`.
4. Restore `17%` and delete `(95% interval 9% to 25%, 45 tasks)`. Expect exit 1 and `no-interval`.
5. Restore check 2's line and delete the marker. Expect exit 1 and `no-receipt`.
6. Restore the marker and set `"publishable":false` in `summary.json`. Expect exit 1 and `not-publishable`.

**Risks and open questions**

- The scan is a heuristic. It will miss a claim worded in a new way, so a person still reads every release note.
- Transcripts are not in git. A reader of the repository gets every run's line and the summary, but cannot redo
  the transcript audit (did the tool run in one arm and not in the other) without asking for a copy. The
  manifest's hashes prove that a copy is the one the run produced; they do not make it available, and a
  transcript lost with the machine that ran it cannot be shown at all.
- Results hold public repository content and model output only. No client content may ever enter `bench/results/`;
  the harness refuses an `--out` folder that lies inside a client's folder. The one exception is `bench/results/`
  of the checkout it was built from, since this repository may itself sit in a client's folder.
- Text outside the repository (a website, a post) is beyond the check and is covered by the rule only.
