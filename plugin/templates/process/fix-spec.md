# fix: <slug>

Status: reproducing | reproduced | fixed
Severity: critical | high | medium | low
Hotfix: no | yes (reason: <production impact now, fix path obvious>)
data_classes_touched: [] # personal data exposed? then this bug is also an incident: start the clock (docs/incident-playbook.md)

## Report (verbatim)
Paste the report, stack trace or failing command exactly as received.

## Observed
What happens.

## Expected
What should happen, citing the PRD FR or rule when one exists.

## Environment
Version or commit, OS, browser or client, data set, flags.

## Reproduction
The locked regression test: `<path>::<test name>` and the command that runs it.
Written by the tester before any source edit and locked by `build-gate lock-tests`.

## Root cause
One paragraph after the fix: the actual cause, not the symptom.

## Lesson
The one sentence that would have prevented it (also appended to docs/LEARNINGS.md).

## Migration plan
Only when schema files change. Else "none".
