# 010: A failed shell command in Codex is logged

**Status** done: CI green on Linux, macOS and Windows for `6d72fb5e` on 2026-11-11 · **Stage** Sense · **Entries** S-02; F-03 (the Codex half: step 1 for Codex, steps 4 to
6, one handler); F-02 (the host `codex` and its login rule); S-07 (the drops file, one reason); D-11; phase 0
question 2 for Codex · **Needs** 009 · **Feeds** 014, 036

## Result

A client's Codex session now feeds the same event log as its Claude Code sessions. Codex has no failure
event: every finished shell call arrives on its one post-tool event, with the command's output and no exit
code. The code is in Codex's own session file, which the payload names. The lobe looks the call up there and
writes the same scrubbed line that features 003 to 009 write for Claude Code: `fail` with a fingerprint, `ok`
with no output, `hidden-fail` for a pipe that hid its exit code, a short reference for a repeat. Nothing is
printed into the Codex session.

`tentaqles settings render <client>` registers the handler in the client's Codex config and writes its trust
record, so the person accepts nothing by hand. The guard's two Codex hooks and their trust stay as they are.

From the sandbox of [002](002-sandbox.md), where `acme` has the Codex agent:

```bash
export CODEX_HOME="$TQ_HOME/identities/acme/codex"; ACME="$W/clients/acme"; cd "$ACME"
grep -c 'lobe-hook codex post-tool-use; exit 0' "$CODEX_HOME/config.toml"     # 1
S="$CODEX_HOME/sessions/2026/11/10/rollout-s1.jsonl"; mkdir -p "$(dirname "$S")"
printf '%s\n' '{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution",
"id":"call_01","exit_code":2}}}' | tr -d '\n' >> "$S"; echo >> "$S"
printf '%s' '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"'"$ACME"'","tool_name":"Bash",
"tool_use_id":"call_01","transcript_path":"'"$S"'","tool_input":{"command":"make build"},
"tool_response":"boom"}' | tr -d '\n' | tentaqles lobe-hook codex post-tool-use
```

The hook prints nothing. The day file gains one line with `"host":"codex"`, `"kind":"fail"`, `"exit":2`,
`"error":"boom"` and `"fp":"fp_e4f70458808e965c"`, the fingerprint the same error gets from Claude Code.

## Scope

**In**

- Phase 0 question 2 for Codex: the capture scripts, the owner's run, the written result, the fixtures.
- `internal/codexconfig`: a second list, `lobeEvents`, with one row (`PostToolUse`); the render of the lobe's
  group after the person's and the guard's; its trust record, written by `Render` (decision D-11);
  `LobeHookTrust`. `HookTrust` and `CheckGuard` keep reading the guard's handlers only.
- The host `codex` in `tentaqles lobe-hook`: its six event names, the login rule with `CODEX_HOME`, and one
  event with work, `post-tool-use`.
- The session-file lookup of a call's exit code (`internal/lobe/hook`), and `internal/lobe/sense/codex.go`
  with `codexText` and `FromCodexPostTool` (S-02 steps 1 to 7, with step 3 read from the session file).
- The drops file in its smallest form: `events/drops-YYYYMMDD.jsonl` with the one reason `no-exit`.
- The doctor finding `lobe-hooks-untrusted` and the Codex line of `tentaqles lobe status`.
- Codex level with features 003 to 009: the table under "What it builds on" says what each needed.

**Out**

- The other five handlers of F-03 step 4: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `Stop` and
  `SessionEnd`. Nothing they would run exists for Codex yet. [036](036-lessons-in-codex.md) brings what the
  lobe says and Codex's `Stop` handler; [011](011-retention-and-sweep.md) registers Codex's `SessionEnd`
  handler for the sweep.
- Anything printed into a Codex session, such as the fix or the lesson after a failure
  ([036](036-lessons-in-codex.md)). `FromCodexPostTool` returns the outcome those steps will read.
- A failed `apply_patch` or MCP call. In Codex 0.154.0 neither reaches PostToolUse (task 2), and Codex has no
  failure event, so the lobe never sees one. Reading them from the session file is a change to S-02, which
  the owner decides.
- The other drop reasons (`full`, `big`, `disk`), the sweep of drops files and the finding
  `lobe-events-dropped` ([011](011-retention-and-sweep.md)).
- Timing the Codex hook ([012](012-hook-latency-and-stall.md)) and attacking its login rule from outside
  ([013](013-isolation-leak-suite.md)).
- A marker for "this is a Codex agent session" (F-07, Risks). Task 2 records whether one exists; nothing uses
  it here.

## What it builds on

