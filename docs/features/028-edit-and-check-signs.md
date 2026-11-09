# 028: Undone edits and failed-then-passed checks

**Status** not started · **Stage** Lessons · **Entries** C-02 (steps 4 (b), 5, 6 and 7: the `undone` and `fixed`
signs), C-01 (step 4, the hand-over of an identical retry after an edit), C-03 (the sign input) ·
**Needs** 027 · **Feeds** 035, 036

## Result

Two more kinds of evidence produce candidates after a turn. When the agent edits a file and then runs `git
restore` on it, the pass records an `undone` sign. When a check fails, a file is edited, and the same check then
passes, it records a `fixed` sign. Both sit in `candidates.jsonl`, waiting to be worded as lessons. An echoed
`git restore` and a plain flake record nothing.

```sh
tentaqles lobe consolidate --session s4 --only signs --json   # "sign":"undone","files":["a.go"]
tentaqles lobe consolidate --session s6 --only signs --json   # "sign":"fixed","check":"go test"
```

## Scope

**In**

- The `undone` sign (C-02 step 5): one of four git undo forms, run as a real command, covering a file that an
  edit event of this session touched.
- The `fixed` sign (step 6): C-01 hands over a failure and an identical later `ok` call with an edit between.
- Evidence (b) for a correction (step 4): an undo after the flagged prompt of a file edited before it.
- The cap of 3 signs per session across the three kinds (step 7), and the rule that a `fixed` sign is drafted
  only once its fingerprint was fixed in 2 sessions (proposed).
- Wording these candidates through the model path of 025 (C-03 steps 2 and 5), with `source: repair`.

**Out**

- A template for these signs. C-03 step 7 has none: a candidate of these kinds waits for the model.
- An edit that reverses an earlier one by hand. S-03 stores only the file path of a successful edit, so only
  the four git forms count (C-02 Risks). No feature takes it.
- Evidence (c), a declined or interrupted call (not in the first build; see 027).
- The M-04 count of `fixed` signs that were never drafted. The lines stay in `candidates.jsonl` until they
  expire; the benchmark of feature 022 is not changed here.
- These signs from past sessions (feature 035) and from Codex (feature 036).

## What it builds on

- 004, 008: edit events. An `ok` event of an edit tool keeps the file path as its projection (S-03).
- 006, 009: the failure's fingerprint and `kind` (`fail`, `hidden-fail`), which alone say whether a call failed.
- 018: `pair.go` and `attempt.go`. Today an identical retry with an edit between is dropped with the reason
  `edit-between` (C-01 step 4, `TestLobePairDrops`); the C-01 key (`go test`) names the check.
- 019: `consolidate.File(candidate, lesson, evidence)`, the one path of a machine draft.
- 025: `Ask` and `Check` in `draft.go`, `child.go`, `--draft model` and `--draft file:<path>`.
- 027: `signs.go` with the `Sign` shape and the cap, the `corrected` sign with evidence (a) and (d), `--only
  signs`, the `sign` action line, and the rule that a correction takes over what serves as its evidence.
- 020: the lesson shown right after a failure with a listed fingerprint, used here to watch a `fixed` lesson.

## What it leaves for later features

- The candidate kinds `undone` and `fixed` in `candidates.jsonl`, and the `sign` action line with `files` and
  `check`. 035 produces the same signs from transcripts; 036 from Codex events.
- Evidence (b) inside the `corrected` rules of 027.
- Nothing new in the store layout, the hooks or the settings.

## How it works

1. **Where it runs.** Only in the detached pass and in `tentaqles lobe consolidate`. No hook changes. With
   `lessons` off nothing runs. A session whose file holds `"tampered":true` is skipped, as in 027.
