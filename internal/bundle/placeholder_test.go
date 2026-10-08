package bundle

import (
	"reflect"
	"strings"
	"testing"
)

func TestPlaceholderize(t *testing.T) {
	jwt := "eyJ" + "hbGciOiJIUzI1NiJ9.eyJ" + "zdWIiOiIxIn0.abcdefghij"
	conn := "postgresql://u:" + "pw12345678@db/x"
	srv := MCPServer{
		"command": "npx",
		"args":    []any{"-y", "@modelcontextprotocol/server-postgres", conn},
		"env":     map[string]any{"API_TOKEN": jwt, "MODE": "prod"},
	}
	out, vars := Placeholderize("dbi-mcp prd", srv)
	want := MCPServer{
		"command": "npx",
		"args":    []any{"-y", "@modelcontextprotocol/server-postgres", "${TQ_DBI_MCP_PRD_ARGS_2}"},
		"env":     map[string]any{"API_TOKEN": "${API_TOKEN}", "MODE": "prod"},
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("out = %#v", out)
	}
	if strings.Join(vars, ",") != "API_TOKEN,TQ_DBI_MCP_PRD_ARGS_2" {
		t.Fatalf("vars = %v", vars)
	}
	// The input is not mutated.
	if srv["env"].(map[string]any)["API_TOKEN"] != jwt {
		t.Fatal("input mutated")
	}
	// Already-placeholdered specs pass through unchanged.
	again, v2 := Placeholderize("x", out)
	if !reflect.DeepEqual(again, out) || len(v2) != 0 {
		t.Fatalf("not idempotent: %#v %v", again, v2)
	}
}
