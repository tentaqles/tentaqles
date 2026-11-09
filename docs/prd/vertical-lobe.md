# The Vertical Lobe: product requirements

Status: draft for the owner's review, 2026-11-09. Nothing in this document is built yet.

The vertical lobe is the part of an octopus's brain that gathers what the animal senses and turns it into long-term
memory. This document specifies the same thing for tentaqles: a memory that belongs to one client, learns from every
mistake an AI agent makes there, keeps a map of that client's code, and stops the agent from repeating a mistake it
has already made. The identity isolation tentaqles has today stays as it is and becomes the ground the memory
stands on.

This page holds the whole picture: the promise, the rules, the architecture and the build order. Each feature has its
own entry in one of seven companion pages:

| Page | Features |
|---|---|
| [Foundation](lobe-foundation.md) | F-01 to F-07: the per-client store, the fail-open engine, hook wiring, sharing, safety, settings, commands |
| [Sense](lobe-sense.md) | S-01 to S-08: capturing failures and successes from every tool, scrubbing, fingerprints |
| [Consolidate](lobe-consolidate.md) | C-01 to C-08: turning events into lessons, the quality gate, fading, backfill, starter rules |
| [Graph](lobe-graph.md) | G-01 to G-12: the code and knowledge graph, blast radius, optional documents |
| [Recall](lobe-recall.md) | R-01 to R-06: search, what is injected and when, agent tools, stale lessons |
| [Enforce](lobe-enforce.md) | E-01 to E-07: the rule ladder, silent trials, blocking, checks before a commit |
| [Benchmarks](lobe-benchmarks.md) | M-01 to M-08: how every claim is measured |

## 1. The promise

A person who installs tentaqles notices three things in the first week.

1. **One identity per client, enforced.** They stop signing in and out of services. Code, messages and commits never
   reach the wrong client's service, even when two clients use the same provider. This exists today.
2. **Fewer tokens.** Their agent finds code, data and documents through a graph instead of reading files one by one.
3. **Fewer errors.** A mistake an agent makes in a client's work is learned there and not made twice: the lesson
   speaks up the next time the mistake is about to happen, which also saves tokens (the rule ladder, 5.5).

It is for anyone who works with an AI coding agent: software, AI, data, frontend, backend and full-stack engineers,
data scientists, analysts, and UI and UX designers. It must be worth installing for a person with one client or one
employer, and it must stay safe for a person with many.

## 2. Goals, non-goals and anti-goals

**Goals**

- Capture every failed tool call from Claude Code and the Codex CLI: shell commands, SQL, file edits, API and MCP
  tools.
- Turn "this failed, then this worked" into a lesson that names the pattern to avoid and the reason.
- Bring a lesson back at the moment it applies, in a few hundred characters, not at session start in bulk.
- Block a call only when a rule is exact, has proved itself, and the person said yes; or when a built-in rule can
  prove the command cannot run on this machine and the person has switched blocking on.
- Keep a graph of the client's code, SQL, configuration and documents that answers "where is this" and "what breaks
  if I change this" without the agent reading whole files.
- Keep every client's memory, graph and rules apart, with the same strength as its logins.
- Back each claim in section 1 with a benchmark anyone can rerun.

**Non-goals**

- A hosted service, an account, or a sync server. Sharing happens through the client's own git repositories.
- Project, task or milestone tracking.
- Coordinating several agents.
- A general chat memory ("the user likes tabs"). The lobe remembers how work succeeds and fails in a client's systems.
- Supporting agents other than Claude Code and Codex in the first releases.

**Anti-goals: outcomes that make this a failure even if it runs**

- A lesson, graph node or login of one client becomes visible in another client's session. The target is zero, ever.
- A hook stalls or slows a session noticeably. A slow memory is worse than no memory.
- A wrongly learned rule blocks correct work and the person cannot see why or undo it in one step.
- Client data (rows, names, secrets) ends up stored in a lesson or sent anywhere.
- Text planted in a tool result or document becomes a rule the agent obeys.
- The identity guard becomes weaker because memory code shares its path.

## 3. Principles

Every feature entry is judged against these. A feature that needs one bent says so and needs the owner's yes.

1. **Isolation comes first.** Everything the lobe stores sits under the client's identity folder, and the client is
   resolved by the code tentaqles already uses. No lobe data is keyed by anything a second client could share.
2. **The guard fails closed. The lobe fails open.** They are two engines started as two processes. When the lobe is
   slow, confused or broken, the tool call goes ahead as if the lobe were not installed.
3. **The lobe never says "allow".** A lobe hook prints nothing, prints context, or denies. It cannot override the
   guard, a permission prompt or another hook.
4. **Hooks never stall.** A hook does bounded work under a deadline and hands everything slow to a detached process.
5. **A lesson is information, never an instruction.** Lessons are framed as what was observed here before. Anything
   that came from tool output is quarantined until it passes checks, and nothing learned can block until a person
   approves it.
6. **Patterns, not data.** A lesson holds the shape of what failed and why. Raw events are scrubbed on capture and
   expire. Nothing a client would call their data is kept in a lesson.
7. **Nothing leaves the machine.** No telemetry, no vendor cloud. The only network use is the client's own agent CLI,
   under that client's own login, when the person has left model drafting switched on.
8. **Plain files a person can read.** Lessons are Markdown. Logs are JSON lines. Indexes are caches that can be
   deleted and rebuilt.
