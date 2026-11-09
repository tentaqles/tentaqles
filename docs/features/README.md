# The Vertical Lobe, feature by feature

The [product requirements](../prd/vertical-lobe.md) describe the Vertical Lobe by mechanism: 56 entries on seven
pages. This folder cuts the same work into 62 features that each run end to end. One feature is built, watched
and judged at a time. The next one starts only when the one before it has passed its gate.

Nothing here is built yet. Each file is the brief for the team that builds that feature.

## How to use these files

1. **One feature at a time, in number order.** The owner asks for one feature. A later feature never starts
   before the features it needs have passed their gates.
2. **The feature file is the brief.** It says what a person will see when the feature works, what is in and
   out, what it needs from earlier features, how it works, what to read, the tasks in order, the tests, the
   evaluation and the gate.
3. **The requirements pages hold the shared rules.** The store layout, the event and lesson shapes, the hook
   table and time limits, the ladder and the settings live in the core page (sections 5.1 to 5.7) and are not
   repeated here. A feature file names the sections and entries to read. Where a feature file and a
   requirements page disagree, stop and ask the owner; do not pick one.
4. **Check the brief before starting.** Earlier features teach things. Before the first task of a feature, its
   team reads the file against the code as it now stands and lists what no longer fits. The owner approves
   changes to the file before code is written.
5. **Done is enforced by code.** A feature is done when its tests are in the repository and run in CI, its
   tester brief passes, and the house rules below still hold. A review alone never closes a feature.
6. **A thin foundation is widened, not rebuilt.** The first feature that needs the store, the engine or a hook
   builds the smallest form that works. A later feature widens it only as far as it needs and says so in its
   file.

## House rules every feature keeps

These come from the core page's principles (section 3) and the repository's `CLAUDE.md`. Each is held by tests
that the first feature needing it adds, and every later feature keeps those tests green.

- **Isolation.** Everything is stored under `$TQ_HOME/identities/<client>/lobe`. A lobe hook acts only when the
  folder and the session's login belong to the same client (core page 5.1); otherwise it exits 0 with no output
  and no write.
- **The guard is untouched.** The identity guard fails closed; the lobe fails open and never says allow. Lobe
  code lives in `internal/lobe/...` and follows the four import rules of core page 5.1. Every test under "Must
  not trade off" in `CLAUDE.md` stays green and unweakened.
- **A hook never stalls.** Bounded work under a deadline, exit 0 with no output on any failure, everything slow
  in the detached pass (core page 5.2).
- **No secret on disk.** Text is scrubbed before it is written anywhere (S-05).
- **No model in a hook.** Only the two steps of core page 5.7 may call a model, and only from the detached pass
  or a command a person runs.
- **Sources.** Code may be ported from Trail-code, agentmemory and Graphify with their notices kept. BASE gives
  ideas only: no code, no identifiers. Source projects are read at the commits pinned in core page section 7.
