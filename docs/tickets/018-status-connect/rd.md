# 018: `status --json` for the skill, and `tentaqles connect <provider>`

**Status:** landed
**Risk:** High: `connect` is a write an agent may call, so a bug lets an agent add an out-of-scope or attacker-chosen MCP server to a client
**UI:** none

IDEATION: an onboarding skill "drives the login to each configured provider by OAuth, token or MCP". The skill can't read the manifest or write Claude's config, which are protected. It reads the client's setup through `status --json`, and registers a provider's MCP server through `connect`, which writes only what the embedded catalog defines. `docs/IDEATION.md` is the spec.

## Requirements

- **REQ-1**: `tentaqles status --json` must print one JSON object with `identity`, `root`, `scope`, `providers`, `browsers` and `other_providers`, and write nothing.
- **REQ-2**: `providers` must have one entry per provider in scope, with `id`, `category`, `cli`, `cli_resolves`, `credential`, `connection` and `verified`, where `credential` and `connection` are `present`, `missing` or `none`, and `verified` is true or false.
- **REQ-3**: In a neutral folder, `identity` must be null and the exit 0.
- **REQ-4**: In an untrusted client, the object must hold `identity`, `trusted` of false and `reason`, and no provider data.
- **REQ-5**: `status --json` must carry no token, credential value, environment value, or MCP `env` value.
- **REQ-6**: `verified` must be true only when the provider's `verify` read-only command passed in this run, with a 5 s limit each.
- **REQ-7**: `tq doctor --json` must keep its current output.
- **REQ-8**: A catalog entry may have `connect:` with `name`, and `transport` of `http` or `stdio`, with `url` for http and `command` plus `args` for stdio. `gh`, `linear`, `asana` and `jira` must have one, each with the vendor's documentation URL in a comment beside it.
- **REQ-9**: `tentaqles connect <provider>` must add the catalog's entry to `mcpServers` in the client's `.claude.json`, keep every other key, and print `<provider>: connected as <name>`.
- **REQ-10**: Running it again must print `<provider>: already connected` and write nothing.
- **REQ-11**: A provider outside the scope must refuse with exit 1 and `<provider> is outside the scope of identity <ws>: <scope>`, write nothing, and log one line in the denial log with category `identity`.
- **REQ-12**: A provider with no `connect:` must print `<provider>: no MCP server in the catalog` and exit 0, writing nothing.
- **REQ-13**: In a neutral or untrusted folder, `connect` must refuse with exit 1, naming the reason.
- **REQ-14**: Each change must write one line to `$TQ_HOME/connections.jsonl` with time, identity, provider, server name and caller, and no value of the entry.
- **REQ-15**: `connect` must take no argument other than the provider, and a user provider file must not change the entry.
- **REQ-16**: The hook must allow `tentaqles connect <provider>` for an agent, while the client's `.claude.json` stays denied to every file tool.
- **REQ-17**: A server `connect` wrote must pass the MCP scope rules and survive `tq settings render`.

## Out of scope

- Authenticating the server: a human does that with `/mcp`, guided by the skill.
- A claude.ai connector, which only the developer can enable.
- Removing a connection.

## Affected areas

- `internal/cmd/status.go`, `internal/cmd/connect.go` (new), `internal/cmd/root.go`
- `internal/identitystatus/` (new): builds the JSON
- `internal/connect/` (new): edits `.claude.json` safely, and logs
- `internal/providers/`, `internal/providers/catalog/gh.yaml`, `linear.yaml`, `asana.yaml`, `jira.yaml`
- `docs/COMMANDS.md`, `docs/PROVIDERS.md`, `docs/threat-model.md`, `CHANGELOG.md`, `docs/adr/0017-connect.md`
- `CLAUDE.md`: Must not trade off names `connect`'s tests

## Do not touch

- `internal/mcpscope/`, `internal/policy/mcpentry.go`: the rules that judge entries don't change.
- `plugin/skills/`: ticket 021.

## Decisions

- **Agreed with the owner**: `connect` is agent-callable, catalog-only and logged.
- **Decided by the ticket writer**: `connect` re-signs nothing: `.claude.json` is not in the integrity record, and the entry can only be the catalog's.
- **Decided by the ticket writer**: the four providers with `connect:` are ones whose vendors document a hosted server. More can be added as data.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 018` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
