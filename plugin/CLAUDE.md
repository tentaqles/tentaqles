# CLAUDE.md: tentaqles plugin

The Claude Code plugin in `plugin/` of this repo. It ships from the root marketplace
(`.claude-plugin/marketplace.json`, pinned to a release tag and its sha; see `docs/RELEASING.md`).

It holds the onboarding skill (`skills/onboard`) and the guard hook (`hooks/hooks.json`,
`scripts/tq_hook.sh`), and nothing else. There is no Python in it; `tests/` is a pytest suite for the hook
script.

- The hook is a bridge to `tq claude-hook`. All rules live in the Go CLI.
- `pre-tool-use` answers 0 or 2 and nothing else: Claude Code runs a call whose hook answers anything
  else, or that it killed. If tq is missing, fails or hangs, the call is denied.
- No environment variable and nothing in the plugin folder picks the tq that runs.
- Every hook declares a short timeout, and the script's own deadline stays under it. Hooks must never stall.
- A skill never runs a human-only command. It prints the line for the human, who runs it with `!`.

Tests: `PYTHONPATH=plugin python -m pytest plugin/tests -q`. They need Git Bash on Windows.
