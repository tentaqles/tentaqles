# 033: Share a lesson with teammates

**Status** not started · **Stage** Lessons · **Entries** F-04 (filing: steps 1 to 5 and 7), F-06 (`share`),
F-05 (the tamper mark, repository part), F-07 (`share`, `unshare`, one doctor finding), C-04 (where a lesson is
filed), C-05 (`shared-strength.json`) · **Needs** 031 · **Feeds** 034, 035, 036, 040

## Result

A lesson can live in the repository it is about, at `<repo>/.tentaqles/lessons/<id>.md`. `git status` shows it
as a new file, and the person commits and pushes it like any other file. With `share: shared` a lesson learned
in a repository is filed there from the start. A private lesson moves there with one command, and back with
another. The lobe never runs `git add`, `git commit` or `git push`. On the machine that wrote it, a shared
lesson speaks exactly as it did while it was private.

```bash
tentaqles lobe set share shared            # a person: new lessons go to the repository of their session
tentaqles lobe lessons share l-4c7d19e2    # a person, inside the repository: move one private lesson there
git status --porcelain                     # ?? .tentaqles/
tentaqles lobe lessons --json              # the lesson has "scope":"shared" and "state":"ok"
tentaqles lobe lessons unshare l-4c7d19e2  # a person: move it back to the private store
```

## Scope

**In**

- The `share` setting acts: a new lesson that passed the gate is filed private or shared (F-04 step 2).
- `tentaqles lobe lessons share <id> [--yes]` and `tentaqles lobe lessons unshare <id> [--yes]` (F-04 steps 3
  and 4), both person-only.
- The header of a shared lesson: `scope: shared`, `origin.by: team`, no `session`, no `events`, no `repo`.
- What may not be shared: a lesson with `anyway: true`, a lesson that fails the whole gate, a quarantined
  draft, anything asked for inside an agent session, anything outside a repository.
- `intake.json` in its first form: one entry per shared file this machine wrote, with `"by":"me"`.
- Recall of a shared lesson this machine wrote: the header cache and the hook's fallback reader learn the
  second place, and `applies` is read against the repository the file sits in (F-04 step 7).
- `shared-strength.json`: this machine's strength numbers for a shared lesson (C-05 step 8).
- The leak guards of F-04 step 5 that need no commit hook: private files stay outside every checkout, the
  doctor finding `lobe-private-in-repo`, and `tentaqles lobe lessons forget` refusing a shared lesson.
- The tamper mark, first form: a session whose tool call writes into `<repo>/.tentaqles/` teaches nothing.

**Out**

- The intake check of a file this machine did not write, the states `uncommitted` and `rejected`, the two git
  calls, the width rule and `"wide":true`, a teammate's successor (feature 034).
- The tamper mark for the client's `lobe` folder, the manifest and the session transcripts (feature 034).
- The warning before a commit about a staged private lesson (E-06, feature 040).
- `tentaqles lobe rules approve` refusing a lesson with `anyway: true` (feature 038).
- Everything in a Codex session (feature 036).

## What it builds on

- 002: the sandbox and the standard tester setup, with the helper `lobeset`.
- 003: `internal/lobe/store` (`Open`, `RepoKey`, `Update`, the store lock, whole-file replace),
  `internal/lobe/hook` (the steps of the two post-tool events), `internal/lobe/shellwords`, the session file
  `sessions/<session>.json` in which each entry owns its keys, `internal/lobe/scrub`.
- 005: `internal/lobe/settings` (`From`, the nine keys, `share` read but not yet used), `tentaqles lobe set`,
  `tentaqles lobe status`, `internal/lobe/health` and its findings in `tentaqles doctor`.
- 016: `internal/lobe/lesson` (parse, write, `id.go`), `internal/lobe/gate` (`gate.Check`), the commands
  `lessons`, `lessons show`, `lessons gate`, `lessons add` with `--anyway`, the person check in
  `internal/cmd/lobe_lessons.go`, `internal/lobe/safety` (`origin.go`, `shape.go`).
