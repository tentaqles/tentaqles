# 047: Signatures and argument counts

**Status** not started · **Stage** Graph · **Entries** G-04 (all but what E-05 does with it), G-03 (step 4,
overloads) · **Needs** 045 · **Feeds** 051, 057

## Result

Every function and method in the graph now carries how many arguments it takes: the number of declared
parameters, the smallest number a call may pass, and whether it accepts any number. Every call edge carries
how many arguments each call passes. The header of a definition is stored as one line, however many lines it
spans in the file. A call to one of several overloads resolves when exactly one of them fits.

```bash
cd "$W/clients/acme/shop"       # sig.py of the Evaluation
tentaqles lobe graph build
tentaqles lobe graph outline sig.py
grep '"id":"sig.py#load"' "$TQ_HOME"/identities/acme/lobe/graph/r_*/graph.jsonl
```

```
load · function  L1-L3  def load(path, limit=10, *rest)
any_kw · function  L5-L6  def any_kw(a, **kw)
caller · function  L8-L10  def caller(xs)
```

The node line of `load` holds `"params":{"n":2,"min":1,"variadic":true,"check":true}`.

## Scope

**In**

- The `params` capture on a definition's parameter list and the `args` capture on a call's arguments, in the
  `.tq.scm` files of the first signature set: Python, TypeScript with TSX, Go, Java and C# (decided), and
  JavaScript files, which get counts with `check` false.
- `params.go`, `params_table.go` and `sig.go` in `internal/lobe/graph/extract`; `Fits` in
  `internal/lobe/graph/types.go`.
- `params` on node lines, `argc` on `calls` edge lines, the new `sig` for all twelve first languages.
- G-03 step 4: among same-named methods on one owner, exactly one whose counts fit the call gives an edge.
- A higher extractor version, so every parse-cache file of an older build is ignored.

**Out**

- The note after an edit that shows a call with an impossible number of arguments
  ([051](051-argument-check.md), E-05). E-05 has no counting code of its own: it calls `CountArgs` and
  `Fits` from here.
- Switching a language's notes on, which waits for M-05's two numbers on replayed edits (051, with
  [041](041-enforcement-accuracy.md)'s harness).
- `params` for Rust, C, C++, Ruby, PHP and Kotlin. They get `sig` only, which means "never checked". The
  requirements name no later feature for them.
- Checking a call that resolved to a class (a constructor). Not in this version (G-04).
- Parameter names, types and default values as fields. Only the counts and the header text are stored.
- The comparison of our edges with Trail-code's ([057](057-graph-accuracy.md)).

## What it builds on

- **[043](043-graph-of-one-repository.md)**: the adapter (`parse.Tree`, `Captures`), the registry and the
  `.tq.scm` files, `internal/lobe/graph/extract`, the extractor version, the parse cache, node lines with
  `sig` as the first line, `tentaqles lobe graph outline`, `TestLobeGraphQueriesCompile` (a capture nothing
  reads fails it), `TestLobeGraphExtractFixtures`.
- **[045](045-resolution-and-callers.md)**: `resolve.go` with rules 2 to 11, where same-named methods on one
  owner are still `ambiguous`; edge lines with `lines`; `TestLobeResolveRuleTable`.
- **[044](044-graph-soundness.md)**: the check inside every build, which reads only the keys its rules name,
  so `params` and `argc` pass it unchanged.
- **[046](046-find-and-map.md)**: the index, whose body terms include `sig`; it is rebuilt by every build.
- **[042](042-parser-runtime.md)**: the pinned grammar revisions. Node type names differ between grammar
  versions, so the tables here are tied to those revisions.
- **[003](003-shell-failure-claude-code.md)**: `scrub.Text`, because a default value can hold a secret.

## What it leaves for later features

- `extract.Params{N, Min int; Variadic, Check bool}`,
  `extract.CountParams(lang string, list Tree) (Params, bool)` and
  `extract.CountArgs(lang string, list Tree) int`, which returns -1 when a call cannot be counted (051 calls
  `CountArgs` on the `args` capture of the one-file parse that [049](049-blast-radius.md) makes).
- `graph.Fits(p Params, argc int) bool`, the single copy of the fit rule (051).
- On a node line: `sig`, then `params` with the four keys `n`, `min`, `variadic`, `check` in that order. A
  node without `params` is never judged.
- On a `calls` edge line from a language this feature counts: `argc`, a list parallel to `lines`.
- Extractor version raised by one (048 writes it into `files.json`).

## How it works

1. **Captures** (G-04 step 1). Each `.tq.scm` of the first signature set captures `params` on a definition's
   parameter list and `args` on a call's argument list. The other seven languages get neither capture,
   because a capture nothing reads fails `TestLobeGraphQueriesCompile`.
2. **Classify** (step 2). A per-language table maps a parameter's node type to one class: receiver (`self`,
   `cls`, a Go receiver, a TypeScript `this` parameter), required, optional (has a default or `?`), variadic
   (`*args`, `...rest`, `...T`, `params`), keyword collector (`**kwargs`).
