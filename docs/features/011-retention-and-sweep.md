# 011: Old events expire

**Status** not started · **Stage** Sense · **Entries** S-07 (the rest: line and file limits, the torn-line
repair, the drops file, the sweep, `lobe events purge`), F-06 (`retention_days` gets its reader), F-03 (the
`SessionEnd` entry), F-02 (the `session-end` row), F-07 (`lobe-events-dropped`) · **Needs** 005 ·
**Feeds** 012, 013, 018

## Result

When a Claude Code session in a client's folder ends, the day files of that client's event log that are older
than `retention_days` are gone. Newer files, and files with other names, stay. A line that was cut short by a
crash costs only itself: the next event starts on a fresh line and every reader skips the broken one. A log
that grows too large stops taking lines and counts what it refused, without ever holding up a tool call.

The steps that show it, in the sandbox, with acme's login folder in `CLAUDE_CONFIG_DIR`:

```bash
mkdir -p "$TQ_HOME/identities/acme/lobe/events"
: > "$TQ_HOME/identities/acme/lobe/events/events-20200101.jsonl"
printf '{"session_id":"s1","hook_event_name":"SessionEnd","cwd":"%s","reason":"other"}' "$W/clients/acme" |
  tentaqles lobe-hook claude session-end; echo "exit=$?"
ls "$TQ_HOME/identities/acme/lobe/events"
```

The hook prints only `exit=0`, and the file dated 2020 is no longer listed.

## Scope

**In**

- `eventlog.Sweep(dir, retentionDays, now)` with its bounds (S-07 step 7).
- The `session-end` row of the hook's step table: capture class, 1 second cutoff (F-02 step 2).
- One new plugin entry, `SessionEnd`, through the wrapper of 003, and one more name in the pinned plugin
  test's list (F-03 step 2).
- `retention_days` is read by the sweep: a whole number from 1 to 365 (range proposed), else 14 (F-06).
- The limit on one line and the two fill levels of a day file (S-07 steps 2 and 4).
- The repair of a torn line on append (S-07 step 5, second sentence).
- The drops file `events/drops-YYYYMMDD.jsonl` and the outcome state `dropped` (S-07 step 6).
- `tentaqles lobe events purge`, for a person only (S-07 "How to build it", F-07 step 1).
- The doctor finding `lobe-events-dropped` (F-07 step 5).
- Codex's `SessionEnd` handler: one more row in `lobeEvents` of `internal/codexconfig`, with timeout 3
  (F-03 steps 4 and 5) and its trust record, rendered the way 010 rendered `PostToolUse`. The sweep is
  the same code for both hosts. This is the one Codex part outside 010 and 036: the Sense stage ships
  after 015, and a client used only through Codex must not keep events forever.

**Out**

- The `Stop` entry, `hook.Detach`, `internal/lobe/pass` and `tentaqles lobe consolidate` (018). The
  requirements run the sweep twice: at session end, and as the first step of the detached pass that `Stop`
  starts. Only the first is built here. 018 calls `eventlog.Sweep` as its pass's first step and changes
  nothing in it. Until 018, a session that ends without a `SessionEnd` event (a crash) leaves expired files
  in place until the next session of that client ends.
- The trim of `stats/hook-errors.jsonl` to its newest 1,000 lines, which the detached pass does (018).
- The expiry of candidate lines with `retention_days` (018, where `candidates.jsonl` is born).
- Codex's `Stop` handler, which arrives in 036 with the rest of what the lobe does in Codex.
- `sessions/<session>.json`. Nothing in the requirements expires a session file, and Sense writes none. The
  sweep does not touch that folder.
- The latency and stall measurements of `session-end` (012) and the leak attacks (013).

## What it builds on

- **003**: `hook.Main` with its seven known event names and cutoffs, the client rule, `store.Open`,
  `store.Lock`, `store.Append`, `eventlog.Append`, `sense.Outcome`, `plugin/scripts/lobe_hook.sh` (which
  already accepts `session-end`), the plugin test's list of lobe events, `stats/hook-errors.jsonl`.