- **Repository rules.** `CGO_ENABLED=0`. Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE` and never touch the
  real `~/.tentaqles`. Every new command gets a section in `docs/reference/commands.md`, and every change a
  line in `CHANGELOG.md` (and in `plugin/CHANGELOG.md` when the plugin changes).
- **Testers are not people.** A tester agent works in the sandbox (feature 002), never runs a person-only
  command, and lists such steps under "Person checks" for the owner.

## The features

"Entries" names the requirements entries a feature builds, in whole or in part; the feature file says which
part. Features 003 to 035 are built for Claude Code. Feature 010 brings Codex level with it for capture, 011
adds Codex's session-end sweep, and 036 brings everything the lobe says. From 037 on every feature covers
both hosts. "Needs" names the direct prerequisites; number order takes care of the rest.

### Ground

| # | Feature | What a person sees when it works | Entries | Needs |
|---|---|---|---|---|
| 001 | [Resolver refuses a second folder with a client's name](001-resolver-second-folder.md) | A folder that copies a client's name and marker under another clients folder resolves to no client | D-12, M-03 (attack L-09) | none |
| 002 | [Sandbox with two test clients](002-sandbox.md) | `tentaqles lobe sandbox <folder>` builds a throwaway home in which `acme` and `globex` resolve, and refuses a folder inside the real home | F-07 (standard setup), F-01 (paths) | 001 |

### Sense

| # | Feature | What a person sees when it works | Entries | Needs |
|---|---|---|---|---|
| 003 | [A failed shell command in Claude Code is logged](003-shell-failure-claude-code.md) | A failed Bash call in a client's session adds one scrubbed line to that client's event log, and nothing anywhere else | F-01, F-02, F-03, S-01, S-05, S-06 (event id), S-07, F-07 (notices, `selftest`); D-11, D-13 | 002 |
| 004 | [A failed call from any tool is logged](004-any-tool-failure.md) | A failed file edit, web fetch or MCP call lands in the same log | S-01 | 003 |
| 005 | [See what was captured, and switch it off](005-status-and-capture-switch.md) | `tentaqles lobe status` and `tentaqles lobe events` show the log; `tentaqles lobe set capture off` stops it; `tentaqles doctor` reports the lobe's health | F-06, F-07 | 004 |
| 006 | [The same error gets the same fingerprint](006-error-fingerprints.md) | Two failures that differ only in a path or a number carry one fingerprint in the log | S-08 | 004 |
| 007 | [A repeated call is stored once](007-duplicate-filter.md) | An agent that fails the same way twenty times adds one full line and short references | S-06 | 006 |
| 008 | [Successes are recorded, minimally](008-successes.md) | A successful call adds a small `ok` line with no output in it | S-03 | 004 |
| 009 | [A failure that looks like a success is caught](009-hidden-failures.md) | `npm test \| tail -5` that exits 0 with failing tests is logged as `hidden-fail` | S-04 | 006, 008 |
| 010 | [A failed shell command in Codex is logged](010-shell-failure-codex.md) | A failed shell call in a Codex session is logged as in 003 to 009, read from Codex's one post-tool event. A non-shell Codex tool is logged only when its result carries an error flag | S-02, F-03 (Codex) | 009 |
| 011 | [Old events expire](011-retention-and-sweep.md) | Day files older than `retention_days` are gone after a session ends, and a broken line never stops the log | S-07, F-06, F-03 (session end, both hosts) | 005, 010 |
| 012 | [Hook speed and stall checks](012-hook-latency-and-stall.md) | `tentaqles lobe bench latency` and `stall` print the measured time of each hook against its limit | M-06, M-02 (where the checks live) | 011 |
| 013 | [Isolation leak suite](013-isolation-leak-suite.md) | `tentaqles lobe bench leak` runs its attacks and reports zero leaks | M-03 | 001, 011, 012 |
| 014 | [A starter rule speaks before a command runs](014-starter-rule-before.md) | PowerShell syntax about to run in Git Bash gets one short note first | C-08, F-05 (the frame), F-06 | 010 |
| 015 | [A starter rule gives the fix after a failure](015-starter-rule-after-failure.md) | After a known error the agent reads the usual fix beside it | C-08 | 014 |

### Lessons

| # | Feature | What a person sees when it works | Entries | Needs |
|---|---|---|---|---|
| 016 | [Write a lesson by hand](016-lesson-by-hand.md) | `tentaqles lobe lessons add` files a Markdown lesson that passed the quality gate, or says which check failed | C-04, F-05 (shape and origin) | 005 |
| 017 | [A lesson is shown before the matching call](017-lesson-before-call.md) | The agent gets the lesson as one line just before a call its `when` block matches | R-04, E-01 (stages to advice), F-05 | 016, 014 |
| 018 | [A failure and its fix become a repair pair](018-repair-pairing.md) | After a turn, `tentaqles lobe consolidate` lists the failed call and the retry that worked | C-01, F-02 (detached pass), F-03 (`Stop`), S-08 (the fold) | 009, 011 |
| 019 | [A repair becomes a lesson](019-lesson-from-repair.md) | Fail, fix, and in the next session the note appears before the same mistake | C-03 (template), C-04, F-05 (quarantine), E-01 (enter rule) | 017, 018 |
| 020 | [The lesson for this exact error](020-lesson-after-failure.md) | Right after a failure with a known fingerprint, the lesson that lists it is shown | R-04 | 019, 015 |
| 021 | [Benchmark harness and reporting rules](021-benchmark-harness.md) | `lobebench` runs one task with and without the lobe and writes paired results; `check-claims` refuses a number with no receipt | M-02, M-08 | 013 |
| 022 | [Repeat-mistake benchmark](022-repeat-mistake-benchmark.md) | `lobebench repeat` reports the repeat rate at the second meeting, with and without lessons | M-04 | 019, 021 |
| 023 | [Every fire is recorded and judged](023-fires-and-verdicts.md) | `trials.jsonl` holds each time a rule matched and what the call then did | E-02 | 019 |
| 024 | [A wrong note is taken back](024-automatic-demotion.md) | One false alarm sends a lesson back to trial; `tentaqles lobe rules` shows why; a person can demote or retire | E-07, E-01 (person check, mute) | 023 |
| 025 | [The client's own model words the lesson](025-model-drafting.md) | With `model: on` the lesson is worded by the client's agent CLI; with it off the template still files one | C-03 | 019, 024 |
| 026 | [Session brief](026-session-brief.md) | A new session starts with a few lines: how many lessons, how many wait, and the standing ones | R-05, E-01 (confirm) | 024 |
| 027 | [A correction from the person becomes a lesson](027-correction-sign.md) | "No, use pnpm" typed to the agent yields a lesson that speaks at once | C-02 (corrections) | 025 |
| 028 | [Undone edits and failed-then-passed checks](028-edit-and-check-signs.md) | The two other signs of C-02 produce candidates | C-02 (other signs) | 027 |
| 029 | [Lessons in Brazilian Portuguese](029-lessons-in-portuguese.md) | A lesson a person writes in Portuguese passes the gate without `--anyway` | C-04; D-14 | 016 |
| 030 | [Lessons strengthen and fade](030-reinforcement-and-fading.md) | A lesson that keeps proving right grows stronger; an unused one fades and retires | C-05 | 024 |
| 031 | [Replacing a lesson](031-replacing-a-lesson.md) | A new lesson supersedes an old one; only the newest is shown, and the history stays | C-06, F-07 (`lessons forget`) | 030 |
| 032 | [Stale lessons](032-stale-lessons.md) | A lesson about a file that is gone is shown marked "may be stale" and after the fresh ones | R-06 (files) | 031 |
| 033 | [Share a lesson with teammates](033-share-a-lesson.md) | A lesson is filed in the repository's `.tentaqles/lessons/` and travels through git | F-04 (filing) | 031 |
| 034 | [A teammate's lesson arrives](034-teammate-lesson-intake.md) | A pulled lesson that passes intake speaks as a note; one that fails is held back | F-04 (intake), F-05 (quarantine, tamper) | 033 |
| 035 | [Backfill from past sessions](035-backfill.md) | `tentaqles lobe backfill` mines earlier transcripts; mined lessons start silent and earn their way up | C-07, E-01 (trial to advice) | 028, 024 |
| 036 | [Lessons and starter rules in Codex](036-lessons-in-codex.md) | What the lobe says in Claude Code (014 to 035) also reaches a Codex session, as far as Codex's events allow; the file lists what Codex does not get | F-03, R-04, R-05, C-02, C-07, C-08, S-02 on Codex | 035 |

### Blocking

| # | Feature | What a person sees when it works | Entries | Needs |
|---|---|---|---|---|
| 037 | [Test cases on every rule](037-rule-test-cases.md) | `tentaqles lobe rules test` runs each rule's own examples; a rule that fails them cannot warn by pattern | E-03, E-07 (failed tests) | 036 |
| 038 | [Approve a rule to block](038-approve-to-block.md) | After a typed yes, the matching call is denied with the reason and the one-call override | E-01 (block step), E-07 (block rows) | 037 |
| 039 | [Sure-fail block](039-sure-fail-block.md) | With `enforce: block`, a command that provably cannot run here is denied with the fix | E-04 | 038 |
| 040 | [Check before a commit](040-check-before-commit.md) | Before `git commit` the agent is told which lesson the staged change goes against | E-06 | 038 |
| 041 | [Enforcement accuracy benchmark](041-enforcement-accuracy.md) | A labeled set replayed through the hook reports wrong blocks and misses | M-05 | 039, 040 |

### Graph

| # | Feature | What a person sees when it works | Entries | Needs |
|---|---|---|---|---|
| 042 | [Parser without cgo](042-parser-runtime.md) | A written result names the parser library, with speed and size measured | G-01 | none |
| 043 | [The graph of one repository](043-graph-of-one-repository.md) | `tentaqles lobe graph build` writes symbols; `tentaqles lobe graph outline <file>` lists them | G-02, R-03 (outline), F-06 (`graph`) | 042, 005 |
| 044 | [Graph soundness check](044-graph-soundness.md) | `tentaqles lobe graph check` reports that the built graph agrees with itself | G-12 | 043 |
| 045 | [Who calls what](045-resolution-and-callers.md) | `tentaqles lobe graph callers <symbol>` lists callers across files, each with its confidence | G-03, R-03 (callers) | 044 |
| 046 | [Find code by words](046-find-and-map.md) | `tentaqles lobe graph find` returns a short ranked list; `map` shows folders and counts | R-01, R-03 (find, map) | 045 |
| 047 | [Signatures and argument counts](047-signatures.md) | Each function in the graph shows how many arguments it takes | G-04, G-03 (overloads) | 045 |
| 048 | [The graph stays current](048-graph-stays-current.md) | After the agent edits a file, the graph matches the code again after the turn | G-08 | 046, 018, 010 |
| 049 | [Blast radius](049-blast-radius.md) | `tentaqles lobe graph impact` and a note after an edit list what depends on the change | G-09, R-03 (impact), R-05 (the brief's graph sentence) | 048 |
| 050 | [Code pointers with a prompt](050-prompt-injection.md) | A prompt gets up to 2 lessons and up to 3 code pointers; most prompts get nothing | R-02 | 049 |
| 051 | [Argument check after an edit](051-argument-check.md) | A call written with an impossible number of arguments gets one line showing the signature | E-05 | 047, 048 |
| 052 | [SQL in the graph](052-sql-in-the-graph.md) | Tables, views and routines are nodes, and `impact` shows what reads and writes a table | G-05 | 045 |
| 053 | [Documents and configuration in the graph](053-docs-and-config.md) | Markdown headings, configuration keys and Terraform blocks are nodes | G-06 | 045 |
| 054 | [Why-comments](054-why-comments.md) | A `WHY:` comment becomes a note that is shown with its function after an edit | G-07 | 048 |
| 055 | [Stale lessons by symbol](055-stale-by-symbol.md) | A lesson about `file#Symbol` is marked stale when the symbol is gone | R-06 (symbols) | 048, 032 |
| 056 | [One graph for many repositories](056-many-repositories.md) | A client's repositories are linked; `--all` crosses them and no other client's | G-10 | 049 |
| 057 | [Graph accuracy against Trail-code](057-graph-accuracy.md) | Our graph and Trail-code's are compared node by node, each difference with a verdict | M-07 | 047, 021 |
| 058 | [Token savings accounting](058-token-savings.md) | `tentaqles lobe status --savings` shows estimates, marked as estimates | M-01 | 050 |
| 059 | [Token and correctness benchmark](059-token-benchmark.md) | The question and pull-request sets run with and without the graph; the paid public run waits for the owner | M-02 (real runs), M-08; D-15 | 058, 022 |
| 060 | [The remaining languages](060-remaining-languages.md) | Each further language arrives with its query file and passes its fixtures | G-02 | 057 |
| 061 | [PDFs and images](061-pdfs-and-images.md) | With `documents: on`, PDFs and images become topic nodes, read once | G-11 | 053, 025 |
| 062 | [The graph as MCP tools](062-mcp-server.md) | The five graph commands are also offered as MCP tools, off by default | R-03 (MCP) | 056 |

