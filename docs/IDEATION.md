The product is an identity manager for developers who work for multiple companies or
clients. It isolates each client's data and tools from the others by giving each client an
isolated development environment. For each company or client, the identity holds:
- An isolated terminal (PowerShell, pwsh, Git Bash and others) with specific tools; a client
  that works with AWS only does not get the Azure CLI installed or reachable through that
  terminal.
- Isolated logins and sessions for agentic AIs (Claude Code, OpenAI Codex, OpenCode, Kimi
  Code and others). Two companies the developer works for may each provide their own Claude
  Code subscription, so each identity terminal is logged in to its own Claude Code account.
- Isolated platform connections: git providers (GitHub, GitLab, Azure DevOps, Bitbucket),
  cloud providers (AWS, Azure, GCP), data platforms (Databricks, Snowflake), email providers
  (Google, Microsoft) and the rest.
- Isolated tech stack and preferences.
- Isolated Chrome or Edge profiles, so whenever a terminal or an agent interacts with a
  browser it opens the correct profile.

How it should work

A developer starts working with a new client. They create a folder with the client's name
under the main clients folder, open a terminal and type a tentaqles command that makes that
folder run with its own terminal, Claude Code account, Codex account, GitHub CLI, Azure CLI,
AWS CLI and every other identity-bound CLI and tool. The command (`tq init` and `tq add`)
asks for what it needs with checkboxes, text inputs or single and multiple selections:
- The identity email: text input.
- The git provider: single choice (GitHub, GitLab, Bitbucket, Azure DevOps), Other, Skip.
- Cloud providers: multiple choice (Azure, AWS, Google Cloud Platform, others, skip).
- Team and meeting platforms: multiple choice (Microsoft Teams, Slack, Discord, Zoom, Google
  Meet, others, skip).
- Data platforms: multiple choice (Databricks, Snowflake, Google Colab, others, skip).
- AI harness providers: multiple choice (Claude Code, OpenAI Codex, Kimi Code, OpenCode,
  others, skip).
- Task management provider: single choice (Asana, Linear, GitHub Projects, Azure DevOps,
  Trello, Jira, others, skip); only its login is managed, nothing is mirrored.
- Browser and profile (Google Chrome, Microsoft Edge, Safari, Opera and others).
- Anything else meaningful to the day-to-day developer workflow.

The system prepares the manifest and the other settings for the selections, installs the
CLIs for the selected providers and their dependencies, then starts a new terminal session
with the correct identity and closes the terminal where the setup was started.

The developer then starts the AI harness of choice, Claude Code for example, and is
prompted to log in to the client's account. Inside Claude Code under the client's root
folder, the tentaqles plugin is already installed for that harness, or the developer
installs it. An onboarding skill then drives the login to each configured provider: Slack
MCP, Linear, Asana, Azure DevOps, Azure CLI, GitHub CLI and so on, by OAuth, token or MCP.
The developer is then fully connected to the client's stack through the agent: messages,
email, task management, git, cloud, under that client's identity and no other.

Decision of 2026-11-03

The board, the headless orchestrators, the review page, the packs, the build gate and the
guard modes that this note once described were built (v0.8.2) and then removed: their
confirmations and blocks made daily work slower than the protection was worth. From
v0.9.0 tentaqles is the identity isolation described above, plus secrets protection (no
`.env` reads, no environment dumps, no secrets in commits) and the plugin's memory,
preamble and skills. An agent session runs with no other confirmation from tentaqles; a
command under the wrong identity is blocked with the reason, and that is all. The
requirements live in `docs/prd.md`; v0.8.2 stays as a tag for the removed features.
