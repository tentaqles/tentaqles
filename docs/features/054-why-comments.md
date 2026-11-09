# 054: Why-comments

**Status** not started · **Stage** Graph · **Entries** G-07, G-09 step 6 (the notes on the seeds), G-12 step 4
(one `explains` edge) · **Needs** 048, and in fact also 044, 049 and 050 · **Feeds** 057

## Result

A comment that starts with `WHY:`, `NOTE:`, `IMPORTANT:`, `HACK:` or `RATIONALE:` becomes a small node in the
graph, a note, linked to the function, class or file it sits on. When the agent edits that function, the list
it reads after the edit carries the comment, quoted. When a prompt or a search matches the comment's words,
the pointer leads to its line. The reason someone wrote down travels with the code pointer, so an agent that
follows a pointer instead of reading the file still sees the warning.

From the sandbox of [002](002-sandbox.md), with acme's login exported and the `pay.py` fixture of the
Evaluation section built, after `rows[:50]` becomes `rows[:40]` inside `Ledger.post`, the `post-tool-use` hook
prints an answer that holds:

```
WHY comment (pay.py:2): "the bank API rejects batches over 50 rows"
HACK comment (pay.py:5): "slice instead of paging"
```

and `tentaqles lobe graph find "bank batches"` prints a hit of kind `note` at `pay.py:L2-L2`.

## Scope

**In**

- `internal/lobe/graph/extract/notes.go`: `extractNotes(comments, src, symbols) ([]Node, []Edge)` (G-07 steps
  1 to 6).
- The `comment` capture in the `queries/<lang>.tq.scm` file of every first language (G-02 step 4).
- New rows in `schema.go`: the kind `note`, the relation `explains`, the rule `comment`, the node keys `tag`
  and `text`. One more rule function in `check.go`: a note has exactly one `explains` edge (G-12 step 4).
- Notes in the parse cache, and a new extractor version (G-02 step 5, G-08 step 1).
- The notes on the seeds in the list after an edit, on both hosts: up to 2, each as its quoted 80-character
  name (G-09 step 6, proposed), read from the same `EditParse` as the list.
- `note` added to the list of text kinds of [050](050-prompt-injection.md), so a prompt pointer to a note
  prints its text in quotation marks.

**Out**

- Docstrings as notes, and reference nodes for `ADR-12` or `RFC 793` (G-07: not ported; the second is an open
  question there).
- `TODO` and `FIXME` comments. They are tasks, not notes (G-07 step 2).
- Attaching a function's notes to a `find` hit or a prompt pointer for that function. The requirements show a
  note in two places only: the list after an edit of its target (G-09 step 6), and as a hit of its own (R-02
  step 6). The index's line "travels with the pointer to its function" reads wider than that. Reported.
- Notes in the report of `tentaqles lobe graph impact`. G-09's `Report` has no field for them.
- Any path from a note to a lesson or a ladder entry. A note is repository text, so it is data (principle 5).

## What it builds on

- [003](003-shell-failure-claude-code.md): the scrubber of S-05, whose output type the store's writers take.
- [042](042-parser-runtime.md): the grammars and `internal/lobe/graph/parse`.
- [043](043-graph-of-one-repository.md): `graph/extract`, the `.tq.scm` files, the smallest-enclosing-symbol
  lookup, ids and the `~N` suffix, the line shapes with the place of `tag` and `text` after `level`.
- [044](044-graph-soundness.md): `schema.go`, `Check` and the list of rule functions in `check.go`.
- [048](048-graph-stays-current.md): the parse cache and the extractor version.
- [049](049-blast-radius.md): `EditParse` (the tree of the edited file and its symbols), the seeds, the list in
  `blastfmt.go`, the hash kept per session. Its walk already leaves `explains` out by name.
- [050](050-prompt-injection.md): the pointer line and its list of text kinds.
- [046](046-find-and-map.md): the search index. If it keeps no text for a node, this feature adds the note's
  `text` to the index and its build, for the snippet.

