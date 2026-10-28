---
name: refine
description: Make one tq hub card ready for an agent run - read the card and the PRD requirement it came from, run grill-me with the user on every business-rule branch the card touches, write a refined body with the decided rules and binary acceptance checks, hand the user the one command that saves that body (editing a body is human-only), then tag the card agent-ready (agents may edit tags on planning lanes). Use when the user says "refine card c-...", "make this card agent-ready", "grill me on this card", "is this card ready to run", or "/tentaqles:refine".
argument-hint: '<card>'
---

# Refine

`/tentaqles:plan-cards` makes one backlog card per FR and never tags it. This skill takes
one of those cards (or any card in a planning lane) to `agent-ready`: the human decides the
rules, the agent writes them down, and the card's body becomes the task text a headless run
gets (spec B §9). The human still moves the card into an active lane to start the run.

`$ARGUMENTS` is the card id (`c-…`). Without one, run `tq hub list --lane backlog --timeout 2s`
and ask which card.

This skill needs a human to interview. In headless mode (`TENTAQLES_HEADLESS=1` or
`TQ_HUB_TASK` set) say "refine needs the human; run /tentaqles:refine <card> in a session"
and stop.

## 1. Read the card

```bash
tq hub show "<card>" --json --timeout 2s 2>&1; echo "hub_exit=$?"
```

- `hub_exit` not 0: quote the first line and stop. One bounded call; never retry in a loop.
- Note `lane`, `kind`, `tags`, `project_dir`, `parent` and the `body`. If the body starts
  with a `---` front-matter block (a `budget:` line), keep that block exactly as it is.
- The lane must be a planning lane (backlog, todo or another non-active lane). If the card
  is already running, in review or done, say refining it now changes nothing and stop.
- Already `agent-ready`: say so and ask whether to refine it again anyway.

Then read what the card came from: the `Source: PRD <path> §4 FR-n` line in the body
names the PRD and the FR. Read that FR in the project's `docs/prd.md` (under `project_dir`
from the workspace root), the project's `CLAUDE.md`, and the parent card
(`tq hub show <parent> --timeout 2s`) for context. Read the code the FR touches when it
exists. Do not change any code.

## 2. Grill the user on this card

Follow `/tentaqles:grill-me` (its rules and question areas), scoped to this one card:

- one question at a time, each tied to a branch the code will take, with the likely
  options and a recommended default;
- never invent a rule: an answer the user does not know becomes an open question with a
  safe default and an owner;
- stop when every branch has an answer or an owner (usually 3 to 10 questions for one FR).

## 3. Write the refined body

Write it to a temporary file:

```bash
f=$(mktemp "${TMPDIR:-/tmp}/tq-refine-XXXXXX.md") && echo "$f"
```

The body, in this order:

1. the front-matter block, unchanged, if the card had one;
2. the FR text (what and why), tightened with the answers;
3. `**Rules decided**`: one bullet per answer ("a done todo can be edited; it stays done");
4. `**Acceptance checks**`: binary, observable statements a test can prove blind, the
   PRD's checks plus one per new rule; never scores or adjectives;
5. `**Open questions**` only if some remain (each with its owner);
6. the `Source:` line, unchanged.

If the answers change what the FR requires, update that FR's text and acceptance checks in
`docs/prd.md` too, so the PRD stays the source; an answer nobody could give goes into PRD
§10 as a `NEEDS-DECISION` item. The PRD edit is committed with the next change.

## 4. Save the body (the human's step)

An agent session cannot edit a card's title or body; tq refuses it (`human-only`). Post the
proposal on the card so it is on the record, then give the user the command:

```bash
tq hub comment "<card>" - --timeout 2s < "$f"; echo "comment_exit=$?"
```

Then print, for the user to run in their own terminal:

    tq hub edit <card> --body-file "<the temp file path>"

(or paste the body into the card on the hub page, `tq hub open <card>`). Wait until they
say it is done, then check it:

```bash
tq hub show "<card>" --json --timeout 2s 2>&1
```

The body must now hold the `**Acceptance checks**` you wrote. If it does not, say so and
wait again.

## 5. Tag it agent-ready

Only when the body is saved and nothing in **Open questions** blocks the build (and PRD §10
has no `NEEDS-DECISION` item this card depends on):

```bash
tq hub edit "<card>" --add-tag agent-ready --timeout 2s 2>&1; echo "tag_exit=$?"
```

Agents may edit tags on a card in a planning lane; a `human-only` refusal means the card is
no longer in one, so tell the user. Tagging starts nothing. End with the user's next step:
move the card into an active lane (the board, or `tq hub move <card> "in progress"` in their
terminal) to start its run.

## Never

- edit the card's title or body yourself, or run the `tq hub edit --body-file` command for
  the user; it is theirs
- tag `agent-ready` while a rule the build needs is still open
- move the card, start or answer a run, or set a budget
- invent a business rule the user did not give
- copy card or PRD text into another tool
