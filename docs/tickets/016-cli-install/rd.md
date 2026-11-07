# 016: `tentaqles reach` installs the client's CLIs

**Status:** landed
**Risk:** High: a scope bug lets an agent install another client's CLI on the machine, and a wrong catalog entry runs an unintended package
**UI:** none

IDEATION: "The system prepares the manifest and the other settings for the selections, installs the CLIs for the selected providers." `docs/IDEATION.md` is the spec. The onboarding skill uses the same command to retry, so an agent can call it, for providers in scope only. Stopped work on branch `worktree-agent-a65abe1e1f3cf5844` (commit 84790bd, package `internal/install`) can be copied where it fits.

## Requirements

- **REQ-1**: `tentaqles install <provider>` must, for a provider in the current identity's scope with a method for this OS, run the first available package manager in the order `winget`, `scoop`, `brew`, `apt`, `npm`, `pip`, then run the catalog's `version_args`, and print one line.
- **REQ-2**: The line must be `<provider>: installed <version>`, `<provider>: already present <version>`, or `<provider>: manual, download from <docs URL>`, the last when no method is usable.
- **REQ-3**: A provider already resolving on the identity PATH must print `already present` and run no package manager.
- **REQ-4**: A provider outside the scope must refuse with exit 1 and `<provider> is outside the scope of identity <ws>: <scope>`, run nothing, and log one line in the denial log with category `identity`.
- **REQ-5**: In a neutral or untrusted folder it must refuse with exit 1, naming the reason.
- **REQ-6**: A provider with `cli: null` must print `<provider>: no CLI, nothing to install` and exit 0.
- **REQ-7**: A failed package manager must print `<provider>: install failed: <reason>`, with exit 1. The reason must be the package manager's error line, not its first progress line, spinner frame or warning.
- **REQ-8**: Each run must have a 10-minute limit. At the limit the whole process tree must be stopped, and the result treated as REQ-7.
- **REQ-9**: tq must never run a value under `url`, and never launch an installer file itself.
- **REQ-10**: The package id must come from the embedded catalog and be passed as one argv element, with no shell. No argument from the command line may reach the package manager, and a user provider file must not change what is installed.
- **REQ-11**: After a Windows install, verification must find the CLI through the user and machine PATH read from the registry, since a package manager's PATH change does not reach a running process.
- **REQ-12**: The package manager and the version check must run with the identity's environment.
- **REQ-13**: The hook must allow `tentaqles install <in-scope provider>` for an agent. It is not human-only.
- **REQ-14**: `tentaqles reach`, after the trust is signed, must print the REQ-2 line for each provider in scope with a CLI, and continue past a failure. `--no-install` must skip this step.
- **REQ-15**: `docs/PROVIDERS.md` must list the methods and their order, and state that desktop installers are never launched by tq, and that tq accepts the vendor's licence through winget's flags.
- **REQ-16**: `docs/threat-model.md` must have a row for an agent installing an in-scope CLI without a person.

## Out of scope

- Per-identity installs: a CLI installs machine-wide, and the PATH layer decides who reaches it.
- Upgrading a CLI already present.
- MCP entries and logins: tickets 018 and 021.
- Azure DevOps' `az extension add`: it stays a printed note.

## Affected areas

- `internal/install/` (new): pick a method, run it, verify
- `internal/cmd/install.go` (new), `internal/cmd/root.go`, `internal/cmd/reach.go`: the command, and the step in reach
- `internal/providers/`, `internal/providers/catalog/*.yaml`: only as needed for install data
- `internal/audit/`: a denial layer for a command's own refusal
- `internal/testutil/`, `internal/cmd/main_test.go`: a fake package manager for tests
- `docs/PROVIDERS.md`, `docs/COMMANDS.md`, `docs/threat-model.md`, `docs/CLAUDE-HOOK.md`, `CHANGELOG.md`, `docs/adr/0015-provider-install.md`

## Do not touch

- `internal/policy/selfprotect.go`: `install` isn't on the human-only list, and nothing needs adding.

## Decisions

- **Agreed with the owner**: an agent may call it; machine-wide install is fine; winget's accept-agreements flags are fine; azure-devops reports `installed` on `az`, with the extension as a note.
- **Decided by the ticket writer**: tq never adds `sudo`. An apt run that lacks rights reports REQ-7's line.
- **Decided by the ticket writer**: `winget` may run a vendor installer behind the scenes. That counts as a package manager.
- **Decided by the ticket writer**: the manual line prints the catalog's `docs:` page, because a direct `.msi` link is not a download page.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 016` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
