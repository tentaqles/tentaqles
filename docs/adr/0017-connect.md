# ADR-0017: `tentaqles connect`, the one agent-run write of the Claude config

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md` (the spec), ADR-0005 (self-protection), ADR-0007 (provider scope), ADR-0008 (MCP scope), ADR-0012 (reach)

## Context

IDEATION has an onboarding skill that "drives the login to each configured provider by OAuth, token or MCP".
For MCP it must register a provider's server in the client's Claude config. The skill runs in the agent's
session, which can't read the manifest and can't write `.claude.json`. Every other tq command that
writes under an identity dir is human-only (ADR-0003), and a human-only command can't serve the skill.

The skill also needs the client's setup (scope, providers, what is already connected). It can't read the
manifest or `.claude.json` either, so it needs a read command that carries no secret.

## Decision

- `tentaqles status --json` prints one JSON object and writes nothing: `identity`, `root`, `scope`, one
  entry per provider in scope (`id`, `category`, `cli`, `cli_resolves`, `credential`, `connection`,
  `verified`), `browsers` and `other_providers`. `credential` and `connection` are `present`, `missing`
  or `none`, by presence and server name only; no token, environment value or MCP `env` value is read
  into it. `verified` is true only when the provider's read-only `verify` command passed in this run,
  with a 5 s limit each, and a keychain lookup has the same 5 s limit (a timeout reads `missing`). An
  identity with no `claude` identity has no Claude config: its `connection` reads `missing` and no config
  file is read. The connect entry is taken from the embedded catalog, so a user override of a provider
  does not hide a connection. A neutral folder gives `identity: null`; an untrusted client gives `trusted:
  false` and a `reason` and no provider data. `tq doctor --json` is unchanged and stays the health report;
  `tq status --json` used to print it and no longer does.
- `tentaqles connect <provider>` is **not** human-only. It is narrow instead:
  - it takes the provider and nothing else: no flag, no second argument;
  - the entry is the embedded catalog's `connect:` block (`name`, `transport`, `url` or `command` and
    `args`; no `env`, no header). A user provider file that carries `connect:` is not loaded, and the
    command reads the embedded catalog only, so neither a caller nor a user file chooses the entry;
  - the provider must be in the identity's scope (`Manifest.Scope`, the same one the hook judges by);
    outside it, `connect` exits 1 with `<provider> is outside the scope of identity <ws>: <scope>` and
    writes a line in the denial log, category `identity`;
  - a neutral folder, an untrusted client, or an identity with no `claude` identity is refused;
  - it adds one member to the top-level `mcpServers` object, editing the file as bytes so every other
    key, number and indent stays, under a lock file and an atomic rename; a file that is not JSON, or
    whose `mcpServers` is not an object, is left alone and named;
  - a server already there (by name) is `already connected` and nothing is written;
  - Claude Code rewrites `.claude.json` while it runs and does not honour connect's lock. connect reads
    the file again just before the rename and makes its edit again if it changed, and when a session
    looks open (`bundle.InUse`, as for `tq settings render`) it prints a warning on stderr, apart from the
    success line, to run `tentaqles status --json` after restarting;
  - each change is one line in `$TQ_HOME/connections.jsonl`: time, identity, provider, server name,
    caller (`human` or `harness`) and `outcome`, never a value of the entry. The line is written
    immediately before the file is replaced, after the last check that the file did not change, so a
    run that changes nothing (already there, a file that never settles, a failed read) writes none. If
    the replacement then fails, a second line for the same change says `failed`: a `connected` line
    with no `failed` line after it is the change. The replacement itself is one rename (atomic on the
    same folder). A crash between the line and the rename leaves a `connected` line with no change:
    `status --json` then reads the connection as missing, and running `connect` again makes the change
    and writes its line.
- `connect` re-signs nothing: `.claude.json` is not in the integrity record, and the entry can only be the
  catalog's.
- **The hook's cross-client rule narrows (rounds 4 and 5).** `tq/cross-client-mcp` denied a server that
  another client's bundle lists, even when this identity's own scope owns it. The exception is the entry
  `connect` writes, and only that entry: a server is exempt when every entry of its name the harness loads
  (`mcpscope.Entries`: the identity's `.claude.json`, and the `.mcp.json` files from the cwd up to the root)
  equals the embedded catalog's `connect:` entry for a provider in this identity's scope (`connectExempt`,
  reading the embedded catalog only, read-only). Only when another client's bundle would otherwise deny it
  is the check made. A same-named server with any other entry (for example a `.mcp.json` in the client's
  root that points at another host), a server whose only entries are unlike the catalog's, a name no in-scope
  provider owns (including one in another client's bundle), a provider's name when that provider is out of
  scope, and any server when a config file the harness reads cannot be parsed, stay denied as before. The
  check adds no matching beyond this comparison; it is a speed bump (AR2). Tests:
  `TestHook_ConnectedServerNotCrossClientDenied` (all four catalog entries),
  `TestHook_OtherClientsBundleServerStaysDenied`, `TestHook_OutOfScopeProviderNameStaysDenied`,
  `TestHook_ForeignSameNameInProjectMCPJSONDenied`, `TestHook_ForeignBesideCatalogEntryDenied`,
  `TestHook_OnlyCatalogEntryPasses`, `TestHook_UnreadableConfigNoExemption`.
- **A recursive search from a protected folder (round 3, `internal/policy/selfprotect.go`).** A recursive
  search (`grep -r`, `rg`, `findstr /s`, `Get-ChildItem -Recurse`) whose run folder (the cwd, or the folder a
  `cd` went to) is a read-denied protected folder is denied as `tq/protected-path`, whatever root it names,
  including no root. The folder itself is protected (`backups/` under the identity's Claude home, as
  above), so a search or a copy of it is denied, while a file beside it stays readable.
- The identity's `.claude.json` and every other copy of its MCP entries become **protected paths**,
  `KindState`: `<identity>/claude/.claude.json*` (the file, its lock and temporary files, Claude's
  `.claude.json.backup`), the folder `<identity>/claude/backups/` and its `.claude.json*` files (Claude
  Code's timestamped backups: the folder itself is protected, so a search or a copy of it is denied, and
  a file beside them, which is not an MCP entry, is not) and `<identity>/claude/mcp-out-of-scope.json*` (the entries `tq settings render` removes,
  values included, and its `.corrupt-<time>` files). No file tool, MCP input or shell redirect reads
  or writes them (`tq/protected-path`); `projects/`, `memory/`, `todos/` and the rest of the Claude
  home stay writable. Before this ADR it fell under
  "the harness's own working data" and was writable by a session, which would have made `connect`'s limits
  moot: an agent could write any server by hand. `connect` runs as a process, not as a tool call, and its
  command line names no path, so the hook allows it. Claude Code reads and writes the file itself and is
  not a tool call either.
- The four providers whose vendors document a hosted MCP server (`gh`, `linear`, `asana`, `jira`) carry a
  `connect:` entry, each with the vendor's documentation URL beside it. More are data.

## Consequences

- An agent that is prompted to connect an out-of-scope provider is refused and logged. An agent that is
  prompted to connect an in-scope one gets the catalog's server, which is the intended path. The entry's URL
  is the vendor's hosted server for a provider the human already put in the scope; the server still needs
  the human's `/mcp` sign-in, so connecting it grants no access by itself.
- A session can no longer read the identity's `.claude.json`, where MCP servers may keep headers. The
  skill reads `status --json` instead.
- A user who edits `.claude.json` by hand, or runs `claude mcp add`, is a human or Claude Code, not a tool
  call, and is unaffected. An agent that runs `claude mcp add` in the shell is not stopped by this rule
  (AR2); the MCP scope rules still judge the server's tools by name and entry (ADR-0008).
- A vendor URL can go stale; whether it still answers isn't checked at build time.

## Alternatives considered

| Option | Why not |
|---|---|
| Make `connect` human-only | The skill could not register a server; the developer would copy a command by hand for each provider |
| Let the caller pass a URL or command | Any prompt-injected agent could register an attacker's server |
| Let a user provider file define `connect:` | A file outside the signed release would choose what an agent-run command writes |
| Re-encode `.claude.json` with Go's JSON encoder | Reorders keys and changes indentation in a file Claude Code owns (see ADR-0008) |
