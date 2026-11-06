# 011: `tentaqles edit`

**Status:** ready
**Risk:** High: edit changes an identity's scope and re-signs its trust, so a bug can widen a client's scope with no real confirmation, or lose a hand-written `blocked_commands` entry
**UI:** none

PRD v2 §4.2 and §8.4. The human's way to change an existing identity: re-run only the prompts they select, confirm, and get the identity's config, guard layers and signed record updated. It is also how an existing identity turns on `guard: v2`.

## Requirements

- **REQ-1**: `tentaqles edit [<name>]` must act on the named identity or the current folder's.
- **REQ-2**: A folder that is not an identity must refuse with `<folder> is not an identity: use tentaqles reach` and exit 1.
- **REQ-3**: edit is human-only: under any `internal/humanonly` detector it must refuse and log before reading any answer.
- **REQ-4**: On a terminal, edit must print the banner, unless `--no-banner` or `TQ_NO_BANNER=1`, then show a multi-select of the prompts of `reach`, plus `Guard v2`, and re-run only those selected.
- **REQ-5**: Each re-run prompt must show the current value as its default.
- **REQ-6**: Before writing, edit must print the change as a list, one line per changed field, marking providers added to and removed from the scope, and ask for a typed `yes`. Any other answer must change nothing.
- **REQ-7**: With no change, edit must print `No change.` and write nothing.
- **REQ-8**: Edit must keep every manifest field it did not change, including `blocked_commands`, `claude.env`, `claude.bundle`, `mcp`, `scope` and `other_providers`, and any block it doesn't know.
- **REQ-9**: After the `yes`, edit must write the manifest, run the render that `tq settings render` runs, and update the signed record, so the identity is trusted when it returns.
- **REQ-10**: A provider added to the scope must get its CLI line from ticket 008's install, unless `--no-install`, and a failed install must not stop edit.
- **REQ-11**: A provider removed from the scope must leave the PATH, the MCP list and the hook's scope on the next prompt and session. Its credentials on disk are not touched, and edit must print where they stay.
- **REQ-12**: Selecting `Guard v2` on an identity without it must set `guard: v2`, naming the behaviours it turns on before the `yes`. Clearing it on an identity that has it must do the reverse and warn that the scope layers go.
- **REQ-13**: A step failing after the `yes` must print `edit stopped at <step>: <reason>`, leave the signed record as it was before the edit, so the identity reads untrusted if the manifest changed, and tell the human to run `tq hooks verify` and re-run edit.
- **REQ-14**: The identity's folder name and `client` must not change through edit.

## Out of scope

- Renaming an identity.
- Removing an identity. `tq deny` and `tq uninstall` remain.
- Non-interactive editing: a `--from` for `edit` isn't in the PRD.
- Deleting an out-of-scope provider's credentials.
- Starting a new terminal. Edit runs in the terminal it was called from.

## Affected areas

- `internal/cmd/edit.go` (new), `internal/cmd/root.go`
- `internal/reach/`: reuse the prompts
- `internal/manifest/`: a write that keeps unknown keys and comments
- `internal/policy/selfprotect.go`: `edit` is already listed, and gets a test
- `docs/COMMANDS.md`, `docs/SETUP.md`, `CHANGELOG.md`

## Do not touch

- `internal/trust/`: use its signing API.
- `plugin/skills/`: ticket 012.

## Decisions

- **Agreed with the owner**: one flag: `edit` is how an existing identity turns on `guard: v2`.
- **Decided by the ticket writer**: `Guard v2` is an added item in the multi-select, because the PRD's `reach` always sets it and an existing identity needs a human path to it.
- **Decided by the ticket writer**: a removed provider's stored credentials stay, because deleting a login can't be undone, and the owner has a no-rotation stance. The hook still denies them.
- **Decided by the ticket writer**: comments in the manifest survive where the YAML library allows, using a node-level edit. A lost comment is not a failure.
- **Decided by the ticket writer**: no new ADR: this reuses reach's and `tq settings render`'s boundaries, and doesn't touch a trust-boundary path except for a test on the existing list.

## Definition of done

- Every check in [eval.md](eval.md) passes on the final tree: `node .claude/hooks/gate.mjs check 011` exits 0.
- Nothing under Out of scope was built, and nothing under Do not touch changed.
