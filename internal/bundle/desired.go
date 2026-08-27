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

	for _, p := range b.Plugins {
		if idx := strings.Index(p, "@"); idx >= 0 {
			mkt := p[idx+1:]
			inBundle := false
			for _, m := range b.Marketplaces {
				if m == mkt {
					inBundle = true
					break
				}
			}
			if !inBundle {
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
		sk, ok := cat.Skills[s]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown skill %q", s))
			continue
		}
		d.Skills[s] = sk.Path
	}

	for _, m := range b.MCP {
		srv, ok := cat.MCP[m]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown mcp server %q", m))
			continue
		}
		d.MCP[m] = srv
	}

	return d, errs
}