## What it leaves for later features

- Note nodes and `explains` edges in every graph. 057 reports them as having no counterpart in Trail-code.
- `extractNotes`, which works on any tree with `comment` captures. A language of
  [060](060-remaining-languages.md) gets notes by adding the capture to its query file.
- The `note` rows in `schema.go` and the one-`explains` rule in `check.go`.

## How it works

1. **Where notes come from** (G-07 step 1). During extraction the builder visits every comment node of the
   parse tree, through the `comment` capture, in every language that has a query file. A tag inside a string
   literal is not a comment node and gives no note.
2. **The tag** (G-07 step 2). Strip the comment marker and test the rest against
   `^(WHY|NOTE|IMPORTANT|HACK|RATIONALE):\s+\S`. Tags are upper case only. A tag with no text is no note. The
   pattern is compiled on first use, never in a package-level variable (core page 5.1).
3. **Text and name** (G-07 step 3). The text is the rest of the line plus the untagged comment lines directly
   below, joined by spaces, scrubbed (S-05), then cut at 400 characters (proposed). The name is the text cut
   to 80 characters on a word boundary. A multi-line `/* WHY: ... */` block is one note; ` * NOTE:` inside a
   doc block counts.
4. **Target** (G-07 step 4), by the first rule that fits. A comment after code on its line goes to the
   innermost symbol whose span holds the line. A block that ends directly above a definition or its
   decorators goes to that definition, also inside a class body. Any other comment goes to the innermost
   symbol whose span holds it. Otherwise the file node.
5. **Output** (G-07 step 5). A node with `"kind":"note"`, the comment's lines as `span`, and the keys `tag`
   and `text`. One edge from the note: `"relation":"explains"`, `"confidence":"extracted"`, `"score":1`,
   `"rule":"comment"`. Decided here: a note gets no `contains` edge; its `explains` edge is its one tie.
6. **Id** (G-07 step 6). `<path>#note:` plus the first 8 hex characters (proposed) of the SHA-256 of tag and
   text (decided here: the bytes of `<tag>: <text>`), so the id survives moving lines. Identical notes in one
   file get the `~N` suffix of G-02.
7. **Soundness.** `check.go` gains the rule function "a note has exactly one `explains` edge": 0 or 2 edges
   give one problem each. The build runs it with the rest of the check before it replaces the graph.
8. **After an edit** (G-09 step 6). The blast step of 049 runs `extractNotes` on the `EditParse` it already
   has, so the notes are the ones in the file as it is now and nothing is parsed twice. It keeps the notes
   whose target is a seed, in line order, at most 2. Each is one line of the list, after the tests line:
   `<TAG> comment (<path>:<line>): "<name>"`, with control characters removed (line form proposed here). It
   is quoted as a comment from the code and never worded as an instruction. Decided here: when the answer is
   over its 700 characters, dependent lines are dropped before a note; and a seed with notes prints the
   header and the notes even when it has no dependent and no test file.
9. **In a prompt and in `find`.** A note is a node like any other for R-01's scoring: its name, its path and
   the terms of its lines. With `note` in 050's text kinds, a prompt pointer prints the name in quotation
   marks and, when the text is longer than the name, the text as a quoted snippet of at most 140 characters.
   `find` prints the note with its kind and its line.
10. **Switches.** Notes are built whenever `graph` is on. The list after an edit and the prompt block show
    one only when `recall` is on (G-07), which 049 and 050 already require.
11. **Hosts.** The list after an edit is the same under `tentaqles lobe-hook claude post-tool-use` and
    `tentaqles lobe-hook codex post-tool-use`; the prompt pointer is the same on both.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-07](../prd/lobe-graph.md), all of it; [G-09](../prd/lobe-graph.md) step 6 and "How to build it" (a note is quoted, never an instruction); [G-12](../prd/lobe-graph.md) step 4 and "How to build it"; [G-02](../prd/lobe-graph.md) steps 4 and 5 and the table of line shapes; [R-02](../prd/lobe-recall.md) step 6; [core page](../prd/vertical-lobe.md) section 3, principle 5, and 5.1 |
