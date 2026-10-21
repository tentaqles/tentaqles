---
name: supply-chain
description: "Harden a repository's software supply chain, extending ci-pipeline: pin every GitHub Action to a commit SHA, set least-privilege workflow permissions, enforce lockfile-only installs and dependency audits, generate a CycloneDX or SPDX SBOM on release with the CISA 2026 minimum elements, require secret scanning, and report an OpenSSF Scorecard-style checklist. Use during /tentaqles:kickoff, when the user says \"harden CI\", \"supply chain\", \"SBOM\", \"pin actions\", \"Scorecard\", or when tq's guard flags an unpinned or branch-pinned action."
argument-hint: '[review | apply] [--headless]'
---

# Supply chain

`ci-pipeline` sets up the pipeline; this skill hardens what runs inside it and
what ships out of it. Checklist: `references/checklist.md` (rows `C1`…`C10`).

Headless mode: when `TENTAQLES_HEADLESS=1` is set, or `--headless` is in
`$ARGUMENTS`, `AskUserQuestion` is never called. At a human stop (a choice you
cannot make from the repo), use
`build-gate ask <slug> --kind question --text "<the question>" --options <a>,<b>`
only when the caller passed the slug of an active build run (the `build-gate`
prefix is in `skills/build/SKILL.md`); otherwise report what it needs as your
final output and end the turn. Human-stop cases: which SBOM tool to use when
several fit, generating or changing a lockfile (NEEDS-DECISION when headless),
and branch protection (needs repo admin; report the settings to apply instead).
With no slug (standalone use) the skill reports
and changes files, and records no review verdict.

## 1. Detect

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
ls "$root/.github/workflows" 2>/dev/null || echo "no GitHub Actions"
ls "$root/.gitlab-ci.yml" "$root/azure-pipelines.yml" "$root/Jenkinsfile" 2>/dev/null
ls "$root"/package-lock.json "$root"/pnpm-lock.yaml "$root"/yarn.lock "$root"/uv.lock "$root"/poetry.lock "$root"/go.sum "$root"/Cargo.lock 2>/dev/null
grep -rn "uses:" "$root/.github/workflows" 2>/dev/null | grep -vE "@[0-9a-f]{40}" || true
```

With **no GitHub Actions**: skip rows C1 to C3 with the reason "no Actions
workflows" and still apply C4 to C10 (lockfiles, audit, SBOM, secret scanning,
signing, branch protection). For GitLab CI or Azure Pipelines, pin included
templates and images by digest (C10) and use the platform's secret scanning.

## 2. Apply (or review)

- **C1 pinning**: replace every `uses: owner/repo@vN` or `@main` with the full
  commit SHA of the release and the version in a trailing comment (`# v4.2.2`).
  Resolve a tag to a commit SHA with
  `gh api repos/<o>/<r>/git/ref/tags/<tag> --jq '.object.type + " " + .object.sha'`.
  If the type is `commit`, that SHA is the pin. If it is `tag` (an annotated
  tag), dereference it: `gh api repos/<o>/<r>/git/tags/<sha> --jq .object.sha`.
  tq refuses unpinned refs: `@main`/`@master` (`tq/gha-branch-ref`) and
  `@latest`/`@head` (`tq/gha-unpinned`).
- **C2 permissions**: `permissions: contents: read` at the top of every
  workflow; grant `id-token: write`, `pull-requests: write` or `packages: write`
  per job only. `persist-credentials: false` on every checkout.
- **C3 untrusted input**: never `pull_request_target` with a PR-head checkout;
  pass `github.event.*` values through `env:` not `run:` interpolation.
- **C4 lockfile-only**: commit the lockfile and install from it only. Per
  ecosystem (CI command / lockfile generation):

  | Ecosystem | CI install command | Generate the lockfile (MUTATING) |
  |---|---|---|
  | npm | `npm ci` | `npm install --package-lock-only` |
  | pnpm | `pnpm install --frozen-lockfile` | `pnpm install --lockfile-only` |
  | yarn | `yarn install --immutable` | `yarn install --mode update-lockfile` |
  | pip | `pip install --require-hashes -r requirements.txt` | `pip-compile --generate-hashes` |
  | uv | `uv sync --locked` | `uv lock` |
  | poetry | `poetry install --sync` | `poetry lock` |
  | go | `go mod verify` (checks `go.sum`) | `go mod tidy` |
  | cargo | `cargo build --locked` | `cargo generate-lockfile` |

  Generation commands change files and dependency versions: run them only when
  the user agrees and report the change; headless: record a NEEDS-DECISION and
  do not run them.
- **C5 audit and updates**: an audit step (`npm audit --audit-level=high`,
  `pip-audit`, `govulncheck ./...`) and a Dependabot or Renovate config with
  grouped weekly updates that also bumps action SHAs.
- **C6 SBOM**: on release, generate CycloneDX (or SPDX) with a pinned tool
  (`cyclonedx-npm`, `cyclonedx-py`, `syft`), covering the CISA 2026 minimum
  elements (component name, version, supplier, unique identifier, dependency
  relationships, author, timestamp, licence, hash), and attach it to the release.
- **C7 secret scanning**: enable the platform's secret scanning and push
  protection, or a pinned gitleaks action as a required check; locally tq's
  `commit-secret` rule already scans commits.
- **C8 attestations**: build in CI; sign or attest release artifacts
  (`actions/attest-build-provenance` pinned, or Sigstore `cosign`).
- **C9 branch protection**: required checks, reviews, no force push to the
  default branch (tq denies it locally too).
- **C10 images and toolchains**: `FROM image@sha256:...` with the tag kept in a
  comment, exact tool versions. Resolve a digest with
  `docker buildx imagetools inspect <image>:<tag>` (the `Digest:` line, the
  index digest for multi-arch) or `crane digest <image>:<tag>`; the same applies
  to compose and Kubernetes `image:` lines.

Run `tq decide triage --staged` on the workflow changes (they come back
`high`); the `security-reviewer` runs in the build engine for workflow diffs.

## 3. Report

A table of the ten rows with `done | skipped (reason) | todo`, then commit
only the files this run changed, each named explicitly (omit any path you did
not change; a path git does not know aborts the add):

```bash
root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
git -C "$root" add .github/workflows/<changed-file> [.github/dependabot.yml only if changed] [<lockfile-you-changed>]
git -C "$root" commit -m "ci(supply-chain): pin actions, least-privilege permissions, SBOM on release"
```

Never `git add -A` or `git add .`.
