# 044: Graph soundness check

**Status** not started · **Stage** Graph · **Entries** G-12 (all but the two rules of G-07 and G-10),
F-07 (the finding `lobe-graph-unsound`) · **Needs** 043 · **Feeds** 045, 048, 052, 053, 054, 056, 057

## Result

`tentaqles lobe graph check` reads a built graph and reports whether its parts agree with each other: every
edge starts and ends at a real node, every span is well formed, every name comes from the allowed lists. The
same check runs inside every build, before the old graph is replaced. A graph that fails it twice is not
written, and `tentaqles doctor` says so.

```bash
cd "$W/clients/acme/shop"
tentaqles lobe graph build
tentaqles lobe graph check --json --strict; echo "exit=$?"     # "ok":true ... exit=0
tentaqles lobe graph check --file "$W/bad.jsonl" --json --strict; echo "exit=$?"   # a problem list, exit=1
```

## Scope

**In**

- `schema.go`: one table of allowed kinds, relations, confidence values and rule names, shared by the
  builder and the check. It ships with the names of G-02 and G-03.
- `Check(g *Graph) Report` with the node rules, the edge rules, the count of self-loop calls, the added
  rules of G-12 step 4 that need no later feature, and the reported counts of step 5.
- `tentaqles lobe graph check [--all] [--file <path>] [--json] [--strict]` and its exit codes 0, 1 and 2.
- The check inside every build: drop the files a problem names, check once more, keep the old graph if it
  still fails, write `check.json`; a build that passes deletes `check.json`.
- The doctor finding `lobe-graph-unsound`.
- `TestLobeGraphSoundFixtures` over every language fixture and over tentaqles itself, in CI.

**Out**

- The rule "a note has exactly one `explains` edge" ([054](054-why-comments.md)).
- The rule on `links.json` and `CheckLinks`; `--all` reading `links.json`
  ([056](056-many-repositories.md)). Here `--all` checks every graph folder of the client and no links.
- The kinds, relations and rule names of SQL ([052](052-sql-in-the-graph.md)), documents and configuration
  ([053](053-docs-and-config.md)), why-comments (054) and PDFs ([061](061-pdfs-and-images.md)): each adds
  its own rows to `schema.go`.
- "The dirty markers stay" after a failed build ([048](048-graph-stays-current.md)); no marker exists yet.
- Whether the graph is right, not merely consistent ([057](057-graph-accuracy.md)).
- A CI threshold on orphans or on the `dropped` counts. G-12 leaves it open; nothing fails on them here.

## What it builds on

- **[043](043-graph-of-one-repository.md)**: `internal/lobe/graph` (`types.go`, `store.go`, `build.go`,
  `GraphDir`), `graph.jsonl` with its meta, node and `contains` edge lines, the `errors` key of the meta
  line, the language fixtures of `TestLobeGraphExtractFixtures`, `tentaqles lobe graph build`, and the
  `## tentaqles lobe graph` section of the command reference.
- **[005](005-status-and-capture-switch.md)**: `internal/lobe/health`, whose findings
  `internal/cmd/doctor.go` appends, with `TestLobeDoctorCodes` and `TestLobeHealthNeverReachesTheGuard`;
  `internal/lobe/cli`; the scope and login rule of F-07 step 3.
- **[002](002-sandbox.md)**: the sandbox.

Until [045](045-resolution-and-callers.md) a built graph holds only `contains` edges, so every symbol counts
as an orphan. Orphans are reported and never fail the check.

## What it leaves for later features

- `internal/lobe/graph/schema.go`: the allowed lists. 045 writes edges whose `rule` must be in it; 052, 053,
  054 and 061 add rows.
- `internal/lobe/graph/check.go`: `Check(g *Graph) Report` and a list of extra rule functions, to which 054
  and 056 (`CheckLinks`) each add one.
- `Report` as JSON: `{"graph","nodes","edges","ext","ok","violations","problems","self_loop_calls","calls",
  "orphans","by_kind","by_relation","by_confidence"}` (057 reads it for both graphs).
- `graph/<repo-key>/check.json` (048 keeps markers beside it; `tentaqles doctor` reads it).
- The build's rule "check before replace" (048's detached build calls the same builder).
- The command `tentaqles lobe graph check`.

## How it works

