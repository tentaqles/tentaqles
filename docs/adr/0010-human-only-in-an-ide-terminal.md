# ADR-0010: Human-only commands work in an IDE terminal

Status: accepted
Date: 2026-11-06
Cites: ADR-0003 (the three detectors), `docs/threat-model.md` boundary B2

## Context

A real run on 2026-11-06 found two false refusals in the human-only check.

- `CLAUDE_CODE_SSE_PORT` was one of the environment markers. The Claude Code IDE integration sets
  it in a VS Code integrated terminal, so a human typing in that terminal was refused every
  human-only command.
- The process-tree walker split an interpreter's command line on path separators, so
  `powershell -File C:\...\claude\...\walk.ps1` read as a harness because of a folder name.

## Decision

- `CLAUDE_CODE_SSE_PORT` is no longer a marker, in `internal/markers`, `plugin/tentaqles/tqhome.py`
  and `plugin/scripts/tq_env.sh`.
- In an interpreter's command line, only the last part of a path counts as a harness word.
  `/usr/bin/claude` and `C:\npm\claude.cmd` still match. `C:\x\claude\run.ps1` does not.

## Why an agent is still caught

- An agent's shell carries `CLAUDECODE`, which stays a marker, and the process tree finds the
  harness above it, inside an IDE as well as outside. A human-only command needs a terminal on
  stdin and stdout and a typed yes too, and the hook denies it by name.
- A script under a `claude` folder that starts a harness still has that harness in its own chain,
  so the walker finds it by name.

## Consequences

- `TQ_HOME` is now honoured in an IDE terminal that has only this variable set, as in any human
  terminal. A Claude session started there sets `CLAUDECODE`, so it ignores `TQ_HOME` as before.
- An interpreter whose command line names a harness only through a folder is no longer flagged by
  that word. It remains caught when the harness is a real process in the chain, or by a marker.
