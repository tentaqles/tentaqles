package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/paths"
)

func TestUnpinnedPackages(t *testing.T) {
	raw := []byte(`{"statusLine":{"command":"npx -y ccstatusline@latest"},
	 "mcpServers":{
	   "a":{"command":"npx","args":["-y","@modelcontextprotocol/server-postgres"]},
	   "b":{"command":"npx","args":["-y","hostinger-api-mcp@latest"]},
	   "c":{"command":"npx","args":["-y","@playwright/mcp@0.0.41"]},
	   "d":{"command":"node","args":["C:/x/server.js"]}}}`)
	var hits []string
	for _, c := range commandStrings(raw) {
		if unpinnedRe.MatchString(c) {
			hits = append(hits, c)
		}
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %v", hits)
	}
	for _, h := range hits {
		if h == "npx -y @playwright/mcp@0.0.41" || h == "node C:/x/server.js" {
			t.Errorf("pinned/local command flagged: %s", h)
		}
	}
}

func TestPythonStoreAlias(t *testing.T) {
	t.Setenv("TENTAQLES_PY", "")
	var codes []string
	add := func(level, code, ws, msg, fix string) { codes = append(codes, code) }
	look := func(string) (string, error) { return `C:\Users\x\AppData\Local\Microsoft\WindowsApps\python3.exe`, nil }
	pythonStoreAlias(look, "windows", add)
	pythonStoreAlias(look, "linux", add)
	if len(codes) != 1 || codes[0] != "python-store-alias" {
		t.Fatalf("codes = %v", codes)
	}
}

func TestPluginForeignPath(t *testing.T) {
	isolatedHome(t)
	write := func(id, json string) {
		dir := filepath.Join(paths.IdentityDir(id, "claude"), "plugins")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "installed_plugins.json"), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	own := filepath.ToSlash(filepath.Join(paths.IdentityDir("uplabs", "claude"), "plugins", "cache", "m", "tentaqles", "0.11.0"))
	foreign := filepath.ToSlash(filepath.Join(paths.IdentityDir("dirtybird", "claude"), "plugins", "cache", "m", "aws", "2.0.2"))
	write("uplabs", `{"version":2,"plugins":{
	  "tentaqles@m":[{"version":"0.11.0","installPath":"`+own+`"}],
	  "aws@m":[{"version":"2.0.2","installPath":"`+foreign+`"}],
	  "legacy@m":[{"version":"1.0.0","installPath":"C:/Users/x/.claude/plugins/cache/m/legacy/1.0.0"}]}}`)
	write("dirtybird", `{"version":2,"plugins":{"aws@m":[{"version":"2.0.2","installPath":"`+foreign+`"}]}}`)

	var got []Finding
	add := func(level, code, ws, msg, fix string) {
		got = append(got, Finding{Level: level, Code: code, Workspace: ws, Msg: msg, Fix: fix})
	}
	pluginForeignPath([]string{"dirtybird", "uplabs"}, add)

	// dirtybird's record points into its own dir: nothing. uplabs has two
	// records that live elsewhere: one in another identity, one in ~/.claude.
	if len(got) != 2 {
		t.Fatalf("findings = %+v", got)
	}
	for _, f := range got {
		if f.Code != "plugin-foreign-path" || f.Workspace != "uplabs" || f.Level != "warn" || f.Fix == "" {
			t.Fatalf("%+v", f)
		}
	}
	if !strings.Contains(got[0].Msg, "aws@m") || !strings.Contains(got[0].Msg, "dirtybird") {
		t.Fatalf("message must name the plugin and the identity it points into: %q", got[0].Msg)
	}
	if !strings.Contains(got[1].Msg, "legacy@m") {
		t.Fatalf("%q", got[1].Msg)
	}
}
