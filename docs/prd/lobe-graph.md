# Lobe: Graph (G-01 to G-12)

This stage builds a map of one client's repositories: files, symbols, tables, documents and configuration as nodes,
and "contains, calls, imports, reads, writes" as edges. G-01 picks the parser. G-02 turns each file into nodes and
unresolved references. G-03 turns references into edges with an honest label. G-04 adds argument counts. G-05 and G-06
add SQL, documents and configuration with no model call. G-07 to G-12 keep the graph current and sound. Recall and
enforcement read it.

The store layout, the hook table, the settings and the lesson shape this page leans on are in the
[core page](vertical-lobe.md). The graph is built when the client's `graph` setting is on, whatever `recall` and
`enforce` say (core page 5.6).

## G-01 Parser runtime without cgo

**Phase** 0 · **Origin** spike; third-party runtime · **Depends on** none

**What it is.** A throwaway test to find the library tentaqles parses code with. tree-sitter is a parser generator
with grammars for most languages; its usual Go bindings need cgo (C code linked into the Go program), which tentaqles
forbids. The spike measures the cgo-free ways to run those grammars; the owner decides from its written result.

**Why it matters.** "Fewer tokens" rests on the graph, and the graph rests on parsing 25 languages inside one cgo-free
binary for six platforms (core page 5.1 and 8). If no candidate parses a useful share of them, G-02 to G-10 are
planned again; if only some grammars fail, the first release ships fewer languages (step 5).

**Mechanism**

1. Grammars: 25, which are the 24 Trail-code parses (its registries at `src/graph/extract.ts` L32 and
   `src/graph/generic.ts` L47) plus `sql` for G-05. `hcl` (G-06) is an extra with no pass mark, because G-06 has a
   fallback for it. "First set" means G-02's 12 grammars plus `sql`. The 24 are `c`, `c_sharp`, `clojure`, `cpp`,
   `dart`, `elixir`, `gleam`, `go`, `java`, `kotlin`, `lua`, `nix`, `ocaml`, `php`, `python`, `r`, `ruby`, `rust`,
   `scala`, `solidity`, `swift`, `tsx`, `typescript` and `zig`.
2. Corpus and oracle: 20 (proposed) pinned public files per grammar plus G-02's query files. The reference
   tree-sitter CLI prints each file's S-expression (the parse tree as text) and each query's captures (name, start
   byte, end byte). The corpus, with each file's source URL and license, and the oracle's recorded output are
   committed under `spike/parser/testdata/`, so a rerun needs neither that CLI nor the network.
3. Try in order and stop at the first that meets every pass mark: (a) `gotreesitter`, a pure-Go tree-sitter runtime;
   (b) grammars compiled to WebAssembly under `wazero`, a pure-Go WebAssembly runtime; (c) others, `bonsai` first.
4. Measure the table below on Windows amd64 and Linux amd64 at least, then write `docs/prd/spikes/G-01-parser.md`:
   every value, the machine, OS and Go version, the grammars left out under step 5, and a recommendation.
5. One runtime (decided). A release carries exactly one of these runtimes. A grammar that the chosen runtime cannot
   load, or whose trees or captures differ from the oracle's (measures 3 and 4), is left out of the first release:
   its registry row is not shipped and its files are skipped as unclaimed extensions (G-02). A second runtime is
   never added to cover the gap. The written result names every grammar left out and the measure it missed. When
   no candidate meets every mark, the result ranks the candidates by the grammars each parses correctly, first set
   first, and recommends the best one with a shorter language list, never a blend of two. A result under mark 2
   therefore reaches the owner as a shorter list, not as a request for a second runtime.

