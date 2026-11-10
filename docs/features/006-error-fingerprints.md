# 006: The same error gets the same fingerprint

**Status** not started · **Stage** Sense · **Entries** S-08 (the fingerprint and its verb; not the fold); F-07
(the fingerprint count of `lobe status`) · **Needs** 004 · **Feeds** 007, 009

## Result

Every failure line in a client's event log carries `fp`, a short id for "the same error". Two failures that
differ only in a path, a line number, an id or a quoted value carry one `fp`. Another cause, or the same text
from another kind of tool, carries another. A person can ask for the fingerprint of any error text, with no
client and no store:

```bash
printf '%s' '/usr/bin/bash: line 1: Get-ChildItem: command not found' | tentaqles lobe fingerprint --tool Bash
printf '%s' '/bin/bash: line 12: Get-ChildItem: command not found' | tentaqles lobe fingerprint --tool Bash
```

Both print the same two lines:

```
fp_b4f3e92d90787db6
<path>: line <n>: Get-ChildItem: command not found
```

In the log, from the sandbox of [002](002-sandbox.md) with acme's login exported, two failed calls of one MCP
tool with the errors `widget 4411 is not frobnicated` and `widget 97 is not frobnicated` leave two lines that
both hold `"fp":"fp_48622a622f4928cc"`, and `tentaqles lobe status` counts 2 events and 1 fingerprint.

## Scope

**In**

- The package `internal/lobe/fingerprint` with `Class`, `Signature`, `Template` and
  `Of(tool, errText) (fp, template)`, all pure: version `fp1`, S-08 steps 1 to 4.
- The call from `sense.Record`: after scrubbing (S-05) and before the append (S-07), every `fail` event gets
  its `fp`, in the line and in `sense.Outcome.FP`.
- The verb `tentaqles lobe fingerprint --tool <name>` and its section in `docs/reference/commands.md`.
- The fingerprint count in `tentaqles lobe status`, which [005](005-status-and-capture-switch.md) left to
  this feature: the number of distinct `fp` values in the log.
- The test vectors of S-08, as unit tests and as the tester brief below.

**Out**

- `fingerprint.Fold` and the file `fingerprints.json` (S-08 step 4, second half). The fold runs only in the
  detached pass, and no pass exists before [018](018-repair-pairing.md). The index names no feature for the
  fold. This file leaves it to 018, whose pass order already has a place for it, and the writer's report says
  so. Nothing here can call a fold, and a tester could not watch one.
- The fingerprint of a `hidden-fail` event ([009](009-hidden-failures.md)) and of a Codex failure
  ([010](010-shell-failure-codex.md)). Both call `fingerprint.Of` as it is built here.
- The duplicate filter's use of `fp` ([007](007-duplicate-filter.md)).
- Lessons that list a fingerprint and the trigger after a failure ([019](019-lesson-from-repair.md),
  [020](020-lesson-after-failure.md)); the verdict on a fire ([023](023-fires-and-verdicts.md)).
