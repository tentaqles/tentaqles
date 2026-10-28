---
name: onboard
description: Connect every company tool of this client workspace and verify it - the agent and cloud CLIs, the git host, the tracker the hub mirrors, and the chat and email MCP connectors. Reads the workspace manifest, prints each login for the user to run in their own terminal (tq login <ws> <id>, tq secret set <ws> <provider>, acli jira auth login), guides /mcp for connectors, then verifies with tq doctor --json and read-only tool calls. It never handles tokens and never runs a login itself. Ends with a table of what is connected. Use when the user says "onboard", "connect my tools", "set up this client", "log me in", "is everything connected", or "/tentaqles:onboard".
argument-hint: '[check]'
---

# Onboard

Run inside a client folder that `tq onboard` (or `tq add`) created. The manifest
(`.tentaqles.yaml`, names only) says which tools this company uses. This skill walks
through each one: it prints the login for the user, waits, then verifies. The logins
are the user's. Never run `tq login`, `tq allow`, `tq secret set`, `tq bundle sync`
or a CLI's own login (`az login`, `gh auth login`, `acli jira auth login`): they are
human steps, several are refused from an agent session, and a login opens a browser
or a hidden prompt that only the user can answer.

**Tokens.** Never ask for a token, an API key or a password, and never repeat or store
one. If the user pastes one into the chat, say that it should not be shared here, do
not use it, and point them at `tq secret set <ws> <provider>`, which reads it from a
hidden prompt into the OS keychain.

`$ARGUMENTS` is empty (walk through everything) or `check` (skip to step 3 and the table).

## 1. Find the workspace and read the manifest

```bash
printf 'ws=%s\nroot=%s\n' "${TQ_WS:-}" "${TQ_WS_ROOT:-}"
```

- Both set: read `<root>/.tentaqles.yaml` with the Read tool.
- Empty: look for `.tentaqles.yaml` in this folder and its parents. None found: say
  "This folder is not a tq workspace. Run `tq onboard` in your own terminal first."
  and stop.
- The manifest is not trusted yet (doctor says `untrusted` in step 3, or tq printed
  `untrusted` when it created it): the first line the user runs is `tq allow <ws>`.

From the manifest, list what to connect: `identities`, `git.provider`, `cloud`
(`provider` and `also`), `data_platform`, `agents`, `tracker`, `chat`, `email`.
A block that is missing was skipped; leave it out of the walk-through and show it as
"skipped" in the table.

## 2. Offer each login

Print the lines as one numbered list for the user to run in their own terminal
(PowerShell, Windows Terminal, or a macOS/Linux terminal), then ask them to say when
they are done. Do not run any of them.

- **CLIs** (every identity, the cloud CLIs, the data platform CLI, the agent CLIs):
  `tq login <ws> <id>` for each one, for example `tq login acme claude`.
- **Tracker** (`tracker.provider`), by the fallback order cli, then keychain, then env:
  - cli: GitHub uses `tq login <ws> gh`. Azure DevOps runs `az login`: use
    `tq login <ws> azure-devops` when `identities` lists `azure-devops`, else
    `tq login <ws> az` (the same CLI; doctor's fix line names the right one). Jira uses
    `acli jira auth login` (acli keeps one global login, not one per workspace). Linear
    and Asana have no CLI login.
  - keychain: `tq secret set <ws> <provider>`, which reads the token from a hidden
    prompt. `tq secret status` shows what is stored, never the value.
  - env (`tracker.env` names the variable): the user puts the token where `tq dotenv`
    resolves that variable. Never read or print a `.env` file.
  - mcp: no background sync; `/tentaqles:sync` does the work from a Claude session.
- **MCP connectors** (chat: Slack, Teams; email: Gmail, Outlook; and the tracker
  connectors Linear, Asana, Atlassian, Notion when the tracker is MCP-only):
  1. Whether the server is configured comes from doctor's names-only findings in
     step 3 (`chat-ok` / `chat-mcp-missing`, `email-ok` / `email-mcp-missing`), and for
     the tracker connectors from the read-only call in step 3. Do not list or open the
     tq bundle catalog or any `.claude.json` to find out: their MCP entries can carry
     connector tokens in `env`.
  2. Missing: tell the user to add it to the tq bundle catalog and to
     `claude.bundle.mcp` in the manifest, then run `tq allow <ws>` and
     `tq bundle sync <ws>` themselves (doctor's fix line names the entry).
  3. Then authenticate it with `/mcp` in this Claude session (or connect it on
     claude.ai). A restart of Claude Code may be needed before a new server shows up.

## 3. Verify

When the user says they are done (or for `check`):

```bash
tq doctor --json 2>&1; echo "doctor_exit=$?"
```

Read the findings for this workspace (`workspace` equal to `<ws>`, plus the global ones):

- `untrusted`: `tq allow <ws>` first.
- `identity-logged-out`, `claude-not-logged-in`: that login did not take; repeat its line.
- `identity-wrong-account`: signed in as someone else; the fix line says who to sign in as.
- `cli-missing`: install it with the fix line, then log in.
- `tracker-ok`: the tracker is connected; it names the account and the scope.
- `tracker-needs-session`: no background credential; offer the keychain line, or
  `/tentaqles:sync` for MCP-only.
- `tracker-no-adapter`: this tq build cannot sync that tracker yet; `/tentaqles:sync`.
- `tracker-auth-failed`: the message names the source that failed; repeat that login.
- `chat-ok`, `email-ok`: the MCP server is configured.
- `chat-mcp-missing`, `email-mcp-missing`: the entry is not in the identity's
  `.claude.json`; fine if the user connected it on claude.ai, otherwise step 2.

Then, for each MCP connector whose tools are available in this session, make one
read-only call to prove it works: list one Slack channel, list one Teams team, list
Gmail labels, list one Outlook folder, list Linear teams, list Asana workspaces, list
Atlassian accessible resources, search Notion for one page. Never send, post, create,
edit or delete anything. A tool that is not available: say "not loaded in this
session" in the table rather than guessing.

## 4. The table

End with one table, one row per service in the manifest (skipped blocks too):

| Service | Provider | Connected through | Status | Next step |
|---|---|---|---|---|
| Claude | claude | tq login | connected (tester@acme.test) | - |
| Tracker | jira | keychain | not connected | tq secret set acme jira |
| Chat | slack | MCP | connected (read-only call ok) | - |
| Email | - | - | skipped | - |

Status is one of: connected, not connected, not loaded in this session, skipped. The
next step is the exact line the user runs. Then stop.

## Never

- ask for, read, repeat or store a token, key or password
- run `tq login`, `tq allow`, `tq secret set`, `tq bundle sync` or any CLI login
- read or print `.env` files or the whole environment
- Read, cat or list the tq bundle catalog file or any `.claude.json` (MCP entries can
  hold connector tokens in `env`); presence comes from doctor's findings
- send, post or change anything through a connector while verifying
- edit the manifest yourself; a change needs `tq allow`, which is the user's
