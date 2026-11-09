# 013: Isolation leak suite

**Status** not started · **Stage** Sense · **Entries** M-03 · **Needs** 011 (and, by number order, 001 for
attack L-09 and 012 for the `bench` verb) · **Feeds** 021, every phase gate, and every later feature that
completes an attack

## Result

One command runs deliberate attacks that try to make one client's data show up in another client's session,
and reports how many leaked. The pass mark is zero. At this point only capture exists, so the suite runs the
attacks that capture can be attacked with and lists every other attack as not run, with what it waits for.

```bash
tentaqles lobe bench leak --out "$W/leak"; echo "exit=$?"         # exit=0
cat "$W/leak/summary.json"   # {"leaks":0,"controls_failed":0,"ran":[...],"not_run":[{"id":"L-01","needs":"R-04"},...]}
tentaqles lobe bench leak --plant events --out "$W/leak2"; echo "exit=$?"   # exit=1, "leaks" at least 1
```

## Scope

**In**

- The action `tentaqles lobe bench leak [--plant <what>] --out <dir>` and its package
  `internal/lobe/benchcheck/leak` (`attacks.go`, `canary.go`).
- The canaries, planted where a feature exists to hold them: in an event and in the login file of a fake
  agent CLI.
- The control, the swap of the two clients, and the scan.
- The attacks that run in full now: L-04, L-05, L-06, L-08, L-09, L-12, L-13 and L-15.
- The early parts of L-01, L-02 and L-03 that capture alone can run (step 6 says how they are counted).
- L-14's steps, which only a person's run executes.
- `--plant events`, which proves the suite can turn red.
- `test/integration/lobeleak_test.go`, which runs the suite against the built program in CI.
- A new line under "Must not trade off" in `CLAUDE.md` with the test names (M-03, "How to build it").

**Out**

Each waiting attack names the requirements entry it waits for (the `needs` text in `summary.json`) and the
feature that brings it, read from the index.

| Attack | Waits for (`needs`) | Feature | What that feature adds to the suite |
|---|---|---|---|
| L-01 | `R-04` | 020 | Acme has a lesson that lists the shared fingerprint; globex fails the same way and its hook must print nothing of acme's |
| L-02 | `R-04, G-02` | 017, then 043 | A private lesson with `applies: repo` (017), then a graph symbol that exists only in acme's working tree (043). Complete at 043 |
| L-03 | `C-08, R-04` | 014, then 017 | Two `sessions/` files for one session id; shown-once state (014 if a starter rule is the first to write the session file) and recall (017) do not cross. Complete at 017 |
| L-07 | `C-04, F-04` | 016, 017, then 034 | A linked `lobe/lessons` folder: the first read verb (016) and the hook (017). A linked `.tentaqles/lessons` in the repository (034). Complete at 034 |
| L-10 | `F-02 step 8, C-03` | 018, then 025 | The `stop` hook and its detached pass (018), then the fake agent CLI that records the child's environment (025). Complete at 025 |
| L-11 | `C-07` | 035 | Backfill reads only acme's own login folder |
| L-14 | `person` | none | The owner's person check runs it; a tester's run always lists it as not run |

Also out:

- `--plant lesson` (016) and `--plant graph` (043). Until then each prints one line and writes no summary.
- The planting of a canary in a private lesson (016) and in a symbol name (043).
- New hook events and new commands. L-04, L-05, L-13 and L-15 pick them up from the hook's event table and
  the command table with no new attack (step 11).
- The resolver's fix. It is feature [001](001-resolver-second-folder.md); this suite only attacks it.
- F-02's in-package tests of the client rule (`TestLobeHookServesOnlyItsLogin` and the two beside it), which
  feature 003 owns. This suite attacks the same rule from outside, through the built program.

## What it builds on

Task 1 confirms each line against the code.

- 001: the resolver refuses a second folder with a client's name. Without it L-09 reports a leak.
- 002: `sandboxBuild`, handed over as a function value; `TestLobeCommandTable`, the list of lobe commands.
- 003: `tentaqles lobe-hook`, the client rule (F-02 step 4), the store and its refusal of linked folders.
- 004 to 009: capture on `post-tool-use-failure` and `post-tool-use`, fingerprints, the duplicate filter.
- 005: `tentaqles lobe status`, `events` and `settings`; the scope rule and the login rule for commands
  (F-07 step 3); `cli.Deps` with the "inside an agent session" function.
