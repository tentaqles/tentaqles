# 051: Argument check after an edit

**Status** not started · **Stage** Graph · **Entries** E-05, M-05 (the argument-check row), G-04 step 9 ·
**Needs** 047, 048, and in fact also 049 (the one parse) and 041 (the benchmark) · **Feeds** 058

## Result

The agent writes a call to a function of the client's repository with a number of arguments the function
cannot take. Right after the edit it reads one line that shows the signature. The call is never stopped: this
is a note, at every setting. The enforcement accuracy benchmark of [041](041-enforcement-accuracy.md) gains a
row for this check, and a language's notes print only once that row has met its two numbers.

From the sandbox of [002](002-sandbox.md), with acme's login exported and the `shop` fixture of the Evaluation
section built (`lib.py` holds `def add(a, b):`), after line 2 of `main.py` becomes `print(add(1, 2, 3))`:

```bash
printf '%s' '{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"'"$S"'","tool_name":"Edit",
"tool_use_id":"t1","tool_input":{"file_path":"'"$S"'/main.py","old_string":"add(1, 2)",
"new_string":"add(1, 2, 3)"}}' | tr -d '\n' | tentaqles lobe-hook claude post-tool-use
```

prints one JSON object whose `additionalContext` holds:

```
Argument count: add in lib.py takes 2 arguments (def add(a, b)); the call at main.py:2 passes 3.
```

and, with `lobebench` built from the same commit, `lobebench enforce --fixture argcheck --out "$W/m05a"`
exits 0 and writes a summary with an `argument check` row that has 0 wrong blocks.

## Scope

**In**

- The package `internal/lobe/argcheck`: `argcheck.go` (`Check(tree, ranges, graph) []Note`), `calls.go` (the
  call expressions in the changed ranges) and `overloads.go` (E-05 step 5).
- The argument step of `post-tool-use` on both hosts, behind the blast step of [049](049-blast-radius.md), on
  the same `EditParse`, with its notes in front of the blast list in one answer.
- The limits of E-05: 20 calls per edit, a file of at most 512 KiB, 3 notes per edit, each note once per
  session, 300 ms (all proposed).
- The table of languages whose notes print, and the rule that a language enters it only with a result that
  meets M-05's two numbers (E-05 "How to build it", G-04 step 9).
- The argument-check row of M-05: the case form for an edit, its replay through `post-tool-use`, the rule that
  a deny from it is a wrong block, the fixture `argcheck`, and the replay set for Python.

**Out**

- The M-01 line in `stats/` for each printed note ([058](058-token-savings.md)).
- A call that resolves to a class (a constructor): not checked in this version (G-04).
- Languages outside G-04's first signature set. They have no `params`, so they are never judged;
  [060](060-remaining-languages.md) adds languages with `sig` only.
- A check before the edit. E-05 explains why it runs after: the file on disk is the new text and 049's parse
  is reused.
