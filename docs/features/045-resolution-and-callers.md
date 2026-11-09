# 045: Who calls what

**Status** not started · **Stage** Graph · **Entries** G-03 (all but step 4, overloads), G-02 (`ext` lines,
`dropped`, `lines`, the recursion test), R-03 (callers; steps 2, 5, 6) · **Needs** 044 ·
**Feeds** 046, 047, 049, 051, 052, 053, 056, 057

## Result

The graph now says which function calls which, across files, and how sure it is. A reference the code
states outright is labeled `extracted`; one that rests on a unique name is `inferred`; an ambiguous one is
dropped and counted, never guessed. `tentaqles lobe graph callers <name>` lists the callers with that label
and the line of each call.

```bash
cd "$W/clients/acme/app"        # the Go fixture of the Evaluation
tentaqles lobe graph build
tentaqles lobe graph callers ParseSpan
```

```
ParseSpan · function  lib/span.go:L3-L3
1. calls <- LoadA · function  app/a.go:L5  extracted
     func LoadA() { lib.ParseSpan("L1") }
2. calls <- TestParseSpan · function  lib/span_test.go:L5  inferred
     func TestParseSpan(t *testing.T) { ParseSpan("L1") }
```

## Scope

**In**

- `internal/lobe/graph/resolve` in full but for overloads: the indexes of G-03 step 1, rules 2 to 11, the
  kinds of step 3, the families of step 5, the duplicate removal of step 6.
- One import resolver per first language, the Go module and folder rules, and `testpath.go`, the lobe's one
  definition of a test path.
- Edge lines with `confidence`, `score`, `rule` and `lines`; `ext` lines; the `dropped` counters on the meta
  line.
- The lookup interface the rules read nodes through, with its in-memory form.
- `tentaqles lobe graph callers <symbol> [--out] [--in <path>] [--json]` (R-03 step 2), with the rules of
  steps 5 and 6.

**Out**

- G-03 step 4, picking among overloads by argument count, and `TestLobeResolveOverloadNoFit`
  ([047](047-signatures.md), which brings the counts). Until then same-named methods on one owner are
  several candidates, so a call to them is dropped as `ambiguous`.
- `argc` on edges (047).
- The search index that serves the three lookups inside a hook ([046](046-find-and-map.md) writes it,
  [051](051-argument-check.md) reads it in a hook). Nothing here runs in a hook.
- Walks deeper than one edge, and `tentaqles lobe graph impact` ([049](049-blast-radius.md)).
- `find` and `map` (046); `--all` and links between repositories ([056](056-many-repositories.md)).
- "(may be stale)" on a hit ([048](048-graph-stays-current.md)).
- Edges for SQL ([052](052-sql-in-the-graph.md)), documents and configuration
  ([053](053-docs-and-config.md)).
- The measured precision of these rules ([057](057-graph-accuracy.md)).

## What it builds on

- **[043](043-graph-of-one-repository.md)**: nodes and raw references in the parse cache (with
  `import.source`, `import.name`, `receiver`, `extends`), `resolve.Resolve` with rule 1, `build.go`,
  `graph.jsonl`, the fixtures, `internal/lobe/query` (`registry.go`, `render.go` with the 8,000-character
  cap), `TestLobeQueryToolCount`, `TestLobeQueryNeverBuilds`, `TestLobeQueryOtherClient`.
- **[044](044-graph-soundness.md)**: `schema.go`, which already lists G-03's nine rule names, six relations
  and two confidence values; `Check` inside every build; `TestLobeGraphSoundFixtures`.
- **[005](005-status-and-capture-switch.md)**: `internal/lobe/cli`, F-07 step 3 and its tests.
- **[002](002-sandbox.md)**: the sandbox.

The extractor version does not change: 043 already wrote every capture these rules read.

## What it leaves for later features

- `internal/lobe/graph/resolve`: `resolve.go`, `imports.go`, `families.go`, `testpath.go`. The rule table is
  one function over a small lookup interface (by name; by file and name; by owner id and name). `Resolve`
  feeds it in-memory tables. 046 writes the same three lookups into the index, and 051 serves them in a
  hook, so an edge in the graph and an answer in a hook cannot disagree.
