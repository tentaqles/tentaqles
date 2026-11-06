---
name: write-tickets
description: Turn a request or a PRD into tickets that workers can build without asking questions. Each ticket has rd.md (what to build) and eval.md (pass or fail checks), written and approved by the owner before any code exists. Use whenever the owner asks to write tickets, turn a PRD into work, spec a feature, scope or plan features, write requirements or acceptance criteria, break a list into work items, or lists things they want built, even if they never say "ticket". Also the first step in a new project, because it sets up the project profile. It writes tickets and never builds them.
---

# Write tickets

Each feature becomes a folder under `ticketsDir`, with two files.

- `rd.md` says what done means.
- `eval.md` proves it. Anyone can run it and get a clear pass or fail.

Workers run at the same time, each in its own worktree, and **they can't ask anyone anything while they work**. So a ticket settles every decision up front. Whatever is left open, a worker guesses, and two workers guess differently.

This skill writes tickets. It doesn't build. Read `.claude/README.md` first: it's the contract. In the commands below, `gate` means `node .claude/hooks/gate.mjs`, run from the project root.

The owner may answer in Portuguese or English. Judge what they mean by reading it, never by looking for a keyword.

## 1. First run: the profile

Do this if `.claude/project.json` is missing, or its `confirmed` is false. Otherwise skip to step 2.

1. If it's missing, copy `.claude/project.template.json` to `.claude/project.json`.
2. Read the project: manifests and scripts, README, CLAUDE.md, CI config, `.env.example`, entry points, how it starts. If a command doesn't exist, leave it out. Never invent one. Ask the owner about anything you can't find.
3. Fill in the profile.
   - `stack`: one of `cli`, `web`, `api`, `library`.
   - `capabilities`: `browser` if a browser tool can drive the app. `mockups` if it has a UI. `screenshots` if Playwright is in the project or the owner wants it. `ports` if it runs a server. `testAccounts` if it has sign-in. `teams` stays off unless the owner asks.
   - `ticketsDir`: an existing specs folder if there is one, otherwise the default. `mainBranch`: the real one.
   - `riskQuestion`: keep the default, or a sharper question for this project.
   - `highRiskPaths`: sign-in, permissions, money, migrations, deletion, personal data, security code, CI and release config.
   - `gates.round`: the fast commands every round must pass. `gates.release`: the full gate. Take them from CI and the scripts.
   - `blind`: `commands` and `read` are what a stranger would have. `port` if it's a server.
   - `app`: `build`, `start`, `readyPath`, `env`, `testData`, `ports`, `accounts`, `healthPaths`. Leave out what doesn't apply.
   - `app.coreFlows`: the 3 to 6 flows a user can't live without.
4. If it's an app, check it really starts. Pick a spare port, start it there in the background, request `readyPath`, then stop it. Fix the command until this works. If it needs a gitignored file such as `.env` to start, propose listing it in `.worktreeinclude` at the project root, so each new worktree gets a copy.
5. Show the owner the filled-in profile and ask them to confirm or correct it. Point at the core flows and the High-risk paths. Set `confirmed: true` only on their yes.
6. Add `.job/` and `.claude/worktrees/` to `.gitignore` if they're missing.
7. Run `gate doctor`. Fix what you can, and tell the owner what's left.

The profile and `.gitignore` go in the first ticket's commit (step 8), because workers branch from that commit and need them.

## 2. Split into features

One ticket per feature. A feature is the smallest piece that can ship and be checked on its own, and is worth something to a user.

- "Add sign-in and a dashboard" is two tickets.
- "Add a button, and make it save the form" is one, because the button is useless without the saving.
- A large epic or a PRD becomes several. Tell the owner how you split it, so they can change it.

If a ticket for the same feature already exists, update it rather than making a duplicate, and say so. If it's already approved, see step 9.

## 3. Interview

Ask **one question at a time**, until a worker could build the feature without making any decision that changes what a user sees or gets. Use AskUserQuestion where options make sense, with your recommendation first.

- Ask about who it's for, what happens on each path, what the user sees, what happens when it fails, limits, and what's out of scope.
- A small call that doesn't change the outcome, such as a variable name, you make yourself. Write it under Decisions.

There is no "open questions" section. A ticket with an open question isn't finished.

## 4. Read the code

Read the code the features touch: the files, routes, data models and APIs, the patterns already in use, the test runner and how tests are named. Keep it to what you need to be precise. Name real paths, real commands and real labels in the ticket.

If this raises a new question, go back to step 3. Don't ask what the code already answers.

## 5. Rate the risk

Use the rules in `.claude/skills/rate-risk/SKILL.md`, with `riskQuestion` and `highRiskPaths` from the profile. Anything whose Affected areas sit on a High-risk path is High.

**Low risk gets no ticket.** Tell the owner: this one is Low because of what breaks, so it needs a normal session and then `/code-review low`. Stop there for that feature.

## 6. Write the ticket

The folder is `<ticketsDir>/NNN-slug`. Find the highest number already there and continue from it. Start from [rd-template.md](rd-template.md) and [eval-template.md](eval-template.md).

### rd.md

- Every requirement is testable. "Must be fast" fails. "Results appear within 300 ms for up to 1,000 items" passes.
- Say what the system does, not how. Name a library only if the owner or the code requires it.
- One behaviour per requirement. Quote user-facing text exactly.
- Requirement ids match `requirementId` in the profile. Never renumber or reuse one.
- Team ticket (only with `capabilities.teams`): use `team` when separate parts must agree on a shared shape to work at all. Affected areas gets one block per part, so each teammate owns different files. Otherwise it's `solo`.

### eval.md

It decides whether the feature counts as done, so it has to be strict enough that bad work fails it.

