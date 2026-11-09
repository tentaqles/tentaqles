# 046: Find code by words

**Status** not started · **Stage** Graph · **Entries** R-01 (all of it), R-03 (find and map; steps 1, 4, 5,
6), G-08 (the index and its build id, written by the build) · **Needs** 045 ·
**Feeds** 048, 049, 050, 051, 056, 058, 062

## Result

`tentaqles lobe graph find` takes a few plain words and returns a short ranked list of symbols, each with a
file and a line span, so the agent reads one function instead of several files. A question such as "who
calls X" is answered from the graph's edges. `tentaqles lobe graph map` shows the repository's folders with
their file and symbol counts and nothing else.

```bash
cd "$W/clients/acme/app"        # the Go fixture of the Evaluation
tentaqles lobe graph build
tentaqles lobe graph find "parse span"
tentaqles lobe graph find "who calls ParseSpan"
tentaqles lobe graph map
```

```
1. ParseSpan · function  lib/span.go:L3-L3
   func ParseSpan(s string) (int, int) { return 0, 0 }
2. TestParseSpan · function  lib/span_test.go:L5-L5
   func TestParseSpan(t *testing.T) { ParseSpan("L1") }
```

The second line of a hit is the node's `sig`, which is still the definition's first line here;
[047](047-signatures.md) cuts it at the body.

```
lib/  2 files, 2 symbols
app/  1 file, 1 symbol
```

## Scope

**In**

- Package `internal/lobe/search`: the tokenizer, the index, the lexical score, the graph rank, the
  structural route, and `Search(ix *Index, query string, opt Options) Result` (R-01 steps 1 to 7).
- The index file under `graph/<repo-key>/`, written by every build beside `graph.jsonl`, with the build id.
  It holds the per-node term counts, the document frequencies, the mean body length, the adjacency, and
  G-03's three lookups.
- `tentaqles lobe graph find <query> [--limit N] [--in <path>] [--source] [--json]` (R-03 step 1).
- `tentaqles lobe graph map [--max-dirs N]` (R-03 step 4).
- A rule that keeps secret-shaped words out of the index.

**Out**

- `find --all` across a client's repositories ([056](056-many-repositories.md)). Until then the flag is
  refused with one line and exit 1.
- Search inside a hook: code pointers with a prompt ([050](050-prompt-injection.md)), the lookups served to
  the argument check ([051](051-argument-check.md)), the adjacency walked by blast radius
  ([049](049-blast-radius.md)). This feature writes what they read and runs none of it in a hook.
- Rebuilding the index after the turn, and "(may be stale)" on a hit
  ([048](048-graph-stays-current.md)).
- Counting the tokens an answer saved ([058](058-token-savings.md)).
- The MCP form of the tools ([062](062-mcp-server.md)).
- Scoring of document and configuration nodes ([053](053-docs-and-config.md)); R-01 leaves it open.
- Grouping code into areas and ranking central symbols; core page section 11 leaves it for later.

## What it builds on

- **[045](045-resolution-and-callers.md)**: edges with `confidence`, `score`, `rule` and `lines`; the lookup
  interface (by name; by file and name; by owner and name); `testpath.go`; the one-step walk and the symbol
  matching used by `callers`; the registry with two tools.
- **[043](043-graph-of-one-repository.md)**: `build.go`, which already reads every file to hash it;
  `store.go`; `GraphDir`; `sig` on nodes; `internal/lobe/query` (`render.go`, the 8,000-character cap);
  `TestLobeQueryToolCount`, `TestLobeQueryNeverBuilds`, `TestLobeQueryOtherClient`.
- **[044](044-graph-soundness.md)**: the check that runs before the graph and its sidecars are replaced.
- **[003](003-shell-failure-claude-code.md)**: `internal/lobe/scrub` (`Detect`), the store's atomic write,
  and the written result of F-01's cache-format spike (plain files or an embedded database).
- **[005](005-status-and-capture-switch.md)**: `internal/lobe/cli`, F-07 step 3 and its tests.

## What it leaves for later features

