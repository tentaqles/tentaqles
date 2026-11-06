---
name: fix-a-bug
description: Take a bug report, prove it by reproducing it twice, literally and then varied, and give a verdict (Confirmed, Not reproduced, Works as specified, Known limit, Spec silent). When it's Confirmed, prove the root cause, find the change that caused it, offer a revert on P0 and P1, write a failing test first, fix the cause, and have a fresh agent confirm it's gone. Use when the user says something is broken, wrong, failing or buggy, pastes an error, or types "/fix-a-bug <what's wrong>".
---

# Fix a bug

A report is a claim, not a fact. First watch it happen. Then find why, and which change caused it. Then fix the cause and leave a check behind so it can't come back. Never edit code for a bug you haven't watched happen.

```
intake -> what should happen -> reproduce twice -> verdict
                                                     | Confirmed
                                                     v
      root cause -> same cause elsewhere -> which change -> revert? -> failing test -> fix
                                                                                       |
                         report <- land <- gate risky, round gate <- blind re-check <--+
```

Read `.claude/project.json` first. It has `mainBranch`, `ticketsDir`, `jobDir`, `capabilities`, `gates` and `app`. Replace those names below with their values.

Pick a short `<slug>` for the bug. Everything for it lives in `BUG=<absolute path of this checkout>/<jobDir>/fix-<slug>/`, which is gitignored: `notes.md` (the running ledger), `repro-1.txt`, `repro-2.txt`, screenshots, logs. The fix gets its own worktree, and the lander removes that when it lands, so nothing you want to keep goes there. After a context summary, re-read `notes.md` before doing anything.

"The checked-out branch" below is the one this session has checked out. The lander lands on that branch, so the fix starts from it and this checkout stays on it.

The shell blocks are bash, which is Git Bash on Windows. Run git as plain, separate commands with literal values: Claude Code refuses git commands it can't verify in an isolated session.

## 0. Intake

1. The report is `$ARGUMENTS`, in the reporter's words. Copy it verbatim to the top of `$BUG/notes.md`. Don't rewrite it. Your reading of it is a hypothesis.
2. Pull out what it gives you: who, where, what they did, what they saw, what they expected, when, on what platform. Note what's missing.
3. Ask, at most 3 questions, only if you can't even pick a flow to try. Otherwise start. Missing details are things you vary in step 2.

## 1. Find what should happen

1. Find the ticket that covers it: `<ticketsDir>/BOARD.md`, then that ticket's `rd.md`. Copy the requirement (`REQ-n`) or quoted text into `notes.md` verbatim.
2. Read `REVIEW.md`, "What counts as a real failure", and the priorities in `CLAUDE.md`. Something can be a bug even if no ticket names it.
3. Read `REVIEW.md`, "What to ignore", now, before spending time. A known limit, a planned feature or an open product question isn't a bug to fix here.

## 2. Reproduce it

### 2.1 Set up

Start from current code. A stale main branch means you reproduce, and fix, something that may already be gone.

```bash
git fetch origin <mainBranch>
git rev-list --count <mainBranch>..origin/<mainBranch>
```

If the fetch fails, say so in `notes.md` and tell the owner what you're testing against. Never hide the error: `git pull --ff-only 2>/dev/null` once hid a stale main this way. If the count isn't 0, the main branch here is behind. On the main branch, run `git merge --ff-only origin/<mainBranch>` and read the result. On another branch, tell the owner and ask. If the project has no remote, note that and go on.

Run `<app.build>` if the profile has one. If the build itself fails, that's the bug: go to step 3.

**No browser (`capabilities.browser` is false).** Run the reported commands. Give the tool a throwaway home and state folder, so nothing touches the developer's real config. Save the command, exit code and output of each attempt to `$BUG/repro-<n>.txt`.

**Browser (`capabilities.browser` is true).** Use a production build on throwaway data. The developer's data must come back exactly as it was, whatever happens.

