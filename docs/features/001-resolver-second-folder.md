# 001: Resolver refuses a second folder with a client's name

**Status** built and evaluated on Windows on 2026-11-09; not committed; CI on Linux and macOS not run · **Stage** Ground · **Entries** D-12, M-03 (attack L-09) · **Needs** none ·
**Feeds** 002, 013

## Result

A folder that carries a client's name and a copy of its marker, under another registered clients folder, is
no client. A terminal opened there gets no identity, `tentaqles list` shows the client once, and the real
client's folder works as before. This is identity work. No lobe code exists yet and none is written here.

What shows it, from the repository (PowerShell on the Windows dev box):

```
go test ./internal/resolve -run "TestResolve_SecondFolderWithAClientsName|TestResolve_MarkerOrManifestOfAnotherClient" -count=1 -v
```

And with the built program, in the hand-made home of the Evaluation section:

```bash
cd "$W/one/acme/app" && tentaqles env --shell bash --json   # "workspace":"acme"
cd "$W/two/acme"     && tentaqles env --shell bash --json   # "workspace":""
```

## Scope

**In**

- One test in `internal/resolve` that reproduces the case and settles a claim that was read from the code and
  never run: does `two/acme`, holding a copy of acme's marker, resolve to acme?
- If it already refuses: the test stays as the guard, the `CLAUDE.md` line gains its name, and no code changes.
- If it resolves: a written plan with the options below, the owner's pick, then the change that binds a client
  to the folder it was created in, with its tests.
- A walk through every caller of the resolver, so the plan says what each does in the second folder.

**Out**

- The lobe-level attack L-09 and its test `TestLobeLeak_SecondFolderWithAClientsName` (feature
  [013](013-isolation-leak-suite.md)). It needs a lobe hook, which does not exist yet.
- Any change to what `tentaqles reach` asks or to its duplicate check, which already refuses to create a second
  client with a taken name (`TestReach_SameNameElsewhereRefused`).
- Any lobe package, the sandbox (feature [002](002-sandbox.md)) and the store.
- Weakening, skipping or editing any test named under "Must not trade off" in `CLAUDE.md`. None is touched.

## What it builds on

Nothing of the lobe; this is the first feature. It uses what tentaqles has today:

- `internal/resolve`: `Resolve`, `ListWorkspaces`, `Find`, and the unexported `folder`, `locate` and `load`.
- `internal/registry`: `Config{Bases}`, `Load`, `Save`, `AddBase`, `Normalize`, `SamePath`.
- `internal/manifest`: `MarkerBytes`, `Peek`, `Load`, `Parse` and its secret scan.
- `internal/workspace`: `Add`, which writes the manifest, the marker and the registry entry for the base.
- `internal/paths`: `ManifestPath`, `IdentitiesRoot`, `Config`.
- The fixture helpers in `internal/resolve/resolve_test.go` (`write`, `fixture`).

## What it leaves for later features

- `TestResolve_SecondFolderWithAClientsName`, green, named in `CLAUDE.md`. Feature 013 relies on it: L-09 can
  only report zero leaks when the resolver refuses.
- If a fix was needed: the binding itself. `workspace.Add` records it, so the sandbox of feature 002 gets bound
  clients with no code of its own, and every lobe hook inherits the refusal through `resolve.Resolve`.
- The rule for a client that has no recorded folder (an install from before this change), written down, so
  feature 002 and the tester setup know what a fresh client looks like on disk.

## How it works

1. **What the code does today (read, not run).** `Resolve(cwd, cfg)` calls `folder`, which finds the first
   registered base that contains `cwd` and takes the first path element below it as the client's name. `load`
   then calls `locate`, which accepts the folder when `<root>/.tentaqles.yaml` is a marker that names the folder
   (ignoring case on Windows) and `paths.ManifestPath(name)` is a regular file. `load` accepts the manifest
   when its `client` equals the folder's name. No step compares the folder with the one the client was created
   in. Read that way, `two/acme` with a copied marker returns a workspace named `acme` with `Root` `two/acme`.
