# 056: One graph for many repositories

**Status** not started · **Stage** Graph · **Entries** G-10; G-08 (worktree layout, `build --all`); G-09
(`impact --all`); R-03 (`find --all`); G-12 (`check --all`, the link rule); M-03 (the graph attacks); A-24, A-25 ·
**Needs** 049 · **Feeds** 062

## Result

A client with several repositories gets one graph per repository, the sub-projects inside a repository marked as
scopes, and a derived file of links between the repositories. Two commands cross those links when asked with
`--all`. A linked git worktree shares its repository's parse cache and keeps its own graph. Nothing of another
client is ever read. A person sees it like this, in a client folder that holds `app` (which imports `lib`) and
`lib`:

```bash
cd ~/clients/acme && tentaqles lobe graph build --all      # every repository and worktree, then the links
cd app && tentaqles lobe graph links                       # main.go imports lib, rule "module"
cd ../lib && tentaqles lobe graph impact --all --file greet.go --lines 3-3   # lists main in app
cd ../app && tentaqles lobe graph find Greet --all         # a hit in lib, with its root
tentaqles lobe graph status --json                         # "scopes":[...] for a monorepo
tentaqles lobe graph check --all                           # every graph of the client and links.json
```

## Scope

**In**

- Scopes inside one repository: the nine marker files, workspace lists, the depth guard, the 5-node floor, the
  `scopes` key on the meta line, and `scopes` in `tentaqles lobe graph status` (G-10 steps 1 to 3).
- `DiscoverRepos` and `tentaqles lobe graph build --all`: the bounded search under the client folder, one graph
  folder per repository, removal of the graph folder of a repository that is gone (G-10 step 4).
- Worktrees (A-24): a linked worktree is listed with its main repository, is built by `build --all` into
  `graph/<repo-key>/wt-<8 hex>/`, reuses the repository's `parse/`, and its folder is removed when the worktree
  is gone (G-10 step 5, G-08 step 1). The folder layout itself is 043's; see "What it builds on".
- The link pass, `graph/links.json` and `tentaqles lobe graph links [--json]` (G-10 step 6).
- `--all` on `impact` (G-09) and on `find` (R-03), which both refuse the flag until now, and the link rule
  `CheckLinks` in `check` and `check --all` (G-12 step 4).
- The isolation rules of G-10 and the M-03 attacks that concern the graph: L-02 widened from one graph to links
  and the two `--all` commands, and the new command `links` and the new flags under L-13 and L-15.
- Both hosts. The commands are the same in a Claude Code and a Codex session; the login check reads
  `CLAUDE_CONFIG_DIR` or `CODEX_HOME` (F-07 step 3).

**Out**

- Anything in a hook. Blast radius inside `post-tool-use` stays in one repository and never opens `links.json`
  ([049](049-blast-radius.md), A-25). No hook and no `stop` runs the repository search.
- `--all` on `callers`, `outline` and `map`. The requirements give the flag to `build`, `check`, `impact` and
  `find` only.
- The MCP surface of `find` and `impact` ([062](062-mcp-server.md)).
- Submodules and nested clones. They are not entered (G-10 risks). No feature takes them.
- Ranking each scope on its own terms in search. G-10 leaves it as an open question for R-01; no feature takes
  it. This feature records scopes and prints them, and changes no ranking.
- A progress line and a skip list for a client with hundreds of repositories (G-10 risks). No feature takes it.
- `tentaqles lobe graph documents --all`, where `--all` means something else ([061](061-pdfs-and-images.md)).

## What it builds on

- [002](002-sandbox.md): `tentaqles lobe sandbox`, the standard tester setup with `acme` and `globex`.
- [003](003-shell-failure-claude-code.md): `internal/lobe/store` with `store.RepoKey(clientDir, repoDir)`, which
  already keys a linked worktree by its main repository without running git (F-01 step 5, `TestLobeRepoKey`),
  the atomic `Replace` and the store lock.
- [005](005-status-and-capture-switch.md): the command rules of F-07 step 3 (the current folder's client,
  `--client` inside an agent session, the login check) and the `graph` setting.
- [013](013-isolation-leak-suite.md): `internal/lobe/benchcheck/leak` with its attack table, `not_run` with
  `needs`, and `--plant`. L-13 and L-15 take new commands from the command table with no new attack. The graph
  half of L-02 and `--plant graph` arrived with 043.
