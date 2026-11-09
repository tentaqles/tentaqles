# 027: A correction from the person becomes a lesson

**Status** not started · **Stage** Lessons · **Entries** C-02 (steps 1 to 4, 7 and 8, the `corrected` sign),
C-03 (the parts that word a correction), E-01 (enter rule for `correction`); D-9 · **Needs** 025 ·
**Feeds** 028, 035, 036, 050

## Result

The person types "no, use pnpm instead of npm" to the agent, and the agent switches. After the turn the lobe has
filed a lesson with `source: correction`. It speaks at once: the next time `npm` is about to run in this client,
the agent reads the note first. A prompt that only sounds like a correction ("no problem, add a test") leaves
nothing behind.

```sh
tentaqles lobe consolidate --session s1 --only signs --json     # one line with "sign":"corrected"
tentaqles lobe consolidate --session s1 --draft template --json # one line with "action":"file"
tentaqles lobe lessons                                          # the new lesson, at advice
```

## Scope

**In**

- The lobe's `UserPromptSubmit` entry in `plugin/hooks/hooks.json` and the `user-prompt-submit` handler of
  `tentaqles lobe-hook claude`, which in this feature only notes a possible correction and prints nothing.
- The wording test (C-02 step 1) with one embedded word file per language, English and Brazilian Portuguese,
  picked by the client's manifest `language` (step 2).
- The flag line in `candidates.jsonl` and the `corrections` key of the session file (step 3).
- In the detached pass: a flag becomes a `corrected` sign on evidence (a) or (d), or is dropped (step 4); the
  cap of 3 signs per session (step 7); `--only signs` and the `sign` action line of `--json`.
- Wording a correction: the two templates of C-03 step 7, the sign input for the model (C-03 step 2) and the
  check that a lesson never quotes the prompt (C-03 step 5).
- The lesson enters at `advice` (D-9) and is shown by what 017 and 026 already built.

**Out**

- The `undone` and `fixed` signs, and evidence (b), an undo after the prompt (feature 028).
- Evidence (c), a call the person declined or interrupted. It is not in the first build: S-01 records nothing
  for such a call (C-02 Risks). No feature takes it yet.
- Code pointers and lessons printed with a prompt, R-02, which shares this hook process (feature 050).
- Corrections found in past sessions (feature 035). Corrections in a Codex session (feature 036).
- Wording lists for other languages (core page section 11).

## What it builds on

- 003, 005: the hook engine and its client rule, the store, `scrub.Clean` (S-05), `lobe_hook.sh`, the settings
  `capture` and `lessons`.
- 008, 009: `ok` events with a short projection of the input, and the full input of a success that follows a
  failure.
- 011: the sweep that expires event files and, with them, lines of `candidates.jsonl`.
- 016: the gate, `lesson` parse and write, the `origin` block.
- 017: the note before a matching call, and `ladder.Enter`.
- 018: `internal/lobe/consolidate` (`run.go`, `pair.go`, and `candidates.go` with `Append`, `Pending`,
  `Close(id, state)` and `Expire`), the pass and its ordered steps, `tentaqles lobe consolidate` with
  `--session`, `--only pair`, `--json` and `--now`, and the rule that a session whose file holds
  `"tampered":true` teaches nothing.
- 019: `Template(pair)` in `template.go`, `consolidate.File(candidate, lesson, evidence)` (quarantine, gate,
  filing, `ladder.Enter`, the closing state), `--draft template`.
- 025: `Ask(candidate)` and `Check(answer, evidence)` in `draft.go`, `child.go`, the closing state `skipped`,
  `--draft model` and `--draft file:<path>`. It left the sign input and the prompt-quote check to this feature.
- 026: the brief, which shows a correction lesson that has no `when` block.

## What it leaves for later features