9. **Honest numbers.** Each claim has a benchmark with the harness and raw results published. We report the average
   with its interval, never "up to".

## 4. How it works

```
        tool call fails or succeeds            a prompt, or a tool about to run
                    |                                        |
                 SENSE                                    RECALL
   scrub, drop duplicates, fingerprint       match lessons and code to this moment,
   append one line to the client's log       inject a few hundred characters
                    |                                        |
              CONSOLIDATE  (after the turn, detached)     ENFORCE
   pair a failure with the retry that worked,    seen -> trial -> advice -> block
   draft a lesson, check it, file it             deny only on an exact, approved rule
                    |                                        |
                    +------------  per-client store  --------+
                                         |
                                       GRAPH
              code, SQL, config and documents; who calls what; what breaks
```

- **Sense** records what happened. It is the only stage that runs on every tool call, so it does the least.
- **Consolidate** runs after a turn ends, in a detached process. It decides what was worth learning.
- **Recall** runs before a prompt is answered and before a tool runs. It is where tokens are saved.
- **Enforce** is recall with teeth. Most rules never get past advice.
- **Graph** feeds recall (where is the code) and enforcement (does this call match the function's signature).

Each kind of mistake has a path:

| Mistake | How it is caught |
|---|---|
| PowerShell syntax sent to Git Bash, or the reverse | A starter rule (C-08) recognizes it before it runs |
| A SQL pattern this client's engine rejects | A lesson learned from the first failure (C-01, C-03), recalled when SQL is about to run there (R-04) |
| Wrong number of arguments to a function in the repo | The graph knows the signature (G-04, E-05); no learning needed |
| Wrong use of an outside API or MCP tool | Learned from the failure, keyed by the tool and the error fingerprint (S-08) |

## 5. Architecture

### 5.1 One program, two engines

tentaqles stays one Go program built without cgo for Linux, macOS and Windows on amd64 and arm64.

| | Identity guard (today) | Lobe (this document) |
|---|---|---|
| Entry points | `tentaqles claude-hook`, `tentaqles codex-hook` | `tentaqles lobe-hook <host> <event>`, `tentaqles lobe <verb>` |
| On error, panic or timeout | Deny | Exit 0 with no output |
| Can deny a tool | Yes, for identity drift | Yes, only when `enforce` is `block`: for a block rule a person approved (E-01), or a built-in sure-fail rule (E-04) |
| Can allow a tool | No (prints nothing) | No (prints nothing) |
| Packages | `internal/guard`, `internal/cmd/claudehook.go`, `internal/cmd/codexhook.go` | new `internal/lobe/...` |

Four import rules, each kept by a test (F-02):

- The guard's files (`internal/guard` and the two hook files in `internal/cmd`) never import a lobe package.
- A lobe package may import `internal/resolve`, `internal/paths`, `internal/registry` and `internal/manifest`, and
  nothing else of tentaqles. It never imports `internal/guard` or `internal/cmd`. Where it needs something the guard
  has, such as splitting a shell command into words, it has its own copy (`internal/lobe/shellwords`) tested against
  the same cases.
- A lobe package has no `init` function and no package-level call, because it is linked into the same program as the
  guard and must not run before the guard decides.
- Verbs that need helpers private to `internal/cmd` (for example "is a person typing this") live in
  `internal/cmd/lobe_*.go` and call into the lobe packages.

**Which client a lobe hook serves.** The hook acts only when two things agree: the folder in the hook's JSON resolves
to a client, and the session's login folder (`CLAUDE_CONFIG_DIR` for Claude Code, `CODEX_HOME` for Codex) is that
same client's identity folder. When they differ, or the login variable is unset, the hook exits 0 with no output and
writes nothing in any store (F-02, tested by M-03).

### 5.2 Hooks

The lobe adds its own hook entries beside the guard's. Each entry starts its own process.

| Event (Claude Code) | Lobe work, all under a deadline | Features | Class |
|---|---|---|---|
| SessionStart | Print a short brief: counts, and lessons that apply to all work here | R-05 | inject |
| UserPromptSubmit | Inject matching code pointers and lessons; note a correction | R-02, C-02 | inject |
| PreToolUse, every tool | Note the lessons and starter rules for this tool and command; deny on an approved block rule or a sure-fail rule; warn before a commit | R-04, C-08, E-01, E-04, E-06 | inject |
| PostToolUse, every tool | Record the success; detect a hidden failure; judge the rules that fired on this call; mark edited files for the graph; after an edit, show blast radius and check argument counts against the graph; after a hidden failure, answer it as PostToolUseFailure does | S-03, S-04, E-02, G-08, G-09, E-05, C-08, R-04 | capture, then inject |
| PostToolUseFailure, every tool | Record the failure; judge the rules that fired on this call; show a starter rule's fix and the lesson already learned for this exact error, in one answer | S-01, E-02, C-08, R-04 | capture, then inject |
| Stop | Start one detached pass, in this order: sweep expired events, fold fingerprints, consolidation, ladder moves, graph build, then the optional document run | S-07, S-08, C-01 to C-06, E-07, G-08, G-11 | capture |
| SessionEnd | Sweep expired events; nothing is buffered, so there is nothing to flush | S-07 | capture |

Codex has no separate failure event; a failed shell call arrives on its post-tool event (S-02). Which event carries
a failed MCP call in Claude Code, and whether Codex exposes an exit code, are not documented. Both are settled by a
live capture before Sense is built (phase 0).

**Time budgets (proposed; M-06 holds them).**

- **Capture** work (appending the event line) finishes in 150 ms at the 95th percentile and is cut off at 1 second.
- **Inject** work (anything that prints context or a deny) aims at 300 ms at the 95th percentile and is cut off at
  1.5 seconds, after which the call goes ahead as if the lobe were not installed.
- On the two post-tool events the event line is appended first and measured against the capture budget; the whole
  process aims at the inject target and is cut off at 1.5 seconds.
- The registered timeout is 5 seconds, or the host's own cap where that is lower (Codex is reported to cap its
  session-end hooks at 3 seconds; phase 0 confirms).
