# 032: Stale lessons

**Status** not started · **Stage** Lessons · **Entries** R-06 (files only; without the symbol check and without
the deny path), R-04 (step 10, the order), R-05 (step 3, the order), F-07 (`lessons --stale`, the stale count
of `status`) · **Needs** 031 · **Feeds** 033, 034, 038, 050, 055

## Result

A lesson can name the files it is about in `touches`. When one of those files is gone, the lesson is still
shown, with the mark "may be stale" and after the fresh lessons, so the agent checks the code first. Nothing is
removed and the lesson file is never changed. The person sees which lessons are stale and why.

```sh
tentaqles lobe lessons --stale      # Lesson U ... etl/load.py is gone
tentaqles lobe status               # counts the stale lessons
```

Before a matching call the agent reads: `- Lesson U. Do: ... (may be stale: etl/load.py is gone; check the code
first)`.

## Scope

**In**

- `recall.Gone` in `internal/lobe/recall/stale.go`: an entry of `touches` is gone when nothing exists at its
  path (R-06 steps 2 to 4, the file half), with the path safety rules.
- The mark on a printed lesson line (step 5) and the stale lesson ranked after every fresh one before a call,
  after a failure and in the brief (step 6).
- The reason "repository not found" for a private lesson whose `repo` matches no repository of the client.
- The stale flag per lesson, cached in `index/` by the detached pass (steps 1 and 9).
- `tentaqles lobe lessons --stale` and the stale count of `tentaqles lobe status` (step 8).

**Out**

- The symbol part of an entry, `file#Symbol`: the text search of step 4 (2 MiB, whole word) and the graph
  lookup, with `TestLobeStaleSymbolGone`, `TestLobeStaleWholeWord` and `TestLobeStaleLargeFileNotJudged`
  (feature 055). Here an entry with a symbol is judged by its file alone.
- "A stale rule never denies": E-01's deny path reading the cached flag, with `TestLobeStaleNeverDenies`
  (feature 038). This feature writes the flag; nothing reads it for a deny yet.
- A shared lesson's `touches`, judged against the repository its file sits in (features 033 and 034).
- The stale order in the prompt block, R-02 step 5 (feature 050).
- Following a renamed file (not planned, R-06 Risks).

## What it builds on

- 003: `store.RepoKey(clientDir, repoDir)` and the walk to the nearest `.git` at or above `cwd`.
- 005: `tentaqles lobe status` and its `--json` object with `lessons`.
- 016: the header keys `touches` and `repo`; the gate's `form` rule that a private lesson with `touches`
  carries `repo`; `tentaqles lobe lessons`; and `lessons add --touches`, which writes the repository's key as
  `repo` and is refused outside a repository (R-06 asks for both; 016 built them).
- 017: `recall/trigger.go`, `format.go` (the lesson line, cut at 300 characters), `pretool.go` with the order
  of R-04 step 10 minus its first key (every lesson counts as fresh there), and the header cache in `index/`
  (file name proposed there: `index/lessons.json`).
- 018: the detached pass and `tentaqles lobe consolidate`.
- 020: `postfail.go`, the same order after a failure.
- 026: `brief.go`, the order of standing lessons.
- 031: `lesson.Live`; only live lessons are judged.

## What it leaves for later features

- `recall.Gone(l Lesson, root string, g GraphLookup, deadline time.Time) []string`, with `g` nil here. 055
  passes a graph lookup and adds the symbol half.
- The stale flag and the "repository not found" verdict beside each lesson's header in `index/`. 038 reads the
  flag before a deny; a `block` rule with a `touches` entry and no flag counts as stale there.
- Staleness as the first key of every lesson order. 050 uses it in the prompt block.
- `lessons --stale` and `"stale":<n>` in `tentaqles lobe status --json`.
- A function that lists the client's repositories for a command and for the pass (step 7 below). 056 replaces
  it with G-10's `DiscoverRepos`.

## How it works

