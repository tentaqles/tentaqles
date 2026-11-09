# 060: The remaining languages

**Status** not started · **Stage** Graph · **Entries** G-02 (step 7, the phase 5 languages); G-01 (the written
result's list); M-07 (one fixture per language); F-07 (the `NOTICE` file) · **Needs** 057 · **Feeds** none

## Result

Each further language arrives alone, as one registry row and one query file with fixtures, and from that moment
its files are in the graph. This file is a recipe to repeat once per language, and the list of languages it
applies to. A person sees it for, say, Lua:

```bash
cd ~/clients/acme/game && tentaqles lobe graph build
tentaqles lobe graph outline src/player.lua      # its functions, with spans
tentaqles lobe graph find "spawn enemy"          # hits in .lua files
```

Before the language has its row, the same commands list nothing for those files: they are skipped as unclaimed
extensions.

## Scope

**In**

- The 12 languages of G-01's list that the first set leaves for phase 5: `clojure`, `dart`, `elixir`, `gleam`,
  `lua`, `nix`, `ocaml`, `r`, `scala`, `solidity`, `swift` and `zig`. A grammar that G-01's written result left
  out has no row and is not attempted.
- Per language: the registry row, the query file (Trail-code's where it has one, used as it is; otherwise
  ours), our added captures in `queries/<lang>.tq.scm`, a fixture with a golden file, a fixture repository for
  the soundness check, and a fixture for the comparison with Trail-code.
- Per language: its language family and its import resolver in resolution, its test-path patterns, and the
  entry in `NOTICE` for a copied query file or a new grammar.
- The repeatable evaluation at the end of this file, run once per language.

**Out**

- Argument counts. G-04's first signature set is Python, TypeScript with TSX, Go, Java and C#; "every other
  language gets `sig` only and no `params`". A language added here is never judged by the argument check of
  [051](051-argument-check.md). A second signature set is in no feature.
- A hand-written walker. Extraction has one path, the query file (G-02 step 7). A language without a query file
  waits.
- A second parser runtime for a grammar the chosen one cannot load (G-01 step 5).
- Languages outside G-01's 24. TOML, INI and Dockerfiles are not covered by any feature (G-06 risks).
- `sql` and `hcl`, which are 052's and 053's.

## What it builds on

- [042](042-parser-runtime.md): the written result `docs/prd/spikes/G-01-parser.md`, which names the runtime,
  the pinned grammar revisions and every grammar left out, with the measure it missed; the corpus and oracle
  under `spike/parser/testdata/` on the spike branch.
- [043](043-graph-of-one-repository.md): `internal/lobe/graph/parse` (`Load`, `Parse`, `Captures`),
  `graph/lang` (`registry.go`, `queries/`), `graph/extract`, and the tests whose counts rise here:
  `TestLobeGraphQueriesCompile`, `TestLobeGraphExtractFixtures`, `TestLobeGraphNoQueryFileNoRow`.
- [044](044-graph-soundness.md): `TestLobeGraphSoundFixtures`, one fixture repository per language.
- [045](045-resolution-and-callers.md): `graph/resolve` with `imports.go` (one import resolver per language),
  `families.go` and `testpath.go`.
- [047](047-signatures.md): `sig.go`, which gives every language its one-line header.
- [048](048-graph-stays-current.md): the extractor version in `files.json` and in each parse-cache file.
- [052](052-sql-in-the-graph.md) and [054](054-why-comments.md): the `sql.string` and `comment` captures that a
  new `.tq.scm` file should carry where the grammar has string and comment nodes.
- [057](057-graph-accuracy.md): `lobebench graphdiff`, `bench/graphdiff/theirs/`, `verdicts.yaml`, and the
  container script that builds Trail-code's graph.
- F-07 (first built in 003 or 005): `NOTICE`, `third_party/licenses/`, `TestNoticeListsEveryPort`.

## What it leaves for later features

Nothing in the index depends on this feature. It leaves a registry with up to 24 rows, the recipe below for any
grammar added later, and one line per language in the table at the end of "How it works".

## How it works

The recipe, per language. One language is one small change that passes the whole gate before the next starts.

1. **Check the grammar.** Read G-01's written result. If it lists the grammar as left out, stop: the language
   has no row, and that is recorded in the table below with the measure it missed. If the result did not
   measure the grammar with a query file, rerun the oracle comparison for it (measures 3 and 4 of G-01: trees
   and captures equal to the oracle's).
2. **Link the grammar** into the one runtime, at the revision G-01 pinned. No GPL or MPL grammar. Measure the
   growth of the stripped binary and add it to the running total; G-01's mark is at most 40 MB for all 25
   grammars (proposed). The grammar is loaded on the first `Load`, never from an `init` function or a
   package-level variable (core page 5.1).
3. **The query file.** If Trail-code has `src/graph/queries/<lang>.scm` at the commit pinned in core page
   section 7, copy it unchanged, strip the seven editor predicates Trail-code strips, and list it in `NOTICE`.
   Otherwise write ours in the capture style of Trail-code's files. Either way the added captures go in
   `queries/<lang>.tq.scm`: `import.source`, `import.name`, `receiver`, `extends`, and `comment` and
   `sql.string` where the grammar has such nodes (G-02 step 4). `params` and `args` are not added (G-04 step 8).
   A capture nothing reads fails `TestLobeGraphQueriesCompile`, except `doc`, `ignore` and `local.scope`.
   Trail-code's `solidity.scm` has one more, `reference.unknown` (G-02); it is stripped or added to the ignored
   list by a decision written in the change.
4. **The registry row** in `graph/lang/registry.go`: the language, its lower-cased extensions as in Trail-code's
   registry, and the query pair. The table is built on first use.
5. **Resolution.** Add the language to `families.go`: `scala` and `clojure` join the family of `java` and
   `kotlin`; every other language of this list stands alone (G-03 step 5). Add its import resolver to
   `imports.go`; with none, an import becomes an `ext` line and no edge. Add its test-file patterns to
   `testpath.go` only where Graphify's list has them.
6. **The fixture.** One small source file set that exercises a function, a method in a class-like owner, a call,
   an import and a base type where the language has them, with a golden file of node ids, owners and raw
   references in `TestLobeGraphExtractFixtures`. A fixture repository of the same language joins
   `TestLobeGraphSoundFixtures` and must give 0 violations and `self_loop_calls` 0, with a recursive function
   in it.
7. **Stores that already exist.** A new row alone needs no new extractor version: files of the new language were
   never in `files.json`, so the next probe reports them as added and the next build parses them (G-08 step 2).
   The version rises only when the change alters what an existing language yields, for example an edit to a
   shared `.tq.scm` rule; then every cached parse of an older build is ignored (G-02 step 5).
8. **The comparison.** Build Trail-code's graph of the fixture repository with 057's script, commit it under
   `bench/graphdiff/theirs/`, run `lobebench graphdiff`, and give every difference a verdict. Trail-code parses
   some of these languages with a hand-written walker and not a query file; expect more differences there, each
   still needing a verdict. Gates as in M-07: unexplained 0 and `ours-bug` 0.
9. **Ship it.** The counts in `TestLobeGraphQueriesCompile` and `TestLobeGraphExtractFixtures` rise by one. One
   line in `CHANGELOG.md`. The table below gets its row filled.

The list. "Family" is G-03's. The other columns are filled as each language is done.

| Language | Family | Trail-code query file | G-01 result | Binary growth | Status |
|---|---|---|---|---|---|
| `clojure` | java, kotlin, scala, clojure | check at task 1 | | | not started |
| `dart` | alone | check at task 1 | | | not started |
| `elixir` | alone | check at task 1 | | | not started |
| `gleam` | alone | check at task 1 | | | not started |
| `lua` | alone | check at task 1 | | | not started |
| `nix` | alone | check at task 1 | | | not started |
| `ocaml` | alone | check at task 1 | | | not started |
| `r` | alone | check at task 1 | | | not started |
| `scala` | java, kotlin, scala, clojure | check at task 1 | | | not started |
| `solidity` | alone | yes (G-02 cites its L43) | | | not started |
| `swift` | alone | check at task 1 | | | not started |
| `zig` | alone | check at task 1 | | | not started |

Trail-code has 17 query files and the first set used 8 of them, so 9 of these 12 languages should have one and 3
need a query file of ours. The requirements do not say which; task 1 reads the folder and fills the column.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-02](../prd/lobe-graph.md) steps 1 to 4 and 7, the "first languages" row, and "How to build it"; [G-01](../prd/lobe-graph.md) steps 1 and 5 and measures 3, 4 and 7; [G-03](../prd/lobe-graph.md) step 5 and the `imports.go` bullet; [G-04](../prd/lobe-graph.md) step 8; [G-07](../prd/lobe-graph.md) step 1 and its last risk; [G-12](../prd/lobe-graph.md), `TestLobeGraphSoundFixtures`; [M-07](../prd/lobe-benchmarks.md) steps 1 and 6; [F-07](../prd/lobe-foundation.md) step 6; [core page](../prd/vertical-lobe.md) sections 7 and 8 (phase 5) |
| Source to port | [`src/graph/generic.ts` L47](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L47): one row per language, with its extensions. [L114](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L114): the kind table. [L627](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L627): an unmapped kind becomes function. [`src/graph/queries/java.scm` L1](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/queries/java.scm#L1): the capture style the query files follow. The other query files sit in the same folder at the same commit; `generic.ts` L140 lists the seven editor predicates that are stripped |
| Not ported | The walker in `src/graph/generic.ts` at L659, which guesses definitions from node types, and the swallowed load and compile failures at L175 and L181 |
| Grammar licenses | The grammar's own repository, at the revision G-01 pinned. Web, not machine-checked |
| Existing code | `internal/lobe/graph/lang/registry.go` and `queries/`, `internal/lobe/graph/resolve/`, the fixture folders of 043 and 044, `bench/graphdiff/`, `NOTICE` |

## Build tasks

Task 1 is done once. Tasks 2 to 8 repeat per language; two languages can be in work side by side, because each
touches its own row, query pair and fixture.

1. Check this brief against the code as it stands and list what no longer fits. Read G-01's written result and
   Trail-code's `src/graph/queries/` folder at the pinned commit, and fill the two columns of the list. Put the
   order of the languages to the owner; this file proposes those with a Trail-code query file first. Files: this
   file's table only. Check: the owner approves the order.
2. The grammar: link it, measure the binary, record the license. Check: `CGO_ENABLED=0 go build` for the six
   targets, and the growth written in the table.
3. The query pair and the registry row. Test: `TestLobeGraphQueriesCompile` with the count one higher.
4. The resolution entries (family, import resolver, test paths). Test: a case for the language in
   `TestLobeResolveNoCrossFamily`, and one import case in the fixture's golden file. Can run beside task 3.
5. The fixture and golden file, and the fixture repository. Tests: `TestLobeGraphExtractFixtures`,
   `TestLobeGraphSoundFixtures`, `TestLobeGraphDeterministic` on the new fixture.
6. Stores that already exist. Test: `TestLobeGraphNewLanguageIsAdded` (new here): a store built before the row
   existed reports the language's files as added at the next probe, and the build after it equals a cold build
   byte for byte. `TestLobeGraphIncrementalEqualsCold` stays green.
7. The comparison with Trail-code and its verdicts. Test: `TestLobeGraphDiff_FixturesFullyExplained`.
8. Docs: the language list in `docs/` wherever 043 put it, `NOTICE` and `third_party/licenses/` for a copied
   query file or a new grammar, `CHANGELOG.md` under `[Unreleased]`. Then the tester run of "Evaluation" for
   this language.

## Tests that stay in the repository

- `TestLobeGraphQueriesCompile`: N of N query pairs compile, N being 12 plus the languages added so far; 0
  capture names outside the read list and the ignored ones.
- `TestLobeGraphExtractFixtures`: per language, node ids, owners and raw references equal a golden file: N of N.
- `TestLobeGraphNoQueryFileNoRow`: every registry row names a query file that exists and compiles, and a file of
  a language with no row gives 0 nodes. This is what keeps a language out until all three parts exist.
- `TestLobeGraphSoundFixtures`: every fixture repository, the new one included, gives 0 violations and
  `self_loop_calls` 0.
- `TestLobeGraphDiff_FixturesFullyExplained`: unexplained 0 on the new fixture.
- `TestLobeGraphNewLanguageIsAdded` (new here): a store built before a row existed picks the language's files up
  as added, and the result equals a cold build.
- `TestLobeParamsAbsentWhenUnsupported`, widened by one case per language: a function of the new language has
  `sig` and no `params`.
- `TestNoticeListsEveryPort`: 0 ported files without a `NOTICE` entry.
- House rules kept green: the import and no-`init` test of F-02 (a grammar package must do no work at import
  time that the guard's process pays), `CGO_ENABLED=0` builds for the six targets,
  `TestGoreleaserBuildsOneProgramUnderTwoNames`, M-06's latency check of 012 (a larger binary must not move a
  hook past its limit), and every test under "Must not trade off".

## Evaluation

Tester brief, run once per language. The builder fills three values in before handing it over: `<ext>` (one
extension of the language), `<lang>` (its registry name), and the fixture file `F` with the two ids it must
give. Start from the standard tester setup of [002](002-sandbox.md) (F-07). No network. In `$W/clients/acme` run
`git init poly`. Copy the fixture file to `poly/sample.<ext>` and create `poly/keep.py` holding
`def keep():` and `    return 1`. Run `tentaqles lobe graph build` in `poly`; expect exit 0. `G` is `graph.jsonl`
in the one folder under `$TQ_HOME/identities/acme/lobe/graph/`.

1. Line 1 of `G` contains `"<lang>":{"files":1,` and `"python":{"files":1,`, and its `errors` key reports none.
2. `G` holds a node line for each of the two ids the builder named, each with `"path":"sample.<ext>"`,
   `"lang":"<lang>"` and a `sig` key, and neither line holds `"params"`.
3. `tentaqles lobe graph outline sample.<ext>` exits 0 and lists both definitions with their spans.
4. `tentaqles lobe graph check --json --strict` exits 0 with `"ok":true` and `"self_loop_calls":0`.
5. Record the SHA-256 of `G`, delete the graph folder, build again and compare: the same hash.
6. Rename `poly/sample.<ext>` to `poly/sample.unclaimedext` and build. No line of `G` holds `sample.`: an
   unclaimed extension is skipped.
7. `tentaqles version --notices` names the grammar, and Trail-code's query file when one was copied.

**Person checks** (the owner): open one real repository of that language in a client of their own, run
`tentaqles lobe graph build` and `tentaqles lobe graph outline` on a file they know, and read whether the list
is right.

Scoring: PASS needs 7 of 7 for the language. "Could not run" is a FAIL. A language that fails keeps no row.

## Done when

Per language:

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) with the counts one higher.
- [ ] The tester brief passes for the language, 7 of 7.
- [ ] The comparison with Trail-code shows unexplained 0 and `ours-bug` 0 for its fixture.
- [ ] The house rules hold: no cgo, no work at import time, the latency check of 012 within its limits, the
  guard's tests untouched.
- [ ] `NOTICE`, the language list in the docs and `CHANGELOG.md` are updated, and the row of the table above is
  filled.
- [ ] The owner has seen the result described under Result for that language.

The feature as a whole is done when every row of the table reads `shipped` or `left out`, each `left out` with
its reason.

## Risks and unknowns

- Which of the 12 have a Trail-code query file was not read for the requirements. Task 1 settles it.
- The `tags.scm` style and the grammars' node type names differ between grammar revisions; the query files are
  pinned to the revisions G-01 fixed.
- G-01's corpus and oracle live on a branch that is never merged. If that branch is gone, step 1's rerun needs
  the reference tree-sitter CLI again (network, once).
- Each grammar grows the binary. If the total passes G-01's 40 MB mark (proposed), the owner chooses which
  languages stay out.
- A grammar without comment nodes gives no why-comments for its language (G-07 risks).
- Trail-code's walker-only languages have no query-file output to compare with, so their differences in 057's
  comparison may be many.
- The order of the 12 is not in the requirements. It is the owner's call at task 1.
