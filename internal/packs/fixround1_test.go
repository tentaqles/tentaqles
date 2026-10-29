package packs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/policy"
)

// F4: a check's Timeout reaches the tool run when the check sets none.
func TestCheckTimeoutReachesTheRunner(t *testing.T) {
	isolate(t)
	in := goldenInput(t)
	ft := in.Tools.(fakeTools)
	ft.seen = map[string]time.Duration{}
	in.Tools = ft
	RunChecks(context.Background(), in, RunOptions{Packs: []string{"data"}})
	if ft.seen["sqlfluff"] != 300*time.Second {
		t.Fatalf("Spec.Timeout %v, want the check's 300s", ft.seen["sqlfluff"])
	}
}

// A spent budget fails the remaining checks without probing tools.
func TestSpentBudgetSkipsProbes(t *testing.T) {
	isolate(t)
	in := goldenInput(t)
	ft := in.Tools.(fakeTools)
	ft.probed = map[string]int{}
	in.Tools = ft
	in.Budget = time.Now().Add(-time.Second)
	rep := RunChecks(context.Background(), in, RunOptions{})
	if len(ft.probed) != 0 {
		t.Fatalf("probed %v after the budget", ft.probed)
	}
	for _, r := range rep.Results {
		if r.Status != Fail || r.Detail != "timed out" {
			t.Fatalf("%+v", r)
		}
	}
}

// F7: growth is seen for a non-canonical baseline path, and a swap of one
// entry for another (same count) is growth.
func TestBaselineGrowthNonCanonicalAndSwap(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	writeFile(t, root, ".tq/base.json", `{"entries":[{"check":"a","fingerprint":"sha256:2"}]}`)
	for _, spelling := range []string{"./.tq/base.json", `.tq\base.json`} {
		m := packsOf(t, "data: { baseline: '"+spelling+"' }\n")
		in := &Input{Root: root, Base: "HEAD", Config: Merge(m, nil), Changed: []ChangedFile{{Path: ".tq/base.json", Status: 'M'}},
			Git: oldSide{".tq/base.json": `{"entries":[{"check":"a","fingerprint":"sha256:1"}]}`}}
		if !BaselineGrew(in.ForPack("data")) {
			t.Fatalf("%q: a swapped entry is growth", spelling)
		}
	}
}

// F14: the staged read honours max.
func TestStagedReadHonoursMax(t *testing.T) {
	in := &Input{Staged: true, Git: oldSide{"big.sql": strings.Repeat("x", 100)}}
	if _, err := in.ReadNew("big.sql", 10); err == nil {
		t.Fatal("over max accepted")
	}
}

