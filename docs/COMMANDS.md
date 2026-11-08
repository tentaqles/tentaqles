# Commands

Every command is `tentaqles <command>`. `tq <command>` is its alias: the same binary under a shorter name, so
`tq run acme -- claude` and `tentaqles run acme -- claude` are the same command. A *client* is a folder under one
of your clients folders that `tentaqles reach` set up: it holds a marker file, `.tentaqles.yaml`, and its manifest
lives under `~/.tentaqles/identities/<client>/manifest.yaml` (`$TQ_HOME` moves `~/.tentaqles`).

## Setting up a client

### `tentaqles reach [<path>]`

Makes the current folder, or `<path>`, a client. The folder's name is the client's name (lowercase letters,
digits, `.`, `_`, `-`), and its parent folder is registered as a clients folder.

For a new client it prints the banner (`--no-banner` or `TQ_NO_BANNER=1` for none) and asks for the client's email,
git provider, clouds, team, data and task tools, email provider, and browsers with a profile each. A name typed
under *Other* is recorded in the manifest's `other_providers`, and reach says that tentaqles can't isolate it. It
then prints what it is about to do and waits for a typed `yes`. Nothing is written before that. Then it:

- writes the manifest and the marker, and a folder under `~/.tentaqles/identities/<client>/` for each identity
  (Claude Code, and each provider with its own config home);
- writes the folder's `.gitconfig-tentaqles` with the client's name and email, and the `includeIf` lines in
  `~/.gitconfig-tentaqles` that apply it to every repository under the folder;
- renders the client's Claude settings, PATH mask and browser shims (as `tentaqles settings render`);
- installs the tentaqles plugin into the client's Claude config, with `claude plugin marketplace add
  tentaqles/tentaqles` and `claude plugin install tentaqles@tentaqles`, two minutes each at most. A failed install
  doesn't fail reach: it prints the reason and the lines to run. `--no-plugin` skips it and prints the same lines;
- adds tq's activation block to the profile of each shell it detects, when the block isn't there yet.

In a folder that already is a client, reach shows the client and what it can use, then offers a menu: nothing,
the providers, the browser and profile, or the email. The chosen part of the form opens with the current answers.
reach lists the changes and waits for `yes`, then rewrites only those keys of the manifest (other keys and comments
stay), makes the folders of new identities, rewrites the folder's git email when it changed, and renders the
client's files again. A provider taken out keeps its identity folder, with any login in it, and reach names it.

reach refuses to run from inside a Claude Code session, and without a terminal on stdin and stdout: it is the
developer's command.

### `tentaqles login <client> <cli>`

Runs the CLI's own login with the client's config home, so the credentials land in
`~/.tentaqles/identities/<client>/<cli>`. For `aws`, `--sso` runs `aws configure sso` and `--sso-session NAME`
runs `aws sso login --sso-session NAME`. Logging in from a client's terminal does the same.

## Working in a client's folder

### The shell hook: `tentaqles hooks install|remove|status`, `tentaqles activate <shell>`, `tentaqles env`

`tentaqles hooks install [shell...]` (or `--all-detected`) adds the activation block to your shell profiles; `remove`
takes it out; `status` shows each shell's profile and state. Shells: bash, zsh, fish, PowerShell, pwsh, and cmd
(through `tentaqles activate cmd`). `tentaqles activate <shell>` prints the block to add by hand.

At every prompt the hook runs `tentaqles env`, which prints the changes for the current folder:

- in a client's folder, or any folder under it: `TQ_WS` and `TQ_WS_ROOT`; `CLAUDE_CONFIG_DIR`; each provider's
  config home variable (`GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`, `AWS_CONFIG_FILE` and `AWS_SHARED_CREDENTIALS_FILE`,
  `CLOUDSDK_CONFIG`, `GLAB_CONFIG_DIR`, `DATABRICKS_CONFIG_FILE` and others, see `tentaqles providers list`); `BROWSER`
  when the client lists a browser; and a `PATH` without the folders that hold only CLIs of providers outside the
  client's scope (a folder shared with other tools gets a mask instead, see below). The variables of those
  providers (`AWS_PROFILE`, `AZURE_*` and the like) are unset.
- leaving the folder puts back exactly what was there before.

`TQ_ENABLED=0` turns the hook off in a shell. `tentaqles env --json` prints the same changes as JSON.

### `tentaqles run <client>|. -- <command> [args...]`

Runs a command with a client's environment without changing folder; `.` is the current folder's client (none in a
neutral folder). The command is looked up on the client's PATH, so a CLI outside the client's scope isn't found,
however it is spelled.

For `claude`, tq adds `--dangerously-skip-permissions` unless the command line sets a permission mode, runs a
`claude` subcommand, or asks for `--help` or `--version`. It refuses to start a session when the client's Claude
config has no enabled tentaqles plugin, or when `disableAllHooks` is set there or in the folder's
`.claude/settings.json`; the refusal prints the lines that fix it.

From inside a Claude Code session, `tentaqles run` runs only the client of the current folder: another client's name is
refused, and a nested `claude` gets no bypass flag.

### `tentaqles open [--browser chrome|edge] <url>`

Opens an `http` or `https` URL in the client's browser profile: the first browser of the manifest's `browsers`
list, or the one `--browser` names, started from its install path with `--profile-directory`. It refuses any other
scheme, a value starting with `-`, a neutral folder, a client without browsers, and a browser that isn't installed.
In a client's terminal, `BROWSER`, `xdg-open` and `open` go the same way: they are shims under
`~/.tentaqles/shims/<client>/`.

## Keeping a client's files in line

### `tentaqles settings render [client...] | --all`

Brings each client's files in line with its manifest:

- the client's Claude `settings.json`: the manifest's `claude.env`, and the tq status line (which replaces only an
  unpinned `npx` status line; `--no-statusline` leaves it alone). Only what tq wrote earlier is removed;
- the PATH mask, `~/.tentaqles/identities/<client>/path-mask/`: one stub per CLI of a provider outside the client's
  scope, for the PATH folders that hold it beside other tools. A stub prints `<name>: command not found` and exits
  127;
- the browser shims, when the client lists a browser.

`--dry-run` writes nothing. Run it after editing a manifest by hand, and after upgrading tq (the shims and stubs are
copies of the binary).

### `tentaqles doctor [--json]`

Reports, without changing anything: the shell hook (installed, and in effect in this shell), git (the include file,
`user.useConfigOnly`, a global `user.email` that would leak into neutral folders, each client's
`.gitconfig-tentaqles`), each client's identity folders and Claude login, the CLIs it needs (missing, or shadowed by
a script earlier on PATH), its browser profiles and shims, its Claude settings, its plugin, and a shell function
that replaces tq's `claude` launcher. Exits 1 when a finding is an error.

### `tentaqles list [--json]`

Lists the clients: name, git email, identities and folder.

### `tentaqles providers list|show <id>|check [id...]|add <id>`

The provider catalog: the CLIs tentaqles knows, the variable that moves each one's config home, its login command,
and what a client's terminal hides when the provider is outside its scope. `add` writes a provider file of your own
under `~/.tentaqles/providers/`; see [PROVIDERS.md](PROVIDERS.md).

### `tentaqles version`

The version and the commit the binary was built from.

## Used by the Claude Code plugin

`tentaqles claude-hook pre-tool-use` and `tentaqles claude-hook session-start` are what the plugin's hook runs; see
[CLAUDE-HOOK.md](CLAUDE-HOOK.md). `tentaqles statusline` is the status line the client's Claude settings run: the client,
the git email, the model, the context size and the cost.
