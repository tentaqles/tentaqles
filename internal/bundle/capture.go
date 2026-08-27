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
			if !validName(e.Name()) {
				c.Warnings = append(c.Warnings, fmt.Sprintf("skill %q: invalid name, skipped", e.Name()))
				continue
			}
			names = append(names, e.Name())
			c.Catalog.Skills[e.Name()] = Skill{Path: filepath.Join(skillsRoot, e.Name())}
		}
		sort.Strings(names)
		c.Bundle.Skills = names
	}

	// mcp servers: <dir>/.claude.json, else (for the default profile only)
	// <home>/.claude.json, plus <dir>/.mcp.json. Earlier sources win.
	sources, srcWarnings := mcpSourceFiles(dir)
	c.Warnings = append(c.Warnings, srcWarnings...)

	servers := map[string]any{}
	for _, path := range sources {
		root, err := ReadJSONMap(path)
		if err != nil {
			return c, err
		}
		raw, ok := root["mcpServers"].(map[string]any)
		if !ok {
			continue
		}
		for name, spec := range raw {
			if _, exists := servers[name]; exists {
				continue // earlier source wins
			}
			servers[name] = spec
		}
	}

	if len(servers) > 0 {
		var names []string
		for name, spec := range servers {
			specMap, ok := spec.(map[string]any)
			if !ok {
				continue
			}
			if !validName(name) {
				c.Warnings = append(c.Warnings, fmt.Sprintf("mcp server %q: invalid name, skipped", name))
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

// mcpSourceFiles returns, in precedence order, the files Capture reads
// mcpServers from for dir, along with warnings naming each file used and
// each candidate that was absent.
func mcpSourceFiles(dir string) ([]string, []string) {
	var sources, warnings []string

	dirClaudeJSON := filepath.Join(dir, ".claude.json")
	if fileExists(dirClaudeJSON) {
		sources = append(sources, dirClaudeJSON)
		warnings = append(warnings, fmt.Sprintf("mcp: read %s", dirClaudeJSON))
	} else {
		warnings = append(warnings, fmt.Sprintf("mcp: %s absent", dirClaudeJSON))
		if home, err := os.UserHomeDir(); err == nil && isDefaultClaudeDir(dir, home) {
			homeClaudeJSON := filepath.Join(home, ".claude.json")
			if fileExists(homeClaudeJSON) {
				sources = append(sources, homeClaudeJSON)
				warnings = append(warnings, fmt.Sprintf("mcp: read %s", homeClaudeJSON))
			} else {
				warnings = append(warnings, fmt.Sprintf("mcp: %s absent", homeClaudeJSON))
			}
		}
	}

	dotMCP := filepath.Join(dir, ".mcp.json")
	if fileExists(dotMCP) {
		sources = append(sources, dotMCP)
		warnings = append(warnings, fmt.Sprintf("mcp: read %s", dotMCP))
	} else {
		warnings = append(warnings, fmt.Sprintf("mcp: %s absent", dotMCP))
	}

	return sources, warnings
}

// isDefaultClaudeDir reports whether dir resolves to <home>/.claude.
func isDefaultClaudeDir(dir, home string) bool {
	want := filepath.Join(home, ".claude")
	a, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	b, err := filepath.Abs(want)
	if err != nil {
		return false
	}
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
