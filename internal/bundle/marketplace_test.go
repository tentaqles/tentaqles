package bundle

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"gopkg.in/yaml.v3"
)

// A github or git marketplace pinned to a tag, with autoUpdate, renders the
// ref and the marketplace.json path inside the source and autoUpdate beside
// it; unset optional fields stay out so Claude Code's defaults apply.
func TestMarketplace_ToKnownMarketplace_RefPathAutoUpdate(t *testing.T) {
	gh := Marketplace{Source: "github", Repo: "tentaqles/tentaqles", Ref: "v0.8.0", AutoUpdate: true}
	want := map[string]any{
		"source":     map[string]any{"source": "github", "repo": "tentaqles/tentaqles", "ref": "v0.8.0"},
		"autoUpdate": true,
	}
	if got := gh.ToKnownMarketplace(); !reflect.DeepEqual(got, want) {
		t.Fatalf("github: got %+v want %+v", got, want)
	}

	g := Marketplace{Source: "git", URL: "https://gitlab.example.com/acme/plugins.git", Ref: "main", Path: "mp/marketplace.json"}
	wantG := map[string]any{"source": map[string]any{
		"source": "git", "url": "https://gitlab.example.com/acme/plugins.git", "ref": "main", "path": "mp/marketplace.json",
	}}
	if got := g.ToKnownMarketplace(); !reflect.DeepEqual(got, wantG) {
		t.Fatalf("git: got %+v want %+v", got, wantG)
	}

	// For a directory source the path is the folder, and autoUpdate is
	// still written beside the source.
	d := Marketplace{Source: "directory", Path: "/p", AutoUpdate: true}
	wantD := map[string]any{"source": map[string]any{"source": "directory", "path": "/p"}, "autoUpdate": true}
	if got := d.ToKnownMarketplace(); !reflect.DeepEqual(got, wantD) {
		t.Fatalf("directory: got %+v want %+v", got, wantD)
	}
}

func TestCatalog_Validate_MarketplaceKinds(t *testing.T) {
	c := &Catalog{Marketplaces: map[string]Marketplace{
		"gh":      {Source: "github", Repo: "tentaqles/tentaqles", Ref: "v0.8.0", AutoUpdate: true},
		"git":     {Source: "git", URL: "https://example.com/x.git", Ref: "main"},
		"url":     {Source: "url", URL: "https://example.com/marketplace.json"},
		"dir":     {Source: "directory", Path: "/p"},
		"old":     {Source: "local", Path: "/p"},
		"norepo":  {Source: "github"},
		"nourl":   {Source: "git"},
		"unknown": {Source: "svn"},
	}}
	got := c.Validate()
	sort.Strings(got)
	want := []string{
		`marketplace "norepo": github source has no repo`,
		`marketplace "nourl": git source has no url`,
		`marketplace "unknown": unknown source kind "svn"`,
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Validate = %q, want %q", got, want)
	}
}

