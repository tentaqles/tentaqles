# 008: Catalog install methods and `tentaqles install <provider>`: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and replace the package manager with a fake program on PATH that records its argv. No real package is installed.

## Not covered

A real `winget install` needs network access and changes the machine, so the owner checks it through ticket 010's `reach` on a fresh folder. Network failure inside a package manager looks like REQ-8.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 An in-scope CLI installs
- **Covers**: REQ-2, REQ-3
- **Method**: test TestInstall_InScope
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_InScope$' -count=1`
- **Expect**: The fake records `install --id Microsoft.AzureCLI -e` or the catalog's actual id, the version runs, and the line reads `az: installed <version>`.

### U-02 Already present
- **Covers**: REQ-4
- **Method**: test TestInstall_AlreadyPresent
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_AlreadyPresent$' -count=1`
- **Expect**: `already present` prints, and the fake package manager's log is empty.

### U-03 No method on this OS
- **Covers**: REQ-3, REQ-7
- **Method**: test TestInstall_ManualAndNoCLI
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_ManualAndNoCLI$' -count=1`
- **Expect**: A provider with no method prints `manual, download from <docs URL>` and exits 0, and `slack` prints `no CLI, nothing to install`.

### F-01 Package manager fails, or hangs
- **Covers**: REQ-8, REQ-9
- **Method**: test TestInstall_FailureAndTimeout
- **Steps**:
  1. `go test ./internal/install -run '^TestInstall_FailureAndTimeout$' -count=1`
- **Expect**: A failing fake prints `install failed:` with its first error line, exit 1, and a hanging one is stopped at a lowered limit with the same line.

### F-02 Neutral folder and unknown provider
- **Covers**: REQ-6
- **Method**: test TestInstall_NoIdentity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_NoIdentity$' -count=1`
- **Expect**: A neutral folder gives `tentaqles install needs an identity`, an unknown id names the catalog, and both exit 1.

### F-03 Verification sees a new PATH entry on Windows
- **Covers**: REQ-10
- **Method**: test TestInstall_RefreshesPathFromRegistry
- **Steps**:
  1. `go test ./internal/install -run '^TestInstall_RefreshesPathFromRegistry$' -count=1`
- **Expect**: Through a registry seam holding a new folder, the version command runs from that folder. It skips with a reason on non-Windows.

### Q-01 The catalog is well formed
- **Covers**: REQ-1, REQ-12
- **Method**: test TestCatalogMatchLists
- **Steps**:
  1. `go test ./internal/providers -count=1`
- **Expect**: `ok`; the five new providers load with `category`, `docs` and `match`, and a method outside the six is refused.

### Q-02 Docs
- **Covers**: REQ-14
- **Method**: inspect
- **Steps**:
  1. Read `docs/PROVIDERS.md` and `docs/COMMANDS.md`.
- **Expect**: Both name the command and the method order, and `docs/PROVIDERS.md` says desktop installers are never run.

### R-01 Scope tests cover the new providers
- **Covers**: REQ-12
- **Method**: test TestScope_EveryProvider
- **Steps**:
  1. `go test ./internal/policy -run 'TestScope_EveryProvider|TestOutOfScopeEnv_SkipsGenericTools' -count=1`
- **Expect**: Both pass, and a `go test -v` line names `discord`, `zoom`, `google-meet`, `colab` and `trello`.

### R-02 The mask and PATH layer don't shift
- **Covers**: REQ-12
- **Method**: test TestPathLayer_ScenarioAwsNotOnPath
- **Steps**:
  1. `go test ./internal/envplan ./internal/cmd -count=1`
- **Expect**: `ok` for both, with no ambiguous word added to any executable mask. Added `executables` must not hide a common program.

### S-01 Out of scope is refused and logged
- **Covers**: REQ-5
- **Method**: test TestInstall_OutOfScopeRefused
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_OutOfScopeRefused$' -count=1`
  2. On an Azure-only identity, run `tentaqles install aws`.
- **Expect**: Exit 1, the line names the scope, the fake package manager's log is empty, and the denial log has one line with category `identity`.

### S-02 Input cannot reach a shell or a package manager
- **Covers**: REQ-11
- **Method**: test TestInstall_NoShellNoExtraArgs
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_NoShellNoExtraArgs$' -count=1`
  2. Run `tentaqles install "aws & calc"`, `tentaqles install az --source evil` and `tentaqles install az -- --override x`.
- **Expect**: Each refuses before running anything, and in the other tests the fake's argv is exactly the method's args and the catalog id.

### S-03 A user provider file can't install anything
- **Covers**: REQ-1, REQ-11
- **Method**: test TestInstall_UserProviderHasNoInstall
- **Steps**:
  1. `go test ./internal/cmd -run '^TestInstall_UserProviderHasNoInstall$' -count=1`
  2. Add a user provider file whose `install` names an arbitrary package, and an override of a built-in's install.
- **Expect**: Neither is run, and the built-in's method is the one used. User files can only widen denial, as `TestScope_UserOverrideCannotAllow` requires.

### S-04 The guard allows the in-scope call and no more
- **Covers**: REQ-13
- **Method**: test TestPreToolUse_InstallAllowed
- **Steps**:
  1. `go test ./internal/cmd -run '^TestPreToolUse_InstallAllowed$' -count=1`
- **Expect**: The hook allows `tentaqles install az`, and `tentaqles reach` under the same call still denies.
