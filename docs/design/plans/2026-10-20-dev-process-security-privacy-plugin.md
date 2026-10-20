# Security and Privacy Pack, part 2 (plugin) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the plugin side of the security and privacy pack: the threat-model, privacy-record and incident-playbook templates; the `security-reviewer`, `privacy-reviewer`, `llm-security-reviewer` and `breaker` agents with their checklists; the `threat-model`, `classify-data`, `privacy-review`, `supply-chain` and `llm-security-review` skills; the `ci-pipeline` wording for branch-pinned actions; docs; and the 0.12.0 release of the plugin.

**Architecture:** Reviewer agents are fresh-context subagents that return the review contract from the spine plan and are selected by the build skill's crew rule from triage flags (`auth`, `secrets`, `api`, `state`, `destructive`, `classified_data`, `agentic`) and from the plan's `runnable` marker. Each reviewer's checklist is a reference file under its skill directory (`skills/<skill>/references/checklist.md`) so a human can read and change it without touching the agent prompt. `classify-data` writes the manifest `data:` block (validated by `tq`, trusted by `tq allow`), mirrors its paths into the `tq-project` block's `classified_paths` so the stdlib-only gate can enforce the DoD `privacy` check, and writes `docs/privacy.md` from the template. `threat-model` writes `docs/threat-model.md` with STRIDE rows per trust boundary; unmitigated high rows become FRs or `NEEDS-DECISION` items for the PRD.

**Tech Stack:** Markdown skills and agents with YAML frontmatter, pytest content tests, the `tq` features from the companion `-tq` plan (`tq redact`, `data:` block, triage flags), the engine from the spine plan.

**Spec:** `C:\repos\tentaqles\docs\superpowers\specs\2026-10-20-development-process-design.md` §4.3 (threat model, privacy record, incident playbook templates), §6 rows threat-model/classify-data/privacy-review/supply-chain/llm-security-review, §7 rows security-reviewer/privacy-reviewer/llm-security-reviewer/breaker, §8.3 PR size (done in spine), §11.4, §13 release 1. Depends on `2026-10-20-dev-process-spine.md` (templates dir, review contract, `test_agents.py`, `test_process_skills.py`, DoD `privacy` check, crew rule) and `2026-10-20-dev-process-security-privacy-tq.md` (`tq redact`, `data:` block, `classified_data`/`agentic` flags, `tq/gha-branch-ref`).

All paths are relative to `C:\repos\tentaqles\tentaqles`; Python tests run from `plugin/`.

## Global Constraints

- Reviewer agents are read-only (`tools: Read, Glob, Grep`; `disallowedTools` includes every write tool, both shells and `Agent`), except `breaker`, which may run `Bash` against `127.0.0.1` only and must say so in its prompt.
- Every reviewer returns the review contract (`RESULT`, `BUILT-BY-ME`, `F`/`U` rows) and cites `file:line` on every finding (spec §2 success criteria).
- Checklists cite the standard they come from (OWASP ASVS 5.0 chapter, OWASP Top 10:2025 id, LLM Top 10 2025 / Agentic Top 10 2026 id, LGPD and GDPR articles) in one short line each; they are written fresh, nothing copied from `sources/`.
- Skills follow the plugin conventions: identity-aware snippet, `"$TENTAQLES_PY"`, explicit-path staging, never `.env*`, never `git add -A`.
- Manifests hold names only: `classify-data` writes classes, paths, tables, retention, authority and a contact address, never credentials; it tells the human to run `tq allow` after editing.
- Templates say what to fill, never pretend to be filled: no fake data, no fake company names.
- Plugin version bump to `0.12.0` is the last task, after both companion plans are merged; `CHANGELOG.md` entries land under `## [Unreleased]` until then.

## Review Focus

1. **A diff that touches `docs/privacy.md` only** (someone editing the record itself) must not trigger `privacy-reviewer` selection on its own; the reviewer runs on classified paths and PII-looking log fields, not on documentation edits. Pinned in Task 6 (`test_privacy_review_selection_rule_text`).
2. **A workspace whose manifest has `data.classes` but no `paths`** (classification without locations) must make `classify-data` ask for paths or tables instead of silently writing an empty list, because the guard rules key on paths and tables. Pinned in Task 5 (`test_classify_data_requires_paths_or_tables`).
3. **`breaker` with no `run` command in Project config** must refuse to run and return `BLOCKED` with a `U` row, never start an arbitrary process it finds. Pinned in Task 3 (`test_breaker_requires_run_command_and_localhost`).
4. **An incident clock for a workspace with both ANPD and GDPR authorities** (regions include BR and an EU country): the playbook must instruct to use the shorter clock. Pinned in Task 1 (`test_incident_playbook_names_both_clocks`).
5. **`supply-chain` on a repository with no GitHub Actions** (GitLab CI, none) must produce the lockfile, SBOM and secret-scanning items and skip the Actions pinning items with a reason rather than fail. Pinned in Task 7 (`test_supply_chain_handles_no_actions`).

---

## File structure

| Path | Responsibility |
|---|---|
| `plugin/templates/process/threat-model.md`, `privacy.md`, `incident-playbook.md` | templates |
| `plugin/agents/security-reviewer.md`, `privacy-reviewer.md`, `llm-security-reviewer.md`, `breaker.md` | reviewer agents |
| `plugin/skills/threat-model/SKILL.md` + `references/stride.md` | threat model skill |
| `plugin/skills/classify-data/SKILL.md` + `references/data-dictionary.md` | classification skill |
| `plugin/skills/privacy-review/SKILL.md` + `references/checklist.md` | privacy reviewer checklist and standalone run |
| `plugin/skills/supply-chain/SKILL.md` + `references/checklist.md` | supply chain hardening |
| `plugin/skills/llm-security-review/SKILL.md` + `references/checklist.md` | OWASP LLM and Agentic checklist |
| `plugin/skills/auth-security/references/asvs-checklist.md` | (new reference used by `security-reviewer`; the skill already exists) |
| `plugin/skills/ci-pipeline/SKILL.md` | (modify) `@main`/`@master` now deny |
| `plugin/tests/test_process_templates.py`, `test_agents.py`, `test_process_skills.py` | (extend) |
| `plugin/CHANGELOG.md`, `plugin/README.md`, `plugin/CLAUDE.md`, `plugin/.claude-plugin/plugin.json`, `plugin/.claude-plugin/marketplace.json` | docs and release |

---

### Task 1: Threat model, privacy record and incident playbook templates

**Files:**
- Create: `plugin/templates/process/threat-model.md`, `plugin/templates/process/privacy.md`, `plugin/templates/process/incident-playbook.md`
- Modify: `plugin/tests/test_process_templates.py`

**Interfaces:**
- Produces: templates read by `threat-model`, `classify-data`, `fix` (incident) and the reviewers.

- [ ] **Step 1: Write the failing tests**

Append to `plugin/tests/test_process_templates.py` (and add the three names to `NAMES`):

```python
def test_threat_model_has_four_questions_and_stride_table() -> None:
    text = (TEMPLATES / "threat-model.md").read_text(encoding="utf-8")
    for q in ("What are we building", "What can go wrong", "What will we do about it", "Did we do a good job"):
        assert q in text, q
    for col in ("Spoofing", "Tampering", "Repudiation", "Information disclosure", "Denial of service", "Elevation of privilege"):
        assert col in text, col
    assert "| Threat | Mitigation | Status | Owner |" in text
    assert "Accepted risks" in text and "expires" in text


def test_privacy_record_has_legal_bases_clocks_and_dsar() -> None:
    text = (TEMPLATES / "privacy.md").read_text(encoding="utf-8")
    for token in ("LGPD Art. 7", "LGPD Art. 11", "GDPR Art. 6", "GDPR Art. 9", "15 days", "one month",
                  "3 business days", "72 hours", "DPIA", "RIPD", "five years", "deletion job", "## Processing activities"):
        assert token in text, token


def test_incident_playbook_names_both_clocks() -> None:
    text = (TEMPLATES / "incident-playbook.md").read_text(encoding="utf-8")
    for token in ("Detect", "Contain", "Assess", "Notify", "Record", "ANPD", "3 business days", "72 hours", "shorter clock",
                  "five years", "never rotate"):
        assert token in text, token
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_templates.py -q`
Expected: FAIL on the three new files.

