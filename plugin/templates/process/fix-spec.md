# fix: <slug>

Status: reproducing | reproduced | fixed
Severity: critical | high | medium | low
Hotfix: no | yes (reason: <production impact now, fix path obvious>)
data_classes_touched: [] # personal data exposed? then this bug is also an incident: start the clock (docs/incident-playbook.md)

## Report (summary, personal data replaced)
Summarise the report, stack trace or failing command, keeping its meaning,
with personal data replaced by placeholders (`<customer>`, `<email>`,
`<row id>`); never paste it verbatim. This file is committed. Leave the
original where it came from (the tracker or a local file) and link to it.

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
