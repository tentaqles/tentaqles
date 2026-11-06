# Taking the build loop to another project

Everything specific to a project lives in `.claude/project.json` and `REVIEW.md`. The rest copies as it is. [README.md](README.md) is the contract.

## Before you start

- Node 20.11 or newer.
- git, and bash. On Windows that's Git Bash.
- agent-browser, for browser projects only: `npm i -g agent-browser`, then `agent-browser install`.

## Steps

1. **Copy `.claude/`** into the new project, without `worktrees/`. Also leave out `.job/` if there is one. If the project already has a `.claude/settings.json`, merge it by hand: the `worktree.baseRef` setting and the `.env` deny rules have to survive.
2. **Add to `.gitignore`:** `.job/` and `.claude/worktrees/`.
3. **Reset the profile.** Delete `.claude/project.json`, or set `"confirmed": false` in it. `.claude/project.template.json` shows every field.
4. **Replace this repo's own files.** `.claude/gates/round.sh` belongs to the repo the pack came from. It runs that repo's build and tests. Delete it, or rewrite it for your stack and point `gates.round` at it. Don't trust the copy.
5. **Fill the profile.** Run `/write-tickets`. It interviews you and writes `project.json`: stack, capabilities, gates, high-risk paths and the app commands. You confirm it.
6. **Set up the blind test.** The guard allows a `blind.commands` entry exactly as written, or its bare name, and no other path. A bare name comes from PATH, which may be an old installed copy. A path entry, like `./bin/tool`, makes sure it's your build. Leave `.exe` off. Set `app.build` so `/first-time-user` can build it. In `blind.read`, list only what a stranger would have, as paths from the repo root.
7. **Set up the review.** Copy `.claude/REVIEW.template.md` to `REVIEW.md` at the repo root and fill it in. Keep it under 6 KB, since it loads in every session.
8. **Load it.** Add a line `@REVIEW.md` to `CLAUDE.md`. `/code-review` reads `CLAUDE.md`, not `REVIEW.md`.
9. **Commit everything to the branch before starting.** Workers branch from HEAD, so anything uncommitted doesn't exist for them. That includes `.claude/` itself.
10. **Trust the folder once.** Start `claude` there and accept the prompt. Agent hooks are silently skipped in an untrusted folder, with no error. `gate check` then refuses a ticket because its builder hook never ran, and a first-time-user agent replies `GUARD OFF`.
11. **Check it.**

    ```bash
    node .claude/hooks/gate.mjs doctor
    ```

    Every line should say `ok`. Then start a new session: project agents load when a session starts.

## Teams (optional)

Teams are off by default. To turn them on:

1. Set `capabilities.teams` to `true` in `.claude/project.json`.
2. Set the environment variable `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`.
3. Launch `claude --agent router-teams --name router` through your normal launcher, in place of `router`.

## Untested

These parts have never had a real run:

- **The browser path:** `capabilities.browser`, `agent-browser` in `/first-time-user`, `/fix-a-bug` and the verifier, the throwaway data setup and restore, and the guard's browser mode against a live app. The guard has unit tests only.
- **Teams:** `router-teams` and `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`.
- **macOS and Linux.** The pack has only run on Windows. The hooks and the gate are written to be portable, but nobody has checked.

Expect to fix things the first time you use one of these. Treat the first run as a test of the pack.

## What it depends on

The facts about Claude Code this design relies on are in [README.md](README.md), with the version they were checked on. On a much newer Claude Code, re-check them before trusting a long run.
