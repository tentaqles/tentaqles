---
name: babysit-pr
description: Watch a pull request until CI is green and every review comment is addressed — bounded check watching, failed-job log diagnosis (gh run view --log-failed), fix and push, reply to or resolve review threads, with an iteration cap and clear stop conditions. Use after the ship skill opens a PR, or when the user says "babysit this PR", "watch CI", "get this PR green", "fix the failing checks", "address the review comments", "why is CI red", or "/babysit-pr". Never merges with --admin, never force-pushes the default branch, never disables checks.
---

# Babysit PR

Loop on one pull request until it is **green with no unresolved review
comments**, or until something needs the user. Pairs with `ship`, which opens
the PR and hands it here.

For CI that takes a long time, run this skill under `/loop` (for example
`/loop 10m /tentaqles:babysit-pr 123`) so each pass is short and nothing
busy-waits in one turn.

## Setup

Use the PR number or URL the user (or `ship`) gave; otherwise the PR for the
current branch:

```bash
gh pr view ${PR:-} --json number,url,headRefName,baseRefName,headRefOid,state,isDraft,mergeable,reviewDecision
```

Stop if the PR is merged or closed. Make sure the local branch matches the PR
head before fixing anything:

```bash
gh pr checkout <number> && git pull --ff-only
```

(`gh pr checkout` fetches the branch when it isn't local and handles PRs from
forks.)

Set a cap: **at most 5 fix iterations** (a pushed fix counts as one) unless
the user asked for a different number. Keep a short log of each iteration:
what failed, the cause, the commit that fixed it.

## Each iteration

### 1. Watch checks (bounded)

Never `sleep` in a loop. A foreground Bash call is capped at 10 minutes, so
keep the watch under that, or run it with `run_in_background` (or the
`Monitor` tool) for longer CI:

```bash
timeout 540 gh pr checks <number> --watch --fail-fast --interval 30; echo "checks_exit=$?"
```

| Exit | Meaning | Next |
|---|---|---|
| 0 | all checks passed | go to step 3 (review comments) |
| 1 | a check failed, **or the PR has no checks** | tell them apart (below), then step 2 |
| 124 | `timeout` hit: still pending | report state and end this pass (resume later or under `/loop`) |
| 8 | pending (only from a non-`--watch` call) | same as 124 |

On exit 1, first count the checks:

```bash
gh pr checks <number> --json name -q length
```

`0` means no CI is configured: say so and skip to step 3. "No CI configured"
is not "green".

No `timeout` binary (stock macOS)? Use `gtimeout` from coreutils, or run the
watch with `run_in_background`.

### 2. Diagnose a failure

Find the failed run **for the current head commit**, not an older one:

```bash
gh run list --branch <headRefName> --commit <headRefOid> --json databaseId,name,status,conclusion,workflowName
gh run view <run-id> --log-failed 2>&1 | tail -n 200
```

Read the log and name the cause before touching code:

- **Real failure in this PR's change** (test, lint, type error, build): fix
  it locally, re-run the same command locally, commit (conventional message,
  explicit paths), `git push`. Next iteration.
- **Failure unrelated to the diff, looks flaky** (network timeout, runner
  died, rate limit, a test that passes locally and touches nothing you
  changed): re-run the failed jobs **once** with `gh run rerun <run-id> --failed`.
  If it fails the same way again, stop and ask the user.
- **Infra or config the PR can't fix** (expired CI secret, quota, missing
  permission, broken runner image): stop and ask the user. Do not work around
  it.

If a fix needs secrets locally, use `tq dotenv run -- <cmd>`; never read or
print a `.env`.

### 3. Review comments

```bash
gh pr view <number> --comments
gh api graphql -f query='
query($owner:String!,$repo:String!,$num:Int!){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$num){
      reviewThreads(first:100){
        nodes{ id isResolved isOutdated path line
          comments(first:20){ nodes{ databaseId author{login} body } } }
      }
    }
  }
}' -F owner='{owner}' -F repo='{repo}' -F num=<number>
```

(`gh api` fills `{owner}` and `{repo}` from the current repository.)

For every **unresolved** thread, decide:

- **Valid and in scope:** fix it, push, then reply with what changed and the
  commit SHA:
  `gh api repos/{owner}/{repo}/pulls/<number>/comments/<first-comment-databaseId>/replies -f body="Fixed in <sha>: …"`
  (use the `databaseId` of the thread's **first** comment, `comments.nodes[0]`;
  replying to a reply's id returns 404)
- **Disagree, or out of scope:** reply with the reason and leave the thread
  open for the reviewer. Don't silently ignore it.
- **Needs a product or design decision:** stop and ask the user.

Resolve a thread only after you fixed what it asked for:

```bash
gh api graphql -f query='mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{isResolved}}}' -F id=<thread-id>
```

Top-level review comments and bot comments (linters, coverage) get the same
treatment: act on them or answer them.

Any push restarts CI, so go back to step 1.

## Stop conditions

Stop and report when the first of these holds:

1. **Done:** checks exited 0 on the current head **and** no unresolved review
   threads remain. Quote the checks output as the proof.
2. **Cap reached:** the iteration limit is used up. Report what is still red
   and your best diagnosis.
3. **Needs the user:** flaky or broken infra, a missing secret or permission,
   a product decision, a reviewer disagreement, or a fix that would grow
   beyond the PR's scope.
4. **Still pending:** CI hasn't finished within the watch timeout. Report and
   let `/loop` or the user resume.

Report format:

```
PR <url>: <green | red | pending | needs you>
Checks: <passed/failed/pending counts, quoted from gh pr checks>
Review: <N threads resolved, M answered and left open, K waiting on the user>
Iterations: <n>/<cap> — <one line per fix: cause → commit>
Next: <what the user needs to do, if anything>
```

Only say "green" when the `gh pr checks` output you just read shows every
check passed.

## Merging

Not part of this skill. If the user explicitly asks to merge once green, use
the normal path (`gh pr merge --squash`, or `--auto` to merge when the
required checks pass) and respect branch protection.

## Never

- `gh pr merge --admin` or any other bypass of branch protection
- force-push the default branch; on the PR branch, add commits instead of
  rewriting history unless the user asks
- disable, skip or weaken checks to get green: no `[skip ci]`, no
  `continue-on-error`, no deleting or `skip`-marking tests, no lowering
  coverage thresholds, no editing workflow files to drop a failing job
- claim green without the checks output in hand
- print or rotate secrets