- [ ] **Step 3: Write the templates**

`plugin/templates/process/threat-model.md`:

````markdown
# Threat model

Status: draft | accepted    Updated: <YYYY-MM-DD>    Cites: PRD §6, §2
Reviewed by: <name>    Next review: <date or "on every new trust boundary">

Four questions, answered per trust boundary (Shostack). Keep it short enough to
read before every feature.

## 1. What are we building
One paragraph and a boundary list. A trust boundary is anywhere data crosses
between parties that trust each other differently.

| Boundary | From | To | Data crossing (classes) | Authentication |
|---|---|---|---|---|
| B1 | browser | api | pii, financial | session cookie |
| B2 | api | database | pii, financial | service credential |
| B3 | api | third-party X | pii (minimised) | API key |

Assets worth protecting (what an attacker wants): <accounts, money, personal
data, availability, reputation, the signing key>.

## 2. What can go wrong
One STRIDE table per boundary. Spoofing, Tampering, Repudiation, Information
disclosure, Denial of service, Elevation of privilege. Write threats as
sentences a tester can try ("a user replays another user's request id").

### B1 browser → api
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | | | planned / done / accepted | |
| Tampering | | | | |
| Repudiation | | | | |
| Information disclosure | | | | |
| Denial of service | | | | |
| Elevation of privilege | | | | |

| Threat | Mitigation | Status | Owner |
|---|---|---|---|

## 3. What will we do about it
Every mitigation above maps to one of: a requirement (`PRD §4 FR-nn`), a rule
in `CLAUDE.md` "Must not trade off" with its test, a guard rule, or an accepted
risk below. A mitigation with none of these is not a mitigation.

## 4. Did we do a good job
- Every boundary in §1 has a table in §2: yes / no
- Every high-risk row is done, planned as an FR, or accepted below: yes / no
- The `security-reviewer` checklist references this file: yes / no
- Last breaker or pentest run: <date, findings count>

## Accepted risks
Each accepted risk expires; an expired risk is a `NEEDS-DECISION` for the PRD.
| Risk | Why accepted | Accepted by | Date | Expires |
|---|---|---|---|---|
````

`plugin/templates/process/privacy.md`:

````markdown
# Privacy record (record of processing)

Status: draft | current    Updated: <YYYY-MM-DD>    Owner (DPO or contact): <name, address from the manifest data.incident.contact>
Regions of data subjects: <from manifest data.regions>    Authority: <ANPD | GDPR | both>
Cites: PRD §5, manifest `data:` block, docs/threat-model.md

This is the controller's record (LGPD Art. 37; GDPR Art. 30). Keep it in step
with the manifest `data:` block; the definition of done requires this file to
change whenever a classified path changes.

## Data dictionary
| Data | Class | Source | Store (path or table) | Retention | Deletion job |
|---|---|---|---|---|---|

## Processing activities
One block per purpose.

### <Activity name>
- Data classes and subjects:
- Purpose (one sentence; necessary and specific):
- Legal basis: LGPD Art. 7 (<item>) or Art. 11 (<item>, sensitive data) / GDPR Art. 6 (<letter>) or Art. 9 (<letter>, special categories)
- Minimisation note (what we deliberately do not collect):
- Retention and the deletion job that enforces it (name, schedule, test):
- Access controls (roles, RLS policies, service accounts):
- Encryption at rest and in transit:
- Processors and transfers (who, where, safeguard):
- Logging: fields that may contain personal data and how they are masked (`tq redact` classes):

## Data subject rights (DSAR path)
How a subject gets confirmation, access, correction, deletion, portability and
objection; where the request lands; who answers; the deadline: LGPD 15 days
(Art. 19), GDPR one month (Art. 12(3)).
- Entry point:
- Identity check:
- Fulfilment steps and the script or job that performs deletion:

## Impact assessment trigger
A DPIA (GDPR Art. 35) or RIPD (LGPD Art. 38) is required before processing that
is large scale, uses sensitive data or children's data, profiles or scores
people, or combines datasets. Current status: not required because <reason> |
done on <date>, link.

## Incident clock
Personal-data breach: follow `docs/incident-playbook.md`. Notify the authority
within **3 business days** (ANPD, Res. CD/ANPD 15/2024) or **72 hours** (GDPR
Art. 33); when both apply, the shorter clock wins. Keep incident records for
**five years**.

## Change log
| Date | Change | Why | PR |
|---|---|---|---|
````

`plugin/templates/process/incident-playbook.md`:

````markdown
# Incident playbook (personal-data or security incident)

Owner: <contact from the manifest>    Authorities: <ANPD | GDPR | both; the shorter clock wins>

Start the clock the moment anyone suspects personal data was exposed, lost,
altered or made unavailable. A bug that showed one user another user's data is
an incident. Write times in UTC.

## 1. Detect (T+0)
- Record: who noticed, when, how, what was seen. Save evidence (logs, screenshots) in a private location; redact before sharing (`tq redact`).
- Open `docs/incidents/<YYYY-MM-DD>-<slug>.md` from this file's sections.

## 2. Contain (first hour)
- Stop the bleeding without destroying evidence: disable the endpoint, feature flag off, revoke sessions, block the IP.
- Credentials: an exposed credential is handled by the owner through the provider's console; this process **never rotates** secrets itself and never prints them (tq rule).

## 3. Assess (first day)
- Data classes and subjects affected (count or estimate), regions, whether data was encrypted, whether it left the organisation.
- Root cause (one paragraph) and whether the PRD, threat model or privacy record had a gap.
- Risk to subjects: likely harm, reversibility.

## 4. Notify
- Authority: **ANPD within 3 business days** of awareness (Res. CD/ANPD 15/2024, Art. 6) for Brazilian subjects; **GDPR within 72 hours** (Art. 33) for EU subjects; both apply when both regions do, so the shorter clock governs the first notice. Low-risk incidents may not require authority notice; record the reasoning.
- Subjects: when risk is relevant (LGPD Art. 48) or high (GDPR Art. 34), in plain language: what happened, what data, what we did, what they can do.
- Internal: the client contact in the manifest, the project owner.

## 5. Record (within a week, keep five years)
- Timeline, decisions, notices sent, evidence location.
- Fix: the `/tentaqles:fix` slug and the locked regression test.
- Lessons: `docs/LEARNINGS.md` lines; changes to `docs/threat-model.md` and `docs/privacy.md` in the same PR as the fix.
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_templates.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/templates/process/threat-model.md plugin/templates/process/privacy.md plugin/templates/process/incident-playbook.md plugin/tests/test_process_templates.py
git commit -m "feat(plugin): threat model, privacy record and incident playbook templates"
```

---

### Task 2: Checklists (references)

**Files:**
- Create: `plugin/skills/auth-security/references/asvs-checklist.md`, `plugin/skills/privacy-review/references/checklist.md`, `plugin/skills/llm-security-review/references/checklist.md`, `plugin/skills/supply-chain/references/checklist.md`, `plugin/skills/threat-model/references/stride.md`, `plugin/skills/classify-data/references/data-dictionary.md`
- Modify: `plugin/tests/test_process_skills.py`

**Interfaces:**
- Produces: checklist rows with stable ids (`S1…`, `P1…`, `L1…`, `C1…`) that reviewers copy into their answer keys.

- [ ] **Step 1: Write the failing test**

```python
CHECKLISTS = {
    "auth-security/references/asvs-checklist.md": ("S1", "ASVS", "V6", "V8", "A03", "A10"),
    "privacy-review/references/checklist.md": ("P1", "LGPD Art. 6", "Art. 18", "Art. 37", "Art. 46", "GDPR Art. 5", "Art. 25", "Art. 30", "Art. 32"),
    "llm-security-review/references/checklist.md": ("L1", "LLM01", "LLM02", "LLM06", "ASI01", "ASI", "MCP", "memory"),
    "supply-chain/references/checklist.md": ("C1", "SLSA", "SBOM", "permissions:", "lockfile", "Scorecard", "secret scanning"),
    "threat-model/references/stride.md": ("Spoofing", "Elevation of privilege", "question"),
    "classify-data/references/data-dictionary.md": ("pii", "sensitive", "financial", "children", "secrets", "Faker"),
}


