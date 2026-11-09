# 018: A failure and its fix become a repair pair

**Status** not started · **Stage** Lessons · **Entries** C-01 (all but the lesson-aware branch of step 5),
F-02 (step 8, the detached pass), F-03 (the `Stop` entry), S-08 (step 4, the fold) · **Needs** 009, 011 ·
**Feeds** 019, 023, 024, 025, 027, 028, 030, 031, 035, 048, 061

## Result

After a turn in which the agent failed a call and then got it right, the lobe holds one repair pair: the failed
call, the retry that worked, and what changed between them. The Stop hook starts the work in a detached process
and returns at once. A person or a tester can run the same pass by hand and read what it found.

```sh
# in a sandbox, logged in as acme: one failed call, then the retry that worked
tentaqles lobe-hook claude post-tool-use-failure < fail.json    # npx vitest run
tentaqles lobe-hook claude post-tool-use < ok.json              # npx vitest run --pool=forks
tentaqles lobe consolidate --session s1 --only pair --json
# {"action":"pair","session":"s1","class":"shell","key":"npx vitest","tries":0,
#  "diff":[{"kind":"flag","op":"add","name":"--pool","text":"--pool=forks"}]}
tentaqles lobe consolidate --session s1 --only pair             # a later run: the pair is used, no line
```

The pair is also one `pending` line in `candidates.jsonl`. Nothing is shown to the agent yet; feature 019 turns
the line into a lesson.

## Scope

**In**

- The detached pass: the `Stop` entry in the plugin, `hook.Detach`, the pass runner `internal/lobe/pass`,
  `consolidate.lock`, and the `stop` event starting one pass and not waiting (F-02 step 8, core page 5.2).
- `tentaqles lobe consolidate` with `--session <id>`, `--only pair`, `--now <time>`, `--json` and the hidden
  `--detached`. It runs the same pass in the foreground.
- The pass order as far as it exists today: S-07's sweep, S-08's fold, then pairing.
- `fingerprint.Fold` and the file `fingerprints.json` (S-08 step 4). Feature 006 left the fold to this
  feature, because it runs only in the pass.
- C-01 steps 1 to 4 in full: all four pairing classes (shell, SQL, API, other), the window of 10, the drops.
- C-01 step 5 as far as the cap of 5 pairs per session and the `pending` line in `candidates.jsonl`.
- Two chores the pass owns: `candidates.jsonl` lines expire with the events, and `stats/hook-errors.jsonl` is
  trimmed to its newest 1,000 lines (proposed, F-02 step 7). Features 003 and 011 left both to this feature.
- The `stop` row of `tentaqles lobe bench latency`, `stop` as an event of `bench stall` (feature 012 names
  both), and the first half of leak attack L-10 (feature 013).

**Out**

- Wording a pair as a lesson, the gate, filing, `--draft` and `--only draft` (feature 019; the model in 025).
- A pair whose fingerprint an active lesson already lists. Reinforcing that lesson is feature 030. The case
  where the working call matches the lesson's `avoid.pattern` (a false alarm, a demotion and a successor) is
  features 023, 024 and 031. Until then every kept pair becomes a `pending` candidate.
- The `fixed` sign for an identical retry after an edit (C-01 step 4, C-02 step 6): feature 028. Here that
  case is dropped and gives no pair.
- `--only signs` (features 027 and 028) and `--only fade` (feature 030).
- E-07's ladder moves and E-02's `Settle` in the pass (features 023 and 024), the graph build (feature 048),
  the document run (feature 061).
- Writing the `tampered` mark (feature 034). Pairing already skips a session that carries it.
- The second half of L-10, the fake agent CLI that records the child's environment (feature 025).
- Codex: the pass reads whatever events feature 010 logged, with no Codex-specific code. A Codex `Stop`
  handler (F-03 step 4) is not built here. The index names no feature for it; feature 011's risks say the
  same of Codex's `SessionEnd`. Until one exists, a Codex-only client gets a pass only when a person runs
  `tentaqles lobe consolidate`.

## What it builds on

Task 1 checks each name against the code; the feature files of these numbers are the reference.

