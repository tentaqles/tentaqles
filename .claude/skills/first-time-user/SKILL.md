---
name: first-time-user
description: Send a blind agent through the product as a stranger, and report every place a new user gets stuck, confused or slowed down, with suggested fixes. In a browser project it uses the running app on throwaway data. In a CLI project it gets only the tool's help and a short list of docs. Use when asked for a first-time user test, a fresh-eyes or cold walkthrough, onboarding friction, "where would a new user get stuck", or "/first-time-user [feature or goal]".
---

# First-time user test

You already know how this works, so you'll miss what confuses someone who doesn't. This skill sends an agent that never saw it being built to use it like a stranger.

You are the orchestrator. You prepare the product, hand a blind agent almost nothing, and turn what it reports into a list of stuck points with fixes. You know the product. The agent must not, so everything you know stays out of its brief.

**Who starts it.** A normal session runs every step here. The router can start it too, but its Bash is read-only and it has no Write, so it can't build, start a server, touch data or save a report. If you're the router, in a browser project, or a CLI project with no fresh build, ask the owner to run `/first-time-user` in a normal session from the checkout to judge. In a CLI project whose build is ready, do steps 1b.2 and 2 only, then pass the reports to the owner, with the copy tested, and the human items below.

```
preflight -> build (and throwaway data) -> blind agent(s) -> clean up -> report and suggested fixes
```

## Two modes

Read `.claude/project.json`. The mode comes from the profile, not from you.

| | Browser | CLI |
|---|---|---|
| When | `capabilities.browser` is true | `capabilities.browser` is false |
| The agent gets | a local URL, on `blind.port` | the commands in `blind.commands`, and the files in `blind.read` |
| It can do | use the app like a visitor | read `--help` output and those files |
| You prepare | a build, running on throwaway data | a build, so the commands exist |

## What keeps the agent blind

- `.claude/agents/first-time-user.md` has `tools: Bash, Read` and `omitClaudeMd: true`, so it doesn't load `CLAUDE.md` or `REVIEW.md`.
- Its hook, `.claude/hooks/blind-guard.mjs`, allows only what the mode allows and denies everything else. It fails closed.
- Agent hooks only run in a trusted folder, and they're skipped silently otherwise. The agent's first command is a canary that the guard must deny. If its reply is `GUARD OFF`, the run is void. Stop and tell the owner to trust the folder and run `node .claude/hooks/gate.mjs doctor`.
- The one leak left is you. The brief below is all it learns. Don't add what the product does, routes, logins, copy, specs or hints, even when a run goes badly.

## 0. Preflight

1. **What to test.** `$ARGUMENTS` may name a feature, give goals in the user's own words, or be empty.
   - **Empty.** One agent, no goal. Working out what it's for is part of the test.
   - **Goals**, one per line or separated by `;`. One agent per goal, passed verbatim. Never write a goal yourself.
   - **A feature or ticket name.** Read its `rd.md`. Ask the owner, in one question, how a real person would put the goal, offering a plain-words default with no product names and no labels from the UI. Use what they confirm, verbatim. If you can't ask, run with no goal and say so.
2. **Run folder.** `RUN=.job/first-time-user/<YYYY-MM-DD-HHMM>`, one subfolder per agent. The guard only accepts screenshots under `.job/first-time-user/`.

   ```bash
   mkdir -p "$RUN/v1"
   git rev-parse HEAD
   ```

   Note the commit. The report names what was tested. Run from the checkout you want judged: for a ticket, that's its worktree.

The shell blocks are bash, which is Git Bash on Windows.

## 1a. Prepare: browser mode

The test gets its own data. The developer's data must come back exactly as it was, whatever happens.

1. **agent-browser.** Check `agent-browser --version`. If it's missing, ask before installing anything global. If the owner would rather not, use `npx -y agent-browser` as the command. Call it `AB` below.
2. **Port.** Use `blind.port`. The guard opens no other port, so you can't pick another one. Check it's free:

   ```bash
   node -e "require('net').connect(<port>,'127.0.0.1').on('connect',()=>{console.log('busy');process.exit(1)}).on('error',()=>console.log('free'))"
   ```

   If it's busy, stop and ask. Don't stop what you didn't start.
3. **Data and build.** If `app.testData` or `app.start` is empty, ask before guessing.

   ```bash
   BACKUP=.job/backups/ftu-$(date +%s)
   <app.testData.setup, with {backup} = $BACKUP>
   <app.build>
   ```

   If the build fails, restore the data and show the owner. An empty app tests nothing but its empty states, so use seed data if the project has it. Never hand the agent the seeded logins.
4. **Start.** Run `<app.start, with {port}> > $RUN/server.log 2>&1` with `app.env`, using `run_in_background`. Poll `curl -s -o /dev/null -w '%{http_code}' http://localhost:<port><app.readyPath>` until it answers 200. Give up after about 60 s, show the log, and go to step 3.

