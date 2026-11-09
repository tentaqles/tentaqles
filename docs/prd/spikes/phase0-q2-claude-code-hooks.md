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

## Still from the hooks page only (web, not machine-checked)

These rows wait for the owner's capture session (`spike/hook-capture/README.md`), which records every hook
event's field names and types without their values. Each feature that needs a row confirms it before it is
closed.

| Situation | What the page says | Needed by |
|---|---|---|
| A failed `Edit`, `WebFetch` or MCP call | `PostToolUseFailure` with the same fields; `error` is the tool's own text, with no `Exit code` line | 004 |
| A successful call | `PostToolUse` with `tool_input`, `tool_response` and `tool_use_id` | 008 |
| A hidden failure (`npm test \| tail`, an MCP error flag) | `PostToolUse`; the error is inside `tool_response` | 009 |
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
