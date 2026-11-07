# Changelog

All notable changes to the Tentaqles plugin. Versions follow [semver](https://semver.org/).
Releases before 0.16.0 are described on the
[GitHub releases page](https://github.com/tentaqles/tentaqles/releases).

## [Unreleased]

- **The onboarding skill drives the logins (ticket 021).** `skills/onboard/SKILL.md` is rewritten. Its
  first step is `tentaqles status --json`; it never reads the manifest, `.claude.json` or the state
  folder. It stops in a neutral or untrusted folder, walks `providers` in order (CLI, connection, then
  credential), runs `tentaqles install` for a missing CLI and `tentaqles connect` for an MCP server.
  A login runs under `!` only for a known browser flow (gh, az, acli jira); every other login, and
  `tq login` and `tq secret set`, is the developer's own terminal with no `!`, since tq refuses them in a
  session and a key prompt must not run in one. A CLI that still does not resolve after an install gets no
  login that run. It ends with one table. It never picks an installer, never handles a token, and refuses
  a provider outside `scope`. Text checks: `tests/test_onboard_skill.py`.

- **The plugin is the onboarding skill and the guard hook (ADR-0014).** Everything else is removed, all its
  Python included. With no fallback, a missing tq denies every tool.

- **`CLAUDE_CODE_SSE_PORT` is no longer an agent marker (ADR-0010).** `tqhome.py` and `tq_env.sh`
  follow `internal/markers`: the IDE integration sets it in a human's integrated terminal.

- **Fail closed (ADR-0006).** `tq_hook.sh` runs tq in the background under a watchdog (bash
  builtins, `sleep` and `kill`; no GNU `timeout`) that stops a tq still running after 7 s
  (KILL at 8 s), at least 5 s under the 15 s hook timeout, and denies; `TQ_HOOK_WRAPPER_DEADLINE` can only lower it. It
  turns an exit of tq other than 0 (allow) or 2 (deny) on `pre-tool-use` (a crash, a killed
  process) into exit 2 with `BLOCKED [tq/guard-error]`, since Claude Code runs a call on any
  other status; the
  fallback `identity-guard.py` denies a payload it cannot parse or that names no tool (it was
  allowed) (`tests/test_identity_guard.py`: `test_tq_hook_stops_a_hanging_tq_and_denies`,
  `test_tq_hook_deadline_is_under_the_hook_timeout`, `test_fallback_never_answers_ask`). tq itself never asks any more, so no skill may
  rely on a confirm from tq.
- **`tq_hook.sh` no longer honours `TQ_BIN` and never runs a tq from the plugin folder
  (ADR-0004).** An agent can set a variable, and the tq that enforces must not be its
  choice; the plugin ships no binary, so `$CLAUDE_PLUGIN_ROOT/bin/tq` is not looked at
  either: the binary is the tq on PATH, then the well-known install dirs. The plugin
  folder (not `skills/`, `agents/`, `templates/`, `tests/`, `__pycache__`) is covered by the
  signed integrity record, so a planted `bin/tq` is reported by `tq hooks verify`. The
  fail-closed fallback is unchanged (`tests/test_tq_off.py::test_tq_bin_is_not_honoured`,
  `test_tq_hook_never_runs_a_tq_from_the_plugin_folder`).
- **No bytecode from the plugin folder.** `tq_env.sh` exports `PYTHONDONTWRITEBYTECODE=1`
  and `PYTHONPYCACHEPREFIX=<tq home>/pycache` on every run (outside its interpreter guard)
  and every OS, `<tq home>` resolved as tq's `paths.Home` does (`TQ_HOME`, or
  `~/.tentaqles` when it is unset or an agent marker is set), so a `.pyc` planted in a
  `__pycache__` next to the plugin's sources, which the integrity record does not cover, is
  never loaded, and the only place Python would look is inside the protected state folder
  (`tests/test_pycache.py`).
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
  point them at a forged state folder. The markers are tq's one list
  (`internal/markers`, 12 names: Claude Code's entrypoint and SSE port, Codex's thread,
  OpenCode and Kimi Code among them), repeated in `tqhome.AGENT_MARKERS` and pinned to
  the Go file by `tests/test_tqhome_markers.py`. The tests set
  `tqhome.ALLOW_HOME_OVERRIDE` (`tests/conftest.py`); nothing else does.
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
