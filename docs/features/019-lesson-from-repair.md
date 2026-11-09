# 019: A repair becomes a lesson

**Status** not started · **Stage** Lessons · **Entries** C-03 (step 7, the template, and the lobe-set keys of
step 5), C-04 (step 4, filing a machine draft), F-05 (step 2, quarantine; step 1 for a repair), E-01 (the enter
rule for `source: repair`; D-9) · **Needs** 017, 018 · **Feeds** 020, 022, 023, 025, 030, 031, 033

## Result

This is the loop the product exists for. The agent fails a call and fixes it. After the turn the lobe files a
short lesson, with no model and no person. In the next session, just before the agent makes the same call, it
reads one line that says what worked here.

```sh
# session s1, logged in as acme: fail, then fix
tentaqles lobe-hook claude post-tool-use-failure < fail.json    # npx vitest run
tentaqles lobe-hook claude post-tool-use < ok.json              # npx vitest run --pool=forks
tentaqles lobe consolidate --session s1 --json
# {"action":"pair",...}
# {"action":"draft","session":"s1","by":"template"}
# {"action":"file","lesson":"l-4c7d19e2"}

# session s2: the same call is about to run
tentaqles lobe-hook claude pre-tool-use < pre.json              # npx vitest run
# additionalContext, inside the frame:
# - npx vitest: use --pool=forks. Do: Run `npx vitest` with `--pool=forks`. Avoid: Running `npx vitest`
#   without it. Why: Otherwise the call fails here
```

In a real session nobody runs `consolidate`: the Stop hook starts the pass (feature 018).

## Scope

**In**

- The draft step of the pass, for `pending` candidates of kind `repair`, by the template only (C-03 step 7).
- The keys the lobe sets on every machine draft (C-03 step 5): `kind`, `source`, `origin`, `when.tool`,
  `when.fingerprint`.
- The path of a machine draft through quarantine and the gate into `lessons/` (F-05 step 2, C-04 steps 2 and
  4), with both exceptions of C-04 step 2 and the candidate closed as `filed`, `refused` or `dropped`.
- The gate's `grounded` check and the fields of `gate.Evidence`. Feature 016 left both to this feature,
  because no evidence exists before a pair does.
- E-01's enter rule for a lesson filed here with `source: repair`: it enters at `advice` and is shown from
  the next matching call (D-9).
- `--draft template` and `--only draft` on `tentaqles lobe consolidate`, and the `draft` and `file` action
  lines.

**Out**

- The model path: C-03 steps 1 to 4, 6 and 8, `--draft model`, `--draft file:<path>`, an `avoid` block, the
  `why` key of the `draft` line (feature 025). A pair the template cannot word stays `pending` until then.
- Reinforcing the lesson a dropped repeat stands for (C-04 step 2, C-05 step 2b): feature 030.
- `supersedes` and successors (C-03 step 5, C-06): feature 031.
- Filing a lesson as shared (`share: shared`, F-04): feature 033. Every lesson here is private.
- Candidates from corrections and other signs (features 027 and 028) and from the backfill (feature 035).
- Showing the lesson right after a failure with its fingerprint (R-04 step 8): feature 020.
- Recording fires and taking a wrong note back (E-02, E-07): features 023 and 024. Until then a wrong lesson
  is removed by deleting its file.
- The tamper mark (feature 034). Pairing already skips a marked session (feature 018).
- Codex: a lesson learned in Claude Code is shown in Codex from feature 036.

## What it builds on

Task 1 checks each name against the code.

- [016](016-lesson-by-hand.md): `lesson.Lesson`, `lesson.Parse`, `lesson.Write`, `lesson.ID`;
  `gate.Check(l, ev, existing, anyway)` with every check of C-04's table but `grounded`, and an empty
  `Evidence` type; `safety.Shape` and the origin block writer; the filing function (temporary name, rename
  under the store lock); `tentaqles lobe lessons`, which already lists a file in `lessons/quarantine/` with
  the state `quarantine`, and the `quarantine` count of `tentaqles lobe status`.
