package providers

import (
	_ "embed"
	"fmt"
	"sync"

	"gopkg.in/yaml.v3"
)

// The general-purpose, provider-free MCP servers of mcp_general.yaml (ADR-0008): the second
// leg of the owner's hybrid MCP list. A server matching one is kept though no
// provider of the scope claims it.

//go:embed mcp_general.yaml
var mcpGeneralYAML []byte

// GeneralMCP is one provider-free MCP server and what an entry is matched by.
type GeneralMCP struct {
	Name  string `yaml:"name"`
	Match Match  `yaml:"match"`
}

var (
	generalOnce sync.Once
	general     []GeneralMCP
	generalErr  error
)

// MCPGeneral is the embedded list, validated once.
func MCPGeneral() ([]GeneralMCP, error) {
	generalOnce.Do(func() {
		var doc struct {
			Servers []GeneralMCP `yaml:"servers"`
		}
		if err := yaml.Unmarshal(mcpGeneralYAML, &doc); err != nil {
			generalErr = fmt.Errorf("mcp_general.yaml: %w", err)
			return
		}
		for _, s := range doc.Servers {
			if err := s.Match.Validate(); err != nil {
				generalErr = fmt.Errorf("mcp_general.yaml: %s: %w", s.Name, err)
				return
			}
		}
		general = doc.Servers
	})
	return general, generalErr
}
