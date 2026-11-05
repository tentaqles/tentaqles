package cmd

import (
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// scopeNote is what an identity deny adds after its reason (PRD v2 §6.1
// step 5, §7.3): the identity's scope, so the model reports the mistake
// instead of retrying. It is the one place the deny message reads the
// scope; identityScope is the seam Manifest.Scope() (the provider scope of
// PRD v2 §6) replaces.
func scopeNote(ws *resolve.Workspace) string {
	if ws == nil || ws.Manifest == nil {
		return "\nScope: no trusted identity here; report this to the developer instead of retrying."
	}
	return "\nScope of identity " + ws.Name + ": " + identityScope(ws.Manifest) +
		". Anything outside it is denied; report this to the developer instead of retrying."
}

// identityScope lists what the manifest names today: the git host, the
// cloud provider and its cloud.also, the MCP servers of the bundle.
func identityScope(m *manifest.Manifest) string {
	var parts []string
	host := m.Git.Host
	if host == "" {
		host = m.Git.Provider
	}
	if host != "" {
		parts = append(parts, "git "+host)
	}
	clouds := []string{}
	if p := cloudString(m.Cloud, "provider"); p != "" {
		clouds = append(clouds, manifest.CloudProviderName(p))
	}
	for _, x := range cloudStrings(m.Cloud, "also") {
		clouds = append(clouds, manifest.CloudProviderName(x))
	}
	if len(clouds) > 0 {
		parts = append(parts, "cloud "+strings.Join(clouds, ", "))
	} else {
		parts = append(parts, "no cloud")
	}
	if m.Claude.Bundle != nil && len(m.Claude.Bundle.MCP) > 0 {
		parts = append(parts, "MCP "+strings.Join(m.Claude.Bundle.MCP, ", "))
	}
	return strings.Join(parts, "; ")
}
