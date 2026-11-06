# 004: `tentaqles` is the CLI, `tq` is its alias: checks

Requirements: [rd.md](rd.md)

## Setup

`go build -o .job/004/bin/ ./cmd/tq ./cmd/tentaqles`. Run from the ticket's worktree.

## Not covered

A real release is not cut: goreleaser runs as a snapshot if it is installed, otherwise its config is read. The owner checks the first tagged release as `docs/RELEASING.md` describes.

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 Both names run the same command
- **Covers**: REQ-2
- **Method**: test TestBothNamesGiveTheSameOutput
- **Steps**:
  1. `go test ./test/integration -run '^TestBothNamesGiveTheSameOutput$' -count=1`
  2. The test builds both, and for `version`, `list`, `doctor --json`, `providers list`, an unknown command and a usage error compares stdout, stderr and the exit code.
- **Expect**: Byte-identical for each.

### U-02 Help reads tentaqles
- **Covers**: REQ-3
- **Method**: command
- **Steps**:
  1. `.job/004/bin/tq --help`
  2. `.job/004/bin/tq run --help`
- **Expect**: The usage lines read `tentaqles` and `tentaqles run`, and neither output holds the word `tq ` as a command.

### U-03 Both names in each shell
- **Covers**: REQ-7
- **Method**: test TestHook_Golden
- **Steps**:
  1. `go test ./internal/shell -run 'TestHook_Golden|TestHook_BothNames' -count=1`
- **Expect**: Each shell's hook defines both names, and PowerShell's `--` handling covers both.

### F-01 An installer with one binary missing
- **Covers**: REQ-1
- **Method**: test TestInstallers_RefuseAnArchiveMissingABinary
- **Steps**:
  1. `go test ./test/integration -run '^TestInstallers_RefuseAnArchiveMissingABinary$' -count=1`
- **Expect**: On an archive with only `tq` the installer exits non-zero and installs nothing. The test skips with a stated reason on an OS whose installer it can't run.

### F-02 One name deleted from disk
- **Covers**: REQ-8
- **Method**: test TestHookScript_FindsEitherName
- **Steps**:
  1. `go test ./internal/hookscript -run '^TestHookScript_FindsEitherName$' -count=1`
  2. `PYTHONPATH=plugin python -m pytest plugin/tests/test_identity_guard.py -q`
- **Expect**: With only `tentaqles` or only `tq` present, the script runs it; with both, it runs `tentaqles`; the plugin tests exit 0.

### Q-01 No user-visible tq left
- **Covers**: REQ-3, REQ-4
- **Method**: test TestNoTqInUserVisibleText
- **Steps**:
  1. `go test ./internal/cmd -run '^TestNoTqInUserVisibleText$' -count=1`
- **Expect**: Walking the cobra tree's Short, Long, Use and Example finds no `tq ` command word.

### Q-02 The release config stays valid
- **Covers**: REQ-1
- **Method**: inspect
- **Steps**:
  1. Read `.goreleaser.yaml`, and run `goreleaser check` if goreleaser is installed.
- **Expect**: Two builds with the same ldflags, `project_name: tq` unchanged, and both binaries listed for Scoop and the Homebrew cask.

### R-01 The release build still injects the commit
- **Covers**: REQ-1
- **Method**: test TestReleaseBuildInjectsTheCommit
- **Steps**:
  1. `go test ./... -run 'TestReleaseBuildInjectsTheCommit|TestRootMarketplacePin' -count=1`
- **Expect**: Both pass and both binaries carry the commit.

### R-02 State does not move
- **Covers**: REQ-9
- **Method**: test TestHome_IgnoresTQHomeUnderAnAgentMarker
- **Steps**:
  1. `go test ./internal/paths ./internal/cmd -count=1`
- **Expect**: `ok`, with no `TQ_*` name changed in the diff.

### S-01 The guard matches every spelling
- **Covers**: REQ-5
- **Method**: test TestSelfProtection_BothNames
- **Steps**:
  1. `go test ./internal/policy -run '^TestSelfProtection_BothNames$' -count=1`
  2. For every human-only verb, feed the hook `tentaqles <verb>`, `tentaqles.exe <verb>`, `C:\x\tentaqles.exe <verb>` and `/usr/local/bin/tentaqles <verb>`, plus `tq` in the same forms.
- **Expect**: Each is denied with `BLOCKED [self-protection/tq/human-only-command]`, and `tentaqles status` and `tentaqles version` pass.

### S-02 The binary under either name is protected
- **Covers**: REQ-6
- **Method**: test TestProtectedPaths_BothBinaries
- **Steps**:
  1. `go test ./internal/cmd -run '^TestProtectedPaths_BothBinaries$' -count=1`
  2. With the hook running as `tq`, feed a Write call and `cp evil <dir>/tentaqles`, then the same with the names swapped.
- **Expect**: Each is denied with `BLOCKED [self-protection/tq/protected-path]`.

### S-03 Human-only refuses under the new name
- **Covers**: REQ-4, REQ-5
- **Method**: test TestHumanOnlyCommandsRefuseWithMarkerAndLog
- **Steps**:
  1. `go test ./internal/cmd -run 'TestHumanOnlyCommandsRefuseWithMarkerAndLog|TestAllowAndDenyRefuseAgents' -count=1`
- **Expect**: Both pass, and the refusal text reads `tentaqles allow: human-only`, with only that word changed in the tests.

### S-04 ADR and threat model
- **Covers**: REQ-1, REQ-6
- **Method**: inspect
- **Steps**:
  1. Read `docs/adr/0012-tentaqles-name-and-tq-alias.md` and the matching lines of `docs/threat-model.md`.
- **Expect**: The ADR names both binaries, the unchanged archive name and the guard's matching, and the threat model's distribution boundary names both binaries.
