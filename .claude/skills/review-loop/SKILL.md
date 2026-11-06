---
name: review-loop
description: The inner loop for one approved ticket. The ticket-lead subagent claims the ticket, then loops builder, then verifier and reviewer, then triage, until `gate check` exits 0 and no P0 or P1 is open. Checkers are fresh every round and never see the builder's reasoning. Stops and returns to the router when the loop stops making progress. Used only by the ticket-lead.
---

# Review loop

One approved ticket in. One branch that passes its checks and an independent review out. You are the ticket-lead, a subagent in your own worktree. You never write code or judge it yourself. You brief agents, keep the ledger, sort what they return, and decide whether to loop or stop.

```
claim ─▶ ┌─ builder ─▶ verifier ∥ reviewer ─▶ triage ┐
         └───────────────── fix list ◀───────────────┘
                              │ gate check exits 0, no open P0 or P1
                              ▼
                        final message to the router
```

`gate` below means `node .claude/hooks/gate.mjs`. `<t>` is the ticket's folder name. Agent types are `builder`, `verifier` and `reviewer`. Their instructions are in their own agent files. Don't restate them in a brief.

## Why fresh checkers

The agent that built a change made every decision in it, so it ends up agreeing with itself. The checks have to come from somewhere else.

- A new verifier and a new reviewer every round. Never resume one.
- Nothing about the builder's reasoning goes in their briefs. No summary, no hypotheses, no "the builder says X is fixed", no "focus on X".
- The builder gets the fix list, not the checkers' full reports, so it fixes what was found rather than arguing with it.

## 0. Start

1. If `.job/ticket.json` already names this ticket and `.job/<t>/state.md` exists, you are resuming. Read `state.md` and continue from its round. Don't claim again: a second claim moves the base.
2. Otherwise run `gate claim <t>`. It refuses a ticket whose approval doesn't match. Stop there and return that output as a STOP.
3. If the prompt has a `From:` line, do "Starting from a branch" below.
4. `gate lint <t>` and `gate approval <t>` must both exit 0. If not, return a STOP with the output. An approved ticket changes only with the owner's yes.
5. Read the risk from `rd.md`. High adds the steps marked High below. Medium is the normal loop. A Low ticket shouldn't be here: run it as Medium.
6. Read `.claude/project.json`. Only if `capabilities.ports` is on, give the builder and verifier the port `app.ports.workerBase` plus the ticket number. Only if `capabilities.testAccounts` is on, give them the account `app.accounts.workerPrefix` plus the ticket number. Nothing about ports, accounts or a browser goes in a brief otherwise.
7. Create `.job/<t>/state.md` (format under "Ledger"). The base is `base` in `.job/ticket.json`.

### Starting from a branch

The router sends `From: <branch>` in two cases.

- **`From: <branch> Worktree: <path>`**: the ticket was in flight in an earlier session, and that session's agent can't be resumed. Your new worktree takes it over.
  1. `gate carry <t> --from <path>`, which brings `state.md`, rounds and evidence.
  2. `git merge --ff-only <branch>`. If that fails, the main branch moved, so run `git merge --no-edit <branch>`. A conflict is a STOP.
  3. `(cd "<path>" && node .claude/hooks/gate.mjs step <t> done "continued in <your worktree>")`, so the monitor stops watching the old worktree.
  4. Update Base in `state.md`, then continue from its round. The tree may have changed, so run a fresh verifier and reviewer before you trust an earlier result.
- **`From: <branch>` with no worktree**: a team built the ticket on that branch. Run `git merge --ff-only <branch>`. Then go to round 1 as normal, and add to the builder's brief: `The code is already on this branch. Make the round gate pass, and write your report.` The checks need the builder's hooks to have run, so don't skip this builder.

## 1. Builder

Run `gate step <t> build --round N` BEFORE you start or resume the builder. The stop gate and the wait below both read that status.

Brief:

> Ticket: `<t>`. Round: `N`. Mode: `build` (round 1) | `fix` | `rethink`.
> Fix list: `.job/<t>/round-<N-1>/fix-list.md` (fix and rethink only).
> Write your report to `.job/<t>/round-N/builder.md`.

**Round 1 and rethink: FRESH.** One Agent call with `subagent_type: builder`, foreground. Save its agent ID in `state.md`.

**Later rounds: RESUMED** with `SendMessage` to that agent ID, with the same brief. It already knows the code.

A resumed nested agent's reply never reaches you. This is verified. So after `SendMessage`, wait for the done marker with a Bash poll loop, and set the Bash `timeout` parameter to `560000`, because the default is far shorter. The builder's Stop hook writes `.job/<t>/round-N/builder.done`, containing the tree hash, when it passes. `builder.md` alone can appear before that, so it isn't the signal.

```bash
for i in $(seq 1 100); do [ -f .job/<t>/round-N/builder.done ] && break; grep -qE '"step": *"(blocked|failed)"' .job/<t>/status.json && break; sleep 5; done; grep '"step"' .job/<t>/status.json; ls .job/<t>/round-N/
```

