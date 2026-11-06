# ADR-0012: `tentaqles reach`, and the guard knows the name

Status: accepted
Date: 2026-11-06
Cites: `docs/IDEATION.md` (the spec), ADR-0003 (human-only), ADR-0004 (signed record), ADR-0005 (self-protection)

## Context

IDEATION names one way to add a client: `tentaqles reach [<path>]`. The hook's human-only match
only knew the binary name `tq`, and had no `reach` verb. An agent running `tentaqles allow` or
`tq reach` would have passed the hook.

## Decision

- `reach` is human-only, in the command and in the hook. It is refused before it prints or asks
  anything.
- The hook treats `tentaqles` as it treats `tq`: bare, with `.exe`, by path, and through
  `go run ./cmd/tentaqles`.
- `reach` signs through `signWorkspace`, the path `tq allow` uses, after one typed `yes`. There is no
  new way to trust a folder, and no separate `tq allow` step for a new client.
- A folder's name is its identity's name, and its parent is registered as a clients folder. A name
  already used by a client in another folder is refused, because two folders would share one
  manifest, one set of credentials and one trust record.
- Multi-select answers are written to the manifest's `scope:` list. A name typed under "Other" goes
  to `other_providers` and is never in the scope: tq has no catalog entry to isolate or guard it, and
  `reach` says so.

## Consequences

- `tentaqles` and `tq` are built from two mains. The release config and the installers still ship
  only `tq`, and they change when the release is cut.
- A protected-path rule still knows only the running binary, not its sibling under the other name.
  That gets fixed when both ship together.
- CLI install, the terminal handover, and browser profile enforcement are not part of this change.
  `browsers:` is recorded in the manifest, and nothing reads it yet.
- A failure after the manifest is written leaves an untrusted client. A second `reach` reports it
  and names the fix. There is no automatic resume yet.