2. **What that would mean.** A terminal in `two/acme` carries acme's identity (`tentaqles env`), `tentaqles run .`
   runs as acme, the guard judges commands there as acme's, `ListWorkspaces` returns two workspaces named
   `acme`, `SyncGit` writes a git include rule for both roots, `Find` returns whichever sorts first, and
   `markerFolder` in `internal/cmd/drop.go` picks the first base in registry order. Later, a lobe hook would
   read and write acme's memory from there (M-03 attack L-09).
3. **How the case arises.** A base is registered when any client is created under it (`workspace.Add` calls
   `AddBase`). The copy arrives when a client's folder is cloned or copied under that other base with its
   marker inside, and keeps its name. `tentaqles reach` is not involved, so its duplicate check never runs.
4. **The test settles it** (build task 2). The fixture is two bases in one `registry.Config`, client `acme`
   made under the first, and under the second a folder `acme` whose only content is a copy of the marker.
5. **Outcome A: the resolver already refuses.** The reading in step 1 was wrong. Write down why (which check
   refused). The test stays, `CLAUDE.md` names it, and tasks 3 to 6 are skipped.
6. **Outcome B: it resolves.** Nothing is coded yet. Task 3 writes the plan and the owner picks one option:

   | Option | Where the folder is recorded | For | Against |
   |---|---|---|---|
   | A. Registry | `$TQ_HOME/config.yaml` gains a map from client name to its root, beside `bases` | The resolver already holds the registry, so no extra read. One place lists every client | `tentaqles drop` deletes the identity folder whole and would need new code to remove the entry. A lost or hand-edited `config.yaml` unbinds every client at once |
   | B. Manifest | A `root` key in `identities/<client>/manifest.yaml` | `load` already reads the manifest. `drop` removes it with the client. `reach.Edit` keeps keys it does not own | The manifest is "names only": a path is not a secret, but it is not a name and it is true for one machine only. Every manifest byte passes the secret scan (`manifest.Parse`), so a path that looks like a token would fail the load, and the guard denies every shell command for a manifest that fails to load |
   | C. Own file | A small file beside the manifest, for example `identities/<client>/root` | The manifest stays names only. `drop` removes it with the client and needs no new code | One more small read on every resolve, which the hook's latency tests must still pass |
   | D. No record | Nothing is stored. A name that two registered bases both hold is refused, or the first base wins | No migration | It does not bind a client to the folder it was created in, which is what core page section 8 asks. "Both refused" lets a copied folder switch the real client off. "First wins" follows the order bases were registered, not where the client was made. Listed for completeness |

   Manifests hold names only and never a secret, whichever option is picked. The plan must also answer, for
   the option picked:

   - **A client with no record.** Clients made before this change have none. Either a person-run command
     writes the record, or the resolver treats "no record" in a stated way until one exists. A hook never
     writes it: the guard's hook reads and decides, nothing more.
   - **A moved folder.** Today a person can move a client's folder to another registered base and it keeps
     working. After the change it is refused until the record is updated. The plan names the person-run step
     that does that, with a typed yes.
   - **What the refused folder is.** Proposed here: neutral, with a `Reason` that names the client and where
     it lives, as the existing case "marker names client X but the folder is Y" is neutral. The other choice
     is `Unreadable`, on which the guard denies every shell command there.
   - **How paths are compared.** Proposed: as `resolve.norm` compares today, through `registry.Normalize`
     and ignoring case on Windows.
   - **Every caller.** `env`, `run`, `add` and `remove` (`changeTarget`), `settings render`, `statusline`,
     `list`, `doctor`, `SyncGit`, `reach` in a folder that holds a marker (`reachShowExisting`), and
     `markerFolder` in `drop.go`, which looks for the marker itself and does not go through the resolver.
