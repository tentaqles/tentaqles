# {feature} eval: {Feature name}

| | |
|---|---|
| **RD** | [rd.md](rd.md) |
| **Risk** | Medium: {the worst break, from rate-risk} |
| **Mockup** | [mockup.html](mockup.html) <!-- or: None: no UI --> |
| **Eval status** | Draft |
| **Approved** | — |
| **New tests** | {test name}, {test name} |

<!--
Eval status moves Draft → In review → Approved → Passing (or Parked). The eval gate hook
(.claude/hooks/eval-gate.mjs) only lets it become Approved right after the user says yes, keeps an
approved eval's checks and risk fixed, and only lets it become Passing (and the RD Shipped) when
`node .claude/hooks/eval-gate.mjs check {feature} --run` passes.
-->

## How to verify

1. `node .claude/hooks/eval-gate.mjs check {feature} --run` runs the gate commands (lint, build, the full test suite). Every one must pass, including the new tests listed above. This is the regression suite.
2. Work through every check below against a production build of the app (see `REVIEW.md` §2 for setup, accounts and test data).
3. Record each result as you go: `- **Result**: PASS` or `FAIL: {what happened}`, and `- **Evidence**:` with the test name, a command and its output, or a screenshot saved under `{evidenceDir}/{feature}/` (backtick the path; the gate checks it exists).
4. `node .claude/hooks/eval-gate.mjs check {feature} --run` exits 0. Only then set **Eval status** to Passing and the RD to Shipped.

Setup and test data: {which seeded accounts and records each check uses, any extra rows to create, clock or time zone assumptions}.

## Checks

Categories: **U** ways to use it · **F** ways it can fail · **R** what must not break · **Q** speed, accessibility, quality · **S** security and privacy · **D** design matches the mockup.
Method is one of `test <name>` (an automated test), `browser` (by hand in a real browser against the production build), `command` (a shell command and its expected output), `inspect` (read code or the database).

### U-01 {User does the main thing}
- **Covers**: REQ-1, REQ-2
- **Method**: test {name}
- **Steps**:
  1. As {who}, on {route}, {action with the exact label, e.g. click "Save changes"}.
  2. …
- **Expect**: {Observable outcome, with user-facing copy quoted verbatim from the RD, and the database / email side effects.}
- **Result**: PENDING
- **Evidence**: —

### F-01 {Required field left empty}
- **Covers**: REQ-5
- **Method**: test {name}
- **Steps**:
  1. …
- **Expect**: Inline error "{exact RD copy}" next to the field; every other input keeps its value; nothing written to the database.
- **Result**: PENDING
- **Evidence**: —

### R-01 {Neighbouring feature still works}
- **Covers**: REQ-7
- **Method**: test {existing test name}
- **Steps**:
  1. …
- **Expect**: …
- **Result**: PENDING
- **Evidence**: —

### Q-01 {Stays within the speed budget}
- **Covers**: NFR performance
- **Method**: command
- **Steps**:
  1. {How speed is measured in this project, e.g. a Lighthouse run, a timing script, the test suite's perf output.}
- **Expect**: {The budget from REVIEW.md / CLAUDE.md, or no worse than the baseline taken before building.}
- **Result**: PENDING
- **Evidence**: —

### S-01 {Another user can't act on it}
- **Covers**: REQ-9
- **Method**: browser
- **Steps**:
  1. As user B, send user A's request with A's id (forged form data or a second session).
- **Expect**: Refused with {the not-found / forbidden behaviour the RD names}; database unchanged.
- **Result**: PENDING
- **Evidence**: —

### D-01 Matches the mockup at 1280 px and 360 px
- **Covers**: mockup.html
- **Method**: browser
- **Steps**:
  1. Open `mockup.html` and the built screen side by side at 1280 px and at 360 px.
  2. Screenshot both to `{evidenceDir}/{feature}/`.
- **Expect**: Same layout, hierarchy, copy, spacing rhythm and states; no horizontal scroll at 360 px. Differences are listed here and approved, or fixed.
- **Result**: PENDING
- **Evidence**: —

## Coverage

| RD requirement | Checks |
|---|---|
| REQ-1 | U-01 |

## Not covered

{Anything deliberately not verified and why, e.g. real email delivery (console fallback in tests). Empty is fine.}
