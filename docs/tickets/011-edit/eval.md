# 011: `tentaqles edit`: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use the human-only seams, a scripted prompt reader and fakes for package managers.

## Not covered

Nothing beyond the fakes: a real install is covered by ticket 010's owner check.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 Change one prompt
- **Covers**: REQ-1, REQ-4, REQ-5, REQ-6, REQ-9
- **Method**: test TestEdit_OnePrompt
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_OnePrompt$' -count=1`
- **Expect**: Only the selected prompt runs, with the current value as default; the change list prints; after `yes` the manifest has the new value, and the workspace is trusted.

### U-02 No change
- **Covers**: REQ-7
- **Method**: test TestEdit_NoChange
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_NoChange$' -count=1`
- **Expect**: `No change.` prints, with no `yes` asked, and every file's bytes are unchanged.

### U-03 Add a provider
- **Covers**: REQ-10
- **Method**: test TestEdit_AddProvider
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_AddProvider$' -count=1`
- **Expect**: The provider enters the scope, the install line prints, `--no-install` skips it, and a failed install still ends trusted.

### U-04 Remove a provider
- **Covers**: REQ-11
- **Method**: test TestEdit_RemoveProvider
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_RemoveProvider$' -count=1`
- **Expect**: After removing `aws`, the identity PATH drops it, its MCP entry goes, the hook denies `aws s3 ls`, its credentials folder is intact, and edit printed that folder.

### U-05 Turn on guard v2
- **Covers**: REQ-12
- **Method**: test TestEdit_GuardV2
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_GuardV2$' -count=1`
- **Expect**: Selecting it sets `guard: v2`, the listed behaviours print before the `yes`, and clearing it warns, removes the mask and keeps the manifest otherwise.

### F-01 Not an identity
- **Covers**: REQ-2
- **Method**: test TestEdit_NotAnIdentity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_NotAnIdentity$' -count=1`
- **Expect**: Exit 1 with `is not an identity: use tentaqles reach`, for a neutral folder and an unknown name.

### F-02 Any answer but yes
- **Covers**: REQ-6
- **Method**: test TestEdit_DeclineChangesNothing
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_DeclineChangesNothing$' -count=1`
- **Expect**: `no`, an empty answer and a closed input leave every byte under `$TQ_HOME` unchanged.

### F-03 A step fails after the yes
- **Covers**: REQ-13
- **Method**: test TestEdit_FailedRender
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_FailedRender$' -count=1`
- **Expect**: The exact line prints, the old record stands, the identity reads untrusted, and the hook denies a mutating git command.

### F-04 A bad value
- **Covers**: REQ-4, REQ-14
- **Method**: test TestEdit_BadValues
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_BadValues$' -count=1`
- **Expect**: An invalid email and a token-shaped value re-ask, and there is no prompt for the name or `client`.

### Q-01 Docs
- **Covers**: REQ-1
- **Method**: inspect
- **Steps**:
  1. Read `docs/COMMANDS.md` and `docs/SETUP.md`.
- **Expect**: Edit is described, including how an existing identity turns on `guard: v2`.

### R-01 Hand-written fields survive
- **Covers**: REQ-8
- **Method**: test TestEdit_KeepsEveryOtherField
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_KeepsEveryOtherField$' -count=1`
- **Expect**: `blocked_commands`, `claude.env`, `claude.bundle`, `mcp`, `scope`, `other_providers` and an unknown block all parse equal before and after a change to the email.

### R-02 Edited manifests still lose trust outside edit
- **Covers**: REQ-9
- **Method**: test TestEditedManifestLosesTrust
- **Steps**:
  1. `go test ./internal/trust ./internal/cmd -run 'TestEditedManifestLosesTrust' -count=1`
- **Expect**: Passes with no change to that test.

### S-01 An agent cannot edit
- **Covers**: REQ-3
- **Method**: test TestEdit_RefusesAgents
- **Steps**:
  1. `go test ./internal/cmd ./internal/policy -run '^TestEdit_RefusesAgents$' -count=1`
  2. Under each detector alone, and through the hook as `tentaqles edit`, `tq edit acme` and a path to the binary.
- **Expect**: Each refuses before asking anything, the hook denies with `BLOCKED [self-protection/tq/human-only-command]`, and a refusal is logged.

### S-02 Widening the scope always shows and confirms
- **Covers**: REQ-6
- **Method**: test TestEdit_ScopeChangeIsNamed
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEdit_ScopeChangeIsNamed$' -count=1`
- **Expect**: The confirmation text names each added and removed provider before the `yes` is read.

### S-03 Edit signs only through the human check
- **Covers**: REQ-9
- **Method**: test TestSigningRefusesUnderAHarnessProcessTree
- **Steps**:
  1. `go test ./internal/trust ./internal/cmd -run 'TestSigningRefusesUnderAHarnessProcessTree|TestEdit_SignsOnlyAfterGate' -count=1`
- **Expect**: Both pass, and no signature is written before the `yes`.