- The guard's own deadlines (6 seconds to decide, 15 seconds registered) do not change.

### 5.3 Where things live

Everything private to a person sits under the client's identity folder, so `tentaqles drop` removes it with the rest
of the client and no second client can reach it.

```
$TQ_HOME/identities/<client>/lobe/
  store.json                     which client this store belongs to, and its layout version (F-01)
  lock                           one writer at a time (F-01)
  consolidate.lock               one detached pass at a time (C-01)
  events/events-YYYYMMDD.jsonl   scrubbed tool events, append-only, expire after 14 days (S-07)
  events/drops-YYYYMMDD.jsonl    a count of events that could not be written, never their content (S-07)
  fingerprints.json              one entry per error pattern (S-08)
  sessions/<session>.json        what this session was already shown or asked, and its open rule fires
  lessons/<id>.md                private lessons (C-04)
  lessons/quarantine/            drafts waiting for the gate and lessons it refused; never shown (F-05)
  intake.json                    the verdict on each shared lesson that arrived through git (F-04)
  candidates.jsonl               repairs and corrections waiting to become lessons (C-01, C-02, C-03)
  shared-strength.json           this machine's strength record for shared lessons (C-05)
  backfill.json                  how far the day-one backfill got (C-07)
  ladder.json                    each lesson's stage and record on this machine (E-01)
  approvals.json                 the exact pattern and words a person approved for each block rule (E-01)
  ladder-log.jsonl               every move up or down the ladder, with its reason (E-07)
  trials.jsonl                   every time a rule fired, silently or not, and what happened next (E-02)
  graph/<repo-key>/              one repository's graph, parse cache, search index and build state (G-02, G-08, R-01)
  graph/links.json               links between a client's repositories (G-10)
  documents.json                 which PDFs and images were processed, by content hash (G-11)
  documents/<hash>.json          what was read out of each one (G-11)
  index/                         caches, safe to delete: lesson headers, compiled grammars (F-01, R-04, G-01)
  stats/stats-YYYYMMDD.jsonl     token and error counters (M-01)
  stats/hook-errors.jsonl        why a lobe hook gave up, never tool content (F-02)
  tmp/                           scratch for the detached pass (C-03)
```

`<repo-key>` is `r_` plus 8 hex characters, derived from the repository's root folder; F-01 gives the formula. It is
the same value as `repo` in an event. A linked git worktree takes the key of its main repository, so it shares that
repository's lessons and parse cache; only its graph is kept apart, in a subfolder (G-08).

Shared lessons live in the repository they are about, at `<repo>/.tentaqles/lessons/<id>.md`, and travel through
that repository's own git history (F-04). Nothing else is written inside a repository.

Files are the source of truth. `index/` and `graph/` are caches: deleting them only costs a rebuild. That is why
what was read out of documents sits in `documents/`, outside `graph/`: it cost model calls and must survive a
rebuild and a later `documents: off`. Whether the caches use plain files or an embedded pure-Go database is decided
by a spike (F-01).

### 5.4 Shapes

**Event**, one JSON object per line:

```json
{"v":1,"id":"evt_9f2c1a7b","at":"2026-11-09T14:03:11Z","session":"abc","host":"claude","tool":"Bash",
 "kind":"fail","repo":"r_51ac07d2","cwd":"etl","input":"<scrubbed, at most 4000 characters>",
 "error":"<scrubbed, at most 4000 characters>","exit":1,"fp":"fp_3e81b2c4d5a6f709"}
```

- `kind` is `fail`, `ok` or `hidden-fail`.
- `id` is unique within a session only. Anything that points at an event uses `session` and `id` together.
- `repo` is the repository key (5.3) and `cwd` is relative to the repository. Outside a repository `repo` is left
  out.
- `error` holds the error text without the host's "Exit code N" line; the number is in `exit`.
- A successful call drops its output and keeps a short projection of its input (S-03). The one exception is the
  success that follows a failure of the same tool in the same session: it keeps its scrubbed input in full, because
  it may be the repair (C-01). It expires with the rest of the log.
- `dup` is added by the duplicate filter when a line stands for repeats of an earlier one (S-06).

**Fingerprint**: `{id, tool_class, template, count, sessions, first_seen, last_seen}`. The template is the error
text with paths, numbers, identifiers and quoted values masked (S-08).

**Lesson**, a Markdown file with a YAML header:

