# 022: An agent can't run a command under another client

**Status:** landed
**Risk:** High: from one client's agent session, `tq run <other> -- <cmd>` runs under the other client's identity and credentials, which is what the product exists to prevent
**UI:** none

A plain literal command crosses clients today. `docs/IDEATION.md`: "If during reasoning and research Claude finds those tools through another company's identity, a hook blocks it". `docs/prd.md` FR-13 says this is refused, but `TestRunFromAgentSession` only launches the agent's own workspace. A human's `tq run <other>` from any folder is the command's purpose and stays.

A working fix exists: `C:\repos\tentaqles\tentaqles\.claude\worktrees\agent-af6cad90c98e77445\.job\evidence\fix.patch`. Start from it.

## Requirements

- **REQ-1**: In an agent session, by any `internal/humanonly` agent detector (a marker or the process tree), `tq run <ws> -- <cmd>` must refuse with exit 1 when `<ws>` is not the workspace of the current folder, and start nothing.
- **REQ-2**: The refusal must read `tq run: <ws> is another client. An agent session can only run under the client of its own folder.`, and log one line in the denial log with category `identity`.
- **REQ-3**: An agent session in a neutral folder naming any client must be refused the same way.
- **REQ-4**: An agent session must still run `tq run . -- <cmd>` and `tq run <its own client> -- <cmd>`.
- **REQ-5**: A human's `tq run <any client> -- <cmd>`, from any folder, must keep working with no prompt.
- **REQ-6**: The check must run before the workspace's environment is built, so an error can't leak another client's values.

## Out of scope

- `cd <other> && tq run <other>` in one shell line: the process's own folder decides. It's a text-filter matter (AR2), and the ADR records it.
- A hook rule for this. The command is the check.

## Affected areas

- `internal/cmd/run.go`: the refusal
- `internal/cmd/run_test.go`, `internal/cmd/humanonly_test.go`: the tests, and moving `tq run <ws>` out of `TestFreeCommandsRunUnderAMarker`
- `docs/COMMANDS.md`, `docs/threat-model.md`, `docs/prd.md`, `CHANGELOG.md`, `docs/adr/0020-run-other-client.md`
- `CLAUDE.md`: Must not trade off names the refusal's test

## Do not touch

- `internal/policy/`, `internal/guard/`: no rule changes.
- `internal/humanonly/`: reuse its detector.

## Decisions

- **Decided by the ticket writer**: compare with the process's own folder, not the inherited `TQ_WS`. An agent can set a variable, so the folder is the stronger signal, and the hook already judges a `cd` to another client's root.
- **Decided by the ticket writer**: the refusal uses the denial layer `command` if ticket 016 has landed, otherwise `human-only`. Either way the category is `identity`.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 022` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
