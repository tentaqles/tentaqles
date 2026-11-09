# 050: Code pointers with a prompt

**Status** not started · **Stage** Graph · **Entries** R-02, M-06 (the prompt row with a graph) · **Needs** 049,
and in fact 017, 027, 032, 036, 046 and 048 · **Feeds** 053, 054, 055, 058, 059

## Result

The person sends a prompt that names code or uses a phrase a lesson lists. Before the agent starts, it reads
one small block: up to 2 lessons and up to 3 pointers, each pointer a name, a file and a line span, never the
code itself. A prompt such as "ok" or "thanks, that looks good to me" adds nothing, and costs nothing.

From the sandbox of [002](002-sandbox.md), with acme's login exported and the fixture of the Evaluation
section built:

```bash
printf '{"session_id":"s1","cwd":"%s","prompt":"fix the empty input bug in ParseSpan"}' "$R" |
  tentaqles lobe-hook claude user-prompt-submit
```

prints one JSON object whose `additionalContext` is:

```
[tentaqles lobe] Code that matches this prompt:
1. ParseSpan · function  lib/span.go:L3-L3
   func ParseSpan(s string) (int, int)
```

The same prompt sent again in session `s1` prints nothing. A prompt with the words `merge into` prints the
lesson that lists that phrase, inside the frame of F-05.

## Scope

**In**

- `internal/lobe/recall/prompt.go`: `Prompt(c Client, in HookInput, deadline time.Time) string`, all eight steps
  of R-02.
- The R-02 step of the `user-prompt-submit` event on both hosts. The hook entry and the event exist since
  [027](027-correction-sign.md) (Claude Code) and [036](036-lessons-in-codex.md) (Codex).
- Lessons reached by a `when.words` phrase, and lessons reached because their `touches` names a file among the
  pointers.
- Pointers from `search.Search` with the coverage gate, and the rule that a weak match prints nothing.
- The seen filter before the cap, stale lessons behind fresh ones, the cut to 2 and 3, the 1,500-character
  budget (proposed), and the session keys `shown` and `pointers`.
- The pointer line and its snippet, including the rule for text someone wrote: the snippet of a note or
  document node prints in quotation marks with control characters removed (R-02 step 6). No such node exists
  yet; the rule is built and tested here on hand-made hits.
- The `user-prompt-submit` row of `tentaqles lobe bench latency` run with a prompt that matches code.

**Out**

- The M-01 line in `stats/` for each printed block ([058](058-token-savings.md)).
- Heading, key and block nodes ([053](053-docs-and-config.md)) and note nodes ([054](054-why-comments.md)).
- The `file#Symbol` check of a stale lesson through the graph ([055](055-stale-by-symbol.md)). Here `Gone` is
  called as [032](032-stale-lessons.md) built it.
- Pointers into another repository of the client. No hook crosses repositories (G-09, G-10); `find --all` is
  [056](056-many-repositories.md).
- C-02's correction flag in the same process. It stays as 027 built it.
- The sentence of the session brief that names the graph verbs (`Code graph: <K> symbols; query it with
  tentaqles lobe graph find, callers, outline, map or impact`, R-05 step 4). The index gives it to no feature.
  It matters here, because R-02 stays silent on a weak match and relies on that sentence to tell the agent
  the verbs exist. Reported to the owner; not built in this feature.

## What it builds on

- [002](002-sandbox.md): the sandbox and `lobeset`.
- [014](014-starter-rule-before.md): `internal/lobe/safety` with `safety.Wrap`, the only function that puts
  lesson text into an `Answer`.
- [016](016-lesson-by-hand.md): the lesson file, `tentaqles lobe lessons gate` and `lessons add`.
- [017](017-lesson-before-call.md): `internal/lobe/recall` with `trigger.go` (the matcher, eligibility by R-04
  step 1, `applies`), `session.go` (`sessions/<session>.json`, the keys `shown` and `pointers`, newest 40),
  `format.go` (the lesson line of 300 characters), and the header cache in `index/`. If 017 left the
  whole-word matcher for `when.words` out, because nothing read it, this feature adds it to `trigger.go`.
- [026](026-session-brief.md): standing lessons printed in the brief are in `shown`, so they are not repeated.
- [027](027-correction-sign.md): the `UserPromptSubmit` entry in `plugin/hooks/hooks.json`, the
  `user-prompt-submit` event in `internal/lobe/hook`, and C-02's flag step in it.
