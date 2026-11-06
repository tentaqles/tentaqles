# 001-dotenv-test-wsl: The dotenv masking test runs on a real bash

**Status:** landed
**Risk:** Medium: the test that proves `tq dotenv run` masks secrets fails, or silently proves nothing, on a Windows box whose `bash` on PATH is the WSL launcher

`TestDotenvRunLoadsAndMasks` in `internal/cmd/dotenv_test.go` looks up `bash` with `exec.LookPath`. On Windows with PowerShell that can find `C:\Windows\System32\bash.exe`, the WSL launcher, which fails with `execvpe(/bin/bash) failed` when no distribution has it. The test then fails with "exit code not propagated". This is the only failing test on the owner's box.
**UI:** none

## Requirements
- REQ-1 On Windows, the test runs the child command with a working bash when one exists, such as Git for Windows, even if the WSL launcher comes first on PATH.
- REQ-2 On Windows with no working bash, the test skips with a message saying why. It doesn't fail.
- REQ-3 What the test asserts doesn't weaken: the exit code still propagates, the secret never appears in either stream, and both streams still show the masked value.

## Out of scope
- `tq dotenv run` itself. Its behaviour doesn't change.
- Other tests that call bash.

## Affected areas
- `internal/cmd/dotenv_test.go`
- `CHANGELOG.md`

## Do not touch
- `internal/dotenv/`
- `internal/cmd/dotenv.go`

## Decisions
- A working bash means one that runs `bash -c "exit 0"` successfully, not merely one that exists on PATH.

## Definition of done
The test passes on the owner's box under PowerShell's PATH, and the secret-masking assertions are unchanged.
