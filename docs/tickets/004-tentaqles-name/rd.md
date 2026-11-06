# 004: `tentaqles` is the CLI, `tq` is its alias

**Status:** parked
**Risk:** High: a human-only verb spelled `tentaqles` that the guard doesn't match lets an agent reach a human-only command, and a wrong installer breaks every install
**UI:** none

PRD v2 §0: the CLI is `tentaqles`, `tq` is an alias installed with it, and both must work in every supported shell and run the same binary. Docs, messages and examples use `tentaqles`. Existing users keep typing `tq` with no change, so favour compatibility.

## Requirements

- **REQ-1**: A release must install both `tentaqles` and `tq` on Windows, macOS and Linux, through `installers/install.ps1`, `installers/install.sh`, Scoop and Homebrew.
- **REQ-2**: `tentaqles <args>` and `tq <args>` must give the same stdout, stderr and exit code for every command.
- **REQ-3**: `tentaqles --help` must start with `tentaqles` in its usage line, and every subcommand's usage and examples must read `tentaqles`, whichever name was typed.
- **REQ-4**: Every human-only refusal, hook deny reason and doctor fix line must name `tentaqles` and not `tq`, for example `tentaqles allow: human-only`.
- **REQ-5**: The hook must treat `tentaqles`, `tentaqles.exe`, and a path to either exactly as it treats `tq`, for every human-only verb and every rule that names tq today.
- **REQ-6**: The running binary and its sibling under the other name must both be in the protected path set.
- **REQ-7**: In bash, zsh, fish, PowerShell, pwsh and cmd.exe the profile hook must make both names work, including the `--` handling of `tq run` and `tq dotenv run` on PowerShell, in a terminal with the hook loaded.
- **REQ-8**: `plugin/scripts/tq_hook.sh` and the script of ticket 002 must find either name, and prefer `tentaqles`.
- **REQ-9**: `TQ_HOME`, `TQ_WS`, every other `TQ_*` variable, the `$TQ_HOME` layout and the `claude-hook` protocol must not change.

## Out of scope

- Renaming the Go module, `cmd/tq/` or the `internal/` packages.
- Renaming the release archive (`project_name: tq`), so an already-published installer keeps working.
- Renaming `TQ_*` variables.
- Docs that record history: `CHANGELOG.md` entries already written, old ADRs and PRD v1 text.

## Affected areas

- `cmd/tentaqles/main.go` (new), `cmd/tq/main.go`: both mains
- `.goreleaser.yaml`, `installers/install.ps1`, `installers/install.sh`: both binaries, and the Scoop and cask lists
- `internal/cmd/`: command names, help and message text
- `internal/policy/selfprotect.go`, `internal/protect/pathset.go`, `internal/humanonly/`, `internal/guard/`: both spellings
- `internal/doctor/`: fix lines
- `internal/shell/hooks/*.tmpl`, `internal/shell/testdata/*`: both names
- `plugin/scripts/tq_hook.sh`, `internal/hookscript/`: find either name
- `README.md`, `docs/COMMANDS.md`, `docs/SETUP.md`, `docs/CLAUDE-HOOK.md`, `docs/CLAUDE-SETTINGS.md`, `docs/PROVIDERS.md`, `docs/MIGRATE.md`, `docs/RELEASING.md`, `CHANGELOG.md`
- `docs/adr/0012-tentaqles-name-and-tq-alias.md`

## Do not touch

- `plugin/skills/`: ticket 012 writes the new skill, and ticket 014 deletes the rest.
- `internal/providers/catalog/`: ticket 008.

## Decisions

- **Agreed with the owner**: `allow`, `deny`, `doctor` and every other kept command keep their names.
- **Decided by the ticket writer**: two binaries in the release, one under each name. A Windows symlink needs admin rights, and a hard link doesn't survive a zip.
- **Decided by the ticket writer**: the name typed does not change output. Help always says `tentaqles`, so scripts can compare output across both.
- **Decided by the ticket writer**: `go build ./cmd/tq` still works for development.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 004` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