| # | Measure | Pass mark (all proposed) |
|---|---|---|
| 1 | `CGO_ENABLED=0 go build` for linux, darwin and windows on amd64 and arm64, with no GPL or MPL grammar linked | 6 of 6 exit 0; yes |
| 2 | Grammars that load and parse their corpus files | 13 of 13 in the first set; at least 22 of 25 |
| 3 | Trees equal to the oracle's S-expression | 100% of first-set files; at least 98% of all files |
| 4 | Captures equal to the oracle's, with the `#eq?`, `#match?` and `#any-of?` predicates applied | 100% in the first set |
| 5 | Cold start: process start to the first finished parse of a 200-line file, per grammar, with the grammar cache empty and warm if the candidate keeps one. It matters because G-09 and E-05 parse the edited file once inside the PostToolUse hook (core page 5.2). There the event line is appended first and measured against the capture budget of 150 ms, so a parse never delays capture; the parse counts against the inject budget (300 ms at the 95th percentile, proposed) and the 1.5-second cutoff of the whole process. No other hook event is meant to parse | 95th percentile at most 250 ms with a warm cache; the empty-cache value is reported with no mark |
| 6 | Throughput: parse plus queries over the whole corpus on one core | at least 1 MB of source per second |
| 7 | Growth of the stripped binary (`-trimpath -ldflags "-s -w"`) | at most 20 MB for the first set and 40 MB for all 25 |
| 8 | Peak memory while parsing one 1,000,000-byte file (G-02's size cap) | at most 400 MB |
| 9 | 8 goroutines parsing at once under `go test -race`. The race detector itself needs cgo and a C compiler on Linux and Windows, so only this test run has cgo on; the adapters stay free of it | 0 race reports; results equal the serial run |
| 10 | Hostile input (a cut-off file, random bytes, one 1 MB line) under a 5-second deadline per file | 0 panics leave the adapter; 0 calls run past the deadline |

**Source**

| Project | What we take | Where to read |
|---|---|---|
| gotreesitter (MIT) | candidate (a) | [README](https://raw.githubusercontent.com/odvcencio/gotreesitter/main/README.md): pure Go, a query language with predicates, 206 grammars, four copyleft grammars in the default set. [BENCH.md](https://raw.githubusercontent.com/odvcencio/gotreesitter/main/BENCH.md): full parse 4.815 times slower than C, its own figure. Web, not machine-checked |
| wazero (Apache-2.0) | the runtime for (b) | [README](https://raw.githubusercontent.com/tetratelabs/wazero/main/README.md): compiler on amd64 and arm64, interpreter elsewhere, no tested windows/arm64 entry. [Package page](https://pkg.go.dev/github.com/tetratelabs/wazero): memory limit 65,536 pages (4 GB) by default. [API page](https://pkg.go.dev/github.com/tetratelabs/wazero/api): `Function.Call` is not safe for concurrent use. Web, not machine-checked |
| Trail-code (MIT) | proof that WebAssembly grammars plus query files carry a product | [`src/graph/generic.ts` L131](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L131): loads `tree-sitter-wasm/<lang>/tree-sitter-<lang>.wasm`. [`package.json` L63](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/package.json#L63): that grammar bundle at `^2.0.4` |
| bonsai (MIT) | candidate (c) | [README](https://raw.githubusercontent.com/msuozzo/bonsai/main/README.md): one Go module per grammar, 17 grammars, no documented query API. Web, not machine-checked |
| Go toolchain | why the cgo bindings are out | [cgo documentation](https://raw.githubusercontent.com/golang/go/master/src/cmd/cgo/doc.go): a file that imports "C" is not built when cgo is off. [Race detector article](https://go.dev/doc/articles/race_detector): the detector requires cgo and, outside macOS, an installed C compiler. Web, not machine-checked |
| tentaqles (new) | the grammar list, the corpus, the oracle method and every pass mark | this entry |

**How to build it**

- `spike/parser/` on a branch that is never merged, with one adapter per candidate under `candidates/`; nothing under
  `internal/` changes. Pass the parser the whole byte slice: Trail-code's 16,384-character slices work around a
  32 KB string limit of its Node tree-sitter binding (`generic.ts` L225, `src/graph/depth-parser.ts` L130).
- One adapter interface, kept by G-02 as `internal/lobe/graph/parse`: `Load(lang string) error`,
  `Parse(ctx context.Context, lang string, src []byte) (Tree, error)` and
  `Captures(lang, query string, t Tree) ([]Capture, error)`, with `Capture{Name string; Start, End uint32}`.
- `gotreesitter`: import only the 25 grammar packages and build with `-tags gotreesitter_no_copyleft`. Note each
  grammar that needs an external scanner and has none.
- `wazero`: check first whether grammar `.wasm` files built for the browser runtime can be hosted; if not, compile the
  tree-sitter C runtime and the 25 `parser.c` files into one WASI module. Use one instance per goroutine,
  `WithCloseOnContextDone(true)` and a low memory limit, and time cold start with and without a compilation cache.
- Any cache of compiled grammars or parser tables is per client. In the product it sits under
  `$TQ_HOME/identities/<client>/lobe/index/grammars/` (`index/` is the cache folder of core page 5.3), never
  in a folder two clients share, even though it holds no client content (principle 1). Each
  client therefore pays the compile once. The spike takes the cache folder as a flag and reports cold start with it
  empty and warm, its size on disk and the time to fill it.
- No work at import time. The adapter loads a grammar on the first `Load`, never from an `init` function or a
  package-level variable (core page 5.1): the lobe is linked into the same program as the guard. The written result
  records what each candidate's own packages do at import time, and what that adds to the start of a process that
  parses nothing.
- `hcl`: try it on each candidate and record the result. G-06's fallback is `github.com/hashicorp/hcl/v2`, a Go
  library and not a second grammar runtime, so step 5 does not rule it out. Measure 1's "no GPL or MPL grammar" is
  about grammars a runtime bundles; that library is the one MPL-2.0 dependency accepted (G-06).

**How we know it works**

- The written result has a value in every cell for (a) and (b), or says why (b) was not needed: yes or no.
- The written result recommends one runtime and lists the grammars left out of the first release, or says there
  are none: yes or no.
- `TestLobeSpikeParserMatchesOracle`: 0 tree mismatches and 0 capture mismatches in the first set.
- `TestLobeSpikeParserColdStart`: the 95th percentile of 50 runs per grammar, grammar cache warm, is at most 250 ms.
- `TestLobeSpikeParserHostileInput`: 0 panics and 0 late returns.

**Tester brief**

Start from the standard tester setup in F-07; no client is needed. You also receive the `spike/parser` folder with
its `testdata` corpus and recorded oracle output, the Go toolchain, a C compiler on `PATH` (only the race run in
check 1 uses it) and the written result. Network is allowed for check 1 only. Run every command in `spike/parser`.

1. Run `go mod download`, then `CGO_ENABLED=0 go test ./...`, then `CGO_ENABLED=1 go test -race ./...`. Expected:
   exit code 0 three times.
2. With `CGO_ENABLED=0`, for each pair of `GOOS` (linux, darwin, windows) and `GOARCH` (amd64, arm64), run
   `go build -o out/<os>-<arch> .`. Expected: exit code 0 six times.
3. Run `go run . oracle`. Expected: per grammar a line `<lang> trees=<a>/<b> captures=<c>/<d>`, with `a` = `b` and `c`
   = `d` for the first set, then `grammars ok: <n>/25` with `n` at least 22. They equal the written result, and
   the grammars not counted in `n` are exactly those the written result lists as left out of the first release.
4. Run `go run . coldstart --runs 50`, which warms the grammar cache first if the candidate keeps one. Expected: per
   grammar a line `<lang> p95_ms=<n>` with `n` at most 250.
5. Run `go run . hostile`. Expected: the last line is `panics=0 late=0` and the exit code is 0.

**Risks and open questions**

- Thin speed evidence: gotreesitter's parse figure is its own, and wazero's 4.72-times slowdown is a [third-party
  test](https://00f.net/2026/06/23/webassembly-runtimes-2026/) on C cryptography code (web, not machine-checked).
- gotreesitter v0.57.0 was published on 2026-11-08 (Go module proxy, web), so its API may still move. Pin the version.
- The wazero route may need our own WebAssembly build of the grammars. `bonsai` has no documented query API, so
  choosing it means writing a query engine for G-02.
- A cold start at the 250 ms mark leaves about 50 ms of the 300 ms inject target (proposed) for the rest of the
  PostToolUse work of G-09 and E-05. M-06 measures the whole event; G-09 has a fallback that does not parse.
- A per-client grammar cache means the first parse in every new client is slow, and a third-party runtime may do
  work at import time that every tentaqles process pays, the guard's hook included. Both are in the written result.
- With one runtime, a grammar that fails drops its language from the first release, however popular it is.

## G-02 Symbol extraction from query files

**Phase** 4 · **Origin** port: Trail-code · **Depends on** G-01, F-01, S-05

**What it is.** A tree-sitter query file (`.scm`) is a list of patterns that name parts of a parse tree: "this is a
function definition, this is its name, this is a call". G-02 runs one per language and writes nodes and unresolved
references to one graph file per repository. A new language is one registry row and one query file.

**Why it matters.** It serves "fewer tokens": recall answers "where is `upsert`" with a path and a line range. Every
later G entry builds on it, and the `touches` field of a lesson (core page 5.4) holds these node ids, with the
lesson's `repo` key naming the graph they are in.

**Mechanism**

1. List files (G-08 decides when, and only while the client's `graph` setting is on) with
   `git ls-files --cached --others --exclude-standard`, or with a plain walk outside git or when git fails. Skip dot
   entries, Trail-code's ten build folders, files over 1,000,000 bytes and unclaimed extensions. The dot rule has
   named exceptions, all of them G-06's: the `.github/` folder, and the dot-named files in its gate lists. Pick the
   row by lower-cased extension, parse the whole file (G-01), run its query.
2. A `definition.<kind>` capture with a `name` is a node. Kinds: function, method, class, interface, type, struct,
   enum, module, constant, variable; `field` and `property` become variable, `object` class, the rest function. The
   query files tag a trait as `interface`, so there is no trait kind. The span widens one parent at a time while the
   direct parent's type ends in `definition`, `declaration`, `specifier` or `_item`, and stops at the first parent
   that does not, so a method in a class body keeps its own span.
3. `reference.call` or `reference.send` with `name` is a raw call; `reference.class`, `.interface`, `.implementation`
   and `.module` are raw references. The source is the smallest enclosing definition, or the file for a call. A
   reference outside any definition and a call on a definition's own name are dropped. A recursive call
   therefore produces no `calls` edge (decided): a built graph holds no `calls` edge from a symbol to itself, G-12
   expects a count of 0, and M-07 gives that difference from Trail-code the verdict `deliberate`.
4. New captures: `import.source`, `import.name`, `receiver` (member call), `extends` (a base type in a class
   header, which G-03 reads for parents), `params`, `args` (G-04), `sql.string` (G-05), `comment` (G-07).
5. Cache each file's result (its nodes and raw references, before resolution) in
   `graph/<repo-key>/parse/<hh>/<sha>.json`, keyed by the file's SHA-256 (G-08); `<hh>` is the first two hex
   characters of `<sha>`. A cache file names the extractor version that wrote it, the same version G-08 keeps in
   `files.json`, and a build of another version ignores it. Resolve (G-03), then write
   `graph/<repo-key>/graph.jsonl`, one JSON object per line: meta, nodes by id, edges by source, relation and
   target, then `ext` lines. Sort by byte value and write no timestamp, so equal input gives equal bytes. A
   linked git worktree has its main repository's key, so it reads and fills that same `parse/` cache; its
   `graph.jsonl` goes to its own subfolder `graph/<repo-key>/wt-<8 hex>/`, whose layout is G-08's (step 1 there).
6. `<repo-key>` is the repository key of core page 5.3, `r_` plus 8 hex characters. F-01 owns its formula. It is
   the value an event carries as `repo`, a lesson carries as `repo`, and the meta line carries as `repo`.
7. Phases (decided). Phase 4 ends with the first languages of the table below. Each remaining language ships in
   phase 5, one at a time, when it has a query file and passes its fixtures (core page section 8). Extraction has
   one path, the query file: Trail-code's hand-written walker is not ported, so a language without a query file
   waits and its files are skipped as unclaimed extensions until one is written.

| Part | Definition |
|---|---|
| meta line | keys in order: `t`, `v` (1), `repo` (the repository key), `nodes`, `edges`, `languages` (per language `files`, `nodes` with file nodes counted, `calls` as raw call captures before resolution), `dropped` (`ambiguous`, `untyped`, `unknown`), `errors`. Keys that later entries add follow `errors` in this order: `sql` (G-05), `skipped` (G-06), `scopes` (G-10) |
| node line | keys in order: `t`, `id`, `name`, `kind`, `path`, `span` (first and last line), `lang`, `owner`, `sig`, `params` (G-04), `exported`, `hash` (first 16 hex characters, proposed, of the SHA-256 of the definition's bytes). Keys that later entries add follow `hash` in this order: `defined` (G-05), `level` (G-06), `tag` and `text` (G-07), `origin` (G-11). A key with no value is left out |
| edge line | keys in order: `t`, `source`, `relation`, `target`, `confidence`, `score`, `rule`, `lines` (reference lines, sorted, at most 20, proposed; absent on `contains`), `argc` (G-04; only on a `calls` edge from a language G-04 counts) |
| ext line | keys in order: `t`, `source`, `relation`, `name`: an import or base type that is not in the repository |
| `relation` | the six G-12 allows from this entry and G-03: `contains`, `calls` (a raw call), `imports`, `extends` (an `extends` capture), `implements` (a `reference.implementation` capture) and `references` (every other raw reference). Trail-code's query-file tier writes the last three all as `references` (`generic.ts` L651); splitting them is new. Later entries add their own (G-05, G-06, G-07) |
| `id` | a file (kind `file`): its repository-relative path with `/`. A symbol: `<path>#<name>`, or `<path>#<Owner>.<name>` when owned. A repeated id gets `~2`, then `~3`, in document order |
| `owner` (new) | the id of the enclosing class, interface, struct, enum or module in the same file. A function owned by a class-like node becomes kind `method` |
| first languages (proposed) | `go`, `python`, `typescript` (also JavaScript files), `tsx` (also JSX files; both as in Trail-code's `src/graph/extract.ts` L32), `java`, `c_sharp`, `rust`, `c`, `cpp`, `ruby`, `php`, `kotlin`: these ship in phase 4. The other 12 of G-01's list ship in phase 5, each when it has a query file and passes its fixtures (step 7). A grammar that G-01's result leaves out has no row in either group |

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port: registry, kinds, ids, enclosing owner; 8 of its 17 query files, used as they are | [`src/graph/generic.ts` L47](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L47): one row per language. [L114](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L114): the kind table. [L627](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L627): an unmapped kind becomes function. [L743](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L743): the widening loop climbs only while the direct parent matches. [L300](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L300): id and `~N`. [L259](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L259): smallest enclosing span. [L728](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L728): span widening. [`src/graph/queries/java.scm` L1](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/queries/java.scm#L1): the capture style the query files follow |
| Trail-code (MIT) | port: listing limits; idea: sorted output with no timestamp | [`src/ingest/fs.ts` L28](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ingest/fs.ts#L28): 1,000,000 bytes, compared with `>` at L277; the skipped folders at L9. [`src/graph/write.ts` L50](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/write.ts#L50): it sorts with `localeCompare`, which depends on the host, so we sort by byte value |
| tree-sitter grammar repositories (MIT) | `queries/tags.scm` for go, python, typescript, tsx | Trail-code reads these from an npm package at run time ([`generic.ts` L155](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L155)); they are not in its repository. Web, not machine-checked |
| tentaqles (new) | owner-qualified ids, the added captures, the JSON lines file, per-language counters | this entry |

**How to build it**

- `internal/lobe/graph` (`types.go`, `store.go`), `graph/lang` (`registry.go`, `queries/`), `graph/extract`.
- Of the rest of tentaqles these packages import only what core page 5.1 allows (`internal/resolve`,
  `internal/paths`, `internal/registry`, `internal/manifest`). They run `git` as a child process under a deadline
  of their own and do not import `internal/gitcfg`. They have no `init` function and no package-level call: the
  registry is a table built on first use.
- The repository key comes from F-01's function in the store package. This package never derives it, so the graph
  folder, the meta line and S-01's events cannot drift apart.
- Copy Trail-code's query files for java, c, cpp, c_sharp, ruby, rust, php and kotlin unchanged (`NOTICE`, F-07) and
  keep the added captures in `queries/<lang>.tq.scm`. Strip the seven editor predicates it strips (`generic.ts` L140).
  Emit one node per start offset when a query tags a node twice (L298).
- Fail loudly. Trail-code swallows a failed grammar load and query compile (L175, L181) and still lists the language
  as indexed (`src/graph/build.ts` L340). We compile every query in a unit test, write run-time failures to `errors`
  and count `calls` per language. A capture nothing reads fails a test, except `doc`, `ignore` and `local.scope`,
  which the copied files carry. Trail-code's `solidity.scm` L43 has another one, `reference.unknown`.
- Offsets are UTF-8 bytes and lines are 1-based. Trail-code's offsets index a JavaScript string in UTF-16 units
  (L311), and it counts a trailing empty line in a file's span (L245).
- Not copied: bare-name ids (two methods `run` in a file are `run` and `run~2` there), body text (up to 5,000
  characters per symbol there), and the walker that guesses definitions from node types (L659). `graph.jsonl` holds
  no source text: search is R-01's, whose index keeps term counts that G-08's build takes from the file's bytes.
- No walker, in either phase (step 7). A phase 5 language is added as one registry row, one query file (Trail-code's
  where it has one, used as it is; otherwise ours) and one fixture with a golden file in
  `TestLobeGraphExtractFixtures`. Until all three exist the language has no row.
- `sig` is the definition's first line without a trailing `{` (L305) until G-04 replaces it, passed through the
  scrubber (S-05) because a default value can hold a secret. `exported` is false for a lower-case Go name and a
  Python name starting with `_`. Write through F-01's atomic write under the store lock.

**How we know it works**

- `TestLobeGraphQueriesCompile`: 12 of 12 query pairs compile; 0 capture names outside the read list and the three
  ignored ones.
- `TestLobeGraphExtractFixtures`: per language, node ids, owners and raw references equal a golden file: 12 of 12.
- `TestLobeGraphDeterministic`: two builds with the file order reversed give the same bytes: yes.
- `TestLobeGraphOffsetsAndCap`: the span after a non-ASCII comment is right; a 1,000,001-byte file is skipped.
- `TestLobeGraphRepoKeyIsTheStores`: the graph folder's name, `repo` on the meta line and `repo` in an event that
  S-01 records in the same repository are one string, `r_` plus 8 hex characters: yes.
- `TestLobeGraphRecursiveCallNoEdge`: a function that calls itself and one other function gives 1 `calls` edge,
  to the other function, and 0 from the symbol to itself.
- `TestLobeGraphNoQueryFileNoRow`: every registry row names a query file that exists and compiles, and a file of
  a language with no row gives 0 nodes: yes.
- The count 12 is the first-language list, which is where phase 4 ends; a language that G-01's result leaves out
  lowers it in both tests, and each language added in phase 5 raises it by one.

**Tester brief**

Start from the standard tester setup in F-07. In the `acme` client folder (`$W/clients/acme`) run `git init shop`.
Create `shop/app.py` with six lines, four spaces per indent level: `class Loader:`, `    def run(self):`,
`        return 1`, an empty line, `def main():` and `    return Loader()`. Run `tentaqles lobe graph build` in
`shop`. Expected: exit code 0 and exactly one folder under `$TQ_HOME/identities/acme/lobe/graph/`, named `r_` plus
8 hex characters; `<K>` is that name. The checks read `graph.jsonl` in that folder.

1. Line 1 starts with `{"t":"meta","v":1,"repo":"<K>",` and contains `"python":{"files":1,"nodes":4,"calls":1}`
   (the file, the class, the method and `main`; the one call is `Loader()`).
2. One line contains `"id":"app.py#Loader.run","name":"run","kind":"method","path":"app.py","span":[2,3]`.
3. Record the file's SHA-256, run the build again and compare. Expected: the same hash.

**Risks and open questions**

- The `tags.scm` files for go, python, typescript and tsx were not read by machine. G-01's measure 4 covers them.
- A `~N` id shifts when an earlier same-named definition is deleted, so a lesson's `touches` can go stale (R-06).
- A Go method is not written inside its type, so its owner needs a receiver capture in `go.tq.scm` (new, unproven).
- If F-01's spike picks a database for the caches, the line shapes stay as the logical record.
- The parse cache grows by one file per content hash and nothing in this entry removes one. `tentaqles lobe rebuild`
  (F-01) empties it; pruning during a build is G-08's to decide.

## G-03 Cross-file resolution with honest confidence

**Phase** 4 · **Origin** port: Trail-code, Graphify · **Depends on** G-02, G-04

**What it is.** G-02 leaves references as bare names: "`main` calls something named `parse`". Resolution decides which
node that is, or decides that it cannot know. Each edge carries a confidence label (`extracted`: the source text
states it; `inferred`: it is the only candidate by name), a score and the name of the rule that made it.

**Why it matters.** Blast radius (G-09) and the argument check (E-05) act on edges. A wrong edge marked certain is
worse than a missing one: Trail-code's helper `make` once collected 1,040 in-edges from 476 files (`resolve.ts` L46).

**Mechanism**

1. Index nodes by name, by file then name, and by owner id then name. Read class parents from raw `extends`.
2. For each raw reference apply the first rule that fits; "exactly one" means after filtering by allowed kind. An
   ambiguous name is dropped, not guessed: no rule picks "the first", "the nearest" or "the most likely" candidate.

| # | Applies to | Rule | Result: label, score, `rule` |
|---|---|---|---|
| 1 | containment | file or owner to its definition | extracted, 1, `contains` |
| 2 | import | the module text resolves to a file in the repository | extracted, 1, `import`; otherwise an `ext` line and no edge |
| 3 | bare name | exactly one definition in the same file | extracted, 1, `same-file` |
| 4 | bare name | the file imports that name from a module resolved by rule 2, and that file has exactly one such definition | extracted, 1, `import-symbol` |
| 5 | member call on `self`, `this` or `cls` | exactly one method of that name on the enclosing owner, else on a parent up to 3 levels | same file: extracted, 1; other file: inferred, 0.85; `self-member` |
| 6 | member call on an imported module or package name | exactly one definition of that name in that module's file (Go: in the package folder) | extracted, 1, `import-member` |
| 7 | member call on a type name | the type resolves by rule 3, 4 or 9 and has exactly one such method | the type's label and score, `owner-member` |
| 8 | member call on anything else | no receiver type is known | dropped, counted `untyped` |
| 9 | bare name outside TypeScript and JavaScript | exactly one definition in the repository, in the same language family (Go: same folder) | inferred, 0.85, `unique-name` |
| 10 | bare name, several candidates, caller not a test file | exactly one candidate is left after removing those in test files | inferred, 0.75, `unique-nontest` |
| 11 | anything left | no rule fits | dropped, counted `ambiguous` (two or more candidates) or `unknown` (none) |

3. Kinds: a call looks for a function or method, then, if no node of that name has those kinds, for a class, struct or
   enum (a constructor). Other references look for a class, interface, struct, enum, type or module.
4. Overloads: of same-named methods on one owner, exactly one whose counts fit the call (G-04) gives an edge.
5. Families: `typescript` with `tsx`; `java`, `kotlin`, `scala`, `clojure`; `c` with `cpp`; all others alone.
6. Remove duplicates on source, relation and target, keeping the higher score.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port: rules 3, 8 and 9, families, the Go folder rule | [`src/graph/resolve.ts` L395](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L395): same file needs a unique match. [L405](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L405): unique across files is `inferred`. [L297](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L297): a member call with no receiver type is dropped. [L55](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L55): families. [L411](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L411): Go |
| Trail-code (MIT) | not copied: four places where its code guesses or mislabels | [L478](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L478): the first same-file overload is returned as `extracted`. [L447](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L447): when no overload fits, all are kept. [L237](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L237): an unresolved base class stays as a bare name labeled `inferred`. [L230](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L230): an unresolved import is labeled `extracted` |
| Graphify (Apache-2.0) | port: rules 4 and 10, the JavaScript exception, the scores. Not copied: nearest-path tie-breaks | [`graphify/extract.py` L12125](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L12125): a unique imported symbol wins. [L12232](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L12232): a JavaScript or TypeScript call with no import gets no edge. [L12248](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L12248): 0.85, and 1.0 at L12243. [`graphify/paths.py` L441](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/paths.py#L441): test candidates are removed for a non-test caller. [L362](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/paths.py#L362): same folder, then the longest shared path prefix, which is a guess |
| tentaqles (new) | rules 5 to 7 from the `receiver` capture, rule names on edges, the drop counters, the 0.75 score | this entry |

**How to build it**

- `internal/lobe/graph/resolve`: `resolve.go` (pure:
  `Resolve(nodes []Node, raw []RawRef, mods []GoModule) (edges []Edge, ext []Ext, dropped Dropped)`), `imports.go`
  (one import resolver per language), `families.go`, `testpath.go`.
- A Go import targets the package folder's lowest-sorted non-test file; Trail-code includes tests (`resolve.ts` L724).
- Owner keys are node ids; Trail-code keys methods by bare class name (L173), so same-named classes merge. The 3-level
  parent limit is its own (L459).
- Rule 10 is kept (decided). It uses Graphify's test-file patterns (`paths.py` L261 to L284), where a production
  folder named `spec` or `test` counts as tests. The rule can only add an edge at 0.75, so the harm is bounded.
  `testpath.go` is the lobe's one definition of a test path; G-09 sets tests apart with it.
- E-05 needs the target of single calls inside a hook: it runs at PostToolUse after an edit, on the one-file parse
  G-09 makes (core page 5.2), where the whole graph is never loaded. So the rules read nodes through a small lookup
  interface (by name, by file and name, by owner and name). The build passes in-memory tables, and a hook passes
  lookups served by the index G-08 writes under `graph/<repo-key>/`; nothing inside a hook loads `graph.jsonl`.
  Both run the same rule table, so an edge in the graph and an answer in a hook cannot disagree.
- 1 and 0.85 are Graphify's values; 0.75 is proposed, from the same five-step scale (`extract.py` L12212). This entry
  only labels: G-09 and E-05 choose the lowest score they act on.

**How we know it works**

- `TestLobeResolveAmbiguousDropped`: `a.py` and `b.py` define `parse` and `c.py` calls it: 0 edges, `ambiguous` is 1.
- `TestLobeResolveOverloadNoFit`: a Java class with `f(int)` and `f(int,int)` and a call `f(1,2,3)`: 0 edges.
- `TestLobeResolveNoCrossFamily`: a Go file calls `make()` and a TypeScript file defines `make`: 0 edges.
- `TestLobeResolveEveryEdgeHasRule`: 100% of edges name a rule from the table, and both ends are nodes.

**Tester brief**

Start from the standard tester setup in F-07. In the `acme` client folder (`$W/clients/acme`) run `git init shop`
and create seven files in `shop` (indent: four spaces). `a.py` and `b.py`: `def parse():`, `    return 1`. `c.py`:
`def main():`, `    return parse()`. `e.py`: `from a import parse`, an empty line, `def go():`,
`    return parse()`. `f.py`: `def only_here():`, `    return 4`. `g.py`: `def use():`, `    return only_here()`.
`x.py`: `def obj_call(o):`, `    return o.parse()`. Run `tentaqles lobe graph build` in `shop` and read
`graph.jsonl` in the one folder under `$TQ_HOME/identities/acme/lobe/graph/`.

1. No line contains `"source":"c.py#main","relation":"calls"` or `"source":"x.py#obj_call","relation":"calls"`.
2. One line starts with `{"t":"edge","source":"e.py#go","relation":"calls","target":"a.py#parse",` followed
   directly by `"confidence":"extracted","score":1,"rule":"import-symbol","lines":[4]`. An `argc` key (G-04) may
   end the line.
3. One line contains `"source":"g.py#use","relation":"calls","target":"f.py#only_here",` followed directly by
   `"confidence":"inferred","score":0.85,"rule":"unique-name"`.
4. Line 1 contains `"dropped":{"ambiguous":1,"untyped":1,"unknown":0}`.

**Risks and open questions**

- With no receiver types, most method calls in object-oriented code fall under rule 8. Trail-code's comment at L306
  says a name fallback there cut precision from 73% to 37%; we did not check that measurement. M-07 measures ours.
- Rule 9 can be fooled by a local name that shadows a function elsewhere; M-07 decides whether Python joins TypeScript
  outside it (`extract.py` L12221). Rule 4 misses re-exports, and path aliases stay `ext`.
- Rule 10 is the only rule that filters by a naming habit (test-file names). Its edges carry `unique-nontest` and
  0.75, so a reader can discount them, and M-07's hand-checked sample shows how often the habit misleads.

## G-04 Signatures and argument counts

**Phase** 4 · **Origin** new; partial port: Trail-code, Graphify · **Depends on** G-02, S-05

**What it is.** For each function and method the graph stores how many arguments it takes: the number of declared
parameters, the smallest number a call may pass, whether it accepts any number, and the header as one line of text.
Each call stores how many arguments it passes.

**Why it matters.** The table of mistakes in core page section 4 promises that a wrong number of arguments to a
function in the repository is caught from the graph with no learning (E-05). E-05 reads these counts at PostToolUse,
after an edit, on the parse G-09 already made (core page 5.2); it prints a note and never denies. Both sources are
thin here. Trail-code keeps the header text for every language but a count only for Java methods and constructors and
for Swift; Graphify keeps counts only for C#. Neither stores parameter names or defaults as fields or reports a
mismatch: both use counts only to pick an overload, and both guess when nothing fits. The counting rules for Python,
TypeScript and Go are ours.

**Mechanism**

1. Each first-language `.tq.scm` captures `params` on a definition's parameter list and `args` on a call's arguments.
2. Classify each parameter by node type with a per-language table: receiver (`self`, `cls`, a Go receiver, a
   TypeScript `this` parameter), required, optional (has a default or `?`), variadic (`*args`, `...rest`, `...T`,
   `params`), keyword collector (`**kwargs`).
3. Store `params` on the node, four keys in this order. `n` counts required plus optional parameters, `min` the
   required ones; `variadic` says whether a variadic parameter exists. Receivers and collectors are not counted.
4. The fourth key, `check`, is false when the counts must not be used to judge a call: a keyword collector, a Python
   decorator other than `@staticmethod` or `@classmethod`, TypeScript overload signatures, any JavaScript file.
5. `sig` becomes the text from the start of the definition to the start of its body, with white space collapsed and a
   trailing `{`, `:`, `=` or `=>` removed, cut at 300 characters (proposed) and passed through the scrubber (S-05).
6. A call's argument count goes into the edge's `argc` list, parallel to `lines`. The value is -1 when it cannot be
   counted: a spread or unpacked argument (`*xs`, `**kw`, `...xs`), or in Go a single argument that is itself a call.
7. The fit rule, the only one G-03 and E-05 use: a call fits when `min <= argc` and (`argc <= n` or `variadic`).
8. The first signature set is Python, TypeScript with TSX, Go, Java and C# (decided; C# does not wait for a second
   set). JavaScript gets counts with `check` false. Every other language gets `sig` only and no `params`, which
   means "never checked".
9. What E-05 reads of this entry: `params` (`n`, `min`, `variadic`, `check`) and `sig` on the target's node, and the
   fit rule of step 7. It counts the arguments of the call it judges from the fresh parse of the edited file, with
   the counter of step 6, so a stored `argc` and a count taken in the hook cannot differ for the same text.
   E-05 has no counting code of its own. It is not a lesson, holds no ladder entry and writes no trial line. A
   language's notes are switched on only after its counting table meets M-05's two numbers on replayed edits
   (proposed).

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port: header text; Java counting | [`src/graph/extract.ts` L563](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L563): the header slice, cleaned at L2627. [L1677](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L1677): Java count, variadic at L1678. [L713](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L713): call counts for Java and Swift only |
| Trail-code (MIT) | not copied: a stale comment and a guess | [`src/graph/resolve.ts` L429](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L429): says only Java emits counts, yet Swift sets them at `extract.ts` L1919. [L447](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L447): when no candidate fits, every candidate is returned |
| Graphify (Apache-2.0) | port: C# rules and the "exactly one fits" pick | [`graphify/extractors/engine.py` L226](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L226): a parameter without `=` is required. [L242](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L242): an extension method needs one fewer. [L289](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L289): one fit or none |
| Graphify (Apache-2.0) | not copied: a guess | [`engine.py` L10609](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L10609): with no unique fit, the call binds to the first declared overload |
| tentaqles (new) | counting tables for Python, TypeScript and Go; `check`; the fit rule; `argc` of -1 | this entry |

**How to build it**

- `internal/lobe/graph/extract`: `params.go` (`Params{N, Min int; Variadic, Check bool}`,
  `CountParams(lang string, list Tree) (Params, bool)` and `CountArgs(lang string, list Tree) int`, which returns -1
  for step 6's cases), `params_table.go` (node type to class, per language) and `sig.go`.
  `internal/lobe/graph/types.go` exports `Fits(p Params, argc int) bool`, the single copy of the fit rule.
- E-05 keeps no counter and no fit rule of its own: it calls `CountArgs` on the `args` capture of G-09's one-file
  parse and `Fits` on the stored `params`. A node without `params`, a `check` of false, or an `argc` of -1 ends
  its check for that call, so a language outside the first signature set is never judged.
- Python: the first parameter of a method is the receiver unless the method is a `@staticmethod`. The `/` and `*`
  markers are not parameters. Keyword arguments at a call count toward `argc`.
- TypeScript: `x?: T` and `x = v` are optional; several body-less declarations of one name are overload signatures.
- Go: there are no defaults, so `min` equals `n`. `f(g())` may pass several values, which is why it is not counted.
- Java: count `formal_parameter` and `spread_parameter` children as Trail-code does; a spread one sets `variadic`. C#:
  `params` sets `variadic`, and an extension method called as a member needs one argument fewer.
- A call that G-03 resolved to a class (a constructor) is not checked in this version.
- Where we differ: Trail-code counts the variadic parameter and accepts `argc >= arity - 1` (`resolve.ts` L445);
  leaving it out of `n` and `min` gives the same answer with one rule. Its query-file tier keeps only the first
  physical line of a header (`src/graph/generic.ts` L305), which cuts multi-line headers.

**How we know it works**

- `TestLobeParamsTable`: 8 headers for each of 5 languages; 40 of 40 give the expected `n`, `min`, `variadic`,
  `check`.
- `TestLobeParamsFits`: 12 cases of the fit rule, 12 of 12.
- `TestLobeSigMultiLine`: a header spread over 3 lines is stored as one line: yes.
- `TestLobeArgcNotCountable`: `f(*xs)`, `f(...xs)` and Go `f(g())` each store -1: 3 of 3.
- `TestLobeArgcSameInHookAndBuild`: for 10 calls, `CountArgs` on a one-file parse equals the `argc` the build
  stored for the same text: 10 of 10.
- `TestLobeParamsAbsentWhenUnsupported`: a Ruby method has `sig` and no `params`: yes.

**Tester brief**

Start from the standard tester setup in F-07. In the `acme` client folder (`$W/clients/acme`) run `git init shop`.
Create `shop/sig.py` with ten lines: `def load(path,`, `         limit=10, *rest):`, `    return path`, an empty
line, `def any_kw(a, **kw):`, `    return a`, an empty line, `def caller(xs):`, `    load("a", 1)` and
`    load(*xs)`. Run `tentaqles lobe graph build` in `shop` and read `graph.jsonl` in the one folder under
`$TQ_HOME/identities/acme/lobe/graph/`.

1. The line with `"id":"sig.py#load"` contains `"span":[1,3]` and `"sig":"def load(path, limit=10, *rest)"`.
2. The same line contains `"params":{"n":2,"min":1,"variadic":true,"check":true}`.
3. The line with `"id":"sig.py#any_kw"` contains `"params":{"n":1,"min":1,"variadic":false,"check":false}`.
4. Exactly one line contains `"source":"sig.py#caller","relation":"calls","target":"sig.py#load"`, and it ends with
   `"lines":[9,10],"argc":[2,-1]}`.

**Risks and open questions**

- Dynamic languages change signatures at run time (decorators, patched methods, `functools.partial`); `check` covers
  only what we can see. How often the check is wrong on real code is E-05's and M-05's number.
- The counting tables are ours and unproven. The harm of a wrong count is bounded: E-05 prints a note after the edit
  and never denies (core page 5.2), and in G-03 a wrong count can only lose or misplace an edge among same-named
  overloads on one owner. E-05 holds no counting code, no ladder entry and no trial line of its own, and a
  language's notes are switched on only after its counting table meets M-05's two numbers on replayed edits
  (proposed).
- Node type names differ between grammar versions, so the tables are pinned to the revisions G-01 fixes.
- C# is in the first signature set on Graphify's rules, the only source that counts for it. `TestLobeParamsTable`
  is the one check on those rules until M-05 reports E-05's number.

## G-05 SQL in the graph

**Phase** 4 · **Origin** port: Graphify · **Depends on** G-01, G-02, G-03

**What it is.** SQL files become nodes for tables, views, routines (functions and procedures), triggers and indexes,
and edges that say which view, routine, statement or code function reads or writes which table. Everything comes from
parsing and pattern matching. No model is called.

**Why it matters.** Recalling a lesson when SQL is about to run (R-04) needs the tables a command touches; "what
breaks if I change this table" (G-09) needs their readers and writers. Data engineers and analysts are in the promise.

**Mechanism**

1. Route `.sql` files here. Replace each line that holds only `GO` (the T-SQL batch separator, with an optional count)
   by `;` padded to the same byte length, so offsets and line numbers hold. Parse with the `sql` grammar.
2. Register the name of every created table, view and routine before walking, so order in the file does not matter.
3. Walk: `CREATE TABLE` gives a `table` node and a `references` edge per foreign key (`ALTER TABLE ... ADD CONSTRAINT`
   too); `CREATE VIEW` a `view` node and a `reads_from` edge per `FROM` or `JOIN` table, except `WITH` names in scope;
   `CREATE FUNCTION` or `PROCEDURE` a `routine` node with the same reads; `CREATE TRIGGER` a `trigger` node with
   `triggers` to its table and `executes` to its routine; `CREATE INDEX` an `index` node with `indexes` to its table.
4. New: a top-level `SELECT`, `INSERT`, `UPDATE`, `DELETE` or `MERGE` gives `reads_from` and `writes_to` edges from
   the file node. Graphify's walk has no branch for these.
5. If the tree has an error node, run the recovery scan. Blank comments and single-quoted strings to spaces, keeping
   every offset. Patterns then find the names after `CREATE [OR REPLACE|ALTER] FUNCTION|PROC|PROCEDURE|TRIGGER|TABLE`
   and, inside each routine body, the read targets after `FROM` and `JOIN` and the write targets after `INSERT`,
   `UPDATE`, `DELETE`, `MERGE` and `INTO`. `UPDATE o ... FROM dbo.Orders o` is resolved through the alias.
6. New: SQL in code. `sql.string` captures string literals. A string counts as SQL only if, after leading white space
   and ignoring case, it has one of five shapes (proposed): `SELECT ... FROM x`, `INSERT INTO x`, `UPDATE x SET`,
   `DELETE FROM x`, `MERGE INTO x`. The body scanner of step 5 then gives edges from the enclosing function.
7. Names: strip a pair of backticks that wraps a whole dotted path (new), split on `.`, strip one pair of quotes,
   backticks or brackets per part, and lower-case. The node id is `sql:<name>`, one node per table for the whole
   repository. A bare name resolves to a schema-qualified table only when exactly one schema has it. A table that
   is used but never created is a node with `"defined":false`.
8. Labels: tree-walk edges are extracted, 1, `sql-ddl` or `sql-statement`. Recovery edges are inferred, 0.85,
   `sql-recovery`. Code-string edges are inferred, 0.75, `sql-string`. The last two scores are proposed.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Graphify (Apache-2.0) | port: the walk, `GO` handling, name keys | [`graphify/extractors/sql.py` L549](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L549): the walk starts with `create_table`. [L482](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L482): `GO` becomes `;` at equal length. [L449](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L449): the lower-cased key. [L874](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L874): a bare-name alias only when unique |
| Graphify (Apache-2.0) | port: the recovery scan | [`sql.py` L113](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L113): the masker. [L296](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L296): the write verbs. [L342](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L342): the only place `writes_to` is made. [L933](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L933): it runs only after a failed parse |
| Graphify (Apache-2.0) | not copied: three weak spots and reading a live database | [`sql.py` L696](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L696): a trigger links only to a routine defined earlier in the file. [L467](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L467): a missing grammar gives an empty result. [L519](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/sql.py#L519): every edge is `EXTRACTED`, pattern-recovered ones too. [`graphify/pg_introspect.py` L153](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/pg_introspect.py#L153): reading a live database needs a login and a network connection, which principle 7 rules out |
| tentaqles (new) | node kinds, `sql:` ids, top-level statements, SQL strings in code, a label per path | this entry |

**How to build it**

- `internal/lobe/graph/sqlgraph`: `mask.go`, `gosep.go`, `walk.go`, `recover.go`, `refs.go`, `names.go`.
- New kinds `table`, `view`, `routine`, `trigger`, `index` (Graphify has no kind field, `sql.py` L511). `path` and
  `span` point at the `CREATE` statement, the first by sorted path if several, and the file `contains` the node.
- Port as data the list of words that follow `FROM` or `INTO` without naming a table (`sql.py` L313).
- With no usable `sql` grammar (G-01's result may leave it out), run step 5 on every file with all edges inferred;
  Graphify returns nothing then.
- Dialect fixtures, first set (proposed): Spark SQL as Databricks runs it (one folder), PostgreSQL, T-SQL and
  BigQuery. Each is a folder of `.sql` files under `sqlgraph/testdata/<dialect>/` with a golden list of nodes and
  edges and a golden `recovered` count, so a grammar change that pushes a dialect into recovery shows as a diff.
- BigQuery and Spark quote with backticks, and BigQuery may put a whole path inside one pair, as in
  `` `shop.sales.orders` ``. Step 7 strips that outer pair only when no other backtick is inside it, then splits on
  `.` and strips a pair per part.
- Holes copied knowingly, in recovery only: PostgreSQL dollar-quoted bodies and MySQL double-quoted strings are not
  blanked (L86), and a string continued on the next line is scanned as code (L103).
- Where we differ: Graphify lower-cases every name, so PostgreSQL `"Users"` and `users` merge; we keep the case of a
  double-quoted part with an upper-case letter (proposed). It registers only tables and views ahead (L838).
- Only names are stored, never the text of a statement or string (principle 6). Line 1 gains
  `"sql":{"files":N,"recovered":M}` to show the share of files that fell to recovery.

**How we know it works**

- `TestLobeSQLTriggerOrder`: a trigger written before its function: 1 `executes` edge.
- `TestLobeSQLRecoveryNoFabrication`: 6 broken files with `CREATE` text in comments and quoted strings: 0 extra nodes.
- `TestLobeSQLStringShapes`: 10 SQL strings give edges; 10 English sentences that start with a SQL verb give 0.
- `TestLobeSQLCTENotATable`: `WITH levels AS (...) SELECT ... FROM levels`: no node `sql:levels`.
- `TestLobeSQLDialectFixtures`: the 4 dialect folders of the first set give their golden nodes, edges and
  `recovered` counts: 4 of 4.
- `TestLobeSQLBacktickPath`: `` `shop.sales.orders` `` and `` `shop`.`sales`.`orders` `` give the same node id: yes.

**Tester brief**

Start from the standard tester setup in F-07. In the `acme` client folder (`$W/clients/acme`) run `git init shop`.
Create `shop/db/schema.sql`, three lines: `CREATE TABLE b.orders (uid int REFERENCES a.users(id));`,
`CREATE TABLE a.users (id int);` and `INSERT INTO a.users SELECT uid FROM b.orders;`. Create `shop/etl.py`, three
lines: `def load(cur):`, `    cur.execute("UPDATE a.users SET id = 1")` and
`    print("Update your profile from settings")`. Run `tentaqles lobe graph build` in `shop` and read `graph.jsonl` in
the one folder under `$TQ_HOME/identities/acme/lobe/graph/`.

1. One line contains `"id":"sql:a.users","name":"a.users","kind":"table","path":"db/schema.sql","span":[2,2]`.
2. One line contains `"source":"sql:b.orders","relation":"references","target":"sql:a.users",` followed directly by
   `"confidence":"extracted","score":1,"rule":"sql-ddl"`.
3. Two lines have `"source":"db/schema.sql"` and `"rule":"sql-statement","lines":[3]`: one with
   `"relation":"reads_from","target":"sql:b.orders"`, one with `"relation":"writes_to","target":"sql:a.users"`.
4. One line contains `"source":"etl.py#load","relation":"writes_to","target":"sql:a.users",` followed directly by
   `"confidence":"inferred","score":0.75,"rule":"sql-string"`.
5. No node id contains `your`, `profile` or `settings`, and no line contains `SET id`.
6. Line 1 contains `"sql":{"files":1,"recovered":0}`: the file parsed cleanly, so checks 2 and 3 did not come from
   the recovery scan.

**Risks and open questions**

- One general SQL grammar serves every dialect, and dialect syntax falls to recovery, where edges are only inferred.
  How often that happens in a client repository is unknown until the `recovered` count is read.
- SQL built from joined strings, an ORM or dbt's `ref()` is not seen. Same-named tables of two databases merge.
- MySQL is outside the first fixture set, so its syntax is covered only by the general grammar and the recovery
  scan until a fixture is added.

## G-06 Documents and configuration in the graph

**Phase** 4 · **Origin** port: Graphify · **Depends on** G-01, G-02, G-03

**What it is.** Markdown, YAML, JSON and Terraform files get nodes too: a heading, a configuration key, a Terraform
block. Links between documents, and code names mentioned in prose, become edges. Scanners and parsers do all of it; no
model is called. PDFs and images are G-11. This entry needs only the `graph` setting on; the `documents` setting of
core page 5.6 switches G-11 alone.

**Why it matters.** It serves "fewer tokens": "where is the deploy job defined" and "which document describes
`upsert`" get a path and a line range, where today the agent reads a whole README or workflow file.

**Mechanism**

1. Markdown (`.md`, `.mdx`, `.markdown`, `.qmd`). Front matter (`---` on the first line, closed within 200 lines) has
   no headings. A fenced block (3 or more backticks or tildes; the closer is the same character, at least as long) is
   skipped. A heading line (at most 3 spaces, 1 to 6 `#`, space, text, a closing `#` run removed) is a `heading` node
   under the nearest shallower heading or the file. Its span stops before the next heading of its level or shallower.
2. Links, reference definitions and `[[wikilinks]]` give one `references` edge per target, from the file: extracted,
   1, `doc-link`. URLs are skipped. New: a `#anchor` retargets the edge to the heading with that slug, if exactly one.
3. A code span in prose is a mention from its heading, resolved last (`doc-mention`): with a path
   (`etl/load.py::upsert`) extracted, 1; a bare name with exactly one such function or method inferred, 0.95; else no
   edge.
4. YAML (`.yaml`, `.yml`), configuration only: a known file name (29), a file under `.github/workflows/` or a known
   top-level key; not OpenAPI or JSON schemas. Each mapping key is a `key` node. Limits: 1 MiB, 500 keys, depth 6.
5. JSON (`.json`, `.jsonc`): the same idea and limits; data files give no nodes. Each entry of a dependency block in
   `package.json` gives an `imports` edge from the file to a `dependency` node.
6. Terraform (`.tf`, `.tfvars`, `.hcl`): each block is a `block` node named by its address (`aws_s3_bucket.logs`,
   `module.<name>`, `var.<name>`), per folder. A reference to another address gives `references`, or `depends_on`
   inside that attribute (`tf-ref`); `count`, `each`, `self`, `path` and `terraform` heads do not count.
7. Ids: a heading is `<path>#<slug>` (lower case, spaces to `-`, punctuation dropped, `~N` on collision); a key is
   `<path>#<full.key.path>` with `[i]` for list items; a block is `tf:<folder>#<address>`.
8. Values are never stored for YAML, JSON or Terraform: keys, addresses and lines only.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Graphify (Apache-2.0) | port: the Markdown scanner and mention scores | [`graphify/extractors/markdown.py` L65](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L65): the heading pattern. [L71](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L71): fences. [L109](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L109): 200 lines. [L659](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L659): zero tokens, so no model call. [`graphify/markdown_resolution.py` L208](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/markdown_resolution.py#L208): a mention with a path, 1.0. [L237](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/markdown_resolution.py#L237): bare and unique, 0.95; otherwise none |
| Graphify (Apache-2.0) | port: gates and limits for YAML and JSON; Terraform blocks and references | [`graphify/extractors/yaml_config.py` L74](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/yaml_config.py#L74): the file names; limits at L24, L25 and L71. [`graphify/extractors/json_config.py` L294](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/json_config.py#L294): data JSON is skipped; limits at L95, L169 and L174. [`graphify/extractors/terraform.py` L425](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/terraform.py#L425): block addresses; heads that are not references at L21; folder scope at L77 |
| Graphify (Apache-2.0) | not copied: four weak spots | [`json_config.py` L186](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/json_config.py#L186): a key's id uses only its parent key, so keys merge. [`terraform.py` L32](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/terraform.py#L32): values are stored and redacted by a pattern in which `auth` also hits `author`. [`terraform.py` L242](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/terraform.py#L242): edge targets are never checked. [`markdown.py` L341](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/markdown.py#L341): anchors are cut, so no link reaches a heading |
| hashicorp/hcl v2 (MPL-2.0) | the fallback Terraform parser, as an unmodified Go module | [LICENSE](https://github.com/hashicorp/hcl/blob/main/LICENSE): Mozilla Public License 2.0. [Package page](https://pkg.go.dev/github.com/hashicorp/hcl/v2): the `hclsyntax` parser this entry would call. Web, not machine-checked |
| tentaqles (new) | full-path key ids, links to headings, heading spans, no stored values | this entry |

**How to build it**

- `internal/lobe/graph/docs`: `markdown.go`, `mentions.go`, `yaml.go`, `json.go`, `terraform.go` and `gate.go`. New
  node kinds: `heading` (with `level`), `key`, `block`, `dependency`.
- Markdown is a hand-written line scanner. YAML uses `gopkg.in/yaml.v3`, already in `go.mod`. JSON uses our own small
  scanner that accepts comments and trailing commas (`tsconfig.json` has both; the standard library rejects them).
- Terraform uses the `hcl` grammar on G-01's runtime if the spike shows it works; otherwise
  `github.com/hashicorp/hcl/v2`, pure Go under MPL-2.0 (decided: acceptable). It is imported as a module and never
  modified or copied into the tree; `NOTICE` lists it and its license text ships with the others (F-07). Either
  parser gives the same nodes and edges: `TestLobeDocsTerraformFixtures` holds one golden file for both.
- G-02 skips dot entries. `.github/` is the named exception (decided): the lister descends into it for this entry,
  so workflow files are read. The dot-named files in the gate lists (`.gitlab-ci.yml`, `.travis.yml`,
  `.pre-commit-config.yaml`, `.eslintrc.json` and the like) are picked by exact name at any depth the lister
  reaches. No other dot folder is entered. A link or Terraform edge is written only when its target node exists,
  else as an `ext` line.
- Also not copied: front matter stored on the page node (`markdown.py` L551; it holds values), Markdown sent to a
  model in a full run (`graphify/cli.py` L4575), and list items that reuse the parent's key (`yaml_config.py` L281).
- Heading text and keys are untrusted: cut at 120 characters (proposed), control characters stripped, shown by R-02
  only as pointers under F-05's rules. Files the gates skip are counted on line 1 under `skipped`.

**How we know it works**

- `TestLobeDocsMarkdownFences`: `Setup` with a closing `#` run, `C#`, and a fenced heading line give 2 headings.
- `TestLobeDocsMentionAmbiguous`: two classes `Widget`; a bare mention gives 0 edges, a mention with a path gives 1.
- `TestLobeDocsYAMLKeyPaths`: a workflow with two jobs, each with named steps: 0 duplicate ids.
- `TestLobeDocsNoValuesStored`: `password: hunter2` and `token = "abc123"` appear nowhere in the graph bytes: yes.
- `TestLobeDocsTerraformFixtures`: the Terraform fixture gives its golden blocks and references with whichever
  parser the build uses: yes.
- `TestLobeDocsDotEntries`: `.github/workflows/ci.yml` and a root `.gitlab-ci.yml` give key nodes; a workflow-shaped
  file under `.cache/` gives 0 nodes.

**Tester brief**

Start from the standard tester setup in F-07. In the `acme` client folder (`$W/clients/acme`) run `git init shop`
and create five files in `shop`. `f.py`: `def only_here():`, `    return 4`. `docs/ops.md`: `## Deploy`.
`infra/main.tf`: `variable "region" {}`, `resource "aws_s3_bucket" "logs" {`, `  token = "abc123"`,
`  bucket = var.region`, `}`. `.github/workflows/ci.yml`: `jobs:`, `  build:`, `    steps:`, `      - name: one`.
`docs/guide.md`: `## Setup ##`, ``See `only_here`.``, `[ops]: ./ops.md#deploy`, `~~~`, `# not a heading`, `~~~`.
Run `tentaqles lobe graph build` in `shop` and read `graph.jsonl` in the one folder under
`$TQ_HOME/identities/acme/lobe/graph/`.

1. One line contains `"id":"docs/guide.md#setup","name":"Setup","kind":"heading","path":"docs/guide.md","span":[1,6]`.
2. One line contains `"source":"docs/guide.md","relation":"references","target":"docs/ops.md#deploy",` followed
   directly by `"confidence":"extracted","score":1,"rule":"doc-link"`.
3. One line contains `"source":"docs/guide.md#setup","relation":"references","target":"f.py#only_here",` followed
   directly by `"confidence":"inferred","score":0.95,"rule":"doc-mention"`.
4. One line contains `"id":".github/workflows/ci.yml#jobs.build.steps[0].name"`, and one contains
   `"source":"tf:infra#aws_s3_bucket.logs","relation":"references","target":"tf:infra#var.region"`.
5. No line contains `not a heading` or `abc123`.

**Risks and open questions**

- The gates are name and key lists: an unknown format is skipped and the `skipped` counts are the only sign. TOML, INI
  and Dockerfiles are not covered; `.env` files are never read.
- A bare mention such as `run` resolves whenever one function has that name; the share of wrong mention edges is not
  measured yet. An `[i]` id shifts when a list item is inserted, so a lesson that `touches` it can go stale.
- If the fallback is taken, `hashicorp/hcl/v2` is the graph's only MPL-2.0 dependency. It must stay unmodified: a
  patched copy in the tree would bring MPL duties on those files. Its binary growth is not in G-01's measure 7.

## G-07 Why-comments

**Phase** 4 · **Origin** port: Graphify · **Depends on** G-02, G-08, G-12, S-05

**What it is.** A why-comment is a code comment that starts with a tag such as `WHY:` or `NOTE:` and gives a reason.
The graph builder turns each one into a small node called a note and links it to the function, class or file it
sits on, so the reason travels with the code pointer.

**Why it matters.** It serves "fewer errors". An agent that follows a graph pointer instead of reading the file
never sees the warning written above the code, "fixes" the odd line, and repeats a mistake someone wrote down.

**Mechanism**

1. During extraction (G-02) the builder visits every comment node in the parse tree, in every language with a
   grammar. Graphify scans raw lines, and only for Python and for JavaScript and TypeScript.
2. It strips the comment marker and tests the rest against `^(WHY|NOTE|IMPORTANT|HACK|RATIONALE):\s+\S`: five of
   Graphify's seven tags (`graphify/extract.py` L1823). `TODO` and `FIXME` comments are not notes: they are tasks.
3. The text is the rest of the line plus the untagged comment lines directly below, joined by spaces, scrubbed
   (S-05), then cut at 400 characters (proposed). The name is the text cut to 80 characters on a word boundary.
   The width 80 and scrubbing before cutting are Graphify's (L1826, L1838).
4. Target, by the first rule that fits: a comment after code on its line goes to the innermost symbol whose span
   holds the line; a block that ends directly above a definition or its decorators goes to that definition, also
   inside a class body; any other comment goes to the innermost symbol whose span holds it; else the file node.
5. Output, in G-02's line shapes: a node with `"kind":"note"` and the extra keys `tag` and `text`, and an edge from
   the note with `"relation":"explains"`, `"confidence":"extracted"`, `"score":1`, `"rule":"comment"`. Graphify's
   names are `rationale` and `rationale_for` (L1928, L1935).
6. The id is `<path>#note:` plus the first 8 hex characters (proposed) of the SHA-256 of tag and text, so it
   survives moving lines. Identical notes in one file get the collision suffix G-02 uses.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Graphify (Apache-2.0) | port | [`graphify/extract.py` L1823](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1823): the seven Python prefixes, `# NOTE:` to `# FIXME:` |
| Graphify (Apache-2.0) | port | [`graphify/extract.py` L1928](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1928) and [L1935](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1935): the node type `rationale` and the edge `rationale_for`, marked `EXTRACTED` one line below |
| Graphify (Apache-2.0) | port, changed | [`graphify/extract.py` L1984](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1984): the scan is `startswith` on each stripped line, and L1985 attaches the note to the file node |
| Graphify (Apache-2.0) | port, changed | [`graphify/extract.py` L2552](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L2552): the 14 JavaScript and TypeScript prefixes; [L2642](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L2642) is the same scan, and L2603 again targets the file |
| tentaqles (new) | new | Comment nodes from the parse tree, the innermost-symbol target, the content-based id |

**How to build it**

- `internal/lobe/graph/extract/notes.go`: `extractNotes(comments, src, symbols) ([]Node, []Edge)`, fed by a
  `comment` capture added to each `queries/<lang>.tq.scm` (G-02), so notes sit in the parse cache (G-08). Add `note`
  and `explains` to the shared table (G-12), and to its check the rule that a note has exactly one `explains` edge.
  G-09 never walks `explains` edges.
- The tag pattern is compiled on first use, not in a package-level variable: a lobe package has no package-level
  call ([core page](vertical-lobe.md) 5.1).
- Where the source is weak. Its scan is a text test (L1984): a line inside a multi-line string that starts with
  `# NOTE:` becomes a note, and a comment after code is missed. Every comment note points at the file (L1985,
  L2603); only docstrings reach a class or function (L1961, L1973). Its id holds the line number (L1922), so one
  inserted line renames every note below. Its model prompt forbids separate rationale nodes (`graphify/llm.py`
  L532) while this code creates them. We copy none of these.
- Not ported: docstrings over 20 characters as notes (L1911), since a docstring says what code does, not why it is
  odd; and the `ADR-12` and `RFC 793` reference nodes (L2562).
- Edge cases: a multi-line `/* WHY: */` block is one note; ` * NOTE:` in a doc block counts; a tag with no text
  is no note; tags are upper case only.
- A note is repository text, so it is data (principle 5). No code path turns it into a lesson or a ladder entry.
  Notes are built with the graph whenever the `graph` setting is `on`; R-02 and G-09 show one only under `recall`.

**How we know it works**

- `TestLobeNoteTags`: 5 tags in 5 comment styles give 25 notes; `# TODO:` and `// FIXME:` give 0.
- `TestLobeNoteTarget`: notes after code, above a method in a class body, loose in a body and at the top of a file
  get the 4 expected targets.
- `TestLobeNoteIgnoresStrings`: a tag inside a string literal gives 0 notes.
- `TestLobeNoteIDStable`: 10 lines inserted above leave every note id unchanged. Yes or no.
- `TestLobeNoteScrubbed`: a note holding a secret that S-05 masks is stored without it. Yes or no.
- `TestLobeNoteNeverALesson`: after a build, `lessons/` holds 0 files and `ladder.json` gains 0 entries.
- `TestLobeNoteOneExplains`: a graph where a note has 0 or 2 `explains` edges gives 1 problem each in G-12's check.

**Tester brief**

Start from the standard tester setup in F-07. In `$W/clients/acme` run `git init shop`. Create `shop/pay.py` with
exactly these 6 lines and run `tentaqles lobe graph build` in `shop`. `L` is `$TQ_HOME/identities/acme/lobe`, and
`J` is the file `graph.jsonl` in the single folder under `L/graph/`.
```python
class Ledger:
    # WHY: the bank API rejects batches over 50 rows
    def post(self, rows):
        text = "# NOTE: not a comment"
        return rows[:50]  # HACK: slice instead of paging
# TODO: remove after migration
```
1. `J` has exactly 2 lines with `"kind":"note"`: one with `"span":[2,2]` and `"tag":"WHY"`, one with `"span":[5,5]`
   and `"tag":"HACK"`. Exactly 2 lines contain `"relation":"explains","target":"pay.py#Ledger.post"`.
2. No line of `J` contains `not a comment` or `remove after`.
3. Note the 2 note ids. Insert two empty lines at the top of `pay.py` and build again. `J` holds the same 2 ids,
   now with `"span":[4,4]` and `"span":[7,7]`.
4. `L/lessons` does not exist or is empty. `$TQ_HOME/identities/globex/lobe/graph` does not exist or is empty.

**Risks and open questions**

- Anyone who can commit can plant a note to steer the agent. It is shown quoted and capped (R-02), never a rule.
- The G-01 spike should confirm that each grammar exposes comment nodes. A language without them gets no notes.
- Open: should `ADR-n` and `RFC n` mentions (Graphify L2562) become links to documents in G-06?
- A trap that its author marked only with `FIXME` or `TODO` is not a note and does not travel with the pointer.

## G-08 Keeping the graph current

**Phase** 4 · **Origin** port: Trail-code · **Depends on** F-01, F-02, F-03, F-06, G-02, G-03, G-12

**What it is.** The graph is a cache of the code, so it is stale as soon as a file changes. This feature notices a
change with one cheap file-system check per file, records each edit the agent makes in a tiny marker file, and
rebuilds the graph after the turn in the detached pass: a process the `stop` hook starts and does not wait for.

**Why it matters.** A stale graph sends the agent to lines that moved and callers that are gone, which costs more
tokens than no graph. Rebuilding inside a hook would break the anti-goal "a hook stalls or slows a session".

**Mechanism**

1. Beside `graph.jsonl` and `parse/` (G-02), each `graph/<repo-key>/` gains `files.json`, `dirty/` and `build.lock`.
   `files.json` maps each path to `[size, mtime_ns, sha256, read_at_ns]` and names the extractor version; another
   version voids every record. Worktrees (decided; this entry owns the layout). A linked git worktree takes the
   repository key of its main repository; F-01 owns that formula and finds the main repository without running
   git (when `.git` is a file, its `gitdir:` line and that folder's `commondir`). Its files differ from the main
   working tree's, so its graph is kept apart in `graph/<repo-key>/wt-<8 hex>/`, where the 8 hex characters are
   the first of the SHA-256 of the worktree's path relative to the client folder, written with `/` (proposed).
   That subfolder holds everything this page places in `graph/<repo-key>/` for the main working tree
   (`graph.jsonl`, `files.json`, `dirty/`, `build.lock`, R-01's index, `summary.json`, G-12's `check.json`),
   with one exception: there is no `parse/` in it. The parse cache is the main repository's
   `graph/<repo-key>/parse/`, shared by every worktree, because it is keyed by content hash. Every step below
   that names `graph/<repo-key>/` means the `wt-` subfolder when the session or the command is in a linked
   worktree: its marker, its lock, its probe and its build are its own.
2. Probe: `stat` every file the build would read. A file is clean, unread, when size and mtime equal the record
   (Trail `src/graph/fingerprint.ts` L135) and the record was read at least 2 seconds after that mtime (Graphify
   `graphify/cache.py` L270, L315); `read_at_ns` is the clock taken just before the file was read. Any other file
   is hashed and the hash decides (`fingerprint.ts` L185), so a `touch` is not drift. The probe writes nothing and
   returns sorted `changed`, `added` and `removed` lists.
3. After an edit, `post-tool-use` for `Write`, `Edit`, `MultiEdit` and Codex `apply_patch` creates one file
   `dirty/<unix-nanos>-<pid>` holding the edited paths, relative to the repository. This is G-08's place in the
   hook table of core page 5.2: it comes after the event line is appended (S-03) and the open fires are judged
   (E-02), and it is written even when the duplicate filter (S-06) writes no new line for the event. The step
   does no probe and no parse. The one-file parse G-09 adds runs after it and can be cut off without losing the
   marker.
4. The `stop` hook starts one detached pass, which runs in this order: S-07's sweep, S-08's fold, consolidation,
   E-07's ladder moves, the graph build, then G-11's document run from phase 5 (core page 5.2). A pass started by
   `tentaqles lobe consolidate --only` or `--session` still runs E-07's moves and leaves out the graph build. The
   build step takes `build.lock`, lists the markers and runs the probe. It probes even with no marker, because a
   shell command, a formatter or `git checkout` changes files without an edit tool.
5. On drift or a marker it rebuilds: hash every file, reuse a cached parse when the hash matches (Trail
   `src/graph/build.ts` L316) and the cache file names this build's extractor version (G-02 step 5), parse the
   rest, resolve edges over all nodes (L360), run G-12, replace `graph.jsonl`, write `files.json`, R-01's index
   and `summary.json` (counts for R-05), and delete the markers listed in step 4. The index sits under
   `graph/<repo-key>/` and carries G-03's three lookups (by name; by file and name; by owner and name) and the
   adjacency G-09 walks, because nothing inside a hook loads `graph.jsonl`.
6. A rebuild is cut off at 120 seconds (`src/claude/sync-run.ts` L15), a first build at 10 minutes
   (`src/graph/refresh.ts` L69). After a cutoff the markers stay, and the next `stop` tries again.
7. Readers (hooks, R-03's verbs, G-09's `impact`) never rebuild and never wait. When a marker names a file they
   cite, they add "(may be stale)". The only build a person or agent waits for is `tentaqles lobe graph build`.
8. The switch. Steps 3 to 6 run when the client's `graph` setting is `on`, whatever `recall`, `lessons` and
   `enforce` say (core page 5.6): with `recall: off` the graph is still kept current, for the person's own
   queries and for the day recall comes back. With `graph: off` the hook writes no marker, the pass skips its
   build step, readers act as if there were no graph, and what is already on disk stays, unread.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port | [`src/graph/fingerprint.ts` L135](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/fingerprint.ts#L135): the stat fast path is exact equality of size and mtime; [L185](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/fingerprint.ts#L185) hashes a suspect file. Its blind spot is a same-length edit inside one mtime tick, with an environment variable as the way out (L111) |
| Graphify (Apache-2.0) | port | [`graphify/cache.py` L315](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/cache.py#L315): a stat record is trusted only when it was read after the mtime tick closed, which narrows that blind spot |
| Trail-code (MIT) | port | [`src/graph/build.ts` L316](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/build.ts#L316): a cached parse is replayed only on an equal content hash; [L360](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/build.ts#L360) resolves edges over everything |
| Trail-code (MIT) | port, changed | [`src/claude/hooks.ts` L99](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hooks.ts#L99): the post-edit hook sets `dirty` but first runs `check . --json` (L69) under an 8-second cap (`src/claude/hook-io.ts` L15) and records 0 on a timeout; ours writes only a marker. L90 reads only the first file of a Codex patch; we read every header |
| Trail-code (MIT) | port, changed | [`src/claude/sync-run.ts` L15](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/sync-run.ts#L15): the 120000 ms cap; L53 clears `dirty` without looking for edits made during the build. [`src/graph/refresh.ts` L214](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/refresh.ts#L214): a query rebuilds while its caller waits, in a child with no timeout; we do not take that |
| tentaqles (new) | new | Marker files in place of a flag, the probe at every `stop`, readers that never rebuild |

**How to build it**

- Package `internal/lobe/graph`: `probe.go` (`Probe(repo) (Drift, error)`), `dirty.go`, `buildlock.go`, `build.go`.
  The detached entry point is the one F-02 defines. The build follows E-07's moves in its pass; from phase 5 only
  G-11's document run comes after it. It carries its own deadline (step 6), so a slow or failed earlier step
  neither shortens nor skips it (proposed). Hashes are SHA-256 of raw bytes, as in G-02's `parse/` keys.
- `build.lock` is created exclusively and holds `{pid, at}` (Trail `src/util/state.ts` L208). Its age is the
  file's modification time (L256). It can be taken over at 15 minutes, or at 5 minutes when its process is dead
  (L24, L29, L257). It only decides who builds. Each file is still written by F-01's atomic write under the store
  lock, held for that write alone, so capture never waits.
- `files.json`, the index and `summary.json` carry the build id, the first 16 hex characters (proposed) of the
  SHA-256 of `graph.jsonl`. A reader ignores a sidecar whose build id differs from the graph's.
- Commands: `tentaqles lobe graph build [--all]` builds in the foreground under the same lock (`--all` is G-10's);
  there is no separate `refresh` verb. `tentaqles lobe graph status [--json]` runs the probe and prints `enabled`
  (the `graph` setting), `repo` (the repository's path relative to the client folder), `files` (records in
  `files.json`), `scopes` (G-10; `[]` for one scope), `changed`, `added`, `removed`, `dirty` (marker files) and
  `building` (`build.lock` exists). Neither command is person-only. With `graph: off`, `build` builds nothing,
  prints one line that names the setting and the fix `tentaqles lobe set graph on`, and exits 1.
- `GraphDir(clientDir, workDir) string` in `internal/lobe/graph` is the one place that returns a working tree's
  graph folder: `graph/<repo-key>/` for a main working tree, `graph/<repo-key>/wt-<8 hex>/` for a linked one. It
  calls F-01's functions for the key and for the worktree test and derives neither. `ParseDir` always returns
  the main repository's `parse/`. Two worktrees that write the same cache file write the same bytes, through
  F-01's atomic write. `files.json` in a `wt-` folder carries `root` (G-10), the worktree's path relative to the
  client folder, so `build --all` can remove the folder of a worktree that is gone.
- Marker files replace Trail's single `dirty` flag. A builder deletes only the markers it listed before it started,
  so an edit made during a build keeps its marker. A Codex patch can name several files: take every header.
- An incremental build must give the same bytes as a build from nothing. Any error in the hook or the detached
  process leaves the old graph and the markers in place.

**How we know it works**

- `TestLobeGraphProbeTouchIsClean`: rewriting a file with the same bytes gives 0 drift and writes 0 files.
- `TestLobeGraphProbeSameSizeEdit`: a same-length edit within 2 seconds of the last build, mtime put back, is seen.
- `TestLobeGraphIncrementalEqualsCold`: after 1 edit among 50 files, same bytes as a cold build and 1 file parsed.
- `TestLobeGraphEditDuringBuildStaysDirty`: a marker created while a build runs still exists after it. Yes or no.
- `TestLobeGraphOneBuilder`: two detached processes started together run exactly 1 build.
- `TestLobeGraphPostEditOnlyMarks`: with no graph, so that G-09 has nothing to do, the hook writes 1 marker, opens
  0 source files and meets the capture budget that M-06 holds.
- `TestLobeGraphSettingGatesBuild`: with `graph: off`, an edit and a `stop` give 0 markers and 0 builds; with
  `graph: on` and `recall`, `lessons` and `enforce` all `off`, they give 1 marker and 1 build.
- `TestLobeGraphBuildSurvivesEarlierSteps`: with the consolidation step forced to fail, the pass still runs 1
  build. Yes or no.
- `TestLobeGraphWorktreeApart`: a repository and one linked worktree whose `a.py` differs give 1 folder
  `graph/<repo-key>/` holding 1 `wt-` subfolder; the two `graph.jsonl` files differ; the subfolder has no
  `parse/`; building the worktree parses only `a.py`; an edit in the worktree leaves 1 marker in the subfolder
  and 0 in the main one.

**Tester brief**

Start from the standard tester setup in F-07, which leaves `model` off, so the detached pass starts no model call.
In `$W/clients/acme` run `git init shop`; `<S>` is the absolute path of that `shop` folder. Create `shop/a.py`
containing `def one():\n    return 1\n` (`\n` is a line break) and `shop/b.py` containing `x = 2\n`. In `<S>` run
`tentaqles lobe graph build`. `G` is the single folder in `$TQ_HOME/identities/acme/lobe/graph/`. "Status" is
`tentaqles lobe graph status --json` run in `<S>`. "The edit JSON" is this object, with the session id a check
names: `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"<S>","tool_name":"Edit","tool_input":
{"file_path":"<S>/a.py","old_string":"return 1","new_string":"return 2"}}`. It is piped to `tentaqles lobe-hook
claude post-tool-use`. "The stop JSON" is `{"session_id":"s1","hook_event_name":"Stop","cwd":"<S>"}`, piped to
`tentaqles lobe-hook claude stop`.

Then run the line below: a lobe hook acts only when the session's login folder is the client's (core page 5.1).
The checks change settings with `lobeset`, the helper of F-07's standard setup, one key per call: a tester does
not run `tentaqles lobe set`, which answers only to a person.
```bash
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
```
1. Status exits 0 and prints one JSON object holding `"enabled":true`, `"repo":"shop"`, `"files":2`,
   `"changed":[]`, `"added":[]`, `"removed":[]`, `"dirty":0` and `"building":false`. Rewrite `a.py` with the same
   content: the same output.
2. Change `return 1` to `return 2` in `a.py` and pipe the edit JSON. Exit 0. `G/dirty` holds 1 file containing
   `a.py`. Status shows `"changed":["a.py"]` and `"dirty":1`.
3. Pipe the stop JSON. The hook exits 0 in under 2 seconds, without waiting for the build. Within 30 seconds
   status shows `"changed":[]`, `"dirty":0`.
4. Create `G/build.lock` containing `{"pid":1,"at":"<now>"}`, where `<now>` is the current UTC time written like
   `2026-11-09T14:03:11Z`, and `shop/c.py` containing `y = 3\n`. Pipe the stop JSON again. After 5 seconds
   status shows `"added":["c.py"]` and `"building":true`.
5. Delete `G/build.lock` and pipe the stop JSON. Within 30 seconds status shows `"added":[]` and `"files":3`.
6. Delete the folder `G` and run `tentaqles lobe graph build`: exit 0, and status shows `"files":3`.
7. Run `lobeset acme recall off; lobeset acme lessons off; lobeset acme enforce off`. Change `return 2` to
   `return 3` in `a.py` and pipe the edit JSON with `"session_id":"s7"`: `G/dirty` holds 1 file. Pipe the stop
   JSON: within 30 seconds status shows `"changed":[]` and `"dirty":0`. Those three settings do not stop the
   build.
8. Run `lobeset acme graph off`. Change `return 3` to `return 4`, pipe the edit JSON with
   `"session_id":"s8"`, then the stop JSON, and wait 10 seconds. `G/dirty` holds 0 files and status shows
   `"enabled":false`. `tentaqles lobe graph build; echo "exit=$?"` prints a line that names the `graph` setting,
   then `exit=1`.
9. Run `lobeset acme graph on`, then `unset CLAUDE_CONFIG_DIR`. Pipe the edit JSON with `"session_id":"s9"`: exit
   0, empty stdout, and `G/dirty` still holds 0 files, because no login folder says whose session this is.
10. A linked worktree. In `<S>` run `git add -A`, then
    `git -c user.name=t -c user.email=t@example.test commit -q -m one`, then `git worktree add -q ../shop-wt`.
    In `$W/clients/acme/shop-wt` run `tentaqles lobe graph build`: exit 0.
    `$TQ_HOME/identities/acme/lobe/graph/` still holds exactly 1 folder, `G`. `G` now holds exactly 1 folder
    whose name is `wt-` plus 8 hex characters; it holds `graph.jsonl` and `files.json` and no `parse` folder.

Person checks (the owner runs these by hand in `<S>`, in the shell prepared above, after the checks):

1. `tentaqles lobe set graph off --client acme`, answered with yes; `tentaqles lobe graph build` then prints the
   line that names the `graph` setting and exits 1. `tentaqles lobe set graph default --client acme`, answered
   with yes; `tentaqles lobe graph build` then exits 0.

**Risks and open questions**

- A same-length edit that also restores an old mtime escapes the probe; `tentaqles lobe graph build` repairs it.
- The 120-second cap is Trail's, for its parser; M-06 measures ours. Trail's timing comments are unrepeated claims.
- Only the session's own repository is rebuilt at `stop`. The others wait for a session there or `--all` (G-10).
- The build comes after consolidation and E-07's moves (core page 5.2), so a slow consolidation delays it: with
  `model: on`,
  C-03's limits (proposed there) allow 5 drafting calls of 60 seconds each before the build starts. Readers mark
  what a marker names as possibly stale meanwhile (step 7); a change that no marker names is not marked. M-06
  should report the time from `stop` to a current graph.

## G-09 Blast radius

**Phase** 4 · **Origin** port: Trail-code, Graphify · **Depends on** G-02, G-03, G-07, G-08, F-02, F-03, R-01, R-04

**What it is.** The blast radius of a change is the set of symbols that depend on what changed and may break. From
a diff, or from a file the agent just edited, the lobe finds the changed symbols, walks the graph backwards two
steps to their dependents, sets test code apart, and shows a short capped list.

**Why it matters.** It serves "fewer errors" and "fewer tokens": the agent learns who calls the function it just
changed without searching for call sites. Without it the goal "what breaks if I change this" has no answer.

**Mechanism**

1. Ranges. A diff gives the new-side line range of each hunk from `git diff --unified=0` (Trail
   `src/blast/diff.ts` L132). For an edit seen by `post-tool-use`, find each inserted text in the file as it is
   now; a text that is missing or occurs twice makes the whole file the range.
2. Seeds. The hook parses the edited file once (G-02) and takes the symbols whose span overlaps a range, so spans
   are current even after earlier edits in the turn (G-08). A diff uses the stored graph's spans.
3. Innermost wins: a symbol that strictly contains another matched symbol is dropped, so a changed method seeds
   itself and not its class (`src/blast/blast.ts` L169). A range touching no symbol seeds the file node (L214).
4. Walk backwards, breadth first, once per changed file (L237), to depth 2, the default of both sources
   (`src/blast/blast-cli.ts` L43, Graphify `graphify/affected.py` L307), over every relation except `contains` and
   `explains`. From a method, also walk the callers of the same-named method on each supertype, at the same depth,
   without reporting that method (`affected.py` L297, L355). Inside a hook the walk reads one repository's index
   and stops at its edge (decided): it never opens `graph/links.json` or another repository's index. Crossing
   to the client's other repositories is the command's work, `tentaqles lobe graph impact --all`, which follows
   `graph/links.json` (G-10) and has no hook budget.
5. A dependent reached twice keeps its shallowest depth (`blast.ts` L247). Dependents inside a changed file are
   dropped (L464). Dependents on a test path go to a second list.
6. Render after an edit: a header; at most 8 lines of the form `<relation> <- <name> (<path>:<site line>)`, such
   as `calls <- total (total.py:4)`; then `+N more` (`src/claude/format.ts` L83, L98); a line `tests: N files
   (...)` naming up to 4 as Trail's short form does (L90), `1 file` for one; and up to 2 notes from G-07 on the
   seeds, each as its quoted 80-character name (proposed). Depth 1 comes first, `inferred` last. A list equal to
   one already shown in this session is not printed again (`sessions/<session>.json` keeps a hash of each).
7. Where the edit path runs. `post-tool-use` is capture, then inject (core page 5.2). The event line is appended
   first (S-03) and held to the capture budget; the open fires are judged (E-02); the marker is written (G-08).
   A duplicate does not end the hook early: the judging and the marker still run when S-06 writes no new line.
   Then the hook parses the edited file once, builds the list and runs E-05's argument check on the same parse
   (step 8). Both are inject work, which aims at 300 ms at the 95th percentile (proposed), and they answer in
   one `additionalContext`: E-05's notes first, then the list. The whole process is cut off at 1.5 seconds; a
   cutoff prints nothing and loses neither the event line nor the marker.
8. What E-05 receives. E-05's argument check runs in the same `post-tool-use` process, after this list is built,
   and shares the one parse. G-09 hands it a single value: the file's path and language, its bytes as they are
   now, the
   parse tree, the changed line ranges of step 1, and the file's symbols with their current spans. E-05 reads its
   call expressions from that tree and never parses the file again. Nothing else passes between the two: E-05
   does not see the walk or the list, and it stays a note, never a deny.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port, changed | [`src/blast/blast.ts` L169](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/blast/blast.ts#L169): the innermost-symbol rule; L170 lets two symbols with one span drop each other, so we require strict containment. [L237](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/blast/blast.ts#L237) is the walk per changed file. L198 skips deleted files; we seed them from the stored graph and mark hits "target deleted" |
| Trail-code (MIT) | port, changed | [`src/claude/format.ts` L72](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L72): the hook's list matches every node in the edited file, because only the path reaches it (`src/claude/hooks.ts` L102), so a one-line edit lists the callers of every function there; ours is symbol-level. [L83](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L83) is the cap of 8. L91 counts edges and calls them callers; we count dependents |
| Trail-code (MIT) | idea, not its pattern | [`src/blast/blast.ts` L536](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/blast/blast.ts#L536): the test path pattern. It misses `test_*.py`, `*Test.java`, `*Tests.cs` and `*_spec.rb` outside a `tests` folder. We set tests apart as Trail does, but with G-03's `testpath.go` (Graphify's wider list), so the lobe has one definition of a test path |
| Graphify (Apache-2.0) | port | [`graphify/affected.py` L297](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/affected.py#L297): an override match needs the same label on the supertype method; [L355](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/affected.py#L355) queues it at the same depth; L379 carries the call site of each hit |
| Graphify (Apache-2.0) | evidence | [`graphify/extract.py` L2476](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L2476): its own measurement, recall 0.80 at depth 3 on about 700 files, and deeper walks did not help |
| tentaqles (new) | new | Seeds from a fresh parse of the edited file, seeds for deleted files, once per session |

**How to build it**

- Package `internal/lobe/graph`: `blast.go` (ranges, seeds, walk; pure functions over a loaded graph),
  `blastfmt.go` (the renderer) and `editparse.go`. The `post-tool-use` handler calls them after it writes the
  dirty marker (G-08).
- `ParseEdit(payload) (EditParse, bool)` is the one parse of step 7, and `EditParse{Path, Lang string; Src []byte;
  Tree parse.Tree; Ranges [][2]int; Symbols []Node}` is the value of step 8 (`parse.Tree` is G-01's adapter type).
  The handler passes it to `Blast` and then to E-05's check. It is skipped when neither would print: `recall` or
  `graph` is `off`, there is no graph, or the file's language has no grammar.
- `Blast(g, changed, depth) Report`; `Report{Seeds, Impacted, Tests, Deleted, Unindexed, Stale}`; `Hit{ID, Name,
  Path, Depth, Relation, Confidence, Score, Site}`; the JSON keys are these names in lower case. `Site` is the
  first of the edge's `lines` (G-02). Edges of every score are walked; a hit reached through an `inferred` edge
  is marked and sorted last (proposed). A test path is what G-03's `testpath.go` says it is.
- In the hook the walk reads in-edges through the adjacency in R-01's index, which G-08 writes. A hook never
  loads the whole `graph.jsonl`, whose size has no bound.
- The walker returns everything and only the renderer cuts. The header reads "from the code graph", never "all
  callers": Graphify's 0.80 says missing edges limit recall, not depth, so depth stays 2.
- Order in `post-tool-use`: the event line (S-03), the hidden-failure check (S-04), judging open fires (E-02), the
  marker (G-08), `ParseEdit`, this list, then E-05's notes, so a cutoff loses neither the line nor the marker.
  The list prints nothing when it cannot finish, when there is no graph, or when `recall` or `graph` is `off`.
  The list and E-05's notes leave in one `additionalContext`: the notes come first, and the list is cut first
  when both do not fit R-04's budget of 700 characters (proposed). That number is the budget inside F-05's
  frame; the frame adds its own fixed text. A note from G-07 is quoted as a comment from the code, never worded
  as an instruction (principle 5).
- Deadline: F-02 cuts both post-tool events off at 1.5 seconds (core page 5.2). The append is measured against
  the capture budget, 150 ms at the 95th percentile, and this step against the inject aim. M-06 measures the
  post-tool events both ways, so an edit with a graph behind it must be one of its cases.
- `tentaqles lobe graph impact [--all] [--base <ref>] [--file <path> --lines <a>-<b>] [--depth <n>] [--json]
  [--full]`
  prints the report, capped at 8,000 characters like the other agent tools (R-03), with a last line that counts
  what was cut. Without `--all` it walks the current repository's graph, as the hook does. With `--all` a walk
  that reaches a node named as `to` in `graph/links.json` goes on to the link's `from` node in the other
  repository's graph, one step deeper, under the same depth limit; such a hit carries its repository's `root`
  (G-10) and the link's confidence (proposed). `--all` is G-10's flag and is built with it, last in phase 4.
  `--full` lifts the cap for a person and is refused inside an agent session. It is one of the
  five agent commands under `tentaqles lobe graph` (R-03; core page decision A-19), so without `--full` it is
  not person-only. It probes first and never builds (G-08 step 7, R-03): drifted paths are listed under `stale`
  with the fix line `tentaqles lobe graph build`. With `graph: off` it prints G-08's line that names the setting
  and exits 1. R-03's `callers` uses this walk.

**How we know it works**

- `TestLobeBlastSymbolLevel`: with `add` and `area` in one file, editing `add` lists 1 caller of `add`, 0 of `area`.
- `TestLobeBlastSeeds`: a change in method `get` of class `Cache` gives 1 seed; two symbols of one span give 2.
- `TestLobeBlastOverride`: `main` calls `Animal.speak`; `Dog.speak` lists `main`, and 0 hits without the inheritance.
- `TestLobeBlastCap`: 20 dependents render as 8 lines and `+12 more`.
- `TestLobeBlastTestsApart`: 3 dependents in test files give 0 lines in the main list and `tests: 3 files`.
- `TestLobeBlastOneParse`: an edit with a graph runs exactly 1 parse, and E-05's check receives that same tree.
- `TestLobeBlastCutoffKeepsCapture`: with the parser stalled 2 seconds, the process ends by the 1.5-second cutoff
  with empty stdout, 1 event line and 1 marker.
- `TestLobeBlastSilentWhenOff`: `recall: off` or `graph: off` gives empty stdout and 0 parses. 2 of 2.
- `TestLobeBlastNotesBeforeList`: with E-05 notes and a list that together pass the budget, the notes are whole,
  they come first, and only the list is cut. Yes or no.
- `TestLobeBlastHookStaysInRepo`: with a `links.json` that links a caller in a second repository to the edited
  symbol, the hook's list holds 0 paths of the second repository and the process opens 0 files outside the
  edited repository's graph folder; `impact --all` on the same change lists that caller.
- `TestLobeBlastImpactCap`: a report of 20,000 characters prints at most 8,000 from `impact`; with `--full` it
  prints whole, and under `CLAUDECODE=1` `--full` exits non-zero.

**Tester brief**

Start from the standard tester setup in F-07. In `$W/clients/acme` run `git init calc`; `<S>` is the absolute path
of that `calc` folder. Create these files in it (`\n` is a line break) and run `tentaqles lobe graph build` in
`<S>`. Then run the line below the list: a lobe hook acts only when the session's login folder is the client's
(core page 5.1). The checks change settings with `lobeset`, the helper of F-07's standard setup, one key per
call: a tester does not run `tentaqles lobe set`, which answers only to a person.
- `calc.py`: `def add(a, b):\n    return a + b\n\ndef area(w, h):\n    return w * h\n`
- `total.py`: `from calc import add\n\ndef total(xs):\n    return add(xs[0], xs[1])\n`
- `report.py`: `from calc import area\n\ndef report(w, h):\n    return area(w, h)\n`
- `grand.py`: `from total import total\n\ndef grand(xs):\n    return total(xs)\n`
- `test_calc.py`: `from calc import add\n\ndef test_add():\n    assert add(1, 2) == 3\n`
```bash
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
```
1. `tentaqles lobe graph impact --file calc.py --lines 2-2 --json` exits 0. `seeds` has 1 entry, `"name":"add"`.
   `impacted` holds `total` with `"depth":1` and `grand` with `"depth":2`, and nothing with path `report.py` or
   `test_calc.py`. `tests` has an entry with `"path":"test_calc.py"`. With `--depth 1`, `grand` is absent.
2. Change `return a + b` to `return b + a` in `calc.py` and pipe this to `tentaqles lobe-hook claude post-tool-use`:
   `{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"<S>","tool_name":"Edit","tool_input":{"file_path":
   "<S>/calc.py","old_string":"return a + b","new_string":"return b + a"}}`. Exit 0. Stdout is one JSON object
   whose `hookSpecificOutput.additionalContext` contains `calls <- total (total.py:4)`, `<- grand (grand.py:4)`
   and `tests: 1 file (test_calc.py)`, and does not contain `report`.
3. Pipe the same JSON again: exit 0 and empty stdout, because session `s1` has seen this list.
4. Run `lobeset acme recall off`; pipe the JSON with `"session_id":"s2"`: exit 0 and empty stdout. Run
   `lobeset acme recall on; lobeset acme graph off`; pipe it with `"session_id":"s3"`: exit 0 and empty stdout.
5. Run `lobeset acme graph on`; pipe the JSON with `"session_id":"s4"`: stdout contains
   `calls <- total (total.py:4)` again, so it was the two settings that kept check 4 silent.
6. Count the lines that contain `"session":"s2"` in the `events/events-*.jsonl` files under
   `$TQ_HOME/identities/acme/lobe/`: at least 1. The event was recorded although nothing was printed.

Person checks (the owner runs these by hand in `<S>`, in the shell prepared above, after the checks):

1. `tentaqles lobe set recall off --client acme`, answered with yes. Piping the JSON of check 2 with
   `"session_id":"p1"` to `tentaqles lobe-hook claude post-tool-use` gives exit 0 and empty stdout.
   `tentaqles lobe set recall default --client acme`, answered with yes, and `"session_id":"p2"`: the list is back.

**Risks and open questions**

- If the G-01 runtime starts too slowly to parse one file in the hook, fall back to a file-wide list, labeled so.
  E-05 then gets no parse and prints nothing for that edit.
- A short list can read as "safe to change" when edges are missing. M-07 should report recall for this walk.
- A Codex `apply_patch` needs the payload S-02 captures. The CLI report layout is ours (Trail's was not read).
- A parse of one file plus an index read may not fit the inject aim of 300 ms (proposed) on Windows, where
  starting the process is already a large share (M-06). The 1.5-second cutoff then decides: nothing is printed
  for that edit, and M-06 should report how often that happens.
- If G-01's runtime keeps a cache of compiled grammars, the hook reads it from this client's `index/grammars/`
  only; no
  such cache is shared between clients (core page 5.3, principle 1).

## G-10 One graph for a client's many repositories

**Phase** 4 · **Origin** port: Trail-code, Graphify · **Depends on** F-01, G-02, G-03, G-08, G-09, G-12, R-01, R-03

**What it is.** A client often has several repositories, and one repository often holds several sub-projects. The
lobe keeps one graph per repository, marks the sub-projects inside it (called scopes), and links repositories of
the same client where one imports or calls the other. All of it sits in that client's store and joins no other.
It is built last in phase 4 (decided), after G-09 and R-03, whose commands it gives the `--all` flag.

**Why it matters.** It serves "fewer tokens" where the caller and the code it calls are in different repositories,
and it guards the first anti-goal: a graph node of one client must never show in another client's session.

**Mechanism**

1. Scopes. A folder is a candidate when it holds one of nine marker files: `package.json`, `go.mod`,
   `pyproject.toml`, `setup.py`, `Cargo.toml`, `composer.json`, `pom.xml`, `build.gradle`, `build.gradle.kts` (Trail
   `src/graph/scopes.ts` L32).
2. Workspace globs come from `pnpm-workspace.yaml` or the `workspaces` key of `package.json`; we add the `use`
   lines of `go.work` and Cargo's workspace `members` (proposed). When a list exists, a folder whose only marker
   is `package.json` counts only if a glob matches it (L165). A candidate deeper than 2 path segments is dropped
   unless a glob matched it (L172). Of two nested candidates the shallower stays, except that a glob match
   replaces a parent that no glob matched (L237, L252).
3. When more than one scope remains, one with fewer than 5 non-file nodes is folded into the root
   (`src/graph/build.ts` L109, L122). This entry adds the key `scopes` to the meta line of `graph.jsonl` (G-02):
   the prefixes, longest first, then by byte value; a repository with one scope has `[]`.
4. Repositories. Each gets `graph/<repo-key>/`. The key is `r_` plus 8 hex characters, derived from the
   repository's root folder; F-01 owns the formula, and S-01 writes the same value as `repo` in every event (core
   page 5.3 and 5.4). `tentaqles lobe graph build --all` looks for folders holding `.git` under the client's
   folder, to depth 3 (proposed), without following links or junctions and without entering a repository already
   found, and deletes the graph folder of a repository that is gone. No hook and no `stop` runs this search: the
   detached pass builds the session's repository only (G-08).
5. Worktrees. The search of step 4 also lists linked git worktrees: a folder whose `.git` is a file that leads,
   through its `gitdir:` line and that folder's `commondir`, to a repository of this client (F-01 owns the test
   and the key; no git command runs). A linked worktree is not a repository of its own. It has its main
   repository's key and is built into `graph/<repo-key>/wt-<8 hex>/` (G-08 owns the layout), reusing that
   repository's parse cache. `build --all` builds each listed worktree after its main repository and deletes the
   `wt-` folder of a worktree that is gone. The search does not enter a worktree to look for more repositories,
   as it does not enter a repository.
6. Links. After a build, one pass reads the graph of every repository's main working tree and rewrites
   `graph/links.json` from nothing. It skips `wt-` folders (proposed): a worktree repeats its repository's
   declarations, and counting them would break the "exactly one" rules below. A session in a linked worktree
   uses its repository's links. An `ext` import (G-02) of the module or package name that exactly one other
   repository declares in
   `go.mod`, `package.json` or `pyproject.toml`, or of a path under such a Go module, becomes an `imports` link,
   `extracted` (new). An unresolved member call from the parse cache becomes a `calls` link only when the client's
   repositories hold exactly one same-language declaration of that name and it is in another repository:
   `inferred`, 0.85 (Graphify `graphify/cross_repo_calls.py` L278, L311).
7. Who follows the links (decided). Two commands, each with the flag `--all`: `tentaqles lobe graph impact --all`
   (G-09) and `tentaqles lobe graph find --all` (R-03). `impact --all` carries its backward walk across a link
   into the other repository's graph; `find --all` ranks over every repository of the client and names each
   hit's repository. They are commands and have no hook budget. No hook follows a link: inside a hook, blast
   radius (G-09) walks one repository's index and stops there.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port, widened | [`src/graph/scopes.ts` L32](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/scopes.ts#L32): the nine marker files; [L172](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/scopes.ts#L172) is the depth guard. Globs other than `dir/*`, `dir/**` and plain names match nothing (L114), and workspace lists are read only from pnpm and `package.json` (L69); we add `go.work` and Cargo workspace members (proposed) |
| Trail-code (MIT) | port | [`src/graph/build.ts` L109](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/build.ts#L109): `MIN_SCOPE_NODES = 5`, applied only when more than one scope exists (L114) |
| Trail-code (MIT) | port, changed | [`src/graph/workspace.ts` L98](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/workspace.ts#L98): a folder with no `.git` and two or more git children is split, one graph per child. It looks at immediate children only (`src/graph/scopes.ts` L339) and has no links between repositories; results are fused by rank with `RRF_K = 60` (`src/ask/fuse.ts` L79) |
| Graphify (Apache-2.0) | port | [`graphify/cross_repo_calls.py` L278](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/cross_repo_calls.py#L278): anything but exactly one candidate is skipped; [L311](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/cross_repo_calls.py#L311) sets the score 0.85. `graphify/extract.py` L5103 caps the unresolved calls kept per caller at 64, and the link pass keeps that cap |
| Graphify (Apache-2.0) | not taken | [`graphify/global_graph.py` L10](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/global_graph.py#L10): its merged store is one file in the home folder for every project on the machine, and L146 merges nodes without a source file by label across repositories, so removing one repository can cut another's edges |
| tentaqles (new) | new | Links by declared package name, the search bounded to the client folder, `links.json` as a derived file |

**How to build it**

- Package `internal/lobe/graph`: `scopes.go` (`DiscoverScopes`, `ScopeOf`), `repos.go` (`DiscoverRepos(clientDir)`),
  `links.go` (`Link(clientGraphDir)`). A scope is `{prefix, label, markers}`; a path belongs to the longest match.
- `RepoKey(clientDir, repoDir)` lives in the store package and F-01 owns its formula, because S-01 writes the key
  as `repo` in every event from phase 1, long before a graph exists. This entry and G-02 only call it.
- `files.json` (G-08) gains `root`, the repository's path relative to the client folder, so a key leads back to a
  place. The link pass skips a graph folder whose `root` no longer holds `.git`. The `files.json` of a `wt-`
  folder carries the worktree's path as `root`; `DiscoverRepos` returns each repository with its linked
  worktrees, `{root, key, worktrees}`.
- Build order (decided): this entry is the last of phase 4. It adds `--all` to `build` and `check`, and to
  G-09's `impact` and R-03's `find`, which exist by then. The hook path of G-09 is not touched: `links.json` is
  opened only by `links`, `check`, the link pass and those two `--all` commands.
- `links.json`: `{"v":1,"links":[{"from_repo","from","to_repo","to","relation","confidence","score","rule"}]}`,
  sorted; `rule` is `module` or `single-declaration`. `from_repo` and `to_repo` are `root` paths; `from` and `to`
  are node ids. For an import, `to` is the file G-03's import rule picks inside the target repository, and there
  is no link when it picks none. It is a cache: deleting it costs one pass.
- `tentaqles lobe graph links [--json]` prints the links that start or end in the current repository; `--json`
  prints them as one array, `[]` when there are none. `status` (G-08) prints `scopes` from the meta line. Neither
  `links` nor `build --all` is person-only; with `graph: off` both print G-08's line that names the setting and
  exit 1.
- `linkcheck.go` adds this entry's rule to G-12's check: `CheckLinks(links, graphs) []Problem` reports a link
  whose end is missing from the named repository's graph. The link pass runs it before it writes `links.json`,
  and `tentaqles lobe graph check` runs it whenever that file exists.
- Links never enter a repository's graph file, so each graph stays rebuildable alone and byte-stable (G-08).
  Because the pass starts from nothing, a link disappears when a second candidate or a removed repository shows.
- Isolation, each rule with a test that M-03 reruns: every path the package opens is built from one client name
  and lies under that client's `lobe/graph/`; no function takes two clients; no index exists outside a client's
  folder; a repository whose real path is outside the client's folder is ignored, so a link or junction into
  another client lends nothing; the same repository cloned under two clients gets two unrelated graphs.

**How we know it works**

- `TestLobeScopesDiscovery`: a pnpm workspace listing `packages/*` gives 2 scopes, and none for unlisted `tools/x`.
- `TestLobeScopesFloor`: of two scopes with 4 and 5 non-file nodes, 1 remains. Alone, the 4-node scope remains.
- `TestLobeReposDiscovered`: 2 repositories give 2 graph folders, each named by F-01's `RepoKey`; a third behind
  a symbolic link gives 0 more.
- `TestLobeReposListWorktrees`: a repository with 2 linked worktrees gives 1 graph folder holding 2 `wt-`
  subfolders after `build --all`; removing one worktree and building again leaves 1; a module declared in the
  repository and repeated in its worktrees still gives 1 link, because the link pass skips `wt-` folders.
- `TestLobeAllFollowsLinks`: with `app` importing `lib`, `impact --all` on a change in `lib` lists the caller in
  `app` and `impact` without the flag lists 0 paths of `app`; `find --all` run in `app` returns a hit in `lib`.
- `TestLobeLinkNeedsExactlyOne`: one declaration gives 1 link with score 0.85; a second declaration gives 0.
- `TestLobeLinkEndsExist`: a `links.json` whose `to` names a missing node gives 1 problem in G-12's check.
- `TestLobeGraphNeverCrossesClients`: one module in both clients; `acme`'s links and results hold 0 `globex` paths.

**Tester brief**

Start from the standard tester setup in F-07. `ACME` is `$W/clients/acme` and `GLOBEX` is `$W/clients/globex`; each
already holds an empty repository `app`. Run `git init lib` in both. Create these files (`\n` is a line break),
then run `tentaqles lobe graph build --all` in `ACME` and in `GLOBEX`.
- `ACME/lib/go.mod` and `GLOBEX/lib/go.mod`: `module example.test/lib\n\ngo 1.22\n`
- `ACME/lib/greet.go` and `GLOBEX/lib/greet.go`: `package lib\n\nfunc Greet() string { return "hi" }\n`
- `ACME/app/go.mod`: `module example.test/app\n\ngo 1.22\n`
- `ACME/app/main.go`: `package main\n\nimport "example.test/lib"\n\nfunc main() { println(lib.Greet()) }\n`

1. `$TQ_HOME/identities/acme/lobe/graph/` holds exactly 2 folders and `links.json`; so does that of `globex`.
2. In `ACME/app`, `tentaqles lobe graph links --json` exits 0 and prints at least 1 object with `"from":"main.go"`,
   `"relation":"imports"`, `"rule":"module"` and a `to_repo` naming `lib`. In `GLOBEX/lib` it prints `[]`.
3. No file under `$TQ_HOME/identities/acme/` contains the absolute path of `GLOBEX`, and none under
   `$TQ_HOME/identities/globex/` contains the absolute path of `ACME`. On Windows search for the path written
   with `/`, with `\` and with `\\`.
4. In `ACME/lib`, `tentaqles lobe graph impact --all --file greet.go --lines 3-3 --json` exits 0 and its
   `impacted` list has an entry with `"name":"main"`; the same command without `--all` has none. In `ACME/app`,
   `tentaqles lobe graph find Greet --all --json` exits 0 and prints a hit whose path is `greet.go`.
5. Copy `ACME/lib` to `ACME/lib2` and build with `--all`. In `ACME/app` the command of check 2 now prints `[]`.
6. A linked worktree. In `ACME/lib` run `git add -A`, then
   `git -c user.name=t -c user.email=t@example.test commit -q -m one`, then `git worktree add -q ../lib-wt`.
   Build with `--all` in `ACME`. `$TQ_HOME/identities/acme/lobe/graph/` holds exactly 3 folders (`app`, `lib`,
   `lib2`), no fourth for `lib-wt`, and exactly one of them holds a folder named `wt-` plus 8 hex characters.
7. In `ACME` run `git init mono`. Add `mono/pnpm-workspace.yaml` with the lines `packages:` and `  - 'packages/*'`.
   In each of `mono/packages/a`, `mono/packages/b` and `mono/tools/x` add `package.json` containing `{}` and `m.ts`
   with five lines, `export function f1() {}` to `export function f5() {}`. Build in `ACME/mono`. There,
   `tentaqles lobe graph status --json` shows `"scopes":["packages/a","packages/b"]`.

**Risks and open questions**

- For R-01: Trail's `README.md` L768 says scopes rank on their own terms; `src/ask/fuse.ts` L385 uses one maximum.
- A client folder with hundreds of repositories makes `--all` slow. It needs a progress line and a skip list.
- Submodules and nested clones are not entered. Trail has a switch for each (`src/graph/scopes.ts` L136); we do not.
  S-01 walks up to the nearest `.git`, so events inside a nested clone carry a key that has no graph behind it.
- The key comes from the path (F-01), so a renamed repository folder gets a new graph and `--all` deletes the old
  one. Events and lessons that carry the old key in `repo` no longer match the folder; F-01 weighs that.
- A name match across repositories is a guess even with one candidate, so such links are `inferred`.

## G-11 Optional PDF and image processing

**Phase** 5 · **Origin** idea: Graphify · **Depends on** F-05, F-06, C-03, S-05, G-06, G-08, G-12

**What it is.** When a client's `documents` setting is `on`, the lobe asks that client's own agent CLI to read each
PDF and image in the client's repositories once and return a few named topics, which become graph nodes. A
bookkeeping file records what was read, so no content is sent twice. The setting is `off` by default.

**Why it matters.** Specifications and diagrams often hold the answer the agent is looking for, and it cannot
search them: "fewer tokens". It is optional because it is the only graph feature that spends model allowance.

**Mechanism**

1. In scope: `.pdf`, `.png`, `.jpg`, `.jpeg`, `.gif` and `.webp` files in the client's repositories, from G-02's file
   listing before its filters. Kind is decided by extension, as in Graphify (`graphify/detect.py` L50, L51).
2. Nothing runs unless `documents`, `model` and `graph` are all `on` (principle 7; core page 5.6). Otherwise such
   a file is a plain file node. This is the second of the two model steps in core page 5.7, and like the first it
   never runs in a hook: work happens only in the detached pass that `stop` starts, after the graph build, or in
   the foreground when a person runs `tentaqles lobe graph documents`.
3. Each file is hashed (SHA-256 of its bytes). A hash that `documents.json` records as `done` is skipped.
4. The owner's rule (F-06 states it in full). Switched `on` later: the next run reads every file in scope that is
   already there and not `done`. Switched `off` later: what was processed stays in `documents.json`, in
   `documents/` and in the graph, and no new or changed file is read; a changed file's old nodes are marked
   `stale`, and a deleted file's nodes are removed, as F-06 reads the rule.
5. One file is one call through C-03's launcher: headless, with `TQ_LOBE_NO_HOOKS=1` in the child's environment,
   and not started through `tentaqles run`. The child inherits the environment of the pass, which is the
   session's, or of the person's terminal. Before a file is copied or anything is sent, the launcher checks that
   the login folder in that environment (`CLAUDE_CONFIG_DIR`, links resolved) is this client's identity folder;
   if it is not, the run reads nothing and reports `identity`. C-03's launcher takes a tool list and an optional
   input file, so this entry reuses it unchanged: it passes the file, which the launcher copies into the child's
   folder under `lobe/tmp/`, and a tool list that holds `Read` only.
6. The answer must be JSON, `{"topics":[{"name","summary"}],"mentions":["<code name>"]}`. The lobe keeps at most 30
   topics and 30 mentions, names of 80 characters and summaries of 200 (all four proposed), scrubs each string
   (S-05) and writes `documents/<hash>.json`. A run that wrote a result leaves a marker in `dirty/` (G-08) and
   then builds once more; that build reuses every cached parse and merges the results without a call. A summary
   is client text: it stays in `documents/` and in the graph, both under this client's store, and no code path
   copies a topic or a summary into a lesson, private or shared.
7. Cost guard, this step's own and apart from C-03's daily count (all proposed): a run reads at most 10 files and
   ends after 10 minutes; the detached pass starts at most one run per client per local day; a call is cut off at
   120 seconds; a PDF over 20 MB or an image over 5 MiB is `skipped-large`; a PDF is read to page 20; one child
   runs per client, as in C-03; a file is tried on at most 3 runs. A person may run the command by hand at any
   time: that run keeps the 10-file limit and does not count as the day's detached run. `--all` lifts the limit
   for one run by hand and asks for a typed yes first.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Graphify (Apache-2.0) | idea | [`graphify/detect.py` L50](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/detect.py#L50): PDFs and images are told apart by extension; [L614](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/detect.py#L614) reads PDF text page by page with a Python library, with no OCR, so a scanned PDF yields nothing |
| Graphify (Apache-2.0) | idea | [`graphify/llm.py` L1950](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L1950): its Claude CLI path runs `-p --output-format json --no-session-persistence`; [L1972](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L1972) adds `--json-schema` because newer CLIs otherwise answer in prose |
| Graphify (Apache-2.0) | idea | [`graphify/llm.py` L534](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L534): file content is wrapped and declared data, never instructions; [L853](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L853) is the 5 MiB image limit |
| Graphify (Apache-2.0) | not taken | [`graphify/llm.py` L1359](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L1359): its own comment records that split-and-retry once cost up to 15 billed calls for one bad answer, and truncated answers are still split up to 3 levels (L485); [L2763](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L2763) runs 4 calls at once; `graphify/cache.py` L1066 puts a prompt fingerprint in the cache path, so a prompt change re-bills every file |
| tentaqles (new) | new | The on and off rule, `documents.json`, the cost guard, results kept outside `graph/` |

**How to build it**

- Package `internal/lobe/documents`: `scan.go`, `ledger.go`, `run.go`, `ask.go` (C-03's launcher), `merge.go`.
  The command that starts a child answers only to a person, so its entry is `internal/cmd/lobe_documents.go`,
  which calls into the package (core page 5.1). The first version starts the Claude Code CLI only, as C-03 does.
- `documents.json`: `{"v":1,"last_run":"2026-11-09","files":{"<sha256>":{"kind":"pdf","bytes":48211,
  "paths":["<repo-key>:docs/spec.pdf"],"status":"done","tries":1,"topics":7,"prompt":"p1"}}}`. Status is `done`,
  `no-text`, `failed` or `skipped-large`; a waiting file has no entry. The key is the content hash alone: a moved
  or copied file adds a path, not a call. `last_run` is the local date of the last detached run that started a
  call; a run by hand leaves it alone.
- Results live in `lobe/documents/`, not under `graph/`: core page 5.3 says that deleting `graph/` costs only a
  rebuild and puts these files outside it, and F-06 asks that `tentaqles lobe rebuild` keep them. A new prompt
  version never reprocesses by itself; `--redo` is the only way.
- Merge: a topic is a `heading` node (G-06's kind) under the file node, with G-06's id form, the file node's span
  and two extra keys after `level`: `"origin":"model"` and `summary`, the short text the core page allows a
  document node to hold. A mention is a `references` edge only when exactly one symbol has that name: `inferred`,
  score 0.75 (proposed), rule `doc-model`; the key and the rule are added to G-12's table. The prompt declares
  the file data, and nothing in an answer becomes a lesson (F-05). Recall shows a topic of model origin quoted
  and labeled as such, as it shows a note (G-07, R-02).
- No OCR and no PDF parser are built in: the lobe gets what the agent CLI can read. An answer with no topics is
  `no-text`, so a scanned PDF the CLI cannot read keeps its plain file node. Office files are out at first.
- `tentaqles lobe graph documents [--all] [--redo] [--json] [--answer file:<path>]`. Without `--answer` it starts
  children, so it is person-only and refuses inside an agent session. `--answer` takes the answer from a file in
  place of starting a child, like C-03's `--draft file:`. It sends nothing, so an agent or a tester may run it;
  the three settings are still checked, the login folder is not. `--json` prints one object with `read` (files
  read in this run), `waiting` (files still to read) and, when a check stopped the run, `reason`: the first of
  `documents`, `model`, `graph`, `identity` and `no-cli` that failed. A stopped run exits 0.

**How we know it works**

- `TestLobeDocumentsOffCallsNothing`: with any of the three settings `off`, 3 PDFs and a `stop` give 0 calls and
  no ledger. 3 of 3.
- `TestLobeDocumentsOnOffOn`: `on` with 3 files present gives 3 calls; `off` and a 4th file gives 0 calls and
  keeps the 3 results and their nodes; `on` again gives 1 call.
- `TestLobeDocumentsNeverBilledTwice`: renaming, copying and touching a `done` file gives 0 calls.
- `TestLobeDocumentsRunCap`: 30 waiting files give exactly 10 calls in one automatic run.
- `TestLobeDocumentsOneDetachedRunADay`: after that run, a second `stop` on the same local day gives 0 calls and
  the first `stop` of the next day gives 10; a run by hand in between gives 10 and leaves `last_run` unchanged.
- `TestLobeDocumentsSurviveGraphDelete`: deleting `graph/` and rebuilding gives 0 calls and the same nodes.
- `TestLobeDocumentsChild`: the child gets `TQ_LOBE_NO_HOOKS=1` and the `Read` tool only, and no `tentaqles run`
  process is started; with another client's `CLAUDE_CONFIG_DIR`, 0 children start and 0 files reach `tmp/`.
- `TestLobeDocumentsPersonOnly`: under `CLAUDECODE=1` the command without `--answer` exits non-zero and starts 0
  children; with `--answer` it runs.
- `TestLobeDocumentsNeverALesson`: after a run, 0 lesson files are new, in the store and in the repository, and
  no summary text occurs in a file outside `documents/` and `graph/`.

**Tester brief**

Start from the standard tester setup in F-07, which sets `model` to `off`. No network is used: every run below
takes its answer from a file, so no child is started. Pipe no hook event in this brief. `L` is
`$TQ_HOME/identities/acme/lobe` and `D` is `L/documents.json`; "entries" are the keys under `files` in `D`. Write
`$W/ans.json` containing `{"topics":[{"name":"Refund policy","summary":"Refunds within 30 days"}],"mentions":[]}`.
In `$W/clients/acme` run `git init shop` and create `shop/a.pdf`, `shop/b.pdf` and `shop/c.png` containing
`%PDF-1.4 a`, `%PDF-1.4 b` and `png c`. "Run" is `tentaqles lobe graph documents --json --answer
file:$W/ans.json` in `shop`; it prints one JSON object. The checks change settings with `lobeset`, the helper of
F-07's standard setup, one key per call: a tester does not run `tentaqles lobe set`, which answers only to a
person.

1. Run: exit 0, `"read":0`, and `D` does not exist. `lobeset acme documents on` and run: `"read":0`,
   `"reason":"model"`. `lobeset acme model on; lobeset acme graph off` and run: `"read":0`, `"reason":"graph"`.
   `D` still does not exist.
2. `lobeset acme graph on` and run: `"read":3`; `D` has 3 entries, each with `"status":"done"`;
   `L/documents/` has 3 files; the `graph.jsonl` under `L/graph/` has at least 3 lines containing
   `"name":"Refund policy"`.
3. Run again: `"read":0`. Copy `a.pdf` to `shop/a2.pdf` and run: `"read":0`, and one entry of `D` lists 2 paths.
4. `lobeset acme documents off`, create `shop/d.pdf` containing `%PDF-1.4 d`, run: `"read":0` and `D` still
   has 3 entries. `lobeset acme documents on` and run: `"read":1` and `D` has 4 entries.
5. Delete `L/graph`, run `tentaqles lobe graph build` in `shop`, then run: `"read":0`; the new `graph.jsonl` under
   `L/graph/` has at least 4 lines containing `"name":"Refund policy"`.
6. `lobeset acme model off`, create `shop/e.pdf` containing `%PDF-1.4 e`, run: `"read":0` and `D` still
   has 4 entries.
7. `lobeset acme model on`. Create `shop/n1.pdf` to `shop/n12.pdf` containing `%PDF-1.4 n1` to
   `%PDF-1.4 n12`. Run: `"read":10`, the limit of one run. Run again: `"read":3`, and `D` has 17 entries.
8. `CLAUDECODE=1 tentaqles lobe graph documents --json; echo "exit=$?"`, with no `--answer`: the last line is
   `exit=1`, a line says the command answers only to a person, and `D` still has 17 entries.
9. `grep -rl "Refunds within 30 days" "$TQ_HOME/identities" "$W/clients"` lists only files under
   `$TQ_HOME/identities/acme/lobe/documents/` and `$TQ_HOME/identities/acme/lobe/graph/`: no lesson, nothing in a
   repository, nothing of `globex`.

Person checks (the owner runs these by hand in `shop`, in the shell prepared above, after the checks):

1. `tentaqles lobe set documents off --client acme`, answered with yes; create `shop/p.pdf` containing
   `%PDF-1.4 p`; the run of the brief prints `"read":0`. `tentaqles lobe set documents on --client acme`, answered
   with yes; the run prints `"read":1`.
2. Create `shop/q1.pdf` to `shop/q12.pdf` as in check 7. The run with `--all` added asks for a typed yes. Answered
   with no, `D` is unchanged. Answered with yes, it prints `"read":12`.

**Risks and open questions**

- Whether the Codex CLI can read a PDF or image in headless mode is unconfirmed. If not, this is Claude Code only.
- With no parser the lobe cannot count pages or tokens before a call; the 20-page limit is only an instruction.
- `Read` is a tool C-03's own drafting never grants. Phase 0 should confirm the child cannot read outside its
  folder with it.
- A topic summary is client text held in graph nodes and in `documents/`. It is scrubbed (S-05), but a name the
  scrubber does not know stays in. It never leaves the client's store, and `tentaqles drop` removes it.
- The automatic pace is 10 files a day (proposed), so a backlog of 500 documents takes 50 days unless a person
  runs the command with `--all`. The command's `waiting` count is the only place the backlog shows.
- A run by hand has no daily cap of its own: the person-only gate and the 10-file limit are what bound it.

## G-12 Graph soundness check

**Phase** 4 · **Origin** port: Graft · **Depends on** G-02, G-03, F-07

**What it is.** A sound graph is one whose parts agree with each other: every edge starts at a real node, every
line span is well formed, every name comes from the allowed lists. The check reads a built graph, tests a fixed
set of such rules, called invariants, and reports counts. It needs no second source of truth.

**Why it matters.** Recall and blast radius trust the graph without reading the code, so one dangling edge becomes
a wrong pointer in the agent's context. The check also lets G-08 claim an incremental build equals a cold one.

**Mechanism**

1. Node rules, from Graft `scripts/graph-quality.mjs`: unique id (L43), non-empty name (L44), kind in the allowed
   list (L45), a well-formed span with start not after end (L46, L48). Our span is `[first, last]` (G-02).
2. Edge rules: relation and confidence in their allowed lists (L55, L56); source and target are both nodes (L57,
   L61). Graft lets `imports`, `extends`, `implements` and `references` keep an outside name as target (L60). In
   `graph.jsonl` an outside name is an `ext` line (G-02), so no edge is excused; an `ext` line's source is a node.
3. Counted, not failed: a `calls` edge from a symbol to itself (L63), because direct recursion is real. A built
   graph holds none (decided): G-02 drops a call on a definition's own name, so a recursive call produces no
   `calls` edge and the count the check expects from every build is 0. The count stays in the report so that a
   graph file from elsewhere is read correctly. Trail-code keeps recursion edges; M-07 gives that difference the
   verdict `deliberate`.
4. Added rules (new): the meta line's counts equal the lines present; a symbol's span lies inside its file node's
   span; each non-file node's path is the path of exactly one file node; every edge names a `rule` from the
   allowed list (G-03); no two edges share source, relation and target; lines are in G-02's order, compared byte
   by byte. Two more arrive with the features they check, each written and tested in that feature's entry: a
   note has exactly one `explains` edge (G-07), and each end of a link in `links.json` exists in the named
   repository's graph (G-10).
5. Reported: counts by kind, relation and confidence; `ext` lines; `calls` edges; orphans, meaning non-file nodes
   with no edge other than `contains` (L69 to L72). At most 15 problems are listed (L98).
6. Exit codes as in the script: 0 sound or no `--strict`; 1 `--strict` with a problem (L117); 2 no graph (L28).
7. It runs as `tentaqles lobe graph check`, inside every build before the graph is replaced, and in CI. G-08's
   builder is the caller: G-07, G-08, G-10 and G-11 depend on this entry, and it depends on none of them.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Graft 0.17.0 (MIT) | port, tightened | `scripts/graph-quality.mjs` L43 to L48: the four node rules; L55 to L61: the edge rules, with four relations whose target may stay unresolved (L60). We excuse none, because our format has `ext` lines |
| Graft 0.17.0 (MIT) | port | `scripts/graph-quality.mjs` L19 to L21: 11 kinds, 6 relations, 4 confidence values; L117 and L28: exit codes 1 and 2 |
| Graft 0.17.0 (MIT) | port, changed | `scripts/graph-quality.mjs` L11 and L13: the header promises spans "within file line count" and "no self-loop call edges", and the code checks neither. L87 names a count `unresolvedExternalImports` that covers four relations (L60) |
| Graft 0.17.0 (MIT) | port, changed | `src/graph/invariants.ts` L16 to L18: why a self-loop call is not a problem; L40 to L41: its own warning that the unresolved-target exception could hide a bug; L21 to L22: the script keeps a second copy of the rules on purpose. `test/graph-invariants.test.ts` L8 to L9 speaks of a future `graft check --invariants`; we found no such flag in `src` |
| tentaqles (new) | new | The rules in step 4, the check inside every build, one shared table of allowed names |

**How to build it**

- `schema.go` in `internal/lobe/graph` holds the allowed kinds, relations, confidence values and rule names, shared
  by the builder and `check.go`. It ships with the names of G-02 and G-03; G-05, G-06, G-07 and G-11 each add
  their own rows when they are built. The tables are plain literals: no `init` and no package-level call (core
  page 5.1).
- `Check(g *Graph) Report`: one pass over nodes and one over edges. It takes no type from a later entry: a later
  rule is a function its own entry adds to a list in `check.go`, as G-10 adds `CheckLinks`. `Report` as JSON:
  `{"graph","nodes","edges","ext","ok","violations","problems","self_loop_calls","calls","orphans","by_kind",
  "by_relation","by_confidence"}`.
- Problem lines are fixed strings: `dangling <relation> target: <source> -> <target>`, `dangling source:
  <source>`, `bad span [5]: <id>`, `inverted span [9,5]: <id>`, and one such form per rule. They are sorted, so
  `problems[0]` is stable. The check reads only the keys its rules name; a line may lack any other key.
- Inside a build, when the check fails the builder drops the nodes and edges of each file a problem names and
  checks once more. If it still fails, the previous graph stays, the problems go to
  `graph/<repo-key>/check.json`, and the dirty markers stay (G-08). Recall keeps using the previous graph. A
  build that passes deletes `check.json`.
- `tentaqles lobe graph check [--all] [--file <path>] [--json] [--strict]` is not person-only. `--all` checks
  every graph of the client and `links.json`. `--file` checks one graph file, needs no client and skips the
  `links.json` rule. `tentaqles doctor` (F-07) shows a `check.json` that exists as the warning
  `lobe-graph-unsound`, with the client's name and the fix line `tentaqles lobe graph build`.

**How we know it works**

- `TestLobeGraphCheckDanglingCall`: a `calls` edge to a missing id gives 1 problem and exit code 1 with `--strict`.
- `TestLobeGraphCheckNoExcuse`: an `imports` edge to `lodash`, which is no node, gives 1 problem; an `ext` line, 0.
- `TestLobeGraphCheckSpans`: spans `[5]`, `[10,5]` and one past the end of its file give 3 problems.
- `TestLobeGraphCheckSelfLoop`: a hand-written graph file with one `calls` edge from a symbol to itself gives
  `ok` true, `self_loop_calls` 1 and exit code 0.
- `TestLobeGraphSoundFixtures`: every fixture repository, one per language, and tentaqles itself give 0 violations
  and `self_loop_calls` 0, recursive functions in the fixtures included.
- `TestLobeGraphBuildKeepsOldGraph`: a build forced to emit a dangling edge leaves the old graph byte-identical,
  writes `check.json`, and makes `tentaqles doctor --json` show `lobe-graph-unsound` once.

**Tester brief**

Start from the standard tester setup in F-07. Create `$W/good.jsonl` with these 8 lines. "Check X" means
`tentaqles lobe graph check --file X --json --strict` run in `$W`. The edge lines leave out `score` and `lines`,
which no rule reads. The last line is a `calls` edge from `g` to itself, which no build writes (G-02 drops a
recursive call); it is in the file to show that the check counts such an edge and does not fail on it.
```json
{"t":"meta","v":1,"repo":"r_00000000","nodes":3,"edges":4}
{"t":"node","id":"a.py","name":"a.py","kind":"file","path":"a.py","span":[1,9]}
{"t":"node","id":"a.py#f","name":"f","kind":"function","path":"a.py","span":[1,3]}
{"t":"node","id":"a.py#g","name":"g","kind":"function","path":"a.py","span":[5,9]}
{"t":"edge","source":"a.py","relation":"contains","target":"a.py#f","confidence":"extracted","rule":"contains"}
{"t":"edge","source":"a.py","relation":"contains","target":"a.py#g","confidence":"extracted","rule":"contains"}
{"t":"edge","source":"a.py#f","relation":"calls","target":"a.py#g","confidence":"extracted","rule":"same-file"}
{"t":"edge","source":"a.py#g","relation":"calls","target":"a.py#g","confidence":"extracted","rule":"same-file"}
```
1. Check `good.jsonl`: exit 0, with `"ok":true`, `"violations":0`, `"nodes":3`, `"edges":4`, `"calls":2`,
   `"self_loop_calls":1`, `"orphans":0`.
2. In a copy, change the target on line 7 to `a.py#gone`. Check it: exit 1, `"violations":1`, and `problems[0]` is
   `dangling calls target: a.py#f -> a.py#gone`. Without `--strict`: the same output and exit 0.
3. In a copy, change the span of `a.py#g` to `[9,5]`. Check it: exit 1 and `problems[0]` is
   `inverted span [9,5]: a.py#g`. With `[5,12]` in its place: exit 1 and `"violations":1`.
4. In a copy, append the line `{"t":"ext","source":"a.py","relation":"imports","name":"os"}`. Check it: exit 0,
   `"ok":true`, `"ext":1`.
5. Check `missing.jsonl`, a file that does not exist: exit 2.

**Risks and open questions**

- The fixture uses the keys and names of G-02 and G-03 as drafted. If they change, the fixture changes with them.
- One extractor bug can fail every build. Dropping the named files hides them until someone reads `doctor`.
- The check proves the graph is consistent, not that it is right: a missing call edge passes. M-07 measures that.
- Open: should a rising orphan share or a rising `dropped` count fail CI at a threshold, or only be reported?
