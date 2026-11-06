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

// The harness config layer (layer 3 of the PRD v2 §6.1 scenario, ADR-0007): for company-x
// (Azure only) no AWS or GitHub MCP server stays registered in its Claude or Codex home,
// whatever its name spells, while the Azure server and servers that match no provider stay.

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
		// by the entry, whatever the name (round 2)
		{"cloud", map[string]any{"command": "uvx", "args": []any{"awslabs.aws-api-mcp-server"}, "env": map[string]any{"AWS_PROFILE": "prod"}}, true},
		{"gh", map[string]any{"command": "npx", "args": []any{"@modelcontextprotocol/server-github"}}, true},
		{"helper", map[string]any{"command": "node", "args": []any{"x.js"}, "env": map[string]any{"AWS_ACCESS_KEY_ID": "x"}}, true},
		// kept
		{"azure", map[string]any{"command": "npx", "args": []any{"-y", "@azure/mcp@latest"}}, false},
		{"postgres", map[string]any{"command": "uvx", "args": []any{"postgres-mcp"}}, false},
		// the hybrid list (2026-11-05): a server of no provider and no general purpose goes
		{"docs", map[string]any{"url": "https://docs.example.com/mcp"}, true},
		{"registry", map[string]any{"command": "node", "args": []any{"r.js"}, "env": map[string]any{"NPM_TOKEN": "x"}}, true},
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

// Claude Code's own formatting: 2-space indent, its key order, a large number, < > &.
const claudeJSON = `{
  "numStartups": 12345678901234567,
  "theme": "dark <b>&",
  "zeta": 1,
  "mcpServers": {
    "cloud": {
      "command": "uvx",
      "args": [
        "awslabs.aws-api-mcp-server@latest"
      ],
      "env": {
        "AWS_PROFILE": "prod"
      }
    },
    "azure": {
      "command": "npx",
      "args": [
        "-y",
        "@azure/mcp@latest"
      ]
    },
    "gh": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-github"
      ]
    }
  },
  "projects": {
    "C:/work/company-x": {
      "mcpServers": {
        "aws": {
          "type": "http",
          "url": "https://knowledge-mcp.global.api.aws"
        }
      },
      "allowedTools": []
    },
    "C:/work/other": {
      "mcpServers": {
        "notes": {
          "command": "notes-mcp"
        },
        "aws-docs": {
          "command": "uvx",
          "args": [
            "awslabs.aws-documentation-mcp-server"
          ]
        }
      }
    }
  },
  "alpha": true
}
`

const claudeWant = `{
  "numStartups": 12345678901234567,
  "theme": "dark <b>&",
  "zeta": 1,
  "mcpServers": {
    "azure": {
      "command": "npx",
      "args": [
        "-y",
        "@azure/mcp@latest"
      ]
    }
  },
  "projects": {
    "C:/work/company-x": {
      "mcpServers": {},
      "allowedTools": []
    },
    "C:/work/other": {
      "mcpServers": {}
    }
  },
  "alpha": true
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
	if err != nil || len(dry) != 5 {
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
	if !reflect.DeepEqual(got, []string{"aws", "aws-docs", "cloud", "gh", "notes"}) {
		t.Fatalf("removed %v", got)
	}
	// Byte for byte everything else: indentation, key order, the number, < > &.
	if b, _ := os.ReadFile(p); string(b) != claudeWant {
		t.Fatalf(".claude.json:\n%s\n--- want\n%s", b, claudeWant)
	}
	kb, err := os.ReadFile(filepath.Join(dir, RemovedFile))
	if err != nil || !strings.Contains(string(kb), "awslabs.aws-api-mcp-server@latest") || !strings.Contains(string(kb), "server-github") {
		t.Fatalf("removed file: %s %v", kb, err)
	}
	if again, err := FilterClaude(dir, j, false); err != nil || len(again) != 0 {
		t.Fatalf("second pass = %v, %v", again, err)
	}
	if r, err := FilterClaude(t.TempDir(), j, false); err != nil || r != nil {
		t.Fatalf("missing file: %v %v", r, err)
	}
}

func TestDeleteMember(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"a":1,"b":2,"c":3}`, `{"a":1,"c":3}`},
		{`{"a":1,"b":2}`, `{"a":1}`},
		{`{"b":2,"a":1}`, `{"a":1}`},
		{`{ "b" : { "x": "}\"" } }`, `{}`},
		{`{"a":{"b":[1,{"c":"]"}]},"b":"x"}`, `{"a":{"b":[1,{"c":"]"}]}}`},
	}
	for _, c := range cases {
		out, err := deleteMember([]byte(c.in), []string{"b"})
		if err != nil || string(out) != c.want {
			t.Errorf("%s: got %s (%v), want %s", c.in, out, err, c.want)
		}
	}
	if _, err := deleteMember([]byte(`{"a":`), []string{"a"}); err == nil {
		t.Fatal("broken JSON accepted")
	}
}

