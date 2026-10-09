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

## Explore: find the code that answers a question

```
tq decide explore "where does the breaker trip after a jev failure" [--path DIR] [--top 5] [--candidates 30] [--json] [--no-jev]
```

The goal is to have an agent read 5 ranges instead of 50 files. It runs in two stages:

1. **Keyword pre-filter (local, deterministic).** It takes up to 8 terms from the question:
   - identifiers and words of 3+ letters, minus stopwords;
   - camelCase and snake_case names also contribute their parts.

   It walks the tree in Go (`internal/ignore`). The matcher errs toward exclusion: what it lets through may be sent to Jev, so anything git would ignore is never read.
   - **Sources, all read as text:**
     - every `.gitignore`, `.ignore` and `.rgignore`, each applying below its own directory, including those in ancestors of `--path` up to the repo top;
     - `.git/info/exclude`, resolved through linked worktrees;
     - every global excludes file git could use. That is `core.excludesFile` from `GIT_CONFIG_GLOBAL`, `~/.gitconfig`, `$XDG_CONFIG_HOME/git/config`, the system config and the repo config, following `[include]` and every `[includeIf]` regardless of its condition, plus `~/.config/git/ignore`. Where git picks one source, explore takes the union.
   - **Syntax:**
     - `*`, `?` and `[...]` classes with ranges and `!`/`^`;
     - `**` in leading, middle and trailing position;
     - leading and middle `/` anchoring, and trailing `/` for directories only;
     - `\#` and `\!` escapes, and trailing spaces.
   - **Matching is case-insensitive.**
   - **Negations (`!pattern`) are dropped.** They can only un-ignore, so dropping them excludes more.
   - **Fail closed.** A pattern the parser is not sure about (a POSIX class, an unterminated class, a trailing backslash…) makes explore skip the whole subtree that file governs. For a repo-wide or global source, that is the whole walk. The output ends with `note: skipped N subtree(s)…`, which gives a count, never a path.
   - **Tested against git.** A differential test builds a repo with a rich ignore set and asserts that no file `git check-ignore` reports as ignored is ever yielded.

   It also skips `.git`, `node_modules`, `vendor`, `dist` and binaries. An always-deny list applies whatever the ignore files say:
   - `.env*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks`, `*.kdbx` and `id_*`;
   - any name containing `credentials` or `secret`, and `*.tfstate*`;
   - `.npmrc`, `.pypirc`, `.netrc`, `.git-credentials` and `.pgpass`;
   - dumps: `*.sqlite`, `*.db`, `*.dump`, `*.sql.gz` and `*.bak`;
   - anything under `.aws/`, `.ssh/` or `.gnupg/`;
   - lockfiles.

   It deliberately runs no `git` process. `git grep` in a cloned repo reads that repo's `.git/config`, and `core.fsmonitor`, `core.pager`, `diff.external` or a textconv driver there can run any program.

   It never follows a link. Symlinks, Windows junctions and other reparse points are skipped in the walk. Before any file is read, every path component is checked to be a plain directory, the file must be a regular file, and its resolved path must stay inside the resolved `--path`. So a link can never pull in `~/.ssh/id_rsa` or another client's repo.

   Each hit becomes a ~40-line window. Overlapping windows in one file merge, up to 60 lines. Spans are ranked by distinct terms, then hit count, with a bonus for a term in the file name and a small penalty for test files. The best `--candidates` are kept.
2. **Jev re-rank.** For each span it asks one noul question: "Is the code span data.spans.sN relevant to answering this question: …?"
   - All questions go in one call. The call is split only when the spans would exceed the ~80 KB request cap, with at most 3 requests in parallel.
   - The span text is clipped to 4 KB, then redacted and framed like every other Jev state.

The output is the top `--top` ranges as `score  path:start-end  [terms]`. The header says which ranking you got:

- `jev ranked N candidate spans (k request(s))`: the scores are Jev's relevance probabilities.
- `keyword ranking … (jev unavailable: …)`: the scores are keyword scores normalized to [0, 1]. You get this fallback when:
  - the backend is off, there is no key, or the directory is not in a trusted workspace;
  - any request fails, or any answer is missing;
  - you pass `--no-jev`.

  A partial set of Jev answers is never mixed with keyword scores.

Jev only re-orders what the keyword stage found. It never adds a file. `.env*` files, private keys (`.pem`, `.key`, `id_rsa*`…) and lockfiles are never read, so their content can never reach Jev. The policy comes from the workspace holding `--path`. Explore is a ranking, not a gate, so it runs in shadow and enforce mode alike. Calls appear in `log.jsonl` with `purpose: explore`.

The plugin's `explore` skill (`/tentaqles:explore`) tells agents to run this first and then read only the printed ranges with `offset`/`limit`.

## Memory gates (shadow)

The plugin decides what memory keeps and what it surfaces in two places. Each can ask Jev the same question and log the answer. Neither changes what the plugin does.

| gate | where | plugin decision today | Jev question (noul) | logged as |
|---|---|---|---|---|
| capture | `knowledge-capture.py` (PostToolUse) | a regex (`decided to`, `root cause`, …) matches, and the files mentioned are touched | does this text record a decision, root cause, workaround or discovery worth remembering? | `kind: memory-capture`; `would.worth` = keep/drop; `p.worth`; `extra` = tool, number of paths |
| recall | `session-preamble.py` (SessionStart) | the top 5 semantic facts by strength and the 3 newest decisions are printed | per candidate (up to 15 facts and 10 decisions): is this useful context for the next session, given the last session summary and hot files? | `kind: memory-recall`; `p` per item, keyed by its current rank (`f0`…, `d0`…); `extra.facts_overlap` = how many of today's top 5 Jev would also pick; `would.facts` = same/reorder |

How they run:

- **Never on the hook's critical path.** The hook does only local work: the regex, and a few SQLite reads for recall. It then hands a payload to `scripts/jev-memory-gate.py` through `_detach.spawn_detached` and returns.
  - The detached worker runs `tq decide ask --json` with a 5 s request deadline and an 8 s process timeout, then `tq decide log`.
  - If the spawn fails, the gate is skipped. It never runs inline.
- **Only where Jev is on.** The hook checks the manifest's `decision.backend: typesafe` first, so other workspaces never start a worker. tq still enforces trust, policy and the key.
  - "Backend off", "not trusted" and "no key" are not logged.
  - Other errors are logged as `error`, with no decision.
  - The recall gate is skipped when every candidate is already printed, since nothing could be re-ranked.
- **Opt out** with `TENTAQLES_JEV_MEMORY=0` (also `false`, `no`, `off`). The gates also stay off inside the plugin's own headless `claude -p` child.
- **No content on command lines or in logs.**
  - The text goes to tq in a temp state file that is deleted after the call. The plugin redacts it, and tq redacts it again.
  - `tq decide log` accepts only short plain tokens as keys and values (ids, actions, counts) and refuses anything that looks like text.

```
tq decide log --kind memory-capture --p worth=0.82 --would worth=keep --extra tool=Bash
```

Both gates are shadow-only by design: there is no enforce switch yet. Before Jev may drop a capture or reorder the preamble:

1. Label a sample of `memory-capture` lines (was the regex hit worth keeping?).
2. Run them through `tq decide eval`.
3. Check that recall overlap is stable across a few weeks of sessions.

Even then, the gates may only filter or re-rank what the deterministic code produced, and on any error they fall back to it.
