# 043: The graph of one repository

**Status** not started · **Stage** Graph · **Entries** G-02 (first languages), G-01 (the adapter, kept),
G-03 (rule 1 only), G-08 (the `build` command and `GraphDir`), R-03 (outline; steps 3, 5, 6), F-06 (`graph`),
F-01 (graph paths) · **Needs** 042, 005 · **Feeds** 044, 045, 046, 047, 048, 052, 053, 054, 057, 060

## Result

In a client's repository, `tentaqles lobe graph build` parses the code and writes one file of symbols under
that client's store. `tentaqles lobe graph outline <file>` then lists a file's definitions without anyone
opening the file. Nothing is written inside the repository and nothing reaches another client.

```bash
cd "$W/clients/acme" && git init -q shop && cd shop
printf 'class Loader:\n    def run(self):\n        return 1\n\ndef main():\n    return Loader()\n' > app.py
tentaqles lobe graph build
tentaqles lobe graph outline app.py
```

```
Loader · class  L1-L3  class Loader:
Loader.run · method  L2-L3  def run(self):
main · function  L5-L6  def main():
```

The graph file is `$TQ_HOME/identities/acme/lobe/graph/r_<8 hex>/graph.jsonl`. With
`tentaqles lobe set graph off`, `build` and `outline` print one line and exit 1.

## Scope

**In**

- The product adapter `internal/lobe/graph/parse` over the runtime the owner chose in
  [042](042-parser-runtime.md), with the grammar cache under the client's `index/grammars/` if that runtime
  keeps one.
- The registry and query files for the first languages of G-02: `go`, `python`, `typescript` (also
  JavaScript files), `tsx` (also JSX files), `java`, `c_sharp`, `rust`, `c`, `cpp`, `ruby`, `php`, `kotlin`
  (the list is proposed), less any grammar 042's result leaves out.
- Extraction (G-02 steps 2 to 4): nodes, kinds, spans, ids, owners, `sig` (first line), `exported`, `hash`,
  raw calls and raw references, and the captures `import.source`, `import.name`, `receiver` and `extends`.
- File listing (step 1), the parse cache (step 5), `graph.jsonl` with its meta line, node lines and
  `contains` edges, sorted, with no timestamp.
- `GraphDir` and `ParseDir`, including the `wt-<8 hex>` subfolder of a linked worktree (G-02 step 5).
- `tentaqles lobe graph build` in the foreground, and what the `graph` setting does to it (F-06).
- `tentaqles lobe graph outline <file>` (R-03 step 3), with the client rule of step 5 and the cap of step 6.
- The `NOTICE` entries for the runtime, the grammars and the 12 query files; the port headers.
- The leak-suite attacks that waited for a graph ([013](013-isolation-leak-suite.md)) now run.

**Out**

- The soundness check and `tentaqles lobe graph check` ([044](044-graph-soundness.md)).
- Every edge but `contains`, `ext` lines, the `dropped` counters, `lines` on edges, and
  `tentaqles lobe graph callers` ([045](045-resolution-and-callers.md)).
- The search index, `tentaqles lobe graph find` and `map` ([046](046-find-and-map.md)).
- The `params` and `args` captures, `params` on nodes, `argc` on edges, the multi-line `sig`
  ([047](047-signatures.md)).
- `files.json`, the change check, dirty markers, the build after the turn, `build.lock`, `summary.json`,
  `tentaqles lobe graph status` and "(may be stale)" ([048](048-graph-stays-current.md)).
- `--all` on `build` ([056](056-many-repositories.md)); the command refuses the flag until then.
- `.sql` files and the `sql.string` capture ([052](052-sql-in-the-graph.md)); Markdown, configuration,
  Terraform and the dot-entry exceptions such as `.github/` ([053](053-docs-and-config.md)); the `comment`
  capture ([054](054-why-comments.md)).
- The other 12 languages ([060](060-remaining-languages.md)); the MCP form ([062](062-mcp-server.md));
  token counters for the verbs ([058](058-token-savings.md)); the comparison with Trail-code
  ([057](057-graph-accuracy.md)).

