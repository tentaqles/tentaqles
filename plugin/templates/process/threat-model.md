# Threat model

Status: draft | accepted | superseded    Updated: <YYYY-MM-DD>    Cites: PRD §6, §2
Reviewed by: <name>    Next review: <date or "on every new trust boundary">

Four questions, answered per trust boundary (Shostack). Keep it short enough to
read before every feature.

## 1. What are we building
One paragraph and a boundary list. A trust boundary is anywhere data crosses
between parties that trust each other differently.

| Boundary | From | To | Data crossing (classes) | Authentication |
|---|---|---|---|---|
| B1 | <from> | <to> | <classes> | <auth> |

Replace the placeholder row, and add one row per boundary. An example in prose
(e.g.): B1 is a browser calling the api with personal data over a session
cookie; B2 is the api calling the database with a service credential.

Assets worth protecting (what an attacker wants): <accounts, money, personal
data, availability, reputation, the signing key>.

## 2. What can go wrong
One STRIDE table per boundary. Spoofing, Tampering, Repudiation, Information
disclosure, Denial of service, Elevation of privilege. Write threats as
sentences a tester can try ("a user replays another user's request id").
Status is one of: planned, done, accepted, superseded.

### B1 <from> -> <to>
| Category | Threat | Mitigation | Status | Owner |
|---|---|---|---|---|
| Spoofing | | | | |
| Tampering | | | | |
| Repudiation | | | | |
| Information disclosure | | | | |
| Denial of service | | | | |
| Elevation of privilege | | | | |

## 3. What will we do about it
Every mitigation above maps to one of: a requirement (`PRD §4 FR-nn`), a rule
in `CLAUDE.md` "Must not trade off" with its test, a guard rule, or an accepted
risk below. A mitigation with none of these is not a mitigation.

## 4. Did we do a good job
- Every boundary in §1 has a table in §2: yes / no
- Every high-risk row is done, planned as an FR, or accepted below: yes / no
- The `security-reviewer` checklist references this file: yes / no
- Last breaker or pentest run: <date, findings count>

## Accepted risks
Each accepted risk expires; an expired risk is a `NEEDS-DECISION` for the PRD.

| Risk | Why accepted | Accepted by | Date | Expires |
|---|---|---|---|---|