```bash
mkdir -p "$BUG"
BACKUP=<jobDir>/backups/fix-<slug>-$(date +%s)
<app.testData.setup, with {backup} = $BACKUP>
```

Check the port `app.ports.reproducer` is free. This works on every OS:

```bash
node -e "require('net').connect(<port>,'127.0.0.1').on('connect',()=>{console.log('busy');process.exit(1)}).on('error',()=>console.log('free'))"
```

If it's busy, ask before stopping anything. You didn't start it. Start `<app.start, with {port}> > $BUG/server.log 2>&1` with `app.env`, using `run_in_background`, and poll `curl -s -o /dev/null -w '%{http_code}' http://localhost:<port><app.readyPath>` until it answers 200. Use the account `app.accounts.reproducer`. Drive it with `agent-browser --session fix-<slug>`. To see a page, use `agent-browser` or a screenshot, never a desktop `open`, `start` or `xdg-open`.

### 2.2 Try it, twice

1. **Literal first.** Same user, place, inputs, platform. Capture the output, or a screenshot before and after the failing step. Check the logs and, in a browser, `console` and `errors`.
2. **Not seen? Vary what the report left open**, one thing at a time, and log each attempt in `notes.md`: each kind of user, empty, long and odd input (unicode, spaces, max length), retry and repeat, two at once (a race needs two separate sessions), a dependency misconfigured or down, each OS you can reach. In a browser also try logged in and out, 360 and 1280 px, keyboard and mouse, back, refresh, double click.
3. **Still not seen, and it smells data dependent?** Ask the owner before running against a copy of their real data. Never run against the original. If it only happens on their data, find out whether the bug is in the code or in the data.
4. **Do it again.** It counts as reproduced only if it happens on two separate attempts, each from a clean start. Evidence for both: `repro-1` and `repro-2`. If it happens once in several tries, say it's intermittent and give the rate.

### 2.3 Verdict

Write one at the top of `notes.md`:

| Verdict | Means | Next |
|---|---|---|
| **Confirmed** | You saw it twice, and it breaks a requirement, a `CLAUDE.md` priority or a `REVIEW.md` rule | Step 3 |
| **Not reproduced** | You tried the literal steps and the variations, and it works | Clean up. Report every attempt with evidence. Ask for the missing detail. Change no code |
| **Works as specified** | It happens, and `rd.md` says it should | Clean up. Quote the `rd.md`. Changing it is a new behaviour: offer `/write-tickets`, don't fix |
| **Known limit** | `REVIEW.md`, "What to ignore", covers it | Clean up. Name what will address it |
| **Spec silent** | It happens, and nothing says what should happen | Ask the owner what should happen. Their answer is a new behaviour, so it goes through `/write-tickets`, unless it plainly breaks a `CLAUDE.md` priority |

Give it a severity from `REVIEW.md`. Tell the owner a Confirmed P0 in one line now, before starting the fix.

## 3. Root cause

1. **Locate.** Follow the flow from the entry point down. Read the code you're blaming, all of it, not a grep line.
2. **Hypothesise, then prove.** Write "X happens because `file:line` does Y when Z". Prove it with something that flips the outcome: a log line, or a minimal input that triggers it and one that doesn't. A hypothesis you didn't test goes in the report as a hypothesis.
3. **Same cause elsewhere.** Search for the same pattern: the same helper, the same call shape, the same missing check. One root cause is one fix covering every site. List the sites in `notes.md`.
4. **Which change caused it.** Run `git log -L <start>,<end>:<file>` or `git log -p -- <file>` on the culprit lines. Note when they came in, in which branch, and what that change was trying to do, so the fix doesn't undo it.
5. **Bisect if it isn't obvious and the flow used to work.** Bisect moves HEAD, so it gets its OWN worktree. Never bisect in the checkout you're working in, or one an agent is using.

   ```bash
   git worktree add --detach .claude/worktrees/bisect-<slug> HEAD
   git -C .claude/worktrees/bisect-<slug> bisect start HEAD <known good commit>
   git -C .claude/worktrees/bisect-<slug> bisect run bash <absolute path to $BUG/bisect.sh>
   git -C .claude/worktrees/bisect-<slug> bisect reset
   git worktree remove .claude/worktrees/bisect-<slug>
   ```

   The script exits 1 when the bug reproduces, 0 when it doesn't, and 125 when a commit can't be tested. Keep it in `$BUG/` with an absolute path, since a new worktree has no `.job/`. If Claude Code refuses these commands in an isolated session, skip the bisect and say so in `notes.md`. Write the first bad commit in `notes.md`.