- [002](002-sandbox.md): `tentaqles lobe sandbox` and the standard tester setup.
- [003](003-shell-failure-claude-code.md): `internal/lobe/hook` (`Main`, which already knows the event name
  `stop` and its 1 second cutoff, the client rule, the step table, the five-key error line),
  `internal/lobe/store` (`Open`, `Lock`, `Append`, `Replace`, `Update`, `SessionFile`, `ErrBusy`),
  `internal/lobe/eventlog`, `scrub.Clean`, and `plugin/scripts/lobe_hook.sh`, which already accepts `stop`.
- [005](005-status-and-capture-switch.md): `settings.From` (all nine keys), `eventlog.Scan` and `Tail`,
  `internal/lobe/cli`, `internal/cmd/lobe.go`, `tentaqles lobe status`.
- [006](006-error-fingerprints.md): `fingerprint.Of`, `Class` and `Template`, and the `fp` on every failure.
- [007](007-duplicate-filter.md): reference lines (`dup`) and the reader rule "the first line with a session
  and id counts".
- [008](008-successes.md) and [009](009-hidden-failures.md): `ok` events, the full input kept on the success
  that follows a failure, `hidden-fail` events, and `internal/lobe/shellwords`.
- [011](011-retention-and-sweep.md): `eventlog.Sweep`, called at `session-end`.
- [012](012-hook-latency-and-stall.md) and [013](013-isolation-leak-suite.md): the row table of `latency`,
  the case table of `stall`, the attack table in `attacks.go`.
- [014](014-starter-rule-before.md): the `lessons` switch acting.

**What this feature widens.** The brief for this file assumed that feature 011 had registered `Stop`. It did
not: 011 registers `SessionEnd` only and leaves `Stop` here. So this feature adds one group, `Stop`, to
`plugin/hooks/hooks.json`, and the name `Stop` to the list in the pinned plugin test. `hook.Main` gets the
first step of its `stop` row. `eventlog.Sweep` gains a second caller and is not changed. If feature 003
built only the store lock, this feature adds `store.LockFile(name, ctx)` for `consolidate.lock` (F-01 step 6).

## What it leaves for later features

- `internal/lobe/pass`: `Run(opts)` with an ordered list of steps. Later features add theirs in the order of
  core page 5.2: `ladder.Settle` and the ladder moves (023, 024), the graph build (048), documents (061).
- `internal/lobe/consolidate/run.go`: the consolidation step, with its own ordered parts. Today it pairs.
  Later parts: signs (027, 028), draft (019, 025), strength (030), replace (031).
- `hook.Detach` and the hidden flag `--detached`, which feature 025 reads to tell the pass from a run a
  person started.
- `tentaqles lobe consolidate` and its `--json` action lines. This feature prints `pair`; the other names of
  C-01's list (`sign`, `draft`, `file`, `reinforce`, `fade`, `retire`, `replace`) arrive with their features.
- `consolidate.PairEvents(events, used) []Pair`: pairing as a pure function over a list of events, which
  feature 035 runs over past sessions. `Pair`, `Change`, `Class`, `Key`, `Core`.
- `candidates.go` with `Append`, `Pending`, `Close(id, state)` and `Expire`; `candidates.jsonl` with `kind:
  repair` lines; the key `paired` in `sessions/<session>.json`. Features 019, 025, 027, 028 and 035 use them.
- The drop reason `edit-between` returned by pairing, which feature 028 turns into a `fixed` sign.
- `route(pair)`: the one function every kept pair passes. Today it always answers "candidate". Features 030
  and 031 give it the other two answers of C-01 step 5.
- `fingerprints.json` with the masked template of every fingerprint: feature 025 sends that template to the
  model, never the raw error; `tentaqles lobe status` counts its entries.

## How it works

1. **The plugin entry.** `plugin/hooks/hooks.json` gains one group, `Stop`, with no matcher:
   `bash "${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh" stop`, `"timeout": 5` (F-03 step 2). The wrapper does
   not change. The guard's two groups stay byte for byte.
2. **Stop.** `tentaqles lobe-hook claude stop` is capture class, cut off at 1 second (core page 5.2). After
   the client rule (F-02 step 4), the settings and the store, it calls `hook.Detach` and returns. It prints
   nothing. It starts no pass when `capture` and `lessons` are both off (F-02 step 8; the `graph` switch joins
   that test in feature 043).
