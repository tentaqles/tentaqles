# 007: A repeated call is stored once

**Status** not started · **Stage** Sense · **Entries** S-06 (steps 2 to 6; step 1 came with 003); S-07 (the
readers' rule for two lines with one id) · **Needs** 006 · **Feeds** none named in the index; 008, 009 and 010
pass every line through it

## Result

An agent stuck in a loop fails the same way twenty times in a minute. The client's event log gains one full
line and nineteen short reference lines: each says "this happened again" and points at the first, with no
input and no error text. A hook payload that arrives twice leaves one line, not two.

From the sandbox of [002](002-sandbox.md), with acme's login exported, twenty failures of one call under
twenty call ids give:

```bash
wc -l < "$LOG"              # 20
grep -c '"dup":' "$LOG"     # 19
grep -c '"error":' "$LOG"   # 1
```

## Scope

**In**

- `internal/lobe/eventlog/dedupe.go`: `Check(tail []Event, e Event) (v Verdict, firstID string)` with the
  verdicts `new`, `replay` and `repeat`, pure (S-06, "How to build it"). `ID` is already there from 003.
- The key `dup` on `Event` (`json:"dup,omitempty"`) and the reference line of S-06 step 4.
- The check inside `sense.Record`, under the store lock that 003's append already tries: read the tail of
  today's file, decide, then write a full line, a reference line or nothing (S-06 steps 2 to 6).
- The states `ref` and `replay` of `sense.Outcome.State`.
- The readers' side: of two lines with the same `session` and `id` the first counts (S-06 step 6), and
  `tentaqles lobe events` shows a reference line together with its first line (S-07, "How to build it";
  [005](005-status-and-capture-switch.md) left it to this feature).

**Out**

- The two fill levels of the day file, 8 MiB and 16 MiB (S-07 step 4). They are checked under the same lock
  and belong to [011](011-retention-and-sweep.md), with the drops file and the repair of a torn line.
- What other steps do with a duplicate: E-02's verdict and G-08's mark still run on a `ref` or `replay`
  outcome ([023](023-fires-and-verdicts.md), [048](048-graph-stays-current.md)). This feature only returns the
  outcome.
- Counting a reference line as one more occurrence of its fingerprint (the fold,
  [018](018-repair-pairing.md)) and in M-01's `failures` counter ([058](058-token-savings.md)).
- C-01's rule that a reference line stands for its first line when pairing ([018](018-repair-pairing.md)).
- `ok` lines in general ([008](008-successes.md)), `hidden-fail` lines ([009](009-hidden-failures.md)) and
  Codex ([010](010-shell-failure-codex.md)). Each passes through `Check` as built here, with no change to it.
- Timing the tail read against the capture budget ([012](012-hook-latency-and-stall.md)).

## What it builds on

- [002](002-sandbox.md): the sandbox and the standard tester setup.
- [003](003-shell-failure-claude-code.md): `eventlog.ID(host, session, callID)` in both forms of S-06 step 1,
  with `TestLobeEventIDVector`; the store lock (`store.Lock`, `ErrBusy`, an operating-system lock on the file
  `lock`); the append that tries the lock every 5 ms for at most 50 ms and then writes with or without it
  (`TestLobeLogLockHeld`); `sense.Record` and `Outcome` with the states `full` and `dropped`; the written
  result `docs/prd/spikes/phase0-q3-log-append.md` (do twenty appending processes tear a line, and what does
  the lock cost). Until this feature, a payload sent twice writes two lines with one `id`.
- [004](004-any-tool-failure.md): failure lines for every tool, and the first `ok` line (a search that found
  nothing).
- [005](005-status-and-capture-switch.md): `eventlog.Scan` and `eventlog.Tail(path, max)`, lock-free readers
  that skip and count broken lines; `tentaqles lobe events`, which prints the newest stored lines as they are
  on disk, at most 20 (proposed there); `tentaqles lobe status` with its counts by `kind`.
- [006](006-error-fingerprints.md): the key `fp` on every `fail` line. A repeat needs the same `fp`.

## What it leaves for later features

- `eventlog.Check`, `Verdict` and the `dup` key: used unchanged by 008, 009 and 010.
- `Outcome.State` values `ref` and `replay`: 020, 023 and 048 act on an outcome whatever its state.
- The rule every reader keeps: a pointer to an event is `session` plus `id`, never `id` alone (core page 5.4),
  and a line with `dup` has no text of its own. Readers in 018, 020, 023, 035 and 058 follow it.
- `eventlog.Scan` with the first-line rule built in, so no later reader counts a replay twice.

## How it works

1. **Order inside `Record`.** S-05 scrubs, S-08 fingerprints, then this step, then the append (S-01 step 7).
   The check compares stored forms: the scrubbed `input` of up to 4000 characters and the `fp`.
2. **The lock** (S-07 step 3, F-01 step 6). 003's append tries the store lock every 5 ms for at most 50 ms
   (proposed). With the lock, steps 3 to 6 run and the line is appended before the lock is let go. Without
   it, because another process holds it, the check is skipped and the full line is appended anyway: a lost
   event costs more than a duplicate. The lock is never held while waiting for another lock.
3. **The tail** (S-06 step 2). Read the last 64 KiB (proposed) of today's event file with `eventlog.Tail` and
   use its whole lines. The file is today's by the UTC date of the event's `at`.
4. **Replay** (S-06 step 3). The tail holds a line with the same `session` and `id`. Nothing is written.
   `Record` returns the outcome with state `replay`, and `Kind` and `FP` set as for a new event.
5. **Repeat** (S-06 step 4). The tail holds a full line (one with no `dup`) with the same `session`, `tool`,
   `kind`, `input` and `fp`, whose `at` is at most 300 seconds earlier. A reference line is written with
   exactly these keys: `v`, `id`, `at`, `session`, `host`, `tool`, `kind`, `fp` and `dup`, where `dup` is the
   first line's id and `fp` is there only when the first line has one. It has no `input`, no `error`, no
   `exit`, no `repo` and no `cwd`. State `ref`.
6. **Otherwise** the full line is written. State `full`.
7. **When the check is skipped** (S-06 step 6): `session` is empty; the lock was not obtained in time; the tail
   cannot be read. The full line is written.
8. **Neither case ends the hook early.** `Record` returns its outcome and the steps after Sense run. In this
   feature there are none yet.
9. **Readers** (S-06 step 6, S-07 step 8). They take no lock. Of two lines with the same `session` and `id`
   they keep the first: that is what makes a race between two unlocked writers harmless. `Scan` applies the
   rule, so the counts of `tentaqles lobe status` count such a pair once. A reference line counts as one event
   of its `kind`.
10. **`tentaqles lobe events`** prints lines as they are on disk. When its selection holds a reference line
    whose first line (the line with that `session` and the id in `dup`) is not in the selection, that first
    line is printed in front of the selection, so the output can be longer than the selection. (S-07 says
    "each reference line shown with its first line"; this form is this file's choice.)

Edge cases, all from S-06:

- A repeat is compared with full lines only, so a hit does not extend the window: a loop that lasts longer
  than 300 seconds writes one full line every 300 seconds.
- At UTC midnight the new file's tail is empty, so one duplicate can pass. Accepted.
- In a busy session 64 KiB can cover less than 300 seconds. Some repeats are then stored in full. Harmless.
- The same content in another session is never a repeat.
- An `ok` event has no output to compare. Once successes are logged (008), two successful `git status` calls
  10 seconds apart are a repeat. Intended: the reference line still says the call ran and succeeded.
- An `input` that S-05 cut at 4000 characters is compared as cut. Two calls that differ only after the cut
  count as a repeat.

## Where to read

| What | Where |
|---|---|
| Requirements | [S-06](../prd/lobe-sense.md), whole entry; [S-07](../prd/lobe-sense.md) steps 3 and 8 and the `tentaqles lobe events` item of "How to build it"; [F-01](../prd/lobe-foundation.md) step 6 (the store lock); "Where Sense ends" at the top of the [Sense page](../prd/lobe-sense.md), the items "The outcome" and "A duplicate"; [core page 5.4](../prd/vertical-lobe.md), the event shape |
| Source to port | agentmemory (Apache-2.0): [`src/functions/dedup.ts` L3](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/dedup.ts#L3): the 5 minute window; L38 to L45 build the key from session, tool, the first 500 characters of the input and a digest of the output |
| Weaknesses not to copy | [`src/functions/observe.ts` L96](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/observe.ts#L96): the content check is skipped when an event id is present, and its hooks always send one. [`src/hooks/_capture.ts` L52](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture.ts#L52): with no host id the hook's clock goes into the id, so the same content at two times is never a duplicate |
| The id, already built | [`src/capture/event-id.ts` L66](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/event-id.ts#L66): an id made from the host's own call id and the session; L50 is the digest |
| Existing tentaqles code | `internal/lobe/eventlog` (`append.go`, `read.go`, the `ID` function), `internal/lobe/store` (the lock), `internal/lobe/sense/sense.go`, the `events` and `status` verbs in `internal/lobe/cli`, all from 003 and 005 |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits. Look at: where 003's
   append takes and releases the lock (the tail read and the write must sit inside one hold); what `Tail`
   returns for a first piece that starts mid-line; how `Scan` and the `events` verb pick their lines. Read
   `docs/prd/spikes/phase0-q3-log-append.md`. The owner approves changes to this file first.
2. **`Check`.** Create `internal/lobe/eventlog/dedupe.go` (or add to it, if 003 put `ID` there) and add `Dup`
   to `Event`. The file is a port of agentmemory: it starts with the `// Ported from` header, and `NOTICE`
   lists it. Proof: `TestLobeDedupeCheck`. Can run beside task 4.
3. **Wire the check into `Record`**: inside the lock hold, `Tail`, `Check`, then nothing, a reference line or
   a full line; the states `ref` and `replay`. Files: `internal/lobe/sense/sense.go`,
   `internal/lobe/eventlog/append.go`. Proof: `TestLobeDedupeReplay`, `TestLobeDedupeRepeat`,
   `TestLobeDedupeWindow`, `TestLobeDedupeOtherSession`, `TestLobeDedupeRace`, `TestLobeLogLockHeld` still
   green, and `TestLobeSenseOutcome` widened with the two states.
4. **Readers.** Put the first-line rule into `Scan`, and make `tentaqles lobe events` print the first line of
   a reference line that falls outside its selection. Files: `internal/lobe/eventlog/read.go`, the `events`
   verb in `internal/lobe/cli`. Proof: `TestLobeReaderKeepsTheFirst` and
   `TestLobeEventsShowsReferenceWithFirst`.
5. **Docs.** `docs/reference/commands.md`: the `tentaqles lobe events` section says what a reference line is
   and when an extra first line is printed. One line in `CHANGELOG.md` under `[Unreleased]`. The plugin does
   not change. No "Must not trade off" line is added.
6. **Tester run.** A fresh agent runs the Evaluation below on the built binary.

## Tests that stay in the repository

- `TestLobeEventIDVector` (from 003, kept): `ID("claude", "s1", "toolu_01")` is `evt_9bde2e09`.
- `TestLobeDedupeReplay`: the same payload twice gives 1 line.
- `TestLobeDedupeRepeat`: the same failure under two call ids, 10 seconds apart, gives 2 lines. The second has
  `dup` set to the first id, has no `input` and no `error`, and is under 250 bytes (proposed).
- `TestLobeDedupeWindow`: with a test clock, the same failure 301 seconds later gives 2 full lines.
- `TestLobeDedupeOtherSession`: the same content in another session gives 2 full lines.
- `TestLobeDedupeRace`: 20 processes send the same payload at once. A reader counts 1 event.
- `TestLobeDedupeCheck` (new name, not in the requirements): a table over `Check`: an empty tail, a replay, a
  repeat, a different `fp`, a different `input`, a different `kind`, a tail that holds only a reference line
  (not a repeat), an empty `session` (no check). 8 of 8.
- `TestLobeLogLockHeld` (from 003, kept): with the lock held elsewhere for 2 seconds, `Append` returns in
  under 100 ms (proposed) and writes the full line, although the same content is already in the tail.
- `TestLobeReaderKeepsTheFirst` (new name): two lines with one `session` and `id` read as one event; the same
  `id` in two sessions reads as two.
- `TestLobeEventsShowsReferenceWithFirst` (new name): with 1 full line followed by 25 reference lines,
  `tentaqles lobe events` prints the full line first and then the newest reference lines.
- House rules kept green: `TestLobeHookAlwaysExitsZero` (its held-lock case), `TestLobeHookServesOnlyItsLogin`,
  `TestLobeSenseFailureSkips`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeLogAppendParallel`, `TestLobeStall_GuardStillDenies`, and every test under "Must not trade off" in
  `CLAUDE.md`, unedited.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows) and an empty
working folder. It uses no network and never reads or writes the real `~/.tentaqles`. Setup:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
ACME="$W/clients/acme"; ID="$TQ_HOME/identities"
LOG="$ID/acme/lobe/events/events-$(date -u +%Y%m%d).jsonl"
export CLAUDE_CONFIG_DIR="$ID/acme/claude"; cd "$ACME"
P='{"session_id":"SID","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping",'
P="$P"'"tool_use_id":"CALL","tool_input":{"n":4411},"error":"widget 4411 is not frobnicated"}'
send() {  # send <session> <call id>
  j="${P//ACME/$ACME}"; j="${j/SID/$1}"; j="${j/CALL/$2}"
  printf '%s' "$j" | tentaqles lobe-hook claude post-tool-use-failure; echo "exit=$?"
}
```

Stop and report FAIL if a line starts with `SETUP FAIL`. Run checks 1 to 6 within 5 minutes, and not across
UTC midnight (if `date -u +%Y%m%d` changes during the run, start again). Every `send` must print only `exit=0`.

1. `send s1 toolu_01`. Expect 1 line in `LOG`, with `id` `evt_9bde2e09`, `input` `{"n":4411}`, the `error`,
   `fp` `fp_48622a622f4928cc` and no `dup`.
2. `send s1 toolu_01` again: a replay. Expect still 1 line.
3. `send s1 toolu_02`: a repeat. Expect 2 lines. The second has `id` `evt_8618533e`, `dup` `evt_9bde2e09`,
   `kind` `fail`, `fp` `fp_48622a622f4928cc`, no `input` and no `error`, and is shorter than 250 bytes.
4. `send s2 toolu_01`: another session. Expect 3 lines. The third has `id` `evt_56f0c6aa`, `input`
   `{"n":4411}` and no `dup`.
5. The loop: `for i in $(seq 1 20); do send s3 "loop_$i"; done`. Expect 23 lines. Of the 20 new ones, exactly
   one has an `error` and `id` `evt_ddae6c70`; the other 19 have `dup` `evt_ddae6c70` and no `error`.
   `grep -c '"dup":' "$LOG"` prints `20`.
6. Five more: `for i in $(seq 21 25); do send s3 "loop_$i"; done`. Expect 28 lines. Then
   `tentaqles lobe events > ev.txt`. `grep -c '"dup":"evt_ddae6c70"' ev.txt` prints `20`,
   `grep -c '"id":"evt_ddae6c70"' ev.txt` prints `1`, and that line is the first line of `ev.txt`: the first
   line was outside the newest 20 and is shown all the same. (If 005 chose another number than 20, the first
   count is that number; the other two expectations stand.)
7. Another client and no login. `CLAUDE_CONFIG_DIR="$ID/globex/claude" send s5 toolu_01` and
   `(unset CLAUDE_CONFIG_DIR; send s5 toolu_01)` each print only `exit=0`. `LOG` still has 28 lines, and
   `ls "$ID/globex/lobe/events" 2>/dev/null | wc -l` prints `0`.

A held lock is not checked here: a tester has no portable way to hold the store lock from outside the hook.
`TestLobeLogLockHeld` and `TestLobeDedupeRace` cover it. There are no person checks. PASS needs 7 of 7.

## Done when

- [ ] the tests named above are in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) on Linux, macOS and Windows
- [ ] the tester brief passes, 7 of 7
- [ ] the house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeLogAppendParallel`, `TestLobeLogLockHeld`, `TestLobeGuardNeverImportsLobe`, and every test under
  "Must not trade off" in `CLAUDE.md` unedited
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated; `NOTICE` lists the ported file
- [x] the owner has seen twenty identical failures leave one full line and nineteen reference lines

## Risks and unknowns

- The numbers are proposed and unmeasured: 64 KiB of tail, 300 seconds, 50 ms of lock wait, 250 bytes for a
  reference line. [012](012-hook-latency-and-stall.md) measures what the tail read costs the hook. The read
  now sits inside the lock hold, which F-01 wants under 100 ms (proposed).
- Whether an append without the lock can tear a line on Windows is phase 0 question 3, answered in 003's
  written result. If it says a line can tear, step 2's "append anyway" relies on the readers skipping a
  broken line (S-07 step 8), which 005 built, and on the repair of a torn line, which waits for 011.
- Every reader of the log must know reference lines. A reader that ignores `dup` sees events with no text.
  Each later feature that reads the log names this file.
- The form of `tentaqles lobe events` for a reference line (step 10) is this file's reading of one sentence
  in S-07. The owner may prefer the reference line printed with its first line's text filled in.
- Two events of one session share an 8 digit id about once in 4 billion pairs. Readers then keep the first and
  the second event is lost; nothing else breaks, because no pointer uses `id` without `session` (S-06, Risks).
- A loop of 20 identical failures later weighs 20 in the fingerprint counts. Whether it should weigh 1 is for
  M-01 to report (S-06, Risks).

## Result of the build, the review and the tester run

Built on 2026-11-10 by four Haiku builders (the check and the readers' rule, the `events` verb and docs, the
wiring into `Record`, one fix round), reviewed as one change by a Sonnet reviewer.

- **Brief changes.** `Event` already had `dup`. `Check` lives in `dedupe_check.go` (with the agentmemory port
  header; `dedupe.go` keeps `ID`). The tail is decoded into a plain `TailEvent` with the strings as stored, and
  `Check(tail []TailEvent, e Event)` compares the new event's scrubbed input with the stored string: the first
  build decoded the tail back through the scrubber, which cost scrub time per line inside the store lock and cut
  long inputs again so they never compared equal (the main session caught it). The check and the write run in
  `eventlog.AppendChecked`, inside one lock hold; a busy lock writes the full line.
- **The review** found no blocking defect and confirmed the lock hold, the reference line's keys (it still
  passes `scrub.Line`), and that 063's rejected calls neither double nor get lost. Fixed from it, with tests:
  `Scan` kept a seen set over every retained day (now the current and the previous day file, each line parsed
  once); `lobe events` could read every older day file for an expired first line (now only the reference's day
  and the one before) and took the newer copy of a pair (now the older, as `Scan`); a failed reference line
  would have dropped the event (now the full line is written); the docs said the first-line rule held across
  all of `lobe events` (it holds within the printed lines).
- Tests: `go vet`, `go build` and `go test ./... -count=1` green on Windows; the plugin suite 34 passed.
- Tester brief: 7 of 7 on Windows 11 in Git Bash against a program built from the working tree; the reference
  line is 173 bytes.

The owner's check, 2026-11-10, in the `tqx` playground: twenty identical failures of `mcp__acme__ping` under
twenty call ids left 20 lines, 19 of them reference lines with `"dup":"evt_d3c26cb1"`, and 1 line with an
`error`; `tentaqles lobe events` printed the full line first and the 19 references after it, each with only
`v`, `id`, `at`, `session`, `host`, `tool`, `kind`, `fp` and `dup`.

Left open: CI on Linux and macOS.
