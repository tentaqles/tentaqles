# tentaqles

An identity isolator for developers who work for several clients: one folder per client, and a terminal opened in
it, or in any subfolder, carries that client's identity and nothing else: its Claude Code config home (its own
subscription), its git name and email, its own login for each CLI, its browser profile, and a PATH without the CLIs
of providers it doesn't use. Claude writes the client's documents and messages in the client's language. A small Claude Code hook blocks a git, gh or cloud command that would run under the
wrong identity.

## Reading path

`README.md` is what it is for. `docs/README.md` indexes the rest: `docs/how-it-works.md`, `docs/threat-model.md`, and
the references under `docs/reference/` (commands, manifest, providers, claude-hook). The command reference has a
section for every command; `test/integration` fails when one is missing.

## Layout

- `cmd/tentaqles/`, `cmd/tq/`: one program under two names; the release builds `./cmd/tentaqles` as both.
- `internal/cmd/`: every command (`root.go` registers them): `reach`, `add`, `remove`, `drop`, `bind`, `env`, `activate`, `hooks`, `run`, `login`,
  `open`, `settings render`, `statusline`, `doctor`, `list`, `providers`, `version`, `claude-hook`.
  `claudehook.go` is the hook (`session-start`, `pre-tool-use`); `codexhook.go` is the Codex CLI hook (`codex-hook`);
  `edit.go` is `add`/`remove`, `drop.go` is `drop`; `lobe.go` is the `lobe` group and `lobe_sandbox.go` its first
  command, `lobe sandbox` (a throwaway home with two test clients);
  `render.go` writes the PATH mask and the browser shims.
- `internal/reach/`: the form's options and answers, `Change` (what `add`/`remove` mean), and `Edit`, which
  changes only the keys the form owns.
