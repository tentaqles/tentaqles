# 063: A call Claude Code rejected before it ran is logged

**Status** not started · **Stage** Sense · **Entries** S-01 (the gap it leaves) · **Needs** 004 · **Feeds** 005,
018, 035

Added on 2026-11-09, after the index was cut, on the owner's word. It is built right after 004.

## Result

Claude Code checks some calls before it runs them, and a call that fails that check fires no hook at all. The
owner's capture session proved it for `Edit`: when `old_string` is not in the file, there is no `PreToolUse`,
no `PostToolUse` and no `PostToolUseFailure`; the only trace is a `tool_result` with `is_error` true whose text
is wrapped in `<tool_use_error>` in the session's transcript
([phase 0 result](../prd/spikes/phase0-q2-claude-code-hooks.md)). Such a call now lands in the client's event
log like any other failure, when the agent finishes its turn.

From the sandbox of [002](002-sandbox.md), with acme's login exported, `ACME` set to acme's folder and a
transcript `T` under acme's login folder that holds an `Edit` call and its `<tool_use_error>` result:

```bash
printf '{"session_id":"s1","hook_event_name":"Stop","cwd":"%s","transcript_path":"%s"}' "$ACME" "$T" |
  tentaqles lobe-hook claude stop
```

prints nothing and adds one line with `"tool":"Edit"`, `"kind":"fail"`, the edit's input and the error
`String to replace not found in file.` and what follows it, with no `exit`.

## Scope

**In**

- The plugin's `Stop` entry: `bash "${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh" stop`, timeout 5. The owner's
  yes to changing `test_the_plugin_holds_only_the_guard_hook` for memory hooks is D-11; this adds `Stop` to its
  list.
- `transcript_path` read from the payload.
- `internal/lobe/sense/transcript.go`: `Rejected(data []byte) []Call`, pure: no disk, no clock.
- `internal/lobe/hook/rejected.go`: the `stop` step: the path check, the cursor, the bounded read, the
  duplicate check, `Record`.
- `store.CursorFile(session)`: `cursors/<name>.json`, named as `SessionFile` names its file.

**Out**

- Subagent transcripts (Claude Code keeps them in separate files and has `SubagentStop`). A recorded limit.
- Codex ([010](010-shell-failure-codex.md)).
- Failed calls whose hook did fire: 003 and 004 record those, and this feature never records a result without
  the `<tool_use_error>` wrapper.
- Old transcripts and whole-history mining ([035](035-backfill.md)).

## How it works

1. **When.** On `Stop`, which fires each time the agent ends its turn. The hook contract's `stop` event and its
   capture cutoff (1 s) already exist in `hook/steps.go`.
2. **Who.** The client rule of 003 holds unchanged: the payload's `cwd` resolves to a client and
   `CLAUDE_CONFIG_DIR` is that client's own login folder. Otherwise nothing is read and nothing is written.
3. **Which file.** `transcript_path`, made absolute and clean, must lie inside `<CLAUDE_CONFIG_DIR>/projects/`
   (compared as `loginMatches` compares, case-insensitive on Windows). Every component from the login folder
   down is checked with `Lstat`, in order: the login folder, `projects`, each folder below it, then the file.
   Each folder must be a directory and neither a symlink nor `ModeIrregular` (Go reports a Windows junction as
   irregular); the last component must be a regular file. Any other path, or a link or junction anywhere on the
   way: nothing is read. The file that is opened must be the one that was checked (`f.Stat()` and `os.SameFile`
   against the `Lstat` of the file); otherwise nothing is read. The memory never reads a file outside the
   session's own login folder.