The guard's Codex code is read and not changed: `internal/cmd/codexhook.go` (`tentaqles codex-hook
pre-tool-use` and `session-start`, and the deny object at L63 to L72), `renderCodex` and `codexHookProgram`
in `internal/cmd/settings.go` (L180, L219), `CheckGuard` in `internal/codexconfig/guard.go`, and the
`codex-guard-*` findings in `internal/doctor/codex.go`.

What exists from each earlier feature, and what Codex needs on top of it:

| Feature | What exists, for Claude Code | What this feature adds for Codex |
|---|---|---|
| [002](002-sandbox.md) | The sandbox gives `acme` the Codex agent and renders `identities/acme/codex/config.toml` | The same render now also writes the lobe's handler and its trust record |
| [003](003-shell-failure-claude-code.md) | `hook.Main` for the host `claude`, the client rule, the store, the lock, scrubbing, the day file, `eventlog.ID`, `sense.Record`, the plugin entry and wrapper, phase 0 question 2 for Claude Code. `tentaqles lobe-hook codex ...` exits 0 and does nothing | The host `codex`; `CODEX_HOME` checked against `paths.IdentityDir(client, "codex")`; a handler in the Codex config in place of a plugin entry; `; exit 0` in place of the wrapper; the exit code read from Codex's session file, under 003's transcript rule; `host` `codex`; phase 0 question 2 for Codex. Scrubbing, the store and the day file are shared as they are |
| [004](004-any-tool-failure.md) | A failure of any tool is logged; refused and interrupted calls and the lobe's own tools are skipped; `shellwords` | Codex has no failure event. A tool that is not the shell tool is recorded only when its result carries an error flag (none arrived in task 2). The own-tool skip applies unchanged. The capture found no refusal texts, so `refused.go` does not change |
| [005](005-status-and-capture-switch.md) | The `capture` switch, `lobe status`, `lobe events`, `internal/lobe/health` | `capture: off` also stops Codex capture and the `no-exit` drops line. `lobe status` reports the Codex handler. `health` gains `lobe-hooks-untrusted` |
| [006](006-error-fingerprints.md) | `fp` on every failure, from the scrubbed `error` | The code goes into `exit` and the output, unchanged, into `error`, so one error has one `fp` on both hosts. `apply_patch` is already in the class `edit` |
| [007](007-duplicate-filter.md) | `eventlog.Check`, reference lines, replays | Nothing new. The id is `ID("codex", session, tool_use_id)`; without a call id the clock form applies |
| [008](008-successes.md) | `ok` lines, `sense.Project`, the full input after a failure, the background rule | Code 0 gives `ok` with `exit` 0. No code found writes no event unless `Hidden` fires. A tool that is not the shell tool never gets an `ok` |
| [009](009-hidden-failures.md) | `sense.Hidden`, the kind `hidden-fail` | The output of a call that exited 0 goes through `Hidden`, and so does the output of a call whose code was not found. Rule A is the only rule for other tools. Rule C applies, because Codex's shell tool is named `Bash` |

## What it leaves for later features

- `lobeEvents` in `internal/codexconfig` and the code that renders and trusts a lobe group: 036 adds the
  other rows (with timeout 3 for `SessionEnd`, F-03 step 4). 012 reads the list for its timeout check.
- `codexconfig.LobeHookTrust(codexHome) (Trust, error)`: 036, and every later doctor or status line.
- The host `codex` in `hook.Main`, with all six event names known and `post-tool-use` wired: 036 gives the
  other five their steps.
- `sense.FromCodexPostTool` and the `sense.Outcome` it returns for a Codex call: 036 (the answer after a
  failure), and E-02's verdict on Codex once 036 opens fires there.
- `eventlog` drops in their first form (`drops.go`, the reason `no-exit`, the 64 KiB stop): 011 adds the
  other reasons, the sweep and the finding. 011's file expects exactly this hand-over.
- `docs/prd/spikes/phase0-q2-codex-hooks.md`: 036 (what Codex does with added context), 038 (the deny form),
  018 (whether a host stops a hook's child processes) and 005's rule for person-only commands (the Codex
  session marker).

## How it works

**The handler in the Codex config** (F-03 steps 4 to 6)

1. `internal/codexconfig` gets `lobeEvents` beside `hookEvents` (L92). It is plain data, and the package
   imports no lobe package, because the guard imports it. One row here: table `PostToolUse`, label
   `post_tool_use`, subcommand `post-tool-use`, no matcher.
2. The handler is `<program> lobe-hook codex post-tool-use; exit 0`, type `command`, timeout 5, one
   `command` field and no `commandWindows`. `<program>` is `Managed.Command`, the same value the guard's
   handlers use (`codexHookProgram`). Codex 0.154.0 starts a hook in the session's shell
   (`codex-rs/core/src/session/mod.rs`, `shell.derive_exec_args`): `powershell.exe -NoProfile -Command` on
   Windows, the user's shell elsewhere; only with no session shell does it fall back to `COMSPEC /C` or
   `$SHELL -lc` (`codex-rs/hooks/src/engine/command_runner.rs`). `|| exit 0` is a parse error in Windows
   PowerShell 5.1 (checked on the owner's machine), so the tail is `; exit 0`, which ends with 0 in
   PowerShell and in a POSIX shell. Under cmd, `;` is not a separator: `lobe-hook` then gets two extra
   arguments, which it already ignores, and exits 0 on its own. The tail stands in for the plugin wrapper: a
   fault that `recover` cannot catch exits 2, and on a POSIX shell an exit 2 would replace the tool's result.
3. Render order inside each event: the person's groups, the guard's group, then the lobe's. `PostToolUse` has
   no guard group, so there it is the person's groups, then the lobe's. The guard's groups keep their index,
   so Codex's key for them (`<config path>:<label>:<group>:<handler>`) and their hash do not change.
4. Ownership. `ownedCommand` (L447) learns the lobe suffix, so a re-render removes the old lobe handler and
   writes one, as it does for the guard's. The walk over owned handlers must then tell the two families
   apart: `trustHooks` handles both, `HookTrust` only the guard's, `LobeHookTrust` only the lobe's. Today
   only `PostToolUse` holds a lobe handler, but 036 puts one into the guard's two events, so the split is
   built now.
5. Trust. `Render` already ends by writing `[hooks.state."<key>"]` with `trusted_hash` and `enabled = true`
   for each handler tentaqles owns (L151 to L157). It now does so for the lobe's handler too. The owner
   decided this (D-11); the package comment at L16 to L17, which says the person accepts hooks in Codex's
   `/hooks` screen, is corrected.
6. `LobeHookTrust` returns the five states of `Trust` for the lobe's handlers: `Missing` when one of
   `lobeEvents` has none, `Disabled`, `Untrusted`, `Modified` or `Trusted`. No state of it stops anything:
   `tentaqles run` keeps asking `CheckGuard`, which never looks at a lobe handler.
7. The label `post_tool_use` and the hash of the rendered handler are Codex's own, fixed by a golden hash as
   the two existing ones are (`internal/codexconfig/codexconfig_test.go` L17 to L36). Codex 0.154.0
   reported, for `tentaqles lobe-hook codex post-tool-use; exit 0`, timeout 5, alone in `PostToolUse`: label
   `post_tool_use`, `currentHash`
   `sha256:36e35d7278d88e1f884c1b818070a0ced16a988cb18ca3d9d8d29595a6163f47`.
8. Nothing in `internal/cmd/settings.go` changes its call. A client that already has Codex gets the handler
   at its next `tentaqles settings render <client>`; until then `lobe-hooks-untrusted` says so.

**The hook** (F-02 steps 1 to 5, S-02)

9. `tentaqles lobe-hook codex <event>` knows six event names: `session-start`, `user-prompt-submit`,
   `pre-tool-use`, `post-tool-use`, `stop` and `session-end` (F-03 step 4; all six fired in task 2). Only
   `post-tool-use` has a step. Any other event, known or not, exits 0 with no output and no write. Today
   `hook.Main` returns at once for any host but `claude` (`main.go` L74), and `knownEvent` (`steps.go` L25)
   is the seven events of Claude Code; the Codex set is its own list, without `post-tool-use-failure`.
10. The login rule for this host reads `CODEX_HOME` and never `CLAUDE_CONFIG_DIR`. It must name
    `paths.IdentityDir(client, "codex")`, compared as 003 compares the Claude Code folder. Unset, empty or
    any other folder: exit 0, nothing read under any `lobe` folder, nothing written anywhere.
11. The payload: `session_id`, `cwd`, `tool_name`, `tool_use_id`, `transcript_path`, `tool_input`,
    `tool_response` (task 2: all present on every PostToolUse). The skip rules of S-01 steps 2 and 3 apply:
    `capture` off, and the lobe's own tools.
12. Result text (S-02 step 2): `tool_response` itself when it is text (the `Bash` form in task 2); the `text`
    values joined by line breaks when it is a list of parts; its `content` when it is an object (the MCP form
    in task 2). For `Bash` this text is the command's output and nothing else; it is the `error` as it is.
13. Exit code, for the shell tool `Bash` (S-02 step 3, changed by the owner on 2026-11-10 after task 2). The
    code is read from Codex's session file, `transcript_path`:
    - The path must lie inside the `sessions` folder of the login folder that step 10 accepted, checked as
      `transcriptAllowed` (`rejected.go` L68) checks a Claude transcript: every component from the login
      folder down by `Lstat`, no link and no junction, a regular file at the end. `transcriptAllowed` takes
      the subfolder (`projects` or `sessions`) as a parameter in place of its fixed `projects`.
    - At most the last 1 MiB of the file is read (proposed), from the end, and never under the store's lock.
    - The code is the whole-number `exit_code` of the last line that is a JSON object with `type`
      `event_msg`, `payload.type` `item_completed`, `payload.item.type` `CommandExecution` and
      `payload.item.id` equal to `tool_use_id`.
    - No `tool_use_id`, a path outside the folder, a file that cannot be read, no such line in the last 1 MiB,
      or an `exit_code` that is not a whole number: no code. Nothing is retried; task 2 found the line in 3 of
      3 calls on the first look.
    - The output text is never searched for an exit code. Header words a command prints stay in the `error`.
14. What is written (S-02 steps 4 and 5):

    | Reading | Result |
    |---|---|
    | Code 1, no output, and a grep (`sense.GrepFoundNothing`) | An `ok` event with `exit` 1 and the success input, as S-01 writes it (step 16; added from the review) |
    | Code not 0 | A `fail` event: `host` `codex`, `exit` N, the output as `error`, the full input |
    | Code 0 | The output goes to `sense.Hidden`. Hidden: a `hidden-fail` event. Not hidden: an `ok` event with `exit` 0 and the projected input |
    | No code found | `Hidden`'s text rules run on the output. If one fires, a `hidden-fail` event. If none fires, no event, and one `no-exit` line in the drops file |

    Task 2 saw no still-running command: a 40-second `ping` ended inside one call with one PostToolUse.
15. Other Codex tools, `apply_patch` and MCP tools (S-02 step 6): only `Hidden`'s rule A applies, the error
    flag. With a flag, a `hidden-fail` event. With no flag nothing is written: no `fail`, no `ok`, no drops
    line. In Codex 0.154.0 a failed `apply_patch` and an MCP error never reach PostToolUse (task 2), so rule
    A is kept only for a later Codex that sends them; the session file is not read for these tools.
16. Every event then takes the path of the Claude Code events: S-05, S-08, S-06, S-07, through the same
    `Record`. A line written here is the line S-01, S-03 or S-04 would write, except for `host`.
17. The drops file (S-07 step 6). `events/drops-YYYYMMDD.jsonl`, named by the UTC date, one line
    `{"at":"<time>","why":"no-exit"}` per call, appended like an event line. It never holds content. It stops
    growing at 64 KiB (proposed). A failed write of it is ignored.
18. Nothing is printed. The outcome is returned for the steps 036 adds.

Edge cases: a negative `exit_code` is accepted; of two lines for one call id, the last wins; a line that is
not JSON, or is cut at the 1 MiB edge, is skipped; only the call's own line is used, and no other line
of the session file reaches the event or any other file. A
`hidden-fail` of a call whose session line said code 0 keeps `exit` 0 (this file's reading; S-04 does not
say).

## Where to read

| What | Where |
|---|---|
| Requirements | [S-02](../prd/lobe-sense.md), whole entry; [F-03](../prd/lobe-foundation.md) steps 1 and 4 to 7, "How to build it" (the Codex items) and its risks; [F-02](../prd/lobe-foundation.md) step 4 and the output table; [S-07](../prd/lobe-sense.md) step 6; [S-04](../prd/lobe-sense.md) steps 1 and 2; [F-07](../prd/lobe-foundation.md) step 5 (`lobe-hooks-untrusted`); [core page](../prd/vertical-lobe.md) 5.1, 5.2 (the note on Codex), section 8 (who runs a capture) and decision D-11 |
| The capture | [phase 0, question 2, for Codex](../prd/spikes/phase0-q2-codex-hooks.md): the events, the fields, the session-file lookup, the probes, the golden hash |
| Codex source, rust-v0.154.0 | `codex-rs/core/src/session/mod.rs` (the hook shell is the session's shell), `codex-rs/core/src/shell.rs` (`derive_exec_args`: PowerShell gets `-NoProfile -Command`), `codex-rs/hooks/src/engine/command_runner.rs` (the `COMSPEC /C` and `$SHELL -lc` fallback; a job object ends a hook's child processes) |
| Web, not machine-checked | Codex [hooks](https://developers.openai.com/codex/hooks.md): PostToolUse for Bash also runs after a non-zero exit; no exit code field is listed |
| Existing tentaqles code | `internal/codexconfig/codexconfig.go`: L16 to L17 (the package comment step 5 corrects), L92 `hookEvents`, L109 `Render`, L151 to L157 (trust last), L170 `trustHooks`, L235 `HookTrust`, L280 `ownedHandlers` (walks `hookEvents` only), L355 `renderHooks`, L447 `ownedCommand`, L511 `hookHash`. `internal/codexconfig/codexconfig_test.go` L17 to L36 (how the golden hashes were read from Codex; `commandWindows` is hashed on Windows only), L601 `TestHookTrustGoldenHashes`. `internal/codexconfig/guard.go` L31 `CheckGuard`. `internal/cmd/codexhook.go` (the guard's hook, unchanged). `internal/cmd/settings.go` L219 `codexHookProgram` (a bare path on Windows, which PowerShell runs as written). `internal/doctor/codex.go` (the `codex-guard-*` findings). `internal/lobe/hook/main.go` L74 (the host check), `steps.go` L25 `knownEvent`, `rejected.go` L68 `transcriptAllowed` and its tail reader. `internal/lobe/health/health.go` L63 to L67 (facts arrive as fields of `Client`). `internal/lobe/cli/status.go` L60 (`Status` takes the hook facts as parameters). `internal/lobe/sense`, from 003 to 009; `spike/hook-capture/` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits. Look at: how
   `internal/lobe/hook` lists hosts and events; how `health` and `lobe status` receive facts they may not
   import (005 hands `hooks.json` bytes in from `internal/cmd`); the line numbers named above; which
   `codexconfig` tests compare a whole rendered file. The owner approves changes to this file first.
   *Done 2026-11-10, with task 2.* The line numbers held. Changed: the handler tail (step 2), the hook host
   and events (step 9), the exit code (steps 13 and 14), the facts of health and status (task 7). No
   `codexconfig` test compares a whole rendered file: `TestRenderIdempotent` compares two renders with each
   other, and the others read the owned handlers table by table (`ownedCommands`, `checkOwned`).
2. **Phase 0, question 2, for Codex.** A builder cannot create a logged-in client and must not read a
   session. Add to `spike/hook-capture/`: a throwaway hook that appends its stdin to one file per event; a
   Codex config that registers it for every Codex hook event in a scratch `CODEX_HOME` setup of the client
   the owner names; and the reducer of 003, which prints counts and field names with their JSON types, never
   values. The owner runs one Codex session through: a failed shell command, a successful one, a long command
   that is still running when its first result arrives, a failed `apply_patch`, an MCP error result, an
   unknown MCP tool, a prompt, a stop and a session end. Write the result to
   `docs/prd/spikes/phase0-q2-codex-hooks.md`:
   - per call kind, the event that fired and its field names; whether `session_id`, `cwd` and `tool_use_id`
     are there; the JSON type of `tool_response`;
   - the tool names as Codex writes them (is the shell tool `Bash`; how an MCP tool is named);
   - how often each header form was seen: `Process exited with code N`, `Exit code: N`,
     `Process running with session ID N`, a line that is exactly `Output:`, the JSON form with
     `metadata.exit_code`; and whether any field states an exit code;
   - which call, under which tool name, carries the end of a command that was still running;
   - what Codex does when a PostToolUse hook exits 2, prints text, or runs 10 seconds under a timeout of 5;
   - Codex's version, the event label in its hook keys, and its own `currentHash` for the handler of step 2,
     read with `codex app-server` (`hooks/list`) from a scratch `CODEX_HOME`, as the comment at
     `codexconfig_test.go` L16 describes;
   - for later features, from the same run: what Codex does with added context on each event (036), whether
     it cuts a session-end hook at 3 seconds (036), whether it stops a hook's child processes (018), and any
     environment variable that marks a Codex agent session (F-07).

   The builder writes the fixtures from the field names, with made-up values. No captured payload leaves the
   owner's machine. If the result contradicts steps 9 to 15, stop and ask the owner before building.
   *Done 2026-11-10*, in two runs; the second checked the session-file lookup. The result contradicted steps
   13 and 15, the owner chose the session file, and steps 2 and 13 to 15 were rewritten.
3. **The Codex config.** Change `internal/codexconfig/codexconfig.go`: `lobeEvents`, the render of the lobe
   group, the lobe suffix in `ownedCommand`, the split of the walk, trust for both families, `LobeHookTrust`,
   the corrected package comment. Proof: `TestLobeRenderWritesTheCodexHandler`,
   `TestLobeCodexHooksNeverChangeGuardTrust`, `TestLobeHookTrustGoldenHashes`, and the package's existing
   tests. Can run beside tasks 4 and 5.
4. **The session-file reader.** In `internal/lobe/hook`: `transcriptAllowed` gains its subfolder
   parameter, and a new `codexExit(login, path, callID) (code int, ok bool)` does step 13. Nothing is
   ported, so `NOTICE` does not change. Proof: `TestLobeCodexExitFromSession`, the Codex rows of
   `TestLobeTranscriptOnlyUnderLogin`, and `rejected.go`'s existing tests unchanged.
5. **The drops file.** Create `internal/lobe/eventlog/drops.go` with the one reason. Proof:
   `TestLobeCodexNoExitCounted`.
6. **The Codex path.** Create `internal/lobe/sense/codex.go` with `codexText` and `FromCodexPostTool`
   (steps 11, 12 and 14 to 16; the code from task 4 is passed in), and add the host `codex` with its event
   list, login rule and `post-tool-use` step to `internal/lobe/hook`. Proof: `TestLobeCodexSameFingerprintAsClaude`,
   `TestLobeCodexNeedsTheClientsLogin`, `TestLobeCodexPostToolPaths`, and the Codex rows of
   `TestLobeHookServesOnlyItsLogin`, `TestLobeHookLoginUnsetDoesNothing`, `TestLobeHookAlwaysExitsZero` and
   `TestLobeNoHooksSwitch`.
7. **Health and status.** `internal/cmd/doctor.go` asks `codexconfig.LobeHookTrust` for each client with codex
   in its scope and hands the state to `internal/lobe/health` as a plain string field of `health.Client`,
   beside `PluginFound` and `HooksJSON`, because a lobe package may not import `internal/codexconfig`;
   `health` returns `lobe-hooks-untrusted` (warn) for any state but `trusted`, with the fix line
   `tentaqles settings render <client>`; an empty field (no codex) gives nothing. `tentaqles lobe status`
   gets the same state as one more parameter of `cli.Status`, as it gets `hooksJSON`, and shows it as
   `"codex":"<state>"` inside `hooks`. Proof: `TestLobeDoctorCodes`
   with the new code, `TestLobeHealthNeverReachesTheGuard`, `TestLobeStatusShowsTheCodexHandler`.
8. **Docs.** `docs/reference/commands.md`: the `lobe-hook` section names the host `codex`; the
   `settings render` section says it writes and trusts a third handler; `doctor` lists the new code.
   `CHANGELOG.md`, one line under `[Unreleased]`. `CLAUDE.md`: the layout line of `internal/codexconfig`
   (the transcript line keeps its one test, `TestLobeTranscriptOnlyUnderLogin`, which gains Codex rows), and
   one new "Must not trade off" line, with the owner's yes on its wording: "The lobe's Codex handlers never
   change the guard's Codex trust and never stop a session: `TestLobeCodexHooksNeverChangeGuardTrust`." The
   plugin does not change.
9. **Tester run.** A fresh agent runs the Evaluation below on the built binary.

## Tests that stay in the repository

- `TestLobeCodexExitFromSession`, 8 of 8: a line with code 1 gives 1; code 0 gives 0; code -1 gives -1; two
  lines for one id, the last wins; no line for the id, no code; the only line more than 1 MiB from the end,
  no code; a line whose `exit_code` is a string, no code; a line for another id only, no code.
- `TestLobeTranscriptOnlyUnderLogin`, with Codex rows: a session file under another client's `codex`
  folder, under the login folder but outside `sessions`, under a `sessionsX` sibling, through a link or a
  junction, and a folder in place of the file: each gives no code, though each path leads to a line that
  would give one.
- `TestLobeCodexNoExitCounted`: a call with no code found adds exactly 1 `no-exit` line to the drops file.
- `TestLobeCodexSameFingerprintAsClaude`: one error body through S-01 and through S-02 gives the same `fp`.
- `TestLobeCodexNeedsTheClientsLogin`: with `CODEX_HOME` unset, or set to another client's folder, a failed
  shell call writes 0 lines and no drops line.
- `TestLobeCodexPostToolPaths` (new name, not in the requirements): one case per row of the table in step 14
  (no code found split in two: `Hidden` fires, and it does not) and per case of step 15, plus the own-tool
  skip and `capture: off`. It checks the kind, `exit`, the projected or full `input`, and that nothing is on
  stdout. 8 of 8.
- `TestLobeCodexHooksNeverChangeGuardTrust`: with lobe handlers missing, untrusted or edited, `HookTrust` is
  `Trusted` and `CheckGuard` is OK in 3 of 3 cases; the two golden guard hashes are unchanged.
- `TestLobeRenderWritesTheCodexHandler` (new name): after `Render`, `hooks.PostToolUse` ends with one group
  that holds one handler, `<command> lobe-hook codex post-tool-use; exit 0`, timeout 5, no matcher, with a
  state entry whose `trusted_hash` is its hash and `enabled = true`; a person's `PostToolUse` group stays in
  front; a second `Render` returns `changed` false and the same bytes; a changed command replaces the
  handler; `LobeHookTrust` is `Trusted`, and `Missing` once the handler is deleted.
- `TestLobeHookTrustGoldenHashes` (new name): the hash of the rendered lobe handler equals
  `sha256:36e35d7278d88e1f884c1b818070a0ced16a988cb18ca3d9d8d29595a6163f47`, the one Codex reported in
  task 2, as `TestHookTrustGoldenHashes` does for the guard's two.
- `TestLobeCodexHandlerTimeoutsRendered` (F-03, beside `TestLobeHookTimeoutsRegistered`, which reads only the
  plugin's `hooks.json`): the rendered Codex lobe handler has timeout 5; the guard's two have 15.
- `TestLobeMalformedPostToolUseKeepsTheGuard` (from the review): a person's `PostToolUse` that is not a list
  of groups leaves `Render` writing and trusting the guard's handlers, and the table as it was.
- `TestLobeStatusShowsTheCodexHandler` (new name): `tentaqles lobe status --json` holds `"codex":"trusted"`
  after a render, another state after the handler is edited, and no `codex` key for a client without codex.
- F-02's house tests, now with Codex: `TestLobeHookServesOnlyItsLogin` (13 of 13: Claude Code's 7 events and
  Codex's 6), `TestLobeHookLoginUnsetDoesNothing` (with `CLAUDE_CONFIG_DIR` right and `CODEX_HOME` unset),
  `TestLobeHookAlwaysExitsZero`, `TestLobeNoHooksSwitch`.
- The phase 0 comparison: the fixtures match the field names in `docs/prd/spikes/phase0-q2-codex-hooks.md`.
- Kept green and unedited: `TestRunCodex_RefusesWithoutTheHooks`, `TestRunCodex_RefusesHooksOffOptions`,
  `TestCodexPreToolUse_PanicDenies`, `TestCodexPreToolUse_WatchdogDenies`, `TestCodexHookProgram`,
  `TestHookTrustGoldenHashes`, `TestHookHashMatchesCodex`, `TestCheckGuard`, `TestOwnedCommandQuotedPath`,
  `TestLobeGuardNeverImportsLobe` (`internal/codexconfig` reaches no lobe package), `TestLobeScrubParity`,
  and the rest of "Must not trade off" in `CLAUDE.md`. Task 1 found no `codexconfig` test that compares a
  whole rendered file; a test that reads every table of `hooks` may see the new `PostToolUse` group.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows) and an empty
working folder. It uses no network, starts no Codex, and never reads or writes the real `~/.tentaqles`. Setup:

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
ACME="$W/clients/acme"; ID="$TQ_HOME/identities"
D="$(date -u +%Y%m%d)"; LOG="$ID/acme/lobe/events/events-$D.jsonl"; DROPS="$ID/acme/lobe/events/drops-$D.jsonl"
export CODEX_HOME="$ID/acme/codex"; C="$CODEX_HOME/config.toml"; cd "$ACME"
S="$CODEX_HOME/sessions/2026/11/10/rollout-s1.jsonl"; G="$ID/globex/codex/sessions/2026/11/10/rollout-g.jsonl"
mkdir -p "$(dirname "$S")" "$(dirname "$G")" || echo "SETUP FAIL sessions"
sline() {  # sline <call id> <exit code> [file]: Codex's session-file line for a finished command
  printf '{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"%s","command":"x","exit_code":%s}}}\n' "$1" "$2" >> "${3:-$S}"
}
cx() { p="${1//ACME/$ACME}"; p="${p//SESS/$S}"; p="${p//GLOB/$G}"; printf '%s' "$p" | tentaqles lobe-hook codex post-tool-use; echo "exit=$?"; }
sh1() {  # sh1 <call id> <command> <exit code> <output>: one finished Codex shell call in session s1
  sline "$1" "$3"
  cx '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"'"$1"'","transcript_path":"SESS","tool_input":{"command":"'"$2"'"},"tool_response":"'"$4"'"}'
}
```