- `internal/lobe/consolidate/signs.go` with `Sign{Kind, Session, At, Text, Check string; Named, Events, Files
  []string; FP string}` and `wording.go` with `wording/en.txt` and `wording/pt-BR.txt`. 028 adds two kinds of
  sign and evidence (b); 035 runs the same code over past sessions; 036 feeds it from Codex.
- The `user-prompt-submit` row of `hook/steps.go`, which 050 widens with R-02's print.
- The candidate kinds `flag` and `corrected`, and the session key `corrections`:
  `{"prompts": 2, "flags": [{"at": "<time>", "candidate": "c_…"}]}`.
- `--only signs`, and the `sign` action line: `sign`, `session`, and `named` where it applies.

## How it works

1. **Wiring.** `plugin/hooks/hooks.json` gains a `UserPromptSubmit` group: `bash
   "${CLAUDE_PLUGIN_ROOT}/scripts/lobe_hook.sh" user-prompt-submit`, `"timeout": 5`, no matcher. The guard has
   no entry for this event. The handler is in the inject class of core page 5.2, although it prints nothing
   here. The wrapper turns every status into exit 0, which matters on this event: an exit 2 would block the
   person's prompt.
2. **When the hook writes nothing.** The client rule fails; `capture` or `lessons` is `off`; the prompt starts
   with `/` or `<`; the store lock is busy for 50 ms.
