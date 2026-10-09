package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeDotenvLine is built at runtime so the commit guard never sees a
// secret-shaped literal.
func fakeDotenvLine() string { return "FLUXGATE_" + "TOKEN=" + strings.Repeat("q7", 12) + "\n" }

func hitPaths(hits []decide.Hit) []string {
	set := map[string]bool{}
	for _, h := range hits {
		set[h.Path] = true
	}
	var out []string
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func TestExplorePrefilter(t *testing.T) {
	files := map[string]string{
		"src/fluxgate.go":       "package src\n\nfunc OpenFluxgate() {}\n",
		"src/other.go":          "package src\n\nfunc Unrelated() {}\n",
		"ignored/fluxgate.go":   "fluxgate\n",
		"gen/out.log":           "fluxgate\n",
		"node_modules/x/x.js":   "fluxgate\n",
		".env":                  fakeDotenvLine(),
		".env.local":            fakeDotenvLine(),
		"keys/server.pem":       "fluxgate\n",
		"bin/blob.dat":          "fluxgate\x00\x01",
		".gitignore":            "ignored/\n*.log\n",
		"docs/notes/README.txt": "the FLUXGATE opens at dawn\n",
	}
	want := []string{"docs/notes/README.txt", "src/fluxgate.go"}

	t.Run("walk outside a repo", func(t *testing.T) {
		root := testutil.TempDir(t)
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
		writeTree(t, root, files)
		if isGitWorkTree(root) {
			t.Skip("temp dir is inside a git work tree")
		}
		hits, err := exploreHits(root, []string{"fluxgate"})
		if err != nil {
			t.Fatal(err)
		}
		if got := hitPaths(hits); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("hits in %v, want %v", got, want)
		}
	})

	t.Run("git grep inside a repo", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not installed")
		}
		root := testutil.TempDir(t)
		writeTree(t, root, files)
		cmd := exec.Command("git", "init", "-q")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v %s", err, out)
		}
		// Untracked files count too: nothing is committed here. The .env
		// files are not ignored, so only the explore skip list keeps them out.
		hits, err := exploreHits(root, []string{"fluxgate"})
		if err != nil {
			t.Fatal(err)
		}
		// git grep -I skips the binary; node_modules is not ignored in this
		// repo, so git finds it (a real repo ignores it).
		got := hitPaths(hits)
		for _, p := range got {
			if strings.HasPrefix(p, ".env") || strings.HasSuffix(p, ".pem") || strings.HasPrefix(p, "ignored/") ||
				strings.HasSuffix(p, ".log") || strings.HasSuffix(p, ".dat") {
				t.Fatalf("pre-filter returned %s: %v", p, got)
			}
		}
		for _, w := range want {
			if !contains(got, w) {
				t.Fatalf("missing %s in %v", w, got)
			}
		}
		for _, h := range hits {
			if h.Path == "src/fluxgate.go" && h.Line != 3 {
				t.Fatalf("line = %d, want 3", h.Line)
			}
		}
	})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type exploreJSON struct {
	Mode     string `json:"mode"`
	Fallback string `json:"fallback"`
	Batches  int    `json:"batches"`
	Results  []struct {
		ID    string  `json:"id"`
		Path  string  `json:"path"`
		Start int     `json:"start"`
		Score float64 `json:"score"`
	} `json:"results"`
}

func exploreRepo(t *testing.T, root string) {
	t.Helper()
	writeTree(t, root, map[string]string{
		"a/fluxgate.go":  "package a\n// fluxgate opens\nfunc Open() {}\n",
		"b/fluxgate.go":  "package b\n// fluxgate closes\nfunc Close() {}\n",
		"c/fluxgate.go":  "package c\n// fluxgate valve\nfunc Valve() {}\n",
		"d/unrelated.go": "package d\n",
		".env":           fakeDotenvLine(),
	})
}

func runExplore(t *testing.T, args ...string) (exploreJSON, string) {
	t.Helper()
	code, out, _, err := runTQ(t, append([]string{"decide", "explore"}, args...)...)
	if err != nil || code != 0 {
		t.Fatalf("explore: code=%d err=%v out=%s", code, err, out)
	}
	var v exploreJSON
	if strings.Contains(strings.Join(args, " "), "--json") {
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
	}
	return v, out
}

func TestExploreCLI_JevReranks(t *testing.T) {
	// Jev prefers the span for c/, the last in keyword order (ties break
	// by path), so the top result proves the re-rank.
	url, calls := fakeJev(t, func(qs map[string]any) map[string]any {
		out := map[string]any{}
		for id := range qs {
			p := 0.1
			if id == "s3" {
				p = 0.95
			}
			out[id] = map[string]any{"noul": p}
		}
		return out
	}, 0)
	ws := jevWorkspace(t, url, "shadow")
	exploreRepo(t, ws)
	v, _ := runExplore(t, "where does the fluxgate open?", "--path", ws, "--json", "--top", "2")
	if v.Mode != "jev" || calls.Load() != 1 || v.Batches != 1 {
		t.Fatalf("mode=%s calls=%d batches=%d fallback=%q", v.Mode, calls.Load(), v.Batches, v.Fallback)
	}
	if len(v.Results) != 2 || v.Results[0].Path != "c/fluxgate.go" || v.Results[0].Score != 0.95 {
		t.Fatalf("results = %+v", v.Results)
	}
	// Explore is a CLI ranking, not a gate: it writes the cost log, never
	// a judgment line.
	if _, err := os.Stat(filepath.Join(paths.Home(), "decide", "log.jsonl")); err != nil {
		t.Fatalf("cost log missing: %v", err)
	}
}