func TestReadOldAbsentWrapsErrNotExist(t *testing.T) {
	if _, err := (&Input{}).ReadOld("x"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if !absentAtRef(errors.New("git show: exit status 128: fatal: path 'new.yaml' does not exist in 'HEAD'")) ||
		!absentAtRef(errors.New("fatal: path 'a' exists on disk, but not in 'HEAD'")) {
		t.Fatal("git's absent messages")
	}
}

// F10: --paths cleans .., refuses escapes, expands directories and keeps
// git's spelling; ReadOld of an added file is ErrNotExist.
func TestSelectPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolate(t)
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	writeFile(t, root, "models/a.sql", "select 1")
	writeFile(t, root, "models/sub/b.sql", "select 2")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "init")
	writeFile(t, root, "models/new.sql", "select 3")
	ctx := context.Background()
	for _, bad := range []string{"sub/../../outside.txt", "../x", filepath.Join(filepath.Dir(root), "elsewhere.sql")} {
		if _, _, err := Select(ctx, root, Selection{Paths: []string{bad}}); !errors.Is(err, ErrSelection) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	cs, _, err := Select(ctx, root, Selection{Paths: []string{"models/"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]byte{}
	for _, c := range cs {
		got[c.Path] = c.Status
	}
	if got["models/a.sql"] != 'M' || got["models/sub/b.sql"] != 'M' || got["models/new.sql"] != 'A' || len(got) != 3 {
		t.Fatalf("directory expansion: %v", got)
	}
	cs, _, _ = Select(ctx, root, Selection{Paths: []string{"models/x/../a.sql", filepath.Join(root, "models", "new.sql")}})
	if len(cs) != 2 || cs[0].Path != "models/a.sql" || cs[0].Status != 'M' || cs[1].Path != "models/new.sql" {
		t.Fatalf("cleaned and absolute: %+v", cs)
	}
	if runtime.GOOS == "windows" {
		cs, _, _ = Select(ctx, root, Selection{Paths: []string{"MODELS/A.SQL"}})
		if len(cs) != 1 || cs[0].Path != "models/a.sql" || cs[0].Status != 'M' {
			t.Fatalf("case-insensitive on Windows: %+v", cs)
		}
	}
	in := &Input{Root: root, Base: "HEAD", Git: GitRepo{Root: root}}
	if _, err := in.ReadOld("models/new.sql"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("added file: %v", err)
	}
}

// C1: guards run only for packs the trusted manifest enables.
func TestGuardsNeedTheTrustedManifest(t *testing.T) {
	isolate(t)
	env := dataEnv(t)
	env.Manifest = nil
	if os := EvaluateGuards(bash("bq query x"), env, nil, nil); len(os) != 0 {
		t.Fatalf("no manifest: %+v", os)
	}
	// A config that claims the pack is enabled is not enough.
	env = envFor(t, "data: { enabled: false }\n")
	env.Config = Merge(packsOf(t, "data: { enabled: true }\n"), nil)
	if os := EvaluateGuards(bash("bq query x"), env, nil, nil); len(os) != 0 {
		t.Fatalf("manifest disabled: %+v", os)
	}
}

// M4: a guard can never supersede a protected rule.
func TestUnsupersedableIDs(t *testing.T) {
	for _, id := range []string{"tq/hub-store-write", "tq/pack-config-change", "tq/secret-store-read", "tq/env-file-read", "tq/guard-change"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("%s accepted in Supersedes", id)
				}
			}()
			RegisterGuard(Guard{ID: "tq/x-" + strings.ReplaceAll(id, "/", "-"), Pack: "db", Supersedes: []string{id},
				Check: func(policy.ToolCall, GuardCtx) (policy.Action, string, bool) { return policy.Allow, "", true }})
		}()
	}
}

// m4: one total budget for all guards of a hook call.
func TestTotalGuardBudget(t *testing.T) {
	isolate(t)
	prev := totalGuardBudget
	totalGuardBudget = -1
	defer func() { totalGuardBudget = prev }()
	os := EvaluateGuards(bash("ls"), dataEnv(t), nil, nil)
	if len(os) == 0 {
		t.Fatal("no outcomes")
	}
	for _, o := range os {
		if o.Action != policy.Deny || !o.Failed || !strings.Contains(o.Reason, "total budget") {
			t.Fatalf("%+v", o)
		}
	}
}

// ReadState and HashState refuse a hard link inside the state directory.
func TestStateReadsRefuseHardLinks(t *testing.T) {
	home := isolate(t)
	g := NewGuardCtx(dataEnv(t), "data")
	dir := StateDir("acme", "data")
	outside := filepath.Join(home, "outside.jsonl")
	os.WriteFile(outside, []byte("x"), 0o600)
	if err := os.Link(outside, filepath.Join(dir, "ledger.jsonl")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if _, err := g.ReadState("ledger.jsonl"); !errors.Is(err, ErrRefused) {
		t.Fatalf("ReadState: %v", err)
	}
	if _, err := g.HashState("ledger.jsonl"); !errors.Is(err, ErrRefused) {
		t.Fatalf("HashState: %v", err)
	}
}

// M1: ReadLocal resolves the final path from the handle: a hard link, a
// directory link in the middle of the path and (on Windows) an 8.3 short
// name cannot reach a protected or outside file.
func TestReadLocalResolvesTheFinalPath(t *testing.T) {
	home := isolate(t)
	ws := filepath.Join(home, "acme")
	writeFile(t, ws, "ok.txt", "fine")
	writeFile(t, home, "secret/id_ed25519", "KEY")
	writeFile(t, home, ".aws/credentials", "[default]")
	env := dataEnv(t)
	env.WorkspaceRoot, env.RepoRoot = ws, ws
	g := NewGuardCtx(env, "cloud")
	if b, err := g.ReadLocal("ok.txt", 0); err != nil || string(b) != "fine" {
		t.Fatalf("%q %v", b, err)
	}
	// A hard link to an outside file.
	if err := os.Link(filepath.Join(home, "secret", "id_ed25519"), filepath.Join(ws, "hard.txt")); err == nil {
		if _, err := g.ReadLocal("hard.txt", 0); !errors.Is(err, ErrRefused) {
			t.Fatalf("hard link: %v", err)
		}
	}
	// A directory link in the middle of the path.
	link := filepath.Join(ws, "cfg")
	var mk error
	if runtime.GOOS == "windows" {
		mk = exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Join(home, "secret")).Run()
	} else {
		mk = os.Symlink(filepath.Join(home, "secret"), link)
	}
	if mk == nil {
		if _, err := g.ReadLocal("cfg/id_ed25519", 0); !errors.Is(err, ErrRefused) {
			t.Fatalf("directory link: %v", err)
		}
	}
	if runtime.GOOS == "windows" {
		short := shortName(t, filepath.Join(home, ".aws", "credentials"))
		if short != "" && !strings.EqualFold(short, filepath.Join(home, ".aws", "credentials")) {
			if _, err := g.ReadLocal(short, 0); !errors.Is(err, ErrRefused) {
				t.Fatalf("short name %s: %v", short, err)
			}
		} else {
			t.Log("8.3 names are off on this volume; short-name case not exercised")
		}
	}
}

