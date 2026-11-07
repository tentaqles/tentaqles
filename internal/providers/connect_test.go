package providers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded catalog's connect: entries (ADR-0017) are the four providers whose vendors
// document a hosted MCP server, each an https URL, and each names its documentation.
func TestConnect_CatalogEntries(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gh", "linear", "asana", "jira"} {
		p, ok := c.Get(id)
		if !ok || p.Connect == nil {
			t.Fatalf("%s has no connect:", id)
		}
		if p.Connect.Transport != TransportHTTP || !strings.HasPrefix(p.Connect.URL, "https://") || p.Connect.Name == "" {
			t.Errorf("%s: %+v", id, p.Connect)
		}
		raw, err := embeddedFS.ReadFile("catalog/" + id + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		// The comment lines just above the key hold the vendor's documentation URL.
		lines := strings.Split(strings.SplitN(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\nconnect:\n", 2)[0], "\n")
		above := strings.Join(lines[max(0, len(lines)-3):], "\n")
		if !strings.Contains(above, "# ") || !strings.Contains(above, "https://") {
			t.Errorf("%s: no vendor documentation URL in a comment beside connect:\n%s", id, above)
		}
	}
	e := Connect{Name: "github", Transport: TransportHTTP, URL: "https://x.test/mcp"}.Entry()
	if len(e) != 2 || e["type"] != "http" || e["url"] != "https://x.test/mcp" {
		t.Errorf("http entry: %v", e)
	}
	s := Connect{Name: "x", Transport: TransportStdio, Command: "npx", Args: []string{"-y", "pkg"}}.Entry()
	if s["type"] != "stdio" || s["command"] != "npx" || len(s["args"].([]any)) != 2 {
		t.Errorf("stdio entry: %v", s)
	}
}

func TestConnect_Validate(t *testing.T) {
	ok := []Connect{
		{Name: "github", Transport: "http", URL: "https://api.githubcopilot.com/mcp/"},
		{Name: "x", Transport: "stdio", Command: "npx", Args: []string{"-y", "pkg"}},
	}
	for _, c := range ok {
		if err := c.validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	bad := []Connect{
		{Name: "", Transport: "http", URL: "https://x.test"},
		{Name: "a b", Transport: "http", URL: "https://x.test"},
		{Name: "x", Transport: "sse", URL: "https://x.test"},
		{Name: "x", Transport: "http", URL: "http://x.test"},
		{Name: "x", Transport: "http", URL: "https://u:p@x.test"},
		{Name: "x", Transport: "http", URL: "https:///path"},
		{Name: "x", Transport: "http", URL: "https://x.test", Command: "sh"},
		{Name: "x", Transport: "http", URL: "https://x.test/\n"},
		{Name: "x", Transport: "stdio"},
		{Name: "x", Transport: "stdio", Command: "sh", URL: "https://x.test"},
		{Name: "x", Transport: "stdio", Command: "sh", Args: []string{"a\x00b"}},
	}
	for _, c := range bad {
		if err := c.validate(); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	// Through the provider's own validation, with the provider named.
	p := Provider{ID: "x", Name: "X", Category: "other", Connect: &Connect{Name: "x", Transport: "ftp"}}
	if err := p.Validate(); err == nil || !strings.Contains(err.Error(), "connect") {
		t.Errorf("Validate: %v", err)
	}
}

// A user file may not choose connect:, for a built-in id or a new one: it is not loaded, and
// the embedded entry stays.
func TestConnect_UserFileIsNotLoaded(t *testing.T) {
	t.Setenv("TQ_HOME", t.TempDir())
	if err := os.MkdirAll(UserDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for id, body := range map[string]string{
		"gh":    "id: gh\nname: GitHub\ncategory: vcs\nconnect:\n  name: evil\n  transport: http\n  url: https://evil.test/mcp\n",
		"newco": "id: newco\nname: Newco\ncategory: other\nconnect:\n  name: newco\n  transport: http\n  url: https://mcp.newco.test/\n",
	} {
		if err := os.WriteFile(filepath.Join(UserDir(), id+".yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c, errs := LoadAll()
	if len(errs) != 2 {
		t.Fatalf("errors: %v", errs)
	}
	for _, e := range errs {
		if !strings.Contains(e.Error(), "connect") {
			t.Errorf("error does not name connect: %v", e)
		}
	}
	if gh, _ := c.Get("gh"); gh.Connect == nil || gh.Connect.Name != "github" {
		t.Errorf("gh connect = %+v", gh.Connect)
	}
	if _, ok := c.Get("newco"); ok {
		t.Error("newco loaded")
	}
	// A user file without connect: still extends a built-in as before.
	if err := os.WriteFile(filepath.Join(UserDir(), "gh.yaml"), []byte("id: gh\nname: GitHub\ncategory: vcs\nmatch:\n  env: [MY_GH_VAR]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, errs = LoadAll()
	if len(errs) != 1 {
		t.Fatalf("errors: %v", errs)
	}
	if gh, _ := c.Get("gh"); !contains(gh.Match.Env, "MY_GH_VAR") {
		t.Errorf("the override did not extend gh: %v", gh.Match.Env)
	}
}
