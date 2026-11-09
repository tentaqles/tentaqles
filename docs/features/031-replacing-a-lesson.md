# 031: Replacing a lesson

**Status** not started · **Stage** Lessons · **Entries** C-06 (private lessons only), C-03 (step 5,
`supersedes`), C-04 (step 2, the `repeat` exemption), E-07 (the rows `replaced` and `replacement_undone`), F-07
(`lessons replace`, `history`, `forget`; `lobe-lesson-fork`) · **Needs** 030 · **Feeds** 032, 033, 034, 038

## Result

A lesson is never edited to say something new. A new lesson names the old one in `supersedes`. From then on only
the newest in the chain is shown to the agent, the old file stays untouched as the record, and `history` prints
the chain. Removing the successor brings the old lesson back. When a repair shows that what a lesson says to
avoid is what works, the lobe files the successor by itself.

```sh
tentaqles lobe lessons replace l-aaaa0001 new.md     # a person; prints the new id
tentaqles lobe lessons history l-aaaa0001            # l-aaaa0001 ... replaced / l-bbbb0002 ... live
tentaqles lobe lessons forget l-bbbb0002             # a person; the old lesson is live again
```

## Scope

**In**

- The `supersedes` link and the pure functions `Live`, `Chain` and `LeadsBack` in
  `internal/lobe/lesson/chain.go` (C-06 steps 1 to 4).
- Every reader of lessons shows live lessons only: the header cache, the note before a call (017), the lesson
  after a failure (020), the brief (026) and `tentaqles lobe lessons`.
- `tentaqles lobe lessons replace <old id> <file> [--anyway]` (a person), `tentaqles lobe lessons history <id>`
  (anyone) and `tentaqles lobe lessons forget <id> [--yes]` (a person).
- E-07's two rows in `Settle`: the old lesson to `retired` with the reason `replaced`, and back to its enter
  stage with `replacement_undone` when the successor's file is gone.
- The automatic case (step 5): the candidates that feature 030 left with `supersedes` are worded and filed as
  successors, and a new repair with the fingerprint of a lesson retired as `faded` is drafted as its
  successor.
- The doctor warning `lobe-lesson-fork`.

**Out**

- Anything that needs a shared lesson on disk. Replacing a shared lesson, by the command with `share:
  shared` or by a commit that adds a file, and the end-to-end refusal of a private successor over a real
  shared file (feature 033). A teammate's successor that arrives through git and must pass intake (feature
  034). The rule itself is one line over the `scope` key and is built here; see step 2.
- `tentaqles lobe lessons forget` refusing a shared lesson and naming `rules retire` and `unshare` (feature
  033). Here every lesson is private.
- What the person is told when the automatic case cannot replace a shared lesson (C-06 Risks; not designed).
- Losing an approval when a `block` rule is replaced or comes back: `approvals.json` does not exist yet
  (feature 038 completes `TestLobeReplaceDropsApproval` and `TestLobeReplaceUndoReenters`).
- Stale successors and stale order (feature 032).

## What it builds on

- 016: `internal/lobe/lesson` (parse, write, `id.go`: the id hashes kind, title, sorted fingerprints and
  `supersedes`), the gate with its `repeat` check, `internal/cmd/lobe_lessons.go`, the list `tentaqles lobe
  lessons`, and the header key `supersedes` as a known key.
- 005: `tentaqles doctor` findings from `internal/lobe/health`.
- 017: the header cache in `index/` and the eligible rule ("is not replaced or retired").
- 019, 025: the draft step (`template.go`, `draft.go`) and filing through quarantine.
- 023, 024: `ladder.Settle`, `ladder/apply.go`, `ladder-log.jsonl`, `ladder.Enter`, `tentaqles lobe rules`.
- 026: `lessons confirm`, the other way back for a retired lesson.
- 030: fading, `--only fade` (a short pass that still runs `Settle`), and `pending` candidates that carry
  `supersedes`, which the draft step has skipped until now.