Stop and report FAIL if a line starts with `SETUP FAIL`. Run the checks in order, within 5 minutes. Every `cx`
and `sh1` call must print only `exit=0`. In `sh1`, a `\n` inside the body is written as the two characters
backslash and `n`, as in the JSON.

1. The wiring. `grep -c 'lobe-hook codex post-tool-use; exit 0' "$C"` prints `1`; `grep -c ' codex-hook ' "$C"`
   prints `2`; `grep -c trusted_hash "$C"` prints `3`; `tentaqles doctor --json | grep -c '"code":"codex-guard-'`
   prints `0`; `tentaqles doctor --json | grep -c '"code":"lobe-hooks-untrusted"'` prints `0`; and
   `tentaqles lobe status --json | grep -c '"codex":"trusted"'` prints `1`.
2. A failed command: `sh1 call_01 'make build' 2 'boom'`. Expect 1 line in `LOG` with `host` `codex`, `kind`
   `fail`, `exit` 2, `error` `boom`, `id` `evt_99df6dec`, `fp` `fp_e4f70458808e965c` and `input`
   `{"command":"make build"}`.
3. A success: `sh1 call_02 'make build' 0 'ok'`. Expect 2 lines. The second has `id` `evt_e4d672f2`, `kind`
   `ok`, `exit` 0, no `error` and no `fp`. The text `CommandExecution` is in no file under `$ID/acme/lobe`.
