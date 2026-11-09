# 062: The graph as MCP tools

**Status** not started · **Stage** Graph · **Entries** R-03 (steps 5 to 8, the MCP server); S-01 (step 3, the
`lobe_` prefix); F-07 (the `graph mcp` row); A-19 · **Needs** 056 · **Feeds** none

## Result

The five agent commands of the graph are also offered as tools over the Model Context Protocol, for a person who
prefers tools to shell commands. It is off by default: nothing registers the server, so a client has none until
its person adds one line to that client's host configuration by hand. The tools answer with the same text as the
commands, byte for byte.

```bash
tentaqles lobe graph mcp        # serves lobe_find, lobe_callers, lobe_outline, lobe_map, lobe_impact on stdio
```

A person adds that command as an MCP server named, say, `graph` in the client's Claude Code or Codex
configuration. In the next session the agent has the five tools, and a call to `lobe_find` returns what
`tentaqles lobe graph find` prints. Remove the entry and the tools are gone.

## Scope

**In**

- The action `mcp` of `tentaqles lobe graph`, and `internal/lobe/query/mcp.go`.
- Five tools over the one registry of 046 and 049: `lobe_find`, `lobe_callers`, `lobe_outline`, `lobe_map` and
  `lobe_impact`. The index line says "the four graph commands"; R-03 and F-07 say five, the four of R-03 and
  G-09's `impact`.
- The client check on every call, as the commands make it (R-03 step 5, F-07 step 3).
- Proof that nothing registers the server: no render step, no plugin file, no setting.
- Proof that Sense records no event for a call to one of these tools (S-01 step 3), on both hosts.
- One stats line per tool call (M-01), so the cost of the server can be reported apart.
- The docs that tell a person how to add and remove the entry by hand for each host.

**Out**

- Registering the server for anyone. No command of tentaqles writes an MCP entry, now or later (R-03 step 7).
- More tools. Core page section 11 rules out dozens; `links`, `status`, `check` and `build` stay commands.
- `--full` of `impact`. It lifts the cap for a person and is refused inside an agent session, so no tool
  offers it.
- A long-running service of any other kind, HTTP transport included. The server speaks stdio and lives as long
  as the session that started it.
- Lessons through MCP. No tool files a lesson, changes a setting or touches the ladder.

## What it builds on

- [046](046-find-and-map.md): `internal/lobe/query` with `registry.go`
  (`Tool{Name, Summary string; Run func(Client, Args) (Answer, error)}`), one file per verb, `render.go`, and
  the 8,000-character cap with its last line that counts what was cut.
- [043](043-graph-of-one-repository.md), [045](045-resolution-and-callers.md) and [049](049-blast-radius.md):
  `outline`, `callers` and `impact` in that registry; `TestLobeQueryToolCount` holds it at exactly 5.
- [056](056-many-repositories.md): the `all` argument of `find` and `impact`.
- [005](005-status-and-capture-switch.md): the command rules of F-07 step 3 and the `graph` setting.
- [004](004-any-tool-failure.md), [008](008-successes.md) and [010](010-shell-failure-codex.md): the rule that
  the lobe's own tools are not recorded, which already skips an MCP tool whose own name starts with `lobe_`
  (`mcp__<server>__lobe_find`). Until now that rule matched nothing real.
- [058](058-token-savings.md): `stats.Append` and the `tool` key of a stats line.
- [013](013-isolation-leak-suite.md): attacks L-13 and L-15, which run R-03's agent tools under another
  client's name and login.
- Existing tentaqles code: `internal/codexconfig`, whose `TestRenderKeepsUserConfig` already shows that a
  hand-written `[mcp_servers.<name>]` table in a client's Codex `config.toml` survives a render.

## What it leaves for later features

Nothing in the index depends on this feature. It leaves the `mcp` action, the tool names with the `lobe_`
prefix, and `mcp.go` as the second surface of the registry: a tool added to the registry later appears on both
surfaces with no further code.

## How it works

