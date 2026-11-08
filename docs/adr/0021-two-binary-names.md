# ADR-0021: The release ships `tentaqles` and its alias `tq`

Status: accepted
Date: 2026-11-08
Cites: `docs/IDEATION.md` (the spec: "The command should be `tentaqles reach`"), ADR-0005 (self-protection: the tq binary is a protected path), ADR-0008 (the PATH-mask stub is a copy of the binary), ADR-0012 (reach), ADR-0016 (the browser shims are copies of the binary); threat model TM-1, TM-2 (checksums and provenance)

## Context

IDEATION's command is `tentaqles reach`. The code has answered to both names for a while (`cmd/tentaqles`,
`cmd/tq`, and the self-protection rule reads both), but `.goreleaser.yaml` built only `tq`, so an installed
machine had no `tentaqles` and the spec's first command failed.

Three things copy or match the binary by name or by bytes:

- The plugin hook (`plugin/scripts/tq_hook.sh`) resolves `tq` on PATH and in the install folders, never from
  the environment.
- The browser shims (`~/.tentaqles/shims/<client>/`) and the PATH-mask stubs are copies of the running binary
  (`envplan.StubSource`, which takes `tq` or `tentaqles`). `envplan.ShimsReady`, read by `tq doctor` and the
  dry-run render, compares the shims' size with the running binary.
- `internal/protect` protects the running binary, its folder when it is tentaqles' own, and in a shared folder
  (`~/.local/bin`) the files named like it.

## Decision

- **One program, two names, the same bytes.** `.goreleaser.yaml` has two builds, `tq` and `tentaqles`, of the
  same package (`./cmd/tentaqles`) with the same `env`, `-trimpath`, `ldflags`, OS and arch lists; they differ
  only in `id` and `binary`. Go's output does not depend on the output file name, so the two binaries are byte
  for byte the same (checked on a snapshot release: the Windows and macOS pairs hash the same). A shim or stub
  written by `tentaqles reach` therefore matches a later `tq doctor`, and the reverse. `cmd/tq` stays in the
  tree for `go install .../cmd/tq` and for `go run ./cmd/tq`, which the self-protection rule still reads.
  `TestGoreleaserBuildsOneProgramUnderTwoNames` keeps the two builds identical.
- **Every archive carries both.** The archive lists both build ids. The Scoop manifest installs every binary of
  the archive; the Homebrew cask names both (`binaries: [tq, tentaqles]`). Archive names (`tq_<ver>_<os>_<arch>`)
  and `checksums.txt` are unchanged, so the installers' checksum and provenance checks (TM-1, TM-2) cover both
  binaries through the one archive they verify.
- **The installers install both** (`install.sh` copies both, failing on a missing one under `set -e`;
  `install.ps1` expands the zip and refuses an archive without either), and their next step is
  `tentaqles reach <client-folder>`.
- **`tq` stays the name the hook runs.** `tq_hook.sh` is unchanged: it resolves `tq`, so the enforcing binary is
  found the same way as before, and nothing reads a binary name from the environment.
- **Both names are protected beside each other.** In a shared bin folder `internal/protect` now matches
  `tentaqles` and `tentaqles.*` as well as `tq` and `tq.*` (`isTqBinaryName`), so a session can't overwrite the
  `tentaqles` beside the `tq` the hook runs as, or the reverse. A folder of tentaqles' own was already protected
  whole. Tested in `TestBinaryDirectory`, with names that only start like the program (`tqx`,
  `tentaqles-helper`) left free.

## Consequences

- The release is about twice its size (two copies of the binary per archive). Accepted: a link or a copy made by
  the installer would differ per package manager, and Scoop and Homebrew install what the archive holds.
- A machine that installs by `go install` gets the name of the package it names, as before; the docs give both.
- A `tentaqles` replaced by something else in a folder tq does not protect (a separate folder earlier on PATH) is
  a program run as the developer's user (AR1), as it is for `tq`.

## Alternatives considered

| Option | Why not |
|---|---|
| Ship only `tentaqles` and drop `tq` | The plugin hook, the docs and every existing install call `tq`; renaming what the hook runs is a trust-boundary change of its own, for no gain |
| Build `tq` from `./cmd/tq` and `tentaqles` from `./cmd/tentaqles` | The binaries differ by their build info, so `ShimsReady` reports shims written by the other name as missing and every render rewrites them |
| Make the installers create a link or a copy | Scoop and Homebrew install the archive's binaries, not the installer scripts' steps; three install paths would diverge |