- `testpath.go`: used by 046 (the test factor of the ranking) and 049 (tests set apart).
- Edge lines `{t, source, relation, target, confidence, score, rule, lines}` and `ext` lines
  `{t, source, relation, name}` in `graph.jsonl` (046, 047, 049, 052, 056, 057).
- `Hit{ID, Name, Path, Depth, Relation, Confidence, Score, Site}` in `internal/lobe/graph`, JSON keys in
  lower case, and the one-step walk over edges. G-09 names this shape and says `callers` uses its walk; 049
  takes the walk to depth 2.
- The command `tentaqles lobe graph callers`, the second tool of the registry (062 serves it as
  `lobe_callers`).

## How it works

1. **When.** Resolution runs in the build of 043, after extraction and before the soundness check: all nodes
   and all raw references of the repository in, edges, `ext` lines and counters out. `Resolve` is pure.
2. **Indexes** (G-03 step 1): nodes by name, by file then name, and by owner id then name. Class parents come
   from the raw `extends` captures. Owner keys are node ids, so same-named classes never merge.
3. **The rule table** (step 2). For each raw reference the first rule that fits wins. "Exactly one" is
   counted after filtering by allowed kind. No rule picks "the first", "the nearest" or "the most likely".

   | # | Applies to | Result |
   |---|---|---|
   | 1 | containment (043) | extracted, 1, `contains` |
   | 2 | an import whose module text resolves to a file in the repository | extracted, 1, `import`; otherwise an `ext` line and no edge |
   | 3 | a bare name with exactly one definition in the same file | extracted, 1, `same-file` |
   | 4 | a bare name the file imports from a module that rule 2 resolved, with exactly one such definition there | extracted, 1, `import-symbol` |
   | 5 | a member call on `self`, `this` or `cls`: exactly one method of that name on the owner, else on a parent up to 3 levels | same file: extracted, 1; other file: inferred, 0.85; `self-member` |
   | 6 | a member call on an imported module or package name: exactly one definition in that module's file (Go: in the package folder) | extracted, 1, `import-member` |
   | 7 | a member call on a type name that resolves by rule 3, 4 or 9 and has exactly one such method | the type's label and score, `owner-member` |
   | 8 | a member call on anything else | dropped, counted `untyped` |
   | 9 | a bare name outside TypeScript and JavaScript with exactly one definition in the repository, same family (Go: same folder) | inferred, 0.85, `unique-name` |
   | 10 | a bare name with several candidates, caller not a test file, exactly one left after removing test files | inferred, 0.75 (proposed), `unique-nontest` |
   | 11 | anything left | dropped, counted `ambiguous` (two or more candidates) or `unknown` (none) |

4. **Kinds** (step 3). A call looks for a function or method; if no node of that name has those kinds, for a
   class, struct or enum (a constructor). Other references look for a class, interface, struct, enum, type
   or module.
5. **Relations.** A raw call gives `calls`; an `extends` capture gives `extends`; a
   `reference.implementation` capture gives `implements`; every other raw reference gives `references`; an
   import gives `imports` from the file node to the file node. A base type with no candidate in the
   repository becomes an `ext` line, like an import that resolves to nothing.
6. **Families** (step 5): `typescript` with `tsx`; `java`, `kotlin`, `scala`, `clojure`; `c` with `cpp`; all
   others alone.