Medium and High need U, F, Q, R and S checks. High needs at least 3 S, with at least one that runs and attacks it.

- **U**: each role, each entry point, first and repeat use, smallest and largest valid input.
- **F**: walk this list and write a check for each item that applies. Validation (empty, too long, wrong format, odd characters). Not signed in, or session expired. The wrong user. Stale data, double submit. Two at once on something unique. A dependency down or slow. Time boundaries and time zones. Empty lists and very long ones. A deleted or unknown id. Say under Not covered why the rest don't apply.
- **Q**: the project's speed budgets, no needless new dependency, and for a UI: labelled inputs, visible focus, keyboard use, no horizontal scroll at 360 px.
- **R**: each specific thing this could break in what shares its code, routes or tables. Name the existing test where there is one. The gates run the full suite anyway, so don't list it.
- **S**: access is rechecked on the server, personal data reaches only those allowed, secrets stay out of output and logs, input can't become a command.

What makes a check concrete:

| Vague | Concrete |
|---|---|
| "Validation works" | "Submit with Title empty. The form isn't sent, and `Title is required` appears under Title." |
| "The API handles errors" | "`curl -i -X POST localhost:<port>/api/tasks` returns status 400 and a body containing `title_required`." |
| "It's saved" | "Move card A to Done and reload. A is still in Done." |
| "It's responsive" | "At 360 px there is no horizontal scrollbar." |

Methods:

| Method | When |
|---|---|
| `test TestName` | Anything a test can drive. Name a real test, or the one the builder must write. The verifier reads it. |
| `command` | A shell command with its exact expected output. |
| `inspect` | Read code or data for a property. |
| `browser` | A real browser. Only with `capabilities.browser`. |
| `human` | Only a person can judge. It blocks a release, not a landing. |

The parser and lint are strict. Keep to these:

- **Expect is one line.** The parser reads only that line.
- **Steps are indented** under the Steps line.
- **Nothing in curly braces** in Covers, Method, Steps or Expect. Lint reads any pair of them as a leftover placeholder, including real JSON. Quote a fragment without the braces, or check it in a test. Write `<port>` for a port.
- **Checks go last in the file.** Approval freezes the file's bytes so far, and later checks can only be appended. So Setup and Not covered go above the checks, and no coverage table goes below. Lint works out coverage itself.
- **Every check has a Covers line.** It names the requirement it proves. A check that guards something else names it in a few words, such as `mockup.html`.

## 7. The mockup and the board

**Mockup.** Only with `capabilities.mockups`, when the ticket has UI. Draw `<ticketsDir>/NNN-slug/mockup.html` so the owner sees the feature before anything is built.

- One self-contained page, with every state on it, each labelled: default, empty, loading, pending, each error, success, signed out. Include the phone layout in a 360 px frame.
- Use the app's real fonts, colours, class names and chrome. Copy comes from `rd.md`, word for word. No invented reviews, logos or numbers.
- Set `**UI:** [mockup.html](mockup.html)` in `rd.md` and add a D check against it. Its Method is `browser` with `capabilities.browser`, `command` with `capabilities.screenshots`, and `human` otherwise.
- With `capabilities.screenshots`, check that it renders, then look at the images. Horizontal scroll at 360 px is a bug in the mockup too.

```
node .claude/skills/write-tickets/scripts/shoot.mjs <ticketsDir>/NNN-slug/mockup.html .job/NNN-slug/mockup --widths 360,1280
```

No UI means `**UI:** none`.

**Board.** Add a row to `<ticketsDir>/BOARD.md`. If the file doesn't exist, create it from [board-template.md](board-template.md). The Mode column exists only with `capabilities.teams`.

Compare Affected areas with every ticket that hasn't landed, including those already ready. If they don't overlap, Runs is `alongside`. If they do, it's `waits for NNN: reason`, or you move the shared change into one ticket and list it under Do not touch in the other.

## 8. Lint, ask, approve

1. Run `gate lint <t>` until it's clean. Lint doesn't read the prose in `rd.md` or the check titles, so also search both files for a leftover `{`.
2. Show the owner, for each ticket: the path, the risk and why, the check count per category, the board row, and anything under Not covered.
3. **List every decision you made on your own**: wording, limits, states, edge cases, one line each. Nothing gets approved by accident.
4. Ask with AskUserQuestion, one question per ticket: approve it, or what to change? Make any changes, lint again, and ask again.
5. Move on only after an explicit yes from the owner to that ticket. A reply that names several tickets counts for each one. "Yes, but change X" isn't a yes yet. "Looks fine so far" isn't either. If you can't tell, ask again. Without a yes, the ticket stays `draft`.
6. On a yes, run these in order:

   ```
   gate approve <t>
   gate board <t> ready
   ```

7. Commit with explicit paths, because workers branch from the checked-out commit. Add `.claude/project.json` and `.gitignore` if step 1 changed them.

   ```
   git add -- <ticketsDir>/NNN-slug <ticketsDir>/BOARD.md
   git commit -m "docs(tickets): NNN slug" -- <ticketsDir>/NNN-slug <ticketsDir>/BOARD.md
   ```

## 9. Changes after approval

- Any change to an approved `rd.md`, an existing check or `mockup.html`: run `gate board <t> draft`, make the change, then lint, a new yes, `gate approve`, `gate board <t> ready` and a commit. `gate` doesn't hash the mockup, so this rule is what protects it.
- If a worker already has the ticket, tell the owner first.
- New checks may be appended to the end of `eval.md` without a new yes. Lint and commit them.

## 10. Hand over

Tell the owner, in a few lines: each ticket's path and what it is, which tickets run alongside and which wait, and which are still draft. Don't paste the files.

Then give the next step: open the router through their normal launcher with `claude --agent router --name router`, and tell it a ticket number, or "go".
