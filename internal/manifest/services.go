package manifest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/providers"
)

// Company services (spec B §6): the chat and email connectors the agent uses over MCP, the data
// platform and the agent CLIs this workspace logs into. Every block is optional, and like the
// rest of the manifest they hold names only, never secrets.

// TrackerProviders are the tracker accounts tq secret keeps a token for.
var TrackerProviders = []string{"github", "azure-devops", "jira", "linear", "asana"}

// ChatProviders and EmailProviders are reached through the agent's MCP connectors only.
var (
	ChatProviders  = []string{"slack", "teams", "none"}
	EmailProviders = []string{"gmail", "outlook", "none"}
)

// Service is a one-provider block (chat, email, data_platform).
type Service struct {
	Provider string `yaml:"provider"`
}

// Enabled is whether the block names a provider other than none.
func (s *Service) Enabled() bool { return s != nil && s.Provider != "" && s.Provider != "none" }

func (s *Service) validate(field string, known []string) error {
	if s == nil {
		return nil
	}
	if s.Provider == "" {
		return fmt.Errorf("%s.provider is required (%s)", field, strings.Join(known, ", "))
	}
	if !contains(known, s.Provider) {
		return fmt.Errorf("%s.provider: unknown provider %q (known: %s)", field, s.Provider, strings.Join(known, ", "))
	}
	return nil
}

// catalogIDs lists the catalog providers of one category.
func catalogIDs(cat string) []string {
	var out []string
	for _, p := range providers.MustLoad().ByCategory(cat) {
		out = append(out, p.ID)
	}
	sort.Strings(out)
	return out
}

// DataPlatforms are the accepted data_platform providers: the catalog's data ids, plus none.
func DataPlatforms() []string { return append(catalogIDs("data"), "none") }

// AgentCLIs are the accepted agents entries: the catalog's agent ids.
func AgentCLIs() []string { return catalogIDs("agent") }

// ValidateServices checks the chat, email, data_platform and agents blocks (Load runs it;
// writers call it before writing).
func (m *Manifest) ValidateServices() error {
	if err := m.Chat.validate("chat", ChatProviders); err != nil {
		return err
	}
	if err := m.Email.validate("email", EmailProviders); err != nil {
		return err
	}
	if m.DataPlatform != nil {
		if err := m.DataPlatform.validate("data_platform", DataPlatforms()); err != nil {
			return err
		}
	}
	if len(m.Agents) > 0 {
		known := AgentCLIs()
		seen := map[string]bool{}
		for _, a := range m.Agents {
			if !contains(known, a) {
				return fmt.Errorf("agents: unknown agent CLI %q (known: %s)", a, strings.Join(known, ", "))
			}
			if seen[a] {
				return fmt.Errorf("agents: %q is listed twice", a)
			}
			seen[a] = true
		}
	}
	return nil
}
