# 010: `tentaqles reach`: checks

Requirements: [rd.md](rd.md)

## Setup

Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`, and use the human-only seams, a scripted prompt reader, and fakes for the terminal launcher, process killer, package managers and `claude`.

## Not covered

A real terminal handover and a real login are the owner's U-08 and the first-time-user pass. A browser's own first run is out of scope.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 A named client
- **Covers**: REQ-1, REQ-5, REQ-9, REQ-10
- **Method**: test TestReach_NamedClientFullRun
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_NamedClientFullRun$' -count=1`
- **Expect**: The folder exists, the prompts ran in the stated order after the banner, and the seams recorded the ten steps in order, with the old terminal ended last.

### U-02 The current folder
- **Covers**: REQ-1
- **Method**: test TestReach_CurrentFolder
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_CurrentFolder$' -count=1`
- **Expect**: The identity name is the folder's name, and its parent is registered.

### U-03 First use asks for the clients directory
- **Covers**: REQ-2
- **Method**: test TestReach_FirstUseClientsDir
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_FirstUseClientsDir$' -count=1`
- **Expect**: With none registered it asks once and registers; with `--clients-dir` it doesn't ask; a second reach doesn't ask.

### U-04 Prompt options
- **Covers**: REQ-6, REQ-7, REQ-8, REQ-21
- **Method**: test TestReach_PromptSchema
- **Steps**:
  1. `go test ./internal/reach -run '^TestReach_PromptSchema$' -count=1`
- **Expect**: Each prompt's options equal the table, an invalid email re-asks, `Other` asks for text and lands in `other_providers`, and the task provider appears in `Scope()`.

### U-05 From a file
- **Covers**: REQ-18
- **Method**: test TestReach_From
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_From$' -count=1`
- **Expect**: No prompts appear but the `yes` does, and the identity matches the file.

### U-06 The flags
- **Covers**: REQ-11, REQ-14
- **Method**: test TestReach_NoInstallNoOnboardNoBanner
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_NoInstallNoOnboardNoBanner$' -count=1`
- **Expect**: `--no-install` runs no package manager, `--no-onboard` doesn't start claude, and `--no-banner` prints no art.

### U-07 The identity is fully set up
- **Covers**: REQ-12, REQ-13
- **Method**: test TestReach_ClaudeSetup
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_ClaudeSetup$' -count=1`
- **Expect**: Settings hold the guard hook, the shim exists, the scope block is in `CLAUDE.md`, and the plugin is registered; with registration forced to fail, reach still succeeds and the install line prints on first launch.

### U-08 The owner adds a real client
- **Covers**: REQ-10, REQ-15
- **Method**: human
- **Steps**:
  1. Run `tentaqles reach scratch-client` from PowerShell and answer the prompts.
- **Expect**: A new terminal opens with the banner and Claude Code starts its login; the old terminal closes.

### F-01 Already an identity
- **Covers**: REQ-3
- **Method**: test TestReach_AlreadyAnIdentity
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_AlreadyAnIdentity$' -count=1`
- **Expect**: Exit 1 with `is already an identity: use tentaqles edit`, with no byte changed under `$TQ_HOME`.

### F-02 A step fails and resumes
- **Covers**: REQ-17
- **Method**: test TestReach_ResumesFromFailedStep
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_ResumesFromFailedStep$' -count=1`
  2. Fail each of the steps in turn except the CLI install, then re-run.
- **Expect**: Each failure prints the exact line and leaves no trust record; the re-run asks no prompt, repeats no finished step, and ends with a valid identity.

### F-03 A CLI install fails
- **Covers**: REQ-11
- **Method**: test TestReach_FailedInstallContinues
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_FailedInstallContinues$' -count=1`
- **Expect**: The failure line prints and every later step still runs.

### F-04 The new terminal can't open
- **Covers**: REQ-15, REQ-16
- **Method**: test TestReach_TerminalHandover
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_TerminalHandover$' -count=1`
- **Expect**: When the new terminal fails, the kill seam was not called and the resume line prints; when the old one can't be identified, `Close this terminal when you are ready.` prints and exit is 0.

