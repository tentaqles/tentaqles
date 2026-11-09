# 042: Parser without cgo

**Status** not started · **Stage** Graph · **Entries** G-01 (all of it; phase 0 question 1) · **Needs** none ·
**Feeds** 043, 047, 052, 053, 060

## Result

A written result, `docs/prd/spikes/G-01-parser.md`, names one library that runs tree-sitter grammars from Go
with cgo off. It gives a measured value for every one of the ten measures below, on Windows amd64 and Linux
amd64, lists the grammars that are left out of the first release, and recommends one runtime. The owner reads
it and decides. Nothing of the product changes.

A person can repeat the three numbers that matter from the spike folder:

```bash
cd spike/parser
go run . oracle                # per grammar: trees=<a>/<b> captures=<c>/<d>, then "grammars ok: <n>/25"
go run . coldstart --runs 50   # per grammar: p95_ms=<n>
go run . hostile               # last line: panics=0 late=0
```

This feature needs nothing from the lobe. It can be asked for at any time, also before feature 002.

## Scope

**In**

- The grammar list of G-01 step 1: 25 grammars (the 24 Trail-code parses, plus `sql`), and `hcl` as an extra
  with no pass mark.
- The corpus and the oracle (step 2), committed under `spike/parser/testdata/` so a rerun needs neither the
  reference tree-sitter CLI nor the network.
- One adapter per candidate under `spike/parser/candidates/`, tried in the order of step 3, stopping at the
  first candidate that meets every pass mark.
- The ten measures of step 4 on Windows amd64 and Linux amd64 at least.
- The three spike tests and the three harness actions `oracle`, `coldstart` and `hostile`.
- The written result, with the owner's decision recorded in it.

**Out**

- The product adapter `internal/lobe/graph/parse`, the registry and the query files with our added captures
  (feature [043](043-graph-of-one-repository.md)). The spike only fixes the adapter's interface.
- The grammar cache inside a client's store, `index/grammars/` (043). The spike takes a cache folder as a flag.
- The `NOTICE` entries for the runtime and the grammars (043, when they first ship in the binary).
- The node type tables for argument counts, pinned to the grammar revisions this spike fixes
  (feature [047](047-signatures.md)).
- Using the `sql` grammar (feature [052](052-sql-in-the-graph.md)) and deciding between the `hcl` grammar and
  the Go library fallback (feature [053](053-docs-and-config.md)). The spike measures both and decides neither.
- Query files and fixtures for the 12 languages after the first set (feature
  [060](060-remaining-languages.md)).
- A second runtime to cover a gap. G-01 step 5 rules it out: one release carries exactly one runtime.

## What it builds on

Nothing; no lobe code is needed. It uses the Go toolchain the repository already pins in `go.mod`
(`go 1.26.7` today) and changes no file under `internal/`, `cmd/` or `plugin/`. When feature
[002](002-sandbox.md) exists, the tester may start from its sandbox, but no client is needed.

## What it leaves for later features

- **The written result** `docs/prd/spikes/G-01-parser.md`: the chosen runtime and its pinned version, the
  grammars left out and the measure each missed, every measured value, and what each candidate's packages do
  at import time. Read first by 043; then by 052 (`sql` in or out), 053 (`hcl` on the runtime or the fallback)
  and 060 (which of the other 12 grammars may get a row).
- **The adapter interface**, kept by 043 as package `internal/lobe/graph/parse`:
  `Load(lang string) error`, `Parse(ctx context.Context, lang string, src []byte) (Tree, error)` and
  `Captures(lang, query string, t Tree) ([]Capture, error)`, with `Capture{Name string; Start, End uint32}`.
- **The pinned grammar revisions.** 047's parameter tables and 043's query files are tied to them.
- **The corpus and recorded oracle output**, with each file's source URL and license. 043 may copy the first
  set's files into its own fixtures, license lines included.
- **The cold-start figure per grammar.** Features [049](049-blast-radius.md) and
  [051](051-argument-check.md) parse one file inside the PostToolUse hook and build on this number;
  [012](012-hook-latency-and-stall.md) measures the whole event later.

## How it works

1. **Grammars.** 25: `c`, `c_sharp`, `clojure`, `cpp`, `dart`, `elixir`, `gleam`, `go`, `java`, `kotlin`,
   `lua`, `nix`, `ocaml`, `php`, `python`, `r`, `ruby`, `rust`, `scala`, `solidity`, `swift`, `tsx`,
   `typescript`, `zig`, and `sql`. "First set" means G-02's 12 (`go`, `python`, `typescript`, `tsx`, `java`,
   `c_sharp`, `rust`, `c`, `cpp`, `ruby`, `php`, `kotlin`) plus `sql`: 13 grammars. `hcl` is tried on every
   candidate and its result recorded, with no pass mark.
