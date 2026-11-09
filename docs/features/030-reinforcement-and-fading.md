# 030: Lessons strengthen and fade

**Status** not started · **Stage** Lessons · **Entries** C-05 (without `shared-strength.json`), C-01 (step 5,
the branch for a pair an active lesson already covers), C-04 (step 2, a dropped repeat reinforces), E-07 (the
`faded` row), F-07 (`lessons confirm` as a reinforcement) · **Needs** 024 · **Feeds** 031, 033, 035

## Result

A lesson's `strength` now moves. When the same mistake comes back and is repaired the same way, the lesson that
covers it grows stronger and no second lesson is filed. A lesson nobody needs loses 0.05 a week and, at 0.1, is
retired: kept on disk, no longer recalled. A lesson a person wrote, a decision, and a rule at `block` never fade.

```sh
tentaqles lobe consolidate --session s2 --draft template --json   # "action":"reinforce","from":0.5,"to":0.55
tentaqles lobe consolidate --only fade --json --now 2026-12-21T12:00:00Z   # "action":"fade","to":0.2
tentaqles lobe rules --json                                       # "stage":"retired" once it has faded out
```

## Scope

**In**

- The starting strength by `source` (C-05 step 1), written by the code that files a lesson.
- Reinforcement on four kinds of evidence (step 2): a new repair with a fingerprint the lesson lists, a machine
  draft the gate drops as a repeat of it, a `hit` verdict in `trials.jsonl`, and a person's `lessons confirm`.
  At most one step per lesson per session (proposed).
- Fading at the end of the consolidation part of every pass (step 5), with `--only fade` and `--now`.
- Fading out at 0.1 or less: E-07's `Settle` retires the lesson with the reason `faded` (step 6).
- The set that never fades (step 7).
- The `reinforce` and `fade` action lines of `--json`.
- A repair that contradicts its lesson reinforces nothing and leaves a `pending` candidate that names the
  lesson under `supersedes`.

**Out**

- Wording and filing that candidate as a successor, and a new lesson for the fingerprint of a lesson retired
  as `faded` (feature 031).
- `shared-strength.json`, this machine's numbers for a shared lesson (feature 033, the first feature in which
  a shared lesson exists).