- The wrong-argument-count family of the repeat-mistake benchmark and its arm `all` (M-04 step 2, "join in
  phase 4"). No feature in the index names it; reported to the owner.
- Replay sets for TypeScript with TSX, Go, Java and C# are built here only as far as "Done when" says: each
  language stays silent until its own set meets the numbers.

## What it builds on

- [002](002-sandbox.md): the sandbox and `lobeset`.
- [017](017-lesson-before-call.md): `sessions/<session>.json` and `Update`, each writer with its own keys.
- [021](021-benchmark-harness.md): `lobebench`, built from `bench/cmd/lobebench`, and the receipts that
  `tentaqles lobe bench check-claims` reads.
- [036](036-lessons-in-codex.md): the Codex output form for `post-tool-use`.
- [041](041-enforcement-accuracy.md): `lobebench enforce`, `bench/enforce/cases.jsonl`, `bench/internal/enforce`
  (a fresh sandbox per case, the login variable, classification as deny, advice or nothing from exit code,
  stdout and stderr only), and `summary.json` with the three rates per stage and rule source.
- [045](045-resolution-and-callers.md): `internal/lobe/graph/resolve`, whose rule table reads nodes through a
  lookup interface (by name; by file and name; by owner and name), so a hook can resolve one call.
- [046](046-find-and-map.md): the index under `graph/<repo-key>/` that serves those lookups in a hook.
- [047](047-signatures.md): `extract.CountArgs`, `extract.CountParams`, `Params{N, Min, Variadic, Check}`,
  `graph.Fits`, the `args` and `params` captures, `sig` and `params` on a node, `argc` on an edge. If the index
  does not carry `sig` and `params` for a node, this feature adds them to the index and its build.
- [048](048-graph-stays-current.md): the dirty markers, read here to know which files the graph has not read.
- [049](049-blast-radius.md): `graph.ParseEdit`, `EditParse{Path, Lang, Src, Tree, Ranges, Symbols}`, the
  function in `blastfmt.go` that joins notes and list and cuts the list first, the step order of
  `post-tool-use`, and `TestLobeBlastOneParse`.

## What it leaves for later features

- `argcheck.Check` and `Note`. 058 counts printed notes; nothing else calls them.
- The language table in `internal/lobe/argcheck` (name proposed here: `enabled`). A later change adds a
  language together with its result file.
- The session key for notes already shown (name proposed here: `argnotes`, a list of hashes).
- The rule source `argument check` in `bench/internal/enforce` and in `summary.json`; the case key `repo`
  (proposed here) that names a fixture repository; `bench/enforce/argcheck/`.

## How it works

1. **When the step runs** (E-05 step 1). `post-tool-use` for `Write`, `Edit`, `MultiEdit` or Codex
   `apply_patch`, after the event line, E-02's verdict, the marker and the blast walk. It needs `graph` on,
   `recall` on, `enforce` not `off`, a graph with a usable index for the repository, and a language whose
   counts G-04 stores. `lessons` plays no part. When 049's step made no `EditParse`, this step does nothing.
2. **Calls** (E-05 step 2). From the tree in `EditParse`, the call expressions that overlap its `Ranges`, in
   file order, at most 20, in a file of at most 524,288 bytes. The node names come from G-02's query files
   (the `reference.call` capture and its `args`). A parse error inside the changed ranges ends the check. A
   new file is one range. There is no second parse.
3. **Target** (E-05 step 3). The name is resolved with G-03's rule table: imports from the fresh tree, nodes
   from the index lookups. The check goes on only for exactly one target at the highest level: `extracted`,
   score 1. An inferred target, a guess or several candidates ends the check for that call.
4. **Skips** (E-05 step 4). `CountArgs` returns -1 (a spread or unpacked argument; in Go a single argument
   that is a call). The target is a class. The target is in another file that a dirty marker names. The
   target is one of several overloads and one has no stored count. The target's `check` is false. A target
   defined in the edited file is counted from the fresh tree with `CountParams`, not from the graph.
5. **Mismatch** (E-05 step 5, G-04 step 7). `graph.Fits(params, argc)` is the only fit rule: a call fits when
   `min <= argc` and (`argc <= n` or `variadic`). With overloads it is a mismatch only when no overload
   accepts the count. This package holds no counting code.
6. **The note.** One line per mismatch, in E-05's form: `Argument count: <name> in <path> takes <counts>
   arguments (<sig>); the call at <path>:<line> passes <argc>.` `<counts>` is `n` when `min` equals `n`; the
   words for a range and for a variadic target are fixed in task 3 (proposed: `1 to 2`, `at least 1`). At
   most 3 notes per edit. A note whose hash is under this feature's key in `sessions/<session>.json` is not
   printed again; the key is written after the print.
7. **One answer** (E-05 step 6). The notes and 049's list leave in one `additionalContext`, outside F-05's
   frame: the notes first, then the list. Over 700 characters (proposed) the list is cut first. A note is
   never cut to make room for the list.
8. **Time** (E-05 step 7). The step has 300 ms (proposed) inside the 1.5 second cutoff. When its time runs
   out the answer goes out without its lines, with the list. A cutoff of the whole process loses the answer
   and neither the event line nor the marker.
9. **Never a deny.** The step returns `Answer.Context` only. It writes no line to `trials.jsonl`, no entry to
   `ladder.json`, and nothing but its session key. `enforce: block` changes nothing.
10. **Languages.** The check runs for every language of the first signature set (Python, TypeScript with TSX,
    Go, Java, C#; JavaScript has `check` false). It prints only for a language in this package's language
    table. The table is a plain literal with no package-level call (core page 5.1). A language enters it in
    the same change that commits its replay result (step 13).
11. **Codex.** A patch that names several files is checked file by file, in the order of the patch, 20 calls
    in all. The answer leaves in the form 036 fixed.
12. **The benchmark row** (M-05 steps 2, 7 and 8). A case of the argument check is one line of
    `bench/enforce/cases.jsonl` whose `hook` is the PostToolUse JSON of an edit. It has no lesson: its `rule`
    is `argcheck`, and `repo` names a fixture repository under `bench/enforce/argcheck/`. For each case the
    harness builds a fresh sandbox, copies the repository into the client, builds the graph, makes the edit
    in the file as the edit tool would have left it, sets the login variable and pipes `hook` to
    `tentaqles lobe-hook <host> post-tool-use`. A printed note is `advice`. A deny is counted as a wrong block
    whatever the label. The row reports the three rates of M-05 step 8 and, per language, the notes printed,
    the notes on cases labeled `bad`, the good cases and the good cases that got a note.
13. **Switching a language on.** Two numbers, both proposed: at least 95 in 100 printed notes name a real
    mismatch, and 1,000 replayed correct edits print 0 notes. Decided in this brief: the correct edits are
    call sites that exist in pinned code (each replayed as the line stands), from tentaqles' own source for
    Go and from pinned public repositories named in `bench/enforce/argcheck/corpus.json` (URL, commit,
    license) for the others, fetched by `lobebench` and never committed. The `bad` cases, at least 100 per
    language, are real calls with one argument added or removed, labeled under M-05 step 5. The run happens on
    the branch that adds the language to the table, and the result file is committed with it.

## Where to read

| What | Where |
|---|---|
| Requirements | [E-05](../prd/lobe-enforce.md), all of it; [G-04](../prd/lobe-graph.md) steps 6 to 9 and "How to build it"; [G-03](../prd/lobe-graph.md) "How to build it" (the lookup interface); [G-09](../prd/lobe-graph.md) steps 7 and 8; [M-05](../prd/lobe-benchmarks.md) steps 1 to 9; [R-04](../prd/lobe-recall.md) "How to build it", the last item; [core page](../prd/vertical-lobe.md) section 4 (the table of mistakes), 5.2 and 5.6 |
| Source: what the two projects store | [`src/graph/extract.ts` L1677](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L1677), [L1919](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L1919), [L713](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L713) |
| Source to port: the count rule | [`src/graph/resolve.ts` L445](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L445); [`graphify/extractors/engine.py` L242](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L242), [L287](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L287) |
| Source: weaknesses not taken | [`src/graph/resolve.ts` L447](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L447), [L478](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L478); [`graphify/extractors/engine.py` L10609](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L10609): both guess when no overload fits |
| Lobe code from earlier features | `internal/lobe/graph/editparse.go`, `blastfmt.go`, `types.go` (`Fits`); `internal/lobe/graph/extract/params.go`; `internal/lobe/graph/resolve`; `internal/lobe/hook/steps.go`; `bench/internal/enforce`; `bench/enforce/cases.jsonl` |
| Existing tentaqles code | none outside the lobe; `docs/reference/commands.md` for the hook's section |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the shape of `EditParse`,
   whether the index carries `sig` and `params`, how `summary.json` names a rule source and its rates, how a
   case of 041 installs its fixture, the Codex patch payload. Write the Codex edit JSON of Evaluation check
   13 into this file. The owner approves changes, and the corpus list of step 13, before code is written.
2. **`calls.go` and `overloads.go`** (side by side with 3): calls in ranges, the limits, the skips, the
   overload rule. Tests: `TestLobeArgcheck_CountRule`, `TestLobeArgcheck_SpreadSkips`,
   `TestLobeArgcheck_OnlyWhenCertain`, `TestLobeArgcheck_Limits`.
3. **`argcheck.go`**: `Check`, the note text, the stale-target skip, the count from the fresh tree for a
   target in the edited file. Tests: `TestLobeArgcheck_StaleTargetSkips`, `TestLobeArgcheck_NoteText`.
4. **The hook step**, both hosts: `steps.go`, the switches, the 300 ms, the session key, the joined answer.
   Tests: `TestLobeArgcheck_NotesBeforeTheList`, `TestLobeArgcheck_OneParse`, `TestLobeArgcheck_Budget`,
   `TestLobeArgcheck_NeverDenies`, `TestLobeArgcheck_NoTrialLine`, `TestLobeArgcheck_OncePerSession`,
   `TestLobeArgcheck_CodexPatch`, `TestLobeBlastOneParse` (the hand-over half).
5. **The language table** with no language in it, and its guard. Tests:
   `TestLobeArgcheck_SwitchedOffLanguageIsSilent`, `TestLobeArgcheck_EnabledNeedsResult`.
6. **The benchmark row** (side by side with 2 to 5): the case form, the replay through `post-tool-use`, the
   source in `summary.json`, the fixture `argcheck`. Tests: `TestLobeEnforceArgcheckGoesToPostToolUse`,
   `TestLobeEnforceArgcheckDenyIsWrongBlock`, and 041's tests unchanged.
7. **The Python replay set**: the corpus entry, the generator of correct edits, at least 100 `bad` cases with
   their labels. Run it. When the two numbers hold, add `python` to the table and commit the result file with
   it. When they do not, stop and report the count and the failing cases to the owner; fix the counting table
   in 047's code, never the cases.
8. **The other four languages**, one at a time, each as task 7. A language that does not reach its numbers
   stays out of the table and is named in the changelog as "checked silently".
9. **Docs.** The `post-tool-use` rows of the `tentaqles lobe-hook` section in `docs/reference/commands.md`; a
   paragraph in the bench README on the argument-check row; one line in `CHANGELOG.md` under `[Unreleased]`
   that lists the languages whose notes are on. The plugin does not change.
10. **Tester run** of the Evaluation section by a fresh agent.

## Tests that stay in the repository

- `TestLobeArgcheck_CountRule`: 10 cases of `min`, `n`, `variadic` and a count, counted by `CountArgs` and
  judged by `Fits`, give 10 of 10 expected answers.
- `TestLobeArgcheck_NotesBeforeTheList`: an edit whose notes and blast list pass the budget keeps every note,
  ahead of the list, in one `additionalContext`, and loses blast lines.
- `TestLobeArgcheck_NoTrialLine`: after 5 mismatches `trials.jsonl` and `ladder.json` are unchanged.
- `TestLobeArgcheck_OnlyWhenCertain`: two functions of one name that G-03 cannot tell apart give 0 notes.
- `TestLobeArgcheck_SpreadSkips`: `f(*xs)` gives 0 notes.
- `TestLobeArgcheck_StaleTargetSkips`: a target whose file a dirty marker names gives 0 notes.
- `TestLobeArgcheck_OneParse`: one edit that yields a blast list and a mismatch parses the edited file 1 time.
- `TestLobeArgcheck_NeverDenies`: with `enforce: block` and a mismatch, stdout holds no deny decision.
- `TestLobeArgcheck_Budget`: with the check stalled 2 seconds, the event line and the marker are written and
  the answer goes out by the cutoff without an argument note.
- `TestLobeBlastOneParse` (049), completed: E-05's check receives the same tree the blast step parsed.
- New names in this brief: `TestLobeArgcheck_Limits` (25 changed calls judge 20; a file of 524,289 bytes
  judges 0; 5 mismatches print 3), `TestLobeArgcheck_NoteText` (equal counts, a range, a variadic target; a
  target in the edited file counted from the fresh tree), `TestLobeArgcheck_OncePerSession`,
  `TestLobeArgcheck_CodexPatch` (two files in one patch, 20 calls in all),
  `TestLobeArgcheck_SwitchedOffLanguageIsSilent` (a mismatch in a language outside the table prints 0 bytes and
  still runs), `TestLobeArgcheck_EnabledNeedsResult` (every language in the table has a committed result file
  that meets the two numbers), `TestLobeEnforceArgcheckGoesToPostToolUse` (an argument case is piped to
  `post-tool-use`, never to `pre-tool-use`), `TestLobeEnforceArgcheckDenyIsWrongBlock` (a stub hook that
  denies on such a case raises `wrong_blocks` for a case labeled `bad` too).
- From 041, unchanged and green: `TestLobeEnforceZeroWrongBlocks`, `TestLobeEnforceCountsAMiss`,
  `TestLobeEnforceTimeoutIsNothing`, `TestLobeEnforceSetSize`.
- House rules: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeCaptureSurvivesALateStep`, `TestLobeEveryInjectionIsFramed` (an answer of argument notes holds no
  lesson text), `TestLobeHookNeverPrintsAllow`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  and every test under "Must not trade off" in `CLAUDE.md`, unedited.

## Evaluation

A fresh tester agent gets the built `tentaqles` and a `lobebench` built from the same commit, both first on
`PATH`, the repository's `bench` folder to read, an empty working folder and this section. Use bash (Git Bash
on Windows). No network. The standard setup:

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
S="$W/clients/acme/shop"; L="$TQ_HOME/identities/acme/lobe"; git init -q "$S"; cd "$S"
printf 'def add(a, b):\n    return a + b\n' > lib.py
printf 'from lib import add\nprint(add(1, 2))\n' > main.py
tentaqles lobe graph build || echo "SETUP FAIL build"
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
to() {  # to <session> <call>: write line 2 of main.py as the edit tool would, then pipe the edit
  printf 'from lib import add\nprint(%s)\n' "$2" > "$S/main.py"
  printf '{"session_id":"%s","hook_event_name":"PostToolUse","cwd":"%s","tool_name":"Edit",' "$1" "$S"
  printf '"tool_use_id":"t1","tool_input":{"file_path":"%s/main.py",' "$S"
  printf '"old_string":"add(1, 2)","new_string":"%s"}}' "$2"; }