- **004**: the `ok` line of a grep that found nothing. It is the one `ok` event this feature can count on
  from its stated needs; when 008 is built, every success writes one.
- **005**: `settings.From` (it already reads `retention_days` and gives 14 for a wrong value),
  `eventlog.Scan` and `Tail` with the reader rule, `tentaqles lobe events`, `internal/lobe/health`, the
  command rules, `internal/cmd/lobe.go`.
- **007, 008, 009, 010**, when they are built before this one, as their numbers say: the limits below apply
  to their lines too, and 010's drop reason `no-exit` (S-02) is written through the drops file of this
  feature. If 010 left that reason uncounted because no drops file existed, wire it here.

## What it leaves for later features

| Name | What it is | Used by |
|---|---|---|
| `eventlog.Sweep(dir, retentionDays, now)` | deletes expired day files, bounded | 018 (first step of the pass) |
| `eventlog` drops | `drops-YYYYMMDD.jsonl`, lines `{"at":"...","why":"<reason>"}`; reasons `full`, `big`, `disk`, `no-exit` | 010 (`no-exit`), 058 (error counters) |
| `Outcome.State` `dropped` | no line was written; `Kind` is still set | 023 (E-02 judges it the same way) |
| The `session-end` step list | one step today | later features add nothing: the event only sweeps |
| `tentaqles lobe events purge` | deletes every events and drops file of the client | the owner; 013 does not use it |
| `lobe-events-dropped` | the alarm for a log that refuses lines | 010, 012 |

## How it works

1. **The entry in the plugin.** `plugin/hooks/hooks.json` gains one group, `SessionEnd`, with no matcher:
   `bash "${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh" session-end`, `"timeout": 5`. The wrapper does not
   change. The guard's two groups stay byte for byte. The pinned test's list of lobe events gains
   `SessionEnd`.
2. **The event.** `session-end` is capture work: cut off at 1 second, the watchdog at 250 ms more (F-02
   steps 1 and 2). The hook reads `session_id` and `cwd` from the payload and ignores the rest. It then does
   what every lobe hook does, in the order of 003 and 005: resolve the folder, apply the client rule, read
   the settings, open the store. Its one step is the sweep. It prints nothing, on any path.
3. **What the sweep deletes** (S-07 step 7). Each `events-YYYYMMDD.jsonl` and `drops-YYYYMMDD.jsonl` in
   `events/` whose date is more than `retention_days` days before today's UTC date. With 14, on 2026-11-23
   the file for 2026-11-08 goes and the one for 2026-11-09 stays. A file with any other name, or with a
   date that does not parse, is left alone. When the day files together hold more than 64 MiB (proposed),
   the oldest go too, until they do not.
4. **Its bounds.** The sweep takes the store lock as a hook does and is skipped when the lock is held. It
   deletes at most 64 files per run (proposed), oldest first; the rest wait for the next run. It runs when
   `capture` is `off`, so switching capture off never keeps old events around (F-06 step 1).
5. **`retention_days`** comes from `settings.From`. A lower number makes the next sweep delete more; a
   higher number keeps files longer from now on; nothing deleted comes back.
6. **One line's limit** (S-07 step 2). When the JSON line with its line break is over 32 KiB (proposed),
   `input` and `error` are cut in half, at most 4 times (proposed). A line still too long is dropped with
   the reason `big`.
7. **A day file's fill levels** (S-07 step 4), checked only when the hook holds the lock. At 8 MiB or more
   in today's file (proposed), `ok` events are no longer written. At 16 MiB or more (proposed), nothing is.
   Each event not written is a drop with the reason `full`. Without the lock the check is skipped and the
   line is appended, as in 003.
8. **The repair** (S-07 step 5). Before the append, when the file's last byte is not a line break, a line
   break goes first. Only the torn line is lost; 005's readers skip and count it.
