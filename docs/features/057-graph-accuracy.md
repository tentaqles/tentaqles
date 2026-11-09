# 057: Graph accuracy against Trail-code

**Status** not started · **Stage** Graph · **Entries** M-07; M-02 (the `graphdiff` action of `lobebench`); A-27 ·
**Needs** 047 · **Feeds** 060

## Result

Our graph and Trail-code's graph of the same repositories at the same commits are compared node by node and edge
by edge. Every difference carries a written verdict: our bug, their bug, or a deliberate difference. A person
sees a report per repository and relation, and a run that fails when one difference has no verdict.

```bash
go build ./bench/cmd/lobebench
lobebench graphdiff --fixture py2 --out /tmp/g1          # exit 0, "unexplained":0
lobebench graphdiff --fixture py2-ghost --out /tmp/g2    # exit 1, one unexplained node of theirs
```

The real comparison covers one fixture per first-set language and the three public repositories of the token
benchmark. It ends with a labeled sample of 200 edges (proposed) that says how often each graph is right.

## Scope

**In**

- `bench/internal/graphdiff` (`load.go`, `ours.go`, `diff.go`, `verdict.go`) and the action `lobebench graphdiff`.
- `bench/graphdiff/map.yaml`, `bench/graphdiff/verdicts.yaml`, and Trail-code's graphs committed as
  `bench/graphdiff/theirs/<repo>@<commit>.json`.
- The four built-in fixtures `py2`, `py2-ghost`, `py2-ghost-explained` and `py2-stale`.
- One fixture per language of the first set that Trail-code also parses (12, less any that G-01's result left
  out), and the three public repositories M-02 pins.
- The verdicts known before the first run (M-07 step 9), the numbers and the two gates (M-07 step 7).
- The labeled sample: 200 edges, two labeling agents, a person's check of 50 and of every disagreement (M-07
  step 8, A-27).
- The script that builds Trail-code's graph of a public repository in a container.

**Out**

- SQL nodes ([052](052-sql-in-the-graph.md)), documents and configuration ([053](053-docs-and-config.md)),
  why-comments ([054](054-why-comments.md)) and model-read topics ([061](061-pdfs-and-images.md)). Trail-code has
  no counterpart, so M-07 does not cover them. The diff ignores our nodes of those kinds and their edges.
- The 12 later languages. Each adds its own fixture and reruns this comparison
  ([060](060-remaining-languages.md)).
- Links between repositories ([056](056-many-repositories.md)). Each repository is compared alone.
- Recall of the blast-radius walk. G-09's risks say M-07 "should report" it; M-07's mechanism has no such step.
  No feature takes it.
- Confirming R-01's ranking constants on our data. R-01's risks name M-07 for it; M-07 has no such step. No
  feature takes it.
- Any client repository. Trail-code is run only on public code.

## What it builds on

