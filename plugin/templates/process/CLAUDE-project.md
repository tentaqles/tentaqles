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
The definition of done (`/tentaqles:dod`) and the ship skill read this section.

- Checks: test `<the command that runs the whole suite, e.g. python -m pytest -q>`; lint `<optional, e.g. ruff check .>`; typecheck `<optional, e.g. mypy src>`
- Architecture paths (a change there needs an ADR under `docs/adr/`): `src/core/**`, `db/schema/**`, `infra/**`
- Changelog: `CHANGELOG.md` (the file that must change on every feature or fix; "none" when this project keeps none)
- Domain and roles: <who uses this and as what>
- Acting as a role in tests: <a seeded test user and a factory; never real credentials>
- URLs: local <http://127.0.0.1:PORT>, staging <...>

## Architecture rules
<layering, dependency direction, where things live>

## Shared helpers
<the helpers everyone should reuse instead of rewriting>