```markdown
---
id: l-4c7d19e2
title: Use MERGE with a source alias on this warehouse
kind: lesson            # lesson | decision
source: repair          # repair | correction | backfill | person
scope: private          # private | shared
applies: client         # client | repo | path
repo: r_51ac07d2        # present when applies is repo or path, or when touches is not empty
origin:                 # who and what produced it (F-05)
  by: me                # me | team (a lesson that arrived through git)
  host: claude
  drafted: model        # model | template | person
when:                   # what makes this lesson relevant (R-04); see the rules below
  tool: [Bash, mcp__warehouse__query]
  command: [databricks, dbsql]
  place: [etl/]
  words: [merge into]
  fingerprint: [fp_3e81b2c4d5a6f709]
avoid:                  # optional; only a lesson with this block can ever reach "block" (E-01)
  pattern: '(?i)merge\s+into\s+\S+\s+using\s+\('
  must_match: ["MERGE INTO t USING (SELECT 1) ON ..."]
  must_not_match: ["MERGE INTO t USING src AS s ON ..."]
touches: [etl/load.py#upsert]
strength: 1.0
seen: 3
created: 2026-11-09
last_seen: 2026-11-09
last_faded: 2026-11-09
supersedes: l-1b22aa90
---
## Do
## Avoid
## Why
```

How a `when` block matches (R-04 owns the matcher):

- Inside one key, any listed value is enough.
- `tool`, `command` and `place` narrow each other: a call matches when every one of those keys that is present
  matches. `command` is read only for shell tools. A `when` block whose only key is a shell tool would match every
  shell call, so the gate refuses it (C-04).
- `fingerprint` matches on its own, when the failure just recorded has that fingerprint.
- `words` matches on its own, and only against a prompt (R-02).
- `applies` limits where any of this counts: `client` means everywhere in the client; `repo` means only in the
  repository named by `repo`; `path` means only in that repository and under a `place` entry, which must be present.

`kind` is `lesson` (something to do or avoid) or `decision` (a choice a person made and the reason, added by hand).
A decision has no `avoid` block, so it can inform and never block.

A lesson id is `l-` plus 8 hex characters, lengthened up to 12 when two would collide; every reader accepts 8 to 12.
Where the lesson's wording came from is recorded in `origin`, not in `source`. F-05 owns the `origin` block and C-04
owns every other key; where this example and those entries differ in detail, the entries win. A shared lesson
carries no `repo`: its repository is the one its file sits in. Starter rules (C-08) are not lesson files; they are a
catalog inside the program, and a file that claims to be one is refused.

**Ladder entry** (local to this person and machine): `{lesson, stage, hits, false_alarms, sessions, approved_at}`.
Stages are `seen`, `trial`, `advice`, `block` and `retired`. What a person approved for a block rule (the exact
pattern, the `when` block and the words a deny prints) is kept in `approvals.json`, so a later edit to the lesson
file, by hand or through git, changes neither what is blocked nor what is said; an edit to the pattern or the
`when` block ends the approval.

### 5.5 The rule ladder

A lesson climbs one step at a time and falls back as soon as it proves wrong.

| Stage | What the agent experiences | How a lesson gets here |
|---|---|---|
| seen | Nothing | It was mined from old sessions (C-07), passed the quality gate (C-04) and has no `when` block |
| trial | Nothing. Each time it would have fired is logged with what happened next (E-02) | It was mined from old sessions and has a `when` block, or a false alarm sent it back from advice (E-07) |
| advice | A short note before the matching call, or a line in the session brief when it has no `when` block | It was learned here from a failure and its fix or from the person's correction; or a person wrote or confirmed it; or it came from a teammate and passed intake (F-04); or its trial record is clean |
| block | The call is denied with the lesson's reason and how to override once | It has an exact `avoid` pattern, a clean advice record, and the person typed yes |
| retired | Nothing | It faded, was replaced, or proved wrong |

A lesson that arrives through git from a teammate and passes intake (F-04) speaks on arrival, as a note (D-10). It
enters your ladder at advice with no record and no approval, and never above; what its file says about who wrote it
earns it nothing more. Blocking is always a decision made by the person whose agent is being blocked, and a block
rule denies only where the lesson's `when` block also matches. The thresholds are in E-01.

Three notes on the ladder:

- **A new lesson speaks at once, as a note (D-9).** A lesson learned here from a failure and the fix that worked,
  or from the person's own correction, enters at `advice` and is shown from the next matching call. A note is
  information: the first false alarm sends the lesson back to `trial`, and a second one retires it (E-07). The
  silent trial is kept for lessons mined from old sessions (C-07), which rest on weaker evidence, and for a lesson
  a false alarm sent back. The step to `block` still needs a clean record at `advice` and a typed yes. The session
  brief says how many lessons wait for the person and how to confirm them (R-05).
- **A stale lesson** (the code it points at changed or is gone, R-06) is still shown, marked as possibly out of
  date and ranked after fresh ones. It never denies while it is stale; its approval is kept, and it denies again
  once it is no longer stale.
- **Build order.** The ladder up to `advice` ships with the first lesson that is shown (phase 2). The step from
  `advice` to `block` ships in phase 3 and is refused until then.

### 5.6 Settings

Settings are per client, in the client's manifest under a `lobe` key (F-06).