## What it leaves for later features

- `lesson.Live(all []Lesson) []Lesson`, `lesson.Chain(all []Lesson, id string) []Lesson` and
  `lesson.LeadsBack(all []Lesson, newID, oldID string) bool`. 033 adds the rule that a private lesson never
  hides a shared one; 034 feeds pulled files in; 038 asks `Live` before a deny.
- The commands `lessons replace`, `lessons history` and `lessons forget`; 033 widens the refusals.
- The reasons `replaced` and `replacement_undone` in `ladder-log.jsonl`.
- The finding `lobe-lesson-fork`.

## How it works

1. **The link** (step 1) is one header key on the successor: `supersedes: <old id>`. The old file is not
   touched.
2. **Who is replaced** (step 2). Lesson X is replaced when some lesson R has `supersedes: X`, passed the gate
   (it sits in `lessons/`, not in quarantine), is not retired, and is not a private lesson pointing at a
   shared one. That last rule reads the `scope` key of the two parsed lessons. No shared lesson exists before
   feature 033, so here a unit test over parsed lessons holds it. `Live` computes it from the parsed files each
   time the header cache is refreshed. A cache older than the lessons folder is not used: a reader then takes
   the headers from the files (R-04), so `tentaqles lobe rebuild` is never required and a successor placed or
   removed by hand counts at the next read.
3. **Chains and forks** (step 3). When B replaces A and C replaces B, only C is live and no file is rewritten.
   Two lessons that replace the same old one are both live; `tentaqles doctor` reports one `lobe-lesson-fork`
   warning per such old lesson, naming the three ids.
4. **Circles** (step 4). `replace` refuses a link whose chain would lead back to the new lesson, or that names
   the lesson itself. A hand edit can still put a circle on disk, so the reader keeps the ids it has passed
   and stops at the first one it meets twice. Of the lessons in the circle, the one with the latest `created`
   is live, the greater id on a tie.
5. **The ladder follows** (E-07). `Live` changes no stage. In the next pass `Settle` sets a replaced lesson to
   `retired` with the reason `replaced`. `Settle` checks replaced, replacement undone, faded, tests, pattern,
   idle, counts, in that order, and moves a lesson once per run.
6. **The successor** (step 6) enters by E-01's enter rule, with no record, whatever stage the old lesson had:
   `advice` when learned or written here (D-9), never above. Nothing of the old entry moves to it.
7. **`tentaqles lobe lessons replace <old id> <file>`**, a person only (the check of `lessons add`, in
   `internal/cmd/lobe_lessons.go`). It finds the old lesson before the gate runs, sets `supersedes` and
   `source: person`, computes the id, runs the gate with the old lesson exempt from `repeat`, and prints the new
   id. It exits 1 and writes nothing when the old id is unknown, when the link would close a circle or name
   itself, when the file fails the gate, or when a private lesson would replace one whose `scope` is `shared`.
   `--anyway` passes `why`, `status` and `repeat` only, as in `add`.
8. **`tentaqles lobe lessons history <id>`** prints the chain that holds the id, oldest first, one line per
   lesson: id, `created`, title, and `live` or `replaced`. Anyone may run it; it only reads.