7. **Then the change** (tasks 4 and 5): `workspace.Add` records the folder, `resolve.load` refuses a folder
   that is not the recorded one, and `ListWorkspaces` reports the refused folder among its errors, as it does
   for any folder whose marker resolves to nothing.

## Where to read

| What | Where |
|---|---|
| Requirements | [core page](../prd/vertical-lobe.md), section 8 ("Before phase 1: one fix outside the lobe") and section 10, decision D-12; [M-03](../prd/lobe-benchmarks.md), mechanism step 6, attack L-09, its test and the second risk |
| Source to port | None. This is tentaqles' own identity code |
| Existing tentaqles code | `internal/resolve/resolve.go` (`folder` L60, `locate` L101, `load` L125, `ListWorkspaces` L142, `Find` L176); `internal/resolve/resolve_test.go` (`fixture` L24, `TestResolve_MarkerOrManifestOfAnotherClient` L71); `internal/registry/registry.go` (`Config` L15, `Normalize` L56, `AddBase` L99); `internal/manifest/marker.go` (`MarkerBytes` L42, `Peek` L54); `internal/manifest/manifest.go` (`Parse` L108, the secret scan L109); `internal/workspace/scaffold.go` (`Add` L44, the two "already exists" checks L80 to L85); `internal/workspace/syncgit.go`; `internal/cmd/reach.go` (the duplicate check L83, `reachShowExisting` L246); `internal/cmd/reach_test.go` (`TestReach_SameNameElsewhereRefused` L215); `internal/cmd/drop.go` (`markerFolder` L129); `internal/cmd/run.go` (`cwdWorkspace` L263, `findWorkspace` L273); `internal/cmd/env.go`; `internal/reach/edit.go` (`Edit` L105) |
| Repository rules | `CLAUDE.md`, "Must not trade off" and "Rules" |

## Build tasks

1. **Check this brief against the code as it stands.** Read the files in the table above and list what no
   longer fits (a moved function, a new caller of the resolver). The owner approves changes to this file.
2. **Write the test that settles the claim.** Add `TestResolve_SecondFolderWithAClientsName` to
   `internal/resolve/resolve_test.go`, built on `fixture`. Run it on Windows and on Linux or macOS. Do not
   change `resolve.go` in this task. Write the outcome (each assertion, each system) at the end of this file
   under a new heading "Result of the first test". Proof: the test's own output.
3. **Only if it resolved: write the plan.** One page: the option table above filled in with what the code
   review of every caller found, an answer to each of the five points of step 6, and the list of files to
   change. Stop. The owner picks an option and approves the plan before any code is written.
4. **Only if it resolved: record the folder.** Change `internal/workspace/scaffold.go` (and
   `internal/registry` or `internal/manifest` or `internal/paths`, by the option picked) so `Add` writes the
   record. Proof: `TestAdd_BindsTheClientToItsFolder`, and every test in `internal/workspace` and
   `internal/cmd` unedited and green.
5. **Only if it resolved: refuse in the resolver.** Change `internal/resolve/resolve.go`. Add the person-run
   step for a client with no record and for a moved folder, as the plan says. Proof: the test of task 2 turns
   green, plus `TestResolve_ClientWithNoRecordedFolder`, `TestEnv_SecondFolderGetsNoIdentity` and
   `TestRunDot_SecondFolderHasNoIdentity`. Tasks 4 and 5 touch different packages and can run side by side once the
   plan fixes the shape of the record.
6. **Only if it resolved: the callers.** Fix what task 3 found in `reach` (a folder with a copied marker is not
   "already a client") and in `drop.go`'s `markerFolder`. Each gets a test of its own. No `TestReach_*` or
   `TestDrop_*` test is edited.
7. **Docs.** `CLAUDE.md`: add the new test name to the line "A copied marker or manifest lends a folder
   nothing". `CHANGELOG.md` under `[Unreleased]`. If a fix was made: `docs/how-it-works.md`,
   `docs/threat-model.md` and, when a manifest or registry key was added, `docs/reference/manifest.md`. If a
   command gained a step, its section in `docs/reference/commands.md`.
