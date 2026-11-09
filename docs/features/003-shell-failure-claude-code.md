# 003: A failed shell command in Claude Code is logged

**Status** built and evaluated on Windows on 2026-11-09 (tests, plugin suite, tester brief 13 of 13, the owner's real-session checks); CI pending · **Stage** Sense · **Entries** F-01 (folder, lock, append, repository key), F-02 (engine,
client rule, deadline, watchdog, error file), F-03 (one Claude Code entry, the wrapper), S-01 (shell tools),
S-05 (all), S-06 (the event id only), S-07 (day file, append), F-07 (notices); D-11, D-13 · **Needs** 002 ·
**Feeds** 004, 005, 006, 007, 008, 010, 011, 012, 013

## Result

In a Claude Code session opened in a client's folder, a Bash or PowerShell call fails. One new line appears in
that client's event log, `$TQ_HOME/identities/<client>/lobe/events/events-YYYYMMDD.jsonl`. The line holds the
command and the error with every secret replaced by a marker. Nothing is printed into the session, nothing is
written for any other client, and the tool call is never slowed or stopped by the lobe.

The steps that show it, in the sandbox of feature 002 (the full tester brief is under Evaluation):

```bash
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
printf '%s' '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"'"$W"'/clients/acme/app",
"tool_name":"Bash","tool_use_id":"toolu_01","tool_input":{"command":"npm run buld"},
"error":"Exit code 1\nnpm error Missing script: buld"}' | tr -d '\n' |
  tentaqles lobe-hook claude post-tool-use-failure; echo "exit=$?"
cat "$TQ_HOME"/identities/acme/lobe/events/events-*.jsonl
```

The first command prints only `exit=0`. The second prints one JSON line with `"kind":"fail"`. In a real session
the owner sees the same line after a real failed command (Person checks).

## Scope

This is the first lobe code that runs in a session. It carries the foundations in their smallest working form.

**In**

- The store folder of one client: `store.Open`, `store.json`, the refusals, the store lock, the whole-line
  append and `store.RepoKey` (F-01 steps 1, 3, 4, 5 first paragraph, 6 first bullet, 7).
- The engine `tentaqles lobe-hook <host> <event>` for the host `claude`: the entry before cobra, the watchdog,
  the deadline, the 1 MiB read, the client rule, `stats/hook-errors.jsonl` (F-02 steps 1 to 5 and 7).
- One plugin entry, `PostToolUseFailure`, the wrapper `plugin/scripts/lobe_hook.sh`, and the changed pinned
  plugin test (F-03 steps 2, 3, 5 and 7; decision D-11).
- The capture of a failed call of the tools `Bash` and `PowerShell` (S-01 steps 1, 5, 6 first paragraph, 7).
- Secret scrubbing, the whole entry (S-05).
- The event id (S-06 step 1) and the day file with its append (S-07 steps 1, 3 and 5 first sentence, and the
  "exit 0 on any error" half of step 6).
- The root `LICENSE` (MIT), `NOTICE`, `third_party/licenses/` and `tentaqles version --notices` (F-07 step 6,
  decision D-13), because this feature holds the first ported code.
- Phase 0 question 2 for Claude Code and phase 0 question 3 (core page section 8), each with a written result.
- The rest of the small store API, because features from 014 on name it as coming from here: `store.Read`,
  `store.SessionFile`, `store.Replace` and `store.Update` (F-01 "How to build it"), and
  `tentaqles lobe selftest`, which runs the store's writer, replace and key checks from the binary (F-07).

**Out**

Each line is something this feature does not do yet, on purpose, and the feature that adds it.

- Any tool that is not `Bash` or `PowerShell`: the hook starts, sees the tool and writes nothing (004).
- An interrupted call and a call the person or a hook refused are recorded as failures here. The skip rules
  for `is_interrupt` and the host's refusal texts arrive in 004.
- The lobe's own tools are recorded here. The skip rule, and `internal/lobe/shellwords` that it needs, arrive
  in 004. So does the rule that a `grep` that found nothing is `ok` and not a failure.
- A linked git worktree is keyed by its own folder here. The key of its main repository arrives in 004.
- No setting is read. Capture is always on (005 adds `internal/lobe/settings` and the `capture` switch).
- No command shows the log, and `tentaqles doctor` says nothing about the lobe (005). The render step of
  `tentaqles reach` and `tentaqles settings render` does not open the store yet (005).
- No `fp` in the event (006).
- No duplicate check. The same payload sent twice writes two lines with the same `id` (007).
- No `ok` line for a success (008) and no `hidden-fail` (009).
- Codex: `tentaqles lobe-hook codex ...` exits 0 and does nothing (010).
- No expiry, no drops file, no size limits, no repair of a torn line (011).
- No output of any kind: the `Answer` type and `emit` of F-02 step 6 arrive with the first feature that prints
  (014).
- No detached pass: `hook.Detach` and `internal/lobe/pass` (018).
- `tentaqles lobe bench latency` and `stall` (012) and the leak suite (013). The house-rule tests below are
  born here; those two features attack the same rules from outside the process.
- `store.LockFile` (018) and `tentaqles lobe rebuild` (017 for lesson headers, 043 for the graph).

## What it builds on

- **001**: `resolve.Resolve` refuses a second folder with a client's name. The client rule below trusts it.
- **002**: `tentaqles lobe sandbox <folder>` and the standard tester setup (F-07), the `lobe` command group in
  `internal/cmd/lobe.go`, and `paths.LobeDir(client)`. If 002 left `paths.LobeDir` out, add it here.
- Existing tentaqles code, used and not changed: `resolve.Resolve(cwd, cfg)` (`internal/resolve/resolve.go`
  L48), `registry.Load()`, `paths.IdentityDir(ws, cli)` (`internal/paths/paths.go` L28), `testutil.HomeRun`
  (`internal/testutil/home.go` L18).
- Nothing else. This is the first lobe code under `internal/lobe/`.

## What it leaves for later features

