# Privacy review checklist (answer-key rows)

Judge each row from the diff and the files it touches. Cite file:line.

| # | Check | Source |
|---|---|---|
| P1 | Every personal-data field touched is in `docs/privacy.md` with purpose and legal basis (LGPD Art. 7 or 11; GDPR Art. 6 or 9) | LGPD Art. 6 I, 7, 11; GDPR Art. 5(1)(b), 6, 9 |
| P2 | Minimisation: the change collects or exposes only fields the purpose needs; no "while we're here" columns | LGPD Art. 6 III; GDPR Art. 5(1)(c), Art. 25 |
| P3 | Retention: a stated period and a deletion job (or TTL) that enforces it, with a test | LGPD Art. 15, 16; GDPR Art. 5(1)(e) |
| P4 | Access: the data is behind authorization (RLS, roles); service accounts are least privilege | LGPD Art. 46; GDPR Art. 32 |
| P5 | Logging and events: no new log line, metric label, analytics event or error message carries personal data unmasked (`tq redact` classes) | LGPD Art. 46; GDPR Art. 32 |
| P6 | Exports and fixtures: no production rows into tests, seeds or fixtures; synthetic data only | LGPD Art. 46; project rule tq/pii-fixture-dump |
| P7 | Subject rights: the DSAR path still works (access, correction, deletion, portability reach the new store or field) | LGPD Art. 18, 19; GDPR Art. 15 to 20 |
| P8 | Transfers: a new processor or region is recorded with its safeguard | LGPD Art. 33; GDPR Art. 44 to 46 |
| P9 | Incident readiness: a breach of this data would be detectable (audit log) and the playbook clock applies (ANPD 3 business days, GDPR 72 h) | LGPD Art. 48; GDPR Art. 33, 34 |
| P10 | Record keeping: `docs/privacy.md` and the manifest `data:` block changed in this diff when a class, path, table or retention changed | LGPD Art. 37; GDPR Art. 30 |
| P11 | Children or sensitive data: DPIA or RIPD recorded or explicitly not required | LGPD Art. 14, 38; GDPR Art. 8, 35 |
