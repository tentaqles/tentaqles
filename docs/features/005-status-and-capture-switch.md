# 005: See what was captured, and switch it off

**Status** not started · **Stage** Sense · **Entries** F-06 (reading, `lobe settings`, `lobe set`, the reader
of `capture`), F-07 (`lobe status`, the command rules, eight doctor findings), S-07 (`lobe events` and the
reader rule of step 8), F-01 (the render step opens the store) · **Needs** 004 · **Feeds** 006, 007, 011, 014,
016, 043

## Result

A person in a client's folder can see what the lobe holds and stop it. `tentaqles lobe status` prints the
client, the store's health, the settings and the number of events. `tentaqles lobe events` prints the newest
scrubbed lines. `tentaqles lobe settings` prints the nine settings and where each value comes from.
`tentaqles lobe set capture off` stops recording from the next tool call; `on` starts it again.
`tentaqles doctor` reports a broken store, a wrong setting or a failing hook, with the client's name.

```bash
cd <a client's folder>
tentaqles lobe status
tentaqles lobe events
tentaqles lobe settings
tentaqles lobe set capture off      # a person's terminal only; asks for a typed yes
tentaqles doctor
```

## Scope

**In**

- `internal/manifest`: the field `Lobe *yaml.Node` with the tag `lobe,omitempty`, never validated (F-06 step
  2). If 002 already added it, keep it.
- `internal/lobe/settings`: `From(ws)`, the defaults, the safe values, `Edit(raw, key, value)` (F-06 steps
  1, 2, 5). All nine keys are read, printed and can be set. Only `capture` has a reader in the code so far.
- The hook reads the settings after the client rule (F-02 step 5). With `capture` off, Sense writes nothing.
- `tentaqles lobe settings [--client <name>] [--json]` (F-06 step 3).
- `tentaqles lobe set <key> <value> [--client <name>] [--yes]`, for a person only, with the value `default`
  and the reminders (F-06 steps 4 to 6).
- The log readers `eventlog.Scan` and `eventlog.Tail` (S-07 step 8) and `tentaqles lobe events`.
- `tentaqles lobe status [--json]` (F-07 step 1 and "How to build it").
- The rules every `tentaqles lobe` command follows: person-only, the client it works on, and the login check
  (F-07 steps 2 and 3).
- `internal/lobe/health` and eight doctor findings: `lobe-store-linked`, `lobe-store-foreign`,
  `lobe-store-missing`, `lobe-store-unreadable`, `lobe-store-version`, `lobe-hooks-missing`,
  `lobe-setting-invalid`, `lobe-hook-failing` (F-07 step 5).
- The render step of `tentaqles reach` and `tentaqles settings render` opens the store once (F-01 step 3).

**Out**

- `tentaqles lobe events purge` (011, with the drops file it also deletes).
- `tentaqles lobe status --savings` (058).
- What each other key does. The key is read and shown from now on; its behavior arrives with its feature:

  | Key | Its reader arrives with |
  |---|---|
  | `capture` | this feature |
  | `retention_days` | 011, the sweep |
  | `recall`, `lessons`, `enforce` | 014, the first thing the lobe says; `enforce: block` acts as advice until 038 |
  | `model` | 025 |
  | `share` | 033 |
  | `graph` | 043 |
  | `documents` | 061, with F-06 step 7 and `TestLobeDocumentsSwitch` |

- `TestLobeSwitchesAreIndependent`: its rows need a lesson and a graph (017 and 043).
- In `lobe status`: the fingerprint count (006), lessons per stage and `quarantine` (016), `stale` (032),
  `tampered_sessions` (034).
- A reference line shown with its first line in `lobe events` (007).
- The other five doctor findings: `lobe-hooks-untrusted` (010), `lobe-events-dropped` (011),
  `lobe-lesson-fork` (031), `lobe-private-in-repo` (033), `lobe-graph-unsound` (044).
- E-01's stronger person check (terminals, the parent walk). `lobe set` uses the plain agent-session check
  that `tentaqles add` uses. The five verbs that need the stronger check arrive from 024 on.

## What it builds on

- **002**: the sandbox, the `lobe` command group in `internal/cmd/lobe.go`, and the helper `lobeset` of the
  standard tester setup. The sandbox manifests already end with a `lobe` mapping that holds `model: off`.
