# Privacy record (record of processing)

Status: draft | current    Updated: <YYYY-MM-DD>    Owner (DPO or contact): <name, address from the manifest data.incident.contact>
Regions of data subjects: <from manifest data.regions>    Authorities: <authorities for data.regions; when both apply, the shorter clock governs the first notice>
Cites: PRD §5, manifest `data:` block, docs/threat-model.md

This is the controller's record (LGPD Art. 37; GDPR Art. 30). Keep it in step
with the manifest `data:` block; the definition of done requires this file to
change whenever a classified path changes.

## Data dictionary
| Data | Class | Source | Store (path or table) | Retention | Deletion job |
|---|---|---|---|---|---|

## Processing activities
One block per purpose.

### <Activity name>
- Data classes and subjects:
- Purpose (one sentence; necessary and specific):
- Legal basis: LGPD Art. 7 (<item>) or LGPD Art. 11 (<item>, sensitive data) / GDPR Art. 6 (<letter>) or GDPR Art. 9 (<letter>, special categories)
- Common bases in plain words: consent, contract, legal obligation, legitimate interest; for sensitive data: explicit consent or the listed exceptions. If unsure, add a `NEEDS-DECISION` line to `## Open decisions` instead of guessing.
- Minimisation note (what we deliberately do not collect):
- Retention and the deletion job that enforces it (name, schedule, test):
- Access controls (roles, RLS policies, service accounts):
- Encryption at rest and in transit:
- Processors and transfers (who, where, safeguard):
- Logging: fields that may contain personal data and how they are masked (`tq redact` classes):

## Data subject rights (DSAR path)
How a subject gets confirmation, access, correction, deletion, portability and
objection; where the request lands; who answers; the deadline: LGPD 15 days
(Art. 19), GDPR one month (Art. 12(3)).
- Entry point:
- Identity check:
- Fulfilment steps and the script or job that performs deletion:

## Impact assessment trigger
A DPIA (GDPR Art. 35) or RIPD (LGPD Art. 38) is required before processing that
is large scale, uses sensitive data or children's data, profiles or scores
people, or combines datasets. Current status: not required because <reason> |
done on <date>, link.

## Incident clock
Personal-data breach: follow `docs/incident-playbook.md`. Notify the authority
within **3 business days** (ANPD, Res. CD/ANPD 15/2024) or **72 hours** (GDPR
Art. 33); when both apply, the shorter clock wins. Keep incident records for
**five years**.

## Open decisions
One `NEEDS-DECISION` per line, for the DPO or client to answer. Remove a line
when it is decided and record the answer in the activity above.
- NEEDS-DECISION: <question> (owner, date)

## Change log
| Date | Change | Why | PR |
|---|---|---|---|
