package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
	"gopkg.in/yaml.v3"
)

// Captured is what Capture reconstructs from an existing Claude identity
// dir: a manifest Bundle referencing catalog entries, and the Catalog
// entries themselves, ready to be merged into the real catalog and a
// workspace's manifest.
type Captured struct {
	Bundle   manifest.Bundle
	Catalog  Catalog
	Warnings []string
}

// Capture reads dir's settings.json, plugins/known_marketplaces.json,
// skills/*, and .claude.json to reconstruct a Bundle + Catalog.
func Capture(dir string) (Captured, error) {
	var c Captured
	c.Catalog = Catalog{
		Marketplaces: map[string]Marketplace{},
		Skills:       map[string]Skill{},
		MCP:          map[string]MCPServer{},
	}

	settings, err := ReadJSONMap(filepath.Join(dir, "settings.json"))
	if err != nil {
		return c, err
	}

	enabledPlugins := map[string]bool{}
	if raw, ok := settings["enabledPlugins"].(map[string]any); ok {
		for k, v := range raw {
			if b, ok := v.(bool); ok && b {
				enabledPlugins[k] = true
			}
		}
	}

	referencedMkts := map[string]bool{}
	var plugins []string
	for name := range enabledPlugins {
		plugins = append(plugins, name)
		if idx := strings.Index(name, "@"); idx >= 0 {
			referencedMkts[name[idx+1:]] = true
		}
	}
	sort.Strings(plugins)
	c.Bundle.Plugins = plugins

	extraMkts := map[string]bool{}
	if raw, ok := settings["extraKnownMarketplaces"].(map[string]any); ok {
		for k := range raw {
			extraMkts[k] = true
		}
	}

	mktNames := map[string]bool{}
	for k := range referencedMkts {
		mktNames[k] = true
	}
	for k := range extraMkts {
		mktNames[k] = true
	}

	knownPath := filepath.Join(dir, "plugins", "known_marketplaces.json")
	known, err := ReadJSONMap(knownPath)
	if err != nil {
		return c, err
	}
	for name, raw := range known {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		src, ok := entry["source"].(map[string]any)
		if !ok {
			c.Warnings = append(c.Warnings, fmt.Sprintf("marketplace %q: unrecognized source shape, skipped", name))
			continue
		}
		mp, ok := marketplaceFromSource(src)
		if !ok {
			c.Warnings = append(c.Warnings, fmt.Sprintf("marketplace %q: unknown source kind, skipped", name))
			continue
		}
		c.Catalog.Marketplaces[name] = mp
	}

	var mkts []string
	for name := range mktNames {
		mkts = append(mkts, name)
	}
	sort.Strings(mkts)
	c.Bundle.Marketplaces = mkts

	// skills: every dir under <dir>/skills
	skillsRoot := filepath.Join(dir, "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			names = append(names, e.Name())
			c.Catalog.Skills[e.Name()] = Skill{Path: filepath.Join(skillsRoot, e.Name())}
		}
		sort.Strings(names)
		c.Bundle.Skills = names
	}

	// mcp servers from .claude.json
	claudeJSON, err := ReadJSONMap(filepath.Join(dir, ".claude.json"))
	if err != nil {
		return c, err
	}
	if raw, ok := claudeJSON["mcpServers"].(map[string]any); ok {
		var names []string
		for name, spec := range raw {
			specMap, ok := spec.(map[string]any)
			if !ok {
				continue
			}
			names = append(names, name)
			c.Catalog.MCP[name] = MCPServer(specMap)
		}
		sort.Strings(names)
		c.Bundle.MCP = names
	}

	return c, nil
}

// marketplaceFromSource maps a known_marketplaces.json source object into a
// Marketplace. Returns ok=false for a source kind Capture doesn't
// recognize, unless it carries a url, in which case it's captured as a
// "url" source.
func marketplaceFromSource(src map[string]any) (Marketplace, bool) {
	kind, _ := src["source"].(string)
	switch kind {
	case "github":
		repo, _ := src["repo"].(string)
		return Marketplace{Source: "github", Repo: repo}, true
	case "url":
		url, _ := src["url"].(string)
		return Marketplace{Source: "url", URL: url}, true
	case "local":
		path, _ := src["path"].(string)
		return Marketplace{Source: "local", Path: path}, true
	default:
		if url, ok := src["url"].(string); ok && url != "" {
			return Marketplace{Source: "url", URL: url}, true
		}
		return Marketplace{}, false
	}
}

// BundleYAML renders c.Bundle as a "claude:\n  bundle:\n    ..." YAML
// fragment ready to paste into a manifest's claude.bundle key.
func (c Captured) BundleYAML() string {
	out, err := yaml.Marshal(map[string]any{
		"claude": map[string]any{
			"bundle": c.Bundle,
		},
	})
	if err != nil {
		return ""
	}
	return string(out)
}
