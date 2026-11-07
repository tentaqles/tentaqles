---
name: write-evals
description: Write the eval for a feature before it is built (<featuresDir>/<feature>/eval.md, plus mockup.html when it has UI), then get the user's approval. Use whenever the user brings a new feature or a change in behaviour to build, before writing any code, even if they don't mention evals. This skill only writes evals and gets them approved; it never builds the feature.
---

# write-evals

Turn a feature into an eval an agent can verify against, mechanically: every way to use it, every way it can fail, and everything it must not break, each with exact steps and an exact expected result. If the feature has UI, also draw an HTML mockup that sets the design bar. Then get the user's yes.

An eval is a fixed test that tells you whether the work is done. It has to be strict enough that bad work fails it.

**This skill writes evals. It does not build.** No edits to app code while running it: the eval gate hook blocks edits to every path in `codePaths` while any eval is in `Draft` or `In review`. Building starts after approval, against the approved eval (the `review-loop` skill does it).

## 0. First run in a project

1. Read `.claude/evals.config.json`. If `"confirmed"` is `false`, fill it in for this project before anything else:
   - `featuresDir`: where feature folders live (default `specs/features`; use the project's existing specs folder if it has one).
   - `codePaths`: the folders and files that are app code (source, tests, scripts, the dependency manifest such as `package.json`, `pyproject.toml`, `Gemfile`, `go.mod`). Not specs, docs, `.claude/` or build output.
   - `gateCommands`: the commands that must exit 0 for any feature to be done, in order: typically lint, type check/build, and the test suite (e.g. `npm run lint`, `npm test`; `ruff check .`, `pytest`; `bundle exec rspec`; `go vet ./...`, `go test ./...`). Find them in the project's scripts, `Makefile`, task runner or CI config; don't guess. Leave out anything slow and flaky.
   - `app.build`, `app.start` (with `{port}`), `app.readyPath`: how to run a production-like build locally. Empty for projects with nothing to run in a browser (libraries, CLIs).
   - `app.testData.setup` / `restore`: how to give a test run its own throwaway data and put the developer's data back after. `{backup}` is a path under `evidenceDir`. For a file database: copy it to `{backup}`, reset and seed / move `{backup}` back. For a server database: point `app.env` at a separate test database and reset + seed it, with `restore` empty. Empty if the app keeps no data.
   - `mainBranch`.
   Detect each value from the project itself. Anything you can't find, ask the user rather than assume a stack.
   Show the user the filled-in config in one message and ask them to confirm or correct it. On a yes, set `"confirmed": true`.
2. Make sure `evidenceDir` (default `evidence/`) is in `.gitignore`: screenshots and run logs don't belong in git.
3. If there's no `specs/CLAUDE.md` (or the equivalent index in `featuresDir`'s parent), create it from [specs-index-template.md](specs-index-template.md): fill in the feature map (each existing feature → its main code) and the invariants from the code, and leave `{{…}}` only where you'd be guessing. Reviews use the feature map to find which feature a changed file belongs to.
4. If the project has no `REVIEW.md`, say so once: evals work without it, but `review-loop` and `fix-a-bug` lean on it. Offer to write it from the pack's template after this skill finishes.

## 1. Pin down the feature

1. Read `CLAUDE.md` (MUST NOT TRADE OFF), `specs/CLAUDE.md` (feature map, invariants), the product doc if there is one (note its non-goals and open questions) and the `rd.md` of every feature this one touches.
2. Name the feature: a short kebab-case folder name (`team-invites`, `export-invoices`). Use the existing folder if there is one.
3. **Risk.** Read the **Risk** row in `rd.md`. If there isn't one, run the `rate-risk` skill now and use its rating. The risk decides how strict this eval has to be (see "What each level changes downstream" in `rate-risk`).
4. If `<featuresDir>/<feature>/rd.md` doesn't exist, write it first from [rd-template.md](rd-template.md), **Status** `Planned`, with the **Risk** row. Number every requirement `REQ-1`, `REQ-2`…; the eval must cover each one. Quote user-facing copy exactly; the eval quotes the RD.
5. Ask the user only about genuine product decisions you can't settle from the specs or the code (anything listed as an open question counts). Batch them in one question. Don't decide them silently. If the feature crosses a non-goal, stop and tell the user the product doc has to change first.

