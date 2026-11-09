# 026: Session brief

**Status** not started · **Stage** Lessons · **Entries** R-05 (without the graph sentence, the block count and
the stale order), E-01 (`lessons confirm`), F-07 (`lessons --waiting`, `lessons confirm`), E-07 (the moves the
brief lists) · **Needs** 024 · **Feeds** 027, 030, 032, 034, 035, 036, 038, 048, 050

## Result

A new Claude Code session in a client's folder starts with a few lines from the lobe: how many lessons the client
has, how many wait for the person to confirm them, and the lessons that apply to all work here and so have no
trigger. A client with nothing to say gets nothing. The person lists what waits with one command and vouches for a
lesson with another, after which it speaks.

```sh
printf '{"session_id":"s1","cwd":"%s","source":"startup"}' "$PWD" |
  CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude" tentaqles lobe-hook claude session-start
tentaqles lobe lessons --waiting
tentaqles lobe lessons confirm l-aaaa0004        # a person, at a terminal
```

## Scope

**In**

- The lobe's `SessionStart` entry in `plugin/hooks/hooks.json` and the `session-start` handler of
  `tentaqles lobe-hook claude`.
- The brief of R-05: line 1 with the lesson count, the waiting line, the standing lessons inside F-05's frame,
  the 1,000-character budget, and the rules for `startup`, `resume`, `clear` and `compact`.
- The lines about rules that moved since the last brief (R-05 step 6, E-07 step 2).
- `tentaqles lobe lessons --waiting`.
- `tentaqles lobe lessons confirm <id>`: a lesson at `seen` or `trial` moves to `advice`; a retired lesson
  re-enters and moves to `advice`.

**Out**

- The `Code graph:` sentence of line 1. No feature in the index names it. Feature 048 writes the
  `summary.json` it reads (G-08) and is the natural place; see the final Risk.
- The `<B> can block` part of line 1. B is always 0 until a rule can block (feature 038).
- Stale lessons ranked last and R-06's note in the brief, with `TestLobeBriefStaleLast` (feature 032).
- A teammate's standing lesson in the brief, and the width `confirm` gives a lesson from git (feature 034).
- `confirm` as a reinforcement of the lesson's strength, C-05 step 2 (d) (feature 030).
- Lessons that really wait: mined lessons arrive with the backfill (feature 035). Until then nothing waits in
  normal use, and the tester sets the state up by hand.
- A correction lesson filed by the lobe (feature 027). A hand-written file with `source: correction` already
  works here.
- The brief in a Codex session, as plain text (feature 036).
- R-02's use of the `shown` list on a prompt (feature 050).

## What it builds on

- 002: `tentaqles lobe sandbox` and the standard tester setup.
- 003: the store (`internal/lobe/store`: `Open`, `Update`, `SessionFile`), the hook engine
  (`internal/lobe/hook`: `Main`, the client rule, `Answer`, `emit`, `steps.go`), the wrapper
  `plugin/scripts/lobe_hook.sh`, and the plugin test `test_the_plugin_holds_only_the_guard_hook` as changed
  under decision D-11.
- 005: `internal/lobe/settings` (`recall`, `lessons`, `enforce`) and `tentaqles lobe status`.
- 012: `tentaqles lobe bench latency` and `stall`.
- 014: `safety.Wrap`, F-05's frame.
- 016: `internal/lobe/lesson` (parse), `internal/lobe/gate`, `tentaqles lobe lessons` (the list and `--json`),
  `tentaqles lobe lessons gate`, and `internal/cmd/lobe_lessons.go` with `lessons add`.
- 017: `internal/lobe/recall` (`session.go` with the session record and its `shown` list, `format.go`), the
  rule for an eligible lesson (R-04 step 1) with `applies`, the header cache in `index/`, and
  `internal/lobe/ladder` (`ladder.go`, `enter.go`: the stage a lesson acts at, with the `enforce` cap).