| Key | Values | Default | Meaning |
|---|---|---|---|
| `capture` | on, off | on | Record tool events. Off: nothing new is recorded or learned |
| `recall` | on, off | on | The master switch for everything the lobe prints into a session: the brief, code pointers, lessons, blast radius. Off: the lobe is silent, except a deny (an approved block rule, or a sure-fail rule) when `enforce` is `block` |
| `lessons` | on, off | on | Learn and use lessons. Off: nothing is drafted, shown or enforced; the graph still serves |
| `graph` | on, off | on | Build and use the code graph. Off: no graph is built; lessons still work |
| `enforce` | off, advice, block | advice | The most a learned rule may do. Off: no rule reaches the agent in any form and no trial is recorded. Advice: notes only. Block: approved rules may deny |
| `share` | private, shared | private | Where a new lesson is filed |
| `model` | on, off | on | Allow the two model steps in 5.7 to call the client's own agent CLI |
| `documents` | on, off | off | Process PDFs and images into the graph. Needs `model` and `graph` on |
| `retention_days` | number | 14 | How long raw events are kept |

A lesson is shown only when `recall` and `lessons` are on and `enforce` is not `off`. Starter rules count as
lessons for these switches. `documents` follows the
owner's rule: switching it on later processes the files already there; switching it off later keeps what was
processed and skips new files (G-11).

### 5.7 Model use

Sense, recall and enforcement never call a model, and no hook ever does. Two steps may:

1. **Wording a lesson (C-03).** Consolidation is deterministic up to the point of wording a general lesson. With
   `model: off` the lobe still pairs repairs and files lessons in a plainer, template form.
2. **Reading a PDF or image (G-11)**, only when `documents` is on.

Both call the client's own agent CLI in headless mode, under the client's own login folder, which is checked before
anything is sent. Both run from the detached pass, or in the foreground when a person asks for them by command.
The child session runs with `TQ_LOBE_NO_HOOKS=1`, so the lobe cannot feed on its own output. Each step has its own
daily cap (C-03 and G-11 propose the numbers).

## 6. Feature index

Origin says how much comes from elsewhere: **port** (rebuild the same logic in Go, license allows it), **idea** (the
concept only), **new** (ours). Phase is the build order in section 8.

| ID | Feature | Origin | Phase |
|---|---|---|---|
| F-01 | Per-client store | new, on tentaqles' own pattern | 1 |
| F-02 | Fail-open memory engine | new | 1 |
| F-03 | Hook wiring for Claude Code and Codex | new, on tentaqles' own pattern | 1 |
| F-04 | Private and shared lessons | port: Trail-code | 2 |
| F-05 | Lessons are data: provenance, quarantine, tamper distrust | port: Trail-code; research | 1 |
| F-06 | Settings | new | 1 |
| F-07 | Commands and doctor checks | new | 1 |
| S-01 | Capture failures from every tool | port: agentmemory | 1 |
| S-02 | Capture failures from Codex | port: Trail-code | 1 |
| S-03 | Record successes, minimally | port: agentmemory | 1 |
| S-04 | Detect failures that look like successes | port: Trail-code | 1 |
| S-05 | Secret scrubbing | port: agentmemory, Trail-code | 1 |
| S-06 | Duplicate filter | port: agentmemory | 1 |
| S-07 | Event log that never stalls a hook | port: agentmemory | 1 |
| S-08 | Error fingerprints | new, from Drain and error-tracker practice | 1 |
| C-01 | Repair pairing | port: Trail-code, widened | 2 |
| C-02 | Correction and sign detection | port: Trail-code; idea: BASE | 2 |
| C-03 | Lesson drafting with the client's own model | new; idea: agentmemory | 2 |
| C-04 | Lesson format and quality gate | port: Trail-code | 2 |
| C-05 | Reinforcement and fading | port: agentmemory | 2 |
| C-06 | Replacing a lesson and keeping its history | port: Trail-code; idea: BASE | 2 |
| C-07 | Day-one backfill from past sessions | port: Trail-code | 2 |
| C-08 | Starter rules | port: Trail-code; new catalog | 1 |
| G-01 | Parser runtime without cgo | spike; third-party runtime | 0 |
| G-02 | Symbol extraction from query files | port: Trail-code | 4 |
| G-03 | Cross-file resolution with honest confidence | port: Trail-code, Graphify | 4 |
| G-04 | Signatures and argument counts | new; partial port: Trail-code, Graphify | 4 |
| G-05 | SQL in the graph | port: Graphify | 4 |
| G-06 | Documents and configuration in the graph | port: Graphify | 4 |
| G-07 | Why-comments | port: Graphify | 4 |
| G-08 | Keeping the graph current | port: Trail-code | 4 |
| G-09 | Blast radius | port: Trail-code, Graphify | 4 |
| G-10 | One graph for a client's many repositories | port: Trail-code, Graphify | 4 |
| G-11 | Optional PDF and image processing | idea: Graphify | 5 |
| G-12 | Graph soundness check | port: Graft | 4 |
| R-01 | Search ranking | port: Trail-code | 4 |
| R-02 | What is injected with each prompt | port: Trail-code | 4 |
| R-03 | Query tools for the agent | port: Trail-code | 4 |
| R-04 | Scoped triggers and recall just before a tool runs | idea: BASE | 2 |
| R-05 | Session-start brief | port: Trail-code | 2 |
| R-06 | Stale lessons | port: Trail-code | 2 |
| E-01 | The rule ladder | new; idea: BASE | 2, block step in 3 |
| E-02 | Silent trial | idea: BASE | 2 |
| E-03 | Test cases on every rule | idea: BASE | 3 |
| E-04 | Sure-fail block | port: Trail-code | 3 |
| E-05 | Argument check from the graph | new | 4 |
| E-06 | Check before a commit | port: Trail-code | 3 |
| E-07 | Automatic demotion | new; idea: BASE | 2 |
| M-01 | Token savings accounting | port: Trail-code | 4 |
| M-02 | Benchmark harness | idea: Graft | 0 |
| M-03 | Isolation leak suite | new | 1 |
| M-04 | Repeat-mistake benchmark | new | 2 |
| M-05 | Enforcement accuracy | new | 3 |
| M-06 | Hook latency budget | new | 1 |
| M-07 | Graph accuracy against Trail-code | new | 4 |
| M-08 | Reporting rules | new | 0 |

