---
name: system-design-review
description: Run a structured review of a system design, architecture proposal, RFC or ADR - requirements and SLOs, data model, failure modes and blast radius, idempotency/retries/backpressure, observability, security, cost, migration and rollback - and output a findings table ranked by severity. Use when the user shares a design doc, RFC, architecture diagram or plan for a new service, pipeline, integration or major feature and asks to review it, poke holes in it, or check it is production-ready; or says "review this design", "architecture review", "is this design sound", "what could go wrong" or "RFC review".
---

# System Design Review

Find what will hurt in production before code exists. The output is a
ranked findings table, not a rewrite of the design.

## 1. Get the inputs

Read the design (doc, RFC, diagram, issue, or code if it already exists).
If it lacks any of these, ask before reviewing; a review against unknown
requirements is guesswork:

- What it does and for whom; what is explicitly out of scope.
- Expected load: requests/s or jobs/day, data volume and growth, peak vs average.
- SLOs or at least targets: availability, latency p95/p99, freshness, RPO/RTO.
- Constraints: budget, team size, existing stack, compliance, deadlines.

For a design not yet written, start from the spec template in the `grill-me`
skill (`references/spec-template.md`).

## 2. Review each dimension

Walk through every dimension in
[references/review-checklist.md](references/review-checklist.md). For each,
note what the design says, what it is missing, and what could fail. The
dimensions:

1. Requirements, constraints and SLOs
2. Data model and storage (ownership, consistency, growth, migrations)
3. Failure modes and blast radius (SPOFs, dependencies, partial failure)
4. Idempotency, retries, timeouts and backpressure
5. Observability (logs, metrics, traces, alerts tied to SLOs)
6. Security and privacy (authn/authz, secrets, tenant isolation, PII)
7. Cost (unit cost at expected and 10x load, the expensive component)
8. Migration, rollout and rollback
9. Simplicity: what can be cut or deferred (over-engineering is a finding too)

Use the failure-mode table in the checklist for every external dependency
and every stateful component.

## 3. Rank the findings

| Severity | Meaning |
|---|---|
| **critical** | Data loss or corruption, security breach, cross-tenant exposure, or an SLO that cannot be met by design. Block until fixed. |
| **high** | Likely outage or incident under expected load or a common failure (dependency down, retry storm, deploy mid-migration). Fix before launch. |
| **medium** | Operability or cost problem that will bite at growth or during incidents (no alert, no runbook, unbounded queue). Fix soon. |
| **low** | Hardening, clarity, nice to have. |
| **question** | The design does not say; the answer decides the severity. |

## 4. Output

```markdown
## Design review: <name>

**Verdict:** ready | ready with fixes | needs another pass
**Assumed load / SLOs:** <what the review assumed>

| # | Severity | Area | Finding | Impact | Recommendation |
|---|---|---|---|---|---|
| 1 | critical | Idempotency | Payment webhook handler inserts on every delivery | duplicate charges on provider retries | dedupe on provider event id with a unique constraint; return 200 for replays |

**Open questions:** ...
**What is good:** <1-3 bullets, so it is not lost in the next revision>
**Deliberately deferred:** <sharding, multi-region, ... and the trigger to revisit>
```

Sort by severity, then by blast radius. Every finding names a concrete
change. If the design is fine for its stated load, say so instead of adding
scale it does not need.

## Pointers

- Schema and migration plans: check them with the `db-migration` skill.
- Auth and tenant isolation: the `auth-security` checklist.
- Once implemented, `tq decide triage --range main..HEAD` tells you whether
  the diff needs a full review.
- Record the accepted design and its tradeoffs as an ADR in the repo and log
  the decision with `/tentaqles:session-wrap`.