4. No line in the session file. Expect 2 lines in `LOG`, and `DROPS` has 1 line that holds `"why":"no-exit"`
   and none of `make`, `RUNS`.

   ```
   cx '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"call_03","transcript_path":"SESS","tool_input":{"command":"make build"},"tool_response":"RUNS"}'
   ```

5. A session file outside the login. `sline call_04 1 "$G"`, then the JSON below. Expect 2 lines in `LOG`
   and 2 lines in `DROPS`: the file under globex's folder was not used.

   ```
   cx '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"call_04","transcript_path":"GLOB","tool_input":{"command":"make build"},"tool_response":"boom"}'
   ```

6. Header words in the output are not read. `sline call_05 3`, then the JSON below. Expect 3 lines. The
   third has `id` `evt_7dc80186`, `kind` `fail`, `exit` 3, `error` `Process exited with code 0\nboom` and an
   `fp` that is not `fp_e4f70458808e965c`.

   ```
   cx '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"call_05","transcript_path":"SESS","tool_input":{"command":"make test"},"tool_response":"Process exited with code 0\nboom"}'
   ```

7. The same error from Claude Code. Expect 4 lines. The fourth has `host` `claude`, `id` `evt_53f277b4`,
   `exit` 2, `error` `boom` and the same `fp` `fp_e4f70458808e965c`.

   ```
   printf '%s' '{"session_id":"c1","hook_event_name":"PostToolUseFailure","cwd":"'"$ACME"'","tool_name":"Bash","tool_use_id":"toolu_01","tool_input":{"command":"make build"},"error":"Exit code 2\nboom"}' | CLAUDE_CONFIG_DIR="$ID/acme/claude" tentaqles lobe-hook claude post-tool-use-failure; echo "exit=$?"
   ```

