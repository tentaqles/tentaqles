# Threat model

Document status: draft    Updated: 2026-11-05    Cites: `docs/CLAUDE-HOOK.md`, `docs/RELEASING.md`, `docs/adr/0001-guard-narrows-to-identity-and-secrets.md`, `docs/adr/0002-protected-manifest-location.md`, `docs/adr/0003-human-only-detection.md`, `docs/adr/0004-signed-integrity-record.md`, `docs/adr/0005-self-protection-category.md`
Reviewed by: the repo owner    Next review: 2027-02-01, or on every new trust boundary

Four questions, answered per trust boundary (Shostack). The requirements live in
`docs/prd.md`; planned mitigations are listed under [Planned work](#planned-work) as
`TM-n` items with acceptance checks, and mitigation rows cite those ids.

## 1. What are we building

`tq` is a CLI that keeps each client's identity (git, gh, cloud, Claude and Codex config
homes) separate, and blocks a command that a coding agent (Claude Code, Codex) runs under
the wrong identity or that would put a secret into the transcript or a commit. It ships
with a Claude Code plugin whose hooks call `tq claude-hook`; the plugin also keeps the
session memory, the identity preamble, the knowledge graph and the skills. Everything runs
on the developer's machine as the developer's OS user; the only remote party is GitHub
(code, releases). By the owner's decision of 2026-11-03 (`docs/prd.md` D-29 to D-31) the
hook allows or blocks and never asks, and tq sets no permission mode of its own beyond the
launcher's skip: anything beyond identity and secrets is the harness's own business.

