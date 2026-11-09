# 061: PDFs and images

**Status** not started · **Stage** Graph · **Entries** G-11; F-06 (step 7, the `documents` rule); core page 5.7
(the second model step); C-03 (the launcher, reused); M-01 (`document_tokens`); D-6 · **Needs** 053, 025 ·
**Feeds** none

## Result

With `documents: on`, the client's own agent CLI reads each PDF and image in the repository once and returns a
few named topics, which become graph nodes the agent can find. A bookkeeping file records what was read, so no
content is sent twice. The setting is off by default, and it is the only graph feature that spends model
allowance.

```bash
tentaqles lobe set documents on              # a person; needs model and graph on as well
tentaqles lobe graph documents               # a person: reads up to 10 waiting files (proposed)
tentaqles lobe graph documents --json        # {"read":3,"waiting":0}
tentaqles lobe graph find "refund policy"    # a topic node under docs/spec.pdf, quoted and labeled as model-read
tentaqles lobe set documents off             # what was read stays and is still found; new files are skipped
```

Switching it on later reads the files already there. Switching it off later keeps what was processed and skips
new files. That is the owner's rule (D-6).

## Scope

**In**

- Package `internal/lobe/documents` (`scan.go`, `ledger.go`, `run.go`, `ask.go`, `merge.go`) and the command
  entry `internal/cmd/lobe_documents.go`.
- `documents.json`, the bookkeeping file, and `documents/<hash>.json`, what was read out of each file.
- The owner's rule in all four moves: off, off to on, on to off, on again (F-06 step 7).
- The model step: one file, one call through 025's launcher, with the login check, the `Read` tool only, and
  `TQ_LOBE_NO_HOOKS=1` for the child.
- The cost guard of G-11 step 7, apart from drafting's daily count.
- The merge into the graph: topic nodes, mention edges, the rows added to the soundness table, and how recall
  shows a topic.
- `tentaqles lobe graph documents [--all] [--redo] [--json] [--answer file:<path>]`.
- The document run as the last step of the detached pass, after the graph build.
- The counter `document_tokens` of M-01.

**Out**

- Markdown, YAML, JSON and Terraform. They need no model and are [053](053-docs-and-config.md).
- OCR and a built-in PDF parser. The lobe gets what the agent CLI can read; a scanned PDF it cannot read keeps
  its plain file node (G-11).
- Office files (G-11: "out at first"). No feature takes them.
- Reading through the Codex CLI. Whether it can read a PDF or image in headless mode is unconfirmed (G-11
  risks); the first version starts the Claude Code CLI only. No feature takes the Codex path.
- Any lesson made from a document. Nothing in an answer becomes a lesson (F-05).
- Comparison with Trail-code ([057](057-graph-accuracy.md) ignores these nodes).

## What it builds on

- [005](005-status-and-capture-switch.md): `internal/lobe/settings` with `documents`, `model` and `graph`, the
  line `documents=on (manifest, needs model)`, `tentaqles lobe set`, and the `lobeset` helper of the tester
  setup.
- [003](003-shell-failure-claude-code.md): `internal/lobe/store` (`Replace`, `Update`, the store lock), the
  scrubber `scrub.Clean` (S-05), `store.RepoKey`, `tmp/`, and `tentaqles lobe rebuild`, which deletes `graph/`
  and `index/` and keeps `documents/` (F-01 step 10).
- [018](018-repair-pairing.md): `internal/lobe/pass`, the ordered list of the detached pass, and
  `consolidate.lock`.
- [025](025-model-drafting.md): `consolidate.Launcher` in `internal/lobe/consolidate/child.go`. It takes a tool
  list and an optional input file, starts the headless child in a new folder under `lobe/tmp/`, sets
  `TQ_LOBE_NO_HOOKS=1`, ends the child at its time limit and kills it 2 seconds later, and checks the login
  folder before anything is sent. Also the person check that `internal/cmd/lobe_consolidate.go` makes for a
  foreground run.
