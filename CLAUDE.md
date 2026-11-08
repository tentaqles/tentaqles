# tentaqles

An identity isolator for developers who work for several clients: one folder per client, and a terminal opened in
it, or in any subfolder, carries that client's identity and nothing else: its Claude Code config home (its own
subscription), its git name and email, its own login for each CLI, its browser profile, and a PATH without the CLIs
of providers it doesn't use. A small Claude Code hook blocks a git, gh or cloud command that would run under the
wrong identity.

## Reading path

`docs/IDEATION.md` is what it is for. Then `docs/threat-model.md` (what it does and does not protect against), `docs/COMMANDS.md`, `docs/CLAUDE-HOOK.md` and
`docs/PROVIDERS.md`.

## Layout

- `cmd/tentaqles/`, `cmd/tq/`: one program under two names; the release builds `./cmd/tentaqles` as both.
- `internal/cmd/`: every command (`root.go` registers them): `reach`, `add`, `remove`, `drop`, `env`, `activate`, `hooks`, `run`, `login`,
  `open`, `settings render`, `statusline`, `doctor`, `list`, `providers`, `version`, `claude-hook`.
  `claudehook.go` is the hook (`session-start`, `pre-tool-use`); `render.go` writes the PATH mask and the browser
  shims.
- `internal/reach/`: the form's options and answers, `Change` (what `add`/`remove` mean), and `Edit`, which
  changes only the keys the form owns.
- `internal/workspace/`: creates a client (manifest, marker, identity folders, git include).
- `internal/resolve/`: maps a folder to its client (marker in the folder, manifest under `~/.tentaqles`).
- `internal/manifest/`, `internal/registry/` (the clients folders), `internal/paths/`.
- `internal/envplan/`: the identity environment: variables, the PATH layer and mask, the browser shims, the shell
  state that restores what was there on leaving. `internal/scope/` compiles what a client's terminal hides.
- `internal/shell/`, `internal/hooks/`: the shell activation templates and the profile blocks.
- `internal/guard/`: the drift check (`guard.Decide`) and the shell-text reading it needs.
- `internal/guardplugin/`: whether a client's Claude config has the enabled plugin hook.
- `internal/providers/`: the catalog (`catalog/*.yaml`, embedded) and user provider files.
- `internal/doctor/`, `internal/claudesettings/`, `internal/statusline/`, `internal/browser/`, `internal/banner/`,
  `internal/gitcfg/`, `internal/detect/`, `internal/testutil/`.
- `plugin/`: the Claude Code plugin: `hooks/hooks.json` and `scripts/tq_hook.sh`, which runs `tentaqles claude-hook`.
- `test/integration/`: git identity end to end, the marketplace pins, the release config.
- `installers/`, `.goreleaser.yaml`, `.github/workflows/`: release and CI. Steps in `docs/RELEASING.md`.

## Must not trade off

Each line names the tests that keep it. Weakening, skipping or removing one needs the owner's yes.

- A client's terminal and `tentaqles run` carry that client's identity and no other, and leaving the folder restores the
  environment exactly: `TestRunClaudeHasTheClientsIdentity`, `TestRunDotResolvesCwd`, `TestEnv_ScopeLayerRestore`,
  `TestLayer_EnterAndLeaveRestoresExactly`.
- A CLI of a provider outside the client's scope does not resolve on its PATH (dropped, or masked in a shared
  folder), its variables are unset, and an in-scope claim always wins: `TestEnv_ScopeReferenceScenario_PATH`,
  `TestEnv_ScopeReferenceScenario_SharedFolder`, `TestRun_OutOfScopeCLIRefused`, `TestOutOfScopeExecutables`,
  `TestSubcommandClaimKeepsTheCLI`, `TestOutOfScopeEnv`, `TestInScopeClaimIsTheEmbeddedOne`,
  `TestCatalogMatchLists`, `TestUserOverrideExtendsMatch`.
- The hook blocks a git, gh or cloud command under the wrong identity, never read-only git for drift, and allows
  local commands: `TestCanonicalSuite`, `TestReadOnlyGitExemptFromEnvDrift`, `TestNeutralRemoteNamesReach`,
  `TestReach_GuardsTheNewClient`.
- The hook never stalls a session and fails closed: `TestHookDeadlinesUnderHarnessTimeouts`,
  `TestHookStall_SpinningDecisionIsDenied`, `TestHookStall_WatchdogEndsAStuckMainGoroutine`,
  `TestPreToolUseGuardPanicDenies`, `TestRunMainHookErrorsDeny`, `TestRunMainWatchdogDenies`, and the plugin's
  `test_tq_hook_*` tests.
- No Claude session starts in a client's folder without the plugin hook: `TestRun_RefusesWithNoGuardHook`,
  `TestRun_DisabledPluginIsNoHook`, `TestRun_ForeignHookIsNoHook`, `TestRun_ProjectSettingsCountToo`.
- A Claude session runs only its own folder's client: `TestRunOtherClient_RefusedForAgents`,
  `TestRunOtherClient_NeutralFolderRefused`, `TestRunOtherClient_InheritedEnvIgnored`,
  `TestRunOtherClient_HumanAllowed`.
- reach writes nothing before a typed yes, and an edit changes only the keys the form owns:
  `TestReach_ConfirmsBeforeWriting`, `TestReach_ExistingClientDeclinedChangesNothing`, `TestEdit_Providers`,
  `TestEdit_NoChangeIsTheSameBytes`, `TestChange_DeclinedWritesNothing`, `TestChange_Refusals`,
  `TestEdit_DropsAHandListedIdentity`.
- drop deletes nothing before the client's name is typed back, deletes nothing of another client, and keeps the
  user's files: `TestDrop_DeclinedDeletesNothing`, `TestDrop_UndoesTheClient`, `TestDrop_Refusals`.
- A copied marker or manifest lends a folder nothing: `TestResolve_MarkerOrManifestOfAnotherClient`.
- A client with browsers opens links in its own profile: `TestBrowserShims`, `TestOpen_UsesTheClientsProfile`,
  `TestOpen_Refusals`.
- The plugin installs from a pinned tag and sha, and the release ships both names as the same bytes:
  `TestRootMarketplacePin`, `TestGoreleaserBuildsOneProgramUnderTwoNames`.

## Project config

CI's `go` job runs `go vet ./...`, `go test ./... -count=1` and a build on three OSes; locally run `go build ./...`
too. The plugin suite is `PYTHONPATH=plugin python -m pytest plugin/tests -q` (needs Git Bash on Windows). The
changelog is `CHANGELOG.md` under `[Unreleased]`; a change under `plugin/` also gets a line in
`plugin/CHANGELOG.md`.

Every command is `tentaqles <command>`; `tq <command>` is its alias. User-facing text (help, errors, fix lines,
docs) names the `tentaqles` form.

## Rules

- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test` and git through PowerShell. Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`
  (each package's `TestMain` calls `testutil.HomeRun`); a test never touches the real `~/.tentaqles`.
- Language: en. Conventional commits.
