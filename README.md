# tentaqles

Terminal-identity orchestration for developers working across several clients: one base folder, one identity per subfolder, the right CLIs/agents/plugins per identity. This monorepo holds the `tq` CLI (`cmd/tq`, `internal/`) and the Claude Code plugin (`plugin/`).

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

With the Claude Code plugin, `tq` guards two things and nothing else, in every
Claude permission mode: the **identity** a command runs under (a `git push`,
`gh` or cloud command under the wrong workspace, in an untrusted folder or
from a stale shell is blocked with the reason) and **secrets** (`.env` reads,
environment dumps, credential-store reads and a commit that would record a
secret are blocked; `tq dotenv run` is the way to use a `.env`). Every
decision is allow or block, never a confirmation (the one exception is the
fail-safe when the guard itself fails, which asks); anything else is Claude
Code's own permission settings' business. See
[docs/CLAUDE-HOOK.md](docs/CLAUDE-HOOK.md).

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

For repeatable setups, drive it from a YAML plan instead; `--from` skips the
prompts but still needs a terminal and a typed `yes`:

```sh
tq setup --example > setup.yaml       # write a two-company sample plan
tq setup --from setup.yaml --dry-run  # preview + tool-check, write nothing
tq setup --from setup.yaml            # apply: it says what it writes, you type yes
```

Both the wizard and `--from` runs accept `--json` for machine-readable
output (preview, tool-check results, and — once applied — the report), and
`--write-plan <path>` to also save the wizard's answers as a plan file you
can replay later (the wizard always additionally saves its answers to
`~/.tentaqles/last-setup.yaml`). `tq setup` is human-only: it refuses a
non-interactive stdin or stdout, a harness in the process tree or an agent
marker, and asks you to type `yes` before it writes. `--yes` is accepted for
older scripts but no longer skips that confirmation.

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
shell session, without needing to uninstall anything. That is all it does:
tq's Claude Code hooks and the plugin's skill scripts still run, so a
`claude` started there is still guarded. For one launch with no tq at all
use `claude-off` / `codex-off` (`tq run --off -- claude`, human-only): the
plugin is switched off for that process and the identity env is cleaned.

See [`docs/SETUP.md`](docs/SETUP.md) for the full plan YAML schema and field
reference.

## Commands