- 010: Codex's post-tool event. 011: the `session-end` sweep.
- 012: the verb `tentaqles lobe bench`, the helper that builds a home under `--out`, and
  `TestLobeBenchShipsOnlyTheFourChecks`.
- Existing tentaqles code: `internal/registry` (`Load`, `AddBase`, `Save`) to register a second base for
  L-09; `manifest.MarkerBytes`; the pattern of `TestResolve_MarkerOrManifestOfAnotherClient`.

## What it leaves for later features

| Name | What it is | Used by |
|---|---|---|
| `tentaqles lobe bench leak` | The third bench action | Every phase gate (core page section 8); 021 |
| The attack table in `attacks.go` | One row per attack: id, the entries it needs, setup, attack, scan | 014, 016, 017, 018, 020, 025, 034, 035 and 043 each fill in the part named in the table under Scope |
| `canary.go` | The two canary words, the planting places and the scanner | The same features add a planting place or a read verb to scan |
| `summary.json` | `leaks`, `controls_failed`, `ran`, `not_run` | The claim for promise 1 (M-08, feature 021) |
| The `CLAUDE.md` line | The suite's tests under "Must not trade off" | Every later feature keeps them green |

## How it works

1. **Where it lives.** The package ships in the binary (M-02) and keeps the import rules of core page 5.1. It
   reaches the product only by starting the program's own file as a child: `lobe-hook <host> <event>` with a
   JSON on stdin, or a `lobe` command. Each attack gets a fresh home under `<out>/<attack id>/`, built with
   feature 012's helper and the sandbox builder: clients `acme` and `globex`, each with an empty repository
   `app`. `TMP`, `TEMP` and `TMPDIR` of every child point inside that home.
2. **Canaries.** `acmecanaryword` and `globexcanaryword`: plain letters, so scrubbing (S-05) and masking
   (S-08) leave them alone. Now each is planted in two places: an event, written by the client's own hook
   under its own login, with the word in the command and in the error; and a file in the client's login
   folder that stands for the login file of a fake agent CLI. The lesson and the symbol name join later.
3. **Control first.** Before an attack counts, the suite proves the canary can be recalled inside its own
   client: `tentaqles lobe events`, run in acme's folder, prints `acmecanaryword`, and acme's event file holds
   it. A failed control fails the attack and is counted in `controls_failed`.
4. **Both directions.** The attack runs as globex against acme, then with the clients swapped.
5. **The scan.** After the attack: the output of every command the attack ran; every file under
   `identities/<other>/`; the other client's folder; a neutral folder; the temp folder. The scan reads bytes
   and also calls every read verb that exists (`status`, `events`, `settings`), because a cache may compress
   text. One hit is a leak, printed as one line with the attack, the direction and the place.
6. **Run, or not run.** An attack counts as run only when every part of it ran. L-01, L-02 and L-03 have a
   part that capture alone can run; that part runs now and its scan can add to `leaks`, but the attack stays
   under `not_run` with the entries it waits for until its last part is built. Nothing is listed as passed
   that did not run in full (M-03 step 4).