## 2. Read the code it lands in

Read the code for every feature it touches, the data model (tables, constraints, migrations) involved, shared UI (components, design tokens, CSS) and the existing automated tests. Note the test runner, how tests are named and found, and how an end-to-end test drives the app, so new checks can point at real tests the builder will add.

## 3. Write `<featuresDir>/<feature>/eval.md`

Start from [template.md](template.md). Every check has a **Covers**, **Method**, **Steps**, **Expect**, **Result** (`PENDING`) and **Evidence** (`—`) line; the gate parses them.

Rules for a check an agent can actually verify:

- **Steps are concrete**: who is logged in (which seeded account, or a fresh one), which route or screen, which element by its accessible name, which input values.
- **Expect is observable**: exact copy from the RD in quotes, the URL after, what the database holds (row counts, status), what gets sent (email, webhook, log line). Never "works", "looks right" or "handles errors gracefully".
- **Method** is one of:
  - `test <name>`: an automated test, existing or new. Anything a test can drive goes here; name the new tests in the header's **New tests** row and the builder writes them.
  - `browser`: by hand in a real browser against a production build, for visual and feel checks.
  - `command`: a shell command and its expected output (build, lint, curl, a SQL query).
  - `inspect`: read code or the database for a property (e.g. "every new endpoint checks the session before reading the body").
- One behaviour per check. If a check needs "and" in its title, split it.
- No browser UI (an API, a CLI, a library)? Use `test` and `command` checks, read "screen" as "request" or "command", skip the mockup and D checks, and skip the browser-only items (360 px, focus, hover).

Which categories the eval needs depends on the risk (the gate enforces it):

| Risk | Required |
|---|---|
| Low | U, F, Q (+ D with a mockup) |
| Medium | U, F, R, Q, S (+ D) |
| High | U, F, R, Q, S (+ D), at least 3 S checks, at least one S check that attacks the running app (`test`, `browser` or `command`), and every item on the failure list below either checked or explained in **Not covered** |

