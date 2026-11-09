# 002: Sandbox with two test clients

**Status** not started · **Stage** Ground · **Entries** F-07 (the sandbox and the standard tester setup),
F-01 (paths), decision A-20 · **Needs** 001 · **Feeds** 003 and every later tester brief; 012, 013, 021

## Result

One command builds a throwaway home with the two standard test clients, `acme` and `globex`, so a tester agent,
a benchmark and a CI job get clients without pretending to be a person at `tentaqles reach`. It refuses any
folder that the three home variables do not already point into, so it can never touch the real home.

```bash
W="$(pwd -W 2>/dev/null || pwd)"; mkdir -p "$W/home"
export HOME="$W/home" USERPROFILE="$W/home" TQ_HOME="$W/home/.tentaqles" TQ_NO_BANNER=1
unset GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM
tentaqles lobe sandbox "$W"            # prints: ok acme globex
tentaqles list                         # acme and globex, under $W/clients
tentaqles lobe sandbox "$W"            # prints a line that starts with SETUP FAIL, exit 1
```

## Scope

**In**

- The `lobe` command group, with `sandbox` as its first and only command.
- `tentaqles lobe sandbox <folder>`: the refusals, the two clients, the `lobe` mapping with `model: off` at
  the end of each manifest, the Codex agent for `acme` only.
- The builder behind the command as one function, so later bench checks call the same code.
- `paths.LobeDir(client)`, and the provider loader's refusal of a provider whose id is `lobe`.
- The standard tester setup as a tested text: the block of F-07 with its `lobeset` helper.
- The command's section in `docs/reference/commands.md`, and `lobe` in the docs test's `documentedGroups`.

**Out**

- The store: `store.Open`, `store.json`, locks, the layout (feature 003). The sandbox creates no `lobe`
  folder. The "two stores" that F-07's `TestLobeSandbox_BuildsTheStandardPair` counts join that test in 003,
  when the render step first opens a store.
- The `lobe-hook` command and the plugin's lobe entries (feature 003).
- `tentaqles lobe status`, `events`, `settings`, `set`, the scope and login rules of F-07 step 3, and the
  doctor findings (feature 005). F-07's tester checks 1, 2, 3 and 6 go with them.
- Reading the `lobe` key: the manifest's `Lobe` field and `internal/lobe/settings` (feature 005, F-06). Until
  then the key is ignored on load, which is what today's manifest parser does with a key it does not know.
- The root `LICENSE`, `NOTICE`, `third_party/licenses/` and `tentaqles version --notices` (feature 003, with
  the first ported code, decision D-13). F-07's tester check 4 goes with them.
- `tentaqles lobe bench` (features [012](012-hook-latency-and-stall.md) and
  [013](013-isolation-leak-suite.md)). F-07's tester check 7 goes with them.
- `internal/lobe/cli`. The sandbox calls `internal/workspace`, which a lobe package may not import, so it
  lives in `internal/cmd`. The first command anyone may run from a lobe package creates `cli` (003 or 005).
- Any change to `reach`, `add`, `drop` or their tests.

## What it builds on

- Feature [001](001-resolver-second-folder.md): the resolver's refusal. If 001 made `workspace.Add` record
  the client's folder, the sandbox's clients are bound because the sandbox calls `Add`.
- Existing tentaqles code, used as it is: `reach.Answers`, `(*Answers).AddOptions` and `reach.GitNameFor`
  (`internal/reach/reach.go`); `workspace.Add` (`internal/workspace/scaffold.go`); `renderIdentityFiles`
  and `renderCodex` (`internal/cmd/settings.go`); `gitcfg.RunGit` and `gitcfg.EnsureGlobal`;
  `registry.Normalize`; `writeFileAtomic` (`internal/cmd/reach.go`); `manifest.Load`; `NewRoot`
  (`internal/cmd/root.go`); the test helpers `isolateHome` and `unsetenv` (`internal/cmd/run_test.go`).

## What it leaves for later features