8. **Tester run.** Hand the built program and the Evaluation section to a fresh tester agent.

## Tests that stay in the repository

Test names here are proposed by this file, except where a requirements page is named.

- `TestResolve_SecondFolderWithAClientsName` (`internal/resolve`). Two bases, `one` and `two`; client `acme`
  under `one`; `two/acme` holds only a copy of the marker. It asserts, for both orders of the two bases in the
  registry: `one/acme` resolves to `acme` with `Root` `one/acme` (the control); `two/acme` and
  `two/acme/app/src` give no workspace; `ListWorkspaces` returns exactly 1 workspace named `acme`, with `Root`
  under `one`; `Find(cfg, "acme")` returns that one. On Windows it adds `two/Acme`, because the marker's name
  is compared without case there. 5 assertions, 2 registry orders.
- `TestResolve_MarkerOrManifestOfAnotherClient`, `TestResolve_FolderAndSubfolders`, `TestResolve_Neutral`,
  `TestResolve_Unreadable`, `TestListAndFind`: unedited and green.
- Only if a fix was made:
  - `TestAdd_BindsTheClientToItsFolder` (`internal/workspace`): after `Add`, the record names the folder, and
    the manifest still loads and holds no path unless option B was picked.
  - `TestResolve_ClientWithNoRecordedFolder`: the rule the plan chose for a client from before the change.
  - `TestEnv_SecondFolderGetsNoIdentity` (`internal/cmd`): `tentaqles env --json` in the second folder names
    no workspace and sets none of acme's variables.
  - `TestRunDot_SecondFolderHasNoIdentity` (`internal/cmd`): `tentaqles run . -- <command>` in the second
    folder starts the command with none of acme's environment, as it does in any neutral folder.
- House rules. Every test named under "Must not trade off" in `CLAUDE.md` passes unedited. The nearest ones:
  `TestReach_ConfirmsBeforeWriting`, `TestReach_GuardsTheNewClient`, `TestEdit_NoChangeIsTheSameBytes`,
  `TestDrop_UndoesTheClient`, `TestDrop_Refusals`, `TestRunDotResolvesCwd`, `TestEnv_ScopeLayerRestore`,
  `TestCanonicalSuite`. Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`.

## Evaluation

The sandbox does not exist yet (it is feature 002), so this brief builds its home by hand, with the same files
`workspace.Add` leaves on disk. It runs no person-only command. Use bash (Git Bash on Windows), in an empty
working folder, with the built `tentaqles` first on `PATH`. No network.

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home" "$W/one/acme/app" "$W/two/acme/app" "$W/two/beta"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset CLAUDE_CONFIG_DIR CODEX_HOME TQ_WS TQ_WS_ROOT __TQ_STATE TQ_ENABLED
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM XDG_CONFIG_HOME
mkdir -p "$TQ_HOME/identities/acme" "$TQ_HOME/identities/beta"
printf 'bases:\n  - %s\n  - %s\n' "$W/one" "$W/two" > "$TQ_HOME/config.yaml"
for c in acme beta; do
  printf 'schema: tentaqles-client-v2\nclient: %s\ngit: { email: dev@%s.test }\nidentities: { claude: {} }\n' \
    "$c" "$c" > "$TQ_HOME/identities/$c/manifest.yaml"
done
printf 'schema: tentaqles-marker-v1\nclient: acme\n' > "$W/one/acme/.tentaqles.yaml"
printf 'schema: tentaqles-marker-v1\nclient: beta\n' > "$W/two/beta/.tentaqles.yaml"
cp "$W/one/acme/.tentaqles.yaml" "$W/two/acme/.tentaqles.yaml"
printf 'clients:
  acme: %s
  beta: %s
' "$W/one/acme" "$W/two/beta" >> "$TQ_HOME/config.yaml"
T=/full/path/to/the/built/tentaqles   # see the note below
```

