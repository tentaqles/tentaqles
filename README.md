# tentaqles

One terminal identity per client folder, for developers who work for several clients.

You keep one folder per client. A terminal opened in that folder, or in any folder under it, carries that client's
identity and no other:

- **Claude Code** runs with the client's own config home, so it is logged in to the client's own Claude account
  and subscription.
- **git** commits with the client's name and email.
- **Each CLI** (`gh`, `az`, `aws`, `gcloud`, `glab`, `databricks` and others) reads its own login for that client.
- **The browser**: links the terminal opens go to the client's Chrome or Edge profile.
- **Only the client's tools**: the CLIs of providers the client doesn't use answer `command not found` in that
  terminal.

A small hook in Claude Code blocks a `git`, `gh` or cloud command that would run under the wrong identity.

[docs/IDEATION.md](docs/IDEATION.md) describes what it is for.

## Install

Every install puts one program on the PATH under two names: `tentaqles`, and its alias `tq`, which the plugin hook
runs.

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.ps1 | iex
```

Both installers check the archive's SHA-256 against the release's `checksums.txt`, and, when `gh` is logged in,
its build provenance.

Homebrew or Scoop:

```sh
brew install --cask --no-quarantine tentaqles/tap/tq   # macOS / Linux
scoop bucket add tentaqles https://github.com/tentaqles/scoop-bucket && scoop install tq
```

`--no-quarantine` is needed on macOS because the binary is not signed with an Apple Developer certificate; the
`curl` installer doesn't need it.

From source (Go 1.26 or later):

```sh
go install github.com/tentaqles/tentaqles/cmd/tentaqles@latest
go install github.com/tentaqles/tentaqles/cmd/tq@latest
```

## Quick start

```sh
tentaqles reach ~/clients/acme
```

`reach` shows a banner and asks for the client's email, git host, clouds, team, data and task tools, email
provider and browser profile. After you type `yes` it:

1. writes the client's manifest under `~/.tentaqles/identities/acme/`, and a marker file in the folder;
2. points the folder's git at the client's name and email;
3. renders the client's Claude settings, the PATH mask and the browser shims;
4. installs the tentaqles plugin into the client's Claude config;
5. adds tq's activation block to your shell profiles, if it isn't there yet.

Then open a new terminal in `~/clients/acme`, start `claude` and log in with the client's account. Log each CLI in
once inside that terminal, or with `tq login acme gh`, and the login stays the client's.

Run `tentaqles reach` again in the same folder to change the client's providers, browser or email.

## Commands

See [docs/COMMANDS.md](docs/COMMANDS.md).

## What it does not do

The isolation is the terminal's environment. A program can still read another client's files, or run a CLI by its
full path, and the hook only reads the text of a shell command. Tentaqles prevents mistakes; it is not a sandbox.
See [docs/threat-model.md](docs/threat-model.md).
