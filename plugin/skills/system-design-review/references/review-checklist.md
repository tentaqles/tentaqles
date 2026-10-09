# System design review checklist

Ask each question of the design. "Not addressed" is itself a finding (often
a `question` or `medium`).

## 1. Requirements, constraints, SLOs

- Functional scope and explicit non-goals written down?
- Load numbers: average and peak throughput, payload sizes, data volume now and in 12 months?
- SLOs: availability, latency (p95/p99), freshness or end-to-end lag, RPO (data you may lose) and RTO (time to recover)?
- Are the SLOs achievable given the dependencies' own SLAs (availability multiplies across serial dependencies)?
- Constraints: budget, team skills, existing stack, compliance (LGPD/GDPR, HIPAA, PCI), data residency?
- Does the simplest architecture that meets these numbers look like this one? If not, why not?

## 2. Data model and storage

- Who owns each piece of data (single writer)? Source of truth vs caches and copies?
- SQL by default; NoSQL / separate vector DB only with an argument (latency, schema flexibility, write volume). Postgres + pgvector often suffices.
- Consistency needs per operation: strong, read-your-writes, eventual? Replica lag handled for read-after-write?
- Keys and constraints: natural vs surrogate keys, uniqueness enforced in the database, not only in code.
- Indexes for the main access patterns; growth of the biggest tables; retention, archival, deletion (right to be forgotten).
- Large binaries in object storage with the key in the DB, CDN in front if served publicly.
- Multi-tenant isolation: tenant id on every row and query, RLS or schema/database per tenant?
- Schema evolution: can it change with expand/contract without downtime?
- Time: timestamps in UTC (`timestamptz`), money as integer minor units or decimal, never float.

## 3. Failure modes and blast radius

Fill this for every external dependency and stateful component:

| Component | Failure | Detection | Effect on users | Mitigation | Blast radius |
|---|---|---|---|---|---|
| Primary DB | down / failover | health check, error rate | writes fail | managed HA, retry with backoff, read-only mode | all tenants |
| Cache | down / cold | error metric | slower reads | fall through to DB, rate-limit the stampede | latency only |
| Third-party API | slow / 5xx / rate-limited | latency + error metrics | feature degraded | timeout, circuit breaker, queue and retry later, fallback | one feature |
| Queue / worker | backlog | queue depth, oldest message age | delayed jobs | autoscale, DLQ, alert on age | async features |

Also ask:

- Single points of failure named, each mitigated or explicitly accepted?
- Partial failure: what happens when step 3 of 5 fails? Is state left half-written? Saga / outbox / compensation?
- Can one tenant, one bad message or one huge request take down everyone (noisy neighbour, poison message, unbounded query)?
- Bulkheads: separate pools/queues for critical vs bulk work?
- Stateless web tier (no sessions or files in process memory or local disk)?
- Graceful degradation: what still works when a dependency is down?
- Disaster recovery: backups tested by restoring; RPO/RTO met?

## 4. Idempotency, retries, backpressure

- Every consumer of at-least-once delivery (queues, webhooks, cron, n8n triggers, retried HTTP calls) idempotent? Dedupe key + unique constraint or processed-events table?
- Client-facing mutating APIs that may be retried accept an idempotency key?
- Retries: bounded, exponential backoff with jitter, only on retryable errors, total time budget below the caller's timeout?
- Timeouts set on every network call (DB, HTTP, LLM), shorter than the upstream timeout?
- Circuit breakers or fail-fast for flaky dependencies; no retry storms (retries at several layers multiply)?
- Backpressure: bounded queues, concurrency limits, rate limits to downstream APIs (including LLM/token limits), load shedding with `429`/`503`?
- Dead-letter queue with alerting and a replay procedure?
- Ordering guarantees needed? If so, per key, and how is reordering handled?
- Exactly-once claims: usually at-least-once + idempotency in practice; say which.

## 5. Observability

- Structured logs with request/trace ids and tenant id; no secrets or PII in logs.
- Metrics: RED (rate, errors, duration) per endpoint; USE for resources; queue depth and oldest-message age; business metrics (orders/min, docs ingested).
- Distributed tracing across services, queues and LLM/tool calls (OpenTelemetry; Langfuse/Phoenix for LLM apps) with model, prompt version, tokens, cost.
- Alerts tied to SLOs (burn rate), each with an owner and a runbook; no alerts nobody acts on.
- Health checks: liveness vs readiness (readiness checks the DB).
- Silent-failure guards: a job that "succeeds" processing 0 items alerts.
- Audit log for security-relevant actions.

## 6. Security and privacy

- Authentication method and who owns it (IdP vs in-house)? See `auth-security`.
- Authorization enforced server-side on every entry point, including internal services, webhooks and workers.
- Tenant isolation tested; least-privilege service accounts and DB roles.
- Secrets in a secret store / env, never in code, images or client bundles; rotation possible.
- Data classification: PII, credentials, client confidential data; encryption in transit and at rest; retention and deletion.
- LLM / RAG: prompt injection from ingested documents, PII redaction before indexing or sending to external APIs, per-client data policy (local-only vs API allowed).
- Input validation at trust boundaries; webhook signatures verified; SSRF on any URL-fetching feature.
- Network: only the load balancer / edge public; app servers and databases private; WAF and rate limiting at the edge.

## 7. Cost

- Unit cost per request / job / tenant at expected load and at 10x.
- The most expensive component (often LLM tokens, egress, managed DB tier, always-on compute) and a lever to reduce it (caching, batching, smaller model, serverless for spiky load).
- Hard limits or budget alerts on usage-priced services (LLM APIs, serverless invocations, log ingestion).
- Cost of observability itself (log volume, trace sampling).
- Serverless vs server: spiky and low traffic -> serverless; constant load, CPU-heavy or long jobs -> server/containers.

## 8. Migration, rollout, rollback

- Path from current state: data migration plan, backfill, dual-write or dual-read period (expand/contract).
- Rollout: feature flags, canary or percentage rollout, tenant-by-tenant; deploy is separate from release.
- Rollback: can the previous version run against the new schema? What is irreversible (data drops, external side effects like emails or payments) and how is it guarded?
- Compatibility: API versioning, old mobile clients, consumers of events or webhooks.
- Cutover runbook with owner, checkpoints, success criteria and a go/no-go decision.
- Backfill and reprocessing: can you rebuild derived data (search index, embeddings) from the source of truth?

## 9. Simplicity

- Can a monolith, a single Postgres, a cron job or a managed service replace a proposed component?
- Microservices, Kafka, sharding, multi-region or Kubernetes without load evidence: flag as over-engineering (`low` or `medium` by cost).
- What is deliberately deferred, and what metric triggers revisiting it?