9. **`tentaqles lobe lessons forget <id> [--yes]`**, a person only (refuses under `CLAUDECODE`, asks for a
   typed yes unless `--yes`). It deletes a private lesson's file under the store lock and refreshes `index/`.
   It writes no stage: `Settle` retires the entry of a lesson whose file is gone in the next pass. E-07 has
   no reason for that row; this file logs it as `why` `person`, `by` `settle` (this file's choice).
10. **Undo** (step 8) is removing the successor's file, with `forget` or by hand. The old lesson is live again
    at the next read. In the next pass `Settle` finds an entry retired as `replaced` (read from
    `ladder-log.jsonl`) whose lesson is live again and brings it back through the enter rule, with the reason
    `replacement_undone`: never above `advice`, with counts 0. This is the one case in which a retired entry
    returns without a person's word.
11. **The automatic case** (step 5). A `pending` candidate that names a lesson under `supersedes` (030: the
    third answer of `route(pair)`, when the working call matched that lesson's `avoid.pattern`) is now worded
    like any pair. The lobe, not the model,
    sets `supersedes` on the draft (C-03 step 5). The gate exempts the named lesson from `repeat` (C-04 step
    2). Once the draft passes, the successor is filed and steps 2 and 5 do the rest. A draft that fails the
    gate files nothing, and the old lesson stays live, already demoted by its false alarm. In the same way a
    new repair whose fingerprint is listed by a lesson retired as `faded` is drafted as a successor that names
    it. When the working call does not match the pattern, or the lesson has none, nothing is replaced: 030
    reinforces.
12. **Output.** `--json` on `tentaqles lobe consolidate` prints `{"action":"replace","lesson":"<new
    id>","supersedes":"<old id>"}` after the `file` line of a successor (key names proposed).

## Where to read

| What | Where |
|---|---|
| Requirements | [C-06](../prd/lobe-consolidate.md), all steps, read without the cases that concern a shared lesson; [C-03](../prd/lobe-consolidate.md) step 5; [C-04](../prd/lobe-consolidate.md) steps 1 and 2; [C-01](../prd/lobe-consolidate.md) step 5; [C-05](../prd/lobe-consolidate.md) step 6; [E-07](../prd/lobe-enforce.md), the rows `replaced` and "a replacement is undone", step 3 and "How to build it"; [F-07](../prd/lobe-foundation.md), the rows `lessons replace`, `history` and `forget`, and the doctor row `lobe-lesson-fork` |
| Source to port | [`src/notes/learning-fields.ts` L193](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/learning-fields.ts#L193): the newer learning wins; [L191](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/learning-fields.ts#L191): one that is held replaces nothing; [`src/cli.ts` L1379](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L1379): the old learning is found before the gate runs, and an unknown name stops the save; [`src/notes/note-gate.ts` L112](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/note-gate.ts#L112): the replaced learning is exempt from the repeat rule |
| Source, weakness we avoid | [`src/cli.ts` L1468](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L1468): "takes over from" is printed in every case, also when the old learning stays live |
| BASE | Idea only, nothing ported: the link between two records is the fact, chains are walked to their live end, a circle is refused on write and the walk still ends if one exists |
| Existing tentaqles code | `internal/lobe/lesson`, `internal/lobe/gate` (`repeat`), `internal/lobe/ladder` (`settle.go`, `apply.go`, `enter.go`), `internal/lobe/consolidate` (`draft.go`, `template.go`, `candidates.go`), `internal/lobe/health`, `internal/cmd/lobe_lessons.go`, `internal/cmd/doctor.go` L45 to L54 (the text output omits a finding's code; only `--json` has it) |

## Build tasks

1. **Check this brief against the code.** Read where the header cache computes "replaced", what `Settle` does
   today for the rows `replaced` and `replacement_undone`, how 030 skips a candidate with `supersedes`, and
   whether `forget` exists. List what no longer fits. The owner approves changes first.
2. **The chain functions.** `internal/lobe/lesson/chain.go`, `chain_test.go`. Proof: `TestLobeReplaceHidesOld`,
   `TestLobeReplaceCycle` (the disk half).
3. **Readers use `Live`.** The cache refresh, the fallback read from files, the list command. Proof:
   `TestLobeReplaceOnlyNewestIsShown`.
4. **`Settle`** (side by side with 3). The two rows. Proof: `TestLobeDemote_Table` (its rows `replaced` and
   `replacement_undone`), `TestLobeDemote_UndoComesBackAtMostAdvice`, `TestLobeReplaceUndoReenters`,
   `TestLobeReplaceDropsApproval`.
5. **`replace` and `history`** (side by side with 3 and 4). Files: `internal/cmd/lobe_lessons.go`,
   `internal/lobe/cli`. Proof: `TestLobeReplaceCycle` (the write half), `TestLobeReplaceCommandRefusals`,
   `TestLobeCommandTable`, `TestLobePersonVerbsRefuseInAgent`.
6. **`forget`** (side by side with 5). Proof: `TestLobeForgetDeletesOnlyTheFile`.
7. **The automatic case.** The draft step words a candidate with `supersedes`; the faded case; the `replace`
   action line. Files: `consolidate/draft.go`, `template.go`, `run.go`; the gate's `repeat`. Proof:
   `TestLobeReplaceWhenAvoidedWorks`, `TestLobeReplaceFadedLessonRelearned`.
8. **The doctor finding** (side by side with 5 to 7). Files: `internal/lobe/health`. Proof:
   `TestLobeReplaceForkFinding`, `TestLobeDoctorCodes`.
9. **Docs.** `docs/reference/commands.md`: `replace`, `history` and `forget` under `tentaqles lobe lessons`;
   the `replace` action line under `tentaqles lobe consolidate`. `docs/troubleshooting.md`: one entry for
   `lobe-lesson-fork`. `CHANGELOG.md`. The plugin does not change.
10. **Tester run**, then the person checks.

## Tests that stay in the repository

- `TestLobeReplaceHidesOld`: after B replaces A, `Live` returns B only; in a chain A, B, C it returns C only.
- `TestLobeReplaceCycle`: writing a circle is refused; a circle on disk gives exactly 1 live lesson.
- `TestLobeReplaceDropsApproval`: an old lesson stored at `block` gives a successor that is not at `block` and
  has counts 0. Feature 038 adds the `approvals.json` half.
- `TestLobeReplaceUndoReenters`: an old lesson stored at `block` is replaced, then the successor's file is
  removed; after the next pass the old lesson is live, at `advice` or lower by the enter rule, with 0 hits.
  Feature 038 adds "no line in `approvals.json`".
- `TestLobeReplaceWhenAvoidedWorks`: a repair with an active lesson's fingerprint whose working call matches
  its `avoid.pattern` files 1 successor with `supersedes` once the draft passes the gate, and `Live` returns
  the successor only; a draft that fails the gate files nothing and the old lesson stays live.
- `TestLobeReplaceForkFinding`: two live lessons that replace one old lesson give 1 `lobe-lesson-fork` finding.
- `TestLobeDemote_UndoComesBackAtMostAdvice`: a lesson that held `block` before it was replaced returns, when
  the successor's file is deleted, at its enter stage with 0 counts.
- `TestLobeReplaceOnlyNewestIsShown` (new): with A replaced by B, the hook before a matching call, the hook
  after a listed failure and the brief print B and never A, also when the header cache is out of date.
- `TestLobeReplaceCommandRefusals` (new): `replace` exits 1 and writes 0 files for an unknown old id, a link
  to itself and a file that fails the gate; with `--anyway` a file failing only `why` is filed with
  `anyway: true`.
- `TestLobeReplacePrivateOverSharedRefused`: over parsed lessons, a private lesson that names a `scope: shared`
  lesson replaces nothing in `Live`, and the command's check refuses the pair and writes 0 files. Feature 033
  runs the same test against a shared file on disk.
- `TestLobeForgetDeletesOnlyTheFile` (new): `forget` removes one file, writes no stage, and refuses under
  `CLAUDECODE=1` with 0 bytes changed; after the next `Settle` the entry is `retired` with one log line.
- `TestLobeReplaceFadedLessonRelearned` (new): a new repair with the fingerprint of a lesson retired as `faded`
  files 1 lesson that names it in `supersedes`; the faded lesson's file is byte-identical.
- House rules. No hook changes: `TestLobeHookAlwaysExitsZero` and `TestLobeHookServesOnlyItsLogin` stay green.
  The quarantine holds: `TestLobeQuarantineIsNeverRecalled`, and a successor in quarantine replaces nothing.
  One writer of stages: `TestLobeDemote_OneWriter`. `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, `TestDocsReferenceEveryCommand`.

## Evaluation

The tester gets the built `tentaqles` first on `PATH` and an empty working folder. Use bash (Git Bash on
Windows). No network. Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
A="$W/clients/acme"; cd "$A"; D="$TQ_HOME/identities/acme/lobe"; L="$D/lessons"; mkdir -p "$L"
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private,\n' "$1" "$2"
  printf ' applies: client, when: {command: [dbsql]}, strength: 1.0, seen: 0, created: %s%s}\n---\n' "$4" "$5"
  printf '## Do\n%s\n## Avoid\n`SELECT TOP n`.\n## Why\n' "$3"
  printf 'The engine rejects TOP with a syntax error because it does not support it.\n'; }
mk l-aaaa0001 "Use LIMIT, not TOP, on this warehouse" 'Write `SELECT ... LIMIT n`.' 2026-11-01 \
  > "$L/l-aaaa0001.md"
new() { mk "$1" "$2" 'Write `SELECT ... FETCH FIRST n ROWS ONLY`.' 2026-11-09 ", supersedes: l-aaaa0001"; }
new l-bbbb0002 "Use FETCH FIRST, not LIMIT or TOP, here" > "$L/l-bbbb0002.md"
printf '%s\n' '{"v":1,"entries":[{"lesson":"l-aaaa0001","stage":"advice","hits":3,"false_alarms":0,
"sessions":2,"approved_at":null}]}' | tr -d '\n' > "$D/ladder.json"
cksum "$L/l-aaaa0001.md" > "$W/old.sum"
SETTLE() { tentaqles lobe consolidate --only fade; echo "exit=$?"; tentaqles lobe rules --json; }
hook() { printf '{"session_id":"%s","cwd":"%s","tool_name":"Bash","tool_input":{"command":"dbsql -f q.sql"}}' \
  "$1" "$A" | CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude" tentaqles lobe-hook claude pre-tool-use; }
```

No line may start with `SETUP FAIL`. `tentaqles lobe lessons replace` answers only to a person, so the successor
is written by hand. `SETTLE` runs a short pass, which still makes E-07's moves, and prints the ladder.

1. `tentaqles lobe lessons history l-aaaa0001`: exit 0 and two lines; the first starts with `l-aaaa0001` and
   ends with `replaced`, the second starts with `l-bbbb0002` and ends with `live`.
2. Only the newest is shown. `hook h1`: the `additionalContext` contains `Use FETCH FIRST` and does not
   contain `Use LIMIT, not TOP, on this warehouse`.
3. `SETTLE`: `exit=0`; the entry for `l-aaaa0001` has `"stage":"retired"`; `$D/ladder-log.jsonl` has a line with
   `replaced`; `cksum "$L/l-aaaa0001.md" | cmp - "$W/old.sum"` is silent.
4. A circle on disk. `cp "$L/l-aaaa0001.md" "$W/keep.md"`, then `sed -i.bak 's/created: 2026-11-01}/created:
   2026-11-01, supersedes: l-bbbb0002}/' "$L/l-aaaa0001.md"; rm "$L/l-aaaa0001.md.bak"`. `tentaqles lobe lessons
   history l-bbbb0002` exits 0 within 5 seconds and prints two lines, exactly one ending with `live`. Then `cp
   "$W/keep.md" "$L/l-aaaa0001.md"`.
5. Undo. `rm "$L/l-bbbb0002.md"`, then `tentaqles lobe lessons history l-aaaa0001`: one line, ending with
   `live`. `SETTLE`: the entry for `l-aaaa0001` has `"stage":"advice"` and `"hits":0`, and no `approved_at` time;
   `$D/ladder-log.jsonl` has a line with `replacement_undone`. `hook h2` contains `Use LIMIT, not TOP, on this
   warehouse`.
6. Fork. `new l-bbbb0002 "Use FETCH FIRST, not LIMIT or TOP, here" > "$L/l-bbbb0002.md"` and `new l-cccc0003
   "Page with OFFSET and FETCH on this warehouse" > "$L/l-cccc0003.md"`. `tentaqles doctor --json | grep -c
   '"code":"lobe-lesson-fork"'` prints `1`.
7. A person only. `CLAUDECODE=1 tentaqles lobe lessons replace l-aaaa0001 "$W/keep.md"; echo "exit=$?"` and
   `CLAUDECODE=1 tentaqles lobe lessons forget l-cccc0003 --yes; echo "exit=$?"`: neither exit code is 0, and
   `ls "$L" | wc -l` still prints `3`.

**Person checks** (for the owner, at a terminal, with the tester's `HOME`, `USERPROFILE` and `TQ_HOME` exported,
in `$A`, after `rm "$L"/l-bbbb0002.md "$L"/l-cccc0003.md`)

1. Write the successor without an id and file it:
   ```bash
   mk x "Use FETCH FIRST, not LIMIT or TOP, here" 'Write `SELECT ... FETCH FIRST n ROWS ONLY`.' 2026-11-09 |
     sed 's/id: x, //' > "$W/new.md"
   tentaqles lobe lessons replace l-aaaa0001 "$W/new.md"
   ```
   Expected: exit 0 and an id on stdout; `$L` holds two `.md` files; the new one names `l-aaaa0001` under
   `supersedes`; `cksum "$L/l-aaaa0001.md" | cmp - "$W/old.sum"` is silent. The same command with `l-ffff9999`
   as the old id: exit 1, still two files.
2. `tentaqles lobe lessons forget <new id>`, typing `yes`, then `tentaqles lobe lessons history l-aaaa0001`: one
   line, ending with `live`.

PASS needs 7 of 7. "Could not run" is a FAIL. The person checks are reported apart.

## Done when

- [ ] Every test above is in the repository and green in CI: `go vet ./...`, `go test ./... -count=1`,
  `go build ./...`.
- [ ] The tester brief passes 7 of 7.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeQuarantineIsNeverRecalled`, `TestLobeDemote_OneWriter`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, and every test under "Must not trade off" in `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md`, `docs/troubleshooting.md` and `CHANGELOG.md` are updated.
- [ ] The owner has replaced a lesson, read its history and undone the replacement.

## Risks and unknowns

- The automatic case needs an `avoid.pattern` on the old lesson. A template writes none (C-03 step 7), so in
  practice only a model-worded or hand-written lesson can be replaced this way; a lesson without a pattern is
  only reinforced by a contradicting repair. M-04 should show whether that leaves wrong lessons in place.
- `lessons confirm` on a lesson that is replaced while its successor is live is not covered by the
  requirements. As written here, `confirm` moves the entry and the next `Settle` retires it again, because
  `Live` is computed from the files. The owner may prefer that `confirm` refuses and names the successor.
- Undo brings an old lesson back at `advice` at most. That is intended, and it will matter from feature 038:
  a rule that blocked before its replacement does not block after the undo until it is approved again.
- A successor placed by hand counts as soon as its file is in `lessons/`, gate or no gate: the gate runs on
  `add`, `replace` and on drafts, not on a read. A file that would fail the gate still hides the old lesson.
  Feature 034 closes this for pulled files with intake; for a private file edited by hand it stays open.
- The key names of the `replace` action line are proposed; C-01 names the action and no keys.
- `forget` on a lesson that others in the chain point at leaves a successor whose `supersedes` names no file.
  `Chain` must treat a missing link as the chain's start.
- What forces a change to this file: an id that does not include `supersedes` in its hash (two successors
  with one title would then clash); a `Settle` that cannot read the reason of an earlier retirement from
  `ladder-log.jsonl`.