- 023: `ladder.Counts`, `ladder.Settle`.
- 024: `internal/lobe/person` (E-01's stronger person check), `apply` in `internal/lobe/ladder` as the one
  writer of a stage and of `ladder-log.jsonl`, `tentaqles lobe rules` with the reason of a rule's last move,
  `rules demote`, `rules retire`.

## What it leaves for later features

- `recall.Brief(c Client, in HookInput, deadline time.Time) string` in `internal/lobe/recall/brief.go`. Features
  032 (stale order and note), 034 (teammate lessons), 038 (`<B>`), 048 (graph sentence) widen it; 036 prints it
  for Codex.
- The `brief` key of `sessions/<session>.json`, and the emptying of `shown` and `pointers` on `clear` and
  `compact` (050 relies on it).
- The waiting set as one function, used by the brief and by `lessons --waiting` (035 relies on both).
- `tentaqles lobe lessons confirm <id>` in `internal/cmd/lobe_lessons.go`. 030 adds the reinforcement, 034 the
  width of a lesson from git, 031 names it as a way back for a retired lesson.
- The `session-start` row of `hook/steps.go` and the lobe's `SessionStart` group in `hooks.json`.

## How it works

1. **Wiring.** `plugin/hooks/hooks.json` gains a second group under `SessionStart`, after the guard's: `bash
   "${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh" session-start`, `"timeout": 5`, no matcher (F-03 step 2). The
   guard's group stays byte for byte. It is another entry and another process, so a slow or broken brief cannot
   change the guard's preamble or its exit code.
2. **Handler.** `tentaqles lobe-hook claude session-start` is in the inject class, cut off at 1.5 seconds (core
   page 5.2). Its own target is tighter, 150 ms at the 95th percentile (proposed), because nobody knows whether
   the host runs the two entries side by side. It reads `session_id`, `cwd` and `source` from the payload. It
   never loads a graph and never walks a repository: counts come from the header cache in `index/` (read as
   R-04 reads it, from the files when the cache is out of date) and from `ladder.json`.
3. **Silence.** Nothing is printed when the client rule fails (core page 5.1), when `recall` is off, or when
   the client has no eligible lesson, no lesson waiting and no rule that moved (step 7).
4. **Counts.** `<N>` is the number of eligible lessons (R-04 step 1, with `applies` judged for `cwd`). `<W>` is
   the number of lessons that wait: stored at `seen` or `trial`, with no `avoid` block and no
   `when.fingerprint` entry, so they cannot earn a hit (E-02), and counted only where `applies` lets them count
   here. An unreadable lesson is skipped and not counted.
5. **Line 1.** `[tentaqles lobe] <N> lessons for this client.` It is left out when N is 0.
6. **The waiting line.** When `lessons` is on, `enforce` is not `off` and W is above 0: `[tentaqles lobe] <W>
   waiting for you to confirm: tentaqles lobe lessons --waiting` (wording proposed). It holds a count and a
   command, never a lesson's text. It is inside the budget and is never dropped.
7. **Moved rules.** Up to 3 lines, then one line with a count of the rest (proposed), for the moves in
   `ladder-log.jsonl` made since the last brief by `hook` or `settle`, whose `why` is neither `entered` nor
   `person` (the person already knows what they did). A line holds the
   lesson's id, the two stages and the reason, and no lesson text, so it stays outside the frame:
   `[tentaqles lobe] l-4c7d19e2 moved from advice to trial (false_alarm); see tentaqles lobe rules` (wording
   proposed). "Since the last brief" is this file's choice (proposed), because the requirements do not say
   where that time is kept: on `startup` it is the `brief` time in the most recently changed other file of
   `sessions/` (one folder listing and one read), or the last 7 days when there is none; on `clear` and
   `compact` it is this session's own `brief` time. The log is read from its tail, 64 KiB at most.
8. **Standing lessons.** The eligible lessons with no `when` block that apply here: `applies: client`, or
   `applies: repo` in `cwd`'s repository. Order: lessons whose `source` is `person` or `correction` first, then
   `strength`, then newest `created`. Keep 5 (proposed). They follow inside F-05's frame, written by
   `safety.Wrap`, one line each: `- <title>. <first line of Do>`, cut at 160 characters.
9. **Budget.** The brief is at most 1,000 characters, frame included (proposed). Standing lessons drop from the
   end until it fits. It never holds the client's name, git identity, cloud or tools; the guard's preamble has
   them.
10. **Repetition.** `source` `startup`: print, and set `brief` to the time in the session record. `resume`:
    print nothing when `brief` is set. `clear` and `compact`: empty the record's `shown` and `pointers`, print
    again and set `brief`. Any other value, `fork` included, is treated as `startup`.
11. **Record.** The lessons printed are added to `shown` (newest 40 ids, R-04), under the store lock, after the
    print. A busy lock (50 ms) skips the write and the brief may show once more.
12. **Output.** The text goes to F-02's `emit` as one `additionalContext` with `hookEventName`
    `SessionStart`; the hook exits 0. On any failure it exits 0 with no output.
13. **`tentaqles lobe lessons --waiting`.** Anyone may run it. It lists the set of step 4 in the form of the
    plain list (016), with `--json` as well, and prints nothing but a one-line note when the set is empty.
14. **`tentaqles lobe lessons confirm <id>`.** A person only, by E-01's stronger check: `CLAUDECODE` unset,
    stdin and stdout are terminals, no agent among the 12 nearest parent processes. There is no `--yes`. It has
    one meaning: the person vouches for the lesson as written. A lesson at `seen` or `trial` moves to `advice`.
    A retired lesson re-enters and moves to `advice`. Both go through `ladder/apply.go` with `why`
    `confirmed` and `by` `person`, and the three counts return to 0. A lesson already at `advice` keeps its
    entry: the command says so in one line and exits 0. An id with no file in `lessons/` exits 1. A refusal
    changes no byte.

## Where to read

| What | Where |
|---|---|
| Requirements | [R-05](../prd/lobe-recall.md), all steps except the graph sentence; [R-04](../prd/lobe-recall.md) step 1 (eligible) and the session record under "How to build it"; [E-01](../prd/lobe-enforce.md), the rows `trial` to `advice`, "a person, not an agent" and "mute"; [E-02](../prd/lobe-enforce.md), "The limit of the record"; [E-07](../prd/lobe-enforce.md) step 2; [F-07](../prd/lobe-foundation.md), the rows `lessons` and `lessons confirm`, step 2; [F-03](../prd/lobe-foundation.md) steps 2 and 3; [core page 5.2 and 5.5](../prd/vertical-lobe.md) |
| Source to port | [`src/claude/format.ts` L411](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L411): standing learnings at session start; [L421](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L421): the person's own first, then newest; [`src/notes/flatten.ts` L36](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/flatten.ts#L36): one line of 160 characters |
| Source, not copied | [`src/claude/hooks.ts` L578](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hooks.ts#L578) and [`src/claude/format.ts` L355](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L355): the long session start we do not copy |
| Host reference (web) | <https://code.claude.com/docs/en/hooks>: the SessionStart `source` values |
| Existing tentaqles code | `plugin/hooks/hooks.json` L3 to L13 (the guard's `SessionStart` group, timeout 15); `plugin/scripts/tq_hook.sh`; `plugin/tests/test_tq_hook.py` (`test_the_plugin_holds_only_the_guard_hook`); `internal/cmd/claudehook.go` L103 (`sessionStart`) and L133 (`renderSessionStart`, which opens with `Client:`); `internal/cmd/run.go` L283 (`insideAgent`); `internal/cmd/reach.go` L437 (`interactive`) |

## Build tasks

1. **Check this brief against the code.** Read `internal/lobe/recall`, `internal/lobe/ladder`,
   `internal/lobe/person`, `internal/lobe/hook/steps.go`, `plugin/scripts/lobe_hook.sh` and the plugin test as
   they stand. List what no longer fits (the wrapper's event list, the shape of the header cache, what the
   pinned plugin test now asserts). The owner approves changes to this file before code is written.
2. **The waiting set and the counts.** One function over the header cache and `ladder.json` that returns the
   eligible lessons, the standing ones and the waiting ones for a `cwd`. Files: `internal/lobe/recall/brief.go`.
   Proof: `TestLobeBriefWaitingLine`, `TestLobeBriefStandingSources`.
3. **The brief text.** `recall.Brief`: lines, frame, order, budget, the moved-rule lines. Files: `brief.go`,
   `brief_test.go`. Proof: `TestLobeBriefBudget`, `TestLobeBriefSilentWhenEmpty`, `TestLobeBriefNoPreambleText`,
   `TestLobeBriefMoveLines`.
4. **The handler and the session record.** The `session-start` row in `hook/steps.go`; `source` handling; the
   `brief` key; `shown`. Proof: `TestLobeBriefResumeAndCompact`, `TestLobeBriefLeavesGuardAlone`, and the
   engine's tests widened to this event (`TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
   `TestLobeNoHooksSwitch`, `TestLobeEveryInjectionIsFramed`).
