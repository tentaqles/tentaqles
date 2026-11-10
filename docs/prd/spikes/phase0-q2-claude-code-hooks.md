# Phase 0, question 2: which hook events and fields really arrive

The question, from the [core page](../vertical-lobe.md), section 8: which hook events and fields really arrive
for failed shell, edit and MCP calls in Claude Code and Codex? The design of [S-01](../lobe-sense.md) and
[F-03](../lobe-foundation.md) rests on Claude Code's hooks page, which is a web source. This page records what
has been confirmed on the owner's machine and what still rests on the page.

## Confirmed on the owner's machine, 2026-11-09

Two real failed shell commands in a Claude Code session, with the checkout's plugin loaded through
`claude --plugin-dir` and the development build answering the hook:

| Fact | Seen |
|---|---|
| A failed `Bash` call fires `PostToolUseFailure` | yes, once per failed call, and nothing twice |
| The payload carries `cwd`, `session_id`, `tool_name`, `tool_use_id`, `tool_input` and `error` | yes: the event was built from them and holds the command, its `description` and the error |
| `error` starts with a line `Exit code N` | yes: `ls` on a missing folder gave `exit` 2, `echo ...; false` gave `exit` 1, and the stored error holds only the text after that line |
| `tool_input` arrives as a JSON object with `command` and `description` | yes |
| The hook writes nothing for other clients | yes: one client's memory folder exists, the four others have none |
| A made-up GitHub token in the command and in the output is redacted | yes: `echo ghp_A...; false` stored `echo [redacted:api-key]; false` and an error of `[redacted:api-key]`; no `ghp_` under any memory folder |
| `--plugin-dir` loads a checkout's plugin on the owner's Claude Code version | yes |

Not seen in these runs, because the owner's folder had no `.git` at or above it inside the client folder: the
repository key. Its walk is covered by `TestLobeSenseLocate`.

## Confirmed by the capture session, 2026-11-09

Two capture sessions on the owner's machine (Claude Code v2.1.321, `bypassPermissions` mode), in a scratch
folder inside one client, with `spike/hook-capture/`. Field names and types only, no values, except where a
row says the owner's own scratch transcript was read.

| Fact | Seen |
|---|---|
| **An `Edit` whose `old_string` is not in the file fires no hook at all** | Two Edit calls on an existing `notes.txt`; the transcript holds both `tool_use` blocks and a `tool_result` with `is_error: true` and `<tool_use_error>String to replace not found in file.`; the capture holds no `PreToolUse`, `PostToolUse` or `PostToolUseFailure` for `Edit`. Claude Code checks the input before any hook runs, so a hook never sees this failure; only the transcript does |
| `PostToolUseFailure` for `Bash` | fields `cwd`, `duration_ms`, `effort` (object), `error`, `hook_event_name`, `is_interrupt` (boolean, present, `false`), `permission_mode`, `prompt_id`, `scratchpad_dir`, `session_id`, `tool_input` (`command`, `description`), `tool_name`, `tool_use_id`, `transcript_path`; `error` first line `Exit code N` in every payload |
| `PreToolUse` | the same fields without `error`, `is_interrupt`, `duration_ms` |
| `PostToolUse` for `Write` | adds `duration_ms` and `tool_response` (object: `type`, `filePath`, `content`, `structuredPatch`, `originalFile`, `userModified`) |
| `SessionStart` | `source` `startup`, plus `model`, `cwd`, `session_id`, `transcript_path`, `scratchpad_dir` |
| `SessionEnd` | `reason` `prompt_input_exit` after `/exit` |
| `Stop` | `background_tasks`, `last_assistant_message`, `session_crons`, `stop_hook_active` (`false`), `effort`, `permission_mode`, `prompt_id` |
| `UserPromptSubmit` | `prompt`, `permission_mode`, `prompt_id` |
| `Notification` | `message`, `notification_type` (`idle_prompt`) |
| `transcript_path` | on every event; the transcript line pairs `tool_use.id` with `tool_result.tool_use_id` and carries `is_error` |

Not seen: a declined call (`bypassPermissions` asks nothing), an interrupted call, any MCP call, a failed
`WebFetch`, and an `Edit` that fails while running rather than at its input check (for example a file that
cannot be written).

## Confirmed by the success capture, 2026-11-10

A third capture session (same machine and mode, `spike/hook-capture/`), one successful call per tool. Field
names and types only. Every successful call fired `PostToolUse` once, with `cwd`, `duration_ms`, `effort`,
`hook_event_name`, `permission_mode`, `prompt_id`, `scratchpad_dir`, `session_id`, `tool_input`, `tool_name`,
`tool_response`, `tool_use_id` and `transcript_path`; an MCP call adds `mcp_server` (object). No
`PostToolUseFailure` fired.