- [021](021-benchmark-harness.md): the program `bench/cmd/lobebench`, its `--fixture` and `--out` rules (a home
  under `--out`, an existing folder refused, an `--out` inside the real `TQ_HOME` or a client's folder refused),
  `summary.json`, and the embedded fixtures in `bench/testdata`. The index lists only 047 under "Needs"; this
  feature cannot start before 021.
- [043](043-graph-of-one-repository.md): `internal/lobe/graph` and `graph/extract`, the node and edge shapes, the
  first languages and their fixture repositories with golden files (`TestLobeGraphExtractFixtures`).
- [044](044-graph-soundness.md): `Check(g *Graph) Report` and `schema.go` with the allowed kinds, relations,
  confidence values and rule names.
- [045](045-resolution-and-callers.md): the six relations, the `rule` on every edge, `extends` and `implements`
  apart from `references`, and the decision that a recursive call gives no `calls` edge.
- [047](047-signatures.md): `sig`, `params` and `argc`, so the graph under test is the one phase 4 ships.

## What it leaves for later features

- `lobebench graphdiff` and `bench/internal/graphdiff`, which [060](060-remaining-languages.md) reruns for each
  language it adds.
- `bench/graphdiff/verdicts.yaml` and `map.yaml`, which 060 extends.
- The pinned list of the three public repositories with their commits. If [021](021-benchmark-harness.md) did not
  pin them, this feature does, in the task files under `bench/tasks/`, and
  [059](059-token-benchmark.md) uses the same pins.
- The precision per resolution rule from the labeled sample, which the owner needs for two open questions of
  G-03 (see step 9).

## How it works

1. **Their graph.** A script builds Trail-code at the commit pinned in core page section 7 in a container that
   holds only the public repository, with the network off during the build of the graph, no language-server
   pass and no model. Its output, the file `wiring.json` in its `.graph` folder, is committed as
   `bench/graphdiff/theirs/<repo>@<commit>.json`. A rerun then needs neither Trail-code nor the network.
   Installing Trail-code needs Node and one network install, before the network is switched off.
2. **Our graph.** `ours.go` builds our graph in-process through `internal/lobe/graph` on a copy of the
   repository under `--out`. It reads and writes no client's store.
3. **Soundness first.** Both graphs pass through `Check` (G-12). `load.go` turns their file into our `Graph`
   type; an edge of theirs whose target is an outside name, in the four relations Graft's script excuses,
   becomes an `ext` line, because our format has no excused edge. M-07 says both graphs "first pass" the check
   and also expects their kind `trait` to fail it. This file reads that as: a problem in our graph stops the run;
   a problem in their graph is listed as a difference that needs a verdict.
4. **Keys.** A node is (path, kind, name, start line). An edge is (source key, relation, target key or outside
   name). `map.yaml` maps kinds and relations that are named differently. Their one relation `references`
   matches an edge of ours with the same ends whose relation is `extends`, `implements` or `references`.
5. **The diff** gives three lists: only theirs, only ours, and same key with a different span or confidence.
   A file one side skips (generated, vendored, too large) is one difference per file, not one per node. Line
   endings are normalized before spans are compared. Node order never matters.
6. **Verdicts.** Each difference needs one in `verdicts.yaml`: `ours-bug` (with an issue; it blocks the phase),
   `theirs-bug` (with the Trail-code file and line that explain it), or `deliberate` (with the entry that
   decided it). A verdict matches on side, kind, relation, language and path glob, so one may cover a class. A
   verdict that matches nothing is an error.
7. **Known before the first run** (M-07 step 9), entered in `verdicts.yaml` from the start: their kind `trait`
   for PHP is `theirs-bug`; a self-loop `calls` edge that is only theirs is `deliberate` (G-02); the confidence
   values `lsp_resolved` and `lsp_dispatch` never appear in ours and are `deliberate` (core page section 11).
8. **Numbers** per repository and relation: matched over theirs, matched over ours, the count per verdict and
   the unexplained count, in `summary.json`, with every difference in `diff.jsonl`. Gates: unexplained 0, and
   `ours-bug` 0 at the end of phase 4. Targets for the real run (proposed): at least 99% of their nodes and 95%
   of their `calls` edges with confidence `extracted` matched before verdicts.
9. **The labeled sample.** Agreement is not correctness. 200 edges (proposed) are drawn at random from the
   union with a recorded seed and labeled right or wrong against the source code. Two labeling agents, neither
   of which wrote the graph code under test, each label every edge. A person checks a random 50 (proposed) and
   every edge where the two agents disagree; the person's label stands. The report gives each graph's precision
   on the 200 edges and says who labeled what. For our edges it also splits right and wrong by `rule`, because
   G-03 leaves two questions to this sample: whether `unique-name` misleads in Python, and how often
   `unique-nontest` does.
10. **Exit codes.** 0 when unexplained is 0; 1 when a difference has no verdict; non-zero with a message when a
    verdict matches nothing.

## Where to read

| What | Where |
|---|---|
| Requirements | [M-07](../prd/lobe-benchmarks.md), all of it; [M-02](../prd/lobe-benchmarks.md), "How to build it" (the harness program, `--out`, fixtures); [G-12](../prd/lobe-graph.md) steps 1 to 3; [G-02](../prd/lobe-graph.md) step 3 and the `relation` row; [G-03](../prd/lobe-graph.md) risks; [core page](../prd/vertical-lobe.md) section 7 (the pinned commits) and decision A-27 |
| The reference graph | [`src/claude/stats.ts` L9](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/stats.ts#L9): where `wiring.json` is read from; [`src/graph/types.ts` L40](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/types.ts#L40): the four confidence values |
| Their bug, known | [`src/graph/types.ts` L24](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/types.ts#L24) declares kind `trait`; the allowed set at [`src/graph/invariants.ts` L26](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/invariants.ts#L26) has 11 kinds without it |
| The allowed names | [`scripts/graph-quality.mjs` L19](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/scripts/graph-quality.mjs#L19): the allowed kinds, relations and confidence values |
| Caution | Graft 0.17.0 `README.md` L113 and L169 disagree on whether a build pass uses a model. Only the structural file is compared |
| Existing code | `bench/cmd/lobebench` and `bench/internal/` from 021; `internal/lobe/graph` from 043 to 047; `test/integration/` for how CI runs a built program |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: the layout of `bench/` after
   021, how a `lobebench` action is registered, whether the three public repositories are pinned and where, and
   which first-set languages G-01's result left out. Files: none. Check: a written list the owner approves.
2. `bench/internal/graphdiff/load.go` and `ours.go`, with the conversion of step 3. Test:
   `TestLobeGraphDiff_OrderIndependent`. Can run beside task 3.
3. `diff.go`, `verdict.go`, `bench/graphdiff/map.yaml`, and the action `lobebench graphdiff` writing
   `summary.json` and `diff.jsonl`. Tests: `TestLobeGraphDiff_FindsAPlantedDifference`,
   `TestLobeGraphDiff_StaleVerdictFails`.
4. The four `py2` fixtures under `bench/testdata`. Their graph of the two-file Python repository is a real
   Trail-code output, made with the script of task 5. Check: the four tester checks below.
5. The container script that builds Trail-code's graph of one public repository at one commit. It runs on
   public code only. It needs a container runtime and one network install; if the builder's machine has
   neither, the owner runs it (person check 1). Check: the script's output for `py2` equals the committed file.
6. One fixture per first-set language: reuse 043's fixture repositories, commit their graph under
   `bench/graphdiff/theirs/`, and write the verdicts. Test: `TestLobeGraphDiff_FixturesFullyExplained`. One
   agent per language can work side by side; `verdicts.yaml` is merged at the end.
7. The three public repositories at M-02's commits: their graphs committed, the diff run, every difference
   given a verdict. An `ours-bug` verdict opens an issue and goes back to the feature that owns the code (043,
   045 or 047). Check: `summary.json` with unexplained 0.
8. The labeled sample: the draw with its seed, the two labeling agents, the report with precision per graph and
   per rule. The person's part is person check 2.
9. Docs and changelog: one line in `CHANGELOG.md` under `[Unreleased]`. No `tentaqles` command is added, so
   `docs/reference/commands.md` does not change. The harness's own help text names `graphdiff`.
10. The tester run of "Evaluation", by an agent that built none of this.

## Tests that stay in the repository

- `TestLobeGraphDiff_FixturesFullyExplained`: unexplained 0 on the real graph of every fixture repository.
- `TestLobeGraphDiff_FindsAPlantedDifference`: one invented node in their file gives exactly 1 difference.
- `TestLobeGraphDiff_StaleVerdictFails`: a verdict that matches nothing exits non-zero.
- `TestLobeGraphDiff_OrderIndependent`: shuffled input gives the same report.
- `TestLobeGraphDiff_SkippedFileIsOneDifference` (new here): a file only one side has gives 1 difference,
  whatever its node count.
- `TestLobeGraphDiff_TheirReferencesMatchOurThree` (new here): one `references` edge of theirs matches our edge
  with the same ends for each of `extends`, `implements` and `references`. 3 of 3.
- Kept green from earlier features: `TestLobeBenchHarnessIsInTheRepo`, `TestLobeBenchShipsOnlyTheFourChecks`
  (the released program gains no action), `TestLobeGraphSoundFixtures`, `TestLobeGraphExtractFixtures`.
- House rules: the action refuses an `--out` inside the real `TQ_HOME` or a client's folder (021's test); the
  import rule that nothing under `cmd/` or `internal/` imports a package under `bench/`; the guard's tests
  untouched.

## Evaluation

Tester brief. Start from the standard tester setup of [002](002-sandbox.md) (F-07). No network. This brief needs
one file more than that setup: the harness program `lobebench`, built from the same commit with
`go build ./bench/cmd/lobebench` and put on `PATH` beside `tentaqles`. Four built-in fixtures share a two-file
Python repository: `py2` (Trail-code's real graph of it), `py2-ghost` (that graph plus one invented node
`ghost.py#ghost`), `py2-ghost-explained` (the same with a verdict for that node) and `py2-stale` (the real graph
with that verdict). The harness builds our graph in-process on a copy of that repository under `--out`.

1. `lobebench graphdiff --fixture py2 --out "$W/g1"`. Expect exit 0, and in `$W/g1/summary.json`
   `"unexplained":0` and `matched_over_theirs` 1.0 for nodes.
2. The same with `--fixture py2-ghost --out "$W/g2"`. Expect exit 1, `"unexplained":1`, and one line in
   `$W/g2/diff.jsonl` with `"side":"theirs"` and `"node":"ghost.py#ghost"`.
3. The same with `--fixture py2-ghost-explained --out "$W/g3"`. Expect exit 0, `"unexplained":0` and
   `"theirs-bug":1`.
4. The same with `--fixture py2-stale --out "$W/g4"`. Expect a non-zero exit and a message that a verdict
   matches nothing.
5. Run check 1's command again. Expect a non-zero exit and a message naming `$W/g1`: a used folder is refused.
6. List the files under `$TQ_HOME/identities/acme/lobe/` before and after checks 1 to 4. Expect no difference:
   the harness touched no client's store.
7. `tentaqles lobe bench --help | grep -c graphdiff` prints `0`: the released program has no such action.

**Person checks** (the owner)

1. If the builder could not run a container: run the script of task 5 for each pinned repository and hand back
   the `wiring.json` files. They hold public code only.
2. Label the sample: read the 50 drawn edges and every edge the two labeling agents disagree on, mark each right
   or wrong against the source, and return the file. The report names the owner as the checker.
3. Read the real run's `summary.json`: unexplained 0 and `ours-bug` 0 for every repository.

Scoring: PASS needs 7 of 7. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`, which now also builds `./bench/...`).
- [ ] The tester brief passes, 7 of 7.
- [ ] The real run is committed: every fixture and the three public repositories with unexplained 0 and
  `ours-bug` 0, and the labeled-sample report with the owner's labels.
- [ ] The house rules hold: no client store touched (check 6), `TestLobeBenchShipsOnlyTheFourChecks`, the guard's
  tests untouched.
- [ ] `CHANGELOG.md` updated.
- [ ] The owner has seen the result described under Result and the precision per rule.

## Risks and unknowns

- Trail-code is a reference, not the truth. Step 9 is the only check on real correctness, 200 edges is small,
  and a person sees 50 of them plus the disagreements.
- A different parser runtime (G-01) can give different trees for the same grammar. Those differences appear here
  first and may be many. Each still needs a verdict.
- Building their graph needs Node, a network install and a pinned commit. The committed files keep reruns
  offline, but they go stale if the repositories are re-pinned.
- The shape of `wiring.json` was read, not run. Task 4 produces the first real file; `load.go` follows what that
  file holds, and `map.yaml` grows with it.
- "Both graphs first pass the soundness check" and "kind `trait` is their bug" cannot both hold for a PHP
  fixture. This file's reading is in step 3. If the owner reads it otherwise, step 3 and `load.go` change.
- All numbers marked proposed (200 edges, 50 checked, the 99% and 95% targets) are the requirements' proposals.
- The order problem: the index puts this feature after 047 only, yet it needs 021's harness, and its three
  public repositories are M-02's, which 059 uses after it.