- [032](032-stale-lessons.md): `stale.go` (`Gone`), the stale note and "stale after fresh".
- [034](034-teammate-lesson-intake.md): shared lessons that `intake.json` lists as `ok` for this repository.
- [036](036-lessons-in-codex.md): the Codex `UserPromptSubmit` handler and the Codex output form.
- [046](046-find-and-map.md): `search.Search(ix, query, opt)`, `Options{Limit, In, Deadline}`, `Result{Mode,
  Hits, Coverage, CoverageStrong, Note}`, `Hit{Name, Kind, Path, Start, End, Score}`, and the index file.
- [047](047-signatures.md): `sig`, the header line a pointer shows as its snippet.
- [048](048-graph-stays-current.md): the index rewritten after each build, its build id, the dirty markers.
- [012](012-hook-latency-and-stall.md): `tentaqles lobe bench latency`.

## What it leaves for later features

- `recall.Prompt`, the one function behind the prompt block. 058 appends its stats line after the print.
- The pointer line in `format.go`: `<n>. <name> · <kind>  <path>:L<start>-L<end>`, a snippet line of at most
  140 characters, and the quoting rule by node kind. 053 and 054 only add their kinds to its list.
- The session key `pointers`: the node ids of the newest 40 pointers shown (shape proposed here).
- The latency row with a graph. 058 and 059 read its figures.

## How it works

1. **Where it runs.** `tentaqles lobe-hook <host> user-prompt-submit`, inject class: 300 ms at the 95th
   percentile (proposed), cut off at 1.5 seconds (core page 5.2). The payload fields read are `session_id`,
   `cwd` and `prompt`. C-02's flag step keeps the place 027 gave it; this step is added to the same row of
   `steps.go`. No step of this feature starts after 1.0 second (proposed).
2. **Silence** (R-02 step 1). Nothing is printed when F-02's client rule fails, when `recall` is off, when the
   prompt starts with `<task-notification>`, or when the trimmed prompt has fewer than 3 characters.
3. **Lessons by words** (R-02 step 2). Each eligible lesson (R-04 step 1: `recall` and `lessons` on, `enforce`
   not `off`, stage `advice` or `block`, `applies` counts here) is tested with the whole-word matcher on
   `when.words`: the phrase as whole words, case ignored, with letters, digits and `_` as word characters.
   Order: phrases found, then `strength`, then newest `created`. This step never waits for the search.
4. **Pointers** (R-02 step 3). Only when `graph` is on, the prompt has 12 or more characters and the index of
   `cwd`'s repository is usable (its build id equals the graph's). `Search(prompt, Limit: 3)` with the hook's
   deadline. A `lexical` result is shown only when `coverage_strong >= 0.1` or `coverage >= 0.5`; a
   `structural` result always. Anything weaker prints no pointer and no line that tells the agent to search.
   The hook reads the index and never `graph.jsonl`.
5. **Lessons by file** (R-02 step 4). An eligible lesson of this repository whose `touches` names the file of
   one of those hits joins after the lessons of step 3. A private lesson counts when its `repo` is this
   repository's key; a shared one when its file sits in this repository.
6. **Seen, stale, cut** (R-02 step 5). Drop the lesson ids in `shown` and the node ids in `pointers`. Call
   `Gone` for the lessons that are left and move the stale ones behind the fresh ones. Only then keep 2
   lessons and 3 pointers.
7. **Format** (R-02 step 6). The lessons go through `safety.Wrap`, each as R-04's lesson line, with R-06's note
   when stale. Then the line `[tentaqles lobe] Code that matches this prompt:` and per hit
   `<n>. <name> · <kind>  <path>:L<start>-L<end>` with a snippet line under it: the node's `sig`, cut at 140
   characters. For a node whose kind is in the list of text kinds (empty in this feature) the name and the
   snippet are text someone wrote: each prints in quotation marks with control characters removed, and a
   snippet that only repeats the name is left out. A pointer whose file a dirty marker names
   gains ` (may be stale)` (G-08 step 7). With lessons only there is no pointer header; with pointers only
   there is no frame. The block has at most 1,500 characters, frame included (proposed); pointers drop from
   the end until it fits.
8. **Print and record** (R-02 step 7). The block is the step's `Answer.Context`; `Main` prints it in the
   host's form, for Claude Code
   `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"<block>"}}`. With nothing to
   say the hook prints zero bytes. After the print the step records the lesson ids and the pointer node ids,
   newest 40 each, with `Update` under the store lock; a busy lock skips the write and never the print.
