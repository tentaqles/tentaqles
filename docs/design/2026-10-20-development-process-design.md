# Tentaqles development process: design

Status: design approved in conversation on 2026-10-20; this document is the written spec for review. Nothing here is implemented. The implementation plan follows this spec once it is approved.

Scope: the process layer tentaqles ships for developers and IT professionals (software engineers and architects, data scientists, data engineers and architects, cloud architects, database administrators), with security and data protection built into every path. Release 1 is specified in full; releases 2 and 3 are outlined.

## 1. Decisions already taken

| Question | Decision |
|---|---|
| Shape | One engine, three on-ramps: `/kickoff`, `/feature`, `/fix` feed the existing evidence-gated build engine. Discipline packs are optional overlays. |
| Packs (eventually) | Security and privacy; data engineering; data science and ML; cloud and database. |
| Enforcement | Security and data protection fail closed (guard rules, hooks). Quality is evidence-gated with a logged human override. |
| Bugfix | Reproduce first: a locked failing regression test before any source edit. `hotfix` skips the critic, never the test. |
| Artifacts | In the repository under `docs/`; the PRD is required for `/kickoff`. |
| Data protection depth | `data:` classification in the manifest; personal data may be read and processed locally, but is blocked at the exits: memory, commits, pushes, PR and issue text, fixtures, model calls. Privacy reviewer, `docs/privacy.md`. (Revised 2026-10-20 at plan review.) |
| Phasing | Release 1: spine plus security and privacy. Release 2: data engineering and ML. Release 3: cloud and database. UI gate later. |

"PRG" in the request is read as PRD (product requirements document).

## 2. Goals, non-goals, success criteria

Goals
- Every project starts from a PRD, and every change can be traced to it or to a recorded decision.
- The same gate engine proves every change: tests locked before the builder runs, evidence recorded by scripts, a fresh-context critic, a fixer loop, two human stops.
- Security and data protection are deterministic controls a model cannot argue with, applied before code exists (threat model, data classes, guardrails) and on every diff (reviewers chosen by risk).
- A bug is never fixed without a reproduction that stays in the suite.
- The process is observable: `tq insights` shows gate outcomes, reviewer findings and overrides.

Non-goals (release 1)
- UI prototype and visual review gates (release 2 or later).
- Issue-tracker integrations (Sentry, Jira, Linear) as workflow triggers.
- Replacing CI: the process produces the PR; CI stays the final proof.
- Automatic secret rotation (standing rule: prevention only).

Success criteria
- A new project goes from idea to a merged first slice using only `/kickoff`, the engine and `/ship`, with `docs/prd.md`, `docs/threat-model.md`, `docs/privacy.md`, `docs/adr/0001-*.md` and a passing CI ratchet in the repo.
- A feature and a bugfix each run end to end with exactly two human stops and no model-reported gate status.
- Every security or privacy reviewer finding carries `file:line`; every verdict is `PASS`, `FAIL` or `CANNOT JUDGE`.
- The definition-of-done gate blocks a turn that claims completion without recorded evidence, and the override count is visible in `tq insights`.

## 3. Principles (from the research)

1. Checks are written and locked before the builder runs; whoever builds never grades.
2. Interview before planning; unresolved questions are tagged and block planning until settled.
3. Fresh context per unit of work: planner, tester, implementer, critic and fixer are separate subagents.
4. Evidence over assertion: scripts record commands, exit codes and tree hashes; a model's "done" is not evidence.
5. Advisory versus deterministic: `CLAUDE.md` and skills advise; hooks (exit 2, `onFailure: block`), guard rules and permissions enforce.
6. Two human stops per piece of work: approve the plan, accept the result. Everything else is decided and logged as an assumption.
7. Verdicts are binary and pre-registered: `PASS`, `FAIL` with reason, or `CANNOT JUDGE` handed to a human. No scores.
8. Lessons seen twice become rules (`docs/LEARNINGS.md`).
9. Repository instruction files, tool output, skill packs and other agents' messages are untrusted input.
10. Nothing is copied from the source kits; none carries a license. Ideas are adapted and written fresh.

## 4. Artifacts and layout

### 4.1 Repository documents