6. **Does the fix need a spec change?** `rd.md` is frozen, and approved checks in `eval.md` can't change. If the only fix changes a requirement or an approved check, stop and ask the owner. That work goes through `/write-tickets`.

## 3.5 Offer a revert (P0 and P1 only)

If the bug is P0 or P1, the culprit is one merge or commit, and a proper fix will take a while, offer the owner the fast undo before fixing:

> The bug came in with `<sha>` (`<branch>`, <date>). I can revert it on `fix/revert-<slug>` and open a PR, so it stops happening soon, then fix it properly and bring the work back. Revert first, or go straight to the fix?

On a yes:

```bash
git worktree add -b fix/revert-<slug> .claude/worktrees/revert-<slug> origin/<mainBranch>
git -C .claude/worktrees/revert-<slug> revert <sha>
```

Add `-m 1` to the revert if `<sha>` is a merge. In that worktree, run the round gate as `fix-revert-<slug>`, confirm the bug is gone with the step 2 repro, push the branch and open a PR. Then go on to step 4.

Never revert without the owner's yes. Never `git reset --hard` and never force-push. A revert is a new commit on a branch, and it arrives by PR.

## 4. Fix

1. **Branch, in its own worktree.** The worktree starts from HEAD, so uncommitted changes in this checkout won't be in it. If there are any, ask first.

   ```bash
   git worktree add -b fix/<slug> .claude/worktrees/fix-<slug> HEAD
   ```

   The rest of this step and all of step 5 happen in that worktree. A check for the ticket's `eval.md` goes there too, so it lands with the fix.
2. **Failing test first.** Add a test that catches this bug, in the project's test style, next to the tests that cover the area. Run it and confirm it fails for the reason in the report. A test that passes before the fix doesn't test the bug. If the bug truly can't be a test (a real third-party account, a timing you can't force), say why in `notes.md` and use a repeatable command instead.
3. **Add the bug to the ticket's checks.** If a ticket covers it, append a new check at the very end of its `eval.md`, with the next free id, usually `F`:

   ```markdown
   ### F-07 <The bug, in one line>
   - **Added by fix-a-bug**: "<the report verbatim>", <YYYY-MM-DD>
   - **Covers**: REQ-n
   - **Method**: test <TestName>
   - **Steps**:
     1. <Exact step.>
   - **Expect**: <An observable result, quoted exactly, on this one line.>
   ```

   Only add, and only at the end: the gate refuses any change to the approved part. Curly braces in `Steps` or `Expect` read as an unfilled placeholder, so reword them. Weakening a check to make it pass is never the fix.
4. **Fix the cause**, not the symptom, at every site from step 3. Follow the surrounding code. The fix may not weaken a `CLAUDE.md` priority. If the obvious fix would, find another or stop and ask.
5. **Commit** the test and fix together: `fix: <what was broken, for whom>`. The body names the root cause `file:line` and the commit that introduced it. Stage named files, never `git add -A`.

## 5. Verify

1. **Round gate.** It names an exact tree, so commit first. It must pass.

   ```bash
   node .claude/hooks/gate.mjs run fix-<slug> round
   ```