- The idle row of E-07 for a `block` rule, 90 days (feature 038).
- The relearn count M-04 should report (feature 022's benchmark is not changed here).

## What it builds on

- 016: the lesson header keys `strength`, `seen`, `created`, `last_seen`, `last_faded`, and the lesson writer;
  `lessons add` files a person's lesson at 1.0.
- 018: the pass (`consolidate/run.go`), C-01's pairs with their fingerprint, `--session`, `--only`, `--now`,
  `--json`, and `route(pair)`, the one function every kept pair goes through, which today always answers
  "candidate".
- 019: template drafting and filing a repair lesson at 0.5, and the one function in which a machine draft
  whose only failure is `repeat` is dropped (C-04 step 2); today it only closes the candidate.
- 023: `trials.jsonl` with its verdicts, `ladder.Settle`, `ladder.Counts`.
- 024: `ladder/apply.go`, `ladder-log.jsonl`, the `retire` action line, the false-alarm demotion that follows a
  working call the lesson says to avoid.
- 026: `tentaqles lobe lessons confirm`, which moves a retired lesson back to `advice`.
- 027: a correction lesson is filed at 0.7.
- The key `tampered` of the session file, which 018 already reads. Nothing writes it before features 033
  and 034; the tests here set it by hand.

## What it leaves for later features

- `internal/lobe/consolidate/strength.go`: `Reinforce(l *Lesson, now time.Time)` and `Fade(l *Lesson, now
  time.Time) (faded bool)`, both pure. 035 files `backfill` lessons at 0.4.
- One place that loads and saves a lesson's four numbers (`strength`, `seen`, `last_seen`, `last_faded`). Here
  it has one form, the private lesson's header. 033 adds the second, `shared-strength.json`, in C-05's shape:
  `{"l-…": {"strength": 0.5, "seen": 0, "last_seen": "", "last_faded": ""}}`.
- The `reinforced` key of `sessions/<session>.json`: a list of lesson ids reinforced in that session.
- Candidates that carry `supersedes` in `candidates.jsonl`, left `pending` and not worded, for 031.
- The `faded` reason in `ladder-log.jsonl`, which 031 reads to draft a successor for a faded lesson.

## How it works

1. **Starting strength** (proposed unless noted): `person` 1.0, `correction` 0.7, `repair` 0.5 (agentmemory's
   default), `backfill` 0.4. Who worded the lesson changes nothing. A missing or out-of-range `strength` is
   read as the source's starting value. This feature checks that every filing path writes its value.
2. **Reinforce when** (step 2):
   - (a) C-01 finds a pair whose fingerprint is in the lesson's `when.fingerprint` and whose working call does
     not match its `avoid.pattern`; a lesson with no pattern counts. `route(pair)` answers "reinforce" and
     the pair is not appended as a candidate (C-01 step 5): nothing is drafted.
   - (b) the gate's `repeat` check drops a machine draft in the lesson's favor; 019's drop function now also
     names the lesson it repeats.
   - (c) E-02 judged one of its fires a `hit`, read from the `trials.jsonl` lines written since the last pass.
     A verdict from a session marked `tampered` is skipped.
   - (d) a person runs `tentaqles lobe lessons confirm <id>`.
3. **What is not evidence** (step 3). A `pass`, an `unrelated` and an `unknown` verdict reinforce nothing: a
   call that worked after a note cannot be told from a call that was never going to fail.
4. **The step** (step 4): `strength = min(1, strength + 0.1 × (1 - strength))`, rounded to 3 decimals; `seen`
   plus 1; `last_seen` is today. From 0.5 the first two steps give 0.55 and 0.595. At most one step per lesson
   per session: the pass keeps the ids under `reinforced` in the session file. For (d) there is no session,
   and each run of the command is one step.
5. **A repair that contradicts its lesson.** When the working call matches the lesson's `avoid.pattern`, what
   the lesson says to avoid is what worked. Nothing is reinforced. E-02 has recorded a false alarm and E-07
   has demoted the lesson (023, 024). `route(pair)` gives its third answer: the pair is appended to
   `candidates.jsonl` as `pending` with the lesson's id under `supersedes`. Until feature 031 is built the
   draft step leaves a candidate with that key alone (this file's choice): worded as a plain lesson it would
   be dropped as a repeat and would reinforce the very lesson it contradicts.
6. **Fading** (step 5) runs at the end of the consolidation part of every pass, also with `--session`, and
   alone with `--only fade`. There is no timer. The baseline is the later of `last_seen` and `last_faded`, or
   `created` when both are absent. Under 7 days: nothing, and the file's bytes do not change. Otherwise
   `strength = max(0.05, strength - 0.05 × days / 7)`, rounded to 3 decimals, and `last_faded` is today.
7. **Never faded** (step 7): `source: person`, `kind: decision`, and any lesson stored at `block`. A lesson
   with no ladder entry is treated at its enter stage. A lesson already at `retired` is not faded further
   (this file's choice): it is not recalled, and its numbers are what a later `confirm` starts from.
8. **Fading out** (step 6). At 0.1 or less the lesson has faded out. `run.go` writes the numbers and never
   writes `ladder.json`. `ladder.Settle`, the next part of the same pass, reads the strength and makes the move
   to `retired` with the reason `faded`; the `retire` action line is printed from that move. The file is kept.
   A lesson from one repair that is never supported again is retired after 56 days. It does not come back by
   itself; a person's `lessons confirm` brings it back to `advice` (026) and, by (d), lifts its strength.
9. **Writing.** A private lesson's numbers are in its own header, rewritten in place under the store lock by
   temporary file and rename (F-01), and the header cache in `index/` is refreshed. Only the four keys
   `strength`, `seen`, `last_seen` and `last_faded` change; the body and every other key keep their bytes.
10. **The clock** is passed in. `--now <time>` sets it for a run of `tentaqles lobe consolidate`.
11. **Output.** `--json` prints `{"action":"reinforce","lesson":"l-…","from":0.5,"to":0.55}` and
    `{"action":"fade","lesson":"l-…","from":0.55,"to":0.2}`, one object per line.
12. **Switches.** With `lessons` off nothing here runs. Days with `lessons` off count as days without
    evidence.

## Where to read

| What | Where |
|---|---|
| Requirements | [C-05](../prd/lobe-consolidate.md), all steps except the shared half of step 8; [C-01](../prd/lobe-consolidate.md) step 5; [C-04](../prd/lobe-consolidate.md) step 2; [E-02](../prd/lobe-enforce.md) step 3 (the verdict table) and "A repair that contradicts its lesson"; [E-07](../prd/lobe-enforce.md), the `faded` row and "How to build it"; [F-07](../prd/lobe-foundation.md), the `lessons confirm` row |
| Source to port | [`src/functions/lessons.ts` L61](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L61): add 0.1 of the remaining headroom, capped at 1.0; [L145](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L145): default 0.5; [L160](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L160): fade rate 0.05 per week; [L346](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L346): nothing fades in the first week; [L349](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L349): linear in weeks, floor 0.05; [L354](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L354): 3 decimals; [`src/functions/replay.ts` L175](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/replay.ts#L175): imported lessons start at 0.4 |
| Source, weakness we avoid | [`src/functions/lessons.ts` L342](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L342): the baseline prefers the last fade over a newer reinforcement; [L358](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/lessons.ts#L358): only a lesson never reinforced is ever removed; [`src/index.ts` L725](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/index.ts#L725): the sweep is a timer inside a server |
| Source, idea | [`src/init/prefs.ts` L181](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/prefs.ts#L181): evidence is counted in distinct days, not raw repeats |
| Existing tentaqles code | `internal/lobe/consolidate/run.go`, `pair.go`, `candidates.go`; `internal/lobe/gate` (the `repeat` drop); `internal/lobe/trial` (`trials.jsonl`); `internal/lobe/ladder` (`settle.go`, `apply.go`); `internal/cmd/lobe_lessons.go` (`confirm`) |

A ported Go file starts with `// Ported from agentmemory src/functions/lessons.ts at <commit> (Apache-2.0).`,
and `NOTICE` lists it (F-07).

## Build tasks

1. **Check this brief against the code.** Read how 019 and 027 write a starting strength, what 019 does today
   with a second pair of the same fingerprint, and whether 024 built the `faded` row of `Settle` or left a
   stub. List what no longer fits. The owner approves changes first.
2. **The two pure functions.** `strength.go`, `strength_test.go`. Proof: `TestLobeReinforceSteps` (the
   arithmetic), `TestLobeFadeWeekly`, `TestLobeFadeClockRestarts`.
3. **Reinforcement in the pass.** Evidence (a), (b) and (c); the `reinforced` key; the contradicting repair and
   its candidate. Files: `run.go`, `pair.go` (`route`), `candidates.go`. Proof: `TestLobeReinforceSteps` (the session
   rule), `TestLobeReinforceNotWhenAvoidedWorks`, `TestLobeReinforceIgnoresPass`.
4. **Fading in the pass** (side by side with 3). `--only fade`, the never-faded set, the in-place header write.
   Proof: `TestLobeFadeSkipsPersonDecisionBlock`, `TestLobeFadeKeepsOtherBytes`.
5. **Fading out.** The `faded` row in `ladder.Settle`. Proof: `TestLobeFadeLeavesLadderToE07`.
6. **`confirm` reinforces** (side by side with 3 to 5). Files: `internal/cmd/lobe_lessons.go`. Proof:
   `TestLobeConfirm_Reinforces`.
7. **Docs.** `docs/reference/commands.md`: `--only fade` and the two action lines under `tentaqles lobe
   consolidate`; the strength effect under `tentaqles lobe lessons confirm`. `CHANGELOG.md`; `NOTICE` if a file
   is ported. The plugin does not change.
8. **Tester run**, then the person check.

## Tests that stay in the repository

- `TestLobeReinforceSteps`: 0.5 becomes 0.55, then 0.595, in two sessions; 3 triggers in one session give 1
  step.
- `TestLobeFadeWeekly`: strength 0.5, created 49 days before `now`, gives 0.15, not faded out; 7 days later
  0.1, faded out, and E-07 retires it. After 6 days the file bytes are unchanged.
- `TestLobeFadeClockRestarts`: strength 0.6, `last_faded` 8 days ago, `last_seen` today stays 0.6.
  agentmemory's rule would give 0.543.
- `TestLobeFadeSkipsPersonDecisionBlock`: 0 changes after 400 days for a `person` lesson, a `decision` and a
  lesson whose stored stage is `block`. `TestLobeFadeSkipsRetired` (new): the same for a lesson at `retired`.
- `TestLobeReinforceNotWhenAvoidedWorks`: a pair with the lesson's fingerprint whose working call matches its
  `avoid.pattern` gives 0 steps and 1 `pending` candidate that names the lesson under `supersedes`; the same
  pair against a lesson with no `avoid` block gives 1 step and 0 candidates.
- `TestLobeReinforceIgnoresPass`: 5 `pass` lines in `trials.jsonl` for a lesson give 0 steps; 1 `hit` line gives
  1 step; a `hit` from a session whose file holds `"tampered":true`, set by hand, gives 0.
- `TestLobeFadeLeavesLadderToE07`: after a fade to 0.1 and before `Settle` runs, `ladder.json` is
  byte-identical; after `Settle` the stage is `retired` and `ladder-log.jsonl` has 1 line with `faded`.
- `TestLobeFadeKeepsOtherBytes` (new): after a fade, a diff of the lesson file shows the lines `strength` and
  `last_faded` only.
- `TestLobeConfirm_Reinforces` (new): `confirm` on a lesson at 0.5 gives 0.55 and `seen` plus 1; under
  `CLAUDECODE=1` it changes 0 bytes.
- `TestLobeSupersedesCandidateWaits` (new): a `pending` candidate with `supersedes` starts 0 drafts.
- House rules. No hook changes: `TestLobeHookAlwaysExitsZero` and `TestLobeHookServesOnlyItsLogin` stay green
  untouched. The pass holds the store lock only around each write: `TestLobeDraftHoldsNoLock` still passes
  with a fade in the same pass. `TestLobeDemote_OneWriter` still finds one log line per stage change.
  `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`.

## Evaluation

The tester gets the built `tentaqles` first on `PATH` and an empty working folder. Use bash (Git Bash on
Windows). No network; the sandbox switches `model` off. Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
I="$TQ_HOME/identities"; A="$W/clients/acme"; cd "$A"; export CLAUDE_CONFIG_DIR="$I/acme/claude"
L="$I/acme/lobe/lessons"; D="$I/acme/lobe"
H() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"%s","tool_name":"Bash",' "$1" "$A" "$2"; }
FAIL() { { H "$1" PostToolUseFailure; printf '"tool_input":{"command":"npx vitest run"},'
  printf '"error":"Error: Worker exited unexpectedly"}'; } | tentaqles lobe-hook claude post-tool-use-failure; }
FIX() { { H "$1" PostToolUse; printf '%s"tool_input":{"command":"npx vitest run --pool=forks"},' "$2"
  printf '"tool_response":{"stdout":"ok","stderr":""}}'; } | tentaqles lobe-hook claude post-tool-use; }
PAIR() { FAIL "$1"; FIX "$1"; }
RUN() { tentaqles lobe consolidate "$@" --json; echo "exit=$?"; }
```

No line may start with `SETUP FAIL`. Every command below must exit 0.

1. `PAIR s1`, then `RUN --session s1 --draft template --now 2026-11-01T12:00:00Z`: a line with
   `"action":"file"`; call its lesson id `ID`. `$L/ID.md` has `strength: 0.5`, `seen: 0` and `created:
   2026-11-01`.
2. `PAIR s2`, then `RUN --session s2 --draft template --now 2026-11-02T12:00:00Z`: a line with
   `"action":"reinforce"`, `"lesson":"ID"`, `"from":0.5` and `"to":0.55`, and no line with `"action":"file"`.
   The file has `strength: 0.55`, `seen: 1` and `last_seen: 2026-11-02`. `$L` still holds one file.
3. The same failure again in one session adds nothing. `PAIR s2` again, then the same `RUN` for `s2`: no line
   with `"action":"reinforce"`, and the file still has `strength: 0.55`.
4. A call that works is no evidence. Pipe `{"session_id":"s3","cwd":"<A>","hook_event_name":"PreToolUse",
   "tool_name":"Bash","tool_use_id":"t1","tool_input":{"command":"npx vitest run --pool=forks"}}`, with `<A>`
   replaced by the value of `$A`, to `tentaqles lobe-hook claude pre-tool-use`. Then `FIX s3
   '"tool_use_id":"t1",'`. Then `RUN --session s3 --draft template --now 2026-11-02T13:00:00Z`: no line with
   `"action":"reinforce"`; the file still has `strength: 0.55` and `seen: 1`; `$D/trials.jsonl` has a line with
   `"verdict":"pass"`.
5. `RUN --only fade --now 2026-11-08T12:00:00Z`: no line with `"action":"fade"`. `cksum "$L/ID.md"` before and
   after gives the same sum.
6. `RUN --only fade --now 2026-12-21T12:00:00Z`, 49 days after `last_seen`: a line with `"action":"fade"`,
   `"from":0.55` and `"to":0.2`. The file has `strength: 0.2` and `last_faded: 2026-12-21`.
7. `RUN --only fade --now 2027-01-04T12:00:00Z`, 14 days later: `"to":0.1`, and a line with
   `"action":"retire"`, `"lesson":"ID"` and `"why":"faded"`. `$L/ID.md` still exists, `tentaqles lobe rules
   --json` shows the lesson with `"stage":"retired"`, and `$D/ladder-log.jsonl` has a line with `faded`.
8. Retired means silent. The input of check 4 with `session_id` `s4` and `"command":"npx vitest run"`, piped
   to `tentaqles lobe-hook claude pre-tool-use`: exit 0 and empty stdout.
9. A person's lesson does not fade. Write `$L/l-aaaa0001.md` with the header `{id: l-aaaa0001, title: "Use
   LIMIT, not TOP, on this warehouse", kind: lesson, source: person, scope: private, applies: client, when:
   {command: [dbsql]}, strength: 1.0, seen: 0, created: 2026-11-01}` between two `---` lines and the sections
   `## Do` (`Write SELECT ... LIMIT n.`), `## Avoid` (`SELECT TOP n.`) and `## Why` (`The engine rejects TOP
   because it does not support it.`). `tentaqles lobe lessons gate "$L/l-aaaa0001.md"` prints `ok`. Then `RUN
   --only fade --now 2028-01-01T12:00:00Z`: no line names `l-aaaa0001`, and the file still has `strength: 1.0`.

**Person checks** (for the owner, at a terminal, with the tester's `HOME`, `USERPROFILE` and `TQ_HOME` exported,
in `$A`, after the checks above)

1. `tentaqles lobe lessons confirm ID`: exit 0. `tentaqles lobe rules --json` shows the lesson at
   `"stage":"advice"`, and `$L/ID.md` has `strength: 0.19` and `seen: 2`: the person's word brought the faded
   lesson back and counted as one step.

PASS needs 9 of 9. "Could not run" is a FAIL. The person check is reported apart.

## Done when

- [ ] Every test above is in the repository and green in CI: `go vet ./...`, `go test ./... -count=1`,
  `go build ./...`.
- [ ] The tester brief passes 9 of 9.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeDemote_OneWriter`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, and every test
  under "Must not trade off" in `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md` and, for a ported file, `NOTICE` are updated.
- [ ] The owner has watched one lesson go from 0.5 to 0.55 and one fade out, with the commands under Result.

## Risks and unknowns

- The starting strengths and the 56-day life are choices, not measurements (proposed). M-04 should report how
  many retired lessons were later relearned; a high count means fading is too fast.
- A lesson that does its job is never hit again, so it fades in 56 days unless a person confirms it. That is
  the price of not counting a `pass`.
- Days with `lessons` off count as days without evidence, so a long pause can retire machine lessons at the
  first pass after it. F-06 keeps no record of when the switch changed.
- Between this feature and 031, a contradicted lesson is demoted by 024 and its successor candidate waits
  unworded (step 5). If the candidate expires first (`retention_days`), the successor is lost and the next
  contradicting repair makes a new candidate.
- Rewriting the header changes the lesson file's time and size. The session record keeps a hash of the printed
  line, not of the file (R-04), so a lesson is not shown again because its strength moved; task 1 confirms it.
- Evidence (b) depends on how 019 reports a dropped repeat; if it does not name the lesson it repeated, the
  gate's result needs that id.
- What forces a change to this file: a lesson writer that cannot rewrite four header keys and keep the other
  bytes; a `Settle` in which the `faded` row cannot be reached from the pass.