5. **Wiring** (side by side with 2 to 4). The `SessionStart` group in `plugin/hooks/hooks.json`;
   `session-start` in the wrapper's event list if it is not there; the pinned plugin test widened to one more
   lobe event. Proof: the plugin suite, `TestLobeHookTimeoutsRegistered`.
6. **`lessons --waiting`** (side by side with 3 to 5). Files: `internal/lobe/cli`. Proof:
   `TestLobeLessonsWaitingList`.
7. **`lessons confirm`** (side by side with 3 to 5). Files: `internal/cmd/lobe_lessons.go`,
   `internal/lobe/ladder/moves.go`. Proof: `TestLobeConfirm_MovesToAdvice`, `TestLobePerson_RefusesAgents`
   widened to this verb, `TestLobePersonVerbsRefuseInAgent`, `TestLobeCommandTable`.
8. **Speed.** Add `session-start` with a store of 500 lessons to `tentaqles lobe bench latency`, and the event
   to `bench stall`. Proof: the bench prints the 95th percentile against 150 ms (proposed).
9. **Docs.** `docs/reference/commands.md`: `session-start` under `tentaqles lobe-hook`, `--waiting` and
   `confirm` under `tentaqles lobe lessons`. `CHANGELOG.md` and `plugin/CHANGELOG.md` under `[Unreleased]`;
   `plugin/CLAUDE.md` names the new entry. Proof: `TestDocsReferenceEveryCommand`.
