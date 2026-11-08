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

- **Amendment (2026-11-08): an existing client's menu.** IDEATION says reach "will check if that folder
  already has a tentaqles identity and depending on that, the menu options may differ". In a folder that is
  a trusted client whose manifest is in the protected location and whose integrity record matches, reach
  offers: nothing (the default), the providers, the browser and profile, or the email. The chosen part of
  the same form opens with the current answers. `reach.Edit` changes only the keys the form owns
  (`git.email`, `git.provider`, the identities of providers chosen or unchosen, `cloud.provider` and
  `cloud.also`, the form's ids in `scope:`, `email.provider`, `browsers:`, `other_providers:`) in the YAML
  node tree, so a hand-written block, key or comment stays, and checks the result with `manifest.Parse`.
  Nothing is written before the typed `yes` of `humanGate`, whose text lists every change. Then the
  manifest is replaced atomically, and `signWorkspace` (the path of `tq allow`) renders and signs again:
  there is no new way to trust. A provider taken out loses its identity entry, so it leaves the scope, and
  its folder stays with any login in it; reach says so. An untrusted client, a mismatched integrity record
  and an unmigrated manifest get no menu: re-signing those is `tq allow`'s or `tq hooks verify --accept`'s
  decision, made knowingly, not a side effect of an edit. The refusal of an agent comes first, as for a new
  client. Tests: `TestReach_ExistingClientChangesProviders`, `TestReach_ExistingClientChangesEmail`,
  `TestReach_ExistingClientChangesBrowser`, `TestReach_ExistingClientDeclinedChangesNothing`,
  `TestReach_ExistingClientNoChange`, `TestReach_UntrustedClientHasNoMenu`,
  `TestReach_ExistingClientRefusesAgent`, and `internal/reach`'s `TestEdit_*`.

## Consequences

- `tentaqles` and `tq` are built from two mains. The release config and the installers shipped only
  `tq`; since ADR-0021 they ship both, the same bytes.
- A protected-path rule knew only the running binary, not its sibling under the other name. Since
  ADR-0021 both names are protected beside each other.
- CLI install, the terminal handover, and browser profile enforcement are not part of this change.
  `browsers:` is recorded in the manifest, and nothing reads it yet.
- A failure after the manifest is written leaves an untrusted client. A second `reach` reports it
  and names the fix. There is no automatic resume yet.
