# 002: The CLI registers the guard hook in the identity's settings: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`. Run Go tests through PowerShell on Windows. Script tests need Git Bash.

## Not covered

A live Claude Code session is not started: the hook payload is fed to the script on stdin, the same way the harness does, because a live session needs a login. The first-time-user pass before landing does not cover it either; the owner confirms with one session after landing.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 A render registers the hook
- **Covers**: REQ-1, REQ-2, REQ-3
- **Method**: test TestSettingsRender_RegistersGuardHook
- **Steps**:
  1. `go test ./internal/cmd -run '^TestSettingsRender_RegistersGuardHook$' -count=1`
- **Expect**: For a flagged and an unflagged workspace, settings.json holds the exact matcher of REQ-1 and timeout 15, and the script exists under `$TQ_HOME/hooks/`.

### U-02 A render is idempotent and keeps user entries
- **Covers**: REQ-10
- **Method**: test TestSettingsRender_KeepsUserHooks
- **Steps**:
  1. `go test ./internal/cmd -run '^TestSettingsRender_KeepsUserHooks$' -count=1`
- **Expect**: A user PreToolUse entry stays in place and order, and the second render prints `up to date`.

### U-03 The instruction file states the scope
- **Covers**: REQ-13
- **Method**: test TestSettingsRender_ScopeBlockInClaudeMd
- **Steps**:
  1. `go test ./internal/cmd -run '^TestSettingsRender_ScopeBlockInClaudeMd$' -count=1`
- **Expect**: Under `guard: v2` the block names the scope and the rest of an existing file is byte-identical; with the flag off only the block goes, and no file is created where there was none.

### F-01 tq exits without a decision
- **Covers**: REQ-4
- **Method**: test TestHookScript_NoDecisionDenies
- **Steps**:
  1. `go test ./internal/hookscript -run '^TestHookScript_NoDecisionDenies$' -count=1`
- **Expect**: A fake tq exiting 1, 3 and 139 each gives exit 2 and stderr containing `BLOCKED [tq/guard-error]`; exits 0 and 2 pass through.

### F-02 tq hangs
- **Covers**: REQ-5
- **Method**: test TestHookScript_HangingTqDenies
- **Steps**:
  1. `go test ./internal/hookscript -run '^TestHookScript_HangingTqDenies$' -count=1`
- **Expect**: A fake tq that sleeps forever gives exit 2 in under 10 s.

### F-03 tq is missing
- **Covers**: REQ-6
- **Method**: test TestHookScript_MissingTqDenies
- **Steps**:
  1. `go test ./internal/hookscript -run '^TestHookScript_MissingTqDenies$' -count=1`
- **Expect**: With no tq on PATH or in the install folders, Bash, Read and an MCP tool payload each give exit 2.

### F-04 No hook anywhere
- **Covers**: REQ-11, REQ-12
- **Method**: test TestRun_RefusesWithNoGuardHook
- **Steps**:
  1. `go test ./internal/cmd ./internal/doctor -run 'TestRun_RefusesWithNoGuardHook|TestDoctor_GuardHookMissing' -count=1`
- **Expect**: Doctor reports `guard-hook-missing` with the fix line, `tq run . -- claude` exits non-zero with that line, and the launch seam was not called.

### Q-01 The script stays fast and portable
- **Covers**: REQ-3, REQ-5
- **Method**: inspect
- **Steps**:
  1. Read the embedded script. Check that it uses no array, no GNU `timeout`, and only bash builtins, `sleep` and `kill`.
  2. `gofmt -l internal/hookscript internal/claudesettings` and `go vet ./...`
- **Expect**: Every subprocess is bounded, `gofmt` prints nothing, and `go vet` exits 0.

### R-01 The plugin hook still works until 014
- **Covers**: REQ-1
- **Method**: command
- **Steps**:
  1. `PYTHONPATH=plugin python -m pytest plugin/tests/test_identity_guard.py plugin/tests/test_tq_off.py -q`
- **Expect**: Exit 0 with no test removed or skipped compared with main.

### R-02 Named guard tests are unchanged
- **Covers**: REQ-2
- **Method**: test TestHookDeadlinesUnderHarnessTimeouts
- **Steps**:
  1. `go test ./internal/cmd -run 'TestHookDeadlinesUnderHarnessTimeouts|TestNoAskCodePath|TestPreToolUse_SecretRules|TestFlippedByteFailsVerification' -count=1`
  2. `go test ./internal/trust -count=1`
- **Expect**: Every package reports `ok`, and those tests changed nothing in the diff but additions.

### S-01 Removing the plugin does not remove the guard
- **Covers**: REQ-9
- **Method**: test TestHookScript_GuardWorksWithoutPlugin
- **Steps**:
  1. `go test ./internal/cmd -run '^TestHookScript_GuardWorksWithoutPlugin$' -count=1`
  2. The test builds tq, renders an identity with `enabledPlugins` set to false for tentaqles, and feeds a Read of `.env` to the rendered hook command.
- **Expect**: Exit 2 and stderr containing `BLOCKED [secrets/tq/env-file-read]`.

### S-02 An edited script loses trust
- **Covers**: REQ-8
- **Method**: test TestEditedHookScriptLosesTrust
- **Steps**:
  1. `go test ./internal/trust ./internal/cmd -run '^TestEditedHookScriptLosesTrust$' -count=1`
  2. The test flips one byte of `$TQ_HOME/hooks/pre-tool-use.sh` after signing.
- **Expect**: The workspace reads untrusted, and `tq hooks verify` names `hooks/pre-tool-use.sh`.

### S-03 An agent cannot write the script
- **Covers**: REQ-3, REQ-8
- **Method**: test TestProtectedPaths_HookScript
- **Steps**:
  1. `go test ./internal/cmd -run '^TestProtectedPaths_HookScript$' -count=1`
  2. Feed the hook a Write tool call on the script, `echo x > <script>`, `cp evil <script>` and `rm <script>`.
- **Expect**: Each is denied with `BLOCKED [self-protection/tq/protected-path]` and the file is unchanged.

### S-04 The environment does not pick the binary
- **Covers**: REQ-7
- **Method**: test TestHookScript_IgnoresEnvBinary
- **Steps**:
  1. `go test ./internal/hookscript -run '^TestHookScript_IgnoresEnvBinary$' -count=1`
  2. Set `TQ_BIN` and `CLAUDE_PLUGIN_ROOT` to a folder holding a fake tq that exits 0.
- **Expect**: The fake is never run, shown by its marker file staying absent.

### S-05 An ADR records the boundary
- **Covers**: REQ-1, REQ-6
- **Method**: inspect
- **Steps**:
  1. Read `docs/adr/0010-guard-hook-in-identity-settings.md` and `docs/threat-model.md`.
- **Expect**: The ADR states why the hook moved, the no-bash gap and the double run until ticket 014, and the threat model's B1 names the new script.