8. A replay and a repeat. `sh1 call_01 'make build' 2 'boom'` again: still 4 lines. Then
   `sh1 call_06 'make build' 2 'boom'`: 5 lines, and the fifth has `id` `evt_c60f30cd`, `dup` `evt_99df6dec`,
   `host` `codex`, `kind` `fail`, no `input` and no `error`.
9. A hidden failure:
   `sh1 call_07 'npm test 2>&1 | tail -5' 0 'FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n'`.
   Expect 6 lines. The sixth has `id` `evt_02e77f18`, `host` `codex`, `kind` `hidden-fail`, `input`
   `{"command":"npm test 2>&1 | tail -5"}`, an `error` that starts with `FAIL src/cart.test.ts`, and `fp`
   `fp_90da432fb402b004`.
10. A secret. Expect 7 lines. `grep -c -E 's3cretPW|Zx81kQpL0029aaBB|hunter2' "$LOG"` prints `0`, and
    `grep -o '\[redacted:' "$LOG" | wc -l` prints 3 or more.

    ```
    sh1 call_08 'curl -u app:s3cretPW https://api.example.test/v1/items?api_key=Zx81kQpL0029aaBB' 22 'password=hunter2 rejected'
    ```

11. Tools that are not the shell tool. The first JSON leaves `LOG` at 7 lines and `DROPS` at 2 lines. The
    second gives 8 lines; the eighth has `id` `evt_69da6bd9`, `host` `codex`, `kind` `hidden-fail`, `error`
    `widget 4411 is not frobnicated` and `fp` `fp_48622a622f4928cc`.

    ```
    cx '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"apply_patch","tool_use_id":"call_09","tool_input":{"input":"*** Begin Patch"},"tool_response":"Done"}'
    cx '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"mcp__acme__ping","tool_use_id":"call_10","tool_input":{"n":4411},"tool_response":{"isError":true,"content":[{"type":"text","text":"widget 4411 is not frobnicated"}]}}'
    ```