- 017: `internal/lobe/recall` (the header cache in `index/`, the fallback reader, `applies`),
  `internal/lobe/ladder` (`Enter`).
- 018 and 019: the detached pass, `candidates.jsonl`, the filing code of C-04 step 4, which today always writes
  into `lessons/`.
- 024: `tentaqles lobe rules retire`. 030: strength in a private lesson's header. 031: `lesson.Live`,
  `lessons replace` and its refusal of a private lesson over a shared one, `lessons forget`. 032:
  `recall.Gone`, which judges `touches` against a repository root.

## What it leaves for later features

- Package `internal/lobe/share`, file `share.go`: `Place`, `ToShared`, `ToPrivate`, `RepoRoot`. Feature 034
  adds `intake.go` beside it.
- `intake.json` in the store, shape of F-04: `{"v":1,"repos":{"<repo-key>":{"root":"app","lessons":{"<name>":`
  `{"sha256":"...","verdict":"ok","by":"me"}}}}}`, compact JSON. Feature 034 adds `"by":"pull"`,
  `"verdict":"rejected"`, `reason` and `wide`. `tentaqles doctor` (here and in 040) and feature 034 find
  repositories through its `root` values.
- The rule every reader keeps: a shared file is read only when `intake.json` lists its name as `ok` with the
  SHA-256 of its present bytes (034, 035, 036, 040).
- `shared-strength.json` filled for real (030 defined its shape).
- `internal/lobe/safety/tamper.go` and the key `tampered` of the session file, for one protected place.
  Feature 034 adds the other places; features 035 and 036 read the mark.
- The commands `lessons share` and `lessons unshare`, and `"scope":"shared"` rows in `lessons --json`.
- The doctor finding `lobe-private-in-repo` (034 and 040 rely on it).

## How it works

1. **Two places, one format** (F-04 step 1, core page 5.3). Private:
   `$TQ_HOME/identities/<client>/lobe/lessons/<id>.md`. Shared: `<repo>/.tentaqles/lessons/<id>.md`. The
   repository must lie inside the client's folder. `scope` in the header says which place a file belongs to.
2. **Where a new lesson is filed** (F-04 step 2). The filing code of C-04 step 4 calls `share.Place` just
   before it writes. The answer is shared only when all three hold: the `share` setting is `shared`; the lesson
   carries no `anyway: true`; a repository is known for it. Otherwise it is private. The repository is the one
   of the current folder for `lessons add` and `lessons replace`, and the one of the candidate's events for a
   lesson the pass files. `share.RepoRoot(key)` turns the `repo` key of those events into a folder from two
   places only: the repository of the folder the pass runs in, when its key is the same, and the `root` values
   in `intake.json`. No folder found means private. The setting is read at filing time; changing it later
   moves nothing (F-06 step 1).
3. **The shared header** (F-04 steps 3 and 7, F-05 step 1). `share.ToShared` sets `scope: shared` and
   `origin.by: team`, removes `origin.session` and `origin.events`, and removes `repo`. `origin.host`,
   `origin.drafted` and `origin.lobe` stay. Nothing in the file names the client or a path of this machine.
4. **Scrub and gate on the final bytes.** The scrubber (S-05) and the whole gate (C-04) run on the bytes that
   will be written, with no bypass. One problem refuses the write. The lesson's own private file is left out of
   the `repeat` comparison, and so is the lesson named in `supersedes`. `grounded` runs as `tentaqles lobe
   lessons gate <file>` runs it when no evidence is at hand (feature 016).
