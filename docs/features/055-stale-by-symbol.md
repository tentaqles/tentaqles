# 055: Stale lessons by symbol

**Status** not started · **Stage** Graph · **Entries** R-06 (the `file#Symbol` half: step 4 "from phase 4",
and `GraphLookup`) · **Needs** 048, 032 · **Feeds** none

## Result

A lesson can name the code it is about as `file#Symbol` in `touches`. Since [032](032-stale-lessons.md) such a
lesson is marked "may be stale" when the file's text no longer holds the symbol's name as a whole word. That
check misses a symbol that was removed while its name survives in a comment or a string. From this feature
on, when the repository's graph is current for that file, the graph decides: the lesson is stale when the
file has no definition of that name. Everything else about a stale lesson stays as 032 built it: it is still
shown, with the mark, after the fresh lessons; it never denies; nothing is written into the lesson file.

From the sandbox of [002](002-sandbox.md), with the fixture of the Evaluation section, after `etl/load.py`
loses `def upsert():` and keeps the comment `# upsert was replaced by load_all`:

```bash
tentaqles lobe lessons --stale      # no "Lesson U": the graph has not read the file, the text check answers
tentaqles lobe graph build
tentaqles lobe lessons --stale      # "Lesson U" with "etl/load.py#upsert"
```

and the note before a matching call then carries `(may be stale: etl/load.py#upsert is gone; check the code
first)`.

## Scope

**In**

- The graph lookup behind `recall.Gone(l Lesson, root string, g GraphLookup, deadline time.Time) []string`,
  which 032 calls with `g` nil: an adapter over the search index's lookup by file and name (R-06 step 4,
  R-01 "How to build it").
- The rule that says when the graph is current for a file and may answer, and when the text check of 032
  stays (decided in this brief; R-06 says only "with a current graph (G-08)").
- The callers that now pass the lookup: the note before a call and after a failure (R-04) and the prompt
  block (R-02) on both hosts, `tentaqles lobe lessons --stale`, `tentaqles lobe status`, and the step of the
  detached pass that caches the stale flag in `index/`.

**Out**

- The file half of R-06, path safety, "repository not found", the wording of the mark, the order after fresh
  lessons, the cached flag and the listing commands themselves ([032](032-stale-lessons.md)).
- The rule that a stale block rule does not deny ([038](038-approve-to-block.md) reads the cached flag). This
  feature changes how the flag is computed, not who reads it.
- The session brief. R-05 step 9 says the brief never loads the graph, so `session-start` keeps calling `Gone`
  with a nil lookup (decided in this brief).
- Entries in files that have no query file: SQL, Markdown, YAML, JSON, Terraform. Their node names are not
  symbol names (`a.users`, a key path), so the text check keeps judging them (decided in this brief).
- Matching the owner part of an entry such as `etl/load.py#Loader.upsert`. R-06 judges the last dot-part
  only, for the text check and for the graph. Reported as a question.
- Following a renamed file or symbol (R-06 risk: not planned).

## What it builds on

- [032](032-stale-lessons.md): `internal/lobe/recall/stale.go` with `Gone`, the split of an entry at `#`, the
  path safety rule, the text check (a file of at most 2,097,152 bytes, the last dot-part as a whole word),
  the 50 ms stop in a hook (proposed), the mark, the stale flag per lesson that the detached pass writes into
  `index/`, `tentaqles lobe lessons --stale` and the stale count of `tentaqles lobe status`. If 032 declared
  `Gone` without the `g` parameter, this feature adds it and passes nil where 032 called it.
- [017](017-lesson-before-call.md), [020](020-lesson-after-failure.md): the R-04 paths that call `Gone` for
  the lessons that matched, before the cut to 2.
- [036](036-lessons-in-codex.md): the same paths under `tentaqles lobe-hook codex`.
- [038](038-approve-to-block.md): `TestLobeStaleNeverDenies`, which stays green.
- [043](043-graph-of-one-repository.md): node ids and kinds, and the registry rows that say which languages
  have a query file.