## What it builds on

- **042**: the written result `docs/prd/spikes/G-01-parser.md` (runtime, pinned version, grammars left out,
  grammar revisions), the adapter interface, and the corpus.
- **[002](002-sandbox.md)**: `tentaqles lobe sandbox`, the clients `acme` and `globex`, the `lobeset` helper.
- **[003](003-shell-failure-claude-code.md)**: `internal/lobe/store` (`Open`, `Replace`, the atomic write, the
  store lock, `RepoKey` and its worktree test), `paths.LobeDir`, `internal/lobe/scrub` (`Text`, the `Clean`
  type), the repository walk of S-01 step 6, the rule that Sense records no `tentaqles lobe` call, the root
  `LICENSE` and `NOTICE`, and the import tests `TestLobeGuardNeverImportsLobe` and
  `TestLobeImportsOnlyTheFour`.
- **[005](005-status-and-capture-switch.md)**: `internal/lobe/settings` (`settings.From`),
  `tentaqles lobe settings` and `tentaqles lobe set`, `internal/lobe/cli` with `cli.Deps`, the registration in
  `internal/cmd/lobe.go`, the scope and login rule of F-07 step 3 with `TestLobeCommandTable`,
  `TestLobeAgentCannotReadAnotherClient` and `TestLobeCommandRefusesAnotherLogin`, and `lobe` in
  `documentedGroups` of `test/integration/docs_test.go`.
- **013**: the leak suite and `tentaqles lobe bench leak`.

This feature widens 005's settings only if `graph` is not yet decoded there, and adds its rows to the three
F-07 tests above.

## What it leaves for later features

- `internal/lobe/graph/parse`: `Load`, `Parse`, `Captures`, `Tree`, `Capture` (044 to 054, 060).
- `internal/lobe/graph/lang`: the registry (one row per language: extensions, grammar, query pair) and
  `queries/<lang>.scm` with `queries/<lang>.tq.scm` (047, 052, 054, 060 add captures or rows).
- `internal/lobe/graph/extract`: one file in, nodes and raw references out; the extractor version, a number
  that every feature raises when it changes what a parse-cache file holds (047, 052, 053, 054).
- `internal/lobe/graph`: `types.go` (`Node`, `Edge`, `Ext`, `RawRef`, `Graph`), `store.go` (read and write
  `graph.jsonl`, the parse cache), `GraphDir(clientDir, workDir)`, `ParseDir`, `build.go` (044 adds the check,
  045 resolution, 046 the index, 048 the incremental path).
- `internal/lobe/graph/resolve`: `Resolve(nodes []Node, raw []RawRef, mods []GoModule) (edges []Edge,
  ext []Ext, dropped Dropped)` with rule 1 only (045 adds rules 2 to 11).
- `internal/lobe/query`: `registry.go` (`Tool{Name, Summary string; Run func(Client, Args) (Answer, error)}`),
  `outline.go`, `render.go` with the 8,000-character cap (045, 046, 049, 062).
- Files in the store: `graph/<repo-key>/graph.jsonl`, `graph/<repo-key>/parse/<hh>/<sha>.json`,
  `graph/<repo-key>/wt-<8 hex>/graph.jsonl`, and `index/grammars/` if the runtime keeps a cache.
- Commands: `tentaqles lobe graph build`, `tentaqles lobe graph outline <file>`.
- Node ids (`<path>#<name>`, `<path>#<Owner>.<name>`, `~N`), which a lesson's `touches` holds
  ([055](055-stale-by-symbol.md)).

## How it works

1. **The command.** `tentaqles lobe graph build` resolves the client from the working folder with
   `internal/resolve` and applies F-07 step 3 (scope and login). It reads the settings. With `graph: off` it
   builds nothing, prints one line that names the setting and the fix `tentaqles lobe set graph on`, and
   exits 1. `recall`, `lessons` and `enforce` do not stop it (core page 5.6). It is not person-only.
