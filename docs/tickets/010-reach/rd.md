# 010: `tentaqles reach`

**Status:** parked
**Risk:** High: reach creates an identity and signs its trust, so a bug can create a client under the wrong email or scope, or sign without a human; closing the wrong terminal loses work
**UI:** none

PRD v2 §4. One command that adds a client and leaves it fully provisioned, with no manual steps beyond logins. It replaces `tq init`, `tq add`, `tq onboard` and `tq setup`, which ticket 015 removes. Favour a resumable failure over a half-made identity.

## Requirements

- **REQ-1**: `tentaqles reach <name>` must create `<clients directory>/<name>` and make it an identity. With no name, it must use the current folder, with its folder name as the identity name and its parent as the clients directory.
- **REQ-2**: On first use with no clients directory registered, reach must ask `Clients directory` with the folder's parent or `~/clients` as the default, and register it. With `--clients-dir <folder>` it must register that one and not ask.
- **REQ-3**: A folder that is already an identity must refuse with `<name> is already an identity: use tentaqles edit` and exit 1, writing nothing.
- **REQ-4**: reach is human-only: under any `internal/humanonly` detector it must refuse and log before anything is written, with or without `--from`.
- **REQ-5**: On a terminal, reach must print the banner, unless `--no-banner` or `TQ_NO_BANNER=1`, then ask in order: identity email, git provider, cloud providers, team and meeting platforms, data platforms, task management provider, browser and profile, preferred shell, email provider.
- **REQ-6**: Each prompt must offer `Skip`, except where the browser and shell prompts have a default. Identity email must validate as an email. Each multi-select must offer `Other` with a free-text follow-up.
- **REQ-7**: The options must be exactly those of PRD §4.3, with Chrome and Edge only for the browser, and Google, Microsoft, Other, Skip for email. A browser left at Skip writes no `browser:` block.
- **REQ-8**: An `Other` value must be stored in the manifest under `other_providers` as names only, and must not enter the scope.
- **REQ-9**: Before any write, reach must show what it will create and ask for a typed `yes`.
- **REQ-10**: reach must provision in this order, each step recorded: manifest with `guard: v2`, `blocked_commands` empty and the chosen providers; empty trust store location; CLI installs; Claude Code setup; browser profile recorded; folder registered; signed integrity record; new terminal; `claude` with the onboarding skill as first prompt; ending the old terminal.
- **REQ-11**: The CLI install step must print the one line of ticket 008 per provider and continue past a failure. `--no-install` must skip it.
- **REQ-12**: The Claude Code setup step must leave the identity's config home with the guard hook, the shim, an MCP list holding nothing out of scope, the scope block, and the tentaqles plugin pre-registered.
- **REQ-13**: When the plugin can't be pre-registered, the identity terminal must print the exact install line on the first `claude` launch, and reach must not fail for that.
- **REQ-14**: `--no-onboard` must skip starting `claude`.
- **REQ-15**: The old terminal must end only after the new terminal has started. When a new terminal cannot start, the old one must stay open and print the failing step and how to resume.
- **REQ-16**: When the old terminal can't be ended, reach must print `Close this terminal when you are ready.` and exit 0.
- **REQ-17**: When a step other than a CLI install fails, reach must stop, print `reach stopped at step <n>: <reason>. Run tentaqles reach <name> to resume.`, and a re-run must resume from that step without repeating the prompts.
- **REQ-18**: `reach --from <manifest.yaml>` must skip the prompts, validate the file the way the manifest loader does, reject secret-shaped values, and still require the typed `yes`.
- **REQ-19**: `--from` with an unknown key, a value `LooksLikeSecret` flags, or a `client` that isn't the folder's name must refuse before any write.
- **REQ-20**: A name with a path separator, `..`, a control character or a reserved Windows name must refuse.
- **REQ-21**: The manifest must accept `task:` naming the task provider, `shell:` the preferred shell, and `other_providers:`, and the task provider must enter the scope.

## Out of scope

- Authenticating anything: ticket 012's skill does it.
- Removing the old commands: ticket 015.
- `edit`: ticket 011.
- Creating the browser's profile itself: the first launch does it.
- Prompts the PRD allows but doesn't need: container runtime, secrets manager, VPN.

## Affected areas

- `internal/cmd/reach.go` (new), `internal/cmd/root.go`
- `internal/reach/` (new): the journal of steps, the resume logic, the ordering
- `internal/onboard/`, `internal/cmd/onboard_wizard.go`: reuse the wizard's prompts
- `internal/workspace/scaffold.go`: manifest written first, in the protected location
- `internal/termopen/termopen.go`: the handover, and end-the-old-terminal
- `internal/manifest/`: `task:`, `shell:`, `other_providers:`, `Scope()`
- `internal/registry/`: register a clients directory
- `internal/bundle/`: pre-register the plugin
- `internal/policy/selfprotect.go`: `reach` and `edit` on the human-only list
- `docs/COMMANDS.md`, `docs/SETUP.md`, `README.md`, `CHANGELOG.md`, `docs/adr/0018-reach.md`
- `CLAUDE.md`: Must not trade off gains `TestReachDoesNotTrustFromAgent` and `TestReachConfirmsBeforeWriting`

## Do not touch

- `internal/cmd/add.go`, `internal/cmd/onboard.go`, `internal/cmd/setup.go`, `internal/cmd/init.go`: ticket 015 removes them, and their tests stay green until then.
- `plugin/skills/`: ticket 012.

## Decisions

- **Agreed with the owner**: `init` is deleted, so reach registers the clients directory.
- **Agreed with the owner**: one flag: reach writes `guard: v2` in every new manifest.
- **Agreed with the owner**: the browser prompt offers Chrome and Edge only.
- **Decided by the ticket writer**: the email provider prompt is an addition allowed by PRD §4.3, because §3.3 and §5.4 require an email provider and the table has no prompt for it. It defaults to Skip.
- **Decided by the ticket writer**: the typed `yes` signs at step 7. reach is the one signing path that needs no separate `tq allow`, per §4.4 step 7 and §8.4.
- **Decided by the ticket writer**: `Other` values are stored but out of scope, because a provider with no catalog entry can't be matched or isolated. `status --json` lists them as manual, and `tentaqles providers add` then `edit` brings one in.
- **Decided by the ticket writer**: the task prompt's Azure DevOps maps to `azure-devops`, and GitHub Projects maps to `gh`.
- **Decided by the ticket writer**: ending the old terminal means ending the parent shell process, only when reach is a direct child of one on a TTY. Otherwise it prints REQ-16's line, so nothing is killed that reach can't identify.
- **Decided by the ticket writer**: the steps journal lives in `$TQ_HOME/identities/<ws>/reach.json` and is removed on success.
- **Decided by the ticket writer**: the new terminal uses the preferred shell when `termopen` can start it, and otherwise the default.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 010` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
