# 003: `claude` is a shim on the identity PATH

**Status:** parked
**Risk:** High: a broken shim starts Claude Code under the wrong login or with no identity, or recurses and blocks every session
**UI:** none

PRD v2 §7.2, §3.2 and the Phase 3 gate. Today `claude` in a terminal is a shell function, so a script, a child process or cmd.exe reaches the real binary with no identity. This ticket puts a real `claude` program first on a `guard: v2` identity's PATH. It favours getting arguments through byte for byte, and never starting a session it can't bind to the identity.

## Requirements

- **REQ-1**: `tq settings render <ws>` under `guard: v2` must write a `claude` shim into `$TQ_HOME/shims/`. On Windows it is `claude.exe`, a hard link to the tq binary or a copy across volumes. On POSIX it is an executable script with the absolute path of tq.
- **REQ-2**: The identity PATH of a `guard: v2` identity must hold `$TQ_HOME/shims` before every other entry, so `Get-Command claude` in PowerShell, `command -v claude` in bash and `where claude` in cmd.exe all resolve to the shim.
- **REQ-3**: Running the shim in an identity must start the real `claude` with `--dangerously-skip-permissions`, `CLAUDE_CONFIG_DIR` set to `~/.tentaqles/identities/<ws>/claude`, and the rest of the environment the identity gives `tq run . -- claude`.
- **REQ-4**: The shim must find the real `claude` on the identity PATH with the shims folder left out. If none exists it must print `claude: not found on the PATH of identity <ws>` and exit 127, and it must never call itself.
- **REQ-5**: Every argument typed after `claude` must reach the real binary byte for byte, including one holding `&`, `%`, `^`, `"`, `|`, spaces or non-ASCII characters.
- **REQ-6**: The flag rules of `launchClaudeArgs` must apply unchanged: a typed `--permission-mode` or `--dangerously-skip-permissions` replaces the shim's flag; `claude` subcommands, `--help` and `--version` get none; a nested agent gets none.
- **REQ-7**: The shim must refuse to start when the identity's integrity record does not match, printing what changed and the line `tq hooks verify --accept`, and exit non-zero.
- **REQ-8**: The shim must not change what a plain `claude` does in a neutral folder or in an identity without `guard: v2`: `$TQ_HOME/shims` is not on their PATH.
- **REQ-9**: `docs/CLAUDE-SETTINGS.md` must state the observed precedence when the shim's flag and a developer-typed flag are both given, from a run of the real `claude` recorded in `docs/adr/0011-claude-shim.md` with the version it ran on.
- **REQ-10**: Leaving the identity folder must take `$TQ_HOME/shims` off PATH, restoring it exactly as `TestLayer_EnterAndLeaveRestoresExactly` requires.

## Out of scope

- Removing the `claude` shell function: it stays for neutral folders, and it gives the same result where the shim also exists.
- Codex and its function: ticket 013.
- Raw sessions: ticket 006 decides how `claude` behaves there.
- Shims for other CLIs. The PRD allows them, but scope is already enforced by the PATH layer and the mask.

## Affected areas

- `cmd/tq/main.go`, `internal/envplan/`: the shim entry beside `StubMain`, the shims folder on the identity PATH, the real-binary lookup
- `internal/cmd/run.go`: reuse the launch for the shim
- `internal/cmd/scope_render.go`, `internal/cmd/settings.go`: write the shim
- `internal/trust/covered.go`: already covers `$TQ_HOME/shims/`, and may need only tests
- `internal/doctor/launcher.go`: stop warning about a shadow from tq's own shim
- `docs/CLAUDE-SETTINGS.md`, `docs/adr/0011-claude-shim.md`, `CHANGELOG.md`

## Do not touch

- `internal/shell/hooks/*.tmpl`: ticket 004 owns them next.
- `internal/policy/`: no rule changes.

## Decisions

- **Agreed with the owner**: one flag, `guard: v2`.
- **Decided by the ticket writer**: a program on Windows and not a `.cmd`, because cmd.exe re-parses a batch file's arguments and `claude -p "a & calc"` would run `calc`. This is the same reasoning as the mask stubs.
- **Decided by the ticket writer**: a hard-linked shim goes stale when tq is upgraded. `tq hooks verify --accept` and `tq settings render` rewrite it; until then the mismatch refuses the launch with the fix line.
- **Decided by the ticket writer**: one shims folder for the machine, not one per identity, since the shim resolves the identity from the folder it runs in. It is what `internal/trust/covered.go` already covers.
- **Decided by the ticket writer**: the shim resolves its identity from the working directory, exactly as `tq run .` does.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 003` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