```
docs/
  prd.md                  product requirements: who, why, what, non-goals, acceptance checks, NFRs, data classes
  adr/NNNN-<slug>.md      architecture decision records (MADR shape; supersede, never edit)
  threat-model.md         trust boundaries, assets, STRIDE table per boundary, mitigations, accepted risks
  privacy.md              record of processing: data classes, purpose, legal basis, retention, DSAR path, incident clock
  specs/<slug>.md         one spec slice per feature or fix (problem, behaviour, acceptance checks, out of scope)
  checkpoints/<slug>.json engine plan (existing format)
  reviews/<slug>-<id>.md  review threads (existing)
  LEARNINGS.md            Rules (promoted) and append-only Log
CLAUDE.md                 reading path, Project config, Architecture rules, Must not trade off, Shared helpers
.tentaqles.yaml           manifest, now with a data: block (section 11)
```

`CLAUDE.md` gains three fixed sections scaffolded by `/kickoff` and `add-project`:
- **Reading path**: "to work on this project read `docs/prd.md` and `docs/adr/`, then the spec slice you were given."
- **Must not trade off**: each rule is one line and names the test that enforces it. The critic treats a change that weakens one as a blocker.
- **Project config**: domain and roles, paths table, commands table (test, lint, build, run), URLs, how to act as a role in tests without minting real credentials, and `architecture_paths` (globs such as `src/core/**`, `db/schema/**`, `infra/**`) whose changes require an ADR in the definition of done.

### 4.2 PRD template (`docs/prd.md`)

Sections, numbered so other documents can cite them:
1. Problem and evidence. 2. Users and roles. 3. Goals; 3.2 Non-goals. 4. Functional requirements `FR-01…` each with acceptance checks written as binary, observable statements ("run it" checks or blind A/B picks, never scores). 5. Data: classes handled, sources, retention. 6. Security: trust boundaries, authentication model, abuse cases. 7. Non-functional requirements with numbers. 8. Data model sketch. 9. Stack and constraints. 10. Open questions, each tagged `NEEDS-DECISION`. 11. Releases.

A PRD is **planning-ready** when section 10 is empty and every FR has at least one acceptance check. `/kickoff` and `/feature` check this mechanically.

### 4.3 Other templates

ADR (Context, Decision, Consequences, Alternatives, Status; supersede by a new record). Threat model (assets, boundaries, STRIDE rows: threat, mitigation, status, owner; accepted risks with expiry). Privacy record (section 11.4). Spec slice (existing shape plus `derived_from: FR-xx` and `data_classes_touched`). LEARNINGS (Rules; Log entries `L<n> · date · slug#cp · gate · failure / root cause / fix / lesson`; promotion after two recurrences or a human rejection). Incident playbook (detect, contain, assess data classes, notify within the clock, record for five years). Review thread (existing). Answer key for critics: `| # | check | judged by | reference | from |`, rows `PASS` / `FAIL — reason` / `U<n>: CANNOT JUDGE`, final `RESULT: PASS | FAIL | BLOCKED — U…`.

## 5. Workflows

Each workflow ends by handing the branch to `/ship`, which already enforces identity, risk triage, explicit-path staging, a PR with evidence, and `babysit-pr`. CI output remains the only accepted proof of "green".

### 5.1 New project: `/kickoff <idea | prd path>`

Human stops: (1) approve the data classes and the first checkpoint plan together; (2) accept the result.

