# 003: `claude` is a shim on the identity PATH: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use a fake `claude` program that records its argv and environment to a file. Build the shim from the tree under test.

## Not covered

No test logs in to Claude Code. REQ-9 is proven by one real run of `claude` with both flags, recorded in the ADR, and the owner confirms a logged-in session after landing.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 The shim starts the identity's Claude Code
- **Covers**: REQ-1, REQ-2, REQ-3
- **Method**: test TestShim_StartsClaudeWithIdentity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestShim_StartsClaudeWithIdentity$' -count=1`
- **Expect**: The fake claude records `--dangerously-skip-permissions` first and `CLAUDE_CONFIG_DIR` under the identity's folder.

### U-02 The name resolves to the shim in each shell
- **Covers**: REQ-2
- **Method**: test TestShim_ResolvesFirstOnPath
- **Steps**:
  1. `go test ./internal/envplan -run '^TestShim_ResolvesFirstOnPath$' -count=1`
- **Expect**: The identity PATH lookup returns the file under `$TQ_HOME/shims` with Windows and MSYS PATH spellings.

### U-03 The flags pass as today
- **Covers**: REQ-6
- **Method**: test TestLaunchClaudeArgs
- **Steps**:
  1. `go test ./internal/cmd -run 'TestLaunchClaudeArgs|TestRunClaudeAlwaysBypass|TestShim_TypedFlagsWin' -count=1`
- **Expect**: All three pass, and a typed `--permission-mode plan` reaches the fake claude with no skip flag beside it.

### U-04 Precedence is documented
- **Covers**: REQ-9
- **Method**: inspect
- **Steps**:
  1. Read the precedence paragraph in `docs/CLAUDE-SETTINGS.md` and the run in `docs/adr/0011-claude-shim.md`.
- **Expect**: The ADR quotes the command, the Claude Code version and the observed result, and the doc states the same result.

### F-01 No real claude
- **Covers**: REQ-4
- **Method**: test TestShim_NoRealClaude
- **Steps**:
  1. `go test ./internal/cmd -run '^TestShim_NoRealClaude$' -count=1`
- **Expect**: With only the shim on PATH, it exits 127 with `claude: not found on the PATH of identity <ws>` within 2 s and spawns nothing.

### F-02 A mismatch refuses the launch
- **Covers**: REQ-7
- **Method**: test TestShim_RefusesOnIntegrityMismatch
- **Steps**:
  1. `go test ./internal/cmd -run '^TestShim_RefusesOnIntegrityMismatch$' -count=1`
- **Expect**: After an edited manifest, the shim exits non-zero, prints the changed file and `tq hooks verify --accept`, and the fake claude was not started.

### F-03 Stale shim after an upgrade
- **Covers**: REQ-1, REQ-7
- **Method**: test TestShim_RewrittenByRender
- **Steps**:
  1. `go test ./internal/cmd -run '^TestShim_RewrittenByRender$' -count=1`
- **Expect**: Replacing the tq binary and rendering rewrites the shim, and the workspace is trusted afterwards.

### Q-01 Start-up cost
- **Covers**: REQ-3
- **Method**: test BenchmarkShimStart
- **Steps**:
  1. `go test ./internal/cmd -run '^$' -bench '^BenchmarkShimStart$' -benchtime=20x`
- **Expect**: The shim's own work before starting claude takes under 150 ms per run.

### R-01 No change outside the flag
- **Covers**: REQ-8
- **Method**: test TestShim_NotOnPathWithoutFlag
- **Steps**:
  1. `go test ./internal/envplan -run '^TestShim_NotOnPathWithoutFlag$' -count=1`
- **Expect**: In a neutral folder and an unflagged identity, PATH is byte-identical to before.

### R-02 Leaving restores the PATH
- **Covers**: REQ-10
- **Method**: test TestLayer_EnterAndLeaveRestoresExactly
- **Steps**:
  1. `go test ./internal/envplan -run 'TestLayer_EnterAndLeaveRestoresExactly|TestState_FitsCmdLine|TestEnv_ScopeLayerRestoreAndV1' -count=1`
- **Expect**: All pass, with those tests changed only by additions.

### S-01 Arguments cannot become commands
- **Covers**: REQ-5
- **Method**: test TestShim_ArgvFidelity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestShim_ArgvFidelity$' -count=1`
  2. Pass the arguments `a & calc`, `%PATH%`, `^x`, `"q"`, `b|c` and a non-ASCII word through the built shim.
- **Expect**: The fake claude records each argument byte-identical, and no second program runs.

### S-02 The shim cannot be redirected to another program
- **Covers**: REQ-1, REQ-4
- **Method**: test TestShim_IgnoresPlantedClaudeInCwd
- **Steps**:
  1. `go test ./internal/cmd -run '^TestShim_IgnoresPlantedClaudeInCwd$' -count=1`
  2. Plant `claude.exe` in the working directory, and an entry `.` on PATH.
- **Expect**: The planted program runs only if `.` is really on the identity PATH after the real install folder, and the shim's lookup never treats the working directory as a default.

### S-03 An agent cannot change the shim
- **Covers**: REQ-1
- **Method**: test TestProtectedPaths_Shim
- **Steps**:
  1. `go test ./internal/cmd -run '^TestProtectedPaths_Shim$' -count=1`
  2. Feed the hook a Write call, a shell redirect and a delete on `$TQ_HOME/shims/claude.exe`.
- **Expect**: Each is denied with `BLOCKED [self-protection/tq/protected-path]`.

### S-04 Another identity's config home is not reached
- **Covers**: REQ-3
- **Method**: test TestShim_ClaudeConfigFromCwdNotEnv
- **Steps**:
  1. `go test ./internal/cmd -run '^TestShim_ClaudeConfigFromCwdNotEnv$' -count=1`
  2. Run the shim in identity A with `CLAUDE_CONFIG_DIR` already set to identity B's folder.
- **Expect**: The fake claude records A's folder.