5. **The write.** Under the store lock: create `.tentaqles/lessons/` if needed, write a temporary file in that
   folder, rename it to `<id>.md`. A target that already exists with other bytes refuses the write. Then
   `store.Update` records the file in `intake.json` under this machine's key for that repository, with `root`
   (the path relative to the client folder), the SHA-256 of the bytes, `"verdict":"ok"` and `"by":"me"`. The
   header cache in `index/` is refreshed. No git command runs. The lobe writes LF line ends. The SHA-256 is
   taken with every CRLF read as LF, so a later checkout under git's `core.autocrlf` does not change it, and
   the lesson parser reads both line ends.
6. **The ladder.** A lesson filed shared by step 2 enters through `ladder.Enter` exactly as a private one does:
   by its `source` (E-01's enter rule). A lesson moved by `share` keeps its entry, because its id does not
   change.
7. **`tentaqles lobe lessons share <id> [--yes]`.** In order, each refusal with one line, exit 1 and no write:
   the command runs inside an agent session; the current folder is in no repository of this client; `<id>` is
   not a private lesson in `lessons/` (a quarantined draft is not one); the lesson carries `anyway: true` (the
   line names `--anyway`); step 4 finds a problem (one line per problem). Deleting the `anyway` key by hand
   gains nothing, because the fault that needed the bypass fails step 4. Then the command prints the file as it
   will be written and waits for a typed `yes`; `--yes` skips only that. Then: step 5, the entry in
   `shared-strength.json` (step 10), and last the removal of the private file. A run that stopped half way
   finds the target with the same bytes and finishes. It ends by printing the path and one line saying that the
   commit is the person's to make.
8. **`tentaqles lobe lessons unshare <id> [--yes]`** does the reverse, inside the repository the file sits in.
   It sets `scope: private` and `origin.by: me`, writes `repo` with this machine's key when `applies` is `repo`
   or `path` or `touches` is not empty (core page 5.4), writes this machine's strength numbers back into the
   header, and runs the gate's `parse` and `form` checks on the result. After the typed `yes` it writes the
   private file, deletes the repository file, and removes the entries in `intake.json` and
   `shared-strength.json`. The deletion in the repository is the person's to commit; git history keeps the old
   text.
9. **Recall.** The header cache lists, beside the private lessons, every shared file `intake.json` records as
   `ok`, in every repository of the client it knows. The hook's fallback reader, used when the cache is out of
   date, reads the private folder and the `ok` files of the repository of `cwd` (R-04). Before a hook prints a
   shared lesson it opens the file, as it does for any lesson, and compares the SHA-256 of the bytes with the
   recorded one; on a difference the lesson is skipped. For a shared lesson `applies: repo` means the
   repository the file sits in, `applies: path` means there and under a `when.place` entry, and `applies:
   client` means every repository of the client this checkout is in (F-04 step 6). On the machine that wrote
   it, the lesson takes the width its `applies` states at once.
10. **Strength** (C-05 step 8). The shared file is never rewritten for a number. On `share` the header's
    `strength`, `seen`, `last_seen` and `last_faded` seed this lesson's entry in `shared-strength.json`, and
    reinforcement and fading (feature 030) work on that entry from then on.
11. **Listing.** `tentaqles lobe lessons [--json]` lists shared lessons of the client with `"scope":"shared"`.
    A file this machine wrote has `"state":"ok"`.
12. **Leak guards** (F-04 step 5). Only steps 5 and 7 write into `.tentaqles/lessons/`, and they write `scope:
    shared` only. `internal/lobe/health` reports `lobe-private-in-repo` (level error) for each file under a
    known repository's `.tentaqles/lessons/` whose header says `scope: private`; it finds repositories through
    the `root` values in `intake.json` and never walks a client folder (F-07). `tentaqles lobe lessons forget`
    refuses a shared lesson and names `rules retire` and `unshare` (F-07).
13. **Tamper mark, first form** (F-05 step 5). On `post-tool-use` and `post-tool-use-failure`, after the event
    line is appended, `safety.Tampered(tool, input)` reads the payload. It answers yes for an edit tool whose
    cleaned file path, links resolved and case folded on Windows, lies under the `.tentaqles` folder at the
    root of a repository of this client. For a shell tool it first looks for the folder name in the command,
    then splits the command with `internal/lobe/shellwords` and checks redirect targets and the arguments of
    programs that write (`tee`, `cp`, `mv`, `rm`, `sed -i`, `Set-Content`). A yes sets `"tampered":true` in
    `sessions/<session>.json`; the key is absent until then and is never cleared. The pass takes no repair
    pair and no sign from that session, so it files nothing from it, private or shared; the events stay in the
    log. A read is not a write. The lobe's own commands are not writes in this sense. With `capture: off` the
    check does not run. `tentaqles lobe status --json` counts such sessions as `tampered_sessions`.
14. **Edge cases.** `replace` of a shared lesson files a shared successor when `share` is `shared`; with
    `share: private` it is refused, as 031 built it. A successor the pass drafts for a shared lesson while
    `share` is `private` is filed private and replaces nothing (C-06 names this gap in its risks). After
    `unshare`, a lesson that named a shared lesson in `supersedes` no longer replaces it: `lesson.Live` returns
    the old one again. `tentaqles drop` removes the client's store and leaves repository lessons where they are
    (F-01 step 11).

## Where to read

| What | Where |
|---|---|
| Requirements | [F-04](../prd/lobe-foundation.md), steps 1 to 5 and 7, "How to build it", the person checks; [F-05](../prd/lobe-foundation.md), step 1 (the "On share" column) and step 5; [F-06](../prd/lobe-foundation.md), the `share` row; [F-07](../prd/lobe-foundation.md), the rows `lessons share`, `lessons unshare`, `lessons forget` and the finding `lobe-private-in-repo`; [C-04](../prd/lobe-consolidate.md), steps 3 and 4 and the `form` row; [C-05](../prd/lobe-consolidate.md), step 8; [C-06](../prd/lobe-consolidate.md), step 2 and the first risk; [R-04](../prd/lobe-recall.md), "How to build it" on the header cache; [core page 5.3 and 5.4](../prd/vertical-lobe.md) |
| Source to port | [`src/notes/notes.ts` L303](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/notes.ts#L303): one format, two folders. [`src/notes/learning-fields.ts` L192](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/learning-fields.ts#L192): a personal learning never replaces a shared one. [`src/trace/tamper.ts` L3](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/tamper.ts#L3): the mark and, at L20, the protected places. [`src/trace/runs.ts` L196](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/runs.ts#L196): the mark is set once |
| Source not taken | [`src/cli.ts` L1699](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L1699): Trail's default is team. [`src/functions/team.ts` L62](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/functions/team.ts#L62): content is scrubbed on share, the project name is not |
| Existing tentaqles code | `internal/cmd/edit.go` and `internal/cmd/reach.go` (the agent-session refusal, the typed yes, `--yes`); `internal/cmd/doctor.go` (how findings are printed); `internal/cmd/drop.go` (what `drop` removes); `test/integration/docs_test.go` (`documentedGroups`) |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits. Look in particular at: the
   name and shape of the filing function of 019; whether 016 already wrote `anyway` refusals for `share`; how
   `gate.Check` runs `grounded` with no evidence; whether 031's `forget` already refuses a shared lesson;
   whether an earlier feature already wrote the `tampered` key; which folder the detached pass runs in. The
   owner approves changes to this file before code is written.
2. `internal/lobe/share/share.go`: `Place`, `ToShared`, `ToPrivate`, `RepoRoot`, and the `intake.json` reader
   and writer on `store.Update`. Proof: `TestLobeOriginBlock` (the part after share),
   `TestLobeSharedLessonRepoIsWhereItLives` (cases 1 and 2).
3. Side by side with 2: `internal/lobe/safety/tamper.go`, its call in the two post-tool steps of
   `internal/lobe/hook/steps.go`, the skip in the pass, the count in `status`. Proof: `TestLobeTamperRepoWrite`.
4. The filing code calls `share.Place` and writes by step 5. Files: the filing code of 019, `lessons add` and
   `lessons replace` in `internal/cmd/lobe_lessons.go`. Proof: `TestLobeShareSettingFilesInRepo`,
   `TestLobePrivateNeverInRepo`.
5. Recall reads the second place: the cache refresh, the fallback reader, the SHA-256 check before a print,
   `applies` for a shared lesson, `recall.Gone` given the root from `intake.json`. Files: `internal/lobe/recall`.
   Proof: `TestLobeSharedLessonIsRecalledHere`, `TestLobeSharedLessonStaysInItsClient`,
   `TestLobeSharedLessonLineEnds`.
6. The commands `lessons share` and `lessons unshare` in `internal/cmd/lobe_lessons.go`, and the listing.
   Proof: `TestLobeShareMovesOneFile`, `TestLobeShareRefusesAnyway`, `TestLobeUnshareRestoresPrivate`,
   `TestLobeCommandTable`, `TestLobePersonVerbsRefuseInAgent`.
7. Side by side with 6: `shared-strength.json` on share, unshare, reinforce and fade
   (`internal/lobe/consolidate/strength.go` callers). Proof: `TestLobeShareStrengthStaysLocal`.
8. Side by side with 6: the finding `lobe-private-in-repo` in `internal/lobe/health`, and the refusal in
   `lessons forget`. Proof: `TestLobeDoctorCodes`, `TestLobeForgetRefusesShared`,
   `TestLobeRetireMutesOnlyHere`.
9. Docs: the `lessons` section of `docs/reference/commands.md` gains `share` and `unshare`;
   `docs/reference/manifest.md` says what `share` does; one line in `CHANGELOG.md` under `[Unreleased]`. The
   plugin does not change.
10. The tester run of the Evaluation below, then the owner's person checks.

## Tests that stay in the repository

Names marked (new) are this file's; the others are the requirements' names.

- `TestLobeShareMovesOneFile`: after share, 1 file in the repository, 0 in the private store, 0 git commits.
- `TestLobePrivateNeverInRepo`: 1,000 lessons filed with `share: private` (proposed size); 0 files in any
  checkout.
- `TestLobeShareRefusesAnyway`, cases 1 and 2 of 3: `share` refuses a lesson with `anyway: true`, and still
  refuses after the key is deleted by hand. `TestLobeGateAnywayStaysPrivate` holds the same two from the
  gate's side. Case 3 of each arrives in 034.
- `TestLobeSharedLessonRepoIsWhereItLives`, cases 1 and 2 of 3: a shared file is filed under this machine's
  key for the repository it sits in and `recall.Gone` judges its `touches` there; after `share` the file has
  no `repo` key.
- `TestLobeOriginBlock`, the part after share: `by` is `team`, and `session` and `events` are gone.
- `TestLobeRetireMutesOnlyHere`: after `rules retire` on a shared lesson the repository file is
  byte-identical and 0 notes are printed on this machine.
- `TestLobeShareSettingFilesInRepo` (new): with `share: shared`, a repair lesson of a session in a repository
  lands in that repository; one of a session outside any repository is private; one filed with `--anyway` is
  private. 3 of 3. Changing the setting afterwards moves 0 files.
- `TestLobeSharedLessonIsRecalledHere` (new): an uncommitted shared lesson this machine wrote is printed
  before its matching call; after one byte of the file changes it is not. 2 of 2.
- `TestLobeSharedLessonLineEnds` (new): the same lesson with LF and with CRLF line ends parses to the same
  header and has the same recorded SHA-256. Yes or no.
- `TestLobeUnshareRestoresPrivate` (new): the private file is back with `scope: private`, `by: me` and the
  `repo` key it needs; the repository folder holds 0 lessons; `intake.json` has no entry for it.
- `TestLobeShareStrengthStaysLocal` (new): 3 reinforcements of a shared lesson change 0 bytes in the
  repository and 1 entry in `shared-strength.json`.
- `TestLobeForgetRefusesShared` (new): exit 1, 0 bytes changed, the output names `rules retire` and `unshare`.
- `TestLobeTamperRepoWrite` (new): an `Edit` under `<repo>/.tentaqles/` and a shell call that runs
  `echo x >> .tentaqles/lessons/a.md` each set the mark; `cat` of the same file, an edit of `src/a.go` and
  `tentaqles lobe lessons gate` do not; the marked session yields 0 candidates and 0 lessons. 5 of 5, then
  yes or no.
- House rules. Isolation: `TestLobeSharedLessonStaysInItsClient` (new): a lesson shared in a repository of
  `acme` is in 0 listings and 0 hook outputs of `globex`, and the same repository cloned under both clients
  gives two `intake.json` files that never name each other. Never stalls: `TestLobeHookAlwaysExitsZero` and
  `TestLobeCaptureSurvivesALateStep` stay green with the tamper step in the post-tool events. No secret on
  disk: step 4 runs the scrubber, held by `TestLobeShareMovesOneFile` with a planted token that refuses the
  share. Guard untouched: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, and every test under
  "Must not trade off" in `CLAUDE.md`.
- Widened: `TestLobeCommandTable`, `TestLobePersonVerbsRefuseInAgent` (two more verbs), `TestLobeDoctorCodes`
  (one more code), `TestDocsReferenceEveryCommand`.

## Evaluation

The tester gets the built `tentaqles`, this brief and an empty working folder. Use bash (Git Bash on Windows)
and no network. Run the standard tester setup first:

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
A="$W/clients/acme"; R="$A/app"; S="$R/.tentaqles/lessons"; L="$TQ_HOME/identities/acme/lobe"
H="$TQ_HOME/identities/acme/claude"; cd "$R"
fail() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"%s"},"error":"%s"}' "$1" "$2" "$3" "$4" |
  CLAUDE_CONFIG_DIR="$H" tentaqles lobe-hook claude post-tool-use-failure; }
