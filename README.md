# tentaqles

Terminal-identity orchestration for developers working across several clients: one base folder, one identity per subfolder, the right CLIs/agents/plugins per identity. This monorepo holds the `tq` CLI (`cmd/tq`, `internal/`), the Claude Code plugin (`plugin/`), and the provider catalog and desktop setup app (planned: `providers/`, `desktop/`).

## Claude Code plugin

`plugin/` wires `tq` into Claude Code: its `SessionStart` and `PreToolUse`
hooks shell out to `tq claude-hook` for identity reporting and enforcement.
See [docs/CLAUDE-HOOK.md](docs/CLAUDE-HOOK.md) for the hook protocol, rule
precedence, and how to test it by hand.

---

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

The desktop setup app is unsigned for the same reason: right-click it and
choose *Open* rather than double-clicking it.

From source (requires Go 1.26+):

```sh
go install github.com/tentaqles/tentaqles/cmd/tq@latest
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

## Setup

For onboarding more than one company at once (or scripting a fresh machine),
`tq setup` scaffolds several workspaces — plus shell hooks — from a single
declarative plan, instead of running `tq add` and `tq activate` by hand for
each one.

Run it with no flags for the interactive wizard, which walks through eight
steps: welcome/consent, base folder (offering to create it), a repeatable
company loop (name, display name, color, git name/email/user, providers,
permission mode), a tool-check summary of what's installed vs. missing, shell
hook selection, a full preview of every change, and a final confirmation
before anything is applied. The wizard refuses to finish with zero companies
— it re-prompts, and if you decline twice it aborts with an error rather than
applying an empty plan.

For non-interactive or repeatable setups, drive it from a YAML plan instead:

```sh
tq setup --example > setup.yaml       # write a two-company sample plan
tq setup --from setup.yaml --dry-run  # preview + tool-check, write nothing
tq setup --from setup.yaml --yes      # apply without an interactive prompt
```

Both the wizard and `--from` runs accept `--json` for machine-readable
output (preview, tool-check results, and — once applied — the report), and
`--write-plan <path>` to also save the wizard's answers as a plan file you
can replay later (the wizard always additionally saves its answers to
`~/.tentaqles/last-setup.yaml`). Without `--yes`, `tq setup --from` on a
non-interactive stdin refuses to apply rather than silently doing nothing.

Setup plan files carry git identity metadata, so `tq setup`/the wizard write
them with owner-only permissions (`0600`, containing directory `0700`) on
platforms that enforce POSIX permission bits.

Shell hooks — the `eval "$(tq activate <shell>)"` (or equivalent) line that
makes `cd` switch identities automatically — can also be managed on their
own:

```sh
tq hooks status              # installed / present (unmanaged) / missing / no profile, per shell
tq hooks install <shell...>  # or: --all-detected
tq hooks remove <shell...>   # or: --all-detected
```

`tq init --install-hook` runs the same install step right after registering
a base folder. Either way, if you already have your own activation line in a
profile file, `tq` detects it as "present (unmanaged)" and leaves it alone —
hand-installed hook lines are never duplicated.

The installed hook line respects a kill switch: setting `TQ_ENABLED=0` in
your environment makes the hook skip calling `tq activate` entirely for that
shell session, without needing to uninstall anything.

See [`docs/SETUP.md`](docs/SETUP.md) for the full plan YAML schema and field
reference.

Prefer a GUI? The `tentaqles-setup` desktop app wraps the same setup flow in
a wizard — see [`docs/DESKTOP.md`](docs/DESKTOP.md) for install (incl.
unsigned-app steps) and the dev loop.

## Commands

| Command | Description |
|---|---|
| `tq version` | Print the tq version. |
| `tq init <base-folder>` | Register a base folder; each first-level subfolder becomes a terminal identity. `--install-hook` also installs the shell activation hook into detected shell profiles. |
| `tq setup` | Interactive wizard that scaffolds one or more companies (workspaces) plus shell hooks from a single flow. `--example` prints a sample plan YAML; `--from <file>` loads a plan instead of prompting (`--dry-run` to preview only, `--yes` to apply without confirming); `--write-plan <path>` also saves the wizard's answers as a plan file; `--json` for machine-readable output. See [`docs/SETUP.md`](docs/SETUP.md). |
| `tq hooks status` | Show install status (`installed`, `present (unmanaged)`, `missing`, `no profile`) for each known shell. |
| `tq hooks install <shell...>` | Install tq's activation block into shell profile(s). `--all-detected` targets every detected shell. |
| `tq hooks remove <shell...>` | Remove tq's activation block from shell profile(s). `--all-detected` targets every detected shell. |
| `tq add <name>` | Create a workspace folder with its manifest, identity dirs and git identity. Flags: `--base`, `--git-email` (required), `--git-name`, `--display-name`, `--color`, `--identities`, `--permission-mode`. |
| `tq allow <name>` | Trust a workspace's current manifest so it can export env. `--bypass` additionally allows `permission_mode: bypass`. |
| `tq deny <name>` | Revoke trust for a workspace. |
| `tq list` | List workspaces under all registered bases. `--json` for machine-readable output. |
| `tq env` | Print the env changes for the current directory (used by the shell hook). `--shell <name>`, `--json`. |
| `tq activate <shell>` | Print the hook to add to your shell profile, e.g. `eval "$(tq activate bash)"`. Shells: `bash`, `zsh`, `fish`, `pwsh`, `powershell`, `cmd`. |
| `tq login <workspace> <identity>` | Run a CLI's own login flow inside the workspace's private config home. |
| `tq run <workspace> -- <command> [args...]` | Run a command with a workspace's identity without cd-ing into it. |
| `tq doctor` | Verify hooks, trust, git and env against the manifests (never mutates). `--json` for machine-readable findings. `--verify auto\|all\|off` controls whether it asks each CLI which account is signed in -- see [Checking the account, not just the folder](#checking-the-account-not-just-the-folder). |
| `tq migrate` | Move a setup built by hand before `tq` under `tq`'s management: identity dirs, global git config, shell hooks, and (opt-in) the `cmd.exe` AutoRun hook. Dry run unless `--apply`; `--steps identity,git,shell,cmd`, `--force`, `--json`. Every mutation is journalled first. See [`docs/MIGRATE.md`](docs/MIGRATE.md). |
| `tq uninstall --restore [<ts>\|latest]` | Undo a migration by replaying its journal backwards. Lists what it would undo and does nothing until you add `--yes`. See [`docs/MIGRATE.md`](docs/MIGRATE.md). |
| `tq bundle sync <workspace>` | Materialize a workspace's `claude.bundle` into its Claude identity dir (settings, skills, MCP servers). Refuses untrusted workspaces. `--force` to sync while Claude appears to be running there, `--json` for machine-readable output. |
| `tq bundle diff <workspace>` | Show drift between a workspace's `claude.bundle` and what's actually on disk, without changing anything. Exits 1 if any drift is found. `--json`. |
| `tq bundle capture [workspace]` | Reconstruct a `claude.bundle` manifest fragment (printed to stdout) and catalog entries from an existing Claude identity dir. `--dir <path>` to capture from an arbitrary dir instead of a workspace's own. `--write-catalog` upserts the captured entries into the catalog (never overwrites an existing name). |
| `tq bundle catalog` | Print the catalog's path, entry counts, and any `Validate()` warnings. |
| `tq providers list` | List providers in the catalog. `--category <c>` to filter, `--json` for machine-readable output. |
| `tq providers show <id>` | Print a provider's full definition as YAML. |
| `tq providers check [<id>...]` | Probe whether provider CLIs are installed. `--all` (default with no ids), `--workspace <ws>` to check only that workspace's identities, `--json`. Exits 1 if any provider with a CLI is missing. |
| `tq providers add <id>` | Add or override a provider as a user file under `providers/`. Flags: `--name`, `--category` (required), `--command`, `--version-args`, `--env KEY=VALUE` (repeatable), `--login "args..."`, `--verify "args..."`, `--docs`, `--force` to overwrite an embedded or existing user provider. |

## Checking the account, not just the folder

Pointing a CLI at a private config directory routes the tool. It does not
prove the right account is inside it. A workspace can be wired up perfectly --
correct directory, correct variables, `tq doctor` otherwise clean -- and still
be signed in as the wrong client, because nothing was ever asked.

Declare what you expect and `tq doctor` will ask:

```yaml
git:
  provider: github
  expected_user: rndomingues      # the account `gh` must be signed in as
