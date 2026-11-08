# Troubleshooting

For when something does not behave as expected. Start with `tentaqles doctor` in the folder where it goes wrong: it
checks the shell hook, git, every client's files, Claude settings and plugin, and prints a fix line for each finding.
`tentaqles doctor --json` prints the same findings as JSON; it exits 1 when one is an error. The second half of this
page lists every finding by its code.

## Symptoms

### The identity does not switch in a new terminal

- Run `tentaqles hooks status`: each shell's profile should read `installed`. If not,
  `tentaqles hooks install --all-detected`, then open a new terminal.
- `TQ_ENABLED=0` in the environment turns the hook off.
- The folder must be under a registered clients folder and hold a marker that names it:
  `tentaqles list` shows the clients and their folders. A subfolder of a client folder is that client's; the clients
  folder itself is neutral.
- In cmd.exe there is no prompt hook: run the block `tentaqles activate cmd` prints after each `cd`, or use
  `tentaqles run . -- <command>`.

### claude refuses to start: "no session starts without the tentaqles guard"

`tentaqles run` (and the `claude` the shell hook defines) refuses a Claude session in a client whose Claude config
has no enabled tentaqles plugin, or where `disableAllHooks` is set. The message names the reason and prints the lines
that fix it, usually:

```sh
tentaqles run acme -- claude plugin marketplace add tentaqles/tentaqles
tentaqles run acme -- claude plugin install tentaqles@tentaqles
```

See [`guard-hook-missing`](#guard-hook-missing).

### A command is blocked by the hook

The hook prints `BLOCKED [identity/<rule>]: <reason>` and the fix. The rules are in the
[Claude Code hook reference](reference/claude-hook.md). The most common:

- `env-drift` or `claude-config-drift`: the shell was opened in another folder and never updated. Open a new terminal
  in the client's folder.
- `wrong-cloud`: a cloud CLI the client does not use. Add the cloud (`tentaqles add cloud aws`) if the client does
  use it.
- `neutral-remote`: a remote git, gh or cloud command outside every client folder. `cd` into the client's folder.
- `BLOCKED [tq/guard-error]: the client manifest of this folder cannot be read`: the manifest has an error, so the
  hook denies every shell command there. `tentaqles doctor` names the error; fix the manifest.

### "command not found (outside the scope of identity …)"

`aws: command not found (outside the scope of identity acme)` is the client's PATH mask: acme does not use that
provider. If it should, add it (`tentaqles add cloud aws`, `tentaqles add <provider>`), then open a new terminal.

### The prompt says "tq: PATH mask missing"

The client's PATH needs its mask folder (a PATH folder shared with other tools holds an out-of-scope CLI) and the
folder is missing or was changed. Run `tentaqles settings render <client>`. Until then, out-of-scope CLIs still
resolve in that terminal.

### The prompt says "tq: shell state lost"

The shell's `__TQ_STATE` was unset or edited while a client identity was active. tentaqles removed its own variables
and mask folders and cannot put back the values from before it. Open a new terminal.

### The prompt says "tq: PATH over cmd.exe's 8191-character line"

cmd.exe cannot take the client's PATH, so it keeps its own and out-of-scope CLIs still resolve there; the rest of the
identity applies. Shorten PATH, or use PowerShell.

### git commits with the wrong email

- `git config user.email` in the repository shows what git uses. Under a client folder it should be the client's.
- `tentaqles doctor` reports a missing or edited `.gitconfig-tentaqles`, a missing include in `~/.gitconfig`, and a
  global `user.email` that would apply where no client rule does.
- A repository's own `.git/config` with a `user.email` overrides the client's: remove it.

### Claude writes in the wrong language

- `tentaqles list` shows each client's language. Change it with `tentaqles add language <tag or name>`.
- Restart the Claude session: the language instruction is added when a session starts.
- A `language` set by hand in the client's Claude `settings.json` before tentaqles wrote one stays as you set it.
  Delete it and run `tentaqles settings render <client>`.

### A link opens in the wrong browser profile

Only links opened through `BROWSER`, `xdg-open`, `open` or `tentaqles open` use the client's profile. The client
needs a browser in its manifest (`tentaqles add browser chrome "Profile 2"`), and the profile name is the profile's
folder name, not the name the browser shows. A link clicked in the terminal window is opened by the operating system,
in its default browser.

## Findings of `tentaqles doctor`

Errors make `tentaqles doctor` exit 1; warnings and notes do not.

### `no-bases`

Error. No clients folder is registered yet. Fix: `tentaqles reach <client folder>`.

### `manifest-invalid`

Error. A client's manifest does not load; the message names the file and the problem. Until it is fixed that client
gets no identity and the hook denies every shell command in its folder. Fix the manifest under
`~/.tentaqles/identities` (the keys are in the [files reference](reference/manifest.md)).

### `provider-file-invalid`

Error, or warning for a minor problem. A provider file of your own under `~/.tentaqles/providers` does not load. Fix
the file, then run `tentaqles providers list`. See [providers](reference/providers.md).

### `git-missing`

Error. git is not on PATH, so no client's git identity can apply. Install git, then run `tentaqles doctor` again.

### `git-include-missing`

Error. `~/.gitconfig` does not include `~/.gitconfig-tentaqles`, so no client folder's git identity applies. Fix:
`git config --global --add include.path ~/.gitconfig-tentaqles` (doctor prints the exact path).

### `git-useconfigonly`

Error. The global `user.useConfigOnly` is not `true`: outside client folders git guesses an identity from the machine.
Fix: `git config --global user.useConfigOnly true`.

### `global-email-set`

Error. A global `user.email` is set: commits outside every client folder use it silently. Fix: remove it from
`~/.gitconfig`.

### `includeif-unmanaged`

Warning. `~/.gitconfig` has a hand-written `includeIf` for a client folder that tentaqles's include file already
covers. Fix: remove it from `~/.gitconfig`.

### `include-orphan`

Warning. The global git config includes a file that does not exist. Fix: remove that `include.path` from
`~/.gitconfig`.

### `git-ws-file-missing`

Error. A client folder has no `.gitconfig-tentaqles`. Fix: `tentaqles reach <client folder>` rewrites it.

### `git-ws-file-tampered`

Error. A client folder's `.gitconfig-tentaqles` no longer looks like the file tentaqles writes. Fix: delete it, then
`tentaqles reach <client folder>` rewrites it.

### `git-email-drift`

Error. In the current folder git's `user.email` is not the manifest's. Usually a repository's own config overrides it.
Fix: remove the override; `tentaqles reach <client folder>` restores `.gitconfig-tentaqles`.

### `identity-dir-missing`

Warning. A provider in the client's identities has no folder under `~/.tentaqles/identities/<client>/`. Fix: create the folder
doctor names, then log in: `tentaqles login <client> <cli>`.

### `claude-not-logged-in`

Warning. The client's Claude config home has no credentials (not checked on macOS, where Claude Code keeps them in
the keychain). Fix: `tentaqles login <client> claude`, or start `claude` in the client's folder and log in.