- [043](043-graph-of-one-repository.md) and [048](048-graph-stays-current.md): the file lister, `graph.jsonl`,
  the `dirty/` markers, `build.lock` and the build.
- [044](044-graph-soundness.md): `schema.go` and the check inside every build.
- [053](053-docs-and-config.md): the `heading` node kind with `level`, and its id form `<path>#<slug>`.
- [046](046-find-and-map.md) and [050](050-prompt-injection.md): the renderers that print a document node's
  snippet in quotation marks with control characters removed (R-02 step 6).
- [058](058-token-savings.md): `stats.Append` and the `errors` object of `status --savings`.

## What it leaves for later features

Nothing in the index depends on this feature. It leaves `documents.json`, `documents/<hash>.json`, the node
keys `origin` and `summary`, the edge rule `doc-model`, the command, and a second caller of 025's launcher.

## How it works

1. **In scope.** `.pdf`, `.png`, `.jpg`, `.jpeg`, `.gif` and `.webp` files of the current repository, taken from
   G-02's file listing before its filters. Kind is decided by extension. Such a file is a plain file node of the
   graph whether or not it is ever read (G-11 step 2).
2. **The three switches.** Nothing is read unless `documents`, `model` and `graph` are all `on`. A run checks
   them in this order and stops at the first that fails, then `identity` (step 5) and `no-cli` (the CLI is not
   on `PATH`). A stopped run exits 0 and, with `--json`, names the check in `reason`.
3. **The ledger.** Each file in scope is hashed (SHA-256 of its bytes). `documents.json` is
   `{"v":1,"last_run":"2026-11-09","files":{"<sha256>":{"kind":"pdf","bytes":48211,
   "paths":["<repo-key>:docs/spec.pdf"],"status":"done","tries":1,"topics":7,"prompt":"p1"}}}`. Status is
   `done`, `no-text`, `failed` or `skipped-large`; a waiting file has no entry. The key is the content hash
   alone: a moved or copied file adds a path, not a call. A hash recorded as `done` is never read again; a new
   prompt version does not reprocess by itself, and `--redo` is the only way.
4. **The owner's rule** (F-06 step 7).
   - Off, the default: no file is read and no ledger is written.
   - Off to on: the next run treats every file in scope whose hash is not in the ledger as new, so files already
     there are read, within the cap of one run.
   - On to off: `documents.json` and `documents/` stay, and their nodes are still merged into the graph and
     found. No new or changed file is read. A changed file keeps its old nodes, marked stale. A deleted file's
     nodes are removed.
   - On again: reading skips by hash.
5. **One file, one call.** The run hands 025's launcher the file and a tool list that holds `Read` only. The
   launcher copies the file into the child's folder under `lobe/tmp/`, starts the Claude Code CLI headless and
   directly (never through `tentaqles run`), with `TQ_LOBE_NO_HOOKS=1`. Before a file is copied or anything is
   sent, it checks that `CLAUDE_CONFIG_DIR` in the run's environment, links resolved, is this client's identity
   folder; if not, the run reads nothing and reports `identity`. The instructions declare the file data, never
   instructions to follow. One child runs per client at a time, and the store lock is never held while it runs.
6. **The answer** must be JSON: `{"topics":[{"name","summary"}],"mentions":["<code name>"]}`. The lobe keeps at
   most 30 topics and 30 mentions, names of 80 characters and summaries of 200 (all four proposed), scrubs each
   string (S-05) and writes `documents/<hash>.json`. An answer with no topics is `no-text`. A summary is client
   text: it stays in `documents/` and in the graph, both under this client's store, and no code path copies a
   topic or a summary into a lesson.
