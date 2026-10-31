# What the state-folder guard is for

`tq/hub-store-write` reads each shell command an agent wants to run and blocks the
ones that reach tq's state folder (`$TQ_HOME`, normally `~/.tentaqles`). It protects
only the parts tq relies on: `hub/`, `packs/`, `trust/`, `guard/`, `build-gate/`,
`guard.yaml`, `config.yaml` and `audit.jsonl`. The rest of the folder, such as
`identities/<id>/claude/` and `bundles/`, stays open. File tools (Edit, Write) are
checked on the resolved path, which is much more reliable than reading a command.

## What it is

A seatbelt for honest mistakes. It catches an agent that edits a ledger to "fix" a
stuck approval, cleans up `~/.tentaqles/packs` while tidying disk space, follows a
path through `..` or a variable tq exported, or links a folder into the state folder
without meaning harm. Those are the slips that happen in practice, and the guard
stops them with a plain reason the agent can act on.

Planned (`docs/prd.md` FR-70, decision D-24): `tq/human-answer` becomes the one locked
rule whose verdict depends on the session class as well as the text. From a `console`
session it allows `tq hub answer` and the pack `apply` verbs (the other board commands were
never matched by this rule; the hub's own policy decides them); from `run`, `nested` or
`unknown` it denies them; the reserved `hub key|token|serve-key` verbs, HTTP calls to the
hub's decision routes and reads of `serve-*.json` stay denied in every class. The
class comes from environment and process ancestry, so no rewording changes it; AR2 still
applies to the text half.

## What it is not

It is not a security boundary against a determined adversary. It is a text filter
over shell languages (bash, PowerShell, cmd), and a text filter over a shell can never
be complete: the shell decides what a command means only when it runs it. Each
review round of a larger rework found about ten new bypasses, and the extra rules
started blocking normal commands. So tq ships only small fixes that close a clear
hole without new false positives, and documents the rest.

## Known gaps, by class

- **Globs.** A pattern the shell expands to the state folder's own name
  (`~/.tentaq*`, `~/.t?ntaqles`) is not seen as that name. A glob right under a
  recognised root (`~/.tentaqles/p*`) is blocked.
- **Escapes.** Bash backslashes (`pa\cks`), PowerShell backticks, line
  continuations and empty expansions (`.tentaq$()les`) can split a name. cmd carets
  (`pa^cks`) are folded; the others are not, in every position.
- **Variables.** A value built at run time (`D=pa; D+=cks`, a value read from a
  file, `Join-Path` with variables) is not resolved. Only the variables tq itself
  exports into the state folder are expanded.
- **Links.** A link made by a command the guard does not know (a script, an
  interpreter call, a link target held in a variable) is not seen. Writes through an
  existing link elsewhere are caught for file tools, not for shell commands.
- **Interpreters.** Python, Node, Ruby, Perl and others can build any path from
  pieces (`os.path.join(*parts)`, `chr()`, base64), and a script file written in one
  step and run in another is never read by the guard.

## Known false positives

The guard prefers a false alarm to a carve-out a shell can abuse. These are blocked
on purpose:

- a commit message with `$(...)` or a backtick that mentions a protected part, such
  as the `git commit -m "$(cat <<'EOF' ... EOF)"` form. Write the message to a file and
  use `git commit -F <file>`;
- a tq command that names the state folder and has a parenthesis, a backtick or a
  curly quote anywhere, even inside quotes (`tq hub add "check the hub lock (prod)"`
  with the hub path in it);
- `cp -s` (or another link option) of a file whose name starts with `.tentaqles`,
  such as `.tentaqles.yaml`.

## The real protections

1. **Claude Code's sandbox for writes.** Turn on Claude Code's sandbox (`/sandbox`)
   so shell commands can write only inside the working folder. The operating system
   then refuses a write to `~/.tentaqles` however the command spells it. Its limits:
   - it runs on macOS and on Linux or WSL2, not on native Windows;
   - it covers the Bash tool, not the PowerShell tool;
   - a `tq` the agent starts runs inside it too, and tq must write its state folder.
     In the sandbox's `excludedCommands`, list only exact tq subcommands that write
     state and start nothing: `tq hub add`, `tq hub move`, `tq hub comment`,
     `tq hub cancel`. Entries match as prefixes, so never list a bare `tq hub` or
     `tq pack`: they would cover `tq hub run` (starts an agent run), `tq hub open`
     (starts a browser) and `tq pack <pack> plan` (starts the plan tool). Never
     exclude `tq run`, `tq dotenv run` or `tq redact run` either: they start a child
     command, which would then run outside the sandbox. Do not exclude `tq` as a
     whole, and do not allow-list `TQ_HOME` for writes: either gives every command
     that write access and removes the protection.
2. **Signed, tamper-evident state.** What is signed today:
   - pack ledger answers, with a key kept in the OS keychain. A ledger line written
     by anything other than tq does not verify, so a forged write cannot grant,
     widen, extend or replay an approval
     ([framework-security.md](packs/framework-security.md));
   - build-gate approvals, sessions, reviews and decisions, with an HMAC whose key
     is a file in the state folder (`build-gate/hmac.key`). Only this text filter
     protects that file, so an agent that gets past the filter can read the key and
     sign its own records;
   - hub tokens, which are minted with an HMAC.

   Moving the build-gate key into the OS keychain was considered and dropped: the
   agent's own build steps must be able to sign, so the key would gain little
   ([threat model, Decisions](threat-model.md#decisions)). Extending signatures to the
   other protected parts (`trust/`, `guard.yaml`, `config.yaml`) is possible follow-up
   work.

The guard stays on, and is never-disable, because catching the common slip early is
cheaper than recovering from it. Treat a block as useful feedback, and do not read
its silence as proof that a command is safe.
