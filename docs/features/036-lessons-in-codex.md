# 036: Lessons and starter rules in Codex

**Status** not started · **Stage** Lessons · **Entries** F-03 (the Codex handlers, complete), F-02 (the Codex
envelopes), R-04, R-05, C-02 and C-08 on Codex; also S-02 (what a Codex failure is), E-02 (fires on Codex's
one post-tool event), F-05 (the frame; the tamper mark for `apply_patch`), C-03 (a Codex candidate is
templated), C-07 (Codex transcripts); phase 0 question 2, the Codex half · **Needs** 035 · **Feeds** 037 and
every later feature, which cover both hosts

## Result

A Codex session in a client's folder gets what a Claude Code session gets from features 014 to 035, through
Codex's own hooks. The session starts with the brief. Before a shell call that a lesson's `when` block
matches, the agent reads the note. After a failed shell command it reads a starter rule's fix and the lesson
for that exact error. A correction the person types is noticed. Fires are recorded and judged, and a wrong
note is taken back. The client has one store, so a lesson learned in Codex speaks in Claude Code and the
reverse. `tentaqles lobe backfill` also reads the client's Codex sessions. Four things Codex does not get are
listed under Scope.

```bash
tentaqles settings render acme         # writes six lobe handlers into acme's Codex config and trusts them
tentaqles doctor                       # no lobe-hooks-untrusted finding
tentaqles run . -- codex               # a Codex session under the client's login
tentaqles lobe events                  # events with "host":"codex"
tentaqles lobe rules                   # fires counted from both hosts
```

## Scope

**In**

- The Codex half of phase 0 question 2 that feature 010 did not need: what Codex does with a hook's output.
- All six Codex handlers of F-03 step 4, trusted by tentaqles, with the guard's trust untouched.
- The Codex payload readers and the Codex output envelopes for `session-start`, `user-prompt-submit`,
  `pre-tool-use`, `post-tool-use`, `stop` and `session-end`.
- Every kind of output of 014 to 035 in Codex: see the table in "How it works", one row per feature.
- `tentaqles lobe backfill` reading `codex/sessions`, for repairs.

**Out, because the requirements say Codex lacks it**

- A starter note before a call. Codex's shell tool does not say which shell it runs, so only entries with
  `shell: any` apply, and none of those has a `pattern` (C-08, risks).
- Anything after a failed call that is not a shell call. Such a failure is not recorded in Codex until phase 0
  says what arrives (S-02 step 6), so it gets no fix, no lesson and no verdict.
- The signs `undone` and `fixed`, and correction evidence (b). They need edit events, and a successful
  `apply_patch` leaves no event (S-02 step 6).
- Wording by the model for a candidate whose `host` is `codex`: it is templated until Codex's headless flags
  are confirmed (C-03, risks). Correction signs from Codex transcripts in backfill (C-07, risks).

**Out, taken by another feature**

- Lessons and code pointers added to a prompt (R-02, feature 050). `user-prompt-submit` prints nothing here.
- Any deny in Codex (features 038 and 039), the check before a commit (040), blast radius and argument notes
  after an `apply_patch` (049, 051).
- A Codex adapter for the benchmark harness. M-02 step 8 says it follows when Codex's usage fields are
  confirmed; no feature in the index takes it.

## What it builds on

- 010: `tentaqles lobe-hook codex post-tool-use` with S-02 (`internal/lobe/sense/codex.go`:
  `FromCodexPostTool`, `codexExit`), the client rule read from `CODEX_HOME`, the written result of the Codex
  capture, and whatever part of F-03's Codex wiring it built in `internal/codexconfig` (`lobeEvents` with at
  least `PostToolUse`, the handler form with `|| exit 0`, the trust records). Task 1 lists what is there.
- 003 and 005: `internal/lobe/hook` (`Main`, `payload.go`, `steps.go`, `emit.go`, `Detach`), the settings,
  `tentaqles lobe status`, `internal/lobe/health`. 011: the sweep at session end. 012 and 013: `tentaqles lobe
  bench latency --host <host>`, `stall` and `leak`.
- 014 and 015: `internal/lobe/starters` (`MatchCommand`, `MatchError`). 017 and 020: `internal/lobe/recall`
  (`Call`, the matcher, `pretool.go`, `postfail.go`). 023 and 024: `internal/lobe/trial`, `ladder.Settle`,
  `apply`. 025: C-03's drafting. 026: `recall.Brief`. 027 and 028: C-02's flag step and signs. 033 and 034:
  `safety/tamper.go`. 035: `internal/lobe/backfill`.
- The guard's Codex code, read and not changed in behavior: `internal/cmd/codexhook.go`,
  `internal/codexconfig` (`hookEvents`, `Render`, `trustHooks`, `HookTrust`, `CheckGuard`, `hookHash`),
  `internal/cmd/settings.go` (`renderCodex`, `codexHookProgram`).

## What it leaves for later features

- Six Codex handlers and their trust; `codexconfig.LobeHookTrust`; the finding `lobe-hooks-untrusted`.
- A host switch in `hook/payload.go` and `hook/emit.go` that every feature from 037 on uses without a second
  thought: one `Answer`, two envelopes.