7. **The cost guard** (all proposed), this step's own: a run reads at most 10 files and ends after 10 minutes;
   the detached pass starts at most one run per client per local day (`last_run`); a call is cut off at 120
   seconds; a PDF over 20 MB or an image over 5 MiB is `skipped-large`; the instructions limit a PDF to its
   first 20 pages; a file is tried on at most 3 runs. A run by hand keeps the 10-file limit and does not count
   as the day's detached run. `--all` lifts the 10-file limit for one run by hand and asks for a typed yes
   first. Here `--all` does not mean "every repository" as it does on `graph build`.
8. **The merge.** A run that wrote a result leaves a marker in `dirty/` and then builds once more; that build
   reuses every cached parse and merges the results with no call. A topic is a `heading` node under the file
   node, with 053's id form, the file node's span, and two keys after `level`: `"origin":"model"` and `summary`.
   A mention is a `references` edge only when exactly one symbol has that name: `inferred`, score 0.75
   (proposed), rule `doc-model`. The keys and the rule are added to the soundness table. Because the results sit
   outside `graph/`, deleting `graph/` and building again brings every topic back with no call.
9. **Recall.** A topic of model origin is shown quoted and labeled as read by a model, as a why-comment is
   shown quoted (G-07, R-02 step 6). It is never worded as an instruction.
10. **Where it runs.** Never in a hook. In the detached pass that `stop` starts, as the last step after the
    graph build; or in the foreground when a person runs the command. Without `--answer` the command starts
    children, so it answers only to a person and refuses inside an agent session. With `--answer file:<path>`
    it takes the answer from that file for every waiting file, starts no child and sends nothing, so an agent
    or a tester may run it; the three settings are still checked and the login folder is not.
11. **Output.** `--json` prints one object: `read` (files read in this run), `waiting` (files still to read)
    and, when a check stopped the run, `reason`.
12. **Accounting.** After each child returns, the run appends M-01's `document_tokens` line through 058's
    `stats.Append`.

## Where to read