| Source to port: the tags | [`graphify/extract.py` L1823](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1823): seven Python prefixes, of which we take five; the width 80 and scrubbing before cutting at L1826 and L1838 of the same file |
| Source to port: node and edge | [`graphify/extract.py` L1928](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1928) and [L1935](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1935): `rationale` and `rationale_for` there, `note` and `explains` here |
| Source: changed | [`graphify/extract.py` L1984](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L1984) (a text test on each line, and the file as target), [`graphify/extract.py` L2552](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L2552) and [L2642](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extract.py#L2642) (the same for JavaScript and TypeScript) |
| Lobe code from earlier features | `internal/lobe/graph/extract`, `graph/lang/queries/*.tq.scm`, `schema.go`, `check.go`, `editparse.go`, `blastfmt.go`, `internal/lobe/recall/format.go`, `internal/lobe/search` |
| Existing tentaqles code | `NOTICE` (Graphify is listed since phase 1); the test that checks the `// Ported from` header |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: what `EditParse` exposes of
   the tree, how `blastfmt.go` orders and cuts the list, whether the index keeps text for a node, the
   text-kinds list. Write the Codex edit JSON of Evaluation check 7 into this file. The owner approves
   changes, and the decisions of steps 5, 6 and 8, before code is written.
2. **Confirm comment nodes.** A throwaway test parses a three-line file with one tagged comment in each first
   language and lists the languages whose grammar exposes no comment node (G-07 risk). Such a language gets
   no notes; write the list into Risks and the changelog line.
3. **`notes.go`** (side by side with 4): steps 2 to 6. Tests: `TestLobeNoteTags`, `TestLobeNoteTarget`,
   `TestLobeNoteIgnoresStrings`, `TestLobeNoteIDStable`, `TestLobeNoteScrubbed`, `TestLobeNoteBlocks`.
4. **The `comment` capture** in each `.tq.scm`. Test: `TestLobeGraphQueriesCompile` (the capture is read).
5. **Into the build**: the rows in `schema.go`, the rule function in `check.go`, notes in the cache record,
   the new extractor version, the note's text in the index. Tests: `TestLobeNoteOneExplains`,
   `TestLobeNoteNeverALesson`, `TestLobeGraphIncrementalEqualsCold` and `TestLobeGraphDeterministic` with
   tagged comments in their fixture.
6. **After an edit and in a prompt**: the note lines in `blastfmt.go`, the cut order, `note` in the text
   kinds. Tests: `TestLobeNoteOnSeedsAfterEdit`, `TestLobeNoteQuotedNeverInstruction`,
   `TestLobePromptQuotesTextNodes` with a real note, `TestLobeBlastOneParse` unchanged (still 1 parse).
7. **Docs.** A paragraph on notes in the `tentaqles lobe graph` section of `docs/reference/commands.md` and
   in the `post-tool-use` rows of the `tentaqles lobe-hook` section; one line in `CHANGELOG.md` under
   `[Unreleased]`. Every ported Go file starts with its `// Ported from` line. The plugin does not change.
8. **Tester run** of the Evaluation section by a fresh agent.

## Tests that stay in the repository

- `TestLobeNoteTags`: 5 tags in 5 comment styles give 25 notes; `# TODO:` and `// FIXME:` give 0.
- `TestLobeNoteTarget`: notes after code, above a method in a class body, loose in a body and at the top of a
  file get the 4 expected targets.
- `TestLobeNoteIgnoresStrings`: a tag inside a string literal gives 0 notes.
- `TestLobeNoteIDStable`: 10 lines inserted above leave every note id unchanged.
- `TestLobeNoteScrubbed`: a note holding a secret that S-05 masks is stored without it.
- `TestLobeNoteNeverALesson`: after a build, `lessons/` holds 0 files and `ladder.json` gains 0 entries.
- `TestLobeNoteOneExplains`: a graph where a note has 0 or 2 `explains` edges gives 1 problem each in the
  check.
- New names in this brief: `TestLobeNoteBlocks` (a multi-line block comment is 1 note; ` * NOTE:` in a doc
  block counts; a tag with no text gives 0; a lower-case tag gives 0; a text of 600 characters is stored as
  400 and named with at most 80), `TestLobeNoteOnSeedsAfterEdit` (3 notes on the edited method print 2, in
  line order, quoted, after the tests line; a note on another method prints 0; a seed with notes and no
  dependent still prints; with `recall: off` 0 bytes), `TestLobeNoteQuotedNeverInstruction` (a note whose
  text holds a line break, a control character and the words of F-05's frame prints on one line inside
  quotation marks with the control character removed, and the answer holds no frame line).
- From earlier features, kept green: `TestLobeBlastOneParse`, `TestLobeBlastNotesBeforeList` (E-05's notes
  still come first), `TestLobeBlastCap`, `TestLobeGraphSoundFixtures`, `TestLobeEveryInjectionIsFramed` (a
  list with comments holds no lesson text), `TestLobePromptBudget`.
- House rules: `TestLobeGuardNeverImportsLobe` (the tag pattern is not a package-level call),
  `TestLobeImportsOnlyTheFour`, `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeBlastCutoffKeepsCapture`, `TestNoticeListsEveryPort`, and every test under "Must not trade off" in
  `CLAUDE.md`, unedited.

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
S="$W/clients/acme/shop"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; git init -q "$S"; cd "$S"
printf '%s\n' 'class Ledger:' '    # WHY: the bank API rejects batches over 50 rows' \
  '    def post(self, rows):' '        text = "# NOTE: not a comment"' \
  '        return rows[:50]  # HACK: slice instead of paging' '# TODO: remove after migration' > pay.py
tentaqles lobe graph build || echo "SETUP FAIL build"
J="$(ls -d "$L"/graph/r_*)/graph.jsonl"
export CLAUDE_CONFIG_DIR="$I/acme/claude"
edit() {  # edit <session>: the Edit payload for pay.py, piped to the Claude Code hook
  printf '{"session_id":"%s","hook_event_name":"PostToolUse","cwd":"%s","tool_name":"Edit","tool_input":' "$1" "$S"
  printf '{"file_path":"%s/pay.py","old_string":"rows[:50]","new_string":"rows[:40]"}}' "$S"; }
hook() { edit "$1" | tentaqles lobe-hook claude post-tool-use; echo " exit=$?"; }
ask() {  # ask <host> <session> <prompt>: the prompt hook under acme's login for that host
  v=CLAUDE_CONFIG_DIR; [ "$1" = codex ] && v=CODEX_HOME
  printf '{"session_id":"%s","cwd":"%s","prompt":"%s"}' "$2" "$S" "$3" |
    env "$v=$I/acme/$1" tentaqles lobe-hook "$1" user-prompt-submit; echo " exit=$?"; }
```

`pay.py` has exactly 6 lines. Run every check in `$S`. A tester never runs `tentaqles lobe set`.

1. `J` has exactly 2 lines with `"kind":"note"`: one with `"span":[2,2]` and `"tag":"WHY"`, one with
   `"span":[5,5]` and `"tag":"HACK"`. Exactly 2 lines contain
   `"relation":"explains","target":"pay.py#Ledger.post"`, each with `"confidence":"extracted","score":1,
   "rule":"comment"`.
2. `grep -c -E 'not a comment|remove after' "$J"` prints `0`.
3. `tentaqles lobe graph check --json --strict`: exit 0 and `"ok":true`.
4. `tentaqles lobe graph find "bank batches"`: exit 0 and one hit line contains `· note` and `pay.py:L2-L2`.
5. Change `rows[:50]` to `rows[:40]` in `pay.py`, then `hook s1`: ` exit=0`; `additionalContext` contains
   `the bank API rejects batches over 50 rows`, `slice instead of paging`, `pay.py:2` and `pay.py:5`, each
   comment between `\"` marks as JSON escapes them; it does not contain `not a comment`, `remove after` or
   `not instructions`; stdout holds no `permissionDecision`.
6. `hook s1` again: nothing is printed before ` exit=0`.
7. Codex. `export CODEX_HOME="$I/acme/codex"` and pipe the Codex edit JSON to
   `tentaqles lobe-hook codex post-tool-use`: exit 0 and the output contains
   `the bank API rejects batches over 50 rows`. The Codex edit JSON is the `apply_patch` payload for session
   `c1` and folder `$S` with one file header, `pay.py`, and one hunk that replaces `rows[:50]` by
   `rows[:40]` on line 5. Build task 1 writes it here in full.
8. `lobeset acme recall off`, then `hook s2`: nothing before ` exit=0`. `lobeset acme recall on`.
9. `ask claude n1 "which batches over 50 rows does the bank API take"`: ` exit=0`; `additionalContext`
   contains `pay.py:L2` and `the bank API rejects batches over 50 rows` between `\"` marks.
   `ask codex n2 "which batches over 50 rows does the bank API take"`: the output contains `pay.py:L2`.
10. Note the 2 note ids in `J`. Insert two empty lines at the top of `pay.py` and run
    `tentaqles lobe graph build`. `J` holds the same 2 ids, now with `"span":[4,4]` and `"span":[7,7]`.
11. `ls "$L/lessons" 2>/dev/null | wc -l` prints `0`, `grep -c note "$L/ladder.json" 2>/dev/null` prints `0`
    or nothing, and `ls "$I"/globex/lobe/graph 2>/dev/null | wc -l` prints `0`.

There are no person checks. PASS needs 11 of 11. "Could not run" is a FAIL. The tester reports anything
surprising and fixes nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems with `CGO_ENABLED=0`).
- [ ] The tester brief passes, 11 of 11.
- [ ] The house rules hold: `TestLobeNoteNeverALesson`, `TestLobeNoteScrubbed`,
  `TestLobeNoteQuotedNeverInstruction`, `TestLobeHookAlwaysExitsZero`, `TestLobeBlastCutoffKeepsCapture`,
  `TestLobeGuardNeverImportsLobe`, and `tentaqles lobe bench leak` at zero leaks.