`beta` is a real client under `two`; it is why `two` is a registered base. The `clients:` lines are the record
`tentaqles reach` writes for a new client. Call the built program by its full path, `$T`, and read every
`tentaqles` in the checks below as `$T`: in an activated shell `tentaqles` is a shell function that runs the
installed program, whatever `PATH` says. `E` stands for `$T env --shell bash --json`.

1. `cd "$W/one/acme/app"; $E | grep -c '"workspace":"acme"'` prints `1`. This is the control: the real client
   resolves from a subfolder.
2. `cd "$W/two/beta"; $E | grep -c '"workspace":"beta"'` prints `1`: the second base is registered and works.
3. `cd "$W/two/acme"; $E | grep -c '"workspace":""'` prints `1`, and the same command with
   `'"workspace":"acme"'` prints `0`.
4. `cd "$W/two/acme/app"; $E | grep -c '"workspace":"acme"'` prints `0`: a subfolder of the copy is no client.
5. `cd "$W"; tentaqles list --json | grep -o '"Name":"acme"' | wc -l` prints `1`, and the `Root` beside it
   ends in `one/acme` (on Windows `one\\acme`), not in `two`.
6. `cd "$W/one/acme"; $E | grep -c 'identities.*acme.*claude'` prints `1`: the environment to set names
   acme's Claude config folder. `cd "$W/two/acme"; $E | grep -c 'identities.*acme.*claude'` prints `0`.
7. The real client's guard still answers:
   `printf '{"cwd":"%s","tool_name":"Bash","tool_input":{"command":"ls"}}' "$W/one/acme" |`
   `tentaqles claude-hook pre-tool-use; echo "exit=$?"` prints `exit=0`.
8. Nothing was written into the copy: `ls -A "$W/two/acme"` lists `.tentaqles.yaml` and `app` only.
9. The copy is blocked: the hook input of check 7 with `cwd` `$W/two/acme` prints a line that starts with
   `BLOCKED` and names `$W/one/acme`, and `exit=2`.

**Person checks**

- Only if a fix was made. On their own machine, after upgrading, the owner opens a terminal in one existing
  client's folder and sees the same identity as before (`tentaqles doctor` shows no new error). This is the
  check on clients that were created before the change; a tester has no such client.
- Only if the plan added a person-run step for a moved folder: the owner runs it once on a throwaway client.

PASS needs 9 of 9. "Could not run" is a FAIL.

## Done when

- [ ] `TestResolve_SecondFolderWithAClientsName` is in the repository and green in CI on the three systems
  (`go vet ./...`, `go test ./... -count=1`, `go build ./...`).
- [ ] The outcome of the first test is written in this file.
- [ ] If a fix was needed: the owner picked the option and approved the plan before code was written, and the
  tests of tasks 4 to 6 are green.
- [ ] Every "Must not trade off" test passes unedited; `git diff` shows no change to an existing test's
  assertions.
- [ ] The tester brief passes, 9 of 9.
- [ ] `CLAUDE.md` names the new test; `CHANGELOG.md` has its line; the docs of task 7 are updated.
- [ ] The owner has seen the two `tentaqles env` outputs under Result.

## Risks and unknowns

- **The claim itself is "read, not run".** M-03 says the resolver keys a client by folder name
  (`internal/resolve/resolve.go` L125 to L137) and that only `reach` refuses a second folder with the name.
  Task 2 exists to replace that reading with a result. This file describes both outcomes for that reason.
- **Existing installs.** The owner's own clients have no recorded folder. A fix that refuses a client with no
  record would switch off every existing client on upgrade, and the guard would stop judging commands there.
  The plan's rule for "no record" decides this; it needs the owner's yes.
- **Moving a client.** Binding makes a moved folder a refused folder. If the plan has no step for it, a person
  would have to drop and recreate the client and lose its logins.