### F-05 Bad names and bad files
- **Covers**: REQ-19, REQ-20
- **Method**: test TestReach_BadInput
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_BadInput$' -count=1`
  2. Try `..`, `a/b`, `a\b`, `CON`, a name with a newline, a `--from` with an unknown key, one with a token-shaped value, and one whose `client` differs.
- **Expect**: Each refuses before any write and names the problem.

### Q-01 Ordering is enforced by code
- **Covers**: REQ-10
- **Method**: test TestReach_StepOrderIsFixed
- **Steps**:
  1. `go test ./internal/reach -run '^TestReach_StepOrderIsFixed$' -count=1`
- **Expect**: The steps come from one ordered list, and the test fails if any two are swapped.

### Q-02 Docs
- **Covers**: REQ-1
- **Method**: inspect
- **Steps**:
  1. Read `README.md`, `docs/SETUP.md` and `docs/COMMANDS.md`.
- **Expect**: Each starts a new client with `tentaqles reach`, with no step beyond the logins.

### R-01 Old commands still work until 015
- **Covers**: REQ-10
- **Method**: test TestAddConfirmsBeforeWriting
- **Steps**:
  1. `go test ./internal/cmd ./internal/onboard ./internal/workspace -count=1`
- **Expect**: `ok`, and `tq add`, `tq onboard`, `tq init` tests are unchanged.

### R-02 The identity passes the scenario
- **Covers**: REQ-12, REQ-21
- **Method**: test TestReach_ReferenceScenario
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_ReferenceScenario$' -count=1`
  2. Reach an Azure-only identity, then check PATH, the MCP list and the hook for `aws ec2 run-instances`.
- **Expect**: `aws` doesn't resolve, no AWS server is listed, the hook denies with the scope named, and `az` resolves.

### S-01 An agent cannot reach
- **Covers**: REQ-4
- **Method**: test TestReachDoesNotTrustFromAgent
- **Steps**:
  1. `go test ./internal/cmd ./internal/policy -run '^TestReachDoesNotTrustFromAgent$' -count=1`
  2. Under each detector alone, with and without `--from`, and through the hook as `tentaqles reach x` and `tq reach x`.
- **Expect**: Each refuses with `human-only` before any write, the hook denies with `BLOCKED [self-protection/tq/human-only-command]`, and a refusal is logged.

### S-02 No write before the yes
- **Covers**: REQ-9, REQ-18
- **Method**: test TestReachConfirmsBeforeWriting
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReachConfirmsBeforeWriting$' -count=1`
- **Expect**: Answering anything other than `yes`, with and without `--from`, leaves `$TQ_HOME` and the folder byte-identical.

### S-03 The manifest holds no secret
- **Covers**: REQ-8, REQ-19
- **Method**: test TestReach_NoSecretInManifest
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_NoSecretInManifest$' -count=1`
  2. Type a token-shaped value as the `Other` free text and as the browser profile.
- **Expect**: Reach refuses it and re-asks, and no file holds the value.

### S-04 Reach kills only its own parent shell
- **Covers**: REQ-15, REQ-16
- **Method**: test TestReach_KillsOnlyAShellParent
- **Steps**:
  1. `go test ./internal/reach -run '^TestReach_KillsOnlyAShellParent$' -count=1`
  2. Use the `ParentOf` seam with a parent of `pwsh`, `bash`, `explorer`, `node`, and a missing process.
- **Expect**: Only a known shell name is ended, and every other case prints the close line.

### S-05 The folder lends nothing before it's signed
- **Covers**: REQ-10
- **Method**: test TestReach_UntrustedUntilSigned
- **Steps**:
  1. `go test ./internal/cmd -run '^TestReach_UntrustedUntilSigned$' -count=1`
- **Expect**: With a failure before the signing step, the folder resolves as untrusted and the hook denies mutating git in it.
