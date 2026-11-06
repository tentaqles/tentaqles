# 015: Remove the replaced v1 commands

**Status:** ready
**Risk:** High: this edits human-only lists and shell hooks, so a slip could leave a command unguarded or break every terminal, and users lose commands they may have scripted
**UI:** none

PRD v2 Phase 6 and the owner's command-by-command review of 2026-11-06. With `reach`, `edit`, `off` and `on` in place, the commands they replace go. Every other command stays. The gate: no v1 path that was replaced stays reachable.

## Requirements

- **REQ-1**: `tentaqles add`, `onboard`, `setup`, `init` and `worktrees` must no longer exist: running one must print cobra's unknown command error and exit 1.
- **REQ-2**: `tentaqles run --off` must refuse with `unknown flag: --off`, and the `claude-off` function must be gone from every shell hook.
- **REQ-3**: `TQ_ENABLED=0` must no longer change anything: shell hooks stop reading it, and `docs/MIGRATE.md`'s kill-switch section must point to `tq hooks remove`.
- **REQ-4**: `internal/setup/`, `pkg/setupapi/`, `internal/worktrees/` and any part of `internal/onboard/` not used by `reach` must be deleted, along with their tests.
- **REQ-5**: These commands must still work with no change in behaviour: `version`, `env`, `activate`, `claude-hook`, `run`, `login`, `allow`, `deny`, `list`, `doctor`, `status`, `bundle`, `providers`, `hooks`, `secret`, `secrets`, `settings`, `statusline`, `dotenv`, `migrate`, `uninstall`, `reach`, `edit`, `off`, `on`, `install`, `connect`, `banner`.
- **REQ-6**: `tq migrate` and `tq uninstall --restore` must keep working on an install made by the removed commands, including a registered base and a manifest made by `tq add`.
- **REQ-7**: `internal/policy/selfprotect.go`'s human-only list must drop the removed verbs and keep every other. A shell call to a removed verb must pass the hook and fail on its own.
- **REQ-8**: Every doctor fix line, deny reason, help text and doc naming a removed command must name its replacement: `tentaqles reach` for `add`, `onboard`, `setup` and `init`, and `tentaqles off` for `run --off` and `claude-off`.
- **REQ-9**: `CLAUDE.md`'s Must not trade off must replace `TestAddDoesNotTrustFromAgent` with `TestReachDoesNotTrustFromAgent`, `TestAddConfirmsBeforeWriting` with `TestReachConfirmsBeforeWriting`, and remove `TestSetup_YesDoesNotSkipConfirm` from where it's listed, and must update its command list and layout.
- **REQ-10**: `docs/prd.md`, `docs/COMMANDS.md`, `docs/SETUP.md`, `README.md` and `docs/threat-model.md` must describe the kept command set, and `docs/COMMANDS.md`'s section 1 must equal the registered commands.
- **REQ-11**: `tq doctor` must not warn about a removed command's leftover data, and must not touch it.
- **REQ-12**: `go build ./...`, `go vet ./...` and `go test ./... -count=1` must pass, on the three OSes in CI.

## Out of scope

- `tq bundle`, `providers`, `migrate`, `uninstall`, `statusline`: the owner kept them.
- Removing the `claude` shell function: it still serves neutral folders.
- A deprecation period or aliases: the owner chose delete.
- Deleting a user's registered bases or existing identities.
- Plugin content: ticket 014.

## Affected areas

- `internal/cmd/add.go`, `onboard.go`, `onboard_wizard.go`, `setup.go`, `setup_wizard.go`, `init.go`, `worktrees.go` and their tests: delete
- `internal/cmd/root.go`, `internal/cmd/run.go`: registrations, `--off`
- `internal/setup/`, `pkg/setupapi/`, `internal/worktrees/`, `internal/onboard/`: delete or trim
- `internal/envplan/off.go`: `OffEnviron` goes if nothing else uses it
- `internal/shell/hooks/*.tmpl`, `internal/shell/testdata/*`: `claude-off`, `TQ_ENABLED`
- `internal/policy/selfprotect.go`, `internal/doctor/`, `internal/migrate/`
- `README.md`, `docs/COMMANDS.md`, `docs/SETUP.md`, `docs/MIGRATE.md`, `docs/prd.md`, `docs/threat-model.md`, `CHANGELOG.md`, `CLAUDE.md`
- `plugin/tests/test_tq_off.py`: only if `test_scripts_have_no_off_switch` names something removed

## Do not touch

- `internal/workspace/`, `internal/registry/`: `reach` uses them.
- `internal/humanonly/`, `internal/markers/`: the detectors don't change.
- `DISCOVERY.md`: a record of its day.

## Decisions

- **Agreed with the owner**: delete `add`, `onboard`, `setup`, `run --off` and the off functions, `init`, `worktrees`.
- **Agreed with the owner**: keep everything else listed in REQ-5.
- **Decided by the ticket writer**: `TQ_ENABLED=0` goes, per the owner's 2026-11-05 decision to retire it. `tq hooks remove` is the way to stop routing.
- **Decided by the ticket writer**: a removed verb on the hook's human-only list goes too, since an unknown command has nothing to protect; REQ-7's test keeps the rest.
- **Decided by the ticket writer**: no ADR of its own: the list only shrinks, and ADR-0003 and ADR-0005 get a line naming the removal.
- **Decided by the ticket writer**: `TestAddDoesNotTrustFromAgent` and `TestAddConfirmsBeforeWriting` are replaced by the same behaviour on `reach`, from ticket 010. A replaced test isn't a weakened one.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 015` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
- The whole-PRD acceptance list of PRD §11 is true, proven by the owner's U-05.
