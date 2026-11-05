# ADR-0004: A signed integrity record per workspace replaces the trust marker

Status: accepted
Date: 2026-11-05
Cites: PRD v2 §8.3 (integrity verification), §8.2 (protected paths), §8.4 (what a human must do), §6.4 (hook installation); DISCOVERY.md §7 (signing key: keychain, plain-file fallback, decided 2026-11-05) and §9 (Phase 1); threat model boundaries B1 (the hook and the state folder), B2 (human-only actions), B8 (what reaches the machine)

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
identity's Claude config dir, that identity's `settings.json`, the Codex hook
files and the tq binary itself were all outside it, although they are what
makes the hook run at all. A marker is also trivial to forge: any file tool can
create an empty file under `$TQ_HOME/trust/` (threat model B1, "writing the
marker file ... is not caught"). And `tq run .` in an untrusted folder warned
and launched Claude in bypass with no identity, which is the opposite of
"refuse to start the harness". The plugin's hook bridge honoured `$TQ_BIN`, an
environment variable any session can set, to choose which binary enforces.

## Decision
We replace the marker with a signed integrity record at
`$TQ_HOME/trust/<name>.json` (`trust.SignedRecordPath`): schema
`tentaqles-integrity-v1`, the workspace name, the manifest hash resolve read,
a map of covered path → sha256 (`""` records an absent file), `signed_at`, and
an HMAC-SHA256 signature over the canonical JSON of everything else (fields in
a fixed order, map keys sorted, no whitespace, so re-indenting the file changes
nothing and any change to a value fails). The covered paths (`trust.Covered`)
are: the protected manifest, the root marker (in the old layout, the manifest
itself), the identity's Claude `settings.json`, every file under `hooks/`,
`scripts/` and `.claude-plugin/` of the tentaqles plugin installed in the
identity's Claude config dir (the highest installed version, the rule of
`plugin/CLAUDE.md`), the Codex `hooks.json` and the tq-owned block of its
`config.toml` (`codexcfg.ManagedBlock`, recorded under the virtual path
`config.toml#tq`), every file under `$TQ_HOME/shims/` when it exists (Phase 3
creates it), and the running tq binary (`os.Executable`, symlinks resolved).
A covered file that does not exist is recorded as absent; it appearing later
is a change, and so is a file swapped for a link or a directory.

The key is 32 random bytes in the OS keychain through go-keyring, service
`tentaqles` (the service `internal/secret` uses), account `integrity-key`,
generated on first use. When the keychain is unavailable the key is the file
`$TQ_HOME/integrity.key` (0600, hex) and `tq doctor` warns `integrity-key-file`
while it exists; a key never changes silently (the file is used before a new
key is generated, the keychain entry wins when both exist). No error, output or
log line names the key.