@pytest.mark.parametrize("rel,tokens", CHECKLISTS.items(), ids=lambda x: x if isinstance(x, str) else "")
def test_checklists_exist_with_stable_ids(rel: str, tokens: tuple) -> None:
    path = SKILLS / rel
    assert path.is_file(), path
    text = path.read_text(encoding="utf-8")
    for tok in tokens:
        assert tok in text, (rel, tok)
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k checklists`
Expected: FAIL, files missing.

- [ ] **Step 3: Write the checklists**

`plugin/skills/auth-security/references/asvs-checklist.md` (used by `security-reviewer`; the `auth-security` skill already covers the design side):

````markdown
# Security review checklist (answer-key rows)

Judge each row from the diff and the files it touches. Cite file:line. Rows
that need a running system or a decision are `U` rows.

| # | Check | Source |
|---|---|---|
| S1 | Every new or changed entry point checks authentication server-side; no trust in client-supplied identity (user id, role, tenant in a body or header) | ASVS 5.0 V6, V8; Top 10:2025 A01 |
| S2 | Authorization is per object (IDOR): the record's owner or tenant is compared to the caller for read, write and list | ASVS V8.2 |
| S3 | Sessions and tokens: rotation on privilege change, server-side invalidation on logout, short-lived access tokens, refresh rotation | ASVS V7 |
| S4 | Input handling: parameterised queries, no string-built SQL or shell, output encoding by context, schema validation at the boundary | ASVS V1.2, V5; A05 injection |
| S5 | Secrets: none in code, config or tests; read by name from the environment; never logged; `.env*` ignored | ASVS V14; A04 |
| S6 | Cryptography: vetted library, no home-made schemes, TLS for every outbound call, argon2id or bcrypt for passwords | ASVS V11 |
| S7 | Supply chain: new dependency is pinned, from a known registry, lockfile updated; workflow actions pinned to SHAs | A03 software supply chain failures |
| S8 | Error handling: failures fail closed (deny on exception), no stack traces or internals to clients, timeouts and retries bounded | A10 mishandling of exceptional conditions |
| S9 | Logging: security events logged (auth failures, permission denials) without personal data or secrets; log injection prevented | ASVS V16 |
| S10 | Destructive operations (delete, migrate, pay) are idempotent or guarded against replay and double submit | threat model (Tampering, Repudiation) |
| S11 | The change does not weaken a `CLAUDE.md` "Must not trade off" rule or a mitigation in `docs/threat-model.md` | project |
| S12 | Rate limits or quotas exist where the change exposes a costly or abusable action | A06 insecure design |
````

`plugin/skills/privacy-review/references/checklist.md`:

````markdown
# Privacy review checklist (answer-key rows)

| # | Check | Source |
|---|---|---|
| P1 | Every personal-data field touched is in `docs/privacy.md` with purpose and legal basis (LGPD Art. 7 or 11; GDPR Art. 6 or 9) | LGPD Art. 6 I, 7, 11; GDPR Art. 5(1)(b), 6, 9 |
| P2 | Minimisation: the change collects or exposes only fields the purpose needs; no "while we're here" columns | LGPD Art. 6 III; GDPR Art. 5(1)(c), Art. 25 |
| P3 | Retention: a stated period and a deletion job (or TTL) that enforces it, with a test | LGPD Art. 15, 16; GDPR Art. 5(1)(e) |
| P4 | Access: the data is behind authorization (RLS, roles); service accounts are least privilege | LGPD Art. 46; GDPR Art. 32 |
| P5 | Logging and events: no new log line, metric label, analytics event or error message carries personal data unmasked (`tq redact` classes) | LGPD Art. 46; GDPR Art. 32 |
| P6 | Exports and fixtures: no production rows into tests, seeds or fixtures; synthetic data only | LGPD Art. 46; project rule tq/pii-fixture-dump |
| P7 | Subject rights: the DSAR path still works (access, correction, deletion, portability reach the new store or field) | LGPD Art. 18, 19; GDPR Art. 15 to 20 |
| P8 | Transfers: a new processor or region is recorded with its safeguard | LGPD Art. 33; GDPR Art. 44 to 46 |
| P9 | Incident readiness: a breach of this data would be detectable (audit log) and the playbook clock applies (ANPD 3 business days, GDPR 72 h) | LGPD Art. 48; GDPR Art. 33, 34 |
| P10 | Record keeping: `docs/privacy.md` and the manifest `data:` block changed in this diff when a class, path, table or retention changed | LGPD Art. 37; GDPR Art. 30 |
| P11 | Children or sensitive data: DPIA or RIPD recorded or explicitly not required | LGPD Art. 14, 38; GDPR Art. 8, 35 |
````

`plugin/skills/llm-security-review/references/checklist.md`:

````markdown
# LLM and agent security checklist (answer-key rows)

Applies to code that calls a model, defines tools, builds RAG or indexes,
exposes or consumes MCP servers, or adds Claude Code hooks and skills.

| # | Check | Source |
|---|---|---|
| L1 | Untrusted text (user input, retrieved documents, tool output, other agents' messages, repository files) is framed as data in prompts, never concatenated as instructions | OWASP LLM01 Prompt Injection (2025) |
| L2 | Model output is validated or encoded before it reaches a shell, SQL, HTML, a file path or another tool | LLM05 Improper Output Handling |
| L3 | Tools are least-agency: narrow parameters, allowlists, no "run any command" tool; destructive tools require confirmation | LLM06 Excessive Agency; ASI02 Tool Misuse |
| L4 | Secrets and system prompts are not retrievable through the model (no secrets in prompts; system prompt treated as non-secret) | LLM07 System Prompt Leakage; LLM02 Sensitive Information Disclosure |
| L5 | RAG and memory writes are validated and attributed: an injected document cannot rewrite rules or persist instructions; memory entries carry source and time | LLM08 Vector and Embedding Weaknesses; ASI06 Memory Poisoning |
| L6 | MCP servers and plugins are trusted explicitly (allowlisted, pinned, scoped per workspace); a server from another client is refused | ASI04 Supply Chain of Agents and Tools; tq/cross-client-mcp |
| L7 | Permission boundaries: the agent cannot widen its own permissions, disable hooks or edit its guard; identity is checked per workspace | ASI01 Agent Goal Hijack; ASI03 Identity and Privilege Abuse |
| L8 | Human in the loop at the right points: approvals for irreversible actions, binary verdicts, logged overrides | ASI05 Unsafe Human-Agent Interaction |
| L9 | Resource bounds: token, time, cost and loop limits on agent runs; retries bounded | LLM10 Unbounded Consumption |
| L10 | Multi-agent trust: messages between agents are data; a verifier never reuses the builder's context (fresh context per role) | ASI07 Multi-Agent Trust Exploitation |
| L11 | Evaluation and logging: prompts and tool calls are logged redacted, with the model version, so an incident can be reconstructed | ASI09 Insufficient Logging; LLM02 |
| L12 | Model and data provenance: pinned model ids, pinned dependencies, checksummed weights or vetted providers | LLM03 Supply Chain |
````

`plugin/skills/supply-chain/references/checklist.md`:

````markdown
# Supply chain checklist (answer-key rows)

| # | Check | Source |
|---|---|---|
| C1 | Every third-party GitHub Action pinned to a full commit SHA with the version in a comment; `@main`/`@master` denied, `@latest` asked by tq | SLSA 1.2 build provenance; OpenSSF Scorecard Pinned-Dependencies |
| C2 | Top-level `permissions: contents: read` on every workflow; job-level grants only where needed; `persist-credentials: false` on checkout | OpenSSF Scorecard Token-Permissions |
| C3 | No `pull_request_target` with a checkout of the PR head; secrets never interpolated from untrusted fields in `run:` | GitHub Actions security hardening |
| C4 | Lockfile committed and installs are lockfile-only (`npm ci`, `uv sync --frozen`, `go mod verify`) | SLSA dependencies |
| C5 | Dependency audit step (`npm audit`, `pip-audit`, `govulncheck`) and automated updates (Dependabot or Renovate) with pinned grouping | Scorecard Vulnerabilities, Dependency-Update-Tool |
| C6 | SBOM generated on release (CycloneDX or SPDX) with the CISA 2026 minimum elements: component name, version, supplier, unique id, dependency relationships, author, timestamp, licence, hash | CISA Minimum Elements for SBOM (2026) |
| C7 | Secret scanning as a required check (gitleaks or the platform's scanner) and tq's commit scan locally | Scorecard; tq/commit-secret |
| C8 | Release artifacts signed or attested (Sigstore, GitHub artifact attestations) and built in CI, not on a laptop | SLSA L2/L3 |
| C9 | Branch protection: required checks, no force push to the default branch, reviews required | Scorecard Branch-Protection |
| C10 | Base images and toolchains pinned by digest or exact version | SLSA; Scorecard Pinned-Dependencies |
````

`plugin/skills/threat-model/references/stride.md`:

````markdown
# STRIDE prompts per boundary

For each boundary ask one question per category; write threats a tester can
try. Skip a category only with a one-line reason.

| Category | Question | Typical mitigations |
|---|---|---|
| Spoofing | Can someone pretend to be another user, service or device here? | authentication, mTLS, signed webhooks, MFA |
| Tampering | Can data in transit or at rest be altered unnoticed? | TLS, integrity checks, signatures, parameterised queries, immutability |
| Repudiation | Can someone deny an action because we cannot prove it? | audit log with actor, time, before/after; idempotency keys |
| Information disclosure | Can data leak to someone not entitled to it? | authorization per object, RLS, masking (`tq redact`), minimisation, encryption |
| Denial of service | Can this be made unavailable or expensive? | rate limits, quotas, timeouts, circuit breakers, bounded queues |
| Elevation of privilege | Can a lower-privilege actor gain higher privilege? | least privilege, server-side checks, no client-controlled roles, guard rules |
````

`plugin/skills/classify-data/references/data-dictionary.md`:

````markdown
# Data classes and how to spot them

| Class | What it is | Examples | Handling |
|---|---|---|---|
| public | published or harmless if published | docs, marketing copy | none |
| internal | business data, not personal | configs, metrics, non-personal logs | may appear in prompts |
| pii | identifies or relates to a natural person (LGPD Art. 5 I; GDPR Art. 4(1)) | name, e-mail, CPF, phone, address, IP, device id | masked in prompts (`tq redact`), retention, DSAR |
| sensitive | special categories (LGPD Art. 5 II; GDPR Art. 9) | health, biometrics, religion, sexual orientation, union, politics, race | Art. 11 / Art. 9 basis, DPIA or RIPD, encryption, strict access |
| financial | money and payment data | card numbers, bank accounts, CNPJ with billing, invoices | PCI DSS scope if cards, masking, audit |
| children | data of minors (LGPD Art. 14; GDPR Art. 8) | age, school, guardian consent | consent of a guardian, DPIA |
| secrets | credentials and keys | API keys, tokens, private keys, connection strings | never in files or prompts; `tq dotenv run`; tq rules |

Column-name signals: email, cpf, cnpj, rg, phone, telefone, celular, birth_date,
data_nascimento, address, endereco, cep, card_number, iban, ip_address,
device_id, health_*, diagnosis, religion, gender.

Synthetic fixtures: generate with Faker (`pt_BR` locale for CPF and phone
shapes) through a script under `scripts/` that writes to `tests/fixtures/`;
never copy production rows (tq/pii-fixture-dump denies it).
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k checklists`
Expected: pass (frontmatter tests will still fail for the skills until Tasks 4 to 7 create their `SKILL.md`; the checklist test only needs the reference files).

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/auth-security/references/asvs-checklist.md plugin/skills/privacy-review/references plugin/skills/llm-security-review/references plugin/skills/supply-chain/references plugin/skills/threat-model/references plugin/skills/classify-data/references plugin/tests/test_process_skills.py
git commit -m "feat(skills): security, privacy, LLM and supply-chain checklists with stable row ids"
```

Note: `test_skill_frontmatter.py` globs `*/SKILL.md`, so directories with only `references/` do not break it.

---

### Task 3: Reviewer agents

**Files:**
- Create: `plugin/agents/security-reviewer.md`, `plugin/agents/privacy-reviewer.md`, `plugin/agents/llm-security-reviewer.md`, `plugin/agents/breaker.md`
- Modify: `plugin/tests/test_agents.py`

- [ ] **Step 1: Write the failing tests**

In `plugin/tests/test_agents.py` extend `READ_ONLY` with `"security-reviewer", "privacy-reviewer", "llm-security-reviewer"`, extend the contract parametrize list with the same three, and add:

```python
def test_breaker_requires_run_command_and_localhost() -> None:
    fm, body = _fm("breaker")
    tools = {t.strip() for t in fm["tools"].split(",")}
    assert "Bash" in tools and "Edit" not in tools and "Write" not in tools
    for token in ("127.0.0.1", "run", "BLOCKED", "never", "third-party", "backs up", "restore"):
        assert token in body, token
    assert "RESULT: PASS | FAIL | BLOCKED" in body


