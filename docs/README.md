# Tentaqles documentation

Every page of the docs, by what you need. New here? Start with [Getting started](getting-started.md).

## Start

- [Getting started](getting-started.md): install, set up a first client, log in, and check that it works.

## Understand

- [How it works](how-it-works.md): how a folder becomes a client, what a client's terminal carries, git, Claude Code,
  the language and the browser profiles.
- [Threat model](threat-model.md): what tentaqles protects against, what it doesn't, and the tests that hold each
  promise.

## Look up

- [Commands](reference/commands.md): every command, its flags and examples.
- [Manifest and files](reference/manifest.md): every manifest key, the files under `~/.tentaqles`, the environment
  variables.
- [Providers](reference/providers.md): the catalog of CLIs tentaqles isolates, and how to add your own.
- [Claude Code hook](reference/claude-hook.md): what the plugin's hook checks, the reasons it blocks, and the
  session-start preamble.

## Fix

- [Troubleshooting](troubleshooting.md): common symptoms, and every `tentaqles doctor` finding with its fix.

## Maintain

- [Releasing](RELEASING.md): how a release is cut, published and pinned.

## Planned: the Vertical Lobe

A draft for review. Nothing on these pages is built yet.

- [The Vertical Lobe: product requirements](prd/vertical-lobe.md): a per-client memory, code graph and rule enforcer
  on top of the identity isolation. The promise, principles, architecture, build order and decisions.
- [Foundation](prd/lobe-foundation.md): the per-client store, the fail-open engine, hook wiring, sharing, safety,
  settings and commands.
- [Sense](prd/lobe-sense.md): capturing failures and successes from every tool, scrubbing and fingerprints.
- [Consolidate](prd/lobe-consolidate.md): turning events into lessons, the quality gate, fading, backfill and
  starter rules.
- [Graph](prd/lobe-graph.md): the code and knowledge graph, blast radius and optional documents.
- [Recall](prd/lobe-recall.md): search, what is injected and when, agent tools and stale lessons.
- [Enforce](prd/lobe-enforce.md): the rule ladder, silent trials, blocking and the check before a commit.
- [Benchmarks](prd/lobe-benchmarks.md): how every claim is measured.
- [Phase 0, question 2: which hook events and fields really arrive](prd/spikes/phase0-q2-claude-code-hooks.md): what two real runs confirmed, and what still rests on the hooks page.
- [Phase 0, question 3: do parallel appends tear a line?](prd/spikes/phase0-q3-log-append.md): the measured answer, from feature 003.

## Planned: the Vertical Lobe, feature by feature

The same work cut into 62 features that are built and judged one at a time. Nothing is built yet.

- [How the features are built, and the full list](features/README.md)
- [001: Resolver refuses a second folder with a client's name](features/001-resolver-second-folder.md)
- [002: Sandbox with two test clients](features/002-sandbox.md)
- [003: A failed shell command in Claude Code is logged](features/003-shell-failure-claude-code.md)
- [004: A failed call from any tool is logged](features/004-any-tool-failure.md)
- [063: A call Claude Code rejected before it ran is logged](features/063-rejected-calls.md)
- [005: See what was captured, and switch it off](features/005-status-and-capture-switch.md)
- [006: The same error gets the same fingerprint](features/006-error-fingerprints.md)
- [007: A repeated call is stored once](features/007-duplicate-filter.md)
- [008: Successes are recorded, minimally](features/008-successes.md)
- [009: A failure that looks like a success is caught](features/009-hidden-failures.md)
- [010: A failed shell command in Codex is logged](features/010-shell-failure-codex.md)
- [011: Old events expire](features/011-retention-and-sweep.md)
- [012: Hook speed and stall checks](features/012-hook-latency-and-stall.md)
- [013: Isolation leak suite](features/013-isolation-leak-suite.md)
- [014: A starter rule speaks before a command runs](features/014-starter-rule-before.md)
- [015: A starter rule gives the fix after a failure](features/015-starter-rule-after-failure.md)
- [016: Write a lesson by hand](features/016-lesson-by-hand.md)
- [017: A lesson is shown before the matching call](features/017-lesson-before-call.md)
- [018: A failure and its fix become a repair pair](features/018-repair-pairing.md)
- [019: A repair becomes a lesson](features/019-lesson-from-repair.md)
- [020: The lesson for this exact error](features/020-lesson-after-failure.md)
- [021: Benchmark harness and reporting rules](features/021-benchmark-harness.md)
- [022: Repeat-mistake benchmark](features/022-repeat-mistake-benchmark.md)
- [023: Every fire is recorded and judged](features/023-fires-and-verdicts.md)
- [024: A wrong note is taken back](features/024-automatic-demotion.md)
- [025: The client's own model words the lesson](features/025-model-drafting.md)
- [026: Session brief](features/026-session-brief.md)
- [027: A correction from the person becomes a lesson](features/027-correction-sign.md)
- [028: Undone edits and failed-then-passed checks](features/028-edit-and-check-signs.md)
- [029: Lessons in Brazilian Portuguese](features/029-lessons-in-portuguese.md)
- [030: Lessons strengthen and fade](features/030-reinforcement-and-fading.md)
- [031: Replacing a lesson](features/031-replacing-a-lesson.md)
- [032: Stale lessons](features/032-stale-lessons.md)
- [033: Share a lesson with teammates](features/033-share-a-lesson.md)
- [034: A teammate's lesson arrives](features/034-teammate-lesson-intake.md)
- [035: Backfill from past sessions](features/035-backfill.md)
- [036: Lessons and starter rules in Codex](features/036-lessons-in-codex.md)
- [037: Test cases on every rule](features/037-rule-test-cases.md)
- [038: Approve a rule to block](features/038-approve-to-block.md)
- [039: Sure-fail block](features/039-sure-fail-block.md)
- [040: Check before a commit](features/040-check-before-commit.md)
- [041: Enforcement accuracy benchmark](features/041-enforcement-accuracy.md)
- [042: Parser without cgo](features/042-parser-runtime.md)
- [043: The graph of one repository](features/043-graph-of-one-repository.md)
- [044: Graph soundness check](features/044-graph-soundness.md)
- [045: Who calls what](features/045-resolution-and-callers.md)
- [046: Find code by words](features/046-find-and-map.md)
- [047: Signatures and argument counts](features/047-signatures.md)
- [048: The graph stays current](features/048-graph-stays-current.md)
- [049: Blast radius](features/049-blast-radius.md)
- [050: Code pointers with a prompt](features/050-prompt-injection.md)
- [051: Argument check after an edit](features/051-argument-check.md)
- [052: SQL in the graph](features/052-sql-in-the-graph.md)
- [053: Documents and configuration in the graph](features/053-docs-and-config.md)
- [054: Why-comments](features/054-why-comments.md)
- [055: Stale lessons by symbol](features/055-stale-by-symbol.md)
- [056: One graph for many repositories](features/056-many-repositories.md)
- [057: Graph accuracy against Trail-code](features/057-graph-accuracy.md)
- [058: Token savings accounting](features/058-token-savings.md)
- [059: Token and correctness benchmark](features/059-token-benchmark.md)
- [060: The remaining languages](features/060-remaining-languages.md)
- [061: PDFs and images](features/061-pdfs-and-images.md)
- [062: The graph as MCP tools](features/062-mcp-server.md)
