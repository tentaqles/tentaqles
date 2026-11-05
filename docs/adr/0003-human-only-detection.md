# ADR-0003: Human-only commands refuse on three detectors and confirm before every write

Status: accepted
Date: 2026-11-05
Cites: PRD v2 §8.1 (human-only commands), §8.4 (what a human must do to change the guard), §14 Phase 1 gate; threat model boundary B2 (human-only actions); DISCOVERY.md §4 (harness marker variables), §7.1 (`tq onboard --from` and `tq setup --from` are human-only)

## Context
Self-protection (PRD v2 §8) rests on one rule: an agent must not be able to change
hooks, the guard, the manifest, the trust store, the shims or any identity unless a
human allows it. Until now the human-only check was one detector, the environment
marker list in `internal/cmd/humanonly.go`, applied to `tq allow`, `tq deny`,
`tq secret` and `tq run --off`; `tq add`, `tq setup` and `tq onboard --from`
scaffolded from a session and only left the folder untrusted, and `tq init`,
`tq hooks install`, `tq settings render`, `tq bundle sync`, `tq migrate`,
`tq uninstall`, `tq providers add` and `tq login` had no check at all. A marker is
easy to lose: a harness that forgets to export it, a subagent started through a
shell that scrubs the environment, a headless `claude -p` child. PRD v2 §8.1 names
three detectors and a confirmation, and the owner's decisions of 2026-11-05 fix the
command list and the marker list (`CLAUDE_CONFIG_DIR` and `CODEX_HOME` excluded:
tq sets them in human terminals).

## Decision
We move the check into `internal/humanonly` and apply it to every command that
writes under `$TQ_HOME` or an identity config dir: `init`, `add`, `allow`, `deny`,
`hooks install`, `hooks remove`, `settings render`, `bundle sync`, `bundle capture`,
`migrate`, `uninstall`, `setup` (wizard and `--from`), `onboard` (wizard and
`--from`), `secret set`, `secret rm`, `providers add`, `login`, `run --off` and
`worktrees prune`. Each refuses, with no prompt, when any of three detectors fires:

1. stdin or stdout is not an interactive terminal (`humanonly.IsTerminal`);
2. the process tree holds a harness or subagent: the parent chain is walked to the
   root and each executable's base name is matched against `claude`, `codex`,
   `opencode`, `kimi` (with or without `.exe`), and `node` when its command line
   mentions `claude` or `codex`. Windows reads a toolhelp snapshot and the command
   line from the PEB; Linux reads `/proc/<pid>/stat`, `comm` and `cmdline`; macOS
   uses `sysctl kern.proc` and `kern.procargs2` with `ps` as the last resort; any
   other OS has the walker stubbed and `tq doctor` says so;
3. the environment carries a harness marker: `TENTAQLES_HEADLESS`,
   `TENTAQLES_HEADLESS_CHILD`, `CLAUDE_PLUGIN_ROOT`, `CLAUDECODE`,
   `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_SSE_PORT`, `TQ_HUB_TASK`, `CODEX_SANDBOX`,
   `CODEX_SANDBOX_NETWORK_DISABLED`, `CODEX_THREAD_ID`, `OPENCODE`, `KIMI_CODE`.

A refusal appends one JSON line to `$TQ_HOME/denials.jsonl` (time, kind
`human-only`, command, reason naming the detector, pid, cwd; never an environment
value) and returns an error that names the command as human-only and the detector.

When all three pass, the command prints one line saying what it is about to write
and asks `Type yes to continue:`; the answer is read from the controlling terminal
(`CONIN$`, `/dev/tty`), never from a piped stdin. Anything but `yes` exits 1 with
nothing written. The confirmation is mandatory: `--yes` on `tq setup` no longer
skips it (the flag is accepted for older scripts), and `--yes` on `tq uninstall`
and `tq worktrees prune` still selects the replay or the removal over the listing
but no longer skips it. A dry run or a listing asks nothing, since it writes nothing.

Free to run from a session: `env`, `activate`, `doctor`, `list`, `claude-hook`,
`run` (normal), `dotenv run`, `secrets audit`, `bundle diff`, `bundle catalog`,
`hooks status`, `settings` read-only paths, `version`, `secret status` (presence
only, never a value), `worktrees list`, `providers list|show|check`.

Every detector and the confirmation are seams (`humanonly.IsTerminal`,
`humanonly.ParentOf`, `humanonly.Confirm`) so tests feed a fake terminal, a fake
process tree and a scripted answer; `go test` itself may run inside a harness.

## Consequences
- The Phase 1 gate's first half holds: an agent cannot trust a folder, scaffold one,
  rewrite a profile, a `settings.json`, the catalog, a provider file or the keychain
  through tq. `TestHumanOnlyCommandsRefuseWithMarkerAndLog` runs every listed
  invocation under a marker and checks that the only file the home gains is the
  denial log; `TestAddDoesNotTrustFromAgent` now proves a session gets no folder at
  all; `TestAllowAndDenyRefuseAgents`, `TestSecretIsHumanOnly`,
  `TestRunOffRefusesAgents` and `TestOnboard_FromRefusedInAgentSession` keep their
  commands refused.
- A human types `yes` once per write. Scripts that drove `tq setup --from --yes`,
  `tq uninstall --restore --yes` or `tq add` non-interactively stop working by
  design: they are not a human at a terminal.
- `tq secret set` reads the token from the hidden prompt only; a piped stdin is
  refused by the first detector.
- `tq secret status` becomes free: it prints presence, never a value, and a session
  may need it to tell the developer what to store.
- A terminal emulator that is not a console to Go (mintty's pipes, an IDE's task
  runner) fails the first detector; the developer runs the command from a console
  terminal. `tq doctor` reports the detectors (`human-only`), with a warning on an
  OS whose walker is stubbed.
- `internal/humanonly` joins the paths that cross a trust boundary; a change there
  needs an ADR, like `internal/guard` and `internal/trust`.

## Alternatives considered
| Option | Why not |
|---|---|
| Keep the environment marker as the only detector | One lost variable opens every write; PRD v2 §8.1 asks for all three |
| A hook rule in `internal/policy` that denies `tq allow` and friends in the shell | The hook sees only tool calls through the harness; a subagent or a reworded command bypasses it, and the command itself must refuse |
| Read the confirmation from stdin | A piped `yes` would pass; the first detector already requires a terminal, and the controlling terminal is the only reader a program cannot script |
| Skip the confirmation on `--yes` | §8.1 makes the confirmation unconditional; a flag an agent can type is not a human |
| Shell out to `ps` / `Get-CimInstance` for the tree on every OS | Seconds on Windows (see `listProcesses` in `migrate.go`); the toolhelp snapshot and `/proc` are milliseconds and need no subprocess |
