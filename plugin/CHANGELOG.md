# Changelog

All notable changes to the Tentaqles plugin. Versions follow [semver](https://semver.org/).
Releases before 0.16.0 are described on the
[GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

- **`tq_hook.sh` no longer honours `TQ_BIN` (ADR-0004).** An agent can set a variable, and
  the tq that enforces must not be its choice: the binary is the plugin's own `bin/tq`, then
  PATH, then the well-known install dirs. The plugin folder, `bin/` included (not
  `skills/`, `agents/`, `templates/`, `tests/`, `__pycache__`), is covered by the signed
  integrity record, so a planted `bin/tq` is reported by `tq hooks verify`; a tq on PATH is
  not covered. The fail-closed fallback is unchanged
  (`tests/test_tq_off.py::test_tq_bin_is_not_honoured`).
- **The protected manifest location (ADR-0002).** `tentaqles.manifest.loader` resolves a
  root marker (`schema: tentaqles-marker-v1`) to `$TQ_HOME/identities/<name>/manifest.yaml`
  (`~/.tentaqles` when `TQ_HOME` is unset); `_manifest_path` is the file loaded and
  `_client_root` stays the folder; the old layout (the full manifest in the root) still
  loads; a marker whose `client` is not the folder name, or without its protected copy, is
  unknown. The loader resolves like tq: the directory must lie under a base registered in
  `$TQ_HOME/config.yaml` and the identity is that base's first-level folder, so a
  `.tentaqles.yaml` planted deeper never shadows it; with no `config.yaml` an agent
  session is neutral (tq would resolve "outside any base"), and the walk-up applies only
  to a plain process on a machine without tq. New helpers `protected_manifest_path`,
  `is_protected_manifest_path` and `workspace_root_for`
  (`tests/test_manifest_loader_marker.py`).
- **`TQ_HOME` is ignored under an agent marker** (`tentaqles.tqhome`, used by the loader and
  `tentaqles.knowledge`): the hooks read `~/.tentaqles`, so a settings `env` block cannot
  point them at a forged state folder. The tests set `tqhome.ALLOW_HOME_OVERRIDE`
  (`tests/conftest.py`); nothing else does.
- **`snapshot-guard.py` fires again.** It reads the written file from `tool_input.file_path`
  (the hook payload shape; the bare `file_path` stays as a fallback) and snapshots the
  manifest tq reads (the protected copy for a marked workspace) before a Write to the
  marker, the protected copy or an old-layout manifest. `scripts/snapshot.py capture`
  reads the same manifest (`tests/test_snapshot_guard.py`).
- **The fallback guard protects tq's state (ADR-0005).** With tq missing,
  `scripts/identity-guard.py` blocks a Write, Edit, MultiEdit or NotebookEdit under
  `~/.tentaqles` or the installed plugin (`<config dir>/plugins/cache/tentaqles`), a Read or
  Grep under `~/.tentaqles`, and a shell command that names either one (a text match on the
  expanded home), with `BLOCKED [self-protection/tq/protected-path]`
  (`tests/test_identity_guard.py`).
- **`client-settings` and `rollback` no longer write the manifest.** tq's hook denies a
  session's write to the manifest and the root marker; both skills now print the changed
  manifest and ask the user to write it and run `tq allow <name>`.
- **The PreToolUse matcher gains `Glob`** so a Glob into tq's state folder reaches the hook
  (guarded like `Grep`, ADR-0005); `tests/test_identity_guard.py` pins it.

## [0.16.0] — 2026-11-04

Ships with tq v0.9.0. The plugin adds, on top of tq's identity and secrets hook, the
per-client session memory, the identity preamble, the knowledge graph, 40 skills and five
read-only reviewer agents.

- **Hooks.** SessionStart (bootstrap, `tq claude-hook session-start`, the session
  preamble), PreToolUse (`tq claude-hook pre-tool-use`, the snapshot guard), PostToolUse
  (knowledge capture), Stop (stop capture) and SessionEnd (session end). `scripts/tq_hook.sh`
  and `scripts/tq_run.sh` fail closed when tq is missing and have no off switch: the off
  launchers (`claude-off`, `codex-off`, `tq run --off`) switch the plugin off through Claude's
  `--settings`; `TQ_ENABLED=0` keeps its one meaning, shell activation only
  (`tests/test_tq_off.py`).
- **Skills: 40.** The dev-process skills are plain checklists: `ship` (a self-assessed risk
  line and the reviewer agents on a high-risk diff), `fix`, `feature`, `kickoff`, `direction`,
  `dod`, `llm-security-review`, `privacy-review`, `classify-data` (writes `docs/privacy.md`).
  Human-only commands (`tq allow`, `tq deny`, `tq secret set`) are printed for the user,
  never run.
- **Agents: five, all read-only** (Read, Glob, Grep): `critic`, `spec-critic`,
  `security-reviewer`, `privacy-reviewer`, `llm-security-reviewer`; each reads the diff the
  caller names plus the spec slice, `CLAUDE.md`, `docs/LEARNINGS.md` and
  `docs/threat-model.md`, and answers with the review contract.
- **Templates.** `templates/process/CLAUDE-project.md` carries a prose checks line (test,
  lint, typecheck) that `dod` runs.
- **Privacy.** Everything the plugin stores or sends passes through `redact_str`: secrets and
  the checksummed identifiers (CPF, CNPJ, card, IBAN) are masked; reading personal data
  locally is never blocked.
- **Install.** From the root marketplace of `github.com/tentaqles/tentaqles`
  (`/plugin marketplace add tentaqles/tentaqles`, `/plugin install tentaqles@tentaqles`),
  pinned to a release tag and its commit.
