# 052: SQL in the graph

**Status** not started · **Stage** Graph · **Entries** G-05 · **Needs** 045, and in fact also 044, 046, 048
and 049 · **Feeds** 057

## Result

After `tentaqles lobe graph build`, the tables, views, routines, triggers and indexes of a repository's `.sql`
files are nodes of its graph, and so is every table that code or SQL uses. Edges say who reads and who writes
each table: a view, a routine, a statement in a file, or a function that holds SQL in a string. The agent can
find a table by name and ask what depends on it. No model is called and no database is contacted.

From the sandbox of [002](002-sandbox.md), with the `shop` fixture of the Evaluation section built:

```bash
tentaqles lobe graph find "users"
tentaqles lobe graph impact --file db/schema.sql --lines 2-2 --json
```

The first prints a hit `a.users · table  db/schema.sql:L2-L2`. The second prints a report whose `seeds` holds
`a.users` and whose `impacted` holds the Python function `load` with `"relation":"writes_to"`, because
`etl.py` runs `UPDATE a.users SET id = 1` from a string.

## Scope

**In**

- The package `internal/lobe/graph/sqlgraph`: `mask.go`, `gosep.go`, `walk.go`, `recover.go`, `refs.go`,
  `names.go` (G-05 steps 1 to 8).
- `.sql` files routed to it by the lister of [043](043-graph-of-one-repository.md), parsed with the `sql`
  grammar when [042](042-parser-runtime.md) kept it, and scanned by patterns when the tree has an error node
  or there is no grammar.
- SQL in code: the `sql.string` capture in the `.tq.scm` file of every first language, the five shapes that
  make a string count as SQL (proposed), and edges from the enclosing function.
- New rows in `schema.go`: the kinds `table`, `view`, `routine`, `trigger`, `index`; the relations
  `reads_from`, `writes_to`, `triggers`, `executes`, `indexes`; the rules `sql-ddl`, `sql-statement`,
  `sql-recovery`, `sql-string`; the node key `defined`; the meta key `sql`.
- The four dialect fixture folders of the first set (proposed): Spark SQL as Databricks runs it, PostgreSQL,
  T-SQL and BigQuery, each with golden nodes, edges and a `recovered` count.
- The rule for a table that is used and never created, in the soundness check and in the search index.
- A new extractor version, so that every parse cache file and `files.json` record of an older build is void
  (G-02 step 5, G-08 step 1).

**Out**

- Reading a live database. It needs a login and a network connection, which principle 7 rules out (G-05).
- MySQL fixtures, dbt `ref()`, ORMs and SQL built from joined strings (G-05 risks). Not planned.
- Using the tables a command touches to recall a lesson before SQL runs. G-05 names it under "Why it
  matters"; R-04's matcher has no key for a table, and no feature in the index takes it. Reported.
- `tentaqles lobe graph callers` over `reads_from` and `writes_to`. R-03 gives `callers` R-01's five
  relations, so it does not list a table's readers; `impact` does. Reported as a question.
- The list after an edit of a `.sql` file inside the hook ([049](049-blast-radius.md) leaves it out).
- A comparison with Trail-code, which has no SQL ([057](057-graph-accuracy.md) says so in its report).

## What it builds on

- [042](042-parser-runtime.md): the written result for the `sql` grammar (loaded or left out), and
  `internal/lobe/graph/parse`.
- [043](043-graph-of-one-repository.md): the lister (git or a walk, the dot rule, the 1,000,000-byte cap), the
  registry and the `.tq.scm` files, `graph/extract`, the node, edge, `ext` and meta line shapes with their key
  order, sorted output with no timestamp, the atomic write, `tentaqles lobe graph build`.
- [044](044-graph-soundness.md): `schema.go` (allowed kinds, relations, confidence values, rules), `Check`, the
  list of rule functions in `check.go`, `tentaqles lobe graph check`.
- [045](045-resolution-and-callers.md): the resolve stage that turns raw references into labeled edges and
  removes duplicates on source, relation and target, keeping the higher score.
- [046](046-find-and-map.md): the search index built with the graph, and `find`.
- [048](048-graph-stays-current.md): the parse cache `parse/<hh>/<sha>.json`, the extractor version,
  `files.json`, the incremental build that must equal a cold one.