12. The lobe's own tool, and capture off. `sh1 call_11 'tentaqles lobe status' 0 'ok'`: still 8 lines. Then
    `lobeset acme capture off`, `sh1 call_12 'make lint' 1 'boom'` and the JSON of check 4 with `call_13`:
    still 8 lines in `LOG` and 2 lines in `DROPS`. Then `lobeset acme capture on`.
13. The login. `CODEX_HOME="$ID/globex/codex" sh1 call_14 'make lint' 1 'boom'`, then
    `(unset CODEX_HOME; export CLAUDE_CONFIG_DIR="$ID/acme/claude"; sh1 call_14 'make lint' 1 'boom')`. Each
    prints only `exit=0`. `LOG` still has 8 lines, `DROPS` 2 lines, and
    `ls "$ID/globex/lobe/events" 2>/dev/null | wc -l` prints `0`.
14. The lobe's handler is taken away; the guard does not notice.
    `grep -v 'lobe-hook codex' "$C" > "$W/c.toml"; cp "$W/c.toml" "$C"`. Then
    `tentaqles doctor --json | grep -c '"code":"codex-guard-'` still prints `0`, and
    `tentaqles doctor --json | grep -c '"code":"lobe-hooks-untrusted"'` prints `1`.

**Person checks** (the owner, at a terminal of their own):

