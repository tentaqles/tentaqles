# Specs

<!--
write-evals creates this file as specs/CLAUDE.md on its first run (Claude Code loads a CLAUDE.md in a
subfolder whenever it works in that folder). It fills in the feature map and invariants from the code,
and keeps the feature map up to date as features ship.
-->

Product specs for **{{App name}}**: {{one line on what the app does and for whom}}. These docs are the source of truth for *what* the app does. Code lives in {{../src}}.

## Layout

| File | What it holds |
|---|---|
| {{`prd.md`}} | {{Product requirements, if the project has them: goals, non-goals, the full feature list, open questions}} |
| `features/<feature>/rd.md` | One folder per feature, named after it, holding its requirements doc (RD) |
| `features/<feature>/eval.md`, `mockup.html` | The approved eval a feature is built and verified against, and its design mockup when it has UI. Written by `write-evals` before any code; a feature ships only when `node .claude/hooks/eval-gate.mjs check <feature> --run` passes |

## Feature map

Every code file belongs to a feature. Reviews use this table to find the RD for a changed file; a changed file that maps to no feature is a finding.

| Feature | Status | Risk | Main code |
|---|---|---|---|
| {{team-invites}} | {{Shipped / Planned}} | {{High}} | {{`src/routes/settings/team/*`, `src/api/invites.ts`}} |

## Writing and updating specs

- **RD structure**, in this order: header table (Status, Risk, Routes, Code) → Overview → User stories → Requirements (a table of `REQ-n` rows) → UI, data and messages → Acceptance criteria (**Given / when / then**) → Edge cases and known limits → Out of scope. Start from `.claude/skills/write-evals/rd-template.md`.
- **IDs are permanent.** Never renumber or reuse a requirement ID. Add new requirements at the end; refer to other features by folder name.
- **Status** is `Planned` or `Shipped`. Keep the feature map in sync with RD headers.
- **When a feature ships**: RD status `Shipped`, add its row to the feature map with its code, and update every "known limits" line in other RDs that pointed at it.
- **When code changes behaviour**: update the RD in the same change, including exact user-facing copy (errors, headings and button labels are quoted verbatim).
- Known limits name the feature that will fix them, or say none is planned.
- Use absolute dates.

## Invariants code must keep

Hard rules for anything built here; changing one needs a spec change first. Keep them concrete enough to check.

- **Security**: {{how passwords and tokens are stored; how every action checks auth and ownership; what redirects accept}}.
- **Data**: {{the database constraints that are the source of truth: unique fields, one-per rules, foreign keys; how races resolve}}.
- **Privacy**: {{exactly which personal data is collected and who can see it}}.
- **Third parties**: {{email, payments, storage: what happens when each is down; timeouts}}.
- **UI**: {{design system: fonts, colours, shared component and class locations; accessibility bar}}.
- **Framework**: {{version-specific rules the code must follow}}.

## Open questions

{{Unresolved product decisions. Don't decide these in code; flag them.}}