3. **Count** (step 3). `n` counts required plus optional parameters, `min` the required ones; `variadic`
   says whether a variadic parameter exists. Receivers and collectors are not counted.
4. **`check`** (step 4) is false when the counts must not be used to judge a call: a keyword collector, a
   Python decorator other than `@staticmethod` or `@classmethod`, TypeScript overload signatures, any
   JavaScript file.
5. **Per language** (G-04 "How to build it").
   - Python: the first parameter of a method is the receiver unless the method is a `@staticmethod`. The `/`
     and `*` markers are not parameters. Keyword arguments at a call count toward `argc`.
   - TypeScript: `x?: T` and `x = v` are optional; several body-less declarations of one name are overload
     signatures.
   - Go: no defaults, so `min` equals `n`.
   - Java: count `formal_parameter` and `spread_parameter` children; a spread one sets `variadic`.
   - C#: `params` sets `variadic`; an extension method called as a member needs one argument fewer.
6. **`sig`** (step 5) becomes the text from the start of the definition to the start of its body, with white
   space collapsed and a trailing `{`, `:`, `=` or `=>` removed, cut at 300 characters (proposed) and passed
   through `scrub.Text`. This holds for all twelve first languages, with or without `params`.
7. **`argc`** (step 6). Each raw call records its argument count, or -1 when it cannot be counted: a spread
   or unpacked argument (`*xs`, `**kw`, `...xs`), or in Go a single argument that is itself a call, because
   `f(g())` may pass several values. On the merged edge, `argc` is a list parallel to `lines`, so the same
   at most 20 (proposed) calls are kept. `argc` is written only on a `calls` edge whose source file is in a
   language this feature counts.
8. **The fit rule** (step 7), the only one: a call fits when `min <= argc` and (`argc <= n` or `variadic`).
9. **Overloads** (G-03 step 4). Where a rule of 045 looks for "exactly one" method and finds several of the
   same name on one owner, the call's `argc` is tested against each with `Fits`. Exactly one fit gives the
   edge, with that rule's label. No fit, several fits, a candidate without `params` or with `check` false,
   or an `argc` of -1 gives no edge, and the call is counted `ambiguous`. Nothing is guessed.
10. **Where we differ.** Trail-code counts the variadic parameter and accepts `argc >= arity - 1`; leaving it
    out of `n` and `min` gives the same answer with one rule. Its query-file tier keeps only the first
    physical line of a header, which cuts multi-line headers. When no overload fits, Trail-code keeps every
    candidate and Graphify binds to the first declared one; we keep none.
11. **The cache.** The extractor version rises by one. The next build ignores every older parse-cache file
    and parses each file again.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-04](../prd/lobe-graph.md), all of it; [G-03](../prd/lobe-graph.md), step 4 and `TestLobeResolveOverloadNoFit`; [G-02](../prd/lobe-graph.md), the rows "node line" and "edge line" (where `params` and `argc` sit) and step 5 (the extractor version); [S-05](../prd/lobe-sense.md) for `scrub.Text` |
