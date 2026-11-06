# 020: `reach` opens the client's terminal and closes the old one: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use seams for the terminal launcher, the parent lookup and the process killer. No real terminal opens or closes, and no real shell profile is touched.

## Not covered

A real window opening and closing is the owner's U-04.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 The handover
- **Covers**: REQ-1, REQ-4, REQ-10
- **Method**: test TestReach_Handover
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_Handover$' -count=1`
- **Expect**: The launcher got the client's folder, then all output printed, then the Enter prompt, then the kill seam got the parent's pid. That order is asserted.

### U-02 The profile hook
- **Covers**: REQ-2, REQ-3
- **Method**: test TestReach_InstallsProfileHook
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_InstallsProfileHook$' -count=1`
- **Expect**: With a temp profile and no block, the block is written after the `yes`, the confirmation says so, and a second reach writes nothing more.

### U-03 --no-terminal
- **Covers**: REQ-7
- **Method**: test TestReach_NoTerminalFlag
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_NoTerminalFlag$' -count=1`
- **Expect**: Neither seam is called, and the next step prints.

### U-04 The owner adds a client
- **Covers**: REQ-1, REQ-3, REQ-4
- **Method**: human
- **Steps**:
  1. Run `tentaqles reach` on a scratch client from PowerShell, then in the new terminal run `$env:CLAUDE_CONFIG_DIR`.
- **Expect**: A new terminal opens in the folder with the client's config folder set, and pressing Enter closes the old one.

### F-01 The new terminal can't open
- **Covers**: REQ-6
- **Method**: test TestReach_TerminalWontOpen
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_TerminalWontOpen$' -count=1`
- **Expect**: The exact line prints, the kill seam isn't called, exit 0, and the client is trusted.

### F-02 The old terminal can't be identified or ended
- **Covers**: REQ-4, REQ-5
- **Method**: test TestReach_OldTerminalNotEnded
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_OldTerminalNotEnded$' -count=1`
- **Expect**: For parents `explorer`, `node`, `python`, an unreadable parent, and a kill that fails, `Close this terminal when you are ready.` prints, and exit is 0.

### F-03 A closed input at the pause
- **Covers**: REQ-10
- **Method**: test TestReach_PauseClosedInput
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_PauseClosedInput$' -count=1`
- **Expect**: It proceeds and doesn't hang.

### Q-01 Docs
- **Covers**: REQ-1
- **Method**: inspect
- **Steps**:
  1. Read `docs/COMMANDS.md` and `README.md`.
- **Expect**: Both describe the handover and `--no-terminal`.

### R-01 No handover when reach did nothing new
- **Covers**: REQ-8, REQ-9
- **Method**: test TestReach_NoHandoverWithoutANewClient
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_NoHandoverWithoutANewClient$' -count=1`
- **Expect**: An existing client, a declined `yes`, and a bad folder call neither seam.

### R-02 Reach's other tests hold
- **Covers**: REQ-1
- **Method**: test TestReach_PathMakesATrustedClient
- **Steps**:
  1. `go test ./internal/cmd ./internal/termopen ./internal/hooks -count=1`
- **Expect**: `ok` for each.

### S-01 Only the direct parent shell is ended
- **Covers**: REQ-4
- **Method**: test TestReach_KillsOnlyADirectShellParent
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_KillsOnlyADirectShellParent$' -count=1`
- **Expect**: With a chain of `reach` under `python` under `pwsh`, nothing is ended. Under `pwsh`, only that pid is.

### S-02 A hostile folder name
- **Covers**: REQ-11
- **Method**: test TestPlan_HostileFolderNames
- **Steps**:
  1. `go test ./internal/termopen -run '^TestPlan_HostileFolderNames$' -count=1`
- **Expect**: For `a & calc`, `a;b`, `%PATH%` and a quote, the folder is one argv element or the working directory, and never part of a parsed command line.

### S-03 An agent never gets here
- **Covers**: REQ-4
- **Method**: test TestReachDoesNotTrustFromAgent
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReachDoesNotTrustFromAgent$' -count=1`
- **Expect**: `ok`, and under an agent marker neither seam is called.

### S-04 The profile block is the same one
- **Covers**: REQ-2
- **Method**: test TestReach_ProfileBlockIsTqHooksInstall
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_ProfileBlockIsTqHooksInstall$' -count=1`
- **Expect**: The bytes reach writes equal what `tq hooks install` writes, so `tq hooks remove` takes it back.
