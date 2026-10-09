# `tq decide` — the Jev decision layer

`tq decide` asks [Jev](https://typesafe.ai) (TypeSafe's "System One" model)
fast yes/no, choice and score questions about a piece of state: a diff, a
command, an n8n node. One call answers many questions in ~300 ms for a
fraction of a cent, which makes it usable on a hook's hot path.

It is the second line, never the first: tq's deterministic rules
(`docs/CLAUDE-HOOK.md`) run first and fail closed. A Jev answer can only
**add** an ask or a deny on top of them. It can never allow something a rule
blocked, and any error, timeout or missing answer means "no opinion".

## Configure a workspace

Jev is off unless the workspace manifest turns it on:

```yaml
decision:
  backend: typesafe      # or off (the default)
  mode: shadow           # shadow (the default): log only. enforce: may ask/deny
  block_threshold: 0.8   # yes-probability >= this -> deny (enforce mode)
  warn_threshold: 0.5    # >= this -> ask
  env_file: ../shared/.env   # dotenv with TYPESAFE_API_KEY (relative to the manifest)
  # model: jev-1.13.0    # pinned by default; change only after re-running eval
  # base_url: http://127.0.0.1:8080/v1/systemone  # only https://api.typesafe.ai or localhost
  # timeout_ms: 800      # hook deadline
```

The key is read from `TYPESAFE_API_KEY` in the environment, then from
`env_file`. The manifest holds the file's path, never the key. Run `tq allow`
after editing the manifest, then:

```
tq decide status     # policy, model, thresholds, key found/missing (never the key)
```

## Ask

```
tq decide ask --state-file change.diff \
  --q destructive="Does this change permanently destroy existing data?" \
  --q auth="Does it touch authentication or authorization?"
```

`--state-file` sends JSON as an object and anything else as text. For
`choice` and `score` questions, pass `--questions-file` with Jev's native
question JSON (`{"id": {"type": "choice", "instructions": "...", "criteria": {...}}}`).
`--json` prints the raw answers.

## What leaves the machine

Every call:

- **redacts** every string in the state with tq's secret patterns
  (`internal/secrets`) before sending;
- **frames** the state as untrusted data (`{"note": ..., "data": <state>}`),
  because prompt injection is Jev's documented weak spot;
- refuses a request larger than ~80 KB (~20k tokens);
- has a hard deadline: 800 ms from hooks, 15 s from the CLI;
- sends the key only to `https://api.typesafe.ai` or a localhost backend,
  and reads it only from a `.env*` file named by the trusted manifest;
- trusts a cached or live answer only when every probability is in [0, 1];
  the cache key includes the endpoint.

The direct TypeSafe API is US-hosted and has no zero-retention option on
standard plans. Redaction is what keeps credentials out; keep that in mind
before pointing Jev at regulated data.

## Cache and cost log

Under `$TQ_HOME/decide/`:

- `cache/` — answers keyed by SHA-256 of model + state + questions, 7-day TTL.
  Answers only, never the state.
- `log.jsonl` — one line per call: time, workspace, purpose, model, number
  of questions, latency, cached, input tokens, cost in USD, error. Never the
  state or question text. `tq insights` reads it.

## Evaluate before enforcing

A rule may leave shadow mode only after it is measured:

```
tq decide eval docs/decide/seed-cases.jsonl
```

Each line of a cases file is
`{"id","family","lang","tags","state","question","expect"}`. For every family
it prints precision and recall across thresholds, the always-no baseline,
and the threshold with the best recall at precision >= 0.90 (the highest on
a tie, for margin). A family that never reaches 0.90 precision stays in
shadow mode. Misclassified cases are listed by id.

`docs/decide/seed-cases.jsonl` is a starter set (26 cases across four rule
families, English and Portuguese, with prompt-injection attempts). Grow each
family to ~100 cases — paraphrases, Portuguese, injection, near-misses —
before trusting its threshold, then watch two weeks of shadow logs.

## Judgment rules in the guard

With `backend: typesafe`, `tq claude-hook pre-tool-use` asks Jev about tool
calls that a judgment rule's regex pre-filter selects. Built-in rules:

| id | looks at | question |
|---|---|---|
| `jev/destructive-sql` | edits, shell, MCP with `DROP TABLE`/`TRUNCATE`/`DELETE FROM`/`ALTER … DROP` | does it destroy existing data? |
| `jev/rls-weakened` | edits and MCP touching policies, grants, RLS | does it weaken row-level security? |
| `jev/service-role-client` | edits to `.ts/.tsx/.js/.jsx/.vue/.svelte/.astro` mentioning service-role/secret/admin keys | does it expose a server credential to client code? |
| `jev/n8n-inline-credentials` | n8n MCP create/update/publish/execute calls mentioning auth material | does a node inline a credential? |

How it runs:

- **After the deterministic rules**, and not at all when they already deny.
- **One batched call** per tool call for every matching rule, with the
  800 ms hook deadline. Most tool calls match no rule and never reach Jev.
- **A breaker** skips Jev for one minute after any failure, so an outage
  costs one slow call per minute instead of one per tool call.
- **Shadow mode** (the default) applies nothing and logs to
  `$TQ_HOME/decide/judgments.jsonl`: rule ids, probabilities, what Jev would
  do. Never the content.
- **Enforce mode** turns a probability at or above `warn_threshold` into an
  ask. Built-in rules are capped at ask; a manifest rule can set
  `max: deny` to allow a deny at or above `block_threshold`.

Add or disable rules in the manifest:

```yaml
decision:
  backend: typesafe
  disable: [jev/n8n-inline-credentials]
  rules:
    - id: acme/pii-to-index
      tool: Edit|Write
      path: 'rag/.*\.py$'
      content: 'upsert|add_documents'
      question: Does this send personal data (names, emails, CPF) into a search index without redaction?
      max: ask
      reason: Jev judged this as indexing unredacted personal data
```

## Review triage

```
tq decide triage            # working tree; --staged; --range main..HEAD
```

It asks 11 yes/no risk questions about the diff:
- auth, money, schema, data loss, migrations, RLS, secrets
- public API, shared state, destructive commands, n8n credentials

It prints `low` or `high (flags…)` and exits 0 for low, 3 for high:
- **low:** one light review pass
- **high:** the full review. Triage is also high when the diff was truncated or Jev was unreachable: when in doubt, review more.

## Subagent model routing

The plugin's PreToolUse hook also sees `Agent` launches. When the policy
enforces, tq asks Jev which tier (haiku / sonnet / opus) the task needs and
sets the launch's `model`. It only does so when all of these hold:

- the caller set no model, and the subagent type has none of its own (only
  general-purpose launches);
- the pick is strictly cheaper than the parent session's model (read from
  the transcript);