// The catalog YAML a user writes for the tentaqles marketplace loads into
// the fields ToKnownMarketplace renders, and marshals back with them.
func TestCatalog_YAMLRefAutoUpdate(t *testing.T) {
	var c Catalog
	src := "marketplaces:\n  tentaqles:\n    source: github\n    repo: tentaqles/tentaqles\n    ref: v0.8.0\n    autoUpdate: true\n"
	if err := yaml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	want := Marketplace{Source: "github", Repo: "tentaqles/tentaqles", Ref: "v0.8.0", AutoUpdate: true}
	if got := c.Marketplaces["tentaqles"]; got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	out, err := yaml.Marshal(c.Marketplaces["tentaqles"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "autoUpdate: true") || !strings.Contains(string(out), "ref: v0.8.0") {
		t.Fatalf("marshal lost fields: %s", out)
	}
}

// A github marketplace with a ref and marketplace.json path, and autoUpdate
// set on the settings.json entry, captures into the catalog and renders back
// to the same entry.
func TestCapture_GithubRefAutoUpdateRoundTrips(t *testing.T) {
	dir := t.TempDir()
	src := `{"source": "github", "repo": "tentaqles/tentaqles", "ref": "v0.8.0", "path": ".claude-plugin/marketplace.json"}`
	writeFile(t, filepath.Join(dir, "settings.json"), `{"extraKnownMarketplaces": {"tentaqles": {"source": `+src+`, "autoUpdate": true}}}`)
	// known_marketplaces.json carries no autoUpdate here: the settings
	// entry is where it is set.
	writeFile(t, filepath.Join(dir, "plugins", "known_marketplaces.json"), `{"tentaqles": {"source": `+src+`, "installLocation": "x"}}`)

	c, err := Capture(dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	mp := c.Catalog.Marketplaces["tentaqles"]
	want := Marketplace{Source: "github", Repo: "tentaqles/tentaqles", Ref: "v0.8.0", Path: ".claude-plugin/marketplace.json", AutoUpdate: true}
	if mp != want {
		t.Fatalf("captured %+v, want %+v (warnings %v)", mp, want, c.Warnings)
	}
	if w := (&Catalog{Marketplaces: c.Catalog.Marketplaces}).Validate(); len(w) != 0 {
		t.Fatalf("captured github source should validate: %v", w)
	}
	settings, err := ReadJSONMap(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	orig := settings["extraKnownMarketplaces"].(map[string]any)["tentaqles"]
	if got := mp.ToKnownMarketplace(); !reflect.DeepEqual(normalizeJSON(got), normalizeJSON(orig)) {
		t.Fatalf("render: got %+v want %+v", got, orig)
	}
}

// Without a settings entry, autoUpdate falls back to known_marketplaces.json.
func TestCapture_AutoUpdateFallsBackToKnown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "settings.json"), `{"enabledPlugins": {"p@acme": true}}`)
	writeFile(t, filepath.Join(dir, "plugins", "known_marketplaces.json"), `{"acme": {"source": {"source": "github", "repo": "acme/p"}, "autoUpdate": true}}`)
	c, err := Capture(dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if got := c.Catalog.Marketplaces["acme"]; !got.AutoUpdate {
		t.Fatalf("captured %+v, want AutoUpdate", got)
	}
}

// A git source captures with its url and ref.
func TestCapture_GitSource(t *testing.T) {
	dir := t.TempDir()
	src := `{"source": {"source": "git", "url": "https://example.com/acme/plugins.git", "ref": "main"}}`
	writeFile(t, filepath.Join(dir, "settings.json"), `{"extraKnownMarketplaces": {"acme": `+src+`}}`)
	writeFile(t, filepath.Join(dir, "plugins", "known_marketplaces.json"), `{"acme": `+src+`}`)
	c, err := Capture(dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	want := Marketplace{Source: "git", URL: "https://example.com/acme/plugins.git", Ref: "main"}
	if got := c.Catalog.Marketplaces["acme"]; got != want {
		t.Fatalf("captured %+v, want %+v", got, want)
	}
}

// Moving the catalog's ref to a new tag, or switching autoUpdate on, is
// reported as marketplace-source drift naming the ref on both sides; a sync
// clears it.
func TestDiff_ReportsMarketplaceRefAndAutoUpdateDrift(t *testing.T) {
	isolateHome(t)

	skillSrc := t.TempDir()
	os.WriteFile(filepath.Join(skillSrc, "SKILL.md"), []byte("# s"), 0o644)
	cat := testCatalog(t, t.TempDir(), skillSrc)
	cat.Marketplaces["mymkt"] = Marketplace{Source: "github", Repo: "tentaqles/tentaqles", Ref: "v0.7.0"}
	ws, _ := setupWorkspace(t, &manifest.Bundle{Marketplaces: []string{"mymkt"}})
	if _, err := Sync(ws, cat, Options{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if drifts, err := Diff(ws, cat); err != nil || len(drifts) != 0 {
		t.Fatalf("after sync: %+v %v", drifts, err)
	}

	sourceDrift := func() *Drift {
		t.Helper()
		drifts, err := Diff(ws, cat)
		if err != nil {
			t.Fatal(err)
		}
		for i := range drifts {
			if drifts[i].Kind == "marketplace-source" && drifts[i].Name == "mymkt" {
				return &drifts[i]
			}
		}
		return nil
	}

	cat.Marketplaces["mymkt"] = Marketplace{Source: "github", Repo: "tentaqles/tentaqles", Ref: "v0.8.0"}
	got := sourceDrift()
	if got == nil || !strings.Contains(got.Detail, "settings has github tentaqles/tentaqles@v0.7.0") || !strings.Contains(got.Detail, "the catalog has github tentaqles/tentaqles@v0.8.0") {
		t.Fatalf("want ref drift naming both refs, got %+v", got)
	}

	cat.Marketplaces["mymkt"] = Marketplace{Source: "github", Repo: "tentaqles/tentaqles", Ref: "v0.7.0", AutoUpdate: true}
	got = sourceDrift()
	if got == nil || !strings.Contains(got.Detail, "the catalog has github tentaqles/tentaqles@v0.7.0 autoUpdate") {
		t.Fatalf("want autoUpdate drift, got %+v", got)
	}

	if _, err := Sync(ws, cat, Options{}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := sourceDrift(); got != nil {
		t.Fatalf("after second sync: %+v", got)
	}
}

func TestDescribeSource(t *testing.T) {
	cases := []struct {
		in   Marketplace
		want string
	}{
		{Marketplace{Source: "github", Repo: "a/b"}, "github a/b"},
		{Marketplace{Source: "github", Repo: "a/b", Ref: "v1", Path: "x/marketplace.json", AutoUpdate: true}, "github a/b@v1 path x/marketplace.json autoUpdate"},
		{Marketplace{Source: "git", URL: "https://e.com/r.git", Ref: "main"}, "git https://e.com/r.git@main"},
		{Marketplace{Source: "directory", Path: "/p"}, "directory /p"},
	}
	for _, c := range cases {
		if got := describeSource(c.in.ToKnownMarketplace()); got != c.want {
			t.Errorf("describeSource(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
	off := map[string]any{"source": map[string]any{"source": "github", "repo": "a/b"}, "autoUpdate": false}
	if got := describeSource(off); got != "github a/b autoUpdate off" {
		t.Errorf("explicit false: %q", got)
	}
}
