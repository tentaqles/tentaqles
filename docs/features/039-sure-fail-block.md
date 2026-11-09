# 039: Sure-fail block

**Status** not started · **Stage** Blocking · **Entries** E-04, C-08 (step 8, the `timeout-missing` entry),
E-03 (examples of a sure-fail rule) · **Needs** 038 · **Feeds** 041

## Result

With `enforce: block`, a shell command that provably cannot run on this machine is denied before it runs, with
the fix. The first rule is `timeout-missing`: `timeout 60 npm test` on a machine with no `timeout` program.
Nobody approves this rule and it has no record: the proof on every call takes the record's place, and setting
`enforce` to `block` is the person's consent. Under `enforce: advice` the same call gets the fix as a note.
Anything the lobe is not sure about goes ahead with no output.

```bash
# a person, once:           tentaqles lobe set enforce block
# the agent then tries:     timeout 60 npm test        -> denied, with the fix and the way out
tentaqles lobe rules --json                            # timeout-missing sits at "stage":"advice"
tentaqles lobe rules demote timeout-missing            # a person only: the rule goes silent
```

## Scope

**In**

- Package `internal/lobe/surefail`: `surefail.go` (`Decide(call, env)`), `reader.go`, `probe.go`, and the table
  `surefail.Rules()`, built on first use from C-08's catalog.
- The five conditions of E-04 step 2 for one rule, `timeout-missing`.
- The reader that answers "not sure" for every way out it lists, and the probe `probe.ProgramMissing`.
- The decision at PreToolUse for a shell tool on both hosts: a deny under `enforce: block`, the fix as a note
  under `enforce: advice`, nothing under `off`, `lessons: off` or any doubt.
- The deny in E-01's form, with `tentaqles lobe rules demote timeout-missing` as the way out.
- The rule's examples in the catalog, at least 3 `must_not_match` lines, run through `surefail.Decide` by
  `tentaqles lobe rules test timeout-missing` and by a unit test.
- The fire in `trials.jsonl`, with the stored stage `advice` and the `act` `deny` or `note`.

**Out**

- Any second sure-fail rule. A cross-shell rule (a PowerShell cmdlet name in a Bash command) qualifies only
  when a probe can meet step 2; none is designed. No feature takes one.
- The approval path for other starters (see the Scope of [038](038-approve-to-block.md)).
- Zero wrong denies over a command set on three systems (feature [041](041-enforcement-accuracy.md)).

## What it builds on

- 003, 010, 036: `internal/lobe/hook` and the Codex hooks; `internal/lobe/shellwords`, the lobe's own word
  splitter, tested against the guard's cases.
- 014 and 015: `internal/lobe/starters` with `catalog/*.yaml`, `MatchCommand`, `MatchError`, `quoted.go`,
  `programs.go`, and the entry `timeout-missing` (shell `bash`, programs `timeout`, no pattern before the
  call, the error `timeout: command not found\s*$`, `checked: source`). Until now that entry speaks only
  after its error, and it has no `must_match` or `must_not_match` lines, because 014 gives them only to an
  entry with a `pattern`.
- 017 and 023: each starter's ladder entry at `advice` under its catalog id, and its fires in `trials.jsonl`.
- 024: `tentaqles lobe rules demote`, and the automatic demotion on a false alarm.
- 036: how a catalog entry's `shell` is fitted to Codex's shell tool. C-08's risks say Codex does not name
  its shell, so only `shell: any` entries applied there until S-02 supplies it.
- 037: `tentaqles lobe rules test` and its path for a catalog id.
- 038: the deny through F-02's writer on both hosts, the rule that a deny is the only output of a run, and
  the caps, `recall: off` included.

## What it leaves for later features

- `surefail.Decide` and `surefail.Rules()`: 041 replays sure-fail cases twice, under `block` and under
  `advice`, and gates on zero wrong denies.
- `probe.ProgramMissing`: the model for the probe of any later sure-fail rule, which the owner reviews.

## How it works

1. **Which rules qualify.** `surefail.Rules()` holds the catalog entries that meet all five conditions of
   E-04 step 2: built in, a probe, a reader, tests, a fixed fix text. A lesson file never qualifies; the gate
   already refuses `kind: starter` and `source: starter` (C-04). Nothing registers itself: the table is built
   on first use, with no `init` and no package-level call (core page 5.1).
2. **When it runs.** On `pre-tool-use`, for a shell tool only, after 038's decision for approved rules and
   before any note. It needs `lessons` on and `enforce` not `off`, and the rule's stored stage must be
   `advice`. A rule a person demoted sits at `trial` and is silent; it climbs back only on E-01's record.