- **Neutral or unreadable.** In a neutral folder the guard has no client to compare against. In an unreadable
  one it denies. The proposal here is neutral, to match the existing marker case; the owner may prefer deny.
- **Option B and the secret scan.** A folder path inside the manifest is scanned like every other byte. A
  path that matches the scan would make the manifest fail to load.
- **The Evaluation was written from the code, not run.** The hand-made home and the outputs of
  `tentaqles env --json` and `tentaqles list --json` are read from `internal/cmd/env.go` and `list.go`.
  Task 1 runs checks 1, 2 and 5 against today's program and corrects the brief if an output differs.
- **The Evaluation fixture depends on the option.** The line marked `BINDING` cannot be written until the
  owner has picked. If outcome A holds, the line is removed and the brief runs as it is.
- **What would force a change to this file:** a caller of the resolver that task 1 finds and this file does
  not list; an outcome of task 2 that differs between Windows and the other systems.

## Result of the first test

Run on 2026-11-09 on Windows 11 (the dev box), against `internal/resolve/resolve.go` unchanged:
`go test ./internal/resolve -run "TestResolve_SecondFolderWithAClientsName" -count=1 -v`. The test fails. This
is outcome B: the second folder resolves to the client. The claim that was read from the code is now a result.

| # | Assertion | Bases in order `one`, `two` | Bases in order `two`, `one` |
|---|---|---|---|
| 1 | The real folder and its subfolder resolve to `acme` (the control) | holds | holds |
| 2 | `two/acme` gives no workspace | fails: it resolves to `acme` | fails: it resolves to `acme` |
| 3 | `two/acme/app/src` gives no workspace | fails: it resolves to `acme` | fails: it resolves to `acme` |
| 4 | `ListWorkspaces` returns one `acme`, under `one` | fails: two workspaces named `acme` | fails: two workspaces named `acme` |
| 5 | `Find("acme")` returns the one under `one` | holds, by registry order only | fails: it returns the copy under `two` |
| 6 | Windows only: `two/Acme` gives no workspace | fails: it resolves to `acme` | fails: it resolves to `acme` |

The five other tests of the package pass unedited. The test was not run on Linux or macOS: the dev box has
neither, so that run waits for CI. Nothing in the code path differs by system except the comparison of names
without case, which assertion 6 covers on Windows only.

What it means: a copy of a client's folder under another registered clients folder carries that client's
identity, `tentaqles list` shows the client twice, and which folder `Find` returns depends on the order the
clients folders were registered. Tasks 3 to 6 apply. No code changes until the owner has picked an option.

## The plan (task 3)

The owner picked on 2026-11-09, after seeing the test result:

| Point | The owner's pick |
|---|---|
| Where the folder is recorded | Option A, the registry: `$TQ_HOME/config.yaml` gains a map from client name to its folder, beside `bases` |
| A client with no record | It keeps working as today. `tentaqles doctor` flags it, and `tentaqles settings render` writes the record when exactly one folder claims the name. When two folders claim a name that has no record, both are refused until a person binds one |
| What the refused folder is | Blocked: `Unreadable`, so the guard denies shell commands there, with a reason that names the client and where it lives |
| A moved folder | A person-only command, run in the client's new folder, with a typed yes. It also binds an old client by hand |

Decided by the author, for the owner to overturn:

- **The command is `tentaqles bind`**, with no arguments, run inside the client's folder. It refuses inside an
  agent session and takes no `--yes` flag.
- **Paths are compared as the resolver compares today**: through `registry.Normalize`, ignoring case on Windows.
- **`tentaqles reach` in a folder that holds a copied marker** no longer says "already a client". It names the
  client's real folder and the two ways out: delete the copied marker, or run `tentaqles bind` if the client
  moved. What `reach` asks when it creates or changes a client is untouched.

What each caller does in the second folder after the change:

| Caller | Today | After |
|---|---|---|
| `tentaqles env`, a terminal's activation | acme's identity | no identity; the reason names acme's folder |
| `tentaqles run .` | runs as acme | as in any folder that is unreadable today |
| The guard hook (`claude-hook`, `codex-hook`) | judges commands as acme's | denies shell commands, as for any unreadable folder |
| `tentaqles list`, `tentaqles doctor` | acme twice | acme once; the copy is reported as an error with its reason |
| `SyncGit` (the git include rules) | a rule for both folders | a rule for the real folder only |
| `tentaqles settings render` | renders acme twice | renders acme once, and records the folder of a client that has no record |
| `tentaqles drop acme` | may pick the copy's folder by registry order | uses the recorded folder and removes the record |
| `tentaqles reach` in the copy | "This folder is already a client" | refuses, naming the real folder |

Files to change:

- `internal/registry/registry.go`: `Config.Clients`, `Folder`, `Bind`, `Unbind`.
- `internal/workspace/scaffold.go`: `Add` binds the new client.
- `internal/resolve/resolve.go`: `load` checks the record; the rule for no record; `Claim` for `bind`.
- `internal/cmd/bind.go` (new) and `internal/cmd/root.go`: the `bind` command.
- `internal/cmd/settings.go`: record an unbound client. `internal/doctor/doctor.go`: the finding.
- `internal/cmd/drop.go`: the recorded folder first, and the record removed. `internal/cmd/reach.go`: the
  copied marker.
- Docs: `docs/reference/commands.md` (`bind`), `docs/reference/manifest.md` (the registry key),
  `docs/how-it-works.md`, `docs/threat-model.md`, `CHANGELOG.md`, `CLAUDE.md`.

The Evaluation's `BINDING` line becomes: append to `$TQ_HOME/config.yaml` the lines
`clients:`, `  acme: <W>/one/acme` and `  beta: <W>/two/beta`. Checks 3 and 4 then expect no workspace as
written, and the guard check gains one line: the same hook input with `cwd` in the copy is denied.

## Result of the tester run

Run on 2026-11-09 on Windows 11 in Git Bash, against a program built from the working tree
(`CGO_ENABLED=0 go build ./cmd/tentaqles`), in a throwaway home. 9 of 9 pass.

| # | Check | Result |
|---|---|---|
| 1 | The real client resolves from a subfolder | pass |
| 2 | A real client under the second clients folder resolves | pass |
| 3 | The copy names no workspace | pass; the reason names the client's real folder |
| 4 | A subfolder of the copy is no client | pass |
| 5 | `list` shows `acme` once, under `one` | pass; the copy is listed under `errors` |
| 6 | Only the real folder gets acme's Claude config folder | pass |
| 7 | The guard still answers in the real folder | pass, `exit=0` |
| 8 | Nothing was written into the copy | pass |
| 9 | The guard denies in the copy | pass, `BLOCKED` and `exit=2` |

Also seen: `doctor --json` reports one `client-folder-refused` finding for the copy.

The same nine checks were first run, by mistake, against the installed program (0.14.x), because `tentaqles`
is a shell function in an activated shell. That run is the "before": checks 3, 4, 5, 6 and 9 failed there, and
the copy got acme's identity. The brief now says to call the built program by its full path.

Left open:

- Not run on Linux or macOS. CI does that once the branch is pushed.
- Person check, `tentaqles bind` on a throwaway client: done by the owner on 2026-11-09 with the development
  build (`tqx` 0.15.0-lobe.1) in a throwaway home. In the copy, `bind` showed both folders and waited for
  `yes`; afterwards `list` showed acme at the new folder and the old folder was refused with its reason.
- Person check, an existing client after an upgrade: partly done. The development build lists the owner's five
  real clients, each once, read-only. Opening a real client's terminal with the new build waits for an install
  that replaces the released one, which the owner has not asked for.
- The guard's deny line in the copy starts with "the client manifest of this folder cannot be read" and then
  gives the real reason in brackets. The wording comes from the guard's existing text for an unreadable folder
  and was left as it is.
