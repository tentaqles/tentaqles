---
name: n8n-triage
description: Diagnose and fix a failing n8n workflow safely — read-only investigation first (failed executions, the error node, its input data, the last good run) through the n8n MCP read tools, then a proposed fix applied to an inactive draft copy, hardened with credential references, idempotency and an audit log, validated on the copy, promoted to the live workflow only after explicit confirmation, and optionally codified as JSON in the repo. Use when the user says "n8n workflow is failing", "check n8n errors", "why did this n8n execution fail", "fix the n8n workflow", "n8n monitor is red", "/n8n-triage", or shares an n8n execution or workflow link.
---

# n8n Triage

Find out why an n8n workflow fails and fix it without breaking production on
the way. The order is fixed: **read → diagnose → fix a copy → validate →
promote with confirmation → codify.**

Assume the instance is **production** unless the user or the workspace config
says it is a sandbox. Live workflows move real data and call real services.

Two guardrails may be in place when tq is installed: its guard asks before
n8n MCP writes (create, update, publish, execute, delete), and the optional
Jev rule `jev/n8n-inline-credentials` may flag a node that inlines a
credential. Both match on MCP tool names, so whether they fire depends on how
the server names its tools, and neither sees REST calls made from the shell.
Treat them as a second line, never as the plan: this skill's own
confirmation steps apply either way.

## 0. Find the tools

n8n MCP servers name their tools differently. Look for the n8n tools that are
loaded (search the tool list for `n8n`) and sort them into:

- **read:** list/search workflows, get workflow, list/search executions, get
  execution, validate workflow
- **write:** create, update, activate/publish, execute/run, delete, archive

Use only read tools until step 4. If no n8n MCP server is connected, use the
n8n REST API through tq so the key never shows up in the conversation (never
read or print the `.env`):

```bash
tq dotenv run -- bash -c 'curl -s -H "X-N8N-API-KEY: $N8N_API_KEY" "$N8N_BASE_URL/api/v1/executions?status=error&limit=5"'
```

(`N8N_API_KEY` and `N8N_BASE_URL` are examples. Take the real names from the
project's `.env.example` or docs; templates are readable, the `.env` is not.)

The REST fallback is **read-only (GET)**. No guard sees these calls, so every
POST, PUT, PATCH or DELETE needs the user's explicit confirmation each time,
under the same rules as steps 4 and 6.

## 1. Read (no changes)

1. **Identify the workflow** by name, id or the link the user gave. Note
   whether it is active and what triggers it (schedule, webhook, another
   workflow).
2. **List recent failed executions**, small pages: `status=error`, limit 5-10,
   for that workflow. Execution fetches with full data time out on big runs,
   so list first and ask for the summary before the full data.
3. **Open the latest failure** and record: execution id, start time, the
   **error node**, the error message and code, and the input items that node
   received (just enough items to see the shape).
4. **Open the last successful execution** of the same workflow and compare the
   error node's input. A changed field, an empty list or a new type is often
   the whole story.
5. **Get the workflow definition** and read the error node plus its upstream
   nodes: parameters, expressions, retry settings, credentials used.
6. **Frequency:** one failure, every run since a given time, or intermittent?
   That points to data, a deploy or change, or an external service.

Execution data often holds personal data, tokens and connection strings.
Summarize it; don't paste raw payloads into the conversation, files or PRs.

## 2. Diagnose

State the root cause in one or two sentences with evidence (execution id, node,
error, the input field that differs). Common causes:

| Symptom | Usual cause |
|---|---|
| 401 / 403 from an HTTP or app node | expired or rotated credential, wrong scope; **the user** renews it in n8n, never you |
| 429, timeouts, 5xx | rate limit or upstream outage: retry with backoff, batching |
| "Cannot read properties of undefined" in Code/Set nodes | upstream shape changed, or an empty item list |
| duplicate rows or emails | a retried or re-run execution with no idempotency |
| works manually, fails on schedule | different input (pinned data hid it), timezone, or concurrency |

If the cause is outside the workflow (a credential to renew, a vendor outage,
a quota), stop here and tell the user what to do. There is nothing to deploy.

## 3. Propose the fix

Show the change as a before/after of the affected nodes (parameters and
expressions, not the whole workflow JSON). While you are in there, apply the
hardening below to the nodes you touch and flag the rest as follow-ups.

**Credentials by reference.** Nodes use n8n credentials (`credentials: {
<type>: { id, name } }`). No API keys, bearer tokens, passwords or connection
strings in node parameters, headers, query strings or Code nodes. If you find
one, move it to a credential (the user creates it in n8n) and reference it.
Never echo the inline value back, and never rotate it yourself; tell the user
it is exposed.

**Idempotency.** A re-run or retried execution must not double-write: use a
natural dedupe key (source id plus event type or date) and upsert instead of
insert, or check a processed-keys table or the Remove Duplicates node before
side effects (emails, payments, tickets).

**Audit log.** One row per run with the execution id, workflow id, start and
end time, items in and out, status, and the error message on failure. Send it
to a table or sheet the team already uses. Set an **error workflow** (Workflow
settings → Error workflow) so failures notify someone instead of failing
silently.

**Resilience.** Retry on fail with backoff on HTTP and app nodes that call
external services; explicit handling for empty input; timeouts on HTTP nodes.

## 4. Apply to a copy, not the live workflow

Create an **inactive draft copy** (for example `[draft] <name> <date>`) with
the fix:

- leave it **inactive**
- replace the trigger with a **Manual Trigger**, or change the webhook path,
  so the copy can't fire on the live schedule or steal live webhook calls
- point side-effect nodes (writes, emails, payments) at a test target, or
  disable them for the first run
- keep the same credential references; don't create new credentials

If the instance has no safe place for a copy, prepare the fixed workflow JSON
locally and review it with the user instead.

The tq guard may ask before this create. Either way, say what you are
creating and why.

## 5. Validate on the copy

**Before any execution of the draft**, every side-effect node (database
writes, emails, messages, payments, tickets, calls to external write APIs)
must be either disabled or pointed at a test target. The draft uses the live
credentials, so "disabled for the first run" is not enough for the runs
below. If a side effect can't be isolated, skip the runs that would trigger
it, say so, and ask the user before going further.

1. Run the workflow's validate tool, if the server has one.
2. Execute the draft with the failing execution's input (pin a few items,
   with personal data replaced by placeholders) and confirm the error node now
   passes and the output has the expected shape.