- [017](017-lesson-before-call.md): `internal/lobe/recall` (the `when` matcher, the session record, the
  lesson line, `pretool.go`), the header cache in `index/`, `internal/lobe/ladder` (`Enter`, which answers
  `advice` for `source: repair`; `apply.go`; the caps), `tentaqles lobe rules`, and the rule that recall
  reads `lessons/*.md` only.
- [014](014-starter-rule-before.md): `safety.Wrap`, the frame.
- [018](018-repair-pairing.md): `internal/lobe/pass`, `tentaqles lobe consolidate`, `candidates.jsonl` with
  `candidates.Pending` and `candidates.Close`, `Pair` and `Change` with the field table of that file.

**What this feature widens.** The consolidation step gains a draft part after pairing. `consolidate` gains
`--draft` and the value `draft` for `--only`. `gate.Evidence` gets its two fields and `gate.Check` its
`grounded` check. The lesson writer gains `origin.session` and `origin.events`. `ladder.Enter` gets its
first caller that is not a person.

## What it leaves for later features

- `consolidate.File(candidate, lesson, evidence)`: the one path of a machine draft through quarantine, the
  gate, feature 016's filing function, `ladder.Enter` and the candidate's closing state. Features 025, 027,
  028, 031 and 035 call it; feature 033 makes it file in the place `share` gives.
- `gate.Evidence{Failing, Working string}` and the `grounded` check, for every later machine draft.
- `template.go` with `Template(pair) (lesson, ok)`: the fallback of feature 025 and the wording of features
  027 and 028.
- The `draft` (`session`, `by`) and `file` (`lesson`) action lines; feature 025 adds `why` to `draft`.
- The closing states `filed`, `refused` and `dropped` on candidate lines. Feature 025 adds `skipped`.
- The place where a draft is dropped as a repeat: one function that gets the id of the lesson the draft
  repeats (the gate's `repeat` problem names it) and today only closes the candidate. Feature 030 adds the
  reinforcement of that lesson.
- Lessons with `source: repair`, a `when.fingerprint` entry and no `avoid` block, at `advice`: what features
  020, 022, 023 and 030 work on.

## How it works

1. **Where it runs.** The draft step follows pairing in the consolidation part of the pass (core page 5.2),
   detached after Stop or in a foreground `tentaqles lobe consolidate`. `--only draft` runs it without
   pairing. With `lessons` off it does not run. `--draft` takes `template` only in this feature, and that is
   also what a run without the flag does.
2. **Which candidates.** Lines of `candidates.jsonl` with `kind` `repair` and `state` `pending`, oldest
   first; with `--session`, that session's. The two events of the pair are read from the log by session and
   id. A candidate whose events are gone is closed as `dropped`.
3. **Can the template word it?** Yes when the pair's `diff` holds exactly one change (proposed) and its
   kind is `flag`, `env`, `wrapper`, `step`, `cwd` or `shell`. Any other pair (class SQL, API or other, a
   `words` difference, several changes) keeps `state` `pending`: it waits for the model (C-03 step 7) and
   expires with the events. Nothing is printed for it.
4. **The sentences.** The first row is C-03's own. The other rows are proposed here, because C-03 gives one
   example and says "fixed sentences"; the owner may reword them. `<key>` is the pair's key, `<name>` and
   `<text>` are the change's fields (feature 018's table). An empty folder is written `the repository root`.

   | Change | Title | Do | Avoid | Why |
   |---|---|---|---|---|
   | `flag` added | `<key>: use <text>` | ``Run `<key>` with `<text>`.`` | ``Running `<key>` without it.`` | `Otherwise the call fails here.` |
   | `flag` removed | `<key>: leave out <text>` | ``Run `<key>` without `<text>`.`` | ``Passing `<text>` to `<key>`.`` | `With it the call fails here.` |
   | `env` | `<key>: set <name> first` | ``Set `<name>` when you run `<key>`.`` | ``Running `<key>` with `<name>` unset.`` | `The call fails here because the variable is not set.` |
   | `wrapper` | `<key>: run it through <name>` | ``Run `<key>` through `<name>`.`` | ``Running `<key>` directly.`` | `Run directly, the call fails here.` |
   | `step` | `<key>: run <name> first` | ``Run `<name>` before `<key>`.`` | ``Running `<key>` before `<name>`.`` | `The call fails here because that step has not run.` |
   | `cwd` | `<key>: run it from <text>` | ``Run `<key>` from `<text>`.`` | ``Running it from `<name>`.`` | `From the other folder the call fails here.` |
   | `shell` | `<key>: use the <text> tool` | ``Run `<key>` with the `<text>` tool.`` | ``Running it with the `<name>` tool.`` | `The other shell fails on it here.` |

   A template copies no error text and writes no `avoid` block.
