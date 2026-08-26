# tq

## What it is

`tq` gives every workspace folder its own terminal identity. Register a base
folder once, and each first-level subfolder under it becomes a separate
identity: its own git author/email, its own env vars, its own logged-in CLI
sessions (gh, gcloud, aws, whatever), all isolated from every other folder on
your machine. A shell hook watches your current directory and swaps the
identity in and out as you `cd` between client or project folders, so you
stop leaking one client's git email or API key into another client's repo.

## Install

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/tentaqles/tentaqles/main/cli/installers/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/tentaqles/tentaqles/main/cli/installers/install.ps1 | iex
```

From source (requires Go 1.22+):

```sh
go install github.com/tentaqles/tentaqles/cli/cmd/tq@latest
```

## Quick start

```sh
tq init ~/repos
```

Add this line to your shell profile (`.bashrc`, `.zshrc`, or your PowerShell
`$PROFILE`):

```sh
eval "$(tq activate bash)"   # or: zsh, pwsh
```

Then create a workspace and log in to whatever you need inside it:

```sh
tq add acme --git-email you@acme.com
tq login acme gh
cd ~/repos/acme
```

The moment you `cd` into `~/repos/acme`, your shell prompt/environment picks
up acme's git identity and any credentials you logged in with while inside
that workspace. `cd` back out, and none of it follows you.

## How it works

`tq env` is the workhorse: given the current directory, it prints the shell
commands needed to set or clear the workspace-scoped environment (`TQ_WS`,
`GIT_CONFIG_GLOBAL`, per-CLI config-home vars, etc.). The shell hook installed
by `tq activate` calls `tq env` on every prompt and evaluates its output,
using an internal `__TQ_STATE` marker to know what it set last time so it can
cleanly unset those variables when you leave the workspace — no leftover env
from a workspace you've already left.

Before any of that env is exported, the workspace's manifest must be
**trusted** with `tq allow`. A manifest that has changed since it was last
trusted, or was never trusted at all, is treated as untrusted: `tq env` and
the hook fail closed and export nothing for it, rather than silently loading
identity from a file that could have been tampered with. `tq deny` revokes
trust at any time.

Git identity is enforced the same way: each workspace gets its own git config
file, and until that workspace is trusted, `tq` will not point git at it —
commands that would touch git fail closed instead of falling back to your
global identity.

## Commands

| Command | Description |
|---|---|
| `tq version` | Print the tq version. |
| `tq init <base-folder>` | Register a base folder; each first-level subfolder becomes a terminal identity. |
| `tq add <name>` | Create a workspace folder with its manifest, identity dirs and git identity. |
| `tq allow <name>` | Trust a workspace's current manifest so it can export env. |
| `tq deny <name>` | Revoke trust for a workspace. |
| `tq list` | List workspaces under all registered bases. |
| `tq env` | Print the env changes for the current directory (used by the shell hook). |
| `tq activate <shell>` | Print the hook to add to your shell profile, e.g. `eval "$(tq activate bash)"`. |
| `tq login <workspace> <identity>` | Run a CLI's own login flow inside the workspace's private config home. |
| `tq run <workspace> -- <command> [args...]` | Run a command with a workspace's identity without cd-ing into it. |
| `tq doctor` | Verify hooks, trust, git and env against the manifests (never mutates). |

## Where things live

All `tq` state lives under `~/.tentaqles/`:

- `~/.tentaqles/config.json` — registered base folders.
- `~/.tentaqles/workspaces/<name>/manifest.json` — a workspace's identity
  definition (git email, allowed env, trust hash).
- `~/.tentaqles/workspaces/<name>/home/` — the private config home used for
  `tq login` / `tq run`, so CLI credentials for one workspace never mix with
  another's `~/.config` or `~/.aws`.
- `~/.tentaqles/workspaces/<name>/git/` — the workspace-scoped git config.

Nothing under `~/.tentaqles/` is meant to be edited by hand while a workspace
is trusted — editing the manifest invalidates trust and `tq allow` must be
run again.

## Limitations

- `cmd.exe` has no hook into directory changes, so the automatic `cd`-based
  identity switch does not work there. Use `tq run <workspace> -- <command>`
  to run a single command under a workspace's identity, or invoke the
  generated hook file directly from a `cmd.exe` script.
- The macOS Keychain integration path for storing CLI credentials referenced
  by `tq login` has not been independently verified on real Keychain-backed
  CLIs — treat it as best-effort until confirmed on your setup.

## Security

`tq` never stores secrets itself. Manifests hold only workspace names, git
identity metadata, and hashes used to detect tampering — no tokens, no
passwords, no API keys. Anything a CLI writes during `tq login` (a gh token,
a cloud credential file, etc.) is written by that CLI into the workspace's
own private config home, isolated from other workspaces, exactly as it would
be written to your real home directory if you weren't using `tq`.
