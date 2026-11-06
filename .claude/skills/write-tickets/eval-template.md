# {NNN}: {Title}: checks

Requirements: [rd.md](rd.md)

## Setup

{Exact steps to reach a state where the checks can run: install, seed data, start command. The checks may run in a fresh copy of the project, with other copies running at the same time. So create everything from scratch, and write `<port>` for the port, never a fixed one. Write "None" if the checks need nothing.}

## Not covered

{Anything deliberately not checked, and why. Write "Nothing" if so.}

## Checks

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy, **D** design matches the mockup.

New checks go at the end of this file. Nothing else goes after the checks.

### U-01 {The user does the main thing}
- **Covers**: REQ-1
- **Method**: test {TestName}
- **Steps**:
  1. {Exact step, with the exact command, route or label.}
- **Expect**: {An observable result, quoted exactly, on this one line.}

### F-01 {One way it can fail}
- **Covers**: REQ-2
- **Method**: test {TestName}
- **Steps**:
  1. {Exact step that causes the failure.}
- **Expect**: {The exact error or refusal, and what stays unchanged.}

### Q-01 {One quality bar}
- **Covers**: {REQ-n}
- **Method**: command
- **Steps**:
  1. {The exact command.}
- **Expect**: {The exact output or limit.}

### R-01 {One existing behaviour that must not break}
- **Covers**: {REQ-n}
- **Method**: test {ExistingTestName}
- **Steps**:
  1. {Exact step.}
- **Expect**: {The exact result.}

### S-01 {One attack that must fail}
- **Covers**: {REQ-n}
- **Method**: command
- **Steps**:
  1. {The exact attack: forged id, bad input, wrong user.}
- **Expect**: {The exact refusal, and what stays unchanged.}

### S-02 {High risk only, delete this check on Medium: a second security check}
- **Covers**: {REQ-n}
- **Method**: {test TestName | command | inspect}
- **Steps**:
  1. {Exact step.}
- **Expect**: {The exact result.}

### S-03 {High risk only, delete this check on Medium: a third security check}
- **Covers**: {REQ-n}
- **Method**: {test TestName | command | inspect}
- **Steps**:
  1. {Exact step.}
- **Expect**: {The exact result.}

### D-01 {Mockup only, delete this check otherwise: matches the mockup at phone and desktop width}
- **Covers**: mockup.html
- **Method**: {browser | command | human}
- **Steps**:
  1. {Open mockup.html and the built screen at 360 px and 1280 px. Compare each state.}
- **Expect**: {Same layout, copy and states, no horizontal scroll at 360 px.}
