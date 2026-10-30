# Releasing

One tag, `vX.Y.Z`, releases both the `tq` CLI (with the desktop app) and the Claude Code
plugin. The plugin ships from the marketplace at the root of this repo,
[`.claude-plugin/marketplace.json`](../.claude-plugin/marketplace.json): its one entry,
`tentaqles`, is a `git-subdir` source that checks out `plugin/` of
`https://github.com/tentaqles/tentaqles.git` at the release tag.

`plugin/.claude-plugin/marketplace.json` is a separate local marketplace (`"source": "./"`)
for plugin development; it is not what users install.

## Steps

1. **Changelogs.** In [`CHANGELOG.md`](../CHANGELOG.md), move `[Unreleased]` to
   `[X.Y.Z] — <date>` and open a new empty `[Unreleased]`. If the plugin changed, do the
   same in [`plugin/CHANGELOG.md`](../plugin/CHANGELOG.md) with the plugin's own version.
2. **Plugin version** (only if the plugin changed). Bump `version` in
   `plugin/.claude-plugin/plugin.json`, `plugin/.claude-plugin/marketplace.json` and
   `.claude-plugin/marketplace.json` to the same value. Claude Code makes a new cache copy
   only when this version changes, so a plugin change without a bump never reaches users.
   `TestRootMarketplaceMatchesPlugin` (`test/integration`) fails when the three disagree.
3. **Marketplace ref.** Set `plugins[0].source.ref` in `.claude-plugin/marketplace.json` to
   the new tag, `vX.Y.Z`. Do this on every release, even when the plugin did not change:
   the release workflow refuses a tag whose marketplace ref is anything else.
4. **Merge** the release PR to `main` with CI green.
5. **Tag** the merge commit and push the tag:

   ```
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

## What the tag publishes

`.github/workflows/release.yml` runs on the tag:

- its first step fails the release unless the root marketplace's plugin `source.ref` equals
  the tag (`$GITHUB_REF_NAME`);
- goreleaser builds `tq` for every platform, the GitHub release, the Homebrew tap and the
  Scoop bucket;
- the desktop job builds the setup app installers and adds them to the release.

The plugin needs no build step: once the tag exists, a marketplace refresh reads the root
`marketplace.json` from `main` (the marketplace source has no `ref`), finds the new version
and installs `plugin/` at the tag.

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
default `.claude-plugin/marketplace.json`) and `autoUpdate`. `tq bundle diff <ws>` reports a
changed ref or autoUpdate as `marketplace-source` drift, e.g.
`settings has github tentaqles/tentaqles@v0.7.0, the catalog has github tentaqles/tentaqles@v0.8.0`.

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
