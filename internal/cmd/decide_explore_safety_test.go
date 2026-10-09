package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// Fake credentials are assembled at runtime so the commit guard never sees
// a secret-shaped literal in this file.
func fakeGitHubToken() string { return "gh" + "p_" + strings.Repeat("Z9y8", 9) }
func fakeAWSKey() string      { return "AK" + "IA" + "QWERTYUIOPASDFGH" }
func fakePEM() string {
	return "-----BEGIN " + "RSA PRIVATE KEY-----\n" +
		"MIIEowIBAAKCAQEA" + strings.Repeat("Qk", 20) + "\n" +
		strings.Repeat("Lm", 30) + "\n" +
		"-----END " + "RSA PRIVATE KEY-----"
}

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestExploreSecretFilesNeverYielded: secret-bearing files are kept out by
// the allowlist and deny-list alone, whether ignore files list them,
// are missing, or are broken.
func TestExploreSecretFilesNeverYielded(t *testing.T) {
	secretFiles := map[string]string{
		".env":         fakeDotenvLine(),
		"secrets.json": `{"token": "` + fakeGitHubToken() + `"}` + "\n",
		"config.yaml":  "api_key: " + fakeGitHubToken() + "\n",
		"id_rsa":       fakePEM() + "\n",
		"notes.sql.gz": "fluxgate\n",
		"ok.go":        "package ok\n",
	}
	listing := ".env\nsecrets.json\nconfig.yaml\nid_rsa\nnotes.sql.gz\n"
	variants := map[string]func(t *testing.T, root string){
		"ignore files missing": func(*testing.T, string) {},
		"listed in .gitignore": func(t *testing.T, root string) {
			writeTree(t, root, map[string]string{".gitignore": listing})
		},
		"broken .gitignore": func(t *testing.T, root string) {
			writeTree(t, root, map[string]string{".gitignore": listing + "[unclosed\n"})
		},
		".gitignore is a directory": func(t *testing.T, root string) { mkdirAll(t, filepath.Join(root, ".gitignore")) },
		"broken info/exclude and global": func(t *testing.T, root string) {
			writeTree(t, root, map[string]string{".git/info/exclude": "[z-a]\n"})
			mkdirAll(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "git", "ignore"))
		},
	}
	never := []string{".env", "secrets.json", "id_rsa", "notes.sql.gz"}
	for name, setup := range variants {
		t.Run(name, func(t *testing.T) {
			isolateGitConfig(t)
			root := testutil.TempDir(t)
			writeTree(t, root, secretFiles)
			setup(t, root)

			got, _ := walkedWith(t, root, exploreOpts{})
			if strings.Join(got, ",") != "ok.go" {
				t.Fatalf("default walk yielded %v", got)
			}
			got, _ = walkedWith(t, root, exploreOpts{IncludeConfig: true})
			for _, f := range never {
				if contains(got, f) {
					t.Fatalf("--include-config yielded %s: %v", f, got)
				}
			}
		})
	}
}