3. **The reader** (`reader.go`) splits the command with `internal/lobe/shellwords` and answers "not sure"
   unless every `timeout` word is a run of the program with a duration. It is "not sure" for: a command over
   64 KiB, a changed `PATH`, `source`, `.`, `eval`, `hash`, a function with the probed name, an alias with
   it, and a mention of the name that is not a run (`command -v timeout`, a quoted string). Another program
   such as `gtimeout` is no match at all.
4. **The probe** (`probe.go`) reads variables and file entries only. It starts no process, uses no network
   and has 50 ms (proposed). `ProgramMissing` looks in every absolute `PATH` folder, on Windows with each
   `PATHEXT` ending, then beside the first `bash.exe` on `PATH`: that folder, `../usr/bin` and
   `../mingw64/bin`. No `bash.exe` on Windows means "not sure".
5. **The answer.** The rule matched, the reader is sure and the probe proves it:
   - under `enforce: block`, a deny, also when `recall` is off;
   - under `enforce: advice`, the catalog's fix as a note in R-04's block, ahead of lessons, silenced by
     `recall: off`. The proof stands in for C-08's wait for a first matching error in the session, so the
     note may come on the first call.
   A "not sure", an error or a timeout prints nothing, and the call goes ahead.
6. **The deny** has E-01's form and is built from constants only: nothing from the command or its output is
   put into it. Proposed wording: "[tentaqles lobe] Stopped before it ran: built-in rule timeout-missing. No
   timeout program was found on this machine's PATH, so this command cannot run. Usual fix: There is no
   timeout program here. Run the command without it and use the tool's own timeout. If that is wrong for
   this machine, tell the person, who can switch the rule off with: tentaqles lobe rules demote
   timeout-missing". It is at most 600 characters.
7. **No approval, no record needed.** The rule has no entry in `approvals.json` and never a stored `block`.
   It skips E-01's 5 hits and typed `yes` and E-03's replay, and nothing else.
8. **The fire** goes to `trials.jsonl` as 023 records a starter's fire, with the stored stage `advice` and the
   `act` `deny` or `note`. A denied call never runs, so its fire closes as `unknown`. Under `advice` a wrong
   probe shows up as a call that succeeds: a false alarm, and E-07 sends the rule to `trial`.
9. **Tests of the rule.** The catalog entry gains `must_match` and at least 3 `must_not_match` examples, each
   using one of the reader's ways out (proposed). They are run through `surefail.Decide` with the probe
   answering "missing", not through a pattern alone, so each way out is tested by the function the hook
   calls. `tentaqles lobe rules test timeout-missing` runs the same.
10. **Hosts.** In Claude Code the tool `Bash` is bash. For Codex the rule acts only where 036 fits the
    entry's `shell: bash` to the Codex shell tool; where the shell is not known, the answer is "not sure".

## Where to read