Write the paths out literally, with no shell variables. Each call lasts about 8 minutes. Repeat it until `builder.done` is listed, or the step is `blocked` or `failed`. If nothing comes after about 40 minutes, or the `SendMessage` itself fails, start a fresh builder with the fix list. Note that in `state.md`.

Start the verifier only after the done marker. The verifier and `gate record` then apply to the tree in that marker.

When the builder is done, read only the **Status**, **Commit**, **Questions** and **Decisions I made** of its report, and the status file. Record the commit in `state.md`, and keep its decisions for your final message.

- **`blocked`**: the note is a question only the owner can answer. Return it to the router as a STOP. When you are resumed with the answer, record it under Decisions, delete that round's `builder.md` and any `builder.done` so neither the stop gate nor the wait sees the old one, run `gate step <t> build --round N` to clear the block, and resume the builder with the answer.
- **`failed`**: the stop gate released this builder, and `gate check` will refuse the ticket. Return a STOP with the note.
- **Questions in the report that block the build**: the same as `blocked`. Never answer a product question yourself.

## 2. Verifier and reviewer

Run `gate step <t> verify`. Then start a fresh verifier and a fresh reviewer in parallel: two Agent calls in one message, both in the foreground. They get the ticket, the round and the scope, and nothing else.

**Verifier** (`subagent_type: verifier`):

> Directory: `<absolute path of this worktree>`. Ticket: `<t>`. Round: `N`.
> After your own pass, re-check these earlier findings: `<IDs and one-line titles of open findings, or "none">`.

**Reviewer** (`subagent_type: reviewer`, and on High risk set `model: opus` on the call):

> Directory: `<absolute path of this worktree>`. Ticket: `<t>`. Round: `N`.
> Scope: `git diff <base>...HEAD` (round 1) | `git diff <sha of round N-1>..HEAD` (later rounds).
> Already reported and still open, don't report again: `<ID, class and one-line title of each OPEN finding, or "none">`.

The list holds OPEN findings only. A closed finding that comes back must be reported, so never put a closed finding on it.

The scope narrows after round 1 because a reviewer that re-reads everything mostly rediscovers old problems, one at a time, and every round costs usage.

Never run the builder at the same time as either of them. It would change the tree they are judging.

The reviewer has no Write, so you save its reply: write the JSON array to `.job/<t>/round-N/reviewer.json`, and its "Not checked" list to `state.md`. A question on that list that needs a product call is a decision for the owner.

The verifier gives you results in `.job/<t>/round-N/results.md`, one line per check, and findings in `verifier.json`. You run `gate record <t> .job/<t>/round-N/results.md`, which stores them against this exact tree.

If an agent returns without its findings, or they don't parse, start a fresh one for the same role and round once. If that fails too, return a STOP.

## 3. Triage

Run `gate step <t> triage`.

1. Run `gate check <t>`. Its output is ground truth. A verifier saying PASS doesn't override it.
2. Read both findings files. For each finding:
   - **Drop** it if `REVIEW.md` says to ignore it, or it has no repro and no rule. Record why in `state.md`.
   - **Merge** duplicates. One root cause is one finding. Keep the higher severity.
   - **Match** it to an earlier finding with the same root cause or the same **class**. Keep the original ID (`L-03` style) and bump its Rounds open. A match against a closed finding or class is a recurrence: reopen it, and note it under Recurrences.
   - **Escalate** it, not to the builder, when the only fix would change `rd.md`, an approved check, or something under Out of scope or Do not touch, or when the project's own rules conflict. That's a decision only the owner can make. Editing an approved check to make it pass is forbidden.
3. The fix list is every open P0 and P1, every failing check, and every line from `gate check`. P2s go on it when a round is running anyway, unless the fix would touch code outside the ticket. A P2 alone never causes a round. P3s are reported, never looped on.
4. Group the fix list **by class**, so the builder fixes each kind of mistake everywhere it appears, not only where it was found.
5. Write `.job/<t>/round-N/fix-list.md` and update `state.md`.
6. Add the round's line to `state.md`, and send the same line to the router with `SendMessage` to `main`:

   `Round N: gate FAIL (k), P0 a / P1 b / P2 c open, x fixed.`

   Nothing longer. Use `gate PASS` when `gate check` exits 0.

## 4. Loop, stop or finish

**Done** when `gate check <t>` exits 0 and no P0 or P1 is open. Go to §5.

**High risk, before done:** run one more fresh reviewer over the whole diff, with the scope `git diff <base>...HEAD`, the do-not-repeat list as usual, and `model: opus`. Save it to `round-N/reviewer-final.json` and triage it the same way. A new P0 or P1 means another round.

**Stop** when any of these hold, in this order:

- The same finding is still open after 2 consecutive fix rounds.
- The P0 + P1 + failing count didn't go down for 2 rounds in a row.
- A fix added a new P0.
- A decision only the owner can make, including anything escalated in triage.
- Round `limits.roundCheckpoint`, or a multiple of it, ended without passing. This is a checkpoint asking whether to continue, never a hard cap.

More of the same fixing won't get there. Run `gate step <t> waiting`, then return to the router with the finding IDs, the evidence paths and these options:

- **rethink**: a fresh builder in `rethink` mode changes the approach, not only the failing lines.
- **keep going**: another round, as is.
- **take over**: the owner finishes it by hand. See "Take over" below.
- **park**: run `gate step <t> done "parked"` and end.

Before any STOP, run `git rev-list --count <base>..HEAD`. If it's 0, run `git commit --allow-empty -m "wip(<t>): keep the worktree"`. A worktree without changes is removed when you finish, and `.job/` is gitignored, so it wouldn't count as a change and your state would go with it.

**Otherwise loop** to §1 with round N+1.

**The router can resume you after a PASS too**, with findings from `/deep-review` or `/first-time-user`, or an error from the lander. Add them to the ledger with that source, write a fix list, and run another round from §1.

On a resume with a landing failure, `CONFLICT` or `FAILED`, the builder can't merge, so you start it. Merge the target branch named in the message's `Target:` line: `git merge --no-edit <target>`. Never assume `mainBranch`. If the message names no target, return a STOP. If the merge stops on conflicts, leave the markers in place, and put the files on the fix list with `Resolve these conflicts using both tickets' rd.md, then commit to finish the merge.` Then run a fix round as usual, since the tree is new.

### Take over

The owner chose to finish the ticket by hand.

1. Run `gate step <t> waiting "taken over by the owner"`, and return a STOP that says so. The owner works in this worktree and commits. A `waiting` ticket showing as stale is ignored with that reason.
2. When they say they're finished, the router resumes you. Check `.job/<t>/hook-marker.json`. `gate check` still needs that marker.
3. If it's missing, no builder hook has run yet, so a builder round comes first. Run §1 as round N+1, with a fresh builder. Add to its brief: `The code is already on this branch. Make the round gate pass, and write your report.` Then go on to §2.
4. If it's there, run `gate step <t> verify --round N+1`, with N+1 written as the number. Run `gate run <t> round`, because the owner's tree has no stamp yet. Then start at §2, with a fresh verifier and reviewer. No builder runs unless triage gives it a fix list.

## 5. Final message

Run `gate step <t> done`. Mark Phase `done` in `state.md`.

Propose a one-line `REVIEW.md` rule for each of these:

- a class that came back;
- a finding an earlier round should have caught;
- a finding dropped as noise.

The owner decides through the router. You don't edit `REVIEW.md`.

Your final message goes to the router, at most 8 lines:

```
Ticket: 012-slug: PASS   (or STOP: why, finding IDs, evidence paths)
Options: rethink, keep going, take over, park   (STOP only)
Branch: <branch>  Worktree: <absolute path>
Tree: <git rev-parse HEAD^{tree}>  Rounds: N
Decisions I made that the ticket didn't cover: ... or none
Open: <P2 and P3 findings, each as OPEN, or none>
Proposed REVIEW.md rules: ... or none
```

Non-blocking P2s and P3s go in `Open`. They don't hold up the merge. Never report PASS while `gate check` fails.

Put everything longer in `state.md` and point to it. You don't merge, push, or change the board. The lander does that.

## 6. Ledger

After any summary, re-read `state.md` before doing anything else.

`.job/<t>/state.md`:

```markdown
# <t> review loop
Started: YYYY-MM-DD · Risk: Medium
Branch: <branch> · Base: <sha> · Builder agent: <id>
Current round: 3 · Phase: build | verify | triage | waiting | done

## Rounds
| Round | Commit | Gate | Checks failing | P0 | P1 | P2 | Fixed | New |
|---|---|---|---|---|---|---|---|---|
| 1 | a1b2c3d | FAIL (9) | U-02, F-01, Q-01 | 1 | 3 | 2 | – | 6 |

## Open findings
| ID | Sev | Class | Source | Title | First seen | Rounds open |
|---|---|---|---|---|---|---|
| L-01 | P0 | missing-ownership-check | reviewer | cancel skips the ownership check | 1 | 1 |

## Closed / dropped
- L-04 dropped round 2: REVIEW.md lists it as a known limit.

## Recurrences (for REVIEW.md rules)
- class `copy-mismatch`: L-02 round 1, came back as L-07 round 3.

## Decisions (from the owner)
- YYYY-MM-DD: ...
```

`.job/<t>/round-N/fix-list.md`, for the builder:

```markdown
# <t> fix list, round N
Gate output:
<paste of gate check output>

## Class: missing-ownership-check
Fix every place this kind of mistake appears in the ticket, not only the ones listed.

### L-01 P0 <title>
- Where: `<file>:<line>`
- Rule: REVIEW.md rule, or REQ-4
- Repro: ...
- Expected: "..." (exact wording from the ticket)
- Actual: ...
- Evidence: `.job/<t>/evidence/E-3.txt`
```

The findings JSON that the verifier and reviewer produce is an array of:

```json
{ "id": "V-1", "severity": "P0|P1|P2|P3", "class": "kebab-case kind of mistake", "check": "U-01 | null",
  "title": "...", "where": "file:line", "rule": "...", "repro": "...", "expected": "...", "actual": "...",
  "evidence": [".job/<t>/evidence/E-1.txt"], "fixDirection": "...", "recheck": "L-03 | null", "recheckStatus": "fixed | still open | null" }
```
