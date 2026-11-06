# ADR-0013: Claude Code is the only harness

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md`, ADR-0004 (signed record), ADR-0005 (self-protection), ADR-0006 (deny only)

## Context

IDEATION names Claude Code and no other harness. tq carried a Codex launcher, hook answer format,
config render, MCP filter, doctor checks and `apply_patch` parsing, all of it code a guard has to keep
correct.

## Decision

All of that is removed. `tq run <ws> -- codex` runs `codex` like any other command, with no flag added.

What stays, on purpose:

- `internal/markers` and the process-tree detector still recognise Codex. A human-only command under
  Codex is refused.
- `internal/providers/catalog/codex.yaml` stays, so `codex` is an out-of-scope CLI like any other.
- `~/.tentaqles/identities/<ws>/codex` stays protected as part of the state folder. tq deletes nothing
  in it.

## Consequences

- An old Codex `hooks.json` still calls `tq claude-hook pre-tool-use --agent codex`. That flag no longer
  exists, so the call is denied as `tq/guard-error`: a leftover Codex session fails closed.
- A record signed by an older tq may cover the Codex files. Keys the record holds that tq no longer
  covers read as absent, so one that held a hash shows as removed, and the workspace is untrusted until a
  human runs `tq hooks verify --accept`. There is no exception for them. Every workspace needs that
  once after upgrading anyway, because the plugin changes too.
- With the owner's approval, the must-not-trade-off tests that only guarded Codex are gone:
  `TestProtectedPaths_Codex`, `TestCodexHookResolves`, `TestLaunchArgsInjectsTheBypassFlag`,
  `TestFilterCodex`, `TestFilterCodex_CredentialFields`. `TestPreToolUseIncompleteScanDeniesBothAgents` is
  now `TestPreToolUseIncompleteScanDenies`.