7. **The attacks that run in full now.**
   - **L-04.** The hook JSON's `cwd` is acme's `app` while the login variable names globex's identity folder;
     then the same with the login variable unset. For Claude Code the variable is `CLAUDE_CONFIG_DIR`, for
     Codex `CODEX_HOME`. Expected: exit 0, 0 bytes of output, and the file trees of both stores unchanged.
   - **L-05.** The reverse through inherited variables: `cwd` is globex's while `TQ_WS`, `TQ_WS_ROOT` and the
     login variable are acme's. Expected: the same. L-04 and L-05 together are three forms, each for both
     hosts and for every lobe hook event the host has: 6 runs per event.
   - **L-06.** As globex, `session_id` and `tool_name` hold `../../acme/lobe/lessons/x`, and the same with
     backslashes. Expected: no file is created outside `identities/globex/lobe/`.
   - **L-08.** Acme's marker is copied into a neutral folder and into a subfolder of globex's `app`. A hook
     with `cwd` in the neutral folder, under acme's login, prints nothing and writes nothing. A hook with
     `cwd` in the subfolder is still globex: under globex's login its event lands in globex's store, and
     under acme's login nothing is written.
   - **L-09.** A second base is added to the registry of the attack's home, with a folder `acme` in it that
     holds a copy of acme's marker. The hook JSON's `cwd` is that folder and the login is acme's. Expected:
     no output, acme's store unchanged, nothing new in that folder. A write or any output is a leak counted
     in `leaks` like any other (M-03 step 6).
   - **L-12.** 200 hook calls per client, interleaved from 8 workers (proposed), failures and successes, each
     with its client's canary. Expected: no canary in the wrong store, and every line of both logs parses.
   - **L-13.** Inside an agent session (`CLAUDECODE=1`) in globex's folder, every `tentaqles lobe` command in
     the command table that takes `--client` is run with `--client acme`; a command that takes a path is
     also run with a `..` path into acme. Expected: refused, as F-07 step 3 states, and no acme data printed.
   - **L-15.** Inside an agent session in acme's folder, with the login variable naming globex's identity
     folder, every `tentaqles lobe` command that reads or writes a store is run. Expected: exit 1, nothing of
     acme's store printed, nothing written in either store. With the variable unset the same commands exit 0
     and work in acme's store only. Run for each host's variable.
8. **The early parts.** L-01: the same failing command and error in both clients, each under its own login;
   both event lines must carry the same `fp` (the precondition of the attack), then the scan. L-02: both
   `app` repositories have the same repository key, so both event lines carry the same `repo`; then the scan.
   L-03: one `session_id` used in both clients; each store holds one full line for it and no `dup` line that
   points at the other's.
9. **L-14.** `tentaqles drop acme`, then a new client named `acme`: no acme canary may remain under
   `$TQ_HOME` or the temp folder, and the new store is empty. `drop` answers only to a person. When the
   action runs inside an agent session it lists L-14 under `not_run` with `"needs":"person"` and runs no part
   of it. In a person's terminal it runs `drop` as a child with `--yes`.
10. **The result.** `<out>/summary.json`, compact JSON on one line: `leaks` and `controls_failed` (numbers),
    `not_run` (objects with `id` and `needs`) and `ran` (ids; this key is proposed here, so the number of
    attacks run can be reported, as the claims table of the benchmarks page asks). The exit code is 0 when
    `leaks` and `controls_failed` are both 0, else 1.
11. **Growing.** L-04 and L-05 take their events from the hook package's own table, and L-13 and L-15 take
    their commands from the command table, so a new hook event or a new command is attacked with no edit
    here. A test fails when a registered lobe hook event or a store-reading command is not attacked.
12. **`--plant`.** `--plant events` copies acme's event log into globex's store by hand, inside the home the
    action created, before the scan. The run must then report at least 1 leak and exit 1. It exists so a test
    can prove the suite turns red. No build of the product keys a store by anything but the client.

## Where to read

