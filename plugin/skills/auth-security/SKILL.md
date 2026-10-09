---
name: auth-security
description: Design, implement or audit authentication, sessions and authorization - identity provider vs own auth, argon2id password hashing, sessions vs JWT, refresh-token rotation, CSRF, rate limiting, MFA, server-side authorization, Supabase RLS and service-role key handling, and secrets management - ending in an OWASP ASVS-style checklist. Use when building login, signup, password reset, sessions, tokens, API keys, roles or permissions; touching auth middleware, RLS policies or env secrets; or when the user says "auth", "login", "JWT", "hash passwords", "RLS", "permissions", "security review" or "is this secure".
---

# Auth & Security

Authentication mistakes are rare to make and catastrophic when made. Prefer
not writing auth at all; when you must, use well-reviewed libraries with the
parameters below, and enforce authorization on the server for every request.

## 1. Decide who owns auth (in this order)

1. **Managed identity provider** (Supabase Auth, Clerk, Auth0, Cognito,
   Entra ID / B2C, Firebase Auth): default for new apps, B2B SSO (SAML/OIDC),
   MFA, compliance needs, or a small team.
2. **Auth framework** in the app (Better Auth, Auth.js, Django auth, ASP.NET
   Identity, Devise): when data must stay in your database or the IdP's cost
   or lock-in is a problem.
3. **Passwordless** (magic link, passkeys/WebAuthn, OAuth social login):
   preferred for B2C where possible; no password to leak.
4. **Your own password storage**: only with a library, never a hand-rolled
   scheme. If the repo already uses an IdP or framework, do not add
   `bcrypt`/`argon2` code next to it: that is a smell.

## 2. Passwords (only if you store them)

- **argon2id**, via the platform's maintained library, with at least OWASP's
  minimum: `m = 19456 KiB (19 MiB), t = 2, p = 1`; or `m = 46 MiB, t = 1`;
  or `m = 12 MiB, t = 3`. Tune upward so a hash takes about 0.2-0.5 s on
  production hardware.
- Fallbacks: scrypt `N = 2^17, r = 8, p = 1`; bcrypt cost >= 10 for legacy
  only (72-byte input limit); PBKDF2-HMAC-SHA256 >= 600,000 iterations only
  where FIPS requires it. Never MD5/SHA-x for passwords.
- Store the library's encoded string (`$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>`),
  which carries salt and parameters. The library generates the per-user salt.
- **Rehash on login** when stored parameters are below the current ones.
- Optional pepper: an HMAC key from a secret store (never the DB or code),
  with a version id so it can be rotated without locking everyone out.
- Verify with the library's constant-time `verify`. Same response and similar
  timing for "no such user" and "wrong password". Never log passwords,
  hashes, tokens or reset links.

## 3. Sessions, tokens and browser defenses

Details and tradeoffs: [references/sessions-tokens.md](references/sessions-tokens.md).

- Web app with its own backend: **server-side session** in an `HttpOnly;
  Secure; SameSite=Lax` cookie is the simple, revocable default.
- JWT access tokens: short-lived (5-15 min), validated for signature,
  algorithm allow-list, `iss`, `aud`, `exp`; never in `localStorage` for
  apps exposed to XSS.
- **Refresh tokens rotate on every use**, are stored hashed, and reuse of an
  old one revokes the whole token family.
- CSRF: cookie-authenticated state-changing requests need `SameSite` plus a
  CSRF token or an Origin check. Bearer-token APIs are not CSRF-prone.
- Rate-limit login, signup, reset and MFA endpoints per IP and per account,
  with backoff or lockout; MFA (TOTP or passkeys) for admins at minimum.
- Reset and magic-link tokens: random, single-use, short TTL, stored hashed.

## 4. Authorization: server side, every route

- Every handler, server action, RPC and API route checks **who** the caller
  is and **whether they may touch this specific resource** (no IDOR: never
  trust an id from the client without checking ownership or tenant).
- Deny by default; centralize checks (middleware, policy functions) and test
  them, including the "other tenant" case.
- Hiding a button is not authorization.
- **Supabase:** RLS enabled on every table in an exposed schema, with
  policies per operation (`select`, `insert` with `with check`, `update`,
  `delete`) using `(select auth.uid())`. Test policies as `anon` and as a
  second user. The **service-role key bypasses RLS**: it lives only on the
  server (edge functions, backend jobs), never in client code, never in a
  `NEXT_PUBLIC_*` / `VITE_*` / `EXPO_PUBLIC_*` variable, never in a mobile
  bundle. Schema changes to policies go through `db-migration`.

## 5. Secrets

- Secrets come from env or a secret store (cloud secret manager, CI
  secrets); `.env` is gitignored and only `.env.example` with placeholders
  like `DATABASE_URL=${DATABASE_URL}` is committed.
- Run commands that need secrets with `tq dotenv run -- <cmd>`, which loads
  `.env` into that one command without printing it; tq's guard keeps agents
  from reading `.env` files directly.
- Never print, echo, log or paste a secret value, and do not rotate secrets
  from an agent session; if one leaked, stop and tell the user.

## 6. Review

Run `tq decide triage --staged` (auth and secrets changes come back `high`),
then go through [references/asvs-checklist.md](references/asvs-checklist.md)
and report each failed item with severity (critical / high / medium / low),
file and line, and the fix.

Audit mode for an existing app: grep for weak hashes (`md5`, `sha1`,
`createHash('sha256')` near `password`), plaintext password columns,
secrets in client bundles (`NEXT_PUBLIC_.*(SECRET|SERVICE_ROLE|KEY)`), tables
without RLS, routes without an auth check, and tokens in `localStorage`.