5. **The header.** The lobe sets every key; none comes from the events as free text.
   `kind: lesson`, `source: repair`, `scope: private`. `origin`: `by: me`, `host` from the failing event,
   `drafted: template`, `session`, `events` (the two ids), `lobe` (the program's version). `when`: `tool`
   is the failing call's tool, `command` is the key, `fingerprint` is the pair's `fp`. `applies` is `repo`
   with `repo` set when the failing event has a `repo`, else `client` (C-04, "How to build it"). `strength:
   0.5` (C-05 step 1), `seen: 0`, `created` is the pass's date. The id is C-04 step 1's hash.
6. **Quarantine.** The draft is written to `lessons/quarantine/<id>.md` (F-05 step 2). Recall never reads
   that folder, so nothing unchecked can be shown.
7. **The gate.** `gate.Check(lesson, evidence, existing, false)` with the two event inputs as evidence. A
   machine draft has no bypass. `grounded` is new here (C-04's table): it fails when a word of a
   `when.command` or `when.words` entry is in neither input, or when an `avoid.pattern` misses the failing
   input or matches the working one. A template lesson passes it by construction; the check is built now
   because every later draft needs it.
8. **Outcomes (C-04 steps 2 and 4).**
   - Pass: under the store lock the file is renamed into `lessons/<id>.md`, `ladder.Enter` gives it its
     entry, `index/` is refreshed, and the candidate closes as `filed`.
   - Only `repeat` failed: the draft is deleted and the candidate closes as `dropped`. This is what happens
     when the same repair is seen again: the lesson is already there.
   - Only `pattern` failed: the draft is filed without its `avoid` block. No template writes one, so
     this branch first meets a real draft in feature 025; it is built and tested here.
   - `secret`, `data` or `lobe/shape` failed: the draft is deleted at once; the candidate closes as `refused`.
   - Anything else failed: the draft stays in quarantine, never shown, until its candidate line expires;
     the candidate closes as `refused`. The candidate line does not name the draft, so the chore of feature
     018 is widened by file age (proposed): it deletes a quarantine file older than `retention_days`.
   In a foreground run the gate's problems are printed on stderr, one `<check>: <message>` line each.
9. **The enter rule.** `File` calls `ladder.Enter`. A lesson filed here with `source: repair` enters at
   `advice` (E-01's table, D-9). Its line in `ladder-log.jsonl` has `why` `entered` and `by` `settle`: E-01
   names `hook`, `settle` and `person`, and a filing by the pass is neither of the other two (this file's
   choice; feature 017 left it open). The lesson carries a `when` block, so feature 017's hook shows it
   before the next call that matches `tool` and `command`, once per session, inside the frame, under the
   same switches as any lesson (`recall`, `lessons`, `enforce`).
10. **Output.** With `--json`: `{"action":"draft","session":"<s>","by":"template"}`, then
    `{"action":"file","lesson":"<id>"}` when it was filed. A refused or dropped draft prints no `file` line.

## Where to read

| What | Where |
|---|---|
| Requirements | [C-03](../prd/lobe-consolidate.md) steps 5 and 7 and "How to build it" (`template.go`, `--draft template`); [C-04](../prd/lobe-consolidate.md) steps 1, 2 and 4, the table of checks, "How to build it"; [F-05](../prd/lobe-foundation.md) steps 1 and 2; [E-01](../prd/lobe-enforce.md), the `enter` row and "How to build it" (`ladder.Enter`); [C-05](../prd/lobe-consolidate.md) step 1 (starting strength); [core page](../prd/vertical-lobe.md) 5.4, 5.5 and decision D-9 |
| How the lesson is shown | [R-04](../prd/lobe-recall.md) steps 1 to 4 and 9 to 11 (built by feature 017) |
| Source to port (Trail-code, MIT) | [`src/notes/learning-fields.ts` L25](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/learning-fields.ts#L25): the source is set by whatever saved the learning, never by a flag; [`src/learn/bar.ts` L125](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/learn/bar.ts#L125): a finding is kept only when its words are in the material. The template, the quarantine folder and the enter rule are tentaqles' own design |
| Existing tentaqles code | the lobe packages named under "What it builds on"; `internal/testutil/home.go` (`HomeRun`); `docs/reference/commands.md` |

## Build tasks

1. **Check this brief against the code.** Read features 016, 017 and 018 as built. List what no longer
   fits: the signature of `gate.Check`, what the filing code of feature 016 exposes, what `ladder.Enter`
   answers for `repair`, the fields `diff.go` fills. Run the seven Why sentences of step 4 through the gate
   as built; if the gate refuses C-03's own sentence, stop and ask the owner (see Risks). The owner approves
   changes to this file before code is written.
2. **The template.** `internal/lobe/consolidate/template.go`. Proof: `TestLobeTemplateSentences`,
   `TestLobeTemplatePassesGate`, `TestLobeTemplateWritesNoAvoidAndNoError`, `TestLobeDraftWaitsForTheModel`.
3. **The filing path.** Side by side with task 2. `internal/lobe/consolidate/draft.go` with `File`;
   quarantine write, gate call, the four outcomes, the candidate's closing state. Proof:
   `TestLobeDraftFilesThroughQuarantine`, `TestLobeDraftRefusals`, `TestLobeQuarantineIsNeverRecalled`.
4. **The pass step and the flags.** The draft step in `internal/lobe/pass`; `--draft template` and `--only
   draft` in `internal/cmd/lobe_consolidate.go`; the two action lines. Proof:
   `TestLobeDraftSourceAndOrigin`, `TestLobeDraftSkippedWhenLessonsOff`.
5. **Grounded and enter.** `internal/lobe/gate/grounded.go` and the `Evidence` fields; the call of
   `ladder.Enter` from `File`. Proof: `TestLobeGateGrounded`, `TestLobeGateAnywayOnlyThree`,
   `TestLobeRepairLessonEntersAtAdvice`, `TestLobeOriginBlock`.
6. **The loop.** One test that runs fail, fix, pass and the next session's hook in a test home. Proof:
   `TestLobeRepairLessonIsShownNextSession`.
7. **Docs.** `docs/reference/commands.md`: `--draft` and `--only draft` under `tentaqles lobe consolidate`.
   One line in `CHANGELOG.md`. No plugin change.
8. **Tester run.** A fresh agent runs "Evaluation" below.

## Tests that stay in the repository

From the requirements:

- `TestLobeDraftSourceAndOrigin` (C-03): the part this slice can run. A repair candidate worded by the
  template has `source: repair` and `origin.drafted: template`, and no file holds `source: model`. The full
  "5 of 5" arrives with features 025, 027 and 028.
- `TestLobeOriginBlock` (F-05): the `repair` path beside the `person` path of feature 016. Both files have
  `by`, `host` and `drafted`: 2 of the 4 paths.
- `TestLobeQuarantineIsNeverRecalled` (F-05): 100 quarantined lessons (proposed), 0 in any recall.
- `TestLobeGateAnywayOnlyThree` (C-04): with `grounded` built, the count reaches the entry's own: 3 filed, 7
  refused.

New for this slice:

- `TestLobeTemplateSentences`: the 7 rows of the table give the 7 exact texts.
- `TestLobeTemplatePassesGate`: each of the 7 template lessons passes `gate.Check` with 0 problems.
- `TestLobeTemplateWritesNoAvoidAndNoError`: no `avoid` key; a marker string in the error text occurs 0
  times under `lessons/`, quarantine included.
- `TestLobeDraftWaitsForTheModel`: a pair of class `sql`, a `words` difference and a pair with two changes
  each stay `pending` and file nothing: 3 of 3.
- `TestLobeDraftFilesThroughQuarantine`: while the gate runs the draft exists only in `quarantine/`; after a
  pass it exists only in `lessons/`, the header cache lists it and `ladder.json` has it at `advice`.
- `TestLobeDraftRefusals`: a draft failing `data` leaves no file and closes `refused`; a repeat leaves the
  folder byte-identical, closes `dropped` and names the lesson it repeats; a title over 100 characters stays
  in quarantine and closes `refused`: 3 of 3.
- `TestLobeDraftPatternOnlyFailure`: a hand-built draft whose only failure is `pattern` is filed with no
  `avoid` key.
- `TestLobeGateGrounded`: a `when.command` word in neither input, a pattern that misses the failing input and
  one that matches the working input each fail `grounded`; a `source: person` lesson is not tested: 4 of 4.
- `TestLobeRepairLessonEntersAtAdvice`: a lesson filed by `File` has an entry at `advice` with counts 0 and
  no approval, and `ladder-log.jsonl` has one line with `entered` and `settle`.
- `TestLobeDraftSkippedWhenLessonsOff`: with `lessons` off the step files 0 lessons and closes 0 candidates.
- `TestLobeRepairLessonIsShownNextSession`: fail, fix and one pass in session 1; the `pre-tool-use` hook of
  session 2 prints the lesson inside F-05's frame; the same hook under another client's login prints 0
  bytes and that client's store is byte-identical.

House rules this feature keeps green: `TestLobeEveryInjectionIsFramed`, `TestLobeGuardNeverImportsLobe`,
`TestLobeImportsOnlyTheFour`, `TestLobeHookServesOnlyItsLogin`, the tests of features 016 to 018, feature
012's latency check for `pre-tool-use`, feature 013's leak suite, and every "Must not trade off" test in
`CLAUDE.md`, unedited. No model is called: no test in this feature starts a child process.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows) and an empty
working folder. It uses no network, never clears `CLAUDECODE` and never runs a person-only command.

**Setup.** Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
lobeset() { m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"; }
A="$W/clients/acme"; ID="$TQ_HOME/identities"; L="$ID/acme/lobe"; cd "$A"
export CLAUDE_CONFIG_DIR="$ID/acme/claude"
fail() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"%s"},"error":"%s"}' \
  "$1" "$A" "$2" "$3" | tentaqles lobe-hook claude post-tool-use-failure; }
ok() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"%s"},"tool_response":{"stdout":"ok","stderr":""}}' \
  "$1" "$A" "$2" | tentaqles lobe-hook claude post-tool-use; }
