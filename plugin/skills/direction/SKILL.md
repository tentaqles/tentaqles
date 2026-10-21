---
name: direction
description: Read-only drift audit of a repository against its PRD. Grades every functional requirement Aligned, Partial, Missing or Contradicted by reading the code and tests (never by trusting a summary), classifies work the PRD never asked for, and returns ON COURSE, DRIFTING or OFF COURSE with recommendations limited to Fix code, Update PRD or Decide. Use when the user asks "are we on track", "did we drift", "does the code match the PRD", at the end of /tentaqles:kickoff, before a release, or "/tentaqles:direction".
argument-hint: "[FR-nn ...] [--headless]"
---

# Direction

The PRD says what should exist. This audit says what does. It reads; it never
modifies files, commits or runs the app. Allowed tools: Read, Grep, Glob and
read-only git commands (`git log`, `git diff --stat`, `git ls-files`). If you
did not read the code that implements an FR, you do not grade it: it goes under
"Not verified".

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. At a human stop, write the
question with `build-gate ask <slug> --kind question --text "<the question>" --options <a>,<b>`
(the `build-gate` prefix is in `skills/build/SKILL.md`) and end your turn.

## 1. Inputs

```bash
_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/plugin.json" ] && _tqe="${_d%/}" && break; done; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
"$TENTAQLES_PY" "$_tqe/scripts/prd-status.py" --cwd "$root"; echo "prd_exit=$?"
git -C "$root" log --oneline -30
```

`prd_exit` is 0 when the PRD is planning-ready, 3 when it is not ready (audit
anyway and say so in the verdict), 4 when there is no PRD. For 4, stop and
point the user to `/tentaqles:prd` ("from existing code" variant); headless,
write the question instead with `build-gate ask` and end the turn. Otherwise
read `docs/prd.md` (section 3 goals, 3.2 non-goals, section 4 FRs with their
acceptance checks, section 11 releases), `docs/adr/*.md` (accepted decisions)
and `docs/specs/*.md`.

## 2. Grade each FR (or the FRs named in `$ARGUMENTS`)

For every acceptance check of the FR, find the code and the test that prove it
(Grep for the nouns in the check; read the test; confirm it would fail if the
behaviour were removed). Then grade:

| Grade | Meaning |
|---|---|
| Aligned | every acceptance check has code and a test you read |
| Partial | some checks proven, some not; name the missing ones |
| Missing | no code for it |
| Contradicted | code does the opposite, or violates a 3.2 non-goal or an ADR |

An FR you could not trace within a reasonable search (say, ten Grep calls) is
"Not verified", not "Missing".

## 3. Unplanned work

List what the code does that no FR asked for: endpoints, tables, jobs, flags,
dependencies. Classify each: `harmless` (dev tooling), `should be an FR`
(users will rely on it), `violates a non-goal`, or `risk` (touches auth, money,
PII, or a trust boundary with no threat-model row).

## 4. Verdict

- **ON COURSE**: every FR of the current release (section 11) Aligned or
  Partial with a plan, nothing Contradicted, no unplanned `risk`.
- **DRIFTING**: Missing FRs in the current release, or unplanned `should be an
  FR` work, or an unresolved `NEEDS-DECISION` that code already answered.
- **OFF COURSE**: any Contradicted FR, any non-goal violated, or unplanned
  `risk` work.

## 5. Output

```
DIRECTION: ON COURSE | DRIFTING | OFF COURSE

| FR | Grade | Evidence (file:line, test) | Gap |
|---|---|---|---|

Unplanned work:
- <item> · <class> · <file>

Not verified:
- <FR> · <why>

Recommendations (each one of exactly three kinds):
- Fix code: <what, to satisfy which check>
- Update PRD: <what the PRD must say to match a deliberate change; cite the ADR or decision>
- Decide: <the question only the human can answer, as a NEEDS-DECISION line for section 10>
```

No other recommendation kinds. No scores. Do not propose changes to files
yourself: hand the list to the user; `/tentaqles:feature` picks up "Fix code"
items, `/tentaqles:prd` the "Update PRD" ones.
