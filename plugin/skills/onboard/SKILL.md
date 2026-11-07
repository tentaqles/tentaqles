---
name: onboard
description: Connects this client's tools one provider at a time - installs each CLI in scope, connects its MCP server, has you log in, verifies it, and ends with a table. Reads only tentaqles status --json. Use when the user says "onboard", "connect my tools", "log me in" or "is everything connected", and on a first session in a new client.
---

# Onboard

Connects this client's tools one provider at a time and verifies each one. The only view of the
client is `tentaqles status --json`.

Never read the workspace manifest, `.claude.json`, `.env` files, the environment or anything under `~/.tentaqles`: they are protected, and the status output says what you need.
If only `tq` is on PATH, use `tq` for `tentaqles`: it is the same program.

Rules for every step:
- A login runs under `!` only when it is a known browser flow: `! gh auth login --web`, `! az login` or `! acli jira auth login`, printed as its own line that starts with `!`. Every other login runs in the developer's own terminal, with no `!`, and you wait for them to say done. Never run a login yourself.
- `tq login` and `tq secret set` run in the developer's own terminal, with no `!`: tq refuses them inside a session.
- Install and connect only providers in `scope`. A provider outside it is refused: do not install, log in or connect it.
- Never choose an installer or download a file. Show every line `tentaqles install` prints.
- The hook judges every command. Never work around a refusal.

## 1. Read the client

Run `tentaqles status --json` and read the one object.

- `identity` null (a neutral folder) or `trusted` false (an untrusted client): this folder is not a trusted client. Say the `reason` as it is. Neutral: the developer runs `tentaqles reach` in their own terminal. Untrusted: the developer runs `tq hooks verify` in their own terminal and reads what it reports. Then stop. Do not walk the providers.
- Otherwise `scope` is what the client may use, and `providers` lists every provider in it. Walk them in that order.

## 2. Walk the providers, one at a time

For each provider, do its CLI step (a), connection (b) and credential (c), then the next provider. Before (a), read its definition with `tentaqles providers show <id>`. Say one line before each provider with its id and what is left. Skip what is in place: no `cli`, or a CLI that resolves; a `connection` that is `present` or `none`; a `credential` that is `present`. notion and trello are reached only through a claude.ai connector (tq has no catalog server for them), so their `connection` reads `none` and they always get step b. A re-run works the same way, so a CLI removed or a credential expired since the last run gets its step again.

A task provider (`category` `pm`) gets its install, connection and login like any other. Its only limit is on tasks: no reading or writing beyond its one read-only check in step 4.

### a. CLI

When `cli` is set and `cli_resolves` is false, run `tentaqles install <id>`. Show the developer every line it prints, `then:` lines included.
- `installed <version>` or `already present <version>`: a `then:` line is for the developer to run in their own terminal. Run `tentaqles status --json` and read `cli_resolves`: a session started before the install may not see the CLI yet. If it still reads false, tell the developer to restart Claude Code and run this skill again, and print no login for it this run.
- `manual, download from <url>`: give the developer that page with `tentaqles open <url>`, or print the URL if that refuses. Ask them to install it and say done, then check `cli_resolves`. If it still reads false, tell the developer to restart Claude Code or fix the install and run this skill again, and print no login for it this run.
- `install failed: <reason>`: show the reason. The developer fixes it or installs by hand, then says done; check `cli_resolves`. If it still reads false, tell the developer to fix the install and run this skill again, and print no login for it this run. The table says `missing`.

### b. Connection

When `connection` is `missing`, or the provider is notion or trello, run `tentaqles connect <id>`. Its line says:
- `connected as <name>`: the developer runs `/mcp` in this session, signs in to `<name>`, and says done. If the server is not listed, they restart Claude Code and run this skill again. Wait.
- `already connected`: nothing to do.
- `no MCP server in the catalog`: the reach is a claude.ai connector, which `status --json` cannot see. If step 4 names a read-only check for this provider and its tools are loaded, make it now; if it passes, the connector is connected. Otherwise give the exact step: run `tentaqles open <url>` for `https://claude.ai/settings/connectors`, or print that URL if it refuses; on claude.ai, add `<id>` under Connectors and sign in there; say done; then make the check again. Tools not loaded here read `not loaded in this session`.
- A refusal (outside `scope`, untrusted, neutral): show it as it is and go on. A warning on stderr: relay it.

### c. Credential