| What reads this later | [E-05](../prd/lobe-enforce.md), to see what it needs; build none of it |
| Source to port: header text, Java counting | Trail-code [`src/graph/extract.ts` L563](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L563) (cleaned at L2627), [L1677](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L1677) (variadic at L1678), [L713](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L713) |
| Not copied | Trail-code [`src/graph/resolve.ts` L429](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L429) (a stale comment) and [L447](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L447) (no fit keeps every candidate); also `resolve.ts` L445 and `src/graph/generic.ts` L305 |
| Source to port: C# rules, one fit or none | Graphify [`graphify/extractors/engine.py` L226](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L226), [L242](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L242), [L289](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L289) |
| Not copied | Graphify [`engine.py` L10609](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L10609): with no unique fit, the call binds to the first declared overload |
| Existing tentaqles code | `internal/lobe/graph/extract` and `lang/queries/*.tq.scm` (043), `internal/lobe/graph/resolve/resolve.go` (045), `internal/lobe/graph/types.go`, `internal/lobe/graph/schema.go`, `docs/reference/commands.md` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the grammar revisions in
   `go.mod` against 042's result; the node type names of parameter and argument lists in those revisions;
   how 043 stores a raw call (where the count goes); how 045 reports a no-fit; the extractor version. The
   owner approves changes before code.
2. **`Fits`** in `types.go`. Proves it: `TestLobeParamsFits`.
3. **Counting, one agent per language, side by side**: the `params` and `args` captures in the `.tq.scm`
   file, the rows of `params_table.go`, and 8 headers for `TestLobeParamsTable`. Languages: Python;
   TypeScript with TSX and JavaScript; Go; Java; C#. `params.go` (`CountParams`, `CountArgs`) is written
   first by the Python agent and shared. Proves it: `TestLobeParamsTable`, `TestLobeArgcNotCountable`,
   `TestLobeGraphQueriesCompile`.
4. **`sig.go`** for all twelve languages (side by side with task 3). Proves it: `TestLobeSigMultiLine`,
   `TestLobeParamsAbsentWhenUnsupported`, and the regenerated goldens of `TestLobeGraphExtractFixtures`.
5. **Into extraction**: `params` on nodes, the count on raw calls, the extractor version raised. Proves it:
   `TestLobeArgcSameInHookAndBuild`, `TestLobeGraphParseCacheVersion`.
6. **Into resolution**: step 4 in `resolve.go`, `argc` on edge lines in the key order of G-02. Proves it:
   `TestLobeResolveOverloadNoFit`, `TestLobeResolveRuleTable` (the overload row now gives an edge),
   `TestLobeGraphSoundFixtures`, `TestLobeGraphDeterministic`.
7. **Docs**: what `params`, `check`, `argc` and -1 mean, under `## tentaqles lobe graph` in
   `docs/reference/commands.md`; a line in `CHANGELOG.md`. The plugin does not change.
8. **Tester run** of the Evaluation below.

## Tests that stay in the repository

- `TestLobeParamsTable`: 8 headers for each of 5 languages; 40 of 40 give the expected `n`, `min`,
  `variadic`, `check`.
- `TestLobeParamsFits`: 12 cases of the fit rule, 12 of 12.
- `TestLobeSigMultiLine`: a header spread over 3 lines is stored as one line.
- `TestLobeArgcNotCountable`: `f(*xs)`, `f(...xs)` and Go `f(g())` each store -1: 3 of 3.
- `TestLobeArgcSameInHookAndBuild`: for 10 calls, `CountArgs` on a one-file parse equals the `argc` the
  build stored for the same text: 10 of 10.
- `TestLobeParamsAbsentWhenUnsupported`: a Ruby method has `sig` and no `params`.
- `TestLobeResolveOverloadNoFit` (G-03): a Java class with `f(int)` and `f(int,int)` and a call `f(1,2,3)`:
  0 edges. Widened here: the call `f(1,2)` gives exactly 1 edge, to the second `f`.
- Widened: `TestLobeGraphExtractFixtures` (goldens with the new `sig` and `params`),
  `TestLobeGraphQueriesCompile` (the two new capture names are read), `TestLobeResolveRuleTable`,
  `TestLobeGraphParseCacheVersion`.
