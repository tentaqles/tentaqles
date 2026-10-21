# <Feature name>

Status: draft | approved | implemented
derived_from: PRD §4 FR-nn (or "no PRD: legacy repository, see kickoff later")
data_classes_touched: [] # from the manifest data: block, e.g. [pii]
trust_boundaries_crossed: [] # from docs/threat-model.md, e.g. [browser->api]

## Problem
What is wrong or missing for whom, in two sentences.

## Behaviour
What the system does when this is done. Inputs, outputs, rules the code will
branch on (the grill-me interview answers go here as a table).

| ID | Rule | Example | Decided by |
|---|---|---|---|

## Acceptance checks
Binary, observable, each traceable to an FR acceptance check or a rule above.
- [ ]

## Out of scope
-

## Open questions
Each tagged NEEDS-DECISION. Planning is blocked while any remain.
-

## Migration plan
Only when schema files change: expand/contract steps, backfill, rollback. Else
write "none: no schema change".

## Assumptions
Decisions taken where the PRD and the user were silent, with the source.
-