pre() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"%s"}}' \
  "$1" "$A" "$2" | tentaqles lobe-hook claude pre-tool-use; }
run() { tentaqles lobe consolidate --session "$1" --draft template --json; }
cand() { grep "\"session\":\"$1\"" "$L/candidates.jsonl"; }
```

No line may start with `SETUP FAIL`. The setup leaves `model` off, so nothing here can reach a model. Every
hook and every `run` must exit 0; `fail` and `ok` must print nothing.

1. `fail s1 "npx vitest run" "Error: Worker exited unexpectedly"`, `ok s1 "npx vitest run --pool=forks"`,
   `run s1 > "$W/r1.txt"`. Expected: the file has a line with `"action":"pair"`, a line with
   `"action":"draft"` and `"by":"template"`, and a line with `"action":"file"` and `"lesson":"l-`. Set
   `LID` to that id: `LID=$(grep -o 'l-[0-9a-f]\{8,12\}' "$W/r1.txt" | head -1)`.
2. `F="$L/lessons/$LID.md"`. The file exists and holds `source: repair`, `drafted: template`, `by: me`,
   `host: claude`, a `fingerprint:` entry with `fp_`, and the text ``Run `npx vitest` with `--pool=forks`.``
   `grep -c '^avoid:' "$F"` prints `0` and `grep -c 'Worker exited' "$F"` prints `0`.
   `ls "$L/lessons/quarantine" 2>/dev/null | wc -l` prints `0`.
