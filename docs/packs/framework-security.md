# Pack framework: security model and known limits

This page covers how the pack framework keeps an agent from granting itself a gated action (an ML promotion, a cloud apply or destroy), and what it does not yet stop.

## What protects an approval

- **Only a human answers.**
  - `tq hub answer` on a pack approval needs a real terminal on stdin. You must also type the binding back: the pack, the action, and a 12-hex code derived from the subject. `--choice` alone never answers one.
  - On the board, a pack approval is answered only from a browser session: the session cookie the hub set, plus a same-origin form post with the CSRF token rendered into that page.
  - The local key (`X-TQ-Hub-Local`), the token header and the JSON API never answer a pack approval.
- **Agents are stopped at the hook.** `tq/human-answer` is a core deny rule. It cannot be disabled or superseded, and unattended mode never skips it. It covers:
  - `tq hub answer`, `tq pack <x> approve` and `tq pack ml promote apply` from Bash or PowerShell;
  - HTTP posts to the hub's `/decisions/` and `/answer` endpoints;
  - reads of `hub/serve-*.json`, which holds the hub's local key.
- **The protected parts of the state directory are off limits to agents.** The protected parts are `hub/` (the hub store and its serve info), `packs/` (ledgers, claims, plans and state dirs), `build-gate/` (gate records and the HMAC key), `trust/` (the manifest trust store) and `guard.yaml`. The rest of the directory, such as `identities/<id>/claude/` (auto-memory, the plugin cache, projects) and `bundles/`, stays open to agents. `tq/hub-store-write` (never-disable) denies:
  - any shell command, other than a bare `tq …` call, that reaches a protected part. The directory is matched as its real path, its final path, its 8.3 short name, `~/.tentaqles`, `$TQ_HOME`, `${TQ_HOME}`, `%TQ_HOME%` and `$env:TQ_HOME`, with quotes, glob brackets, `/./` and `//` folded away. A glob segment under the root also counts, as does a bare mention of the root with a protected name elsewhere in the command (a variable or a link);
  - link and junction creation aimed at the root or a protected part;
  - a `cd` under the root followed by a relative path into a protected part;
  - Edit, Write and NotebookEdit of any path that resolves into a protected part. The nearest existing parent of the path is resolved through its open handle first.
- **Single use and binding.**
  - An approval is claimed with an `O_EXCL` file before the allow or before the human-run command, so only one use ever wins.
  - `Verify` compares the pack, action, subject and target, and the evidence hashes.

## Known limits (the key-isolation follow-up)

- **Processes outside the hooks.** The hook rules stop a careless agent, not one set on defeating them. An agent process that does not go through Claude Code's PreToolUse hook, such as a script it starts that later writes files, is not seen.
- **A pseudo-terminal.** A pty (`script`, ConPTY, pywinpty) can satisfy the terminal checks of `tq hub answer` and `approval.Confirm`. The typed binding and the hook rules make this harder but do not rule it out.
- **The ledger is not signed.** `answered` lines are plain JSON. A raw append that gets past the hooks forges an approval. The follow-up is a MAC on each ledger line, keyed by a secret the agent cannot read (OS key isolation), or a re-check of each answer against the hub database.
- **Second factors.**
  - `approval.Confirm` (typed `yes` at a terminal) is a second factor. The required human answer is the first factor.
  - `tq/pack-config-change` asks, and does not deny. The repo layer can no longer enable or loosen anything, so its residual is a baseline edit, which triage still flags as `baseline_change`.
- **The board without a token.** On loopback, the hub has no login. A local process that can talk HTTP could fetch a page, take its session and CSRF token, and post the form. `tq/human-answer` denies such posts from the agent's shell; anything outside the hooks is the limit above.
