# Releasing

One tag, `vX.Y.Z`, releases both the `tq` CLI (with the desktop app) and the Claude Code
plugin. The plugin ships from the marketplace at the root of this repo,
`.claude-plugin/marketplace.json`: its one entry, `tentaqles`, is a `git-subdir` source that
checks out `plugin/` of `https://github.com/tentaqles/tentaqles.git` at a release tag
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

With both `ref` and `sha` set Claude Code checks out the `sha`, so a moved or re-pushed tag
cannot change what installs. The entry has no `version`: the installed version is
`plugin.json`'s at the pinned commit, and a version on `main` would drift between the
release PR and the post-release PR.

`main` only ever names a tag that exists and whose release passed. So the root marketplace
is updated *after* the tag, never in the release PR. The file does not exist until the first
post-release PR (for `v0.8.0`) adds it.

`plugin/.claude-plugin/marketplace.json` is a separate local marketplace (`"source": "./"`)
for plugin development; it is not what users install, and it does keep a `version`.

## Steps

1. **Release PR.** In [`CHANGELOG.md`](../CHANGELOG.md), move `[Unreleased]` to
   `[X.Y.Z] — <date>` and open a new empty `[Unreleased]`. If the plugin changed, do the
   same in [`plugin/CHANGELOG.md`](../plugin/CHANGELOG.md) with the plugin's own version and
   bump `version` in `plugin/.claude-plugin/plugin.json` and
   `plugin/.claude-plugin/marketplace.json` to it. Claude Code makes a new cache copy only
   when this version changes, so a plugin change without a bump never reaches users.
   `TestDevMarketplaceMatchesPlugin` (`test/integration`) fails when the two disagree. Do
   not touch `.claude-plugin/marketplace.json` here.
2. **Merge** the release PR to `main` with CI green.
3. **Tag** the merged `main` commit and push the tag:

   ```
   git fetch origin
   git tag vX.Y.Z origin/main
   git push origin vX.Y.Z
   ```

4. **Wait** for the `release` workflow on the tag to pass. If it fails, fix on `main` and
   release the next patch version; do not move the tag.
5. **Post-release PR.** Set the root `.claude-plugin/marketplace.json` entry's `source.ref`
   to `vX.Y.Z` and `source.sha` to the tag's commit, `git rev-parse "vX.Y.Z^{commit}"`.
   CI's `marketplace-pin` job fails unless the tag exists, resolves to that sha, and the sha
   is on `origin/main`; `TestRootMarketplacePin` checks the shape (git-subdir, exact url,
   path `plugin`, `vX.Y.Z` ref, 40 lowercase hex sha, no `version`). Users get the new
   plugin once this PR is merged.

## What the tag publishes

`.github/workflows/release.yml` runs on the tag:

- its first step fails the release unless the tagged commit is on `origin/main` and
  `plugin.json`'s version equals `plugin/.claude-plugin/marketplace.json`'s;
- goreleaser builds `tq` for every platform, the GitHub release, the Homebrew tap and the
  Scoop bucket;
- the desktop job builds the setup app installers and adds them to the release.

The plugin needs no build step. A marketplace refresh reads the root `marketplace.json`
from `main` (the marketplace source has no `ref`); after the post-release PR it names the new
commit and installs `plugin/` from it.

## How identities pick it up

Each tq identity has its own Claude config dir, so the marketplace is set once in the tq
bundle catalog (`$TQ_HOME/bundles/catalog.yaml`) and synced into every workspace:

```yaml
marketplaces:
  tentaqles:
    source: github
    repo: tentaqles/tentaqles
    autoUpdate: true
```

with the workspace manifest listing it:

```yaml
claude:
  bundle:
    marketplaces: [tentaqles]
    plugins: [tentaqles@tentaqles]
```

then `tq bundle sync <ws>`. That writes, in the identity's `settings.json`:

```json
"extraKnownMarketplaces": {
  "tentaqles": {"source": {"source": "github", "repo": "tentaqles/tentaqles"}, "autoUpdate": true}
}
```

Claude Code leaves auto-update off for third-party marketplaces; `autoUpdate: true` makes it
refresh the marketplace at startup and update the plugin when its version changes. Without
it, run `/plugin marketplace update tentaqles` in a session.

Catalog marketplace fields, as Claude Code names them: `source` (`github`, `git`, `url`,
`directory`; `local` is an older spelling of `directory`), `repo` (github), `url` (git clone
URL, or the marketplace.json URL for `url`), `ref` (branch or tag, github and git), `path`
(the folder for `directory`; for github and git the path of marketplace.json in the repo,
default `.claude-plugin/marketplace.json`), `autoUpdate`, and the tq-only `insecure` (below). `tq bundle diff <ws>` reports a
changed ref or autoUpdate as `marketplace-source` drift, e.g.
`settings has github tentaqles/tentaqles@v0.7.0, the catalog has github tentaqles/tentaqles@v0.8.0`.

The catalog check (`tq bundle catalog`, `tq doctor`, `tq bundle sync`) warns when a `git` or
`url` source URL carries credentials (userinfo, or a token-shaped value): `settings.json`
would hold them in clear, so use a git credential helper. It refuses, and `sync` will not
write, a `git` url that is not `https://`, `ssh://` or `user@host:path` (for example
`file://`, `ext::`, a local path), and a `ref` that starts with `-`, contains `..`, `:`, a
space, one of `~^?*[\` or a control character, or ends with `.lock` or `/`. A git url whose
user or host starts with `-`, or that holds a control character, is refused too.

Plugins install hooks, so a plain-text transport lets anyone on the network path run code:
`http://` and `git://` git urls and any `url` source that is not `https://` are refused. For
a local mirror, set `insecure: true` on that catalog entry: tq then writes it, warns on every
check, and never puts `insecure` into `settings.json`. Drift details never print URL userinfo.

### The development workspace: one key, two sources

The workspace you develop the plugin in should load it from your checkout, while every other
identity follows the release. Both must use the settings key `tentaqles` so the plugin id
stays `tentaqles@tentaqles` everywhere. Give the catalog two entries:

```yaml
marketplaces:
  tentaqles:     {source: github, repo: tentaqles/tentaqles, autoUpdate: true}
  tentaqles-dev: {source: directory, path: 'C:\repos\tentaqles\tentaqles\plugin'}
```

and in the development workspace's bundle list the alias `key=catalogName`:

```yaml
claude:
  bundle:
    marketplaces: [tentaqles=tentaqles-dev]
    plugins: [tentaqles@tentaqles]
```

`tentaqles=tentaqles-dev` writes catalog entry `tentaqles-dev` under settings key `tentaqles`.
A plain name (`tentaqles`) is both the key and the catalog entry. Plugins always name the
key. An alias to a catalog entry that does not exist is an error naming both, and both halves
must be plain names (no `/`, `\`, `=`, `@`, control characters, or leading or trailing
spaces).
