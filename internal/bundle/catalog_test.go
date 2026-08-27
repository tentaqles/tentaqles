package bundle

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestCatalog_LoadMissingIsEmpty(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	c, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Marketplaces) != 0 || len(c.Skills) != 0 || len(c.MCP) != 0 {
		t.Fatalf("want empty catalog, got %+v", c)
	}
}

func TestCatalog_RoundTrip(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	c := &Catalog{
		Marketplaces: map[string]Marketplace{
			"aws": {Source: "github", Repo: "anthropics/aws"},
		},
		Skills: map[string]Skill{
			"snowflake-snowpark-patterns": {Path: "/tmp/skills/snowflake"},
		},
		MCP: map[string]MCPServer{
			"github": {"command": "npx", "args": []any{"gh-mcp"}},
		},
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Marketplaces, got.Marketplaces) {
		t.Fatalf("marketplaces mismatch: %+v vs %+v", c.Marketplaces, got.Marketplaces)
	}
	if !reflect.DeepEqual(c.Skills, got.Skills) {
		t.Fatalf("skills mismatch: %+v vs %+v", c.Skills, got.Skills)
	}
	if !reflect.DeepEqual(c.MCP, got.MCP) {
		t.Fatalf("mcp mismatch: %+v vs %+v", c.MCP, got.MCP)
	}
}

func TestCatalog_Validate_WarnsOnTokenAndMissingSkill(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	c := &Catalog{
		Skills: map[string]Skill{
			"missing-skill": {Path: filepath.Join(t.TempDir(), "does-not-exist")},
		},
		MCP: map[string]MCPServer{
			"github": {"env": map[string]any{"GITHUB_TOKEN": "ghp_xxxxxxxxxxxxxxxxxxxxxxxx"}},
		},
	}
	warnings := c.Validate()
	if len(warnings) < 2 {
		t.Fatalf("expected at least 2 warnings, got %v", warnings)
	}
	var hasToken, hasSkill bool
	for _, w := range warnings {
		if containsFold(w, "token") {
			hasToken = true
		}
		if containsFold(w, "missing-skill") {
			hasSkill = true
		}
	}
	if !hasToken {
		t.Errorf("expected a warning mentioning 'token', got %v", warnings)
	}
	if !hasSkill {
		t.Errorf("expected a warning mentioning the missing skill, got %v", warnings)
	}
}

func containsFold(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if equalFold(s[i:i+len(sub)], sub) {
				return true
			}
		}
		return false
	})()
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func TestMarketplace_ToKnownMarketplace(t *testing.T) {
	gh := Marketplace{Source: "github", Repo: "anthropics/x"}
	want := map[string]any{"source": map[string]any{"source": "github", "repo": "anthropics/x"}}
	if got := gh.ToKnownMarketplace(); !reflect.DeepEqual(got, want) {
		t.Fatalf("github: got %+v want %+v", got, want)
	}

	u := Marketplace{Source: "url", URL: "https://example.com/mp.json"}
	wantU := map[string]any{"source": map[string]any{"source": "url", "url": "https://example.com/mp.json"}}
	if got := u.ToKnownMarketplace(); !reflect.DeepEqual(got, wantU) {
		t.Fatalf("url: got %+v want %+v", got, wantU)
	}

	l := Marketplace{Source: "local", Path: "/some/path"}
	wantL := map[string]any{"source": map[string]any{"source": "local", "path": "/some/path"}}
	if got := l.ToKnownMarketplace(); !reflect.DeepEqual(got, wantL) {
		t.Fatalf("local: got %+v want %+v", got, wantL)
	}
}