### `cli-missing`

Warning. A CLI the client uses is not on PATH. Fix: the install hint doctor prints, or take the provider out of the
client (`tentaqles remove <provider>`).

### `cli-shadowed`

Warning. A CLI on PATH resolves to a script that sets the identity itself or recomputes it from the current folder,
overriding what tentaqles exports, so `tentaqles run` and `tentaqles login` can act on the wrong client. Fix: put the
real CLI ahead of that folder on PATH, or remove the folder.

### `env-drift`

Error. The shell's `TQ_WS` names another client than the current folder's, or the folder is neutral and `TQ_WS` is
still set: the shell hook did not run here. Fix: open a new terminal, or run
`eval "$(tentaqles env --shell <shell>)"`.

### `hook-missing`

Warning. The current folder is under a clients folder but the shell carries no tentaqles state: the shell hook is
probably not installed. Fix: `tentaqles hooks install --all-detected`, then open a new terminal.

### `claude-config-drift`

Error. `CLAUDE_CONFIG_DIR` is not the client's (or is unset while `TQ_WS` names the client). Fix: open a new terminal
in the folder, or run `eval "$(tentaqles env --shell <shell>)"`.

### `guard-hook-missing`

Error. A Claude session started in this client would run without the tentaqles plugin's hook, so `tentaqles run`
refuses to start one. The message gives the reason and the fix:

- the plugin is not installed: `tentaqles run <client> -- claude plugin marketplace add tentaqles/tentaqles`, then
  `tentaqles run <client> -- claude plugin install tentaqles@tentaqles`;
- it is installed but disabled: `tentaqles run <client> -- claude plugin enable tentaqles@tentaqles`;
- `disableAllHooks` is set, or the plugin is set off under `enabledPlugins`, in the client's `settings.json` or the
  folder's `.claude/settings.json`: remove the setting;
- a settings file cannot be read as JSON: repair it;
- the installed plugin's hook is not the tentaqles hook: uninstall it, then install it again;
- the client has no `claude` identity: add it to the manifest's identities, then `tentaqles settings render <client>`.

### `settings-drift`

Warning. The client's Claude `settings.json` differs from what `tentaqles settings render` writes (its `env`,
`language` or status line). Fix: `tentaqles settings render <client>`.

### `unpinned-package`

Warning. The client's `settings.json` or `.claude.json` runs an `npx`, `bunx`, `pnpm dlx` or `uvx` package without a
fixed version (`@latest`, or no version at all), which resolves anew on every run. Fix: pin an exact version, such as `pkg@1.2.3`.

### `plugin-version-skew`

Warning. A Claude plugin installed in several clients is at different versions. Fix: update it in each client,
`tentaqles run <client> -- claude plugin update <plugin>`.

### `plugins-dir-linked`

Warning. A client's Claude `plugins` folder is a link to a folder another Claude config also uses, so a plugin
install from either rewrites the same files. Fix: give the client its own `plugins` folder (replace the link with a
real folder).

### `plugin-foreign-path`

Warning. Plugins in a client's Claude config are installed from another config's folder. Fix: reinstall each inside
the client: `tentaqles run <client> -- claude plugin uninstall <plugin>`, then
`tentaqles run <client> -- claude plugin install <plugin>`.

### `claude-alias-shadow`

Warning, or a note. A shell profile defines a `claude` alias or function that replaces the `claude` the shell hook
defines (which starts Claude Code through `tentaqles run`). In PowerShell any alias wins over the function. Fix:
delete the alias or function; doctor names the profile and the lines. An alias that only adds flags to `claude` is a
note.

### `browser-profile-missing`

Warning. The client's browser profile has no folder in the browser's data folder. Fix: correct the profile in the
manifest's `browsers:` (it is the profile's folder name, such as `Profile 2`), or run `tentaqles open <url>` in the
client's folder to make the browser create it.

### `browser-shims-missing`

Warning. The client lists a browser but its shims are not written. Fix: `tentaqles settings render <client>`.
