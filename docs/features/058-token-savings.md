# 058: Token savings accounting

**Status** not started · **Stage** Graph · **Entries** M-01; F-07 (the `status --savings` row); M-08 (rule 7) ·
**Needs** 050 · **Feeds** 059

## Result

A person asks what the lobe put in front of the agent and what that probably saved, per client. Every token
figure is an estimate and says so on its own line. Nothing about savings is ever printed into the agent's
context.

```bash
cd ~/clients/acme/app
tentaqles lobe status --savings            # the last 7 days (proposed), each token line marked "estimate"
tentaqles lobe status --savings --days 30
tentaqles lobe status --savings --json     # one object with a "savings" key
```

The text shows three groups: what was injected (count, bytes, estimated tokens), what graph answers probably
saved as a range from low to high, and the error counters. The limits of the estimate are printed with the
numbers.

## Scope

**In**

- Package `internal/lobe/stats`: `stats.go` (`Line`, `Append`, `Sum`) and `tokens.go` (`ToTokens`).
- One stats line from every lobe code path that prints text for the agent, in both hosts: the session brief,
  the prompt block, lesson and starter-rule notes before a call and after a failure, the blast-radius list with
  the argument note, the commit warning, a deny, and the five agent commands under `tentaqles lobe graph`.
- The baselines `low` and `high` of a graph answer, the 400,000-byte cap (proposed), and the rule that a saving
  is never negative.
- The error counters of M-01 step 5 except `document_tokens`.
- `tentaqles lobe status --savings [--days N] [--json]`, a new flag on the `status` verb of 005.
- Keeping stats files 90 days (proposed): the sweep of 011 gains this one rule.
- The printed limits (M-01 step 7) and the word `estimate` on every token line.

**Out**

- Measured figures. They come only from the harness ([059](059-token-benchmark.md)), which prints this estimate
  beside the measured difference for the same runs (M-01 step 7). A figure from this command is never quoted as
  a benchmark result (M-08 rule 7); `check-claims` of [021](021-benchmark-harness.md) holds that.
- `document_tokens`, the counter of the document reading step ([061](061-pdfs-and-images.md)).
- Stats lines from the tools of the optional MCP server ([062](062-mcp-server.md)).
- Dollars. No price table lives in the product (M-01, "How to build it").
- Deciding R-01's ranking constants or R-02's budget and coverage floors. Their risks say "M-01 decides" and
  046 and 050 point here. This feature delivers the counts; a change to a constant is a change to 046 or 050,
  made by the owner after reading them. Counters cannot test a ranking, so nothing here confirms R-01's weights.
- Any line, hint or tally shown to the agent. The agent is never asked to recite a total.

## What it builds on

- [003](003-shell-failure-claude-code.md): `internal/lobe/store` (`Append`, the store lock, `ErrBusy`), the
  event log, `internal/lobe/hook` (`Main`, `emit`, the `Answer` type), and the client rule.
- [005](005-status-and-capture-switch.md): `tentaqles lobe status [--json]` in `internal/lobe/cli`, to which the
  flag is added.
- [006](006-error-fingerprints.md) and [007](007-duplicate-filter.md): `fingerprints.json`, and the reference
  lines the duplicate filter writes, which count as failures (S-06).
- [011](011-retention-and-sweep.md): the sweep that deletes expired day files.
- [023](023-fires-and-verdicts.md): `trials.jsonl`, with `act` and `verdict` on every fire.
- [025](025-model-drafting.md): the launcher in `internal/lobe/consolidate/child.go`, whose child answers in the
  CLI's JSON.
- The printing paths, each of which gains one call: 014 and 015 (starter rules), 017 and 020 (lesson notes),
  026 (the brief), 036 (the same in Codex), 038 and 039 (denies), 040 (the commit warning), 043, 045 and 046
  (`outline`, `callers`, `find`, `map`), 049 (`impact` and the blast list), 050 (the prompt block), 051 (the
  argument note), 056 (`find --all`, `impact --all`).
- [046](046-find-and-map.md) and [048](048-graph-stays-current.md): R-01's index under `graph/<repo-key>/`, which
  the build writes and every hook reads in place of `graph.jsonl`.