- `recall.Call` filled from a Codex payload, with the paths of an `apply_patch` (049 and 051 need them).
- `backfill/codex.go`.
- The written list of what Codex does not deliver, in `docs/how-it-works.md`.

## How it works

1. **The facts to confirm first** (F-03 step 1; core page section 8). Each comes from the capture the owner
   ran for feature 010, or from a second run of the same throwaway hook. The builder writes the hook and the
   reducer; the owner runs them in one client they name and hands back counts, field names and yes or no,
   never content. Needed here: (a) does the model receive `additionalContext` printed on PreToolUse and on
   PostToolUse, and plain text printed by a second SessionStart handler; (b) does SessionStart carry `source`,
   with which values; (c) is `tool_use_id` present before and after a call; (d) the name of the prompt field
   on UserPromptSubmit; (e) for which tools PreToolUse and PostToolUse fire, their `tool_name` values, and the
   shape of the `apply_patch` input; (f) does the shell tool's input name its shell; (g) any variable that
   marks a Codex agent session; (h) does Codex stop a hook's child processes when the hook returns; (i) the
   cap on a session-end hook; (j) the field names of a rollout file's lines. A "no" on (a) for an event means
   that event can deliver nothing in Codex: stop and ask the owner before building on.
2. **Registration** (F-03 steps 4 to 6). `lobeEvents` in `internal/codexconfig` lists `SessionStart`,
   `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop` and `SessionEnd`. Each handler is `<program>
   lobe-hook codex <event> || exit 0`, with no matcher and timeout 5; `SessionEnd` gets timeout 3. The program
   is the one `codexHookProgram` gives the guard. In each event the order is: the person's groups, the guard's
   group, then the lobe's, so the guard's group index, key and hash do not change. `Render` writes
   `trusted_hash` for the lobe's handlers as it does for the guard's (decision D-11). The labels Codex hashes
   for the four events the guard does not use are read from codex-rs and fixed by golden hashes.
3. **The guard's trust stays the guard's.** `PreToolUse` and `SessionStart` now hold a guard handler and a
   lobe handler. `ownedCommand` learns the lobe suffix so that a re-render replaces lobe handlers, but the
   walk behind `HookTrust` and `CheckGuard` must keep visiting guard handlers only: today `ownedHandlers`
   visits every owned handler of an event, so it needs a guard-only test of the command. A new
   `LobeHookTrust` reports the lobe's handlers. A missing, edited or untrusted lobe handler gives the warning
   `lobe-hooks-untrusted` and never stops `tentaqles run`.
4. **One process, the same steps.** `tentaqles lobe-hook codex <event>` enters `hook.Main` as the Claude Code
   form does. The client rule reads `CODEX_HOME` and wants `paths.IdentityDir(client, "codex")` (F-02 step 4).
   The steps of an event are those of F-02 step 2; the host changes three things only: how the payload is
   read, what `post-tool-use` means, and the envelope. Codex has no failure event, so `post-tool-use` does the
   work of both post-tool events: S-02 decides `fail`, `ok`, still running or `no-exit`, and after a `fail` or
   `hidden-fail` outcome the answer to the failure is printed on this same event.
5. **Envelopes** (F-02, the table after step 8). `session-start`: the brief as plain text on stdout, as the
   guard's Codex preamble is printed. Every other event that prints: one JSON object,
   `{"hookSpecificOutput":{"hookEventName":"<Event>","additionalContext":"<text>"}}`, with `PreToolUse` or
   `PostToolUse`. `stop` and `session-end` print nothing. Exit code 0 always. Lesson text is always inside
   F-05's frame, in the plain text too. The budgets are those of Claude Code (700 characters inside the frame
   for a note, 1,000 for the brief); Codex's own cap of 2,500 tokens is far above them.
6. **The call** (R-04, "How to build it"). `recall.Call` is filled from the Codex payload: `Tool` is
   `tool_name` as Codex sends it, `Command` is `tool_input.command` for `Bash`, `Paths` are the files an
   `apply_patch` names. `when.tool` is compared exactly, so a lesson with `tool: [Bash]` matches in both hosts
   and one with `tool: [Edit]` or `tool: [PowerShell]` never matches in Codex.
