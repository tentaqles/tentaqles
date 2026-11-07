---
name: onboard
description: Connects this client's tools one provider at a time - installs each CLI in scope, has you log in, connects its MCP server, verifies it, and ends with a table. Reads only tentaqles status --json. Use when the user says "onboard", "connect my tools", "log me in" or "is everything connected", and on a first session in a new client.
---

# Onboard

Connects this client's tools one provider at a time and verifies each one. The only view of the
client is `tentaqles status --json`.

Never read the workspace manifest, `.claude.json` or anything under `~/.tentaqles`. They are
protected, and the status output says what you need. If only `tq` is on PATH, use `tq` for
`tentaqles`: it is the same program.

Rules for every step:
- A login is the developer's. Print it as its own line that starts with `!`, for them to run in
  this session, then wait for them to say done. Never run a login yourself.
- Install and connect only providers in `scope`. A provider outside it is refused: do not install,
  log in or connect it.
- Never choose an installer and never download a file. Print only what `tentaqles install` prints.
- The hook judges every command. Never work around a refusal.

## 1. Read the client

Run `tentaqles status --json` and read the one object.

- `identity` is null (a neutral folder), or `trusted` is false (an untrusted client): this folder is
  not a trusted client. Say the `reason` from the output as it is. Neutral: the developer runs
  `tentaqles reach` in their own terminal to set the folder up. Untrusted: the developer runs
  `tq hooks verify` in their own terminal and reads what it reports. Then stop. Do not walk the
  providers.
- Otherwise the client is `identity`, `scope` is what it may use, and `providers` lists every
  provider in `scope`, in order. Walk them in that order.

## 2. Walk the providers, one at a time

Finish one provider before the next: CLI, credential, connection. Say one line before each provider
with its id and what is left. Skip what is already in place: a provider whose CLI resolves (or has
no `cli`), whose `credential` is `present` and whose `connection` is `present` needs no step. A
`connection` of `none` means no MCP server reaches the provider. A re-run works the same way, so a
CLI removed or a credential expired since the last run gets its step again.

A task provider (`category` `pm`) gets only its login and its connection, and the one read-only
check in step 4.

### a. CLI

When `cli` is set and `cli_resolves` is false, run `tentaqles install <id>`. Its one line says:
- `installed <version>` or `already present <version>`: go on.
- `manual, download from <url>`: give the developer that page. If the URL is http or https, also run
  `tentaqles open <url>`; if that refuses, print the URL instead. Ask them to install it and say
  done, then run `tentaqles status --json` and read this provider's `cli_resolves`.
- `install failed: <reason>`: show the reason. Ask the developer to fix it or install it by hand,
  and to say done; then check `cli_resolves` as above. If it still reads false, the table says
  `missing` and you go on.

### b. Credential

Skip this when `credential` is `present`. When it is `none`, tq cannot tell a CLI login from
logged out: ask the developer once whether they are signed in to that CLI, and print its login only
if they say no. When it is `missing`, use the first method the provider supports, in this order:

1. **OAuth**: the provider has a `cli` with a sign-in command. Print that login as a line starting
   with `!`, and wait for the developer. For example:
   ```text
   ! gh auth login --web
   ```
   Other examples: `! az login` for Azure, `! acli jira auth login` for Jira. For a CLI not named
   here, run `<cli> --help` and print the sign-in subcommand it names. A CLI with no sign-in goes to
   the next method.
2. **Token**: the provider is a tracker (`github`, `azure-devops`, `jira`, `linear` or `asana`).
   Print `! tq secret set <identity> <name>`, where `<name>` is the provider id, except that `gh` is
   `github`. tq reads the token from a hidden prompt into the OS keychain. Wait for the developer.
3. **MCP**: neither applies. The sign-in is the connector in step c.

After the developer says done, run `tentaqles status --json` and read `credential`. If it is still
`missing`, say so once and go on.

### c. Connection

When `connection` is `missing`, run `tentaqles connect <id>`. Its line says:
- `connected as <name>`: the server is in the client's Claude config. Tell the developer to run
  `/mcp` in this session, sign in to `<name>`, and say done. If it is not listed, they restart
  Claude Code and run this skill again. Wait.
- `already connected`: nothing to do.
- `no MCP server in the catalog`: the step is a claude.ai connector. Give the developer the exact
  step: on claude.ai, open Settings, then Connectors, add `<id>` and sign in there, and say done.
  Step 4 checks it afterwards, since `status --json` cannot see a claude.ai connector.
- A refusal (outside `scope`, untrusted, neutral): show it as it is and go on.
- A warning on stderr: relay it.

### d. Other providers

For each name in `other_providers`, help the developer install it and log in the way the vendor
documents: `!` lines and the vendor's page, never an installer chosen by you. Say: "This login is
not isolated per client, and the guard cannot check it." Do not run `tentaqles install`,
`tentaqles connect` or `tq secret set` for it: it is outside `scope`, and those refuse. Its table
row reads `not checked`.

## 3. Tokens

- Never ask for a token, an API key or a password. Never repeat one, and never store one in a file,
  a memory or the transcript.
- If a token is pasted into the chat: say it should not be shared here, do not use it, and print the
  `! tq secret set <identity> <name>` line for the developer to run instead.

## 4. Verify

When the developer says the steps are done, or at the end, run `tentaqles status --json` once more.
- `verified` true: connected.
- `verified` false: say what is still missing, once, and go on.
- For each MCP connector whose tools are loaded in this session (chat, email, or a tracker's
  connector), make one read-only call: list one Slack channel, list Gmail labels, list Linear teams.
  A task provider gets only this one call. Never send, post, create, edit or delete. A connector
  not loaded here is "not loaded in this session", not a guess.

## 5. The table

End with one table: one row per provider in `providers`, then one row per `other_providers` name.

| Provider | CLI | Credential | Connections | Verified |
|---|---|---|---|---|
| gh | present | present | present | yes |

CLI is `present`, `missing` or `none` (no CLI). Credential and Connections are the last
`status --json` values (`present`, `missing` or `none`). Verified is `yes`, `no`, `not loaded in this
session`, or `not checked` for an other provider. Then stop.

## Never

- ask for, read, repeat or store a token, key or password
- run a login yourself, change trust, or run `tq secret set` yourself
- read or print `.env` files, or the environment
- read the manifest, the Claude config or the tq state folder
- install, log in or connect a provider outside `scope`, or choose an installer
- send, post, create, edit or delete anything through a connector
