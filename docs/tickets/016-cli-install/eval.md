# 016: `tentaqles reach` installs the client's CLIs: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use a fake package manager on PATH that records its argv. No real package is installed. Run targeted tests while building; the gates run the full suite.

## Not covered

A real `winget install` changes the machine, so the owner checks it in the end-to-end run.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 An in-scope CLI installs
- **Covers**: REQ-1, REQ-2, REQ-12
- **Method**: test TestInstall_InScope
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_InScope$' -count=1`
- **Expect**: The fake records the method's args with the catalog id, it runs under the identity's environment, and the line reads `az: installed <version>`.

### U-02 Already present, manual and no CLI
- **Covers**: REQ-2, REQ-3, REQ-6
- **Method**: test TestInstall_PresentManualNoCLI
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_PresentManualNoCLI$' -count=1`
- **Expect**: `already present` runs no package manager, a provider with no method prints the docs page, and `slack` prints `no CLI, nothing to install`.

### U-03 Reach installs each CLI
- **Covers**: REQ-14
- **Method**: test TestReach_InstallsAndContinues
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_InstallsAndContinues$' -count=1`
- **Expect**: One line per provider with a CLI after the trust is signed, a failure doesn't stop the rest, the client stays trusted, and `--no-install` runs nothing.

### F-01 The failure line is the error
- **Covers**: REQ-7
- **Method**: test TestInstall_FailureLineIsTheError
- **Steps**:
  1. `go test ./internal/install -run '^TestInstall_FailureLineIsTheError$' -count=1`
- **Expect**: For a fake printing progress and spinner frames on stdout then an error, and one printing `npm warn` then `npm error` on stderr, the line holds the error. Exit is 1.

### F-02 A hang stops the whole tree
- **Covers**: REQ-8
- **Method**: test TestInstall_TimeoutStopsTheProcessTree
- **Steps**:
  1. `go test ./internal/install -run '^TestInstall_TimeoutStopsTheProcessTree$' -count=1`
- **Expect**: At a lowered limit, a fake that starts a child has both processes gone, and the failure line says it was stopped.

### F-03 Neutral and untrusted folders
- **Covers**: REQ-5
- **Method**: test TestInstall_NoIdentity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_NoIdentity$' -count=1`
- **Expect**: Each exits 1 naming the reason, and an unknown provider id names the catalog.

### F-04 A new PATH entry on Windows
- **Covers**: REQ-11
- **Method**: test TestInstall_RefreshesPathFromRegistry
- **Steps**:
  1. `go test ./internal/install -run '^TestInstall_RefreshesPathFromRegistry$' -count=1`
- **Expect**: Through a registry seam holding a new folder, the version command runs from it. It skips with a reason on other systems.

### Q-01 Docs
- **Covers**: REQ-15, REQ-16
- **Method**: inspect
- **Steps**:
  1. Read `docs/PROVIDERS.md`, `docs/COMMANDS.md` and `docs/threat-model.md`.
- **Expect**: The command, the method order, the licence note, the rule on desktop installers and the threat-model row are there.

### R-01 The catalog tests hold
- **Covers**: REQ-1
- **Method**: test TestCatalogMatchLists
- **Steps**:
  1. `go test ./internal/providers ./internal/policy -count=1`
- **Expect**: `ok` for both.

### R-02 Reach still works without installing
- **Covers**: REQ-14
- **Method**: test TestReach_PathMakesATrustedClient
- **Steps**:
  1. `go test ./internal/cmd -run 'TestReach' -count=1`
- **Expect**: `ok`.

### S-01 Out of scope is refused and logged
- **Covers**: REQ-4
- **Method**: test TestInstall_OutOfScopeRefused
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_OutOfScopeRefused$' -count=1`
  2. On an Azure-only identity, run `tentaqles install aws`.
- **Expect**: Exit 1, the line names the scope, the fake's log is empty, and the denial log has one line with category `identity`.

### S-02 Input cannot reach a shell or a package manager
- **Covers**: REQ-9, REQ-10
- **Method**: test TestInstall_NoShellNoExtraArgs
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_NoShellNoExtraArgs$' -count=1`
  2. Run `tentaqles install "aws & calc"`, `tentaqles install az --source evil` and `tentaqles install az -- --override x`.
- **Expect**: Each refuses before running anything, and in other tests the fake's argv is exactly the method's args and the catalog id.

### S-03 A user provider file installs nothing
- **Covers**: REQ-10
- **Method**: test TestInstall_UserProviderHasNoInstall
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_UserProviderHasNoInstall$' -count=1`
  2. Add a user file with a new provider's install, and one overriding a built-in's.
- **Expect**: Neither is run, and the built-in's method is the one used.

### S-04 The guard allows the in-scope call and no more
- **Covers**: REQ-13
- **Method**: test TestPreToolUse_InstallAllowed
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_InstallAllowed$' -count=1`
- **Expect**: The hook allows `tentaqles install az` and `tq install az`, and `tq allow acme` still denies with `BLOCKED [self-protection/tq/human-only-command]`.
