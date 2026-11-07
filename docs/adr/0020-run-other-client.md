# ADR-0020: An agent session runs only the identity of its own folder

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md`, `docs/prd.md` FR-13, ADR-0003 (human-only detection), ADR-0008 (identity PATH), ADR-0011 (one guard)

## Context

`tq run <ws> -- <cmd>` launches a command under workspace `<ws>`'s identity from any folder. A
human needs that: it is the command's purpose. An agent session that names another client gets
that client's identity and credentials, so a plain command crosses clients. The hook judges a
command's text and the folders it touches; nothing judged the name `tq run` was given. FR-13 says
a nested agent cannot loosen the identity, but `TestRunFromAgentSession` only launched the agent's
own workspace.

## Decision

In an agent session, `tq run <name> -- <cmd>` runs only when `<name>` is the trusted workspace of
the folder the process runs in. An agent session is any detector of `humanonly.DetectAgent`: a
harness marker in the environment, a harness in the process tree, or a process tree that cannot be
followed to a terminal. For any other name (another client's, a neutral folder's, or one that is no
client), `tq run` exits 1 with this text and starts nothing:

    tq run: <ws> is another client. An agent session can only run under the client of its own folder.

It writes one line to the denial log: category `identity`, rule `other-identity`, layer `human-only`,
caller `harness`, with the detector in the reason and no path of the other client.

- The folder decides, not an inherited `TQ_WS` or `TQ_WS_ROOT`, which an agent can set.
- The name is compared before any workspace is looked up, and the check runs before the
  environment is built, so an agent learns nothing about a workspace it may not run.
- `tq run .` and `tq run <its own client>` launch as before, with that identity and no bypass flag.
- A human's `tq run <any client>` from any folder launches as before, with no prompt.
- The denial layer is `human-only`. The ticket named `command` for the case where ticket 016 lands
  first; no such layer exists yet.

No hook rule was added: the command is the check (`internal/policy` and `internal/guard` are
unchanged).

## Consequences

- `cd <other> && tq run <other>` in one shell line passes this check, because the process's own
  folder is the other client's. The hook judges the `cd` into another client's root, and the
  rest is a text-filter matter (AR2 in `docs/threat-model.md`).
- A human whose process tree cannot be followed to a terminal is refused `tq run <other>` the same
  way (the detector is unverified, and FR-13 counts it as an agent). `tq run <own>` and `tq run .`
  still work from the folder.
- Every refusal is one line in `$TQ_HOME/denials.jsonl`, read with `tq status --denials`. A denial
  log that cannot be written still refuses, and the message says so.