1. Build task 2: the capture run in one real client, and the hand-back of counts and field names (done
   2026-11-10, two runs).
2. In a real client with codex in its scope: `tentaqles settings render <client>`, then start `codex` there
   through tentaqles. Codex's `/hooks` screen lists the lobe's handler as trusted, with nothing to accept.
   Let the agent run `ls /no-such-folder-7731`. `tentaqles lobe events` in that folder then shows a line with
   `"host":"codex"` and `"kind":"fail"`. Nothing extra appeared in the Codex session.
   *Done 2026-11-11*, in the tqx playground's `acme` (the dev build copied first on `PATH` as `tentaqles`,
   since a render writes the path of the `tentaqles` found there), with Codex 0.167.1, which had updated
   itself: one line with `host` `codex`, `kind` `fail`, `exit` 1 and the PowerShell error; nothing extra in
   the session. The hook ran without `--dangerously-bypass-hook-trust`, so 0.167.1 trusted the hash
   tentaqles wrote. Codex 0.167.1 starts a background server on a socket under `CODEX_HOME` whose path must
   be shorter than about 108 characters; the playground's is longer, so the run used `codex --no-daemon`.

PASS needs 14 of 14 from the tester.

## Done when

- [ ] `docs/prd/spikes/phase0-q2-codex-hooks.md` is written from the owner's run, the owner has read it, and
  the fixtures match its field names (S-02: "this entry is not done until the answer is yes")
