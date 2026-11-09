# 035: Backfill from past sessions

**Status** not started · **Stage** Lessons · **Entries** C-07 (the Claude Code transcripts), phase 0
question 4 (core page section 8), E-01 (the enter rule for `backfill`; `trial` to `advice` by record), E-02
(the lessons that wait), C-05 (strength 0.4), F-07 (`tentaqles lobe backfill`); D-9 · **Needs** 028, 024 ·
**Feeds** 036

## Result

On day one the person runs one command in a client's folder. It reads that client's own earlier Claude Code
sessions, finds the repairs that came back on two different days, and offers them as lessons. After a typed
`yes` they are filed. They start silent, because old sessions are weaker evidence than a repair seen live
(decision D-9): `tentaqles lobe rules` lists them at `trial` or `seen` and the agent is shown nothing. A mined
lesson that then proves right twice, in two sessions, moves to `advice` and speaks. One that proves wrong is
retired without ever having been shown. One that cannot prove itself waits, and the session brief says how
many wait and how to confirm them.

```bash
tentaqles lobe backfill --dry-run          # anyone: counts only, writes nothing, calls no model
tentaqles lobe backfill                    # a person: lists titles with session counts, asks for yes
tentaqles lobe rules                       # the new lessons at trial or seen
tentaqles lobe lessons --waiting           # the ones that wait for the person
tentaqles lobe lessons confirm l-4c7d19e2  # a person: move one to advice
```

## Scope

**In**

- Phase 0 question 4, as the second build task: a throwaway count the owner runs in one client they name.
- `tentaqles lobe backfill --dry-run [--json]` and `tentaqles lobe backfill [--yes] [--json] [--draft <how>]`.
- The reading rule: only the client's own login folder, never `~/.claude`, never another client.
- The reader for Claude Code transcripts, which turns each tool call into an event in memory.
- C-01 and C-02 run on those events; the two-sessions, two-days rule; the cap of 20; drafting through C-03;
  the gate; the list; the typed yes; filing with `source: backfill` and strength 0.4.
- `backfill.json`, so a later run reads only newer sessions.
- The silent trial for real: mined lessons enter at `trial` or `seen`, earn `advice` by record, and the
  waiting line of feature 026 gets its first real members.

**Out**

- Codex transcripts under the client's `codex/sessions` (feature 036). `roots.go` names both roots here; this
  feature reads one.
- Sessions from before tentaqles, under `~/.claude` and `~/.codex`: never read, and no flag will add them.
- The step from `advice` to `block` for a mined lesson (feature 038).
- Counting how many mined lessons were junk (M-04, feature 022 reports it once backfill exists).

## What it builds on

- 003: the store, `scrub.Clean`, `sense.Event`, `store.RepoKey` and S-01's walk to a repository.
  006: `internal/lobe/fingerprint` (S-08). 009: S-04's rules for a failure that looks like a success.
- 016: `gate.Check`, the lesson id function. 019: the filing code and quarantine.
- 017: `ladder.Enter`, whose enter rule already puts a `source: backfill` lesson at `trial` (with a `when`
  block) or `seen` (without); recall prints nothing below `advice`.
- 018: C-01's pairing as functions over a list of events. 027 and 028: C-02's flags and signs.
- 023: fires, verdicts, `trials.jsonl`, `ladder.Counts`, `ladder.Settle`. 024: `apply`, and the row "3 false
  alarms at `trial` with 0 hits".
- 025: C-03 (`draft.go`, `child.go`, `template.go`, `--draft`, the daily limit).
- 026: the waiting line of the brief, `tentaqles lobe lessons --waiting`, `tentaqles lobe lessons confirm`.
- 030: starting strength by `source`. 033: the `share` setting, which a mined lesson follows like any other.
- 034: the tamper mark, which covers the transcripts this feature reads.

## What it leaves for later features

- Package `internal/lobe/backfill`: `roots.go`, `claude.go`, `run.go`, `mark.go`. Feature 036 adds `codex.go`.
- `backfill.json` in the store.
- The command `tentaqles lobe backfill` and its `--json` lines (036 widens the counts to Codex sessions).
- Proof that E-01's `trial` to `advice` by record works on real members; 037 and 038 build on that record.