1. **When the check runs** (step 1). Each time a lesson is about to be printed by a hook (before a call, after
   a failure, in the brief) and each time it is listed (`tentaqles lobe lessons`, `tentaqles lobe status`). It
   also runs in the detached pass for every live lesson with a `touches` entry, with no time limit, and the
   pass writes the verdict as a flag beside the lesson's header in `index/`. The pass does this after E-07's
   moves, in every run, also with `--only` or `--session` (this file's choice; core page 5.2 does not place
   it). The verdict is never written into the lesson file.
2. **Which repository** (step 2). A private lesson is judged in the repository its `repo` key names. In a hook
   that is `cwd`'s repository when the keys are equal; a lesson of another repository is not judged there. A
   lesson with `touches` and no `repo`, written by hand, is not judged.
3. **One entry** (steps 3 and 4). Split the entry at `#` into path and symbol. Leave it unjudged when the path
   is absolute, starts with a drive letter or a backslash, leaves the repository through `..`, or resolves
   through a symbolic link to outside it: the nearest existing ancestor, after `filepath.EvalSymlinks`, must be
   under the root. The entry is gone when nothing exists at the path. A path that exists, as a file or a
   folder, is fresh, whatever its symbol part says. Paths are joined to the root; no file is opened or read.
4. **The mark** (step 5). One gone entry marks the lesson. Its printed line gains ` (may be stale: <entry> is
   gone; check the code first)`, naming at most 2 entries as written, each cut at 120 characters with control
   characters removed, then `and <N> more`. The note is inside the 300 characters of the line: the Do, Avoid
   and Why parts are cut first. In the brief the note follows the lesson's line in the same way.
5. **The order** (step 6). A stale lesson stays eligible and is ranked after every fresh one. Staleness comes
   before every other key: in R-04 step 10 (before a call and after a failure) and in R-05 step 3 (the brief).
   The check runs for the lessons that matched, before the cut to 2 or 5.
6. **In a hook the check stops after 50 ms** (proposed). Entries not reached are not judged and the lesson
   counts as fresh. A hook never writes the flag and never looks for repositories.
