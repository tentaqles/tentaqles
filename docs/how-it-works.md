# How it works

For developers who want to know what tentaqles does to a terminal, to git and to Claude Code, and why. The
commands themselves are in the [command reference](reference/commands.md); the files and variables in the
[files reference](reference/manifest.md).

```text
~/clients/                       a clients folder (registered in ~/.tentaqles/config.yaml)
├── acme/                        a client folder: .tentaqles.yaml names "acme"
│   └── api/src/                 any subfolder is acme's too
└── globex/                      another client

            cd ~/clients/acme/api/src
                      │
                      ▼
   resolve: base ~/clients → folder acme → marker "acme" → ~/.tentaqles/identities/acme/manifest.yaml
                      │
                      ▼
   acme's terminal: CLAUDE_CONFIG_DIR, GH_CONFIG_DIR, AWS_CONFIG_FILE … → ~/.tentaqles/identities/acme/…
                    git name and email of acme, BROWSER → acme's profile,
                    PATH without the CLIs acme does not use
```

## From a folder to its client

A **clients folder** is a folder whose children are clients, such as `~/clients` or `C:\repos`. tentaqles
registers it in `~/.tentaqles/config.yaml` the first time `tentaqles reach` makes one of its children a client.

A **client folder** is a direct child of a clients folder that holds a marker, `.tentaqles.yaml`. The marker names
the client and nothing else; the client's settings live in its **manifest**,
`~/.tentaqles/identities/<client>/manifest.yaml`, outside the folder, where a repository you clone can't change it.

To find the client of a folder, tentaqles walks up to the clients folder it is in, takes the first folder under it,
and checks:

1. that folder has a marker, and it is a regular file;
2. the marker's `client` is that folder's name;
3. the manifest `~/.tentaqles/identities/<name>/manifest.yaml` exists and loads, and its `client` is the same name.

Any folder below a client folder belongs to that client: `~/clients/acme/api/src` is acme. A folder outside every
clients folder, a clients folder itself, and a folder with no marker are **neutral**: no client identity at all.

A marker copied into another folder lends it nothing: it names a client that is not that folder, so the folder stays
neutral. A client whose manifest does not load is **unreadable**: its terminal gets no identity, and the Claude Code
hook denies every shell command there until the manifest is fixed.

## A client's terminal

The shell hook (`tentaqles hooks install`, which `reach` runs for you) adds a short block to your shell profile. At
every prompt it runs `tentaqles env`, which compares the current folder with what the shell carries and prints the
changes. In a client's folder the shell gets:

- **`TQ_WS` and `TQ_WS_ROOT`**: the client's name and folder.
- **`CLAUDE_CONFIG_DIR`**: `~/.tentaqles/identities/<client>/claude`. Claude Code started there uses the client's own
  config home, so its own login and subscription, its own settings and plugins.
- **A config home per identity**: each provider in the manifest's `identities` gets its folder under
  `~/.tentaqles/identities/<client>/`, and the variable that moves its CLI's config there: `GH_CONFIG_DIR` for gh,
  `AZURE_CONFIG_DIR` for az, `AWS_CONFIG_FILE` and `AWS_SHARED_CREDENTIALS_FILE` for aws, `CLOUDSDK_CONFIG` for
  gcloud, and so on (`tentaqles providers show <id>`). A login made in that terminal, or with
  `tentaqles login <client> <cli>`, lands there and stays the client's.
- **`BROWSER`**, when the client lists a browser: the client's `tq-open` shim (see [Browsers](#browsers)).
- **The PATH layer** (below), and the variables of providers outside the client's scope unset.

The hook also defines `tentaqles`, `tq` and `claude` as shell functions; `claude` runs
`tentaqles run . -- claude`.

### Leaving restores everything

Before it changes a variable, tentaqles records the value it had (or that it had none) in `__TQ_STATE`, in the shell
itself. When you leave the client's folder, or move to another client, it puts every value back exactly as it was,
PATH included, merged with any change you made to PATH while inside. tentaqles keeps no state on disk for this: two
terminals in two clients never see each other.

If `__TQ_STATE` is lost (unset or edited) while `TQ_WS` still names a client, tentaqles cannot know the old values.
It fails closed: it removes every variable it sets for any client and every mask folder from PATH, applies the
current folder's identity afresh, and says so at the prompt. A new terminal has your values.

`TQ_ENABLED=0` turns the hook off in a shell. `tentaqles run <client> -- <command>` gives one command a client's
environment without changing folder.

### The PATH

Each provider in the catalog lists its executables (`tentaqles providers show <id>`). The **scope** of a client is
every provider it uses: its identities, its clouds, its git and email providers and its `scope:` list. A CLI of a
provider **outside** the scope must not resolve by name in the client's terminal:

