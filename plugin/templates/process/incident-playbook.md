# Incident playbook (personal-data or security incident)

Owner: <contact from the manifest>    Authorities: <authorities for data.regions; when both apply, the shorter clock governs the first notice>

Start the clock the moment anyone suspects personal data was exposed, lost,
altered or made unavailable. A bug that showed one user another user's data is
an incident. Write times in UTC.

## 1. Detect (T+0)
- Record: who noticed, when, how, what was seen. Save evidence (logs, screenshots) in a private location; redact before sharing (`tq redact`).
- Open `docs/incidents/<YYYY-MM-DD>-<slug>.md` from this file's sections.

## 2. Contain (first hour)
- Stop the bleeding without destroying evidence: disable the endpoint, feature flag off, revoke sessions, block the IP.
- Credentials: an exposed credential is handled by the owner through the provider's console; this process **never rotates** secrets itself and never prints them (tq rule).

## 3. Assess (first day)
- Data classes and subjects affected (count or estimate), regions, whether data was encrypted, whether it left the organisation.
- Root cause (one paragraph) and whether the PRD, threat model or privacy record had a gap.
- Risk to subjects: likely harm, reversibility.

## 4. Notify
- Authority: **ANPD within 3 business days** of awareness (Res. CD/ANPD 15/2024, Art. 6) for Brazilian subjects; **GDPR within 72 hours** (Art. 33) for EU subjects; both apply when both regions do, so the shorter clock governs the first notice. Low-risk incidents may not require authority notice; record the reasoning.
- Subjects: when risk is relevant (LGPD Art. 48) or high (GDPR Art. 34), in plain language: what happened, what data, what we did, what they can do.
- Internal: the client contact in the manifest, the project owner.

## 5. Record (within a week, keep five years)
- Timeline, decisions, notices sent, evidence location.
- Fix: the `/tentaqles:fix` slug and the locked regression test.
- Lessons: `docs/LEARNINGS.md` lines; changes to `docs/threat-model.md` and `docs/privacy.md` in the same PR as the fix.
