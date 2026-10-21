# Review contract (answer key)

Every reviewer returns this and nothing else. Verdicts are pre-registered and
binary; there are no scores.

```
RESULT: PASS | FAIL | BLOCKED
BUILT-BY-ME: no | yes — <how you were involved>
F1 BLOCKER <file:line> — <problem> → <concrete fix>
F2 MAJOR   <file:line> — <problem> → <concrete fix>
F3 MINOR   <file:line> — <problem> → <concrete fix>
U1 CANNOT JUDGE — <check> — <what a human must look at, and where>
```

- The first line is exactly one of `RESULT: PASS`, `RESULT: FAIL` or `RESULT: BLOCKED`.
- `FAIL` when any BLOCKER or MAJOR exists. MINOR alone is `PASS` (listed so it
  is logged).
- `BLOCKED` when a check you were asked for cannot be judged from the files
  (missing environment, a decision only a human can make). Every `U` row names
  the check and what the human must look at. A BLOCKED result goes to the human
  with the `U` rows; the human settles it in their own terminal with
  `build-gate judge <slug> <id> --reviewer <you> --result PASS|FAIL`.
- Every `F` row has a file and line and a concrete fix. No line, no finding.
- `BUILT-BY-ME: yes` when you had any part in producing the change (you wrote
  code, answered the planner, fixed a round). The orchestrator then discards the
  verdict and runs a fresh reviewer.
- In later rounds, rule on each earlier finding first:
  `F<n> resolved | upheld | withdrawn — <why>`, verifying a "fixed" claim by
  reading the cited lines. Then list new findings, numbered on.