## How it works

1. **Phase 0 question 4 comes first.** "How many failure-and-fix pairs in the owner's own past sessions would
   have made a useful lesson?" A builder agent cannot answer it: it needs a real, logged-in client, and no
   builder or tester reads a live session (core page section 8). The builder writes a throwaway program. The
   owner runs it in one client they name (the author assumes the owner's own `tentaqles` client) and hands
   back its output, which holds counts and field names, never content. It prints two things. First, for the
   transcript files: the field names per line type with counts, to confirm the line shapes of step 4.
   Second: sessions, skips by reason, failed calls, failure-then-fix pairs, and how many pairs recur in 1, 2
   and 3 sessions on as many different days, for look-back windows of 90, 180 and 400 days. The written
   result states whether "2 sessions on 2 days", "180 days" and "at most 20" stay. The owner approves any
   changed number before task 3 starts.
2. **Who may run it** (C-07 step 1). A person, never a hook. Without `--dry-run` the command refuses inside
   an agent session and changes nothing; `--yes` skips the typed yes, not that check. `--dry-run` only reads
   and counts, so anyone may run it (proposed). The client is the one of the current folder
   (`internal/resolve`). With `lessons` off the command refuses with one line that names the setting.
3. **What is read** (C-07 step 2). `roots.go` builds exactly two roots from `paths.IdentityDir` alone:
   `IdentityDir(client, "claude")/projects` and `IdentityDir(client, "codex")/sessions`. No environment
   variable, flag or setting can add a third. Links and junctions are resolved first, and a root that
   resolves outside `$TQ_HOME/identities/<client>/` is refused with a message. This feature reads the first
   root; the second waits for 036.
4. **Which sessions** (C-07 step 3). Session files under the root, newest first by modification time. Skipped,
   each with a counted reason: a file last written more than 180 days ago (`old`, proposed); a headless
   session, one whose `entrypoint` starts with `sdk`, which also covers the children C-03 starts
   (`headless`); a session whose first `cwd` does not resolve to this client (`foreign`); a session whose id
   has the tamper mark in this store (`tampered`). A single line over 2 MiB is skipped unparsed and counted
   (`long_lines`); the session goes on. A session newer than the time in `backfill.json` is read; an older
   one is not.
5. **Reading a session** (C-07 step 4). At most 60 seconds per session and 300 for all of them (Trail-code's
   limits). A `tool_use` part is joined to its `tool_result` part by id. The call failed when `is_error` is
   true or S-04 says so. S-01's skip rules apply: a call the person refused or interrupted, and the lobe's own
   tools, give no event. Each call becomes a `sense.Event` in memory through the same scrubber (S-05) and
   fingerprint (S-08) code as live capture, with `repo` and `cwd` from the line's `cwd`. `events/` is not
   written, and no session file is written.
6. **Mining** (C-07 step 5). C-01 pairs each session's events. C-02 runs on them too: the person's prompts in
   the transcript are tested with the wording list of the client's language, and a flag becomes a sign only
   with C-02's evidence. A prompt is held in memory for the run and is never written to the store.
7. **What qualifies.** A repair qualifies only when its fingerprint was repaired in 2 sessions on 2 different
   local days (proposed; step 1 tunes it). A sign follows C-02's own limits: at most 3 per session, and a
   `fixed` sign only once its fingerprint was fixed in 2 sessions. At most 20 candidates qualify (proposed):
   repairs first, most sessions first, then newest; then signs, newest first.
8. **Drafting** (C-07 step 6, C-03). In the foreground, from the person's terminal, which core page 5.7
   allows. C-03's own conditions decide between the model and the template: the `model` setting, the login
   check on `CLAUDE_CONFIG_DIR`, the CLI on PATH. At most 10 model calls per run (proposed); they count toward
   C-03's daily limit, and the template words the rest. `--draft template`, `--draft file:<path>` and `--draft
   model` work as in `tentaqles lobe consolidate`. Each draft passes `gate.Check` with the in-memory events as
   evidence. A draft whose only fault is `repeat` is dropped and the lesson it repeats is reinforced (C-04
   step 2). The lesson id comes from C-04's function, so a mined lesson and a later live one with the same
   title and fingerprints are one file.
9. **The list and the yes.** The command prints one line per draft: its title, the number of sessions and
   days behind it, and `model` or `template`. It prints counts for everything else. Nothing is filed before a
   typed `yes`. A declined run files nothing, writes no `backfill.json` and exits as a declined `tentaqles
   add` does. The store lock is taken only while filing.
10. **Filing.** Each lesson is filed by the code of 019, in the place `share` gives (033), with `source:
    backfill`, `strength: 0.4` (C-05 step 1), and `origin` holding `by: me`, `host: claude`, `drafted: model`
    or `template`, and `session`, the newest session of its evidence. It has no `origin.events`: the events
    were never in the log.
11. **Entering silent** (E-01's enter rule, decision D-9). `ladder.Enter` puts a mined lesson with a `when`
    block at `trial` and one without at `seen`. Neither is eligible for recall, so the brief, the note before
    a call and the note after a failure print nothing for it.
12. **Earning advice.** At `trial` each matching call opens a silent fire (E-02 step 1) and the result judges
    it. A hit is the mistake coming back: the call fails with a fingerprint the lesson lists, or, for a lesson
    with an `avoid` block, the avoided call fails. After the turn `ladder.Settle` counts the lesson's lines in
    `trials.jsonl` since its last move. With at least 2 hits in at least 2 sessions and 0 false alarms it
    hands the move to `apply`: `trial` to `advice`, logged with the reason `record` and `"by":"settle"`
    (E-01's row "`trial` to `advice`"). From the next matching call the lesson is a note. Three false alarms
    at `trial` with 0 hits retire it as `proved_wrong` (E-07), never shown. If `Settle` does not yet make the
    move up by record, this feature adds it in `internal/lobe/ladder/moves.go`.
13. **Waiting.** A mined lesson with no `avoid` block and no fingerprint cannot earn a hit (E-02). That is a
    lesson mined from a correction, or one with `touches` and no `when` block. It stays at `trial` or `seen`
    until the person runs `tentaqles lobe lessons confirm <id>`. The brief of 026 counts it in its one line
    `<W> waiting for you to confirm: tentaqles lobe lessons --waiting`, and that command lists it. A lesson a
    template drafts from a repair always lists the repair's fingerprint, so it earns its way and does not
    wait.
14. **The mark** (C-07 step 7). After a run that filed, or that found nothing to file, `mark.go` writes
    `backfill.json` with `store.Update`: `{"v":1,"newest":"<time of the newest session read>","at":"<time of`
    `the run>","sessions":2,"failures":2,"pairs":2,"qualifying":1,"filed":1}` (shape proposed). `--dry-run`
    writes nothing, this file included.
15. **Output.** The printed report holds counts and the titles of the drafts, never session text. With
    `--json` the first line is `{"sessions":2,"skipped":{"headless":0,"foreign":0,"old":0,"tampered":0,`
    `"long_lines":0},"failures":2,"pairs":2,"qualifying":1}`, followed by one line per action as in C-01:
    `draft` with `by` and `why`, `file` with `lesson`. `sessions` counts the sessions read, not the skipped.

## Where to read

| What | Where |
|---|---|
| Requirements | [C-07](../prd/lobe-consolidate.md), whole; [core page section 8](../prd/vertical-lobe.md), question 4 and the last paragraph; [E-01](../prd/lobe-enforce.md), the rows "enter" and "`trial` to `advice`", and tester check 2; [E-02](../prd/lobe-enforce.md), steps 1 to 5 and "the limit of the record"; [E-07](../prd/lobe-enforce.md), the row for 3 false alarms at `trial`; [C-03](../prd/lobe-consolidate.md), steps 1, 6 and 7; [C-02](../prd/lobe-consolidate.md), steps 1, 4, 7 and 8; [C-05](../prd/lobe-consolidate.md), step 1; [R-05](../prd/lobe-recall.md), step 4; [F-07](../prd/lobe-foundation.md), the two `backfill` rows and the `lessons --waiting` row; [core page 5.5 and 5.7](../prd/vertical-lobe.md), decision D-9 |
| Source to port | [`src/init/sessions.ts` L35](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/sessions.ts#L35): 400 days there, 180 here. [`src/init/transcript.ts` L50](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/transcript.ts#L50): lines over 2 MiB are skipped unparsed. [`src/trace/claude.ts` L119](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/claude.ts#L119): an `sdk` entrypoint is not a person. [`src/init/look-back-mine.ts` L156](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/look-back-mine.ts#L156): 60 seconds per session. [`src/init/run.ts` L92](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/run.ts#L92): 300 seconds in all. [`src/init/prefs.ts` L181](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/prefs.ts#L181): 2 distinct local days |
| Source not copied | [`src/init/sessions.ts` L150](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/sessions.ts#L150): discovery reads only the home folder's `.claude/projects`. [`src/init/transcript.ts` L179](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/init/transcript.ts#L179): every line with a tool result is skipped there, and we need exactly those lines. [`src/functions/replay.ts` L149](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/replay.ts#L149): imported lessons get an id with another prefix, so one text can be two records |
| Existing tentaqles code | `internal/paths/paths.go` (`IdentityDir`); `internal/providers/catalog/claude.yaml` and `codex.yaml` (that folder is the client's `CLAUDE_CONFIG_DIR` and `CODEX_HOME`); `internal/cmd/reach.go` (`confirm`, `errAborted`, the typed yes); `internal/cmd/edit.go` (`--yes`, the agent-session refusal) |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits. Look in particular at:
   whether C-01 and C-02 can run on events that are not in the log; whether `Settle` already promotes from
   `trial` by record; what 026's waiting count reads; where earlier phase 0 results were written (G-01 uses
   `docs/prd/spikes/`). The owner approves changes to this file before code is written.
2. The throwaway count of step 1: a small program under `spike/backfill-count/`, on a branch that is not
   merged, that takes a client name, reads only that client's two roots and prints counts and field names.
   The owner runs it and hands back the output. Write the result beside the other phase 0 results, with the
   three numbers it confirms or changes. Check: the result file exists and holds no command, path, error text
   or prompt.
3. `internal/lobe/backfill/roots.go`. Proof: `TestLobeBackfillOnlyThisClient`,
   `TestLobeBackfillRootOutsideRefused`, `TestLobeBackfillNeverReadsHome`.
4. Side by side with 3: `claude.go`, the line reader, with fixtures written from the field names of task 2
   and made-up values. Proof: `TestLobeBackfillReadsClaudeLines`, `TestLobeBackfillScrubs`.
5. `run.go`: selection, skips, time limits, mining, the qualifying rule, the counts, and `--dry-run --json`
   in `internal/lobe/cli`. Proof: `TestLobeBackfillTwoDays`, `TestLobeBackfillWritesNoEvents`,
   `TestLobeBackfillSkips`, `TestLobeBackfillTimeLimits`.
6. Drafting, the list, the yes, filing and `mark.go`; the person check in `internal/cmd/lobe_backfill.go`.
   Proof: `TestLobeBackfillFilingNeedsAPerson`, `TestLobeBackfillDeclinedFilesNothing`,
   `TestLobeBackfillMarkReadsOnlyNewer`, `TestLobeBackfillPromptNeverStored`,
   `TestLobeBackfillModelCallsCounted`, `TestLobeOriginBlock` (the backfill path).
7. Side by side with 6: the ladder. Confirm or add the move up by record. Proof: `TestLobeBackfillEntersSilent`,
   `TestLobeBackfillEarnsAdvice`, `TestLobeLadder_Thresholds`, `TestLobeBriefWaitingLine`.
8. Docs: a `## tentaqles lobe backfill` section in `docs/reference/commands.md`; a paragraph in
   `docs/getting-started.md` on the first day; one line in `docs/threat-model.md` on what backfill reads; one
   line in `CHANGELOG.md`. The plugin does not change.
9. The tester run of the Evaluation below, then the owner's person checks.

## Tests that stay in the repository

Names marked (new) are this file's; the others are the requirements' names.

- `TestLobeBackfillOnlyThisClient`: a run for `acme` opens 0 files outside `identities/acme/` (file-open
  recorder).
- `TestLobeBackfillRootOutsideRefused`: a `projects` link that points outside the client folder gives 0
  sessions.
- `TestLobeBackfillTwoDays`: the same repair twice on one day gives 0 qualifying; on 2 days it gives 1.
- `TestLobeBackfillWritesNoEvents`: `events/` has the same listing before and after; a second run reads 0
  sessions.
- `TestLobeBackfillNeverReadsHome`: with sessions under `$HOME/.claude/projects` and `$HOME/.codex/sessions`
  and `CLAUDE_CONFIG_DIR` pointing at `$HOME/.claude`, a run opens 0 files under either folder.
- `TestLobeBackfillFilingNeedsAPerson`: inside an agent session a run without `--dry-run` exits non-zero and
  writes 0 bytes, with and without `--yes`; `--dry-run` exits 0 and writes 0 bytes.
- `TestLobeOriginBlock`, the backfill path: `by`, `host` and `drafted` are set and `source` is `backfill`.
- `TestLobeBackfillReadsClaudeLines` (new): a call and its result joined by id; `is_error` gives `fail`; a
  piped test run that exits 0 with failures gives `hidden-fail`; a refused call gives no event. 4 of 4.
- `TestLobeBackfillSkips` (new): an old file, a headless session, a foreign `cwd`, a marked session and a
  line of 2 MiB plus 1 byte are each counted under their reason and teach nothing. 5 of 5.
- `TestLobeBackfillTimeLimits` (new): a reader that stalls is left after 60 seconds on a test clock, and the
  run stops reading at 300.
- `TestLobeBackfillDeclinedFilesNothing` (new): a typed `no` leaves 0 lessons and no `backfill.json`.
- `TestLobeBackfillMarkReadsOnlyNewer` (new): after a filing run, a session older than `newest` is not read
  and a newer one is.
- `TestLobeBackfillPromptNeverStored` (new): after a run that mined a correction, 0 files of the store hold
  a 4-word run of its prompt.
- `TestLobeBackfillModelCallsCounted` (new): 12 qualifying candidates with `model: on` start 10 children and
  template 2; a later pass the same day sees 10 calls used.
- `TestLobeBackfillEntersSilent` (new): a mined lesson with a `when` block is at `trial`, one with `touches`
  and no `when` block is at `seen`, and the three recall hooks print 0 bytes for both. 2 of 2.
- `TestLobeBackfillEarnsAdvice` (new): 2 hits in 2 sessions and 0 false alarms give `advice` after `Settle`,
  with one `record` line in `ladder-log.jsonl`; 2 hits in 1 session, or 2 hits and 1 false alarm, leave it at
  `trial`; 3 false alarms and 0 hits retire it. 4 of 4.
- Kept green: `TestLobeLadder_Thresholds`, `TestLobeBriefWaitingLine`,
  `TestLobeTrial_NoHitWithoutAvoidOrFingerprint`, `TestLobeRecallStageAndSwitches`,
  `TestLobeDraftStartsNothing`, `TestLobeCommandTable`, `TestLobePersonVerbsRefuseInAgent`,
  `TestDocsReferenceEveryCommand`.
- House rules. Isolation: the three reading tests above, and `TestLobeBackfillForeignSessionTeachesNothing`
  (new): a session of `globex` copied into `acme`'s login folder is counted `foreign` and yields 0 pairs. No
  secret on disk: `TestLobeBackfillScrubs` (new): a token in a transcript command is in 0 files of the store
  and 0 lines of output. Never stalls: no hook changes; `TestLobeHookAlwaysExitsZero` stays green. Guard
  untouched: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour` (the package imports
  `internal/resolve` and `internal/paths` and nothing else of tentaqles), and the tests under "Must not trade
  off" in `CLAUDE.md`.

## Evaluation

The tester gets the built `tentaqles`, this brief and an empty working folder. Use bash (Git Bash on Windows)
and no network. A backfill that files answers only to a person, so the tester counts with `--dry-run` and
plays a mined lesson by writing its file by hand.

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
lobeset() { m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"; }
A="$W/clients/acme"; G="$W/clients/globex"; I="$TQ_HOME/identities"; D="$I/acme/lobe"; H="$I/acme/claude"
P="$H/projects/p1"; mkdir -p "$P" "$I/globex/claude/projects/p1" "$HOME/.claude/projects/p1"; cd "$A"
line() { printf '{%s"type":"%s","sessionId":"%s","cwd":"%s","timestamp":"%sT10:0%s:00Z","message":{"role":"%s","content":[%s]}}\n' "${8:+$8,}" "$1" "$2" "$3" "$4" "$5" "$1" "$6"; }
sess() { # sess <file> <session id> <cwd> <day> [extra key]
  { line assistant "$2" "$3" "$4" 0 '{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"npx vitest run"}}' "" "$5"
    line user "$2" "$3" "$4" 1 '{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"Error: Worker exited unexpectedly"}' "" "$5"
    line assistant "$2" "$3" "$4" 2 '{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"npx vitest run --pool=forks"}}' "" "$5"
    line user "$2" "$3" "$4" 3 '{"type":"tool_result","tool_use_id":"t2","is_error":false,"content":"ok"}' "" "$5"; } > "$1"; }
COUNT() { (cd "$A" && tentaqles lobe backfill --dry-run --json); }
sess "$P/a.jsonl" a "$A" 2026-10-01; sess "$P/b.jsonl" b "$A" 2026-10-02
for d in "$I/globex/claude/projects/p1" "$HOME/.claude/projects/p1"; do
  sess "$d/a.jsonl" a "$G" 2026-10-01; sess "$d/b.jsonl" b "$G" 2026-10-02; done
```

Stop with FAIL if a line starts with `SETUP FAIL`. Each session file has four lines: a failed `npx vitest
run` and the retry with `--pool=forks` that worked. COUNT must exit 0 and print one line.

1. COUNT prints `"sessions":2`, `"pairs":2` and `"qualifying":1`.
2. `sess "$P/c.jsonl" c "$A" 2026-10-03 '"entrypoint":"sdk-cli"'` and `sess "$P/d.jsonl" d "$G" 2026-10-04`.
   COUNT prints `"sessions":2`, `"headless":1` and `"foreign":1`.
3. `mv "$P/b.jsonl" "$W/"`. COUNT prints `"qualifying":0`: one day is not enough. `mv "$W/b.jsonl" "$P/"`.
4. `rm "$P"/[abcd].jsonl`, so only globex's folder and `$HOME/.claude` hold sessions. COUNT prints
   `"sessions":0` and `"pairs":0`. Then write `a.jsonl` and `b.jsonl` again as in the setup.
5. A dry run wrote nothing. `test -e "$D/backfill.json"` is false, and `ls "$D/events" "$D/lessons"
   2>/dev/null | wc -l` prints `0`.
6. An agent cannot file. `CLAUDECODE=1 tentaqles lobe backfill --yes --draft template; echo "exit=$?"` prints
   an exit code that is not 0, and check 5 still holds.
7. `lobeset acme lessons off; tentaqles lobe backfill --dry-run; echo "exit=$?"` prints one line that names
   `lessons` and an exit code that is not 0. Then `lobeset acme lessons on`.

For checks 8 to 11 the tester writes three lesson files as backfill would have filed them:

```bash
mkdir -p "$D/lessons"
put() { # put <id> <title> <when block> <avoid line or empty> <do> <avoid> <why>
  { printf -- '---\nid: %s\ntitle: %s\nkind: lesson\nsource: backfill\nscope: private\napplies: client\n' "$1" "$2"
    printf 'origin: {by: me, host: claude, drafted: template}\nwhen: %s\n' "$3"
    [ -z "$4" ] || printf '%s\n' "$4"
    printf 'strength: 0.4\nseen: 0\ncreated: 2026-11-09\n---\n## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$5" "$6" "$7"
  } > "$D/lessons/$1.md"; tentaqles lobe lessons gate "$D/lessons/$1.md"; }
put l-4c7d19e2 "Alias the MERGE source" "{tool: [Bash], command: [dbsql]}" \
  "avoid: {pattern: '(?i)merge into [a-z]+ using [(]', must_match: ['MERGE INTO t USING (SELECT 1)'], must_not_match: ['MERGE INTO t USING s']}" \
  "Give the source an alias." "A subquery as the source." "The warehouse rejects it because the source has no alias."
put l-5d8e2af3 "Name the profile for job lists" "{tool: [Bash], command: [databricks]}" \
  "avoid: {pattern: '(?i)jobs list --all', must_match: ['databricks jobs list --all'], must_not_match: ['databricks jobs list --profile dev']}" \
  "Pass a profile." "The default one." "The job list fails because the default profile is unset."
put l-6e9f3b04 "Install packages with pnpm in this client" "{command: [yarn]}" "" \
  "Run pnpm for installs." "Running yarn for this." "The person corrected the agent here because yarn was not wanted."
tentaqles lobe consolidate >/dev/null
hook() { printf '{"session_id":"%s","hook_event_name":"%s","cwd":"%s","tool_name":"Bash","tool_use_id":"%s","tool_input":{"command":"%s"}%s}' "$2" "$3" "$A" "$4" "$5" "$6" |
  CLAUDE_CONFIG_DIR="$H" tentaqles lobe-hook claude "$1"; }
pre()  { hook pre-tool-use "$1" PreToolUse "$2" "$3" ""; }
fail() { hook post-tool-use-failure "$1" PostToolUseFailure "$2" "$3" ',"error":"source needs an alias"'; }
ok()   { hook post-tool-use "$1" PostToolUse "$2" "$3" ',"tool_response":{"stdout":"1 row"}'; }
M="dbsql -e 'MERGE INTO t USING (SELECT 1)'"
```

Each `put` must print `ok`; if one does not, stop and report FAIL. The first two lessons carry an `avoid`
block, so a call can prove them right or wrong. The third has none and lists no fingerprint.

8. They start silent. `tentaqles lobe rules --json` has `"stage":"trial"` for all three ids. `pre b1 t1 "$M"`
   exits 0 with empty stdout, and `grep -c '"by":"avoid"' "$D/sessions/b1.json"` prints `1`.
9. One earns its way. `fail b1 t1 "$M"`, then `pre b2 t2 "$M"` and `fail b2 t2 "$M"`. `grep -c
   '"verdict":"hit"' "$D/trials.jsonl"` prints `2`. `tentaqles lobe rules --json` still has `"stage":"trial"`
   for `l-4c7d19e2` with `"hits":2`. `tentaqles lobe consolidate >/dev/null`, then the stage is `"advice"`,
   and `grep l-4c7d19e2 "$D/ladder-log.jsonl" | tail -1` holds `"from":"trial"`, `"to":"advice"` and
   `"why":"record"`. `pre b3 t3 "$M"` now prints `Alias the MERGE source` and `not instructions`.
10. One proves wrong and is never shown. For `n` in 1, 2, 3: `pre f$n u$n "databricks jobs list --all"` prints
    nothing, then `ok f$n u$n "databricks jobs list --all"`. `tentaqles lobe consolidate >/dev/null`.
    `tentaqles lobe rules --json` has `"stage":"retired"` for `l-5d8e2af3`, and `grep l-5d8e2af3
    "$D/ladder-log.jsonl" | tail -1` holds `"why":"proved_wrong"`.
11. One waits. `pre w1 v1 "yarn install"` prints nothing. `tentaqles lobe lessons --waiting` prints
    `Install packages with pnpm in this client` and neither of the other two titles. `printf
    '{"session_id":"w2","cwd":"%s","source":"startup"}' "$A" | CLAUDE_CONFIG_DIR="$H" tentaqles lobe-hook
    claude session-start` prints `1 waiting` and `tentaqles lobe lessons --waiting` and does not print
    `Install packages with pnpm`. `CLAUDECODE=1 tentaqles lobe lessons confirm l-6e9f3b04; echo "exit=$?"`
    prints an exit code that is not 0, and `tentaqles lobe rules --json` still has `"stage":"trial"` for it.

**Person checks**, for the owner, in a terminal of their own with the tester's `HOME`, `USERPROFILE` and
`TQ_HOME` exported, from `$W/clients/acme`, with `a.jsonl` and `b.jsonl` in place:

1. `tentaqles lobe backfill --draft template`: it prints the counts and one title with `2 sessions`, and
   waits. Type `no`: no lesson is added and `backfill.json` does not exist.
2. `tentaqles lobe backfill --draft template --yes --json`: exit 0 and exactly one line with
   `"action":"file"`. That lesson has `source: backfill`, `drafted: template` and `strength: 0.4`, and
   `tentaqles lobe rules --json` lists it at `"stage":"trial"`. `$TQ_HOME/identities/acme/lobe/events` and
   `$TQ_HOME/identities/globex/lobe/lessons` are empty or absent. Run it again: `"sessions":0` and no such
   line.
3. `tentaqles lobe lessons confirm l-6e9f3b04`: `tentaqles lobe rules --json` has `"stage":"advice"` for it,
   and a new session-start brief has no `waiting` line.
4. In the client the owner named for phase 0: `tentaqles lobe backfill --dry-run`. It prints counts that fit
   the throwaway count of task 2, and no command, path, error text or prompt.

Scoring: PASS needs 11 of 11 tester checks. "Could not run" is a FAIL. The person checks are reported apart.

## Done when

- [ ] The written result of phase 0 question 4 exists, holds counts only, and the owner has approved the
  three numbers.
- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin suite is not affected.
- [ ] The tester brief passes, 11 of 11.
- [ ] The house rules hold: `TestLobeBackfillOnlyThisClient`, `TestLobeBackfillNeverReadsHome`,
  `TestLobeBackfillForeignSessionTeachesNothing`, `TestLobeBackfillScrubs`, `TestLobeHookAlwaysExitsZero`,
  `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, and the tests under "Must not trade off" in
  `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md`, `docs/getting-started.md`, `docs/threat-model.md` and `CHANGELOG.md` are
  updated.
- [ ] The owner has run a backfill in one real client, seen the mined lessons at `trial`, and seen one reach
  `advice` or confirmed one.

## Risks and unknowns

- **The transcript line shapes are not confirmed.** They come from Trail-code's readers and from synthetic
  fixtures (C-07, risks). Task 2 confirms them on real files before `claude.go` is written. If Claude Code
  keeps subagent turns or the person's prompts in another form than the fixtures assume, steps 5 and 6 change.
- **An empty first run.** Sessions from before tentaqles sit under `~/.claude` and are never read, so a
  person who installs tentaqles today mines nothing (C-07, risks). Task 2 shows what the owner's client holds.
- **Signs in backfill.** C-07 says C-02 runs on each session and gives the two-day rule for repairs only. This
  file lets a sign qualify under C-02's own limits and puts signs after repairs in the cap of 20. A lesson
  mined from one old correction therefore can be filed; it waits at `trial` until the person confirms it.
- **Two JSON keys are this file's.** C-07 lists four skip reasons and shows three keys. `long_lines` and
  `tampered` are added here, and so is the shape of `backfill.json`, which C-07 describes in words only.
- **Model calls before the yes.** Titles are needed for the list, so the model is called before the person
  answers. C-03 counts a day's calls from the `drafted_at` field of candidate lines, and backfill keeps its
  candidates in memory. Task 6 must record the calls where C-03 counts them, with no prompt text, also when
  the person declines. How is not in the requirements.
- **Old tampering.** The mark exists only for sessions the lobe saw live. An older session in which a tool
  wrote into a transcript carries no mark and is read like any other.
- **A foreign session in the right folder.** A session started in one client's folder under another client's
  login is skipped by its `cwd` and counted `foreign` (C-07, risks).
- **Shared mined lessons.** With `share: shared` a mined lesson is filed in the repository. Here it is silent
  at `trial`; on a teammate's machine it passes intake and speaks at once (A-30). The owner may want backfill
  to file private whatever the setting says.
- What would change this file: other numbers from task 2, or Claude Code moving its session files.