9. **Edge cases** (R-02 "How to build it"). No index, an index whose build id differs, `graph: off` or a search
   that returns after the 1.0 second stop gives lessons only. `lessons: off` or `enforce: off` leaves no
   lesson eligible, so only pointers print. An unreadable session file counts as empty.
10. **Codex.** The same step runs under `tentaqles lobe-hook codex user-prompt-submit` with `CODEX_HOME` as the
    login. The text is the same; the envelope is the one 036 fixed.
11. **Differences from Trail-code, kept on purpose.** A lesson reaches a prompt only by its own phrase or a
    shared file, never by one title word. The seen filter runs before the cap. A weak match costs zero
    characters. The search runs in process, not in a child.

## Where to read

| What | Where |
|---|---|
| Requirements | [R-02](../prd/lobe-recall.md), all of it; [R-01](../prd/lobe-recall.md) steps 5 to 7 and "How to build it" (the index, `Deadline`); [R-04](../prd/lobe-recall.md) steps 1, 3, 6 and 9 and the session record; [R-06](../prd/lobe-recall.md) steps 5 and 6; [F-05](../prd/lobe-foundation.md) step 4; [F-02](../prd/lobe-foundation.md) step 2 (the `user-prompt-submit` row) and step 6; [G-08](../prd/lobe-graph.md) step 7; [core page](../prd/vertical-lobe.md) 5.2, 5.4 and 5.6 |
| Source to port: floors and counts | [`src/claude/prompt-hook.ts` L37](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L37) (3 characters), [L32](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L32) (12 characters), [L229](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L229) (3 hits), [L262](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/prompt-hook.ts#L262) (task notices) |
| Source to port: gate and format | [`src/claude/format.ts` L333](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L333), [`src/ask/fuse.ts` L100](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/ask/fuse.ts#L100), [`src/claude/format.ts` L236](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L236), [L118](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L118), [L289](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L289) |
| Source: weaknesses changed | [`src/notes/find-notes.ts` L57](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/find-notes.ts#L57), [L60](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/find-notes.ts#L60), [`src/claude/format.ts` L132](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L132) |
| Web, not machine-checked | <https://code.claude.com/docs/en/hooks>: the host caps `additionalContext` at 10,000 characters |
| Lobe code from earlier features | `internal/lobe/recall/trigger.go`, `session.go`, `format.go`, `stale.go`; `internal/lobe/search`; `internal/lobe/safety/frame.go`; `internal/lobe/hook/steps.go` |
| Existing tentaqles code | `plugin/hooks/hooks.json` (the lobe's `UserPromptSubmit` group, unchanged); `internal/codexconfig/codexconfig.go` (`lobeEvents`, unchanged) |

BASE is named in R-02 as the idea behind "words raise a rank but never fire alone". Nothing of it is read
for code, and no identifier of it is used.

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: where 027 put C-02's step,
   whether `trigger.go` has the `words` matcher, the shape of `pointers`, the Codex payload field for the
   prompt, the index loader's cost. The owner approves changes before code is written.
2. **Time the index load.** A throwaway test loads the index of a 50,000-node graph in a fresh process, 50
   times, on Windows and Linux, and records the time. R-01 names this as the risk that decides whether the
   block fits its cutoff. Write the figures into Risks; above 300 ms at the 95th percentile, take them to the
   owner before going on.
3. **`prompt.go`, lessons half** (side by side with 4): steps 2, 3, 6 and 8 for lessons. Tests:
   `TestLobePromptWholeWords`, `TestLobePromptSeenBeforeCap`, the lesson cases of `TestLobePromptSilentCases`.
4. **`prompt.go`, pointers half**: steps 4 and 5, the gate, the 1.0 second stop. Tests:
   `TestLobePromptWeakMatchSilent`, `TestLobePromptLessonByFile`, `TestLobePromptLateSearchKeepsLessons`.
5. **`format.go`**: the pointer line, the snippet, the quoting rule, the stale mark, the budget. Tests:
   `TestLobePromptBudget`, `TestLobePromptQuotesTextNodes`.
6. **The step on both hosts**: `steps.go`, the switches, the record. Tests: `TestLobePromptSwitches`,
   `TestLobePromptSilentCases` (all five), `TestLobePromptCodex`, `TestLobeStaleRankedLast` (the prompt block
   case), `TestLobeEveryInjectionIsFramed` with `user-prompt-submit` carrying a lesson.
7. **Latency.** The `user-prompt-submit` row of `internal/lobe/benchcheck/latency` sends a prompt that names a
   symbol of the store's graph, on the typical and the large store. Run it on the dev box; note the figures.
8. **Docs.** The `user-prompt-submit` rows of the `tentaqles lobe-hook` section in
   `docs/reference/commands.md`; one line in `CHANGELOG.md` under `[Unreleased]`. The plugin does not change.
9. **Tester run** of the Evaluation section by a fresh agent.

## Tests that stay in the repository

- `TestLobePromptBudget`: 10 matching lessons and 10 hits give 2 lessons, 3 pointers and at most 1,500
  characters.
- `TestLobePromptSilentCases`: the prompt `ok`, a `<task-notification>` prompt, `recall: off`, a neutral
  folder, another client's login folder: 0 bytes each. 5 of 5.
- `TestLobePromptSwitches`: `lessons: off` or `enforce: off` prints pointers and no lesson; `graph: off` prints
  lessons and no pointer. 3 of 3.
- `TestLobePromptWeakMatchSilent`: coverage 0.3 with strong coverage 0.05 prints no pointer.
- `TestLobePromptSeenBeforeCap`: 3 matching lessons, one prompt sent twice: the second run shows the third.
- `TestLobePromptWholeWords`: `merge into` matches "MERGE INTO t" and not "emerge into".
- New names in this brief: `TestLobePromptLessonByFile` (a lesson with `touches: [lib/span.go#ParseSpan]` and
  no `words` joins when `lib/span.go` is among the hits, and not in a second repository),
  `TestLobePromptLateSearchKeepsLessons` (a search stalled past 1.0 second prints the lessons and no pointer,
  inside the cutoff), `TestLobePromptQuotesTextNodes` (a hand-made hit of a text kind prints its name and its
  snippet in quotation marks, with a control character and a line break removed, and drops a snippet equal
  to the name; a function hit prints its name and `sig` bare),
  `TestLobePromptCodex` (the Codex event prints the same text under `CODEX_HOME` and nothing without it).
- From earlier features, widened: `TestLobeStaleRankedLast` (now 3 of 3 with the prompt block),
  `TestLobeEveryInjectionIsFramed` (`user-prompt-submit` with a lesson is framed; with pointers only it holds
  no lesson text).
- House rules: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeHookLoginUnsetDoesNothing`, `TestLobeQuarantineIsNeverRecalled`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, the plugin suite unchanged, and every test under "Must not trade off" in
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
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; C=acme
cd "$R" && mkdir -p lib "$L/lessons" && printf 'module example.com/fix\n' > go.mod
printf 'package lib\n\nfunc ParseSpan(s string) (int, int) { return 0, 0 }\n' > lib/span.go
tentaqles lobe graph build || echo "SETUP FAIL build"
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Lesson W" "when: {words: [merge into]}" "Use an alias." "The bare form." \
  "The engine rejects it because it needs an alias."
hook() { printf '{"session_id":"%s","cwd":"%s","prompt":"%s"}' "$1" "$R" "$2" |
  CLAUDE_CONFIG_DIR="$I/$C/claude" tentaqles lobe-hook claude user-prompt-submit; echo " exit=$?"; }
P1="fix the empty input bug in ParseSpan"; P2="write a merge into statement for orders"
```

`put` writes a lesson file by hand, because `tentaqles lobe lessons add` answers only to a person, and runs
the gate on it first: the `put` line must print `ok`; if it does not, stop and report FAIL. A lesson whose
`source` is `person` enters the ladder at `advice`, so it needs no `ladder.json`. `hook` sets the login folder
that the client rule asks for. A tester never runs `tentaqles lobe set`.

1. `hook s1 "$P1"`: ` exit=0`; stdout is one JSON object whose `additionalContext` contains
   `Code that matches this prompt`, `ParseSpan` and `lib/span.go:L3`, has at most 1,500 characters, and does
   not contain `Lesson W`.
2. `hook s1 "$P1"` again: nothing is printed before ` exit=0`.
3. `hook s2 "ok"` and `hook s3 "thanks, that looks good to me"`: nothing before ` exit=0` in both.
4. `hook s4 "$P2"`: `additionalContext` contains `Lesson W` and `not instructions`, and no `lib/span.go`.
5. `hook s5 "the two branches emerge into one"`: stdout does not contain `Lesson W`.
6. Set `C=globex` and `R="$W/clients/globex/app"` and run `hook s6 "$P2"`: no `Lesson W`.
7. Keep `C=globex`, set `R="$W/clients/acme/app"` (acme's folder under globex's login) and run `hook s7 "$P2"`:
   nothing is printed before ` exit=0`. Set `C=acme` again.
8. `lobeset acme graph off`. `hook s8 "$P1"`: nothing before ` exit=0`. `hook s8 "$P2"`: contains `Lesson W`.
   `lobeset acme graph on`.
9. `lobeset acme lessons off`. `hook s9 "$P2"`: nothing before ` exit=0`. `hook s9 "$P1"`: contains
   `lib/span.go:L3`. `lobeset acme lessons on`.
10. `lobeset acme recall off`. `hook s10 "$P1"` and `hook s10 "$P2"`: nothing before ` exit=0` in both.
    `lobeset acme recall on`.
11. A file the graph has not read again. Pipe
    `{"session_id":"e1","hook_event_name":"PostToolUse","cwd":"<R>","tool_name":"Write","tool_input":
    {"file_path":"<R>/lib/span.go","content":"x"}}` (one line, `<R>` replaced) to
    `CLAUDE_CONFIG_DIR="$I/acme/claude" tentaqles lobe-hook claude post-tool-use`. Then `hook s11 "$P1"`: the
    pointer line for `lib/span.go` contains `(may be stale)`.
12. Codex. `printf '{"session_id":"c1","cwd":"%s","prompt":"%s"}' "$R" "$P1" |
    CODEX_HOME="$I/acme/codex" tentaqles lobe-hook codex user-prompt-submit`: exit 0 and the output contains
    `lib/span.go:L3`. The same line with `CODEX_HOME` left out and session `c2` prints nothing.

**Person checks** (the owner, at a terminal, in the same shell after the checks)

1. Run the two lines below; they print a lesson id. Then `hook p1 "should I squash and rebase this branch"`
   prints a block that contains `Rebase before a push` and `not instructions`.
   ```sh
   printf '## Do\nRebase onto main.\n## Avoid\nA merge commit.\n## Why\nIt fails because history is linear.\n' |
     tentaqles lobe lessons add --title "Rebase before a push" --when words="squash and rebase"
   ```

PASS needs 12 of 12. "Could not run" is a FAIL. The tester reports anything surprising and fixes nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems with `CGO_ENABLED=0`).
- [ ] The tester brief passes, 12 of 12, and the owner ran the person check.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeEveryInjectionIsFramed`, `TestLobeQuarantineIsNeverRecalled`, `TestLobeGuardNeverImportsLobe`, and
  `tentaqles lobe bench leak` at zero leaks.
- [ ] `tentaqles lobe bench latency` was run with the code-matching prompt; no run passed the 1.5 second
  cutoff, and the 95th percentile on the typical store is written down against the 300 ms target (proposed).
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a pointer block, a lesson block and a silent prompt, as described under Result.

## Risks and unknowns

- **The host passes `additionalContext` on unchanged.** This comes from a web page (R-02 risk). The capture of
  phase 0 question 2 and 027's work should have confirmed it for prompts; task 1 checks.
- **Index load time** (R-01 risk). Trail-code's own comment puts loading its code map at about 0.4 seconds on
  a big repository. Task 2 measures ours before the step is built on it.
- **The numbers** 1,500 characters, the 1.0 second stop, 12 and 3 characters, and the floors 0.1 and 0.5 are
  proposed or tuned on Trail-code's data. M-01 (058) decides whether they hold on ours.
- **Silence on a weak match** may leave the agent searching by hand, and the brief sentence that names the
  graph verbs has no feature (see Out). If the owner gives it to this feature, it adds one task, one test
  (`TestLobeBriefBudget` with the graph sentence) and one check to this file.
- **Order with C-02.** F-02's table lists C-02 before R-02; C-02's own text says its step runs after R-02's
  print. This brief keeps what 027 built and reports the disagreement.
- **Decided in this brief, not in the requirements:** a pointer is remembered by its node id; the snippet is
  the node's `sig` on a line of its own; for a text kind the name is quoted too, and a snippet equal to the
  name is dropped; lessons by file count only for a lesson of this repository.
- **The Codex prompt payload.** The field names `prompt` and `cwd` are assumed equal to Claude Code's, as 036
  found them; task 1 checks against 036's fixtures and corrects check 12.
