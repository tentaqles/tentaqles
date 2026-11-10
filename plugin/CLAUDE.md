# CLAUDE.md: tentaqles plugin

The Claude Code plugin in `plugin/` of this repo. It ships from the root marketplace (`.claude-plugin/marketplace.json`,
pinned to a release tag and its sha; see `docs/RELEASING.md`).

It holds the hook (`hooks/hooks.json`, `scripts/tq_hook.sh`) and nothing else; `tests/` is a pytest suite for the
script. It also holds the memory's hook entries (today three, on `PostToolUseFailure`, `PostToolUse` and `Stop`), which run
`scripts/lobe_hook.sh` and fail open: they exit 0 and print nothing on any problem, and never answer a deny.

- The hook is a bridge to `tentaqles claude-hook`. The rules live in the Go CLI (`internal/guard`).
- The PreToolUse matcher is `^(Bash|PowerShell)$`: only shell commands are checked.
- `pre-tool-use` answers 0 or 2 and nothing else: Claude Code runs a call whose hook answers anything else, or that
  it killed. If tq is missing, fails or hangs, the call is denied.
- No environment variable and nothing in the plugin folder picks the tq that runs.
- Every hook declares a short timeout, and the script's own deadline stays under it. Hooks must never stall.

Tests: `PYTHONPATH=plugin python -m pytest plugin/tests -q`. They need Git Bash on Windows.
