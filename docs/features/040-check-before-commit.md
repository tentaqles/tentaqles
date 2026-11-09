# 040: Check before a commit

**Status** not started · **Stage** Blocking · **Entries** E-06, F-04 (step 5: a private lesson staged in a
repository) · **Needs** 038 · **Feeds** 041

## Result

Just before the agent runs `git commit`, in Claude Code or in Codex, the lobe compares the staged change with
the client's lessons using plain text rules. When the change goes against a lesson, the agent is told which
lesson and which line, and the commit still runs. It also warns when the commit would publish a private
lesson. It warns and never blocks, at any setting. A person can run the same check by hand.

```bash
cd "$W/clients/acme/etl"
git add load.sql
tentaqles lobe check                  # the staged change against the lessons; always exits 0
tentaqles lobe check --base main      # the branch since its merge base with main
# in a session, "git commit -m merge" gets:
#   [tentaqles lobe] Before this commit: load.sql:1 matches the avoid pattern of l-4c7d19e2 (TITLE). ...
```

## Scope

**In**

- Package `internal/lobe/commitcheck`: `trigger.go`, `diff.go`, `terms.go`, `rules.go`, `report.go`.
- The trigger: PreToolUse for a shell tool whose command runs `git commit`, on both hosts.
- Reading the staged change with a bounded, read-only git child.
- The candidates and the three rules of E-06 step 4: pattern, value, words.
- Step 5, the part of F-04 that waited for this feature: a staged file under `.tentaqles/lessons/` that says
  `scope: private`, or fails the cheap intake checks, is a finding of its own.
- A shared lesson changed by the same commit is read from `HEAD`.
- At most 5 findings, once per session for the same set, printed inside R-04's block.
- `tentaqles lobe check [--base <ref>]`, for anyone, always exit 0.

**Out**

- `git push` and `gh pr create`: the first version covers `git commit` only (E-06 step 1). No feature takes
  them.
- A stop. This check never denies; a deny is the work of [038](038-approve-to-block.md) and
  [039](039-sure-fail-block.md).
- The full intake check and the gate on a staged lesson: they run in the detached pass, never in a hook
  (feature [034](034-teammate-lesson-intake.md)).
- The doctor finding `lobe-private-in-repo`, which reports the same file (features
  [033](033-share-a-lesson.md) and 034).
- Numbers on how often a finding is wrong. E-06 expects them from M-05, by file type and for the word rule,
  but M-05 has no step for a staged change. No feature takes them; see the final report.

## What it builds on

- 003, 010, 036: `internal/lobe/hook`, the Codex hooks, `internal/lobe/shellwords`, the session file
  `sessions/<session>.json` in which each writer keeps keys of its own.
- 016: `internal/lobe/lesson` (parsing a header and the three sections) and the gate's word lists.
- 017: `internal/lobe/recall` with R-04's command test in `trigger.go` (statements, wrappers, flags and their
  values, the program name), the block before a tool, its 700 characters inside F-05's frame, and
  `safety.Wrap`. C-08's starter line already prints in that block ahead of lessons.
- 023 and 024: which lessons act at `advice` or `block` after the caps.
- 032: R-06's stale verdict and its mark.
- 033 and 034: shared lessons in `<repo>/.tentaqles/lessons/`, `intake.json`, and the cheap checks of
  intake's step 8 (name, size, a header that parses, `scope: shared`).
- 037: `ladder.Match` over an `avoid.pattern` that has passed its tests.
- 038: when a deny goes out it is the only output, so the findings are dropped.

## What it leaves for later features

- `commitcheck` and `tentaqles lobe check`: nothing later widens them. 041 does not measure them (see Scope).
- `diff.go`, a parser of a unified diff by hunk-header counts. G-09 (feature 049) has its own diff reading;
  if that team wants this one, it says so in its own file.

## How it works

1. **Trigger** (`trigger.go`). The event is PreToolUse for a shell tool. R-04's command test finds a
   statement whose program is `git` and whose subcommand is `commit`; git's own options may come before the
   word. A command that changes folder first (`cd x && git commit`, `git -C x commit`) prints nothing.
