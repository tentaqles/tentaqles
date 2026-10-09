# Sessions, tokens and browser defenses

## Server-side sessions vs JWT

| | Server-side session (opaque id in cookie) | Stateless JWT access token |
|---|---|---|
| Revocation | instant: delete the row / key | not until `exp`, unless you add a denylist (which makes it stateful) |
| Storage | session store (DB, Redis) | none server-side |
| Size on the wire | small id | hundreds of bytes to kB |
| Best for | web apps with their own backend, admin panels | service-to-service, mobile/SPA calling several APIs, IdP-issued tokens |
| Main risks | session fixation (rotate id on login), store availability | long-lived tokens, `alg` confusion, tokens in `localStorage` stolen by XSS |

Default: a server-side session for a classic web app; short-lived JWT + rotating
refresh token when an IdP issues them or several services must validate
without a shared store. Do not build "JWT in localStorage with a 30-day
expiry".

Session cookie: `HttpOnly; Secure; SameSite=Lax; Path=/`, `__Host-` prefix
where possible, rotate the session id on login and privilege change, idle
timeout (e.g. 30 min) plus absolute timeout (e.g. 12 h-30 days by risk),
server-side invalidation on logout and password change.

## JWT validation checklist

- Algorithm allow-list (`RS256`/`ES256`/`EdDSA`, or `HS256` with a strong
  server-only key); reject `none`; never pick the algorithm from the token
  header alone.
- Verify signature, `exp`, `nbf`, `iss`, `aud` on every request. Allow a
  small clock skew (<= 60 s).
- Fetch the IdP's JWKS with caching and handle key rotation (`kid`).
- Keep claims minimal; no PII or secrets in the payload (it is only encoded).
- Supabase: verify on the server with `supabase.auth.getUser()` /
  `getClaims()`, not by trusting a session object read from client storage.

## Refresh-token rotation

1. Issue an access token (5-15 min) and a refresh token (opaque, random,
   days to weeks), the refresh token in an `HttpOnly` cookie or secure
   device storage on mobile.
2. Store only a **hash** of the refresh token, with a family id, user id,
   device info and expiry.
3. On refresh: verify, **issue a new refresh token, mark the old one used**.
4. If a used token is presented again, assume theft: **revoke the whole
   family** and force re-login.
5. Revoke all families on password change, MFA reset or "log out everywhere".

## CSRF

- Needed when the browser sends credentials automatically (cookies, basic
  auth) on state-changing requests.
- Layers: `SameSite=Lax` (or `Strict`) cookies; a synchronizer or
  double-submit CSRF token on forms and non-GET requests; verify `Origin` /
  `Sec-Fetch-Site` headers; never change state on GET.
- Frameworks: use the built-in protection (Django, Rails, Laravel, ASP.NET
  antiforgery, Next.js Server Actions' origin check) instead of writing your own.
- APIs authenticated only by an `Authorization: Bearer` header are not
  CSRF-prone; CORS still must not reflect arbitrary origins with credentials.

## Rate limiting and brute force

- Login: per account (e.g. 5-10 failures then exponential backoff or a
  temporary lock) **and** per IP / device (credential stuffing spreads
  across accounts).
- Signup, password reset, magic-link send, MFA verify, and any endpoint that
  sends email/SMS: per IP and per target to stop enumeration and cost abuse.
- Store counters in a shared store (Redis, DB), not process memory, so
  limits hold across instances.
- Return `429` with `Retry-After`; same message for unknown and known
  accounts on reset ("if the account exists, we sent an email").
- Add CAPTCHA or proof-of-work only after a threshold, not on every login.
- Check new passwords against a breached-password list (k-anonymity API or
  local list); minimum length 8 with MFA or 15 without, maximum at least 64,
  no composition rules or forced periodic rotation.

## MFA

- TOTP or passkeys/WebAuthn; SMS only as a fallback.
- Required for admins and for sensitive actions (step-up auth).
- Recovery codes: generated once, stored hashed, single use.
- Rate-limit MFA verification; bind the MFA challenge to the pending login.

## Supabase RLS patterns

```sql
ALTER TABLE public.documents ENABLE ROW LEVEL SECURITY;

CREATE POLICY documents_select_own ON public.documents
  FOR SELECT TO authenticated
  USING (owner_id = (select auth.uid()));

CREATE POLICY documents_insert_own ON public.documents
  FOR INSERT TO authenticated
  WITH CHECK (owner_id = (select auth.uid()));

CREATE POLICY documents_update_own ON public.documents
  FOR UPDATE TO authenticated
  USING (owner_id = (select auth.uid()))
  WITH CHECK (owner_id = (select auth.uid()));
```

- Index the columns policies filter on (`owner_id`, `tenant_id`).
- Multi-tenant: resolve membership through a table
  (`exists (select 1 from memberships m where m.tenant_id = documents.tenant_id and m.user_id = (select auth.uid()))`),
  not from user-editable `user_metadata`; use `app_metadata` or a custom
  access-token hook for role claims.
- Views run with the creator's rights unless `security_invoker = true`.
- `SECURITY DEFINER` functions bypass RLS: set `search_path = ''`, check the
  caller inside, and keep them out of exposed schemas unless intended.
- Storage buckets have their own policies on `storage.objects`.
- The service-role key is server-only. The anon/publishable key is public by
  design and safe only because RLS is on.