9. **A drop** (S-07 step 6). `Record` returns an outcome with the state `dropped`, the hook exits 0 and
   prints nothing. One line, `{"at":"<time>","why":"<reason>"}`, is added to `events/drops-YYYYMMDD.jsonl`
   when that write works. A write error on the event itself is the reason `disk`. The drops file holds a
   count of events that could not be written, never their content: exactly those two keys. It stops growing
   at 64 KiB (proposed).
10. **`tentaqles lobe events purge`** deletes every `events-*.jsonl` and `drops-*.jsonl` of the client
    under the store lock, and nothing else. It answers only to a person: the check sits in
    `internal/cmd/lobe_events.go`, it refuses inside an agent session and changes nothing, and it asks
    before it deletes, as the other person-only commands do. It follows the command rules of 005.
11. **`lobe-events-dropped`** is a warning with the client's name when the drops files hold 5 or more lines
    from the last 24 hours (proposed), shown with the count per reason. `internal/lobe/health` gains it.
12. **What the sweep never touches**: `lessons/`, `sessions/`, `stats/`, `store.json`, anything outside
    `events/`, and anything of another client. A session-end hook under another client's login, or under
    none, deletes nothing: the client rule ends it before the store is opened.

## Where to read

| What | Where |
|---|---|
| Requirements | [S-07](../prd/lobe-sense.md) steps 2, 4 to 7, "How to build it" and its risks; [S-01](../prd/lobe-sense.md) "Where Sense ends" for the state `dropped` |
| Requirements | [F-06](../prd/lobe-foundation.md) step 1, the rows `retention_days` and `capture`; [F-02](../prd/lobe-foundation.md) steps 1, 2 and 8; [F-03](../prd/lobe-foundation.md) steps 2 and 5; [F-07](../prd/lobe-foundation.md) step 1 (the two `events` rows) and step 5 |
| Shared rules | [core page](../prd/vertical-lobe.md) 5.2 (the SessionEnd and Stop rows, the budgets), 5.3 (the two files under `events/`), 5.6, A-9 |
| Test names that guard these paths | [M-06](../prd/lobe-benchmarks.md): the `stop`, `session-end` row and `TestLobeHookCutoffsUnderRegisteredTimeout` |
| Source to port: cap, age limit, drop and count | [`src/capture/spool.ts` L104](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L104) (a 5 MiB cap; L106: a 168 hour age limit; L346: when full, drop and count; L251: a reader skips a line that does not parse) |
| Source to port, changed: the lock around pruning | [`src/capture/spool.ts` L202](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L202) (L341 skips the pruning without the lock) |
| Host facts (web, not machine-checked) | <https://code.claude.com/docs/en/hooks>: a SessionEnd hook gets 1.5 s unless its own timeout is higher |
| Existing code from earlier features | `internal/lobe/eventlog/append.go` and `read.go`, `internal/lobe/hook/steps.go`, `internal/lobe/health/health.go`, `internal/lobe/cli/events.go`, `plugin/hooks/hooks.json`, `plugin/tests/test_tq_hook.py` |

## Build tasks

Tasks 2, 3 and 4 can run side by side. Task 5 needs task 2; task 6 needs task 3.

1. **Check this brief against the code as it stands**: which of 006 to 010 are built, what an `ok` line
   looks like now, whether 010 wrote a `no-exit` drop anywhere, the names in `eventlog` and `hook`. List
   what no longer fits; the owner approves changes to this file first.
2. **The sweep.** File: `internal/lobe/eventlog/sweep.go`. Tests: `TestLobeLogSweep`,
   `TestLobeLogSweepBounds` (name proposed here).
3. **Limits, repair and drops.** Files: `internal/lobe/eventlog/append.go`, `drops.go`;
   `internal/lobe/sense/sense.go` for the state `dropped`. Tests: `TestLobeLogCaps`, `TestLobeLogBigLine`,
   `TestLobeLogTornLineRepair`, `TestLobeLogDropsFile` (three names proposed here), and `TestLobeLogDiskFull`
   from 003 with one more line: the drop is counted with the reason `disk` when the drops file can be
   written.
