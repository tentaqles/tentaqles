// Package bundle manages the tq bundle catalog and syncs a workspace's
// Claude identity directory to the plugins/skills/MCP servers its manifest
// declares.
package bundle

import (
	"fmt"
	"os"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/paths"
	"gopkg.in/yaml.v3"
)

// Marketplace describes a plugin marketplace source, matching the shape
// Claude Code expects under settings.extraKnownMarketplaces. The fields
// carry the same names and meanings as Claude Code's own source object, so
// a catalog entry reads like the JSON it renders to:
//
//   - Source: github | git | url | directory ("local" is an older spelling
//     of directory and is written as directory).
//   - Repo: owner/repo, for github.
//   - URL: the clone URL for git, the marketplace.json URL for url.
//   - Ref: branch or tag to check out, for github and git (empty: the
//     repo's default branch).
//   - Path: for directory, the local folder; for github and git, the path
//     of marketplace.json inside the repo (empty: Claude Code's default,
//     .claude-plugin/marketplace.json). Unused for url.
//   - AutoUpdate: rendered as the entry's top-level "autoUpdate": true, next
//     to "source" (Claude Code leaves it off for third-party marketplaces).
//   - Insecure: tq-only opt-in that lets a git source use http:// or git://
//     and a url source use http:// (a local mirror). Never rendered into
//     settings.json; every catalog check warns about it.
type Marketplace struct {
	Source     string `yaml:"source"`
	Repo       string `yaml:"repo,omitempty"`
	URL        string `yaml:"url,omitempty"`
	Ref        string `yaml:"ref,omitempty"`
	Path       string `yaml:"path,omitempty"`
	AutoUpdate bool   `yaml:"autoUpdate,omitempty"`
	Insecure   bool   `yaml:"insecure,omitempty"`
}

// ToKnownMarketplace renders m in the {"source": {...}} shape used by
// settings.json's extraKnownMarketplaces map, with "autoUpdate": true
// beside "source" when m.AutoUpdate is set.
func (m Marketplace) ToKnownMarketplace() map[string]any {
	src := map[string]any{"source": m.Source}
	switch m.Source {
	case "github":
		src["repo"] = m.Repo
		putRefPath(src, m)
	case "git":
		src["url"] = m.URL
		putRefPath(src, m)
	case "url":
		src["url"] = m.URL
	case "directory", "local":
		// Claude Code names a local folder source "directory"; it does not
		// know "local", so an older catalog entry is written the new way.
		src["source"] = "directory"
		src["path"] = m.Path
	}
	entry := map[string]any{"source": src}
	if m.AutoUpdate {
		entry["autoUpdate"] = true
	}
	return entry
}

// putRefPath adds the optional ref and marketplace.json path of a github or
// git source, leaving them out when empty so Claude Code's defaults apply.
func putRefPath(src map[string]any, m Marketplace) {
	if m.Ref != "" {
		src["ref"] = m.Ref
	}
	if m.Path != "" {
		src["path"] = m.Path
	}
}

// Skill names a source directory to be copied into a workspace's skills dir.
type Skill struct {
	Path string `yaml:"path"`
}

// MCPServer is a verbatim mcpServers entry (command/args/env/url/type).
type MCPServer map[string]any

// Catalog names marketplaces, skill sources, and MCP server specs once, so
// manifests can reference them by name in claude.bundle.
type Catalog struct {
	Marketplaces map[string]Marketplace `yaml:"marketplaces"`
	Skills       map[string]Skill       `yaml:"skills"`
	MCP          map[string]MCPServer   `yaml:"mcp"`
}

// LoadCatalog reads the catalog from paths.Catalog(). A missing file yields
// an empty catalog, not an error.
func LoadCatalog() (*Catalog, error) {
	raw, err := os.ReadFile(paths.Catalog())
	if err != nil {
		if os.IsNotExist(err) {
			return &Catalog{
				Marketplaces: map[string]Marketplace{},
				Skills:       map[string]Skill{},
				MCP:          map[string]MCPServer{},
			}, nil
		}
		return nil, err
	}
	var c Catalog
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", paths.Catalog(), err)
	}
	if c.Marketplaces == nil {
		c.Marketplaces = map[string]Marketplace{}
	}
	if c.Skills == nil {
		c.Skills = map[string]Skill{}
	}
	if c.MCP == nil {
		c.MCP = map[string]MCPServer{}
	}
	return &c, nil
}

// Save writes the catalog to paths.Catalog(), creating its parent directory
// if needed, with 0600 permissions.
func (c *Catalog) Save() error {
	if err := os.MkdirAll(paths.BundlesDir(), 0o700); err != nil {
		return err
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(paths.Catalog(), out, 0o600)
}

// Validate returns warnings for token-shaped values, unknown marketplace
// source kinds, unsafe or credential-carrying marketplace URLs and refs
// (see marketplaceChecks; the unsafe ones start with "refused:"), and skill
// paths that don't exist on disk.
func (c *Catalog) Validate() []string {
	var warnings []string

	for name, mp := range c.Marketplaces {
		warns, refusals := marketplaceChecks(mp)
		for _, w := range warns {
			warnings = append(warnings, fmt.Sprintf("marketplace %q: %s", name, w))
		}
		// Refusals also fail Compute, so sync never writes them.
		for _, r := range refusals {
			warnings = append(warnings, fmt.Sprintf("refused: marketplace %q: %s (sync will not write it)", name, r))
		}
	}

	for name, sk := range c.Skills {
		if sk.Path == "" {
			warnings = append(warnings, fmt.Sprintf("skill %q: no path set", name))
			continue
		}
		if _, err := os.Stat(sk.Path); err != nil {
			warnings = append(warnings, fmt.Sprintf("skill %q: path %s does not exist", name, sk.Path))
		}
	}

	for name, srv := range c.MCP {
		walkTokenLike(fmt.Sprintf("mcp %q", name), map[string]any(srv), &warnings)
	}

	return warnings
}

// walkTokenLike recursively scans an MCP server spec's values for
// token-shaped strings and appends a warning for each.
func walkTokenLike(ctx string, v any, warnings *[]string) {
	switch val := v.(type) {
	case string:
		if manifest.LooksLikeSecret(val) {
			*warnings = append(*warnings, fmt.Sprintf("%s: value looks like a token/secret (replace it with a ${VAR} reference; `tq secrets audit` lists every copy)", ctx))
		}
	case map[string]any:
		for k, vv := range val {
			walkTokenLike(fmt.Sprintf("%s.%s", ctx, k), vv, warnings)
		}
	case []any:
		for i, vv := range val {
			walkTokenLike(fmt.Sprintf("%s[%d]", ctx, i), vv, warnings)
		}
	}
}
