# tentaqles (monorepo)

One repo, one installer: terminal-identity orchestration for developers who work across several clients/companies.

## Reading path
To work on this repo read `docs/prd.md` (requirements with status, backlog and decisions) and
`docs/threat-model.md` first, then the spec slice you were given (`docs/specs/<slug>.md`) and
the ADRs under `docs/adr/`. `docs/LEARNINGS.md` lists the rules this repo learned the hard way.
The last three appear with the first gated builds (`/tentaqles:feature`).

## Layout
- `cmd/tq/`, `internal/` — the `tq` Go CLI (identity switching, trust, git identity, bundles, doctor). `go test ./...` must stay green.
- `internal/hub/` — `tq hub` (release 2, spec A): per-workspace SQLite board under `$TQ_HOME/hub/<workspace>/` (`store`, `activity`), headless runner (`runner`, `adapter`, `gate`, `notify`), web page and API (`web`, `serveinfo`). Board sync (spec B): `sync` (engine, outbox, MCP fallback), `sync/cred` (credential chain cli → keychain → env → mcp), tracker adapters `sync/jira` and `sync/linear` (linked into the binary by `cmd/tq/adapters.go`). `docs/HUB.md`.
- `internal/packs/` — `tq pack` (releases 3 and 4, specs C and D): the pack framework (registry, merge rules, `tq-packs.yaml`, guards, `GuardCtx`), `approval` and `ledger` (pack approvals, keychain-signed answers, claims), `baseline`, `toolrun` (external tools with a filtered env), `scan` (shell and PowerShell readers), `envgate` (environments and policies), and the packs `data`, `ml`, `cloud` and `db`. Pack guards are registered from `internal/cmd/claudehook_iac.go` and `claudehook_db.go`. `docs/PACKS.md`, `docs/packs/`.
- `internal/secret` (`tq secret`, OS keychain via go-keyring), `internal/onboard` (`tq onboard`), `internal/termopen` (open a terminal in a folder).
- `providers/` — (planned) provider catalog YAML (cloud / vcs / data / deploy / pm / agent / custom), embedded in the binary.
- `desktop/` — (planned) Wails v2 setup app sharing `internal/`.
- `plugin/` — the Claude Code plugin (Python): hooks shell out to `tq claude-hook` for identity reporting/enforcement, plus memory/graph/dashboard. Installed from this repo's root marketplace (`.claude-plugin/marketplace.json`, git-subdir `plugin/` pinned to a release tag and its sha, added after the tag in a post-release PR): `/plugin marketplace add tentaqles/tentaqles`, `/plugin install tentaqles@tentaqles`. Release steps in `docs/RELEASING.md`.
- `installers/`, `.goreleaser.yaml`, `.github/workflows/` — CLI release + CI.
- `docs/` — user-facing docs: `SETUP.md`, `CLAUDE-HOOK.md`, `PROVIDERS.md`, `DESKTOP.md`, and `MIGRATE.md` (`tq migrate` steps, the journal, `tq uninstall --restore`, the `TQ_ENABLED=0` kill switch), and `CLAUDE-SETTINGS.md` (`tq settings render`, `tq statusline`, `tq worktrees`), `DECIDE.md` (`tq decide`, the Jev decision layer: policy, redaction, cache, eval, judgment rules, triage, routing, Stop check, skill picker, explore, memory gates), `INSIGHTS.md` (`tq insights`), `HUB.md` (`tq hub`: board, headless runs, decisions, review page, budgets, board sync), and `PACKS.md` with `docs/packs/` (`tq pack`: config, checks, guards, approvals, one page per pack area, `framework-security.md`). `docs/SETUP.md` covers setup plans and points at packs. `RELEASING.md`: release order (release PR, tag, release workflow, post-release PR pinning ref + sha), and catalog entries for identities.
- `templates/` — standalone client-system templates (n8n Jev decision sub-workflow, RAG starter, pandas Jev labeller, dbt contract starter, ML starter), each with its own tests (CI job `templates`; a template with a `uv.lock` installs from it).
- Docs live in the workspace: `../docs/plans/*.md` (spec + plans), `../docs/superpowers/plans/*.md` (task plans).