| Name | What it is | Used by |
|---|---|---|
| `internal/lobe/store` | `Open(ws, mode)`, `Lock(ctx)`, `Append(rel, scrub.Clean)`, `Read`, `SessionFile`, `Replace`, `Update`, `RepoKey(clientDir, repoDir)`, `ErrBusy` | every later feature |
| `store.json` | `{"v":1,"client":"<name>","created":"<time>"}` | 005 (doctor), 013 |
| `internal/lobe/scrub` | `Input`, `Text`, `Detect`, the type `Clean` | 004, 008, 010, and every writer |
| `internal/lobe/eventlog` | `ID(host, session, callID)`, `Append(dir, e, now)`, the `Event` type | 005 (`Scan`, `Tail`), 007 (`Check`), 011 (`Sweep`, drops) |
| `internal/lobe/sense` | `FromClaudeFailure`, `Record(e) Outcome`, `CallText` for shell tools, the repository walk in `place.go` | 004, 006, 008, 009, 010, 023 |
| `internal/lobe/hook` | `Main`, the payload reader, the client rule, the step table, the cutoffs, `errors.go` | every feature that adds a hook step |
| `stats/hook-errors.jsonl` | five keys per line: `at`, `host`, `event`, `step`, `class` | 005 (`lobe-hook-failing`), 018 (the trim) |
| `plugin/scripts/lobe_hook.sh` | accepts all seven event names from this feature on | 008, 011, 014, 018, 026, 027 |
| The plugin test's list of lobe events | exactly `["PostToolUseFailure"]` here | each feature that adds an entry extends it |
| `TQ_LOBE_NO_HOOKS=1` | every lobe hook does nothing | 025, 061 |
| `LICENSE`, `NOTICE`, `third_party/licenses/`, the `// Ported from` header | the notice rule and its test | every feature that ports code |
| The phase 0 capture result | field names per event and call kind, for Claude Code | 004, 008, 011, 014, 026, 027 |

## How it works

1. **The entry in the plugin.** `plugin/hooks/hooks.json` gains one group, `PostToolUseFailure`, with no
   matcher: `bash "${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh" post-tool-use-failure`, `"timeout": 5`. The
   entry is in its final form: it fires for every tool, and the tool filter of this feature lives in Go, so 004
   changes no plugin file. The guard's two groups stay byte for byte (F-03 step 2).
2. **The wrapper.** `lobe_hook.sh` inverts `tq_hook.sh` (F-03 step 3). It exits 0 at once when
   `TQ_LOBE_NO_HOOKS` is `1`, when the event is not one of the seven names of core page 5.2 in their
   `lobe-hook` spelling, or when no binary is found. It finds the binary as `tq_hook.sh` does (L66 to L85). It
   then runs `<binary> lobe-hook claude <event>`, passes stdin and stdout through, drops stderr and exits 0
   whatever the status. It has no watchdog. Its file name must not contain `tq_hook.sh`.
3. **Before cobra.** In `cmd/tentaqles/main.go` and `cmd/tq/main.go`, after the stub and the shim checks and
   before `cmd.RunMain`, `main` asks `hook.Main`. When the first argument is `lobe-hook`, `Main` handles the
   run and returns its exit code, which is always 0. `Main` first arms a watchdog that calls `os.Exit(0)` at
   the event's cutoff plus 250 ms (proposed), defers a `recover`, and returns at once when `TQ_LOBE_NO_HOOKS`
   is `1` (F-02 step 1).
4. **Hosts and events.** The only host is `claude`. The seven event names are known, with the cutoffs of F-02
   step 2 (1.5 s for `post-tool-use-failure`). Only `post-tool-use-failure` has steps. Any other host or
   event, known or not, exits 0 with no output and no write.
5. **The work** runs in one goroutine with its own `recover` (F-02 step 3). It reads at most 1 MiB of stdin,
   checks UTF-8 and that the text is one JSON object, and reads `session_id`, `cwd`, `tool_name`,
   `tool_use_id`, `tool_input` and `error`. `tool_input` arrives as an object or as text that holds the
   object; the reader is rebuilt from `internal/cmd/claudehook.go` L353, not imported. With no `cwd` the
   process folder is used. `resolve.Resolve(cwd, cfg)` names the client. A neutral folder, an unreadable
   manifest or a registry that cannot be loaded ends the hook. `TQ_WS` and `TQ_WS_ROOT` decide nothing.