3. **Detach.** `hook.Detach` starts this same binary (`os.Executable`) as `lobe consolidate --detached`. The
   child gets the payload's `cwd` as its working folder, so it resolves the same client, and the hook's
   environment, so the login variable is the session's. Its stdin, stdout and stderr are the null device and
   it inherits no handle; on Windows it is a detached process in a new process group, elsewhere a new
   session. The hook does not wait for it. A start that fails leaves one `stats/hook-errors.jsonl` line with
   class `detach` (F-02 step 7) and the hook still exits 0.
4. **One pass at a time (proposed).** `pass.Run` resolves the client from the working folder, applies the
   command rule of F-07 step 3 (in an agent session the login variable is unset or this client's own), reads
   the settings, opens the store and tries `consolidate.lock` without waiting. When it is held, the run exits
   0 at once; a run without `--detached` first prints one line saying a pass is already running. The lock is
   held for the whole run. The store lock is taken only around each write, inside F-01's 100 ms. The pass
   resolves the store folder again before each batch of writes and never creates it (F-01, "How to build
   it").
5. **Order.** (a) `eventlog.Sweep`, which also runs with `capture` off. (b) `fingerprint.Fold`. (c) The
   chores: drop `candidates.jsonl` lines older than `retention_days`, trim `stats/hook-errors.jsonl`. (d)
   The consolidation step, which here is pairing; it is skipped when `lessons` is off. `--only pair` and
   `--session` limit (d) only; (a) to (c) always run. `--now` sets the clock of the whole run, the sweep
   included.
6. **The fold (S-08 step 4).** `fingerprint.Fold(lobeDir)` reads the event lines after its read position,
   which it keeps in `fingerprints.json`. Each `fail` or `hidden-fail` line adds to the entry of its `fp`:
   `{id, tool_class, template, count, sessions, first_seen, last_seen}` (core page 5.4). The template is
   worked out again from the line's `tool` and `error` with `fingerprint.Of`. A reference line counts as one
   more occurrence of its `fp`. The file is replaced whole under the store lock; when the lock is busy the
   fold is given up for this pass and the position stays.
7. **What pairing reads.** The two newest `events-*.jsonl` files through `eventlog.Scan`, no lock, bad lines
   skipped. Events are grouped by session, oldest first. A reference line (`dup`, S-06) is read as a repeat of
   its first line and takes that line's `input`; one whose first line is not in the two files is skipped.
8. **Candidates (C-01 step 2).** A `fail` or `hidden-fail` event whose id is not under `paired` in its
   session file. Skipped and marked used: an event whose `input` is at the 4,000-character cap. Skipped
   whole: a session whose file holds `"tampered":true`.
9. **The window (C-01 step 3).** The pairing class and the "same attempt" test come from the table in C-01;
   build them as that table says, with the lobe's own `shellwords`. A shell call that runs a query client is
   class SQL, one that runs `curl`, `Invoke-RestMethod` or `gh api` is class API, and any other shell call is
   class shell. Read the next 10 events of the failure's class in its session. Stop at the first `ok` event
   that is the same attempt and that no earlier failure claimed. Failed tries of the same attempt before it
   count in `tries` (0 when the first retry worked) and are marked used. A failure with no fix and fewer than
   10 later events of its class stays unused for the next pass; with 10 or more it is marked used.
10. **Drops (C-01 step 4).** A found fix gives no pair, and failure and fix are marked used, when: the
    inputs are identical once quotes are removed and spaces collapsed (a flake); they are identical with an
    `ok` edit event between them (reason `edit-between`, for feature 028); only a path argument changed; the
    change adds an item of the never-teach list (proposed: `sudo`, `doas`, `--no-verify`, `--force`,
    `--insecure`); the difference would hold an S-05 marker; a compared value of the fix is exactly 200
    characters long.
11. **Cap and record (C-01 step 5).** At most 5 pairs per session in one pass, oldest failure first; a sixth
    stays unused and is found by the next pass. Each kept pair goes through `route`, then is appended to
    `candidates.jsonl` as
    `{"v":1,"id":"c_…","kind":"repair","state":"pending","at":"<time of the failure>","pair":{…}}`. The pair
    holds `id`, `session`, `fail`, `fix` (event ids of that session), `class`, `key`, `fp`, `tries` and
    `diff`; it holds no input and no error text. The key `at` is added here, so that a line can expire. The
    candidate id is `c_` plus the first 8 hex characters of SHA-256 over the session, a zero byte and the
    failure's event id (proposed), so a line is never appended twice. Then `paired` in the session file
    gains the used ids, through `store.Update`.
12. **Output.** With `--json`, one compact object per action on stdout; a pair has `action`, `session`,
    `class`, `key`, `tries` and `diff`. Without it, one line per pair (proposed):
    `pair <session> <class> <key>: <failed input> -> <working input>`, each input cut at 120 characters.
    Exit 0 when the pass ran, also with nothing found.

How `diff.go` fills a `Change`. C-01 gives the recorded differences per class and one worked example, the
first row. The other rows are this file's reading (proposed); feature 019's template reads them.

| Class | `kind` | `op` | `name` | `text` |
|---|---|---|---|---|
| shell | `flag` | `add`, `remove` | the flag's name, `--pool` | the flag as written, `--pool=forks`; a new word right after a new flag is its value and stays with it, `-C app` |
| shell | `env` | `add` | the variable's name | empty: a value is never kept |
| shell | `wrapper` | `add` | the wrapper program | empty |
| shell | `step` | `add` | the key of the passing call in between | empty |
| shell | `cwd` | `change` | the failing call's `cwd` | the working call's `cwd` |
| shell | `shell` | `change` | the failing call's tool | the working call's tool |
| shell | `words` | `add`, `remove` | empty | one word; at most 12 each way |
| SQL | `token` | `add`, `remove` | empty | one lower-case token, literals masked; at most 12 each way |
| API, other | `key` | `add`, `remove`, `type` | the argument key | empty, or `old -> new` for two short code-like words |
| API (HTTP) | `method`, `path`, `query`, `header` | `add`, `remove`, `change` | the key or header name | the method or masked path |

## Where to read

| What | Where |
|---|---|
| Requirements | [C-01](../prd/lobe-consolidate.md), all of it; [F-02](../prd/lobe-foundation.md) steps 2, 7 and 8 and "How to build it"; [F-03](../prd/lobe-foundation.md) step 2 and "How to build it" (the pinned test); [F-01](../prd/lobe-foundation.md) steps 6 to 9; [F-07](../prd/lobe-foundation.md) step 1 (the `consolidate` row) and step 3; [S-08](../prd/lobe-sense.md) step 4 and "How to build it" (`Fold`); [M-06](../prd/lobe-benchmarks.md) and [M-03](../prd/lobe-benchmarks.md) (attack L-10); [core page](../prd/vertical-lobe.md) 5.1 to 5.4 |
| Shapes pairing reads | [S-03](../prd/lobe-sense.md) steps 4 and 5; [S-04](../prd/lobe-sense.md) step 5; [S-06](../prd/lobe-sense.md) steps 3, 4 and 6; [S-07](../prd/lobe-sense.md) steps 7 and 8; [S-08](../prd/lobe-sense.md) step 1 |
| Source to port (Trail-code, MIT) | [`src/trace/repairs.ts` L36](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L36): `WINDOW = 10`; [L38](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L38): `MAX_REPAIRS = 5`; [L94](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L94): a flake; [L199](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L199): an edit between; [L230](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L230): one unsafe part drops the repair; [`src/trace/shell.ts` L351](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/shell.ts#L351): the strip order, up to 20 rounds; [L130](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/shell.ts#L130): the five output filters; [`src/trace/walls.ts` L165](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/walls.ts#L165): the programs that only print |
| Source, limits we do not copy | [`src/trace/repairs.ts` L257](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L257); [L54](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L54) and [L56](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L56): the query clients Trail-code ignores, our starting list for class SQL; [L172](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L172); [`src/trace/wall-state.ts` L65](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/wall-state.ts#L65); [`src/trace/runs.ts` L320](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/runs.ts#L320) |
| Existing tentaqles code | `cmd/tentaqles/main.go` L13 to L23 (where `hook.Main` is asked); `internal/cmd/root.go`; `internal/cmd/run.go` L283 (`insideAgent`) and L289 (`refuseOtherWorkspace`); `internal/paths/paths.go` L28; `internal/testutil/home.go` (`HomeRun`) and `fakeprog.go`; `plugin/hooks/hooks.json`; `plugin/tests/test_tq_hook.py` (`test_the_plugin_holds_only_the_guard_hook`); `test/integration/docs_test.go` L16 (`documentedGroups`) |

## Build tasks

1. **Check this brief against the code.** Read the code of features 003 to 014 as it stands. List what no
   longer fits: the names under "What it builds on", what `stop` does today, whether `store.LockFile` exists,
   the shape of the bench and leak tables. The owner approves changes to this file before code is written.
2. **Does a host stop a hook's children?** Read the written result of the phase 0 capture (feature 003, F-03
   step 1) for the line on child processes. If it is missing, write a throwaway Stop hook that starts a
   detached child which writes a file after 10 seconds, and hand it to the owner to run in one real client
   (core page section 8). Write the result beside the other phase 0 results. If a host stops the child,
   stop and ask the owner: F-02's risk says the pass must then also start at the next `session-start`.
3. **Detach and the plugin entry.** `internal/lobe/hook/detach_windows.go`, `detach_unix.go`; the `stop` row
   of `steps.go`; the `Stop` group in `plugin/hooks/hooks.json`; the list in `plugin/tests/test_tq_hook.py`.
   Proof: `TestLobeDetachDoesNotHoldTheHookOpen`, `TestLobeStopStartsNoPassWhenOff`,
   `TestLobeHookTimeoutsRegistered`, the plugin suite.
4. **The pass runner and the command.** `internal/lobe/pass/pass.go` (steps, `consolidate.lock`, `--now`,
   the two chores); `internal/cmd/lobe_consolidate.go` (the cobra command and its flags; it sits in
   `internal/cmd` because feature 025 adds a person check there); `store.LockFile` if missing. Proof:
   `TestLobePassOrder`, `TestLobePassOneAtATime`, `TestLobeConsolidateServesOnlyItsClient`.
5. **The fold.** Side by side with task 4. `internal/lobe/fingerprint/fold.go`. Proof:
   `TestLobeFingerprintFold`.
6. **Attempts.** Side by side with task 4. `internal/lobe/consolidate/attempt.go`, `shellcore.go`,
   `sqlshape.go`, `httpshape.go`. Proof: `TestLobeShellCoreTable`, `TestLobePairSQLMasksLiterals`,
   `TestLobePairAPIAndOther`.
7. **Differences.** After task 6's types exist. `diff.go`. Proof: `TestLobePairAddedFlag`,
   `TestLobePairDrops`.
8. **Pairing and candidates.** `pair.go`, `candidates.go`, `run.go`; the `pair` action line and the plain
   line. Proof: `TestLobePairWindow`, `TestLobePairUsedOnce`, `TestLobePairCapPerSession`,
   `TestLobePairSkips`, `TestLobePairReferenceLine`, `TestLobeCandidatesHoldNoErrorText`.
9. **Bench and leak rows.** The `stop` row of `latency`, `stop` in the events of `stall`, the first half of
   L-10 in `attacks.go` (a Stop in acme starts the pass, hooks then run in globex, and the pass wrote only
   under acme). L-10 stays under `not_run`, now waiting for `C-03`. Proof:
   `TestLobeLatency_EveryRegisteredHookHasARow`, `TestLobeLeak_NothingSkippedSilently`.
10. **Notices.** Every file ported from Trail-code starts with the header F-07 asks for and is listed in
    `NOTICE`. Proof: `TestNoticeListsEveryPort`.
11. **Docs.** A `## tentaqles lobe consolidate` section in `docs/reference/commands.md`; one line under
    `[Unreleased]` in `CHANGELOG.md` and one in `plugin/CHANGELOG.md`; `plugin/CLAUDE.md` names the new
    entry. If `CLAUDE.md` has a "never stalls" line for the lobe, add
    `TestLobeDetachDoesNotHoldTheHookOpen` to it. The plugin change reaches people by release
    (`TestRootMarketplacePin`, `docs/RELEASING.md`).
12. **Tester run.** A fresh agent runs "Evaluation" below against the built binary.

## Tests that stay in the repository

From the requirements:

- `TestLobePairAddedFlag`: `npx vitest run` then `npx vitest run --pool=forks` gives 1 pair with 1 `flag`
  change.
- `TestLobePairWindow`: 9 other shell events between give 1 pair; 10 give 0.
- `TestLobePairSQLMasksLiterals`: a `TOP` to `LIMIT` rewrite gives 1 pair of class `sql` in which `42` is
  absent.
- `TestLobePairDrops`: a flake, an identical retry after an edit, and a retry adding `--no-verify` each give
  0 pairs: 3 of 3.
- `TestLobeDetachDoesNotHoldTheHookOpen` (F-02): `stop` returns in under 1 s while its child sleeps 5 s.
- `TestLobeHookTimeoutsRegistered` (F-03): the new `Stop` entry has timeout 5; the guard's entries have 15.
- `test_the_plugin_holds_only_the_guard_hook` (plugin): its list of lobe events gains `Stop`; the guard's two
  entries are unchanged. No other pinned plugin test is edited.

New for this slice:

- `TestLobePassOrder`: the sweep, the fold and pairing run in that order; with `lessons` off the first two
  run and pairing does not.
- `TestLobePassOneAtATime`: with `consolidate.lock` held by another process, a run exits 0 in under 1 s and
  changes 0 bytes in the store.
- `TestLobeStopStartsNoPassWhenOff`: with `capture` and `lessons` off, `stop` starts 0 processes.
- `TestLobeFingerprintFold`: 3 failures of 2 fingerprints in 2 sessions and 1 reference line give 2 entries
  with counts 3 and 1; a second run adds nothing; with the store lock held the file is byte-identical.
- `TestLobePairUsedOnce`: a second pass over the same events gives 0 pairs; a failure with 3 later events
  and no fix gives 0 pairs now and 1 pair after its fix arrives.
- `TestLobePairCapPerSession`: 7 repairs in one session give 5 pairs, and the next pass gives 2.
- `TestLobePairSkips`: a tampered session, an input at 4,000 characters, a fix value of exactly 200
  characters, a path-only change and a difference holding a scrub marker each give 0 pairs: 5 of 5.
- `TestLobePairReferenceLine`: a failure, its `dup` repeat and then the fix give 1 pair with `tries` 1.
- `TestLobePairAPIAndOther`: an MCP call fixed by `state: opened` to `open` gives 1 pair of class `api` that
  holds both words; the same call fixed by a new 60-character value gives a pair that holds the key and no
  value; two `Edit` calls on different files give 0 pairs.
- `TestLobeShellCoreTable`: a leading `cd`, a trailing `| tail -5`, `FOO=1` and `timeout 60` are stripped;
  `npm install` and `npm test` get different keys; `echo` gets none.
- `TestLobeCandidatesHoldNoErrorText`: an error text with a marker string is paired, and the marker occurs 0
  times in `candidates.jsonl`.
- `TestLobeConsolidateServesOnlyItsClient`: with `CLAUDECODE=1` in acme's folder and the login variable
  naming globex's identity folder, the command exits 1 and both stores are byte-identical.

House rules this feature keeps green: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
`TestLobeHookAlwaysExitsZero` and `TestLobeHookServesOnlyItsLogin` (both cover `stop`),
`TestLobeHookErrorsHoldNoContent` (now with class `detach`), `TestLobeNoHooksSwitch`,
`TestDocsReferenceEveryCommand`, `TestLobeCommandTable`, `TestLobeLatency_EveryRegisteredHookHasARow`, the
stall tests of feature 012 with `stop`, feature 013's leak suite, the plugin's `test_tq_hook_*` and
`test_lobe_hook_*` tests, and every test under "Must not trade off" in `CLAUDE.md`, unedited.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows) and an empty
working folder. It is also handed the repository's `plugin` folder, which it only reads; `PLUGIN` is its
path. It uses no network, never clears `CLAUDECODE` and never runs a person-only command.

**Setup.** Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
lobeset() { m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"; }
A="$W/clients/acme"; ID="$TQ_HOME/identities"; L="$ID/acme/lobe"; cd "$A"
export CLAUDE_CONFIG_DIR="$ID/acme/claude"
fail() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"%s"},"error":"%s"}' \
  "$1" "$A" "$2" "$3" | tentaqles lobe-hook claude post-tool-use-failure; }
ok() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"%s"},"tool_response":{"stdout":"ok","stderr":""}}' \
  "$1" "$A" "$2" | tentaqles lobe-hook claude post-tool-use; }