- [049](049-blast-radius.md): `impact`, whose walk takes every relation except `contains` and `explains`.
- The scrubber of [003](003-shell-failure-claude-code.md) (S-05) is not needed: no statement text is stored.

## What it leaves for later features

- The SQL kinds, relations and rules in `schema.go`. 053 and 054 add theirs the same way.
- `sqlgraph`'s body scanner (`refs.go`), which finds read and write targets in a piece of SQL text. Nothing
  else uses it yet.
- `"sql":{"files":N,"recovered":M}` on the meta line, after `errors`. 053 puts `skipped` after it, 056 `scopes`.
- The rule "a node with no `path` is not in the search index" (decided here), which any later kind without a
  place in a file follows.
- `sqlgraph/testdata/<dialect>/`, where a later dialect (MySQL) is one more folder.

## How it works

1. **Routing** (G-05 step 1). The lister claims the extension `.sql`. SQL has no registry row and no query
   file: the build hands the file to `sqlgraph`. Each line that holds only `GO`, with an optional count, is
   replaced by `;` padded to the same byte length (`gosep.go`), so offsets and line numbers hold. The text is
   parsed with the `sql` grammar.
2. **Per file, in any order** (G-05 step 2). The names of every created table, view and routine are
   registered before the walk, so a trigger written before its function still links to it.
3. **The walk** (G-05 step 3). `CREATE TABLE` gives a `table` node and a `references` edge per foreign key,
   and so does `ALTER TABLE ... ADD CONSTRAINT`. `CREATE VIEW` gives a `view` node and a `reads_from` edge per
   `FROM` or `JOIN` table, except `WITH` names in scope. `CREATE FUNCTION` or `PROCEDURE` gives a `routine`
   node with the same reads. `CREATE TRIGGER` gives a `trigger` node with `triggers` to its table and
   `executes` to its routine. `CREATE INDEX` gives an `index` node with `indexes` to its table.
4. **Statements** (G-05 step 4, new). A top-level `SELECT`, `INSERT`, `UPDATE`, `DELETE` or `MERGE` gives
   `reads_from` and `writes_to` edges from the file node, with the statement's line in `lines`.
5. **Recovery** (G-05 step 5). When the tree has an error node, or there is no usable grammar, `mask.go`
   blanks comments and single-quoted strings to spaces, keeping every offset, and `recover.go` finds the
   names after `CREATE [OR REPLACE|ALTER] FUNCTION|PROC|PROCEDURE|TRIGGER|TABLE`. Inside each routine body
   `refs.go` finds the read targets after `FROM` and `JOIN` and the write targets after `INSERT`, `UPDATE`,
   `DELETE`, `MERGE` and `INTO`. `UPDATE o ... FROM dbo.Orders o` is resolved through the alias. The list of
   words that follow `FROM` or `INTO` without naming a table is ported as data.
6. **SQL in code** (G-05 step 6, new). The `sql.string` capture gives the string literals of a code file. A
   string counts only if, after leading white space and ignoring case, it has one of five shapes (proposed):
   `SELECT ... FROM x`, `INSERT INTO x`, `UPDATE x SET`, `DELETE FROM x`, `MERGE INTO x`. `refs.go` then gives
   edges from the enclosing definition, or from the file node when there is none. A target that is not a
   plain dotted name, such as an interpolation, gives no edge.
7. **Names** (G-05 step 7). Strip a pair of backticks that wraps a whole dotted path, but only when no other
   backtick is inside it; split on `.`; strip one pair of quotes, backticks or brackets per part; lower-case
   each part, except a double-quoted part that holds an upper-case letter, which keeps its case (proposed).
   The node id is `sql:<name>`, one node per table for the whole repository. A bare name resolves to a
   schema-qualified table only when exactly one schema has it.
8. **One naming pass for the repository.** Each file's result (created objects and raw table references,
   with lines and the path that found them) is what the parse cache holds, keyed by the file's hash. After
   all files are read, one pass in the resolve stage registers every created name, resolves bare names,
   and writes one node per name. `path` and `span` point at the `CREATE` statement, the first by sorted path
   if several, and that file `contains` the node. A SQL node has `"lang":"sql"` and no `sig`.
9. **A table used and never created** is a node with `"defined":false`. Decided in this brief: it has no
   `path` and no `span` (G-02: a key with no value is left out) and no `contains` edge; `check.go` gains a
   rule function that excuses such a node from the span and path rules and reports it as a problem when it
   has no incoming edge; the search index leaves it out, so `find` and the prompt block never print a
   pointer without a file. The blast walk still passes through it.