ok() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"%s"},"tool_response":{"stdout":"ok","stderr":""}}' "$1" "$2" "$3" |
  CLAUDE_CONFIG_DIR="$H" tentaqles lobe-hook claude post-tool-use; }
pre() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"%s"}}' "$1" "$2" "$3" |
  CLAUDE_CONFIG_DIR="$H" tentaqles lobe-hook claude pre-tool-use; }
file() { tentaqles lobe consolidate --session "$1" --draft template --json | grep '"action":"file"' |
  grep -o 'l-[0-9a-f]\{8,12\}' | head -1; }
```

Stop with FAIL if a line starts with `SETUP FAIL`. A tester never runs a person-only command to succeed.

1. Private by default. `fail s1 "$R" "npx vitest run" "Error: Worker exited unexpectedly"`, then
   `ok s1 "$R" "npx vitest run --pool=forks"`, then `P1=$(file s1); echo "$P1"` prints an id. `test -f
   "$L/lessons/$P1.md"` is true and `test -e "$R/.tentaqles"` is false.
2. Shared by setting. `lobeset acme share shared`, then `fail s2 "$R" "pip install foo" "error:
   externally-managed-environment"`, `ok s2 "$R" "pip install --user foo"` and `ID=$(file s2); echo "$ID"`
   prints an id. `test -f "$S/$ID.md"` is true and `test -e "$L/lessons/$ID.md"` is false.
