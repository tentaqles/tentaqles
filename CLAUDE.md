# tentaqles (monorepo)

One repo, one installer: an identity isolator with secrets protection for developers who
work across several clients/companies. One folder per client; the terminal, the agent
sessions and the external tools (git and its hosts, the cloud CLIs, the Claude and Codex
config homes) carry that client's identity and nothing else; a command under the wrong
identity is blocked with the reason.

## Reading path
To work on this repo read `docs/prd.md` (requirements with status, backlog and the owner's
decisions) and `docs/threat-model.md` first, then the ADRs under `docs/adr/`.

## Layout
- `cmd/tq/`, `internal/` — the `tq` Go CLI. `go test ./...` must stay green.
  - `internal/guard/` — the identity-drift block, `guard.Decide` (`blocked-command`,
    `wrong-cloud`, `neutral-remote`, `untrusted`, `env-drift`, `claude-config-drift`,
    `git-email-drift`, `gh-user`) and the shell tokenizer the hook uses.
  - `internal/policy/` — the five secret rules of `builtin.go` (`tq/secret-store-read`,
    `tq/secret-store-shell`, `tq/env-file-read`, `tq/env-file-shell`, `tq/env-dump`), the
    two self-protection rules of `selfprotect.go` (`tq/protected-path`,
    `tq/human-only-command`, ADR-0005), the six identity rules of `identity.go` under
    `guard: v2` (`policy.Scope`: `out-of-scope-cli`, `-env`, `-path`, `-host`, `-mcp`,
    `other-identity`, read from the catalog's `match:` lists and `manifest.Scope()`,
    ADR-0006) and their evaluator; nothing else is a rule. Every
    rule carries its category (`identity`, `secrets`, `self-protection`); a deny reads
    `BLOCKED [<category>/<rule>]`.
  - `internal/cmd/` — every command; `internal/cmd/root.go` registers them: `init`, `add`,
    `allow`, `deny`, `list`, `env`, `activate`, `hooks`, `login`, `run`, `doctor`, `status`, `bundle`,
    `providers`, `setup`, `onboard`, `secret`, `secrets`, `settings`, `statusline`,
    `worktrees`, `dotenv`, `migrate`, `uninstall`, `version`, `claude-hook`. The hook is
    `claudehook*.go`: `pre-tool-use` runs `guard.Decide` (identity), the provider-scope
    rules under `guard: v2` (`claudehook_scope.go`), then the secret rules
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
    agents, templates, tests and caches, Codex hook files and shims, under an HMAC whose
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
    `internal/envplan/`, `internal/shell/` (activation hooks and the `claude`/`codex`
    launchers), `internal/hooks/` (shell profile blocks), `internal/gitcfg/`,
    `internal/doctor/`, `internal/setup/` and `internal/onboard/` (`tq setup`,
    `tq onboard`), `internal/migrate/`, `internal/bundle/`, `internal/providers/` (the
    embedded catalog), `internal/claudesettings/`, `internal/codexcfg/`,
    `internal/statusline/`, `internal/secret/` (`tq secret`, OS keychain via go-keyring),
    `internal/secrets/` (`tq secrets audit`), `internal/dotenv/`, `internal/commitscan/`
    (`tq/commit-secret`), `internal/audit/`, `internal/detect/`, `internal/verify/`,
    `internal/termopen/`, `internal/testutil/`.
- `pkg/setupapi/` — the setup plan API shared by `tq setup` and `tq onboard`.
- `test/integration/` — git identity end to end and the marketplace pin tests.
- `providers/` — a README; the provider catalog YAML lives in `internal/providers/catalog/`.
- `plugin/` — the Claude Code plugin (Python): hooks shell out to `tq claude-hook` for
  identity and secrets, plus the session memory, the identity preamble, the knowledge
  graph, 40 skills (plain checklists) and 5 read-only reviewer agents. Installed from this
  repo's root marketplace (`.claude-plugin/marketplace.json`, git-subdir `plugin/` pinned to
  a release tag and its sha, added after the tag in a post-release PR):
  `/plugin marketplace add tentaqles/tentaqles`, `/plugin install tentaqles@tentaqles`.
  Release steps in `docs/RELEASING.md`.
