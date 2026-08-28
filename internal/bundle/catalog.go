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
// Claude Code expects under settings.extraKnownMarketplaces.
type Marketplace struct {
	Source string `yaml:"source"` // github|url|local
	Repo   string `yaml:"repo,omitempty"`
	URL    string `yaml:"url,omitempty"`
	Path   string `yaml:"path,omitempty"`
}

// ToKnownMarketplace renders m in the {"source": {...}} shape used by
// settings.json's extraKnownMarketplaces map.
func (m Marketplace) ToKnownMarketplace() map[string]any {
	src := map[string]any{"source": m.Source}
	switch m.Source {
	case "github":
		src["repo"] = m.Repo
	case "url":
		src["url"] = m.URL
	case "local":
		src["path"] = m.Path
	}
	return map[string]any{"source": src}
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
// source kinds, and skill paths that don't exist on disk.
func (c *Catalog) Validate() []string {
	var warnings []string

	for name, mp := range c.Marketplaces {
		switch mp.Source {
		case "github", "url", "local":
		default:
			warnings = append(warnings, fmt.Sprintf("marketplace %q: unknown source kind %q", name, mp.Source))
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
			*warnings = append(*warnings, fmt.Sprintf("%s: value looks like a token/secret", ctx))
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