- **003**: `store.Open` and its refusals (the findings report what `Open` refuses), `hook.Main` and its
  order of work, `stats/hook-errors.jsonl`, `eventlog.Event`, `eventlog.Append`, the plugin entry.
- **004**: every tool in the log, and `ok` lines (the grep rule), so counts are by `kind`.
- Existing tentaqles code, used from `internal/cmd` only: `insideAgent()` (`internal/cmd/run.go` L283),
  `refuseOtherWorkspace` (L289) as the pattern for another client, `changeTarget` and the flags of
  `tentaqles add` (`internal/cmd/edit.go` L57, L83 to L84), `confirm` and `writeFileAtomic`
  (`internal/cmd/reach.go` L283 to L336), `renderIdentityFiles` (`internal/cmd/settings.go` L125),
  `guardplugin.PluginRoot` (`internal/guardplugin/plugin.go` L28), `doctor.Finding` and `doctor.Exit`.

## What it leaves for later features

| Name | What it is | Used by |
|---|---|---|
| `settings.From(ws)` | the nine effective values, each with its source (`default`, `manifest`, `invalid`) | every feature that adds a reader: 011, 014, 025, 033, 043, 061 |
| `settings.Edit(raw, key, value)` | changes `lobe.<key>` only; the same bytes when nothing changes | `lobe set`; nothing else writes the manifest |
| `manifest.Manifest.Lobe` | the raw `lobe` node | `settings.From` only |
| `eventlog.Scan`, `eventlog.Tail` | lock-free readers that skip and count broken lines | 006, 007, 011, 018, 020 |
| `internal/lobe/cli` and `cli.Deps` | the commands anyone may run; `Deps` carries "is this an agent session" as a function value | every later `tentaqles lobe` verb |
| The command rules | one helper for "which client" and one for the login check | every later verb; 013 attacks them (L-13, L-15) |
| `internal/lobe/health` | `Findings(...)`, returned to `internal/cmd/doctor.go` | 010, 011, 031, 033, 044 add their codes |
| `lobe status --json` | `client`, `store`, `settings`, `events`, `hooks`; later features add keys, none is renamed | 006, 016, 032, 034, 058 |
| The store exists after a render | `lobe-store-missing` means something | 013, 016 |

## How it works

1. **The manifest field.** `Lobe` accepts any YAML value and `manifest.Parse` never checks it, because the
   guard denies every shell command when a manifest fails to load (F-06 "How to build it"). Unknown keys
   were already ignored on load (`internal/manifest/manifest.go` L113), so older binaries read the file too.
2. **Reading.** `settings.From(ws)` decodes the node. An absent key gives the default. A value outside the
   key's list gives the safe value of F-06 step 1 ("Wrong value") and marks the key `invalid`. A `lobe` value
   that is not a mapping makes all nine keys invalid. `on` and `off` are read bare or quoted, and `true`,
   `false`, `yes` and `no` are read as well. No environment variable overrides a setting.
3. **`capture`**: values `on`, `off`; default `on`; wrong value `off`. While it is off no event is written
   and the hook still exits 0 with no output; what is in the log stays and can be read. Switched back on,
   recording starts with the next call and nothing from the off period is recovered (F-06 step 1). The hook
   reads the settings after the client rule and before `store.Open`, the order of F-02 step 5; an unreadable
   manifest ends the hook with no output. The check itself sits in Sense (S-01 step 2).
4. **`tentaqles lobe settings`** prints one line per key in the order `capture`, `recall`, `lessons`,
   `graph`, `enforce`, `share`, `model`, `documents`, `retention_days`, as `enforce=advice (default)`. A key
   that is on but cannot act because another key is off says so: `documents=on (manifest, needs model)`.
   `--json` prints one compact object on one line with the nine keys and their effective values.
5. **`tentaqles lobe set <key> <value>`** lives in `internal/cmd/lobe_set.go`. It refuses inside an agent
   session and changes nothing. It refuses an unknown key and a value outside the key's list, with exit 1.
   It lists the change and waits for a typed yes; `--yes` skips the wait. The value `default` removes the
   key, and the `lobe` mapping when it is then empty. The edit works on the YAML node tree and touches
   `lobe.<key>` only; a new `lobe` mapping is added as the last top-level key. No change returns the same
   bytes and prints `Nothing changed.` The result is checked by `manifest.Parse` and written with
   `writeFileAtomic` at mode 0600. It prints one reminder line when it leaves a pair that surprises (F-06
   step 6): `recall: off` with `enforce: block`; `documents: on` while `model` or `graph` is off;
   `enforce: block` before blocking ships.