identities:
  claude:
    expected_account: dev@acme.test
    expected_subscription: max
```

```
[error] acme: gh is signed in as someone-else, but this workspace expects rndomingues
[error] acme: claude is on the team plan here, but this workspace expects max
[warn]  acme: gh is not signed in for this workspace
```

`expected_user` applies to the CLI for the workspace's `git.provider` (`gh` for
GitHub, `glab` for GitLab), so a workspace on Azure DevOps whose
`expected_user` is an email address is not compared against a GitHub username.

**What it costs.** These checks run the CLI's own status command, which takes
hundreds of milliseconds and sometimes a network call. So the default
(`--verify auto`) only runs one where it can answer a question you actually
asked: an expectation declared in the manifest, or a login state `tq` has no
cheaper way to observe -- Claude on macOS keeps credentials in the Keychain
rather than in a file, so asking the CLI is the only way to tell. Declare
nothing and you pay nothing. `--verify all` checks every identity that has a
verify command; `--verify off` skips them entirely.

The identity checks never run on `tq`'s hot paths -- the per-prompt env diff
and the plugin's pre-tool-use hook -- because a subprocess there would be felt
on every command.

## Providers

`tq` resolves each identity (`gh`, `aws`, `claude`, ...) to a CLI, its
install hints, and the env vars that point it at a private per-workspace
config directory, via a provider catalog: built-in YAML embedded in the
binary, plus user overrides/additions under `~/.tentaqles/providers/`
(`$TQ_HOME/providers/` if set). Use `tq providers list` to see what's
registered, `tq providers check` (wired into `tq doctor`'s `cli-missing`
hints too) to see what's actually installed, and `tq providers add` to add a
custom provider or override a built-in one. See
[`docs/PROVIDERS.md`](docs/PROVIDERS.md) for the full schema.

## Bundles

Two or more workspaces often want the same Claude plugins, skills, and MCP
servers. Instead of repeating that config in every manifest, `tq` keeps a
single shared **catalog** of named marketplaces/skills/MCP servers, and each
workspace's manifest just lists which catalog names it wants under
`claude.bundle`. `tq bundle sync` then materializes that into the workspace's
private Claude identity dir.

Catalog (`~/.tentaqles/bundles/catalog.yaml`):

```yaml
marketplaces:
  acme-internal:
    source: github
    repo: acme/claude-plugins
