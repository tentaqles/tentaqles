# tq insights

`tq insights` measures how Claude Code is used across your tq identities and
compares the result with the 30-day targets of the workflow-upgrade plan. It
replaces the one-off session-mining scripts.

```
tq insights [--since 7d|30d|12h|2w|YYYY-MM-DD] [--ws NAME...] [--json] [--top 10] [--no-worktrees]
```

| flag | default | meaning |
|---|---|---|
| `--since` | `30d` | window start: a duration (`h`, `d`, `w`) or a UTC date |
| `--ws` | all | only these identities (repeatable or comma-separated). `default` is `~/.claude` |
| `--json` | off | print the stable JSON schema (`tq.insights/v1`) instead of tables |
| `--top` | 10 | rows in the largest-session tables |
| `--no-worktrees` | off | skip the git worktree scan (the slowest part) |

## What it reads, and what it never does

- Transcripts: `~/.tentaqles/identities/<ws>/claude/projects/**/*.jsonl`
  (one identity per directory) and `~/.claude/projects/**/*.jsonl` (reported
  as `default`, the pre-tq setup). Files whose modification time is before
  `--since` are skipped, and inside a file only records stamped at or after
  `--since` count.
- Jev logs: `$TQ_HOME/decide/log.jsonl` and `$TQ_HOME/decide/judgments.jsonl`,
  filtered by `--since` and `--ws`.
- Worktrees: the same scan as `tq worktrees list`, limited to `--ws`.

It is **read-only**. Files are streamed line by line (a line longer than
32 MiB is skipped), so transcripts of hundreds of MB never sit in memory.
Output holds **aggregates only**: counts, token totals, costs, rule ids,
session ids and project directory names. It never prints prompt text, tool
input or tool output.

## Sections

### Cost and tokens

Per identity and in total:

- `sessions`: main transcripts with activity in the window, split by class:
  - `interactive`: you typing.
  - `security_review`: a headless review session. The first prompt starts with
    "Review this change for security vulnerabilities" or "You previously
    flagged these candidate vulnerabilities". This is the same rule the
    mining scripts used.
  - `probe`: "Reply with exactly ..." checks.
  - `sdk_other`: any other `sdk-*` entrypoint.
- `subagents`: subagent transcripts (`<session>/subagents/*.jsonl`). Their
  cost counts toward the identity, and toward the parent session in the
  top-session tables.
