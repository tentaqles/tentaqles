# Lobe: Sense (S-01 to S-08)

Sense turns each tool call into one line in the client's event log and does nothing else. A *hook* is a command
that the agent program (the *host*: Claude Code or Codex) runs around a tool call, with the call's details as JSON
on stdin. S-01 and S-02 read a failed call, S-03 reads a successful one, and S-04 finds the failures that the host
reported as successes. Every event then takes the same path: S-05 removes secrets and cuts the text to size, S-08
gives a failure its fingerprint, S-06 checks for a duplicate, and S-07 appends the line. Sense never calls a model,
never starts another program, and prints nothing. The event shape, the store layout, the hook table and the time
budgets these entries follow are in the [core page](vertical-lobe.md), sections 5.2 to 5.4.

**Where Sense ends.** The two post-tool hooks carry more than Sense (core page 5.2). Sense is their capture part
and runs first; the inject work of other pages runs after it, in the same process.

- **Order.** `sense.Record` returns before any other feature of the hook starts. After a failure the later steps
  are E-02's verdict on the rules that fired on this call, C-08's starter fix and R-04's lesson for this exact
  error. A hidden failure counts as "after a failure": whatever event carries a `fail` or `hidden-fail` outcome
  also carries the answer to it, so the fix and the lesson are printed on Claude Code's PostToolUseFailure, on
  Claude Code's PostToolUse (a hidden failure) and on Codex's one post-tool event, always inside F-05's frame.
  After a success they are E-02's verdict, G-08's mark on an edited file and, after an edit, G-09's blast
  radius and E-05's argument check. None of them changes or removes the line, and Sense prints nothing for them.
- **The line.** It is whole in today's event file when `Record` returns, so R-04's fingerprint trigger, which
  reads the tail of that file, and every later hook see it.
- **The outcome.** `Record` also returns `sense.Outcome{Session, ID, Tool, CallID, Call, Kind, FP, State}` to the
  rest of the process. `CallID` is the host's `tool_use_id`. `Call` is the call text from `sense.CallText`: the
  command string for a shell tool, else the compact JSON of the tool input with sorted keys, scrubbed by S-05 and
  cut at 4000 characters. It is built from the whole input, before S-03's projection, and Sense never stores it.
  E-01 and E-02 call the same function before the call runs, so E-02's `call` hash is the same on both sides.
  `State` is `full` (a full line was written), `ref` (a reference line, S-06), `replay` (the line was already
  there) or `dropped` (S-07 could not write it; `Kind` and `FP` are still set).
- **A duplicate.** A replay or a repeat (S-06) does not end the hook early. `Record` returns its outcome with
  state `replay` or `ref`, and E-02's verdict and G-08's mark on an edited file still run when S-06 wrote no new
  line.
- **The session file.** Sense reads and writes no key in `sessions/<session>.json`. E-02 keeps `open_fires`
  there, written at PreToolUse. Its verdict step finds the fire by the outcome's `CallID`, or by `Tool` and the
  hash of `Call`, judges it by `Kind` and `FP`, and stores `Session` and `ID` as the trial line's `event`. E-02
  judges an outcome whose `State` is `dropped` the same way: its `Kind` and `FP` are known although no line was
  written.
- **No outcome.** When a skip rule applies (S-01 steps 2 and 3), or a command only went to the background or is still
  running (S-03, S-02), there is no line and no outcome. An open fire then waits for E-02's own time limit.
- **Time.** Sense's part is measured against the capture budget, 150 ms at the 95th percentile (proposed), and
  the whole process is cut off at 1.5 seconds. F-02 owns both clocks; M-06 measures a post-tool event both ways.

## S-01 Capture failures from every tool

**Phase** 1 · **Origin** port: agentmemory · **Depends on** F-01, F-02, F-03, F-06, S-05, S-06, S-07, S-08

**What it is.** When any tool call fails in Claude Code, a hook hands the lobe the tool's name, its input and the
error text, and the lobe writes one `fail` event to the client's event log. It covers every tool: shell commands,
file edits, web fetches and MCP tools (tools an outside program serves over the Model Context Protocol).

**Why it matters.** Promise line 3, "fewer errors", starts here: a mistake that is not recorded cannot become a
lesson. Decision D-3 asks for every kind of tool error. A capture limited to the shell would miss SQL sent through
an MCP tool and every failed edit.

**Mechanism**

1. F-03 registers `tentaqles lobe-hook claude post-tool-use-failure` on `PostToolUseFailure` with no matcher. The
   hook reads `session_id`, `cwd`, `tool_name`, `tool_use_id`, `tool_input`, `error` and `is_interrupt` from stdin.
2. Skip rules. F-02 has already ended the hook, with nothing written in any store, when stdin is not a JSON object
   or is over its size cap; when `TQ_LOBE_NO_HOOKS` is `1`; when `cwd` resolves to no client; or when the
   session's login folder (`CLAUDE_CONFIG_DIR`, or `CODEX_HOME` for Codex) is unset or is not that client's
   identity folder (core page 5.1). Sense itself writes nothing when `capture` is `off` (F-06); when
   `is_interrupt` is true; when the error starts with a host refusal (the person or a hook said no, so the tool
   never ran); or when the tool is one of the lobe's own.
3. The lobe's own tools are a shell call (`Bash`, `PowerShell`) whose program is `tentaqles` or `tq` with `lobe`
   as its first word, and any tool of the optional MCP server (R-03). Neither is recorded, as a failure or as a
   success, so the lobe does not learn from its own output. The command is split with `internal/lobe/shellwords`
   and the program is compared without its folder and `.exe`, case ignored. A command of several statements is
   the lobe's own only when every statement is, a leading `cd` aside (proposed): `cd app && tq lobe graph map`
   is skipped and `npm test && tentaqles lobe status` is recorded. An MCP tool is the lobe's own when its own
   name, the part after the server in `mcp__<server>__<tool>`, starts with `lobe_`, the prefix R-03 gives that
   server's tools (proposed); the server part is whatever the person registered.
4. A shell call whose error is empty or only `Exit code 1`, and whose command runs `grep`, `egrep`, `fgrep`,
   `zgrep`, `rg`, `ag`, `ack` or `git grep`, found nothing. It is recorded as `ok` (S-03), not as a failure.
5. The event (core page 5.4) has `kind` `fail`, `host` `claude`, `at` from the hook's UTC clock and `id` from
   S-06. `input` is the compact JSON of `tool_input` with sorted keys; there and in the line itself, `&`, `<`
   and `>` are written as they are. For `Bash` and `PowerShell`, a first line `Exit code N` sets `exit` to N and
   is removed from `error`, so both hosts store the same `error` for the same failure (S-02).
6. `repo` and `cwd`: walk up from `cwd` to the first folder that holds `.git`, staying inside the client folder.
   `repo` is that repository's key, `r_` plus 8 hex characters; F-01 owns the formula (`store.RepoKey`) and the
   graph folder of G-02 carries the same value. `cwd` is the path relative to the folder where the walk
   stopped, `.` at its root. With no repository, `repo` is left out and `cwd` is relative to the client folder,
   `.` at its root.
   A `.git` that is a file marks a linked git worktree, which takes the key of its main repository (core page
   5.3), so its events meet that repository's lessons with `applies: repo`. It is found without running git:
   read the file's `gitdir:` line, then the `commondir` file in the folder that line names, which leads to the
   main repository's `.git` folder; the key is the one of the folder that holds it. When the `gitdir:` folder
   has no `commondir` file (a submodule), the folder where the walk stopped is the repository.
7. The event passes S-05, S-08, S-06 and S-07, in that order. Sense prints nothing. `Record` then hands its
   outcome to the steps that follow in the same process (see "Where Sense ends" above): E-02 judges the rules
   that fired on this call, and C-08 and R-04 may print a starter rule's fix and the lesson for this exact error.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| agentmemory (Apache-2.0) | port | [`plugin/hooks/hooks.json` L48](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/plugin/hooks/hooks.json#L48): `PostToolUseFailure` is registered with no `matcher` key |