10. **Tester run.** A fresh agent runs the Evaluation below; the owner runs the person checks.

## Tests that stay in the repository

- `TestLobeBriefBudget`: 30 standing lessons with 60-character lines give 5 lesson lines; with 160-character
  lines the brief still has at most 1,000 characters.
- `TestLobeBriefSilentWhenEmpty`: a client with no lessons gives 0 bytes; so does `recall: off`.
- `TestLobeBriefWaitingLine`: with 2 `backfill` lessons at `seen` or `trial` and no `avoid` block or
  fingerprint, one line holds `2 waiting` and `tentaqles lobe lessons --waiting` and neither title; after one is
  confirmed it says `1 waiting`; with none waiting, or with `lessons: off`, there is no such line: 4 of 4.
- `TestLobeBriefStandingSources`: a standing lesson with `source` `person` or `correction` is printed with no
  confirmation; the gate refuses one with `source` `repair` or `backfill`; a `backfill` lesson with a `touches`
  entry and no `when` block sits at `seen`, is not printed and is counted as waiting: 5 of 5. Feature 034 adds
  the sixth case of R-05, a teammate's lesson.
- `TestLobeBriefNoPreambleText`: the output has none of `Client:`, `Git:`, `Cloud:`, `Identity:`, `Tools:`.
- `TestLobeBriefResumeAndCompact`: `resume` after `startup` gives 0 bytes; `compact` prints again and empties
  `shown`; an unknown `source` prints as `startup`.
- `TestLobeBriefLeavesGuardAlone`: the guard's `claude-hook session-start` output is byte-identical with and
  without lobe data.
- `TestLobeBriefMoveLines` (new): 5 moves since the last brief give 3 lines and one count line; a move with
  `why` `person` and an `entered` line give none; no line holds a lesson's title.
- `TestLobeLessonsWaitingList` (new): `lessons --waiting` lists exactly the lessons the brief counts.
- `TestLobeConfirm_MovesToAdvice` (new): `seen`, `trial` and `retired` each end at `advice` with counts 0 and one
  `ladder-log.jsonl` line with `confirmed` and `person`; a lesson at `advice` and an unknown id change 0 bytes.
- House rules. Never stalls: `TestLobeHookAlwaysExitsZero` and `bench stall` cover `session-start`. Wrong
  client gets nothing: `TestLobeHookServesOnlyItsLogin` covers it, and the leak suite of 013 stays at zero. The
  guard is untouched: `TestLobeGuardNeverImportsLobe`, `TestHookDeadlinesUnderHarnessTimeouts`,
  `test_tq_hook_deadline_is_under_the_hook_timeout`, and the guard's two `hooks.json` entries byte-identical.
  A person only: `TestLobePerson_RefusesAgents`, `TestLobePersonVerbsRefuseInAgent`.