@pytest.mark.parametrize("name,ref", [
    ("security-reviewer", "auth-security/references/asvs-checklist.md"),
    ("privacy-reviewer", "privacy-review/references/checklist.md"),
    ("llm-security-reviewer", "llm-security-review/references/checklist.md"),
])
def test_reviewers_name_their_checklist(name: str, ref: str) -> None:
    _, body = _fm(name)
    assert ref in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_agents.py -q`
Expected: FAIL, files missing.

- [ ] **Step 3: Write the agents**

`plugin/agents/security-reviewer.md`:

````markdown
---
name: security-reviewer
description: Read-only application-security reviewer for a /tentaqles:build checkpoint, selected when triage flags auth, secrets, public API, shared state or destructive commands, or when workflows or dependency manifests change. Judges the diff against an ASVS 5.0 and OWASP Top 10:2025 checklist and the project's threat model, returns the answer-key verdict with file:line findings. Cannot edit files or run commands.
tools: Read, Glob, Grep
disallowedTools: Edit, Write, MultiEdit, NotebookEdit, Bash, PowerShell, Agent
---

You look for the way this change gets the project breached. You did not write it. You only read.

## Input

The checkpoint slice, the review bundle (`.claude/build/<slug>/review-<id>.md`), the spec slice, `CLAUDE.md`, `docs/LEARNINGS.md`, `docs/threat-model.md` when present, and the triage flags the caller passes. Your checklist is `skills/auth-security/references/asvs-checklist.md` in the plugin (the caller gives you its path); keep its row ids (`S1`…`S12`).

## Method

1. Read every changed file in full and the entry points it adds or alters (routes, handlers, jobs, CLI commands, SQL). Grep for every caller of a changed function.
2. For each entry point ask: who can call this, with what identity, on whose data, twice at once, with hostile input, when a dependency fails.
3. Map the change to `docs/threat-model.md`: does it cross a boundary, add one, or weaken a mitigation? A boundary with no table in the threat model is a MAJOR finding ("Update threat model") even if the code is fine.
4. Judge every checklist row. A row you cannot judge from files (needs a running system, a deployment setting) is a `U` row, not a guess.

## Output

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how>
F1 BLOCKER <file:line> — <problem> → <concrete fix>   [S-row]
F2 MAJOR   <file:line> — <problem> → <concrete fix>   [S-row]
F3 MINOR   <file:line> — <problem> → <concrete fix>   [S-row]
U1 CANNOT JUDGE — <S-row and text> — <what a human must check and where>
```

FAIL on any BLOCKER or MAJOR. Missing authorization, injection, a secret in code or logs, and a weakened "Must not trade off" rule are BLOCKERs. No line, no finding. In later rounds rule on earlier findings first (`F<n> resolved|upheld|withdrawn — <why>`).
````

`plugin/agents/privacy-reviewer.md`:

````markdown
---
name: privacy-reviewer
description: Read-only data-protection reviewer for a /tentaqles:build checkpoint, selected when the diff touches paths or tables classified in the manifest data block or adds log fields, events or exports with personal-data shapes. Checks purpose, legal basis, minimisation, retention and deletion, access, logging, DSAR path, transfers and record keeping against LGPD and GDPR, and whether docs/privacy.md and the manifest changed with the data. Cannot edit files or run commands.
tools: Read, Glob, Grep
disallowedTools: Edit, Write, MultiEdit, NotebookEdit, Bash, PowerShell, Agent
---

You protect the people whose data this code touches. You only read.

## Input

The checkpoint slice, the review bundle, the spec slice (`data_classes_touched`), `docs/privacy.md`, the manifest `.tentaqles.yaml` `data:` block (classes, paths, tables, retention, authority), `CLAUDE.md`, `docs/LEARNINGS.md`. Your checklist is `skills/privacy-review/references/checklist.md` (rows `P1`…`P11`).