7. **Imports.** One resolver per language turns the module text into a repository file. A Go import targets
   the package folder's lowest-sorted non-test file; the module path comes from `go.mod` (`mods
   []GoModule`). Re-exports and path aliases are not followed: they stay `ext`.
8. **Test paths.** `testpath.go` ports Graphify's patterns (`graphify/paths.py` L261 to L284). A production
   folder named `spec` or `test` counts as tests; rule 10 can only add an edge at 0.75, so the harm is
   bounded.
9. **Duplicates** (step 6). Edges are unique on source, relation and target; the higher score stays. `lines`
   holds the reference lines of the merged edge, sorted, at most 20 (proposed). `contains` has no `lines`.
10. **The meta line** now carries real `dropped` counts, and `edges` counts every edge line. `ext` lines
    follow the edge lines, sorted by byte value.
11. **Callers.** `tentaqles lobe graph callers <symbol>` loads this working tree's `graph.jsonl`. It
    resolves `<symbol>` by whole symbol name with case ignored, then by last dot-part (R-01 step 2's rule).
    For each match it prints a header and the direct edges into it over five relations (`calls`,
    `references`, `implements`, `extends`, `imports`), or out of it with `--out`.
12. **A hit** is `<n>. <relation> <- <name> · <kind>  <path>:L<site>  <confidence>` (`->` with `--out`),
    followed by the calling line: the text of line `Site`, the first of the edge's `lines`, read from the
    file as it is now, trimmed and cut at 140 characters. When the file no longer has that line, the hit is
    printed without it. Hits are sorted by path and line. At most 50 (proposed); a last line
    `(N more not shown; narrow with --in)` counts the rest. `--in <path>` keeps hits under that path, by
    component, and must stay inside the repository. `--json` prints the same hits in the same order as `Hit`
    objects, with `depth` 1.
13. **The verb's rules** are 043's: client from the working folder, F-07 step 3, exit 1 with one line on
    stderr outside a client, without a graph or with `graph: off`; it never builds and never writes. An
    unknown symbol exits 1 and names it. Several matches print under separate headers.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-03](../prd/lobe-graph.md), all of it; [G-02](../prd/lobe-graph.md), step 3 and the rows "edge line", "ext line" and "`relation`" of the parts table; [G-09](../prd/lobe-graph.md), "How to build it" (the `Hit` shape; "`callers` uses this walk") |
| Requirements | [R-03](../prd/lobe-recall.md), steps 2, 5 and 6, "How to build it" (edge cases); [R-01](../prd/lobe-recall.md), step 2 (how a symbol name resolves); [core page](../prd/vertical-lobe.md) 5.1 |
| Source to port: rules 3, 8, 9, families, Go | Trail-code [`src/graph/resolve.ts` L395](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L395), [L405](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L405), [L297](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L297), [L55](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L55), [L411](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L411) |
| Not copied: four guesses | Trail-code [L478](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L478), [L447](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L447), [L237](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L237), [L230](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L230). Also L724 (Go imports include tests), L173 (methods keyed by bare class name), L459 (the 3-level limit, kept) |
| Source to port: rules 4 and 10, scores | Graphify [`graphify/extract.py` L12125](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L12125), [L12232](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L12232), [L12248](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L12248); [`graphify/paths.py` L441](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/paths.py#L441) (test candidates removed), and L261 to L284 of the same file (the test-file patterns) |
| Not copied: a tie-break | Graphify [`graphify/paths.py` L362](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/paths.py#L362): same folder, then longest shared prefix, which is a guess |
| Source to port: callers | Trail-code [`src/graph/traverse-cli.ts` L238](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/traverse-cli.ts#L238) (the call-site line). Not copied: [`src/graph/traverse.ts` L130](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/traverse.ts#L130), which collects every edge with no cap |
| Existing tentaqles code | `internal/lobe/graph/resolve/resolve.go` (043, rule 1), `internal/lobe/graph/build.go`, `schema.go` and `check.go` (044), `internal/lobe/query` (043), `internal/lobe/cli`, `docs/reference/commands.md` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the fields 043 kept on a
   raw reference (is the receiver text there, the import names, the enclosing owner); whether the Go
   receiver capture worked; the rule names in `schema.go`; how 043's `render.go` cuts output. The owner
   approves changes before code.
2. **The rule table**: `resolve.go` with the lookup interface, the in-memory tables, rules 2 to 11, kinds,
   duplicates, counters, and `families.go`. One fixture per rule. Proves it: `TestLobeResolveRuleTable`,
   `TestLobeResolveAmbiguousDropped`, `TestLobeResolveNoCrossFamily`, `TestLobeResolveEveryEdgeHasRule`.
3. **`testpath.go`** (side by side with task 2). Proves it: a table test over Graphify's patterns, with a
   production folder named `spec` counted as tests.
4. **Import resolvers**, `imports.go`, side by side, one agent per group: go (with `go.mod`); python;
   typescript and tsx (with JavaScript); java and kotlin; c_sharp; c and cpp; rust; ruby and php. Each has a
   fixture with one import that resolves and one that becomes an `ext` line.
5. **Into the build**: `build.go` writes edge and `ext` lines and the counters. Proves it:
   `TestLobeGraphRecursiveCallNoEdge`, `TestLobeGraphDeterministic`, `TestLobeGraphSoundFixtures` (still 0
   violations with real edges).
6. **`tentaqles lobe graph callers`**: `internal/lobe/query/callers.go`, the `Hit` type and the one-step
   walk in `internal/lobe/graph`. Proves it: `TestLobeQueryCaps`, `TestLobeQueryToolCount` (2),
   `TestLobeQueryNeverBuilds`, `TestLobeQueryOtherClient`, the new rows in `TestLobeCommandTable` and
   `TestLobeCommandRefusesAnotherLogin`.
7. **Docs**: the `callers` action under `## tentaqles lobe graph` in `docs/reference/commands.md`, with the
   rule names and what each label means; a line in `CHANGELOG.md`. The plugin does not change.