2. **`undone`** (step 5). An `ok` event of a shell tool whose command, split into statements and words by
   `internal/lobe/shellwords`, runs `git restore`, `git checkout -- <path>`, `git revert` or `git reset --hard`
   as a real command, outside quotes. It counts when it covers a file that an earlier edit event of this
   session touched. `git restore` and `git checkout --` cover the paths they name; a folder covers the files
   under it. `git revert` and `git reset --hard` name no file and cover every file the session edited in that
   repository (this file's reading; the requirements say only "covering a file"). The sign records `files`:
   the covered files, relative to the repository, or to the working folder outside one.
3. **`fixed`** (step 6). `pair.go` no longer throws away an identical retry that has an edit event between the
   failure and the `ok` call: the drop with the reason `edit-between` is handed to `signs.go`. It is still not
   a pair. The sign records `check` (the
   C-01 key), `files` (the files edited between the two calls) and `fp` (the failure's fingerprint). Whether a
   call failed comes from the event's `kind`, never from a second set of text patterns. `--only signs` computes
   the pairs in memory for this.
4. **Evidence (b).** An `open` flag (027) also becomes a `corrected` sign when an undo (step 2) comes after the
   prompt and covers a file edited before it. That undo is then the correction's evidence and is not recorded
   as an `undone` sign of its own (this file's choice, the same as 027 makes for a pair). A correction whose
   only evidence is (b) fits no template and waits for the model.
5. **Cap and order.** All signs of a session are kept in time order, and at most 3 become candidates, oldest
   first, whatever their kinds. Each is one line of `candidates.jsonl` with `kind` `undone` or `fixed` and
   `state` `pending`.
6. **Two sessions for `fixed`.** A `fixed` candidate is drafted only once `candidates.jsonl` holds `fixed`
   lines with the same `fp` from 2 different sessions (proposed). The line that completes the count is drafted,
   and the earlier pending lines with that `fp` are closed as `skipped` (this file's choice). Until then a line
   stays `pending` and costs nothing.
7. **Wording.** Through the model only (C-03 step 1 decides whether a model may be used). The input is the
   sign's kind and the inputs of the events it names, inside the two marker lines, at most 6,000 characters
   (proposed). The lobe sets `kind: lesson`, `source: repair`, `origin.drafted: model`, and for a `fixed` sign
   `when.fingerprint` with its `fp`. A model answer's `when.command` and `when.words` must be grounded in those
   inputs. With `model: off`, or past a limit, the candidate stays `pending` until its line expires with
   `retention_days`.
8. **The gate.** A lesson with `source: repair` needs a `when` entry or a `touches` entry (C-04, `form`). A
   `fixed` lesson always has its fingerprint. An `undone` lesson has only what the model answered: an answer
   with an empty `when` fails `form`, and the draft stays in quarantine.
9. **Entering.** A filed lesson enters at `advice` like any repair lesson (D-9) and starts at strength 0.5.

## Where to read

| What | Where |
|---|---|
| Requirements | [C-02](../prd/lobe-consolidate.md), steps 4 to 7, "How to build it", the tester checks 3 and 4, Risks; [C-01](../prd/lobe-consolidate.md), steps 3 and 4 and the table's Shell row (the key); [C-03](../prd/lobe-consolidate.md), steps 1, 2, 5 and 7 (last sentence); [C-04](../prd/lobe-consolidate.md), the `form` and `grounded` rows; [core page 5.4](../prd/vertical-lobe.md) |
| Source to port | [`src/claude/note-signals.ts` L97](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L97): the four git undo forms; [L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L42): 3 per session; [`src/trace/repairs.ts` L199](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/repairs.ts#L199): an edit between the calls means no repair |
| Source, weakness we avoid | [`src/claude/note-signals.ts` L259](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L259): "0 failed" anywhere in the output cancels a failure line. We read the event's `kind` |
| Existing tentaqles code | `internal/lobe/consolidate/pair.go`, `attempt.go`, `signs.go`, `draft.go`; `internal/lobe/shellwords` |

## Build tasks

1. **Check this brief against the code.** Read `pair.go`, `signs.go` and `draft.go` as 018, 025 and 027 left
   them. List what no longer fits: how an edit event names its file, how 027 settled the order of events and
   prompts, and whether the draft step already accepts a sign. The owner approves changes first.
2. **`undone`.** The four git forms on split words; the files they cover; the sign. Files: `signs.go`,
   `signs_test.go`. Proof: `TestLobeSignUndoIsParsed`.
3. **`fixed`** (side by side with 2). The hand-over from `pair.go`; the sign. Proof: `TestLobeSignFixedFromRetry`
   and `TestLobePairDrops`, which still counts 0 pairs.
4. **Evidence (b) and the shared cap.** Proof: `TestLobeSignCorrectedByUndo`, `TestLobeSignCapAndCost` widened
   to mixed kinds.
5. **Drafting.** The sign input, `source: repair`, the two-session rule. Files: `draft.go`, `run.go`. Proof:
   `TestLobeSignFixedNeedsTwoSessions`, `TestLobeDraftSourceAndOrigin` widened to `undone` and `fixed`.
6. **Docs.** `docs/reference/commands.md`: the `sign` values under `tentaqles lobe consolidate`.
   `CHANGELOG.md`. The plugin does not change.
7. **Tester run.**

## Tests that stay in the repository

- `TestLobeSignUndoIsParsed`: `git restore a.go` after an edit of `a.go` gives 1 sign; `echo "git restore
  a.go"` gives 0; `git restore b.go` after an edit of `a.go` gives 0; `git reset --hard` after an edit gives 1.
- `TestLobeSignFixedFromRetry` (new): fail, edit, the identical call passes gives 1 `fixed` sign with `check`,
  `files` and `fp`; fail and the identical call passes with no edit gives 0 signs and 0 pairs (a flake).
- `TestLobePairDrops`: unchanged in what it counts, 0 pairs for each of its cases.
- `TestLobeSignCorrectedByUndo` (new): an edit, a flagged prompt, then `git restore` of that file gives 1
  `corrected` sign and 0 `undone` signs; the same with no prompt gives 1 `undone` sign.
- `TestLobeSignCapAndCost`: 5 signs of mixed kinds in one session give 3 candidates, the 3 oldest.
- `TestLobeSignFixedNeedsTwoSessions` (new): one session's `fixed` sign starts 0 drafts; the same fingerprint
  fixed in a second session starts exactly 1, for the second session's line, and the first closes as `skipped`.
- `TestLobeDraftSourceAndOrigin`: `undone` and `fixed` candidates file with `source: repair`, never `model`.
- House rules. No hook changes, so the stall, login and guard tests stay green untouched:
  `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`, `TestLobeGuardNeverImportsLobe`. No model
  in a hook, and no model without leave: `TestLobeDraftStartsNothing` still gives 0 children. A tampered
  session teaches nothing: `TestLobeSignSkipsTamperedSession` (new) sets `"tampered":true` in a session file by
  hand and gets 0 signs of any kind.

## Evaluation

The tester gets the built `tentaqles` first on `PATH` and an empty working folder. Use bash (Git Bash on
Windows). No network; the sandbox switches `model` off and no check switches it on. Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
I="$TQ_HOME/identities"; A="$W/clients/acme"; cd "$A"; export CLAUDE_CONFIG_DIR="$I/acme/claude"
H() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"%s",' "$1" "$A" "$2"; }
PROMPT() { { H "$1" UserPromptSubmit; printf '"prompt":"%s"}' "$2"; } |
  tentaqles lobe-hook claude user-prompt-submit; echo " exit=$?"; }
OK() { { H "$1" PostToolUse; printf '"tool_name":"Bash","tool_input":{"command":"%s"},' "$2"
  printf '"tool_response":{"stdout":"ok","stderr":""}}'; } | tentaqles lobe-hook claude post-tool-use; }
EDIT() { { H "$1" PostToolUse; printf '"tool_name":"Edit","tool_input":{"file_path":"%s/%s",' "$A" "${2:-a.go}"
  printf '"old_string":"x","new_string":"y"},"tool_response":{}}'; } | tentaqles lobe-hook claude post-tool-use; }
FAIL() { { H "$1" PostToolUseFailure; printf '"tool_name":"Bash","tool_input":{"command":"%s"},' "$2"
  printf '"error":"%s"}' "$3"; } | tentaqles lobe-hook claude post-tool-use-failure; }
SIGNS() { tentaqles lobe consolidate --session "$1" --only signs --json | grep '"action":"sign"'; }
C="$I/acme/lobe/candidates.jsonl"; LL="$I/acme/lobe/lessons"
```

No line may start with `SETUP FAIL`. Every hook call must exit 0.

1. `EDIT s4`, `OK s4 "git restore a.go"`, `SIGNS s4`: 1 line, with `"sign":"undone"` and `"files":["a.go"]`.
2. `EDIT s5`, `OK s5 'echo \"git restore a.go\"'`, `SIGNS s5`: 0 lines. The command is only quoted text.
3. `FAIL s6 "go test ./..." "--- FAIL: TestA"`, `EDIT s6`, `OK s6 "go test ./..."`, `SIGNS s6`: 1 line, with
   `"sign":"fixed"`, `"check":"go test"` and `"files":["a.go"]`. Then `tentaqles lobe consolidate --session s6
   --only pair --json | grep -c '"action":"pair"'` prints `0`: it is a sign, not a pair.
4. A flake is nothing. `FAIL s7 "go test ./..." "--- FAIL: TestA"`, `OK s7 "go test ./..."`, `SIGNS s7`: 0
   lines.
5. An undo as the proof of a correction. `PROMPT s8 start`, `EDIT s8`, `PROMPT s8 "no, that file is
   generated"`, `OK s8 "git restore a.go"`, `SIGNS s8`: 1 line, with `"sign":"corrected"`, and no line with
   `"sign":"undone"`.
6. The cap. `for f in b c d e; do EDIT s9 $f.go; OK s9 "git restore $f.go"; done`, then `SIGNS s9`: 3 lines,
   and none names `e.go`. Four different files are used because the duplicate filter (007) folds identical
   calls.
7. One session is not enough for a `fixed` lesson. Write `reply.json` in `$A`: `{"skip":false,"title":"Update
   the fixture before running the Go tests","do":"Edit the fixture, then run the tests.","avoid":"Running the
   tests against the old fixture.","why":"TestA fails because the fixture no longer matches the code.",
   "when":{"command":[],"place":[],"words":[]}}`. Run `tentaqles lobe consolidate --session s6 --draft
   file:reply.json --json`: exit 0 and no line with `"action":"file"`.
8. Two sessions are. `FAIL s10 "go test ./..." "--- FAIL: TestA"`, `EDIT s10`, `OK s10 "go test ./..."`, then
   `tentaqles lobe consolidate --session s10 --draft file:reply.json --json`: a line with `"by":"model"` and
   exactly one line with `"action":"file"`. That file in `$LL` has the title above, `source: repair`,
   `drafted: model` and a `fingerprint:` entry under `when`. `grep -c '"kind":"fixed"' "$C"` prints `2`, and
   `grep '"kind":"fixed"' "$C" | grep -c '"state":"pending"'` prints `0`.
9. The lesson answers the failure. `FAIL s11 "go test ./..." "--- FAIL: TestA"` prints an `additionalContext`
   that contains `Update the fixture before running the Go tests` and `not instructions`.
10. With `model` off, the `undone` candidate of check 1 is still waiting: `grep '"kind":"undone"' "$C" | grep
    -c '"session":"s4"'` prints `1`, on a line with `"state":"pending"`, and `$LL` holds one file.

There are no person checks: nothing in this feature answers only to a person.

PASS needs 10 of 10. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI: `go vet ./...`, `go test ./... -count=1`,
  `go build ./...`.
- [ ] The tester brief passes 10 of 10.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, `TestLobeDraftStartsNothing`, and every test
  under "Must not trade off" in `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen an `undone` and a `fixed` sign from the commands under Result.

## Risks and unknowns

- **What an `undone` lesson is keyed on.** C-03 names no key the lobe sets for it: no fingerprint, no command
  key, and nothing writes `touches` on a machine draft. Such a lesson files only when the model answers with
  a grounded `when` block. If the owner wants the sign's `files` written as `touches`, that is a change to
  C-03 and to this file.
- **What `git revert` and `git reset --hard` cover** is this file's reading (step 2). The pass runs no git
  command, so it cannot know which files a revert really changed.
- The two-session count reads `candidates.jsonl`, whose lines expire with `retention_days` (14 by default). A
  check that is fixed once a month never reaches 2. The figure is proposed; M-04 should say.
- Which `fixed` line is drafted (the one that completes the count), and that the earlier ones close as
  `skipped` (step 6), is this file's choice.
- An edit is seen only as a file path. Two edits of one file, the second undoing the first by hand, look like
  two ordinary edits.
- The `fixed` sign trusts S-04: a check that still fails but is read as `ok` gives a wrong sign. The gate and
  the two-session rule limit the cost.
- What forces a change to this file: an edit event that does not keep its file path; a 027 that orders
  prompts and events in a way step 4 cannot use.
