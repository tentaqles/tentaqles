# Reviewing tentaqles

How to judge a change to this repo.

## Priorities

Read `CLAUDE.md`, "Must not trade off", and `docs/threat-model.md`. They name a test for each line. If they disagree with this file, they win. A named test weakened, skipped or removed is a P0.

## What counts as a real failure

Something a developer or an agent can trigger, with a path you can show.

- **Identity drift.** A plain command runs under the wrong client's identity and isn't blocked: git email, gh user, cloud, the Claude or Codex config home. Or one client's config, credential or MCP server reaches another's session.
- **Secret leak.** A secret reaches the transcript, a commit, a log, a manifest or an error message. Includes a `.env` read, an environment dump, a secret-store read and `tq secret` printing a value.
- **Trust boundary.** An agent can do a human-only action (`tq allow`, `tq deny`, `tq secret` writes, `tq run --off`). A folder gets trusted without a person. An edited manifest keeps its trust. A repository manifest widens what the hook allows. A release or plugin install skips its checksum, provenance or pin. The code is also in `internal/cmd/humanonly.go` and `internal/cmd/claudehook*.go`.
- **A hook that stalls.** Any hook path with no timeout, an unbounded subprocess or network call, a read that can block, or heavy work on every tool call. Hooks must never stall a session.
- **A false positive that blocks everyday work.** A normal command under the right identity is denied: read-only git, ordinary builds, a `.env.example` read, a quoted string that only looks like a command. A block must give its reason. A guard nobody can live with gets turned off.
- **Breaks an install.** A migration, `tq uninstall --restore`, a shell profile block or an upgrade that loses a user's config, on Windows, macOS or Linux.

## Severity

Set by the impact if it happens, not by how likely you think it is. Unsure between two: pick the higher and say why in one line.

| Level | Meaning | Land? | Examples |
|---|---|---|---|
| **P0** | Identity crosses clients, a secret leaks, a person is taken out of a trust decision, sessions stall, or a user's install is damaged | No | A plain `git push` under the wrong email passes; a hook hangs; an agent runs `tq allow`; a must-not-trade-off test weakened |
| **P1** | A core command fails, or everyday work is blocked with no way through | No, unless the owner accepts it | A false positive on read-only git; `tq setup` fails on one OS; a failing test; a trust-boundary change with no ADR |
| **P2** | A real but contained defect with a workaround | Yes, with a follow-up | A wrong hint in a block reason; a doc that names a removed command |
| **P3** | No user impact | Yes | A clearer name, a simpler equivalent |

## What to ignore

Don't report these.

- **Guard text-filter bypass spellings.** The owner's rule: the guard is a speed bump for shell text. Rewordings (`env git`, `sh -c`, aliases, globs, escapes, variables, interpreters, links) aren't findings. No bypass-chasing and no clever carve-outs. Accepted as AR2 in `docs/threat-model.md`. A plain, literal command that used to block and no longer does IS a finding.
- **A program already running as the developer's OS user** (AR1): reading tq's files, writing trust markers, driving a pty, redirecting `TQ_BIN` or PATH.
- **What the owner left to the harness** (AR2): `git push --force`, a cloud delete, `terraform apply`, writes to the state folder with a file tool. A hook timeout or parse error letting a call through. The hook not asking for a confirmation.
- An accepted risk past its expiry is a question for the owner, not a finding.
- Planned work that isn't built, and open product questions. Flag it only if the code decides one.
- Style that matches the surrounding code. Anything `gofmt` and `go vet` accept.
- Generated files, `.job/`, `.claude/worktrees/`, `dist/`.
- Hypotheticals with no reachable path.

## How to report

Reviewer agents reply in their own JSON with these same fields. Anywhere else, lead with the verdict:

```markdown
## Review: <branch or PR> - <date>
**Verdict:** Land | Land after P1 fixes | Do not land (N blockers)
**Gates:** go build, go vet, go test, plugin suite: pass or fail, or not run

#### P0-1 <what breaks, for whom>
- **Class:** kebab-case kind of mistake, like `hook-no-timeout`
- **Where:** `file:line`, and every other place it shows up
- **Rule:** the line of this file, `CLAUDE.md` or the threat model it breaks
- **Repro:** steps someone with no context can follow
- **Expected / Actual:** what should happen, and what you saw, quoted exactly
- **Fix direction:** one or two lines

### Checked and fine
What you exercised that passed.

### Not checked
Anything you couldn't run, and why. Never leave this out.

### Questions
Things that need a product call. Not findings.
```

- **No rule, no finding.** Name the rule it breaks, or move it to Questions or drop it.
- **One finding per root cause.** Five sites from one helper is one finding listing five places.
- **Every finding has a class**, so the fix covers the whole class.

## Verification

- `go build ./...`, `go vet ./...` and `go test ./... -count=1`. Tests isolate `TQ_HOME`, `HOME` and `USERPROFILE`. A test that reads the real home folder is a finding. On Windows, run them through PowerShell.
- A change under `plugin/`: `PYTHONPATH=plugin python -m pytest plugin/tests -q`.
- New behaviour needs a new test. A changed flow updates its test in the same diff.
- A change on a trust-boundary path needs an ADR under `docs/adr/`. `CLAUDE.md`, "Project config", lists the paths. No ADR is a P1.
- A change to the guard or the secret rules tests what it blocks and what it must still allow.
- Evidence is the command, the exit code and the output. Report a skipped step as skipped.

## Rules learned over time

Format: `- **YYYY-MM-DD, area**: the rule. *Why:* what happened.` Added with the owner's yes, newest last.
- **2026-11-06, guard**: narrowing a rule to fix a false positive keeps a deny test for every form it used to block. *Why:* a fix for a false positive on `start` let plain forms through.
