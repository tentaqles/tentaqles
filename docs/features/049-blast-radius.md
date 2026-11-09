# 049: Blast radius

**Status** not started · **Stage** Graph · **Entries** G-09, R-03 (`impact`), M-06 (the edit case of the
latency run) · **Needs** 048 · **Feeds** 050, 051, 052, 054, 056, 058, 062

## Result

The agent edits a function. Right after the edit it reads a short list of what depends on that function,
two steps back through the code graph, with test files set apart. A person or an agent can ask the same
question for a line range or a diff with `tentaqles lobe graph impact`. Everything stays inside one
repository (decision A-25); crossing repositories is [056](056-many-repositories.md).

From the sandbox of [002](002-sandbox.md), with acme's login exported and the `calc` fixture of the
Evaluation section built:

```bash
tentaqles lobe graph impact --file calc.py --lines 2-2 --json
```

prints one JSON object whose `seeds` holds `add`, whose `impacted` holds `total` at depth 1 and `grand` at
depth 2, and whose `tests` holds `test_calc.py`. After `return a + b` becomes `return b + a` in `calc.py`:

```bash
printf '%s' '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"'"$S"'","tool_name":"Edit",
"tool_input":{"file_path":"'"$S"'/calc.py","old_string":"return a + b","new_string":"return b + a"}}' |
  tr -d '\n' | tentaqles lobe-hook claude post-tool-use
```

prints one JSON object whose `additionalContext` holds these lines under a one-line header:

```
calls <- total (total.py:4)
calls <- grand (grand.py:4)
tests: 1 file (test_calc.py)
```

The same edit sent a second time in session `s1` prints nothing.

## Scope

**In**

- `internal/lobe/graph/blast.go`: changed ranges, seeds, the innermost rule, the backward walk to depth 2, the
  supertype rule for methods, tests set apart (G-09 steps 1 to 5).
- `internal/lobe/graph/editparse.go`: `ParseEdit` and the `EditParse` value, the one parse of the edited file
  inside `post-tool-use` (G-09 steps 2, 7 and 8).
- `internal/lobe/graph/blastfmt.go`: the list after an edit and the report of the command (G-09 step 6).
- The blast step of `post-tool-use` on both hosts, for `Write`, `Edit`, `MultiEdit` and Codex `apply_patch`,
  after the marker of [048](048-graph-stays-current.md), with "once per session" kept in the session file.
- `tentaqles lobe graph impact [--base <ref>] [--file <path> --lines <a>-<b>] [--depth <n>] [--json] [--full]`,
  the fifth tool of R-03's registry, with its 8,000-character cap (proposed) and the person-only `--full`.
- `tentaqles lobe graph callers` moved onto this walker at depth 1 (G-09: "R-03's `callers` uses this walk"),
  with the output of [045](045-resolution-and-callers.md) unchanged.
- The `whole` row of `tentaqles lobe bench latency` for `post-tool-use` gains the case "an edit of a function
  with callers in the graph" (M-06 step 4).
- The session brief's graph sentence (R-05 step 4). With `graph` on and a `summary.json` from 048, line 1 of
  the brief of 026 continues ` Code graph: <K> symbols; query it with tentaqles lobe graph find, callers,
  outline, map or impact.` It waits for this feature because it names `impact`. It joins the build task
  that adds the `impact` command. Test (name proposed here): `TestLobeBriefGraphSentence`: the sentence
  is printed with the stored count, and not with `graph: off` or with no `summary.json`: 3 of 3.

**Out**

- `--all` on `impact`, and any read of `graph/links.json` ([056](056-many-repositories.md)). Until then the
  flag is refused with one line and exit 1.
- E-05's argument notes, which share the parse and the answer ([051](051-argument-check.md)). This feature
  hands over `EditParse` and leaves the place for the notes in front of the list.