- The Drain merge layer, `alias_of` and a second mask list `fp2` (S-08, "When Drain-style clustering is
  added"). They wait for a measurement on labeled failures in phase 2. The index names no feature for that
  measurement; the writer's report says so.

## What it builds on

- [002](002-sandbox.md): `tentaqles lobe sandbox` and the standard tester setup.
- [003](003-shell-failure-claude-code.md): `internal/lobe/hook` (`Main`, the client rule of F-02 step 4, the
  cutoffs); `internal/lobe/scrub` (`Text`, `Clean`, the markers `[redacted:<kind>]`); `internal/lobe/eventlog`
  (`Event`, `Append`); `internal/lobe/sense` (`FromClaudeFailure`, `Record`, `Outcome`); the rule of S-01 step
  5 that moves a first line `Exit code N` of a shell tool into `exit`; the import tests
  `TestLobeGuardNeverImportsLobe` and `TestLobeImportsOnlyTheFour`; the `// Ported from` header and `NOTICE`.
- [004](004-any-tool-failure.md): failure lines for every tool, so every tool class of S-08 step 1 has
  events, and the one `ok` line that exists so far (a search that found nothing).
- [005](005-status-and-capture-switch.md): `internal/lobe/cli` and its registration in `internal/cmd/lobe.go`;
  `eventlog.Scan`; `tentaqles lobe events`, which prints the lines; `tentaqles lobe status` with its `events`
  counts; the `capture` switch, which `Record` checks before this step.

003 wrote events with no `fp`, and its `Outcome` has no `FP`. This feature adds both: `fp` with `omitempty`
(S-01, "How to build it").

## What it leaves for later features

- `fingerprint.Of(tool, errText) (fp, template)`: 009, 010, 018 (the fold works the template out again from a
  line's `tool` and `error`) and 035 (the backfill fingerprints old calls with the same code, C-07).
- The package `internal/lobe/fingerprint` itself: 018 adds `Fold(lobeDir string)` and `fingerprints.json` to
  it. 018's file says it uses the fold "if it was built". It was not.
- `fingerprint.Class(tool)`: 018 (the `tool_class` of a fold entry) and 058 (M-01's counters).
- The key `fingerprints` inside `events` of `tentaqles lobe status --json` (name proposed here): a count of
  distinct `fp` values in the day files. 018 may add the fold's longer-lived counts beside it; no key is
  renamed.
- `fingerprint.Template`: 025 (C-03 sends the model the masked template, never the raw error).
- The key `fp` on every `fail` line: 007 (a repeat needs the same `fp`), 018 (C-01 tells a known mistake from a
  new one), 019 (the `when.fingerprint` of a drafted lesson), 020 (R-04's trigger) and 023 (E-02's verdict).
- `sense.Outcome.FP`: 020 and 023 read it in the same process, not from the log.
- The verb `tentaqles lobe fingerprint`: every later tester brief that needs an expected `fp`.
- The prefix `fp1` and its mask order are frozen from here on. Changing S-08 steps 1 to 4 needs a new prefix
  and a plan for the lessons that list old values.

## How it works

1. **Where it runs.** `sense.Record` runs its steps in the order of S-01 step 7: S-05, S-08, S-06, S-07. This
   feature adds the second. It reads the event's `tool` and its `error` as S-05 left it: scrubbed, and cut to
   the 4000 characters of core page 5.4. A reader can therefore work the same `fp` out again from a stored
   line. Only an event of `kind` `fail` gets one. An `ok` line has no `fp`.
2. **Tool class** (S-08 step 1). `shell` for `Bash` and `PowerShell`; `edit` for `Edit`, `Write`, `MultiEdit`,
   `NotebookEdit` and `apply_patch`; `read` for `Read`, `Grep` and `Glob`; `web` for `WebFetch` and
   `WebSearch`; `mcp:<server>` for `mcp__<server>__<tool>`; else `other`. A name that starts `mcp__` and has
   no second `__` is `other`. The tool `unknown` (a payload with no `tool_name`, S-01) is `other`.
3. **Signature** (S-08 step 2): one line of the error text, color codes removed, picked by the four rules (a)
   to (d) in their order. Read them there; do not shorten them. Particular to this slice: for a shell tool the
   `Exit code N` line is already gone from `error` (S-01 step 5), so an error that was only that line arrives
   empty and its signature is `<empty>`. All shell failures with no text then share one fingerprint. That is
   accepted: the exit code is in `exit`, and such an event has nothing to teach by its text.
4. **Masks** (S-08 step 3): the seven passes in their order, then spaces collapsed, the ends trimmed and a cut
   at 200 characters. Mask (1) turns an S-05 marker into `<secret>`, which is why the step works on scrubbed
   text. Mask (3) looks at the character before a quote, which Go's `regexp` cannot do: write a scanner by
   hand. The order of the masks is part of `fp1`.
5. **Hash** (S-08 step 4). `fp` is `fp_` plus the first 16 hex digits of SHA-256 over `fp1`, a zero byte, the
   tool class, a zero byte and the UTF-8 template.
6. **What is written.** `fp` goes into the event line and into `Outcome.FP`. Nothing else: no
   `fingerprints.json`, no session file, no stats line. The template is not stored in the event.
7. **The count.** `tentaqles lobe status` reads the day files through `eventlog.Scan`, as it does for its
   event counts, and adds the number of distinct `fp` values: a text line, and the key `fingerprints` inside
   `events` in `--json`. It covers only what is still in the log, so it falls as day files expire.
8. **The verb.** `tentaqles lobe fingerprint --tool <name>` reads an error text on stdin and prints exactly two
   lines, the `fp` and the template, and exits 0. It passes the text through `scrub.Text` first, as the hook
   does, so the verb and the hook agree on one text and the printed template never holds a secret. It reads no
   store, resolves no client and needs no login folder, so it works in a neutral folder and inside an agent
   session. `--tool` is required; without it the verb prints one line on stderr and exits 1. Run by an agent
   through a shell tool it is one of the lobe's own tools and is not recorded (S-01 step 3).
9. **Cost.** `Of` is part of capture work (core page 5.2). It is one pass over at most 4000 characters with no
   disk and no clock. Every table it needs is built on first use: no `init` function and no package-level
   call (core page 5.1).

All rules of `fp1` are marked proposed in S-08 and stay proposed here.

## Where to read

| What | Where |
|---|---|
| Requirements | [S-08](../prd/lobe-sense.md), whole entry, without the fold of step 4 and without the Drain section; [S-01](../prd/lobe-sense.md) steps 5 and 7; [core page 5.4](../prd/vertical-lobe.md), "Fingerprint"; [core page 5.1](../prd/vertical-lobe.md), the import rules; [F-07](../prd/lobe-foundation.md) step 1, the row of the verb, and step 4 |
| Source to port | Trail-code (MIT): [`src/trace/failure.ts` L59](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/trace/failure.ts#L59): pick the first line that says it failed; a bare `Exit code N` line (L56) is only a fallback |
| Ideas, read on the web | Drain3 [drain.py](https://raw.githubusercontent.com/logpai/Drain3/master/drain3/drain.py): an ordered mask pass; Sentry [event grouping](https://docs.sentry.io/concepts/data-management/event-grouping/): type and value before the message, the order of step 2(a). Neither was machine-checked |
| Existing tentaqles code | `internal/lobe/sense/sense.go` (`Record`), `internal/lobe/scrub`, `internal/lobe/eventlog` (`Event`, `Scan`), `internal/lobe/cli` (the `status` verb), `internal/cmd/lobe.go`, all from 003 and 005; `test/integration/docs_test.go` L41 (`TestDocsReferenceEveryCommand`) |

## Build tasks

1. **Check this brief against the code as it stands** and list what no longer fits. Look at: the order of steps
   inside `sense.Record`; where the `Exit code N` line is removed; the exact marker text `scrub` writes;
   whether `Event` has `fp` and `Outcome` has `FP`; how `internal/lobe/cli` registers a verb; whether
   `TestLobeCommandTable` exists and how it lists commands. The owner approves changes to this file first.
2. **Class and signature.** Create `internal/lobe/fingerprint/class.go` and `signature.go`. The line pick of
   rule (c) is ported from Trail-code: the file starts with the header line F-07 asks of a ported file, and
   `NOTICE` lists it. Proof: the signature half of `TestLobeFingerprintVectors`. Can run beside task 3.
3. **Template.** Create `template.go` with the hand-written scanner for the seven masks. Proof:
   `TestLobeFingerprintStable`. Can run beside task 2.
4. **`Of`.** Create `fingerprint.go`: class, signature, template, hash. Proof: `TestLobeFingerprintVectors` and
   `TestLobeFingerprintDistinct`.
5. **Wire it into the hook.** Change `internal/lobe/sense/sense.go` (and the `Event` and `Outcome` types where
   needed) so a `fail` event gets `fp` between scrubbing and the append. Proof: `TestLobeSenseOutcome`, widened
   to check `FP`, and `TestLobeSenseFingerprintInLine`.
6. **The verb.** Create `internal/lobe/cli/fingerprint.go` and register it in `internal/cmd/lobe.go`. Proof:
   `TestLobeFingerprintVerb`. Can run beside task 5.
7. **The count.** Add the distinct-`fp` count to the `status` verb in `internal/lobe/cli`, text and `--json`.
   Proof: `TestLobeStatusCountsFingerprints`. After task 5.
8. **Docs.** A `## tentaqles lobe fingerprint` section in `docs/reference/commands.md`, and one sentence in
   the `tentaqles lobe status` section; one line in `CHANGELOG.md` under `[Unreleased]`; the verb's row in
   `TestLobeCommandTable` if that test lists commands. The plugin does not change. No "Must not trade off"
   line is added.
9. **Tester run.** A fresh agent runs the Evaluation below on the built binary.

## Tests that stay in the repository

- `TestLobeFingerprintVectors`: rows 1 to 3 of S-08's tester table, each as written and with its change, are 6
  texts that give 3 values; rows 4 and 5 give their own. 8 texts, 5 values.
- `TestLobeFingerprintStable`: 50 generated variants of each of rows 1 to 3 (other paths, numbers, ids and
  quoted values) give 1 fingerprint per row. 150 of 150.
- `TestLobeFingerprintDistinct`: 20 pairs with different causes, among them `HTTP 404` and `HTTP 429`,
  `SQLSTATE: 42601` and `SQLSTATE: 42P01`, `ENOENT` and `EACCES`, give 20 pairs of different fingerprints.
- `TestLobeSenseOutcome` (from 003, widened): after a failure, the outcome's `FP` equals the `fp` of the line.
- `TestLobeSenseFingerprintInLine` (new name, not in the requirements): six failures, one per tool class,
  write six lines that each hold the `fp` that `fingerprint.Of` gives for the line's own `tool` and `error`; a
  shell failure whose error was only `Exit code 3` holds `exit` 3, no `error`, and the `fp` of `<empty>`.
- `TestLobeFingerprintVerb` (new name): the five rows through the verb print two lines each and exit 0; a text
  with `password=hunter2` prints a template with `<secret>` and without `hunter2`; with `TQ_HOME` pointing at
  an empty folder and the process in a neutral folder the verb works and creates no file; without `--tool` it
  exits 1.
- `TestLobeStatusCountsFingerprints` (new name): a log with 5 failure lines of 2 fingerprints, 1 `ok` line and
  1 broken line gives `"fingerprints":2` in `tentaqles lobe status --json`; an empty log gives 0.
- House rules kept green: `TestLobeGuardNeverImportsLobe` (the new package has no `init` and no package-level
  call), `TestLobeImportsOnlyTheFour`, `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeSenseFailureSkips`, `TestLobeScrubParity`, `TestDocsReferenceEveryCommand`, and every test under
  "Must not trade off" in `CLAUDE.md`, unedited.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH`, bash (Git Bash on Windows) and an empty
working folder. It uses no network and never reads or writes the real `~/.tentaqles`. Setup:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
ACME="$W/clients/acme"; ID="$TQ_HOME/identities"
LOG="$ID/acme/lobe/events/events-$(date -u +%Y%m%d).jsonl"
fpof() { tentaqles lobe fingerprint --tool "$1"; echo "exit=$?"; }
send() { printf '%s' "${1//ACME/$ACME}" | tentaqles lobe-hook claude post-tool-use-failure; echo "exit=$?"; }
snap() { (cd "$TQ_HOME" && find . -type f -exec cksum {} + | sort -k3); }
```

Stop and report FAIL if a line starts with `SETUP FAIL`. Checks 1 to 6 run in `$W`, a neutral folder, with no
login variable. Each prints the `fp`, then the template, then `exit=0`, and nothing else.

1. `printf 'Exit code 127\n/usr/bin/bash: line 1: Get-ChildItem: command not found' | fpof Bash` prints
   `fp_b4f3e92d90787db6` and `<path>: line <n>: Get-ChildItem: command not found`. So does
   `printf '%s' '/bin/bash: line 12: Get-ChildItem: command not found' | fpof Bash`.
2. With `--tool mcp__warehouse__query`, each of these two texts prints `fp_f44eaf6b555b0cd2` and
   `[PARSE_SYNTAX_ERROR] Syntax error at or near <q>. SQLSTATE: 42601 (line <n>, pos <n>)`:

   ```
   [PARSE_SYNTAX_ERROR] Syntax error at or near '('. SQLSTATE: 42601 (line 3, pos 27)
   [PARSE_SYNTAX_ERROR] Syntax error at or near 'SELECT'. SQLSTATE: 42601 (line 1, pos 41)
   ```

3. With `--tool mcp__tracker__create_issue`, each of these two texts (each is one line) prints
   `fp_c9f4aa346d1ec77f` and
   `HTTP 429 Too Many Requests code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds.`:

   ```
   HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 4f2a. Retry after 17 seconds.","request_id":"req_8f3a9c21d0b4"}}
   HTTP 429 Too Many Requests: {"error":{"code":"rate_limited","message":"Rate limit exceeded for key ending 91cc. Retry after 3 seconds.","request_id":"req_02b7e6a955f1"}}
   ```

4. The first text of check 2 with `--tool Bash` prints `fp_366015c0d74849a2` and the template of check 2: the
   tool class is part of the hash.
5. The first text of check 3 with `429 Too Many Requests` changed to `404 Not Found`, same tool, prints
   `fp_8c90d679b4fe1c9f` and
   `HTTP 404 Not Found code=rate_limited Rate limit exceeded for key ending <id>. Retry after <n> seconds.`
6. `snap > s1; printf '%s' 'login failed: password=hunter2 rejected' | fpof Bash; snap > s2; cmp s1 s2`. The
   template line holds `<secret>` and holds neither `hunter2` nor `redacted`. `cmp` is silent: the verb wrote
   nothing under `$TQ_HOME`.

Then `export CLAUDE_CONFIG_DIR="$ID/acme/claude"; cd "$ACME"`. Every `send` below must print only `exit=0`.

7. Send this. Expect 1 line in `LOG` with `id` `evt_9bde2e09`, `kind` `fail` and `fp` `fp_48622a622f4928cc`.

   ```
   send '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"mcp__acme__ping","tool_use_id":"toolu_01","tool_input":{"n":4411},"error":"widget 4411 is not frobnicated"}'
   ```

8. The same with `toolu_02`, `{"n":97}` and the error `widget 97 is not frobnicated`. Expect 2 lines. The
   second has `id` `evt_8618533e`, its own `input` and `error`, and the same `fp` `fp_48622a622f4928cc`.
9. Another tool class. Expect 3 lines. The `fp` of the third differs from `fp_48622a622f4928cc` and equals the
   first line that `printf '%s' 'widget 4411 is not frobnicated' | fpof Edit` prints.

   ```
   send '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Edit","tool_use_id":"toolu_03","tool_input":{"file_path":"ACME/notes.txt","old_string":"a","new_string":"b"},"error":"widget 4411 is not frobnicated"}'
   ```

10. Two shell failures that differ in a path and a number. Expect 5 lines. Lines 4 and 5 both have `exit` 1,
    an `error` that does not start with `Exit code`, and one shared `fp`. That `fp` and the template
    `frobnicate: cannot open <path>: widget <n> missing` are what
    `printf '%s' 'frobnicate: cannot open /tmp/x/in.csv: widget 97 missing' | fpof Bash` prints.

    ```
    send '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_04","tool_input":{"command":"frobnicate /srv/data/run-17/in.csv"},"error":"Exit code 1\nfrobnicate: cannot open /srv/data/run-17/in.csv: widget 4411 missing"}'
    send '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_05","tool_input":{"command":"frobnicate /tmp/x/in.csv"},"error":"Exit code 1\nfrobnicate: cannot open /tmp/x/in.csv: widget 97 missing"}'
    ```

11. A secret in the error. Expect 6 lines. `LOG` does not hold `hunter2`. The sixth line's `fp` equals the
    `fp` that check 6 printed.

    ```
    send '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_06","tool_input":{"command":"frobnicate --login"},"error":"Exit code 1\nlogin failed: password=hunter2 rejected"}'
    ```

12. A line that is not a failure has no `fp`. Expect 7 lines. The seventh has `kind` `ok` and no `fp`.

    ```
    send '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"ACME","tool_name":"Bash","tool_use_id":"toolu_07","tool_input":{"command":"grep -n zzz notes.txt"},"error":"Exit code 1"}'
    ```

13. `tentaqles lobe status --json | grep -c '"fingerprints":4'` prints `1`: lines 1 and 2 share one, lines 4 and
    5 share one, lines 3 and 6 have their own, and line 7 has none.
14. `test -e "$ID/acme/lobe/fingerprints.json"; echo $?` prints `1` (the fold is not part of this feature), and
    `ls "$ID/globex/lobe/events" 2>/dev/null | wc -l` prints `0`.

There are no person checks. PASS needs 14 of 14. "Could not run" is a FAIL.

## Done when

- [ ] `TestLobeFingerprintVectors`, `TestLobeFingerprintStable`, `TestLobeFingerprintDistinct`,
  `TestLobeSenseFingerprintInLine`, `TestLobeFingerprintVerb`, `TestLobeStatusCountsFingerprints` and the
  widened `TestLobeSenseOutcome` are in the repository and green in CI (`go vet ./...`,
  `go test ./... -count=1`, `go build ./...`) on the three systems
- [ ] the tester brief passes, 14 of 14
- [ ] the house rules hold: `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`, `TestLobeScrubParity`, and every test under
  "Must not trade off" in `CLAUDE.md` unedited
- [ ] `docs/reference/commands.md` has the verb's section (`TestDocsReferenceEveryCommand` green) and
  `CHANGELOG.md` has its line
- [x] the owner has seen two differing errors print one fingerprint, and two log lines share an `fp`

## Risks and unknowns

- Every rule of `fp1` is proposed and none is measured: there is no corpus yet, the example texts were written
  by hand, and the error words of rule (c) are English only (S-08, Risks). A change to the rules after lessons
  exist needs a new prefix, so the vectors must be right before 019.
- The five vectors in the tester table were recomputed for this file from the templates S-08 prints (step 4's
  hash) and match. That proves the hash recipe, not the masks: only the built scanner proves those.
- The verb scrubs its input before it fingerprints it. S-08 says the signature is taken from scrubbed text
  and does not say the verb scrubs; this file reads it that way so the verb and the hook agree. If the owner
  reads it otherwise, step 7 and check 6 change.
- A shell failure with no text gets the fingerprint of `<empty>` (step 3). If later features need such
  failures apart, the fix belongs in a new mask list, not here.
- Masking quoted values merges different SQL syntax errors of one engine, and one engine error through two
  tools gives two fingerprints (S-08, Risks). Both are known limits; 022's benchmark is where the first shows.
- The fold is left out. Until 018 builds it there is no `fingerprints.json`, so nothing counts how often a
  pattern was seen or in how many sessions. The count in `tentaqles lobe status` is a plainer thing: distinct
  values still in the log. F-07 does not say which of the two its "counts of fingerprints" means.
- The core page's fingerprint shape lists `sessions` without a type, and S-08 does not say how a fold
  remembers which sessions it has counted between runs. That is for 018 to settle with the owner.

## Result of the build, the review and the tester run

Built on 2026-11-10 by four Haiku builders (class and signature, the mask scanner, `Of` and its vectors, the
verb with the status count and docs), reviewed as one change by a Sonnet reviewer; the main session made the
scrubber exemption, the wiring into `sense.Record` and the review's two fixes.

- **Brief check (task 1).** The code fit. `Outcome.FP` and the event's `fp` key existed already, empty. Feature
  063 (added after this brief) records through `sense.Record` too, so a rejected call gets its `fp` as well.
- **The scrubber refused every line with an `fp`.** `fp_b4f3e92d90787db6` has the shape of a vendor key, so
  `scrub.Line` returned `ErrUnsafe` and the line was dropped. The builder stopped instead of editing the
  scrubber. The exemption admits only the fingerprint's own shape, `fp_` and exactly 16 lowercase hex digits;
  any other `fp_` value of a key's shape is still redacted (`TestLobeScrubOwnFingerprint`). A real key of
  exactly that shape would pass; none is known.
- **The review found** a mask 4 bug: the keyword's token class took a capital word (`Error: HTTP 404`), was
  rejected, and the number after it was lost, so `HTTP 404` and `HTTP 429` merged in that form. The token is now
  exact (`TestLobeFingerprintKeywordBeforeCapitalWord`). It also found rule (a) reading JSON from a `{` inside a
  traceback frame. S-08 says rule (a) applies when "the text from its first `{` parses as a JSON object"; the
  code now requires the whole rest to be that one object, which also lets rule (b) find the traceback's last
  line (`TestLobeFingerprintJSONMustBeTheWholeRest`). The five vectors of S-08 did not change.
- **Known narrow case, left:** `echo x | tentaqles lobe fingerprint --tool Bash` run by an agent is not the
  lobe's own tool (every statement must be), as for every lobe verb (004).
- Tests: `go vet`, `go build` and `go test ./... -count=1` green on Windows; the plugin suite 34 passed.
- Tester brief: 14 of 14 on Windows 11 in Git Bash against a program built from the working tree.

The owner's check, 2026-11-10, in the `tqx` playground: the two `Get-ChildItem` errors printed one
fingerprint, `fp_b4f3e92d90787db6`, and one template; two failed calls of `mcp__acme__ping` with
`widget 4411` and `widget 97` left two lines with `"fp":"fp_48622a622f4928cc"`, and `tentaqles lobe status` showed
2 events and 1 fingerprint.

Left open: CI on Linux and macOS.