1. **PRD.** If no planning-ready PRD exists, run `/prd` (section 6). Stop here if section 10 is not empty; list the open decisions and how each could be settled (ask, research, prototype).
2. **Project shape.** Detect or confirm the stack from the PRD's section 9 and the repository (never assume a default). Write `docs/adr/0001-stack-and-layout.md`. Scaffold `CLAUDE.md` with the three fixed sections; the "Must not trade off" list starts from PRD section 6 and 7 and every line names a test to be written in the first checkpoint.
3. **Threat model.** Answer the four questions (what are we building, what can go wrong, what will we do about it, did we do a good job) per trust boundary from PRD section 6 using STRIDE; write `docs/threat-model.md`. Unmitigated high-risk rows become FRs or `NEEDS-DECISION` items.
4. **Data classification.** From PRD section 5, write the manifest `data:` block (section 11.1) and `docs/privacy.md`. If any class is `pii`, `sensitive`, `financial` or `children`, the privacy record must name purpose, legal basis, retention and the DSAR path before planning continues. Changing the manifest changes its trust hash, so the human runs `tq allow` as part of stop 1.
5. **Guardrails before code.** `ci-pipeline` ratchet with baseline; supply-chain checks (actions pinned to SHAs, `permissions:` on every workflow, lockfiles committed, SBOM on release); secret scanning as a required check; `tq settings render` for deny-first permissions; `.gitignore` for `.env*`, `.claude/build/`.
6. **First slice.** `build-planner` derives `docs/checkpoints/<slug>.json` from the PRD's release 1 FRs. **Human stop 1**: data classes, threat-model accepted risks, checkpoint plan, verify commands.
7. **Engine.** The build engine runs (section 5.4). Reviewer crew per checkpoint from triage (section 7).
8. **Direction.** `/direction` audits the result against the PRD: every FR `Aligned / Partial / Missing / Contradicted`, unplanned work classified, overall `ON COURSE / DRIFTING / OFF COURSE`, recommendations limited to Fix code, Update PRD, Decide.
9. **Human stop 2**, then `/ship`.

### 5.2 New feature: `/feature <description | FR-xx>`

Human stops: (1) checkpoint plan; (2) result.

1. **Locate in the PRD.** If the workspace has a PRD, find the FR. A feature with no FR is first a PRD change (new FR with acceptance checks, committed on the branch), never code. If the workspace has no PRD (legacy project), the spec slice records that and `/feature` continues; `/kickoff` can backfill a PRD later.
2. **Interview.** `grill-me` on business-rule branches, invalid states, permissions, time and locale. Answers go into `docs/specs/<slug>.md` as acceptance checks. Open points are `NEEDS-DECISION` and block planning.
3. **Data and security pre-check.** The spec names `data_classes_touched` and trust boundaries crossed. `tq decide triage` over the planned paths selects the reviewer crew (section 7) before any code exists, so the plan already lists which reviewers will run.
4. **Plan.** `build-planner` writes the checkpoint plan. **Human stop 1.**
5. **Engine** (section 5.4).
6. **Definition of done** at the last checkpoint (section 8.1). **Human stop 2**, then `/ship`. If the feature changed architecture, the PR includes an ADR; if it changed a data class or retention, `docs/privacy.md` and the manifest.

### 5.3 Bugfix: `/fix <report | issue text | failing command>`

Human stops: (1) confirm reproduction and severity; (2) result. For a `hotfix`, stop 1 is skipped and the debt is recorded.

1. **Intake.** Capture the report verbatim into `docs/specs/fix-<slug>.md`: observed, expected, environment, data classes involved (a bug that exposed personal data is also an incident: start the incident playbook clock). Classify severity and whether `hotfix` applies (production impact now, fix path obvious).
2. **Reproduce first.** A tester subagent writes the smallest failing test at the lowest layer that shows the bug; the engine locks it (`build-gate lock-tests`). No reproduction means no fix: the skill reports what it tried, what to instrument, and stops at **human stop 1** with options (add logging, provide data, accept a repro at a higher layer).
3. **Root cause.** The systematic-debugging discipline: read the error, trace the data flow, compare with working paths, one hypothesis at a time. The implementer subagent may not edit the locked test.
4. **Fix.** The smallest change that turns the locked test green without changing other behaviour; verify evidence recorded by the gate. Root cause, fix and lesson appended to `docs/LEARNINGS.md`.
5. **Review by risk.** `tq decide triage` on the diff: low risk gets the quality critic (light); high risk gets the full crew. `hotfix` skips the critic, never the regression test, and adds a pending item "post-hotfix review of <slug>" that `wrap` and the preamble surface until resolved.
6. **Definition of done**, **human stop 2**, `/ship`. If the bug came from a gap in the PRD, threat model or privacy record, that document changes in the same PR.

### 5.4 The engine (existing, with additions)

