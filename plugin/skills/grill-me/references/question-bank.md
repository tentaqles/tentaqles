# Grill-me question bank

Pick the questions whose answer changes the code. Always ask with a
concrete example and suggested options.

## Actors and permissions

- Who may perform this action? Which roles may not? What about the owner vs a teammate vs an admin vs support staff?
- Can a user act on another user's or another tenant's record through this feature (direct id, shared link, export)?
- What does a suspended, unverified, trial or deleted user see?
- Are there actions that need a second approver or re-authentication?
- Is anything visible but not editable? Editable but not deletable?
- Does an API key / service account / automation (n8n, cron) act as a user or as the system? Whose permissions apply?

## States and invalid states

- List the states. Which transitions are allowed, and who can trigger each?
- Which combinations must be impossible (paid but no invoice, shipped but cancelled)? Enforced by the database or the code?
- Can a state be reversed (un-cancel, re-open)? What happens to side effects already done (emails, charges)?
- What happens to items in flight when the rule or configuration changes (old orders under the old price)?
- What is the initial state of existing records when this ships (backfill)?

## Edge cases and limits

- Zero items, one item, the maximum, max + 1. What is the maximum and why?
- Duplicates: same email with different case, same file twice, same request twice.
- Missing or partial data: optional fields absent, a referenced record deleted, an import row with errors (skip the row, fail the batch?).
- Very long text, unicode, emoji, right-to-left text, leading/trailing spaces.
- Negative numbers, zero quantities, zero prices, 100% discounts.
- What does the user see in each of these cases (message, empty state)?

## Time and dates

- Whose time zone defines "today", "end of month", "expires at": the user's, the tenant's, the server's (UTC)?
- Is a boundary inclusive? "Within 30 days" counted from which event, to which instant?
- What happens on DST change days (a 23- or 25-hour day; a 02:30 that does not exist or occurs twice)?
- Month arithmetic: Jan 31 + 1 month = ? Feb 29 in non-leap years?
- Business days and holidays: whose calendar?
- Recurring schedules: stored as rule + time zone, or as UTC instants?
- Clock skew between services; ordering by created_at vs a sequence.

## Money and numbers

- Currency per tenant, per user or per transaction? Conversions and whose rate, when?
- Stored in minor units (integer cents) or fixed decimal? Never float.
- Rounding mode (half-up, half-even/bankers) and **where**: per line, per tax, per invoice total?
- Order of operations: discount before or after tax? Coupons stack?
- Splits that do not divide evenly (100 / 3): who gets the extra cent?
- Refunds and credits: partial, full, to the original method? Can a total go negative?
- Taxes: inclusive or exclusive prices, per-region rules, exemptions.
- Limits and quotas: counted when (on request, on success)? Reset when, in which time zone?

## Concurrency

- Two users edit the same record: last write wins, optimistic locking (version column), or merge?
- Double-click / double submit / client retry: idempotency key? What does the second request return?
- Two workers pick the same job: locking (`FOR UPDATE SKIP LOCKED`), unique constraints?
- Webhooks or events delivered twice or out of order: dedupe by event id? Ignore older updates?
- Inventory, seats, balances: can two purchases both take the last unit? Enforced with a constraint or atomic update?
- Long-running operations: what if the user changes the input while it runs?

## Failure and recovery

- A dependency (payment provider, email, LLM API, n8n webhook) is down or slow: fail, queue and retry, or degrade?
- A multi-step operation fails at step N: roll back, compensate, or resume? Is the user told?
- Retries: how many, with what backoff, and what if the side effect actually happened (charged but timeout)?
- Who is alerted, and what can an operator do (replay, manual fix)?
- What is logged for debugging without logging personal data or secrets?

## Data lifecycle and audit

- What is kept, and for how long? Soft delete or hard delete? What does "delete my account" remove?
- Who can see the history? Do we need an audit log of who changed what, when?
- Exports and imports: formats, encoding, size limits, partial failures.
- Personal or confidential data: may it be sent to external services (LLM APIs, analytics)?

## Quiz mode (reviewing an existing plan or PR)

For each rule the code introduces, ask the user to predict the behaviour:

- "If a user in UTC-3 cancels at 22:00 on the last day of the trial, are they charged?"
- "This `if (amount > limit)`: is a payment exactly at the limit allowed?"
- "The retry loop runs 5 times: what does the customer see after the 5th failure?"

A wrong or unsure answer is a finding: either the code or the user's model
of it is wrong. Record it and resolve it before merge.