4. **The cursor.** `cursors/<name>.json` in the client's store holds `{"v":1,"offset":N,"head":"<hex>"}`: the
   byte offset after the last whole line already read, and the hex SHA-256 of the file's first min(4096, N)
   bytes, bytes the cursor has already passed. The cursor is used only when its offset is not past the end of
   the file and the same bytes of the file still give its `head`. Otherwise (no cursor, no `head` as an older cursor has, a different head, or an offset past the
   end: the file was replaced) reading starts at `max(0, size - 8 MiB)`, at the first line start at or after
   that point. At most 8 MiB is read per run. A line longer than the 8 MiB window has no break inside the window:
   the cursor moves on by one window with no line read, so the long line is skipped a window at a time, and its
   tail, a partial line that does not parse, is skipped too. Only whole lines (ending in `\n`) count, and the
   cursor moves to the end of the last whole line read. A part line at the end is read on a later run.
5. **What counts** (`sense.RejectedWithContext`). Every JSON line whose `message.content` is a list. A `tool_use`
   block gives an id, a `name` and an `input`. A `tool_result` block counts when `is_error` is true and its
   content (a string, or a list whose `text` parts are joined by line breaks), with leading space trimmed,
   starts with `<tool_use_error>`. Its error is the text with the opening `<tool_use_error>` and a closing
   `</tool_use_error>` removed and the ends trimmed. Its tool and input are the `tool_use` with the same id, in
   the chunk read or in the look-back: up to 1 MiB of bytes before the chunk, cut to start after its first line
   break, so only whole lines are read. The look-back is used for `tool_use` blocks only; a rejected result in
   it is never counted. If no `tool_use` is found, the tool is `unknown` and the input `{}`. The same
   `tool_use_id` counts once, however many lines repeat it (the transcript repeats blocks in `api-request-blob`
   lines). A line that does not parse, or is longer than 1 MiB, is skipped. At most 200 calls per run, in file
   order; when more are found, the cursor moves to the end of the line that holds the 200th call, and the next
   run reads the rest (step 4).
6. **The skip rules of 004** (`sense.Skip`) apply to each call: the memory's own tool and a refusal are not
   recorded.
7. **Already logged.** The event id is `eventlog.ID("claude", session, tool_use_id)`, the same id 004 gives a
   hook-reported failure. The ids of today's and yesterday's day files (UTC) are read into one set, each line's
   top-level `"id"` taken by parsing the line as JSON; an `"id"` inside another field does not count. The day
   files are read only when a call is left after the skip rules (step 6). The scan checks the run's context
   between lines, and a done context ends the run with nothing written.
8. **The event.** `sense.FromClaudeFailure` with the payload's `session_id` and `cwd`, the call's tool, id,
   input and error, `IsInterrupt` false, the hook's time. So `kind` is `fail`, `exit` is absent for every tool
   but a shell tool, and scrubbing is exactly 004's. Then `sense.Record`.
9. **No empty store.** A run that finds nothing to record does not create the client's store. If the store
   exists already, the cursor is written; if not, nothing is (the next run reads the tail again). The store
   lock and the error file work as in 003 once the store is open.
10. **Order and cost.** Read the cursor (read-only open), read the file, decide, then open for writing, append
    the lines, replace the cursor last. An error stops the run; the cursor not written means the next run looks
    again, and step 7 keeps that from doubling a line.

## Build tasks

1. **Check this brief against the code** (`hook/main.go` opens the store before the steps run; this step must
   open it itself, read-only first). List what does not fit before building.
2. **`sense.Rejected`** and its fixtures. Proof: `TestLobeRejectedParse`.
3. **The step**, the payload key, `store.CursorFile`, the `stop` wiring in `stepsFor`, and the store opening
   rule of step 9. Proof: `TestLobeRejectedStop`, `TestLobeTranscriptOnlyUnderLogin`,
   `TestLobeRejectedNoEmptyStore`.
4. **The plugin**: the `Stop` entry; `test_the_plugin_holds_only_the_guard_hook` lists `Stop`;
   `TestLobeHookTimeoutsRegistered` already checks it. A line in `plugin/CHANGELOG.md`.
