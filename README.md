```text
████████╗███████╗███╗   ██╗████████╗ █████╗  ██████╗ ██╗     ███████╗███████╗
╚══██╔══╝██╔════╝████╗  ██║╚══██╔══╝██╔══██╗██╔═══██╗██║     ██╔════╝██╔════╝
   ██║   █████╗  ██╔██╗ ██║   ██║   ███████║██║   ██║██║     █████╗  ███████╗
   ██║   ██╔══╝  ██║╚██╗██║   ██║   ██╔══██║██║▄▄ ██║██║     ██╔══╝  ╚════██║
   ██║   ███████╗██║ ╚████║   ██║   ██║  ██║╚██████╔╝███████╗███████╗███████║
   ╚═╝   ╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝ ╚══▀▀═╝ ╚══════╝╚══════╝╚══════╝
```

**One folder per client. One identity per terminal.**

`cd` into a client's folder and Claude Code, git, `gh`, `az`, `aws` and your browser switch to that client's
accounts, Claude writes in that client's language, and the other clients' tools are gone.

[![Release](https://img.shields.io/github/v/release/tentaqles/tentaqles)](https://github.com/tentaqles/tentaqles/releases)
[![CI](https://github.com/tentaqles/tentaqles/actions/workflows/test.yml/badge.svg)](https://github.com/tentaqles/tentaqles/actions/workflows/test.yml)
![Platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-informational)

[Get started](#get-started) · [How it works](docs/how-it-works.md) · [Commands](docs/reference/commands.md) ·
[Threat model](docs/threat-model.md) · [All docs](docs/README.md)

- **The folder decides.** A terminal opened in a client's folder, or in any folder under it, carries that client's
  identity and nothing else. Leave the folder and your shell is exactly as it was.
- **Each client is its own account.** Its own Claude Code login and subscription, its own git name and email, its
  own login for every CLI, its own browser profile.
- **Mistakes get stopped, not just avoided.** A small Claude Code hook blocks a `git`, `gh` or cloud command that
  would run under the wrong identity, before it runs.

## See it in action

```console
~/clients/acme $ git config user.email
dev@acme.example
~/clients/acme $ gh api user --jq .login
acme-dev
~/clients/acme $ echo $CLAUDE_CONFIG_DIR
/home/you/.tentaqles/identities/acme/claude
~/clients/acme $ aws s3 ls                    # acme works on Azure, not AWS
aws: command not found (outside the scope of identity acme)

~/clients/acme $ cd ../globex/api             # another client, a subfolder of it
~/clients/globex/api $ git config user.email
dev@globex.example
~/clients/globex/api $ aws sts get-caller-identity --query Account
"123456789012"

~/clients/globex/api $ cd ~                   # out of every client: back to your own shell
~ $ echo $CLAUDE_CONFIG_DIR

```

And when a Claude Code session in acme's folder tries to commit after someone set another email by hand:

```text
BLOCKED [identity/git-email-drift]: Git email mismatch.
  Expected: dev@acme.example
  Actual:   me@personal.example
  Fix: tentaqles doctor (the client folder sets it; do not set user.email by hand)
```

## Why

If you work for several clients, you carry several identities: a GitHub user per client, an Azure subscription
here and an AWS account there, a company email for each one's commits, and, more and more, a Claude Code
subscription each company pays for. Switching them by hand works until the day it doesn't: a commit pushed with the
wrong email, an `az` command run against the wrong subscription, a Claude session billed to the wrong company, a
link opened in the wrong browser profile.

Tentaqles makes the folder you are in decide. You set a client up once, and from then on every terminal and every
Claude Code session in its folder is that client's.

## What each client gets

| | In a terminal under the client's folder |
|---|---|
| **Claude Code** | Its own config home (`CLAUDE_CONFIG_DIR`), so its own account and subscription, settings and plugins. |
| **Language** | Claude writes the client's reports, documents, emails and messages in the client's language: Portuguese (Brazil) for one client, English (United States) for another. |
| **git** | The client's name and email, in every repository under the folder. |
| **CLI logins** | `gh`, `glab`, `az`, `aws`, `gcloud`, `databricks`, `doctl` and [many more](docs/reference/providers.md#catalog) each read their own login for that client. |
| **Tools** | Only the client's: the CLIs of providers it doesn't use are not on the PATH, and their variables are unset. |
| **Browser** | Links the terminal opens go to the client's Chrome or Edge profile. |
| **Guard** | The tentaqles plugin for Claude Code blocks a `git`, `gh` or cloud command under the wrong identity. |

## Get started

**1. Install.**

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.ps1 | iex
```

<details>
<summary>Homebrew, Scoop, Go, and what the installers check</summary>

```sh
brew install --cask --no-quarantine tentaqles/tap/tq      # macOS / Linux
scoop bucket add tentaqles https://github.com/tentaqles/scoop-bucket
scoop install tq                                           # Windows
go install github.com/tentaqles/tentaqles/cmd/tentaqles@latest   # from source, Go 1.26+
go install github.com/tentaqles/tentaqles/cmd/tq@latest
```

Both installers check the archive's SHA-256 against the release's `checksums.txt` and, when `gh` is logged in, its
build provenance (`gh attestation verify`); they refuse to install when either fails. They install two names of the
same program: `tentaqles`, and its short alias `tq`. `install.sh` puts them in `~/.local/bin` and tells you when
that folder is not on your PATH; `install.ps1` puts them in `%LOCALAPPDATA%\tentaqles\bin` and adds it to your user
PATH. `TQ_BIN_DIR` picks another folder. `--no-quarantine` is needed on macOS because the binary is not signed with an
Apple Developer certificate.

</details>

**2. Make a folder a client.**

```sh
tentaqles reach ~/clients/acme
```

`reach` asks, one screen at a time: the client's email, its language (a list you filter by typing), its git host,
clouds, team, data and task tools, email provider, and browser profiles. It shows what it will do and writes nothing
until you type `yes`.

**3. Open a new terminal in the folder and log in once.**

```sh
cd ~/clients/acme
claude                        # log in with the client's Claude account
tentaqles login acme gh       # and each CLI the client uses: az, aws, gcloud...
```

That's it. From now on, every terminal opened under `~/clients/acme` is acme's. You get:

- the client's manifest in `~/.tentaqles/identities/acme/`, and a one-line marker in the folder;
- a git identity that applies to every repository under the folder;
- the client's own Claude Code config, with its settings and the tentaqles plugin installed;
- one login folder per CLI, under the same identity folder;
- a short activation block in your shell profiles (bash, zsh, fish, PowerShell).

The full walk-through, with what to check after each step, is in [Getting started](docs/getting-started.md).

## Everyday use

| You want to | Run |
|---|---|
| Set up a new client | `tentaqles reach <folder>` |
| Change one part of a client | `tentaqles add cloud aws`, `tentaqles add language pt-BR`, `tentaqles remove gh` |
| Change a client through the form | `tentaqles reach` in its folder |
| Log a CLI in for a client | `tentaqles login <client> <cli>` |
| Run one command as a client, from anywhere | `tentaqles run <client> -- <command>` |
| Open a link in the client's browser profile | `tentaqles open <url>` |
| See every client | `tentaqles list` |
| Check that everything is wired | `tentaqles doctor` |
| Undo a client as if it never existed | `tentaqles drop <client>` |

Every command is `tentaqles <command>`; `tq <command>` is the same thing, shorter. See the
[command reference](docs/reference/commands.md).

## Platforms

| | |
|---|---|
| **Operating systems** | Windows (x64, ARM64), macOS (Intel, Apple Silicon), Linux (x64, ARM64) |
| **Shells, automatic** | bash (Git Bash included), zsh, fish, Windows PowerShell 5.1, PowerShell 7 |
| **Shells, by hand** | cmd.exe: no prompt hook, so run the activation after `cd`, and start Claude with `tentaqles run . -- claude` |
| **Browsers** | Chrome and Edge profiles |
| **Claude Code plugin** | Needs bash: Git Bash on Windows, which Claude Code needs there too |

Any terminal app works (Windows Terminal, the VS Code terminal, iTerm2, GNOME Terminal); what matters is the shell
inside it.

## How it compares

- **direnv, mise**: load variables per folder. tentaqles does that too, and adds the identity on top: the login
  folders, the PATH without other clients' tools, git, Claude Code and the guard.
- **git `includeIf`**: switches the commit email per folder. tentaqles writes those rules for you, and covers every
  other CLI.
- **`gh auth switch`, AWS profiles, `az account set`**: switch one tool's account by hand, globally. tentaqles keeps
  each login apart, so two terminals can be two clients at once.
- **Separate OS users or VMs**: a real boundary, and a lot of friction. tentaqles is a convenience layer that prevents
  mistakes; see [what it does not do](#what-it-does-not-do).

## What it does not do

The isolation is the terminal's environment. A program can still read another client's files or run a CLI by its full
path, and the hook reads the text of a shell command, not what it ends up doing. Tentaqles prevents mistakes; it is
not a sandbox. The [threat model](docs/threat-model.md) says exactly what it does and does not protect against.

## Documentation

- [Getting started](docs/getting-started.md): from install to a first client, step by step.
- [How it works](docs/how-it-works.md): folders, manifests, the shell hook, git, Claude Code, language and browsers.
- [Commands](docs/reference/commands.md): every command and flag.
- [Manifest and files](docs/reference/manifest.md): every manifest key, the files under `~/.tentaqles`, the
  environment variables.
- [Providers](docs/reference/providers.md): the CLIs tentaqles isolates, and how to add your own.
- [Claude Code hook](docs/reference/claude-hook.md): what the plugin checks and blocks.
- [Troubleshooting](docs/troubleshooting.md): every `tentaqles doctor` finding and its fix.
- [Threat model](docs/threat-model.md): what it protects against, and what it doesn't.

## Contributing

The code is Go; [CLAUDE.md](CLAUDE.md) maps the packages and lists the tests that guard each promise above.
`go test ./...` runs the suite; the plugin's tests are `PYTHONPATH=plugin python -m pytest plugin/tests -q`.
Releases follow [docs/RELEASING.md](docs/RELEASING.md).
