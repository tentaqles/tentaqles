# Auth review checklist (OWASP ASVS-style)

Loosely follows OWASP ASVS chapters. Mark each item pass / fail / n/a and
report every fail with severity, location and fix.

Severity guide: **critical** = account takeover, data of other users or
tenants exposed, secret exposed; **high** = a control missing on a sensitive
path (no rate limit on login, no RLS on a table holding user data);
**medium** = weakened control (long-lived tokens, weak params); **low** =
hardening.

## V2 Authentication

- [ ] Uses a managed IdP or established framework; no hand-rolled auth or crypto.
- [ ] Passwords hashed with argon2id (or scrypt/bcrypt/PBKDF2 at the stated minimums) via a library; salt from the library.
- [ ] Encoded hash with parameters stored; rehash on login when parameters are outdated.
- [ ] Pepper (if any) read from a secret store, versioned.
- [ ] No plaintext or reversibly encrypted password column anywhere (including legacy/import tables).
- [ ] Password policy: min length 8 (15 without MFA), max >= 64, breached-password check, no composition rules or forced rotation.
- [ ] Uniform error and timing for unknown user vs wrong password; reset does not reveal account existence.
- [ ] Login, signup, reset, magic-link and MFA endpoints rate-limited per IP and per account, with counters in a shared store.
- [ ] MFA available; required for admins and step-up for sensitive actions; recovery codes hashed and single use.
- [ ] Reset / magic-link / email-verify tokens random (>= 128 bits), single use, short TTL, stored hashed.
- [ ] Changing email or password requires the current password or recent re-auth, and notifies the user.

## V3 Session management

- [ ] Session ids / refresh tokens random, opaque, stored hashed server-side.
- [ ] Cookies `HttpOnly`, `Secure`, `SameSite=Lax|Strict`; `__Host-` prefix where possible.
- [ ] Session id rotated on login and privilege change; invalidated on logout and password change.
- [ ] Idle and absolute timeouts defined.
- [ ] JWT: algorithm allow-list, `exp`/`iss`/`aud` verified, short access-token lifetime, no secrets/PII in claims.
- [ ] Refresh tokens rotate on use; reuse revokes the family.
- [ ] Tokens not stored in `localStorage`/`sessionStorage` in apps exposed to XSS.

## V4 Access control

- [ ] Every route, server action, RPC and webhook enforces authentication server-side (deny by default).
- [ ] Every resource access checks ownership / tenant (no IDOR); ids from the client are never trusted alone.
- [ ] Role checks centralized and tested, including "user of another tenant" and "unauthenticated" cases.
- [ ] Admin functions on separate routes with stronger auth; no hidden-but-reachable admin endpoints.
- [ ] Supabase: RLS enabled on every table in exposed schemas, with policies per operation and `with check` on writes.
- [ ] Supabase: service-role key only in server code; no secret in `NEXT_PUBLIC_*`, `VITE_*`, `EXPO_PUBLIC_*` or mobile bundles.
- [ ] `SECURITY DEFINER` functions and views reviewed (`search_path`, `security_invoker`).
- [ ] File/object storage access controlled per user/tenant; signed URLs short-lived.

## V5 Input and output

- [ ] Parameterized queries / ORM everywhere; no string-built SQL with user input.
- [ ] Output encoding by the framework; no `dangerouslySetInnerHTML` / `v-html` with user data without sanitizing.
- [ ] Redirect targets validated against an allow-list (no open redirect after login).
- [ ] Webhooks verify the provider's signature and timestamp.

## V6 / V8 Cryptography and data protection

- [ ] TLS everywhere; HSTS on web apps.
- [ ] No custom crypto; keys from a KMS or secret store; algorithms current.
- [ ] Sensitive data (tokens, passwords, PII) never logged; logs redact `Authorization`, cookies and known secret fields.
- [ ] Backups and exports of user data access-controlled.

## V13 / V14 API and configuration

- [ ] CORS does not reflect arbitrary origins with credentials.
- [ ] CSRF protection on cookie-authenticated state-changing requests.
- [ ] Security headers: CSP (at least `frame-ancestors`), `X-Content-Type-Options`, `Referrer-Policy`.
- [ ] Secrets only via env or a secret store; `.env` gitignored; `.env.example` holds placeholders only.
- [ ] CI secrets via `${{ secrets.* }}` or OIDC; never echoed; least-privilege workflow permissions.
- [ ] Dependencies audited (`npm audit`, `pip-audit`, `govulncheck`) in CI.
- [ ] Error responses do not leak stack traces or internal ids in production.

## Report format

```markdown
| # | Severity | ASVS area | Finding | Location | Fix |
|---|---|---|---|---|---|
| 1 | critical | V4 | `/api/invoices/:id` loads by id without tenant check | src/api/invoices.ts:42 | filter by `tenant_id` from the session; add a cross-tenant test |
```
