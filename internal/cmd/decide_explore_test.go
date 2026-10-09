package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	isolateGitConfig(t)
	root := testutil.TempDir(t)
	writeTree(t, root, map[string]string{
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
		// A nested .gitignore applies below its own directory only.
		"pkg/.gitignore":       "generated.go\n",
		"pkg/generated.go":     "fluxgate\n",
		"pkg/fluxgate_impl.go": "fluxgate\n",
		"generated.go":         "fluxgate\n",
		// .git/info/exclude is honoured; .git itself is never walked.
		".git/info/exclude":   "scratch/\n",
		".git/config":         "fluxgate\n",
		"scratch/fluxgate.go": "fluxgate\n",
	})
	hits, _, err := exploreHits(root, []string{"fluxgate"}, exploreOpts{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docs/notes/README.txt", "generated.go", "pkg/fluxgate_impl.go", "src/fluxgate.go"}
	if got := hitPaths(hits); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("hits in %v, want %v", got, want)
	}
	for _, h := range hits {
		if h.Path == "src/fluxgate.go" && h.Line != 3 {
			t.Fatalf("line = %d, want 3", h.Line)
		}
	}
}

// TestExploreNeverRunsRepoConfig: explore runs no git process, so a hostile
// repo's core.fsmonitor (or pager, diff driver, textconv…) never executes.
func TestExploreNeverRunsRepoConfig(t *testing.T) {
	isolateGitConfig(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	isolateHome(t)
	root := testutil.TempDir(t)
	marker := filepath.Join(testutil.TempDir(t), "pwned")
	hook := filepath.Join(testutil.TempDir(t), "fsmonitor.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho x > '"+filepath.ToSlash(marker)+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"a/fluxgate.go": "package a\n// fluxgate\n"})
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "core.fsmonitor", filepath.ToSlash(hook)},
		{"config", "core.pager", "sh '" + filepath.ToSlash(hook) + "'"},
		{"config", "diff.external", filepath.ToSlash(hook)},
	} {
		cmd := exec.Command(git, args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	v, _ := runExplore(t, "where is the fluxgate", "--path", root, "--json", "--no-jev")
	if len(v.Results) != 1 || v.Results[0].Path != "a/fluxgate.go" {
		t.Fatalf("results = %+v", v.Results)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("explore ran the repo's configured program")
	}
	// Control: git itself does run the trap, so the test proves something.
	cmd := exec.Command(git, "status")
	cmd.Dir = root
	_ = cmd.Run()
	if _, err := os.Stat(marker); err != nil {
		t.Logf("control: git status did not fire the fsmonitor hook on this platform")
	}
}

// symlinkOrSkip creates a link, or skips where the OS refuses (Windows
// without developer mode or the privilege).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
}

func TestExploreNeverFollowsSymlinksOutOfRoot(t *testing.T) {
	isolateGitConfig(t)
	outside := testutil.TempDir(t)
	writeTree(t, outside, map[string]string{
		"secret.go":      "fluxgate other-client secret\n",
		"repo/stolen.go": "fluxgate other-client code\n",
	})
	root := testutil.TempDir(t)
	writeTree(t, root, map[string]string{"own/fluxgate.go": "fluxgate here\n"})
	symlinkOrSkip(t, filepath.Join(outside, "secret.go"), filepath.Join(root, "link.go"))
	symlinkOrSkip(t, filepath.Join(outside, "repo"), filepath.Join(root, "linkdir"))

	hits, _, err := exploreHits(root, []string{"fluxgate"}, exploreOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got := hitPaths(hits); strings.Join(got, ",") != "own/fluxgate.go" {
		t.Fatalf("walk followed a link: %v", got)
	}
	// Paths that reach the reader from elsewhere get the same checks: a
	// linked file, a file under a linked directory, and a ".." escape.
	for _, rel := range []string{"link.go", "linkdir/stolen.go", "../" + filepath.Base(outside) + "/secret.go"} {
		if raw, ok := readExploreFile(root, rel, exploreOpts{}); ok {
			t.Errorf("read %s: %q", rel, raw)
		}
		fc := &fileCache{root: root, lines: map[string][]string{}}
		if fc.text(rel, 1, 40) != "" {
			t.Errorf("span text for %s", rel)
		}
	}
	if _, ok := readExploreFile(root, "own/fluxgate.go", exploreOpts{}); !ok {
		t.Fatal("regular file inside root was refused")
	}
}

// TestExploreNeverFollowsJunctions: on Windows a directory junction needs no
// privilege, so it is the realistic way a repo points outside itself.
func TestExploreNeverFollowsJunctions(t *testing.T) {
	isolateGitConfig(t)
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows-only")
	}
	outside := testutil.TempDir(t)
	writeTree(t, outside, map[string]string{"stolen.go": "fluxgate other-client code\n"})
	root := testutil.TempDir(t)
	writeTree(t, root, map[string]string{"own/fluxgate.go": "fluxgate here\n"})
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, "junction"), outside).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	hits, _, err := exploreHits(root, []string{"fluxgate"}, exploreOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if got := hitPaths(hits); strings.Join(got, ",") != "own/fluxgate.go" {
		t.Fatalf("walk followed a junction: %v", got)
	}
	if raw, ok := readExploreFile(root, "junction/stolen.go", exploreOpts{}); ok {
		t.Fatalf("read through a junction: %q", raw)
	}
}

func TestExploreRootThroughSymlinkStillWorks(t *testing.T) {
	isolateGitConfig(t)
	real := testutil.TempDir(t)
	writeTree(t, real, map[string]string{"a/fluxgate.go": "fluxgate\n"})
	link := filepath.Join(testutil.TempDir(t), "ws")
	symlinkOrSkip(t, real, link)
	root, err := exploreRoot(link)
	if err != nil {
		t.Fatal(err)
	}
	hits, _, _ := exploreHits(root, []string{"fluxgate"}, exploreOpts{})
	if len(hits) != 1 {
		t.Fatalf("hits = %v", hits)
	}
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
	isolateGitConfig(t)
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
	isolateGitConfig(t)
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
	isolateGitConfig(t)
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