4. **The plugin entry.** Files: `plugin/hooks/hooks.json`, `plugin/tests/test_tq_hook.py` (the list),
   `plugin/CLAUDE.md`. Check: the plugin suite; a diff that shows the guard's two groups unchanged.
5. **The `session-end` step.** Files: `internal/lobe/hook/steps.go` and the step itself. Tests:
   `TestLobeSessionEndSweeps` (name proposed here), and `TestLobeHookServesOnlyItsLogin` with an expired
   file added to its fixture.
6. **The Codex handler.** File: `internal/codexconfig/codexconfig.go`: the `SessionEnd` row in `lobeEvents`
   with timeout 3, rendered and trusted as 010 did for `PostToolUse`; the guard's trust is untouched.
   Tests: `TestLobeCodexSessionEndHandler` (name proposed here), and 010's
   `TestLobeCodexHooksNeverChangeGuardTrust` unedited.
7. **`purge` and the finding.** Files: `internal/cmd/lobe_events.go`, `internal/lobe/cli/events.go`,
   `internal/lobe/health/health.go`. Tests: `TestLobeEventsPurge` (name proposed here),
   `TestLobePersonVerbsRefuseInAgent`, `TestLobeCommandTable`, `TestLobeDoctorCodes`.
8. **Docs.** `docs/reference/commands.md`: `purge` under `## tentaqles lobe events`, `session-end` under
   `## tentaqles lobe-hook claude`, the new finding under `## tentaqles doctor`.
   `docs/reference/manifest.md`: `retention_days` acts from now on. `CHANGELOG.md` and `plugin/CHANGELOG.md`
   under `[Unreleased]`. No new "Must not trade off" line; the lines of 003 gain no test.
9. **The tester run** (Evaluation), by a fresh agent, then the owner's Person checks.

## Tests that stay in the repository

Names are the requirements' unless marked.

- `TestLobeLogSweep`: retention 14, files 13, 14 and 15 days old: only the third is deleted; `notes.txt`
  stays.
- `TestLobeLogSweepBounds` (name proposed here): with the lock held elsewhere nothing is deleted; with 100
  expired files one run deletes the 64 oldest and the next the other 36; with day files over 64 MiB the
  oldest go until the total is under it; a file named `events-2020.jsonl` stays; with `capture` off the
  sweep still deletes; retention 3 and an invalid value (read as 14) each delete what step 3 says.
- `TestLobeLogCaps`: at 8 MiB an `ok` is dropped and a `fail` written; at 16 MiB both are dropped, 1 line
  each in the drops file.
- `TestLobeLogBigLine` (name proposed here): a line over 32 KiB is written with `input` and `error` halved
  until it fits; one that does not fit after 4 halvings is dropped with the reason `big`.
- `TestLobeLogTornLineRepair` (name proposed here): after the 5 characters `{"v":` with no line break, the
  next event is a whole line of its own, and `Scan` returns it and a skip count of 1.
- `TestLobeLogDropsFile` (name proposed here): each drop line has exactly the keys `at` and `why`; with a
  payload full of marker strings, 0 markers are in the file; the file stops at 64 KiB; when the drops file
  cannot be written the hook still exits 0.
- `TestLobeSessionEndSweeps` (name proposed here): the hook on `session-end` deletes an expired file,
  prints 0 bytes and exits 0 within 1.25 s; with stdin that never closes it exits 0 by then and deletes
  nothing.
- `TestLobeEventsPurge` (name proposed here): events and drops files are gone, `keep.txt` and every file
  outside `events/` stay, and a second client's store is byte-identical.
- Widened: `TestLobePersonVerbsRefuseInAgent` (`lobe events purge` exits non-zero under `CLAUDECODE=1` and
  changes 0 bytes), `TestLobeCommandTable` (one more row, marked "yes"), `TestLobeDoctorCodes` (one more
  code), the plugin's `test_the_plugin_holds_only_the_guard_hook` (the list gains `SessionEnd`).

