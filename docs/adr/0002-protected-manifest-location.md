# ADR-0002: The manifest lives under ~/.tentaqles; the identity root keeps a name-only marker

Status: accepted
Date: 2026-11-05
Cites: PRD v2 §8.2 (protected paths), §8.3 (integrity record), §13 (constraint: nothing changes for an existing identity on upgrade); DISCOVERY.md §2 (term map) and §7 (owner decision of 2026-11-05); threat model boundaries B1 (the hook and the state folder), B2 (human-only actions)

## Context
PRD v2 §8.2 says the Tentaqles config area, the manifests and the trust stores MUST
live outside every identity root and every project folder. v1 kept the manifest at
`<root>/.tentaqles.yaml`, inside the folder the agent works in: any file tool in a
session can rewrite it. Trust by hash (ADR-0001, `internal/trust`) makes such an edit
visible, since the workspace turns untrusted until a human runs `tq allow`, but it does
not keep the manifest out of reach, and the hook has no rule for the file (threat model
AR2). The repo owner decided on 2026-11-05 to follow §8.2 with a name-only marker left in
the folder, the guard reading only the protected copy.

Term map (DISCOVERY.md §2): PRD identity = code workspace (one first-level folder under a
registered base); PRD identity root = workspace root; PRD manifest = `.tentaqles.yaml`;
the PRD trust store (credentials) is the OS keychain, while the v1 "trust" is the manifest
hash record under `$TQ_HOME/trust/`.

## Decision
We keep the manifest of workspace `<name>` at `$TQ_HOME/identities/<name>/manifest.yaml`
(`paths.ManifestPath`; `$TQ_HOME` defaults to `~/.tentaqles`). The workspace root keeps
`.tentaqles.yaml` as a marker holding exactly `schema: tentaqles-marker-v1` and
`client: <name>` plus the "names only" comment (`manifest.MarkerBytes`).

Resolution (`resolve.locate`) is fail-closed: the folder must be a first-level child of a
base in `$TQ_HOME/config.yaml`, the marker's `client` must equal the folder name
(case-insensitively on Windows), and the protected copy must exist; anything else
resolves neutral with a reason. A root file that is a full manifest (schema
`tentaqles-client-v1|v2`) is the old layout and is read from the root exactly as before;
when a protected copy exists as well, the protected copy is read and `tq doctor` warns
`manifest-root-stale`. Every command and the hook go through `resolve`, so nothing reads
a manifest from an identity root once it is marked.

Trust does not change: `trust.IsTrusted`, `Allow` and `Deny` over the sha256 of the file
tq reads, with the same API, so `tq allow` and `tq deny` keep working. Editing the marker
never changes that hash; a marker that names another workspace resolves neutral.

`tq add`, `tq onboard` and `tq setup` create the new layout. An existing identity keeps
the old layout until the human runs `tq migrate manifests --apply` (also
`tq migrate --steps manifests`; never a default step; `--apply` refuses an agent session):
the manifest is copied byte for byte into the protected location, so its hash and
therefore its trust carry over unchanged, the root file is rewritten as the marker, both
writes are journaled and `tq uninstall --restore` reverses them. `tq doctor` warns
`manifest-in-root` with that command as the fix.

The protected area is created private to the user: 0700 directories and 0600 files on
Unix; on Windows `internal/protect` runs
`icacls <home> /inheritance:r /grant:r "<user>:(OI)(CI)F"` once, from `tq init`,
`tq add` and `tq migrate manifests --apply`, records the outcome at
`$TQ_HOME/protected-acl.json`, and `tq doctor` reports it as `protected-acl`. This is
defense in depth: an icacls failure is reported, never a command failure.

The plugin's loader (`plugin/tentaqles/manifest/loader.py`) walks up to the root file as
before and resolves a marker to the protected copy under `$TQ_HOME`; `_manifest_path`
names the file loaded and `_client_root` stays the folder. `snapshot-guard.py` snapshots
the copy tq reads before a Write to the marker, the protected copy or an old-layout file.

## Consequences
- The guard reads nothing from an identity root but the workspace name. The file the
  hook enforces against is in the state folder, which the hook rule of P1.3 will cover.
- Nothing changes for an existing identity on upgrade (PRD §13): the old layout resolves
  as before, with a doctor warning; the layout changes only when the human migrates it.
- P1.4 replaces the hash-file trust record with a signed record over the manifest, the
  hooks and the shims; the `trust` API stays as it is here so that change is local.
- A workspace folder copied or cloned to another machine no longer carries its manifest:
  `tq doctor` and `tq list` name the missing protected copy (`marker without a protected
  manifest`), and `tq add` refuses to scaffold over a leftover protected copy.
- Any same-user program can still write the state folder (threat model AR1); the Windows
  ACL and the Unix modes narrow that to the user's own account only.
- We must keep: `TestResolve_MarkerResolvesToProtectedManifest`,
  `TestResolve_MarkerClientMismatch_Neutral`,
  `TestResolve_MarkerWithoutProtectedManifest_Neutral`,
  `TestResolve_OldLayoutStillResolves`, `TestResolve_BothFiles_ProtectedWinsAndRootIsStale`,
  `TestResolve_EditedProtectedManifestLosesTrust_MarkerEditDoesNot`,
  `TestEditedManifestLosesTrust` (both layouts), `TestAdd_CreatesMarkerAndProtectedManifest`,
  `TestManifests_MovesJournalsAndKeepsTrust`, `TestManifests_RestorePutsTheManifestBack`,
  `TestMigrateManifests_ApplyRefusesAgents`,
  `TestEnsureHome_ICACLSFailureIsRecordedNeverReturned`, and in the plugin
  `tests/test_manifest_loader_marker.py` and the marker cases of
  `tests/test_snapshot_guard.py`.

## Alternatives considered
| Option | Why not |
|---|---|
| Keep the manifest in the root and add a hook rule that denies writes to it | The rule is a text filter (AR2): a shell redirect, a script or an editor outside the session still reaches the file; §8.2 asks for the location, not a filter |
| No marker: resolve folders from `config.yaml` and the protected area alone | A plain directory under a base could not be told from a workspace whose protected copy is missing, and the plugin's walk-up from any subfolder would need the registry on every hook call |
| Convert every manifest on upgrade | Breaks the §13 constraint; the human runs `tq migrate manifests` per machine, and `tq uninstall --restore` undoes it |
| Hash the marker together with the manifest | The marker holds nothing the guard reads; a changed marker is already a mismatch that resolves neutral |
