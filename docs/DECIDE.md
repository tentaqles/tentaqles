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