- [046](046-find-and-map.md): the search index under the graph folder with its lookup by file and name.
- [048](048-graph-stays-current.md): `GraphDir` (also for a linked worktree), `files.json` with a record
  `[size, mtime_ns, sha256, read_at_ns]` per path, the build id that ties the index to the graph, the dirty
  markers.
- [050](050-prompt-injection.md): the prompt block, which calls `Gone` for its lessons.

## What it leaves for later features

- `GraphLookup` with two questions, "is the graph current for this file" and "does this file define this
  name" (method names proposed here: `Current(path)` and `Defines(path, name)`), and its adapter beside the
  index. [056](056-many-repositories.md) can hand the pass a lookup per repository of the client.
- Nothing else. No file in the store changes shape.

## How it works

1. **What changes** (R-06 step 4). For a `touches` entry with a symbol whose file exists: when the graph is
   current for that file (step 2), the entry is gone when the graph has no definition of that name in that
   file. Otherwise the text check of 032 answers, exactly as before. An entry without a symbol, a file that
   is gone, a folder, an unreadable file and a file over 2 MiB are judged as in 032.
2. **Current for a file**, decided in this brief. All of these hold:
   - the client's `graph` setting is on, and the lesson's repository has a graph folder (`GraphDir`);
   - the index's build id equals the graph's (G-08: a sidecar with another build id is ignored);
   - the file's language has a query file, that is a registry row (G-02);
   - no dirty marker names the file (G-08 step 3);
   - the file's size and modification time equal its record in `files.json` (one `stat`, no hash).
   When one fails, the graph says nothing about that file and the text check answers.
3. **The name** (R-06 step 4, as in 032). The symbol part of the entry, without a `~N` suffix, cut to its
   last dot-part: `Loader.upsert` and `upsert~2` are both asked as `upsert`. The file defines the name when
   the index's lookup by file and name returns a node whose kind is a definition kind of G-02 (function,
   method, class, interface, type, struct, enum, module, constant, variable). A file node, a note and a node
   of a kind that 052 or 053 added do not count.
4. **In a hook** (R-06 steps 4 and 9). The lookup is served by the index of `cwd`'s repository;
   `graph.jsonl` is never loaded. The index is opened only when a lesson that matched has an entry with a
   symbol. The 50 ms stop of 032 covers it: an entry not reached in time is not judged and the lesson counts
   as fresh. A lesson of another repository than `cwd`'s is not judged in a hook, as in 032.
5. **In a command.** `tentaqles lobe lessons --stale` and `tentaqles lobe status` build one lookup per known
   repository of the client that has a graph, and have no 50 ms stop. They never build a graph and never
   wait for one.
6. **In the detached pass.** The step that writes the stale flag calls `Gone` with the lookup, with no stop.
   It stays where 032 put it in the pass. The graph build comes later in the same pass (G-08 step 4), so for
   a file edited in this turn a marker exists, the text check answers, and the next pass sets the flag from
   the fresh graph.
7. **Hosts.** The same on both: `pre-tool-use`, the answer after a failure and `user-prompt-submit` of
   `tentaqles lobe-hook claude` and of `tentaqles lobe-hook codex`.
8. **What never happens.** The lesson file is not written. No ladder move is made for staleness (E-07). A
   hook never writes the flag. The lookup opens files only under this client's graph folder for the lesson's
   repository; R-06 step 3 runs first, so an entry that leaves the repository is never looked up.

## Where to read

