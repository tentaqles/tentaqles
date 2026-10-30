package bundle

import (
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
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

	// bundleMkts maps each settings key to the catalog entry written under
	// it ("key=catalogName", or a plain name used as both).
	bundleMkts := make(map[string]string, len(b.Marketplaces))
	for _, item := range b.Marketplaces {
		key, catName, err := ParseMarketplaceItem(item)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if prev, dup := bundleMkts[key]; dup && prev != catName {
			errs = append(errs, fmt.Errorf("marketplace key %q listed twice, for catalog entries %q and %q", key, prev, catName))
			continue
		}
		mp, ok := cat.Marketplaces[catName]
		if !ok {
			if key != catName {
				errs = append(errs, fmt.Errorf("marketplace %q: unknown catalog entry %q", key, catName))
			} else {
				errs = append(errs, fmt.Errorf("unknown marketplace %q", catName))
			}
			continue
		}
		bundleMkts[key] = catName
		d.Marketplaces[key] = mp.ToKnownMarketplace()
	}

	for _, p := range b.Plugins {
		if idx := strings.Index(p, "@"); idx >= 0 {
			mkt := p[idx+1:]
			if !listedKey(b.Marketplaces, mkt) {
				errs = append(errs, fmt.Errorf("plugin %q: marketplace %q not listed in bundle.marketplaces", p, mkt))
				continue
			}
			if _, ok := bundleMkts[mkt]; !ok {
				errs = append(errs, fmt.Errorf("plugin %q: unknown marketplace %q", p, mkt))
				continue
			}
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

// ParseMarketplaceItem splits a claude.bundle.marketplaces item into the
// settings key it is written under and the catalog entry it names.
// "key=catalogName" writes catalog entry catalogName under settings key key
// (plugins still name the key: "plugin@key"); a plain "name" is both. Both
// halves of an alias must be valid names (validName) without "=" or "@".
func ParseMarketplaceItem(item string) (key, catalogName string, err error) {
	k, c, aliased := strings.Cut(item, "=")
	if !aliased {
		return item, item, nil
	}
	for _, part := range []string{k, c} {
		if !validName(part) || strings.ContainsAny(part, "=@") || strings.TrimSpace(part) != part {
			return "", "", fmt.Errorf("invalid marketplace alias %q: want key=catalogName, both plain names", item)
		}
	}
	return k, c, nil
}

// listedKey reports whether key is the settings key of one of items,
// including items that failed to resolve.
func listedKey(items []string, key string) bool {
	for _, item := range items {
		if k, _, err := ParseMarketplaceItem(item); err == nil && k == key {
			return true
		}
	}
	return false
}
