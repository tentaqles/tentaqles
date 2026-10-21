# <Product or project name>: product requirements

Status: draft | planning-ready | superseded by <link>
Owner: <name>    Updated: <YYYY-MM-DD>

A PRD says what and why. How is decided in ADRs and spec slices. Every later
document cites a section number from here (`PRD §4 FR-03`).

Planning-ready means: section 10 is empty and every FR has at least one
acceptance check. `/tentaqles:kickoff` and `/tentaqles:feature` check this
mechanically (`prd-status.py`); a PRD that is not planning-ready blocks planning.

## 1. Problem and evidence
What hurts today, for whom, and the evidence (numbers, quotes, tickets). One
paragraph, then bullets.

## 2. Users and roles
| Role | Who they are | What they need from this | Can | Cannot |
|---|---|---|---|---|

## 3. Goals
Three to five outcomes, each measurable.
- G1:
- G2:

### 3.2 Non-goals
What this will not do, so nobody plans it by accident.
-

## 4. Functional requirements
One `### FR-nn` per requirement. Acceptance checks are binary, observable
statements someone can run or decide blind ("the second submit returns 409",
"a tester picks the new copy in a blind A/B"), never scores or adjectives.

### FR-01 <short imperative title>
Why: <one sentence tying it to a goal>
**Acceptance checks**
- <binary, observable statement>
- <binary, observable statement>

### FR-02 <title>
Why:
**Acceptance checks**
-

## 5. Data
Data classes handled (public, internal, pii, sensitive, financial, children,
secrets), where each comes from, where it lives, how long it is kept.
| Data | Class | Source | Store | Retention |
|---|---|---|---|---|

## 6. Security
Trust boundaries (who talks to what), the authentication model, roles and
permissions, and the abuse cases you expect (what a malicious user would try).
- Boundaries:
- Authentication:
- Abuse cases:

## 7. Non-functional requirements
Numbers only: load, latency, availability, data volume, cost ceiling, supported
browsers or platforms, accessibility level.
-

## 8. Data model sketch
Entities and their relations in prose or a small diagram. Not the schema.

## 9. Stack and constraints
Language, framework, database, hosting, must-use libraries, must-not-use
libraries, and why each constraint exists.
-

## 10. Open questions
Each item starts with `NEEDS-DECISION` and names how it could be settled (ask,
research, prototype) and who decides. Remove the item when decided and move the
answer into the section it belongs to. Empty section = ready to plan.
- NEEDS-DECISION: <question> (settle by: ask <who> | research | prototype)

## 11. Releases
Which FRs ship in release 1, 2, 3, and what "done" means for each release.
| Release | FRs | Done when |
|---|---|---|
