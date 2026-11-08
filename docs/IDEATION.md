# Tentaqles: what it is for

Tentaqles is an identity manager for developers who work for several clients. It gives each client an isolated
development environment, tied to a folder. For each client, the identity holds:

- an isolated terminal, with the client's tools only: a client that works with AWS only does not get the Azure CLI
  reachable through that terminal;
- its own Claude Code login: two companies may each provide their own Claude Code subscription, so each client's
  terminal starts Claude Code logged in to that client's account;
- its own logins for the platforms the client uses: git hosts (GitHub, GitLab, Azure DevOps, Bitbucket), clouds
  (AWS, Azure, GCP), data platforms (Databricks, Snowflake) and the other CLIs of a development stack;
- its own git name and email;
- its own Chrome or Edge profile, so a link a terminal or an agent opens lands in the right browser profile.

## How it works

The developer starts working with a new client. They create a folder with the client's name under their clients
folder, open a terminal there, and run `tentaqles reach` (or `tentaqles reach <path>` from anywhere). That makes the
folder an isolated identity:

- Any terminal started in that folder, or in a subfolder, is the client's terminal. It reaches only the client's
  tools: the CLIs of providers the client doesn't use are not on its PATH, and their variables are unset.
- Claude Code started in that terminal uses the client's own Claude config home, and so the client's account.
- A CLI run in that terminal, by the developer or by Claude Code, uses the client's own login for it: every `az`
  command runs against the client's Azure subscription, every `gh` command as the client's GitHub user. The same
  goes for the other git hosts, the clouds and the data platforms.
- git commits there with the client's name and email.

## `tentaqles reach`

`tentaqles reach` opens with the banner below, as one text asset the developer can replace.

Default (UTF-8 terminals; 77 columns, fits an 80-column cmd.exe window):

```text
████████╗███████╗███╗   ██╗████████╗ █████╗  ██████╗ ██╗     ███████╗███████╗
╚══██╔══╝██╔════╝████╗  ██║╚══██╔══╝██╔══██╗██╔═══██╗██║     ██╔════╝██╔════╝
   ██║   █████╗  ██╔██╗ ██║   ██║   ███████║██║   ██║██║     █████╗  ███████╗
   ██║   ██╔══╝  ██║╚██╗██║   ██║   ██╔══██║██║▄▄ ██║██║     ██╔══╝  ╚════██║
   ██║   ███████╗██║ ╚████║   ██║   ██║  ██║╚██████╔╝███████╗███████╗███████║
   ╚═╝   ╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝ ╚══▀▀═╝ ╚══════╝╚══════╝╚══════╝

  tentaqles 0.12.0  ·  identity: dirtybird
  C:\clients\dirtybird
```

Fallback (terminal reports no UTF-8):

```text
  ______           __              __
 /_  __/__  ____  / /_____ _____ _/ /__  _____
  / / / _ \/ __ \/ __/ __ `/ __ `/ / _ \/ ___/
 / / /  __/ / / / /_/ /_/ / /_/ / /  __(__  )
/_/  \___/_/ /_/\__/\__,_/\__, /_/\___/____/
                            /_/
```

- The status line under the banner shows the version, the identity name and the working directory.
- `--no-banner` and `TQ_NO_BANNER=1` turn it off for scripts and non-interactive use.

For a new client, `reach` asks, with checkboxes, text inputs and single or multiple choices:

- the identity email (checked for a valid format);
- the language for the client: Claude writes the client's reports, documents, emails and messages to clients and
  teammates in it. A list starting on English (United States), then every other language by name, filtered as the
  developer types (`por` leaves the Portuguese ones) and moved through with the arrows;
- the git provider: GitHub, GitLab, Bitbucket, Azure DevOps, other, skip;
- the cloud providers: Azure, AWS, Google Cloud, other, skip;
- the team and meeting platforms: Microsoft Teams, Slack, Discord, Zoom, Google Meet, other, skip;
- the data platforms: Databricks, Snowflake, Google Colab, other, skip;
- the task management provider: Asana, Linear, GitHub Projects, Azure DevOps, Trello, Jira, other, skip;
- the email provider: Google, Microsoft, other, skip;
- the browsers and the profile for each: Google Chrome, Microsoft Edge, skip.

The answers go to the client's manifest: the list of tools the developer set for that identity. After the
developer types `yes`, `reach` writes the manifest and the folder's git identity, prepares the client's Claude
settings, PATH and browser profile, installs the tentaqles plugin into the client's Claude config, and adds the
shell activation to the developer's shell profiles.

In a folder that already is a client, `reach` shows the client and offers to change its providers, its browser and
profile, its email or its language, through the same questions.

## Claude Code

The developer opens a new terminal in the client's folder and starts Claude Code, which asks them to log in to the
client's account the first time. From then on, every session started under that folder is that client's.

The tentaqles plugin adds one small check: before Claude Code runs a shell command, the plugin compares it with the
folder's client and blocks a `git`, `gh` or cloud command that would run under the wrong identity (another git
email, another GitHub user, a cloud the client doesn't use), with the reason. It reads the command's text only:
it prevents mistakes, it is not a sandbox.
