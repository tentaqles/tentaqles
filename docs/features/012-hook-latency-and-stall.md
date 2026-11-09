# 012: Hook speed and stall checks

**Status** not started · **Stage** Sense · **Entries** M-06, M-02 (where the shipped checks keep their code) ·
**Needs** 011 · **Feeds** 013, and every later feature that adds a hook path

## Result

Two commands in the released program measure the lobe's hooks from outside, as whole processes. `latency`
prints how long each hook that exists takes against its limit. `stall` breaks the store or stdin on purpose and
shows that the hook still exits 0, silent, by its cutoff. Both build their own throwaway home under `--out`.

```bash
tentaqles lobe bench latency --host claude --runs 200 --store typical --out "$W/lat"
cat "$W/lat/summary.json"       # one entry per event and mode, plus "floor"
tentaqles lobe bench stall --case open-stdin --event post-tool-use --out "$W/s1"
# {"case":"open-stdin","event":"post-tool-use","exit":0,"stdout_bytes":0,"ms":...,"pass":true}
```

## Scope

**In**

- The verb `tentaqles lobe bench` with its first two actions, `latency` and `stall` (M-06).
- Where the shipped checks keep their code (M-02, "How to build it", second and third bullets): the packages
  `internal/lobe/benchcheck/latency` and `internal/lobe/benchcheck/stall`, the home they build under `--out`,
  and the rule that they reach the product only by starting the program's own file as a child.
- A latency row for every lobe hook that exists after features 003 to 011, and the `floor`.
- The cost of logging every success (M-06 step 7).
- The stall cases `held-lock`, `blocked-file`, `huge-payload`, `open-stdin`, `full-disk`, `write-error` and
  `crowd`, and the spinning handler as a Go test.
- The test that every lobe cutoff sits under its registered timeout, and the test that the guard still denies
  while the lobe's store hangs.

**Out**

- `tentaqles lobe bench leak` (feature [013](013-isolation-leak-suite.md)) and `check-claims` (feature 021).
- The task harness `lobebench`, the `bench/` folder, arms, tasks, statistics and reports: the rest of M-02
  (features 021 and 059).
