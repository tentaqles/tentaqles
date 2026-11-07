# tentaqles

An identity manager for developers who work for several clients. One folder per client: a terminal or a
Claude Code session opened there sees only that client's tools and accounts, and a hook blocks what belongs to
another client. [docs/IDEATION.md](docs/IDEATION.md) is the spec.

## Install

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.ps1 | iex
```

Homebrew or Scoop:

```sh
brew install --cask --no-quarantine tentaqles/tap/tq   # macOS / Linux
scoop bucket add tentaqles https://github.com/tentaqles/scoop-bucket && scoop install tq
```

`--cask` is required for `--no-quarantine` to be accepted at all: it is a
cask-only option, and `brew install --no-quarantine ...` fails with
`invalid option`. If it is already installed, use `brew reinstall --cask
--no-quarantine tentaqles/tap/tq`.

`--no-quarantine` is not optional on macOS today. `tq` is not signed with an
Apple Developer certificate, so Homebrew's default quarantine makes Gatekeeper
refuse it with *"Apple could not verify tq is free of malware"*. The `curl`
installer above has no such problem -- files fetched with `curl` are never
quarantined. If you already installed it and hit the block:

```sh
xattr -dr com.apple.quarantine "$(brew --prefix)/Caskroom/tq"
```

From source (requires Go 1.26+):

```sh
go install github.com/tentaqles/tentaqles/cmd/tq@latest
```

## Quick start

```powershell
tq hooks install                     # once: terminals follow the folder
tentaqles reach C:\clients\acme      # the banner, a short form, then a typed yes
```

reach then opens a new terminal in that folder, and ends the terminal it ran in after you press Enter (only when
that is an interactive shell; `--no-terminal` skips both, and see ADR-0019). In the new terminal, start `claude`. In it:

- Only the CLIs of that client's providers run. Another provider's CLI answers `command not found`.
- `git`, `gh`, `az` and the other CLIs use that client's account.
- Claude Code uses that client's login.
- If an agent reaches for another client's tool anyway, by path or by API, the hook blocks it and names the
  scope.
- An agent cannot change the guard, the manifest or the identity. Those commands are human-only.

## Commands

See [docs/COMMANDS.md](docs/COMMANDS.md).

## Status

Being rebuilt around IDEATION. Installing the CLIs during `reach`, browser profiles and the new onboarding skill are
not built yet. The handover to a new terminal is built (ADR-0019).

## Security

See [docs/threat-model.md](docs/threat-model.md). The guard is a speed bump for shell text, not a sandbox: it
stops mistakes and casual reach, not a program already running as your user.