- House rules kept green: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour` (the tables are
  built on first use), `TestLobeGraphSoundFixtures`, `TestLobeLeak_AllAttacksZeroLeaks`, and every test
  under "Must not trade off" in `CLAUDE.md`, unedited. A scrubbed `sig` keeps "no secret on disk": add one
  row to the extraction fixtures with a default value shaped like a key, stored as `[redacted:...]`. No hook
  changes.

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
printf 'def load(path,\n         limit=10, *rest):\n    return path\n\n' > sig.py
printf 'def any_kw(a, **kw):\n    return a\n\n' >> sig.py
printf 'def caller(xs):\n    load("a", 1)\n    load(*xs)\n' >> sig.py
printf 'class O {\n    void f(int a) {}\n    void f(int a, int b) {}\n' > O.java
printf '    void g() {\n        f(1, 2);\n        f(1, 2, 3);\n    }\n}\n' >> O.java
printf 'def hi(a)\n  a\nend\n' > r.rb
tentaqles lobe graph build || echo "SETUP FAIL build"
G="$(ls -d "$TQ_HOME"/identities/acme/lobe/graph/r_*)"
```

If a line starts with `SETUP FAIL`, stop and report FAIL. `sig.py` has ten lines and `O.java` eight. The
checks read `$G/graph.jsonl` and run in `shop`.

1. The line with `"id":"sig.py#load"` contains `"span":[1,3]` and `"sig":"def load(path, limit=10, *rest)"`.
2. The same line contains `"params":{"n":2,"min":1,"variadic":true,"check":true}`.
3. The line with `"id":"sig.py#any_kw"` contains `"params":{"n":1,"min":1,"variadic":false,"check":false}`.
4. Exactly one line contains `"source":"sig.py#caller","relation":"calls","target":"sig.py#load"`, and it
   ends with `"lines":[9,10],"argc":[2,-1]}`.
5. The line with `"id":"O.java#O.f~2"` contains `"params":{"n":2,"min":2,"variadic":false,"check":true}`.
6. Exactly one line contains `"source":"O.java#O.g","relation":"calls"`. It contains
   `"target":"O.java#O.f~2"` and ends with `"lines":[5],"argc":[2]}`: the call with three arguments fits
   neither `f` and gave no edge.
7. Line 1 contains `"dropped":{"ambiguous":1,`: the one ambiguous call is `f(1, 2, 3)`.
8. The line with `"id":"r.rb#hi"` contains `"sig":"def hi(a)"` and does not contain `"params"`.
9. `tentaqles lobe graph outline sig.py` prints 3 lines; line 1 holds `L1-L3` and
   `def load(path, limit=10, *rest)`.
10. Record the SHA-256 of `graph.jsonl`, run `tentaqles lobe graph build` again and compare: the same hash.
11. `tentaqles lobe graph check --json --strict; echo "exit=$?"` prints `"ok":true` and `exit=0`.

**Person checks**: none.

Scoring: PASS needs 11 of 11. "Could not run" is a FAIL.

## Done when

- [ ] every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin did not change
- [ ] the tester brief passes, 11 of 11
- [ ] the house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeGraphSoundFixtures`, `TestLobeLeak_AllAttacksZeroLeaks`, the scrubbed-`sig` fixture row
- [ ] `docs/reference/commands.md` explains `params`, `check` and `argc`; `CHANGELOG.md` has its line
- [ ] the owner has seen the outline and the node line of the Result section

## Risks and unknowns

- **The counting tables are ours and unproven** for Python, TypeScript and Go. The harm of a wrong count is
  bounded: in resolution it can only lose or misplace an edge among same-named overloads on one owner, and
  051's note never denies. How often the check is wrong on real code is 051's and M-05's number.
- **C# rests on Graphify's rules alone**, the only source that counts for it. `TestLobeParamsTable` is the
  one check on them until M-05 reports.
- **Dynamic languages change signatures at run time** (decorators, patched methods, `functools.partial`).
  `check` covers only what we can see.
- **Node type names differ between grammar versions.** The tables are pinned to the revisions 042 fixed; a
  grammar bump must rerun `TestLobeParamsTable`.
- **Captures only in five languages.** G-04 step 1 says "each first-language `.tq.scm`", step 8 limits
  `params` to the first signature set. This file follows step 8, so that no capture goes unread. `sig`
  needs no capture: the body's start comes from the definition node.
- **The order problem this file absorbs.** G-03 depends on G-04, but resolution (045) is built first. Step
  4 of G-03 and its test are therefore here.
- **The 300-character cut and the 20 kept calls are proposed.**
- **What would change this file:** a grammar revision other than 042's, or an owner's call to count more
  languages now.