3. Run it with a normal, previously working input too, so the fix doesn't
   break the happy path.
4. Run it twice **against the test target** and confirm the idempotency
   guard stops the second run from double-writing. With no test target, skip
   this run and list idempotency as unverified.

Report what ran, with execution ids and outcomes. "Should work" is not
validation.

## 6. Promote, with explicit confirmation

Changing a live production workflow needs the user's explicit yes, given for
this workflow, after they have seen the diff and the validation results. A
general "go ahead" from earlier in the session does not count. Then:

1. Apply the same node changes to the live workflow (the tq guard may ask
   again; your confirmation from the user is what counts).
2. Keep its original trigger and active state.
3. Watch the next execution, or trigger one safely if the user agrees, and
   confirm it succeeds.
4. Delete or archive the draft copy once the user is happy.

Undo plan: keep the pre-change workflow JSON (from step 1) so the live
workflow can be restored exactly.

## 7. Codify (offer)

Once the fix is validated live, offer to put the workflow under version
control:

- export the workflow JSON to the repo (for example `n8n/workflows/<slug>.json`)
- strip `pinData`, execution data and anything instance-specific that isn't
  needed; credentials stay as `{ id, name }` references only
- check the file for secrets before staging it
- commit and open a PR with the **`ship`** skill, linking the failing and
  validating execution ids

From then on, the repo is the source of truth and changes go through review.

## Report

```
Workflow: <name> (<id>), <prod|sandbox>, <active|inactive>
Failure: execution <id> at <time>, node "<node>": <error>
Root cause: <one line, with evidence>
Fix: <what changed> (validated on draft <id>: executions <ids>)
Hardening: credentials <ok|moved N>, idempotency <added|present|follow-up>, audit log <added|present|follow-up>
Live: <unchanged | promoted after confirmation, next run <id> succeeded>
Codified: <path / PR | offered>
```

## Never

- write to a live production workflow without explicit, specific confirmation
- activate the draft copy, or let it share a live webhook path or schedule
- put a credential value into a node, a file, a PR or the conversation, or
  rotate a credential yourself
- paste raw execution payloads with personal data or secrets
- read a `.env` file directly; use `tq dotenv run -- <cmd>`