## 7. Sources, licenses and how to look things up

Every feature entry names the files it draws on. All of them refer to these exact commits, read on 2026-11-08 and
2026-11-09.

| Project | Repository | Commit | License | What we may take |
|---|---|---|---|---|
| Trail-code | `github.com/trailhq/Trail-Code` | `e02b754c1d4e1de82b841522e2d8cae811702870` | MIT | Logic and files, with the copyright notice kept |
| agentmemory | `github.com/rohitg00/agentmemory` | `77f45358a8300299158e3a416e9010176408ea87` | Apache-2.0 | Logic and files, with the license and notice kept |
| Graphify | `github.com/Graphify-Labs/graphify` | `6c8002eca4acad58db6160eabcb466e61fc35afb` (v0.9.104) | Apache-2.0, some parts MIT | Logic and files, with `NOTICE` and both license texts kept |
| BASE | `github.com/ChristopherKahler/base` | `a37b9a8f88fb5676ff9ed3ddd9f124d4cd6aa8ae` (v0.16.0) | FSL-1.1-ALv2 | Ideas only. No code, no copied text |
| Graft 0.17.0 | Trail-code before its rename; a local copy on the owner's machine at `sources/source-code`, beside this repository, not a checkout | none | MIT | The benchmark method in its README, and two scripts |

To read a source: clone the repository, check out the commit above, and open the path the entry names. A permanent
link has the form `https://github.com/<owner>/<repo>/blob/<commit>/<path>#L<line>`.

Three rules for anyone building from these entries:

- **Port means rebuild, not translate.** The four projects are TypeScript, Python and Rust. Rebuild the behavior in
  Go and prove it with the entry's checks. Where a file can be used as it is (tree-sitter query files), the entry
  says so.
- **Keep the notices.** Anything ported from Trail-code, agentmemory or Graphify is listed in a `NOTICE` file with
  its license (F-07).
- **BASE is ideas only.** Its license forbids using its code in a product with similar functions. Entries marked
  "idea: BASE" describe a mechanism in our own words and are designed fresh.

How the facts here were gathered: fifty research agents read the five codebases and the web, one topic each, and
every claim about a codebase came with a file, a line and the exact text of that line. A script checked each quoted
line against the file: 396 of 398 matched. Facts that come from web pages could not be checked that way; entries
mark them "web" and name the page.

## 8. Build order

Phases are an order, not a schedule. Each phase ends with its checks green and the isolation suite (M-03) at zero
leaks. "Checks" means what runs without a paid agent: unit tests, the tester briefs, the latency and stall tests
(M-06) and the harness on its fixtures. Runs with a real agent cost money and do not hold a phase back; they hold
back a published number (M-08). A real run that misses its target means the claim is not made, and the miss is
recorded. One attack in that suite, L-09, tests the folder resolver and not the lobe; its fix is identity work that
lands before phase 1 (D-12, below), so the suite counts it like any other attack.

**Phase 0: answer five questions before building.** Each is a short throwaway test with a written result.

1. Which parser runs the twenty-five grammars listed in G-01 from Go without cgo at acceptable speed and size?
2. Which hook events and fields really arrive for failed shell, edit and MCP calls in Claude Code and Codex (S-01,
   S-02)?
3. Do twenty short-lived processes appending to one log on Windows ever tear a line, and what does a lock cost
   (F-01, S-07)?
4. How many failure-and-fix pairs in the owner's own past sessions would have made a useful lesson (C-07)? Counts
   only; no client content leaves the machine or enters the report.
5. Does the benchmark harness run one task with and without tentaqles and record tokens, calls, time and outcome
   (M-02)?

**Before phase 1: one fix outside the lobe (D-12).** The folder resolver must refuse a second folder that carries
a client's name. First a test in `internal/resolve` reproduces the case: a folder named like an existing client,
under another registered clients folder, holding a copied marker. If it resolves to that client, the resolver is
changed to bind each client to the folder it was created in; how the binding is stored is settled in the plan for
that change. If the test shows the resolver already refuses, the test stays as the guard and no code changes. This
is its own small change to identity code. It weakens no existing test and ships before any memory code.

**Phase 1: Sense.** The store, the fail-open engine, hook wiring, capture, scrubbing, fingerprints, starter rules,
settings and commands. The lobe's hooks are new entries in the existing plugin (D-11). The root `LICENSE` file
(MIT, D-13) and the `NOTICE` file land with the first ported code. A person sees fewer cross-shell errors from the
first hour, and `tentaqles lobe status` shows what was captured.