func TestExploreCLI_FallsBackWhenJevIsDown(t *testing.T) {
	url, _ := fakeJev(t, nil, 503)
	ws := jevWorkspace(t, url, "shadow")
	exploreRepo(t, ws)
	v, _ := runExplore(t, "where does the fluxgate open?", "--path", ws, "--json")
	if v.Mode != "keyword" || !strings.Contains(v.Fallback, "jev unavailable") || len(v.Results) != 3 {
		t.Fatalf("mode=%s fallback=%q results=%d", v.Mode, v.Fallback, len(v.Results))
	}
	_, text := runExplore(t, "where does the fluxgate open?", "--path", ws)
	if !strings.Contains(text, "keyword ranking") || !strings.Contains(text, "a/fluxgate.go:1-") {
		t.Fatalf("text output: %s", text)
	}
}

func TestExploreCLI_NoWorkspaceStillAnswers(t *testing.T) {
	isolateHome(t)
	root := testutil.TempDir(t)
	exploreRepo(t, root)
	v, _ := runExplore(t, "fluxgate valve", "--path", root, "--json")
	if v.Mode != "keyword" || !strings.Contains(v.Fallback, "not in a trusted workspace") || len(v.Results) == 0 {
		t.Fatalf("mode=%s fallback=%q", v.Mode, v.Fallback)
	}
	if v.Results[0].Path != "c/fluxgate.go" {
		t.Fatalf("both terms hit c/ only: %+v", v.Results)
	}
	_, text := runExplore(t, "zzqv nothing matches", "--path", root)
	if !strings.Contains(text, "no keyword hits") {
		t.Fatalf("text: %s", text)
	}
}

// TestMemoryGateContract runs the exact commands plugin/tentaqles/memory/
// jev_gate.py issues, so a CLI change that would break the gate fails here.
func TestMemoryGateContract(t *testing.T) {
	url, _ := fakeJev(t, noulAll(0.73), 0)
	ws := jevWorkspace(t, url, "shadow")
	t.Chdir(ws)
	state := filepath.Join(t.TempDir(), "tq-jev-x.json")
	os.WriteFile(state, []byte(`{"tool":"Bash","text":"decided to switch"}`), 0o600)
	code, out, _, err := runTQ(t, "decide", "ask", "--state-file", state, "--json", "--timeout", "5s", "--purpose", "memory-capture",
		"--q", "worth=noul:Does data.text record a decision, a root cause, a workaround or a discovery that would be worth remembering?")
	if err != nil || code != 0 {
		t.Fatalf("ask: code=%d err=%v", code, err)
	}
	var answers map[string]struct {
		Noul *float64 `json:"noul"`
	}
	if json.Unmarshal([]byte(out), &answers) != nil || answers["worth"].Noul == nil || *answers["worth"].Noul != 0.73 {
		t.Fatalf("ask --json output: %s", out)
	}
	logRaw, _ := os.ReadFile(filepath.Join(paths.Home(), "decide", "log.jsonl"))
	if !strings.Contains(string(logRaw), `"purpose":"memory-capture"`) {
		t.Fatalf("cost log purpose: %s", logRaw)
	}
	code, _, _, err = runTQ(t, "decide", "log", "--kind", "memory-recall", "--mode", "shadow", "--p", "f0=0.7300",
		"--would", "facts=reorder", "--extra", "facts_overlap=2/5", "--extra", "candidates=10", "--error", "")
	if err != nil || code != 0 {
		t.Fatalf("log: code=%d err=%v", code, err)
	}
	raw, _ := os.ReadFile(filepath.Join(paths.Home(), "decide", "judgments.jsonl"))
	if !strings.Contains(string(raw), `"ws":"acme"`) || !strings.Contains(string(raw), `"facts_overlap":"2/5"`) {
		t.Fatalf("judgment: %s", raw)
	}
}

func TestDecideLog(t *testing.T) {
	isolateHome(t)
	code, _, _, err := runTQ(t, "decide", "log", "--kind", "memory-capture", "--p", "worth=0.82", "--would", "worth=keep", "--extra", "tool=Bash")
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	raw, err := os.ReadFile(filepath.Join(paths.Home(), "decide", "judgments.jsonl"))
	if err != nil || !strings.Contains(string(raw), `"kind":"memory-capture"`) || !strings.Contains(string(raw), `"worth":0.82`) ||
		!strings.Contains(string(raw), `"mode":"shadow"`) {
		t.Fatalf("log: %v %s", err, raw)
	}
	for _, bad := range [][]string{
		{"--kind", "Memory Capture"},
		{"--kind", "memory-capture", "--p", "worth=1.5"},
		{"--kind", "memory-capture", "--extra", "note=the user decided to drop the table"},
		{"--kind", "memory-capture", "--mode", "loud"},
	} {
		if _, _, _, err := runTQ(t, append([]string{"decide", "log"}, bad...)...); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
