package bundle

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func aliasCatalog() *Catalog {
	return &Catalog{Marketplaces: map[string]Marketplace{
		"tentaqles":     {Source: "github", Repo: "tentaqles/tentaqles", AutoUpdate: true},
		"tentaqles-dev": {Source: "directory", Path: `C:\repos\tentaqles\tentaqles\plugin`},
	}}
}

func TestParseMarketplaceItem(t *testing.T) {
	ok := []struct{ in, key, cat string }{
		{"tentaqles", "tentaqles", "tentaqles"},
		{"tentaqles=tentaqles-dev", "tentaqles", "tentaqles-dev"},
	}
	for _, c := range ok {
		k, n, err := ParseMarketplaceItem(c.in)
		if err != nil || k != c.key || n != c.cat {
			t.Errorf("%q: got (%q, %q, %v), want (%q, %q)", c.in, k, n, err, c.key, c.cat)
		}
	}
	for _, bad := range []string{"=x", "x=", "=", "a=b=c", "a/b=c", "a=..", "a@b=c", "a= b", "a=b\tc"} {
		if _, _, err := ParseMarketplaceItem(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

// key=catalogName writes the catalog entry under the settings key, and a
// plugin naming the key resolves.
func TestCompute_MarketplaceAlias(t *testing.T) {
	cat := aliasCatalog()
	b := &manifest.Bundle{
		Marketplaces: []string{"tentaqles=tentaqles-dev"},
		Plugins:      []string{"tentaqles@tentaqles"},
	}
	d, errs := Compute(b, cat)
	if len(errs) != 0 {
		t.Fatalf("errs: %v", errs)
	}
	if !reflect.DeepEqual(d.Marketplaces, map[string]map[string]any{"tentaqles": cat.Marketplaces["tentaqles-dev"].ToKnownMarketplace()}) {
		t.Fatalf("Marketplaces = %+v", d.Marketplaces)
	}
	if !d.EnabledPlugins["tentaqles@tentaqles"] {
		t.Fatalf("EnabledPlugins = %v", d.EnabledPlugins)
	}

	// A plain name still means key == catalogName.
	d, errs = Compute(&manifest.Bundle{Marketplaces: []string{"tentaqles"}, Plugins: []string{"tentaqles@tentaqles"}}, cat)
	if len(errs) != 0 || !reflect.DeepEqual(d.Marketplaces["tentaqles"], cat.Marketplaces["tentaqles"].ToKnownMarketplace()) {
		t.Fatalf("plain: %+v %v", d.Marketplaces, errs)
	}

	// The plugin names the key, not the catalog entry.
	_, errs = Compute(&manifest.Bundle{Marketplaces: []string{"tentaqles=tentaqles-dev"}, Plugins: []string{"tentaqles@tentaqles-dev"}}, cat)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `marketplace "tentaqles-dev" not listed`) {
		t.Fatalf("plugin naming the catalog entry: %v", errs)
	}
}

func TestCompute_MarketplaceAliasErrors(t *testing.T) {
	cat := aliasCatalog()
	_, errs := Compute(&manifest.Bundle{Marketplaces: []string{"tentaqles=nope"}, Plugins: []string{"tentaqles@tentaqles"}}, cat)
	if len(errs) != 2 || !strings.Contains(errs[0].Error(), `marketplace "tentaqles": unknown catalog entry "nope"`) ||
		!strings.Contains(errs[1].Error(), `plugin "tentaqles@tentaqles": unknown marketplace "tentaqles"`) {
		t.Fatalf("unknown alias target: %v", errs)
	}

	_, errs = Compute(&manifest.Bundle{Marketplaces: []string{"tentaqles=a/b"}}, cat)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "invalid marketplace alias") {
		t.Fatalf("invalid alias: %v", errs)
	}

	_, errs = Compute(&manifest.Bundle{Marketplaces: []string{"tentaqles", "tentaqles=tentaqles-dev"}}, cat)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), `listed twice`) {
		t.Fatalf("duplicate key: %v", errs)
	}
}

// Sync writes the aliased entry under the key, Diff is clean afterwards and
// reports drift by key when the alias target changes.
func TestSyncDiff_MarketplaceAlias(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# s"), 0o644)
	cat := testCatalog(t, t.TempDir(), skillSrc)
	for k, v := range aliasCatalog().Marketplaces {
		cat.Marketplaces[k] = v
	}
	ws, _ := setupWorkspace(t, &manifest.Bundle{
		Marketplaces: []string{"tentaqles=tentaqles-dev"},
		Plugins:      []string{"tentaqles@tentaqles"},
	})
	if _, err := Sync(ws, cat, Options{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	settings, err := ReadJSONMap(filepath.Join(identityDirForTest(ws), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	mkts, _ := settings["extraKnownMarketplaces"].(map[string]any)
	if _, ok := mkts["tentaqles-dev"]; ok || len(mkts) != 1 {
		t.Fatalf("extraKnownMarketplaces = %+v", mkts)
	}
	if got := describeSource(mkts["tentaqles"]); got != `directory C:\repos\tentaqles\tentaqles\plugin` {
		t.Fatalf("settings key tentaqles has %s", got)
	}
	if drifts, err := Diff(ws, cat); err != nil || len(drifts) != 0 {
		t.Fatalf("after sync: %+v %v", drifts, err)
	}

	cat.Marketplaces["tentaqles-dev"] = Marketplace{Source: "directory", Path: "/elsewhere"}
	drifts, err := Diff(ws, cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(drifts) != 1 || drifts[0].Kind != "marketplace-source" || drifts[0].Name != "tentaqles" {
		t.Fatalf("want marketplace-source drift on key tentaqles, got %+v", drifts)
	}
}