3. The header. `grep -c 'scope: shared' "$S/$ID.md"` prints `1`, `grep -c 'by: team' "$S/$ID.md"` prints `1`,
   and `grep -c -E 'session:|events:|repo:' "$S/$ID.md"` prints `0`.
4. No git command ran. `git status --porcelain` prints `?? .tentaqles/` and `git rev-list --all --count`
   prints `0`.
5. The record. `grep -c '"by":"me"' "$L/intake.json"` prints `1`; the file holds `"root":"app"` and
   `"verdict":"ok"`; and `grep -c "$(sha256sum "$S/$ID.md" | cut -c1-64)" "$L/intake.json"` prints `1`.
6. The listing. `tentaqles lobe lessons --json` shows `$ID` with `"scope":"shared"`, `"state":"ok"` and
   `"stage":"advice"`, and `$P1` with `"scope":"private"`.
7. It speaks here, uncommitted. `pre s3 "$R" "pip install bar"` exits 0 and its stdout contains `--user` and
   `not instructions` and no `permissionDecision`.
8. It is held to its repository. `git init -q "$A/app2"`, then `pre s4 "$A/app2" "pip install bar"` prints
   nothing: the lesson came from events in `app`, so it has `applies: repo`.
9. No repository, no sharing. From `$A`: `fail s5 "$A" "cargo build" "no Cargo.toml"`, `ok s5 "$A" "cargo
   build --manifest-path app/Cargo.toml"`, `P2=$(cd "$A" && file s5)`. `test -f "$L/lessons/$P2.md"` is true
   and `ls "$S" | wc -l` prints `1`.