2. **Switches.** The check runs when `enforce` is not `off` and `recall` and `lessons` are on. It has 1,000 ms
   (proposed) inside the hook's 1.5 second cut-off and prints nothing past either.
3. **The change** (`diff.go`). `git diff --cached --unified=3` in the payload's `cwd`, plus tracked changes
   when the command has `-a`. git runs as a child with `--no-optional-locks --no-ext-diff --no-color`, never
   fetches, and is cut off at 800 ms (proposed). At most 400 KiB are read. The diff is parsed by hunk-header
   counts, not by line prefixes. No repository, nothing staged, only renames or only binary files: nothing is
   printed.
4. **Candidates**, up to 25. Lessons acting at `advice` or `block`, from the private folder and from this
   repository's `.tentaqles/lessons/`. First those whose `touches` or `when.place` names a changed file or
   its folder. Then those that share at least 2 terms with the added lines. A term has 3 to 40 characters,
   is no stop word and is not all digits. A stale lesson stays a candidate, after the fresh ones of its
   group, and its finding carries R-06's mark.
5. **Rules** (`rules.go`), per changed file and candidate, strongest first. Files under `.tentaqles/` are left
   out here and go to step 6.
   - Pattern: the lesson's `avoid.pattern` matches an added line, or the joined added lines of one hunk.
   - Value: a removed and an added line share an identifier and differ in a number, and `## Avoid` names the
     identifier with the new number, or `## Do` names it with the old number only. Digits only: a number
     word such as "first" is not a number.
   - Words: a sentence under `## Avoid` shares 2 of its terms (3 when it has more than 4) with one unbroken
     run of added lines. It is reported only when the lesson touches that exact file.
6. **Staged lesson files.** A staged file under `.tentaqles/lessons/` gets the cheap part of intake: its
   name, its size, a header that parses and `scope: shared`. One that says `scope: private`, or fails, is a
   finding, because this commit would publish it. The gate is not run in a hook. A shared lesson that this
   commit changes is read from `HEAD` for steps 4 and 5, so a commit cannot silence the lesson it breaks.
7. **Report** (`report.go`). One finding is kept per file and lesson, and at most 5 are printed. The text is
   E-06 step 6's: "[tentaqles lobe] Before this commit: load.sql:1 matches the avoid pattern of l-4c7d19e2
   (TITLE). The commit goes ahead. A finding can be a word match; if it is real, fix it next and tell the
   person." A staged lesson reads: ".tentaqles/lessons/l-4c7d19e2.md is a private lesson and this commit
   would publish it." With several findings the opening words and the two closing sentences appear once,
   with one clause per finding. The findings print in R-04's block, ahead of lessons and inside its 700
   characters, through `safety.Wrap`, because a title is lesson text. The envelope is the one 036 fixed for
   each host.
8. **Once per set.** The key is the SHA-256 of the sorted `file|lesson` pairs. The last 20 keys are kept in
   `sessions/<session>.json` under a key of this feature's own, `commit_checks` (name proposed here). The
   same set is not printed twice in a session.
9. **Never a deny.** With `enforce: block` and a finding, the output holds no `permissionDecision`. When 038
   or 039 denies the same call, that deny is the only output and the findings are dropped.
10. **By hand.** `tentaqles lobe check` runs steps 3 to 7 on the staged change of the current repository and
    prints the findings as plain lines. With `--base <ref>` it reads the change from the merge base of
    `<ref>` and `HEAD` to `HEAD` instead. It has no hook budget, no once-per-set memory, and always exits 0.

## Where to read