`trust.IsTrusted(name, hash)` keeps its signature and now means: the signed
record exists, its signature verifies, its manifest hash equals `hash`, and
every covered file hashes as recorded. `trust.Check` gives the reason
("untrusted (run: tq allow <name>)" with no record, "untrusted: integrity
record signature invalid (run: tq hooks verify <name>)", "untrusted: integrity
mismatch, changed <path> (run: tq hooks verify <name>)") and whether it is a
mismatch; `trust.Verify(name)` returns the report with every change (path,
kind changed/added/removed, old and new hash). `trust.Allow(name, hash)` signs
the current state; `trust.Deny` removes the record and every legacy marker. The
markers of older versions (`trust/<hash>`, `trust/<name>/<hash>`) still count
for a workspace that has no signed record, until `tq allow` or
`tq migrate manifests --apply` (`rekey-trust`, journaled, restorable) replaces
them; `tq doctor` warns `integrity-record-legacy` meanwhile. The manifests
migration also signs again the record of a workspace it moves
(`sign-integrity`, the old record backed up), since the marker and the
protected copy are covered files.

Verification points: `resolve` (every command and the hook), so `tq env` at
every prompt withholds the identity env on a mismatch and emits one line the
shell prints on stderr, naming the first changed file and `tq hooks verify`, as
direnv nags until `direnv allow`; `tq run <ws>` and `tq run .` refuse with the
same reason and launch nothing; the session-start preamble and `tq doctor`
report `integrity` (error) listing every changed file; `tq hooks verify [<ws>]`
prints the report and exits 1 on a mismatch. Only a human signs:
`tq allow` and `tq hooks verify --accept` refuse an agent session
(`refuseAgent`; P1.2 adds the TTY and confirmation gate). `tq allow` renders the
identity files and then signs, so the rendered `settings.json` and Codex files
are covered; `tq settings render` and `tq bundle sync` sign again after writing
a covered file for a workspace that was trusted, outside an agent session only;
in a session the record is left stale and the human is told what to run.

Verification is cheap on purpose: only the covered files are hashed, nothing
is cached on disk, and the one large file (the tq binary) keeps its hash in an
in-process memo keyed by size and mtime.

`plugin/scripts/tq_hook.sh` no longer honours `$TQ_BIN`: the binary is the
plugin's own `bin/tq`, then PATH, then the well-known install dirs, and the one
that runs is a covered file of the record.

## Consequences
- A hook, a settings file, a shim or a tq binary changed behind the human's
  back makes the workspace untrusted everywhere at once: no identity in the
  shell, no harness launch, a line at every prompt and in the preamble, until
  the human reads `tq hooks verify` and accepts.
- A record cannot be forged with a file tool: it needs the key, which lives in
  the keychain (or in the 0600 fallback file, which is threat model AR1 and
  reported by doctor).
- Upgrading tq, updating the plugin (a new version folder in the cache) or
  changing `settings.json` through Claude Code changes a covered file: the
  human runs `tq hooks verify --accept <ws>` once per workspace after reading
  the report. `docs/MIGRATE.md` says so.
- `tq run .` in an untrusted folder no longer launches anything; a neutral
  folder (no workspace) still launches the agent without an identity, as
  before.
- Nothing changes for an existing identity on upgrade (PRD §13): a legacy
  marker keeps counting, with a doctor warning, until the human signs.
- We must keep: `TestSignedRecordRoundTrips`, `TestFlippedByteFailsVerification`,
  `TestOnlyValuesAreSigned`, `TestRecordSignedWithAnotherKeyFails`,
  `TestTrustIsKeyedByWorkspaceName`, `TestEditedManifestIsNamed`,
  `TestPluginHookChangesAreNamed`, `TestAbsentCoveredFileAppearingIsAChange`,
  `TestBinaryChangeIsNamed`, `TestCodexBlockIsCoveredNotTheRest`,
  `TestShimsAreCovered`, `TestPluginRootPicksHighestVersion`,
  `TestKeychainUnavailableFallsBackToFile`, `TestKeySources`,
  `TestLegacyRecordsCountUntilResigned`, `TestInvalidKeysAreNeverTrusted`,
  `TestResolve_IntegrityMismatchIsUntrustedWithReason`,
  `TestRun_RefusesOnIntegrityMismatch`, `TestRunDotUntrustedRefuses`,
  `TestEnv_NoIdentityAndOneWarningOnMismatch`, `TestEmit_WarnLinePerShell`,
  `TestHooksVerify_ReportExitAndAccept`, `TestSessionStart_PrintsIntegrityMismatch`,
  `TestAllow_SignsAfterRenderingIdentityFiles`,
  `TestSettingsRender_ResignsForHumanNotAgent`,
  `TestRun_IntegrityListsTheChangedFiles`, `TestRun_IntegrityKeyFileWarns`,
  `TestRun_IntegrityLegacyRecordWarns`, `TestRunForCwd_UntrustedIsStructural`,
  `TestManifests_MovesJournalsAndKeepsTrust`,
  `TestManifests_RekeysLegacyTrustRecords`, and in the plugin
  `test_tq_bin_is_not_honoured`.

## Alternatives considered
| Option | Why not |
|---|---|
| Keep the empty marker and add a hook rule that denies writes under `$TQ_HOME/trust/` | A text filter (AR2): a redirect, a script or an editor outside the session still creates the file; §8.3 asks for a signed record |
| Sign with an asymmetric key (Ed25519) | The verifier and the signer are the same user on the same machine; a public key buys nothing here and the private key needs the same keychain. HMAC-SHA256 is in the standard library and the record stays one small file |
| Store the key in a file only | Any program running as the user reads it (AR1); the keychain binds it to the user's session. The file is the fallback the PRD allows, and doctor reports it |
| Cover the whole Claude config dir and the whole plugin | The config dir holds credentials and session state that change on every run; the plugin's skills and memory code do not enforce anything. Coverage is the files the hook depends on, as the owner decided |
| Cache the verification result on disk | A cache is one more file to forge; hashing a dozen small files and one memoized binary per prompt is cheap |
| Let `tq settings render` sign in an agent session too (it only writes tq's own content) | A session could race the render against an edit of a covered file; signing stays a human act, the session is told what the human runs |
| Keep `$TQ_BIN` for tests | The tests put a fake `tq` on PATH instead; an agent-settable override of the enforcing binary has no place in the hook |
