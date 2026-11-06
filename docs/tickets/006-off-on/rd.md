# 006: `tentaqles off` and `tentaqles on`

**Status:** ready
**Risk:** High: if an agent can switch the guard off or forge a raw session, every protection of that identity is gone with no human involved
**UI:** none

PRD v2 §9. The developer can turn one identity's guard off, for the terminal they are in and every new one, until they turn it back on. Other identities and already-open terminals are not affected. Turning off is human-only, and turning on is free and immediate everywhere. When a state can't be proven off, it is on.

## Requirements

- **REQ-1**: `tentaqles off [<identity>]` must act on the named identity, or the current folder's, and must refuse with `human-only` under any detector of `internal/humanonly`, logging the refusal.
- **REQ-2**: On a terminal, `tentaqles off` must ask for a typed `yes`, naming the identity, before writing anything.
- **REQ-3**: `tentaqles off` must write a signed off state under `$TQ_HOME` holding the identity, the time and a list of session ids, and must add its own terminal's new session id to that list.
- **REQ-4**: An off state that is missing, unreadable, unsigned, or fails its signature must count as on. The guard stays on and the terminal is an identity terminal.
- **REQ-5**: `tentaqles on [<identity>]` must delete the off state, for any caller, an agent included, with no prompt, and must print `guard on for <identity>`.
- **REQ-6**: The terminal that ran `off` must become a raw session by its next prompt at the latest: PATH is the user's global PATH, identity and provider config variables are unset, and `CLAUDE_CONFIG_DIR` is unset.
- **REQ-7**: A terminal opened or entered in that identity while it is off must start as a raw session and add its own session id to the signed list, only if `internal/humanonly` finds no agent in its process tree or environment.
- **REQ-8**: A shell started by an agent, under any detector, must stay an identity terminal with the guard on while the identity is off, and must not add any id to the list.
- **REQ-9**: A terminal of that identity already open when `off` ran elsewhere must keep its identity environment and guard until `tentaqles off` is typed in it, or until it closes.
- **REQ-10**: Every raw session must carry `TQ_OFF_SESSION=<identity>:<id>`, with `<id>` a random 128-bit value, and the marker alone must never change any decision.
- **REQ-11**: `tq claude-hook pre-tool-use` must allow a call with exit 0 and no rule run, only when the off state exists, its signature passes, the marker's identity equals the identity resolved for the call's working directory, and the list holds the marker's id. In every other case it runs the rules as usual.
- **REQ-12**: The plugin's memory hooks in a session under an accepted marker must behave as with any session on the global config, and tq adds no hook to the global config.
- **REQ-13**: `claude` in a raw session must run the real `claude` with `--dangerously-skip-permissions`, the user's global login and no `CLAUDE_CONFIG_DIR`.
- **REQ-14**: Every raw session must print the banner with `guard: off`, then the line `  guard off for <identity>: no identity environment, no guard. Run tentaqles on to restore it.`.
- **REQ-15**: After `tentaqles on`, the terminal that ran it must be an identity terminal again by its next prompt, with PATH and variables as the identity sets them.
- **REQ-16**: A raw terminal still open after `on` must have its guard on at once, through REQ-11, while its environment stays raw until it closes.
- **REQ-17**: Identities other than the named one must not change in any way: same environment, same hook decisions.
- **REQ-18**: `off` on an identity without `guard: v2` must refuse with a line saying the identity needs `guard: v2`, naming `tentaqles edit`, and write nothing.
- **REQ-19**: `tq doctor` and `tq list` must show `guard: off` for an identity in the off state, naming how many sessions are listed but no session id.
- **REQ-20**: `docs/COMMANDS.md` must carry the §9.4 limits: sessions that keep running, marker reach, project `CLAUDE.md` files, a missing global login, and terminal interception through the profile hook only.

## Out of scope

- `--for <minutes>`: the owner decided no on 2026-11-05.
- `tq run --off`, `claude-off` and `codex-off`, which ticket 015 removes.
- A human approval path at runtime for an agent.

## Affected areas

- `internal/offstate/` (new): the signed record, using `internal/trust`'s key
- `internal/cmd/off.go` (new), `internal/cmd/root.go`: the two commands
- `internal/envplan/`: the raw environment, session id, keeping the terminal's decision
- `internal/cmd/env.go`, `internal/cmd/activate.go`, `internal/shell/hooks/*.tmpl`: decide at open, and react to `off` and `on` typed in this terminal
- `internal/cmd/claudehook.go`: REQ-11, before any rule
- `internal/cmd/run.go`, the shim of ticket 003: `claude` in a raw session
- `internal/policy/selfprotect.go`, `internal/protect/pathset.go`: `off` is human-only, `on` is free, the off state is a protected path
- `internal/doctor/`, `internal/cmd/list.go`, `internal/banner/`
- `docs/COMMANDS.md`, `docs/threat-model.md`, `CHANGELOG.md`, `docs/adr/0014-guard-off-state.md`
- `CLAUDE.md`: Must not trade off gains the off-state tests

## Do not touch

- `plugin/tests/test_tq_off.py`: keeps passing until ticket 014 touches the plugin. The plugin's hook still only runs tq.
- `internal/humanonly/`: reuse the detectors; add none.

## Decisions

- **Agreed with the owner**: no time box.
- **Agreed with the owner**: `tq run --off` and the off launchers go, so `off` is the only way.
- **Decided by the ticket writer**: entering an off identity by `cd` from a neutral folder counts as opening a terminal there: it is raw. The PRD covers only opening. Entering a terminal into an identity that is off, under its human-only checks, is the closest reading.
- **Decided by the ticket writer**: a raw terminal that `cd`s to another identity gets that identity's normal environment and guard. The marker names one identity, and REQ-11 compares it to the working directory, so other identities are never affected.
- **Decided by the ticket writer**: the off state lives at `$TQ_HOME/identities/<ws>/off.json`, signed with the integrity key. Signing passes the same human check as the integrity record.
- **Decided by the ticket writer**: REQ-11 only reads the off state, with no write, and needs no key beyond verifying, so it adds no write path to the hook. Its read stays within the 6 s deadline.
- **Decided by the ticket writer**: an unflagged identity has no off state (REQ-18), since §0 keeps new behaviour behind the flag and `guard: v2` is that flag.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 006` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
