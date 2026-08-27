package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapture_DefaultDirFallsBackToHomeClaudeJSON(t *testing.T) {
	home := isolateHome(t)

	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// no <dir>/.claude.json on purpose
	writeFile(t, filepath.Join(home, ".claude.json"),
		`{"mcpServers":{"a":{"command":"a-cmd"}}}`)
	writeFile(t, filepath.Join(dir, ".mcp.json"),
		`{"mcpServers":{"b":{"command":"b-cmd"}}}`)

	c, err := Capture(dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if _, ok := c.Catalog.MCP["a"]; !ok {
		t.Fatalf("server a not captured; MCP = %v", c.Catalog.MCP)
	}
	if _, ok := c.Catalog.MCP["b"]; !ok {
		t.Fatalf("server b not captured; MCP = %v", c.Catalog.MCP)
	}
	if len(c.Bundle.MCP) != 2 || c.Bundle.MCP[0] != "a" || c.Bundle.MCP[1] != "b" {
		t.Fatalf("Bundle.MCP = %v, want [a b]", c.Bundle.MCP)
	}

	joined := strings.Join(c.Warnings, "\n")
	for _, want := range []string{
		filepath.Join(home, ".claude.json"),
		filepath.Join(dir, ".mcp.json"),
		filepath.Join(dir, ".claude.json"),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings do not mention %s:\n%s", want, joined)
		}
	}
}

func TestCapture_NonDefaultDirDoesNotFallBackToHome(t *testing.T) {
	home := isolateHome(t)

	dir := filepath.Join(home, "profiles", "acme", "claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".claude.json"),
		`{"mcpServers":{"a":{"command":"a-cmd"}}}`)

	c, err := Capture(dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if _, ok := c.Catalog.MCP["a"]; ok {
		t.Fatal("home .claude.json should not be used for a non-default dir")
	}
}

func TestCapture_SkipsInvalidNames(t *testing.T) {
	home := isolateHome(t)
	dir := filepath.Join(home, "identity")

	writeFile(t, filepath.Join(dir, ".claude.json"),
		`{"mcpServers":{"ok":{"command":"x"},"bad/name":{"command":"y"},"..":{"command":"z"}}}`)

	c, err := Capture(dir)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(c.Bundle.MCP) != 1 || c.Bundle.MCP[0] != "ok" {
		t.Fatalf("Bundle.MCP = %v, want [ok]", c.Bundle.MCP)
	}
	joined := strings.Join(c.Warnings, "\n")
	if !strings.Contains(joined, "invalid name") {
		t.Fatalf("expected invalid-name warning, got:\n%s", joined)
	}
}
