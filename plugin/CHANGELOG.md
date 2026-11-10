# Changelog

Notable changes to the Tentaqles plugin. Versions follow [semver](https://semver.org/).

## [Unreleased]

- A new `PostToolUse` entry runs `scripts/lobe_hook.sh post-tool-use` on every tool call (timeout 5 s, fails open),
  so the memory records each successful call as a small `ok` line with no output in it.
- A new hook entry on `Stop` runs `scripts/lobe_hook.sh stop`, which starts `tentaqles lobe-hook claude stop` to record
  a call Claude Code rejected before it ran (timeout 5 s). It fails open, like the other memory hook.
- A new hook entry on `PostToolUseFailure` runs `scripts/lobe_hook.sh post-tool-use-failure`, which starts
  `tentaqles lobe-hook claude post-tool-use-failure` to record the failed call in that client's memory. It fails
  open (exit 0, no output, whatever goes wrong) and does nothing when `TQ_LOBE_NO_HOOKS=1`. The guard's two
  entries are unchanged.
- The README describes the session-start preamble in full; a comment in `scripts/tq_hook.sh` no longer names a
  removed plan step. No behaviour change.

## [0.17.0] - 2026-11-08

- **The plugin is the identity check.** Its PreToolUse hook, on `^(Bash|PowerShell)$`, runs
  `tentaqles claude-hook pre-tool-use` through `scripts/tq_hook.sh` and blocks a shell command under the wrong
  identity: the wrong git email, gh user or cloud, a stale shell, a remote command outside every client folder.
  File edits and MCP tools never start a tentaqles process.
- Its SessionStart hook runs `tentaqles claude-hook session-start`: the client, its git identity and the rules.
- The hook fails closed: with no tentaqles on PATH, or a check that does not answer in time, the call is denied.
- Installs from the root marketplace of `github.com/tentaqles/tentaqles`, pinned to a release tag and its commit.