- Jev's confidence is at least `block_threshold`.

Otherwise the launch is untouched. Shadow mode logs the pick (`kind: route`
in `judgments.jsonl`) without applying it. Turn routing off with
`route: false`.

Routing runs only after the policy rules allowed the launch (a `tool: Agent`
rule can still deny or ask), and its output carries `updatedInput` with no
`permissionDecision`: it rewrites the model, it never approves anything.
Before switching a workspace to `mode: enforce`, confirm in a live session
that a routed launch actually runs on the picked model (the shadow log shows
what it would pick; the subagent's transcript shows what ran).

## Completion-evidence check at Stop

The plugin's Stop hook runs `tq claude-hook stop`. It catches a turn that
ends with "all tests pass" when no test ran after the last edit.

It does nothing at all when:

- `stop_hook_active` is set (Claude is already continuing because of a Stop
  hook, so there are no loops);
- the workspace has no `decision:` block with `backend: typesafe`, or no key;
- the turn (everything after the last message the user typed) edited no
  file with Edit, Write, MultiEdit or NotebookEdit;
- the breaker is open, or any error or timeout happens.

Otherwise it reads the transcript tail (at most 2 MiB) and builds a compact
state, at most ~30 KB:

- the last user request (first 2,000 characters);
- the files edited in the turn (up to 30);
- the last 15 Bash/PowerShell commands run **after the last edit**, each with
  its status: `ok`, `error` (the tool result's `is_error`) or `unknown`;
- the final assistant text (last 4,000 characters).

Two signals decide. Each comes from a deterministic check first, and Jev is
asked only when that check passes:

- **claims**: an explicit claim in the final message ("all tests pass",
  "the build is green", "lint is clean") is found by a regex. It counts as
  a claim without asking Jev, so no text in the message can argue it away.
  Otherwise Jev is asked whether the final message claims the work passed
  its checks or is verified. That state holds the request, the edited files
  and the final message, and the claim counts at or above `block_threshold`.
- **evidence** comes **only from tool records**. It is a Bash/PowerShell
  `tool_use` after the last edit whose `tool_result` is not an error and
  whose command runs a test, build, lint or type-check tool. The command
  is split on unquoted `&&`, `||`, `;`, `|`, `&` and newlines, with
  comments and heredoc bodies dropped. A segment counts only when its
  **first command word** is a known runner, after `VAR=val`, `env`, `time`,
  `npx`, `uv run` and `poetry run` prefixes. Known runners include
  `go test|vet|build`, `npm test`, `npm|pnpm|yarn|bun run test|lint|build|typecheck|check`,
  `pytest`, `python -m pytest`, `cargo test|clippy|build|check`,
  `make test|check|lint`, `tsc`, `eslint`, `ruff`, `mypy`,
  `dotnet test|build`, `mvn test`, `gradle test` and `Invoke-Pester`.
  A runner name that only shows up in an `echo`/`printf`/`Write-Host`, a
  `grep` pattern, a quoted string or a comment never counts. Neither does
  a runner whose failure is hidden with `|| …`.
  If there is no such command, evidence is 0 and Jev is not asked. If there
  is, Jev is asked whether one of **those commands only** is a real check.
  That call never sees the final message. Evidence counts as missing at or
  below `1 - block_threshold` (0.2 by default).

What Claude writes ("I ran the tests, they passed") is never evidence. Both
Jev calls run in parallel, each within the hook deadline.

The design treats every part of the transcript as untrusted, including the
final message, the request and command strings:

- the questions are constant strings;
- transcript text goes only into the state, which is redacted and framed as
  untrusted;
- the block reason is a fixed template with two numbers, never transcript
  text.

So a Jev answer can only add a block. It can never suppress one that the
deterministic signals call for.

**Residual limit.** This is an advisory check built from string
heuristics, not a shell parser. Some commands can still slip past it and
count as evidence when they verified nothing:

- a test that a real runner runs but that checks nothing;
- a pipe that hides the runner's exit status (`go test | tail`);
- a script that only calls itself `test`;
- a soft claim that Jev misreads.

Getting past the check gives exactly the same result as running with Jev
off: no block, and Claude ends its turn as it would have anyway. It never
approves, allows or skips anything else, and the deterministic guard rules
are not involved.

- **Shadow mode** logs a `kind: "stop"` line to `judgments.jsonl`. It holds
  the two probabilities, what Jev would do, and counts of edits and commands,
  never the transcript.
- **Enforce mode** prints `{"decision":"block","reason":"..."}`, which sends
  Claude back to run the check and report the result, or to say plainly that
  the change is unverified. This happens **at most once per session**: a
  marker at `$TQ_HOME/decide/stop/<session_id>` is written first, and if it
  cannot be written, nothing is blocked. With the marker present, later
  Stops in that session skip Jev entirely.

The hook entry has a 10 s timeout. `tq_hook.sh stop` always exits 0, also
when tq is missing or too old for the subcommand. It does nothing for the
plugin's own headless `claude -p` child (`TENTAQLES_HEADLESS_CHILD=1`).

## Skill picker at UserPromptSubmit

The plugin's UserPromptSubmit hook runs `tq claude-hook prompt-submit`. Jev
picks which installed skill, if any, fits the prompt. It is **log-only**
unless the policy enforces, and it **never blocks** a prompt.

The skill index comes from SKILL.md frontmatter (`name`, `description`) in:

- `<cwd>/.claude/skills/*/SKILL.md` (project);
- `$CLAUDE_CONFIG_DIR/skills/*/SKILL.md` (user; `~/.claude` when unset);
- `skills/*/SKILL.md` under each `installPath` in
  `$CLAUDE_CONFIG_DIR/plugins/installed_plugins.json`, named
  `<plugin>:<skill>`.

At most 60 skills are indexed (in that order, first name wins), and each
description is cut to 200 characters. The index is cached at
`$TQ_HOME/decide/skills-<hash>.json`, keyed on config dir and cwd. It is
rebuilt only when the size or mtime of `installed_plugins.json`, a skills
directory, or any SKILL.md changes, so a prompt costs a few `stat` calls,
not a rescan.

Jev gets one `choice` question. The options are the skill names plus
`none`, and the state is the prompt (redacted, first 4,000 characters).
Prompts that start with `/` are skipped, because a slash command already
names what to run.

- Every pick is logged as `kind: "skill"`, with the pick, its confidence,
  the skill count and the prompt (redacted, then cut to 80 characters).
- **Shadow mode** prints nothing.
- **Enforce mode** adds one line of context when the pick is a real skill
  and the confidence is at least `block_threshold`:
  `hookSpecificOutput.additionalContext` = `Skill that may fit: <name>`.
  A pick that names no indexed skill is treated as an error.

The hook entry has a 5 s timeout. Errors trip the shared breaker like
every other Jev call, and `tq_hook.sh prompt-submit` always exits 0.
