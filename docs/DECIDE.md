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
tq decide explore "where does the breaker trip after a jev failure" [--path DIR] [--top 5] [--candidates 30] [--json] [--no-jev] [--include-config]
```

The goal is to have an agent read 5 ranges instead of 50 files. It runs in two stages:

1. **Keyword pre-filter (local, deterministic).** It takes up to 8 terms from the question:
   - identifiers and words of 3+ letters, minus stopwords;
   - camelCase and snake_case names also contribute their parts.

   It walks the tree in Go and reads only files that pass every rule below. Each hit becomes a ~40-line window. Overlapping windows in one file merge, up to 60 lines. Spans are ranked by distinct terms, then hit count, with a bonus for a term in the file name and a small penalty for test files. The best `--candidates` are kept.
2. **Jev re-rank.** For each span it asks one noul question: "Is the code span data.spans.sN relevant to answering this question: …?"
   - All questions go in one call. The call is split only when the spans would exceed the ~80 KB request cap, with at most 3 requests in parallel.
   - The span text is clipped to 4 KB, then redacted and framed like every other Jev state.

### What explore may read and send

These are the security controls. Each holds on its own, and none depends on ignore files.

1. **Allowlist.** Explore reads source and docs files only:
   - code: `.go .py .ts .tsx .js .jsx .mjs .cjs .java .kt .cs .rb .php .rs .swift .c .h .cpp .hpp .scala .sql .sh .ps1 .vue .svelte .astro`;
   - docs: `.md .mdx .rst .txt`, plus `Makefile` and `Dockerfile`.

   Config formats commonly hold secrets: `.json .yaml .yml .toml .ini .properties .xml .tfvars`. They are read only with `--include-config`, and are still deny-listed and redacted. Everything else is never read.
2. **Deny-list**, whatever the allowlist, `--include-config` or ignore files say:
   - `.env*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks`, `*.kdbx` and `id_*`;
   - any name containing `credentials` or `secret`, and `*.tfstate*`;
   - `.npmrc`, `.pypirc`, `.netrc`, `.git-credentials` and `.pgpass`;
   - dumps: `*.sqlite`, `*.db`, `*.dump`, `*.sql.gz` and `*.bak`;
   - anything under `.aws/`, `.ssh/`, `.gnupg/` or `.git/`;
   - lockfiles.
3. **Redaction.** Every file is redacted line by line with `internal/secrets` before it is matched or turned into a span, so line numbers never shift. Whole PEM private-key blocks are blanked, because the pattern alone only catches the `BEGIN` line. tq redacts the request again before sending.
4. **Containment.**
   - No symlinks, Windows junctions or other reparse points.
   - Every path component must be a plain directory, and the file a regular file whose resolved path stays inside the resolved `--path`.
   - Nested repos are not entered: a directory below `--path` with its own `.git` (directory or `gitdir:` file) is skipped and counted (`skipped N nested repo(s)`). It is often another project or client. To search one, point `--path` inside it.
   - Files over 1 MB and binaries are skipped.
5. **No git process.** `git grep` in a cloned repo reads that repo's `.git/config`, and `core.fsmonitor`, `core.pager`, `diff.external` or a textconv driver there can run any program.

### Ignore files only reduce noise

Explore also honours ignore files (`internal/ignore`) to skip build output, vendored and generated code. This is relevance filtering, **not** a security control: a file the rules above admit is safe to read whether or not an ignore file lists it. Explore makes no attempt at exact git parity.

- **Sources, all read as text:**
  - every `.gitignore`, `.ignore` and `.rgignore`, including those in ancestors of `--path` up to the repo top;
  - `.git/info/exclude`, resolved through linked worktrees;
  - `core.excludesFile` from the git configs it can find (system, global, XDG, repo; following includes), plus `~/.config/git/ignore`.
- **Syntax:** `*`, `?`, `[...]`, `**`, `/` anchoring, trailing `/`, `\#`/`\!` escapes and trailing spaces. Matching is case-insensitive, and negations are dropped.
- **Best effort.** A source that cannot be read or parsed is skipped and counted: `note: N ignore source(s) could not be read or parsed and were not applied`. This means a little more noise, not a leak.
- **Tested against git.** A differential test checks that on a rich fixture nothing `git check-ignore` reports is yielded. It guards relevance quality, not safety.

The output is the top `--top` ranges as `score  path:start-end  [terms]`. The header says which ranking you got:

- `jev ranked N candidate spans (k request(s))`: the scores are Jev's relevance probabilities.
- `keyword ranking … (jev unavailable: …)`: the scores are keyword scores normalized to [0, 1]. You get this fallback when:
  - the backend is off, there is no key, or the directory is not in a trusted workspace;
  - any request fails, or any answer is missing;
  - you pass `--no-jev`.

  A partial set of Jev answers is never mixed with keyword scores.

Jev only re-orders what the keyword stage found. It never adds a file. The policy comes from the workspace holding `--path`. Explore is a ranking, not a gate, so it runs in shadow and enforce mode alike. Calls appear in `log.jsonl` with `purpose: explore`.

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
