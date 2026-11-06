# Tentaqles plugin

The Claude Code plugin of [Tentaqles](https://github.com/tentaqles/tentaqles). It holds two things:

- **The onboarding skill**, `/tentaqles:onboard`, which connects a client's tools after `tentaqles reach`.
- **The guard hook**, `scripts/tq_hook.sh`, which runs `tq claude-hook` before each tool call and at
  session start. It denies a call when tq is missing, fails or hangs, because Claude Code runs a call whose
  hook did not answer.

Tentaqles itself is the `tentaqles` CLI. Install it first.