`build` keeps its structure: worktree, plan approval, per checkpoint tests → lock → implement → behaviour gate → review gate → fixer loop, three failures split, circuit breaker, `docs/LEARNINGS.md`. Additions:
- The review gate runs a **crew**, not one critic: quality critic always; `spec-critic`, `security-reviewer`, `privacy-reviewer`, `breaker` as selected by triage flags (section 7). Each returns the answer-key verdict; the gate passes only when every reviewer returns `PASS`; any `CANNOT JUDGE` goes to the human at stop 2 with the unresolved rows.
- Each reviewer runs on a different model tier than the implementer where routing allows (section 10).
- The definition-of-done check (section 8.1) runs at the last checkpoint and at Stop.

## 6. Skills

New or changed skills in release 1 (all follow the plugin conventions: bash blocks start with the identity-aware snippet, Python through `tq_run.sh`, no bare `python`).

| Skill | Trigger | Output | Notes |
|---|---|---|---|
| `prd` | "write a PRD", "start a PRD", `/kickoff` without one | `docs/prd.md` | Interview (8 to 12 questions, "you decide" allowed and logged), what and why only; marks `NEEDS-DECISION`; refuses "planning-ready" while any remain. Variants: from idea, from brief, from existing code (reverse-engineer a PRD for a legacy repo). |
| `kickoff` | "new project", "start project X", "kick off" | section 5.1 artifacts, first checkpoint plan | Requires planning-ready PRD. Composes `prd`, `threat-model`, `classify-data`, `ci-pipeline`, `build`. |
| `feature` | "add feature", "implement FR-xx", "/feature" | spec slice, plan, engine run | Composes `grill-me`, `build`, `direction`. |
| `fix` | "fix bug", "this fails", stack traces pasted, `/fix` | `docs/specs/fix-<slug>.md`, locked regression test, fix | Jev skill picker suggests it when a prompt looks like a bug report. `--hotfix` flag. |
| `threat-model` | "threat model", `kickoff`, `feature` crossing a new boundary | `docs/threat-model.md` | Four questions, STRIDE per boundary; unmitigated high rows become FRs. |
| `classify-data` | `kickoff`, "what data do we handle", PII-looking schema change | manifest `data:` block, `docs/privacy.md` | Produces the data dictionary with classes; prompts for purpose, legal basis, retention, DSAR path. |
| `privacy-review` | diff touching PII paths, "privacy review" | review findings, `docs/privacy.md` update | Runs the `privacy-reviewer` agent; used by the engine and standalone. |
| `direction` | "are we on track", end of `kickoff`, before a release | drift scorecard | Read-only (Read, Grep, Glob, git read commands); never marks aligned without reading the code; "Not verified" section instead of guessing. |
| `dod` | end of `feature`/`fix`, Stop hook, `ship` | checklist with evidence or the missing items | Section 8.1. |
| `supply-chain` | `kickoff`, "harden CI", unpinned action found | workflow edits, SBOM step, Scorecard | Extends `ci-pipeline`. |
| `llm-security-review` | diff adds LLM calls, tools, RAG, MCP servers or hooks | findings | OWASP LLM Top 10 (2025) and Agentic Top 10 (2026): output handling, least-agency tools, memory write validation, MCP trust. Applies to tentaqles itself. |
| `add-project` (changed) | existing | scaffolds `CLAUDE.md` fixed sections, `docs/` skeleton, LEARNINGS | Points to `/kickoff` for the PRD. |
| `ship` (changed) | existing | adds `dod` evidence and PR size advisory to the PR body | Large diffs (over about 400 changed lines) need a stated reason. |
| `build` (changed) | existing | reviewer crew, answer-key verdicts, DoD at last checkpoint | Section 5.4. |

## 7. Agents

All agents are fresh-context subagents with explicit tool lists. Reviewers are read-only (Read, Grep, Glob) except where noted. Every reviewer receives: the spec slice, the review bundle (diff, new files, recorded verify evidence), `CLAUDE.md`, `docs/LEARNINGS.md`, and its own checklist. Every reviewer returns the answer-key verdict with `file:line` on each finding and states whether it had any part in producing the change ("if you built this, say so").

