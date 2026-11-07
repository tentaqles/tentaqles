# 022: An agent can't run a command under another client: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use the `execCommand` seam, so no real command runs.

## Not covered

A live Claude Code session: the seams set the same marker and process tree a session has. The owner sees it in the end-to-end run.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 An agent keeps its own client
- **Covers**: REQ-4
- **Method**: test TestRunFromAgentSession
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunFromAgentSession$' -count=1`
- **Expect**: `tq run acme` from acme's folder, and `tq run .`, launch with `TQ_WS=acme` under a marker. The test is kept and extended.

### U-02 A human runs any client
- **Covers**: REQ-5
- **Method**: test TestRunOtherClient_HumanAllowed
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunOtherClient_HumanAllowed$' -count=1`
- **Expect**: From another client's folder and a neutral folder, the launch happens with `TQ_WS` of the named client, and no prompt is asked.

### F-01 An unknown client
- **Covers**: REQ-1
- **Method**: test TestRunOtherClient_UnknownName
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunOtherClient_UnknownName$' -count=1`
- **Expect**: Under a marker, a name that isn't a client exits 1 and starts nothing, and the message doesn't list other clients' names.

### F-02 No human ancestry
- **Covers**: REQ-1
- **Method**: test TestRunOtherClient_ProcessTreeUnverified
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunOtherClient_ProcessTreeUnverified$' -count=1`
- **Expect**: With no marker and an unverified process tree, naming another client refuses.

### Q-01 The change is small
- **Covers**: REQ-1
- **Method**: inspect
- **Steps**:
  1. `git diff --stat` against the base, and read `internal/cmd/run.go`.
- **Expect**: One check in `run.go` and no change under `internal/policy`, `internal/guard` or `internal/humanonly`.

### R-01 The launcher holds
- **Covers**: REQ-4, REQ-5
- **Method**: test TestRunClaudeAlwaysBypass
- **Steps**:
  1. `go test ./internal/cmd -run 'TestRunClaudeAlwaysBypass|TestLaunchClaudeArgs|TestRunCommandFlagsAreNotTqFlags|TestFreeCommandsRunUnderAMarker' -count=1`
- **Expect**: All pass, with only `tq run <ws>` moved in the last one.

### S-01 Another client is refused, under every detector
- **Covers**: REQ-1, REQ-2
- **Method**: test TestRunOtherClient_RefusedForAgents
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunOtherClient_RefusedForAgents$' -count=1`
  2. Under each entry of `agentMarkers` alone, and under a harness in the process tree through the `ParentOf` seam, run `tq run beta -- gh pr list` from alpha's folder.
- **Expect**: Exit 1, the exact message, the `execCommand` seam not called, and one denial line with category `identity`.

### S-02 A neutral folder is refused
- **Covers**: REQ-3
- **Method**: test TestRunOtherClient_NeutralFolderRefused
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunOtherClient_NeutralFolderRefused$' -count=1`
- **Expect**: Under a marker in a neutral folder, `tq run beta -- az group list` exits 1 and starts nothing.

### S-03 Nothing of the other client leaks
- **Covers**: REQ-6
- **Method**: test TestRunOtherClient_NoLeak
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunOtherClient_NoLeak$' -count=1`
- **Expect**: The refusal text and the denial line hold no path under beta's identity folder and none of beta's manifest values.

### S-04 An inherited variable doesn't help
- **Covers**: REQ-1
- **Method**: test TestRunOtherClient_InheritedEnvIgnored
- **Steps**:
  1. `go test ./internal/cmd -run '^TestRunOtherClient_InheritedEnvIgnored$' -count=1`
- **Expect**: With `TQ_WS=beta` and `TQ_WS_ROOT` set to beta's folder, from alpha's folder under a marker, `tq run beta` is still refused.
