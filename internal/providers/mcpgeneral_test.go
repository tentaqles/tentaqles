package providers

import "testing"

// The general-purpose, provider-free MCP servers (ADR-0007, the owner's hybrid list of
// 2026-11-05): each named server is there, validates, and carries something to match by.
func TestMCPGeneral(t *testing.T) {
	g, err := MCPGeneral()
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, s := range g {
		have[s.Name] = true
		if err := s.Match.Validate(); err != nil {
			t.Errorf("%s: %v", s.Name, err)
		}
		if len(s.Match.MCPPackages)+len(s.Match.MCPHosts)+len(s.Match.Images) == 0 {
			t.Errorf("%s: nothing to match an entry by", s.Name)
		}
	}
	for _, n := range []string{"playwright", "context7", "memory", "filesystem", "fetch", "git", "sequential-thinking",
		"time", "everything", "postgres", "sqlite", "puppeteer", "chrome-devtools"} {
		if !have[n] {
			t.Errorf("%s missing from the general list", n)
		}
	}
}
