# Getting started

From nothing to a working client in about five minutes: install tentaqles, make a folder a client, log in, and check
each step. The examples use a client named `acme` under `~/clients`; use your own names.

## Before you start

- **Claude Code** installed (`claude` on your PATH). tentaqles installs its plugin into each client's Claude config.
- **git**.
- The CLIs your clients use (`gh`, `az`, `aws`...), installed the usual way. tentaqles doesn't install them; it gives
  each client its own login for them.
- On Windows, **Git Bash** (Claude Code needs it, and so does the tentaqles plugin).

## 1. Install

```sh
# macOS / Linux
curl -fsSL https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.sh | sh
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/tentaqles/tentaqles/main/installers/install.ps1 | iex
```

The installer checks the download's checksum (and its build provenance when `gh` is logged in), then installs
`tentaqles` and its alias `tq`. On macOS and Linux they go to `~/.local/bin`; add it to your PATH if the installer says
so. On Windows they go to `%LOCALAPPDATA%\tentaqles\bin`, which the installer adds to your user PATH; open a new
terminal afterwards. Homebrew, Scoop and Go are in the [README](../README.md#get-started).

Check:

```console
$ tentaqles version
tentaqles 0.13.1 (commit 546d922...)
```

## 2. Make a folder a client

Pick a folder for all your clients, such as `~/clients`. Each client is one folder in it, named after the client in
lowercase (`acme`, `globex`, `acme-labs`).

```sh
tentaqles reach ~/clients/acme
```

The folder may already hold your repositories; reach only adds two small files to it. If it doesn't exist yet, reach
creates it. reach then asks, one question at a time:

| Question | What to answer |
|---|---|
| Identity email | The email the client knows you by. Every commit under the folder uses it. |
| Language | The language Claude writes this client's reports, documents, emails and messages in. The list starts on English (United States); type to filter it (`por` leaves the Portuguese ones), move with the arrows, choose with enter. |
| Git provider | GitHub, GitLab, Bitbucket or Azure DevOps. |
| Cloud providers | Azure, AWS, Google Cloud: tick every one the client uses. The first is the main one. |
| Team, data, task tools | Teams, Slack, Databricks, Jira and so on. A name under *Other* is recorded, but tentaqles can't isolate it. |
| Email provider | Google or Microsoft. |
| Browsers | Chrome or Edge, and the profile folder for this client (see [step 5](#5-point-links-at-the-clients-browser-profile)). |

reach then lists what it is about to do and waits for you to type `yes`. Nothing is written before that. After the
`yes` it writes the client's manifest and marker, the folder's git identity, the client's Claude settings, installs
the tentaqles plugin into the client's Claude config, and adds the activation block to your shell profiles if it
isn't there yet. [How it works](how-it-works.md) explains each of these.

reach runs only in your own terminal, not from inside a Claude Code session.

## 3. Open a terminal in the folder

Open a **new** terminal (the profile block loads when a shell starts), then:

```sh
cd ~/clients/acme
git config user.email          # the client's email
echo $CLAUDE_CONFIG_DIR        # ~/.tentaqles/identities/acme/claude
tentaqles doctor               # every check of this client
```

In PowerShell, `$env:CLAUDE_CONFIG_DIR` is the variable. A subfolder (`~/clients/acme/some-repo`) is acme's too. Leave
the folder (`cd ~`) and the variables go back to what they were.

## 4. Log in once per client

```sh
claude                         # log in with the client's Claude account
tentaqles login acme gh        # each CLI the client uses
tentaqles login acme az
```

Each login lands in the client's own folder under `~/.tentaqles/identities/acme/`, so it never touches your other
clients' logins. Logging in from a terminal in the client's folder (`gh auth login`, `az login`) does the same.

When `claude` starts in a client's folder it runs through `tentaqles run`, which checks that the tentaqles plugin is
enabled for the client. If reach could not install it, the refusal prints the two lines to run.

## 5. Point links at the client's browser profile

If you answered the browser question, links the terminal opens go to that profile. tentaqles needs the profile's
**folder name**, not the name you see in the browser:

- Chrome: open `chrome://version`, and take the last part of *Profile Path* (`Default`, `Profile 1`, ...).
- Edge: open `edge://version`, the same way.

To set it or change it later:

```sh
tentaqles add browser chrome "Profile 1" --client acme
```

Then `tentaqles open https://example.com` opens it in acme's profile, and so do `gh pr view --web` and the other
tools that open links through `BROWSER`. A link you click in the terminal window itself opens your default browser:
the terminal app hands it to the operating system, not to the shell.

## Optional: Using Codex

If the client uses the OpenAI Codex CLI, add it and log in once:

```sh
tentaqles add codex --client acme
tentaqles login acme codex
```

Codex gets its own home under the client's folder, with tentaqles' two hooks in its config. Start `codex` in the
client's folder as you start `claude`: `tentaqles run` refuses a Codex session whose hooks are missing or off.

## 6. Add the next client

Repeat step 2 with another folder: `tentaqles reach ~/clients/globex`. Two terminals can now be two clients at once,
each with its own logins, email, tools and language.

## Change or remove a client

```sh
tentaqles add cloud aws              # in the client's folder, or with --client acme
tentaqles add language pt-BR
tentaqles remove slack
tentaqles reach                      # the menu: providers, browser, email or language
tentaqles drop acme                  # undo the client completely, logins included
```

Each one lists the change and waits for a typed confirmation. After a change, open a new terminal in the folder (or
`cd` out and back in) and restart its Claude sessions.

## Next

- [How it works](how-it-works.md), to know what each piece does.
- [Commands](reference/commands.md), for every flag.
- [Troubleshooting](troubleshooting.md), when `tentaqles doctor` reports something.