7. **"Repository not found"** (step 2). A private lesson whose `repo` matches no repository of the client is
   stale with that reason, and its entries are not looked at. Its line gains ` (may be stale: repository not
   found; check the code first)`. Only the listing commands and the pass reach this verdict; the pass caches
   it with the flag, and a hook prints it from the cache. How they know the client's repositories is this
   file's choice, because no graph or intake record exists yet: they look for folders that hold `.git` under
   the client folder, to depth 3 (G-10's figure, proposed), without following links or junctions and without
   entering a repository already found, and compare `store.RepoKey` of each with the lesson's `repo`.
8. **`tentaqles lobe lessons --stale`** lists the stale lessons with their gone entries or with `repository not
   found`; anyone may run it. `tentaqles lobe status` counts them, and `--json` has `"stale":<n>` under
   `lessons`. Both compute the verdict when they run and do not read the flag.
9. **What does not happen.** No ladder move is made for staleness (E-07 step 4). The lesson file keeps its
    bytes. A stale lesson is never removed without the person, who replaces it (031) or retires it with
    `tentaqles lobe rules retire <id>`.

## Where to read

| What | Where |
|---|---|
| Requirements | [R-06](../prd/lobe-recall.md), all steps, read without the symbol half of step 4 and without step 7's deny; [R-04](../prd/lobe-recall.md) steps 10 and 11; [R-05](../prd/lobe-recall.md) steps 3 and 5; [F-01](../prd/lobe-foundation.md) step 5 (the repository key) and step 10 (caches); [E-07](../prd/lobe-enforce.md) step 4; [G-10](../prd/lobe-graph.md) step 4, for the depth of the search only; [core page 5.5](../prd/vertical-lobe.md), the note on a stale lesson |
| Source to port | [`src/notes/stale.ts` L83](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L83): a path is gone when nothing exists there; [L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L42): paths that would leave the checkout are never looked at; [`src/notes/stale.ts` L100](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L100): the verdict is set in memory only; [`src/claude/format.ts` L205](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/format.ts#L205): judged only for learnings about to be shown; [`src/notes/notes.ts` L442](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/notes.ts#L442): at most two entries are named, each cut at 120 characters |
| Source, for feature 055 | The symbol lines of `src/notes/stale.ts` (L86, L70, L64, L27) are not used here |
| Existing tentaqles code | `internal/lobe/recall` (`pretool.go`, `postfail.go`, `brief.go`, `format.go`); `internal/lobe/store/repokey.go`; `internal/lobe/cli` (the `lessons` list, `status`); `internal/cmd/lobe_lessons.go` (`add --touches`); `internal/lobe/pass` (the ordered steps) |

A ported Go file starts with `// Ported from Trail-code src/notes/stale.ts at <commit> (MIT).`, and `NOTICE`
lists it (F-07).

## Build tasks

1. **Check this brief against the code.** Read how 017 ordered matched lessons and whether it left a place for
   staleness, how the header cache stores a lesson (it needs one flag and one reason per lesson), and that
   `lessons add --touches` writes `repo` and refuses outside a repository, as 016 built it. List what no
   longer fits. The owner approves changes first.
2. **`Gone`.** `recall/stale.go`, `stale_test.go`: the split, path safety, existence, the 50 ms stop. Proof:
   `TestLobeStaleFileGone`, `TestLobeStaleStaysInsideRepo`, `TestLobeStaleLeavesFileBytes`,
   `TestLobeStaleSymbolPartNotJudged`.
3. **The mark and the order.** `format.go`, `pretool.go`, `postfail.go`, `brief.go`. Proof:
   `TestLobeStaleRankedLast`, `TestLobeBriefStaleLast`, `TestLobePreToolBudget` still green.
4. **Repositories and "repository not found"** (side by side with 3). The bounded search; the verdict in the
   listing commands. Proof: `TestLobeStaleRepoNotFound`.
5. **The cached flag.** The pass step; the cache keys; the hook reading the cached reason. Files:
   `internal/lobe/pass`, the cache writer. Proof: `TestLobeStaleFlagCached`, `TestLobeRecallOutdatedCache`
   still green (a hook writes 0 bytes under `index/`).
6. **`lessons --stale` and `status`** (side by side with 5). Files: `internal/lobe/cli`. Proof:
   `TestLobeStaleListAndCount`.
7. **Speed.** Add a store whose 500 lessons all carry `touches` to `tentaqles lobe bench latency` for
   `pre-tool-use` and `session-start`. Proof: the bench stays inside the limits it prints.
8. **Docs.** `docs/reference/commands.md`: `--stale` under `tentaqles lobe lessons`, the stale count under
   `tentaqles lobe status`. `CHANGELOG.md`; `NOTICE` if a file is ported. The plugin does not change.
9. **Tester run**, then the person checks.

## Tests that stay in the repository

- `TestLobeStaleFileGone`: a lesson whose `touches` names a missing file returns that entry; an existing file
  returns none.
- `TestLobeStaleStaysInsideRepo`: `../x`, an absolute path and a link pointing outside open 0 files outside the
  root and return no entry.
- `TestLobeStaleRepoNotFound`: a private lesson whose `repo` names no repository of the client is listed by
  `tentaqles lobe lessons --stale` with `repository not found`; one whose `repo` names a second, existing
  repository is not.
- `TestLobeStaleLeavesFileBytes`: the lesson file is byte-identical before and after 100 checks.
- `TestLobeStaleRankedLast`: a stale lesson with the higher `strength` prints after a fresh one in the block
  before a tool and in the brief: 2 of 2. Feature 050 adds the third case, the prompt block.
- `TestLobeBriefStaleLast`: a stale standing lesson prints after every fresh one and carries the note.
- `TestLobeStaleFlagCached`: after a detached pass, a lesson with a gone `touches` entry has its stale flag set
  in `index/` and a fresh one has it clear; the lesson files and `ladder.json` are byte-identical.
- `TestLobeStaleSymbolPartNotJudged` (new): with `etl/load.py` present, the entry `etl/load.py#upsert` is fresh
  whatever the file holds; with the file gone, the entry is returned as written. Feature 055 replaces it.
- `TestLobeStaleListAndCount` (new): `lessons --stale` names each gone entry; `status --json` has `"stale":1`.
- `TestLobeStaleNoteInsideLineBudget` (new): a lesson line with the note has at most 300 characters, and two
  long entries are each cut at 120.
- House rules. Never stalls: `TestLobeStaleStopsAtFiftyMs` (new) gives a hook with 10 lessons on a slow file
  system its answer inside the cut-off, and `TestLobeHookAlwaysExitsZero` stays green. Isolation: path safety
  is an isolation rule, and `TestLobeStaleStaysInsideRepo` holds it; the repository search never leaves the
  client folder (`TestLobeStaleRepoSearchStaysInClient`, new). A hook never walks a repository or the client
  folder. `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`.

## Evaluation

The tester gets the built `tentaqles` first on `PATH` and an empty working folder. Use bash (Git Bash on
Windows). No network. Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"; C=acme
cd "$R" && mkdir -p etl "$L/lessons" && printf 'def upsert():\n    pass\n' > etl/load.py
K="r_$(printf app | sha256sum | cut -c1-8)"
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Lesson U" "when: {command: [databricks]}, touches: [etl/load.py], repo: $K" \
  "Use an alias." "The bare form." "The engine rejects it because it needs an alias."
put l-aaaa0002 "Name the databricks profile" "when: {command: [databricks]}" "Pass a profile." \
  "The default one." "The job fails because the default is unset." 0.5
sha256sum "$L"/lessons/*.md > "$W/before"
hook() { printf '{"session_id":"%s","cwd":"%s","tool_name":"Bash","tool_input":{"command":"%s"}}' \
  "$1" "$R" "${2:-databricks jobs list}" |
  CLAUDE_CONFIG_DIR="$I/$C/claude" tentaqles lobe-hook claude pre-tool-use; }
```

No line may start with `SETUP FAIL`, `K` must be `r_a172cedc`, and each `put` must print `ok`; if not, stop and
report FAIL. The lesson files are written by hand because `tentaqles lobe lessons add` answers only to a person.
`Lesson U` has `strength` 1.0 and the second lesson 0.5; both enter at `advice`.

1. `tentaqles lobe lessons --stale`: exit 0; the output does not contain `Lesson U`.
2. `hook s0`: the `additionalContext` contains `Lesson U` and, after it, `Name the databricks profile`; it does
   not contain `may be stale`.
3. `rm etl/load.py`, then `tentaqles lobe lessons --stale`: the output contains `Lesson U` and `etl/load.py`,
   and does not contain `Name the databricks profile`. `tentaqles lobe status --json | grep -c '"stale":1'`
   prints `1`.
4. `hook s1`: the `additionalContext` contains `may be stale: etl/load.py is gone`, and `Name the databricks
   profile` now comes before `Lesson U`: the stale lesson is ranked last.
5. Only the file counts in this feature. `printf 'def other():\n    pass\n' > etl/load.py`, then `tentaqles
   lobe lessons --stale`: no `Lesson U`.
6. `sha256sum "$L"/lessons/*.md | diff - "$W/before"`: no output, exit 0.
7. A path that leaves the repository is never looked at. Nothing exists at either path below:
   ```bash
   put l-aaaa0004 "Lesson X" "when: {command: [dbt]}, touches: [etl/gone.py, ../nope.txt], repo: $K" \
     "Run dbt deps first." "A bare run." "The run fails because packages are missing."
   ```
   The `put` may print a `form:` line for the `..` entry instead of `ok`; the file is copied either way.
   `tentaqles lobe lessons --stale` lists `Lesson X` with `etl/gone.py` and without `nope.txt`: the entry
   that points above the repository was left unjudged.
8. Place a copy of `Lesson U` whose `repo` names no repository and that answers to another command:
   ```bash
   sed "s/$K/r_00000000/; s/l-aaaa0001/l-aaaa0003/; s/Lesson U/Lesson V/; s/databricks/dbsql/" \
     "$L/lessons/l-aaaa0001.md" > "$L/lessons/l-aaaa0003.md"
   ```
   `tentaqles lobe lessons --stale`: the output contains `Lesson V` and `repository not found`, and does not
   contain `Lesson U`.
9. The pass caches the verdict for the hooks. `tentaqles lobe consolidate --only fade; echo "exit=$?"` prints
   `exit=0`. Then `hook s2 "dbsql -f q.sql"`: the `additionalContext` contains `Lesson V` and `may be stale:
   repository not found`.
10. Another client's login sees nothing. Set `C=globex` and run `hook s3`: empty stdout. Set `C=acme` again.

**Person checks** (for the owner, at a terminal, with the tester's `HOME`, `USERPROFILE` and `TQ_HOME` exported,
in the same shell after the checks above). They are R-06's own person checks. The flag was built in 016; they
are run again here because this is the first feature that reads what it writes.

1. Run the two lines below in `$R`. They print an id; `grep -c "repo: $K" "$L/lessons/<id>.md"` prints `1`.
   ```sh
   printf '## Do\nRead the loader first.\n## Avoid\nGuessing its columns.\n## Why\nIt fails because they change.\n' |
     tentaqles lobe lessons add --title "Check the loader columns" --when command=psql --touches 'etl/load.py'
   ```
2. `cd "$W/clients/acme"` (the client folder, outside the `app` repository) and run the same two lines with the
   title `Check the loader twice`: exit 1 with a line that names `--touches`, and no new file under
   `$L/lessons/`.

PASS needs 10 of 10. "Could not run" is a FAIL. The person checks are reported apart.

## Done when

- [ ] Every test above is in the repository and green in CI: `go vet ./...`, `go test ./... -count=1`,
  `go build ./...`.
- [ ] The tester brief passes 10 of 10.
- [ ] The house rules hold: `TestLobeStaleStaysInsideRepo`, `TestLobeStaleStopsAtFiftyMs`,
  `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`, `TestLobeRecallOutdatedCache`,
  `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`, and every test under "Must not trade off" in
  `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md` and, for a ported file, `NOTICE` are updated.
- [ ] The owner has seen a lesson marked "may be stale" before a call and in `tentaqles lobe lessons --stale`.

## Risks and unknowns

- **How the client's repositories are known** is this file's choice (step 7). R-06 says the listing commands
  and the pass "know the client's repositories (F-01)", but before the graph (`files.json`) and intake
  (`intake.json`) no file records them. G-10 says no `stop` runs its search, for the graph build; here the pass
  does run a bounded search, for the stale flag only. If the owner prefers no search in the pass, "repository
  not found" reaches a hook only after a person ran a listing command, or waits for feature 043.
- A repository nested deeper than 3 folders under the client folder is not found, and its private lessons are
  shown as "repository not found". The depth is proposed.
- With the symbol half moved to feature 055, a lesson about `file#Symbol` whose symbol was removed is shown as
  fresh until then. The requirements planned a text check for phase 2; the feature list defers it.
- A renamed file marks its lessons stale although they still hold. A branch switch can do the same for a
  while; no ladder move is made, so it costs the person nothing.
- The cached verdict is as old as the last detached pass. A lesson whose repository came back still prints
  "repository not found" in a hook until the next pass.
- The repository key comes from the repository's path, so a repository moved inside the client folder leaves
  its private lessons with the old key until they are filed again.
- What forces a change to this file: a header cache that cannot hold a flag and a reason per lesson; an order
  function in 017 that every caller copied instead of sharing; a gate that refuses a `touches` entry with `..`
  so that check 7 cannot place its fixture (the copy by hand still works).
