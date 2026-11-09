# Command reference

For anyone looking up a command: what it does, its arguments and flags, and examples. New here? Start with
[Getting started](../getting-started.md). For the ideas behind the commands, read [How it works](../how-it-works.md).

Every command is `tentaqles <command>`. `tq <command>` is its alias: the same binary under a shorter name, so
`tq run acme -- claude` and `tentaqles run acme -- claude` are the same command. `tq --help` prints its usage as
`tentaqles`.

A *client* is a folder that `tentaqles reach` set up. The folder holds a marker file, `.tentaqles.yaml`, and the
client's manifest lives at `~/.tentaqles/identities/<client>/manifest.yaml` (`$TQ_HOME` moves `~/.tentaqles`). See
[Manifest and files](manifest.md).

## Index

| Command | Purpose |
|---|---|
| **Setting up a client** | |
| [`tentaqles reach`](#tentaqles-reach) | Make a folder a client, or change the client it already is |
| [`tentaqles add`](#tentaqles-add) | Add a provider, browser, email or language to a client (`set` is the same command) |
| [`tentaqles remove`](#tentaqles-remove) | Remove a provider or browser from a client |
| [`tentaqles drop`](#tentaqles-drop) | Undo everything tentaqles set up for a client |
| [`tentaqles login`](#tentaqles-login) | Run a CLI's own login inside the client's config home |
| **Working in a client's folder** | |
| [`tentaqles hooks install`](#tentaqles-hooks-install) | Add the activation block to your shell profiles |
| [`tentaqles hooks remove`](#tentaqles-hooks-remove) | Take the activation block out of your shell profiles |
| [`tentaqles hooks status`](#tentaqles-hooks-status) | Show each shell's profile and whether the block is in it |
| [`tentaqles activate`](#tentaqles-activate) | Print the activation block for one shell |
| [`tentaqles env`](#tentaqles-env) | Print the environment changes for the current folder |
| [`tentaqles run`](#tentaqles-run) | Run a command with a client's environment |
| [`tentaqles open`](#tentaqles-open) | Open a URL in the client's browser profile |
| **Keeping files in line** | |
| [`tentaqles settings render`](#tentaqles-settings-render) | Write each client's Claude settings, PATH mask and browser shims |
| [`tentaqles doctor`](#tentaqles-doctor) | Check the setup without changing anything |
| [`tentaqles list`](#tentaqles-list) | List the clients |
| [`tentaqles providers list`](#tentaqles-providers-list) | List the provider catalog |
| [`tentaqles providers show`](#tentaqles-providers-show) | Print one provider's definition |
| [`tentaqles providers check`](#tentaqles-providers-check) | Check which provider CLIs are installed |
| [`tentaqles providers add`](#tentaqles-providers-add) | Add or override a provider with a file of your own |
| [`tentaqles version`](#tentaqles-version) | Print the version and commit |
| **Test setup** | |
| [`tentaqles lobe sandbox`](#tentaqles-lobe-sandbox) | Build a throwaway home with two test clients, acme and globex |
| **Used by the Claude Code plugin** | |
| [`tentaqles claude-hook pre-tool-use`](#tentaqles-claude-hook-pre-tool-use) | Decide whether a shell command may run |
| [`tentaqles claude-hook session-start`](#tentaqles-claude-hook-session-start) | Print the client's preamble at the start of a session |
| **Used by the OpenAI Codex CLI hooks** | |
| [`tentaqles codex-hook pre-tool-use`](#tentaqles-codex-hook-pre-tool-use) | Decide whether a shell command may run |
| [`tentaqles codex-hook session-start`](#tentaqles-codex-hook-session-start) | Print the client's preamble at the start of a session |
| [`tentaqles statusline`](#tentaqles-statusline) | The status line a client's Claude settings run |

Commands that change a client (`reach`, `add`, `remove`, `drop`) wait for a typed confirmation and refuse to run from
inside a Claude Code session: they are the developer's commands.

---

## tentaqles reach

Make the current folder, or `<path>`, a client, or change the client it already is.

### Synopsis

```
tentaqles reach [<path>] [--no-banner] [--no-plugin]
```

### Description

The folder's name is the client's name: lowercase letters, digits, `.`, `_` and `-`, starting with a letter or a
digit. Its parent folder is registered as a clients folder. A `<path>` that does not exist yet is created.

**A new client.** reach prints the banner, then asks, in order:

1. the client's email (checked for a valid format);
2. the client's language, the one Claude writes the client's reports, documents, emails and messages in. The list
   starts on English (United States), selected; the rest follow by English name. Typing filters it (`por` leaves
   Portuguese (Brazil) and Portuguese (Portugal)), the arrows move through it, and enter chooses;
3. the git provider, the clouds, the team and meeting platforms, the data platforms, the AI agents besides Claude Code,
   the task management provider and the email provider. A name typed under *Other* goes into the manifest's `other_providers`, and reach says that
   tentaqles can't isolate it;
4. the browsers, with a profile for each.

It then prints what it is about to do and waits for a typed `yes`. Nothing is written before that. Then it:

- writes the manifest and the marker, and a folder under `~/.tentaqles/identities/<client>/` for each identity
  (Claude Code, and each provider with its own config home);
- writes the folder's `.gitconfig-tentaqles` with the client's name and email, and the `includeIf` rules in
  `~/.gitconfig-tentaqles` that apply it to every repository under the folder;
- makes sure your global `~/.gitconfig` includes `~/.gitconfig-tentaqles` (an `include.path` entry) and sets
  `user.useConfigOnly = true`, so a repository outside every client folder has no email to commit with;
- renders the client's Claude settings, PATH mask and browser shims, as
  [`tentaqles settings render`](#tentaqles-settings-render) does;
- installs the tentaqles plugin into the client's Claude config, with `claude plugin marketplace add
  tentaqles/tentaqles` and `claude plugin install tentaqles@tentaqles`, two minutes each at most. A failed install
  doesn't fail reach: it prints the reason and the lines to run;
- adds the activation block to the profile of each shell it detects, when the block isn't there yet.

**An existing client.** In a folder that already is a client, reach shows the client and what it can use, then
offers a menu: nothing, the providers, the AI agents besides Claude Code, the browser and profile, the email, or the
language. The chosen part of the
form opens with the current answers. reach lists the changes and waits for `yes`, then rewrites only those keys of
the manifest (other keys and comments stay), makes the folders of new identities, rewrites the folder's git email
when it changed, and renders the client's files again. A provider taken out keeps its identity folder, with any
login in it, and reach names it.

reach needs a terminal on stdin and stdout, and refuses to run inside a Claude Code session.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--no-banner` | off | Print no banner. `TQ_NO_BANNER=1` does the same. |
| `--no-plugin` | off | Do not install the tentaqles plugin; print the lines that install it instead. |

### Examples

```
# make the current folder a client
cd ~/clients/acme
tentaqles reach

# make a folder that doesn't exist yet a client
tentaqles reach ~/clients/globex

# change an existing client through the menu
cd ~/clients/acme && tentaqles reach
```

### See also

[`add`](#tentaqles-add), [`drop`](#tentaqles-drop), [How it works](../how-it-works.md),
[Manifest and files](manifest.md).

---

## tentaqles add

Change one part of a client's setup without reach's menu. `tentaqles set` is the same command.

### Synopsis

```
tentaqles add <category> [value...] [--client <name or folder>] [--yes]
tentaqles add <provider>            [--client <name or folder>] [--yes]
```

### Description

`add` works on the current folder's client, or on `--client`. It lists every change and waits for a typed `yes`,
then does what reach's menu does: rewrites only the keys it changes, makes the folders of new identities, updates the
folder's git email, and renders the client's files again. Adding a provider with a login of its own prints the next
step, `tentaqles login <client> <cli>`.

| Category | Holds | `add` |
|---|---|---|
| `email` | the client's git email | sets it (needs the address: there is no form for it) |
| `language` | the language Claude writes in for the client: a tag (`pt-BR`) or a name (`brasil`, `japanese`) | sets it |
| `git` | one host: `github`, `gitlab`, `bitbucket`, `azure-devops` (also `gh`, `glab`, `ado`, `azdo`) | replaces it |
| `task` | one tool: `asana`, `linear`, `gh` (GitHub Projects), `azure-devops`, `trello`, `jira` | replaces it |
| `mail` | one email provider: `gmail`, `outlook` (also `google`, `microsoft`) | replaces it |
| `cloud` | a list: `azure`, `aws`, `gcp` (also `az`, `gcloud`, `google`, `amazon`); the first is the primary | adds the values |
| `team` | a list: `teams`, `slack`, `discord`, `zoom`, `google-meet` | adds the values |
| `data` | a list: `databricks`, `snowflake`, `colab` | adds the values |
| `agent` | a list of AI coding agents besides Claude Code: `cline`, `codex`, `continue`, `copilot`, `cursor`, `gemini`, `goose`, `opencode` | adds the values |
| `browser` | a list of browsers, each with a profile: `chrome`, `edge` | adds the browser, or changes its profile (default `Default`) |
| `other` | names tentaqles does not manage | adds the names |

Claude Code is every client's agent, so `claude` is not a value of `agent`. `codex` gets its own Codex home
(`CODEX_HOME`), and the client's Codex config with its two guard hooks.

A value alone finds its category: `tentaqles add aws` is `tentaqles add cloud aws`. With no value, `add <category>`
opens that category's part of the form with the current answers ticked; `email` is the exception and needs the
address.

A language name that fits more than one language, such as `portuguese`, is refused with the matches named.

Open terminals in the client's folder pick up the change at their next prompt after you `cd` out and back in, or in a
new terminal. Claude sessions already running need a restart.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--client` | the current folder's client | The client to change: its name, or its folder. |
| `-y`, `--yes` | off | Do not ask for the typed `yes`. |

### Examples

```
# move the client to Azure DevOps
tentaqles add git azure-devops

# add a cloud, naming the client
tentaqles add cloud aws --client acme

# a value alone finds its category
tentaqles add aws

# an AI agent besides Claude Code, for one client
tentaqles add codex --client personal

# the same, with the category named
tentaqles add agent gemini

# open the task part of the form
tentaqles add task

# the browser and its profile folder
tentaqles add browser chrome "Profile 2"

# the language, by tag or by name
tentaqles add language pt-BR
tentaqles add language japanese

# set is the same command
tentaqles set email dev@acme.example.com
```

### See also

[`remove`](#tentaqles-remove), [`reach`](#tentaqles-reach), [`login`](#tentaqles-login).

---

## tentaqles remove

Remove a provider, browser or other name from a client.

### Synopsis

```
tentaqles remove <category> [value...] [--client <name or folder>] [--yes]
tentaqles remove <provider>            [--client <name or folder>] [--yes]
```

### Description

`remove` takes the categories of [`add`](#tentaqles-add), with these meanings:

| Category | `remove` |
|---|---|
| `email`, `language` | refused: a client always has one |
| `git`, `task`, `mail` | clears it; a value, when given, must be the current one |
| `cloud` | removes the values; the next cloud becomes the primary |
| `team`, `data`, `agent`, `other` | removes the values |
| `browser` | removes the browser named |

A value alone finds its category: `tentaqles remove slack`. `gh` alone means the git host GitHub: when GitHub is the
client's git host, `remove gh` clears it; otherwise it takes out a `gh` identity listed in the manifest.
`tentaqles remove <provider>` takes out any provider no answer holds this way, such as an identity listed by hand. With no value, `remove` of a list category opens that category's part of the form.

Removing a provider keeps its identity folder, with any login in it, and says where it is. Open terminals and Claude
sessions need a restart to see the change, as with `add`.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--client` | the current folder's client | The client to change: its name, or its folder. |
| `-y`, `--yes` | off | Do not ask for the typed `yes`. |

### Examples

```
# take a team tool out
tentaqles remove slack

# stop using Codex for one client
tentaqles remove codex --client personal

# take gh out of a client that moved to Azure DevOps
tentaqles remove gh --client globex

# remove a browser
tentaqles remove browser edge
```

---

## tentaqles drop

Undo everything tentaqles set up for a client, as if it never existed.

### Synopsis

```
tentaqles drop <client or folder> [--yes]
```

### Description

`drop` deletes:

- `~/.tentaqles/identities/<client>/`: the manifest, the client's Claude config (with the plugin installed in it) and
  every CLI login there;
- the browser shims, `~/.tentaqles/shims/<client>/`;
- the marker and `.gitconfig-tentaqles` in the client's folder, and the client's `includeIf` rule in
  `~/.gitconfig-tentaqles`;
- the clients folder's registration, when no other client is left in it;
- the record of the client's folder.

The folder itself stays with your files in it, as an ordinary folder; it is deleted only when nothing else is left in
it. What every client shares stays: the shell profile block, and the `include.path` and `user.useConfigOnly` settings
reach put in your global `~/.gitconfig`. A client that is only partly there (its folder deleted by hand, say) is
dropped as far as it exists.

`drop` lists every deletion and waits for the client's name typed back. The logins it deletes can't be restored. It
takes only a client's name or a folder that holds a marker, never a path for a name, and refuses to run inside a
Claude Code session.

### Flags

| Flag | Default | Description |
|---|---|---|
| `-y`, `--yes` | off | Do not ask for the client's name. |

### Examples

```
tentaqles drop acme
tentaqles drop ~/clients/globex
```

---

## tentaqles bind

Bind the client in this folder to this folder.

### Synopsis

```
tentaqles bind
```

### Description

tentaqles records the folder each client was created in. Only that folder is the client. A copy of the client's
folder somewhere else is refused.

Run `bind` inside a client's folder after you moved it to another clients folder. Run it too for a client created
before folders were recorded. It shows the recorded folder and this folder, and asks you to type `yes`. Nothing is
written before that `yes`.

`bind` refuses to run inside a Claude Code session. Run it in your own terminal.

### Examples

```
cd ~/clients/acme
tentaqles bind
```

---

## tentaqles login

Run a CLI's own login with the client's config home, so the credentials land in the client's identity folder.

### Synopsis

```
tentaqles login <client> <cli> [--sso | --sso-session <name>]
```

### Description

The login is the CLI's own (`gh auth login`, `az login`, `claude auth login`, and so on; `tentaqles providers show
<cli>` names it), run with the client's environment. The credentials land in
`~/.tentaqles/identities/<client>/<cli>`. Logging in from a terminal in the client's folder does the same.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--sso` | off | `aws` only: run `aws configure sso`, the first-time SSO setup. |
| `--sso-session` | none | `aws` only: run `aws sso login --sso-session <name>`. |

### Examples

```
tentaqles login acme gh
tentaqles login acme claude
tentaqles login globex aws --sso
```

---

## tentaqles hooks install

Add the activation block to your shell profiles.

### Synopsis

```
tentaqles hooks install <shell>... | --all-detected
```

### Description

Shells: `bash`, `zsh`, `fish`, `powershell` (Windows PowerShell 5.1) and `pwsh` (PowerShell 7). The first change to a
profile keeps a backup of it as it was, beside it (`<profile>.tq-backup`). A block that differs from the current one
(shown as `outdated` by `hooks status`) is rewritten in place. A profile that holds only one of the block's two
markers is refused; `hooks remove` repairs it.

cmd.exe has no profile file: `hooks install` can't install it. Print its block with `tentaqles activate cmd` and run
it after each `cd`, or use [`tentaqles run`](#tentaqles-run).

With neither a shell nor `--all-detected`, the command fails.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--all-detected` | off | Install for every shell whose profile exists or whose program is on PATH. |

### Examples

```
tentaqles hooks install pwsh
tentaqles hooks install --all-detected
```

---

## tentaqles hooks remove

Take the activation block out of your shell profiles.

### Synopsis

```
tentaqles hooks remove <shell>... | --all-detected
```

### Description

Removes the block, and one blank line before it. A profile with only one of the two markers is repaired: everything
from that marker to the next marker, or to the end of the file, goes. The profile is backed up first, as for
`install`. With neither a shell nor `--all-detected`, the command fails.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--all-detected` | off | Remove from every shell whose profile exists or whose program is on PATH. |

### Examples

```
tentaqles hooks remove zsh
tentaqles hooks remove --all-detected
```

---

## tentaqles hooks status

Show each shell's profile and whether the block is in it.

### Synopsis

```
tentaqles hooks status
```

### Description

Prints one line per shell: its name, its profile file and its state, one of `installed`, `outdated` (a block that
differs from the current one), `present (unmanaged)` (a `tentaqles activate` line written by hand), `missing`,
`no profile`, `corrupt` (only one marker) or `unreadable`.

### Examples

```
tentaqles hooks status
```

---

## tentaqles activate

Print the activation block for one shell.

### Synopsis

```
tentaqles activate <shell>
```

### Description

`<shell>` is `bash`, `zsh`, `fish`, `powershell`, `pwsh` or `cmd`. The block registers a prompt hook that runs
[`tentaqles env`](#tentaqles-env) at every prompt, and defines `tentaqles`, `tq` and a `claude` function that starts
Claude Code through `tentaqles run . -- claude`. The cmd block has no prompt hook and no functions: run it after each
`cd`, and start Claude with `tentaqles run . -- claude`.

`hooks install` writes this block into your profile for you; `activate` is for adding it by hand.

### Examples

```
# bash or zsh, in ~/.bashrc or ~/.zshrc
eval "$(tentaqles activate bash)"

# PowerShell, in $PROFILE
tentaqles activate pwsh | Out-String | Invoke-Expression

# cmd.exe: save the block, then call it after each cd
tentaqles activate cmd > %USERPROFILE%\tq.cmd
```

---

## tentaqles env

Print the environment changes for the current folder. The shell hook runs it at every prompt.

### Synopsis

```
tentaqles env [--shell <shell>] [--json] [--cmd-delayed]
```

### Description

In a client's folder, or any folder under it, it sets:

- `TQ_WS` and `TQ_WS_ROOT`, the client's name and folder;
- `CLAUDE_CONFIG_DIR`, the client's own Claude Code config, and so its own account;
- each provider's config home variable (`GH_CONFIG_DIR`, `AZURE_CONFIG_DIR`, `AWS_CONFIG_FILE` and
  `AWS_SHARED_CREDENTIALS_FILE`, `CLOUDSDK_CONFIG`, `GLAB_CONFIG_DIR`, `DATABRICKS_CONFIG_FILE` and others; see
  [Providers](providers.md));
- `BROWSER`, when the client lists a browser, and the shims folder first on `PATH`;
- a `PATH` without the folders that hold only CLIs of providers outside the client's scope. A folder shared with
  other tools stays, and the client's mask folder goes in front of it, with a stub for each hidden CLI. The variables
  of those providers (`AWS_PROFILE`, `AZURE_*` and the like) are unset.

Leaving the folder puts back exactly what was there before. tentaqles keeps what it needs for that in `__TQ_STATE`.
A shell whose state is lost while a client was active fails closed: tentaqles's own variables and mask folders go,
the folder's identity is applied afresh, and the prompt says the earlier values are lost.

`TQ_ENABLED=0` turns the hook off in a shell; `env` then prints nothing.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--shell` | `bash` | The shell to print code for: `bash`, `zsh`, `fish`, `pwsh`, `powershell` or `cmd`. |
| `--json` | off | Print the changes as JSON instead of shell code. |
| `--cmd-delayed` | off | cmd.exe with delayed expansion on (the cmd block passes it): escape `!` and `^`. |

### Examples

```
cd ~/clients/acme
tentaqles env --json
tentaqles env --shell pwsh
```

### See also

[How it works](../how-it-works.md).

---

## tentaqles run

Run a command with a client's environment, without changing folder.

### Synopsis

```
tentaqles run <client>|. -- <command> [args...]
```

### Description

`.` is the current folder's client; in a neutral folder it is no client at all. The command is looked up on the
client's PATH, so a CLI outside the client's scope isn't found, however it is spelled (`aws`, `aws.exe`, its full
path). Flags after the client belong to the command.

For `claude`:

- tentaqles adds `--dangerously-skip-permissions`, unless the command line sets `--permission-mode` or
  `--dangerously-skip-permissions` itself, runs a `claude` subcommand (`plugin`, `mcp`, `auth` and the like), or asks
  for `--help` or `--version`;
- a session (not a subcommand, `--help` or `--version`) starts only with the tentaqles plugin's hook in place. `run`
  refuses when the client has no Claude identity, when its `settings.json` can't be read, when the plugin is not
  installed or not enabled, when `disableAllHooks` is set, or when a project settings file
  (`.claude/settings.json` or `.claude/settings.local.json`, in any folder from the current one up to the client's
  folder) switches the plugin or all hooks off. The refusal prints the lines that fix it. See
  [Claude hook](claude-hook.md).

For `codex`:

- a session starts only with the tentaqles hooks in the client's Codex config (`CODEX_HOME`) enabled and trusted.
  `run` refuses when they are missing, not trusted, or turned off (`enabled = false` under the hooks, or
  `hooks = false` or `codex_hooks = false` under `[features]`), and prints `tentaqles settings render <client>` as the
  fix;
- a session also refuses an option that turns the hooks off for that run: `--disable hooks`, `--disable codex_hooks`,
  `-c features.hooks=false` or `-c features.codex_hooks=false`, in any of their spellings;
- a session is any codex command except the subcommands that run no agent (`login`, `logout`, `completion`, `doctor`,
  `features`, `plugin`, `mcp`, `update`, `help`, and the like) and `--help` or `--version`. A subcommand tentaqles does
  not know is a session, so it needs the hooks.

From inside a Claude Code session, `run` runs only the client of the current folder: another client's name, or a
neutral folder, is refused, and a nested `claude` gets no bypass flag.

### Examples

```
# a command under acme's identity, from anywhere
tentaqles run acme -- gh auth status

# Claude Code in the current folder's client
tentaqles run . -- claude

# a claude subcommand gets no added flag
tentaqles run acme -- claude plugin list
```

---

## tentaqles open

Open an `http` or `https` URL in the client's browser profile.

### Synopsis

```
tentaqles open [--browser chrome|edge] <url>
```

### Description

`open` starts the client's browser from its install path with `--profile-directory=<the client's profile>`: the first
browser of the manifest's `browsers` list, or the one `--browser` names. It never hands the URL to the system's
default browser.

It refuses any other URL scheme, a value starting with `-`, a folder that is no client's, a client without browsers,
and a listed browser that isn't installed.

In a client's terminal, `BROWSER`, `xdg-open` and `open` go the same way: they are shims under
`~/.tentaqles/shims/<client>/`. A program that opens links through the operating system directly (on Windows,
`Start-Process`, `start`, or a click on a link in the terminal window) uses the system's default browser instead.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--browser` | the first listed browser | `chrome` or `edge`. |

### Examples

```
tentaqles open https://github.com/acme/app/pulls
tentaqles open --browser edge https://portal.azure.com
```

---

## tentaqles settings render

Bring each client's files in line with its manifest.

### Synopsis

```
tentaqles settings render <client>... | --all [--dry-run] [--no-statusline]
```

### Description

For each client it writes:

- **its Claude `settings.json`**:
  - the env vars of the manifest's `claude.env`, plus three tentaqles always sets: `ENABLE_STOP_REVIEW=0`,
    `SECURITY_REVIEW_MODEL` and `SG_AGENTIC_MODEL` (they keep the security-review plugin's reviews off Opus);
  - Claude Code's `language` setting, the English name of the client's language ("Portuguese (Brazil)"). A
    `language` you set by hand before tentaqles wrote one stays yours;
  - the tentaqles status line. It replaces a missing status line, a command containing `@latest`, a `ccstatusline`
    command (pinned or not), and a previous tentaqles status line; any other status line stays.

  Only what tentaqles wrote earlier is ever removed; anything added by hand stays. tentaqles records what it wrote in
  `.tq-settings-state.json` beside the settings;
- **its Codex `config.toml`**, when codex is in the client's scope: the PreToolUse and SessionStart hooks that run
  `tentaqles codex-hook` (the command is the absolute path of tentaqles, quoted), trusted and enabled, and the
  client's language line as `developer_instructions`. Every other key and state entry stays. Nothing is written for a
  client without codex, and nothing is removed. `--dry-run` says it did not check the Codex config. The hooks are
  trusted here because opting the client into codex is the consent;
- **its PATH mask**, `~/.tentaqles/identities/<client>/path-mask/`: one stub per CLI of a provider outside the client's
  scope, for the PATH folders that hold it beside other tools. A stub prints
  `<name>: command not found (outside the scope of identity <client>)` and exits 127;
- **its browser shims**, when the client lists a browser.

It also records the folder of a client that has none recorded (a client created before tentaqles recorded
folders), so that a copy of the folder elsewhere is refused from then on. A name that two folders claim is
never recorded here: run [`tentaqles bind`](#tentaqles-bind) in the folder that is the client.

Run it after editing a manifest by hand, and after upgrading tentaqles: the shims and stubs are copies of the binary.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--all` | off | Render every client. |
| `--dry-run` | off | Show what would change; write nothing. |
| `--no-statusline` | off | Leave the status line alone. |

### Examples

```
tentaqles settings render --all
tentaqles settings render acme --dry-run
```

---

## tentaqles doctor

Check the setup without changing anything.

### Synopsis

```
tentaqles doctor [--json]
```

### Description

doctor reports:

- **the shell hook**: whether this shell carries the identity of the folder it runs in. It reads this shell's state,
  not your profile files; `tentaqles hooks status` shows the profiles;
- **git**: the include file, `user.useConfigOnly`, a global `user.email` that would leak into neutral folders, and
  each client's `.gitconfig-tentaqles`;
- **each client**: its manifest, its identity folders and Claude login, the CLIs it needs (missing, or shadowed by a
  script earlier on PATH that sets the identity itself), its browser profiles and shims, its Claude settings and its
  plugin;
- **your shell profiles**: an alias or function that replaces the `claude` launcher;
- **your provider files** under `~/.tentaqles/providers/`.

Each finding has a level (`ok`, `info`, `warn`, `error`), a message and, when there is one, the fix. doctor exits 1
when any finding is an error, including when there are no clients yet. See [Troubleshooting](../troubleshooting.md)
for each finding.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | off | Print the findings as JSON. |

### Examples

```
tentaqles doctor
tentaqles doctor --json
```

---

## tentaqles list

List the clients.

### Synopsis

```
tentaqles list [--json]
```

### Description

Prints each client's name, git email, language, identities and folder. A client whose manifest can't be read is
reported as a warning on stderr.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | off | Print the clients folders, the clients and any errors as JSON. |

### Examples

```
tentaqles list
```

---

## tentaqles providers list

List the provider catalog.

### Synopsis

```
tentaqles providers list [--category <category>] [--json]
```

### Description

Prints each provider's id, name, category, CLI, whether it has an identity (a config home tentaqles moves) and its
source (`embedded`, or your own file). Overlaps between providers are reported on stderr. A provider file of your own
that does not load is an error, and the command fails.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--category` | all | Show one category: `cloud`, `vcs`, `data`, `deploy`, `pm`, `agent`, `chat`, `other` and the like. |
| `--json` | off | Print the providers as JSON. |

### Examples

```
tentaqles providers list
tentaqles providers list --category cloud
```

---

## tentaqles providers show

Print one provider's full definition as YAML.

### Synopsis

```
tentaqles providers show <id>
```

### Examples

```
tentaqles providers show aws
```

---

## tentaqles providers check

Check which provider CLIs are installed.

### Synopsis

```
tentaqles providers check [<id>...] [--all] [--workspace <client>] [--json]
```

### Description

Prints `[ok]` with the version and path for an installed CLI, `[missing]` with an install hint for a missing one, and
`[n/a]` for a provider with no CLI. With no ids and no flag, it checks every provider. It exits 1 when a CLI is
missing.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--all` | off | Check every provider in the catalog. |
| `--workspace` | none | Check only the providers of this client's identities. |
| `--json` | off | Print the results as JSON. |

### Examples

```
tentaqles providers check gh az
tentaqles providers check --workspace acme
```

---

## tentaqles providers add

Add a provider, or override a built-in one, with a file of your own under `~/.tentaqles/providers/`.

### Synopsis

```
tentaqles providers add <id> --name <name> --category <category> [flags]
```

### Description

Writes `~/.tentaqles/providers/<id>.yaml` and prints its path. A built-in provider, or one that already has a file of
yours, is refused without `--force`. See [Providers](providers.md) for the file format and what each field does.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--name` | required | Display name. |
| `--category` | required | One of `cloud`, `vcs`, `data`, `deploy`, `pm`, `agent`, `other`. |
| `--command` | none | The CLI's command name. |
| `--version-args` | none | Arguments that print the CLI's version (comma-separated or repeated). |
| `--env` | none | An identity variable, `KEY=VALUE`; repeat for more. `{dir}` stands for the client's identity folder. |
| `--login` | none | The login command's arguments, such as `"auth login"`. |
| `--docs` | none | A docs URL. |
| `--force` | off | Overwrite a built-in provider or an existing file of yours. |

### Examples

```
tentaqles providers add fly --name Fly.io --category deploy \
  --command flyctl --version-args version --env 'FLY_CONFIG_DIR={dir}' --login "auth login"
```

---

## tentaqles version

Print the version and the commit the binary was built from.

### Synopsis

```
tentaqles version [--notices]
```

### Description

A binary built with `go install` or `go build` prints `dev (commit unknown (dev build))`.

With `--notices`, the command prints the notices built into the binary instead. It prints the NOTICE file, the
tentaqles license, and the license text of each project tentaqles draws on. The version line is not printed.

### Flags

| Flag | Default | Description |
|---|---|---|
| `--notices` | off | Print the NOTICE file, the tentaqles license, and the license texts of the projects tentaqles draws on. |

---

## tentaqles lobe sandbox

Build a throwaway tentaqles home with two test clients, `acme` and `globex`.

### Synopsis

```
tentaqles lobe sandbox <folder>
```

### Description

`lobe sandbox` builds a fresh home under `<folder>`, with two clients. Test agents, benchmarks and CI use it to get
clients without a person answering the `tentaqles reach` form. A developer's own setup does not need it.

It writes:

- the clients `acme` and `globex`, under `<folder>/clients`;
- the Codex agent for `acme`. `globex` has none;
- a `lobe:` mapping at the end of each manifest, holding `model: off`.

It installs no plugin and writes no shell profile. It also runs inside a Claude Code session.

Before it writes anything, the command refuses unless:

- `TQ_HOME`, `HOME` and `USERPROFILE` all point inside `<folder>`;
- `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` are unset. Git's global config goes under `HOME`;
- the home (`TQ_HOME`) and `<folder>/clients` are each empty or absent.

So the command never touches a real home. A refusal creates nothing.

It prints `ok acme globex` when it succeeds. On a refusal or a failure it prints one line that starts with
`SETUP FAIL`, and it exits with status 1.

### Examples

```
export TQ_HOME="$PWD/sandbox/home/.tentaqles" HOME="$PWD/sandbox/home" USERPROFILE="$PWD/sandbox/home"
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM
tentaqles lobe sandbox "$PWD/sandbox"
```

---

## tentaqles lobe selftest

Prove the memory's store works on this machine, with the installed program. A tester runs it in a client's folder.

### Synopsis

```
tentaqles lobe selftest
```

### Description

Run inside a client's folder, `lobe selftest` runs three checks of the store, the same ones the store's tests make, from
the installed program:

- twenty writer processes each append 500 lines to one file under the store lock. The command then reads the file and
  counts its lines, the lines that do not parse as JSON (`torn`), and the writer and line pairs that are missing
  (`lost`);
- one file is replaced 1000 times while a reader reads it in a loop. A read whose bytes are not a complete JSON object
  counts as `partial`;
- the store gives the key of the client's `app` folder (`repo-key`).

All of it runs in a scratch folder under the client's memory folder, `tmp/<random>` inside the store, and the scratch
folder is removed at the end. The `tmp` folder itself may stay, empty.

It prints three lines:

```
writers=20 lines=10000 torn=0 lost=0
replaces=1000 partial=0
repo-key app=r_a172cedc
```

It exits 0 only when `lines` is 10000, `torn`, `lost` and `partial` are 0, and the key is `r_a172cedc`. Otherwise it
prints the lines and exits 1. Outside every client's folder it prints `tentaqles lobe selftest: run it inside a client's
folder` and exits 1.

The command's writer children run the same program with hidden flags. A person never runs those.

---

## tentaqles lobe-hook claude

The entry point of the memory's hooks. The Claude Code plugin starts it; you don't.

### Synopsis

```
tentaqles lobe-hook claude <event>
```

### Description

The plugin starts `tentaqles lobe-hook claude <event>` with the hook's JSON on stdin. The command records what the memory
needs about the call and prints nothing or a little context. On any problem it prints nothing and exits 0, so it never
blocks a tool call.

The seven events are `session-start`, `user-prompt-submit`, `pre-tool-use`, `post-tool-use`, `post-tool-use-failure`,
`stop` and `session-end`.

Setting `TQ_LOBE_NO_HOOKS=1` switches the hooks off. A person never needs to run this command.

---

## tentaqles claude-hook pre-tool-use

Decide whether a shell command Claude Code is about to run may run. The plugin's hook calls it; you don't.

### Synopsis

```
tentaqles claude-hook pre-tool-use [--json]
```

### Description

Reads the hook's JSON on stdin and answers with an exit code: 0 lets the command run, 2 blocks it, with the reason on
stderr. It blocks a git, gh or cloud command under the wrong identity and fails closed: anything it can't judge is
blocked. The rules, and every case it fails closed on, are in [Claude hook](claude-hook.md).

### Flags

| Flag | Default | Description |
|---|---|---|
| `--json` | off | Print the decision as JSON. |

---

## tentaqles claude-hook session-start

Print the preamble Claude Code adds at the start of a session. The plugin's hook calls it; you don't.

### Synopsis

```
tentaqles claude-hook session-start
```

### Description

Prints the folder's client; its language, with the instruction to write the client's reports, documents, emails and
messages in it; its git provider, user and email; its clouds; the identity and its `CLAUDE_CONFIG_DIR`; the
client's tools; the findings of `tentaqles doctor` for this folder; and one line on what the hook blocks. In a
neutral folder it says so. It never fails: a session always starts. See [Claude hook](claude-hook.md).

---

## tentaqles codex-hook pre-tool-use

Decide whether a shell command the OpenAI Codex CLI is about to run may run. The Codex hook calls it; you don't.

### Synopsis

```
tentaqles codex-hook pre-tool-use
```

### Description

Reads the hook's JSON on stdin. The same check as `claude-hook pre-tool-use`, with Codex's answer: a blocked command
prints one JSON object on stdout (`permissionDecision` `deny`, the reason) and the reason on stderr, and exits 0; an
allowed one prints nothing and exits 0. Every error is a deny the same way, since Codex runs a call whose hook gives no
deny. See [Claude hook](claude-hook.md) for the rules.

---

## tentaqles codex-hook session-start

Print the preamble OpenAI Codex CLI adds at the start of a session. The Codex hook calls it; you don't.

### Synopsis

```
tentaqles codex-hook session-start
```

### Description

Prints the same text as `claude-hook session-start` for the payload's folder. It never fails: a session always starts.

---

## tentaqles statusline

The status line a client's Claude settings run. It doesn't show in `tentaqles --help`.

### Synopsis

```
tentaqles statusline
```

### Description

Reads Claude Code's status-line JSON on stdin and prints one line: the client, the git email, the model, the context
size and the cost. [`settings render`](#tentaqles-settings-render) writes it into each client's settings.
