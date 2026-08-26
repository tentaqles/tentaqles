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
`TQ_WS_ROOT`, and one config-home variable per isolated CLI, e.g.
`CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`). The shell hook installed
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

Git identity follows the same trust boundary. Each workspace gets its own
`.gitconfig-tentaqles` next to its manifest, and `tq` maintains
`~/.gitconfig-tentaqles` as a list of `includeIf` blocks — one per workspace.
**Only trusted workspaces are wired into that include chain**: an untrusted or
newly-tampered workspace is left out entirely, so git never reads a config
file `tq` has not vouched for. `tq allow` and `tq deny` rewrite the include
list immediately.

`tq init` also sets global `user.useConfigOnly=true`. That is what makes the
setup fail closed: inside a trusted workspace git picks up that workspace's
name and email through the include, and anywhere else git refuses to guess an
identity, so a commit made outside any trusted workspace fails rather than
being attributed to the wrong person.

## Commands

| Command | Description |
|---|---|
| `tq version` | Print the tq version. |
| `tq init <base-folder>` | Register a base folder; each first-level subfolder becomes a terminal identity. |
| `tq add <name>` | Create a workspace folder with its manifest, identity dirs and git identity. Flags: `--base`, `--git-email` (required), `--git-name`, `--display-name`, `--color`, `--identities`, `--permission-mode`. |
| `tq allow <name>` | Trust a workspace's current manifest so it can export env. `--bypass` additionally allows `permission_mode: bypass`. |
| `tq deny <name>` | Revoke trust for a workspace. |
| `tq list` | List workspaces under all registered bases. `--json` for machine-readable output. |
| `tq env` | Print the env changes for the current directory (used by the shell hook). `--shell <name>`, `--json`. |
| `tq activate <shell>` | Print the hook to add to your shell profile, e.g. `eval "$(tq activate bash)"`. Shells: `bash`, `zsh`, `fish`, `pwsh`, `powershell`, `cmd`. |
| `tq login <workspace> <identity>` | Run a CLI's own login flow inside the workspace's private config home. |
| `tq run <workspace> -- <command> [args...]` | Run a command with a workspace's identity without cd-ing into it. |
| `tq doctor` | Verify hooks, trust, git and env against the manifests (never mutates). `--json` for machine-readable findings. |

## Where things live

Machine-wide state lives under `~/.tentaqles/` (override with `$TQ_HOME`):

- `~/.tentaqles/config.yaml` — the list of registered base folders.
- `~/.tentaqles/trust/<sha256>` — one empty marker file per trusted manifest,
  named after the manifest's SHA-256. A `.bypass` suffix marks a manifest
  additionally allowed to run Claude with `--dangerously-skip-permissions`.
- `~/.tentaqles/identities/<workspace>/<cli>/` — the private config home for
  one CLI in one workspace (`CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`,
  `AZURE_CONFIG_DIR`, …), so credentials that `tq login` causes a CLI to write
  never mix with another workspace's.
- `~/.tentaqles/audit.jsonl` — append-only log of identity switches.

Per-workspace state lives in the workspace folder itself:

- `<base>/<workspace>/.tentaqles.yaml` — the manifest: workspace name, git
  name/email, which CLI identities to isolate, Claude permission mode. Names
  only, never secrets.
- `<base>/<workspace>/.gitconfig-tentaqles` — generated by `tq add`; the
  `[user]` block git uses inside this workspace.
- `~/.gitconfig-tentaqles` — generated; the `includeIf` blocks pointing git at
  each **trusted** workspace's file. Both generated files are rewritten by
  `tq`; do not hand-edit them (`tq doctor` flags tampering).

Editing a manifest changes its hash and therefore revokes trust — `tq allow`
must be run again before that workspace exports anything.

## Limitations

- `cmd.exe` has no hook into directory changes, so the automatic `cd`-based
  identity switch does not work there. Use `tq run <workspace> -- <command>`
  to run a single command under a workspace's identity, or invoke the
  generated hook file directly from a `cmd.exe` script.
- `tq` isolates CLIs by pointing them at a private config directory. A CLI
  that keeps its credentials in the OS keychain instead of on disk is not
  isolated by that mechanism.

## Security

`tq` never stores secrets itself. Manifests hold only workspace names, git
identity metadata, and hashes used to detect tampering — no tokens, no
passwords, no API keys. Anything a CLI writes during `tq login` (a gh token,
a cloud credential file, etc.) is written by that CLI into the workspace's
own private config home, isolated from other workspaces, exactly as it would
be written to your real home directory if you weren't using `tq`.
