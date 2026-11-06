# 013: Retire Codex support: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`. Run from the ticket's worktree.

## Not covered

A live Codex session is out of scope, as there's nothing left to run against. Nothing else is left out.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 No Codex launcher
- **Covers**: REQ-1
- **Method**: test TestHook_Golden
- **Steps**:
  1. `go test ./internal/shell ./internal/cmd -run 'TestHook_Golden|TestRun_CodexIsAnOrdinaryCommand' -count=1`
- **Expect**: No hook defines `codex` or `codex-off`, and a fake codex records exactly the typed arguments.

### U-02 Render leaves old files alone
- **Covers**: REQ-2
- **Method**: test TestSettingsRender_LeavesOldCodexFiles
- **Steps**:
  1. `go test ./internal/cmd -run '^TestSettingsRender_LeavesOldCodexFiles$' -count=1`
- **Expect**: Existing `hooks.json` and `config.toml` keep their bytes, one line names them, and a new identity gets neither.

### U-03 Docs say Claude Code only
- **Covers**: REQ-12
- **Method**: command
- **Steps**:
  1. `git grep -il codex -- README.md docs/COMMANDS.md docs/CLAUDE-SETTINGS.md docs/CLAUDE-HOOK.md`
- **Expect**: Each file listed mentions Codex only to say it isn't supported, or lists nothing.

### F-01 The Codex hook call denies
- **Covers**: REQ-3
- **Method**: test TestRunMainCodexAgentDenies
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunMainCodexAgentDenies$' -count=1`
- **Expect**: Exit 2 and `BLOCKED [tq/guard-error]`, naming Codex as unsupported, with one denial line logged.

### F-02 An old signed record
- **Covers**: REQ-6
- **Method**: test TestOldRecordWithCodexKeysVerifies
- **Steps**:
  1. `go test ./internal/trust -run '^TestOldRecordWithCodexKeysVerifies$' -count=1`
- **Expect**: A record holding extra `codex/` keys verifies, while one with an extra key anywhere else, or with a changed manifest, doesn't.

### Q-01 The code is gone
- **Covers**: REQ-1, REQ-2, REQ-4, REQ-5, REQ-10
- **Method**: command
- **Steps**:
  1. `git grep -il codex -- 'internal/*.go' ':!*_test.go'`
- **Expect**: Only `internal/markers/`, `internal/humanonly/`, `internal/trust/`'s old-record rule and `internal/protect/` if it names the state folder appear.

### Q-02 The gates pass
- **Covers**: REQ-1
- **Method**: command
- **Steps**:
  1. `go build ./...`
  2. `go vet ./...`
  3. `go test ./... -count=1`
- **Expect**: Each exits 0.

### R-01 Claude Code's hook is unchanged
- **Covers**: REQ-4
- **Method**: test TestPreToolUse_SecretRules
- **Steps**:
  1. `go test ./internal/cmd -run 'TestPreToolUse_SecretRules|TestPreToolUse_ScopeReferenceScenario|TestNoAskCodePath|TestPreToolUseGuardPanicDenies|TestPreToolUseIncompleteScanDenies' -count=1`
- **Expect**: All pass, with no change to the first four beyond removed Codex cases.

### R-02 Codex stays out of scope
- **Covers**: REQ-8
- **Method**: test TestScope_EveryProvider
- **Steps**:
  1. `go test ./internal/policy -run 'TestScope_EveryProvider' -count=1`
- **Expect**: Passes, and `codex` is denied on an identity whose scope leaves it out.

### R-03 The MCP filter keeps Claude's list
- **Covers**: REQ-10
- **Method**: test TestFilterClaude
- **Steps**:
  1. `go test ./internal/mcpscope -run 'TestFilterClaude|TestSettingsRender_ScopeReferenceScenario_MCP' -count=1`
  2. `go test ./internal/cmd -run 'TestSettingsRender_ScopeReferenceScenario_MCP' -count=1`
- **Expect**: All pass unchanged.

### S-01 Codex still counts as an agent
- **Covers**: REQ-9
- **Method**: test TestProcessTreeDetector
- **Steps**:
  1. `go test ./internal/humanonly -run 'TestProcessTreeDetector|TestEnvMarkerDetector' -count=1`
- **Expect**: Both pass unchanged, including `codex` in the tree and `CODEX_SANDBOX` set.

### S-02 Protected paths still hold
- **Covers**: REQ-7
- **Method**: test TestProtectedPaths_Table
- **Steps**:
  1. `go test ./internal/cmd -run 'TestProtectedPaths_Table|TestProtectedPaths_RuleOrder|TestProtectedPaths_OldCodexFolder' -count=1`
- **Expect**: All pass, and a Write to `~/.tentaqles/identities/acme/codex/hooks.json` is denied.

### S-03 Named tests only change as listed
- **Covers**: REQ-11
- **Method**: inspect
- **Steps**:
  1. `git diff main -- CLAUDE.md`
  2. Compare every test named in the removed lines with REQ-11's list.
- **Expect**: Exactly the tests of REQ-11 left or were renamed; every other named test is still present and exists in the tree.

### S-04 Signing has no new weak path
- **Covers**: REQ-6
- **Method**: test TestFlippedByteFailsVerification
- **Steps**:
  1. `go test ./internal/trust ./internal/cmd -run 'TestFlippedByteFailsVerification|TestLegacyDowngradeIsUntrusted|TestPluginHookChangesAreNamed' -count=1`
- **Expect**: All pass unchanged.
