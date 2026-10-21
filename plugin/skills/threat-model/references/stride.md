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