7. **What each feature needs.** The table goes through 014 to 035. "Nothing" means the feature works on the
   store or in the pass and does not care which host wrote an event.

   | Feature | What Codex needs here |
   |---|---|
   | 014 starter note before a call | The `PreToolUse` handler and `MatchCommand` on the Codex call. With `shell: any` alone no entry has a `pattern`, so nothing is printed. A test pins that |
   | 015 starter fix after a failure | On `post-tool-use` with a `fail` or `hidden-fail` outcome, `MatchError` over the `shell: any` entries: `npm-needs-login`, `git-https-auth`, `disk-full`. The `bash` and `powershell` entries, `timeout-missing` among them, do not apply. The line prints ahead of lessons, in the same block |
   | 016 lesson by hand | Nothing in the hooks. The person-only commands know an agent by `CLAUDECODE` only; see Risks |
   | 017 lesson before a call, the ladder to `advice` | The `PreToolUse` handler, step 6, the envelope of step 5. The ladder is the same file for both hosts |
   | 018 repair pairing | The `Stop` handler, which calls `hook.Detach`. Pairing reads events, and 010 already writes them with `"host":"codex"` |
   | 019 a repair becomes a lesson | `origin.host: codex`, taken from the candidate's events |
   | 020 the lesson for this exact error | R-04 step 8 on Codex's `post-tool-use` after a `fail`, with `hookEventName` `PostToolUse`. It shares the session record with step 7 of R-04, so the lesson is not repeated before the next call |
   | 021 harness, 022 repeat benchmark | Nothing. Published numbers are from Claude Code (A-26) |
   | 023 fires and verdicts | A fire is opened on `pre-tool-use` and judged on `post-tool-use`, found by `tool_use_id` or by tool and call hash. The trial line has `"host":"codex"`. A call with no exit code named, a call still running and a call that is not a shell call give no outcome; the fire closes as `unknown` after 10 minutes |
   | 024 a wrong note is taken back | Nothing new: the move is applied in the Codex `post-tool-use` process, under the same lock |
   | 025 model drafting | A candidate whose `host` is `codex` goes to the template. The action line says `"by":"template"`; C-03 names no `why` for this case, and this file uses `host` |
   | 026 session brief | The `SessionStart` handler and plain text. A payload with no `source` is treated as `startup` (R-05, edge cases), so a resumed Codex session may print the brief again |
   | 027 a correction becomes a lesson | The `UserPromptSubmit` handler and the prompt field of fact (d). The flag step is the same code; the hook prints nothing. Evidence (a) and (d) work on Codex events |
   | 028 undone edits, fixed checks | Nothing can be built: there are no edit events. A test pins that a Codex session yields 0 `undone` and 0 `fixed` signs, so the gap is visible when S-02 changes |
   | 029 Portuguese | Nothing. The gate is the same; C-02's wording list follows the client's language |
   | 030 strengthen and fade, 031 replace, 032 stale | Nothing. Hits from Codex fires reinforce; the stale mark prints in a Codex note |
   | 033 share | Nothing in filing. The tamper check runs on Codex's `post-tool-use`: a `Bash` command as before, and an `apply_patch` whose paths lie under a protected place |
   | 034 a teammate's lesson | Nothing. A pulled lesson speaks in Codex through the same recall |
   | 035 backfill | `codex.go`, step 9 below |

8. **The pass and the model.** `stop` starts the detached pass exactly as in Claude Code. The pass reads every
   session in the two newest event files, whichever host wrote them. C-03's login check reads
   `CLAUDE_CONFIG_DIR` from the environment of the run; a pass started from a Codex session has it only when
   the terminal carries the client's whole identity, as a tentaqles terminal does. Without it a Claude Code
   candidate is templated with the reason `identity`.
9. **Backfill from Codex transcripts** (C-07 steps 2 to 5). `codex.go` reads
   `IdentityDir(client, "codex")/sessions/YYYY/MM/DD/rollout-*.jsonl`, one `{timestamp, type, payload}` object
   per line. A shell call and its output are joined by the call id. The exit code is read from the output
   text with S-02's `codexExit`; a call with no code named gives no event. A session whose first `cwd` does
   not resolve to this client is `foreign`. The same limits apply: 180 days, 2 MiB per line, 60 and 300
   seconds. Only C-01 runs on these events: Codex transcripts carry no reliable mark of what the person typed,
   so no C-02 sign is mined. Sessions of both hosts count together for "2 sessions on 2 different local
   days", because S-02 gives one error the same fingerprint on both hosts. A lesson's `origin.host` is the
   host of the newest session behind it. `--json` counts the sessions of both roots.
10. **`TQ_LOBE_NO_HOOKS=1`** makes all six Codex hooks do nothing, in `hook.Main` (F-03 step 7). Under Codex
    no wrapper script runs; the `|| exit 0` tail turns any status into 0, on cmd and on a POSIX shell alike.

## Where to read