## When a stage ships

Features are built and judged one at a time, but a release is cut only at the end of a stage, with that stage's
gates green and the leak suite (013) at zero leaks. Inside a stage some protections arrive after the thing they
protect, on purpose, so that each feature stays small:

- Lessons are first shown in 017 and first learned in 019. Fires are recorded in 023, a wrong note is taken back
  in 024, and a session that touched lobe files stops teaching in 033 and 034. So no build from inside the
  Lessons stage is released: the stage ships after 036 (author's call A-14 in the core page).
- A rule can block from 038, and the accuracy of blocking is measured in 041. The Blocking stage ships after
  041, and `enforce: block` is not offered in a release before that.
- The Sense stage ships after 015 and the Graph stage after 059. Features 060 to 062 ship one by one.

## Parts of the requirements no feature takes yet

The writers of these files found parts of the requirements that belong to no feature. They are listed here so
that none is lost. Each needs a home, or a decision to leave it out, before the stage it belongs to ships.

- **Sense.** The Drain measurement of S-08 (200 labeled failures, phase 2). An expiry rule for
  `sessions/<session>.json`: the requirements have none.
- **Lessons.** Evidence (c) of C-02 (a declined or interrupted call) and an edit reversed by hand. Portuguese
  lists for F-05's shape check. A reason in E-07's table for a ladder entry whose lesson file is gone.
- **Blocking.** Approving a starter rule that is not sure-fail (C-08 step 8 gives no approval record for it).
  The noise numbers E-06 expects for the commit check and the rejection rates F-05 expects for its shape
  lists, both of which point at M-05, which has no step for them.
- **Graph.** `params` for Rust, C, C++, Ruby, PHP and Kotlin. Pruning of the parse cache. Ranking per scope
  and a progress line for `--all`. Using table names from G-05 to recall a lesson before SQL runs. A list
  after an edit to a `.sql`, Markdown or configuration file. A step that confirms R-01's ranking constants.
- **Benchmarks.** The Codex adapter of the harness (M-02 step 8, author's call A-26). The wrong-argument-count
  family and arm `all` of the repeat-mistake benchmark, which the requirements place in the Graph stage.

## What every feature file contains

Every file uses these headings, in this order.

- **Header.** `# NNN: Title`, then one line: status (`not started`), stage, requirements entries, needs, feeds.
- **Result.** What a person sees when it works, and the commands that show it.
- **Scope.** What is in. What is out, and which feature takes each thing left out.
- **What it builds on.** Packages, functions, files and shapes from earlier features, by feature number.
- **What it leaves for later features.** The names later features will rely on.
- **How it works.** The mechanism of this slice, step by step. Shared rules are cited, not repeated.
- **Where to read.** The requirements entries and steps to read, and the source code links this slice uses.
- **Build tasks.** Ordered tasks, each sized for one agent, each naming its files and the test that proves it.
- **Tests that stay in the repository.** Test names and what each asserts, including the house-rule tests.
- **Evaluation.** The tester brief: exact inputs, expected outputs, and "Person checks" for the owner.
- **Done when.** The gate, as a checklist.
- **Risks and unknowns.** Facts to verify first, and what could force a change to this file.
