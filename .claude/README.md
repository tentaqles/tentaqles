# The build loop

A portable development process for Claude Code. You talk to one session, the router. Each piece of work is a ticket whose checks are written and approved before any code exists. Each ticket is built in its own git worktree and judged by agents that never saw how it was built. The loop stops and asks you when it stops making progress.

This file is the contract every agent follows. Everything specific to a project lives in `.claude/project.json`, so `.claude/` can be copied to another project: see [PORTING.md](PORTING.md). Design record: `docs/` is deliberately not involved; the rationale for each rule is in the agent or skill that uses it.

## How you use it

```
/write-tickets add X                  interview, risk, rd.md, eval.md. You approve.
claude --agent router --name router   through your normal launcher, then "012" or "go"
/fix-a-bug the guard denies X         reproduce, verdict, cause, failing test first, fix
/first-time-user                      a blind agent tries to use it
/deep-review                          fixed-script audit of a branch
/rate-risk                            rate the current diff
```

## Who does what

| Agent | Job | Writes |
|---|---|---|
| router | Talks to you, starts ticket-leads, lander, reproducer, tracks them. Never builds. Has no Edit or Write unless a project turns teams on. | `gate job` only |
| ticket-lead | Runs the review loop for one ticket in its own worktree. Triages. Returns stops to the router. | `.job/` only |
| builder | The only role that writes product code. Commits on the ticket branch. | code, tests, its report |
| verifier | Fresh each round. Runs every eval check, and reads the test behind each `test` check. Reports results. | `.job/` evidence and findings |
| reviewer | Fresh each round. Reads the diff against `REVIEW.md`, and for scope. Read-only. | its reply |
| lander | One at a time. Merges in a detached worktree, gets the merged tree verified, then fast-forwards the branch. Release mode runs the full gate and opens the PR after your go-ahead. | merges, board status |
| reproducer | Bug in, verdict out. Small fix on its own branch. | a fix branch |
| first-time-user | Blind. Uses only what a stranger would have. | its reply |

Layers: `router → ticket-lead → builder / verifier / reviewer`.

Agents can't talk to you. A question comes back up to the router, which asks you and resumes the agent with your answer.

## Files

| File | Written by | What |
|---|---|---|
| `<ticketsDir>/BOARD.md` | `gate board` | `\| # \| Title \| Status \| Risk \| Runs \|`, plus `Mode` where teams are on. Status: `draft`, `ready`, `landed`, `parked`. Runs: `alongside`, or `waits for NNN: reason`. |
| `<ticketsDir>/NNN-slug/rd.md` | `/write-tickets`, then frozen | What to build. Header lines `**Status:**`, `**Risk:** Medium: why`, `**UI:** none`. Sections: Requirements (`REQ-n`), Out of scope, Affected areas, Do not touch, Decisions. |
| `<ticketsDir>/NNN-slug/eval.md` | `/write-tickets`, then append-only | The checks, format below. |
| `<ticketsDir>/NNN-slug/approval.json` | `gate approve` | Hash of `rd.md` (ignoring the Status line), length and hash of the approved `eval.md`. |
| `<ticketsDir>/NNN-slug/results.md`, `evidence/` | `gate board landed` | Copied in at landing. |
| `<ticketsDir>/NNN-slug/human.json` | `gate human` | Your results for human checks. |
| `.job/ticket.json` | `gate claim` | Which ticket this worktree works on, and its base commit. |
| `.job/<t>/status.json` | `gate step`, `gate block` | `ticket, step, round, note, branch, stepStarted, updated, released`. Steps: `claimed, build, verify, triage, waiting, done, blocked, failed`. |
| `.job/<t>/state.md` | ticket-lead | The ledger. Survives compaction. Re-read it first after any summary. |
| `.job/<t>/round-N/` | builder, verifier, reviewer, ticket-lead | `builder.md`, `verifier.json`, `reviewer.json`, `fix-list.md`. |
| `.job/<t>/evidence/` | verifier | `E-n.txt` with command, exit code and output; PNGs on web projects. |
| `.job/<t>/results/<tree>.md` | `gate record` | One line per check, `ID \| PASS or FAIL or HUMAN \| evidence`. Keyed by tree hash, so recording doesn't move HEAD. |
| `.job/router-state.json` | `gate job` | The router's job list. Survives compaction. |

`.job/` and `.claude/worktrees/` are gitignored.

## Eval check format

```markdown
### U-01 Title
- **Covers**: REQ-1, REQ-2
- **Method**: test TestName | command | inspect | human | browser
- **Steps**:
  1. Exact steps, with the exact command or label.
- **Expect**: An observable result, quoted exactly.
```

Categories: **U** ways to use it, **F** ways it can fail, **Q** quality, **R** what must not break, **S** security and privacy, **D** design matches the mockup (web only).

- `human` checks can only be passed by you, with `gate human`. They block a release, not a landing.
- `browser` needs `capabilities.browser` in the profile.

## Risk

The question is in `riskQuestion` of `project.json`. Anything in `highRiskPaths` is High no matter how small.

| | Low | Medium | High |
|---|---|---|---|
| Ticket | None. Normal session, then `/code-review low`. | Yes | Yes |
| Checks needed | | U F Q R S | U F Q R S, at least 3 S with one that runs and attacks it |
| Each round | | builder, then verifier and reviewer | same, stronger model on the reviewer |
| Before landing | | | `/deep-review`, `/first-time-user` if user-visible behaviour changed |

## The gate

`node .claude/hooks/gate.mjs`; the top of the file lists its commands. `lint`, `approve`, `board`, `claim`, `step`, `block`, `run`, `record`, `check`, `carry`, `human`, `release`, `job`, `jobs`, `status`, `doctor`.

It's a speed bump against a model's mistakes and shortcuts, not a security boundary. Hooks run only on the builder, so a normal session runs none of them.

Rules every hook in this pack keeps: `"shell": "bash"`, `"timeout": 5`, no test suites inside a hook, every subprocess bounded. A hook crash logs to `.job/gate-errors.log` and lets the call through. `gate check` at landing fails closed. The exception is the blind guard, which fails closed because a leak spoils the test.

## Facts about Claude Code this design depends on

Verified on 2.1.319 by a live spike.

- The router's `Agent(a, b)` allowlist also limits what its subagents can start. It has to list every nested type.
- A nested agent shares its parent's worktree, unless its frontmatter says `isolation: worktree`.
- A worktree without changes is removed when its agent finishes. One with changes stays.
- `SendMessage` from a subagent resumes a nested agent, but the reply never reaches the parent. Hand off through a file and wait for that file. The router, as the main session, does receive replies.
- A subagent can message the router with `SendMessage` to `main`.
- `CLAUDE_PROJECT_DIR` stays at the session's root; a hook's `cwd` is the worktree. Hooks and the gate follow `cwd`.
- After a Stop-gate block, the agent's next reply replaces its final message. Reports go in files.
- Project agents load when a session starts. New agents need a new session.
- `/code-review` reads `CLAUDE.md`, not `REVIEW.md`, so `CLAUDE.md` imports `@REVIEW.md`.
- Workflows can't ask for human input mid-run, so the build loop isn't a workflow. `/deep-review` is.
- On Windows, `bash` can resolve to the WSL launcher. `gate run` finds Git Bash itself.

## Safety

- Builders can't push, merge, rebase, `reset --hard` or touch PRs.
- Nobody uses `git reset --hard` on the main branch. A failed merge never touches the branch, because it happens in a detached worktree and the branch moves only by fast-forward.
- Agents can't read `.env` files through the Read tool, in any permission mode. On a project without a guard like `tq`, shell reads aren't covered.
