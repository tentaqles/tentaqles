# 012: The onboarding skill

**Status:** parked
**Risk:** High: a skill step that handles a credential badly puts a client's token in the transcript or in another identity's store
**UI:** none

PRD v2 §5.2 to §5.4. After `reach`, Claude Code starts in the new terminal and the skill walks each provider in scope: CLI, credential, connections, verify. It reads only `status --json`, and it uses the commands of tickets 008 and 009. The hook still enforces everything; the skill is the happy path, so a wrong instruction fails safe.

## Requirements

- **REQ-1**: `plugin/skills/onboard/SKILL.md` must be rewritten so its first step runs `tentaqles status --json`, and it must never read the manifest, `.claude.json` or anything under `~/.tentaqles`.
- **REQ-2**: In a neutral or untrusted folder, it must say what `status --json` reported, naming `tentaqles reach` or `tq hooks verify`, and stop.
- **REQ-3**: It must walk `providers` in the order given, one provider at a time, doing the four steps of REQ-4 to REQ-7.
- **REQ-4**: CLI step: when `cli` is set and `cli_resolves` is false, it must run `tentaqles install <provider>`, and when the output says `manual`, it must give the vendor's page from that output and check again after the developer says done. It must never choose an install command or download a file itself.
- **REQ-5**: Credential step: when `credential` is `present`, it must skip it. Otherwise it must use the first method the provider supports in this order: OAuth device flow, personal access token, MCP server credential.
- **REQ-6**: Connections step: it must run `tentaqles connect <provider>`. On `no MCP server in the catalog`, or for a claude.ai connector, it must give the developer the exact step and check afterwards.
- **REQ-7**: Verify step: it must make one read-only call, as `tentaqles status --json` does in its `verified` field, or one named read-only MCP call.
- **REQ-8**: When a login needs an interactive terminal, the skill must print the exact command with the `!` prefix for the developer to run in the session, for example `! gh auth login --web`, and wait.
- **REQ-9**: A browser step must tell the developer to use the identity's browser, and for an `xdg-open` or `BROWSER` route rely on tq's shim, never opening a URL with `start`, `open` or a browser's name.
- **REQ-10**: A personal access token must go through `tentaqles secret set <ws> <provider>`, which the skill prints for the developer to run with `!`. The skill must not ask for, repeat or store a token, and if one is pasted, it must say not to share it here and not use it.
- **REQ-11**: For the task provider, the skill must do only the login and the connection: no reading or writing tasks beyond the one read-only verify call.
- **REQ-12**: The skill must end with one table, one row per provider, with columns `Provider`, `CLI`, `Credential`, `Connections`, `Verified`, and each `other_providers` name shown as `manual`.
- **REQ-13**: A skill re-run must skip what is already present and verified, and pick up a CLI removed or a credential expired since the last run.
- **REQ-14**: It must refuse to install, authenticate or connect a provider outside `scope`, naming `tentaqles edit` as the human's way to add it.
- **REQ-15**: Its description must trigger on a first `reach` launch as well as the phrases `onboard`, `connect my tools`, `log me in` and `is everything connected`.
- **REQ-16**: `reach`'s first prompt in ticket 010 must invoke this skill by its real name, `/tentaqles:onboard`.

## Out of scope

- The other 39 skills: ticket 014.
- Automating a login's browser page.
- A hook of any kind. The skill never changes enforcement.

## Affected areas

- `plugin/skills/onboard/SKILL.md`: rewrite
- `plugin/tests/test_onboard_skill.py` (new or moved): text checks of the skill
- `plugin/tests/`: the existing `test_onboard_skill_covers_every_provider_kind` and `test_onboard_skill_runs_no_human_only_command`, updated to the new text
- `plugin/CHANGELOG.md`, `CHANGELOG.md`, `docs/COMMANDS.md`

## Do not touch

- `plugin/hooks/`, `plugin/scripts/`: ticket 014.
- `internal/`: the commands exist by now.

## Decisions

- **Agreed with the owner**: `connect` is the path for connections.
- **Decided by the ticket writer**: the skill keeps the name `onboard`, so `/tentaqles:onboard` still works and `reach` has a stable name.
- **Decided by the ticket writer**: a login that needs a terminal is run by the developer with Claude Code's `!` prefix. The skill can't answer a device prompt, and this keeps the login under the identity's environment.
- **Decided by the ticket writer**: the D-08 rule that logins stay the human's still applies in spirit: the human runs the login, and the skill prints the line and verifies.
- **Decided by the ticket writer**: no ADR: this doesn't touch a trust-boundary path.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 012` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