- [043](043-graph-of-one-repository.md): `internal/lobe/graph` (`types.go`, `store.go`), `graph.jsonl` with its
  meta, node, edge and `ext` lines, the parse cache with raw references, the file lister,
  `tentaqles lobe graph build`, and `GraphDir` and `ParseDir` with the `wt-<8 hex>` subfolder of a linked
  worktree (`TestLobeGraphDirLayout`).
- [044](044-graph-soundness.md): `Check`, `schema.go`, the list of rule functions in `check.go`, `check.json`,
  and `tentaqles lobe graph check [--all] [--file <path>] [--json] [--strict]`, where `--all` checks every graph
  folder of the client and no links yet.
- [045](045-resolution-and-callers.md): G-03's import rule (the file an import resolves to; for Go the
  lowest-sorted non-test file of the package folder), the language families, and the `dropped` counters.
- [046](046-find-and-map.md): `internal/lobe/search` (`Search`, `Index`), `internal/lobe/query` (the registry,
  `find`, the 8,000-character cap), and `TestLobeQueryFindAllRefused`, which this feature replaces.
- [048](048-graph-stays-current.md): `files.json`, `build.lock`, the probe, the `dirty/` markers and
  `tentaqles lobe graph status`. G-08 gives the worktree its own marker, lock, probe and build. Task 1 checks
  whether 048 added `root` to `files.json` and `TestLobeGraphWorktreeApart`; whatever is missing is built here.
- [049](049-blast-radius.md): `Blast`, `Report`, `Hit`, `tentaqles lobe graph impact`, which refuses `--all`
  until this feature, and the hook half of `TestLobeBlastHookStaysInRepo`; its `impact --all` half is added
  here.

## What it leaves for later features

- `scopes.go`: `DiscoverScopes`, `ScopeOf`; the `scopes` key on the meta line.
- `repos.go`: `DiscoverRepos(clientDir)`, returning `{root, key, worktrees}` per repository.
- `links.go`: `Link(clientGraphDir)`; `graph/links.json` in the shape of G-10; `linkcheck.go`: `CheckLinks`.
- `root` in every `files.json`, which `tentaqles doctor` already expects for its repository findings (F-07).
- `--all` on `build`, `check`, `impact` and `find`, and the command `tentaqles lobe graph links`.
- [062](062-mcp-server.md) serves `find` and `impact` through the same registry, so their `all` argument comes
  from here.
- [061](061-pdfs-and-images.md) writes document paths as `<repo-key>:<path>`; `root` leads a key back to a folder.
- The phase gate: the leak suite of 013 now runs its graph attacks and must stay at zero leaks.

## How it works

1. **Scopes.** After a repository's files are listed, `DiscoverScopes` marks a folder as a candidate when it
   holds one of the nine marker files of G-10 step 1. Workspace lists come from `pnpm-workspace.yaml`, the
   `workspaces` key of `package.json`, the `use` lines of `go.work` and Cargo's workspace `members` (the last two
   proposed). Only globs of the forms `dir/*`, `dir/**` and a plain name match. When a list exists, a folder
   whose only marker is `package.json` counts only if a glob matches it. A candidate deeper than 2 path segments
   is dropped unless a glob matched it. Of two nested candidates the shallower stays, except that a glob match
   replaces a parent no glob matched (G-10 step 2).