House-rule tests. *A hook never stalls*: `TestLobeHookAlwaysExitsZero`, `TestLobeStall_SpinningHandler`,
`TestLobeHookCutoffsUnderRegisteredTimeout` and `TestLobeHookTimeoutsRegistered` cover the new event without
an edit, because they walk the event table and the entries; `TestLobeSessionEndSweeps` adds the 1 second
class. *The wrong client gets nothing*: `TestLobeHookServesOnlyItsLogin` now proves that a session end under
another client's login deletes nothing; `TestLobeHookLoginUnsetDoesNothing` and `TestLobeNoHooksSwitch` stay
green. *No secret on disk*: `TestLobeLogDropsFile`, and the `TestLobeScrub*` tests unedited. *The guard is
untouched*: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, `TestHookDeadlinesUnderHarnessTimeouts`
and every other test under "Must not trade off" in `CLAUDE.md`, unedited.
- `TestLobeCodexSessionEndHandler` (name proposed here): after a render, the Codex config holds the lobe's
  `SessionEnd` handler with timeout 3 and a trust record, and `tentaqles lobe-hook codex session-end`
  deletes an expired day file for the client whose `CODEX_HOME` it is, and nothing for another: 3 of 3.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, an empty working folder, and the
repository's `plugin` folder to read (`PLUGIN` is its path). Use bash (Git Bash on Windows). No network. Run
the standard setup of feature 002 first:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
lobeset() {  # lobeset <client> <key> <value>: a fixture edit of a sandbox manifest
  m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"
}
```

Stop and report FAIL when a line starts with `SETUP FAIL`. Then:

```bash
ID="$TQ_HOME/identities"; ACME="$W/clients/acme"; AL="$ID/acme/claude"
EVENTS="$ID/acme/lobe/events"; mkdir -p "$EVENTS"
LOG="$EVENTS/events-$(date -u +%Y%m%d).jsonl"; DROPS="$EVENTS/drops-$(date -u +%Y%m%d).jsonl"
E="tentaqles lobe-hook claude session-end"; F="tentaqles lobe-hook claude post-tool-use-failure"
d() { date -u -d "$1 days ago" +%Y%m%d 2>/dev/null || date -u -v-"$1"d +%Y%m%d; }   # d 15: the date 15 days ago
old() { : > "$EVENTS/$1-$2.jsonl"; }                                                # old events 20200101
endj() { printf '{"session_id":"s1","hook_event_name":"SessionEnd","cwd":"%s","reason":"other"}' "$ACME"; }
end() { endj | CLAUDE_CONFIG_DIR="$AL" $E; echo "exit=$?"; }                        # a session end as acme
fail() {  # fail <tool_use_id> <command> <error>: one failed Bash call as acme
  printf '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"%s","tool_name":"Bash","tool_use_id":"%s","tool_input":{"command":"%s"},"error":"%s"}' \
    "$ACME" "$1" "$2" "$3" | CLAUDE_CONFIG_DIR="$AL" $F
}
has() { test -f "$EVENTS/$1-$2.jsonl" && echo yes || echo no; }                      # has events 20200101
cd "$ACME"
```

The login variable is set only inside `end` and `fail`. `end` must always print only `exit=0`. If the UTC
date changes during the run, start again.

1. **The sweep goes by date.** `old events 20200101; old drops 20200101; old events "$(d 15)"`, then
   `old events "$(d 14)"; old events "$(d 13)"; : > "$EVENTS/keep.txt"; fail toolu_01 'x --one' 'no'`, then
   `end`. `has events 20200101`, `has drops 20200101` and `has events "$(d 15)"` print `no`.
   `has events "$(d 14)"` and `has events "$(d 13)"` print `yes`; `keep.txt` and `LOG` are still there.
2. **`retention_days`.** `lobeset acme retention_days 3; old events "$(d 4)"; old events "$(d 3)"; end`.
   `has events "$(d 4)"`, `has events "$(d 13)"` and `has events "$(d 14)"` print `no`; `has events "$(d 3)"`
   prints `yes`. Then `lobeset acme retention_days 0`: `tentaqles lobe settings | grep retention` prints
   `retention_days=14 (invalid)`, and after `old events "$(d 10)"; end`, `has events "$(d 10)"` prints
   `yes`. Then `lobeset acme retention_days 14`.
3. **Capture off still sweeps.** `lobeset acme capture off; old events 20200102; end`.
   `has events 20200102` prints `no`. Then `lobeset acme capture on`.
4. **The wrong client deletes nothing.** `old events 20200103`. Each of these prints only `exit=0`:
   `endj | CLAUDE_CONFIG_DIR="$ID/globex/claude" $E; echo "exit=$?"`,
   `endj | env -u CLAUDE_CONFIG_DIR $E; echo "exit=$?"`,
   `endj | CLAUDE_CONFIG_DIR="$AL" TQ_LOBE_NO_HOOKS=1 $E; echo "exit=$?"`.
   `has events 20200103` prints `yes` after all three. Then `end`, and it prints `no`.
5. **The sweep is bounded.** `for i in $(seq 100 169); do old events "$(d $i)"; done`, then
   `B=$(ls "$EVENTS" | wc -l); end; ls "$EVENTS" | wc -l` prints `exit=0` and B minus 64. A second `end`
   brings the count to B minus 70.
6. **It never stalls.** `SECONDS=0; CLAUDE_CONFIG_DIR="$AL" $E < <(sleep 10); echo "exit=$? s=$SECONDS"`
   prints `exit=0` and `s` of 2 or less. `printf 'not json' | CLAUDE_CONFIG_DIR="$AL" $E; echo "exit=$?"`
   prints only `exit=0`.
7. **A torn line costs only itself.** `printf '{"v":' >> "$LOG"`, then `fail toolu_02 'x --two' 'no'`.
   `wc -l < "$LOG"` prints `3`: line 2 is `{"v":` and line 3 parses as JSON with `id` `evt_8618533e`.
   `tentaqles lobe events | wc -l` prints `2`, with exit 0.
8. **A full file refuses, and counts.** `yes '{"v":1,"pad":true}' | head -c 8388608 > "$LOG"`. A grep that
   found nothing is an `ok` event: `fail toolu_03 'grep -n zzz notes.txt' 'Exit code 1'` prints nothing,
   `grep -c zzz "$LOG"` prints `0`, and `DROPS` has 1 line that holds `"why":"full"`. A failure is still
   written: after `fail toolu_04 'x --three' 'no'`, `tail -n 1 "$LOG"` holds `--three`. Then
   `yes '{"v":1,"pad":true}' | head -c 16777216 > "$LOG"` and `fail toolu_05 'x --four' 'no'`:
   `grep -c -- --four "$LOG"` prints `0` and `DROPS` has 2 lines. Each line of `DROPS` is a JSON object
   with exactly the keys `at` and `why`, and `grep -c -E 'four|zzz|grep|toolu' "$DROPS"` prints `0`.
9. **The alarm.** `tentaqles doctor --json | grep -c '"code":"lobe-events-dropped"'` prints `0` with 2
   drops. After `fail toolu_06 'x --five' 'no'; fail toolu_07 'x --six' 'no'; fail toolu_08 'x --seven' 'no'`
   `DROPS` has 5 lines and the same command prints `1`.
10. **The plugin.** `J="$PLUGIN/hooks/hooks.json"; K="$PLUGIN/scripts/lobe_hook.sh"`.
    `grep -c 'lobe_hook.sh\\" session-end' "$J"` prints `1`; `grep -c tq_hook.sh "$J"` prints `2`;
    `grep -c '"timeout": 15' "$J"` prints `2`. `printf '{}' | bash "$K" session-end; echo "exit=$?"` prints
    only `exit=0`.
11. **`purge` answers only to a person.** `CLAUDECODE=1 tentaqles lobe events purge; echo "exit=$?"` prints
    `exit=1`, and `LOG` and `DROPS` are still there.

**Person checks**, for the owner:

1. In a terminal of their own, after exporting the tester's `HOME`, `USERPROFILE` and `TQ_HOME`, in
   `clients/acme`: `tentaqles lobe events purge`, answering what it asks. Expect `events/` to hold no
   `events-*.jsonl` and no `drops-*.jsonl` file, and `keep.txt` to stay.
2. In a real client, with this build and the checkout's plugin loaded as in 003: create an empty
   `events-20200101.jsonl` in that client's `lobe/events`, open a Claude Code session there and end it.
   Expect the file to be gone and today's file to stay. Then start a session and close its terminal window
   instead of ending it, and tell the builder whether the file planted before it was removed: that shows
   whether `SessionEnd` fires on that path.

**Scoring.** PASS needs 11 of 11. "Could not run" is a FAIL. Report anything surprising; fix nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI on three
  systems: `go vet ./...`, `go test ./... -count=1`, `go build ./...`, and the plugin suite
  (`PYTHONPATH=plugin python -m pytest plugin/tests -q`).
- [ ] The tester brief passes, 11 of 11.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookCutoffsUnderRegisteredTimeout`,
  `TestLobeHookServesOnlyItsLogin` (with the expired file in its fixture), `TestLobeLogDropsFile`,
  `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, and the guard's tests unedited.
- [ ] The guard's two entries in `plugin/hooks/hooks.json` are byte-identical before and after.
- [ ] `docs/reference/commands.md`, `docs/reference/manifest.md`, `CHANGELOG.md`, `plugin/CHANGELOG.md` and
  `plugin/CLAUDE.md` are updated.
- [ ] The owner has seen an old file disappear when a real session ended (Person check 2).

## Risks and unknowns

- **`SessionEnd` may not fire** after a crash or a closed window, and Claude Code gives the hook about 1.5
  seconds by default (web, not machine-checked; S-07 risks). The sweep works under the 1 second cutoff.
  The second caller, the pass after `Stop`, arrives in 018; until then an expired file can outlive its date
  by as long as the client has no clean session end. Person check 2 measures the closed-window path.
- **Which fields a `SessionEnd` payload carries** comes from the phase 0 result of 003. This feature needs
  only `cwd`. If the payload has no `cwd`, the hook falls back to the process folder, as the guard does.
- **Codex.** The Codex `SessionEnd` handler was added to this feature after the brief was written, so the
  build team takes the details from 010's pattern (`lobeEvents`, the render, the trust record). That
  Codex cuts a session-end hook at 3 seconds is a web fact; 010's capture result should show it.
- **The sizes are estimates** (S-07 risks): 32 KiB per line, 8 and 16 MiB per day file, 64 MiB in all, 64
  files per run, 64 KiB of drops. 012 measures what a large file costs the append.
- **The fill levels need the lock.** A hook that could not get the lock in 50 ms appends without the size
  check, so a file can pass 16 MiB under heavy contention. Accepted by S-07 step 3.
- **An `ok` line exists only through the grep rule** when this feature is built on its stated needs alone.
  Check 8 uses that rule. If 004's grep rule changed, check 8 and `TestLobeLogCaps` need another `ok` event.
- **A plugin change ships by release** (`TestRootMarketplacePin`). Until the pin moves, only the checkout's
  plugin has the `SessionEnd` entry.
- **The trim of `stats/hook-errors.jsonl`** belongs to the detached pass. Until 018 the file only grows, by
  at most one short line per failed hook.
- **Session files.** The assignment for this feature named them, but S-07 and F-01 give no rule that
  expires `sessions/<session>.json`. None is invented here. If the owner wants one, it is a change to the
  requirements first.
