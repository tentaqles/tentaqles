# Phase 0, question 2, for Codex: what Codex's hooks receive

Feature 010, build task 2. The owner ran one Codex session on 2026-11-10 against a scratch `CODEX_HOME`
written by `spike/hook-capture/codex_setup.py --mcp`, with `codex_capture.py` registered for all 14 events of
the Codex hooks page. `reduce.py` turned the payloads into counts and field names; no payload left the owner's
machine, and the scratch folder was deleted after the run.

- Codex CLI 0.154.0 (npm), Windows 11. The shell tool ran PowerShell (its error text was PowerShell's).
- Started with `--dangerously-bypass-hook-trust`. Codex showed "1 startup issue"; it was not opened.
- 11 prompts were planned; 10 `UserPromptSubmit` payloads arrived (one prompt was not sent).
- 43 payloads in 7 files.

## Events that fired

| Event | Payloads | Fields (all strings unless noted) |
|---|---|---|
| SessionStart | 1 | `cwd`, `hook_event_name`, `model`, `permission_mode`, `session_id`, `source` (`startup`), `transcript_path` |
| UserPromptSubmit | 10 | `cwd`, `hook_event_name`, `model`, `permission_mode`, `prompt`, `session_id`, `transcript_path`, `turn_id` |
| PreToolUse | 9 | `cwd`, `hook_event_name`, `model`, `permission_mode`, `session_id`, `tool_input` (object), `tool_name`, `tool_use_id`, `transcript_path`, `turn_id` |
| PermissionRequest | 8 | as PreToolUse, without `tool_use_id` |
| PostToolUse | 6 | as PreToolUse, plus `tool_response` |
| Stop | 8 | `cwd`, `hook_event_name`, `last_assistant_message`, `model`, `permission_mode`, `session_id`, `stop_hook_active` (boolean, always false), `transcript_path`, `turn_id` |
| SessionEnd | 1 | `cwd`, `hook_event_name`, `reason` (`other`), `session_id`, `transcript_path` |

Never fired: SubagentStart, SubagentStop, PreModelRequest, PostModelRequest, PreCompact, PostCompact,
Interrupt. Nothing in the run needed a subagent or a compaction; the two model-request events fired zero times
across 10 turns, so in 0.154.0 they either do not exist or need something the config did not have. 8 `Stop`
payloads for 10 turns: two turns ended without one (not explained by the run).

`permission_mode` was `default` in every payload. `session_id`, `cwd` and `tool_use_id` are present in every
tool payload (`tool_use_id` is missing only from PermissionRequest).

## Per call kind

| Call | Tool name as Codex writes it | PreToolUse | PermissionRequest | PostToolUse | `tool_response` |
|---|---|---|---|---|---|
| Failed shell command (`ls` of a missing folder) | `Bash` | yes | yes | yes | string |
| Successful shell command | `Bash` | yes | yes | yes | string |
| Long command (`ping -n 40`) | `Bash` | yes | yes | yes, once, after it ended | string |
| Failed `apply_patch` | `apply_patch` (`tool_input.command`, a string) | yes | no | **no** | none |
| MCP tool returning `isError` (`flag_error`) | `mcp__capture__flag_error` | yes | yes | **no** | none |
| MCP tool raising a JSON-RPC error (`rpc_error`) | `mcp__capture__rpc_error` | yes | yes | **no** | none |
| Successful MCP tool (`ok_echo`) | `mcp__capture__ok_echo` | yes | yes | yes | object with `content` (array); no `isError` |
| Unknown MCP tool | none: Codex failed it before any call ("is not a function") | no | no | no | none |

The model runs tools through Codex's code-mode `exec` tool, but the hooks see `Bash` and the MCP name
`mcp__<server>__<tool>`, the same form as Claude Code.

## The exit code of a shell call

- The `Bash` `tool_response` is the command's plain output. In 5 of 5 payloads it held none of the header
  forms: `Process exited with code N` 0, `Exit code: N` 0, `Process running with session ID N` 0, a line that
  is exactly `Output:` 0, `Wall time` 0, `Chunk ID` 0. It never parsed as JSON, so the `metadata.exit_code`
  form was seen 0 times.
- No field of the PostToolUse payload states an exit code.
- The failed `ls` reached PostToolUse like the successful calls; nothing in the payload tells them apart
  except the text.
- The session file does hold the code. In the owner's first attempt the same day (run against the client's
  real Codex home by mistake, no hooks loaded), the rollout file under `CODEX_HOME/sessions` has, per shell
  command, an `event_msg` of type `item_completed` whose `item` has `type` `CommandExecution` and the keys
  `aggregated_output`, `command`, `cwd`, `duration`, `exit_code`, `formatted_output`, `id`, `parsed_cmd`,
  `process_id`, `source`, `status` (`completed` or `failed`), `stderr`, `stdout`. 6 such items: 1 non-zero
  code, 5 zero. Only key names and counts were read.