5. **Docs**: `docs/reference/commands.md`, `lobe-hook` section, names the `stop` event and what it reads;
   `CHANGELOG.md` under `[Unreleased]` replaces the "not recorded yet" sentence of 004; the phase 0 page row.
   `CLAUDE.md` gains one "Must not trade off" line: the memory reads a transcript only inside the session's own
   login folder, kept by `TestLobeTranscriptOnlyUnderLogin`.
6. **Review** by a Sonnet reviewer of the whole diff, then the tester run.

## Tests that stay in the repository

- `TestLobeRejectedParse`: a fixture transcript (made-up values) with: a rejected `Edit` (string content); a
  rejected call whose content is a list of text parts; a `Bash` failure with `Exit code 1` and no wrapper (not
  counted); a success (not counted); the same rejected block repeated in an `api-request-blob` line (counted
  once); a result whose `tool_use` is not in the bytes (`unknown`, `{}`); a broken line (skipped). 7 of 7.
- `TestLobeRejectedStop`: through `hook.Main` on a temp home: one line for the rejected edit; a second run adds
  nothing (cursor); a new rejected call appended adds one; an id already logged by `post-tool-use-failure` is
  not written again; a part line at the end is not read until its `\n` arrives, then once; a secret in the edit's
  `old_string` is redacted and appears in no file under the store; the memory's own command rejected is not
  logged.
- `TestLobeTranscriptOnlyUnderLogin`: a `transcript_path` outside `<login>/projects/` (another client's login
  folder, a sibling folder, a `..` path, a link where links can be made) is not read: nothing written.
- `TestLobeRejectedNoEmptyStore`: a transcript with no rejected call, for a client with no store, leaves no
  `lobe` folder.
- Kept green: every house rule of 004, `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeHookCutoffsUnderRegisteredTimeout`, `TestLobeImportsOnlyTheFour`, `TestLobeNoInitAndNoStartupCalls`,
  and every test under "Must not trade off" in `CLAUDE.md`, unedited.

## Evaluation

The tester is a fresh agent with the built program, Git Bash and an empty folder; no network; never the real
`~/.tentaqles`. Setup as in 004's brief (sandbox, `ID`, `ACME`, `LOG`, acme's login exported). The transcript
lives at `T="$ID/acme/claude/projects/p/s1.jsonl"`. `S()` pipes
`{"session_id":"s1","hook_event_name":"Stop","cwd":"$ACME","transcript_path":"$T"}` to `lobe-hook claude stop`.
`X(id, name, input, text)` appends an assistant `tool_use` line and a user `tool_result` line with `is_error`
true and the content `<tool_use_error>text</tool_use_error>`.

1. `X a1 Edit {file_path,old_string:"ZZZ"} "String to replace not found in file.\nString: ZZZ"`, `S`: 1 line;
   `tool` `Edit`, `kind` `fail`, no `exit`, `error` starts `String to replace not found in file.`, `id` is
   `evt_` and the first 8 hex of sha256 of `claude\0s1\0a1`.
2. `S` again: still 1.
3. Append a `Bash` `tool_result` `Exit code 1` with no wrapper (id `b1`), and `X a2 Write ...`, `S`: 2 lines.
4. Feed a `post-tool-use-failure` payload for id `a3`, then `X a3 Edit ...`, `S`: 3 lines, not 4.
5. `X a4` with `old_string` holding `ghp_` and 36 letters, `S`: 4 lines; `grep -r ghp_ "$ID/acme/lobe"` finds
   nothing.
6. `X a5 Bash {"command":"tq lobe status"} "..."`, `S`: still 4.
7. Half a line with no `\n` at the end, `S`: 4; complete it as a rejected `X a6` result, `S`: 5.
8. The same payload with `transcript_path` pointing to a copy of `T` outside `$ID/acme/claude/projects`:
   still 5.
9. globex's login with acme's folder: nothing; with globex's folder and a transcript under globex's login
   holding no rejected call: no `$ID/globex/lobe` folder.

PASS needs 9 of 9.

## Done when

