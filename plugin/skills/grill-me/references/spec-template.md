# Spec / RFC template

Copy to `docs/specs/<feature>.md` (or the project's RFC folder). Keep it
language-agnostic: inputs, outputs, rules and invariants, not code. Short
is fine; empty sections are not: write "n/a" with a reason.

```markdown
# <Feature name>

**Status:** draft | in review | accepted | implemented | superseded by <link>
**Author:** <name>    **Reviewers:** <names>    **Date:** <YYYY-MM-DD>
**Links:** issue / ticket, designs, related specs

## 1. Problem and goal
What problem, for whom, and how we will know it is solved (a measurable outcome).

## 2. Scope
**In scope:**
- ...
**Out of scope (explicitly):**
- ...

## 3. Users, roles and permissions
| Role | Can | Cannot |
|---|---|---|

## 4. Inputs and outputs
For each entry point (UI action, API endpoint, job, webhook, n8n trigger):
| Entry point | Input (fields, types, constraints) | Output / side effects | Errors |
|---|---|---|---|

## 5. Business rules
| ID | Rule | Example | Decided by |
|---|---|---|---|
| R1 | | | |

## 6. States and invariants
States, allowed transitions, and what must always be true (e.g. "an invoice
total equals the sum of its lines", "a seat is assigned to at most one user").

## 7. Edge cases
Empty / max / duplicates / time zones / rounding / concurrency / partial failure:
the decisions taken for each one that applies.

## 8. Data model changes
Tables/collections, columns, constraints, indexes; migration approach
(expand/contract), backfill, retention. Link the migration PR.

## 9. Non-functional requirements
Load (avg/peak), latency and availability targets, data volume, security and
privacy constraints, cost ceiling.

## 10. Failure handling
Dependencies and what happens when each fails; retries, idempotency keys,
alerts, what the user sees.

## 11. Alternatives considered
| Option | Pros | Cons | Why not |
|---|---|---|---|

## 12. Rollout and rollback
Feature flag, migration order, rollout steps, how to roll back, what is irreversible.

## 13. Verification (verifier first)
| Rule / requirement | Test that proves it | Type (unit / integration / e2e / eval / manual) | Status |
|---|---|---|---|
| R1 | | | todo |

Exit condition for implementation: every row above passes in CI.

## 14. Open questions
| # | Question | Default until decided | Owner | Due |
|---|---|---|---|---|
```