10. An agent cannot share. `CLAUDECODE=1 tentaqles lobe lessons share "$P1" --yes; echo "exit=$?"` prints
    `exit=1`, `ls "$S" | wc -l` still prints `1`, and `test -f "$L/lessons/$P1.md"` is true. The same with
    `unshare "$ID"` prints `exit=1` and `test -f "$S/$ID.md"` is true.
11. The setting moves nothing. `lobeset acme share private`, then `test -f "$S/$ID.md"` is true and check 6
    gives the same two rows.
12. A write into the repository's `.tentaqles` marks the session. `ok t1 "$R" "echo x >> $S/$ID.md"`, then
    `grep -c '"tampered":true' "$L/sessions/t1.json"` prints `1`. `fail t1 "$R" "go build ./..." "no go.mod"`,
    `ok t1 "$R" "go build -C app ./..."`, then `file t1` prints nothing. `ok t2 "$R" "cat $S/$ID.md"`, then
    `grep -c '"tampered":true' "$L/sessions/t2.json"` prints `0` or reports that the file does not exist.
    `tentaqles lobe status --json | grep -c '"tampered_sessions":1'` prints `1`.
13. A private file in the repository is reported. `sed 's/scope: shared/scope: private/' "$S/$ID.md" >
    "$S/l-bbbb0001.md"`, then `tentaqles doctor --json | grep -c '"code":"lobe-private-in-repo"'` prints `1`.
    `rm "$S/l-bbbb0001.md"`, and the same command prints `0`.