pairs() { tentaqles lobe consolidate --session "$1" --only pair --json | grep '"action":"pair"'; }
```

No line may start with `SETUP FAIL`. The `export` is the login: a lobe hook acts only under the client's own
login folder. Every `fail`, `ok` and `pairs` call must exit 0, and `fail` and `ok` must print nothing.

1. `fail s1 "npx vitest run" "Error: Worker exited unexpectedly"`, `ok s1 "npx vitest run --pool=forks"`,
   `pairs s1`. Expected: 1 line. It holds `"class":"shell"` and
   `"diff":[{"kind":"flag","op":"add","name":"--pool","text":"--pool=forks"}]`. `pairs s1` again: 0 lines.
2. `grep -c '"kind":"repair"' "$L/candidates.jsonl"` prints `1`, the line holds `"state":"pending"`, and
   `grep -c 'Worker exited' "$L/candidates.jsonl"` prints `0`.
3. `fail s2 "npm test" "1 failed"`, `ok s2 "npm test"`, `pairs s2`. Expected: 0 lines. Then
   `fail s3 "git commit -m wip" "hook failed"`, `ok s3 "git commit --no-verify -m wip"`, `pairs s3`.
   Expected: 0 lines.
4. SQL. Pipe the first line below to `tentaqles lobe-hook claude post-tool-use-failure` and the second to
   `tentaqles lobe-hook claude post-tool-use`, each with `ACME` replaced by the value of `$A`. Then
   `pairs s4`. Expected: 1 line. It holds `"class":"sql"`, contains `top` and `limit`, and does not contain
   `987654321`.

   ```json
   {"session_id":"s4","cwd":"ACME","hook_event_name":"PostToolUseFailure","tool_name":"mcp__warehouse__query","tool_input":{"sql":"SELECT TOP 5 name FROM crm.users WHERE id = 987654321"},"error":"syntax error near TOP"}
   {"session_id":"s4","cwd":"ACME","hook_event_name":"PostToolUse","tool_name":"mcp__warehouse__query","tool_input":{"sql":"SELECT name FROM crm.users WHERE id = 987654321 LIMIT 5"},"tool_response":"ok"}
   ```

5. Another client's login teaches nothing. `export CLAUDE_CONFIG_DIR="$ID/globex/claude"`, then
   `fail s5 "npx vitest run" "Error: Worker exited unexpectedly"` and `ok s5 "npx vitest run --pool=forks"`.
   `export CLAUDE_CONFIG_DIR="$ID/acme/claude"`, then `pairs s5`. Expected: 0 lines, no file
   `$L/sessions/s5.json`, and no file under `$ID/globex/lobe/events`.
6. The window. `fail s6 "npx vitest run" "Error: Worker exited unexpectedly"`, then
   `for i in 1 2 3 4 5 6 7 8 9 10; do ok s6 "ls dir$i"; done`, then `ok s6 "npx vitest run --pool=forks"`.
   `pairs s6`: 0 lines. The same in session `s7` with 9 `ls` calls: 1 line.
7. The switch. `lobeset acme lessons off`, `fail s8 "pip install foo" "error: externally-managed-environment"`,
   `ok s8 "pip install --user foo"`, `pairs s8`: 0 lines. `lobeset acme lessons on`, `pairs s8`: 1 line.
8. The fold. `grep -o '"template"' "$L/fingerprints.json" | wc -l` prints 4 or more, and
   `grep -c 'Worker exited unexpectedly' "$L/fingerprints.json"` prints `1` or more.
   `ls "$ID/globex/lobe/fingerprints.json" 2>/dev/null | wc -l` prints `0`.
9. The detached pass. `fail s9 "cargo build" "no Cargo.toml"`, then
   `ok s9 "cargo build --manifest-path app/Cargo.toml"`. Then run:

   ```bash
   SECONDS=0
   printf '{"session_id":"s9","cwd":"%s","hook_event_name":"Stop"}' "$A" | tentaqles lobe-hook claude stop
   echo "exit=$? s=$SECONDS"
   for i in $(seq 20); do grep -q '"session":"s9"' "$L/candidates.jsonl" && break; sleep 0.5; done
   grep -c '"session":"s9"' "$L/candidates.jsonl"
   ```

   Expected: the hook prints nothing, then `exit=0` with `s` of 2 or less, and the last line prints `1`:
   the pass ran after the hook had returned.
10. The plugin. `J="$PLUGIN/hooks/hooks.json"`. `grep -c 'lobe_hook.sh\\" stop"' "$J"` prints `1`,
    `grep -c tq_hook.sh "$J"` prints `2` and `grep -c '"timeout": 15' "$J"` prints `2`.
11. The hook under faults. `tentaqles lobe bench stall --case held-lock --event stop --out "$W/stall"; echo
    "exit=$?"` prints `exit=0`.
12. The guard is unchanged: `printf 'not json' | tentaqles claude-hook pre-tool-use; echo "exit=$?"` prints
    a `BLOCKED` line and ends with `exit=2`.

**Person checks** (the owner, in one real client, in a real Claude Code session, with the plugin of this
build installed)

1. Ask the agent to run a command that fails and then the corrected form with one more flag. After the turn
   ends, wait 10 seconds and run `tentaqles lobe events`, then `tentaqles lobe consolidate --json`.
   Expected: the two calls are in the log; `candidates.jsonl` in that client's `lobe` folder holds a
   `pending` line for the pair, and the command prints no `pair` line for it, because the detached pass had
   already used it. This is the check that the host does not stop the detached pass.

**Score.** PASS needs 12 of 12. "Could not run" is a FAIL.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems, and the plugin suite).
- [ ] The tester brief passes 12 of 12.
- [ ] The house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`, `TestLobeDetachDoesNotHoldTheHookOpen`,
  `TestLobeCandidatesHoldNoErrorText`, and every "Must not trade off" test in `CLAUDE.md` unedited.