| What | Where |
|---|---|
| Requirements | [M-03](../prd/lobe-benchmarks.md), whole entry; [M-02](../prd/lobe-benchmarks.md), "How to build it", bullets 2 and 3. [Core page](../prd/vertical-lobe.md): section 2 (first anti-goal), 5.1 ("Which client a lobe hook serves"), section 8 (every phase ends at zero leaks; L-09 and D-12), decision A-12. [F-02](../prd/lobe-foundation.md) step 4; [F-07](../prd/lobe-foundation.md) step 3 and its last-but-one risk; [F-01](../prd/lobe-foundation.md) steps 4 and 9; [S-06](../prd/lobe-sense.md) and [S-08](../prd/lobe-sense.md) for what `dup` and `fp` mean |
| Source to port | None: the attacks and the canary scan are ours. Not taken, and the reason L-02 exists: Trail-code [`src/notes/just-me.ts` L12](https://github.com/trailhq/Trail-Code/blob/e02b754c1d4e1de82b841522e2d8cae811702870/src/notes/just-me.ts#L12) |
| Existing tentaqles code | `internal/resolve/resolve_test.go` L71 (a copied marker or manifest lends a folder nothing; L-08 repeats it for the lobe); `internal/testutil/home.go` L18 (`HomeRun`); `internal/resolve/resolve.go` and the result of feature 001; `internal/registry/registry.go` (`AddBase` L99, `Save` L34); `internal/cmd/run.go` L283 (`insideAgent`) and L289 (`refuseOtherWorkspace`); `internal/cmd/drop.go` for L-14 |

## Build tasks

1. **Check this brief against the code as it stands.** Confirm: the lobe hook events registered per host; that
   feature 005 built the scope and login rules of F-07 step 3 for `status`, `events` and `settings`; the name
   of the hook package's event table and of the command table; what feature 001 decided. If the command rules
   are not built, L-13 and L-15 move under `not_run` with `F-07`, and checks 6 and 7 of the Evaluation change.
   The owner approves changes to this file.
2. **Canaries and the scanner.** `internal/lobe/benchcheck/leak/canary.go`. Proof: a unit test in which a
   canary hidden in a file, in a command's output and in a read verb's output is found, 3 of 3, and a clean
   tree gives 0.
3. **The attack table and the runner.** `attacks.go`, `run.go`, and the action in `internal/lobe/cli/bench.go`.
   Start with the control, L-04 and L-05. Proof: `TestLobeLeak_ControlsRecallOwnCanary`,
   `TestLobeLeak_LoginMismatchDoesNothing`.
4. **The other attacks.** These can be written side by side, one agent each, once task 3 is in: L-06 and
   L-08; L-09; L-12; L-13 and L-15; the early parts of L-01, L-02 and L-03; L-14. Proof:
   `TestLobeLeak_SecondFolderWithAClientsName`, `TestLobeLeak_CommandUnderAnotherLoginRefused`,
   `TestLobeLeak_NothingSkippedSilently`, `TestLobeLeak_AllAttacksZeroLeaks`.
5. **The plant.** `--plant events`. Proof: `TestLobeLeak_SuiteGoesRedOnAPlantedLeak`.
6. **CI.** `test/integration/lobeleak_test.go` builds `./cmd/tentaqles` into a temp folder and runs
   `lobe bench leak` with it, on the three systems. No test builds the program today, so add the helper here
   unless an earlier feature left one. Extend `TestLobeBenchShipsOnlyTheFourChecks` to three actions.
7. **Docs.** The `leak` action in the `## tentaqles lobe bench` section of `docs/reference/commands.md`;
   `CHANGELOG.md` under `[Unreleased]`; in `CLAUDE.md`, a new "Must not trade off" line, proposed wording:
   "Nothing of one client's lobe reaches another client's session: `TestLobeLeak_AllAttacksZeroLeaks`,
   `TestLobeLeak_ControlsRecallOwnCanary`, `TestLobeLeak_SuiteGoesRedOnAPlantedLeak`,
   `TestLobeLeak_NothingSkippedSilently`, `TestLobeLeak_LoginMismatchDoesNothing`,
   `TestLobeLeak_CommandUnderAnotherLoginRefused`, `TestLobeLeak_SecondFolderWithAClientsName`." The plugin
   does not change.
8. **Tester run**, then the owner's person check for L-14.

## Tests that stay in the repository

All names are M-03's. They live in `test/integration/lobeleak_test.go` and run the built program.

- `TestLobeLeak_AllAttacksZeroLeaks`: `leaks` is 0 and `controls_failed` is 0 over the attacks that run.
- `TestLobeLeak_ControlsRecallOwnCanary`: every attack's control found its own canary, in both directions.
- `TestLobeLeak_SuiteGoesRedOnAPlantedLeak`: the plant `events` reports at least 1 leak and exit 1. 1 of 1
  now; the plants `lesson` (016) and `graph` (043) make it 3 of 3.
- `TestLobeLeak_NothingSkippedSilently`: each of the 15 attacks is in exactly one of `ran` and `not_run`;
  every `not_run` entry has a `needs`; now `ran` holds 8 ids and `not_run` 7. It also fails when a registered
  lobe hook event or a store-reading command is missing from L-04, L-05, L-13 or L-15.
- `TestLobeLeak_LoginMismatchDoesNothing`: L-04 and L-05 in their three forms, for both hosts: exit 0, 0 bytes
  of output and 0 files changed under `identities/` in 6 of 6 runs, for each event that exists.
- `TestLobeLeak_CommandUnderAnotherLoginRefused`: L-15 for each host's variable: exit 1, no acme canary in
  the output and 0 files changed under `identities/`; with the variable unset the same commands exit 0.
- `TestLobeLeak_SecondFolderWithAClientsName`: L-09 runs, and a write or any output in the second folder is
  counted under `leaks`. To prove the counting, the test also runs the attack's scan over a home in which a
  file was written into the second folder by hand, and expects 1 leak.
- House rules kept green: `TestResolve_SecondFolderWithAClientsName` (001),
  `TestResolve_MarkerOrManifestOfAnotherClient`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeHookLoginUnsetDoesNothing`, `TestLobeStoreRefusesLinkAndForeign`, `TestLobeGuardNeverImportsLobe`,
  `TestLobeImportsOnlyTheFour`, `TestLobeCommandTable`, `TestDocsReferenceEveryCommand`, and every guard
  test under "Must not trade off". The suite never touches the real `~/.tentaqles`.

## Evaluation

A tester agent runs this in an empty working folder with the built `tentaqles` first on `PATH`. Bash (Git
Bash on Windows). No network. Start with the standard tester setup:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
A="$W/clients/acme/app"; G="$W/clients/globex/app"; I="$TQ_HOME/identities"
F="tentaqles lobe-hook claude post-tool-use-failure"
J='{"session_id":"SID","hook_event_name":"PostToolUseFailure","cwd":"CWD","tool_name":"Bash",'
J="$J"'"tool_input":{"command":"WORD FLAG"},"error":"WORD: unknown option"}'
js(){ printf '%s' "$J" | sed -e "s|SID|$1|" -e "s|CWD|$2|" -e "s|WORD|$3|g" -e "s|FLAG|$4|"; }
```

No line may start with `SETUP FAIL`. `js <session> <cwd> <word> <flag>` prints one hook JSON. The setup leaves
both login variables unset; a check sets the one it needs on the command itself.

1. `cd "$A"; js s1 "$A" acmecanaryword --sync | CLAUDE_CONFIG_DIR="$I/acme/claude" $F; echo "exit=$?"` prints
   only `exit=0`, and `cat "$I"/acme/lobe/events/*.jsonl | grep -c acmecanaryword` prints `1`.
2. `grep -r -l acmecanaryword "$I/globex" "$W/clients/globex" | wc -l` prints `0`.
3. Logged in as globex inside acme's folder:
   `js s2 "$A" acmecanaryword --again | CLAUDE_CONFIG_DIR="$I/globex/claude" $F; echo "exit=$?"` prints only
   `exit=0`. The count of check 1 is still `1`, `grep -r -c -e --again "$I" | grep -v ':0$' | wc -l` prints
   `0`, and check 2 still prints `0`. The new session and command keep the duplicate filter from hiding a
   write.
4. No login: `js s3 "$A" acmecanaryword --nologin | $F; echo "exit=$?"` prints only `exit=0`. No file under
   `$I` holds `--nologin`, and the count of check 1 is still `1`.
5. `cd "$G"; js ../../acme/lobe/sessions/evil "$G" globexcanaryword --sync |`
   `CLAUDE_CONFIG_DIR="$I/globex/claude" $F; echo "exit=$?"` prints only `exit=0`. Then
   `find "$I/acme" -name 'evil*' | wc -l` prints `0` and `grep -r -l globexcanaryword "$I/acme" | wc -l`
   prints `0`.
6. `cd "$A"; CLAUDECODE=1 CLAUDE_CONFIG_DIR="$I/globex/claude" tentaqles lobe events; echo "exit=$?"` prints
   `exit=1` and no line holding `acmecanaryword`. The same command without `CLAUDE_CONFIG_DIR` exits 0 and
   prints a line holding `acmecanaryword`.
7. `tentaqles lobe bench leak --out "$W/leak"; echo "exit=$?"` prints `exit=0`. In `$W/leak/summary.json`:
   `"leaks":0` and `"controls_failed":0`; `grep -o '"id":"L-[0-9]*"' | sort` over the file gives exactly
   `L-01`, `L-02`, `L-03`, `L-07`, `L-10`, `L-11` and `L-14`; each of those entries has a `needs`, and
   `L-14` has `"needs":"person"`. `L-09` is not among them: it ran and counts in `leaks` like any other.
8. `tentaqles lobe bench leak --plant events --out "$W/leak2"; echo "exit=$?"` prints `exit=1`, and
   `$W/leak2/summary.json` has a `"leaks"` value of at least 1.
9. `tentaqles lobe bench leak --plant lesson --out "$W/leak3"; echo "exit=$?"` prints `exit=1` and a line
   that names `C-04`, and `test -e "$W/leak3/summary.json"` is false: that plant waits for lessons.
10. `tentaqles lobe bench --help | grep -c -E '^  (leak|latency|stall) '` prints `3`.
11. After check 7, the tester's own stores are as they were: the count of check 1 is still `1` and check 2
    still prints `0`. The suite worked only under `$W/leak`.

**Person checks**

- In a terminal of their own, the owner runs `tentaqles lobe bench leak --out <new folder>` and expects
  `L-14` to be absent from `not_run` and `"leaks":0`. L-14 needs `tentaqles drop`, which a tester never runs.

PASS needs 11 of 11. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI on the three systems (`go vet ./...`,
  `go test ./... -count=1`, `go build ./...`).
- [ ] The tester brief passes, 11 of 11, and the owner's run shows L-14 ran with zero leaks.
- [ ] House rules: zero leaks; every "Must not trade off" test passes unedited; the import tests are green;
  no guard file changed.
- [ ] `CLAUDE.md` has the new line with the owner's yes to its wording; `docs/reference/commands.md` and
  `CHANGELOG.md` are updated.
- [ ] The owner has seen a `summary.json` with `"leaks":0` and the red run of `--plant events`.

## Risks and unknowns

- **L-09 depends on feature 001.** M-03 says the resolver keys a client by folder name and may show a hit
  until the fix lands (read, not run). If 001's first test showed the resolver already refuses, L-09 is green
  with no change. If 001 was skipped, this suite reports a leak and the phase gate stays shut, as it should.
- **The command rules.** L-13, L-15 and tester check 6 assume feature 005 built the scope and login rules of
  F-07 step 3. Task 1 confirms it.
- **What "run" means.** M-03 says an attack that needs a later phase is listed as not run, never as passed.
  This file reads L-13 and L-15 as run in full now, because they are defined over "every command", and they
  grow by the command table. It reads L-01, L-02 and L-03 as not run, with an early part that still scans.
  If the owner reads either differently, only the counts in `TestLobeLeak_NothingSkippedSilently` and in
  tester check 7 change.
- **The `needs` text.** M-03 gives one example, `"needs":"person"`. This file fills it with requirements
  entry ids, comma-separated, because the shipped program should not carry this folder's feature numbers.
- **`ran`.** The key is proposed here; M-03 names only `leaks`, `controls_failed` and `not_run`.
- **L-14 needs a new `acme` in a home that still holds `globex`.** The sandbox builder refuses a home that
  has a client, by design (F-07 step 8). Proposed: `internal/cmd/lobe_sandbox.go` also hands over a
  one-client function, never offered as a command, that keeps the builder's first two refusals (the three
  home variables inside the folder, the git variables unset). It needs the owner's yes, because it widens
  what feature 002 left. Without it, L-14 can check only that nothing of acme remains after the drop.
- **The rule trusts the login variable.** L-04, L-05 and L-15 keep one client's memory out of a session
  logged in as another. They are no defense against a process that sets both the folder and the variable to
  acme's, which is simply an acme session (M-03, first risk).
- **A shared lesson is not a leak.** From feature 033 on, a lesson committed to a repository reaches whoever
  clones it. That is the design (F-04); the scanner must not plant a canary in a shared lesson.
- **Windows links.** L-07 will need a junction on Windows. It is out of scope here, but the scanner must not
  follow links out of the attack's home when it walks a tree.
- **What would force a change to this file:** a different set of hooks or commands after 011; another outcome
  of feature 001; the owner's reading of "run" or of the `needs` text.