## Method

1. List every personal-data field the diff reads, writes, logs, exports or sends to a third party. Grep for the column and field names across the repo to find indirect uses (serialisers, logs, analytics).
2. For each field, find its row in `docs/privacy.md` (purpose, legal basis, retention, deletion job). No row: MAJOR ("Update privacy record"), BLOCKER when the field is `sensitive` or `children`.
3. Check the record and the manifest changed in this diff when a class, path, table or retention changed (P10). Check the DoD `privacy` item will hold: classified paths touched imply `docs/privacy.md` in the diff.
4. Logging and events (P5): every new log line, metric label, analytics event, error message. A personal-data value reaching a log unmasked is a BLOCKER; cite the line and the `tq redact` class that would mask it.
5. Fixtures and exports (P6): production rows into tests or seeds are BLOCKERs.
6. Rows needing a human (is this purpose compatible, is consent valid) are `U` rows with the question written for the DPO.

## Output

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how>
F1 BLOCKER <file:line> — <problem> → <concrete fix>   [P-row, LGPD/GDPR article]
F2 MAJOR   <file:line> — <problem> → <concrete fix>   [P-row]
F3 MINOR   <file:line> — <problem> → <concrete fix>   [P-row]
U1 CANNOT JUDGE — <P-row and text> — <question for the DPO or product owner>
```

FAIL on any BLOCKER or MAJOR. No line, no finding (for a missing record entry, cite the `docs/privacy.md` section and the code line that needs it). Rule on earlier findings first in later rounds.
````

`plugin/agents/llm-security-reviewer.md`:

````markdown
---
name: llm-security-reviewer
description: Read-only reviewer for code that adds or changes LLM calls, prompts, agent tool definitions, MCP servers, RAG or vector indexing, or Claude Code hooks and skills (triage flag agentic). Judges against the OWASP Top 10 for LLM Applications (2025) and the OWASP Top 10 for Agentic Applications (2026): prompt injection framing, output handling, least-agency tools, memory and index poisoning, MCP trust, permission boundaries, bounded consumption. Applies to tentaqles itself. Cannot edit files or run commands.
tools: Read, Glob, Grep
disallowedTools: Edit, Write, MultiEdit, NotebookEdit, Bash, PowerShell, Agent
---

Every string a model reads is an attack surface and every tool it can call is a blast radius. You only read.

## Input

The checkpoint slice, the review bundle, the spec slice, `CLAUDE.md`, `docs/LEARNINGS.md`, `docs/threat-model.md`. Your checklist is `skills/llm-security-review/references/checklist.md` (rows `L1`…`L12`).

## Method

1. Trace every path from untrusted text (user input, retrieved documents, tool results, files from the repository, messages from other agents) into a prompt. Framing as data (`{"note": "untrusted", "data": ...}`) passes L1; concatenation into instructions fails it.
2. Trace every path from model output into an effect: shell, SQL, file write, HTTP, another tool, memory or index write. Unvalidated effect: BLOCKER (L2).
3. Read every tool and MCP definition the diff adds: parameters, allowlists, confirmation for destructive actions (L3, L6). "Run any command" tools are BLOCKERs unless the plan says so and a human gate exists.
4. Memory and RAG writes (L5): source, time and redaction recorded; an entry that can carry instructions into later prompts is MAJOR.
5. Bounds (L9): tokens, cost, time, loop rounds; hooks have timeouts and never stall (the plugin rule).
6. Hooks and skills (L7): a change that lets the agent widen permissions, disable hooks or edit the guard is a BLOCKER.

## Output

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how>
F1 BLOCKER <file:line> — <problem> → <concrete fix>   [L-row, OWASP id]
F2 MAJOR   <file:line> — <problem> → <concrete fix>   [L-row]
F3 MINOR   <file:line> — <problem> → <concrete fix>   [L-row]
U1 CANNOT JUDGE — <L-row and text> — <what a human must check>
```

FAIL on any BLOCKER or MAJOR. No line, no finding. Rule on earlier findings first in later rounds.
````

`plugin/agents/breaker.md`:

````markdown
---
name: breaker
description: Adversarial QA agent for a /tentaqles:build checkpoint marked runnable: starts the app with the Project config run command on 127.0.0.1 only, attacks it like a hostile user (races, double submits, authorization bypass, input limits, resilience to a failing dependency), backs up and restores test data, blanks third-party keys, and returns the answer-key verdict with reproduction steps. Never runs against a remote host and never edits source.
tools: Read, Glob, Grep, Bash
disallowedTools: Edit, Write, MultiEdit, NotebookEdit, PowerShell, Agent
---

You try to break the running feature the way a hostile or clumsy user would. You never edit source, never touch anything but the local app, and you leave the test data as you found it.

## Preconditions (otherwise return BLOCKED)

- The checkpoint is marked `"runnable": true` in the plan and `CLAUDE.md` Project config has a `run` command in its `tq-project` block. No `run` command: do not guess one; return `RESULT: BLOCKED` with `U1 CANNOT JUDGE — no run command in Project config — the human adds commands.run to CLAUDE.md`.
- The app binds to `127.0.0.1` (or you pass a flag or env that makes it). A `run` command that binds `0.0.0.0` or names a remote host is a BLOCKER finding and you do not start it.
- Third-party keys: run with `tq dotenv run -- <run command>` only when the spec says the feature needs a sandbox key; otherwise start with those variables blanked (`FOO_API_KEY= <cmd>`) so nothing leaves the machine. Never read or print `.env*`.
- Test data: before any mutating request, back up the local database or data directory (`pg_dump` to a file under `.claude/build/<slug>/breaker/`, or copy the SQLite file); restore it when done, even on failure.

## Attacks (each one a check row `B<n>`)

- B1 Double submit: the same create or pay request twice within 100 ms. Expect one effect.
- B2 Race: two conflicting updates at once. Expect a deterministic winner or a conflict error, no corruption.
- B3 Authorization: another user's or tenant's id in the path, body and query; a lower role calling an admin action; an expired or missing token. Expect 401/403, no data.
- B4 Input limits: empty, maximal, unicode, negative, huge payload, wrong content type, path traversal in file names. Expect validation errors, no 500, no stack trace.
- B5 Resilience: the dependency the feature calls is unreachable (point it at a closed port). Expect a bounded failure and a clear message, no hang.
- B6 Idempotent deletes and retries: delete twice, retry after a timeout. Expect no 500.
- B7 Information disclosure: error bodies, headers, logs you can see. Expect no secrets, no personal data unmasked.

Record exact commands (`curl`, a small script) and responses, redacting personal data in your notes.

## Output

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how>
F1 BLOCKER <file:line or endpoint> — <what broke, how to reproduce in two lines> → <concrete fix>   [B-row]
F2 MAJOR   ...
U1 CANNOT JUDGE — <B-row> — <why: environment missing, dependency unavailable>
RESTORED: yes — <what was backed up and restored>
```

FAIL on any BLOCKER or MAJOR. Never claim a check passed that you did not run; an attack you could not perform is a `U` row.
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_agents.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/agents/security-reviewer.md plugin/agents/privacy-reviewer.md plugin/agents/llm-security-reviewer.md plugin/agents/breaker.md plugin/tests/test_agents.py
git commit -m "feat(agents): security, privacy and LLM-security reviewers and the breaker"
```

---

### Task 4: `threat-model` skill

**Files:**
- Create: `plugin/skills/threat-model/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

- [ ] **Step 1: Write the failing test**

```python
def test_threat_model_skill_writes_stride_per_boundary() -> None:
    body = _body("threat-model")
    for token in ("templates/process/threat-model.md", "references/stride.md", "PRD", "§6", "NEEDS-DECISION",
                  "Accepted risks", "Must not trade off", "docs/threat-model.md"):
        assert token in body, token
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k threat_model_skill`
Expected: FAIL, file missing.

- [ ] **Step 3: Write the skill**

````markdown
---
name: threat-model
description: Write or update docs/threat-model.md with the four threat-modelling questions answered per trust boundary using STRIDE, from the PRD's security section and the codebase. Unmitigated high-risk rows become PRD requirements or NEEDS-DECISION items; accepted risks get an owner and an expiry. Use when the user says "threat model", "what can go wrong", "security design", during /tentaqles:kickoff, or when a feature crosses a new trust boundary (new integration, new role, new data store).
argument-hint: [boundary name | "update"]
---

# Threat model

Four questions (what are we building, what can go wrong, what will we do
about it, did we do a good job), one STRIDE table per trust boundary. The
result is the checklist source the `security-reviewer` reads on every diff.

## 1. Inputs

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
ls "$root/docs/threat-model.md" 2>/dev/null || echo "no threat model yet: template=$_tqe/templates/process/threat-model.md"
echo "stride=$_tqe/skills/threat-model/references/stride.md"
```