2. **Blind re-check.** Start one fresh `general-purpose` agent with only this brief. It gets no root cause and no description of the fix.

   > Someone reported this bug: "<the report verbatim>".
   > Steps that reproduced it: <your step 2.2 steps>. <How to reach the tool: the command, or the local URL and account.>
   > Save what you see under `<BUG>/recheck/`. Don't read or change code. Don't stop or restart anything.
   > Try the steps twice, then two or three nearby variations of your own. Report: does the bug still happen (yes or no), what you saw at each step, quoted exactly, and anything else that looked broken on the way.

   If it still happens, go back to step 3 with what it found. Anything else it found is a separate item for the report, not part of this fix.

## 6. Clean up, always

Do this after every verdict, and after a failure too.

- Stop anything you started the way you started it: `TaskStop` on a background server. Then run the port check from step 2.1. If it's still busy, tell the owner. Don't kill by port.
- In a browser: `agent-browser --session fix-<slug> close`, then `<app.testData.restore, with {backup} = $BACKUP>`. Confirm the developer's data is back.
- Remove any bisect worktree that's left: `git worktree list` shows them.

## 7. Land

```bash
node .claude/hooks/gate.mjs risky <the checked-out branch> fix/<slug>
```

It lists the files on High-risk paths and exits 1 if there are any.

- **Exit 0 (Low and Medium paths).** It may land with no ticket. Start the `lander` agent on its fix path. Give it the branch `fix/<slug>`, the worktree's absolute path and the slug `<slug>`. Don't merge it yourself. It replies `LANDED`, `NEEDS TICKET`, `CONFLICT`, `FAILED`, `MOVED` or `REFUSED`. Pass that on as it is.
- **Exit 1 (a High-risk path).** Stop. Show the owner the list and your evidence. It goes through a ticket with `/write-tickets`. Leave the branch and the failing test for that ticket to pick up.

If anything in step 5 failed, leave it on the branch and say what's left. Don't push from here.

If a review should have caught this, propose one line for `REVIEW.md`, "Rules learned over time". Don't add it without the owner's yes.

## 8. Report

```markdown
## Bug: <one line> - <Confirmed | Not reproduced | Works as specified | Known limit | Spec silent> (<P0-P3>)

**Reported:** "<verbatim>"
**Expected:** "<requirement or rule, verbatim>" - `<ticketsDir>/<t>/rd.md` REQ-n
**Actual:** <what you saw: exact text, exit code or status> - `<evidence path>`
**Repro:** <numbered steps someone with no context can follow>. Seen 2 of 2.

**Root cause:** `<file:line>` - <one or two lines>. Proven by <what flipped it>.
**Same cause elsewhere:** <sites fixed, or none found, and how you searched>
**Introduced by:** `<commit>` in <branch>, <date> - <what that change was trying to do>
**Revert:** PR <link> | offered, declined | not needed (<severity>)
**Fix:** <what changed and where>; branch `fix/<slug>`
**Guard:** test "<name>" failed before the fix and passes after. Check <id> added to `<t>` | no ticket covers it.

**Gates:** round <pass or fail> | gate risky <none, or the files> | blind re-check: <gone or still there>
**Landing:** handed to the lander | waiting on the owner because of a High-risk path | left on the branch because ...
**Also found:** <other things noticed, with severity, not fixed>
**Not checked:** <anything you couldn't run, and why>
```

For any verdict other than Confirmed, keep the first four lines, list every attempt from `notes.md` with its evidence, and say what you need to go further.

## Rules

- No code changes before a Confirmed verdict, and none for any other verdict.
- Never edit `rd.md`. Never change an approved check. Never weaken a check to match the code.
- One bug per run. Other problems go under "Also found". Offer to run this skill on them next.
- Queries against any database are read only. Test emails use `example.com`. Nothing real gets sent during a reproduction.
- Report what happened, including the attempts that failed. "Fixed" means the test and the blind re-check both say so.