2. **Corpus.** 20 (proposed) pinned public files per grammar. Each is recorded with its source URL and its
   license. A file under a license that forbids committing a copy is not used.
3. **Query files.** The oracle also needs G-02's query files as they exist before our added captures: the 8
   Trail-code files (java, c, cpp, c_sharp, ruby, rust, php, kotlin), copied unchanged, and the `tags.scm`
   of the grammar repositories for go, python, typescript and tsx. `sql` has no query file in G-02, so it is
   measured on trees only (this file's reading of measure 4).
4. **Oracle.** The reference tree-sitter CLI prints each corpus file's S-expression and each query's captures
   (name, start byte, end byte). Both outputs are committed under `spike/parser/testdata/`. The CLI's version
   and each grammar's revision are written beside them.
5. **Candidates, in order.** (a) `gotreesitter`, a pure-Go tree-sitter runtime: import only the 25 grammar
   packages, build with `-tags gotreesitter_no_copyleft`, pin the version, and note each grammar that needs an
   external scanner and has none. (b) Grammars compiled to WebAssembly under `wazero`: first check whether
   grammar `.wasm` files built for the browser runtime can be hosted; if not, compile the tree-sitter C
   runtime and the 25 `parser.c` files into one WASI module. Use one instance per goroutine,
   `WithCloseOnContextDone(true)` and a low memory limit. (c) Others, `bonsai` first. Stop at the first
   candidate that meets every pass mark. Candidate (b) is measured only when (a) misses a mark, and the
   result says so.
6. **One adapter interface** for all candidates (see "What it leaves"). The parser gets the whole byte slice
   of a file, never slices of it. No work happens at import time: a grammar loads on the first `Load`, never
   from an `init` function or a package-level variable (core page 5.1).
7. **The cache.** If a candidate keeps a cache of compiled grammars or parser tables, the harness takes the
   cache folder as a flag and reports cold start with it empty and warm, its size on disk and the time to
   fill it. In the product that cache is per client (G-01 "How to build it"); the spike only measures.
8. **The ten measures**, every pass mark proposed:

   | # | Measure | Pass mark (proposed) |
   |---|---|---|
   | 1 | `CGO_ENABLED=0 go build` for linux, darwin and windows on amd64 and arm64, with no GPL or MPL grammar linked | 6 of 6 exit 0; yes |
   | 2 | Grammars that load and parse their corpus files | 13 of 13 in the first set; at least 22 of 25 |
   | 3 | Trees equal to the oracle's S-expression | 100% of first-set files; at least 98% of all files |
   | 4 | Captures equal to the oracle's, with `#eq?`, `#match?` and `#any-of?` applied | 100% in the first set |
   | 5 | Cold start: process start to the first finished parse of a 200-line file, per grammar | 95th percentile at most 250 ms with a warm cache; the empty-cache value is reported with no mark |
   | 6 | Throughput: parse plus queries over the whole corpus on one core | at least 1 MB of source per second |
   | 7 | Growth of the stripped binary (`-trimpath -ldflags "-s -w"`) | at most 20 MB for the first set and 40 MB for all 25 |
   | 8 | Peak memory while parsing one 1,000,000-byte file | at most 400 MB |
   | 9 | 8 goroutines parsing at once under `go test -race` (only this run has cgo on) | 0 race reports; results equal the serial run |
   | 10 | Hostile input (a cut-off file, random bytes, one 1 MB line) under a 5-second deadline per file | 0 panics leave the adapter; 0 calls run past the deadline |

9. **One runtime (decided, step 5).** A grammar the chosen runtime cannot load, or whose trees or captures
   differ from the oracle's, is left out of the first release. When no candidate meets every mark, the result
   ranks the candidates by the grammars each parses correctly, first set first, and recommends the best one
   with a shorter language list. It never recommends a blend of two.
10. **The written result** holds: every value of the table for each measured candidate; the machine, OS and Go
    version of each run; the grammars left out and the measure each missed; what each candidate's own packages
    do at import time and what that adds to the start of a process that parses nothing; the `hcl` result; and
    one recommendation. A last section, "Decision", is filled in by the owner.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-01](../prd/lobe-graph.md), all of it; [core page](../prd/vertical-lobe.md) 5.1 (one program, no cgo, no work at import time), 5.2 (the budgets measure 5 is set against) and section 8 (phase 0 question 1) |
