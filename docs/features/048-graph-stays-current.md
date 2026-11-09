# 048: The graph stays current

**Status** not started · **Stage** Graph · **Entries** G-08 (all of it), F-02 (the build step of the
detached pass), F-07 (`tentaqles lobe graph status`), G-12 (markers stay after a failed build) ·
**Needs** 046 · **Feeds** 049, 050, 051, 054, 055, 056

## Result

After the agent edits a file, in Claude Code or in Codex, the graph matches the code again shortly after the
turn ends, and nobody waits for it. The edit leaves a tiny marker file. When the turn stops, a detached
process notices what changed with one cheap file check per file, parses only the changed files and replaces
the graph. Until then a query that cites an edited file says "(may be stale)".

```bash
cd "$W/clients/acme/shop"
tentaqles lobe graph status --json     # "changed":[] ... "dirty":0
# the agent edits a.py: the PostToolUse hook writes one marker
tentaqles lobe graph status --json     # "changed":["a.py"] ... "dirty":1
# the turn ends: the Stop hook returns at once; the detached pass rebuilds
tentaqles lobe graph status --json     # "changed":[] ... "dirty":0
```

## Scope

**In**

- `files.json`, `dirty/`, `build.lock` and `summary.json` in each graph folder, with the build id on the
  sidecars.
- The probe: one `stat` per file, a hash only for a suspect file (G-08 step 2).
- The marker written at PostToolUse after `Write`, `Edit` and `MultiEdit` in Claude Code and after
  `apply_patch` in Codex, reading every file header of a patch (step 3).
- The graph build as a step of the detached pass that the Stop hook starts, on both hosts (steps 4 to 6):
  the lock, the incremental rebuild, the two cutoffs, markers that survive a failed or cut-off build.
- `tentaqles lobe graph build` under the same lock, now writing the sidecars.
- `tentaqles lobe graph status [--json]`.
- "(may be stale)" on `outline`, `callers` and `find` hits that cite a file a marker names (step 7).
- The `graph` switch for all of the above (step 8), and a Stop that starts a pass when `graph` alone is on.
- The linked-worktree layout in full, with its test.

**Out**

- The one-file parse inside the PostToolUse hook, the blast-radius note and `tentaqles lobe graph impact`
  ([049](049-blast-radius.md)). Here the hook step writes a marker and parses nothing.
- The argument note after an edit ([051](051-argument-check.md)); code pointers with a prompt
  ([050](050-prompt-injection.md)). Both read the index this feature keeps current.
- `build --all`, `scopes` other than `[]`, removing the folder of a worktree that is gone, and
  `graph/links.json` ([056](056-many-repositories.md)).
- G-11's document run, which follows the build in the pass from phase 5
  ([061](061-pdfs-and-images.md)).
- Marking a lesson stale when its symbol is gone ([055](055-stale-by-symbol.md)).
- The session brief's sentence "Code graph: <K> symbols" (R-05 step 4). This feature writes the
  `summary.json` it reads; [049](049-blast-radius.md) switches the sentence on, because it names `impact`.
- Pruning the parse cache. G-02 leaves it to G-08 and G-08 does not decide; `tentaqles lobe rebuild` still
  empties it.

## What it builds on

- **[043](043-graph-of-one-repository.md)**: `build.go` (hash every file, reuse a cached parse of the same
  extractor version), `list.go`, `GraphDir`, `ParseDir`, the extractor version, `tentaqles lobe graph build`
  with the `graph` switch, `TestLobeGraphDirLayout`.
- **[044](044-graph-soundness.md)**: the check before the graph is replaced, `check.json`,
  `TestLobeGraphBuildKeepsOldGraph`.
- **[045](045-resolution-and-callers.md)** and **[047](047-signatures.md)**: resolution over all nodes; the
  current extractor version.
