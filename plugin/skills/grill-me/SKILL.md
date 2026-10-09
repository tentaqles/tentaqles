---
name: grill-me
description: Interview the user about every business-rule branch of a feature before writing code - edge cases, invalid states, permissions, time zones and dates, money and rounding, concurrency, failure handling - then write the answers into a spec section and a "verifier first" list of tests that prove each rule. Use when about to implement a feature with domain logic (billing, scheduling, permissions, workflows, imports, n8n automations, state machines), when requirements are vague, when the user says "grill me", "interview me", "ask me questions first", "what am I missing", "spec this out", or wants to check they understand a plan or an AI-written PR.
---

# Grill Me

Fast code generation removed the thinking time that slow coding used to
force. Put it back up front: the agent questions the human about each rule
and branch, so the human, not the model, decides the domain behaviour. The
output is a spec the code must satisfy and the tests that prove it.

## Rules of the interview

- **One question at a time**, each tied to a concrete branch: "When a
  subscription is cancelled mid-cycle, do we refund the unused days, credit
  them, or nothing?" Offer the likely options and a recommended default.
- **Never invent a business rule.** If the user does not know, record it as
  an open question with a safe default and who decides; do not silently pick.
- Ask about what the **code will branch on**: every `if`, status, role,
  limit, date comparison and rounding step you expect to write.
- Push on vague answers ("it should just work", "normal users") with a
  concrete example: specific user, amount, date, time zone.
- Stop when every branch has an answer or an owner. Aim for 5-15 questions;
  group trivial ones.
- For an existing plan or AI-written PR, quiz the user on the rules the code
  introduced ("this rejects refunds after 30 days: is 30 from purchase or
  delivery?") to check understanding instead of reading every line.

## Question areas

Go through each area that applies. The full bank with examples is in
[references/question-bank.md](references/question-bank.md).

1. **Actors and permissions:** who can do this, on whose data, what an admin, a
   suspended user or another tenant sees.
2. **States and invalid states:** the lifecycle, allowed transitions,
   what must be impossible, what happens to in-flight items on a change.
3. **Edge cases and limits:** empty, one, max, duplicates, very large input,
   partial input, unicode, deleted references.
4. **Time and dates:** which time zone, "end of day" for whom, DST, month
   ends, leap years, business days, expiry boundaries (inclusive?).
5. **Money and numbers:** currency, minor units, rounding mode and where it
   happens, taxes, discounts order, negative totals, refunds, splits that do
   not divide evenly.
6. **Concurrency:** two users or two requests at once, double submit,
   retries, webhooks arriving twice or out of order, last write wins or not.
7. **Failure and recovery:** a dependency is down, a step half-completes,
   what the user sees, what is retried, who is notified.
8. **Data lifecycle and audit:** what is kept, for how long, who changed what.

## Output

When the interview ends, write the answers into the project's spec (create
one from [references/spec-template.md](references/spec-template.md) if none
exists, e.g. `docs/specs/<feature>.md`), in this shape:

```markdown
## Business rules

| ID | Rule | Example | Decided by |
|---|---|---|---|
| R1 | Refunds allowed up to 30 days after delivery (inclusive, customer's time zone) | delivered 2026-03-01 -> refundable through 2026-03-31 23:59 local | user, 2026-10-09 |

## Open questions
- Q1: Partial refunds for bundles? Default until decided: not allowed. Owner: product.

## Verifier first

| Rule | Test that proves it | Type |
|---|---|---|
| R1 | `refund allowed on day 30 at 23:59 local, rejected on day 31 00:00` | unit |
| R1 | `refund window uses customer TZ, not server TZ` | unit |
```

**Verifier first:** write (or at least name) these tests before the
implementation, and make "all verifier tests pass" the exit condition of
any agent loop on this feature. Each rule gets at least one test at its
boundary; a rule without a test is not done. Prefer property-based tests for
money, dates and invariants.

## Pointers

- Rules that touch auth, money, schema or data deletion are high risk:
  `tq decide triage` flags them, and they deserve the full review.
- Schema implied by the rules: `db-migration`. Architecture-level questions
  (load, failure modes, cost): `system-design-review`.
- Record the decisions with `/tentaqles:session-wrap` so the next session
  knows why a rule exists.