| The first-language list | [G-02](../prd/lobe-graph.md), the table row "first languages" and the Source rows for the query files |
| Candidate (a) | gotreesitter [README](https://raw.githubusercontent.com/odvcencio/gotreesitter/main/README.md) and [BENCH.md](https://raw.githubusercontent.com/odvcencio/gotreesitter/main/BENCH.md). Web, not machine-checked |
| Candidate (b) | wazero [README](https://raw.githubusercontent.com/tetratelabs/wazero/main/README.md), [Package page](https://pkg.go.dev/github.com/tetratelabs/wazero), [API page](https://pkg.go.dev/github.com/tetratelabs/wazero/api). Web, not machine-checked |
| Proof that WebAssembly grammars carry a product | Trail-code [`src/graph/generic.ts` L131](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L131) and [`package.json` L63](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/package.json#L63) |
| The 24 grammars | Trail-code `src/graph/extract.ts` L32 and [`src/graph/generic.ts` L47](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/generic.ts#L47) |
| Candidate (c) | bonsai [README](https://raw.githubusercontent.com/msuozzo/bonsai/main/README.md). Web, not machine-checked |
| Why the cgo bindings are out | [cgo documentation](https://raw.githubusercontent.com/golang/go/master/src/cmd/cgo/doc.go); [Race detector article](https://go.dev/doc/articles/race_detector). Web, not machine-checked |
| Existing tentaqles code | `go.mod` (the Go version); `.github/workflows/test.yml` (CI runs `go vet ./...` and `go test ./... -count=1`); `.goreleaser.yaml` (the six targets). Nothing under `internal/` is touched |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the Go version in
   `go.mod`, the six release targets in `.goreleaser.yaml`, whether a `spike/` folder already exists, and
   the current versions of `gotreesitter`, `wazero` and `bonsai` (G-01 notes that gotreesitter v0.57.0 was
   published on 2026-11-08). Pin a version of each. The owner approves changes to this file before code.
2. **Scaffold the spike** on a branch that is never merged: `spike/parser/` with its own `go.mod` (so no
   candidate dependency enters the root `go.mod`; the tester runs `go mod download` there), the adapter
   interface, `candidates/`, `testdata/`, and a `main.go` with the actions `oracle`, `coldstart` and
   `hostile`. Check: `CGO_ENABLED=0 go build .` exits 0 with no candidate yet.
3. **Corpus and oracle** (side by side with task 4). Pick the files, record URL and license, run the
   reference CLI, commit its S-expressions and captures. Check: every grammar has 20 files and every file a
   license line; the 12 query files are present.
4. **Harness** (side by side with task 3): the comparison of trees and captures, the cold-start timer (a
   fresh process per run), the throughput and memory measures, the hostile inputs, and the three tests of
   "Tests that stay in the repository". Check: the tests fail cleanly against an adapter that returns nothing.
5. **Candidate (a), `gotreesitter`.** Write its adapter; run measures 1 to 10 on Windows amd64. Check: a
   value in every cell.
6. **Candidates (b) and (c), only if (a) misses a mark.** Same measures, same harness. If (b) is not needed,
   the result says why.
7. **Linux amd64 run** of every measured candidate (a CI job on the spike branch or a Linux machine). Also
   the six cross-builds of measure 1 and the binary growth of measure 7. Check: the two machines' values sit
   side by side in the result.
8. **`hcl`** on each measured candidate: loads or not, trees right or not. Recorded, no mark.
9. **Write `docs/prd/spikes/G-01-parser.md`** in the form of step 10 above. Add one line to `CHANGELOG.md`
   under `[Unreleased]` when the result lands on the main line. No command is added, so
   `docs/reference/commands.md`, `plugin/CHANGELOG.md` and `CLAUDE.md` do not change.
10. **Tester run** of the Evaluation below, by an agent that wrote none of the spike.
11. **The owner's decision**, written into the result's "Decision" section: the runtime, and the grammars
    left out.

## Tests that stay in the repository

The spike branch is never merged, so these three tests live in `spike/parser/` on that branch and CI on the
main line does not run them. They are the record a rerun can repeat.

- `TestLobeSpikeParserMatchesOracle`: 0 tree mismatches and 0 capture mismatches in the first set.
- `TestLobeSpikeParserColdStart`: the 95th percentile of 50 runs per grammar, grammar cache warm, is at most
  250 ms (proposed).
- `TestLobeSpikeParserHostileInput`: 0 panics and 0 late returns.

House rules: the guard is untouched because nothing under `internal/`, `cmd/` or `plugin/` changes on the
spike branch; every test named under "Must not trade off" in `CLAUDE.md` stays as it is. The adapters build
with `CGO_ENABLED=0`; only the race run of measure 9 has cgo on. No model is called and no client store is
read.

## Evaluation

You are a tester agent. You did not write the spike. You receive the `spike/parser` folder with its
`testdata` corpus and recorded oracle output, the Go toolchain, a C compiler on `PATH` (only the race run
in check 1 uses it), the written result, and a checkout of the main line to compare against. Network is
allowed for check 1 only. No `tentaqles` binary and no client are needed.

Setup, in bash (Git Bash on Windows), from an empty working folder:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles"
```

Then run every command in `spike/parser`.

1. `go mod download`, then `CGO_ENABLED=0 go test ./...`, then `CGO_ENABLED=1 go test -race ./...`.
   Expected: exit code 0 three times.
2. With `CGO_ENABLED=0`, for each pair of `GOOS` (linux, darwin, windows) and `GOARCH` (amd64, arm64), run
   `go build -o out/<os>-<arch> .`. Expected: exit code 0 six times.
3. `go run . oracle`. Expected: per grammar a line `<lang> trees=<a>/<b> captures=<c>/<d>`, with `a` = `b`
   and `c` = `d` for the first set, then `grammars ok: <n>/25` with `n` at least 22. The numbers equal the
   written result, and the grammars not counted in `n` are exactly those the result lists as left out.
4. `go run . coldstart --runs 50`. Expected: per grammar a line `<lang> p95_ms=<n>` with `n` at most 250.
5. `go run . hostile`. Expected: the last line is `panics=0 late=0` and the exit code is 0.
6. Read `docs/prd/spikes/G-01-parser.md`. Expected: a value in every cell of the ten measures for candidate
   (a) and for candidate (b), or a sentence that says why (b) was not needed; the machine, OS and Go version
   of each run; Windows amd64 and Linux amd64 both present.
7. The same file recommends exactly one runtime and lists the grammars left out of the first release, or says
   there are none. It never recommends two runtimes together.
8. In the checkout, `git diff --name-only main...HEAD -- internal cmd plugin` prints nothing.

**Person checks**

1. The owner reads the result and writes the decision into its "Decision" section: the runtime, and the
   grammars left out of the first release.

Scoring: PASS needs 8 of 8. If check 3 or 4 fails only because no candidate meets every mark, report FAIL
with the numbers; G-01 step 5 then applies, and the gate becomes the owner's written decision on the shorter
language list.

## Done when

- [ ] `TestLobeSpikeParserMatchesOracle`, `TestLobeSpikeParserColdStart` and `TestLobeSpikeParserHostileInput`
  are on the spike branch and green there with `CGO_ENABLED=0 go test ./...`
- [ ] `go vet ./...`, `go test ./... -count=1` and `go build ./...` are green on the main line, unchanged
- [ ] the tester brief passes, or the owner has accepted a shorter language list in writing (step 9)
- [ ] the house rules hold: no file under `internal/`, `cmd/` or `plugin/` changed
- [ ] `docs/prd/spikes/G-01-parser.md` has every value, both machines, the left-out grammars, the import-time
  note, the `hcl` result and one recommendation; `CHANGELOG.md` has its line
- [ ] the owner has read the result and recorded the decision in it

## Risks and unknowns

- **Thin speed evidence (web).** gotreesitter's "4.815 times slower than C" is its own figure, and wazero's
  4.72-times slowdown is a
  [third-party test](https://00f.net/2026/06/23/webassembly-runtimes-2026/) on C cryptography code. Neither
  was machine-checked. The spike's own numbers replace both.
- **A moving API (web).** gotreesitter v0.57.0 is days old. Pin the version in task 1.
- **The wazero route may need our own WebAssembly build** of the grammars, which needs a C toolchain at
  build time of the spike only. `bonsai` has no documented query API, so choosing it means writing a query
  engine in 043.
- **Cold start at the mark leaves little room.** 250 ms of a 300 ms inject target (both proposed) leaves
  about 50 ms for the rest of the PostToolUse work of 049 and 051. 049 has a fallback that does not parse.
- **Import-time work.** A third-party runtime may do work at import time that every `tentaqles` process
  pays, the guard's hook included. The result must state it; it can rule a candidate out.
- **A per-client grammar cache** makes the first parse in every new client slow. The result reports the
  time to fill it.
- **The four `tags.scm` files** (go, python, typescript, tsx) were not read by machine when the requirements
  were written. Measure 4 is their first check.
- **What would change this file:** a candidate list that has moved on; a decision to measure on more
  machines; the owner allowing a second runtime, which G-01 step 5 forbids today.