6. **The client rule** (core page 5.1, F-02 step 4), in full because every later feature stands on it:
   `CLAUDE_CONFIG_DIR` must name `paths.IdentityDir(client, "claude")`. Both paths are made absolute and
   cleaned, then compared whole, ignoring case on Windows; a trailing separator and mixed `/` and `\` make no
   difference; nothing is resolved through links. When the variable is unset, empty or names any other
   folder, the hook exits 0. By then it has read stdin, the registry, the folder's marker and the manifest,
   and nothing under any `lobe` folder. It has written nothing anywhere: no store, no event, no error line.
   `CODEX_HOME` is not looked at.
7. **The store** (F-01 steps 1, 3, 4). `store.Open(ws, ReadWrite)` creates
   `$TQ_HOME/identities/<client>/lobe` with mode 0700 and `store.json` with mode 0600 when they are absent.
   It refuses, and the hook ends, when `identities`, `identities/<client>` or `lobe` is a link or junction,
   when `store.json` does not parse or names another client, or when its `v` is newer than 1. Only `events/`
   and `stats/` are created under it in this feature, each on first use.
8. **The tool filter of this slice.** When `tool_name` is neither `Bash` nor `PowerShell`, nothing is
   written. 004 removes this one check.
9. **The event** (core page 5.4, S-01 step 5): `v` 1; `id` from `eventlog.ID` (S-06 step 1, both forms);
   `at` from the hook's UTC clock, to the second; `session` (empty when the payload has none); `host`
   `claude`; `tool`; `kind` `fail`. `input` is the compact JSON of `tool_input` with sorted keys, with `&`,
   `<` and `>` written as they are. A first line `Exit code N` in `error` sets `exit` to N and is removed
   from `error`. An empty `error` is left out. There is no `fp` and no `dup` yet.
10. **`repo` and `cwd`** (S-01 step 6, first paragraph). Walk up from the payload's `cwd` to the first folder
    that holds `.git`, never above the client folder. `repo` is `store.RepoKey(clientDir, repoDir)`: `r_` plus
    the first 8 hex characters of the SHA-256 of the repository's path relative to the client folder, written
    with `/`, `.` when the client folder is the repository, lower-cased on Windows (F-01 step 5, proposed).
    `cwd` is relative to the repository, `.` at its root. With no repository, `repo` is left out and `cwd` is
    relative to the client folder. No git command runs and no process is started.
11. **Scrubbing** (S-05 steps 1 to 4, complete). The key pass, the window, the ordered text pass and the final
    cut run on `input` and `error` before anything reaches the disk. `store.Append` takes only `scrub.Clean`,
    whose field is unexported, so no other package can hand the store raw text.
12. **The append** (S-07 steps 1, 3, 5; F-01 steps 6 and 7). The file is `events/events-YYYYMMDD.jsonl`,
    named by the UTC date of `at`. The hook tries the store lock every 5 ms for at most 50 ms (proposed), then
    appends with or without it: one `Write` of the whole line with its newline, on a file opened with
    `O_APPEND`, no fsync. The lock is an operating-system lock on the file `lock` (`LockFileEx` over its first
    byte on Windows, `flock` elsewhere), released by the system when the process dies.
13. **The outcome.** `sense.Record` returns `sense.Outcome` with `Session`, `ID`, `Tool`, `CallID`, `Call`,
    `Kind` and `State` (`full`, or `dropped` when the write failed). Nothing reads it yet. `Call` comes from
    `sense.CallText`, which for a shell tool is the command string, scrubbed and cut at 4000 characters.
14. **Failure** (F-02 step 7). On any error, panic or deadline the hook prints nothing and exits 0. When the
    store was already open, it adds one line to `stats/hook-errors.jsonl` with exactly the keys `at`, `host`,
    `event`, `step` and `class`. `step` is the entry id of the running step (`S-01`, `S-05`, `S-07`). `class`
    is one of `deadline`, `panic`, `busy`, `io`, `oversize`, `detach` or `error`. The line never holds payload
    text, a path, a tool name, a session id or the text of a Go error. At most one line per process, appended
    without the lock. Before the store is open nothing is written anywhere.
15. **Nothing is printed.** Stdout stays at 0 bytes on every path of this feature.

Edge cases this slice must hold: stdin that never closes (the read is inside the goroutine, the watchdog ends
the process); a payload over 1 MiB (nothing written); a missing `tool_use_id` (the id is hashed from host,
session, tool, kind, input and the clock in nanoseconds); a `cwd` that does not exist (resolve it as text; no
`repo` when the walk finds nothing); an `events` path that is a file (exit 0, class `io`).

## Where to read

| What | Where |
|---|---|
| Requirements | [F-01](../prd/lobe-foundation.md) steps 1, 3 to 7; [F-02](../prd/lobe-foundation.md) steps 1 to 5 and 7; [F-03](../prd/lobe-foundation.md) steps 1 to 3, 5, 7; [F-07](../prd/lobe-foundation.md) steps 4 and 6 |
| Requirements | [S-01](../prd/lobe-sense.md) steps 1, 2, 5, 6, 7 and "Where Sense ends"; [S-05](../prd/lobe-sense.md), all; [S-06](../prd/lobe-sense.md) step 1; [S-07](../prd/lobe-sense.md) steps 1, 3, 5, 6 |
| Shared rules | [core page](../prd/vertical-lobe.md) 5.1 (import rules, client rule), 5.2 (budgets), 5.3 (layout), 5.4 (event), section 7 (sources), section 8 (phase 0), D-11, D-13 |
| Test names that guard these paths | [M-06](../prd/lobe-benchmarks.md) "How we know it works"; [M-03](../prd/lobe-benchmarks.md) attacks L-04, L-05, L-06, L-12 |
| Source to port: failure hook | [`src/hooks/post-tool-failure.ts` L69](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L69) (any error exits 0), [`src/hooks/post-tool-failure.ts` L31](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L31) (L34 to L38: tool name, input, error) |
| Source to port: registration | [`plugin/hooks/hooks.json` L48](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/plugin/hooks/hooks.json#L48) (no matcher), [`src/claude/settings-merge.ts` L99](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/settings-merge.ts#L99) (timeout 5; L47: the `exit 0` fallback), [`src/claude/hooks.ts` L531](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hooks.ts#L531) (the no-hooks switch), [`src/claude/post-bash.ts` L4](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/post-bash.ts#L4) (a failed Bash call arrives on PostToolUseFailure) |
| Source to port: scrubbing | [`src/functions/privacy.ts` L30](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/privacy.ts#L30), [`src/hooks/_capture-filter.ts` L78](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture-filter.ts#L78), [`src/notes/secrets.ts` L30](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/secrets.ts#L30), [`src/trace/redact.ts` L83](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/redact.ts#L83); the line lists are in S-05's Source table |
| Weakness not to copy | [`src/hooks/_capture-filter.ts` L72](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture-filter.ts#L72): text at or under the cap is returned unscrubbed |
| Source to port: id and append | [`src/capture/event-id.ts` L66](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/event-id.ts#L66), [`src/capture/spool.ts` L348](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L348), [`src/capture/spool.ts` L202](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L202) (the wait loop); not taken: [`src/capture/spool.ts` L351](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L351) (fsync) |
| Ideas for the lock | [`src/capture/spool.ts` L183](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L183), [`src/util/state.ts` L203](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/util/state.ts#L203), [`src/skills/own-folder.ts` L35](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/own-folder.ts#L35) (refuses a linked folder) |
| Notice text | [`NOTICE` L1](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/NOTICE#L1); agentmemory ships no `NOTICE`, its copyright line is `LICENSE` L178 |
| Host facts (web, not machine-checked) | <https://code.claude.com/docs/en/hooks> |
| Existing tentaqles code | `cmd/tentaqles/main.go` L13 to L23 and `cmd/tq/main.go` (where `hook.Main` is asked); `internal/cmd/claudehook.go` L239 to L258 (the goroutine and the buffered answer, as a pattern), L270 to L283 (the `cwd` fallback), L353 (`toolInputCommand`); `internal/cmd/claudehook_main.go` L21 to L47 (the guard's watchdog, the contrast) |
| Existing plugin files | `plugin/hooks/hooks.json`; `plugin/scripts/tq_hook.sh` L25 to L32 (event check), L66 to L85 (finding the binary); `plugin/tests/test_tq_hook.py` L41 to L65 (helpers), L199 to L208 (the pinned test); `plugin/CLAUDE.md` |
| Existing tests to keep green, unedited | `internal/cmd/claudehook_main_test.go` (`TestHookDeadlinesUnderHarnessTimeouts`), `internal/guardplugin/guardhook.go` L267 (`preToolUseRunsGuard`), `test/integration/docs_test.go` L16 and L41, `internal/cmd/drop_test.go` |

## Build tasks

Tasks 4, 5 and 6 can run side by side once task 1 is done. Tasks 2 and 3 run side by side with them; task 2
waits on the owner. Tasks 7 to 10 are in order. Task 11 can start as soon as task 2 has its result.

1. **Check this brief against the code as it stands** and list what no longer fits: what 001 and 002 really
   left (`paths.LobeDir`, the `lobe` command group, whether the sandbox creates a store), the line numbers
   above, the plugin files. The owner approves changes to this file before code is written.
2. **Phase 0 question 2, Claude Code.** Write the capture scripts under `spike/hook-capture/`: a throwaway
   hook that appends its stdin to one file per event, a settings file that registers it for every Claude Code
   hook event inside a scratch folder of the client the owner names, and a reducer that turns the captured
   files into counts and field names with their JSON types, never values. The owner runs one session through
   this list (F-03 step 1, plus two calls from S-01's risks): a failed Bash command, a failed PowerShell
   command, a failed edit, an MCP error result, an unknown MCP tool, a call declined at the permission
   prompt, a call interrupted while running, a successful command, a prompt, a stop and a session end. The
   owner hands back the reducer's output only. Write the result to
   `docs/prd/spikes/phase0-q2-claude-code-hooks.md`: per call kind, the event that fired, its fields, whether
   `error` starts with an `Exit code N` line, and whether `tool_use_id` and `is_interrupt` are present. This
   feature builds only on the two failed shell rows; the other rows are for 004, 008, 011, 014, 026 and 027.
   Test fixtures are written from the field names with made-up values. No captured payload leaves the owner's
   machine. Check: the result file exists and the fixtures of task 9 match its field names.
3. **Phase 0 question 3.** Write the throwaway test: 20 processes append 500 lines each (proposed) to one
   file opened with `O_APPEND`, once with no lock and once with the store lock, on Windows (the dev box) and
   on Linux and macOS (CI). Record torn lines, lost lines and the 95th percentile of the lock wait. Write the
   result to `docs/prd/spikes/phase0-q3-log-append.md` with one sentence on what it means: if no line tears
   without the lock, the lock serves the duplicate filter of 007 only; if one tears, readers rely on the skip
   rule of 005 and the repair of 011. The test stays as `TestLobeLogAppendParallel`. Check: the result file,
   and the test green on three systems.
4. **License and notices.** Add the root `LICENSE` (MIT), `NOTICE` (Trail-code, agentmemory, Graphify and
   Graft with their copyright lines, read at the commits of core page section 7; BASE is not listed) and
   `third_party/licenses/<project>/` with each license text. Add the flag `tentaqles version --notices`. Add
   the three paths to the archives in `.goreleaser.yaml`. Files: the three paths, `internal/cmd/version.go`,
   `.goreleaser.yaml`. `go:embed` cannot reach above its own folder, so the embedding package sits at the
   repository root or the texts are copied by a generator that a test checks. Tests: `TestRootLicenseIsMIT`,
   `TestNoticeListsEveryPort`, and `TestGoreleaserBuildsOneProgramUnderTwoNames` unedited.
5. **`internal/lobe/scrub`**: `scrub.go`, `rules.go`, `keys.go`, `cut.go` (S-05 "How to build it"). The rule
   table is built on first use. Tests: the four `TestLobeScrub*` tests.
6. **`internal/lobe/store`**: `store.go`, `lock_windows.go`, `lock_unix.go`, `append.go`, `repokey.go`. It
   imports `internal/paths` and nothing else of tentaqles. `ErrBusy` is a constant of a string type. Needs
   the `Clean` type of task 5 (agree on it first). Tests: `TestLobeStoreTwentyWriters`,
   `TestLobeLockDiesWithItsHolder`, `TestLobeStoreRefusesLinkAndForeign`, `TestLobeRepoKey`. Also
   `replace.go` (`Read`, `SessionFile`, `Replace`, `Update`) with `TestLobeReplaceUnderReader` and
   `TestLobeBusyReplaceWritesNothing`, and `tentaqles lobe selftest` in `internal/cmd/lobe.go` with its
   section in the command reference (F-01 "How we know it works" and tester check 2).
7. **`internal/lobe/eventlog`**: `append.go` (`Append(dir string, e Event, now time.Time) error`) and
   `dedupe.go` with `ID` only. Tests: `TestLobeEventIDVector`, `TestLobeLogAppendParallel`,
   `TestLobeLogLockHeld`, `TestLobeLogDiskFull`.
8. **`internal/lobe/sense`**: `claude.go` (`FromClaudeFailure`), `place.go`, `calltext.go`, `sense.go`
   (`Record`). Tests: `TestLobeSenseFailureEveryTool` (the shell rows), `TestLobeSenseStartsNoProcess`,
   `TestLobeSenseOutcome`.
9. **`internal/lobe/hook`**: `main.go`, `payload.go`, `client.go`, `steps.go`, `errors.go`; the call in the
   two `main.go` files; a `lobe-hook` cobra command in `internal/cmd/lobe.go` with the subcommand `claude`
   that calls the same `Main`, for help and docs. Tests: the `TestLobeHook*` tests,
   `TestLobeCaptureSurvivesALateStep`, `TestLobeNoHooksSwitch`, `TestLobeStall_SpinningHandler`,
   `TestLobeSenseFailureSkips` (four rows).
10. **Import rules.** `test/integration`: `TestLobeGuardNeverImportsLobe` and `TestLobeImportsOnlyTheFour`
    (F-02 "How to build it"). Also `TestLobeStall_GuardStillDenies`.
11. **The plugin.** `plugin/scripts/lobe_hook.sh`, the new group in `plugin/hooks/hooks.json`, the changed
    `test_the_plugin_holds_only_the_guard_hook`, the five new `test_lobe_hook_*` tests, `plugin/CLAUDE.md`.
    Go side: `TestLobeHookTimeoutsRegistered`, `TestLobeHookCutoffsUnderRegisteredTimeout`. Check: the plugin
    suite, and a diff that shows the guard's two groups unchanged.
12. **Identity code, two small edits.** `internal/cmd/drop.go` L163: the confirmation line of the identity
    folder also names the lobe's memory. The provider loader rejects a provider with the id `lobe`, because
    `paths.IdentityDir(ws, "lobe")` is the store. Tests: `TestDrop_RemovesTheLobe`, a new test in
    `internal/providers` (name proposed here: `TestProviders_RejectsTheIdLobe`), and every `TestDrop_*` test
    unedited.
13. **Docs.** `docs/reference/commands.md`: a section `## tentaqles lobe-hook claude`, and the new flag in
    `## tentaqles version`; `lobe-hook` joins `documentedGroups` in `test/integration/docs_test.go`.
    `docs/reference/manifest.md`: the `lobe` folder under "`~/.tentaqles`" and `TQ_LOBE_NO_HOOKS` under
    "Environment variables". `CHANGELOG.md` and `plugin/CHANGELOG.md` under `[Unreleased]`. `CLAUDE.md`:
    the new packages under Layout, and four lines under "Must not trade off" (see Done when).
    `docs/RELEASING.md` if the archive contents are listed there.