| What | Where |
|---|---|
| Requirements | [F-03](../prd/lobe-foundation.md), steps 1 and 4 to 7, "How to build it" (the Codex bullets), tester checks 4 and 5, the risks; [F-02](../prd/lobe-foundation.md), steps 2, 4 and 6, the envelope table, the edge cases; [S-02](../prd/lobe-sense.md), whole; [C-08](../prd/lobe-consolidate.md), steps 1, 2 and 5, the catalog table, the last risk; [R-04](../prd/lobe-recall.md), steps 7, 8 and 11 and "How to build it"; [R-05](../prd/lobe-recall.md), steps 7 and 9 and the first risk; [C-02](../prd/lobe-consolidate.md), steps 1, 3 to 6; [C-03](../prd/lobe-consolidate.md), step 1 and the first risk; [C-07](../prd/lobe-consolidate.md), steps 2 to 5, "How to build it", the first risk; [E-02](../prd/lobe-enforce.md), steps 1 to 4 and the third risk; [F-05](../prd/lobe-foundation.md), steps 4 and 5; [F-07](../prd/lobe-foundation.md), the hook rows of the command table and the first risk; [M-06](../prd/lobe-benchmarks.md), step 10; [core page 5.1, 5.2 and section 8](../prd/vertical-lobe.md) |
| Source to port | [`src/trace/codex-exec.ts` L14](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/codex-exec.ts#L14): the header ends at the first line that is exactly `Output:`. [`src/hosts/codex-hooks.ts` L64](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/hosts/codex-hooks.ts#L64): a Codex PostToolUse entry with a 5 second timeout. [`src/claude/settings-merge.ts` L99](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/settings-merge.ts#L99): timeout 5, and at L47 the `exit 0` fallback after a double pipe. [`src/claude/hook-io.ts` L115](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hook-io.ts#L115): one JSON object with `additionalContext`, nothing when empty. In the same project, `src/trace/codex.ts` L2: the rollout line shape |
| Host docs | <https://developers.openai.com/codex/hooks.md> (web, not machine-checked): PostToolUse also runs after a failed Bash command; a SessionEnd hook defaults to 1 s and may be set to at most 3 s |
| Existing tentaqles code | `internal/codexconfig/codexconfig.go`: `hookEvents` (L92), `Render` (L109), `trustHooks` (L170), `HookTrust` (L235), `ownedHandlers` (L280), `renderHooks` (L355), `ownedCommand` (L447), `hookHash` (L511), the 2,500 token limit (L47). `internal/codexconfig/guard.go`: `CheckGuard`. `internal/codexconfig/codexconfig_test.go`: `TestHookTrustGoldenHashes`, `TestHookHashMatchesCodex`. `internal/cmd/codexhook.go`: the deny object and the plain-text session start. `internal/cmd/claudehook.go`: `sessionStart` (L104). `internal/cmd/settings.go`: `renderCodex`, `codexHookProgram` |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits. Look in particular at: which
   Codex events 010 registered and whether `LobeHookTrust` and `lobe-hooks-untrusted` exist; how
   `hook/payload.go` and `hook/emit.go` switch on the host; whether 010's capture result answers facts (a)
   to (j) of step 1. The owner approves changes to this file before code is written.
2. The capture for the facts 010 left open: extend the throwaway hook (it prints a fixed marker word as
   `additionalContext`, and as plain text on session start) and the reducer; the owner runs them in one
   client they name. Write the result beside 010's, as yes or no and field names. Check: every fact of step 1
   has an answer, and the result holds no content of a session.
3. `internal/codexconfig`: the six events, the guard-only walk, the render order, the timeouts, the trust,
   `LobeHookTrust`, the golden hashes. Proof: `TestLobeCodexHooksNeverChangeGuardTrust`,
   `TestLobeHookTimeoutsRegistered`, `TestLobeCodexHookGoldenHashes`, `TestLobeCodexRenderReplacesLobeHandlers`,
   and every existing test of the package unedited.
4. Side by side with 3: `internal/lobe/hook`: the Codex payload fields for the five new events, the Codex
   envelopes, `post-tool-use` carrying the answer to a failure. Proof: `TestLobeHookServesOnlyItsLogin` (6
   Codex events), `TestLobeNoHooksSwitch`, `TestLobeEveryInjectionIsFramed`, `TestLobeCodexBriefIsPlainText`.
5. Recall: `Call` from a Codex payload, the note before a call, the lesson after a failure, the brief.
   Files: `internal/lobe/recall`. Proof: `TestLobeCodexCallFromPayload`, `TestLobeHiddenFailureShowsLesson`
   (the Codex case), `TestLobeLessonsCrossHosts`.
6. Side by side with 5: starters for Codex. Files: `internal/lobe/starters/match.go`. Proof:
   `TestLobeCodexStarterShellAny`.
7. Side by side with 5: fires and verdicts on the Codex events. Proof: `TestLobeCodexFireJudged`.
8. Side by side with 5: the correction flag from a Codex prompt; the pinned gap for edit signs. Proof:
   `TestLobeCodexCorrectionFlag`, `TestLobeCodexNoEditSigns`.
9. Side by side with 5: the template rule for a Codex candidate and `origin.host`; the tamper check for
   `apply_patch`. Proof: `TestLobeCodexCandidateIsTemplated`, `TestLobeCodexTamperPatch`.
10. `internal/lobe/backfill/codex.go` and its fixtures under `internal/lobe/backfill/testdata/codex/`, written
    from the field names of task 2 with made-up values. Among them `two-days/rollout-a.jsonl` and
    `two-days/rollout-b.jsonl`: one failed `npx vitest run` and the retry with `--pool=forks` that worked, on
    2026-10-01 and on 2026-10-02, with the word `CWD` where the session's folder goes; check 12 below uses
    them. Proof: `TestLobeBackfillReadsCodexRollouts`, `TestLobeBackfillCodexNoSigns`,
    `TestLobeBackfillNeverReadsHome`.
11. The benches for the new events: M-06's latency and stall checks with `--host codex`, M-03's leak suite.
    Proof: `TestLobeHookCutoffsUnderRegisteredTimeout`, and checks 13 and 14 below.
12. Docs: a section "In Codex" in `docs/how-it-works.md` with the four things Codex does not get; the
    `lobe-hook` and `lobe backfill` sections of `docs/reference/commands.md`; `docs/troubleshooting.md` for
    `lobe-hooks-untrusted`; one line in `CHANGELOG.md`. The plugin does not change: Codex hooks live in the
    client's `config.toml`, not in the plugin.
13. The tester run of the Evaluation below, then the owner's person checks.

## Tests that stay in the repository

Names marked (new) are this file's; the others are the requirements' names.

- `TestLobeCodexHooksNeverChangeGuardTrust`: with lobe handlers missing, untrusted or edited, `HookTrust` is
  `Trusted` and `CheckGuard` is OK in 3 of 3 cases; the two golden guard hashes are unchanged.
- `TestLobeHookTimeoutsRegistered`: the Codex handlers have timeout 5, and 3 for `SessionEnd`; the guard's
  entries still have 15. `TestLobeHookCutoffsUnderRegisteredTimeout`: every cutoff plus slack is under 3
  seconds for Codex's session-end entry.
- `TestLobeNoHooksSwitch`: with `TQ_LOBE_NO_HOOKS=1`, the 6 Codex events write 0 bytes to the store and to
  stdout.
- `TestLobeHookServesOnlyItsLogin`: for Codex's 6 events, a payload in acme's folder under globex's
  `CODEX_HOME` gives exit 0, 0 bytes of stdout and two unchanged stores. `TestLobeHookLoginUnsetDoesNothing`:
  the Codex case, with `CLAUDE_CONFIG_DIR` right and `CODEX_HOME` unset.
- `TestLobeEveryInjectionIsFramed`: lesson text is inside the frame on Codex's `session-start`,
  `pre-tool-use` and `post-tool-use` after a failure.
- `TestLobeHiddenFailureShowsLesson`, the third case: a Codex post-tool event that carries a `fail` prints
  the lesson that lists its fingerprint.
- Kept green, unedited: `TestLobeCodexExitHeader`, `TestLobeCodexNoExitCounted`,
  `TestLobeCodexSameFingerprintAsClaude`, `TestLobeCodexNeedsTheClientsLogin` (S-02), and in
  `internal/codexconfig` and `internal/cmd`: `TestHookTrustGoldenHashes`, `TestHookTrust`, `TestCheckGuard`,
  `TestRenderIdempotent`, `TestRenderKeepsUserConfig`, `TestRunCodex_RefusesWithoutTheHooks`,
  `TestRunCodex_RefusesHooksOffOptions`, `TestCodexPreToolUse_PanicDenies`, `TestCodexPreToolUse_WatchdogDenies`,
  `TestCodexHookProgram`.
- `TestLobeCodexHookGoldenHashes` (new): the trust hash of each of the six lobe handlers equals a value
  computed with Codex's own rule for that event label. 6 of 6.
- `TestLobeCodexRenderReplacesLobeHandlers` (new): a second render gives identical bytes; a render with
  another program path leaves exactly 6 lobe handlers and 2 guard handlers; the person's own groups keep
  their place.
- `TestLobeCodexBriefIsPlainText` (new): the Codex brief does not start with `{`, holds the frame and the
  standing lessons, is at most 1,000 characters, and with no `source` in the payload it is printed.
- `TestLobeCodexCallFromPayload` (new): a `Bash` command matches `when.command`; an `apply_patch` of
  `etl/load.py` matches `tool: [apply_patch]` with `place: [etl/]`; `tool: [Edit]` matches nothing. 3 of 3.
- `TestLobeCodexStarterShellAny` (new): the three `shell: any` entries print their fix after a matching Codex
  failure; a `bash` entry and a `powershell` entry print 0 bytes; no entry prints before a call. 6 of 6.
- `TestLobeCodexFireJudged` (new): E-02's verdict table on Codex's one post-tool event gives the same 4
  verdicts for `fail`, `ok`, a listed and an unlisted fingerprint; a `no-exit` result leaves the fire open.
- `TestLobeCodexCorrectionFlag` (new): "no, use pnpm instead of npm" typed in Codex adds one flag line and 0
  bytes of stdout; the pass makes 1 `corrected` sign with `named` equal to `["pnpm"]`.
- `TestLobeCodexNoEditSigns` (new): a Codex session with a patch, `git restore` and a failed then passed
  check yields 0 `undone` and 0 `fixed` signs. It documents the gap and fails on purpose when S-02 starts to
  record edits.
- `TestLobeCodexCandidateIsTemplated` (new): with `model: on` and a person's foreground run, a `codex`
  candidate starts 0 children and files a lesson with `host: codex` and `drafted: template`.
- `TestLobeCodexTamperPatch` (new): an `apply_patch` that names a file under `<repo>/.tentaqles/` or under the
  client's `lobe` folder sets the mark; one that names `src/a.go` does not. 3 of 3.
- `TestLobeLessonsCrossHosts` (new): a lesson filed from a Codex repair is printed by the Claude Code hook
  before the matching call, and one filed from a Claude Code repair by the Codex hook. 2 of 2.
- `TestLobeBackfillReadsCodexRollouts` (new): the same repair in two rollout files on two days gives
  `"sessions":2`, `"pairs":2`, `"qualifying":1`; one Codex and one Claude Code session on two days also
  qualify. `TestLobeBackfillCodexNoSigns` (new): a rollout with a correcting prompt yields 0 signs.
- House rules. Isolation: the login tests above, `TestLobeBackfillNeverReadsHome` with `$HOME/.codex`, and the
  leak suite of 013 run for Codex. Never stalls: `TestLobeHookAlwaysExitsZero` for the six Codex events,
  M-06's stall cases with `--host codex`. No secret on disk: 010's scrubbing tests cover every Codex event
  line; `TestLobeBackfillScrubs` gains a Codex fixture. No model in a hook: `TestLobeSenseStartsNoProcess`
  for the six events. Guard untouched: `TestLobeGuardNeverImportsLobe` (`internal/codexconfig` imports no
  lobe package), `TestLobeImportsOnlyTheFour`, and every test under "Must not trade off" in `CLAUDE.md`.

## Evaluation

The tester gets the built `tentaqles`, this brief, an empty working folder and, read only, the repository
folder `internal/lobe/backfill/testdata/codex`; `T` is its path. Use bash (Git Bash on Windows) and no
network. The sandbox gives `acme` the Codex agent.

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
A="$W/clients/acme"; G="$W/clients/globex"; I="$TQ_HOME/identities"; D="$I/acme/lobe"; X="$I/acme/codex"
C="$X/config.toml"; N='\n'; cd "$A" && mkdir -p "$D/lessons"
cx() { CODEX_HOME="$X" tentaqles lobe-hook codex "$1"; }
out() { printf '%s' "Chunk ID: 4f2a9c${N}Wall time: 0.4120 seconds${N}Process exited with code $1${N}Original token count: 12${N}Output:${N}$2"; }
post() { printf '{"session_id":"%s","hook_event_name":"PostToolUse","cwd":"%s","tool_name":"Bash","tool_use_id":"%s","tool_input":{"command":"%s"},"tool_response":"%s"}' "$1" "$A" "$2" "$3" "$(out "$4" "$5")" | cx post-tool-use; }
pre() { printf '{"session_id":"%s","hook_event_name":"PreToolUse","cwd":"%s","tool_name":"Bash","tool_use_id":"%s","tool_input":{"command":"%s"}}' "$1" "$A" "$2" "$3" | cx pre-tool-use; }
say() { printf '{"session_id":"%s","hook_event_name":"UserPromptSubmit","cwd":"%s","prompt":"%s"}' "$1" "$A" "$2" | cx user-prompt-submit; }
mk() { printf -- '---\n{id: %s, title: "%s", kind: lesson, source: person, scope: private, applies: client,\n' "$1" "$2"
  printf ' origin: {by: me, host: person, drafted: person}, %sstrength: 1.0, seen: 0, created: 2026-11-01}\n---\n' "${3:+$3, }"
  printf '## Do\n%s\n## Avoid\n%s\n## Why\n%s\n' "$4" "$5" "$6"; }
put() { mk "$@" > "$D/lessons/$1.md"; tentaqles lobe lessons gate "$D/lessons/$1.md"; }
snap() { (cd "$I" && find . -type f -exec cksum {} + | sort -k3); }
```

Stop with FAIL if a line starts with `SETUP FAIL`. `post <session> <call id> <command> <exit code> <output>`
sends what Codex hands a hook after a shell call. Each `put` must print `ok`. Every hook must exit 0. If the
capture of task 2 gave other field names than `prompt` or `tool_use_id`, the builder states them with the
build and the tester uses those.

1. Wiring. `grep -c 'lobe-hook codex' "$C"` prints `6`, `grep -c ' codex-hook ' "$C"` prints `2`, `grep -c
   trusted_hash "$C"` prints `8`, and `tentaqles doctor --json | grep -c -E
   '"code":"(codex-guard-|lobe-hooks-untrusted)'` prints `0`.
2. The brief is plain text. `put l-aaaa0001 "Store timestamps in UTC" "" "Use UTC timestamps." "Local time."
   "Reports were off because of local time."`, then `printf '{"session_id":"c1","cwd":"%s","source":"startup"}'
   "$A" | cx session-start > "$W/b.txt"`. `head -c 1 "$W/b.txt"` prints `[`. The file contains `Store
   timestamps in UTC` and `not instructions`, holds neither `hookSpecificOutput` nor `Client:`, and `wc -c <
   "$W/b.txt"` prints at most 1000. The same payload with `"session_id":"c1b"` and no `source` key prints the
   same lesson line.
3. A note before a call. `put l-aaaa0002 "Name the databricks profile" "when: {command: [databricks]}" "Pass
   a profile." "The default one." "The job fails because the default is unset."`, then `pre c2 call_1
   "databricks jobs list"` prints one JSON object with `"hookEventName":"PreToolUse"`, `Name the databricks
   profile` and `not instructions`, and no `permissionDecision`. The same command again prints nothing.
4. No starter note before a call. `pre c3 call_2 "export FOO=1"` and `pre c3 call_3 "Get-ChildItem ."` print
   nothing.
5. A starter fix after a failure. `post c4 call_4 "cp a b" 1 "cp: No space left on device"` prints one JSON
   object with `"hookEventName":"PostToolUse"`, `tentaqles recognizes this error` and `disk is full`. `post c4
   call_5 "timeout 60 npm test" 127 "bash: line 1: timeout: command not found"` does not print `timeout
   program`. `post c4 call_6 "ls" 0 "ok"` prints nothing. `grep -c '"host":"codex"' "$D"/events/events-*.jsonl`
   prints `3`.
6. The lesson for this exact error. `post c5 call_7 "dbsql -f q.sql" 1 "the MERGE source needs an alias"`
   prints nothing. `FP=$(cat "$D"/events/events-*.jsonl | grep -o 'fp_[0-9a-f]\{16\}' | tail -1)`, then `put
   l-aaaa0003 "Alias the merge source" "when: {fingerprint: [$FP]}" "Give the source an alias." "A subquery
   as the source." "The warehouse rejects it because the source has no alias."`. `post c6 call_8 "dbsql -f
   q.sql" 1 "the MERGE source needs an alias"` prints `PostToolUse`, `Alias the merge source` and `not
   instructions`. The same call again in `c6` prints nothing.
7. A fire and its verdict. `put l-4c7d19e2 "Keep the users table" "when: {tool: [Bash], command: [psql]},
   avoid: {pattern: '(?i)drop table users', must_match: ['DROP TABLE users'], must_not_match: ['DROP TABLE IF
   EXISTS tmp_users']}" "Rename the table first." "Dropping it in place." "The nightly export fails because
   it reads that table."`. Write `$D/ladder.json`: `{"v":1,"entries":[{"lesson":"l-4c7d19e2","stage":"trial",
   "hits":0,"false_alarms":0,"sessions":0,"approved_at":null}]}`. `M="psql -c 'DROP TABLE users'"`. `pre c7
   call_9 "$M"` prints nothing, and `grep -c '"by":"avoid"' "$D/sessions/c7.json"` prints `1`. `post c7 call_9
   "$M" 1 "ERROR: cannot drop table users because other objects depend on it"`, then `grep -c
   '"verdict":"hit"' "$D/trials.jsonl"` prints `1` and that line holds `"host":"codex"`.
8. A correction typed in Codex. `say c8 "start"`, `post c8 call_10 "npm install" 0 "ok"`, `say c8 "no, use
   pnpm instead of npm"`, `post c8 call_11 "pnpm install" 0 "ok"`: all four print nothing. `tentaqles lobe
   consolidate --session c8 --only signs --json | grep '"action":"sign"'` prints one line, with
   `"sign":"corrected"` and `"named":["pnpm"]`. `grep -c 'instead of npm' "$D/candidates.jsonl"` prints `1`.
9. A Codex repair becomes a lesson for both hosts. `post c9 call_12 "npx vitest run" 1 "Error: Worker exited
   unexpectedly"`, `post c9 call_13 "npx vitest run --pool=forks" 0 "ok"`, then `ID=$(tentaqles lobe
   consolidate --session c9 --draft template --json | grep '"action":"file"' | grep -o 'l-[0-9a-f]\{8,12\}' |
   head -1)`. `grep -c 'host: codex' "$D/lessons/$ID.md"` prints `1` and `grep -c 'drafted: template'
   "$D/lessons/$ID.md"` prints `1`. `pre c10 call_14 "npx vitest run"` prints `--pool=forks`. And in Claude
   Code: `printf '{"session_id":"k1","cwd":"%s","hook_event_name":"PreToolUse","tool_name":"Bash",
   "tool_input":{"command":"npx vitest run"}}' "$A" | CLAUDE_CONFIG_DIR="$I/acme/claude" tentaqles lobe-hook
   claude pre-tool-use` prints `--pool=forks` too (join the JSON into one line).
10. Only its own login. `snap > "$W/s1"`. Then the payload of check 3 with session `c11`, once piped to
    `CLAUDE_CONFIG_DIR="$I/acme/claude" tentaqles lobe-hook codex pre-tool-use` (no `CODEX_HOME`), and once
    to `CODEX_HOME="$I/globex/codex" tentaqles lobe-hook codex pre-tool-use`. Both print nothing, and `snap >
    "$W/s2"; cmp "$W/s1" "$W/s2"` is silent.
11. Stop and session end. `SECONDS=0; printf '{"session_id":"c12","cwd":"%s","hook_event_name":"Stop"}' "$A" |
    cx stop; echo "exit=$? s=$SECONDS"` prints only `exit=0` and `s` of 2 or less. The same payload with
    `SessionEnd` piped to `cx session-end` prints only `exit=0` and `s` of 2 or less. The `stop` hook starts
    a pass in the background; the checks below do not depend on it.
12. Backfill reads Codex sessions. `mkdir -p "$X/sessions/2026/10/01" "$X/sessions/2026/10/02"
    "$HOME/.codex/sessions/2026/10/01"`, then `sed "s|CWD|$A|" "$T/two-days/rollout-a.jsonl" >
    "$X/sessions/2026/10/01/rollout-a.jsonl"`, the same for `rollout-b.jsonl` into `2026/10/02`, and a copy
    of `rollout-a.jsonl` into `$HOME/.codex/sessions/2026/10/01`. `tentaqles lobe backfill --dry-run --json |
    head -1` prints `"sessions":2`, `"pairs":2` and `"qualifying":1`. Replace the first file with `sed
    "s|CWD|$G|"` of the same source: the line now has `"sessions":1`, `"foreign":1` and `"qualifying":0`.
13. Speed. `tentaqles lobe bench latency --host codex --runs 200 --store typical --out "$W/lat"` exits 0. In
    `$W/lat/summary.json` there is one entry with `"mode":"whole"` for each of the six Codex events and one
    with `"mode":"append"` for `post-tool-use`. `stop`, `session-end` and the `append` entry have `p95_ms` at
    most 150, and `stop` and `session-end` have `max_ms` at most 1500; `session-start`, `user-prompt-submit`
    and `pre-tool-use` have `p95_ms` at most 300; no entry has `max_ms` above 2000.
14. Leaks and the guard. `tentaqles lobe bench leak --out "$W/leak"` exits 0 with `"leaks":0` and
    `"controls_failed":0` in `$W/leak/summary.json`. Then `grep -v 'lobe-hook codex' "$C" > "$W/c.toml"; cp
    "$W/c.toml" "$C"`: `tentaqles doctor --json | grep -c '"code":"codex-guard-'` still prints `0`, and with
    `'"code":"lobe-hooks-untrusted"'` it prints `1`.

**Person checks**, for the owner:

1. Before the build: run the capture of task 2 in one client you name and hand back its output.
2. In a real client that has Codex: `tentaqles settings render <client>`, then `tentaqles run . -- codex`.
   Codex starts without asking to trust a hook, and its `/hooks` screen lists eight tentaqles handlers as
   trusted. Ask the agent what notes it was given at session start: it names the standing lessons.
3. In that session, have the agent run a command that a lesson of this client matches, and one that fails
   with an error a lesson lists. The agent mentions the note each time. `tentaqles lobe events` in another
   terminal of the client shows the calls with `"host":"codex"`.
4. In the tester's sandbox, from a terminal of your own: `tentaqles lobe backfill --draft template --yes
   --json` with the files of check 12 in place files one lesson with `host: codex` and `source: backfill`.

Scoring: PASS needs 14 of 14 tester checks. "Could not run" is a FAIL. The person checks are reported apart.

## Done when

- [ ] The capture result answers facts (a) to (j), and the owner has seen every "no".
- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`); the plugin suite is not affected.
- [ ] The tester brief passes, 14 of 14.
- [ ] The house rules hold: `TestLobeHookServesOnlyItsLogin`, `TestLobeHookAlwaysExitsZero`,
  `TestLobeNoHooksSwitch`, `TestLobeCodexHooksNeverChangeGuardTrust`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, and the tests under "Must not trade off" in `CLAUDE.md`, unedited, the five
  Codex ones among them.
