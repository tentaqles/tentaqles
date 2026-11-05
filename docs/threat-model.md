# Threat model

Document status: draft    Updated: 2026-11-05    Cites: `docs/CLAUDE-HOOK.md`, `docs/RELEASING.md`, `docs/adr/0001-guard-narrows-to-identity-and-secrets.md`, `docs/adr/0002-protected-manifest-location.md`
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
| B2 | human | tq human-only actions (`tq allow`, `tq deny`, `tq secret` writes, `tq run --off`, the `tq onboard` wizard, logins) | trust decisions, keychain tokens | no agent environment marker (`humanonly.go`); a terminal on stdin for the hidden prompt and the wizard |
| B8 | GitHub releases and repo | the user's machine (installers, tq binary, plugin) | executables, hooks | HTTPS; checksums and build provenance; plugin pinned by tag and commit sha |
| B9 | contributors and CI | the repo and its releases | code, workflows, dependencies | GitHub account, rulesets |

Assets worth protecting: each client's identity and credentials (cross-client leaks are
the worst outcome), secrets in `.env` files, credential stores and the keychain, the trust
markers under `$TQ_HOME/trust/`, the manifests under `$TQ_HOME/identities/<ws>/`
(ADR-0002), and the integrity of what users install.

## 2. What can go wrong

### B1 agent -> tq hook and the state folder
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | An agent runs `git push` or a cloud command while the shell is set to another client's identity | `guard.Decide` (`internal/guard/guard.go`) blocks on git email, gh user or cloud drift with exit 2; `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`, `TestInlineIdentityOverride`, `TestPreToolUse_SecretRules` (the git email drift case). Rewordings (`env git`, `sh -c`, aliases) are listed in `docs/CLAUDE-HOOK.md`; per-workspace config folders are the real separation | done | tq |
| Tampering | An agent trusts an untrusted folder itself, or scaffolds one as trusted | `tq allow` and `tq deny` refuse an agent session by its environment marker (`internal/cmd/humanonly.go`); `tq add` and `tq setup` leave a new folder untrusted from a session; `TestAllowAndDenyRefuseAgents`, `TestAddDoesNotTrustFromAgent`. Writing the marker file under `$TQ_HOME/trust/` with a file tool is not caught: no rule guards the state folder any more (AR2, D-31) | done; the state folder is AR2 | tq |
| Tampering | An agent edits a trusted manifest to add a cloud or a blocked command exemption | trust is by file hash (`internal/trust`): an edit makes the workspace untrusted until a human runs `tq allow`; a repository manifest cannot add clouds; `TestEditedManifestLosesTrust`, `TestPreToolUse_UntrustedManifestCannotAddClouds`. The manifest tq reads lives under `$TQ_HOME/identities/<ws>/manifest.yaml`, outside the folder the session works in, and the workspace root holds a name-only marker (ADR-0002); a marker that names another workspace resolves neutral; `TestResolve_MarkerResolvesToProtectedManifest`, `TestResolve_MarkerClientMismatch_Neutral`. An identity not yet migrated keeps its manifest in the root until the human runs `tq migrate manifests` (`tq doctor`: `manifest-in-root`) | done | tq |
| Repudiation | An agent's blocked actions leave no trace | the block reason goes to the transcript (stderr, exit 2) and identity switches to `audit.jsonl` (`internal/audit`); hook decisions are not logged by tq, and a same-user process can rewrite the audit file (AR1) | accepted (AR1) | tq |
| Information disclosure | An agent reads a `.env` file, a credential store or prints the environment into the transcript | the five secret rules of `internal/policy/builtin.go` deny the call; `tq dotenv run` masks values in output (`internal/dotenv`); `TestPreToolUse_SecretRules`, `TestBuiltinDecisions`, `TestPolicy_ReadEnvDenied_WindowsPath`, `TestDotenvRunLoadsAndMasks` | done | tq |
| Information disclosure | A commit records a `.env`, a private key or a secret-shaped value | `tq/commit-secret` scans what the commit would record, the working tree included when the command also stages (`internal/commitscan`); `TestPolicy_CommitWithSecretDenied`, `TestAdded_StagedAndChainedAdd` | done | tq |
| Information disclosure | A session reaches another client's MCP server | `tq/cross-client-mcp` denies an MCP server that another client's bundle declares and this workspace's does not; `TestPolicy_CrossClientMCPDenied`, `TestMCPServer` | done | tq |
| Denial of service | A hook hangs and stalls every tool call | hook timeouts in `plugin/hooks/hooks.json`, a bounded gh lookup, a deadline on the commit scan (`TestScanCtx_DeadlineIsAnError`). A hook timeout or a tq parse error lets the call through (AR2) | done | tq |
| Elevation of privilege | A guard bug panics and the command runs unchecked | `guardPanicAsk` turns a panic into an ask (a deny in Codex), rule id `tq/guard-error`; `TestPreToolUseGuardPanicAsks`, `TestPreToolUseAgentCodexNeverAsks` | done | tq |
| Elevation of privilege | An agent force-pushes, deletes a cloud resource, applies infrastructure or widens its own settings | not tq's: by the owner's decision (D-30, D-31) these are the harness's permission settings' business; `TestPreToolUse_SecretRules` keeps `git push --force` and `terraform apply` untouched by tq | accepted (AR2) | owner |

### B2 human -> tq human-only actions
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | An agent runs `tq allow`, `tq deny`, `tq secret set`, `tq run --off` or the `tq onboard` wizard itself | each refuses when the agent environment marker is set, with no prompt (`internal/cmd/humanonly.go`); `TestAllowAndDenyRefuseAgents`, `TestSecretIsHumanOnly`, `TestRunOffRefusesAgents`, `TestOnboard_InteractiveRefusedInAgentSession`. A pty driven by a same-user program with the marker stripped passes (AR1) | done | tq |
| Tampering | A trusted manifest is edited after `tq allow` | trust is by file hash; `TestEditedManifestLosesTrust`, `TestHashChangesWhenFileChanges` | done | tq |
| Repudiation | (skipped) a human acts in their own terminal; trust markers carry their mtime | n/a | n/a | n/a |
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
| Elevation of privilege | `TQ_BIN` or PATH points the plugin hook at a fake tq | the environment is the user's own (AR1); if tq is missing the hook falls back to `plugin/scripts/identity-guard.py`, then blocks (`plugin/scripts/tq_hook.sh`) | done | plugin |

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
| AR1: a program running as the developer's OS user can read tq's files and keychain entries, write its trust markers and logs, drive a pty past the human-only check, and redirect the hook through `TQ_BIN` or PATH | tq cannot defend against code already running as the user; that is the job of the OS, antivirus and not running untrusted programs | the repo owner | 2026-10-30 | 2027-02-01 |
| AR2: the identity guard and the secret rules are text filters: a reworded command (globs, escapes, variables, interpreters, links) can get past them, a hook timeout or tq parse error lets a call through, and nothing guards the state folder. With no confirmations left, a wrong `git push --force`, a cloud delete or a `terraform apply` is the harness's own permission settings' business, by the owner's decision (D-30, D-31) | the rules catch honest mistakes and prompt-injected slips, not a determined adversary; chasing every bypass added false positives and complexity, and the confirmations cost more than they protected. Real protection is the per-workspace config homes and the harness's own sandbox and permission settings | the repo owner | 2026-11-03 | 2027-02-01 |

## Decisions

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