- [ ] The guard's two entries in `plugin/hooks/hooks.json` are byte-identical before and after.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md`, `plugin/CHANGELOG.md` and `plugin/CLAUDE.md` are
  updated; ported files are in `NOTICE`.
- [ ] The written answer to task 2 exists.
- [ ] The owner has seen a real failure and its fix become a `pending` line after a real turn.

## Risks and unknowns

- **A host may stop a hook's children.** Not known for either host (F-02's risks). Task 2 answers it before
  `hook.Detach` is built on. On Windows the host may hold its children in a job object; whether a detached
  child must also break away from the job is part of that answer.
- **The list of query clients is not in the requirements.** C-01 says "a query client on a command line" and
  names none. This file starts from the programs Trail-code ignores at `repairs.ts` L54 (proposed). The
  owner should confirm the list.
- **A shared key is a loose test** (C-01's risks): `git push origin main` failing and `git push --tags`
  working would pair. Nothing is shown from a pair in this feature, so the cost here is a junk candidate.
- **`--now` moves the sweep too.** A run with a clock in the future deletes event files as that day would.
  Briefs that pass `--now` must expect it.
- **The fold's `sessions` count** is a count of distinct sessions, which a number alone cannot keep across
  passes. S-08 fixes the entry's seven keys and says only that the read position is kept in the file. What
  else the fold stores to count sessions is the builder's choice, outside the seven keys (proposed).
- **The `stop` row of `bench latency` starts a real pass on each run.** The one-pass lock makes all but one
  exit at once. If the leftover processes disturb the timing, the row runs with `capture` and `lessons` off
  and a second row measures one real start.
- **The plugin changes.** A hook entry reaches people only by a plugin release with a moved pin.
- **Proposed numbers:** one pass per client, the key pattern, 12 words, the 0.6 share of words, 40 characters
  for a kept value, the never-teach list, 1,000 error lines. Trail-code's numbers (window 10, cap 5, 20
  rounds) are ported as they are.
- **Read, not run:** every Trail-code line cited above. The behavior is rebuilt and proved by the tests here.
- **What `<tool_use_error>` text carries** is open until the phase 0 capture says (C-01's risks). Such
  errors are paired like any other.
- **What would change this file:** a phase 0 result that forbids detaching; an owner decision to move the
  lesson-aware branch of step 5 into this feature; a different shape of the bench or leak tables.