- The quoted why-comments on the seeds, G-09 step 6 ([054](054-why-comments.md)).
- The M-01 line in `stats/` for each printed list and each `impact` answer ([058](058-token-savings.md)).
- `lobe_impact` as an MCP tool ([062](062-mcp-server.md)).
- A list after an edit of a `.sql`, Markdown, YAML, JSON or Terraform file. `ParseEdit` serves the languages
  that have a query file (G-02). For those other files the hook writes the marker and prints no list; the
  stored graph answers through `impact`. No feature in the index takes a hook list for them (reported).
- A deleted file inside the hook. `impact` seeds a deleted file from the stored graph; the hook parses only
  files that exist after the edit (decided in this brief, see Risks).
- M-07's recall figure for this walk ([057](057-graph-accuracy.md)).

## What it builds on

- [002](002-sandbox.md): the sandbox, `acme` and `globex`, the `lobeset` helper.
- [003](003-shell-failure-claude-code.md), [008](008-successes.md), [010](010-shell-failure-codex.md):
  `internal/lobe/hook` (`Main`, the client rule, the cutoffs, `Answer{Context, Deny}`, `emit`, `steps.go`) and
  the `post-tool-use` event on both hosts with the event line appended first.
- [005](005-status-and-capture-switch.md): `internal/lobe/settings` (`recall`, `graph`).
- [012](012-hook-latency-and-stall.md): `tentaqles lobe bench latency`, its `append` and `whole` rows and its
  store builders.
- [017](017-lesson-before-call.md): `sessions/<session>.json` and the `Update` call under the store lock, where
  each writer changes only its own keys (F-01 step 9, R-04).
- [023](023-fires-and-verdicts.md): E-02's verdict step, which runs before this one.
- [036](036-lessons-in-codex.md): the Codex output form of F-02 for an event that adds context.
- [042](042-parser-runtime.md): `internal/lobe/graph/parse` (`Load`, `Parse`, `Captures`) and the written
  result with the cold-start figures of G-01 measure 5.
- [043](043-graph-of-one-repository.md): `internal/lobe/graph` (`types.go`, `store.go`), `graph/extract`, the
  node and edge shapes, `tentaqles lobe graph` with `build` and `outline`, and `internal/lobe/query`
  (`registry.go`, `render.go`).
- [045](045-resolution-and-callers.md): the edges with `confidence`, `score`, `rule` and `lines`; `testpath.go`,
  the lobe's one definition of a test path; `callers`.
- [046](046-find-and-map.md): `internal/lobe/search` and its index under `graph/<repo-key>/`, with the three
  lookups of G-03 and the adjacency this walk reads. If that adjacency lacks an edge's relation, confidence,
  score or first reference line, this feature adds them to the index and its build.
- [048](048-graph-stays-current.md): `probe.go` (`Probe`), `dirty.go` (the marker step for the four edit tools,
  reading every file header of a Codex patch), `GraphDir`, `files.json`, the build id, `graph status`.

## What it leaves for later features

- `graph.ParseEdit(payload) (EditParse, bool)` and `EditParse{Path, Lang string; Src []byte; Tree parse.Tree;
  Ranges [][2]int; Symbols []Node}`. 051 reads its call expressions from that tree; 054 reads its comments.
- `graph.Blast(g, changed, depth) Report`, `Report{Seeds, Impacted, Tests, Deleted, Unindexed, Stale}` and
  `Hit{ID, Name, Path, Depth, Relation, Confidence, Score, Site}`, with JSON keys in lower case. 056 carries
  the walk across a link; 062 serves the report as `lobe_impact`; 052 and 053 rely on the walk taking every
  relation but `contains` and `explains`.
- `blastfmt.go`: one function that builds the post-edit answer from two parts, notes and list, and cuts the
  list first. Here the notes part is always empty. 051 fills it; 054 adds the why-comments to the list.
- The blast step in the `post-tool-use` row of `steps.go`, after the marker. 051 adds its step behind it.
- The session key for lists already shown (name proposed here: `blast`, a list of hashes). 051 keeps its own.
- The `impact` tool in `internal/lobe/query`, so the registry holds its five tools. 056 adds `--all`.
- The latency case with a graph behind an edit. 051 and 054 run it again with their work added.

## How it works

