package bundle

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func TestCompute_RejectsInvalidNames(t *testing.T) {
	cat := &Catalog{
		Skills: map[string]Skill{},
		MCP:    map[string]MCPServer{},
	}
	b := &manifest.Bundle{
		Skills: []string{".."},
		MCP:    []string{"../escape"},
	}

	d, errs := Compute(b, cat)

	if len(d.Skills) != 0 {
		t.Fatalf("d.Skills = %v, want empty", d.Skills)
	}
	if len(d.MCP) != 0 {
		t.Fatalf("d.MCP = %v, want empty", d.MCP)
	}

	var sawSkill, sawMCP bool
	for _, err := range errs {
		if strings.Contains(err.Error(), "invalid skill name") {
			sawSkill = true
		}
		if strings.Contains(err.Error(), "invalid mcp server name") {
			sawMCP = true
		}
	}
	if !sawSkill {
		t.Fatalf("expected an invalid skill name error, got %v", errs)
	}
	if !sawMCP {
		t.Fatalf("expected an invalid mcp server name error, got %v", errs)
	}
}
