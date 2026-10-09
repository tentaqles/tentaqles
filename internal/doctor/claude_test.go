package doctor

import "testing"

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
