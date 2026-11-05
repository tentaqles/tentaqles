package mcpscope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

// The harness config layer (layer 3 of the PRD v2 Â§6.1 scenario, ADR-0007): for company-x
// (Azure only) no AWS MCP server stays registered in its Claude or Codex home, whatever its
// name spells, while the Azure server and a server that matches no provider stay.

func companyX(v2 bool) *manifest.Manifest {
	m := &manifest.Manifest{Schema: "tentaqles-client-v2", Client: "company-x",
		Identities: map[string]manifest.Identity{"az": {}, "claude": {}},
		Git:        manifest.Git{Provider: "azure-devops"}, Cloud: map[string]any{"provider": "azure"}}
	if v2 {
		m.Guard = "v2"
	}
	return m
}

func TestJudge(t *testing.T) {
	j := For("company-x", companyX(true))
	cases := []struct {
		name  string
		entry map[string]any
		out   bool
	}{
		{"aws", nil, true},
		{"awslabs-aws-api", nil, true},
		{"plugin_x_aws", nil, true},
		{"cloud-api", map[string]any{"type": "http", "url": "https://ec2.us-east-1.amazonaws.com/mcp"}, true},
		{"deployer", map[string]any{"command": "aws", "args": []any{"mcp"}}, true},
		{"azure", map[string]any{"command": "npx", "args": []any{"-y", "@azure/mcp@latest"}}, false},
		{"azure-aws-bridge", nil, false}, // an in-scope token wins, as in the hook
		{"postgres", map[string]any{"command": "uvx", "args": []any{"postgres-mcp"}}, false},
		{"docs", map[string]any{"url": "https://docs.example.com/mcp"}, false},
	}
	for _, c := range cases {
		why, out := j.Out(c.name, c.entry)
		if out != c.out {
			t.Errorf("%s: out=%v (%s), want %v", c.name, out, why, c.out)
		}
	}
	if For("company-x", companyX(false)) != nil {
		t.Fatal("a judge without guard: v2")
	}
	var nilJ *Judge
	if _, out := nilJ.Out("aws", nil); out {
		t.Fatal("a nil judge filters")
	}
}

const claudeJSON = `{
  "numStartups": 12345678901234567,
  "theme": "dark <b>&",
  "mcpServers": {
    "aws-api": {"command": "uvx", "args": ["awslabs.aws-api-mcp-server@latest"]},
    "azure": {"command": "npx", "args": ["-y", "@azure/mcp@latest", "server", "start"]},
    "mydb": {"command": "uvx", "args": ["postgres-mcp"]}
  },
  "projects": {
    "C:/work/company-x": {"mcpServers": {"aws": {"type": "http", "url": "https://knowledge-mcp.global.api.aws"}, "notes": {"command": "notes-mcp"}}},
    "C:/work/other": {"allowedTools": []}
  }
}
`

func TestFilterClaude(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(p, []byte(claudeJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	j := For("company-x", companyX(true))
	dry, err := FilterClaude(dir, j, true)
	if err != nil || len(dry) != 2 {
		t.Fatalf("dry run = %v, %v", dry, err)
	}
	if b, _ := os.ReadFile(p); string(b) != claudeJSON {
		t.Fatal("a dry run wrote the file")
	}
	removed, err := FilterClaude(dir, j, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range removed {
		got = append(got, r.Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"aws", "aws-api"}) {
		t.Fatalf("removed %v", got)
	}
	raw, _ := os.ReadFile(p)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	top := root["mcpServers"].(map[string]any)
	if _, ok := top["aws-api"]; ok || top["azure"] == nil || top["mydb"] == nil {
		t.Fatalf("top-level servers = %v", top)
	}
	proj := root["projects"].(map[string]any)["C:/work/company-x"].(map[string]any)["mcpServers"].(map[string]any)
	if _, ok := proj["aws"]; ok || proj["notes"] == nil {
		t.Fatalf("project servers = %v", proj)
	}
	// Every other key as it was: the large number exact, < > & unescaped.
	if !strings.Contains(string(raw), `"numStartups": 12345678901234567`) || !strings.Contains(string(raw), `"theme": "dark <b>&"`) {
		t.Fatalf("other keys changed:\n%s", raw)
	}
	// The removed entries are kept beside the file.
	kb, err := os.ReadFile(filepath.Join(dir, RemovedFile))
	if err != nil || !strings.Contains(string(kb), "awslabs.aws-api-mcp-server@latest") || !strings.Contains(string(kb), "knowledge-mcp.global.api.aws") {
		t.Fatalf("removed file: %s %v", kb, err)
	}
	// Idempotent.
	if again, err := FilterClaude(dir, j, false); err != nil || len(again) != 0 {
		t.Fatalf("second pass = %v, %v", again, err)
	}
	// No file: nothing to do.
	if r, err := FilterClaude(t.TempDir(), j, false); err != nil || r != nil {
		t.Fatalf("missing file: %v %v", r, err)
	}
}

const codexTOML = `# >>> managed by tq: edit with tq settings render, not by hand
approval_policy = "on-request"
sandbox_mode = "workspace-write"
# <<< managed by tq
model = "gpt-5"
# a comment that looks like a value = [

[mcp_servers.aws]
command = "uvx"
args = [
  "awslabs.aws-api-mcp-server@latest",
  ["nested", "array"],
]

[mcp_servers.aws.env]
AWS_REGION = "us-east-1"

[mcp_servers.azure]
command = "npx"
args = ["-y", "@azure/mcp@latest"]

[mcp_servers."cloud-api"]
url = "https://ec2.amazonaws.com/mcp"

[mcp_servers]
aws-docs = { command = "uvx", args = ["awslabs.aws-documentation-mcp-server"] }
notes = { command = "notes-mcp" }

[profiles.fast]
model = "gpt-5-mini"
`

const codexWant = `# >>> managed by tq: edit with tq settings render, not by hand
approval_policy = "on-request"
sandbox_mode = "workspace-write"
# <<< managed by tq
model = "gpt-5"
# a comment that looks like a value = [

[mcp_servers.azure]
command = "npx"
args = ["-y", "@azure/mcp@latest"]

[mcp_servers]
notes = { command = "notes-mcp" }

[profiles.fast]
model = "gpt-5-mini"
`

func TestFilterCodex(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(codexTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	j := For("company-x", companyX(true))
	removed, err := FilterCodex(dir, j, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range removed {
		got = append(got, r.Name)
	}
	if !reflect.DeepEqual(got, []string{"aws", "cloud-api", "aws-docs"}) {
		t.Fatalf("removed %v", got)
	}
	b, _ := os.ReadFile(p)
	if string(b) != codexWant {
		t.Fatalf("config.toml:\n%s\n--- want\n%s", b, codexWant)
	}
	if again, err := FilterCodex(dir, j, false); err != nil || len(again) != 0 {
		t.Fatalf("second pass = %v, %v", again, err)
	}
	kb, _ := os.ReadFile(filepath.Join(dir, RemovedFile))
	if !strings.Contains(string(kb), `AWS_REGION = \"us-east-1\"`) {
		t.Fatalf("removed file lacks the aws table: %s", kb)
	}
}