## Must not trade off
From `docs/threat-model.md`; each line names the test that keeps it.
- Identity drift blocks git/gh/cloud commands: `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`.
- Mode changes and `tq allow` stay human-only: `TestAllowModeIsHumanOnly`.
- Identity, secrets, self-protection and privacy stay on in every mode: `TestRelaxedLockedDeniesStillBlock`.
- An edited manifest loses its trust and mode grant: `TestPlainAllowAfterEditDropsTheGrant`.
- Build-gate records are signed and a rollback is refused: `test_an_older_signed_approval_copied_back_counts_as_rolled_back`.
- Pack answers count only when signed: `TestSignedAnswersOnly`.
- The hub API refuses human-only actions from run, nested and unknown sessions, from the legacy agent header, and from cross-site requests: `TestAPIAgentHeaderRefusesHumanOnly`, `TestRebindingHostAndOriginAreRefused` (planned, B-33: `TestAPIClassHeaderPolicy`).
- A run, nested or unknown session cannot answer, start, cancel or move a card into an active lane (planned, B-32: `TestSessionClassMatrix`, `TestRunClassCannotAnswer`).
- Mode and trust changes, `tq guard|unattended`, LAN binds and hub keys refuse the console too: `TestModeWritesRefuseAgents` (planned, B-33: `TestConsoleStaysOutOfTheTerminalOnly`).
- External tools get a filtered environment: `TestToolEnvIsFiltered`.
- The plugin installs from a pinned tag and sha: `TestRootMarketplacePin`.

## Project config
Humans read the prose; tools read the fenced block (the build gate's definition of done reads
it through `plugin/tentaqles/workflow/project.py`). Keep both in step.

- `test` and `lint` are the `go` CI job's commands; there is no typecheck. The plugin's pytest
  suite is not in `test`: on Windows it takes about 13 minutes, which does not fit the gate's
  900 s budget per command next to the Go suite. A checkpoint's verify commands name the
  plugin test files its change touches, and CI's `plugin` job runs the whole suite
  (`PYTHONPATH=plugin python -m pytest plugin/tests -q`). The `templates`, `opa` and `desktop`
  CI jobs are outside the gate too: run them yourself when a change touches `templates/`,
  the OPA policies or `desktop/`.
- `architecture_paths` are the trust boundaries of `docs/threat-model.md` (B1 to B9): the guard
  and policy rules, trust, the hooks, the build gate, the hub gate, runner, store and API, board
  sync and its credentials, pack approvals, ledgers, environments and tool runs, and the release
  pipeline. A change there needs an ADR under `docs/adr/` (template:
  `plugin/templates/process/adr-0000-template.md`).
- `classified_paths` is empty: tq keeps no personal data of its own, and the hub masks card
  text before it is stored.
- `changelog` is this repository's `CHANGELOG.md`, under `[Unreleased]`. A change under
  `plugin/` also gets its line in `plugin/CHANGELOG.md`; the root entry is then one pointer line.

```json tq-project
{
  "commands": {
    "test": "go test ./... -count=1",
    "lint": "go vet ./...",
    "build": "go build ./..."
  },
  "architecture_paths": [
    "internal/guard/**",
    "internal/guardcfg/**",
    "internal/policy/**",
    "internal/trust/**",
    "internal/hooks/**",
    "internal/session/**",
    "internal/hub/gate/**",
    "internal/hub/runner/**",
    "internal/hub/store/**",
    "internal/hub/web/*.go",
    "internal/hub/sync/*.go",
    "internal/hub/sync/cred/**",
    "internal/packs/approval/**",
    "internal/packs/ledger/**",
    "internal/packs/envgate/**",
    "internal/packs/toolrun/**",
    "plugin/hooks/**",
    "plugin/scripts/tq_hook.sh",
    "plugin/scripts/*-guard.py",
    "plugin/scripts/hub-*.py",
    "plugin/scripts/build-gate.py",
    "plugin/scripts/loop-runner.py",
    "plugin/tentaqles/workflow/gate.py",
    "plugin/tentaqles/workflow/loopguard.py",
    ".github/workflows/**",
    ".github/dependabot.yml",
    ".goreleaser.yaml",
    ".claude-plugin/marketplace.json",
    "installers/**"
  ],
  "classified_paths": [],
  "changelog": "CHANGELOG.md"
}
```

## Rules
- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test`/git via PowerShell; tests must isolate `TQ_HOME`, `HOME`, `USERPROFILE`.
- Language: en. Conventional commits.