- [ ] the four tests above are in the repository and green in CI on the three systems; the plugin suite green
- [ ] the tester run passes, 9 of 9
- [ ] the house rules hold, every "Must not trade off" test unedited, the new line added
- [ ] the docs and both changelogs are updated
- [x] the owner has seen a rejected edit from a real session in the client's log

## Risks and unknowns

- **The transcript is not a documented interface.** Its line shape (`message.content`, `tool_use`,
  `tool_result`, `is_error`, the `<tool_use_error>` wrapper) is what the owner's Claude Code v2.1.321 wrote. A
  change in Claude Code silently stops this feature; it never breaks a session (fail open). 005's status counts
  will show the silence.
- **Late, not at once.** The line lands at the end of the turn, not when the call failed. Nothing reads the log
  in the same turn yet.
- **Which calls carry the wrapper** is known for `Edit` only. Others (a `Read` of a missing file, a `Write`
  without a prior read, a schema error) are expected to use it too; the real-session check confirms what it can.
- **Subagents** are not covered.
- **A transcript path spelled differently is refused, silently.** An 8.3 short name (`PROGRA~1`) or a `\\?\`
  prefix names the same file but fails the textual check, so nothing is read and nothing is logged, with no
  error line. The path is taken as the payload spells it; a spelling that differs from the login folder's is not
  followed.
- **A 201st call on the same line as the 200th is lost.** The cap moves the cursor to the end of the line that
  holds the 200th call, so a second rejected result on that very line is not read by the next run. Parallel
  calls may share one transcript line; more than 200 rejected results in one line is not expected.
- **A replaced file that starts with the same bytes** as the old one up to the cursor is not noticed. The head
  covers only the bytes the cursor has passed (at most 4 KiB), so a file that grows keeps its cursor.

## Result of the build, the review and the tester run

Built on 2026-11-09 by three Haiku builders (the transcript reader, the plugin entry and docs, the stop step),
reviewed as one change by a Sonnet reviewer, fixed in one round by a Haiku builder, with one more fix by the
main session.

- The review found three blocking problems. A link at the login folder or its `projects` folder was followed
  (the check started below `projects`); every component from the login folder down is now checked, and the
  file opened must be the file checked (`os.SameFile`). The link test skipped on Windows; junctions, which need
  no administrator, now cover `projects\p`, `projects` itself and a login given through a junction. A line over
  8 MiB held the cursor in place for the rest of the session; such a window is now skipped.
- Also fixed from the review: the day files are read only when a call is left to record, and their ids are
  parsed, not searched as text; a 1 MiB look-back finds a call's `tool_use` written before the cursor, so a
  rejected memory command split across two runs is still skipped; the 200 cap moves the cursor only past the
  calls returned; the cursor carries a fingerprint of the bytes it passed, so a replaced file is read again.
- The main session changed that fingerprint to cover only the bytes before the cursor (at most 4 KiB): as
  first built it covered the file's first 4 KiB, so a small, growing transcript was re-read from its start on
  every turn. `TestLobeRejectedSmallFileKeepsCursor` proves it.
- Tests: `go vet`, `go build` and `go test ./... -count=1` green on Windows; the plugin suite 34 passed. The
  symlink case skips on this machine (no privilege); the junction cases run.
- Tester brief: 9 of 9 on Windows 11 in Git Bash against a program built from the working tree.

Confirmed in the owner's real session on 2026-11-10 (UTC), in a yduqs repository with the checkout's plugin and
the development build as `tq`: an Edit of `ZZZ-not-there` in an existing file, rejected by Claude Code before any
tool hook, added one line to yduqs's log at the end of the turn: `tool` `Edit`, `kind` `fail`, no `exit`, the
repository's key, the edit's input, and the error `String to replace not found in file.` with the string. The
cursor file for the session was written. The test file was removed afterwards.

CI green on Linux, macOS and Windows (with govulncheck, the marketplace pin and the plugin suite) for commit
`38029b68` on 2026-11-10. Nothing is left open.
