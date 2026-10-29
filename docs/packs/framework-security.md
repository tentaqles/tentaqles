# Pack framework: security model and known limits

This page covers how the pack framework keeps an agent from granting itself a gated action (an ML promotion, a cloud apply or destroy), and what it does not yet stop.

## What protects an approval

- **A signed ledger.**
  - Each `answered` line in a pack ledger carries an HMAC-SHA256 over its canonical JSON: the decision id, the binding (pack, action, subject, target), the evidence hash, the choice, the actor kind and the time.
  - The key is per workspace and lives in the OS keychain, under service `tentaqles-packs` with account `<workspace>`.
  - Only the human answer path creates the key or signs with it.
  - `approval.Verify`, `Find` and `FindGuard` ignore an answered line whose MAC is missing or wrong, or whose binding or evidence differs from the asked line.
  - If the keychain cannot be read, no answer counts (fail closed). A raw append to the ledger can no longer forge a grant.
  - On a machine with no keychain, such as headless Linux with no Secret Service, pack approvals cannot be granted, and `tq doctor` reports `packs-keychain-missing`.
- **Only a human answers, at a terminal.**
  - `tq hub answer` on a pack approval needs a real terminal on stdin. You must also type the binding back: the pack, the action, and a 12-hex code of the subject. `--choice` alone never answers one.
  - That path puts a human-presence marker, carrying the signer, into the hub service's context. The service refuses a pack approval without it, so every answer path goes through this check.
- **The board does not answer pack approvals (for now).**
  - The board shows the decision, its evidence and the exact `tq hub answer …` command.
  - Every web route refuses an answer for a `data.pack` decision (403), whatever the cookie, token or local key.
  - A browser session proves nothing that a same-user script cannot reproduce. **WebAuthn** with user verification (a platform passkey such as Windows Hello or Touch ID, registered from the human's terminal) is the planned way to answer from the board.
- **Agents are stopped at the hook (defence in depth).** `tq/human-answer` is a core deny rule. It cannot be disabled or superseded, and unattended mode never skips it. It covers:
  - `tq hub answer`, `tq pack <x> approve` and `tq pack ml promote apply`, in any spelling: global flags before the verb, `t''q`, a variable or call operator, a subprocess argv list;
  - HTTP clients reaching the hub's decision and answer endpoints. The URL is matched after percent-decoding and after quote and `+` folding;
  - reads of `hub/serve-*.json`.
  - `tq/secret-store-shell` denies shell commands that name the ledger key's keychain entry (`tentaqles-packs`), for example `cmdkey`, PasswordVault and CredentialManager, `secret-tool`, `security find-generic-password` and keyring libraries.
- **The protected parts of the state directory.**
  - The protected parts are:
    - `hub/` (the hub store and its serve info);
    - `packs/` (ledgers, claims, plans, state dirs);
    - `build-gate/` (gate records and the HMAC key);
    - `trust/` (the manifest trust store);
    - `guard.yaml` and `guard/` (guard settings and logs);
    - `config.yaml` (the base registry: editing it can drop a session's workspace, and with it the client's guard rules);
    - `audit.jsonl`.
  - The rest stays open to agents, such as `identities/<id>/claude/` (memory, plugin cache, projects) and `bundles/`.
  - `tq/hub-store-write` (never-disable) denies a shell command, other than a bare `tq …` call, that reaches a protected part. The match runs after these folds:
    - every root spelling (real, final and 8.3 paths; `~/.tentaqles`; the `TQ_HOME` spellings);
    - every variable tq exports that points inside the state directory (`CLAUDE_CONFIG_DIR` and the others), in each of its spellings including the bare name;
    - quotes, glob brackets and `+` folded;
    - `..` resolved lexically.
  - Also denied:
    - a root spelling and a `..` path in one command (fail closed);
    - a bare root plus a protected name in another path token;
    - a link created at or under the root;
    - a `cd` under the root, tracked cumulatively, followed by a relative path into a protected part;
    - the 8.3 names of the protected children.
  - Edit and Write are checked on the canonical path, which is resolved through the nearest existing parent's handle.
- **Single use and binding.**
  - An approval is claimed with an `O_EXCL` file before the allow or before the human-run command, so one use wins.
  - `Verify` compares pack, action, subject, target and evidence.

## Known limits (the key-isolation follow-up)

- **The regex layer is not complete.** String building the matcher cannot follow will pass it. Examples are `os.path.join(*['.tenta','qles'])`, `chr()` sequences, base64 and script files written in one step and run in another. The ledger MAC is what holds there: a write that gets past the hook still cannot produce a valid answered line.
- **Claims are files.** Deleting a claim file under `packs/<ws>/<pack>/claims/` would make a used approval look unused. Only the regex layer protects them. `consumed` lines are not signed.
- **A pseudo-terminal.** A pty (`script`, ConPTY, pywinpty) can satisfy the terminal check of `tq hub answer` and `approval.Confirm`. The typed binding makes this harder. The keychain key stays out of the agent's reach only while the keychain itself refuses same-user reads, which most OS keychains do not do for a same-user process outside the hooks.
- **Processes outside the hooks.** A process that does not go through Claude Code's PreToolUse hook, such as a script an agent starts that later acts, is not seen by the hook rules.
- **A ledger-lock denial of service.** Appends and compaction take an OS lock on `<pack>/ledger.lock`, with a 2 s bound. A same-user process that holds the lock makes every gated call deny and every pack answer fail ("retry"). This fails closed; it never grants.
- **Second factors.**
  - `approval.Confirm` is a second factor. The signed human answer is the first.
  - `tq/pack-config-change` asks, and does not deny. The repo layer can no longer enable or loosen anything; a baseline edit is still flagged as `baseline_change`.
