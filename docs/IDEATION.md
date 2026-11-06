Tentaqles is an identity manager for developers who work for multiple clients. It isolates each client's data and tools from the others by giving each client anisolated development environment. For each client, the identity holds:
- An isolated terminal with specific tools; a client that works with AWS only, does not get the Azure CLI installed or reachable through that terminal.
- Isolated logins and sessions for agentic Claude Code. Two companies the developer works for may each provide their own Claude Code subscription, so each identity terminal is logged in to its own Claude Code account.
- Isolated platform connections: git providers (GitHub, GitLab, Azure DevOps, Bitbucket), cloud providers (AWS, Azure, GCP), data platforms (Databricks, Snowflake), email providers(Google, Microsoft) and other usual tools for development stack.
- Isolated tech stack and preferences.
- Isolated Chrome and/or Edge profiles, so whenever a terminal or an agent interacts with a browser it opens the correct browser on the correct profile.

How it should work

A developer starts working with a new client. He create a folder with the client's name under his main client's folder. He opens a terminal, navigate to that folder and type a tentaqles command that initiates a tentaqles set up that makes that folde ran isolated identity, wich means:
- Any terminal initiated in that fodler or subfolder is it's own terminal. It has acess only to installed tools for that terminal identity, such as the clis installed and other packages. So for example, a client that works with AWS only, does not get the Azure CLI installed or reachable through that terminal.
- Starting claude code on a terminal initiated under that fodler will always be attached to the claude code account for that specific identity.
- running a cli trough that terminal or having Claude code run a cli call trough a bash command or powrshell command, will access the identity specific account for that CLI, so if for that client is registered an account for azure, every az command will be for that azure subscription registered. The same goes for:
  - github, gitlab, Azure DevOps, bitbucket or other git providers
  - Databricks, snowflake or other data platforms
  - Linear, Asana, Jira or or other task managemt provider
  - Slack, Teams, Discord, or other message provider 

The command should be "tentaqles reach" running from the terminal on the folder or, if I am in another folder, I should ebe able to run "tentaqles reach <path> and the setup will be done for that folder. The command initiate tentaqles and start with the banner:

`tentaqles reach`, MUST open with the banner below, in the style of the Hermes and Claude Code launch screens. Ship it as one text asset so the developer can replace it.

Default (UTF-8 terminals; 77 columns, fits an 80-column cmd.exe window):

```text
████████╗███████╗███╗   ██╗████████╗ █████╗  ██████╗ ██╗     ███████╗███████╗
╚══██╔══╝██╔════╝████╗  ██║╚══██╔══╝██╔══██╗██╔═══██╗██║     ██╔════╝██╔════╝
   ██║   █████╗  ██╔██╗ ██║   ██║   ███████║██║   ██║██║     █████╗  ███████╗
   ██║   ██╔══╝  ██║╚██╗██║   ██║   ██╔══██║██║▄▄ ██║██║     ██╔══╝  ╚════██║
   ██║   ███████╗██║ ╚████║   ██║   ██║  ██║╚██████╔╝███████╗███████╗███████║
   ╚═╝   ╚══════╝╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝ ╚══▀▀═╝ ╚══════╝╚══════╝╚══════╝

  tentaqles 0.1.0  ·  identity: dirtybird  ·  guard: on
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

- The status line under the banner MUST show version, identity name, and working directory. 
- Respect `--no-banner` and `TQ_NO_BANNER=1` for scripts and non-interactive mode.
- Colors are OPEN; propose one scheme and keep it in the same asset.

`tentaqles reach` will chack if that folder already has a tentaqles identity and depending on that, the menu options may differ. Among other possible options, it must have the  option, with will guide the developer trough a multistep form asking for what it needs with checkboxes, text inputs or single and multiple selections:
- The identity email: text input. (check if valid format)
- The git provider: single choice (GitHub, GitLab, Bitbucket, Azure DevOps), Other, Skip.
- Cloud providers: multiple choice (Azure, AWS, Google Cloud Platform, others, skip).
- Team and meeting platforms: multiple choice (Microsoft Teams, Slack, Discord, Zoom, Google
  Meet, others, skip).
- Data platforms: multiple choice (Databricks, Snowflake, Google Colab, others, skip).
- Task management provider: single choice (Asana, Linear, GitHub Projects, Azure DevOps,
  Trello, Jira, others, skip);
- Browser and profile: multiple choice (Google Chrome, Microsoft Edge, Skip)
- Anything else meaningful to the day-to-day developer workflow.

The system prepares the manifest and the other settings for the selections, installs the CLIs for the selected providers and their dependencies, then starts a new terminal session with the correct identity and closes the terminal where the setup was started.

The developer then starts Claude Code and is prompted to log in to the client's account. Inside Claude Code under the client's root
folder, the tentaqles plugin is already installed for that harness, or the developer installs it. An onboarding skill then drives the login to each configured provider by OAuth, token or MCP. The developer is then fully connected to the client's stack through the agent: messages,
email, task management, git, cloud, under that client's identity and no other.

Then, say the developer gets confused and asks an agent running for company X, which has an Azure subscription, to create a virtual machine in AWS. Claude Code should know that this identity runs on Azure, not AWS, and not call the AWS CLI, MCPs or tools. If during reasoning and research Claude finds those tools through another company's identity, a hook blocks it from using them, based on the settings. Mistakes are guarded
by deterministic scripts, routes and hooks, not by stochastic LLM reasoning alone.

AI agents must not be able to overwrite the hooks, the guards or the identities unless a human allows it.