| What | Where |
|---|---|
| Requirements | [R-06](../prd/lobe-recall.md) steps 1, 2, 4, 7 and 9, "How to build it" and the first risk; [R-01](../prd/lobe-recall.md) "How to build it" (the index carries the lookup by file and name; a hook never loads `graph.jsonl`); [R-05](../prd/lobe-recall.md) step 9; [G-08](../prd/lobe-graph.md) steps 1, 2, 3 and 7; [G-02](../prd/lobe-graph.md) step 2 (the kinds) and its risk about `~N`; [core page](../prd/vertical-lobe.md) 5.5, the note on a stale lesson |
| Source: the weakness this feature removes | [`src/notes/stale.ts` L12](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L12): the file's text is searched, not the code map, to save a hook about 0.4 s |
| Source: the text check that stays | [`src/notes/stale.ts` L86](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L86), [L70](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L70), [L64](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/stale.ts#L64) |
| Lobe code from earlier features | `internal/lobe/recall/stale.go`, `pretool.go`, `postfail.go`, `prompt.go`, `brief.go`; `internal/lobe/search` (the index); `internal/lobe/graph` (`GraphDir`, `dirty.go`, `files.json`); `internal/lobe/pass`; `internal/lobe/cli` (`lessons`, `status`) |
| Existing tentaqles code | none outside the lobe; `docs/reference/commands.md` for `tentaqles lobe lessons` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the signature of `Gone`,
   where the pass writes the stale flag, how the listing commands find the client's repositories, whether the
   index can answer one lookup without loading whole. The owner approves changes, and the rule of step 2,
   before code is written.
2. **Time one lookup.** A throwaway test opens the index of a 50,000-node graph in a fresh process and asks
   one name in one file, 50 times, on Windows and Linux. The answer must fit well inside the 50 ms stop
   (proposed). If the 95th percentile is above 20 ms, stop and ask the owner: the other course is that hooks
   keep the text check and only the commands and the pass ask the graph. Write the figures into Risks.
3. **The adapter**: `Current` and `Defines` over the index, `files.json` and the markers. Tests:
   `TestLobeStaleGraphNotCurrent`, `TestLobeStaleLookupNameForms`.
4. **`Gone`** asks the lookup for symbol entries and falls back to the text check. Tests:
   `TestLobeStaleSymbolGoneByGraph`, and 032's `TestLobeStaleSymbolGone`, `TestLobeStaleWholeWord`,
   `TestLobeStaleLargeFileNotJudged`, `TestLobeStaleStaysInsideRepo` unchanged with a nil lookup.
5. **The callers** (side by side): the three hook paths on both hosts, the two commands, the pass. Tests:
   `TestLobeStaleHookUsesIndexOnly`, `TestLobeStaleBriefKeepsTextCheck`, `TestLobeStaleFlagCached` with the
   comment case, `TestLobeStaleRankedLast` with a lesson stale by graph, `TestLobeStaleNeverDenies` unchanged.
6. **Docs.** One paragraph in the `tentaqles lobe lessons` section of `docs/reference/commands.md` on how a
   symbol is judged; one line in `CHANGELOG.md` under `[Unreleased]`. The plugin does not change.
7. **Tester run** of the Evaluation section by a fresh agent.

## Tests that stay in the repository

- `TestLobeStaleSymbolGoneByGraph` (new name in this brief): `etl/load.py` once defined `upsert` and now
  holds only a comment with that word. With a nil lookup `Gone` returns no entry; with a current graph it
  returns `etl/load.py#upsert`; with `def upsert():` back and a build, no entry.
- `TestLobeStaleGraphNotCurrent` (new): 5 cases, each with the comment-only file and a graph that would say
  "gone": `graph: off`; a build id that differs; a dirty marker that names the file; a file whose size
  differs from its record; a file of a language with no query file. In 5 of 5 the answer is the text
  check's, no entry.
- `TestLobeStaleLookupNameForms` (new): `a.py#Loader.run`, `a.py#run~2` and `a.py#run` all ask for `run`; a
  note named `run` and the file node do not count as a definition; a method `run` does. 5 of 5.
- `TestLobeStaleHookUsesIndexOnly` (new): a `pre-tool-use` run with a matching lesson that has a symbol entry
  opens 0 files named `graph.jsonl`; with the index made slow, the hook answers by its cutoff, the lesson
  prints without the mark, and stdout is one whole JSON object.
- `TestLobeStaleBriefKeepsTextCheck` (new): `session-start` with a standing lesson that has a symbol entry
  opens 0 files under `graph/`.
- From 032, unchanged and green: `TestLobeStaleFileGone`, `TestLobeStaleSymbolGone`,
  `TestLobeStaleWholeWord`, `TestLobeStaleLargeFileNotJudged`, `TestLobeStaleStaysInsideRepo`,
  `TestLobeStaleRepoNotFound`, `TestLobeStaleLeavesFileBytes`.
- From 032, widened: `TestLobeStaleFlagCached` (after a pass with a current graph the comment-only lesson has
  its flag set; lesson files and `ladder.json` are byte-identical), `TestLobeStaleRankedLast` (a lesson stale
  by graph prints after a fresh one in the prompt block and in the block before a tool).
- From 038, unchanged: `TestLobeStaleNeverDenies`.
- House rules: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeRecallOutdatedCache` (the hook writes 0 bytes under `index/`), `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, and every test under "Must not trade off" in `CLAUDE.md`, unedited.

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
R="$W/clients/acme/app"; I="$TQ_HOME/identities"; L="$I/acme/lobe"
cd "$R" && mkdir -p etl "$L/lessons" && printf 'def upsert():\n    pass\n' > etl/load.py
tentaqles lobe graph build || echo "SETUP FAIL build"
K="r_$(printf app | sha256sum | cut -c1-8)"
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' \
    "$1" "$2"; printf ' %sstrength: %s, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }" "${7:-1.0}"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$W/$1.md"; tentaqles lobe lessons gate "$W/$1.md"; cp "$W/$1.md" "$L/lessons/$1.md"; }