8. **Tester run** of the Evaluation below.

## Tests that stay in the repository

- `TestLobeResolveAmbiguousDropped`: `a.py` and `b.py` define `parse` and `c.py` calls it: 0 edges,
  `ambiguous` is 1.
- `TestLobeResolveNoCrossFamily`: a Go file calls `make()` and a TypeScript file defines `make`: 0 edges.
- `TestLobeResolveEveryEdgeHasRule`: 100% of edges name a rule from the table, and both ends are nodes.
- `TestLobeResolveRuleTable` (new here): one fixture per rule 1 to 11 gives the label, score and `rule` of
  the table, 11 of 11; same-named methods on one owner give 0 edges and `ambiguous` 1 until 047.
- `TestLobeGraphRecursiveCallNoEdge` (G-02): a function that calls itself and one other function gives 1
  `calls` edge, to the other function, and 0 from the symbol to itself.
- `TestLobeQueryCaps` (R-03): a symbol with 500 callers prints 50 hits and a last line that counts 450.
- Widened: `TestLobeQueryToolCount` (exactly 2 tools), `TestLobeQueryNeverBuilds` and
  `TestLobeQueryOtherClient` (now also `callers`), `TestLobeGraphSoundFixtures`,
  `TestLobeGraphDeterministic`.
- House rules kept green: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour` (the resolve package
  has no `init`; its tables are built on first use), `TestLobeLeak_AllAttacksZeroLeaks`,
  `TestDocsReferenceEveryCommand`, and every test under "Must not trade off" in `CLAUDE.md`, unedited. No
  hook changes.

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

cd "$W/clients/acme" && git init -q shop && cd shop
printf 'def parse():\n    return 1\n' > a.py; cp a.py b.py
printf 'def main():\n    return parse()\n' > c.py
printf 'from a import parse\n\ndef go():\n    return parse()\n' > e.py
printf 'def only_here():\n    return 4\n' > f.py
printf 'def use():\n    return only_here()\n' > g.py
printf 'def obj_call(o):\n    return o.parse()\n' > x.py
tentaqles lobe graph build || echo "SETUP FAIL build shop"

cd "$W/clients/acme/app" && mkdir -p lib app && printf 'module example.com/fix\n' > go.mod
printf 'package lib\n\nfunc ParseSpan(s string) (int, int) { return 0, 0 }\n' > lib/span.go
printf 'package lib\n\nimport "testing"\n\nfunc TestParseSpan(t *testing.T) { ParseSpan("L1") }\n' > lib/span_test.go
printf 'package app\n\nimport "example.com/fix/lib"\n\nfunc LoadA() { lib.ParseSpan("L1") }\n' > app/a.go
tentaqles lobe graph build || echo "SETUP FAIL build app"
GA="$TQ_HOME/identities/acme/lobe/graph"
```

If a line starts with `SETUP FAIL`, stop and report FAIL. `$GA` now holds two folders. "Shop's graph" is the
`graph.jsonl` that holds `c.py`; "app's graph" is the one in `$GA/r_a172cedc`.

1. In shop's graph no line contains `"source":"c.py#main","relation":"calls"` or
   `"source":"x.py#obj_call","relation":"calls"`.
2. One line starts with `{"t":"edge","source":"e.py#go","relation":"calls","target":"a.py#parse",` followed
   directly by `"confidence":"extracted","score":1,"rule":"import-symbol","lines":[4]`.
3. One line contains `"source":"g.py#use","relation":"calls","target":"f.py#only_here",` followed directly
   by `"confidence":"inferred","score":0.85,"rule":"unique-name"`.