| Agent | Role | Selected when | Checklist source |
|---|---|---|---|
| `build-planner` | existing | always | plan schema |
| `build-critic` | quality: correctness, tests, architecture, consistency, docs | always | existing, verdict grammar upgraded |
| `spec-critic` | compliance: does the diff do what the spec slice and the FR say, nothing more | always on `feature` and `kickoff`; on `fix` when the fix touches files outside the failing path | spec slice, PRD |
| `security-reviewer` | ASVS 5.0 chapter by surface (V6 authentication, V7 session, V8 authorization, V11 crypto, V1 encoding and injection), OWASP Top 10:2025 incl. A03 supply chain and A10 exceptional conditions | triage flags: auth, secrets, public API, shared state, destructive commands; any workflow or dependency manifest change | `auth-security` references, threat model |
| `privacy-reviewer` | purpose, legal basis, minimisation, retention and deletion job, DSAR path, logging of personal data, cross-border transfer, incident clock | diff touches paths or tables classified in the manifest `data:` block; new log fields, events or exports with PII patterns | `docs/privacy.md`, LGPD Art. 6, 18, 19, 37, 46 to 48; GDPR Art. 5, 25, 30, 32 to 35 |
| `llm-security-reviewer` | OWASP LLM and Agentic Top 10 | diff adds LLM calls, tool definitions, RAG, MCP servers, hooks or skills | section 6 |
| `breaker` (Bash allowed, localhost only) | adversarial QA against the running app in the worktree: races, double submits, authorization bypass, input limits, resilience; backs up and restores test data; blanks third-party keys | user-facing change with a runnable app and a `run` command in Project config | threat model, spec |
| `build-fixer` | existing | on any FAIL | findings only |
| `arbiter` | one disputed finding, cited files, `CLAUDE.md` | a finding upheld twice after dispute | existing rule |

Model tiers: reviewers run on a stronger or at least different tier than the implementer; the implementer is routed by Jev when the policy enforces (existing routing). Caps: three critic rounds per checkpoint, three failed attempts per gate, then split, then circuit breaker (existing).

## 8. Hooks and guard rules

Existing: 22 guard rules in tq (destructive SQL and cloud operations, force push, env dumps, secret stores, migrations, MCP writes, unpinned actions, guard self-protection); hooks on SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Stop, SessionEnd; Jev completion-evidence check at Stop.

### 8.1 Definition of done (new, Stop and `ship`)

When the turn belongs to a `feature`, `fix` or `kickoff` run (marker file in `.claude/build/`), the Stop hook requires recorded evidence for:
- the project's test command exit 0 after the last edit (tool records, not text);
- lint and type-check commands when Project config lists them;
- every locked test byte-identical;
- no open `FAIL` or `CANNOT JUDGE` from the crew;
- an ADR when files under architecture-tagged paths changed; `docs/privacy.md` and the manifest when a data class or retention changed; a migration plan when schema files changed;
- changelog entry when the project keeps one.

Missing items block the stop with the list. The human can override by stating so in the next prompt; the override is logged to `docs/LEARNINGS.md` and counted by `tq insights`. The Stop hook honours `stop_hook_active` and the 8-block cap as today.

### 8.2 New guard rules (tq, fail closed)

| Rule | Event | Action |
|---|---|---|
| `tq/pii-fixture-dump` | Bash, SQL MCP | deny copying rows from tables or paths classified `pii`/`sensitive`/`financial` into `tests/`, `fixtures/`, `seeds/` without a pseudonymisation step; synthetic fixtures only |
| `tq/commit-pii` | Bash `git commit` | deny a commit whose added lines carry personal or financial data (CPF, CNPJ, card, IBAN always; e-mail and phone when the manifest declares `pii`), the same scan as `tq/commit-secret`; fixtures marked synthetic with `tq:allow-pii` pass |
| `tq/push-pii` | Bash `git push` | deny a push whose unpushed commits carry such data (scan of `@{upstream}..HEAD`); the fix is to rewrite the commits locally |
| `tq/text-pii` | Bash `gh pr/issue/release …`, `git commit -m`, `git tag -m`, `git notes` | deny when the message, body or `--body-file` carries such data: nothing personal leaves the machine in PR or issue text |
| `tq/iac-apply` | Bash | ask on `terraform apply`, `pulumi up`, `az deployment`, `aws cloudformation deploy` and similar; deny when the shell has no explicit environment variable naming the target environment |
| `tq/remote-migration` | Bash, SQL MCP | ask on migrations against non-local connection strings (existing `migration-edit` and `mcp-apply-migration` extended) |
| `tq/config-change` | ConfigChange hook | deny edits that add `Bash(*)`, `bypassPermissions`, disable hooks, or widen `permissions.allow` outside `tq settings render`; log every settings change |
| `tq/commit-message` | Bash `git commit` | ask when the message is not Conventional Commits; `!` or `BREAKING CHANGE` requires a MAJOR note |
| `tq/actions-unpinned` | existing `gha-unpinned` | kept; extended to deny `@main`/`@master` refs |