skills:
  snowflake-patterns:
    path: /repos/shared/skills/snowflake-patterns
mcp:
  github:
    command: gh-mcp
```

Manifest (`<workspace>/.tentaqles.yaml`):

```yaml
claude:
  bundle:
    marketplaces: [acme-internal]
    plugins: [some-plugin@acme-internal]
    skills: [snowflake-patterns]
    mcp: [github]
```

Two things to know about `sync`:

- `enabledPlugins` in the identity dir's `settings.json` is replaced wholesale
  from the bundle, so any entry there set to `false` is dropped rather than
  preserved. List a plugin under `claude.bundle.plugins` if you want it kept.
- If the identity dir's `skills/` is a symlink (or a Windows junction/reparse
  point), or otherwise resolves outside the identity dir, `sync` refuses to
  touch it rather than writing through the link into a shared skills
  directory.

Commands: `tq bundle sync <workspace>` to apply, `tq bundle diff <workspace>`
to see what's out of sync, `tq bundle capture <workspace> --write-catalog`
to reverse-engineer a bundle from a Claude config dir that already has
plugins/skills/MCP servers configured by hand, and `tq bundle catalog` to
inspect the catalog itself.

Editing `claude.bundle` in a manifest changes its hash, so re-run
`tq allow <workspace>` after editing a manifest before syncing. Also close
any Claude sessions using that workspace's identity dir before running
`tq bundle sync` (it refuses to sync into a config dir Claude appears to be
using) — or pass `--force` if you're sure it's safe.

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
- `~/.tentaqles/backups/<ts>/` — one `tq migrate --apply` journal: every
  mutation it made, plus byte-exact copies of every file it overwrote.
  `tq uninstall --restore <ts>` replays it backwards — see
  [`docs/MIGRATE.md`](docs/MIGRATE.md).

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
  that keeps its credentials in the OS keychain is only isolated if it
  namespaces that keychain entry per config directory. Measured on macOS
  26.6: Claude Code does — two accounts on different subscription tiers were
  logged in simultaneously under two `CLAUDE_CONFIG_DIR` values, and an empty
  config directory reports logged-out even though a `Claude Code-credentials`
  keychain item exists. `tq` therefore never has to store a token itself. A
  CLI that does *not* namespace would need its own mechanism; none of the
  ones in the catalog are currently known to behave that way.

## Security

`tq` never stores secrets itself. Manifests hold only workspace names, git
identity metadata, and hashes used to detect tampering — no tokens, no
passwords, no API keys. Anything a CLI writes during `tq login` (a gh token,
a cloud credential file, etc.) is written by that CLI into the workspace's
own private config home, isolated from other workspaces, exactly as it would
be written to your real home directory if you weren't using `tq`.