14. **The tester run** (Evaluation), by a fresh agent, then the owner's Person checks.

## Tests that stay in the repository

Names are the requirements' unless marked. Counts are for this slice; a later feature that widens a test says so.

**Store (F-01)**

- `TestLobeStoreTwentyWriters`: 20 processes, 500 lines each, with the 5 s wait: 10,000 lines, 0 torn, 0 lost.
- `TestLobeLockDiesWithItsHolder`: after the holder is killed, the next `Lock` succeeds in under 100 ms
  (proposed).
- `TestLobeStoreRefusesLinkAndForeign`: a junction, a copied `store.json` and a newer `v` are refused. 3 of 3.
- `TestLobeRepoKey`: `app` gives `r_a172cedc`; `.` gives `r_cdb4ee2a`; `etl\load` and `etl/load` give one
  key; on Windows `App` and `app` give one key. 4 of 4 here; 004 adds the worktree row.
- `TestDrop_RemovesTheLobe`: after drop, `identities/<client>` is gone; `TestDrop_UndoesTheClient` unchanged.

**Engine (F-02, F-03)**

- `TestLobeHookAlwaysExitsZero`: 12 bad inputs (empty, not JSON, invalid UTF-8, over 1 MiB, unknown host,
  unknown event, neutral folder, unreadable manifest, panicking step, 10 s step, held lock, stdin never
  closed) give exit 0 and 0 bytes of stdout in 12 of 12, each within 1.75 s.