## 1b. Prepare: CLI mode

1. **Build.** Run `<app.build>` so the commands in `blind.commands` exist. If `app.build` is empty, ask the owner how to build it, and don't guess. If the build fails, show the owner and stop.
2. **Make sure the agent will run your build.** The guard allows an entry of `blind.commands` exactly as written, or the bare name of one. It denies any other path. So the exact command path you pass is the entry itself.
   - **An entry with a slash**, like `./bin/tool`, is a path. Build to that path. Leave `.exe` off the entry: the guard strips it from what's typed, and bash on Windows finds the `.exe` by itself.
   - **A bare entry**, like `tool`, comes from PATH, which may be an older installed copy. Run `command -v <entry>` and `<entry> --version`. If that isn't your build, stop and tell the owner: put a path in `blind.commands`, or accept testing the installed copy. Write in the report which copy was tested.
3. **Files.** Turn each `blind.read` entry into an absolute path, since the Read tool needs one. Check each exists. A missing file is a finding, not something to fix before the run.

### What CLI mode can't test

Say this plainly in every CLI report. The agent reads help text and docs. It never runs the tool for real. So it can tell you whether the words make sense and whether a stranger knows what to do next. It can't tell you:

- whether the commands work, or whether the docs match what they do
- anything that needs real setup: an install, an account, a login, a credential, a second machine
- anything that needs a real terminal: prompts, wizards, colour, shell integration, a new shell session
- what a real error message looks like, since it can't cause one

Each of these goes to the owner as a `human` item, with exact steps to try. For a ticket, offer to append it as a check with `Method: human` at the end of `eval.md`. The owner passes it with `gate human`, and it blocks a release, not a landing.

## 2. Brief the agents

One `Agent` call per agent, `subagent_type: "first-time-user"`, all in one message so they run in parallel. Send the brief exactly, filling only the angle-bracket fields.

Browser mode:

> Mode: browser
> Here's a link someone sent you: http://localhost:<port><start path>
> Browser command (this is `AB`): `<AB>`
> Your session name: `ftu-<n>` (use `ftu-<n>-2`, `ftu-<n>-3` to arrive again as a new visitor)
> Save your screenshots under: `<RUN>/v<n>/`
> Goal: <the goal verbatim, or: "None given. Work out what this site is for and try to use it.">
> Follow your instructions and finish with your report.

CLI mode:

> Mode: cli
> Someone told you to try this command-line tool: `<the blind.commands entry, exactly>`
> You may read these files: <absolute paths from blind.read>
> Goal: <the goal verbatim, or: "None given. Work out what this tool is for and how you'd start using it.">
> Follow your instructions and finish with your report.

Wait for them to finish. Don't send a follow-up with hints. If an agent comes back early because it was stuck, that is the finding.

## 3. Clean up, always

Do this after failures too.

**Browser mode:**

- `AB close` for each session the agents named.
- Stop the server the way you started it: `TaskStop` on the background task. Then run the port check. If it's still busy, tell the owner. Don't kill by port.
- `<app.testData.restore, with {backup} = $BACKUP>`. Confirm the developer's data is back before reporting.

**CLI mode:** nothing was started. Leave the build in place if it's gitignored. Remove it if it isn't.

## 4. Report and suggest fixes

All of it goes under `.job/first-time-user/<run>/`.

1. Keep each agent's report as written. Don't soften, merge away or re-rank its findings. Save them as `$RUN/v<n>/report.md`.
2. Add what only you can see, in a separate section marked as yours: the commit tested, which copy of the command ran, server errors from `$RUN/server.log` matched to a step where you can, and any finding whose screenshot is missing.
3. **Suggest fixes.** Now you can read the code, and the agent couldn't. For each stuck point, find the page or help text behind it and write one fix: what to change, where (`file:line`), and whether it changes behaviour a ticket describes. If it does, it needs `/write-tickets`, not a quick edit.
4. Write `$RUN/report.md`: every stuck point across agents in one table, ordered Blocked, Slowed, Confused, with the agent, where, a one-line description, evidence and suggested fix. Then the first impressions side by side. Then your notes. In CLI mode, end with "Human items": what this mode can't test, with exact steps.
5. Reply with the table, the first impressions, the human items and the path to `$RUN/report.md`. Offer the fixes, don't start them: small copy fixes as a low-risk change, bugs through `/fix-a-bug`, behaviour changes through `/write-tickets`.

## Rules

- A guard denial in an agent's transcript is expected, not a bug. If an agent "couldn't do X because a tool was blocked" and X is something a real browser user could do, fix the guard's allowlist and tell the owner.
- Never edit the guard to let the agent read more files, run JS, run the tool for real or open non-app URLs. Never add to `blind.read` to get a run past a stuck point. Being stuck is the result.
- Never run this against production, or any URL other than the local server you started.
- Everything the agent denied or couldn't reach goes in the report. Don't fill the gap from what you know.