2. **The floor.** When more than one scope remains, a scope with fewer than 5 non-file nodes is folded into the
   root. A single scope is never folded. The meta line gains `scopes` after `skipped`: the prefixes, longest
   first, then by byte value; one scope gives `[]` (G-10 step 3, G-02's key order). A scope is
   `{prefix, label, markers}` and `ScopeOf(path)` returns the longest matching prefix.
3. **Repositories.** `tentaqles lobe graph build --all` calls `DiscoverRepos`: it looks for folders that hold
   `.git` under the client's folder to depth 3 (proposed), follows no link and no junction, and does not enter a
   repository or a worktree it has found. A repository whose real path lies outside the client's folder is
   ignored. Each repository is built under the same `build.lock` as a plain build (G-08). The graph folder of a
   repository that is gone is deleted: its `files.json` names a `root` that no longer holds `.git`.
4. **Worktrees.** The same search lists a folder whose `.git` is a file that leads, through its `gitdir:` line
   and that folder's `commondir`, to a repository of this client. F-01 owns the test and the key; no git command
   runs. A linked worktree is built after its main repository into `graph/<repo-key>/wt-<8 hex>/`, the 8 hex
   characters being the first of the SHA-256 of the worktree's path relative to the client folder, written with
   `/` (proposed). That subfolder holds everything a main working tree's folder holds except `parse/`:
   `ParseDir` always returns the main repository's `parse/`. Its `files.json` carries the worktree's path as
   `root`. `build --all` deletes the `wt-` folder of a worktree that is gone (G-08 step 1, G-10 step 5).
5. **Links.** After `build --all`, `Link` reads the graph of every repository's main working tree and rewrites
   `graph/links.json` from nothing. It skips `wt-` folders (proposed) and any graph folder whose `root` no longer
   holds `.git`. Two rules make a link (G-10 step 6):
   - `module`: an `ext` import of the module or package name that exactly one other repository declares in
     `go.mod`, `package.json` or `pyproject.toml`, or of a path under such a Go module, gives an `imports` link,
     `extracted`. `from` is the importing node; `to` is the file G-03's import rule picks inside the target
     repository. When that rule picks no file there is no link.
   - `single-declaration`: an unresolved member call from the parse cache gives a `calls` link only when the
     client's repositories hold exactly one same-language declaration of that name and it is in another
     repository: `inferred`, score 0.85. At most 64 unresolved calls are kept per caller.

   The file is `{"v":1,"links":[{"from_repo","from","to_repo","to","relation","confidence","score","rule"}]}`,
   sorted; `from_repo` and `to_repo` are `root` paths, `from` and `to` are node ids. Before it is written,
   `CheckLinks` must report no link whose end is missing from the named graph. Links never enter a
   `graph.jsonl`. Because the pass starts from nothing, a link disappears when a second candidate shows.
6. **`tentaqles lobe graph links [--json]`** prints the links that start or end in the current repository;
   `--json` prints one array, `[]` when there are none. A session in a linked worktree uses its repository's
   links.
7. **`impact --all`.** The backward walk of 049 goes on across a link: when it reaches a node named as `to` in
   `links.json`, it continues at the link's `from` node in the other repository's graph, one step deeper, under
   the same depth limit. Such a hit carries its repository's `root` and the link's confidence (proposed).
   Without `--all` nothing changes.
8. **`find --all`.** The search runs over every repository of the client and names each foreign hit's `root`
   (G-10 step 7; R-03 step 1 says it follows `links.json`). The requirements do not say how the lists of several
   repositories merge. This file proposes: run `Search` on each repository's index with the same limit, then
   merge by reciprocal rank with the constant 60 that Trail-code uses (`src/ask/fuse.ts` L79, named in G-10's
   source table), ties by `root` then name. The owner confirms this at task 1.
9. **`check --all`** checks every graph of the client and `links.json`; `check` without it runs `CheckLinks`
   whenever `links.json` exists; `--file` skips the link rule (G-12).
10. **The switch.** With `graph: off`, `build --all` and `links` print the line of G-08 that names the setting
    and the fix `tentaqles lobe set graph on`, and exit 1. None of these commands is person-only.
11. **Isolation.** Every path the package opens is built from one client name and lies under that client's
    `lobe/graph/`. No function takes two clients. No index exists outside a client's folder. The same repository
    cloned under two clients gets two unrelated graphs. `links.json` is opened only by `links`, `check`, the
    link pass and the two `--all` commands.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-10](../prd/lobe-graph.md), all of it; [G-08](../prd/lobe-graph.md) step 1 and the `GraphDir` and `build` bullets; [G-09](../prd/lobe-graph.md) step 4 and the `impact` bullet; [G-12](../prd/lobe-graph.md) step 4 and the `check` bullet; [R-03](../prd/lobe-recall.md) steps 1 and 5; [F-01](../prd/lobe-foundation.md) step 5; [F-07](../prd/lobe-foundation.md) step 3; [M-03](../prd/lobe-benchmarks.md) attacks L-02, L-13, L-15; [core page 5.3](../prd/vertical-lobe.md) |