6. **Readers** (S-07 step 8). `Scan` walks the day files oldest first; `Tail(path, max)` reads the end of
   one file. Both take no lock, read whole lines, skip a blank line, a line that is not JSON and a last line
   with no line break, and count the lines they skipped.
7. **`tentaqles lobe events`** prints the newest stored lines as they are on disk, one JSON object per line,
   oldest of the selection first, at most 20 (the number is proposed; F-07 gives the command no flag for
   it). It prints nothing and exits 0 when the log is empty.
8. **`tentaqles lobe status`** prints, as text: the client; the store's state; the nine settings; the event
   count in total and by `kind`, the number of day files and of skipped lines; and whether the plugin this
   client's Claude Code loads has the lobe's entries. When `CLAUDE_CONFIG_DIR` is unset or is not this
   client's login folder, one more line names the variable and says that the lobe's hooks do nothing in this
   terminal (F-07 "How to build it", F-02 risks). `--json` prints one compact object on one line:
   `{"client":"acme","store":{"state":"ok","v":1},"settings":{...},"events":{"total":3,"fail":3,"ok":0,
   "hidden_fail":0,"days":1,"skipped":0},"hooks":{"claude":"ok"}}`. `client`, `store`, `settings`, `events`
   and `hooks` are F-07's keys; the keys inside `events` and the values of `hooks` (`ok`, `missing`, `none`
   when no plugin is installed) are proposed here. A refused store prints one line that names `store.json`
   or the linked folder, and exits 1.
9. **The command rules** (F-07 steps 2 and 3), built once and used by all four commands:
   - A command works on the current folder's client, or on `--client`.
   - Inside an agent session (`CLAUDECODE` set), `--client` is accepted only when it names the current
     folder's client. Another client, or any client from a neutral folder, is refused with exit 1.
   - Inside an agent session a command that reads or writes a store also refuses, with exit 1 and nothing
     from the store printed, when `CLAUDE_CONFIG_DIR` or `CODEX_HOME` is set to anything but this client's
     own login folder. The paths are compared as in the hook's client rule (003 step 6). With both unset the
     command works.
   - `lobe set` is person-only: it refuses whenever `CLAUDECODE` is set.
   - A verb in `internal/lobe/cli` learns "is this an agent session" through `cli.Deps`, as `doctor.Deps`
     does (`internal/doctor/doctor.go` L28), because a lobe package may not import `internal/cmd`.
10. **Health.** `internal/lobe/health` returns findings; `internal/cmd/doctor.go` appends them to the list
    of `doctor.Run`, so the text and `--json` output and the exit code treat them like any other finding.
    They stay out of `internal/doctor`, which the guard imports (F-02 "How to build it"). `health` cannot
    import `internal/guardplugin`, so `doctor.go` finds the installed plugin with `guardplugin.PluginRoot`
    and hands the bytes of its `hooks/hooks.json` to `health`. The eight codes, their levels and their
    conditions are F-07 step 5. `lobe-setting-invalid` is one finding per client, however many keys are
    wrong. `lobe-hook-failing` counts lines of `stats/hook-errors.jsonl` from the last 24 hours and reports
    at 5 or more (proposed). `lobe-hooks-missing` is reported only when a plugin is installed and its
    `hooks.json` has no `lobe_hook.sh` entry.
11. **The render step.** `renderIdentityFiles` calls `store.Open(ws, ReadWrite)` once, after the Claude
    settings. A refusal is a warning line, never an error: the render goes on. The sandbox runs the same
    step, so both sandbox clients have a store before any hook runs.

## Where to read

