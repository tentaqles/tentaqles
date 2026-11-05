# tentaqles (monorepo)

One repo, one installer: an identity isolator with secrets protection for developers who
work across several clients/companies. One folder per client; the terminal, the agent
sessions and the external tools (git and its hosts, the cloud CLIs, the Claude and Codex
config homes) carry that client's identity and nothing else; a command under the wrong
identity is blocked with the reason.

## Reading path
To work on this repo read `docs/prd.md` (requirements with status, backlog and the owner's
decisions) and `docs/threat-model.md` first, then the ADRs under `docs/adr/`.

## Layout
- `cmd/tq/`, `internal/` — the `tq` Go CLI. `go test ./...` must stay green.
  - `internal/guard/` — the identity-drift block, `guard.Decide` (`blocked-command`,
    `wrong-cloud`, `neutral-remote`, `untrusted`, `env-drift`, `claude-config-drift`,
    `git-email-drift`, `gh-user`) and the shell tokenizer the hook uses.
  - `internal/policy/` — the five secret rules of `builtin.go` (`tq/secret-store-read`,
    `tq/secret-store-shell`, `tq/env-file-read`, `tq/env-file-shell`, `tq/env-dump`) and
    their evaluator; nothing else is a rule.
  - `internal/cmd/` — every command; `internal/cmd/root.go` registers them: `init`, `add`,
    `allow`, `deny`, `list`, `env`, `activate`, `hooks`, `login`, `run`, `doctor`, `bundle`,
    `providers`, `setup`, `onboard`, `secret`, `secrets`, `settings`, `statusline`,
    `worktrees`, `dotenv`, `migrate`, `uninstall`, `version`, `claude-hook`. The hook is
    `claudehook*.go`: `pre-tool-use` runs `guard.Decide`, then the secret rules plus
    `tq/commit-secret` and `tq/cross-client-mcp`, with `tq/guard-error` as the fail-safe;
    `session-start` prints the preamble. `humanonly.go` keeps `tq allow`, `tq deny`,
    `tq secret` and `tq run --off` out of agent sessions (environment marker, no prompt).
  - `internal/trust/` (manifest trust by file hash), `internal/manifest/` (the manifest,
    read from `~/.tentaqles/identities/<ws>/manifest.yaml` with a name-only marker
    `.tentaqles.yaml` in the workspace root, or from the root itself for an identity not
    yet migrated with `tq migrate manifests`, ADR-0002; a top-level block tq does not
    know is recorded as dropped, never read), `internal/protect/` (the state folder
    private to the user: modes, and on Windows the one icacls run `tq doctor` reports as
    `protected-acl`), `internal/registry/`, `internal/resolve/` (which file is the
    manifest, both layouts), `internal/workspace/`, `internal/paths/`,
    `internal/envplan/`, `internal/shell/` (activation hooks and the `claude`/`codex`
    launchers), `internal/hooks/` (shell profile blocks), `internal/gitcfg/`,
    `internal/doctor/`, `internal/setup/` and `internal/onboard/` (`tq setup`,
    `tq onboard`), `internal/migrate/`, `internal/bundle/`, `internal/providers/` (the
    embedded catalog), `internal/claudesettings/`, `internal/codexcfg/`,
    `internal/statusline/`, `internal/secret/` (`tq secret`, OS keychain via go-keyring),
    `internal/secrets/` (`tq secrets audit`), `internal/dotenv/`, `internal/commitscan/`
    (`tq/commit-secret`), `internal/audit/`, `internal/detect/`, `internal/verify/`,
    `internal/termopen/`, `internal/testutil/`.
- `pkg/setupapi/` — the setup plan API shared by `tq setup` and `tq onboard`.
- `test/integration/` — git identity end to end and the marketplace pin tests.
- `providers/` — a README; the provider catalog YAML lives in `internal/providers/catalog/`.
- `plugin/` — the Claude Code plugin (Python): hooks shell out to `tq claude-hook` for
  identity and secrets, plus the session memory, the identity preamble, the knowledge
  graph, 40 skills (plain checklists) and 5 read-only reviewer agents. Installed from this
  repo's root marketplace (`.claude-plugin/marketplace.json`, git-subdir `plugin/` pinned to
  a release tag and its sha, added after the tag in a post-release PR):
  `/plugin marketplace add tentaqles/tentaqles`, `/plugin install tentaqles@tentaqles`.
  Release steps in `docs/RELEASING.md`.