| What | Where |
|---|---|
| Requirements | [E-06](../prd/lobe-enforce.md), whole entry; [F-04](../prd/lobe-foundation.md) steps 5 and 8; [R-04](../prd/lobe-recall.md) steps 3, 4 and 11 and the last "How to build it" item; [R-06](../prd/lobe-recall.md) steps 5 and 6; [core page 5.2](../prd/vertical-lobe.md) |
| Why it only warns (Trail-code, MIT) | [`src/claude/pre-bash-check.ts` L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L42), [L40](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L40), [`CHANGELOG.md` L12](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/CHANGELOG.md#L12) |
| Source to port: trigger, once per set | [`src/claude/pre-bash.ts` L43](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash.ts#L43), [`src/claude/pre-bash-check.ts` L112](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L112), [L125](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L125), [L79](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash-check.ts#L79) |
| Source to port: candidates, caps, rules | [`src/check/local-check.ts` L37](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L37), [L40](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L40), [L167](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L167), [L901](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L901), [L824](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L824), [`src/cloud/diff.ts` L54](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cloud/diff.ts#L54) |
| Weakness not taken | number words: [`src/check/local-check.ts` L709](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L709); the pool cut before file matches: [L521](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/check/local-check.ts#L521); exit code 1: [`src/cli.ts` L3221](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/cli.ts#L3221); the warning returns before later guards: [`src/claude/pre-bash.ts` L270](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash.ts#L270) |
| Lobe code from earlier features | `internal/lobe/recall` (`trigger.go`, `pretool.go`, `format.go`, `stale.go`), `internal/lobe/share/intake.go`, `internal/lobe/safety/frame.go`, `internal/lobe/hook/steps.go`, `internal/lobe/cli` |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: how R-04's block takes lines
   from other steps (C-08's line is the model), what intake exports for its cheap checks, how the session
   file's keys are owned. The owner approves changes before code is written.
2. `diff.go` and `terms.go`, with the git child behind an interface so a test can stall it. Ported files
   start with the `// Ported from` line and are listed in `NOTICE`. If the stop words at the source link
   equal the gate's English list, reuse the gate's file and keep one copy. Proof:
   `TestLobeCommitCheck_Budget`, a table test of hunk parsing.
3. Side by side with 2: `trigger.go` on R-04's command test. Proof: `TestLobeCommitCheck_OnlyGitCommit`.
4. After 2: `rules.go` and the candidates. Proof: `TestLobeCommitCheck_PatternFinding`,
   `_NumberWordsIgnored`, `_TouchBeforeWords`, `_StaleRankedLast`.
5. After 4: the staged-lesson finding and the read from `HEAD`. Proof:
   `TestLobeCommitCheck_PrivateLessonStaged`, `TestLobeCommitCheck_ChangedLessonReadFromHead`.
6. After 4: `report.go`, the step in `pre-tool-use` for both hosts, the once-per-set key. Proof:
   `TestLobeCommitCheck_NeverDenies`, `TestLobeCommitCheck_DenyDropsFindings`, `TestLobePreToolBudget`.
7. After 4: `tentaqles lobe check` in `internal/lobe/cli`. Proof: `TestLobeCommandTable`, evaluation check 3.
8. Docs: a `tentaqles lobe check` section in `docs/reference/commands.md`; `CHANGELOG.md`. The plugin does
   not change.
9. Tester run of the Evaluation below.

## Tests that stay in the repository

- `TestLobeCommitCheck_PatternFinding`: a staged line that matches an `avoid.pattern` gives 1 finding with
  its line.
- `TestLobeCommitCheck_NeverDenies`: with a finding and `enforce: block`: 0 denies; `tentaqles lobe check`
  exits 0.
- `TestLobeCommitCheck_OnlyGitCommit`: with the same staged change, `git push` and `gh pr create` print 0
  findings.
- `TestLobeCommitCheck_NumberWordsIgnored`: "after the first failure" and a retry count going 3 to 1: 0
  findings.
- `TestLobeCommitCheck_TouchBeforeWords`: among 100 word matches and 1 touch-only lesson, that 1 is a
  candidate.
- `TestLobeCommitCheck_StaleRankedLast`: a fresh and a stale lesson touch one file: the fresh finding comes
  first and the stale one carries R-06's mark.
- `TestLobeCommitCheck_PrivateLessonStaged`: a staged `.tentaqles/lessons/` file with `scope: private` gives 1
  finding and 0 denies.
- `TestLobeCommitCheck_Budget`: with git stalled 3 seconds the hook answers by its cut-off with empty stdout.
- This file's names, for parts E-06 states without naming a test:
  `TestLobeCommitCheck_ChangedLessonReadFromHead` (a commit that deletes the `avoid` block of a shared lesson
  and breaks it still gets the finding); `TestLobeCommitCheck_DenyDropsFindings` (a call that 038 denies
  prints the deny and no finding); `TestLobeCommitCheck_OncePerSet` (the same set twice in a session prints
  once; a changed set prints again); `TestLobeCommitCheck_Switches` (`enforce: off`, `recall: off` and
  `lessons: off` each give 0 bytes); `TestLobeCommitCheck_BothHosts` (the Codex hook prints the same finding
  in Codex's envelope, and nothing under the other client's login); `TestLobeCommitCheck_GitIsReadOnly` (the
  index and the working tree are byte-identical before and after, and no fetch runs).
- House rules kept green: `TestLobeEveryInjectionIsFramed`, `TestLobePreToolBudget` (700 characters with
  findings and lessons together), `TestLobeHookAlwaysExitsZero`, `TestLobeHookNeverPrintsAllow`,
  `TestLobeHookServesOnlyItsLogin`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestNoticeListsEveryPort`, `TestDocsReferenceEveryCommand`, the latency and stall checks of 012 with a
  `git commit` call, the leak suite of 013, and every test under "Must not trade off" in `CLAUDE.md`
  unedited.

## Evaluation

Run the standard tester setup of [002](002-sandbox.md), which is F-07's block.
`<acme>` is `$W/clients/acme` and `<lobe>` is `$TQ_HOME/identities/acme/lobe`. Run
`export CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"` and
`export CODEX_HOME="$TQ_HOME/identities/acme/codex"` first. Backticked text split over two lines is one line.
Create `<acme>/etl`, run `git init` there, and write `load.sql` with the one line
`MERGE INTO t USING (SELECT 1);`. Write `<lobe>/lessons/l-4c7d19e2.md`:

```markdown
---
id: l-4c7d19e2
title: Alias the MERGE source
kind: lesson
source: person
scope: private
applies: client
origin: {by: me, host: person, drafted: person}
when: {place: [load.sql]}
avoid:
  pattern: '(?i)merge into \S+ using \('
  must_match: ["MERGE INTO t USING (SELECT 1)", "merge into dim using (select 2)"]
  must_not_match: ["MERGE INTO t USING s", "MERGE INTO t USING src AS s"]
strength: 1.0
seen: 3
created: 2026-11-09
---
## Do
Give the source an alias.
## Avoid
A subquery as the source.
## Why
The warehouse rejects it because the source has no alias.
```

`tentaqles lobe lessons gate <lobe>/lessons/l-4c7d19e2.md` must print `ok`; if not, stop and report FAIL. A
lesson that a person wrote enters at `advice`, so no `ladder.json` is needed. Run `git add load.sql`. Run
every command inside `<acme>/etl`. Input A, piped to `tentaqles lobe-hook claude pre-tool-use`:
`{"session_id":"s1", "hook_event_name":"PreToolUse", "cwd":"<acme>/etl", "tool_name":"Bash",
"tool_use_id":"t1", "tool_input":{"command":"git commit -m merge"}}`. No check below runs `git commit`
itself; "Set KEY to X" means `lobeset acme KEY X`.

1. Input A: exit code 0; stdout contains `load.sql:1`, `l-4c7d19e2` and `goes ahead`, and no deny decision.
2. Input A again, unchanged: stdout does not contain `load.sql:1`.
3. `tentaqles lobe check`: exit code 0, and stdout contains `load.sql:1` and `l-4c7d19e2`.
4. Set `enforce` to `block`. Input A with `session_id` `s2`: stdout contains `load.sql:1` and no deny
   decision.
5. Input A with `session_id` `s3` and the command `git push`: stdout does not contain `load.sql:1`.
6. Input A with `session_id` `s4` and the command `git -c user.name=x commit -m merge`: stdout contains
   `load.sql:1`. With `session_id` `s5` and the command `cd .. && git commit -m merge`: stdout does not
   contain `load.sql:1`.
7. Input A with `session_id` `s6`, piped to `tentaqles lobe-hook codex pre-tool-use`: exit code 0, stdout
   contains `load.sql:1` and `l-4c7d19e2`, and no deny decision.
8. Set `enforce` to `off`. Input A with `session_id` `s7`: exit code 0 and empty stdout. Set `enforce` to
   `advice` and `recall` to `off`. Input A with `session_id` `s8`: empty stdout. Set `recall` to `on`.
9. With `CLAUDE_CONFIG_DIR` set to `$TQ_HOME/identities/globex/claude`, input A with `session_id` `s9`: exit
   code 0 and empty stdout.
10. Copy the lesson file to `<acme>/etl/.tentaqles/lessons/l-4c7d19e2.md` and run `git add .tentaqles`. Input A
    with `session_id` `s10`: stdout contains `.tentaqles/lessons/l-4c7d19e2.md` and `private lesson`, and no
    deny decision.
11. `git status --porcelain` lists the same staged files as before check 1 plus the one of check 10, and
    `git rev-list --all --count` prints `0`: the lobe staged nothing and committed nothing.

**Person checks.** None is required: no command of this feature answers only to a person. One is useful for
the owner, in a real session of a client they name: stage a change that goes against one of that client's
lessons and ask the agent to commit. The agent reports the finding and the commit is made.

PASS needs 11 of 11.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) on the three systems; the plugin suite is untouched.
- [ ] The tester brief passes, 11 of 11.
- [ ] The house rules hold: `TestLobeCommitCheck_NeverDenies`, `TestLobeCommitCheck_Budget`,
  `TestLobeEveryInjectionIsFramed`, `TestLobeHookServesOnlyItsLogin`, `TestLobeGuardNeverImportsLobe`, the
  latency and stall checks, the leak suite at zero leaks, the guard's tests unedited.
- [ ] `docs/reference/commands.md`, `NOTICE` and `CHANGELOG.md` are updated.
- [ ] The owner has seen a finding before a commit and has seen the commit go ahead.

## Risks and unknowns

- **Noise is the known failure of this check.** Trail-code built it as a stop and switched it off: 4 word
  matches on one pull request, none real. We start with a warning. E-06 proposes dropping the word rule if
  M-05 shows over 1 false finding in 10, but no benchmark step measures findings (see Scope). Until one
  exists, the owner's own use is the only measure.
- **An `avoid.pattern` is written for call text.** It can match a test or a quoted example in a file.
- **The guard and this check both see `git commit`.** The guard decides identity and fails closed; this check
  only reads the staged change with a local, read-only git child and never says allow. Its git child must
  not be something the guard's own tests see as a second identity path: task 1 confirms that `git diff` with
  the three flags reads no remote and writes no lock.
- **Budget.** 1,000 ms and 800 ms are proposed, under Trail-code's 3.5 seconds, so a big change may go
  unchecked. `git` start-up on Windows may eat most of it; the latency check of 012 with a `git commit` call
  is the measure, and a miss prints nothing.
- **Only `git commit` is covered.** A change pushed, or opened as a pull request, without a commit from the
  agent's session is not checked.
- **A private lesson staged in a repository gets a warning, not a stop.** `tentaqles doctor` reports the same
  file as `lobe-private-in-repo` (F-04).
- **This file's own choices**: the session key `commit_checks`; reading `--base <ref>` as the merge base of
  `<ref>` and `HEAD`; that a shared lesson new in this commit, which `HEAD` does not have, is read as intake
  recorded it; that the findings pass through `safety.Wrap`. Each is in the final report.