func TestExploreRedactsEverySpan(t *testing.T) {
	isolateGitConfig(t)
	root := testutil.TempDir(t)
	body := strings.Join([]string{
		"package keys",
		"",
		"// fluxgate client",
		`const token = "` + fakeGitHubToken() + `"`,
		`var aws = "` + fakeAWSKey() + `"`,
		"const pem = `" + fakePEM() + "`",
		"func Fluxgate() {}",
	}, "\n") + "\n"
	writeTree(t, root, map[string]string{"keys/client.go": body})

	var text string
	_, err := walkFiles(root, exploreOpts{}, func(rel, s string) bool { text = s; return true })
	if err != nil || text == "" {
		t.Fatalf("file not yielded: %v", err)
	}
	for _, raw := range []string{fakeGitHubToken(), fakeAWSKey(), "MIIEowIBAAKCAQEA", strings.Repeat("Lm", 30)} {
		if strings.Contains(text, raw) {
			t.Fatalf("unredacted value in yielded text:\n%s", text)
		}
	}
	if !strings.Contains(text, "[REDACTED:github_token]") || !strings.Contains(text, "[REDACTED:private_key]") {
		t.Fatalf("redaction markers missing:\n%s", text)
	}
	// Line numbers survive redaction: spans still point at the real lines.
	bodyLines := strings.Split(body, "\n")
	fnLine := len(bodyLines) - 2 // "func Fluxgate() {}" before the final newline
	if strings.Count(text, "\n") != strings.Count(body, "\n") || bodyLines[fnLine] != "func Fluxgate() {}" ||
		strings.Split(text, "\n")[fnLine] != "func Fluxgate() {}" {
		t.Fatalf("redaction shifted lines:\n%s", text)
	}

	// End to end: nothing secret-shaped reaches the (fake) Jev endpoint.
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		mu.Unlock()
		var req struct {
			Questions map[string]any `json:"questions"`
		}
		_ = json.Unmarshal(raw, &req)
		ans := map[string]any{}
		for id := range req.Questions {
			ans[id] = map[string]any{"noul": 0.9}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": ans})
	}))
	t.Cleanup(srv.Close)
	ws := jevWorkspace(t, srv.URL, "shadow")
	isolateGitConfig(t)
	writeTree(t, ws, map[string]string{"keys/client.go": body})
	v, _ := runExplore(t, "fluxgate client", "--path", ws, "--json")
	if v.Mode != "jev" || len(bodies) == 0 {
		t.Fatalf("mode=%s requests=%d fallback=%q", v.Mode, len(bodies), v.Fallback)
	}
	for _, b := range bodies {
		for _, raw := range []string{fakeGitHubToken(), fakeAWSKey(), "MIIEowIBAAKCAQEA"} {
			if strings.Contains(b, raw) {
				t.Fatal("a secret reached the Jev request")
			}
		}
		if !strings.Contains(b, "func Fluxgate()") {
			t.Fatalf("span text missing from request: %.200s", b)
		}
	}
}

func TestExploreIncludeConfig(t *testing.T) {
	isolateGitConfig(t)
	root := testutil.TempDir(t)
	writeTree(t, root, map[string]string{
		"app/settings.yaml":    "fluxgate:\n  api_key: " + fakeGitHubToken() + "\n",
		"app/routes.json":      `{"fluxgate": "/v1"}` + "\n",
		"app/main.go":          "package app // fluxgate\n",
		"app/secrets.yaml":     "fluxgate: 1\n",
		"app/credentials.json": `{"fluxgate": 1}` + "\n",
		"app/.env.yaml":        "fluxgate: 1\n",
		"app/prod.tfstate":     `{"fluxgate": 1}` + "\n",
	})
	got, _ := walkedWith(t, root, exploreOpts{})
	if strings.Join(got, ",") != "app/main.go" {
		t.Fatalf("config read without --include-config: %v", got)
	}
	texts := map[string]string{}
	_, err := walkFiles(root, exploreOpts{IncludeConfig: true}, func(rel, s string) bool { texts[rel] = s; return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(texts) != 3 || texts["app/settings.yaml"] == "" || texts["app/routes.json"] == "" {
		t.Fatalf("--include-config yielded %v", keys(texts))
	}
	if strings.Contains(texts["app/settings.yaml"], fakeGitHubToken()) || !strings.Contains(texts["app/settings.yaml"], "[REDACTED:") {
		t.Fatalf("config not redacted: %s", texts["app/settings.yaml"])
	}

	// The flag is wired through the CLI.
	isolateHome(t)
	v, _ := runExplore(t, "fluxgate", "--path", root, "--no-jev", "--json", "--include-config", "--top", "10")
	var paths []string
	for _, r := range v.Results {
		paths = append(paths, r.Path)
	}
	if !contains(paths, "app/settings.yaml") || contains(paths, "app/secrets.yaml") {
		t.Fatalf("CLI results: %v", paths)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestExploreUnreadableDirIsCounted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not remove read access on Windows")
	}
	isolateGitConfig(t)
	root := testutil.TempDir(t)
	writeTree(t, root, map[string]string{"top.go": "x\n", "locked/a.go": "x\n"})
	p := filepath.Join(root, "locked")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o755) })
	if _, err := os.ReadDir(p); err == nil {
		t.Skip("running with privileges that bypass chmod")
	}
	got, stats := walked(t, root)
	if strings.Join(got, ",") != "top.go" || stats.UnreadableDirs != 1 {
		t.Fatalf("got %v unreadable=%d", got, stats.UnreadableDirs)
	}
}

