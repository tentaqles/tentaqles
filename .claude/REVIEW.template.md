# Reviewing {project}

<!-- Copy to REVIEW.md at the repo root and add `@REVIEW.md` to CLAUDE.md. Fill every {placeholder}, delete what doesn't apply, and keep it under 6 KB, since it loads in every session. The section names are used by the skills: keep them. Delete "Using the app" if the project has no UI. -->

How to judge a change to this project.

## Priorities

Read `CLAUDE.md`, {section with the project's priorities}. If they disagree with this file, they win.

## What counts as a real failure

Something a user or an attacker can trigger, with a path you can show.

- **{The worst thing for this product, e.g. Security and privacy}.** {An endpoint that doesn't check the session and ownership itself. Personal data ({fields}) in what the server sends, not just what renders. Login responses that reveal whether an account exists. Secrets in client bundles or logs.}
- **Data integrity.** {A duplicate of something that must be unique. A race ending in a 500 or a duplicate row. A limit enforced only in app code. Several writes with no transaction. A side effect that blocks or rolls back its write.}
- **Availability.** {A core flow failing when a third party is down or slow. An outbound call with no timeout. A busy database giving a 500 instead of "try again" with the input kept.}
- **Speed.** Budgets: {LCP < 2.0 s at p75, API p95 < 300 ms}. Measured on a production build. {An unbounded query on a hot path.}
- **User experience.** {A form that clears input on error. "Something went wrong" or a raw error. A dead end. A list with no empty state. A button with no pending state, so a double click double submits.}
- **Accessibility.** {Horizontal scroll at 360 px. A control unreachable by keyboard. An input with no label. Motion that ignores reduced-motion.}
- **Spec.** Behaviour or copy that differs from the ticket's `rd.md`. Anything crossing a non-goal.
- **{Project rule}.** {All DB access goes through lib/db.ts.}

## Severity

Set by the impact if it happens, not by how likely you think it is. Unsure between two: pick the higher and say why in one line.

| Level | Meaning | Land? | Examples |
|---|---|---|---|
| **P0** | {A customer loses money or data, gets locked out or sees someone else's data} | No | {Another user's data visible; 500 on a race} |
| **P1** | {A core flow fails for some users} | No, unless the owner accepts it | {Input lost on error; a keyboard trap; a failing test} |
| **P2** | A real but contained defect with a workaround | Yes, with a follow-up | {A missing hover state} |
| **P3** | No user impact | Yes | {A clearer name} |

## What to ignore

Don't report these.

- Known limits an `rd.md` lists. Report only if the change makes one worse.
- Planned work that isn't built, and open product questions. Flag it only if the code decides one.
- Style that matches the surrounding code. Anything the linter accepts.
- Dev-server-only behaviour, and speed numbers from a dev server or a single sample.
- Generated files: `.job/`, `.claude/worktrees/`, {build output, lock files}.
- {Deliberate decisions that look like bugs, e.g. "light theme only".}
- Hypotheticals.

## Using the app

Review on a production build, on throwaway data: `app.testData.setup` first and `app.testData.restore` after, always.

```bash
agent-browser --session user open http://localhost:<port>/{page}
agent-browser --session user snapshot -i     # refs @e1, @e2; take a new one after every change
agent-browser --session user click @e7
agent-browser --session user fill @e3 'text'
agent-browser --session user console         # then: errors
agent-browser --session user set viewport 360 800
agent-browser --session user screenshot .job/review/name.png
```

- One named session per actor (`user`, `user-b`, `anon`). A race needs two separate sessions, not two clicks in one tab.
- Find things by role and visible text. A field you can't find by its label is a finding.
- A console error or server error during a flow is a finding.
- Test emails use `example.com`. Queries are read only.

| Feature | Walk | Pass when |
|---|---|---|
| {feature} | {`anon`: open ..., click "...", fill ...} | {exact copy, where you land, what's stored} |

For every changed screen: keyboard through it with visible focus; 360 px in each state; reduced motion; hover, focus and pending; back and refresh after a change; logged out.

## How to report

Reviewer agents reply in their own JSON with these same fields. Anywhere else, lead with the verdict:

```markdown
## Review: <branch or PR> - <date>
**Verdict:** Land | Land after P1 fixes | Do not land (N blockers)
**Gates:** {each gate}: pass or fail, or not run

#### P0-1 <what breaks, for whom>
- **Class:** kebab-case kind of mistake, like `missing-ownership-check`
- **Where:** `file:line`, and every other place it shows up
- **Rule:** the line of this file, `CLAUDE.md` or `rd.md` it breaks
- **Repro:** steps someone with no context can follow
- **Expected / Actual:** what should happen, and what you saw, quoted exactly
- **Fix direction:** one or two lines

### Checked and fine
What you exercised that passed.

### Not checked
Anything you couldn't run, and why. Never leave this out.

### Questions
Things that need a product call. Not findings.
```

- **No rule, no finding.** Name the rule it breaks, or move it to Questions or drop it.
- **One finding per root cause.** Five sites from one helper is one finding listing five places.
- **Every finding has a class**, so the fix covers the whole class.

## Verification

- {Build, lint and test commands.} All pass, none skipped.
- New behaviour needs a new test, and a changed flow needs its test updated in the same diff.
- {Kinds of test this project uses and doesn't use.}
- {What a change on a high-risk path needs.}
- Evidence is the command, exit code and output, the copy you saw, the screenshot path.

## Rules learned over time

Format: `- **YYYY-MM-DD, area**: the rule. *Why:* what happened.` Added with the owner's yes, newest last.