1. **Allowed names.** Kinds: `file` and G-02's ten (`function`, `method`, `class`, `interface`, `type`,
   `struct`, `enum`, `module`, `constant`, `variable`). Relations: `contains`, `calls`, `imports`, `extends`,
   `implements`, `references`. Confidence: `extracted`, `inferred`. Rule names: G-03's nine (`contains`,
   `import`, `same-file`, `import-symbol`, `self-member`, `import-member`, `owner-member`, `unique-name`,
   `unique-nontest`). The tables are plain literals: no `init`, no package-level call (core page 5.1).
2. **Node rules** (G-12 step 1): unique id, non-empty name, kind in the list, a span of the form
   `[first, last]` with first not after last.
3. **Edge rules** (step 2): relation and confidence in their lists; source and target are both nodes. No
   relation is excused: an outside name is an `ext` line, never an edge. An `ext` line's source is a node.
4. **Counted, not failed** (step 3): a `calls` edge from a symbol to itself. A built graph holds none,
   because G-02 drops a call on a definition's own name; the count stays in the report so that a graph file
   from elsewhere is read correctly.
5. **Added rules** (step 4): the meta line's `nodes` and `edges` equal the lines present; a symbol's span
   lies inside its file node's span; each non-file node's `path` is the path of exactly one file node; every
   edge names a `rule` from the list; no two edges share source, relation and target; lines are in G-02's
   order, compared byte by byte.
6. **Problem lines** are fixed strings, sorted, so `problems[0]` is stable. G-12 gives four:
   `dangling <relation> target: <source> -> <target>`, `dangling source: <source>`, `bad span [5]: <id>`
   and `inverted span [9,5]: <id>`. Every other rule gets one such form, listed in the command reference.
   At most 15 are listed; `violations` counts them all.
7. **Reported** (step 5): `nodes`, `edges`, `ext`, counts `by_kind`, `by_relation` and `by_confidence`,
   `calls`, `self_loop_calls`, and `orphans`: non-file nodes with no edge other than `contains`. `graph` is
   the path of the file that was checked. `ok` is true when `violations` is 0.
8. **Lenient reading.** The check reads only the keys its rules name; a line may lack any other key.
9. **The command.** `tentaqles lobe graph check` checks the graph of the working folder's repository. `--all`
   checks every graph folder of the client, `wt-` subfolders included, and prints one report per graph.
   `--file <path>` checks one graph file and needs no client, no store and no setting. `--json` prints each
   report as one compact line. It is not person-only and it never builds.