- `installers/`, `.goreleaser.yaml`, `.github/workflows/` — CLI release + CI. `test.yml`
  has four jobs: `go` (three OSes), `govulncheck`, `marketplace-pin`, `plugin`;
  `release.yml` runs on a `v*` tag.
- `scripts/spike-isolation.sh` — the macOS keychain-isolation spike behind the "Limitations"
  note of `README.md`.
- `docs/` — user-facing docs: `SETUP.md` (setup plans, `tq onboard`), `CLAUDE-HOOK.md`
  (the hook protocol, the identity rules, the secret rules, `tq dotenv run`),
  `CLAUDE-SETTINGS.md` (`tq settings render`, `tq statusline`, `tq worktrees`, Codex),
  `COMMANDS.md` (every command and skill, plus a test walkthrough), `PROVIDERS.md` (the
  catalog schema, `tq secret`), `MIGRATE.md` (`tq migrate`, the journal,
  `tq uninstall --restore`, the `TQ_ENABLED=0` kill switch, "Upgrading an existing install"),
  `RELEASING.md` (release PR, tag, release workflow, post-release PR pinning ref + sha),
  `IDEATION.md` (the isolation vision), `prd.md`, `threat-model.md`, `adr/`.

## Must not trade off
From `docs/threat-model.md`; each line names the test that keeps it.
- Identity drift blocks git/gh/cloud commands: `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`.
- Under `guard: v2` everything outside the identity's provider scope is denied in every
  tool (CLI by name, path, wrapper, `python -m`; variables; credential paths; API hosts; MCP
  servers; other identities' roots), before secrets, with a message naming the scope; the
  same identity's in-scope CLIs and ordinary dev commands pass; without the flag the v1
  rules are unchanged: `TestPreToolUse_ScopeReferenceScenario`, `TestPreToolUse_ScopeRuleOrder`,
  `TestPreToolUse_ScopeV1Unchanged`, `TestScope_OutOfScopeCLI`, `TestScope_OutOfScopeEnv`,
  `TestScope_OutOfScopePath`, `TestScope_OutOfScopeHost`, `TestScope_OutOfScopeMCP`,
  `TestScope_OtherIdentity`, `TestScope_InScopeWins`, `TestCatalogMatchLists`,
  `TestUserOverrideExtendsMatch`.
- Under `guard: v2` the scope is provider-specific (a shared CLI is owned per subcommand, a
  user file can only widen denial), a folder whose manifest or a provider file cannot be read
  denies every tool with `tq/guard-error`, and grep patterns / commit messages / here-strings
  stay text while real expansions are denied: `TestScope_EveryProvider` (all 62 providers),
  `TestScope_SharedCLIPrecision`, `TestScope_UserOverrideCannotAllow`,
  `TestScope_MCPExactAllowAndPrecedence`, `TestScope_Mutants`,
  `TestPreToolUse_UnreadableManifestDeniesAll`, `TestPreToolUse_BadProviderFileDeniesAll`,
  `TestPreToolUse_GuardMappingIsV1`.
- The secret rules fire on `.env` reads, environment dumps and secret-store reads, and a
  `.env.example` is allowed: `TestPreToolUse_SecretRules`, `TestBuiltinIsTheFiveSecretRules`.
- Trust is human-only, and so is every tq write under `$TQ_HOME` or an identity dir:
  `TestAllowAndDenyRefuseAgents`, `TestAddDoesNotTrustFromAgent`,
  `TestHumanOnlyCommandsRefuseWithMarkerAndLog`, `TestAddConfirmsBeforeWriting`, and the
  detector tests of `internal/humanonly` (`TestTTYDetector`, `TestProcessTreeDetector`,
  `TestEnvMarkerDetector`, `TestGateConfirms`), `TestHooksVerify_ReportExitAndAccept`.
- An edited manifest loses its trust: `TestEditedManifestLosesTrust`.
- A session cannot read or write tq's state, nor write the root marker, the installed
  plugin, the tq binary or tq's keychain entry, by any tool, nor run a human-only tq verb
  in the shell; the categories run blocked_commands → identity → secrets →
  self-protection and every deny is logged: `TestProtectedPaths_Table`,
  `TestProtectedPaths_RuleOrder`, `TestProtectedPaths_Codex`, `TestKeychainWrite`.
- The integrity record is signed and covers the hooks, the enforcing settings keys and the
  shims; a mismatch is at least as strict as untrusted (the hook denies mutating git and
  remote commands), withholds the identity and refuses the launch; a signed workspace never
  falls back to a legacy marker; only a human signs:
  `TestFlippedByteFailsVerification`, `TestPluginHookChangesAreNamed`,
  `TestPreToolUse_IntegrityMismatchDeniesRemoteAndGit`, `TestRun_RefusesOnIntegrityMismatch`,
  `TestEnv_NoIdentityAndOneNoticeOnMismatch`, `TestLegacyDowngradeIsUntrusted`,
  `TestSigningRefusesUnderAHarnessProcessTree`.
- The plugin hook never takes the enforcing binary from the environment:
  `test_tq_bin_is_not_honoured`, `test_tq_hook_never_runs_a_tq_from_the_plugin_folder`;
  Python never reads bytecode next to the plugin's sources:
  `test_a_forged_pyc_next_to_the_sources_is_not_loaded`.
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
- The hook has no ask and fails closed: a payload it cannot read, a guard panic, the
  deadline, an unreadable registry or manifest, and an incomplete commit scan all deny:
  `TestNoAskCodePath`, `TestNoAskVerdict`, `TestPreToolUse_MalformedPayloadDenies`,
  `TestPreToolUseGuardPanicDenies`, `TestPreToolUse_TimeoutDenies`,
  `TestPreToolUse_UnreadableRegistryDenies`, `TestPreToolUse_MissingOrBrokenManifestDenies`,
  `TestPreToolUseIncompleteScanDeniesBothAgents`, `TestPreToolUse_FieldTypesDeny`,
  `TestPreToolUse_GoneManifestWithEvidenceDenies`, `TestNoAskAnswerInAnyOutput`,
  `TestRunMainHookErrorsDeny`, `TestRunMainWatchdogDenies`, `TestCodexHookResolves`,
  `test_unreadable_payload_denies`, `test_tq_hook_turns_a_tq_without_decision_into_a_deny`,
  `test_tq_hook_stops_a_hanging_tq_and_denies`, `test_fallback_never_answers_ask`.
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
  `TestRenderWritesNoConfigKeys`, `TestLoadRecordsDroppedClaudeKeys`,
  `TestRenderKeepsADefaultModeTheEvidenceDoesNotName`, `TestRenderCorruptStateClaimsNothing`,
  `TestRenderMarkerVariants`, `TestNothingWrittenNamesAPermissionMode`; the Codex launcher
  adds only the full-bypass flag: `TestLaunchArgsInjectsTheBypassFlag`.

## Project config
CI's `go` job runs `go vet ./...`, `go test ./... -count=1` and a build; locally run
`go build ./...` too. The plugin suite is `PYTHONPATH=plugin python -m pytest plugin/tests -q`
(CI job `plugin`; about 13 minutes on Windows). The changelog is `CHANGELOG.md` under
`[Unreleased]`; a change under `plugin/` also gets its line in `plugin/CHANGELOG.md`, and the
root entry is then one pointer line.

A change under `internal/guard/`, `internal/policy/`, `internal/trust/`, `internal/humanonly/`,
`internal/markers/`, `internal/protect/`, `internal/hooks/`,
`plugin/hooks/`, `plugin/scripts/tq_hook.sh`, `.github/workflows/`,
`.goreleaser.yaml`, `.claude-plugin/marketplace.json` or `installers/` crosses a trust
boundary of `docs/threat-model.md` and needs an ADR under `docs/adr/` (template:
`plugin/templates/process/adr-0000-template.md`).

## Rules
- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test`/git via PowerShell; tests must isolate `TQ_HOME`, `HOME`, `USERPROFILE`.
- Language: en. Conventional commits.