- `internal/workspace/`: creates a client (manifest, marker, identity folders, git include).
- `internal/resolve/`: maps a folder to its client (marker in the folder, manifest under `~/.tentaqles`).
- `internal/manifest/` (`language.go`: the client languages), `internal/registry/` (the clients folders and each client's folder), `internal/paths/`.
- `internal/envplan/`: the identity environment: variables, the PATH layer and mask, the browser shims, the shell
  state that restores what was there on leaving. `internal/scope/` compiles what a client's terminal hides.
- `internal/shell/`, `internal/hooks/`: the shell activation templates and the profile blocks.
- `internal/guard/`: the drift check (`guard.Decide`) and the shell-text reading it needs.
- `internal/guardplugin/`: whether a client's Claude config has the enabled plugin hook.
- `internal/codexconfig/`: tentaqles' part of a client's Codex `config.toml`: the two hooks, their trust, and
  `developer_instructions`.
- `internal/providers/`: the catalog (`catalog/*.yaml`, embedded) and user provider files.
- `internal/lobe/`: the memory ("the Vertical Lobe"), one package per part, imports only `internal/resolve`,
  `internal/paths`, `internal/registry` and `internal/manifest` from this repository, no `init` and no
  package-level call: `hook/` (the `lobe-hook` entry, fail open, bounded, answered in `main` before cobra),
  `store/` (the per-client folder, its lock, whole-line appends), `scrub/` (secrets out before anything is
  written), `eventlog/` (the event's shape and its day file), `sense/` (a failed call becomes an event),
  `selftest/`. Design: `docs/prd/`, build order: `docs/features/`.
- `internal/doctor/`, `internal/claudesettings/`, `internal/statusline/`, `internal/browser/`, `internal/banner/`,
  `internal/gitcfg/`, `internal/detect/`, `internal/testutil/`.
- `plugin/`: the Claude Code plugin: `hooks/hooks.json`, `scripts/tq_hook.sh` (runs `tentaqles claude-hook`, fails
  closed) and `scripts/lobe_hook.sh` (runs `tentaqles lobe-hook`, fails open).
- `test/integration/`: git identity end to end, the marketplace pins, the release config, the docs.
- `installers/`, `.goreleaser.yaml`, `.github/workflows/`: release and CI. Steps in `docs/RELEASING.md`.
- `dev/tqx.ps1`: builds this checkout as `tqx` beside the released program, never over it, and opens a
  playground on a throwaway home (`-Playground`).

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
- No Codex session starts in a client's folder without tentaqles' enabled, trusted hooks, and a hook error is a deny:
  `TestRunCodex_RefusesWithoutTheHooks`, `TestRunCodex_RefusesHooksOffOptions`, `TestCodexPreToolUse_PanicDenies`,
  `TestCodexPreToolUse_WatchdogDenies`, `TestCodexHookProgram`.
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
- A client is the folder it was created in and no other: a copy of it under another clients folder gets no
  identity and is denied, and only a person moves the record: `TestResolve_SecondFolderWithAClientsName`,
  `TestResolve_ClientWithNoRecordedFolder`, `TestPreToolUse_CopiedFolderDenies`,
  `TestEnv_SecondFolderGetsNoIdentity`, `TestBind_RefusedForAgents`, `TestBind_DeclinedWritesNothing`,
  `TestDrop_RefusesACopiedFolder`.
- A client with browsers opens links in its own profile: `TestBrowserShims`, `TestOpen_UsesTheClientsProfile`,
  `TestOpen_Refusals`.
- A memory hook never stalls a session and fails open: exit 0 and no output on any failure, every run bounded by
  a cutoff and a watchdog: `TestLobeHookAlwaysExitsZero`, `TestLobeCaptureSurvivesALateStep`,
  `TestLobeStall_SpinningHandler`, `TestLobeStall_WatchdogEndsAStuckMain`,
  `TestLobeHookCutoffsUnderRegisteredTimeout`, `TestLobeLogLockHeld`, and the plugin's `test_lobe_hook_*` tests.
- A memory hook serves only the client that both the folder and the session's login name; otherwise it writes
  nothing anywhere: `TestLobeHookServesOnlyItsLogin`, `TestLobeHookLoginUnsetDoesNothing`,
  `TestLobeHookLoginPathForms`, `TestLobeSenseFailureSkips`, `TestLobeStoreRefusesLinkAndForeign`.
- No secret reaches the memory's store: `TestLobeScrubParity`, `TestLobeScrubCleanParity`,
  `TestLobeSenseSecretsNeverInTheEvent`, `TestLobeHookErrorsHoldNoContent`, `TestLobeLogSecretInMetadataIsRefused`.
- The guard never imports or runs memory code, and the memory imports only the four packages:
  `TestLobeGuardNeverImportsLobe`, `TestLobeNoInitAndNoStartupCalls`, `TestLobeImportsOnlyTheFour`,
  `TestLobeStall_GuardStillDenies`.
- The plugin installs from a pinned tag and sha, and the release ships both names as the same bytes:
  `TestRootMarketplacePin`, `TestGoreleaserBuildsOneProgramUnderTwoNames`.

## Project config

CI runs `go vet ./...`, `go test ./... -count=1` and a build on three OSes, plus `govulncheck`, the marketplace pin
check and the plugin suite; locally run `go build ./...` too. The plugin suite is `PYTHONPATH=plugin python -m pytest plugin/tests -q` (needs Git Bash on Windows). The
changelog is `CHANGELOG.md` under `[Unreleased]`; a change under `plugin/` also gets a line in
`plugin/CHANGELOG.md`.

Every command is `tentaqles <command>`; `tq <command>` is its alias. User-facing text (help, errors, fix lines,
docs) names the `tentaqles` form.

## Rules

- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test` and git through PowerShell. Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`
  (a package whose tests touch them runs `testutil.HomeRun` from its `TestMain`); a test never touches the real
  `~/.tentaqles`.
- Language: en. Conventional commits.