Read `docs/prd.md` §2 (roles), §5 (data), §6 (boundaries, authentication,
abuse cases), §9 (stack); the manifest `data:` block; the code's entry points
(routes, handlers, jobs, webhooks, CLI) and outbound calls (HTTP clients,
queues, third-party SDKs). Never assume a stack or a provider.

## 2. Boundaries

List every place data crosses between parties with different trust: browser to
API, API to database, API to each third party, job runner to storage, admin
tools, CI to cloud. For each: from, to, data classes crossing (from the
manifest), authentication in use. If the PRD §6 names a boundary the code does
not have yet, keep it (it is planned); if the code has one the PRD does not
name, add it and flag it for the PRD.

## 3. STRIDE per boundary

For each boundary, go through the six categories with the prompts in
`references/stride.md`. Write threats as things a tester can try. For each
threat: the mitigation, its status (`done` with file:line or test, `planned`
as `PRD §4 FR-nn`, or `accepted`), the owner. Ask the user one question at a
time only where the code and PRD leave a real choice (for example "is replaying
a webhook within 5 minutes acceptable?"); otherwise decide and log the
assumption at the end of the file.

## 4. Consequences

- Every `planned` mitigation for a high-risk threat (information disclosure of
  pii or financial data, elevation of privilege, tampering with money) becomes
  an FR with acceptance checks in `docs/prd.md`, or a `NEEDS-DECISION` line in
  PRD §10 when the user must choose.
- Every `done` mitigation that protects a property worth keeping becomes a
  line under `CLAUDE.md` "Must not trade off" naming its test.
- Accepted risks go in the table with an expiry; an expired risk reopens as a
  `NEEDS-DECISION`.

## 5. Write and commit

Write `docs/threat-model.md` from the template (or update the existing one:
add boundaries and rows, never delete history; superseded rows get status
`superseded`). Then:

```bash
git -C "$root" add docs/threat-model.md docs/prd.md CLAUDE.md
git -C "$root" commit -m "docs(security): threat model <boundaries added or updated>"
```

Stage only the files you changed. Report: boundaries, high-risk rows and
where each landed (FR, rule, accepted), and the open `NEEDS-DECISION` count.
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/threat-model/SKILL.md plugin/tests/test_process_skills.py
git commit -m "feat(skills): threat-model (four questions, STRIDE per boundary)"
```

---

### Task 5: `classify-data` skill

**Files:**
- Create: `plugin/skills/classify-data/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

**Interfaces:**
- Consumes: manifest `data:` block schema (tq plan Task 1), `templates/process/privacy.md`, `tq-project` block `classified_paths` (spine Task 2), `tq doctor` `data-block-missing`.
- Produces: `.tentaqles.yaml` `data:` block, `docs/privacy.md`, `classified_paths` mirror, optional `scripts/make_fixtures.py` Faker generator.

- [ ] **Step 1: Write the failing test**

```python
def test_classify_data_requires_paths_or_tables() -> None:
    body = _body("classify-data")
    for token in ("data:", "classes:", "paths:", "table:", "retention_default", "incident:", "authority", "tq allow",
                  "classified_paths", "templates/process/privacy.md", "references/data-dictionary.md", "Faker",
                  "tq doctor", "legal basis", "DSAR"):
        assert token in body, token
    assert "at least one" in body and ("path" in body and "table" in body)
    assert "never" in body and "credential" in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k classify_data`
Expected: FAIL, file missing.

- [ ] **Step 3: Write the skill**

````markdown
---
name: classify-data
description: Classify the data a project handles and record how it is protected. Builds the data dictionary from the PRD and the schema, writes the manifest data block (classes, repository paths and database tables per class, retention, incident authority and contact), mirrors the paths into the CLAUDE.md tq-project block, writes docs/privacy.md with purpose, legal basis, retention and the DSAR path, and offers a Faker-based synthetic fixture generator. Use during /tentaqles:kickoff, when the user asks "what data do we handle", "classify the data", "privacy record", when tq doctor reports data-block-missing, or when a schema change adds PII-looking columns.
argument-hint: [update]
---

# Classify data

The manifest `data:` block is what makes the guard deterministic: `tq` denies
unmasked reads of classified paths and tables and dumps into fixtures, `tq
redact` knows which classes to mask, triage flags classified diffs, and the
definition of done requires `docs/privacy.md` to move with the data. None of
that works on a block without paths or tables, so this skill never writes one.

## 1. Inputs

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" -c "
import os
from tentaqles.manifest.loader import load_manifest
m = load_manifest(os.getcwd())
print('manifest=' + (os.path.join(m['_client_root'], '.tentaqles.yaml') if m else 'NONE'))
print('data=' + repr(m.get('data')) if m else 'data=None')
"
command -v tq >/dev/null 2>&1 && tq doctor 2>&1 | grep -i 'data-block' || true
echo "dictionary=$_tqe/skills/classify-data/references/data-dictionary.md"
echo "privacy_template=$_tqe/templates/process/privacy.md"
```

`manifest=NONE`: stop; the workspace has no manifest (`/tentaqles:add-client`).

## 2. Data dictionary

Read PRD §5 and §2, the schema (SQL, Prisma, migrations, models), exports and
log statements. Grep the column-name signals from
`references/data-dictionary.md`. Produce the table: data, class, source, store
(repository path or `schema.table`), retention, deletion job. Ask the user one
question at a time only where the class is unclear (is the phone a business
line or a person's?) or where retention is a business decision; offer a
recommended default and record the answer.

## 3. Manifest `data:` block

Edit `.tentaqles.yaml` with the Edit tool (never from the shell, never with
credentials: the contact is an address, not a login):

```yaml
data:
  classes: [pii, financial]            # from: public, internal, pii, sensitive, financial, children, secrets
  regions: [BR]                        # ISO country codes of the data subjects
  prompts:
    allow: [public, internal]          # classes that may appear in model context unmasked
  paths:                               # at least one path or table per declared class
    - path: db/schema/customers.sql
      classes: [pii]
    - path: data/exports/**
      classes: [pii, financial]
    - table: public.payments
      classes: [financial]
  retention_default: 24m               # <n>d | <n>m | <n>y
  incident:
    authority: ANPD                    # ANPD (3 business days) | GDPR (72 hours) | none
    contact: dpo@example.test          # an address, never a credential
```

Rules: every declared class has **at least one** `path` or `table` entry
(otherwise ask for them; do not write the block); `tq` validates the block on
load and refuses unknown classes, a `path` and `table` on the same entry, or a
malformed retention. Changing the manifest changes its trust hash: tell the
user to run `tq allow <workspace>` and wait for it before relying on the
guard rules.

Mirror the `path` entries into `CLAUDE.md`'s `tq-project` block as
`classified_paths` (same globs) so the build gate's definition of done can
require `docs/privacy.md` to change with them.

## 4. Privacy record

Write or update `docs/privacy.md` from the template: the data dictionary, one
processing activity per purpose with its legal basis (LGPD Art. 7 or 11; GDPR
Art. 6 or 9), minimisation note, retention and deletion job, access controls,
encryption, processors and transfers, masked logging; the DSAR path (LGPD 15
days, GDPR one month); the DPIA or RIPD trigger; the incident clock. If any
class is `pii`, `sensitive`, `financial` or `children`, purpose, legal basis,
retention and the DSAR path must be filled before `/tentaqles:kickoff`
continues; leave `NEEDS-DECISION` lines where the user must decide and say so.

## 5. Synthetic fixtures

Offer to write `scripts/make_fixtures.py`: Faker (`pt_BR` where CPF and
phone shapes matter) generating one file per classified table into
`tests/fixtures/`, deterministic seed, no network. Production rows into
fixtures are denied by `tq/pii-fixture-dump`.

## 6. Commit

```bash
git -C "$root" add docs/privacy.md CLAUDE.md
git -C "$root" commit -m "docs(privacy): data classification and record of processing"
```

Stage `.tentaqles.yaml` only when the manifest lives in this repository. Report
the classes, the paths and tables, the authority and clock, the open
`NEEDS-DECISION` items, and remind the user: `tq allow <workspace>`.
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/classify-data/SKILL.md plugin/tests/test_process_skills.py
git commit -m "feat(skills): classify-data (manifest data block, privacy record, synthetic fixtures)"
```

---

### Task 6: `privacy-review` skill

**Files:**
- Create: `plugin/skills/privacy-review/SKILL.md`
- Modify: `plugin/tests/test_process_skills.py`

- [ ] **Step 1: Write the failing test**

```python
def test_privacy_review_selection_rule_text() -> None:
    body = _body("privacy-review")
    for token in ("privacy-reviewer", "references/checklist.md", "classified_paths", "data:", "docs/privacy.md",
                  "record-review", "tq decide triage", "classified_data"):
        assert token in body, token
    assert "only docs/privacy.md" in body or "documentation-only" in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k privacy_review`
Expected: FAIL, file missing.

- [ ] **Step 3: Write the skill**

````markdown
---
name: privacy-review
description: Run the privacy-reviewer agent on a diff that touches classified data (manifest data block paths or tables, or new log fields, events and exports with personal-data shapes) and turn its findings into fixes and privacy-record updates. Used by the build engine's review gate and standalone when the user says "privacy review", "LGPD check", "GDPR check", "is this compliant", or before shipping a change to personal data handling.
argument-hint: [--staged | --range A..B | <slug> <checkpoint id>]
---

# Privacy review

The reviewer is `agents/privacy-reviewer.md`; its checklist is
`references/checklist.md` (rows `P1`…`P11`). This skill decides when it runs,
feeds it, and records the verdict.

## When it runs

- In the build engine: when triage prints the `classified_data` flag, or the
  checkpoint's `changed_files` match the `tq-project` block's `classified_paths`
  or a manifest `data:` table, or the diff adds log statements, analytics
  events or exports whose fields look personal (e-mail, CPF, phone, address,
  birth date, card). A documentation-only diff that changes only docs/privacy.md
  does not select it.
- Standalone: on request, over `--staged`, a `--range`, or the working tree.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
tq decide triage --staged; echo "triage_exit=$?"
git -C "$root" diff --staged --name-only
echo "checklist=$_tqe/skills/privacy-review/references/checklist.md"
```

## Feed the reviewer

Launch the `privacy-reviewer` agent with: the diff (the review bundle path
inside a build, or `git diff --staged` written to a file under your scratchpad
directory, redacted with `tq redact --classes all` first), the list of changed
files, the spec slice (`data_classes_touched`), the paths `docs/privacy.md`,
`.tentaqles.yaml`, `CLAUDE.md`, `docs/LEARNINGS.md`, and the checklist path.
Ask for the review contract output. Never paste `.env*` contents or real rows.

## Record and act

Inside a build (`<slug> <id>` given):

```bash
"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" record-review <slug> <id> --reviewer privacy-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N
```

Then the engine's fixer loop handles FAIL and the human settles BLOCKED with
`build-gate judge`. Standalone: list the findings with `file:line`, fix the
code ones, and update `docs/privacy.md` (and the manifest `data:` block, then
`tq allow`) for the record ones. A `U` row is a question for the DPO or
product owner: write it as a `NEEDS-DECISION` line in `docs/privacy.md`.

Never call a change privacy-reviewed in a PR body unless a recorded
`privacy-reviewer` PASS exists for the current code (or the human's judge).
````

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/privacy-review/SKILL.md plugin/tests/test_process_skills.py
git commit -m "feat(skills): privacy-review (selection rule, reviewer feed, recorded verdict)"
```

---

### Task 7: `supply-chain` and `llm-security-review` skills; `ci-pipeline` wording

**Files:**
- Create: `plugin/skills/supply-chain/SKILL.md`, `plugin/skills/llm-security-review/SKILL.md`
- Modify: `plugin/skills/ci-pipeline/SKILL.md` (section 4 first bullet)
- Modify: `plugin/tests/test_process_skills.py`

- [ ] **Step 1: Write the failing tests**

```python
def test_supply_chain_handles_no_actions() -> None:
    body = _body("supply-chain")
    for token in ("references/checklist.md", "SBOM", "CycloneDX", "lockfile", "secret scanning", "Scorecard",
                  "tq/gha-branch-ref", "no GitHub Actions", "GitLab"):
        assert token in body, token


def test_llm_security_review_skill_feeds_the_reviewer() -> None:
    body = _body("llm-security-review")
    for token in ("llm-security-reviewer", "references/checklist.md", "agentic", "record-review", "MCP", "hooks", "tentaqles itself"):
        assert token in body, token


def test_ci_pipeline_says_branch_refs_are_denied() -> None:
    body = _body("ci-pipeline")
    assert "tq/gha-branch-ref" in body and "denies" in body
```

- [ ] **Step 2: Run to verify failure**

Run: `cd plugin && python -m pytest tests/test_process_skills.py -q -k "supply_chain or llm_security or ci_pipeline_says"`
Expected: FAIL.

- [ ] **Step 3: Write the skills and the wording**

`plugin/skills/supply-chain/SKILL.md`:

````markdown
---
name: supply-chain
description: Harden a repository's software supply chain, extending ci-pipeline: pin every GitHub Action to a commit SHA, set least-privilege workflow permissions, enforce lockfile-only installs and dependency audits, generate a CycloneDX or SPDX SBOM on release with the CISA 2026 minimum elements, require secret scanning, and report an OpenSSF Scorecard-style checklist. Use during /tentaqles:kickoff, when the user says "harden CI", "supply chain", "SBOM", "pin actions", "Scorecard", or when tq's guard flags an unpinned or branch-pinned action.
argument-hint: [review | apply]
---

# Supply chain

`ci-pipeline` sets up the pipeline; this skill hardens what runs inside it and
what ships out of it. Checklist: `references/checklist.md` (rows `C1`…`C10`).

## 1. Detect

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
ls "$root/.github/workflows" 2>/dev/null || echo "no GitHub Actions"
ls "$root/.gitlab-ci.yml" "$root/azure-pipelines.yml" "$root/Jenkinsfile" 2>/dev/null
ls "$root"/package-lock.json "$root"/pnpm-lock.yaml "$root"/yarn.lock "$root"/uv.lock "$root"/poetry.lock "$root"/go.sum "$root"/Cargo.lock 2>/dev/null
grep -rn "uses:" "$root/.github/workflows" 2>/dev/null | grep -vE "@[0-9a-f]{40}" || true
```

With **no GitHub Actions**: skip rows C1 to C3 with the reason "no Actions
workflows" and still apply C4 to C10 (lockfiles, audit, SBOM, secret scanning,
signing, branch protection). For GitLab CI or Azure Pipelines, pin included
templates and images by digest (C10) and use the platform's secret scanning.

## 2. Apply (or review)

- **C1 pinning**: replace every `uses: owner/repo@vN` or `@main` with the full
  commit SHA of the release and the version in a trailing comment (`# v4.2.2`).
  Get the SHA with `gh api repos/<owner>/<repo>/git/ref/tags/<tag> --jq .object.sha`
  (for annotated tags follow `.object.url` once more). tq **denies** `@main`
  and `@master` (`tq/gha-branch-ref`) and asks on `@latest` (`tq/gha-unpinned`).
- **C2 permissions**: `permissions: contents: read` at the top of every
  workflow; grant `id-token: write`, `pull-requests: write` or `packages: write`
  per job only. `persist-credentials: false` on every checkout.
- **C3 untrusted input**: never `pull_request_target` with a PR-head checkout;
  pass `github.event.*` values through `env:` not `run:` interpolation.
- **C4 lockfile-only**: `npm ci`, `pnpm install --frozen-lockfile`,
  `uv sync --frozen`, `go mod verify`; commit the lockfile.
- **C5 audit and updates**: an audit step (`npm audit --audit-level=high`,
  `pip-audit`, `govulncheck ./...`) and a Dependabot or Renovate config with
  grouped weekly updates that also bumps action SHAs.
- **C6 SBOM**: on release, generate CycloneDX (or SPDX) with a pinned tool
  (`cyclonedx-npm`, `cyclonedx-py`, `syft`), covering the CISA 2026 minimum
  elements (component name, version, supplier, unique identifier, dependency
  relationships, author, timestamp, licence, hash), and attach it to the release.
- **C7 secret scanning**: enable the platform scanner and push protection, or a
  pinned gitleaks action as a required check; locally tq's `commit-secret` rule
  already scans commits.
- **C8 attestations**: build in CI; sign or attest release artifacts
  (`actions/attest-build-provenance` pinned, or Sigstore `cosign`).
- **C9 branch protection**: required checks, reviews, no force push to the
  default branch (tq denies it locally too).
- **C10 images and toolchains**: `FROM image@sha256:...`, exact tool versions.

Run `tq decide triage --staged` on the workflow changes (they come back
`high`); the `security-reviewer` runs in the build engine for workflow diffs.

## 3. Report

A table of the ten rows with `done | skipped (reason) | todo`, then commit
explicit paths (`.github/`, lockfiles, `dependabot.yml`, release workflow):

```bash
git -C "$root" add .github
git -C "$root" commit -m "ci(supply-chain): pin actions, least-privilege permissions, SBOM on release"
```
````

`plugin/skills/llm-security-review/SKILL.md`:

````markdown
---
name: llm-security-review
description: Run the llm-security-reviewer agent on a diff that adds or changes LLM calls, prompts, agent tool definitions, MCP servers, RAG or vector indexing, or Claude Code hooks and skills, against the OWASP Top 10 for LLM Applications (2025) and for Agentic Applications (2026), and turn its findings into fixes. Used by the build engine on the agentic triage flag and standalone when the user says "LLM security", "prompt injection review", "review this agent", "is this MCP server safe", including for tentaqles itself.
argument-hint: [--staged | --range A..B | <slug> <checkpoint id>]
---

# LLM and agent security review

Reviewer: `agents/llm-security-reviewer.md`; checklist:
`references/checklist.md` (rows `L1`…`L12`). Applies to this plugin and `tq`
as much as to client code: every hook, skill, memory write and Jev call here is
in scope.

## When

- In the build engine: triage printed the `agentic` flag, or the diff touches
  prompt files, tool or MCP definitions, `hooks.json`, `skills/`, `agents/`,
  RAG or embedding code.
- Standalone on request.

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
tq decide triage --staged; echo "triage_exit=$?"
echo "checklist=$_tqe/skills/llm-security-review/references/checklist.md"
```

## Feed the reviewer

Launch `llm-security-reviewer` with the diff (bundle path or a redacted diff
file under your scratchpad), the changed files, the spec slice, `CLAUDE.md`,
`docs/LEARNINGS.md`, `docs/threat-model.md` and the checklist path. Point it
at the trust boundaries the change crosses (model provider, MCP servers, hooks,
memory store). Ask for the review contract output.

## Record and act

Inside a build: `"$TENTAQLES_PY" "$_tqe/scripts/build-gate.py" record-review <slug> <id> --reviewer llm-security-reviewer --result PASS|FAIL|BLOCKED --blockers N --majors N --minors N --unjudged N`,
then the fixer loop or `build-gate judge` as usual. Standalone: fix the
findings; a `U` row about trust in a provider or an MCP server is a decision
for the user (record it as an ADR when taken). A finding against hooks or the
guard is a BLOCKER in tentaqles' own repositories: hooks must fail closed and
never widen permissions.
````

In `plugin/skills/ci-pipeline/SKILL.md` section 4, replace the first bullet with:

```markdown
- Pin every third-party action to a full commit SHA with the version in a
  comment; let Dependabot/Renovate bump them. tq's guard denies `@main` and
  `@master` in workflow files (`tq/gha-branch-ref`) and asks on `@latest`
  (`tq/gha-unpinned`). `/tentaqles:supply-chain` applies the full checklist.
```

- [ ] **Step 4: Run the tests**

Run: `cd plugin && python -m pytest tests/test_process_skills.py tests/test_engineering_skills.py tests/test_skill_frontmatter.py tests/test_skill_bootstrap_snippet.py -q`
Expected: pass (`test_engineering_skills.py` checks `ci-pipeline` links and SHA pins; the new sentence keeps both).

- [ ] **Step 5: Commit**

```bash
git add plugin/skills/supply-chain/SKILL.md plugin/skills/llm-security-review/SKILL.md plugin/skills/ci-pipeline/SKILL.md plugin/tests/test_process_skills.py
git commit -m "feat(skills): supply-chain and llm-security-review; ci-pipeline notes branch refs are denied"
```

---

### Task 8: Docs, changelog, release 0.12.0

**Files:**
- Modify: `plugin/CHANGELOG.md`, `plugin/README.md`, `plugin/CLAUDE.md`, `plugin/.claude-plugin/plugin.json`, `plugin/.claude-plugin/marketplace.json`

- [ ] **Step 1: Changelog**

Under `## [Unreleased]` (created by the spine plan) add:

```markdown
- **Security and privacy pack.** Skills `threat-model`, `classify-data`,
  `privacy-review`, `supply-chain`, `llm-security-review`; agents
  `security-reviewer`, `privacy-reviewer`, `llm-security-reviewer` (read-only)
  and `breaker` (localhost only); templates for the threat model, the privacy
  record and the incident playbook; checklists with stable row ids (ASVS 5.0 /
  OWASP Top 10:2025, LGPD/GDPR, OWASP LLM 2025 and Agentic 2026, SLSA/SBOM).
  Requires tq with the manifest `data:` block, `tq redact`, the
  `classified_data` and `agentic` triage flags and the new guard rules
  (`tq/classified-read`, `tq/pii-fixture-dump`, `tq/iac-apply`,
  `tq/remote-migration`, `tq/settings-widen`, `tq/settings-shell-write`,
  `tq/gha-branch-ref`, `tq/commit-message`).
```

Then rename `## [Unreleased]` to `## [0.12.0] — <date>` once both companion plans are merged.

- [ ] **Step 2: README and plugin CLAUDE.md**

README skills table: rows for `/tentaqles:threat-model`, `/tentaqles:classify-data`, `/tentaqles:privacy-review`, `/tentaqles:supply-chain`, `/tentaqles:llm-security-review`; the `/tentaqles:build` row names the crew agents. Hooks table: the `PreToolUse` row mentions the data-protection rules and `tq redact run`. `plugin/CLAUDE.md`: list the new agents and the `references/` checklists; add the rule "reviewer agents are read-only; `breaker` is the only agent with Bash and binds to 127.0.0.1".

- [ ] **Step 3: Version bump and full verification**

Set `"version": "0.12.0"` in `plugin/.claude-plugin/plugin.json` and `plugin/.claude-plugin/marketplace.json`. Run:

```bash
cd plugin && python -m pytest tests -q
cd .. && go test ./...
```

Expected: green. Record the counts in the commit body.

- [ ] **Step 4: Commit**

```bash
git add plugin/CHANGELOG.md plugin/README.md plugin/CLAUDE.md plugin/.claude-plugin/plugin.json plugin/.claude-plugin/marketplace.json
git commit -m "release(plugin): 0.12.0 development process spine and security/privacy pack"
```

Publishing (subtree split to `tentaqles/tentaqles-plugin`, reinstall per identity) follows the procedure used for 0.11.1 and is run only when the user asks.

---

## Self-review notes

- Spec coverage: §4.3 threat model, privacy record, incident playbook (Task 1); §6 rows threat-model, classify-data, privacy-review, supply-chain, llm-security-review (Tasks 4 to 7); §7 rows security-reviewer, privacy-reviewer, llm-security-reviewer, breaker with checklist sources (Tasks 2, 3); §11.3 synthetic fixtures (Task 5); §11.4 (Task 1, 5); §13 release 1 docs and version (Task 8).
- Open point resolution: `breaker` runs only when a checkpoint is marked `runnable` and Project config has `run` (Task 3, spine Task 7).
- Type consistency with the spine: reviewer names match gate `REVIEWERS`; verdict grammar matches `templates/process/review-contract.md`; `classified_paths` key matches `project.py`; checklist row prefixes `S`, `P`, `L`, `C`, `B` are distinct from the contract's `F`/`U` rows.