| Tool | `tool_input` fields | `tool_response` |
|---|---|---|
| `Bash` | `command`, `description`, `run_in_background` | object: `stdout`, `stderr`, `interrupted`, `isImage`, `noOutputExpected`, `backgroundTaskId` |
| `PowerShell` | `command`, `description` | object: `stdout`, `stderr`, `interrupted`, `isImage` |
| `Edit` | `file_path`, `old_string`, `new_string`, `replace_all` | object: `filePath`, `oldString`, `newString`, `originalFile`, `replaceAll`, `structuredPatch`, `userModified` |
| `Write` | `file_path`, `content` | object: `type`, `filePath`, `content`, `structuredPatch`, `originalFile` (null for a new file), `userModified` |
| `Read` | `file_path` | object: `type`, `file` (object) |
| `Grep` | `pattern`, `path`, `output_mode` | object: `mode`, `content`, `filenames`, `numFiles`, `numLines`, `totalLines` |
| `Glob` | `pattern`, `path` | object: `filenames`, `numFiles`, `totalMatches`, `truncated`, `countIsComplete`, `durationMs` |
| `WebFetch` | `url`, `prompt` | object: `url`, `code`, `codeText`, `bytes`, `result`, `durationMs` |
| `ToolSearch` | `query`, `max_results` | object: `query`, `matches`, `total_deferred_tools` |
| `mcp__*` | `query` | string |

| Fact | Seen |
|---|---|
| **A command started with `run_in_background` reports its start as an object with `backgroundTaskId`** | the one `Bash` call was the background `sleep`; its result holds `backgroundTaskId` (string) and none of the texts `Command running in background with ID:`. Feature 008 tells a start apart by a non-empty `backgroundTaskId`, and keeps the texts as a second check |
| The field names the 008 projection keeps | `command` (shell), `file_path` (edit, write, read), `pattern` and `path` (grep, glob), `url` (web fetch) all arrive under those names |

Not seen: `NotebookEdit`, `MultiEdit`, `WebSearch`, a `PowerShell` call in the background, and a run moved to
the background after its timeout.

## Confirmed for feature 009, 2026-11-10

Two facts this feature needs (task 2). The shell result form is in the success capture above: `Bash` and
`PowerShell` hand `tool_response` as an object whose standard output and standard error are `stdout` and
`stderr`. The MCP rows come from a fourth capture session (same machine and mode), with the made-up server
`spike/hook-capture/mcp_fail_server.py` (named `capture`, `setup.py --mcp`), whose three tools answer with fixed
text:

| Call | Event | What arrived |
|---|---|---|
| `flag_error`: a normal result with `isError` true | `PostToolUseFailure`, once | `error` (string) holds the tool's text; no `tool_response`; `is_interrupt` `false` |
| `rpc_error`: a JSON-RPC error answer | `PostToolUseFailure`, once | `error` (string) holds the error's message; no `tool_response`; `is_interrupt` `false` |
| `ok_echo`: a normal result | `PostToolUse`, once | `tool_response` is an array (the content parts); the earlier capture's MCP success arrived as a string |

So on Claude Code an MCP tool's error is a failure, recorded by 004 as `fail`, and never a `PostToolUse` with an
error flag. Rule A of S-04 is kept as written (it costs nothing unused and Codex may need it, 010), but on Claude
Code it is a side path that this capture never saw fire. Every MCP payload also carries `mcp_server` (object).

## Still from the hooks page only (web, not machine-checked)

Each feature that needs a row confirms it before it is closed.

| Situation | What the page says | Needed by |
|---|---|---|
| A failed `WebFetch`, or an `Edit` that fails while running | `PostToolUseFailure` with the same fields; `error` is the tool's own text, with no `Exit code` line. An `Edit` that fails its input check fires nothing, and a failed MCP call is a `PostToolUseFailure` (both confirmed above) | 004, 063 |
| A hidden failure (`npm test \| tail`) | `PostToolUse`; the error is inside `tool_response`. An MCP error flag does not arrive here on Claude Code (confirmed above) | 009 |
| A call interrupted while running | the page says cancelling a running tool does not fire `PostToolUseFailure`; the tool result carries the interruption text | 004 |
| A call declined at the permission prompt | `PermissionDenied` (and `PermissionRequest` before it); no `PostToolUseFailure` | 004 |
| `is_interrupt` | optional boolean on `PostToolUseFailure`, true when the failure reached Claude Code as an abort | 004 |
| A prompt | `UserPromptSubmit` with `prompt` | 027 |
| Session start and end | `SessionStart` with `source` (`startup`, `resume`, `clear`, `compact`, `fork`); `SessionEnd` with a reason (`clear`, `resume`, `logout`, `prompt_input_exit`, `other`) | 011, 026 |
| `Stop` | fires when the agent finishes its turn | 018 |
| The hook's own output | `additionalContext` inside `hookSpecificOutput` is added to what the agent reads; `PostToolUseFailure` has no deny | 014 |
| Codex | one post-tool event, with the exit code in a text header | 010 |

## What it means for feature 003

The one row feature 003 stands on, a failed shell call on `PostToolUseFailure` with `Exit code N`, is confirmed
by two real runs. The other rows are for later features, each of which confirms its own before it is closed.

## What it means for feature 063

063 rests on the confirmed transcript rows (the `tool_use`/`tool_result` pairing, `is_error`, the `<tool_use_error>`
wrapper, `transcript_path` on every event); the transcript is not a documented interface, so a change in Claude Code
silences 063 without breaking a session.
