# Changelog

All notable changes to the Tentaqles plugin. Versions follow [semver](https://semver.org/).
Releases before 0.16.0 are described on the
[GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

- **The protected manifest location (ADR-0002).** `tentaqles.manifest.loader` resolves a
  root marker (`schema: tentaqles-marker-v1`) to `$TQ_HOME/identities/<name>/manifest.yaml`
  (`~/.tentaqles` when `TQ_HOME` is unset); `_manifest_path` is the file loaded and
  `_client_root` stays the folder; the old layout (the full manifest in the root) still
  loads; a marker whose `client` is not the folder name, or without its protected copy, is
  unknown. New helpers `protected_manifest_path`, `is_protected_manifest_path` and
  `workspace_root_for` (`tests/test_manifest_loader_marker.py`).
- **`snapshot-guard.py` fires again.** It reads the written file from `tool_input.file_path`
  (the hook payload shape; the bare `file_path` stays as a fallback) and snapshots the
  manifest tq reads (the protected copy for a marked workspace) before a Write to the
  marker, the protected copy or an old-layout manifest. `scripts/snapshot.py capture`
  reads the same manifest (`tests/test_snapshot_guard.py`).

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
