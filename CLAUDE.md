# tentaqles (monorepo)

One repo, one installer: terminal-identity orchestration for developers who work across several clients/companies.

## Layout
- `cmd/tq/`, `internal/` — the `tq` Go CLI (identity switching, trust, git identity, bundles, doctor). `go test ./...` must stay green.
- `providers/` — (planned) provider catalog YAML (cloud / vcs / data / deploy / pm / agent / custom), embedded in the binary.
- `desktop/` — (planned) Wails v2 setup app sharing `internal/`.
- `plugin/` — the Claude Code plugin (Python): hooks shell out to `tq claude-hook` for identity reporting/enforcement, plus memory/graph/dashboard. Installed via `/plugin marketplace add tentaqles/tentaqles-plugin` today; will move to this repo's marketplace.
- `installers/`, `.goreleaser.yaml`, `.github/workflows/` — CLI release + CI.
- `docs/` — user-facing docs: `SETUP.md`, `CLAUDE-HOOK.md`, `PROVIDERS.md`, `DESKTOP.md`, and `MIGRATE.md` (`tq migrate` steps, the journal, `tq uninstall --restore`, the `TQ_ENABLED=0` kill switch).
- Docs live in the workspace: `../docs/plans/*.md` (spec + plans), `../docs/superpowers/plans/*.md` (task plans).

## Rules
- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test`/git via PowerShell; tests must isolate `TQ_HOME`, `HOME`, `USERPROFILE`.
- Language: en. Conventional commits.