**Phase 2: Lessons.** Repair pairing, corrections, drafting, the gate with its English and Brazilian Portuguese
word lists (D-14), fading, backfill, sharing, then the ladder up to advice with its silent trial and automatic
demotion, then triggers, the session brief and stale lessons. Nothing learned is shown before the ladder exists.
This is where the repeat-mistake number first moves.

**Phase 3: Blocking.** Test cases on every rule, the step from advice to block, the sure-fail block, the check
before a commit.

**Phase 4: Graph.** Extraction, resolution, signatures, SQL, documents and configuration, freshness, blast radius,
many repositories, search, prompt injection, agent tools, the argument check, token accounting. It ends with the
first set of languages named in G-02.

**Phase 5: optional and later.** PDF and image processing (G-11); the remaining languages, each when it has a query
file and passes its fixtures; then the items in section 11.

**The phase 0 captures need a real, logged-in client.** A builder agent cannot create one, and must not read a
second client's sessions. The builder writes the capture scripts; the owner runs them in one client they name (the
author assumes the owner's own `tentaqles` client) and hands back the results, which hold counts and field names,
never content. Question 5 includes one real run of one task, started by the owner the same way.

## 9. How a feature entry reads, and how it is tested

Every entry in the companion pages has the same parts:

- **What it is** and **why it matters**, in a few sentences.
- **Mechanism**: the steps in order, with the real numbers.
- **Source**: project, license, whether we port or take the idea, and the files and lines to read.
- **How to build it**: the Go package, the shapes, the edge cases, and where we deliberately differ from the source.
- **How we know it works**: checks with a number or a yes-or-no answer.
- **Tester brief**: instructions a separate tester agent can follow without reading anything else.
- **Risks and open questions**.

**Rules for a tester agent.** A tester is a fresh agent that did not build the feature. It receives the built
`tentaqles` binary, the feature's tester brief, and an empty working folder.

1. Set `TQ_HOME`, `HOME` and `USERPROFILE` to folders inside the working folder before the first command. Never read
   or write the real `~/.tentaqles`. F-07 gives the exact commands as "the standard tester setup"; every brief
   starts from it.
2. Create clients only inside that folder. The standard pair is `acme` and `globex`.
3. Use no network unless the brief says so.
4. Run every numbered check. For each, report PASS or FAIL with the command and the lines of output that prove it.
5. "Could not run" is a FAIL. One FAIL fails the feature.
6. Report anything surprising that the checks did not ask about. Do not fix anything.
7. Some commands answer only to a person at a terminal (approving a block rule, letting one call through). A tester
   agent never pretends to be a person: it does not unset `CLAUDECODE` or fake a terminal. Briefs list such steps
   apart, as "person checks", for the owner to run; the logic behind them is covered by unit tests.

## 10. Decisions

**Made by the owner (2026-11-08 and 2026-11-09)**

| # | Decision |
|---|---|
| D-1 | Build our own, as one program in Go, instead of adopting Trail-code, Graphify, BASE or agentmemory |
| D-2 | It must be worth using with a single client or employer |
| D-3 | Capture every kind of tool error, not only shell or SQL |
| D-4 | The code graph is part of the product, not a separate install per client |
| D-5 | A lesson can stay private or be shared with teammates on the same client; the person chooses |
| D-6 | PDF and image processing is optional and can be switched at any time, with the behavior in 5.6 |
| D-7 | Every claim is backed by a benchmark |
| D-8 | Borrow mechanisms and code patterns from the four projects |
| D-9 | A new lesson speaks at once, as a note: one learned from a failure and its fix, or from the person's correction, is shown from the next matching call and taken back on the first false alarm. The silent trial stays for lessons mined from old sessions (5.5, E-01) |
| D-10 | A teammate's lesson that passes intake speaks on arrival, as a note, and can never block unless this person approves it here (5.5, F-04) |
| D-11 | The lobe's hooks ship in the existing plugin. The pinned test `test_the_plugin_holds_only_the_guard_hook` changes with the owner's yes: the guard's entries stay as they are and every other entry is a lobe entry. tentaqles writes Codex's trust records for the lobe's handlers (F-03) |
| D-12 | The folder resolver refuses a second folder with a client's name. A test reproduces the case first; the fix is its own change to identity code and lands before phase 1 (section 8, M-03 attack L-09) |
| D-13 | tentaqles ships under the MIT license. A root `LICENSE` file lands with the `NOTICE` file, before any ported code ships (F-07) |
| D-14 | A person may write a lesson in Brazilian Portuguese: the gate gets Brazilian Portuguese word lists in phase 2 (C-04). Other languages follow the same way later |
| D-15 | The cheaper benchmark sets run first. The SWE-bench Verified run waits for one approval from the owner at phase 4, asked with a checked cost (M-02) |

**Made by the author; each can be overturned**

