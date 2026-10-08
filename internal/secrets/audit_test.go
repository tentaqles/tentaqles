package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, p, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAudit(t *testing.T) {
	root := t.TempDir()
	tok := j("eyJ", "hbGciOiJIUzI1NiJ9.eyJ", "zdWIiOiIxIn0.abcdefghij")
	put(t, filepath.Join(root, "client", ".mcp.json"), `{"mcpServers":{"n8n":{"env":{"TOKEN":"`+tok+`"}}}}`)
	put(t, filepath.Join(root, "client", "node_modules", "x", ".mcp.json"), `{"t":"`+tok+`"}`)
	put(t, filepath.Join(root, "client", "src", "main.py"), tok) // not a config file
	put(t, filepath.Join(root, "safe", ".mcp.json"), `{"env":{"TOKEN":"${N8N_TOKEN}"}}`)
	put(t, filepath.Join(root, "repo", ".env"), "X=1\n")
	put(t, filepath.Join(root, "repo", ".env.example"), "X=\n")
	put(t, filepath.Join(root, "ignored", ".env"), "X=1\n")
	extra := filepath.Join(t.TempDir(), "catalog.yaml")
	put(t, extra, "mcp:\n  pg:\n    env:\n      URL: "+j("postgres://u:", "pw12345@h/db")+"\n")

	hits := Audit(AuditOptions{
		Roots: []string{root},
		Files: []string{extra},
		IsIgnored: func(p string) (bool, bool) {
			return strings.Contains(p, "ignored"), true
		},
	})
	var got []string
	for _, h := range hits {
		rel, _ := filepath.Rel(root, h.Path)
		if strings.HasPrefix(rel, "..") {
			rel = filepath.Base(h.Path)
		}
		got = append(got, filepath.ToSlash(rel)+":"+h.Pattern)
		if strings.Contains(h.Path+h.Pattern, "abcdefghij") {
			t.Fatal("hit leaks a value")
		}
	}
	want := map[string]bool{
		"client/.mcp.json:jwt":           true,
		"repo/.env:env_not_gitignored":   true,
		"catalog.yaml:connection_string": true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected hit %s (all: %v)", g, got)
		}
	}
}