put l-aaaa0001 "Lesson U" "when: {command: [databricks]}, touches: [\"etl/load.py#upsert\"], repo: $K" \
  "Use an alias." "The bare form." "The engine rejects it because it needs an alias."
put l-aaaa0002 "Name the databricks profile" "when: {command: [databricks]}" "Pass a profile." \
  "The default one." "The job fails because the default is unset." 0.5
sha256sum "$L"/lessons/*.md > "$W/before"
call() { printf '{"session_id":"%s","cwd":"%s","tool_name":"Bash","tool_input":{"command":"databricks jobs list"}}' \
  "$1" "$R"; }
hook() { call "$1" | CLAUDE_CONFIG_DIR="$I/acme/claude" tentaqles lobe-hook claude pre-tool-use; echo " exit=$?"; }
gone() { printf '# upsert was replaced by load_all\ndef load_all():\n    pass\n' > etl/load.py; }
back() { printf 'def upsert():\n    pass\n' > etl/load.py; }
```

`put` writes a lesson file by hand, because `tentaqles lobe lessons add` answers only to a person, and runs
the gate on it first: each `put` must print `ok`; if one does not, stop and report FAIL. `K` is the
repository key of `app`, which `Lesson U` carries because it is private and has a `touches` entry; the one
folder under `$L/graph/` must have that name. `Lesson U` has `strength` 1.0 and the second lesson 0.5. `gone`
removes the function and leaves its name in a comment; `back` restores it. Run every check in `$R`. A tester
never runs `tentaqles lobe set`.

1. `tentaqles lobe lessons --stale`: exit 0; the output does not contain `Lesson U`.
2. `hook s0`: `additionalContext` contains `Lesson U` and, after it, `Name the databricks profile`; it does
   not contain `may be stale`.
3. `gone`, then `tentaqles lobe lessons --stale`: the output does not contain `Lesson U`. The graph has not
   read the file again, so the text check answers, and the word is still in the comment.
4. `tentaqles lobe graph build`, then `tentaqles lobe lessons --stale`: the output contains `Lesson U` and
   `etl/load.py#upsert`, and does not contain `Name the databricks profile`. `tentaqles lobe status --json`
   contains `"stale":1`.
5. `hook s1`: `additionalContext` contains `may be stale: etl/load.py#upsert is gone`, and
   `Name the databricks profile` now comes before `Lesson U`.
6. Codex. `call c1 | CODEX_HOME="$I/acme/codex" tentaqles lobe-hook codex pre-tool-use`: exit 0 and the
   output contains `may be stale: etl/load.py#upsert is gone`.
7. `lobeset acme graph off`, then `tentaqles lobe lessons --stale`: no `Lesson U`, because without the graph
   the text check answers. `lobeset acme graph on`, then the same command: `Lesson U` is listed again.
8. A marker. Pipe `{"session_id":"e1","hook_event_name":"PostToolUse","cwd":"<R>","tool_name":"Edit",
   "tool_input":{"file_path":"<R>/etl/load.py","old_string":"load_all","new_string":"load_all"}}` (one line,
   `<R>` replaced) to `CLAUDE_CONFIG_DIR="$I/acme/claude" tentaqles lobe-hook claude post-tool-use`. Then
   `hook s2`: `additionalContext` contains `Lesson U` and no `may be stale`: a marker names the file, so the
   graph is not asked.
9. `back`, then `tentaqles lobe graph build`, then `tentaqles lobe lessons --stale`: no `Lesson U`, and
   `tentaqles lobe graph status --json` contains `"dirty":0`.
10. `printf 'def upsert_all():\n    pass\n' > etl/load.py`, then `tentaqles lobe graph build`, then
    `tentaqles lobe lessons --stale`: the output contains `Lesson U`. `upsert_all` is not `upsert`, for the
    graph as for the text check.
11. `rm etl/load.py`, then `tentaqles lobe lessons --stale`: the output still contains `Lesson U`. `back` and
    `tentaqles lobe graph build`, then the same command: no `Lesson U`.
12. `sha256sum "$L"/lessons/*.md | diff - "$W/before"`: no output, exit 0. No lesson file was written.
13. `find "$L/graph" -name graph.jsonl | wc -l` prints `1`, and
    `ls "$I"/globex/lobe/graph 2>/dev/null | wc -l` prints `0`.

There are no person checks: the person-only steps of R-06 (`tentaqles lobe lessons add --touches`) belong to
032. PASS needs 13 of 13. "Could not run" is a FAIL. The tester reports anything surprising and fixes
nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI (`go vet
  ./...`, `go test ./... -count=1`, `go build ./...` on three systems with `CGO_ENABLED=0`).
- [ ] The tester brief passes, 13 of 13.
- [ ] The house rules hold: `TestLobeStaleLeavesFileBytes`, `TestLobeStaleStaysInsideRepo`,
  `TestLobeStaleNeverDenies`, `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeGuardNeverImportsLobe`, and `tentaqles lobe bench leak` at zero leaks.
- [ ] `tentaqles lobe bench latency` was run again; `pre-tool-use` with 500 lessons stays inside its cutoff,
  and its 95th percentile is written down against R-04's 100 ms (proposed).
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a lesson marked stale after a build, although its symbol's name is still in the
  file.

## Risks and unknowns

- **One lookup inside 50 ms.** Whether the index answers a single lookup without being loaded whole depends
  on the format F-01's spike chose. Task 2 measures it. If it does not fit, hooks keep the text check and the
  graph answers only in the commands and the pass; that changes step 4 and check 5 of this file.
- **"Current" is decided here**, not in the requirements (step 2). It is cautious on purpose: in doubt the
  text check answers, which is what a person had before this feature. A same-length edit that restores the
  old modification time escapes the `stat`, as it escapes the probe of G-08; the next build repairs it.
- **The flag is as old as the last pass** (R-06 risk), and inside a pass the flag step runs before the build.
  A symbol removed during a turn is therefore flagged one pass later.
- **The last dot-part only.** `etl/load.py#Loader.upsert` stays fresh while any definition named `upsert`
  remains in the file. Matching the owner too would be stricter and could mark a lesson stale that a person
  wrote without the owner part. Question for the owner.
- **A `~N` id shifts** when an earlier same-named definition is deleted (G-02 risk). Because the suffix is
  dropped before the lookup, such a lesson stays fresh while one definition of the name is left.
- **Files without a query file** keep the text check, so a lesson about a table or a heading is judged by a
  word search. Node names there are not symbol names; a rule per kind is not in the requirements.
- **The numbers** 50 ms and 2 MiB are 032's; the first is proposed in the requirements and stays proposed.