**Who we defend against.** The main adversary is the agent itself: it follows prompts
and file content it did not write, so it can be steered (prompt injection) or simply
make a mistake. The second is the supply chain: what reaches the machine from GitHub.
Out of scope: any program already running as the developer's OS user
([AR1](#accepted-risks)), a determined adversary rewording shell commands to get past
the text guard, and every action the owner left to the harness's permission settings
([AR2](#accepted-risks)).

| Boundary | From | To | Data crossing (classes) | Authentication |
|---|---|---|---|---|
| B1 | agent session | tq hook (`tq claude-hook pre-tool-use`) and the state folder (`$TQ_HOME`) | commands, file paths, file content; may carry secrets | none: every tool call passes through the PreToolUse hook |
| B2 | human | tq human-only actions: every command that writes under `$TQ_HOME` or an identity config dir (`tq init`, `tq add`, `tq allow`, `tq deny`, `tq hooks install|remove`, `tq settings render` (not `--dry-run`), `tq bundle sync|capture`, `tq migrate --apply`, `tq uninstall`\|remove`, `tq settings render`, `tq bundle sync\|capture`, `tq migrate`, `tq uninstall`, `tq setup`, `tq onboard`, `tq secret set\|rm`, `tq providers add`, `tq login`, `tq run --off`, `tq worktrees prune`) | trust decisions, keychain tokens, the registry, profiles, identity settings, the catalog | three detectors in `internal/humanonly` (a terminal on stdin and stdout, no harness in the process tree, no harness environment marker), then a typed `yes` read from the controlling terminal before the first write (ADR-0003) |
| B8 | GitHub releases and repo | the user's machine (installers, tq binary, plugin) | executables, hooks | HTTPS; checksums and build provenance; plugin pinned by tag and commit sha |
| B9 | contributors and CI | the repo and its releases | code, workflows, dependencies | GitHub account, rulesets |

Assets worth protecting: each client's identity and credentials (cross-client leaks are
the worst outcome), secrets in `.env` files, credential stores and the keychain, the signed integrity records
under `$TQ_HOME/trust/` and their key (the keychain entry `tentaqles/integrity-key`, or the
fallback file `$TQ_HOME/integrity.key`; ADR-0004), the manifests under
`$TQ_HOME/identities/<ws>/` (ADR-0002), the hook files the records cover (the installed
plugin's `hooks/` and `scripts/`, the enforcing keys of each identity's Claude
`settings.json`, its Codex hook files, the shims), and the integrity of what users install.

## 2. What can go wrong

### B1 agent -> tq hook and the state folder
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | An agent runs `git push` or a cloud command while the shell is set to another client's identity | `guard.Decide` (`internal/guard/guard.go`) blocks on git email, gh user or cloud drift with exit 2; `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`, `TestInlineIdentityOverride`, `TestPreToolUse_SecretRules` (the git email drift case). Rewordings (`env git`, `sh -c`, aliases) are listed in `docs/CLAUDE-HOOK.md`; per-workspace config folders are the real separation | done | tq |
| Tampering | An agent writes a trust record by hand, or has tq sign one through any command | a record written with a file tool does not count: it is signed with a key in the OS keychain (ADR-0004), and a flipped byte fails the signature ("integrity record signature invalid"); `TestFlippedByteFailsVerification`, `TestOnlyValuesAreSigned`, `TestTrustIsKeyedByWorkspaceName`. Every signature passes the human-only check of ADR-0003 inside `internal/trust` (`trust.HumanCheck`), whatever command reaches it, so `env -u CLAUDECODE tq add` under a harness signs nothing; `TestSigningRefusesWithoutAHuman`, `TestSigningRefusesUnderAHarnessProcessTree`, `TestHooksVerify_ReportExitAndAccept`. The fallback key file, when the keychain is unavailable, is readable by any same-user program (AR1) and reported by `tq doctor` (`integrity-key-file`; `TestRun_IntegrityKeyFileWarns`) | done | tq |
| Tampering | An agent downgrades a signed workspace to a legacy trust marker (deletes `trust/<ws>.json` and plants `trust/<hash>`) | a marker counts only while the signed snapshot `trust/legacy.json` taken at the first signature lists it, and never for a workspace with a signed record; `tq deny` leaves a signed tombstone instead of deleting; a missing or forged snapshot makes no marker count; `TestLegacyMarkersCountOnlyFromTheSnapshot`, `TestDenyLeavesASignedTombstone`, `TestLegacyDowngradeIsUntrusted`, `TestManifests_RekeysLegacyTrustRecords` (a planted marker is never turned into a signed record) | done | tq |
| Tampering | An agent replaces the integrity key in the keychain and signs records with it | not prevented: any same-user process can write the keychain entry (AR1). The P1.3 hook rule denies, in a session, the `cmdkey`, `security add-generic-password` and `secret-tool` lines that name the `tentaqles` service; a key swapped without re-signing every record reports "integrity key changed (signed with key X, the key now is Y)", and `tq hooks verify` prints the key id; `TestKeyChangeIsReportedByID` | accepted (AR1); the command-line rule is P1.3 | tq |
| Tampering | An agent edits the plugin's hook files (`hooks/hooks.json`, `scripts/tq_hook.sh`) or the hooks, plugins, MCP switches, `env` or status line of the identity's Claude `settings.json` so the pre-tool hook no longer calls tq, or swaps the Codex hook files or a shim | the signed integrity record covers them (`trust.Covered`, keys canonical so another spelling of a path is no escape): every command and the hook verify it through `resolve`, once per command; a mismatch is at least as strict as untrusted: the hook refuses every mutating git and every remote command with the reason naming the first changed file, `tq env` withholds the identity and prints a fixed line at every prompt, `tq run <ws>` and `tq run .` launch nothing, and the session-start preamble and `tq doctor` report `integrity` and `untrusted`; only a human re-signs, after reading `tq hooks verify`; `TestPluginHookChangesAreNamed`, `TestSettingsEnforcingKeysOnly`, `TestAbsentCoveredFileAppearingIsAChange`, `TestShimsAreCovered`, `TestKeysAreCanonical`, `TestPreToolUse_IntegrityMismatchDeniesRemoteAndGit`, `TestRun_RefusesOnIntegrityMismatch`, `TestEnv_NoIdentityAndOneNoticeOnMismatch`, `TestSessionStart_PrintsIntegrityMismatch`, `TestRun_IntegrityListsTheChangedFiles`. A move by `tq migrate manifests` re-signs only a record that verifies in full: `TestManifests_DoesNotResignAFailingRecord` | done | tq |
| Tampering | A file path or folder name in the per-prompt line injects shell code | the line `tq env` emits is fixed words plus the workspace name only when it matches `workspace.NameRe`; no path, manifest value or unvalidated name reaches it; `TestNoticeNeverCarriesAnUnvalidatedName`, `TestEmit_NoticeLinePerShell` | done | tq |
| Tampering | An agent edits a trusted manifest to add a cloud or a blocked command exemption | the manifest's hash is in the signed integrity record (`internal/trust`, ADR-0004): an edit makes the workspace untrusted, with the manifest named as the changed file, until a human signs again (`tq allow`, `tq hooks verify --accept`); a repository manifest cannot add clouds; `TestEditedManifestLosesTrust`, `TestEditedManifestIsNamed`, `TestPreToolUse_UntrustedManifestCannotAddClouds`. The manifest tq reads lives under `$TQ_HOME/identities/<ws>/manifest.yaml`, outside the folder the session works in, and the workspace root holds a name-only marker (ADR-0002); a marker that names another workspace resolves neutral; `TestResolve_MarkerResolvesToProtectedManifest`, `TestResolve_MarkerClientMismatch_Neutral`. A trusted manifest copied into another folder (the replay) resolves neutral because its `client` is not the folder name, and the trust record is keyed by workspace name too (`trust/<ws>/<hash>`); `TestResolve_ReplayOfATrustedManifest_Neutral`, `TestTrustIsKeyedByWorkspaceName`. A symlink at the root file is neutral: `TestResolve_RootFileSymlink_Neutral`. An identity not yet migrated keeps its manifest in the root until the human runs `tq migrate manifests` (`tq doctor`: `manifest-in-root`) | done | tq |
| Spoofing | An agent points tq at a forged state folder through `TQ_HOME` (the hook inherits the harness environment; a workspace's settings `env` block is agent-writable) | under an agent marker `paths.Home()` and the plugin's `tentaqles.tqhome` ignore `TQ_HOME` and read `~/.tentaqles`; `tq doctor` warns `tq-home-override`; `TestHome_IgnoresTQHomeUnderAnAgentMarker`, `test_tq_home_is_ignored_under_an_agent_marker` | done | tq |
| Tampering | An agent trusts an untrusted folder itself, scaffolds one, or rewrites a profile, an identity `settings.json`, the catalog, a provider file or the keychain through tq | every tq command that writes under `$TQ_HOME` or an identity config dir is human-only (`internal/humanonly`, ADR-0003): it refuses when stdin or stdout is not a terminal, when a harness is in the process tree or when a harness marker is set, logs the refusal to `$TQ_HOME/denials.jsonl`, and otherwise asks the human to type `yes` before the first write; `TestHumanOnlyCommandsRefuseWithMarkerAndLog`, `TestHumanOnlyCommandsRefuseWithoutTTYOrUnderAHarnessProcess`, `TestAllowAndDenyRefuseAgents`, `TestAddDoesNotTrustFromAgent`, `TestAddConfirmsBeforeWriting`. The hook also denies the human-only verbs in the shell before they run (`tq/human-only-command`, ADR-0005; `TestHumanOnlyInvocation`, `TestProtectedPaths_Table`) | done | tq |
| Tampering | An agent writes tq's state directly, not through tq: a trust record, the protected manifest, `config.yaml`, an identity's `settings.json` or Codex `hooks.json`, the plugin's `hooks.json` or `tq_hook.sh`, the root marker, the tq binary, the integrity key or tq's keychain entry, with a file tool, an MCP server, Codex's `apply_patch` or the shell (a redirect, a copy, a move, a delete, an ACL or mode change, a link, `sed -i`, an interpreter's inline code, `git -C`, a `cd` into the folder) | the self-protection category of the hook (`internal/policy/selfprotect.go`, `internal/protect/pathset.go`, ADR-0005): `tq/protected-path` denies every write to a protected path, canonicalised (absolute, symlinks resolved, case-insensitive on Windows and macOS, `~` and the home variables expanded); `TestProtectedPaths_Table` (one case per write route and per path kind), `TestProtectedPaths_Codex`, `TestProtectedPaths_MCPInput`, `TestProtectedPaths_OtherWorkspaceAndNeutralCwd`, `TestKeychainWrite`, `TestMatchKinds`, `TestNormalizeExpandsAndPlaces`, `TestSelfProtectionShellAndFileTools`; with tq missing, `plugin/scripts/identity-guard.py` denies a file tool or a shell command under `~/.tentaqles` or the plugin cache (`test_fallback_denies_protected_paths`). A path built at run time (a script, an alias) gets past the text rules (AR2); the signed integrity record (P1.4) detects the change | done | tq |
| Information disclosure | An agent reads the protected manifest, the trust store, the registry, an identity's settings or the denial log into the transcript | `tq/protected-path` denies a Read, Grep or shell read (`cat`, `type`, `Get-Content`, `grep`, a `<` redirect, an interpreter's inline code) of tq's state; the root marker, the plugin's files and the binary stay readable; `TestProtectedPaths_Table` | done | tq |
| Repudiation | An agent's blocked actions leave no trace | every hook deny of every category is one line in `$TQ_HOME/denials.jsonl` (category, layer, tool, command or path, rule, reason, workspace, pid, cwd; never an environment value or file content), and identity switches go to `audit.jsonl` (`internal/audit`); `TestProtectedPaths_Table`, `TestProtectedPaths_RuleOrder`, `TestDenyPrefixNamesTheCategory`. The log is a protected path for a session; a same-user process can rewrite it (AR1) | done; same-user writes accepted (AR1) | tq |
| Information disclosure | An agent reads a `.env` file, a credential store or prints the environment into the transcript | the five secret rules of `internal/policy/builtin.go` deny the call; `tq dotenv run` masks values in output (`internal/dotenv`); `TestPreToolUse_SecretRules`, `TestBuiltinDecisions`, `TestPolicy_ReadEnvDenied_WindowsPath`, `TestDotenvRunLoadsAndMasks` | done | tq |
| Information disclosure | A commit records a `.env`, a private key or a secret-shaped value | `tq/commit-secret` scans what the commit would record, the working tree included when the command also stages (`internal/commitscan`); `TestPolicy_CommitWithSecretDenied`, `TestAdded_StagedAndChainedAdd` | done | tq |
| Information disclosure | A session reaches another client's MCP server | `tq/cross-client-mcp` denies an MCP server that another client's bundle declares and this workspace's does not; `TestPolicy_CrossClientMCPDenied`, `TestMCPServer` | done | tq |
| Denial of service | A hook hangs and stalls every tool call | hook timeouts in `plugin/hooks/hooks.json`, a bounded gh lookup, a deadline on the commit scan (`TestScanCtx_DeadlineIsAnError`). A hook timeout or a tq parse error lets the call through (AR2) | done | tq |
| Elevation of privilege | A guard bug panics and the command runs unchecked | `guardPanicAsk` turns a panic into an ask (a deny in Codex), rule id `tq/guard-error`; `TestPreToolUseGuardPanicAsks`, `TestPreToolUseAgentCodexNeverAsks` | done | tq |
| Elevation of privilege | An agent force-pushes, deletes a cloud resource, applies infrastructure or widens its own settings | not tq's: by the owner's decision (D-30, D-31) these are the harness's permission settings' business; `TestPreToolUse_SecretRules` keeps `git push --force` and `terraform apply` untouched by tq | accepted (AR2) | owner |

### B2 human -> tq human-only actions
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | An agent runs a human-only command itself, from a shell that lost the marker, a headless child, a subagent, or an orphaned child of a harness that exited | each refuses when stdin or stdout is not a terminal, when `claude`, `codex`, `opencode`, `kimi` or a shell or runtime (`node`, `bun`, `deno`, `npx`, `bash`, `pwsh`, `cmd`, ...) whose command line names one is in the process tree, when the chain cannot be followed to a terminal, a login shell or the desktop shell (a parent that exited, a reused pid, an unreadable command line, a root reached through services only), or when a harness marker is set, with no prompt (`internal/humanonly`, ADR-0003); `TestTTYDetector`, `TestProcessTreeDetector`, `TestEnvMarkerDetector`, `TestWalkRefusesAnOrphanedChain`, `TestWalkRefusesAReusedPid`, `TestAllowAndDenyRefuseAgents`, `TestSecretIsHumanOnly`, `TestRunOffRefusesAgents`, `TestOnboard_InteractiveRefusedInAgentSession`, `TestOnboard_FromRefusedInAgentSession`. The three detectors stop a harness and its children; a same-user program driving a pseudo-terminal with a scrubbed environment from outside the harness tree is not stopped, nor is a harness tq does not know by name: the protected-path rules (ADR-0005) deny what a session could change through its tools, and the signed integrity record (P1.4) detects what such a program changes | done; P1.4 follows | tq |
| Spoofing | A piped `yes` answers the confirmation for the human | the confirmation is read from the controlling terminal (`CONIN$`, `/dev/tty`), never from stdin, and a piped stdin already fails the first detector; `TestGateConfirms`, `TestAddConfirmsBeforeWriting`, `TestSetup_YesDoesNotSkipConfirm` | done | tq |
| Tampering | A trusted manifest, or a hook file the record covers, is edited after `tq allow` | the signed record names the file; a human reads `tq hooks verify` (with the plugin upgrade summary first) and confirms before `--accept` signs; `TestEditedManifestLosesTrust`, `TestEditedManifestIsNamed`, `TestHashChangesWhenFileChanges`, `TestHooksVerify_ReportExitAndAccept`, `TestHooksVerify_SummarisesAPluginUpgrade` | done | tq |
| Repudiation | A refused human-only command leaves no trace | every refusal is one line in `$TQ_HOME/denials.jsonl` (time, command, detector and reason, pid, cwd; never an environment value); `TestRefusalIsLogged`. A same-user process can rewrite the file (AR1) | done | tq |
| Information disclosure | A keychain token is printed by `tq secret` | `tq secret status` prints present or absent, never a value; `set` reads from a hidden prompt or stdin; `TestSecretSetStatusRm`, `TestSecretSetHiddenPromptOnATerminal` | done | tq |
| Denial of service | (skipped) a human can always run tq directly | n/a | n/a | n/a |
| Elevation of privilege | A trusted manifest widens what the hook allows (`cloud.also`, `claude.permission_mode: bypass`) | the manifest is the human's own choice, hash-trusted by `tq allow`; `cloud.also` is validated on load and only a trusted manifest's list counts; `TestExtraClouds`, `TestPreToolUse_UntrustedManifestCannotAddClouds` | done | tq |

### B8 GitHub -> the user's machine
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | A swapped release archive is installed as tq | `installers/install.ps1` and `install.sh` check the archive's SHA-256 against the release's `checksums.txt` and refuse a mismatch or a missing entry (TM-1) | done | tq |
| Spoofing | A release is built by something other than this repo's workflow | the release workflow attests build provenance for every archive in `checksums.txt` (`.github/workflows/release.yml`); `install.ps1` and `install.sh` run `gh attestation verify` with the release workflow as signer when `gh` is logged in, and refuse on failure (TM-2) | done | tq |
| Tampering | A moved or re-pushed tag changes what the plugin installs | the root marketplace pins tag and commit sha, CI checks the tag resolves to it, tags `v*` cannot be updated or deleted (ruleset); `TestRootMarketplacePin`, CI job `marketplace-pin` | done | tq |
| Tampering | A bundle catalog entry installs a plugin over `http://`, `git://`, `file://` or `ext::` | refused unless `insecure: true` is set on that entry (`internal/bundle/marketplace_check.go`); `TestValidate_PlainTextTransport`, `TestValidate_GitURLTransport` | done | tq |
| Repudiation | (skipped) releases are tied to tags and workflow runs on GitHub | n/a | n/a | n/a |
| Information disclosure | A catalog URL with credentials ends up in `settings.json` | warned on every check, never printed in drift output; `TestValidate_URLCredentials`, `TestDescribeSource_StripsUserinfo` | done | tq |
| Denial of service | (skipped) GitHub availability | n/a | n/a | n/a |
| Elevation of privilege | An environment variable or PATH points the plugin hook at a fake tq | `tq_hook.sh` takes no binary from the environment: `TQ_BIN` is not honoured (`test_tq_bin_is_not_honoured`), the order is the plugin's `bin/tq` (a covered file of the signed record when the plugin bundles one), PATH, then the well-known install dirs. PATH and the tq binary itself are the user's own and the OS's (AR1); Phase 3's shim folder decides which tq an identity shell runs. If tq is missing the hook falls back to `plugin/scripts/identity-guard.py`, then blocks | done | plugin |

### B9 contributors and CI -> the repo and releases
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | (skipped) one maintainer; GitHub account security | n/a | n/a | n/a |
| Tampering | A third-party action changes under its tag and alters a release built with a write token | every `uses:` in `.github/workflows/` names a commit sha with the version in a comment (TM-4); Dependabot proposes updates | done | tq |
| Tampering | A change merges to `main` with failing tests | the ruleset requires the six CI checks on `main` (`go` on three OSes, `govulncheck`, `marketplace-pin`, `plugin`) | done | tq |
| Tampering | A fork PR runs with secrets | CI uses `pull_request` with `contents: read`, never `pull_request_target` (`.github/workflows/test.yml`) | done | tq |
| Repudiation | (skipped) git history and PRs | n/a | n/a | n/a |
| Information disclosure | (skipped) release secrets are deploy keys scoped to the tap and bucket repos | n/a | n/a | n/a |
| Denial of service | (skipped) n/a for a local tool | n/a | n/a | n/a |
| Elevation of privilege | A dependency with a known vulnerability ships in tq | CI job `govulncheck` (`.github/workflows/test.yml`) and weekly Dependabot updates for Go modules and actions (`.github/dependabot.yml`) (TM-5) | done | tq |

## 3. What will we do about it

Every `done` row cites its test or the code that keeps it. AR1 and AR2 cover the rows whose
remaining gap is a same-user program, a reworded command, or an action the owner left to
the harness.

### Planned work

- **TM-1 Installers check checksums: done.** `install.ps1` and `install.sh` download
  `checksums.txt` from the same release and refuse to install when the archive's SHA-256
  is missing or different. Checks: a tampered archive fails the install with a clear
  message; a matching one installs.
- **TM-2 Signed build provenance: done.** The release workflow attests every archive
  (`actions/attest-build-provenance`); the installers verify it when
  `gh` is logged in. Without `gh` the checksum is the only check.
- **TM-4 Pin GitHub Actions by commit sha: done** in every workflow, with the version in a
  comment. Checks: no `uses:` line names a tag or branch.
- **TM-5 Dependency scanning: done.** A `govulncheck` CI job and `.github/dependabot.yml` for
  Go modules and GitHub Actions. Checks: the job runs on every PR.

## 4. Did we do a good job
- Every boundary in §1 has a table in §2: yes
- Every high-risk row is done, planned as a TM item, or accepted below: yes
- The `security-reviewer` checklist references this file: yes (`plugin/agents/security-reviewer.md` reads `docs/threat-model.md`)
- Last breaker or pentest run: none against tq itself

## Accepted risks
Each accepted risk expires; an expired risk is reopened as a decision for the repo owner.

| Risk | Why accepted | Accepted by | Date | Expires |
|---|---|---|---|---|
| AR1: a program running as the developer's OS user can read tq's files and keychain entries, read or replace the integrity key and so sign a record (ADR-0004; a key swapped without re-signing reports "integrity key changed"), write its logs directly, drive a pty past the human-only check (an anchored chain through `tmux`, `screen`, `conhost` or `wsl -e`, ADR-0003), and redirect the hook through PATH. `TQ_BIN` is no longer honoured by the hook (`test_tq_bin_is_not_honoured`). Pointing tq at a forged state folder or log through `TQ_HOME` is no longer in this row: under an agent marker `TQ_HOME` is ignored (ADR-0002, `TestHome_IgnoresTQHomeUnderAnAgentMarker`) | tq cannot defend against code already running as the user; that is the job of the OS, antivirus and not running untrusted programs. The protected-path rules (ADR-0005) keep a session's tools off tq's state; P1.4 (signed record) detects what such a program changes | the repo owner | 2026-10-30 | 2027-02-01 |
| AR2: the identity guard, the secret rules and the self-protection rules are text filters: a reworded command (escapes, variables tq does not expand, a script or an alias that builds the path at run time) can get past them, and a hook timeout or tq parse error lets a call through. With no confirmations left, a wrong `git push --force`, a cloud delete or a `terraform apply` is the harness's own permission settings' business, by the owner's decision (D-30, D-31) | the rules catch honest mistakes and prompt-injected slips, not a determined adversary; chasing every bypass added false positives and complexity, and the confirmations cost more than they protected. Real protection is the per-workspace config homes and the harness's own sandbox and permission settings | the repo owner | 2026-11-03 | 2027-02-01 |

## Decisions

- **2026-11-05, self-protection category (repo owner, PRD v2 §6.3, §7.1, §8.2).**
  The hook has three categories, evaluated `blocked_commands` → identity →
  secrets → self-protection, first match denies, and every deny reads
  `BLOCKED [<category>/<rule>]` and is one line in `$TQ_HOME/denials.jsonl`.
  Self-protection is two rules: `tq/protected-path` (no read or write of tq's
  state, no write of the root marker, the installed plugin, the tq binary or
  tq's keychain entry) and `tq/human-only-command` (no human-only tq verb in
  the shell). Dry runs are reads and free (`tq migrate`, `tq settings render
  --dry-run`). ADR-0005.
- **2026-11-05, human-only commands (repo owner, PRD v2 §8.1).** Every tq command that
  writes under `$TQ_HOME` or an identity config dir is human-only: three detectors (a
  terminal on stdin and stdout, no harness in the process tree, no harness marker), a
  refusal logged to `$TQ_HOME/denials.jsonl`, and a typed `yes` from the controlling
  terminal before the first write. `--yes` never skips that confirmation. `tq add`,
  `tq setup --from` and `tq onboard --from` refuse a session instead of scaffolding an
  untrusted folder. `CLAUDE_CONFIG_DIR` and `CODEX_HOME` are not markers. ADR-0003.
- **2026-11-03, scope (repo owner).** tentaqles is identity isolation plus secrets
  protection and nothing else (`docs/prd.md` D-29 to D-31; ADR-0001). The hook allows or
  blocks and never asks; `tq allow` and `tq deny` refuse an agent session by its
  environment marker, not by a hook rule. Boundary ids are B1, B2, B8 and B9; the other
  numbers are not used.

## Assumptions
- The PRD is `docs/prd.md` (FR-10 to FR-13 for the hook and trust, FR-14 to FR-16 for
  secrets); `TM-n` items here are the mitigation checklist.
- Expiry of accepted risks defaults to 90 days.
- The same-user boundary (AR1) is the line between in-scope and out-of-scope threats.