- `internal/lobe/search`: `tokenize.go`, `index.go`, `score.go`, `pagerank.go`, `structural.go`.
  `Options{Limit int; In string; Deadline time.Time}`; `Result{Mode string; Hits []Hit; Coverage,
  CoverageStrong float64; Note string}`; `Hit{Name, Kind, Path string; Start, End int; Score float64}`.
  `Mode` is `structural`, `lexical` or `empty`. 050 calls `Search` inside a hook with a `Deadline`; 062
  serves it as `lobe_find`.
- The index under `graph/<repo-key>/`, with the build id: the first 16 hex characters (proposed) of the
  SHA-256 of `graph.jsonl`. 048 rewrites it after the turn; 049 walks its adjacency; 051 reads its three
  lookups; 050 scores against it.
- `coverage` and `coverage_strong`, which 050 uses to decide whether a prompt gets a pointer at all.
- The commands `tentaqles lobe graph find` and `tentaqles lobe graph map`; the registry holds four tools.

## How it works

1. **Tokenize** (R-01 step 1). Split camelCase and every non-alphanumeric character, lower-case, drop tokens
   under 2 characters and Trail-code's 32 stop words. An acronym splits from the next word: `HTTPResponse`
   gives `http` and `response`. In a query a repeated term counts once.
2. **The index.** The build of 043 counts, for each node, the terms of its name, of its path, and of its
   body. The body is the node's `sig` plus the terms of the bytes of its span in the file (a file node's
   span is the whole file). The build then writes the index: those counts, the document frequencies, the
   mean body length, the adjacency over `calls`, `references`, `implements`, `extends` and `imports`, and
   the three lookups. No source text is stored, only term counts.
3. **No secret in the index.** Before a word of a file is split into terms, a word that `scrub.Detect` flags
   is left out. The index sits on disk, and the house rule "no secret on disk" covers it.
4. **The build id.** The index carries the build id of the `graph.jsonl` it was built from. `Search` and the
   commands do not use an index whose id differs. The index is written through F-01's atomic write, after
   the graph has passed the soundness check.
5. **Structural route** (step 2). The query contains, in any letter case, `who calls`, `what calls`,
   `callers of`, `called by` or `used by` (incoming), or `callees of`, `what does <name> call`,
   `what does <name> import` or `depends on` (outgoing). The query's words are resolved longest first, by
   whole symbol name with case ignored, then by last dot-part. The answer is up to `limit` neighbors over
   `calls`, `references`, `implements` and `extends` (outgoing adds `imports`), sorted by path and line, with
   `mode: structural`.
6. **Falling back** (step 3). If no word resolves, the search goes on lexically and `note` names the longest
   word. If the symbol has no such edge, it goes on lexically and `note` names the symbol.
7. **Lexical score** (step 4): `3*S(name) + 2*S(path) + BM25(body)`. `S` sums `tf*idf` over the query terms,
   with `idf = ln(1 + N/(1+df))` over the `N` scored nodes. BM25 uses `k1 = 1.2` and `b = 0.75`.
8. **Graph rank** (step 5). Divide the scores by the top score. Run personalized PageRank over the five
   relations, undirected: the seeds are the scores as shares of their sum, teleport is 0.25, a node with no
   edge returns its mass to the seeds, there are exactly 25 iterations, and the result is divided by its
   maximum. A self-loop is dropped and repeated edges count once.
9. **Candidates and final score** (step 6): nodes with a lexical score, plus nodes whose rank is at least
   0.15. Final score: `(lexical + 0.5 * rank) * factor`. The factor is 0.35 for a test path (`testpath.go`)
   and 1 otherwise, or when the query names tests. It is applied once, last.
10. **Order and cut** (step 7). Sort by final score, then name; keep at most 2 hits per file (proposed);
    return `limit` hits and the two coverage shares of the top hit: `coverage` (name, path or body) and
    `coverage_strong` (name; 0 for a file).
11. **Deadline.** When `Options.Deadline` passes before step 8, `Search` returns the lexical order without
    the graph rank. Commands set no deadline.
12. **`find`.** Default limit 5 (proposed), highest 20; a higher value is read as 20. A hit prints
    `<n>. <name> · <kind>  <path>:L<start>-L<end>` and one signature line of at most 140 characters.
    `--source` adds up to 30 lines of code per hit (proposed), read from the file as it is now. `--in <path>`
    filters nodes by path prefix, by component, before scoring, and must stay inside the repository.
    `--json` prints the `Result` as one compact line with the keys `mode`, `hits`, `coverage`,
    `coverage_strong` and `note`; a hit has `name`, `kind`, `path`, `start`, `end` and `score`.