1. **Start.** `tentaqles lobe graph mcp` reads JSON-RPC messages on stdin and writes answers on stdout, one per
   line, until stdin closes. It serves three requests: `initialize`, `tools/list` and `tools/call`, and takes a
   notification without answering it. Logs go to stderr only. It builds nothing, writes no file but the stats
   line of step 7, and holds no lock while idle.
2. **The tools.** `tools/list` is generated from the registry: for each `Tool`, the name `lobe_` plus the verb,
   the registry's `Summary` as the description, and an input schema made from the verb's arguments (`query`,
   `limit`, `in`, `all`, `source` for `lobe_find`; `symbol`, `out`, `in` for `lobe_callers`; `file` for
   `lobe_outline`; `max_dirs` for `lobe_map`; `base`, `file`, `lines`, `depth`, `all` for `lobe_impact`). The
   defaults, limits and caps are the registry's, so they cannot differ from the commands'. Trail-code lists its
   tools only when an index exists (R-03 source table); ours does the same: with no graph, or with `graph` off,
   the list is empty.
3. **A call.** `tools/call` runs the tool's `Run` and `render`, the same two functions the command runs, and
   returns the text as one text content item. `TestLobeQuerySurfacesAgree` holds the two surfaces
   byte-identical for 20 fixed inputs. A `--json` form is not offered; the text is the answer.