- **[046](046-find-and-map.md)**: the index with its build id, written by the build.
- **[003](003-shell-failure-claude-code.md)** to **[011](011-retention-and-sweep.md)**: `tentaqles lobe-hook`
  with its client rule, cutoffs and step table (`internal/lobe/hook`, `steps.go`), the plugin's and Codex's
  hook entries for `PostToolUse` and `Stop` (no matcher, so edit tools already arrive), the event line
  (S-03), the duplicate filter (S-06), `stats/hook-errors.jsonl`, the store's atomic write, `ErrBusy`.
- **[010](010-shell-failure-codex.md)**: the Codex post-tool payload and its fixtures from the owner's
  phase 0 capture, an `apply_patch` call among them.
- **[018](018-repair-pairing.md)**: `hook.Detach`, `tentaqles lobe consolidate --detached`, the pass runner
  `internal/lobe/pass` with its ordered steps and `consolidate.lock`.
- **[023](023-fires-and-verdicts.md)** and **[024](024-automatic-demotion.md)**: E-02's judging, which runs
  before the marker in the hook, and E-07's ladder moves, which run before the build in the pass.
- **[012](012-hook-latency-and-stall.md)** and **[013](013-isolation-leak-suite.md)**: the latency, stall
  and leak checks, which must still pass.
- **[005](005-status-and-capture-switch.md)**: settings, `internal/lobe/cli`, F-07 step 3 and its tests.

## What it leaves for later features

- `internal/lobe/graph`: `probe.go` (`Probe(repo) (Drift, error)` with sorted `changed`, `added`,
  `removed`), `dirty.go` (write, list and delete markers), `buildlock.go`, and the incremental path in
  `build.go`. 049's `impact` probes first; 056 builds every repository with the same code.
- `graph/<repo-key>/files.json`: per path `[size, mtime_ns, sha256, read_at_ns]`, the extractor version, the
  build id, and `root`, the repository's path relative to the client folder. `tentaqles doctor` and 056 lead
  from a key back to a folder through `root`.
- `graph/<repo-key>/dirty/<unix-nanos>-<pid>`: 049 and 050 mark what a marker names as possibly stale.
- `graph/<repo-key>/summary.json`: the build id and the symbol count, for the session brief
  ([026](026-session-brief.md)).
- The post-tool step "mark edited files", after which 049 adds its one-file parse, and the list of edited
  paths that step hands on.
- The graph build step of the pass; 061 adds the document run after it.
- The command `tentaqles lobe graph status`, whose `scopes` key 056 fills.

## How it works

1. **The files** (G-08 step 1). Beside `graph.jsonl` and `parse/`, each graph folder gains `files.json`,
   `dirty/`, `build.lock` and `summary.json`. `files.json`, the index and `summary.json` carry the build id,
   the first 16 hex characters (proposed) of the SHA-256 of `graph.jsonl`; a reader ignores a sidecar whose
   id differs from the graph's. Another extractor version in `files.json` voids every record.
2. **The probe** (step 2). `stat` every file the build would read. A file is clean, unread, when its size
   and mtime equal the record and the record was read at least 2 seconds after that mtime; `read_at_ns` is
   the clock taken just before the file was read. Any other file is hashed, and the hash decides, so a
   `touch` is not drift. The probe writes nothing and returns sorted `changed`, `added` and `removed` lists.
3. **The marker, Claude Code.** `tentaqles lobe-hook claude post-tool-use` with `tool_name` `Write`, `Edit`
   or `MultiEdit` reads `tool_input.file_path`.
4. **The marker, Codex.** `tentaqles lobe-hook codex post-tool-use` with `tool_name` `apply_patch` reads the
   patch text in `tool_input` and takes the path of every file header in it, not only the first.
5. **The marker, both hosts.** The step runs after the event line is appended (S-03) and the open fires are
   judged (E-02), and also when the duplicate filter (S-06) writes no new line. Each path is made relative
   to the repository of the payload's `cwd`, with `/`; a path outside that repository is left out. With at
   least one path left, the step creates one file `dirty/<unix-nanos>-<pid>` that holds the paths, one per
   line. It does no probe and no parse and opens no source file. It runs inside F-02's client rule, so a
   session under another client's login, or under none, writes nothing. A failure leaves one line in
   `stats/hook-errors.jsonl` with step `G-08` and never any output.