10. **Exit codes** (step 6): 0 when sound, or when `--strict` is absent; 1 with `--strict` and a problem; 2
    when there is no graph to read (a missing file, a repository with no graph, `graph: off`, a folder that
    is no client's). A refusal under F-07 step 3 exits 1, like every other lobe command.
11. **Inside a build.** The builder of 043 calls `Check` on the graph it is about to write. When it fails,
    the builder drops the nodes and edges of each file a problem names, records each dropped file in the
    meta line's `errors`, and checks once more. If it still fails, the previous `graph.jsonl` stays byte for
    byte, the problems go to `graph/<repo-key>/check.json`, and `tentaqles lobe graph build` prints one line
    and exits 1. A build that passes deletes `check.json`.
12. **Doctor.** `internal/lobe/health` looks for `check.json` in the folders under the client's `graph/`
    (it reads the store, never a client folder) and reports the warning `lobe-graph-unsound` with the
    client's name and the fix line `tentaqles lobe graph build`.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-12](../prd/lobe-graph.md), all of it; [G-02](../prd/lobe-graph.md), the parts table (line shapes and order); [G-03](../prd/lobe-graph.md), the rule table (the rule names); [F-07](../prd/lobe-foundation.md), step 5 (the finding) and "How to build it" (`internal/lobe/health`); [core page](../prd/vertical-lobe.md) 5.1 |
| Source to port | Graft 0.17.0 (MIT), `scripts/graph-quality.mjs`: L43 to L48 (node rules), L55 to L61 (edge rules), L63 (self-loop count), L69 to L72 (orphans), L98 (15 problems), L117 and L28 (exit codes), L19 to L21 (the lists). `src/graph/invariants.ts` L16 to L18, L21 to L22, L40 to L41. A local copy on the owner's machine at `sources/source-code`, beside this repository (core page section 7) |
| The same script at a pinned commit | Trail-code [`scripts/graph-quality.mjs` L19](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/scripts/graph-quality.mjs#L19) and [`src/graph/invariants.ts` L26](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/invariants.ts#L26), from [M-07](../prd/lobe-benchmarks.md)'s Source table |
| Existing tentaqles code | `internal/lobe/graph/build.go`, `store.go` and `types.go` (043); `internal/lobe/health` and `internal/cmd/doctor.go` (text output omits a finding's code, only `--json` has it); `internal/lobe/cli`; `docs/reference/commands.md` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the real key order and
   names 043 wrote to `graph.jsonl`; the shape of `errors` on the meta line; how `internal/lobe/health`
   takes a new finding; whether Graft's local copy is reachable for the builder (if not, read the script at
   Trail-code's pinned commit). The owner approves changes before code.
2. **`schema.go`** with the four lists; make the builder of 043 take kinds, relations and rule names from
   it. Proves it: the fixtures of 043 still give the same bytes.
3. **`check.go`**: `Check`, `Report`, the rules of steps 2 to 7, the fixed problem strings, the list of
   extra rule functions (empty here). Proves it: `TestLobeGraphCheckDanglingCall`,
   `TestLobeGraphCheckNoExcuse`, `TestLobeGraphCheckSpans`, `TestLobeGraphCheckSelfLoop`.
4. **The command** (side by side with task 5): `tentaqles lobe graph check` in `internal/lobe/cli`, with
   `--file`, `--all`, `--json`, `--strict` and the exit codes. Proves it: `TestLobeGraphCheckExitCodes`, and
   the new row in `TestLobeCommandTable`.
5. **The check inside the build** (side by side with task 4): the drop-and-check-again path, `check.json`,
   its deletion by a passing build. Proves it: `TestLobeGraphBuildKeepsOldGraph`.
6. **The finding** `lobe-graph-unsound` in `internal/lobe/health`. Proves it: `TestLobeDoctorCodes` with the
   new fixture, `TestLobeHealthNeverReachesTheGuard`.
7. **`TestLobeGraphSoundFixtures`** over every fixture repository of 043 and over the tentaqles repository
   itself; it runs in CI with the rest of `go test ./...`.
8. **Docs**: the `check` action, its exit codes and the problem strings under `## tentaqles lobe graph` in
   `docs/reference/commands.md`; the finding in the doctor part of the reference; a line in `CHANGELOG.md`.
   The plugin does not change.
9. **Tester run** of the Evaluation below.

## Tests that stay in the repository

- `TestLobeGraphCheckDanglingCall`: a `calls` edge to a missing id gives 1 problem and exit code 1 with
  `--strict`.
- `TestLobeGraphCheckNoExcuse`: an `imports` edge to `lodash`, which is no node, gives 1 problem; an `ext`
  line gives 0.
- `TestLobeGraphCheckSpans`: spans `[5]`, `[10,5]` and one past the end of its file give 3 problems.
- `TestLobeGraphCheckSelfLoop`: a hand-written graph file with one `calls` edge from a symbol to itself
  gives `ok` true, `self_loop_calls` 1 and exit code 0.
- `TestLobeGraphSoundFixtures`: every fixture repository, one per language, and tentaqles itself give 0
  violations and `self_loop_calls` 0, recursive functions in the fixtures included.
- `TestLobeGraphBuildKeepsOldGraph`: a build forced to emit a dangling edge leaves the old graph
  byte-identical, writes `check.json`, and makes `tentaqles doctor --json` show `lobe-graph-unsound` once.
- `TestLobeGraphCheckExitCodes` (new here): 0 without `--strict` on a broken file, 1 with it, 2 for a
  missing file, for a repository with no graph and for `graph: off`; `--file` works outside any client.
- `TestLobeDoctorCodes` gains the fixture for `lobe-graph-unsound`.
- House rules kept green: `TestLobeGuardNeverImportsLobe` (the tables are literals),
  `TestLobeImportsOnlyTheFour`, `TestLobeHealthNeverReachesTheGuard`, `TestLobeQueryOtherClient`,
  `TestLobeLeak_AllAttacksZeroLeaks`, `TestDocsReferenceEveryCommand`, and every test under "Must not trade
  off" in `CLAUDE.md`, unedited. No hook changes.

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
```

If a line starts with `SETUP FAIL`, stop and report FAIL. Create `$W/good.jsonl` with these 8 lines. "Check
X" means `tentaqles lobe graph check --file X --json --strict` run in `$W`. The edge lines leave out `score`
and `lines`, which no rule reads. The last line is a `calls` edge from `g` to itself, which no build writes;
it is there to show that the check counts such an edge and does not fail on it.

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
2. In a copy, change the target on line 7 to `a.py#gone`. Check it: exit 1, `"violations":1`, and
   `problems[0]` is `dangling calls target: a.py#f -> a.py#gone`. Without `--strict`: the same output and
   exit 0.
3. In a copy, change the span of `a.py#g` to `[9,5]`. Check it: exit 1 and `problems[0]` is
   `inverted span [9,5]: a.py#g`. With `[5,12]` in its place: exit 1 and `"violations":1`.
4. In a copy, append the line `{"t":"ext","source":"a.py","relation":"imports","name":"os"}`. Check it: exit
   0, `"ok":true`, `"ext":1`.
5. Check `missing.jsonl`, a file that does not exist: exit 2.
6. A built graph. Run:
   `cd "$W/clients/acme" && git init -q shop && cd shop`, then
   `printf 'class Loader:\n    def run(self):\n        return 1\n\ndef main():\n    return Loader()\n' > app.py`,
   then `tentaqles lobe graph build`. Now `tentaqles lobe graph check --json --strict; echo "exit=$?"` prints
   one JSON line with `"ok":true`, `"violations":0`, `"nodes":4`, `"self_loop_calls":0`, then `exit=0`.
7. Still in `shop`: `tentaqles lobe graph check --all --json --strict; echo "exit=$?"` prints one JSON line
   and `exit=0`.
8. `(cd "$W/clients/globex/app" && tentaqles lobe graph check; echo "exit=$?")` prints `exit=2`: there is no
   graph, and the check does not build one. `ls "$TQ_HOME/identities/globex/lobe/graph" 2>/dev/null | wc -l`
   prints `0`.
9. `G="$(ls -d "$TQ_HOME"/identities/acme/lobe/graph/r_*)"`, then
   `printf '{"problems":["dangling source: x"]}' > "$G/check.json"`.
   `tentaqles doctor --json | grep -c '"code":"lobe-graph-unsound"'` prints `1`. Run
   `tentaqles lobe graph build` in `shop`: exit 0. `test -e "$G/check.json"; echo $?` prints `1`, and the
   doctor count is `0`.
10. In `shop`: `CLAUDECODE=1 CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles lobe graph check
    --json; echo "exit=$?"` prints `exit=1` and no `"nodes"`.

**Person checks**: none. No step of this feature answers only to a person.

Scoring: PASS needs 10 of 10. "Could not run" is a FAIL.

## Done when

- [ ] every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin did not change
- [ ] the tester brief passes, 10 of 10
- [ ] the house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeHealthNeverReachesTheGuard`, `TestLobeLeak_AllAttacksZeroLeaks`
- [ ] `docs/reference/commands.md` documents `check`, its exit codes and the problem strings; `CHANGELOG.md`
  has its line
- [ ] the owner has seen a sound graph pass and a broken file fail, as under Result

## Risks and unknowns

- **The fixture uses the keys and names of G-02 and G-03 as drafted.** If 043 changed one, the fixture and
  this file change with it (task 1).
- **The confidence list.** Graft allows four confidence values; G-03 writes two. This file allows the two
  our builder can write. If the owner wants Graft's four accepted in a file from elsewhere, step 1 changes.
- **One extractor bug can fail every build.** Dropping the named files keeps the graph alive but hides those
  files. G-12 says the only notice is `tentaqles doctor`; yet `check.json` exists only when the second check
  also fails. This file therefore records each dropped file in the meta line's `errors`. The owner may want
  a louder notice.
- **Exit code 2 with `graph: off`** is this file's reading of "readers act as if there were no graph"
  (G-08 step 8).
- **The check proves consistency, not truth.** A missing call edge passes. 057 measures that.
- **Open in G-12:** whether a rising orphan share or `dropped` count should fail CI. Not built here.
- **Graft's script is a local copy** with no permanent link. If the builder cannot read it, Trail-code's
  copy at the pinned commit is the fallback, and the line numbers must be checked again.
