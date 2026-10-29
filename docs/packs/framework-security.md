# Pack framework: security model and known limits

This page covers how the pack framework keeps an agent from granting itself a gated action (an ML promotion, a cloud apply or destroy), and what it does not yet stop.

## What protects an approval

- **A signed ledger.**
  - Each `answered` line in a pack ledger carries an Ed25519 signature (domain tag `tq.packs.ledger/v2`) over the decisive fields: the decision id, the binding (pack, action, subject, target), the evidence hash, the grant, the expiry, the time of the asked line, the choice, the actor kind and the answer time.
  - Readers take every one of these fields from the signed line, not from the `asked` line. Rewriting the asked line (its grant, its expiry or its binding) changes nothing: a signed reject stays a reject, and a signed expiry stays the expiry.
  - The keys are per workspace and live in the OS keychain under service `tentaqles-packs`. The private key (account `<workspace>`) is read only on the human answer path, which creates the pair on first use. The public key (account `<workspace>/pub`) is a separate entry that readers load; it is never written to a file. The answer path refuses to sign when the public entry does not match the private key. `tq doctor` (`packs-keychain-missing`) probes the public entry only.
  - **The shown binding.** The terminal answer passes the decision as the hub store shows it (binding, expiry, grant and evidence) to the signer. The signer refuses unless the ledger's asked line matches it field for field, so a rewritten ledger cannot get a different action signed. The displayed decision also travels with the human-presence marker, and the store row must still equal it when the answer is signed. On any difference the decision stays pending.
  - `approval.Verify`, `Find`, `FindGuard` and `List` ignore an answered line whose signature is missing or wrong.
  - If the keychain cannot be read, no answer counts and every pack guard that looks up an approval denies (fail closed).
  - On a machine with no keychain, such as headless Linux with no Secret Service, pack approvals cannot be granted.
- **A high-water mark makes use stick.**
  - Each consume (the hook's allow or ask, `approval.Claim` and `approval.MarkUsed`) raises a keychain entry `<workspace>/<pack>/hwm` to the signed answer time of the approval it used, and records its id. If the write fails, the call is denied, or the human-run command is told not to run.
  - Readers treat as used the marked approval and every answer in the pack signed at or before the mark (answer times carry nanoseconds, and a tie is retired too). Deleting a claim file, compacting the ledger or re-appending lines therefore cannot make a used approval usable again.
  - **The cost:** using one approval also retires every older unused approval in the same pack. Answer approvals in the order you mean to use them, or ask again.
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
    - `build-gate/` (gate records and the build gate's HMAC key);
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
  - `Verify` compares pack, action, subject, target and evidence with the signed line.
  - **Claim on ask.** When a pack guard allows a call and another rule asks, the hook cannot know whether the human will accept the prompt or whether the call runs. It claims the approval anyway, and the prompt says so ("it is consumed now, even if you decline"). Declining wastes the approval; it never lets one approval run twice.
  - The human-run order is `FromCommand`, `HumanOnly`, `Confirm`, `Claim`, run, `MarkUsed`, `Comment`.
- **Ledger before store.** An answer appends its signed ledger line before the hub store commits the answer. If the commit then fails (rare, and only on the human path), the ledger holds a signed answer for a decision the store still shows as pending. A retry appends a second signed line; readers use the first valid one, so the outcome is the one the human signed first.
## Known limits (the key-isolation follow-up)

- **The regex layer is not complete.** String building the matcher cannot follow will pass it. Examples are `os.path.join(*['.tenta','qles'])`, `chr()` sequences, base64 and script files written in one step and run in another. The ledger signature is what holds there: a write that gets past the hook cannot produce a valid answered line without the private key.
- **Claims are files, the mark is not.** Claim files and `consumed` lines are not signed. Deleting them no longer revives an approval, because the keychain high-water mark still says it was used; a process that can write the keychain can lower the mark, which is the same reach as reading the private key.
- **A pseudo-terminal.** A pty (`script`, ConPTY, pywinpty) can satisfy the terminal check of `tq hub answer` and `approval.Confirm`. The typed binding makes this harder. The private key stays out of the agent's reach only while the keychain refuses same-user reads, which most OS keychains do not do for a same-user process outside the hooks. What the signature guarantees is that a grant needs the private key: a ledger write alone, from any process, cannot grant, widen, extend or replay an approval. It does not stop a same-user process that reads the keychain directly.
- **Processes outside the hooks.** A process that does not go through Claude Code's PreToolUse hook, such as a script an agent starts that later acts, is not seen by the hook rules.
- **A ledger-lock denial of service.** Appends and compaction take an OS lock on `<pack>/ledger.lock`, with a 2 s bound. A same-user process that holds the lock makes every gated call deny and every pack answer fail ("retry"). This fails closed; it never grants.
- **Second factors.**
  - `approval.Confirm` is a second factor. The signed human answer is the first.
  - `tq/pack-config-change` asks, and does not deny. The repo layer can no longer enable or loosen anything; a baseline edit is still flagged as `baseline_change`.
