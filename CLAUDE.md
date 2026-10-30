# tentaqles (monorepo)

One repo, one installer: terminal-identity orchestration for developers who work across several clients/companies.

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
- The hub API refuses human-only actions from agents and cross-site requests: `TestAPIAgentHeaderRefusesHumanOnly`, `TestRebindingHostAndOriginAreRefused`.
- External tools get a filtered environment: `TestToolEnvIsFiltered`.
- The plugin installs from a pinned tag and sha: `TestRootMarketplacePin`.

## Rules
- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test`/git via PowerShell; tests must isolate `TQ_HOME`, `HOME`, `USERPROFILE`.
- Language: en. Conventional commits.