## Evaluation

The tester gets the built `tentaqles` first on `PATH`, an empty working folder, and the repository's `plugin`
folder to read (`PLUGIN` is its path). Use bash (Git Bash on Windows). No network. Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; C=acme; cd "$R" && mkdir -p "$L/lessons"
K="r_$(printf app | sha256sum | cut -c1-8)"; printf '# Changes\n' > CHANGELOG.md
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Store timestamps in UTC" "" "Use UTC timestamps." "Local time." \
  "Reports were off because of local time."
put l-aaaa0002 "Name the databricks profile" "when: {command: [databricks]}" "Pass a profile." \
  "The default one." "The job fails because the default is unset."
mk l-aaaa0003 "Prefer short commit subjects" "" "Keep the subject short." "A long subject." \
  "The log was hard to scan because subjects wrapped." |
  sed 's/source: person/source: correction/' > "$W/l-aaaa0003.md"
tentaqles lobe lessons gate "$W/l-aaaa0003.md"; cp "$W/l-aaaa0003.md" "$L/lessons/l-aaaa0003.md"
mk l-aaaa0004 "Check changelog before tagging" "touches: [CHANGELOG.md], repo: $K" "Add a changelog line." \
  "A tag with no entry." "A release went out undocumented because nobody checked." |
  sed 's/source: person/source: backfill/' > "$W/l-aaaa0004.md"
tentaqles lobe lessons gate "$W/l-aaaa0004.md"; cp "$W/l-aaaa0004.md" "$L/lessons/l-aaaa0004.md"
hook() { printf '{"session_id":"%s","cwd":"%s","source":"%s"}' "$1" "$R" "$2" |
  CLAUDE_CONFIG_DIR="$I/$C/claude" tentaqles lobe-hook claude session-start; echo " exit=$?"; }