- `TestLobeCaptureSurvivesALateStep`: a test step that sleeps past the cutoff, placed after the append on
  `post-tool-use-failure`, leaves the event line in the log, exit 0 and 0 bytes of stdout.
- `TestLobeHookErrorsHoldNoContent`: 7 forced failures, one per class, through a test step, with a payload
  full of marker strings; each leaves one line with exactly the five keys and 0 marker strings in `stats/`.
- `TestLobeNoHooksSwitch`: with `TQ_LOBE_NO_HOOKS=1`, 7 of 7 events write 0 bytes to the store and stdout.
- `TestLobeHookTimeoutsRegistered`: every lobe entry in `hooks.json` has timeout 5; the guard's have 15.
- Plugin: `test_lobe_hook_always_exits_zero` (statuses 1, 2, 3, 134, 137: 5 of 5 give exit 0 and empty
  stdout), `test_lobe_hook_missing_binary`, `test_lobe_hook_unknown_event`, `test_lobe_hook_no_hooks_switch`,
  `test_lobe_hook_passes_stdin_and_stdout`.
- Plugin, changed with the owner's yes (D-11): `test_the_plugin_holds_only_the_guard_hook` asserts that the
  guard's two entries are present and unchanged (timeout 15, the same command text), that every other entry
  runs `lobe_hook.sh <event>` with timeout 5, and that the lobe's events are exactly `PostToolUseFailure`.
  The seven events of F-03 are the most the list may ever hold. No other pinned plugin test is edited.

**Capture (S-01, S-06, S-07)**

- `TestLobeSenseFailureEveryTool`: failures for `Bash` and `PowerShell` write 2 lines with `kind` `fail`; a
  failure for `Edit` writes 0. 004 turns this into the requirements' 6 lines.
- `TestLobeSenseFailureSkips`: bad JSON, no client, another client's login folder, no login variable: 0 lines
  in any store, exit 0 and empty stdout in 4 of 4. 004 adds interrupt and refusal; 005 adds `capture: off`.
- `TestLobeSenseStartsNoProcess`: with an empty `PATH` the hook still writes its line.
- `TestLobeSenseOutcome`: the outcome's `ID` and `Kind` equal the written line's, the line can be read as soon
  as `Record` returns, and `CallText` gives one text for the payloads before and after a shell call.
- `TestLobeEventIDVector`: `ID("claude", "s1", "toolu_01")` is `evt_9bde2e09`.
- `TestLobeLogAppendParallel` (also the phase 0 test): 20 processes append 500 lines each; 10,000 lines,
  10,000 parse, 0 torn, on Linux, macOS and Windows.
- `TestLobeLogLockHeld`: with the lock held elsewhere for 2 seconds, `Append` returns in under 100 ms
  (proposed) and writes.
- `TestLobeLogDiskFull`: with a write error injected, the hook exits 0 with empty stdout; the file is
  unchanged.

**House rules, born here and kept green by every later feature**

- *A hook never stalls*: `TestLobeHookAlwaysExitsZero`, `TestLobeCaptureSurvivesALateStep`,
  `TestLobeLogLockHeld`, `TestLobeLogDiskFull`, the plugin's `test_lobe_hook_always_exits_zero`, and from
  M-06 `TestLobeStall_SpinningHandler` (a child of the test binary loops forever in the handler; the watchdog
  ends it with exit 0 and no output by the cutoff plus 500 ms) and `TestLobeHookCutoffsUnderRegisteredTimeout`
  (every cutoff plus slack is under 5 seconds). M-06's other stall tests arrive in 012 with the bench.