4. **The client check, on every call** (R-03 step 7). The server resolves the client from its working folder
   with `internal/resolve` on each call, not once at start, and reads only that client's `graph/<repo-key>/`
   (with `all`, that client's other graph folders and `links.json`). When `CLAUDE_CONFIG_DIR` or `CODEX_HOME`
   names anything but this client's own login folder, the call is refused; unset is allowed. A host starts
   this process, so the server applies that rule to every call and does not look at `CLAUDECODE`. Outside a
   client folder, without a graph, or with `graph` off, the call returns the same one line the command prints
   on stderr, as a tool error, with the fix `tentaqles lobe graph build` where that is the fix.
5. **Off by default.** No render step, plugin file or setting writes an MCP entry. After `tentaqles reach` and
   `tentaqles settings render`, no file under the client's identity folder names `lobe graph mcp`, and the
   plugin has no MCP entry. A person adds the entry to the client's host configuration by hand and removes it
   the same way.
6. **Not recorded.** A call to one of these tools is one of the lobe's own tools: Sense records no event for
   it, as a failure or as a success, so the lobe does not learn from its own output. Sense knows the tools by
   the part of the tool name after the server, which starts with `lobe_`; the server part is whatever the
   person chose. How Codex names an MCP tool in its hook payload is not confirmed (S-01 risks); the capture of
   task 2 settles it, and Sense's rule is widened if the form differs.
7. **Counted.** After the answer is written, the handler appends one stats line as the commands do, with
   `"tool":"lobe_find"` (the MCP name, so a sum can tell the two surfaces apart). The fixed cost, the five
   descriptions the host loads in every session, cannot be counted by the lobe. A test holds the size of the
   `tools/list` answer under 2,000 bytes (proposed here), and the docs state that cost as an estimate.
8. **Failing open.** A malformed message, an unknown method or a panic in a tool gives a JSON-RPC error for
   that request and the server goes on. A call is cut off after 10 seconds (proposed here) with an error
   answer. The server never prints anything that is not a protocol message.

## Where to read

| What | Where |
|---|---|
| Requirements | [R-03](../prd/lobe-recall.md), all of it, steps 5 to 8 first; [S-01](../prd/lobe-sense.md) step 3 and its last risk; [F-07](../prd/lobe-foundation.md) step 1 (the row and the paragraph under the table) and step 3; [G-09](../prd/lobe-graph.md), the `impact` bullet; [M-01](../prd/lobe-benchmarks.md) step 1; [core page](../prd/vertical-lobe.md) decision A-19 and section 11 |
| Source to port | [`src/mcp/tools.ts` L46](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L46): the six tool definitions we choose four from; [`src/mcp/server.ts` L93](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/server.ts#L93): tools are listed only when an index exists |
| Weakness, changed | [`src/mcp/tools.ts` L275](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L275): MCP default 5 hits, against 8 at [`src/cli.ts` L835](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L835); [`src/mcp/tools.ts` L278](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L278): MCP always inlines source; [L147](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L147): MCP omits the call-site line that [`src/graph/traverse-cli.ts` L238](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/traverse-cli.ts#L238) prints. One registry ends these differences |
| Weakness, not copied | [`src/mcp/tools.ts` L318](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L318): an unbounded depth |
| The protocol | The Model Context Protocol specification for the stdio transport, `initialize`, `tools/list` and `tools/call`. Not cited by the requirements; read at build time, web, not machine-checked |
| Existing tentaqles code | `internal/lobe/query/` (046), `internal/lobe/cli` (the `graph` verb), `internal/codexconfig/codexconfig_test.go` (`TestRenderKeepsUserConfig`), `internal/claudesettings/` (what a render writes into a client's Claude home), `plugin/hooks/hooks.json`, `test/integration/docs_test.go` |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: the registry's `Args` type and
   whether a schema can be made from it; how `render` returns its text; whether `tentaqles settings render`
   touches MCP entries in a client's Claude home today. Put to the owner the three proposals marked "proposed
   here" (no SDK dependency, the 2,000 bytes, the 10 seconds). Files: none. Check: a written list the owner
   approves.
2. A capture with a written result: how each host names one of these tools in its hook payload, and whether the
   host passes its login variable and working folder to an MCP server process it starts. The builder writes
   the capture script and a stub server; the owner runs it once in one client they name, in Claude Code and in
   Codex. The result holds field names and tool-name forms, never content. Can run beside task 3.
3. `internal/lobe/query/mcp.go`: the stdio loop, `initialize`, `tools/list` from the registry, `tools/call`. It
   is written by hand on `encoding/json`, with no new dependency (proposed here): three methods do not justify
   an SDK whose packages would be linked into the same program as the guard. Tests:
   `TestLobeQuerySurfacesAgree`, `TestLobeQueryToolCount`, `TestLobeQueryMCPProtocol`.
4. The action `mcp` in `internal/lobe/cli`, the client check on every call, and the refusals. Tests:
   `TestLobeQueryOtherClient` (its MCP half), `TestLobeQueryNeverBuilds` (its MCP half),
   `TestLobeQueryMCPChecksEveryCall`. Needs task 3.
5. Off by default. Test: `TestLobeQueryMCPOffByDefault`, and `TestLobeQueryMCPEntrySurvivesRender` for a
   hand-written entry on each host. Can run beside task 4.
6. Sense and stats: widen the own-tool rule if task 2 found another name form; append the stats line. Tests:
   the S-01 case that `mcp__graph__lobe_find` writes 0 lines, now also in the form task 2 found for Codex;
   `TestLobeStatsCountsEachAgentCommand` with five MCP cases; `TestLobeQueryMCPListBudget`.
7. The leak suite: L-13 and L-15 also call each MCP tool, under another client's name, `..` paths and another
   client's login. Test: `test/integration/lobeleak_test.go` stays at 0 leaks.
8. Docs: the `mcp` action in the `tentaqles lobe graph` section of `docs/reference/commands.md`, with the two
   hand-written entries (Claude Code and Codex), how to remove them, and the per-session cost as an estimate;
   `CHANGELOG.md` under `[Unreleased]`. The plugin does not change, and `plugin/CHANGELOG.md` gets no line.
9. The tester run of "Evaluation", by an agent that built none of this.

## Tests that stay in the repository

- `TestLobeQueryToolCount`: the registry holds exactly 5 tools, and `tools/list` names exactly `lobe_find`,
  `lobe_callers`, `lobe_outline`, `lobe_map` and `lobe_impact`.
- `TestLobeQuerySurfacesAgree`: for 20 fixed inputs the CLI text and the MCP text are byte-identical.
- `TestLobeQueryMCPOffByDefault`: after `tentaqles reach` and `tentaqles settings render`, no file under the
  client's identity folder names `lobe graph mcp`, and the plugin has no MCP entry.
- `TestLobeQueryCaps`: a symbol with 500 callers gives 50 hits and a last line that counts 450, through
  `lobe_callers` as through the command.
- `TestLobeQueryOtherClient`: run in `globex`, no tool returns a path from `acme`'s graph; run in `acme` with
  `globex`'s login folder set, each tool call is refused. 0 leaks.
- `TestLobeQueryNeverBuilds`: with no graph, or with `graph: off`, `tools/list` is empty, each call returns an
  error in under 200 ms and no file is created.
- `TestLobeQueryMCPProtocol` (new here): `initialize`, `tools/list` and one `tools/call` round-trip; a malformed
  line, an unknown method and an unknown tool each give an error answer and the next request still works; every
  byte on stdout is a protocol message.
- `TestLobeQueryMCPChecksEveryCall` (new here): a server started in `acme`'s folder answers; after the folder's
  marker is replaced by another client's, the next call is refused. The check is made per call.
- `TestLobeQueryMCPEntrySurvivesRender` (new here): a hand-written entry in the client's Codex `config.toml` and
  one in the client's Claude home are byte-identical after `tentaqles settings render`. 2 of 2.
- `TestLobeQueryMCPListBudget` (new here): the `tools/list` answer is at most 2,000 bytes (proposed here).
- The S-01 own-tool test, widened: a post-tool event for `mcp__graph__lobe_find`, in each form task 2 found,
  writes 0 event lines; `mcp__graph__other` is recorded.
- House rules kept green: the import and no-`init` test of F-02 (`mcp.go` adds no `init` and no package-level
  call), `TestLobeLeak_AllAttacksZeroLeaks`, `TestLobeCommandRefusesAnotherLogin`, `TestLobeCommandTable`,
  `TestDocsReferenceEveryCommand`, the plugin suite unchanged (`test_the_plugin_holds_only_the_guard_hook` as
  D-11 left it: no MCP entry), and every test under "Must not trade off".

## Evaluation

Tester brief. Start from the standard tester setup of [002](002-sandbox.md) (F-07). No network. Then run:

```sh
cd "$W/clients/acme/app" && mkdir -p lib app && printf 'module example.com/fix\n' > go.mod
printf 'package lib\n\nfunc ParseSpan(s string) (int, int) { return 0, 0 }\n' > lib/span.go
printf 'package app\n\nimport "example.com/fix/lib"\n\nfunc LoadA() { lib.ParseSpan("L1") }\n' > app/a.go
tentaqles lobe graph build
INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"<V>","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}'
LIST='{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
call() { printf '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"%s","arguments":%s}}' "$1" "$2"; }
mcp() { printf '%s\n' "$@" | tentaqles lobe graph mcp; }
Q='{"query":"parse span"}'
```

`<V>` is the protocol version the builder names when handing this brief over. `mcp` sends its arguments as
lines and prints the server's answers; the server exits when its input ends.

1. `mcp "$INIT" "$LIST"` exits 0. The answer with `"id":2` names exactly five tools: `lobe_find`,
   `lobe_callers`, `lobe_outline`, `lobe_map` and `lobe_impact`. No tool name lacks the prefix `lobe_`.
2. `mcp "$INIT" "$(call lobe_find "$Q")"`: the answer with `"id":3` holds a text that starts `1. ParseSpan` and
   contains `lib/span.go:L3`.
3. Save `tentaqles lobe graph find "parse span"` to `$W/cli.txt`. Extract the text of check 2's answer to
   `$W/mcp.txt` (decode the JSON string). `cmp "$W/cli.txt" "$W/mcp.txt"` is silent.
4. `mcp "$INIT" "$(call lobe_callers '{"symbol":"NoSuchName"}')"`: the answer with `"id":3` is an error answer
   that contains `NoSuchName`, and the process still exits 0.
5. `(cd "$W/clients/globex/app" && mcp "$INIT" "$LIST" "$(call lobe_find "$Q")")`: no tool is listed, the call
   is an error, and no answer contains `span.go`.
6. `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" mcp "$INIT" "$(call lobe_find "$Q")"`, still in
   acme's `app` folder: the call is refused and no answer contains `span.go`. The same with
   `CODEX_HOME="$TQ_HOME/identities/globex/codex"` in its place.
7. `grep -rl "graph mcp" "$TQ_HOME/identities" | wc -l` prints `0`: no MCP server is registered for either
   client.
8. Export `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`. `A` is the absolute path of acme's `app`
   folder. Count the lines in the files under `$TQ_HOME/identities/acme/lobe/events/` (0 if there are none).
   Pipe `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"<A>","tool_name":"mcp__graph__lobe_find",
   "tool_input":{"query":"zzz"},"tool_response":{"content":"none"}}` into `tentaqles lobe-hook claude
   post-tool-use`: exit 0, empty stdout, and the same count. Pipe it again with the tool name
   `mcp__graph__other`: the count is one higher.
9. `lobeset acme graph off`, then `mcp "$INIT" "$LIST"`: no tool is listed. `lobeset acme graph on`.
10. `printf '%s\nnot json\n%s\n' "$INIT" "$LIST" | tentaqles lobe graph mcp`: the bad line gets an error answer,
    the answer with `"id":2` still lists five tools, and every line of stdout parses as JSON.

**Person checks** (the owner, in one client of their own)

1. Run the capture of task 2 in Claude Code and in Codex and hand back the result.
2. Add the server by hand to that client's Claude Code configuration, start a session with `tentaqles run`, ask
   the agent to find a function with `lobe_find`, and see the answer. Then `tentaqles lobe events` shows no
   line for that call. Remove the entry; the next session has no such tools.
3. The same in Codex, with `[mcp_servers.graph]` in the client's Codex `config.toml`. Run
   `tentaqles settings render` and see the entry still there.

Scoring: PASS needs 10 of 10. "Could not run" is a FAIL.

## Done when

- [ ] The written result of task 2 exists, and Sense's own-tool rule covers every name form it found.
- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin suite is unchanged and green.
- [ ] The tester brief passes, 10 of 10, and the owner has run the three person checks.
- [ ] The house rules hold: `TestLobeQueryMCPOffByDefault`, `TestLobeQueryOtherClient`,
  `TestLobeLeak_AllAttacksZeroLeaks` with the tools attacked, the own-tool test, the guard's tests untouched.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` updated.
- [ ] The owner has seen the result described under Result: the tools appear only after the hand-written entry.

## Risks and unknowns

- The protocol's details (the version string, the message framing on stdio, the shape of a tool error) are not
  in the requirements and were not machine-checked. Read the specification first; `TestLobeQueryMCPProtocol`
  and person checks 2 and 3 are what prove the server against two real hosts.
- Whether a host hands its login variable and working folder to an MCP server process is unconfirmed. If the
  variable is absent, the login half of the client check sees "unset" and allows; the folder half still holds.
  If the working folder is not the client's repository, every call is refused and the docs must say which
  folder to start the server in. Task 2 settles both.
- How Codex names an MCP tool in a hook payload is unknown (S-01 risks). Until task 2, the own-tool rule may
  miss it and the lobe would record its own tools' calls in Codex.
- The server lives as long as the session, close to the "long-running background service" core page section 11
  rules out, and its five descriptions cost tokens in every session of a client that adds it. That is why it is
  off by default (R-03 risks).
- The login check is a convenience: an agent with a shell can unset the variable. The boundary stays the
  client's own folder and store.
- Three numbers and choices here are this file's, marked "proposed here": no SDK dependency, the 2,000-byte
  list, the 10-second cutoff of a call.
- The index says four commands; the requirements say five tools. This file follows the requirements.