| Command | Description |
|---|---|
| `tq version` | Print the tq version and the commit it was built from. |
| `tq init <base-folder>` | Register a base folder; each first-level subfolder becomes a terminal identity. `--install-hook` also installs the shell activation hook into detected shell profiles. |
| `tq onboard [<name>]` | Set up one client workspace and record its company tools (identity, git provider, cloud, data platform, agent CLIs, chat, email) through a wizard; `--from <yaml>` answers from a file. Names only, never a token; the workspace is left untrusted and the `tq allow` and login lines are printed for you. See [`docs/SETUP.md`](docs/SETUP.md). |
| `tq setup` | Interactive wizard that scaffolds one or more companies (workspaces) plus shell hooks from a single flow. `--example` prints a sample plan YAML; `--from <file>` loads a plan instead of prompting (`--dry-run` to preview only; `--yes` is accepted for older scripts, the confirmation is still asked); `--write-plan <path>` also saves the wizard's answers as a plan file; `--json` for machine-readable output. See [`docs/SETUP.md`](docs/SETUP.md). |
| `tq hooks status` | Show install status (`installed`, `present (unmanaged)`, `missing`, `no profile`) for each known shell. |
| `tq hooks install <shell...>` | Install tq's activation block into shell profile(s). `--all-detected` targets every detected shell. Human-only. |
| `tq hooks remove <shell...>` | Remove tq's activation block from shell profile(s). `--all-detected` targets every detected shell. Human-only. |
| `tq hooks verify [<name>\|--all]` | Check a workspace's signed integrity record against the files it covers (manifest, marker, the enforcing keys of the Claude settings, the loaded plugin folder and its `installed_plugins.json` entry, Codex hook files, shims) and list what changed; exit 1 on a mismatch. `--accept` signs the current state again: human-only, after you confirm; `--accept --all` signs every workspace whose only change is a new plugin version that is the published release byte for byte, with one confirmation. |
| `tq add <name>` | Create a workspace folder with its manifest, identity dirs and git identity. Flags: `--base`, `--git-email` (required), `--git-name`, `--display-name`, `--color`, `--git-provider`, `--identities` (default `claude,gh`), `--permission-mode`. Human-only: a session is refused; you confirm, and the folder is trusted. |
| `tq allow <name>` | Trust a workspace as it is now: renders the identity's settings, then signs the integrity record over the manifest, the enforcing keys of the identity's Claude settings, the installed plugin hooks, the Codex hook files and the shims, so the workspace can export env. Human-only: it refuses an agent session and you confirm. An edit to any covered file drops the trust until a human signs again (`tq allow`, or `tq hooks verify --accept`). |
| `tq deny <name>` | Revoke trust for a workspace. Human-only. |
| `tq list` | List workspaces under all registered bases. `--json` for machine-readable output. |
| `tq env` | Print the env changes for the current directory (used by the shell hook). `--shell <name>`, `--json`. |
| `tq activate <shell>` | Print the hook to add to your shell profile, e.g. `eval "$(tq activate bash)"`. Shells: `bash`, `zsh`, `fish`, `pwsh`, `powershell`, `cmd`. |
| `tq login <workspace> <identity>` | Run a CLI's own login flow inside the workspace's private config home. For `aws`: one `[sso-session]` in the private config runs `aws sso login --sso-session NAME`; several need `--sso-session NAME`; `--sso` runs `aws configure sso`; otherwise `aws configure`. |
| `tq run <workspace>\|. -- <command> [args...]` | Run a command with a workspace's identity without cd-ing into it (`.` is the current folder). `--off` starts `claude` or `codex` with no tq at all: main install and logins, no identity, the plugin off (human-only). |
| `tq doctor` | Verify hooks, trust, git and env against the manifests (never mutates). `--json` for machine-readable findings. `--verify auto\|all\|off` controls whether it asks each CLI which account is signed in -- see [Checking the account, not just the folder](#checking-the-account-not-just-the-folder). |
| `tq migrate` | Move a setup built by hand before `tq` under `tq`'s management: identity dirs, global git config, shell hooks, and (opt-in) the `cmd.exe` AutoRun hook. Dry run unless `--apply`; `--steps identity,git,shell,cmd`, `--force`, `--json`. Every mutation is journalled first. Human-only with `--apply` (the dry run is free). See [`docs/MIGRATE.md`](docs/MIGRATE.md). |
| `tq uninstall --restore [<ts>\|latest]` | Undo a migration by replaying its journal backwards. Lists what it would undo and does nothing until you add `--yes`; with `--yes` you still type `yes` at the confirmation. Human-only. See [`docs/MIGRATE.md`](docs/MIGRATE.md). |
| `tq bundle sync <workspace>` | Materialize a workspace's `claude.bundle` into its Claude identity dir (settings, skills, MCP servers). Refuses untrusted workspaces. `--force` to sync while Claude appears to be running there, `--json` for machine-readable output. |
| `tq bundle diff <workspace>` | Show drift between a workspace's `claude.bundle` and what's actually on disk, without changing anything. Exits 1 if any drift is found. `--json`. |
| `tq bundle capture [workspace]` | Reconstruct a `claude.bundle` manifest fragment (printed to stdout) and catalog entries from an existing Claude identity dir. `--dir <path>` to capture from an arbitrary dir instead of a workspace's own. `--write-catalog` upserts the captured entries into the catalog (never overwrites an existing name). |
| `tq bundle catalog` | Print the catalog's path, entry counts, and any `Validate()` warnings. |
| `tq providers list` | List providers in the catalog. `--category <c>` to filter, `--json` for machine-readable output. |
| `tq providers show <id>` | Print a provider's full definition as YAML. |
| `tq providers check [<id>...]` | Probe whether provider CLIs are installed. `--all` (default with no ids), `--workspace <ws>` to check only that workspace's identities, `--json`. Exits 1 if any provider with a CLI is missing. |
| `tq providers add <id>` | Add or override a provider as a user file under `providers/`. Flags: `--name`, `--category` (required), `--command`, `--version-args`, `--env KEY=VALUE` (repeatable), `--login "args..."`, `--verify "args..."`, `--docs`, `--force` to overwrite an embedded or existing user provider. |
| `tq secret set\|rm\|status <workspace> <provider>` | Keep a git-host or issue-tracker token (`github`, `azure-devops`, `jira`, `linear`, `asana`) in the OS keychain; `set` reads it from a hidden prompt on your terminal, `status` shows present or absent and never a value. `set` and `rm` are human-only; `status` runs anywhere. See [`docs/PROVIDERS.md`](docs/PROVIDERS.md#tq-secret). |
| `tq secrets audit` | List agent-config files holding secret-shaped values, and `.env` files git would commit; path and line only, never the value. `--root <dir>` adds a directory, `--json`. |
| `tq dotenv run [--file F]... -- <command>` | Run a command with a `.env` loaded into its environment and every loaded value replaced by `[REDACTED:NAME]` in its output. See [docs/CLAUDE-HOOK.md](docs/CLAUDE-HOOK.md#using-a-env-without-printing-it). |
| `tq settings render [workspace...]` | Write the tq-owned part of each Claude identity's `settings.json` (baseline permissions, env, status line) and the Codex identity's `config.toml` and `hooks.json`. `--all`, `--dry-run`, `--no-statusline`. Human-only (`--dry-run` writes and asks nothing and is free from a session). `tq settings anchors` lists the terminals the human-only check accepts; `add <exe>` / `remove <exe>` (human-only) keep yours in `~/.tentaqles/anchors.yaml`. See [`docs/CLAUDE-SETTINGS.md`](docs/CLAUDE-SETTINGS.md). |
| `tq statusline` | The Claude Code status line: client, git email, model, context size, cost. |
| `tq worktrees list\|prune` | List linked git worktrees in every workspace repo; `prune` removes the clean, merged, old ones (`--older-than` days, `--yes` to remove instead of listing, then you confirm; human-only). |
| `tq claude-hook session-start\|pre-tool-use` | What the Claude Code plugin's hooks call: the identity preamble, and the allow-or-block decision for each tool call. See [docs/CLAUDE-HOOK.md](docs/CLAUDE-HOOK.md). |

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
  aws:
    expected_account: "123456789012"   # AWS account id (sts get-caller-identity)
```

```
[error] acme: gh is signed in as someone-else, but this workspace expects rndomingues
[error] acme: claude is on the team plan here, but this workspace expects max
[warn]  acme: gh is not signed in for this workspace
```

`expected_user` applies to the CLI for the workspace's `git.provider` (`gh` for
GitHub, `glab` for GitLab), so a workspace on Azure DevOps whose
`expected_user` is an email address is not compared against a GitHub username.

The AWS check runs `aws sts get-caller-identity` against the default profile
(or `AWS_PROFILE` if set), and `expected_account` is the 12-digit account id. A
setup from `aws configure sso` that only created a named profile reads as
logged out until `AWS_PROFILE` points at it.

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

Manifest (`~/.tentaqles/identities/<workspace>/manifest.yaml`; for an identity not yet
migrated with `tq migrate manifests`, `<workspace>/.tentaqles.yaml`):

```yaml
claude:
  bundle:
    marketplaces: [acme-internal]
    plugins: [some-plugin@acme-internal]
    skills: [snowflake-patterns]
    mcp: [github]
```

A marketplace entry takes Claude Code's own source fields: `source` (`github`, `git`, `url`,
`directory`), `repo`, `url`, `ref` (branch or tag), `path` (the folder for `directory`, the
marketplace.json path in the repo for `github`/`git`), `autoUpdate: true`, and `insecure: true` (tq-only; allows a plain-text mirror, never written to
settings). A bundle item
`key=catalogName` writes catalog entry `catalogName` under settings key `key`, so one
workspace can load a plugin from a local checkout under the same plugin id. See
[docs/RELEASING.md](docs/RELEASING.md#how-identities-pick-it-up).

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
- `~/.tentaqles/trust/<workspace>.json` — the signed integrity record of one
  workspace: the manifest's SHA-256 and the hash of every covered file
  (marker, the enforcing keys of the Claude settings, the loaded plugin folder
  but its skills, agents, templates, tests and caches, Codex hook files, shims),
  under an HMAC-SHA256 signature
  whose key sits in the OS keychain (service `tentaqles`, account
  `integrity-key`; the fallback `~/.tentaqles/integrity.key` is used only when
  the keychain is unavailable and `tq doctor` warns about it). `tq deny` turns
  it into a signed tombstone; tq never deletes it. The markers of older
  versions (`trust/<sha256>`, `trust/<workspace>/<sha256>`) count only while
  the signed snapshot `trust/legacy.json` lists them, until `tq allow` or
  `tq migrate manifests` replaces them.
- `~/.tentaqles/identities/<workspace>/<cli>/` — the private config home for
  one CLI in one workspace (`CLAUDE_CONFIG_DIR`, `GH_CONFIG_DIR`,
  `AZURE_CONFIG_DIR`, …), so credentials that `tq login` causes a CLI to write
  never mix with another workspace's.
- `~/.tentaqles/audit.jsonl` — append-only log of identity switches.
- `~/.tentaqles/backups/<ts>/` — one `tq migrate --apply` journal: every
  mutation it made, plus byte-exact copies of every file it overwrote.
  `tq uninstall --restore <ts>` replays it backwards — see
  [`docs/MIGRATE.md`](docs/MIGRATE.md).

Per-workspace state:

- `~/.tentaqles/identities/<workspace>/manifest.yaml` — the manifest: workspace
  name, git name/email, which CLI identities to isolate, Claude permission
  mode. Names only, never secrets. `<base>/<workspace>/.tentaqles.yaml` is a
  name-only marker pointing at it; an identity created before this layout keeps
  the full manifest in the folder until you run `tq migrate manifests`
  (`docs/adr/0002-protected-manifest-location.md`).
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