| agentmemory (Apache-2.0) | port | [`src/hooks/post-tool-failure.ts` L31](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L31): an interrupted call is skipped; L34 to L38 read the tool name, input and error |
| agentmemory (Apache-2.0) | weakness, not copied | [`src/hooks/_capture-filter.ts` L72](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture-filter.ts#L72): a value at or under the cap is returned before any scrubbing runs |
| Trail-code (MIT) | port | [`src/trace/run-calls.ts` L114](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/run-calls.ts#L114): the texts Claude Code writes for a refused call; L246 is the rule for a grep that found nothing |
| Trail-code (MIT) | port | [`src/claude/post-bash.ts` L4](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/post-bash.ts#L4): a failed Bash call arrives on PostToolUseFailure and never on PostToolUse |
| Claude Code hooks page | web, not machine-checked | [hooks](https://code.claude.com/docs/en/hooks): PostToolUseFailure carries `tool_name`, `tool_input` and an `error` text, and cannot block |

**How to build it**

- Package `internal/lobe/sense`: `claude.go` (the payload and `FromClaudeFailure`), `refused.go`, `own.go` (step
  3), `place.go`, `calltext.go` (`CallText`, shared with E-01 and E-02), `sense.go` (`Record(e Event) Outcome`
  runs S-05, S-08, S-06, S-07). `Event` is the core page's shape with `omitempty` on `repo`, `input`, `error`,
  `exit`, `fp` and `dup`; `exit` is a pointer, so a stated 0 is still written.
- `tool_input` arrives as an object or as text that holds that object. The guard reads both forms
  (`internal/cmd/claudehook.go` L353). Rebuild that small reader: the lobe may not import `internal/cmd`.
- Imports (core page 5.1). Of tentaqles, the Sense packages import only `internal/resolve`, `internal/paths`,
  `internal/registry` and `internal/manifest`, plus other lobe packages. Steps 3 and 4 find a command's program
  with `internal/lobe/shellwords`, the lobe's own splitter, never with `internal/guard`. No Sense package has an
  `init` function or a package-level call: every pattern in S-02, S-04 and S-05 is compiled on first use.
- F-02 resolves the client and checks the login folder before any Sense code runs. Sense is handed the client
  and the open store and never reads `CLAUDE_CONFIG_DIR` or `CODEX_HOME` itself.
- A missing `tool_name` is stored as `unknown`, a missing `session_id` as an empty `session`, for which S-06
  skips its check. agentmemory files such events under one session named `unknown` (`post-tool-failure.ts` L33),
  which makes unrelated failures look like repeats of each other.
- Where we differ from agentmemory, on purpose. It posts to a local server with a 3000 ms abort and a 3500 ms exit
  cap (`post-tool-failure.ts` L12, L13); we append to a file. It stores the input as text cut to 4000 characters
  and scrubs it only when it is over that cap (L52 to L55); S-05 always scrubs, and scrubs before it cuts. It
  runs git with a 500 ms timeout to find the project
  (`src/hooks/_project.ts` L16); we read the folder tree and start no process.

**How we know it works**

- `TestLobeSenseFailureEveryTool`: failures for `Bash`, `PowerShell`, `Edit`, `Write`, `WebFetch` and
  `mcp__acme__ping` write 6 lines, all with `kind` `fail`.
- `TestLobeSenseFailureSkips`: interrupt, refusal, bad JSON, no client, another client's login folder, no login
  variable, `capture: off`: 0 lines in any store, exit 0 and empty stdout in all 7 cases.
- `TestLobeSenseOwnTools`: `tentaqles lobe graph find x`, `tq lobe status`, `cd app && tentaqles.exe lobe graph
  map` and the tool `mcp__graph__lobe_find` write 0 lines; `tentaqles doctor`, `npm test && tentaqles lobe
  status`, `echo tentaqles lobe` and `mcp__acme__ping` write 1 line each. 8 of 8.
- `TestLobeSenseGrepNoMatch`: `grep` with `Exit code 1` and no other text gives `ok`; with `Exit code 2`, `fail`.
- `TestLobeSenseStartsNoProcess`: with an empty `PATH` the hook still writes its line. Yes or no.
- `TestLobeSenseWorktreeKey`: an event from a folder whose `.git` is a file with a `gitdir:` line, behind which
  a `commondir` file leads to the repository `app`, has the `repo` of an event from `app` itself; with no
  `commondir` file it has the key of its own folder. 2 of 2, with an empty `PATH`.
- `TestLobeSenseOutcome`: after a failure, the outcome's `ID`, `Kind` and `FP` equal the written line's, the line
  can be read as soon as `Record` returns, and `CallText` gives one text for the payloads before and after a call.
- The append stays inside the capture budget of core page 5.2 (150 ms at the 95th percentile, proposed). M-06
  measures the post-tool events both ways: the append alone, and the whole process against its 1.5 second cut-off.

**Tester brief**

Setup: the standard tester setup in F-07. Then run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`,
the login folder of `acme`: without it a lobe hook writes nothing (core page 5.1). `ACME` is `$W/clients/acme`;
`LOG` is `$TQ_HOME/identities/acme/lobe/events/events-<UTC date, YYYYMMDD>.jsonl`. From inside `ACME`, pipe each
JSON, `ACME` replaced, to `tentaqles lobe-hook claude post-tool-use-failure`. A JSON printed here over two lines
is one line: join the lines with a space. No error below matches a starter rule (C-08) and no lesson exists, so
the steps after Sense print nothing.

1. `{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping",
   "tool_use_id":"toolu_01","tool_input":{"n":4411},"error":"widget 4411 is not frobnicated"}`
   Expect exit 0, empty stdout and 1 line in `LOG`. As JSON it has `v` 1, `id` `evt_9bde2e09`, `session` `s1`,
   `host` `claude`, `tool` `mcp__acme__ping`, `kind` `fail`, `cwd` `.`, `input` `{"n":4411}`, `error`
   `widget 4411 is not frobnicated`, `fp` `fp_48622a622f4928cc`, and no `exit` and no `repo`.
2. The same with `toolu_02` and `"is_interrupt":true` added, then with `toolu_03` and the error
   `The user doesn't want to proceed with this tool use.`, then the plain text `not json`. Each time expect exit
   0, empty stdout and still 1 line.
3. `{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash",
   "tool_use_id":"toolu_04","tool_input":{"command":"grep -n zzz notes.txt"},"error":"Exit code 1"}`
   Expect 2 lines. The second has `id` `evt_20ed2862`, `kind` `ok`, and no `error` and no `fp`.
4. Check 3 with `toolu_05` and the command `tentaqles lobe graph find zzz`, then with `toolu_06`, the tool
   `mcp__graph__lobe_find` and the input `{"q":"zzz"}`. Each time expect exit 0, empty stdout and still 2 lines.
5. Check 1 with `"session_id":"s3"`, `toolu_07` and `"cwd":"ACME/app"`. Expect 3 lines. The third has `cwd` `.`
   and a `repo` of `r_` plus 8 hex digits; under F-01's formula as drafted (the path `app`) it is `r_a172cedc`.
6. Check 1 with `toolu_08`, once with `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude"` set for that one
   command and once through `env -u CLAUDE_CONFIG_DIR`. Each time expect exit 0, empty stdout and still 3 lines,
   and no `events-*.jsonl` file exists under `$TQ_HOME/identities/globex/lobe`.
7. A linked worktree, made by hand with no git. Create the folder `ACME/app/.git/worktrees/wt` and in it the
   file `commondir` holding the one line `../..`. Create the folder `ACME/app-wt` and in it the file `.git`
   holding the one line `gitdir: ACME/app/.git/worktrees/wt`, `ACME` replaced. Then check 1 with
   `"session_id":"s4"`, `toolu_09` and `"cwd":"ACME/app-wt"`. Expect 4 lines. The fourth has `cwd` `.` and the
   same `repo` as the third line.

**Risks and open questions**

- Which event carries a failed MCP call is not documented (core page 5.2). If it arrives on PostToolUse, S-04
  catches it. The phase 0 capture (question 2) decides which path is the main one. The builder writes the
  capture script; the owner runs it in one client they name and hands back counts and field names, never
  content (core page section 8).
- The `error` text per tool was not captured live, and the hooks page was read through a fetch tool.
- Refusal texts follow the host's wording and change with releases. Keep them in one table with the version they
  were seen on. A missed refusal is stored as a failure and can mislead C-01.
- A refused or interrupted call leaves nothing, in the log or in the session file. C-02's evidence rule (c)
  would need a note of it and is not in the first build; phase 0 question 2 must first show how a payload marks
  such a call.
- The walk stops at the nearest `.git`, so an event inside a nested clone or a submodule carries a key that has
  no graph behind it (G-10 does not enter them). The event is still paired and fingerprinted.
- Every tool of the optional MCP server carries the name prefix `lobe_` (R-03), but Codex may name MCP tools in
  another form than `mcp__<server>__<tool>`. Until the server ships the rule matches nothing.

## S-02 Capture failures from Codex

**Phase** 1 · **Origin** port: Trail-code · **Depends on** F-02, F-03, S-01, S-03, S-04, S-07

**What it is.** Codex has no failure event. Every finished shell call arrives on its post-tool event, and the
exit code is present only as text in a header that the model also sees. The lobe reads that header, decides
between `fail` and `ok`, and records the same event S-01 would.

**Why it matters.** The goals name Codex beside Claude Code. Without this entry the lobe would record every
failed Codex command as a success, and C-01 would pair a failure with a "fix" that also failed.

**Mechanism**

1. F-03 registers `tentaqles lobe-hook codex post-tool-use`. It reads `session_id`, `cwd`, `tool_name`,
   `tool_use_id`, `tool_input` and `tool_response`. The skip rules of S-01 steps 2 and 3 apply, with `CODEX_HOME`
   as the login folder.
2. Result text: `tool_response` itself when it is text; the `text` values joined by line breaks when it is a list
   of parts; its `content` when it is an object.
3. Exit code, for the shell tool (`Bash`). If the text starts with `{` and parses as a JSON object, a whole-number
   `metadata.exit_code` is the code and `output` is the body. Otherwise the header is the text before the first
   line that is exactly `Output:` (no such line, no header) and the body is what follows it. In the header,
   `Process running with session ID N` means still running; `Process exited with code N` or `Exit code: N` gives
   the code.
4. Code not 0: a `fail` event with `host` `codex`, `exit` N and the body as `error`. Code 0: the body goes to
   S-04 and then S-03. Still running: nothing is written.
5. No code named: S-04's text rules run on the whole text. If none fires, no event is written and one `no-exit`
   line goes to the drops file (S-07; core page 5.3), so a change in Codex's header format shows up as a count.
6. Other Codex tools (`apply_patch`, MCP tools): only S-04's error flag rule applies. With no flag, nothing is
   written, neither `fail` nor `ok`. Non-shell Codex failures stay unrecorded until the phase 0 capture says
   what arrives; an `ok` without evidence would feed wrong pairs to C-01.
7. An event written here leaves the same line and the same outcome as S-01 or S-03 would, so the steps after
   Sense (E-02's verdict, C-08, R-04, G-08) run on Codex's one post-tool event for both kinds of result.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/trace/codex-exec.ts` L14](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/codex-exec.ts#L14): the header ends at the first line that is exactly `Output:`; L15 to L17 are the exit and running patterns |
| Trail-code (MIT) | port | [`src/trace/codex-exec.ts` L39](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/codex-exec.ts#L39): the JSON form, with `metadata.exit_code` as a whole number |
| Trail-code (MIT) | port, changed | [`src/claude/post-bash.ts` L142](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/post-bash.ts#L142): no exit named means nothing is written; L146 puts `Exit code N` in front of a failed body |
| Trail-code (MIT) | port | [`src/hosts/codex-hooks.ts` L64](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/hosts/codex-hooks.ts#L64): the PostToolUse entry for `Bash`, with a 5 second timeout |
| Codex hooks page | web, not machine-checked | [hooks](https://developers.openai.com/codex/hooks.md): PostToolUse for Bash also runs after a non-zero exit; no exit code field is listed |
| tentaqles | existing | `internal/codexconfig/codexconfig.go` L93: the guard's Codex matcher is `^Bash$`, so the shell tool is named `Bash` |

**How to build it**

- `internal/lobe/sense/codex.go`: `codexText`, `codexExit(text string) (code int, state exitState)` with states
  `exited`, `running` and `none`, `codexBody`, and `FromCodexPostTool`. The three patterns use no look-ahead and
  compile in Go's `regexp` with the `(?m)` flag, on first use and not at package level (core page 5.1).
- Keep the source's edge cases. Header words that the command itself prints sit in the body and are never read
  (pinned at `test/claude-codex-post-bash.test.ts` L32). A text with no `Output:` line has no header
  (`codex-exec.ts` L55). A negative code is accepted. The body loses one leading line break (L54).
- Where we differ. Trail puts `Exit code N` in front of the body (`post-bash.ts` L146). We put N in `exit` and
  leave `error` clean, so S-08 gives one error the same fingerprint on both hosts.
- A weakness we do not copy. For a call with no code named, Trail's hook writes nothing (`post-bash.ts` L142)
  while its transcript reader counts the same call as failed (`src/trace/codex.ts` L228). We give one answer:
  unknown, counted in the drops file, never `ok` and never `fail`.

**How we know it works**

- `TestLobeCodexExitHeader`, 8 of 8: code 1 gives `fail`; code 0 `ok`; `Exit code: 2` `fail`; JSON `exit_code` 3
  `fail`; JSON `exit_code` 0 `ok`; running, no event; no `Output:` line, no event; header code 0 with the body
  line `Process exited with code 1`, `ok`.
- `TestLobeCodexNoExitCounted`: a text with no code adds exactly 1 `no-exit` line to the drops file.
- `TestLobeCodexSameFingerprintAsClaude`: one error body through S-01 and through S-02 gives the same `fp`.
- `TestLobeCodexNeedsTheClientsLogin`: with `CODEX_HOME` unset, or set to another client's folder, a failed shell
  call writes 0 lines and no drops line. Yes or no.
- Phase 0 capture: live payloads for a failed shell call, a failed `apply_patch` and a failed MCP call match the
  fixtures. Yes or no. This entry is not done until the answer is yes. The capture needs a real, logged-in
  client (core page section 8): the builder writes the capture script, and the owner runs it in one client they
  name and hands back counts and field names, never content. The builder compares those field names with the
  fixtures; no builder or tester agent runs the capture or reads a live session.

**Tester brief**

Setup: the standard tester setup in F-07; its sandbox already gives `acme` the Codex agent. Then run
`export CODEX_HOME="$TQ_HOME/identities/acme/codex"`, the Codex login folder of `acme`: without it a lobe hook
writes nothing (core page 5.1). `ACME` is `$W/clients/acme`. `LOG` is
`$TQ_HOME/identities/acme/lobe/events/events-<today's UTC date, YYYYMMDD>.jsonl` and `DROPS` is
`drops-<the same date>.jsonl` beside it. Run from inside `ACME`. Pipe each JSON, with `ACME` replaced, to
`tentaqles lobe-hook codex post-tool-use`. A JSON printed here over several lines is one line: join the lines
with a space. Every check expects exit 0 and empty stdout.

1. `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"call_01",
   "tool_input":{"command":"make build"},"tool_response":"Chunk ID: 4f2a9c\nWall time: 0.4120
   seconds\nProcess exited with code 2\nOriginal token count: 12\nOutput:\nboom"}`
   Expect 1 line in `LOG` with `host` `codex`, `kind` `fail`, `exit` 2, `error` `boom`, `id` `evt_99df6dec`, `fp`
   `fp_e4f70458808e965c` and `input` `{"command":"make build"}`.
2. The same with `call_02`, `Process exited with code 0` and the body `ok`. Expect 2 lines. The second has `id`
   `evt_e4d672f2`, `kind` `ok`, `exit` 0, and no `error` and no `fp`.
3. The same with `call_03` and `"tool_response":"Process running with session ID 7\nOutput:\nRUNS"`. Expect 2
   lines in `LOG` and no `DROPS` file.
4. The same with `call_04` and `"tool_response":"Process exited with code 0"`. Expect 2 lines in `LOG`, and
   `DROPS` has 1 line that holds `"why":"no-exit"`.

**Risks and open questions**

- The exit code exists only as header text. If a Codex release renames the line, every shell call reads as
  `no-exit`. The drops count is the only alarm; F-07's doctor finding `lobe-events-dropped` (S-07) reports it.
- Nothing here was checked against a live Codex session. Trail's fixtures (`test/codex-fixtures.ts` L48) are
  written by hand.
- Step 6 means that in phase 1 a failed `apply_patch` or MCP call in Codex teaches nothing, and a rule that fired
  on such a call gets no verdict from E-02. The phase 0 capture is what ends that gap.
- A command that is still running when its first result arrives ends in a later call. Whether a hook fires for
  that call, and under which tool name, is not known, so such a command may leave no event. Phase 0 settles it.

## S-03 Record successes, minimally

**Phase** 1 · **Origin** port: agentmemory · **Depends on** F-02, F-03, S-01, S-04, S-05, S-06, S-07

**What it is.** After a tool call succeeds, the lobe writes a small `ok` event: which tool ran, when, and the
part of its input that identifies the call. The tool's output is never stored. Every success is logged this way,
not only one that follows a failure.

**Why it matters.** The goal "this failed, then this worked" needs the second half, and E-02 needs to know how
each call ended when a rule would have fired. Everything else about a success is the client's data, which
principle 6 keeps out. This hook runs on every tool call, so it must do the least (core page section 4).

**Mechanism**

1. `tentaqles lobe-hook claude post-tool-use` reads `session_id`, `cwd`, `tool_name`, `tool_use_id`, `tool_input`
   and `tool_response`. Codex arrives through S-02. The skip rules of S-01 steps 2 and 3 apply.
2. S-04 reads `tool_response` first. A hidden failure becomes a `hidden-fail` event and this entry is done; the
   steps after Sense then treat the call as a failure (see "Where Sense ends" above). A
   result that only says the command went to the background writes no event: it started, it did not yet work.
3. Otherwise `tool_response` is dropped here. No later step sees it.
4. The input is reduced to the fields that identify the call (the *projection*): `command` for `Bash` and
   `PowerShell`; `file_path` or `notebook_path` for `Edit`, `Write`, `MultiEdit`, `NotebookEdit` and `Read`;
   `pattern` and `path` for `Grep` and `Glob`; `url` or `query` for `WebFetch` and `WebSearch`. Any other tool,
   MCP tools included, keeps every top-level key, with text values cut to 200 characters (proposed) and nested
   values replaced by `{}` or `[]`. A known tool with none of its fields is treated as any other tool.
5. One exception (core page 5.4): the success that follows a failure in the same session keeps its scrubbed
   input in full, as a failure does, and expires with the rest of the log. Here that means: the tail that S-06
   reads holds an earlier `fail` or `hidden-fail` from the same session and tool. This is the success C-01 may
   pair with that failure, and C-01 must see what changed. The same tool is enough, because C-01 pairs across
   tools only for shell calls, whose projection is already the whole command. If the tail could not be read, the
   full input is kept. This read comes before the projection and takes no lock (S-07 step 8); S-06 reads the
   tail again later, under the lock.
6. The event has `kind` `ok`, no `error` and no `fp`, and `exit` 0 for a shell call when the host states it. It
   passes S-05, S-06 and S-07. `Record` then hands its outcome to the steps that follow in the same process (see
   "Where Sense ends" above): E-02's verdict, G-08's mark on an edited file, and G-09 and E-05 after an edit.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| agentmemory (Apache-2.0) | port, changed | [`src/hooks/post-tool-use.ts` L40](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-use.ts#L40): the success path reads the input whole; L56 passes it on with no cap |
| agentmemory (Apache-2.0) | not taken | [`src/hooks/_capture-filter.ts` L60](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture-filter.ts#L60): output is kept up to 8000 characters by default. We keep none |
| agentmemory (Apache-2.0) | weakness, not copied | [`src/hooks/post-tool-failure.ts` L55](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/post-tool-failure.ts#L55): a failure's input is stored as cut text while a success keeps the object, so the two cannot be compared field by field |
| Trail-code (MIT) | port | [`src/trace/run-calls.ts` L138](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/run-calls.ts#L138): the two texts Claude Code writes for a command that went to the background |
| tentaqles (new) | own design | the projection and the rule that keeps the full input after a failure |

**How to build it**

- `internal/lobe/sense/project.go`: `Project(tool string, input json.RawMessage) json.RawMessage`, driven by one
  table with a test per row. `claude.go` gains `FromClaudePostTool`.
- The order is fixed: S-04 reads the response, the response is dropped, the tail is read for step 5, the input
  is projected, S-05 scrubs what is left. S-05 still runs on a projection, because a command line can hold a token.
- The stored `input` has one form for `ok` and `fail`: compact JSON with sorted keys. C-01 can then compare a
  failure and a success key by key, which agentmemory's two forms do not allow.
- agentmemory moves base64 images out of the output (`src/hooks/post-tool-use.ts` L79 to L90). We keep no output.
  The 200 character cut keeps a pasted blob out of the input.
- We found no code in agentmemory that pairs a failure with a later success. That was a search, not a proof.
- The field names per tool come from use, not from a schema we could check. Confirm them in the phase 0 capture.

**How we know it works**

- `TestLobeSenseOkDropsOutput`: a response that holds `OUTPUT-MARKER-7731` is processed, and the marker appears 0
  times in any file under the `lobe` folder.
- `TestLobeSenseOkProjection`: 9 tools, one per name group above plus one MCP tool: 9 of 9 match.
- `TestLobeSenseOkFullAfterFailure`: after a failure on the same tool in the same session, a success holds the
  full input. The same success in another session holds the projection.
- `TestLobeSenseBackgroundStartWritesNothing`: 0 lines.
- `TestLobeSenseOkEveryCall`: 100 successful calls in a session with no failure and no rule on trial write 100
  lines. On a fixture of 100 mixed successful calls, the median `ok` line is under 300 bytes (proposed).
- M-06 measures what a line for every success costs the post-tool hook, on its typical and its large store.

**Tester brief**

Setup: the standard tester setup in F-07. Then run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`,
the login folder of `acme`: without it a lobe hook writes nothing (core page 5.1). `ACME` is `$W/clients/acme`.
`LOG` is `$TQ_HOME/identities/acme/lobe/events/events-<today's UTC date, YYYYMMDD>.jsonl`. Run from inside `ACME`.
Pipe each JSON, with `ACME` replaced, to `tentaqles lobe-hook claude post-tool-use` unless the check names another
event. A JSON printed here over several lines is one line: join the lines with a space. `A250` is the letter `a`
written 250 times, `A200` 200 times.

1. `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Write","tool_use_id":"toolu_01",
   "tool_input":{"file_path":"ACME/notes.txt","content":"CONTENT-MARKER-1"},
   "tool_response":{"filePath":"ACME/notes.txt","note":"OUTPUT-MARKER-2"}}`
   Expect exit 0 and empty stdout (this setup has no graph and no lesson, so the steps after Sense add nothing).
   `LOG` has 1 line with `id` `evt_9bde2e09`, `kind` `ok`, `tool` `Write`, `input`
   `{"file_path":"ACME/notes.txt"}`, no `error`, no `fp`. A search of every file under
   `$TQ_HOME/identities/acme/lobe` for the two markers gives 0 hits.
2. `{"session_id":"s2","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"mcp__acme__ping",
   "tool_use_id":"toolu_02","tool_input":{"q":"A250"},"tool_response":"pong"}`
   Expect 2 lines. The second has `input` `{"q":"A200"}`.
3. To `tentaqles lobe-hook claude post-tool-use-failure`:
   `{"session_id":"s2","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping",
   "tool_use_id":"toolu_03","tool_input":{"q":"x"},"error":"widget 4411 is not frobnicated"}`
   Then repeat check 2 with `toolu_04`. Expect 4 lines. The fourth has `kind` `ok` and `input` `{"q":"A250"}`.
4. `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_05",
   "tool_input":{"command":"npm run dev"},"tool_response":"Command running in background with ID: bash_1"}`
   Expect exit 0 and still 4 lines.

**Risks and open questions**

- A line for every success makes successes most of the log. That is the design; S-07's fill levels stop `ok`
  lines first when a day's file grows large, and M-06 measures the cost in time.
- Step 5 keeps up to 4000 characters of a successful command, query or edit for the retention period (core page
  decision A-18). It is scrubbed and private, but closer to client data than a projection, and S-05 removes
  secrets, not rows or names.
- When the earlier failure is older than the tail S-06 reads, the success is a projection and C-01 sees less.
  For an MCP tool that means values cut to 200 characters, which C-01 drops as uncomparable.
- A payload over F-02's size cap leaves no event at all, so a success with a very large result cannot be paired
  and a failure hidden in it is missed. F-02 owns the cap; M-06 should count how often it is reached.
- Whether a background start reaches this hook with that exact text was not captured live.

## S-04 Detect failures that look like successes

**Phase** 1 · **Origin** port: Trail-code · **Depends on** F-02, S-05, S-08 (S-02 and S-03 call it)

**What it is.** Some failures reach the lobe marked as successes. A pipe such as `npm test | tail -5` reports the
exit code of `tail`, a test runner can print failures behind a wrapper that exits 0, and an MCP tool can return a
normal result that carries an error flag. This entry reads the result once, before S-03 drops it, and records such
a call as a `hidden-fail` event.

**Why it matters.** If a failed retry is stored as `ok`, C-01 takes it for the fix and writes a wrong lesson.
Trail-code's source says that in its authors' sessions most failed commands exited 0 because of a pipe
(`src/trace/failure.ts` L3). That is their observation, not our measurement.

**Mechanism**

1. Result text: `tool_response` when it is text; for a shell result object, `stdout` and `stderr`, each when
   not empty, joined by a line break; for a list of parts, the `text` values joined by line breaks; for any other
   object, its `content` read the same way. Of a text longer than 4000 characters, only the first 1500 and the
   last 2500 are read.
2. Rule A, any tool: the response is an object whose `isError` or `is_error` is true.
3. Rule B, shell tools: a test runner's own summary. The first format found decides, each with a zero guard:
   lines starting `Test Files`, `Tests` or `Test Suites:` fail on `[1-9]\d* failed`; TAP `# fail N` when N is not
   0; `Ran N tests in` on a line starting `FAILED`; `test result:` on `test result: FAILED`; Go output on a line
   starting `FAIL`, `--- FAIL` or `panic:`; `N failed`, `N errors` and `N failing` when N is 1 or more. Rule B, like rule C, skips a command whose first program
   only reads (owner, 2026-11-10).
4. Rule C, `Bash` only, when the command hides its exit code: its last top-level pipe segment starts with `tail`,
   `head`, `less`, `more` or `cat` (Trail's list), `grep` or `tee` (proposed), or it ends in `|| true`. It is
   skipped when the first program only reads: `cat`, `grep`, `rg`, `head`, `tail`, `less`, `ls`, `find`, `echo`,
   `sed`, `awk`, `jq`, or `git` with `log`, `show`, `diff` or `status` (proposed). Then one line decides: it
   starts with `fatal:`, `error:`, `error[CODE]:`, `panic:`, `npm ERR!`, `npm error` or `Traceback`, or with
   `Exit code N` or `exit status N` where N is not 0; or it holds `ERR_PNPM_`; or it has a `name:` prefix and
   ends with `command not found`, `No such file or directory` or `Permission denied`.
5. No rule fires: the call goes on to S-03. Otherwise the `hidden-fail` event keeps the full input, as a failure
   does. Its `error` is the text from the deciding line on, at most 20 lines (proposed), with line breaks at its
   end removed. For rule A the deciding line is the first line of the result text. For rule B it is the first
   line starting `FAIL`, `--- FAIL` or `FAILED` when there is one, else the summary line.
6. A `hidden-fail` outcome counts as a failure for the steps after Sense: on this same post-tool event C-08's
   fix and R-04's lesson for the fingerprint are printed, inside F-05's frame, as they are on
   PostToolUseFailure (core page 5.2).

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/trace/failure.ts` L46](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L46): head 1500 and (L47) tail 2500; L51 applies the window |
| Trail-code (MIT) | port, stricter | [`src/trace/failure.ts` L10](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L10): `FAILED_TEXT` matches anywhere in a line and ignores case (flags at L31). Rule C keeps the anchored part only |
| Trail-code (MIT) | port | [`src/trace/checks.ts` L174](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/checks.ts#L174): the summary rule with its zero guard; L177 to L184 are the other runner formats; L185: with no summary, a piped command never passes |
| Trail-code (MIT) | port | [`src/trace/shell.ts` L130](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/shell.ts#L130): the filter programs `tail`, `head`, `less`, `more` and `cat` |
| Trail-code (MIT) | weakness, not copied | [`src/trace/failure.ts` L41](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L41): `Tests\s+\d+ failed` has no zero guard, so `Tests 0 failed` reads as a failure |
| MCP specification | web, not machine-checked | [tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools): a tool's own error is a normal result with `isError` true (rule A) |

**How to build it**

- `internal/lobe/sense/hidden.go`: `Hidden(tool, input, response) (errText string, hidden bool)`, pure: no disk,
  no clock. `pipes.go`: `hidesExit(command string) bool` and `firstProgram`.
- Build one pattern set. Trail has two that disagree (`src/trace/failure.ts` and `src/claude/note-signals.ts`
  L93 to L95), and in the second, `0 failed` anywhere in the output cancels a failure found elsewhere (L259).
- Trail never judges a read-only command by the text it printed (`src/trace/runs.ts` L403). Rule C gets that
  protection with less code: it runs only when the exit code is hidden, skips reading programs and anchors every
  pattern, so a README that mentions `Permission denied` in a sentence does not count.
- `hidesExit` and `firstProgram` split the command with `internal/lobe/shellwords`, the lobe's own splitter (core
  page 5.1; R-04, E-04 and C-08 use it too), which tracks quotes, backticks and `$(` and is tested against the
  same cases as the guard's. Sense never imports `internal/guard`. A command with `<<` (a here-document) counts
  as not hiding its exit code. In doubt prefer `hidden-fail`: a wrong `ok` can produce a wrong lesson.
- The patterns of rules B and C are compiled on first use, not at package level (core page 5.1).

**How we know it works**

- `TestLobeHiddenFixtures`: 15 hidden failures and 15 passes that mention failure words, 30 of 30 (proposed),
  with ` Test Files 1 failed | 12 passed (13)` (fail), `Tests 0 failed` (pass), `FAIL a.test.ts` plus `0 failed`
  on another line (fail).
- `TestLobeHiddenMcpFlag`: `{"isError":true,"content":[{"type":"text","text":"boom"}]}` is hidden, error `boom`.
- `TestLobeHiddenWindow`: a `FAIL` line at character 2000 of a 10,000 character output is not seen (a recorded
  limit). `Hidden` on a 1 MiB response returns in under 5 ms (proposed).

**Tester brief**

Setup: the standard tester setup in F-07. Then run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`,
the login folder of `acme`: without it a lobe hook writes nothing (core page 5.1). `ACME` is `$W/clients/acme`;
`LOG` is `$TQ_HOME/identities/acme/lobe/events/events-<UTC date, YYYYMMDD>.jsonl`. From inside `ACME`, pipe each
JSON, `ACME` replaced, to `tentaqles lobe-hook claude post-tool-use`. A JSON printed here over several lines is
one line: join the lines with a space. Expect exit 0, empty stdout: no error below matches a starter rule (C-08)
and no lesson exists, so the steps after a hidden failure print nothing.

1. `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_01",
   "tool_input":{"command":"npm test 2>&1 | tail -5"},
   "tool_response":{"stdout":"FAIL src/cart.test.ts\n  total is wrong\nTests: 1 failed, 3 passed\n","stderr":""}}`
   Expect 1 line with `id` `evt_9bde2e09`, `kind` `hidden-fail`, `input` `{"command":"npm test 2>&1 | tail -5"}`,
   an `error` that starts with `FAIL src/cart.test.ts`, and `fp` `fp_90da432fb402b004`.
2. The same with `toolu_02`, the command `cargo test` and the stdout `test result: ok. 12 passed; 0 failed\n`.
   Expect 2 lines. The second has `id` `evt_8618533e` and `kind` `ok`.
3. `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"ACME","tool_name":"mcp__acme__ping",
   "tool_use_id":"toolu_03","tool_input":{"n":4411},
   "tool_response":{"isError":true,"content":[{"type":"text","text":"widget 4411 is not frobnicated"}]}}`
   Expect 3 lines. The third has `id` `evt_7fe44cf7`, `kind` `hidden-fail` and `fp` `fp_48622a622f4928cc`.
4. Check 1 with `toolu_04`, the command `git push 2>&1 | tail -3` and the stdout `fatal: no remote\n`. Expect
   line 4 with `id` `evt_20ed2862`, `kind` `hidden-fail` and `error` `fatal: no remote`.
5. Check 1 with `toolu_05`, command `cat deploy.log | tail -3`, stdout `error: disk full\n`: line 5 has `kind` `ok`.

**Risks and open questions**

- Which hook event carries an MCP result with `isError` true is not documented. Rule A costs nothing if unused.
- A failure line in the middle of a long output is outside the window, as in Trail. PowerShell pipelines are not
  covered. The rule C lists are ours and unmeasured; C-07's backfill is the first real corpus to test them on.
- E-02 judges a rule by the `kind` this entry decides. A failure read as `ok` gives a good rule a false alarm,
  and a success read as `hidden-fail` gives a wrong rule a hit. M-05 counts both.

## S-05 Secret scrubbing

**Phase** 1 · **Origin** port: agentmemory, Trail-code · **Depends on** F-02

**What it is.** Before an event is written anywhere, the lobe removes anything that looks like a secret (a
password, a token, a private key) from its input and error text and puts a marker in its place that names the kind
of secret, never its value. It then cuts both texts to the 4000 characters of core page 5.4.

**Why it matters.** The fourth anti-goal is client secrets stored in a lesson. Commands and errors carry tokens
all the time, and every later stage reads the event log, so this is the one place to stop a secret for all of them.

**Mechanism**

1. Key pass, for a JSON input: a text value under a key that names a secret becomes `[redacted:key]`. A key,
   split into words at `_`, `.`, `-`, digits and case changes, names a secret when a word ends in `apikey`,
   `secret`, `token`, `password`, `passwd` or `credential(s)`, is `auth`/`authorization`, or is `api` before `key`.
2. Window: each text value of the input is cut to its first 4256 characters; an `error` longer than 4000, to two
   parts, its first 1756 and its last 2756, which are scrubbed apart. The 256 extra characters are proposed; the
   1500 and 2500 they are added to are S-04's window. A character is one Unicode code point.
3. Text pass on each input value and each error part, in this fixed order. A hit becomes `[redacted:<kind>]`:
   a. `pem`: a private key block, BEGIN line to END line or end of text; `url-login`: `scheme://user:password@`;
   b. `api-key`: keys with a provider prefix, and keys shaped like one (a short prefix, a long mixed body);
   c. `jwt`: three parts of 10 or more characters, starting `eyJ`; `bearer`, `basic`: `Bearer` or
      `Authorization: Basic` plus 8 or more characters; `webhook`: Slack and Discord webhook links; `url-key`: a
      URL query value of 8 or more under `key`, `sig`, `signature`, `auth`, `token`, `access_token` or `api_key`;
   d. `named`: a value after `=` or `:` behind a name that holds `password`, `passwd`, `pwd`, `secret`, `token`,
      `credential`, or `api`, `private`, `access` or `auth` with `key`. Not a value under 4 characters, a
      placeholder (`$TOKEN`, `<token>`, `***`), a variable's name or a path; not a name like `MAX_TOKENS`;
   e. `flag`: a value after `--password`, `--passwd`, `--pass`, `--token`, `--api-key`, `--secret` or
      `--client-secret`, and `user:password` after `-u` or `--user`;
   f. `sql-password`: the quoted value after `IDENTIFIED BY` or `PASSWORD`; `random`: a run of 32 or more key
      characters with upper case, lower case and digits in short pieces, not a hex digest, a path or a link.
4. Final cut. A text that step 2 cut first loses, after scrubbing, the 256 characters next to that cut, so the
   piece of a secret that the cut split (up to 256 characters of it) is dropped and never stored in part. Counting
   from the cut and not from the start matters: a marker can be shorter than the secret it replaced. Then the
   input, written as compact JSON, is cut to 4000 characters ending in `[...]`; a two-part error keeps at most the
   first 1500 of its first part, `[...]` on its own line, and at most the last 2493 of its second. No detector
   matches a marker. A last detector run turns a field that still matches into `[redacted:unsafe]`.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| agentmemory (Apache-2.0) | port | [`src/functions/privacy.ts` L30](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/privacy.ts#L30): the 14 value patterns (L31 to L44); L47 is the private key block and L50 the URL login |
| agentmemory (Apache-2.0) | port, one weakness not copied | [`src/hooks/_capture-filter.ts` L78](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture-filter.ts#L78): the secret key names; L81 to L95 split a key into words. Weakness: L72 and L121 return text and JSON at or under the cap unscrubbed |
| Trail-code (MIT) | port | [`src/notes/secrets.ts` L30](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/secrets.ts#L30): provider prefixes with their least lengths (L31 to L44); L55 digit keys; L221 secret names; L223 names about a secret; L237 flags; L244 plain values (L245: under 4 characters); L254 the random test; L278 vendor-shaped keys; L304, L309 and L311 the least length of 8; L334 the run of 32 |
| Trail-code (MIT) | port, changed | [`src/trace/redact.ts` L83](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/redact.ts#L83): redaction of a text that is sent whole; L97 joins the lines with no check of the joined result |
| tentaqles (new) | own design | the window, the last check, the `sql-password` rule, and one marker format |

**How to build it**

- Package `internal/lobe/scrub`: `scrub.go` (`Input(raw json.RawMessage) Clean`, `Text(s string) Clean`,
  `Detect(s string) (kind string, found bool)`), `rules.go` (the ordered table), `keys.go`, `cut.go`. Scrub the
  values, then write the JSON: a pass over written JSON can eat an escape character and break the line.
- `Clean` is the scrubber's output type, the only text F-01's store writers accept (F-01 step 8). Its field is
  unexported, so no other package can make one without passing through this one. The rule table is built on
  first use, not by an `init` function or a package-level call (core page 5.1).
- Go's `regexp` has no look-ahead or look-behind. The sources use them in `privacy.ts` L33 to L35 and in
  `secrets.ts` L44, L221, L227 and L256. Rebuild each as a match followed by a check of the neighboring character.
- Outcome from agentmemory, rules from both, least lengths as in the source lines. Trail refuses a text with a
  secret because "a scrubbed command no longer works" (`secrets.ts` L3 to L5); an event is never run, so we
  replace the secret and keep the rest. Not ported: agentmemory's `<private>` tags (`privacy.ts` L3 to L28).
- The union closes gaps on both sides. agentmemory lacks `gho_` and `ghr_` (`privacy.ts` L36 against
  `secrets.ts` L32) and needs a named value of 20 or more characters (L31), so `password=hunter2` passes. Trail
  takes a letters-only value under 16 characters for a plain word (L247), so `password: hunterhunter` passes.

**How we know it works**

- `TestLobeScrubParity`: 0 misses on rows that `test/trail-secrets.test.ts` and `test/privacy.test.ts` expect caught.
- `TestLobeScrubShortValue`: `password=hunter2` becomes `password=[redacted:named]`.
- `TestLobeScrubNoFalseAlarm`: a 40 character git hash, a UUID, `MAX_TOKENS=4096`, `$TOKEN` and a file path are
  unchanged: 5 of 5. Both fields at full window size are scrubbed in under 5 ms (proposed).
- `TestLobeScrubWindowEdge`: the error of tester check 5 is stored with no part of its last key. Yes or no.

**Tester brief**

Setup: the standard tester setup in F-07. Then run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`,
the login folder of `acme`: without it a lobe hook writes nothing (core page 5.1). `ACME` is `$W/clients/acme`;
`LOG` is `$TQ_HOME/identities/acme/lobe/events/events-<UTC date, YYYYMMDD>.jsonl`. From inside `ACME`, pipe each
JSON, `ACME` replaced, to `tentaqles lobe-hook claude post-tool-use-failure`. A JSON printed here over several
lines is one line: join the lines with a space. Expect exit 0, empty stdout.

1. `{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash",
   "tool_use_id":"toolu_01","tool_input":{"command":"curl -H \"Authorization: Bearer
   abcdefghijklmnopqrstuvwxyz012345\" https://api.example.test/v1/items?api_key=Zx81kQpL0029aaBB"},
   "error":"password=hunter2 rejected for postgres://app:s3cretPW@db.example.test:5432/main"}`
   Expect 1 line in `LOG`. The file holds none of `abcdefghijklmnopqrstuvwxyz012345`, `Zx81kQpL0029aaBB`,
   `hunter2` and `s3cretPW`, and it holds `[redacted:` at least 4 times.
2. The same with `toolu_02`, the tool `mcp__acme__ping`, `"tool_input":{"password":"hunter2","note":"hi"}` and
   the error `nope`. Expect line 2 with `input` `{"note":"hi","password":"[redacted:key]"}`.
3. Check 1 with `toolu_03`, the command `git show 3f2a9c1e5b7d4a6f8e0c1b2d3a4f5e6d7c8b9a0f && echo MAX_TOKENS=4096`
   and the error `fatal: bad object`. Expect line 3 with an `input` that holds that command unchanged.
4. Check 1 with `toolu_04` and this error: `x` 1000 times, a space, `ghp_` plus `A` 36 times, a space, `x` 5500
   times. Expect line 4 with an `error` of at most 4000 characters, with `[...]` and `[redacted:api-key]` and
   without `ghp_`.
5. Check 1 with `toolu_05` and this error, written with no line breaks: `ghp_` plus `A` 36 times and a space, 15
   times over; `x` 1124 times; a space; `ghp_` plus `B` 36 times; `x` 5000 times. The window cuts the last key
   after 16 of its 40 characters. Expect line 5 with an `error` that holds neither `ghp_` nor `BBBB`.

**Risks and open questions**

- A secret written as prose ("my password is X") is missed by both sources and by us. Key formats come from the
  two codebases and were not checked against the providers' own documents.
- This removes secrets, not client data. An error can still echo a row or a name. Events are private, capped and
  expire (S-07); the quality gate in C-04 is what keeps data out of a lesson.
- An input cut at 4000 characters is no longer valid JSON. A reader such as C-01 must treat an `input` that
  ends in `[...]` as text. A secret longer than 256 characters that the window splits can leave a piece behind.

## S-06 Duplicate filter

**Phase** 1 · **Origin** port: agentmemory · **Depends on** F-01, S-07

**What it is.** The same tool call can reach the lobe twice, and an agent stuck in a loop can fail the same way
twenty times in a minute. The duplicate filter stores each call once. It stores a repeat of the same content as a
short reference to the first event, without the text.

**Why it matters.** Consolidation counts and pairs events. A replayed call would inflate the counts, and a loop
would fill the log's bounded space (S-07) with copies of one error. The reference line keeps the count of real
repeats true for S-08's counts and M-01's error counters while the text is stored once.

**Mechanism**

1. Event id. With a call id from the host (`tool_use_id`): `evt_` plus the first 8 hex digits of SHA-256 over
   `host`, a zero byte, `session`, a zero byte, and the call id. Without one, the hash covers host, session, tool,
   kind, input and the hook's clock in nanoseconds, so two such events never share an id. An id is unique within
   a session only (core page 5.4): every pointer to an event uses `session` and `id` together.
2. With the store lock held (S-07 step 3), read the last 64 KiB (proposed) of today's event file and parse its
   whole lines.
3. Replay: a line with the same `session` and `id` is there. Nothing is written.
4. Repeat: a full line (one with no `dup`) has the same `session`, `tool`, `kind`, `input` and `fp`, and its `at`
   is at most 300 seconds earlier. A reference line is written: `v`, `id`, `at`, `session`, `host`, `tool`,
   `kind`, `fp`, and `dup` set to the first line's id. It has no `input` and no `error`.
   Neither case ends a post-tool hook early: `Record` returns its outcome (state `replay` or `ref`), and G-08's
   mark on an edited file and E-02's verdict still run.
5. Otherwise the full line is written.
6. The check is skipped, and the full line written, when `session` is empty, when the lock was not obtained in
   time, or when the tail cannot be read. Readers apply rule 3 again: of two lines with the same `session` and
   `id`, the first one counts.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| agentmemory (Apache-2.0) | port | [`src/functions/dedup.ts` L3](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/dedup.ts#L3): the 5 minute window; L38 to L45 build the key from session, tool, the first 500 characters of the input and a digest of the output |
| agentmemory (Apache-2.0) | port | [`src/capture/event-id.ts` L66](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/event-id.ts#L66): an id made from the host's own call id and the session; L50 is the digest |
| agentmemory (Apache-2.0) | weakness, not copied | [`src/functions/observe.ts` L96](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/observe.ts#L96): the content check is skipped when an event id is present, and its hooks always send one |
| agentmemory (Apache-2.0) | weakness, not copied | [`src/hooks/_capture.ts` L52](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/hooks/_capture.ts#L52): with no host id the hook's clock goes into the id, so the same content at two times is never a duplicate |
| tentaqles (new) | own design | reference lines, the tail read in place of a server's memory, the reader-side rule |

**How to build it**

- `internal/lobe/eventlog/dedupe.go`: `ID(host, session, callID string) string` and
  `Check(tail []Event, e Event) (v Verdict, firstID string)` with verdicts `new`, `replay` and `repeat`, both pure.
  `Event` has `Dup string` with `json:"dup,omitempty"`; `dup` is part of the event shape in core page 5.4.
- agentmemory keeps its window in the memory of a server that stays up (`dedup.ts` L29 to L36). The core page
  rules out a long-running service (section 11), so our window is the tail of the file, which any hook can read.
- A hit does not extend agentmemory's window: `observe.ts` L121 to L124 return before the entry is recorded
  again (`dedup.ts` L58 to L60 set the expiry). Ours behaves the same: a repeat is compared
  with full lines only, so a loop that lasts longer than 300 seconds writes one full line every 300 seconds.
- agentmemory compares the first 500 characters of the input (`dedup.ts` L41). We compare the stored input, up
  to 4000 characters after S-05, which merges fewer different calls.
- An `ok` event has no output to compare, so two successful `git status` calls 10 seconds apart are a repeat.
  That is intended: the reference line still says the call ran and succeeded, which is all E-02 needs.
- 8 hex digits are 32 bits, and that size is settled. It is enough inside one session and not enough across a
  client's whole log, so `id` is never a key alone: every join uses `session` and `id`, as E-02's trial lines
  and C-01's used-event lists do. E-02 can compute the same id before the tool runs.
- At UTC midnight the new file's tail is empty, so one duplicate can pass. Accepted.

**How we know it works**

- `TestLobeEventIDVector`: `ID("claude", "s1", "toolu_01")` is `evt_9bde2e09`.
- `TestLobeDedupeReplay`: the same payload twice gives 1 line.
- `TestLobeDedupeRepeat`: the same failure under two call ids, 10 seconds apart, gives 2 lines. The second has
  `dup` set to the first id, has no `input` and no `error`, and is under 250 bytes (proposed).
- `TestLobeDedupeWindow`: with a test clock, the same failure 301 seconds later gives 2 full lines.
- `TestLobeDedupeOtherSession`: the same content in another session gives 2 full lines.
- `TestLobeDedupeRace`: 20 processes send the same payload at once. A reader counts 1 event.

**Tester brief**

Setup: the standard tester setup in F-07. Then run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`,
the login folder of `acme`: without it a lobe hook writes nothing (core page 5.1). `ACME` is `$W/clients/acme`.
`LOG` is `$TQ_HOME/identities/acme/lobe/events/events-<today's UTC date, YYYYMMDD>.jsonl`. Run from inside `ACME`.
Pipe each JSON, with `ACME` replaced, to `tentaqles lobe-hook claude post-tool-use-failure`. A JSON printed here
over two lines is one line: join the lines with a space. Run the four checks within 5 minutes.

1. `{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping",
   "tool_use_id":"toolu_01","tool_input":{"n":4411},"error":"widget 4411 is not frobnicated"}`
   Expect exit 0, empty stdout, 1 line in `LOG` with `id` `evt_9bde2e09`.
2. The same JSON again. Expect exit 0 and still 1 line.
3. The same JSON with `toolu_02`. Expect 2 lines. The second has `id` `evt_8618533e`, `dup` `evt_9bde2e09`,
   `kind` `fail`, `fp` `fp_48622a622f4928cc`, and no `input` and no `error`.
4. The JSON of check 1 with `"session_id":"s2"`. Expect 3 lines. The third has `input` `{"n":4411}` and no `dup`.

**Risks and open questions**

- Every reader of the log must know reference lines. A reader that ignores `dup` sees events with no text.
- In a busy session 64 KiB can cover less than 300 seconds. Some repeats are then stored in full. Harmless.
- S-08 counts a reference line as one more occurrence of its fingerprint, and M-01's `failures` counter reads the
  same lines. A loop of 20 identical failures then weighs 20; whether it should weigh 1 is for M-01 to report.
- Two events of one session share an 8-digit id about once in 4 billion pairs. Readers then keep the first
  (step 6) and the second event is lost; nothing else breaks, because no pointer uses `id` without `session`.

## S-07 Event log that never stalls a hook

**Phase** 1 · **Origin** port: agentmemory · **Depends on** F-01, F-02, F-06

**What it is.** The event log is a folder of plain text files, one per UTC day, with one JSON object per line. A
hook adds its line with one write and exits. It never rewrites, sorts or compacts a file. Old files are deleted
whole once they are older than `retention_days`.

**Why it matters.** The second anti-goal is a hook that stalls a session, and the log is written on every tool
call. Decision A-9 says raw events expire, and principle 8 says logs are JSON lines a person can read.

**Mechanism**

1. The file is `events/events-YYYYMMDD.jsonl` in the client's `lobe` folder (core page 5.3), named by the UTC
   date of the event's `at`. Folder and file are created on first use, readable by the owner only where the
   system supports that.
2. The event is one line of JSON plus a line break. If the line is over 32 KiB (proposed), `input` and `error`
   are cut in half, at most 4 times (proposed). A line still too long is dropped with the reason `big`.
3. The hook tries the store lock (F-01) every 5 ms for at most 50 ms (proposed). With the lock it runs the S-06
   check and step 4. Without it, because another process holds it, it skips both and appends anyway.
4. Size, checked with the lock. At 8 MiB or more in today's file (proposed), `ok` events are no longer written.
   At 16 MiB or more (proposed), nothing is. Each skipped event is a drop with the reason `full`.
5. Append: open in append mode, write the whole line with one write call, close; no fsync. If the file's last
   byte is not a line break (an earlier write was cut short), a line break goes first, so only the torn line is lost.
6. On any error (disk full, no permission, path too long) Sense prints nothing and the hook still exits 0;
   `Record` returns an outcome with state `dropped`, which E-02 still judges. Each drop adds
   one line, `{"at":"...","why":"full"}`, to `events/drops-YYYYMMDD.jsonl`, when that write works. The reasons are
   `full`, `big`, `disk` and `no-exit` (S-02). The drops file is part of the store layout in core page 5.3: a
   count of events that could not be written, never their content. It stops at 64 KiB (proposed).
7. Expiry. The SessionEnd hook is an expiry sweep and nothing else: no event is ever buffered, so there is
   nothing to flush (core page 5.2). `tentaqles lobe-hook <host> session-end` runs the sweep, and so does the
   detached pass started at Stop, as its first step. That pass runs in this order (core page 5.2): this sweep,
   S-08's fold, consolidation, E-07's ladder moves, G-08's graph build, then G-11's document run from phase 5.
   The sweep deletes each `events-*.jsonl` and
   `drops-*.jsonl` dated more than `retention_days` days before today's UTC date (F-06: a whole number from 1 to
   365, 14 by default): with 14, on 2026-11-23 the file for 2026-11-08 goes and 2026-11-09 stays. Over 64 MiB
   (proposed), the oldest files go too. The sweep is skipped when the lock is held, deletes at most 64 files per
   run (proposed), and runs even when `capture` is `off`.
8. Readers take no lock. They read whole lines, skip blank lines and lines that are not JSON, and count those.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| agentmemory (Apache-2.0) | port | [`src/capture/spool.ts` L348](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L348): one JSON line written to a file opened in append mode; L332 builds the line |
| agentmemory (Apache-2.0) | port, changed | [`src/capture/spool.ts` L202](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L202): wait for a lock (500 ms, L110), then go on whether or not it was obtained; L341 skips the pruning without the lock |
| agentmemory (Apache-2.0) | not taken | [`src/capture/spool.ts` L351](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L351): an fsync on every append |
| agentmemory (Apache-2.0) | port | [`src/capture/spool.ts` L104](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L104): a 5 MiB cap and (L106) a 168 hour age limit; L346: when full, drop and count; L251: a reader skips a line that does not parse |
| tentaqles (new) | own design | day files, expiry by file name, two fill levels, the torn-line repair, the drops file |

**How to build it**

- Package `internal/lobe/eventlog`: `append.go` (`Append(dir string, e Event, now time.Time) error`), `read.go`
  (`Tail(path string, max int64) []Event` and a `Scan` over day files), `sweep.go`
  (`Sweep(dir string, retentionDays int, now time.Time)`), `drops.go`. No package state: a hook is a new process.
  The line goes to disk through F-01's append, which takes only the scrubber's `Clean` type (S-05).
- `tentaqles lobe events` (F-07 lists it with this entry as co-owner) prints the newest lines through `Scan`,
  each reference line shown with its first line. Its action `purge`, for a person only, deletes every
  `events-*.jsonl` and `drops-*.jsonl` of the client under the store lock. The person check sits in
  `internal/cmd/lobe_events.go`, as core page 5.1 asks of every person-only verb.
- agentmemory's file is a holding area for a server that is down (`src/hooks/_capture.ts` L121); ours is the log
  itself. We keep its form (JSON lines, a cap, an age limit) and leave out its drain process (`_capture.ts` L89
  to L102), its stats file rewritten on each drop (`spool.ts` L225) and its fsync: a crash loses only last lines.
- Whether one append-mode write can tear when 20 processes write 500 lines each on Windows is phase 0
  question 3. If it cannot, the lock in step 3 serves only S-06; if it can, readers rely on step 8. Record the
  result here first. F-01 returns `ErrBusy` when the lock is not obtained in time and leaves what follows to
  this entry: the event is appended anyway (step 3), because a lost event costs more than a line a reader may
  have to skip.
- The append is capture work: 150 ms at the 95th percentile, cut off at 1 second (core page 5.2, proposed; on the
  two post-tool events F-02 cuts the whole process off at 1.5 seconds). An identity folder on a network drive or
  in a synced folder can make any file call slow. F-02's cut-off is the only protection; each cut-off leaves a
  line in `stats/hook-errors.jsonl`, which F-07's `lobe-hook-failing` finding counts.
- F-07's list has the doctor finding `lobe-events-dropped` for this entry: a warning when the drops files hold 5
  or more lines from the last 24 hours (proposed, the threshold of `lobe-hook-failing`), with the count per reason.

**How we know it works**

- `TestLobeLogAppendParallel` (also the phase 0 test): 20 processes append 500 lines each; 10,000 lines, 10,000
  parse, 0 torn, on Linux, macOS and Windows.
- `TestLobeLogLockHeld`: with the lock held elsewhere for 2 seconds, `Append` returns in under 100 ms (proposed)
  and writes.
- `TestLobeLogDiskFull`: with a write error injected, the hook exits 0 with empty stdout; the file is unchanged.
- `TestLobeLogCaps`: at 8 MiB an `ok` is dropped and a `fail` written; at 16 MiB both are dropped, 1 line each.
- `TestLobeLogSweep`: retention 14, files 13, 14 and 15 days old: only the third is deleted; `notes.txt` stays.

**Tester brief**

Setup: the standard tester setup in F-07. Then run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`,
the login folder of `acme`: without it a lobe hook writes nothing (core page 5.1). `ACME` is `$W/clients/acme`;
`EVENTS` is `$TQ_HOME/identities/acme/lobe/events` and `LOG` is `EVENTS/events-<UTC date, YYYYMMDD>.jsonl`. Run
from inside `ACME`, with each JSON on stdin and `ACME` replaced. A JSON printed here over two lines is one line:
join the lines with a space. `FAILJSON` is
`{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping",
"tool_use_id":"toolu_01","tool_input":{"n":4411},"error":"widget 4411 is not frobnicated"}`

1. Pipe `FAILJSON` to `tentaqles lobe-hook claude post-tool-use-failure`. Expect exit 0, empty stdout, and `LOG`
   holds 1 line that parses as JSON and ends with a line break.
2. Create the empty files `events-20200101.jsonl`, `drops-20200101.jsonl` and `keep.txt` in `EVENTS`. Pipe
   `{"session_id":"s1","hook_event_name":"SessionEnd","cwd":"ACME","reason":"other"}` to
   `tentaqles lobe-hook claude session-end`: exit 0, empty stdout, both dated files gone, `keep.txt` and `LOG` kept.
3. Append the 5 characters `{"v":` to `LOG` with no line break. Pipe `FAILJSON` with `toolu_02` as in check 1.
   Expect 3 lines: the second is `{"v":` and the third parses as JSON with `id` `evt_8618533e`.
4. Delete the `EVENTS` folder and create an empty file named `events` in its place. Pipe `FAILJSON` with
   `toolu_03` as in check 1. Expect exit 0 and empty stdout, and `events` is still an empty file.

Person checks (the owner runs these at a terminal; a tester agent does not):

1. After checks 1 to 3, run `tentaqles lobe events purge` in `ACME` and answer what it asks. Expect `EVENTS` to
   hold no `events-*.jsonl` and no `drops-*.jsonl` file, and `keep.txt` to stay.

**Risks and open questions**

- An untorn append-mode write is assumed until phase 0 settles it. The sizes (32 KiB; 8, 16, 64 MiB) are estimates.
- Claude Code gives session-end hooks about 1.5 seconds by default (web, not machine-checked), Codex is reported
  to cap them at 3 seconds, and the event may not fire after a crash. The sweep works under the capture cut-off
  of 1 second, below both, and that is also why it runs in the pass after Stop.

## S-08 Error fingerprints

**Phase** 1 · **Origin** new, from Drain and error-tracker practice · **Depends on** S-05

**What it is.** A fingerprint is a short id for "the same error". One mistake prints different text each time: a
different path, line number, request id or table name. The lobe masks those parts, keeps the fixed words (the
*template*), and hashes the template together with the kind of tool (the *tool class*).

**Why it matters.** Mistakes with an outside API or MCP tool are keyed by the tool and the fingerprint (core page
section 4), lessons list fingerprints under `when`, and "seen 3 times" needs something to count. Three later
steps read the value from the event: R-04 shows a lesson after a failure whose fingerprint the lesson lists, E-02
judges a rule's fire by it, and C-01 uses it to tell a known mistake from a new one.

**Mechanism** (version `fp1`; every rule is proposed; worked examples are in the tester brief)

1. Tool class: `shell` (`Bash`, `PowerShell`), `edit` (`Edit`, `Write`, `MultiEdit`, `NotebookEdit`,
   `apply_patch`), `read` (`Read`, `Grep`, `Glob`), `web` (`WebFetch`, `WebSearch`), `mcp:<server>`
   (`mcp__<server>__<tool>`), else `other`.
2. Signature: one line of the scrubbed error text, color codes removed. (a) If the text from its first `{` parses
   as a JSON object: the text before it without its final `:`, then `status=`, `code=`, `type=` and `error=` for
   each such key holding text or a number, then the `message` (or `msg`, `detail`, `title`) value; keys are looked
   up at the top level, in `error`, then in `errors[0]`. (b) After a `Traceback` line, the last line starting in
   column 0. (c) Else the first line that holds, in any case, `error`, `fatal`, `panic`, `exception`, `fail`,
   `denied`, `not found`, `cannot`, `unable`, `invalid`, `unknown`, `refused` or `timed out`, skipping stack
   frames and a bare `Exit code N` line. (d) Else the first non-empty line, that bare line, or `<empty>`.
3. Masks, in this order: (1) an S-05 marker becomes `<secret>`; (2) a URL becomes `<url:HOST>`, HOST being the
   last two labels of its host name, or `ip`; (3) a quoted value ('...', "..." or backticks, at most 200
   characters, the opening quote not right after a letter or digit) becomes `<q>`; (4) set aside until (7): a
   number, or a token of 5 digits and capital letters, that follows `sqlstate`, `http`, `status`, `status code`,
   `code`, `error`, `errno`, `exit code` or `exit status` (any case) with at most 3 spaces, `:`, `=`, `#` or `(`
   between; and error codes of capital letters, an optional `-` and 1 to 6 digits (`ORA-00942`, `TS2345`); (5) a
   path becomes `<path>`: a token starting with `/`, `~/`, `./`, `../`, a drive letter or `\\`, or holding a slash
   and ending in a file extension or a slash, with any `:line:column` tail; (6) any other token with a digit
   becomes `<n>` when it is only digits and `. , : + -`, else `<id>`; (7) restore (4), collapse spaces, trim, cut
   to 200 characters. In (5) and (6) a token has no spaces, and a final `:`, `,`, `.` or `)` stays outside it.
4. `fp` is `fp_` plus the first 16 hex digits of SHA-256 over `fp1`, a zero byte, the tool class, a zero byte and
   the UTF-8 template. The hook only writes `fp` into the event and into Sense's outcome. The detached pass
   started at Stop folds new events into `fingerprints.json` as its second step, after S-07's sweep and before
   consolidation (core page 5.2), under the store lock.
   An entry has the core page's shape, `{id, tool_class, template, count, sessions, first_seen, last_seen}`, and
   a reference line (S-06) counts as one more occurrence.

**When Drain-style clustering is added.** Drain groups log messages by token count and leading tokens and merges
similar ones. It is not in phase 1. In phase 2, measure `fp1` on at least 200 hand-labeled failures from at least
3 tool classes (both proposed; from C-07's backfill). If over 10% of same-cause pairs get different fingerprints
(proposed), add Drain in the detached pass as a merge layer: within one tool class and token count, templates
sharing 0.4 or more of their tokens (Drain3's default; web) form one cluster, kept as `alias_of` in
`fingerprints.json`; `fp1` itself does not change. If over 5% of same-fingerprint pairs differ in cause
(proposed), make a new mask list, `fp2`.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Drain3 | idea; web, not machine-checked | [drain.py](https://raw.githubusercontent.com/logpai/Drain3/master/drain3/drain.py): an ordered mask pass before tokens are compared; tokens with digits become wildcards; similarity threshold 0.4 by default |
| Sentry | idea; web, not machine-checked | [event grouping](https://docs.sentry.io/concepts/data-management/event-grouping/): the exception type and value come before the message, the order step 2(a) follows. Its pages list no masking rules |
| Trail-code (MIT) | port | [`src/trace/failure.ts` L59](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L59): pick the first line that says it failed; a bare `Exit code N` line (L56) is only a fallback |
| tentaqles (new) | own design | the tool classes, the set-aside step, the JSON signature, the hash recipe |

**How to build it**

- Package `internal/lobe/fingerprint`: `Class`, `Signature`, `Template` and `Of(tool, errText) (fp, template)`,
  all pure; `Fold(lobeDir string)` keeps its read position in `fingerprints.json`.
- Use a hand-written scanner: mask (3) checks the character before a quote, which Go's `regexp` cannot. The mask
  order is part of `fp1`; changing steps 1 to 4 needs a new prefix and a plan for lessons that list old values.
- The template stays in `fingerprints.json`, which is private; a lesson holds only the hash. Masking quoted
  values is the core page's rule (5.4). It merges `near '('` with `near 'SELECT'`: right for counting, a limit
  for lessons (see Risks).
- New verb `tentaqles lobe fingerprint --tool <name>`: stdin is an error text, stdout the `fp` and the template.
  It reads no store and needs no client. Any caller may run it, so it sits with F-07's other verbs in
  `internal/lobe/cli`; F-07's table lists it.

**How we know it works**

- `TestLobeFingerprintVectors`: rows 1 to 3 of the tester table, each as written and with its change, are 6
  texts that give 3 values; rows 4 and 5 give their own. `TestLobeFingerprintStable`: 50 generated variants of
  each of rows 1 to 3 (other paths, numbers, ids, quoted values) give 1 fingerprint per row, 150 of 150.
- `TestLobeFingerprintDistinct`: 20 pairs with different causes, among them `HTTP 404` and `HTTP 429`,
  `SQLSTATE: 42601` and `SQLSTATE: 42P01`, `ENOENT` and `EACCES`, give 20 pairs of different fingerprints.

**Tester brief**

Setup: the standard tester setup in F-07. Run from `$W/clients/acme`; the verb itself needs no client and no
login folder. For each row, send the error text on stdin to `tentaqles lobe fingerprint --tool <tool>`. Expect
exit 0 and exactly two lines on stdout: the `fp`, then the template. Apply the row's change, if any, and expect
the same two lines. The texts were written for this page.

| # | `--tool` | Error text | Change | Template | `fp` |
|---|---|---|---|---|---|
| 1 | `Bash` | two lines: `Exit code 127` and `/usr/bin/bash: line 1: Get-ChildItem: command not found` | drop the first line; `/bin/bash`; `line 12` | `<path>: line <n>: Get-ChildItem: command not found` | `fp_b4f3e92d90787db6` |
| 2 | `mcp__warehouse__query` | `[PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)` | `'SELECT'`; `(line 1, pos 41)` | `[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)` | `fp_f44eaf6b555b0cd2` |
| 3 | `mcp__tracker__create_issue` | `HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.","request_id":"req_8f3a9c21d0b4"}}` | `91cc`; `3 seconds`; `req_02b7e6a955f1` | `HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds.` | `fp_c9f4aa346d1ec77f` |
| 4 | `Bash` | the text of row 2 | none | the template of row 2 | `fp_366015c0d74849a2` |
| 5 | `mcp__tracker__create_issue` | the text of row 3 with `429 Too Many Requests` changed to `404 Not Found` | none | `HTTP 404 Not Found code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds.` | `fp_8c90d679b4fe1c9f` |

**Risks and open questions**

- Nothing here is measured: no corpus yet, example texts written by hand, web sources read through a fetch tool,
  and the Drain paper not read. The error words are English only.
- An unquoted name without digits (`relation orders does not exist`) stays in the template and splits one cause
  by table name; Drain is the planned answer. One engine error through two tools gives two fingerprints (row 4).
- Masking quoted values merges different SQL syntax errors of one engine: `near '('` and `near 'SELECT'` share a
  template and so a fingerprint (row 2). That is wrong for lessons in three places. C-01 sends a repair whose
  fingerprint an active lesson already lists to C-05 and C-06 and drafts nothing, so the second syntax mistake
  never gets a lesson of its own. R-04 shows the first mistake's lesson after the second one's error. E-02 counts
  the second mistake's failure as a hit for the first one's rule whenever that rule fired on the call.
- If that merge matters, M-04 shows it in its SQL dialect family whenever two traps of one stub warehouse print
  errors that differ only inside the quotes: fewer lessons filed than traps met, a repeat rate for the trap
  learned second that stays near the `identity` arm at the second, third and fourth meeting, and advice shown on
  calls it does not fit. If M-04 shows none of that, the merge is harmless. The answer would be a new mask list,
  `fp2`, that keeps a short quoted token after `near` (proposed); Drain cannot help, because it only merges.