| What | Where |
|---|---|
| Requirements | [G-11](../prd/lobe-graph.md), all of it; [F-06](../prd/lobe-foundation.md) step 7 and its first risk; [C-03](../prd/lobe-consolidate.md) steps 1, 3 and 6 and the `child.go` bullet; [G-06](../prd/lobe-graph.md) step 7 (ids) and the last "How to build it" bullet (untrusted text); [G-12](../prd/lobe-graph.md), `schema.go`; [R-02](../prd/lobe-recall.md) step 6; [F-01](../prd/lobe-foundation.md) step 10; [core page 5.3, 5.6 and 5.7](../prd/vertical-lobe.md) |
| Idea source: kinds | [`graphify/detect.py` L50](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/detect.py#L50): PDFs and images are told apart by extension; [L614](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/detect.py#L614): PDF text read page by page with no OCR, so a scanned PDF yields nothing |
| Idea source: the call | [`graphify/llm.py` L1950](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L1950): `-p --output-format json --no-session-persistence`; [L1972](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L1972): `--json-schema`, because newer CLIs otherwise answer in prose |
| Idea source: data, not instructions | [`graphify/llm.py` L534](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L534): file content is wrapped and declared data; [L853](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L853): the 5 MiB image limit |
| Not taken | [`graphify/llm.py` L1359](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L1359): split-and-retry once cost up to 15 billed calls for one bad answer; [L2763](https://github.com/Graphify-Labs/graphify/blob/6c8002eca4acad58db6160eabcb466e61fc35afb/graphify/llm.py#L2763): 4 calls at once; `graphify/cache.py` L1066: a prompt fingerprint in the cache path, so a prompt change re-bills every file |
| Existing code | `internal/lobe/consolidate/child.go` and `internal/cmd/lobe_consolidate.go` (025), `internal/lobe/pass` (018), `internal/lobe/graph/docs` (053), `internal/cmd/run.go` (`insideAgent`), `internal/cmd/reach.go` (the typed yes) |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: whether 025's launcher takes
   the tool list, the input file and the time limit as arguments; where the pass keeps its ordered list; how
   053 writes a `heading` node. Put to the owner the three readings under "Risks and unknowns". Files: none.
   Check: a written list the owner approves.
2. A throwaway check with a written result, for the question G-11 leaves open: can the headless child, given
   the `Read` tool only, read a file outside its own folder? The builder writes the script; the owner runs it
   once in one client they name, because it needs a real login and makes one model call. The result holds yes
   or no and the CLI version, never file content. If the answer is yes, stop and ask the owner before task 5.
3. `scan.go` and `ledger.go`: the scope, the hashes, `documents.json` under `store.Update`, the four moves of
   the owner's rule. Tests: `TestLobeDocumentsOffCallsNothing`, `TestLobeDocumentsNeverBilledTwice`,
   `TestLobeDocumentsSwitch`. Can run beside task 4.
4. `merge.go`, the plain file nodes, the rows in `schema.go`, and the quoted and labeled snippet in the
   renderers of 046 and 050. Tests: `TestLobeDocumentsSurviveGraphDelete`, `TestLobeDocumentsMergeShapes`,
   `TestLobeDocumentsNeverALesson`.
5. `ask.go` and `run.go`: the launcher call, the answer limits, the scrub, the cost guard. Tests:
   `TestLobeDocumentsChild`, `TestLobeDocumentsRunCap`, `TestLobeDocumentsAnswerLimits`. Needs tasks 2 and 3.
6. The command in `internal/cmd/lobe_documents.go`: the person check, `--answer`, `--all` with its typed yes,
   `--redo`, `--json`. Test: `TestLobeDocumentsPersonOnly`. Needs task 5.
7. The pass step and `last_run`. Test: `TestLobeDocumentsOneDetachedRunADay`. Can run beside task 6.
8. `document_tokens` through `stats.Append`. Test: a case added to `TestLobeStatsErrorCounters`.
9. Docs: the `documents` action in the `tentaqles lobe graph` section of `docs/reference/commands.md`, the
   `documents` key in `docs/reference/manifest.md` if 005 documented the `lobe` keys there, `CHANGELOG.md` under
   `[Unreleased]`, and one line under "Must not trade off" in `CLAUDE.md` for `TestLobeDocumentsOffCallsNothing`
   and `TestLobeDocumentsChild`. The plugin does not change.
10. The tester run of "Evaluation", by an agent that built none of this.

## Tests that stay in the repository

- `TestLobeDocumentsOffCallsNothing`: with any of the three settings `off`, 3 PDFs and a `stop` give 0 calls and
  no ledger. 3 of 3.
- `TestLobeDocumentsOnOffOn`: `on` with 3 files present gives 3 calls; `off` and a 4th file gives 0 calls and
  keeps the 3 results and their nodes; `on` again gives 1 call.
- `TestLobeDocumentsSwitch` (F-06's): 4 switches (on, off, on, off) over 3 files; the processed set matches the
  owner's rule each time, a changed file's old nodes are marked stale and a deleted file's nodes are gone.
- `TestLobeDocumentsNeverBilledTwice`: renaming, copying and touching a `done` file gives 0 calls.
- `TestLobeDocumentsRunCap`: 30 waiting files give exactly 10 calls in one automatic run.
- `TestLobeDocumentsOneDetachedRunADay`: after that run, a second `stop` on the same local day gives 0 calls and
  the first `stop` of the next day gives 10; a run by hand in between gives 10 and leaves `last_run` unchanged.
- `TestLobeDocumentsSurviveGraphDelete`: deleting `graph/` and rebuilding gives 0 calls and the same nodes;
  `tentaqles lobe rebuild` with `documents: off` keeps them too.
- `TestLobeDocumentsChild`: the child gets `TQ_LOBE_NO_HOOKS=1` and the `Read` tool only, and no `tentaqles run`
  process is started; with another client's `CLAUDE_CONFIG_DIR`, 0 children start and 0 files reach `tmp/`.
- `TestLobeDocumentsPersonOnly`: under `CLAUDECODE=1` the command without `--answer` exits non-zero and starts 0
  children; with `--answer` it runs.
- `TestLobeDocumentsNeverALesson`: after a run, 0 lesson files are new, in the store and in the repository, and
  no summary text occurs in a file outside `documents/` and `graph/`.
- `TestLobeDocumentsMergeShapes` (new here): a topic line has `"kind":"heading"`, then `"origin":"model"` and
  `summary` after `level`; one unique mention gives 1 edge with `"rule":"doc-model"` and score 0.75; a name two
  symbols share gives 0; the built graph passes the soundness check.
- `TestLobeDocumentsAnswerLimits` (new here): an answer with 40 topics, a 300-character name, a 900-character
  summary and a planted secret is stored as 30 topics, 80 and 200 characters, with the secret scrubbed; an
  answer that is not JSON gives `failed`, and a third failure stops further tries.
- House rules kept green: no model in a hook (the hook table's tests of F-02; this feature adds no hook step),
  `TestLobeNoHooksSwitch`, `TestLobeDraftHoldsNoLock` (the same launcher), `TestLobeLeak_AllAttacksZeroLeaks`
  with attack L-10's fake CLI now also run for a document call, `TestLobeGraphDeterministic`,
  `TestLobePersonVerbsRefuseInAgent`, `TestLobeCommandTable`, `TestDocsReferenceEveryCommand`, the import test
  of F-02, and every test under "Must not trade off".

## Evaluation

Tester brief. Start from the standard tester setup of [002](002-sandbox.md) (F-07), which sets `model` to `off`.
No network is used: every run below takes its answer from a file, so no child is started. Pipe no hook event in
this brief. `L` is `$TQ_HOME/identities/acme/lobe` and `D` is `L/documents.json`; "entries" are the keys under
`files` in `D`. Write `$W/ans.json` containing
`{"topics":[{"name":"Refund policy","summary":"Refunds within 30 days"}],"mentions":[]}`. In `$W/clients/acme`
run `git init shop` and create `shop/a.pdf`, `shop/b.pdf` and `shop/c.png` containing `%PDF-1.4 a`,
`%PDF-1.4 b` and `png c`. "Run" is `tentaqles lobe graph documents --json --answer file:$W/ans.json` in `shop`;
it prints one JSON object. The checks change settings with `lobeset`, the helper of the standard setup, one key
per call: a tester does not run `tentaqles lobe set`, which answers only to a person.

1. Run: exit 0, `"read":0`, and `D` does not exist. `lobeset acme documents on` and run: `"read":0`,
   `"reason":"model"`. `lobeset acme model on; lobeset acme graph off` and run: `"read":0`, `"reason":"graph"`.
   `D` still does not exist.
2. `lobeset acme graph on` and run: `"read":3`; `D` has 3 entries, each with `"status":"done"`; `L/documents/`
   has 3 files; the `graph.jsonl` under `L/graph/` has at least 3 lines containing `"name":"Refund policy"`,
   each also containing `"origin":"model"`.
3. Run again: `"read":0`. Copy `a.pdf` to `shop/a2.pdf` and run: `"read":0`, and one entry of `D` lists 2 paths.
4. `lobeset acme documents off`, create `shop/d.pdf` containing `%PDF-1.4 d`, run: `"read":0` and `D` still has
   3 entries. `lobeset acme documents on` and run: `"read":1` and `D` has 4 entries.
5. Delete `L/graph`, run `tentaqles lobe graph build` in `shop`, then run: `"read":0`; the new `graph.jsonl`
   under `L/graph/` has at least 4 lines containing `"name":"Refund policy"`.
6. `lobeset acme model off`, create `shop/e.pdf` containing `%PDF-1.4 e`, run: `"read":0` and `D` still has 4
   entries.
7. `lobeset acme model on`. Create `shop/n1.pdf` to `shop/n12.pdf` containing `%PDF-1.4 n1` to `%PDF-1.4 n12`.
   Run: `"read":10`, the limit of one run. Run again: `"read":3`, and `D` has 17 entries.
8. `CLAUDECODE=1 tentaqles lobe graph documents --json; echo "exit=$?"`, with no `--answer`: the last line is
   `exit=1`, a line says the command answers only to a person, and `D` still has 17 entries.
9. `grep -rl "Refunds within 30 days" "$TQ_HOME/identities" "$W/clients"` lists only files under
   `$TQ_HOME/identities/acme/lobe/documents/` and `$TQ_HOME/identities/acme/lobe/graph/`: no lesson, nothing in
   a repository, nothing of `globex`.
10. In `shop`, `tentaqles lobe graph find "refund policy"` exits 0 and prints a hit under a `.pdf` or `.png`
    path whose summary line is in quotation marks and carries the label for model-read text.
11. In `shop`, `tentaqles lobe graph check --json --strict` exits 0 with `"ok":true`.

**Person checks** (the owner, by hand in `shop`, in the shell prepared above, after the checks)

1. `tentaqles lobe set documents off --client acme`, answered with yes; create `shop/p.pdf` containing
   `%PDF-1.4 p`; the run of the brief prints `"read":0`. `tentaqles lobe set documents on --client acme`,
   answered with yes; the run prints `"read":1`.
2. Create `shop/q1.pdf` to `shop/q12.pdf` as in check 7. The run with `--all` added asks for a typed yes.
   Answered with no, `D` is unchanged. Answered with yes, it prints `"read":12`.
3. The one real read. In a client of their own with `documents`, `model` and `graph` on, in a repository that
   holds one small real PDF, run `tentaqles lobe graph documents --json` with no `--answer`. Expect
   `"read":1`, then a topic of that PDF from `tentaqles lobe graph find`. Run it again: `"read":0`.

Scoring: PASS needs 11 of 11. "Could not run" is a FAIL.

## Done when

- [ ] The written result of task 2 exists and says the child cannot read outside its folder, or the owner has
  decided what follows.
- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`).
- [ ] The tester brief passes, 11 of 11, and the owner has run the three person checks.
- [ ] The house rules hold: no model in a hook, `TestLobeDocumentsOffCallsNothing`, `TestLobeDocumentsChild`,
  `TestLobeDocumentsNeverALesson`, `TestLobeLeak_AllAttacksZeroLeaks`, the guard's tests untouched.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md` and the `CLAUDE.md` line are updated.
- [ ] The owner has seen the result described under Result, with the setting switched on, off and on again.

## Risks and unknowns

- Three readings in this file that the requirements do not spell out, for the owner at task 1. (a) "Marked
  stale" names no key: this file proposes `"stale":true` as the last key of the node, added to the soundness
  table. (b) A topic's `level` is not given: this file proposes 1. (c) A run scans the current repository, the
  session's for the detached run, as the graph build does; G-11 says "the client's repositories" and gives the
  ledger client-wide paths, so a run over every repository would also fit.
- Removing a deleted document's nodes while the switch is off is F-06's reading of decision D-6. The other
  reading keeps them until the switch is on again.
- The headless flags come from reading Trail-code and Graphify, not from a run. 025 confirmed its own; `Read`
  as the only tool is new here, and task 2 checks it.
- With no parser the lobe cannot count pages or tokens before a call; the 20-page limit is only an instruction.
- A topic summary is client text held in graph nodes and in `documents/`. It is scrubbed, but a name the
  scrubber does not know stays in. It never leaves the client's store, and `tentaqles drop` removes it.
- The automatic pace is 10 files a day (proposed), so a backlog of 500 documents takes 50 days unless a person
  runs the command with `--all`. The `waiting` count is the only place the backlog shows.
- A run by hand has no daily cap of its own: the person-only gate and the 10-file limit are what bound it.
- G-02's list of node keys names `origin` after `tag` and `text` and does not name `summary`. A topic has no
  `tag` or `text`, so the order here fits both entries; the list in G-02 should gain `summary`.
- All numbers of the cost guard and the answer limits are proposed.