## What it leaves for later features

- `stats.Append(store, Line)` and `stats.ToTokens(bytes)`, used by 061 (`document_tokens`) and 062 (one line per
  MCP tool call).
- `stats/stats-YYYYMMDD.jsonl` and the line shape below.
- `tentaqles lobe status --savings --json` with its `savings` object, which the harness of 059 reads from each
  run's home to fill "the lobe's injected bytes" and to print the estimate beside the measured difference.
- The counts R-02 and R-03 wait for: how often a weak match stayed silent cannot be counted (no line is
  written), but how often each agent command is called can.

## How it works

1. **The line.** After its output is flushed, a path that printed text for the agent appends one line to
   `stats/stats-YYYYMMDD.jsonl` (the day in UTC, like the event log):
   `{"v":1,"at":"2026-11-09T14:03:11Z","session":"abc","part":"graph","sent":412,"low":1830,"high":9644}`.
   A call that prints nothing writes no line. The append follows F-01: one whole line, and when it fails it is
   dropped and the hook's result does not change. A line holds numbers and ids only, never text.
2. **`sent`** is the exact UTF-8 byte length of everything the agent receives from that call, headers and
   F-05's frame included. For a hook it is the decoded `additionalContext` or deny reason; for a command it is
   what went to stdout.
3. **`part`.** M-01 shows one value, `graph`. This file proposes a closed list, for the owner to confirm at
   task 1: `graph` when the text names code from the graph (the prompt block when it holds a pointer, the blast
   list, an agent command), `lesson` for every other note (the brief, lesson and starter notes, the argument
   note alone, the commit warning) and `deny`. A line from an agent command also carries `tool` (`find`,
   `callers`, `outline`, `map` or `impact`; proposed here), because R-03 asks this entry to show that the
   commands get called. A command has no session id, so its line leaves `session` out.
4. **Baselines of a graph answer.** `high` is the summed size of the distinct files the answer names: Trail-code's
   baseline, each file read whole. `low` is the size of only the symbol spans it points to. A file named twice
   counts once. A file missing from the graph counts 0. An answer that names no file has no baseline, and its
   line has neither key. `high` is capped at 400,000 bytes per line (proposed); a capped line carries
   `"capped":true` and counts at the cap.
5. **Where the sizes come from.** No hook reads a source file or `graph.jsonl` for accounting. The build of
   048 already holds every file's bytes, so this feature widens R-01's index with two numbers: the byte length
   of each node's span, and for a file node the file's size. This is this file's choice; G-02's node line has no
   byte length, and M-01 does not say where the number comes from.
6. **Tokens** are `(bytes + 2) / 4` in integer math. A line saves `ToTokens(max(0, baseline - sent))`, once with
   `low` and once with `high`, so the saving is a range. An answer larger than its baseline saves 0. A lesson
   has no reading baseline; its value shows in the error counters.
7. **Error counters.** M-01 names them and does not say where each is kept. This file reads each from the
   record that already holds the fact, and adds a stats line only where no record exists:

   | Counter | Read from |
   |---|---|
   | `failures` | Event lines of kind `fail` or `hidden-fail` in the window, the duplicate filter's reference lines included (S-06) |
   | `fingerprints` | Entries of `fingerprints.json` whose `last_seen` is in the window (S-08) |
   | `repeats_after_lesson` | Those failures whose `fp` a live lesson lists under `when.fingerprint`, counted from the lesson's `created` date on |
   | `advice_shown` | Lines of `trials.jsonl` with `"act":"note"` (E-02) |
   | `advice_then_ok` | Of those, the lines whose call then succeeded: verdict `pass` or `false_alarm` |
   | `blocks` | Lines of `trials.jsonl` with `"act":"deny"` |
   | `draft_tokens` | A stats line `{"v":1,"at":"...","part":"draft","tokens":N}` that the drafting step appends after each child returns, N being the billed tokens the CLI's JSON reports for that session |

   Events live for `retention_days` (14 by default), so with `--days` above that the three event counters cover
   fewer days than asked. The output says so in one line when it happens.
