package manifest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/providers"
	"gopkg.in/yaml.v3"
)

// The provider scope (PRD v2 §6, ADR-0007): the catalog ids an identity may touch. It is the
// single source of truth the guard's identity category, the PATH, the environment and the
// harness's MCP list are built from.

// GuardFlag is the top-level `guard:` scalar that switches the guard's v2 identity rules on
// (DISCOVERY §9: every new behaviour ships behind a manifest flag). "v2" turns them on; "",
// absent and "v1" keep the v1 rules. The guard block tq 0.9.0 dropped was a mapping; a
// mapping is read as no flag (Load lists it in Manifest.Ignored).
type GuardFlag string

// UnmarshalYAML reads a scalar flag and ignores the dropped mapping.
func (g *GuardFlag) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		*g = ""
		return nil
	}
	*g = GuardFlag(strings.ToLower(strings.TrimSpace(n.Value)))
	return nil
}

func (g GuardFlag) validate() error {
	switch g {
	case "", "v1", "v2":
		return nil
	}
	return fmt.Errorf("guard: must be v1 or v2, got %q", string(g))
}

// GuardV2 reports whether the manifest turns the v2 identity rules on (guard: v2).
func (m *Manifest) GuardV2() bool { return m.Guard == "v2" }

// gitProviderIDs maps git.provider to its catalog id.
var gitProviderIDs = map[string]string{"github": "gh", "gitlab": "glab", "azure-devops": "azure-devops", "bitbucket": "bitbucket", "gitea": "gitea"}

// Scope returns the sorted catalog ids this identity may touch: IdentityNames (the
// identities block, or claude and gh for a v1 manifest, plus every cloud of the cloud
// block), the git provider, the chat, email and data_platform providers, the agents, the
// providers whose MCP server the bundle lists, and the optional `scope:` list (providers the
// identity uses without a private config home, such as vercel or terraform). An existing
// manifest needs no new field: its scope is what it already declares.
func (m *Manifest) Scope() []string {
	seen := map[string]bool{}
	add := func(id string) {
		if id = strings.TrimSpace(id); id != "" && id != "none" {
			seen[id] = true
		}
	}
	for _, id := range m.IdentityNames() {
		add(id)
	}
	gp := strings.ToLower(strings.TrimSpace(m.Git.Provider))
	if id, ok := gitProviderIDs[gp]; ok {
		add(id)
	} else if _, ok := providers.MustLoad().Get(gp); ok {
		add(gp)
	}
	for _, s := range []*Service{m.Chat, m.Email, m.DataPlatform} {
		if s.Enabled() {
			add(s.Provider)
		}
	}
	for _, a := range m.Agents {
		add(a)
	}
	if m.Claude.Bundle != nil && len(m.Claude.Bundle.MCP) > 0 {
		for _, p := range providers.MustLoad().All() {
			if contains(m.Claude.Bundle.MCP, p.MCPName()) {
				add(p.ID)
			}
		}
	}
	for _, id := range m.ExtraScope {
		add(id)
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// validateScope checks the scope: list: catalog ids, each once.
func (m *Manifest) validateScope() error {
	seen := map[string]bool{}
	for _, id := range m.ExtraScope {
		if _, ok := providers.MustLoad().Get(id); !ok {
			return fmt.Errorf("scope: unknown provider %q (a catalog id: tq providers list)", id)
		}
		if seen[id] {
			return fmt.Errorf("scope: %q is listed twice", id)
		}
		seen[id] = true
	}
	return nil
}
