---
name: rate-risk
description: Rate a ticket, a feature or the current diff Low, Medium or High by asking one question about what happens if it breaks, and say what that rating means for checking. Use when the owner asks how risky a change is, how carefully it needs checking, whether something needs a ticket, or types /rate-risk. With no argument it rates the current diff. It reports a rating and changes nothing.
---

# Rate risk

Checking everything at full depth costs time. Checking nothing lets the expensive mistakes through. So the check matches the risk. This skill decides the level, so nobody picks the depth by feel.

It only reports. It doesn't edit `rd.md`, write checks or touch code. `/write-tickets` records the rating in the ticket it writes.

## The question

Read `riskQuestion` and `highRiskPaths` from `.claude/project.json`. If there's no profile, use the default question below, with no High-risk paths.

**The default question: what happens to a customer if this breaks?**

| Level | When it breaks | Examples |
|---|---|---|
| **Low** | Something only looks wrong. Everything still works. | Copy, spacing, colours, a static page, an icon, an internal label |
| **Medium** | Something stops working, but nobody loses anything. You fix it later and the customer tries again. | Search returns nothing, a filter is ignored, a form won't submit, a page errors, a notification is late, a chart is wrong |
| **High** | A customer loses something, or sees something they shouldn't, and you can't take it back. | Charged twice, data deleted or overwritten, locked out of an account, another person's data shown, a message sent to the wrong person, a permission check skipped |

With the default question, these are High no matter how small the change: sign-in, sessions, passwords, permissions and ownership checks, money, deleting or migrating data, personal data, anything sent to customers, anything that can't be undone.

**If `riskQuestion` asks something else, it replaces this table.** Answer that question. Then Low means nothing is lost and nothing stops working. Medium means something stops working and a retry recovers it. High means the loss or exposure the question is about, and it can't be taken back.

## The rules

1. **Rate the worst plausible break, not the likely one.** "It probably works" isn't a rating.
2. **Anything on a `highRiskPaths` path is High**, no matter how small.
3. **Anything that writes data is at least Medium.**
4. **Unsure between two levels? Pick the higher one**, and say why in one line.
5. **Rate impact, not effort.** A one-line change to an ownership check is High. A 500-line new static page is Low.

## Steps

1. **Find what's being rated.**
   - A ticket number or slug: read its `rd.md` under `ticketsDir`, including Affected areas and the current Risk line.
   - A description in the owner's words: use it as is.
   - No argument: the current diff. Run `node .claude/hooks/gate.mjs risky <mainBranch>`. It lists the committed files on High-risk paths and exits 1 if there are any. It doesn't see uncommitted work, so also run `git status --porcelain` and compare those paths to `highRiskPaths` yourself. Read the diff itself with `git diff` and `git diff <mainBranch>...HEAD`.
2. **Find what it touches.** Read the code it lands in, or will land in: what data it writes, who can reach it, whether it handles money, access, personal data or outgoing messages, and what else shares that code. Don't rate from a name alone. A "profile page" that edits email addresses is a sign-in change.
3. **Answer the question.** Write down the worst thing that happens if each part breaks, one line each. Pick each part's level. The whole thing's level is its highest part's level.
4. **Report in this shape, then stop.** Don't start writing checks or code.

```markdown
## Risk: <ticket or change>: <Low | Medium | High>

**Worst break:** <what happens to whom, one line>
**Why this level:** <the rule or table row that decided it, one line>

| Part | If it breaks | Level |
|---|---|---|
| ... | ... | ... |

**What this means:** <the column for this level from the table below>
```

## What each level means downstream

This table is the contract, from `.claude/README.md`. Every other part of the build loop follows it.

| | Low | Medium | High |
|---|---|---|---|
| Ticket | None. Normal session, then `/code-review low`. | Yes | Yes |
| Checks needed | | U F Q R S | U F Q R S, at least 3 S with one that runs and attacks it |
| Each round | | builder, then verifier and reviewer | same, stronger model on the reviewer |
| Before landing | | | `/deep-review`, `/first-time-user` if user-visible behaviour changed |

`node .claude/hooks/gate.mjs lint <t>` enforces the checks row.

## If the rating disagrees

- The rating can go up at any time, for example when a review finds the change touches a High-risk path after all. Say so. An approved ticket's `rd.md` is frozen, so a new Risk line means going back through `/write-tickets` for a new yes.
- Going down on an approved ticket works the same way, and needs the owner's yes.
- Don't rate to save effort. If the owner wants a lower rating than the rules give, say what the rules give and why. Then their call stands, and it goes under Decisions in the ticket.