8. **The command.** `tentaqles lobe status --savings [--days N] [--json]` sums the last N days (default 7,
   proposed). It is not person-only and follows F-07 step 3: the current folder's client, and inside an agent
   session it refuses under another client's login. With `--json` the status object gains
   `"savings":{"injected":{"count","bytes","tokens_est"},"graph":{"answers","sent_bytes","low_bytes",
   "high_bytes","saved_low_tokens_est","saved_high_tokens_est","capped"},"errors":{...}}`. `injected` sums every
   line with `sent`; `graph` sums the lines that have a baseline; `errors` holds the seven counters of step 7.
9. **The limits, printed with the numbers** (M-01 step 7): bytes divided by 4 is not a tokenizer; `high` assumes
   whole files would have been read and `low` only the right spans, and neither is a bound; `advice_then_ok`
   does not prove the advice prevented an error; injected text is billed again on later turns and the estimate
   ignores that. Every line of the text output that shows tokens contains the word `estimate`.
10. **Never parsed back.** Counters are counted where the text is produced. Tool output that holds a savings
    figure, such as `tokens saved ≈ 99,999`, changes nothing.
11. **Retention.** Stats day files are kept 90 days (proposed) and deleted by the sweep. The `draft` lines hold
    a number and a time only.
12. **Switches and isolation.** A path that is silent because of a setting writes no line. Stats sit in the
    client's own store, so a session under another client's login reads and writes none (core page 5.1).

## Where to read