- [ ] `tentaqles lobe bench latency` was run again with the edit case; no run passed the 1.5 second cutoff.
- [ ] The languages without comment nodes, if any, are named in the changelog line.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a why-comment arrive with the list after an edit, as described under Result.

## Risks and unknowns

- **Comment nodes per grammar** (G-07 risk). Unknown until task 2. A language without them gets no notes.
- **Planted notes.** Anyone who can commit can plant a note to steer the agent. It is shown quoted and capped,
  never as a rule, and never inside F-05's frame, which is for lessons.
- **Decided in this brief, not in the requirements:** no `contains` edge for a note; the bytes hashed for the
  id; the line form of a note in the list; that dependent lines are cut before notes; that a seed with notes
  prints without dependents. The owner approves them at task 1.
- **"Travels with the pointer to its function."** The requirements have no step that adds a function's notes
  to a `find` hit or a prompt pointer for that function (see Out). If the owner wants it, it is a change to
  R-02 or R-03 first, then a task here.
- **A trap marked only `FIXME` or `TODO`** is not a note and does not travel (G-07 risk).
- **The numbers** 400 and 80 characters, 8 hex characters and 2 notes per list are proposed in the
  requirements and stay proposed.
- **Budget.** Two notes of 80 characters take about a third of the 700 characters of the answer after an
  edit. With E-05's notes in front, the dependent lines are the first to go; M-01 (058) will show how often.
