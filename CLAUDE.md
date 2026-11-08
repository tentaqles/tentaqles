# tentaqles (monorepo)

One repo, one installer: an identity isolator with secrets protection for developers who
work across several clients/companies. One folder per client; the terminal, the agent
sessions and the external tools (git and its hosts, the cloud CLIs, the Claude
config home) carry that client's identity and nothing else; a command under the wrong
identity is blocked with the reason.

## Reading path
`docs/IDEATION.md` is the spec: judge scope against it. Then `docs/threat-model.md` and the ADRs under
`docs/adr/`. `docs/prd.md` and PRD v2 are history, and some docs still describe removed commands.

## Layout
- `cmd/tentaqles/`, `cmd/tq/`, `internal/` — the Go CLI, one program under two names; the release builds
  `./cmd/tentaqles` as both `tentaqles` and `tq`, the same bytes (ADR-0021). `go test ./...` must stay green.
  - `internal/guard/` — the identity-drift block, `guard.Decide` (`blocked-command`,
    `wrong-cloud`, `neutral-remote`, `untrusted`, `env-drift`, `claude-config-drift`,
    `git-email-drift`, `gh-user`) and the shell tokenizer the hook uses.
  - `internal/policy/` — the five secret rules of `builtin.go` (`tq/secret-store-read`,
    `tq/secret-store-shell`, `tq/env-file-read`, `tq/env-file-shell`, `tq/env-dump`), the
    two self-protection rules of `selfprotect.go` (`tq/protected-path`,
    `tq/human-only-command`, ADR-0005), the six identity rules of `identity.go`
    (`policy.Scope`: `out-of-scope-cli`, `-env`, `-path`, `-host`, `-mcp`,
    `other-identity`, read from the catalog's `match:` lists and `manifest.Scope()`,
    ADR-0007), the seventh, `other-browser-profile` (`browserprofile.go`, from the manifest's
    `browsers:`, ADR-0016), and their evaluator; nothing else is a rule. Every
    rule carries its category (`identity`, `secrets`, `self-protection`); a deny reads
    `BLOCKED [<category>/<rule>]`.
  - `internal/cmd/` — every command; `internal/cmd/root.go` registers them: `reach`, `open`,
    `allow`, `deny`, `list`, `env`, `activate`, `hooks`, `login`, `run`, `doctor`, `status`, `bundle`,
    `providers`, `secret`, `secrets`, `settings`, `statusline`,
    `dotenv`, `migrate`, `uninstall`, `version`, `connect`, `claude-hook`. `connect` is the
    one agent-callable write of the identity's `.claude.json` (catalog entry only,
    `internal/connect`, ADR-0017); `status --json` is the onboarding skill's read
    (`internal/identitystatus`). The hook is
    `claudehook*.go`: `pre-tool-use` runs `guard.Decide` (identity), the provider-scope
    rules (`claudehook_scope.go`), then the secret rules
    plus `tq/commit-secret` and `tq/cross-client-mcp`, then the self-protection rules on
    the protected path set of the run (`claudehook_protect.go`), first category that
    denies wins; there is no ask, and a call it cannot judge (an unreadable payload, a
    panic, the 6 s deadline, an unreadable registry or manifest) is denied as
    `tq/guard-error` (ADR-0006); every deny is one line in `$TQ_HOME/denials.jsonl`, the
    command redacted by `audit.RedactCommand`, read by `tq status --denials`; `session-start` prints the preamble. `humanonly.go` is the thin wrapper over
    `internal/humanonly` (`refuseAgent`, `humanGate`) that every command writing under
    `paths.Home()` or an identity dir calls.
  - `internal/markers/` — the one list of harness environment markers; `internal/paths`
    (TQ_HOME ignored under a marker) and `internal/humanonly` both read it.
  - `internal/humanonly/` — the human-only check of PRD v2 §8.1 (ADR-0003): three
    detectors (`IsTerminal` on stdin and stdout, the process-tree walker `Walk` with a
    per-OS `parentOf` in `proctree_*.go`, the `Markers` list), a refusal logged to
    `$TQ_HOME/denials.jsonl` through `audit.AppendDenial`, and the typed-yes `Gate`
    read from the controlling terminal. The seams `IsTerminal`, `ParentOf` and
    `Confirm` are what tests replace (`internal/cmd/main_test.go` sets the human
    baseline for the whole cmd suite).
  - `internal/trust/` (the signed integrity record per workspace, `trust/<name>.json`:
    manifest, marker, the enforcing keys of the Claude settings (`settings.json#tq`), the
    tentaqles entry of `installed_plugins.json`, the loaded plugin folder but its skills,
    agents, templates, tests and caches, and shims, under an HMAC whose
    key is in the OS keychain; only the roots are resolved; every signature passes
    `HumanCheck`; `tq deny` leaves a signed tombstone; legacy markers count only from the
    signed snapshot `trust/legacy.json`, never once `trust/.signed` exists without it;
    ADR-0004), `internal/manifest/` (the manifest,
    read from `~/.tentaqles/identities/<ws>/manifest.yaml` with a name-only marker
    `.tentaqles.yaml` in the workspace root, or from the root itself for an identity not
    yet migrated with `tq migrate manifests`, ADR-0002; a top-level block tq does not
    know is recorded as dropped, never read), `internal/protect/` (the state folder
    private to the user: modes, and on Windows the one icacls run `tq doctor` reports as
    `protected-acl`; `pathset.go`, the protected path set of PRD v2 §8.2 the hook's
    `tq/protected-path` matches against), `internal/registry/`, `internal/resolve/` (which file is the
    manifest, both layouts), `internal/workspace/`, `internal/paths/`,
    `internal/envplan/` (the env diff, the identity PATH, the PATH
    mask and the out-of-scope variables unset, ADR-0008), `internal/mcpscope/` (the
    out-of-scope MCP servers removed from the identity's Claude home),
    `internal/shell/` (activation hooks and the `claude` launcher), `internal/hooks/` (shell profile blocks), `internal/gitcfg/`,
    `internal/doctor/`, `internal/reach/` and `internal/banner/` (`tentaqles reach`),
    `internal/migrate/`, `internal/bundle/`, `internal/providers/` (the
    embedded catalog), `internal/claudesettings/`,
    `internal/statusline/`, `internal/secret/` (`tq secret`, OS keychain via go-keyring),
    `internal/secrets/` (`tq secrets audit`), `internal/dotenv/`, `internal/commitscan/`
    (`tq/commit-secret`), `internal/audit/`, `internal/detect/`, `internal/verify/`,
    `internal/termopen/`, `internal/testutil/`.
- `test/integration/` — git identity end to end and the marketplace pin tests.
- `providers/` — a README; the provider catalog YAML lives in `internal/providers/catalog/`.
- `plugin/` — the Claude Code plugin: the onboarding skill and the guard hook, which shells out to
  `tq claude-hook`. No Python. Installed from this repo's root marketplace
  (`.claude-plugin/marketplace.json`, git-subdir `plugin/` pinned to a release tag and its sha, added after
  the tag in a post-release PR):
  `/plugin marketplace add tentaqles/tentaqles`, `/plugin install tentaqles@tentaqles`.
  Release steps in `docs/RELEASING.md`.
- `installers/`, `.goreleaser.yaml`, `.github/workflows/` — CLI release + CI. `test.yml`
  has four jobs: `go` (three OSes), `govulncheck`, `marketplace-pin`, `plugin`;
  `release.yml` runs on a `v*` tag.
- `scripts/spike-isolation.sh` — the macOS keychain-isolation spike behind the "Limitations"
  note of `README.md`.
- `docs/` — user-facing docs: `CLAUDE-HOOK.md`
  (the hook protocol, the identity rules, the secret rules, `tq dotenv run`),
  `CLAUDE-SETTINGS.md` (`tq settings render`, `tq statusline`),
  `COMMANDS.md` (every command and skill, plus a test walkthrough), `PROVIDERS.md` (the
  catalog schema, `tq secret`), `MIGRATE.md` (`tq migrate`, the journal,
  `tq uninstall --restore`, the `TQ_ENABLED=0` kill switch, "Upgrading an existing install"),
  `RELEASING.md` (release PR, tag, release workflow, post-release PR pinning ref + sha),
  `IDEATION.md` (the isolation vision), `prd.md`, `threat-model.md`, `adr/`.

## Development process
This repo is developed with the build loop in `.claude/`: read `.claude/README.md`. It's for
working on this repo and does not ship: nothing in the release, the plugin or the installers
depends on it. `/write-tickets` turns work into tickets under `docs/tickets/`; the router builds them, in the
same session (`/router`) or its own (`claude --agent router --name router`). Low-risk changes
need no ticket.

@REVIEW.md

## Must not trade off
From `docs/threat-model.md`; each line names the test that keeps it.
- Identity drift blocks git/gh/cloud commands: `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`.
- An agent session runs `tq run` only for the identity of its own folder (FR-13, ADR-0020): another
  client's name or a neutral folder is refused: `TestRunOtherClient_RefusedForAgents`,
  `TestRunOtherClient_NeutralFolderRefused`, `TestRunOtherClient_InheritedEnvIgnored`; a human's
  `tq run <any client>` stays free: `TestRunOtherClient_HumanAllowed`.
- Everything outside the identity's provider scope is denied in every
  tool (CLI by name, path, wrapper, `python -m`; variables; credential paths; API hosts; MCP
  servers; other identities' roots), before secrets, with a message naming the scope; the
  same identity's in-scope CLIs and ordinary dev commands pass; a manifest needs no flag for
  this: `TestPreToolUse_ScopeReferenceScenario`, `TestPreToolUse_ScopeRuleOrder`,
  `TestPreToolUse_ManifestNeedsNoFlag`, `TestScope_OutOfScopeCLI`, `TestScope_OutOfScopeEnv`,
  `TestScope_OutOfScopePath`, `TestScope_OutOfScopeHost`, `TestScope_OutOfScopeMCP`,
  `TestScope_OtherIdentity`, `TestScope_InScopeWins`, `TestCatalogMatchLists`,
  `TestUserOverrideExtendsMatch`.
- The scope is provider-specific (a shared CLI is owned per subcommand, a
  user file can only widen denial, a new user id may not claim a built-in's entry), a folder
  whose manifest cannot be read denies every tool with `tq/guard-error`, a bad user provider
  file keeps the built-in lists (an override) or denies only where the scope names it (a new
  id), a PowerShell line is split into commands with PowerShell's quoting as well as bash's,
  and grep patterns / commit messages / data here-strings stay text, by each shell's own
  quoting, while real expansions and run or sent here-strings are scanned: `TestScope_EveryProvider` (every entry of every
  list), `TestScope_SharedCLIPrecision`, `TestScope_UserOverrideCannotAllow`,
  `TestScope_MCPExactAllowAndPrecedence`, `TestScope_Mutants`, `TestScope_Round3Missed`,
  `TestScope_Round3FalsePositives`, `TestScope_PerShellQuoting`, `TestMaskTextArgs_FailClosed`,
  `TestScope_PowerShellCommandSplitting`, `TestSelfProtection_PowerShellCommandSplitting`,
  `TestScope_HereStringsAndHeredocsAsCode`, `TestScope_HostPaths`, `TestScope_GitTransport`,
  `TestPreToolUse_UnreadableManifestDeniesAll`, `TestPreToolUse_BadProviderFiles`,
  `TestPreToolUse_OldGuardKeysStillGuarded`, `TestDeclaredCloudIsInScope`.
- The §6.1 scenario also fails before the hook (ADR-0008): an
  out-of-scope CLI does not resolve on the identity PATH (its folder dropped, or masked by
  a stub in a shared folder) while in-scope CLIs do, its variables are unset, leaving the
  folder restores PATH and variables exactly, `tq run` resolves on the identity PATH, the
  render leaves no out-of-scope MCP server in the identity's Claude home and
  never touches one that matches no provider, the mask folder is used only as tq wrote it,
  and the preamble states the scope: `TestEnv_ScopeReferenceScenario_PATH`,
  `TestSettingsRender_ScopeReferenceScenario_MCP`, `TestSessionStart_ScopeLine`,
  `TestEnv_ScopeLayerRestore`, `TestPathLayer_ScenarioAwsNotOnPath`,
  `TestPathLayer_SharedDirIsMasked`, `TestPathLayer_TamperedMaskIsNotUsed`,
  `TestLayer_EnterAndLeaveRestoresExactly`, `TestPathSyntax_MSYS`, `TestFilterClaude`; round 2: the state fits cmd.exe and every shell restores byte for byte,
  the mask answers every spelling and comes back once fixed, generic tools' variables and
  ambiguous names are left alone, MCP servers are attributed by their config entry at render
  and in the hook: `TestEnv_ScopeReferenceScenario_SharedFolder`,
  `TestEnv_ScopeReferenceScenario_CmdExe`, `TestPathLayer_MaskCoversEverySpelling`,
  `TestPathLayer_MaskComesBack`, `TestState_FitsCmdLine`, `TestEmit_Cmd_RoundTripsEveryCharacter`,
  `TestPreToolUse_MCPJudgedByConfigEntry`, `TestOutOfScopeMCPEntry`,
  `TestOutOfScopeEnv_SkipsGenericTools`; round 3: a same-named server is denied when any
  source's entry is out of scope, every vendor fixture is attributed, a lost state fails
  closed: `TestOutOfScopeMCPEntry_VendorFixtures`, `TestScope_MCPSameNameAnySourceDenies`,
  `TestEntries`, `TestEntries_RemovedStillJudged`, `TestLostState_FailsClosed`,
  `TestUnrecorded`, `TestEmit_Cmd_LineBudget`, `TestPathLayer_StubIsTheTQBinary`; round 4:
  `TestEnv_UnicodePathFoldersRoundTrip`, `TestMCPVendorsHaveMCPData`,
  `TestOutOfScopeMCPEntry_Round3Fixtures`, 
  `TestPluginServers_EveryShape`, `TestLostState_HarnessMissingStateIsLost`; the hybrid MCP
  list: `TestJudgeMCP_Hybrid`, `TestScope_MCPHybridInTheHook`, `TestJudgeMCP_FixtureKeptSets`,
  `TestSettingsRender_HybridMCP`, `TestMCPGeneral`, `TestMCPAllow`.
- A client with a `browsers:` list opens pages only in its own browser profile: `tentaqles open`,
  `BROWSER` and the client's `xdg-open` and `open` start the listed browser by install path with its
  `--profile-directory`; the hook denies a browser started with another profile or none, the OS routes that
  open a URL in the default browser, and Claude in Chrome when Chrome is not the client's, as
  `identity/other-browser-profile`; the shims are protected and covered by the integrity record; without
  `browsers:` nothing changes (ADR-0016): `TestOtherBrowserProfile_Fixtures`,
  `TestOtherBrowserProfile_FalsePositives`, `TestPreToolUse_OtherBrowserProfileDenied`,
  `TestPreToolUse_OsUrlOpenDenied`, `TestPreToolUse_ClaudeInChromeNeedsChrome`, `TestOpen_Refusals`,
  `TestBrowserShims`, `TestBrowserShims_ProtectedAndSigned`, `TestBrowser_NoBlockNoChange`.
- The secret rules fire on `.env` reads, environment dumps and secret-store reads, and a
  `.env.example` is allowed: `TestPreToolUse_SecretRules`, `TestBuiltinIsTheFiveSecretRules`.
- Trust is human-only, and so is every tq write under `$TQ_HOME` or an identity dir:
  `TestAllowAndDenyRefuseAgents`, `TestReachDoesNotTrustFromAgent`,
  `TestHumanOnlyCommandsRefuseWithMarkerAndLog`, `TestReachConfirmsBeforeWriting`, and the
  detector tests of `internal/humanonly` (`TestTTYDetector`, `TestProcessTreeDetector`,
  `TestEnvMarkerDetector`, `TestGateConfirms`), `TestHooksVerify_ReportExitAndAccept`.
- An edited manifest loses its trust: `TestEditedManifestLosesTrust`.
- A session cannot read or write tq's state, nor write the root marker, the installed
  plugin, the tq binary or tq's keychain entry, by any tool, nor run a human-only tq verb
  in the shell; the categories run blocked_commands → identity → secrets →
  self-protection and every deny is logged: `TestProtectedPaths_Table`,
  `TestProtectedPaths_RuleOrder`, `TestKeychainWrite`.
- `tentaqles connect` is the one agent-run write of the identity's `.claude.json`, so it
  is narrow: the provider and nothing else, the embedded catalog's entry (a user file's
  `connect:` is not loaded), a provider in the identity's scope (a refusal is logged),
  a trusted client, each change logged without the entry; the file itself stays a protected
  path for every file tool and shell redirect, and `status --json` carries no secret:
  `TestConnect_WritesCatalogEntry`, `TestConnect_IdempotentAndNoEntry`,
  `TestStatusAndConnect_BadStates`, `TestConnect_OutOfScopeRefused`,
  `TestConnect_NoCallerInput`, `TestConnect_SurvivesRenderAndHook`,
  `TestConnect_DamagedClaudeJson`, `TestConnect_AtomicWrite`,
  `TestPreToolUse_ConnectAllowedFileToolsDenied`, `TestStatusJSON_NoSecrets`,
  `TestPreToolUse_MCPEntryCopiesProtected`, `TestConnect_ConcurrentWriterIsKept`,
  `TestConnect_WarnsWhenClaudeIsOpen`.
- The hook's cross-client exemption for a connected server is exactly its catalog entry: a same-named
  server with any other entry, in `.claude.json` or a `.mcp.json` in the folder, stays denied, and so
  does any server when the config cannot be read: `TestHook_ConnectedServerNotCrossClientDenied`,
  `TestHook_OtherClientsBundleServerStaysDenied`, `TestHook_OutOfScopeProviderNameStaysDenied`,
  `TestHook_ForeignSameNameInProjectMCPJSONDenied`, `TestHook_ForeignBesideCatalogEntryDenied`,
  `TestHook_OnlyCatalogEntryPasses`, `TestHook_UnreadableConfigNoExemption`,
  `TestHook_UnreadableMCPJSONNoExemption`; an entry a render removed does not count: `TestHook_RemovedEntryDoesNotBlockConnectedServer`.
- The integrity record is signed and covers the hooks, the enforcing settings keys and the
  shims; a mismatch is at least as strict as untrusted (the hook denies mutating git and
  remote commands), withholds the identity and refuses the launch; a signed workspace never
  falls back to a legacy marker; only a human signs:
  `TestFlippedByteFailsVerification`, `TestPluginHookChangesAreNamed`,
  `TestPreToolUse_IntegrityMismatchDeniesRemoteAndGit`, `TestRun_RefusesOnIntegrityMismatch`,
  `TestEnv_NoIdentityAndOneNoticeOnMismatch`, `TestLegacyDowngradeIsUntrusted`,
  `TestSigningRefusesUnderAHarnessProcessTree`.
- The plugin hook never takes the enforcing binary from the environment:
  `test_tq_bin_is_not_honoured`, `test_tq_hook_never_runs_a_tq_from_the_plugin_folder`.
- A batch accept signs only a plugin folder that is, byte for byte, the release tq was built
  from (the commit in the binary, never the clone's pin):
  `TestCheckReleaseComparesBytesNotLocation`, `TestARepinnedCloneIsRefused`,
  `TestHooksVerify_AcceptAllSignsThePublishedReleaseOnly`, `TestReleaseBuildInjectsTheCommit`.
- The guard reads the manifest from the protected location only; a marker that names
  another workspace, or has no protected copy, resolves neutral:
  `TestResolve_MarkerClientMismatch_Neutral`, `TestResolve_MarkerWithoutProtectedManifest_Neutral`.
- A trusted manifest copied into another folder lends it nothing (the manifest's `client`
  must be the folder name; trust is keyed by name and hash), and `TQ_HOME` is ignored in
  an agent session: `TestResolve_ReplayOfATrustedManifest_Neutral`,
  `TestTrustIsKeyedByWorkspaceName`, `TestHome_IgnoresTQHomeUnderAnAgentMarker`.
- The plugin installs from a pinned tag and sha: `TestRootMarketplacePin`.
- No Claude session starts in an identity's folder without the tentaqles guard (the enabled plugin's
  PreToolUse hook, ADR-0018): `TestRun_RefusesWithNoGuardHook`, `TestRun_DisabledPluginIsNoHook`,
  `TestRun_ForeignHookIsNoHook`, `TestRun_RefusalOnlyForASession`, `TestRun_ProjectSettingsCountToo`; `tq reach` installs the plugin with
  `TestReach_InstallsThePlugin`.
- The hook has no ask and fails closed: a payload it cannot read, a guard panic, the
  deadline, an unreadable registry or manifest, and an incomplete commit scan all deny:
  `TestNoAskCodePath`, `TestNoAskVerdict`, `TestPreToolUse_MalformedPayloadDenies`,
  `TestPreToolUseGuardPanicDenies`, `TestPreToolUse_TimeoutDenies`,
  `TestPreToolUse_UnreadableRegistryDenies`, `TestPreToolUse_MissingOrBrokenManifestDenies`,
  `TestPreToolUseIncompleteScanDenies`, `TestPreToolUse_FieldTypesDeny`,
  `TestPreToolUse_GoneManifestWithEvidenceDenies`, `TestNoAskAnswerInAnyOutput`,
  `TestRunMainHookErrorsDeny`, `TestRunMainWatchdogDenies`,
  `test_tq_hook_turns_a_tq_without_decision_into_a_deny`,
  `test_tq_hook_stops_a_hanging_tq_and_denies`.
- Every hook deadline is under the shortest harness hook timeout (a harness runs a call whose
  hook it killed): `TestHookDeadlinesUnderHarnessTimeouts`,
  `test_tq_hook_deadline_is_under_the_hook_timeout`.
- Every deny is logged with identity, caller, tool, rule, category and layer, and the logged
  command carries no secret tq can recognise; only a human reads the log:
  `TestDenialLogSchema`, `TestDenialLogRedaction`, `TestRedactCommandLeakCorpus` (0 leaks),
  `TestAppendDenialMasksEveryField`, `TestStatusDenialsRefusesAgent`,
  `TestStatusDenialsEscapesControlCharacters`, `TestDenialLogRotatesAndStreams`.
- tq writes no permission setting of the harness, cleans the ones it wrote, and an old
  manifest with `claude.permission_mode`/`claude.permissions` still loads (PRD v2 §7.2,
  §13): `TestRenderFreshWritesNoPermissionKey`, `TestRenderCleansWhatTheOldRenderWrote`,
  `TestLoadRecordsDroppedClaudeKeys`,
  `TestRenderKeepsADefaultModeTheEvidenceDoesNotName`, `TestRenderCorruptStateClaimsNothing`,
  `TestNothingWrittenNamesAPermissionMode`.

## Project config
CI's `go` job runs `go vet ./...`, `go test ./... -count=1` and a build; locally run
`go build ./...` too. The plugin suite is `PYTHONPATH=plugin python -m pytest plugin/tests -q`
(CI job `plugin`; seconds). The changelog is `CHANGELOG.md` under
`[Unreleased]`; a change under `plugin/` also gets its line in `plugin/CHANGELOG.md`, and the
root entry is then one pointer line.

A change under `internal/guard/`, `internal/policy/`, `internal/trust/`, `internal/humanonly/`,
`internal/markers/`, `internal/protect/`, `internal/hooks/`,
`plugin/hooks/`, `plugin/scripts/tq_hook.sh`, `.github/workflows/`,
`.goreleaser.yaml`, `.claude-plugin/marketplace.json` or `installers/` crosses a trust
boundary of `docs/threat-model.md` and needs an ADR under `docs/adr/` (follow the shape of the latest one there:
Status, Date, Cites, Context, Decision, Consequences, Alternatives considered).

## Rules
- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test`/git via PowerShell; tests must isolate `TQ_HOME`, `HOME`, `USERPROFILE`.
- Language: en. Conventional commits.