// A mcp-out-of-scope.json tq cannot read is never overwritten: it is moved aside.
func TestKeepRemoved_CorruptFileMovedAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, RemovedFile)
	if err := os.WriteFile(p, []byte("{not a list"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := keepRemoved(dir, []Removed{{Name: "aws"}}); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(p + ".corrupt-*")
	if len(m) != 1 {
		t.Fatalf("corrupt file not kept aside: %v", m)
	}
	if b, _ := os.ReadFile(m[0]); string(b) != "{not a list" {
		t.Fatalf("aside file changed: %q", b)
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), `"aws"`) {
		t.Fatalf("new list: %s", b)
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

# The Azure server, kept.
[mcp_servers.azure]
command = "npx"
args = ["-y", "@azure/mcp@latest"]

[mcp_servers.cloud]
command = "node"
args = ["server.js"]
env = { AWS_PROFILE = "prod" }

[mcp_servers.helper]
command = "node"
args = [
  "-y",
  "@modelcontextprotocol/server-github",
]

[mcp_servers."cloud-api"]
url = "https://ec2.amazonaws.com/mcp"

[mcp_servers.x]
command = "node"
[mcp_servers.x.env]
GITHUB_TOKEN = "t"

# The inline servers.
[mcp_servers]
aws-docs = { command = "uvx", args = ["awslabs.aws-documentation-mcp-server"] }
gh2 = { command = "npx", args = ["-y", "@modelcontextprotocol/server-github"] }
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


# The Azure server, kept.
[mcp_servers.azure]
command = "npx"
args = ["-y", "@azure/mcp@latest"]





# The inline servers.
[mcp_servers]

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
	if !reflect.DeepEqual(got, []string{"aws", "cloud", "helper", "cloud-api", "x", "aws-docs", "gh2", "notes"}) {
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

// Entries is what the hook judges an mcp__ call by: the identity's .claude.json (user scope
// and the projects holding cwd), .mcp.json from cwd up to the root, Codex's config.toml.
func TestEntries(t *testing.T) {
	base := t.TempDir()
	claude := filepath.Join(base, "claude")
	codex := filepath.Join(base, "codex")
	root := filepath.Join(base, "ws")
	cwd := filepath.Join(root, "sub")
	for _, d := range []string{claude, codex, cwd} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	rootKey := filepath.ToSlash(root)
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(claude, ".claude.json"), `{"mcpServers":{"user":{"command":"a"}},"projects":{"`+rootKey+`":{"mcpServers":{"local":{"command":"b"}}},"/elsewhere":{"mcpServers":{"other":{"command":"c"}}}}}`)
	write(filepath.Join(root, ".mcp.json"), `{"mcpServers":{"proj":{"command":"d"},"user":{"command":"uvx","args":["awslabs.core-mcp-server"]}}}`)
	// An installed plugin with an MCP server (installed_plugins.json v2 shape).
	plug := filepath.Join(base, "plugcache", "deployer")
	if err := os.MkdirAll(plug, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(claude, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	ij, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{"deployer@mkt": []any{map[string]any{"installPath": plug}}}})
	write(filepath.Join(claude, "plugins", "installed_plugins.json"), string(ij))
	write(filepath.Join(plug, ".mcp.json"), `{"vercel-api":{"type":"http","url":"https://mcp.vercel.com"}}`)
	write(filepath.Join(base, ".mcp.json"), `{"mcpServers":{"above":{"command":"e"}}}`)
	write(filepath.Join(codex, "config.toml"), "[mcp_servers.cx]\ncommand = \"f\"\nargs = [\"g\"]\n")
	e := Entries(claude, codex, cwd, root)
	var got []string
	for n := range e {
		got = append(got, n)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"cx", "local", "plugin_deployer_vercel-api", "proj", "user"}) {
		t.Fatalf("entries = %v", got)
	}
	if len(e["user"]) != 2 {
		t.Fatalf("same-named user entries = %v, want both sources", e["user"])
	}
	j := For("company-x", companyX(true))
	anyOut := false
	for _, en := range e["user"] {
		if _, out := j.Out("user", en); out {
			anyOut = true
		}
	}
	if !anyOut {
		t.Fatal("the .mcp.json user entry that runs awslabs is not seen")
	}
	if _, out := j.Out("plugin_deployer_vercel-api", e["plugin_deployer_vercel-api"][0]); !out {
		t.Fatal("the plugin vercel server is in scope")
	}
	if a, _ := e["cx"][0]["args"].([]any); len(a) != 1 || a[0] != "g" {
		t.Fatalf("codex args = %v", e["cx"])
	}
}

// A server render removed stays judged by the hook: a session started before the render has
// it loaded still.
func TestEntries_RemovedStillJudged(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"mcpServers":{"m14-deploy":{"type":"http","url":"https://mcp.vercel.com"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	codex := t.TempDir()
	if err := os.WriteFile(filepath.Join(codex, "config.toml"), []byte("[mcp_servers.cx]\ncommand = \"uvx\"\nargs = [\"awslabs.core-mcp-server\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := For("company-x", companyX(true))
	if r, err := FilterClaude(dir, j, false); err != nil || len(r) != 1 {
		t.Fatalf("render: %v %v", r, err)
	}
	if r, err := FilterCodex(codex, j, false); err != nil || len(r) != 1 {
		t.Fatalf("codex render: %v %v", r, err)
	}
	e := Entries(dir, codex, "", "")
	for _, n := range []string{"m14-deploy", "cx"} {
		if len(e[n]) == 0 {
			t.Fatalf("%s not among the entries after render: %v", n, e)
		}
		if _, out := j.Out(n, e[n][0]); !out {
			t.Fatalf("%s judged in scope", n)
		}
	}
}

// Codex names a server's credential variables in fields of their own: bearer_token_env_var,
// env_vars, env_http_headers (inline or as a table); each is read as the server's variable.
func TestFilterCodex_CredentialFields(t *testing.T) {
	dir := t.TempDir()
	toml := "model = \"gpt-5\"\n\n" +
		"[mcp_servers.x1]\nurl = \"https://gw.example.com/mcp\"\nbearer_token_env_var = \"GITHUB_PERSONAL_ACCESS_TOKEN\"\n\n" +
		"[mcp_servers.x2]\ncommand = \"node\"\nargs = [\"s.js\"]\nenv_vars = [\"AWS_PROFILE\", \"AWS_SECRET_ACCESS_KEY\"]\n\n" +
		"[mcp_servers.x3]\nurl = \"https://gw.example.com/mcp\"\nenv_http_headers = { \"Authorization\" = \"SUPABASE_ACCESS_TOKEN\" }\n\n" +
		"[mcp_servers.x4]\nurl = \"https://gw.example.com/mcp\"\n[mcp_servers.x4.env_http_headers]\nAuthorization = \"VERCEL_TOKEN\"\n\n" +
		"[mcp_servers.x5]\ncommand = \"npx\"\nargs = [\"@playwright/mcp@latest\"]\nenv_vars = [\"HOME\"]\n\n" +
		"[mcp_servers.'x6-quoted']\ncommand = \"uvx\"\nargs = [\"awslabs.cloudwatch-mcp-server\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := FilterCodex(dir, For("company-x", companyX(true)), false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range removed {
		got = append(got, r.Name)
	}
	if !reflect.DeepEqual(got, []string{"x1", "x2", "x3", "x4", "x6-quoted"}) {
		t.Fatalf("removed %v", got)
	}
}

// A plugin's MCP servers in every shape Claude Code's plugin manifest takes: .mcp.json (with or
// without mcpServers), plugin.json mcpServers as an object, a path string, or an array of
// paths and objects. ${CLAUDE_PLUGIN_ROOT} is the plugin folder.
func TestPluginServers_EveryShape(t *testing.T) {
	base := t.TempDir()
	claude := filepath.Join(base, "claude")
	mk := func(name string, files map[string]string) string {
		p := filepath.Join(base, "cache", name)
		for rel, body := range files {
			fp := filepath.Join(p, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(fp), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fp, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	p1 := mk("deployer", map[string]string{".mcp.json": `{"vercel":{"type":"http","url":"https://mcp.vercel.com"}}`})
	p2 := mk("dbtools", map[string]string{".claude-plugin/plugin.json": `{"name":"dbtools","mcpServers":"./config/servers.json"}`,
		"config/servers.json": `{"mcpServers":{"db":{"command":"npx","args":["-y","@supabase/mcp-server-supabase"]}}}`})
	p3 := mk("inline", map[string]string{".claude-plugin/plugin.json": `{"name":"inline","mcpServers":{"cloud":{"command":"${CLAUDE_PLUGIN_ROOT}/bin/awslabs.aws-api-mcp-server.exe"}}}`})
	p4 := mk("arr", map[string]string{".claude-plugin/plugin.json": `{"name":"arr","mcpServers":["./a.json",{"inl":{"type":"http","url":"https://mcp.stripe.com"}}]}`,
		"a.json": `{"mcpServers":{"srv":{"type":"http","url":"https://api.githubcopilot.com/mcp/"}}}`})
	p5 := mk("helper", map[string]string{".mcp.json": `{"mcpServers":{"pw":{"command":"npx","args":["@playwright/mcp@latest"]}}}`})
	if err := os.MkdirAll(filepath.Join(claude, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	ij, _ := json.Marshal(map[string]any{"version": 2, "plugins": map[string]any{
		"deployer@mkt": []any{map[string]any{"installPath": p1}}, "dbtools@mkt": []any{map[string]any{"installPath": p2}},
		"inline@mkt": []any{map[string]any{"installPath": p3}}, "arr@mkt": []any{map[string]any{"installPath": p4}},
		"helper@mkt": []any{map[string]any{"installPath": p5}}}})
	if err := os.WriteFile(filepath.Join(claude, "plugins", "installed_plugins.json"), ij, 0o600); err != nil {
		t.Fatal(err)
	}
	j := For("company-x", companyX(true))
	out := map[string]bool{}
	for _, ps := range PluginServers(claude) {
		_, o := j.Out(ps.ToolName, ps.Entry)
		out[ps.ToolName] = o
	}
	want := map[string]bool{"plugin_deployer_vercel": true, "plugin_dbtools_db": true, "plugin_inline_cloud": true,
		"plugin_arr_srv": true, "plugin_arr_inl": true, "plugin_helper_pw": false}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("plugin servers = %v, want %v", out, want)
	}
}