2. **The repository** is the nearest folder at or above the working folder that holds `.git`, never above the
   client folder; the walk is S-01's. With no such folder the command prints one line and exits 1, because
   the graph folder is named by the repository key (this file's reading; see Risks).
3. **The folder.** `GraphDir(clientDir, workDir)` returns `graph/<repo-key>/` for a main working tree and
   `graph/<repo-key>/wt-<8 hex>/` for a linked worktree. It calls F-01's functions for the key and for the
   worktree test and derives neither. `ParseDir` always returns the main repository's `parse/`.
4. **Listing** (G-02 step 1). Run `git ls-files --cached --others --exclude-standard` as a child process
   under a deadline of its own; when git is missing, fails or is late, walk the folder instead. Skip dot
   entries, Trail-code's ten build folders, files over 1,000,000 bytes (compared with `>`) and extensions no
   registry row claims. The lobe never imports `internal/gitcfg`.
5. **Parse.** Pick the row by lower-cased extension. Hash the file (SHA-256 of its raw bytes). When
   `parse/<hh>/<sha>.json` exists and names this extractor version, use it. Otherwise parse the whole file
   through the adapter under a per-file deadline and run the language's query pair.
6. **Nodes** (step 2). A `definition.<kind>` capture with a `name` is a node. Kinds, the `field`, `property`
   and `object` mappings, and the span widening rule are G-02 step 2's. Each file is also a node of kind
   `file`: its id is its repository-relative path with `/`, its name the file's base name, its span
   `[1, last line]` with no trailing empty line counted.
7. **Raw references** (steps 3 and 4). `reference.call` and `reference.send` are raw calls; `reference.class`,
   `.interface`, `.implementation` and `.module` are raw references; `import.source`, `import.name`,
   `receiver` and `extends` are kept on the raw record for 045. The source is the smallest enclosing
   definition, or the file for a call. A reference outside any definition is dropped, and so is a call on a
   definition's own name: a recursive call never becomes an edge (decided).
8. **Ids and owners.** `<path>#<name>`, or `<path>#<Owner>.<name>` when owned; a repeated id gets `~2`,
   `~3` in document order. `owner` is the id of the enclosing class, interface, struct, enum or module in
   the same file, and a function with such an owner becomes kind `method`. A Go method takes its owner from a
   receiver capture in `go.tq.scm`.
9. **Other node keys.** `sig` is the definition's first line without a trailing `{`, passed through
   `scrub.Text` (047 replaces it). `exported` is false for a lower-case Go name and a Python name that starts
   with `_`. `hash` is the first 16 hex characters (proposed) of the SHA-256 of the definition's bytes.
   Offsets are UTF-8 bytes; lines are 1-based. No source text is stored.
10. **The parse cache** (step 5). The file's nodes and raw references, before resolution, are written to
    `parse/<hh>/<sha>.json` with the extractor version. A linked worktree reads and fills the main
    repository's cache.
11. **Edges in this feature.** `resolve.Resolve` applies G-03 rule 1 only: one `contains` edge per
    definition, from its owner when it has one, else from its file, with `"confidence":"extracted"`,
    `"score":1`, `"rule":"contains"`. Every other raw reference waits in the parse cache for 045.
12. **Write.** `graph.jsonl` holds one JSON object per line: the meta line, node lines by id, edge lines by
    source, relation and target. Key order is G-02's table; a node key with no value is left out. Lines are
    sorted by byte value and no timestamp is written, so equal input gives equal bytes. The file is replaced
    through F-01's atomic write under the store lock.
13. **The meta line.** Keys in order: `t`, `v` (1), `repo`, `nodes`, `edges`, `languages`, `dropped`,
    `errors`. `languages` has, per language, `files`, `nodes` (file nodes counted) and `calls` (raw call
    captures). `dropped` is `{"ambiguous":0,"untyped":0,"unknown":0}` until 045 counts. `errors` lists the
    run-time failures of this build (a grammar that would not load, a file that hit its deadline), each as a
    path and a class, never the text of an error.
14. **Outline.** `tentaqles lobe graph outline <file>` reads this working tree's `graph.jsonl` and prints the
    file's definitions by line: `<name> · <kind>  L<start>-L<end>  <sig>`, where `<name>` is the id's part
    after `#` and the line is cut at 140 characters. At most 200 entries (proposed) and 8,000 characters
    (proposed); a last line `(N more not shown)` counts what was cut. `<file>` is a path relative to the
    repository or to the working folder; a base name that matches several files lists them and exits 1; an
    unknown file exits 1 and names it on stderr.
15. **The verb's rules** (R-03 step 5). Outside a client folder, without a graph, or with `graph: off`,
    `outline` prints one line on stderr and exits 1. It never builds: a missing graph gives the fix line
    `tentaqles lobe graph build`. Inside an agent session it refuses when `CLAUDE_CONFIG_DIR` or `CODEX_HOME`
    names anything but this client's own login folder; unset is allowed. It writes nothing.
16. **`tentaqles lobe rebuild`** (F-01 step 10) deletes `graph/` with `index/`; from this feature on it also
    builds the current repository's graph when `graph` is on.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-02](../prd/lobe-graph.md), all steps, the parts table, "How to build it"; [G-01](../prd/lobe-graph.md), "How to build it" (adapter, cache, no work at import time); [G-03](../prd/lobe-graph.md), rule 1; [G-08](../prd/lobe-graph.md), "How to build it" (`build`, `GraphDir`, `ParseDir`) |
| Requirements | [R-03](../prd/lobe-recall.md), steps 3, 5, 6 and 8, and "How to build it"; [F-06](../prd/lobe-foundation.md), the `graph` row; [F-01](../prd/lobe-foundation.md), steps 1, 5, 8 and 10; [F-07](../prd/lobe-foundation.md), steps 1 to 4 and 6; [core page](../prd/vertical-lobe.md) 5.1, 5.3 and 5.6 |
| Source to port: registry, kinds, ids, owner | Trail-code [`src/graph/generic.ts` L47](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L47), [L114](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L114), [L627](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L627), [L743](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L743), [L300](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L300), [L259](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L259), [L728](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L728) |
| Source to port: query files | Trail-code [`src/graph/queries/java.scm` L1](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/queries/java.scm#L1) (the capture style; copy java, c, cpp, c_sharp, ruby, rust, php and kotlin unchanged). The `tags.scm` of the grammar repositories for go, python, typescript and tsx, which Trail-code reads at run time ([`generic.ts` L155](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L155)). Web, not machine-checked |
| Source to port: listing, sorted output | Trail-code [`src/ingest/fs.ts` L28](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ingest/fs.ts#L28) (the size cap, `>` at L277, the skipped folders at L9); [`src/graph/write.ts` L50](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/write.ts#L50) (we sort by byte value instead) |
| Source to port: outline | Trail-code [`src/ask/ask.ts` L1461](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L1461) (the outline entry); [`src/mcp/tools.ts` L46](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L46) (the tool definitions) |
| Not copied | Trail-code [`src/graph/refresh.ts` L66](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/refresh.ts#L66): a query may wait 90 s for a first build. Ours never builds. Also `generic.ts` L175, L181 (swallowed load and compile failures), L659 (the walker), L311 (UTF-16 offsets) |
| The spike | `docs/prd/spikes/G-01-parser.md`, and `spike/parser/` on its branch |
| Existing tentaqles code | `internal/cmd/root.go` (`NewRoot`), `internal/cmd/lobe.go` and `internal/lobe/cli` from 005, `internal/lobe/store`, `internal/lobe/scrub`, `internal/lobe/settings`, `internal/resolve/resolve.go`, `internal/paths/paths.go`, `test/integration/docs_test.go`, `docs/reference/commands.md`, `NOTICE`, `.goreleaser.yaml` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the runtime and the
   left-out grammars in 042's result; whether `store.RepoKey` already handles a linked worktree and which
   function is the worktree test; how the store writer creates folders under `graph/`; whether
   `settings.From` already returns `graph`; whether `tentaqles lobe rebuild` exists and which feature built
   it; which leak-suite attacks name a graph under `needs`. The owner approves changes before code.
2. **The adapter**: `internal/lobe/graph/parse`, rebuilt from the spike's adapter for the chosen runtime
   with its version pinned in `go.mod`. A grammar loads on first `Load`; a parse runs under a deadline; a
   panic never leaves the adapter; the grammar cache, if any, sits in the client's `index/grammars/`.
   Proves it: adapter unit tests; `TestLobeGuardNeverImportsLobe` green; `CGO_ENABLED=0 go build ./...` for
   the six targets; the binary's growth written into the pull request.
3. **Types and store** (side by side with task 4): `internal/lobe/graph/types.go`, `store.go`, `GraphDir`,
   `ParseDir`. Proves it: `TestLobeGraphDeterministic` on hand-made nodes, `TestLobeGraphDirLayout`.
4. **Registry and query files** (side by side with task 3): `internal/lobe/graph/lang/registry.go`,
   `queries/*.scm` copied or fetched at their pinned revisions, `queries/*.tq.scm` with the four added
   captures, the seven editor predicates stripped, the `NOTICE` lines. Proves it:
   `TestLobeGraphQueriesCompile`, `TestLobeGraphNoQueryFileNoRow`, `TestNoticeListsEveryPort`.
5. **Extraction**: `internal/lobe/graph/extract`. One agent writes the shared code against Python; then the
   fixtures and golden files go side by side, one agent per group: go; typescript and tsx (with JavaScript
   and JSX); java, kotlin and c_sharp; c, cpp and rust; ruby and php. Proves it:
   `TestLobeGraphExtractFixtures`, `TestLobeGraphOffsetsAndCap`.
6. **Listing**: `list.go` in `internal/lobe/graph`: the git child process with its deadline, the walk, the
   skips. Proves it: a listing test over a fixture with an ignored file, a build folder and a dot folder.
7. **The builder**: `build.go` and the thin `internal/lobe/graph/resolve`; the parse cache; the meta line.
   Proves it: `TestLobeGraphRepoKeyIsTheStores`, `TestLobeGraphParseCacheVersion`,
   `TestLobeGraphDeterministic` end to end.
8. **`tentaqles lobe graph build`** in `internal/lobe/cli`, registered in `internal/cmd/lobe.go`, with the
   `graph` switch. Proves it: `TestLobeGraphBuildRefusesWhenOff`, `TestLobeSwitchesAreIndependent`.
9. **`tentaqles lobe graph outline`** (side by side with task 8): `internal/lobe/query`. Proves it:
   `TestLobeQueryToolCount`, `TestLobeQueryNeverBuilds`, `TestLobeQueryOtherClient`, and the new rows in
   `TestLobeCommandTable` and `TestLobeCommandRefusesAnotherLogin`.
10. **`tentaqles lobe rebuild`** builds the graph, and the leak suite's graph attacks run. Proves it:
    `TestLobeLeak_AllAttacksZeroLeaks`, `TestLobeLeak_NothingSkippedSilently`.
11. **Docs**: a `## tentaqles lobe graph` section in `docs/reference/commands.md` with the actions `build`
    and `outline`; a line in `CHANGELOG.md` under `[Unreleased]`. The plugin does not change.
12. **Tester run** of the Evaluation below.

## Tests that stay in the repository

- `TestLobeGraphQueriesCompile`: 12 of 12 query pairs compile; 0 capture names outside the read list and the
  three ignored ones (`doc`, `ignore`, `local.scope`).
- `TestLobeGraphExtractFixtures`: per language, node ids, owners and raw references equal a golden file: 12
  of 12. The goldens include a recursive function, whose own call is absent from the raw references.
- `TestLobeGraphDeterministic`: two builds with the file order reversed give the same bytes.
- `TestLobeGraphOffsetsAndCap`: the span after a non-ASCII comment is right; a 1,000,001-byte file is
  skipped.
- `TestLobeGraphRepoKeyIsTheStores`: the graph folder's name, `repo` on the meta line and `repo` in an event
  that S-01 records in the same repository are one string, `r_` plus 8 hex characters.
- `TestLobeGraphNoQueryFileNoRow`: every registry row names a query file that exists and compiles, and a file
  of a language with no row gives 0 nodes.
- `TestLobeGraphDirLayout` (new here): a main working tree gives `graph/<repo-key>/`, a linked worktree
  gives `graph/<repo-key>/wt-<8 hex>/`, and `ParseDir` is the same folder for both.
- `TestLobeGraphParseCacheVersion` (new here): a second build parses 0 files; a cache file that names
  another extractor version is ignored and rewritten.
- `TestLobeGraphBuildRefusesWhenOff` (new here): with `graph: off`, `build` exits 1, names the setting and
  writes 0 files.
- `TestLobeSwitchesAreIndependent` (F-06), the two graph cases: with `lessons: off` the build still builds;
  with `recall: off` and `enforce: off` together it still builds.
- `TestLobeQueryToolCount`: the registry holds exactly the tools built so far, 1 here (045 makes it 2, 046
  makes it 4, 049 makes it 5).
- `TestLobeQueryNeverBuilds`: with no graph, or with `graph: off`, `outline` exits 1 in under 200 ms and
  creates no file.
- `TestLobeQueryOtherClient`: run in `globex`, `outline` prints no path from `acme`'s graph; run in `acme`
  with `globex`'s login folder set, it exits 1.
- The count 12 follows the first-language list: a grammar 042's result leaves out lowers it in both tests.
- House rules kept green: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour` (the graph packages
  import only `internal/resolve`, `internal/paths`, `internal/registry` and `internal/manifest`, have no
  `init` and no package-level call), `TestLobeLeak_AllAttacksZeroLeaks`, `TestNoticeListsEveryPort`,
  `TestDocsReferenceEveryCommand`, `TestLobeCommandTable`, and every test under "Must not trade off" in
  `CLAUDE.md`, unedited. No hook changes, so the stall tests are untouched.

## Evaluation

You are a tester agent. You did not build this feature. You receive the built `tentaqles` binary and an
empty working folder. Use bash (Git Bash on Windows), no network, and run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
lobeset() {  # lobeset <client> <key> <value>: a fixture edit of a sandbox manifest
  m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"
}
cd "$W/clients/acme" && git init -q shop && cd shop
printf 'class Loader:\n    def run(self):\n        return 1\n\ndef main():\n    return Loader()\n' > app.py
```

If a line starts with `SETUP FAIL`, stop and report FAIL. Never clear `CLAUDECODE` and never run
`tentaqles lobe set`. Run the checks in `$W/clients/acme/shop` unless a check says otherwise.

1. `tentaqles lobe graph build; echo "exit=$?"` ends with `exit=0`.
   `ls "$TQ_HOME/identities/acme/lobe/graph" | wc -l` prints `1`, and the name is `r_` plus 8 hex characters.
   Set `K` to that name and `G="$TQ_HOME/identities/acme/lobe/graph/$K"`.
2. Line 1 of `$G/graph.jsonl` starts with `{"t":"meta","v":1,"repo":"<K>",` and contains
   `"python":{"files":1,"nodes":4,"calls":1}`.
3. One line contains `"id":"app.py#Loader.run","name":"run","kind":"method","path":"app.py","span":[2,3]`.
4. `grep -c '"t":"edge"' "$G/graph.jsonl"` prints `3`, and so does `grep -c '"relation":"contains"'`.
5. `find "$G/parse" -name '*.json' | wc -l` prints `1`.
6. Record the SHA-256 of `graph.jsonl`, run the build again and compare. Expected: the same hash.
7. `tentaqles lobe graph outline app.py; echo "exit=$?"` prints exactly 3 lines and `exit=0`. Line 1 holds
   `Loader` and `L1-L3`; line 2 holds `Loader.run` and `L2-L3`; line 3 holds `main` and `L5-L6`.
8. `tentaqles lobe graph outline nope.py; echo "exit=$?"` prints `exit=1`; stderr holds `nope.py`.
9. `head -c 1000001 /dev/zero | tr '\0' '#' > big.py; printf 'x\n' > notes.xyz`, then build.
   `grep -c -E 'big\.py|notes\.xyz' "$G/graph.jsonl"` prints `0`, and line 1 still holds `"files":1`.
10. `(cd "$W/clients/globex/app" && tentaqles lobe graph outline app.py; echo "exit=$?")` prints `exit=1` and
    no `Loader`. `ls "$TQ_HOME/identities/globex/lobe/graph" 2>/dev/null | wc -l` prints `0`.
11. `CLAUDECODE=1 CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles lobe graph outline app.py;
    echo "exit=$?"` prints `exit=1` and no `Loader`. The same with
    `CODEX_HOME="$TQ_HOME/identities/globex/codex"` in place of `CLAUDE_CONFIG_DIR`: `exit=1`, no `Loader`.
    With `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`: 3 lines and `exit=0`.
12. `lobeset acme graph off`. `tentaqles lobe graph build; echo "exit=$?"` prints one line that holds `graph`
    and `tentaqles lobe set graph on`, then `exit=1`. `tentaqles lobe graph outline app.py; echo "exit=$?"`
    prints `exit=1`. The hash of `graph.jsonl` is unchanged.
13. `lobeset acme graph on; lobeset acme lessons off; lobeset acme recall off; lobeset acme enforce off`.
    `tentaqles lobe graph build; echo "exit=$?"` ends with `exit=0`.
14. `tentaqles lobe bench leak --out "$W/leak"; echo "exit=$?"` ends with `exit=0`, and
    `grep -c '"leaks":0' "$W/leak/summary.json"` prints `1`.

**Person checks** (the owner, in a terminal with the tester's `HOME`, `USERPROFILE` and `TQ_HOME`):

1. In `shop`: `tentaqles lobe set graph off --client acme`, answered with yes; `tentaqles lobe graph build`
   then prints the line that names the `graph` setting and exits 1. `tentaqles lobe set graph default
   --client acme`, answered with yes; `tentaqles lobe graph build` then exits 0.

Scoring: PASS needs 14 of 14. "Could not run" is a FAIL.

## Done when

- [ ] every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin suite is unchanged because the plugin did not change
- [ ] the tester brief passes, 14 of 14
- [ ] the house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeLeak_AllAttacksZeroLeaks`, `TestLobeQueryOtherClient`, `TestNoticeListsEveryPort`
- [ ] `docs/reference/commands.md` has `## tentaqles lobe graph`; `CHANGELOG.md` has its line; `NOTICE` lists
  the runtime, the grammars and the 12 query files
- [ ] the owner has seen the build and the outline of the Result section

## Risks and unknowns

- **The four `tags.scm` files** were not read by machine (web). Fetch them at the grammar revisions 042
  pinned and let `TestLobeGraphQueriesCompile` and the goldens judge them. One of them may carry a capture
  name outside G-02's read list and the three ignored ones; the test then fails, and the owner decides
  whether the name is read or ignored. Stripping the editor predicates can also turn a pattern that
  excluded local names into one that matches them; the goldens show it.
- **The Go receiver capture** in `go.tq.scm` is new and unproven (G-02). If the grammar cannot express it,
  Go methods have no owner and their ids change; stop and ask.
- **The language list is proposed.** A grammar that 042's result leaves out lowers every count of 12 here.
- **Binary size.** 042 measured up to 20 MB of growth (proposed mark) for the first set. Check the release
  archives still build for all six targets.
- **A repository is required.** G-02 step 1 speaks of "a plain walk outside git". This file reads that as
  "when the `git` program cannot list the files", because the graph folder needs a repository key and F-01
  gives none outside a repository. If the owner wants graphs of folders with no `.git`, step 2 changes.
- **`errors` on the meta line** has no shape in G-02. This file takes a sorted list of path and class.
- **The file node's `name`** is the base name here; the requirements show only a file at the repository
  root (`a.py`).
- **`~N` ids shift** when an earlier same-named definition is deleted; 055 handles the stale `touches`.
- **The parse cache only grows.** `tentaqles lobe rebuild` empties it; 048 decides on pruning.
- **If F-01's cache-format spike picked a database**, the line shapes here stay as the logical record and
  `store.go` changes.
- **What would change this file:** another runtime, a shorter language list, a decision on the three
  readings above.