3. `tentaqles lobe lessons --json | grep "$LID"` prints one line that holds `"stage":"advice"`.
4. The loop closes. `pre s2 "npx vitest run"`: stdout holds `PreToolUse`, `use --pool=forks` and `not
   instructions`, and no `permissionDecision`. The same command again: nothing is printed.
5. Nothing crosses to another client. `CLAUDE_CONFIG_DIR="$ID/globex/claude" pre s3 "npx vitest run"`
   prints nothing. Then, as globex in its own folder:

   ```bash
   printf '{"session_id":"g1","cwd":"%s","tool_name":"Bash","tool_input":{"command":"npx vitest run"}}' \
     "$W/clients/globex" | CLAUDE_CONFIG_DIR="$ID/globex/claude" tentaqles lobe-hook claude pre-tool-use
   ```

   This prints nothing too, and `ls "$ID/globex/lobe/lessons" 2>/dev/null | wc -l` prints `0`.
6. The same repair again files nothing new. `fail s4 "npx vitest run" "Error: Worker exited unexpectedly"`,
   `ok s4 "npx vitest run --pool=forks"`, `run s4`: no line holds `"action":"file"`.
   `ls "$L/lessons"/*.md | wc -l` prints `1`, and `cand s4` holds `"state":"dropped"`.
