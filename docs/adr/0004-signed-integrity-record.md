# ADR-0004: A signed integrity record per workspace replaces the trust marker

Status: accepted
Date: 2026-11-05
Cites: PRD v2 §8.3 (integrity verification), §8.2 (protected paths), §8.1 and §8.4 (human-only changes), §6.4 (hook installation), §13 (nothing changes for an existing identity on upgrade); DISCOVERY.md §7 (signing key: keychain, plain-file fallback, decided 2026-11-05) and §9 (Phase 1); ADR-0003 (human-only detection); threat model boundaries B1 (the hook and the state folder), B2 (human-only actions), B8 (what reaches the machine)

## Context
PRD v2 §8.3 says every identity terminal start and every harness start MUST
verify the hashes of the hook scripts, the shims and the manifest against a
signed record written at `tq init` or the last human-approved change, refuse to
start the harness on a mismatch, print what changed, and require a human to run
`tq hooks verify --accept` on a TTY. The key is a per-user OS keychain entry
with a plain-file fallback the implementation notes call out (DISCOVERY.md §7,
decided 2026-11-05).

Until now trust was an empty marker file named after the manifest's hash
(ADR-0001, then `trust/<name>/<hash>` by ADR-0002). It covered the manifest
alone: the plugin's `hooks/hooks.json` and `scripts/tq_hook.sh` in the
identity's Claude config dir, the `hooks` and `env` of that identity's
`settings.json` and the Codex hook files were all outside it, although they are
what makes the hook run at all. A marker is also trivial to forge: any file tool
can create an empty file under `$TQ_HOME/trust/`. `tq run .` in an untrusted
folder warned and launched Claude in bypass with no identity, the opposite of
"refuse to start the harness". The plugin's hook bridge honoured `$TQ_BIN`, an
environment variable any session can set, to choose which binary enforces.

## Decision
**The record.** We replace the marker with a signed integrity record at
`$TQ_HOME/trust/<name>.json` (`trust.SignedRecordPath`): schema
`tentaqles-integrity-v1`, the workspace name, the manifest hash resolve read, a
map of covered key → sha256 (`""` records an absent file), the key of the plugin
folder covered, the id of the signing key, `signed_at`, a `revoked` flag, and an
HMAC-SHA256 signature over the canonical JSON of everything else (every field in
a fixed order, map keys sorted, no whitespace: re-indenting the file changes
nothing and any change to a value fails).

**What it covers** (`trust.Covered`): the protected manifest; the root marker
(in the old layout, the manifest itself); `settings.json#tq`, the canonical JSON
of the keys of the identity's Claude `settings.json` that decide what runs
(`hooks`, `enabledPlugins`, `enabledMcpjsonServers`, `disableAllMcpServers`,
`env`, `statusLine`), so Claude Code's own rewrites of the theme or the model do
not trip it; every file under `hooks/`, `scripts/` and `.claude-plugin/` of the
tentaqles plugin installed in the identity's Claude config dir (the highest
installed version, the rule of `plugin/CLAUDE.md`); the Codex `hooks.json` and
`config.toml#tq`, the block of `config.toml` tq owns (`codexcfg.ManagedBlock`);
and every file under `$TQ_HOME/shims/` once it exists (Phase 3 creates it). The
tq binary is not covered: §8.3 names the hook scripts, the shims and the
manifest; which tq a shell runs is Phase 3's shim folder, and the binary is the
OS's to protect. A covered path that does not exist is recorded as absent; it
appearing later is a change, and so is a file swapped for a link or a directory.

