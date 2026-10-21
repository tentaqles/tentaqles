# Supply chain checklist (answer-key rows)

| # | Check | Source |
|---|---|---|
| C1 | Every third-party GitHub Action pinned to a full commit SHA with the version in a comment; `@main`/`@master` denied, `@latest` asked by tq | SLSA 1.2 build provenance; OpenSSF Scorecard Pinned-Dependencies |
| C2 | Top-level `permissions: contents: read` on every workflow; job-level grants only where needed; `persist-credentials: false` on checkout | OpenSSF Scorecard Token-Permissions |
| C3 | No `pull_request_target` with a checkout of the PR head; secrets never interpolated from untrusted fields in `run:` | GitHub Actions security hardening |
| C4 | Lockfile committed and installs are lockfile-only (`npm ci`, `uv sync --frozen`, `go mod verify`) | SLSA dependencies |
| C5 | Dependency audit step (`npm audit`, `pip-audit`, `govulncheck`) and automated updates (Dependabot or Renovate) with pinned grouping | Scorecard Vulnerabilities, Dependency-Update-Tool |
| C6 | SBOM generated on release (CycloneDX or SPDX) with the CISA 2026 minimum elements: component name, version, supplier, unique id, dependency relationships, author, timestamp, licence, hash | CISA Minimum Elements for SBOM (2026) |
| C7 | Secret scanning: enable secret scanning (platform scanner with push protection, or pinned gitleaks) as a required check, plus tq's commit scan locally | Scorecard; tq/commit-secret |
| C8 | Release artifacts signed or attested (Sigstore, GitHub artifact attestations) and built in CI, not on a laptop | SLSA L2/L3 |
| C9 | Branch protection: required checks, no force push to the default branch, reviews required | Scorecard Branch-Protection |
| C10 | Base images and toolchains pinned by digest or exact version | SLSA; Scorecard Pinned-Dependencies |
