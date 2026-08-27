package bundle

import (
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/cli/internal/manifest"
)

// Desired is the fully-resolved set of catalog entries a workspace's bundle
// should materialize, ready to be diffed/synced against on-disk state.
type Desired struct {
	EnabledPlugins map[string]bool
	Marketplaces   map[string]map[string]any
	Skills         map[string]string // name -> srcPath
	MCP            map[string]MCPServer
}

// Compute resolves b against cat, returning the desired state and a list of
// errors for any names b references that aren't in the catalog.
func Compute(b *manifest.Bundle, cat *Catalog) (Desired, []error) {
	d := Desired{
		EnabledPlugins: map[string]bool{},
		Marketplaces:   map[string]map[string]any{},
		Skills:         map[string]string{},
		MCP:            map[string]MCPServer{},
	}
	var errs []error

	if b == nil {
		return d, errs
	}

	for _, mkt := range b.Marketplaces {
		mp, ok := cat.Marketplaces[mkt]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown marketplace %q", mkt))
			continue
		}
		d.Marketplaces[mkt] = mp.ToKnownMarketplace()
	}

	bundleMkts := make(map[string]bool, len(b.Marketplaces))
	for _, m := range b.Marketplaces {
		bundleMkts[m] = true
	}

	for _, p := range b.Plugins {
		if idx := strings.Index(p, "@"); idx >= 0 {
			mkt := p[idx+1:]
			if !bundleMkts[mkt] {
				errs = append(errs, fmt.Errorf("plugin %q: marketplace %q not listed in bundle.marketplaces", p, mkt))
				continue
			}
			mp, ok := cat.Marketplaces[mkt]
			if !ok {
				errs = append(errs, fmt.Errorf("plugin %q: unknown marketplace %q", p, mkt))
				continue
			}
			d.Marketplaces[mkt] = mp.ToKnownMarketplace()
			d.EnabledPlugins[p] = true
		} else {
			d.EnabledPlugins[p] = true
		}
	}

	for _, s := range b.Skills {
		if !validName(s) {
			errs = append(errs, fmt.Errorf("invalid skill name: %q", s))
			continue
		}
		sk, ok := cat.Skills[s]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown skill %q", s))
			continue
		}
		d.Skills[s] = sk.Path
	}

	for _, m := range b.MCP {
		if !validName(m) {
			errs = append(errs, fmt.Errorf("invalid mcp server name: %q", m))
			continue
		}
		srv, ok := cat.MCP[m]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown mcp server %q", m))
			continue
		}
		d.MCP[m] = MCPServer(deepCopy(map[string]any(srv)).(map[string]any))
	}

	return d, errs
}