- `cost est`: tokens times list price (see [Pricing](#pricing)). Assistant
  messages are deduplicated by message id, because Claude Code writes one
  line per content block with the same usage.
- `recorded`: Claude Code's own `cost-state.totalCostUSD`, summed over runs
  (a resumed session restarts the counter) that started inside the window.
  It is only written at some points, so treat it as a sanity check, not as
  the truth.
- Tokens are shown as input, output, cache read and cache write.
- `>400k`: main sessions whose peak context (input + cache read + cache write
  of one request) went over 400k tokens. `peak ctx` is the largest peak.
- `cost by tier`: the cost estimate split into opus, sonnet, haiku and fable.

There are also two tables of the largest sessions: one by peak context and
one by cost (subagents included). Each row shows the session id, project
directory, class, duration (first to last record) and subagent count.

### Shell health

- Bash and PowerShell call counts, plus their failure rates (`tool_result`
  with `is_error`). `all fail` is both shells combined; this is the value the
  target uses.
- `heredoc EOF`: "unexpected EOF while looking for matching" errors.
- `wt refusals`: "This agent is isolated in the worktree ..." refusals, when
  a worktree-isolated subagent's compound command is refused.
- `exit 127`: failed results containing `Exit code 127` (command or script
  not found).

### Guard

These counts come from transcripts, so they reflect what Claude actually hit:

- **denies** per rule id, parsed from failed tool results:
  - `BLOCKED [tq/...]` and `BLOCKED [jev/...]` are policy and Jev rules.
  - `BLOCKED: <message>` is the identity guard, mapped to
    `identity/<rule>` (`neutral-remote`, `git-email-drift`, `wrong-cloud`,
    `blocked-command`, ...). Anything unrecognised is `identity/other`.
- **asks** per rule id, parsed from `CONFIRM [rule]` in the PreToolUse hook's
  `permissionDecision` output.
- **Stop-hook blocks**: `hook_blocking_error` on Stop or SubagentStop.
- **Hook errors** and **hook timeouts** (cancelled hooks), by event.
- **Hook latency**: p50 and p95 per event, from `durationMs` on hook success
  and error records.

### Jev

- From `log.jsonl`: calls, cache hits and hit rate, errors, cost, p50/p95
  latency, and calls by purpose.
- From `judgments.jsonl`:
  - Per kind (`guard`, `route`, `triage`, and any future kind such as `stop`
    or `skill`): line count, errors and modes.
  - Per rule (every rule-keyed kind): evaluated, would-ask, would-deny,
    applied-ask, applied-deny, and whether the rule was seen in enforce mode.
  - Routing: parent-to-pick counts, picks cheaper than the parent, and how
    many were applied. It also shows the estimated savings if every cheaper
    pick had been applied, calculated as

    `savings = sum over cheaper picks of avg_subagent_cost x (1 - out_price(pick) / out_price(parent))`

    `avg_subagent_cost` is the mean cost of a subagent transcript in the
    window, and the output prices are Opus 5.5 $20, Sonnet 5.5 $10 and
    Haiku 5.5 $0.50 per MTok. This is a rough figure. It assumes the routed
    task would have used as many tokens on the cheaper tier, and that routed
    tasks cost what an average subagent costs.
  - Triage: low/high counts and how often each flag fired.

### Background review sessions

This is the count of `security_review` sessions, how many of them ran on Opus,
and their estimated cost.

### Worktrees

These are the linked worktrees found by `tq worktrees list`, and how many it
would prune (clean, merged, older than 3 days, or already deleted).

## 30-day targets

Counts are normalised to 30 days (`count x 30 / window days`), so any
`--since` can be compared with the baseline. The baseline comes from the
90-day mining run (2026-07-10 to 2026-10-08).

| id | goal | baseline |
|---|---|---|
| `sessions_over_400k` | no sessions over 400k context | 25 in 90 days |
| `stop_hook_blocks_per_30d` | near zero, defined as at most 3 per 30 days | 71 in 90 days |
| `exit_127_per_30d` | near zero, defined as at most 3 per 30 days | 34 `tq_run.sh` failures in 90 days |
| `shell_failure_rate` | under 5% (Bash and PowerShell combined) | 5.0% (Bash 4.2%, PowerShell 15.2%) |
| `review_sessions_per_30d` | down 80% or more, which means at most 24.9 per 30 days | 373 in 90 days (124.3 per 30 days) |
| `jev_enforce_precision` | Jev rules are enforced only at eval precision 0.9 or higher | none enforced |

Each target has a status of `met`, `not met` or `check`. Precision is not in
the logs, so when any Jev rule shows up in enforce mode,
`jev_enforce_precision` becomes `check`. Confirm each listed rule with
`tq decide eval`.

## Pricing

The table is `Prices` in `internal/insights/pricing.go`. Prices are in USD
per million tokens. The first row whose `Match` is a substring of the
transcript's `message.model` wins, so put more specific rows first.

| match | tier | input | output | cache write (5m) | cache read |
|---|---|---|---|---|---|
| `fable-5-1`, `mythos-5-1` | fable | 10 | 50 | 12.5 | 0.25 |
| `fable`, `mythos` | fable | 10 | 50 | 12.5 | 1.00 |
| `opus-5-5` | opus | 4 | 20 | 5 | 0.20 |
| `opus` (5, 4.8, 4.7, 4.6) | opus | 5 | 25 | 6.25 | 0.50 |
| `sonnet-5` (5.5, 5) | sonnet | 2 | 10 | 2.5 | 0.20 |
| `sonnet` (4.x) | sonnet | 3 | 15 | 3.75 | 0.30 |
| `haiku-5` | haiku | 0.10 | 0.50 | 0.125 | 0.01 |
| `haiku` (4.5) | haiku | 1 | 5 | 1.25 | 0.10 |

- Cache writes with a 1-hour TTL (`usage.cache_creation.ephemeral_1h_input_tokens`)
  are priced at 2x input.
- Haiku 5.5's long-prompt tier (over 100K tokens) is ignored.
- `<synthetic>` and unknown models are not costed.
- These are first-party API list prices. The result estimates usage value,
  not what a subscription is billed.

## JSON schema (`tq.insights/v1`)

The top level is:

```
schema, generated_at, since, until, window_days,
identities: [IdentityReport], total: IdentityReport,
top_by_context: [SessionRow], top_by_cost: [SessionRow],
jev: {log, kinds, rules, route, triage},
worktrees: {total, prunable} | null,
targets: [{id, goal, current, display, baseline, status}]
```

`IdentityReport` has these keys:

```
name, sessions, sessions_by_class, subagent_transcripts, cost_usd,
recorded_cost_usd, cost_by_tier, tokens{input,output,cache_read,cache_write},
peak_context_max, sessions_over_400k,
shell{bash{calls,errors}, powershell{calls,errors}, heredoc_eof,
      worktree_refusals, exit_127, bash_failure_rate,
      powershell_failure_rate, failure_rate},
guard{deny, ask, stop_blocks, hook_errors, hook_timeouts},
hook_latency{<event>: {n, p50_ms, p95_ms}},
reviews{sessions, opus, cost_usd}
```

Rates are fractions between 0 and 1. A test pins the keys, and
`SchemaVersion` is bumped on any breaking change.

## Examples

```
tq insights                          # last 30 days, all identities
tq insights --since 7d --ws dirtybird
tq insights --since 2026-10-01 --json | jq '.targets'
```