Skip this when `credential` is `present`, except a shared login (below). When `credential` is `none` and `cli` is null, there is no CLI login to ask about: skip. When it is `none` and `cli` is set, tq cannot tell a CLI login from logged out: ask the developer once whether they are signed in to that CLI, and print its login only if they say no. Print no login for a CLI that did not resolve in (a).

**Shared login.** A provider whose `cli` is set and whose `identity` in `tentaqles providers show` has no `env` entries keeps one login per machine (jira is one). It is not isolated per client, and the guard cannot check it. Even when `credential` is `present` or `verified` is true, ask the developer to confirm that the signed-in account is this client's: a passing check does not prove it. Before its login, say that signing in here replaces any other client's login. A provider with no `cli` (linear, asana, trello) is not a shared login: its token is per client.

When `credential` is `missing`, verify first, and ask only if that fails:
- If `tentaqles providers show <id>` has a `verify` block, `status --json` already ran it and it failed: go to the login.
- Otherwise, if step 4 names a read-only check for this provider and its tools are loaded, make it now. If it passes, the provider is signed in: skip the login and the token.
- Otherwise ask the developer once whether they are already signed in through its own tool. If so, skip.

When a login is still needed, use the first method the provider supports, in this order:
1. **OAuth**: `providers show` has a `login` block. A known browser flow from the rules is printed under `!`, as for gh:
   ```text
   ! gh auth login --web
   ```
   Any other login is printed for the developer's own terminal, with no `!`: `tq login <identity> <id>` when `providers show` has an identity `env` (the login then stays in the client's private config; for aws, `--sso` for a first SSO setup or `--sso-session <name>` for an SSO client), otherwise the catalog's `<cli.command> <login.args>`.
2. **Token**: the provider is a tracker (`github`, `azure-devops`, `jira`, `linear` or `asana`). Tell the developer to run `tq secret set <identity> <name>` in their own terminal, with no `!`. `<name>` is the provider id, except `gh` is `github`. Ask them to say done.
3. **MCP**: neither applies. Its sign-in is the connector in step b.

With no `login` block and no tracker token, the catalog names no login: print no command. Tell the developer what the provider's `notes` in `providers show` say about how it signs in. If a connector reaches it, step b is its sign-in. Otherwise the table reads `missing`, and you go on.

After the developer says done, run `tentaqles status --json` and read `credential`. Still `missing`: say so once and go on.

### d. Other providers

For each name in `other_providers`, help the developer install it and log in the way the vendor documents: the login in the developer's own terminal (`!` only for a known browser flow from the rules), and the vendor's page opened with `tentaqles open <url>`, or printed if that refuses. Never choose an installer yourself. Say: "This login is not isolated per client, and the guard cannot check it." Do not run `tentaqles install`, `tentaqles connect` or `tq secret set` for it: it is outside `scope`, and those refuse. Its table row reads `not checked`.

## 3. Tokens

Never ask for a token, an API key or a password, and never repeat or store one in a file, a memory or the transcript. If a token is pasted into the chat, say it should not be shared here and do not use it. If the provider is a tracker, tell the developer to run `tq secret set <identity> <name>` in their own terminal instead. Otherwise point them to the provider's own login or connector, and never to `tq secret set`.

## 4. Verify

When the developer says the steps are done, or at the end, run `tentaqles status --json` once more. `verified` true means connected.

For each provider whose check is named here and whose tools are loaded in this session, make one read-only call, and nothing else: gh (GitHub connector) lists one repository of this account; jira (Atlassian connector) lists Atlassian accessible resources; linear lists Linear teams; asana lists Asana workspaces; notion searches Notion for one page; trello lists one Trello board; slack lists one Slack channel; teams lists one Teams team; gmail lists Gmail labels; outlook lists one Outlook folder. discord, zoom and google-meet have no named call: they read `not checked`. Never send, post, create, edit or delete. A task provider gets only its one call. A connector not loaded here reads `not loaded in this session`, not a guess. Verified is `yes` when `verified` is true or the named call passed in this run.

## 5. The table

End with one table: one row per provider in `providers`, then one per `other_providers` name. Then stop.

| Provider | CLI | Credential | Connections | Verified |
|---|---|---|---|---|
| gh | present | present | present | yes |

CLI is `present`, `missing` or `none` (no CLI). Credential and Connections are the last `status --json` values, except that a connector whose named check passed in this run reads `present` in Connections (and in Credential, where that check settled the credential). Verified is `yes`, `no`, `not loaded in this session`, or `not checked`.