- `installers/`, `.goreleaser.yaml`, `.github/workflows/` — CLI release + CI. `test.yml`
  has four jobs: `go` (three OSes), `govulncheck`, `marketplace-pin`, `plugin`;
  `release.yml` runs on a `v*` tag.
- `scripts/spike-isolation.sh` — the macOS keychain-isolation spike behind the "Limitations"
  note of `README.md`.
- `docs/` — user-facing docs: `SETUP.md` (setup plans, `tq onboard`), `CLAUDE-HOOK.md`
  (the hook protocol, the identity rules, the secret rules, `tq dotenv run`),
  `CLAUDE-SETTINGS.md` (`tq settings render`, `tq statusline`, `tq worktrees`, Codex),
  `COMMANDS.md` (every command and skill, plus a test walkthrough), `PROVIDERS.md` (the
  catalog schema, `tq secret`), `MIGRATE.md` (`tq migrate`, the journal,
  `tq uninstall --restore`, the `TQ_ENABLED=0` kill switch, "Upgrading an existing install"),
  `RELEASING.md` (release PR, tag, release workflow, post-release PR pinning ref + sha),
  `IDEATION.md` (the isolation vision), `prd.md`, `threat-model.md`, `adr/`.

## Must not trade off
From `docs/threat-model.md`; each line names the test that keeps it.
- Identity drift blocks git/gh/cloud commands: `TestReadOnlyGitExemptFromEnvDriftAndUntrusted`.
- The secret rules fire on `.env` reads, environment dumps and secret-store reads, and a
  `.env.example` is allowed: `TestPreToolUse_SecretRules`, `TestBuiltinIsTheFiveSecretRules`.
- Trust is human-only: `TestAllowAndDenyRefuseAgents`, `TestAddDoesNotTrustFromAgent`.
- An edited manifest loses its trust: `TestEditedManifestLosesTrust`.
- The guard reads the manifest from the protected location only; a marker that names
  another workspace, or has no protected copy, resolves neutral:
  `TestResolve_MarkerClientMismatch_Neutral`, `TestResolve_MarkerWithoutProtectedManifest_Neutral`.
- A trusted manifest copied into another folder lends it nothing (the manifest's `client`
  must be the folder name; trust is keyed by name and hash), and `TQ_HOME` is ignored in
  an agent session: `TestResolve_ReplayOfATrustedManifest_Neutral`,
  `TestTrustIsKeyedByWorkspaceName`, `TestHome_IgnoresTQHomeUnderAnAgentMarker`.
- The plugin installs from a pinned tag and sha: `TestRootMarketplacePin`.

## Project config
CI's `go` job runs `go vet ./...`, `go test ./... -count=1` and a build; locally run
`go build ./...` too. The plugin suite is `PYTHONPATH=plugin python -m pytest plugin/tests -q`
(CI job `plugin`; about 13 minutes on Windows). The changelog is `CHANGELOG.md` under
`[Unreleased]`; a change under `plugin/` also gets its line in `plugin/CHANGELOG.md`, and the
root entry is then one pointer line.

A change under `internal/guard/`, `internal/policy/`, `internal/trust/`, `internal/hooks/`,
`plugin/hooks/`, `plugin/scripts/tq_hook.sh`, `.github/workflows/`,
`.goreleaser.yaml`, `.claude-plugin/marketplace.json` or `installers/` crosses a trust
boundary of `docs/threat-model.md` and needs an ADR under `docs/adr/` (template:
`plugin/templates/process/adr-0000-template.md`).

## Rules
- Git identity for this repo: `reach@tentaqles.ai` (local config). Never commit secrets; manifests hold names only.
- Windows dev box: run `go test`/git via PowerShell; tests must isolate `TQ_HOME`, `HOME`, `USERPROFILE`.
- Language: en. Conventional commits.
