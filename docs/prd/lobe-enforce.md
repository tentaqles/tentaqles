# Lobe: Enforce (E-01 to E-07)

Enforce is the stage that can stop a tool call. A lesson climbs a ladder of stages (E-01), earns each step on a record
kept while it is silent (E-02), must pass its own test cases (E-03) and falls back as soon as it proves wrong (E-07).
Three checks sit beside the ladder: a block for a command that cannot succeed on this machine (E-04), a note when a
call passes the wrong number of arguments (E-05), and a warning before a commit (E-06). None of them calls a model,
and every doubt ends the same way: the call goes ahead. A deny can come from two things only, both under
`enforce: block`: a `block` rule a person approved (E-01), or a built-in sure-fail rule (E-04). Shapes, hooks,
budgets and settings are those of the [core page](vertical-lobe.md), cited here by section number.

The entries ship in three phases (core page section 8). Phase 2 builds E-01 up to `advice`, E-02 and E-07 as one
unit, because they name each other, in this order: E-01's files, matcher and enter rule, E-02, E-07, then E-01's
verbs. No lesson is shown before that unit exists. Phase 3 adds E-03, then E-01's step from `advice` to `block` with
the deny and the override, then E-04 and E-06. E-05 waits for the graph in phase 4.

## E-01 The rule ladder

**Phase** 2, block step in 3 · **Origin** new; idea: BASE · **Depends on** F-01 to F-07, C-04, R-04, E-02, E-03, E-07

**What it is.** A rule is a lesson (C-04) at a stage where the agent can feel it. The ladder is the list of stages a
lesson moves through on one machine (`seen`, `trial`, `advice`, `block`, `retired`) and the numbers that decide each
move. This entry also fixes what a denied call is told and how a person lets one call through. The stages up to
`advice` ship in phase 2, with the first lesson that is shown. The step from `advice` to `block`, the deny and the
override ship in phase 3, because they need E-03; until then `tentaqles lobe rules approve` refuses.

**Why it matters.** It serves "fewer errors" and avoids the anti-goal of a wrong rule that blocks correct work with no
visible reason and no one-step undo. Without it, nothing learned could ever block, or anything could.

**Mechanism**

| Rule | Value (every number is proposed and measured by M-05; E-02 defines a hit and a false alarm) |
|---|---|
| enter | `seen`: passed the gate (C-04). `trial`: has a `when` block. `advice`: a lesson written on this machine by its person (`source` is `person`). A lesson that arrives through git enters like a local one, `trial` when it has a `when` block, never above `advice`, with no record and no approval; F-04 hands its arrival to this rule, and `source: person` in a pulled file earns nothing. So does an old lesson that returns when its replacement is undone (C-06, E-07). A built-in starter rule (C-08) gets an ordinary entry at `advice` on the day the ladder ships; before that day it acts as advice with no record. A lesson file may not claim `kind: starter` or `source: starter` |
| `trial` to `advice` | at least 2 hits in at least 2 sessions and 0 false alarms; or a person runs `tentaqles lobe lessons confirm <id>`, which has one meaning: the person vouches for the lesson as written (F-07 describes the verb). On this ladder a lesson at `trial` moves to `advice`, and a retired lesson re-enters and moves to `advice`; the width a lesson from git then takes is F-04's part. A lesson that cannot earn a hit (no `avoid` block and no fingerprint, E-02) waits at its entry stage until the person confirms it |
| `advice` to `block` (phase 3) | all five: an `avoid` block that passes E-03's promotion test; at least 5 hits at `advice` in at least 2 sessions; 0 false alarms at `advice`; `enforce` is `block`; the person runs `tentaqles lobe rules approve <id>` in their own terminal and types `yes`. A lesson filed with `--anyway` (C-04) carries `anyway: true` and is refused whatever its record. E-04 states the one exception, for a built-in sure-fail rule, which needs no typed `yes` |
| `approve` in phase 2 | the verb exists and refuses everyone, before any other check: it prints `Blocking is not available yet in this version; <id> stays at advice.`, exits 1 and changes no file. Until phase 3 a stored `block` stage and the setting `enforce: block` both act as `advice` |
| a person, not an agent | `CLAUDECODE` is unset; stdin and stdout are terminals; none of the 12 nearest parent processes (Trail-code's number) is an agent's program; and, for `approve`, the word `yes` is read from the console device, not from stdin. `allow-once`, `demote`, `retire` and `lessons confirm` need the first three. There is no `--yes` flag, and a yes typed in the agent's chat never counts |
| override (phase 3) | `tentaqles lobe rules allow-once <id>`, a person only: the next 1 matching call within 10 minutes is not denied |
| mute | `tentaqles lobe rules retire <id>`, a person only: the lesson is silent on this machine from the next call. For a shared lesson this is the private mute: the ladder is local, so the file in the repository and every teammate's ladder stay as they are. `tentaqles lobe lessons confirm <id>` brings a retired lesson back: it re-enters and moves to `advice` with no record (proposed) |
| down or out | E-07: 1 false alarm; 2 false-alarm demotions; 3 false alarms at `trial` with 0 hits; 90 idle days |
| caps | `enforce` (F-06): a rule acts at the lower of its stored stage and the setting. Under `advice` a stored `block` rule gives advice. Under `off` this stage does no work: no trial line, no note, no deny. `lessons: off` does the same for every lesson and every starter rule (C-08). `recall: off` silences every note, a starter's included; a deny from an approved `block` rule, a sure-fail deny under `enforce: block` (E-04) and the trial record do not need `recall`. Stale (R-06): a stale rule never denies; a `block` rule keeps its approval, acts as advice while its cached stale flag is set, shown with R-06's mark, and denies again when the flag clears. No cap rewrites a stored stage or an approval, so lifting it restores what was there |
| deny text | at most 600 characters, as information: "[tentaqles lobe] Stopped before it ran: it matches rule l-4c7d19e2, approved by the person on this machine (TITLE; failed here N times). Why: WHY. Usual fix: DO. If the rule is wrong for this call, do not rephrase the call; tell the person, who can let one call through with: tentaqles lobe rules allow-once l-4c7d19e2". TITLE, WHY and DO are the lesson's title and the first sentences under its `## Why` and `## Do`, without closing periods, as the person approved them: they are read from `approvals.json`, never from the lesson file, and F-05's `safety.Wrap` puts them in. N is the lesson's `seen` |
| files | `ladder.json`: `{"v":1,"entries":[...]}` with the core page's ladder entry (5.4); `hits`, `false_alarms` and `sessions` count the current stage and return to 0 on a move. `approvals.json` (phase 3): `{"v":1,"approvals":[{"lesson","pattern","when","title","why","do","approved_at","override_until"}]}`, the pattern, the `when` block and the words the person approved. A lesson with no `when` block stores an empty one (`"when":{}`). A deny needs the lesson's `avoid.pattern` and `when` block to equal the stored ones (the pattern byte for byte, `when` as the same keys and values) and prints the stored words, so an edit pulled through git changes neither what is blocked here nor what the deny says; a changed `when` removes the approval the same way a changed pattern does (E-07). `ladder-log.jsonl`: one line per move, up or down, `{"at","lesson","from","to","why","by"}`; `why` is one of E-07's reasons or, for a move up, `entered`, `record`, `confirmed` or `approved`, and `by` is `hook`, `settle` or `person`. The three files are local to this machine and never enter a repository |

1. At PreToolUse a `block` rule denies only when all three hold: the call is where the lesson's `applies` says
   it counts, the lesson's `when` block matches the call, when it has one (R-04's matcher), and its approved
   pattern matches the whole call text. `applies` limits a deny as it limits R-04 and R-05: `client` is
   everywhere in the client; `repo` is only the repository named by `repo` (for a shared lesson, the repository
   its file sits in); `path` is only there and under a `when.place` entry, which must then be present (C-04
   checks it). In the `when` block any listed value of a key is enough, and `tool`, `command` and `place` narrow
   each other: every one of those keys that is present must match, and `command` is read only for shell tools
   (core page 5.4). The same statement sent to another tool, or a call in a folder outside `when.place`, or in
   another repository than an `applies: repo` lesson's, is therefore not denied. The call text is what
   `sense.CallText` returns (S-01): for a shell tool the command string; for any other tool the compact JSON of the
   tool input with sorted keys, as S-01 builds an event's `input`. Both pass S-05's scrubber and are cut at 4,000
   characters, the cap of an event's `input` (core page 5.4). A text that was cut at the cap never denies.
2. The pattern sees the whole text, so inside a matching `when` a quoted mention of it (an `echo`, a commit
   message) matches too. That is decided: E-03's promotion test is the guard before a rule may block, the person
   has `allow-once`, and M-05 reports quoted mentions in a row of their own instead of expecting them to pass.
3. The caps of the table apply before a deny goes out. For the stale cap the hook reads the matched rule's cached
   stale flag, which R-06 sets in the detached pass and keeps in `index/`. While the flag is set the rule gives
   advice instead of a deny and keeps its approval; it denies again when the flag clears. Only a rule with a
   `touches` entry can be stale. Such a rule with no flag in `index/` (the cache was deleted, or no detached
   pass has run yet) counts as stale: it gives advice and the call goes ahead. A rule without `touches` denies
   with no flag (R-06). No check of the code runs in the deny path.
4. With no deny, `advice` rules that R-04 matched go to R-04's injection and budget. A call let through by an
   override gets no output (the lobe never says allow) and uses the grant up; E-02 judges it.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| tentaqles (this repository) | reuse | `internal/cmd/run.go` L283: `insideAgent` is true when `CLAUDECODE` is not empty, and L282 calls it "a convenience check, not a boundary". `internal/cmd/reach.go` L437: `interactive`. `internal/cmd/run_test.go` L277 and L325: `TestRunOtherClient_RefusedForAgents`, `TestRunOtherClient_HumanAllowed` |