14. Nothing reaches the other client. `(cd "$W/clients/globex/app" && tentaqles lobe lessons --json | grep -c
    "$ID")` prints `0`, and `test -e "$TQ_HOME/identities/globex/lobe/intake.json"` is false.

**Person checks**, for the owner, in a terminal of their own with the tester's `HOME`, `USERPROFILE` and
`TQ_HOME` exported, in `$W/clients/acme/app`, after the checks above (`share` is `private` again):

1. `printf '## Do\nGive the MERGE source an alias.\n## Avoid\nA subquery as the source.\n## Why\nThe engine
   rejects it because the source has no alias.\n' > "$W/b.md"`, then `ID=$(tentaqles lobe lessons add --title
   "Alias the MERGE source" --when command=dbsql < "$W/b.md")`. `test -f
   "$TQ_HOME/identities/acme/lobe/lessons/$ID.md"` is true.
2. `tentaqles lobe lessons share "$ID"` prints the file and waits; type `yes`. Exit 0;
   `grep -c 'scope: shared' .tentaqles/lessons/$ID.md` prints `1`; `grep -c -E 'session:|repo:'
   .tentaqles/lessons/$ID.md` prints `0`; the private file is gone; `git rev-list --all --count` prints `0`.
3. `cp .tentaqles/lessons/$ID.md "$W/before.md"`, `tentaqles lobe rules retire "$ID"`, then `tentaqles lobe
   lessons --json` shows the id with `"stage":"retired"` and `cmp "$W/before.md" .tentaqles/lessons/$ID.md` is
   silent.