```

No line may start with `SETUP FAIL`, and each of the four `gate` runs must print `ok`; if not, stop and report
FAIL. The lesson files are written by hand because `tentaqles lobe lessons add` answers only to a person.
Lessons 1 and 3 are standing lessons (no `when` block), allowed because their `source` is `person` and
`correction`; both enter at `advice`. Lesson 2 has a trigger. Lesson 4 has `source: backfill` and no `when`
block, so it enters at `seen`; it has no `avoid` block and no fingerprint, so it waits. `hook` sets the login
folder the client rule asks for.

1. `grep -c 'lobe_hook.sh\\" session-start' "$PLUGIN/hooks/hooks.json"` prints `1`, and
   `grep -c 'tq_hook.sh\\" session-start' "$PLUGIN/hooks/hooks.json"` prints `1`.
2. `hook s1 startup`: `exit=0`; stdout contains `[tentaqles lobe] 3 lessons`, `Store timestamps in UTC`,
   `Prefer short commit subjects` and `not instructions`; it contains `1 waiting` and `tentaqles lobe lessons
   --waiting`; it contains none of `Name the databricks profile`, `Check changelog before tagging` and
   `Client:`; and it has at most 1,000 characters before ` exit=0`.
3. `hook s1 resume`: nothing is printed before ` exit=0`.
4. `hook s1 compact`: stdout contains `Store timestamps in UTC` and `Prefer short commit subjects` again.
5. `tentaqles lobe lessons --waiting`: exit 0; the output contains `Check changelog before tagging` and
   neither `Store timestamps in UTC` nor `Prefer short commit subjects`.
6. `CLAUDECODE=1 tentaqles lobe lessons confirm l-aaaa0004; echo "exit=$?"`: the exit code is not 0, and
   `tentaqles lobe lessons --waiting` still contains `Check changelog before tagging`.
7. A rule that moved. Write `$L/ladder.json` as `{"v":1,"entries":[{"lesson":"l-aaaa0002","stage":"trial",
   "hits":0,"false_alarms":0,"sessions":0,"approved_at":null}]}` on one line, and append to
   `$L/ladder-log.jsonl` the line `{"at":"NOW","lesson":"l-aaaa0002","from":"advice","to":"trial",
   "why":"false_alarm","by":"hook"}`, NOW being `date -u +%Y-%m-%dT%H:%M:%SZ`. Then `hook s2 startup`: stdout
   contains `[tentaqles lobe] 2 lessons`, `l-aaaa0002`, `false_alarm` and `2 waiting`, and does not contain
   `Name the databricks profile`. Lesson 2 now sits at `trial` and cannot earn a hit, so it waits too.
8. `lobeset acme recall off`, then `hook s3 startup`: nothing is printed before ` exit=0`. Then
   `lobeset acme recall on`.
9. Another client's login. Set `C=globex` and run `hook s4 startup`: nothing is printed before ` exit=0`, and
   `ls "$L/sessions"` shows no `s4.json`. Set `C=acme` again.
10. The guard is unchanged: `printf '{"cwd":"%s"}' "$R" | tentaqles claude-hook session-start` prints text that
    starts with `Client: acme` and contains no `[tentaqles lobe]`.

**Person checks** (for the owner, at a terminal, with the tester's `HOME`, `USERPROFILE` and `TQ_HOME` exported,
in `$R`, after the checks above)

1. `tentaqles lobe lessons confirm l-aaaa0004`: exit 0. `tentaqles lobe rules --json` shows `l-aaaa0004` with
   `"stage":"advice"`, and the last line of `$L/ladder-log.jsonl` has `"why":"confirmed"` and `"by":"person"`.
   The tester's `hook s5 startup` then contains `Check changelog before tagging` and `1 waiting`.
2. `tentaqles lobe rules retire l-aaaa0001`, then `tentaqles lobe lessons confirm l-aaaa0001`: `tentaqles lobe
   rules --json` shows `l-aaaa0001` at `"stage":"retired"` after the first command and at `"stage":"advice"`
   after the second.
3. Open a real Claude Code session in a client that has a standing lesson and read the first lines the agent
   was given: the brief is there once, below the guard's preamble or above it, and the preamble is unchanged.

PASS needs 10 of 10. "Could not run" is a FAIL. The person checks are reported apart.

## Done when

- [ ] Every test above is in the repository and green in CI: `go vet ./...`, `go test ./... -count=1`,
  `go build ./...`, and the plugin suite (`PYTHONPATH=plugin python -m pytest plugin/tests -q`).
- [ ] The tester brief passes 10 of 10.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeNoHooksSwitch`, `TestLobeEveryInjectionIsFramed`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, and every test under "Must not trade off" in `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md`, `plugin/CHANGELOG.md` and `plugin/CLAUDE.md` are updated.
- [ ] The owner has run the person checks and seen the brief in a real session.

## Risks and unknowns

- The `source` values (`startup`, `resume`, `clear`, `compact`, and `fork` in the research digest) come from a
  web page. Check them against the phase 0 capture of feature 003 before relying on `resume` and `compact`.
- Whether Claude Code shows `additionalContext` from a second SessionStart hook beside the guard's plain text
  is not confirmed (web). Person check 3 is the proof. If it does not, `emit` prints plain text for this event.
- If the host runs session-start hooks one after another, the brief's run time is added to session start.
- "Since the last brief" (step 7) and the wording of a moved-rule line are this file's choices. The owner may
  prefer a different rule, or no such lines before feature 038.
- A lesson a person demoted to `trial` with `tentaqles lobe rules demote`, with no `avoid` block and no
  fingerprint, fits the definition of waiting and is counted. The requirements define waiting by stage and
  record only.
- With five standing lessons at full length the frame and the lines pass 1,000 characters and the last lesson
  drops. Whether the count or the budget gives way is for M-01's numbers (feature 058).
- A standing lesson with no `avoid` block cannot earn a false alarm, so no hook takes it out of the brief. A
  person mutes it with `tentaqles lobe rules demote <id>` or `tentaqles lobe rules retire <id>`.
- The graph sentence of R-05 step 4 has no feature. If the owner gives it to 048, that feature widens
  `recall.Brief` and `TestLobeBriefSilentWhenEmpty` (a client with a graph and no lesson is not silent).
- What forces a change to this file: an engine (`hook.Main`, `emit`) that cannot print context on
  `session-start`; a header cache without the keys the counts need; a pinned plugin test that names its lobe
  events in a way this feature cannot widen.