6. **Stop.** `tentaqles lobe-hook <host> stop` starts one detached pass and returns (F-02 step 8). It starts
   a pass when any of `capture`, `lessons` and `graph` is on. The pass runs S-07's sweep, S-08's fold,
   consolidation, E-07's ladder moves, then the graph build. A pass started by
   `tentaqles lobe consolidate --only` or `--session` leaves the graph build out.
7. **The build step** (step 4) works on the repository of the session's folder. It takes `build.lock`, lists
   the markers and runs the probe. It probes even with no marker, because a shell command, a formatter or
   `git checkout` changes files without an edit tool. With no drift and no marker it writes nothing. With no
   graph yet it builds from nothing.
8. **The rebuild** (step 5): hash every file, reuse a cached parse when the hash matches and the cache file
   names this extractor version, parse the rest, resolve edges over all nodes, run the soundness check,
   replace `graph.jsonl`, write `files.json`, the index and `summary.json`, and delete the markers listed in
   step 7 and no others. An edit made during the build keeps its marker. An incremental build gives the same
   bytes as a build from nothing.
9. **Cutoffs** (step 6). A rebuild is cut off at 120 seconds, a first build at 10 minutes. The build step
   carries its own deadline, so a slow or failed earlier step of the pass neither shortens nor skips it
   (proposed). After a cutoff, an error or a failed soundness check, the old graph and the markers stay, and
   the next Stop tries again.
10. **`build.lock`** is created exclusively and holds `{pid, at}`. Its age is the file's modification time.
    It can be taken over at 15 minutes, or at 5 minutes when its process is dead. It only decides who
    builds: each file is still written by F-01's atomic write under the store lock, held for that write
    alone, so capture never waits. A pass that finds the lock held skips its build step.
11. **`tentaqles lobe graph build`** builds in the foreground under the same lock and writes the same
    sidecars. When another process holds the lock, it prints one line and exits 1. There is no separate
    `refresh` verb.
12. **`tentaqles lobe graph status [--json]`** runs the probe and prints `enabled` (the `graph` setting),
    `repo` (the repository's path relative to the client folder), `files` (records in `files.json`),
    `scopes` (`[]` until 056), `changed`, `added`, `removed`, `dirty` (marker files) and `building`
    (`build.lock` exists). It never builds, it is not person-only, and it also answers with `graph: off`.
13. **Readers never rebuild and never wait** (step 7). `outline`, `callers` and `find` list the marker
    files, and add "(may be stale)" to a hit that cites a file a marker names. A change that no marker
    names is not marked.
14. **The switch** (step 8). Steps 3 to 9 run when `graph` is `on`, whatever `recall`, `lessons` and
    `enforce` say. With `graph: off` the hook writes no marker, the pass skips its build step, readers act
    as if there were no graph, and what is on disk stays, unread.
15. **Worktrees.** In a linked worktree every path above means the `wt-<8 hex>` subfolder: its marker, its
    lock, its probe and its build are its own. Only `parse/` is the main repository's, shared because it is
    keyed by content hash; two worktrees that write the same cache file write the same bytes.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-08](../prd/lobe-graph.md), all of it; [G-02](../prd/lobe-graph.md), step 5; [G-12](../prd/lobe-graph.md), "How to build it" (a failed check keeps the markers); [R-01](../prd/lobe-recall.md), "How to build it" (the index and its build id); [R-05](../prd/lobe-recall.md), "How to build it" (`summary.json`) |
