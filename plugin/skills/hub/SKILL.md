---
name: hub
description: Work the local tq hub board of this client workspace - see the decisions agent runs are waiting on, explain each in plain language, add a card, or show one. Reads tq hub list --waiting --json (bounded to 2 s). Answering is human-only, so when you pick an answer it prints the exact tq hub answer command for you to run in your own terminal and never runs it. Use when the user says "what's waiting on me", "hub", "pending decisions", "answer the plan approval", "what does the run need", "add a card", "show card c-...", or "/tentaqles:hub".
argument-hint: '[waiting | add "<title>" | show <card>]'
---

# Hub

`tq hub` keeps one board per client workspace and runs agent-ready cards headless
through `/tentaqles:build` (tq's `docs/HUB.md`). Every human stop of a run becomes a
decision waiting on the user. This skill shows those decisions, explains them, and
hands the user the command to answer. It never answers anything itself.

`$ARGUMENTS` is empty or `waiting` (what is waiting), `add "<title>"` (add a card) or
`show <card>`.

## 1. What is waiting

```bash
tq hub list --waiting --json --timeout 2s 2>&1; echo "hub_exit=$?"
```

- `hub_exit=0` and `[]`: say "Nothing is waiting on you in this workspace." and stop.
- `hub_exit=0` with items (newest first): for each, one short paragraph in plain
  words, then the options. By `decision.kind`:
  - `plan-approval`: the run planned the work and waits for approval; summarise the
    checkpoints and verify commands from `decision.text` (approving freezes them).
  - `result`: the run finished its checkpoints; say what to try. Approving finishes the
    build; `request-changes` sends a note back into the engine. The review page
    (`tq hub review <card> --open`) shows the evidence from the gate's records.
  - `judge`: a reviewer could not judge something; list the rows from `decision.text`.
  - `question`: the question as asked. When `decision.data.hub_asked` is true, the run
    stopped before its build existed (often an open PRD decision) and the hub asked for
    it: the options are `answer`, whose note is the answer and is required, and `stop`.
    Answering posts the note as the user's card comment and resumes the same run.
  - `permission`: the run was refused a tool (`allow-once` allows it for one round) or
    stopped at its budget (`raise-and-resume` starts a new run; a note such as
    `rounds=20` sets the new limits).
- Any other exit: no hub data for this workspace. Say so in one line with the first
  line of the output (for example "No hub data for this workspace: no workspace here")
  and stop. One bounded call; never retry in a loop.

## 2. Answering is the user's

When the user picks an answer in the conversation, run nothing. Print the exact
command for them to run in their own terminal (PowerShell, Windows Terminal or a
macOS/Linux terminal), with the card id, the decision id and one of the options
exactly as listed:

    tq hub answer <card> <decision> --choice <option> --note "<their note, if any>"

For a hub `question` the note is the answer and cannot be empty:
`tq hub answer <card> <decision> --choice answer --note "<their answer>"`. A plain
card comment also reaches the next round of a run (under "Notes from the human on
this card"), but it does not resume a waiting run; only the answer does.

Mention that the hub page (`tq hub open`) has the same buttons. Never run `tq hub answer`,
`tq hub move <card> done`, `build-gate answer`, `build-gate approve` or `build-gate token`
yourself: tq and the gate refuse them from an agent session, and the answer is the
human's.

## 3. Add a card

If the body has more than one line, write it to a temporary file first, then:

```bash
tq hub add "<title>" --kind <feature|fix|hotfix|analysis|chore|project> --body-file "<path>" --timeout 2s 2>&1
```

Add `--tag agent-ready` and `--project <folder relative to the workspace root>` only
when the user asks for the card to run. Budgets are the user's: tell them to pass one
themselves if they want it.

## 4. Show a card

```bash
tq hub show <card> --timeout 2s 2>&1
```

## Never

- run `tq hub answer`, move a card to a done lane, or any human-only `build-gate` command
- set a budget for the user
- retry a failed `tq hub` call in a loop; one bounded call per step
- copy card text into another tool; the hub masked what it stored, keep it there