// FromCommand returns what the tq pack <pack> parent stored.
func TestFromCommand(t *testing.T) {
	root := &cobra.Command{Use: "data"}
	leaf := &cobra.Command{Use: "x"}
	root.AddCommand(leaf)
	if _, err := FromCommand(leaf); !errors.Is(err, ErrNoCommandInput) {
		t.Fatalf("%v", err)
	}
	in := (&Input{Root: "r"}).ForPack("data")
	in.SetWorkspace("acme", "w")
	leaf.SetContext(WithInput(context.Background(), in, nil))
	got, err := FromCommand(leaf)
	if err != nil || got.Workspace() != "acme" || got.Pack() != "data" {
		t.Fatalf("%v %+v", err, got)
	}
}

var _ = toolrun.Probe{}

// F16 and the review's test gaps.
func TestSharpEdges(t *testing.T) {
	c := Merge(packsOf(t, "data: { example_block: { churn.v2: { limit: 3 } }, example_paths: { a: b } }\n"), nil)
	d := c.Pack("data")
	if d.Strings("example_paths") != nil {
		t.Fatal("Strings on a mapping must be nil")
	}
	if n := d.NodeAt("example_block", "churn.v2", "limit"); n == nil || n.Value != "3" {
		t.Fatal("NodeAt addresses dotted keys")
	}
	if !MatchGlob("./models/*.sql", "models/a.sql") {
		t.Fatal("a leading ./ in the glob")
	}
	cfg := Merge(packsOf(t, "data: { enabled: true }\nml: { enabled: true }\n"), nil)
	if got := CrewDetail(cfg, []string{"baseline_change"}, map[string]string{"baseline_change": "ml"}); len(got) != 0 {
		t.Fatalf("baseline_change of ml must not add the data reviewer: %v", got)
	}
	if got := CrewDetail(cfg, []string{"baseline_change"}, map[string]string{"baseline_change": "data"}); len(got) != 1 || got[0] != "pipeline-reviewer" {
		t.Fatalf("%v", got)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("a bad hint glob must panic")
			}
		}()
		RegisterPack(Pack{Name: "db", Hints: []Hint{{Glob: "[a-"}}})
	}()
	// Scalar spellings are part of the config: 1 and "1" hash apart, 600s and 10m too.
	h := func(s string) string { return Merge(packsOf(t, s), nil).Hash() }
	if h("data: { x: 1 }\n") == h("data: { x: '1' }\n") || h("data: { x: 600s }\n") == h("data: { x: 10m }\n") {
		t.Fatal("hash must see the scalar's tag and text")
	}
	if h("data: { x: 1 }\n") != h("data: {x: 1}\n") {
		t.Fatal("YAML layout must not change the hash")
	}
}

// Rename status and OldPath in the default and staged selections.
func TestSelectRename(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolate(t)
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	writeFile(t, root, "old.sql", strings.Repeat("select 1;\n", 20))
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "init")
	gitRun(t, root, "mv", "old.sql", "new.sql")
	for _, s := range []Selection{{}, {Staged: true}} {
		cs, _, err := Select(context.Background(), root, s)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range cs {
			if c.Path == "new.sql" && c.Status == 'R' && c.OldPath == "old.sql" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%+v: %+v", s, cs)
		}
	}
	if _, _, err := Select(context.Background(), root, Selection{Base: "no-such-ref"}); err == nil {
		t.Fatal("a bad --base ref must fail")
	}
}