**U: every way to use it.** Each user story and each role (logged out visitor, each kind of user, a user who doesn't own the thing). Each entry point (nav link, direct URL, link from an email, back button). First use and repeat use. Smallest and largest valid inputs. Mobile and desktop. Keyboard only.

**F: every way it can fail.** Walk this list and write a check for each item that applies; say in **Not covered** why the rest don't:

- Each validation rule: empty, too long (max + 1), wrong format, whitespace only, unicode/emoji, HTML/script in text fields. Error copy verbatim; the user's input is kept.
- Not logged in, session expired mid-flow → sent to login and returned to the same place.
- Wrong user: another user's id in a forged request or URL.
- Stale data: the record changed or was deleted between page load and submit; two tabs; double click on submit; back button after success.
- Races: two users at once on something unique → exactly one wins (enforced by the database, not app code), the other gets a friendly message, never a 500 or a duplicate row.
- Dependencies down: a third-party service (email, payments, storage, an API) failing or slow → the main action still succeeds or fails cleanly; every outbound call has a timeout.
- Time: boundaries (inclusive/exclusive), now vs a deadline, time zones, midnight and DST change days.
- Limits: empty lists (a designed empty state), one item, many items (no unbounded query; pagination or a cap).
- Removed or disabled things: deleted record, cancelled item, unknown id → a proper not-found page, not a crash.

**R: what must not break.** List every shipped feature that shares a route, component, table, endpoint or email with this one, and write a check for each specific thing at risk (e.g. "two people claiming the same username at once still produce one account"). Name the existing test that proves it where one exists. The full test suite passing (the gate commands) is required on top of these; don't list the whole suite one by one.

**Q: speed, accessibility, quality.** Speed against the budgets in `REVIEW.md` or `CLAUDE.md` if the project has them; no new dependency without a reason; every interactive element has hover, active, focus and disabled/pending states; labelled inputs, landmarks, visible focus, keyboard-operable menus and pickers, `prefers-reduced-motion` respected, no horizontal scroll at 360 px; the gate commands pass with zero warnings.

**S: security and privacy.** Every new endpoint or server action re-checks the session and ownership itself; personal data only reaches the people allowed to see it (check what the server sends, not just what renders); no new data collected beyond what the RD allows; responses don't reveal whether an account exists; secrets and tokens never reach the client or the logs.

**D: design.** One check per state in the mockup, comparing the built screen at 1280 px and 360 px against `mockup.html`, with screenshots saved under `<evidenceDir>/<feature>/`.

Finish with the **Coverage** table (every `REQ-n` → check ids) and **Not covered**.

## 4. Draw `<featuresDir>/<feature>/mockup.html` (features with UI)

Build the design as a simple web page first instead of building straight into the app. It's cheap to change, the user can see the feature before any tokens go into building it, and it becomes the design the built app is checked against. Make it the real thing, not a wireframe:

- One self-contained HTML file. A CDN build of the app's CSS framework is fine here (it never ships). Use the app's real fonts, colours and spacing.
- Reuse the app's exact class strings, design tokens and component styles (read its shared UI code), and match the existing chrome (nav, container width, footer), so it reads as part of the app.
- Copy is the RD's copy, verbatim. Data looks like the app's demo or seed data and is labelled as demo where it could be mistaken for real. No invented reviews, logos, metrics or urgency.
- Show every state on one page, each labelled: default, empty, loading, pending submit, each inline error, success, logged out, and the mobile layout in a 360 px wide frame. Add hover/focus notes where they aren't obvious.
- Check it renders: `node .claude/skills/write-evals/scripts/shoot.mjs <featuresDir>/<feature>/mockup.html <evidenceDir>/<feature>/mockup --widths 360,1280` screenshots it and prints `horizontalScroll` for each width (needs Playwright in the project; otherwise screenshot it with `agent-browser`). Look at the screenshots before showing the user; horizontal scroll at 360 px is a bug in the mockup too.

Set **Mockup** to `[mockup.html](mockup.html)`, or `None: no UI` for features without an interface (then quote any email or message text in the eval instead).

## 5. Lint, then ask for review

1. `node .claude/hooks/eval-gate.mjs lint <feature>` must exit 0 (well formed, every RD requirement covered, the categories its risk needs). Fix and rerun until it does.
2. Set **Eval status** to `In review`.
3. Open the files for the user (`open` on macOS, `xdg-open` on Linux): `mockup.html`, and `eval.md` and `rd.md` in their editor. Then ask them to review, giving:
   - the file paths;
   - the risk and why;
   - a count of checks per category and the new tests;
   - **every decision you made on your own** while writing the RD, eval and mockup (wording, limits, states, edge-case behaviour), one line each, so nothing gets approved by accident;
   - the open product questions, and anything in **Not covered**.
   Ask plainly: approve, or what to change.
4. If they ask for changes, make them, lint again and ask again. Stay in `In review`.

## 6. On a yes

Only right after the user's own yes (the hook refuses the change otherwise):

1. Set **Eval status** to `Approved` and **Approved** to `{user}, {YYYY-MM-DD}`.
2. If the eval has a speed check with a baseline, capture the baseline now, before any code changes, and save it under `<evidenceDir>/<feature>/`. If any gate command already fails on the untouched code, tell the user before building: the loop can't tell new breakage from old.
3. The skill ends here. Tell the user the eval is approved and that `/review-loop <feature>` builds it.

## Building against an approved eval (after this skill)

For whoever builds it:

- Build the feature and its new tests together; update the RD in the same change if behaviour or copy moves.
- Fill in **Result** and **Evidence** honestly after each verification. A FAIL stays FAIL until it's fixed and rerun.
- An approved eval's checks can't be changed or removed (the hook refuses); new checks can be added. Changing it needs the user's yes again: set it back to `In review` and ask.
- Done means `node .claude/hooks/eval-gate.mjs check <feature> --run` exits 0. Then set **Eval status** to `Passing` and the RD **Status** to `Shipped`. The hook blocks both, and stopping with a "done" claim, until the check passes.
