# 015: Remove the replaced v1 commands: checks

Requirements: [rd.md](rd.md)

## Setup

`go build -o .job/015/bin/ ./cmd/tentaqles`. Go tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`.

## Not covered

CI on macOS and Linux runs at the PR, not in this round; REQ-12 is proven there by the lander's release gate. A user's own scripts calling a removed command can't be checked.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 The removed commands are gone
- **Covers**: REQ-1
- **Method**: test TestRemovedCommandsAreGone
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRemovedCommandsAreGone$' -count=1`
- **Expect**: `add`, `onboard`, `setup`, `init` and `worktrees` each give `unknown command` and exit 1.

### U-02 run --off is gone
- **Covers**: REQ-2
- **Method**: test TestRunOffFlagGone
- **Steps**:
  1. `go test ./internal/cmd ./internal/shell -run 'TestRunOffFlagGone|TestHook_Golden' -count=1`
- **Expect**: The flag gives `unknown flag: --off`, and no golden holds `claude-off`.

### U-03 Kept commands are registered
- **Covers**: REQ-5
- **Method**: test TestKeptCommandSet
- **Steps**:
  1. `go test ./internal/cmd -run '^TestKeptCommandSet$' -count=1`
- **Expect**: The registered names equal the list of REQ-5 exactly.

### U-04 Docs name the replacements
- **Covers**: REQ-8, REQ-10
- **Method**: test TestCommandsDocMatchesTree
- **Steps**:
  1. `go test ./internal/cmd -run '^TestCommandsDocMatchesTree$' -count=1`
  2. `git grep -nE "\b(tq|tentaqles) (add|onboard|setup|init|worktrees)\b|claude-off|run --off|TQ_ENABLED" -- README.md docs/COMMANDS.md docs/SETUP.md docs/MIGRATE.md internal ':!*_test.go'`
- **Expect**: The test passes, and grep's only hits describe the removal or, in `internal/migrate/`, reading an old install.

### U-05 The owner walks the acceptance list
- **Covers**: REQ-5
- **Method**: human
- **Steps**:
  1. `tentaqles reach` an Azure-only client, log in, and let the skill finish.
  2. Ask Claude Code to create a VM on AWS.
  3. Run `tentaqles off`, open a new terminal, run `tentaqles on`, in the folder.
- **Expect**: Each bullet of PRD §11's acceptance list holds: stopped by PATH, the MCP list and the hook; reports the mistake; off and on behave as stated; `tq` and `tentaqles` both work.

### F-01 TQ_ENABLED does nothing
- **Covers**: REQ-3
- **Method**: test TestTQEnabledIsIgnored
- **Steps**:
  1. `go test ./internal/shell ./internal/cmd -run '^TestTQEnabledIsIgnored$' -count=1`
- **Expect**: No hook template reads it, and `tq env` output is identical with it set to 0.

### F-02 An old install still upgrades
- **Covers**: REQ-6
- **Method**: test TestMigrate_IdentityAndGit_ApplyThenRestore
- **Steps**:
  1. `go test ./internal/migrate ./internal/cmd -run 'TestMigrate_IdentityAndGit_ApplyThenRestore|TestRestoreResumesAfterMiddleFailure|TestMigrate_OldAddManifest' -count=1`
- **Expect**: All pass, with a fixture written the way the removed `tq add` wrote it.

### F-03 Old data is ignored quietly
- **Covers**: REQ-11
- **Method**: test TestDoctor_NoWarningForRemovedCommandData
- **Steps**:
  1. `go test ./internal/doctor -run '^TestDoctor_NoWarningForRemovedCommandData$' -count=1`
- **Expect**: No new warning, and the files are unchanged.

### Q-01 The packages are gone
- **Covers**: REQ-4
- **Method**: command
- **Steps**:
  1. `git ls-files internal/setup pkg/setupapi internal/worktrees`
  2. `git grep -n "internal/setup\|pkg/setupapi\|internal/worktrees" -- '*.go'`
- **Expect**: Both print nothing.

### Q-02 The gates pass
- **Covers**: REQ-12
- **Method**: command
- **Steps**:
  1. `go build ./...`
  2. `go vet ./...`
  3. `go test ./... -count=1`
- **Expect**: Each exits 0.

### R-01 The kept commands behave as before
- **Covers**: REQ-5
- **Method**: test TestHumanOnlyCommandsRefuseWithMarkerAndLog
- **Steps**:
  1. `go test ./internal/cmd -run 'TestHumanOnlyCommandsRefuseWithMarkerAndLog|TestAllowAndDenyRefuseAgents|TestDotenvRunLoadsAndMasks|TestSecretIsHumanOnly|TestRunFromAgentSession' -count=1`
- **Expect**: All pass, with only removed verbs dropped from their tables.

### R-02 Terminals still route
- **Covers**: REQ-3, REQ-5
- **Method**: test TestHook_Golden
- **Steps**:
  1. `go test ./internal/shell ./internal/envplan -count=1`
- **Expect**: `ok`, with the `claude` function, the prompt hook and the identity PATH intact.

### S-01 The human-only list keeps the rest
- **Covers**: REQ-7
- **Method**: test TestSelfProtection_HumanOnlyListAfterRemoval
- **Steps**:
  1. `go test ./internal/policy -run '^TestSelfProtection_HumanOnlyListAfterRemoval$' -count=1`
- **Expect**: `reach`, `edit`, `off`, `allow`, `deny`, `uninstall`, `login`, `hooks install`, `hooks remove`, `hooks verify --accept`, `settings render`, `secret set`, `secret rm` and `providers add` all deny; `on`, `install`, `connect` and `status` allow.

### S-02 Named tests change only as listed
- **Covers**: REQ-9
- **Method**: inspect
- **Steps**:
  1. `git diff main -- CLAUDE.md`
- **Expect**: Only the replacements of REQ-9 appear in the test names, and the two `Reach` tests exist.

### S-03 No route around trust remains
- **Covers**: REQ-1, REQ-7
- **Method**: test TestNoOtherSigningPath
- **Steps**:
  1. `go test ./internal/cmd -run '^TestNoOtherSigningPath$' -count=1`
- **Expect**: Walking the registered commands, every one that signs (`reach`, `edit`, `allow`, `settings render`, `hooks verify --accept`, `migrate`) refuses under a marker.