3. **The wording test** (C-02 step 1). Lower-case the prompt and make curly apostrophes straight. A flag is
   raised when the prompt starts with a correcting word, as a whole word, or holds a rule phrase, and holds no
   clarification. A session's first prompt is never a correction: the count of prompts in the session file
   decides. A skipped prompt (step 2) is not counted (this file's choice).
4. **The lists** (step 2). `wording/en.txt`: correcting words `no`, `nope`, `wrong`, `actually`, `don't`,
   `never`, `stop`; rule phrases `instead of`, `use X instead`, `we always use`, `you forgot`; clarifications
   `i mean`, `just asking`. `wording/pt-BR.txt` (proposed): correcting words `não`, `errado`, `na verdade`,
   `pare`, `nunca`; rule phrases `em vez de`, `ao invés de`, `sempre usamos`, `você esqueceu`; clarifications
   `quer dizer`, `quero dizer`, `só perguntando`. Accents are folded before the test, so `nao` and `voce` match.
   A client whose `language` is `pt-BR` is tested with the Portuguese list alone, because `no` opens many
   Portuguese sentences. Any other language uses the English list. Each file also carries the stop words that
   evidence (d) needs: Trail-code's list for English, and a Portuguese list of our own (proposed).
5. **On a flag** (step 3). Under the store lock the hook appends one line to `candidates.jsonl`:
   `{"v":1,"id":"c_…","kind":"flag","state":"open","session":"…","at":"<time>","text":"<scrubbed, at most 500
   characters>"}`, and updates `corrections` in the session file with the prompt count and the candidate's id.
   The prompt passes `scrub.Clean` before the cut. That one line is the only place the prompt text is kept.
   Without a flag the hook writes the prompt count only. The two patterns are compiled inside the call, never
   at package level. Flagging a 10,000-character prompt takes under 5 ms at the 95th percentile (proposed).
6. **In the pass** (step 4). The signs step runs after pairing and before drafting. A session whose file
   holds `"tampered":true` is skipped, as in pairing; nothing writes that mark before features 033 and 034.
   For each `open` flag of any other session, the pass looks for a change of course:
   - (a) a repair pair (C-01) whose failure is before the prompt and whose fix is after it;
   - (d) a word the prompt names, of 3 or more characters and not a stop word, that occurs as a whole word in a
     call's input after the prompt and in no call's input before it.
   With either, the flag's line becomes a `corrected` sign: `kind` `corrected`, `state` `pending`, with `named`
   and the ids of the events. With neither, the line is closed as `dropped` and loses its `text`. `--only
   signs` still computes pairs in memory, for (a).
7. **One learning, one candidate** (this file's choice). A pair that serves as evidence (a) is worded once, as
   the correction. Its own `repair` candidate, if one is pending, is closed as `skipped`.
8. **Cap.** At most 3 signs per session become candidates, oldest first (step 7). The agent is asked nothing and
   the Stop is never blocked.
9. **Wording** (C-03). A correction is templated from its events, never from its prompt. With evidence (a) it
   gets the template of its pair. With evidence (d), when the named word is the program of a call after the
   prompt and the last call of that pairing class before the prompt ran another program: title `Use <new
   program> here, not <old program>`, Do ``Run `<new key>`.``, Avoid ``Running `<old key>` for this.``, Why `The
   person corrected the agent here because the first program was not wanted.`, and `when.command` set to the
   old program. A correction that fits neither template waits for the model. The model's input for a sign is
   its kind, the scrubbed prompt read from the candidate line, and the inputs of the events it names (C-03
   step 2). A model answer whose title or sections repeat 6 or more words in a row of the prompt (proposed) is
   not filed: the candidate goes to the template, with no retry.
10. **Filing.** The lobe sets `source: correction`, `origin.drafted` (`template` or `model`), `when.tool`
    beside `when.command`, and `strength: 0.7` (C-05 step 1, proposed). The draft passes the gate like any
    other (C-04); a correction lesson may have no `when` block and no `touches` entry. When the candidate
    closes, the pass removes `text` from its line. The line expires with `retention_days`.
11. **It speaks at once** (step 8, D-9). `ladder.Enter` puts a `correction` lesson at `advice`. With a `when`
    block it is shown before the matching call (017); without one it is a standing lesson in the brief (026).
    It does not wait for the person. Most correction lessons have no `avoid` block and no fingerprint, so no
    hook ever demotes one; a person mutes it with `tentaqles lobe rules demote <id>` or `tentaqles lobe rules
    retire <id>`.

## Where to read

| What | Where |
|---|---|
| Requirements | [C-02](../prd/lobe-consolidate.md), steps 1 to 4, 7 and 8, "How to build it" and Risks; [C-03](../prd/lobe-consolidate.md), steps 2, 5 and 7; [C-04](../prd/lobe-consolidate.md), the `form` and `grounded` rows; [E-01](../prd/lobe-enforce.md), the enter rule; [F-03](../prd/lobe-foundation.md), steps 2 and 3; [F-02](../prd/lobe-foundation.md), step 2 (the `user-prompt-submit` row); [core page 5.5](../prd/vertical-lobe.md), decision D-9 |
| Source to port | [`src/claude/note-signals.ts` L108](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L108): correcting words at the start of a prompt; [L110](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L110): rule phrases; [L112](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L112): clarifications; [L42](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L42): 3 asks per session. The stop words are in `src/notes/note-gate.ts` L60 to L62 at the same commit |
| Source, weakness we avoid | [`src/claude/note-signals.ts` L248](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/note-signals.ts#L248): any later command counts as proof; [`src/claude/stop.ts` L151](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/claude/stop.ts#L151): the Stop is blocked so the agent writes the learning |
| BASE | Idea only, nothing ported: a matching phrase raises a question and settles nothing |
| Existing tentaqles code | `plugin/hooks/hooks.json`; `plugin/tests/test_tq_hook.py` (`test_the_plugin_holds_only_the_guard_hook`); `internal/manifest/manifest.go` L73 (`Language`) and `internal/manifest/language.go` (`pt-BR`, `DefaultLanguage`); `internal/lobe/consolidate` as 018, 019 and 025 left it |

## Build tasks

1. **Check this brief against the code.** Read `internal/lobe/consolidate`, `hook/steps.go`, the wrapper and
   the candidate states as they stand. List what no longer fits, above all: whether 025 already built the sign
   input and the word-run check, and how the order of a prompt and an event is known when both fall in one
   second (see Risks). The owner approves changes before code is written.
2. **Word files and the wording test.** `wording.go`, `wording/en.txt`, `wording/pt-BR.txt`. Proof:
   `TestLobeSignWordingByLanguage` and a table test of the lists.
3. **The hook step and the wiring** (side by side with 2). The `user-prompt-submit` row in `hook/steps.go`; the
   flag line; the `corrections` key; the `UserPromptSubmit` group in `hooks.json`; the wrapper's event list;
   the pinned plugin test widened by one lobe event. Proof: `TestLobeSignPromptKeptOnce`,
   `TestLobeSignPromptScrubbed`, `TestLobeSignCapAndCost` (the cost part), the plugin suite.
4. **Signs in the pass.** `signs.go`: evidence (a) and (d), the drop, the cap, `--only signs`, the `sign`
   action line, step 7 above. Proof: `TestLobeSignCorrectedNeedsEvidence`, `TestLobeSignCapAndCost`.
5. **Wording a correction.** The two templates in `template.go`; the sign input in `Ask` and the word-run
   check in `Check` (`draft.go`). Proof: `TestLobeDraftNeverQuotesPrompt`, `TestLobeDraftSourceAndOrigin`
   widened to `corrected`.
6. **Entering and speaking.** No new code is expected. Proof: `TestLobeCorrectionSpeaksAtOnce`.
7. **Docs.** `docs/reference/commands.md`: `user-prompt-submit` under `tentaqles lobe-hook`, `--only signs`
   under `tentaqles lobe consolidate`. `CHANGELOG.md`, `plugin/CHANGELOG.md`, `plugin/CLAUDE.md`.
8. **Tester run**, then the person checks.

## Tests that stay in the repository

- `TestLobeSignCorrectedNeedsEvidence`: "no problem, add a test" then `ls` gives 0 signs; `npm install`, "no,
  use pnpm instead of npm", `pnpm install` gives 1 sign with `named` equal to `["pnpm"]`.
- `TestLobeSignWordingByLanguage`: a `pt-BR` client flags "não, use pnpm em vez de npm" and "nao, use pnpm", and
  does not flag "no arquivo a.go, troque x por y"; a `de-DE` client uses the English list: 4 of 4.
- `TestLobeSignCapAndCost`: 5 `corrected` signs in one session give 3 candidates; flagging a 10,000-character
  prompt takes under 5 ms at the 95th percentile (proposed).
- `TestLobeSignPromptKeptOnce`: after a flag, the prompt text is in 1 line of `candidates.jsonl`, at most 500
  characters, and in 0 other files of the store; after the candidate is closed it is in 0 files.
- `TestLobeSignPromptScrubbed` (new): a prompt that holds a token of S-05's test set leaves 0 copies of it in
  the store.
- `TestLobeDraftNeverQuotesPrompt`: a correction's template lesson holds 0 words of its prompt that are not in
  a call's input; a model answer that repeats 6 words in a row of the prompt is templated, not filed.
- `TestLobeDraftSourceAndOrigin`: a `corrected` candidate files with `source: correction`, never `model`, and
  `origin.drafted` is `model` or `template` as the code path says.
- `TestLobeCorrectionSpeaksAtOnce` (new): a filed correction lesson is at `advice` with no entry written by
  hand, prints before a matching call in the next session, and one with no `when` block prints in the brief.
- House rules. Never stalls: `TestLobeHookAlwaysExitsZero`, `bench stall` and `test_lobe_hook_always_exits_zero`
  cover `user-prompt-submit`. Wrong client gets nothing: `TestLobeHookServesOnlyItsLogin` covers the event. No
  secret on disk: `TestLobeSignPromptScrubbed`. No model in a hook: the handler imports no drafting code, and
  `TestLobeNoHooksSwitch` covers the event. The guard is untouched: `TestLobeGuardNeverImportsLobe` and the
  guard's two `hooks.json` entries byte-identical.

## Evaluation

The tester gets the built `tentaqles` first on `PATH`, an empty working folder, and the repository's `plugin`
folder to read (`PLUGIN`). Use bash (Git Bash on Windows). No network; the sandbox switches `model` off. Run:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
lobeset() { m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"; }
I="$TQ_HOME/identities"; N=acme; A="$W/clients/$N"; cd "$A"
export CLAUDE_CONFIG_DIR="$I/acme/claude"
PROMPT() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"UserPromptSubmit","prompt":"%s"}' \
  "$1" "$A" "$2" | tentaqles lobe-hook claude user-prompt-submit; echo " exit=$?"; }
OK() { printf '{"session_id":"%s","cwd":"%s","hook_event_name":"PostToolUse","tool_name":"Bash",' "$1" "$A"
  printf '"tool_input":{"command":"%s"},"tool_response":{"stdout":"ok","stderr":""}}' "$2"; }
ok() { OK "$1" "$2" | tentaqles lobe-hook claude post-tool-use; echo " exit=$?"; }
SIGNS() { tentaqles lobe consolidate --session "$1" --only signs --json | grep '"action":"sign"'; }
C="$I/acme/lobe/candidates.jsonl"; LL="$I/acme/lobe/lessons"
```

No line may start with `SETUP FAIL`. Every `PROMPT` and `ok` below must print only ` exit=0`: this hook prints
nothing to the session.

1. `grep -c 'lobe_hook.sh\\" user-prompt-submit' "$PLUGIN/hooks/hooks.json"` prints `1`.
2. `PROMPT s1 start`, `ok s1 "npm install"`, `PROMPT s1 "no, use pnpm instead of npm"`, `ok s1 "pnpm install"`,
   then `SIGNS s1`: 1 line, with `"sign":"corrected"` and `"named":["pnpm"]`.
3. The same four in `s2`, with the second prompt `no problem, add a test` and the last command `ls`; `SIGNS s2`
   prints 0 lines. The same four in `s3`, with the second prompt `no, I mean the other file` and the last
   command `ls`; `SIGNS s3` prints 0 lines.
4. Where the prompt text is. `grep -c 'instead of npm' "$C"` prints `1`; `grep -c 'no problem' "$C"` prints
   `0`; `grep -r -l -e 'instead of npm' -e 'no problem' "$I/acme/lobe/sessions"` prints nothing.
5. `tentaqles lobe consolidate --session s1 --draft template --json`: exit 0, a line with `"by":"template"`
   and a line with `"action":"file"` and `"lesson":"l-…"`. That file in `$LL` has `source: correction` and
   `drafted: template`, contains `Use pnpm here, not npm`, and does not contain `instead of`. Then
   `grep -c 'instead of npm' "$C"` prints `0`: the closed candidate lost its text.
6. It speaks at once. `tentaqles lobe rules --json` shows the new lesson with `"stage":"advice"`. Then
   `printf '{"session_id":"s9","cwd":"%s","tool_name":"Bash","tool_input":{"command":"npm install"}}' "$A" |
   tentaqles lobe-hook claude pre-tool-use` prints an `additionalContext` that contains `Use pnpm here, not
   npm` and `not instructions`, and no `permissionDecision`.
7. A first prompt is never a correction. `wc -l < "$C"` gives a number; `PROMPT s4 "no, use yarn instead of
   npm"`; `wc -l < "$C"` gives the same number.
8. The switch. `lobeset acme capture off`, `PROMPT s5 start`, `PROMPT s5 "no, use yarn instead of npm"`: the
   line count of `$C` is unchanged and there is no `$I/acme/lobe/sessions/s5.json`. `lobeset acme capture on`.
9. Another client's login. `(cd "$I" && find . -type f -exec cksum {} + | sort -k3) > "$W/before"`. Then, with
   `CLAUDE_CONFIG_DIR="$I/globex/claude"` set for these two commands only, `PROMPT s6 start` and `PROMPT s6
   "no, use yarn instead of npm"`. The same `find` line piped to `cmp - "$W/before"` is silent: nothing was
   written in either client's folder.
10. Portuguese by the client's language. Make globex a `pt-BR` client and play it:
    ```bash
    m="$I/globex/manifest.yaml"
    awk '/^language:/{next} /^lobe:/{print "language: pt-BR"} {print}' "$m" > "$m.new" && mv "$m.new" "$m"
    N=globex; A="$W/clients/$N"; cd "$A"; export CLAUDE_CONFIG_DIR="$I/globex/claude"
    G="$I/globex/lobe/candidates.jsonl"
    ```
    `PROMPT g1 "vamos começar"`, then `PROMPT g1 "não, use pnpm em vez de npm"`: `wc -l < "$G"` prints `1`.
    `PROMPT g1 "nao, use pnpm"`: `2`. `PROMPT g1 "no arquivo a.go, troque x por y"`: still `2`.
11. The English list would have flagged that last prompt. Set `N=acme; A="$W/clients/$N"; cd "$A"` and export
    acme's login again. Note `wc -l < "$C"`, run `PROMPT s7 start` and `PROMPT s7 "no arquivo a.go, troque x
    por y"`: the count is one higher.

**Person checks** (for the owner)

1. Read `internal/lobe/consolidate/wording/pt-BR.txt` as a native speaker. The correcting words, rule
   phrases, clarifications and stop words are the author's guesses. Strike what would flag ordinary prompts,
   add what you really type when you correct an agent, and approve the file before it ships.
2. In a real Claude Code session of one client, let the agent run a command, correct it in your own words
   ("no, use X instead of Y"), and end the turn. `tentaqles lobe lessons` lists a new lesson with `source:
   correction`; in a new session the note appears before the old command. `grep` your own words in
   `$TQ_HOME/identities/<client>/lobe`: after the pass they are in no file.

PASS needs 11 of 11. "Could not run" is a FAIL. The person checks are reported apart.

## Done when

- [ ] Every test above is in the repository and green in CI: `go vet ./...`, `go test ./... -count=1`,
  `go build ./...`, and the plugin suite.
- [ ] The tester brief passes 11 of 11.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeNoHooksSwitch`, `TestLobeSignPromptScrubbed`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, and every test under "Must not trade off" in `CLAUDE.md`, unedited.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md`, `plugin/CHANGELOG.md` and `plugin/CLAUDE.md` are updated.
- [ ] The owner approved `wording/pt-BR.txt` and saw a correction of their own become a note.

## Risks and unknowns

- **Order inside one second.** An event's `at` has whole seconds (core page 5.4), and the checks above send a
  prompt and a call back to back. The pass must still know which came first. The requirements do not say how.
  Task 1 proposes one way (for example the flag records the id of the session's newest event) and the owner
  approves it.
- **A pass that runs too early.** One pass serves every session of the client. A pass started by another
  session's Stop can read a flag before its own turn has produced evidence, and step 6 then drops it. The
  requirements do not cover this. If the owner's use shows lost corrections, a flag must stay open until its
  session's own next Stop or prompt.
- The Portuguese words are our choice and unmeasured (proposed). Person check 1 is the gate for them.
- Evidence (d) can be met by chance. The cap and the gate limit the cost; the lesson still speaks at once, and
  with no `avoid` block only the person mutes it.
- The scrubbed prompt of an open flag sits in `candidates.jsonl` for up to `retention_days`. The scrubber
  removes secrets, not names.
- The word-run check stops a quoted prompt, not a paraphrased one. A name the person typed can still reach a
  lesson through the model's wording.
- A client in any language but `pt-BR` is tested with the English list, so a correction typed in that language
  is missed.
- F-02's table runs C-02's step before R-02's print, and C-02 says after it. Only one step exists until
  feature 050, which must settle the order.
- What forces a change to this file: a host that does not send the prompt text on `UserPromptSubmit` (check
  the phase 0 capture of feature 003); a pinned plugin test that cannot be widened by one event.