| What | Where |
|---|---|
| Requirements | [M-01](../prd/lobe-benchmarks.md), all of it; [M-08](../prd/lobe-benchmarks.md) rule 7; [F-07](../prd/lobe-foundation.md) step 1 (the row) and step 3; [F-01](../prd/lobe-foundation.md) steps 1 and 7; [E-02](../prd/lobe-enforce.md), the `trials.jsonl` line; [S-06](../prd/lobe-sense.md), "Why it matters" and risks; [R-04](../prd/lobe-recall.md), the session record bullet ("from phase 4 the same step appends M-01's line"); [core page 5.2 and 5.3](../prd/vertical-lobe.md) |
| Source to port | [`src/context/savings.ts` L28](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/context/savings.ts#L28): tokens are characters divided by 4, rounded |
| Source to port, changed | [`src/context/savings.ts` L117](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/context/savings.ts#L117): the output is measured without the savings header that L156 puts in front of it; ours counts everything sent |
| Not taken | [`src/graph/map.ts` L283](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/graph/map.ts#L283): a baseline of every file in the graph; [`src/claude/hooks.ts` L272](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/hooks.ts#L272): the total parsed back out of tool output; [`src/context/price.ts` L19](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/context/price.ts#L19): a price table fixed in code |
| Existing code | `internal/lobe/hook` (`emit`, where the bytes are known), `internal/lobe/cli` (the `status` verb), `internal/lobe/query/render.go` (the one renderer of the agent commands), `internal/lobe/store` |

## Build tasks

1. Check this brief against the code as it stands and list what no longer fits. List every place that prints
   for the agent (grep for the `Answer` type and for `render`), and compare with step 1. Put three choices to
   the owner: the `part` list and the `tool` key (step 3), the index as the source of sizes (step 5), and the
   sources of the error counters (step 7). Files: none. Check: a written list the owner approves.
2. `internal/lobe/stats`: `tokens.go`, `stats.go` with `Line`, `Append` and `Sum`. Tests:
   `TestLobeStatsTokensRound`, `TestLobeStatsNeverNegative`, `TestLobeStatsLinesHoldNoText`.
3. Sizes in the index: the build writes the two numbers of step 5; a reader accepts an index without them and
   then records no baseline. Tests: `TestLobeStatsBaselines`, and `TestLobeGraphIncrementalEqualsCold` stays
   green. Can run beside task 2.
4. The hook paths: one `stats.Append` in `hook.Main` after `emit`, fed by what the steps returned, so no step
   counts its own text. Tests: `TestLobeStatsSentCountsHeader`, `TestLobeStatsAppendFailureFailsOpen`,
   `TestLobeStatsSilentCallWritesNoLine`, `TestLobeStatsBothHosts`. Needs task 2.
5. The agent commands: one `stats.Append` after `render` in `internal/lobe/query`. Test:
   `TestLobeStatsCountsEachAgentCommand`. Can run beside task 4.
6. The error counters in `Sum`, and the `draft` line in `internal/lobe/consolidate`. Tests:
   `TestLobeStatsErrorCounters`, `TestLobeStatsIgnoresSavingsText`. Can run beside tasks 4 and 5.
7. `--savings`, `--days` and the `savings` object in `internal/lobe/cli`, with the limits and the word
   `estimate`. Test: `TestLobeStatsEveryTokenLineSaysEstimate`. Needs tasks 2 and 6.
8. The 90-day rule in the sweep. Test: `TestLobeStatsSweepKeepsNinetyDays`.
9. Docs: the `tentaqles lobe status` section of `docs/reference/commands.md`, and `CHANGELOG.md` under
   `[Unreleased]`. The plugin does not change.
10. The tester run of "Evaluation", by an agent that built none of this.

## Tests that stay in the repository

- `TestLobeStatsTokensRound`: `ToTokens` of 0, 1, 2, 6 and 8000 gives 0, 0, 1, 2 and 2000.
- `TestLobeStatsSentCountsHeader`: a 40-byte body with a 20-byte header records `sent` 60.
- `TestLobeStatsNeverNegative`: baseline 100 bytes, sent 400 bytes, saving 0.
- `TestLobeStatsIgnoresSavingsText`: tool output holding `tokens saved ≈ 99,999` changes no counter.
- `TestLobeStatsAppendFailureFailsOpen`: with the stats folder unwritable the hook still exits 0 with its
  output.
- `TestLobeStatsBaselines` (new here): an answer naming one file twice counts it once; a file missing from the
  graph counts 0; an answer naming no file has no `low` and no `high`; a baseline over 400,000 bytes is cut to
  it and marked `capped`. 4 of 4.
- `TestLobeStatsSilentCallWritesNoLine` (new here): a prompt with only a weak match, `recall: off`, and a
  neutral folder each write 0 lines. 3 of 3.
- `TestLobeStatsCountsEachAgentCommand` (new here): `find`, `callers`, `outline`, `map` and `impact` each add 1
  line that names its `tool`. 5 of 5.
- `TestLobeStatsBothHosts` (new here): the same prompt through `lobe-hook claude` and `lobe-hook codex`, each
  under its own login variable, adds 1 line each with equal `sent`.
- `TestLobeStatsErrorCounters` (new here): a fixture store with 3 failures (one a reference line), 2
  fingerprints, 1 failure after a lesson that lists its fingerprint, 2 notes of which 1 then passed, and 1 deny
  gives `failures` 3, `fingerprints` 2, `repeats_after_lesson` 1, `advice_shown` 2, `advice_then_ok` 1,
  `blocks` 1.
- `TestLobeStatsEveryTokenLineSaysEstimate` (new here): 0 lines of the text output hold a token figure without
  the word `estimate`, and the four limits are printed.
- `TestLobeStatsLinesHoldNoText` (new here): after 20 mixed calls whose inputs hold a planted secret and a
  canary word, no stats file holds either, and every line parses to numbers, ids and the keys of step 1.
- `TestLobeStatsSweepKeepsNinetyDays` (new here): a stats file 91 days old is gone after a sweep and one 89
  days old stays.
- House rules kept green: M-06's latency and stall tests of 012 (`tentaqles lobe bench latency` must not move
  past its limits with the extra append), `TestLobeLeak_AllAttacksZeroLeaks`,
  `TestLobeCommandRefusesAnotherLogin`, `TestLobeCommandTable` (one new flag, no new command),
  `TestDocsReferenceEveryCommand`, the import test of F-02, and every test under "Must not trade off".

## Evaluation

Tester brief. Start from the standard tester setup of [002](002-sandbox.md) (F-07): bash, the built `tentaqles`
first on `PATH`, `W` the working folder, the sandbox with `acme` and `globex`. No network. Run
`git init "$W/clients/acme/m01"`; `M` is that folder. Commit two files in it: `a.py`, exactly 4,000 bytes,
starting with the lines `def canary_upsert(a, b):` and `    return a` and filled to size with lines of `#`; and
`tiny.py`, holding the one line `def canary_tiny(): return 1`. In `M` run `tentaqles lobe graph build`; expect
exit 0. Export `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/acme/claude"`. In each JSON below, `<M>` stands for the
path in `M`.

1. In `M`, pipe `{"session_id":"m01","hook_event_name":"UserPromptSubmit","cwd":"<M>",
   "prompt":"where is canary_upsert defined"}` into `tentaqles lobe-hook claude user-prompt-submit`. Expect exit
   0 and injected text containing `a.py` (the decoded `additionalContext` string if stdout is JSON, otherwise
   all of stdout). Call its UTF-8 byte length S.
2. `tentaqles lobe status --savings --json`, under `savings`: `injected.count` is 1, `injected.bytes` is S,
   `graph.high_bytes` is 4000 and `graph.saved_high_tokens_est` is `(4000 - S + 2) / 4` rounded down.
3. Repeat check 1 with the prompt `where is canary_tiny defined`. `injected.count` is now 2 and
   `graph.saved_high_tokens_est` is unchanged: an answer larger than its baseline saves 0.
4. Pipe `{"session_id":"m01","hook_event_name":"PostToolUse","cwd":"<M>","tool_name":"Bash",
   "tool_input":{"command":"echo done"},"tool_response":{"stdout":"[trail] tokens saved ≈ 99,999","stderr":""}}`
   into `tentaqles lobe-hook claude post-tool-use`. Expect exit 0 and the `graph` and `injected` numbers
   unchanged.
5. In `M` run `tentaqles lobe graph find canary_upsert`. Expect exit 0. Then `injected.count` is 3 and
   `graph.answers` is one more than before the command.
6. In `$W/clients/globex/app`, with `CLAUDE_CONFIG_DIR="$TQ_HOME/identities/globex/claude"`,
   `tentaqles lobe status --savings --json` shows 0 for every counter under `savings`. Then set the variable
   back.
7. In `M`, every line of `tentaqles lobe status --savings` that shows tokens contains the word `estimate`.
8. `grep -rl canary "$TQ_HOME/identities/acme/lobe/stats"` prints nothing: stats lines hold no text.
9. Pipe `{"session_id":"m02","hook_event_name":"UserPromptSubmit","cwd":"<M>","prompt":"ok"}` into the hook of
   check 1. Expect exit 0, empty stdout, and `injected.count` still 3: a silent call writes no line.

**Person checks** (the owner): none. No command of this feature answers only to a person. The owner reads
`tentaqles lobe status --savings` in a client of their own after a day of work, for the last gate below.

Scoring: PASS needs 9 of 9. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`).
- [ ] The tester brief passes, 9 of 9.
- [ ] The house rules hold: `TestLobeStatsAppendFailureFailsOpen`, `TestLobeStatsLinesHoldNoText`, the latency
  and stall checks of 012 within their limits, `TestLobeLeak_AllAttacksZeroLeaks`, the guard's tests untouched.
- [ ] `docs/reference/commands.md` and `CHANGELOG.md` updated.
- [ ] The owner has seen the result described under Result in a client of their own, and no token figure
  without the word `estimate`.

## Risks and unknowns

- Proposed numbers: the 7-day default, the 400,000-byte cap, the 90 days of stats.
- Three choices here are this file's, not the requirements': the values of `part` and the `tool` key, sizes
  carried in the index, and counters read from the event log, `fingerprints.json` and `trials.jsonl`. Core page
  5.3 calls the stats files "token and error counters", which can also be read as one stats line per counted
  error. Task 1 settles it before any code.
- Whether the headless CLI's JSON answer reports billed tokens is unconfirmed (C-03's flags come from reading
  Trail-code, not from a run). If it does not, `draft_tokens` shows the number of drafting calls and says so.
- The range may be too wide to mean much, and people will quote the high figure. M-08 forbids quoting it.
- A loop of 20 identical failures weighs 20 in `failures` (S-06 risks). Whether it should weigh 1 is for the
  owner after seeing real numbers.
- Open in M-01: whether 90 days of number-only lines is acceptable next to decision A-9.
- One more append on every printing hook. If the latency check of 012 moves past its limit, the append is
  dropped before any output is.
