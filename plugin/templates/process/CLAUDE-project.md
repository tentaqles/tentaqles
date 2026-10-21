# <Project name>

## Reading path
To work on this project read `docs/prd.md` and `docs/adr/` first, then the
spec slice you were given (`docs/specs/<slug>.md`). `docs/LEARNINGS.md` lists
the rules this repo learned the hard way.

## Must not trade off
One line per rule; each names the test that enforces it. The critic treats a
change that weakens one as a BLOCKER.
- <rule> · enforced by `<test file>::<test name>`

## Project config
Humans read the prose; tools read the fenced block. Keep both in step.

- Domain and roles: <who uses this and as what>
- Acting as a role in tests: <a seeded test user and a factory; never real credentials>
- URLs: local <http://127.0.0.1:PORT>, staging <...>

```json tq-project
{
  "commands": {
    "test": "<the command that runs the whole suite, e.g. python -m pytest -q>",
    "lint": "<optional, e.g. ruff check .>",
    "typecheck": "<optional, e.g. mypy src>",
    "build": "<optional>",
    "run": "<optional: starts the app on 127.0.0.1 for the breaker>"
  },
  "architecture_paths": ["src/core/**", "db/schema/**", "infra/**"],
  "classified_paths": [],
  "changelog": "CHANGELOG.md"
}
```

`architecture_paths` are globs whose changes require an ADR in the definition
of done. `classified_paths` mirrors the manifest `data:` block paths so the gate
can require `docs/privacy.md` to change with them. `changelog` is the file that
must change on every feature or fix (empty string: this project keeps none).

## Architecture rules
<layering, dependency direction, where things live>

## Shared helpers
<the helpers everyone should reuse instead of rewriting>