4. Line 1 contains `"dropped":{"ambiguous":1,"untyped":1,"unknown":0}`.
5. In `shop`: `tentaqles lobe graph callers parse; echo "exit=$?"` prints `exit=0`. The output has two
   headers, one with `a.py` and one with `b.py`. It holds one hit, with `go` and `e.py:L4`. It holds neither
   `main` nor `obj_call`.
6. In `$W/clients/acme/app`: `tentaqles lobe graph callers ParseSpan; echo "exit=$?"` prints `exit=0` and
   exactly 2 hits, `LoadA` (`app/a.go`) then `TestParseSpan` (`lib/span_test.go`). The `LoadA` line ends with
   `extracted` and is followed by a line that contains `lib.ParseSpan("L1")`. The `TestParseSpan` line ends
   with `inferred`.
7. `tentaqles lobe graph callers ParseSpan --json` prints one JSON line that holds
   `"confidence":"extracted"`, `"confidence":"inferred"` and `"site":5`.
8. `tentaqles lobe graph callers LoadA --out` prints exactly 1 hit; it holds `ParseSpan` and `lib/span.go`.
9. `tentaqles lobe graph callers ParseSpan --in lib` prints exactly 1 hit, `TestParseSpan`.
   `tentaqles lobe graph callers ParseSpan --in ../shop; echo "exit=$?"` prints `exit=1`.
10. `tentaqles lobe graph callers NoSuchName; echo "exit=$?"` prints `exit=1`; stderr contains `NoSuchName`.
11. App's graph has a line that starts with
    `{"t":"edge","source":"app/a.go","relation":"imports","target":"lib/span.go",` and the line
    `{"t":"ext","source":"lib/span_test.go","relation":"imports","name":"testing"}`.
12. `tentaqles lobe graph check --all --json --strict; echo "exit=$?"` prints two JSON lines, both with
    `"ok":true`, then `exit=0`.
13. `(cd "$W/clients/globex/app" && tentaqles lobe graph callers ParseSpan; echo "exit=$?")` prints `exit=1`
    and no `span.go`.
14. In acme's `app`: `CLAUDECODE=1 CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles lobe graph
    callers ParseSpan; echo "exit=$?"` prints `exit=1` and no `span.go`. The same with
    `CODEX_HOME="$TQ_HOME/identities/globex/codex"` in place of `CLAUDE_CONFIG_DIR`: `exit=1`.

**Person checks**: none.

Scoring: PASS needs 14 of 14. "Could not run" is a FAIL.

## Done when

- [ ] every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin did not change
- [ ] the tester brief passes, 14 of 14
- [ ] the house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeQueryOtherClient`, `TestLobeLeak_AllAttacksZeroLeaks`, `TestLobeGraphSoundFixtures`
- [ ] `docs/reference/commands.md` documents `callers` and the labels; `CHANGELOG.md` has its line
- [ ] the owner has seen the callers list of the Result section

## Risks and unknowns

- **Most method calls are dropped.** With no receiver types, calls on objects fall under rule 8. Trail-code's
  comment (`resolve.ts` L306) says a name fallback cut precision from 73% to 37%; that measurement was not
  checked. 057 measures ours.
- **Rule 9 can be fooled** by a local name that shadows a function elsewhere. 057 decides whether Python
  joins TypeScript outside it (`extract.py` L12221).
- **Rule 10 rests on a naming habit** (test-file names), at a proposed score of 0.75. Its edges carry
  `unique-nontest`, so a reader can discount them.
- **Rule 4 misses re-exports**, and path aliases stay `ext`.
- **Overloads wait for 047.** Until then a call to one of several same-named methods on one owner is counted
  `ambiguous`. G-03 depends on G-04, yet this feature comes first; step 4 moved with the counts.
- **The confidence on a hit line** comes from the index's wording of this feature. R-03 step 2 lists only
  relation, name, pointer and calling line.
- **The five relations.** R-03 step 2 says callers walks "R-01's five relations" in both directions; R-01
  step 2 uses four for incoming questions and adds `imports` only for outgoing ones. This file follows
  R-03. `imports` edges join file nodes, so they show only when the symbol asked for is a file.
- **A base type that is not in the repository** becomes an `ext` line and is not counted `unknown`. That is
  this file's reading of G-02's definition of an `ext` line.
- **What would change this file:** another shape of raw references from 043, or an owner's call on the
  three readings above.