- [ ] the tests named above are in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) on Linux, macOS and Windows; the plugin suite stays green
- [ ] the tester brief passes, 14 of 14, and the owner has run person check 2
- [ ] the house rules hold: `TestLobeCodexHooksNeverChangeGuardTrust`, `TestLobeHookServesOnlyItsLogin` (13 of
  13), `TestLobeHookAlwaysExitsZero`, `TestLobeCodexNeedsTheClientsLogin`, `TestLobeScrubParity`,
  `TestLobeGuardNeverImportsLobe`, and every test under "Must not trade off" in `CLAUDE.md` unedited
- [ ] `internal/cmd/codexhook.go`, `internal/guard` and the guard's two rendered Codex handlers are unchanged,
  byte for byte, and so are their two golden hashes
- [ ] `docs/reference/commands.md`, `CHANGELOG.md` and `CLAUDE.md` are updated
- [ ] the owner has seen a failed command in a real Codex session appear in `tentaqles lobe events`

## Risks and unknowns

- **The exit code exists only in Codex's session file**, whose format Codex does not document. If a release
  renames `CommandExecution`, `item_completed` or `exit_code`, or writes the line after the hook runs, every
  shell call reads as `no-exit`. The drops count is the only alarm, and the finding that reports it arrives
  with 011. Task 2 saw the line written before the hook in 3 of 3 calls, on one machine, in one version.
- **The session file is large.** Reading at most its last 1 MiB keeps the run inside the 1.5 s cutoff; a call
  whose line lies further back (a single command with more than 1 MiB of later output before its hook) gets
  no code. That would need many lines written between a command's end and its own hook, which task 2 did
  not see.
- **The hook's shell is the session's shell.** On Windows it is PowerShell, so a program path holding a
  character PowerShell reads (`$`, a backtick, `@` or `{` at the start) would not run, for the lobe's handler
  and for the guard's alike. `codexHookProgram` checks only what cmd reads. Not changed here; the guard's
  handlers carry the same risk today.
- **Codex's socket path (0.167.1).** Codex 0.167.1 starts a background server on
  `<CODEX_HOME>\app-server-control\app-server-control.sock` and fails to start when that path is about 108
  characters or more. A client's real path (`~\.tentaqles\identities\<client>\codex\...`) is about 95 for a
  short name, so a long client name or a long user folder can hit it; `codex --no-daemon` works around it.
  Not tentaqles' bug and not changed here; it may need a line in the troubleshooting page later.
- **Codex versions.** The hash rules and the three golden hashes are from Codex 0.154.0; person check 2
  ran on 0.167.1 with the same hash trusted and the same session-file line. A later version may
  hash differently; a wrong hash shows as `lobe-hooks-untrusted` and as a prompt in Codex's `/hooks` screen,
  never as a stop.
- **Non-shell failures teach nothing** (S-02 step 6): in 0.154.0 they never reach PostToolUse, and a rule
  that fires on such a call will get no verdict later. Reading them from the session file is the owner's
  decision for a later feature.
- **A command that is still running** when its first result arrives was not seen in task 2. If a later
  Codex splits a long command over two calls, the first has no `CommandExecution` line yet and counts as
  `no-exit`.
- **MCP tool names** are `mcp__<server>__<tool>`, as in Claude Code (task 2), so the class and the own-tool
  rule apply unchanged.
- **Trust written by tentaqles for a handler the person never saw** follows `Render` today and the owner's
  decision D-11. It is one more handler now and five more with 036.
- **One handler, not six.** F-03 step 4 lists six Codex handlers and its tester brief counts six. This file
  registers the one whose event has work, as the Claude Code side registers one plugin entry per feature.
  The counts in check 1 (1, 2 and 3) follow from that. If the owner wants all six registered now, steps 1
  and 9 and check 1 change, and the empty handlers cost one process start per event.
- **Codex's `Stop` handler arrives in 036.** A client used only through Codex therefore gets no pass from
  018 until then, unless a Claude Code session of the same client runs one. The store is shared, so
  nothing is lost; it is only late. Its `SessionEnd` handler, and so its sweep, arrives in 011.
- **Existing clients** get the handler at their next `tentaqles settings render`. Until then the doctor
  warns and Codex capture is simply off.
- **Two readings are this file's own**: a `hidden-fail` keeps the `exit` 0 its session line stated, and
  `lobe-hooks-untrusted` reaches `health` as a value computed in `internal/cmd`. The owner may overturn
  either at task 1.