| Source to port: scopes | [`src/graph/scopes.ts` L32](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/scopes.ts#L32): the nine marker files; [L172](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/scopes.ts#L172): the depth guard; L114: the glob forms; L69: the workspace lists; L165, L237, L252: the nesting rules |
| Source to port: the floor | [`src/graph/build.ts` L109](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/build.ts#L109): `MIN_SCOPE_NODES = 5`, applied only when more than one scope exists (L114) |
| Source to port, changed: repositories | [`src/graph/workspace.ts` L98](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/workspace.ts#L98): one graph per git child; it has no links between repositories |
| Source to port: call links | [`graphify/cross_repo_calls.py` L278](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/cross_repo_calls.py#L278): anything but exactly one candidate is skipped; [L311](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/cross_repo_calls.py#L311): the score 0.85; `graphify/extract.py` L5103: the cap of 64 |
| Not taken | [`graphify/global_graph.py` L10](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/global_graph.py#L10): one merged store in the home folder for every project on the machine |
| Existing tentaqles code | `internal/resolve/resolve.go` (how a folder becomes a client), `internal/paths/paths.go` (`IdentityDir`), `internal/cmd/run.go` (`insideAgent`), `test/integration/docs_test.go` (`documentedGroups`), and the lobe packages named under "What it builds on" |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits. In particular: what 043 and 048
   built for worktrees; the two refusals of `--all` and the tests that hold them; which leak attacks still sit
   under `not_run`; how the parse cache marks a member call that resolution dropped. Put the merge rule of
   step 8 to the owner. Files: none. Check: a written list the owner approves.
2. Scopes: `internal/lobe/graph/scopes.go`, the `scopes` key in the builder and in `status`. Tests:
   `TestLobeScopesDiscovery`, `TestLobeScopesFloor`. Can run beside task 3.
3. Repositories and worktrees: `internal/lobe/graph/repos.go`, `root` in every `files.json`, and `--all` on
   `build`: each repository, then its worktrees, then removal of folders that are gone. Tests:
   `TestLobeReposDiscovered`, `TestLobeReposListWorktrees`, `TestLobeGraphWorktreeApart`.
4. The link pass: `internal/lobe/graph/links.go`, and `links` as an action of `tentaqles lobe graph` in
   `internal/lobe/cli`. Tests: `TestLobeLinkNeedsExactlyOne`, `TestLobeLinksModule`,
   `TestLobeLinksRebuiltFromNothing`. Needs task 3.
5. The link rule: `internal/lobe/graph/linkcheck.go`, added to the list in `check.go`, and `--all` on `check`.
   Test: `TestLobeLinkEndsExist`. Can run beside task 4 once the `links.json` shape is fixed.
6. `impact --all` in `internal/lobe/graph/blast.go` and the `impact` verb; the refusal of 049 goes. Tests:
   `TestLobeAllFollowsLinks` (the `impact` half), `TestLobeBlastHookStaysInRepo`. Needs task 4. Can run beside
   task 7.
7. `find --all` in `internal/lobe/query`; `TestLobeQueryFindAllRefused` of 046 is replaced by
   `TestLobeQueryFindAll`. Also `TestLobeAllFollowsLinks` (the `find` half). Needs task 4.
8. Isolation: `TestLobeGraphNeverCrossesClients`, `TestLobeGraphPathsStayInTheClient`, and in
   `internal/lobe/benchcheck/leak/attacks.go` L-02 widened: the repository cloned in both clients declares one
   module, acme builds with `--all`, and globex's `links`, `find --all` and `impact --all` must print nothing
   of acme's. Check that L-13 and L-15 run `links` and the `--all` forms. Test:
   `test/integration/lobeleak_test.go` stays at 0 leaks.
9. Docs: the `tentaqles lobe graph` section of `docs/reference/commands.md` (the `links` action and each `--all`),
   `CHANGELOG.md` under `[Unreleased]`, and `CLAUDE.md`: add `TestLobeGraphNeverCrossesClients` and
   `TestLobeGraphPathsStayInTheClient` to the isolation line that 013 added under "Must not trade off".
10. The tester run of "Evaluation", by an agent that built none of this.

## Tests that stay in the repository

- `TestLobeScopesDiscovery`: a pnpm workspace listing `packages/*` gives 2 scopes, and none for unlisted
  `tools/x`.
- `TestLobeScopesFloor`: of two scopes with 4 and 5 non-file nodes, 1 remains. Alone, the 4-node scope remains.
- `TestLobeReposDiscovered`: 2 repositories give 2 graph folders, each named by `store.RepoKey`; a third behind a
  symbolic link gives 0 more.
- `TestLobeReposListWorktrees`: a repository with 2 linked worktrees gives 1 graph folder holding 2 `wt-`
  subfolders after `build --all`; removing one worktree and building again leaves 1; a module declared in the
  repository and repeated in its worktrees still gives 1 link.
- `TestLobeQueryFindAllRefused` (046's) is removed, and the refusal case of `impact --all` in 049's tests with
  it: both flags work from here on. The owner sees this in task 1's list.
- `TestLobeGraphWorktreeApart` (G-08; kept green if 048 added it, added here if not): a repository and one
  linked worktree whose `a.py` differs give 1 folder holding 1 `wt-` subfolder; the two `graph.jsonl` files
  differ; the subfolder has no `parse/`; building the worktree parses only `a.py`; an edit in the worktree leaves
  1 marker in the subfolder and 0 in the main one.
- `TestLobeLinkNeedsExactlyOne`: one declaration gives 1 link with score 0.85; a second declaration gives 0.
- `TestLobeLinksModule` (new here): `app` importing the module `lib` declares gives 1 `imports` link with rule
  `module`, `extracted`; with the module declared in two repositories, 0.
- `TestLobeLinksRebuiltFromNothing` (new here): deleting `links.json` and running the pass gives the same bytes.
- `TestLobeLinkEndsExist`: a `links.json` whose `to` names a missing node gives 1 problem in the check.
- `TestLobeAllFollowsLinks`: with `app` importing `lib`, `impact --all` on a change in `lib` lists the caller in
  `app` and `impact` without the flag lists 0 paths of `app`; `find --all` run in `app` returns a hit in `lib`.
- `TestLobeQueryFindAll`: with two linked repositories in one client, `find` prints hits from the current one
  only and `find --all` prints hits from both, each foreign hit with its `root`; neither prints a path from
  another client's graph.
- `TestLobeBlastHookStaysInRepo` (049's): the hook's list holds 0 paths of the second repository and opens 0
  files outside the edited repository's graph folder; `impact --all` on the same change lists that caller.
- `TestLobeGraphNeverCrossesClients`: one module in both clients; `acme`'s links and results hold 0 `globex`
  paths.
- `TestLobeGraphPathsStayInTheClient` (new here): during `build --all`, `links`, `find --all` and `impact --all`
  in `acme`, 0 files are opened under `identities/globex/` or under `globex`'s folder, a junction from `acme`'s
  folder into `globex`'s repository included.
- House rules kept green: `TestLobeLeak_AllAttacksZeroLeaks`, `TestLobeLeak_SuiteGoesRedOnAPlantedLeak`,
  `TestLobeLeak_NothingSkippedSilently`, `TestLobeCommandRefusesAnotherLogin` (now with `links`),
  `TestLobeGraphDeterministic`, `TestLobeGraphIncrementalEqualsCold`, `TestLobeBlastCutoffKeepsCapture`,
  `TestLobeCommandTable`, `TestDocsReferenceEveryCommand`, the import test of F-02, and every test under "Must
  not trade off" in `CLAUDE.md`.

## Evaluation

Tester brief. Start from the standard tester setup of [002](002-sandbox.md) (F-07): bash, the built `tentaqles`
first on `PATH`, `W` the working folder, `HOME`, `USERPROFILE` and `TQ_HOME` inside it, `tentaqles lobe sandbox
"$W"`, and an empty repository `app` in each client. No network. `ACME` is `$W/clients/acme` and `GLOBEX` is
`$W/clients/globex`. Run `git init lib` in both. Create these files (`\n` is a line break), then run
`tentaqles lobe graph build --all` in `ACME` and in `GLOBEX`.

- `ACME/lib/go.mod` and `GLOBEX/lib/go.mod`: `module example.test/lib\n\ngo 1.22\n`
- `ACME/lib/greet.go` and `GLOBEX/lib/greet.go`: `package lib\n\nfunc Greet() string { return "hi" }\n`
- `ACME/app/go.mod`: `module example.test/app\n\ngo 1.22\n`
- `ACME/app/main.go`: `package main\n\nimport "example.test/lib"\n\nfunc main() { println(lib.Greet()) }\n`

1. `$TQ_HOME/identities/acme/lobe/graph/` holds exactly 2 folders and `links.json`; so does that of `globex`.
2. In `ACME/app`, `tentaqles lobe graph links --json` exits 0 and prints at least 1 object with
   `"from":"main.go"`, `"relation":"imports"`, `"rule":"module"` and a `to_repo` naming `lib`. In `GLOBEX/lib` it
   prints `[]`.
3. No file under `$TQ_HOME/identities/acme/` contains the absolute path of `GLOBEX`, and none under
   `$TQ_HOME/identities/globex/` contains the absolute path of `ACME`. On Windows search for the path written
   with `/`, with `\` and with `\\`.
4. In `ACME/lib`, `tentaqles lobe graph impact --all --file greet.go --lines 3-3 --json` exits 0 and its
   `impacted` list has an entry with `"name":"main"`; the same command without `--all` has none. In `ACME/app`,
   `tentaqles lobe graph find Greet --all --json` exits 0 and prints a hit whose path is `greet.go`; the same
   command without `--all` prints no hit with that path.
5. In `ACME`, `tentaqles lobe graph check --all --json --strict` exits 0 and prints `"ok":true`.
6. Copy `ACME/lib` to `ACME/lib2` and build with `--all`. In `ACME/app` the command of check 2 now prints `[]`.
7. A linked worktree. In `ACME/lib` run `git add -A`, then
   `git -c user.name=t -c user.email=t@example.test commit -q -m one`, then `git worktree add -q ../lib-wt`.
   Build with `--all` in `ACME`. `$TQ_HOME/identities/acme/lobe/graph/` holds exactly 3 folders (those of `app`,
   `lib` and `lib2`), no fourth for `lib-wt`, and exactly one of them holds a folder named `wt-` plus 8 hex
   characters. That folder holds `graph.jsonl` and `files.json` and no `parse` folder.
8. In `ACME/lib` run `git worktree remove ../lib-wt`, then build with `--all` in `ACME`: no folder named `wt-*`
   is left under `$TQ_HOME/identities/acme/lobe/graph/`.
9. In `ACME` run `git init mono`. Add `mono/pnpm-workspace.yaml` with the lines `packages:` and
   `  - 'packages/*'`. In each of `mono/packages/a`, `mono/packages/b` and `mono/tools/x` add `package.json`
   containing `{}` and `m.ts` with five lines, `export function f1() {}` to `export function f5() {}`. Build in
   `ACME/mono`. There, `tentaqles lobe graph status --json` shows `"scopes":["packages/a","packages/b"]`.
10. In `ACME/app`, `CLAUDECODE=1 CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude" tentaqles lobe graph links
    --json; echo "exit=$?"` prints `exit=1` and no line holding `lib`. The same with
    `CODEX_HOME="$TQ_HOME/identities/globex/codex"` in place of `CLAUDE_CONFIG_DIR` also prints `exit=1`.
11. `tentaqles lobe bench leak --out "$W/leak"` exits 0 with `"leaks":0` and `"controls_failed":0` in
    `$W/leak/summary.json`. `not_run` holds `L-14` with `"needs":"person"` and no attack that waits for the
    graph.
12. `tentaqles lobe bench leak --plant graph --out "$W/leak2"` exits 1 with `"leaks"` at least 1.

**Person checks** (the owner, in a terminal of their own)

1. In one real client folder that holds at least two repositories, run `tentaqles lobe graph build --all`, then
   `tentaqles lobe graph links` in one of them, and read whether the links are true.

Scoring: PASS needs 12 of 12. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`).
- [ ] The tester brief passes, 12 of 12.
- [ ] The house rules hold: `TestLobeLeak_AllAttacksZeroLeaks` with the graph attacks run,
  `TestLobeGraphNeverCrossesClients`, `TestLobeGraphPathsStayInTheClient`, `TestLobeBlastHookStaysInRepo`,
  `TestLobeBlastCutoffKeepsCapture`, and the guard's tests untouched.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md` and the `CLAUDE.md` line are updated.
- [ ] The owner has seen the result described under Result, on the person check.

## Risks and unknowns

- Proposed numbers to confirm before building on them: search depth 3; the `go.work` and Cargo workspace lists;
  the `wt-` name from the worktree's relative path; skipping `wt-` folders in the link pass; the confidence a
  cross-repository hit carries.
- The merge rule of `find --all` is this file's proposal, not the requirements'. R-03 says the command follows
  `links.json`; G-10 says it ranks over every repository of the client. This file follows G-10, which owns the
  flag. If the owner reads it the other way, steps 8 and the `find` tests change.
- Scopes rank like the rest of the repository. Trail-code's README says scopes rank on their own terms while its
  code uses one maximum (G-10 risks). A decision there would change 046, not this file.
- A renamed repository folder gets a new key, and `build --all` deletes the old graph folder. Events and lessons
  that carry the old key no longer match (F-01 risks).
- A name match across repositories is a guess even with one candidate, so such links are `inferred`.
- A client folder with hundreds of repositories makes `--all` slow, and nothing here shows progress.
- S-01 walks up to the nearest `.git`, so an event inside a nested clone carries a key with no graph behind it.
- 049's tester brief expects `impact --all` to exit 1, and 046's expects the same of `find --all`. Both checks
  stop holding once this feature lands; a rerun of those briefs must skip them.
