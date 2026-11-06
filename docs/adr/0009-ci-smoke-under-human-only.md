# ADR-0009: The CI smoke test asserts the human-only refusal

Status: proposed

## Context

The `go` job's smoke steps ran `tq init` and `tq add` on the runner, then checked that the shell
hook activated the new workspace. Since ADR-0003 those verbs refuse without a human: stdin and
stdout must be terminals, the process tree must reach a terminal, and `tq add` reads a typed yes
from the terminal itself. A runner has none of these, so the steps can no longer pass.

Faking a terminal (a PTY plus a user anchor naming the wrapper) would build the bypass ADR-0003
exists to prevent, in the repository's own CI, and has no Windows equivalent.

## Decision

The smoke steps assert what holds on a runner:

- `tq init` and `tq add` exit non-zero and `config.yaml` is not written.
- `tq activate` loads in the real shell, and the hook leaves `TQ_WS` unset in folders that are no
  workspace.

The matrix sets `fail-fast: false`, so one OS failing does not cancel another's result.

## Consequences

- CI now checks the refusal with the real binary in a real non-interactive process.
- No CI step activates a real workspace through the real binary and a real shell any more. The Go
  tests cover activation through the `tq env` and shell-hook tests, with the human-only seams
  replaced. A manual run in a terminal, `docs/COMMANDS.md`'s walkthrough, is what covers the rest.
- A red OS costs a full matrix run rather than being cancelled early.