- *The wrong client gets nothing*: `TestLobeHookServesOnlyItsLogin` (the folder is acme's and the login names
  globex's identity folder, for each of Claude Code's 7 events: exit 0, 0 bytes of stdout, both clients'
  store trees byte-identical before and after; 7 of 7 here, 13 of 13 once 010 adds Codex's 6),
  `TestLobeHookLoginUnsetDoesNothing` (unset, empty, naming `~/.claude`: 3 of 3 leave both trees unchanged
  and create no store; 010 adds the Codex form), `TestLobeHookLoginPathForms` (a trailing separator, mixed
  `\` and `/`, another case on Windows, and the plain path as control, which writes one line: 4 of 4),
  `TestLobeStoreRefusesLinkAndForeign`. M-03's `TestLobeLeak_*` tests arrive in 013 and attack the same rule
  from outside (L-04, L-05, L-06, L-12).
- *No secret on disk*: `TestLobeScrubParity` (0 misses on rows that `test/trail-secrets.test.ts` and
  `test/privacy.test.ts` expect caught), `TestLobeScrubShortValue` (`password=hunter2` becomes
  `password=[redacted:named]`), `TestLobeScrubNoFalseAlarm` (a 40 character git hash, a UUID,
  `MAX_TOKENS=4096`, `$TOKEN` and a file path are unchanged, 5 of 5; both fields at full window size in under
  5 ms, proposed), `TestLobeScrubWindowEdge`, and `TestLobeHookErrorsHoldNoContent`.
- *The four import rules*: `TestLobeGuardNeverImportsLobe` (0 lobe packages reachable from
  `internal/guard`, `internal/cmd/claudehook.go`, `claudehook_main.go` and `codexhook.go`; 0 lobe `init`
  functions and 0 package-level calls) and `TestLobeImportsOnlyTheFour` (no lobe package imports a tentaqles
  package outside `internal/resolve`, `internal/paths`, `internal/registry` and `internal/manifest`).
- *The guard is untouched*: every test under "Must not trade off" in `CLAUDE.md` passes unedited, among them
  `TestHookDeadlinesUnderHarnessTimeouts`, `TestCanonicalSuite`, the `TestRun_*` hook tests and the plugin's
  `test_tq_hook_*` tests; and `TestLobeStall_GuardStillDenies` (with the lobe's store lock held and never
  released, the guard still denies a wrong-identity `git push`).
- *Sources*: `TestNoticeListsEveryPort` (0 ported files without a `NOTICE` entry, 0 headers that name BASE),
  `TestRootLicenseIsMIT`. *Docs*: `TestDocsReferenceEveryCommand`.

## Evaluation

The tester is a fresh agent with the built program called by its full path (`$T`; read every `tentaqles` below as
`$T`, because in an activated shell `tentaqles` is a shell function that runs the installed program), an empty
working folder, and the repository's `plugin` folder to read (`PLUGIN` is its path). Use bash (Git Bash on Windows). No network. Run
the standard setup of feature 002 first:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
```

Stop and report FAIL when a line starts with `SETUP FAIL`. Then, staying in `$W`:

```bash
ID="$TQ_HOME/identities"; ACME="$W/clients/acme"; APP="$ACME/app"; mkdir -p "$APP/src/etl"
EVENTS="$ID/acme/lobe/events"; LOG="$EVENTS/events-$(date -u +%Y%m%d).jsonl"
F="tentaqles lobe-hook claude post-tool-use-failure"
export CLAUDE_CONFIG_DIR="$ID/acme/claude"     # the login: without it a lobe hook does nothing
ev() {  # ev <session> <cwd> <tool> <tool_use_id> <command> <error>
  printf '{"session_id":"%s","hook_event_name":"PostToolUseFailure","cwd":"%s","tool_name":"%s",' "$1" "$2" "$3"
  printf '"tool_use_id":"%s","tool_input":{"command":"%s"},"error":"%s"}' "$4" "$5" "$6"
}
rep() { printf "%.0s$1" $(seq 1 "$2"); }       # rep x 1000 prints x 1000 times
snap() { (cd "$ID" && find . -type f -exec cksum {} + | sort -k3); }
n() { wc -l < "$LOG" | tr -d ' '; }
```

"Line k" means line k of `LOG`, read as JSON. `input` is a JSON string whose text is itself compact JSON.
Every hook call must print nothing on stdout and exit 0; say so when one does not.

1. **Bad input fails open.** `printf 'not json' | $F; echo "exit=$?"` prints only `exit=0`.
   `tentaqles lobe-hook cursor bogus </dev/null; echo "exit=$?"` prints only `exit=0`.
   `SECONDS=0; $F < <(sleep 10); echo "exit=$? s=$SECONDS"` prints `exit=0` and `s` of 2 or less.
   `ls "$EVENTS" 2>/dev/null | wc -l` prints `0`.
2. **The line.** Run these four:
   `ev s1 "$APP" Bash toolu_01 'npm run buld' 'Exit code 1\nnpm error Missing script: buld' | $F`,
   `ev s1 "$APP/src/etl" Bash toolu_02 'python load.py' 'Exit code 2\nNo such file' | $F`,
   `ev s1 "$ACME" Bash toolu_03 'make all' 'make: nothing to do' | $F`,
   `ev s1 "$APP" PowerShell toolu_04 'gti .' 'Exit code 1\ngti is not recognized' | $F`.
   `n` prints `4`. Line 1 has `v` 1, `id` `evt_9bde2e09`, `session` `s1`, `host` `claude`, `tool` `Bash`,
   `kind` `fail`, `repo` `r_a172cedc`, `cwd` `.`, `input` `{"command":"npm run buld"}`, `error`
   `npm error Missing script: buld`, `exit` 1, an `at` that ends in `Z`, and no `fp` and no `dup`. Line 2 has
   `id` `evt_8618533e`, `cwd` `src/etl`, the same `repo` and `exit` 2. Line 3 has `id` `evt_7fe44cf7`, `cwd`
   `.`, no `repo` and no `exit`. Line 4 has `tool` `PowerShell` and `id` `evt_20ed2862`.
3. **Secrets.** `ev s1 "$APP" Bash toolu_05 'curl -H \"Authorization: Bearer
   abcdefghijklmnopqrstuvwxyz012345\" https://api.example.test/v1/items?api_key=Zx81kQpL0029aaBB'
   'password=hunter2 rejected for postgres://app:s3cretPW@db.example.test:5432/main' | $F` (one line, a
   space where the line breaks here). `n` prints `5`.
   `grep -r -l -E 'abcdefghijklmnopqrstuvwxyz012345|Zx81kQpL0029aaBB|hunter2|s3cretPW' "$ID/acme/lobe" | wc -l`
   prints `0`, and `grep -o '\[redacted:' "$LOG" | wc -l` prints 4 or more. (The search covers the memory folder
   only: the identity folder also holds the PATH-mask stubs, which are copies of the program, and a program
   holds the alphabet constant that the first marker string is a part of.)
4. **A secret under a key, and sorted keys.** Pipe to `$F`, with `APP` replaced:
   `{"session_id":"s1","cwd":"APP","tool_name":"Bash","tool_use_id":"toolu_06",
   "tool_input":{"password":"hunter2","note":"hi","command":"deploy"},"error":"nope"}`.
   Line 6 has `input` `{"command":"deploy","note":"hi","password":"[redacted:key]"}`.
5. **No false alarm.** `ev s1 "$APP" Bash toolu_07 'git show 3f2a9c1e5b7d4a6f8e0c1b2d3a4f5e6d7c8b9a0f &&
   echo MAX_TOKENS=4096' 'fatal: bad object' | $F` (one line). Line 7 has an `input` that holds that command
   unchanged: `grep -c 'a0f && echo MAX_TOKENS=4096' "$LOG"` prints `1`.
6. **The window.** `E1="$(rep x 1000) ghp_$(rep A 36) $(rep x 5500)"`, then
   `ev s1 "$APP" Bash toolu_08 'true' "$E1" | $F`. Line 8 has an `error` of at most 4000 characters that
   holds `[...]` and `[redacted:api-key]` and no `ghp_`. Then
   `E2="$(for i in $(seq 1 15); do printf 'ghp_%s ' "$(rep A 36)"; done)$(rep x 1124) ghp_$(rep B 36)$(rep x 5000)"`
   and `ev s1 "$APP" Bash toolu_09 'true' "$E2" | $F`. Line 9 has an `error` that holds neither `ghp_` nor
   `BBBB`. `n` prints `9`.
7. **The wrong client gets nothing.** `snap > s1.txt`. Then, each printing only `exit=0`:
   `ev s2 "$APP" Bash toolu_10 'x --globex' 'no' | CLAUDE_CONFIG_DIR="$ID/globex/claude" $F; echo "exit=$?"`,
   `ev s2 "$APP" Bash toolu_11 'x --nologin' 'no' | env -u CLAUDE_CONFIG_DIR $F; echo "exit=$?"`,
   `ev s2 "$W" Bash toolu_12 'x --neutral' 'no' | $F; echo "exit=$?"`,
   `ev s2 "$W/clients/globex/app" Bash toolu_13 'x --other' 'no' | $F; echo "exit=$?"`.
   `snap > s2.txt; cmp s1.txt s2.txt` is silent: nothing was written in either client's folder.
8. **What is not captured yet, and the switch.** `ev s1 "$APP" Edit toolu_14 'x' 'String to replace not found' | $F`
   and `ev s1 "$APP" Bash toolu_15 'x --off' 'no' | TQ_LOBE_NO_HOOKS=1 $F` and
   `printf '{}' | tentaqles lobe-hook claude pre-tool-use` each print nothing. `n` still prints `9`.
9. **Twenty at once.** Run the loop below. `n` prints `29`, and every line of `LOG` parses as JSON and ends
   with a line break.

   ```bash
   for i in $(seq 1 20); do ev s9 "$APP" Bash "toolu_p$i" "false $i" 'Exit code 1' | $F & done; wait
   ```
10. **The plugin.** `J="$PLUGIN/hooks/hooks.json"; K="$PLUGIN/scripts/lobe_hook.sh"`.
    `grep -c lobe_hook.sh "$J"` prints `1`; with `tq_hook.sh` it prints `2`; with `'"timeout": 15'` it prints
    `2`; with `'"timeout": 5'` it prints `1`. `printf '{}' | bash "$K" bogus; echo "exit=$?"` prints only
    `exit=0`. A binary that fails: `mkdir "$W/fake"`, then
    `printf '#!/bin/sh\ncat >/dev/null\necho boom >&2\nexit 2\n' > "$W/fake/tq"; chmod +x "$W/fake/tq"`, then
    `printf '{}' | PATH="$W/fake:$PATH" bash "$K" post-tool-use-failure; echo "exit=$?"` prints only `exit=0`.
    The real binary under the wrapper: `mkdir "$W/bin"; cp "$T" "$W/bin/tq"; cp "$T" "$W/bin/tq.exe"`, then
    `BIN="$(cd "$W/bin" && pwd)"` (a POSIX path: a `C:/...` entry on `PATH` is split at its colon), then
    `ev s1 "$APP" Bash toolu_16 'x --wrapped' 'no' | PATH="$BIN:$PATH" bash "$K" post-tool-use-failure`
    prints nothing, and `n` prints `30`.
11. **The guard is unchanged.** `printf 'not json' | tentaqles claude-hook pre-tool-use; echo "exit=$?"` prints
    a `BLOCKED` line and ends with `exit=2`.
12. **Notices.** `tentaqles version --notices | grep -c -E 'Trail|agentmemory|Graphify|Graft'` prints 4 or
    more, and `tentaqles version --notices | grep -c 'MIT License'` prints 1 or more.
13. **A write error fails open, and the error file holds no content.** Run this check last.
    `rm -rf "$EVENTS"; : > "$EVENTS"`, then `ev s1 "$APP" Bash toolu_17 'npm run buld' 'hunter2 no' | $F;
    echo "exit=$?"` prints only `exit=0`, and `test -f "$EVENTS" && test ! -s "$EVENTS"` is true. The file
    `$ID/acme/lobe/stats/hook-errors.jsonl` has 1 line, with exactly the keys `at`, `host`, `event`, `step`
    and `class`, and `class` is `io`. `grep -c -E 'npm|buld|hunter2|toolu' "$ID"/acme/lobe/stats/*` prints `0`.

**Person checks**, for the owner:

1. The phase 0 capture of build task 2 is run by the owner, in one client they name.
2. A real session. With this build first on `PATH` and the checkout's `plugin` folder loaded as the plugin
   (for example `claude --plugin-dir <checkout>/plugin`, if the installed Claude Code has that flag), open
   Claude Code through tentaqles in a client's folder. Ask it to run `ls /no/such/folder`. Expect one new
   line in that client's `lobe/events/events-<today>.jsonl` with `"kind":"fail"`, the session to behave as
   before, and no new file under any other client's `identities/<name>/lobe`.
3. In that session ask for a command that prints a made-up token and fails, such as
   `echo ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA; false`. Expect the log to hold `[redacted:` and no `ghp_`.

**Scoring.** PASS needs 13 of 13. "Could not run" is a FAIL. Report anything surprising; fix nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI on three
  systems: `go vet ./...`, `go test ./... -count=1`, `go build ./...`, and the plugin suite
  (`PYTHONPATH=plugin python -m pytest plugin/tests -q`).
- [ ] The two phase 0 results are written under `docs/prd/spikes/` and the owner has read them.
- [ ] The tester brief passes, 13 of 13.
- [ ] The house rules hold: never stalls (`TestLobeHookAlwaysExitsZero`, `TestLobeStall_SpinningHandler`,
  `TestLobeHookCutoffsUnderRegisteredTimeout`), wrong client gets nothing (`TestLobeHookServesOnlyItsLogin`,
  `TestLobeHookLoginUnsetDoesNothing`), no secret on disk (`TestLobeScrub*`,
  `TestLobeHookErrorsHoldNoContent`), import rules (`TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`), the guard's tests unedited, `CGO_ENABLED=0`.
- [ ] The guard's two entries in `plugin/hooks/hooks.json` are byte-identical before and after.
- [ ] `CLAUDE.md` has four new "Must not trade off" lines with the tests above: a lobe hook never stalls and
  fails open; a lobe hook serves only the client that both the folder and the login name; no secret reaches
  the lobe's store; the guard never imports or runs lobe code.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md`, `plugin/CHANGELOG.md` and `plugin/CLAUDE.md` are updated.
- [ ] The owner has seen a real failed command of their own land in the log (Person check 2).

## Risks and unknowns

- **Which event and fields arrive** for a failed shell call is from the host's page (web, not
  machine-checked) and from Trail-code's reading. Task 2 settles it. If a failed Bash call does not arrive
  on `PostToolUseFailure`, or carries no `error` text, stop: this file and S-01 change first.
- **The `Exit code N` first line** is how both sources read the exit code. If the capture shows another
  form, step 9 changes and so do checks 2 and 9.
- **Torn lines on Windows** are assumed absent until task 3 says so (F-01 and S-07 risks). The 50 ms lock
  wait is a guess; 012 measures it.
- **Process start on Windows** through `bash` and the wrapper may alone use much of the 150 ms capture
  budget (M-06 risk). This feature holds only the cutoff; 012 measures the budget. The fallback of F-03's
  risks, a direct command with `|| exit 0`, would change `hooks.json` and the plugin test again.
- **A plugin change ships by release**: the new entry reaches an installed plugin only after the tag and
  sha pin move (`TestRootMarketplacePin`, `docs/RELEASING.md`). Until then only the checkout's plugin has
  it, which is what Person check 2 loads. Whether `--plugin-dir` exists on the owner's version is unverified.
- **The archives gain files.** The installers under `installers/` unpack them; check that extra files do
  not break the Windows swap of 0.14.1.
- **`NOTICE` lists four projects** as F-07 step 6 says, although this feature ports from two (agentmemory
  and Trail-code). Graft's copyright line comes from the owner's local copy (core page section 7).
- **Small store parts.** `Read`, `SessionFile`, `Replace`, `Update` and `tentaqles lobe selftest` are built
  here (task 6), because features from 014 on name them as coming from this feature. `store.LockFile`
  arrives in 018, `tentaqles lobe rebuild` in 017 (lesson headers) and 043 (the graph), and the
  cache-format half of the F-01 spike with the first index (017).
- **Thin on purpose**: until 004 an interrupted or refused shell call is logged as a failure, and until 007
  a replayed payload is logged twice. Nothing reads the log before 005, and nothing learns from it before
  018, so neither can mislead a lesson.
- **A session started outside tentaqles**, with no `CLAUDE_CONFIG_DIR`, gets no capture and no notice.
  `tentaqles lobe status` says why from 005 on.

## Result of the build and the tester run

Built on 2026-11-09 by Haiku builders from written briefs, one piece at a time, each read and rerun by the main
session before the next started: capture scripts, license and notices, the scrubber (three passes), the store
(one fix round), the event log, the capture, the hook engine, then the plugin entry, the import-rule tests and
the commands in parallel. Two phase 0 results are written: question 3 in `docs/prd/spikes/phase0-q3-log-append.md`
(no torn line on Windows, with or without the lock); question 2 waits for the owner's capture session.

- Tests: the whole repository is green on Windows (`go vet`, `go build`, `go test ./...`, 31 packages) and the
  plugin suite passes (34 tests). No existing test was edited except the two designed to grow
  (`test_the_plugin_holds_only_the_guard_hook`, with the owner's yes of D-11, and `TestLobeCommandTable`).
- Tester brief: 13 of 13 on Windows 11 in Git Bash, against a program built from the working tree. Two checks
  were corrected in this file after the run, not in the product: check 3 searched the whole identity folder and
  hit the PATH-mask stubs, which are copies of the program; check 10 put a `C:/...` path on `PATH`, which Git
  Bash splits at the colon, so the wrapper found the installed program instead of the built one.
- Measured on this machine: the hook answers bad input in under 5 ms, a step that ignores its deadline is cut at
  the deadline, twenty hooks at once leave a well-formed log, and the scrubber takes about 6 ms for a full-size
  input and error (target 5 ms, proposed).

Built against the documentation, to be confirmed by the owner's capture: the payload fields `session_id`, `cwd`,
`tool_name`, `tool_use_id`, `tool_input`, `error` and `is_interrupt`, and the `Exit code N` first line, are what
Claude Code's hooks page documents for `PostToolUseFailure`. The capture session (Person check 1) confirms them on
the owner's version; a difference changes the fixtures and the reader before this feature is closed.

Person checks 2 and 3: done by the owner on 2026-11-09 in a real Claude Code session in the `yduqs` client, with the
checkout's plugin loaded through `claude --plugin-dir` and the development build answering the hook. `ls
/no/such/folder-xyz` wrote one `fail` line with `exit` 2 and the error text without its `Exit code` line;
`echo ghp_A...; false` wrote a line whose command reads `echo [redacted:api-key]; false` and whose error is
`[redacted:api-key]`; no `ghp_` is on disk under any memory folder; the four other clients have no memory
folder; no error line was written. The phase 0 question 2 page records what these runs confirmed
(`docs/prd/spikes/phase0-q2-claude-code-hooks.md`).

Left open: CI on Linux and macOS; the owner's capture session (Person check 1), which confirms the fields of the
other events for the features that need them.