| Name | What it is | Used by |
|---|---|---|
| `tentaqles lobe` | The command group, in `internal/cmd/lobe.go` (`newLobeCmd`, added in `NewRoot`) | Every feature that adds a `tentaqles lobe` command |
| `tentaqles lobe sandbox <folder>` | The command, in `internal/cmd/lobe_sandbox.go` | Every tester brief from 003 on; `lobebench` (021) runs it as a command |
| `sandboxBuild(folder string) error` | The builder behind the command, with the refusals inside. It reads the process's own environment | 012 and 013 receive it as a function value, because a lobe package may not import `internal/cmd` |
| `paths.LobeDir(client)` | `$TQ_HOME/identities/<client>/lobe`, in `internal/paths/paths.go` | 003 (`store.Open`) and every feature after it |
| The reserved provider id `lobe` | A provider file with that id is refused, because `paths.IdentityDir(ws, "lobe")` is the store's path | 003 |
| The standard tester setup | The bash block under Evaluation, with `lobeset` | Every Evaluation section from 003 on copies it |
| The sandbox manifest's shape | `lobe:` is the last top-level key; its entries are indented two spaces; it holds `model: off` | `lobeset`; F-06's reader and `tentaqles lobe set` (005); `bench/internal/arm` (021) |
| `TestLobeCommandTable` | The list of commands under `lobe`, checked against F-07 step 1 | Every feature that adds a command adds its row |

## How it works

1. **The group.** `internal/cmd/lobe.go` defines `tentaqles lobe` with a short help text and no action of its
   own. `NewRoot` adds it. `test/integration/docs_test.go` gains `"lobe": true` in `documentedGroups`, so
   the reference documents `tentaqles lobe sandbox` in a section of its own (F-07 step 4).
2. **The path.** `paths.LobeDir(client)` returns `filepath.Join(IdentitiesRoot(), client, "lobe")` (F-01 step
   1). It is the same path as `IdentityDir(client, "lobe")`, so the provider loader refuses the id `lobe`
   (F-01, "How to build it"). Nothing creates the folder in this feature.
3. **Not person-only.** The command runs inside an agent session (`CLAUDECODE` set). That is its purpose
   (decision A-20).