1. **When the step runs.** `post-tool-use`, both hosts, after the event line (S-03, or S-02 on Codex), the
   hidden-failure check (S-04), E-02's verdict and the marker of 048, in that order (G-09 "How to build it").
   A duplicate event does not end the hook early. The step is skipped, with no parse, when `recall` or `graph`
   is `off`, when the repository has no graph or an index whose build id differs from the graph's, when the
   tool is not `Write`, `Edit`, `MultiEdit` or `apply_patch`, or when the file's language has no query file.
   `lessons` and `enforce` play no part: the list is not a lesson.
2. **Ranges** (G-09 step 1). The inserted texts are `new_string` for `Edit`, each `edits[].new_string` for
   `MultiEdit`, and each run of added lines of each hunk for a Codex patch, per file header. Each text is
   looked for in the file as it is now on disk; its lines are a range. A text that is missing, is empty or
   occurs twice makes the whole file the range. `Write` and a file a patch adds are one range, the whole file.
3. **One parse** (G-09 steps 2 and 7). `ParseEdit` reads the file once, at most 1,000,000 bytes (G-02's cap),
   parses it with 042's adapter and runs G-02's extraction on it, so the symbols carry their current spans
   even after earlier edits in the turn. It returns the `EditParse` value. A Codex patch that names several
   files is parsed file by file, in the order of the patch; a file is not started once the step's deadline
   has passed. Nothing is parsed twice.
4. **Seeds** (G-09 steps 2 and 3). The symbols whose span overlaps a range. A symbol that strictly contains
   another matched symbol is dropped, so a changed method seeds itself and not its class; two symbols with one
   span both stay. A range that touches no symbol seeds the file node.
5. **Walk** (G-09 step 4). Backwards, breadth first, once per changed file, to depth 2, over every relation
   except `contains` and `explains`. From a method, the callers of the same-named method on each supertype
   are walked at the same depth, and that supertype method is not reported. Edges of every score are walked.
   In the hook the walk reads in-edges from the adjacency in the search index of this repository's
   `GraphDir` and nothing else: it never loads `graph.jsonl`, never opens `graph/links.json` and never opens
   another repository's folder.
6. **Sorting out** (G-09 step 5). A dependent reached twice keeps its shallowest depth. Dependents inside a
   changed file are dropped. Dependents on a test path (`testpath.go`) go to the tests list.
7. **The list.** A header that says the list is "from the code graph" and never says "all callers" (wording
   proposed in task 5). Then at most 8 lines `<relation> <- <name> (<path>:<site line>)`, depth 1 first and
   hits reached through an `inferred` edge last; then `+N more`; then `tests: N files (...)` naming up to 4
   files, `1 file` for one. `Site` is the first of the edge's `lines`. A hit whose file a dirty marker names
   gains ` (may be stale)` (G-08 step 7). With no dependent and no test file nothing is printed.
8. **Budget and repeat.** The post-edit answer is one `additionalContext` of its own, outside F-05's frame,
   of at most 700 characters (proposed; the size of R-04's budget). It is built from notes and list; the list
   is cut first, from its end. A hash of the list is kept under this feature's key in
   `sessions/<session>.json`; a list whose hash is there is not printed again. The write happens after the
   print and is skipped when the lock is busy.
9. **Time and failure.** The step is inject work: it aims at 300 ms at the 95th percentile (proposed) inside
   the 1.5 second cutoff of the whole process (core page 5.2). At the cutoff nothing is printed; the event
   line and the marker are already written. Any error ends the step with no output (F-02).
10. **Codex.** `tentaqles lobe-hook codex post-tool-use` runs the same step on an `apply_patch` payload, under
    `CODEX_HOME`. The answer leaves through F-02's writer in the form 036 fixed for that event.
11. **The command.** `tentaqles lobe graph impact` resolves the client from the working folder and reads only
    that client's `GraphDir` (R-03 step 5), with the login rule of F-07 step 3. It runs `Probe` first and
    never builds: drifted paths are listed under `stale` with the fix line `tentaqles lobe graph build`.
    - `--file <path> --lines <a>-<b>` gives one range. Otherwise the ranges are the new-side hunks of
      `git diff --unified=0` against `--base <ref>`, or against `HEAD` without the flag (decided here). git
      runs as a child process with `--no-optional-locks` under a deadline of its own (G-02's rule).
    - Seeds come from the stored graph's spans. A deleted file is seeded with all its stored symbols, and its
      hits are marked "target deleted". A changed file the graph does not hold is listed under `unindexed`.
    - `--depth <n>` replaces 2. `--json` prints the `Report`. The text form is capped at 8,000 characters
      (proposed) with a last line that counts what was cut; `--full` lifts the cap and is refused when
      `CLAUDECODE` is set. `--all` is refused until 056.
    - With `graph: off`, or without a graph, it prints one line on stderr and exits 1 (G-08, R-03).
12. **`callers`.** The verb of 045 calls the same walker at depth 1 over R-01's five relations. Its text and
    JSON stay byte for byte what 045's tests pin.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-09](../prd/lobe-graph.md), all of it except `--all`; [G-08](../prd/lobe-graph.md) steps 3 and 7; [R-03](../prd/lobe-recall.md) steps 5 and 6 and "How to build it"; [R-04](../prd/lobe-recall.md) "How to build it", the last item (the post-edit answer); [F-02](../prd/lobe-foundation.md) step 2 (the `post-tool-use` row) and step 6; [M-06](../prd/lobe-benchmarks.md) step 4; [core page](../prd/vertical-lobe.md) 5.1, 5.2 and decision A-25 |
| Source to port: seeds and walk | [`src/blast/blast.ts` L169](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/blast/blast.ts#L169) (innermost symbol; we require strict containment), [L237](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/blast/blast.ts#L237) (the walk per changed file); `src/blast/diff.ts` L132 and `src/blast/blast-cli.ts` L43 in the same commit |
| Source to port: the list | [`src/claude/format.ts` L72](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L72) (file-level there, symbol-level here), [L83](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L83) (the cap of 8) |
| Source: test paths, idea only | [`src/blast/blast.ts` L536](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/blast/blast.ts#L536); ours is `testpath.go` of 045 |
| Source to port: overrides | [`graphify/affected.py` L297](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/affected.py#L297), [L355](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/affected.py#L355) |
| Evidence for depth 2 | [`graphify/extract.py` L2476](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L2476) |
| Lobe code from earlier features | `internal/lobe/hook/steps.go`, `internal/lobe/graph/dirty.go`, `probe.go`, `internal/lobe/search`, `internal/lobe/query`, `internal/lobe/benchcheck/latency` |
| Existing tentaqles code | `docs/reference/commands.md`; `test/integration/docs_test.go` (`documentedGroups`); `internal/cmd/run.go` (`insideAgent`, the check `--full` uses through `cli.Deps`) |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the index's adjacency, the
   `post-tool-use` step order, the registry, the Codex `apply_patch` payload fields that 048 reads. Write the
   Codex edit JSON of Evaluation check 13 into this file. The owner approves changes before code is written.
2. **Measure the parse inside the hook.** A throwaway test starts the real `post-tool-use` process 50 times on
   Windows and on Linux with a 200-line file per first language, grammar cache warm, and records the whole
   time. Read it beside 042's result (G-01 measure 5). If more than 5 in 100 runs reach the 1.5 second cutoff,
   stop and take G-09's fallback to the owner: a file-wide list, labeled so, with no parse. Write the numbers
   into Risks below.
3. **`blast.go`** (side by side with 4 and 5): ranges from a diff, seeds, innermost, walk, supertype rule,
   shallowest depth, the drop of in-file dependents, tests apart, deleted and unindexed files. Pure functions
   over a small read interface, so the command passes a loaded graph and the hook passes the index. Tests:
   `TestLobeBlastSymbolLevel`, `TestLobeBlastSeeds`, `TestLobeBlastOverride`, `TestLobeBlastTestsApart`,
   `TestLobeBlastDeletedFileSeeds`.
4. **`editparse.go`**: inserted texts per tool, ranges, the one parse, the size cap. Tests:
   `TestLobeBlastEditRanges`, `TestLobeBlastOneParse` (the parse count; 051 adds the hand-over half).
5. **`blastfmt.go`**: the header, the 8 lines, `+N more`, the tests line, the stale mark, the two-part answer
   with the list cut first, and the text and JSON of the report. Tests: `TestLobeBlastCap`,
   `TestLobeBlastNotesBeforeList` (with hand-made notes), a golden file for the report text.
6. **The hook step**, both hosts: `steps.go`, the switches, the session key, the stay-in-repository rule.
   Tests: `TestLobeBlastSilentWhenOff`, `TestLobeBlastCutoffKeepsCapture`, `TestLobeBlastHookStaysInRepo` (the
   hook half), `TestLobeBlastOncePerSession`, `TestLobeBlastCodexPatch`.
7. **The command**: `impact` in `internal/lobe/query` and `internal/lobe/cli`, the `--full` check through
   `cli.Deps`, the refusal of `--all`, `callers` on the walker. Tests: `TestLobeBlastImpactCap`,
   `TestLobeQueryToolCount` (now 5), `TestLobeQueryNeverBuilds`, `TestLobeQueryOtherClient` and
   `TestLobeCommandRefusesAnotherLogin` with `impact` added, 045's `callers` tests unchanged.
8. **Latency.** Add the edit case to the `whole` row of `post-tool-use` in `internal/lobe/benchcheck/latency`,
   with a graph in the typical and large stores if no earlier feature put one there. Run
   `tentaqles lobe bench latency` on the dev box and note the figures in Risks.
9. **Docs.** The `impact` action in the `tentaqles lobe graph` section of `docs/reference/commands.md`; the
   `post-tool-use` rows of the `tentaqles lobe-hook` section; one line in `CHANGELOG.md` under `[Unreleased]`.
   The plugin does not change.
10. **Tester run** of the Evaluation section by a fresh agent.

## Tests that stay in the repository

- `TestLobeBlastSymbolLevel`: with `add` and `area` in one file, editing `add` lists 1 caller of `add` and 0 of
  `area`.
- `TestLobeBlastSeeds`: a change in method `get` of class `Cache` gives 1 seed; two symbols of one span give 2.
- `TestLobeBlastOverride`: `main` calls `Animal.speak`; a change in `Dog.speak` lists `main`, and 0 hits
  without the inheritance.
- `TestLobeBlastCap`: 20 dependents render as 8 lines and `+12 more`.
- `TestLobeBlastTestsApart`: 3 dependents in test files give 0 lines in the main list and `tests: 3 files`.
- `TestLobeBlastOneParse`: an edit with a graph runs exactly 1 parse of the edited file.
- `TestLobeBlastCutoffKeepsCapture`: with the parser stalled 2 seconds, the process ends by the 1.5 second
  cutoff with empty stdout, 1 event line and 1 marker.
- `TestLobeBlastSilentWhenOff`: `recall: off` or `graph: off` gives empty stdout and 0 parses. 2 of 2.
- `TestLobeBlastHookStaysInRepo`, the hook half: with a file `graph/links.json` planted and a second
  repository's graph that calls the edited symbol, the list holds 0 paths of the second repository and the
  process opens 0 files outside the edited repository's graph folder. 056 adds the `impact --all` half.
- `TestLobeBlastImpactCap`: a report of 20,000 characters prints at most 8,000 from `impact`; with `--full` it
  prints whole, and under `CLAUDECODE=1` `--full` exits non-zero.
- `TestLobeBlastNotesBeforeList`: notes and a list that together pass 700 characters: the notes are whole,
  they come first, and only the list is cut. Here the notes are hand-made; 051 runs it with E-05's own.
- New names in this brief: `TestLobeBlastEditRanges` (a text found once, twice, never and empty; `Write`; a
  two-file Codex patch: 6 of 6), `TestLobeBlastDeletedFileSeeds` (a deleted file seeds its stored symbols and
  marks its hits), `TestLobeBlastOncePerSession` (the second equal list prints 0 bytes; a different list
  prints), `TestLobeBlastCodexPatch` (an `apply_patch` payload gives the same list as `Edit`).
- R-03's tests that now count `impact`: `TestLobeQueryToolCount` (exactly 5 tools), `TestLobeQueryNeverBuilds`,
  `TestLobeQueryOtherClient`, `TestLobeQueryCaps`.
- House rules: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeCaptureSurvivesALateStep`, `TestLobeGraphPostEditOnlyMarks` (048: with no graph the hook still opens
  0 source files), `TestLobeEveryInjectionIsFramed` (a blast list holds no lesson text),
  `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, `TestLobeCommandTable`,
  `TestDocsReferenceEveryCommand`, and every test under "Must not trade off" in `CLAUDE.md`, unedited.

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
lobeset() {  # lobeset <client> <key> <value>: a fixture edit of a sandbox manifest
  m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"
}
```

If a line starts with `SETUP FAIL`, stop and report FAIL. Then the fixture:

```bash
S="$W/clients/acme/calc"; git init -q "$S"; cd "$S"
printf 'def add(a, b):\n    return a + b\n\ndef area(w, h):\n    return w * h\n' > calc.py
printf 'from calc import add\n\ndef total(xs):\n    return add(xs[0], xs[1])\n' > total.py
printf 'from calc import area\n\ndef report(w, h):\n    return area(w, h)\n' > report.py
printf 'from total import total\n\ndef grand(xs):\n    return total(xs)\n' > grand.py
printf 'from calc import add\n\ndef test_add():\n    assert add(1, 2) == 3\n' > test_calc.py
git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m one
tentaqles lobe graph build || echo "SETUP FAIL build"
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
edit() {  # edit <session>: the Edit payload for calc.py, piped to the Claude Code hook
  printf '{"session_id":"%s","hook_event_name":"PostToolUse","cwd":"%s","tool_name":"Edit","tool_input":' "$1" "$S"
  printf '{"file_path":"%s/calc.py","old_string":"return a + b","new_string":"return b + a"}}' "$S"; }
hook() { edit "$1" | tentaqles lobe-hook claude post-tool-use; echo " exit=$?"; }
```

Run every check in `$S` unless it says otherwise. A tester never runs `tentaqles lobe set`.

1. `tentaqles lobe graph impact --file calc.py --lines 2-2 --json`: exit 0. `seeds` has 1 entry, with
   `"name":"add"`. `impacted` holds `total` with `"depth":1` and `grand` with `"depth":2`, and nothing with
   path `report.py` or `test_calc.py`. `tests` has an entry with `"path":"test_calc.py"`. With `--depth 1`
   added, `grand` is absent.
2. Change `return a + b` to `return b + a` in `calc.py`, then `hook s1`: ` exit=0`; stdout is one JSON object
   whose `hookSpecificOutput.additionalContext` contains `calls <- total (total.py:4)`,
   `<- grand (grand.py:4)` and `tests: 1 file (test_calc.py)`, contains `from the code graph`, does not
   contain `report`, has at most 700 characters, and holds no `permissionDecision`.
3. `hook s1` again: nothing is printed before ` exit=0`.
4. `lobeset acme recall off`, then `hook s2`: nothing before ` exit=0`. `lobeset acme recall on; lobeset acme
   graph off`, then `hook s3`: nothing before ` exit=0`.
5. `lobeset acme graph on`, then `hook s4`: stdout contains `calls <- total (total.py:4)` again.
6. `cat "$TQ_HOME"/identities/acme/lobe/events/events-*.jsonl | grep -c '"session":"s2"'` prints 1 or more:
   the event was recorded although nothing was printed.
7. `tentaqles lobe graph impact --file calc.py --lines 2-2`: exit 0; the text names `total`, `grand` and
   `test_calc.py` and has at most 8,000 characters.
8. `tentaqles lobe graph impact --base HEAD --json`: exit 0; `seeds` holds `add`; `impacted` holds `total`;
   `stale` names `calc.py`, because the file changed in check 2 and nothing was built since. The same command
   without `--json` contains the line `tentaqles lobe graph build`. `ls "$TQ_HOME"/identities/acme/lobe/graph`
   still shows one folder and `tentaqles lobe graph status --json` still shows `"changed":["calc.py"]`: the
   command built nothing.
9. `CLAUDECODE=1 tentaqles lobe graph impact --file calc.py --lines 2-2 --full; echo "exit=$?"`: the last line
   is not `exit=0`.
10. `tentaqles lobe graph impact --all --file calc.py --lines 2-2; echo "exit=$?"`: `exit=1` and one line that
    names `--all`.
11. `lobeset acme graph off`, then `tentaqles lobe graph impact --file calc.py --lines 2-2; echo "exit=$?"`: a
    line that names the `graph` setting, then `exit=1`. `lobeset acme graph on`.
12. Isolation. `(cd "$W/clients/globex/app" && tentaqles lobe graph impact --file calc.py --lines 2-2; echo
    "exit=$?")`: `exit=1` and no `total` in the output. Then
    `edit s5 | CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles lobe-hook claude post-tool-use`:
    empty stdout, and `ls "$TQ_HOME"/identities/globex/lobe/graph 2>/dev/null | wc -l` prints `0`.
13. Codex. `export CODEX_HOME="$TQ_HOME/identities/acme/codex"` and pipe the Codex edit JSON to
    `tentaqles lobe-hook codex post-tool-use`: exit 0 and the output contains `calls <- total (total.py:4)`.
    The Codex edit JSON is the `apply_patch` payload for session `c1` and folder `$S` with one file header,
    `calc.py`, and one hunk that replaces `return a + b` by `return b + a`. Build task 1 writes it here in
    full, with the field names 048's marker step reads.

**Person checks** (the owner, at a terminal, in the same shell after the checks)

1. `tentaqles lobe set recall off --client acme`, answered with yes. `hook p1` prints nothing before ` exit=0`.
   `tentaqles lobe set recall default --client acme`, answered with yes. `hook p2` prints the list again.
2. `tentaqles lobe graph impact --file calc.py --lines 2-2 --full` exits 0 and prints the report.

PASS needs 13 of 13. "Could not run" is a FAIL. The tester reports anything surprising and fixes nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems with `CGO_ENABLED=0`).
- [ ] The tester brief passes, 13 of 13, and the owner ran the two person checks.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeBlastCutoffKeepsCapture`, `TestLobeBlastHookStaysInRepo`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, and `tentaqles lobe bench leak` at zero leaks.
- [ ] `tentaqles lobe bench latency` was run with the edit case, and no run passed the 1.5 second cutoff.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen the list after an edit and the `impact` report described under Result.

## Risks and unknowns

- **Parse time in the hook** (G-09 risk, G-01 measure 5). Unknown until build task 2. On Windows process start
  is already a large share of the 300 ms aim (proposed). The 1.5 second cutoff decides: nothing is printed for
  that edit. The fallback, a file-wide list labeled so, changes steps 3 and 4 of this file and leaves 051 with
  no parse.
- **The Codex patch payload.** Its field names come from the capture behind 010 and the fixtures of 048; this
  file was written without them. If a patch carries no added lines per hunk, every patched file is one range.
- **Decided in this brief, not in the requirements:** the default base `HEAD`; the hook skips deleted files and
  files with no query file; a multi-file patch is parsed in order until the deadline; the session key name
  `blast`; an empty answer when there is no dependent and no test file; the 700 characters apply to the whole
  post-edit answer. Each can be overturned by the owner without touching the walk.
- **A short list can read as "safe to change"** when edges are missing (G-09 risk). The header says where the
  list comes from; M-07 (057) measures the recall of this walk.
- **A renamed function** has no callers under its new name, and the callers of the old name are not seeds. The
  list is then empty until the build after the turn.
- **Grammar cache.** If 042's runtime keeps compiled grammars, the hook reads them from this client's
  `index/grammars/` only (core page 5.3, principle 1).
- **The numbers** 8 lines, 4 test names, 700 and 8,000 characters and the 300 ms aim are proposed in the
  requirements and stay proposed here.