run() { to "$1" "$2" | tentaqles lobe-hook claude post-tool-use; echo " exit=$?"; }
```

Run every check in `$S`. A tester never runs `tentaqles lobe set`.

1. `run s1 "add(1, 2, 3)"`: ` exit=0`; stdout is one JSON object whose `additionalContext` contains
   `Argument count`, `takes 2`, `def add(a, b)`, `main.py:2` and `passes 3`; stdout holds no
   `permissionDecision`.
2. `run s2 "add(3, 4)"`: stdout does not contain `Argument count`.
3. `run s3 "add(*[1, 2, 3])"`: stdout does not contain `Argument count`.
4. `run s1 "add(1, 2, 3)"` again: stdout does not contain `Argument count` (once per session).
5. `printf 'def add(a, b, c):\n    return a + b + c\n' > lib.py`, then pipe one line to
   `tentaqles lobe-hook claude post-tool-use`: the JSON of `to` with `session_id` `s4`, `file_path`
   `$S/lib.py`, `old_string` `def add(a, b):` and `new_string` `def add(a, b, c):`. Then
   `run s5 "add(1, 2, 3)"`: stdout does not contain `Argument count`, because the graph has not read the new
   `lib.py` and a marker names it.
6. `tentaqles lobe graph build`, then `run s6 "add(1, 2)"`: stdout contains `Argument count`, `takes 3` and
   `passes 2`.
7. `lobeset acme enforce block`, then `run s7 "add(1, 2)"`: stdout contains `Argument count` and no
   `permissionDecision`.
8. `lobeset acme enforce off`, then `run s8 "add(1, 2)"`: stdout does not contain `Argument count`.
   `lobeset acme enforce advice`.
9. `lobeset acme recall off`, then `run s9 "add(1, 2)"`: nothing is printed before ` exit=0`.
10. `lobeset acme recall on; lobeset acme graph off`, then `run s10 "add(1, 2)"`: nothing before ` exit=0`.
    `lobeset acme graph on`.
11. `lobeset acme lessons off`, then `run s11 "add(1, 2)"`: stdout contains `Argument count`: the `lessons`
    setting plays no part. `lobeset acme lessons on`.
12. `cat "$L/trials.jsonl" 2>/dev/null | wc -l` prints `0`, and
    `cat "$L"/events/events-*.jsonl | grep -c '"kind":"ok"'` prints 1 or more: the edits were recorded and no
    trial line was written.
13. Codex. `export CODEX_HOME="$TQ_HOME/identities/acme/codex"`, write line 2 of `main.py` as
    `print(add(1, 2, 3, 4))` and pipe the Codex edit JSON to `tentaqles lobe-hook codex post-tool-use`: exit 0
    and the output contains `Argument count`, `takes 3` and `passes 4`. The Codex edit JSON is the
    `apply_patch` payload for session `c1` and folder `$S` with one file header, `main.py`, and one hunk that
    replaces line 2 by `print(add(1, 2, 3, 4))`. Build task 1 writes it here in full.
14. `lobebench enforce --fixture argcheck --out "$W/m05a"`: exit 0. In `$W/m05a/summary.json` the
    `argument check` row has `good` 4, `wrong_blocks` 0 and no good case that got advice, and `bad` 2 with no
    miss. `$W/m05a/cases/b1.json` holds the hook's exit code, stdout and stderr, and `classified` is `advice`.
    The fixture is one Python repository with `def add(a, b)`, `def scale(x, factor=2)` and `def log(*parts)`
    and six edits: `b1` `add(1, 2, 3)`, `b2` `add(1)`, `g1` `add(3, 4)`, `g2` `add(*[1, 2, 3])`, `g3`
    `scale(5)`, `g4` `log(1, 2, 3)`.

**Person checks** (the owner, at a terminal, in the same shell after the checks)

1. `tentaqles lobe set enforce block --client acme`, answered with yes. `run p1 "add(1, 2)"` prints the note
   and no `permissionDecision`. `tentaqles lobe set enforce default --client acme`, answered with yes.

PASS needs 14 of 14. "Could not run" is a FAIL. The tester reports anything surprising and fixes nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems with `CGO_ENABLED=0`).
- [ ] The Python replay set met both numbers (proposed: 95 in 100 notes right, 0 notes on 1,000 correct
  edits), its result file is committed, and `python` is in the language table.
- [ ] Each of TypeScript with TSX, Go, Java and C# is either in the table with its result file, or out of it
  and named in the changelog. None is in the table without a result.
- [ ] The tester brief passes, 14 of 14, and the owner ran the person check.
- [ ] The house rules hold: `TestLobeArgcheck_NeverDenies`, `TestLobeHookNeverPrintsAllow`,
  `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`, `TestLobeArgcheck_Budget`,
  `TestLobeGuardNeverImportsLobe`, and `lobebench enforce` on the full set still exits 0 (0 wrong blocks).
- [ ] `tentaqles lobe bench latency` was run again with the edit case; no run passed the 1.5 second cutoff.
- [ ] `docs/reference/commands.md`, the bench README and `CHANGELOG.md` are updated.
- [ ] The owner has seen the note described under Result.

## Risks and unknowns

- **The counting tables are new** (G-04 risk). Trail-code counts for Java and Swift, Graphify for C#; the
  rules for Python, TypeScript and Go are ours. That is why no language prints before its replay.
- **Where the replay edits come from is decided here, not in the requirements** (step 13). The owner must
  approve the corpus list and its licenses before task 7. If the owner prefers hand-written sets only, task 7
  shrinks and the number 1,000 cannot be met as written.
- **The numbers** 20 calls, 512 KiB, 3 notes, 300 ms, 700 characters, 95 in 100 and 1,000 are proposed in the
  requirements and stay proposed.
- **Parse time.** The parse is 049's. If 049 took its fallback (a file-wide list with no parse), this check
  prints nothing, and this feature cannot show its result until that is solved.
- **The note comes after the edit**, so the wrong call is already in the file (E-05 risk).
- **Dynamic languages.** A target can be replaced at run time, and a decorator can change a signature; `check`
  covers only what is visible. The replay measures how often the note is wrong.
- **Decided in this brief, not in the requirements:** the table name `enabled` and the session key `argnotes`;
  the words for a range and a variadic target; `rule: argcheck` and the `repo` key of a case; that a note is
  never cut for the list; that the run for a language happens on the branch that switches it on.
- **Index order.** The index lists 047 and 048 as needs. The code needs 049's `ParseEdit` and 041's harness
  too. In number order both exist; the index column is reported as incomplete.