- a PATH folder that holds only out-of-scope CLIs (the AWS CLI's install folder, say) is **dropped** from PATH;
- a folder shared with other tools (`/usr/local/bin`, npm's global folder, Scoop's shims) is kept, and the client's
  **mask folder**, `~/.tentaqles/identities/<client>/path-mask`, goes in front of it. It holds a stub for every
  out-of-scope name, in every spelling the shell finds on PATH (`aws`, `aws.exe`, `aws.cmd`, `aws.ps1`, …). A stub
  prints `aws: command not found (outside the scope of identity acme)` and exits 127.

An in-scope claim always wins: a CLI that an in-scope provider claims is never hidden. A provider file of your own
can hide more, never less. `tentaqles settings render` writes the stubs; run it again after upgrading.

This hides CLIs by name. A program can still run one by its full path: see the [threat model](threat-model.md).

## Git

Each client folder gets `.gitconfig-tentaqles` with the client's git name and email and `useConfigOnly = true`.
`~/.gitconfig-tentaqles` holds one rule per client folder:

```ini
[includeIf "gitdir/i:C:/repos/acme/"]
    path = C:/repos/acme/.gitconfig-tentaqles
```

and `~/.gitconfig` includes `~/.gitconfig-tentaqles` and sets `user.useConfigOnly = true`. So every repository under
a client folder commits as that client, whatever terminal or tool runs git, and a repository outside every client
folder has no email at all: git refuses to commit there instead of guessing one. A `user.email` in `~/.gitconfig`
would defeat that; `tentaqles doctor` reports it.

## Claude Code

Each client has its own Claude Code config home, `~/.tentaqles/identities/<client>/claude`. The first time you start
`claude` in the client's terminal it asks you to log in: log in with the client's account. Every later session under
that folder is that client's, with its own history, settings and plugins.

`tentaqles settings render` keeps the client's `settings.json` in line with its manifest. It writes only these keys
and records what it wrote in `.tq-settings-state.json`, so a later render removes only its own entries:

- `env`: the manifest's `claude.env`, plus `ENABLE_STOP_REVIEW=0`, `SECURITY_REVIEW_MODEL` and `SG_AGENTIC_MODEL`
  (which keep the security-guidance plugin's reviews off Opus);
- `language`: the client's language name (`Portuguese (Brazil)`), Claude Code's own language setting. A `language`
  you set by hand before tentaqles wrote one stays yours;
- `statusLine`: `tentaqles statusline` (the client, git email, model, context and cost), when there is none, or the
  current one runs an unpinned `@latest` package, `ccstatusline`, or tentaqles's own. `--no-statusline` leaves it.

It writes no permission setting.

### The plugin

`reach` installs the tentaqles plugin into the client's Claude config. Its hooks:

- **SessionStart** adds a short preamble to every session: the client, its language with the instruction to write
  every report, document, email and message for the client's people in it, its git and cloud identity, its
  `CLAUDE_CONFIG_DIR`, its tools, what `tentaqles doctor` finds there, and what the hook blocks.
- **PreToolUse**, on Bash and PowerShell calls only, reads the command and blocks a git, gh or cloud command that
  would run under the wrong identity. It fails closed and never stalls a session. See the
  [Claude Code hook reference](reference/claude-hook.md).

`tentaqles run` refuses to start a Claude session in a client whose config has no enabled tentaqles plugin, so no
session there runs without the check.

## Language

Each client has a language, a tag such as `pt-BR`, stored in its manifest; a manifest without one is `en-US`. Claude
gets it twice: as Claude Code's `language` setting in the client's `settings.json`, and in the session-start
preamble, which asks for every report, document, email and message to the client's clients and teammates in that
language. `reach` asks for it after the email; `tentaqles add language <tag or name>` changes it.

## Browsers

A client may list a browser profile, Chrome or Edge, in `browsers:`. tentaqles then writes three programs in
`~/.tentaqles/shims/<client>/`:

- `tq-open`, which `BROWSER` names;
- `xdg-open` and `open`, first on the client's PATH, which open one `http` or `https` URL the same way and hand
  anything else to the real program.

Each starts the browser from its install path with `--profile-directory=<profile>`, never through the system's
default handler. So a CLI that opens a link through `BROWSER`, `xdg-open` or `open` (`gh auth login`,
`gh pr view --web`, most login flows) opens it in the client's profile, as does `tentaqles open <url>`. A link
opened by the operating system itself, such as a click in the terminal window, goes to the system's default browser.

## What the commands write

| Command | Writes |
|---|---|
| `tentaqles reach` | The manifest, the marker, a folder per identity, the folder's `.gitconfig-tentaqles`, the client's rule in `~/.gitconfig-tentaqles`, the include and `useConfigOnly` in `~/.gitconfig`, the clients folder in `config.yaml`, the client's Claude settings, PATH mask and browser shims, the tentaqles plugin in the client's Claude config, and the activation block in each shell profile that lacks it. |
| `tentaqles add`, `tentaqles remove` | The manifest keys they change (every other key and comment stays), a folder for each new identity, the folder's git email when it changed, and the client's settings, mask and shims again. A removed identity's folder stays, with any login in it. |
| `tentaqles drop` | Deletes the client's identity folder (manifest, Claude config, logins), its shims, its marker and `.gitconfig-tentaqles`, its git rule, and the clients folder's registration when no other client is in it. Your files stay; a folder left empty is deleted. `~/.gitconfig`'s include and `useConfigOnly`, and the shell profile block, stay: every client uses them. |
| `tentaqles settings render` | The client's `settings.json` keys above, the PATH mask and the browser shims. |
| `tentaqles hooks install` | The activation block in your shell profiles. |

`reach`, `add`, `remove` and `drop` list what they will do and wait for a typed confirmation, and they refuse to run
from inside a Claude Code session.