| What | Where |
|---|---|
| Requirements | [F-06](../prd/lobe-foundation.md), all but step 7; [F-07](../prd/lobe-foundation.md) steps 1 to 5, "How to build it" and the standard tester setup; [F-01](../prd/lobe-foundation.md) steps 3 and 4; [F-02](../prd/lobe-foundation.md) step 5 |
| Requirements | [S-07](../prd/lobe-sense.md) step 8 and the `tentaqles lobe events` bullet of "How to build it"; [S-01](../prd/lobe-sense.md) step 2 |
| Shared rules | [core page](../prd/vertical-lobe.md) 5.1 (the fourth import rule), 5.6 (settings), section 9 (tester rules), A-21 |
| Test names that guard these paths | [M-03](../prd/lobe-benchmarks.md) attacks L-13 and L-15 (`TestLobeLeak_CommandUnderAnotherLoginRefused`, built in 013) |
| Source to port | none; this feature is tentaqles' own design on tentaqles' own patterns. For the reader rule: [`src/capture/spool.ts` L104](https://github.com/rohitg00/agentmemory/blob/77f45358a8300299158e3a416e9010176408ea87/src/capture/spool.ts#L104) (L251: a reader skips a line that does not parse) |
| Existing tentaqles code: the edit pattern | `internal/reach/edit.go` L99 to L105 (only the keys the form owns), L252 to L254 (no change, same bytes), L270 (parse before returning); `internal/cmd/reach.go` L283 (`applyEdit`), L336 (`writeFileAtomic`) |
| Existing tentaqles code: person and client | `internal/cmd/edit.go` L57 (refuses in an agent session), L83 to L84 (`--client`, `--yes`); `internal/cmd/run.go` L283 (`insideAgent`), L289 (`refuseOtherWorkspace`) |
| Existing tentaqles code: doctor | `internal/cmd/doctor.go` L30 to L60; `internal/doctor/doctor.go` L20 to L45 (`Finding`, `Deps`), L191 (`Exit`); `internal/guardplugin/plugin.go` L28 (`PluginRoot`) |
| Existing tentaqles code: manifest and render | `internal/manifest/manifest.go` L68 to L90 (the type), L108 to L115 (`Parse`); `internal/cmd/settings.go` L125 (`renderIdentityFiles`); `internal/cmd/claudehook.go` L282 (an unreadable manifest is a guard deny) |
| Docs test | `test/integration/docs_test.go` L16 (`documentedGroups`), L41 |

## Build tasks

Tasks 2 and 3 can run side by side; so can tasks 5, 6 and 7 once task 4 is done.

1. **Check this brief against the code as it stands**: whether 002 added `Manifest.Lobe`, what the sandbox
   writes, the names 003 and 004 gave the hook's step table, the line numbers above. List what no longer
   fits; the owner approves changes to this file first.
2. **The manifest field and `internal/lobe/settings`.** Files: `internal/manifest/manifest.go`,
   `internal/lobe/settings/settings.go`, `edit.go`. Tests: `TestLobeSettingsDefaults`,
   `TestLobeSettingsSafeValues`, `TestLobeBadSettingNeverBreaksTheGuard`, `TestEdit_KeepsTheLobeKey` (in
   `internal/reach`).
3. **The readers.** Files: `internal/lobe/eventlog/read.go`. Test: `TestLobeLogReaderSkipsBrokenLines`
   (name proposed here).
4. **The command rules and `internal/lobe/cli`.** Files: `internal/lobe/cli/cli.go` (the `Deps` type, the
   client and login helpers), `internal/cmd/lobe.go` (registration). Tests:
   `TestLobeAgentCannotReadAnotherClient`, `TestLobeCommandRefusesAnotherLogin`, written against the three
   read verbs as tasks 5 to 7 add them.
5. **`capture` in the hook.** Files: `internal/lobe/hook/main.go` (the settings read), `internal/lobe/sense`
   (the skip). Test: `TestLobeSenseFailureSkips`, now 7 of 7.
6. **`tentaqles lobe settings` and `tentaqles lobe set`.** Files: `internal/lobe/cli/settings.go`,
   `internal/cmd/lobe_set.go`. Tests: `TestLobeSet_ChangesOnlyTheLobeKey`,
   `TestLobePersonVerbsRefuseInAgent`.
7. **`tentaqles lobe events` and `tentaqles lobe status`.** Files: `internal/lobe/cli/events.go`,
   `status.go`. Tests: `TestLobeStatusJSON` (name proposed here), `TestLobeCommandTable`.
8. **Health and the render step.** Files: `internal/lobe/health/health.go`, `internal/cmd/doctor.go`,
   `internal/cmd/settings.go`. Tests: `TestLobeDoctorCodes`, `TestLobeHealthNeverReachesTheGuard`,
   `TestSettingsRender_OpensTheLobeStore` (name proposed here), `TestLobeSandbox_BuildsTheStandardPair`
   with its "two stores" line, and the `TestReach_*` and `TestRun_*` tests unedited.
9. **Docs.** `docs/reference/commands.md`: the sections `## tentaqles lobe status`, `## tentaqles lobe
   events`, `## tentaqles lobe settings` and `## tentaqles lobe set`, and the lobe findings under
   `## tentaqles doctor`. `docs/reference/manifest.md`: the `lobe` key with its nine settings, saying which
   one acts today. `CHANGELOG.md` under `[Unreleased]`. `CLAUDE.md`: the new packages under Layout, and one
   "Must not trade off" line (see Done when). The plugin does not change.
10. **The tester run** (Evaluation), by a fresh agent, then the owner's Person checks.

## Tests that stay in the repository

Names are the requirements' unless marked.

- `TestLobeSettingsDefaults`: a manifest with no `lobe` key gives the 9 defaults of core page 5.6. 9 of 9.
- `TestLobeSettingsSafeValues`: a wrong value under each key gives the table's value and one finding. 9 of 9.
- `TestLobeSet_ChangesOnlyTheLobeKey`: after a set, a diff of the manifest shows lines under `lobe` only;
  setting a key to its current value returns identical bytes; `default` removes the key and then the empty
  mapping.
- `TestLobeBadSettingNeverBreaksTheGuard`: with `lobe: [1, 2]` in the manifest, `claude-hook pre-tool-use` on
  `git status` exits 0.
- `TestEdit_KeepsTheLobeKey`: `reach.Edit` on a manifest with a `lobe` mapping returns it byte for byte.
- `TestLobeSenseFailureSkips`: the six rows of 004 and `capture: off`. 0 lines in any store, exit 0 and empty
  stdout in 7 of 7.
- `TestLobeLogReaderSkipsBrokenLines` (name proposed here): a file with a blank line, a line that is not
  JSON and a last line with no line break gives the whole lines, in order, and a skip count of 2.
- `TestLobeStatusJSON` (name proposed here): one line of compact JSON with `client`, `store`, `settings`,
  `events` and `hooks`; a foreign `store.json` gives exit 1 and a line naming the file.
- `TestLobeCommandTable`: every command registered under `lobe` and `lobe-hook` is a row of F-07 step 1,
  and the ones that refuse under `CLAUDECODE=1` are exactly the rows marked "yes". The test holds the list
  of rows built so far (here: `lobe sandbox`, `lobe status`, `lobe events`, `lobe settings`, `lobe set`,
  `lobe-hook claude`); each later feature adds its rows, and the list ends as the whole table.
- `TestLobeAgentCannotReadAnotherClient`: with `CLAUDECODE=1` in acme's folder, `status`, `events` and
  `settings` with `--client globex` are refused, 3 of 3; the same from a neutral folder with `--client
  acme`, 3 of 3. Later features bring it to the requirements' 6 verbs.
- `TestLobeCommandRefusesAnotherLogin`: with `CLAUDECODE=1` in acme's folder and the login variable naming
  globex's identity folder, the same 3 verbs exit 1 and print nothing from the store; with the variable
  unset they work. 3 of 3, for each host's variable. Later features bring it to 11.
- `TestLobePersonVerbsRefuseInAgent`: `lobe set` exits non-zero under `CLAUDECODE=1` and changes 0 bytes.
- `TestLobeDoctorCodes`: each of the 8 codes built here is produced by its fixture and by no other. 8 of 8
  here; 13 of 13 when the last arrives.
- `TestLobeHealthNeverReachesTheGuard`: with every lobe finding present, the guard still allows `echo hi`.
- `TestSettingsRender_OpensTheLobeStore` (name proposed here): after a render the store exists with
  `"client":"<name>"`; a linked `lobe` folder gives a warning and the render still finishes.
- `TestDocsReferenceEveryCommand`: 0 commands without a section and 0 sections without a command.

House-rule tests kept green, unedited: `TestLobeHookAlwaysExitsZero` (its "unreadable manifest" row now
passes through the settings read), `TestLobeHookServesOnlyItsLogin` (the client rule still runs before the
settings are read, so no setting can switch it off), `TestLobeHookLoginUnsetDoesNothing`, the `TestLobeScrub*`
tests, `TestLobeGuardNeverImportsLobe` (it now also proves that `internal/doctor` reaches no lobe package),
`TestLobeImportsOnlyTheFour`, and every test under "Must not trade off" in `CLAUDE.md`, among them
`TestEdit_NoChangeIsTheSameBytes` and `TestReach_ConfirmsBeforeWriting`.

## Evaluation

The tester is a fresh agent with the built `tentaqles` first on `PATH` and an empty working folder. Use bash
(Git Bash on Windows). No network. A tester never runs `tentaqles lobe set` except to see it refuse; the
helper `lobeset` edits a sandbox manifest as a fixture. Run the standard setup of feature 002 first:

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED TQ_LOBE_NO_HOOKS
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
tentaqles lobe sandbox "$W" || echo "SETUP FAIL sandbox"
for c in acme globex; do git init -q "$W/clients/$c/app" || echo "SETUP FAIL git $c"; done
lobeset() {  # lobeset <client> <key> <value>: a fixture edit of a sandbox manifest
  m="$TQ_HOME/identities/$1/manifest.yaml"
  awk -v k="  $2:" -v v="$3" '/^lobe:/{l=1} !(l && index($0,k)==1){print} END{print k" "v}' "$m" > "$m.new" &&
    mv "$m.new" "$m"
}
```

Stop and report FAIL when a line starts with `SETUP FAIL`. Then:

```bash
ID="$TQ_HOME/identities"; ACME="$W/clients/acme"; GLOBEX="$W/clients/globex"
A="$ID/acme/lobe"; G="$ID/globex/lobe"; LOG="$A/events/events-$(date -u +%Y%m%d).jsonl"
fail() {  # fail <client> <tool_use_id> <tool> <input json>: one failed call, under that client's login
  printf '{"session_id":"s1","hook_event_name":"PostToolUseFailure","cwd":"%s","tool_name":"%s","tool_use_id":"%s","tool_input":%s,"error":"nope"}' \
    "$W/clients/$1" "$3" "$2" "$4" |
    CLAUDE_CONFIG_DIR="$ID/$1/claude" tentaqles lobe-hook claude post-tool-use-failure
}
```

The login variable is set only inside `fail`. Every other command runs with it unset, as a person's plain
terminal would. `cd "$ACME"` before check 1.

1. **A fresh setup is healthy.** `tentaqles doctor --json | grep -c '"code":"lobe-'` prints `0`.
   `test -f "$A/store.json" && test -f "$G/store.json"` is true: the render opened both stores.
   `tentaqles lobe status --json` prints one line, a JSON object holding `"client":"acme"`, `"state":"ok"`
   and `"total":0`. `tentaqles lobe events; echo "exit=$?"` prints only `exit=0`.
2. **What was captured.** `fail acme toolu_01 Bash '{"command":"npm run buld"}'`, then
   `fail acme toolu_02 Edit '{"file_path":"x.txt","old_string":"a","new_string":"b"}'`, then
   `fail acme toolu_03 mcp__acme__ping '{"password":"hunter2"}'`. Each prints nothing.
   `tentaqles lobe events | wc -l` prints `3`. Each printed line parses as JSON; the first has `id`
   `evt_9bde2e09` and the last `evt_7fe44cf7`. `tentaqles lobe events | grep -c hunter2` prints `0`.
   `tentaqles lobe status --json | grep -c '"total":3'` prints `1`. `tentaqles lobe status` prints the name
   `acme`, the number `3`, and a line that names `CLAUDE_CONFIG_DIR`.
3. **The switch.** `lobeset acme capture off`, then `fail acme toolu_04 Bash '{"command":"x --off"}'; echo
   "exit=$?"` prints only `exit=0`. `tentaqles lobe events | wc -l` still prints `3`, and
   `tentaqles lobe settings | grep capture` prints `capture=off (manifest)`. Then `lobeset acme capture on`
   and `fail acme toolu_05 Bash '{"command":"x --on"}'`: `tentaqles lobe events | wc -l` prints `4`, and no
   line holds `--off`.
4. **The nine settings.** `cd "$GLOBEX"; tentaqles lobe settings` prints 9 lines, in the order `capture`,
   `recall`, `lessons`, `graph`, `enforce`, `share`, `model`, `documents`, `retention_days`, among them
   `capture=on (default)`, `enforce=advice (default)`, `graph=on (default)`, `lessons=on (default)`,
   `share=private (default)`, `documents=off (default)`, `retention_days=14 (default)` and
   `model=off (manifest)`. `tentaqles lobe settings --json | grep -c '"capture":"on"'` prints `1`.
5. **A wrong value is the safe value.** `lobeset globex capture maybe; tentaqles lobe settings | grep
   capture` prints `capture=off (invalid)`, and
   `tentaqles doctor --json | grep -c '"code":"lobe-setting-invalid"'` prints `1`. Then
   `fail globex toolu_06 Bash '{"command":"x"}'` prints nothing and `ls "$G/events" 2>/dev/null | wc -l`
   prints `0`. Then `lobeset globex capture on; lobeset globex documents on; tentaqles lobe settings | grep
   documents` prints `documents=on (manifest, needs model)`.
6. **A `lobe` value that is no mapping.** `N="$ID/globex/manifest.yaml"`, then
   `awk '/^lobe:/{exit} {print}' "$N" > "$W/n"; printf 'lobe: 7\n' >> "$W/n"; cp "$W/n" "$N"`. Then
   `tentaqles lobe settings | grep -c '(invalid)'` prints `9`, and the `lobe-setting-invalid` count is still
   `1`. The guard still runs for globex:
   `printf '{"cwd":"%s","tool_name":"Bash","tool_input":{"command":"ls"}}' "$GLOBEX" > "$W/g.json"`, then
   `tentaqles claude-hook pre-tool-use < "$W/g.json"; echo $?` prints `0`.
7. **`set` answers only to a person.** `cd "$ACME"; cp "$ID/acme/manifest.yaml" "$W/m0"`, then
   `CLAUDECODE=1 tentaqles lobe set capture off --yes; echo "exit=$?"` prints `exit=1`, and
   `cmp "$W/m0" "$ID/acme/manifest.yaml"` is silent.
8. **Another client, another login.** In `$ACME`:
   `CLAUDECODE=1 tentaqles lobe status --client globex; echo "exit=$?"` prints `exit=1` and nothing of
   globex. `(cd "$W" && CLAUDECODE=1 tentaqles lobe settings --client acme; echo "exit=$?")` prints `exit=1`
   and no setting. `CLAUDECODE=1 CLAUDE_CONFIG_DIR="$ID/globex/claude" tentaqles lobe events; echo "exit=$?"`
   prints `exit=1` and no line holding `evt_`; so does the same command with
   `CODEX_HOME="$ID/globex/codex"` in place of `CLAUDE_CONFIG_DIR`. With neither variable set,
   `CLAUDECODE=1 tentaqles lobe events | wc -l` prints `4`.
9. **A broken line does not stop a reader.** `printf '{"v":\n' >> "$LOG"`. `tentaqles lobe events | wc -l`
   still prints `4`, with exit 0, and `tentaqles lobe status --json | grep -c '"skipped":1'` prints `1`.
10. **The findings.** Run this check last. Each count comes from
    `tentaqles doctor --json | grep -c '"code":"<code>"'`.
    - Five error lines, written by hand as a fixture; then `lobe-hook-failing` prints `1`:

      ```bash
      mkdir -p "$A/stats"; NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      L='{"at":"%s","host":"claude","event":"post-tool-use-failure","step":"S-07","class":"io"}\n'
      for i in 1 2 3 4 5; do printf "$L" "$NOW" >> "$A/stats/hook-errors.jsonl"; done
      ```
    - `cp "$A/store.json" "$W/store.bak"; cp "$G/store.json" "$A/store.json"`. `lobe-store-foreign` prints
      `1`. `tentaqles lobe status; echo "exit=$?"` prints a line naming `store.json` and `exit=1`.
      `fail acme toolu_07 Bash '{"command":"x --foreign"}'` prints nothing, and `grep -c -- --foreign "$LOG"`
      prints `0`.
    - `printf x > "$A/store.json"`. `lobe-store-unreadable` prints `1`.
    - `printf '{"v":99,"client":"acme","created":"2026-01-01T00:00:00Z"}' > "$A/store.json"`.
      `lobe-store-version` prints `1`. Then `cp "$W/store.bak" "$A/store.json"`.
    - `rm -rf "$G"`. `lobe-store-missing` prints `1`.

    `lobe-store-linked` and `lobe-hooks-missing` need a junction and an installed plugin; they are covered
    by `TestLobeDoctorCodes` and are not part of this brief.

**Person checks**, for the owner, in a terminal of their own after exporting the tester's `HOME`,
`USERPROFILE` and `TQ_HOME`. `N="$TQ_HOME/identities/acme/manifest.yaml"; S="tentaqles lobe set capture"`.
Run them in `clients/acme`, before the tester's check 10 or in a fresh sandbox.

1. `cp "$N" "$W/m1"; $S off --yes; echo "exit=$?"` prints `exit=0`; `tentaqles lobe settings | grep capture`
   prints `capture=off (manifest)`; `diff "$W/m1" "$N" | grep '^[<>]' | grep -v -c capture` prints `0`.
2. `cp "$N" "$W/m2"; $S off --yes` prints `Nothing changed.` and `cmp "$W/m2" "$N"` is silent.
3. `$S maybe --yes; echo "exit=$?"` prints `exit=1`, and `cmp "$W/m2" "$N"` stays silent.
4. `$S default --yes; tentaqles lobe settings | grep capture` prints `capture=on (default)`.
5. `$S off` with no `--yes` lists the change and waits. Answer anything but yes: the manifest is unchanged.
6. `tentaqles lobe set recall off --yes; tentaqles lobe set enforce block --yes` prints a line saying that a
   block rule still denies with recall off. Set both back with `default`.
7. In a real client: `tentaqles lobe status` and `tentaqles lobe events` show the failures of your own
   sessions. `tentaqles lobe set capture off`, fail a command in a Claude Code session, and see no new line.
   Switch it back on.

**Scoring.** PASS needs 10 of 10. "Could not run" is a FAIL. Report anything surprising; fix nothing.

## Done when

- [ ] Every test under "Tests that stay in the repository" is in the repository and green in CI on three
  systems: `go vet ./...`, `go test ./... -count=1`, `go build ./...`. The plugin suite stays green.
- [ ] The tester brief passes, 10 of 10.
- [ ] The house rules hold: `TestLobeHookAlwaysExitsZero`, `TestLobeHookServesOnlyItsLogin`,
  `TestLobeScrub*`, `TestLobeGuardNeverImportsLobe`, `TestLobeImportsOnlyTheFour`,
  `TestLobeBadSettingNeverBreaksTheGuard`, `TestLobeHealthNeverReachesTheGuard`, and the guard's tests
  unedited.
- [ ] `CLAUDE.md` has one new "Must not trade off" line: a `tentaqles lobe` command inside an agent session
  reads only its own folder's client under that client's login, and `lobe set` answers only to a person
  (`TestLobeAgentCannotReadAnotherClient`, `TestLobeCommandRefusesAnotherLogin`,
  `TestLobePersonVerbsRefuseInAgent`, `TestLobeSet_ChangesOnlyTheLobeKey`).
- [ ] `docs/reference/commands.md`, `docs/reference/manifest.md` and `CHANGELOG.md` are updated.
- [ ] The owner has switched capture off and on in a client of their own and seen it in the log (Person
  check 7).

## Risks and unknowns

- **`on` and `off` in YAML.** The encoder may write `"on"` in quotes, because old YAML reads a bare `on` as
  true (F-06 "How to build it"). Both forms must read the same, and `lobeset` writes the bare form.
- **`lobeset` relies on `lobe` being the last top-level key.** `settings.Edit` adds a new mapping last, and
  the sandbox writes it last. A manifest edited by `tentaqles add` afterwards must keep it last, or the
  helper breaks; `TestEdit_KeepsTheLobeKey` covers the bytes, not the position of a new key.
- **All nine keys can be set although one acts.** That is F-06 as written. A person who sets `enforce` or
  `retention_days` today changes nothing until 014 or 011. `docs/reference/manifest.md` says which keys act.
- **A wrong value is the safe value**, so a typo in `capture` silently stops recording. The only notices
  are the doctor finding and the `(invalid)` mark (F-06 risks).
- **`lobe status` can create a store.** F-01 step 3 lets any `tentaqles lobe` verb create it when absent.
  `tentaqles doctor` never does, so `lobe-store-missing` can show for a client whose folder was never
  rendered since this feature and disappear after the first `lobe status`.
- **Proposed here, not in the requirements**: the keys inside `events` and the values of `hooks` in
  `lobe status --json`, the 20 lines of `lobe events` and their order, and three test names. The thresholds
  of `lobe-hook-failing` are proposed in F-07.
- **`CLAUDECODE` is the only agent marker** tentaqles reads, and it is a convenience, not a boundary (F-07
  risks). No marker is known for a Codex session; 010's capture looks for one.
- **A `yaml.Node` field in the manifest type** is new there. Any code that writes a manifest by marshaling
  the type must carry the node through unchanged; `workspace.Add` and `reach.Edit` are the two writers to
  check in task 1.