- Rows and cases for hook paths that do not exist yet. Each arrives with the feature that builds the path:

  | Feature | Row or case it adds (read from the index; that feature's own file decides) |
  |---|---|
  | 014 | `pre-tool-use` for Claude Code, also timed together with the guard's hook on a shell command; `pre-tool-use` as a stall event |
  | 015 | A real `whole` payload: a failure a starter rule answers, on `post-tool-use-failure` and as a hidden failure on `post-tool-use` |
  | 016, 017 | 50 lessons in the typical store and 500 in the large one; `pre-tool-use` with a matching lesson |
  | 018 | `stop`, timed until the hook exits, and `stop` as a stall event with the 1 second cutoff |
  | 020 | `whole` with a failure whose fingerprint a lesson lists; the stall case `blocked-read` and `TestLobeStall_BlockedReadKeepsTheEvent` |
  | 023 | `whole` with an open rule fire to judge |
  | 026 | `session-start` |
  | 027 | `user-prompt-submit` |
  | 036 | The Codex inject events |
  | 038 to 040 | The `pre-tool-use` paths that deny, or warn before a commit |
  | 043, 048 to 051 | The graph in the store sizes; `post-tool-use` after an edit; `user-prompt-submit` with code pointers |

- Changing a budget. The numbers of core page 5.2 are proposed. If a measurement shows one cannot hold, the
  owner changes it in the core page; it is not relaxed here.
- A timing mark inside the hook (F-02, last risk). None is built unless this feature's result asks for it.

## What it builds on

Task 1 confirms each line against the code, because the earlier feature files decide the details.

- 002: `sandboxBuild(folder string) error`, handed over as a function value; the `lobe` command group.
- 003: `internal/lobe/hook` (`Main`, the cutoffs of 1 s and 1.5 s, the watchdog at cutoff plus 250 ms, the
  table of steps per event), `internal/lobe/store` (`Open`, `Lock`, the `lock` file), the wrapper
  `plugin/scripts/lobe_hook.sh`, the lobe entries in `plugin/hooks/hooks.json` with timeout 5, the event log's
  append, `stats/hook-errors.jsonl`, and the two import tests.
- 004 to 009: the capture path on `post-tool-use-failure` and `post-tool-use`: any tool, fingerprints, the
  duplicate filter, `ok` lines with a projection of the input, the full input after a failure, hidden failures.
- 005: `internal/lobe/cli` with its `Deps`, where this verb is added.
- 010: Codex's post-tool event and `lobeEvents` in `internal/codexconfig`.
- 011: the `session-end` sweep.
- Existing tentaqles code: `internal/testutil/latency.go` (`CheckLatency`, `ScaleDown`, `MaxGrowth`); the
  child-process pattern of `internal/cmd/claudehook_watchdog_test.go`; the deadline test
  `TestHookDeadlinesUnderHarnessTimeouts` in `internal/cmd/claudehook_main_test.go`.

**The hooks measured now.** By the index, after feature 011 these lobe hooks exist: for Claude Code
`post-tool-use-failure`, `post-tool-use` and `session-end`; for Codex `post-tool-use`, and `session-end` if
feature 011 registered it for Codex. `pre-tool-use`, `stop`, `session-start` and `user-prompt-submit` have no
lobe work yet and get no row. None of the existing hooks prints anything yet.

## What it leaves for later features

| Name | What it is | Used by |
|---|---|---|
| `tentaqles lobe bench` | The verb, in `internal/lobe/cli`, with `latency` and `stall` | 013 adds `leak`; 021 adds `check-claims` |
| `internal/lobe/benchcheck` | The parent folder of the shipped checks, and the helper that builds a home under `--out` | 013 (`leak`), 021 (`claims`) |
| The row table of `latency` | One row per host, event and mode, with its payload and its class | Every feature in the table under Scope adds its row |
| The case table of `stall` | One function per case, and the events a case may run on | 014, 018 and 020 add events and the case `blocked-read` |
| `summary.json` of `latency` | The shape under "How it works" step 8 | The release gate; later features read their own rows |
| `TestLobeLatency_EveryRegisteredHookHasARow` | Fails when a registered lobe hook has no row | Makes "each feature adds its row" a rule kept by code |
| `TestLobeBenchShipsOnlyTheFourChecks` | In its first form: two actions | 013 and 021 add theirs, which makes four |

## How it works

1. **Where the code lives (M-02).** `internal/lobe/benchcheck/latency` holds the runner, the nearest-rank
   percentiles and the store builders. `internal/lobe/benchcheck/stall` holds one function per case. Both are
   lobe packages: they keep the four import rules of core page 5.1, have no `init` function, and never call
   the hook's code in-process. `internal/lobe/cli` holds the verb; `internal/cmd/lobe.go` registers it and
   passes the sandbox builder in `cli.Deps`. Nothing under `cmd/` or `internal/` imports a package under
   `bench/`, which does not exist yet.
2. **The home.** Each action works in a home it creates under `--out`. It refuses an `--out` that exists and
   is not empty, that lies inside the real `TQ_HOME`, or that lies inside a client's folder; nothing is
   created on a refusal. It then points its own process's `TQ_HOME`, `HOME` and `USERPROFILE` into `--out`,
   clears the variables the standard tester setup clears, calls the sandbox builder, which applies its own
   refusals again, and makes the empty repository `app` in each client as the standard setup does. So the
   action also works from a person's ordinary terminal. It runs no person-only command.
3. **The child.** A run starts the program's own file (`os.Executable()`) as `lobe-hook <host> <event>`, with
   a fixed JSON on stdin whose `cwd` is `<out>/clients/acme/app`, and times it from start to exit. Process
   start is counted, because the person waits for it. The runner sets the host's login variable to acme's
   identity folder (`CLAUDE_CONFIG_DIR`, or `CODEX_HOME` for Codex); without it the hook does nothing (core
   page 5.1). With `--wrapper <path>` the same runs also go through `bash <path> <event>`, the way Claude
   Code starts the hook, and both times are reported. The gates apply to the path the host really runs.
4. **The rows now.** Every number is proposed (M-06 step 5).

   | Host | Event | Mode | Payload | Gate from core page 5.2 |
   |---|---|---|---|---|
   | claude | `post-tool-use-failure` | `append` | A failed `Bash` call with a fresh command each run | 95th percentile at most 150 ms; no run past the 1.5 s cutoff |
   | claude | `post-tool-use-failure` | `whole` | The same; nothing prints yet, so the row repeats `append` and says so | No run past the 1.5 s cutoff; the event line is in the log |
   | claude | `post-tool-use` | `append` | A plain success (and the two other payloads of step 7) | As the first row |
   | claude | `post-tool-use` | `whole` | Repeats `append` and says so | As the second row |
   | claude | `session-end` | `whole` | A `SessionEnd` JSON; the sweep finds nothing to delete, then 3 expired files | 95th percentile at most 150 ms; no run past the 1 s cutoff |
   | codex | `post-tool-use` | `append`, `whole` | A failed and a successful shell call in Codex's shape | As the Claude Code rows |
   | codex | `session-end` | `whole` | Only if 011 registered it | As above, and under Codex's 3 s cap |

   Each run uses a new session id and a new command, so the duplicate filter (S-06) never turns a measured
   write into a reference line.
5. **The floor.** The same child with the login variable unset: process start and nothing else. It is
   reported as `floor`, and the runner checks that the run wrote 0 bytes under `identities/`. Measure it
   first, with and without the wrapper, because on Windows it may alone use much of 150 ms.
6. **Runs and stores.** 20 warm-up runs, then 1,000 measured runs per row (proposed; `--runs` changes it).
   `--store` is `empty`, `typical` (1,000 events) or `large` (100,000 events), sizes proposed. Lessons and the
   graph join the store sizes with their features. The store builders write event lines with the event log's
   own append, spread over day files inside `retention_days`, not through thousands of hook runs.
7. **What logging every success costs (M-06 step 7).** The `append` row of `post-tool-use` runs with three
   payloads: a plain success; the success that follows a failure in the same session, stored with its
   scrubbed input in full; and a success with a 100 KB input (proposed). For each the report gives the
   percentiles and the bytes the call added to the log. It also gives the log's size after 1,000 calls of
   which 50 fail (a proposed mix).
8. **The report.** Percentiles are by nearest rank. `<out>/summary.json` is compact JSON on one line:
   `{"host":"claude","store":"typical","entries":[{"event":"post-tool-use","mode":"append","runs":200,`
   `"p50_ms":0,"p95_ms":0,"p99_ms":0,"max_ms":0,"bytes_per_event":0}],"floor":{"p50_ms":0,"p95_ms":0,`
   `"p99_ms":0,"max_ms":0},"success_cost":{"plain":{},"after_fail":{},"large":{},"log_bytes_after_1000":0}}`.
   M-06 fixes the keys inside an entry and inside `floor`. The names `entries`, `success_cost` and its four
   keys, `"repeats":"append"` on a `whole` entry that repeats its `append` row, and a `wrapper` object with
   the same `entries` and `floor` when `--wrapper` is given, are proposed here. A `whole` run whose event line
   is missing from the log fails the action. The action prints a short table and exits 0 when every run
   finished and no run passed its cutoff, else 1.
9. **Past the cutoff.** A run is past its cutoff when the process is still alive 500 ms after it (proposed:
   the watchdog fires 250 ms after the cutoff, and the process then has to exit). So `max_ms` may reach 2000
   for the 1.5 s events and 1500 for `session-end`.
10. **Stall.** `tentaqles lobe bench stall --case <case> --event <event> --out <dir>` runs one case on one
    Claude Code event that has a row, with the login variable set, so the hook reaches the store before it
    meets the fault. It prints one line of JSON: `case`, `event`, `exit`, `stdout_bytes`, `ms`, `pass`, and
    `log_growth_bytes` for `huge-payload`. `pass` is true when the hook exited 0 with empty stdout by its
    cutoff plus 500 ms. An event with no row yet is refused with exit 1 and a line naming it, never a pass.
    The cases (sizes proposed, M-06 step 9):
    - `held-lock`: another process holds the store lock and never lets go.
    - `blocked-file`: the day's event log is a named pipe nobody reads (Linux, macOS) or is held open with no
      sharing (Windows).
    - `huge-payload`: 50 MB of JSON on stdin; the log may grow by less than 16384 bytes (proposed bound).
    - `open-stdin`: stdin that never closes.
    - `full-disk`: a 1 MB size-limited mount, on Linux CI only. Elsewhere the action says the case is not
      available on this system and exits 1.
    - `write-error`: a plain file put where the `events` folder should be, so every write fails.
    - `crowd`: 50 capture hooks started at once on one store; every one must pass.
    Spinning code is a Go test only: a child of the test binary loops forever in the handler and the watchdog
    must end it with exit 0 and no output, the opposite result of the guard's same test.
11. **CI and the quiet machine.** Published numbers and the release gate come from a quiet machine, run by
    hand. CI checks only what load cannot break: time grows no faster than the store (the method of
    `testutil.CheckLatency`: 8 times the events costs at most 24 times the time), and no run passes its cutoff.
12. **Registered timeouts.** A test reads the lobe's registered timeout from `plugin/hooks/hooks.json` and
    from the Codex handler list, and fails unless every cutoff plus slack is below it: 5 seconds, and 3
    seconds for Codex's session-end handler. The largest cutoff plus slack here is 2 seconds.
13. **Growing.** The row table is data in the `latency` package. A test compares it with the lobe hooks
    that are registered and fails when one has no row. A feature that adds a hook path adds its row, its
    payload and, when the path can meet a new fault, its stall case, in its own change.

## Where to read

| What | Where |
|---|---|
| Requirements | [M-06](../prd/lobe-benchmarks.md), whole entry; [M-02](../prd/lobe-benchmarks.md), "What it is" (last sentence) and "How to build it", bullets 2 and 3, and `TestLobeBenchShipsOnlyTheFourChecks`; the page's opening section on the code's two homes. [Core page](../prd/vertical-lobe.md) 5.1 (import rules, the client rule) and 5.2 (time budgets). [F-02](../prd/lobe-foundation.md) steps 1, 2 and 7; [F-03](../prd/lobe-foundation.md) steps 2 to 5; [F-07](../prd/lobe-foundation.md) step 7 and the rows `lobe bench latency` and `lobe bench stall`; [S-03](../prd/lobe-sense.md) and [S-07](../prd/lobe-sense.md) for what an append writes |
| Source to port | Idea only, we time the whole process: Trail-code [`test/claude-hooks-perf.test.ts` L135](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/test/claude-hooks-perf.test.ts#L135). Not taken: agentmemory [`src/hooks/post-tool-failure.ts` L13](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L13). Fact to respect (web, not machine-checked): https://code.claude.com/docs/en/hooks.md |
| Existing tentaqles code | `internal/testutil/latency.go` L47 (`CheckLatency`) and L38 (`LoadFactor`, why CI cannot hold a wall-clock budget); `internal/cmd/claudehook_main_test.go` L25 (deadlines under the host's timeout); `internal/cmd/claudehook_watchdog_test.go` L78 (a spinning decision ended on time, by re-running the test binary); `plugin/hooks/hooks.json`; `plugin/scripts/lobe_hook.sh` and `internal/lobe/hook` from feature 003 |

## Build tasks

1. **Check this brief against the code as it stands.** List the lobe hooks really registered in
   `plugin/hooks/hooks.json` and in `internal/codexconfig`, the names feature 003 gave the hook's cutoffs and
   step table, how a lobe test gets its fixture clients, and whether the import test lets a lobe test file
   use `internal/testutil`. Correct the row table of step 4. The owner approves changes to this file.
2. **Measure the floor first.** With a throwaway script, time `tentaqles lobe-hook claude post-tool-use` with
   the login variable unset, 200 runs, direct and through `lobe_hook.sh`, on Windows and one other system.
   Write the four percentiles under "Risks and unknowns" below. If the floor alone is over 150 ms at the
   95th percentile, stop and tell the owner before building the rest.
3. **The home helper and the verb.** `internal/lobe/benchcheck/home.go` (the `--out` rules, the environment,
   the builder call); `internal/lobe/cli/bench.go`; `internal/cmd/lobe.go`. Proof:
   `TestLobeBench_RefusesABadOut`, `TestLobeBenchShipsOnlyTheFourChecks`.
4. **Latency.** `internal/lobe/benchcheck/latency/`: `rows.go`, `run.go`, `percentile.go`, `stores.go`,
   `report.go`. Proof: the four `TestLobeLatency_*` tests of M-06 and
   `TestLobeLatency_EveryRegisteredHookHasARow`. Can run beside task 5 once task 3 is in.
5. **Stall.** `internal/lobe/benchcheck/stall/` with one file per case, and
   `internal/lobe/hook/stall_test.go`, which runs the cases in CI against a child of the test binary. Proof:
   the `TestLobeStall_*` tests below.
6. **The two tests outside the lobe packages.** `internal/cmd/lobe_bench_test.go`:
   `TestLobeHookCutoffsUnderRegisteredTimeout` and `TestLobeStall_GuardStillDenies`. They sit in
   `internal/cmd` because they read `internal/codexconfig` and run the guard, which no lobe package may
   import. No guard source file changes.
7. **Docs.** A `## tentaqles lobe bench` section in `docs/reference/commands.md` with both actions, their
   flags, the cases and the note that a hung store switches the lobe off for that call (M-06, second risk);
   `CHANGELOG.md` under `[Unreleased]`; `internal/lobe/benchcheck/` in the layout list of `CLAUDE.md`, and the
   stall tests on the "never stalls" line for lobe hooks if feature 003 added one (ask the owner if not). The
   plugin does not change.
8. **Tester run**, then the owner's quiet-machine run on each system.

## Tests that stay in the repository

Names are M-06's or M-02's unless marked.

- `TestLobeLatency_PostToolMeasuredBothWays`: each post-tool event has an `append` and a `whole` entry, and a
  `whole` run whose event line is missing from the log fails the run.
- `TestLobeLatency_FloorDoesNothing`: with the login variable unset the hook writes 0 bytes under
  `identities/` and its time is reported as `floor`.
- `TestLobeLatency_SuccessCostReported`: the three payloads each report `bytes_per_event`, and the payload
  after a failure reports more bytes than the plain one.
- `TestLobeLatency_GrowsNoFasterThanTheStore`: 8 times the events costs at most 24 times the time.
- `TestLobeLatency_EveryRegisteredHookHasARow` (proposed here): 0 registered lobe hooks without a row, and 0
  rows for an event that is not registered.
- `TestLobeStall_HungStoreFailsOpen` (the cases `held-lock` and `blocked-file`, by this file's reading),
  `TestLobeStall_HugePayload`, `TestLobeStall_OpenStdin`, `TestLobeStall_WriteErrorFailsOpen`,
  `TestLobeStall_FullDisk` (Linux), `TestLobeStall_SpinningHandler`, and `TestLobeStall_Crowd` (proposed
  here; M-06 names the case and no test): each exits 0 with no output by its cutoff plus 500 ms.
- `TestLobeHookCutoffsUnderRegisteredTimeout`: every cutoff plus slack is under 5 seconds, and under 3
  seconds for Codex's session-end entry when it exists.
- `TestLobeStall_GuardStillDenies`: with the lobe's store lock held, `tentaqles claude-hook pre-tool-use`
  still denies a wrong-identity `git push`.
- `TestLobeBenchShipsOnlyTheFourChecks`, first form: `tentaqles lobe bench` offers exactly `latency` and
  `stall`, none of `run`, `report`, `repeat`, `enforce` or `graphdiff`, and the packages `./cmd/tentaqles`
  depends on include none under `bench/`. Features 013 and 021 add `leak` and `check-claims`.
- `TestLobeBench_RefusesABadOut` (proposed here): an `--out` inside `TQ_HOME`, inside a client's folder, or
  not empty is refused with 0 bytes written. 3 of 3.
- House rules kept green: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeHookAlwaysExitsZero`, `TestHookDeadlinesUnderHarnessTimeouts`, the two `TestHookStall_*` tests
  of the guard, `TestDocsReferenceEveryCommand`, `TestLobeCommandTable` (two new rows).

## Evaluation

A tester agent runs this in an empty working folder with the built `tentaqles` first on `PATH`, on a machine
with no other heavy work running. Bash (Git Bash on Windows). No network. The tester is also handed the
repository's `plugin` folder, read only; `PLUGIN` is its path. Start with the standard tester setup:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
```

No line may start with `SETUP FAIL`. The actions build their own client `acme` in a home under `--out` and
set its login variable themselves, so nothing more is exported. `S` is `$W/lat/summary.json`. A count of a
pattern in `S` is `grep -o '<pattern>' "$S" | wc -l`, because the file is one line.

1. `tentaqles lobe bench latency --host claude --runs 200 --store typical --out "$W/lat"; echo "exit=$?"`
   prints `exit=0`. In `S` the count of `"mode":"whole"` is 3 and of `"mode":"append"` is 2: `whole` for
   `post-tool-use`, `post-tool-use-failure` and `session-end`, `append` for the two post-tool events. Each
   entry has `"runs":200` and `p50_ms`, `p95_ms`, `p99_ms` and `max_ms`. There is a `floor` object with the
   same four times.
2. In `S`, `session-end` has `p95_ms` at most 150 and `max_ms` at most 1500. The two `append` entries have
   `p95_ms` at most 150, `max_ms` at most 2000 and a `bytes_per_event` above 0.
3. In `S`, the two `whole` entries of the post-tool events have `max_ms` at most 2000 and hold
   `"repeats":"append"`.
4. In `S`, `success_cost` has `plain`, `after_fail` and `large`, each with a `bytes_per_event`; the one of
   `after_fail` is larger than the one of `plain`; `log_bytes_after_1000` is above 0.
5. `tentaqles lobe bench latency --host codex --runs 50 --store empty --out "$W/latc"; echo "exit=$?"` prints
   `exit=0`, and `$W/latc/summary.json` has a `whole` and an `append` entry for `post-tool-use`.
6. `tentaqles lobe bench latency --host claude --runs 50 --store empty --wrapper "$PLUGIN/scripts/lobe_hook.sh"`
   `--out "$W/latw"; echo "exit=$?"` prints `exit=0`, and `$W/latw/summary.json` has a `wrapper` object with 5
   entries and a `floor`.
7. `tentaqles lobe bench stall --case open-stdin --event post-tool-use --out "$W/s1"` prints one line of JSON
   with `"case":"open-stdin"`, `"exit":0`, `"stdout_bytes":0`, `"pass":true` and an `ms` value at most 2000.
8. `tentaqles lobe bench stall --case huge-payload --event post-tool-use --out "$W/s2"` prints `exit` 0,
   `stdout_bytes` 0, `ms` at most 2000 and `log_growth_bytes` under 16384.
9. `tentaqles lobe bench stall --case held-lock --event post-tool-use-failure --out "$W/s3"`, then the same
   with `--case write-error` and `--out "$W/s4"`, then with `--case crowd` and `--out "$W/s5"`. Each prints
   `exit` 0, `stdout_bytes` 0 and `pass` true, with `ms` at most 2000.
10. `tentaqles lobe bench stall --case open-stdin --event session-end --out "$W/s6"` prints `exit` 0,
    `stdout_bytes` 0, `pass` true and `ms` at most 1500: `session-end` keeps the 1 second cutoff.
11. `tentaqles lobe bench stall --case open-stdin --event pre-tool-use --out "$W/s7"; echo "exit=$?"` prints
    `exit=1`, a line that names `pre-tool-use`, and no `"pass":true`: that hook has no lobe work yet.
12. `tentaqles lobe bench latency --host claude --out "$W/clients/acme/x"; echo "exit=$?"` prints `exit=1` and
    `test -e "$W/clients/acme/x"` is false. The same with `--out "$TQ_HOME/x"` prints `exit=1`, and with
    `--out "$W/lat"` (not empty) prints `exit=1`.
13. `tentaqles lobe bench --help | grep -c -E '^  (latency|stall) '` prints `2`, and the same command with
    `(run|report|repeat|enforce|graphdiff)` prints `0`.
14. The tester's own home was not used: `ls "$TQ_HOME/identities/acme/lobe/events" 2>/dev/null | wc -l`
    prints `0`. Every measured run wrote under its `--out`.

**Person checks**

- The quiet-machine gate, before a release, on Windows, macOS and Linux: the owner runs check 1 with
  `--runs 1000` and with `--wrapper`, on the typical store, and reads the same limits as checks 2 and 3 on the
  path Claude Code really runs. Yes or no per system. A miss is reported to the owner; no limit is relaxed.

PASS needs 14 of 14. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI on the three systems (`go vet ./...`,
  `go test ./... -count=1`, `go build ./...`); `TestLobeStall_FullDisk` runs on Linux.
- [ ] The floor measurement of task 2 is written in this file.
- [ ] The tester brief passes, 14 of 14, and the owner's quiet-machine run is recorded per system.
- [ ] House rules: the guard's tests pass unedited and no guard file changed; the import tests are green; a
  stalled lobe hook exits 0 with no output in every case.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md` and `CLAUDE.md` are updated.
- [ ] The owner has seen a `summary.json` and one stall line as under Result.

## Risks and unknowns

- **Which hooks exist.** The list under "What it builds on" is read from the index, not from built code. In
  particular: whether feature 003 registered all seven Claude Code events or only the ones with work, and
  whether feature 011 registered a Codex session-end handler. Task 1 settles both. Check 1's counts (3 and 2)
  change if a hook was registered early.
- **Every budget is proposed.** 150 ms, 300 ms, 1 s, 1.5 s, the 250 ms watchdog slack, the 500 ms exit slack,
  the run counts, the store sizes and the 16384 byte bound are all proposals until this feature has measured.
- **Process start on Windows.** With a virus scanner, starting the process may alone use much of 150 ms, and
  Claude Code adds a `bash` start for the wrapper. That is why task 2 comes before the build. The fallback
  F-03 names is the direct command `tentaqles lobe-hook claude <event> || exit 0`; taking it is the owner's
  decision and a change to the plugin.
- **Logging every success.** If step 7 shows the capture budget breaks, S-03 names the fallback of logging a
  success only when the session has an earlier failure or a rule on trial. Choosing it changes the core page.
- **The append is bounded from outside.** A hook that printed before it appended would still pass the
  `append` row. Nothing prints yet, so this cannot be seen now; the `whole` row's check that the event line
  exists, and from feature 020 the case `blocked-read`, are what catch that order.
- **Failing open has a cost.** A hung store switches the lobe off for that call, and later that includes
  blocking. That follows principle 2 and belongs in the docs (task 7).
- **JSON names proposed here.** `entries`, `success_cost`, `repeats` and `wrapper` are not in M-06. If the
  owner prefers other names, only the report and the tester checks change.
- **Web facts.** Claude Code's default hook timeout of 600 seconds and "a timed-out PreToolUse hook lets the
  call run" come from its docs page and were not machine-checked. Codex's 3 second cap on session-end hooks
  is reported, and the phase 0 capture of F-03 confirms it.
- **What would force a change to this file:** a different set of registered hooks; a floor over the capture
  budget; a different name for the cutoffs or the step table in `internal/lobe/hook`.
