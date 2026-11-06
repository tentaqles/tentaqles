# 001-dotenv-test-wsl eval

How to run: all commands from the ticket's worktree. `.job/` is gitignored, so the compiled test goes there.

## Checks

### U-01 The test passes with the WSL launcher first on PATH
- **Covers**: REQ-1
- **Method**: command
- **Steps**:
  1. `go test -c -o .job/cmd.test.exe ./internal/cmd`
  2. `PATH="/c/Windows/System32:$PATH" .job/cmd.test.exe -test.run '^TestDotenvRunLoadsAndMasks$' -test.v`
- **Expect**: Output contains `--- PASS: TestDotenvRunLoadsAndMasks` and the exit code is 0.

### F-01 No working bash means a skip, not a failure
- **Covers**: REQ-2
- **Method**: command
- **Steps**:
  1. With the binary from U-01, on a box that has Git for Windows, simulate having none. Run the test with an environment or seam the implementation documents for this, or read the code path if none exists and say so in the evidence.
  2. `PATH="/c/Windows/System32" .job/cmd.test.exe -test.run '^TestDotenvRunLoadsAndMasks$' -test.v`
- **Expect**: The output doesn't contain `--- FAIL`. If the helper finds Git for Windows through a known install path, `--- PASS` is correct here; a `--- SKIP` must carry a message saying no working bash was found.

### Q-01 The change is small and formatted
- **Covers**: REQ-1, REQ-2
- **Method**: command
- **Steps**:
  1. `gofmt -l internal/cmd/dotenv_test.go`
  2. `go vet ./internal/cmd`
- **Expect**: `gofmt` prints nothing and `go vet` exits 0.

### R-01 The rest of the command package still passes
- **Covers**: REQ-3
- **Method**: test TestDotenvRunRefusesNonEnvFiles
- **Steps**:
  1. `go test ./internal/cmd -count=1`
- **Expect**: Exit code 0 and `ok` for the package.

### S-01 The masking assertions haven't weakened
- **Covers**: REQ-3
- **Method**: inspect
- **Steps**:
  1. `git diff <base>...HEAD -- internal/cmd/dotenv_test.go`
- **Expect**: These lines are all unchanged: the check that `code != 7` fails, the check that the secret isn't in `out+errOut`, and the check for `pw=[REDACTED:DB_PASSWORD] region=us` and `err=[REDACTED:DB_PASSWORD]`.