4. **Refusals, all checked before the first write.** `<folder>` is made absolute and normalized with
   `registry.Normalize`. The command refuses unless every one of these holds (F-07 step 8):
   - `TQ_HOME`, `HOME` and `USERPROFILE` are all set, on every system, and each is a path inside `<folder>`
     after the same normalization (compared without case on Windows, on a path boundary). A variable that
     is unset, empty, equal to `<folder>` or outside it refuses (strictly inside is this file's reading).
   - `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` are unset.
   - `$TQ_HOME` is absent or an empty folder, and `<folder>/clients` is absent or an empty folder.

   This is how "a folder inside the real home" is refused: a person's terminal has `HOME` pointing at the
   real home, which is never inside the folder they name. A home that already holds a client is refused by
   the third rule. On a refusal nothing is created, not even `<folder>`.
5. **Building, for `acme` then `globex`.** The steps `reach` runs after its typed yes, with fixed answers:
   - Answers: `reach.Answers{Email: "dev@<name>.test"}`; `acme` also gets `Agent: ["codex"]`, which is what
     `tentaqles add agent codex` gives a client. Every other question is skipped, so the language is the
     default.
   - `AddOptions(<folder>/clients, <name>, reach.GitNameFor("", email))`, with `RunGit` set to the real git
     runner, so `workspace.Add` wires `$HOME/.gitconfig` to tentaqles' include file. That is why the two git
     variables must be unset and `HOME` must be inside the folder.
   - `workspace.Add`, then `renderIdentityFiles`: Claude settings, the PATH mask, and for `acme` the Codex
     config with tentaqles' hooks. The render's output is captured, not printed; a render warning is a
     failure.
   - No plugin install and no shell profile block: `reachGuardPlugin` and `reachHooks` are not called.
   - The manifest then gets `lobe:` and `  model: off` appended as its last two lines, written with
     `writeFileAtomic` at mode 0600, and is loaded once with `manifest.Load` to prove it still parses.
6. **The answer.** `ok acme globex` on stdout and exit 0. On any failure, one line on stdout that starts with
   `SETUP FAIL` and names the rule or step, and exit 1. A failure after the checks passed can leave a partial
   home; the next run then refuses it by the third rule, and the tester starts again in a fresh folder.
7. **The setup block.** The standard tester setup (F-07, tester brief) is bash: it points the three home
   variables into the working folder, clears the variables a tentaqles shell may carry, runs the sandbox,
   makes an empty repository `app` in each client, and defines `lobeset`. `lobeset <client> <key> <value>`
   rewrites one key under `lobe` in a sandbox manifest with `awk`. It drops any line under `lobe:` that starts
   with that key and prints the new line at the end of the file, which is only right while `lobe` is the last
   top-level key. It stands in for `tentaqles lobe set`, which answers only to a person.

## Where to read

| What | Where |
|---|---|
| Requirements | [F-07](../prd/lobe-foundation.md): mechanism steps 2, 4 and 8, the row `tentaqles lobe sandbox`, "How to build it" (the `lobe_sandbox.go` bullet), the two `TestLobeSandbox_*` tests, the whole standard tester setup and tester check 5. [F-01](../prd/lobe-foundation.md): step 1 (first sentence) and the `IdentityDir(ws, "lobe")` bullet. [F-06](../prd/lobe-foundation.md): the bullet on `lobeset` and the constraint row on unknown manifest keys. [Core page](../prd/vertical-lobe.md): 5.1 (the fourth import rule), section 9 (rules for a tester agent), decision A-20. [M-02](../prd/lobe-benchmarks.md), "How to build it": who calls the builder |
| Source to port | None. The sandbox is tentaqles' own design |
| Existing tentaqles code | `internal/cmd/reach.go` L83 to L110 (what `reach` does after the yes) and L336 (`writeFileAtomic`); `internal/reach/reach.go` (`Answers` L36, `AddOptions` L111, `GitNameFor` L163); `internal/workspace/scaffold.go` (`Add` L44); `internal/cmd/settings.go` (`renderIdentityFiles` L125, `renderCodex` L149, `codexHookProgram` L188); `internal/gitcfg/gitcfg.go` (`IncludeFile` L27, `EnsureGlobal` L89); `internal/paths/paths.go` L28; `internal/providers/load.go` (`parseProvider` L54); `internal/cmd/root.go` L16; `internal/cmd/run.go` L283 (`insideAgent`); `internal/cmd/claudehook_main.go` L43 (how a failed command is reported); `test/integration/docs_test.go` L16; `internal/cmd/run_test.go` L21 and `internal/cmd/reach_test.go` L22 (how tests isolate the home) |

## Build tasks

1. **Check this brief against the code as it stands.** In particular: what feature 001 changed in
   `workspace.Add`; that a manifest with an unknown top-level key still loads; that `codex` is a value of
   `reach.Values(reach.CatAgent)`. List what no longer fits; the owner approves changes to this file.
2. **The group, the path and the reserved id.** Create `internal/cmd/lobe.go`; change `internal/cmd/root.go`,
   `internal/paths/paths.go`, `internal/providers/load.go` (or `provider.go`, where ids are validated) and
   `test/integration/docs_test.go`. Proof: `TestLobeDirIsUnderTheIdentity`, `TestProviderIDLobeIsReserved`.
3. **The refusals.** Create `internal/cmd/lobe_sandbox.go` with `sandboxBuild` up to the end of the checks,
   and a seam for the git runner as `reach` has (`reachRunGit`). Proof: `TestLobeSandbox_RefusesARealHome`.
   Can run beside task 2.
4. **The build.** Finish `sandboxBuild` and the command. Proof: `TestLobeSandbox_BuildsTheStandardPair`,
   `TestLobeSandbox_LobeIsTheLastKey`, `TestLobeSandbox_WorksInsideAnAgent`, `TestLobeCommandTable`.
5. **Docs.** A `## tentaqles lobe sandbox` section in `docs/reference/commands.md` (what it builds, the
   refusals, that it is for testers and benchmarks); `CHANGELOG.md` under `[Unreleased]`; the `lobe` group
   and the two new files in the layout list of `CLAUDE.md`. No plugin file changes, so `plugin/CHANGELOG.md`
   gets no line. Proof: `TestDocsReferenceEveryCommand`.
6. **Tester run.** Hand the built program and the Evaluation section to a fresh tester agent.

## Tests that stay in the repository

- `TestLobeSandbox_BuildsTheStandardPair` (F-07; `internal/cmd`): after one run there are 2 manifests under
  `$TQ_HOME/identities/`, both load, both end with `lobe:` and `  model: off`, both clients resolve from
  `<folder>/clients/<name>`, a Codex config exists for `acme` and none for `globex`, and no plugin install
  and no shell profile write happened. The stores F-07 also counts join in feature 003.
- `TestLobeSandbox_RefusesARealHome` (F-07): a home that holds a client, a `HOME` outside the folder and a set
  `GIT_CONFIG_GLOBAL` are each refused with 0 bytes written and no folder created. 3 of 3. This file adds a
  fourth and fifth case to the same test: `USERPROFILE` unset, and a `<folder>/clients` that is not empty.
- `TestLobeSandbox_LobeIsTheLastKey` (proposed here): in both manifests `lobe` is the last top-level key, and
  the manifest still loads after the line `  enforce: block` is appended to the file, which is what `lobeset`
  does.
- `TestLobeSandbox_WorksInsideAnAgent` (proposed here): with `CLAUDECODE=1` the command builds the pair.
- `TestLobeDirIsUnderTheIdentity` (proposed here): `paths.LobeDir("acme")` is
  `$TQ_HOME/identities/acme/lobe`.
- `TestProviderIDLobeIsReserved` (proposed here): a user provider file named `lobe.yaml` is refused.
- `TestLobeCommandTable` (F-07), in its first form: the commands registered under `lobe` are exactly the rows
  of F-07 step 1 built so far (one, `sandbox`), and the ones that refuse under `CLAUDECODE=1` are exactly the
  rows marked "yes" (none yet). Each later feature adds its rows.
- `TestDocsReferenceEveryCommand`: 0 commands without a section and 0 sections without a command.
- House rules. Every `TestReach_*`, `TestDrop_*` and `TestChange_*` test passes unedited. No lobe package
  exists yet, so the import tests arrive with 003. The sandbox tests set all three home variables and
  never run git against the developer's real configuration.

## Evaluation

A tester agent runs this in an empty working folder, with the built `tentaqles` first on `PATH` and `git` on
`PATH`. Bash (Git Bash on Windows). No network. The tester is an agent and stays one: it never clears
`CLAUDECODE`. First, the standard tester setup, exactly as every later brief uses it:

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

`I` is `$TQ_HOME/identities`. Define `snap(){ (cd "$W" && find . -type f -exec cksum {} + | sort -k3); }`:
it prints a checksum of every file under the working folder.

1. The setup printed `ok acme globex` and no line that starts with `SETUP FAIL`. Both
   `$I/acme/manifest.yaml` and `$I/globex/manifest.yaml` exist.
2. `git config --global --show-origin --get-all include.path` names a file under `$W/home`.
3. `tentaqles list --json | grep -o -E '"Name":"(acme|globex)"' | sort -u | wc -l` prints `2`, and the email
   beside each is `dev@acme.test` and `dev@globex.test`.
4. `cd "$W/clients/acme/app"; tentaqles env --shell bash --json | grep -c '"workspace":"acme"'` prints `1`.
   The same in `$W/clients/globex/app` with `globex` prints `1`. In `$W` it prints `0` for both names.
5. `tail -n 2 "$I/acme/manifest.yaml"` prints `lobe:` and `  model: off`. The same for `globex`.
6. `test -f "$I/acme/codex/config.toml"; echo $?` prints `0`, and
   `test -e "$I/globex/codex"; echo $?` prints `1`: `acme` has the Codex agent and `globex` does not.
7. `S1="$(snap)"; tentaqles lobe sandbox "$W"; echo "exit=$?"` prints a line that starts with `SETUP FAIL`
   and `exit=1`. Then `[ "$S1" = "$(snap)" ] && echo same` prints `same`: the home already holds clients, and
   the refused run changed no file.
8. `HOME="$W" tentaqles lobe sandbox "$W/x"; echo "exit=$?"` prints `exit=1`, and `test -e "$W/x"; echo $?`
   prints `1`: `HOME` is not inside the folder named.
9. `(export HOME="$W/y/home" USERPROFILE="$W/y/home" TQ_HOME="$W/y/home/.tentaqles" GIT_CONFIG_GLOBAL="$W/g";`
   `tentaqles lobe sandbox "$W/y"; echo "exit=$?")` prints `exit=1`, and `test -e "$W/y"` is false.
10. `(export HOME="$W/z/home" TQ_HOME="$W/z/home/.tentaqles"; unset USERPROFILE;`
    `tentaqles lobe sandbox "$W/z"; echo "exit=$?")` prints `exit=1`, and `test -e "$W/z"` is false.
11. `lobeset acme enforce block; lobeset acme enforce advice; grep -c '^  enforce:' "$I/acme/manifest.yaml"`
    prints `1`, `tail -n 1 "$I/acme/manifest.yaml"` prints `  enforce: advice`, and check 4 still prints `1`
    for `acme`: the manifest still loads.
12. `tentaqles lobe --help | grep -c -E '^  sandbox '` prints `1`.

**Person checks**

- In a terminal of their own, with nothing exported, the owner runs `tentaqles lobe sandbox <a new folder>`.
  Expect a `SETUP FAIL` line, exit 1, no new folder, and `tentaqles list` showing the same clients as before.
  A tester cannot run this: it has no real home to protect.

PASS needs 12 of 12. "Could not run" is a FAIL.

## Done when

- [ ] Every test above is in the repository and green in CI (`go vet ./...`, `go test ./... -count=1`,
  `go build ./...`) on the three systems.
- [ ] The tester brief passes, 12 of 12, and the owner ran the person check.
- [ ] House rules: `TestReach_*`, `TestDrop_*` and every "Must not trade off" test pass unedited; the sandbox
  tests touch no real home.
- [ ] `docs/reference/commands.md`, `CHANGELOG.md` and the layout list in `CLAUDE.md` are updated.
- [ ] The owner has seen `ok acme globex` and the refusal under Result.

## Risks and unknowns

- **One line or two on a refusal.** F-07 says the command prints "one line that starts with `SETUP FAIL`".
  Any failed command also gets a line `tq: <error>` on stderr from `RunMain`, which sits in
  `internal/cmd/claudehook_main.go`, one of the guard's files. This brief leaves that file alone: one
  `SETUP FAIL` line on stdout, plus the usual stderr line. The setup block and the checks read stdout and
  the exit code only.
- **`XDG_CONFIG_HOME`.** Git can also read `$XDG_CONFIG_HOME/git/config` as global configuration. The setup
  block unsets the variable, but F-07's refusal list does not name it. Verify on the three systems that
  `git config --global` writes under `$HOME` when it is set, and tell the owner if the list needs it.
- **"Inside" the folder.** F-07 does not say whether a home variable equal to `<folder>` counts. This file
  reads it as strictly inside. Its own check 8 (`HOME="$W"` with the folder `$W/x`) passes either way.
- **A manifest key nobody reads yet.** The `lobe` mapping is plain text appended to the file until feature
  005 adds the reader. F-06 states that an unknown key is ignored on load (`internal/manifest/manifest.go`
  L113); task 1 confirms it, because the guard denies every shell command when a manifest fails to load.
- **The Codex hook program.** `renderCodex` writes the path of the installed `tentaqles` into `acme`'s Codex
  config and refuses a path with a space and no short form on Windows. A tester whose program sits in such a
  folder gets `SETUP FAIL`. The brief asks for the program on `PATH`; say so if a CI runner trips on it.
- **Agent option name.** `Agent: ["codex"]` assumes the catalog's id for the Codex agent is `codex`
  (`internal/cmd/settings.go` uses `codexScope = "codex"`). Task 1 confirms it.
- **What would force a change to this file:** feature 001 choosing a binding that a hand-appended manifest
  line could break (option B writes into the manifest too); a later feature needing a third client or a
  single-client builder (013's attack L-14 asks for a new `acme` after a drop, which this builder refuses by
  design; 013 raises it with the owner).