| # | Call | Why |
|---|---|---|
| A-1 | Two engines; the lobe fails open and never says allow | A memory bug must not weaken isolation or stall work |
| A-2 | The store sits under `identities/<client>/lobe` | `drop` removes it with no new code; no shared key between clients |
| A-3 | Files are the truth; indexes are caches | Readable, repairable, and safe to delete |
| A-4 | Shared lessons live in `<repo>/.tentaqles/lessons/` | Git is the only sharing channel a client already trusts |
| A-5 | The ladder is local; a teammate's lesson arrives at most as advice | Nobody else can make your agent refuse work |
| A-6 | A block needs an exact pattern, a clean record and a typed yes | Trail-code built a blocking check and switched it off over false alarms |
| A-7 | Drafting uses the client's own agent CLI, detached, with an off switch | Client content only reaches that client's subscription |
| A-8 | The Trail-code graph is the base; Graphify adds SQL, documents and confidence scores | One design, not a blend of two |
| A-9 | Raw events expire after 14 days | Lessons keep patterns; data does not accumulate |
| A-10 | The lobe's hooks ship in the existing plugin as new entries | One install. Confirmed by the owner as D-11 |
| A-11 | User-facing name is "lobe": `tentaqles lobe`, "lessons", "rules" | Short, and matches the octopus naming |
| A-12 | A lobe hook acts only when the folder and the session's login belong to the same client; otherwise it does nothing | A session logged in as one client must never read or feed another client's memory |
| A-13 | The lobe keeps its own copy of small helpers instead of importing the guard | The guard's code stays untouched by memory work |
| A-14 | The ladder up to advice ships with the first shown lesson; only the block step waits for phase 3 | No lesson is ever shown before it can be demoted automatically |
| A-15 | Switches for `graph` and `lessons` beside `recall` and `enforce` | A person can keep one half without the other, and the benchmarks can measure each half alone |
| A-16 | A stale lesson is shown marked and never denies | Hiding it loses the reason; enforcing it blocks on code that has changed |
| A-17 | Our own key names (`must_match`, `must_not_match`) for a rule's test cases | BASE is ideas only; we do not reuse its identifiers |
| A-18 | The success that follows a failure keeps its scrubbed input in full for 14 days | Without it there is no repair to learn from |
| A-19 | Agent tools are commands (`tentaqles lobe graph ...`), with an MCP server only as an option | Commands work in both hosts with no extra setup and cost no context until used |
| A-20 | `tentaqles lobe sandbox <folder>` builds a throwaway home with two test clients, and refuses any folder inside the real home | Tester agents and benchmarks need clients without pretending to be a person at `reach` |
| A-21 | Capture is on by default, for new and existing clients | The first-week promise needs it; events are scrubbed, stay on the machine and expire after 14 days. `tentaqles lobe set capture off` stops it |
| A-22 | Machine-drafted lessons are written in English | The agent is the reader. A person may write one in English or Brazilian Portuguese (D-14) |
| A-23 | The keys of a `when` block narrow each other; a fingerprint or prompt words match on their own | "Any one key" would fire a shell lesson on every shell call |
| A-24 | A git worktree shares its repository's lessons and parse cache | Agents work in worktrees constantly; a fresh key per worktree would start each one cold |
| A-25 | Inside a hook, blast radius stays in one repository; `tentaqles lobe graph impact --all` crosses a client's repositories | The hook has 1.5 seconds; the command has none |
| A-26 | Published numbers come from Claude Code first, and each names its host and model; Codex follows when its usage fields are confirmed | One adapter in phase 0 instead of two |
| A-27 | Benchmark labels may be drafted by an agent that did not write what is tested; a person checks a sample and every disagreement, and the report says so | Hundreds of cases labeled by the owner alone would hold up phase 3 |
| A-28 | The harness builds the graph before a run and reports the build apart from the task's cost | It mirrors a person's second session, which is when the graph exists |
| A-29 | The gate runs its English and its Brazilian Portuguese word lists on every lesson, whatever the client's language | A teammate's intake runs the same gate and must reach the same verdict on any machine |
| A-30 | A teammate's lesson speaks on arrival whatever `source` its file states, a mined one included | Intake is what vouches for it, and a file's own claims earn nothing either way |

## 11. Left out

**For later.** Learned skills (turning a successful run into a reusable recipe; in Trail-code it is larger than the
whole first version of this). Compiler-grade call edges from language servers. Automatic grouping of code into areas
and a ranking of central functions (R-03's `map` prints folders and counts only). A visual graph or dashboard. Search
by meaning with vectors. Warnings when a teammate's branch edits the same function. Agents other than Claude Code and
Codex. Gate word lists for languages other than English and Brazilian Portuguese (D-14).

**For good.** Any vendor cloud or transcript upload. Telemetry. A long-running background service or REST API.
Dozens of agent tools. Coordination between agents. Project and task tracking. Model-written summaries of every
function. Silent self-update. Exports that send the graph elsewhere.

## 12. Questions the owner answered

The draft held seven questions that only the owner could answer. The owner answered all seven on 2026-11-09, and
the pages are written to those answers. No question is open.

| Question | Answer | Decision |
|---|---|---|
| How soon should a newly learned lesson speak? | At once, as a note; the silent trial stays for lessons mined from old sessions | D-9 |
| Does a teammate's lesson speak when it arrives? | Yes, as a note, once it passes intake; never as a block | D-10 |
| May the lobe's hooks ship in the existing plugin? | Yes; the pinned plugin test changes with the owner's yes | D-11 |
| Should the folder resolver refuse a second folder with a client's name? | Yes; reproduce it with a test, then fix it before phase 1 | D-12 |
| Which license does tentaqles ship under? | MIT | D-13 |
| May a person write lessons in the client's language? | Yes, in Brazilian Portuguese from phase 2; other languages later | D-14 |
| May the public benchmark spend money, and when? | Cheaper sets first; one approval for the larger run at phase 4 | D-15 |