**Keys are canonical** (`trust.Key`): absolute and cleaned, the symlinks of the
longest existing prefix resolved, forward slashes, lower-cased on Windows. A
record signed from a shell whose `TQ_HOME` is typed in another case, or with
`C:/` for `C:\`, verifies from any other spelling with no phantom change.

**The key** is 32 random bytes in the OS keychain through go-keyring, service
`tentaqles` (the service `internal/secret` uses), account `integrity-key`,
generated the first time a human signs; verifying never creates one. When the
keychain is unavailable the key is the file `$TQ_HOME/integrity.key` (0600, hex)
and `tq doctor` warns `integrity-key-file` while it exists. A key never changes
silently: an existing one, in either place, is always used. No error, output or
log line names the key; `tq hooks verify` prints its id (the first 12 hex
digits of its sha256). The keychain does not bind the key to anything a process
of the same user lacks: any same-user process can read or replace the entry
(`cmdkey`, `security add-generic-password`, `secret-tool store`), and with the
key it can sign a record. That is the accepted risk AR1 of the threat model. Two
things narrow it: the P1.3 hook rule denies, in a session, the command lines
that write a keychain entry naming the `tentaqles` service; and the key id in
every record makes a key swapped without re-signing visible as "integrity key
changed (the record was signed with key X, the key now is Y)" instead of an
opaque signature failure.

**The verdict.** `trust.IsTrusted(name, hash)` keeps its signature and means:
the signed record exists, is not revoked, was signed with the key in use, its
signature verifies, its manifest hash equals `hash`, and every covered file
hashes as recorded. `trust.Check` gives the kind (trusted, legacy, untrusted,
mismatch) and the reason ("untrusted (run: tq allow <name>)", "untrusted: trust
revoked by tq deny (run: tq allow <name>)", "untrusted: integrity mismatch,
changed <key> (run: tq hooks verify <name>)", "untrusted: integrity record
signature invalid (run: …)", "untrusted: integrity key changed (…)");
`trust.Verify(name)` returns the report with every change (key, kind
changed/added/removed, old and new hash) and, when the plugin moved to another
version folder, the two versions and how many hook files differ between them.
Within a command (`trust.MemoScope`, opened by the hook, `tq env`, `tq run`,
`tq doctor` and the status line) each workspace is verified once; every
signature clears the memo; nothing is cached on disk.

**Every signature is human-only, at the lowest layer.** `trust.SignRecord`,
`trust.Deny` and the legacy snapshot pass `trust.HumanCheck`, which is
`humanonly.Check` (ADR-0003: the TTY, process-tree and environment-marker
detectors, a refusal logged): whatever command reaches a signature (`tq allow`,
`tq hooks verify --accept`, `tq add`, `tq setup` and the scaffold's `Trust`,
the re-sign after `tq settings render` or `tq bundle sync`, `tq migrate
manifests`), an agent session cannot have tq sign. The commands keep their own
gate on top: `tq allow` and `tq hooks verify --accept` confirm through
`humanGate` after printing what they are about to sign. `tq allow` renders the
identity files first, for a workspace not yet trusted too, and signs once, over
the files as rendered.

**Revocation is a tombstone.** `trust.Deny` rewrites the record as a signed
`revoked: true` record with no files; tq never deletes a record.

**Legacy markers.** The markers of older versions (`trust/<hash>`,
`trust/<name>/<hash>`) are empty files anyone can create, so they cannot count
forever. Before anything is signed on the machine (no key and no list) every
marker counts, as for the tq that wrote it (§13). The first signature takes a
signed snapshot of the markers then on disk, `trust/legacy.json`; from then on
a marker counts only while the snapshot lists it and verifies, and never for a
workspace that has a signed record file (valid, revoked or broken). Every
signature or revocation of a workspace removes its markers and drops them from
the snapshot; `tq migrate manifests --apply` replaces each marker that counts by
a signed record (`rekey-trust`, the snapshot rewrite journaled with it, so
`tq uninstall --restore` makes the markers count again). A marker planted after
the snapshot, or a deleted record with a marker planted in its place, is
untrusted; a missing or forged snapshot makes no marker count (fail closed).
While a workspace is on a counting marker, `tq doctor` warns
`integrity-record-legacy` and every prompt prints "tq: legacy trust record: run
tq allow <ws> to sign hooks and manifest".

**Verification points.** `resolve` (every command and the hook) reports a
mismatch as untrusted with the reason and a `Mismatch` flag. The hook treats a
mismatch at least as strictly as a workspace never allowed: doctor reports
`untrusted` with `integrity`, so the guard refuses every mutating git, and a
remote mutation (`git push`, `gh`, a cloud CLI) is judged as from no workspace,
refused with the reason naming the first changed file. `tq env`, at every
prompt, withholds the identity env and prints one line, fixed words plus the
workspace name when it matches `workspace.NameRe` (never a path or anything a
folder name or a manifest holds; `tq hooks verify` prints the paths): "tq:
integrity mismatch: run tq hooks verify <ws>", "tq: untrusted: run tq allow
<ws>" for a workspace never allowed or revoked, and the legacy line above. `tq
run <ws>` and `tq run .` refuse with the reason and launch nothing. The
session-start preamble and `tq doctor` report `integrity` (error) listing every
change, with the plugin upgrade summary first. `tq hooks verify [<ws>]` prints
the report ("plugin tentaqles 0.16.0 -> 0.17.0 (N hook files changed)" ahead of
the per-file lines when the plugin moved) and exits 1 on a mismatch; a
re-sign of `tq migrate manifests` happens only for a record that verifies in
full, so a move never accepts a change.

`plugin/scripts/tq_hook.sh` no longer honours `$TQ_BIN`: the binary is the
plugin's own `bin/tq`, then PATH, then the well-known install dirs.

## Consequences
- A hook, an enforcing settings key, a Codex hook file or a shim changed behind
  the human's back makes the workspace untrusted everywhere at once: no identity
  in the shell, no harness launch, no mutating git or remote command from the
  hook, a line at every prompt and in the preamble, until the human reads
  `tq hooks verify` and accepts.
- A record cannot be forged with a file tool, and a signed workspace cannot be
  downgraded to a marker; a same-user program that holds the key can sign (AR1).
- Updating the plugin (a new version folder in the cache) changes covered files:
  the human runs `tq hooks verify --accept <ws>` once per workspace after the
  summary line. Upgrading tq itself does not. `docs/MIGRATE.md` says so.
- `tq run .` in an untrusted folder no longer launches anything; a neutral
  folder (no workspace) still launches the agent without an identity.
- Nothing changes for an existing identity on upgrade (§13): its marker keeps
  counting, with a doctor warning and the prompt line, until the human signs.
- We must keep: `TestSignedRecordRoundTrips`, `TestBinaryIsNotCovered`,
  `TestFlippedByteFailsVerification`, `TestOnlyValuesAreSigned`,
  `TestKeyChangeIsReportedByID`, `TestTrustIsKeyedByWorkspaceName`,
  `TestEditedManifestIsNamed`, `TestPluginHookChangesAreNamed`,
  `TestPluginVersionChangeIsSummarised`, `TestAbsentCoveredFileAppearingIsAChange`,
  `TestSettingsEnforcingKeysOnly`, `TestCodexBlockIsCoveredNotTheRest`,
  `TestShimsAreCovered`, `TestKeysAreCanonical`, `TestPluginRootPicksHighestVersion`,
  `TestKeychainUnavailableFallsBackToFile`, `TestKeySources`,
  `TestSigningRefusesWithoutAHuman`, `TestDenyLeavesASignedTombstone`,
  `TestLegacyMarkersCountOnlyFromTheSnapshot`, `TestVerifyOncePerScope`,
  `TestInvalidKeysAreNeverTrusted`, `TestResolve_IntegrityMismatchIsUntrustedWithReason`,
  `TestPreToolUse_IntegrityMismatchDeniesRemoteAndGit`,
  `TestPreToolUse_VerifiesOncePerCall`, `TestRun_RefusesOnIntegrityMismatch`,
  `TestRunDotUntrustedRefuses`, `TestEnv_NoIdentityAndOneNoticeOnMismatch`,
  `TestEnv_NeverTrustedPrintsANotice`, `TestEnv_LegacyTrustPrintsANotice`,
  `TestLegacyDowngradeIsUntrusted`, `TestEmit_NoticeLinePerShell`,
  `TestNoticeNeverCarriesAnUnvalidatedName`, `TestHooksVerify_ReportExitAndAccept`,
  `TestHooksVerify_SummarisesAPluginUpgrade`, `TestSessionStart_PrintsIntegrityMismatch`,
  `TestAllow_RendersThenSignsANeverTrustedWorkspace`,
  `TestSettingsRender_ResignsForAHuman`, `TestSigningRefusesUnderAHarnessProcessTree`,
  `TestRun_IntegrityListsTheChangedFiles`, `TestRun_IntegrityKeyFileWarns`,
  `TestRun_IntegrityLegacyRecordWarns`, `TestRunForCwd_UntrustedIsStructural`,
  `TestManifests_MovesJournalsAndKeepsTrust`, `TestManifests_RekeysLegacyTrustRecords`,
  `TestManifests_DoesNotResignAFailingRecord`, and in the plugin
  `test_tq_bin_is_not_honoured`.

## Alternatives considered
| Option | Why not |
|---|---|
| Keep the empty marker and add a hook rule that denies writes under `$TQ_HOME/trust/` | A text filter (AR2): a redirect, a script or an editor outside the session still creates the file; §8.3 asks for a signed record |
| Sign with an asymmetric key (Ed25519) | The verifier and the signer are the same user on the same machine; a public key buys nothing here and the private key needs the same keychain. HMAC-SHA256 is in the standard library and the record stays one small file |
| Store the key in a file only | A file under `$TQ_HOME` is one more file a file tool in a session can read; a keychain entry is not reachable through a file path, and writing one takes a command line the P1.3 rule denies. Neither stops a same-user program (AR1). The file is the fallback the PRD allows, and doctor reports it |
| Cover the tq binary | §8.3 names hooks, shims and manifest; hashing a 10 MB binary at every prompt is waste, and which tq runs is decided by the shim folder of Phase 3 and protected by the OS |
| Cover the whole `settings.json` | Claude Code rewrites it for a theme, a model or a permission; the record would trip on every session. The enforcing keys are what makes a hook run |
| Delete the record on `tq deny` | A deleted record is indistinguishable from one never written, and a planted marker would count again; the tombstone and the signed legacy snapshot make deletion untrusted |
| Let a marker count until a signed record exists for that workspace | An agent deletes the record and plants a marker; without the snapshot nothing tells the two apart |
| Interpolate the changed path into the prompt line | A path is user-controlled text in a line the shell evaluates; quoting per shell is one bug away from injection. The line is fixed words and a validated name; the paths are in `tq hooks verify` |
| Cache the verification result on disk | A cache is one more file to forge; hashing a dozen small files per prompt is cheap, and within one command the memo verifies once |
| Keep `$TQ_BIN` for tests | The tests put a fake `tq` on PATH instead; an agent-settable override of the enforcing binary has no place in the hook |
