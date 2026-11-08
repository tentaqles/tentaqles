# Releasing

One tag, `vX.Y.Z`, releases both the CLI (`tentaqles`, with its alias `tq`) and the Claude Code plugin. The plugin
ships from the marketplace at the root of this repo, `.claude-plugin/marketplace.json`: its one entry, `tentaqles`,
is a `git-subdir` source that checks out `plugin/` of `https://github.com/tentaqles/tentaqles.git` at a release tag
**and that tag's commit**:

```json
{
  "name": "tentaqles",
  "owner": {"name": "Tentaqles", "email": "reach@tentaqles.ai", "url": "https://github.com/tentaqles"},
  "plugins": [
    {
      "name": "tentaqles",
      "source": {
        "source": "git-subdir",
        "url": "https://github.com/tentaqles/tentaqles.git",
        "path": "plugin",
        "ref": "vX.Y.Z",
        "sha": "<40-hex commit of vX.Y.Z>"
      },
      "description": "<same as plugin/.claude-plugin/marketplace.json>"
    }
  ]
}
```

With both `ref` and `sha` set, Claude Code checks out the `sha`, so a moved or re-pushed tag cannot change what
installs. The entry has no `version`: the installed version is `plugin.json`'s at the pinned commit.

`main` only ever names a tag that exists and whose release passed, so the root marketplace is updated *after* the
tag, never in the release PR. `plugin/.claude-plugin/marketplace.json` is a separate local marketplace
(`"source": "./"`) for plugin development; it is not what users install, and it does keep a `version`.

## Steps

1. **Release PR.** In [`CHANGELOG.md`](../CHANGELOG.md), move `[Unreleased]` to `[X.Y.Z] - <date>` and open a new
   empty `[Unreleased]`. If the plugin changed, do the same in [`plugin/CHANGELOG.md`](../plugin/CHANGELOG.md) with
   the plugin's own version, and bump `version` in `plugin/.claude-plugin/plugin.json` and
   `plugin/.claude-plugin/marketplace.json` to it. Claude Code makes a new cache copy only when this version
   changes. `TestDevMarketplaceMatchesPlugin` (`test/integration`) fails when the two disagree. Do not touch
   `.claude-plugin/marketplace.json` here.
2. **Merge** the release PR to `main` with CI green.
3. **Tag** the merged `main` commit and push the tag:

   ```
   git fetch origin
   git tag vX.Y.Z origin/main
   git push origin vX.Y.Z
   ```

4. **Wait** for the `release` workflow on the tag to pass. If it fails, fix on `main` and release the next patch
   version; do not move the tag.
5. **Post-release PR.** Set the root `.claude-plugin/marketplace.json` entry's `source.ref` to `vX.Y.Z` and
   `source.sha` to the tag's commit, `git rev-parse "vX.Y.Z^{commit}"`. CI's `marketplace-pin` job fails unless the
   tag exists, resolves to that sha, and the sha is on `origin/main`; `TestRootMarketplacePin` checks the shape.
   Users get the new plugin once this PR is merged.

## What the tag publishes

`.github/workflows/release.yml` runs on the tag:

- its first step fails the release unless the tagged commit is on `origin/main` and `plugin.json`'s version equals
  `plugin/.claude-plugin/marketplace.json`'s;
- goreleaser builds `tentaqles` and `tq` (one program under two names, the same bytes) for every platform, the
  GitHub release, the Homebrew cask and the Scoop manifest;
- it attests build provenance for every archive in `checksums.txt`. Check a download with
  `gh attestation verify <file> --repo tentaqles/tentaqles`.

## How clients pick it up

`tentaqles reach` installs the plugin into each new client's Claude config with `claude plugin marketplace add
tentaqles/tentaqles` and `claude plugin install tentaqles@tentaqles`. Claude Code leaves auto-update off for
third-party marketplaces, so to update an existing client, run in its terminal
`claude plugin marketplace update tentaqles`, then `claude plugin update tentaqles@tentaqles`.
