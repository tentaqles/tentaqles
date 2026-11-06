# Tentaqles commands

`tentaqles` is the CLI, and `tq` is the same program under a shorter name. `docs/IDEATION.md` is the spec.

**Human-only** means the command refuses to run from an agent session, and asks you to type `yes` before it
writes. It refuses when stdin or stdout is not a terminal, when a harness is in the process tree, or when the
environment carries an agent marker. The refusal goes to `~/.tentaqles/denials.jsonl`.

## Setting up a client

| Command | What it does |
|---|---|
| `tentaqles reach [<path>]` | **Human-only.** Makes the current folder, or `<path>`, a client identity. It prints the banner (`--no-banner` or `TQ_NO_BANNER=1` for none), asks for the identity email and the client's providers, and after you type `yes` writes the manifest and trusts it. The folder's name is the identity's name, and its parent folder is where your clients live. In a folder that already is a client, it shows the client and changes nothing. |
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
| `tq run <ws>\|. -- <cmd>` | Runs one command under an identity without `cd`-ing into its folder. |
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
| `tq settings render [--all]` | **Human-only.** Writes tq's part of the identity's Claude `settings.json`, the out-of-scope MCP filter and the PATH mask. |
| `tq bundle catalog\|diff\|sync\|capture` | Plugins, skills and MCP servers for a client from a shared catalog. `sync` and `capture` are **human-only**. |
| `tq statusline` | Hidden. What Claude Code's status line calls. |

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