13. **`map`.** Folders of the repository with their file and symbol counts, most files first, 16 by default
    (`--max-dirs`). A folder that holds more than 60% of the files is split one level deeper. Files at the
    root count under `.`. A last line counts the folders not shown. Nothing else is printed: no central
    symbols.
14. **The verbs' rules** are 043's: client from the working folder, F-07 step 3, exit 1 with one line on
    stderr outside a client, without a graph or with `graph: off`. They never build: a missing graph, or an
    index whose build id does not match, gives exit 1 with the fix line `tentaqles lobe graph build`. Output
    is capped at 8,000 characters (proposed) with a last line `(N more not shown; narrow with --in)`. No
    advice to the model, no savings line.

## Where to read

| What | Where |
|---|---|
| Requirements | [R-01](../prd/lobe-recall.md), all of it; [R-03](../prd/lobe-recall.md), steps 1, 4, 5 and 6 and "How to build it"; [G-08](../prd/lobe-graph.md), step 5 and "How to build it" (the index, the build id); [G-03](../prd/lobe-graph.md), "How to build it" (the three lookups); [F-01](../prd/lobe-foundation.md), "How to build it" (the cache-format spike); [S-05](../prd/lobe-sense.md), "How to build it" (`Detect`) |
| Source to port: scores and tokenizer | Trail-code [`src/ask/ask.ts` L940](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L940), [L229](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L229), [L266](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L266); [`src/ask/index-file.ts` L38](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/index-file.ts#L38) (stop words on L28) |
| Source to port: graph rank | Trail-code [`src/ask/graphrank.ts` L142](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/graphrank.ts#L142); [`src/ask/ask.ts` L989](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L989) (weight 0.5 on L417, floor 0.15 on L423); [`src/graph/relations.ts` L14](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/relations.ts#L14); [`src/ask/ask.ts` L457](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L457) |
| Ported, changed | Trail-code [`src/ask/ask.ts` L305](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L305) (intent pattern), [L943](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L943) (the 0.35 factor), [`src/ask/graphrank.ts` L54](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/graphrank.ts#L54), [`src/ask/ask.ts` L522](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/ask.ts#L522) (words that name tests) |
| Source to port: map | Trail-code [`src/graph/map.ts` L92](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/map.ts#L92) (16 folders, the 0.6 split on L97) |
| Not copied | Trail-code [`src/mcp/tools.ts` L275](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/mcp/tools.ts#L275) against [`src/cli.ts` L835](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L835) (two defaults for one question); [`src/claude/format.ts` L364](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L364) (the model is asked for a savings tally); `src/ask/file-rank.ts` L204 (a per-file score) |
| Existing tentaqles code | `internal/lobe/graph/build.go`, `store.go`, `resolve/testpath.go`, `internal/lobe/query` (`registry.go`, `render.go`, `callers.go`), `internal/lobe/scrub`, `internal/lobe/cli`, `docs/reference/commands.md` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: what F-01's cache-format
   spike decided (it fixes the index's format); the file name of the index, which neither R-01 nor F-01
   gives (propose `search` with the format's extension and ask the owner); how `scrub.Detect` behaves on
   single words; how `callers` resolves a symbol name, so `find` shares that code. The owner approves
   changes before code.
2. **`tokenize.go`** with the stop words ported as data. Proves it: a table test with `HTTPResponse`,
   `parse_span`, a 1-character token and a stop word.
3. **`score.go`** (side by side with tasks 4 and 5): idf and BM25. Proves it: `TestLobeSearchIDF`,
   `TestLobeSearchBM25`.
4. **`pagerank.go`** (side by side). Proves it: `TestLobeSearchPageRankPair`.
5. **`structural.go`** (side by side): the intent phrases, the word resolution, the neighbors. Proves it: a
   table test with every phrase in two letter cases, and bare "uses" and "import" staying lexical.
6. **`index.go`**: the `Index` type, its build from nodes, edges and file bytes, the secret rule, its file,
   the build id, and the write from `build.go`. Proves it: `TestLobeSearchIndexBuildID`,
   `TestLobeSearchIndexHoldsNoSecret`, `TestLobeGraphDeterministic` (the index bytes too).
7. **`Search`**: candidates, final score, the test factor, 2 hits per file, coverage, the deadline. Proves
   it: `TestLobeSearchDefinitionAboveTest`, `TestLobeSearchStable`, `TestLobeSearchSpeed`,
   `TestLobeSearchDeadlineGivesLexical`.
8. **`tentaqles lobe graph find`** (side by side with task 9): `internal/lobe/query/find.go`. Proves it:
   `TestLobeQueryToolCount` (4), `TestLobeQueryNeverBuilds`, `TestLobeQueryOtherClient`,
   `TestLobeQueryFindAllRefused`.
9. **`tentaqles lobe graph map`** (side by side with task 8): `internal/lobe/query/map.go`. Proves it:
   `TestLobeQueryMapSplit`.
10. **Docs**: the `find` and `map` actions under `## tentaqles lobe graph` in `docs/reference/commands.md`;
    a line in `CHANGELOG.md`. The plugin does not change.
11. **Tester run** of the Evaluation below.

## Tests that stay in the repository

- `TestLobeSearchIDF`: `df = 9` and `N = 100` give 2.398 within 0.001.
- `TestLobeSearchBM25`: `tf = 1`, mean-length body, `idf = 1` gives exactly 1.0; `tf = 3` gives 1.571 within
  0.001.
- `TestLobeSearchPageRankPair`: two nodes, one edge, seed on A: B divided by A is 0.7510 within 0.0005.
- `TestLobeSearchDefinitionAboveTest`: for "parse span", `ParseSpan` ranks above `TestParseSpan`.
- `TestLobeSearchStable`: 100 runs of one query on one index give byte-identical output.
- `TestLobeSearchSpeed`: on a 50,000-node index the 95th percentile is at most 100 ms (proposed).
- `TestLobeSearchDeadlineGivesLexical` (new here): with a deadline already passed, the hits are in lexical
  order and no rank was computed.
- `TestLobeSearchIndexBuildID` (new here): an index whose build id differs from the graph's is not used, and
  `find` then exits 1 with the fix line.
- `TestLobeSearchIndexHoldsNoSecret` (new here): a source file with a planted provider-style key gives an
  index whose bytes hold neither the key nor its lower-cased form.
- `TestLobeQueryFindAllRefused` (new here): `find --all` exits 1 with one line and reads no other
  repository. 056 replaces it with `TestLobeQueryFindAll`.
- `TestLobeQueryMapSplit` (new here): 16 folders by default; a folder with more than 60% of the files is
  split one level deeper; the last line counts the rest.
- Widened: `TestLobeQueryToolCount` (exactly 4 tools), `TestLobeQueryNeverBuilds` and
  `TestLobeQueryOtherClient` (now also `find` and `map`), `TestLobeGraphDeterministic`,
  `TestLobeCommandTable`, `TestLobeCommandRefusesAnotherLogin`.
- House rules kept green: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour` (the search package
  has no `init`; the stop-word table is a literal or built on first use), `TestLobeLeak_AllAttacksZeroLeaks`,
  `TestDocsReferenceEveryCommand`, and every test under "Must not trade off" in `CLAUDE.md`, unedited. No
  hook changes, and no model is called.

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
cd "$W/clients/acme/app" && mkdir -p lib app && printf 'module example.com/fix\n' > go.mod
printf 'package lib\n\nfunc ParseSpan(s string) (int, int) { return 0, 0 }\n' > lib/span.go
printf 'package lib\n\nimport "testing"\n\nfunc TestParseSpan(t *testing.T) { ParseSpan("L1") }\n' > lib/span_test.go
printf 'package app\n\nimport "example.com/fix/lib"\n\nfunc LoadA() { lib.ParseSpan("L1") }\n' > app/a.go
tentaqles lobe graph build || echo "SETUP FAIL build"
```

If a line starts with `SETUP FAIL`, stop and report FAIL. Run the checks in `$W/clients/acme/app` unless a
check says otherwise. `F` stands for `tentaqles lobe graph find`.

1. `F "who calls ParseSpan" --json`: exit 0; `"mode":"structural"`; exactly 2 hits, `LoadA` (`app/a.go`)
   then `TestParseSpan` (`lib/span_test.go`).
2. `F "What calls ParseSpan" --json`: the same `mode` and the same hits as check 1.
3. `F "parse span" --json`: `"mode":"lexical"`; the first hit is `ParseSpan` in `lib/span.go`;
   `TestParseSpan` comes after it.
4. `F "who calls NoName" --json`: exit 0; `mode` is not `structural`; `note` has `NoName`.
5. `F "parse span"`: exit 0; the first hit starts `1. ParseSpan` and has `lib/span.go:L3`.
6. `F "parse span" --limit 1` prints exactly 1 hit.
7. `F "parse span" --source` prints a line that contains `func ParseSpan(s string) (int, int)`.
8. `F "parse span" --in app --json` has no hit whose `path` starts with `lib/`.
   `F "parse span" --in ../shop; echo "exit=$?"` prints `exit=1`.
9. `F "parse span" --all; echo "exit=$?"` prints one line and `exit=1`.
10. `F "parse span" --json > o1; F "parse span" --json > o2; cmp o1 o2` is silent.
11. `tentaqles lobe graph map; echo "exit=$?"` prints `exit=0`; one line names `lib` with 2 files and one
    names `app` with 1 file. No line names a symbol.
12. `(cd "$W/clients/globex/app" && tentaqles lobe graph find "parse span"; echo "exit=$?")` prints `exit=1`
    and no `span.go`.
13. `CLAUDECODE=1 CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles lobe graph find
    "parse span"; echo "exit=$?"` prints `exit=1` and no `span.go`. The same with
    `CODEX_HOME="$TQ_HOME/identities/globex/codex"` in place of `CLAUDE_CONFIG_DIR`: `exit=1`.
14. `lobeset acme graph off`. `F "parse span"; echo "exit=$?"` and `tentaqles lobe graph map; echo "exit=$?"`
    each print one line that names `graph`, then `exit=1`. Then `lobeset acme graph on`.
15. `rm -rf "$TQ_HOME/identities/acme/lobe/graph"`. `F "parse span"; echo "exit=$?"` prints `exit=1`, and
    stderr holds `tentaqles lobe graph build`. `test -e "$TQ_HOME/identities/acme/lobe/graph"; echo $?`
    prints `1`: the verb built nothing.

**Person checks**: none.

Scoring: PASS needs 15 of 15. "Could not run" is a FAIL.

## Done when

- [ ] every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin did not change
- [ ] the tester brief passes, 15 of 15
- [ ] the house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeQueryOtherClient`, `TestLobeLeak_AllAttacksZeroLeaks`, `TestLobeSearchIndexHoldsNoSecret`
- [ ] `docs/reference/commands.md` documents `find` and `map`; `CHANGELOG.md` has its line
- [ ] the owner has seen `find` and `map` answer as under Result

## Risks and unknowns

- **The index has no file name and no format in the requirements.** R-01 says "format: F-01"; F-01 says
  "R-01 names its files". Task 1 settles both with the owner before code. A hook must load the index inside
  050's cut-off; Trail-code's comment puts loading its code map at about 0.4 s on a big repository
  (`src/notes/stale.ts` L12). If plain files miss the inject target, F-01's rule allows a database.
- **Term counts are derived from source text.** R-01 does not say how a secret in a source file stays out
  of the index. Step 3 is this file's answer; the owner may want a stricter one.
- **A file node's body** is the whole file here, because its span is the whole file. R-01 does not say.
- **The constants were tuned on Trail-code's benchmarks.** 057 and 058 must confirm them on ours. The cap of
  2 hits per file, the limits 5 and 20, the 30 source lines and the 8,000 characters are all proposed.
- **`TestLobeSearchSpeed`'s 100 ms is proposed** and measured on a synthetic index; a slow CI machine may
  need the test to compare against a recorded baseline. Ask before loosening it.
- **`--limit` above 20** is read as 20 here; R-03 says only "highest 20".
- **What would change this file:** the cache-format decision, an index name chosen by the owner, or a
  different secret rule.
