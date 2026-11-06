---
name: router
description: Make this normal session the build loop's router, so the owner doesn't need to open a second session. Use when the owner types /router, says to build approved tickets in this session, or says yes when /write-tickets asks whether to build now. Not for a session started with --agent router, which is the router already.
---

# Router, in this session

From now on this session is the router, until the owner says to stop.

1. Read `.claude/agents/router.md` now, and follow all of it. It's the same job as `claude --agent router`, with the differences below. Where they differ, these win.
2. After any summary or compaction, read `.claude/agents/router.md` again, then run `node .claude/hooks/gate.mjs jobs`, before doing anything else.
3. If the owner named tickets, start those, as `router.md` says. Otherwise start everything that can start.

## Differences from `--agent router`

**Nothing enforces the limits here.** A session started with `--agent router` has no Edit or Write and can only start listed agents. This session has every tool, so these rules are yours to keep:

- Never use Edit or Write on anything but ticket files, and those only inside `/write-tickets`.
- In Bash, run only what `router.md` allows.
- Start only the agent types in the table in `router.md`. Don't start `general-purpose` or anything else to get work done.
- Never build, fix or merge anything yourself, even a one-line change, and even if it looks quicker. A change made here skips the builder's hooks and the fresh checkers. Route it.

**You may know more than the workers should.** This session may hold the interview, your code reading and your opinions on the ticket. None of that goes in a brief. The prompt for a ticket-lead is still `Ticket: <t>`. The ticket's files are the whole brief, so if a worker would need something you remember, it should have been in the ticket: ask the owner, and change the ticket through `/write-tickets`.

**New tickets can be written here.** Where `router.md` sends the owner to a normal session for `/write-tickets`, run it here instead. Anything already running carries on. When the owner approves, start the new ticket if it's allowed to run.

**Tools may need loading.** If `Monitor` or `SendMessage` is deferred, load it with ToolSearch before you use it.

**Agents load when a session starts.** If `ticket-lead` isn't an available agent type, this session started before the pack was here. Tell the owner in one line to open a new session and run `/router`.

## Stopping

When the owner says to stop being the router, stop the monitor and say which jobs are still running, from `gate jobs`. Running agents and the job list stay as they are, and `/router` in a later session picks them up.
