# Tentaqles commands

`tentaqles` is the CLI, and `tq` is the same program under a shorter name. `docs/IDEATION.md` is the spec.

**Human-only** means the command refuses to run from an agent session, and asks you to type `yes` before it
writes. It refuses when stdin or stdout is not a terminal, when a harness is in the process tree, or when the
environment carries an agent marker. The refusal goes to `~/.tentaqles/denials.jsonl`.

## Setting up a client

| Command | What it does |
|---|---|
| `tentaqles reach [<path>]` | **Human-only.** Makes the current folder, or `<path>`, a client identity. It prints the banner (`--no-banner` or `TQ_NO_BANNER=1` for none), asks for the identity email and the client's providers, and after you type `yes` writes the manifest and trusts it. The folder's name is the identity's name, and its parent folder is where your clients live. In a folder that already is a client, it shows the client and changes nothing. Once the client is trusted it installs the tentaqles plugin into the client's Claude config, with `claude plugin marketplace add tentaqles/tentaqles` and `claude plugin install tentaqles@tentaqles` (two minutes each at most), and signs the record again: the plugin holds the guard (`--no-plugin` skips this). A failed install does not fail reach: it prints the reason and the line to run, and `tq run` refuses to start a session until the plugin is there. Then it installs the CLI of each provider the client can use, one line each (`--no-install` skips this; a failure does not stop the rest). |
| `tentaqles install <provider>` | Installs a provider's CLI for the current client, through the first package manager on its PATH from `winget`, `scoop`, `brew`, `apt`, `npm`, `pip`, and prints `<provider>: installed <version>`, `already present <version>`, `manual, download from <docs URL>` or `install failed: <reason>` (exit 1). It takes the provider id and nothing else, runs at most 10 minutes, and refuses a provider outside the client's scope (exit 1, logged). An agent may run it for a provider in scope. tq never launches a desktop installer, and accepts the vendor's licence through winget's flags. See [PROVIDERS.md](PROVIDERS.md#installing-a-providers-cli). |
| `tq login <ws> <identity>` | **Human-only.** Runs a CLI's own login inside that client's private config folder. |
| `tq secret set\|rm\|status` | Keeps an issue-tracker token in the OS keychain. `set` and `rm` are **human-only**. `status` shows presence only. |
| `tq list` | Lists every client, whether it is trusted, its git email and its identities. |

## Trust

| Command | What it does |
|---|---|
| `tq allow <ws>` | **Human-only.** Trusts the client as it is now, after a hand edit of its manifest. It renders the identity's settings, then signs the integrity record. |
| `tq deny <ws>` | **Human-only.** Revokes trust. |
| `tq hooks verify [--accept]` | Lists what changed against the signed record. `--accept` re-signs, and is **human-only**. |

## The terminal

| Command | What it does |
|---|---|
| `tq hooks install\|remove\|status` | Manages the shell-profile block that makes a terminal follow the folder. `install` and `remove` are **human-only**. |
| `tq run <ws>\|. -- <cmd>` | Runs one command under an identity without `cd`-ing into its folder. A `claude` session (a prompt, or the interactive UI) in an identity's folder starts only when the client's Claude config has the tentaqles plugin enabled with its guard hook, and no project settings file in the folders it starts in (up to the client's folder) switches the guard off: otherwise it exits non-zero, says why, and prints the line `tq run <ws> -- claude plugin install tentaqles@tentaqles`. `claude` subcommands, `--help` and `--version` are not refused, so that line runs. A neutral folder's `claude` is unchanged. See [CLAUDE-HOOK.md](CLAUDE-HOOK.md#no-session-without-the-hook). |
| `tentaqles open [--browser chrome\|edge] <url>` | Opens an `http` or `https` URL in the client's browser: the first one in its `browsers:` list, started from its install path with `--profile-directory=<the client's profile>`, never through the system's default handler. It refuses any other scheme, a value that starts with `-`, a folder that is not a client's (nothing opens), a client with no `browsers:` (`no browser is set for this client`) and a listed browser that is not installed (`<browser> is not installed`). An agent may run it. |
| `tq activate <shell>` | Prints the shell hook. The profile block runs it. |
| `tq env` | Prints the environment changes for this folder. The shell hook runs it at each prompt. cmd.exe has no prompt hook, so there you call the hook yourself or use `tq run`. |

## Checking

| Command | What it does |
|---|---|
| `tq doctor`, `tq status` | Health report: trust, hooks, git identity, logins and settings drift. Read-only, and each finding ends with its fix. `--json` for scripts. |
| `tq status --denials` | **Human-only read.** The denial log. |
| `tq providers list\|show\|check\|add` | The provider catalog. `add` is **human-only**. |
| `tq secrets audit` | Finds where secrets sit in agent config. Prints path and line only. |
| `tq dotenv run [--file F] -- <cmd>` | Uses a `.env` for one command and masks its values in the output. It is the only way to use a `.env`, because the guard blocks reading one. |
| `tq version` | The version and the commit it was built from. |

## Claude Code config

| Command | What it does |
|---|---|
| `tq settings render [--all]` | **Human-only.** Writes tq's part of the identity's Claude `settings.json`, the out-of-scope MCP filter and the PATH mask. For a client with `browsers:` it also writes the browser shims and signs them, and sets `--user-data-dir` on the Playwright MCP entries (see Browsers). |
| `tq bundle catalog\|diff\|sync\|capture` | Plugins, skills and MCP servers for a client from a shared catalog. `sync` and `capture` are **human-only**. |
| `tq statusline` | Hidden. What Claude Code's status line calls. |

## Browsers

`tentaqles reach` records a `browsers:` list in the manifest (Chrome or Edge, and the profile folder of each). For
a client that has one, tq (ADR-0016):

- opens pages in that profile: `tentaqles open <url>`, `BROWSER`, and the client's `xdg-open` and `open`. In the
  client's terminal `BROWSER` names a program under `~/.tentaqles/shims/<client>/`, and that folder comes first on
  `PATH`, so `xdg-open <url>` and `open <url>` do what `tentaqles open` does for any argument that starts with
  `http://` or `https://`: it opens in the client's profile, or tq refuses it and nothing opens; it never reaches the
  real program. `open -a "Google Chrome" <url>` and `open -a "Microsoft Edge" <url>` go to the client's profile in
  that browser, and `open -a` for any other app, like any line with no such URL, passes to the real program. Leaving the folder puts `BROWSER` and `PATH` back as they were. The programs
  are copies of the tq binary, written by `tq settings render` and covered by the integrity record: after you
  upgrade tq, run `tq settings render <client>` so they match the new binary.
- denies, in the hook, as `BLOCKED [identity/other-browser-profile]`, a shell command that starts `chrome`, `msedge`
  or `google-chrome` with another profile, another `--user-data-dir`, or neither flag (Chrome then picks the profile
  it used last; `--version`, `--help` and `--headless` with no profile flag are allowed), and the routes that hand a URL to the system's default browser: `start <url>`,
  `Start-Process <url>`, `explorer <url>`, `rundll32 url.dll` and `cmd /c start <url>`. The message names
  `tentaqles open <url>`. A URL in a commit message, a grep pattern or an `echo` stays allowed.
  `--headless` with no profile flag is allowed by the owner's decision, but which user data folder a headless
  start uses is unconfirmed: if it is the default one, a headless page reads that profile's cookies.
- gives the Playwright MCP server its own browser folder, `~/.tentaqles/identities/<client>/browser`, because it
  cannot attach to a profile that is already open: `tq settings render` sets `--user-data-dir` on its entries in the
  client's Claude config and keeps their other arguments. It edits the identity's `.claude.json` only: a
  repository's `.mcp.json` and a plugin's own files are never rewritten, so a Playwright server declared there
  keeps its own arguments.
- warns in `tq doctor` (`browser-profile-missing`) when a named profile has no folder in that browser's user data
  folder. The browser makes the profile the first time `tentaqles open` starts it.

**Claude in Chrome attaches to a browser that is already running**, in whatever profile that browser is in, so tq
cannot verify its profile. The hook denies `mcp__claude-in-chrome__*` for a client whose `browsers:` list does not
include Chrome, and allows it for one that does; for that client, keep only the client's own Chrome profile open
while Claude in Chrome is in use.

A client with no `browsers:` list is unchanged: no `BROWSER`, no shims on `PATH`, no browser rule in the hook.

## Upgrading and leaving

| Command | What it does |
|---|---|
| `tq migrate` | **Human-only with `--apply`.** Moves a hand-built setup and old manifests under tq's management. |
| `tq uninstall --restore` | **Human-only.** Replays the migration journal backwards. |

## The plugin

| Skill | What it does |
|---|---|
| `/tentaqles:onboard` | Connects a client's tools in Claude Code. |

The plugin also holds the guard hook, which runs `tq claude-hook` before each tool call.
