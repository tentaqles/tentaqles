package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/connect"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/mcpscope"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/policy"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/secret"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// ghBody is a client that uses GitHub (gh) and Azure (az) with a Claude identity, and Linear
// without a private config home.
const ghBody = "git: { email: dev@acme.example, provider: github }\n" +
	"identities: { gh: {}, claude: {}, az: {} }\nscope: [linear]\n"

const ghEntryURL = "https://api.githubcopilot.com/mcp/"

// statusSeams replaces the verify runner and the PATH lookup of tq status --json.
func statusSeams(t *testing.T, run func(name string) (string, error)) {
	t.Helper()
	pr, pl, pv := statusRunCLI, statusLookPath, statusVerifyLimit
	statusRunCLI = func(_ context.Context, _ []string, name string, _ []string) (string, error) { return run(name) }
	statusLookPath = func(n string) (string, error) {
		if n == "gh" {
			return "/fake/gh", nil
		}
		return "", exec.ErrNotFound
	}
	statusVerifyLimit = 2 * time.Second
	t.Cleanup(func() { statusRunCLI, statusLookPath, statusVerifyLimit = pr, pl, pv })
}

func claudeJSONOf(ws string) string {
	return filepath.Join(paths.IdentityDir(ws, "claude"), ".claude.json")
}

func writeClaudeJSON(t *testing.T, ws, body string) string {
	t.Helper()
	p := claudeJSONOf(ws)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func fileBytes(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return "<missing>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func logLines(t *testing.T, p string) []string {
	t.Helper()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func statusObject(t *testing.T) map[string]any {
	t.Helper()
	out, err := runTq(t, "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v\n%s", err, out)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out)
	}
	return obj
}

// U-01
func TestStatusJSON_Identity(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	statusSeams(t, func(name string) (string, error) {
		if name == "gh" {
			return "Logged in to github.com account someone", nil
		}
		return "", errors.New("exit status 1")
	})
	before := fileBytes(t, claudeJSONOf("acme"))
	obj := statusObject(t)
	for _, k := range []string{"identity", "root", "scope", "providers", "browsers", "other_providers"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("no %q in %v", k, obj)
		}
	}
	if obj["identity"] != "acme" || obj["trusted"] != true {
		t.Errorf("identity/trusted: %v %v", obj["identity"], obj["trusted"])
	}
	if r, _ := obj["root"].(string); !strings.EqualFold(filepath.Clean(r), filepath.Clean(root)) {
		t.Errorf("root = %q, want %q", r, root)
	}
	scope, _ := obj["scope"].([]any)
	want := []string{"az", "claude", "gh", "linear"}
	if len(scope) != len(want) {
		t.Fatalf("scope = %v, want %v", scope, want)
	}
	provs, _ := obj["providers"].([]any)
	if len(provs) != len(want) {
		t.Fatalf("providers = %v", provs)
	}
	for i, pv := range provs {
		p := pv.(map[string]any)
		for _, k := range []string{"id", "category", "cli", "cli_resolves", "credential", "connection", "verified"} {
			if _, ok := p[k]; !ok {
				t.Errorf("provider %v lacks %q", p["id"], k)
			}
		}
		if p["id"] != want[i] {
			t.Errorf("provider %d is %v, want %s", i, p["id"], want[i])
		}
		for _, k := range []string{"credential", "connection"} {
			if v := p[k]; v != "present" && v != "missing" && v != "none" {
				t.Errorf("%v %s = %v", p["id"], k, v)
			}
		}
		if _, ok := p["verified"].(bool); !ok {
			t.Errorf("%v verified is not a bool: %v", p["id"], p["verified"])
		}
		if p["id"] == "gh" && (p["verified"] != true || p["cli_resolves"] != true || p["credential"] != "present" || p["cli"] != "gh") {
			t.Errorf("gh: %v", p)
		}
		if p["id"] == "linear" && (p["cli"] != nil || p["verified"] != false || p["connection"] != "missing") {
			t.Errorf("linear: %v", p)
		}
	}
	if got := fileBytes(t, claudeJSONOf("acme")); got != before {
		t.Errorf("status wrote the Claude config: %q", got)
	}
	if _, err := os.Stat(connect.LogPath()); err == nil {
		t.Error("status wrote the connection log")
	}
}

