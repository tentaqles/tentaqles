# Tickets

The build loop's board, kept by `.claude/hooks/gate.mjs`. See `.claude/README.md`.

| # | Title | Status | Risk | Runs |
|---|---|---|---|---|
| 001 | The dotenv masking test runs on a real bash | landed | Medium | alongside |
| 002 | The CLI registers the guard hook in the identity's settings | parked | High | alongside |
| 003 | `claude` is a shim on the identity PATH | parked | High | waits for 002: both change the render and trust coverage |
| 004 | `tentaqles` is the CLI, `tq` is its alias | parked | High | waits for 003: rewrites message text across internal/cmd and the hook script |
| 005 | The identity terminal opens with the banner, and cmd.exe routes itself | parked | High | waits for 004: shell templates, and the banner reads tentaqles |
| 006 | `tentaqles off` and `tentaqles on` | parked | High | waits for 005: raw sessions need the shim and the banner |
| 007 | One browser profile per identity | parked | High | waits for 003: needs the shims folder |
| 008 | Catalog install methods and `tentaqles install <provider>` | parked | High | alongside |
| 009 | `status --json` for the skill, and `tentaqles connect <provider>` | parked | High | waits for 002, 008: needs the render's hook and both change the catalog schema |
| 010 | `tentaqles reach` | parked | High | waits for 006, 007, 009: reach provisions everything they build |
| 011 | `tentaqles edit` | parked | High | waits for 010: reuses reach's prompts |
| 012 | The onboarding skill | ready | High | waits for 010: reach starts it, and it uses status, install and connect |
| 013 | Retire Codex support | ready | High | waits for 011: touches run, render, trust and shell hooks |
| 014 | Cut the plugin down to onboarding, memory and the graph | ready | High | waits for 012: keeps the finished skill; needs 002 landed |
| 015 | Remove the replaced v1 commands | ready | High | waits for 013: same files; reach and edit must have replaced them |