All enforcement hooks are declared with `onFailure: "block"` so a crashed guard fails closed, and with timeouts (hooks must never stall).

### 8.3 Advisory hooks

- SessionStart: inject the reading path and the open `NEEDS-DECISION` count when `docs/prd.md` exists.
- PR size advisory in `ship` (not a block).
- PostToolUse async logger of Bash commands for audit (existing `knowledge-capture` extended with command and exit code).

## 9. Jev integration

- **Skill picker** (UserPromptSubmit): suggests `/fix` for bug-shaped prompts, `/feature` for feature requests, `/prd` or `/kickoff` for new-project language. Shadow first, as every Jev feature.
- **Triage** (`tq decide triage`): the eleven existing risk questions plus two new ones: "touches a classified data path" (from the manifest) and "adds LLM, tool, MCP or hook code". Flags map to the reviewer crew (section 7).
- **Stop check**: the definition of done (8.1) extends the existing completion-evidence check; claims are regex-caught first, Jev only when the regex passes, evidence only from tool records.
- **Routing**: reviewers get a tier at least as strong as the implementer; the implementer may be routed cheaper when confident (existing rule).
- **Memory gates**: unchanged; `docs/LEARNINGS.md` entries are also recorded as decisions when they promote to Rules.

## 10. Templates shipped

`docs/prd.md`, `docs/adr/0000-template.md`, `docs/threat-model.md`, `docs/privacy.md`, `docs/specs/_template.md`, `docs/specs/_fix-template.md`, `docs/LEARNINGS.md`, `docs/incident-playbook.md`, `CLAUDE.md` project scaffold, answer-key review contract, checkpoint plan (existing). Each template is written fresh under the plugin's MIT license.

## 11. Data protection design

### 11.1 Manifest `data:` block

```yaml
data:
  classes: [pii, financial]          # from: public, internal, pii, sensitive, financial, children, secrets
  regions: [BR]                      # where subjects live; drives the incident clock and transfer checks
  prompts:
    allow: [public, internal]        # classes that may appear in model context unmasked
  paths:                             # where classified data lives in the repo or runtime
    - path: db/schema/customers.sql
      classes: [pii]
    - table: public.payments
      classes: [financial]
  retention_default: 24m
  incident:
    authority: ANPD                  # ANPD: 3 business days; GDPR authority: 72 hours
    contact: dpo@example.test
```

`classify-data` writes it; `tq doctor` warns when a repo has PII-looking columns (email, cpf, phone, birth date, address) and no `data:` block.

### 11.2 Where personal data may go (revised 2026-10-20)

The user's rule: working with customer data must stay practical. Claude may read files and query tables that hold personal data and run scripts over them. The controls sit at the **exits**, where data would be registered or published:

- **Memory and knowledge**: everything written to `memory.db`, the vault mirror, atoms and reference docs passes a redaction that covers personal and financial data (CPF, CNPJ, e-mail, phone, card, IBAN) as well as secrets, selected by the manifest's `data.classes`.
- **Model and Jev calls from hooks**: the same redaction before any text leaves the machine (Jev already redacts secrets; PII is added).
- **Git**: `tq/commit-pii` and `tq/push-pii` deny recording or publishing such data; `tq/pii-fixture-dump` denies copying real rows into `tests/`, `fixtures/`, `seeds/`.
- **Text that goes to GitHub**: `tq/text-pii` denies PR and issue bodies, comments, release notes, commit and tag messages that carry it.
- `tq redact` is a helper, not a gate: `tq redact run -- <cmd>` masks a command's output when the user wants a shareable copy (a PR body, a bug report). Nothing forces reads through it.
- `.env*`, secret stores and environment dumps stay denied (existing rules).
- Without a `data:` block only the checksum-validated detectors run (CPF, CNPJ, card, IBAN), so a repository with no classification is not flooded with e-mail false positives; declaring `pii` turns e-mail and phone detection on.