// TestExploreSkipsNestedRepos: a directory below the root holding its own
// .git (a nested clone, submodule or worktree) is never entered.
func TestExploreSkipsNestedRepos(t *testing.T) {
	isolateGitConfig(t)
	isolateHome(t)
	root := testutil.TempDir(t)
	writeTree(t, root, map[string]string{
		".git/HEAD":                        "ref: refs/heads/main\n",
		"own/fluxgate.go":                  "fluxgate\n",
		"clients/acme/.git/HEAD":           "ref: refs/heads/main\n",
		"clients/acme/fluxgate.go":         "fluxgate acme\n",
		"clients/acme/deep/fluxgate.go":    "fluxgate acme\n",
		"vendor-ish/lib/.git":              "gitdir: ../../.git/modules/lib\n",
		"vendor-ish/lib/fluxgate.go":       "fluxgate lib\n",
		".git/modules/lib/HEAD":            "ref: refs/heads/main\n",
		"clients/plain-folder/fluxgate.go": "fluxgate plain\n",
	})
	got, stats := walked(t, root)
	if strings.Join(got, ",") != "clients/plain-folder/fluxgate.go,own/fluxgate.go" || stats.NestedRepos != 2 {
		t.Fatalf("got %v nested=%d", got, stats.NestedRepos)
	}
	// Paths reaching the reader from elsewhere get the same rule.
	if _, ok := readExploreFile(root, "clients/acme/deep/fluxgate.go", exploreOpts{}); ok {
		t.Fatal("read a file inside a nested repo")
	}
	_, text := runExplore(t, "fluxgate", "--path", root, "--no-jev")
	if !strings.Contains(text, "skipped 2 nested repo(s)") || strings.Contains(text, "acme") {
		t.Fatalf("footer: %s", text)
	}
	// --path inside the nested repo searches it: its own repo is the root's.
	got, stats = walked(t, filepath.Join(root, "clients", "acme"))
	if strings.Join(got, ",") != "deep/fluxgate.go,fluxgate.go" || stats.NestedRepos != 0 {
		t.Fatalf("inside nested repo: got %v nested=%d", got, stats.NestedRepos)
	}
}

func TestExploreAllowlist(t *testing.T) {
	cases := map[string]bool{
		"a.go": true, "b.py": true, "c.tsx": true, "d.sql": true, "e.ps1": true, "f.astro": true,
		"README.md": true, "notes.txt": true, "Makefile": true, "Dockerfile": true, "docs/x.rst": true,
		"a.json": false, "a.yaml": false, "a.yml": false, "a.toml": false, "a.ini": false,
		"a.properties": false, "a.xml": false, "a.tfvars": false, ".env": false, "a.cfg": false,
		"a.log": false, "a.csv": false, "a.exe": false, "noext": false,
	}
	for f, want := range cases {
		if got := allowedFile(f, exploreOpts{}); got != want {
			t.Errorf("allowedFile(%q) = %v, want %v", f, got, want)
		}
	}
	for _, f := range []string{"a.json", "a.yaml", "a.yml", "a.toml", "a.ini", "a.properties", "a.xml", "a.tfvars"} {
		if !allowedFile(f, exploreOpts{IncludeConfig: true}) {
			t.Errorf("--include-config should admit %s", f)
		}
	}
}