10. **Labels** (G-05 step 8). Tree-walk edges: `extracted`, 1, rule `sql-ddl` (from a `CREATE` or `ALTER`) or
    `sql-statement`. Recovery edges: `inferred`, 0.85, `sql-recovery`. Code-string edges: `inferred`, 0.75,
    `sql-string`. The last two scores are proposed. Duplicates on source, relation and target keep the
    higher score.
11. **What is stored** (principle 6). Names, kinds, lines and labels. Never the text of a statement or of a
    string. Line 1 gains `"sql":{"files":N,"recovered":M}`; `languages` gets no `sql` row, because SQL has no
    query file (decided here).
12. **Hosts.** No hook is added or changed. Claude Code and Codex read the same graph through the same
    commands, and the build after a turn (048) picks up a changed `.sql` file like any other.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-05](../prd/lobe-graph.md), all of it; [G-02](../prd/lobe-graph.md) steps 1, 4 and 5 and the table of line shapes; [G-03](../prd/lobe-graph.md) step 6; [G-12](../prd/lobe-graph.md) steps 1, 2 and 4 and "How to build it"; [G-08](../prd/lobe-graph.md) steps 1 and 5; [G-01](../prd/lobe-graph.md) steps 1 and 5 (the `sql` grammar); [core page](../prd/vertical-lobe.md) section 3, principles 6 and 7 |