- The hook can find it. A second run the same day (3 shell calls: a failed `ls`, `echo`, a short `ping`),
  with a probe in the PostToolUse hook that looked up the call's `tool_use_id` in its `transcript_path`:
  - `tool_use_id` and an existing `transcript_path` in 3 of 3 calls; the transcript was under `CODEX_HOME` in
    3 of 3;
  - a `CommandExecution` item whose `item.id` equals `tool_use_id` and that has a whole-number `exit_code`, on
    the first look, before any wait: 3 of 3; its code was non-zero once (the `ls`) and zero twice;
  - the id appeared only at `event_msg` / `item_completed`: `payload.item.id`, never at the model's
    `custom_tool_call` `call_id`.
- A still-running command: not seen. `ping -n 40` ran its 40 seconds inside one call, and one PostToolUse
  arrived after it ended.

## What Codex does with a PostToolUse hook's result

| The hook | What Codex showed | The tool result |
|---|---|---|
| Wrote to stderr and exited 2 (`PROBE_EXIT2`) | "Hook failed — hook exited with code 1" | unchanged; the model reported the output and exit code 0 |
| Printed plain text to stdout (`PROBE_PRINT`) | nothing | unchanged |
| Slept 10 s under a timeout of 5 (`PROBE_SLOW`) | "Hook failed — hook timed out after 5s" | unchanged |

- The exit 2 reached Codex as code 1, so the block-and-replace of an exit 2 did not happen on Windows. The
  layer that changes the code is PowerShell's `-Command` (see "The shell a hook runs in").
- A hook that fails or times out is shown to the person and does not stop the turn.
- Child processes: the `PROBE_SLOW` hook started a child, in its own process group, that would write a marker
  file after 15 s. 0 marker files after the run: Codex ended the child with the hook.

## For later features

- Session end (036): the SessionEnd hook wrote its payload, then slept 5 s under a timeout of 3. The late
  marker was not written: Codex cut it.
- Added context (036): not tested; the only stdout probe was plain text, which Codex ignored.
- Environment (F-07): the hooks saw `CODEX_HOME`, `CODEX_MANAGED_BY_NPM` and `CODEX_MANAGED_PACKAGE_ROOT`.
  No variable names a session or says "agent". `CODEX_MANAGED_BY_NPM` is set by the npm launcher, so it is not
  a session marker.

## Handler label and hash

Read with `codex app-server` (`hooks/list`) from a scratch `CODEX_HOME` with Codex 0.154.0, as the comment at
`internal/codexconfig/codexconfig_test.go` L17 describes, for a handler of type `command`, timeout 5, alone
in table `PostToolUse` (label in the hook key: `post_tool_use`):

| Handler | `currentHash` |
|---|---|
| `tentaqles lobe-hook codex post-tool-use; exit 0` (the one 010 renders) | `sha256:36e35d7278d88e1f884c1b818070a0ced16a988cb18ca3d9d8d29595a6163f47` |
| `tentaqles lobe-hook codex post-tool-use \|\| exit 0` (the brief's first form, dropped) | `sha256:140302254c16dbede2f2fbc92386c1d2db71db32d62b2012c0237dfb24810bfe` |

## The shell a hook runs in

Codex 0.154.0 starts a command hook in the session's shell (`codex-rs/core/src/session/mod.rs`): on this
machine `powershell.exe -NoProfile -Command "<handler>"`, Windows PowerShell 5.1, since PowerShell 7 is not
installed. With no session shell it falls back to `COMSPEC /C` or `$SHELL -lc`
(`codex-rs/hooks/src/engine/command_runner.rs`). This explains the exit 2 seen as 1: `-Command` turns a
failed native command into exit 1. Checked on the owner's machine: under `powershell.exe -NoProfile
-Command`, `<cmd> || exit 0` is a parse error (exit 1), and `cmd /c exit 2; exit 0` exits 0.

## Checked again on Codex 0.167.1 (2026-11-11)

Feature 010's real-session check ran after Codex updated itself to 0.167.1, in the tqx playground. With
the rendered handler and no bypass flag, the hook ran (so 0.167.1 trusted the hash above), found the
`CommandExecution` line by `tool_use_id`, and wrote one `fail` line with `exit` 1. 0.167.1 also starts a
background server whose socket lives under `CODEX_HOME`; a path of about 108 characters or more makes it
fail to start, and `codex --no-daemon` works around that.

## Against the 010 brief

- Steps 13 and 14 expect the exit code in the `Bash` result text. In 0.154.0 it is not there, so every shell
  call would land in the "No code named" row: a failed command is written only if `Hidden`'s text rules fire.
  The owner chose on 2026-11-10 to read the code from the session file instead; the second run shows the
  lookup by `tool_use_id` works.
- Step 15 expects `apply_patch` and MCP errors to reach PostToolUse with an error flag. They do not reach
  PostToolUse at all.
- Step 2 says Codex starts a hook through cmd on Windows. It uses the session's shell, PowerShell here, where
  `|| exit 0` does not parse; the brief now renders `; exit 0`.
- Step 9's six events all exist and fired (SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Stop,
  SessionEnd).
- Step 11's payload fields are all present.