| What | Where |
|---|---|
| Requirements | [E-04](../prd/lobe-enforce.md), whole entry; [C-08](../prd/lobe-consolidate.md) steps 5 to 8, the catalog table and its risks; [E-01](../prd/lobe-enforce.md), the caps row and the deny text row; [E-02](../prd/lobe-enforce.md) step 1; [core page 5.1, 5.2 and 5.6](../prd/vertical-lobe.md) |
| Source to port (Trail-code, MIT): decision and reader | [`src/trace/guard-fast.ts` L166](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L166), [L173](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L173), [L118](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L118), [L95](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L95), [L101](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L101), [L44](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L44) |
| Source to port: the probe | [`src/trace/guard-fast.ts` L152](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L152), [L35](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L35), [L129](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L129) |
| Source to port: the consent, the catalog entry | [`src/trace/guard-fast.ts` L79](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L79), [L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L42), [`src/trace/walls.ts` L74](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/walls.ts#L74), [`src/trace/walls.ts` L68](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/walls.ts#L68) |
| Weakness not taken | "this Mac" and Unix-only folders: [`src/trace/guard-fast.ts` L26](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/guard-fast.ts#L26); a commit warning returns before this check: [`src/claude/pre-bash.ts` L270](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/pre-bash.ts#L270) |
| Lobe code from earlier features | `internal/lobe/starters` (`catalog/*.yaml`, `match.go`, `quoted.go`, `programs.go`), `internal/lobe/shellwords`, `internal/lobe/ladder/deny.go`, `internal/lobe/hook/steps.go` |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits: what 036 decided about the
   shell of Codex's tool, how 023 records a starter's fire, how 038's decision is wired into `steps.go`. Fix
   the expected result of evaluation check 10 from the first answer. The owner approves before code is written.
2. `reader.go`, with a table test of the ways out. Each ported file starts with the `// Ported from` line
   and is listed in `NOTICE` (F-07). Proof: `TestLobeSureFail_WaysOut`.
3. Side by side with 2: `probe.go`, with the folder list injected so a test needs no real `PATH`. Proof:
   `TestLobeSureFail_TimeoutMissing`, `TestLobeSureFail_ProbeBudget`.
4. After 2 and 3: `surefail.go` (`Decide`, `Rules()`), the catalog examples, and the catalog path of
   `tentaqles lobe rules test`. Proof: `TestLobeSureFail_FileCannotQualify`,
   `TestLobeStartersCompileAndSelfTest` still green.
5. After 4: the step in `pre-tool-use` for both hosts, the deny text, the note, the fire. Proof:
   `TestLobeSureFail_FollowsTheSetting`, `TestLobeSureFail_SurvivesRecallOff`.
6. Docs: `docs/reference/commands.md` (what `enforce: block` now also does, under `tentaqles lobe set` and
   `tentaqles lobe rules`), `CHANGELOG.md`. The plugin does not change.
7. Tester run of the Evaluation below, on Windows and on one Unix system, then the person checks.

## Tests that stay in the repository

- `TestLobeSureFail_TimeoutMissing`: no `timeout` anywhere gives a deny; one in a `PATH` folder gives none:
  2 of 2.
- `TestLobeSureFail_WaysOut`: 10 commands, one per way out in the reader row (9, a function and an alias
  counted apart) and `gtimeout 5 x`, give 0 denies.
- `TestLobeSureFail_FollowsTheSetting`: on a new store with no `approvals.json`, the same call is denied under
  `enforce: block`, gets the fix as a note under `advice`, and gets nothing under `off`: 3 of 3.
- `TestLobeSureFail_SurvivesRecallOff`: with `recall: off` the call is denied under `enforce: block`, gets no
  note under `advice`, and gets nothing under `block` with `lessons: off`: 3 of 3.
- `TestLobeSureFail_FileCannotQualify`: a lesson file that claims `kind: starter`, and one that claims
  `source: starter`, never deny: 2 of 2.
- `TestLobeSureFail_ProbeBudget`: with the probe stalled 2 seconds the hook answers by its cut-off with no
  deny.
- `TestLobeSureFail_FixedText` (this file's name): for 100 commands that hold a marker string, the deny holds
  the marker 0 times.
- `TestLobeSureFail_BothHosts` (this file's name): the Claude Code hook and the Codex hook give the answer
  task 1 fixed for each, and nothing under the other client's login.
- House rules kept green: `TestLobeHookAlwaysExitsZero`, `TestLobeHookNeverPrintsAllow`,
  `TestLobeHookServesOnlyItsLogin`, `TestLobeGuardNeverImportsLobe` (no `init`, no package-level call),
  `TestLobeImportsOnlyTheFour`, `TestLobeShellwordsSameCases`, `TestNoticeListsEveryPort`, the latency and
  stall checks of 012 with this step in the hook, the leak suite of 013, and every test under "Must not trade
  off" in `CLAUDE.md` unedited.

## Evaluation

Run the standard tester setup of [002](002-sandbox.md), which is F-07's block.
`<acme>` is `$W/clients/acme`, `<lobe>` is `$TQ_HOME/identities/acme/lobe` and `<work>` is `$W`. Backticked
text split over two lines is one line. Create the empty folder `<work>/bin`; on Windows put an empty file
`bash.exe` in it. Run every hook inside `<acme>` with `PATH` set to `<work>/bin` alone, with
`CLAUDE_CONFIG_DIR` set to `$TQ_HOME/identities/acme/claude` and `CODEX_HOME` set to
`$TQ_HOME/identities/acme/codex`, calling the `tentaqles` binary by its full path. "Set `enforce` to X" means
`lobeset acme enforce X`, run with the shell's ordinary `PATH`. No tester runs `tentaqles lobe set`. Set
`enforce` to `block`. Write no `ladder.json` and no `approvals.json`: the rule is built in. Input A, piped to
`tentaqles lobe-hook claude pre-tool-use`: `{"session_id":"s1", "hook_event_name":"PreToolUse",
"cwd":"<acme>", "tool_name":"Bash", "tool_use_id":"t1", "tool_input":{"command":"timeout 60 npm test"}}`.

1. Input A: exit code 0; stdout has `"permissionDecision":"deny"` and
   `tentaqles lobe rules demote timeout-missing`. `<lobe>/approvals.json` does not exist.
2. Input A with the command `command -v timeout && timeout 60 npm test`: exit code 0, no deny decision.
3. Input A with the command `PATH=/opt/x/bin:$PATH timeout 60 npm test`: exit code 0, no deny decision.
4. Create an executable file `timeout` in `<work>/bin` (on Windows, `timeout.exe`), then input A: no deny
   decision.
5. Delete that file and set `enforce` to `advice`, then input A with `session_id` `s2`: exit code 0, no deny
   decision, and stdout contains `timeout` (the fix, as a note).
6. Set `enforce` to `off`, then input A with `session_id` `s3`: exit code 0 and empty stdout.
7. `tentaqles lobe rules --json`: the entry for `timeout-missing` has `"stage":"advice"`.
8. Run `lobeset acme enforce block` and `lobeset acme recall off`, then input A with `session_id` `s5`: stdout
   has `"permissionDecision":"deny"`. Then run `lobeset acme recall on`.
9. Input A with `session_id` `s6` and the command `timeout 60 echo SECRETMARK`: stdout has a deny decision and
   does not contain `SECRETMARK`.
10. Input A with `session_id` `s7`, piped to `tentaqles lobe-hook codex pre-tool-use`: the result build task 1
    recorded for Codex on this system, which is either the deny of check 1 or exit code 0 with empty stdout.
    Any other output is a FAIL.
11. With `CLAUDE_CONFIG_DIR` set to `$TQ_HOME/identities/globex/claude`, input A with `session_id` `s8`: exit
    code 0 and empty stdout.
12. `tentaqles lobe rules test timeout-missing`, with the shell's ordinary `PATH`: exit code 0.

**Person checks**, for the owner, in a terminal with the tester's `HOME`, `USERPROFILE` and `TQ_HOME`
exported (`tentaqles lobe rules demote` answers only to a person). The tester sets `enforce` to `block` first
and pipes the input.

1. `tentaqles lobe rules demote timeout-missing`: exit code 0; `<lobe>/ladder.json` has `"stage":"trial"` for
   the rule, and `<lobe>/ladder-log.jsonl` has a line with `"why":"person"`.
2. Input A with `session_id` `s4`: exit code 0 and empty stdout. The demoted rule is silent.
3. In a real session of a client the owner names, on the owner's Windows machine, with `enforce: block`: ask
   the agent to run `timeout 5 echo hi`. If Git Bash there has a `timeout` program, the call runs and nothing
   is said. If it has none, the call is stopped with the fix. Either result passes; a deny of a command that
   would have run is a FAIL and goes to the risks below.

PASS needs 12 of 12 from the tester on each system and 3 of 3 from the owner.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) on the three systems; the plugin suite is untouched.
- [ ] The tester brief passes, 12 of 12, on Windows and on one Unix system, and the person checks pass.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookNeverPrintsAllow`,
  `TestLobeHookServesOnlyItsLogin`, `TestLobeGuardNeverImportsLobe`, `TestLobeSureFail_ProbeBudget`, the
  latency and stall checks, the leak suite at zero leaks, the guard's tests unedited.
- [ ] `docs/reference/commands.md`, `NOTICE` and `CHANGELOG.md` are updated.
- [ ] The owner has seen `timeout 60 npm test` stopped with the fix, and the same call noted under `advice`.

## Risks and unknowns

- **A login profile can add `PATH` folders the probe never sees.** The rule then denies a command that would
  have run. The deny names `demote`, and `enforce` is one step away. Person check 3 looks for it.
- **Windows.** Which `bash.exe` each host runs is unconfirmed, and Trail-code's rule on Windows was read, not
  run. Windows also ships its own `timeout.exe` in the system folder; when that folder is on `PATH` the probe
  finds it and the rule stays silent, which is the safe side. Whether Git Bash would run it is not checked.
- **Codex.** Whether `shell: bash` applies to Codex's shell tool depends on 036. Check 10 takes its expected
  result from task 1 for that reason. If 036 left it open, the Codex answer is "not sure" on every system.
- **`timeout-missing` has no pattern before the call** in C-08's catalog. This feature gives it one voice
  before the call, the reader. If 014 gave it a pattern, task 1 reports it.
- **The deny wording in step 6 is proposed.** E-04 fixes its form (the proved fact, the usual fix, the way
  out) and that the fix is a constant; it does not give the sentence.
- **The source proves one rule.** Every further sure-fail rule is our own design and needs its probe reviewed
  by the owner (E-04, risks).
- **No record stands behind the first deny.** The proof on every call and 041's zero wrong denies are what
  stand in for it. 50 ms and "at least 3 examples" are proposed.