| Requirements | [F-02](../prd/lobe-foundation.md), step 2 (the order on `post-tool-use`), step 7 (hook errors) and step 8 (the pass); [F-01](../prd/lobe-foundation.md), steps 1, 5, 6 and 8; [F-06](../prd/lobe-foundation.md), the `graph` row; [F-07](../prd/lobe-foundation.md), the `graph build` and `graph status` rows; [S-02](../prd/lobe-sense.md), steps 1, 6 and 7 (Codex's post-tool payload); [core page](../prd/vertical-lobe.md) 5.2, 5.3 and 5.6 |
| Source to port: the probe | Trail-code [`src/graph/fingerprint.ts` L135](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/fingerprint.ts#L135) (stat fast path), [L185](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/fingerprint.ts#L185) (hash a suspect file); Graphify [`graphify/cache.py` L315](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/cache.py#L315) (trust a record only when read after the mtime tick closed; also L270) |
| Source to port: the rebuild | Trail-code [`src/graph/build.ts` L316](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/build.ts#L316) (replay a cached parse only on an equal hash), [L360](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/build.ts#L360) (resolve over everything) |
| Ported, changed: the hook and the cap | Trail-code [`src/claude/hooks.ts` L99](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hooks.ts#L99) (its hook runs a check under an 8-second cap; L90 reads only the first file of a Codex patch); [`src/claude/sync-run.ts` L15](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/sync-run.ts#L15) (the 120000 ms cap; L53 clears its flag without looking for edits made during the build) |
| Not copied | Trail-code [`src/graph/refresh.ts` L214](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/refresh.ts#L214): a query rebuilds while its caller waits. L69 of the same file is the 10-minute first build |
| The lock | Trail-code [`src/util/state.ts` L203](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/util/state.ts#L203) (a pid lock file; L208, L24, L29, L256, L257 for the ages) |
| Existing tentaqles code | `internal/lobe/hook/steps.go`, `internal/lobe/pass`, `internal/lobe/graph/build.go` and `store.go`, `internal/lobe/query`, `internal/lobe/store`, `internal/lobe/health`, `plugin/hooks/hooks.json` and `internal/codexconfig` (read only: the entries already exist), `docs/reference/commands.md` |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits: the key that carries the
   patch text in Codex's `apply_patch` payload, from 010's fixtures (write it into check 11 of the
   Evaluation, where it reads `<P>`); how 018's `hook.Detach` hands the session's folder to the pass; where
   the step table lets a step run after E-02; how the store writer takes a list of paths (its `scrub.Clean`
   type cuts text at 4000 characters, so a long patch may need several markers); whether 026's brief already
   reads `summary.json`. The owner approves changes before code.
2. **`files.json` and the build id** written by the build of 043; `root` in it. Proves it:
   `TestLobeGraphIncrementalEqualsCold` (first half: same bytes as a cold build).
3. **`probe.go`** (side by side with tasks 4 and 5). Proves it: `TestLobeGraphProbeTouchIsClean`,
   `TestLobeGraphProbeSameSizeEdit`.
4. **`dirty.go` and the hook step** for both hosts (side by side). Proves it:
   `TestLobeGraphPostEditOnlyMarks`, `TestLobeGraphCodexPatchMarksEveryFile`, and the unchanged
   `TestLobeHookServesOnlyItsLogin` and `TestLobeCaptureSurvivesALateStep`.
5. **`buildlock.go`** (side by side). Proves it: `TestLobeGraphOneBuilder`, and a unit test of the two
   take-over ages with a fake clock.
6. **The incremental build**: delete only the listed markers, the two cutoffs, `summary.json`. Proves it:
   `TestLobeGraphIncrementalEqualsCold` (1 file parsed), `TestLobeGraphEditDuringBuildStaysDirty`,
   `TestLobeGraphBuildKeepsOldGraph` (widened: the markers stay).
7. **The pass step** in `internal/lobe/pass`, and Stop starting a pass with only `graph` on. Proves it:
   `TestLobeGraphSettingGatesBuild`, `TestLobeGraphBuildSurvivesEarlierSteps`,
   `TestLobeDetachDoesNotHoldTheHookOpen`.
8. **`tentaqles lobe graph status`** and `build` under the lock. Proves it: `TestLobeGraphStatus`, the new
   rows in `TestLobeCommandTable`, `TestLobeAgentCannotReadAnotherClient` and
   `TestLobeCommandRefusesAnotherLogin`.
9. **"(may be stale)"** in `internal/lobe/query` (side by side with task 8). Proves it:
   `TestLobeQueryMarksStale`.
10. **Worktrees** end to end. Proves it: `TestLobeGraphWorktreeApart`.
11. **Hook checks again**: run 012's `tentaqles lobe bench latency` and `stall` for both hosts and 013's
    `tentaqles lobe bench leak`; an edit payload joins the latency cases.
12. **Docs**: the `status` action and the stale mark under `## tentaqles lobe graph`, and the new work of
    `post-tool-use` and `stop` under the `lobe-hook` sections of `docs/reference/commands.md`; a line in
    `CHANGELOG.md`. The plugin's files do not change, so `plugin/CHANGELOG.md` gets no line.
13. **Tester run** of the Evaluation below.

## Tests that stay in the repository

- `TestLobeGraphProbeTouchIsClean`: rewriting a file with the same bytes gives 0 drift and writes 0 files.
- `TestLobeGraphProbeSameSizeEdit`: a same-length edit within 2 seconds of the last build, mtime put back,
  is seen.
- `TestLobeGraphIncrementalEqualsCold`: after 1 edit among 50 files, same bytes as a cold build and 1 file
  parsed.
- `TestLobeGraphEditDuringBuildStaysDirty`: a marker created while a build runs still exists after it.
- `TestLobeGraphOneBuilder`: two detached processes started together run exactly 1 build.
- `TestLobeGraphPostEditOnlyMarks`: with no graph, the hook writes 1 marker, opens 0 source files and meets
  the capture budget that M-06 holds.
- `TestLobeGraphSettingGatesBuild`: with `graph: off`, an edit and a `stop` give 0 markers and 0 builds;
  with `graph: on` and `recall`, `lessons` and `enforce` all `off`, they give 1 marker and 1 build.
- `TestLobeGraphBuildSurvivesEarlierSteps`: with the consolidation step forced to fail, the pass still runs
  1 build.
- `TestLobeGraphWorktreeApart`: a repository and one linked worktree whose `a.py` differs give 1 folder
  `graph/<repo-key>/` holding 1 `wt-` subfolder; the two `graph.jsonl` files differ; the subfolder has no
  `parse/`; building the worktree parses only `a.py`; an edit in the worktree leaves 1 marker in the
  subfolder and 0 in the main one.
- `TestLobeGraphCodexPatchMarksEveryFile` (new here): an `apply_patch` payload that names three files leaves
  1 marker with 3 paths; a path outside the repository is left out.
- `TestLobeGraphStatus` (new here): the nine keys; `building` follows `build.lock`; the command creates no
  file and starts no build; with `graph: off` it prints `"enabled":false` and exits 0.
- `TestLobeQueryMarksStale` (new here): with a marker that names `a.py`, `outline a.py` and a `find` hit in
  `a.py` carry "(may be stale)" and a hit in `b.py` does not.
- Widened: `TestLobeGraphBuildKeepsOldGraph` (markers stay), `TestLobeHookAlwaysExitsZero` (an edit payload
  and a patch payload among the inputs), `TestLobeAgentCannotReadAnotherClient` (`graph status`).
- House rules kept green. Never stalls: `TestLobeHookAlwaysExitsZero`, `TestLobeCaptureSurvivesALateStep`,
  `TestLobeDetachDoesNotHoldTheHookOpen`, and 012's checks. Wrong client gets nothing:
  `TestLobeHookServesOnlyItsLogin`, `TestLobeHookLoginUnsetDoesNothing`, `TestLobeLeak_AllAttacksZeroLeaks`.
  No secret on disk: a marker holds paths only, written through the store's writer. The guard untouched:
  `TestLobeGuardNeverImportsLobe`, and every test under "Must not trade off" in `CLAUDE.md`, unedited. No
  model is called by the build step.

## Evaluation

You are a tester agent. You did not build this feature. You receive the built `tentaqles` binary and an
empty working folder. Use bash (Git Bash on Windows), no network, and run:

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
cd "$W/clients/acme" && git init -q shop && cd shop
printf 'def one():\n    return 1\n' > a.py; printf 'x = 2\n' > b.py
tentaqles lobe graph build || echo "SETUP FAIL build"
export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"
```

If a line starts with `SETUP FAIL`, stop and report FAIL. The setup leaves `model` off, so the pass starts
no model call. `<S>` is the absolute path of `shop`. `G` is the single folder in
`$TQ_HOME/identities/acme/lobe/graph/`. "Status" is `tentaqles lobe graph status --json` run in `<S>`.

Three inputs, each one line of JSON with `<S>` replaced by that path:

```json
{"session_id":"s1","hook_event_name":"PostToolUse","cwd":"<S>","tool_name":"Edit","tool_input":{"file_path":"<S>/a.py","old_string":"return 1","new_string":"return 2"}}
{"session_id":"s1","hook_event_name":"Stop","cwd":"<S>"}
{"session_id":"c1","hook_event_name":"PostToolUse","cwd":"<S>","tool_name":"apply_patch","tool_input":{"<P>":"*** Begin Patch\n*** Update File: a.py\n@@\n-    return 4\n+    return 5\n*** Add File: d.py\n+z = 9\n*** End Patch\n"}}
```

The first is "the edit JSON", piped to `tentaqles lobe-hook claude post-tool-use` with the session id a
check names. The second is "the stop JSON", piped to `tentaqles lobe-hook claude stop`. The third is "the
patch JSON" for Codex; `<P>` is the key that carries the patch text, which the builder filled in. If it
still reads `<P>`, report FAIL for check 11. Change settings only with `lobeset`; never run
`tentaqles lobe set`.

1. Status exits 0 and prints one JSON object holding `"enabled":true`, `"repo":"shop"`, `"files":2`,
   `"changed":[]`, `"added":[]`, `"removed":[]`, `"dirty":0` and `"building":false`. Rewrite `a.py` with the
   same content: the same output.
2. Change `return 1` to `return 2` in `a.py` and pipe the edit JSON. Exit 0. `G/dirty` holds 1 file
   containing `a.py`. Status shows `"changed":["a.py"]` and `"dirty":1`.
3. `tentaqles lobe graph outline a.py` prints a line that holds `one` and `(may be stale)`.
4. Pipe the stop JSON. The hook exits 0 in under 2 seconds, without waiting for the build. Within 30 seconds
   status shows `"changed":[]` and `"dirty":0`, and the outline no longer holds `(may be stale)`.
5. Create `G/build.lock` containing `{"pid":1,"at":"<now>"}`, where `<now>` is the current UTC time written
   like `2026-11-09T14:03:11Z`, and `shop/c.py` containing `y = 3\n`. Pipe the stop JSON again. After 5
   seconds status shows `"added":["c.py"]` and `"building":true`.
6. Delete `G/build.lock` and pipe the stop JSON. Within 30 seconds status shows `"added":[]` and `"files":3`.
7. Delete the folder `G` and run `tentaqles lobe graph build`: exit 0, and status shows `"files":3`.
8. Run `lobeset acme recall off; lobeset acme lessons off; lobeset acme enforce off`. Change `return 2` to
   `return 3` in `a.py` and pipe the edit JSON with `"session_id":"s8"`: `G/dirty` holds 1 file. Pipe the
   stop JSON: within 30 seconds status shows `"changed":[]` and `"dirty":0`. Those three settings do not stop
   the build.
9. Run `lobeset acme graph off`. Change `return 3` to `return 4`, pipe the edit JSON with
   `"session_id":"s9"`, then the stop JSON, and wait 10 seconds. `G/dirty` holds 0 files and status shows
   `"enabled":false`. `tentaqles lobe graph build; echo "exit=$?"` prints a line that names the `graph`
   setting, then `exit=1`. Then run `lobeset acme graph on`.
10. Logged in as globex inside acme's folder: pipe the edit JSON with `"session_id":"s10"` under
    `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude"`. Exit 0, empty stdout, `G/dirty` holds 0 files,
    and `ls "$TQ_HOME/identities/globex/lobe/graph" 2>/dev/null | wc -l` prints `0`.
11. Codex. Run `unset CLAUDE_CONFIG_DIR; export CODEX_HOME="$TQ_HOME/identities/acme/codex"`. Change
    `return 4` to `return 5` in `a.py` and create `d.py` containing `z = 9\n`. Pipe the patch JSON to
    `tentaqles lobe-hook codex post-tool-use`. Exit 0. `G/dirty` holds 1 file that contains both `a.py` and
    `d.py`.
12. Pipe the stop JSON, with `"session_id":"c1"`, to `tentaqles lobe-hook codex stop`. It exits 0 in under 2
    seconds. Within 30 seconds status shows `"changed":[]`, `"added":[]`, `"dirty":0` and `"files":4`.
13. Run `unset CODEX_HOME`. Change `return 5` to `return 6` and pipe the edit JSON with `"session_id":"s13"`:
    exit 0, empty stdout, and `G/dirty` still holds 0 files, because no login folder says whose session
    this is.
14. A linked worktree. In `<S>` run `git add -A`, then
    `git -c user.name=t -c user.email=t@example.test commit -q -m one`, then
    `git worktree add -q ../shop-wt`. In `$W/clients/acme/shop-wt` run `tentaqles lobe graph build`: exit 0.
    `$TQ_HOME/identities/acme/lobe/graph/` still holds exactly 1 folder, `G`. `G` now holds exactly 1 folder
    whose name is `wt-` plus 8 hex characters; it holds `graph.jsonl` and `files.json` and no `parse`
    folder.

**Person checks**: none here. The owner's check of the `graph` switch by hand is in
[043](043-graph-of-one-repository.md).

Scoring: PASS needs 14 of 14. "Could not run" is a FAIL.

## Done when

- [ ] every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin suite still passes, unchanged
- [ ] the tester brief passes, 14 of 14, with `<P>` filled in
- [ ] the house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeCaptureSurvivesALateStep`,
  `TestLobeDetachDoesNotHoldTheHookOpen`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeLeak_AllAttacksZeroLeaks`, `TestLobeGuardNeverImportsLobe`; 012's latency and stall checks pass
  for both hosts with an edit payload
- [ ] `docs/reference/commands.md` documents `status`, the stale mark and the hook work; `CHANGELOG.md` has
  its line
- [ ] the owner has seen an edit turn into a marker and the graph catch up after the turn, as under Result

## Risks and unknowns

- **Codex's `apply_patch` payload.** The key that carries the patch text and the header forms come from the
  owner's phase 0 capture (S-02, feature 010). They were not documented when the requirements were written.
  If no patch text arrives on Codex's post-tool event, Codex edits are found only by the probe at Stop, and
  this file changes.
- **Whether a host stops a hook's child processes** when the hook returns is not known (F-02). If one does,
  the pass, and with it the build, must also start at the next session start.
- **Which repository a marker belongs to.** G-08 says "relative to the repository". This file takes the
  repository of the payload's `cwd`. An edit to a file in a sibling repository, or a session whose folder is
  the client folder above several repositories, leaves no marker and gets no build at Stop; the probe of
  the next build there still finds the change.
- **A same-length edit that also restores an old mtime** escapes the probe. `tentaqles lobe graph build`
  repairs it.
- **The 120-second cap is Trail-code's**, for its parser. 012 measures ours. Trail-code's timing comments
  are unrepeated claims.
- **A slow consolidation delays the build.** With `model: on`, C-03's limits (proposed there) allow 5
  drafting calls of 60 seconds each before the build starts. Readers mark what a marker names meanwhile.
- **Key names inside `files.json` and `summary.json`** are not given by the requirements, only their
  content. This file's proposal: `files.json` as `{"v","extractor","build","root","files"}` and
  `summary.json` as `{"v","build","symbols"}`. Confirm in task 1.
- **A foreground `build` that finds the lock held** exits 1 with one line here. G-08 says only "under the
  same lock".
- **The brief's graph sentence** (R-05) reads `summary.json` and names `impact`, which arrives with 049. If
  026 already reads the file, the sentence appears as soon as this feature lands. The owner should say
  which feature owns it.
- **The parse cache only grows** until someone runs `tentaqles lobe rebuild`.
- **What would change this file:** the phase 0 capture's answers for Codex, or an owner's call on the
  repository of a marker.
