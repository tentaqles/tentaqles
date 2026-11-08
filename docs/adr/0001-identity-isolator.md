# ADR-0001: Tentaqles is an identity isolator

Status: accepted
Date: 2026-11-08

## Context

Tentaqles grew into an agent guard: a signed trust record per client, human-only commands detected through
the process tree, a policy layer that denied every tool call outside a client's provider scope (CLIs, variables,
credential paths, API hosts, MCP servers), secret rules, self-protection of its own files, a denial log, an
agent-run `connect` that registered hosted MCP servers, and an onboarding skill that was to connect every tool of a
client through the agent. Connecting tools that way turned out not to keep up with how often vendors change their
URLs and sign-in methods, and the guard needed constant work to stay both strict and livable.

The owner decided (2026-11-08) to keep what makes each client folder its own identity and drop the rest. The
earlier ADRs (0001 to 0021) described the removed machinery; they are in the git history.

## Decision

- **A client is a folder.** `tentaqles reach` makes it one: a marker, `.tentaqles.yaml`, names the client, and the
  manifest lives under `~/.tentaqles/identities/<client>/manifest.yaml`. A marker must name its own folder and the
  manifest's `client` must too, so a copied marker or manifest lends a folder nothing. The manifest holds names
  only; a secret-shaped value makes it fail to load.
- **The terminal carries the identity.** The shell hook sets, in the client's folder and every subfolder,
  `CLAUDE_CONFIG_DIR` (the client's own Claude Code login and subscription), each CLI's config-home variable from
  the provider catalog, `BROWSER` and the browser shims, and a PATH without the CLIs of providers outside the
  client's scope (dropped folders, or stubs in a mask folder), with those providers' variables unset. Leaving the
  folder restores the environment exactly. git takes the client's name and email from the folder's
  `.gitconfig-tentaqles` through `includeIf`.
- **reach is the form.** It asks for the client's tools and writes them to the manifest after a typed `yes`; in an
  existing client it changes the manifest through the same questions, editing only the keys the form owns. It
  installs the tentaqles plugin and the shell activation block. It refuses to run from inside Claude Code.
- **A small drift check stays.** The plugin's PreToolUse hook, for Bash and PowerShell only, runs `guard.Decide`:
  blocked commands, wrong cloud, a remote command outside every client folder, a stale shell, a wrong
  `CLAUDE_CONFIG_DIR`, a wrong git email, a wrong gh user. It fails closed within its deadlines. `tq run` refuses
  to start a Claude session without the plugin, and from inside a session runs only the folder's client.
- **Nothing is signed, nothing is human-only.** Without a trust record there is nothing for a human-only check to
  protect: the manifest and the client's files are the developer's own files.
- **One program, two names.** The release ships `tentaqles` and `tq`, built from the same package so the bytes
  match (the shims and mask stubs are copies of the binary).

## Consequences

- An agent can edit a client's manifest or settings; the change applies at the next prompt. Tentaqles prevents
  mistakes, not a determined program running as the developer (see `docs/threat-model.md`).
- Connecting MCP servers, connectors and tokens is the developer's job, per client, in the client's Claude config.
- An install of an earlier version keeps files tentaqles no longer reads (`trust/`, `denials.jsonl`, `bundles/`,
  `mcp-out-of-scope.json` and the like); they can be deleted. A client from an earlier version whose manifest still
  sits in its folder needs `tentaqles reach` again in a new folder, or its manifest moved by hand to
  `~/.tentaqles/identities/<client>/manifest.yaml`, leaving in the folder a `.tentaqles.yaml` that reads
  `schema: tentaqles-marker-v1` and `client: <client>`.
