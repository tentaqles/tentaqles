# Changelog

All notable changes to the Tentaqles plugin. Versions follow [semver](https://semver.org/).
Releases before 0.16.0 are described on the
[GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

- **The plugin is the identity check and nothing else** ([ADR-0001](../docs/adr/0001-identity-isolator.md)). The
  onboarding skill is removed. The PreToolUse matcher is now `^(Bash|PowerShell)$`: tq checks shell commands only
  (the wrong git email, gh user or cloud, a stale shell, a remote command outside every client folder), so file
  edits and MCP tools no longer start a tq process.
- The description names what the plugin does now.

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