### 11.3 Fixtures and tests

Synthetic data only. `tq/pii-fixture-dump` denies production extracts into fixtures; the `classify-data` skill offers a Faker-based generator per classified table.

### 11.4 Privacy record (`docs/privacy.md`)

Per processing activity: data classes, subjects, purpose, legal basis (LGPD Art. 7 or 11; GDPR Art. 6 or 9), minimisation note, retention and the deletion job that enforces it, access controls, encryption at rest and in transit, DSAR path (confirmation, access, correction, deletion, portability; LGPD response within 15 days, GDPR within one month), transfers, DPIA or RIPD trigger (large scale, sensitive, children, profiling), incident clock (ANPD three business days per Res. CD/ANPD 15/2024; GDPR 72 hours per Art. 33), record retention five years.

## 12. Measurement (`tq insights`)

Per workspace and across identities: gate pass rate per gate type; reviewer findings by reviewer and severity; definition-of-done overrides with reasons; `CANNOT JUDGE` rate; time from PRD planning-ready to merged PR; hotfix debt open count; LEARNINGS promotions.

## 13. Releases

**Release 1 (this spec):** `prd`, `kickoff`, `feature`, `fix`, `threat-model`, `classify-data`, `privacy-review`, `direction`, `dod`, `supply-chain`, `llm-security-review`; agents `spec-critic`, `security-reviewer`, `privacy-reviewer`, `llm-security-reviewer`, `breaker`; guard rules 8.2; DoD Stop gate; templates; `tq redact`; triage questions; insights metrics; `add-project`, `ship`, `build` changes; docs.

**Release 2:** data engineering pack (`data-contract` with ODCS and breaking-change rules, dbt gates, `pipeline-review`, warehouse cost guardrail) and ML pack (`ml-review` leakage lint, reproducibility checklist, model promotion gate, `MODEL_CARD.md`, drift check). UI gate (prototype, visual and behaviour reviewers) if wanted.

**Release 3:** cloud and database pack (`iac-policy` with Checkov or Trivy and Conftest, IAM least-privilege lint, apply and destroy gated, backup and restore drill, migration SQL lint, RLS gate, pgTAP RLS tests).

**Backlog added at plan review (2026-10-20), not part of releases 1 to 3:**