| Source to port: the walk, `GO`, name keys | [`graphify/extractors/sql.py` L549](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L549), [L482](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L482), [L449](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L449), [L874](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L874) |
| Source to port: the recovery scan | [`sql.py` L113](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L113) (the masker), [L296](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L296) (the write verbs), [L342](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L342), [L933](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L933); the word list at L313 and the holes at L86 and L103 of the same file |
| Source: not copied | [`sql.py` L696](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L696) (trigger order), [L467](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L467) (no grammar, no result), [L519](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L519) (every edge `EXTRACTED`), [`graphify/pg_introspect.py` L153](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/pg_introspect.py#L153) (a live database) |
| Lobe code from earlier features | `internal/lobe/graph/schema.go`, `check.go`, `build.go`, `graph/extract`, `graph/lang/queries/*.tq.scm`, `graph/resolve`, `internal/lobe/search` (the index build) |
| Existing tentaqles code | `NOTICE` and `third_party/licenses/` (Graphify is listed since phase 1); the test that checks the `// Ported from` header |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: how the build routes a
   file to an extractor, what a parse cache file holds, where the resolve stage can take a repository-wide
   pass, how `check.go` lists its rule functions, how the index build picks its nodes. The owner approves
   changes, and the decisions of step 9 and step 11, before code is written.
2. **Read 042's result for `sql`** and run the four dialect folders through the grammar once, as a throwaway
   test. Write down per dialect how many files parse without an error node. If the grammar was left out, or
   a dialect falls to recovery for most files, tell the owner before going on: every edge of those files will
   be `inferred`.
3. **`names.go`, `mask.go`, `gosep.go`** (side by side with 4 and 5). Tests: `TestLobeSQLBacktickPath`,
   `TestLobeSQLNameCase`, `TestLobeSQLGoKeepsOffsets`.
4. **`walk.go`**: steps 2 to 4. Tests: `TestLobeSQLTriggerOrder`, `TestLobeSQLCTENotATable`.
5. **`recover.go` and `refs.go`**: step 5, with the word list as a data file. Tests:
   `TestLobeSQLRecoveryNoFabrication`, `TestLobeSQLNoGrammarAllInferred`.
6. **SQL in code**: the `sql.string` capture in each first language's `.tq.scm`, the shape test, the edges.
   Tests: `TestLobeSQLStringShapes`, `TestLobeGraphQueriesCompile` (the capture is now read).
7. **Into the build**: routing, the cache record, the naming pass, the rows in `schema.go`, the undefined
   table rule in `check.go` and the index, the meta key, the new extractor version. Tests:
   `TestLobeSQLUndefinedTable`, `TestLobeSQLNoStatementText`, `TestLobeGraphIncrementalEqualsCold` and
   `TestLobeGraphDeterministic` with a fixture that holds `.sql` files.
8. **Dialect fixtures**: `sqlgraph/testdata/<dialect>/` for the four dialects, each with its golden list and
   golden `recovered` count. Test: `TestLobeSQLDialectFixtures`.
9. **Docs.** A paragraph on SQL in the `tentaqles lobe graph` section of `docs/reference/commands.md` (what
   `build` reads, what `find` and `impact` return for a table); one line in `CHANGELOG.md` under
   `[Unreleased]`. Every ported Go file starts with its `// Ported from` line. The plugin does not change.
10. **Tester run** of the Evaluation section by a fresh agent.

## Tests that stay in the repository

- `TestLobeSQLTriggerOrder`: a trigger written before its function gives 1 `executes` edge.
- `TestLobeSQLRecoveryNoFabrication`: 6 broken files with `CREATE` text in comments and quoted strings give 0
  extra nodes.
- `TestLobeSQLStringShapes`: 10 SQL strings give edges; 10 English sentences that start with a SQL verb give
  0.
- `TestLobeSQLCTENotATable`: `WITH levels AS (...) SELECT ... FROM levels` gives no node `sql:levels`.
- `TestLobeSQLDialectFixtures`: the 4 dialect folders give their golden nodes, edges and `recovered` counts. 4
  of 4.
- `TestLobeSQLBacktickPath`: `` `shop.sales.orders` `` and `` `shop`.`sales`.`orders` `` give the same node
  id.
- New names in this brief: `TestLobeSQLNameCase` (`"Users"` and `users` are two nodes; `[dbo].[Orders]` and
  `dbo.orders` are one), `TestLobeSQLGoKeepsOffsets` (a node after three `GO` lines has the right span),
  `TestLobeSQLNoGrammarAllInferred` (with the grammar switched off in the test, every edge is `inferred` and
  `recovered` equals `files`), `TestLobeSQLUndefinedTable` (a table only read gives one node with
  `"defined":false`, no `path`, 0 problems in the check, and no hit in `find`; with its reader removed and
  the node kept by hand, the check reports 1 problem), `TestLobeSQLNoStatementText` (a fixture whose statements
  hold the marker strings `hunter2` and `SET id` leaves neither in `graph.jsonl`, the parse cache or the
  index).
- From earlier features, kept green with SQL in their fixtures: `TestLobeGraphQueriesCompile`,
  `TestLobeGraphDeterministic`, `TestLobeGraphIncrementalEqualsCold`, `TestLobeGraphSoundFixtures`,
  `TestLobeResolveEveryEdgeHasRule`, `TestLobeBlastSymbolLevel`.
- House rules: `TestLobeGuardNeverImportsLobe` (no `init`, no package-level call: the patterns of `recover.go`
  compile on first use), `TestLobeImportsOnlyTheFour`, `TestNoticeListsEveryPort`,
  `TestLobeGraphNeverCrossesClients` when 056 exists, and every test under "Must not trade off" in `CLAUDE.md`,
  unedited.

## Evaluation

A fresh tester agent gets the built `tentaqles` first on `PATH`, an empty working folder and this section.
Use bash (Git Bash on Windows). No network. The standard setup:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
```

If a line starts with `SETUP FAIL`, stop and report FAIL. Then the fixture:

```bash
S="$W/clients/acme/shop"; git init -q "$S"; cd "$S"; mkdir db
printf '%s\n' 'CREATE TABLE b.orders (uid int REFERENCES a.users(id));' 'CREATE TABLE a.users (id int);' \
  'INSERT INTO a.users SELECT uid FROM b.orders;' > db/schema.sql
printf '%s\n' 'def load(cur):' '    cur.execute("UPDATE a.users SET id = 1")' \
  '    print("Update your profile from settings")' > etl.py
tentaqles lobe graph build || echo "SETUP FAIL build"
J="$(ls -d "$TQ_HOME"/identities/acme/lobe/graph/r_*)/graph.jsonl"
```

`J` is the graph file of `shop`; there is one folder under `graph/`. Run every check in `$S`.

1. One line of `J` contains
   `"id":"sql:a.users","name":"a.users","kind":"table","path":"db/schema.sql","span":[2,2]`.
2. One line contains `"source":"sql:b.orders","relation":"references","target":"sql:a.users",` followed
   directly by `"confidence":"extracted","score":1,"rule":"sql-ddl"`.
3. Two lines have `"source":"db/schema.sql"` and `"rule":"sql-statement","lines":[3]`: one with
   `"relation":"reads_from","target":"sql:b.orders"`, one with `"relation":"writes_to","target":"sql:a.users"`.
4. One line contains `"source":"etl.py#load","relation":"writes_to","target":"sql:a.users",` followed directly
   by `"confidence":"inferred","score":0.75,"rule":"sql-string"`.
5. `grep -c -i -E 'your|profile|settings|SET id' "$J"` prints `0`: no node comes from the English sentence
   and no statement text is stored.
6. Line 1 of `J` contains `"sql":{"files":1,"recovered":0}`: the file parsed cleanly, so checks 2 and 3 did
   not come from the recovery scan. If the written result of 042 left the `sql` grammar out, this check
   expects `"recovered":1` instead, and checks 2 and 3 expect `"confidence":"inferred","score":0.85,
   "rule":"sql-recovery"`; the tester is told which case applies.
7. `tentaqles lobe graph check --json --strict`: exit 0 and `"ok":true`.
8. `tentaqles lobe graph find "users"`: exit 0 and one hit line contains `a.users · table` and
   `db/schema.sql:L2-L2`.
9. `tentaqles lobe graph impact --file db/schema.sql --lines 2-2 --json`: exit 0; `seeds` holds an entry with
   `"name":"a.users"`; `impacted` holds an entry with `"name":"load"`, `"path":"etl.py"` and
   `"relation":"writes_to"`.
10. `printf 'SELECT * FROM c.missing;\n' > db/q.sql`, then `tentaqles lobe graph build`. One line of `J`
    contains `"id":"sql:c.missing"` and `"defined":false` and no `"path"`. Line 1 contains `"sql":{"files":2`.
    The command of check 7 still exits 0, and the output of `tentaqles lobe graph find "missing"` does not
    contain `c.missing · table`.
11. `sha256sum "$J"`, then `tentaqles lobe graph build` again, then `sha256sum "$J"`: the same hash.
12. `ls "$TQ_HOME"/identities/globex/lobe/graph 2>/dev/null | wc -l` prints `0`, and
    `(cd "$W/clients/globex/app" && tentaqles lobe graph find "users"; echo "exit=$?")` prints `exit=1` and
    no `a.users`.

There are no person checks. PASS needs 12 of 12. "Could not run" is a FAIL. The tester reports anything
surprising and fixes nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems with `CGO_ENABLED=0`).
- [ ] The tester brief passes, 12 of 12.
- [ ] The house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeSQLNoStatementText`, `TestNoticeListsEveryPort`, and `tentaqles lobe bench leak` at zero leaks.
- [ ] The per-dialect `recovered` counts of task 2 and of the golden files are written into the changelog
  line or the bench notes, so a later grammar change shows as a difference.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a table found by name and the function that writes it listed by `impact`.

## Risks and unknowns

- **The `sql` grammar.** 042's result may leave it out, and one general grammar serves every dialect. What
  falls to recovery is only `inferred`. Task 2 measures it on the four fixture folders; how often it happens
  in a client's repository is unknown until the `recovered` count is read there.
- **Decided in this brief, not in the requirements:** the shape of a table that is used and never created
  (step 9); no `languages` row and no `sig` for SQL (steps 8 and 11); a code string outside any definition
  hangs on the file node; a target that is not a plain dotted name gives no edge; the repository-wide naming
  pass sits in the resolve stage. The owner approves them at task 1.
- **A routine's writes.** G-05 step 3 gives a routine that parses cleanly "the same reads" as a view and names
  no writes; step 5 finds writes only in the recovery scan. This brief builds it as written. Whether the body
  scanner should also run on a cleanly parsed routine is a question for the owner.
- **Scores 0.85 and 0.75**, the five string shapes, the case rule for a double-quoted part and the first
  dialect set are proposed in the requirements and stay proposed.
- **Holes copied knowingly, in recovery only:** PostgreSQL dollar-quoted bodies and MySQL double-quoted
  strings are not blanked, and a string continued on the next line is scanned as code (G-05).
- **Same-named tables of two databases merge** into one node (G-05 risk).
- **The search rank.** R-01's graph rank walks five relations, none of them a SQL relation, so a table's rank
  comes from its name, path and text alone. M-01 and M-07 show whether that is enough.