7. A pair the template cannot word waits. Pipe the two SQL payloads of session `s5` below, then `run s5`:
   a line holds `"class":"sql"`, no line holds `"action":"file"`, `cand s5` holds `"state":"pending"`, and
   `lessons` still holds one file.
8. Data never reaches a lesson. `fail s6 "deploy run" "denied"`, `ok s6 "deploy run --build=123456789"`,
   `run s6`: no line holds `"action":"file"`. `grep -rl 123456789 "$L/lessons" | wc -l` prints `0`, and
   `cand s6` holds `"state":"refused"`.
9. Quarantine is never shown. `mkdir -p "$L/lessons/quarantine"` and write the lesson file below into
   `$L/lessons/quarantine/l-aaaa0002.md`. Then `pre s7 "dbsql -f q.sql"` prints nothing, and
   `tentaqles lobe lessons --json | grep l-aaaa0002` holds `"state":"quarantine"`.
10. The switch. `lobeset acme lessons off`, `fail s8 "pip install foo" "error: externally-managed-environment"`,
    `ok s8 "pip install --user foo"`, `run s8`: no output line holds `"action"`. `lobeset acme lessons on`,
    `run s8`: a line holds `"action":"file"`, and `grep -rl 'pip install: use --user' "$L/lessons"` names
    one file.
11. After a real Stop. `fail s9 "cargo test" "error: linking with cc failed"`, `ok s9 "cargo test --release"`,
    then `printf '{"session_id":"s9","cwd":"%s","hook_event_name":"Stop"}' "$A" | tentaqles lobe-hook claude stop`
    prints nothing and exits 0. Wait up to 10 seconds for `grep -rl 'cargo test' "$L/lessons"` to name a
    file. Then `pre s10 "cargo test"` prints a block that holds `use --release`.

The SQL payloads of check 7, each one line, with `ACME` replaced by the value of `$A`. The first goes to
`tentaqles lobe-hook claude post-tool-use-failure`, the second to `tentaqles lobe-hook claude post-tool-use`:

```json
{"session_id":"s5","cwd":"ACME","hook_event_name":"PostToolUseFailure","tool_name":"mcp__warehouse__query","tool_input":{"sql":"SELECT TOP 5 name FROM crm.users"},"error":"syntax error near TOP"}
{"session_id":"s5","cwd":"ACME","hook_event_name":"PostToolUse","tool_name":"mcp__warehouse__query","tool_input":{"sql":"SELECT name FROM crm.users LIMIT 5"},"tool_response":"ok"}
```

The lesson file of check 9:

```markdown
---
{id: l-aaaa0002, title: "Hidden while in quarantine", kind: lesson, source: person, scope: private,
 applies: client, when: {command: [dbsql]}, strength: 1.0, seen: 0, created: 2026-11-01}
---
## Do
Give the MERGE source an alias.
## Avoid
A subquery as the source.
## Why
The engine rejects it because the source has no alias.
```

**Person checks** (the owner, in one real client, in real Claude Code sessions)

1. In a first session, ask the agent to run a command that fails here and then the same command with the
   flag that makes it work. End the turn and wait 10 seconds. `tentaqles lobe lessons` lists a new lesson
   at `advice` whose title names the flag.
2. Start a new session in the same folder and ask for the first command again. Expected: the agent
   mentions or follows the note before or instead of repeating the failing form. Say what you saw.

**Score.** PASS needs 11 of 11. "Could not run" is a FAIL.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems). The plugin suite is unchanged and green.
- [ ] The tester brief passes 11 of 11.
- [ ] The house rules hold: `TestLobeQuarantineIsNeverRecalled`, `TestLobeEveryInjectionIsFramed`,
  `TestLobeRepairLessonIsShownNextSession` (wrong client gets nothing), `TestLobeTemplateWritesNoAvoidAndNoError`
  (no error text in a lesson), `TestLobeGuardNeverImportsLobe`, and every "Must not trade off" test unedited.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen the loop: fail, fix, and the note in the next session.

## Risks and unknowns

- **C-03's own Why sentence may not pass C-04's gate.** `why` needs a reason word from the gate's lists, and
  C-04 names `because`, `since`, `otherwise`, `fails`, `rejects` "and the rest". `Without it the call failed
  here.` holds none of the five named words. C-03's tester brief expects that lesson to be filed. Task 1 runs
  it through the gate as built. If it fails, the two requirements entries disagree: stop and ask the owner
  whether the sentence or the word list changes. The six proposed rows use `fails` or `because` on purpose.
- **Six of the seven template rows are proposed here.** C-03 gives one example. The owner may reword any row;
  `TestLobeTemplateSentences` pins whatever is agreed.
- **"Exactly one change" is this file's rule (proposed).** C-03 does not say what a template does with a pair
  that has two differences. Waiting for the model is the careful reading.
- **A template lesson cannot be taken back by a hook.** It has no `avoid` block, so it can never earn a false
  alarm (E-02, C-01's risks). A junk pair that passes the gate speaks at once (D-9). From feature 024 a person
  mutes it with `tentaqles lobe rules demote` or `retire`; before that, by deleting the file. Feature 022
  must report the junk rate.
- **The note also precedes the correct call.** The `when` block matches `npx vitest` with or without the flag,
  so the agent reads the note once per session even when it already does the right thing.
- **Feature 030 changes what a repeat does.** Today a repeat is dropped by the gate. Feature 030 moves a pair
  with a known fingerprint out of drafting at pairing time (C-01 step 5) and reinforces the lesson.
- **Until feature 034 no session is marked tampered.** A session that edits the lobe's own files can still
  teach a lesson between this feature and that one.
- **What would change this file:** a filing function in feature 016 that cannot be reused for a machine
  draft; a `ladder.Enter` that needs E-07's `apply` (feature 024) to write a stage; an owner decision on the
  template sentences.