| Trail-code (MIT) | port: the person check, the form of a deny | [`src/skills/person.ts` L98](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L98): terminals, `CLAUDECODE` and no agent among 12 parents ([L29](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L29)). Weak: [L70](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L70) gives no process table on Windows and [L102](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/skills/person.ts#L102) reads that as "agent", so the check cannot pass there (read, not run). [`src/trace/guard-fast.ts` L25](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L25): a deny gives who stopped it, the fact, the usual fix, the way out. [`src/claude/pre-bash-skills.ts` L231](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-skills.ts#L231): "even after a yes in chat" |
| Trail-code (MIT) | weakness, not taken | [`src/notes/always-follow-gate.ts` L69](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/always-follow-gate.ts#L69): one shared word ties a typed chat line to a standing rule. [`src/claude/pre-bash-check.ts` L89](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L89): the agent passes a stop by running the command again |
| BASE (FSL-1.1) | idea only | [`src/shadow/report.rs` L349](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/report.rs#L349): a change is promoted only when a counted record of wins against losses clears a fixed ratio |
| tentaqles (new) | new | the five stages, the thresholds, the approval file, the override |

**How to build it**

- Packages: `internal/lobe/ladder` (`ladder.go`, `enter.go`, `moves.go`, `approve.go`, `deny.go`) and
  `internal/lobe/person`. The person-only verbs sit in `internal/cmd/lobe_rules.go` and, for `confirm`, in
  `internal/cmd/lobe_lessons.go` (core page 5.1). They call `insideAgent` and `interactive` there; both are
  unexported, so these verbs cannot sit in F-07's `internal/lobe/cli`.
- Phase 2 ships `ladder.go`, `enter.go`, `moves.go` up to `advice`, the caps, the list `tentaqles lobe rules`,
  `lessons confirm`, `rules demote`, `rules retire` and an `approve` that only refuses. Phase 3 ships `approve.go`,
  `deny.go`, `approvals.json` and `rules allow-once`.
- `ladder.Enter` gives a lesson its first entry by the enter rule. C-04 calls it when it files a lesson; `Settle`
  (E-02) calls it for a lesson file that has no entry yet, such as one pulled through git or written by hand. Until
  then the lesson acts at its enter stage and `tentaqles lobe rules` lists it there. Every write of a stage goes
  through E-07's `apply`, which also writes the log line.
- The deny is one JSON object on stdout with exit code 0: `hookSpecificOutput` holding `permissionDecision` `deny` and
  `permissionDecisionReason`, as tentaqles answers Codex today (`internal/cmd/codexhook.go` L64). For Claude Code the
  shape is from https://code.claude.com/docs/en/hooks (web, not machine-checked). F-02 owns the writer.
- `approve` refuses a lesson that carries `anyway: true` and runs the whole gate (C-04) again with no bypass, so
  deleting the key by hand gains nothing. Then it prints the pattern, the `when` block, the title and the two
  sentences it will store, then E-03's results, then asks.
- What a person approved is kept as readable text in `approvals.json` (core page 5.4), not as a hash of the lesson
  file, so the person can read what is enforced (principle 8). A deny is decided from that file, the lesson's
  `when` block and `applies`, the ladder stage, the cached stale flag (for a rule with a `touches` entry) and the
  `enforce` setting, never from lesson prose (F-05).
- The hook reads lesson headers from F-01's `index/` cache, as R-04 does. A lesson file placed by hand is seen
  without `tentaqles lobe rebuild`: when the header cache is older than the lessons folder, the hook reads the
  headers from the files inside its budget (R-04, F-01).
- Edge cases: a missing or unreadable ladder or approvals file means entry stages and no deny. Writes take F-01's
  lock. A hook writes these files in two cases only: to use up a grant, and to apply a demotion (E-07). When the lock
  stays busy for 50 ms (proposed), a PreToolUse hook lets the call through and leaves the grant as it is.
- The three files are compact JSON, as F-07's `--json` output is, so a check can search for `"stage":"trial"`. The
  briefs on this page write lesson files by hand; each carries F-05's `origin` block and passes C-04's gate.
- Where we differ: tentaqles knows an agent only by `CLAUDECODE`, and `drop` takes `--yes`. We add the console read
  and the parent walk (on Windows, `CreateToolhelp32Snapshot` in `golang.org/x/sys/windows`).

**How we know it works**

- `TestLobeLadder_Thresholds`: 8 records around the limits give the 8 expected stages.
- `TestLobeLadder_BlockNotAvailable` (phase 2 only): `approve` prints its line, exits 1 and changes 0 bytes, and a
  hand-written `block` stage gives a note and no deny.
- `TestLobeLadder_BlockNeedsAllFive` (from phase 3): dropping each of the five conditions in turn gives 5 refusals.
- `TestLobeLadder_AnywayNeverBlocks`: a lesson with `anyway: true` that meets all five is refused, and is still
  refused after the key is deleted by hand: 2 of 2.
- `TestLobeLadder_WholeCallText`: the approved pattern inside `echo "..."`, in a call the `when` block matches,
  is denied: yes.
- `TestLobeLadder_WhenMustMatch`: a `block` rule with a `when` block denies its statement in a matching call, and
  not the same statement sent to another tool or run in a folder outside `when.place`: 1 deny and 2 passes.
- `TestLobeLadder_WhenKeysNarrow`: with `tool` and `command` both present, a call that matches only one of them
  is not denied, and a second listed value of one key is enough: 2 passes and 1 deny.
- `TestLobeLadder_AppliesLimitsTheDeny`: an `applies: repo` rule denies in its repository and not in another;
  an `applies: path` rule denies under its `when.place` entry and not beside it: 2 denies and 2 passes.
- `TestLobeLadder_ChangedWhenEndsApproval`: a lesson whose `when` block no longer equals the stored one gives no
  deny and loses its approval at the next `Settle`; a lesson with no `when` block is stored with an empty one
  and denies: 2 of 2.
- `TestLobeLadder_MissingStaleFlag`: with no flag in `index/`, a `block` rule with a `touches` entry gives a note
  and no deny, and one without `touches` denies: 2 of 2.
- `TestLobeLadder_Caps`: `enforce: advice`, `enforce: off`, `lessons: off` and a set stale flag each give 0 denies
  from a `block` rule and leave `ladder.json` and `approvals.json` byte-identical; `recall: off` alone gives 1 deny,
  and so does clearing the stale flag.
- `TestLobeLadder_GitLessonEntersLikeALocalOne`: a pulled lesson with `source: person` and a `when` block enters
  at `trial`, with no record and no approval: yes.
- `TestLobeLadder_RetireMutesHere`: after `retire` on a shared lesson, 0 notes on this machine and the file in the
  repository is byte-identical.
- `TestLobePerson_RefusesAgents`: `CLAUDECODE` set, piped stdin or an agent parent each refuse; 0 bytes change.
- `TestLobeOverride_OneCall`: call 1 after a grant is not denied, call 2 is; an 11-minute-old grant passes 0 calls.

**Tester brief**

Start from the standard tester setup in F-07. `<acme>` is `$W/clients/acme` and `<lobe>` is
`$TQ_HOME/identities/acme/lobe`. Run every command
inside `<acme>`, after `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`: a lobe hook acts only when the
session's login folder is the client's own (core page 5.1). Backticked text split over two lines is one line: join
it with a space. "Set `enforce` to X" means: run `lobeset acme enforce X`, the setup's helper, which writes the
setting into the sandbox manifest as a fixture. `tentaqles lobe set` answers only to a person and no tester runs
it. Write `<lobe>/lessons/l-4c7d19e2.md`:

```markdown
---
id: l-4c7d19e2
title: Alias the MERGE source
kind: lesson
source: person
scope: private
applies: client
origin: {by: me, host: person, drafted: person}
when: {tool: [Bash], command: [dbsql]}
avoid:
  pattern: '(?i)merge into \S+ using \('
  must_match: ["MERGE INTO t USING (SELECT 1)", "merge into dim using (select 2)"]
  must_not_match: ["MERGE INTO t USING s", "MERGE INTO t USING src AS s"]
strength: 1.0
seen: 3
created: 2026-11-09
---
## Do
Give the source an alias.
## Avoid
A subquery as the source.
## Why
The warehouse rejects it because the source has no alias.
```

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if it does not, stop and report FAIL.
A hook sees a lesson file placed by hand without `tentaqles lobe rebuild` (R-04). This brief still runs
`tentaqles lobe rebuild` now and after every change to the lesson file, so that each check starts from a fresh
header cache. Input A, piped to `tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1",
"hook_event_name":"PreToolUse", "cwd":"<acme>", "tool_name":"Bash", "tool_use_id":"t1",
"tool_input":{"command":"dbsql -e 'MERGE INTO t USING (SELECT 1)'"}}`.

At the end of phase 2 run checks 1 to 4. At the end of phase 3 run checks 1, 2 and 4 to 14; check 3 belongs to a
phase 2 build alone.

1. Input A: exit code 0; stdout contains `Alias the MERGE source` (R-04's note) and no deny decision. Then
   `tentaqles lobe rules --json`: the entry for `l-4c7d19e2` has `"stage":"advice"`, where a lesson that a person
   wrote enters.
2. Write `<lobe>/ladder.json`: `{"v":1, "entries":[{"lesson":"l-4c7d19e2", "stage":"trial", "hits":0,
   "false_alarms":0, "sessions":0, "approved_at":null}]}`. Input A with `session_id` `s2`: exit code 0 and empty
   stdout.
3. `tentaqles lobe rules approve l-4c7d19e2`: exit code 1; the output contains `Blocking is not available yet`;
   `ladder.json` is unchanged and there is no `approvals.json`.
4. `CLAUDECODE=1 tentaqles lobe lessons confirm l-4c7d19e2`: exit code not 0, and `ladder.json` is unchanged.

For checks 5 to 14 set `enforce` to `block` and write the approved state by hand. `<lobe>/ladder.json`: `{"v":1,
"entries":[{"lesson":"l-4c7d19e2", "stage":"block", "hits":5, "false_alarms":0, "sessions":2,
"approved_at":"2026-11-09T10:00:00Z"}]}`. `<lobe>/approvals.json`: `{"v":1, "approvals":[{"lesson":"l-4c7d19e2",
"pattern":"(?i)merge into \\S+ using \\(", "when":{"tool":["Bash"], "command":["dbsql"]}, "title":"Alias the
MERGE source", "why":"The warehouse rejects it because the source has no alias.", "do":"Give the source an alias.",
"approved_at":"2026-11-09T10:00:00Z", "override_until":null}]}`. The lesson has no `touches` entry, so it cannot
be stale and no stale flag is needed for a deny.

5. Input A with `session_id` `s3`: exit code 0; stdout has `"permissionDecision":"deny"`, `The warehouse rejects it`
   and `tentaqles lobe rules allow-once l-4c7d19e2`.
6. Input A with the command `dbsql -e 'MERGE INTO t USING s'`: exit code 0 and no deny decision in stdout.
7. Input A with the command `dbsql -e 'SELECT 1'; echo 'MERGE INTO t USING (SELECT 1)'`: stdout has a deny
   decision. The `when` block matches (one statement runs `dbsql`), and the pattern is tested on the whole call
   text, so a quoted mention matches inside a matching `when`. Then input A with the command
   `echo 'MERGE INTO t USING (SELECT 1)'` alone: exit code 0 and no deny decision, because no statement runs
   `dbsql` and `tool` and `command` narrow each other.
8. `CLAUDECODE=1 tentaqles lobe rules allow-once l-4c7d19e2`: exit code not 0, and `approvals.json` is unchanged.
9. `echo yes | tentaqles lobe rules approve l-4c7d19e2`: exit code not 0, and both JSON files are unchanged.
10. Set `enforce` to `advice`, then input A: exit code 0 and no deny decision in stdout.
11. Set `enforce` to `block`, change the line under `## Do` to `Run it twice.`, then input A: stdout has a deny
    decision and `Give the source an alias`, and does not contain `Run it twice`.
12. Change `using` to `usin` in the lesson's pattern, then input A: exit code 0 and no deny decision in stdout.
13. Restore the lesson file. Input A with `tool_name` `Read` and `tool_input`
    `{"file_path":"MERGE INTO t USING (SELECT 1)"}`: exit code 0 and no deny decision in stdout. The lesson's `when`
    block names the tool `Bash` alone, and a `block` rule denies only a call its `when` block matches.
14. In the lesson file set `when: {tool: [Bash], command: [dbsql, databricks]}`, then input A: exit code 0 and no
    deny decision in stdout. The `when` block still matches the call but no longer equals the approved one, and
    a changed `when` ends the approval as a changed pattern does.

Person checks, for the owner to run in a terminal with the tester's `HOME`, `USERPROFILE`, `TQ_HOME` and
`CLAUDE_CONFIG_DIR` exported (`confirm`, `approve`, `allow-once` and `retire` answer only to a person). The tester
prepares each state and pipes the inputs; the owner types the commands.

1. From phase 2. With `ladder.json` as check 2 wrote it, `tentaqles lobe lessons confirm l-4c7d19e2`: exit code 0;
   `ladder.json` has `"stage":"advice"`; `ladder-log.jsonl` has a line with `"why":"confirmed"` and `"by":"person"`.
2. From phase 3. The tester restores the lesson file, runs `tentaqles lobe rebuild`, deletes `approvals.json` and
   `trials.jsonl`, sets `enforce` to `block` and writes `ladder.json` with the entry at `"stage":"advice"`, the three
   counts 0 and `"approved_at":null`. Input B, piped to `tentaqles lobe-hook claude post-tool-use-failure`, is input
   A with `hook_event_name` `PostToolUseFailure` and the added key `"error":"source needs an alias"`. The tester
   pipes input A and then input B five times, with `tool_use_id` `p1` to `p5`, the first three pairs with
   `session_id` `pa` and the last two with `pb`. Then `tentaqles lobe rules approve l-4c7d19e2` prints the pattern,
   the `when` block, the title, the two sentences and E-03's results, and asks. Type `yes`: exit code 0;
   `ladder.json` has `"stage":"block"` and `approvals.json` holds the pattern and, under `when`, the lesson's
   `when` block.
3. Input A with `session_id` `pc`: stdout has a deny decision. `tentaqles lobe rules allow-once l-4c7d19e2`, then
   input A twice: the first has no deny decision, the second has one.
4. `tentaqles lobe rules retire l-4c7d19e2`: `ladder.json` has `"stage":"retired"`, and input A with `session_id`
   `pd` gives exit code 0 and empty stdout.

**Risks and open questions**

- The person check is a hint, not a boundary: an agent with a shell can write `approvals.json` by hand (see F-05).
- On Windows the parent walk sees program names only (`node.exe` hides an agent). No Codex marker is confirmed yet.
- A `block` rule earns a false alarm only through an override, or while a cap holds it at advice. Should an approval
  end after a number of denies?
- A quoted mention of an approved pattern is denied (step 2). A person who wants one mention left alone writes it
  as a `must_not_match` line, and the step to `block` is refused until the pattern no longer matches it (E-03).
  Matching only what the command runs would need a reader that E-03's examples and the commit check share; it is
  left for a later version.
- A stale `block` rule only advises, so the mistake it stopped can run again until the code it points at is back or
  the person replaces the lesson (C-06). The stale flag is cached, so it clears at the next detached pass, not at
  the moment the code returns.

## E-02 Silent trial

**Phase** 2 · **Origin** idea: BASE · **Depends on** F-01, S-01, S-03, S-04, S-05, S-08, R-04, E-01, E-07

**What it is.** A fire is one moment when a rule matched a call about to run. On trial the agent is shown nothing; the
lobe records each fire and what the call then did, and that record alone decides whether the rule moves up. Fires at
`advice` and `block` go into the same file, so one record serves every stage.

**Why it matters.** A lesson drafted from one failure is often too broad. The trial lets it be wrong at no cost.
Without it, the first sign of a wrong rule would be a wrong note or a wrong block in front of the agent.

**Mechanism**

1. At PreToolUse, after the answer is written, a lesson stored at `trial`, `advice` or `block` fires when its
   `avoid.pattern` matches the call text, built by `sense.CallText` as in E-01 step 1, and its `when` block, when
   it has one, matches the call (`by: avoid`) or, if it has no `avoid` block, when R-04's matcher selects it for
   this call (`by: when`). A built-in starter rule (C-08) fires when C-08's own matcher selects it, and is
   recorded in the same way. Only a `trial` fire is silent. The others also record what E-01 did (`act`),
   because E-01 counts hits at `advice` and E-07 needs the result of an overridden call. Under `enforce: off` or
   `lessons: off` nothing fires: no fire is opened and no trial line is written (core page 5.6).
2. The fire goes into `open_fires` in `sessions/<session>.json` with the lesson, stage, `by`, tool, `tool_use_id` and
   `call` (`c_` plus the first 16 hex characters of the SHA-256 of the call text; the text is not stored). Proposed
   limits: 5 fires per call, 20 `by: when` fires per lesson per session, 25 ms; past one, the fire is dropped.
3. The result arrives on a post-tool event: PostToolUse or PostToolUseFailure in Claude Code, the one post-tool
   event in Codex (S-02). Sense runs first: it classifies and fingerprints the result, appends the event line
   (S-01, S-03, S-04, S-08) and hands over a `sense.Outcome`. Then the hook finds the open fire by the outcome's
   `CallID` (the `tool_use_id`), or by `tool` and `call`, oldest first, and judges it from the outcome's kind and
   fingerprint. An outcome whose state is `dropped` is judged too: its kind and fingerprint are known even though
   no line was written.

   | Fire | Result of the call | Verdict |
   |---|---|---|
   | `by: avoid` | `ok` | `false_alarm` |
   | `by: avoid` | `fail` or `hidden-fail`, and the lesson lists no fingerprint or lists this one | `hit` |
   | `by: when` | `fail` or `hidden-fail` with a fingerprint the lesson lists; or `ok` | `hit`; or `pass` |
   | either | a failure with a fingerprint the lesson does not list; or no result within 10 minutes (proposed) | `unrelated`; or `unknown` |

4. The hook appends the fire with `verdict`, `event` and `judged_at` to `trials.jsonl` and removes it from the session
   file, within 30 ms (proposed). `trials.jsonl` holds every fire at `trial`, `advice` and `block`, not only the
   silent ones. Only `hit` and `false_alarm` change the ladder.
5. After the turn, the detached pass started at Stop runs consolidation, then `ladder.Settle`, then the graph build
   (core page 5.2). `Settle` closes fires open for 10 minutes as `unknown`, redoes each lesson's counts from its lines
   since its last move, and hands E-01's promotions to E-07's `apply`.
6. On a post-tool event the event line is appended first and measured against the capture budget; the whole process
   is cut off at 1.5 seconds (core page 5.2). A hook cut off before step 1, 2 or 4 loses the fire: one data point
   and nothing else.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| BASE (FSL-1.1) | idea only | [`src/shadow/run.rs` L55](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/run.rs#L55): a candidate runs beside the live matcher, and only what it would have added or dropped is recorded. [L39](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/run.rs#L39) and [`src/config.rs` L398](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/config.rs#L398): it has 50 ms, and past that the event is marked skipped ([L62](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/run.rs#L62)) |
| BASE (FSL-1.1) | idea only | [`src/usage.rs` L391](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/usage.rs#L391): what happened next is the judge; there, a correction on the same or the next turn |
| BASE (FSL-1.1) | weakness, not taken | [`src/shadow/report.rs` L211](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/report.rs#L211): only `typed` events fill the promotion gate, and [`src/usage.rs` L865](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/usage.rs#L865) classes a prompt logged without its text as `textless`, so with text logging off the gate never fills |
| tentaqles (new) | new | the tool result as the judge, one record per lesson, the verdict table |

**How to build it**

- Package `internal/lobe/trial`: `fire.go`, `judge.go` (the pure table of step 3), `journal.go`, `settle.go`.
  `sense.CallText` (S-01) builds the call text on both sides, so the hooks before and after a call get the same
  hash. `judge.go` takes a `sense.Outcome`; for one whose state is `dropped` the verdict line's `event` names no
  stored line.
- A `trials.jsonl` line, kept 180 days (proposed): `{"v":1, "id", "at", "session", "host", "lesson", "stage", "by",
  "act", "tool", "call", "verdict", "event", "judged_at"}`. `stage` is the stored stage and `act` is `none`, `note`,
  `deny` or `pass`, so a rule that a cap held back shows as `block` with `note`. `event` is the `id` of the event
  line; it is read together with `session`, because an event id is unique within a session only (core page 5.4).
- `ladder.Counts(lesson)` redoes a lesson's counts from its `trials.jsonl` lines since its last move. `Settle` and
  `tentaqles lobe rules` both call it, so the list is current before `Settle` has run.
- Edge cases: of two identical calls, the oldest open fire is judged first. A dead session's open fires are closed by
  the next `Settle`. A denied call never runs, so its fire closes as `unknown`. With `capture: off` (F-06) no fire is
  opened (proposed), because no result would come back to judge it. With `recall: off` nothing is printed, so a fire's
  `act` is `none` unless it is a deny, and it is judged like any other. A verdict from a session F-05 marked as
  tampered moves no rule and is left out of the counts.
- The limit of the record: a lesson with neither an `avoid` block nor a fingerprint cannot earn a hit. That is
  most correction lessons, and standing lessons not written by the person. Such a lesson waits at its entry stage
  until the person confirms it (`tentaqles lobe lessons confirm <id>`, E-01). It is surfaced in two places:
  R-05's brief prints one line with the number of lessons waiting and the command, and
  `tentaqles lobe lessons --waiting` lists them.
- A repair that contradicts its lesson: when a new repair has the fingerprint of an active lesson and the working
  fix matches that lesson's `avoid.pattern` (what the lesson says to avoid is what worked), the fire on the
  working call is `by: avoid` with the result `ok`, so this entry records a `false_alarm`. C-05 does not reinforce
  the lesson, E-07 demotes it, and the pair becomes an ordinary candidate that C-03 words as a successor carrying
  `supersedes`; C-06 files the replacement once the successor passes the gate. When the working fix does not
  match the pattern, C-05 reinforces the lesson as usual.
- Where we differ: BASE judges one machine-wide setting by typed corrections, which needs stored prompt text; we judge
  each lesson by its call's result. The core page's hook table (5.2) carries both of this entry's steps: the verdict
  on the two post-tool events, after the append, and the ladder moves in the detached pass at Stop.

**How we know it works**

- `TestLobeTrial_VerdictTable`: the 6 outcomes of step 3 give the 6 expected verdicts.
- `TestLobeTrial_NoCallText`: after 100 fires on commands holding a marker, the marker is stored 0 times.
- `TestLobeTrial_EveryStageRecorded`: one fire each at `trial`, `advice` and `block` gives 3 lines, with `act`
  `none`, `note` and `deny`.
- `TestLobeTrial_OffRecordsNothing`: under `enforce: off`, and again under `lessons: off`, 10 matching calls open 0
  fires and write 0 lines.
- `TestLobeTrial_AfterTheAppend`: on both post-tool events the event line is whole in the log before the verdict
  line is written: 2 of 2.
- `TestLobeTrial_DroppedOutcomeIsJudged`: a `fail` outcome whose state is `dropped` closes its open fire with the
  verdict `hit`, and no event line exists: yes.
- `TestLobeTrial_StarterFireRecorded`: a starter rule that C-08's matcher selects writes 1 trial line; under
  `recall: off` it prints nothing and still writes the line.
- `TestLobeTrial_WorkingFixThatMatchesAvoid`: after a failure with a fingerprint the lesson lists, a call that
  matches the lesson's `avoid.pattern` and works is judged `false_alarm`, and the lesson is demoted: yes.
- `TestLobeTrial_NoHitWithoutAvoidOrFingerprint`: a lesson with neither an `avoid` block nor a fingerprint earns
  0 hits over 10 matching failed calls and stays at its entry stage: yes.
- `TestLobeTrial_BudgetDropsFire`: with the session write stalled 200 ms, the hook exits within 50 ms of its answer.
- M-06: with no open fire, the 95th percentile of a post-tool event's append rises by less than 2 ms (proposed).

**Tester brief**

Start from the standard tester setup in F-07. `<acme>` is `$W/clients/acme` and `<lobe>` is
`$TQ_HOME/identities/acme/lobe`. Run every command
inside `<acme>`, after `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`: a lobe hook acts only when the
session's login folder is the client's own (core page 5.1). Backticked text split over two lines is one line: join
it with a space. Write `<lobe>/lessons/l-4c7d19e2.md`:

```markdown
---
id: l-4c7d19e2
title: Alias the MERGE source
kind: lesson
source: person
scope: private
applies: client
origin: {by: me, host: person, drafted: person}
when: {tool: [Bash], command: [dbsql]}
avoid:
  pattern: '(?i)merge into \S+ using \('
  must_match: ["MERGE INTO t USING (SELECT 1)", "merge into dim using (select 2)"]
  must_not_match: ["MERGE INTO t USING s", "MERGE INTO t USING src AS s"]
strength: 1.0
seen: 3
created: 2026-11-09
---
## Do
Give the source an alias.
## Avoid
A subquery as the source.
## Why
The warehouse rejects it because the source has no alias.
```

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if it does not, stop and report FAIL.
The hooks see the file without `tentaqles lobe rebuild` (R-04). A lesson that a person wrote
enters at `advice`, so put it on trial by hand. `<lobe>/ladder.json`: `{"v":1, "entries":[{"lesson":"l-4c7d19e2",
"stage":"trial", "hits":0, "false_alarms":0, "sessions":0, "approved_at":null}]}`. Input A, piped to
`tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1", "hook_event_name":"PreToolUse", "cwd":"<acme>",
"tool_name":"Bash", "tool_use_id":"t1", "tool_input":{"command":"dbsql -e 'MERGE INTO t USING (SELECT 1)'"}}`.
Input B, piped to `tentaqles lobe-hook claude post-tool-use-failure`, is input A with `hook_event_name`
`PostToolUseFailure` and the added key `"error":"source needs an alias"`. Input C, piped to
`tentaqles lobe-hook claude post-tool-use`, is input A with `hook_event_name` `PostToolUse`, `tool_use_id` `t2` and
the added key `"tool_response":{"stdout":"1 row"}`.

1. Input A: exit code 0, empty stdout; `<lobe>/sessions/s1.json` has 1 entry in `open_fires`, with `"by":"avoid"`.
2. Input B: exit code 0; `<lobe>/trials.jsonl` has exactly 1 line, with `"verdict":"hit"`; `open_fires` is empty.
   That line's `event` value is the `id` of a line with `"kind":"fail"` and `"session":"s1"` in today's file under
   `<lobe>/events/`.
3. Input A with `tool_use_id` `t2`, then input C: line 2 of `trials.jsonl` has `"verdict":"false_alarm"`.
4. Neither `trials.jsonl` nor `sessions/s1.json` contains the text `SELECT 1`.
5. `tentaqles lobe rules --json`: the entry for `l-4c7d19e2` has `"stage":"trial"`, `"hits":1`, `"false_alarms":1`.
6. Run `lobeset acme enforce off`, the setup's helper (`tentaqles lobe set` answers only to a person and no
   tester runs it). Input A with `session_id` `s2` and `tool_use_id`
   `t3`, then input B with the same two values: both exit with code 0 and empty stdout; `trials.jsonl` still has
   exactly 2 lines, and `<lobe>/sessions/s2.json` is absent or has no entry in `open_fires`.

**Risks and open questions**

- The judge trusts Sense: if S-04 reads a failure as a success, a good rule gets a false alarm. M-05 counts it.
- With no fingerprint listed, any failure of a matching call is a hit. C-03 should always fill `when.fingerprint`.
- Whether both hosts send `tool_use_id` before and after a call is not confirmed (phase 0, question 2).
- The verdict is extra work on the two post-tool events, the path that must do the least. It comes after the
  append, has 30 ms (proposed) and loses only the fire when it runs out. M-06 measures those events both ways.

## E-03 Test cases on every rule

**Phase** 3 · **Origin** idea: BASE · **Depends on** S-05, S-07, C-04, E-01, E-02, E-07

**What it is.** Every `avoid` block carries examples: `must_match` lists calls its pattern must match and
`must_not_match` lists calls it must not. The lobe runs them through the same matcher the hook uses, when a rule is
created, when it changes and before any promotion. A rule that fails its own examples can neither warn by pattern
nor block.

**Why it matters.** The goal says a call is blocked only when a rule is exact. A regular expression drafted from one
error is the likeliest source of a wrong block, and without tests "exact" is an opinion.

**Mechanism**

Numbers are proposed unless a source is named.

1. Shape. The pattern compiles with Go's `regexp`, has at most 200 characters and does not match the empty string.
   Each example has at most 1,000 characters and comes out of S-05's scrubber unchanged, so an example that holds a
   secret fails.
2. Examples. The pattern matches every `must_match` and no `must_not_match`. An `avoid` block needs at least 1 of
   each to exist and at least 2 of each to reach `block`. At most 5 of each are read (BASE reads at most 3 and 2).
3. Breadth. The pattern must match none of 50 everyday calls shipped in the binary (`git status`, `ls -la`,
   `npm test`, `SELECT 1` and the like). The 200 characters and the list of 50 are the ones C-04's gate ships in
   phase 2, so a pattern meets one limit and one list in every phase.
4. Replay, for promotion to `block` only. The pattern is run over the client's stored events inside the retention
   window (S-07), newest 5,000. It must match the call text of at least 1 `fail` event and of 0 `ok` events. Examples
   written by the hand that wrote the pattern only show that the two agree; replay tests against what happened.
5. Whole call text. Every step tries the pattern on the whole call text, as the hook does (E-01), so an example
   that only quotes the avoided form counts as a match. A person who wants such a mention left alone lists it under
   `must_not_match`; promotion is then refused until the pattern no longer matches it.
6. When they run. At creation, C-04's `pattern` check runs steps 1 to 3 with these numbers. It ships in phase 2
   with the gate; from phase 3 the gate calls this entry's package and its own copy goes away. After every turn,
   `ladder.Settle` (E-02) runs steps 1 to 3 for every lesson with an `avoid` block, which covers an edit by hand
   and a lesson pulled through git. Before promotion, `tentaqles lobe rules approve` runs steps 1 to 4 and only then
   asks for `yes`. On request, `tentaqles lobe rules test [<id>] [--replay]` runs them and exits 1 when any test
   fails.
7. On a failure E-07 moves the rule to `trial` at once and removes its approval. The failing example and what was
   expected go to `ladder-log.jsonl`.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| BASE (FSL-1.1) | idea only | [`src/domain/rules.rs` L517](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L517): a rule carries up to 3 inputs that must serve it and ([L519](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rules.rs#L519)) up to 2 that must not |
| BASE (FSL-1.1) | idea only | [`src/domain/rule_test.rs` L422](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rule_test.rs#L422): one command replays every stored example through the live matcher. [L425](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rule_test.rs#L425): a must-fire example is judged with itself held out of the rule's scoring text, so it cannot pass by matching itself |
| BASE (FSL-1.1) | weakness, not taken | [`src/domain/rule_test.rs` L314](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/domain/rule_test.rs#L314): every example is replayed as a prompt, so a rule that matches on a tool call or a path is never tested |
| Go `regexp` package | fact | https://pkg.go.dev/regexp (web, not machine-checked): matching time is linear in the size of the input, so a pattern cannot stall a hook |
| tentaqles (new) | new | the breadth list, replay against stored events, the scrub check |

**How to build it**

- Package `internal/lobe/rulecheck`: `check.go` (`Run(avoid, level) Result`), `everyday.go` (the embedded list),
  `replay.go`. The matcher is `ladder.Match(pattern, callText)`, the function the PreToolUse hook calls; there is no
  second copy, which is the gap in BASE's test command.
- Phase 3 moves the gate onto `rulecheck.Run` and its list of safe commands into `everyday.go`. C-04's `pattern`
  check and steps 1 to 3 are then the same code, called from the gate, `Settle`, `approve` and `rules test`.
- An example is a call text as E-01 step 1 defines it: the command string for a shell tool, the compact JSON of the
  tool input otherwise. Replay rebuilds the call text from each stored event's `input` (for a shell tool, its
  `command` value) and skips an event whose `input` was cut at the cap. An `ok` event that holds only S-03's
  projection is matched on what it holds.
- Edge cases: a lesson with no `avoid` block passes. Replay with no stored `fail` event fails with "no stored failure
  matches", and the person waits for the next hit. A machine draft whose only fault is its pattern is filed without
  the `avoid` block (C-04), so it can still advise through its `when` block. `rules test` reads the lesson file
  itself, not the hooks' index, so it sees an edit at once.
- Where we differ: BASE's examples are prompts and ours are calls. Its hold-out step has no meaning for a regular
  expression; replay takes its place as the guard against examples that only confirm themselves. Our key names,
  `must_match` and `must_not_match`, are our own.

**How we know it works**

- `TestLobeRulecheck_Examples`: 6 patterns with known answers give 6 of 6 expected results.
- `TestLobeRulecheck_Breadth`: `(?i)git` fails and names `git status`; the core page's sample pattern (5.4) matches
  0 of 50.
- `TestLobeRulecheck_SameMatcherAsHook`: for 50 pairs of pattern and text, `rulecheck` and the hook agree 50 times.
- `TestLobeRulecheck_SameAsTheGate`: for 20 `avoid` blocks, C-04's `pattern` check and steps 1 to 3 give the same
  20 answers.
- `TestLobeRulecheck_ReplayFindsSuccess`: 1 stored `ok` event that matches refuses the promotion: yes.
- `TestLobeRulecheck_QuotedMentionRefuses`: a `must_not_match` line that quotes the avoided form fails step 2: yes.
- `TestLobeRulecheck_SecretInExample`: an example holding a cloud access key fails at step 1: yes.
- `TestLobeRulecheck_SettleDemotes`: an edited `must_match` that the pattern misses leaves the rule at `trial`: yes.

**Tester brief**

Start from the standard tester setup in F-07. `<lobe>` is `$TQ_HOME/identities/acme/lobe`. Run every command inside
`$W/clients/acme`. Backticked text split over two lines is one line: join it with a space. Write
`<lobe>/lessons/l-4c7d19e2.md`:

```markdown
---
id: l-4c7d19e2
title: Alias the MERGE source
kind: lesson
source: person
scope: private
applies: client
origin: {by: me, host: person, drafted: person}
when: {tool: [Bash], command: [dbsql]}
avoid:
  pattern: '(?i)merge into \S+ using \('
  must_match: ["MERGE INTO t USING (SELECT 1)", "merge into dim using (select 2)"]
  must_not_match: ["MERGE INTO t USING s", "MERGE INTO t USING src AS s"]
strength: 1.0
seen: 3
created: 2026-11-09
---
## Do
Give the source an alias.
## Avoid
A subquery as the source.
## Why
The warehouse rejects it because the source has no alias.
```

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if it does not, stop and report FAIL.
Restore this file after each check.

1. `tentaqles lobe rules test l-4c7d19e2`: exit code 0.
2. Set `must_not_match: ["MERGE INTO t USING (SELECT 2)"]`, run it again: exit code 1; the output names
   `must_not_match`.
3. Set `pattern: '(?i)git'` and `must_match: ["git push"]`, run it again: exit code 1; the output names
   `git status`.
4. Set `pattern: 'merge ('`, run it again: exit code 1; the output says the pattern does not compile.
5. Delete the `must_not_match` line, run it again: exit code 1; the output names `must_not_match`.
6. Set `must_not_match: ["echo 'MERGE INTO t USING (SELECT 1)'"]`, run it again: exit code 1; the output names
   `must_not_match`. A quoted mention is a match, because the pattern sees the whole call text.
7. `tentaqles lobe rules test l-4c7d19e2 --replay` on the restored file: exit code 1; the output contains
   `no stored failure matches`.

**Risks and open questions**

- The everyday list is our guess at harmless calls; a client with other habits is covered by replay and the trial.
- Replay reads only S-07's retention window, so a slow-climbing rule may need one more hit on promotion day.
- S-03 stores a success as a projection of its input: the command for a shell tool, a reduced form for other
  tools, and the full input only for the success that follows a failure in the same session (core page 5.4). So
  replay can miss a success of a non-shell tool that matched. The advice record (E-02) is the cover for that.
- Go's `regexp` has no lookahead, so "not followed by" cannot be written. Such a rule stays at `advice`.
- A pattern cannot tell a call that runs the avoided form from one that only quotes it (step 5). A rule whose
  honest use is often quoted, in commit messages for example, will fail promotion and stay at `advice`.

## E-04 Sure-fail block

**Phase** 3 · **Origin** port: Trail-code · **Depends on** F-02, F-06, C-08, E-01, E-02, E-03, E-07

**What it is.** Some commands cannot succeed on this machine for a reason the lobe can prove before they run, such as
a program that is not installed. A sure-fail rule denies such a command and gives the fix. It is the one kind of block
that waits for neither a long record nor a typed `yes`: a proof on every call takes the record's place, and the
person's consent is the setting `enforce: block`.

**Why it matters.** The first row of the core page's table of mistakes (section 4) is a command sent to the wrong
shell, caught before it runs. Without this entry the agent pays one failed call in every session for an outcome that
was known.

**Mechanism**

1. Trail-code has exactly one such rule: it denies `timeout N ...` when this machine has no `timeout` program, and
   only when it is sure. The Source table gives each of its conditions with the line.
2. A starter rule (C-08) qualifies as sure-fail when all five hold:

   | Condition | Meaning |
   |---|---|
   | built in | it is in the catalog inside the binary; a lesson file, learned or pulled, never qualifies, and may not claim `kind: starter` or `source: starter` |
   | probe | Go code proves from this machine's files and environment that the matched command cannot succeed; it reads variables and file entries, starts no process, uses no network and has 50 ms (proposed) |
   | reader | it answers "not sure" for anything it does not fully understand: a command over 64 KiB (Trail-code's limit), a changed `PATH`, `source`, `.`, `eval`, `hash`, a function or alias with the probed name, or a mention of that name that is not a run of it |
   | tests | it passes E-03 with at least 3 `must_not_match` examples, each using one of the reader's ways out (proposed) |
   | fixed text | its fix text is a constant in the catalog; nothing from the command or its output is put into it |

3. It holds an ordinary ladder entry at `advice`, like every starter (C-08, E-01), and never a stored `block`. What
   it does at `advice` follows the `enforce` setting: under `advice` it gives its fix as a note, and under `block`
   it denies. It needs no typed `yes` of its own, no approval entry and no record of hits: setting `enforce` to
   `block` is the person's consent. It skips E-01's 5 hits and typed `yes` and E-03's replay, and nothing else.
4. At PreToolUse for a shell tool: the rule matches, the reader is sure, the probe proves it, and the hook denies
   (under `enforce: block`) or adds the fix as a note (under `enforce: advice`). A "not sure", an error or a timeout
   prints nothing. The proof also stands in for C-08's wait for a first matching error in the session, so the note
   may come on the first call. `recall: off` silences the note and leaves the deny: a sure-fail deny survives
   `recall: off` when `enforce` is `block` (core page 5.6). `enforce: off` silences both, and so does
   `lessons: off`, because a starter counts as a lesson for the switches (C-08). From phase 2 each fire is recorded
   by E-02, which uses C-08's own matcher.
5. The deny has E-01's form: the proved fact is the reason, `tentaqles lobe rules demote <id>` the way out. A
   demoted rule sits at `trial`, where it is silent, and climbs back only on E-01's record: 2 hits in 2 sessions and
   no false alarm. A wrong probe therefore cannot bring it back, because the call it would have denied succeeds.
6. The first rule is Trail-code's, with the id `timeout-missing`; C-08 owns the catalog. A cross-shell rule (a
   PowerShell cmdlet name in a Bash command) qualifies only when a probe can meet step 2.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | port: the decision and the reader | [`src/trace/guard-fast.ts` L166](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L166): deny only when sure, and nothing on any error ([L173](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L173)). [L118](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L118): every `timeout` word must be a run with a duration. [L95](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L95), [L101](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L101): an own function or alias, a changed `PATH` or loaded shell setup mean "not sure". [L44](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L44): 64 KiB |
| Trail-code (MIT) | port: the probe | [`src/trace/guard-fast.ts` L152](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L152): `PATH` folders, then 12 fixed ones ([L35](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L35)). [L129](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L129): on Windows each `PATHEXT` ending is tried |
| Trail-code (MIT) | port: the consent | [`src/trace/guard-fast.ts` L79](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L79): enforced only while the kept file, at most 4 KiB ([L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L42)), says active, `no-timeout`, `sure-fail`. [`src/trace/walls.ts` L74](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/walls.ts#L74): the only catalog entry with a `check` |
| Trail-code (MIT) | weakness, not taken | [`src/trace/guard-fast.ts` L26](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L26): the text says "this Mac" on every system, and the fixed folders are Unix paths. [`src/claude/pre-bash.ts` L270](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash.ts#L270): a commit warning returns before the sure-fail check runs |
| tentaqles (new) | new | the five conditions of step 2; the ladder and the `enforce` setting in place of a kept file |

**How to build it**

- Package `internal/lobe/surefail`: `surefail.go` (`Decide(call, env)`), `reader.go`, `probe.go`. The sure-fail
  rules are a table that `surefail.Rules()` builds on first use from C-08's catalog, each with its reader and its
  probe. Nothing registers itself: a lobe package has no `init` function and no package-level call (core page 5.1).
- The rule has no entry in `approvals.json`. Its pattern and text are constants in the binary, which an edit through
  git cannot change, and nothing was typed for it. Its fires go to `trials.jsonl` like any other (E-02), with the
  stored stage `advice` and the `act` `deny` or `note`.
- E-03 runs a sure-fail rule's examples through `surefail.Decide` with the probe answering "missing", not
  through the pattern alone, so each of the reader's ways out is tested by the function the hook calls.
- `probe.ProgramMissing` looks in every absolute `PATH` folder (on Windows with each `PATHEXT` ending), then beside
  the first `bash.exe` on `PATH`: that folder, `../usr/bin`, `../mingw64/bin`. No `bash.exe` on Windows means "not
  sure".
- The reader splits a command into words with `internal/lobe/shellwords`, the lobe's own splitter, which is tested
  against the same cases as the guard's (core page 5.1). R-04, C-08 and S-04 use the same package. No lobe package
  imports `internal/guard`.
- Where we differ: the `enforce` setting and the ladder stand where Trail-code reads a kept file. Our folders come
  from the shell that will run the command, not from a macOS list. All our checks run, and a deny wins.

**How we know it works**

- `TestLobeSureFail_TimeoutMissing`: no `timeout` anywhere gives a deny; one in a `PATH` folder gives none: 2 of 2.
- `TestLobeSureFail_WaysOut`: 10 commands, one per way out in the reader row (9, a function and an alias counted
  apart) and `gtimeout 5 x`, give 0 denies.
- `TestLobeSureFail_FollowsTheSetting`: on a new store with no `approvals.json`, the same call is denied under
  `enforce: block`, gets the fix as a note under `advice`, and gets nothing under `off`: 3 of 3.
- `TestLobeSureFail_SurvivesRecallOff`: with `recall: off` the call is denied under `enforce: block`, gets no note
  under `advice`, and gets nothing under `block` with `lessons: off`: 3 of 3.
- `TestLobeSureFail_FileCannotQualify`: a lesson file that claims `kind: starter`, and one that claims
  `source: starter`, never deny: 2 of 2.
- `TestLobeSureFail_ProbeBudget`: with the probe stalled 2 seconds the hook answers by its cut-off with no deny.
- M-05: 0 wrong denies over its command set, on each of the three systems.

**Tester brief**

Start from the standard tester setup in F-07. `<acme>` is `$W/clients/acme`, `<lobe>` is
`$TQ_HOME/identities/acme/lobe` and `<work>` is `$W`.
Backticked text split over two lines is one line: join it with a space. Create the empty folder `<work>/bin`; on
Windows put an empty file `bash.exe` in it. Run every hook inside `<acme>` with `PATH` set to `<work>/bin` alone
and `CLAUDE_CONFIG_DIR` set to `$TQ_HOME/identities/acme/claude` (a lobe hook acts only when the session's login
folder is the client's own, core page 5.1), calling the `tentaqles` binary by its full path. "Set `enforce` to X"
means: run `lobeset acme enforce X`, the setup's helper, with the shell's ordinary `PATH`; it writes the setting
into the sandbox manifest as a fixture. `tentaqles lobe set` answers only to a
person and no tester runs it. Set `enforce` to `block`. Write no `ladder.json` and no `approvals.json`: the rule is
built in. Input A, piped to `tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1",
"hook_event_name":"PreToolUse", "cwd":"<acme>", "tool_name":"Bash", "tool_use_id":"t1",
"tool_input":{"command":"timeout 60 npm test"}}`.

1. Input A: exit code 0; stdout has `"permissionDecision":"deny"` and `tentaqles lobe rules demote timeout-missing`.
   `<lobe>/approvals.json` does not exist.
2. Input A with the command `command -v timeout && timeout 60 npm test`: exit code 0, no deny decision.
3. Input A with the command `PATH=/opt/x/bin:$PATH timeout 60 npm test`: exit code 0, no deny decision.
4. Create an executable file `timeout` in `<work>/bin` (on Windows, `timeout.exe`), then input A: no deny decision.
5. Delete that file and set `enforce` to `advice`, then input A with `session_id` `s2`: exit code 0, no deny
   decision, and stdout contains `timeout` (the fix, as a note).
6. Set `enforce` to `off`, then input A with `session_id` `s3`: exit code 0 and empty stdout.
7. `tentaqles lobe rules --json`: the entry for `timeout-missing` has `"stage":"advice"`.
8. Run `lobeset acme enforce block` and `lobeset acme recall off`, then input A with `session_id` `s5`: stdout has
   `"permissionDecision":"deny"`. A sure-fail deny survives `recall: off`. Then run `lobeset acme recall on`.

Person checks, for the owner to run in a terminal with the tester's `HOME`, `USERPROFILE` and `TQ_HOME` exported
(`tentaqles lobe rules demote` answers only to a person). The tester sets `enforce` to `block` first and pipes the
input.

1. `tentaqles lobe rules demote timeout-missing`: exit code 0; `<lobe>/ladder.json` has `"stage":"trial"` for the
   rule, and `<lobe>/ladder-log.jsonl` has a line with `"why":"person"`.
2. Input A with `session_id` `s4`: exit code 0 and empty stdout. The demoted rule is silent.

**Risks and open questions**

- A login profile can add `PATH` folders the probe never sees. The rule then denies a command that would have run;
  the deny names `demote`, and the `enforce` setting is one step away.
- Which `bash.exe` each host runs on Windows is unconfirmed, and Trail-code's rule on Windows was read, not run.
- The source proves one rule. Every further sure-fail rule is our own design and needs its probe reviewed.
- Under `enforce: block` the rule denies from its first match on a machine, with no record there. The proof on
  every call and M-05's zero wrong denies are what stand in for the record.

## E-05 Argument check from the graph

**Phase** 4 · **Origin** new · **Depends on** F-02, F-06, G-01, G-02, G-03, G-04, G-08, G-09, R-04

**What it is.** When the agent has just written code that calls a function in the client's repository, the lobe
compares the number of arguments in the call with the function's stored signature (G-04). If the count cannot be
right, the agent gets one line of advice that shows the signature, right after the edit. It is a note and never a
deny.

**Why it matters.** The core page's table of mistakes (section 4) lists a wrong number of arguments to a function in
the repository as caught by the graph, with no learning. Without this entry the mistake is found when the code runs,
often several calls later. The note arrives while the agent is still working on that file.

**Mechanism**

1. The trigger is PostToolUse for a tool that wrote a file (`Write`, `Edit`, `MultiEdit`, or Codex `apply_patch`),
   after the event line is appended (S-03) and the edited file is marked for the graph (G-08). It needs `graph` on,
   `recall` on, `enforce` not `off`, a built graph for the repository and a language whose counts G-04 stores. The
   `lessons` setting plays no part, because this is not a lesson.
2. G-09 parses the edited file once at this moment, as it now is on disk, to find what the edit changed. This check
   reads the same tree: there is no second parse, and nothing is rebuilt in memory. It keeps the call expressions
   that overlap G-09's changed ranges (each inserted text as found in the file; a text that is missing or occurs
   twice makes the whole file the range): at most 20 calls, in file order, in a file of at most 512 KiB (both
   proposed).
3. For each call it asks G-03 for the target, resolving the name through the fresh tree's imports against the
   stored graph. It goes on only when G-03 returns exactly one target at its highest confidence level. A guess, an
   inferred target or several candidates ends the check for that call.
4. It skips a call whose count cannot be trusted: a count of -1 by G-04's rules (a spread or unpacking argument
   such as `*xs`, `**kw` or `...xs`; in Go, a single argument that is itself a call); a target that is a class; a
   target in another file that a G-08 marker names, because the graph has not read that file's new text; overloads
   of which one has no stored count. A target defined in the edited file is counted from the fresh tree.
5. G-04 gives `min` (required parameters), `n` (required plus optional), `variadic` and `check`, counted as a caller
   sees them. A target whose `check` is false is skipped. It is a mismatch when G-04's fit rule fails: the call
   passes fewer than `min`, or more than `n` and the target is not variadic. With overloads it is a mismatch only
   when no overload accepts the count.
6. After an edit, G-09 and this check answer in one `additionalContext`: this check's notes first, then the blast
   list, and when the text is too long the list is cut first. The 700 characters, R-04's budget, are the budget
   inside F-05's frame; the frame adds its own fixed text. A note reads: "Argument count: add in lib.py takes 2
   arguments (def add(a, b)); the call at main.py:2 passes 3." At most 3 notes per edit (proposed), each once per
   session.
7. It never denies, at any setting, and the event it runs on cannot stop a call. It is not a lesson, has no ladder
   entry and writes no trial line. It has 300 ms (proposed) inside the 1.5 second cut-off of the post-tool events
   (core page 5.2); when that runs out, the answer goes out without its lines.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | partial port: what to store | [`src/graph/extract.ts` L1677](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L1677): the count of declared parameters, kept for Java methods and constructors only. [L1919](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L1919): Swift, parameters minus defaults |
| Trail-code (MIT) | port: the count rule | [`src/graph/resolve.ts` L445](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L445): a variadic target accepts one less than its count and upward; any other must be equal |
| Trail-code (MIT) | limit | [`src/graph/extract.ts` L713](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/extract.ts#L713): the call's argument count exists for Java and Swift only |
| Trail-code (MIT) | weakness, not taken | [`src/graph/resolve.ts` L447](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L447): when no candidate fits the count the whole set comes back and nobody is told. [L478](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/resolve.ts#L478): the first candidate in the same file is then bound as certain |
| Graphify (Apache-2.0) | port: the lower bound | [`graphify/extractors/engine.py` L242](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L242): required parameters, one less for an extension method (C# only). [L287](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L287): the fit test |
| Graphify (Apache-2.0) | weakness, not taken | [`graphify/extractors/engine.py` L10609](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/extractors/engine.py#L10609): when the count picks no overload the call is bound to the first one declared |
| tentaqles (new) | new | reporting a mismatch, the certainty rule, and the moment: after the edit, on the parse G-09 already made |

**How to build it**

- Package `internal/lobe/argcheck`: `argcheck.go` (`Check(tree, ranges, graph) []Note`), `calls.go` (call
  expressions in the changed ranges, node names from G-02's query files) and `overloads.go` (the overload rule of
  step 5). This package has no counting code of its own: a call's count comes from G-04's `CountArgs`, the code
  that fills `argc` on an edge, and the fit rule is G-04's `Fits` in `internal/lobe/graph`, its single copy.
- The `post-tool-use` handler calls it with the tree and the ranges G-09 already has, after the dirty marker is
  written (G-08) and the blast walk is done. A cut-off therefore costs this check first, and never the event line
  or the marker.
- It reads `params` (`n`, `min`, `variadic`, `check`) and `sig` from G-04 and the target with its confidence from
  G-03, through R-01's index as G-09 does, never the whole `graph.jsonl`. It writes only what it showed, to
  `sessions/<session>.json`.
- Edge cases: a new file is one range, the whole file. A parse error in the changed ranges ends the check. A Codex
  patch that names several files is checked file by file, 20 calls in all. No graph, or `graph: off`, means no
  note. G-04 sets `check` to false where a count misleads, as for a decorated function.
- A language's notes are switched on only once its counting table has met M-05's two numbers on replayed edits
  (proposed). Until then the check runs for that language and prints nothing. G-04's tables are new, and this
  check has no ladder entry on which they could prove themselves.
- Where we differ: neither source reports a mismatch; both use the count only to choose between overloads (Java,
  Swift, C#) and guess when nothing fits. We report, and only when the target is certain.
- Why after the edit and not before it (core page 5.2): the file on disk is already the new text, so nothing is
  rebuilt in memory, and G-09's parse is reused instead of a second one. The price is that the wrong call is in the
  file when the note arrives.

**How we know it works**

- `TestLobeArgcheck_CountRule`: 10 cases of `min`, `n`, `variadic` and a count, counted by G-04's `CountArgs` and
  judged by its `Fits`, give 10 of 10 expected answers.
- `TestLobeArgcheck_NotesBeforeTheList`: an edit whose notes and blast list pass the budget keeps every note,
  ahead of the list, in one `additionalContext`, and loses blast lines: yes.
- `TestLobeArgcheck_NoTrialLine`: after 5 mismatches `trials.jsonl` and `ladder.json` are unchanged: yes.
- `TestLobeArgcheck_OnlyWhenCertain`: two functions of one name that G-03 cannot tell apart give 0 notes.
- `TestLobeArgcheck_SpreadSkips`: `f(*xs)` gives 0 notes.
- `TestLobeArgcheck_StaleTargetSkips`: a target whose file a dirty marker names gives 0 notes.
- `TestLobeArgcheck_OneParse`: one edit that yields a blast list and a mismatch parses the edited file 1 time.
- `TestLobeArgcheck_NeverDenies`: with `enforce: block` and a mismatch, stdout holds no deny decision: yes.
- `TestLobeArgcheck_Budget`: with the check stalled 2 seconds, the event line and the marker are written and the
  answer goes out by the cut-off without an argument note.
- M-05 (both numbers proposed): at least 95 in 100 printed notes name a real mismatch, and 1,000 replayed correct
  edits print 0 notes.

**Tester brief**

Start from the standard tester setup in F-07. `<acme>` is `$W/clients/acme`.
Run `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"` first: a
lobe hook acts only when the session's login folder is the client's own (core page 5.1). Backticked text split over
two lines is one line: join it with a space. Create the folder `<acme>/shop`, run `git init` in it, and write
`lib.py` with the two lines `def add(a, b):` and `    return a + b`, and `main.py` with the two lines
`from lib import add` and `print(add(1, 2))`. Run every command inside `<acme>/shop`. Build the graph with
`tentaqles lobe graph build`. Input A, piped to `tentaqles lobe-hook claude post-tool-use`: `{"session_id":"s1",
"hook_event_name":"PostToolUse", "cwd":"<acme>/shop", "tool_name":"Edit", "tool_use_id":"t1",
"tool_input":{"file_path":"<acme>/shop/main.py", "old_string":"add(1, 2)", "new_string":"add(1, 2, 3)"}}`.

Two short forms are used below. "Edit to T as S" means: write line 2 of `main.py` as `print(T)`, as the edit tool
would have left it, then pipe input A with `session_id` S and `new_string` T. "Set KEY to X" means: run
`lobeset acme KEY X`, the setup's helper, which writes the setting into the sandbox manifest as a fixture.
`tentaqles lobe set` answers only to a person and no tester runs it.

1. Edit to `add(1, 2, 3)` as `s1`: exit code 0; stdout is one JSON object whose `additionalContext` contains
   `Argument count`, `takes 2` and `passes 3`; stdout holds no `permissionDecision`.
2. Edit to `add(3, 4)` as `s2`: stdout does not contain `Argument count`.
3. Edit to `add(*[1, 2, 3])` as `s3`: stdout does not contain `Argument count`.
4. Edit to `add(1, 2, 3)` as `s1` again: stdout does not contain `Argument count` (once per session).
5. Write line 1 of `lib.py` as `def add(a, b, c):` and pipe input A with `session_id` `s4`, `file_path`
   `<acme>/shop/lib.py`, `old_string` `def add(a, b):` and `new_string` `def add(a, b, c):`. Then edit to
   `add(1, 2, 3)` as `s5`: stdout does not contain `Argument count`, because the graph has not read the new `lib.py`.
6. `tentaqles lobe graph build`, then edit to `add(1, 2)` as `s6`: stdout contains `Argument count`, `takes 3` and
   `passes 2`.
7. Set `enforce` to `block`, then edit to `add(1, 2)` as `s7`: stdout contains `Argument count` and no
   `permissionDecision`.
8. Set `recall` to `off`, then edit to `add(1, 2)` as `s8`: exit code 0 and empty stdout.
9. Set `recall` to `on` and `graph` to `off`, then edit to `add(1, 2)` as `s9`: exit code 0 and empty stdout.

**Risks and open questions**

- Everything rests on G-04 storing `min` and `n` for languages with defaults, keyword arguments and receivers. The
  sources cover three languages, so the first list may be short.
- The parse time of G-01's runtime inside a hook is unknown until the phase 0 spike. The parse is G-09's, so this
  check adds only the walk over the calls and the lookups. If G-09 has to give up its parse, this check prints
  nothing.
- The note comes after the edit, so the wrong call is already in the file. Checking before the edit would need a
  parse of text built in memory, which the time before a tool does not allow.
- In a dynamic language a target can be replaced at run time; M-05 measures how often the note is wrong.

## E-06 Check before a commit

**Phase** 3 · **Origin** port: Trail-code · **Depends on** F-02, F-04, F-06, C-04, R-04, R-06, E-01, E-03

**What it is.** Before the agent runs `git commit`, the lobe compares the staged change with the client's lessons
using plain text rules. If the change goes against a lesson, the agent is told which one and where, and the commit
still runs. It warns and never blocks.

**Why it matters.** A lesson about code has no tool call to match, and the commit is the last cheap moment to notice.
Trail-code built this as a stop and switched it off: `SHIP_CHECK_STOPS` is `false`, with the comment that the check
"still flags word matches as conflicts (4 on PR #706, none real)". We start where it ended.

**Mechanism**

1. The trigger is PreToolUse for a shell tool whose command runs `git commit` (git's own options may come before the
   word), found with R-04's command test. The first version covers `git commit` only: `git push` and
   `gh pr create` are not checked.
2. The hook reads `git diff --cached --unified=3`, plus tracked changes when the command has `-a`, within 800 ms
   (proposed) and 400 KiB (Trail-code's cap). Files under `.tentaqles/` are left out of step 4 and go to step 5.
3. Candidates are lessons acting at `advice` or `block`: first those whose `touches` or `when.place` names a changed
   file or its folder, then those sharing at least 2 terms with added lines, up to 25 (Trail-code's). A stale lesson
   (R-06) stays a candidate, ranked after the fresh ones of its group, and its finding carries R-06's mark.
4. Three rules run per changed file and candidate, strongest first. Pattern: the lesson's `avoid.pattern` matches an
   added line, or the joined added lines of one hunk. Value: a removed and an added line share an identifier and
   differ in a number, and `## Avoid` names the identifier with the new number or `## Do` names it with the old number
   only (digits only). Words: a sentence under `## Avoid` shares 2 of its terms (3 when it has more than 4) with one
   unbroken run of added lines. That is Trail-code's rule; we report it only when the lesson touches that exact file.
5. A staged file under `.tentaqles/lessons/` gets the cheap part of F-04's intake check: its name, its size, a
   header that parses and `scope: shared`. One that says `scope: private`, or fails, is a finding of its own,
   because this commit would publish it (F-04). The gate is not run in a hook.
6. One finding is kept per file and lesson and at most 5 are printed (Trail-code's number), as context through R-04's
   channel, once per session for the same set (SHA-256 of the sorted `file|lesson` pairs, last 20 kept, as in
   Trail-code): "[tentaqles lobe] Before this commit: load.sql:1 matches the avoid pattern of l-4c7d19e2 (TITLE). The
   commit goes ahead. A finding can be a word match; if it is real, fix it next and tell the person." A staged
   lesson reads: ".tentaqles/lessons/l-4c7d19e2.md is a private lesson and this commit would publish it."
7. It never denies, at any setting. It runs when `enforce` is not `off` and `recall` and `lessons` are on, within
   1,000 ms (proposed) and the hook's cut-off (core page 5.2), and prints nothing past either.
   `tentaqles lobe check [--base <ref>]` runs the same rules by hand on the staged change or on the branch since its
   merge base, and always exits 0.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| Trail-code (MIT) | the reason to warn only | [`src/claude/pre-bash-check.ts` L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L42): the stop is off; [L40](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L40) gives the count. [`CHANGELOG.md` L12](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/CHANGELOG.md#L12) says the same |
| Trail-code (MIT) | port: trigger and once per set | [`src/claude/pre-bash.ts` L43](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash.ts#L43): the command pattern. [`src/claude/pre-bash-check.ts` L112](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L112): the key; [L125](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L125): last 20; [L79](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L79): 5 lines |
| Trail-code (MIT) | port: candidates, caps and rules | [`src/check/local-check.ts` L37](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L37): 25 candidates; [L40](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L40): 2 shared terms; [L167](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L167): a term has 3 to 40 characters, is no stop word and not all digits. [L901](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L901): the old number is in the decided sentence and the new one is not. [L824](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L824): 2 shared terms of up to 4, else 3. [`src/cloud/diff.ts` L54](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cloud/diff.ts#L54): 400 KiB and 3 lines of context |
| Trail-code (MIT) | weakness, not taken | [`src/check/local-check.ts` L709](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L709): number words count as numbers, so "first" reads as 1. [L521](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L521): the pool is cut to 100 by word score before file matches are chosen. [`src/cli.ts` L3221](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L3221): exit code 1 on a conflict. [`src/claude/pre-bash.ts` L270](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash.ts#L270): the warning returns before the later guards run |
| tentaqles (new) | new | the pattern rule, which uses the tested `avoid` block |

**How to build it**

- Package `internal/lobe/commitcheck`: `trigger.go`, `diff.go` (parsed by hunk-header counts, not line prefixes),
  `terms.go`, `rules.go`, `report.go`. git runs as a child with `--no-optional-locks --no-ext-diff --no-color` and
  never fetches. `trigger.go` reuses R-04's command test, which splits with `internal/lobe/shellwords`.
- Lessons come from the private folder and the committed repository's `.tentaqles/lessons/`. A shared lesson changed
  by the same commit is read from `HEAD`, so a commit cannot silence it (as Trail-code, `src/cli.ts` L3195).
- Edge cases: no repository, nothing staged, only renames or binary files, or a command that changes folder first
  (`cd x && git commit`, `git -C x commit`) print nothing.
- Where we differ: ours always exits 0, counts digits only, picks file matches before word matches, and shares one
  answer with the other checks; a deny from a `block` rule wins and the findings are dropped.

**How we know it works**

- `TestLobeCommitCheck_PatternFinding`: a staged line that matches an `avoid.pattern` gives 1 finding with its line.
- `TestLobeCommitCheck_NeverDenies`: with a finding and `enforce: block`: 0 denies; `tentaqles lobe check` exits 0.
- `TestLobeCommitCheck_OnlyGitCommit`: with the same staged change, `git push` and `gh pr create` print 0 findings.
- `TestLobeCommitCheck_NumberWordsIgnored`: "after the first failure" and a retry count going 3 to 1: 0 findings.
- `TestLobeCommitCheck_TouchBeforeWords`: among 100 word matches and 1 touch-only lesson, that 1 is a candidate: yes.
- `TestLobeCommitCheck_StaleRankedLast`: a fresh and a stale lesson touch one file: the fresh finding comes first
  and the stale one carries R-06's mark: yes.
- `TestLobeCommitCheck_PrivateLessonStaged`: a staged `.tentaqles/lessons/` file with `scope: private` gives 1
  finding and 0 denies.
- `TestLobeCommitCheck_Budget`: with git stalled 3 seconds the hook answers by its cut-off with empty stdout.

**Tester brief**

Start from the standard tester setup in F-07. `<acme>` is `$W/clients/acme` and `<lobe>` is
`$TQ_HOME/identities/acme/lobe`. Run
`export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"` first: a lobe hook acts only when the session's login
folder is the client's own (core page 5.1). Backticked text split over two lines is one line: join it with a space.
Create `<acme>/etl`, run `git init` there, and write `load.sql` with the one line `MERGE INTO t USING (SELECT 1);`.
Write `<lobe>/lessons/l-4c7d19e2.md`:

```markdown
---
id: l-4c7d19e2
title: Alias the MERGE source
kind: lesson
source: person
scope: private
applies: client
origin: {by: me, host: person, drafted: person}
when: {place: [load.sql]}
avoid:
  pattern: '(?i)merge into \S+ using \('
  must_match: ["MERGE INTO t USING (SELECT 1)", "merge into dim using (select 2)"]
  must_not_match: ["MERGE INTO t USING s", "MERGE INTO t USING src AS s"]
strength: 1.0
seen: 3
created: 2026-11-09
---
## Do
Give the source an alias.
## Avoid
A subquery as the source.
## Why
The warehouse rejects it because the source has no alias.
```

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if it does not, stop and report FAIL.
The hooks see the file without `tentaqles lobe rebuild` (R-04). A lesson that a person wrote
enters at `advice`, so no `ladder.json` is needed. Run `git add load.sql`. Run every command inside `<acme>/etl`.
Input A, piped to `tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1", "hook_event_name":"PreToolUse",
"cwd":"<acme>/etl", "tool_name":"Bash", "tool_use_id":"t1", "tool_input":{"command":"git commit -m merge"}}`.

1. Input A: exit code 0; stdout contains `load.sql:1`, `l-4c7d19e2` and `goes ahead`, and no deny decision.
2. Input A again, unchanged: stdout does not contain `load.sql:1`.
3. `tentaqles lobe check`: exit code 0, and stdout contains `load.sql:1` and `l-4c7d19e2`.
4. Run `lobeset acme enforce block`, the setup's helper (`tentaqles lobe set` answers only to a person and no
   tester runs it). Input A with `session_id` `s2`: stdout
   contains `load.sql:1` and no deny decision.
5. Input A with `session_id` `s3` and the command `git push`: stdout does not contain `load.sql:1`.
6. Copy the lesson file to `<acme>/etl/.tentaqles/lessons/l-4c7d19e2.md` and run `git add .tentaqles`. Input A with
   `session_id` `s4`: stdout contains `.tentaqles/lessons/l-4c7d19e2.md` and `private lesson`, and no deny decision.

**Risks and open questions**

- An `avoid.pattern` written for call text can match a test or a quoted example in a file; M-05 reports by file type.
- The word rule is the one Trail-code found noisy; drop it if M-05 shows over 1 false finding in 10 (proposed).
- Our cut-off is under Trail-code's 3.5 s (`src/claude/pre-bash-check.ts` L35), so a big change may go unchecked.
- Only `git commit` is covered. A change that is pushed or opened as a pull request without a commit from the
  agent's session is not checked; Trail-code's check also runs before those two.
- A private lesson staged in a repository gets a warning here, not a stop. `tentaqles doctor` reports the same file
  as `lobe-private-in-repo` (F-04).

## E-07 Automatic demotion

**Phase** 2 · **Origin** new; idea: BASE · **Depends on** F-01, C-05, C-06, R-05, R-06, E-01, E-02; phase 3: E-03

**What it is.** A rule that proves wrong, or whose ground has gone, moves down the ladder or is retired. This happens
at once and nobody is asked. Moving up takes a record and, for a block, a person. Moving down takes one event. This
entry also owns the writing of every stage move, up or down: one function applies a move and logs it.

**Why it matters.** It keeps the anti-goal "a wrongly learned rule blocks correct work" from lasting longer than one
call. Trail-code could stop its commit check from blocking only by changing a constant and shipping a release.

**Mechanism**

| Trigger (numbers are E-01's, proposed) | Seen by (`Settle` is E-02's step in the detached pass after each turn) | Move | `why` |
|---|---|---|---|
| false alarm at `advice` | post-tool hook (E-02) | to `trial` | `false_alarm` |
| false alarm at `block`: a call succeeded that an override let through, or that the rule only advised on because a cap held it | post-tool hook | to `advice`; approval removed | `override_succeeded`, or `false_alarm` under a cap |
| second false-alarm demotion of one lesson | post-tool hook | to `retired` | `proved_wrong` |
| 3 false alarms at `trial` with 0 hits | `Settle` | to `retired` | `proved_wrong` |
| E-03 tests fail | `Settle`, `tentaqles lobe rules test` | to `trial`; approval removed | `tests_failed` |
| the lesson's `avoid.pattern` or `when` block differs from the approved one | PreToolUse (no deny), then `Settle` | `block` to `advice`; approval removed | `pattern_changed` (a changed `when` is logged under the same reason) |
| faded below C-05's floor | `Settle` | to `retired` | `faded` |
| a `block` rule with no deny and no hit for 90 days | `Settle` | to `advice`; approval removed | `idle` |
| replaced (C-06) | `Settle` | old lesson to `retired`; the new one enters by E-01 with no record and no approval | `replaced` |
| a replacement is undone: the successor's file is gone (C-06) | `Settle` | the old lesson, if it was retired as `replaced`, from `retired` to its enter stage (E-01): never above `advice`, with a fresh record and no inherited approval | `replacement_undone` |
| `tentaqles lobe rules demote <id>`, `tentaqles lobe rules retire <id>` | a person (E-01's check, no typed `yes`) | one stage down; to `retired`, which is also the private mute of a shared lesson (E-01) | `person` |

1. The post-tool hook that writes a `false_alarm` verdict applies the move under F-01's lock (50 ms wait, proposed):
   `ladder.json`, `approvals.json`, then `ladder-log.jsonl`. If the lock is busy, `Settle` applies it after the turn.
2. A move sets the three counts to 0, and leaving `block` sets `approved_at` to null. Nobody is asked and nothing is
   injected; the next session brief (R-05) lists the moves and `tentaqles lobe rules` shows their reasons.
3. The way back is the first climb again from 0, with a new typed `yes` for `block`. A retired lesson returns in
   three cases only: as a new lesson (C-06); when the replacement that retired it is undone, by the row above; and
   when a person vouches for it with `tentaqles lobe lessons confirm <id>` (E-01, F-07): it re-enters and moves to
   `advice`. An overridden call that fails is a hit, and the
   rule stays at `block`.
4. Stale is not a move. A lesson that R-06 marks stale is still shown, marked as possibly out of date and ranked
   after fresh ones (R-04, R-06). A stale rule never denies: a `block` rule acts as advice while its cached stale
   flag is set (E-01 step 3) and denies again when the flag clears. This entry makes no ladder move for staleness
   alone. The rule's stage, record and approval stay, so a branch switch that hides a file for an hour costs the
   person nothing.
5. Phases. The rows that name `block`, an approval, an override or E-03's tests arrive in phase 3. The other rows
   ship in phase 2 with E-01 and E-02, so no lesson is shown before it can be demoted.

**Source**

| Project | What we take | Where to read |
|---|---|---|
| BASE (FSL-1.1) | idea only | [`src/shadow/promote.rs` L222](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/promote.rs#L222): after a promotion it watches the next prompts and undoes the promotion by itself, with no question, when corrections rise past a margin (100 prompts and 2.5 deviations, [`src/config.rs` L401](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/config.rs#L401)) |
| BASE (FSL-1.1) | weakness, not taken | [`src/shadow/promote.rs` L22](https://github.com/ChristopherKahler/base/blob/a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae/src/shadow/promote.rs#L22): its own note says the margin still fired on 5.5% of points of a replayed log in which nothing had changed. A rate over a window is noisy, so we move on single events |
| Trail-code (MIT) | port: a changed rule stops enforcing | [`src/trace/guard-fast.ts` L79](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L79): the deny path reads the kept rule on every call, and anything but the three exact values allows |
| tentaqles (new) | new | the trigger table, the move made inside the post-tool hook, one writer for every move |

**How to build it**

- In `internal/lobe/ladder`: `demote.go` (`Demote(entry, why) Move`, pure) and `apply.go` (lock, write, log).
  `apply` is the only code that writes a stage. The post-tool hook, `ladder.Settle`, `rulecheck` and the `demote`
  and `retire` actions call it for a move down; `ladder.Enter`, `Settle`'s promotions, `lessons confirm` and
  `rules approve` call it for a move up. C-04, C-05, C-06 and F-04 write no stage: they call `ladder.Enter` or
  leave the move to `Settle`, which reads the lesson files, C-05's strength and C-06's chain.
- A move applies only when the stored stage is the one it starts from, so applying it twice changes nothing. `Settle`
  checks replaced, replacement undone, faded, tests, pattern, idle, counts, in that order, and moves a lesson once
  per run.
- `ladder.json` is written before `approvals.json`; an approval with no `block` entry is ignored. The number of
  earlier false-alarm demotions of a lesson is counted from `ladder-log.jsonl`, because the core page's ladder entry
  (5.4) has no field for it. The same log tells `Settle` whether a retired lesson was retired as `replaced`.
- The stale cap is not in this package's moves. The PreToolUse hook reads the cached stale flag at decision time
  (E-01 step 3); R-06 sets and clears it in the detached pass, and no ladder file is written.
- Where we differ: BASE undoes a whole configuration when a rate rises. We move one rule on one event that names it.

**How we know it works**

- `TestLobeDemote_Table`: the 11 rows of the trigger table give the 11 expected stages.
- `TestLobeDemote_InTheHook`: after 1 `false_alarm` at `advice`, the stage is `trial` before the hook exits: yes.
- `TestLobeDemote_BusyLock`: with the lock held 500 ms the stage is unchanged at hook exit and `trial` after `Settle`.
- `TestLobeDemote_AgentRefused`: with `CLAUDECODE` set, `tentaqles lobe rules demote` changes 0 bytes.
- `TestLobeDemote_StaleNeverDenies`: a `block` rule whose cached stale flag is set gives a note and no deny, with
  `ladder.json` and `approvals.json` byte-identical; once the flag clears, the next call is denied.
- `TestLobeDemote_UndoComesBackAtMostAdvice`: a lesson that held `block` before it was replaced returns, when the
  successor's file is deleted, at its enter stage with 0 counts and no approval.
- `TestLobeDemote_OneWriter`: over 50 moves, up and down, every stage change has exactly 1 line in
  `ladder-log.jsonl`.

**Tester brief**

Start from the standard tester setup in F-07. `<acme>` is `$W/clients/acme` and `<lobe>` is
`$TQ_HOME/identities/acme/lobe`. Run every command
inside `<acme>`, after `export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`: a lobe hook acts only when the
session's login folder is the client's own (core page 5.1). Backticked text split over two lines is one line: join
it with a space. Write `<lobe>/lessons/l-4c7d19e2.md`:

```markdown
---
id: l-4c7d19e2
title: Alias the MERGE source
kind: lesson
source: person
scope: private
applies: client
origin: {by: me, host: person, drafted: person}
when: {tool: [Bash], command: [dbsql]}
avoid:
  pattern: '(?i)merge into \S+ using \('
  must_match: ["MERGE INTO t USING (SELECT 1)", "merge into dim using (select 2)"]
  must_not_match: ["MERGE INTO t USING s", "MERGE INTO t USING src AS s"]
strength: 1.0
seen: 3
created: 2026-11-09
---
## Do
Give the source an alias.
## Avoid
A subquery as the source.
## Why
The warehouse rejects it because the source has no alias.
```

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if it does not, stop and report FAIL.
The hooks see the file without `tentaqles lobe rebuild` (R-04). `<lobe>/ladder.json`: `{"v":1,
"entries":[{"lesson":"l-4c7d19e2", "stage":"advice", "hits":3, "false_alarms":0, "sessions":2,
"approved_at":null}]}`. Input A, piped to `tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1",
"hook_event_name":"PreToolUse", "cwd":"<acme>", "tool_name":"Bash", "tool_use_id":"t1",
"tool_input":{"command":"dbsql -e 'MERGE INTO t USING (SELECT 1)'"}}`. Input B, piped to
`tentaqles lobe-hook claude post-tool-use`, is input A with `hook_event_name` `PostToolUse` and the added key
`"tool_response":{"stdout":"1 row"}`.

Checks 1 to 3 run at the end of phase 2. Check 4 needs the block step and runs at the end of phase 3.

1. Input A: exit code 0; stdout contains `Alias the MERGE source` (R-04's note) and no deny decision.
2. Input B: exit code 0; `ladder.json` has `"stage":"trial"`; `ladder-log.jsonl` has 1 line, with `false_alarm`.
3. `CLAUDECODE=1 tentaqles lobe rules demote l-4c7d19e2`: exit code not 0, and `ladder.json` is unchanged.
4. Delete `ladder-log.jsonl` and `trials.jsonl`. Run `lobeset acme enforce block`, the setup's helper (`tentaqles lobe
   set` answers only to a person and no tester runs it). In `ladder.json` set `"stage":"block"` and
   `"approved_at":"2026-11-09T10:00:00Z"`. Write `<lobe>/approvals.json`: `{"v":1,
   "approvals":[{"lesson":"l-4c7d19e2", "pattern":"(?i)merge into \\S+ using \\(", "when":{"tool":["Bash"],
   "command":["dbsql"]}, "title":"Alias the MERGE source", "why":"w", "do":"d", "approved_at":"2026-11-09T10:00:00Z",
   "override_until":"T"}]}`, T being the time 5 minutes from now in UTC, in the form of `approved_at`. Input A and
   then input B, both with `tool_use_id` `t3`. Expect no deny decision from input A; after input B, `"stage":"advice"`
   in `ladder.json` and no entry for the lesson in `approvals.json`.

Person checks, for the owner to run in a terminal with the tester's `HOME`, `USERPROFILE` and `TQ_HOME` exported
(`demote` and `retire` answer only to a person). They run at the end of phase 2, after the checks above.

1. Set the entry in `ladder.json` to `"stage":"advice"`, then `tentaqles lobe rules demote l-4c7d19e2`: exit code
   0; `ladder.json` has `"stage":"trial"` and the three counts 0; the last line of `ladder-log.jsonl` has
   `"why":"person"`.
2. `tentaqles lobe rules retire l-4c7d19e2`: `ladder.json` has `"stage":"retired"`. The tester then pipes input A
   with `session_id` `s9`: exit code 0 and empty stdout.

**Risks and open questions**

- One false alarm can be chance (S-04 misreads, or the call works once). We accept a slow climb over a wrong block.
- An agent can cause a false alarm on purpose and so silence a rule at `advice`; a `block` rule would deny that call.
- While a `block` rule is stale it only advises. That is the price of never denying on code that has changed (core
  page 5.5); the idle row removes an approval that staleness has kept unused for 90 days.
- Is the second false-alarm demotion the right point to retire a lesson, or should it be the third?