4. `tentaqles lobe lessons unshare "$ID" --yes`: the private file is back with `scope: private`, and
   `.tentaqles/lessons` no longer holds it.
5. `printf '## Do\nUse two workers.\n## Avoid\nMore than two.\n## Why\nIt is better.\n' > "$W/w.md"`, then
   `X=$(tentaqles lobe lessons add --title "Use two workers on this machine" --anyway < "$W/w.md")` and
   `tentaqles lobe lessons share "$X"; echo "exit=$?"`. Expect a line that names `--anyway`, `exit=1`, and no
   file `.tentaqles/lessons/$X.md`. With `tentaqles lobe set share shared --yes` first, a second `--anyway`
   lesson with another title is still filed in the private store.

Scoring: PASS needs 14 of 14 tester checks. "Could not run" is a FAIL. The person checks are reported apart.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin suite is not affected.
- [ ] The tester brief passes, 14 of 14.
- [ ] The house rules hold: `TestLobeSharedLessonStaysInItsClient`, `TestLobeHookAlwaysExitsZero`,
  `TestLobeCaptureSurvivesALateStep`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, and the
  tests under "Must not trade off" in `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md`, `docs/reference/manifest.md` and `CHANGELOG.md` are updated.
- [ ] The owner has shared one lesson, seen it in `git status`, and moved it back.

## Risks and unknowns

- **How a repository key leads back to a folder.** F-01 step 5 says `files.json` and `intake.json` keep
  `root`; before this feature neither exists, and the graph's `files.json` arrives in 043. Step 2 above uses
  the folder the pass runs in and `intake.json`. A lesson of a session in a repository the pass cannot find
  stays private even with `share: shared`. If 032 built its own lookup for R-06 step 2, use that one.
- **The SHA-256 check before a print** (step 9) is this file's reading of F-04 step 8, which ties a verdict
  to a file's bytes. The requirements do not say what a hook does with a shared file that changed since it
  was recorded. Without the check, text pulled a minute ago would reach the agent before 034's intake saw it.
- **Line ends on Windows.** Git for Windows often rewrites LF as CRLF on checkout. Taking the SHA-256 with
  CRLF read as LF (step 5) is this file's choice; the requirements do not mention line ends. Without it a
  shared lesson would lose its record at the first branch switch.
- **`grounded` with no evidence.** A lesson's events expire after `retention_days`, so `share` usually has
  none. The whole gate must still run. Task 1 confirms what 016 decided for this case.
- **`unshare` and the strength numbers.** F-04 says only "does the reverse". Writing this machine's numbers
  back into the header is this file's choice.
- **The tamper mark is split.** F-05's brief runs its tamper checks at the end of phase 1, and 018, 023 and
  030 already read the mark, but the index gives the mark to 034. This feature writes it for one place. If an
  earlier feature already built the writer, task 3 only adds the place.
- **An agent in a Codex session** is not known by `CLAUDECODE` (F-07, risks). `share --yes` is stopped only
  for Claude Code until feature 036 settles it.
- A shared lesson is as trusted as any file committed to that repository, no more (F-04, risks). The agent
  can copy a private lesson into a repository by hand; the session is then marked, and the file is not
  recalled, because `intake.json` does not list it.
- What would change this file: a different shape for `intake.json` in 034, or a decision by the owner that
  `share: shared` needs a typed yes per lesson.
