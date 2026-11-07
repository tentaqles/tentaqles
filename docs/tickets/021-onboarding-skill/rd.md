# 021: The onboarding skill drives the logins

**Status:** landed
**Risk:** High: a skill step that handles a credential badly puts a client's token in the transcript or in another client's store
**UI:** none

IDEATION: "An onboarding skill then drives the login to each configured provider by OAuth, token or MCP. The developer is then fully connected to the client's stack through the agent." `docs/IDEATION.md` is the spec. It reads only `tentaqles status --json`, and uses `tentaqles install` and `tentaqles connect`. The hook still enforces everything, so a wrong instruction fails safe.

## Requirements

- **REQ-1**: `plugin/skills/onboard/SKILL.md` must be rewritten so its first step runs `tentaqles status --json`, and it must never read the manifest, `.claude.json` or anything under `~/.tentaqles`.
- **REQ-2**: In a neutral or untrusted folder, it must say what `status --json` reported, naming `tentaqles reach` or `tq hooks verify`, and stop.
- **REQ-3**: It must walk `providers` in the order given, one at a time.
- **REQ-4**: CLI step: when `cli` is set and `cli_resolves` is false, it must run `tentaqles install <provider>`. When the output says `manual`, it must give the vendor's page from that output and check again after the developer says done. It must never choose an install command or download a file itself.
- **REQ-5**: Credential step: when `credential` is `present`, it must skip it. Otherwise it must use the first method the provider supports in this order: OAuth, token, MCP.
- **REQ-6**: When a login needs an interactive terminal, the skill must print the exact command with the `!` prefix for the developer to run in the session, for example `! gh auth login --web`, and wait.
- **REQ-7**: A token must go through `tq secret set <ws> <provider>`, which the skill prints for the developer to run with `!`. The skill must not ask for, repeat or store a token, and if one is pasted it must say not to share it here and not use it.
- **REQ-8**: Connections step: it must run `tentaqles connect <provider>`. On `no MCP server in the catalog`, or for a claude.ai connector, it must give the developer the exact step and check afterwards.
- **REQ-9**: Verify step: it must use `verified` from `status --json`, or make one named read-only MCP call.
- **REQ-10**: A browser step must use `tentaqles open <url>`, never `start`, `open` or a browser's name.
- **REQ-11**: For each name in `other_providers`, it may help the developer install and log in, and it must say that the login isn't isolated per client and that the guard can't check it.
- **REQ-12**: For a task provider, it must do only the login and the connection, with no reading or writing tasks beyond the one read-only verify call.
- **REQ-13**: It must end with one table, one row per provider, with columns `Provider`, `CLI`, `Credential`, `Connections`, `Verified`.
- **REQ-14**: A re-run must skip what is present and verified, and pick up a CLI removed or a credential expired since the last run.
- **REQ-15**: It must refuse to install, authenticate or connect a provider outside `scope`.
- **REQ-16**: Its description must trigger on `onboard`, `connect my tools`, `log me in` and `is everything connected`, and on a first session in a new client.

## Out of scope

- Automating a login's browser page.
- A hook of any kind. The skill never changes enforcement.
- Sending or changing anything through a connector while verifying.

## Affected areas

- `plugin/skills/onboard/SKILL.md`: rewrite
- `plugin/tests/test_onboard_skill.py` (new): text checks of the skill
- `plugin/CHANGELOG.md`, `CHANGELOG.md`, `docs/COMMANDS.md`

## Do not touch

- `plugin/hooks/`, `plugin/scripts/`: the hook doesn't change.
- `internal/`: the commands exist by now.

## Decisions

- **Agreed with the owner**: the skill may try to help with an `Other` provider, and says plainly that it isn't isolated or guarded.
- **Decided by the ticket writer**: the skill keeps the name `onboard`, so `/tentaqles:onboard` still works.
- **Decided by the ticket writer**: a login that needs a terminal is run by the developer with the `!` prefix, because the skill can't answer a device prompt. This keeps the login under the client's environment.
- **Decided by the ticket writer**: the ticket adds no ADR, since it doesn't touch a trust-boundary path.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 021` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
