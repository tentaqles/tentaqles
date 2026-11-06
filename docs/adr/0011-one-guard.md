# ADR-0011: One guard, always on

Status: accepted
Date: 2026-11-06
Supersedes: the `guard: v2` flag of ADR-0007 and ADR-0008. Their rules stay; only the switch goes.

## Context

ADR-0007 and ADR-0008 built the provider-scope guard behind a manifest flag, `guard: v2`, so an
existing identity would not change on upgrade. The owner reset the product to `docs/IDEATION.md`
on 2026-11-06. It describes one guard: a client that works with AWS only cannot reach the Azure
CLI. With the flag, a client that lacks it can, and a new client depends on a line nobody should
have to know about.

## Decision

There is no flag. Every trusted workspace gets the identity PATH, the MCP filter and the
provider-scope rules. A `guard:` line in an older manifest still loads and means nothing, whatever
its value, so an old manifest does not start failing.

A manifest's declared cloud (`cloud.provider`) is always in the identity's scope and has its own
config folder, like `cloud.also` already did. Removing the flag exposed this: a manifest with
`cloud: { provider: azure }` and an `identities` block without `az` would have had its own cloud
denied.

## Consequences

- **Upgrading changes existing workspaces.** A tool the manifest does not declare is denied and
  masked. The way through is the manifest's `scope:` list and `tq allow`. This is why the owner's
  existing manifests get reviewed before the new build is installed.
- A client whose declared cloud was not in `identities` gets a new, empty config folder for that
  CLI, so it logs in again. Its old login stays where it was.
- The tests that pinned the flag-off behaviour are gone or rewritten, with the owner's approval:
  `TestPreToolUse_ScopeV1Unchanged` is now `TestPreToolUse_ManifestNeedsNoFlag`,
  `TestPreToolUse_GuardMappingIsV1` is now `TestPreToolUse_OldGuardKeysStillGuarded`, and
  `TestEnv_ScopeLayerRestoreAndV1` is now `TestEnv_ScopeLayerRestore`, without its flag-off half.
- `TestPreToolUse_UnreadableManifestDeniesAll` no longer has the `guard: v3` cases, because an
  unknown `guard:` value is no longer an error. Its other cases stay.
- `wrong-cloud` stays in `internal/guard` for a command judged with no trusted manifest.
