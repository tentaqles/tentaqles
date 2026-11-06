# 009: `status --json` for the skill, and `tentaqles connect <provider>`

**Status:** ready
**Risk:** High: `connect` is the one write an agent may call, so a bug lets an agent add an out-of-scope or attacker-chosen MCP server to the identity
**UI:** none

PRD v2 §5.2 and §10. The onboarding skill can't read the manifest or write Claude's config, which are protected. It reads the identity's setup through `status --json`, and registers a provider's MCP server through `connect`, which writes only what the embedded catalog defines, for a provider in scope.

## Requirements

- **REQ-1**: `tentaqles status --json` must print one JSON object with `identity`, `root`, `guard`, `scope`, `providers`, `browser` and `other_providers`. It writes nothing.
- **REQ-2**: `providers` must have one entry per provider in scope, in manifest order, with `id`, `category`, `cli`, `cli_resolves`, `credential`, `connection` and `verified`, where `credential` and `connection` are `present`, `missing` or `none`, and `verified` is true or false.
- **REQ-3**: In a neutral folder, `identity` must be null and the exit 0.
- **REQ-4**: In an untrusted workspace, the object must hold `identity`, a `trusted` of false and `reason`, and no provider data.
- **REQ-5**: `tentaqles status --json` must carry no token, credential value, environment value, or MCP `env` value. A provider's `credential` is presence only.
- **REQ-6**: `tentaqles doctor --json` must keep its current output.
- **REQ-7**: A catalog entry may have `connect:` with `name`, and `transport` of `http` or `stdio`, with `url` for http and `command` plus `args` for stdio. `gh`, `linear`, `asana` and `jira` must have one, each with the vendor's documentation URL in a comment beside it.
- **REQ-8**: `tentaqles connect <provider>` must add the catalog's entry to `mcpServers` in `<identity>/claude/.claude.json`, keep every other key byte-for-value, and print `<provider>: connected as <name>`.
- **REQ-9**: Running `connect` again must print `<provider>: already connected` and write nothing.
- **REQ-10**: A provider outside the scope must refuse with exit 1 and `<provider> is outside the scope of identity <ws>: <scope>`, write nothing, and log one line in the denial log with category `identity`.
- **REQ-11**: A provider with no `connect:` must print `<provider>: no MCP server in the catalog` and exit 0, writing nothing.
- **REQ-12**: In a neutral or untrusted folder, or with an identity whose guard is off, `connect` must refuse with exit 1 naming the reason.
- **REQ-13**: Each change must write one line to `$TQ_HOME/connections.jsonl`, with time, identity, provider, server name, and caller, and no value of the entry.
- **REQ-14**: `connect` must take no argument other than the provider: no URL, command or env.
- **REQ-15**: The hook must allow `tentaqles connect <provider>` for an agent, while `$TQ_HOME` and the identity's `.claude.json` stay denied to every file tool.
- **REQ-16**: A server `connect` wrote must pass `tq/cross-client-mcp` and the MCP scope rules unchanged, and must survive `tq settings render`.
- **REQ-17**: `docs/COMMANDS.md` must describe both commands, and that `status --json` is the skill's read and `doctor --json` the health report.

## Out of scope

- Authenticating the MCP server: a human does it with `/mcp`, or the skill guides them.
- A claude.ai connector, which only the developer can enable.
- Other connections beyond MCP. The skill tells the developer the exact step.
- User-file `connect:` entries: the owner accepted catalog-only.
- Removing a connection.

## Affected areas

- `internal/cmd/status.go`, `internal/cmd/connect.go` (new), `internal/cmd/root.go`
- `internal/identitystatus/` (new): builds the JSON from the manifest and the catalog
- `internal/connect/` (new): edits `.claude.json` safely, and logs
- `internal/providers/`, `internal/providers/catalog/gh.yaml`, `linear.yaml`, `asana.yaml`, `jira.yaml`
- `internal/policy/selfprotect.go`: confirm `connect` and `status` stay free, with a test
- `docs/COMMANDS.md`, `docs/PROVIDERS.md`, `docs/threat-model.md`, `CHANGELOG.md`, `docs/adr/0017-connect-one-agent-write.md`
- `CLAUDE.md`: Must not trade off names `connect`'s tests

## Do not touch

- `internal/mcpscope/`, `internal/policy/mcpentry.go`: the rules that judge entries don't change.
- `plugin/skills/`: ticket 012.

## Decisions

- **Agreed with the owner**: accept `connect`, agent-callable, catalog-only, logged.
- **Decided by the ticket writer**: the `verified` field is true only when the provider's `verify` read-only command last passed in this `status` run, with a 5 s limit each, so `--json` is bounded and needs the network only for that check.
- **Decided by the ticket writer**: `connect` doesn't re-sign anything: `.claude.json` is not in the integrity record, and the entry can't be other than the catalog's.
- **Decided by the ticket writer**: the `other_providers` field lists `Other` free-text names from `reach`, ticket 010, as `manual`.
- **Decided by the ticket writer**: the four providers with a `connect:` entry are the ones whose vendors document a hosted MCP server. More can be added as data, and none other is required for this ticket.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 009` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
