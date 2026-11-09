# Changelog

Notable changes to the `tentaqles` CLI and the docs of this repository. The Claude Code plugin keeps its own log in
[`plugin/CHANGELOG.md`](plugin/CHANGELOG.md).

## [Unreleased]

## [0.14.1] - 2026-11-09

- The Windows installer no longer breaks when a tentaqles process is running, such as the launcher of an open Claude
  session. It unpacks to a staging folder, moves each existing file aside, and puts them all back if a step fails, so
  an upgrade can't leave you without a working `tq`. A copy still in use is removed by the next install.

## [0.14.0] - 2026-11-09

- A child shell started from an activated client folder (an agent's `powershell -Command`, a `bash -lc`) no longer
  prints `tq: shell state lost` on every command when it is in the same folder and already has that client's
  identity: nothing needs restoring there, so the notice is silent. The notice still shows when the shell is elsewhere.
- **`tentaqles codex-hook`** is the OpenAI Codex CLI counterpart of `claude-hook`: `pre-tool-use` runs the same identity
  check and answers Codex's deny object (stdout, exit 0), and every error path denies the same way; `session-start`
  prints the same preamble. The two share one decision path; `claude-hook` is unchanged.
- A codex session in a client folder starts only with the tentaqles hooks enabled and trusted in the client's Codex
  config, and refuses `--disable hooks` and `-c features.hooks=false`. `tentaqles settings render` writes and trusts those
  hooks (and the client's language) when codex is in the client's scope; `tentaqles doctor` reports a client whose
  hooks are missing, off, untrusted or unreadable, and `codex` in a client folder runs through `tentaqles run` in every
  shell.
- **An AI coding agent besides Claude Code** is a category of `tentaqles add`, `remove` and the reach form: `add codex`
  (or `add agent codex`) puts a catalog agent in the client's scope, and gives codex its own Codex home.

## [0.13.2] - 2026-11-08

- **Docs rebuilt.** The README says what tentaqles is for, shows it working, and gets you to a first client in three
  steps. `docs/` has an index, a getting-started tutorial, a how-it-works page, troubleshooting for every doctor
  finding, and references for commands, the manifest, providers and the Claude Code hook, all checked against the
  code. Tests fail when a command has no section in the reference, a docs link is broken, or a page is missing from
  the index.
- The hook compares `gh` with `git.expected_user` only for a GitHub client: on Azure DevOps that key is an email,
  and every `gh` command was blocked.
- The session-start `Git:` line no longer prints an empty user.
- `tentaqles doctor` drops the Python-alias check (the plugin runs no Python), and `identity-dir-missing` names the
  folder to create.

## [0.13.1] - 2026-11-08

- A manifest holds only what tentaqles reads: `git.host` and the per-identity `share_capabilities`,
  `expected_account`, `expected_subscription` and `profile` are gone, and a new client's manifest has no empty keys.

## [0.13.0] - 2026-11-08

- **Each client has a language.** `tentaqles reach` asks for it after the email, from a list that starts on English
  (United States), then every language by name; typing filters the list (`por` leaves the Portuguese ones) and the
  arrows move through it. The manifest stores the tag (`language: pt-BR`). The client's Claude `settings.json` gets
  Claude Code's `language` setting, and each session starts with the instruction to write the client's reports,
  documents, emails and messages in that language. `tentaqles add language pt-BR` (or a name: `brasil`,
  `japanese`) changes it, reach's menu offers it, and `tentaqles list` shows it. A manifest without `language` is
  English (United States).

## [0.12.0] - 2026-11-08

Tentaqles is an identity isolator for developers who work for several clients: one folder per client, and a terminal
opened in it, or in any subfolder, carries that client's identity and nothing else.

- **`tentaqles reach [<path>]`** makes a folder a client: a banner, a form (email, git host, clouds, team, data and
  task tools, email provider, browsers and profiles), and after a typed `yes` the client's manifest under
  `~/.tentaqles/identities/<client>/`, a marker in the folder, the folder's git identity, the client's Claude
  settings, PATH mask and browser shims, the tentaqles plugin in the client's Claude config, and the activation
  block in your shell profiles. In a folder that already is a client, reach changes its providers, browser or email
  through the same questions.
- **`tentaqles add` and `tentaqles remove`** change one part of a client's setup without the menu:
  `tentaqles add git azure-devops`, `tentaqles add cloud aws`, `tentaqles remove gh`, `tentaqles add browser chrome
  "Profile 2"`. A value alone finds its category; with no value, the category's part of the form opens.
- **`tentaqles drop <client>`** undoes a client as if it never existed: its identity folder (manifest, Claude config,
  logins), its browser shims, the marker and git identity in its folder, its git rule, and the clients folder's
  registration when no other client is left there. Your files stay; an emptied folder is deleted.
- **A client's terminal**, in its folder and every subfolder, carries `CLAUDE_CONFIG_DIR` (its own Claude account),
  each CLI's config home, `BROWSER`, and a PATH without the CLIs of providers outside its scope, whose variables are
  unset. Leaving the folder restores what was there.
- **`tentaqles run`** runs a command with a client's environment, refuses a Claude session without the plugin, and
  from inside a session runs only the folder's client.
- **The plugin's hook** runs on Bash and PowerShell calls and blocks a git, gh or cloud command under the wrong
  identity: a manifest's blocked commands, the wrong cloud, a remote command outside every client folder, a stale
  shell, the wrong `CLAUDE_CONFIG_DIR`, git email or gh user.
- `tentaqles login`, `open`, `settings render`, `doctor`, `list`, `providers`, `hooks` and `version` complete the
  set; see [docs/COMMANDS.md](docs/COMMANDS.md).
- Every command is `tentaqles <command>`, with `tq <command>` as its alias: the release ships both binaries, the same
  bytes.
- Commands that change a client (`reach`, `add`, `remove`, `drop`) wait for a typed confirmation and refuse to run
  from inside Claude Code.