- **Local task hub (`tq hub`).** A shared human-and-agent workspace run as a local server for ourselves (the role Paperclip or a Linear board plays elsewhere): a per-workspace queue with states `inbox → next → doing → waiting → done`; tasks tagged `agent-ready` are picked up by a local runner that opens a headless Claude Code session with the workspace identity, the memory store and `tq dotenv` for credentials; the runner posts its plan and progress back to the task, moves it to `waiting` when it needs a human, and sends a desktop notification. Low-friction capture (a hotkey, a phone shortcut hitting the local server over the LAN, `tq hub add "<task>"`). Every agent output passes **evals** before `waiting`: the project's definition of done for code tasks, and a checklist eval (tone, no LLM-isms, "verified not plausible", "would the owner have decided this") for non-code tasks, with the retry loop the engine already has. The human's job becomes scoping and QA, with a persistent trail per task. Reuses: `loop-runner.py` (headless rounds under budgets), `build-gate`, the signal bus, `tq insights` for token budgets per task. Source: Nick Saraev, "A Practical AI Agent Workflow For Companies In 2027" (clipping, 2026-07-14); the user's direction is a local server of our own instead of a hosted tracker.
- **Knowledge base ingestion pipeline.** Beyond the Markdown connector shipped in 0.11.1, adapt the Cerebras Knowledge design (Tai, Kim, Gao, "How We Built Our Knowledge Base", 2026-07-15; saved as `sources/cerebras-ai-blog-how-we-built-our-knowledge-base-2026-10-20-20_07_35.pdf`, and Nick Saraev's walkthrough clipping of 2026-07-19):
  - *One table, one connector per source.* Every source (Slack or Discord, wiki, repositories, mail, the YouTube channel, custom databases as small plugin scripts) lands in one embeddings table with `document`, `embedding`, `metadata`, `source`, timestamps; each source declares what it is, how to connect and how often to fetch. For tentaqles this is the per-client `memory.db` (`reference_docs` today), never a cross-client index.
  - *Distillation before embedding.* An LLM turns each thread into one structured artifact (`question` an engineer would search for, `summary`, `resolution`, `systems`, `code_refs`) and that normalised document is what gets embedded, not the raw transcript. Chat ingestion re-fetches the whole thread on every reply and upserts it as one row.
  - *Bursting.* Runs of consecutive messages by one author are embedded on their own with the thread topic prepended, only when they clear a threshold (a rare token, IDF ≥ 4.0; ≥ 200 characters; or reactions), so a tangent that holds the answer stays findable.
  - *Hybrid retrieval, fused.* Full-text (exact tokens, pasted errors) + embeddings (paraphrase) + IDF (rare tokens beat filler) + age decay (newer wins ties), each a ranked list, fused with reciprocal rank fusion (k = 60), de-duplicated per source, capped per file, then a small reranker scores 0 to 10 and keeps ten, and winners are expanded with neighbouring sections. tentaqles already has FTS5 BM25 × recency; this adds the embedding list, IDF and RRF.
  - *Planner → executor → synthesis.* A light planning pass picks retrieval tools (`search`, `search_slack`, `search_code` as ripgrep, `recent_prs`, `who_knows`, `subsystem_index` of per-file summaries), fans them out in parallel, normalises into one evidence schema (score, recency, source hints), and a final pass answers with citations. Over MCP the primitives are exposed as narrow, LLM-free tools so Claude Code is the orchestrator; a web UI runs the full pipeline.
  - *Projects as scope.* A project is a named bundle of sources (channels, repos, document spaces); a user's default project scopes every query. In tentaqles the workspace is the project.
  - *Code.* Language-aware chunking coarse to fine (class → method → block), incremental re-embedding of changed chunks per commit, allow and deny lists per repository.
  - *Eval.* The same question set with and without the knowledge base, counting correct, partial and declined answers (Saraev: 17/20 vs 0/20 on 640 documents).
  - Keep the existing redaction before storage and `data:` classes (`prompts.allow`) for what may enter context unmasked; the authorization and audit layer is per workspace identity.

## 14. Assumptions and open points

Assumptions logged by the designer (correct any that are wrong):
- "PRG" means PRD.
- Legacy repositories without a PRD may use `/feature` and `/fix`; only `/kickoff` requires one.
- `tq redact` is a new Go subcommand used as a helper; reads of personal data are not gated (user decision 2026-10-20: controls at the exits, see §11.2).
- The `ConfigChange` hook event is available in the user's Claude Code version (listed in the current hooks reference).
- Reviewer model tiers follow the existing Jev routing policy; where routing is off, reviewers use the session's model.
- The vault export (release shipped today) also mirrors `docs/prd.md` status and open `NEEDS-DECISION` items into `context.md` in a later minor release; not part of release 1.

Open points for the user at spec review:
- Whether `/fix` on a workspace with Jev disabled should default to light or full review (proposal: full, "when in doubt review more").
- Whether `breaker` runs by default for every user-facing feature or only when the plan marks a checkpoint `runnable` (proposal: only when marked; it needs a running app).

## 15. Provenance

Research notes (not shipped): `scratchpad/research/clippings.md`, `sources.md`, `domain-practices.md`, `agentic-workflows.md` from this session. Primary standards referenced: NIST SSDF SP 800-218, OWASP ASVS 5.0.0, OWASP Top 10:2025, OWASP Top 10 for LLM Applications 2025 and for Agentic Applications 2026, SLSA 1.2, CISA 2026 Minimum Elements for SBOM, GDPR, LGPD (Lei 13.709/2018) and ANPD Res. CD/ANPD 15/2024, DORA 2025. Workflow ideas adapted from the Shopify checkpoint workflow, Vercel's direction and breaker agents, Wayfinder's answer key, the Karpathy loop, Cole Medin's validation-first planning and Anthropic's Claude Code best practices; no files copied.
