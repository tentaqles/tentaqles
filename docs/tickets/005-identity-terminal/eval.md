# 005: The identity terminal opens with the banner, and cmd.exe routes itself: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`. AutoRun tests use the fake registry `Deps.Reg` of `internal/migrate`, or an equivalent seam, and never write the real registry. U-05 is the one manual check, and the owner runs it.

## Not covered

Real window rendering of the art is not machine-checked; U-05 is the owner's check. Agent writes to the AutoRun value with `reg add` are not blocked: the guard is a text filter and that spelling is out of scope for this ticket (AR2).

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 The banner prints
- **Covers**: REQ-1, REQ-2, REQ-5
- **Method**: test TestBanner_UTF8
- **Steps**:
  1. `go test ./internal/banner -run '^TestBanner_UTF8$' -count=1`
- **Expect**: Art, then the exact status line with the version and name, then the folder, with cyan art and no line wider than 77 columns.

### U-02 Each shell prints it first
- **Covers**: REQ-6, REQ-11
- **Method**: test TestHook_Golden
- **Steps**:
  1. `go test ./internal/shell -run 'TestHook_Golden|TestHook_BannerIsFirstOutput' -count=1`
- **Expect**: In each template the banner call runs on terminal start, before any other print.

### U-03 cd prints the status lines once
- **Covers**: REQ-7
- **Method**: test TestEnv_StatusOnEntry
- **Steps**:
  1. `go test ./internal/cmd -run '^TestEnv_StatusOnEntry$' -count=1`
- **Expect**: First entry prints two lines, a second prompt in the same folder prints none, and leaving and returning prints two again.

### U-04 AutoRun round trip
- **Covers**: REQ-9, REQ-12, REQ-13
- **Method**: test TestAutoRun_InstallRemoveRestoresExactly
- **Steps**:
  1. `go test ./internal/hooks -run '^TestAutoRun_InstallRemoveRestoresExactly$' -count=1`
- **Expect**: With no value, a REG_SZ value and a REG_EXPAND_SZ value, install then remove restores the exact value and type, and status reports each state.

### U-05 The owner opens a terminal
- **Covers**: REQ-6, REQ-11
- **Method**: human
- **Steps**:
  1. On a flagged identity, open PowerShell and cmd.exe in the folder, and look at the first output.
- **Expect**: The art is readable and aligned, and the status line names the identity and `guard: on`.

### F-01 No UTF-8
- **Covers**: REQ-3
- **Method**: test TestBanner_FallbackIsPureASCII
- **Steps**:
  1. `go test ./internal/banner -run '^TestBanner_FallbackIsPureASCII$' -count=1`
- **Expect**: Every output byte is below 0x80, and the status line uses ` | `.

### F-02 Not a terminal
- **Covers**: REQ-4
- **Method**: test TestBanner_Silent
- **Steps**:
  1. `go test ./internal/banner -run '^TestBanner_Silent$' -count=1`
- **Expect**: With `TQ_NO_BANNER=1`, `--no-banner` or a piped stdout the output is empty, and with `NO_COLOR` it holds no ESC byte.

### F-03 A broken AutoRun target
- **Covers**: REQ-9, REQ-10
- **Method**: test TestAutoRun_MissingScriptIsSilent
- **Steps**:
  1. `go test ./internal/hooks -run '^TestAutoRun_MissingScriptIsSilent$' -count=1`
- **Expect**: The AutoRun value is written so that a deleted target gives no error text and exit 0 in a new cmd.exe.

### Q-01 cmd /c stays fast and silent
- **Covers**: REQ-10
- **Method**: test TestAutoRun_CmdCIsUntouched
- **Steps**:
  1. `go test ./internal/hooks -run '^TestAutoRun_CmdCIsUntouched$' -count=1`
  2. On Windows the test runs the script under `cmd /c echo ok` and `cmd /k exit` directly.
- **Expect**: Output is exactly `ok`, tq was not started, and the environment is unchanged.

### R-01 No change without the flag
- **Covers**: REQ-8
- **Method**: test TestBanner_NotForUnflagged
- **Steps**:
  1. `go test ./internal/cmd -run '^TestBanner_NotForUnflagged$' -count=1`
- **Expect**: A neutral folder and an unflagged identity print nothing, and their `tq env` output is byte-identical to before.

### R-02 Migrate clears only the legacy value
- **Covers**: REQ-13
- **Method**: test TestCmdStep_LeavesTqAutoRun
- **Steps**:
  1. `go test ./internal/migrate -count=1`
- **Expect**: `ok`, the legacy value is still cleared, and tq's value is reported as not legacy.

### S-01 AutoRun is human-only
- **Covers**: REQ-9
- **Method**: test TestHooksInstallRefusesAgents
- **Steps**:
  1. `go test ./internal/cmd -run 'TestHooksInstallRefusesAgents|TestHumanOnlyCommandsRefuseWithMarkerAndLog' -count=1`
- **Expect**: With `CLAUDECODE=1`, `tq hooks install` exits 1 with `human-only`, the fake registry has no write, and a refusal is logged.

### S-02 The AutoRun script cannot be written by an agent
- **Covers**: REQ-9
- **Method**: test TestProtectedPaths_ShellInit
- **Steps**:
  1. `go test ./internal/cmd -run '^TestProtectedPaths_ShellInit$' -count=1`
  2. Feed the hook a Write call and `echo x > <script>` on the script under `$TQ_HOME/shell/`.
- **Expect**: Each is denied with `BLOCKED [self-protection/tq/protected-path]`.

### S-03 A folder name cannot inject into cmd.exe
- **Covers**: REQ-11
- **Method**: test TestAutoRun_HostileFolderName
- **Steps**:
  1. `go test ./internal/hooks ./internal/shell -run '^TestAutoRun_HostileFolderName$' -count=1`
  2. Enter an identity root named `a & calc` and one holding `%PATH%`.
- **Expect**: The status line prints the name as text, and no second program runs.

### S-04 The banner holds no secret
- **Covers**: REQ-2
- **Method**: inspect
- **Steps**:
  1. Read `internal/banner/` and its callers, and read `docs/adr/0013-identity-terminal-and-cmd-autorun.md`.
- **Expect**: It prints only version, identity name, guard state and folder, and the ADR records the AutoRun risk and the `cmd /c` exit.

### Q-02 The cmd.exe limit is documented
- **Covers**: REQ-14
- **Method**: inspect
- **Steps**:
  1. Read the cmd.exe paragraph of `docs/COMMANDS.md`.
- **Expect**: It says the identity is decided when cmd.exe starts, and that `cd` to another client does not switch it.