// S-03
func TestStatusJSON_NoSecrets(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	// The keychain seam (go-keyring's mock), a provider variable, and an MCP entry with an env
	// value and a header token.
	if err := secret.Set("acme", "github", "KEYCHAIN_MARKER_7f3a"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINEAR_API_KEY", "ENV_MARKER_91bc")
	t.Setenv("AZURE_CLIENT_SECRET", "AZ_MARKER_55de")
	// (Built from parts: a fixture line that reads as a credential would trip the commit scan.)
	cfg, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"github": map[string]any{"type": "http", "url": "https://x.test/mcp",
			"headers": map[string]any{"Authorization": "Bear" + "er " + "HEADER_MARKER_c0de"}},
		"mydb": map[string]any{"command": "x", "env": map[string]any{"DB_PASS" + "WORD": "MCP_ENV_MARKER_3344"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	writeClaudeJSON(t, "acme", string(cfg))
	statusSeams(t, func(name string) (string, error) {
		return "token VERIFY_OUT_MARKER_88aa", errors.New("exit status 1 VERIFY_ERR_MARKER_01")
	})
	out, err := runTq(t, "status", "--json")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"KEYCHAIN_MARKER", "ENV_MARKER", "AZ_MARKER", "HEADER_MARKER", "MCP_ENV_MARKER", "VERIFY_OUT_MARKER", "VERIFY_ERR_MARKER", "x.test", "dev@acme.example"} {
		if strings.Contains(out, m) {
			t.Errorf("the output holds %q:\n%s", m, out)
		}
	}
	var st struct {
		Providers []struct{ ID, Credential, Connection string }
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{}
	for _, p := range st.Providers {
		got[p.ID] = [2]string{p.Credential, p.Connection}
	}
	if got["gh"] != [2]string{"present", "present"} || got["linear"][0] != "present" || got["az"][0] != "present" {
		t.Errorf("credential/connection = %v", got)
	}
}

// U-02
func TestConnect_WritesCatalogEntry(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	p := writeClaudeJSON(t, "acme", "{\n  \"numStartups\": 3,\n  \"mcpServers\": {\n    \"mydb\": {\"command\": \"x\"}\n  }\n}\n")
	out, err := runTq(t, "connect", "gh")
	if err != nil || strings.TrimSpace(out) != "gh: connected as github" {
		t.Fatalf("connect gh: %v %q", err, out)
	}
	var got struct {
		N   float64                   `json:"numStartups"`
		MCP map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(fileBytes(t, p)), &got); err != nil {
		t.Fatal(err)
	}
	if got.N != 3 || got.MCP["mydb"]["command"] != "x" || len(got.MCP) != 2 {
		t.Errorf("other keys changed: %+v", got)
	}
	if e := got.MCP["github"]; len(e) != 2 || e["type"] != "http" || e["url"] != ghEntryURL {
		t.Errorf("github entry = %v", e)
	}
	lines := logLines(t, connect.LogPath())
	if len(lines) != 1 {
		t.Fatalf("connection log: %v", lines)
	}
	var rec connect.Record
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Time.IsZero() || rec.Identity != "acme" || rec.Provider != "gh" || rec.Server != "github" || rec.Caller != connect.CallerHuman || rec.Outcome != connect.OutcomeConnected {
		t.Errorf("record: %+v", rec)
	}
	if strings.Contains(lines[0], "http") || strings.Contains(lines[0], "githubcopilot") {
		t.Errorf("the log line carries the entry: %s", lines[0])
	}
}

// An agent may run connect, and its line says so.
func TestConnect_CallerIsLoggedForAnAgent(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	t.Setenv(humanonly.Markers[0], "1")
	if out, err := runTq(t, "connect", "linear"); err != nil || strings.TrimSpace(out) != "linear: connected as linear" {
		t.Fatalf("connect under a harness marker: %v %q", err, out)
	}
	lines := logLines(t, connect.LogPath())
	if len(lines) != 1 || !strings.Contains(lines[0], `"caller":"harness"`) {
		t.Fatalf("log: %v", lines)
	}
}

// U-03
func TestConnect_IdempotentAndNoEntry(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	p := claudeJSONOf("acme")
	if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "gh: connected as github") {
		t.Fatalf("first: %v %q", err, out)
	}
	first := fileBytes(t, p)
	out, err := runTq(t, "connect", "gh")
	if err != nil || strings.TrimSpace(out) != "gh: already connected" {
		t.Fatalf("second: %v %q", err, out)
	}
	if fileBytes(t, p) != first {
		t.Error("the second run changed the file")
	}
	if n := len(logLines(t, connect.LogPath())); n != 1 {
		t.Errorf("the second run logged: %d lines", n)
	}
	out, err = runTq(t, "connect", "az")
	if err != nil || strings.TrimSpace(out) != "az: no MCP server in the catalog" {
		t.Fatalf("az: %v %q", err, out)
	}
	if fileBytes(t, p) != first || len(logLines(t, connect.LogPath())) != 1 {
		t.Error("az wrote something")
	}
}

// F-01
func TestStatusAndConnect_BadStates(t *testing.T) {
	root := mkUntrusted(t, "evil")
	statusSeams(t, func(string) (string, error) { return "", errors.New("must not run") })
	cj := claudeJSONOf("evil")

	// Untrusted: the object names the identity, says why, and holds no provider data.
	chdir(t, root)
	obj := statusObject(t)
	if obj["identity"] != "evil" || obj["trusted"] != false || obj["reason"] == "" || obj["reason"] == nil {
		t.Errorf("untrusted: %v", obj)
	}
	for _, k := range []string{"providers", "scope", "browsers", "other_providers"} {
		if a, ok := obj[k].([]any); !ok || len(a) != 0 {
			t.Errorf("untrusted %s = %v", k, obj[k])
		}
	}
	_, err := runTq(t, "connect", "gh")
	if err == nil || !strings.Contains(err.Error(), "untrusted") || !strings.Contains(err.Error(), "evil") {
		t.Errorf("untrusted connect: %v", err)
	}
	if fileBytes(t, cj) != "<missing>" {
		t.Error("connect wrote in an untrusted client")
	}

	// Neutral.
	chdir(t, t.TempDir())
	obj = statusObject(t)
	if v, ok := obj["identity"]; !ok || v != nil {
		t.Errorf("neutral identity = %v (present %v)", v, ok)
	}
	if _, err := runTq(t, "connect", "gh"); err == nil || !strings.Contains(err.Error(), "neutral") {
		t.Errorf("neutral connect: %v", err)
	}
	if fileBytes(t, cj) != "<missing>" || len(logLines(t, connect.LogPath())) != 0 {
		t.Error("connect wrote in a neutral folder")
	}
}

// S-01
func TestConnect_OutOfScopeRefused(t *testing.T) {
	root, _ := setupScoped(t, "company-x", companyXBody)
	chdir(t, root)
	body := "{\"keep\": true}\n"
	p := writeClaudeJSON(t, "company-x", body)
	out, err := runTq(t, "connect", "gh")
	want := "gh is outside the scope of identity company-x: az, azure-devops, claude"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("connect gh: err=%v out=%q, want %q", err, out, want)
	}
	if fileBytes(t, p) != body {
		t.Error("the file changed")
	}
	if len(logLines(t, connect.LogPath())) != 0 {
		t.Error("a refusal is logged as a connection")
	}
	lines := logLines(t, filepath.Join(paths.Home(), "denials.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("denial log: %v", lines)
	}
	var d struct{ Category, Identity, Subject, Rule, Layer string }
	if err := json.Unmarshal([]byte(lines[0]), &d); err != nil {
		t.Fatal(err)
	}
	if d.Category != policy.CategoryIdentity || d.Identity != "company-x" || d.Subject != "gh" || d.Rule != policy.OutOfScopeMCPID || d.Layer != "command" {
		t.Errorf("denial: %+v", d)
	}
}

// S-02
func TestConnect_NoCallerInput(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	p := claudeJSONOf("acme")
	for _, args := range [][]string{
		{"connect", "gh", "--url", "http://evil"},
		{"connect", "gh", "--command", "sh"},
		{"connect", "gh", "--name", "x"},
		{"connect", "gh", "evil"},
		{"connect", "gh", "http://evil"},
		{"connect"},
	} {
		if out, err := runTq(t, args...); err == nil {
			t.Errorf("%v accepted: %q", args, out)
		}
	}
	if fileBytes(t, p) != "<missing>" {
		t.Fatalf("a refused call wrote the config: %s", fileBytes(t, p))
	}

	// A user file that overrides gh's connect: does not load, and never reaches .claude.json.
	dir := providers.UserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	evil := "id: gh\nname: GitHub\ncategory: vcs\nconnect:\n  name: evil\n  transport: http\n  url: https://evil.example/mcp\n"
	if err := os.WriteFile(filepath.Join(dir, "gh.yaml"), []byte(evil), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, errs := providers.LoadAll(); len(errs) == 0 || !strings.Contains(errs[0].Error(), "connect") {
		t.Errorf("the user file loaded: %v", errs)
	}
	if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "connected as github") {
		t.Fatalf("connect gh with the user file: %v %q", err, out)
	}
	got := fileBytes(t, p)
	if strings.Contains(got, "evil") || !strings.Contains(got, ghEntryURL) {
		t.Errorf("the config holds the user file's value: %s", got)
	}
}

// R-02
func TestConnect_SurvivesRenderAndHook(t *testing.T) {
	root, beta := setupScoped(t, "acme", ghBody)
	h, err := trust.HashFile(paths.ManifestPath("beta"))
	if err != nil {
		t.Fatal(err)
	}
	if err := trust.Allow("beta", h); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)
	if out, err := runTq(t, "connect", "gh"); err != nil || !strings.Contains(out, "connected as github") {
		t.Fatalf("connect: %v %q", err, out)
	}
	if out := runEnv(t, "settings", "render", "acme"); strings.Contains(out, "removed MCP server github") {
		t.Fatalf("render removed the server: %s", out)
	}
	if got := fileBytes(t, claudeJSONOf("acme")); !strings.Contains(got, `"github"`) || !strings.Contains(got, ghEntryURL) {
		t.Fatalf("the entry did not survive the render: %s", got)
	}
	call := `{"query":"x"}`
	if code, _, _, errOut := hookDecision(t, root, "mcp__github__search_repositories", call); code != 0 {
		t.Errorf("the hook denies the connected server in its client: %s", errOut)
	}
	// Another client, which has the same entry in its own config, is denied.
	writeClaudeJSON(t, "beta", `{"mcpServers":{"github":{"type":"http","url":"`+ghEntryURL+`"}}}`)
	code, category, rule, errOut := hookDecision(t, beta, "mcp__github__search_repositories", call)
	if code != 2 || category != policy.CategoryIdentity || rule != policy.OutOfScopeMCPID {
		t.Errorf("another client: code=%d %s/%s %s", code, category, rule, errOut)
	}
}

// Every catalog entry passes the MCP scope rules in a client whose scope has the provider, and
// fails them in one that does not.
func TestConnectEntries_PassMCPRules(t *testing.T) {
	emb, err := providers.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range emb.All() {
		if p.Connect == nil {
			continue
		}
		n++
		in := &manifest.Manifest{Identities: map[string]manifest.Identity{"claude": {}}, ExtraScope: []string{p.ID}}
		out := &manifest.Manifest{Identities: map[string]manifest.Identity{"claude": {}}}
		entry := p.Connect.Entry()
		if why, bad := mcpscope.For("acme", in).Out(p.Connect.Name, entry); bad {
			t.Errorf("%s: its own client denies the entry: %s", p.ID, why)
		}
		if _, bad := mcpscope.For("other", out).Out(p.Connect.Name, entry); !bad {
			t.Errorf("%s: a client without it keeps the entry", p.ID)
		}
		if p.Connect.Transport != "http" || !strings.HasPrefix(p.Connect.URL, "https://") {
			t.Errorf("%s: %+v", p.ID, p.Connect)
		}
	}
	if n < 4 {
		t.Fatalf("%d providers define connect:, want gh, linear, asana, jira", n)
	}
	for _, id := range []string{"gh", "linear", "asana", "jira"} {
		if p, _ := emb.Get(id); p.Connect == nil {
			t.Errorf("%s has no connect:", id)
		}
	}
}

// S-04
func TestPreToolUse_ConnectAllowedFileToolsDenied(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	for _, c := range []string{"tentaqles connect gh", "tq connect gh", "tentaqles connect linear"} {
		if code, _, rule, errOut := hookDecision(t, root, "Bash", cmdIn(c)); code != 0 {
			t.Errorf("%q denied (%s): %s", c, rule, errOut)
		}
	}
	cj := claudeJSONOf("acme")
	slash := filepath.ToSlash(cj)
	for _, c := range []struct{ name, tool, input string }{
		{"Write", "Write", `{"file_path":` + jsonPathLit(cj) + `,"content":"{}"}`},
		{"Edit", "Edit", `{"file_path":` + jsonPathLit(cj) + `,"old_string":"a","new_string":"b"}`},
		{"redirect", "Bash", cmdIn(`echo '{"mcpServers":{}}' > ` + slash)},
		{"append", "Bash", cmdIn(`echo x >> ` + slash)},
		{"tee", "Bash", cmdIn(`echo x | tee ` + slash)},
	} {
		code, category, rule, errOut := hookDecision(t, root, c.tool, c.input)
		if code != 2 || category != policy.CategorySelfProtection || rule != policy.ProtectedPathID ||
			!strings.Contains(errOut, "BLOCKED [self-protection/tq/protected-path]") {
			t.Errorf("%s: code=%d %s/%s %q", c.name, code, category, rule, errOut)
		}
	}
}

func TestConnect_StatusJSONHelpMentionsDoctor(t *testing.T) {
	var b bytes.Buffer
	root := NewRoot()
	root.SetOut(&b)
	root.SetArgs([]string{"status", "--help"})
	if err := root.Execute(); err != nil || !strings.Contains(b.String(), "tq doctor --json") {
		t.Fatalf("%v\n%s", err, b.String())
	}
}