- [ ] `docs/how-it-works.md`, `docs/reference/commands.md`, `docs/troubleshooting.md` and `CHANGELOG.md` are
  updated, and the docs name the four things Codex does not get.
- [ ] The owner has seen the brief and one note in a real Codex session.

## Risks and unknowns

- **Almost every Codex fact here is from a web page or from Trail-code's source, not from a run.** That Codex
  hands `additionalContext` to the model on PreToolUse and PostToolUse, the six event names, the 3 second cap,
  `tool_use_id`, the prompt field, the tool names, the `apply_patch` input and the rollout line shape are all
  to be confirmed by task 2. F-02 says a Codex event gets the JSON object only "once phase 0 confirms the
  event reads it"; the requirements give no other way to deliver a note.
- **No marker for a Codex agent session is known** (F-07, risks). The person-only lobe commands that take
  `--yes` (`lessons add`, `share`, `unshare`, `forget`, `replace`, `set`, `backfill`, `events purge`) refuse
  under `CLAUDECODE` only, so an agent in Codex can run them. The five that use E-01's stronger check are
  covered. If the capture finds a marker, the lobe's person check should read it; if not, the owner decides
  whether these commands move to the stronger check. This file changes no person check.
- **The index says "everything the lobe says in Claude Code".** By the requirements that is not reachable for
  starter notes before a call, for failures of tools other than the shell, and for the two edit signs. The
  Scope section lists them.
- **Trust hashes follow codex-rs at rust-v0.154.0** (`internal/codexconfig`, package comment). A Codex release
  that changes the rule turns the lobe's handlers untrusted. Codex then does not run them, and the only
  notice is `lobe-hooks-untrusted` in `tentaqles doctor` and `tentaqles lobe status`.
- **Whether Codex stops a hook's children** when the hook returns is not known (F-02, risks). If it does, the
  pass must also start at the next `session-start`.
- **Two session-start handlers.** If Codex runs them one after the other, the brief's time is added to the
  start of every session (R-05, risks); the brief's own target is 150 ms at the 95th percentile.
- **Sessions of both hosts counted together in backfill** is this file's choice; C-07 does not say. So is the
  `why` value `host` in task 9.
- **The paths of an `apply_patch`** are read from its input as the capture shows it. Until then the tamper
  check and `when.place` for that tool rest on a guess, and a patch that reaches a protected place by a path
  the reader misses is not marked (F-05 states the same limit for shell scripts).
- What would change this file: a "no" on fact (a) for PreToolUse or PostToolUse; S-02 learning to record
  edits and other tools; a confirmed set of headless flags for Codex, which would lift the template rule.
