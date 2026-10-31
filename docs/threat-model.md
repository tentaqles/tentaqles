# Threat model

Document status: draft    Updated: 2026-10-31    Cites: `docs/GUARD-SCOPE.md`, `docs/CLAUDE-HOOK.md`, `docs/packs/framework-security.md`, `docs/HUB.md`, `docs/DECIDE.md`, `docs/RELEASING.md`
Reviewed by: the repo owner    Next review: 2027-01-28, or on every new trust boundary

Four questions, answered per trust boundary (Shostack). The requirements live in
`docs/prd.md`; planned mitigations are listed under [Planned work](#planned-work) as
`TM-n` items with acceptance checks, mitigation rows cite those ids, and the PRD's
backlog rows (`B-nn`) name the work that closes a planned row.

## 1. What are we building

`tq` is a CLI that keeps each client's identity (git, gh, cloud, Claude config) separate,
and guards what a coding agent (Claude Code, Codex) may run while it works for that
client. It ships with a Claude Code plugin whose hooks call `tq claude-hook`, a gated
build engine (`/tentaqles:build`) whose approvals are signed, pack checks and approvals
for data, ml, cloud and db work, a local board (`tq hub`) with a web page and headless
agent runs, and sync to issue trackers. Everything runs on the developer's machine as
the developer's OS user; the only remote parties are GitHub (code, releases), the
trackers, and the `tq decide` model endpoint.

**Who we defend against.** The main adversary is the agent itself: it follows prompts
and file content it did not write, so it can be steered (prompt injection) or simply
make a mistake. The second is the supply chain: what reaches the machine from GitHub.
Out of scope: any program already running as the developer's OS user
([AR1](#accepted-risks)), and a determined adversary rewording shell commands to get
past the text guard ([AR2](#accepted-risks)).

| Boundary | From | To | Data crossing (classes) | Authentication |
|---|---|---|---|---|
| B1 | agent session | tq hooks and the state folder (`$TQ_HOME`) | commands, file paths, file content; may carry secrets and personal data | none: every tool call passes through the PreToolUse hook |
| B2 | human | tq terminal-only actions (mode change, `tq allow|guard|unattended`, logins, `tq secret` writes, LAN bind, `tq hub key|token|serve-key`) | approval and trust decisions | class `terminal` (no agent process above), terminal on stdin and stdout, typed one-time code (build gate) |
| B3 | plugin (Python) | build-gate records (`$TQ_HOME/build-gate`) | approvals, sessions, generations, test evidence | HMAC-SHA256, key in `build-gate/hmac.key` |
| B4 | pack checks | external tools (dbt, ruff, checkov, ...) | repo files, filtered environment | none: tq picks the binary |
| B5 | browser or local process | hub web API (`127.0.0.1:7470`) | board cards, decisions, budgets | Host allow-list, Origin checks, token or `LocalKey` |
| B6 | hub runner | headless agent run (`claude -p`, class `run`) | prompts, allowed tools, budgets | run settings file; `TENTAQLES_HEADLESS=1` and `TQ_HUB_TASK` force class `run`; build tokens dropped from the environment |
| B7 | tq | remote services: trackers (Jira, Linear, Azure DevOps) and the `tq decide` endpoint | card text, decision state (redacted) | tracker token from the credential chain; decide API key |
| B8 | GitHub releases and repo | the user's machine (installers, tq binary, desktop app, plugin) | executables, hooks | HTTPS; plugin pinned by tag and commit sha |
| B9 | contributors and CI | the repo and its releases | code, workflows, dependencies | GitHub account, rulesets |
| B10 | console session (the human's interactive top-level `claude`, class `console`) | hub board actions through the CLI client (answer, run, move, edit, cancel, push, sync retry, pack approvals) | decision answers, lane moves, card bodies and tags | class computed by tq from environment plus process ancestry, never from a flag or header alone; actor `console:<os user>` plus the Claude session id; the human's choice in a multiple-choice question (skill protocol, AR3) |

Assets worth protecting: each client's identity and credentials (cross-client leaks are
the worst outcome), secrets in `.env` files and the keychain, personal data in client
repos, the integrity of approvals (build gate, pack ledger, hub decisions), the state
folder, and the integrity of what users install.

## 2. What can go wrong

### B1 agent -> tq hooks and the state folder
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | An agent runs `git push` or a cloud command while the shell is set to another client's identity | `guard.Decide` (`internal/guard/guard.go`) blocks on git email, gh user or cloud drift with exit 2; `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`, `TestInlineIdentityOverride`. Rewordings (`env git`, `sh -c`, aliases) are listed in `docs/CLAUDE-HOOK.md`; per-workspace config folders are the real separation | done | tq |
| Tampering | An agent edits a ledger, trust file or `guard.yaml` to unblock itself | `tq/hub-store-write`, `stateDirDecision` (`internal/cmd/claudehook_tqstate.go`); file tools checked on the resolved path; `TestHookHubStoreWrite`, `TestStateDirTqExemption`. Command rewordings are AR2 | done | tq |
| Tampering | An agent widens its own permissions or removes tq's hooks in `settings.json`, or starts a nested agent with looser settings | `tq/settings-widen`, `tq/settings-shell-write` (`internal/policy/builtin.go`), `tq/nested-agent-loosened` (`internal/policy/builtin_agents.go`); `TestGuardProtectsItsOwnSettings`, `TestHookNestedAgentNeverLoosensAStrictParent` | done | tq |
| Repudiation | An agent's skipped or blocked actions leave no trace | guard-mode decisions logged to `guard/mode.jsonl` (`internal/guardlog`), identity switches to `audit.jsonl` (`internal/audit`); both protected parts of the state folder; `TestModeAuditsMCPContent`. No hash chain: a same-user process can rewrite them (AR1) | done | tq |
| Information disclosure | An agent reads a `.env` file or prints the environment into the transcript | `.env` read and `tq/env-dump` rules (`internal/policy/builtin.go`), `tq dotenv run` masks values in output (`internal/dotenv`); `TestPolicy_ReadEnvDenied_WindowsPath`, `TestDotenvRunLoadsAndMasks` | done | tq |
| Information disclosure | An agent commits or pushes personal data (CPF, emails) from a client repo | `commitPIIDecision`, `textPIIDecision` (`internal/cmd/claudehook_pii.go`), push scan; `TestCommitPII_DeniedWithCPFEvenWithoutDataBlock`, `TestPushPII_DeadlineAsks` | done | tq |
| Information disclosure | An agent reads the build-gate HMAC key with an ordinary file command and signs its own approval | an ordinary read of `build-gate/` is blocked by `tq/hub-store-write`; getting past it is AR2. TM-3 (keychain) was dropped, see [Decisions](#decisions) | accepted (AR2) | tq |
| Denial of service | A hook hangs and stalls every tool call | hook timeouts in `plugin/hooks/hooks.json`, bounded gh lookup, PII scan deadline asks; `TestPushPII_DeadlineAsks`. A hook timeout or tq parse error lets the call through (AR2) | done | tq |
| Elevation of privilege | A guard bug panics and the command runs unchecked | `guardPanicAsk` turns a panic into an ask (deny in Codex), rule `tq/guard-error` is locked; `TestPreToolUseGuardPanicAsks` | done | tq |

### B2 human -> tq human-only actions
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | An agent runs `tq allow --mode relaxed` or a mode change itself | `requireHuman` (`internal/cmd/humanonly.go`) needs a terminal on stdin and stdout and no agent marker; from FR-69 that means class `terminal`: `console`, `run`, `nested` and `unknown` are refused; `tq/mode-change` deny rule; `TestAllowModeIsHumanOnly`, `TestModeWritesRefuseAgents` (`CLAUDECODE` alone still refuses). A pty driven by a same-user program passes (AR1) | done | tq |
| Spoofing | A console session binds the hub off loopback, mints hub keys or tokens, or changes trust or mode | `--bind` off loopback and `hub key|token|serve-key` refuse every class but `terminal`; trust and mode commands keep `requireHuman`; `tq/human-answer` denies key/token/serve-key, API curls and `serve-*.json` reads in every class (`TestConsoleStaysOutOfTheTerminalOnly`) | planned (B-33) | tq |
| Spoofing | An agent answers a build-gate approval | `human_confirm` needs a TTY and a typed one-time code (`plugin/tentaqles/workflow/gate.py`); `test_human_only_cli_needs_tty_and_code` | done | plugin |
| Tampering | A trusted manifest is edited after `tq allow` to add a cloud or loosen the mode | trust is by file hash (`internal/trust`); an edit makes it untrusted and drops the mode grant; `TestPlainAllowAfterEditDropsTheGrant`, `TestPreToolUse_UntrustedManifestCannotAddClouds` | done | tq |
| Repudiation | Nobody can tell which mode a workspace was in when something ran | mode grants and skipped confirms logged in `guard/mode.jsonl`; session start prints the mode | done | tq |
| Information disclosure | (skipped) no data leaves at this boundary | n/a | n/a | n/a |
| Denial of service | (skipped) a human can always run tq directly | n/a | n/a | n/a |
| Elevation of privilege | Relaxed mode switches off identity, secret or privacy checks | `guardcfg.Locked` keeps identity, secrets, self-protection and privacy on in every mode; `TestRelaxedLockedDeniesStillBlock`, `TestLockedNeverChange`, `TestRelaxedKeepsPrivacy` | done | tq |

### B3 plugin -> build-gate records
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | An agent forges the hub's actor token to approve a checkpoint | hub token is an HMAC over action, slug and actor, refused after a rollback; `test_forged_hub_token_is_refused`, `TestTokenMatchesPythonVectors`, `TestAnswerSendsAValidToken` | done | plugin |
| Tampering | An agent writes an approval or test-evidence record by hand | every record is signed (`sign`, `_sig_ok` in `gate.py`); an unsigned or badly signed record does not count | done | plugin |
| Tampering | An agent copies back an older signed approval (rollback) | signed generation records under `build-gate/gen/`; a mismatch refuses every gate until a human re-approves; `test_an_older_signed_approval_copied_back_counts_as_rolled_back` | done | plugin |
| Repudiation | A checkpoint passes with no record of which tests ran on which code | `evidence.jsonl` is signed and bound to project, slug and generation | done | plugin |
| Information disclosure | The key file is read and used to sign forged records | key is a file with mode 0600, which Windows ignores; a same-user program can read it (AR1), an agent only by getting past the text filter (AR2). TM-3 was dropped, see [Decisions](#decisions) | accepted (AR1, AR2) | tq |
| Denial of service | The key or a record is deleted and the build stalls | a missing or broken signature refuses the gate (fail closed); a human re-approves | done | plugin |
| Elevation of privilege | A headless run inherits a human's build token | the runner drops `TQ_BUILD_TOKEN` and `TQ_BUILD_ACTOR` (`internal/hub/adapter/adapter.go`); `TestClaudeArgvAndPermissionWrapper` | done | tq |

### B4 pack checks -> external tools
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | A repo ships its own `dbt` or `.venv/bin/ruff` that tq runs as the real tool | `NoExecRepo` skips repo-local binaries, also through PATH; `TestRepoLocalFailsClosed`, `TestNoExecRepoSkipsVenvVersion` | done | tq |
| Tampering | (skipped) the tool reads the repo it is asked to check; its output is a result, not trusted input to the guard | n/a | n/a | n/a |
| Repudiation | (skipped) tool runs are recorded with the pack check result | n/a | n/a | n/a |
| Information disclosure | A tool (or its telemetry) gets the shell's secrets | `toolEnv` passes only allow-listed names, drops secret-shaped values and URL credentials, sets telemetry off; `TestToolEnvIsFiltered` | done | tq |
| Denial of service | A tool hangs, forks children or prints gigabytes | timeouts, Job Object with kill-on-close, bounded start, 1 MiB output cap; `TestTimeoutKillsTree`, `TestOutputCapped`, `TestAttachThatHangsFailsFast` | done | tq |
| Elevation of privilege | A child process escapes the tree and keeps running after the check | the child starts suspended and runs only once inside the Job Object (`tree_windows.go`) | done | tq |

### B5 browser or local process -> hub web API
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | A web page in the browser calls the hub API (CSRF, DNS rebinding) | Host allow-list, Origin, Referer and Sec-Fetch-Site checks on writes, CSP; fails closed with neither allow-list nor token (`internal/hub/web/auth.go`); `TestRebindingHostAndOriginAreRefused`, `TestNoAllowListAndNoTokenFailsClosed` | done | tq |
| Spoofing | Someone on the network reaches the board | binds `127.0.0.1` by default; any other bind needs a token and an agent session cannot pass `--bind` (`internal/cmd/hub_serve.go`) | done | tq |
| Tampering | An agent answers a decision or approves a plan through the API | the CLI client sends the session class (`X-TQ-Hub-Session`); `run`, `nested`, `unknown`, any unlisted value and the legacy `X-TQ-Hub-Agent: 1` cannot answer, approve, close or set budgets; `console` may answer and move; `TestAPIAgentHeaderRefusesHumanOnly`, `TestAPIClassHeaderPolicy`. The header is self-declared: a same-user process forges it and `serve-*.json` holds `LocalKey` (AR1); a direct curl from any Claude shell is denied by `tq/human-answer` (AR2) | done for the agent header; planned (B-33) for the class | tq |
| Repudiation | A card move or answer cannot be traced | the board's activity log (`internal/hub/activity`) records actor and time | done | tq |
| Information disclosure | The live update stream leaks card content to another page | SSE sends ids only; `TestSSEChangeCarriesIDsOnly` | done | tq |
| Denial of service | (skipped) local-only service; a local process that floods it is AR1 | n/a | n/a | n/a |
| Elevation of privilege | A non-loopback viewer gets write access | token compared in constant time, stored only as a SHA-256 hash | done | tq |

### B6 hub runner -> headless agent run
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | A run or a nested agent claims class `console` to answer its own decision or start a card | `TENTAQLES_HEADLESS=1`, `TQ_HUB_TASK`, `TENTAQLES_HEADLESS_CHILD` or a `tq hub serve` ancestor force `run`; two or more claude/codex ancestors force `nested`; a failed walk under an agent marker is `unknown`, refused like `run`; the runner passes no class to the hub; `TestSessionClassMatrix`, `TestRunClassCannotAnswer` | planned (B-32) | tq |
| Tampering | A run edits the live project while a human works in it | runs prefer a worktree; without one, the project lock serialises runs (`projectlock.go`); `TestProjectLockWaitsUntilRelocation` | done | tq |
| Repudiation | A run's actions cannot be tied to its card | each round posts its transcript to the card and the activity log records actor and verb (`internal/hub/activity`) | done | tq |
| Information disclosure | The prompt or allowed tools leak through the process list | passed on stdin and a `--settings` file, never argv (`internal/hub/adapter/claude.go`); `TestClaudeArgvAndPermissionWrapper` | done | tq |
| Denial of service | A run loops and spends money or time without end | USD, minute and round budgets (`internal/hub/runner/limit.go`); `TestEachBudgetLimitStopsTheRun` | done | tq |
| Elevation of privilege | A run gets bypass permissions the human never granted | the hub adds no permission flag; bypass only after a human `tq allow --bypass` | done | tq |

### B7 tq -> remote services
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | A manifest points the Jira or Azure DevOps base URL at an `http://` or attacker host and the token is sent there | no manifest field sets a tracker URL: Jira builds only `https://<site>.atlassian.net` from `tracker.scope.org` (`siteURL`, `internal/hub/sync/jira/jira.go`), the other adapters use fixed `https://` APIs, and `hubsync.Config.BaseURL` is set only by tests | done | tq |
| Tampering | (skipped) tracker data is shown on the board, never executed | n/a | n/a | n/a |
| Repudiation | (skipped) the tracker keeps its own history | n/a | n/a | n/a |
| Information disclosure | Card text sent to a tracker carries a secret or personal data | outbound text redacted with the exit classes (`internal/hub/sync/engine.go`); `TestRedactionOfOutboundText` | done | tq |
| Information disclosure | A tracker token is printed in a log or error | the token type redacts itself in every format; `TestChainNeverPrintsAValue` | done | tq |
| Information disclosure | `tq decide` sends secrets or personal data to the model endpoint | keys and values redacted with secret patterns, endpoint fixed to the vendor or localhost; `TestRedactsKeysAndQuestions`. Personal data is not masked (`docs/DECIDE.md`); the state it sends is built by tq, not free text | done | tq |
| Denial of service | (skipped) a slow tracker delays sync, never a tool call | n/a | n/a | n/a |
| Elevation of privilege | A cached decide answer from another endpoint is reused | the cache key includes the endpoint and answers are validated; `TestCacheIsPerEndpointAndValidated` | done | tq |

### B8 GitHub -> the user's machine
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | A swapped release archive is installed as tq | `installers/install.ps1` and `install.sh` check the archive's SHA-256 against the release's `checksums.txt` and refuse a mismatch or a missing entry (TM-1) | done | tq |
| Spoofing | A release is built by something other than this repo's workflow | the release workflow attests build provenance for every archive in `checksums.txt` and each installer (`.github/workflows/release.yml`, first in v0.8.2); `install.ps1` and `install.sh` run `gh attestation verify` with the release workflow as signer when `gh` is logged in, and refuse on failure (TM-2) | done | tq |
| Tampering | A moved or re-pushed tag changes what the plugin installs | the root marketplace pins tag and commit sha, CI checks the tag resolves to it, tags `v*` cannot be updated or deleted (ruleset); `TestRootMarketplacePin`, CI job `marketplace-pin` | done | tq |
| Tampering | A bundle catalog entry installs a plugin over `http://`, `git://`, `file://` or `ext::` | refused unless `insecure: true` is set on that entry (`internal/bundle/marketplace_check.go`); `TestValidate_PlainTextTransport`, `TestValidate_GitURLTransport` | done | tq |
| Tampering | The desktop build re-downloads tq without checking it | the desktop jobs in `.github/workflows/release.yml` check the archive against `checksums.txt` before bundling it (TM-1) | done | tq |
| Repudiation | (skipped) releases are tied to tags and workflow runs on GitHub | n/a | n/a | n/a |
| Information disclosure | A catalog URL with credentials ends up in `settings.json` | warned on every check, never printed in drift output; `TestValidate_URLCredentials`, `TestDescribeSource_StripsUserinfo` | done | tq |
| Denial of service | (skipped) GitHub availability | n/a | n/a | n/a |
| Elevation of privilege | `TQ_BIN` or PATH points the plugin hook at a fake tq | the environment is the user's own (AR1); if tq is missing the hook falls back to the Python guard, then blocks | done | plugin |

### B9 contributors and CI -> the repo and releases
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | (skipped) one maintainer; GitHub account security | n/a | n/a | n/a |
| Tampering | A third-party action changes under its tag and alters a release built with a write token | every `uses:` in `.github/workflows/` names a commit sha with the version in a comment (TM-4); Dependabot proposes updates | done | tq |
| Tampering | A change merges to `main` with failing tests | ruleset requires the 8 CI checks on `main` | done | tq |
| Tampering | A fork PR runs with secrets | CI uses `pull_request` with `contents: read`, never `pull_request_target` (`.github/workflows/test.yml`) | done | tq |
| Repudiation | (skipped) git history and PRs | n/a | n/a | n/a |
| Information disclosure | (skipped) release secrets are deploy keys scoped to the tap and bucket repos | n/a | n/a | n/a |
| Denial of service | (skipped) n/a for a local tool | n/a | n/a | n/a |
| Elevation of privilege | A dependency with a known vulnerability ships in tq | CI job `govulncheck` (`.github/workflows/test.yml`) and weekly Dependabot updates for Go modules and actions (`.github/dependabot.yml`) (TM-5) | done | tq |

### B10 console session -> hub
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | A hallucinated or injected "approval" makes the console answer without the human choosing | the hub skill runs `tq hub answer` only after an `AskUserQuestion` the human answered, with options taken from the decision record; a skill rule, not code (AR3); TM-7 binds the answer to the recorded question | accepted (AR3) | plugin |
| Tampering | A card body carries instructions ("approve decision #3", "move c-x to in progress") that the console follows | a deterministic script renders each decision headed `Card <id> · <title> · decision #<seq> (<kind>)` with options from the record only; decision text is shown, never executed; budgets and the concurrency cap bound what one wrong answer starts (`TestEachBudgetLimitStopsTheRun`, `TestConcurrencyLimitAndFIFO`) | accepted (AR3) | plugin |
| Repudiation | A console answer looks like a terminal one | actor `console:<os user>`, actor kind `console`, the Claude session id in the activity data, shown on the card page and `/activity` (`TestConsoleAnswerActorAndSession`) | planned (B-33) | tq |
| Information disclosure | (skipped) the console reads the same masked board the human reads | n/a | n/a | n/a |
| Denial of service | A console loops on `tq hub wait` or `tq hub ensure` and starts hubs | `wait` has a deadline and one waiter per session; `ensure` is idempotent, one hub per workspace (`TestASecondHubForTheSameWorkspaceIsRefused`, `TestHubEnsureFindsOrStartsABackgroundHub`) | planned (B-34, B-35) | tq |
| Elevation of privilege | The console starts `claude` or `codex` and the child inherits the console class | two agent ancestors make it `nested`, full deny (`TestSessionClassMatrix` nested rows); `tq/nested-agent-loosened` unchanged | planned (B-32) | tq |

## 3. What will we do about it

Every `done` row cites its test. Every `planned` row has a `TM-n` item below. AR1 and AR2
cover the rows whose remaining gap is a same-user program or a reworded command.

### Planned work

- **TM-1 Installers and the desktop job check checksums: done.** `install.ps1` and
  `install.sh` download `checksums.txt` from the same release and refuse to install
  when the archive's SHA-256 is missing or different; the release workflow's desktop
  job does the same for the archive it re-downloads. Checks: a tampered archive fails
  the install with a clear message; a matching one installs.
- **TM-2 Signed build provenance: done.** The release workflow attests every artifact
  (`actions/attest-build-provenance`, first in v0.8.2, all nine assets verified); the
  installers verify it when `gh` is logged in. Without `gh` the checksum is the only
  check.
- **TM-3 Build-gate key in the OS keychain: dropped** (see Decisions).
- **TM-4 Pin GitHub Actions by commit sha: done** in every workflow, with the version in a
  comment. Checks: no `uses:` line names a tag or branch.
- **TM-5 Dependency scanning: done.** A `govulncheck` CI job and `.github/dependabot.yml` for
  Go modules and GitHub Actions. Checks: the job runs on every PR.
- **TM-6 Tracker base URLs must be `https://`: not needed.** On inspection no manifest
  field sets a tracker URL (see the B7 Spoofing row).

- **TM-7 Bind console answers to a recorded question: planned (B-36).** The plugin's
  PostToolUse records each `AskUserQuestion` answer headed `Card … decision #<seq>` as a
  record signed with the gate key (card, decision, seq, choice, session id); `tq hub
  answer` from class `console` then needs a matching unexpired record, and `tq claude-hook`
  is denied when invoked from the Bash tool. Checks: an answer with no record is refused
  ("no recorded question"); a replayed record is refused; the terminal path is unchanged.
  Due before AR3 expires.

## 4. Did we do a good job
- Every boundary in §1 has a table in §2: yes
- Every high-risk row is done, planned as a TM item, or accepted below: yes
- The `security-reviewer` checklist references this file: yes (`plugin/agents/security-reviewer.md` reads `docs/threat-model.md`)
- Last breaker or pentest run: none against tq itself; the state-folder guard had three scoped security review rounds in PR #57 (2026-10-30)

## Accepted risks
Each accepted risk expires; an expired risk is reopened as a decision for the repo owner.

| Risk | Why accepted | Accepted by | Date | Expires |
|---|---|---|---|---|
| AR1: a program running as the developer's OS user can read tq's files and keychain entries, write its logs, drive a pty past the human check, and redirect the hook through `TQ_BIN` or PATH | tq cannot defend against code already running as the user; that is the job of the OS, antivirus and not running untrusted programs | the repo owner | 2026-10-30 | 2027-01-28 |
| AR2: the shell-command guards (state folder, secrets, identity) are text filters; a reworded command (globs, escapes, variables, interpreters, links) can get past them, and a hook timeout or tq parse error lets a call through | they catch honest mistakes and prompt-injected slips, not a determined adversary; chasing every bypass added false positives and complexity. Real protection is Claude Code's sandbox and signed state (`docs/GUARD-SCOPE.md`) | the repo owner | 2026-10-30 | 2027-01-28 |
| AR3: a console session's model acts on what it reads, so a prompt injection in a card body or a hallucinated approval can make it answer a decision or start a run the human did not intend | the console exists only for an interactive top-level session the human started; every answer flows through a multiple-choice question the human chooses in (skill protocol, not code); actor and session id are logged; budgets and the concurrency cap bound the blast radius; TM-7 turns the protocol into a check | the repo owner | 2026-10-31 | 2027-01-28 |

## Decisions

- **2026-10-30, TM-3 dropped (repo owner).** Moving the build-gate key into the OS
  keychain with tq doing all signing was planned and then dropped. Build steps the
  agent runs legitimately (`record-review`, `lock-tests`, `dod`) must sign records, so
  tq would need a sign command the agent can reach, guarded again only by a text rule;
  on Windows a same-user program reads Credential Manager entries anyway. The cost was
  a tq process beside every hook call in build repos (about 0.2 s per tool call on
  Windows), a tq-only test setup in CI, and a macOS keychain prompt that could block
  builds. The remaining gain, the key no longer being a plain file, is already covered
  by AR1 and AR2. Real protection would need tq to own every build-gate state change
  and sign only records it produced; that is a possible future project, not planned.
- **2026-10-31, the console answers (repo owner).** PRD D-18 ("never from inside an agent
  session") is superseded by D-24: the human drives the board from one interactive
  `claude`, so the session the human types into answers decisions and starts, moves and
  edits cards as the human's proxy after a multiple-choice question. The line moves from
  "no agent session" to "no run, nested or unknown session"; tq computes the class from
  environment plus process ancestry, not from a flag. Pack approvals are answered from the
  console too, still keychain-signed. Console comments stay agent comments; only an
  answer's note is the human's. What stays at the terminal is D-26 (B2). Residual risk
  AR3; hardening TM-7.

## Assumptions
- The PRD is `docs/prd.md` (FR-69 to FR-72 for the console); `TM-n` items here are the
  mitigation checklist and the PRD's backlog names the work.
- Expiry of accepted risks defaults to 90 days.
- The same-user boundary (AR1) is the line between in-scope and out-of-scope threats.
